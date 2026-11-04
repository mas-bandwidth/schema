#!/bin/sh
# THE RUST LEG'S RELEASE STORAGE VERDICT (docs/SPEC.md §5.1): over the answers
# a harness run of the Rust driver wrote under <work>/out/rust, built with
# debug assertions off, every row that sets a count or a length past the
# storage it indexes answers refused, its lawful twin answers wrote, and a row
# the write contract holds only under debug_assert! answers wrote, which is
# the witness that the driver really is the release configuration.
#
# usage: release-storage-verdict.sh <work>
set -u
out="$1/out/rust"
bad=
want() {
	got=$(cat "$out/$2" 2>/dev/null)
	[ "$got" = "$1" ] || bad="$bad $2=${got:-missing}"
}
for row in write-refusal/wref_count write-refusal-fixed/fref_count \
		write-refusal-fixed/fref_length write-refusal-message/mref_length; do
	want refused "$row"
	want wrote "${row}_ok"
done
for row in write-refusal/wref_range_high write-refusal-fixed/fref_range_high \
		write-refusal-message/mref_range_high; do
	want wrote "$row"
done
if [ -n "$bad" ]; then
	echo "RUST RELEASE STORAGE GATE FAILED:$bad"
	exit 1
fi
echo 'Rust release storage gate: wref_count, fref_count, fref_length and mref_length refused with debug assertions off; their twins wrote; the debug-only range rows wrote'
