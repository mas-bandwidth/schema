#!/bin/sh
# THE ELIXIR DRIVER'S CASE OUTCOMES (make/elixir.mk, elixir-case-outcomes).
#
# Runs the real driver (test/conformance/elixir/driver) over manifest.txt in
# this directory, one surface at a time, and holds every case to its line in
# expected.txt: an absent codec is an EMPTY <case>.absent, a present codec's
# refusal is that surface's refusal answer, and a driver error leaves no
# answer, names the case on stderr, lets the cases after it run, and turns the
# surface's exit status non-zero. Nothing is matched by a case's name inside
# the driver; the names here only say which row is which.
#
# usage: check.sh <fixture dir> <work dir>   (from the repository root)
set -u
fixture=$1
work=$2
driver=test/conformance/elixir/driver
rm -rf "$work" && mkdir -p "$work" || exit 1
fail=0

say() {
	echo "elixir case outcomes: $*"
	fail=1
}

surfaces=$(grep -v '^#' "$fixture/expected.txt" | awk 'NF { print $1 }' | uniq)
for surface in $surfaces; do
	out="$work/$surface"
	mkdir -p "$out"
	"$driver" "$fixture/manifest.txt" "$surface" "$out" > "$work/$surface.stdout" 2> "$work/$surface.stderr"
	status=$?
	errors=$(awk -v s="$surface" '$1 == s && $3 == "error" { print $2 }' "$fixture/expected.txt")
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
	awk -v s="$surface" '$1 == s { print $2, $3 }' "$fixture/expected.txt" | while read -r name answer; do
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
			echo "FAIL $surface $name: expected.txt names no answer $answer" ;;
		esac
	done > "$work/$surface.fail"
	if [ -s "$work/$surface.fail" ]; then
		cat "$work/$surface.fail"
		fail=1
	fi
done

if [ "$fail" -ne 0 ]; then
	echo "elixir case outcomes: FAILED"
	exit 1
fi
echo "elixir case outcomes: absent, refused and driver-error cases each answered as the driver contract says ($(echo $surfaces | wc -w | tr -d ' ') surfaces)"
