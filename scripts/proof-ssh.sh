#!/usr/bin/env bash
# Runs the real ssh against the Cave of a proof (docs/IMPLEMENTATION.md "Proof methods"), for the
# --during option of scripts/tui-proof.sh and scripts/proof.sh: an attempt that is accepted, one
# with a wrong key, one with a name nobody has.
#
# Usage: scripts/proof-ssh.sh <login name> <client key name> [ssh arguments and command]
#
#   <client key name>  a file of client-keys/ in the fixture: alice, bob or mallory
#
# It reads CAVE_PROOF_PORT (the port of the server) and CAVE_PROOF_FIXTURE (the copy of the
# fixture), which the proof scripts give their --during command. The connection is to 127.0.0.1
# only. ssh runs with -F none, an empty home, no agent, only the one key and a known_hosts that
# holds exactly the fixture's host key, which it checks: it never turns host key checking off and
# never reads anything of the user's ~/.ssh. Exit status: ssh's own (255 when it was refused),
# 64 for a usage mistake.
#
# Example: scripts/proof-ssh.sh alice alice true
set -uo pipefail

usage() {
  sed -n '2,/^# Example/p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

[[ $# -ge 2 ]] || usage
login="$1"
key_name="$2"
shift 2
port="${CAVE_PROOF_PORT:-}"
fixture="${CAVE_PROOF_FIXTURE:-}"
if [[ -z "$port" || -z "$fixture" ]]; then
  echo "error: CAVE_PROOF_PORT and CAVE_PROOF_FIXTURE must be set (the proof scripts set them for --during)" >&2
  exit 64
fi
if [[ ! -f "$fixture/client-keys/$key_name" || ! -f "$fixture/ssh_host_ed25519_key.pub" ]]; then
  echo "error: the fixture '$fixture' has no client-keys/$key_name or no host key" >&2
  exit 64
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/cave-ssh.XXXXXX")"
chmod 700 "$work"
trap 'rm -rf "$work"' EXIT

cp "$fixture/client-keys/$key_name" "$work/key"
chmod 600 "$work/key"
host_key="$(cut -d' ' -f1,2 "$fixture/ssh_host_ed25519_key.pub")"
printf '[127.0.0.1]:%s %s\n' "$port" "$host_key" >"$work/known_hosts"

export HOME="$work"
unset SSH_AUTH_SOCK
timeout -k 2 30 ssh -F none -i "$work/key" \
  -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes \
  -o PasswordAuthentication=no -o PreferredAuthentications=publickey \
  -o StrictHostKeyChecking=yes -o UserKnownHostsFile="$work/known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o ConnectTimeout=10 -o LogLevel=ERROR \
  -p "$port" "$login@127.0.0.1" "$@"
