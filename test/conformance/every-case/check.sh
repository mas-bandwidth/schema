#!/bin/sh
# EVERY CASE IS ANSWERED (test/conformance/README.md, exit codes): a driver
# that stops at one case it cannot run leaves every case after it unanswered,
# and the whole surface scores nothing. A test that never answers is a test that
# does nothing, so this holds one leg to the rule over the real corpus.
#
# It runs the harness for that one leg, then, for each surface named below that
# the leg's `list` carries, requires
#   - the surface's driver run to have exited 0 (the harness prints
#     `<leg> / <surface>: the driver exited N` otherwise), and
#   - an answer for EVERY case line of that surface in the leg's derived
#     manifest: <case>, or <case>.absent (empty for no codec, holding a reason
#     for a refusal).
# Each case without an answer is named. Whether an answer is RIGHT is the
# matrix's job (make conformance), not this one's: this gate is green when the
# driver answered, and red, by case, when it did not.
#
# usage: check.sh <leg> <work dir> [surface ...]   (from the repository root,
#        with build/conformance-harness and the leg's driver built)
set -u
leg=$1
work=$2
shift 2
surfaces=${*:-write-refusal write-refusal-fixed write-refusal-message write-refusal-json json-hostile}
label="$leg every case answered"
rm -rf "$work" && mkdir -p "$work" || exit 1
log="$work.log"

./build/conformance-harness run --only "$leg" --work "$work" > "$log" 2>&1
# the run's own verdict is the matrix's; this gate reads what was answered

manifest="$work/manifest-$leg.txt"
[ -f "$manifest" ] || manifest="$work/manifest.txt"
[ -f "$manifest" ] || { echo "$label: the harness wrote no derived manifest"; tail -20 "$log"; exit 1; }
listed=$(test/conformance/"$leg"/driver "$manifest" list 2>/dev/null)

fail=0
checked=0
for surface in $surfaces; do
	echo "$listed" | grep -qx "$surface" || continue
	checked=$((checked + 1))
	if grep -q "^  $leg / $surface: the driver exited" "$log"; then
		echo "$label: $surface: the driver exited non-zero"
		grep -A3 "^  $leg / $surface: the driver exited" "$log" | sed -n '2,4p'
		fail=1
	fi
	out="$work/out/$leg/$surface"
	missing=$(awk -v s="$surface" '$1 == s { print $2 }' "$manifest" | while read -r name; do
		[ -e "$out/$name" ] || [ -e "$out/$name.absent" ] || echo "$name"
	done)
	if [ -n "$missing" ]; then
		for name in $missing; do echo "$label: $surface $name: no answer"; done
		fail=1
	fi
done

if [ "$fail" -ne 0 ]; then
	echo "$label: FAILED"
	exit 1
fi
[ "$checked" -gt 0 ] || { echo "$label: the leg lists none of: $surfaces"; exit 1; }
echo "$label: every case of $checked surfaces answered, each driver run exited 0"
