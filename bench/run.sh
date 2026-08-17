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
#                     [--round K] [--bare] [--reuse-build]
#   --debug       also build and run the Debug pair (matched-pair methodology;
#                 only Release numbers are meaningful, Debug is recorded so
#                 pathological debug regressions are visible)
#   --out FILE    results CSV (default bench/results/<date>-<arch>-<host>.csv)
#   --compiler    C++ compiler (default: $CXX, else c++)
#   --only LANG   run a single language leg (c|cpp|go|rust|cs|js). For shared boxes
#                 under one-profile-at-a-time discipline: a driver runs the
#                 legs serially with quiet-window checks between them. Each
#                 leg's CSV carries the full preamble; measurement code and
#                 flags are identical to the all-language invocation.
#   --round K     forward --round K to every runner (BENCH-STANDARD.md §2.4:
#                 one warmup + one measured run per benchmark, per-round rates;
#                 the interleaved driver aggregates across rounds)
#   --bare        rows only, no preamble — for the driver's per-round files
#   --reuse-build reuse existing C/C++ bench binaries instead of recompiling
#                 (the driver builds once at pass start and reuses per round)
#   --inline      run the §4.1 inline verdict pass for each executed language
#                 afterwards and backfill the CSV's inline column (rows stay
#                 unknown — and un-ratioable — without it). Costs a full
#                 go build -a and a cargo rebuild; the pass driver always
#                 does this via its own --inline.
#
# environment:
#   SERIALIZE     path to the classic serialize runtime checkout (default
#                 ../serialize, same as the Makefile)
#   SERIALIZE_C   path to the serialize.c runtime checkout (default
#                 ../serialize.c, same as the Makefile)
#   SERIALIZE_GO / SERIALIZE_RS / SERIALIZE_CS / SERIALIZE_JS
#                 the go/rust/cs/js runtime checkouts (defaults ../serialize.go,
#                 ../serialize.rs, ../serialize.cs, ../serialize.js) — recorded
#                 in the preamble
#                 (§3.5: every row records the runtime commit its leg was
#                 built against) AND fed to the builds: an override
#                 materializes generated manifests / a go -modfile / an
#                 msbuild property (bench/tools/runtime-paths.sh) so the leg
#                 really compiles against the recorded path, and every
#                 preamble line is verified against the toolchain's own
#                 resolution before it is printed — a mismatch REFUSES the
#                 run with no rows (the 2026-08-15 defect: the manifests
#                 hardcoded their paths and a pass recorded a fix branch's
#                 sha while measuring the default checkout)
#   BENCH_OPT_LEVEL  C/C++ optimization level for the standard leg (default
#                 O3; §3.3 publishes O2 and O3). Recorded in the flags line
#                 AND stamped into the runners' opt column via -DBENCH_OPT.
#   BENCH_CPU     core to pin to where taskset exists (default 0)
#   BENCH_NOISE   noise label recorded in the results preamble, e.g.
#                 "NOISY: game server owns isolated cores, bench on shared core 0"

set -e

cd "$(dirname "$0")/.."     # repo root

DEBUG=0
OUT=""
ONLY=""
ROUND=""
BARE=0
REUSE=0
INLINE=0
CXX_BIN="${CXX:-c++}"
while [ $# -gt 0 ]; do
    case "$1" in
        --debug) DEBUG=1 ;;
        --out) OUT="$2"; shift ;;
        --compiler) CXX_BIN="$2"; shift ;;
        --only) ONLY="$2"; shift ;;
        --round) ROUND="$2"; shift ;;
        --bare) BARE=1 ;;
        --reuse-build) REUSE=1 ;;
        --inline) INLINE=1 ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done
RUNNER_ARGS="--csv"
if [ -n "$ROUND" ]; then
    RUNNER_ARGS="--csv --round $ROUND"
fi
case "$ONLY" in
    ""|c|cpp|go|rust|cs|js) ;;
    *) echo "unknown --only language: $ONLY (c|cpp|go|rust|cs|js)" >&2; exit 1 ;;
esac

# §3.5 provenance mechanics: sets the SERIALIZE* defaults, materializes the
# override build configs when an env var points away from a default, and
# provides verify_runtime (the fail-closed check used below)
. bench/tools/runtime-paths.sh
runtime_paths_init
if [ ! -f "$SERIALIZE/serialize.h" ]; then
    echo "serialize.h not found at $SERIALIZE — set SERIALIZE to the classic serialize checkout" >&2
    exit 1
fi
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
NODE_VERSION="$(node --version 2>/dev/null | head -1 || true)"

# Release flags: the schema repo's own flags (-std=c++17 -Wall -Wextra -Werror
# -ffp-contract=off) plus the serialize repo's Release bench configuration
# (-O3 -DNDEBUG -fno-rtti, SERIALIZE_RELEASE). Deliberate divergence from
# serialize's own bench: no -ffast-math — the schema repo pins wire
# determinism with -ffp-contract=off, and the generated quantize paths do
# real float math. Recorded here so the numbers carry their flags.
# -Itest: the corpus maps Vec3/Quat onto the hand-written C++ types in
# test/vec_math.h (SPEC §4.2 native type mapping), so the generated C++ headers
# include it. C++ only — the C target ignores the mapping.
# -Igenerated/bench/cpp: the bench-corpus generated code (RealWorldWire.h —
# the §1.7 realistic snapshot the real_packet rows measure).
COMMON_FLAGS="-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -Igenerated/cpp -Igenerated/bench/cpp -Itest -I$SERIALIZE"

# g++ (13.3 checked) rejects two things in the GENERATED code that clang never
# flags: -Wclass-memaccess (branch zeroing memsets non-trivial generated
# structs, Types.h ReadRigidBody) and -Wtype-limits (read_int64 on a uint32
# full-range field expands to unsigned < 0, Wire.h ReadProbeBits). Those are
# emitter findings, not bench findings — tracked for a fix; suppressed here so
# the bench builds on both compilers with -Werror otherwise live.
if $CXX_BIN --version 2>/dev/null | head -1 | grep -qi 'g++\|gcc'; then
    COMMON_FLAGS="$COMMON_FLAGS -Wno-class-memaccess -Wno-type-limits"
fi
# The optimization level and the runners' opt column come from ONE variable,
# so the recorded flags and the recorded opt cannot disagree (§3.3 publishes
# two levels; -DBENCH_OPT stamps the level into every CSV row).
OPT_LEVEL="${BENCH_OPT_LEVEL:-O3}"
case "$OPT_LEVEL" in
    O2|O3) ;;
    *) echo "BENCH_OPT_LEVEL must be O2 or O3, got $OPT_LEVEL" >&2; exit 1 ;;
esac
RELEASE_FLAGS="-$OPT_LEVEL -DNDEBUG -DSERIALIZE_RELEASE -DBENCH_OPT=\"$OPT_LEVEL\" $COMMON_FLAGS"
DEBUG_FLAGS="-O0 -g -DSERIALIZE_DEBUG $COMMON_FLAGS"

# C: the repo's own C flags (the Makefile's C test leg) at the same Release
# optimization as the C++ leg. NO -flto, deliberately — every other leg is
# measured in its language's default release configuration (cargo release is
# no-LTO too), and serialize.c is a compiled translation unit rather than a
# header, so this row carries a call boundary the header-only C++ runtime does
# not have. That is a property of the runtime's packaging, not of the
# generated code; bench/README.md says so where the numbers live.
# -Igenerated/bench/c: the bench-corpus generated code (RealWorldWire.h —
# the §1.7 realistic snapshot the real_packet rows measure).
C_COMMON_FLAGS="-std=c99 -Wall -Wextra -Werror -Igenerated/c -Igenerated/bench/c -I$SERIALIZE_C"
# gcc (13.3 checked) additionally rejects the C runner's bounded strncpy of a
# golden basename under -Werror (-Wstringop-truncation): the truncation is
# deliberate (fixed-width name slot, terminator guaranteed by the zeroed
# struct), and clang has no such warning. Same pattern as the C++
# accommodations above; every space-box era to date carried this flag by hand
# in its build script (realworld-build.sh) — this lets bench/run.sh build the
# C leg on gcc directly. Warning suppression only: codegen flags unchanged.
if $CC_BIN --version 2>/dev/null | head -1 | grep -qi 'gcc'; then
    C_COMMON_FLAGS="$C_COMMON_FLAGS -Wno-stringop-truncation"
fi
C_RELEASE_FLAGS="-$OPT_LEVEL -DNDEBUG -DBENCH_OPT=\"$OPT_LEVEL\" $C_COMMON_FLAGS"
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
        echo "# node: ${NODE_VERSION:-not present} (NODE_ENV=production — serialize.js's caller-trust release mode; the runner records the mode that ran in its checks column)"
        echo "# pinning: $PIN_DESC"
        echo "# noise: ${BENCH_NOISE:-unlabelled}"
        echo "# schema commit: $(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
        # §3.5 / §5.2: the runtime commit for EVERY language, with its branch —
        # the serialize checkouts ride PR branches during review, and a number
        # measured against a branch must say so. Each line carries its
        # verification verdict from the provenance guard below: the commit is
        # only printed as a build fact when the toolchain itself resolved to
        # that path ([build-verified: ...]); a leg that could not run this
        # invocation says so instead of pretending.
        echo "# serialize commit: $(commit_of "$SERIALIZE") ${PROV_CPP:-}"
        echo "# serialize.c commit: $(commit_of "$SERIALIZE_C") ${PROV_C:-}"
        echo "# serialize.go commit: $(commit_of "$SERIALIZE_GO") ${PROV_GO:-}"
        echo "# serialize.rs commit: $(commit_of "$SERIALIZE_RS") ${PROV_RUST:-}"
        echo "# serialize.cs commit: $(commit_of "$SERIALIZE_CS") ${PROV_CS:-}"
        echo "# serialize.js commit: $(commit_of "$SERIALIZE_JS") ${PROV_JS:-}"
    } >> "$OUT"
}

commit_of() {
    local sha branch
    sha="$(git -C "$1" rev-parse --short HEAD 2>/dev/null || echo unknown)"
    branch="$(git -C "$1" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
    echo "$sha ($branch)"
}

# ---- §3.5 provenance guard: verify BEFORE any row or preamble exists ----
# Every "# serialize* commit:" line the preamble prints must be proven
# against the toolchain's own resolution (bench/tools/runtime-paths.sh), and
# a mismatch refuses the whole run — exit non-zero, no rows, $OUT untouched.
# --bare emits rows with no preamble, so it verifies the leg it will run:
# those rows land under a preamble that makes the same claim.
prov_verify() {
    local resolved rc=0
    resolved="$(verify_runtime "$1")" || rc=$?
    case $rc in
        0) resolved="[build-verified: $resolved]" ;;
        2) resolved="[UNVERIFIED — leg cannot run this invocation; path recorded from the environment, not proven against a build]" ;;
        *)
            echo "REFUSED (§3.5): the $1 leg's build does not resolve to the runtime path the preamble would record — no rows written" >&2
            exit 1 ;;
    esac
    case "$1" in
        cpp)  PROV_CPP="$resolved" ;;
        c)    PROV_C="$resolved" ;;
        go)   PROV_GO="$resolved" ;;
        rust) PROV_RUST="$resolved" ;;
        cs)   PROV_CS="$resolved" ;;
        js)   PROV_JS="$resolved" ;;
    esac
}
# A language whose leg does NOT run in this invocation cannot misrecord what
# it measured — none of its code runs here. Its preamble line still prints,
# so it is MARKED rather than proven; refusing the whole run over a bystander
# language inverted --only's own contract (measured on the EPYC box: go
# toolchain present, serialize.go checkout absent, the cpp control leg
# refused before a single row existed).
prov_note() {
    local resolved
    if resolved="$(verify_runtime "$1" 2>/dev/null)"; then
        resolved="[build-verified: $resolved]"
    else
        resolved="[UNVERIFIED — leg not run this invocation; path recorded from the environment, not proven against a build]"
    fi
    case "$1" in
        cpp)  PROV_CPP="$resolved" ;;
        c)    PROV_C="$resolved" ;;
        go)   PROV_GO="$resolved" ;;
        rust) PROV_RUST="$resolved" ;;
        cs)   PROV_CS="$resolved" ;;
    esac
}
if [ -n "$ONLY" ]; then
    prov_verify "$ONLY"
    for _lang in cpp c go rust cs; do
        [ "$_lang" != "$ONLY" ] && prov_note "$_lang"
    done
else
    for _lang in cpp c go rust cs js; do prov_verify "$_lang"; done
fi

: > "$OUT"
if [ "$BARE" = 0 ]; then
    emit_preamble Release
fi

# ---- C++ (the reference runner) ----
if [ -z "$ONLY" ] || [ "$ONLY" = cpp ]; then
    if [ "$REUSE" = 1 ] && [ -x build/bench/schema_bench_cpp ]; then
        echo "== cpp: reusing build/bench/schema_bench_cpp ==" >&2
    else
        echo "== cpp: build (Release) ==" >&2
        $CXX_BIN $RELEASE_FLAGS bench/cpp/bench_main.cpp -o build/bench/schema_bench_cpp
    fi

    echo "== cpp: run (Release) ==" >&2
    $PIN ./build/bench/schema_bench_cpp $RUNNER_ARGS >> "$OUT"

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
        if [ "$REUSE" = 1 ] && [ -x build/bench/schema_bench_c ]; then
            echo "== c: reusing build/bench/schema_bench_c ==" >&2
        else
            echo "== c: build (Release) ==" >&2
            $CC_BIN $C_RELEASE_FLAGS bench/c/bench_main.c "$SERIALIZE_C/serialize.c" -o build/bench/schema_bench_c -lm
        fi

        echo "== c: run (Release) ==" >&2
        $PIN ./build/bench/schema_bench_c $RUNNER_ARGS >> "$OUT"

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
            # $GO_MODFILE_ARG: empty at the default path; the §3.5 override
            # modfile when SERIALIZE_GO points elsewhere
            ( cd bench/go && $PIN go run $GO_MODFILE_ARG . $RUNNER_ARGS ) >> "$OUT"
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
            # $RS_CARGO_ARGS: empty at the default path; --manifest-path to
            # the §3.5 override manifests (plus --target-dir target, so the
            # binary stays at bench/rust/target/release/benchrust) otherwise
            ( cd bench/rust && PATH="/opt/homebrew/opt/rustup/bin:$PATH" $PIN cargo run --release --quiet $RS_CARGO_ARGS -- $RUNNER_ARGS ) >> "$OUT"
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
            # $CS_PROP_ARGS: empty at the default path; --property:
            # SerializeCsRoot=<abs> (consumed by the csproj includes) otherwise
            ( cd bench/cs && $PIN dotnet run -c Release $CS_PROP_ARGS -- $RUNNER_ARGS ) >> "$OUT"
        else
            echo "SKIP cs: runner present but no dotnet" >&2
        fi
    else
        echo "SKIP cs: runner not landed yet (bench/cs/*.csproj — see bench/cs/README.md)" >&2
    fi
fi

# ---- JavaScript (lands with the serialize.js port) ----
if [ -z "$ONLY" ] || [ "$ONLY" = js ]; then
    if [ -f bench/js/main.mjs ]; then
        if command -v node >/dev/null 2>&1; then
            echo "== js: run ==" >&2
            # NODE_ENV=production: the release leg — serialize.js forks its
            # checked/production modes at module load, and the runner records
            # the mode that ran in its checks column (production = contract).
            # $JS_ENV: empty at the default path; the §3.5 SERIALIZE_JS
            # override otherwise (already verified by the provenance guard).
            ( cd bench/js && $PIN env NODE_ENV=production $JS_ENV node main.mjs $RUNNER_ARGS ) >> "$OUT"
        else
            echo "SKIP js: runner present but no node" >&2
        fi
    else
        echo "SKIP js: runner not landed yet (bench/js/main.mjs — see bench/README.md)" >&2
    fi
fi

# ---- inline verdict pass (§4.1/§4.2): backfill the inline column ----
# js is absent from this list DELIBERATELY: the verdict is a per-compiled-
# language disassembly pass and a JIT leg has no AOT artifact to walk —
# js rows keep inline=unknown, which correctly refuses their ratios.
if [ "$INLINE" = 1 ]; then
    for lang in cpp c go rust cs; do
        if { [ -z "$ONLY" ] || [ "$ONLY" = "$lang" ]; } && grep -q "^$lang," "$OUT" 2>/dev/null; then
            bench/tools/inline-verdict.sh "$lang" "$OUT" \
                || echo "inline verdict failed for $lang — its rows stay unknown (un-ratioable)" >&2
        fi
    done
fi

echo "results: $OUT" >&2
