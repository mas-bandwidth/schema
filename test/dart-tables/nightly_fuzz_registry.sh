#!/bin/sh
# Emit the registry the nightly fuzzer reads: one prefixed import per generated
# <Base>Wire.dart library and one row per table root, carrying that root's
# load, save, toJson, fromJson and report constructors. The table list is the
# generated corpus itself, so no second list can drift from it.
#
# usage: nightly_fuzz_registry.sh <generated-root> <output.dart> <absolute-root>
set -eu
gen=$1
out=$2
root=$3

{
  i=0
  for f in "$gen"/*/*Wire.dart; do
    [ -e "$f" ] || continue
    echo "import '$root/$f' as u$i;"
    i=$((i + 1))
  done
  echo 'final List<dynamic> rawTables = <dynamic>['
  i=0
  for f in "$gen"/*/*Wire.dart; do
    [ -e "$f" ] || continue
    unit=$(basename "$(dirname "$f")")
    grep -hE '^[A-Za-z0-9_?<>]+ [a-z][A-Za-z0-9_]*WireLoad\(Uint8List data, \[TableWireReport\? report\]\) \{' "$f" |
      sed -E 's/^([A-Za-z0-9_?<>]+) ([a-z][A-Za-z0-9_]*)WireLoad.*/\1 \2/' |
      while read -r typ lc; do
        echo "  ['$unit/$typ', () => u$i.$typ(), (d, r) => u$i.${lc}WireLoad(d, r), (v) => u$i.${lc}WireSave(v), (v) => u$i.${lc}ToJson(v), (s, r) => u$i.${lc}FromJson(s, r), () => u$i.TableWireReport(), () => u$i.TableTextReport()],"
      done
    i=$((i + 1))
  done
  echo '];'
} > "$out"

rows=$(grep -c "^  \['" "$out" || true)
if [ "$rows" -eq 0 ]; then
  echo "nightly fuzz registry: no table roots under $gen" >&2
  exit 1
fi
