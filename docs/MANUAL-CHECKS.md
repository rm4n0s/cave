# Manual checks

What only a person at a computer can see. Each item has the step it belongs to and the exact command. When you have looked, tell the answer: it is written in the step's entry of `docs/IMPLEMENTED.md`, and the item is removed from this list (the file is deleted when it is empty).

## Step 6.8: the tray and the notifications

On your desktop run (the fixture listens on loopback only, on a port the system chooses):

```
port=$(go run scripts/freeport.go); echo $port; scripts/isolated.sh --config scripts/testdata/two-people -- go run -tags debug_mode ./cmd/cave --port $port
```

Is there a tray icon, and does its menu work (`Show Cave`, `Stop server` or `Start server`, `Quit`)? Does the close button hide the window when `Close to the tray` is on in Settings?

Then, in another terminal, with the port it printed:

```
CAVE_PROOF_PORT=<port> CAVE_PROOF_FIXTURE=scripts/testdata/two-people scripts/proof-ssh.sh alice alice true
```

Does a notification appear (`alice connected`, `From 127.0.0.1`)? And for a refused attempt, with `Notify when a connection is refused` on in Settings:

```
CAVE_PROOF_PORT=<port> CAVE_PROOF_FIXTURE=scripts/testdata/two-people scripts/proof-ssh.sh nobody mallory true
```

## Step 7.2: a terminal session on a Windows PC

Wine makes the pseudo console but does not connect the process's standard handles to it, so the tests that read a terminal's output skip under Wine (`TestShellOnATerminalOnWindows`). On a Windows PC, with `tcave.exe` running and a person added whose key your `ssh` has:

```
ssh -t <person>@<pc>
```

Does it give a `cmd.exe` prompt? Does `dir` print? Does `echo %CAVE_PERSON%` print the person's name? Does resizing the terminal window change what `mode con` shows? Does `exit` end the session? (`ssh -t <person>@<pc> exit 5` must give the status 5: `echo %ERRORLEVEL%` or `echo $?` afterwards.)

## Step 7.4: agent forwarding on a Windows PC

Wine has no OpenSSH, and ignores `FILE_FLAG_FIRST_PIPE_INSTANCE` (the test of a taken pipe name skips there). On a Windows PC, with `tcave.exe` running, the OpenSSH client's agent running (`Get-Service ssh-agent`, `ssh-add` with a key) and a person added whose key that agent has:

```
ssh -A <person>@<pc> ssh-add -l
```

Does it list the key of your agent (the one of the computer you typed it on, not the PC's)? Does `echo %SSH_AUTH_SOCK%` in `ssh -A <person>@<pc>` print `\\.\pipe\cave-agent-…`?

## Step 7.5: the programs on a Windows PC

Build the files with `scripts/heavy.sh -- scripts/build-windows.sh` and copy `dist/windows-amd64/cave.exe` and `tcave.exe` to a Windows 10 (1809 or newer) or Windows 11 PC. Then:

- Does `cave.exe` open without a console window? Do the tray icon and a notification appear (add a person, then connect with `ssh` from another computer, with `Notify when someone connects` on in Settings)?
- From another computer, does `ssh <person>@<pc>` give a shell (ConPTY)? Does `sftp <person>@<pc>` list the drives at `/` and work? Does `ssh -A <person>@<pc> ssh-add -l` list the key of your agent (step 7.4 has the same check)?
- Does `tcave.exe` draw correctly in Windows Terminal and in the old console (`cmd.exe` window)? Do the keys and the colors work, and does `q` quit?
