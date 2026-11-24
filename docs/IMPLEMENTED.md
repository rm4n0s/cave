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

## Step 0.3: Storage
- **Date**: 2026-11-24
- **Branch**: feat/step-0.3-storage
- **Summary**: `internal/core/storage`: `Resolve` decides the configuration, state and runtime folders (flags, then `CAVE_CONFIG_DIR`/`CAVE_STATE_DIR`, then XDG on Linux and `%AppData%`/`%LocalAppData%` on Windows); `Ensure` creates them with mode `0700`; `WriteFileAtomic`; `ReadFile` with a size limit; `Acquire`/`Release`, the one-instance lock (`flock` on Linux, `LockFileEx` on a byte far from the holder's line on Windows). `storagetest.Isolate` gives a test its own folders, home and XDG folders and removes the SSH agent from its environment. Adds `golang.org/x/sys`.
- **Decisions**: the lock's function is `Acquire` (a type and a function cannot both be `Lock`; the plan's text is updated). A folder given by a flag or a `CAVE_` variable must be absolute (`StorageDirNotAbsolute`); the programs make a flag absolute before they pass it (step 4.1). Relative XDG variables are ignored, as the XDG specification says. Without `XDG_RUNTIME_DIR` the runtime folder is `<tmp>/cave-<uid>`, and `Ensure` refuses a runtime folder that is not a real folder of this user with mode `0700` (`StorageRuntimeDirUnsafe`): another user could have made it first. Each function that takes a `ctx` refuses a cancelled one with its own Tag. The close of a file that was only read is not an error. Platform primitives (`tryLock`, `unlock`, `syncDir`) return the OS's error as it is, and their one caller tags it.
- **Checks**: `scripts/checks.sh` passed (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested). The Wine run is skipped until step 0.5 adds its script; the Windows tests of this step (`TestResolveOnWindows`, and the lock and file tests, which are not Linux-only) first run there.
- **Tests**: the resolution table on Linux (11 cases) and on Windows (6 cases, in a `_windows_test.go`); folder and file modes; every step of the atomic write failing through a fake file system, with the old content kept and the temporary file removed; read limits and errors; the lock (a second holder is refused with the first one's program and PID, release and take again, different folders, each failing step); the runtime folder's refusals (open to others, a symbolic link, a file, a parent that cannot be searched or written); `Isolate` leaves no variable pointing into the real home and restores the environment.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 0.4: Clock, sanitize, logging
- **Date**: 2026-11-24
- **Branch**: feat/step-0.4-clock-sanitize-logging
- **Summary**: `internal/core/clock`: `Clock` (now, tickers, `AfterFunc`), the system's clock, and `clock.Fake`, whose timers and tickers fire inside `Advance`, in order, on the caller's goroutine. `internal/core/sanitize`: `Display` replaces every control character, format character, line separator and bad UTF-8 byte with U+FFFD and cuts to a number of characters with `…`. `internal/core/logging`: `ParseLevel` and `Open`, a `slog` logger over a file that starts over past 1 MiB and keeps the full one as `.1`.
- **Decisions**: the log is JSON lines, not text (SPEC §15 updated): a logged error can then be read back as an `errors.ErrorJson` and replayed in a test, which is what the errors package is for. When the full log file cannot be put aside, the record is appended to it all the same and the write reports `LogRotateFailed`. New rule 9 of SPEC §4.2, checked by `internal/archtest`: no Go file contains an invisible character or invalid UTF-8; they are written as escapes (the test table of `sanitize` first held them raw).
- **Checks**: `scripts/checks.sh` passed (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested). The Wine run is skipped until step 0.5 adds its script.
- **Tests**: the fake clock's order of firing (by time, then by creation), stop and reset, a timer made while another fires, tickers that drop unreceived ticks, concurrent use; `Display` on 30 cases (escape sequences, bidirectional overrides and isolates, zero-width and tag characters, invalid UTF-8, the cut) and a sweep of every byte and of the code points proving that nothing unsafe gets through; log levels, records at and below the level, a logged error read back with its route, rotation at and past the limit, and each write and close failure by route.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none
