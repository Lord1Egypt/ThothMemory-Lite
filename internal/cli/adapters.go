package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ServerName is the key under which every client registers ThothMemory Lite.
// It differs from UltraLite's "thothmemory" so both can be registered.
const ServerName = "thoth-lite"

type Scope string

const (
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
)

// Env is everything an adapter needs, injectable for tests.
type Env struct {
	Dir    string                       // project directory (absolute)
	Home   string                       // user home directory
	Getenv func(string) string          // environment lookup
	Look   func(string) (string, error) // executable lookup
	Run    func(ctx context.Context, dir string, name string, args ...string) (string, error)
	Launch func(client string) (exe string, args []string) // how the client starts mcp-stdio
}

// DefaultEnv uses the real environment.
func DefaultEnv(dir string, launch func(string) (string, []string)) Env {
	home, _ := os.UserHomeDir()
	abs, _ := filepath.Abs(dir)
	return Env{Dir: abs, Home: home, Getenv: os.Getenv, Look: exec.LookPath, Launch: launch,
		Run: func(ctx context.Context, dir, name string, args ...string) (string, error) {
			ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir = dir
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			e := cmd.Run()
			return out.String(), e
		}}
}

func (e Env) env(name, fallback string) string {
	if v := e.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// ClientInfo is what detection found.
type ClientInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Installed  bool   `json:"installed"`
	Executable string `json:"executable,omitempty"`
	Version    string `json:"version,omitempty"`
	Evidence   string `json:"evidence"`
}

// Capabilities describe what Lite implements for a client. They are a
// statement about this code, not about runtime verification (see Verify).
type Capabilities struct {
	Transport    string   `json:"transport"`
	ConfigFormat string   `json:"config_format"`
	Scopes       []Scope  `json:"scopes"`
	ConfigFiles  []string `json:"config_files"`
	Instructions string   `json:"instructions"`
	Lifecycle    string   `json:"lifecycle"`
	RuntimeCheck string   `json:"runtime_check"`
	Notes        string   `json:"notes,omitempty"`
}

// Edit is one file change; After == nil deletes a file Lite created.
type Edit struct {
	Path   string
	Before []byte
	After  []byte
}

type Plan struct {
	Client string
	Scope  Scope
	Edits  []Edit
	Notes  []string
}

// Check is one verification level. Status is verified, failed, pending,
// not-run or unsupported, never inferred.
type Check struct {
	Level  string `json:"level"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Report struct {
	Client string  `json:"client"`
	Scope  Scope   `json:"scope"`
	Checks []Check `json:"checks"`
}

type Adapter interface {
	ID() string
	Name() string
	Detect(ctx context.Context, e Env) ClientInfo
	Capabilities() Capabilities
	ConfigPath(e Env, s Scope) (string, error)
	PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error)
	PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error)
	Verify(ctx context.Context, e Env, s Scope) Report
}

// Adapters is the registry, in documentation order.
func Adapters() []Adapter {
	return []Adapter{claudeAdapter{}, codexAdapter{}, opencodeAdapter{}, antigravityAdapter{}, clineAdapter{}, rooAdapter{}, cursorAdapter{}, continueAdapter{}}
}

func AdapterFor(id string) (Adapter, error) {
	for _, a := range Adapters() {
		if a.ID() == id {
			return a, nil
		}
	}
	ids := []string{}
	for _, a := range Adapters() {
		ids = append(ids, a.ID())
	}
	return nil, fmt.Errorf("unknown client %q; supported: %s", id, strings.Join(ids, ", "))
}

// Apply writes a plan's files with backups of anything replaced.
func Apply(p Plan) error {
	for _, ed := range p.Edits {
		if ed.After == nil {
			if e := os.Remove(ed.Path); e != nil && !os.IsNotExist(e) {
				return e
			}
			continue
		}
		if e := save(ed.Path, ed.After); e != nil {
			return e
		}
	}
	return nil
}

// ---------------------------------------------------------------- helpers

var versionRE = regexp.MustCompile(`\d+\.\d+\.\d+`)

func detectBinary(ctx context.Context, e Env, id, name string, bins ...string) ClientInfo {
	info := ClientInfo{ID: id, Name: name}
	for _, b := range bins {
		path, err := e.Look(b)
		if err != nil {
			continue
		}
		info.Installed, info.Executable = true, path
		out, err := e.Run(ctx, e.Dir, path, "--version")
		if err == nil {
			info.Version = versionRE.FindString(out)
		}
		info.Evidence = "executable " + b + " on PATH"
		return info
	}
	info.Evidence = "no " + strings.Join(bins, "/") + " executable on PATH"
	return info
}

func existsFile(p string) bool { st, e := os.Stat(p); return e == nil && !st.IsDir() }
func existsDir(p string) bool  { st, e := os.Stat(p); return e == nil && st.IsDir() }

func readFile(p string) ([]byte, error) {
	b, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		return nil, nil
	}
	return b, e
}

// jsonServers plans adding/removing entry under m[key][ServerName] in a JSON(C)
// file, refusing to replace a different existing registration.
func jsonServers(path, key string, entry map[string]any, remove bool) (Edit, bool, error) {
	before, e := readFile(path)
	if e != nil {
		return Edit{}, false, e
	}
	m := map[string]any{}
	if len(bytes.TrimSpace(before)) > 0 {
		if m, e = JSONC(before); e != nil {
			return Edit{}, false, fmt.Errorf("%s is not valid JSON: %w; no files changed", path, e)
		}
	}
	servers, e := object(m, key)
	if e != nil {
		return Edit{}, false, fmt.Errorf("%s: %w", path, e)
	}
	old, exists := servers[ServerName]
	if remove {
		if !exists {
			return Edit{}, false, nil
		}
		delete(servers, ServerName)
		return Edit{Path: path, Before: before, After: jsonBytes(m)}, true, nil
	}
	if exists {
		if string(jsonBytes(old)) == string(jsonBytes(entry)) {
			return Edit{}, false, nil
		}
		return Edit{}, false, fmt.Errorf("%s already has a different %q registration; review it (or run disconnect) before replacing", path, ServerName)
	}
	servers[ServerName] = entry
	return Edit{Path: path, Before: before, After: jsonBytes(m)}, true, nil
}

func plan(client string, s Scope, ed Edit, changed bool, notes ...string) Plan {
	p := Plan{Client: client, Scope: s, Notes: notes}
	if changed {
		p.Edits = append(p.Edits, ed)
	}
	return p
}

// configCheck reports whether the configuration holds our server entry.
func configCheck(path string, find func([]byte) bool) Check {
	b, e := readFile(path)
	switch {
	case e != nil:
		return Check{"configured", "failed", e.Error()}
	case b == nil:
		return Check{"configured", "failed", path + " does not exist"}
	case !find(b):
		return Check{"configured", "failed", fmt.Sprintf("%s has no %q entry", path, ServerName)}
	}
	return Check{"configured", "verified", path}
}

func jsonHas(key string) func([]byte) bool {
	return func(b []byte) bool {
		m, e := JSONC(b)
		if e != nil {
			return false
		}
		s, ok := m[key].(map[string]any)
		if !ok {
			return false
		}
		_, ok = s[ServerName]
		return ok
	}
}

func scopeError(client string, s Scope) error {
	return fmt.Errorf("%s does not support %s-scope registration in ThothMemory Lite", client, s)
}

// runtimeCheck runs a client's own listing command and classifies our line.
func runtimeCheck(ctx context.Context, e Env, info ClientInfo, args []string, classify func(line string) (string, string)) Check {
	if !info.Installed {
		return Check{"client-runtime", "not-run", info.Evidence}
	}
	out, err := e.Run(ctx, e.Dir, info.Executable, args...)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, ServerName) {
			status, detail := classify(stripANSI(line))
			return Check{"client-runtime", status, strings.Join(append([]string{filepath.Base(info.Executable)}, args...), " ") + ": " + detail}
		}
	}
	detail := "server not listed"
	if err != nil {
		detail += " (" + err.Error() + ")"
	}
	return Check{"client-runtime", "failed", detail}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return strings.TrimSpace(ansi.ReplaceAllString(s, "")) }

// ---------------------------------------------------------------- Claude Code

type claudeAdapter struct{}

func (claudeAdapter) ID() string   { return "claude" }
func (claudeAdapter) Name() string { return "Claude Code" }
func (claudeAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	return detectBinary(ctx, e, "claude", "Claude Code", "claude")
}
func (claudeAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio", ConfigFormat: "JSON (mcpServers)", Scopes: []Scope{ScopeProject, ScopeUser},
		ConfigFiles:  []string{"project: .mcp.json", "user: ~/.claude.json (or $CLAUDE_CONFIG_DIR/.claude.json)"},
		Instructions: "CLAUDE.md (imports the protocol file)",
		Lifecycle:    "hooks in .claude/settings.json: SessionStart, UserPromptSubmit, PreCompact, Stop, SessionEnd (thoth-lite hooks claude)",
		RuntimeCheck: "claude mcp list (starts the server and health-checks it)",
		Notes:        "Project servers stay 'pending approval' until a person approves them in Claude Code."}
}
func (a claudeAdapter) ConfigPath(e Env, s Scope) (string, error) {
	if s == ScopeProject {
		return filepath.Join(e.Dir, ".mcp.json"), nil
	}
	return filepath.Join(e.env("CLAUDE_CONFIG_DIR", e.Home), ".claude.json"), nil
}
func (a claudeAdapter) entry(e Env) map[string]any {
	exe, args := e.Launch(a.ID())
	return map[string]any{"type": "stdio", "command": exe, "args": args}
}
func (a claudeAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, _ := a.ConfigPath(e, s)
	ed, changed, err := jsonServers(path, "mcpServers", a.entry(e), false)
	note := "Restart Claude Code."
	if s == ScopeProject {
		note = "Restart Claude Code and approve the project server when it asks (until then `claude mcp list` shows it pending)."
	}
	return plan(a.ID(), s, ed, changed, note), err
}
func (a claudeAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, _ := a.ConfigPath(e, s)
	ed, changed, err := jsonServers(path, "mcpServers", nil, true)
	return plan(a.ID(), s, ed, changed), err
}
func (a claudeAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, _ := a.ConfigPath(e, s)
	r := Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, jsonHas("mcpServers"))}}
	r.Checks = append(r.Checks, runtimeCheck(ctx, e, a.Detect(ctx, e), []string{"mcp", "list"}, func(l string) (string, string) {
		switch {
		case strings.Contains(l, "Connected"):
			return "verified", "connected"
		case strings.Contains(l, "Pending approval"):
			return "pending", "configured; waiting for approval in Claude Code"
		}
		return "failed", l
	}))
	return r
}

// ---------------------------------------------------------------- Codex

type codexAdapter struct{}

func (codexAdapter) ID() string   { return "codex" }
func (codexAdapter) Name() string { return "Codex CLI" }
func (codexAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	return detectBinary(ctx, e, "codex", "Codex CLI", "codex")
}
func (codexAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio", ConfigFormat: "TOML ([mcp_servers.<name>])", Scopes: []Scope{ScopeProject, ScopeUser},
		ConfigFiles:  []string{"project: .codex/config.toml (loaded for trusted projects)", "user: ~/.codex/config.toml (or $CODEX_HOME)"},
		Instructions: "AGENTS.md",
		Lifecycle:    "hooks in .codex/hooks.json when Codex hooks are enabled (thoth-lite hooks codex)",
		RuntimeCheck: "codex mcp list (lists configuration; does not start the server)"}
}
func (a codexAdapter) ConfigPath(e Env, s Scope) (string, error) {
	if s == ScopeProject {
		return filepath.Join(e.Dir, ".codex", "config.toml"), nil
	}
	return filepath.Join(e.env("CODEX_HOME", filepath.Join(e.Home, ".codex")), "config.toml"), nil
}
func (a codexAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, _ := a.ConfigPath(e, s)
	before, err := readFile(path)
	if err != nil {
		return Plan{}, err
	}
	exe, args := e.Launch(a.ID())
	after, err := MergeCodex(before, ServerName, exe, args)
	if err != nil {
		return Plan{}, err
	}
	notes := []string{"Codex reads a project .codex/config.toml only for trusted projects."}
	return plan(a.ID(), s, Edit{path, before, after}, string(after) != string(before), notes...), nil
}
func (a codexAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, _ := a.ConfigPath(e, s)
	before, err := readFile(path)
	if err != nil || before == nil {
		return Plan{Client: a.ID(), Scope: s}, err
	}
	after, removed, err := RemoveCodex(before, ServerName)
	return plan(a.ID(), s, Edit{path, before, after}, removed), err
}
func (a codexAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, _ := a.ConfigPath(e, s)
	r := Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, func(b []byte) bool { return CodexHas(b, ServerName) })}}
	r.Checks = append(r.Checks, runtimeCheck(ctx, e, a.Detect(ctx, e), []string{"mcp", "list"}, func(l string) (string, string) {
		if strings.Contains(l, "enabled") {
			return "listed", "enabled in Codex's configuration (Codex's list does not start the server)"
		}
		return "failed", l
	}))
	return r
}

// ---------------------------------------------------------------- OpenCode

type opencodeAdapter struct{}

// OpenCodeV2Since is the first OpenCode release that accepts the V2
// "mcp.servers" configuration, measured by running `opencode mcp list`
// against real releases: 1.18.23 rejects it, 1.18.24 accepts it.
var OpenCodeV2Since = [3]int{1, 18, 24}

func (opencodeAdapter) ID() string   { return "opencode" }
func (opencodeAdapter) Name() string { return "OpenCode" }
func (opencodeAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	return detectBinary(ctx, e, "opencode", "OpenCode", "opencode")
}
func (opencodeAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio (type local)", ConfigFormat: "JSON/JSONC; legacy mcp.<name> or V2 mcp.servers.<name> (V2 needs OpenCode >= 1.18.24)",
		Scopes:       []Scope{ScopeProject, ScopeUser},
		ConfigFiles:  []string{"project: opencode.json(c)", "user: ~/.config/opencode/opencode.json(c) (or $XDG_CONFIG_HOME)"},
		Instructions: "AGENTS.md",
		Lifecycle:    "plugin .opencode/plugins/thoth-lite.js: session start context, idle, error, compaction (thoth-lite hooks opencode)",
		RuntimeCheck: "opencode mcp list (starts the server and reports connected)"}
}
func (a opencodeAdapter) ConfigPath(e Env, s Scope) (string, error) {
	base := e.Dir
	if s == ScopeUser {
		base = filepath.Join(e.env("XDG_CONFIG_HOME", filepath.Join(e.Home, ".config")), "opencode")
	}
	if existsFile(filepath.Join(base, "opencode.jsonc")) {
		return filepath.Join(base, "opencode.jsonc"), nil
	}
	return filepath.Join(base, "opencode.json"), nil
}

// ParseVersion reads "1.18.24" (or a line containing it).
func ParseVersion(s string) ([3]int, bool) {
	m := versionRE.FindString(s)
	if m == "" {
		return [3]int{}, false
	}
	var v [3]int
	for i, part := range strings.Split(m, ".") {
		v[i], _ = strconv.Atoi(part)
	}
	return v, true
}

func versionAtLeast(v, min [3]int) bool {
	for i := range v {
		if v[i] != min[i] {
			return v[i] > min[i]
		}
	}
	return true
}

// OpenCodeEntry chooses the configuration shape for a file and runtime. A V2
// file is only written for a runtime known to accept V2; a legacy file always
// gets a legacy entry, which every tested release accepts.
func OpenCodeEntry(m map[string]any, version string, command []string) (schema string, path []string, entry map[string]any, err error) {
	schema, err = OpenCodeSchema(m)
	if err != nil {
		return "", nil, nil, err
	}
	if schema == "v2" {
		v, ok := ParseVersion(version)
		if !ok {
			return "", nil, nil, errors.New("this opencode config uses the V2 format, but the OpenCode version is unknown; install OpenCode or register manually")
		}
		if !versionAtLeast(v, OpenCodeV2Since) {
			return "", nil, nil, fmt.Errorf("this opencode config uses the V2 format, which OpenCode %s rejects (V2 is accepted from 1.18.24); no files changed", version)
		}
		return "v2", []string{"mcp", "servers"}, map[string]any{"type": "local", "command": command, "disabled": false}, nil
	}
	return "legacy", []string{"mcp"}, map[string]any{"type": "local", "command": command, "enabled": true}, nil
}

func (a opencodeAdapter) edit(ctx context.Context, e Env, s Scope, remove bool) (Plan, error) {
	path, _ := a.ConfigPath(e, s)
	before, err := readFile(path)
	if err != nil {
		return Plan{}, err
	}
	m := map[string]any{}
	if len(bytes.TrimSpace(before)) > 0 {
		if m, err = JSONC(before); err != nil {
			return Plan{}, fmt.Errorf("%s is not valid JSON: %w; no files changed", path, err)
		}
	}
	exe, args := e.Launch(a.ID())
	info := a.Detect(ctx, e)
	schema, keys, entry, err := OpenCodeEntry(m, info.Version, append([]string{exe}, args...))
	if err != nil && !remove {
		return Plan{}, err
	}
	if remove {
		// Remove from whichever shape holds it.
		changed := false
		if mc, ok := m["mcp"].(map[string]any); ok {
			if _, ok := mc[ServerName]; ok {
				delete(mc, ServerName)
				changed = true
			}
			if sv, ok := mc["servers"].(map[string]any); ok {
				if _, ok := sv[ServerName]; ok {
					delete(sv, ServerName)
					changed = true
				}
			}
		}
		return plan(a.ID(), s, Edit{path, before, jsonBytes(m)}, changed), nil
	}
	parent := m
	for _, k := range keys {
		if parent, err = object(parent, k); err != nil {
			return Plan{}, fmt.Errorf("%s: %w", path, err)
		}
	}
	if old, ok := parent[ServerName]; ok {
		if string(jsonBytes(old)) == string(jsonBytes(entry)) {
			return Plan{Client: a.ID(), Scope: s}, nil
		}
		return Plan{}, fmt.Errorf("%s already has a different %q registration; review it (or run disconnect) before replacing", path, ServerName)
	}
	parent[ServerName] = entry
	note := fmt.Sprintf("OpenCode %s, %s configuration format.", map[bool]string{true: info.Version, false: "(not installed)"}[info.Installed], schema)
	return plan(a.ID(), s, Edit{path, before, jsonBytes(m)}, true, note), nil
}
func (a opencodeAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	return a.edit(ctx, e, s, false)
}
func (a opencodeAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	return a.edit(ctx, e, s, true)
}
func (a opencodeAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, _ := a.ConfigPath(e, s)
	r := Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, func(b []byte) bool {
		m, err := JSONC(b)
		if err != nil {
			return false
		}
		mc, _ := m["mcp"].(map[string]any)
		if _, ok := mc[ServerName]; ok {
			return true
		}
		sv, _ := mc["servers"].(map[string]any)
		_, ok := sv[ServerName]
		return ok
	})}}
	r.Checks = append(r.Checks, runtimeCheck(ctx, e, a.Detect(ctx, e), []string{"mcp", "list"}, func(l string) (string, string) {
		if strings.Contains(l, "connected") && !strings.Contains(l, "failed") {
			return "verified", "connected"
		}
		return "failed", l
	}))
	return r
}

// ---------------------------------------------------------------- Antigravity

type antigravityAdapter struct{}

func (antigravityAdapter) ID() string   { return "antigravity" }
func (antigravityAdapter) Name() string { return "Google Antigravity" }
func (antigravityAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	return detectBinary(ctx, e, "antigravity", "Google Antigravity", "agy", "antigravity")
}
func (antigravityAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio", ConfigFormat: "JSON (mcpServers)", Scopes: []Scope{ScopeUser},
		ConfigFiles:  []string{"user: ~/.gemini/config/mcp_config.json (the file `agy mcp add` writes)"},
		Instructions: "AGENTS.md / GEMINI.md (not runtime-verified)",
		Lifecycle:    "none implemented: instruction-based workflow (start, checkpoint and finish through MCP)",
		RuntimeCheck: "agy mcp list (lists configuration; does not start the server)",
		Notes:        "The Antigravity CLI has no project-scoped MCP configuration."}
}
func (a antigravityAdapter) ConfigPath(e Env, s Scope) (string, error) {
	if s != ScopeUser {
		return "", scopeError(a.Name(), s)
	}
	return filepath.Join(e.Home, ".gemini", "config", "mcp_config.json"), nil
}
func (a antigravityAdapter) entry(e Env) map[string]any {
	exe, args := e.Launch(a.ID())
	return map[string]any{"command": exe, "args": args, "disabled": false}
}
func (a antigravityAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	ed, changed, err := jsonServers(path, "mcpServers", a.entry(e), false)
	return plan(a.ID(), s, ed, changed), err
}
func (a antigravityAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	ed, changed, err := jsonServers(path, "mcpServers", nil, true)
	return plan(a.ID(), s, ed, changed), err
}
func (a antigravityAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Report{Client: a.ID(), Scope: s, Checks: []Check{{"configured", "unsupported", err.Error()}}}
	}
	r := Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, jsonHas("mcpServers"))}}
	r.Checks = append(r.Checks, runtimeCheck(ctx, e, a.Detect(ctx, e), []string{"mcp", "list"}, func(l string) (string, string) {
		if strings.Contains(l, "enabled") {
			return "listed", "enabled in Antigravity's configuration (its list does not start the server)"
		}
		return "failed", l
	}))
	return r
}

// ---------------------------------------------------------------- Cline

type clineAdapter struct{}

func (clineAdapter) ID() string   { return "cline" }
func (clineAdapter) Name() string { return "Cline" }

// candidates are the documented locations of cline_mcp_settings.json, newest first.
func (clineAdapter) candidates(e Env) []string {
	var out []string
	if v := e.Getenv("CLINE_MCP_SETTINGS_PATH"); v != "" {
		out = append(out, v)
	}
	out = append(out, filepath.Join(e.Home, ".cline", "data", "settings", "cline_mcp_settings.json"))
	return append(out, vscodeGlobalStorage(e, "saoudrizwan.claude-dev", "cline_mcp_settings.json")...)
}
func (a clineAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	info := ClientInfo{ID: a.ID(), Name: a.Name(), Evidence: "no Cline MCP settings file found"}
	for _, p := range a.candidates(e) {
		if existsFile(p) {
			info.Installed, info.Evidence = true, "settings file "+p
			return info
		}
	}
	if b := detectBinary(ctx, e, a.ID(), a.Name(), "cline"); b.Installed {
		return b
	}
	return info
}
func (clineAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio", ConfigFormat: "JSON (mcpServers; disabled, autoApprove)", Scopes: []Scope{ScopeUser},
		ConfigFiles:  []string{"$CLINE_MCP_SETTINGS_PATH", "~/.cline/data/settings/cline_mcp_settings.json (Cline 4.x)", "VS Code globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json (older)"},
		Instructions: "AGENTS.md or .clinerules (not runtime-verified)",
		Lifecycle:    "none implemented: instruction-based workflow",
		RuntimeCheck: "none available here (Cline is an editor extension); check the MCP Servers panel",
		Notes:        "The settings file location changed across Cline versions; Lite only edits a file that already exists (open Cline's MCP settings once to create it)."}
}
func (a clineAdapter) ConfigPath(e Env, s Scope) (string, error) {
	if s != ScopeUser {
		return "", scopeError(a.Name(), s)
	}
	for _, p := range a.candidates(e) {
		if existsFile(p) {
			return p, nil
		}
	}
	return "", errors.New("no Cline MCP settings file found; open Cline → MCP Servers → Configure once to create it, or set CLINE_MCP_SETTINGS_PATH, then rerun")
}
func (a clineAdapter) entry(e Env) map[string]any {
	exe, args := e.Launch(a.ID())
	return map[string]any{"command": exe, "args": args, "disabled": false, "autoApprove": []any{}}
}
func (a clineAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	ed, changed, err := jsonServers(path, "mcpServers", a.entry(e), false)
	return plan(a.ID(), s, ed, changed, "Cline asks before each tool call unless you approve the tools."), err
}
func (a clineAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	ed, changed, err := jsonServers(path, "mcpServers", nil, true)
	return plan(a.ID(), s, ed, changed), err
}
func (a clineAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Report{Client: a.ID(), Scope: s, Checks: []Check{{"configured", "failed", err.Error()}}}
	}
	return Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, jsonHas("mcpServers")),
		{"client-runtime", "not-run", "no command-line check exists for the Cline extension"}}}
}

// vscodeGlobalStorage returns an extension's settings file under each VS Code
// flavour's globalStorage for this OS.
func vscodeGlobalStorage(e Env, extension, file string) []string {
	var bases []string
	switch runtime.GOOS {
	case "windows":
		bases = []string{e.env("APPDATA", filepath.Join(e.Home, "AppData", "Roaming"))}
	case "darwin":
		bases = []string{filepath.Join(e.Home, "Library", "Application Support")}
	default:
		bases = []string{e.env("XDG_CONFIG_HOME", filepath.Join(e.Home, ".config"))}
	}
	var out []string
	for _, b := range bases {
		for _, flavour := range []string{"Code", "Code - Insiders", "VSCodium"} {
			out = append(out, filepath.Join(b, flavour, "User", "globalStorage", extension, "settings", file))
		}
	}
	return out
}

// ---------------------------------------------------------------- Roo Code

type rooAdapter struct{}

func (rooAdapter) ID() string   { return "roo" }
func (rooAdapter) Name() string { return "Roo Code" }
func (a rooAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	info := ClientInfo{ID: a.ID(), Name: a.Name(), Evidence: "no Roo Code settings found"}
	for _, p := range vscodeGlobalStorage(e, "rooveterinaryinc.roo-cline", "mcp_settings.json") {
		if existsFile(p) {
			info.Installed, info.Evidence = true, "settings file "+p
		}
	}
	if existsDir(filepath.Join(e.Dir, ".roo")) {
		info.Evidence += "; project has .roo/"
	}
	return info
}
func (rooAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio", ConfigFormat: "JSON (mcpServers)", Scopes: []Scope{ScopeProject, ScopeUser},
		ConfigFiles:  []string{"project: .roo/mcp.json", "user: VS Code globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json"},
		Instructions: "AGENTS.md or .roo/rules (not runtime-verified)",
		Lifecycle:    "none implemented: instruction-based workflow",
		RuntimeCheck: "none available here (Roo Code is an editor extension)"}
}
func (a rooAdapter) ConfigPath(e Env, s Scope) (string, error) {
	if s == ScopeProject {
		return filepath.Join(e.Dir, ".roo", "mcp.json"), nil
	}
	for _, p := range vscodeGlobalStorage(e, "rooveterinaryinc.roo-cline", "mcp_settings.json") {
		if existsFile(p) {
			return p, nil
		}
	}
	return "", errors.New("no Roo Code global MCP settings found; use --scope project (.roo/mcp.json) or open Roo Code's MCP settings once")
}
func (a rooAdapter) entry(e Env) map[string]any {
	exe, args := e.Launch(a.ID())
	return map[string]any{"type": "stdio", "command": exe, "args": args}
}
func (a rooAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	ed, changed, err := jsonServers(path, "mcpServers", a.entry(e), false)
	return plan(a.ID(), s, ed, changed), err
}
func (a rooAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	ed, changed, err := jsonServers(path, "mcpServers", nil, true)
	return plan(a.ID(), s, ed, changed), err
}
func (a rooAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Report{Client: a.ID(), Scope: s, Checks: []Check{{"configured", "failed", err.Error()}}}
	}
	return Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, jsonHas("mcpServers")),
		{"client-runtime", "not-run", "no command-line check exists for the Roo Code extension"}}}
}

// ---------------------------------------------------------------- Cursor

type cursorAdapter struct{}

func (cursorAdapter) ID() string   { return "cursor" }
func (cursorAdapter) Name() string { return "Cursor" }
func (a cursorAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	info := detectBinary(ctx, e, a.ID(), a.Name(), "cursor-agent", "cursor")
	if !info.Installed && existsDir(filepath.Join(e.Home, ".cursor")) {
		info.Installed, info.Evidence = true, "~/.cursor exists"
	}
	return info
}
func (cursorAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio", ConfigFormat: "JSON (mcpServers)", Scopes: []Scope{ScopeProject, ScopeUser},
		ConfigFiles:  []string{"project: .cursor/mcp.json", "user: ~/.cursor/mcp.json"},
		Instructions: "AGENTS.md or .cursor/rules (not runtime-verified)",
		Lifecycle:    "none implemented: instruction-based workflow",
		RuntimeCheck: "cursor-agent mcp list when the Cursor CLI is installed"}
}
func (a cursorAdapter) ConfigPath(e Env, s Scope) (string, error) {
	if s == ScopeProject {
		return filepath.Join(e.Dir, ".cursor", "mcp.json"), nil
	}
	return filepath.Join(e.Home, ".cursor", "mcp.json"), nil
}
func (a cursorAdapter) entry(e Env) map[string]any {
	exe, args := e.Launch(a.ID())
	return map[string]any{"type": "stdio", "command": exe, "args": args}
}
func (a cursorAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, _ := a.ConfigPath(e, s)
	ed, changed, err := jsonServers(path, "mcpServers", a.entry(e), false)
	return plan(a.ID(), s, ed, changed), err
}
func (a cursorAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, _ := a.ConfigPath(e, s)
	ed, changed, err := jsonServers(path, "mcpServers", nil, true)
	return plan(a.ID(), s, ed, changed), err
}
func (a cursorAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, _ := a.ConfigPath(e, s)
	r := Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, jsonHas("mcpServers"))}}
	info := detectBinary(ctx, e, a.ID(), a.Name(), "cursor-agent")
	if !info.Installed {
		return append2(r, Check{"client-runtime", "not-run", "cursor-agent is not installed"})
	}
	return append2(r, runtimeCheck(ctx, e, info, []string{"mcp", "list"}, func(l string) (string, string) {
		if strings.Contains(strings.ToLower(l), "error") || strings.Contains(strings.ToLower(l), "fail") {
			return "failed", l
		}
		return "listed", l
	}))
}

func append2(r Report, c Check) Report { r.Checks = append(r.Checks, c); return r }

// ---------------------------------------------------------------- Continue

type continueAdapter struct{}

func (continueAdapter) ID() string   { return "continue" }
func (continueAdapter) Name() string { return "Continue" }
func (a continueAdapter) Detect(ctx context.Context, e Env) ClientInfo {
	info := detectBinary(ctx, e, a.ID(), a.Name(), "cn")
	if !info.Installed && existsDir(filepath.Join(e.Home, ".continue")) {
		info.Installed, info.Evidence = true, "~/.continue exists"
	}
	return info
}
func (continueAdapter) Capabilities() Capabilities {
	return Capabilities{Transport: "stdio", ConfigFormat: "YAML block (name, version, schema: v1, mcpServers list)", Scopes: []Scope{ScopeProject},
		ConfigFiles:  []string{"project: .continue/mcpServers/thoth-lite.yaml (a file Lite owns)"},
		Instructions: ".continue/rules (not runtime-verified)",
		Lifecycle:    "none implemented: instruction-based workflow (MCP tools work in Continue's agent mode)",
		RuntimeCheck: "none available here (Continue is an editor extension)",
		Notes:        "User scope would mean editing ~/.continue/config.yaml; Lite does not edit YAML it does not own."}
}
func (a continueAdapter) ConfigPath(e Env, s Scope) (string, error) {
	if s != ScopeProject {
		return "", scopeError(a.Name(), s)
	}
	return filepath.Join(e.Dir, ".continue", "mcpServers", ServerName+".yaml"), nil
}

// ContinueBlock renders the YAML block. Strings are JSON-quoted, which is valid
// YAML, so no YAML library is needed.
func ContinueBlock(exe string, args []string) []byte {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	var b strings.Builder
	b.WriteString("# Generated by thoth-lite connect continue. Remove with: thoth-lite disconnect continue\n")
	b.WriteString("name: " + q("ThothMemory Lite") + "\nversion: " + q("1.0.0") + "\nschema: v1\nmcpServers:\n")
	b.WriteString("  - name: " + q(ServerName) + "\n    type: stdio\n    command: " + q(exe) + "\n    args:\n")
	for _, a := range args {
		b.WriteString("      - " + q(a) + "\n")
	}
	return []byte(b.String())
}
func (a continueAdapter) PlanInstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	exe, args := e.Launch(a.ID())
	before, err := readFile(path)
	if err != nil {
		return Plan{}, err
	}
	after := ContinueBlock(exe, args)
	if before != nil && !bytes.HasPrefix(before, []byte("# Generated by thoth-lite")) {
		return Plan{}, fmt.Errorf("%s exists and was not generated by thoth-lite; review it first", path)
	}
	return plan(a.ID(), s, Edit{path, before, after}, string(before) != string(after), "MCP tools are available in Continue's agent mode."), nil
}
func (a continueAdapter) PlanUninstall(ctx context.Context, e Env, s Scope) (Plan, error) {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Plan{}, err
	}
	before, err := readFile(path)
	if err != nil || before == nil {
		return Plan{Client: a.ID(), Scope: s}, err
	}
	if !bytes.HasPrefix(before, []byte("# Generated by thoth-lite")) {
		return Plan{}, fmt.Errorf("%s was not generated by thoth-lite; not removing it", path)
	}
	return plan(a.ID(), s, Edit{Path: path, Before: before}, true), nil
}
func (a continueAdapter) Verify(ctx context.Context, e Env, s Scope) Report {
	path, err := a.ConfigPath(e, s)
	if err != nil {
		return Report{Client: a.ID(), Scope: s, Checks: []Check{{"configured", "unsupported", err.Error()}}}
	}
	return Report{Client: a.ID(), Scope: s, Checks: []Check{configCheck(path, func(b []byte) bool { return bytes.Contains(b, []byte(`"`+ServerName+`"`)) }),
		{"client-runtime", "not-run", "no command-line check exists for the Continue extension"}}}
}
