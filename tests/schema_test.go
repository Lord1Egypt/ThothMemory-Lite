package tests

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/lite"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/memory"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/sqlite"
)

const ultraliteFixture = "testdata/ultralite-0.1.0.db"

var ultraliteTables = []string{"principals", "projects", "agents", "sessions", "tasks", "task_dependencies", "evidence",
	"attestations", "events", "records", "operations", "imports"}

// copyFixture copies the UltraLite 0.1.0 database into a temporary directory.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "memory.db")
	in, e := os.Open(ultraliteFixture)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	out, e := os.Create(dst)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = io.Copy(out, in); e != nil {
		t.Fatal(e)
	}
	if e = out.Close(); e != nil {
		t.Fatal(e)
	}
	return dst
}

// tableDigest hashes every row of table in rowid order, so any changed,
// lost or added value is detected.
func tableDigest(t *testing.T, db *sql.DB, table string) (string, int) {
	t.Helper()
	rows, e := db.Query(fmt.Sprintf(`SELECT * FROM "%s" ORDER BY rowid`, table))
	if e != nil {
		t.Fatal(table, e)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	h := sha256.New()
	n := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if e = rows.Scan(ptrs...); e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(h, "%q\n", vals)
		n++
	}
	return hex.EncodeToString(h.Sum(nil)), n
}

func digests(t *testing.T, path string) map[string]string {
	t.Helper()
	db, e := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	out := map[string]string{}
	for _, table := range ultraliteTables {
		d, n := tableDigest(t, db, table)
		out[table] = fmt.Sprintf("%s/%d", d, n)
	}
	return out
}

func migrations(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, e := db.Query("SELECT version FROM schema_migrations ORDER BY version")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		out = append(out, v)
	}
	return out
}

func TestUpgradeFromUltraLite010PreservesEveryRow(t *testing.T) {
	ctx := context.Background()
	fixtureSum := fileHash(t, ultraliteFixture)
	path := copyFixture(t)
	before := digests(t, path)
	if before["projects"][len(before["projects"])-2:] != "/2" {
		t.Fatalf("unexpected ultraliteFixture contents %v", before)
	}
	s, e := sqlite.Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal("repeat migration:", e)
	}
	got := migrations(t, s.DB)
	if strings.Join(got, ",") != "001_initial.sql,002_project_aliases.sql,003_client_integrations.sql" {
		t.Fatal("applied migrations", got)
	}
	if e = sqlite.Check(ctx, s.DB); e != nil {
		t.Fatal(e)
	}
	owner := memory.Principal{ID: "local-owner", Owner: true, Write: true, Projects: []string{"*"}}
	if e = s.Register(ctx, owner); e != nil {
		t.Fatal(e)
	}
	projects := must(s.Call(ctx, owner, "list_projects", memory.Input{})).([]memory.Row)
	if len(projects) != 2 {
		t.Fatal("projects after upgrade", len(projects))
	}
	for _, p := range projects {
		brief := mustJSON(t, must(s.Call(ctx, owner, "get_project_context", memory.Input{ProjectID: p["id"].(string)})))
		for _, want := range []string{"with checksums", "ERROR 23505", "incomplete"} {
			if !strings.Contains(string(brief), want) {
				t.Errorf("upgraded briefing lost %q", want)
			}
		}
		hit := mustJSON(t, must(s.Call(ctx, owner, "search", memory.Input{ProjectID: p["id"].(string), Query: "القرار"})))
		if !strings.Contains(string(hit), "transactional migrations") {
			t.Error("Arabic search lost after upgrade")
		}
	}
	s.Close()
	after := digests(t, path)
	for _, table := range ultraliteTables {
		if table == "principals" {
			continue // the test registered local-owner above
		}
		if before[table] != after[table] {
			t.Errorf("table %s changed during upgrade: %s -> %s", table, before[table], after[table])
		}
	}
	if fileHash(t, ultraliteFixture) != fixtureSum {
		t.Fatal("the ultraliteFixture itself was modified")
	}
}

func TestInterruptedMigrationLeavesNoPartialSchema(t *testing.T) {
	ctx := context.Background()
	path := copyFixture(t)
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	// A conflicting object makes migration 003 fail after 002 already ran in
	// the same transaction.
	if _, e = db.Exec("CREATE TABLE client_integrations(conflict INTEGER)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	before := digests(t, path)
	s, e := sqlite.Open(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e == nil || !strings.Contains(e.Error(), "003_client_integrations.sql") {
		t.Fatal("expected migration 003 to fail:", e)
	}
	if got := migrations(t, s.DB); strings.Join(got, ",") != "001_initial.sql" {
		t.Fatal("partial migration recorded:", got)
	}
	var n int
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='project_aliases'").Scan(&n); e != nil || n != 0 {
		t.Fatal("migration 002 was not rolled back with 003", n, e)
	}
	if e = sqlite.Check(ctx, s.DB); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("DROP TABLE client_integrations"); e != nil {
		t.Fatal(e)
	}
	if e = s.Migrate(ctx); e != nil {
		t.Fatal("migration after removing the conflict:", e)
	}
	s.Close()
	after := digests(t, path)
	for _, table := range ultraliteTables {
		if before[table] != after[table] {
			t.Errorf("table %s changed", table)
		}
	}
}

func TestMigrationChecksumAndDowngradeGuards(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, sql, want string
	}{
		{"altered migration", "UPDATE schema_migrations SET checksum='0000' WHERE version='002_project_aliases.sql'", "checksum mismatch"},
		{"newer schema", "INSERT INTO schema_migrations(version,checksum,applied_at) VALUES('999_future.sql','x','2099-01-01T00:00:00.000000Z')", "newer than this thoth-lite"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := copyFixture(t)
			s, e := sqlite.Open(ctx, path)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Migrate(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e = s.DB.Exec(tc.sql); e != nil {
				t.Fatal(e)
			}
			s.Close()
			s, e = sqlite.Open(ctx, path)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			if e = s.Migrate(ctx); e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, e)
			}
		})
	}
}

func TestProjectIdentityAliases(t *testing.T) {
	ctx := context.Background()
	s, agent := setup(t)
	owner := memory.Principal{ID: "local-owner", Owner: true, Write: true, Projects: []string{"*"}}
	if e := s.Register(ctx, owner); e != nil {
		t.Fatal(e)
	}
	local := "local:7e0d3c1a-1111-4222-8333-444455556666"
	remote := "https://github.com/thoth-lite-test/Later-Remote.git"
	canonical, _ := memory.CanonicalRepository(remote)
	project := id(must(s.Call(ctx, agent, "create_project", memory.Input{Name: "local first", Repository: local})))
	other := id(must(s.Call(ctx, agent, "create_project", memory.Input{Name: "other", Repository: "https://github.com/thoth-lite-test/other"})))

	notFound := must(s.Call(ctx, agent, "resolve_project", memory.Input{Repository: remote})).(memory.Row)
	if notFound["found"] != false || notFound["identity"] != canonical {
		t.Fatal("unlinked remote resolved", notFound)
	}
	if e := s.LinkIdentity(ctx, project, canonical, agent); e == nil {
		t.Fatal("non-owner linked an identity")
	}
	if e := s.LinkIdentity(ctx, project, canonical, owner); e != nil {
		t.Fatal(e)
	}
	found := must(s.Call(ctx, agent, "resolve_project", memory.Input{Repository: remote})).(memory.Row)
	if found["id"] != project || found["matched_by"] != "alias" || found["found"] != true {
		t.Fatal("alias did not resolve", found)
	}
	// Registering the remote again must return the linked project, never a duplicate.
	again := must(s.Call(ctx, agent, "create_project", memory.Input{Name: "duplicate?", Repository: remote}))
	if id(again) != project {
		t.Fatal("create_project created a duplicate for a linked identity")
	}
	list := must(s.Call(ctx, agent, "list_projects", memory.Input{})).([]memory.Row)
	if len(list) != 2 {
		t.Fatal("project count changed", len(list))
	}
	if e := s.LinkIdentity(ctx, project, "github.com/thoth-lite-test/other", owner); e == nil || !strings.Contains(e.Error(), "never merged") {
		t.Fatal("linked another project's repository:", e)
	}
	if e := s.LinkIdentity(ctx, other, canonical, owner); e == nil || !strings.Contains(e.Error(), "never merged") {
		t.Fatal("one identity linked to two projects:", e)
	}
	if _, e := s.DB.Exec("INSERT INTO projects(id,name,repository,created_at) VALUES(?,?,?,?)", memory.NewID(), "bypass", canonical, "2026-01-01T00:00:00.000000Z"); e == nil {
		t.Fatal("database accepted a project whose repository is an alias")
	}
	if _, e := s.DB.Exec("UPDATE project_aliases SET project_id=? WHERE identity=?", other, canonical); e == nil {
		t.Fatal("alias edited in place")
	}
	scoped := memory.Principal{ID: "scoped", Write: true, Projects: []string{other}}
	if e := s.Register(ctx, scoped); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Call(ctx, scoped, "resolve_project", memory.Input{Repository: remote}); e == nil {
		t.Fatal("scoped credential resolved a project outside its scope")
	}
	if e := s.UnlinkIdentity(ctx, project, canonical, owner); e != nil {
		t.Fatal(e)
	}
	if r := must(s.Call(ctx, agent, "resolve_project", memory.Input{Repository: remote})).(memory.Row); r["found"] != false {
		t.Fatal("unlinked alias still resolves", r)
	}
	events := mustJSON(t, must(s.Call(ctx, agent, "get_recent_activity", memory.Input{ProjectID: project})))
	if !strings.Contains(string(events), "identity_linked") || !strings.Contains(string(events), "identity_unlinked") {
		t.Fatal("identity changes missing from history", string(events))
	}
}

func TestClientHandshakeIsRecorded(t *testing.T) {
	home := filepath.Join(t.TempDir(), "memory")
	if out, e := run(t, home, nil, "init"); e != nil {
		t.Fatal(out, e)
	}
	cs, cmd := stdioAgent(t, home, "codex")
	invoke(t, cs, "list_projects", map[string]any{}, false)
	cs.Close()
	gone(t, cmd)
	ctx := context.Background()
	s, release, e := lite.Open(ctx, lite.Home{Dir: home})
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	rows, e := s.Integrations(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 || rows[0]["principal_id"] != "codex" || rows[0]["client_name"] != "codex" || rows[0]["client_version"] != "test" || rows[0]["transport"] != "stdio" {
		t.Fatal("handshake not recorded", rows)
	}
	if rows[0]["last_operation_at"] == nil {
		t.Fatal("successful tool call not recorded", rows)
	}
	if _, e := time.Parse(time.RFC3339, fmt.Sprint(rows[0]["last_handshake_at"])); e != nil {
		t.Fatal("handshake time", rows[0]["last_handshake_at"], e)
	}
}
