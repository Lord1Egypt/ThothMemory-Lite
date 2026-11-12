# Validation report

Date: 2026-11-12. Everything here comes from runs in this repository's development session or its GitHub CI.
"Verified" means a real run produced the stated result; "unit-tested" and "contract-tested" are labelled as
such and are weaker.

## Identity and provenance

| | |
|---|---|
| New repository | https://github.com/Lord1Egypt/ThothMemory-Lite (public, MIT) |
| Source baseline | `Lord1Egypt/ThothMemory` @ `0ff63ab40e4bf14bc78faa574e2bb7a5118c16e2` (UltraLite 0.1.0, schema `001_initial.sql`) |
| Original repository unchanged | Re-checked 2026-11-12: `main` = `0ff63ab…`, latest release v0.2.3 (2026-10-14), last push 2026-11-09 (before this work). Nothing in this project writes to it. |
| Live UltraLite installation | Executable, data directory and database never written by this project's commands. **One exception, disclosed below.** |

## Delivery (branch → PR → CI; merge is the owner's)

| PR | Phase | Commits | CI |
|---|---|---|---|
| main | provenance baseline | `4d541db` | — |
| [#1](https://github.com/Lord1Egypt/ThothMemory-Lite/pull/1) | 0–1 SQLite-only extraction | `3b46fe4`, `b1c8d82` | 7/7 pass |
| [#2](https://github.com/Lord1Egypt/ThothMemory-Lite/pull/2) | 2 schema, aliases, integrations | `678d0c2`, `4eb3654` | 7/7 pass |
| [#3](https://github.com/Lord1Egypt/ThothMemory-Lite/pull/3) | 3 client adapters, OpenCode 1.x/2.x | `28ef985`, `52d8a07` | 7/7 pass |
| [#4](https://github.com/Lord1Egypt/ThothMemory-Lite/pull/4) | 4–5 capture fixes, handoff, restore | `d823fdb`, `d3239e0` | 7/7 pass |
| #5 | 6–7 packaging, docs, qualification | this branch | see the PR |

CI jobs: tests on ubuntu-24.04 (amd64), ubuntu-24.04-arm, windows-2025 and macos-15; `security` (govulncheck,
no server database drivers); `cross-build` (5 targets built twice and compared byte-for-byte); GitGuardian.
The PRs are stacked; each is open for owner review because the session cannot merge its own PRs.

## Tests

`go test -count=1 -v ./...` with an isolated `HOME`: **exit 0; 46 top-level tests (76 including subtests)
passed, 0 failed, 1 skipped** (`TestSQLiteHelperProcess`, a helper that only runs as a subprocess).
`go vet ./...` and `gofmt -l .` are clean. `govulncheck` 1.9.0: no vulnerabilities found.

| Area | Tests (highlights) |
|---|---|
| Storage contract | shared behaviour suite over in-memory MCP transports: handoff and evidence gates, concurrency and history immutability, claims and budgets, interrupted writes, multilingual search, checkpoint recovery, dependencies and supersession, ideas, notes and secret screening |
| SQLite | concurrent processes, SIGKILL recovery, idempotency, stale revisions, Unicode search, owner permissions (ported audit scenarios) |
| Migrations | upgrade of a fixture **written by the original UltraLite code**: every row of every original table hashes identically; an interrupted multi-file migration leaves no partial schema; altered checksums and newer schemas refused; CRLF-checkout checksum stability |
| UltraLite import | the source database, WAL and token policy are byte-identical after import from a live, uncheckpointed WAL; newer schema refused |
| Identity | aliases: no duplicates, never merged automatically, triggers enforce uniqueness, scoped credentials, history events |
| Clients | 8 adapters: install/verify/uninstall, backups, idempotency, conflict refusal; OpenCode V2 version gate; OpenCode 2 plugin contract (Node) and API verifier; instructions for every client; revoked credentials never recreated |
| Capture | both UltraLite registration errors reproduced and fixed through real hook commands; stdin deadline |
| Disaster recovery | clean-machine recovery with identical table hashes; corrupt, unsigned, newer-schema and in-use restores refused with live memory unchanged; interrupted restore recovered |

## Real-client evidence

Isolated configuration directories or throwaway projects; no global client configuration was edited.

| Client | Result |
|---|---|
| OpenCode 2.2.7 | V2 and legacy configs connected (service API); a real free-model turn called `memory_get_health` → `0.1.0-dev`; the V2 plugin registered the project, started a session and recorded idle in a real run |
| OpenCode 1.17.13 / 1.19.5 | connected (`opencode mcp list`); V2 boundary measured across 10 releases (accepted from 1.18.24) |
| Claude Code 2.1.322 | connected (`claude mcp list`), session observed in `client_integrations` |
| Codex CLI 0.166.0 | listed; real `codex exec` sessions used memory tools (handoff below) |
| Antigravity (agy 1.4.12) | listed in an isolated HOME. **Blocked:** agy only reads MCP servers from the global `~/.gemini/config/mcp_config.json` (`--settings` rejects `mcpServers`), and the owner forbade editing global configs, so no real tool call was run |
| Cline, Roo Code, Cursor, Continue | not installed: **unit-tested only, not runtime-verified** |

### Cross-agent handoff (`scripts/real-client-handoff.sh`)

Codex 0.166.0 → Claude Code 2.1.322 → OpenCode 2.2.7 → a fresh Codex session, real models, one disposable
memory. Codex recorded a task, a decision ("`divide(a, 0)` returns None, never raise"), a rejected approach, a
failing test and an honest "partially completed" handoff. Claude started with no chat history, recovered the
constraints from memory, implemented the decided behaviour (not the rejected one), ran the tests and handed
off. OpenCode and the fresh Codex session read back the decision, the rejection, Claude's passing test and the
agent order. Script checks, 6/6 PASS:

| Check | Result |
|---|---|
| calc.py implements the recorded decision | PASS |
| tests pass after the handoff | PASS |
| codex and claude sessions recorded | PASS |
| decision recorded | PASS |
| OpenCode read the decision | PASS |
| UltraLite installation untouched | PASS |

**Incident:** the first, manual handoff run (before the script existed) did not exclude user-level Claude
settings. The owner's global UltraLite hooks therefore fired, and the **live UltraLite memory gained one test
project, `handoff-demo` (`e18e5d94-9b3c-4c9b-8173-5102740d56ee`), with one unfinished session.** No existing
data was modified or deleted. UltraLite history is append-only and cleanup is the owner's decision. The script
now passes `--setting-sources project,local`, and the live project count stayed at 12 through every later run.

## Backup and restore

Local only; no remote or cloud feature exists.
- **Disaster recovery.** Backup on machine A, delete A entirely, restore on machine B, `init`. Every table
  hashes identically, `doctor` passes, and an agent with a new credential continues the work.
- **Throughput.** A 268 MB memory backed up in 7.2 s and restored in 12.7 s (see PERFORMANCE.md).

## Platforms

| Platform | Build | Runtime |
|---|---|---|
| linux/amd64 | ✅ | ✅ full suite locally and in CI; real clients |
| linux/arm64 | ✅ | ✅ full suite in CI (native arm64 runner) |
| windows/amd64 | ✅ | ✅ full suite in CI, CLI smoke test (init, doctor with a real MCP session, backup); hook scripts are POSIX-only and skipped |
| darwin/arm64 | ✅ | ✅ full suite in CI |
| android/arm64 | ✅ cross-build, byte-reproducible | **not tested** (no device) |

## Security findings

Fixed: world-readable `tokens.json` after `permissions`; lifecycle hang on open stdin; restore moving live
memory before checking compatibility; databases from newer versions opened by older code; CRLF checksum drift
on Windows; collisions with UltraLite's instruction block and hooks. Details in SECURITY.md. Not provided:
encryption at rest and remote backup (out of scope).

## Known limitations

- Antigravity runtime use is blocked by its global-only MCP configuration (see above).
- Cline, Roo Code, Cursor and Continue are not runtime-verified, and none of them has lifecycle capture
  (instruction-based workflow only).
- Claude Code and Codex hooks and the OpenCode 1.x plugin were exercised with simulated payloads and contract
  tests in this project, not in real client runs; OpenCode 2.x capture was exercised for real.
- Codex `exec` needs `default_tools_approval_mode = "approve"` to call memory tools non-interactively;
  `connect` leaves that choice to the user.
- Search is a linear scan: about 20 ms at 10k events per project, up to about 250 ms at the 100k budget.
- The release workflow has not run yet (it needs a tag or a manual dispatch on `main`).

## Release readiness

**Not ready to tag.** Code, tests and documentation are complete for the agreed scope and every PR is green on
four operating systems. Before `v0.1.0`:

1. Owner reviews and merges PRs #1 → #5 in order.
2. Owner decides about the stray `handoff-demo` project in live UltraLite memory.
3. Release workflow dry run (`workflow_dispatch`) on `main`.
4. Owner authorizes the tag; the draft release is reviewed and published by the owner.
