# Provenance

ThothMemory Lite is an independent, SQLite-only successor to ThothMemory UltraLite.

| | |
|---|---|
| Source repository | https://github.com/Lord1Egypt/ThothMemory (read-only for this project) |
| Source commit | `0ff63ab40e4bf14bc78faa574e2bb7a5118c16e2` (2026-10-14, "Merge pull request #19 from Lord1Egypt/docs/v0.2.3-verified") |
| Source edition used | UltraLite 0.1.0 (`cmd/thothmemory-ultralite`) |
| Compatible UltraLite schema | `001_initial.sql` (SQLite, STRICT tables) |
| License | MIT; the original copyright notice is kept in `LICENSE` |

Git history was not copied: Lite has its own history and versioning, starting at `0.1.0-dev`.

## Imported (modified as needed)

`internal/capture`, `internal/cli` (client registration, hooks, instructions, permissions, private
files, portable export/import CLI), `internal/mcptools`, `internal/memory`, `internal/portable`,
`internal/sqlite` (schema `001_initial.sql` unchanged), `internal/sqlstore`, `internal/contract`
(shared behaviour suite, now driven over in-memory MCP transports), `internal/ultralite` (renamed
`internal/lite`), `pkg/memorycore`, `cmd/thothmemory-ultralite` (renamed `cmd/thoth-lite`),
`protocol.go`, `THOTHMEMORY_AGENT_PROTOCOL.md`, and the SQLite-relevant tests.

## Removed

* PostgreSQL (Full) and MariaDB (Mini) backends: `internal/store`, `internal/mariadb`, `internal/mini`,
  `cmd/thothmemory`, `cmd/thothmemory-mini`, and the `pgx`/`mysql` modules.
* The HTTP service and dashboard: `internal/gateway`, `internal/web`. Lite opens no network listener.
* Docker/Compose packaging, data-volume adoption, Compose backups, the HTTP bridge client and Full/Mini
  CI and release workflows.
* Tests that only exercised PostgreSQL/MariaDB. Their SQLite-relevant scenarios (multilingual search,
  crashed-session honesty, scoped credentials, four-session engineering handoff) were ported to SQLite
  in `tests/audit_test.go`.

## Coexistence with UltraLite

Lite uses its own data directory (`~/.local/share/thoth-lite` on Linux, `THOTH_LITE_HOME` to override)
and ignores UltraLite's `THOTH_HOME`, because opening a database applies migrations. UltraLite memory
is brought over only by `thoth-lite import-ultralite`, which reads a read-only snapshot and never writes
to the UltraLite files.
