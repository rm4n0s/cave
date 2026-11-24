#!/usr/bin/env bash
# Takes a screenshot of the desktop app as a proof (docs/IMPLEMENTATION.md "Proof methods",
# method D). From Glutter's scripts/screenshot-headless.sh (github.com/rm4n0s/glutter,
# BSD-3-Clause, same author), with Cave's names: cave runs isolated (scripts/isolated.sh) inside
# a headless sway (scripts/headless-wayland.sh, software rendering, never the GPU), on a loopback
# port that the system chose; when its window is mapped, the screen is saved with grim.
#
# Usage: scripts/proof.sh <out.png> [--config <fixture dir>] [--size 960x640] [--during '<command>']
#                         [--after <seconds>] -- [cave args]
#
#   --config <dir>   the fixture: a Cave configuration folder (copied; the original is never
#                    changed). Without it the configuration is empty. The fixture's settings.json
#                    must set server.address to 127.0.0.1 or ::1: a proof never listens on the
#                    network, and the script refuses a fixture that does not
#   --size <w>x<h>   the size of the screen, and so of the window, which is floating at its own
#                    size: sway has no way to resize a window here, so the window is as big as the
#                    output (default 960x640)
#   --during <cmd>   run this shell command in the background once the window is mapped, for
#                    example real ssh attempts: it gets CAVE_PROOF_PORT (the port of the server)
#                    and CAVE_PROOF_FIXTURE (the copy of the fixture) in its environment, runs in
#                    the repository, under timeout -k 2 60 and in its own process group
#   --after <s>      seconds to wait, after the window is mapped and --during started, before the
#                    screen is saved (default 1)
#
# Everything the script starts (sway, cave, the --during command) is stopped when it ends, also
# after a failure. A port changes from run to run, so a proof is looked at, not compared.
#
# Needs sway, grim and jq: sudo dnf install sway grim jq
#
# Exit status: 0 on success; 1 when no window appeared or cave ended early; 2 when a tool is
# missing; 64 for a usage mistake.
set -euo pipefail

usage() {
  sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//' >&2
  exit 64
}

[[ $# -ge 2 ]] || usage
out="$1"
shift
config=""
size="960x640"
during=""
after="1"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) [[ $# -ge 2 ]] || usage; config="$2"; shift 2 ;;
    --size) [[ $# -ge 2 ]] || usage; size="$2"; shift 2 ;;
    --during) [[ $# -ge 2 ]] || usage; during="$2"; shift 2 ;;
    --after) [[ $# -ge 2 ]] || usage; after="$2"; shift 2 ;;
    --) shift; break ;;
    *) usage ;;
  esac
done
[[ "$size" =~ ^[0-9]+x[0-9]+$ ]] || { echo "error: --size must be <w>x<h>, not '$size'" >&2; exit 64; }
[[ "$after" =~ ^[0-9]+(\.[0-9]+)?$ ]] || { echo "error: --after must be a number of seconds, not '$after'" >&2; exit 64; }
if [[ -n "$config" && ! -d "$config" ]]; then
  echo "error: the fixture folder '$config' does not exist" >&2
  exit 64
fi

for tool in sway swaymsg grim jq go timeout; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: $tool is not installed." >&2
    echo "       install what is needed with: sudo dnf install sway grim jq" >&2
    exit 2
  fi
done

root="$(cd "$(dirname "$0")/.." && pwd)"
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac

work="$(mktemp -d "${TMPDIR:-/tmp}/cave-proof.XXXXXX")"
chmod 700 "$work"
trap 'rm -rf "$work"' EXIT

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

(cd "$root" && go build -o "$work/cave" ./cmd/cave)
port="$(cd "$root" && go run scripts/freeport.go)"
mkdir -p "$(dirname "$out")"

# inner runs inside the headless compositor.
inner() {
  local out="$1" work="$2" port="$3" during="$4" after="$5"
  shift 5
  # isolated.sh gives cave a runtime folder of its own, so the compositor's socket is named by its
  # whole path (a Wayland client takes an absolute WAYLAND_DISPLAY as the path of the socket).
  export WAYLAND_DISPLAY="$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY"
  # The app draws on the CPU (Glutter's soft renderer): a proof never uses the GPU.
  export GLUTTER_RENDERER=soft
  # cave runs in a process group of its own, so that it can be stopped with everything it started.
  set -m
  timeout -k 2 120 "$root/scripts/isolated.sh" --config "$work/fixture" -- "$work/cave" --port "$port" "$@" &
  local app=$!
  local during_pid=""
  set +m
  # The trap runs after this function has returned: the process IDs are expanded now.
  trap "kill -TERM -- -$app 2>/dev/null || true; sleep 0.3; kill -KILL -- -$app 2>/dev/null || true; wait $app 2>/dev/null || true" EXIT

  local mapped=0
  for _ in $(seq 1 150); do
    if ! kill -0 "$app" 2>/dev/null; then
      wait "$app" 2>/dev/null || true
      echo "error: cave ended before it mapped a window." >&2
      return 1
    fi
    # A window node has a process ID; workspaces and containers do not.
    if [[ "$(swaymsg -t get_tree 2>/dev/null | jq '[.. | objects | select((.pid? // 0) > 0)] | length')" -gt 0 ]]; then
      mapped=1
      break
    fi
    sleep 0.1
  done
  if [[ "$mapped" -ne 1 ]]; then
    echo "error: no window was mapped within 15 seconds." >&2
    return 1
  fi
  if [[ -n "$during" ]]; then
    set -m
    (
      cd "$root"
      export CAVE_PROOF_PORT="$port" CAVE_PROOF_FIXTURE="$work/fixture"
      exec timeout -k 2 60 bash -c "$during"
    ) &
    during_pid=$!
    set +m
    trap "kill -TERM -- -$app 2>/dev/null || true; kill -TERM -- -$during_pid 2>/dev/null || true; sleep 0.3; kill -KILL -- -$app 2>/dev/null || true; kill -KILL -- -$during_pid 2>/dev/null || true; wait $app 2>/dev/null || true" EXIT
  fi
  sleep "$after"
  grim "$out"
}
export -f inner
export root

HEADLESS_SIZE="$size" WLR_RENDERER=pixman \
  "$root/scripts/headless-wayland.sh" -- bash -c 'inner "$@"' inner "$out" "$work" "$port" "$during" "$after" "$@"
echo "proof.sh: wrote $out"
