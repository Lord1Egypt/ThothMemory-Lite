# ThothMemory Lite

Standalone, SQLite-only shared memory for AI coding agents, in one Go executable: `thoth-lite`.

Claude Code, Codex, OpenCode, Antigravity, Cline, Roo Code, Cursor and Continue launch `thoth-lite mcp-stdio`
as an MCP server and share one local memory: projects, sessions, checkpoints, tasks, decisions, issues,
evidence and handoffs. A new agent session picks up exactly where another agent, or another client, left off.
There is no database server, no network listener, no Docker and no paid API.

**Status: 0.1.0-dev, not released.** Successor to ThothMemory UltraLite 0.1.0 ([provenance](docs/PROVENANCE.md)).
What is verified, and how, is in [docs/VALIDATION_REPORT.md](docs/VALIDATION_REPORT.md).

## Install

```bash
go install github.com/Lord1Egypt/ThothMemory-Lite/cmd/thoth-lite@latest   # Go 1.26.11+, no CGO
thoth-lite init        # creates ~/.local/share/thoth-lite (override: --data DIR or THOTH_LITE_HOME)
thoth-lite doctor      # permissions, integrity, and a real MCP stdio session
```

Prebuilt archives for linux/amd64, linux/arm64, windows/amd64, darwin/arm64 and android/arm64 will be
attached to releases. Android builds are cross-compiled and not runtime-tested.

## Connect your agents

```bash
thoth-lite connect all --dir ~/src/my-project          # every detected client, project scope
thoth-lite connect claude --scope user                 # one client, user scope
thoth-lite instructions --dir ~/src/my-project         # the memory protocol, in each client's instruction file
thoth-lite hooks claude --dir ~/src/my-project         # automatic lifecycle capture (claude, codex, opencode)
thoth-lite integrations doctor                         # what is configured, connected and observed
```

`connect` shows every file it will change, asks first, backs up what it replaces, and never overwrites a
different existing registration. The agent protocol tells agents to **record every step that changes the
state of the work** (requests, decisions, failures, test runs, commits, task changes) and to checkpoint and
hand off honestly. Per-client status, with evidence: [docs/CLIENT_COMPATIBILITY.md](docs/CLIENT_COMPATIBILITY.md).

## Day to day

```bash
thoth-lite projects                      # every project: activity, blockers, next action
thoth-lite brief PROJECT --head SHA      # resume briefing; marks evidence from other commits stale
thoth-lite search PROJECT "query"        # Arabic, English, identifiers, commit prefixes
thoth-lite tasks PROJECT | timeline PROJECT
thoth-lite projects init-local           # a checkout with no git remote gets a stable local:<uuid>
thoth-lite projects link PROJECT REMOTE  # a remote added later resolves to the same project
thoth-lite backup /media/usb/memory.db   # consistent snapshot + .sha256
thoth-lite restore FILE --confirm        # verified, staged, previous memory kept
thoth-lite capture status|set MODE|retry
thoth-lite permissions PRINCIPAL --projects UUID --read-only | --revoke
```

## Coming from ThothMemory UltraLite

Lite keeps its own data directory and never opens UltraLite's on its own, so both can stay installed. Copy
your memory (UltraLite's files are only read):

```bash
thoth-lite import-ultralite            # or --from DIR, or --file BACKUP.db
thoth-lite connect all
```

See [docs/MIGRATION_GUIDE.md](docs/MIGRATION_GUIDE.md).

## Documentation

| | |
|---|---|
| [ARCHITECTURE](docs/ARCHITECTURE.md) | processes, packages, locking |
| [DATABASE_SCHEMA](docs/DATABASE_SCHEMA.md), [SCHEMA_AUDIT](docs/SCHEMA_AUDIT.md) | tables, migrations, and why |
| [CLIENT_COMPATIBILITY](docs/CLIENT_COMPATIBILITY.md) | eight clients, evidence per client, OpenCode 1.x/2.x |
| [AUTOMATIC_MEMORY](docs/AUTOMATIC_MEMORY.md) | protocol, lifecycle capture, project resolution, handoffs |
| [DISASTER_RECOVERY](docs/DISASTER_RECOVERY.md) | local backup, restore, new-machine recovery |
| [SECURITY](docs/SECURITY.md) | model, controls, findings fixed |
| [PERFORMANCE](docs/PERFORMANCE.md) | measured numbers vs UltraLite |
| [VALIDATION_REPORT](docs/VALIDATION_REPORT.md), [RELEASE_CHECKLIST](docs/RELEASE_CHECKLIST.md) | evidence and release gates |

## License

MIT. Derived from ThothMemory UltraLite at commit `0ff63ab`; see [docs/PROVENANCE.md](docs/PROVENANCE.md).
