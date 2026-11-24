#!/usr/bin/env bash
# heavy.sh runs a heavy command (a full test run, the linter, a release build) in one of two slots that every agent on this
# machine shares, so that at most two heavy commands run at once (CLAUDE.md "Memory (OOM)"). With --alone it takes both slots
# and runs by itself: real-input display tests (scripts/realinput.sh), scripts/perf.sh and other timing measurements.
#
#   scripts/heavy.sh -- GOMEMLIMIT=3GiB go test -race -p 2 ./...
#   scripts/heavy.sh --alone -- scripts/realinput.sh --shots /tmp/shots
#
# It waits for a free slot (printing once that it waits), runs the command, and exits with the command's status. The locks are
# flock(1) locks on files in $XDG_RUNTIME_DIR (or /tmp), released by the kernel when the command ends, also when it is killed.
# A third lock, the gate, keeps an --alone command from waiting for ever: it holds the gate while it waits for the two slots,
# and a command that is not alone takes a free slot only through the gate, so none starts while an --alone command waits.
# macOS has no flock(1) and an old bash: the locks are then taken with Perl's flock, on the same descriptors.
set -euo pipefail

# The descriptors of the two slots and the gate (fixed numbers: the bash of macOS cannot choose them).
slot1=7
slot2=8
gate=9

# lock [-n|-s|-u|-x] <fd>: flock(1) when the system has it, else the same call through Perl. A lock belongs to the open file
# that the descriptor names, which this shell keeps, so it lasts after the program that took it ends.
lock() {
	if command -v flock >/dev/null 2>&1; then
		flock "$@"
		return
	fi
	local how=LOCK_EX
	local nb=0
	while [[ $# -gt 1 ]]; do
		case "$1" in
		-n) nb=1 ;;
		-s) how=LOCK_SH ;;
		-u) how=LOCK_UN ;;
		-x) how=LOCK_EX ;;
		esac
		shift
	done
	if [[ $nb -eq 1 ]]; then
		how="$how|LOCK_NB"
	fi
	perl -MFcntl=:flock -e 'open(my $f, ">&=", $ARGV[0]) or exit 2; flock($f, '"$how"') or exit 1' "$1"
}

# release <fd>...: closes the descriptors.
release() {
	local fd
	for fd in "$@"; do
		eval "exec $fd>&-"
	done
}

alone=0
while [[ $# -gt 0 ]]; do
	case "$1" in
	--alone) alone=1; shift ;;
	--) shift; break ;;
	*) break ;;
	esac
done
if [[ $# -eq 0 ]]; then
	echo "usage: scripts/heavy.sh [--alone] -- <command> [args...]" >&2
	exit 2
fi

dir="${XDG_RUNTIME_DIR:-/tmp}"
exec 7>"$dir/cave-heavy.1.lock"
exec 8>"$dir/cave-heavy.2.lock"
exec 9>"$dir/cave-heavy.gate.lock"

if [[ $alone -eq 1 ]]; then
	# The gate, then both slots, always in that order, holding nothing when it starts to wait (a slot that the quick try took
	# is given back first). A command that is not alone never waits while it holds a slot or the gate, so this cannot deadlock.
	if ! lock -n "$slot1" || ! lock -n "$slot2"; then
		lock -u "$slot1"
		echo "heavy.sh: waiting for both slots..." >&2
		lock -x "$gate"
		lock "$slot1"
		lock "$slot2"
	fi
	release "$gate"
else
	waited=0
	while true; do
		# the gate is shared among the commands that are not alone and held only for the two tries
		lock -s "$gate"
		if lock -n "$slot1"; then
			release "$slot2" "$gate"
			break
		fi
		if lock -n "$slot2"; then
			release "$slot1" "$gate"
			break
		fi
		lock -u "$gate"
		if [[ $waited -eq 0 ]]; then
			echo "heavy.sh: waiting for a free slot..." >&2
			waited=1
		fi
		sleep 2
	done
fi

# env lets the command start with VAR=value assignments.
exec env "$@"
