# Security

## Model

ThothMemory Lite is a local, single-user tool. Agents start `thoth-lite mcp-stdio` as a child process and talk
over stdin/stdout; **there is no network listener**. The trust boundary is the operating-system user: the data
directory and everything in it are owner-only.

| Control | Implementation |
|---|---|
| Per-client credentials | One random 256-bit token per client (`codex`, `claude`, `opencode`, `antigravity`, `cline`, `roo`, `cursor`, `continue`) plus `local-agent` and a read-only `local-reader`. Client configs contain the token **file path**, never the token. |
| Constant-time token check | `subtle.ConstantTimeCompare` against `.secrets/tokens.json` |
| Scopes | `thoth-lite permissions PRINCIPAL --projects UUID,… [--read-only]`, or `--revoke`. Read-only credentials never see write tools. Project-scoped credentials get `forbidden` for other projects, including `memory_resolve_project` for their identities. |
| Revocation is final | A revoked principal (token file kept, policy entry removed) is never recreated by `init` or `connect` (tested). |
| Owner-only actions | Evidence attestation, identity linking/unlinking, restore: local CLI as the OS user, never through MCP |
| File permissions | Data dir and `.secrets/` 0700; `memory.db`, `-wal`, `-shm`, tokens, policy and backups 0600 (`doctor` checks the data dir, `.secrets/` and `memory.db`); Windows: private owner-only DACLs |
| SQL | Parameterized queries throughout; `trusted_schema=OFF`; foreign keys on |
| History integrity | Append-only triggers on events, records, evidence, attestations, operations, imports; idempotent operation IDs; optimistic revisions |
| Input bounds | Schema-validated MCP inputs; bodies ≤ 8,000 characters; IDs, titles, locators bounded; hook stdin ≤ 64 KiB and read with a 2 s deadline |
| Secret screening | Writes whose text looks like a credential are refused; prompt capture is opt-in and skips credential-looking prompts |
| Untrusted memory | Memory is presented to agents as untrusted data, in the MCP server instructions, every briefing and the protocol file. It is never treated as instructions or authorization. |
| Config edits | `connect` shows every file first, needs approval (`--yes` for automation), backs up anything replaced, refuses to overwrite a different existing registration, and never edits files it did not create except to add or remove its own entry |
| Subprocesses | Argument lists only (`exec.Command`), no shell, except the hook command strings that clients run, which are shell-quoted |
| Supply chain | Pure Go, no CGO. Direct dependencies: MCP Go SDK, modernc SQLite, go-toml, x/sys, x/text, jsonschema-go. `govulncheck` 1.9.0 in CI. Go 1.26.11. Reproducible cross-builds compared byte-for-byte in CI. |

## Findings fixed in this project

| Finding | Severity | Fix |
|---|---|---|
| `permissions` set `tokens.json` (all tokens) to mode 0644, a leftover from the Docker edition. The 0700 `.secrets` directory limited exposure to the owner, but the file mode was wrong. | Medium | Kept 0600; `EnsureCredential` also enforces it |
| A launcher that left stdin open (OpenCode plugins via Node `execFile`) made `lifecycle` block, hanging the agent turn | Medium (availability) | Plugins spawn with stdin closed; stdin read with a deadline |
| Restore moved live memory aside before checking schema compatibility | Medium (integrity) | Verify, then stage, migrate and check, then swap, with rollback |
| A database or backup written by a newer `thoth-lite` was opened by older code | Medium (integrity) | Refused on open, import and restore |
| CRLF checkouts (Git on Windows) changed migration checksums, so existing databases were refused | Medium (availability) | `.gitattributes` keeps `*.sql` LF; checksums normalize CRLF |
| Lite's instruction block and hook detection collided with UltraLite's | Low | Distinct markers and exact-command hook matching |

## Not provided

- Encryption at rest. `memory.db` is protected by file permissions only; use disk encryption.
- Remote or cloud backup (explicitly out of scope). Backups are local files; protect them like the database.
- Protection against the OS user or root: anyone who can read the data directory can read the memory and
  the tokens.

## Reporting

Please report vulnerabilities privately through GitHub security advisories on this repository.
