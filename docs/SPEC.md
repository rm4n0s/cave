# Cave: Specification

Cave does the job of `sshd` as an application that an ordinary user starts, watches and stops: a desktop application or a terminal UI. This file is the source of truth for what Cave does. `docs/IMPLEMENTATION.md` breaks it into steps.

## 1. Purpose

- A person who wants to let somebody into their computer over SSH starts Cave, adds that **person** with one or more **public keys**, and tells them `ssh -p 2222 <name>@<address>`.
- Cave shows **who tried to connect and how many times** (the attempts list), who is connected now, and lets the user disconnect them.
- Cave needs no root, no system service and no `sshd_config`. When Cave is not running, nobody can connect.
- Standard clients work: OpenSSH's `ssh`, `scp`, `sftp`, `rsync` and `git` over SSH, `sshfs`, PuTTY, and editors' remote modes that only need a shell, `exec` and SFTP.

## 2. Decisions

| # | Decision | Source |
|---|---|---|
| D1 | The development process and error handling are those of Emetos's `CLAUDE.md` | the user, 2026-11-24 |
| D2 | Go, cgo-free. The SSH protocol is `golang.org/x/crypto/ssh` | follows D1 |
| D3 | The desktop UI is built on Glutter, with its Material 3 widgets | the user, 2026-11-24 ("use Glutter library for the GUI"); Cave is a form-and-list app, so Material fits |
| D4 | The TUI is built on Bubble Tea v2 and Lip Gloss v2 (`charm.land/...`), and on nothing else of Charm (no `bubbles`): Cave's own list, table and text field are in `internal/tui` | the user, 2026-11-24 |
| D5 | Besides shell, `exec` and PTY, Cave has the SFTP subsystem, TCP port forwarding (`-L`, `-R`, `-D`) and agent forwarding. No X11 forwarding | the user, 2026-11-24 |
| D6 | Linux and Windows first, Windows tested under Wine; macOS later | the user, 2026-11-24 |
| D7 | The login name of a client must be the name of a person in Cave, and the key must be one of that person's | the user, 2026-11-24 |
| D8 | Two programs, `cave` (desktop) and `tcave` (terminal), over one application layer and the same files. One instance at a time per configuration folder | Windows separates GUI and console programs; the TUI must not need a display. The name `tcave`: the user, 2026-11-24 |
| D9 | Every session runs as the OS user who started Cave, with that user's rights | Cave has no root, so it cannot change user |
| D10 | Public key authentication only | §7 |
| D11 | The attempts list is cleared when the setting `attempts.clear` says, or by hand | the user, 2026-11-24 ("put in settings when the list gets cleared") |
| D12 | SFTP is written in Cave (protocol version 3); no dependency for it | follows "prefer the standard library" |

## 3. Platforms and requirements

- **Linux**: x86-64 and arm64, a kernel with `/dev/ptmx` and devpts. The desktop app needs what Glutter needs (Wayland or X11; Vulkan or OpenGL ES 3.0, else its software renderer). The TUI needs a terminal.
- **Windows**: 10 version 1809 or later (ConPTY), x86-64 and arm64. Section 13.
- **macOS**: not yet. No file may assume that "not Windows" means Linux: Linux-only code is in `_linux.go` files.
- Go 1.27. No cgo anywhere.

## 4. Architecture

```
cmd/cave ──► internal/gui ──┐
                            ├──► internal/app ──► internal/core/...
cmd/tcave ► internal/tui ┘
```

### 4.1 Packages

| Package | What it is |
|---|---|
| `cmd/cave` | `main` of the desktop app: flags, then `gui.Run` |
| `cmd/tcave` | `main` of the TUI: flags, then `tui.Run` |
| `internal/app` | The application: owns the stores and the server, gives snapshots, commands and events (§10). Also `version.go` and the flags both programs share |
| `internal/core/storage` | Folders, atomic writes, the instance lock (§5). `storage/storagetest` isolates tests |
| `internal/core/clock` | `Clock` (now, timers) and a fake for tests |
| `internal/core/sanitize` | `Display`: makes hostile text safe to store, log and draw (§8.3) |
| `internal/core/logging` | `slog` to a size-limited file (§15) |
| `internal/core/settings` | The settings: schema, defaults, validation, file (§6) |
| `internal/core/people` | People and their public keys: parsing, validation, the store, the lookup that authentication uses (§7) |
| `internal/core/attempts` | The attempts list: recording, counting, limits, clearing, file (§8) |
| `internal/core/hostkey` | The host key: create, load, fingerprint (§9.10) |
| `internal/core/server` | The listener, the handshake and its limits, authentication, the registry of connections, the dispatch of channels and global requests, keepalive (§9.1–9.3, 9.9) |
| `internal/core/session` | Session channels: `pty-req`, `env`, `shell`, `exec`, `subsystem`, `window-change`, `signal`; the environment; the process (§9.4, 9.5) |
| `internal/core/pty` | Pseudo terminals: devpts on Linux, ConPTY on Windows; terminal modes (§9.6) |
| `internal/core/sftp` | The SFTP version 3 server (§9.7) |
| `internal/core/forward` | `direct-tcpip` and `tcpip-forward` (§9.8) |
| `internal/core/agentfwd` | Agent forwarding: the socket or pipe that sessions see (§9.8) |
| `internal/core/netinfo` | The addresses of this computer, for "how to connect" |
| `internal/core/sshtest` | Test harness: keys, an in-process client, and the real OpenSSH programs, isolated (§14) |
| `internal/gui/...` | The desktop UI (§11); `internal/gui/testutil` has the test fonts and pumps |
| `internal/tui/...` | The terminal UI (§12) |
| `internal/archtest` | Tests of the rules of §4.2; `internal/archtest/cmd/errortags` lists error Tags and finds untested ones |

### 4.2 Boundaries (checked by `internal/archtest`)

1. `internal/core/...` imports nothing of `internal/app`, `internal/gui`, `internal/tui`, Glutter, Bubble Tea or Lip Gloss.
2. `internal/app` imports the core and no UI library.
3. `internal/gui` and `internal/tui` never import each other. Glutter appears only under `internal/gui` and `cmd/cave`; Bubble Tea and Lip Gloss only under `internal/tui` and `cmd/tcave`.
4. No file imports the standard library `errors`, and no non-test file calls `fmt.Errorf`.
5. No file contains `import "C"`.
6. `ssh.InsecureIgnoreHostKey` appears only in `_test.go` files and in `internal/core/sshtest`.
7. The first argument of `errors.New`, `errors.NewErr` and `errors.NewErrf` is a string literal or a constant whose name starts with `Tag`/`tag`, and every Tag is asserted by a test (a route in `HasRoute`, a comparison with `.Tag`, or `errors.Is` with `&errors.Error{Tag: ...}`); `docs/ERROR-TAGS-UNTESTED.md` lists the exceptions with a reason each. A Tag is created at one place only: every failure case has its own (the versions of one function in the files of different platforms may share one). No error is created outside a function.
8. The module's direct dependencies are those CLAUDE.md allows.
9. No Go file contains an invisible character (a control or format character other than the newline and the tab) or invalid UTF-8: such a character is written as an escape (`"\u202E"`), so that what a reviewer sees is what the compiler gets.

### 4.3 Threads

The server accepts each connection on its own goroutine; sessions, SFTP and forwards have theirs. The stores (`settings`, `people`, `attempts`) are safe for concurrent use. `internal/app` calls its subscribers from its own goroutines; a UI hands the event to its UI thread (`Post`/`SetState` in the desktop app, `Program.Send` in the TUI) and reads a fresh snapshot there.

## 5. Storage

### 5.1 Folders

| | Linux | Windows |
|---|---|---|
| Configuration | `$XDG_CONFIG_HOME/cave` (else `~/.config/cave`) | `%AppData%\Cave` |
| State | `$XDG_STATE_HOME/cave` (else `~/.local/state/cave`) | `%LocalAppData%\Cave` |
| Runtime (agent sockets) | `$XDG_RUNTIME_DIR/cave` (else a `0700` folder under the temporary folder) | named pipes, no folder |

`--config-dir` and `--state-dir` (or `CAVE_CONFIG_DIR`, `CAVE_STATE_DIR`) replace them; the flag wins over the variable. Folders are created with mode `0700`.

### 5.2 Files

| File | Folder | Content |
|---|---|---|
| `settings.json` | configuration | §6 |
| `people.json` | configuration | §7.1 |
| `ssh_host_ed25519_key`, `ssh_host_ed25519_key.pub` | configuration | §9.10 |
| `attempts.json` | state | §8 |
| `cave.log`, `cave.log.1` | state | §15 |
| `lock` | state | §5.4 |

Every JSON file is an object with `"version": 1`. A file with a higher version is refused (`…VersionUnsupported`) and never overwritten. A file that does not parse is refused (`…ParseFailed`) and never overwritten: the app shows the error and the path and offers to quit; it does not start with empty data over a broken `people.json`. A missing file means defaults (no people, default settings, an empty list).

### 5.3 Writing

Files are written atomically: a temporary file in the same folder with mode `0600`, `fsync`, rename over the target, `fsync` of the folder (Linux). A failed write leaves the old file.

### 5.4 One instance

`cave` and `tcave` take an exclusive lock on `<state>/lock` (`flock` on Linux, `LockFileEx` on Windows) and write their PID and program name into it. A second instance on the same folders fails with `AlreadyRunning` and says which program and PID hold it. The lock is released when the process ends, however it ends.

## 6. Settings

`settings.json`: `{"version":1,"settings":{"server.port":2222, ...}}`. Unknown keys are kept when the file is rewritten. A value of the wrong type or out of range is an error at load (`SettingsFileValueInvalid`, with the key), and is refused when set (`SettingInvalid`; an unknown name is `SettingUnknown`). The file holds only the settings that were set; the others follow the defaults. Setting a value that a setting already has changes nothing.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `server.port` | integer 1–65535 | `2222` | The port Cave listens on |
| `server.address` | string | `""` | The address to listen on; empty means every interface (IPv4 and IPv6). Otherwise an IP literal |
| `server.startOnLaunch` | boolean | `true` | Start the server when Cave starts |
| `server.keepAliveSeconds` | integer 0–3600 | `60` | Ask an idle client whether it is alive this often; after 3 unanswered, disconnect. `0` turns it off |
| `attempts.clear` | `never`, `onStart`, `hourly`, `daily`, `weekly`, `monthly` | `never` | When the attempts list is cleared (§8.4) |
| `session.shell` | string | `""` | The shell of sessions; empty means the user's own (§9.5, §13.2) |
| `session.acceptEnv` | list of strings | `["LANG","LC_*"]` | Names of environment variables a client may set; `*` matches any run of characters |
| `sftp.enabled` | boolean | `true` | The SFTP subsystem |
| `forwarding.local` | boolean | `true` | Clients may open connections from this computer (`-L`, `-D`) |
| `forwarding.remote` | boolean | `true` | Clients may listen on this computer (`-R`) |
| `forwarding.gatewayPorts` | boolean | `false` | `-R` may listen on addresses other than loopback |
| `forwarding.agent` | boolean | `true` | Agent forwarding (`-A`) |
| `notify.accepted` | boolean | `true` | Desktop: a notification when somebody connects |
| `notify.rejected` | boolean | `false` | Desktop: a notification when an attempt is refused (at most one a minute) |
| `ui.theme` | `system`, `light`, `dark` | `system` | Desktop: the theme |
| `ui.closeToTray` | boolean | `false` | Desktop: the close button hides the window and Cave keeps running in the tray (only when a tray exists) |

A change takes effect at once. Changing `server.port` or `server.address` while the server runs restarts the listener and keeps the established connections; if the new address cannot be listened on, the server stops with that error shown (§9.1). Turning a feature off (`sftp.enabled`, `forwarding.*`) refuses new uses and leaves running ones.

Flags for one run, never saved: `--port <n>`, `--no-start` (do not start the server at launch).

## 7. People, keys and authentication

### 7.1 People

`people.json`:

```json
{"version":1,"people":[
  {"id":"p_3f9a…","name":"alice","enabled":true,"createdAt":"2026-11-24T10:00:00Z",
   "keys":[{"id":"k_81c2…","key":"ssh-ed25519 AAAAC3…","comment":"laptop","addedAt":"2026-11-24T10:01:00Z"}]}
]}
```

- `id`: `p_` and 16 hexadecimal digits from `crypto/rand`; keys have `k_` ids. Ids never change.
- `name`: the login name. 1 to 32 characters of `A–Z a–z 0–9 . _ -`, not starting with `-` or `.`. Unique ignoring case (`PersonNameTaken`); compared exactly at login. A bad name is `PersonNameInvalid`.
- `enabled`: a disabled person cannot connect; their keys stay.
- A person or key that a command names and that does not exist is `PersonNotFound` or `KeyNotFound`. A `people.json` that breaks one of these rules (a bad or repeated id or name, a key that is not acceptable or is there twice) is `PeopleFileInvalid`.
- A person may have no key (then they cannot connect).
- Renaming, disabling or removing a person, or removing a key, **disconnects** every connection that was authenticated as that person (rename, disable, remove) or with that key (remove key).

### 7.2 Keys

- Input is text in the `authorized_keys` format: one key per line, `<type> <base64> [comment]`. Blank lines and lines starting with `#` are skipped. Several lines add several keys; either all are added or none.
- Text without any key is `KeyTextEmpty`. A key that is in the text twice is taken once. An error about a line has the line's number.
- A line with options before the type (`command="…" ssh-ed25519 …`) is refused: `KeyOptionsUnsupported`.
- Accepted types: `ssh-ed25519`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `ssh-rsa` (2048 bits or more, else `KeyTooWeak`), `sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`. Anything else, certificates and `ssh-dss` included: `KeyTypeUnsupported`. Text that is not a public key: `KeyParseFailed`; a private key pasted by mistake: `KeyIsPrivate` (the message says to paste the `.pub` file, and the text is not stored or logged).
- A key belongs to at most one person. Adding a key that somebody already has: `KeyAlreadyAssigned` (metadata: the owner's name and the fingerprint).
- The fingerprint shown everywhere is OpenSSH's `SHA256:…`. The comment can be edited; it is display text (`sanitize.Display`, 64 characters).
- Keys can be imported from a file (a `.pub` file or an `authorized_keys` file of at most 1 MiB): same rules.

### 7.3 Authentication

The server offers only `publickey`. For each key a client offers, with login name `n`:

1. The fingerprint is remembered for this connection (at most 16).
2. No person is named exactly `n` → refused, reason `unknownName`.
3. The person is disabled → refused, reason `disabled`.
4. The key is not one of the person's → refused, reason `wrongKey`.
5. Otherwise the key is acceptable, and the connection is authenticated when the client proves it with a signature.

Every refusal looks the same to the client (the protocol's ordinary "try another method/key" answer, same code path, no message). RSA keys sign with `rsa-sha2-256` or `rsa-sha2-512`; SHA-1 signatures (`ssh-rsa`) are not accepted. At most 6 authentication requests per connection (`MaxAuthTries`).

People are looked up in the store's current state at the time of the request, so an edit applies to the next attempt at once.

## 8. Attempts

### 8.1 What an attempt is

One TCP connection to Cave's port is one attempt, whatever happens on it, with exactly one **outcome**:

| Outcome | Meaning |
|---|---|
| `accepted` | Authenticated as a person |
| `unknownName` | The login name is nobody's |
| `disabled` | The person is disabled |
| `wrongKey` | The name is a person's; none of the keys offered is theirs |
| `noKey` | The client reached authentication and offered no public key |
| `notProven` | The client offered a key that is the person's and did not prove it (no signature, or a bad one) |
| `handshakeFailed` | The connection ended before authentication started (a port scanner, a non-SSH client, no common algorithm, the time limit) |
| `refused` | Cave closed it at once because of the limits of §9.2 |

When several reasons occurred on one connection, the outcome is the first that applies of: `accepted`, `notProven`, then the reason of the last key offered, else `noKey`. Authentication starts when the key exchange has ended: a client that leaves after it without a request is `noKey`. A key offered with a signature algorithm that Cave does not take (RSA with SHA-1) is not looked at and counts as no key. A client that sends lines that are not an identification line is waited for, as the protocol allows, and ended by the time limit of §9.2.

### 8.2 The list

The list holds **entries**; an attempt adds 1 to the count of the entry with the same (address, login name, outcome, person), or makes a new entry. An entry has:

| Field | Content |
|---|---|
| `id` | `a_` and 16 hexadecimal digits |
| `outcome` | §8.1 |
| `personId`, `personName` | The person the name belongs to (empty for `unknownName`, `noKey` without a matching name, `handshakeFailed`, `refused`). The name is copied, so the entry still reads after the person is removed |
| `loginName` | What the client sent, sanitized, at most 64 characters; empty when there was none |
| `address` | The client's IP address, without the port |
| `count` | How many attempts |
| `firstAt`, `lastAt` | Times of the first and the last |
| `clientVersion` | The client's version string of the last attempt, sanitized, at most 64 characters |
| `keys` | The fingerprints offered at the last attempt, at most 8 |
| `keyOwner` | When a key offered at the last attempt belongs to another person, that person's name |

"Who" in the UIs is `personName`, or "Unknown" when it is empty. The default order is `lastAt`, newest first.

### 8.3 Hostile text

`sanitize.Display(s, max)` is applied to every string that came from a client before it is stored: invalid UTF-8, control characters (C0, DEL, C1), Unicode format characters (bidirectional overrides and isolates, zero-width characters, U+FEFF) and line or paragraph separators are each replaced by `U+FFFD`; the result is cut to `max` characters with `…` as the last one when it was longer. The UIs draw stored strings as they are and never interpret them.

### 8.4 Clearing

- **By hand**: "Clear list" empties the list; "Remove" deletes one entry.
- `attempts.clear`:
  - `never`: only by hand.
  - `onStart`: the list is emptied each time Cave starts, before the server starts.
  - `hourly`, `daily`, `weekly`, `monthly`: the whole list is emptied every 1 hour, 24 hours, 7 days or 30 days. The next time is stored in `attempts.json` (`nextClearAt`); it is set to now plus the period when the setting changes to a period and after every clear by this rule. It is checked at start (a time that passed while Cave was closed clears at once) and once a minute while Cave runs.
- The UIs show when the list was last cleared and, for a period, when it will be next.

### 8.5 Limits and the file

- At most 5 000 entries. When a new entry would exceed it, the entry with the oldest `lastAt` is dropped. `count` stops at 2⁵³.
- `attempts.json`: `{"version":1,"lastClearedAt":…,"nextClearAt":…,"entries":[…]}`. It is written at most once every 5 seconds while there are changes, and when Cave closes. A flood of attempts therefore costs memory and disk bounded by the limits above.
- A delayed write that fails is logged, and the next change tries again. Removing an entry that is not there is `AttemptNotFound`. When the file is read, its texts are made safe again, two entries of one group become one, a count past the limit becomes the limit, and of more than 5 000 entries the newest are kept; an entry that cannot be (an unknown outcome, a count below 1, a bad or repeated id, a last time before the first) is `AttemptsFileInvalid`.

## 9. The SSH server

### 9.1 Listening

- The server listens on `server.address`:`server.port` over TCP. With an empty address it listens on every interface, IPv4 and IPv6.
- Failing to listen (`ListenFailed`; the metadata has the address) leaves the server stopped, and the UIs show the reason: the port is taken, or a port below 1024 needs rights Cave does not have.
- States: `stopped`, `starting`, `running`, `stopping`. `Stop` closes the listener and every connection; `Start` on a running server and `Stop` on a stopped one do nothing.
- On Linux, Cave refuses to run as root (`RunningAsRoot`) unless started with `--allow-root`: every person would get a root shell.
- Identification string: `SSH-2.0-Cave_<version>`.

### 9.2 Handshake and limits

- Algorithms: the defaults of `golang.org/x/crypto/ssh` for key exchange, ciphers and MACs; host key algorithm `ssh-ed25519`.
- A connection has 30 seconds from accept to authenticated; then it is closed.
- At most 64 connections that are not yet authenticated, and at most 8 of them from one address. One more is closed at once (`refused`). Authenticated connections are not limited in number.
- At most 6 authentication requests (§7.3).

### 9.3 Connections

An authenticated connection is listed in the **registry** until it ends: id (`c_` and 16 hexadecimal digits), person, key fingerprint, address, client version, since when, and what it has open: sessions (`shell`, `exec` with the command sanitized to 80 characters, `sftp`), local forwards in use, remote forwards (listening address and port), agent forwarding. `Disconnect(id)` closes one. At most 256 channels are open at once per connection; one more is refused (`resource shortage`).

Channel types: `session` (§9.4), `direct-tcpip` (§9.8). Global requests: `tcpip-forward`, `cancel-tcpip-forward` (§9.8), `keepalive@openssh.com` and `no-more-sessions@openssh.com` (answered or obeyed). Everything else is refused.

### 9.4 Sessions

A `session` channel takes these requests:

| Request | Effect |
|---|---|
| `pty-req` | A PTY of the given size and modes for the process to come (§9.6). Once per channel |
| `env` | Sets a variable when its name matches `session.acceptEnv`; refused otherwise. At most 64 variables of at most 4 KiB each |
| `auth-agent-req@openssh.com` | Agent forwarding for this session (§9.8) |
| `shell` | Starts the shell as a login shell |
| `exec` | Starts `<shell> -c <command>` (Linux), §13.2 for Windows |
| `subsystem` | `sftp` starts the SFTP server (§9.7); any other name is refused |
| `window-change` | Resizes the PTY |
| `signal` | Sends the signal to the process's group (Linux: `HUP INT QUIT TERM KILL USR1 USR2`; others refused) |
| `break` | Accepted and ignored |

Only one of `shell`, `exec`, `subsystem` succeeds per channel. Rules:

- The process starts in the home folder (else `/`).
- **Without a PTY**: standard input, output and error are pipes; error goes to the channel's extended data. When the client closes its side, standard input is closed.
- **With a PTY**: the three are the PTY, and the process is the leader of a new session with the PTY as controlling terminal.
- When the process ends, Cave sends what is left of its output (waiting at most 2 seconds for descendants that still hold the pipes), then `exit-status`, or `exit-signal` when a signal killed it, and closes the channel.
- When the channel or the connection ends first: with a PTY, the PTY is closed (the kernel sends `SIGHUP`); without one, the process's group gets `SIGHUP` once. Cave never sends `SIGKILL`: a job started with `nohup` survives, as with `sshd`.
- A process that cannot start (`SessionStartFailed`) is reported to the client on the channel's error stream with exit status 255, and logged. The request itself is granted, as with `sshd`, whose child fails after the request was answered.
- A signal that killed the process and has no name in the protocol is reported as the exit status a shell gives it: 128 plus its number.

### 9.5 The environment of a session (Linux)

The process does **not** inherit Cave's environment. It gets exactly:

| Variable | Value |
|---|---|
| `USER`, `LOGNAME` | The OS user |
| `HOME` | The OS user's home |
| `SHELL` | The shell |
| `PATH` | Cave's own `PATH`, else `/usr/local/bin:/usr/bin:/bin` |
| `LANG`, `LC_*` | Cave's own, unless the client set them |
| `XDG_RUNTIME_DIR` | Cave's own, when set |
| `TERM` | From `pty-req` (only with a PTY) |
| `SSH_CLIENT`, `SSH_CONNECTION` | As `sshd` sets them |
| `SSH_TTY` | The PTY's path (only with a PTY) |
| `SSH_AUTH_SOCK` | The forwarded agent's socket (only with agent forwarding). Never Cave's own |
| `CAVE_PERSON` | The person's name |
| accepted `env` requests | As sent |

What Cave sets is put over what the client set: whatever `session.acceptEnv` allows, a client cannot set `USER`, `LOGNAME`, `HOME`, `SHELL`, `PATH`, `XDG_RUNTIME_DIR`, `TERM`, the `SSH_` variables or `CAVE_PERSON`. An `env` request after the process started is refused.

The shell is `session.shell` when set; else the user's shell in `/etc/passwd`; else `$SHELL` of Cave; else `/bin/sh`. A login shell is started with `-` before its name in `argv[0]`.

### 9.6 PTY

- Linux: `/dev/ptmx`, unlocked and named with `ioctl` (`TIOCSPTLCK`, `TIOCGPTN`), opened without cgo. The size comes from `pty-req` and `window-change` (`TIOCSWINSZ`; columns and rows from 1 to 4096, else the request is refused).
- The encoded terminal modes of `pty-req` (RFC 4254 §8) are applied to the PTY for the opcodes Linux has; unknown opcodes are skipped, and a malformed list fails the request (`PtyModesInvalid`).
- Windows: ConPTY (§13.3).

### 9.7 SFTP

Version 3 of the protocol (draft-ietf-secsh-filexfer-02), as OpenSSH's `sftp-server` speaks it, running inside Cave as the OS user, over the whole file system, starting in the home folder.

- Requests: `INIT`, `OPEN`, `CLOSE`, `READ`, `WRITE`, `LSTAT`, `FSTAT`, `SETSTAT`, `FSETSTAT`, `OPENDIR`, `READDIR`, `REMOVE`, `MKDIR`, `RMDIR`, `REALPATH`, `STAT`, `RENAME`, `READLINK`, `SYMLINK`, and the extensions `posix-rename@openssh.com`, `hardlink@openssh.com`, `fsync@openssh.com`, `statvfs@openssh.com`, `fstatvfs@openssh.com` (Linux) and `limits@openssh.com`. An unknown request gets `SSH_FX_OP_UNSUPPORTED`.
- `RENAME` fails when the target exists (version 3); `posix-rename` replaces it.
- `READDIR` gives `ls -l` style long names.
- Limits: a packet of at most 256 KiB (a longer one ends the session), a read of at most 255 KiB per request, at most 256 open handles per session (`SSH_FX_FAILURE` beyond).
- Errors of the OS map to `SSH_FX_NO_SUCH_FILE`, `SSH_FX_PERMISSION_DENIED` or `SSH_FX_FAILURE` with the OS's message.
- OpenSSH's `scp` uses SFTP (since 9.0) and so works without a helper; `scp -O` runs `scp` through `exec`, which works when `scp` is installed.

### 9.8 Forwarding

- **`direct-tcpip`** (`-L`, `-D`): when `forwarding.local` is on, Cave connects to the host and port asked (15-second limit) and copies both ways. Off: the channel is refused as `administratively prohibited`. A failed connection is refused as `connect failed`.
- **`tcpip-forward`** (`-R`): when `forwarding.remote` is on, Cave listens and opens a `forwarded-tcpip` channel for each connection that arrives. With `forwarding.gatewayPorts` off, it listens on loopback whatever address was asked (`127.0.0.1`, or `::1` when the address asked is an IPv6 literal); on, it listens on the address asked (empty, `*` and `0.0.0.0` mean every interface; `localhost` means loopback). Port 0 lets the OS choose, and the reply carries the port. At most 32 listeners per connection. `cancel-tcpip-forward` closes one; the end of the connection closes all.
- **Agent** (`auth-agent-req@openssh.com`): when `forwarding.agent` is on, the session gets a private socket (Linux: `agent.sock` in a new `0700` folder under the runtime folder, the socket `0600`) whose connections are carried to the client over `auth-agent@openssh.com` channels. The socket is removed when the session ends.

### 9.9 Keepalive

Every `server.keepAliveSeconds`, Cave sends `keepalive@openssh.com` wanting a reply to a connection from which nothing arrived in that time. Three in a row without anything arriving close it.

### 9.10 The host key

- An Ed25519 key, made at the first start, saved in OpenSSH's private key format with mode `0600`, with its `.pub` beside it.
- The UIs show its `SHA256:` fingerprint, so a client's first-connection question can be checked.
- "New host key" replaces it (the UIs warn that every client will see a changed-host-key warning). It takes effect for new connections.
- A host key file that cannot be read (the errors of `storage`), is not a private key (`HostKeyParseFailed`) or is a key of another type (`HostKeyTypeUnsupported`) is an error; Cave never silently replaces it. The `.pub` file is derived: when it is missing or differs, it is written again.

## 10. The application layer (`internal/app`)

`app.Open(ctx, Options) (*App, error)` takes the lock, opens the log, loads settings, people, attempts and the host key, applies `attempts.clear` (`onStart`, or an overdue period), and starts the server when `server.startOnLaunch` (and not `--no-start`). `(*App).Close(ctx)` stops the server, saves, releases the lock.

- **Snapshots** (plain values, safe to keep): `Server()` (state, listening addresses, error, host key fingerprint, the addresses of this computer with a ready `ssh -p <port> <name>@<address>` line), `People()`, `Attempts()` (entries, last cleared, next clear), `Connections()`, `Settings()`.
- **Commands**: `StartServer`, `StopServer`; `AddPerson`, `RenamePerson`, `SetPersonEnabled`, `RemovePerson`, `AddKeys` (text), `ImportKeys` (file), `SetKeyComment`, `RemoveKey`; `ClearAttempts`, `RemoveAttempt`; `Disconnect`; `Set` (one setting); `NewHostKey`. Each returns an `error` with a Tag the UIs turn into a sentence.
- **Events**: `Subscribe(func(Event)) (cancel func())`. `Changed{Topics}` (a set of `Server`, `People`, `Attempts`, `Connections`, `Settings`), coalesced to at most one every 100 ms; `Accepted{person, address}` and `Rejected{entry}` for notifications.
- `app.Messages` turns an error Tag into the sentence shown to the user (one table for both UIs); an unknown Tag shows the error's own message.

## 11. The desktop app (`cave`)

Material 3, light and dark (`ui.theme`), one window (960×640 at first, at least 720×480) titled "Cave". A navigation rail with five pages; below 800 logical pixels wide it becomes a bottom bar.

### 11.1 Overview
The server's state with one big Start/Stop button; the listening port; the host key fingerprint (copy button); "How to connect": for each address of this computer, the line `ssh -p <port> <name>@<address>` (copy button); counts: people, connected now, attempts in the list. When the server could not start, the error and what to do. When there are no people, a hint with a button to the People page.

### 11.2 People
A list of people (name, number of keys, enabled switch, connected-now mark). "Add person" asks for the name and, optionally, keys (a multi-line field for pasting, and "Import from file…" through the system's file dialog). Selecting a person shows their keys (type, fingerprint, comment, added) with add, edit comment and remove, and Rename and Remove for the person. Removing a person or a key asks first, and says how many connections it will close. Errors of §7 appear under the field they belong to.

### 11.3 Attempts
A table: Who, Login name, Address, Result, Count, First, Last. A filter (All, Accepted, Refused) and a search field (who, login name, address). Selecting a row shows the rest (client version, key fingerprints, key owner). "Clear list" (asks first) and "Remove". A line says when the list was last cleared and when it will be next, with a link to the setting. Refused results are worded for the user ("Unknown name", "Wrong key", "No key offered", "Key not proven", "Disabled person", "Not SSH or broken handshake", "Too many connections").

### 11.4 Connections
Who is connected now: person, address, since, what is open (§9.3). "Disconnect" (asks first), "Disconnect all".

### 11.5 Settings
Every setting of §6 in groups (Server, Attempts list, Sessions, File transfer and forwarding, Notifications, Appearance), each with a one-line explanation; the host key with "New host key…"; the paths of the configuration and state folders; the version.

### 11.6 Tray, notifications, closing
- A tray icon when the desktop has a tray: it shows whether the server runs, and its menu has Show, Start/Stop server and Quit.
- Notifications as `notify.*` say.
- The close button: with `ui.closeToTray` and a tray, the window hides. Otherwise Cave quits, after asking when people are connected ("3 people are connected. Quit and disconnect them?"). The end of the desktop session quits without asking.

## 12. The TUI (`tcave`)

The same five pages, in the alternate screen, with the mouse off (so the terminal's own selection and copy work).

- A header: "Cave", the server's state and port, connected-now and attempts counts. A footer: the keys of the current page.
- `1`–`5` or `Tab`/`Shift+Tab` change page; `s` starts or stops the server; `?` shows every key; `q` or `Ctrl+C` quits (asking when people are connected). Lists move with the arrows, `j`/`k`, `PgUp`/`PgDn`, `Home`/`End`.
- People: `a` add, `Enter` open, `e` rename, `Space` enable/disable, `d` remove (asks); inside a person: `a` add keys (a multi-line field; paste works through bracketed paste), `i` import from a file (a path field), `c` edit comment, `d` remove key, `Esc` back.
- Attempts: `f` cycles the filter, `/` searches, `Enter` shows details, `d` removes, `C` clears (asks).
- Connections: `d` disconnect (asks), `D` disconnect all (asks).
- Settings: `Enter` or `Space` changes (a switch flips, a choice cycles, a number or text opens a field); `Esc` cancels an edit.
- Works from 60×16 cells; smaller shows "Terminal too small". Honors `NO_COLOR`. Columns are cut with `…`, never wrapped.
- While the TUI runs, nothing is written to the terminal but the TUI; logs go to the file.

## 13. Windows

### 13.1 General
The same programs and features, with these differences. `cave.exe` is a GUI program and `tcave.exe` a console program. Folders are those of §5.1; files rely on the profile's ACLs (mode bits mean nothing). `server.port` below 1024 works.

### 13.2 Sessions
- The shell is `session.shell`, else `%ComSpec%`, else `cmd.exe`. `shell` starts it; `exec` starts `<shell> /d /c <command>` with the command passed as written (for a `session.shell` whose name is `powershell.exe` or `pwsh.exe`: `-NoLogo -NoProfile -Command <command>`).
- The environment is Cave's own for the variables Windows programs need (`SystemRoot`, `SystemDrive`, `windir`, `ComSpec`, `PATH`, `PATHEXT`, `TEMP`, `TMP`, `USERNAME`, `USERPROFILE`, `HOMEDRIVE`, `HOMEPATH`, `APPDATA`, `LOCALAPPDATA`, `ProgramData`, `ProgramFiles`, `ProgramFiles(x86)`, `ProgramW6432`, `CommonProgramFiles`, `COMPUTERNAME`, `OS`, `PROCESSOR_ARCHITECTURE`, `NUMBER_OF_PROCESSORS`), plus `TERM`, `SSH_CLIENT`, `SSH_CONNECTION`, `SSH_AUTH_SOCK`, `CAVE_PERSON` and accepted `env` requests, as in §9.5.
- Each session's process is in a job object that ends its processes when the session ends (Windows has no `SIGHUP`). `signal` is accepted for `KILL` and `TERM` (the job is ended) and refused for the others.

### 13.3 ConPTY
`pty-req` makes a pseudo console (`CreatePseudoConsole`), resized by `window-change`. Terminal modes are ignored.

### 13.4 SFTP
Paths use `/` and start with the drive: `/C:/Users/ada`. `/` lists the drives. `REALPATH` of `.` is the home folder. Permissions are reported as Go reports them; `SETSTAT` of permissions changes only the read-only bit; owner and group are 0; `SYMLINK`, `hardlink` and `statvfs` answer `SSH_FX_OP_UNSUPPORTED` when Windows refuses them.

### 13.5 Agent forwarding
The session's `SSH_AUTH_SOCK` is a named pipe `\\.\pipe\cave-agent-<random>` that only the current user may open, which is what Windows' OpenSSH expects.

### 13.6 Testing
Under Wine (§14.4). What Wine cannot show is listed for the user in `docs/MANUAL-CHECKS.md`.

## 14. Testing

### 14.1 Isolation
No test, proof or check touches the user's Cave folders, `~/.ssh`, SSH agent, `~/.wine`, or the system's `sshd`, or listens anywhere but loopback on a port the OS chose.
- Go tests: `storagetest.Isolate(t)` points the folders, `HOME`, `XDG_*` and the runtime folder at `t.TempDir()`.
- Scripts: `scripts/isolated.sh` runs a command with all of those under a temporary folder (or a fixture copied into one) and removes it afterwards.
- Sessions started by tests run with the isolated home and a shell the test chooses (`/bin/sh`).

### 14.2 Levels
- **Unit**: every package, table-driven, with a route test for every error Tag.
- **Protocol**: the server on `127.0.0.1:0` with the client of `golang.org/x/crypto/ssh`: authentication (each outcome of §8.1), limits, sessions, SFTP (a small test client in `sshtest`), forwarding, agent.
- **OpenSSH**: the same through the real `ssh`, `sftp`, `scp`, `ssh-agent` (`sshtest`, CLAUDE.md "Testing notes"); skipped only when OpenSSH is missing.
- **UI**: `gluttertest` widget and golden tests for the desktop app; model tests with text goldens for the TUI.
- **Display**: `cave_display` tests and proofs in a headless compositor.
- **Windows**: `GOOS=windows` build and vet always; the tests of the packages with Windows files under Wine.

### 14.3 Fakes
`clock` has a fake clock; `internal/app` takes a `Clock`, the folders and a logger, so UIs are tested over a real `App` in a temporary folder with the server on loopback, or over a fake `App` behind the small interface each UI declares.

### 14.4 Wine
`scripts/wine-exec.sh` (from Glutter's script of the same name) runs a Windows program in a private prefix under `.wine-test/` on a headless X server and stops Wine afterwards; `scripts/test-windows.sh` runs `go test -exec` through it, one package at a time.

## 15. Logging

`slog` JSON lines to `<state>/cave.log` (JSON, so that a logged error can be read back as an `errors.ErrorJson` and replayed in a test); when it passes 1 MiB it becomes `cave.log.1` (replacing the old one). `--log-level debug|info|warn|error` (default `info`). At `info`: start and stop, listen, every attempt with its outcome (login name and client version sanitized), every session start and end, forwards, errors. Never a private key, never the content of sessions.

## 16. Versions and builds

- `internal/app/version.go` has `devVersion` (the version being developed, `0.1.0-dev` at first); a release is tagged `vMAJOR.MINOR.PATCH`, and the version printed is the module's version when built from a tag (`runtime/debug.ReadBuildInfo`), else `devVersion`. `--version` prints it.
- `go build ./cmd/cave ./cmd/tcave` builds both. The Windows desktop program is packaged with Glutter's `glutter-windows build` (icon, version, manifest).

## 17. Not in this version

Named so that nobody adds them by accident; each needs a spec change first: password or any other authentication method; several OS users or changing user; per-person permissions (shell, files, forwarding); certificates and `authorized_keys` options; X11 forwarding; blocking addresses after failures; attaching a second UI to a running instance; running as a system service; macOS; translations.

## 18. Milestones

| | Milestone | Result |
|---|---|---|
| M0 | Foundations | The module, the checks, the scripts, storage and isolation |
| M1 | Data | Settings, people and keys, the attempts list, the host key |
| M2 | Server | Listening, authentication, attempts, connections, sessions with and without a PTY, on Linux |
| M3 | Subsystems | SFTP, port forwarding, agent forwarding, keepalive |
| M4 | Application | `internal/app`: one object both UIs drive |
| M5 | TUI | `tcave`, complete |
| M6 | Desktop | `cave`, complete, with tray and notifications |
| M7 | Windows | Everything on Windows, tested under Wine |
| M8 | Release | Manual, packaging, the security pass |
