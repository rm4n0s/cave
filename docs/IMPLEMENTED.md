# Cave: Implemented steps

One entry per finished step of `docs/IMPLEMENTATION.md`, newest last. Format:

```
## Step <id>: <title>
- **Date**: YYYY-MM-DD
- **Branch**: feat/step-<id>-<slug>
- **Summary**: what was built, in two or three lines
- **Decisions**: choices the step made that the spec did not (and the spec section updated), or "none"
- **Checks**: the result of each check of rule 10 (passed, or skipped and why)
- **Tests**: what the tests cover; for OpenSSH and Wine tests, what ran and what was skipped
- **Proof**: the files, embedded or linked, or "none"
- **Manual checks**: "none", or "pending, see docs/MANUAL-CHECKS.md"
- **Glutter issues**: "none", or the new entries of docs/GLUTTER-ISSUES.md
```

---

## Step 0.1: The module and the checks
- **Date**: 2026-11-24
- **Branch**: feat/step-0.1-module
- **Summary**: The module `github.com/rm4n0s/cave` with `github.com/rm4n0s/errors`; `app.Version()`; `cmd/cave` and `cmd/tcave`, which print their version for `--version` and otherwise say they are not built yet; the linter's configuration; `scripts/heavy.sh` and `scripts/checks.sh`.  The TUI program was named `cave-tui` in this step; it was renamed `tcave` right after (the user, 2026-11-24).
- **Decisions**: a usage mistake exits with status 2 and any other error with 1. `scripts/checks.sh --no-wine` leaves the Wine run out while working (never to finish a step).
- **Checks**: `scripts/checks.sh` passed: gofmt, vet (Linux and Windows), golangci-lint (0 issues), race tests as a debug build, cgo-free build and tests, the Windows build. The error-tag check and the Wine run are skipped until steps 0.2 and 0.5 add their scripts.
- **Tests**: `versionFrom` on release tags, pseudo-versions and development builds; `run` of both programs for `--version`, an unknown flag, an unexpected argument, a closed output stream and no flag, each error by its route.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none
