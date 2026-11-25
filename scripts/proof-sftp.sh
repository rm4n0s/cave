#!/usr/bin/env bash
# Runs the real sftp against the Cave of a proof (docs/IMPLEMENTATION.md "Proof methods"), for the
# --during option of scripts/tui-proof.sh and scripts/proof.sh: files uploaded and downloaded, for
# the history.
#
# Usage: scripts/proof-sftp.sh <login name> <client key name> <sftp command>...
#
#   <client key name>  a file of client-keys/ in the fixture: alice, bob or mallory
#   <sftp command>     one line of an sftp batch each, run in a folder of its own that holds
#                      notes.txt (2 KiB) and photo.jpg (300 KiB) to upload
#
# It reads CAVE_PROOF_PORT and CAVE_PROOF_FIXTURE as scripts/proof-ssh.sh does, and runs sftp the
# way that script runs ssh: -F none, an empty home, no agent, only the one key and a known_hosts
# that holds exactly the fixture's host key, which it checks; 127.0.0.1 only. Exit status: sftp's
# own, 64 for a usage mistake.
#
# Example: scripts/proof-sftp.sh alice alice 'put notes.txt' 'get notes.txt back.txt'
set -uo pipefail

usage() {
  sed -n '2,/^# Example/p' "$0" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

[[ $# -ge 3 ]] || usage
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

work="$(mktemp -d "${TMPDIR:-/tmp}/cave-sftp.XXXXXX")"
chmod 700 "$work"
trap 'rm -rf "$work"' EXIT

cp "$fixture/client-keys/$key_name" "$work/key"
chmod 600 "$work/key"
host_key="$(cut -d' ' -f1,2 "$fixture/ssh_host_ed25519_key.pub")"
printf '[127.0.0.1]:%s %s\n' "$port" "$host_key" >"$work/known_hosts"
mkdir "$work/files"
head -c 2048 /dev/zero | tr '\0' 'n' >"$work/files/notes.txt"
head -c 307200 /dev/zero | tr '\0' 'p' >"$work/files/photo.jpg"
printf '%s\n' "$@" >"$work/batch"

export HOME="$work"
unset SSH_AUTH_SOCK
cd "$work/files" || exit 1
timeout -k 2 30 sftp -F none -i "$work/key" -b "$work/batch" \
  -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes \
  -o PasswordAuthentication=no -o PreferredAuthentications=publickey \
  -o StrictHostKeyChecking=yes -o UserKnownHostsFile="$work/known_hosts" -o GlobalKnownHostsFile=/dev/null \
  -o ConnectTimeout=10 -o LogLevel=ERROR \
  -P "$port" "$login@127.0.0.1"
