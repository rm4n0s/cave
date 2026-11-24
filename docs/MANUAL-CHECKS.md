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
