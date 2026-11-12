# Architecture

```
Claude Code ─┐                                    ┌─ thoth-lite connect / disconnect / integrations
Codex CLI ───┤  each launches its own             │   (adapters: config files, verification)
OpenCode ────┤  `thoth-lite mcp-stdio`            ├─ thoth-lite instructions (protocol v2 in each
Antigravity ─┤  with its own token file           │   client's instruction file)
Cline ───────┤            │                       └─ thoth-lite hooks / lifecycle (capture)
Roo Code ────┤            ▼
Cursor ──────┤   MCP server (official Go SDK)  ── per-credential tools, schema validation
Continue ────┘            │
                          ▼
                 memory domain (internal/memory) ── authorization, revisions, evidence rules,
                          │                          idempotent operations, secret screening
                          ▼
                 SQL layer (internal/sqlstore) ── bounded, project-scoped queries
                          │
                          ▼
                 SQLite engine (internal/sqlite) ── modernc pure-Go SQLite, WAL, synchronous=FULL,
                          │                         checksummed migrations, backup/verify/snapshot
                          ▼
                 DATA/memory.db   DATA/.secrets/   DATA/capture/   DATA/backups/
```

## Processes

There is no daemon. Every client session starts one `thoth-lite mcp-stdio` process; CLI commands and hooks
are short-lived processes. They all open the same SQLite file:

- **Shared lock.** Every process holds a shared advisory lock on `memory.db.instance-lock` while it has the
  memory open. `restore` takes it exclusively, so it refuses while anything is connected.
- **One write transaction at a time.** Writers use `BEGIN IMMEDIATE`, wait up to 250 ms per attempt, and
  retry within a 10 s budget (`THOTH_SQLITE_BUSY_MS`). WAL lets readers continue during writes.
- **Migrations** run on open, in one transaction, and only when needed: opening a current database takes no
  write lock.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/thoth-lite` | CLI entry point: init, doctor, mcp-stdio, inspection, backup/restore, import-ultralite, projects, connect, integrations, hooks, capture |
| `internal/lite` | Data directory, credentials resolution, stdio server wiring, backup/restore/import, lifecycle entry |
| `internal/mcptools` | Tool contracts (26 tools), JSON schema validation, MCP server and session observer |
| `internal/memory` | Domain rules: authorization, input bounds, revisions, evidence/attestation rules, briefing, search terms |
| `internal/sqlstore` | SQL for every read and write (shared engine-neutral code, SQLite dialect) |
| `internal/sqlite` | Engine: connection settings, migrations and guards, integrity checks, snapshots, identity aliases, client integrations |
| `internal/capture` | Lifecycle events → memory: project resolution, sessions, checkpoints, retry queue |
| `internal/cli` | Client adapters, instructions, hooks/plugins, permissions, private files, export/import CLI |
| `internal/portable` | Portable project export/import format (version 1) |
| `internal/contract` | Behaviour suite every change must pass, driven over in-memory MCP transports |
| `pkg/memorycore` | In-process embedding API (for example in an Android app's Go core) |
| `cmd/thoth-bench` | Development benchmark tool (not released) |

Schema and its rationale: [DATABASE_SCHEMA.md](DATABASE_SCHEMA.md), [SCHEMA_AUDIT.md](SCHEMA_AUDIT.md).
Clients: [CLIENT_COMPATIBILITY.md](CLIENT_COMPATIBILITY.md). Capture: [AUTOMATIC_MEMORY.md](AUTOMATIC_MEMORY.md).
