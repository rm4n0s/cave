#!/usr/bin/env bash
# Runs a command away from the user's own files (docs/SPEC.md §14.1): Cave's folders, the home
# folder, the XDG folders and the runtime folder are in a new temporary folder, and the user's
# SSH agent is taken out of the environment. The temporary folder is removed afterwards.
#
# Usage: scripts/isolated.sh [--config <fixture dir>] [--keep] -- <command> [args...]
#
#   --config <dir>   copy this folder in as Cave's configuration folder (settings.json,
#                    people.json, a host key) before the command starts
#   --keep           do not remove the temporary folder; its path is printed on standard error
#
# Inside the command:
#   CAVE_CONFIG_DIR, CAVE_STATE_DIR                  Cave's folders
#   HOME, XDG_CONFIG_HOME, XDG_STATE_HOME,
#   XDG_CACHE_HOME, XDG_DATA_HOME, XDG_RUNTIME_DIR   folders inside the temporary folder
#   CAVE_ISOLATED_ROOT                               the temporary folder
#   SSH_AUTH_SOCK, SSH_AGENT_PID, SSH_ASKPASS        unset
#
# Exit status: the command's own; 64 for a usage mistake.
set -euo pipefail

usage() {
  echo "usage: $0 [--config <fixture dir>] [--keep] -- <command> [args...]" >&2
  exit 64
}

config=""
keep=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      [[ $# -ge 2 ]] || usage
      config="$2"
      shift 2
      ;;
    --keep)
      keep=1
      shift
      ;;
    --)
      shift
      break
      ;;
    *) usage ;;
  esac
done
[[ $# -gt 0 ]] || usage
if [[ -n "$config" && ! -d "$config" ]]; then
  echo "error: the fixture folder '$config' does not exist" >&2
  exit 64
fi

# The runtime folder holds sockets, whose paths must be short: the root's name is kept short.
root="$(mktemp -d "${TMPDIR:-/tmp}/cave-iso.XXXXXX")"
chmod 700 "$root"
cleanup() {
  if [[ $keep -eq 1 ]]; then
    echo "isolated.sh: kept $root" >&2
  else
    rm -rf "$root"
  fi
}
trap cleanup EXIT

mkdir -m 700 "$root/home" "$root/config" "$root/state" "$root/xdg-config" "$root/xdg-state" \
  "$root/xdg-cache" "$root/xdg-data" "$root/run"
if [[ -n "$config" ]]; then
  cp -R "$config"/. "$root/config"/
  chmod -R go-rwx "$root/config"
fi

export CAVE_ISOLATED_ROOT="$root"
export CAVE_CONFIG_DIR="$root/config"
export CAVE_STATE_DIR="$root/state"
export HOME="$root/home"
export XDG_CONFIG_HOME="$root/xdg-config"
export XDG_STATE_HOME="$root/xdg-state"
export XDG_CACHE_HOME="$root/xdg-cache"
export XDG_DATA_HOME="$root/xdg-data"
export XDG_RUNTIME_DIR="$root/run"
unset SSH_AUTH_SOCK SSH_AGENT_PID SSH_ASKPASS

# Run the command and keep its exit status; the trap removes the folder.
set +e
"$@"
status=$?
exit "$status"
