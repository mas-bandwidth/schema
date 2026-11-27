#!/bin/bash
# make/checks/wire-fuzz-release-run.sh — ONE RUN of the release-tier wire-fuzz
# gate (make/wire-fuzz-release.mk): one leg, one arm, one seed, through the
# shared harness, written as one row of the gate's table.
#
#   bash make/checks/wire-fuzz-release-run.sh <leg> <arm> <seed> <n> <dir>
#
# <arm> is `plain` or `retain` (the harness's --retain). The leg's driver
# command, extra harness flags and environment arrive as WFR_DRIVER, WFR_FLAGS
# and WFR_ENV, because a driver is a command line and make passes it whole.
# An empty WFR_DRIVER is a leg with no wire-fuzz driver at all.
#
# The row is <dir>/<leg>.<arm>.<seed>.row, tab-separated: leg, arm, seed,
# verdict. The verdict is one of
#   pass (<M> mutants, <S> s[, <K> ledgered seeds absent (<ledger>)])
#   RED: <the first divergence: root, mutant, what differs>
#   RED: <a run that exited 0 and proved nothing: no summary, no seed, no
#        mutant, or an absence in words other than the harness's ledgered
#        clause>
#   owed: <the owed list's entry>            (and the arm runs red, or has no driver)
#   RED: owed but green: ...                 (the owed entry is stale; remove it)
# The run itself exits 0 whatever the verdict, so every run of a leg finishes
# and lands its row; the leg's target reads its roster of rows BY NAME
# (tools/wirefuzzrelease table) and is red on a RED row and on a missing one.
# A run whose row could not be written exits 2.
#
# THE OWED LIST is test/conformance/wire-fuzz-owed.txt: `<leg> <arm> <why>`.
# An owed arm still RUNS when the leg has a driver, so an owed entry that has
# gone green turns the gate red until the entry is removed; an owed arm is
# never a silent skip.
#
# THE ABSENT SEEDS are the harness's: a seed the leg has no codec for is held
# by name in the leg's ledger under test/conformance/wire-absent/ or the run
# stops red (--leg, test/conformance/harness/wirefuzz_absent.go, W-ROSTER).
# The row carries the ledgered count the summary prints.
set -eu -o pipefail
leg=$1 arm=$2 seed=$3 n=$4 dir=$5
owedfile=${WFR_OWED:-test/conformance/wire-fuzz-owed.txt}
mkdir -p "$dir"
base=$dir/$leg.$arm.$seed
row=$base.row log=$base.log
rm -f "$row" "$log" "$base.bin"

# THE VERDICT is tools/wirefuzzrelease's (`row`): the owed list and the rule
# every leg is held to live there, under unit tests.
tool=${WFR_TOOL:-go run ./tools/wirefuzzrelease}

if [ -z "${WFR_DRIVER:-}" ]; then
	# shellcheck disable=SC2086 # WFR_TOOL is a command line
	$tool row -leg "$leg" -arm "$arm" -seed "$seed" -dir "$dir" -owed "$owedfile" -no-driver || exit 2
	exit 0
fi

retain=
[ "$arm" = retain ] && retain=--retain
start=$(date +%s)
# A failed harness is a scored run. Save its status for the row writer.
status=0
# shellcheck disable=SC2086 # WFR_ENV and WFR_FLAGS are word lists by design
env ${WFR_ENV:-} ./build/conformance-harness wire-fuzz --leg "$leg" ${WFR_FLAGS:-} $retain \
	--driver "$WFR_DRIVER" --seed "$seed" --n "$n" --failed "$base.bin" > "$log" 2>&1 || status=$?
secs=$(( $(date +%s) - start ))
# shellcheck disable=SC2086
$tool row -leg "$leg" -arm "$arm" -seed "$seed" -dir "$dir" -owed "$owedfile" -status "$status" -secs "$secs" -n "$n" || exit 2
exit 0
