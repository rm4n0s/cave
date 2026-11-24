#!/usr/bin/env bash
# Builds Cave for Windows (amd64): tcave.exe, a console program, and cave.exe, a GUI program that
# opens no console window (Glutter's glutter-windows tool: the manifest, the version information
# and the subsystem). Both are cgo-free, so no Windows toolchain is needed.
#
# Usage: scripts/build-windows.sh [--out <dir>]      (default: dist/windows-amd64)
#
# The version in the file properties of cave.exe is the release tag when the checkout is exactly
# on one, else the version in development (internal/app/version.go), as 1.2.3.4. dist/ is
# git-ignored. This is a heavy command: run it through scripts/heavy.sh (CLAUDE.md "Memory (OOM)").
#
# Exit status: 0 on success; 64 for a usage mistake; the build's own otherwise.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/dist/windows-amd64"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) [[ $# -ge 2 ]] || { echo "usage: $0 [--out <dir>]" >&2; exit 64; }; out="$2"; shift 2 ;;
    *) echo "usage: $0 [--out <dir>]" >&2; exit 64 ;;
  esac
done
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac
mkdir -p "$out"

cd "$root"
version="$(git describe --tags --exact-match 2>/dev/null | sed -n 's/^v\([0-9]*\.[0-9]*\.[0-9]*\)$/\1/p' || true)"
if [[ -z "$version" ]]; then
  version="$(sed -n 's/^const devVersion = "\([0-9]*\.[0-9]*\.[0-9]*\)-dev"$/\1/p' internal/app/version.go)"
fi
[[ -n "$version" ]] || { echo "build-windows.sh: no version found" >&2; exit 1; }

GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o "$out/tcave.exe" ./cmd/tcave
# Until step 8.2 draws one, cave.exe has Glutter's icon.
go run github.com/rm4n0s/glutter/cmd/glutter-windows build -arch amd64 -name Cave -app-id org.rm4n0s.cave \
  -version "$version.0" -o "$out/cave.exe" ./cmd/cave
echo "build-windows.sh: $out/tcave.exe and $out/cave.exe (version $version)"
