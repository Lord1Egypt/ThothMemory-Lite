package tests

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Regression tests for the two lifecycle registration failures reported
// against UltraLite:
//
//	"no origin remote; register a local:<uuid> project explicitly"
//	"project not registered; the start event registers it"
//
// They run the installed Claude Code hook commands through the shell, exactly
// as the client does.

func bareRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@example.invalid"}, {"config", "user.name", "Tester"}} {
		if out, e := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); e != nil {
			t.Fatal(string(out))
		}
	}
	commit(t, repo, "a.txt", "a")
	return repo
}

func hookedFixture(t *testing.T, repo string) (fixture, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hook command strings use POSIX sh")
	}
	f := fixture{t: t, home: filepath.Join(t.TempDir(), "memory"), repo: repo}
	if out, e := f.ul("", nil, "init"); e != nil {
		t.Fatal(out)
	}
	if out, e := f.ul("", nil, "hooks", "claude", "--dir", repo, "--yes"); e != nil {
		t.Fatal(out)
	}
	return f, filepath.Join(repo, ".claude", "settings.json")
}

func TestLocalCheckoutWithoutOriginIsCapturedAfterInitLocal(t *testing.T) {
	f, hooks := hookedFixture(t, bareRepo(t))
	out, e := f.hook(hooks, "SessionStart", `{}`)
	if e == nil || !strings.Contains(out, "projects init-local") {
		t.Fatalf("missing-origin checkout must fail with a fix, got %v: %s", e, out)
	}
	if n := f.sql("SELECT COUNT(*) FROM projects"); n != 0 {
		t.Fatal("a project was invented for an unidentified checkout")
	}
	out, e = f.ul("", nil, "projects", "init-local", "--dir", f.repo, "--name", "Local only")
	if e != nil || !strings.Contains(out, "bound to local:") {
		t.Fatal(out, e)
	}
	again, e := f.ul("", nil, "projects", "init-local", "--dir", f.repo)
	if e != nil || f.sql("SELECT COUNT(*) FROM projects") != 1 {
		t.Fatal("init-local is not idempotent", again, e)
	}
	if out, e = f.hook(hooks, "SessionStart", `{}`); e != nil || !strings.Contains(out, "UNTRUSTED DATA") {
		t.Fatal("start after init-local:", out, e)
	}
	if out, e = f.hook(hooks, "SessionEnd", `{}`); e != nil {
		t.Fatal(out, e)
	}
	if f.sql("SELECT COUNT(*) FROM sessions WHERE agent_id='claude'") != 1 {
		t.Fatal("session not recorded for the local project")
	}
	if f.sql("SELECT COUNT(*) FROM sessions WHERE finished_at IS NOT NULL") != 0 {
		t.Fatal("a lifecycle end event was turned into a finished session")
	}
}

func TestNonStartEventRegistersProjectWithoutClaimingCompletion(t *testing.T) {
	f, hooks := hookedFixture(t, gitFixture(t))
	// The project was never registered (no start event): previously
	// "project not registered; the start event registers it".
	out, e := f.hook(hooks, "SessionEnd", `{}`)
	if e != nil {
		t.Fatal("end event before any start failed:", out)
	}
	if f.sql("SELECT COUNT(*) FROM projects WHERE repository='github.com/example-owner/demo-app'") != 1 {
		t.Fatal("project not registered from its canonical remote")
	}
	if f.sql("SELECT COUNT(*) FROM events WHERE body LIKE '%Client session ended%'") != 1 || f.sql("SELECT COUNT(*) FROM sessions") != 0 {
		t.Fatal("end event must be recorded as metadata only, with no invented session")
	}
	if out, e = f.hook(hooks, "Stop", `{}`); e != nil {
		t.Fatal(out)
	}
	if f.sql("SELECT COUNT(*) FROM projects") != 1 {
		t.Fatal("a second event duplicated the project")
	}
}

func TestRemoteAddedLaterNeedsExplicitLink(t *testing.T) {
	repo := bareRepo(t)
	f, hooks := hookedFixture(t, repo)
	if out, e := f.ul("", nil, "projects", "init-local", "--dir", repo); e != nil {
		t.Fatal(out)
	}
	if out, e := f.hook(hooks, "SessionStart", `{}`); e != nil {
		t.Fatal(out)
	}
	project := firstProject(t, f)
	if out, e := exec.Command("git", "-C", repo, "remote", "add", "origin", "https://github.com/Example-Owner/Later.git").CombinedOutput(); e != nil {
		t.Fatal(string(out))
	}
	out, e := f.hook(hooks, "PreCompact", `{}`)
	if e != nil || !strings.Contains(out, "thoth-lite projects link "+project+" github.com/example-owner/later") {
		t.Fatalf("unlinked new remote must keep the binding and say how to link, got %v: %s", e, out)
	}
	if f.sql("SELECT COUNT(*) FROM projects") != 1 {
		t.Fatal("a new remote silently created a second project")
	}
	if out, e = f.ul("", nil, "projects", "link", project, "https://github.com/Example-Owner/Later.git", "--yes"); e != nil {
		t.Fatal(out)
	}
	if out, e = f.hook(hooks, "PreCompact", `{}`); e != nil || strings.Contains(out, "not linked") {
		t.Fatal("linked remote still reported as unlinked:", out, e)
	}
	if f.sql("SELECT COUNT(*) FROM projects") != 1 || f.sql("SELECT COUNT(*) FROM project_aliases") != 1 {
		t.Fatal("link did not resolve to the existing project")
	}
}

func firstProject(t *testing.T, f fixture) string {
	t.Helper()
	out, e := f.ul("", nil, "projects")
	if e != nil {
		t.Fatal(out)
	}
	return strings.Fields(out)[0]
}
