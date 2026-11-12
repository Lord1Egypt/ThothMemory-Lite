# Database schema

One SQLite file (`memory.db`) in the data directory. Migrations are embedded in the executable
(`internal/sqlite/schema/`) and applied on open. The rationale for every table is in
[SCHEMA_AUDIT.md](SCHEMA_AUDIT.md).

| Migration | Adds |
|---|---|
| `001_initial.sql` | The UltraLite 0.1.0 schema, unchanged: `principals`, `projects`, `agents`, `sessions`, `tasks`, `task_dependencies`, `evidence`, `attestations`, `events`, `records`, `operations`, `imports`, append-only triggers |
| `002_project_aliases.sql` | `project_aliases` and identity-uniqueness triggers |
| `003_client_integrations.sql` | `client_integrations` |

## Connection settings

Every connection uses `foreign_keys=ON`, `journal_mode=WAL` (readers never block the writer),
`synchronous=FULL` (each commit is fsynced), `trusted_schema=OFF`, `busy_timeout=250 ms` per attempt with
application retries up to 10 s (`THOTH_SQLITE_BUSY_MS`), and `BEGIN IMMEDIATE` for writes. At most 4
connections per process. Files are created owner-only (0600; the directory is 0700).

## Tables added by Lite

### `project_aliases`

| Column | Type | Notes |
|---|---|---|
| `identity` | TEXT PK | canonical identity (`host/path` or `local:<uuid>`), 1–1000 chars |
| `project_id` | TEXT | FK `projects(id)` |
| `principal_id` | TEXT | FK `principals(id)`: who linked it (the local owner) |
| `created_at` | TEXT | UTC |

Triggers: an alias may not equal any `projects.repository`; a project may not be created with, or changed
to, a repository that is an alias; alias rows cannot be updated.

### `client_integrations`

| Column | Type | Notes |
|---|---|---|
| `principal_id` | TEXT | FK `principals(id)`; with `client_name`, the primary key |
| `client_name` | TEXT | client's self-reported MCP implementation name |
| `client_version` | TEXT | self-reported version (≤100 chars) |
| `transport` | TEXT | `stdio` or `in-process` |
| `first_seen_at`, `last_handshake_at` | TEXT | UTC |
| `last_operation_at` | TEXT NULL | last successful tool call, written when the session ends |
| `handshakes` | INTEGER | sessions that identified this client |

Self-reported names and versions are data, not proof; they show that a client completed an MCP session
with this memory.
