# Schema audit

Audit of the UltraLite 0.1.0 SQLite schema (`001_initial.sql`, source commit `0ff63ab`) and the
decisions taken for ThothMemory Lite. Each proposed addition was kept only when a concrete product
requirement needed it and tests could exercise it.

## 1. Baseline inventory (`001_initial.sql`)

All tables are `STRICT`. Timestamps are fixed-width UTC text (`2006-01-02T15:04:05.000000Z`), so text
order is chronological. `search_text` columns hold NFC-lowercased text written by the application.

| Table | Responsibility | Key / constraints | Mutation semantics | Main access paths (indexes) |
|---|---|---|---|---|
| `principals` | Credential identities (`codex`, `claude`, `read`, …) | PK `id` (1–200 chars) | insert-only, idempotent | PK |
| `projects` | One project per canonical identity | PK UUID; `repository` UNIQUE; `event_budget` 100–10M | insert; `event_count` incremented per event | PK, UNIQUE(repository) |
| `agents` | Agent names seen in sessions | PK `id` | insert-only | PK |
| `sessions` | Agent sessions at a Git revision | PK UUID; FK project, agent, principal; `revision` for optimistic updates | insert; checkpoint/finish bump `revision`; `finished_at` stays NULL if never finished | (project_id, started_at) |
| `tasks` | Work items with an 8-state lifecycle | PK UUID; state CHECK; `revision`; claim + lease expiry | updated in place under expected revision | (project_id,state,updated_at), (project_id,updated_at) |
| `task_dependencies` | Task → task edges within one project | PK(project,task,depends_on); composite FKs keep both ends in the project; no self-edges | insert-only | PK, reverse index |
| `evidence` | Reported test/build/CI/commit evidence | composite FKs; `source_kind`/`outcome` CHECKs | **append-only (triggers)** | (project,task,revision), (project,created_at) |
| `attestations` | Owner review of evidence | one per evidence (UNIQUE) | **append-only** | (project_id) |
| `events` | Timeline of everything | FKs to task/session in the same project; `details` must be valid JSON | **append-only** | (project,created_at,id), (project,task,created_at), (project,session) |
| `records` | Decisions, issues, observations, handoffs, ideas, notes | `supersedes` UNIQUE + same-project FK; kind CHECK | **append-only** | (project,created_at,id), (project,kind,created_at), (project,session) |
| `operations` | Idempotency: operation ID → payload hash + stored response | PK(project,principal,operation_id) | **append-only** | PK |
| `imports` | Provenance of portable imports | UNIQUE(project,checksum) | **append-only** | (project,imported_at) |
| `schema_migrations` | Applied migration files + SHA-256 | PK version | insert-only | PK |

Findings:

* **Integrity is strong.** Composite `(project_id, id)` foreign keys make every cross-reference
  same-project by construction. Append-only triggers protect history. Optimistic `revision` columns and
  the `operations` table give stale-write rejection and exact idempotent retries. `PRAGMA
  foreign_key_check` and `integrity_check` pass on the real UltraLite copy audited below.
* **Supersession already models replacement.** A newer decision `supersedes` an older one (UNIQUE, so
  history cannot fork). Because `supersedes` may point at any record kind, recording a decision or
  observation that supersedes an issue already marks that issue resolved: the briefing and the
  `open_issues` count exclude superseded records.
* **Failed experiments, reproduction steps, test evidence, release evidence and human approval** are
  representable today: `record_issue` (failed attempt + reproduction), `record_test_result` /
  `attach_evidence` with `outcome=fail|pass`, `source_kind=release`, and owner `attestations`.
* **No redundant storage was found** apart from the denormalised `search_text`, which is deliberate:
  it is the search index (see §4).
* **Retention:** history is never deleted. The per-project `event_budget` (default 100,000 events) is
  the growth bound; the owner can raise it. This is unchanged.

Real-data check (not committed): a copy of a working UltraLite memory (10 projects, 188 records, 431
events, 54 evidence, 18 sessions, 17 tasks, 11 dependencies) imported through
`thoth-lite import-ultralite` with identical row counts in all 13 tables and a passing `doctor`.

## 2. Additions (implemented)

### `002_project_aliases.sql` — project identities (proposal F)

**Requirement:** the lifecycle error *"project not registered"* after an `origin` remote is added to a
project first registered as `local:<uuid>`. Today the only fix is to create a second project for the
remote, which splits the history.

**Design:** `project_aliases(identity PK, project_id FK, principal_id FK, created_at)`. A project keeps its
one canonical `projects.repository`; an alias maps another canonical identity to it. Triggers guarantee
an identity is either a repository or an alias, never both (on insert of either side and on repository
update). Aliases are immutable rows; the owner can remove one. `create_project` with a linked identity
returns the existing project instead of creating a duplicate. Linking is owner-only
(`thoth-lite projects link`), refuses any identity already owned by another project ("projects are never
merged automatically") and records `identity_linked` / `identity_unlinked` events in the same transaction.
New read tool `memory_resolve_project` resolves any identity, respecting credential scopes.

**Not added:** automatic linking. A new remote may belong to an unrelated project; the capture flow
(Phase 4) reports the ambiguity and asks for explicit confirmation instead.

### `003_client_integrations.sql` — observed client integrations (proposal E)

**Requirement:** an honest client compatibility report must show which clients actually connected, not
which configurations were written. **Design:** one row per (credential, client implementation) with the
client's self-reported name and version, transport, first/last handshake, handshake count and last
successful tool call. It is operational state (updated in place) and stores no memory content. It is
written when an MCP session first identifies the client. Classic clients send `initialize`; newer clients
(for example the Go SDK 1.8 client using `server/discover`) carry the identity in each request's `_meta`;
both are handled and both are tested.

## 3. Proposals rejected (with reasons)

| Proposal | Decision | Reason |
|---|---|---|
| A. `artifacts` | rejected | `evidence` already stores locator, `artifact_digest` (SHA-256), `git_revision` and source kind, linked to tasks and attestable. A parallel table would duplicate it. Large binaries never belong in memory. |
| B. `context_sources` | rejected | Freshness is computed at read time: every briefing and search compares stored `git_revision` with the caller's HEAD and returns `stale`. A stored freshness status would itself go stale. |
| C. `memory_relations` | rejected for now | The briefing needs only "replaced" and "resolved", which `supersedes` already provides; tasks have `task_dependencies`; records link to task/session/evidence. Generic typed relations (`contradicts`, `implements` …) would need polymorphic same-project validation and have no consumer yet. Revisit when a read path needs them. |
| D. `workspace_checkouts` | rejected | A local absolute path is not a portable identity, and a shared database should not accumulate per-machine paths. The checkout→project binding belongs with the checkout. Phase 4 stores it in that checkout's Git config, plus `project_aliases` for remotes. |
| G. backup metadata in SQLite | rejected | Backups are local files with a SHA-256 sidecar. Storing their metadata inside the database being backed up is circular: a restore would bring back stale backup records. |

## 4. Search: FTS5 evaluated and not adopted

Search is a deterministic substring scan: NFC-lowercased `INSTR` over records, events, evidence and
tasks, ranked by phrase/title matches. It finds Arabic inside words, commit-hash prefixes and symbols such
as `23505`. Measured with `cmd/thoth-bench` (i7-9750H, WSL2, synthetic Arabic/English/code text, 3
projects, limit 20):

| Events per project (records = 10%) | DB size | Search p50 (typical) | Exact ID |
|---|---|---|---|
| 1,000 | <1 MB | 2–3 ms | 0.3 ms |
| 10,000 | 28 MB | 18–25 ms | 0.2 ms |
| 100,000 (default budget maximum) | 280 MB | 150–245 ms | 0.3 ms |

An FTS5 `trigram` index over the same 100k events (SQLite 3.51):

* added **30%** to the database size (264 → 344 MB) and 9 s of indexing;
* was faster for frequent words only where the scan was already effectively instant (the scan stops after
  the first 20 matches);
* was **far slower for selective, code-like queries** (`e1234`: 8.5 s vs 83 ms; a non-matching term:
  1.6 s vs 62 ms), because common trigrams have huge posting lists;
* cannot match queries shorter than 3 characters, so the scan would still be needed.

Decision: keep substring search. The remaining cost at the 100k maximum comes from ranking every
candidate row, which is acceptable for an occasional agent query. The decision can be revisited with
measurements if real histories approach the budget.

## 5. Context budget

The briefing (`get_project_context`) is already bounded and deterministic. It returns fixed per-section
limits: 12 tasks (active and blocked first), 4 sessions, 4 checkpoints, the latest handoff, 8 current
decisions/issues (superseded ones excluded), 8 ideas/notes and 8 evidence. Bodies are cut to 1,200 bytes
on a rune boundary with a pointer to the full record, so the worst case is about 54 KB of body text.
Large event histories cannot displace decisions, blockers or handoffs, because events are not part of the
briefing. No history is deleted. No change was needed.

## 6. Migration rules applied

* `001_initial.sql` is byte-identical to UltraLite's (its checksum is verified on every open).
* New migrations are additive and run in one transaction; a failure in a later file rolls back earlier
  files of the same run (tested: a conflicting object makes `003` fail and `002` is not left behind).
* A checksum mismatch is refused. A database recording a migration this build does not know (written by a
  newer `thoth-lite`) is refused on open and on import. Downgrades are never attempted.
* The portable export format is unchanged (format version 1, source edition `ultralite`). Aliases and
  client integrations are local wiring and are not exported.
