#!/bin/sh
# make/goose-wirefuzz.sh — THE WIRE-FUZZ DIFFERENTIAL for the Goose leg
# (docs/PORTING.md I15; make/goose-fuzz.mk says why it is shaped this way).
#
#   goose-wirefuzz.sh <goose> <harness> <units dir> <work dir> <seed> <n> [plain|retain|all]
#
# For every unit the goose target generated (a directory with a table
# module), builds the leg for that unit and runs the harness's arms over it:
# --builder over its variable-class file roots, the region arm (fixed) over
# its value-only roots, and --builder --retain over its variable roots. Which
# arms a unit has is read from the corpus, never from a list of names: the
# harness prints each profile's roster (wire-fuzz --list-roots), and an arm
# runs for every unit that roster names a root of that class for. An arm that
# runs and whose driver then announces no root (the harness's "the leg has a
# codec for none") is red, so a driver that stops presenting a unit's roots
# reads red, never as one arm fewer. Every arm that runs is required green;
# nothing holds a red arm back (the no-skips ruling, docs/rulings.json). The
# line printed per arm is the harness's.
# `retain` selects only --builder --retain, and `all` adds it to both plain
# arms. Every mode compiles the unit legs once before running its arms.
set -eu -o pipefail
goose=$1 harness=$2 units=$3 work=$4 seed=$5 n=$6
# Direct callers keep the original two plain arms unless they opt in.
mode=${7:-plain}
case "$mode" in
    plain) arms="builder fixed" ;;
    retain) arms="retain" ;;
    all) arms="builder fixed retain" ;;
    *) echo "goose wire-fuzz: unknown mode $mode" >&2; exit 2 ;;
esac
rm -rf "$work" && mkdir -p "$work" || exit 2
# the units each arm has roots for, from the harness's own roster
"$harness" wire-fuzz --list-roots >"$work/roster-plain.txt" || {
	echo "goose wire-fuzz: the harness did not list the plain roster" >&2; exit 2; }
"$harness" wire-fuzz --list-roots --builder >"$work/roster-builder.txt" || {
	echo "goose wire-fuzz: the harness did not list the builder roster" >&2; exit 2; }
"$harness" wire-fuzz --list-roots --builder --retain >"$work/roster-retain.txt" || {
	echo "goose wire-fuzz: the harness did not list the retain roster" >&2; exit 2; }
awk -F'\t' '$3 == "fixed" { print $1 }' "$work/roster-plain.txt" | sort -u >"$work/arm-fixed.txt"
cut -f1 "$work/roster-builder.txt" | sort -u >"$work/arm-builder.txt"
cut -f1 "$work/roster-retain.txt" | sort -u >"$work/arm-retain.txt"
for arm in $arms; do
	if [ ! -s "$work/arm-$arm.txt" ]; then
		echo "goose wire-fuzz: the corpus names no root for the $arm arm" >&2
		exit 2
	fi
done
fail=0
ran=0
# Each leg is a C compile. Cap parallel builds on shared fleet machines;
# callers may set GOOSE_FUZZ_JOBS. The arms run one by one.
jobs=${GOOSE_FUZZ_JOBS:-4}
for d in "$units"/*/; do
	if ls "$d"*Table.goose >/dev/null 2>&1; then basename "$d"; fi
done >"$work/units.txt"
xargs -P "$jobs" -I{} sh -c 'sh make/goose-wirefuzz-build.sh "$1" "$2/{}" "$3/{}" || echo "{}" >>"$3/build-failed.txt"' _ "$goose" "$units" "$work" <"$work/units.txt"
for u in $(cat "$work/units.txt"); do
	if [ ! -x "$work/$u/wirefuzz" ]; then
		echo "goose wire-fuzz: $u: the leg does not build"
		fail=1
		continue
	fi
	unit_arms=
	for arm in $arms; do
		if grep -qxF "$u" "$work/arm-$arm.txt"; then
			unit_arms="$unit_arms $arm"
		fi
	done
	if [ -z "$unit_arms" ]; then
		echo "goose wire-fuzz: $u: the corpus names no root of this unit for the arms of this mode ($arms)"
		continue
	fi
	for arm in $unit_arms; do
        driver_arm=$arm
        flag=
        case "$arm" in
            builder) flag=--builder ;;
            retain) flag="--builder --retain"; driver_arm=builder ;;
        esac
		log="$work/$u.$arm.log"
		rc=0
		"$harness" wire-fuzz $flag --driver "$work/$u/wirefuzz $u $driver_arm" --seed "$seed" --n "$n" \
			--failed "$work/failed-$u-$arm.bin" >"$log" 2>&1 || rc=$?
		ran=$((ran + 1))
		if [ $rc -ne 0 ]; then
			echo "goose wire-fuzz: $u ($arm arm) RED:"
			sed 's/^/  /' "$log"
			fail=1
		else
			echo "goose wire-fuzz: $u ($arm arm): $(grep -E '^wire-fuzz[a-z-]*( retain)?:' "$log" | tail -1)"
		fi
	done
done
if [ $ran -eq 0 ]; then
	echo "goose wire-fuzz REFUSES: no arm ran over $units"
	exit 2
fi
if [ $fail -ne 0 ]; then
	echo "GOOSE WIRE-FUZZ FAILED"
	exit 1
fi
echo "goose wire-fuzz: $ran arms ran, all green"
