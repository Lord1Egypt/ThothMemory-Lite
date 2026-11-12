package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv simulates installed clients: versions maps a binary to its
// --version output; lists maps it to its "mcp list" output.
func fakeEnv(t *testing.T, versions, lists map[string]string) Env {
	t.Helper()
	root := t.TempDir()
	dir, home := filepath.Join(root, "project"), filepath.Join(root, "home")
	for _, d := range []string{dir, home} {
		if e := os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	return Env{
		Dir: dir, Home: home,
		Getenv: func(string) string { return "" },
		Look: func(b string) (string, error) {
			if _, ok := versions[b]; ok {
				return "/fake/bin/" + b, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, _ string, name string, args ...string) (string, error) {
			b := filepath.Base(name)
			if len(args) == 1 && args[0] == "--version" {
				return versions[b], nil
			}
			return lists[b], nil
		},
		Launch: func(client string) (string, []string) {
			return "/opt/thoth-lite", []string{"mcp-stdio", "--data", "/data", "--token-file", "/data/.secrets/" + client + "_token"}
		},
	}
}

func applyOK(t *testing.T, p Plan, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
	if e = Apply(p); e != nil {
		t.Fatal(e)
	}
}

// Every adapter installs, verifies its configuration, is idempotent, refuses a
// conflicting entry, keeps unrelated settings, and uninstalls only its entry.
func TestAdaptersInstallVerifyUninstall(t *testing.T) {
	ctx := context.Background()
	for _, a := range Adapters() {
		t.Run(a.ID(), func(t *testing.T) {
			env := fakeEnv(t, map[string]string{}, nil)
			scope := ScopeProject
			if !contains(a.Capabilities().Scopes, ScopeProject) {
				scope = ScopeUser
			}
			if a.ID() == "cline" {
				// Lite only edits an existing Cline settings file.
				if _, e := a.PlanInstall(ctx, env, scope); e == nil {
					t.Fatal("cline planned a file that does not exist yet")
				}
				p := filepath.Join(env.Home, ".cline", "data", "settings", "cline_mcp_settings.json")
				_ = os.MkdirAll(filepath.Dir(p), 0700)
				_ = os.WriteFile(p, []byte(`{"mcpServers":{"other":{"command":"x"}}}`), 0600)
			}
			path, err := a.ConfigPath(env, scope)
			if err != nil {
				t.Fatal(err)
			}
			if a.ID() != "continue" && a.ID() != "codex" && a.ID() != "cline" {
				_ = os.MkdirAll(filepath.Dir(path), 0700)
				_ = os.WriteFile(path, []byte(`{"keep":"me","mcpServers":{"other":{"command":"x"}},"mcp":{"other":{"type":"local","command":["x"]}}}`), 0600)
			}
			if a.ID() == "codex" {
				_ = os.MkdirAll(filepath.Dir(path), 0700)
				_ = os.WriteFile(path, []byte("# my settings\nmodel = \"custom\"\n"), 0600)
			}
			before, _ := os.ReadFile(path)
			p, e := a.PlanInstall(ctx, env, scope)
			applyOK(t, p, e)
			after, _ := os.ReadFile(path)
			if !bytes.Contains(after, []byte(ServerName)) || !bytes.Contains(after, []byte(a.ID()+"_token")) {
				t.Fatalf("entry missing or not using the client's own credential:\n%s", after)
			}
			for _, keep := range []string{"keep", "other", "custom"} {
				if bytes.Contains(before, []byte(keep)) && !bytes.Contains(after, []byte(keep)) {
					t.Fatalf("unrelated setting %q lost:\n%s", keep, after)
				}
			}
			if len(before) > 0 {
				backups, _ := filepath.Glob(path + ".thoth-backup-*")
				if len(backups) != 1 {
					t.Fatal("no backup of the replaced configuration")
				}
			}
			if r := a.Verify(ctx, env, scope); r.Checks[0].Status != "verified" {
				t.Fatalf("configured check: %+v", r.Checks)
			}
			again, e := a.PlanInstall(ctx, env, scope)
			if e != nil || len(again.Edits) != 0 {
				t.Fatal("install is not idempotent", e, again.Edits)
			}
			other := env
			other.Launch = func(string) (string, []string) { return "/elsewhere/thoth-lite", []string{"mcp-stdio"} }
			if _, e = a.PlanInstall(ctx, other, scope); e == nil && a.ID() != "continue" {
				t.Fatal("a different existing registration was replaced")
			}
			u, e := a.PlanUninstall(ctx, env, scope)
			applyOK(t, u, e)
			gone, _ := os.ReadFile(path)
			if bytes.Contains(gone, []byte(`"`+ServerName+`"`)) || bytes.Contains(gone, []byte("[mcp_servers."+ServerName)) {
				t.Fatalf("entry not removed:\n%s", gone)
			}
			for _, keep := range []string{"keep", "other", "custom"} {
				if bytes.Contains(before, []byte(keep)) && !bytes.Contains(gone, []byte(keep)) {
					t.Fatalf("uninstall removed unrelated setting %q", keep)
				}
			}
		})
	}
}

func contains(scopes []Scope, s Scope) bool {
	for _, x := range scopes {
		if x == s {
			return true
		}
	}
	return false
}

// The OpenCode adapter never writes V2 for a runtime that rejects it, writes
// V2 for runtimes that accept it, and leaves the file untouched on refusal.
func TestOpenCodeAdapterV2(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		version string
		ok      bool
	}{{"1.17.13", false}, {"1.18.23", false}, {"1.18.24", true}, {"1.19.5", true}} {
		t.Run(tc.version, func(t *testing.T) {
			env := fakeEnv(t, map[string]string{"opencode": tc.version + "\n"}, map[string]string{"opencode": "●  ✓ thoth-lite \x1b[90mconnected\n"})
			path := filepath.Join(env.Dir, "opencode.json")
			original := []byte(`{"providers":{},"mcp":{"servers":{}}}`)
			_ = os.WriteFile(path, original, 0600)
			p, e := opencodeAdapter{}.PlanInstall(ctx, env, ScopeProject)
			if !tc.ok {
				if e == nil || !strings.Contains(e.Error(), "1.18.24") {
					t.Fatal("V2 written for a runtime that rejects it:", e)
				}
				if b, _ := os.ReadFile(path); !bytes.Equal(b, original) {
					t.Fatal("refused configuration was edited")
				}
				return
			}
			applyOK(t, p, e)
			b, _ := os.ReadFile(path)
			if !strings.Contains(string(b), `"servers": {`) || !strings.Contains(string(b), `"disabled": false`) {
				t.Fatalf("V2 entry not written:\n%s", b)
			}
			r := opencodeAdapter{}.Verify(ctx, env, ScopeProject)
			if r.Checks[1].Status != "verified" {
				t.Fatalf("runtime check: %+v", r.Checks)
			}
		})
	}
}

func TestRuntimeChecksClassifyClientOutput(t *testing.T) {
	ctx := context.Background()
	env := fakeEnv(t, map[string]string{"claude": "2.1.322 (Claude Code)", "codex": "codex-cli 0.166.0", "agy": "1.4.12"}, map[string]string{
		"claude": "thoth-lite: /opt/thoth-lite mcp-stdio - ⏸ Pending approval (run `claude` to approve)\n",
		"codex":  "thoth-lite  /opt/thoth-lite  mcp-stdio  -  -  enabled  Unsupported\n",
		"agy":    "thoth-lite  stdio  enabled  /opt/thoth-lite\n",
	})
	want := map[string]string{"claude": "pending", "codex": "listed", "antigravity": "listed"}
	for id, status := range want {
		a, _ := AdapterFor(id)
		scope := ScopeProject
		if id == "antigravity" {
			scope = ScopeUser
		}
		r := a.Verify(ctx, env, scope)
		if r.Checks[1].Status != status {
			t.Errorf("%s: %+v", id, r.Checks[1])
		}
	}
	if (cursorAdapter{}).Verify(ctx, env, ScopeProject).Checks[1].Status != "not-run" {
		t.Error("cursor runtime check should be not-run without cursor-agent")
	}
}

func TestEnsureCredentialNeverRestoresRevoked(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".secrets")
	if err := InitSecrets(dir); err != nil {
		t.Fatal(err)
	}
	for _, c := range ClientPrincipals {
		if _, err := os.Stat(filepath.Join(dir, c+"_token")); err != nil {
			t.Fatalf("fresh init lacks %s credential", c)
		}
	}
	// Simulate an older installation without the new clients.
	policy, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
	trimmed := strings.Replace(string(policy), `"id": "cursor"`, `"id": "cursor-old"`, 1)
	_ = os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(trimmed), 0600)
	_ = os.Remove(filepath.Join(dir, "cursor_token"))
	if err := EnsureCredential(dir, "cursor"); err != nil {
		t.Fatal("missing credential not created:", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if !strings.Contains(string(after), `"id": "cursor"`) {
		t.Fatal("policy entry not added")
	}
	if st, _ := os.Stat(filepath.Join(dir, "tokens.json")); st.Mode().Perm()&0077 != 0 && os.PathSeparator == '/' {
		t.Fatal("tokens.json is readable by others", st.Mode())
	}
	// Revoked: token file present, policy entry removed.
	if err := Permissions([]string{"cline", "--revoke", "--yes"}); err == nil {
		t.Fatal("permissions must run in the data directory")
	}
	t.Chdir(filepath.Dir(dir))
	if err := Permissions([]string{"cline", "--revoke", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCredential(dir, "cline"); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatal("revoked credential was restored:", err)
	}
}
