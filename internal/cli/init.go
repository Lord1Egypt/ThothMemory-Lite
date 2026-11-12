package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// InitSecrets creates missing random credentials and the default token policy
// in dir. Existing secrets and policy are authoritative and never replaced.
func InitSecrets(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := privatePath(dir, true); e != nil {
		return e
	}
	names := []string{"agent_token", "read_token", "codex_token", "claude_token", "opencode_token"}
	for _, name := range names {
		p := filepath.Join(dir, name)
		if b, e := os.ReadFile(p); e == nil {
			if len(b) < 32 {
				return errors.New("existing secret is invalid; refusing automatic replacement")
			}
			continue
		} else if !os.IsNotExist(e) {
			return e
		}
		var b [32]byte
		if _, e := rand.Read(b[:]); e != nil {
			return e
		}
		if e := exclusive(p, []byte(hex.EncodeToString(b[:]))); e != nil {
			return e
		}
	}
	var ps []map[string]any
	existingPermissions := false
	path := filepath.Join(dir, "tokens.json")
	if b, e := os.ReadFile(path); e == nil {
		existingPermissions = true
		if e = json.Unmarshal(b, &ps); e != nil || ps == nil {
			return errors.New("invalid existing tokens.json")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	changed := false
	for _, item := range []struct {
		id, file string
		write    bool
	}{{"local-agent", "agent_token", true}, {"local-reader", "read_token", false}, {"codex", "codex_token", true}, {"claude", "claude_token", true}, {"opencode", "opencode_token", true}} {
		// Existing policy is authoritative, including intentionally removed principals.
		if existingPermissions {
			continue
		}
		found := false
		for _, p := range ps {
			if p["id"] == item.id {
				found = true
			}
		}
		if found {
			continue
		}
		token, e := os.ReadFile(filepath.Join(dir, item.file))
		if e != nil {
			return e
		}
		ps = append(ps, map[string]any{"id": item.id, "token": string(token), "write": item.write, "projects": []string{"*"}})
		changed = true
	}
	if changed {
		return save(path, jsonBytes(ps))
	}
	return nil
}

func exclusive(path string, b []byte) error {
	f, e := privateCreate(path)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}

// PrivateDir restricts a directory to its owner (Unix mode 0700; Windows
// protected owner-only DACL inherited by new children).
func PrivateDir(path string) error { return privatePath(path, true) }

// PrivateFile restricts an existing file to its owner.
func PrivateFile(path string) error { return privatePath(path, false) }
