# Cave: the guide

This guide is for the person who runs Cave. If you have not started it yet, read the [README](../README.md) first: it takes five minutes. For what Cave does and does not protect, read [SECURITY.md](SECURITY.md).

Everything here is true of both programs, `cave` (the desktop application) and `tcave` (the terminal application), unless it says otherwise. In `tcave`, press `?` to see the keys.

## Contents

- [People and keys](#people-and-keys)
- [The attempts list](#the-attempts-list)
- [Connections](#connections)
- [What a connected person can do](#what-a-connected-person-can-do)
- [Settings](#settings)
- [Files and folders](#files-and-folders)
- [The command line](#the-command-line)
- [Windows](#windows)
- [Troubleshooting](#troubleshooting)

## People and keys

A **person** is somebody you let in. Each person has a **name** and one or more **public keys**.

**The name** is what the person types before the `@`: for a person named `alice`, `ssh alice@your-computer`. It has 1 to 32 characters: letters, digits, dots, underscores and dashes, and it cannot start with a dash or a dot. Two people cannot have names that differ only in capital letters. The name is not a user of your computer; it is only a name in Cave.

**A public key** is half of a pair of keys. The person keeps the private half secret on their own computer, and gives you the public half. When they connect, their computer proves that it holds the private half, without sending it. That is how Cave knows it is them.

**Where a person finds theirs.** On Linux and macOS it is a file in `~/.ssh` whose name ends in `.pub`, such as `id_ed25519.pub`. On Windows it is in `C:\Users\<name>\.ssh`. If they have none, they make one with `ssh-keygen -t ed25519`, and send you the `.pub` file's text. It is one line, such as `ssh-ed25519 AAAAC3Nza… alice@laptop`.

**Adding keys.** Paste the text under the person, or import a file (a `.pub` file or an `authorized_keys` file of up to 1 MiB). You can paste several lines at once: either all of them are added or none. Lines that are empty or start with `#` are skipped. You can edit a key's comment (a note for you, such as `laptop`) at any time.

**Which keys are accepted.** `ssh-ed25519`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `ssh-rsa` (of 2048 bits or more), and the two kinds of security-key (`sk-…`). Cave shows each key by its fingerprint, which looks like `SHA256:…`, as `ssh-keygen -l` does.

**What the errors mean:**

| Cave says | What happened |
|---|---|
| There is no public key in the text | The text has no key line. Paste the contents of a `.pub` file. |
| This is a private key | You pasted the secret half. Paste the file that ends in `.pub` instead. Cave does not keep or log what you pasted. |
| Line N: this is not a public key | Line N is not in the form `type key comment`. |
| Line N: a key with options before its type | An `authorized_keys` line such as `command="…" ssh-ed25519 …`. Cave does not support options; paste only the key. |
| Keys of the type … are not supported | A certificate, or an old key type such as `ssh-dss`. |
| The key is too weak | An RSA key under 2048 bits. |
| This key already belongs to … | A key belongs to one person only. Remove it from the other person first. |
| The name is taken or not valid | See the rules for names above. |

**Disabling, renaming and removing.** A **disabled** person cannot connect, and keeps their keys; enable them again later. **Renaming** a person, **disabling** or **removing** them, or removing one of their keys, disconnects the connections that person (or that key) has open right now.

A person with no key cannot connect.

## The attempts list

Every connection to Cave's port is one **attempt**, whatever happens on it. The Attempts page counts them: repeated attempts from the same address, with the same name and the same result, are one line with a count.

| Result | What it means |
|---|---|
| Accepted | The person got in. |
| Unknown name | The name that was typed is nobody's. |
| Disabled | The person exists but is disabled. |
| Wrong key | The name is a person's, but none of the keys offered is theirs. The line says if the key belongs to somebody else. |
| No key | The client reached the login and offered no public key. |
| Not proven | The client offered a key that is the person's, but could not prove it holds the private half. |
| Handshake failed | The connection ended before the login began: a port scanner, a program that does not speak SSH, or a client that was too slow. |
| Refused | Cave closed the connection at once because too many were waiting (at most 64 at a time, 8 from one address). |

Select a line to see everything about it: the address, the client's program and version, the fingerprints of the keys it offered, and when it first and last tried. What a client sends before it has logged in is not trusted: Cave replaces control characters and tricks of display in it before showing it.

The list keeps at most 5 000 lines; when it is full the oldest line is dropped. You can remove one line, or clear the whole list. The setting `attempts.clear` can clear it for you: never, each time Cave starts, every hour, every day, every week, or every 30 days. The page shows when it was last cleared and when it will be next.

## Connections

The Connections page lists the people who are connected now: who, with which key, from which address, with what client, since when, and what each has open: a shell, a command, file transfer, forwarded ports, a forwarded agent. You can disconnect one connection, or all of them.

Cave also disconnects a client that has gone silent: it asks every `server.keepAliveSeconds` seconds whether the client is still there, and closes the connection after three silent checks more. Set it to 0 to turn this off.

## What a connected person can do

Be clear about this before you add anybody: **a person who is connected is you on this computer.** Their shell runs as the operating system user who started Cave. They can:

- **Run a shell or a command** (`ssh alice@host`, `ssh alice@host ls`), with the same files, programs and network you have. A shell starts in your home folder. It does not get Cave's own environment, only your usual variables, plus `CAVE_PERSON` with the person's name and the `SSH_…` variables that `sshd` also sets. A person may set the variables that `session.acceptEnv` allows (by default `LANG` and `LC_*`), and no others.
- **Copy files** with `sftp` or `scp`, over the whole file system that you can reach, starting in your home folder. Turn it off with `sftp.enabled`.
- **Forward ports.** `ssh -L` and `ssh -D` make Cave open connections from this computer to wherever the person asks, as you could (`forwarding.local`). `ssh -R` makes Cave listen on a port of this computer and carry the connections to the person (`forwarding.remote`); by default it listens on this computer's own loopback address only, and `forwarding.gatewayPorts` lets it listen on the network.
- **Forward their SSH agent** (`ssh -A`, `forwarding.agent`). The person's computer keeps their private keys; programs in the session ask it to sign. **Who can reach a forwarded agent:** while the person is connected, the programs of that session can use their keys, and so can everything that runs as you, and the sessions of any other person who is connected at the same time, because all of them are you. By connecting with `ssh -A` to your Cave, a person trusts you, and trusts whoever else you let in. If you do not want people to put their agent at that risk, turn `forwarding.agent` off.

There is no way to give one person less than another: no per-person shell, files or forwarding. A person you do not trust to be you should not be added.

Cave itself never gives a session your own `SSH_AUTH_SOCK`: a session sees an agent only when its person forwards one.

## Settings

Settings take effect at once. They are saved in `settings.json` in the configuration folder. Cave writes only the ones you changed; the others follow the defaults below. The desktop application shows all of them on the Settings page. `tcave` shows them too, except the ones that are only for the desktop (marked below).

| Setting | Default | What it does |
|---|---|---|
| `server.port` | `2222` | The port Cave listens on, from 1 to 65535. A port below 1024 may need rights Cave does not have. |
| `server.address` | empty | The address to listen on. Empty means every network interface, IPv4 and IPv6. Otherwise one IP address, such as `127.0.0.1` to be reachable only from this computer. |
| `server.startOnLaunch` | on | Start the server when Cave starts. |
| `server.keepAliveSeconds` | `60` | How often to check that a client is still there (0 to 3600; 0 turns it off). |
| `attempts.clear` | `never` | When the attempts list is emptied by itself: `never`, `onStart`, `hourly`, `daily`, `weekly` or `monthly`. |
| `session.shell` | empty | The shell people get. Empty means your own shell (on Windows: `cmd.exe`). |
| `session.acceptEnv` | `LANG`, `LC_*` | The names of the variables a client may set. `*` matches any run of characters. |
| `sftp.enabled` | on | Allow `sftp` and `scp`. |
| `forwarding.local` | on | Allow `ssh -L` and `ssh -D`. |
| `forwarding.remote` | on | Allow `ssh -R`. |
| `forwarding.gatewayPorts` | off | Let `ssh -R` listen on addresses other than loopback. |
| `forwarding.agent` | on | Allow `ssh -A`. See [what a connected person can do](#what-a-connected-person-can-do). |
| `notify.accepted` | on | Desktop only: a notification when somebody connects. |
| `notify.rejected` | off | Desktop only: a notification when an attempt is refused, at most one a minute. |
| `ui.theme` | `system` | Desktop only: `system`, `light` or `dark`. |
| `ui.closeToTray` | off | Desktop only: the close button hides the window and Cave keeps running in the tray, when the desktop has a tray. |

Changing `server.port` or `server.address` while the server runs moves the listener and keeps the connections that are open. If the new address cannot be listened on, the server stops and says why. Turning `sftp.enabled` or a `forwarding.…` setting off refuses new uses and leaves the ones already running.

Settings can also be changed by editing `settings.json` while Cave is closed. A value that is not allowed makes Cave refuse to start and say which one; Cave never overwrites a file it cannot read.

## Files and folders

| | Linux | Windows |
|---|---|---|
| Configuration | `$XDG_CONFIG_HOME/cave`, else `~/.config/cave` | `%AppData%\Cave` |
| State | `$XDG_STATE_HOME/cave`, else `~/.local/state/cave` | `%LocalAppData%\Cave` |

The configuration folder holds `settings.json`, `people.json` (the people and their keys), and the host key, `ssh_host_ed25519_key` and its `.pub`. The state folder holds `attempts.json` (the attempts list), `cave.log` and `cave.log.1` (the log), and `lock`.

The folders are created readable by you only, and so are the files with the people and the host key. Back up the configuration folder to keep your people and your host key. If you copy it to another computer, people will not see a warning about the host key.

**The log.** `cave.log` has one JSON line for each thing that happened: Cave starting and stopping, listening, every attempt with its result, every session starting and ending, port forwards, and errors. It never has a private key or what a session said. It becomes `cave.log.1` when it passes 1 MiB, replacing the old one. Use `--log-level debug` to see more (every file request of an SFTP session, for example).

**The lock.** Only one Cave can use a pair of folders at a time. The file `lock` says which program holds them.

## The command line

Both programs take the same flags. A flag is for one run and is never saved.

| Flag | What it does |
|---|---|
| `--config-dir <folder>` | Use this folder for the settings, the people and the host key. The variable `CAVE_CONFIG_DIR` does the same; the flag wins. |
| `--state-dir <folder>` | Use this folder for the attempts list, the log and the lock. The variable `CAVE_STATE_DIR` does the same; the flag wins. |
| `--port <port>` | Listen on this port for this run, whatever the settings say. |
| `--no-start` | Do not start the server when the program starts. |
| `--allow-root` | Run although the user is root. Every person would get a root shell: do not, unless you mean it. |
| `--log-level <level>` | What the log has: `debug`, `info` (the default), `warn` or `error`. |
| `--page <page>` | The page to show first: `overview`, `people`, `attempts`, `connections` or `settings`. |
| `--version` | Print the version and exit. |
| `--help` | Print the flags and the default folders, and exit. |

Folders given to Cave must be absolute paths. To try Cave without touching your own, run it on empty folders: `cave --config-dir /tmp/cave-try/config --state-dir /tmp/cave-try/state`.

## Windows

Cave works the same way on Windows 10 (version 1809 or newer) and Windows 11, with these differences:

- `cave.exe` is a desktop program that opens no console window; `tcave.exe` is a console program.
- The folders are those in the table above. Files are protected by your profile's permissions.
- The default shell is `%ComSpec%` (usually `cmd.exe`). `ssh alice@host dir` runs `cmd.exe /d /c dir`. If you set `session.shell` to `powershell.exe` or `pwsh.exe`, commands run with `-NoLogo -NoProfile -Command`.
- A session gets the usual Windows variables (such as `SystemRoot`, `PATH`, `USERPROFILE`) and the ones Cave adds, not the rest of Cave's environment.
- A terminal session (`ssh -t`) uses the Windows pseudo console.
- When a session ends, everything it started ends with it, because Windows has no hang-up signal. `signal` requests can end a session (`KILL`, `TERM`); the other signals are refused.
- File transfer paths start with the drive: `/C:/Users/alice`. The path `/` lists the drives, and cannot be changed. `statvfs` is not available, and links may need a privilege.
- Agent forwarding uses a private named pipe, `\\.\pipe\cave-agent-…`, that only your user can open.
- A port below 1024 works without special rights.

Windows' own firewall asks whether to let Cave through the first time it listens on the network. Allow it only for the networks where you want people to reach you.

## Troubleshooting

**"Cave cannot listen on … The port is in use."** Another program already uses that port, or you have two Caves. Choose another port in Settings (or `--port`). A port below 1024 may also need rights you do not have; use 2222 or a higher one.

**"Cave is already running on these folders."** One Cave can run for a pair of folders. Quit the other one first (the message says which program and its process ID), or start this one on other folders with `--config-dir` and `--state-dir`.

**A person says the "host key has changed" and ssh refuses.** Their computer remembers the host key from the first time. The warning appears if you made a new host key (Settings, "New host key"), restored the configuration from a backup of another one, or if they are connecting to a different computer on the same address. If you changed it on purpose, tell them to remove the old line: `ssh-keygen -R "[address]:port"`. If you did not, ask who answered.

**A person is refused.** Look at the Attempts page: it says why. *Unknown name*: the name they typed is not one you added (check the capital letters). *Wrong key*: their ssh offered other keys; they can say which key to use with `ssh -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes`. *Disabled*: enable them. *Handshake failed* from their address: their client could not reach the login (an old client, a firewall, a program that is not ssh). If the Attempts page shows nothing at all, the connection never reached Cave: check the address, the port, and your firewall or router.

**ssh asks for a password.** Cave never does. If ssh shows a password prompt, it is talking to something else (another server on that address, or the default port 22 instead of Cave's). Give `-p <port>`.

**The Attempts list is full of strangers.** Anybody who can reach the port can try. They cannot get in without a key you gave them. If it bothers you, listen on `127.0.0.1` only, change the port, or close it in your firewall.

**Cave starts but nobody can connect from another computer.** Check `server.address` (it must be empty or your network address, not `127.0.0.1`), the firewall, and that your computer's address is the one in the ssh line.

**Where is the log?** In the state folder, `cave.log`. Run with `--log-level debug` for more.
