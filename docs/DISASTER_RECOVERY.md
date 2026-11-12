# Backup, restore and disaster recovery

Everything ThothMemory Lite remembers is one SQLite file, `memory.db`, in the data directory. Backups are
local files you can copy anywhere (USB drive, another machine, your own storage). Lite has no cloud or Git
backup feature and never uploads memory anywhere.

## Back up

```bash
thoth-lite backup                       # DATA/backups/memory-<UTC time>.db + .sha256
thoth-lite backup /media/usb/memory.db  # any path; an existing file is never overwritten
```

A backup is a consistent snapshot taken with SQLite `VACUUM INTO`, which reads one transaction of the live
database, including committed data still in the WAL, while agents keep working. The live `memory.db`,
`-wal` and `-shm` files are never copied directly. Each backup is integrity- and foreign-key-checked after
writing, and its SHA-256 goes to `FILE.sha256`. **Keep the `.sha256` file with the backup**: restore refuses
a backup without it. Backup files are owner-only (0600). Credentials live in `DATA/.secrets/` and are not
part of the backup.

## Restore

```bash
thoth-lite restore FILE            # verify only; changes nothing
thoth-lite restore FILE --confirm  # replace the current memory
```

Nothing in the data directory changes until all of these pass:

1. The SHA-256 matches the sidecar.
2. SQLite `integrity_check` and `foreign_key_check` pass.
3. Every migration recorded in the backup is one this `thoth-lite` knows, with an identical checksum. A
   backup from a newer version is refused (upgrade `thoth-lite` instead).
4. A staged copy (`memory.db.restoring`) is migrated to this build's schema and checked again.

Restore then takes the exclusive lock, so it is refused while any agent session or CLI command has the memory
open. It moves the current database aside as `memory.db.before-restore-<time>` (never deleted) and activates
the staged copy. If the restored database fails to open, the previous one is put back automatically.
Leftovers of an interrupted restore are cleaned up by the next one.

## Recover on a new machine

The old machine is gone and you have only a backup file and its `.sha256`:

```bash
# install thoth-lite (see README)
thoth-lite restore /media/usb/memory.db --confirm   # creates the data directory if needed
thoth-lite init                                     # new credentials for this machine; memory is kept
thoth-lite doctor                                   # permissions, integrity, a real MCP session
thoth-lite connect all                              # register the installed agents again
thoth-lite instructions --dir ~/src/my-project      # if the project's instruction files were lost
```

Credentials are always regenerated on the new machine: tokens from the old machine are not in the backup and
are never needed. Projects bound with `projects init-local` keep their identity in the memory, but the
binding lives in each checkout's `.git/config`. In a fresh clone, run `thoth-lite projects init-local` only
if the checkout has no `origin` remote, then link it with `projects link` if needed.

What is recovered is exactly what was recorded in the memory: projects, sessions, checkpoints, tasks,
decisions, issues, evidence, handoffs, aliases. Original AI conversations are not part of ThothMemory and
cannot be recovered from it.

## Tested

`tests/disaster_recovery_test.go` runs the real executable:

- **Clean-machine recovery.** It builds a memory through real MCP sessions of two agents (two projects,
  dependencies, a superseded decision, an issue, evidence, handoffs, Arabic/Japanese/emoji text, an alias),
  backs it up, deletes the whole data directory and restores on a new one. Every table's rows hash
  identically, `doctor` passes, and a reconnected agent with a new credential gets the briefing, the Arabic
  search hit and the alias.
- **Refusals.** A corrupt backup, a missing sidecar, a missing file and a newer-schema backup are all
  refused, and the live database's hash is unchanged. A restore while an agent holds the memory is refused.
- **Interrupted restore.** A stale staging file from an interrupted restore is handled, and the previous
  memory is preserved.
