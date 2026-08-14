#!/bin/bash
# inline-verdict.sh — the §4 inline verdict pass, one language per invocation.
#
#   bench/tools/inline-verdict.sh <lang> <results.csv>
#
# Produces two things (BENCH-STANDARD.md §4.1/§4.2):
#
#   1. <results>.inline — the per-symbol ledger: every symbol in the measured
#      artifact that still calls into the serialize runtime, with call counts
#      (direct, and transitive through out-of-line generated helpers — a bl
#      into an out-of-line helper contributes that helper's own runtime calls,
#      because the helper body runs per op either way), plus the compiler's
#      own inline remarks (cost/threshold or cost/budget) where the toolchain
#      reports them.
#
#   2. the inline column of <results.csv>, backfilled per row: full when the
#      emitted code for that benchmark's operation contains zero calls into
#      the serialize runtime, partial:N when N remain, unknown when the
#      verdict could not be attributed. unknown stays un-ratioable — that is
#      the §4.2 contract, not a shrug, so this script REFUSES rather than
#      guesses whenever attribution would be a guess.
#
# Ground truth is the §4.1 universal fallback: call instructions counted in
# the emitted code (otool -tv on this arm64 Mac; go tool objdump for Go;
# DOTNET_JitDisasm for C#). Compiler remarks are advisory ledger content;
# the disassembly is the verdict. For C# the count follows §4.1 literally —
# every bl/blr in the method body — because indirect targets are opaque to
# static counting and silently dropping them would fake a full.
#
# The measured artifacts must exist (run.sh / pass-driver.sh just built and
# ran them); this script never changes how they were built. The Go remark
# pass uses `go build -a` — MANDATORY: without it the build cache serves
# cached objects and prints NOTHING, which reads as a false clean. The Rust
# remark pass rebuilds with -Cremark=inline -Cdebuginfo=1 (RUSTFLAGS is in
# cargo's fingerprint, so the change itself forces the rebuild) and
# disassembles the measured binary FIRST, before that rebuild replaces it.
set -u

cd "$(dirname "$0")/../.."      # repo root

if [ $# -ne 2 ]; then
    echo "usage: bench/tools/inline-verdict.sh <c|cpp|go|rust|cs> <results.csv>" >&2
    exit 1
fi
LANG_ARG="$1"
CSV="$2"
[ -f "$CSV" ] || { echo "no such csv: $CSV" >&2; exit 1; }
LEDGER="${CSV%.csv}.inline"
VD="build/bench/inline-verdict-$LANG_ARG"
rm -rf "$VD" && mkdir -p "$VD"

SERIALIZE_C="${SERIALIZE_C:-../serialize.c}"

# The native-binary parsing below is otool/arm64-shaped (bl/blr). On a
# machine without otool (the x86-64 EPYC box) the C/C++/Rust verdicts would
# silently count zero and fake a full — refuse instead; inline stays unknown
# there until the objdump/x86 adapter is written.
case "$LANG_ARG" in
c|cpp|rust)
    command -v otool >/dev/null 2>&1 || {
        echo "inline-verdict: no otool — the arm64/otool parser would fake 'full' on this host; refusing (inline stays unknown)" >&2
        exit 1
    }
    ;;
esac

# bench -> generated per-op entry stems: <snake for c/rust> <Camel for cpp/go/cs>
BENCH_MAP='rigidbody_moving rigid_body RigidBody
rigidbody_at_rest rigid_body RigidBody
chat chat Chat
test test Test
inputpacket input_packet InputPacket
shipcreate ship_create ShipCreate
ship_shallow ship_shallow ShipData_Shallow
probe_header probe_header ProbeHeader
probebits probe_bits ProbeBits
probearray probe_array ProbeArray
testdata test_data TestData
message_batch message Message'

# ---- per-symbol transitive runtime-call counting ----
# stdin: "sym:" header lines and "addr <bl|blr> target" instruction lines
# (otool -tv shape; the go/cs branches translate into it). RT = runtime
# target regex, HELPER = generated helper regex. Output, one line per symbol
# (zero counts included, so an entry that fully inlined the runtime is a
# recorded 0, not an absence):
#   SYM <symbol> <direct> <transitive> <indirect>
count_calls() {
    awk -v RT="$1" -v HELPER="$2" '
        /^[^ \t].*:$/ { sym = substr($0, 1, length($0) - 1); syms[sym] = 1; next }
        $2 == "bl" && sym != "" {
            t = $3
            # helper first: the cs branch passes a catch-all RT, and helper
            # calls must become edges, never direct counts (the regexes are
            # disjoint for every other language)
            if (t ~ HELPER) edge[sym SUBSEP t]++
            else if (t ~ RT) direct[sym]++
        }
        $2 == "blr" && sym != "" { indirect[sym]++ }
        END {
            for (s in syms) total[s] = direct[s]
            for (i = 0; i < 8; i++) {            # fixpoint over the helper DAG
                for (s in syms) fresh[s] = direct[s]
                for (k in edge) {
                    split(k, a, SUBSEP)
                    fresh[a[1]] += edge[k] * total[a[2]]
                }
                for (s in syms) total[s] = fresh[s]
            }
            for (s in syms)
                printf "SYM %s %d %d %d\n", s, direct[s] + 0, total[s] + 0, indirect[s] + 0
        }'
}

# verdict from a transitive count: 0 -> full, N -> partial:N
verdict_of() {
    if [ "$1" -eq 0 ]; then echo "full"; else echo "partial:$1"; fi
}

# transitive count for the first symbol matching the regex; -1 if absent
count_for() {
    awk -v pat="$1" '$1 == "SYM" && $2 ~ pat { print $4; found = 1; exit } END { if (!found) print -1 }' "$VD/counts.txt"
}

# sum of transitive counts across every symbol (the whole-binary fallback)
count_total() {
    awk '$1 == "SYM" { s += $4 } END { print s + 0 }' "$VD/counts.txt"
}

ledger_header() {
    if [ ! -f "$LEDGER" ]; then
        {
            echo "# $(basename "$LEDGER") — per-symbol inline ledger (BENCH-STANDARD.md §4)"
            echo "# generated: $(date -u +%FT%TZ) on $(hostname -s) ($(uname -sm))"
            echo "# ground truth: call instructions into the serialize runtime, counted in"
            echo "# the emitted code and propagated transitively through out-of-line"
            echo "# generated helpers. Compiler remarks are advisory; the disassembly is"
            echo "# the verdict. CSV verdicts: full = zero runtime calls per op,"
            echo "# partial:N = N remain, unknown = not attributable (stays un-ratioable)."
        } > "$LEDGER"
    fi
}

section() {
    ledger_header
    # replace any previous section for this language
    awk -v lang="== $1 " '
        index($0, lang) == 1 { drop = 1; next }
        /^== / { drop = 0 }
        !drop' "$LEDGER" > "$LEDGER.tmp" && mv "$LEDGER.tmp" "$LEDGER"
    echo "== $1 $2" >> "$LEDGER"
}

nonzero_symbols() {
    awk '$1 == "SYM" && $3 + $4 + $5 > 0' "$VD/counts.txt" | sort -k4 -rn | sed 's/^SYM /symbol /'
}

# ---- backfill: rewrite this language's rows' inline column ----
# verdicts.txt lines: "<bench> <path> <verdict>"
backfill() {
    awk -F, -v OFS=, -v lang="$LANG_ARG" -v vf="$VD/verdicts.txt" '
        BEGIN { while ((getline line < vf) > 0) { split(line, a, " "); v[a[1] "," a[2]] = a[3] } }
        $1 == lang && NF == 17 { key = $2 "," $3; if (key in v) $17 = v[key] }
        { print }' "$CSV" > "$CSV.tmp" && mv "$CSV.tmp" "$CSV"
    echo "backfilled $LANG_ARG rows in $CSV" >&2
}

case "$LANG_ARG" in

# --------------------------------------------------------------- C and C++
c|cpp)
    if [ "$LANG_ARG" = c ]; then
        BIN=build/bench/schema_bench_c
        RT='^_?serialize_'                  # serialize.c entry points
        HELPER='^_?(write|read|quantize)_'  # generated helpers (write_vec3 ...)
    else
        BIN=build/bench/schema_bench_cpp
        RT='^__?ZNK?9serialize'             # namespace serialize (runtime proper)
        HELPER='^__?ZNK?7example'           # namespace example (generated)
    fi
    [ -x "$BIN" ] || { echo "$BIN missing — run the pass first" >&2; exit 1; }
    otool -tv "$BIN" > "$VD/disasm.txt"
    count_calls "$RT" "$HELPER" < "$VD/disasm.txt" > "$VD/counts.txt"

    # clang remarks (§4.1: -Rpass=inline -Rpass-missed=inline; NOT
    # -fopt-info-inline, the GCC spelling Apple clang rejects). A shadow
    # compile with the recorded codegen flags plus the remark switches; its
    # binary is discarded — the verdict came from $BIN.
    FLAGS_LINE="$(grep "^# $LANG_ARG flags:" "$CSV" | head -1 | sed "s/^# $LANG_ARG flags: //; s/ (serialize\.c compiled in, no LTO)//")"
    REMARKS="$VD/remarks.txt"
    : > "$REMARKS"
    if [ -n "$FLAGS_LINE" ]; then
        if [ "$LANG_ARG" = c ]; then
            ${CC:-cc} $FLAGS_LINE -Rpass=inline -Rpass-missed=inline -g \
                bench/c/bench_main.c "$SERIALIZE_C/serialize.c" \
                -o "$VD/shadow" -lm 2> "$REMARKS" || true
        else
            ${CXX:-c++} $FLAGS_LINE -Rpass=inline -Rpass-missed=inline -g \
                bench/cpp/bench_main.cpp -o "$VD/shadow" 2> "$REMARKS" || true
        fi
    else
        echo "note: $CSV carries no '# $LANG_ARG flags:' line (bare file?) — remark shadow compile skipped" >> "$VD/remarks.note"
    fi

    section "$LANG_ARG" "(otool -tv of $BIN; clang -Rpass=inline shadow compile)"
    {
        echo "# symbol / direct-runtime-calls / transitive-per-op / indirect (nonzero only)"
        nonzero_symbols
        echo "# missed-inline remarks naming serialize (callee, caller, cost where reported):"
        grep -E "not inlined into" "$REMARKS" 2>/dev/null | grep -i serialize \
            | sed -E "s/^[^:]*:[0-9]+:[0-9]+: remark: //; s/ \[-Rpass.*//" \
            | sort | uniq -c | sort -rn | head -40 | sed 's/^ */remark /' \
            || true
    } >> "$LEDGER"

    : > "$VD/verdicts.txt"
    if [ "$LANG_ARG" = c ]; then
        # C per-row verdicts through the generated per-op entry symbols. The
        # serialize.c calls cross a TU boundary, so an entry's out-of-line
        # body has the same per-op call structure as any inlined copy of it.
        # An entry inlined away entirely stays unknown (with a ledger note)
        # unless the whole binary is call-free — refusing beats guessing.
        echo "$BENCH_MAP" | while read -r bench snake camel; do
            for path in write read; do
                n="$(count_for "^_?${path}_${snake}\$")"
                if [ "$n" -ge 0 ]; then
                    echo "$bench $path $(verdict_of "$n")" >> "$VD/verdicts.txt"
                elif [ "$(count_total)" -eq 0 ]; then
                    echo "$bench $path full" >> "$VD/verdicts.txt"
                else
                    echo "note: c $bench $path: entry symbol inlined away and runtime calls exist elsewhere — inline stays unknown" >> "$LEDGER"
                fi
            done
        done
        echo "# note: static counts include both arms of branchy messages (rigidbody at_rest" >> "$LEDGER"
        echo "# shares rigid_body's entry, so its N is the moving shape's upper bound)" >> "$LEDGER"
    else
        # C++ per-row verdicts by walking the -g shadow's inline stacks.
        # clang is deterministic: same compiler + flags + source means the
        # same inlining; -g adds only metadata. atos -i then names, for every
        # remaining call, which bench_message instantiation and which timed
        # loop it sits in — and setup/self-check/variant code is EXCLUDED
        # rather than miscounted.
        [ -x "$VD/shadow" ] || { echo "shadow build failed — cannot attribute cpp verdicts (see $REMARKS)" >&2; exit 1; }
        otool -tv "$VD/shadow" > "$VD/shadow-disasm.txt"
        count_calls "$RT" "$HELPER" < "$VD/shadow-disasm.txt" > "$VD/shadow-counts.txt"

        # every remaining runtime/helper call site in the shadow text
        awk -v RT="$RT" -v HELPER="$HELPER" '
            /^[^ \t].*:$/ { next }
            $2 == "bl" && ($3 ~ RT || $3 ~ HELPER) { print $1, $3 }
        ' "$VD/shadow-disasm.txt" > "$VD/addrs.txt"

        # drift guard: the shadow must have the same call structure as the
        # measured binary, or the attribution below describes the wrong code
        M_TOTAL="$(count_total)"
        S_TOTAL="$(awk '$1 == "SYM" { s += $4 } END { print s + 0 }' "$VD/shadow-counts.txt")"
        if [ "$M_TOTAL" != "$S_TOTAL" ]; then
            echo "note: SHADOW DRIFT: measured binary has $M_TOTAL transitive runtime calls, -g shadow has $S_TOTAL — verdicts below describe the shadow" >> "$LEDGER"
        fi

        # source maps: which main line calls which bench, and the timed-loop
        # line ranges inside bench_message / bench_batch
        SRC=bench/cpp/bench_main.cpp
        grep -n 'bench_message( "' "$SRC" | sed -E 's/^([0-9]+):.*bench_message\( "([a-z_]+)".*/\1 \2/' > "$VD/benchlines.txt"
        grep -n 'bench_batch();' "$SRC" | sed -E 's/^([0-9]+):.*/\1 message_batch/' >> "$VD/benchlines.txt"
        W1=$(grep -n 'write path: 1 warmup' "$SRC" | cut -d: -f1 | head -1)
        W2=$(grep -n 'read path: 1 warmup' "$SRC" | cut -d: -f1 | head -1)
        RE=$(grep -n 'report( name, "write"' "$SRC" | cut -d: -f1 | head -1)
        B1=$(grep -n 'write path: whole batch' "$SRC" | cut -d: -f1 | head -1)
        B2=$(grep -n 'the read buffer: rebuild' "$SRC" | cut -d: -f1 | head -1)
        B3=$(grep -n 'read path: read messages' "$SRC" | cut -d: -f1 | head -1)
        B4=$(grep -n 'report( "message_batch"' "$SRC" | cut -d: -f1 | head -1)

        # inline stacks for every call site, batched through atos (blank line
        # separates address groups; an explicit separator guards the batch seam)
        : > "$VD/stacks.txt"
        cut -d' ' -f1 "$VD/addrs.txt" | sed 's/^/0x/' | {
            batch=()
            while read -r a; do
                batch+=("$a")
                if [ "${#batch[@]}" -eq 200 ]; then
                    atos -o "$VD/shadow" -i "${batch[@]}" >> "$VD/stacks.txt"; echo "" >> "$VD/stacks.txt"
                    batch=()
                fi
            done
            [ "${#batch[@]}" -gt 0 ] && { atos -o "$VD/shadow" -i "${batch[@]}" >> "$VD/stacks.txt"; echo "" >> "$VD/stacks.txt"; }
            true
        }

        awk -v RT="$RT" -v addrsf="$VD/addrs.txt" -v countsf="$VD/shadow-counts.txt" \
            -v benchf="$VD/benchlines.txt" \
            -v W1="$W1" -v W2="$W2" -v RE="$RE" -v B1="$B1" -v B2="$B2" -v B3="$B3" -v B4="$B4" '
            BEGIN {
                while ((getline line < addrsf) > 0) { na++; split(line, a, " "); target[na] = a[2] }
                while ((getline line < countsf) > 0) { split(line, a, " "); tot[a[2]] = a[4] }
                while ((getline line < benchf) > 0) { split(line, a, " "); benchof[a[1]] = a[2] }
                tmap["RigidBody"] = "rigidbody_moving"; tmap["Chat"] = "chat"
                tmap["Test"] = "test"; tmap["InputPacket"] = "inputpacket"
                tmap["ShipCreate"] = "shipcreate"; tmap["ShipData_Shallow"] = "ship_shallow"
                tmap["ProbeHeader"] = "probe_header"; tmap["ProbeBits"] = "probebits"
                tmap["ProbeArray"] = "probearray"; tmap["TestData"] = "testdata"
                g = 0
            }
            function flush_group(    i, f, bench, path, L, contrib, t, tn) {
                if (nf == 0) return
                g++
                bench = ""; path = ""
                for (i = 1; i <= nf; i++) {
                    f = frames[i]
                    if (f ~ /bench_batch\(\)/ && match(f, /bench_main\.cpp:[0-9]+/)) {
                        L = substr(f, RSTART + 15, RLENGTH - 15) + 0
                        bench = "message_batch"
                        if (L >= B1 && L < B2) path = "write"
                        else if (L >= B3 && L < B4) path = "read"
                        else { untimed++; nf = 0; return }
                        break
                    }
                    if (f ~ /bench_message</ && match(f, /bench_main\.cpp:[0-9]+/)) {
                        L = substr(f, RSTART + 15, RLENGTH - 15) + 0
                        if (L >= W1 && L < W2) path = "write"
                        else if (L >= W2 && L < RE) path = "read"
                        else { untimed++; nf = 0; return }
                        # bench from the main frame call line, else from <T>
                        for (j = i + 1; j <= nf; j++) {
                            if (frames[j] ~ /^main / && match(frames[j], /bench_main\.cpp:[0-9]+/)) {
                                ml = substr(frames[j], RSTART + 15, RLENGTH - 15) + 0
                                if (ml in benchof) bench = benchof[ml]
                            }
                        }
                        if (bench == "") {
                            # "bench_message<" is 14 chars, "example::" is 9
                            if (match(f, /bench_message<example::[A-Za-z_]+/)) {
                                tn = substr(f, RSTART + 23, RLENGTH - 23)
                                if (tn in tmap) bench = tmap[tn]
                                if (bench == "rigidbody_moving" && f ~ /\$_/) bench = "rigidbody_at_rest"
                            }
                        }
                        break
                    }
                }
                if (bench == "" || path == "") { if (i <= nf) unattributed++; else untimed++; nf = 0; return }
                t = target[g]
                contrib = (t ~ RT) ? 1 : tot[t] + 0
                n[bench "," path] += contrib
                nf = 0
            }
            NF == 0 { flush_group(); next }
            { frames[++nf] = $0 }
            END {
                flush_group()
                for (k in n) { split(k, a, ","); printf "N %s %s %d\n", a[1], a[2], n[k] }
                printf "STATS groups %d untimed %d unattributed %d\n", g, untimed, unattributed
            }' "$VD/stacks.txt" > "$VD/attribution.txt"

        # the attribution must PROVE it ran: without the STATS line (or with
        # every group unattributed) a parser failure would default every row
        # to 0 and fake a full — refuse instead, rows stay unknown.
        if ! grep -q '^STATS groups' "$VD/attribution.txt"; then
            echo "cpp attribution failed (no STATS line in $VD/attribution.txt) — inline stays unknown" >&2
            echo "note: cpp attribution FAILED — all cpp rows left unknown" >> "$LEDGER"
            exit 1
        fi
        NADDR="$(wc -l < "$VD/addrs.txt" | tr -d ' ')"
        NACC="$(awk '/^STATS/ { print $3 + 0 }' "$VD/attribution.txt")"
        if [ "$NADDR" != "$NACC" ]; then
            echo "cpp attribution incomplete: $NACC of $NADDR call sites walked — inline stays unknown" >&2
            echo "note: cpp attribution INCOMPLETE ($NACC of $NADDR call sites) — all cpp rows left unknown" >> "$LEDGER"
            exit 1
        fi

        {
            echo "# cpp per-row attribution (atos inline stacks over the -g shadow;"
            echo "# untimed = call sites in setup/self-check/variant code, excluded):"
            sed 's/^/attr /' "$VD/attribution.txt"
        } >> "$LEDGER"

        echo "$BENCH_MAP" | while read -r bench snake camel; do
            for path in write read; do
                n="$(awk -v b="$bench" -v p="$path" '$1 == "N" && $2 == b && $3 == p { print $4; f = 1; exit } END { if (!f) print 0 }' "$VD/attribution.txt")"
                echo "$bench $path $(verdict_of "$n")" >> "$VD/verdicts.txt"
            done
        done
    fi
    backfill
    ;;

# --------------------------------------------------------------------- Go
go)
    command -v go >/dev/null 2>&1 || { echo "no go toolchain" >&2; exit 1; }
    # the measured shape: an ordinary optimized build of the runner
    ( cd bench/go && go build -o "$OLDPWD/$VD/benchgo" . ) || exit 1
    go tool objdump "$VD/benchgo" > "$VD/objdump.txt" 2>/dev/null

    # translate objdump into the otool shape count_calls parses
    awk '
        /^TEXT / { name = $2; sub(/\(SB\)$/, "", name); print name ":"; next }
        {
            for (i = 1; i <= NF; i++)
                if ($i == "CALL") {
                    t = $(i + 1); sub(/\(SB\)$/, "", t)
                    if (t ~ /^[A-Z]+[0-9]*$/) print "0 blr " t   # CALL R26: indirect
                    else print "0 bl " t
                    break
                }
        }' "$VD/objdump.txt" > "$VD/calls.txt"
    count_calls 'serialize%2ego\.|mas-bandwidth\/serialize' '^example\.' < "$VD/calls.txt" > "$VD/counts.txt"

    # remarks: go build -a -gcflags=-m=2 — -a is MANDATORY (see header)
    ( cd bench/go && go build -a -gcflags=all=-m=2 . > /dev/null 2> "$OLDPWD/$VD/remarks.txt" ) || true
    grep -E "serialize\.go/" "$VD/remarks.txt" | grep -E ": can inline|: cannot inline" \
        | sed -E 's/^.*serialize\.go\///' | sed -E 's/ as:.*$//' | sort -u > "$VD/serialize-remarks.txt" || true

    # the §4.1 sanity cross-check: every runtime symbol the disassembly shows
    # as a call target should be one -m=2 could not inline, or one whose
    # callers ran out of budget — and the ledger says which.
    : > "$VD/crosscheck.txt"
    awk '$2 == "bl" && $3 ~ /serialize%2ego\./ { print $3 }' "$VD/calls.txt" | sort -u | while read -r target; do
        short="${target##*serialize%2ego.}"
        if grep -qF "cannot inline $short" "$VD/serialize-remarks.txt"; then
            echo "AGREE: $short is a call target and -m=2 says cannot inline" >> "$VD/crosscheck.txt"
        elif grep -qF "can inline $short" "$VD/serialize-remarks.txt"; then
            echo "NOTE: $short can inline by cost yet remains a call target (caller over budget)" >> "$VD/crosscheck.txt"
        else
            echo "DISAGREE: $short is a call target but -m=2 has no verdict for it" >> "$VD/crosscheck.txt"
        fi
    done

    section go "(go tool objdump of the measured build; go build -a -gcflags=all=-m=2)"
    {
        echo "# sanity cross-check (§4.1): the universal ground truth (objdump call"
        echo "# targets) checked against the compiler-remark verdict:"
        sed 's/^/crosscheck /' "$VD/crosscheck.txt"
        echo "# serialize.go remark ledger (file:line, symbol, cost, budget):"
        sed 's/^/remark /' "$VD/serialize-remarks.txt"
        echo "# symbol / direct-runtime-calls / transitive-per-op / indirect (generated code, nonzero)"
        grep "^SYM example\." "$VD/counts.txt" | awk '$3 + $4 + $5 > 0' | sort -k4 -rn | sed 's/^SYM /symbol /'
    } >> "$LEDGER"

    : > "$VD/verdicts.txt"
    echo "$BENCH_MAP" | while read -r bench snake camel; do
        for path in write read; do
            Cap="$(echo "$path" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
            n="$(count_for "^example\\.${Cap}${camel}\$")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n")" >> "$VD/verdicts.txt"
            else
                echo "note: go $bench $path: example.${Cap}${camel} not found in objdump — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    backfill
    ;;

# ------------------------------------------------------------------- Rust
rust)
    BIN=bench/rust/target/release/benchrust
    [ -x "$BIN" ] || { echo "$BIN missing — run the pass first" >&2; exit 1; }

    # v0 mangling puts the DEFINING crate first and the instantiating crate
    # last, and serialize types appear inside example/benchrust generics —
    # so classify every call target by its EARLIEST crate token, and refuse
    # the naive substring match that would count an example shim mentioning
    # WriteStream as a runtime call.
    otool -tv "$BIN" | awk '
        function firstcrate(t,    best, cls, i, n, names, m) {
            split("9serialize 7example 9benchrust 4core 3std 5alloc", names, " ")
            best = 1000000; cls = "other"
            for (i = 1; i <= 6; i++) {
                n = index(t, names[i])
                if (n > 0 && n < best) { best = n; m = names[i]; cls = substr(m, 2) }
            }
            return cls
        }
        /^[^ \t].*:$/ { print; next }
        $2 == "bl"  { print $1 " bl CRATE_" firstcrate($3) "_" $3; next }
        $2 == "blr" { print $1 " blr indirect"; next }
    ' > "$VD/disasm.txt"
    count_calls '^CRATE_serialize_' '^CRATE_(example|benchrust)_' < "$VD/disasm.txt" > "$VD/counts.txt"

    # remarks: RUSTFLAGS="-Cremark=inline -Cdebuginfo=1" rebuild (fingerprint
    # forces it); runs AFTER the measured binary was disassembled above.
    RUSTUP_BIN="/opt/homebrew/opt/rustup/bin"
    CARGO="cargo"; command -v cargo >/dev/null 2>&1 || CARGO="$RUSTUP_BIN/cargo"
    ( cd bench/rust && \
      RUSTFLAGS="-Cremark=inline -Cdebuginfo=1" CARGO_INCREMENTAL=0 \
      PATH="$RUSTUP_BIN:$PATH" "$CARGO" build --release --quiet 2> "$OLDPWD/$VD/remarks.txt" ) || true
    grep -E "inline \(missed\)" "$VD/remarks.txt" | grep -F serialize \
        | sed -E 's/^[^:]*:[0-9]+:[0-9]+: *//; s/note: //' \
        | sort | uniq -c | sort -rn | head -40 > "$VD/serialize-remarks.txt" || true

    section rust "(otool -tv of the measured $BIN, targets classified by defining crate; -Cremark=inline shadow rebuild)"
    {
        echo "# symbol / direct-runtime-calls / transitive-per-op / indirect (nonzero only)"
        nonzero_symbols | head -60
        echo "# -Cremark=inline missed-inline lines naming serialize (count, remark):"
        sed 's/^ */remark /' "$VD/serialize-remarks.txt"
    } >> "$LEDGER"

    : > "$VD/verdicts.txt"
    echo "$BENCH_MAP" | while read -r bench snake camel; do
        for path in write read; do
            name="${path}_${snake}"
            if [ "$bench" = message_batch ] && [ "$path" = read ]; then
                name="read_message_into"   # the batch read rides the into-path
            fi
            n="$(count_for "${#name}${name}")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n")" >> "$VD/verdicts.txt"
            elif [ "$(count_total)" -eq 0 ]; then
                echo "$bench $path full" >> "$VD/verdicts.txt"
            else
                echo "note: rust $bench $path: no ${name} symbol and runtime calls exist elsewhere — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    backfill
    ;;

# --------------------------------------------------------------------- C#
cs)
    command -v dotnet >/dev/null 2>&1 || { echo "no dotnet toolchain" >&2; exit 1; }
    # §4.1: release runtime, tiering off, disasm of the generated dispatch
    # surface to a file. One quick --round pass JITs every method at FullOpts.
    JIT="$VD/jit.txt"
    ( cd bench/cs && \
      DOTNET_TieredCompilation=0 \
      DOTNET_JitDisasm='Example.Schema:*' \
      DOTNET_JitStdOutFile="$OLDPWD/$JIT" \
      dotnet run -c Release --no-build -- --round 0 > /dev/null 2>&1 ) || true
    [ -s "$JIT" ] || { echo "JitDisasm produced nothing — is the Release build present?" >&2; exit 1; }

    # translate JitDisasm blocks into the otool shape. Per §4.1 the C# count
    # is bl AND blr in the method body; blr targets are opaque, so they are
    # counted as runtime calls rather than silently dropped.
    awk '
        /^; Assembly listing for method / {
            m = $0; sub(/^; Assembly listing for method /, "", m); sub(/\(.*/, "", m)
            print m ":"; next
        }
        $1 == "bl"  { print "0 bl " $2 }
        $1 == "blr" { print "0 blr indirect" }
    ' "$JIT" > "$VD/calls.txt"
    count_calls '.' 'Example\.Schema:' < "$VD/calls.txt" > "$VD/counts.txt"
    # ('.' after the helper carve-out: every non-helper bl counts — Serialize.*
    # methods, JIT helpers, BCL — plus blr via the indirect column, folded in
    # below, because §4.1 counts them all for C#.)

    # fold indirect (blr) counts into the per-method transitive totals
    awk '$1 == "SYM" { $4 += $5; print }' "$VD/counts.txt" > "$VD/counts.folded" \
        && mv "$VD/counts.folded" "$VD/counts.txt"

    section cs "(DOTNET_JitDisasm, DOTNET_TieredCompilation=0, FullOpts; bl+blr per §4.1)"
    {
        echo "# method / direct-calls / per-op-total-with-indirect / indirect (nonzero only)"
        nonzero_symbols
        echo "# JIT inlinee summaries per method:"
        awk '/^; Assembly listing for method /{m=$0; sub(/^; Assembly listing for method /,"",m); sub(/ .*/,"",m)} /inlinees/{print "jit " m "  " substr($0,3)}' "$JIT" | head -40
    } >> "$LEDGER"

    : > "$VD/verdicts.txt"
    echo "$BENCH_MAP" | while read -r bench snake camel; do
        for path in write read; do
            Cap="$(echo "$path" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
            n="$(count_for "^Example\\.Schema:${Cap}${camel}\$")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n")" >> "$VD/verdicts.txt"
            else
                echo "note: cs $bench $path: Example.Schema:${Cap}${camel} not in the JitDisasm output — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    backfill
    ;;

*)
    echo "unknown language: $LANG_ARG (c|cpp|go|rust|cs)" >&2
    exit 1
    ;;
esac

echo "ledger: $LEDGER" >&2
