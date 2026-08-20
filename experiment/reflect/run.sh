#!/bin/bash
# run.sh — EXPERIMENT (issue #105). Both legs of the experiment.
#
#   ./experiment/reflect/run.sh            everything
#   ./experiment/reflect/run.sh wire       the wire differential only
#   ./experiment/reflect/run.sh code       the code-identity pass only
#
# Flags are bench/run.sh's published C++ Release configuration (BENCH-STANDARD
# §3.3) — the same ones every other number in the estate is measured under.
#
# NO TIMINGS ARE TAKEN. This experiment's evidence is the disassembly and the
# §4 hot-call verdict; a throughput ratio measured on a loaded laptop would be
# worthless and is deliberately absent.

set -eu

ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )/../.." && pwd )"
cd "$ROOT"

SERIALIZE="${SERIALIZE:-../serialize}"
CXX_BIN="${CXX:-c++}"
OUT=build/exp
mkdir -p "$OUT"

COMMON=( -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -fno-rtti
         -Igenerated/cpp -Itest -I"$SERIALIZE" )

# The two repairs the experiment landed on (both explained in schema_reflect.h).
REPAIRS=( -DSCHEMA_REFLECT_WRITE_FLAT_FOLD -DSCHEMA_REFLECT_ACCESSOR )

WHAT="${1:-all}"

if [ "$WHAT" = all ] || [ "$WHAT" = wire ]; then
    echo "############ WIRE IDENTITY ############"
    for mode in "baseline::" \
                "flat-fold::-DSCHEMA_REFLECT_WRITE_FLAT_FOLD" \
                "accessor::-DSCHEMA_REFLECT_ACCESSOR" \
                "both::-DSCHEMA_REFLECT_WRITE_FLAT_FOLD -DSCHEMA_REFLECT_ACCESSOR"; do
        name="${mode%%::*}"; flags="${mode##*::}"
        # shellcheck disable=SC2086
        $CXX_BIN -O2 -DNDEBUG -DSERIALIZE_RELEASE $flags "${COMMON[@]}" \
            experiment/reflect/differential.cpp -o "$OUT/differential_$name"
        printf '%-12s ' "$name"
        "$OUT/differential_$name" "${SAMPLES:-2000000}" | tail -3 | tr '\n' ' '
        echo
    done

    echo
    echo "asserts ENABLED (generators clamped into the declared ranges):"
    # shellcheck disable=SC2086
    $CXX_BIN -O1 -g -DSERIALIZE_DEBUG -DEXP_LEGAL_ONLY "${REPAIRS[@]}" "${COMMON[@]}" \
        experiment/reflect/differential.cpp -o "$OUT/differential_asserts"
    printf '%-12s ' "both"
    "$OUT/differential_asserts" "${ASSERT_SAMPLES:-200000}" | tail -3 | tr '\n' ' '
    echo
fi

if [ "$WHAT" = all ] || [ "$WHAT" = code ]; then
    for OPT in O3 O2; do
        for mode in "baseline::" \
                    "repaired::-DSCHEMA_REFLECT_WRITE_FLAT_FOLD -DSCHEMA_REFLECT_ACCESSOR"; do
            name="${mode%%::*}"; flags="${mode##*::}"
            echo
            echo "############ CODE IDENTITY — -$OPT, $name ############"
            # shellcheck disable=SC2086
            $CXX_BIN "-$OPT" -DNDEBUG -DSERIALIZE_RELEASE $flags "${COMMON[@]}" \
                experiment/reflect/codegen_probe.cpp experiment/reflect/probe_main.cpp \
                -o "$OUT/probe_${OPT}_${name}"
            otool -tv "$OUT/probe_${OPT}_${name}" > "$OUT/disasm_${OPT}_${name}.txt"
            go run ./experiment/reflect/tools/compare.go \
                "$OUT/disasm_${OPT}_${name}.txt" "$OUT/norm_${OPT}_${name}"
        done
    done

    echo
    echo "############ CONTROL: TBAA off on BOTH sides (-fno-strict-aliasing) ############"
    echo "# If the residual really is the struct-path TBAA tag a pointer-to-member loses,"
    echo "# removing TBAA from BOTH sides must collapse every pair to identical."
    # shellcheck disable=SC2086
    for mode in "pointer-to-member::-DSCHEMA_REFLECT_WRITE_FLAT_FOLD" "accessor::-DSCHEMA_REFLECT_WRITE_FLAT_FOLD -DSCHEMA_REFLECT_ACCESSOR"; do
        name="${mode%%::*}"; flags="${mode##*::}"
        echo; echo "---- member access: $name ----"
        # shellcheck disable=SC2086
        $CXX_BIN -O3 -fno-strict-aliasing -DNDEBUG -DSERIALIZE_RELEASE $flags "${COMMON[@]}" \
            experiment/reflect/codegen_probe.cpp experiment/reflect/probe_main.cpp -o "$OUT/probe_nostrict_$name"
        otool -tv "$OUT/probe_nostrict_$name" > "$OUT/disasm_nostrict_$name.txt"
        go run ./experiment/reflect/tools/compare.go "$OUT/disasm_nostrict_$name.txt" | tail -12
    done

    echo
    echo "############ the inline remarks behind the verdict ############"
    # shellcheck disable=SC2086
    $CXX_BIN -O3 -DNDEBUG -DSERIALIZE_RELEASE "${COMMON[@]}" -Wno-error -g \
        -Rpass=inline -Rpass-missed=inline \
        -c experiment/reflect/codegen_probe.cpp -o "$OUT/probe_remarks.o" \
        2> "$OUT/inline-remarks.txt" || true
    grep -E "compressed_float|WriteBytes" "$OUT/inline-remarks.txt" \
        | grep -E "probe_(emitted|generic)" | sed 's/\[-Rpass[^]]*\]//' || true
fi
