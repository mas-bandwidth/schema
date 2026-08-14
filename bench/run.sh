#!/bin/bash
# schema bench — top-level driver.
#
# Builds and runs whichever language runners are available, and collects every
# runner's CSV rows into one results file with a metadata preamble (host, cpu,
# compiler, flags, pinning, noise). Languages whose runner or toolchain is
# missing are SKIPPED with the reason printed — the go/rust/cs runners land
# with the serialize ports (see bench/README.md for the runner contract).
#
# usage: bench/run.sh [--debug] [--out FILE] [--compiler CXX] [--only LANG]
#   --debug       also build and run the Debug pair (matched-pair methodology;
#                 only Release numbers are meaningful, Debug is recorded so
#                 pathological debug regressions are visible)
#   --out FILE    results CSV (default bench/results/<date>-<arch>-<host>.csv)
#   --compiler    C++ compiler (default: $CXX, else c++)
#   --only LANG   run a single language leg (c|cpp|go|rust|cs). For shared boxes
#                 under one-profile-at-a-time discipline: a driver runs the
#                 legs serially with quiet-window checks between them. Each
#                 leg's CSV carries the full preamble; measurement code and
#                 flags are identical to the all-language invocation.
#
# environment:
#   SERIALIZE     path to the classic serialize runtime checkout (default
#                 ../serialize, same as the Makefile)
#   SERIALIZE_C   path to the serialize.c runtime checkout (default
#                 ../serialize.c, same as the Makefile)
#   BENCH_CPU     core to pin to where taskset exists (default 0)
#   BENCH_NOISE   noise label recorded in the results preamble, e.g.
#                 "NOISY: game server owns isolated cores, bench on shared core 0"

set -e

cd "$(dirname "$0")/.."     # repo root

DEBUG=0
OUT=""
ONLY=""
CXX_BIN="${CXX:-c++}"
while [ $# -gt 0 ]; do
    case "$1" in
        --debug) DEBUG=1 ;;
        --out) OUT="$2"; shift ;;
        --compiler) CXX_BIN="$2"; shift ;;
        --only) ONLY="$2"; shift ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done
case "$ONLY" in
    ""|c|cpp|go|rust|cs) ;;
    *) echo "unknown --only language: $ONLY (c|cpp|go|rust|cs)" >&2; exit 1 ;;
esac

SERIALIZE="${SERIALIZE:-../serialize}"
if [ ! -f "$SERIALIZE/serialize.h" ]; then
    echo "serialize.h not found at $SERIALIZE — set SERIALIZE to the classic serialize checkout" >&2
    exit 1
fi
SERIALIZE_C="${SERIALIZE_C:-../serialize.c}"
CC_BIN="${CC:-cc}"

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

# language toolchain versions — every CSV carries its exact builders
GO_VERSION="$(go version 2>/dev/null | head -1 || true)"
RUST_VERSION=""
if command -v cargo >/dev/null 2>&1; then
    RUST_VERSION="$(cargo --version 2>/dev/null | head -1); $(rustc --version 2>/dev/null | head -1)"
elif [ -x /opt/homebrew/opt/rustup/bin/cargo ]; then
    RUST_VERSION="$(/opt/homebrew/opt/rustup/bin/cargo --version | head -1); $(/opt/homebrew/opt/rustup/bin/rustc --version | head -1)"
fi
DOTNET_VERSION="$(dotnet --version 2>/dev/null | head -1 || true)"

# Release flags: the schema repo's own flags (-std=c++17 -Wall -Wextra -Werror
# -ffp-contract=off) plus the serialize repo's Release bench configuration
# (-O3 -DNDEBUG -fno-rtti, SERIALIZE_RELEASE). Deliberate divergence from
# serialize's own bench: no -ffast-math — the schema repo pins wire
# determinism with -ffp-contract=off, and the generated quantize paths do
# real float math. Recorded here so the numbers carry their flags.
# -Itest: the corpus maps Vec3/Quat onto the hand-written C++ types in
# test/vec_math.h (SPEC §4.2 native type mapping), so the generated C++ headers
# include it. C++ only — the C target ignores the mapping.
COMMON_FLAGS="-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -Igenerated/cpp -Itest -I$SERIALIZE"

# g++ (13.3 checked) rejects two things in the GENERATED code that clang never
# flags: -Wclass-memaccess (branch zeroing memsets non-trivial generated
# structs, Types.h ReadRigidBody) and -Wtype-limits (read_int64 on a uint32
# full-range field expands to unsigned < 0, Wire.h ReadProbeBits). Those are
# emitter findings, not bench findings — tracked for a fix; suppressed here so
# the bench builds on both compilers with -Werror otherwise live.
if $CXX_BIN --version 2>/dev/null | head -1 | grep -qi 'g++\|gcc'; then
    COMMON_FLAGS="$COMMON_FLAGS -Wno-class-memaccess -Wno-type-limits"
fi
RELEASE_FLAGS="-O3 -DNDEBUG -DSERIALIZE_RELEASE $COMMON_FLAGS"
DEBUG_FLAGS="-O0 -g -DSERIALIZE_DEBUG $COMMON_FLAGS"

# C: the repo's own C flags (the Makefile's C test leg) at the same Release
# optimization as the C++ leg. NO -flto, deliberately — every other leg is
# measured in its language's default release configuration (cargo release is
# no-LTO too), and serialize.c is a compiled translation unit rather than a
# header, so this row carries a call boundary the header-only C++ runtime does
# not have. That is a property of the runtime's packaging, not of the
# generated code; bench/README.md says so where the numbers live.
C_COMMON_FLAGS="-std=c99 -Wall -Wextra -Werror -Igenerated/c -I$SERIALIZE_C"
C_RELEASE_FLAGS="-O3 -DNDEBUG $C_COMMON_FLAGS"
C_DEBUG_FLAGS="-O0 -g $C_COMMON_FLAGS"

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
        echo "# c compiler: $($CC_BIN --version 2>/dev/null | head -1)"
        echo "# c flags: $([ "$build" = Release ] && echo "$C_RELEASE_FLAGS" || echo "$C_DEBUG_FLAGS") (serialize.c compiled in, no LTO)"
        echo "# go: ${GO_VERSION:-not present} (go run, default optimized build)"
        echo "# rust: ${RUST_VERSION:-not present} (cargo run --release: opt-level 3, no LTO)"
        echo "# dotnet: ${DOTNET_VERSION:-not present} (dotnet run -c Release, workstation GC)"
        echo "# pinning: $PIN_DESC"
        echo "# noise: ${BENCH_NOISE:-unlabelled}"
        echo "# schema commit: $(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
        echo "# serialize commit: $(git -C "$SERIALIZE" rev-parse --short HEAD 2>/dev/null || echo unknown)"
    } >> "$OUT"
}

: > "$OUT"
emit_preamble Release

# ---- C++ (the reference runner) ----
if [ -z "$ONLY" ] || [ "$ONLY" = cpp ]; then
    echo "== cpp: build (Release) ==" >&2
    $CXX_BIN $RELEASE_FLAGS bench/cpp/bench_main.cpp -o build/bench/schema_bench_cpp

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
fi

# ---- C (the fifth target; serialize.c is a compiled TU, not a header) ----
if [ -z "$ONLY" ] || [ "$ONLY" = c ]; then
    if [ ! -f generated/c/TypesWire.h ]; then
        echo "SKIP c: generated/c is missing — run make first" >&2
    elif [ ! -f "$SERIALIZE_C/serialize.c" ]; then
        echo "SKIP c: serialize.c not found at $SERIALIZE_C (set SERIALIZE_C)" >&2
    else
        echo "== c: build (Release) ==" >&2
        $CC_BIN $C_RELEASE_FLAGS bench/c/bench_main.c "$SERIALIZE_C/serialize.c" -o build/bench/schema_bench_c -lm

        echo "== c: run (Release) ==" >&2
        $PIN ./build/bench/schema_bench_c --csv >> "$OUT"

        if [ "$DEBUG" = 1 ]; then
            echo "== c: build (Debug) ==" >&2
            $CC_BIN $C_DEBUG_FLAGS bench/c/bench_main.c "$SERIALIZE_C/serialize.c" -o build/bench/schema_bench_c_debug -lm
            echo "== c: run (Debug) ==" >&2
            $PIN ./build/bench/schema_bench_c_debug --csv >> "$OUT"
        fi
    fi
fi

# ---- Go (lands with the serialize.go port) ----
if [ -z "$ONLY" ] || [ "$ONLY" = go ]; then
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
fi

# ---- Rust (lands with the serialize.rs port) ----
if [ -z "$ONLY" ] || [ "$ONLY" = rust ]; then
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
fi

# ---- C# (lands with the serialize.cs port) ----
if [ -z "$ONLY" ] || [ "$ONLY" = cs ]; then
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
fi

echo "results: $OUT" >&2
