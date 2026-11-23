#!/bin/sh
# A CONFORMANCE DRIVER'S CASE OUTCOMES (test/conformance/README.md, "A CASE may
# be absent too"; review F-086 to F-090 and F-174).
#
# Runs a real driver over a fixture manifest, one surface at a time, and holds
# every case to its line in an expected file: an absent codec is an EMPTY
# <case>.absent, a present codec's refusal is that surface's refusal answer,
# and a driver error leaves no answer, names the case on stderr as
# `<surface> <case>: driver error: ...`, lets the cases after it run, and turns
# the surface's exit status non-zero. Nothing is matched by a case's name
# inside a driver; the names here only say which row is which.
#
# expected file: `<surface> <case> <answer>` lines, where answer is
#   bytes   <case> written, and no <case>.absent
#   reason  the present codec refused: <case>.absent holding a reason, no <case>
#   absent  no codec for the case: an EMPTY <case>.absent, no <case>
#   error   a driver error: neither file, and the case named on stderr
#   =<word> <case> holds exactly <word> and a newline, and no <case>.absent
#
# usage: check.sh <driver> <manifest> <expected> <work dir>   (from the repository root)
set -u
driver=$1
manifest=$2
expected=$3
work=$4
label="$(basename "$(dirname "$driver")") case outcomes"
rm -rf "$work" && mkdir -p "$work" || exit 1
fail=0

say() {
	echo "$label: $*"
	fail=1
}

surfaces=$(grep -v '^#' "$expected" | awk 'NF { print $1 }' | uniq)
for surface in $surfaces; do
	out="$work/$surface"
	mkdir -p "$out"
	"$driver" "$manifest" "$surface" "$out" > "$work/$surface.stdout" 2> "$work/$surface.stderr"
	status=$?
	errors=$(awk -v s="$surface" '$1 == s && $3 == "error" { print $2 }' "$expected")
	if [ -n "$errors" ] && [ "$status" -eq 0 ]; then
		say "$surface exited 0 although its driver errors ($errors) must turn it red"
	fi
	if [ -z "$errors" ] && [ "$status" -ne 0 ]; then
		say "$surface exited $status with no driver error expected"; cat "$work/$surface.stderr"
	fi
	# every driver error on stderr is one the fixture expects, by case
	named=$(sed -n "s|^$surface \([^ :]*\): driver error:.*|\1|p" "$work/$surface.stderr" | sort | tr '\n' ' ')
	want=$(echo $errors | tr ' ' '\n' | sed '/^$/d' | sort | tr '\n' ' ')
	if [ "$named" != "$want" ]; then
		say "$surface named driver errors [$named], want [$want]"; cat "$work/$surface.stderr"
	fi
	awk -v s="$surface" '$1 == s { print $2, $3 }' "$expected" | while read -r name answer; do
		f="$out/$name"
		case "$answer" in
		bytes)
			[ -s "$f" ] && [ ! -e "$f.absent" ] || echo "FAIL $surface $name: want bytes" ;;
		reason)
			[ ! -e "$f" ] && [ -s "$f.absent" ] || echo "FAIL $surface $name: want a refusal (<case>.absent holding its reason)" ;;
		absent)
			[ ! -e "$f" ] && [ -e "$f.absent" ] && [ ! -s "$f.absent" ] || echo "FAIL $surface $name: want an EMPTY <case>.absent (no codec)" ;;
		error)
			[ ! -e "$f" ] && [ ! -e "$f.absent" ] || echo "FAIL $surface $name: a driver error left an answer" ;;
		=*)
			[ ! -e "$f.absent" ] && [ "$(cat "$f" 2>/dev/null)" = "${answer#=}" ] || echo "FAIL $surface $name: want ${answer#=}, got '$(cat "$f" "$f.absent" 2>/dev/null)'" ;;
		*)
			echo "FAIL $surface $name: $expected names no answer $answer" ;;
		esac
	done > "$work/$surface.fail"
	if [ -s "$work/$surface.fail" ]; then
		cat "$work/$surface.fail"
		fail=1
	fi
done

if [ "$fail" -ne 0 ]; then
	echo "$label: FAILED"
	exit 1
fi
echo "$label: absent, refused and driver-error cases each answered as the driver contract says ($(echo $surfaces | wc -w | tr -d ' ') surfaces)"
