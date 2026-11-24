# Cave: Implementation guide

This file breaks the milestones in `docs/SPEC.md` §18 into small steps. Each step is one unit of work for one coding agent session. Work goes strictly top to bottom. Every finished step is recorded in `docs/IMPLEMENTED.md`.

**Who this is for**: coding agents (and humans) implementing Cave. You don't need to read the whole file; you need the Rules below and the one step you're doing.

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
    If the step lists **Display tests**, run them too: `scripts/heavy.sh --alone -- scripts/headless-wayland.sh -- go test -race -tags 'cave_display debug_mode' <packages>`. Golden tests have `Golden` in their name, so `go test ./... -run Golden -update-goldens` finds them all.
11. **Proofs.** Only when the step has a **Proof** item:
    - Produce exactly the files it lists, in `docs/implemented-proof-images/`, named `<id>-<name>.<ext>` (e.g. `5.3-people.txt`, `6.2-people.png`), with the method the step gives ("Proof methods" below).
    - **Open every proof** (the Read tool shows images and text) and check that it shows what **Proof** describes. If it doesn't, fix the code. The step isn't done.
    - Never draw, edit, crop or write a proof any other way, and never reuse an older one.
12. **Record the step.** On the step branch, add an entry to `docs/IMPLEMENTED.md`, using the format at the top of that file. Embed or link the proofs.
13. **Commit on the step branch.** Use one commit per step, with the message `Step <id>: <imperative summary>`:
    ```sh
    git add -A && git commit -m "Step 1.2: Add people and their keys"
    ```
14. **Merge every finished step into `main`; publish to GitHub once per milestone.**
    - Merge a step only after rules 10–13 are complete: checks green, proof verified, `IMPLEMENTED.md` entry committed. Never collect several steps into one merge, and never leave a finished step only on its branch.
    - A step counts as finished when its commit is on the local `main` and tagged `step-<id>`; start the next step from there.
    ```sh
    git switch main
    git merge --ff-only feat/step-1.2-people
    git tag step-1.2
    git branch -d feat/step-1.2-people
    ```
    - **Publish a milestone.** When the last step of a milestone (a `## M<n>` section below) is merged, **ask the user for the version number** (CLAUDE.md "Git and GitHub"), tag it, and push `main` and the tags. The milestone counts as published when that commit is on `origin/main`.
    ```sh
    git tag -a vX.Y.Z -m "M<n>: <title>"
    git push origin main
    git push origin --tags
    ```
    If `--ff-only` fails, rebase the step branch onto `main`, run the standard checks again, and retry. If a push is rejected or fails (authentication, network), retry a few times; if it keeps failing, stop and tell the user. Don't start the next milestone's first step until the milestone's push succeeded.
15. **Report to the user.** Give the step id, a two-line summary, the check results, the proof paths, new `docs/GLUTTER-ISSUES.md` entries, new manual checks, and the commit hash now on `main` (and, at the end of a milestone, that it is on `origin/main`). Then start the next step.

### Proof methods

| | Method | How |
|---|---|---|
| T | Transcript | The verbose output of the named Go tests, saved as text: `scripts/transcript.sh <out.txt> -- go test -v -count=1 -run <pattern> <package>` (step 0.5). It removes temporary paths and timings, so the file is stable |
| U | TUI capture | `scripts/tui-proof.sh <out.txt> [--config <fixture dir>] [--size 100x30] [--keys '<tmux keys>']... -- <cave-tui args>` (step 5.1): the TUI runs isolated in a private `tmux` server, gets the keys, and the screen is saved as text |
| G | Golden | A golden file made by a `Golden` test (PNG through Glutter's software renderer, text for the TUI), copied to the proofs folder by the step's command |
| D | Desktop screenshot | `scripts/proof.sh <out.png> [--config <fixture dir>] [--after <duration>] -- <cave args>` (step 6.1): the desktop app runs isolated in a headless compositor and `grim` saves the screen |
| W | Wine screenshot | `scripts/wine-proof.sh <out.png> ...` (step 7.5): the same for the Windows program under Wine |

---

## M0: Foundations

### Step 0.1: The module and the checks
**Read**: SPEC §3, §4.1, §16; CLAUDE.md "Commands", "Definition of done", "Memory (OOM)".
**Tasks**
- `go mod init github.com/rm4n0s/cave` (Go 1.27); add `github.com/rm4n0s/errors`.
- `internal/app/version.go`: `devVersion` and `Version()` (SPEC §16).
- `cmd/cave` and `cmd/cave-tui`: a `main` that parses `--version` and prints `cave <version>` / `cave-tui <version>`, and otherwise says that the program is not built yet and exits 1. Each `main` calls a testable `run(args, stdout, stderr) error`.
- `.golangci.yml` (the linters of Emetos: the standard ones, `revive` with `exported` and `package-comments`, `gofmt`; `testdata` excluded).
- `.gitignore`: `/cave`, `/cave-tui`, `/dist/`, `.wine-test/`, `*.actual.png`, `*.diff.png`, `*.actual.txt`.
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
- `internal/core/storage`: `Dirs{Config, State, Runtime}`; `Resolve(Options{ConfigDir, StateDir}, env)` by SPEC §5.1 on Linux and Windows; `(Dirs).Ensure(ctx)` (mode `0700`); `WriteFileAtomic(ctx, path, data)` (SPEC §5.3); `ReadFile(ctx, path, maxBytes)` (a missing file is reported with the sentinel `ErrNotExist`; a file over the limit is `FileTooLarge`); `Lock(ctx, dirs, program)` and `(*Lock).Release()` (SPEC §5.4; `flock` in `_linux.go`, `LockFileEx` in `_windows.go`; `AlreadyRunning` has the holder's program and PID in its metadata).
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

### Step 4.2: Commands and messages
**Read**: SPEC §10, §7, §8.4, §9.10.
**Tasks**: the commands of SPEC §10 for people, keys, attempts, connections and the host key; `Accepted` and `Rejected` events (the latter at most one a minute per SPEC §6); `Messages`: a sentence for every Tag a command can return, and a test that walks the error-tag tool's list so that no Tag reachable from a command lacks one.
**Tests**: each command's success and errors through the app; removing a person disconnects them; `NewHostKey` changes the fingerprint for the next connection; a full scenario with the real `ssh` (add a person, connect, see the connection and the accepted entry, disconnect, clear).
**Proof** (T): `4.2-app.txt`, the transcript of the scenario test.

---

## M5: TUI

### Step 5.1: The TUI's frame
**Read**: SPEC §12, §11.1; Bubble Tea v2's `Program`, `Model`, `View`, key and window-size messages, and Lip Gloss v2's styles, in the module cache.
**Tasks**: add `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2`; `internal/tui`: the root model over the interface it needs of `*app.App`; header, footer, page switching, the help overlay, "Terminal too small", the quit question, `NO_COLOR`; events of the app arrive as messages; the Overview page (state, port, fingerprint, how to connect, counts, the start error, `s`); `tui.Run(ctx, app, in, out)`; `cmd/cave-tui`: flags, `app.Open`, `tui.Run`, errors as sentences on standard error. `scripts/tui-proof.sh` (method U).
**Tests**: model tests with text goldens at 100×30 and 60×16, in color-less and color profiles: each state of the server, the error state, no people; page keys; the quit question only with connections; too small. A test that the program's output goes nowhere but the TUI's writer.
**Proof** (U): `5.1-overview.txt` (running, with a fixture of two people), `5.1-help.txt`.

### Step 5.2: The TUI's widgets
**Read**: SPEC §12.
**Tasks**: in `internal/tui`: a scrolling list, a table with fixed and flexible columns cut with `…`, a one-line text field (cursor, editing keys, paste), a multi-line field (bracketed paste of several lines), a question box (yes/no, default no), a message line for errors. Width is measured in terminal cells (wide and combining characters).
**Tests**: each widget's keys and rendering as text goldens; cutting with wide characters; a paste with control characters is cleaned; the question's default.

### Step 5.3: The People page
**Read**: SPEC §12, §11.2, §7.
**Tasks**: the list, add, rename, enable, remove (with the number of connections it closes), the person's keys, add by paste, import by path, edit comment, remove key; errors shown at the field with `Messages`.
**Tests**: each flow through key messages over a real `App` in a temporary folder; each error of SPEC §7 shown; goldens.
**Proof** (U): `5.3-people.txt`, `5.3-person-keys.txt`, `5.3-add-key-error.txt`.

### Step 5.4: The Attempts and Connections pages
**Read**: SPEC §12, §11.3, §11.4, §8.
**Tasks**: the attempts table with filter, search, details, remove, clear, the cleared/next line and the wording of results; the connections list with disconnect and disconnect all; times shown relative ("3 min ago") with the fake clock in tests.
**Tests**: goldens with a fixture list containing every outcome and a hostile login name (it must appear sanitized); filter and search; clear asks; disconnect closes a real client.
**Proof** (U): `5.4-attempts.txt` made by really connecting with `ssh` (a right key, a wrong key, an unknown name) to the isolated TUI, `5.4-connections.txt` with a live `ssh` session.

### Step 5.5: The Settings page
**Read**: SPEC §12, §11.5, §6.
**Tasks**: every setting of SPEC §6 that applies to the TUI (the desktop-only ones are shown greyed with "desktop app"), editing by kind, validation messages, the host key and "New host key" (asks), the folders, the version.
**Tests**: each kind's editing; a bad port's message; changing `attempts.clear` updates the next-clear line on the Attempts page; goldens.
**Proof** (U): `5.5-settings.txt`.

---

## M6: Desktop

### Step 6.1: The window and the Overview page
**Read**: SPEC §11, §11.1; Glutter's `docs/GUIDE.md`, `examples/tray`, `examples/navigation`, `gluttertest`, and its `scripts/screenshot-headless.sh`.
**Tasks**: add `github.com/rm4n0s/glutter`; `internal/gui`: `Run(ctx, app)`, the window, the theme from `ui.theme`, the navigation rail and the bottom bar below 800 pixels, the bridge from the app's events to `SetState`; the Overview page; `internal/gui/testutil` (test fonts, a pump over a real `App` in a temporary folder); `cmd/cave`: flags, `app.Open`, `gui.Run`, a dialog for an error at start. `scripts/proof.sh` (method D).
**Tests**: widget tests and PNG goldens (light and dark, wide and narrow) of each server state; the Start/Stop button drives the app; copy buttons put the text on the fake clipboard.
**Display tests**: the app opens a window in headless sway and closes cleanly.
**Proof** (D): `6.1-overview.png`, `6.1-overview-dark-narrow.png`.

### Step 6.2: The People page
**Read**: SPEC §11.2, §7.
**Tasks**: the page of SPEC §11.2 with dialogs for add, rename, add keys, edit comment, and the confirmations; "Import from file…" through `FakeDesktop` in tests.
**Tests**: each flow; each error under its field; goldens.
**Proof** (D): `6.2-people.png`; (G) `6.2-add-key-error.png`.

### Step 6.3: The Attempts page
**Read**: SPEC §11.3, §8.
**Tasks**: the table, filter, search, details, remove, clear, the cleared/next line with its link to the setting.
**Tests**: goldens with the fixture of step 5.4; sorting by a column; the hostile name is drawn as text; clear asks.
**Proof** (D): `6.3-attempts.png`, made after real `ssh` attempts as in step 5.4.

### Step 6.4: The Connections page and quitting
**Read**: SPEC §11.4, §11.6 (closing).
**Tasks**: the page; the quit question when people are connected; the end of the desktop session quits without asking.
**Tests**: the list follows a real client connecting and leaving; disconnect; the question appears only with connections (`ExitRequestReason` through the tester).
**Proof** (D): `6.4-connections.png` with a live `ssh` session.

### Step 6.5: The Settings page
**Read**: SPEC §11.5, §6.
**Tasks**: every setting in its group with its explanation, validation under the field, the host key and "New host key…", the folders, the version.
**Tests**: each control changes the setting and the app follows (theme, port); goldens.
**Proof** (D): `6.5-settings.png`.

### Step 6.6: Tray and notifications
**Read**: SPEC §11.6; Glutter's `desktop` package and `examples/tray`.
**Tasks**: the tray icon and menu, its state; notifications by `notify.*`; `ui.closeToTray`.
**Tests** (`FakeDesktop`): the menu's items act; the icon follows the server's state; a notification for an accepted connection and at most one a minute for refused ones; close hides with a tray and quits without one.
**Manual check**: the tray and a notification on the user's real desktop.

---

## M7: Windows

### Step 7.1: Sessions on Windows
**Read**: SPEC §13.1, §13.2.
**Tasks**: replace `SessionUnsupported`: the shell lookup, the command lines of SPEC §13.2, the environment, pipes, job objects, `signal`, exit statuses. Make every core test that does not need a PTY, SFTP or the agent pass under Wine (those that cannot say why in `t.Skip`).
**Tests** (Wine): `exec` output and exit status through `cmd.exe`; the environment table; the job ends a child tree when the channel closes; the storage, settings, people, attempts, host key and server authentication tests.
**Proof** (T): `7.1-wine-sessions.txt`.

### Step 7.2: ConPTY
**Read**: SPEC §13.3; `windows.CreatePseudoConsole` and the process attribute list in `golang.org/x/sys/windows`.
**Tasks**: replace `PtyUnsupported` with ConPTY; `window-change`.
**Tests** (Wine, when its ConPTY works; otherwise skipped with the reason and a manual check for a PC): a `shell` session echoes a command's output; resize.

### Step 7.3: SFTP on Windows
**Read**: SPEC §13.4.
**Tasks**: the Windows file-system implementation behind the interface of step 3.1: paths, drives at `/`, attributes, the unsupported answers.
**Tests** (Wine): path mapping table (pure, runs everywhere); put, get, list, rename, remove in a temporary folder; `/` lists `C:`.

### Step 7.4: Agent forwarding on Windows
**Read**: SPEC §13.5; the named pipe functions of `golang.org/x/sys/windows`.
**Tasks**: replace `AgentUnsupported` with the named pipe (security descriptor for the current user only); forwarding tests of steps 3.3–3.4 pass under Wine.
**Tests** (Wine): a client in the session (a Go helper built for the test) talks the agent protocol over the pipe and lists the key; the pipe is gone afterwards.

### Step 7.5: The programs on Windows
**Read**: SPEC §13.1, §16; Glutter's `cmd/glutter-windows` and `scripts/wine-screenshot.sh`.
**Tasks**: `scripts/build-windows.sh` (`cave-tui.exe` as a console program; `cave.exe` through `glutter-windows build` with the icon and version); `scripts/wine-proof.sh` (method W); fix what the TUI and the desktop app need on Windows.
**Tests** (Wine): the TUI model tests; `cave-tui.exe --version`; the desktop app draws its first frame (`cave_wine` tag).
**Proof** (W): `7.5-windows-overview.png`.
**Manual check**: on a real Windows PC: ConPTY shell from OpenSSH, the tray, a notification, `ssh -A`.

---

## M8: Release

### Step 8.1: The manual
**Read**: SPEC, all of it.
**Tasks**: `README.md` (what Cave is, install, first use, screenshots from the proofs) and `docs/GUIDE.md` (people and keys, the attempts list and its clearing, what a connected person can do, settings, files and folders, troubleshooting); `--help` of both programs.
**Tests**: `--help` goldens; a test that every setting key of SPEC §6 appears in the guide.

### Step 8.2: Packaging
**Read**: SPEC §16.
**Tasks**: an icon drawn by a small Go program (`internal/gui/icon`, PNG sizes and the tray's); `packaging/cave.desktop`; `scripts/build-release.sh` (Linux amd64 and arm64, Windows amd64 and arm64, into `dist/`, with checksums).
**Tests**: the icon's goldens; the desktop file validates (`desktop-file-validate` when installed); the release script builds every target.

### Step 8.3: The security pass
**Read**: CLAUDE.md "Security"; SPEC §7–§9.
**Tasks**: `docs/SECURITY.md`: what Cave protects against, what it does not, how to report; go through every rule of CLAUDE.md "Security" and every limit of SPEC §8.5, §9.2, §9.3, §9.4, §9.7, §9.8 and name the test that proves it in a table; add the missing tests; a bounded flood test (thousands of connections on loopback: memory stays bounded, the list stays at its limit, the app stays responsive); read every `TODO` left and resolve it.
**Tests**: those the table lacked.
