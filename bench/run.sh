#!/bin/bash
# schema bench — top-level driver.
#
# Builds and runs whichever language runners are available, and collects every
# runner's CSV rows into one results file with a metadata preamble (host, cpu,
# compiler, flags, pinning, noise). Languages whose runner or toolchain is
# missing are SKIPPED with the reason printed — the go/rust/cs runners land
# with the serialize ports (see bench/README.md for the runner contract).
#
# usage: bench/run.sh [--debug] [--out FILE] [--compiler CXX]
#   --debug       also build and run the Debug pair (matched-pair methodology;
#                 only Release numbers are meaningful, Debug is recorded so
#                 pathological debug regressions are visible)
#   --out FILE    results CSV (default bench/results/<date>-<arch>-<host>.csv)
#   --compiler    C++ compiler (default: $CXX, else c++)
#
# environment:
#   SERIALIZE     path to the classic serialize runtime checkout
#                 (default ../serialize-cs-port/serialize, same as the Makefile)
#   BENCH_CPU     core to pin to where taskset exists (default 0)
#   BENCH_NOISE   noise label recorded in the results preamble, e.g.
#                 "NOISY: game server owns isolated cores, bench on shared core 0"

set -e

cd "$(dirname "$0")/.."     # repo root

DEBUG=0
OUT=""
CXX_BIN="${CXX:-c++}"
while [ $# -gt 0 ]; do
    case "$1" in
        --debug) DEBUG=1 ;;
        --out) OUT="$2"; shift ;;
        --compiler) CXX_BIN="$2"; shift ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

SERIALIZE="${SERIALIZE:-../serialize-cs-port/serialize}"
if [ ! -f "$SERIALIZE/serialize.h" ]; then
    echo "serialize.h not found at $SERIALIZE — set SERIALIZE to the classic serialize checkout" >&2
    exit 1
fi

ARCH="$(uname -m)"
HOST="$(hostname -s)"
OUT="${OUT:-bench/results/$(date +%F)-$ARCH-$HOST.csv}"
mkdir -p bench/results build/bench

# cpu pinning: taskset where it exists (linux); none on macOS
PIN=""
PIN_DESC="none"
if command -v taskset >/dev/null 2>&1; then
    PIN="taskset -c ${BENCH_CPU:-0}"
    PIN_DESC="taskset -c ${BENCH_CPU:-0}"
fi

# cpu model
case "$(uname -s)" in
    Darwin) CPU="$(sysctl -n machdep.cpu.brand_string)" ;;
    *) CPU="$(grep -m1 'model name' /proc/cpuinfo | sed 's/.*: //')" ;;
esac

# Release flags: the schema repo's own flags (-std=c++17 -Wall -Wextra -Werror
# -ffp-contract=off) plus the serialize repo's Release bench configuration
# (-O3 -DNDEBUG -fno-rtti, SERIALIZE_RELEASE). Deliberate divergence from
# serialize's own bench: no -ffast-math — the schema repo pins wire
# determinism with -ffp-contract=off, and the generated quantize paths do
# real float math. Recorded here so the numbers carry their flags.
COMMON_FLAGS="-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -Igenerated/cpp -I$SERIALIZE"
RELEASE_FLAGS="-O3 -DNDEBUG -DSERIALIZE_RELEASE $COMMON_FLAGS"
DEBUG_FLAGS="-O0 -g -DSERIALIZE_DEBUG $COMMON_FLAGS"

emit_preamble() {
    local build="$1"
    {
        echo "# schema bench results"
        echo "# date: $(date -u +%FT%TZ)"
        echo "# host: $HOST  arch: $ARCH  os: $(uname -sr)"
        echo "# cpu: $CPU"
        echo "# build: $build"
        echo "# cpp compiler: $($CXX_BIN --version 2>/dev/null | head -1)"
        echo "# cpp flags: $([ "$build" = Release ] && echo "$RELEASE_FLAGS" || echo "$DEBUG_FLAGS")"
        echo "# pinning: $PIN_DESC"
        echo "# noise: ${BENCH_NOISE:-unlabelled}"
        echo "# schema commit: $(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
        echo "# serialize commit: $(git -C "$SERIALIZE" rev-parse --short HEAD 2>/dev/null || echo unknown)"
    } >> "$OUT"
}

# ---- C++ (the reference runner) ----
echo "== cpp: build (Release) ==" >&2
$CXX_BIN $RELEASE_FLAGS bench/cpp/bench_main.cpp -o build/bench/schema_bench_cpp

: > "$OUT"
emit_preamble Release
echo "== cpp: run (Release) ==" >&2
$PIN ./build/bench/schema_bench_cpp --csv >> "$OUT"

if [ "$DEBUG" = 1 ]; then
    echo "== cpp: build (Debug) ==" >&2
    $CXX_BIN $DEBUG_FLAGS bench/cpp/bench_main.cpp -o build/bench/schema_bench_cpp_debug
    echo "" >> "$OUT"
    emit_preamble Debug
    echo "== cpp: run (Debug) ==" >&2
    $PIN ./build/bench/schema_bench_cpp_debug --csv >> "$OUT"
fi

# ---- Go (lands with the serialize.go port) ----
if [ -f bench/go/main.go ]; then
    if command -v go >/dev/null 2>&1; then
        echo "== go: run ==" >&2
        ( cd bench/go && $PIN go run . --csv ) >> "$OUT"
    else
        echo "SKIP go: runner present but no go toolchain" >&2
    fi
else
    echo "SKIP go: runner not landed yet (bench/go/main.go — see bench/go/README.md)" >&2
fi

# ---- Rust (lands with the serialize.rs port) ----
if [ -f bench/rust/Cargo.toml ]; then
    if command -v cargo >/dev/null 2>&1 || [ -x /opt/homebrew/opt/rustup/bin/cargo ]; then
        echo "== rust: run ==" >&2
        ( cd bench/rust && PATH="/opt/homebrew/opt/rustup/bin:$PATH" $PIN cargo run --release --quiet -- --csv ) >> "$OUT"
    else
        echo "SKIP rust: runner present but no cargo" >&2
    fi
else
    echo "SKIP rust: runner not landed yet (bench/rust/Cargo.toml — see bench/rust/README.md)" >&2
fi

# ---- C# (lands with the serialize.cs port) ----
if ls bench/cs/*.csproj >/dev/null 2>&1; then
    if command -v dotnet >/dev/null 2>&1; then
        echo "== cs: run ==" >&2
        ( cd bench/cs && $PIN dotnet run -c Release -- --csv ) >> "$OUT"
    else
        echo "SKIP cs: runner present but no dotnet" >&2
    fi
else
    echo "SKIP cs: runner not landed yet (bench/cs/*.csproj — see bench/cs/README.md)" >&2
fi

echo "results: $OUT" >&2
