#!/bin/sh
# Mutate real facade code in a disposable copy, never the driver or answers.
set -eu
surface=${1:?expected render, upgrade or taskhash}
stage=$(mktemp -d build/cards-luau-control-XXXXXX)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
cp tables/cards/luau/*.luau "$stage/"
python3 - "$stage/cards.luau" "$surface" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
plants = {
 'render': ('key .. ": " .. value .. "\\n"', 'key .. "=" .. value .. "\\n"'),
 'upgrade': ('codec.SaveRetain(root, retain, report)', 'codec.Save(root)'),
 'taskhash': ('local function frame(text) return le64(#text) .. text end', 'local function frame(text) return text end'),
}
a, b = plants[sys.argv[2]]
s = path.read_text()
if s.count(a) != 1: raise SystemExit('sabotage anchor must occur exactly once')
path.write_text(s.replace(a, b))
PY
log="build/cards-luau-$surface-negative.log"
if CARDS_LUAU_FACADE="$stage" ./build/conformance-harness cards \
  --work "$stage/results" --corpus tables/cards/conformance \
  --driver tables/cards/luau/driver \
  --require load,save,render,parse,bounds,upgrade,edit,taskhash,view > "$log" 2>&1; then
  echo "NEGATIVE CONTROL FAILED: $surface code sabotage left the gate green"; exit 1
fi
python3 - "$log" "$surface" <<'PY'
import pathlib, sys
lines = pathlib.Path(sys.argv[1]).read_text().splitlines()
want = sys.argv[2]
counts = dict(load='4/4',save='4/4',render='4/4',parse='2/2',bounds='12/12',upgrade='5/5',edit='8/8',taskhash='3/3',view='1/1')
for surface, count in counts.items():
    prefix = 'cards ' + surface + (' FAIL' if surface == want else ' pass ' + count)
    if sum(line.startswith(prefix) for line in lines) != 1:
        raise SystemExit('NEGATIVE CONTROL FAILED: expected ' + prefix + '\n' + '\n'.join(lines))
print('cards Luau negative control: ' + want + ' source sabotage is red; all eight other surfaces stay green')
PY
