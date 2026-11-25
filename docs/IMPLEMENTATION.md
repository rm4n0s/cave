# Cave: Implementation guide

This file breaks the milestones in `docs/SPEC.md` §18 into small steps. Each step is one unit of work for one coding agent session. Work goes strictly top to bottom. Every finished step is recorded in `docs/IMPLEMENTED.md`.

**Who this is for**: coding agents (and humans) implementing Cave. You don't need to read the whole file; you need the Rules below and the one step you're doing.

**How it is written**: steps 0.1 to 4.1 are finished and stay here as a record, in the short form they were done from. The steps from 4.2 on are written out in full, so that a model with less experience of this code (Claude Sonnet at high effort) can follow them without guessing (the user, 2026-11-24: "update steps to be easily understood from Sonnet 5.5 high"). "How the steps from 4.2 on are written" below says how to read them.

---

## Rules

### Before you start a step

1. **Find your step.** It's the first step in this file that has no entry in `docs/IMPLEMENTED.md`. Do only that step. Never work on two steps at once, and never skip ahead.
2. **Read, in this order:**
   1. `CLAUDE.md`, all of it.
   2. The whole step in this file.
   3. Every section of `docs/SPEC.md` listed under **Read**.
   4. `AI_GUIDE.md` of `github.com/rm4n0s/errors` (in the Go module cache), the first time you write error code in a session.
   5. For a step that uses a library (`golang.org/x/crypto/ssh`, Glutter, Bubble Tea, Lip Gloss, `golang.org/x/sys`), the APIs the step names, in the library's source in the Go module cache (`go list -m -f '{{.Dir}}' <module>`); for Glutter also its `docs/GUIDE.md`. Read the source instead of trusting memory: these libraries change.
   6. From step 4.2 on: "How the steps from 4.2 on are written", "What is already built" and "Traps" below, and the box at the top of the step's milestone ("Read this before any step of M5").
3. **Create the branch** from the local `main` (the spec and this plan were written on `feat/spec` and are on `main`):
   ```sh
   git switch main
   git switch -c feat/step-<id>-<slug>     # e.g. feat/step-1.2-people
   ```

### While you work

4. **Do exactly the step's Tasks.** Don't do work that belongs to a later step, even if it looks easy. If a later step will need a hook, leave only a comment: `// TODO(step 5.3): ...`.
5. **Follow CLAUDE.md.** In particular: `rm4n0s/errors` for every error, no cgo, no UI import under `internal/core` or `internal/app`, the "Security" section, isolated folders and loopback-only ports in every test and script.
6. **Match OpenSSH.** Where `sshd` has the behavior (the environment of a session, exit statuses, the replies to requests, the wording of SFTP long names), match it unless the spec says otherwise, and test it against the real OpenSSH client when the step has **OpenSSH tests**.
7. **Both platforms build.** Every step leaves `GOOS=windows go build ./...` passing. Until M7, a part that Windows does not have yet is a `_windows.go` file whose functions return an error with its own Tag (asserted by a `_windows_test.go` test, which the Wine run executes); M7 replaces them.
8. **Glutter gaps.** When Glutter is missing something or has a bug, work around it in Cave, mark the code with `// Glutter: <what and why>`, add an entry to `docs/GLUTTER-ISSUES.md` (create it if it doesn't exist), and mention it in the report (CLAUDE.md "Glutter").
9. **Stop and ask the user** when any of these happens:
   - the step is ambiguous
   - the spec looks wrong, or a decision is needed that it does not make
   - a tool is missing (give the exact install command, e.g. `sudo dnf install sway grim`)
   - a new dependency seems necessary
   - something needs `sudo`
   - a check still fails after a real attempt to fix it
   - the step says **ask the user**

   Never weaken or delete a test, skip a check, or mark a step done just to get unblocked. **Manual checks** don't stop the work: follow CLAUDE.md "Working through the steps".

**Processes you start.** Every compositor, Cave instance, Wine server, SSH client, agent or shell you start for a test, a proof or a check is stopped when you are done with it: run it under `timeout -k`, in its own process group, and stop it from `t.Cleanup` or `defer` in Go code. Before you finish a step, check with `pgrep -a` that nothing you started is still running. The user works on this desktop and sees every stray window.

**Isolation.** Nothing you run may use the user's real Cave folders, `~/.ssh`, SSH agent or `~/.wine`, or listen on anything but loopback (SPEC §14.1). Go tests call `storagetest.Isolate(t)` (step 0.3); scripts run the apps through `scripts/isolated.sh` (step 0.5), `scripts/tui-proof.sh` (step 5.1) or `scripts/proof.sh` (step 6.1).

### Finishing a step

10. **The standard checks must all pass.** `scripts/checks.sh` (step 0.1) runs them from the repo root, in this order, each heavy one through `scripts/heavy.sh`:
    ```sh
    test -z "$(gofmt -l cmd internal)"
    go vet ./...
    GOOS=windows go vet ./...
    golangci-lint run
    GOMEMLIMIT=3GiB go test -race -p 2 -tags debug_mode ./...
    CGO_ENABLED=0 go build ./...
    CGO_ENABLED=0 go test ./...                  # the tests again, as a release build
    GOOS=windows CGO_ENABLED=0 go build ./...
    scripts/error-tags.sh --untested             # from step 0.2
    scripts/test-windows.sh                      # from step 0.5, under Wine
    ```
    If the step lists **Display tests**, run them too: `scripts/heavy.sh --alone -- scripts/headless-wayland.sh -- go test -race -tags 'cave_display debug_mode' <packages>`. Golden tests have `Golden` in their name; update them one package at a time (`go test ./internal/tui -run Golden -update-goldens`), because a package that does not define the flag fails when it gets it.
11. **Proofs.** Only when the step has a **Proof** item:
    - Produce exactly the files it lists, in `docs/implemented-proof-images/`, named `<id>-<name>.<ext>` (e.g. `5.4-people.txt`, `6.3-people.png`), with the method the step gives ("Proof methods" below).
    - **Open every proof** (the Read tool shows images and text) and check that it shows what **Proof** describes. If it doesn't, fix the code. The step isn't done.
    - Never draw, edit, crop or write a proof any other way, and never reuse an older one.
12. **Record the step.** On the step branch, add an entry to `docs/IMPLEMENTED.md`, using the format at the top of that file. Embed or link the proofs.
13. **Commit on the step branch.** Use one commit per step, with the message `Step <id>: <imperative summary>`:
    ```sh
    git add -A && git commit -m "Step 1.2: Add people and their keys"
    ```
    The last step of a milestone has a second commit after it, `Set the development version to 0.<n+2>.0-dev` (CLAUDE.md "Git and GitHub"); the step says so.
14. **Merge every finished step into `main`; publish to GitHub once per milestone.**
    - Merge a step only after rules 10–13 are complete: checks green, proof verified, `IMPLEMENTED.md` entry committed. Never collect several steps into one merge, and never leave a finished step only on its branch.
    - A step counts as finished when its commit is on the local `main` and tagged `step-<id>`; start the next step from there.
    ```sh
    git switch main
    git merge --ff-only feat/step-1.2-people
    git tag step-1.2
    git branch -d feat/step-1.2-people
    ```
    - **Publish a milestone.** When the last step of a milestone (a `## M<n>` section below) is merged, tag it with the milestone's version and push `main` and the tags, **without asking the user** (CLAUDE.md "Git and GitHub": `M<n>` is `v0.<n+1>.0`). That last step also set `devVersion` to `0.<n+2>.0-dev`. The milestone counts as published when that commit is on `origin/main`.
    ```sh
    git tag -a v0.<n+1>.0 -m "M<n>: <title>"
    git push origin main
    git push origin --tags
    ```
    If `--ff-only` fails, rebase the step branch onto `main`, run the standard checks again, and retry. If a push is rejected or fails (authentication, network), retry a few times; if it keeps failing, stop and tell the user. Don't start the next milestone's first step until the milestone's push succeeded.
15. **Report to the user.** Give the step id, a two-line summary, the check results, the proof paths, new `docs/GLUTTER-ISSUES.md` entries, new manual checks, and the commit hash now on `main` (and, at the end of a milestone, that it is on `origin/main`). Then start the next step.

### Proof methods

| | Method | How |
|---|---|---|
| T | Transcript | The verbose output of the named Go tests, saved as text: `scripts/transcript.sh <out.txt> -- go test -v -count=1 -run <pattern> <package>` (step 0.5). It removes temporary paths and timings, so the file is stable |
| U | TUI capture | `scripts/tui-proof.sh <out.txt> [--config <fixture dir>] [--size 100x30] [--during '<command>'] [--keys '<tmux keys>' \| --text '<text>' \| --wait '<regex>']... -- [tcave args]` (step 5.1): the TUI runs isolated in a private `tmux` server on a loopback port the system chose, gets the keys, and the screen is saved as text |
| G | Golden | A golden file made by a `Golden` test (PNG through Glutter's software renderer, text for the TUI), copied to the proofs folder by the step's command |
| D | Desktop screenshot | `scripts/proof.sh <out.png> [--config <fixture dir>] [--size 960x640] [--during '<command>'] [--after <seconds>] -- [cave args]` (step 6.1): the desktop app runs isolated in a headless compositor and `grim` saves the screen |
| W | Wine screenshot | `scripts/wine-proof.sh <out.png> [--config <fixture dir>] [--after <seconds>] -- [cave args]` (step 7.5): the same for the Windows program under Wine |

A proof with real SSH attempts gives the script a `--during` command made of `scripts/proof-ssh.sh` calls (step 5.6): the real `ssh`, with the test keys of the fixture `scripts/testdata/two-people/` and its own `known_hosts`, never the user's.

### How the steps from 4.2 on are written

Every step has the same parts. Read all of them before you write a line.

| Part | What it is |
|---|---|
| **Goal** | What works when the step is finished. If your work does more than this, you are in another step |
| **Read** | What to read first: spec sections, files of Cave, files of a library. Really open them |
| **Files** | The files the step makes or changes. Use these names |
| **Tasks** | Numbered. Do them in order, and run the package's tests after each (`go test ./internal/app -run <Name>`) |
| **Tests** | The tests to write, most by name. Each one says what it does and what it expects. They are the least; add a test for anything else you built |
| **Proof** | The exact command. Run it, then open the file and check what the step says it must show |
| **Finish** | Rules 10–15, and what the last step of a milestone does more |

How to read them:

- **Names are decisions.** A function, a type, a file, a Tag, a sentence shown to the user or a test name that a step writes out is to be used as written, so that later steps (which name them again) fit.
- **Signatures of Cave's own code** in "What is already built" were copied from the code on 2026-11-24. When the code says something else, the code is right: look with `go doc ./internal/app` (or the package you need) and use what is there. When the difference changes what the step must do, stop and ask (rule 9).
- **Names of a library** (Glutter, Bubble Tea, `x/sys/windows`) tell you where to look. Open the file in the module cache before you call anything; never write a library call from memory.
- **"Look at how X does it"** means: open X and copy the pattern, with the same care for locks, cleanup and errors.
- **When a step does not say** how something small looks (a spacing, a helper's name), choose the simplest thing and go on. When it does not say something that changes behavior (what a key does, what is saved, who may connect), that is rule 9.
- **Work in small rounds**: one task, its tests, `go build ./... && go vet ./<package>`, the package's tests; then the next. Run `scripts/checks.sh` at the end, not after every task.
- **A step is one commit** even when it took long (the last step of a milestone has a second, small one for the version).

### What is already built

What steps 0.1 to 4.1 made, as far as later steps call it. `go doc ./internal/<package>` shows the rest.

**`internal/app`** (the only package a UI calls):

| Name | What it does |
|---|---|
| `Open(ctx, Options) (*App, error)`, `(*App).Close(ctx) error` | Opens and closes the application (SPEC §10) |
| `Options{Program, Dirs, Clock, Logger, LogLevel, Version, Port, NoStart, AllowRoot, IsRoot, Addresses, User, Environ}` | The last four are for tests: who is root, this computer's addresses, the user of sessions, the environment of sessions |
| `ParseFlags(program, args) (Flags, error)`, `Flags.Options(program, env) (Options, error)`, `Usage(program) string`, `Version() string` | The command line both programs share. `--help` comes back as the error `FlagsHelp` |
| `Server() ServerSnapshot` | `State` (`server.Stopped`, `Starting`, `Running`, `Stopping`), `Listening []string`, `Port`, `Err`, `HostKeyFingerprint`, `Addresses []string`, and `Command(person, address) string` (the `ssh` line) |
| `People() []people.Person` | `Person{ID, Name, Enabled, CreatedAt, Keys}`, `Key{ID, Type, Fingerprint, Comment, AddedAt, Public}` |
| `Attempts() AttemptsSnapshot` | `Entries []attempts.Entry`, `LastClearedAt`, `NextClearAt`. `Entry{ID, Outcome, PersonID, PersonName, LoginName, Address, Count, FirstAt, LastAt, ClientVersion, Keys, KeyOwner}`; the outcomes are `attempts.Accepted`, `UnknownName`, `Disabled`, `WrongKey`, `NoKey`, `NotProven`, `HandshakeFailed`, `Refused` |
| `Connections() []server.Connection` | `Connection{ID, PersonID, PersonName, KeyID, KeyFingerprint, Address, ClientVersion, Since, Activities}`, `Activity{Kind, Detail}` with the kinds `shell`, `exec`, `sftp`, `local-forward`, `remote-forward`, `agent` |
| `Settings() settings.Snapshot` | `Bool(key)`, `Int(key)`, `String(key)`, `Strings(key)`, `Value(name) (any, bool)`. The keys are constants of `internal/core/settings` (`settings.ServerPort`, …); `settings.Schema() []Def` lists every setting with its `Kind` (`KindBool`, `KindInt`, `KindString`, `KindChoice`, `KindStrings`), `Default`, `Min`, `Max`, `Choices` |
| `StartServer(ctx)`, `StopServer(ctx)`, `Set(ctx, key string, value any)` | The commands that exist. `Set` saves and does what the change asks for at once |
| `Subscribe(func(Event)) (cancel func())` | `Changed{Topics}` at most once every `CoalesceTime` (100 ms); `Topics` is a set of `TopicServer`, `TopicPeople`, `TopicAttempts`, `TopicConnections`, `TopicSettings` (`Has`) |

Inside the package (for steps 4.2 and 4.3): the fields `a.people` (`*people.Store`), `a.attempts` (`*attempts.List`), `a.settings`, `a.server` (`*server.Server`), `a.hostKey`; `a.changed(topic)` asks for a `Changed`; `events.go` has the subscribers and `send`. The test fixture is in `app_test.go`: `newFixture(t, moreSettings)`, `f.open(changeOptions)`, `f.person(a, name) sshtest.Key`, `f.connect(a, name, key, port) *ssh.Client`, `alive(client)`, `waitGone(t, client)`, `freePort(t)`, and the fake clock `f.clock`.

**The core** (a UI never imports these, except for the types above):

| Package | What later steps use |
|---|---|
| `internal/core/people` | `Store`: `Add`, `Rename`, `SetEnabled`, `Remove`, `AddKeys`, `ImportKeys`, `SetKeyComment`, `RemoveKey` (all with `ctx` first), `List`, `Get`, `ByName`, `Subscribe` |
| `internal/core/attempts` | `List`: `Record(Attempt) Entry`, `Entries`, `Clear`, `Remove(id) error`, `Flush(ctx) error`, `LastClearedAt`, `NextClearAt`, `Subscribe(func())` |
| `internal/core/server` | `Server`: `Connections`, `Disconnect(id) error` (`ConnectionNotFound`), `DisconnectAll`, `SetHostKey(ssh.Signer)`, `State`, `Subscribe` |
| `internal/core/hostkey` | `Create(ctx, dirs)` (a new key, replacing the old), `LoadOrCreate`, `HostKey.Fingerprint()`, `Signer()` |
| `internal/core/clock` | `Clock{Now, NewTicker, AfterFunc}`, `clock.System()`, `clock.NewFake(start)` with `Advance(d)` |
| `internal/core/sanitize` | `Display(s, max) string`: makes text safe to show (SPEC §8.3) |
| `internal/core/storage/storagetest` | `Isolate(t) storage.Dirs`: every folder under `t.TempDir()` |
| `internal/core/errtest` | `Route(t, err, parts...) map[string]any`: fails unless the error's route holds every part; returns the metadata |
| `internal/core/sshtest` | `NewKey(t, sshtest.Ed25519) Key` (`Signer`, `Public`, `AuthorizedKey`, `PrivatePath`); `Dial(t, addr, user, key, hostKey) *ssh.Client`; `DialErr(t, addr, config)`; `ClientConfig(user, hostKey, keys...)`; `Require(t)` (skips without OpenSSH); `NewOpenSSH(t, addr, hostKey)` with `SSH(stdin, user, key, options, command...) Result`, `SFTP`, `SCP`, `StartMaster(user, key) (stop func())`, `Start(...)`; `Result{Stdout, Stderr, ExitCode}` |
| `internal/archtest` | The boundary rules (SPEC §4.2) and `ScanTags(root)`, which `scripts/error-tags.sh` prints |

**Scripts**: `scripts/checks.sh` (every check of rule 10; `--no-wine` while you work), `scripts/heavy.sh [--alone] -- <command>`, `scripts/isolated.sh [--config <dir>] -- <command>` (it copies the fixture and takes away the rights of group and others), `scripts/transcript.sh <out.txt> -- <command>`, `scripts/headless-wayland.sh -- <command>`, `scripts/test-windows.sh [packages]`, `scripts/wine-exec.sh`, `scripts/error-tags.sh [--untested]`.

### Traps

Each of these cost time once already. Read the list before a step and again when a test fails in a way you do not understand.

**Errors**
- Every failure site has its **own** Tag, written as a string literal (or a `Tag…` constant) at the call of `errors.New`, `errors.NewErr` or `errors.NewErrf`. Two sites with one Tag fail the checks. No helper function that builds errors.
- An error that already came from `rm4n0s/errors` is returned as it is. Wrap only errors of the standard library and of other libraries.
- Every new Tag needs a test that asserts its **route** (`errtest.Route(t, err, "Type.Method", "Tag")`), or `scripts/error-tags.sh --untested` fails. A Tag you removed takes its test and its sentence in `app.Message` with it.
- A route starts at the function the test called. For an error that was only logged, it starts below the function that logged it.
- From step 4.3 on, a new Tag that a command can return also needs a sentence in `internal/app/messages.go`; `TestEveryTagHasAMessage` says which.

**Time**
- Code takes time from the `clock.Clock` it was given, never from `time.Now`, `time.After` or `time.Sleep`.
- The fake clock runs timers **inside** `Advance`, on the goroutine that called it. A timer that starts a new timer inside its callback is run in the same `Advance` when its time is reached. Do not assert how many timers are pending in all; other parts have timers too.
- Keepalive is off in the test fixtures (`server.keepAliveSeconds` = 0), because one big `Advance` would ask a client several times in no time and close it. Keep it off in new fixtures.
- What happens on another goroutine (a connection arriving, an event of step 4.3) is waited for with a real timeout of a few seconds in a polling loop, not with the fake clock and not with one long sleep.

**Network and processes**
- Tests and proofs listen on `127.0.0.1` only, on a port the system gave. A fixture **must** save `server.address` = `127.0.0.1`: the default is every interface.
- A connect right after a listener closed may be reset instead of refused; a helper that waits for "nobody listens" accepts only "refused" and tries again otherwise.
- A session in a test runs `/bin/sh` (`cmd.exe` on Windows) with a home under `t.TempDir()`; never the user's shell or home.
- Unix sockets have a short limit for their path; make them under `sshtest.ShortDir(t)`, not under `t.TempDir()`.
- Everything a test or a script starts is stopped by it (`t.Cleanup`, a `trap`), runs under a timeout, and is checked with `pgrep -a` at the end of the step. The user's own `ssh-agent` is not yours; leave it alone.
- `ssh` at `LogLevel=ERROR` prints nothing when a channel is refused: assert through Cave's log or through the effect.

**Text**
- Everything a client sent is hostile. It is cleaned when it is stored; a UI draws stored strings as they are and never uses one as a format string.
- No Go file may hold an invisible character (archtest rule 9). Write such text with escapes: `"\uFFFD"`, `"\x1b[31m"`, `"\u202E"`.
- Never put a key, a path of the user or a secret into a message; a public key is named by its `SHA256:` fingerprint.

**Shell and tools**
- A `cd` in one command stays for the next ones. Use absolute paths, or start every command with `cd /home/dev/ai-projects/github-projects/cave &&`.
- Never read, list or use anything outside the project folder, except the Go module cache and the temporary folders of your own tests (CLAUDE.md "Don't").
- Full test runs, the linter, display tests and Wine runs go through `scripts/heavy.sh`. A command that was killed (exit status 137) used too much memory: find out why before you run it again.
- `git` state: work only on the step's branch; never commit on `main`; check `git status --short` before the commit, so that no stray file (a built program, a `dist/` folder, a temporary fixture) goes in.

**User interfaces**
- A UI calls only `internal/app`. `internal/tui` and `internal/gui` never import each other; Bubble Tea and Lip Gloss stay under `internal/tui` and `cmd/tcave`, Glutter under `internal/gui` and `cmd/cave` (archtest checks it).
- No command of the application runs on a UI thread, and only the UI thread touches the UI's state (the box at the top of M5 and of M6 says how).
- Golden files come from the fake application with fixed content (step 4.5), never from a real one: ids, fingerprints and ports of a real one change on every run. Look at every golden you regenerate.

---

## M0: Foundations

### Step 0.1: The module and the checks
**Read**: SPEC §3, §4.1, §16; CLAUDE.md "Commands", "Definition of done", "Memory (OOM)".
**Tasks**
- `go mod init github.com/rm4n0s/cave` (Go 1.27); add `github.com/rm4n0s/errors`.
- `internal/app/version.go`: `devVersion` and `Version()` (SPEC §16).
- `cmd/cave` and `cmd/tcave`: a `main` that parses `--version` and prints `cave <version>` / `tcave <version>`, and otherwise says that the program is not built yet and exits 1. Each `main` calls a testable `run(args, stdout, stderr) error`.
- `.golangci.yml` (the linters of Emetos: the standard ones, `revive` with `exported` and `package-comments`, `gofmt`; `testdata` excluded).
- `.gitignore`: `/cave`, `/tcave`, `/dist/`, `.wine-test/`, `*.actual.png`, `*.diff.png`, `*.actual.txt`.
- `scripts/heavy.sh` (two machine-wide slots, `--alone`; lock files named `cave-heavy.*`) and `scripts/checks.sh` (rule 10; a check whose script does not exist yet is skipped with a line that says so).
**Tests**: `Version()`; `run` of both programs for `--version`, an unknown flag (its Tag and route) and no flag.

### Step 0.2: Architecture tests and the error-tag tool
**Read**: SPEC §4.2.
**Tasks**
- `internal/archtest`: tests of rules 1–6 and 8 of SPEC §4.2 over the module's packages (parsed with `go/parser` and `go list`), each with a fixture under `testdata` that breaks the rule and must be reported.
- `internal/archtest/errortags.go` and `internal/archtest/cmd/errortags`: list every Tag (Tag, file:line, function) of the non-test code of `cmd` and `internal`, for all platforms (it parses files, it does not build them); `--untested` prints the Tags no test asserts, minus those in `docs/ERROR-TAGS-UNTESTED.md`; `--all` keeps them in. A Tag that is neither a string literal nor a `Tag…`/`tag…` constant is an error. Exit status 1 when it printed something.
- `scripts/error-tags.sh` runs it.
**Tests**: each rule on its fixture; the tool on a fixture tree with tested, untested, excepted and non-literal Tags.

### Step 0.3: Storage
**Read**: SPEC §5, §14.1.
**Tasks**
- `internal/core/storage`: `Dirs{Config, State, Runtime}`; `Resolve(Options{ConfigDir, StateDir}, env)` by SPEC §5.1 on Linux and Windows; `(Dirs).Ensure(ctx)` (mode `0700`); `WriteFileAtomic(ctx, path, data)` (SPEC §5.3); `ReadFile(ctx, path, maxBytes)` (a missing file is reported with the sentinel `ErrNotExist`; a file over the limit is `FileTooLarge`); `Acquire(ctx, dirs, program) (*Lock, error)` and `(*Lock).Release()` (SPEC §5.4; `flock` in `_linux.go`, `LockFileEx` in `_windows.go`; `AlreadyRunning` has the holder's program and PID in its metadata).
- `internal/core/storage/storagetest`: `Isolate(t) storage.Dirs` (SPEC §14.1).
**Tests**: resolution table (flags, variables, XDG, defaults; Windows cases in a `_windows_test.go`); modes of folders and files; an atomic write that fails leaves the old content; a second lock fails and says who holds it; release then lock again works; `Isolate` leaves no variable pointing at the real home.

### Step 0.4: Clock, sanitize, logging
**Read**: SPEC §8.3, §15.
**Tasks**
- `internal/core/clock`: `Clock` (`Now`, `NewTicker`, `AfterFunc`), `System()`, and `clock.Fake` with `Advance` (timers fire in order, synchronously with `Advance`).
- `internal/core/sanitize`: `Display(s string, max int) string` (SPEC §8.3).
- `internal/core/logging`: `Open(ctx, path, level) (*slog.Logger, io.Closer, error)` with the size limit of SPEC §15, and `ParseLevel`.
**Tests**: the fake clock's order of firing and stop; `Display` on a table of hostile strings (escape sequences, bidirectional overrides, invalid UTF-8, long input, combining marks kept); rotation at the limit; a bad level's Tag.

### Step 0.5: Scripts for isolation, transcripts and Wine
**Read**: SPEC §14; CLAUDE.md "Testing notes"; Glutter's `scripts/headless-wayland.sh`, `headless-x11.sh`, `wine-exec.sh`, `test-windows.sh` in the module cache (`go mod download github.com/rm4n0s/glutter@v1.17.11` only fetches the source; do not add the module to `go.mod` yet).
**Tasks**
- `scripts/isolated.sh [--config <fixture dir>] [--keep] -- <command>`: a temporary folder with `CAVE_CONFIG_DIR`, `CAVE_STATE_DIR`, `HOME`, `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `XDG_RUNTIME_DIR` inside it and `SSH_AUTH_SOCK` unset; the fixture is copied in as the configuration; the folder is removed afterwards.
- `scripts/transcript.sh` (proof method T).
- `scripts/headless-wayland.sh`, `scripts/headless-x11.sh`, `scripts/wine-exec.sh`, `scripts/test-windows.sh`: Cave's copies of Glutter's scripts (same author, BSD-3-Clause; say where they come from in the header), with the prefix in `.wine-test/prefix` of this repository and variables named `CAVE_…`.
- `scripts/checks.sh` now runs `scripts/test-windows.sh`.
**Tests**: `scripts/test-windows.sh` passes for `internal/core/storage` (the Windows resolution and lock tests of step 0.3 run under Wine); a Go test in `internal/archtest` runs `scripts/isolated.sh -- env` and asserts the variables (skipped on Windows).
**Proof** (T): `0.5-wine-storage.txt`, the transcript of `scripts/test-windows.sh -v -run 'Lock|Resolve' ./internal/core/storage`.

---

## M1: Data

### Step 1.1: Settings
**Read**: SPEC §5.2, §6.
**Tasks**: `internal/core/settings`: the schema as one table (key, kind, default, range or choices); typed getters (`Bool`, `Int`, `String`, `Strings`); `Store` with `Open(ctx, dirs)`, `Snapshot()`, `Set(ctx, key, value)` (validates, writes the file, calls listeners), `Subscribe`; unknown keys kept; the errors of SPEC §5.2 and §6. `MatchEnvName(patterns, name)` for `session.acceptEnv`.
**Tests**: defaults; every key's bad values (`SettingInvalid` with the key); unknown key kept through a rewrite; `SettingUnknown` when set; version too high and a broken file are refused and not overwritten; listeners; concurrent `Set`; env patterns.

### Step 1.2: People and their keys
**Read**: SPEC §7.1, §7.2.
**Tasks**: `internal/core/people`: `ParseKeys(text) ([]Key, error)` (SPEC §7.2, every error there); `Person`, `Key` (id, type, fingerprint, comment, added, the `ssh.PublicKey`); `Store` with `Open`, `List`, `Add`, `Rename`, `SetEnabled`, `Remove`, `AddKeys`, `ImportKeys(ctx, personID, path)`, `SetKeyComment`, `RemoveKey`, `Subscribe` (the change says which person and key ids were renamed, disabled or removed, for step 2.3); `Lookup(name, key ssh.PublicKey) Result` implementing steps 2–5 of SPEC §7.3 and also naming the other person who owns the key, if any.
**Tests**: names (valid, invalid, taken ignoring case); every key type accepted and refused, weak RSA, options, private key (and that the private text is in no error or log), duplicates, all-or-nothing; file import and its size limit; `Lookup` for every reason; the file's mode and round trip; concurrent use under `-race`. Test keys are generated by the tests.

### Step 1.3: The attempts list
**Read**: SPEC §8.
**Tasks**: `internal/core/attempts`: `Outcome`; `Attempt` (what the server reports: outcome, person id and name, login name, address, client version, key fingerprints, key owner, time); `List` with `Open(ctx, dirs, clock)`, `Record(Attempt)`, `Entries()`, `Remove(id)`, `Clear()`, `ApplyPolicy(policy, atStart bool)` and `PolicyChanged(policy)` (SPEC §8.4), `LastClearedAt`, `NextClearAt`, `Flush(ctx)`, `Subscribe`; sanitizing and cutting at `Record`; the limits and the delayed write of SPEC §8.5 (driven by the clock).
**Tests**: counting into one entry versus new entries; hostile login names stored sanitized; the 5 000 limit drops the oldest; every policy with the fake clock, including a period that passed while closed and a change of policy restarting the period; at most one write per 5 seconds; `Flush`; file errors.

### Step 1.4: The host key
**Read**: SPEC §9.10.
**Tasks**: `internal/core/hostkey`: `LoadOrCreate(ctx, dirs) (*HostKey, error)`, `Create(ctx, dirs)` (replace), `Signer()`, `Fingerprint()`, `AuthorizedKey()`.
**Tests**: created once and stable across loads; mode `0600`; the `.pub` matches; unreadable and unparsable files are errors and are not replaced; replaced key differs; `ssh-keygen -l -f` agrees with the fingerprint (through `exec`, skipped when OpenSSH is missing; the harness of step 2.1 comes later).

---

## M2: Server

### Step 2.1: The test harness
**Read**: SPEC §14; CLAUDE.md "Testing notes".
**Tasks**: `internal/core/sshtest`: `NewKey(t, kind) Key` (signer, `authorized_keys` line, a private key file under `t.TempDir()` with mode `0600`); `Dial(t, addr, user, key, hostKey) *ssh.Client` (the in-process client, checking the host key); `Require(t)` and `OpenSSH` (runs `ssh`, `sftp`, `scp` against an address with the isolation of CLAUDE.md, a `known_hosts` it writes, a timeout, and returns output and exit status); `StartAgent(t, keys...)` (a real `ssh-agent` on a socket under `t.TempDir()`, stopped in cleanup).
**Tests**: against a minimal `x/crypto/ssh` server inside the test (not Cave's): the client and the real `ssh` run `exec` and get the output; the options that isolate are present on every command line; nothing is left running.

### Step 2.2: Listening, the handshake and authentication
**Read**: SPEC §7.3, §8.1, §8.2, §9.1, §9.2; `ServerConfig`, `NewServerConn`, `PublicKeyCallback`, `AuthLogCallback`, `ServerAuthError` in `x/crypto/ssh`.
**Tasks**: `internal/core/server`: `Server` with `New(Options{People, Attempts, HostKey, Settings, Clock, Logger, Version})`, `Start(ctx)`, `Stop(ctx)`, `State()`, `Addrs()`, `Subscribe`; a `ServerConfig` per connection with a tracker of the keys offered; the outcome of SPEC §8.1 recorded for every connection; the limits of SPEC §9.2. After authentication every channel and global request is refused for now.
**Tests**: each outcome of SPEC §8.1 produced by a real client behavior (right key, wrong key, unknown name, disabled, no key, an offered key not proven, garbage bytes, a silent connection past the time limit with the fake clock, the 65th and the 9th-per-address connection); what the client sees is identical for unknown name, disabled and wrong key; RSA with SHA-1 refused; `ListenFailed`; start and stop twice; the real `ssh` gets "Permission denied (publickey)" for a wrong key.

### Step 2.3: Connections
**Read**: SPEC §7.1 (last item), §9.3.
**Tasks**: the registry (`Connections()`, `Disconnect(id)`, `DisconnectAll()`); the dispatch of channels and global requests through handlers registered by type (`session` and the rest come in later steps: still refused, through the dispatch); the 256-channel limit; `keepalive@openssh.com` answered, `no-more-sessions@openssh.com` obeyed; connections closed when their person is renamed, disabled or removed, or their key removed.
**Tests**: the registry's content during and after a connection; `Disconnect`; each people change closes the right connections and no other; the channel limit; unknown channel types and requests refused with the right reason codes; `Stop` closes everything and the registry is empty.

### Step 2.4: Sessions without a PTY
**Read**: SPEC §9.4, §9.5.
**Tasks**: `internal/core/session`: the handler of `session` channels: `env`, `shell`, `exec`, `signal`, `break`; the environment of SPEC §9.5 (`BuildEnv`, a pure function); the shell lookup (`/etc/passwd` reader); pipes; `exit-status` and `exit-signal`; the 2-second drain; `SIGHUP` to the group when the channel ends first; the registry shows the session. `_windows.go`: `SessionUnsupported` for now.
**Tests**: `exec` output on both streams and exit statuses (0, 3, 127); a signal's `exit-signal`; standard input to `cat` and its end; `env` accepted and refused by pattern, the limits; the exact environment (nothing of the test's own environment leaks, `SSH_AUTH_SOCK` of the test never appears); a process that outlives the channel gets `SIGHUP`; a background child holding the pipe does not hang the channel; a second `exec` on one channel is refused; a shell that does not exist: `SessionStartFailed` and status 255.

### Step 2.5: PTY sessions
**Read**: SPEC §9.6.
**Tasks**: `internal/core/pty`: `Open(size, modes) (*Pty, error)`, `Resize`, `Close`, `Name` on Linux without cgo; the terminal-mode decoder of RFC 4254 §8 and its application through `termios`; `_windows.go`: `PtyUnsupported`. `session`: `pty-req`, `window-change`, the process as session leader on the PTY, the login shell's `argv[0]`, `TERM` and `SSH_TTY`.
**Tests**: `tty` prints the PTY's name and `SSH_TTY` equals it; `stty size` shows the size asked and the size after `window-change`; sizes out of range refused; modes applied (`ECHO` off is seen by `stty -a`); a malformed mode list: `PtyModesInvalid`; `echo $0` of a login shell starts with `-`; closing the channel ends a `sleep` (the kernel's `SIGHUP`); no PTY left open after the tests (count `/dev/pts`).

### Step 2.6: OpenSSH against Cave
**Read**: SPEC §1 (the last item), §14.2.
**Tasks**: only tests and fixes: the real `ssh` for `exec`, standard input, exit status, `-tt` with a shell (send commands, read the output, `exit`), `-O` multiplexing with two sessions on one connection (two sessions in the registry); `scp -O` both ways; `rsync -e` and `git clone` over SSH when those programs are installed (skipped with the reason otherwise).
**Proof** (T): `2.6-openssh.txt`, the transcript of the OpenSSH tests of `internal/core/server` and `internal/core/session`.

---

## M3: Subsystems

### Step 3.1: SFTP: files
**Read**: SPEC §9.7; draft-ietf-secsh-filexfer-02 as you know it, and OpenSSH's behavior.
**Tasks**: `internal/core/sftp`: the packet reader and writer with the size limit; `INIT`/`VERSION` with the extensions listed; attributes encoding; handles and their limit; `OPEN` (every `pflags` combination), `CLOSE`, `READ`, `WRITE`, `LSTAT`, `FSTAT`, `STAT`, `SETSTAT`, `FSETSTAT`, `REMOVE`, `REALPATH`; the status mapping; `Server.Serve(ctx, rw)` behind a small file-system interface with the Linux implementation; `sshtest`'s minimal SFTP client for tests. Wire `subsystem sftp` into `session` (with `sftp.enabled`).
**Tests**: each request's success and each status; a packet over the limit ends the session; the handle limit; seeded random packets never panic and never allocate beyond the limit; reading and writing a file of several megabytes in out-of-order chunks; `sftp.enabled` off refuses the subsystem.

### Step 3.2: SFTP: folders, links, extensions
**Read**: SPEC §9.7.
**Tasks**: `OPENDIR`, `READDIR` (long names), `MKDIR`, `RMDIR`, `RENAME`, `READLINK`, `SYMLINK` (OpenSSH's argument order), `posix-rename`, `hardlink`, `fsync`, `statvfs`, `fstatvfs`, `limits`.
**Tests**: each request; `RENAME` onto an existing file fails and `posix-rename` replaces.
**OpenSSH tests**: `sftp -b` batch files (put, get, ls -l, mkdir, rename, rm, ln -s, chmod, df) and `scp` (SFTP mode, both ways, `-r`), comparing files byte for byte.
**Proof** (T): `3.2-sftp.txt`, the transcript of the OpenSSH SFTP tests.

### Step 3.3: Local forwarding
**Read**: SPEC §9.8 (`direct-tcpip`).
**Tasks**: `internal/core/forward`: the `direct-tcpip` handler; the registry shows forwards in use.
**Tests**: bytes both ways to a loopback echo server and half-close; `forwarding.local` off: `administratively prohibited`; a closed port: `connect failed`; the 15-second limit with a dialer the test controls. **OpenSSH tests**: `ssh -L` and `ssh -D` (a SOCKS5 request written by the test) reach the echo server.

### Step 3.4: Remote forwarding
**Read**: SPEC §9.8 (`tcpip-forward`).
**Tasks**: `tcpip-forward`, `cancel-tcpip-forward`, `forwarded-tcpip`; the binding rules; the 32-listener limit; the registry.
**Tests**: port 0 returns the port; connections arrive as channels with the right origin; loopback is forced when `gatewayPorts` is off (asked `0.0.0.0`, got `127.0.0.1`; asked `::`, got `::1`); cancel; the limit; listeners close with the connection; off: refused. With `gatewayPorts` on, the test still asks only for loopback addresses (rule: tests never listen elsewhere) and the address logic is tested as a pure function. **OpenSSH tests**: `ssh -R 0:127.0.0.1:<echo>`.

### Step 3.5: Agent forwarding
**Read**: SPEC §9.8 (agent).
**Tasks**: `internal/core/agentfwd`: the socket of SPEC §9.8 on Linux (`_windows.go`: `AgentUnsupported`); `auth-agent-req@openssh.com` in `session`; `SSH_AUTH_SOCK`.
**Tests**: modes of the folder and socket; `ssh-add -l` in the session lists the key of the client's agent (`sshtest.StartAgent`); the socket is gone after the session; off: the request is refused and no variable is set; Cave's own `SSH_AUTH_SOCK` never reaches a session. **OpenSSH tests**: `ssh -A` with a test agent.
**Proof** (T): `3.5-forwarding.txt`, the transcript of the OpenSSH tests of steps 3.3–3.5.

### Step 3.6: Keepalive
**Read**: SPEC §9.9.
**Tasks**: the keepalive of SPEC §9.9, driven by the clock and by the bytes read from the connection.
**Tests** (fake clock): an idle client that answers stays; one that does not answer is closed after three; traffic postpones the question; `0` turns it off; a change of the setting applies to live connections.

---

## M4: Application

### Step 4.1: The application object
**Read**: SPEC §9.1 (root), §10, §6 (the last two paragraphs).
**Tasks**: `internal/core/netinfo` (`Addresses()`: this computer's addresses, loopback last, link-local IPv6 left out); `internal/app`: `Options` (folders, clock, logger, version, `Port`, `NoStart`, `AllowRoot`), `Flags` (the flag set both programs share: `--config-dir`, `--state-dir`, `--port`, `--no-start`, `--allow-root`, `--log-level`, `--version`), `Open`, `Close`, the snapshots, `Subscribe` with coalescing, `StartServer`, `StopServer`, `Set` and the effects of settings (the listener restarts on a port or address change and keeps connections), the once-a-minute check of the attempts policy, `RunningAsRoot`.
**Tests**: open on empty folders makes the files and listens; a second `Open` is `AlreadyRunning`; `--no-start`; a port in use leaves the app open with the server stopped and the error in the snapshot; changing the port keeps a connected client and accepts on the new port; events are coalesced; `onStart` clears; a period clears while running (fake clock); `Close` flushes attempts and releases the lock; root refusal through an injected "is root" function.

### Step 4.2: Commands

**Goal**: every command of SPEC §10 is a method of `*app.App`, so that a UI never touches the core.

**Before you start**: an earlier session began this step and was paused. If `git stash list` shows `step 4.2: attempts.SubscribeRecords (paused)`, leave it where it is: it belongs to step 4.3, which says what to do with it.

**Read**: SPEC §10, §7.1, §7.2, §8.4, §9.10. "What is already built" above.

**Files**: `internal/app/commands.go` (new), `internal/app/commands_test.go` (new), `internal/app/scenario_linux_test.go` (new).

**Tasks**

1. In `commands.go`, add these methods. Each one calls the method of the store or the server named beside it and returns its error **untouched** (`return err`; wrapping it would change the Tag and the route). Give each a doc comment.

   | Method | It calls |
   |---|---|
   | `AddPerson(ctx, name string) (people.Person, error)` | `a.people.Add` |
   | `RenamePerson(ctx, id, name string) error` | `a.people.Rename` |
   | `SetPersonEnabled(ctx, id string, enabled bool) error` | `a.people.SetEnabled` |
   | `RemovePerson(ctx, id string) error` | `a.people.Remove` |
   | `AddKeys(ctx, personID, text string) ([]people.Key, error)` | `a.people.AddKeys` |
   | `ImportKeys(ctx, personID, path string) ([]people.Key, error)` | `a.people.ImportKeys` |
   | `SetKeyComment(ctx, personID, keyID, comment string) error` | `a.people.SetKeyComment` |
   | `RemoveKey(ctx, personID, keyID string) error` | `a.people.RemoveKey` |
   | `ClearAttempts(ctx) error` | `a.attempts.Clear()`, then `return a.attempts.Flush(ctx)` (a list cleared by hand is saved at once) |
   | `RemoveAttempt(ctx, id string) error` | `a.attempts.Remove` |
   | `Disconnect(ctx, id string) error` | `a.server.Disconnect` |
   | `DisconnectAll()` | `a.server.DisconnectAll` |
   | `NewHostKey(ctx) error` | see task 2 |

   Do not send `Changed` yourself for these: `App.open` already subscribes to the stores and the server, so the event comes by itself. Do not disconnect anybody yourself either: the server listens to the people store and closes the connections of a person who was renamed, disabled or removed (SPEC §7.1).
2. `NewHostKey`: `key, err := hostkey.Create(ctx, <the folders>)`; on an error return it. Then store the key in the field `Server()` reads the fingerprint from (look at `Server()` and guard the write with the same mutex), call `a.server.SetHostKey(key.Signer())`, and call `a.changed(TopicServer)`. Connections that are open stay open; the next connection gets the new key.
3. Add the pure function `CountConnections(conns []server.Connection, personID, keyID string) int`: with an empty `keyID` it counts the connections whose `PersonID` is `personID`; otherwise those whose `KeyID` is `keyID`. The UIs use it for "this will close 2 connections".

**Tests** (in `commands_test.go`; use `newFixture(t, nil)`, `f.open(nil)`, `f.person`, `f.connect`, `waitGone` and `alive` of `app_test.go`)

- `TestPeopleCommands`: add `alice`; `People()` has her, enabled, without keys. Add a key (`sshtest.NewKey(t, sshtest.Ed25519).AuthorizedKey`); set its comment; rename her to `bob`; disable; remove the key; remove the person; `People()` is empty. After the first command, advance the fake clock by `CoalesceTime` and assert that a `Changed` with `TopicPeople` arrived.
- `TestImportKeys`: write a file with two public keys under `t.TempDir()`; `ImportKeys` adds two keys.
- `TestCommandErrors`, a table. Each row runs one command and asserts the route with `errtest.Route(t, err, "App.<Method>", "<Tag>")`:
  `AddPerson("-bad")` → `PersonNameInvalid`; `AddPerson` of a name that exists, in another case (`Alice`) → `PersonNameTaken`; `RenamePerson("p_0000000000000000", "x")` → `PersonNotFound`; `AddKeys` with `""` → `KeyTextEmpty`; `AddKeys` of a key that another person has → `KeyAlreadyAssigned`; `RemoveKey` with an unknown key id → `KeyNotFound`; `RemoveAttempt("a_0000000000000000")` → `AttemptNotFound`; `Disconnect(ctx, "nobody")` → `ConnectionNotFound`.
- `TestChangingAPersonDisconnectsThem`, a table with four rows (rename, disable, remove the person, remove the key): a connected client is gone afterwards (`waitGone`), and `Connections()` is empty.
- `TestDisconnect`: two people connected. `Disconnect` with the first connection's ID closes only that one (`alive` on the other); `DisconnectAll` closes the other.
- `TestClearAndRemoveAttempts`: one accepted connection and one dial with a name nobody has (`sshtest.DialErr`) give two entries. `RemoveAttempt` leaves one; `ClearAttempts` leaves none, `Attempts().LastClearedAt` equals the fake clock's `Now()`, and `attempts.json` on disk has no entry.
- `TestNewHostKey`: remember the old public key (`a.hostKey.Signer().PublicKey()`) and connect a client. After `NewHostKey`: `Server().HostKeyFingerprint` changed; the old client is still `alive`; a new dial that expects the **old** key fails (`sshtest.DialErr` with `sshtest.ClientConfig(name, oldKey, key)`); a dial that expects the new key works; on Linux the private key file has mode `0600`.
- `TestCountConnections`, a table: no connection, by person, by key.
- `TestScenarioWithOpenSSH` in `scenario_linux_test.go` (it starts `/bin/sh`, so it is for Linux; start with `sshtest.Require(t)`). Log one `t.Logf` line after each stage, because the proof is this test's output:
  1. Open the app; `AddPerson("alice")`; `AddKeys` with a new key. Log the `Server().Command("alice", "127.0.0.1")` line.
  2. `o := sshtest.NewOpenSSH(t, "127.0.0.1:<port>", hostPublicKey)`; `o.SSH("", "alice", key, nil, "echo", "hello")` exits 0 and prints `hello`. `Attempts().Entries` has one entry: `accepted`, `alice`, count 1.
  3. `stop := o.StartMaster("alice", key)`; wait until `Connections()` has one connection of `alice`; log its address.
  4. `Disconnect` with its ID; wait until `Connections()` is empty; call `stop()`.
  5. `o.SSH("", "mallory", key, nil, "true")` exits 255; the list has a second entry: `unknownName`, login name `mallory`.
  6. `ClearAttempts`; the list is empty.

**Proof** (T): `scripts/transcript.sh docs/implemented-proof-images/4.2-app.txt -- go test -v -count=1 -run TestScenarioWithOpenSSH ./internal/app`. Open the file: it must show the six stages.

**Finish**: rules 10–15.

### Step 4.3: Events for notifications, and messages

**Goal**: the application says when somebody connected or was refused, and turns every error a UI can get into a sentence.

**Read**: SPEC §10 (Events, `app.Message`), §6 (`notify.*`), §8.3. `internal/app/events.go`. `internal/archtest/errortags.go` (`ScanTags`, `TagReport`, `TagUse`).

**Files**: `internal/core/attempts/attempts.go` and its test; `internal/app/notify.go`, `notify_test.go`, `messages.go`, `messages_test.go` (all new).

**Tasks**

1. **`attempts.List.SubscribeRecords(listener func(Entry)) (cancel func())`**: the listener is called for every attempt that `Record` counts, with a copy of the entry as it is after the count, on the goroutine that called `Record`, after the list's lock is released. A stash may hold it already: if `git stash list` shows `step 4.2: attempts.SubscribeRecords (paused)`, run `git stash pop` on your branch and read what it adds. Otherwise write it like `Subscribe` of the same file: a second slice of listeners under `listenersMu`, copied before the calls, and a `cancel` that makes a new slice instead of changing the old one.
   Tests in the attempts package: the listener gets one entry per `Record`, with `Count` 1, then 2 for the same group; after `cancel` it gets nothing; a listener that calls its own `cancel` inside the call does not deadlock.
2. **The events** (in `notify.go`):
   ```go
   // Accepted is sent when somebody connected.
   type Accepted struct{ Person, Address string }

   // Rejected is sent when an attempt was refused, at most once a minute.
   type Rejected struct{ Entry attempts.Entry }
   ```
   Make both an `Event` the way `Changed` is one (look at `events.go`).
3. **Sending them.** `SubscribeRecords` calls on a connection's goroutine, and a subscriber (a UI) may be slow, so never call a subscriber from there:
   - In `App.open`, make a channel `chan Event` with room for 64 events and start one goroutine that reads it and calls every subscriber (take the list of subscribers the way `send` in `events.go` does: copy under the lock, call outside it).
   - The `SubscribeRecords` listener makes the event (`accepted` → `Accepted{Person: entry.PersonName, Address: entry.Address}`, any other outcome → `Rejected{Entry: entry}`) and puts it in the channel with `select { case ch <- event: default: }`. A full channel drops the event; a notification is not worth blocking a connection.
   - `Rejected` at most once a minute: keep the time of the last one (under a mutex); queue a `Rejected` only when none was queued yet or `clock.Now()` is at least one minute later. Use the app's `Clock`, never `time.Now`.
   - `Close` cancels the subscription, closes the channel and waits for the goroutine, before it releases the lock. Nothing is sent after `Close`.
   - The settings `notify.accepted` and `notify.rejected` are **not** looked at here; the desktop UI looks at them (step 6.8).
4. **`Message(err error) string`** in `messages.go`: the sentence a UI shows for an error.
   - `nil` → `""`.
   - Not an `*errors.Error` (`errors.FromError` says no) → `sanitize.Display(err.Error(), 200)`.
   - A Tag that is in the table → its sentence, with the placeholders filled in.
   - Any other Tag → `sanitize.Display(appErr.Message, 200)`.

   The table is a package-level `var messages = map[string]string{...}` (it is never changed after that). A sentence may hold placeholders: `{name}` is the metadata value with that key (`fmt.Sprint`, then `sanitize.Display(…, 120)`; a key the error does not have gives `?`), and `{cause}` is the text of `appErr.OriginalErr` (sanitized; empty when there is none). Write sentences for a person who does not know SSH: what happened, and what to do when there is something to do. Examples:

   | Tag | Sentence |
   |---|---|
   | `PersonNameInvalid` | `A name has 1 to 32 letters, digits, dots, underscores or hyphens, and does not start with a dot or a hyphen.` |
   | `PersonNameTaken` | `There is already a person named {takenBy}.` |
   | `KeyAlreadyAssigned` | `This key ({fingerprint}) already belongs to {owner}.` |
   | `KeyIsPrivate` | `This is a private key. Paste the public key instead: the file that ends in .pub.` |
   | `KeyTooWeak` | `Line {line}: an RSA key needs at least {minBits} bits.` |
   | `ListenFailed` | `Cave cannot listen on {address}: {cause}. Choose another port in Settings.` |
   | `AlreadyRunning` | `Cave is already running ({program}, process {pid}).` |
   | `RunningAsRoot` | `Cave does not run as root. Start it as your own user.` |

   The metadata keys in this table are from memory. For every sentence you write, open the place that makes the error (`scripts/error-tags.sh` prints `Tag file:line function`) and use the keys that are really there.
5. **Which Tags need a sentence**: every Tag that is made in `internal/app` or in `internal/core/attempts`, `hostkey`, `logging`, `people`, `server`, `settings` or `storage` (about 77). The Tags of `session`, `sftp`, `forward`, `agentfwd`, `pty` and `netinfo` never reach a UI (they are logged), and those of `cmd/` are printed by the programs themselves.

**Tests**

- `notify_test.go` (events arrive on another goroutine: collect them in a channel and wait with a real timeout of a few seconds, not with the fake clock):
  - `TestAcceptedIsSent`: a person connects → `Accepted{"alice", "127.0.0.1"}`.
  - `TestRejectedAtMostOnceAMinute`: three dials with an unknown name → exactly one `Rejected` (wait 200 ms of real time to see that no second one comes); `f.clock.Advance(time.Minute)`; one more dial → a second `Rejected`.
  - `TestASlowSubscriberDoesNotBlockTheServer`: a subscriber that blocks on a channel; 200 calls of `a.attempts.Record(...)` return within a second; then unblock it.
  - `TestNothingIsSentAfterClose`: after `Close`, `a.attempts.Record(...)` sends nothing and does not panic.
- `messages_test.go`:
  - `TestMessage`, a table of real errors made by real commands on a fixture app (so that the metadata is real), with the exact sentence expected. At least: a bad name, a taken name, a key that is already assigned (the sentence has the owner's name and the fingerprint), a private key, an unknown setting, a port in use (`ListenFailed`: listen on the port with `net.Listen("tcp", "127.0.0.1:0")` first), `nil`, a plain error that is not from the package (`io.EOF`; the standard library's `errors` package may not be imported), and a Tag that is not in the table.
  - `TestMessageIsSafe`: an `*errors.Error` whose metadata value holds `"\x1b[31m"` gives a sentence without `\x1b`.
  - `TestEveryTagHasAMessage`: `archtest.ScanTags(<the module's root>)`; for every Tag whose file is under the folders of task 5, fail when `messages` has no entry; and fail for every entry of `messages` whose Tag no longer exists. (The test file may import `internal/archtest`; the boundary rules are about non-test files.)
  - `TestMessagesHaveNoLeftoverPlaceholder`: in the sentences that `TestMessage` got, there is no `{` and no `?` that the sentence itself did not have.

**Finish**: rules 10–15.

### Step 4.4: Words both UIs share

**Goal**: the words that the TUI and the desktop app both show (results, times, what a connection has open, the settings' labels) are written once, in `internal/app`.

**Read**: SPEC §6, §8.1, §9.3, §9.8 (who can reach a forwarded agent), §11.3, §11.6.

**Files**: `internal/app/words.go`, `settings_text.go`, `words_test.go`, `settings_text_test.go` (all new).

**Tasks** (every function is pure: no clock, no I/O; `now` is an argument)

1. `ResultText(o attempts.Outcome) string`: `accepted` → `Accepted`, `unknownName` → `Unknown name`, `wrongKey` → `Wrong key`, `noKey` → `No key offered`, `notProven` → `Key not proven`, `disabled` → `Disabled person`, `handshakeFailed` → `Not SSH or broken handshake`, `refused` → `Too many connections`; any other value → the value itself through `sanitize.Display(…, 32)`.
2. `Who(e attempts.Entry) string`: `e.PersonName`, or `Unknown` when it is empty.
3. `Ago(now, t time.Time) string`: `never` for a zero time; `just now` under 1 minute (also when `t` is after `now`); `3 min ago` under 1 hour; `2 h ago` under 24 hours; `5 d ago` under 30 days; else the date, `2026-11-21`. Round down.
4. `Until(now, t time.Time) string`: `now` when `t` is not after `now`; `in 12 min` under 1 hour (at least `in 1 min`); `in 3 h` under 24 hours; `in 4 d` from then on. Round up, so that a time that is still ahead never shows as `in 0 min`.
5. `ActivitiesText(activities []server.Activity) string`: the activities joined with `, `, in the order shell, exec, sftp, local forwards, remote forwards, agent: `shell`, `exec`, `sftp`, `forward to 127.0.0.1:5432` (a local forward with its `Detail`), `listening on 127.0.0.1:8080` (a remote forward with its `Detail`), `agent`. Several of a kind without a detail are counted: `2 shells`, `3 execs`. No activity → `idle`.
6. `QuitQuestion(conns []server.Connection) string`: counts the different people (`PersonID`): `1 person is connected. Quit and disconnect them?`, `3 people are connected. Quit and disconnect them?`; `""` when nobody is connected.
7. `settings_text.go`:
   ```go
   // SettingText is how the UIs present one setting.
   type SettingText struct {
       Key, Group, Label, Help string
       // DesktopOnly settings do nothing in the TUI.
       DesktopOnly bool
   }

   // SettingTexts returns the text of every setting, in the order the UIs show them.
   func SettingTexts() []SettingText
   ```
   The groups, in this order: `Server`, `Attempts list`, `Sessions`, `File transfer and forwarding`, `Notifications`, `Appearance`. Every key of `settings.Schema()` is there once. `Help` is one sentence in the user's words that says what SPEC §6 says. For `forwarding.agent` it must say who can use a forwarded agent: `While a person is connected with ssh -A, the programs of this computer's user and of the other connected people can use that person's agent.` (SPEC §9.8). `DesktopOnly` is true for `notify.accepted`, `notify.rejected`, `ui.theme` and `ui.closeToTray`.

**Tests**

- Tables for each function. `Ago` and `Until` at every boundary: 0 s, 59 s, 60 s, 59 min 59 s, 1 h, 23 h 59 min, 24 h, 29 d, 30 d, a time in the future, a zero time. `ResultText` for the eight outcomes and an unknown one that holds a control character (it comes back clean). `ActivitiesText` for none, one of each kind, two shells and a forward. `QuitQuestion` for none, one person, one person with two connections (still `1 person`), three people.
- `TestEverySettingHasText`: every key of `settings.Schema()` has exactly one `SettingText` with a group of the list, a label and a help that ends with a full stop; no `SettingText` has a key that the schema lacks.

**Finish**: rules 10–15. No proof.

### Step 4.5: Test support for the UIs

**Goal**: one package gives both UIs the two things their tests need: a fake application with fixed content (for golden files, which must be the same on every run) and a real application in a temporary folder (for flows).

**Why two**: a real application has random ids, random key fingerprints and a port the OS chose, so its pictures change from run to run. SPEC §14.3 allows both.

**Read**: SPEC §14.1, §14.3; `internal/app/app_test.go` (its `fixture`); `internal/core/server/servertest` (how a test-support package of non-test files is written and how `internal/archtest` allows it).

**Files**: `internal/app/apptest/apptest.go`, `fake.go`, `sample.go`, `real.go`, `apptest_test.go` (all new). `internal/app/version.go` (task 6).

**Tasks**

1. `Application`: an interface with every exported method of `*app.App` that a UI calls: the five snapshots (`Server`, `People`, `Attempts`, `Connections`, `Settings`), `Subscribe`, `StartServer`, `StopServer`, `Set`, and the thirteen methods of step 4.2 (`CountConnections` is a function, not one of them). Add `var _ Application = (*app.App)(nil)`, so that the two cannot drift apart.
2. `Fake`: a struct that implements `Application` without doing anything real.
   ```go
   type Fake struct {
       ServerSnapshot   app.ServerSnapshot
       PeopleList       []people.Person
       AttemptsSnapshot app.AttemptsSnapshot
       ConnectionList   []server.Connection
       SettingsSnapshot settings.Snapshot
       Clock            *clock.Fake
       // Fail makes the command with that method name return the error.
       Fail map[string]error
       // has unexported fields: a mutex, the calls, the subscribers
   }
   ```
   - A snapshot method returns its field. A command appends a line to the list of calls (`AddPerson("alice")`, `Set("server.port", 2200)`, `Disconnect("c1")`) and returns `Fail["AddPerson"]` (nil when there is none). A command does **not** change the fields; a test that wants new content changes the fields and calls `Emit`.
   - `Calls() []string` returns the calls so far. `Emit(event app.Event)` calls every subscriber. `Subscribe` works like the real one. `Subscribers() int` says how many subscribers there are now (a UI's test uses it to see that the UI cancelled its subscription).
   - It is safe for concurrent use (a mutex), because a UI calls commands off its UI thread.
3. `Now`: `time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)`. `Sample(t testing.TB) *Fake` returns a fake with this content, always the same:
   - **Server**: `Running`, `Port` 2222, `Listening` `["[::]:2222"]`, `HostKeyFingerprint` `SHA256:` and 43 fixed characters, `Addresses` `["192.0.2.10", "2001:db8::10", "127.0.0.1"]`.
   - **People** (fixed ids such as `p_00000000000000a1`): `alice`, enabled, two keys (`ssh-ed25519` with the comment `laptop`, `ssh-rsa` with the comment `work desktop`); `bob`, disabled, one key without a comment; `carol`, enabled, no key. Key fingerprints are fixed strings (`SHA256:` and 43 characters); `Public` stays nil. Times are `Now` minus some days.
   - **Connections**: `alice` from `198.51.100.7`, since `Now` minus 12 minutes, with a `shell` and a `local-forward` whose `Detail` is `127.0.0.1:5432`.
   - **Attempts**: nine entries, newest first: one for each of the eight outcomes of SPEC §8.1, plus a second `unknownName` whose login name is hostile text as the store would have saved it: write it as `"root\uFFFD[31mX"` (an escape character already replaced; archtest rule 9 forbids invisible characters in Go files, so always write such text with escapes). Vary the rest: counts 1, 3 and 1204; an IPv6 address; a login name of 64 characters that ends in `…`; a client version; one entry with two key fingerprints and a `KeyOwner` (`bob`). `LastClearedAt` is `Now` minus 3 days and `NextClearAt` is `Now` plus 4 days.
   - **Settings**: the defaults with `attempts.clear` = `weekly`. A `settings.Snapshot` cannot be built by hand: open a `settings.Store` in `storagetest.Isolate(t)`, set the value, take `Snapshot()`.
   - **Clock**: `clock.NewFake(Now)`.
   Add `Empty(t testing.TB) *Fake`: server `Stopped`, port 2222, no people, no connections, no attempts, default settings.
4. `Open(t testing.TB, more map[string]any) *Real`: a real `*app.App` for flow tests. Do what `newFixture` and `open` of `app_test.go` do (copy the code; that file cannot be imported): `storagetest.Isolate(t)`; save `server.address` = `127.0.0.1`, `server.port` = a port the OS gave (listen on `127.0.0.1:0`, read the port, close), `session.shell` = `/bin/sh`, `server.keepAliveSeconds` = 0, then the settings of `more`; open with a fake clock, a logger into a buffer, `IsRoot` false, fixed `Addresses`, a `User` whose home is under the temporary folder; close in `t.Cleanup`.
   `Real` has the fields `App *app.App`, `Clock *clock.Fake`, `Dirs storage.Dirs`, `Port int`, and the methods `Addr() string`, `Person(name string) sshtest.Key` (adds the person with a new key through the commands), `Connect(name string, key sshtest.Key) *ssh.Client` (closed in `t.Cleanup`), `Refuse(name string, key sshtest.Key)` (a dial that must fail; it leaves an attempt), `Eventually(what string, cond func() bool)` (polls for up to 5 seconds of real time, then `t.Fatalf` with `what`).
5. If `internal/archtest` complains about the new package, give it the same exception `servertest` has, and add `internal/app/apptest` to the table of SPEC §4.1 (it is there already if this plan's branch added it).
6. **This is the last step of M4.** After the step's commit, in a second commit `Set the development version to 0.6.0-dev`, change `devVersion` in `internal/app/version.go` (and the test that names it). Then merge, tag `step-4.5`, and publish the milestone as `v0.5.0` (rule 14).

**Tests** (`apptest_test.go`)

- `TestSampleIsTheSameEveryTime`: two calls of `Sample` give equal people, attempts, connections and server snapshots (`reflect.DeepEqual`).
- `TestSampleHasEveryOutcome`: the eight outcomes are there; no time is after `Now`; no string of the sample holds a control character.
- `TestFakeRecordsCalls`: commands are listed by `Calls()` in order; `Fail` makes a command return its error; `Emit` reaches a subscriber and not one that was cancelled.
- `TestOpenGivesARunningApplication`: `Open`; the server runs on `127.0.0.1`; `Person` then `Connect` works; `Refuse` adds an entry; after the test nothing listens (the cleanup closed it).

**Finish**: rules 10–15, then the publish of M4.

---

## M5: TUI

**Read this before any step of M5.** These facts about the libraries were checked on 2026-11-24 with `charm.land/bubbletea/v2` v2.1.2 and `charm.land/lipgloss/v2` v2.1.1. Still open the source when you use something that is not listed here.

- Import `tea "charm.land/bubbletea/v2"` and `"charm.land/lipgloss/v2"`. The module `github.com/charmbracelet/x/ansi` comes with them and may be imported under `internal/tui`.
- A model has `Init() tea.Cmd`, `Update(tea.Msg) (tea.Model, tea.Cmd)` and `View() tea.View`. Make the view with `v := tea.NewView(text)` and set `v.AltScreen = true`. Leave `v.MouseMode` at its zero value (the mouse stays off, SPEC §12). Bracketed paste is on unless `DisableBracketedPasteMode` is set; leave it on.
- A key press is a `tea.KeyPressMsg`. Compare `msg.String()` with these names: `"a"`, `"C"` (a capital letter), `"?"`, `"/"`, `"enter"`, `"esc"`, `"tab"`, `"shift+tab"`, `"space"`, `"backspace"`, `"up"`, `"down"`, `"pgup"`, `"pgdown"`, `"home"`, `"end"`, `"ctrl+c"`. The text a key types is `msg.Text` (empty for keys such as Enter).
- In a test, make key presses like this: `tea.KeyPressMsg{Code: 'a', Text: "a"}`, `tea.KeyPressMsg{Code: 'C', Text: "C", Mod: tea.ModShift}`, `tea.KeyPressMsg{Code: tea.KeyEnter}`, `tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}`, `tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}`, `tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}`.
- The size of the terminal comes as `tea.WindowSizeMsg{Width, Height}`; pasted text as `tea.PasteMsg{Content}`; the color profile as `tea.ColorProfileMsg{Profile}`.
- `tea.Quit` is a command; it produces `tea.QuitMsg`. `tea.Batch(cmds...)` produces `tea.BatchMsg`, which is a `[]tea.Cmd`.
- `lipgloss.NewStyle()...Render(s)` **always** writes escape sequences with full colors. Bubble Tea's renderer removes what the terminal cannot show: with `NO_COLOR` set it keeps bold and reverse and drops the colors; when the output is not a terminal it drops every style. So `NO_COLOR` needs no code of yours. What you must do: never write an escape sequence by hand, and never let color be the only sign of something (the selected row also has a `>` in front, the current page is also in brackets, a state is also a word).
- Width is measured in terminal cells: `lipgloss.Width(s)` (it skips escape sequences and counts a wide character as 2). `ansi.Truncate(s, width, "…")` cuts to a width. `ansi.Strip(s)` removes the escape sequences.
- A program for a test runs without a terminal: `tea.NewProgram(model, tea.WithInput(strings.NewReader("q")), tea.WithOutput(&buffer), tea.WithWindowSize(100, 30), tea.WithoutSignalHandler())`, and `Run()` returns when the model quits. `tea.WithColorProfile(colorprofile.TrueColor)` forces colors (`github.com/charmbracelet/colorprofile`).

**How the TUI is built** (every step of M5 follows this):

- **One model, plain data.** The root `Model` holds the snapshots (`server`, `people`, `attempts`, `connections`, `settings`), read again from the app each time a `changedMsg` arrives, plus the size, the current page, and each page's own small state (the selected row, an open field). `View` only draws what the model holds; it never calls the app.
- **A command never runs inside `Update`.** A key that changes something returns a `tea.Cmd` that calls the app and returns `doneMsg{err error}`; `Update` shows `app.Message(err)` on the message line when it arrives. So the terminal never freezes on a slow disk.
- **No command of the model may wait.** No `tea.Tick`, no sleep. Time comes from outside: `Run` starts a goroutine with a ticker of the clock that sends `tickMsg{}` every 30 seconds (for "3 min ago"). This is what makes the test helper below possible.
- **The test helper** (`internal/tui/harness_test.go`, written in step 5.1) sends a message and then every message the returned commands produce, as the program would:
  ```go
  func (h *harness) send(msg tea.Msg) {
      h.t.Helper()
      queue := []tea.Msg{msg}
      for len(queue) > 0 {
          msg, queue = queue[0], queue[1:]
          switch msg := msg.(type) {
          case nil:
              continue
          case tea.QuitMsg:
              h.quit = true
              continue
          case tea.BatchMsg:
              for _, cmd := range msg {
                  if cmd != nil {
                      queue = append(queue, cmd())
                  }
              }
              continue
          }
          var cmd tea.Cmd
          h.model, cmd = h.model.Update(msg)
          if cmd != nil {
              queue = append(queue, cmd())
          }
      }
  }
  ```
  with the shortcuts `h.key("a")`, `h.keys("a", "enter")`, `h.typeText("alice")` (one key press per character), `h.paste(text)`, `h.resize(w, h)`, `h.screen() string` (`ansi.Strip` of the view's content) and `h.golden(name)`.
- **Golden files** are text: `internal/tui/testdata/goldens/<name>.txt` holds `h.screen()`. The helper defines its own flag in a test file: `var updateGoldens = flag.Bool("update-goldens", false, "rewrite the golden files")`. Update with `go test ./internal/tui -run Golden -update-goldens` (name the package: a package without the flag fails when it gets it). Every test that compares a golden has `Golden` in its name. After an update, open every changed file and read it.
- **Golden tests use `apptest.Sample(t)` or `apptest.Empty(t)`**, never a real application (step 4.5 says why). Flow tests that must see a real effect use `apptest.Open(t, nil)`.
- **The screen** (width `W`, height `H`; every line is cut to `W` cells with `…` and never wraps; the view has exactly `H` lines):
  ```
  line 0        Cave   running on port 2222                    1 connected · 9 attempts
  line 1        [1 Overview]  2 People  3 Attempts  4 Connections  5 Settings
  line 2        ────────────────────────────────────────────── (W cells)
  line 3…H-4    the page (H-6 lines)
  line H-3      ────────────────────────────────────────────── (W cells)
  line H-2      the message line: an error or a notice, else empty
  line H-1      the keys of the page:  a add   enter open   ? keys   q quit
  ```
  The words for the server's state: `running on port N`, `stopped`, `starting…`, `stopping…`, and `not running` when `Server().Err` is set (the reason is on the Overview page).
- **Hostile text.** Everything from a client is already made safe when it is stored. Draw stored strings as they are; never pass them through `fmt.Sprintf` as a format, and never build a style from them.

### Step 5.1: The TUI's frame and the program

**Goal**: `tcave` opens the application and shows the frame: header, page names, footer, the five pages (empty for now), the keys that change page, `s`, `?`, `q`. A script captures its screen as a proof.

**Read**: SPEC §12, §10 (Flags), §15. The two lists above. `cmd/tcave/main.go` as it is. `scripts/isolated.sh` and `scripts/transcript.sh` (the style of Cave's scripts).

**Files**: `internal/tui/app.go`, `model.go`, `frame.go`, `keys.go`, `styles.go`, `run.go`; tests `harness_test.go`, `frame_test.go`, `run_test.go`; `cmd/tcave/main.go` and its test; `scripts/tui-proof.sh`, `scripts/freeport.go`; `scripts/testdata/two-people/` (the fixture of the proofs of both programs); `internal/app/flags.go` (the flag `--page`); `internal/archtest` (the rules for the new packages, if they name packages).

**Tasks**

1. Add the libraries: `go get charm.land/bubbletea/v2@v2.1.2 charm.land/lipgloss/v2@v2.1.1`, then `go mod tidy`. If archtest's rule 8 lists the allowed modules, they are there already; if it fails, look at what it expects before changing it.
2. `app.go`: `type App interface { ... }` with the methods of `apptest.Application` that the TUI uses (in this step: `Server`, `People`, `Attempts`, `Connections`, `Settings`, `Subscribe`, `StartServer`, `StopServer`; later steps add theirs).
3. `keys.go`: the pages (`pageOverview` … `pageSettings`, with the names `Overview`, `People`, `Attempts`, `Connections`, `Settings`) and **one** table of key bindings: for each page (and for "everywhere") a list of `{keys, what}` such as `{"a", "add a person"}`. The footer and the help overlay are both drawn from this table, so they cannot disagree. Later steps add their page's rows.
4. `styles.go`: the Lip Gloss styles in one struct (title, current page, selected row, dim text, error, the good and the bad state). Use bold and reverse for "current" and "selected"; use color only as an addition.
5. `model.go`: `Model` and `NewModel(ctx context.Context, a App, clk clock.Clock, opts Options) Model` (`ctx` is the one the commands run with; `Options` is the struct of task 7).
   - `Init` returns nil.
   - `Update` handles: `tea.WindowSizeMsg` (remember the size); `changedMsg` and `tickMsg` (read the five snapshots again); `doneMsg` (set the message line to `app.Message(err)`, or clear it when `err` is nil); `tea.KeyPressMsg`.
   - Keys everywhere: `1`–`5` go to a page; `tab` and `shift+tab` go to the next and the previous page, around the ends; `s` returns a command that calls `StopServer` when the state is `Running` or `Starting`, else `StartServer`; `?` opens the help overlay (any key closes it); `q` and `ctrl+c` return `tea.Quit` (the question when people are connected comes in step 5.2). Any key press clears the message line first.
6. `frame.go`: `View` draws the screen described above. The page's part is, for now, one line with the page's name. Below 60 columns or 16 lines the whole view is the one line `Terminal too small (needs 60×16)`. The help overlay replaces the page's part with every binding of the table, in two groups: "Everywhere" and the current page.
7. `run.go`: `Run(ctx context.Context, a App, opts Options) error` with `Options{In io.Reader; Out io.Writer; Env []string; Clock clock.Clock; Page string; ConfigDir, StateDir, Version string}` (nil fields mean the standard input, the standard output, the process's environment and the system clock; `Page` is the page shown first, empty for the Overview; the last three are shown on the Settings page in step 5.8).
   - Make the program with `tea.WithContext(ctx)`, `tea.WithInput`, `tea.WithOutput`, `tea.WithEnvironment` (only for the fields that are set).
   - Subscribe to the app: the listener does `go program.Send(changedMsg{})` (`Send` waits when the program is not running yet, and the app's goroutine must not wait). Cancel the subscription when `Run` returns.
   - Start a goroutine with `clock.NewTicker(30 * time.Second)` that sends `tickMsg{}`; stop it when `Run` returns.
   - An error of `program.Run()` is returned as `errors.NewErr("TuiRunFailed", err)`; a `ctx` that was cancelled is not an error (return nil). Test the Tag's route (a reader that fails is one way; if no test can make `Run` fail, look at what `program.Run` returns for a cancelled context and decide with the code in front of you).
8. `cmd/tcave/main.go`: replace the `TODO(step 5.1)`:
   - Parse with `app.ParseFlags("tcave", args)`. `FlagsHelp` → print `app.Usage("tcave")` on standard output, exit status 0. Any other error of the flags → `tcave: <app.Message(err)>` on standard error and exit status 2. `--version` → print the version as now.
   - `opts, err := flags.Options("tcave", os.LookupEnv)`; `a, err := app.Open(ctx, opts)`; an error → `tcave: <app.Message(err)>` on standard error, exit status 1.
   - `tui.Run(ctx, a, tui.Options{Page: flags.Page, ConfigDir: …, StateDir: …, Version: app.Version()})`, then `a.Close` with a context of 10 seconds, also when `Run` failed.
   - `ctx` is cancelled by `SIGINT`, `SIGTERM` and `SIGHUP` (`signal.NotifyContext`; `SIGHUP` only where it exists: put that list in `signals_linux.go` and `signals_windows.go`).
   - Remove the Tags the old `main` had and that nothing uses now (`TcaveUsage`, `TcaveUnexpectedArgument`, `TcaveNotBuiltYet`) together with their tests; `scripts/error-tags.sh --untested` must stay empty.
9. The flag `--page <name>` in `app.Flags` (SPEC §10, both programs): `Flags.Page string`, one of `overview`, `people`, `attempts`, `connections`, `settings`; empty when it is not given. Another value is the error `FlagsPageInvalid` (metadata `page`), with its route test, its line in `app.Usage` and its sentence in `app.Message` (`There is no page named {page}. The pages are overview, people, attempts, connections and settings.`). Export the list of names from `internal/app` (`app.Pages`), so that both UIs use the same.
10. `scripts/freeport.go`: a Go file with `//go:build ignore` and `package main` that listens on `127.0.0.1:0`, prints the port and exits. Scripts call it with `go run scripts/freeport.go`.
11. The proof fixture `scripts/testdata/two-people/`: `settings.json` with `"server.address": "127.0.0.1"` and `"session.shell": "/bin/sh"` (a session of a proof runs `/bin/sh` in the isolated home, never the user's shell); `people.json` with `alice` (enabled) and `bob` (disabled), each with one public key; a host key (`ssh_host_ed25519_key` and its `.pub`); `client-keys/alice` and `client-keys/mallory` (private keys for the proofs of later steps). Make the keys with `ssh-keygen -t ed25519 -N '' -C test -f <file>` inside the fixture. Add a `README` that says: these keys were generated for the proofs and tests, are used nowhere else, and protect nothing.
12. `scripts/tui-proof.sh` (method U). Usage:
    ```
    scripts/tui-proof.sh <out.txt> [--config <fixture dir>] [--size 100x30] [--during '<command>'] [--keys '<tmux keys>' | --text '<text>' | --wait '<regex>']... -- [tcave args]
    ```
    What it does, in this order:
    1. Builds `tcave` into a temporary folder (`go build -o`).
    2. Copies the fixture (or makes one with only a `settings.json` that sets `server.address` to `127.0.0.1`) into a temporary folder. It **refuses to run** when the fixture's `settings.json` does not set `server.address` to `127.0.0.1` or `::1`: a proof never listens on the network.
    3. Gets a port from `go run scripts/freeport.go`.
    4. Starts a private tmux server, with `TMUX_TMPDIR` set to the script's temporary folder and `-f /dev/null`, so that the user's own tmux and its configuration are never touched: `tmux -L cave-proof-$$ -f /dev/null new-session -d -x <cols> -y <rows> -- timeout -k 2 120 scripts/isolated.sh --config <copy> -- <tcave> --port <port> <tcave args>`.
    5. Polls `tmux -L … capture-pane -p` until the screen has the word `Cave` (10 seconds at most, else it fails and prints the screen).
    6. With `--during`: runs the command in the background, under `timeout -k 2 60`, in its own process group, with `CAVE_PROOF_PORT` (the port) and `CAVE_PROOF_FIXTURE` (the copy) in its environment.
    7. Goes through the `--keys` and `--wait` arguments **in the order they were given**: `--keys` sends tmux key names (`2`, `Enter`, `C-d`, separated by spaces) with `tmux send-keys` and waits 0.3 seconds; `--text` types its value as it is (`send-keys -l`); `--wait` polls until a line of the screen matches the regular expression (`grep -E`; 15 seconds at most, else it fails and prints the screen).
    8. Waits 0.5 seconds.
    9. Saves `capture-pane -p` to `<out.txt>`.
    10. Always, also after a failure (a `trap`): kills the `--during` process group, sends `q` and `y`, runs `tmux -L … kill-server`, removes the temporary folders.
    The port is different on every run, so the file is too; a proof is looked at, not compared. Follow the style of `scripts/transcript.sh` (the header comment, `set -euo pipefail`, exit status 64 for a usage mistake). Check the script with `shellcheck` if it is installed.

**Tests**

- `frame_test.go` with `apptest.Sample(t)` and `apptest.Empty(t)`:
  - `TestGoldenFrame`, a table: `{sample, 100×30}`, `{sample, 60×16}`, `{empty, 100×30}`, `{sample with Server().State = Stopped, 100×30}`, `{sample with Server().Err set, 100×30}`. Goldens `frame-<name>.txt`.
  - `TestGoldenHelp`: `?` at 100×30; a second key closes it.
  - `TestTooSmall`: 59×16 and 60×15 show only `Terminal too small (needs 60×16)`; 60×16 shows the frame.
  - `TestPageKeys`, a table: `1`…`5`, `tab` from the last page goes to the first, `shift+tab` from the first goes to the last. Assert the model's current page.
  - `TestStartStopKey`: with the sample (running), `s` → the fake's `Calls()` is `["StopServer()"]`; with the empty fake (stopped), `s` → `["StartServer()"]`; with `Fail["StartServer"]` set to a `ListenFailed` error, the message line shows `app.Message` of it.
  - `TestEveryLineFits`: for widths 60, 80, 100 and 137 and heights 16, 30 and 50, on every page: the view has exactly `H` lines and no line is wider than `W` (`lipgloss.Width`).
  - `TestChangedReadsTheSnapshotsAgain`: change the fake's `ConnectionList`, send `changedMsg{}`; the header's count follows.
  - `TestQuitKeys`: `q` and `ctrl+c` set `h.quit`.
  - `TestTheFirstPage`: `Options.Page` = `attempts` starts on the Attempts page; empty starts on the Overview.
- `run_test.go`:
  - `TestRunWritesOnlyToItsWriter`: `Run` with `In: strings.NewReader("q")`, `Out: &buffer`, the sample fake, under a `ctx` with a timeout of 10 seconds. It returns nil; the buffer is not empty; nothing was written to `os.Stdout` or `os.Stderr` (replace them with pipes for the time of the test and read what arrived: nothing).
  - `TestRunEndsWhenTheContextIsCancelled`: an input that never ends (`io.Pipe`), cancel the context, `Run` returns within 5 seconds.
  - `TestRunSendsTheAppsEvents`: `In` is an `io.Pipe`; after `fake.Emit(app.Changed{...})` with a changed `ConnectionList`, the output eventually holds the new count; then write `q`.
- `cmd/tcave`: `--version`, `--help` (exit status 0, the usage on standard output), an unknown flag (exit status 2, a sentence on standard error), a state folder that is a file (exit status 1, a sentence). Run `run(...)` with `--config-dir` and `--state-dir` under `t.TempDir()`, never the real folders.

**Proof** (U):
```
scripts/tui-proof.sh docs/implemented-proof-images/5.1-frame.txt --config scripts/testdata/two-people
scripts/tui-proof.sh docs/implemented-proof-images/5.1-help.txt  --config scripts/testdata/two-people --keys '?'
```
`5.1-frame.txt` must show the header with `running on port`, the five page names and the footer; `5.1-help.txt` the list of keys. Afterwards `pgrep -a tmux` and `pgrep -a tcave` show nothing of yours.

**Finish**: rules 10–15.

### Step 5.2: The TUI's widgets

**Goal**: the small parts every page is made of, each tested alone. No page uses them yet, except the quit question.

**Read**: SPEC §12 (the last three points), §8.3.

**Files**: `internal/tui/text.go`, `list.go`, `table.go`, `field.go`, `area.go`, `question.go`, each with its `_test.go`. `model.go` (the quit question).

**Rule for all of them**: a widget is a small struct with `Update(msg tea.KeyPressMsg)` (or a method per key) and `View(width, height int) string`. It never calls the app and never returns a command. `View` returns exactly `height` lines, none wider than `width`.

**Tasks**

1. `text.go`: `fit(s string, width int) string` cuts `s` to `width` cells with `…` as the last cell when it was longer, and fills with spaces to exactly `width` cells. `clean(s string) string` makes pasted or typed text safe for one line: every control character (C0, DEL, C1), newline and tab becomes a space; invalid UTF-8 becomes `U+FFFD`.
2. `list.go`: `list` keeps a number of rows, the selected row and the first visible row. Methods: `setLen(n)` (keeps the selection inside), `key(name string) bool` for `up`/`k`, `down`/`j`, `pgup`, `pgdown`, `home`, `end` (true when it used the key), `selected() int`, `window(height int) (first, last int)` (the rows to draw, moved so that the selected one is visible).
3. `table.go`: `table{columns []column, rows [][]string}` with `column{title string, width int, flex bool, right bool}`. Fixed columns keep `width` cells; the flexible ones share what is left (at least 4 cells each); every cell goes through `fit`; one space between columns; `right` aligns numbers to the right. `View(width, height, list)` draws the title line, then the visible rows; the selected row starts with `>` and is drawn in the "selected" style, the others start with a space. An empty table draws the title line and the text it was given for "nothing here".
4. `field.go`: `field`, a one-line text input: the text as runes and a cursor. Keys: a typed character (`msg.Text`) is inserted (after `clean`); `left`, `right`, `home`/`ctrl+a`, `end`/`ctrl+e`, `backspace`, `delete`, `ctrl+u` (empty it), `ctrl+w` (delete the word before the cursor). `paste(s)` inserts `clean(s)`. `limit` is the largest number of runes (more is not taken). `View(width)` shows the part of the text around the cursor and draws the cursor with the reverse style; an empty field shows its placeholder in the dim style.
5. `area.go`: `area`, a multi-line input for public keys: a list of lines and a cursor (line, column). Keys as the field, plus `enter` (a new line), `up`, `down`. `paste(s)` splits at `\n` (after turning `\r\n` and `\r` into `\n`) and cleans each line. At most 200 lines and 16 KiB in all; more is not taken. `View(width, height)` shows the lines around the cursor, each cut with `fit` (a key is longer than the screen: it is cut for the eye, the text is whole). `text() string` joins the lines with `\n`.
6. `question.go`: `question{text string, yes string}`, drawn as a box in the middle of the page's part: the text, then `[y] <yes>   [n] Cancel`. `y` or `Y` answers yes; `n`, `N`, `esc` and `enter` answer no (the default is **no**: Enter never destroys anything). Any other key does nothing.
7. The model gets one place for an open question: a field `ask *asking` with the question and the command to run on yes. While it is open, it gets every key. Use it now for quitting: `q` or `ctrl+c` with at least one connection opens the question `app.QuitQuestion(connections)` (step 4.4) with `yes` = `Quit`; without connections it quits at once. A second `ctrl+c` while the question is open quits.

**Tests** (table-driven, one file per widget)

- `fit`: shorter, equal, longer; wide characters (`"日本語"` in 5 cells is `"日本…"`); a combining character; width 0 and 1.
- `clean`: an escape sequence, a newline, a tab, invalid UTF-8.
- `list`: every key at the top, the middle and the bottom; `setLen` smaller than the selection; `window` keeps the selection visible; an empty list.
- `table`: a golden of a table with a flexible column at 60 and at 100 columns; a cell longer than its column ends in `…`; right-aligned numbers; the empty text.
- `field`: typing, every editing key, the limit, a paste with control characters (they become spaces), the view when the text is longer than the width (the cursor is visible).
- `area`: a paste of three lines with `\r\n`; the limits; `text()`.
- `question`: `y` is yes; `n`, `esc`, `enter` are no; another key is nothing; a golden.
- `TestQuitQuestion`: with the sample fake (one connection) `q` shows the question and does not quit; `n` closes it; `q`, `y` quits; with the empty fake `q` quits at once; `q`, `ctrl+c` quits.
- `TestEveryLineFits` of step 5.1 still passes with the question open.

**Finish**: rules 10–15. No proof.

### Step 5.3: The Overview page

**Goal**: page 1 shows everything of SPEC §11.1.

**Read**: SPEC §11.1, §10 (`Server()`).

**Files**: `internal/tui/page_overview.go`, `page_overview_test.go`.

**Tasks**

1. Draw the page from the model's snapshots, in this order, one thing per line, with an empty line between the groups:
   ```
   Server      running on port 2222                      (s stops it)
   Host key    SHA256:…                                  (what a person sees at the first connection)

   How to connect, for a person named alice:
     ssh -p 2222 alice@192.0.2.10
     ssh -p 2222 alice@2001:db8::10
     ssh -p 2222 alice@127.0.0.1

   People 3   Connected now 1   Attempts in the list 9
   ```
   - The lines come from `Server().Command(name, address)` for each of `Server().Addresses`; `name` is the first enabled person's name, or the word `name` when there is none.
   - When the server is stopped: `Server      stopped                    (s starts it)`.
   - When `Server().Err` is set: `Server      not running`, and under it `app.Message(err)` in the error style, cut to the width over at most two lines.
   - When there are no people: a line `Nobody can connect yet. Press 2 to add a person.`
   - When the page is too short for everything, the address lines are the ones left out (show the first, then `… and 2 more`).
2. Add the page's row to the key table: `s` start or stop the server.

**Tests**

- `TestGoldenOverview`, a table at 100×30 and 60×16: the sample; the empty fake; stopped; the error (`ListenFailed`); starting.
- `TestOverviewCommandLines`: the lines equal `Command(...)` of each address; port 22 is not written (set the port to 22 in the fake).

**Proof** (U): `scripts/tui-proof.sh docs/implemented-proof-images/5.3-overview.txt --config scripts/testdata/two-people`. It must show the running server, the fingerprint and the `ssh` lines with `alice`.

**Finish**: rules 10–15.

### Step 5.4: The People page: the list

**Goal**: page 2 lists the people and can add, rename, enable, disable and remove them.

**Read**: SPEC §12 (People), §11.2, §7.1.

**Files**: `internal/tui/page_people.go`, `page_people_test.go`; `app.go` (the interface gets `AddPerson`, `RenamePerson`, `SetPersonEnabled`, `RemovePerson`).

**Tasks**

1. The table: `Name` (flexible), `Keys` (a number, right), `Enabled` (`yes` or `no`), `Connected` (the number of connections of that person, empty for 0). The rows are in the order of `People()`. The empty text: `Nobody yet. Press a to add a person.`
2. Keys (add them to the key table):
   - `a`: a field `Name of the new person:` opens on the message line's place. `enter` runs `AddPerson`; `esc` closes it. When the command fails, the field **stays open** with the text, and the sentence (`app.Message`) is on the line above it. When it works, the field closes and the new person is selected.
   - `e`: the same field, with the current name in it: `RenamePerson`.
   - `space`: `SetPersonEnabled` with the opposite of now. Disabling a person who is connected asks first: `alice is connected (1 connection). Disable and disconnect?`.
   - `d`: the question `Remove alice and their 2 keys? This closes 1 connection.` (leave out the last sentence when there is no connection; count with `app.CountConnections`), `yes` = `Remove` → `RemovePerson`.
   - `enter`: opens the person (step 5.5); for now it does nothing.
   - The list's keys move the selection.
3. While a field or a question is open, it gets every key: the page keys and the keys for everywhere (`1`–`5`, `s`, `q`, `?`) must **not** work then, because the user is typing a name. `ctrl+c` still quits.
4. Keep the selection on the same person (by ID) when the list changes under it.

**Tests**

- `TestGoldenPeople`: the sample at 100×30 and 60×16; the empty fake; the add field open; the add field with an error above it; the remove question.
- With the fake: `a`, type `dave`, `enter` → `Calls()` is `["AddPerson(dave)"]`; with `Fail["AddPerson"]` = a `PersonNameTaken` error, the field stays open with `dave` in it and the sentence is shown; `esc` closes without a call.
- `TestTypingANameDoesNotChangePage`: with the field open, the keys `1`, `s`, `q`, `?` go into the field.
- With `apptest.Open` (the real application): add `alice` with the keys → `People()` has her; `e` renames; `space` disables; `d`, `y` removes. A connected person: `d` shows `This closes 1 connection.`, `y` removes and the client is gone (`Eventually`).
- `TestPeopleKeepsTheSelection`: select the second person, put a new person first in the fake's list, send `changedMsg{}`; the same person is selected.

**Proof** (U): `scripts/tui-proof.sh docs/implemented-proof-images/5.4-people.txt --config scripts/testdata/two-people --keys 2`.

**Finish**: rules 10–15.

### Step 5.5: The People page: a person's keys

**Goal**: `enter` on a person shows their keys, and keys can be added (by paste or from a file), commented and removed.

**Read**: SPEC §12 (People), §7.2.

**Files**: `internal/tui/page_person.go`, `page_person_test.go`; `app.go` (`AddKeys`, `ImportKeys`, `SetKeyComment`, `RemoveKey`).

**Tasks**

1. `enter` on the People page opens the person: the first line is `alice   enabled   1 connection`, then the table of keys: `Type`, `Fingerprint` (flexible), `Comment`, `Added` (the date, `2026-11-21`). The empty text: `No key yet, so alice cannot connect. Press a to add one.` `esc` goes back to the list. When the person disappears (removed elsewhere), go back to the list.
2. Keys (add them to the key table as their own group, "A person"):
   - `a`: the multi-line `area` opens over the page's part with the title `Paste public keys (one per line), then press ctrl+d. esc cancels.` Pasted text arrives as `tea.PasteMsg`; give it to the area. `ctrl+d` runs `AddKeys` with the area's text. An error keeps the area open with the sentence above it (`Line 2: …` errors carry the line's number).
   - `i`: a field `Path of a .pub or authorized_keys file:` → `ImportKeys`. Expand a leading `~/` with the user's home folder (`os.UserHomeDir`), nothing else.
   - `c`: a field with the comment of the selected key → `SetKeyComment`.
   - `d`: the question `Remove this key (SHA256:…)? This closes 1 connection.` → `RemoveKey`.
3. The rule of step 5.4 task 3 holds here too: an open field or area gets every key and every paste.

**Tests**

- `TestGoldenPerson`: `alice` of the sample (two keys) at 100×30 and 60×16; `carol` (no key); the area open with two lines; the area with the error of a private key above it.
- With the fake: a paste of two keys then `ctrl+d` → one call of `AddKeys` whose text has both lines; a paste that holds an escape sequence reaches the call cleaned; `i`, a path, `enter` → `ImportKeys`; `c` → `SetKeyComment`; `d`, `y` → `RemoveKey`; `d`, `enter` → no call.
- A table with the real application (`apptest.Open`): for each error of SPEC §7.2 that a paste can cause (`KeyTextEmpty`, `KeyParseFailed`, `KeyIsPrivate`, `KeyTypeUnsupported`, `KeyTooWeak`, `KeyOptionsUnsupported`, `KeyAlreadyAssigned`), the page shows exactly `app.Message` of that error and the person has no new key. Make the bad inputs in the test (a private key with `sshtest.NewKey(...)`'s file, a 1024-bit RSA key with `crypto/rsa`); never use a key of the user.
- With the real application: add a key by paste, then connect with it (`Connect`); remove the key with `d`, `y`; the client is gone.

**Proof** (U):
```
scripts/tui-proof.sh docs/implemented-proof-images/5.5-person-keys.txt   --config scripts/testdata/two-people --keys 2 --keys Enter
scripts/tui-proof.sh docs/implemented-proof-images/5.5-add-key-error.txt --config scripts/testdata/two-people --keys 2 --keys Enter --keys a --text 'not a key' --keys C-d
```
The second must show the sentence for text that is not a key.

**Finish**: rules 10–15.

### Step 5.6: The Attempts page

**Goal**: page 3 shows who tried to connect and how many times, and can filter, search, show details, remove and clear.

**Read**: SPEC §12 (Attempts), §11.3, §8.

**Files**: `internal/tui/page_attempts.go` and its test; `app.go` (`ClearAttempts`, `RemoveAttempt`); `scripts/proof-ssh.sh`.

**Tasks**

1. The words come from `internal/app` (step 4.4): `app.Who`, `app.ResultText`, `app.Ago`, `app.Until`. `now` is always the model's clock, never `time.Now`.
2. The table: `Who` (`app.Who`), `Login name` (flexible), `Address` (flexible), `Result` (`app.ResultText`), `Count` (right), `Last` (`app.Ago`). Below 80 columns leave `Login name` out. The order is the snapshot's (newest first). The empty text: `Nobody tried to connect yet.`, or `Nothing matches.` when a filter or search hides everything.
3. The first line of the page: `Filter: All   Search: —   Cleared 3 d ago · next clear in 4 d` (`never cleared` for a zero time; the "next" part only when `NextClearAt` is not zero).
4. Keys (add them to the key table):
   - `f`: the filter goes All → Accepted → Refused → All. "Refused" is every outcome but `accepted`.
   - `/`: a field `Search:`; the table follows each key; it matches who, the login name and the address, ignoring case. `enter` keeps the search and closes the field; `esc` clears it.
   - `enter`: details of the selected entry in place of the table: every field of SPEC §8.2 on its own line (first and last as full times in the local zone of the clock, the client version, each key fingerprint, the key owner as `This key belongs to bob.`). `esc` or `enter` goes back.
   - `d`: `RemoveAttempt` of the selected entry, without a question (one entry is cheap to lose).
   - `C` (capital): the question `Clear the list of 9 attempts?` → `ClearAttempts`.
5. Keep the selection on the same entry (by ID) when the list changes; an attempt that arrives while the user reads must not move the row under the cursor.
6. `scripts/proof-ssh.sh`, for the proofs of this and later steps (it runs inside `--during` of the proof scripts). Usage: `scripts/proof-ssh.sh <login name> <client key name> [ssh arguments and command]`. It reads `CAVE_PROOF_PORT` and `CAVE_PROOF_FIXTURE`, makes a temporary folder with a `known_hosts` that holds the fixture's host key for `[127.0.0.1]:<port>`, copies the client key there with mode `0600`, and runs
   `ssh -F none -i <key> -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=<known_hosts> -o GlobalKnownHostsFile=/dev/null -p <port> <login name>@127.0.0.1 <the rest>` with `HOME` set to the temporary folder and `SSH_AUTH_SOCK` unset. It never turns host key checking off, and never reads anything of the user's `~/.ssh`.

**Tests**

- `TestGoldenAttempts`: the sample at 100×30, 80×24 and 60×16; the empty fake; the filter on Refused; a search for `198.51`; the details of the entry with two keys and a key owner; the clear question.
- `TestHostileLoginNameIsDrawnAsText`: the stripped screen of the sample holds `root\uFFFD[31mX` exactly as stored, and the row after it is drawn in its normal place (the text did not move the cursor or change a color).
- With the fake: `f` three times comes back to All; `d` → `RemoveAttempt(<id>)`; `C`, `y` → `ClearAttempts()`; `C`, `enter` → no call.
- With the real application: `Person`, `Connect`, two `Refuse` with a wrong name → the table shows two rows, the refused one with count 2; `C`, `y` → the list is empty.
- `TestAttemptsKeepsTheSelection`: as for people.

**Proof** (U), made with real attempts from OpenSSH:
```
scripts/tui-proof.sh docs/implemented-proof-images/5.6-attempts.txt --config scripts/testdata/two-people \
  --during 'scripts/proof-ssh.sh alice alice true; scripts/proof-ssh.sh alice mallory true; scripts/proof-ssh.sh nobody mallory true; scripts/proof-ssh.sh nobody mallory true' \
  --keys 3 --wait 'Unknown name +2 '
```
It must show three rows: `alice` accepted once, `alice` with a wrong key once, an unknown name twice. (`--wait` here waits for the row of the unknown name to reach the count 2; if your columns are in another order, write the expression for what your row really shows.)

**Finish**: rules 10–15.

### Step 5.7: The Connections page

**Goal**: page 4 shows who is connected now and can disconnect them.

**Read**: SPEC §12 (Connections), §11.4, §9.3.

**Files**: `internal/tui/page_connections.go` and its test; `app.go` (`Disconnect`, `DisconnectAll`).

**Tasks**

1. The table: `Person`, `Address` (flexible), `Since` (`app.Ago`, e.g. `12 min ago`), `Open` (flexible; `app.ActivitiesText`). The empty text: `Nobody is connected.`
2. Keys (add them to the key table): `d` asks `Disconnect alice (198.51.100.7)?` → `Disconnect` with the connection's ID; `D` asks `Disconnect all 3 connections?` → `DisconnectAll` (it does nothing when nobody is connected).
3. Keep the selection on the same connection (by ID).

**Tests**

- `TestGoldenConnections`: the sample at 100×30 and 60×16; the empty fake; three connections with every kind of activity (change the fake's `ConnectionList`); the question.
- With the fake: `d`, `y` → `Disconnect(<id>)`; `D`, `y` → `DisconnectAll()`; `D` with nobody connected → no question.
- With the real application: two clients connect; the page shows two rows (`Eventually`, sending `changedMsg{}` between looks); `d`, `y` closes the selected client and not the other.

**Proof** (U), with a live session:
```
scripts/tui-proof.sh docs/implemented-proof-images/5.7-connections.txt --config scripts/testdata/two-people \
  --during 'scripts/proof-ssh.sh alice alice sleep 30' --wait '1 connected' --keys 4
```
It must show `alice` with `exec`. Afterwards `pgrep -a ssh` shows nothing of yours.

**Finish**: rules 10–15.

### Step 5.8: The Settings page

**Goal**: page 5 shows every setting and changes it; it also shows the host key, the folders and the version. This is the last step of M5.

**Read**: SPEC §12 (Settings), §11.5, §6, §9.8 (who can reach a forwarded agent), §9.10.

**Files**: `internal/tui/page_settings.go` and its test; `app.go` (`Set`, `NewHostKey`); `run.go` and `cmd/tcave` (the folders and the version reach the model through `tui.Options`).

**Tasks**

1. The labels, the explanations, the groups and their order come from `app.SettingTexts()` (step 4.4).
2. The page is a list of rows, group titles between them (a title cannot be selected): `label   value`, and under the list the explanation of the selected row. Values are drawn by kind: a boolean as `on` or `off`, a number as it is, a choice as its word, a text as it is (`—` when empty), a list joined with `, `.
3. The desktop-only settings (`SettingText.DesktopOnly`) are drawn in the dim style with `(desktop app)` after the value; `enter` on them does nothing.
4. `enter` or `space` on a row (add to the key table):
   - a boolean flips: `Set(key, !value)`;
   - a choice goes to the next of `Def.Choices`, around the end;
   - a number or a text opens a field with the current value; `enter` runs `Set` (a number is parsed with `strconv.Atoi`; text that is not a number shows `Type a number.` and keeps the field open); a list is typed with commas (`LANG, LC_*`) and split, with the spaces around each name removed and empty names left out;
   - an error of `Set` keeps the field open with `app.Message(err)` above it; `esc` cancels.
5. After the settings, three rows that are not settings:
   - `Host key   SHA256:…`: `enter` asks `Make a new host key? Every person will see a warning that the host key changed, the next time they connect.` → `NewHostKey`.
   - `Folders   <config dir> · <state dir>` and `Version   <version>`: not selectable.
6. **Last step of M5**: a second commit sets `devVersion` to `0.7.0-dev`; then merge, tag `step-5.8`, publish as `v0.6.0` (rule 14).

**Tests**

- `TestEverySettingIsOnThePage`: every key of `settings.Schema()` has a row.
- `TestGoldenSettings`: the sample at 100×30 (scrolled to the top, and with the last row selected) and 60×16; a number field open; a number field with the error of a port out of range.
- With the fake, a table with one row per kind: a boolean → `Set("sftp.enabled", false)`; a choice → `Set("attempts.clear", "monthly")` from `weekly`; a number → `Set("server.port", 2200)`; a text; a list → `Set("session.acceptEnv", [LANG LC_*])`; `abc` in a number field → no call and `Type a number.`; a desktop-only row → no call.
- With the real application: change `server.port` to another free port → the header shows it and a client can connect there; a port in use → the sentence of `ListenFailed` and the field stays open; change `attempts.clear` to `hourly` → the first line of the Attempts page says `next clear` with the words `app.Until` gives for one hour; the host key row, `enter`, `y` → the fingerprint on the page changed.

**Proof** (U): `scripts/tui-proof.sh docs/implemented-proof-images/5.8-settings.txt --config scripts/testdata/two-people --keys 5`.

**Finish**: rules 10–15, then the publish of M5.

---

## M6: Desktop

**Read this before any step of M6.** Glutter is the user's own toolkit and has Flutter's API in Go. These names were looked up on 2026-11-24 in v1.17.11 (`go list -m -f '{{.Dir}}' github.com/rm4n0s/glutter`, after step 6.1 added it). They tell you where to look; **open the file before you call anything**, because the options structs are not listed here.

- Packages: `material` (import it as `m`; it also gives the names of `widgets`), `widgets`, `foundation` (`foundation.Opt[T]`, `foundation.DebugMode`), `ui`, `desktop` (tray, notifications, file dialogs), `gluttertest`.
- Start: `m.RunAppContext(ctx, widget, m.RunAppOpts{AppID: ..., Window: m.WindowConfig{Title, Size, MinSize}})`. The root is `m.NewMaterialApp(...)` with a light theme, a dark theme and a `ThemeMode`.
- Widgets Cave uses: `m.NewScaffold`, `m.NewNavigationRail(destinations, selectedIndex, opts)`, `m.NewNavigationBar(destinations, opts)`, `m.NewLayoutBuilder`, `m.NewListView`, `m.NewSingleChildScrollView`, `m.NewListTile`, `m.NewDataTable(columns, rows, opts)`, `m.NewCard`, `m.NewDivider`, `m.NewFilledButton(onPressed, child)`, `m.NewTextButton`, `m.NewIconButton(onPressed, icon)`, `m.NewSwitch(value, onChanged)`, `m.NewSegmentedButton`, `m.NewDropdownMenu`, `m.NewTextField`, `m.NewAlertDialog`, `m.ShowDialog[T](bc, builder) <-chan T`, `m.NewSnackBar` with `m.ScaffoldMessengerOf(bc)`, `m.NewTooltip`, `m.NewBadge`, `m.ThemeOf(bc)`, `m.MediaQueryOf(bc)`.
- From another goroutine, hand work to the UI thread with the binding's `Post(fn)` (`widgets/binding.go`) and call `SetState` there.
- Tests: `gluttertest.New(t, opts...)` gives a `*Tester`: `PumpWidget`, `Pump`, `PumpAndSettle`, `Tap(finder)`, `EnterText(finder, text)`, `SendKeyEvent`, `SetViewSize(ui.Size)`, `MatchesGolden(name)` (a PNG in `testdata/goldens/`, written with `-update-goldens`), `ClipboardText()`, `ExitRequests()`, `FakeDesktop()` (tray, notifications, the file chooser). Finders: `gluttertest.ByText`, `ByKey`, `ByType[T]`, `ByIcon`, `ByTooltip`. Matchers: `FindsOneWidget`, `FindsNothing`.
- Where to read: `docs/GUIDE.md` sections 1–6 (widgets, state, layout, inherited data, navigation and dialogs), 8 (asynchronous data), 9 (text input), 11 (fonts); `examples/navigation`, `examples/todo`, `examples/tray`; `gluttertest/*.go`; and Glutter's own tests of a widget (`material/<widget>_test.go`) show how to find and tap it.
- When Glutter lacks something or has a bug: rule 8 (work around it, `// Glutter:` comment, `docs/GLUTTER-ISSUES.md`). Do not stop for it.

**How the desktop UI is built** (every step of M6 follows this):

- **The root holds the data.** One stateful widget at the top holds the five snapshots and the current page. It subscribes to the app in `InitState` and cancels in `Dispose`. An event does `Post(func() { SetState(read the snapshots again) })`. Pages are widgets that get the data and callbacks as constructor arguments and hold only their own small state (the selected row, the text of a search).
- **A command never runs on the UI thread.** `internal/gui/async.go` has one type for it:
  ```go
  // runner runs a command of the application away from the UI thread and gives its error back on it.
  type runner func(work func(ctx context.Context) error, done func(err error))
  ```
  The real one starts a goroutine and calls `done` through `Post`. Tests use `syncRunner`, which calls `work` and then `done` at once, so a test needs no waiting. Every page gets the runner from the root; no page starts a goroutine itself.
- **Errors**: a dialog shows `app.Message(err)` under the field it belongs to and stays open; a command without a dialog (a switch, a button) shows it in a snack bar.
- **Tests**: golden tests and most widget tests use `apptest.Sample(t)` or `apptest.Empty(t)` with `syncRunner` (fixed content, the same picture every time). A few flow tests per page use `apptest.Open(t, nil)` to see a real effect. Fonts come from `internal/gui/testutil`, never from the system. Golden files are PNG; update them with `go test ./internal/gui/... -run Golden -update-goldens`, then **open every changed PNG with the Read tool and look at it**: text that is cut off, overlapping widgets and a wrong theme are bugs, not goldens.
- **Sizes for goldens**: wide is 960×640, narrow is 720×480 (below 800, so the bottom bar shows). Each page has a golden in light-wide and dark-narrow at least.
- **Keys for tests**: give every button, field and row that a test taps a key (look up Glutter's key type; `gluttertest.ByKey` finds it). Find by key rather than by text when the same words are on the screen twice.
- **Hostile text**: draw stored strings with a text widget as they are. Never use a stored string as a format string or inside markup.

### Step 6.1: The window, the frame and the program

**Goal**: `cave` opens the application and shows a window with the navigation and five empty pages. A script takes a screenshot of it as a proof.

**Read**: SPEC §11 (the first paragraph), §10 (Flags), §14.2. The two lists above. Glutter's `docs/GUIDE.md` 1–6 and 11, `examples/navigation/main.go`, and `scripts/screenshot-headless.sh`. `cmd/tcave/main.go` (the same shape, from step 5.1).

**Files**: `internal/gui/app.go`, `run.go`, `root.go`, `shell.go`, `theme.go`, `async.go`, `start_error.go`; tests `pump_test.go`, `shell_test.go`; `internal/gui/testutil/testutil.go`; `cmd/cave/main.go` and its tests, `cmd/cave/display_test.go`; `scripts/proof.sh`; `scripts/testdata/two-people-dark/`.

**Tasks**

1. `go get github.com/rm4n0s/glutter@v1.17.11` (or the newest v1 release, if `go list -m -versions github.com/rm4n0s/glutter` shows a newer one), then `go mod tidy`. Check that `CGO_ENABLED=0 go build ./...` and `GOOS=windows CGO_ENABLED=0 go build ./...` still pass.
2. `app.go`: `type App interface { ... }` with what this step uses (`Server`, `People`, `Attempts`, `Connections`, `Settings`, `Subscribe`); later steps add theirs.
3. `theme.go`: a light and a dark Material 3 theme from one seed color (a calm blue-grey; look at how `examples/gallery` makes its themes), and `themeMode(settings.Snapshot) m.ThemeMode` from `ui.theme` (`system`, `light`, `dark`).
4. `root.go`: the root widget as described above. `shell.go`: the scaffold. With `m.NewLayoutBuilder`: at 800 logical pixels of width or more, a `NavigationRail` on the left with five destinations (an icon and a label each: Overview, People, Attempts, Connections, Settings) and the page beside it; below 800, the page with a `NavigationBar` at the bottom. Each page is, for now, its name as a headline. The Connections destination has a badge with the number of connections when it is not 0.
5. `async.go`: `runner`, the real one and `syncRunner` (in a test file).
6. `run.go`: `Run(ctx context.Context, a App, opts Options) error` with `Options{Page string; ConfigDir, StateDir, Version string}`. Window: title `Cave`, 960×640, at least 720×480, application ID `io.github.rm4n0s.cave`. `Page` (`overview`, `people`, `attempts`, `connections`, `settings`; empty is `overview`) is the page shown first (the flag `--page` of step 5.1). An error of Glutter's run is `errors.NewErr("GuiRunFailed", err)`, with a route test if a test can make it fail; else list the Tag in `docs/ERROR-TAGS-UNTESTED.md` with the reason.
7. `start_error.go`: `RunStartError(ctx, message string) error` opens a small window (480×240) with the title `Cave could not start`, the message and a `Quit` button. A desktop app has no terminal, so this is how the user learns that Cave is already running or that a file is broken.
8. `cmd/cave/main.go`: replace the `TODO(step 6.1)`. The same as `tcave` (step 5.1 task 8), with these differences: an error of `app.Open` is printed on standard error **and** shown with `gui.RunStartError(ctx, app.Message(err))`; then exit status 1. Remove the Tags of the old `main` that nothing uses any more, with their tests.
9. `internal/gui/testutil`: `NewTester(t testing.TB, size ui.Size) *gluttertest.Tester` makes a tester with the test fonts, the fake clock, and that size. Find how Glutter's own golden tests get their fonts (`grep -rn MatchesGolden` in Glutter's `material/` tests, and GUIDE section 11) and do the same; when the fonts must be files in Cave, put them in `internal/gui/testutil/fonts/` with their license file. This package must **not** import `internal/gui` (the tests of `internal/gui` import it; that would be a cycle).
10. `pump_test.go` in `internal/gui`: `pump(t, a App, size ui.Size, page string) *gluttertest.Tester` builds the root with `syncRunner` and pumps it until it settles.
11. `scripts/testdata/two-people-dark/`: a copy of `scripts/testdata/two-people/` (step 5.1) whose `settings.json` also has `"ui.theme": "dark"`. Say in its `README` that the keys are the same test keys.
12. `scripts/proof.sh` (method D). Start from a copy of Glutter's `scripts/screenshot-headless.sh` (keep the note that says where it is from, as Cave's other copied scripts do). Usage:
    ```
    scripts/proof.sh <out.png> [--config <fixture dir>] [--size 960x640] [--during '<command>'] [--after <seconds>] -- [cave args]
    ```
    It builds `cave` (`go build -o <tmp>/cave ./cmd/cave`), copies the fixture and refuses one that does not listen on loopback (as `scripts/tui-proof.sh` does), gets a port from `go run scripts/freeport.go`, and runs `scripts/isolated.sh --config <copy> -- <tmp>/cave --port <port> <cave args>` inside `scripts/headless-wayland.sh` with the output's size set to `--size` (the script it was copied from shows how: `HEADLESS_SIZE`) and software rendering (`WLR_RENDERER=pixman`; look at how Glutter's script keeps the application off the GPU, and keep that). When the window is mapped it runs `--during` in the background (with `CAVE_PROOF_PORT` and `CAVE_PROOF_FIXTURE`, under `timeout -k 2 60`, in its own process group), waits `--after` seconds (default 1), saves the screen with `grim`, and stops everything it started, also after a failure. Everything runs under `timeout -k`. It needs `sway`, `grim` and `jq` (all installed on this machine; if one is missing, rule 9: give the install command and stop).

**Tests**

- `shell_test.go`:
  - `TestGoldenShell`: the sample at 960×640 in the light theme and at 720×480 in the dark theme (set `ui.theme` in the fake's settings). Goldens `shell-light-wide.png`, `shell-dark-narrow.png`.
  - `TestNavigation`: tapping each destination shows that page's headline; at 720×480 the rail is not there (`FindsNothing` for its type) and the bar is; at 960×640 the opposite.
  - `TestTheFirstPage`: `Options.Page` = `attempts` shows the Attempts headline.
  - `TestTheThemeFollowsTheSetting`: change `ui.theme` in the fake and `Emit(app.Changed{Topics: app.TopicSettings})`; after a pump the theme's brightness changed (read it with `m.ThemeOf` from a widget of the tree, or compare a golden).
  - `TestTheBadgeFollowsTheConnections`: change the fake's `ConnectionList`, `Emit`; the badge shows the new number.
  - `TestDisposeCancelsTheSubscription`: pump an empty widget in place of the root; the fake's `Subscribers()` is 0 afterwards.
- `cmd/cave`: `--version`, `--help`, an unknown flag (exit status 2), as for `tcave`.
- **Display test** (`cmd/cave/display_test.go`, first line `//go:build cave_display`): build `cave` into `t.TempDir()`; start it with `CAVE_CONFIG_DIR` and `CAVE_STATE_DIR` under `t.TempDir()` and the flags `--no-start`, in its own process group; wait 3 seconds; send `SIGTERM`; it must exit with status 0 within 10 seconds, and `cave.log` must hold no line of level `ERROR`. Kill the group in `t.Cleanup`. Run it with
  `scripts/heavy.sh --alone -- scripts/headless-wayland.sh -- go test -race -tags 'cave_display debug_mode' ./cmd/cave`.

**Proof** (D):
```
scripts/proof.sh docs/implemented-proof-images/6.1-window.png --config scripts/testdata/two-people
scripts/proof.sh docs/implemented-proof-images/6.1-window-dark-narrow.png --config scripts/testdata/two-people-dark --size 720x480
```
Open both: the first shows the rail on the left in the light theme; the second the bottom bar in the dark theme. Afterwards `pgrep -a sway` and `pgrep -a cave` show nothing of yours.

**Finish**: rules 10–15, with the display test.

### Step 6.2: The Overview page

**Goal**: the first page shows everything of SPEC §11.1.

**Read**: SPEC §11.1, §10 (`Server()`). Step 5.3 (the same content in the TUI).

**Files**: `internal/gui/page_overview.go`, `page_overview_test.go`; `app.go` (`StartServer`, `StopServer`).

**Tasks**

1. The page, top to bottom, in a scroll view with a column that is at most 720 pixels wide:
   - A card for the server: the state in words (`Running on port 2222`, `Stopped`, `Starting…`, `Stopping…`, `Not running`) with a colored dot, and one big filled button: `Stop server` when the state is running or starting, else `Start server`. It runs `StopServer` or `StartServer` through the runner and is disabled while that runs.
   - When `Server().Err` is set: in the same card, `app.Message(err)` in the error color.
   - A card `Host key`: the fingerprint in a monospace style, a copy button (tooltip `Copy`), and the line `A person sees this the first time they connect.`
   - A card `How to connect`: for each address, the line `Server().Command(name, address)` in a monospace style with a copy button. `name` is the first enabled person's name, else the word `name`.
   - Three numbers in a row: `People`, `Connected now`, `Attempts in the list`.
   - When there are no people: a card `Nobody can connect yet.` with the button `Add a person`, which goes to the People page.
2. A copy button puts the text on the clipboard (look up Glutter's clipboard call in `services`) and shows the snack bar `Copied`.

**Tests**

- `TestGoldenOverview`: the sample in light-wide and dark-narrow; the empty fake in light-wide; the error state (`Fail` is not needed: set `ServerSnapshot.Err` to a `ListenFailed` error) in light-wide.
- `TestStartAndStop`: with the sample, tapping `Stop server` gives `Calls()` = `["StopServer()"]`; with the empty fake, `Start server` gives `["StartServer()"]`; with `Fail["StartServer"]` set, a snack bar shows the sentence.
- `TestCopyButtons`: tapping the copy button of the fingerprint puts exactly the fingerprint on the tester's clipboard (`ClipboardText()`); the same for the first `ssh` line.
- `TestAddAPersonGoesToPeople`: with the empty fake, the button shows the People page.
- With the real application (`apptest.Open`, the real runner replaced by `syncRunner`): `Stop server` stops it (`Eventually` the state is `Stopped` and the page says so after a pump).

**Proof** (D): `scripts/proof.sh docs/implemented-proof-images/6.2-overview.png --config scripts/testdata/two-people`.

**Finish**: rules 10–15.

### Step 6.3: The People page: the list

**Goal**: the People page lists the people and can add, rename, enable, disable and remove them.

**Read**: SPEC §11.2, §7.1. Step 5.4. GUIDE section 6 (dialogs and their results) and 9 (text fields).

**Files**: `internal/gui/page_people.go`, `dialogs.go`, their tests; `app.go` (`AddPerson`, `RenamePerson`, `SetPersonEnabled`, `RemovePerson`, `AddKeys`).

**Tasks**

1. `dialogs.go`, two helpers every later page uses:
   - `confirm(bc, title, text, yes string) <-chan bool`: an alert dialog with `Cancel` and the `yes` button (in the error color when it destroys something). Enter does **not** choose `yes`.
   - A text dialog: a title, one text field with a label, `Cancel` and an action button. The action gets the text and returns an `error`; when it is not nil, the dialog stays open and shows `app.Message(err)` under the field (the text field's error text); when it is nil, the dialog closes. The action button is disabled while the action runs.
2. The page at 800 pixels or more: two panes. Left (320 pixels): the list of people and, above it, the button `Add person`. Right: the selected person (step 6.4 fills it; for now the name as a headline and the buttons `Rename` and `Remove`). Below 800: only the list; tapping a person pushes the person's pane as its own page with a back button.
3. A row of the list: the name, under it `2 keys` (`1 key`, `No key`), a mark `connected` when `app.CountConnections` is not 0, and a switch for enabled. The switch runs `SetPersonEnabled`; turning it off for a connected person asks first: `alice is connected. Disable and disconnect?`.
4. `Add person`: a dialog with two fields: `Name`, and `Public keys (optional)`, a field of several lines for pasting. It runs `AddPerson`; when keys were pasted, then `AddKeys` for the new person. An error of the name shows under the name field; an error of the keys shows under the keys field, and the person that was added stays (say so in the dialog: `alice was added. The keys were not:` and the sentence).
5. `Rename`: the text dialog with the current name → `RenamePerson`. `Remove`: `confirm` with `Remove alice and their 2 keys? This closes 1 connection.` (the last sentence only when there is one) → `RemovePerson`.
6. With no people: the text `Nobody yet.` and the button in the middle of the page.
7. Keep the selection on the same person (by ID) when the list changes; when that person is gone, select nobody.

**Tests**

- `TestGoldenPeople`: the sample in light-wide and dark-narrow; the empty fake; the add dialog open with an error under the name field.
- With the fake: add `dave` → `Calls()` = `["AddPerson(dave)"]`; with `Fail["AddPerson"]` = `PersonNameTaken`, the dialog stays and shows the sentence; `Cancel` makes no call; a switch → `SetPersonEnabled(<id>, false)`; rename; remove with `Cancel` (no call) and with `Remove`.
- With the real application: add a person with a pasted key; they are in `People()` with one key. A person who is connected: `Remove` says `This closes 1 connection.`, and after it the client is gone.
- `TestNarrowPushesThePerson`: at 720×480, tapping a person shows their pane with a back button; back shows the list.

**Proof** (D): `scripts/proof.sh docs/implemented-proof-images/6.3-people.png --config scripts/testdata/two-people -- --page people`.

**Finish**: rules 10–15.

### Step 6.4: The People page: a person's keys

**Goal**: the pane of a person shows their keys; keys can be added by paste or from a file, commented and removed.

**Read**: SPEC §11.2, §7.2. Step 5.5. Glutter's `desktop` package: the file chooser (`desktop.OpenFiles`) and `gluttertest`'s `FakeDesktop` for it.

**Files**: `internal/gui/person_pane.go` and its test; `app.go` (`ImportKeys`, `SetKeyComment`, `RemoveKey`).

**Tasks**

1. The pane: the name as a headline, `Enabled`/`Disabled` and `1 connection`, the buttons `Rename` and `Remove` (step 6.3), then `Keys` with the buttons `Add keys` and `Import from file…`, then one row per key: the type, the fingerprint (monospace, cut in the middle with `…` when the pane is narrow, the whole of it in a tooltip), the comment (`No comment` in a dim style), `Added 2026-11-21`, and two icon buttons: edit the comment, remove.
2. `Add keys`: a dialog with a field of several lines (`Paste public keys, one per line`) → `AddKeys`. Errors under the field; a `Line 2: …` sentence is shown as it is.
3. `Import from file…`: the system's file dialog through Glutter's `desktop` package (one file); then `ImportKeys` with the path. An error shows in a snack bar. When the desktop has no file dialog (Glutter says so with an error), show the text dialog `Path of the file` instead.
4. Edit the comment: the text dialog → `SetKeyComment`. Remove: `confirm` with `Remove this key? SHA256:… This closes 1 connection.` → `RemoveKey`.
5. With no key: `No key yet, so alice cannot connect.`

**Tests**

- `TestGoldenPerson`: `alice` of the sample in light-wide and dark-narrow; `carol` (no key); the add dialog with the sentence of a private key under the field.
- With the fake: each of the five commands is called with the right arguments; the import uses the path the fake file chooser answers (`FakeDesktop()`), and makes no call when the chooser is cancelled.
- With the real application, a table of the errors of SPEC §7.2 as in step 5.5: the dialog shows exactly `app.Message` of the error, and the person has no new key.
- With the real application: add a key, connect with it, remove the key; the client is gone.

**Proof**: (G) copy the golden of the add dialog with the error to `docs/implemented-proof-images/6.4-add-key-error.png` (`cp internal/gui/testdata/goldens/<name>.png …`) and look at it.

**Finish**: rules 10–15.

### Step 6.5: The Attempts page

**Goal**: the table of who tried to connect and how many times, with the filter, the search, the details, remove and clear.

**Read**: SPEC §11.3, §8. Step 5.6. Glutter's `material/data_table.go` and its test.

**Files**: `internal/gui/page_attempts.go` and its test; `app.go` (`ClearAttempts`, `RemoveAttempt`).

**Tasks**

1. Above the table, one row: a segmented button `All`, `Accepted`, `Refused`; a search field (`Search who, login name or address`); on the right the button `Clear list`.
2. Under it, one line: `Cleared 3 d ago · next clear in 4 d` (`app.Ago`, `app.Until`; `Never cleared`; the "next" part only when there is one) and a text button `Change…` that goes to the Settings page.
3. The table (`m.NewDataTable`) with the columns `Who`, `Login name`, `Address`, `Result`, `Count` (numeric), `First`, `Last`. `Who` is `app.Who(entry)`; `Result` is `app.ResultText`; `First` and `Last` are `app.Ago`. Tapping a column's title sorts by it; tapping again turns the order around. The default is `Last`, newest first. The table scrolls in both directions when it does not fit. Show at most the first 500 rows that match, and under the table `Showing 500 of 5000. Search to see others.` (5 000 rows of widgets are too many).
4. Tapping a row selects it and shows the details beside the table (under it below 800 pixels): the first and last time in full, the client version, the key fingerprints, `This key belongs to bob.`, and the button `Remove` → `RemoveAttempt` (no question).
5. `Clear list`: `confirm` with `Clear the list of 9 attempts?` → `ClearAttempts`.
6. With no entries: `Nobody tried to connect yet.`; with a filter or a search that hides all: `Nothing matches.`
7. Keep the selection on the same entry (by ID).

**Tests**

- `TestGoldenAttempts`: the sample in light-wide and dark-narrow; the empty fake; a row selected (the entry with two keys and a key owner).
- `TestHostileLoginNameIsDrawnAsText`: the sample's hostile login name is found as text exactly as stored (`ByText("root\uFFFD[31mX")`).
- Filter (Accepted shows 1 row, Refused 8), search (`198.51` shows the rows of that address; the search ignores case), both together.
- Sorting: by `Count` the first row is the one with 1204 after two taps; by `Who`.
- With the fake: `Remove` → `RemoveAttempt(<id>)`; `Clear list` then `Cancel` → no call; then `Clear` → `ClearAttempts()`; `Change…` shows the Settings page.
- `TestOnlyTheFirstRowsAreBuilt`: a fake with 5 000 entries builds the page in under 2 seconds and shows the line `Showing 500 of 5000`.
- With the real application: one accepted connection and two refused dials show two rows with the counts 1 and 2.

**Proof** (D), with real attempts from OpenSSH:
```
scripts/proof.sh docs/implemented-proof-images/6.5-attempts.png --config scripts/testdata/two-people --after 4 \
  --during 'scripts/proof-ssh.sh alice alice true; scripts/proof-ssh.sh alice mallory true; scripts/proof-ssh.sh nobody mallory true; scripts/proof-ssh.sh nobody mallory true' \
  -- --page attempts
```
It must show three rows: `alice` accepted, `alice` with a wrong key, an unknown name with the count 2.

**Finish**: rules 10–15.

### Step 6.6: The Connections page and quitting

**Goal**: who is connected now, with disconnect; and the question before Cave quits while people are connected.

**Read**: SPEC §11.4, §11.6 (the last point), §9.3. Step 5.7. Glutter's `widgets.AppLifecycleListener` (`OnExitRequested`, `ui.AppExitResponse`) and how `gluttertest` asks for an exit (`ExitRequests()` and what is near it in `gluttertest/tester.go`).

**Files**: `internal/gui/page_connections.go`, `quit.go`, their tests; `app.go` (`Disconnect`, `DisconnectAll`); `run.go`.

**Tasks**

1. The page: above, the button `Disconnect all` (disabled when nobody is connected). A list with one row per connection: the person, the address, `Since 12 min ago` (`app.Ago`), what is open (`app.ActivitiesText`), and a button `Disconnect`.
2. `Disconnect`: `confirm` with `Disconnect alice (198.51.100.7)?` → `Disconnect`. `Disconnect all`: `confirm` with `Disconnect all 3 connections?` → `DisconnectAll`.
3. With nobody: `Nobody is connected.`
4. The times on this page and on the Attempts page must move on: the root rebuilds every 30 seconds with a timer of the app's clock (`clock.NewTicker`; hand the tick over with `Post`; stop it in `Dispose`).
5. `quit.go`: when the window's close button is pressed (the exit request of `AppLifecycleListener`): with no connection, let it exit; with connections, cancel the exit and show `confirm` with `app.QuitQuestion(connections)` and `yes` = `Quit`; on yes, exit (look up how an application asks Glutter to exit). When the reason of the request is the end of the desktop session, never ask. (`ui.closeToTray` comes in step 6.8.)
6. `run.go` and `cmd/cave`: after the window is gone, `Run` returns and `main` closes the application, which disconnects everybody.

**Tests**

- `TestGoldenConnections`: the sample in light-wide and dark-narrow; the empty fake; three connections with every kind of activity.
- With the fake: `Disconnect` then `Cancel` → no call; `Disconnect` then yes → `Disconnect(<id>)`; `Disconnect all` → `DisconnectAll()`.
- With the real application: two clients connect, the page shows two rows after `Emit`-free waiting (`Eventually`, pumping between looks); `Disconnect` on the first closes it and not the second.
- `TestTheTimesMoveOn`: advance the fake clock by 5 minutes; after a pump `Since 12 min ago` is `Since 17 min ago`.
- `TestQuitAsksOnlyWithConnections`, a table: no connection → the exit is allowed, no dialog; one connection → the exit is cancelled, the dialog has the words of `app.QuitQuestion`; `Cancel` keeps the app; `Quit` exits; the end of the session with a connection → allowed, no dialog.

**Proof** (D), with a live session:
```
scripts/proof.sh docs/implemented-proof-images/6.6-connections.png --config scripts/testdata/two-people --after 3 \
  --during 'scripts/proof-ssh.sh alice alice sleep 30' -- --page connections
```
It must show `alice` with `exec`. Afterwards `pgrep -a ssh` shows nothing of yours.

**Finish**: rules 10–15.

### Step 6.7: The Settings page

**Goal**: every setting in its group with its explanation; the host key; the folders; the version.

**Read**: SPEC §11.5, §6, §9.8, §9.10. Step 5.8.

**Files**: `internal/gui/page_settings.go` and its test; `app.go` (`Set`, `NewHostKey`).

**Tasks**

1. One scroll view with a card per group of `app.SettingTexts()`, in its order. A row has the label, under it the explanation in a smaller style, and on the right the control for the kind:
   - boolean: a switch → `Set(key, value)`;
   - choice: a dropdown with the choices (worded: `never` → `Never`, `onStart` → `Each time Cave starts`, `hourly` → `Every hour`, `daily` → `Every day`, `weekly` → `Every week`, `monthly` → `Every 30 days`; `system` → `Follow the system`, `light` → `Light`, `dark` → `Dark`) → `Set`;
   - number and text: a text field that shows the value; Enter, or leaving the field, runs `Set` when the text changed (a number is parsed; `Type a number.` under the field when it is not one); `esc` puts the old value back;
   - list (`session.acceptEnv`): a text field with the names joined with `, `, split as in step 5.8.
   An error of `Set` shows under that field (`app.Message`), and the field keeps the text.
2. Nothing is greyed here: every setting applies to the desktop app.
3. A card `Host key`: the fingerprint with a copy button, and `New host key…` → `confirm` with `Make a new host key? Every person will see a warning that the host key changed, the next time they connect.` → `NewHostKey`.
4. A card `About`: `Configuration folder` and `State folder` with their paths (copy buttons), and `Version`.
5. The Attempts page's `Change…` (step 6.5) lands on this page scrolled to the group `Attempts list`.

**Tests**

- `TestGoldenSettings`: the sample in light-wide (the top, and scrolled to the end) and dark-narrow; a port field with the error of a port out of range.
- With the fake, a table with one row per kind (as in step 5.8): the switch of `sftp.enabled`; the dropdown of `attempts.clear`; `server.port` typed and Enter; `session.acceptEnv`; `abc` in the port field → no call and `Type a number.`; `esc` → no call, the old value is back.
- `TestEverySettingIsOnThePage`: for every key of `settings.Schema()`, its label is found (scroll to it when needed).
- With the real application: `ui.theme` → `Dark` makes the theme dark; `server.port` to a free port → a client connects there; a port in use → the sentence under the field; `New host key…` then yes → another fingerprint.

**Proof** (D): `scripts/proof.sh docs/implemented-proof-images/6.7-settings.png --config scripts/testdata/two-people -- --page settings`.

**Finish**: rules 10–15.

### Step 6.8: Tray and notifications

**Goal**: a tray icon with a menu, notifications when somebody connects or is refused, and the close button that can hide the window. This is the last step of M6.

**Read**: SPEC §11.6, §6 (`notify.*`, `ui.closeToTray`). Glutter's `desktop` package (`NewSystemTray`, `NewTrayMenuItem`, `TrayIconFromRGBA`, `ShowNotification`), `examples/tray/main.go`, and `gluttertest/desktop.go` (`FakeDesktop`, `FakeNotification`).

**Files**: `internal/gui/tray.go`, `notify.go`, `tray_icon.go`, their tests; `quit.go`; `root.go`.

**Tasks**

1. `tray_icon.go`: `trayIcon(running bool) image.Image` draws the icon in Go (no image file): a 32×32 rounded square with a keyhole shape; green when the server runs, grey when it does not. Keep it simple; step 8.2 draws the real icon.
2. `tray.go`: the tray (around the root, as `examples/tray` does) with the tooltip `Cave: running on port 2222` or `Cave: stopped`, and the menu: `Show Cave` (shows and raises the window), `Stop server` or `Start server` (by the state), `Quit` (asks as in step 6.6 when people are connected: show the window first, then the question). The icon and the menu follow the server's state.
3. When the desktop has no tray (Glutter reports it; look at how `examples/tray` learns it), Cave works without one and remembers that there is none.
4. `quit.go`: the close button with `ui.closeToTray` on **and** a tray: hide the window and do not exit. Otherwise as in step 6.6.
5. `notify.go`: the root also gets the `Accepted` and `Rejected` events (through `Post`, like `Changed`):
   - `Accepted` with `notify.accepted` on: the notification `alice connected`, body `From 198.51.100.7`.
   - `Rejected` with `notify.rejected` on: `A connection was refused`, body `<app.Who(entry)> from <address>: <app.ResultText(outcome)>`. The application already sends at most one a minute.
   - The settings are read at the time of the event. A notification that fails is logged at the level `warn` and nothing else happens. Notify also while the window is focused: the user may be looking at another page.
6. **Last step of M6**: a second commit sets `devVersion` to `0.8.0-dev`; then merge, tag `step-6.8`, publish as `v0.7.0` (rule 14).

**Tests** (with `FakeDesktop`)

- `TestTrayMenu`: with the sample (running) the menu is `Show Cave`, `Stop server`, `Quit`; activating `Stop server` → `Calls()` has `StopServer()`; with the empty fake the item is `Start server`.
- `TestTrayFollowsTheServer`: change the fake's state, `Emit`; the tooltip and the icon changed (compare the icon's pixels with `trayIcon(false)`).
- `TestTrayIconGolden`: `trayIcon(true)` and `trayIcon(false)` as golden PNG files.
- `TestNotifications`, a table: `Accepted` with the setting on → one notification with the summary `alice connected`; with it off → none; `Rejected` with the setting on → one with the result's words; with it off → none; hostile text in the entry is in the body as stored (no control character).
- `TestClose`, a table: `ui.closeToTray` on with a tray → the window is hidden and no exit; on without a tray → as step 6.6; off with a tray → as step 6.6.
- `TestWithoutATray`: the fake desktop without a tray: the app builds, and nothing fails.

**Manual check** (it does not stop the work; CLAUDE.md "Working through the steps"): add to `docs/MANUAL-CHECKS.md`: "Step 6.8: on your desktop run `port=$(go run scripts/freeport.go); echo $port; scripts/isolated.sh --config scripts/testdata/two-people -- go run -tags debug_mode ./cmd/cave --port $port`. Is there a tray icon, and does its menu work? Then, in another terminal, with the port it printed: `CAVE_PROOF_PORT=<port> CAVE_PROOF_FIXTURE=scripts/testdata/two-people scripts/proof-ssh.sh alice alice true`. Does a notification appear?" (The fixture listens on loopback only, on a port the system chose.)

**Finish**: rules 10–15, then the publish of M6.

---

## M7: Windows

**Read this before any step of M7.**

- The rule of the platforms (CLAUDE.md "Project"): for a part of the system, the `_linux.go` file and the `_windows.go` file have **the same function names and signatures**, and the code that is shared calls only those. Before you write a Windows file, read its Linux twin and list the functions the shared code calls. When the shared code must change so that Windows fits, change it for both, and run the Linux tests of the package again.
- A step of M7 **replaces** an "unsupported" answer (`SessionUnsupported`, `PtyUnsupported`, `SftpUnsupported`, `AgentUnsupported`). When the Tag is no longer made anywhere, remove its `_windows_test.go` route test and its sentence, if it has one; `scripts/error-tags.sh --untested` must print nothing.
- Windows functions come from `golang.org/x/sys/windows` (in the module cache). Look every one up there before you call it: the name, the arguments, and whether it exists. When a function you need is missing there, declare it with `windows.NewLazySystemDLL("kernel32.dll").NewProc("Name")`, as `x/sys` does itself. **No new dependency** (not `go-winio`, not `conpty` packages): if one seems necessary, rule 9.
- Tests run under Wine: `scripts/test-windows.sh ./internal/core/<package>` while you work (through `scripts/heavy.sh --alone`), and all of them in the standard checks. Wine is not Windows. When something works by the documentation of Windows and fails only under Wine, do not bend the code to Wine: skip that test under Wine with the reason in `t.Skip` (find Wine with the registry key or the function `wine_get_version` in `ntdll.dll`; put the check in one helper, `wine.Running()`, in a small package for tests), and add a manual check for a real PC to `docs/MANUAL-CHECKS.md`.
- A test that needs a child process uses the test program itself: start `os.Args[0]` with `-test.run=TestHelperProcess` and an environment variable that says what the helper does (the pattern of the standard library's `os/exec` tests). The test program is a Windows program, so this works under Wine without any tool of Windows.
- Paths in tests come from `t.TempDir()`; never write a path with `C:\` by hand except in the pure tables of step 7.3.

### Step 7.1: Sessions on Windows

**Goal**: `ssh <person>@<windows computer> <command>` and a shell without a terminal work on Windows.

**Read**: SPEC §13.1, §13.2, §9.4, §9.5. `internal/core/session`: every `_linux.go` file and `proc_windows.go` (the `TODO(step 7.1)`). In `golang.org/x/sys/windows`: `CreateJobObject`, `SetInformationJobObject`, `JOBOBJECT_EXTENDED_LIMIT_INFORMATION`, `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, `AssignProcessToJobObject`, `TerminateJobObject`; in `syscall`: `SysProcAttr.CmdLine` and `CreationFlags`.

**Files**: `internal/core/session/proc_windows.go`, `env_windows.go`, `job_windows.go` (name them after their Linux twins where one exists); `session_windows_test.go`; `internal/core/wine/wine.go` (the helper for tests named above; add the package to the table of SPEC §4.1).

**Tasks**

1. **The shell**: `session.shell` when it is set, else the value of `ComSpec` in Cave's environment, else `cmd.exe`.
2. **The command line** (SPEC §13.2):
   - `shell` request: the shell alone.
   - `exec` request with `cmd.exe` (or any shell that is not PowerShell): `<shell> /d /c <command>`, with the command **as the client wrote it**. Go's `exec.Command` would quote it again, so set `SysProcAttr.CmdLine` to the whole line yourself.
   - `exec` with a shell whose file name is `powershell.exe` or `pwsh.exe` (compare without the folder, ignoring case): the arguments `-NoLogo`, `-NoProfile`, `-Command`, `<command>`.
3. **The environment**: exactly the variables SPEC §13.2 lists, copied from Cave's own environment when they are there (Windows names ignore case: compare with `strings.EqualFold`), plus `TERM` (only with a terminal), `SSH_CLIENT`, `SSH_CONNECTION`, `SSH_AUTH_SOCK` (step 7.4), `CAVE_PERSON`, and the client's `env` requests that `session.acceptEnv` allows. Nothing else of Cave's environment reaches a session. Write it as a pure function of (Cave's environment, the session's facts) so that a table tests it on every platform; put that function in a file without a build tag if it uses nothing of Windows.
4. **The working folder**: the user's profile folder (`USERPROFILE`).
5. **The job**: make a job object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, and put the process in it right after it started. When the session ends, close the job: Windows then ends every process of the session. Start the process with `CREATE_NEW_PROCESS_GROUP`.
6. **`signal`**: `KILL` and `TERM` end the job (`TerminateJobObject` with the exit code 1) and the request is answered with success; every other signal is refused. `break` is refused.
7. **The exit status**: the process's exit code, as on Linux. A process that the job ended reports what Windows gives.
8. Standard input, output and error are pipes, as on Linux; the end of the client's input closes the process's standard input.
9. Go through the tests of `internal/core/session`: a test whose name ends in `_linux_test.go` stays for Linux. Do not rename them. Write the Windows ones new, below.

**Tests** (`session_windows_test.go`, under Wine; the session's shell is `cmd.exe`, which Wine has)

- `exec` of `echo hello` prints `hello\r\n` and exits with 0; `exit 3` gives the exit status 3; a command that does not exist gives a status that is not 0 and text on standard error.
- The command reaches the shell as written: `echo "a b"   c` prints `"a b"   c` (cmd.exe keeps quotes and spaces).
- The environment, a table on the pure function (it runs on Linux too): the listed variables are copied, another variable of Cave is not (`SECRET_TOKEN`), `CAVE_PERSON` is the person, an `env` request is taken only when `session.acceptEnv` allows it.
- Standard input: a helper process that copies its input to its output gets 1 MiB and returns it the same.
- The job: `exec` of a helper process that starts a second helper (which sleeps for 60 seconds) and prints its process ID; close the channel; within 5 seconds the second helper is gone (`OpenProcess` fails, or its exit code is set).
- `signal KILL` ends a sleeping helper and the client gets an exit status; `signal INT` is refused.
- The PowerShell line, as a pure table of the function that builds the command line (no PowerShell is needed): `cmd.exe`, `C:\Windows\System32\cmd.exe`, `powershell.exe`, `PWSH.EXE`.
- `scripts/test-windows.sh` passes for every package (storage, settings, people, attempts, hostkey, server and the rest), as it did before.

**Proof** (T): `scripts/transcript.sh docs/implemented-proof-images/7.1-wine-sessions.txt -- scripts/test-windows.sh -v -count=1 -run 'Exec|Job|Signal' ./internal/core/session`. It must show the tests above passing under Wine. Afterwards `pgrep -a wine` shows nothing.

**Finish**: rules 10–15.

### Step 7.2: ConPTY

**Goal**: a session with a terminal (`ssh -t`, an interactive shell) works on Windows.

**Read**: SPEC §13.3, §9.6. `internal/core/pty/pty_linux.go` and `pty_windows.go` (the `TODO(step 7.2)`), and how `internal/core/session` uses the package for a `pty-req`. In `golang.org/x/sys/windows`: `CreatePseudoConsole`, `ResizePseudoConsole`, `ClosePseudoConsole`, `Coord`, `NewProcThreadAttributeList`, `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`, `StartupInfoEx`, `CreateProcess`, `EXTENDED_STARTUPINFO_PRESENT`.

**Files**: `internal/core/pty/pty_windows.go`, `pty_windows_test.go`; `internal/core/session` (the Windows side of starting a process on a terminal).

**Tasks**

1. `pty.Open(size, modes)` on Windows: make two pipes (`windows.CreatePipe`): one whose read end the pseudo console gets (its input) and one whose write end it gets (its output). Call `CreatePseudoConsole` with the size (a dimension of 0 means "not said": use 80 columns and 24 rows) and those two ends, then close the two ends you gave away. The ends you keep are what the session copies to and from the channel. The terminal modes are ignored (SPEC §13.3).
2. `Resize`: `ResizePseudoConsole` (a dimension of 0 keeps the current one, as on Linux). `Close`: `ClosePseudoConsole`, then the pipes; safe to call twice.
3. A process on a pseudo console cannot be started with `os/exec`: build an attribute list with `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` and call `windows.CreateProcess` with `StartupInfoEx` and `EXTENDED_STARTUPINFO_PRESENT`. The command line, the environment (as a UTF-16 block, sorted, each `name=value` ended by a zero and the block by two) and the folder are those of step 7.1. Put the process in the session's job (step 7.1). Wait for it with `WaitForSingleObject` on a goroutine and read the exit code with `GetExitCodeProcess`.
4. Keep the names the shared code calls. When the Linux shape (`Master`, `Slave`, `CloseSlave`) does not fit Windows, give the Windows type the methods the shared code needs and move the difference into a function that exists on both platforms (for example one `start` function per platform that takes the session's facts and returns "a running process with its input and output"). Run the Linux tests of `pty` and `session` after such a change.
5. `SSH_TTY` is not set on Windows; `TERM` is.

**Tests** (under Wine; first find out whether Wine's ConPTY works: a test that only calls `CreatePseudoConsole` and `ClosePseudoConsole`)

- When it works: a `shell` session on a terminal gets `echo marker-%CAVE_PERSON%` typed and its output holds `marker-alice`; `exit 5` ends it with the status 5; a `window-change` to 132×43 succeeds; closing the channel ends the process within 5 seconds.
- When it does not work under Wine: those tests skip with `t.Skip("Wine's ConPTY: <what failed>")`, and `docs/MANUAL-CHECKS.md` gets: "Step 7.2, on a Windows PC: `ssh -t <person>@<pc>` gives a `cmd.exe` prompt; `dir` prints; resizing the terminal window is followed; `exit` ends the session."
- Everywhere: `Open` with a size over the limit is refused with the route of `Size.Check` (it is checked before anything of Windows is called).

**Finish**: rules 10–15. No proof.

### Step 7.3: SFTP on Windows

**Goal**: `sftp` and `scp` work against Cave on Windows.

**Read**: SPEC §13.4, §9.7. `internal/core/sftp/fs.go` (the `FileSystem` interface), `fs_linux.go` (the implementation to mirror), `fs_windows.go` (the stub).

**Files**: `internal/core/sftp/winpath.go` and `winpath_test.go` (no build tag: pure functions, tested everywhere), `fs_windows.go`, `fs_windows_test.go`.

**Tasks**

1. `winpath.go`: two pure functions on strings.
   - `toWindows(sftpPath string) (path string, isRoot bool)`: `/` → the root (`isRoot`); `/C:` and `/C:/` → `C:\`; `/C:/Users/ada` → `C:\Users\ada`; a drive letter in lower case is made upper case; a path without a drive, or with `..` that would leave the drive, is cleaned first with the SFTP rules (`path.Clean` on the `/` form) and never escapes above `/`.
   - `fromWindows(path string) string`: `C:\Users\ada` → `/C:/Users/ada`.
2. `NewFileSystem(home)`: `REALPATH` of `.` is the home folder in the SFTP form. Relative paths are relative to the home folder.
3. The root `/`: `OPENDIR` and `READDIR` list the drives (`windows.GetLogicalDrives`: one bit per letter) as folders named `C:`, `D:`; `STAT` of `/` is a folder. Nothing can be made, removed or renamed in `/` itself: `SSH_FX_PERMISSION_DENIED`.
4. Everything else through the `os` package, as on Linux: open, read, write, close, list, make and remove folders, remove, rename (`posix-rename` replaces the target: `os.Rename` does that on Windows), stat.
5. Attributes: permissions as Go reports them (`FileInfo.Mode().Perm()`), owner and group 0, times as on Linux. `SETSTAT` of permissions changes only the read-only bit (`os.Chmod`); `SETSTAT` of owner or group is accepted and does nothing; times and size are set as on Linux.
6. `SYMLINK`, `READLINK`, `hardlink@openssh.com`: call `os.Symlink`, `os.Readlink`, `os.Link`; when Windows refuses (no privilege, a file system without them), answer `SSH_FX_OP_UNSUPPORTED`. `statvfs@openssh.com` and `fstatvfs@openssh.com`: `SSH_FX_OP_UNSUPPORTED`.
7. Map the errors of Windows to SFTP statuses in one function, the twin of `statusOf` on Linux: not found → `NO_SUCH_FILE`; access denied or a sharing violation → `PERMISSION_DENIED`; the rest → `FAILURE`.
8. The session's `subsystem sftp` request on Windows now starts the server (remove the `SftpUnsupported` answer and its test).

**Tests**

- `winpath_test.go`, a table of at least 20 rows, both directions: the cases of task 1, `/c:/x`, `//C://x`, `/C:/a/../b`, `/C:/..` (stays at `C:\`), `/../..` (the root), a name with a space, a name with a non-ASCII letter.
- `fs_windows_test.go` (under Wine, in `t.TempDir()`, with the test client `sshtest.NewSFTPClient` over an in-memory pipe as the Linux tests do): put a file of 1 MiB and get it back the same; list a folder; make and remove a folder; rename onto an existing file with `posix-rename`; remove; `STAT` of a file that is not there is `NO_SUCH_FILE`; `/` lists at least `C:`; `REALPATH .` is the home in the `/C:/…` form; making a folder in `/` is `PERMISSION_DENIED`; `SETSTAT` with read-only permissions makes the file read-only.
- The session test: the `sftp` subsystem starts under Wine and answers `INIT`.

**Finish**: rules 10–15. No proof.

### Step 7.4: Agent forwarding on Windows

**Goal**: `ssh -A` works on Windows: the session's `SSH_AUTH_SOCK` is a named pipe that only the current user can open.

**Read**: SPEC §13.5, §9.8. `internal/core/agentfwd/agentfwd.go`, `agentfwd_linux.go`, `agentfwd_windows.go` (the stub). In `golang.org/x/sys/windows`: `CreateNamedPipe`, `ConnectNamedPipe`, `DisconnectNamedPipe`, `PIPE_ACCESS_DUPLEX`, `FILE_FLAG_OVERLAPPED`, `FILE_FLAG_FIRST_PIPE_INSTANCE`, `PIPE_REJECT_REMOTE_CLIENTS`, `SecurityDescriptorFromString`, `SecurityAttributes`, `GetCurrentProcessToken`, `Token.GetTokenUser`, `CancelIoEx`, `GetOverlappedResult`.

**Files**: `internal/core/agentfwd/agentfwd_windows.go`, `pipe_windows.go`, `agentfwd_windows_test.go`.

**Tasks**

1. The name: `\\.\pipe\cave-agent-<32 hexadecimal digits from crypto/rand>`. It is the session's `SSH_AUTH_SOCK`.
2. The security descriptor: only the current user. Get the user's SID from the process token and build the SDDL `D:P(A;;GA;;;<SID>)` (a protected list with one entry: all access for that SID), turn it into a descriptor with `SecurityDescriptorFromString`, and pass it in the `SecurityAttributes` of `CreateNamedPipe`. Also pass `PIPE_REJECT_REMOTE_CLIENTS`, so that the pipe cannot be reached from the network, and `FILE_FLAG_FIRST_PIPE_INSTANCE` for the first instance, so that nobody can have made a pipe of that name before.
3. `pipe_windows.go`: a small listener for the pipe, written with overlapped I/O so that `Close` can end a waiting `ConnectNamedPipe` (`CancelIoEx`) and a read that waits. It gives each client as an `io.ReadWriteCloser`. Keep it as small as the agent needs: no message mode, no timeouts.
4. `Listen(ctx, Options)` on Windows: accept clients (at most `MaxConns` at once, as on Linux), and for each one open an agent channel to the client with `Options.Open` and copy both ways, exactly as the Linux file does. `Socket.Path()` is the pipe's name; `Socket.Close()` ends the listener and every copy, and is safe to call twice.
5. Remove `AgentUnsupported` and its test. If overlapped pipes cannot be written with `x/sys/windows` alone, stop and ask (rule 9); do not add a dependency.
6. Run the forwarding tests under Wine too: `scripts/test-windows.sh ./internal/core/forward`. Fix what fails for a reason of Windows (an error number that differs, a Unix socket in a test: skip that test on Windows with the reason).

**Tests** (under Wine)

- A client in the test (open the pipe with `os.OpenFile(name, os.O_RDWR, 0)`) speaks the agent protocol with `golang.org/x/crypto/ssh/agent.NewClient` and lists the one key of an agent the test made (`agent.NewKeyring`), through a fake `Options.Open` that connects to that keyring with `agent.ServeAgent`.
- After `Close`, opening the pipe fails; `Close` twice is fine; a `Close` while a client is connected ends that client's read.
- More than `MaxConns` clients: the extra one waits or is refused, as the Linux test expects; nothing leaks (after `Close`, no goroutine of the package is left: count with the helper the Linux test uses).
- The descriptor, as a pure test of the function that builds the SDDL string: it holds the SID once, `D:P`, and no other entry.
- A session test: with `auth-agent-req@openssh.com` accepted, the session's environment has `SSH_AUTH_SOCK` starting with `\\.\pipe\cave-agent-`.
- Add to `docs/MANUAL-CHECKS.md`: "Step 7.4, on a Windows PC: `ssh -A <person>@<pc> ssh-add -l` lists the key of your agent."

**Finish**: rules 10–15. No proof.

### Step 7.5: The programs on Windows

**Goal**: `cave.exe` and `tcave.exe` are built, start under Wine, and are proven with a screenshot. This is the last step of M7.

**Read**: SPEC §13.1, §16. Glutter's `cmd/glutter-windows` (its `README` or `main.go`: the `build` command and its flags), `scripts/wine-screenshot.sh` and `scripts/wine-exec.sh`. Cave's `scripts/wine-exec.sh` and `scripts/proof.sh`.

**Files**: `scripts/build-windows.sh`, `scripts/wine-proof.sh`; fixes under `internal/tui`, `internal/gui`, `cmd/`; `cmd/cave/wine_test.go`.

**Tasks**

1. `scripts/build-windows.sh [--out <dir>]` (default `dist/windows-amd64`): builds `tcave.exe` with `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o <dir>/tcave.exe ./cmd/tcave` (a console program), and `cave.exe` with Glutter's tool (`go run github.com/rm4n0s/glutter/cmd/glutter-windows build …`, with the version of `internal/app` and, until step 8.2 draws one, without an icon), so that it is a GUI program that opens no console window. `dist/` is git-ignored (add it to `.gitignore`).
2. `scripts/wine-proof.sh <out.png> [--config <fixture dir>] [--after <seconds>] -- [cave args]` (method W): start from Glutter's `scripts/wine-screenshot.sh`, with Cave's private prefix (`scripts/wine-exec.sh`), the fixture copied into a temporary folder and given through `CAVE_CONFIG_DIR` and `CAVE_STATE_DIR` (as Windows paths: `winepath -w`), a port from `go run scripts/freeport.go`, the same refusal of a fixture that does not listen on loopback, and `wineserver -k` at the end.
3. Run the UI tests for Windows: `scripts/test-windows.sh ./internal/tui ./internal/app` and fix what fails for a reason of Windows (path separators in expected text, `SIGHUP`, a shell in a fixture: `apptest.Open` must use `cmd.exe` as the session's shell on Windows).
4. `tcave.exe` on Windows: the console must be in the mode Bubble Tea needs; Bubble Tea does that itself. Check that nothing in `internal/tui` or `cmd/tcave` uses a Unix-only call.
5. `cave.exe`: the folders are those of SPEC §5.1 for Windows; the tray and notifications go through Glutter (when one of them fails under Wine, that is a note for the manual check, not a bug of Cave).

**Tests**

- Under Wine: the model tests of `internal/tui` pass; `tcave.exe --version` prints the version (a test that builds it and runs it through `scripts/wine-exec.sh`).
- `cmd/cave/wine_test.go` with the build tag `cave_wine`: `cave.exe`, started with isolated folders and `--no-start`, draws its first frame (the log gets a line that says so at the level `info`: add that line to `gui.Run` if it is not there) and exits cleanly when it is asked to close. Run it with `scripts/heavy.sh --alone -- …` the way Glutter's `examples/windows_wine_test.go` is run.

**Proof** (W): `scripts/wine-proof.sh docs/implemented-proof-images/7.5-windows-overview.png --config scripts/testdata/two-people`. It must show the Overview page with the server running.

**Manual check**: add to `docs/MANUAL-CHECKS.md`: "Step 7.5, on a Windows PC, with the files of `dist/windows-amd64`: `cave.exe` opens without a console window; the tray icon and a notification appear; from another computer `ssh <person>@<pc>` gives a shell (ConPTY), `sftp` works, `ssh -A` forwards the agent; `tcave.exe` draws correctly in Windows Terminal and in the old console."

**Last step of M7**: a second commit sets `devVersion` to `0.9.0-dev`; then merge, tag `step-7.5`, publish as `v0.8.0` (rule 14).

**Finish**: rules 10–15, then the publish of M7.

---

## M8: Release

### Step 8.1: The manual

**Goal**: a person who has never seen Cave can install it, add somebody and understand what that somebody can do.

**Read**: SPEC, all of it. `app.Usage` and `app.SettingTexts` (the words already written).

**Files**: `README.md`, `docs/GUIDE.md`, `internal/app/flags.go` (the usage text, if it needs more), `docs/guide_test.go` or a test in `internal/app` (below).

**Tasks**

1. `README.md`, in this order: one paragraph on what Cave is and for whom; a screenshot of the desktop app and a capture of the TUI (link the proofs `6.2-overview.png` and `5.3-overview.txt`; do not make new pictures); "Install" (`go install github.com/rm4n0s/cave/cmd/cave@latest` and `…/cmd/tcave@latest`, and the files of a release); "First use" in five numbered steps (start it, add a person, paste their public key, give them the `ssh` line, watch the Attempts page); "Security in short" (public keys only, every session runs as you, what a connected person can do); links to `docs/GUIDE.md` and `docs/SECURITY.md` (step 8.3 writes it; link it now).
2. `docs/GUIDE.md`, one section each: People and keys (what a public key is, where a person finds theirs, the errors and what they mean); The attempts list (what each result means, when it is cleared, the `attempts.clear` setting); Connections; What a connected person can do (a shell as **you**, your files through SFTP, port forwarding, agent forwarding and **who can reach a forwarded agent**: SPEC §9.8, in plain words); Settings (a table of every setting, its default and its meaning: every key of SPEC §6 by its exact name); Files and folders (SPEC §5.1 for both systems, the log); The command line (the flags); Windows (the differences of SPEC §13); Troubleshooting (the port is in use, "already running", a changed host key warning, a client that is refused: where to look).
3. Write for the user: short sentences, no SSH jargon without one line of explanation.
4. `--help` of both programs prints `app.Usage`: one line per flag with what it does, and the two folders it uses by default.

**Tests**

- `TestUsageGolden` for `cave` and `tcave`: the text of `app.Usage` as a golden file.
- `TestTheGuideNamesEverySetting`: every key of `settings.Schema()` is in `docs/GUIDE.md` in backticks.
- `TestTheGuideNamesEveryFlag`: every flag of `app.Flags` is in `docs/GUIDE.md`.
- `TestTheReadmeLinksExist`: every relative link of `README.md` and `docs/GUIDE.md` points to a file that exists.

**Finish**: rules 10–15. No proof.

### Step 8.2: Packaging

**Goal**: an icon, a desktop entry, and one script that builds the files of a release.

**Read**: SPEC §16. Step 7.5 (`scripts/build-windows.sh`). The flags of `glutter-windows build` for the icon and the version.

**Files**: `internal/gui/icon/icon.go`, `icon_test.go`; `packaging/cave.desktop`, `packaging/icons/` (generated PNG files); `scripts/build-release.sh`; `internal/gui/tray_icon.go` (use the new icon).

**Tasks**

1. `internal/gui/icon`: `Draw(size int) *image.RGBA` draws Cave's icon with the standard library only (`image`, `image/draw`, `image/color`; no font, no file): a rounded square in the theme's seed color with a light arch (a cave's mouth) and a small keyhole in it. It must read at 16 pixels and at 256. `Tray(size int, running bool) *image.RGBA`: the same shape, with a green dot when the server runs. Draw with coverage (anti-aliasing) computed per pixel; no dependency.
2. A small `go generate` program (a file with `//go:build ignore` in `internal/gui/icon`, run by a `//go:generate` line) writes `packaging/icons/cave-<size>.png` for 16, 32, 48, 64, 128 and 256, and `packaging/icons/cave.ico` if Glutter's Windows build wants an `.ico` (look at `glutter-windows build`; if it takes PNG files, write no `.ico`). The generated files are committed.
3. The window's icon and the tray's icon use the package (replace the simple icon of step 6.8; update its goldens and look at them).
4. `packaging/cave.desktop`: `Type=Application`, `Name=Cave`, `Comment=An SSH server you start, watch and stop`, `Exec=cave`, `Icon=io.github.rm4n0s.cave`, `Categories=Network;`, `Terminal=false`, `StartupWMClass=io.github.rm4n0s.cave` (the application ID of step 6.1).
5. `scripts/build-release.sh [--version <v>]`: builds into `dist/<version>/`: `cave` and `tcave` for `linux/amd64` and `linux/arm64` (`CGO_ENABLED=0`, `-trimpath`), `cave.exe` and `tcave.exe` for `windows/amd64` and `windows/arm64` (through `scripts/build-windows.sh`; give it a parameter for the architecture); one archive per target (`.tar.gz` for Linux with the two programs, `cave.desktop`, the icons, `README.md`, `LICENSE`; `.zip` for Windows), and `SHA256SUMS`. It uploads nothing and makes no tag: publishing a release's files is the user's decision.

**Tests**

- `TestIconGolden`: `Draw(16)`, `Draw(48)`, `Draw(256)`, `Tray(32, true)`, `Tray(32, false)` as golden PNG files; open them and look.
- `TestTheGeneratedIconsAreCurrent`: the committed `packaging/icons/cave-48.png` equals `Draw(48)`.
- `TestTheDesktopFile`: the file has the keys of task 4; when `desktop-file-validate` is installed, it passes (else that part skips with the reason).
- `scripts/build-release.sh --version 0.0.0-test` in a temporary `dist` builds every target, and `sha256sum -c SHA256SUMS` passes (a test in `internal/archtest`'s scripts test, or a `t.Skip` in short mode: it is heavy; run it through `scripts/heavy.sh`).

**Finish**: rules 10–15. No proof.

### Step 8.3: The security pass

**Goal**: every security rule and every limit has a test that proves it, written down in one table. This is the last step of the plan.

**Read**: CLAUDE.md "Security"; SPEC §7, §8, §9, §13.5, §17.

**Files**: `docs/SECURITY.md`; new tests where the table has a hole; `internal/core/server/flood_test.go`.

**Tasks**

1. `docs/SECURITY.md`:
   - What Cave protects: who may connect (public keys of people you added), the host key, the files of people.
   - What it does not: a person who connects is **you** on this computer (your files, your programs, no sandbox); people are not separated from each other; who can reach a forwarded agent (SPEC §9.8); Cave is not a system service and does not replace a firewall.
   - How to report a problem: a private security advisory on the GitHub repository. Do not write an email address.
2. In the same file, a table **Rule → Test**: one row for every rule of CLAUDE.md "Security" and for every limit and refusal of SPEC §7.3 (each refusal looks the same to the client), §8.3 (hostile text), §8.5, §9.2, §9.3, §9.4, §9.7, §9.8, §9.9 and §13.5. Each row names the test function and its file. Find the tests with `grep`; do not write a name you did not open.
3. For each row without a test: write the test. A test of a refusal asserts the refusal itself (the client's error, the Tag's route, the log line), not only that the good case works.
4. `flood_test.go`: 3 000 connections to a server on `127.0.0.1:0`, at most 50 at a time, of four kinds (a port scanner that connects and closes, random bytes instead of a handshake, an unknown login name, a wrong key). Afterwards: the attempts list has at most 5 000 entries and the entries the test expects; the number of goroutines is back near where it was before (allow a small difference); the heap grew by less than 64 MiB (`runtime.ReadMemStats` after `runtime.GC()`); a good client still connects in under 2 seconds. Give the test a timeout of 2 minutes and keep every input bounded (CLAUDE.md "Memory (OOM)"). No fuzzing.
5. `grep -rn "TODO" cmd internal scripts`: resolve every one (do it, or remove it with a reason in the report). None may be left.
6. Go through SPEC §17 ("Not in this version") and check with `grep` that none of it crept in: no password or keyboard-interactive callback, no X11, no `authorized_keys` options, and `ssh.InsecureIgnoreHostKey` only where archtest allows it.
7. **Last step of M8**: a second commit sets `devVersion` to `0.10.0-dev`; then merge, tag `step-8.3`, publish as `v0.9.0` (rule 14). Then the plan is finished: report it and stop. `v1.0.0` is the user's decision (CLAUDE.md "Git and GitHub").

**Tests**: those the table lacked, and the flood test.

**Finish**: rules 10–15, then the publish of M8, then the final report: what is built, every item still in `docs/MANUAL-CHECKS.md`, every entry of `docs/GLUTTER-ISSUES.md`.

## M9: macOS

The user, 2026-11-25: "make it work for macos". Everything of Cave on macOS 14 or later, without cgo, as on Linux. Published as `v0.10.0`.

**Read this before any step of M9.**

- The rule of the platforms (CLAUDE.md "Project"): what Linux and macOS share moves from `_linux.go` to a `_unix.go` file with `//go:build linux || darwin`; what differs stays in `_linux.go` and gets a `_darwin.go` twin with **the same function names and signatures**. The same for tests: a test that runs on both is in a `_unix_test.go` file, and the facts that differ (a path, a message of the system) are constants of small `_linux_test.go` and `_darwin_test.go` files.
- macOS functions come from `golang.org/x/sys/unix` (in the module cache); look every one up there. When a call is missing (`TIOCPTYGNAME` needs a buffer), use `syscall.Syscall` with the constant from `x/sys`. No cgo, no new dependency.
- Every test runs on macOS **and** stays green on Linux: `GOOS=linux go vet ./...` after every change, and the Linux tests where a Linux computer is at hand.
- The temporary folder of macOS is long (`/var/folders/…/T/`), ends with a slash and is under a link (`/var` is `/private/var`); a socket's path has at most 104 bytes. Tests that make sockets use short folders under `/tmp`.

### Step 9.1: The core and the programs on macOS

**Goal**: `cave` and `tcave` run on macOS, and every test passes there.

**Files**: `internal/core/storage` (`storage_unix.go`, `storage_darwin.go`), `internal/core/storage/storagetest`, `internal/core/pty` (`pty_unix.go`, `pty_darwin.go`), `internal/core/session` (`proc_unix.go`, `dirshell_darwin.go`, the `_unix_test.go` files and `platform_*_test.go`), `internal/core/sftp` (`fs_unix.go`, `fs_darwin.go`), `internal/core/agentfwd`, `internal/core/sshtest`, `internal/app` (the usage), `cmd/*/signals_unix.go`, `scripts/isolated.sh`, `scripts/transcript.sh`, docs/SPEC.md, docs/SECURITY.md (the renamed files).

**Tasks**

1. Storage: the folders of SPEC §5.1 for macOS; the runtime folder `cave-<uid>` under `$TMPDIR`; `flock` and `fsync` of the folder as on Linux.
2. PTY: `/dev/ptmx` with `TIOCPTYGRANT`, `TIOCPTYUNLK`, `TIOCPTYGNAME`; the modes through `TIOCGETA`/`TIOCSETA` (64-bit flags, `_POSIX_VDISABLE` is 0xff). macOS throws away the output the master has not read when the last slave descriptor closes: keep one until the process ended and `TIOCOUTQ` is 0 (`Pty.Release`), so the end of the output reaches the client.
3. Sessions: the shell of the account from `dscl` when `/etc/passwd` has no line for it (SPEC §9.5).
4. SFTP: `renamex_np` with `RENAME_EXCL` (refusing a rename onto the same file itself), the times of `Stat_t`, `statvfs` from `Statfs_t`.
5. The usage names the macOS folders (a golden file for each program).
6. Isolation (SPEC §14.1): `storagetest.Isolate` and `scripts/isolated.sh` also point `TMPDIR` at a folder of their own, and name folders by their real path. `scripts/isolated.sh` keeps Go's caches.

**Tests**: every test of the module on macOS; the PTY's release (`TestReleaseWaitsForTheOutputToBeRead`); the shell from `dscl` (`TestDsclShell`, `TestDirectoryShellOfThisUser`); the macOS folders (`TestResolveOnMacOS`); `TestVFSOf` for macOS.

**Proof** (T, M): `scripts/transcript.sh docs/implemented-proof-images/9.1-macos-openssh.txt -- go test -v -count=1 -run OpenSSH ./internal/core/...` on a Mac: the real `ssh`, `sftp` and `scp` against Cave. `9.1-macos-overview.png`: the real window of `cave` on a Mac, `scripts/isolated.sh --config scripts/testdata/two-people -- go run github.com/rm4n0s/glutter/cmd/glutter-macos shot -o <out.png> ./cmd/cave`.

**Finish**: rules 10–15.

### Step 9.2: The scripts and the release on macOS

**Goal**: the scripts work with the bash and the tools of macOS, and a release has archives for macOS.

**Files**: `scripts/heavy.sh`, `scripts/checks.sh`, `scripts/build-release.sh`, `internal/archtest/release_test.go`.

**Tasks**

1. `scripts/heavy.sh`: macOS has bash 3.2 (no `{fd}>` redirections) and no `flock(1)`: fixed descriptors, and the locks through Perl's `flock` when `flock(1)` is missing.
2. `scripts/checks.sh` vets for Linux and macOS as well as Windows.
3. `scripts/build-release.sh`: `cave-<v>-macos-amd64.tar.gz` and `cave-<v>-macos-arm64.tar.gz` with `cave`, `tcave`, `README.md` and `LICENSE`; `tar` of GNU or BSD.

**Tests**: `TestBuildReleaseMakesEveryArchive` checks the macOS archives (Mach-O programs for the right processor, built without cgo).

**Finish**: rules 10–15. No proof.

### Step 9.3: The manual for macOS

**Goal**: the README and the guide say how Cave runs on a Mac.

**Files**: `README.md`, `docs/GUIDE.md`, `CLAUDE.md`, `docs/MANUAL-CHECKS.md`.

**Tasks**

1. README: macOS in the systems and in the release files.
2. The guide: the macOS folders, and a section "macOS": the shell, the terminal, a downloaded program (`xattr -d com.apple.quarantine`), the folders macOS protects, the firewall, Remote Login.
3. Manual checks for what only a person at a Mac can see: the tray, the notifications, an interactive terminal session.
4. **Last step of M9**: a second commit sets `devVersion` to `0.11.0-dev`; then merge, tag `step-9.3`, publish as `v0.10.0` (rule 14).

**Finish**: rules 10–15. No proof.

---

## M10: History

The user, 2026-11-25: "show a history of connections/disconnections and duration", "show the files that upload/download and its history. The history be in a file", and "leave mosh out" (SPEC D13, D14). The history of connections and SFTP transfers (SPEC §9.11), in the core, the application and both UIs. Published as `v0.11.0`.

**Read this before any step of M10.**

- SPEC §9.11 is the whole of the feature; read it before every step of M10. The UIs are SPEC §11.8 and §12.
- Records are written on the goroutines of connections and SFTP sessions: the store is safe for concurrent use, writes one line under its lock, and never calls a listener while holding it (look at how `internal/core/attempts` notifies).
- Everything in a record that came from a client (the client version, a path, the detail of an activity) is cleaned with `sanitize.Display` before it is stored, by the store itself, so that no caller can forget.
- Tests make the store with `storagetest.Isolate(t)` folders and the fake clock; they never use `time.Now`.

### Step 10.1: The history store

**Goal**: `internal/core/history` keeps the records of SPEC §9.11 in memory and in `history.jsonl`, with its limits, its rotation, its reading at start and its clearing. Nothing calls it yet.

**Read**: SPEC §9.11, §5, §8.4, §8.5. `internal/core/attempts/attempts.go` (options, `Policy`, `ApplyPolicy`, `PolicyChanged`, `Subscribe`, how it notifies) and `internal/core/logging/logging.go` (`rotatingFile`: append, size, rotate).

**Files**: `internal/core/history/history.go`, `file.go`, `history_test.go`, `file_test.go`; `internal/core/storage` only if a helper for mode `0600` appends is missing (look first).

**Tasks**

1. Types:
   - `Reason` (string) with `ReasonClient`, `ReasonDisconnected`, `ReasonPersonChanged`, `ReasonKeepAlive`, `ReasonServerStopped`, `ReasonUnknown` (the words of §9.11).
   - `Result` (string) with `ResultDone`, `ResultFailed`, `ResultInterrupted`.
   - `Activity{Kind, Detail string}` (the store does not import `server`, which will import it).
   - `Connection{ID, PersonID, PersonName, KeyFingerprint, Address, ClientVersion string; ConnectedAt, EndedAt time.Time; Reason Reason; BytesIn, BytesOut int64; Activities []Activity; MoreActivities int}` with `Open() bool` (no reason yet) and `Duration() (time.Duration, bool)` (false while open and for `unknown`).
   - `Transfer{ID, ConnectionID, PersonID, PersonName, Path string; OpenedAt, ClosedAt time.Time; Downloaded, Uploaded int64; Created bool; Result Result}` with `Direction() Direction` (`Upload`, `Download`, `Both`, by the rule of §9.11; `Created` is "opened to create or to empty").
2. `Open(ctx, Options{Dirs, Clock, Logger}) (*Store, error)`: reads `history.jsonl.1` then `history.jsonl` (§9.11 "At start"), line by line with a `bufio.Reader` that skips lines over 64 KiB without holding them; keeps the newest 2 000 connections and 5 000 transfers; logs `history lines skipped` with the count when there were any. An existing file that cannot be read is `HistoryReadFailed` (path in the metadata).
3. Writing: `Connected(Connection)`, `Ended(id string, endedAt time.Time, reason Reason, bytesIn, bytesOut int64, activities []Activity, more int)`, `AddTransfer(Transfer)` (gives the id `t_…` with `ids.New`). Each cleans the client's texts (client version 64, path 256, activity detail 80 characters), updates memory, appends its line (§9.11 "The file"), and notifies. `Ended` of an id that is not there (it was cleared, or rotated away) writes nothing. A failed write is logged with `HistoryWriteFailed` and the next write opens the file again; a failed rotation (`HistoryRotateFailed`) keeps appending to the full file, as the log does.
4. Rotation at 4 MiB (`MaxFileBytes`, a constant tests can lower through an unexported field), with a `state` line at the top of every new file.
5. Reading: `Connections() []Connection` (newest `ConnectedAt` first) and `Transfers() []Transfer` (newest `ClosedAt` first), copies; `LastClearedAt()`, `NextClearAt()`.
6. Clearing: `Clear(ctx) error` (§9.11 "Clearing": removes both files, writes the `state` line and the `connected` lines of the open connections; a file that cannot be removed is `HistoryClearFailed`), `ApplyPolicy(policy attempts.Policy, atStart bool) bool` and `PolicyChanged(policy attempts.Policy)` with the meaning they have in `attempts` (reuse its `Policy` type and `Period`).
7. `Subscribe(func()) (cancel func())`; `Close() error` closes the file (`HistoryCloseFailed`).

**Tests**

- `TestRecordsRoundTrip`: a connection that ends, one that stays open, transfers of each direction and result; a new `Open` on the same folder reads them back equal; the open one comes back `unknown` with no `EndedAt`.
- `TestHostileTextIsCleaned`: a client version, a path and an activity detail with `"\x1b[31m"`, `"‮"` and 300 characters are stored cleaned and cut, in memory and in the file.
- `TestSkippedLines`: a file with a line that is not JSON, half a line at the end, a line of `"v":2`, an unknown kind, a line of 70 KiB and an `ended` line without its `connected` line: the good records are read, the log has the count 6.
- `TestRotation` (a small limit): the file becomes `.1`, the new file starts with a `state` line, a connection that started before the rotation and ended after is read back whole.
- `TestLimitsInMemory`: 2 001 connections and 5 001 transfers keep the newest 2 000 and 5 000.
- `TestFileModes` (skipped on Windows): the file is `0600`.
- `TestClear`: both files are gone, an open connection is kept and its end is paired after a new `Open`; `TestClearPolicy`: `onStart` and the periods as in the attempts tests, with the fake clock, and the times survive a new `Open`.
- A route test for each Tag: `HistoryReadFailed` (a folder where the file should be), `HistoryWriteFailed`, `HistoryRotateFailed`, `HistoryClearFailed`, `HistoryCloseFailed` (make the operating system refuse, as the logging tests do; a Tag that cannot be provoked on one system skips there with a reason).

**Finish**: rules 10–15. No proof.

### Step 10.2: Recording connections

**Goal**: every authenticated connection is a record in the history, with its duration, how it ended, its bytes and what it opened.

**Read**: SPEC §9.3, §9.9, §9.11 (Connections). `internal/core/server/connections.go` (`serveAuthenticated`, `Disconnect`, `disconnectWhere`, `peopleChanged`, `AddActivity`), `auth.go` (`serve`, `countingConn`), `keepalive.go` (`look`), `server.go` (`Stop`).

**Files**: `internal/core/server/` (`server.go`, `connections.go`, `auth.go`, `keepalive.go`, a new `history_test.go`), `internal/core/server/servertest`, `internal/app/app.go` (only to give the server the store: open it in `open`, close it in `Close`).

**Tasks**

1. `server.Options.History *history.Store` (required; the test harness makes one in the test's folders).
2. `countingConn` also counts what is sent (`Write`), from the accept; the connection keeps both counters.
3. `Conn` keeps its end reason: the first of `disconnected` (`Disconnect`, `DisconnectAll`), `personChanged` (`peopleChanged`), `keepalive` (`look`), `serverStopped` (`Stop`: mark every connection of the registry before closing), set before the close that causes it; none set means `client`.
4. `Conn` keeps the activities it opened (`AddActivity`), each (kind, detail) once, at most 32, and the count of the others; `Activities` of the registry stays what is open now.
5. `serveAuthenticated` calls `History.Connected` after the connection is in the registry and `History.Ended` after it is out, with the clock's times.

**Tests** (with the in-process client, `servertest`):

- `TestHistoryReasons`, a table: the client closes → `client`; `Disconnect` → `disconnected`; `DisconnectAll` → `disconnected`; the person disabled, renamed, removed, the key removed → `personChanged`; keepalive with the fake clock → `keepalive`; `Stop` → `serverStopped`. Each has its `EndedAt` and a duration made with the fake clock.
- `TestHistoryActivities`: a shell, the same `exec` twice, another `exec`, a local forward: the record lists each once, in order; 40 different commands keep 32 and count 8.
- `TestHistoryBytes`: both counters are not 0 and grow with a command that prints 1 MiB.
- `TestRefusedAttemptsAreNotHistory`: a wrong key leaves the history empty.

**Finish**: rules 10–15. No proof.

### Step 10.3: Recording transfers

**Goal**: every file an SFTP session reads or writes is a transfer in the history.

**Read**: SPEC §9.7, §9.11 (Transfers). `internal/core/sftp/sftp.go` (`handle`, `open`, `read`, `write`, `close`, `closeAll`, `realpath`), `fs.go` (`FileSystem`, the open flags), `internal/core/session/session.go` (`Register`, `startSubsystem`).

**Files**: `internal/core/sftp/sftp.go`, `transfer.go` and their tests; `internal/core/session/session.go` and its tests; `internal/app/app.go` (gives the session handler the store).

**Tasks**

1. `sftp.Options.Clock`; `Serve(ctx, rw, record func(Transfer))` where `sftp.Transfer{Path string; OpenedAt, ClosedAt time.Time; Downloaded, Uploaded int64; Created bool; Result string}` (`record` may be nil). The path is the opened path made absolute the way `REALPATH` makes it (without resolving links: the name the client used).
2. A file handle counts the bytes its reads returned and its writes took, remembers whether it was opened with the create or truncate flag, and whether a read (other than `SSH_FX_EOF`) or a write failed. `CLOSE` records it with `done` or `failed`; `closeAll` with `interrupted`. A handle with nothing to record (§9.11) records nothing. Folders never record.
3. `session.Options.History *history.Store`: the session gives `Serve` a `record` that adds the connection's id and person and calls `History.AddTransfer`.

**Tests**

- In `sftp`: a table over a fake writer: an upload, a download, read and write on one handle (`both`), a file created and closed empty (an upload of 0 bytes), a file opened and closed without I/O (nothing), a read past the end only (nothing), a failed write (`failed`), the session ending with a file open (`interrupted`), a relative path made absolute.
- In `session`, through the server: `sftp` subsystem uploads and downloads with the `sshtest` SFTP client; the history has both transfers with the person and the connection's id.
- **OpenSSH tests** (`sshtest.Require`): `sftp put`, `sftp get` and `scp` (SFTP mode) of a 300 KiB file: three transfers with the right bytes and `done`; `scp -O` is not a transfer and is an `exec` activity of the connection.

**Finish**: rules 10–15. No proof.

### Step 10.4: History in the application

**Goal**: `internal/app` gives the UIs the history, the command that clears it, its setting and its words; the fake application has a sample history.

**Read**: SPEC §10, §6, §9.11, §11.8. `internal/app/app.go`, `commands.go`, `words.go`, `messages.go`, `settings_text.go`, `flags.go`, `apptest/fake.go`, `apptest/sample.go`, `apptest/real.go`.

**Files**: `internal/core/settings/schema.go` (`history.clear`); `internal/app/app.go`, `commands.go`, `events.go`, `words.go`, `messages.go`, `settings_text.go`, `flags.go` and their tests; `internal/app/apptest/*`; the `Application` interfaces of `internal/tui` and `internal/gui` only as far as the build needs (the pages come later).

**Tasks**

1. Setting `history.clear` (`settings.HistoryClear`, the choices of `attempts.clear`, default `never`); `app.Set` of it calls `PolicyChanged`; the minute check applies both policies; `onStart` applies at `Open`, before the server starts.
2. `TopicHistory`; the store's `Subscribe` changes it.
3. `History() HistorySnapshot{Connections []history.Connection, Transfers []history.Transfer, LastClearedAt, NextClearAt time.Time}`; `ClearHistory(ctx) error`.
4. Words: `ReasonText(history.Reason) string` (the sentences of §11.8), `ResultWord(history.Result) string`, `DirectionText(history.Direction) string`, `DurationText(time.Duration) string` (`45 s`, `12 min`, `2 h 5 min`, `3 d 4 h`), `SizeText(int64) string` (`0 B`, `512 B`, `1.4 KB`, `3.2 MB`, `1.1 GB`, decimal units). The settings group "Attempts list" becomes "Attempts and history" with `history.clear` in it (its label `Clear the history`, its line "When the history of connections and transfers is emptied.").
5. Messages for the new Tags a command can return (`HistoryClearFailed`), and `TestEveryTagHasAMessage` stays green.
6. `Pages` gets `history` between `connections` and `settings`.
7. `apptest`: `Fake.HistorySnapshot`, `ClearHistory` recorded in `Calls`; `Sample` has six connections (one open now, one of each other reason, one `unknown`) and eight transfers (every direction and result, one long path); `Empty` has none.

**Tests**: `TestHistorySnapshot` and `TestClearHistory` over a real application (a client connects, uploads with the test SFTP client, disconnects: the snapshot has both records; a `Changed` has `TopicHistory`); `TestHistoryClearSetting` (`onStart` empties at the next `Open`, `daily` with the fake clock); the words, table-driven; `--page history`.

**Finish**: rules 10–15. No proof.

### Step 10.5: The TUI's History page

**Goal**: `tcave` has a sixth page, History, as SPEC §11.8 and §12 say.

**Read**: SPEC §11.8, §12. The box at the top of M5. `internal/tui/page_attempts.go` (a table with a filter, a search and details: copy its shape), `frame.go` (`tabs`), `model.go` (pages, keys), `keys.go`.

**Files**: `internal/tui/page_history.go` and its test; `frame.go`, `model.go`, `keys.go`, `app.go`; every golden that shows the page line.

**Tasks**

1. The pages are Overview, People, Attempts, Connections, History, Settings: keys `1`–`6`, `Tab` goes round six. The page line: when the names do not fit, the pages that are not current show only their number (`1 2 3 4 [5 History] 6`).
2. The page's first line: `[Connections]  Transfers` (the current tab in brackets), then `last cleared …` and `next clear …` as the Attempts page words them.
3. Connections table: `Who`, `Address` (flexible), `Connected` (`app.Ago`), `Duration` (`app.DurationText`, `connected now`, `—`), `Ended` (`app.ReasonText`). Empty: `No connections yet.`
4. Transfers table: `Who`, `File` (flexible, cut at the start with `…` so the name of the file shows), `Direction`, `Size`, `When` (`app.Ago`), `Result`. Empty: `No files moved yet.`
5. Keys (add them to the key table): `t` switches the tab; `/` searches (who, address, file); `enter` shows the details of the selected row (§11.8, with the connection's transfers under a connection); `C` asks `Clear the history? Connections that are open stay.` → `ClearHistory`.

**Tests**

- `TestGoldenHistory`: the sample at 100×30 and 60×16 on each tab; the empty fake; the details of a connection and of a transfer; the question; a search.
- `TestGoldenFrame` and the other page goldens are updated for the new page line: open every changed file.
- With the fake: `C`, `y` → `ClearHistory()`; `t` switches; `6` shows Settings, `5` History.
- With the real application: a client connects, uploads a file over SFTP and leaves; the page shows the connection with `Client left` and, on the Transfers tab, the file as an upload.

**Proof** (U), with a real transfer:
```
scripts/tui-proof.sh docs/implemented-proof-images/10.5-history.txt --config scripts/testdata/two-people \
  --during 'scripts/proof-ssh.sh alice alice true' --wait '0 connected' --keys 5
```
It must show alice's connection with its duration and `Client left`. Then the same with `--keys 5 --keys t` into `10.5-history-transfers.txt` after an upload (add a `scripts/proof-sftp.sh` beside `proof-ssh.sh` if there is no way yet to upload through the fixture's keys); it must show the file as an upload. Afterwards `pgrep -a ssh` shows nothing of yours.

**Finish**: rules 10–15.

### Step 10.6: The desktop's History page

**Goal**: `cave` has a sixth page, History, as SPEC §11.8 says.

**Read**: SPEC §11, §11.8. The box at the top of M6. `internal/gui/page_attempts.go` (the table, the search, the details, the clear dialog: copy its shape), `shell.go` (the destinations), `root.go`, `app.go`.

**Files**: `internal/gui/page_history.go` and its test; `shell.go`, `root.go`, `app.go`; the goldens that show the navigation.

**Tasks**

1. A destination History (an icon of history or a clock from `m.Icons`; look it up) between Connections and Settings, in the rail and the bottom bar.
2. A segmented button Connections | Transfers, a search field, the line about clearing with a link to the setting, and "Clear history" (a dialog that asks first; its error in the dialog).
3. The two tables of §11.8 with the words of `internal/app`; a selected row shows its details beside the table (wide) or under it (narrow), with the connection's transfers under a connection.

**Tests**

- `TestGoldenHistory`: light-wide and dark-narrow on each tab, a selected connection, a selected transfer, the empty fake, the clear dialog. Open every PNG.
- The navigation goldens of the other pages are updated: open every changed PNG.
- With the fake: the clear dialog → `ClearHistory()`; the segmented button switches; the search narrows the rows.
- With the real application: a connection and an SFTP upload appear on the page.

**Proof** (D): `scripts/proof.sh docs/implemented-proof-images/10.6-history.png --config scripts/testdata/two-people --during '<the commands of step 10.5>' -- --page history`. It must show the connection and its duration.

**Finish**: rules 10–15.

### Step 10.7: The manual for the history

**Goal**: the guide and the README say what the history keeps, where, for how long, and how to clear it.

**Files**: `README.md`, `docs/GUIDE.md`, `docs/SECURITY.md` (the history file: its mode, that it holds names of files and addresses, never content).

**Tasks**

1. The guide: a section "History" (the two tabs, the reasons in words, what is a transfer and what is not: `rsync` and `scp -O`, the file, its limit and rotation, `history.clear`, "Clear history"); the History page in the TUI's keys.
2. README: the history in the features.
3. **Last step of M10**: a second commit sets `devVersion` to `0.12.0-dev`; then merge, tag `step-10.7`, publish as `v0.11.0` (rule 14).

**Finish**: rules 10–15. No proof.
