#!/bin/sh
# The parity table for nova-testguard (test/testguard.tsv): every shared test
# family against every leg (language). A cell is covered when the repository
# registers the leg's runner for that family; a cell with no runner is red
# unless test/testguard.tsv holds it as a gap with the reason it does not apply.
#
#   family            the leg is covered when
#   conformance       test/conformance/<leg> exists and `make registry` lists its
#                     conformance build (cpp is the reference leg the harness builds)
#   tablegen          internal/codegen/<leg>table has Go tests
#   wire-fuzz-plain   make/wire-fuzz-release.mk lists the leg and
#   wire-fuzz-retain  test/conformance/wire-fuzz-owed.txt owes no such arm
#   write-refusal     the leg's conformance driver answers the write-refusal battery
#
#   sh test/testguard/parity.sh rows | columns | pairs
set -eu
legs() { ls -d internal/codegen/*table | sed 's|internal/codegen/||; s|table$||' | sort; }
case "${1:-}" in
rows) printf '%s\n' conformance tablegen wire-fuzz-plain wire-fuzz-retain write-refusal ;;
columns) legs ;;
pairs)
	registry=$(make -s registry | sed -n 's/^conformance: //p')
	wfr=$(sed -n 's/^WIRE_FUZZ_RELEASE_LEGS *:= *//p' make/wire-fuzz-release.mk)
	for leg in $(legs); do
		if [ -d "test/conformance/$leg" ]; then
			case " $registry " in
			*"conformance-$leg"*) echo "conformance	$leg" ;;
			*) if [ "$leg" = cpp ]; then echo "conformance	$leg"; fi ;;
			esac
		fi
		if ls "internal/codegen/${leg}table/"*_test.go > /dev/null 2>&1; then echo "tablegen	$leg"; fi
		case " $wfr " in *" $leg "*)
			for arm in plain retain; do
				grep -Eq "^$leg +$arm( |$)" test/conformance/wire-fuzz-owed.txt || echo "wire-fuzz-$arm	$leg"
			done ;;
		esac
		if [ -d "test/conformance/$leg" ] && grep -rlq 'write-refusal\|WriteRefusal' "test/conformance/$leg"; then echo "write-refusal	$leg"; fi
	done
	;;
*) echo "usage: sh test/testguard/parity.sh rows|columns|pairs" >&2; exit 2 ;;
esac
