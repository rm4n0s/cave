#!/usr/bin/env bash
# checks.sh runs the standard checks of docs/IMPLEMENTATION.md rule 10 (CLAUDE.md "Definition of
# done"), in order, and stops at the first that fails. The heavy ones go through scripts/heavy.sh,
# so that at most two heavy commands run on this machine at once (CLAUDE.md "Memory (OOM)").
#
#   scripts/checks.sh             every check
#   scripts/checks.sh --no-wine   every check but the tests under Wine (while working; never to
#                                 finish a step)
#
# A check whose script does not exist yet (scripts/error-tags.sh comes with step 0.2,
# scripts/test-windows.sh with step 0.5) is skipped with a line that says so. Exit status: 0 when
# every check passed, else the status of the check that failed.
set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

wine=1
for arg in "$@"; do
	case "$arg" in
	--no-wine) wine=0 ;;
	*)
		echo "usage: scripts/checks.sh [--no-wine]" >&2
		exit 2
		;;
	esac
done

heavy="$root/scripts/heavy.sh"
failed=""

# check <name> <command...> runs one check and remembers the first that failed.
check() {
	local name="$1"
	shift
	[[ -n "$failed" ]] && return
	echo "==> $name"
	if ! "$@"; then
		failed="$name"
	fi
}

# gofmt_clean fails, listing them, when a file of cmd or internal is not formatted.
gofmt_clean() {
	local files
	files="$(gofmt -l cmd internal)" || return 1
	if [[ -n "$files" ]]; then
		echo "not formatted (gofmt -w cmd internal):" >&2
		echo "$files" >&2
		return 1
	fi
}

# script_check <name> <script> [args...] runs a check that is a script of this folder, or says
# that the script does not exist yet.
script_check() {
	local name="$1" script="$2"
	shift 2
	[[ -n "$failed" ]] && return
	if [[ ! -x "$root/scripts/$script" ]]; then
		echo "==> $name: skipped, scripts/$script does not exist yet"
		return
	fi
	check "$name" "$@"
}

check "gofmt" gofmt_clean
check "go vet" go vet ./...
check "go vet (windows)" env GOOS=windows go vet ./...
check "golangci-lint" "$heavy" -- golangci-lint run
check "tests, race detector, debug build" "$heavy" -- GOMEMLIMIT=3GiB go test -race -p 2 -tags debug_mode ./...
check "build without cgo" env CGO_ENABLED=0 go build ./...
check "tests without cgo, release build" "$heavy" -- CGO_ENABLED=0 go test ./...
check "build for windows" env GOOS=windows CGO_ENABLED=0 go build ./...
script_check "error tags" error-tags.sh "$root/scripts/error-tags.sh" --untested
if [[ $wine -eq 1 ]]; then
	script_check "tests under Wine" test-windows.sh "$heavy" --alone -- "$root/scripts/test-windows.sh"
else
	echo "==> tests under Wine: skipped, --no-wine"
fi

if [[ -n "$failed" ]]; then
	echo "FAILED: $failed" >&2
	exit 1
fi
echo "all checks passed"
