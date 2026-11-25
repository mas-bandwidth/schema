#!/bin/sh
# The parity table (internal/ci TestTheParityTableIsCovered holds it): every shared test
# family against every leg (language). A cell is covered when the repository
# registers the leg's runner for that family; a cell with no runner is red
# unless that config holds it as a gap with the reason it does not apply.
#
#   family            the leg is covered when
#   conformance       test/conformance/<leg>/driver exists (test/conformance/README.md:
#                     registering a port is one driver at <lang>/driver)
#   tablegen          internal/codegen/<leg>table has Go tests
#   wire-fuzz-plain   make/wire-fuzz-release.mk lists the leg and
#   wire-fuzz-retain  (both arms: no owed list excuses an arm)
#   write-refusal     the leg's conformance driver answers the write-refusal battery
#
#   sh test/testguard/parity.sh rows | columns | pairs
set -eu
legs() { ls -d internal/codegen/*table | sed 's|internal/codegen/||; s|table$||' | sort; }
case "${1:-}" in
rows) printf '%s\n' conformance tablegen wire-fuzz-plain wire-fuzz-retain write-refusal ;;
columns) legs ;;
pairs)
	wfr=$(sed -n 's/^WIRE_FUZZ_RELEASE_LEGS *:= *//p' make/wire-fuzz-release.mk)
	for leg in $(legs); do
		if [ -e "test/conformance/$leg/driver" ]; then echo "conformance	$leg"; fi
		if ls "internal/codegen/${leg}table/"*_test.go > /dev/null 2>&1; then echo "tablegen	$leg"; fi
		case " $wfr " in *" $leg "*)
			for arm in plain retain; do
				echo "wire-fuzz-$arm	$leg"
			done ;;
		esac
		if [ -d "test/conformance/$leg" ] && grep -rlq 'write-refusal\|WriteRefusal' "test/conformance/$leg"; then echo "write-refusal	$leg"; fi
	done
	;;
*) echo "usage: sh test/testguard/parity.sh rows|columns|pairs" >&2; exit 2 ;;
esac
