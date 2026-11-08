#!/bin/sh
# make/goose-hostile.sh — the Goose port runs the shared hostile-input corpus
# (docs/HOSTILE-CORPUS.md; make/goose-hostile-corpus.mk says why).
#
#   goose-hostile.sh <goose> <schema compiler> <corpus dir> <work dir> [known findings]
#
# Generates every schema the manifest names for goose, runs
# test/goose-hostile/runner.goose once per unit, and holds each line to the
# manifest: the verdict and the reason, and for an accept the value — the
# runner's re-saved wire must be the input byte for byte where the input is the
# engine's own encode of the value whose digest the manifest pins, and where it
# is not (a wire the engine reads into another value, a text list past its
# total), the port's canonical text must carry the manifest's digest and its
# re-saved wire must be the engine's own encode of that text.
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
	# one schema file of a locked unit (tables/examples/Guarded.schema) is
	# generated as its whole unit, as the C and C++ runners bind it: the
	# lock holds the unit, never one file of it
	[ -f "$s" ] && [ -f "$(dirname "$s")/schema.lock" ] && src=$(dirname "$s")
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
		echo "fn conformance_text() -> schema_rt::JsonUnit { return $ns::TextUnit; }"
		# THE FIXED FORM (docs/SPEC-TABLES.md §3.4): one arm per fixed root the
		# unit emits a <Root>FixedLoad for. It answers -1 for a root that has
		# none, 0 for a read (the re-saved file and the first record's canonical
		# text appended), and otherwise the load's refusal code; a clamp (oor)
		# is refused as the strict read refuses it.
		echo "fn conformance_fixed(root: const u8[:], data: const u8[:], saved: u8[>..]&, text: u8[>..]&) -> i64 {"
		for r in $(sed -n 's/^fn \([A-Za-z0-9_]*\)FixedLoad(.*/\1/p' "$u"/*Table.goose); do
			echo "    if root == \"$r\" {"
			echo "        var values: $ns::$r[256] = [$ns::$r { .. }; 256];"
			echo "        var rep = schema_rt::FixedReport { .. };"
			echo "        let n = $ns::${r}FixedLoad(values[0..256], data, rep);"
			echo "        if n < 0 { return if rep.reason > 0 { rep.reason } else { schema_rt::FixedRefuseTruncated }; }"
			echo "        if rep.damaged || rep.oor != 0 { return schema_rt::FixedRefuseOutOfRange; }"
			echo "        if $ns::${r}FixedSave(values[0..n], saved) < 0 { return schema_rt::FixedRefuseInvalidValue; }"
			if grep -q "^fn ${r}ToJson(" "$u"/*.goose; then
				echo "        if n == 1 && $ns::${r}ToJson(values[0], text) < 0 { text.resize(0); }"
			fi
			echo "        return 0;"
			echo "    }"
		done
		echo "    return -1;"
		echo "}"
	} >"$u/conformance_unit.goose"
	cp test/goose-hostile/runner.goose "$u/runner.goose"
	"$goose" run "$u/runner.goose" -- "$abs_corpus" "$s" >>"$work/lines" 2>"$work/$slug.err" || {
		cat "$work/$slug.err"
		echo "goose hostile corpus: the runner failed on $s"
		exit 1
	}
done

fail=0 pass=0 absent=0 carried=0
while IFS='	' read -r name carrier schema_path table input _ verdict reason digest; do
	case "$name" in '#'* | '') continue ;; esac
	line=$(grep -m1 "^$name " "$work/lines")
	if [ -z "$line" ]; then
		echo "FAIL $name: the runner printed no line"
		fail=1
		continue
	fi
	set -- $line
	got_verdict=$2 got=${3:-} text=${4:--}
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
		if [ "$text" = - ] || [ "$(printf %s "$text" | perl -ne 'print pack("H*", $_)' | sha256sum | cut -d' ' -f1)" != "$digest" ]; then
			ok=0 why="the port re-saves a different value than the engine's own encode"
		else
			# the engine's own encode of the port's value
			tree=$work/encode/$name
			mkdir -p "$tree"
			printf %s "$text" | perl -ne 'print pack("H*", $_)' >"$tree/$table.json"
			src=$schema_path
			[ "$schema_path" = schemas/probe.schema ] && src=test/goose-hostile/probe.schema
			[ -f "$schema_path" ] && [ -f "$(dirname "$schema_path")/schema.lock" ] && src=$(dirname "$schema_path")
			if ! "$schema" pack --root "$table" --out "$tree/encode.bin" "$tree" "$src" >/dev/null 2>&1 ||
				[ "$got" != "$(od -An -v -tx1 "$tree/encode.bin" | tr -d ' \n')" ]; then
				ok=0 why="the port re-saves a different wire than the engine's own encode of its value"
			fi
		fi
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
