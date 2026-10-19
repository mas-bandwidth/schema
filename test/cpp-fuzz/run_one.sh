#!/bin/sh
# run_one.sh <schema> <generated-dir> <binary> <seconds> <sanitizer-flags> \
#            <seed-dir> <saved-corpus-dir> <work-dir> <artifact-dir> <log>
#
# One fuzz unit: generate one schema's C++ readers, compile the libFuzzer
# harness around them, and run it for the schema's floor. The exit codes are
# the card's contract, kept by the Makefile and the workflow and reported by
# name:
#
#   0   pass: no crash, no hang, no sanitizer or assert finding
#   70  compile failure (the generated reader or the harness did not build)
#   71  setup failure (generation, roots list, or a missing runtime)
#   77  finding (libFuzzer crash, timeout, OOM, leak, sanitizer, or assert)
#   72  runtime failure (the fuzzer itself failed for another reason)
#
# New inputs are written to the work directory under build/, never to the
# committed corpus, so a green nightly leaves the tree clean. The saved
# regression corpus is read-only here; found inputs are saved as artifacts
# under build/ for triage and commit.
set -u

schema=$1
gen=$2
bin=$3
seconds=$4
san=$5
seeds=$6
saved=$7
work=$8
artifacts=$9
log=${10}

CXX=${CPP_FUZZ_CXX:-clang++-18}
base=${CPP_FUZZ_BASE_FLAGS:-}

echo "cpp fuzz setup: $schema"

rm -rf "$gen"
mkdir -p "$gen" "$work" "$artifacts" "$(dirname "$bin")"

if ! ./bin/schema generate --lang cpp --out "$gen" "$schema" >"$log.generate" 2>&1; then
    echo "cpp fuzz setup failed: $schema (generate)"
    cat "$log.generate"
    exit 71
fi
if ! sh test/cpp-fuzz/gen_roots.sh "$gen" "$gen/roots.inc" >>"$log.generate" 2>&1; then
    echo "cpp fuzz setup failed: $schema (roots)"
    cat "$log.generate"
    exit 71
fi

# shellcheck disable=SC2086
if ! $CXX $base $san -I"$gen" -DFUZZ_ROOTS_INC='"roots.inc"' \
    test/cpp-fuzz/nightly_fuzz_main.cpp "$gen"/*Table.cpp -o "$bin" >"$log.compile" 2>&1; then
    echo "cpp fuzz compile failed: $schema"
    cat "$log.compile"
    exit 70
fi

"$bin" -max_total_time="$seconds" -timeout=25 -rss_limit_mb=2048 \
    -error_exitcode=77 -artifact_prefix="$artifacts/" \
    "$work" "$saved" \
    "$seeds/wire" "$seeds/text" "$seeds/cook" "$seeds/go" >"$log" 2>&1
rc=$?
case $rc in
    0)
        echo "cpp fuzz pass: $schema (${seconds}s)"
        ;;
    77)
        echo "cpp fuzz finding: $schema"
        grep -m1 -E 'ERROR: AddressSanitizer|ERROR: MemorySanitizer|runtime error:|SUMMARY:|==ERROR|libFuzzer: (timeout|out-of-memory|leak)' "$log" || true
        ;;
    72)
        echo "cpp fuzz runtime failed: $schema"
        tail -n 20 "$log"
        ;;
    *)
        echo "cpp fuzz runtime failed: $schema (exit $rc)"
        tail -n 20 "$log"
        rc=72
        ;;
esac
exit $rc
