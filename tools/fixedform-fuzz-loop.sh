#!/bin/sh
# THE FIXED FORM FUZZER'S LOOP (test/tables/fixedform_fuzz.cpp). libFuzzer stops
# at -max_total_time and a fuzzer that stops is a fuzzer nobody is running, so
# this restarts it every hour against the SAME corpus directory — each round
# inherits everything the last one found interesting, which is the only reason
# restarting is free.
#
# It is a loop and not a cron entry because a loop can be stopped by deleting
# one file and a cron entry cannot, and because the corpus directory must not
# have two owners.
#
#   start:  nohup tools/fixedform-fuzz-loop.sh > ~/fuzz/log 2>&1 &
#   check:  tail ~/fuzz/log; ls ~/fuzz/crashes
#   stop:   rm -f ~/fuzz/RUN   (the round in flight finishes, then the loop ends)
#           rm -f ~/fuzz/RUN && pkill -f schema_test_fixedform_fuzz   (now)
#
# EVERY WAIT HAS A DEADLINE: the -max_total_time below is the round's, and the
# RUN flag is the loop's. Neither waits forever.
set -u

ROOT="${FUZZ_ROOT:-$HOME/fuzz}"
REPO="${FUZZ_REPO:-$ROOT/schema}"
BIN="$REPO/build/schema_test_fixedform_fuzz"
CORPUS="$REPO/build/fixedform-fuzz-corpus"
# THE GROWING CORPUS IS NOT THE SEED CORPUS, and the first round taught us why:
# libFuzzer writes every interesting unit into the FIRST corpus directory it is
# given, so pointing it at the seeds makes the gate's replay set grow mutants
# overnight and go red on inputs nobody chose. The seeds are handed over second
# and stay exactly what the reference wrote.
FOUND="${FUZZ_FOUND:-$ROOT/corpus}"
CRASHES="${FUZZ_CRASHES:-$ROOT/crashes}"
ROUND="${FUZZ_ROUND_SECONDS:-3600}"

if [ ! -x "$BIN" ]; then
    echo "fixedform fuzz loop: $BIN is not built — make tables-fixedform-fuzz first" >&2
    exit 1
fi

mkdir -p "$CRASHES" "$CORPUS" "$FOUND"
: > "$ROOT/RUN"

# ONE WORKER PER CORE, and on a one-core box that is one worker: -jobs past
# nproc only makes the rounds fight over the same core and the same corpus.
JOBS="${FUZZ_JOBS:-$(nproc 2>/dev/null || echo 1)}"

echo "fixedform fuzz loop: starting, jobs=$JOBS round=${ROUND}s"
echo "fixedform fuzz loop: seeds=$CORPUS (never written) growing=$FOUND crashes=$CRASHES"
echo "fixedform fuzz loop: stop with  rm -f $ROOT/RUN"

round=0
while [ -e "$ROOT/RUN" ]; do
    round=$(( round + 1 ))
    echo "=== round $round — $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
    "$BIN" \
        -max_total_time="$ROUND" \
        -jobs="$JOBS" -workers="$JOBS" \
        -artifact_prefix="$CRASHES/" \
        -print_final_stats=1 \
        -rss_limit_mb=4096 \
        "$FOUND" "$CORPUS" 2>&1
    status=$?
    echo "=== round $round ended, status $status, $(ls -1 "$CRASHES" 2>/dev/null | wc -l) artifacts in $CRASHES ==="
    # A FINDING IS NOT A REASON TO STOP but it is a reason to say so loudly: the
    # artifact is on disk, the next round keeps the corpus, and the report names
    # the count every hour until somebody looks.
    if [ "$status" -ne 0 ]; then
        echo "fixedform fuzz loop: ROUND $round ENDED NONZERO — look in $CRASHES and in the fuzz-*.log beside it"
    fi
done

echo "fixedform fuzz loop: the RUN flag is gone, stopping after $round rounds"
