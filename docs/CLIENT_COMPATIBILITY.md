# Client compatibility

`thoth-lite connect CLIENT` registers the MCP server under the key `thoth-lite`; `thoth-lite instructions`
installs the agent protocol where each client reads instructions; `thoth-lite hooks CLIENT` installs
lifecycle capture where the client has hooks. `thoth-lite integrations doctor` re-runs the checks below on
your machine.

## Verification levels

| Level | Meaning |
|---|---|
| **configured** | Lite's entry is present and well formed in the client's configuration file |
| **listed** | The client's own CLI lists the entry as enabled (it does not start the server) |
| **connected** | The client's own CLI or service started the server and reports it connected |
| **tool call** | A real model turn in the client called a Lite tool and used the result |
| **observed session** | Lite's `client_integrations` table recorded the client's MCP session (self-reported name and version) |
| **automatic capture** | Lifecycle hooks/plugin wrote project, session and event records during a real client run |
| **cross-agent handoff** | Covered by Phase 4 (`docs/VALIDATION_REPORT.md`) |

## Matrix (evidence from 2026-11-12, Linux x86-64/WSL2)

Every runtime check used isolated configuration directories (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, a scratch
`HOME`) or a throwaway project folder; no global client configuration was changed.

| Client | Version tested | configured | client runtime | tool call | observed session | automatic capture | Overall |
|---|---|---|---|---|---|---|---|
| Claude Code | 2.1.322 | ✅ | ✅ connected (`claude mcp list`, user scope); project scope shows *pending approval* until a person approves it | — | ✅ `claude-code 2.1.322` | hooks: carried over from UltraLite, tested with simulated payloads; not exercised in a real run yet | **Verified (connection)** |
| Codex CLI | 0.166.0 | ✅ | listed (`codex mcp list` does not start servers) | — | — | hooks: carried over, simulated only | **Partially verified** |
| OpenCode 1.x | 1.17.13, 1.18.x, 1.19.5 | ✅ | ✅ connected (`opencode mcp list`) on 1.17.13 (legacy) and 1.19.5 (V2) | — | ✅ `opencode 1.17.13`, `1.19.5` | V1 plugin: contract-tested in Node; not exercised in a real 1.x run | **Verified (connection)** |
| OpenCode 2.x | 2.2.7 | ✅ | ✅ connected (OpenCode service API for the project directory) | ✅ free model called `memory_get_health`, answered `0.1.0-dev` | ✅ `opencode 2.2.7` | ✅ V2 plugin: real run registered the project, started a session, recorded idle | **Verified** |
| Google Antigravity | agy 1.4.12 | ✅ (user scope only) | listed (`agy mcp list`) | — | — | none (instruction-based) | **Partially verified** |
| Cline | not installed | ✅ unit-tested | not run | — | — | none (instruction-based) | **Implemented, not runtime-verified** |
| Roo Code | not installed | ✅ unit-tested | not run | — | — | none (instruction-based) | **Implemented, not runtime-verified** |
| Cursor | not installed | ✅ unit-tested | not run (`cursor-agent` absent) | — | — | none (instruction-based) | **Implemented, not runtime-verified** |
| Continue | not installed | ✅ unit-tested | not run | — | — | none (instruction-based) | **Implemented, not runtime-verified** |

"Unit-tested" means the adapter's install/verify/uninstall cycle runs in CI against temporary
directories, including preserving unrelated settings, backups, idempotency, refusing to replace a different
registration and removing only Lite's entry. It does not prove the client loads the file.

## Per-client details

| Client | Config file (project / user) | Instructions file | Lifecycle |
|---|---|---|---|
| Claude Code | `.mcp.json` / `~/.claude.json` (`$CLAUDE_CONFIG_DIR`) | `CLAUDE.md` (imports the protocol) | hooks: SessionStart, UserPromptSubmit, PreCompact, Stop, SessionEnd |
| Codex CLI | `.codex/config.toml` (trusted projects) / `~/.codex/config.toml` (`$CODEX_HOME`) | `AGENTS.md` | hooks in `.codex/hooks.json` when Codex hooks are enabled |
| OpenCode | `opencode.json(c)` / `~/.config/opencode/` | `AGENTS.md` | 1.x: `.opencode/plugins/thoth-lite.js`; 2.x: `.opencode/plugins/thoth-lite/index.js` |
| Antigravity | — / `~/.gemini/config/mcp_config.json` | `GEMINI.md` | none |
| Cline | — / existing `cline_mcp_settings.json` (`$CLINE_MCP_SETTINGS_PATH`, `~/.cline/data/settings/`, or VS Code globalStorage) | `.clinerules/thoth-lite.md` | none |
| Roo Code | `.roo/mcp.json` / existing VS Code globalStorage `mcp_settings.json` | `.roo/rules/thoth-lite.md` | none |
| Cursor | `.cursor/mcp.json` / `~/.cursor/mcp.json` | `.cursor/rules/thoth-lite.mdc` (`alwaysApply`) | none |
| Continue | `.continue/mcpServers/thoth-lite.yaml` / — | `.continue/rules/thoth-lite.md` (`alwaysApply`) | none |

Clients without lifecycle hooks follow the instruction-based workflow in `THOTH_LITE_AGENT_PROTOCOL.md`:
the agent itself resolves the project, starts or resumes a session, records every state-changing step,
checkpoints and finishes through MCP. Nothing is recorded automatically for them, and Lite never claims it is.

## OpenCode versions

Measured by running each release against a throwaway project:

| Release | Legacy `mcp.<name>` | V2 `mcp.servers.<name>` | Plugin API |
|---|---|---|---|
| ≤ 1.18.23 (tested 1.17.13, 1.18.0, 1.18.17, 1.18.22, 1.18.23) | accepted | **rejected** (`Missing key mcp.servers.enabled`) | V1 |
| 1.18.24 – 1.19.5 (tested 1.18.24, 1.18.26, 1.18.35, 1.19.0, 1.19.5) | accepted | accepted | V1 |
| 2.x (tested 2.2.7, installed with `opencode.ai/v2/install`) | accepted | accepted (what `opencode mcp add` writes) | **V2 only**: a V1 plugin fails to load |

What the adapter does:
- **Shape.** It writes V2 only for releases that accept it, and refuses to edit a V2 file for an older
  release (leaving the file untouched). It keeps an existing legacy file's shape, and creates new files on
  2.x in the V2 shape.
- **Plugin.** `hooks opencode` generates the V1 plugin for 1.x and the V2 plugin for 2.x.
- **OpenCode 2 runtime notes.** `opencode mcp list` does not pass the working directory, and a
  `--standalone` server answers before MCP servers connect, so `integrations doctor` asks the OpenCode
  service API for the project directory and polls while the connection is pending. The lifecycle plugin
  launches `thoth-lite` with stdin closed. With Node's `execFile` defaults stdin stayed open, and the
  lifecycle command (which reads hook payloads from stdin) blocked the agent turn. The lifecycle command now
  also stops reading stdin after 2 seconds.
