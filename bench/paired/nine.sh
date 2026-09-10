#!/bin/sh
# THE NINE-LANGUAGE TABLE, from one command. Run from the repository root.
#
#   bench/paired/nine.sh [--lane N] [--rounds 1|2|3] [--out DIR] [--note TEXT]
#
# It builds the driver and every leg, then runs `-mode table`, which asks the
# TREE which table runners it carries (bench/tables/<lang>/), measures exactly
# those with `-mode fast -fast-rounds 3`, names the ones this tree has no
# runner for, and prints ONE markdown table: per language the fixed form's
# save and round trip in ns/record and its bytes, against that language's own
# packet wire and against C++'s fixed form.
#
# The same line runs on every host. The tree, not this script, decides the row
# set, so a leg branch and the integration branch both run it unedited.
#
# --lane N is the Space form. Space's isolated cores are 1..15 and each line
# owns one: `~/bin/bench-lane N` holds that core's lock and `taskset`s the
# whole sitting onto it, so every runner the driver forks inherits the pin and
# the lock is held for the whole sitting rather than per runner. The BUILD goes
# through `~/bin/pin-spread` instead, which spreads each compiler invocation
# over the isolated cores — a build pinned to the one measured core would be
# needlessly slow and would not make the measurement any quieter.
#
# Send its output somewhere OUTSIDE the working tree. Table mode measures a
# named checkpoint, so a stray log file in the tree refuses the sitting; the
# script checks that before it builds and says so.
#
# THE RESULT IS NOT CERTIFIED. It is the fast diagnostic's numbers in the
# published table's shape: reduced iteration counts, no bracketing drift
# controls, no quiet-window seal. The certified sitting stays `-mode run` on
# Space inside the quiet window (bench/README.md, bench/paired/README.md).
set -e

LANE=
ROUNDS=3
OUT=
NOTE=

while [ $# -gt 0 ]; do
    case "$1" in
    --lane)   LANE="$2"; shift 2 ;;
    --rounds) ROUNDS="$2"; shift 2 ;;
    --out)    OUT="$2"; shift 2 ;;
    --note)   NOTE="$2"; shift 2 ;;
    -h|--help)
        sed -n '2,31p' "$0" | cut -c 3-
        exit 0 ;;
    *)
        echo "nine.sh: unknown argument $1" >&2
        exit 2 ;;
    esac
done

[ -f bench/corpus/Bench.schema ] || { echo "nine.sh: run from the repository root" >&2; exit 2; }

# Say this BEFORE the build rather than after it. Table mode measures a named
# checkpoint and refuses a tree that does not match one, and the commonest way
# to dirty the tree is to redirect this script's own log into it — so send
# stdout and stderr somewhere outside the working tree.
if [ -n "$(git status --porcelain 2>/dev/null)" ]; then
    echo "nine.sh: the tree is not clean, and table mode measures a named checkpoint." >&2
    echo "         Commit or stash first, and keep logs outside the working tree:" >&2
    git status --porcelain >&2
    exit 2
fi

PIN=
RUN=
if [ -n "$LANE" ]; then
    case "$LANE" in
    ''|*[!0-9]*) echo "nine.sh: --lane takes a core number" >&2; exit 2 ;;
    esac
    command -v bench-lane >/dev/null 2>&1 || { echo "nine.sh: --lane needs bench-lane on PATH (Space)" >&2; exit 2; }
    command -v pin-spread >/dev/null 2>&1 || { echo "nine.sh: --lane needs pin-spread on PATH (Space)" >&2; exit 2; }
    PIN="pin-spread"
    RUN="bench-lane $LANE"
    : "${NOTE:=pinned diagnostic on lane $LANE; no quiet-window claim}"
fi
: "${NOTE:=uncontrolled diagnostic; no quiet-window claim}"
: "${OUT:=build/paired-nine/$(date -u +%Y%m%dT%H%M%SZ)}"

# The build must land before the driver reads its own HEAD and hashes: table
# mode never builds, and refuses a checkpoint that does not match. The driver
# names the languages so this wrapper and the mode that measures them cannot
# disagree about what this tree carries.
$PIN go build -o bin/schema-paired ./bench/paired
LANGS=$(bin/schema-paired -mode table-langs)

# ONE BUILD, THEN ONE GATE PER LANGUAGE.
#
# The build takes the whole sitting in a single invocation because
# build/paired/build.json is written whole and replaces what was there, and the
# measuring mode refuses a leg that manifest does not carry. Two builds would
# leave a manifest describing half of this table.
#
# The gate is the other way round. A request is one shape or the other and
# never a mixture — a table-only leg may not share `-langs` with a paired one —
# so this walks the discovered list one language at a time, which is always one
# shape, and needs no opinion of its own about which languages are which. The
# gate reuses the build above rather than repeating it.
$PIN bin/schema-paired -mode build -langs "$LANGS"
for LANG_ in $(echo "$LANGS" | tr ',' ' '); do
    bin/schema-paired -mode gate -reuse-build -langs "$LANG_"
done

# The whole pass goes through one bench-lane, so the lock is held once and
# every runner the driver forks inherits the pin. -lane only records it.
if [ -n "$LANE" ]; then
    exec $RUN bin/schema-paired -mode table -out "$OUT" -fast-rounds "$ROUNDS" -noise-note "$NOTE" -lane "$LANE"
fi
exec bin/schema-paired -mode table -out "$OUT" -fast-rounds "$ROUNDS" -noise-note "$NOTE"
