#!/usr/bin/env bash
# Captures the screen of the TUI as a proof (docs/IMPLEMENTATION.md "Proof methods", method U):
# tcave runs isolated (scripts/isolated.sh) in a private tmux server, on a loopback port that
# the system chose, gets the keys it is told to, and the screen is saved as text.
#
# Usage: scripts/tui-proof.sh <out.txt> [options] -- [tcave args]
#
#   --config <dir>    the fixture: a Cave configuration folder (copied; the original is never
#                     changed). Without it the configuration is empty. The fixture's settings.json
#                     must set server.address to 127.0.0.1 or ::1: a proof never listens on the
#                     network, and the script refuses a fixture that does
#   --size <cols>x<rows>   the terminal's size (default 100x30)
#   --during <command>     run this shell command in the background once the TUI is up, for
#                     example real ssh attempts: it gets CAVE_PROOF_PORT (the port of the server)
#                     and CAVE_PROOF_FIXTURE (the copy of the fixture) in its environment, runs in
#                     the repository, under a timeout and in its own process group
#   --keys <keys>     send tmux key names, separated by spaces: '2', 'Enter', 'C-d', '?'
#   --text <text>     type this text as it is
#   --wait <regex>    wait (15 seconds at most) until a line of the screen matches the regular
#                     expression (grep -E); fail, and print the screen, when none does
#
# --keys, --text and --wait can be repeated and are done in the order they were given. The screen
# is saved after the last one, half a second later. A port changes from run to run, so a proof is
# looked at, not compared.
#
# Everything the script starts (tmux, tcave, the --during command) is stopped when it ends, also
# after a failure. Exit status: 0 on success, 1 when a wait failed or something did not start, 64
# for a usage mistake.
set -euo pipefail

usage() {
  sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//' >&2
  exit 64
}

[[ $# -ge 2 ]] || usage
out="$1"
shift
config=""
size="100x30"
during=""
steps=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) [[ $# -ge 2 ]] || usage; config="$2"; shift 2 ;;
    --size) [[ $# -ge 2 ]] || usage; size="$2"; shift 2 ;;
    --during) [[ $# -ge 2 ]] || usage; during="$2"; shift 2 ;;
    --keys) [[ $# -ge 2 ]] || usage; steps+=("keys:$2"); shift 2 ;;
    --text) [[ $# -ge 2 ]] || usage; steps+=("text:$2"); shift 2 ;;
    --wait) [[ $# -ge 2 ]] || usage; steps+=("wait:$2"); shift 2 ;;
    --) shift; break ;;
    *) usage ;;
  esac
done
[[ "$size" =~ ^[0-9]+x[0-9]+$ ]] || { echo "error: --size must be <cols>x<rows>, not '$size'" >&2; exit 64; }
cols="${size%x*}"
rows="${size#*x}"
if [[ -n "$config" && ! -d "$config" ]]; then
  echo "error: the fixture folder '$config' does not exist" >&2
  exit 64
fi
for tool in tmux go timeout; do
  command -v "$tool" >/dev/null 2>&1 || { echo "error: $tool is not installed." >&2; exit 1; }
done

root="$(cd "$(dirname "$0")/.." && pwd)"
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac

work="$(mktemp -d "${TMPDIR:-/tmp}/cave-tui.XXXXXX")"
chmod 700 "$work"
sock="cave-proof-$$"
export TMUX_TMPDIR="$work/tmux"   # a tmux server of its own: the user's is never touched
mkdir -m 700 "$TMUX_TMPDIR"
unset TMUX
during_pid=""

cleanup() {
  local status=$?
  trap - EXIT
  if [[ -n "$during_pid" ]]; then
    kill -TERM -- "-$during_pid" 2>/dev/null || true
  fi
  if tmux -L "$sock" has-session -t proof 2>/dev/null; then
    tmux -L "$sock" send-keys -t proof q 2>/dev/null || true
    sleep 0.2
    tmux -L "$sock" send-keys -t proof y 2>/dev/null || true
    sleep 0.2
  fi
  tmux -L "$sock" kill-server 2>/dev/null || true
  if [[ -n "$during_pid" ]]; then
    kill -KILL -- "-$during_pid" 2>/dev/null || true
    wait "$during_pid" 2>/dev/null || true
  fi
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT

# The fixture: a copy, which must listen on loopback only.
mkdir -m 700 "$work/fixture"
if [[ -n "$config" ]]; then
  cp -R "$config"/. "$work/fixture"/
else
  printf '{"version":1,"settings":{"server.address":"127.0.0.1"}}\n' >"$work/fixture/settings.json"
fi
if ! grep -Eq '"server\.address"[[:space:]]*:[[:space:]]*"(127\.0\.0\.1|::1)"' "$work/fixture/settings.json" 2>/dev/null; then
  echo "error: the fixture's settings.json must set \"server.address\" to \"127.0.0.1\" or \"::1\"; a proof never listens on the network" >&2
  exit 64
fi

(cd "$root" && go build -o "$work/tcave" ./cmd/tcave)
port="$(cd "$root" && go run scripts/freeport.go)"

command_line="$(printf '%q ' exec timeout -k 2 120 "$root/scripts/isolated.sh" --config "$work/fixture" -- "$work/tcave" --port "$port" "$@")"
tmux -L "$sock" -f /dev/null new-session -d -s proof -x "$cols" -y "$rows" "$command_line"

screen() { tmux -L "$sock" capture-pane -p -t proof; }

fail_with_screen() {
  echo "error: $1" >&2
  echo "--- the screen:" >&2
  screen >&2 || true
  exit 1
}

wait_for() { # regex
  local deadline=$((SECONDS + 15))
  until screen | grep -Eq -- "$1"; do
    [[ $SECONDS -lt $deadline ]] || fail_with_screen "no line of the screen matches '$1'"
    sleep 0.1
  done
}

wait_for 'Cave'

if [[ -n "$during" ]]; then
  set -m # job control, so that the command has a process group of its own
  (
    cd "$root"
    export CAVE_PROOF_PORT="$port" CAVE_PROOF_FIXTURE="$work/fixture"
    exec timeout -k 2 60 bash -c "$during"
  ) &
  during_pid=$!
  set +m
fi

for step in "${steps[@]+"${steps[@]}"}"; do
  kind="${step%%:*}"
  value="${step#*:}"
  case "$kind" in
    keys)
      # shellcheck disable=SC2086 # the value is a list of tmux key names
      tmux -L "$sock" send-keys -t proof $value
      sleep 0.3
      ;;
    text)
      tmux -L "$sock" send-keys -t proof -l -- "$value"
      sleep 0.3
      ;;
    wait) wait_for "$value" ;;
  esac
done

sleep 0.5
mkdir -p "$(dirname "$out")"
screen >"$out"
echo "tui-proof.sh: wrote $out"
