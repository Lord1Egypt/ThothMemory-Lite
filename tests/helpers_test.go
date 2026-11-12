package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/contract"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/memory"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setup opens a fresh, migrated SQLite memory in a temporary directory and
// registers one write principal for every project.
func setup(t *testing.T) (*sqlite.Store, memory.Principal) {
	t.Helper()
	ctx := context.Background()
	s, e := sqlite.Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	p := memory.Principal{ID: "audit-agent", Token: strings.Repeat("z", 64), Write: true, Projects: []string{"*"}}
	if e = s.Register(ctx, p); e != nil {
		t.Fatal(e)
	}
	return s, p
}

func invoke(t *testing.T, c *mcp.ClientSession, name string, a map[string]any, fail bool) any {
	t.Helper()
	return contract.Invoke(t, c, name, a, fail)
}

func id(v any) string { return contract.ID(v) }
func must(v any, e error) any {
	if e != nil {
		panic(e)
	}
	return v
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func fileSHA(p string) (string, error) { return sqlite.FileSum(p) }
