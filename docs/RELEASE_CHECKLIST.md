# Release checklist

A release is tagged only after these pass and the owner authorizes it. Publishing the draft created by the
`Release` workflow is a manual owner step.

1. All phase PRs merged to `main`; `main` CI green on Linux amd64/arm64, Windows and macOS, `security`
   (govulncheck, no server database drivers) and `cross-build` (byte-identical double builds).
2. `go test -count=1 ./...` passes locally with an isolated `HOME`.
3. `scripts/real-client-handoff.sh $(mktemp -d)/run` passes all checks with the installed clients,
   including "UltraLite installation untouched".
4. `thoth-lite integrations doctor` results copied into `docs/CLIENT_COMPATIBILITY.md` for the versions
   tested; no client marked verified without evidence.
5. `docs/PERFORMANCE.md` numbers re-measured if storage, search or the MCP layer changed.
6. `memory.LiteVersion` and `CHANGELOG.md` updated (drop `-dev`).
7. Run the `Release` workflow manually (`workflow_dispatch`) and download the archives; on Linux run
   `thoth-lite version`, `init`, `doctor`, `backup` and `restore` from the archive.
8. Owner tags `vX.Y.Z` on `main`; the workflow creates a **draft** release with archives and `SHA256SUMS`.
9. Owner reviews and publishes the draft.

Never: force-push `main`, publish without step 8's draft review, or mark Android or the editor clients
(Cline, Roo Code, Cursor, Continue) as runtime-verified without real runs.
