#!/bin/sh
# THE LOBSTER BENCH LEGS' NEGATIVE CONTROL (make bench-lobster-gate-negative-control,
# make/lobster-bench.mk). Every rule the two legs and the shape gate's Lobster
# lines add is planted against once, and each plant must be refused by name:
#
#   packet-golden   bench_mixed.bin with one byte flipped: the packet leg's
#                   gate refuses "variant 0 is not the wire golden" and exits 1
#   table-golden    bench_table.bin with one byte flipped: the tables leg's
#                   gate refuses the same way
#   warning         the packet runner with a compiler warning planted:
#                   route.sh refuses the untagged line and exits 1, although
#                   the toolchain itself exits 0
#   timing          a .lobster file reading seconds_elapsed() outside the
#                   runner directories: the shape gate refuses it under TIMING
#   names           a .lobster file under bench/lobster/ naming a corpus type
#                   with no ledger entry: the shape gate refuses it under NAMES
#
# and the clean baselines (both legs' gates, the shape gate over the same
# copied tree with nothing planted) must pass, so a refusal is the plant's and
# not the tree's. Run from the repository root; scratch lives in
# build/bench-lobster-control and the tree is never edited.
set -u

ROOT="$(pwd -P)"
W=build/bench-lobster-control
rm -rf "$W"
mkdir -p "$W/wire"
fail=0

expect() { # expect <case> <want-status:0|1> <want-text|-> <log>
    case_name="$1"; want="$2"; text="$3"; log="$4"; got="$5"
    if [ "$want" = 0 ] && [ "$got" != 0 ]; then
        echo "CONTROL FAILED: $case_name: the clean baseline exited $got"; sed 's/^/    /' "$log"; fail=1; return
    fi
    if [ "$want" = 1 ] && [ "$got" = 0 ]; then
        echo "CONTROL FAILED: $case_name: the plant was not refused (exit 0)"; sed 's/^/    /' "$log"; fail=1; return
    fi
    if [ "$text" != "-" ] && ! grep -qF -- "$text" "$log"; then
        echo "CONTROL FAILED: $case_name: the refusal does not name \"$text\""; sed 's/^/    /' "$log"; fail=1; return
    fi
    echo "control $case_name: ok"
}

flip() { # flip <src> <dst>: copy with the first byte inverted
    head -c 1 "$1" | od -An -tu1 | tr -d ' \n' > "$W/byte"
    b=$(( 255 - $(cat "$W/byte") ))
    printf %b "\\$(printf %03o "$b")" > "$2"
    tail -c +2 "$1" >> "$2"
}

# baselines
st=0; bench/lobster/run --gate > "$W/base-packet.log" 2>&1 || st=$?
expect packet-baseline 0 "OK (gate only" "$W/base-packet.log" "$st"
st=0; bench/tables/lobster/leg run --gate > "$W/base-table.log" 2>&1 || st=$?
expect table-baseline 0 "OK (gate only" "$W/base-table.log" "$st"

# packet-golden and table-golden
flip testdata/wire/bench_mixed.bin "$W/wire/bench_mixed.bin"
flip testdata/wire/bench_table.bin "$W/wire/bench_table.bin"
st=0; bench/lobster/run --gate --wire-dir "$ROOT/$W/wire" > "$W/packet-golden.log" 2>&1 || st=$?
expect packet-golden 1 "variant 0 is not the wire golden" "$W/packet-golden.log" "$st"
st=0; bench/tables/lobster/leg run --gate --wire-dir "$ROOT/$W/wire" > "$W/table-golden.log" 2>&1 || st=$?
expect table-golden 1 "variant 0 is not the wire golden" "$W/table-golden.log" "$st"

# warning
mkdir -p "$W/warn"
cp bench/lobster/main.lobster "$W/warn/main.lobster"
printf 'for([1]) _, i:\n    sink += i\n' >> "$W/warn/main.lobster"
st=0; bench/lobster/route.sh "$W/warn/main.lobster" ../../../bench/lobster ../../../generated/bench/lobster -- --gate > "$W/warning.log" 2>&1 || st=$?
expect warning 1 "REFUSES an untagged line (a compiler warning?)" "$W/warning.log" "$st"

# timing and names, over a copied tree (the shape gate reads bench/ and the
# bench goldens; nothing else of the tree is needed for its verdict)
T="$W/tree"
mkdir -p "$T/testdata/wire"
cp -R bench "$T/bench"
cp testdata/wire/bench_* "$T/testdata/wire/"
# every file a ledger entry names outside bench/ comes too, or the ledger
# check refuses the copy for a missing file rather than for the plant
find bench -name SHAPE-GATE.allow -exec awk '/^(names|timing|paths|consts) /{print $2}' {} + | sort -u |
while read -r f; do
    case "$f" in bench/*) continue ;; esac
    [ -f "$f" ] || continue
    mkdir -p "$T/$(dirname "$f")"
    cp "$f" "$T/$f"
done
st=0; go run ./bench/tools/shapegate "$T" > "$W/shape-baseline.log" 2>&1 || st=$?
expect shape-baseline 0 "clean" "$W/shape-baseline.log" "$st"
mkdir -p "$T/tools/planted"
printf 'let t0 = seconds_elapsed()\nprint(t0)\n' > "$T/tools/planted/clock.lobster"
st=0; go run ./bench/tools/shapegate "$T" > "$W/timing.log" 2>&1 || st=$?
expect timing 1 "tools/planted/clock.lobster" "$W/timing.log" "$st"
grep -q "^TIMING" "$W/timing.log" || { echo "CONTROL FAILED: timing: not refused under TIMING"; fail=1; }
rm -rf "$T/tools/planted"
# the planted name is read from the corpus, so this script names no shape
shape="$(awk '/^type /{print $2; exit}' bench/corpus/Bench.schema)"
printf '// a planted second runner naming a corpus type: %s\n' "$shape" > "$T/bench/lobster/planted.lobster"
st=0; go run ./bench/tools/shapegate "$T" > "$W/names.log" 2>&1 || st=$?
expect names 1 "bench/lobster/planted.lobster" "$W/names.log" "$st"
grep -q "^NAMES" "$W/names.log" || { echo "CONTROL FAILED: names: not refused under NAMES"; fail=1; }

if [ "$fail" != 0 ]; then
    exit 1
fi
echo "bench-lobster-gate negative control: 5 plants refused by name, 3 clean baselines pass"
