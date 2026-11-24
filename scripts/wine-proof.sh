#!/usr/bin/env bash
# Takes a screenshot of cave.exe running under Wine, as a proof (docs/IMPLEMENTATION.md "Proof
# methods", method W). From Glutter's scripts/wine-screenshot.sh (github.com/rm4n0s/glutter,
# BSD-3-Clause, same author), with Cave's names: cave.exe is built (Glutter's glutter-windows
# tool), and runs under Wine in Cave's private prefix (scripts/wine-exec.sh) inside a headless X
# server (scripts/headless-x11.sh), with a copy of the fixture as its configuration, on a
# loopback port that the system chose. When the app logs its first frame, and --after seconds
# later, the root window of the X server is saved with ImageMagick's `import`.
#
# Usage: scripts/wine-proof.sh <out.png> [--config <fixture dir>] [--size 960x640] [--after <seconds>]
#                              -- [cave args]
#
#   --config <dir>   the fixture: a Cave configuration folder (copied; the original is never
#                    changed). Without it the configuration is empty. Its settings.json must set
#                    server.address to 127.0.0.1 or ::1: a proof never listens on the network, and
#                    the script refuses a fixture that does not
#   --size <w>x<h>   the size of the X screen, and so of the window, which opens at its own size
#                    (default 1000x700)
#   --after <s>      seconds to wait after the first frame before the screen is saved (default 2)
#
# The folders of the app are under a temporary folder; the user's ~/.wine is never used. Everything
# the script starts (Xvfb, Wine, cave.exe) is stopped when it ends, also after a failure. The port
# changes from run to run, so a proof is looked at, not compared. A heavy command: run it through
# scripts/heavy.sh --alone (CLAUDE.md "Memory (OOM)").
#
# Needs Wine, Xvfb and ImageMagick: sudo dnf install wine-core xorg-x11-server-Xvfb ImageMagick
#
# Exit status: 0 when the file was saved; 1 when cave.exe ended before the capture or no frame
# was drawn; 2 when a tool is missing; 64 for a usage mistake.
set -euo pipefail

usage() {
  sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//' >&2
  exit 64
}

[[ $# -ge 2 ]] || usage
out="$1"
shift
config=""
size="1000x700"
after="2"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config) [[ $# -ge 2 ]] || usage; config="$2"; shift 2 ;;
    --size) [[ $# -ge 2 ]] || usage; size="$2"; shift 2 ;;
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

for tool in wine Xvfb import go timeout; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: $tool is not installed." >&2
    echo "       install what is needed with: sudo dnf install wine-core xorg-x11-server-Xvfb ImageMagick" >&2
    exit 2
  fi
done

root="$(cd "$(dirname "$0")/.." && pwd)"
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac

work="$(mktemp -d "${TMPDIR:-/tmp}/cave-wine-proof.XXXXXX")"
chmod 700 "$work"
trap 'rm -rf "$work"' EXIT

# The fixture: a copy, which must listen on loopback only.
mkdir -m 700 "$work/config" "$work/state"
if [[ -n "$config" ]]; then
  cp -R "$config"/. "$work/config"/
else
  printf '{"version":1,"settings":{"server.address":"127.0.0.1"}}\n' >"$work/config/settings.json"
fi
if ! grep -Eq '"server\.address"[[:space:]]*:[[:space:]]*"(127\.0\.0\.1|::1)"' "$work/config/settings.json" 2>/dev/null; then
  echo "error: the fixture's settings.json must set \"server.address\" to \"127.0.0.1\" or \"::1\"; a proof never listens on the network" >&2
  exit 64
fi

(cd "$root" && go run github.com/rm4n0s/glutter/cmd/glutter-windows build -arch amd64 -name Cave \
  -app-id org.rm4n0s.cave -o "$work/cave.exe" ./cmd/cave >/dev/null)
port="$(cd "$root" && go run scripts/freeport.go)"
mkdir -p "$(dirname "$out")"

# Wine maps the whole file system to the drive Z:.
winpath() { printf 'Z:%s' "${1//\//\\}"; }
export CAVE_CONFIG_DIR CAVE_STATE_DIR
CAVE_CONFIG_DIR="$(winpath "$work/config")"
CAVE_STATE_DIR="$(winpath "$work/state")"
# The app draws on the CPU (Glutter's soft renderer): a proof never uses the GPU.
export GLUTTER_RENDERER=soft
export CAVE_WINE_INSIDE=1
export HEADLESS_SIZE="$size"
export CAVE_PROOF_OUT="$out" CAVE_PROOF_AFTER="$after" CAVE_PROOF_ROOT="$root"

set +e
timeout -k 10 300 "$root/scripts/headless-x11.sh" -- bash -c '
  ulimit -c 1
  log=$(mktemp)
  trap "cat \"\$log\"; rm -f \"\$log\"; wineserver -k >/dev/null 2>&1" EXIT
  set -m
  "$CAVE_PROOF_ROOT/scripts/wine-exec.sh" "$@" >"$log" 2>&1 &
  app=$!
  set +m
  # Wine takes seconds to draw the first frame: the engine says when it did.
  for ((i = 0; ; i++)); do
    grep -qF "first frame presented" "$log" && break
    if ! kill -0 "$app" 2>/dev/null; then
      echo "wine-proof: cave.exe ended before it drew a frame" >&2
      exit 1
    fi
    if ((i >= 900)); then
      echo "wine-proof: no frame in 90 seconds" >&2
      kill -TERM -- -"$app" 2>/dev/null
      exit 1
    fi
    sleep 0.1
  done
  sleep "$CAVE_PROOF_AFTER"
  status=1
  if import -window root "$CAVE_PROOF_OUT"; then status=0; else echo "wine-proof: the capture failed" >&2; fi
  kill -TERM -- -"$app" 2>/dev/null
  for ((i = 0; i < 100; i++)); do kill -0 "$app" 2>/dev/null || break; sleep 0.1; done
  kill -KILL -- -"$app" 2>/dev/null
  wait "$app" 2>/dev/null
  exit "$status"
' bash "$work/cave.exe" --port "$port" "$@"
status=$?
set -e
exit "$status"
