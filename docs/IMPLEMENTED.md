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

## Step 0.5: Scripts for isolation, transcripts and Wine
- **Date**: 2026-11-24
- **Branch**: feat/step-0.5-scripts
- **Summary**: `scripts/isolated.sh` runs a command with Cave's folders, the home, the XDG folders and the runtime folder in a temporary folder and without the user's SSH agent; `scripts/transcript.sh` saves a command's output as a stable proof; `scripts/headless-wayland.sh`, `scripts/headless-x11.sh`, `scripts/wine-exec.sh` and `scripts/test-windows.sh` are Cave's copies of Glutter's scripts. `scripts/checks.sh` now runs the Windows tests under Wine. `devVersion` is `0.2.0-dev`: this is the last step of M0, published as `v0.1.0`.
- **Decisions**: `scripts/wine-exec.sh` forces Mesa's software renderer (`LIBGL_ALWAYS_SOFTWARE`, Mesa's EGL and GLX vendors): Wine's X driver starts EGL even for a console program, and without this it probed the NVIDIA card. `scripts/isolated.sh` also unsets `SSH_ASKPASS`, and exports `CAVE_ISOLATED_ROOT`. A transcript is written only when its command succeeds.
- **Checks**: `scripts/checks.sh` passed, every check: gofmt, vet for Linux and Windows, golangci-lint (0 issues), race tests as a debug build, cgo-free build and tests, Windows build, error tags (none untested), and the tests under Wine (`internal/core/storage`). `pgrep` shows no Wine, Xvfb or compositor left.
- **Tests**: `scripts/isolated.sh` through a Go test: every variable points into the private folder, the SSH variables are gone, the folders have mode 700 and are removed, a fixture becomes the configuration, the exit status is kept, `--keep`, usage mistakes; `scripts/transcript.sh`: durations, ports and temporary paths are taken out, a failing command writes nothing. Under Wine (wine-11.21): the Windows tests of `internal/core/storage` pass, including the lock through `LockFileEx` and the `%AppData%` resolution.
- **Proof**: [0.5-wine-storage.txt](implemented-proof-images/0.5-wine-storage.txt), the transcript of the storage tests under Wine.
- **Manual checks**: none
- **Glutter issues**: none

## Step 1.1: Settings
- **Date**: 2026-11-24
- **Branch**: feat/step-1.1-settings
- **Summary**: `internal/core/settings`: the sixteen settings of SPEC §6 as one table (`Schema`), typed keys and getters (`Bool`, `Int`, `String`, `Strings`, so a getter of the wrong kind does not compile), and `Store`: `Open` reads `settings.json`, `Set` validates, writes the file atomically and then tells the listeners, `Snapshot` is an immutable view. `MatchEnvName` matches the patterns of `session.acceptEnv`. `internal/core/errtest.Route` is the route assertion shared by the tests from here on.
- **Decisions**: a bad value in the file is `SettingsFileValueInvalid` and a bad value given to `Set` is `SettingInvalid` (a Tag is created at one place; SPEC §6 updated). The file holds only the settings that were set, and setting a value a setting already has does nothing (no write, no event). Any version other than 1 is refused, a missing one included. `server.address` takes an IP literal without a zone; `session.acceptEnv` takes at most 64 patterns of letters, digits, `_` and `*`; `session.shell` at most 4096 bytes without NUL. A whole number in the file may be written `2022.0`.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run of `internal/core/storage`).
- **Tests**: the defaults against the table of SPEC §6; set and reopen; 38 bad values covering every setting, none of which changes the value, writes the file or tells a listener; unknown keys kept through a rewrite; twelve broken files refused and left byte for byte; the same value set twice; listeners' order and cancel; a write that fails and a state that cannot be encoded; four goroutines setting at once under the race detector; 25 pattern cases, with one that would blow up a naive matcher.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 1.2: People and their keys
- **Date**: 2026-11-24
- **Branch**: feat/step-1.2-people
- **Summary**: `internal/core/people`: `ParseKeys` reads `authorized_keys` text strictly (every line must be an acceptable key); `Store` keeps people and keys in `people.json` with `Add`, `Rename`, `SetEnabled`, `Remove`, `AddKeys`, `ImportKeys`, `SetKeyComment`, `RemoveKey`, `List`, `Get` and `Subscribe`; `Lookup(name, key)` answers steps 2–5 of SPEC §7.3 from an immutable state, so authentication never waits for an edit. `internal/core/ids` makes the record ids. Adds `golang.org/x/crypto`.
- **Decisions**: (SPEC §7 updated) text without a key is `KeyTextEmpty`; a key repeated in one text is taken once; a `people.json` that breaks a rule is `PeopleFileInvalid`, and Cave does not repair it; missing records are `PersonNotFound` and `KeyNotFound`. A line whose key data names a type the library cannot parse is still `KeyTypeUnsupported` (its type is read from the data), not `KeyParseFailed`. A private key is recognised by `PRIVATE KEY-----` anywhere in the text and its text goes nowhere. The file keeps a key as `<type> <base64>` and the comment apart. A change that sets what is already there (same name, same enabled state, same comment) writes nothing and tells nobody. Times are UTC to the second.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run).
- **Tests**: all seven accepted key types (keys generated by the tests; security keys built from their wire form); 20 refusals (garbage, cut keys, options, RSA 1024, DSA, a certificate signed by a test CA, an unknown type, private keys) with line numbers; the private key's text is in no message, print or logged form; hostile comments and type names made safe; names (9 accepted, 20 refused, 4 taken ignoring case); every command's success, no-op and errors by route; `Lookup` for 11 cases and after each kind of edit; the file's mode, shape and round trip; 21 broken files refused and left untouched; every command when the file cannot be written (nothing changes, nobody is told); four writers and four readers under the race detector.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 1.3: The attempts list
- **Date**: 2026-11-24
- **Branch**: feat/step-1.3-attempts
- **Summary**: `internal/core/attempts`: `List.Record` counts an attempt into the entry of its address, login name, outcome and person, after making every text safe and cutting it; at most 5 000 entries, the oldest dropped; `Entries`, `Remove`, `Clear`; the clearing policies (`ApplyPolicy`, `PolicyChanged`, `LastClearedAt`, `NextClearAt`); the file `attempts.json`, written 5 seconds after the first change and by `Flush`.
- **Decisions**: (SPEC §8.5 updated) a failed delayed write is logged with the error and retried by the next change; reading the file re-sanitizes its texts, merges entries of one group, caps counts and keeps the newest 5 000; an entry that cannot be is `AttemptsFileInvalid`. `Open` takes `Options{Dirs, Clock, Logger}`. The group of an entry is made of the sanitized texts, so the same hostile name always lands in the same entry. A policy is applied by the caller (`ApplyPolicy(policy, atStart)`), which step 4.1 calls at start and once a minute. A periodic policy without a stored time (a file from before it) starts its period instead of clearing.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run).
- **Tests**: counting into one entry and the ten ways to get another; hostile login names, versions, addresses and keys stored safe and cut; the 5 000 limit (the oldest dropped, one refreshed by a new attempt kept); order; remove and clear; every policy with the fake clock (before, at and after the time, overdue while closed, a change restarting the period); one write per 5 seconds through a burst of 100 records, none without a change, `Flush` cancels the timer; the file's round trip; 11 broken files refused untouched; a failed delayed write logged as JSON with its route and recovered; eight writers under the race detector.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 1.4: The host key
- **Date**: 2026-11-24
- **Branch**: feat/step-1.4-hostkey
- **Summary**: `internal/core/hostkey`: `LoadOrCreate` reads `ssh_host_ed25519_key` (OpenSSH's private key format, mode `0600`) or makes it at the first start; `Create` replaces it; `Signer`, `Fingerprint` and `AuthorizedKey`. The `.pub` file is written beside it and repaired when missing or different. `devVersion` is `0.3.0-dev`: this is the last step of M1, published as `v0.2.0`.
- **Decisions**: (SPEC §9.10 updated) a key file that cannot be read fails with the errors of `storage`; one that is not a private key is `HostKeyParseFailed`; a private key of another type is `HostKeyTypeUnsupported`. A new key is read back from the disk before it is used. `docs/ERROR-TAGS-UNTESTED.md` now exists with its first entry, `HostKeyEncodeFailed` (the encoding of a fresh Ed25519 key cannot fail).
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run).
- **Tests**: created once and the same over three loads, without rewriting; the files' modes and contents; the key signs and verifies; `Create` replaces; the public file repaired; six broken or foreign key files refused, left untouched and given no public file; no randomness; folders gone; cancelled contexts. With OpenSSH 10.6 installed: `ssh-keygen -l` prints Cave's fingerprint for the public file, and `ssh-keygen -y` derives the same public key from the private file.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 2.1: The test harness
- **Date**: 2026-11-24
- **Branch**: feat/step-2.1-sshtest
- **Summary**: `internal/core/sshtest`: `NewKey` (Ed25519, ECDSA or RSA, with a private key file of mode `0600` under the test's folder), `ClientConfig`/`Dial`/`DialErr` (the client inside the test, which accepts only the host key it is given), `Require`, `OpenSSH` (the real `ssh`, `sftp` and `scp` with `-F none`, a `known_hosts` of its own, `IdentitiesOnly`, no agent, no prompt, a home under the test's folder, a 30-second limit) and `StartAgent` (a real `ssh-agent` on a socket under a temporary folder, killed when the test ends).
- **Decisions**: `OpenSSH.SSH(stdin, user, key, options, command...)` takes ssh's extra options apart from the command. `UseAgent` turns `IdentitiesOnly` off and names the test's agent, because ssh keeps the first value an option gets. The harness never uses `InsecureIgnoreHostKey`: both the client inside the test and OpenSSH check the host key.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run).
- **Tests**: against a minimal `x/crypto/ssh` server inside the test (not Cave's): the three kinds of key and their files; the client inside the test runs a command, and a wrong key, a wrong name and a wrong host key are errors; the real `ssh` (OpenSSH 10.6) runs a command with input, both output streams and the exit status, gets "Permission denied (publickey)" for a wrong key and "Host key verification failed" for a wrong host key without changing `known_hosts`; the isolating options and environment are on every command, with the user's `SSH_AUTH_SOCK` and `HOME` set to prove they do not get through; a test agent holds the keys, lets `ssh` log in with a key that is only in it, and is gone after the test.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 2.2: Listening, the handshake and authentication
- **Date**: 2026-11-24
- **Branch**: feat/step-2.2-server
- **Summary**: `internal/core/server`: `Server` with `New`, `Start`, `Stop`, `State`, `Addrs`, `SetHostKey` and `Subscribe`. Every connection gets its own `ssh.ServerConfig` (identification `SSH-2.0-Cave_<version>`, the Ed25519 host key, public key authentication only, RSA only with SHA-2, 6 tries) whose callbacks ask `people.Lookup` and feed a tracker; when the handshake ends, the tracker names the outcome of SPEC §8.1 and the attempt is recorded and logged. Limits: 30 seconds to authenticate (a timer of the clock that closes the connection), 64 waiting connections, 8 per address; one more is closed at once and recorded as `refused`. `people.Store.ByName` is new.
- **Decisions**: (SPEC §8.1 updated) authentication starts when the key exchange has ended, so a client that leaves right after it is `noKey`; a key offered with RSA/SHA-1 is not looked at and counts as no key; lines that are not an identification line are waited for, as the protocol allows, and ended by the time limit. Every refused key gets the same error object, so the three refusals share one code path. `Options.Address` overrides where to listen (the tests' `127.0.0.1:0`, later `--port`). An accept that fails while the listener is open is logged (`ServerAcceptFailed`) and retried after 100 ms. `Stop` waits for the goroutines until its context ends (`ServerStopTimedOut`). After authentication every channel and request is refused until steps 2.3 and 2.4.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run).
- **Tests**: 18 client behaviors, each producing its entry (the right key, a wrong key first, RSA with SHA-2 and with SHA-1, unknown and wrong keys, somebody else's key with its owner named, unknown and hostile names, a disabled person, no key, a password, a key offered but not signed for, a signature of something else, an HTTP request, a silent client, a flood of bytes); the five refusals give the client the identical error; edits apply to the next attempt; 30 keys end in a disconnect with 8 fingerprints kept; the identification line and a replaced host key; the limits with real connections from 127.0.0.1–127.0.0.9 (the 9th of an address and the 65th of all are closed without a banner, authenticated clients do not count); the time limit with the fake clock; start, stop and restart with the states told to a listener; a taken port; a failing accept; a listener that will not end. The real `ssh` (OpenSSH 10.6) gets "Permission denied (publickey)" for a wrong key and an unknown name, and is let in with the right key.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 2.3: Connections
- **Date**: 2026-11-24
- **Branch**: feat/step-2.3-connections
- **Summary**: the registry of authenticated connections (`Server.Connections`, `Disconnect`, `DisconnectAll`), each with its person, key, address, client version, start time and activities; the dispatch of what a client asks for to handlers registered by type (`HandleChannel`, `HandleRequest`), with `Conn` as the handlers' view of the connection (`AddActivity`, `SSH`, the person, the addresses); the limit of 256 channels per connection; `keepalive@openssh.com` answered and `no-more-sessions@openssh.com` obeyed; connections closed when their person is renamed, disabled or removed or their key is removed.
- **Decisions**: `Subscribe` now tells which kind of change it was (`StateChanged`, `ConnectionsChanged`). A channel handler returns when its channel is done, and a connection waits for its handlers before it leaves the registry, so `Stop` returns with nothing left running. A channel type or request without a handler is refused (`unknown channel type`, a failure reply). After a connection is put in the registry, its person is looked up once more, so that a change made between the key's check and the registry still ends it. Handlers are registered before `Start`; the maps are replaced, never changed.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run). The server's tests were also run 8 times in a row under the race detector.
- **Tests**: the registry's content, order and events while clients come and go; `Disconnect` of one, of one that is gone, and of all; nine changes of the people, each closing exactly the connections it should (and adding a key, a comment, a person closing none); the 256-channel limit per connection and room again after a close; unknown channel types; global requests with and without a handler and their payloads; no more sessions; activities shown, cut to 80 characters, made safe and removed; `Stop` leaves an empty registry and no running handler; a person disabled during the handshake does not stay connected.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 2.4: Sessions without a PTY
- **Date**: 2026-11-24
- **Branch**: feat/step-2.4-sessions
- **Summary**: `internal/core/session`: the handler of `session` channels (`Register`): `env`, `shell`, `exec`, `signal`, `break`; `BuildEnv`, a pure function that makes the environment of SPEC §9.5 from scratch; `PickShell`, `CurrentUser` and the `/etc/passwd` reader; the process on three pipes in a process group of its own; `exit-status` and `exit-signal`; the 2-second wait for output held by descendants; `SIGHUP` to the group when the channel or the connection ends first; the session shown in the registry and logged. `internal/core/server/servertest` is the server fixture shared by the tests of this and the next packages. On Windows `newCommand` returns `SessionUnsupported` until step 7.1.
- **Decisions**: (SPEC §9.4 and §9.5 updated) what Cave sets is put over what the client set, so a client cannot set who or where the session is whatever `session.acceptEnv` allows; an `env` request after the start is refused; a process that cannot start is granted its request and reported on the error stream with status 255, as with `sshd`; a signal the protocol has no name for is reported as status 128 plus its number. The OS user and Cave's own environment are given to the handler (`Options.User`, `Options.Environ`), so tests run `/bin/sh` in an isolated home with a made-up environment. A session's process is given `*os.File` pipes, so nothing but the process's exit decides when it has ended.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run of `session` and `storage`). The session tests were run 4 times in a row under the race detector; `pgrep` shows no process of theirs left.
- **Tests**: 14 commands (output on both streams, exit statuses 0, 3 and 127, input and its end, input that is not read, signals with and without a name in the protocol, the working folder, quoting); a megabyte of every byte value through `cat`; the session's environment compared line by line with the eleven variables of SPEC §9.5, with secrets in Cave's and in the test's environment; `env` requests accepted and refused by pattern and by the limits; a shell fed commands, started as a login shell; one start per channel; ten signal requests; a process that outlives its session gets `SIGHUP` (channel closed, connection ended, disconnected by the user) and one that ignores it goes on; a background child holding the pipe ends the session after 2 seconds; the registry and the log; 20 sessions at once; a missing home; a shell that does not exist and pipes that cannot be made (status 255, the error logged with its route, no pipe left open); `Stop` ends the sessions.
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 2.5: PTY sessions
- **Date**: 2026-11-24
- **Branch**: feat/step-2.5-pty
- **Summary**: `internal/core/pty`: `Open(size, modes)`, `Resize`, `Close`, `Name`, `Master`, `Slave` and `CloseSlave` on Linux without cgo (`/dev/ptmx`, `TIOCGPTN`, `TIOCSPTLCK`, `TIOCSWINSZ`); `ParseModes`, the decoder of the terminal modes of RFC 4254 §8, and their application through `termios` (control characters, the input, local, output and control flags, the character sizes; speeds and the opcodes Linux does not have are skipped); `Size.Check` (1 to 4096 columns and rows). On Windows `Open` returns `PtyUnsupported` until step 7.2. `internal/core/session`: `pty-req` and `window-change`; with a PTY the process leads a new session whose controlling terminal is the PTY, its three streams are the PTY, `TERM` and `SSH_TTY` are set, and the end of the channel or of the connection closes the PTY, which is how the kernel sends `SIGHUP`.
- **Decisions**: (SPEC §9.4 and §9.6 updated) the terminal type must be printable ASCII without spaces of at most 128 characters, else `pty-req` is refused, because it is the client's text and becomes `TERM`; `pty-req` is granted once per channel and only before the start, and a refused one leaves the channel usable without a PTY; `window-change` without a PTY or with a size out of range is refused; a terminal has no end of input, so the client's end of file changes nothing, as with `sshd`; the 2-second wait for output also holds for a descendant that keeps the PTY. The package uses the descriptors through `SyscallConn`, never `File.Fd`: `Fd` takes a file out of the runtime's poller, after which closing the master no longer ends a read that waits for a process still holding the PTY (a test proves it). The process is started in one place for both kinds of session, so `SessionStartFailed` stays one failure site.
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run of `pty`, `session` and `storage`). The tests of `pty` and `session` were run 4 times in a row under the race detector; `pgrep` shows no process of theirs left.
- **Tests**: `pty`: sizes and their limits; the mode decoder (the end opcode, undefined opcodes, a list cut in a value: `PtyModesInvalid`); a terminal opened with its size and name, resized, with modes applied and read back; every step of `Open` failing (`PtyOpenFailed`, `PtySetupFailed`, `PtySlaveOpenFailed`, `PtyModesApplyFailed`, `PtyResizeFailed`) with nothing left open; the master sees the end when the slave is closed; closing ends a read that is waiting. `session`: `tty` prints the PTY's name, `SSH_TTY` equals it, `TERM` is the type asked, both streams arrive as one with terminal line ends, and the exit status; no terminal without `pty-req`; `stty size` shows the size asked and the size after `window-change`, and four sizes out of range and a request that does not parse change nothing; eleven `pty-req` refused (sizes, a cut mode list, terminal types with an escape sequence, a space, a line feed, non-ASCII, too long, a payload that does not parse), with the error and its route in the log, and the session going on without a terminal; a second `pty-req`, one after the start, and a terminal never used; modes seen by `stty -a` (the defaults, `ECHO` off, several at once, an opcode Linux does not have); a login shell on a terminal whose `$0` is `-sh`; the interrupt character typed into the terminal ends a `sleep` with `SIGINT`; a `sleep` ends when the channel is closed, the connection ends or the user disconnects it, and a job that ignores `SIGHUP` goes on; a background child that keeps the terminal does not hang the session; after every test, and after 20 sessions in a row, this process has no pseudo terminal open (counted in `/proc/self/fd`).
- **Proof**: none
- **Manual checks**: none
- **Glutter issues**: none

## Step 2.6: OpenSSH against Cave
- **Date**: 2026-11-24
- **Branch**: feat/step-2.6-openssh
- **Summary**: tests with the real OpenSSH programs (OpenSSH 10.6) against Cave, and the one fix they asked for. `internal/core/sshtest` got `StartMaster` (a master connection the following commands share, ended with `ssh -O exit`), `SSHCommand` (the isolated `ssh` command line for `rsync -e` and `GIT_SSH_COMMAND`), `RunWith`, `Dir`, and a line in the test's log for every command, so that a verbose run reads as a transcript. `devVersion` is `0.4.0-dev`: this is the last step of M2, published as `v0.3.0`.
- **Decisions**: (SPEC §9.6 updated) **a terminal dimension of 0 is "not said" and is ignored, no longer refused.** `ssh -tt` without a terminal of its own (a script, a cron job) asks for a PTY of 0 by 0; Cave refused it ("PTY allocation request failed"), which broke a standard client (SPEC §1) and contradicted RFC 4254 §6.2 ("Zero dimension parameters MUST be ignored"). Now `pty-req` with 0 leaves the size unknown to the system, as `sshd` does, and `window-change` with 0 keeps that dimension; more than 4096 columns or rows is still refused. The server's test with the real `ssh` was renamed `TestOpenSSHIsRefusedAndAccepted`, so that one pattern (`OpenSSH`) runs every test of this kind. `rsync` and `git` are not part of OpenSSH: their tests skip, saying so, when the program is missing (both are installed here and ran). `git` in the test reads no configuration of the user or the system (`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`).
- **Checks**: `scripts/checks.sh` passed, every check (gofmt, vet for Linux and Windows, golangci-lint 0 issues, race tests as a debug build, cgo-free build and tests, Windows build, error tags: none untested, the Wine run of `pty`, `session` and `storage`). The OpenSSH and PTY tests were run 3 times in a row under the race detector; `pgrep` shows no `ssh`, `rsync`, `git` or `sleep` of theirs left.
- **Tests**: `ssh` running ten commands (both streams, exit statuses 0, 1, 3 and 127, input and its end, input that is not read, quoting, who and where the session is, no terminal); 2 MiB of every byte value through `cat` and back; `ssh -tt` with a login shell fed commands (its `$0` is `-sh`, it is on a terminal, exit status 7, nothing on the error stream) and with a command; a master connection with two sessions at once (one connection with two sessions in the registry), a third after them, `ssh -O exit`, and one entry counted once in the attempts list; `scp -O` of a file and of a folder, up and down, and a missing file; `rsync -a -e ssh` up and down; `git clone` over `ssh://`, a commit and `git push` back. `pty` and `session`: a terminal whose size is not said, and dimensions of 0 in `window-change`.
- **Proof**: [2.6-openssh.txt](implemented-proof-images/2.6-openssh.txt), the verbose run of the OpenSSH tests of `internal/core/server` and `internal/core/session`. Its first test is the server alone, before sessions existed: the third `ssh` there is let in and ends with 255 because that server has nothing to run.
- **Manual checks**: none
- **Glutter issues**: none
