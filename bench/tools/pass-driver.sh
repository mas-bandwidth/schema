#!/bin/bash
# pass-driver.sh — the interleaved measurement pass (BENCH-STANDARD.md §2).
#
# Replaces the one-off epyc-pass-driver.sh: no hardcoded ROOT, runs on both
# the EPYC box (Linux, pinned core, quiet-window discipline) and the M2
# laptop (Darwin, unpinned by construction — §7). The EPYC driver's pscheck,
# quiet-window wait and /proc/stat snapshot live here now.
#
# What a pass is:
#   1. control leg A — the C++ family gen runner, full 7-run mode (§2.6)
#   2. rounds 0..N-1 — every available language once per round, one warmup +
#      one measured run per benchmark (--round K, §2.4), so every leg sees
#      the same load window; a spike hits every language equally instead of
#      masquerading as one language being slow. Every cpp round leg,
#      INCLUDING round 0, passes --reuse-build: control leg A just built the
#      binary, and the "literally the same executable" claim below is only
#      true because no leg in between rebuilds it.
#   3. control leg B — the same C++ binary again (--reuse-build: literally
#      the same executable — same file, same inode — as control leg A ran,
#      same compiler, same flags)
#   4. the driver (not the runner) aggregates max/median/min/spread across
#      rounds, captures load automatically (§2.5 — never operator-typed),
#      computes control_delta_pct and stamps # window: OK|INVALID (§2.6).
#      The tool refuses to publish ratios from an INVALID window.
#
# usage: bench/tools/pass-driver.sh [--out FILE] [--rounds N] [--langs a,b,c]
#                                   [--compiler CXX] [--inline] [--twins]
#   --out FILE      final pass CSV (default bench/results/<date>-<arch>-<host>-pass.csv)
#   --rounds N      measured rounds (default 7, §2.1/§2.4)
#   --langs a,b,c   subset of cpp,c,go,rust,cs (default: all; unavailable
#                   toolchains are skipped and recorded, a skipped leg is a
#                   fact not a failure)
#   --compiler CXX  C++ compiler for the cpp legs and control legs
#   --inline        run the §4.1 inline verdict pass per language afterwards
#                   and backfill the inline column (bench/tools/inline-verdict.sh)
#   --twins         §2.6.1 A/A twin legs: every language leg runs TWICE per
#                   round — the same binary, same file, same inode (--reuse-build
#                   guarantees no rebuild between twins), at alternating
#                   positions in the round order (even rounds a-then-b, odd
#                   rounds b-then-a). Twin-b legs are stamped as rounds
#                   N..2N-1 so aggregation stays mechanical; the twin gate
#                   (bench/tools twingate) compares the two aggregates per
#                   row and marks any row whose twin ratio departs 1.0 beyond
#                   its spread band as state-suspect — the rel tool refuses
#                   to ratio such a row, with the §2.6.1 caption. A pass
#                   introducing a NEW binary configuration MUST run --twins.
#
# environment: SERIALIZE / SERIALIZE_C / SERIALIZE_GO / SERIALIZE_RS /
# SERIALIZE_CS, BENCH_CPU, BENCH_NOISE, BENCH_OPT_LEVEL — all as bench/run.sh.
# The SERIALIZE* paths are §3.5-verified against each toolchain's own
# resolution before the first leg runs, and the pass refuses on mismatch.
set -u

cd "$(dirname "$0")/../.."      # repo root

OUT=""
ROUNDS=7
LANGS="cpp,c,go,rust,cs"
COMPILER="${CXX:-c++}"
INLINE=0
TWINS=0
while [ $# -gt 0 ]; do
    case "$1" in
        --out) OUT="$2"; shift ;;
        --rounds) ROUNDS="$2"; shift ;;
        --langs) LANGS="$2"; shift ;;
        --compiler) COMPILER="$2"; shift ;;
        --inline) INLINE=1 ;;
        --twins) TWINS=1 ;;
        *) echo "unknown argument: $1" >&2; exit 1 ;;
    esac
    shift
done

# The driver's own instruments (controlmedian, aggregate, twingate) are the
# bench/tools Go program. Where the host go toolchain cannot build them —
# the EPYC box carries go 1.22.2 against go.mod's 1.26 — BENCH_TOOLS names a
# prebuilt bench/tools binary (cross-compiled from the SAME schema commit;
# the pass preamble records that commit) and the driver uses it verbatim.
TOOLS="${BENCH_TOOLS:-go run ./bench/tools}"
if [ -n "${BENCH_TOOLS:-}" ]; then
    [ -x "$BENCH_TOOLS" ] || { echo "BENCH_TOOLS=$BENCH_TOOLS is not an executable" >&2; exit 1; }
else
    command -v go >/dev/null 2>&1 || { echo "pass-driver needs the go toolchain (bench/tools), or BENCH_TOOLS=/path/to/prebuilt-bench-tools" >&2; exit 1; }
fi

# ---- §3.5 provenance gate, before ANY leg runs: every runtime path the
# final preamble will record must match what the toolchains actually resolve
# (run.sh re-checks per leg; this refusal is cheap and saves a night).
# Verify only the languages this pass will RUN (--langs): the gate refusing a
# pass over a language it was told to skip inverted its own contract — a
# skipped leg is a fact, not a failure (measured on the EPYC box: go 1.22.2
# present, serialize.go absent, cpp/c pass refused). ----
if ! bench/tools/verify-runtime-paths.sh $(echo "$LANGS" | tr ',' ' ') >&2; then
    echo "REFUSED (§3.5): a leg's build would not use the runtime path the preamble records — no pass, no rows" >&2
    exit 1
fi

# dotnet must not leave build servers running between legs: the Roslyn
# compiler server (VBCSCompiler) and MSBuild nodes linger by default, look
# like "ours" to the quiet-window gate, and stall pscheck for 30 minutes —
# observed on the M2, and exactly why the EPYC driver carried these.
export DOTNET_CLI_TELEMETRY_OPTOUT=1
export DOTNET_CLI_USE_MSBUILD_SERVER=0
export MSBUILDDISABLENODEREUSE=1

ARCH="$(uname -m)"
HOST="$(hostname -s)"
OS="$(uname -s)"
OUT="${OUT:-bench/results/$(date +%F)-$ARCH-$HOST-pass.csv}"
W="build/bench/pass-$(date +%Y%m%d-%H%M%S)"
LOG="$W/pass.log"
mkdir -p "$W" bench/results

log() { echo "$*" >> "$LOG"; }

# ---- load capture (§2.5): automatic, never operator-typed ----

loadavg() {
    case "$OS" in
        Darwin) sysctl -n vm.loadavg | tr -d '{}' | awk '{$1=$1; print}' ;;
        *) cut -d' ' -f1-3 /proc/loadavg ;;
    esac
}

# count of non-bench processes above 5% cpu (both platforms). "Ours" is the
# benchmark tooling itself, which is allowed to be busy.
foreign_procs() {
    ps -A -o pcpu=,comm= 2>/dev/null | awk '
        {
            n = split($2, parts, "/"); name = parts[n]
            if ($1 + 0 > 5 &&
                name !~ /^(schema_bench|benchgo|benchrust|schemabench|go|gopls|cargo|rustc|cc1|cc1plus|clang|clang\+\+|c\+\+|cc|gcc|g\+\+|dotnet|ld|pass-driver\.sh|run\.sh)/)
                count++
        }
        END { print count + 0 }'
}

# linux only: the pinned core's /proc/stat line (100Hz ticks)
core_stat() {
    [ "$OS" = Linux ] && grep "^cpu${BENCH_CPU:-0} " /proc/stat || true
}

# ---- the quiet-window gate (ported from the EPYC driver) ----

# benchmark tooling that must NOT be running between legs
ours() {
    ps -A -o pid=,pcpu=,etime=,comm= 2>/dev/null \
      | grep -E "schema_bench|benchgo|benchrust|schemabench|cargo|rustc|cc1plus|clang\+\+|dotnet|bench_main" \
      | grep -vE "grep|pass-driver|VBCSCompiler|MSBuild" || true
}

pscheck() {
    log "--- pscheck [$1] $(date -u +%FT%TZ) load=$(loadavg)"
    local b i
    b="$(ours)"
    if [ -n "$b" ]; then
        log "  WAITING: ours still running:"; log "$b"
        i=0
        while [ -n "$(ours)" ] && [ $i -lt 180 ]; do sleep 10; i=$((i+1)); done
        if [ -n "$(ours)" ]; then log "  WARNING: never went quiet after 30m; proceeding, flagged"; fi
    else
        log "  clean: none of ours running"
    fi
}

# ---- leg runner ----

FAILED_LEGS=""
leg() {
    local name="$1"; shift
    pscheck "before-$name"
    log "=== leg $name START $(date -u +%FT%TZ) (bench/run.sh $*)"
    if bench/run.sh "$@" >> "$LOG" 2>&1; then
        log "=== leg $name OK $(date -u +%FT%TZ)"
    else
        log "=== leg $name FAILED $(date -u +%FT%TZ)"
        touch "$W/FAILED-$name"
        FAILED_LEGS="$FAILED_LEGS $name"
    fi
    case "$name" in
        *-cs) dotnet build-server shutdown >> "$LOG" 2>&1 || true ;;
    esac
}

# ---- pass start ----

log "===== interleaved pass START $(date -u +%FT%TZ) host=$HOST arch=$ARCH os=$OS rounds=$ROUNDS langs=$LANGS ====="
LOAD_START="$(loadavg)"
LOAD_MAX="$(loadavg | awk '{print $1}')"
FOREIGN_MAX="$(foreign_procs)"
CORE_STAT_START="$(core_stat)"
[ -n "$CORE_STAT_START" ] && log "cpu${BENCH_CPU:-0} pre : $CORE_STAT_START"

sample_boundary() {
    local one fg
    one="$(loadavg | awk '{print $1}')"
    LOAD_MAX="$(printf '%s\n%s\n' "$LOAD_MAX" "$one" | sort -rn | head -1)"
    fg="$(foreign_procs)"
    [ "$fg" -gt "$FOREIGN_MAX" ] && FOREIGN_MAX="$fg"
    log "boundary: load1=$one foreign=$fg"
}

# ---- control leg A (§2.6): full 7-run C++ gen, fresh build, preamble source ----

leg control-start --only cpp --compiler "$COMPILER" --out "$W/control-start.csv"
if [ ! -s "$W/control-start.csv" ] || ! grep -q '^cpp,' "$W/control-start.csv"; then
    echo "control leg A produced no rows — see $LOG" >&2
    exit 1
fi
sample_boundary

# ---- rounds 0..N-1 (§2.4): every available language once per round ----

AVAILABLE=""
SKIPPED=""
# one_leg <K> <lang> <twin a|b>: run one round leg. Twin b legs are the SAME
# binary (--reuse-build always; twin a of round 0 — or control leg A for
# cpp — built it) stamped as round K+ROUNDS so §2.4 duplicate detection
# stays mechanical while the twin aggregates stay separable.
one_leg() {
    local K="$1" lang="$2" twin="$3" SUF="" STAMP="$K" REUSE_FLAG=""
    if [ "$twin" = b ]; then
        SUF="-b"; STAMP=$((K + ROUNDS)); REUSE_FLAG="--reuse-build"
        # twin b never runs before twin a has built (round 0 is a-first)
        echo " $AVAILABLE " | grep -q " $lang " || return 0
    else
        [ "$K" -gt 0 ] && REUSE_FLAG="--reuse-build"
        # cpp reuses from ROUND 0 as well: control leg A just built this
        # exact binary with the same compiler and flags, and control leg B's
        # "literally the same executable" is only true if no leg in between
        # rebuilds it. (Round 0 still builds every OTHER language — the
        # control legs build only cpp.)
        [ "$lang" = cpp ] && REUSE_FLAG="--reuse-build"
        if [ "$K" -gt 0 ] && ! echo " $AVAILABLE " | grep -q " $lang "; then
            return 0        # went missing in round 0; recorded below
        fi
    fi
    leg "round-$K-$lang$SUF" --only "$lang" --compiler "$COMPILER" --bare \
        --round "$K" $REUSE_FLAG --out "$W/round-$K-$lang$SUF.csv"
    if [ "$K" -eq 0 ] && [ "$twin" = a ]; then
        if grep -q "^$lang," "$W/round-0-$lang.csv" 2>/dev/null; then
            AVAILABLE="$AVAILABLE $lang"
        else
            SKIPPED="$SKIPPED $lang"
            log "SKIP $lang: round 0 produced no rows (toolchain or runner missing — see above)"
            rm -f "$W/round-0-$lang.csv" "$W/FAILED-round-0-$lang"
            FAILED_LEGS="$(echo "$FAILED_LEGS" | sed "s/ round-0-$lang//")"
        fi
    fi
    # §2.4 provenance: stamp the round into the per-round CSV so
    # duplicate-round detection is mechanical — aggregate refuses a row
    # contributed twice for one round, and refuses the same file twice.
    if [ -f "$W/round-$K-$lang$SUF.csv" ]; then
        { echo "# round: $STAMP"; cat "$W/round-$K-$lang$SUF.csv"; } > "$W/round-$K-$lang$SUF.csv.tmp" \
            && mv "$W/round-$K-$lang$SUF.csv.tmp" "$W/round-$K-$lang$SUF.csv"
    fi
}
for K in $(seq 0 $((ROUNDS - 1))); do
    # §2.6.1: twins alternate positions in the round order — even rounds run
    # a-legs first, odd rounds b-legs first (round 0 is always a-first so
    # every binary exists before its twin reuses it).
    TWIN_ORDER="a"
    if [ "$TWINS" = 1 ]; then
        TWIN_ORDER="a b"
        [ $((K % 2)) -eq 1 ] && TWIN_ORDER="b a"
    fi
    for twin in $TWIN_ORDER; do
        for lang in $(echo "$LANGS" | tr ',' ' '); do
            one_leg "$K" "$lang" "$twin"
        done
    done
    sample_boundary
done
if [ -z "$(echo "$AVAILABLE" | tr -d ' ')" ]; then
    echo "no language produced rows — see $LOG" >&2
    exit 1
fi

# ---- control leg B (§2.6): the SAME C++ binary again ----

leg control-end --only cpp --compiler "$COMPILER" --bare --reuse-build --out "$W/control-end.csv"
sample_boundary

# ---- pass end: load + pinned-core busy ----

LOAD_END="$(loadavg)"
CORE_BUSY="n/a"
if [ "$OS" = Linux ] && [ -n "$CORE_STAT_START" ]; then
    CORE_STAT_END="$(core_stat)"
    log "cpu${BENCH_CPU:-0} post: $CORE_STAT_END"
    CORE_BUSY="$(echo "$CORE_STAT_START
$CORE_STAT_END" | awk '
        NR==1 { for (i=2; i<=NF; i++) { t1 += $i }; idle1 = $5 + $6 }
        NR==2 { for (i=2; i<=NF; i++) { t2 += $i }; idle2 = $5 + $6 }
        END { dt = t2 - t1; if (dt <= 0) { print "n/a"; exit }
              printf "%.1f", 100.0 * (dt - (idle2 - idle1)) / dt }')"
fi

# ---- window verdict (§2.6): corpus-median headline of the two control legs ----

CTRL_A="$($TOOLS controlmedian "$W/control-start.csv")"
CTRL_B="$($TOOLS controlmedian "$W/control-end.csv")"
CTRL_DELTA="$(awk -v a="$CTRL_A" -v b="$CTRL_B" 'BEGIN { d = (b - a) / a * 100; if (d < 0) d = -d; printf "%.1f", d }')"
WINDOW="$(awk -v d="$CTRL_DELTA" 'BEGIN { print (d <= 5.0) ? "OK" : "INVALID" }')"
log "control: start=$CTRL_A end=$CTRL_B delta=${CTRL_DELTA}% window=$WINDOW"

# ---- aggregate (§2.4: the driver computes the statistics, not the runner) ----

if ! $TOOLS aggregate "$W"/round-*.csv > "$W/aggregated.csv"; then
    echo "aggregation refused — the rounds did not measure one pass; see $W" >&2
    exit 1
fi

# ---- twin gate (§2.6.1): aggregate each twin separately and compare ----

TWIN_VERDICT=""
TWIN_SUSPECTS=""
if [ "$TWINS" = 1 ]; then
    A_FILES="$(ls "$W"/round-*.csv | grep -v -- '-b\.csv$')"
    B_FILES="$(ls "$W"/round-*-b.csv 2>/dev/null || true)"
    if [ -z "$B_FILES" ]; then
        echo "twin gate: no twin-b legs produced rows — gate cannot run" >&2
        TWIN_VERDICT="UNKNOWN (no twin-b rows)"
    else
        # shellcheck disable=SC2086
        $TOOLS aggregate $A_FILES > "$W/twin-a.csv" || exit 1
        # shellcheck disable=SC2086
        $TOOLS aggregate $B_FILES > "$W/twin-b.csv" || exit 1
        if $TOOLS twingate "$W/twin-a.csv" "$W/twin-b.csv" > "$W/twingate.txt" 2>&1; then
            TWIN_VERDICT="OK"
        else
            TWIN_VERDICT="SUSPECT"
            TWIN_SUSPECTS="$(grep '^state-suspect:' "$W/twingate.txt" | sed 's/^state-suspect: //' | tr '\n' ';' | sed 's/;$//')"
        fi
        cat "$W/twingate.txt" >> "$LOG"
        log "twin gate: $TWIN_VERDICT ${TWIN_SUSPECTS:+suspects: $TWIN_SUSPECTS}"
    fi
fi

# ---- assemble the final CSV: run.sh preamble + §5.2 driver lines + rows ----

{
    grep '^#' "$W/control-start.csv"
    echo "# rounds: $ROUNDS   interleaved: yes"
    echo "# langs:$(echo "$AVAILABLE" | tr -s ' ')"
    [ -n "$(echo "$SKIPPED" | tr -d ' ')" ] && echo "# skipped:$(echo "$SKIPPED" | tr -s ' ') (toolchain or runner missing — see pass.log)"
    echo "# load_start: $LOAD_START"
    echo "# load_end: $LOAD_END"
    echo "# load_max: $LOAD_MAX"
    echo "# core_busy_pct: $CORE_BUSY"
    echo "# foreign_procs: $FOREIGN_MAX"
    echo "# control_start_median: $CTRL_A   control_end_median: $CTRL_B"
    echo "# control_delta_pct: $CTRL_DELTA   window: $WINDOW"
    if [ "$TWINS" = 1 ]; then
        echo "# twin_gate: ${TWIN_VERDICT:-UNKNOWN} (§2.6.1 A/A twin legs, alternating positions, same inode)"
        [ -n "$TWIN_SUSPECTS" ] && echo "# twin_suspect: $TWIN_SUSPECTS"
    fi
    cat "$W/aggregated.csv"
} > "$OUT"

log "===== interleaved pass DONE $(date -u +%FT%TZ) window=$WINDOW out=$OUT ====="

# ---- inline verdict pass (§4.1/§4.2), backfilling the final CSV ----

if [ "$INLINE" = 1 ]; then
    for lang in $(echo "$AVAILABLE" | tr -s ' '); do
        if ! bench/tools/inline-verdict.sh "$lang" "$OUT" >> "$LOG" 2>&1; then
            log "inline verdict FAILED for $lang — inline stays unknown (un-ratioable)"
            echo "inline verdict failed for $lang — see $LOG" >&2
        fi
    done
fi

echo "pass: $OUT (window: $WINDOW${TWIN_VERDICT:+; twin gate: $TWIN_VERDICT}; work: $W)" >&2
if [ "$WINDOW" != OK ]; then
    echo "WINDOW INVALID: control legs disagree by ${CTRL_DELTA}% (> 5%) — the tool will refuse ratios from this pass" >&2
fi
if [ "$TWIN_VERDICT" = SUSPECT ]; then
    echo "TWIN GATE SUSPECT (§2.6.1): $TWIN_SUSPECTS — state-selective interference; the rel tool refuses to ratio these rows. Re-run the window." >&2
fi
if [ -n "$(echo "$FAILED_LEGS" | tr -d ' ')" ]; then
    echo "failed legs:$FAILED_LEGS — see $LOG" >&2
    exit 1
fi
