#!/bin/bash
# verify-runtime-paths.sh — the §3.5 provenance gate, standalone.
#
#   bench/tools/verify-runtime-paths.sh [lang ...]     (default: all five)
#
# For each language, asks the toolchain what runtime path its build will
# actually resolve (bench/tools/runtime-paths.sh: cargo pkgid, go list -m,
# msbuild -getItem:Compile; cpp/c share one variable between -I and the
# preamble) and compares it against SERIALIZE / SERIALIZE_C / SERIALIZE_GO /
# SERIALIZE_RS / SERIALIZE_CS. Exits non-zero on ANY mismatch — run.sh calls
# the same checks before emitting a preamble, and pass-driver.sh calls this
# before its first leg so a lying pass refuses before it costs a night.
set -u

cd "$(dirname "$0")/../.."      # repo root

. bench/tools/runtime-paths.sh
runtime_paths_init

LANGS="${*:-cpp c go rust cs}"
FAIL=0
for lang in $LANGS; do
    rc=0
    resolved="$(verify_runtime "$lang")" || rc=$?
    case $rc in
        0) echo "$lang: VERIFIED $resolved" ;;
        2) echo "$lang: leg cannot run this invocation (see above) — nothing to verify" ;;
        *) echo "$lang: MISMATCH — the build would not use the recorded runtime path (§3.5)"; FAIL=1 ;;
    esac
done
exit $FAIL
