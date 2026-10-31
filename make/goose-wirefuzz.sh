#!/bin/sh
# make/goose-wirefuzz.sh — THE WIRE-FUZZ DIFFERENTIAL for the Goose leg
# (docs/PORTING.md I15; make/goose-fuzz.mk says why it is shaped this way).
#
#   goose-wirefuzz.sh <goose> <harness> <units dir> <work dir> <seed> <n> [plain|retain|all]
#
# For every unit the goose target generated (a directory with a table
# module), builds the leg for that unit and runs the harness's two arms over
# it: --builder over its variable-class file roots, the region arm over its
# value-only roots. An arm with no root of this unit is said and skipped. Every
# arm that ran is required green; the line printed per arm is the harness's.
# `retain` selects only --builder --retain, and `all` adds it to both plain
# arms. Every mode compiles the unit legs once before running its arms.
set -u
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
known_file=${SCHEMA_GOOSE_WIREFUZZ_KNOWN:-test/conformance/goose/wirefuzz-known.tsv}
fail=0
ran=0
known_red=0
# Each leg is a C compile. Cap parallel builds on shared fleet machines;
# callers may set GOOSE_FUZZ_JOBS. The arms run one by one.
jobs=${GOOSE_FUZZ_JOBS:-4}
for d in "$units"/*/; do
	ls "$d"*Table.goose >/dev/null 2>&1 && echo "$(basename "$d")"
done >"$work/units.txt"
xargs -P "$jobs" -I{} sh -c 'sh make/goose-wirefuzz-build.sh "$1" "$2/{}" "$3/{}" || echo "{}" >>"$3/build-failed.txt"' _ "$goose" "$units" "$work" <"$work/units.txt"
for u in $(cat "$work/units.txt"); do
	if [ ! -x "$work/$u/wirefuzz" ]; then
		echo "goose wire-fuzz: $u: the leg does not build"
		fail=1
		continue
	fi
	for arm in $arms; do
        driver_arm=$arm
        flag=
        case "$arm" in
            builder) flag=--builder ;;
            retain) flag="--builder --retain"; driver_arm=builder ;;
        esac
		log="$work/$u.$arm.log"
		"$harness" wire-fuzz $flag --driver "$work/$u/wirefuzz $u $driver_arm" --seed "$seed" --n "$n" \
			--failed "$work/failed-$u-$arm.bin" >"$log" 2>&1
		rc=$?
		if [ $rc -ne 0 ] && grep -q 'the leg has a codec for none' "$log"; then
			continue
		fi
		ran=$((ran + 1))
		known=$(awk -F'\t' -v u="$u" -v a="$arm" '$1 == u && $2 == a { print $3 }' "$known_file")
		if [ -n "$known" ]; then
			if [ $rc -ne 0 ] && grep -qF "harness: FAILED after" "$log" && grep -qF "$known" "$log"; then
				echo "goose wire-fuzz: $u ($arm arm): KNOWN, owed: $known"
				known_red=$((known_red + 1))
			elif [ $rc -eq 0 ]; then
				echo "goose wire-fuzz: $u ($arm arm) is GREEN but $known_file holds it red: remove the row"
				fail=1
			else
				echo "goose wire-fuzz: $u ($arm arm) RED on something other than its known finding:"
				sed 's/^/  /' "$log"
				fail=1
			fi
			continue
		fi
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
echo "goose wire-fuzz: $ran arms ran, $((ran - known_red)) green, $known_red held red by name in $known_file"
