# Migration guide

## From ThothMemory UltraLite

Lite never opens UltraLite's data directory on its own: it uses `~/.local/share/thoth-lite` (Linux),
`~/Library/Application Support/ThothMemory-Lite` (macOS) or `%LOCALAPPDATA%\ThothMemory-Lite` (Windows),
overridable with `--data DIR` or `THOTH_LITE_HOME`. It ignores UltraLite's `THOTH_HOME`. Both products can
stay installed side by side.

To copy your UltraLite memory:

```bash
thoth-lite import-ultralite                 # from UltraLite's default directory (or THOTH_HOME)
thoth-lite import-ultralite --from DIR      # from another UltraLite data directory
thoth-lite import-ultralite --file FILE.db  # from an UltraLite backup file
thoth-lite connect all                      # register Lite with your agents (new credentials)
```

What happens:

1. A consistent snapshot of the UltraLite database is taken over a **read-only** connection
   (`VACUUM INTO`, which includes committed data still in the WAL). Nothing is written to the UltraLite
   files; this is tested by hashing the source database, WAL and token policy before and after.
2. The snapshot is integrity- and foreign-key-checked, and every applied migration must be one this build
   knows with an identical checksum. A newer or altered schema is refused.
3. The snapshot becomes the Lite database and Lite's migrations are applied to it.
4. Lite generates its own credentials. UltraLite tokens are never copied.

The command refuses to run if the Lite data directory already contains a database.

Export/import of single projects also works between the two products (portable format version 1). This was
checked manually in both directions against the original UltraLite 0.1.0 binary built from `0ff63ab`
(export from one, import into the other, then search the imported decisions); CI does not run it, because
CI has no UltraLite binary.

## Between Lite versions

Upgrading the executable upgrades the database on first open: new migrations are applied in one
transaction, so an interrupted upgrade leaves the previous schema intact. Before upgrading you can take a
backup with `thoth-lite backup`.

Downgrades are not supported. An older `thoth-lite` refuses a database that records a migration it does
not know, and leaves the file untouched. Restore a backup taken before the upgrade instead.
