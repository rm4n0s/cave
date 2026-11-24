# Error Tags without a test

Every error Tag has a test that asserts its route (CLAUDE.md "Definition of done"). The Tags listed here are the only exceptions: failures that a test cannot make happen. `scripts/error-tags.sh --untested` leaves them out, and a test of `internal/archtest` fails when a Tag is listed here although a test asserts it. One line per Tag, as "- `Tag`: reason".

- `HostKeyEncodeFailed`: `ssh.MarshalPrivateKey` cannot fail for an Ed25519 key that `ed25519.GenerateKey` just made
- `SessionJobCreateFailed`: `CreateJobObject` fails only when Windows is out of resources or refuses its own child, which a test cannot cause
- `SessionJobLimitFailed`: `SetInformationJobObject` fails only when Windows is out of resources or refuses its own child, which a test cannot cause
- `SessionJobOpenFailed`: `OpenProcess` of the process that just started fails only when Windows is out of resources or refuses its own child, which a test cannot cause
- `SessionJobAssignFailed`: `AssignProcessToJobObject` fails only when Windows is out of resources or refuses its own child, which a test cannot cause
- `PtyInputPipeFailed`: `CreatePipe` fails only when Windows is out of handles or memory, which a test cannot cause
- `PtyOutputPipeFailed`: `CreatePipe` fails only when Windows is out of handles or memory, which a test cannot cause
- `PtyConsoleFailed`: `CreatePseudoConsole` fails for a Windows older than 10 1809 or when it is out of resources; no test can make either happen
