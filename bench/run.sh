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
#   --out FILE    results CSV (default bench/results/<date>-<arch>-<host>.csv;
#                 an inherited OUT env var is honored the same way). A FILE
#                 that already exists is REFUSED — never silently overwritten
#   --compiler    C++ compiler (default: $CXX, else c++)
#   --quick       the iteration instrument, never the certification
#                 instrument: every leg runs bench_mixed ONLY (3 measured
#                 runs, golden gate intact), and the driver prints the
#                 blended table — per-message time averaged over write and
#                 read, fastest language = 100% — after the CSV lands. The
#                 table is SINGLE-SUBJECT: family gen (generated code) for
#                 every language, family and checks mode printed per row
#                 under a caption naming what is held constant, and a leg
#                 whose toolchain is missing prints as an ABSENT row with
#                 the reason (#175).
#                 Scaling constants are PROPOSED in BENCH-STANDARD.md terms.
#   --only LANG   run a single language leg (c|cpp|go|rust|cs|js|java|dart|elixir|goose|lobster|luau|squirrel).
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
#   --render FILE print the ruled headline table from an existing results CSV
#                 and exit — no builds, no timed runs (the same table the
#                 --quick tail prints, from the same bench/render.awk)
#   --pgo         build the C and C++ legs two-pass clang PGO (#176): an
#                 instrumented -fprofile-instr-generate build, one --quick
#                 --bare profile run with LLVM_PROFILE_FILE set, an
#                 llvm-profdata merge, then a release rebuild with
#                 -fprofile-instr-use and -mcpu=native. The `# pgo:` preamble
#                 line records the divergence; the other legs are untouched.
#                 The recipe lives in the driver because COMMON_FLAGS resolves
#                 the SERIALIZE include path here — a hand build outside
#                 run.sh must set SERIALIZE or the generated headers cannot
#                 find serialize.h. LLVM_PROFDATA overrides the merge tool.
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
OUT="${OUT:-}"      # an inherited OUT env var is honored, same as --out
ONLY=""
ROUND=""
BARE=0
REUSE=0
INLINE=0
QUICK=0
PGO=0
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
        --pgo) PGO=1 ;;
        --render) RENDER="$2"; shift ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done
# ---- --render FILE: print the ruled headline table from an EXISTING results
# CSV and exit — no builds, no timed runs. The table logic is the same
# bench/render.awk the --quick tail uses; the caption reads the machine from
# the preamble of the file itself, so a rendered table always names the box
# that produced its rows. ----
if [ -n "${RENDER:-}" ]; then
    [ -r "$RENDER" ] || { echo "render: cannot read $RENDER" >&2; exit 1; }
    R_HOST="$(sed -n 's/^# host: \([^ ]*\).*/\1/p' "$RENDER" | head -1)"
    R_CPU="$(sed -n 's/^# cpu: \(.*\)/\1/p' "$RENDER" | head -1)"
    {
        echo ""
        echo "rendered from $RENDER"
        echo "held constant: contract (BENCH-STANDARD §2.9 — bench_mixed, family gen round_trip over max rates), corpus (id per CSV row), machine (${R_HOST:-unknown}, ${R_CPU:-unknown}), one sitting"
        awk -F, -v skips="" -f bench/render.awk "$RENDER"
    } >&2
    exit $?
fi

RUNNER_ARGS="--csv"
if [ -n "$ROUND" ]; then
    RUNNER_ARGS="--csv --round $ROUND"
fi
if [ "$QUICK" = 1 ]; then
    RUNNER_ARGS="$RUNNER_ARGS --quick"
fi
case "$ONLY" in
    ""|c|cpp|go|rust|cs|js|java|dart|elixir|goose|lobster|luau|squirrel) ;;
    *) echo "unknown --only language: $ONLY (c|cpp|go|rust|cs|js|java|dart|elixir|goose|lobster|luau|squirrel)" >&2; exit 1 ;;
esac

# §3.5 provenance mechanics: sets the SERIALIZE* defaults, materializes the
# override build configs when an env var points away from a default, and
# provides verify_runtime (the fail-closed check used below)
. bench/tools/runtime-paths.sh
runtime_paths_init
if [ "$ONLY" != squirrel ] && [ ! -f "$SERIALIZE/serialize.h" ]; then
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

# Refuse to overwrite an existing results file. The dated default makes
# same-day collisions silent, and a committed ledger CSV overwritten by a
# later sitting is exactly how a ledger lies — both boxes hit this on
# 2026-09-01 (#219). No override flag: pick a distinct --out, or remove
# the file deliberately.
if [ -e "$OUT" ]; then
    echo "refusing to overwrite existing results file: $OUT" >&2
    echo "pick a distinct --out (e.g. add a sitting tag), or remove the file first if the overwrite is deliberate" >&2
    exit 1
fi
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
# beam_pinned: true only when an executable named $1 sits in one of
# BEAM_PATH's OWN directories. The elixir leg REQUIRES the pin the way java
# and dart require their absolute $JAVAC_BIN and $DART_BIN: a `command -v`
# under PATH="$BEAM_PATH:$PATH" prepends rather than requires, so with dist/
# absent and a Homebrew or asdf elixir on the caller's PATH the leg ran on
# the system toolchain instead of skipping (#693).
beam_pinned() {
    local dir rest="$BEAM_PATH:"
    while [ -n "$rest" ]; do
        dir="${rest%%:*}"
        rest="${rest#*:}"
        if [ -n "$dir" ] && [ -x "$dir/$1" ]; then
            return 0
        fi
    done
    return 1
}
# The Luau leg (bench/luau/run): the pinned interpreter, LUAU overriding, exactly
# as make/luau.mk pins it. The generated unit carries its vendored
# serialize.luau beside it, so there is no runtime checkout to verify.
LUAU_BIN="${LUAU:-$PWD/dist/luau-0.741/luau}"
LUAU_VERSION=""
if [ -x "$LUAU_BIN" ]; then
    LUAU_VERSION="$LUAU_BIN (pin 0.741)"
fi
SQ_BIN="${SQ:-$PWD/dist/squirrel-3.2/bin/sq}"
SQ_VERSION=""
if [ -x "$SQ_BIN" ]; then
    SQ_VERSION="$SQ_BIN (pin 3.2)"
fi

# The Lobster leg (bench/lobster/run): the pinned toolchain, LOBSTER overriding,
# exactly as make/lobster.mk pins it. The generated unit carries its vendored
# serialize.lobster beside it, so there is no runtime checkout to verify.
LOBSTER_BIN="${LOBSTER:-$PWD/dist/lobster-v2026.8/bin/lobster}"
LOBSTER_VERSION=""
if [ -x "$LOBSTER_BIN" ]; then
    LOBSTER_VERSION="$LOBSTER_BIN (pin v2026.8)"
fi
# the version line records the pin, or "not present" — never a system elixir
# the leg would refuse to run on
ELIXIR_VERSION=""
if beam_pinned elixir && beam_pinned erl; then
    ELIXIR_VERSION="$(PATH="$BEAM_PATH:$PATH" elixir --short-version 2>/dev/null | head -1 || true)"
fi
# Goose (docs/PORT-GOOSE.md): the pinned compiler `make dist-goose` installs,
# or $GOOSE. Its runtime is vendored into the generated unit, so like the
# codegen-only legs there is no runtime checkout; the target is behind the
# schema_leg_goose tag, so the leg builds build/schema-goose itself.
GOOSE_BIN="${GOOSE:-$PWD/dist/goose-v2026.2-linux-x64/goose}"
GOOSE_VERSION="$("$GOOSE_BIN" version 2>/dev/null | head -1 || true)"

# Release flags: the schema repo's own flags (-std=c++17 -Wall -Wextra -Werror
# -ffp-contract=off) plus the serialize repo's Release bench configuration
# (-O3 -DNDEBUG -fno-rtti, SERIALIZE_RELEASE). Deliberate divergence from
# serialize's own bench: no -ffast-math — the schema repo pins wire
# determinism with -ffp-contract=off, and the generated quantize paths do
# real float math. Recorded here so the numbers carry their flags.
# -Igenerated/bench/cpp: the bench-corpus generated code (BenchWire.h), the
# only generated unit either runner compiles.
COMMON_FLAGS="-std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti -Igenerated/bench/cpp -I$SERIALIZE"

# g++ (13.3 checked) rejects two things in the GENERATED code that clang never
# flags: -Wclass-memaccess (branch zeroing memsets a non-trivial generated
# struct) and -Wtype-limits (read_int64 on a full-range unsigned field expands
# to unsigned < 0). Those are emitter findings, not bench findings — tracked
# for a fix; suppressed here so the bench builds on both compilers with
# -Werror otherwise live.
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
# -Igenerated/bench/c: the bench-corpus generated code (BenchWire.h), the
# only generated unit this runner compiles.
C_COMMON_FLAGS="-std=c99 -Wall -Wextra -Werror -Igenerated/bench/c -I$SERIALIZE_C"
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

# ---- two-pass PGO for the native-tier C/C++ legs (#176) ----
# The C and C++ legs compile the same word-codec shape; by default both build
# without PGO, which is one of the three named terms of the c/cpp tie. --pgo
# runs clang's profile-guided pipeline on both legs: instrument with
# -fprofile-instr-generate, one profile run, merge, rebuild with
# -fprofile-instr-use and -mcpu=native. The recipe lives in the driver because
# COMMON_FLAGS resolves the SERIALIZE include path here; a hand build outside
# run.sh has to set SERIALIZE (or pass -I) or the generated headers cannot
# find serialize.h.
PGO_NOTE="off"
PGO_PROFILE_DIR="build/bench/pgo"
PROF_DATA=""
if [ "$PGO" = 1 ]; then
    if [ -n "${LLVM_PROFDATA:-}" ]; then
        PROF_DATA="$LLVM_PROFDATA"
    elif command -v xcrun >/dev/null 2>&1 && xcrun --find llvm-profdata >/dev/null 2>&1; then
        PROF_DATA="xcrun llvm-profdata"
    elif command -v llvm-profdata >/dev/null 2>&1; then
        PROF_DATA="llvm-profdata"
    else
        echo "--pgo needs llvm-profdata (clang's -fprofile-instr-generate/-use; set LLVM_PROFDATA to its path)" >&2
        exit 1
    fi
    PGO_NOTE="two-pass clang PGO: -fprofile-instr-generate, one profile run, llvm-profdata merge, -fprofile-instr-use, -mcpu=native (c and cpp)"
fi

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
        echo "# pgo: $PGO_NOTE"
        echo "# go: ${GO_VERSION:-not present} (go run, default optimized build)"
        echo "# rust: ${RUST_VERSION:-not present} (cargo run --release at opt-level ${OPT_LEVEL#O}, no LTO)"
        echo "# dotnet: ${DOTNET_VERSION:-not present} (dotnet run -c Release, workstation GC)"
        echo "# node: ${NODE_VERSION:-not present} (NODE_ENV=production — serialize.js's caller-trust release mode; the runner records the mode that ran in its checks column)"
        echo "# java: ${JAVA_VERSION:-not present} (generated codecs, zero runtime dependency; default JVM flags, no -ea)"
        echo "# dart: ${DART_VERSION:-not present} (generated codecs, zero runtime dependency; AOT executable)"
        echo "# elixir: ${ELIXIR_VERSION:-not present} (generated codecs, zero runtime dependency; pinned BEAM toolchain)"
        echo "# goose: ${GOOSE_VERSION:-not present} (generated codec over vendored serialize.goose; goose build -O2 with the host C compiler, one translation unit)"
        echo "# lobster: ${LOBSTER_VERSION:-not present} (generated codec + vendored serialize.lobster; default JIT, --runtime-no-asserts; the --cpp row is owed, schema-lobster-bench-cpp)"
        echo "# luau: ${LUAU_VERSION:-not present} (generated codec + vendored serialize.luau; the interpreter, native codegen OFF; iters rescaled for the interpreter, recorded per row)"
        echo "# squirrel: ${SQ_VERSION:-not present} (generated codec + vendored serialize.nut; Squirrel 3.2 64-bit interpreter; iters rescaled for the interpreter, recorded per row)"
        echo "# pinning: $PIN_DESC"
        echo "# noise: ${BENCH_NOISE:-unlabelled}"
        # the schema commit carries its BRANCH, exactly as every runtime line
        # below does. A sweep that silently measured the wrong tree is a
        # failure mode this repo has already had, and a CSV that records only
        # a SHA cannot say which tree produced it once the SHA is rebased away.
        echo "# schema commit: $(commit_of .)"
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
    local sha branch dirty
    sha="$(git -C "$1" rev-parse --short HEAD 2>/dev/null || echo unknown)"
    branch="$(git -C "$1" rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
    # -dirty when the tree differs from HEAD (tracked changes only): a sweep
    # of uncommitted work must SAY so, or the archaeology of an experiment
    # run reconstructs the wrong configuration from a clean-looking sha
    dirty=""
    if [ "$sha" != "unknown" ] && ! git -C "$1" diff-index --quiet HEAD -- 2>/dev/null; then
        dirty="-dirty"
    fi
    echo "$sha$dirty ($branch)"
}

# ---- §3.5 provenance guard: verify BEFORE any row or preamble exists ----
# Every "# serialize* commit:" line the preamble prints must be proven
# against the toolchain's own resolution (bench/tools/runtime-paths.sh), and
# a mismatch refuses the whole run — exit non-zero, no rows, $OUT untouched.
# --bare emits rows with no preamble, so it verifies the leg it will run:
# those rows land under a preamble that makes the same claim.
prov_verify() {
    if [ "$1" = squirrel ]; then
        echo "squirrel: generated codecs, no runtime checkout — nothing for the §3.5 guard to verify" >&2
        return 0
    fi
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
# java/dart/elixir set no PROV_ line here: their legs build from the repo's
# own generated sources with no runtime checkout, so there is nothing for the
# §3.5 guard to verify or misrecord. The exemption itself lives in
# verify_runtime (bench/tools/runtime-paths.sh), which answers "nothing to
# verify" for them — so this guard and the standalone gate pass-driver.sh
# runs before its first leg cannot drift apart, and neither repeats the list.
if [ -n "$ONLY" ]; then
    prov_verify "$ONLY"
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
    elif [ "$PGO" = 1 ]; then
        mkdir -p "$PGO_PROFILE_DIR"
        rm -f "$PGO_PROFILE_DIR/cpp.profraw" "$PGO_PROFILE_DIR/cpp.profdata"
        echo "== cpp: pgo pass 1 (instrumented build) ==" >&2
        $CXX_BIN $RELEASE_FLAGS -fprofile-instr-generate bench/cpp/bench_main.cpp -o build/bench/schema_bench_cpp_pgo
        echo "== cpp: pgo profile run ==" >&2
        LLVM_PROFILE_FILE="$PGO_PROFILE_DIR/cpp.profraw" $PIN ./build/bench/schema_bench_cpp_pgo --csv --quick >/dev/null
        echo "== cpp: pgo merge ==" >&2
        $PROF_DATA merge -output="$PGO_PROFILE_DIR/cpp.profdata" "$PGO_PROFILE_DIR/cpp.profraw"
        echo "== cpp: build (Release, PGO) ==" >&2
        $CXX_BIN $RELEASE_FLAGS -fprofile-instr-use="$PGO_PROFILE_DIR/cpp.profdata" -mcpu=native bench/cpp/bench_main.cpp -o build/bench/schema_bench_cpp
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
    if [ ! -f generated/bench/c/BenchWire.h ]; then
        skip_leg c "generated/bench/c is missing — run make first"
    elif [ ! -f "$SERIALIZE_C/serialize.c" ]; then
        skip_leg c "serialize.c not found at $SERIALIZE_C (set SERIALIZE_C)"
    else
        if [ "$REUSE" = 1 ] && [ -x build/bench/schema_bench_c ]; then
            echo "== c: reusing build/bench/schema_bench_c ==" >&2
        elif [ "$PGO" = 1 ]; then
            mkdir -p "$PGO_PROFILE_DIR"
            rm -f "$PGO_PROFILE_DIR/c.profraw" "$PGO_PROFILE_DIR/c.profdata"
            echo "== c: pgo pass 1 (instrumented build) ==" >&2
            $CC_BIN $C_RELEASE_FLAGS -fprofile-instr-generate bench/c/bench_main.c "$SERIALIZE_C/serialize.c" -o build/bench/schema_bench_c_pgo -lm
            echo "== c: pgo profile run ==" >&2
            LLVM_PROFILE_FILE="$PGO_PROFILE_DIR/c.profraw" $PIN ./build/bench/schema_bench_c_pgo --csv --quick >/dev/null
            echo "== c: pgo merge ==" >&2
            $PROF_DATA merge -output="$PGO_PROFILE_DIR/c.profdata" "$PGO_PROFILE_DIR/c.profraw"
            echo "== c: build (Release, PGO) ==" >&2
            $CC_BIN $C_RELEASE_FLAGS -fprofile-instr-use="$PGO_PROFILE_DIR/c.profdata" -mcpu=native bench/c/bench_main.c "$SERIALIZE_C/serialize.c" -o build/bench/schema_bench_c -lm
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
        # both pinned binaries, tested on BEAM_PATH alone (beam_pinned above);
        # the run still prepends, because the elixir launcher is a shell
        # script that needs the system's coreutils beside the pinned erl
        if beam_pinned elixir && beam_pinned erl; then
            echo "== elixir: run ==" >&2
            ( cd bench/elixir && $PIN env PATH="$BEAM_PATH:$PATH" elixir main.exs $RUNNER_ARGS ) >> "$OUT"
        else
            skip_leg elixir "runner present but no pinned elixir+erl on $BEAM_PATH (populate dist/ per the Makefile, or set BEAM_PATH)"
        fi
    else
        skip_leg elixir "runner not landed yet (bench/elixir/main.exs)"
    fi
fi

# ---- Goose (generated codec over the Bench corpus; goose build -O2) ----
if [ -z "$ONLY" ] || [ "$ONLY" = goose ]; then
    if [ -f bench/goose/main.goose ]; then
        if [ -x "$GOOSE_BIN" ]; then
            if [ "$REUSE" = 1 ] && [ -x build/bench/schema_bench_goose ]; then
                echo "== goose: reusing build/bench/schema_bench_goose ==" >&2
            else
                echo "== goose: build (goose build -O2) ==" >&2
                go build -tags schema_leg_goose -o build/schema-goose ./cmd/schema
                rm -rf build/bench/goose-src && mkdir -p build/bench/goose-src
                ./build/schema-goose generate --lang goose --out build/bench/goose-src bench/corpus/Bench.schema
                cp bench/goose/main.goose build/bench/goose-src/
                "$GOOSE_BIN" build -O2 build/bench/goose-src/main.goose -o build/bench/schema_bench_goose >&2
            fi
            echo "== goose: run ==" >&2
            ( cd bench/goose && $PIN ../../build/bench/schema_bench_goose $RUNNER_ARGS ) >> "$OUT"
        else
            skip_leg goose "runner present but no goose at $GOOSE_BIN (make dist-goose, or set GOOSE)"
        fi
    else
        skip_leg goose "runner not landed yet (bench/goose/main.goose)"
    fi
fi

# ---- Lobster (generated codec + its vendored serialize.lobster; pinned toolchain, JIT) ----
if [ -z "$ONLY" ] || [ "$ONLY" = lobster ]; then
    if [ -f bench/lobster/main.lobster ]; then
        if [ -x "$LOBSTER_BIN" ]; then
            echo "== lobster: run ==" >&2
            $PIN env LOBSTER="$LOBSTER_BIN" bench/lobster/run $RUNNER_ARGS >> "$OUT"
        else
            skip_leg lobster "runner present but no lobster at $LOBSTER_BIN (make dist-lobster, or set LOBSTER)"
        fi
    else
        skip_leg lobster "runner not landed yet (bench/lobster/main.lobster)"
    fi
fi

# ---- Luau (generated codec + its vendored serialize.luau; pinned interpreter, native codegen off) ----
if [ -z "$ONLY" ] || [ "$ONLY" = luau ]; then
    if [ -f bench/luau/main.luau ]; then
        if [ -x "$LUAU_BIN" ]; then
            echo "== luau: run ==" >&2
            $PIN env LUAU="$LUAU_BIN" bench/luau/run $RUNNER_ARGS >> "$OUT"
        else
            skip_leg luau "runner present but no luau at $LUAU_BIN (make dist-luau, or set LUAU)"
        fi
    else
        skip_leg luau "runner not landed yet (bench/luau/main.luau)"
    fi
fi

# ---- Squirrel (generated codec + its vendored serialize.nut; Squirrel 3.2 64-bit interpreter) ----
if [ -z "$ONLY" ] || [ "$ONLY" = squirrel ]; then
    if [ -x "$SQ_BIN" ]; then
        if [ "$REUSE" = 1 ] && [ -f build/bench/squirrel/main.nut ] && [ -f build/bench/squirrel/Bench.nut ]; then
            echo "== squirrel: reusing build/bench/squirrel ==" >&2
        else
            echo "== squirrel: build ==" >&2
            go build -o build/schema-squirrel ./cmd/schema
            rm -rf build/bench/squirrel && mkdir -p build/bench/squirrel
            ./build/schema-squirrel generate --lang squirrel --out build/bench/squirrel bench/corpus/Bench.schema
            cat > build/bench/squirrel/main.nut << 'SQEOF'
local serialize = dofile("internal/codegen/squirrel/serialize/serialize.nut", true);
local Generated = dofile("build/bench/squirrel/schema_module.nut", true).require("Bench");

local NUM_VARIANTS = 64;
local MIXED_ITERS = 320;
local QUICK_MIXED_ITERS = 128;
local MAX_ITERATIONS = 2147483584;
local CSV_SUFFIX = "gen,pkg,always,default,unknown";

local csv = false;
local gate = false;
local quick = false;
local roundMode = false;
local iterations = 0;
local wireDir = "testdata/wire";
local variantDir = "bench/corpus/variants";
local bad = "";

local i = 0;
while (i < vargv.len()) {
  local a = vargv[i];
  if (a == "--csv") csv = true;
  else if (a == "--gate") gate = true;
  else if (a == "--quick") quick = true;
  else if (a == "--round" && i + 1 < vargv.len()) {
    i++;
    roundMode = true;
  } else if (a == "--iterations" && i + 1 < vargv.len()) {
    i++;
    local n = vargv[i].tointeger();
    if (n <= 0 || n > MAX_ITERATIONS || (n % NUM_VARIANTS) != 0) {
      bad = "--iterations requires a positive multiple of 64";
    } else {
      iterations = n;
    }
  } else if (a == "--wire-dir" && i + 1 < vargv.len()) {
    i++;
    wireDir = vargv[i];
  } else if (a == "--variant-dir" && i + 1 < vargv.len()) {
    i++;
    variantDir = vargv[i];
  } else {
    bad = "unknown option: " + a;
  }
  i++;
}

local log = function(s) { print("log " + s + "\n"); };
if (bad != "") {
  log(bad);
  throw bad;
}

local readBytes = function(path) {
  local f = file(path, "rb");
  local b = f.len() == 0 ? blob(0) : f.readblob(f.len());
  f.close();
  return b;
};

local sameBlob = function(a, b, n) {
  if (a.len() < n || b.len() < n) return false;
  for (local j = 0; j < n; j++) {
    if (a[j] != b[j]) return false;
  }
  return true;
};

local FNV_OFFSET = (0xcbf29ce4 << 32) | 0x84222325;
local FNV_PRIME = (0x100 << 32) | 0x000001b3;
local HEX = "0123456789abcdef";
local fnvString = function(h, s) {
  for (local j = 0; j < s.len(); j++) h = (h ^ (s[j] & 0xff)) * FNV_PRIME;
  return h;
};
local fnvBlob = function(h, b) {
  for (local j = 0; j < b.len(); j++) h = (h ^ (b[j] & 0xff)) * FNV_PRIME;
  return h;
};
local hex64 = function(h) {
  local s = "";
  for (local shift = 60; shift >= 0; shift -= 4) {
    s += format("%c", HEX[(h >> shift) & 15]);
  }
  return s;
};

local sortFloats = function(arr) {
  local a = clone arr;
  a.sort();
  return a;
};

local stats = function(rates) {
  local s = sortFloats(rates);
  local n = s.len();
  local median = s[n / 2];
  local lo = s[0];
  local hi = s[n - 1];
  local spread = (hi - lo) / median * 100.0;
  return { median = median, lo = lo, hi = hi, spread = spread };
};

local row = "bench_mixed";
local golden = readBytes(wireDir + "/" + row + ".bin");
local packed = readBytes(variantDir + "/" + row + ".variants.bin");

local h = FNV_OFFSET;
h = fnvString(h, row + ".bin");
h = (h ^ 0) * FNV_PRIME;
h = fnvBlob(h, golden);
h = fnvString(h, row + ".variants.bin");
h = (h ^ 0) * FNV_PRIME;
h = fnvBlob(h, packed);
local cid = hex64(h);

if (packed.len() == 0 || (packed.len() % NUM_VARIANTS) != 0) {
  log("FAILED: " + row + ": bad variant stride");
  throw "bench failed";
}
local record = packed.len() / NUM_VARIANTS;
local bufs = array(NUM_VARIANTS);
for (local k = 0; k < NUM_VARIANTS; k++) {
  packed.seek(k * record);
  bufs[k] = packed.readblob(record);
}

if (golden.len() != record || !sameBlob(bufs[0], golden, record)) {
  log("FAILED: " + row + ": variant 0 != wire golden");
  throw "bench failed";
}

local root = "Bench" + "Mixed";
local newValue = Generated[root];
local writeValue = Generated["Write" + root];
local readValue = Generated["Read" + root];

local cap = (record / 8 + 2) * 8;
local buf = blob(cap);
local ws = serialize.WriteStream(buf);
local rs = serialize.ReadStream(bufs[0]);

local writeOne = function(v) {
  ws.reset(buf);
  if (!writeValue(ws, v)) return -1;
  ws.flush();
  return ws.bytesProcessed();
};

local readOne = function(v, k) {
  rs.reset(bufs[k]);
  return readValue(rs, v);
};

local instances = array(NUM_VARIANTS);
for (local k = 0; k < NUM_VARIANTS; k++) {
  local v = newValue();
  if (!readOne(v, k)) {
    log("FAILED: " + row + ": decode of variant " + k + " failed");
    throw "bench failed";
  }
  local n = writeOne(v);
  if (n != record || !sameBlob(buf, bufs[k], record)) {
    log("FAILED: " + row + ": variant " + k + " round-trip bytes differ");
    throw "bench failed";
  }
  instances[k] = v;
}

local out = newValue();
for (local m = 0; m < 2 * NUM_VARIANTS; m++) {
  local k = m & (NUM_VARIANTS - 1);
  if (!readOne(out, k)) {
    log("FAILED: " + row + ": decode into reused holder failed");
    throw "bench failed";
  }
  local n = writeOne(out);
  if (n != record || !sameBlob(buf, bufs[k], record)) {
    log("FAILED: " + row + ": reused holder round-trip bytes differ");
    throw "bench failed";
  }
}

log("schema bench (squirrel, generated codec, Squirrel 3.2 interpreter" + (quick ? ", --quick: iteration instrument, not certification" : "") + ")");

local sink = 0;
local csvRows = [];
if (!gate) {
  local iters = iterations > 0 ? iterations : (quick ? QUICK_MIXED_ITERS : MIXED_ITERS);
  local numRuns = roundMode ? 1 : (quick ? 3 : 7);

  local writeRates = [];
  for (local run = 0; run <= numRuns; run++) {
    local t0 = clock();
    local acc = 0;
    for (local m = 0; m < iters; m++) {
      local n = writeOne(instances[m & (NUM_VARIANTS - 1)]);
      if (n != record) throw "write failed in loop";
      acc += n;
    }
    local elapsed = clock() - t0;
    sink += acc;
    if (run > 0) writeRates.append(iters.tofloat() / elapsed);
  }

  local tripRates = [];
  for (local run = 0; run <= numRuns; run++) {
    local t0 = clock();
    local acc = 0;
    for (local m = 0; m < iters; m++) {
      local k = m & (NUM_VARIANTS - 1);
      if (!readOne(out, k)) throw "decode failed in loop";
      local n = writeOne(out);
      if (n != record) throw "re-encode failed in loop";
      acc += n;
    }
    local elapsed = clock() - t0;
    sink += acc;
    if (run > 0) tripRates.append(iters.tofloat() / elapsed);
  }

  local reportRow = function(path, rates) {
    local st = stats(rates);
    local mbps = st.median * record / (1024.0 * 1024.0);
    log(format("%-18s %-11s%10.4f M msg/s %10.2f MB/s   (min %.4f, max %.4f, spread %.1f%%)", row, path, st.median / 1e6, mbps, st.lo / 1e6, st.hi / 1e6, st.spread));
    csvRows.append(format("squirrel,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f,%s,%s", row, path, iters, record, rates.len(), st.median, st.lo, st.hi, mbps, st.spread, cid, CSV_SUFFIX));
  };

  reportRow("write", writeRates);
  reportRow("round_trip", tripRates);
  local wst = stats(writeRates);
  local rtst = stats(tripRates);
  local readTime = 1.0 / rtst.median - 1.0 / wst.median;
  if (readTime > 0.0) {
    log(format("%-18s %-11s%10.4f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)", row, "read", 1e-6 / readTime));
  }
}

if (csv && !gate) {
  print("csv lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n");
  foreach (r in csvRows) {
    print("csv " + r + "\n");
  }
}
log("OK (corpus_id " + cid + ")");
log("sink: " + sink);
SQEOF
        fi
        echo "== squirrel: run ==" >&2
        SQ_OUT="build/bench/squirrel/out.txt"
        $PIN "$SQ_BIN" build/bench/squirrel/main.nut $RUNNER_ARGS > "$SQ_OUT"
        while IFS= read -r line || [ -n "$line" ]; do
            case "$line" in
                "csv "*) printf '%s\n' "${line#csv }" >> "$OUT" ;;
                "log "*) printf '%s\n' "${line#log }" >&2 ;;
                *) printf 'squirrel leg REFUSES an untagged line: %s\n' "$line" >&2; exit 1 ;;
            esac
        done < "$SQ_OUT"
    else
        skip_leg squirrel "no sq at $SQ_BIN (make dist-squirrel, or set SQ)"
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
# schema-GENERATED code, the estate's one benchmark subject (schema#196).
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
        awk -F, -v skips="$SKIP_NOTES" -f bench/render.awk "$OUT" || QUICK_STATUS=$?
    } >&2
    if [ "${QUICK_STATUS:-0}" -ne 0 ]; then
        echo "results: $OUT (headline REFUSED)" >&2
        exit 1
    fi
fi

# ---- #175: an --only run that produced zero data rows is a failure, not a
# quietly green empty CSV — the leg was skipped, not measured. Refuse. ----
DATA_ROWS="$(grep -c -E '^(c|cpp|go|rust|cs|js|java|dart|elixir|goose|lobster|luau|squirrel),' "$OUT" 2>/dev/null || true)"
if [ -n "$ONLY" ] && [ "${DATA_ROWS:-0}" -eq 0 ]; then
    echo "REFUSED (#175): --only $ONLY produced ZERO rows — see the SKIP reason above; nothing was measured" >&2
    echo "results: $OUT (EMPTY — refused)" >&2
    exit 1
fi

echo "results: $OUT" >&2
