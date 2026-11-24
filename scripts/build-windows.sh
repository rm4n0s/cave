#!/usr/bin/env bash
# Builds Cave for Windows: tcave.exe, a console program, and cave.exe, a GUI program that opens no
# console window (Glutter's glutter-windows tool: the manifest, the icon, the version information
# and the subsystem). Both are cgo-free, so no Windows toolchain is needed.
#
# Usage: scripts/build-windows.sh [--out <dir>] [--arch amd64|arm64]
#        (defaults: dist/windows-<arch>, amd64)
#
# The version in the file properties of cave.exe is the release tag when the checkout is exactly
# on one, else the version in development (internal/app/version.go), as 1.2.3.0; --version <v>
# gives another (such as 0.0.0-test, which becomes 0.0.0.0). The icon is Cave's (packaging/icons).
# dist/ is git-ignored. This is a heavy command: run it through scripts/heavy.sh (CLAUDE.md
# "Memory (OOM)").
#
# Exit status: 0 on success; 64 for a usage mistake; the build's own otherwise.
set -euo pipefail

usage() { echo "usage: $0 [--out <dir>] [--arch amd64|arm64] [--version <v>]" >&2; exit 64; }

root="$(cd "$(dirname "$0")/.." && pwd)"
out=""
arch="amd64"
version=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) [[ $# -ge 2 ]] || usage; out="$2"; shift 2 ;;
    --arch) [[ $# -ge 2 ]] || usage; arch="$2"; shift 2 ;;
    --version) [[ $# -ge 2 ]] || usage; version="$2"; shift 2 ;;
    *) usage ;;
  esac
done
case "$arch" in amd64 | arm64) ;; *) usage ;; esac
[[ -n "$out" ]] || out="$root/dist/windows-$arch"
case "$out" in /*) ;; *) out="$PWD/$out" ;; esac
mkdir -p "$out"

cd "$root"
if [[ -z "$version" ]]; then
  version="$(git describe --tags --exact-match 2>/dev/null | sed -n 's/^v\([0-9]*\.[0-9]*\.[0-9]*\)$/\1/p' || true)"
fi
if [[ -z "$version" ]]; then
  version="$(sed -n 's/^const devVersion = "\([0-9]*\.[0-9]*\.[0-9]*\)-dev"$/\1/p' internal/app/version.go)"
fi
[[ -n "$version" ]] || { echo "build-windows.sh: no version found" >&2; exit 1; }
# The version of the file properties is four numbers.
numeric="$(sed -n 's/^\([0-9]*\.[0-9]*\.[0-9]*\).*$/\1/p' <<<"$version")"
[[ -n "$numeric" ]] || { echo "build-windows.sh: '$version' is not a version" >&2; exit 64; }

GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -o "$out/tcave.exe" ./cmd/tcave
go run github.com/rm4n0s/glutter/cmd/glutter-windows build -arch "$arch" -name Cave -app-id org.rm4n0s.cave \
  -icon packaging/icons/cave-256.png -version "$numeric.0" -o "$out/cave.exe" ./cmd/cave
echo "build-windows.sh: $out/tcave.exe and $out/cave.exe ($arch, version $version)"
