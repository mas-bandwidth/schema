#!/bin/sh
# make/checks/wire-fuzz-release-run.sh — ONE RUN of the release-tier wire-fuzz
# gate (make/wire-fuzz-release.mk): one leg, one arm, one seed, through the
# shared harness, written as one row of the gate's table.
#
#   sh make/checks/wire-fuzz-release-run.sh <leg> <arm> <seed> <n> <dir>
#
# <arm> is `plain` or `retain` (the harness's --retain). The leg's driver
# command, extra harness flags and environment arrive as WFR_DRIVER, WFR_FLAGS
# and WFR_ENV, because a driver is a command line and make passes it whole.
# An empty WFR_DRIVER is a leg with no wire-fuzz driver at all.
#
# The row is <dir>/<leg>.<arm>.<seed>.row, tab-separated: leg, arm, seed,
# verdict. The verdict is one of
#   pass (<M> mutants, <S> s)
#   RED: <the first divergence: root, mutant, what differs>
#   owed: <the owed list's entry>            (and the arm runs red, or has no driver)
#   RED: owed but green: ...                 (the owed entry is stale; remove it)
# The run itself always exits 0, so every run of a leg finishes and lands its
# row; the leg's target reads the rows and is red on any RED.
#
# THE OWED LIST is test/conformance/wire-fuzz-owed.txt: `<leg> <arm> <why>`.
# An owed arm still RUNS when the leg has a driver, so an owed entry that has
# gone green turns the gate red until the entry is removed; an owed arm is
# never a silent skip.
set -u
leg=$1 arm=$2 seed=$3 n=$4 dir=$5
owedfile=${WFR_OWED:-test/conformance/wire-fuzz-owed.txt}
mkdir -p "$dir"
base=$dir/$leg.$arm.$seed
row=$base.row log=$base.log
rm -f "$row" "$log" "$base.bin"

owed=$(awk -v l="$leg" -v a="$arm" '!/^#/ && $1 == l && $2 == a { $1 = ""; $2 = ""; sub(/^ +/, ""); print; exit }' "$owedfile")

emit() { printf '%s\t%s\t%s\t%s\n' "$leg" "$arm" "$seed" "$1" > "$row"; }

if [ -z "${WFR_DRIVER:-}" ]; then
	if [ -n "$owed" ]; then
		emit "owed: $owed"
	else
		emit "RED: the leg has no wire-fuzz driver and the owed list does not name this arm"
	fi
	exit 0
fi

retain=
[ "$arm" = retain ] && retain=--retain
start=$(date +%s)
# shellcheck disable=SC2086 # WFR_ENV and WFR_FLAGS are word lists by design
env ${WFR_ENV:-} ./build/conformance-harness wire-fuzz ${WFR_FLAGS:-} $retain \
	--driver "$WFR_DRIVER" --seed "$seed" --n "$n" --failed "$base.bin" > "$log" 2>&1
status=$?
secs=$(( $(date +%s) - start ))

if [ $status -eq 0 ] && grep -qE '(^|[^0-9])0 divergences' "$log" && grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' "$log"; then
	mutants=$(sed -nE 's/.* = ([0-9]+) mutants.*/\1/p' "$log" | tail -1)
	if [ -n "$owed" ]; then
		emit "RED: owed but green ($mutants mutants): remove '$leg $arm' from $owedfile"
	else
		emit "pass ($mutants mutants, $secs s)"
	fi
	exit 0
fi

# THE FIRST DIVERGENCE. The harness names it as
#   FAILED after <P> mutants: <what differs>
#     corpus seed <name> (<unit>.<root>), pass <pass> #<i>, run seed <S>, ...
# and the row carries the root, the mutant (the one after the P that passed)
# and what differs. Anything else (a leg that died, a roster it did not
# answer) is the harness's first error line.
first=$(grep -m1 -E 'FAILED after [0-9]+ mutants' "$log")
if [ -n "$first" ]; then
	passed=$(printf '%s\n' "$first" | sed -nE 's/.*FAILED after ([0-9]+) mutants.*/\1/p')
	what=$(printf '%s\n' "$first" | sed -E 's/.*FAILED after [0-9]+ mutants: //')
	where=$(grep -m1 -E '^ *corpus seed ' "$log" | sed -nE 's/^ *corpus seed [^ ]+ \(([^)]*)\), pass ([^,]+),.*/\1, pass \2/p')
	verdict="RED: $where, mutant $((passed + 1)): $what"
else
	line=$(grep -m1 -E 'FAILED|harness:|wire-fuzz:|panic|error' "$log" || tail -1 "$log")
	verdict="RED: exit $status: $line"
fi
verdict=$(printf '%s' "$verdict" | tr '\t\n' '  ' | cut -c1-400)
if [ -n "$owed" ]; then
	emit "owed: $owed (runs red: ${verdict#RED: })"
else
	emit "$verdict"
fi
exit 0
