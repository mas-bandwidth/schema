#!/bin/sh
# make/dart-analyze-tree.sh — the scratch tree `make schema-dart-nightly-fuzz-analyze`
# hands `dart analyze` (make/schema-dart-nightly-fuzz.mk):
#
#   dart-analyze-tree.sh <gen dir> <analyze dir>
#
# For each generated unit <gen dir>/<unit>, its <Base>Wire.dart reader and
# every library the reader imports by a relative path, and theirs in turn,
# are copied to <analyze dir>/<unit>. A reader is not self-contained: its
# enums are the packet library's (Dart 3 extension types, declared once), so
# it imports them from the unit's packet file (`import 'VarEvo1.dart' show
# Tier;`), which imports Schema.dart. A tree of the readers alone does not
# resolve; the whole generated unit would pull in libraries no reader imports
# (tables/scalars' Fixed library imports an Int128.dart the packet emitter
# does not write). A library a reader imports that the corpus did not
# generate fails here, naming it. TestTheDartAnalyzeTreeHoldsWhatTheReadersImport
# (internal/ci) holds this script to it.
set -eu
gen=$1
out=$2
found=0
for f in "$gen"/*/*Wire.dart; do
	if [ ! -f "$f" ]; then
		echo "dart-analyze-tree: no <Base>Wire.dart reader under $gen" >&2
		exit 1
	fi
	found=$((found + 1))
	src=$(dirname "$f")
	dst=$out/$(basename "$src")
	mkdir -p "$dst"
	todo=$(basename "$f")
	while :; do
		# the names still to copy, split on blanks; none left ends the unit
		set -- $todo
		if [ $# -eq 0 ]; then
			break
		fi
		lib=$1
		shift
		todo=$*
		if [ -e "$dst/$lib" ]; then
			continue
		fi
		if [ ! -f "$src/$lib" ]; then
			echo "dart-analyze-tree: $f imports $lib (from $src), which the corpus did not generate" >&2
			exit 1
		fi
		cp "$src/$lib" "$dst/$lib"
		# the relative imports: import 'X.dart' ... (a dart: or package:
		# import has a colon and is not a file of the unit)
		imports=$(sed -n "s/^import '\([^:']*\)'.*/\1/p" "$dst/$lib")
		todo="$todo $imports"
	done
done
echo "dart-analyze-tree: $found readers and the libraries they import, in $out"
