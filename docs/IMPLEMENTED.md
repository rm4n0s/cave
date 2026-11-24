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
- **Summary**: The module `github.com/rm4n0s/cave` with `github.com/rm4n0s/errors`; `app.Version()`; `cmd/cave` and `cmd/tcave`, which print their version for `--version` and otherwise say they are not built yet; the linter's configuration; `scripts/heavy.sh` and `scripts/checks.sh`.  The TUI program was named `cave-tui` in this step and renamed `tcave` right after (the user, 2026-11-24).
- **Decisions**: a usage mistake exits with status 2 and any other error with 1. `scripts/checks.sh --no-wine` leaves the Wine run out while working (never to finish a step).
- **Checks**: `scripts/checks.sh` passed: gofmt, vet (Linux and Windows), golangci-lint (0 issues), race tests as a debug build, cgo-free build and tests, the Windows build. The error-tag check and the Wine run are skipped until steps 0.2 and 0.5 add their scripts.
- **Tests**: `versionFrom` on release tags, pseudo-versions and development builds; `run` of both programs for `--version`, an unknown flag, an unexpected argument, a closed output stream and no flag, each error by its route.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 0.2: Architecture tests and the error-tag tool
- **Date**: 2026-11-24
- **Branch**: feat/step-0.2-archtest
- **Summary**: `internal/archtest` parses the module's Go files (every platform's, without building them) and checks rules 1–6 and 8 of SPEC §4.2 (`CheckBoundaries`) and rule 7 (`ScanTags`): every error Tag, where it is created, and whether a test asserts it. `internal/archtest/cmd/errortags` and `scripts/error-tags.sh` print the Tags and the untested ones. Two tests run both over Cave itself, so `go test ./...` fails on a broken boundary or an untested Tag.
- **Decisions**: SPEC §4.2 rule 7 now also says that a Tag is created at one place only (the versions of one function in different platforms' files may share one) and that no error is created outside a function; the tool reports both. Following that, the two usage errors of `cave`, `tcave` and `errortags` got a Tag each (`…Usage`, `…UnexpectedArgument`/`…RootMissing`). A route contains the frames of library functions between Cave's own (`fs.WalkDir`), so tests of such errors assert the route's ends. SPEC D3 records the user's instruction to use Glutter for the GUI.
- **Checks**: `scripts/checks.sh` passed (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested). The Wine run is skipped until step 0.5 adds its script.
- **Tests**: a fixture module that breaks each boundary rule (23 violations, each asserted, and the allowed imports beside them not reported); a fixture module with Tags that are tested by a route, a comparison, a table key, a sentinel literal and a constant, and Tags that are untested, excepted, not literal, created at package level and created twice; the exceptions file (malformed, unreadable, missing); the source errors (no `go.mod`, no module line, a file that does not parse, a folder that cannot be listed) by route; the command's flags, outputs and exit paths.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none
