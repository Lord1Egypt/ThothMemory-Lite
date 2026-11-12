package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/lite"
)

// populate writes a realistic memory through real MCP stdio sessions of two
// agents: two projects (remote and local), sessions, tasks with a dependency,
// a superseded decision, an issue, evidence, a handoff, Unicode text and an
// identity alias.
func populate(t *testing.T, home string) string {
	t.Helper()
	codex, cmd := stdioAgent(t, home, "codex")
	defer func() { codex.Close(); gone(t, cmd) }()
	head := strings.Repeat("a", 40)
	p := id(invoke(t, codex, "create_project", map[string]any{"name": "DR main", "repository": "https://github.com/thoth-lite-test/dr-main"}, false))
	local := id(invoke(t, codex, "create_project", map[string]any{"name": "DR local", "repository": "local:5c1d0b7e-1111-4222-8333-944455556666"}, false))
	s := id(invoke(t, codex, "start_session", map[string]any{"project_id": p, "operation_id": "drill-op-s1", "agent": "codex", "git_revision": head}, false))
	t1 := id(invoke(t, codex, "create_task", map[string]any{"project_id": p, "operation_id": "drill-op-t1", "title": "Restore drill"}, false))
	invoke(t, codex, "create_task", map[string]any{"project_id": p, "operation_id": "drill-op-t2", "title": "After drill", "depends_on": []any{t1}}, false)
	d1 := id(invoke(t, codex, "record_decision", map[string]any{"project_id": p, "operation_id": "drill-op-d1", "title": "Backups are local files", "body": "القرار: النسخ الاحتياطية ملفات محلية فقط", "git_revision": head}, false))
	invoke(t, codex, "record_decision", map[string]any{"project_id": p, "operation_id": "drill-op-d2", "title": "Backups are verified local files", "body": "Supersedes: also verify SHA-256 ✓", "supersedes": d1}, false)
	invoke(t, codex, "record_issue", map[string]any{"project_id": p, "operation_id": "drill-op-i1", "title": "Copying a live WAL file is unsafe", "body": "Use VACUUM INTO snapshots, never cp memory.db"}, false)
	invoke(t, codex, "record_test_result", map[string]any{"project_id": p, "operation_id": "drill-op-e1", "task_id": t1, "locator": "go test ./...", "git_revision": head, "outcome": "pass", "body": "ok"}, false)
	invoke(t, codex, "finish_session", map[string]any{"project_id": p, "operation_id": "drill-op-f1", "session_id": s, "expected_revision": 1, "git_revision": head, "body": "Handoff: drill prepared; next: restore on a clean machine."}, false)
	invoke(t, codex, "record_note", map[string]any{"project_id": local, "operation_id": "drill-op-n1", "title": "Local note", "body": "café 日本語 emoji 🧪"}, false)
	claude, cmd2 := stdioAgent(t, home, "claude")
	defer func() { claude.Close(); gone(t, cmd2) }()
	invoke(t, claude, "start_session", map[string]any{"project_id": p, "operation_id": "drill-op-s2", "agent": "claude", "git_revision": head}, false)
	if out, e := run(t, home, nil, "projects", "link", local, "https://github.com/thoth-lite-test/dr-local-later", "--yes"); e != nil {
		t.Fatal(out)
	}
	return p
}

func dbDigests(t *testing.T, path string, tables []string) map[string]string {
	t.Helper()
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	out := map[string]string{}
	for _, table := range tables {
		d, n := tableDigest(t, db, table)
		out[table] = fmt.Sprintf("%s/%d", d, n)
	}
	return out
}

var allTables = append(append([]string{}, ultraliteTables...), "project_aliases", "schema_migrations")

func TestDisasterRecoveryOnACleanMachine(t *testing.T) {
	machineA := filepath.Join(t.TempDir(), "machine-a", "memory")
	if out, e := run(t, machineA, nil, "init"); e != nil {
		t.Fatal(out)
	}
	project := populate(t, machineA)
	backupDir := filepath.Join(t.TempDir(), "usb-drive")
	backup := filepath.Join(backupDir, "memory.db")
	if out, e := run(t, machineA, nil, "backup", backup); e != nil || !strings.Contains(out, "Backup verified") {
		t.Fatal(out, e)
	}
	want := dbDigests(t, backup, allTables)
	// Total loss of machine A.
	if e := os.RemoveAll(filepath.Dir(machineA)); e != nil {
		t.Fatal(e)
	}
	machineB := filepath.Join(t.TempDir(), "machine-b", "memory")
	if out, e := run(t, machineB, nil, "restore", backup); e == nil || !strings.Contains(out, "--confirm") {
		t.Fatal("restore without --confirm must only verify:", out)
	}
	if _, e := os.Stat(filepath.Join(machineB, "memory.db")); !os.IsNotExist(e) {
		t.Fatal("unconfirmed restore wrote a database")
	}
	if out, e := run(t, machineB, nil, "restore", backup, "--confirm"); e != nil {
		t.Fatal(out)
	}
	if out, e := run(t, machineB, nil, "init"); e != nil {
		t.Fatal("init after restore (new credentials):", out)
	}
	got := dbDigests(t, filepath.Join(machineB, "memory.db"), allTables)
	for _, table := range allTables {
		if table == "principals" {
			continue // init registers the new machine's principals
		}
		if want[table] != got[table] {
			t.Errorf("table %s differs after recovery", table)
		}
	}
	if out, e := run(t, machineB, nil, "doctor"); e != nil {
		t.Fatal("doctor after recovery:", out)
	}
	// A reconnected agent with a brand-new credential continues the work.
	agent, cmd := stdioAgent(t, machineB, "claude")
	brief := mustJSON(t, invoke(t, agent, "get_project_context", map[string]any{"project_id": project}, false))
	hit := mustJSON(t, invoke(t, agent, "search", map[string]any{"project_id": project, "query": "القرار"}, false))
	resolved := invoke(t, agent, "resolve_project", map[string]any{"repository": "https://github.com/thoth-lite-test/dr-local-later"}, false).(map[string]any)
	agent.Close()
	gone(t, cmd)
	for _, wantText := range []string{"Backups are verified local files", "next: restore on a clean machine", "VACUUM INTO"} {
		if !strings.Contains(string(brief), wantText) {
			t.Errorf("recovered briefing lacks %q", wantText)
		}
	}
	if strings.Contains(string(brief), "\"title\":\"Backups are local files\"") {
		t.Error("superseded decision resurfaced in the briefing")
	}
	if !strings.Contains(string(hit), "النسخ") || resolved["found"] != true {
		t.Error("search or alias lost in recovery")
	}
}

func TestRestoreRefusesBadBackupsWithoutTouchingMemory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "memory")
	if out, e := run(t, home, nil, "init"); e != nil {
		t.Fatal(out)
	}
	populate(t, home)
	dir := t.TempDir()
	good := filepath.Join(dir, "good.db")
	if out, e := run(t, home, nil, "backup", good); e != nil {
		t.Fatal(out)
	}
	live := filepath.Join(home, "memory.db")
	before := fileHash(t, live)
	copyTo := func(name string, mutate func([]byte) []byte, sidecar bool) string {
		b, _ := os.ReadFile(good)
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, mutate(append([]byte{}, b...)), 0600)
		if sidecar {
			s, _ := os.ReadFile(good + ".sha256")
			_ = os.WriteFile(p+".sha256", s, 0600)
		}
		return p
	}
	same := func(b []byte) []byte { return b }
	corrupt := copyTo("corrupt.db", func(b []byte) []byte { b[len(b)/2] ^= 0xff; return b }, true)
	noSidecar := copyTo("nosidecar.db", same, false)
	// A backup from a newer thoth-lite: valid file and sidecar, unknown migration.
	newer := filepath.Join(dir, "newer.db")
	_ = os.WriteFile(newer, mustRead(t, good), 0600)
	db, _ := sql.Open("sqlite", newer)
	if _, e := db.Exec("INSERT INTO schema_migrations(version,checksum,applied_at) VALUES('999_future.sql','x','2099-01-01T00:00:00.000000Z')"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	sum, _ := sqlite3Sum(newer)
	_ = os.WriteFile(newer+".sha256", []byte(sum+"\n"), 0600)
	for name, tc := range map[string]struct{ file, want string }{
		"corrupt":    {corrupt, "SHA-256"},
		"no sidecar": {noSidecar, "sidecar"},
		"newer":      {newer, "does not know"},
		"missing":    {filepath.Join(dir, "absent.db"), "sidecar"},
	} {
		out, e := run(t, home, nil, "restore", tc.file, "--confirm")
		if e == nil || !strings.Contains(out, tc.want) {
			t.Errorf("%s: want refusal mentioning %q, got %v: %s", name, tc.want, e, out)
		}
		if fileHash(t, live) != before {
			t.Fatalf("%s: live memory changed by a refused restore", name)
		}
	}
	// Memory in use by an agent: refused.
	agent, cmd := stdioAgent(t, home, "codex")
	out, e := run(t, home, nil, "restore", good, "--confirm")
	agent.Close()
	gone(t, cmd)
	if e == nil || !strings.Contains(out, "in use") {
		t.Fatal("restore while memory is in use:", out)
	}
	// A crashed earlier restore left a staging file behind; the next restore copes.
	_ = os.WriteFile(live+".restoring", []byte("garbage from an interrupted restore"), 0600)
	if out, e = run(t, home, nil, "restore", good, "--confirm"); e != nil {
		t.Fatal("restore after an interrupted one:", out)
	}
	if _, e := os.Stat(live + ".restoring"); !os.IsNotExist(e) {
		t.Fatal("staging file left behind")
	}
	kept, _ := filepath.Glob(live + ".before-restore-*")
	if len(kept) != 1 {
		t.Fatal("previous memory not preserved", kept)
	}
	if _, release, e := lite.Open(context.Background(), lite.Home{Dir: home}); e != nil {
		t.Fatal("restored memory does not open:", e)
	} else {
		release()
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func sqlite3Sum(p string) (string, error) { return fileSHA(p) }
