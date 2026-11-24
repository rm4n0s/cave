# The fixture of the proofs

A Cave configuration folder for the proofs of `scripts/tui-proof.sh`, `scripts/proof.sh` and
`scripts/wine-proof.sh` (docs/IMPLEMENTATION.md "Proof methods"): two people, `alice` (enabled)
and `bob` (disabled), the host key, and the client keys the proofs connect with.

**Every key here was generated for the proofs and tests with `ssh-keygen -t ed25519 -N ''`. They
are used nowhere else and protect nothing.** The server in this fixture listens on 127.0.0.1
only (`settings.json`), and the scripts refuse a fixture that does not.

- `people.json`: alice has the public key of `client-keys/alice`, bob that of `client-keys/bob`.
- `client-keys/mallory` belongs to nobody: a proof uses it for a wrong key or an unknown name.
- `ssh_host_ed25519_key`: the host key. `scripts/proof-ssh.sh` trusts exactly this one.
- `settings.json`: `ui.theme` is `light`, so that the proofs of the desktop app do not depend on the theme of the system the proof runs on (`two-people-dark` is the same with the dark theme).
