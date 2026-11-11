package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/memory"
)

// LinkIdentity makes identity (already canonical) resolve to project. It
// refuses an identity that is any project's repository or already an alias,
// and records the change as a project event in the same transaction.
func (s *Store) LinkIdentity(ctx context.Context, project, identity string, p memory.Principal) error {
	return s.identityChange(ctx, project, identity, p, true)
}

// UnlinkIdentity removes an alias and records the removal as a project event.
func (s *Store) UnlinkIdentity(ctx context.Context, project, identity string, p memory.Principal) error {
	return s.identityChange(ctx, project, identity, p, false)
}

func (s *Store) identityChange(ctx context.Context, project, identity string, p memory.Principal, link bool) error {
	if !p.Owner {
		return memory.ErrForbidden
	}
	if e := s.Register(ctx, p); e != nil {
		return e
	}
	return retry(ctx, func() error {
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return wrap(e)
		}
		defer tx.Rollback()
		var exists int
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects WHERE id=?", project).Scan(&exists); e != nil {
			return wrap(e)
		}
		if exists == 0 {
			return errors.New("unknown project")
		}
		now := dialect.Time(time.Now())
		body := fmt.Sprintf("Owner linked identity %s to this project; agents in checkouts with that identity now resolve here.", identity)
		if link {
			var other string
			e = tx.QueryRowContext(ctx, "SELECT id FROM projects WHERE repository=? UNION ALL SELECT project_id FROM project_aliases WHERE identity=? LIMIT 1", identity, identity).Scan(&other)
			switch {
			case e == nil && other == project:
				return errors.New("identity already resolves to this project")
			case e == nil:
				return fmt.Errorf("identity already belongs to project %s; projects are never merged automatically", other)
			case !errors.Is(e, sql.ErrNoRows):
				return wrap(e)
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO project_aliases(identity,project_id,principal_id,created_at) VALUES(?,?,?,?)", identity, project, p.ID, now); e != nil {
				return wrap(e)
			}
		} else {
			res, e := tx.ExecContext(ctx, "DELETE FROM project_aliases WHERE identity=? AND project_id=?", identity, project)
			if e != nil {
				return wrap(e)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errors.New("identity is not an alias of this project")
			}
			body = fmt.Sprintf("Owner removed alias %s from this project.", identity)
		}
		kind := map[bool]string{true: "identity_linked", false: "identity_unlinked"}[link]
		res, e := tx.ExecContext(ctx, "UPDATE projects SET event_count=event_count+1 WHERE id=? AND event_count<event_budget", project)
		if e != nil {
			return wrap(e)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errors.New("project event budget exhausted")
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO events(id,project_id,kind,body,principal_id,agent_id,git_revision,created_at,details,search_text) VALUES(?,?,?,?,?,'thoth-lite','',?,'{}',?)",
			memory.NewID(), project, kind, body, p.ID, now, memory.SearchText(kind, body)); e != nil {
			return wrap(e)
		}
		return wrap(tx.Commit())
	})
}

// Aliases lists the identities linked to project.
func (s *Store) Aliases(ctx context.Context, project string) ([]memory.Row, error) {
	return s.Rows(ctx, "SELECT identity,project_id,principal_id,created_at FROM project_aliases WHERE project_id=? ORDER BY created_at,identity", project)
}

// RecordHandshake notes that a client completed MCP initialization.
func (s *Store) RecordHandshake(ctx context.Context, principal, client, version, transport string) error {
	if len(client) == 0 || len(client) > 200 {
		client = "unknown"
	}
	if len(version) > 100 {
		version = version[:100]
	}
	now := dialect.Time(time.Now())
	return retry(ctx, func() error {
		_, e := s.DB.ExecContext(ctx, "INSERT INTO client_integrations(principal_id,client_name,client_version,transport,first_seen_at,last_handshake_at) VALUES(?,?,?,?,?,?)"+
			" ON CONFLICT(principal_id,client_name) DO UPDATE SET client_version=excluded.client_version,transport=excluded.transport,last_handshake_at=excluded.last_handshake_at,handshakes=handshakes+1",
			principal, client, version, transport, now, now)
		return wrap(e)
	})
}

// RecordOperation notes the time of a client's last successful tool call.
func (s *Store) RecordOperation(ctx context.Context, principal, client string, at time.Time) error {
	return retry(ctx, func() error {
		_, e := s.DB.ExecContext(ctx, "UPDATE client_integrations SET last_operation_at=? WHERE principal_id=? AND client_name=?", dialect.Time(at), principal, client)
		return wrap(e)
	})
}

// Integrations lists every observed client, most recent first.
func (s *Store) Integrations(ctx context.Context) ([]memory.Row, error) {
	return s.Rows(ctx, "SELECT principal_id,client_name,client_version,transport,first_seen_at,last_handshake_at,last_operation_at,handshakes FROM client_integrations ORDER BY last_handshake_at DESC")
}
