package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/memory"
)

// ClientOptions are the flags shared by connect and disconnect.
type ClientOptions struct {
	Clients []Adapter
	Scope   Scope
	Dir     string
	Yes     bool
	DryRun  bool
}

// ParseClientArgs parses CLIENT|all [--scope project|user] [--dir PATH] [--yes] [--dry-run].
// "all" selects every client detected on this machine.
func ParseClientArgs(ctx context.Context, args []string, envFor func(dir string) Env) (ClientOptions, Env, error) {
	o := ClientOptions{Scope: ScopeProject, Dir: "."}
	if len(args) == 0 {
		return o, Env{}, errors.New("client required: " + strings.Join(clientIDs(), "|") + "|all")
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--scope", "--dir":
			if i+1 >= len(args) {
				return o, Env{}, errors.New(args[i] + " requires a value")
			}
			if args[i] == "--scope" {
				o.Scope = Scope(args[i+1])
			} else {
				o.Dir = args[i+1]
			}
			i++
		case "--yes":
			o.Yes = true
		case "--dry-run":
			o.DryRun = true
		default:
			return o, Env{}, fmt.Errorf("unknown option %q", args[i])
		}
	}
	if o.Scope != ScopeProject && o.Scope != ScopeUser {
		return o, Env{}, errors.New("scope must be project or user")
	}
	env := envFor(o.Dir)
	if args[0] == "all" {
		for _, a := range Adapters() {
			if a.Detect(ctx, env).Installed {
				o.Clients = append(o.Clients, a)
			}
		}
		if len(o.Clients) == 0 {
			return o, env, errors.New("no supported client detected; name one explicitly")
		}
		return o, env, nil
	}
	a, e := AdapterFor(args[0])
	if e != nil {
		return o, env, e
	}
	o.Clients = []Adapter{a}
	return o, env, nil
}

func clientIDs() []string {
	var ids []string
	for _, a := range Adapters() {
		ids = append(ids, a.ID())
	}
	return ids
}

func approve(paths []string, yes bool) error {
	if yes {
		return nil
	}
	if st, e := os.Stdin.Stat(); e != nil || st.Mode()&os.ModeCharDevice == 0 {
		return errors.New("these are client configuration files; rerun with --yes to approve the change")
	}
	fmt.Print("Write these files (existing ones are backed up first)? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.ToLower(strings.TrimSpace(line)) != "y" {
		return errors.New("cancelled; no files changed")
	}
	return nil
}

// Connect registers ThothMemory Lite with clients after showing every file it
// will change and getting approval. ensure creates the client's credential.
func Connect(ctx context.Context, o ClientOptions, env Env, ensure func(client string) error) error {
	return change(ctx, o, env, ensure, false)
}

// Disconnect removes only the thoth-lite entries Lite added.
func Disconnect(ctx context.Context, o ClientOptions, env Env) error {
	return change(ctx, o, env, nil, true)
}

func change(ctx context.Context, o ClientOptions, env Env, ensure func(string) error, remove bool) error {
	var plans []Plan
	var paths []string
	failed := 0
	for _, a := range o.Clients {
		var p Plan
		var e error
		if remove {
			p, e = a.PlanUninstall(ctx, env, o.Scope)
		} else {
			p, e = a.PlanInstall(ctx, env, o.Scope)
		}
		if e != nil {
			failed++
			fmt.Printf("%-12s skipped: %s\n", a.ID(), e)
			continue
		}
		if len(p.Edits) == 0 {
			fmt.Printf("%-12s already %s\n", a.ID(), map[bool]string{true: "absent", false: "registered"}[remove])
			continue
		}
		for _, ed := range p.Edits {
			action := map[bool]string{true: "modify", false: "create"}[ed.Before != nil]
			if ed.After == nil {
				action = "delete"
			}
			fmt.Printf("%-12s %s %s\n", a.ID(), action, ed.Path)
			paths = append(paths, ed.Path)
		}
		for _, n := range p.Notes {
			fmt.Printf("%-12s note: %s\n", "", n)
		}
		plans = append(plans, p)
	}
	if o.DryRun || len(plans) == 0 {
		if failed > 0 && len(plans) == 0 {
			return errors.New("nothing could be changed")
		}
		return nil
	}
	if e := approve(paths, o.Yes); e != nil {
		return e
	}
	for _, p := range plans {
		if !remove && ensure != nil {
			if e := ensure(p.Client); e != nil {
				return fmt.Errorf("%s: %w", p.Client, e)
			}
		}
		if e := Apply(p); e != nil {
			return fmt.Errorf("%s: %w", p.Client, e)
		}
	}
	fmt.Println("Done. Restart the affected clients. Check with: thoth-lite integrations doctor")
	if failed > 0 {
		return fmt.Errorf("%d client(s) skipped", failed)
	}
	return nil
}

// Observed returns recorded MCP handshakes keyed by client principal.
type Observed func(ctx context.Context) ([]memory.Row, error)

// Integrations implements `integrations list|capabilities|doctor`.
func Integrations(ctx context.Context, args []string, envFor func(dir string) Env, observed Observed) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	switch args[0] {
	case "capabilities":
		out := map[string]any{}
		for _, a := range Adapters() {
			out[a.ID()] = a.Capabilities()
		}
		return enc.Encode(out)
	case "list":
		env := envFor(".")
		rows, e := observed(ctx)
		if e != nil {
			return e
		}
		for _, a := range Adapters() {
			info := a.Detect(ctx, env)
			state := "not detected"
			if info.Installed {
				state = "detected"
				if info.Version != "" {
					state += " " + info.Version
				}
			}
			seen := "no MCP session recorded"
			for _, r := range rows {
				if r["principal_id"] == a.ID() {
					seen = fmt.Sprintf("last MCP session %v as %v %v", r["last_handshake_at"], r["client_name"], r["client_version"])
				}
			}
			fmt.Printf("%-12s %-22s %s\n", a.ID(), state, seen)
		}
		return nil
	case "doctor":
		o, env, e := ParseClientArgs(ctx, append([]string{firstOr(args[1:], "all")}, rest(args[1:])...), envFor)
		if e != nil {
			return e
		}
		var reports []Report
		for _, a := range o.Clients {
			r := a.Verify(ctx, env, o.Scope)
			// Read observed sessions after the client's own check: that check may
			// itself start a session.
			rows, e := observed(ctx)
			if e != nil {
				return e
			}
			obs := Check{"observed-session", "not-run", "no MCP session from this client's credential recorded yet"}
			for _, row := range rows {
				if row["principal_id"] == a.ID() {
					obs = Check{"observed-session", "verified", fmt.Sprintf("%v %v completed an MCP session at %v", row["client_name"], row["client_version"], row["last_handshake_at"])}
				}
			}
			r.Checks = append(r.Checks, obs)
			reports = append(reports, r)
		}
		return enc.Encode(reports)
	}
	return errors.New("integrations list|capabilities|doctor [CLIENT] [--scope project|user] [--dir PATH]")
}

func firstOr(args []string, d string) string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		return args[0]
	}
	return d
}

func rest(args []string) []string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		return args[1:]
	}
	return args
}

// LaunchFor returns how each client starts the stdio server: the executable,
// the data directory and that client's own token file.
func LaunchFor(exe, dataDir, secrets string) func(string) (string, []string) {
	return func(client string) (string, []string) {
		return exe, []string{"mcp-stdio", "--data", dataDir, "--token-file", filepath.Join(secrets, client+"_token")}
	}
}
