# Changelog

## Unreleased (0.1.0-dev)

### Added
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
