# The fixture of the proofs, dark

A copy of `scripts/testdata/two-people` whose `settings.json` also has `"ui.theme": "dark"`, for
the proofs of the desktop app in the dark theme (`scripts/proof.sh`).

**The keys here are the same test keys as in `two-people`: generated for the proofs and tests
with `ssh-keygen -t ed25519 -N ''`, used nowhere else, and protecting nothing.** The server in this
fixture listens on 127.0.0.1 only, and the proof scripts refuse a fixture that does not.
