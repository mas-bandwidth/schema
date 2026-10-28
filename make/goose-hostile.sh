#!/bin/sh
# make/goose-hostile.sh — the Goose port runs the shared hostile-input corpus
# (docs/HOSTILE-CORPUS.md; make/goose-hostile-corpus.mk says why).
#
#   goose-hostile.sh <goose> <schema compiler> <corpus dir> <work dir> [known findings]
#
# Generates every schema the manifest names for goose, runs
# test/goose-hostile/runner.goose once per unit, and holds each line to the
# manifest: the verdict and the reason, and for an accept the value — the
# runner's re-saved wire must be the input byte for byte, because every accept
# input is the engine's own encode of the value whose digest the manifest pins.
# A case the port cannot read yet (a carrier, or a schema the target refuses)
# is a named absence, counted. A disagreement the known-findings file names is
# carried by name; one it does not name is red, and a named one gone green is
# red too.
set -u
goose=$1 schema=$2 corpus=$3 work=$4
known=${5:-test/goose-hostile/known-findings.tsv}
manifest=$corpus/manifest.tsv
rm -rf "$work" && mkdir -p "$work" || exit 2
abs_corpus=$(cd "$corpus" && pwd)
: >"$work/lines"
for s in $(awk -F'\t' '!/^#/ && NF >= 9 { print $3 }' "$manifest" | LC_ALL=C sort -u); do
	slug=$(echo "$s" | tr '/.' '__')
	src=$s
	[ "$s" = schemas/probe.schema ] && src=test/goose-hostile/probe.schema
	u=$work/$slug
	if ! "$schema" generate --lang goose --out "$u" "$src" >"$work/$slug.gen.log" 2>&1 || ! ls "$u"/*Table.goose >/dev/null 2>&1; then
		awk -F'\t' -v s="$s" '!/^#/ && $3 == s { print $1 " absent target-refuses-schema" }' "$manifest" >>"$work/lines"
		continue
	fi
	# THE CONTROL'S SEAM (make/goose-hostile-corpus.mk): a sed the negative
	# control plants into each generated load runtime, never set otherwise
	if [ -n "${SCHEMA_GOOSE_HOSTILE_SABOTAGE:-}" ]; then
		sed "$SCHEMA_GOOSE_HOSTILE_SABOTAGE" "$u/wire_load.goose" >"$u/wire_load.sabotaged" && mv "$u/wire_load.sabotaged" "$u/wire_load.goose" || exit 2
		grep -q SABOTAGED "$u/wire_load.goose" || { echo "goose hostile corpus: the sabotage patched nothing in $s"; exit 2; }
	fi
	ns=$(sed -n 's/^namespace \(.*\);$/\1/p' "$u"/*Table.goose | head -1)
	{
		for f in "$u"/*.goose; do echo "import .$(basename "$f" .goose);"; done
		echo "fn conformance_unit() -> schema_rt::LoadUnit { return $ns::TableUnit; }"
	} >"$u/conformance_unit.goose"
	cp test/goose-hostile/runner.goose "$u/runner.goose"
	"$goose" run "$u/runner.goose" -- "$abs_corpus" "$s" >>"$work/lines" 2>"$work/$slug.err" || {
		cat "$work/$slug.err"
		echo "goose hostile corpus: the runner failed on $s"
		exit 1
	}
done

fail=0 pass=0 absent=0 carried=0
while IFS='	' read -r name carrier _ _ input _ verdict reason _; do
	case "$name" in '#'* | '') continue ;; esac
	line=$(grep -m1 "^$name " "$work/lines")
	if [ -z "$line" ]; then
		echo "FAIL $name: the runner printed no line"
		fail=1
		continue
	fi
	set -- $line
	got_verdict=$2 got=${3:-}
	if [ "$got_verdict" = absent ]; then
		absent=$((absent + 1))
		continue
	fi
	ok=1
	why=
	if [ "$got_verdict" != "$verdict" ]; then
		ok=0 why="the port says $got_verdict${got:+ $got}, the engine says $verdict${reason:+ $reason}"
	elif [ "$verdict" = refuse ] && [ "$got" != "$reason" ]; then
		ok=0 why="the port names $got, the engine names $reason"
	elif [ "$verdict" = accept ] && [ "$got" != "$(od -An -v -tx1 "$corpus/$input" | tr -d ' \n')" ]; then
		ok=0 why="the port re-saves a different value than the engine's own encode"
	fi
	row=$(awk -F'\t' -v n="$name" '$1 == n { print $2 }' "$known")
	if [ $ok -eq 1 ]; then
		if [ -n "$row" ]; then
			echo "FAIL $name: green, but $known carries it ($row): remove the row"
			fail=1
		else
			pass=$((pass + 1))
		fi
	elif [ -n "$row" ] && [ "$row" = "$why" ]; then
		echo "CARRIED $name: $why ($(awk -F'\t' -v n="$name" '$1 == n { print $3 }' "$known"))"
		carried=$((carried + 1))
	else
		echo "FAIL $name: $why"
		fail=1
	fi
done <"$manifest"
echo "goose hostile corpus: $pass agree with the engine, $carried carried by name, $absent absent (carriers or schemas the port does not read yet)"
if [ $pass -eq 0 ]; then
	echo "goose hostile corpus REFUSES: no case ran"
	exit 2
fi
[ $fail -eq 0 ] || { echo "GOOSE HOSTILE CORPUS FAILED"; exit 1; }
