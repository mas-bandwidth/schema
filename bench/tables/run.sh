#!/bin/sh
# bench/tables/run.sh — the tables bench pass.
#
#   bench/tables/run.sh                 every registered leg, one CSV under
#                                       bench/tables/results/
#   bench/tables/run.sh --only cs       one leg
#   bench/tables/run.sh --rounds 5      interleaved rounds (§2.4): every leg
#                                       once per round, so every leg sees the
#                                       same load window; the driver, not the
#                                       runner, aggregates across rounds
#   bench/tables/run.sh --tag pairing   name the sitting in the file name
#   bench/tables/run.sh --bare          rows only, no preamble, stdout
#
# Run from the REPOSITORY ROOT. The legs come from bench/tables/legs.txt and
# nothing here knows a language: adding a port is one line there plus one
# command, which is the whole registration contract (bench/tables/README.md).
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
ROUNDS=1
BARE=0
TAG=""
while [ $# -gt 0 ]; do
    case "$1" in
        --only)   ONLY="$2"; shift 2 ;;
        --rounds) ROUNDS="$2"; shift 2 ;;
        --tag)    TAG="$2"; shift 2 ;;
        --bare)   BARE=1; shift ;;
        *) echo "usage: $0 [--only <lang>] [--rounds N] [--tag <name>] [--bare]" >&2; exit 1 ;;
    esac
done

if [ ! -f bench/tables/legs.txt ]; then
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
    if [ "$sha" != "unknown" ] && ! git -C "$1" diff-index --quiet HEAD -- 2>/dev/null; then
        dirty="-dirty"
    fi
    echo "$sha$dirty ($branch)"
}

if [ "$BARE" = 1 ]; then
    OUT=/dev/stdout
else
    mkdir -p bench/tables/results
    OUT="bench/tables/results/$(date -u +%F)${TAG:+-$TAG}-$ARCH-$HOST.csv"
    if [ -e "$OUT" ]; then
        echo "refusing to overwrite $OUT — pass --tag <name>, or remove it if the overwrite is deliberate" >&2
        exit 1
    fi
    : > "$OUT"
    {
        echo "# schema TABLES bench results (bench/tables/README.md)"
        echo "# date: $(date -u +%FT%TZ)"
        echo "# host: $HOST  arch: $ARCH  os: $(uname -sr)"
        echo "# cpu: $CPU"
        echo "# build: Release"
        echo "# corpus: bench/corpus/BenchTable.schema (one fixed table on the tolerant wire, docs/SPEC-TABLES.md §3)"
        echo "# cpp compiler: $(${CXX:-c++} --version 2>/dev/null | head -1)"
        echo "# dotnet: $(dotnet --version 2>/dev/null || echo 'not present')"
        echo "# rounds: $ROUNDS"
        echo "# pinning: $( [ "$(uname -s)" = Linux ] && echo "taskset -c ${BENCH_CPU:-unset}" || echo "none (macOS has no taskset)" )"
        echo "# noise: ${BENCH_NOISE:-unlabeled}"
        echo "# schema commit: $(commit_of .)"
        echo "# serialize.cs commit: $(commit_of "${SERIALIZE_CS:-../serialize.cs}")  (the closure's type codecs only — no line of the measured table path enters it)"
    } >> "$OUT"
fi

# The CSV v2 header (§5.1), written once here rather than by the first leg to
# print one. emit() runs in a pipeline and therefore in a subshell, so it can
# keep no state between legs; dropping every leg's header unconditionally is
# the form that does not depend on any.
echo "lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline" >> "$OUT"

emit() {
    while IFS= read -r line; do
        case "$line" in
            lang,bench,path,*) ;;
            *) [ -n "$line" ] && echo "$line" >> "$OUT" ;;
        esac
    done
}

RAN=0
while read -r lang cmd; do
    case "$lang" in ''|\#*) continue ;; esac
    [ -n "$ONLY" ] && [ "$ONLY" != "$lang" ] && continue

    echo "== $lang: build ==" >&2
    set +e
    "$cmd" build
    status=$?
    set -e
    if [ "$status" = 2 ]; then
        echo "SKIP $lang (toolchain or generated sources not present)" >&2
        continue
    fi
    [ "$status" != 0 ] && { echo "FAIL $lang: build" >&2; exit 1; }

    r=0
    while [ "$r" -lt "$ROUNDS" ]; do
        if [ "$ROUNDS" = 1 ]; then
            echo "== $lang: run ==" >&2
            "$cmd" run --csv | emit
        else
            echo "== $lang: round $r ==" >&2
            "$cmd" run --csv --round "$r" | emit
        fi
        r=$(( r + 1 ))
    done
    RAN=$(( RAN + 1 ))
done < bench/tables/legs.txt

if [ "$RAN" = 0 ]; then
    echo "no legs ran" >&2
    exit 1
fi

[ "$BARE" = 0 ] && echo "wrote $OUT" >&2
exit 0
