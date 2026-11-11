package tests

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/lite"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/memory"
	_ "modernc.org/sqlite"
)

func fileHash(t *testing.T, path string) string {
	t.Helper()
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		return "absent"
	}
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		t.Fatal(e)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestImportUltraLiteNeverModifiesSource imports from a memory that another
// process holds open with uncheckpointed WAL content, as a live UltraLite does.
func TestImportUltraLiteNeverModifiesSource(t *testing.T) {
	ctx := context.Background()
	src := lite.Home{Dir: filepath.Join(t.TempDir(), "ultralite")}
	if e := lite.Init(ctx, src); e != nil {
		t.Fatal(e)
	}
	s, release, e := lite.Open(ctx, src)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	p := memory.Principal{ID: "codex", Write: true, Projects: []string{"*"}}
	project := id(must(s.Call(ctx, p, "create_project", memory.Input{Name: "imported", Repository: "https://github.com/thoth-lite-test/imported"})))
	must(s.Call(ctx, p, "record_decision", memory.Input{ProjectID: project, OperationID: "import-decision", Title: "قرار", Body: "Arabic and English survive the import ✓"}))
	if _, e = s.DB.ExecContext(ctx, "PRAGMA wal_autocheckpoint=0"); e != nil {
		t.Fatal(e)
	}
	must(s.Call(ctx, p, "append_event", memory.Input{ProjectID: project, OperationID: "import-event-in-wal", Body: "written after autocheckpoint was disabled"}))

	before := map[string]string{}
	for _, f := range []string{src.DB(), src.DB() + "-wal", filepath.Join(src.Secrets(), "tokens.json")} {
		before[f] = fileHash(t, f)
	}
	if before[src.DB()+"-wal"] == "absent" {
		t.Fatal("fixture has no WAL; the test would not cover a live source")
	}
	dst := lite.Home{Dir: filepath.Join(t.TempDir(), "lite")}
	r, e := lite.ImportUltraLite(ctx, dst, src.DB())
	if e != nil {
		t.Fatal(e)
	}
	for f, h := range before {
		if fileHash(t, f) != h {
			t.Errorf("import modified source file %s", f)
		}
	}
	if r.SchemaAfterImport == "" || len(r.SourceMigrations) == 0 || len(r.SourceSHA256) != 64 {
		t.Fatalf("incomplete report %+v", r)
	}
	if fileHash(t, filepath.Join(src.Secrets(), "codex_token")) == fileHash(t, filepath.Join(dst.Secrets(), "codex_token")) {
		t.Fatal("UltraLite credentials were copied; Lite must generate its own")
	}
	got, closeLite, e := lite.Open(ctx, dst)
	if e != nil {
		t.Fatal(e)
	}
	defer closeLite()
	brief := must(got.Call(ctx, lite.Owner, "search", memory.Input{ProjectID: project, Query: "قرار"}))
	if !strings.Contains(string(mustJSON(t, brief)), "survive the import") {
		t.Fatal("imported decision not searchable", brief)
	}
	events := must(got.Call(ctx, lite.Owner, "search", memory.Input{ProjectID: project, Query: "autocheckpoint"}))
	if !strings.Contains(string(mustJSON(t, events)), "autocheckpoint was disabled") {
		t.Fatal("WAL-only write missing from the snapshot")
	}
	if _, e = lite.ImportUltraLite(ctx, dst, src.DB()); e == nil || !strings.Contains(e.Error(), "already exists") {
		t.Fatal("second import overwrote an existing Lite database:", e)
	}
}

func TestImportUltraLiteRefusesUnknownSchema(t *testing.T) {
	ctx := context.Background()
	src := lite.Home{Dir: filepath.Join(t.TempDir(), "newer")}
	if e := lite.Init(ctx, src); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", src.DB())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("INSERT INTO schema_migrations(version,checksum,applied_at) VALUES('999_from_the_future.sql','x','2099-01-01T00:00:00.000000Z')"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	dst := lite.Home{Dir: filepath.Join(t.TempDir(), "lite")}
	_, e = lite.ImportUltraLite(ctx, dst, src.DB())
	if e == nil || !strings.Contains(e.Error(), "does not know") {
		t.Fatal("unknown schema accepted:", e)
	}
	if _, err := os.Stat(dst.DB()); !os.IsNotExist(err) {
		t.Fatal("refused import left a database behind")
	}
}
