# Security

This page says what Cave protects and what it does not. How to use and set up Cave is in the [guide](GUIDE.md); the [README](../README.md) is the short version.

## What Cave protects

- **Who may connect.** Only the people you added, with the public keys you gave them. There is no password login, and no other way in. A name that is nobody's, a person you disabled, and a wrong key all look the same to the client.
- **The host key.** The key that tells a client it is talking to your Cave is stored in the configuration folder, readable only by you. If you replace it, every person sees a warning the next time they connect.
- **The files of people.** The list of people, the host key and the settings are written readable by you only, in folders readable by you only, and written in a way that a crash does not leave half a file.

## What Cave does not protect

- **A person who connects is you.** Every session runs as the operating system user who started Cave: your files, your programs, your network. There is no sandbox, and no way to give one person less than another. Add only people you would trust with your own account.
- **People are not separated from each other.** Two people connected at the same time run as the same user and can see each other's processes and files.
- **A forwarded agent.** A person who connects with `ssh -A` lends their SSH agent to the session, so that programs in it can use their keys. Anybody who can reach that session's agent can use those keys while the person is connected: you, as the user who runs Cave, and any other person you let in at the same time. Do not forward an agent to a Cave run by someone you do not trust, and turn `forwarding.agent` off if you do not want it.
- **Cave is not a system service and not a firewall.** It listens on the network interfaces you set, and anybody who can reach the port can try to connect (the Attempts page shows them). Use your firewall or router to decide who can reach it.

## How to report a problem

Please report a security problem as a private security advisory on the GitHub repository (the Security tab, "Report a vulnerability"). Do not open a public issue for it.
