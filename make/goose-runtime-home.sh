#!/bin/sh
# make/goose-runtime-home.sh — THE RUNTIME HOME gate for Goose (docs/PORTING.md
# J2; make/goose-gates.mk, tables-goose-runtime-home, says why).
#
#   goose-runtime-home.sh <schema compiler> <workdir>
#
# Generates tables/examples, then the same unit with an Aaa.schema that sorts
# before every file in it, and requires (1) every module that is not a schema
# file's own <Base>.goose or <Base>Table.goose — the runtime — to be present
# in both trees under the same name with the same bytes, and (2) each original
# file's <Base>Table.goose to be byte-identical. The unit's own module,
# <Package>Unit.goose (internal/codegen/goosetable UnitModule), holds the data
# that spans every file, so its bytes grow with the unit; it is held to its
# HOME instead: present under the package's name in both trees whenever either
# tree has one, and the only module that declares the unit registry
# (`let TableUnit:`). Each module that moved is named; one is red.
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
	b=${b%Table}
	[ -f "$1/$b.schema" ]
}

pkg=$(sed -n 's/^package \([A-Za-z0-9_]*\).*/\1/p' tables/examples/*.schema | head -1)
unit=$(printf '%s' "$pkg" | cut -c1 | tr '[:lower:]' '[:upper:]')$(printf '%s' "$pkg" | cut -c2-)Unit.goose

bad=0
runtime=0
# the runtime, from both sides, so a module that appears or vanishes is named
for f in $( (cd "$w/base" && ls *.goose; cd "../added" && ls *.goose) | LC_ALL=C sort -u); do
	if own tables/examples "$f" || own "$w/src" "$f"; then
		continue
	fi
	[ "$f" = "$unit" ] && continue
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
# the unit module: at the package's home in both trees, and the only home of
# the registry
units=0
if [ -f "$w/base/$unit" ] || [ -f "$w/added/$unit" ]; then
	units=1
	if [ ! -f "$w/base/$unit" ] || [ ! -f "$w/added/$unit" ]; then
		echo "unit module $unit moved: in $( [ -f "$w/base/$unit" ] && echo the base tree || echo the tree with Aaa.schema ) only"
		bad=1
	fi
fi
for tree in base added; do
	for f in $(cd "$w/$tree" && grep -l '^let TableUnit:' *.goose 2>/dev/null); do
		[ "$f" = "$unit" ] || { echo "unit registry in $f in the $tree tree: its home is $unit"; bad=1; }
	done
done
tables=0
for s in tables/examples/*.schema; do
	t=$(basename "$s" .schema)Table.goose
	[ -f "$w/base/$t" ] || continue
	tables=$((tables + 1))
	cmp -s "$w/base/$t" "$w/added/$t" || { echo "table module $t moved: its bytes changed when Aaa.schema joined the unit"; bad=1; }
done
if [ $bad -ne 0 ]; then
	echo "GOOSE RUNTIME HOME GATE FAILED: a module moved when an earlier-sorting file joined the unit"
	exit 1
fi
echo "goose runtime home gate: $runtime runtime modules and $tables table modules unmoved, and $units unit module at $unit, when an earlier-sorting file joins the unit"
