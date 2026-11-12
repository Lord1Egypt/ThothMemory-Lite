package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitDoesNotRestoreRevokedCredential(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := InitSecrets(".secrets"); err != nil {
		t.Fatal(err)
	}
	var ps []map[string]any
	b, _ := os.ReadFile(".secrets/tokens.json")
	_ = json.Unmarshal(b, &ps)
	kept := []map[string]any{}
	for _, p := range ps {
		if p["id"] != "codex" {
			kept = append(kept, p)
		}
	}
	b = jsonBytes(kept)
	if err := os.WriteFile(".secrets/tokens.json", b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := InitSecrets(".secrets"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(".secrets/tokens.json")
	if string(after) != string(b) {
		t.Fatal("installer restored an intentionally revoked principal")
	}
}

func TestInstructionsRespectIsolatedClientHomes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	codex := filepath.Join(home, "codex-custom")
	claude := filepath.Join(home, "claude-custom")
	xdg := filepath.Join(home, "xdg-custom")
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := Instructions([]string{"--scope", "user", "--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(codex, "AGENTS.md"), filepath.Join(claude, "CLAUDE.md"), filepath.Join(xdg, "opencode", "AGENTS.md")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("isolated instruction target missing: %s", p)
		}
	}
}

func TestPermissionNarrowingAndRevocationSurviveSetup(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := InitSecrets(".secrets"); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(".secrets/codex_token")
	project := "11111111-1111-4111-8111-111111111111"
	if err := Permissions([]string{"codex", "--projects", project, "--read-only", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if err := InitSecrets(".secrets"); err != nil {
		t.Fatal(err)
	}
	var policy []map[string]any
	b, _ := os.ReadFile(".secrets/tokens.json")
	_ = json.Unmarshal(b, &policy)
	for _, p := range policy {
		if p["id"] == "codex" {
			if p["write"] != false || p["projects"].([]any)[0] != project || p["token"] != string(original) {
				t.Fatal("policy or token changed", p["id"])
			}
		}
	}
	if err := Permissions([]string{"codex", "--revoke", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if err := InitSecrets(".secrets"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(".secrets/tokens.json")
	_ = json.Unmarshal(b, &policy)
	for _, p := range policy {
		if p["id"] == "codex" {
			t.Fatal("revocation reversed")
		}
	}
}
