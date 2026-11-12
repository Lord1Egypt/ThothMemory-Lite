# Performance

All numbers were measured on 2026-11-12 on one machine: Intel Core i7-9750H (6 cores/12 threads), Linux
6.18 under WSL2, ext4, Go 1.26.11, `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`. "UltraLite" is the original
`cmd/thothmemory-ultralite` at `0ff63ab`, built the same way on the same machine. Reproduce with
`cmd/thoth-bench` and the commands below. Expect different absolute numbers on other hardware.

## Executable and process

| Metric | Lite | UltraLite | How |
|---|---|---|---|
| Binary size (linux/amd64) | 13.51 MB | 13.25 MB | `stat` |
| Cross-built sizes (linux/arm64, windows/amd64, darwin/arm64, android/arm64) | 12.6 / 13.4 / 12.6 / 13.7 MB | n/a | CI `cross-build` job |
| Cold start (`version`, 30 runs) | p50 4.4 ms, p95 7.6 ms | p50 4.3 ms, p95 5.2 ms | `subprocess.run` timing |
| MCP stdio handshake + `memory_get_health` (`doctor`, 3 runs) | 14–16 ms | 10–11 ms | `doctor` output |
| Server memory during a session (50 tool calls) | VmHWM 18.6 MB | VmHWM 18.3 MB | `/proc/PID/status` |

The handshake is about 5 ms slower than UltraLite's because Lite durably records each client handshake in
`client_integrations` (an fsynced write). That cost buys the observed-session evidence used by
`integrations doctor`.

## Memory operations (`thoth-bench -mode ops -reps 200 -writers 4`)

Each write is one transaction with `synchronous=FULL` (fsync before the call returns).

| Operation | p50 | p95 | max |
|---|---|---|---|
| `append_event` | 2.1 ms | 6.0 ms | 16.2 ms |
| `record_decision` | 3.0 ms | 7.1 ms | 16.9 ms |
| `get_project_context` (briefing) | 0.54 ms | 0.65 ms | 1.4 ms |
| 4 concurrent writers, 800 writes | 527 writes/s sustained (each writer has its own connection pool; SQLite serializes writers, WAL lets readers continue) | | |

## Search (`thoth-bench -events N`, 3 projects, Arabic/English/code text, limit 20)

| Events per project (+10% records) | Database | Typical query p50 | Exact ID |
|---|---|---|---|
| 1,000 | <1 MB | 2–3 ms | 0.3 ms |
| 10,000 | 28 MB | 18–25 ms | 0.2 ms |
| 100,000 (the default per-project budget) | 280 MB | 150–245 ms | 0.3 ms |

The FTS5 trigram alternative and why it was not adopted are in [SCHEMA_AUDIT.md](SCHEMA_AUDIT.md#4-search-fts5-evaluated-and-not-adopted).

## Growth, backup and restore

| Metric | Value |
|---|---|
| Database growth | about 2.6 KB per event plus record at the benchmark's text sizes (28 MB per 11,000 rows; 280 MB per 110,000) |
| Backup of a 268 MB memory (`VACUUM INTO`, integrity check, SHA-256) | 7.2 s (about 37 MB/s) |
| Restore of the same backup (SHA-256, integrity, compatibility, staged copy, migrate, check, swap) | 12.7 s (about 21 MB/s) |
| Backup of the 1.6 MB ops benchmark memory | 33 ms |

## Commands

```bash
go build -o /tmp/tb ./cmd/thoth-bench
/tmp/tb -dir "$(mktemp -d)" -events 100000 -reps 7        # search at scale
/tmp/tb -dir "$(mktemp -d)" -mode ops -reps 200 -writers 4 # operations
```
