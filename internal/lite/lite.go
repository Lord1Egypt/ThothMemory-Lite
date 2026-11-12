// Package lite is ThothMemory Lite: one native executable and one SQLite file
// in a per-user data directory. It is headless by design: clients
// launch it per session over MCP stdio and the owner uses the CLI. It never
// opens a network listener.
package lite

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/capture"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/cli"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/mcptools"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/memory"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Home is the data directory: memory.db, .secrets/ and backups/.
type Home struct{ Dir string }

// DefaultDir is the per-user application data directory: XDG data home on
// Linux, Application Support on macOS, %LOCALAPPDATA% on Windows.
//
// It is deliberately separate from ThothMemory UltraLite's directory and
// ignores UltraLite's THOTH_HOME: opening a database applies migrations, so
// sharing a default would silently upgrade a live UltraLite installation.
// Use `thoth-lite import-ultralite` to copy UltraLite memory explicitly.
func DefaultDir() (string, error) {
	if v := os.Getenv("THOTH_LITE_HOME"); v != "" {
		return filepath.Abs(v)
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "ThothMemory-Lite"), nil
		}
		return filepath.Join(home, "AppData", "Local", "ThothMemory-Lite"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ThothMemory-Lite"), nil
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "thoth-lite"), nil
	}
	return filepath.Join(home, ".local", "share", "thoth-lite"), nil
}

func (h Home) DB() string      { return filepath.Join(h.Dir, "memory.db") }
func (h Home) Secrets() string { return filepath.Join(h.Dir, ".secrets") }
func (h Home) Backups() string { return filepath.Join(h.Dir, "backups") }
func (h Home) lockPath() string {
	return filepath.Join(h.Dir, "memory.db.instance-lock")
}

// Init creates the data directory, credentials and database. It is safe to
// repeat: existing credentials, policy and memory are kept.
func Init(ctx context.Context, h Home) error {
	if e := os.MkdirAll(h.Dir, 0700); e != nil {
		return e
	}
	if e := cli.PrivateDir(h.Dir); e != nil {
		return e
	}
	if e := cli.InitSecrets(h.Secrets()); e != nil {
		return e
	}
	s, e := h.open(ctx, true)
	if e != nil {
		return e
	}
	defer s.Close()
	ps, e := mcptools.LoadCredentials(filepath.Join(h.Secrets(), "tokens.json"))
	if e != nil {
		return e
	}
	for _, p := range ps {
		if e = s.Register(ctx, p); e != nil {
			return e
		}
	}
	return nil
}

// open opens the database and applies migrations, which is how an upgraded
// executable upgrades an existing file. Only init may create a new file, so
// a mistyped data directory never silently starts an empty memory.
func (h Home) open(ctx context.Context, create bool) (*sqlite.Store, error) {
	if _, e := os.Stat(h.DB()); e != nil && !create {
		return nil, fmt.Errorf("no ThothMemory Lite database at %s; run: thoth-lite init", h.DB())
	}
	s, e := sqlite.Open(ctx, h.DB())
	if e != nil {
		return nil, e
	}
	if e = s.Migrate(ctx); e != nil {
		s.Close()
		return nil, e
	}
	if runtime.GOOS != "windows" {
		// SQLite creates files with the process umask; memory is owner-only.
		for _, f := range []string{h.DB(), h.DB() + "-wal", h.DB() + "-shm"} {
			if _, err := os.Stat(f); err == nil {
				if err = cli.PrivateFile(f); err != nil {
					s.Close()
					return nil, err
				}
			}
		}
	}
	return s, nil
}

// Open opens an initialized database and holds the shared instance lock until
// the returned release is called, so restore can tell the memory is in use.
func Open(ctx context.Context, h Home) (*sqlite.Store, func(), error) {
	f, e := os.OpenFile(h.lockPath(), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, nil, e
	}
	if e = lock(f, false); e != nil {
		f.Close()
		return nil, nil, e
	}
	s, e := h.open(ctx, false)
	if e != nil {
		f.Close()
		return nil, nil, e
	}
	return s, func() { s.Close(); f.Close() }, nil
}

// Principal resolves the credential in tokenFile against the token policy,
// with a constant-time comparison.
func (h Home) Principal(tokenFile string) (memory.Principal, error) {
	b, e := os.ReadFile(tokenFile)
	if e != nil {
		return memory.Principal{}, errors.New("token file unreadable")
	}
	token := strings.TrimSpace(string(b))
	ps, e := mcptools.LoadCredentials(filepath.Join(h.Secrets(), "tokens.json"))
	if e != nil {
		return memory.Principal{}, e
	}
	found := -1
	for i, p := range ps {
		if subtle.ConstantTimeCompare([]byte(token), []byte(p.Token)) == 1 {
			found = i
		}
	}
	if found < 0 {
		return memory.Principal{}, errors.New("unauthorized: token is not in the current policy (revoked or wrong data directory)")
	}
	return ps[found], nil
}

// Stdio serves one client over stdin/stdout and returns when it disconnects.
// The client's own scoped token selects the principal: stdio does not bypass
// project allowlists or read-only credentials.
func Stdio(ctx context.Context, h Home, tokenFile string) error {
	p, e := h.Principal(tokenFile)
	if e != nil {
		return e
	}
	s, release, e := Open(ctx, h)
	if e != nil {
		return e
	}
	defer release()
	if e = s.Register(ctx, p); e != nil {
		return e
	}
	var client string
	var lastOp atomic.Int64
	obs := mcptools.Observer{
		Initialized: func(ctx context.Context, name, version string) {
			client = name
			// Diagnostics only: a failed note must never break the session.
			if err := s.RecordHandshake(ctx, p.ID, name, version, "stdio"); err != nil {
				fmt.Fprintln(os.Stderr, "thoth-lite: could not record client handshake:", memory.SafeError(err))
			}
		},
		Succeeded: func() { lastOp.Store(time.Now().UnixNano()) },
	}
	e = mcptools.ServerWith(s, p, obs).Run(ctx, &mcp.StdioTransport{})
	if n := lastOp.Load(); n != 0 && client != "" {
		done, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.RecordOperation(done, p.ID, client, time.Unix(0, n)); err != nil {
			fmt.Fprintln(os.Stderr, "thoth-lite: could not record last operation:", memory.SafeError(err))
		}
	}
	return e
}

// Backup writes a verified online snapshot and a SHA-256 sidecar.
func Backup(ctx context.Context, h Home, target string) (string, string, error) {
	s, release, e := Open(ctx, h)
	if e != nil {
		return "", "", e
	}
	defer release()
	if target == "" {
		if e = os.MkdirAll(h.Backups(), 0700); e != nil {
			return "", "", e
		}
		if e = cli.PrivateDir(h.Backups()); e != nil {
			return "", "", e
		}
		target = filepath.Join(h.Backups(), "memory-"+time.Now().UTC().Format("20060102T150405Z")+".db")
	}
	sum, e := s.Backup(ctx, target)
	if e != nil {
		return "", "", e
	}
	return target, sum, os.WriteFile(target+".sha256", []byte(sum+"\n"), 0600)
}

// Restore replaces the database with a verified backup. Nothing in the data
// directory changes until the backup has passed its SHA-256 sidecar,
// integrity, foreign-key and schema-compatibility checks AND a staged copy has
// been migrated to this build's schema and checked again. It refuses while any
// thoth-lite process has the memory open, keeps the replaced file beside it,
// rolls back if the final swap fails, and requires explicit confirmation. On a
// new machine the data directory may not exist yet; run init afterwards to
// create credentials.
func Restore(ctx context.Context, h Home, backup string, confirmed bool) (string, error) {
	side, e := os.ReadFile(backup + ".sha256")
	if e != nil {
		return "", errors.New("backup has no .sha256 sidecar; refusing an unverifiable restore")
	}
	sum, e := sqlite.FileSum(backup)
	if e != nil || sum != strings.TrimSpace(string(side)) {
		return "", errors.New("backup SHA-256 integrity check failed; nothing changed")
	}
	if e = sqlite.Verify(ctx, backup); e != nil {
		return "", fmt.Errorf("backup is not a healthy ThothMemory database: %w; nothing changed", e)
	}
	if _, e = sqlite.Compatibility(ctx, backup); e != nil {
		return "", fmt.Errorf("backup cannot be restored by this thoth-lite: %w; nothing changed", e)
	}
	if !confirmed {
		return "", errors.New("backup verified. Restore replaces the current memory (it is kept as a .before-restore copy); rerun with --confirm")
	}
	if e = os.MkdirAll(h.Dir, 0700); e != nil {
		return "", e
	}
	if e = cli.PrivateDir(h.Dir); e != nil {
		return "", e
	}
	f, e := os.OpenFile(h.lockPath(), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return "", e
	}
	defer f.Close()
	if lock(f, true) != nil {
		return "", errors.New("memory is in use by another thoth-lite process (an agent session or CLI command); close your agents and retry")
	}
	staging := h.DB() + ".restoring"
	removeDB := func(path string) {
		for _, x := range []string{"", "-wal", "-shm"} {
			os.Remove(path + x)
		}
	}
	removeDB(staging) // leftovers of an interrupted restore
	swapped := false
	defer func() {
		if !swapped {
			removeDB(staging)
		}
	}()
	if e = copyFile(backup, staging); e != nil {
		return "", e
	}
	// Bring the staged copy to this build's schema and check it again before
	// anything in the live slot changes.
	st, e := sqlite.Open(ctx, staging)
	if e != nil {
		return "", e
	}
	if e = st.Migrate(ctx); e == nil {
		if e = sqlite.Check(ctx, st.DB); e == nil {
			_, e = st.DB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		}
	}
	st.Close()
	if e != nil {
		return "", fmt.Errorf("staged restore failed its checks; nothing changed: %w", e)
	}
	os.Remove(staging + "-wal")
	os.Remove(staging + "-shm")
	if e = cli.PrivateFile(staging); e != nil {
		return "", e
	}
	kept := ""
	if _, e = os.Stat(h.DB()); e == nil {
		// Fold the WAL into the main file so the kept copy is self-contained.
		s, e := sqlite.Open(ctx, h.DB())
		if e != nil {
			return "", e
		}
		_, e = s.DB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		s.Close()
		if e != nil {
			return "", e
		}
		kept = h.DB() + ".before-restore-" + time.Now().UTC().Format("20060102T150405.000000000Z")
		if e = os.Rename(h.DB(), kept); e != nil {
			return "", e
		}
		os.Remove(h.DB() + "-wal")
		os.Remove(h.DB() + "-shm")
	}
	if e = os.Rename(staging, h.DB()); e != nil {
		if kept != "" {
			_ = os.Rename(kept, h.DB())
		}
		return "", fmt.Errorf("could not activate the restored memory; previous memory left in place: %w", e)
	}
	swapped = true
	if s, e := h.open(ctx, false); e != nil {
		failed := h.DB() + ".failed-restore-" + time.Now().UTC().Format("20060102T150405Z")
		_ = os.Rename(h.DB(), failed)
		if kept != "" {
			_ = os.Rename(kept, h.DB())
		}
		return "", fmt.Errorf("restored memory did not open, previous memory put back (failed copy at %s): %w", failed, e)
	} else {
		s.Close()
	}
	return kept, nil
}

func copyFile(from, to string) error {
	in, e := os.Open(from)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = io.Copy(out, in); e != nil {
		out.Close()
		os.Remove(to)
		return e
	}
	if e = out.Sync(); e != nil {
		out.Close()
		return e
	}
	return out.Close()
}

// Status reports storage and policy facts for doctor and status.
func Status(ctx context.Context, h Home) (map[string]any, error) {
	s, release, e := Open(ctx, h)
	if e != nil {
		return nil, e
	}
	defer release()
	out := map[string]any{"product": "ThothMemory Lite", "version": memory.LiteVersion, "data_dir": h.Dir}
	st, e := s.Status(ctx)
	if e != nil {
		return nil, e
	}
	for k, v := range st {
		out[k] = v
	}
	b, _ := os.ReadFile(filepath.Join(h.Secrets(), "tokens.json"))
	var ps []memory.Principal
	_ = json.Unmarshal(b, &ps)
	ids := []string{}
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	out["principals"] = ids
	return out, nil
}

// Owner is the local owner identity used by CLI inspection and attestation;
// it exists only in-process and is never accepted from a client.
var Owner = memory.Principal{ID: "local-owner", Owner: true, Write: true, Projects: []string{"*"}}

// Call runs one tool by its MCP name with JSON arguments, applying exactly the
// schema validation and authorization an MCP client gets. It is the
// transport-free boundary for CLI inspection and in-process embedding.
func Call(ctx context.Context, b memory.Backend, p memory.Principal, tool string, args []byte) (any, error) {
	for _, c := range mcptools.Contracts {
		if c.Name == tool {
			if memory.IsWrite(tool) && !p.Write {
				return nil, memory.ErrForbidden
			}
			in, e := mcptools.Decode(c, args)
			if e != nil {
				return nil, e
			}
			return memory.Call(ctx, b, p, tool, in)
		}
	}
	return nil, errors.New("unknown tool " + tool)
}

// CaptureDir holds capture settings and the retry queue.
func (h Home) CaptureDir() capture.Dir { return capture.Dir(filepath.Join(h.Dir, "capture")) }

// inproc lets the capture engine reach memory without any transport.
type inproc struct {
	b          memory.Backend
	p          memory.Principal
	registered bool
}

func (i *inproc) Call(ctx context.Context, tool string, args map[string]any) (any, error) {
	// Registering the principal is a write; do it only when one is needed, so
	// reads (and context at session start) work while another process holds
	// the write lock.
	if memory.IsWrite(tool) && !i.registered {
		if e := i.b.Register(ctx, i.p); e != nil {
			return nil, errors.New(memory.SafeError(e))
		}
		i.registered = true
	}
	raw, _ := json.Marshal(args)
	v, e := Call(ctx, i.b, i.p, tool, raw)
	if e != nil {
		return nil, errors.New(memory.SafeError(e))
	}
	// Normalize to plain JSON values, as an MCP client would receive them.
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	var out any
	return out, json.Unmarshal(b, &out)
}

// Caller opens the memory as the credential in tokenFile.
func (h Home) Caller(ctx context.Context, tokenFile string) (capture.Caller, func(), error) {
	p, e := h.Principal(tokenFile)
	if e != nil {
		return nil, nil, e
	}
	s, release, e := Open(ctx, h)
	if e != nil {
		return nil, nil, e
	}
	return &inproc{b: s, p: p}, release, nil
}

// Lifecycle handles one client lifecycle event for UltraLite: it runs in the
// hook's own process, opens the SQLite file, writes, and exits.
func Lifecycle(ctx context.Context, h Home, agent, event, tokenFile string) error {
	dir, _ := os.Getwd()
	ev := capture.Event{Agent: agent, Name: event, Dir: dir, Stdin: capture.ReadStdin(os.Stdin)}
	d := h.CaptureDir()
	if mode, e := d.Mode(); e == nil && mode == capture.ModeOff {
		return nil
	}
	c, release, e := h.Caller(ctx, tokenFile)
	if e != nil {
		fmt.Fprintln(os.Stderr, "ThothMemory unavailable ("+memory.SafeError(e)+"); record unsynced progress honestly and retry later.")
		return nil
	}
	defer release()
	return capture.Run(ctx, c, d, ev, os.Stdout)
}

// UltraLiteDefaultDir is where ThothMemory UltraLite keeps its memory by
// default, honouring UltraLite's own THOTH_HOME override.
func UltraLiteDefaultDir() (string, error) {
	if v := os.Getenv("THOTH_HOME"); v != "" {
		return filepath.Abs(v)
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	switch runtime.GOOS {
	case "windows":
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "ThothMemory"), nil
		}
		return filepath.Join(home, "AppData", "Local", "ThothMemory"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "ThothMemory"), nil
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "thothmemory"), nil
	}
	return filepath.Join(home, ".local", "share", "thothmemory"), nil
}

// ImportReport describes a completed UltraLite import.
type ImportReport struct {
	Source            string   `json:"source"`
	SourceSHA256      string   `json:"source_snapshot_sha256"`
	SourceMigrations  []string `json:"source_migrations"`
	Database          string   `json:"database"`
	SchemaAfterImport string   `json:"schema_after_import"`
}

// ImportUltraLite copies an UltraLite database into a new Lite home. The
// source is only ever read (a read-only VACUUM INTO snapshot); the copy is
// integrity-checked and schema-checked, then migrated. Lite credentials are
// newly generated: UltraLite tokens are never copied. It refuses to run when
// the Lite home already has a database.
func ImportUltraLite(ctx context.Context, h Home, sourceDB string) (ImportReport, error) {
	r := ImportReport{Source: sourceDB, Database: h.DB()}
	if _, e := os.Stat(h.DB()); !os.IsNotExist(e) {
		return r, fmt.Errorf("a ThothMemory Lite database already exists at %s; import only into a new data directory", h.DB())
	}
	if e := os.MkdirAll(h.Dir, 0700); e != nil {
		return r, e
	}
	if e := cli.PrivateDir(h.Dir); e != nil {
		return r, e
	}
	staging := h.DB() + ".importing"
	os.Remove(staging)
	defer os.Remove(staging)
	if e := sqlite.Snapshot(ctx, sourceDB, staging); e != nil {
		return r, fmt.Errorf("snapshot of UltraLite memory failed (nothing changed): %w", e)
	}
	if e := sqlite.Verify(ctx, staging); e != nil {
		return r, fmt.Errorf("UltraLite snapshot is not healthy (nothing changed): %w", e)
	}
	applied, e := sqlite.Compatibility(ctx, staging)
	if e != nil {
		return r, e
	}
	r.SourceMigrations = applied
	if r.SourceSHA256, e = sqlite.FileSum(staging); e != nil {
		return r, e
	}
	if e = os.Rename(staging, h.DB()); e != nil {
		return r, e
	}
	if e = Init(ctx, h); e != nil {
		return r, fmt.Errorf("imported copy did not open; the source was not modified: %w", e)
	}
	s, release, e := Open(ctx, h)
	if e != nil {
		return r, e
	}
	defer release()
	r.SchemaAfterImport, e = s.SchemaVersion(ctx)
	return r, e
}
