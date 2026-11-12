# Automatic memory and handoffs

ThothMemory Lite keeps engineering context in two complementary ways:

1. **Agent-written memory (all clients).** The agent protocol (`THOTH_LITE_AGENT_PROTOCOL.md`, v2), installed
   by `thoth-lite instructions` in every client's instruction file, requires the agent to resolve the
   project, start or resume a session, and **write to memory after every step that changes the state of the
   work**: requests, ideas, decisions, failures, findings, test runs, commits/CI and task changes. It must
   checkpoint every few steps, before risky commands and before compaction, and finish with an honest handoff.
   Only the agent knows *what* it decided and *why*, so this is the source of semantic memory.
2. **Lifecycle capture (clients with hooks/plugins).** `thoth-lite hooks CLIENT` installs commands that run
   `thoth-lite lifecycle CLIENT EVENT` on client events. They record metadata only: the session start (and
   print the project briefing into the agent's context), checkpoints before compaction, idle/error/end
   markers, and, only in the opt-in `prompts` mode, the user's request text. They never assert outcomes.

| Client | Lifecycle mechanism | Events |
|---|---|---|
| Claude Code | `.claude/settings.json` hooks | SessionStart, UserPromptSubmit, PreCompact, Stop, SessionEnd |
| Codex CLI | `.codex/hooks.json` (when Codex hooks are enabled) | same set |
| OpenCode 1.x | `.opencode/plugins/thoth-lite.js` | session start context, idle, error, compaction |
| OpenCode 2.x | `.opencode/plugins/thoth-lite/index.js` (V2 plugin API) | `context` hook (briefing once per session), `compaction` hook, `session.execution.*` events |
| Antigravity, Cline, Roo Code, Cursor, Continue | none | instruction-based workflow only |

Capture modes: `thoth-lite capture set off|metadata|prompts` (prompts needs `--yes`; prompts that look like
credentials are never stored).

## How a checkout finds its project

On every lifecycle event, capture resolves the project from Git, never from the folder name:

1. **The `origin` remote**, resolved with `memory_resolve_project`, which also matches identities the owner
   linked to a project.
2. **The checkout's binding** (`git config thoth-lite.project`, written by `thoth-lite projects init-local`)
   for repositories without a remote. It lives in `.git/config`, so it is never committed and each checkout
   keeps its own.
3. If the identity is not in memory yet, **any** event registers it. The identity is the natural key, so
   this cannot create a duplicate. An end or idle event without a session is recorded as a metadata event
   only; it never creates or finishes a session.

A remote added *after* a checkout was bound to a `local:<uuid>` project is **not** linked automatically,
because it might belong to a different project. Capture keeps using the binding and prints the exact command
to run if they are the same project:

```bash
thoth-lite projects link <project-id> <remote>
```

These two failures from UltraLite are fixed and covered by `tests/registration_test.go`:

| UltraLite error | Lite behaviour |
|---|---|
| `no origin remote; register a local:<uuid> project explicitly` | `thoth-lite projects init-local` binds the checkout; capture then works on every event |
| `project not registered; the start event registers it` | any event registers the project by its identity; end/idle never fabricate a session or completion |

Failed writes that are transient (memory busy or unreachable) are queued in `DATA/capture/` (at most 200
entries or 512 KiB) and replayed with their **original operation IDs** by the next event or by
`thoth-lite capture retry`, so a retry can never duplicate a record. A refused write (for example a
credential-looking prompt, or a revoked credential) is reported and not retried.

## Cross-agent handoff, verified with real clients

`scripts/real-client-handoff.sh SCRATCH_DIR` runs a real handoff with installed, authenticated clients in a
disposable directory. No client configuration file is written; user-level Claude settings are excluded so
another memory installation's hooks cannot run; and the script fails if an UltraLite installation's project
list changes. The steps and results of the 2026-11-12 run are in `docs/VALIDATION_REPORT.md`:

1. **Codex** records a task, a decision ("`divide(a, 0)` returns None, never raise"), a rejected approach, a
   failing test and a "partially completed" handoff.
2. **Claude Code** starts with no chat history, recovers those constraints from memory, implements exactly
   the decided behaviour (not the rejected one), runs the tests and hands off.
3. **OpenCode 2** reads back the decision, the rejected approach and Claude's passing test.
4. A **fresh Codex session** reads the task state, the agent order and the latest handoff.

Codex needs `default_tools_approval_mode = "approve"` for the `thoth-lite` server to call memory tools in
non-interactive `codex exec` runs; `thoth-lite connect codex` does not set it, so you choose your approval
posture.
