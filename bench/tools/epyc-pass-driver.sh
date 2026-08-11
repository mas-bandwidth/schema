#!/bin/bash
# v5 EPYC pass driver — 2026-08-07
#
# Glenn's gate: ONE PROFILE AT A TIME on reserved core 0 (core 0 does no game
# work; the game server owns isolated cores 1-15). The four languages run
# SERIALLY, each as its own bench/run.sh --only invocation; between legs we
# verify none of our tooling is still running (ps) and snapshot core 0's
# /proc/stat. A C++ g++ write-control runs at start and end to prove the
# window was stable, per the house law.
#
# Legs, in order:
#   1. cpp-gcc-start        C++ g++ (main table row source + write-control A)
#   2. go                   Go
#   3. rust                 Rust
#   4. cs                   C#
#   5. cpp-clang            C++ clang-18 (open item: the g++-vs-clang tiny gap)
#   6. cpp-gcc-prerestrict  C++ g++ vs serialize @ 561e81d (pre-#30 restrict)
#   7. cpp-clang-prerestrict C++ clang-18 vs serialize @ 561e81d
#   8. cpp-gcc-end          C++ g++ (write-control B)
set -u

ROOT="$HOME/rowan-bench/v5-epyc"
R="$ROOT/results"
LOG="$R/pass.log"
mkdir -p "$R"

export PATH="$HOME/rowan-bench/go/bin:$HOME/rowan-bench/cargo/bin:$HOME/rowan-bench/dotnet:$PATH"
export RUSTUP_HOME="$HOME/rowan-bench/rustup"
export CARGO_HOME="$HOME/rowan-bench/cargo"
export DOTNET_ROOT="$HOME/rowan-bench/dotnet"
export DOTNET_CLI_TELEMETRY_OPTOUT=1
export DOTNET_CLI_USE_MSBUILD_SERVER=0
export MSBUILDDISABLENODEREUSE=1
export BENCH_NOISE="RESERVED core 0: game server owns isolated cores 1-15 (one pinned thread each); core 0 does no game work - reserved for bench (Glenn, 2026-08-07) - but remains the sole general-purpose core (irqs, ssh, systemd)"

CLANGXX="$(command -v clang++-18 || command -v clang++)"

log() { echo "$*" >> "$LOG"; }

# processes of ours that must NOT be running between legs
ours() {
    ps -eo pid,psr,pcpu,etimes,comm,args --no-headers 2>/dev/null \
      | grep -E "schema_bench|benchgo|go run|cargo|rustc|cc1plus|clang\+\+|dotnet|bench_main" \
      | grep -vE "grep|pass\.sh" || true
}

pscheck() {
    log "--- pscheck [$1] $(date -u +%FT%TZ) load=$(cut -d' ' -f1-3 /proc/loadavg)"
    local b; b="$(ours)"
    if [ -n "$b" ]; then
        log "  WAITING: ours still running:"; log "$b"
        local i=0
        while [ -n "$(ours)" ] && [ $i -lt 180 ]; do sleep 10; i=$((i+1)); done
        if [ -n "$(ours)" ]; then log "  WARNING: never went quiet after 30m; proceeding, flagged"; fi
    else
        log "  clean: none of ours running"
    fi
    # core 0 utilization over a 5s window (100Hz ticks: user nice system idle iowait irq softirq)
    local s1 s2
    s1=$(grep '^cpu0 ' /proc/stat); sleep 5; s2=$(grep '^cpu0 ' /proc/stat)
    log "  cpu0 pre : $s1"
    log "  cpu0 post: $s2"
}

leg() {
    local name="$1"; shift
    pscheck "before-$name"
    log "=== leg $name START $(date -u +%FT%TZ) ($*)"
    if ( cd "$ROOT/schema" && bench/run.sh --out "$R/$name.csv" "$@" ) >> "$LOG" 2>&1; then
        log "=== leg $name OK $(date -u +%FT%TZ)"
    else
        log "=== leg $name FAILED $(date -u +%FT%TZ)"
        touch "$R/FAILED-$name"
    fi
    sleep 3
}

log "===== v5 EPYC pass START $(date -u +%FT%TZ) host=$(hostname) ====="
log "toolchains: $(g++ --version | head -1) | $($CLANGXX --version | head -1) | $(go version) | $(cargo --version) / $(rustc --version) | dotnet $(dotnet --version)"
log "isolated cpus: $(cat /sys/devices/system/cpu/isolated 2>/dev/null)"

leg cpp-gcc-start         --only cpp  --compiler g++
leg go                    --only go
leg rust                  --only rust
leg cs                    --only cs
dotnet build-server shutdown >> "$LOG" 2>&1 || true
leg cpp-clang             --only cpp  --compiler "$CLANGXX"
export SERIALIZE=../serialize-pre-restrict
leg cpp-gcc-prerestrict   --only cpp  --compiler g++
leg cpp-clang-prerestrict --only cpp  --compiler "$CLANGXX"
unset SERIALIZE
leg cpp-gcc-end           --only cpp  --compiler g++

pscheck "after-all"
log "===== v5 EPYC pass DONE $(date -u +%FT%TZ) ====="
touch "$R/PASS.done"
