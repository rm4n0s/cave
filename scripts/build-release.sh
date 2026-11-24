#!/usr/bin/env bash
# Builds the files of a release into <dist>/<version>/: one archive for each target, and
# SHA256SUMS. It uploads nothing and makes no tag: publishing a release's files is the author's
# decision (CLAUDE.md "Git and GitHub").
#
# Usage: scripts/build-release.sh [--version <v>] [--dist <dir>]
#        (defaults: the release tag when the checkout is exactly on one, else the version in
#         development without "-dev"; dist/)
#
# Targets (every program is cgo-free and built with -trimpath):
#   linux/amd64, linux/arm64      cave-<v>-linux-<arch>.tar.gz   cave, tcave, cave.desktop, icons/,
#                                                                README.md, LICENSE
#   darwin/amd64, darwin/arm64    cave-<v>-macos-<arch>.tar.gz   cave, tcave, README.md, LICENSE
#   windows/amd64, windows/arm64  cave-<v>-windows-<arch>.zip    cave.exe, tcave.exe, README.md,
#                                                                LICENSE
#
# The Windows programs are built by scripts/build-windows.sh. This is a heavy command: run it
# through scripts/heavy.sh --alone (CLAUDE.md "Memory (OOM)").
#
# Needs Go, tar (GNU or BSD), zip and sha256sum. Exit status: 0 on success; 2 when a tool is missing; 64 for a
# usage mistake; the build's own otherwise.
set -euo pipefail

usage() { echo "usage: $0 [--version <v>] [--dist <dir>]" >&2; exit 64; }

root="$(cd "$(dirname "$0")/.." && pwd)"
version=""
dist="$root/dist"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) [[ $# -ge 2 ]] || usage; version="$2"; shift 2 ;;
    --dist) [[ $# -ge 2 ]] || usage; dist="$2"; shift 2 ;;
    *) usage ;;
  esac
done
for tool in go tar zip sha256sum; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: $tool is not installed." >&2
    exit 2
  fi
done
case "$dist" in /*) ;; *) dist="$PWD/$dist" ;; esac

cd "$root"
if [[ -z "$version" ]]; then
  version="$(git describe --tags --exact-match 2>/dev/null | sed -n 's/^v\([0-9]*\.[0-9]*\.[0-9]*\)$/\1/p' || true)"
fi
if [[ -z "$version" ]]; then
  version="$(sed -n 's/^const devVersion = "\([0-9]*\.[0-9]*\.[0-9]*\)-dev"$/\1/p' internal/app/version.go)"
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { echo "error: '$version' is not a version" >&2; exit 64; }

out="$dist/$version"
work="$(mktemp -d "${TMPDIR:-/tmp}/cave-release.XXXXXX")"
trap 'rm -rf "$work"' EXIT
mkdir -p "$out"
rm -f "$out"/cave-"$version"-* "$out/SHA256SUMS"

# pack <name>: a .tar.gz of the folder <name> of the work folder, whose files belong to root.
if tar --version 2>/dev/null | grep -q bsdtar; then
  owner=(--uid 0 --gid 0 --numeric-owner)
else
  owner=(--owner=0 --group=0 --numeric-owner)
fi
pack() {
  tar -C "$work" "${owner[@]}" -czf "$out/$1.tar.gz" "$1"
}

for arch in amd64 arm64; do
  name="cave-$version-linux-$arch"
  dir="$work/$name"
  mkdir -p "$dir/icons"
  for program in cave tcave; do
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -o "$dir/$program" "./cmd/$program"
  done
  cp packaging/cave.desktop "$dir/"
  cp packaging/icons/*.png "$dir/icons/"
  cp README.md LICENSE "$dir/"
  pack "$name"
done

for arch in amd64 arm64; do
  name="cave-$version-macos-$arch"
  dir="$work/$name"
  mkdir -p "$dir"
  for program in cave tcave; do
    GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -o "$dir/$program" "./cmd/$program"
  done
  cp README.md LICENSE "$dir/"
  pack "$name"
done

for arch in amd64 arm64; do
  name="cave-$version-windows-$arch"
  dir="$work/$name"
  mkdir -p "$dir"
  "$root/scripts/build-windows.sh" --out "$dir" --arch "$arch" --version "$version" >/dev/null
  cp README.md LICENSE "$dir/"
  (cd "$work" && zip -qr "$out/$name.zip" "$name")
done

(cd "$out" && sha256sum cave-"$version"-* >SHA256SUMS)
echo "build-release.sh: $out"
ls -l "$out"
