# Test fixtures

`ultralite-0.1.0.db` is a ThothMemory UltraLite 0.1.0 database (schema `001_initial.sql`) written by the
**original** UltraLite code at `Lord1Egypt/ThothMemory@0ff63ab` through its `pkg/memorycore` API, then
compacted with `VACUUM INTO`. It holds synthetic data only: two projects (a GitHub remote and a
`local:<uuid>` identity), finished and unfinished sessions, task dependencies, a superseded decision, a
failed test result, ideas, notes and Arabic/accented text. It contains no credentials (tokens live in
`.secrets/`, which was not copied). Upgrade tests copy it before opening; it is never modified in place.
