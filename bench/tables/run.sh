#!/bin/sh
# bench/tables/run.sh — the tables bench pass.
#
#   bench/tables/run.sh                 every registered leg, one CSV under
#                                       bench/tables/results/
#   bench/tables/run.sh --only c,cpp,cs,go  four required legs
#   bench/tables/run.sh --gate --only c,cpp,cs,go --bare  no timing
#   bench/tables/run.sh --rounds 5      interleaved rounds (§2.4): every leg
#                                       once per round, so every leg sees the
#                                       same load window; the driver, not the
#                                       runner, aggregates across rounds
#   bench/tables/run.sh --tag pairing   name the sitting in the file name
#   bench/tables/run.sh --bare          rows only, no preamble, stdout
#
# Run from the REPOSITORY ROOT. The legs are DISCOVERED — every
# bench/tables/<lang>/leg, in name order — and nothing here knows a language:
# adding a port is that one command, which is the whole registration contract
# (bench/tables/README.md).
#
# WHY THIS IS A SEPARATE PASS FROM bench/run.sh, stated once and not repeated:
# a run's `corpus_id` is FNV-1a-64 over the goldens THAT RUN LOADED (§1.6), so
# folding the table corpus into the type pass would change the corpus_id of
# every bench_mixed row and the tools would then refuse to divide today's type
# numbers against any earlier board. Two corpora, two passes, two boards, and
# every row carries family `table` so a cross-family division refuses on its
# own (§5.3).
set -e

ONLY=""
GATE=0
ROUNDS=1
BARE=0
TAG=""
while [ $# -gt 0 ]; do
    case "$1" in
        --only)   [ $# -ge 2 ] || exit 2; ONLY="$2"; shift 2 ;;
        --gate)   GATE=1; shift ;;
        --rounds) [ $# -ge 2 ] || exit 2; ROUNDS="$2"; shift 2 ;;
        --tag)    [ $# -ge 2 ] || exit 2; TAG="$2"; shift 2 ;;
        --bare)   BARE=1; shift ;;
        *) echo "usage: $0 [--only <lang,...>] [--gate] [--rounds N] [--tag <name>] [--bare]" >&2; exit 1 ;;
    esac
done

case "$ROUNDS" in ''|*[!0-9]*|0|0*) echo "--rounds takes a positive integer" >&2; exit 2 ;; esac
case "$TAG" in *[!a-zA-Z0-9._-]*) echo "--tag takes a filename label" >&2; exit 2 ;; esac
case "$ONLY" in *[!a-z0-9,]*|,*|*,|*,,*) echo "--only takes comma-separated language names" >&2; exit 2 ;; esac

if [ ! -d bench/tables ]; then
    echo "run this from the repository root" >&2
    exit 1
fi

HOST="$(hostname -s 2>/dev/null || echo unknown)"
ARCH="$(uname -m)"
case "$ARCH" in
    arm64|aarch64) CPU="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ //' || echo unknown)" ;;
    *)             CPU="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | sed 's/^ //' || echo unknown)" ;;
esac

commit_of() {
    sha="$(git -C "$1" rev-parse --short HEAD 2>/dev/null || echo unknown)"
    branch="$(git -C "$1" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
    dirty=""
    if [ "$sha" != "unknown" ] && ! git -C "$1" diff --quiet HEAD -- 2>/dev/null; then
        dirty="-dirty"
    fi
    echo "$sha$dirty ($branch)"
}

mkdir -p build
PASS_DIR=$(mktemp -d "build/table-pass.XXXXXX")
trap 'rm -rf "$PASS_DIR"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
DRAFT="$PASS_DIR/result.csv"
: > "$DRAFT"
if [ "$BARE" = 1 ]; then
    OUT=/dev/stdout
else
    mkdir -p bench/tables/results
    OUT="bench/tables/results/$(date -u +%F)${TAG:+-$TAG}-$ARCH-$HOST.csv"
    if [ -e "$OUT" ]; then
        echo "refusing to overwrite $OUT — pass --tag <name>, or remove it if the overwrite is deliberate" >&2
        exit 1
    fi
    : # Publish the file only after every selected leg and round succeeds.
    {
        echo "# schema TABLES bench results (bench/tables/README.md)"
        echo "# date: $(date -u +%FT%TZ)"
        echo "# host: $HOST  arch: $ARCH  os: $(uname -sr)"
        echo "# cpu: $CPU"
        echo "# build: Release"
        echo "# corpus: bench/corpus/BenchTable.schema (one fixed table on the tolerant wire, docs/SPEC-TABLES.md §3)"
        echo "# c compiler: $(${CC:-cc} --version 2>/dev/null | head -1)"
        echo "# go: $(go version 2>/dev/null || echo absent)"
        echo "# c flags: -std=c99 -Wall -Wextra -Werror -ffp-contract=off -${BENCH_OPT_LEVEL:-O3} -DNDEBUG"
        echo "# cpp flags: -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -${BENCH_OPT_LEVEL:-O3} -DNDEBUG -fno-rtti"
        echo "# dotnet tiered compilation: ${DOTNET_TieredCompilation:-runtime-default}"
        echo "# cpp compiler: $(${CXX:-c++} --version 2>/dev/null | head -1)"
        echo "# dotnet: $(dotnet --version 2>/dev/null || echo 'not present')"
        echo "# rounds: $ROUNDS"
        echo "# pinning: not set by this driver; external affinity, if any, must be recorded in noise"
        echo "# noise: ${BENCH_NOISE:-unlabelled}"
        echo "# schema commit: $(commit_of .)"
        echo "# serialize.cs commit: $(commit_of "${SERIALIZE_CS:-../serialize.cs}")  (the closure's type codecs only — no line of the measured table path enters it)"
    } >> "$DRAFT"
fi

# Build every selected leg before starting any clock. A requested missing leg
# is a failed pass; only unrequested default discovery may skip unavailable ports.
LEGS=""
for cmd in bench/tables/*/leg; do
    [ -f "$cmd" ] || continue
    lang="$(basename "$(dirname "$cmd")")"
    if [ -n "$ONLY" ]; then
        case ",$ONLY," in *,$lang,*) ;; *) continue ;; esac
    fi
    echo "== $lang: build ==" >&2
    status=0
    "$cmd" build || status=$?
    if [ "$status" = 2 ] && [ -z "$ONLY" ]; then
        echo "SKIP $lang (toolchain or generated sources not present)" >&2
        continue
    fi
    [ "$status" = 0 ] || { echo "FAIL $lang: build" >&2; exit 1; }
    LEGS="$LEGS $lang"
done
[ -n "$LEGS" ] || { echo "no legs ran" >&2; exit 1; }
for wanted in $(printf '%s' "$ONLY" | tr ',' ' '); do
    case " $LEGS " in *" $wanted "*) ;; *) echo "requested leg $wanted is unavailable" >&2; exit 1 ;; esac
done

if [ "$GATE" = 1 ]; then
    for lang in $LEGS; do
        echo "== $lang: correctness gate, no timing ==" >&2
        bench/tables/"$lang"/leg run --gate || exit 1
    done
    echo "all selected corpus gates passed; no performance measured" >&2
    exit 0
fi

HEADER='lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline'
r=0
while [ "$r" -lt "$ROUNDS" ]; do
    ROUND_FILE="$PASS_DIR/round-$r.csv"
    printf '# round: %s\n%s\n' "$r" "$HEADER" > "$ROUND_FILE"
    # The round is the outer loop: every language sees the same time window.
    for lang in $LEGS; do
        echo "== $lang: round $r ==" >&2
        if [ "$ROUNDS" = 1 ]; then
            bench/tables/"$lang"/leg run --csv > "$PASS_DIR/leg.csv" || exit 1
        else
            bench/tables/"$lang"/leg run --csv --round "$r" > "$PASS_DIR/leg.csv" || exit 1
        fi
        # Capture the producer's status before filtering its header. A pipeline
        # ending in a successful filter must never turn a failed codec green.
        awk -F, -v lang="$lang" '
            /^lang,bench,path,/ || /^#/ || !NF {next}
            NF != 17 || $1 != lang || $2 != "bench_table" || $13 != "table" {exit 1}
            $3 == "write" {write++; print; next}
            $3 == "round_trip" {trip++; print; next}
            {exit 1}
            END {if (write != 1 || trip != 1) exit 1}
        ' "$PASS_DIR/leg.csv" >> "$ROUND_FILE" || {
            echo "FAIL $lang: expected one write and one round_trip table row" >&2; exit 1;
        }
    done
    r=$((r + 1))
done

if [ "$ROUNDS" = 1 ]; then
    cat "$PASS_DIR/round-0.csv" >> "$DRAFT"
else
    # Reuse the packet pass's identity checks and statistical aggregation.
    go run ./bench/tools aggregate "$PASS_DIR"/round-*.csv >> "$DRAFT"
fi
if [ "$BARE" = 1 ]; then
    cat "$DRAFT"
else
    # Same-filesystem link is non-replacing, unlike a final mv after a stale
    # existence check. A failed or interrupted pass leaves no results CSV.
    ln "$DRAFT" "$OUT"
    echo "wrote $OUT" >&2
fi
