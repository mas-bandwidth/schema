#!/bin/sh
# make/goose-runtime-home.sh — THE RUNTIME HOME gate for Goose (docs/PORTING.md
# J2; make/goose-gates.mk, tables-goose-runtime-home, says why).
#
#   goose-runtime-home.sh <schema compiler> <workdir>
#
# Generates tables/examples, then the same unit with an Aaa.schema that sorts
# before every file in it, and requires (1) every module that is not a schema
# file's own (<Base>.goose, <Base>Table.goose, <Base>Cook.goose,
# <Base>Block.goose) — the runtime — to be present in both trees under the same
# name with the same bytes, and (2) each original file's <Base>Table.goose,
# <Base>Cook.goose and <Base>Block.goose to be byte-identical. The unit's package modules,
# <package>_<surface>.goose (internal/codegen/goosetable PackageModule), hold the
# data that spans every file, so their bytes grow with the unit; each is held
# to its HOME instead: present under the package's name in both trees whenever
# either tree has one, and the only module that declares its unit-wide data
# (TableUnit, CookRecords, BlockRecords, MessageAnnouncement). Each module that
# moved is named; one is red.
set -u

if [ $# -ne 2 ]; then
	echo "usage: goose-runtime-home.sh <schema compiler> <workdir>" >&2
	exit 2
fi
schema=$1
w=$2
rm -rf "$w" && mkdir -p "$w/src" || exit 2
cp tables/examples/*.schema "$w/src/" || exit 2
printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' >"$w/src/Aaa.schema"
"$schema" generate --lang goose --out "$w/base" tables/examples >/dev/null 2>"$w/base.log" || { cat "$w/base.log"; exit 2; }
"$schema" generate --lang goose --out "$w/added" "$w/src" >/dev/null 2>"$w/added.log" || { cat "$w/added.log"; exit 2; }

own() { # own <dir> <file>: the file is a schema file's own module
	b=${2%.goose}
	for s in Table Cook Block; do
		[ "${b%"$s"}" != "$b" ] && [ -f "$1/${b%"$s"}.schema" ] && return 0
	done
	[ -f "$1/$b.schema" ]
}

pkg=$(sed -n 's/^package \([A-Za-z0-9_]*\).*/\1/p' tables/examples/*.schema | head -1)
P=${pkg}_
# the package modules (internal/codegen/goosetable PackageModule), each with the
# declaration that is unit-wide data and may live nowhere else
homes="unit:TableUnit cook:CookRecords block:BlockRecords message:MessageAnnouncement"
ishome() {
	for h in $homes; do [ "$1" = "$P${h%%:*}.goose" ] && return 0; done
	return 1
}

bad=0
runtime=0
# the runtime, from both sides, so a module that appears or vanishes is named
for f in $( (cd "$w/base" && ls *.goose; cd "../added" && ls *.goose) | LC_ALL=C sort -u); do
	if own tables/examples "$f" || own "$w/src" "$f"; then
		continue
	fi
	ishome "$f" && continue
	runtime=$((runtime + 1))
	if [ ! -f "$w/base/$f" ] || [ ! -f "$w/added/$f" ]; then
		echo "runtime module $f moved: in $( [ -f "$w/base/$f" ] && echo the base tree || echo the tree with Aaa.schema ) only"
		bad=1
	elif ! cmp -s "$w/base/$f" "$w/added/$f"; then
		echo "runtime module $f moved: its bytes changed when Aaa.schema joined the unit"
		bad=1
	fi
done
if [ $runtime -eq 0 ]; then
	echo "goose runtime home gate REFUSES: no runtime module in either tree"
	exit 2
fi
# each package module: at the package's home in both trees, and the only home
# of its unit-wide declaration
units=0
for h in $homes; do
	m=$P${h%%:*}.goose
	decl=${h#*:}
	if [ -f "$w/base/$m" ] || [ -f "$w/added/$m" ]; then
		units=$((units + 1))
		if [ ! -f "$w/base/$m" ] || [ ! -f "$w/added/$m" ]; then
			echo "package module $m moved: in $( [ -f "$w/base/$m" ] && echo the base tree || echo the tree with Aaa.schema ) only"
			bad=1
		fi
	fi
	for tree in base added; do
		for f in $(cd "$w/$tree" && grep -l "^let $decl:" *.goose 2>/dev/null); do
			[ "$f" = "$m" ] || { echo "unit-wide $decl in $f in the $tree tree: its home is $m"; bad=1; }
		done
	done
done
tables=0
for s in tables/examples/*.schema; do
	for sfx in Table Cook Block; do
		t=$(basename "$s" .schema)$sfx.goose
		[ -f "$w/base/$t" ] || continue
		tables=$((tables + 1))
		[ -f "$w/added/$t" ] || { echo "$(echo $sfx | tr '[:upper:]' '[:lower:]') module $t moved: in the base tree only"; bad=1; continue; }
		cmp -s "$w/base/$t" "$w/added/$t" || { echo "$(echo $sfx | tr '[:upper:]' '[:lower:]') module $t moved: its bytes changed when Aaa.schema joined the unit"; bad=1; }
	done
done
if [ $bad -ne 0 ]; then
	echo "GOOSE RUNTIME HOME GATE FAILED: a module moved when an earlier-sorting file joined the unit"
	exit 1
fi
echo "goose runtime home gate: $runtime runtime modules and $tables file modules unmoved, and $units package modules at ${P}<surface>.goose, when an earlier-sorting file joins the unit"
