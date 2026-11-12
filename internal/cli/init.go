package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ClientPrincipals are the per-client credentials: one token and one policy
// entry per supported client, so each can be narrowed or revoked alone.
var ClientPrincipals = []string{"codex", "claude", "opencode", "antigravity", "cline", "roo", "cursor", "continue"}

// EnsureCredential makes sure client has a credential in dir. A client whose
// token file exists but whose policy entry is gone was revoked by the owner:
// that is reported, never undone. A client that never had a credential (for
// example after an upgrade added new clients) gets a new one.
func EnsureCredential(dir, client string) error {
	path := filepath.Join(dir, "tokens.json")
	b, e := os.ReadFile(path)
	if e != nil {
		return errors.New("no credential policy; run: thoth-lite init")
	}
	var ps []map[string]any
	if e = json.Unmarshal(b, &ps); e != nil || ps == nil {
		return errors.New("invalid existing tokens.json")
	}
	for _, p := range ps {
		if p["id"] == client {
			return nil
		}
	}
	tokenFile := filepath.Join(dir, client+"_token")
	if _, e = os.Stat(tokenFile); e == nil {
		return fmt.Errorf("the %s credential was revoked by the owner; restore its policy entry deliberately before connecting it", client)
	} else if !os.IsNotExist(e) {
		return e
	}
	var raw [32]byte
	if _, e = rand.Read(raw[:]); e != nil {
		return e
	}
	token := hex.EncodeToString(raw[:])
	if e = exclusive(tokenFile, []byte(token)); e != nil {
		return e
	}
	ps = append(ps, map[string]any{"id": client, "token": token, "write": true, "projects": []string{"*"}})
	if e = save(path, jsonBytes(ps)); e != nil {
		return e
	}
	return privatePath(path, false)
}

// InitSecrets creates missing random credentials and the default token policy
// in dir. Existing secrets and policy are authoritative and never replaced.
func InitSecrets(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := privatePath(dir, true); e != nil {
		return e
	}
	names := []string{"agent_token", "read_token"}
	for _, c := range ClientPrincipals {
		names = append(names, c+"_token")
	}
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
	items := []struct {
		id, file string
		write    bool
	}{{"local-agent", "agent_token", true}, {"local-reader", "read_token", false}}
	for _, c := range ClientPrincipals {
		items = append(items, struct {
			id, file string
			write    bool
		}{c, c + "_token", true})
	}
	for _, item := range items {
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
