# Changelog

## Unreleased (0.1.0-dev)

### Added
- Schema `002_project_aliases.sql`: identity aliases so a project registered as `local:<uuid>` can later be
  reached by its Git remote without a duplicate project; owner-only `thoth-lite projects link|unlink|aliases`;
  read tool `memory_resolve_project`.
- Schema `003_client_integrations.sql`: records which MCP clients really connected (self-reported name and
  version, handshakes, last successful call), for both `initialize` and `server/discover` clients.
- Databases written by a newer `thoth-lite` are refused instead of opened; altered migrations are refused.
- `cmd/thoth-bench` (development tool) and documented search measurements; FTS5 evaluated and not adopted.
- Upgrade fixture written by the original UltraLite 0.1.0 code (`tests/testdata/ultralite-0.1.0.db`).
- `thoth-lite import-ultralite [--from DIR | --file DB]`: read-only snapshot of UltraLite memory into a new
  Lite data directory, with integrity, foreign-key and schema-compatibility checks and new credentials.
- CI on Linux amd64/arm64, Windows and macOS; govulncheck; reproducible cross-builds for
  linux/amd64, linux/arm64, windows/amd64, darwin/arm64 and android/arm64.

### Changed
- Extracted from ThothMemory UltraLite (`0ff63ab`) as a standalone module; executable renamed to
  `thoth-lite`; default data directory is `~/.local/share/thoth-lite` (`THOTH_LITE_HOME`), separate from
  UltraLite's.
- The shared behaviour suite now drives the MCP layer over in-memory transports instead of HTTP.
- Go 1.26.11.

### Removed
- PostgreSQL and MariaDB backends, the HTTP service and dashboard, Docker/Compose packaging and their
  dependencies.
