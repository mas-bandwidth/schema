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
#                     [--round K] [--bare] [--reuse-build] [--quick]
#   --debug       also build and run the Debug pair (matched-pair methodology;
#                 only Release numbers are meaningful, Debug is recorded so
#                 pathological debug regressions are visible)
#   --out FILE    results CSV (default bench/results/<date>-<arch>-<host>.csv)
#   --compiler    C++ compiler (default: $CXX, else c++)
#   --quick       the iteration instrument, never the certification
#                 instrument: every leg runs bench_mixed ONLY (3 measured
#                 runs, golden gate intact; the native legs run their gen
#                 and rt rows both), and the driver prints the blended
#                 tables — per-message time averaged over write and read,
#                 fastest language = 100% — after the CSV lands. The
#                 headline table is SINGLE-SUBJECT: family gen (generated
#                 code) for every language, family and checks mode printed
#                 per row under a caption naming what is held constant; the
#                 rt blend prints as a second labeled section (#177), and a
#                 leg whose toolchain is missing prints as an ABSENT row
#                 with the reason (#175).
#                 Scaling constants are PROPOSED in BENCH-STANDARD.md terms.
#   --only LANG   run a single language leg (c|cpp|go|rust|cs|js|java|dart|elixir).
#                 An --only run whose leg is skipped (missing toolchain)
#                 exits non-zero with the reason — zero rows is a refusal,
#                 never a quietly green empty CSV (#175). For shared boxes
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
QUICK=0
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
        --quick) QUICK=1 ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done
RUNNER_ARGS="--csv"
if [ -n "$ROUND" ]; then
    RUNNER_ARGS="--csv --round $ROUND"
fi
if [ "$QUICK" = 1 ]; then
    RUNNER_ARGS="$RUNNER_ARGS --quick"
fi
case "$ONLY" in
    ""|c|cpp|go|rust|cs|js|java|dart|elixir) ;;
    *) echo "unknown --only language: $ONLY (c|cpp|go|rust|cs|js|java|dart|elixir)" >&2; exit 1 ;;
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

# Loud skips (#175): a leg that cannot run is RECORDED, printed as an ABSENT
# row in the quick tables, and an --only invocation that produced zero rows
# exits non-zero — never a silently green empty table.
SKIP_NOTES=""
skip_leg() {
    # ";"-joined lang|reason pairs (BWK awk rejects newlines in -v strings)
    SKIP_NOTES="${SKIP_NOTES}${1}|${2};"
    echo "SKIP $1: $2" >&2
}

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

# The codegen-only legs (java, dart, elixir): schema's backends for these
# languages emit self-contained code with no runtime library, so the legs
# build from the repo's own generated/bench sources and there is no §3.5
# runtime checkout to verify. Toolchains are the repo-pinned dist/ installs
# (the Makefile's own defaults); JAVA/JAVAC/DART/BEAM_PATH override.
JAVA_BIN="${JAVA:-$PWD/dist/jdk-21.0.12.1/Contents/Home/bin/java}"
JAVAC_BIN="${JAVAC:-$PWD/dist/jdk-21.0.12.1/Contents/Home/bin/javac}"
DART_BIN="${DART:-$PWD/dist/dart-sdk-3.13.2/bin/dart}"
BEAM_PATH="${BEAM_PATH:-$PWD/dist/otp-29.0.5/bin:$PWD/dist/elixir-1.20.4/bin}"
JAVA_VERSION="$("$JAVA_BIN" --version 2>/dev/null | head -1 || true)"
DART_VERSION="$("$DART_BIN" --version 2>/dev/null | head -1 || true)"
ELIXIR_VERSION="$(PATH="$BEAM_PATH:$PATH" elixir --short-version 2>/dev/null | head -1 || true)"

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
        echo "# rust: ${RUST_VERSION:-not present} (cargo run --release at opt-level ${OPT_LEVEL#O}, no LTO)"
        echo "# dotnet: ${DOTNET_VERSION:-not present} (dotnet run -c Release, workstation GC)"
        echo "# node: ${NODE_VERSION:-not present} (NODE_ENV=production — serialize.js's caller-trust release mode; the runner records the mode that ran in its checks column)"
        echo "# java: ${JAVA_VERSION:-not present} (generated codecs, zero runtime dependency; default JVM flags, no -ea)"
        echo "# dart: ${DART_VERSION:-not present} (generated codecs, zero runtime dependency; AOT executable)"
        echo "# elixir: ${ELIXIR_VERSION:-not present} (generated codecs, zero runtime dependency; pinned BEAM toolchain)"
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
        js)   PROV_JS="$resolved" ;;
    esac
}
# java/dart/elixir never appear here: their legs build from the repo's own
# generated sources with no runtime checkout, so there is nothing for the
# §3.5 guard to verify or misrecord.
if [ -n "$ONLY" ]; then
    case "$ONLY" in
        java|dart|elixir) ;;
        *) prov_verify "$ONLY" ;;
    esac
    for _lang in cpp c go rust cs js; do
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
        skip_leg c "generated/c is missing — run make first"
    elif [ ! -f "$SERIALIZE_C/serialize.c" ]; then
        skip_leg c "serialize.c not found at $SERIALIZE_C (set SERIALIZE_C)"
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
            skip_leg go "runner present but no go toolchain"
        fi
    else
        skip_leg go "runner not landed yet (bench/go/main.go — see bench/go/README.md)"
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
            # BENCH_OPT stamps the level into the runner's CSV opt column (the
            # cargo analogue of the C/C++ -DBENCH_OPT), and
            # CARGO_PROFILE_RELEASE_OPT_LEVEL is what actually BUILDS at that
            # level. Both, or the pair lies in one direction or the other: the
            # stamp without the profile names a level that was not built, and
            # the profile without the stamp builds a level nothing records.
            ( cd bench/rust && PATH="/opt/homebrew/opt/rustup/bin:$PATH" \
              BENCH_OPT="$OPT_LEVEL" \
              CARGO_PROFILE_RELEASE_OPT_LEVEL="${OPT_LEVEL#O}" \
              $PIN cargo run --release --quiet $RS_CARGO_ARGS -- $RUNNER_ARGS ) >> "$OUT"
        else
            skip_leg rust "runner present but no cargo"
        fi
    else
        skip_leg rust "runner not landed yet (bench/rust/Cargo.toml — see bench/rust/README.md)"
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
            skip_leg cs "runner present but no dotnet"
        fi
    else
        skip_leg cs "runner not landed yet (bench/cs/*.csproj — see bench/cs/README.md)"
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
            skip_leg js "runner present but no node"
        fi
    else
        skip_leg js "runner not landed yet (bench/js/main.mjs — see bench/README.md)"
    fi
fi

# ---- Java (generated codecs over the Bench corpus; no runtime checkout) ----
if [ -z "$ONLY" ] || [ "$ONLY" = java ]; then
    if [ -f bench/java/Main.java ]; then
        if [ -x "$JAVAC_BIN" ] || command -v "$JAVAC_BIN" >/dev/null 2>&1; then
            if [ "$REUSE" = 1 ] && [ -f build/bench/java/Main.class ]; then
                echo "== java: reusing build/bench/java ==" >&2
            else
                echo "== java: build ==" >&2
                mkdir -p build/bench/java
                "$JAVAC_BIN" --release 17 -Xlint:all -Werror -d build/bench/java \
                    generated/bench/java/*.java bench/java/Main.java
            fi
            echo "== java: run ==" >&2
            ( cd bench/java && $PIN "$JAVA_BIN" -cp ../../build/bench/java Main $RUNNER_ARGS ) >> "$OUT"
        else
            skip_leg java "runner present but no javac at $JAVAC_BIN (populate dist/ per the Makefile, or set JAVA/JAVAC)"
        fi
    else
        skip_leg java "runner not landed yet (bench/java/Main.java)"
    fi
fi

# ---- Dart (generated codecs over the Bench corpus; AOT is the timed form) ----
if [ -z "$ONLY" ] || [ "$ONLY" = dart ]; then
    if [ -f bench/dart/main.dart ]; then
        if [ -x "$DART_BIN" ] || command -v "$DART_BIN" >/dev/null 2>&1; then
            if [ "$REUSE" = 1 ] && [ -x build/bench/schema_bench_dart ]; then
                echo "== dart: reusing build/bench/schema_bench_dart ==" >&2
            else
                echo "== dart: build (AOT) ==" >&2
                "$DART_BIN" compile exe bench/dart/main.dart -o build/bench/schema_bench_dart >/dev/null
            fi
            echo "== dart: run ==" >&2
            ( cd bench/dart && $PIN ../../build/bench/schema_bench_dart $RUNNER_ARGS ) >> "$OUT"
        else
            skip_leg dart "runner present but no dart at $DART_BIN (populate dist/ per the Makefile, or set DART)"
        fi
    else
        skip_leg dart "runner not landed yet (bench/dart/main.dart)"
    fi
fi

# ---- Elixir (generated codecs over the Bench corpus; pinned BEAM toolchain) ----
if [ -z "$ONLY" ] || [ "$ONLY" = elixir ]; then
    if [ -f bench/elixir/main.exs ]; then
        if PATH="$BEAM_PATH:$PATH" command -v elixir >/dev/null 2>&1; then
            echo "== elixir: run ==" >&2
            ( cd bench/elixir && $PIN env PATH="$BEAM_PATH:$PATH" elixir main.exs $RUNNER_ARGS ) >> "$OUT"
        else
            skip_leg elixir "runner present but no elixir on $BEAM_PATH (populate dist/ per the Makefile, or set BEAM_PATH)"
        fi
    else
        skip_leg elixir "runner not landed yet (bench/elixir/main.exs)"
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

# ---- --quick: the headline table — one row per language over bench_mixed,
# in the owner's ruled form (#184, 2026-08-31: "I want nothing but a table
# with two columns: language, %", with generated C++ pinned at 100% as the
# DENOMINATOR, so a language beating C++ prints BELOW 100%). Everything
# else — ns/msg, family, checks, spreads — lives in the CSV only.
#
# The gen statistic is the ROUND_TRIP row (BENCH-STANDARD §2.9): the gen
# family's bench_mixed leg is data-driven in every language now (#191), its
# timed rows are write and round_trip, and the published blend is the
# round-trip. §2.9 records that the old (write+read)/2 blend is round_trip/2
# in form; the factor of two cancels in a ratio, so the percentages below are
# the same statistic either way.
#
# SINGLE-SUBJECT (#177): the headline is family gen — every language's
# schema-GENERATED code. The rt rows (the serialize runtime API called by
# hand) print as a second labeled section on their own unchanged write+read
# blend; the two subjects never rank against each other, which is exactly the
# refusal relative.go enforces on the CSVs.
#
# REFUSAL (#175, F4): a headline section with ZERO rows exits non-zero. The
# old blender dropped every row it did not recognise and printed an empty
# section at exit 0 — a leg that produced rows looked identical to a leg that
# produced none. ----
if [ "$QUICK" = 1 ]; then
    QUICK_STATUS=0
    {
        echo ""
        echo "quick mode — iteration instrument, not certification (bench_mixed only)"
        # the caption (#175): every printed comparison names what it holds
        # constant — the profiling doctrine's apples-to-apples rule
        echo "held constant: contract (BENCH-STANDARD §2.8 quick + §2.9 — bench_mixed, family gen round_trip over max rates), corpus (id per CSV row), machine ($HOST, $CPU), one sitting; the read-side sink deviations §2.7 named are gone — the round-trip observes its own decode (#191)"
        echo "NOT equalized: checks mode — recorded per row in the CSV; a cross-language checks ruling is deferred to the owner (#175)"
        awk -F, -v skips="$SKIP_NOTES" '
            # js emits two gen tiers; the flat tier is THE js path (codec
            # column, $18), so codec=runtime rows never enter the table
            $2 == "bench_mixed" && $13 == "gen" && $18 != "runtime" && $3 == "round_trip" { gt[$1] = 1e9 / $9 }
            $2 == "bench_mixed" && $13 == "rt" && $3 == "write" { rw[$1] = $9 }
            $2 == "bench_mixed" && $13 == "rt" && $3 == "read"  { rr[$1] = $9 }
            # ts[] is per-language time per message in ns; cpp is the
            # denominator, so cpp prints 100% and a faster language prints
            # below it.
            function render(ts, n, absent,    i, j, tt, tl, langs, m, sk, parts, ref) {
                i = 0
                for (lang in ts) { i++; langs[i] = lang }
                n = i
                for (i = 1; i <= n; i++)
                    for (j = i + 1; j <= n; j++)
                        if (ts[langs[j]] < ts[langs[i]]) {
                            tl = langs[i]; langs[i] = langs[j]; langs[j] = tl
                        }
                ref = ("cpp" in ts) ? ts["cpp"] : 0
                if (n == 0 && !absent) {
                    print "  (no rows in this run)"
                    return 0
                }
                printf "  %-10s %6s\n", "language", "%"
                for (i = 1; i <= n; i++) {
                    if (ref > 0)
                        printf "  %-10s %5.0f%%\n", langs[i], ts[langs[i]] / ref * 100.0
                    else
                        printf "  %-10s %6s\n", langs[i], "—"
                }
                if (ref <= 0 && n > 0)
                    print "  (no cpp row in this run: cpp is the 100% DENOMINATOR, so no percentage is defined — CSV carries the rates)"
                # loud skips (#175): a missing leg is an ABSENT row with its
                # reason, never a silently narrower table
                if (absent) {
                    m = split(skips, sk, ";")
                    for (i = 1; i <= m; i++) {
                        if (sk[i] == "") continue
                        split(sk[i], parts, "|")
                        printf "  %-10s %6s   ABSENT — %s\n", parts[1], "—", parts[2]
                    }
                }
                return n
            }
            END {
                print "subject: schema-GENERATED code (family gen) — what the compiler delivers; C++ = 100%"
                if (render(gt, 0, 1) == 0) {
                    print "REFUSED (#175/§2.9): the gen headline section has ZERO rows — no leg reported a bench_mixed family-gen round_trip row. Nothing was measured; printing an empty table at exit 0 is the defect this refusal exists to stop." > "/dev/stderr"
                    exit 3
                }
                print ""
                print "subject: hand-written runtime usage (family rt) — what the serialize libraries deliver by hand; never ranked against gen"
                for (lang in rw)
                    if (rr[lang] > 0 && rw[lang] > 0)
                        rt[lang] = (1.0 / rw[lang] + 1.0 / rr[lang]) / 2.0 * 1e9
                render(rt, 0, 0)
            }' "$OUT" || QUICK_STATUS=$?
    } >&2
    if [ "${QUICK_STATUS:-0}" -ne 0 ]; then
        echo "results: $OUT (headline REFUSED)" >&2
        exit 1
    fi
fi

# ---- #175: an --only run that produced zero data rows is a failure, not a
# quietly green empty CSV — the leg was skipped, not measured. Refuse. ----
DATA_ROWS="$(grep -c -E '^(c|cpp|go|rust|cs|js|java|dart|elixir),' "$OUT" 2>/dev/null || true)"
if [ -n "$ONLY" ] && [ "${DATA_ROWS:-0}" -eq 0 ]; then
    echo "REFUSED (#175): --only $ONLY produced ZERO rows — see the SKIP reason above; nothing was measured" >&2
    echo "results: $OUT (EMPTY — refused)" >&2
    exit 1
fi

echo "results: $OUT" >&2
