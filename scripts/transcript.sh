#!/usr/bin/env bash
# Saves the output of a command as a proof (docs/IMPLEMENTATION.md "Proof methods", method T):
# the command line, then its standard output and error, with what changes from run to run taken
# out, so that the file is stable: temporary paths, the ports of loopback addresses, and the
# durations that `go test` prints.
#
# Usage: scripts/transcript.sh <out.txt> -- <command> [args...]
#
# The file is written only when the command succeeds: a failed run proves nothing. Exit status:
# the command's own; 64 for a usage mistake.
set -uo pipefail

if [[ $# -lt 3 || "$2" != "--" ]]; then
  echo "usage: $0 <out.txt> -- <command> [args...]" >&2
  exit 64
fi
out="$1"
shift 2

raw="$(mktemp "${TMPDIR:-/tmp}/cave-transcript.XXXXXX")"
trap 'rm -f "$raw"' EXIT

"$@" >"$raw" 2>&1
status=$?
if [[ $status -ne 0 ]]; then
  cat "$raw" >&2
  echo "transcript.sh: the command failed with status $status; $out was not written" >&2
  exit "$status"
fi

tmp="${TMPDIR:-/tmp}"
{
  printf '$'
  printf ' %q' "$@"
  printf '\n'
  sed -E \
    -e "s#${tmp%/}/[A-Za-z0-9._-]+(/[0-9]{3})?#<tmp>#g" \
    -e 's#[A-Za-z]:\\(users|windows)\\[^ "]*\\(Temp|temp)\\[A-Za-z0-9._-]+(\\[0-9]{3})?#<tmp>#g' \
    -e 's#(127\.0\.0\.1|\[::1\]|localhost):[0-9]+#\1:<port>#g' \
    -e 's# \([0-9]+\.[0-9]+s\)##' \
    -e 's#^((ok|FAIL|PASS)[[:space:]].*)[[:space:]][0-9]+\.[0-9]+s$#\1#' \
    "$raw"
} >"$out"
echo "transcript.sh: wrote $out"
