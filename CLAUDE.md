# CLAUDE.md

Guidance for Claude Code when working in this repository (Go, hosted on GitHub). The development process and the error handling are those of [Emetos](https://github.com/rm4n0s/emetos)'s `CLAUDE.md` (the user, 2026-11-24: "use the CLAUDE.md from github.com/rm4n0s/emetos for the development process and error handling"), adapted to this project.

## Project
Cave does the job of `sshd`, as an application that an ordinary user starts and watches: a desktop application (`cave`) or a terminal UI (`tcave`). The user adds **people**, gives each of them public keys, and sees who tried to connect and how many times. It needs no root: every session runs as the OS user who started Cave. The SSH server is `golang.org/x/crypto/ssh`; the desktop UI is built on [Glutter](https://github.com/rm4n0s/glutter), a cgo-free GUI toolkit for Go with Flutter's framework API; the TUI is built on Bubble Tea and Lip Gloss. Cave targets Linux and Windows 10 1809+ (tested under Wine); macOS comes later (the user, 2026-11-24). The system's parts have `_linux.go` and `_windows.go` files with the same function names, and tests that cannot run on both say why in `t.Skip`.

**`docs/SPEC.md` is the source of truth.** Read the sections that cover a task before starting it. If the code and the spec disagree, or a task needs a decision the spec doesn't make, ask instead of guessing. When a decision changes, update the spec on the same branch.

## Commands
- Build: `go build ./...`. Cave is cgo-free; `CGO_ENABLED=0 go build ./...` and `GOOS=windows CGO_ENABLED=0 go build ./...` must always work
- Test all: `go test -race -tags debug_mode ./...` (the race detector needs cgo, so this one needs a C compiler). `-tags debug_mode` makes a debug build of Glutter; a build without tags is a release build. **The tests run in both modes**: the race run as a debug build, the cgo-free run as a release build. A test of a debug-only behavior skips in a release build (`if !foundation.DebugMode { t.Skip(...) }`)
- Test the cgo-free path, as a release build: `CGO_ENABLED=0 go test ./...`
- Test for Windows, under Wine: `scripts/test-windows.sh` (every package that has files for Windows), or `scripts/test-windows.sh ./internal/core/pty`
- Test one package: `go test ./internal/core/people -run TestName -v`
- Test the programs under Wine (builds `tcave.exe` and `cave.exe` and runs them): `scripts/heavy.sh --alone -- go test -count=1 -tags cave_wine ./cmd/...`
- Build for Windows: `scripts/heavy.sh -- scripts/build-windows.sh [--out <dir>]` (`dist/windows-amd64` by default). Proof of `cave.exe`: `scripts/heavy.sh --alone -- scripts/wine-proof.sh <out.png> --config <fixture dir>`
- Tests that need a real display/GPU: `scripts/headless-wayland.sh -- go test -race -tags 'cave_display debug_mode' ./...`
- Update golden files: `go test ./... -run Golden -update-goldens`, then review every changed file (PNG for the desktop UI, text for the TUI)
- All the checks of "Definition of done" in one command: `scripts/checks.sh`
- Heavy commands (full test runs, lint, the Wine run, display tests): through `scripts/heavy.sh` (`--alone` for display tests and the Wine run), see "Memory (OOM)"; `scripts/checks.sh` does it itself
- Vet: `go vet ./...` and `GOOS=windows go vet ./...`
- Lint: `golangci-lint run`
- Format: `gofmt -w cmd internal`
- Run the desktop app: `go run -tags debug_mode ./cmd/cave` (it uses your real configuration)
- Run the TUI: `go run ./cmd/tcave`
- Run either isolated from your configuration: `scripts/isolated.sh [--config <fixture dir>] -- go run ./cmd/tcave <args>`
- Make a proof: `scripts/proof.sh <out.png> ...` for the desktop app and `scripts/tui-proof.sh <out.txt> ...` for the TUI (docs/IMPLEMENTATION.md "Proof methods")
- After changing dependencies: `go mod tidy`

## Definition of done
Before reporting a task as finished, run `gofmt` (on `cmd` and `internal`), `go vet ./...`, `golangci-lint run` and `go test -race -tags debug_mode ./...` (a debug build), and fix anything they report. Say so explicitly if any step was skipped or failed. Also:
- `CGO_ENABLED=0 go build ./...` and `CGO_ENABLED=0 go test ./...` pass (proves Cave stays cgo-free, and runs every test as a release build)
- `GOOS=windows CGO_ENABLED=0 go build ./...` and `GOOS=windows go vet ./...` pass, and `scripts/test-windows.sh` passes under Wine
- `scripts/error-tags.sh --untested` prints nothing (every error Tag has a test that asserts its route; `docs/ERROR-TAGS-UNTESTED.md` is the only exception list)
- Every new exported identifier has a doc comment
- Every changed golden file has been looked at, not just regenerated

`scripts/checks.sh` runs all of them in this order and stops at the first that fails.

## Layout
- `cmd/cave/` is the desktop application's `main` package; `cmd/tcave/` is the TUI's
- `internal/core/...` is everything that is not UI: storage, settings, people and their keys, the attempts list, the host key, the SSH server, sessions and PTYs, SFTP, forwarding. It never imports Glutter, Bubble Tea, Lip Gloss, `internal/gui` or `internal/tui`, so it is tested without a UI
- `internal/app/` is the application behind both UIs: it owns the core's objects, runs the server, and gives the UIs snapshots, commands and change events. It imports the core and no UI library
- `internal/gui/...` is the desktop UI on Glutter; `internal/tui/...` is the terminal UI on Bubble Tea. Both import `internal/app`, never each other, and nothing imports them but their `cmd`
- `internal/archtest/` checks the package boundaries and forbidden imports, and holds the error-tag tool (SPEC §4.2)
- `docs/SPEC.md` is the specification; `docs/IMPLEMENTATION.md` is the step-by-step plan; `docs/IMPLEMENTED.md` logs finished steps; `docs/implemented-proof-images/` holds each step's proofs
- `scripts/` holds helper scripts
- Tests live next to the code (`foo_test.go`); golden files live in `testdata/goldens/` of their package
- The exact package list is defined in `docs/SPEC.md`

## Go conventions
- Never ignore a returned error; handle it or return it
- Pass `context.Context` as the first parameter of anything that does I/O (files, processes, network). Name it `ctx`. Name a Glutter `BuildContext` `bc`
- Tests are table-driven
- No global mutable state and no singletons. Package-level read-only values are fine and are never reassigned
- Prefer the standard library. Allowed dependencies: `github.com/rm4n0s/glutter` (v1.17.11 or newer, only under `internal/gui` and `cmd/cave`), `github.com/rm4n0s/errors`, `golang.org/x/crypto` (`ssh`, `ssh/agent`), `golang.org/x/sys`, `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2` (only under `internal/tui` and `cmd/tcave`), and what these bring in. A step that first imports one of them adds it with `go get <module>@<version>`. Ask before adding any other
- **No cgo**: never write `import "C"`. If a C library is ever needed, ask first
- Only Glutter's UI thread touches widgets and render objects, and only Bubble Tea's `Update` touches the TUI's model. Other goroutines (the server, sessions, timers) hand work over through `SetState` or the binding's `Post` (desktop) and through a `tea.Msg` (TUI)
- Long-running work never blocks a UI thread, and takes a `ctx` so it can be cancelled

## Glutter
- Follow Glutter's API conventions: `NewX(required..., opts ...XOpts) *X` constructors, options structs, `foundation.Opt[T]` for nullable options. Cave's own widgets follow the same conventions
- When Glutter is missing something or has a bug, work around it in Cave without stopping, and mark the workaround with a `// Glutter:` comment that says what it works around and what Glutter should provide instead
- Keep a list of these workarounds in `docs/GLUTTER-ISSUES.md` (what is missing or broken, where the workaround is, and what the fix in Glutter would be), and mention new entries in the report at the end of the task, so they can be fixed in Glutter later. Create the file with the first entry; remove an entry when the fix is in the Glutter version Cave uses, and delete the file when no entry is left
- Read Glutter's source in the Go module cache when its behavior is unclear (`go list -m -f '{{.Dir}}' github.com/rm4n0s/glutter`); its `docs/GUIDE.md` and `docs/SPEC.md` describe its use and design

## Error handling
Use `github.com/rm4n0s/errors` for all errors. Never use `fmt.Errorf`, and never import the standard library `errors` package: this package re-exports `errors.Is`, `errors.As` and `errors.Unwrap`, so a single import covers everything. Read `AI_GUIDE.md` of github.com/rm4n0s/errors (in the Go module cache: `go list -m -f '{{.Dir}}' github.com/rm4n0s/errors`) before writing error code.

**Creating errors**
- New failure with a fixed message: `errors.New("Tag", "message", "key", value, ...)` (all metadata in this one call, never `New(...).SetMetadata(...)`)
  - Tag: short PascalCase name of the failure, e.g. `HostKeyReadFailed`
  - Trailing arguments are key/value metadata pairs, e.g. `"path", path`
- New failure with a formatted message: `errors.NewErrf("Tag", "format %s", arg)`
- Wrapping an error that is NOT already an `*errors.Error` (stdlib, `x/crypto`, Glutter or Bubble Tea errors that are not from this package): `errors.NewErr("Tag", err)`. Add metadata with one `.SetMetadata("key", value, "key2", value2)` call (a later call merges into the metadata, it does not replace it, but use one call unless the data only becomes available later)
- Every `New`, `NewErr` and `NewErrf` call captures the stack at that point
- Construct the error in the function that detects the failure, never in a helper that only builds it (`errNoWindow()`, `fail(tag, msg)`): the route is the stack at the constructor, so a helper puts its own name in every route. The tag is a string literal at the call
- Sentinels are struct literals, `&errors.Error{Tag: "X"}`, never a package-level `errors.New`: its stack is `init`, so every route through it is wrong
- Never put a secret or a private key in a message or in metadata; a public key goes in as its SHA256 fingerprint

**Propagating errors**
- If the error already came from this package, return it untouched: `return err`. Re-wrapping it would replace the Tag and the stack and break route tests
- Functions return the plain `error` interface, never `*errors.Error` (avoids the non-nil-interface-holding-nil-pointer bug)
- `errors.FromError(err)` is a plain type assertion. It fails if something else wrapped the error, so don't wrap these errors with anything

**Inspecting errors**
- `appErr, ok := errors.FromError(err)`, then use `appErr.Tag`, `appErr.Metadata`, `appErr.Message`
- Use `errors.Is` / `errors.As` / `errors.Unwrap` for the underlying cause stored in `OriginalErr`

**Testing errors**
- Every new failure case gets its own unique Tag and a test that asserts the route, not just that an error occurred:
  ```go
  appErr, ok := errors.FromError(err)
  if !ok {
      t.Fatalf("expected *errors.Error, got %T", err)
  }
  if !appErr.HasRoute("Store.AddKey->ParseKey.KeyParseFailed") {
      t.Errorf("unexpected failure path: %s", appErr.Route())
  }
  ```
- A route lists the call chain from outermost to innermost (`Type.Method` or `Func`, joined by `->`) and ends with the Tag. `HasRoute` matches by substring
- Log errors with `slog`, passing the error as an attribute (`l.Error("failed", "err", err)`): `*errors.Error` is a `slog.LogValuer`, so the log gets the Tag, Route, Metadata and stack frames, and the logged object can be replayed in a test via `errors.ErrorJson` and `errors.FromJson`. Do not call `ToJson` or `json.Marshal` on an error yourself

## Security
Cave is a network server that gives shells. These rules come before convenience:
- Public key authentication only. Never add password, keyboard-interactive, host-based or "none" authentication, not even behind a setting or for a test of the real server
- A connection is accepted only when the login name is an enabled person's name and the client proved a key of that person (SPEC §7). Never add a path that skips this, and never answer an unknown name differently from a wrong key
- Everything a client sends before it is authenticated (login name, client version, keys) is hostile: bounded in size and count, and passed through `sanitize.Display` before it is stored, logged or drawn (SPEC §8.3), so no control character or escape sequence reaches a terminal or a log
- The host key and the files of people are written with mode 0600 in folders of mode 0700, atomically. Tests assert the modes
- `ssh.InsecureIgnoreHostKey` and the like appear only in `_test.go` files
- A change to authentication, to the session's environment or to what a forwarded port may reach gets a test of the refusal, not only of the success

## Git and GitHub
- Never commit or push directly to `main`; work on a branch named `feat/...` or `fix/...`
- **Exception:** if `docs/IMPLEMENTATION.md` exists, follow its Rules section for the steps it lists. Each step is done on its own branch, one at a time. A finished step (all Definition-of-done checks passing, its entry added to `docs/IMPLEMENTED.md` on that branch) is fast-forward merged into the local `main`, without a PR, and tagged `step-<id>` locally. **GitHub gets milestones, not steps:** `main` and the tags are pushed once, when the last step of a milestone (a `## M<n>` section of `docs/IMPLEMENTATION.md`) is merged (rule 14). Never commit work for a step directly on `main`. Everything not covered by that file (other changes, fixes outside the listed steps) still follows the rule above
- **A finished milestone is merged, tagged and published without asking** (the user, 2026-11-24: "if a milestone is finished, then merge on main, tag and publish without asking me"): when the last step of a milestone is on the local `main`, tag that commit with the milestone's version and push `main` and the tags to GitHub, then go on with the next milestone. Do not ask for confirmation or for the version number
- **The version of a milestone** is `v0.<n+1>.0` for milestone `M<n>` (M0 is `v0.1.0`, M1 is `v0.2.0`, … M8 is `v0.9.0`), tagged with `git tag -a v0.<n+1>.0 -m "M<n>: <title>"`. A publish of `main` between two milestones (a fix outside the steps that the user asked to publish) takes the next patch number of the last version (`v0.2.1` after `v0.2.0`). `v1.0.0` and anything else outside this scheme is the user's decision: ask
- The last step of milestone `M<n>` sets `devVersion` of `internal/app/version.go` to `0.<n+2>.0-dev`, the version the next milestone develops, in its own commit
- Commit messages: imperative mood, e.g. "Add retry to client"
- PRs: short description, link the issue (`Closes #123`), keep them small
- No `.github/` directory and no GitHub Actions workflows: never add them. The Definition-of-done checks are run locally

## Working through the steps
- **Never stop until every step in `docs/IMPLEMENTATION.md` is implemented** (through the last one in the file; as the plan grows, the last step moves). A finished step is never the end of a turn: the turn's last action is a tool call that works on the next step, not a summary. Do not write "starting the next step now" and then end the turn; start it. Report a finished step (and a published milestone) in one or two lines in the middle of the work, and keep working
- **One agent only**: the session works on the steps itself, one at a time; it does not hand work to subagents
- Don't stop to ask for confirmation, and don't end a turn with a question such as "Should I start the next step?", until every step in `docs/IMPLEMENTATION.md` is finished (through the last one). After a step is merged (or a milestone published), report it briefly and start the next step in the same turn
- The only reasons to stop are the ones `docs/IMPLEMENTATION.md` names: its rule 9 (an ambiguous step, a wrong spec, a missing tool, a new dependency, `sudo`, a check that keeps failing), a push of a milestone that keeps failing (rule 14; the push itself needs no question), and a step that says **ask the user**. Then stop, ask, and continue with the same step after the answer
- **Manual checks** don't stop the work: add them to `docs/MANUAL-CHECKS.md` (create it if needed) with the step id and the exact command for the user, write "pending, see docs/MANUAL-CHECKS.md" in the step's `IMPLEMENTED.md` entry, and go on. When the user answers, record the answer in that entry and remove the item from the list (delete the file when it is empty)
- A request such as "publish to main" that is already satisfied gets a one-line answer, then work continues with the next step

## Don't
- Don't edit generated files (`*_gen.go`); regenerate them with `go generate ./...`
- Don't commit secrets or `.env` files. Keys in `testdata` are generated for the tests, say so in a `README` next to them, and are used nowhere else
- Don't let a Glutter, Bubble Tea or Lip Gloss type appear in `internal/core` or `internal/app`
- Don't leave processes running: every Cave instance, compositor, Wine server, `ssh` client, agent or shell started for a test, a proof or a check is stopped when it is no longer needed (run it under `timeout -k`, in its own process group), and `pgrep -a` shows nothing left before a task is reported done
- Never look for, list, open or read files outside this project folder (no home-directory keys, configs, logs or other projects). If something outside it is needed, ask the user to provide it. Exceptions: dependency source in the Go module cache (including Glutter's), and temporary folders your own tests and scripts create
- Never let a test, proof or check use the user's real Cave configuration, state or lock, the user's `~/.ssh` (keys, `config`, `known_hosts`, `authorized_keys`), the user's SSH agent, the user's `~/.wine`, or the system's `sshd`: Go tests isolate the folders (`storagetest.Isolate`), and scripts run the apps through `scripts/isolated.sh`, `scripts/proof.sh` or `scripts/tui-proof.sh` (SPEC §14.1)
- Never let a test, proof or check listen on anything but `127.0.0.1` or `::1`, or on a fixed port: ask for port 0 and read the port back

## Memory (OOM)
A run that uses too much memory is killed by the OS (exit 137) and takes the session with it. Be careful:
- Run the race tests with `GOMEMLIMIT=3GiB go test -race -p 2 -tags debug_mode ./...`. At most two heavy test or build commands run at the same time on this machine, counting those of every agent; display tests and the Wine run go alone. `scripts/heavy.sh -- <command>` enforces it: it waits for one of two machine-wide slots, and `scripts/heavy.sh --alone -- <command>` takes both. Run every full test run (`./...`), `golangci-lint run` and display or Wine run through it
- Never run Go fuzzing (`-fuzz`) without `-parallel=1`, a short `-fuzztime` (30s at most) and a memory limit (`GOMEMLIMIT=2GiB`, `ulimit -v`); each fuzz worker grows its own memory. Prefer the seed corpus and seeded random tests
- Run the tests of one package (`go test ./internal/core/attempts`) while working; run `./...` only for the final checks
- Tests, proofs and scripts that start compositors, apps, Wine or SSH clients run under `timeout -k`, and a test that builds big inputs (large files, floods of connections) keeps them bounded
- If a command is killed (137), find out why before running it again: it may be a real memory bug (unbounded growth) that needs a fix and a regression test

## Testing notes
- The core and `internal/app` are tested with plain Go tests; no UI needed. The server's tests connect with the client of `golang.org/x/crypto/ssh` to a server on `127.0.0.1:0`
- Tests with the real OpenSSH programs (`ssh`, `sftp`, `scp`, `ssh-keygen`, `ssh-agent`) go through `internal/core/sshtest`, which runs them with `-F none`, a `known_hosts` and keys under `t.TempDir()`, `IdentitiesOnly=yes`, `IdentityAgent=none` (or an agent the test started on a socket under `t.TempDir()`), `BatchMode=yes` and its own `HOME`. They run in the normal test runs with no build tag and call `sshtest.Require(t)`, which skips them only when OpenSSH is missing. They never read or write the user's keys, configuration, `known_hosts` or agent, and use no network but `127.0.0.1`. `pgrep -a ssh` shows nothing of theirs left when a task is reported done
- Sessions in tests run a shell and commands of the tests' own choosing in a home under `t.TempDir()` (`HOME` of the session is the isolated one); a test never starts the user's login shell with the user's real home
- Desktop widget tests use `gluttertest` (`PumpWidget`, `Pump`, `Tap`, finders) with a fake clock, the fake platform and `FakeDesktop` (tray, notifications, file dialogs); no display needed. Golden tests use `gluttertest`'s `MatchesGolden`, which renders through Glutter's software renderer, so they are deterministic and need no GPU. UI tests use the test fonts through `internal/gui/testutil`, never system fonts
- TUI tests drive the Bubble Tea model directly (`Update` with key and window-size messages, then compare `View`) with a fake clock and a fixed color profile; golden files are text. No terminal needed
- Tests that open real windows carry the `cave_display` build tag and are skipped by the default test run; proofs and display tests run in a headless compositor with a software renderer (`scripts/headless-wayland.sh`, `WLR_RENDERER=pixman`), never on the user's desktop and never on the NVIDIA card
- Windows tests run under Wine through `scripts/wine-exec.sh` (a private prefix in `.wine-test/prefix`, git-ignored, a headless X server, `wineserver -k` at the end). Wine is not Windows: what only a PC shows goes to `docs/MANUAL-CHECKS.md`
- Every error Tag has a route test
