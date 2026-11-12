# ThothMemory Lite

Standalone, SQLite-only shared memory for AI coding agents, in one Go executable: `thoth-lite`.

Agents (Claude Code, Codex, OpenCode and others) launch `thoth-lite mcp-stdio` as an MCP server and share
one local SQLite memory: projects, sessions, checkpoints, tasks, decisions, issues, evidence and handoffs.
There is no database server, no network listener, no Docker and no paid API.

**Status: in development (0.1.0-dev).** Nothing is released yet. This README describes only what is
implemented on `main`.

## What works today

| Area | State |
|---|---|
| MCP stdio server (official Go MCP SDK) with per-agent credentials and project scopes | carried over from UltraLite, tested |
| SQLite engine: pure Go (no CGO), WAL, `synchronous=FULL`, foreign keys, append-only history triggers, idempotent operations, optimistic revisions, checksummed transactional migrations | carried over, tested |
| CLI inspection: `projects`, `brief`, `search`, `tasks`, `timeline`, `call` | carried over, tested |
| Local backup (`VACUUM INTO` snapshot + SHA-256 sidecar) and verified restore that keeps the previous database | carried over, tested |
| Portable project export/import | carried over, tested (SQLite to SQLite) |
| `import-ultralite`: copy UltraLite memory into Lite without writing to the UltraLite files | new, tested (including against a live WAL) |
| Project identity aliases (`projects link`), `memory_resolve_project`, observed client handshakes | new (schema 002, 003), tested |
| Upgrade of UltraLite 0.1.0 databases (every original row preserved; partial migrations roll back; newer schemas refused) | tested against a fixture written by the original code |
| Client adapters for Claude Code, Codex, OpenCode (1.x and 2.x), Antigravity, Cline, Roo Code, Cursor, Continue: `connect`, `disconnect`, `integrations list/capabilities/doctor` | new; runtime status per client in [docs/CLIENT_COMPATIBILITY.md](docs/CLIENT_COMPATIBILITY.md) |
| Agent protocol v2 ("record every step that changes the state of the work") installed for all eight clients by `thoth-lite instructions` | new, tested |
| Automatic lifecycle capture: Claude/Codex hooks, OpenCode 1.x and 2.x plugins | OpenCode 2.2.7 verified in a real run; others carried over / contract-tested |

## Install from source

```bash
go install github.com/Lord1Egypt/ThothMemory-Lite/cmd/thoth-lite@latest   # Go 1.26.11+, no CGO
thoth-lite init        # creates ~/.local/share/thoth-lite (override: --data DIR or THOTH_LITE_HOME)
thoth-lite doctor      # permissions, integrity, and a real MCP stdio session
```

Coming from ThothMemory UltraLite? Lite keeps its own data directory so it never upgrades your UltraLite
database in place. Copy your memory with:

```bash
thoth-lite import-ultralite            # reads ~/.local/share/thothmemory (or THOTH_HOME); never writes to it
thoth-lite import-ultralite --file BACKUP.db
```

## Provenance and license

Derived from ThothMemory UltraLite at commit `0ff63ab`; see [docs/PROVENANCE.md](docs/PROVENANCE.md). MIT license.
