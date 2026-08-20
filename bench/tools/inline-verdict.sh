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
#      reports them. Counts are kept HOT and COLD separately (§4.2): a cold
#      call — error constructor, guarded slow-path fallback, compiler-split
#      cold chunk — is recorded per row beside the hot count, never inside it.
#
#   2. the inline column of <results.csv>, backfilled per row: full when the
#      emitted code for that benchmark's operation contains zero HOT calls
#      into the serialize runtime, partial:N when N hot calls remain, unknown
#      when the verdict could not be attributed. unknown stays un-ratioable —
#      that is the §4.2 contract, not a shrug, so this script REFUSES rather
#      than guesses whenever attribution would be a guess.
#
# Cold classification (§4.2) is by TARGET, per language, and only from signals
# the toolchain actually emits — a call with no signal stays hot, because
# misclassifying hot-as-cold is the direction that publishes a false full:
#   c/cpp: the target is a compiler-split cold-path symbol (`foo.cold` /
#          `foo.cold.N` — Mach-O's spelling of ELF's .text.unlikely section)
#   rust:  the target is a fn carrying #[cold] in the runtime source this leg
#          built against (matched by its <len><name> v0 mangling token,
#          scanned from the crate the bench Cargo.toml points at), or a
#          split-suffix symbol as above
#   go/cs: no cold signal exists in the gc or JitDisasm surface — every call
#          stays hot, and the ledger says so
#
# Ground truth is the §4.1 universal fallback: call instructions counted in
# the emitted code (otool -tv on this arm64 Mac; go tool objdump for Go;
# DOTNET_JitDisasm for C#) — bl/blr, AND a tail-branch b into another
# function (issue #86: the callee was not inlined, the transfer just reused
# the caller's frame; a b to a local label is control flow, never a call).
# Compiler remarks are advisory ledger content;
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

# Library mode (INLINE_VERDICT_LIB=1): a sourcing script — the inline-gate's
# adversarial fixtures (issue #25) — gets the counting/verdict machinery
# DEFINED but nothing executed: no arguments, no CSV, no toolchain probing,
# and a `return` before the per-language pass below. The fixtures must attack
# THIS code, not a copy of it — a copy would drift and prove nothing.
if [ -z "${INLINE_VERDICT_LIB:-}" ]; then
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

    # §3.5 provenance mechanics: the same SERIALIZE* resolution and override
    # build arguments (RS_CARGO_ARGS, GO_MODFILE_ARG) the measured pass used —
    # the shadow/remark rebuilds below must compile against the SAME runtime
    # checkout as the measured binary, or the remarks describe the wrong code.
    . bench/tools/runtime-paths.sh
    runtime_paths_init

    # The native-binary parsing below is otool/arm64-shaped (bl/blr/b). On a
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
fi

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

# families rt and bits: bench -> the runner's noinline timed-loop symbol stems
# (<snake>_write_loop/_read_loop for c/cpp/rust; main.<lowerCamel>WriteLoop for
# go; Program:<Camel>WriteLoop for cs). The loop bodies ARE §4.1's "emitted
# body of the timed loop", so the verdict is a direct transitive count — no
# atos attribution needed for these rows in any language.
RT_MAP='bench_packet rt_bench_packet rtBenchPacket RtBenchPacket
bench_ints rt_bench_ints rtBenchInts RtBenchInts
bench_bits rt_bench_bits rtBenchBits RtBenchBits
bench_mixed rt_bench_mixed rtBenchMixed RtBenchMixed
bitpacker bitpacker bitpacker Bitpacker'

# ---- per-symbol transitive runtime-call counting ----
# stdin: "sym:" header lines and "addr <bl|blr|b> target" instruction lines
# (otool -tv shape; the go/cs branches translate into it). A bare b whose
# target is another function's symbol is a TAIL CALL and counts exactly as
# bl (issue #86); a b to a raw hex address, to the function's own entry, or
# between a function and its own split .cold chunk is intra-function control
# flow and never counts. RTPAT = runtime
# target regex, HELPER = generated helper regex, COLD = cold-target regex
# ("" = no cold signal in this language; everything stays hot). The awk-side
# name is RTPAT, never RT: RT is a BUILT-IN in gawk (the record terminator,
# overwritten on every record read), so an awk variable named RT is silently
# clobbered to "\n" after the first line and every runtime call counts zero —
# exactly the false-full direction this machinery exists to refuse. Ubuntu CI
# runs gawk as awk; the selftest caught this live. Output, one
# line per symbol (zero counts included, so an entry that fully inlined the
# runtime is a recorded 0, not an absence), plus the raw helper edges
# (perop_for divides unroll back out of the timed loops with them):
#   SYM <symbol> <direct-hot> <transitive-hot> <indirect> <direct-cold> <transitive-cold>
#   EDGE <symbol> <helper-target> <emitted-call-sites>
# Cold rules (§4.2): a call whose target matches COLD (and is a runtime or
# helper target — cold calls into libc/panic machinery are outside the
# serialize chain, exactly as their hot twins are) counts cold, once — the
# body of a cold target never runs per op, so its own calls are not
# propagated. A hot helper propagates BOTH its hot and its cold counts to its
# callers. A target matching no signal counts hot: never guess cold.
count_calls() {
    awk -v RTPAT="$1" -v HELPER="$2" -v COLD="${3:-}" '
        function iscold(t) { return COLD != "" && t ~ COLD }
        # one call-target policy for bl and tail-branch b (issue #86).
        # cold first: a compiler-split chunk of a helper matches both
        # HELPER and COLD, and must count cold, never become a hot edge.
        # helper next: the cs branch passes a catch-all RTPAT, and helper
        # calls must become edges, never direct counts (the regexes are
        # disjoint for every other language)
        function count_target(t) {
            if (iscold(t)) { if (t ~ RTPAT || t ~ HELPER) coldn[sym]++ }
            else if (t ~ HELPER) edge[sym SUBSEP t]++
            else if (t ~ RTPAT) direct[sym]++
        }
        /^[^ \t].*:$/ { sym = substr($0, 1, length($0) - 1); syms[sym] = 1; next }
        $2 == "bl" && sym != "" { count_target($3) }
        # a tail branch into another function is a call the verdict must
        # count (issue #86): the callee was NOT inlined — the transfer just
        # reused the caller frame. Only a b whose target is ANOTHER function
        # symbol is a call: a b to a raw address (otool prints intra-function
        # targets as hex), a b back to the function own entry (its loop
        # backedge), and a b between a function and its own split chunk
        # (foo <-> foo.cold/.cold.N — one logical function) are control
        # flow; b.cond forms never match $2 == "b" at all.
        $2 == "b" && sym != "" {
            t = $3
            if (t ~ /^0x/ || t == sym) next
            if (index(t, sym ".") == 1 || index(sym, t ".") == 1) next
            count_target(t)
        }
        $2 == "blr" && sym != "" { indirect[sym]++ }
        END {
            for (s in syms) { total[s] = direct[s]; ctotal[s] = coldn[s] }
            for (i = 0; i < 8; i++) {            # fixpoint over the helper DAG
                for (s in syms) { fresh[s] = direct[s]; cfresh[s] = coldn[s] }
                for (k in edge) {
                    split(k, a, SUBSEP)
                    fresh[a[1]] += edge[k] * total[a[2]]
                    cfresh[a[1]] += edge[k] * ctotal[a[2]]
                }
                for (s in syms) { total[s] = fresh[s]; ctotal[s] = cfresh[s] }
            }
            for (s in syms)
                printf "SYM %s %d %d %d %d %d\n", s, direct[s] + 0, total[s] + 0, indirect[s] + 0, coldn[s] + 0, ctotal[s] + 0
            for (k in edge) {
                split(k, a, SUBSEP)
                printf "EDGE %s %s %d\n", a[1], a[2], edge[k]
            }
        }'
}

# ---- generated-unit spelling: HELPER regexes and entry-point patterns ----
# Library functions (the gate's selftest fixtures attack these, not copies).
#
# unit_helper_re <lang>: the HELPER regex count_calls uses to tell GENERATED
# code from the serialize runtime. Getting it wrong is not a wrong number, it
# is a DROPPED one — a call target matching neither RTPAT nor HELPER counts as
# nothing at all, so a row whose runtime calls flow through such a target
# publishes a false `full`.
#
# unit_go_sym / unit_cs_sym / unit_rust_sym: the pattern that finds ONE bench
# row's generated entry point in that language's symbol table.
unit_helper_re() {
    case "$1" in
    c)    echo '^_?(write|read|quantize|rt_|vary_|bits_)' ;;   # generated helpers (write_vec3 ...) and the hand-written rt/bits ops
    cpp)  echo '^__?ZNK?7example|_GLOBAL__N_1' ;;              # namespace example (generated) + the anon-namespace rt/bits code
    go)   echo '^example[.]|^main[.]' ;;
    rust) echo '^CRATE_(example|benchrust)_' ;;
    cs)   echo '(Example[.]Schema|Program):' ;;
    *)    return 1 ;;
    esac
}

# go: the objdump symbol for <unit>'s <Cap><camel> generated entry point.
# cs: the JitDisasm method for the same.
# rust: the v0 mangling token (<len><name>) for <unit>'s <fn-name>.
#
# PRE-#104: every one of these knows exactly ONE generated unit and hardcodes
# its prefix — the <unit> argument is accepted and IGNORED. That is precisely
# the defect issue #104 names: the estate benches a second generated unit (the
# RealWorld corpus the real_packet row measures) whose symbols carry different
# package/namespace/crate prefixes, and a map that cannot spell them leaves
# the densest compressed-float workload on inline=unknown. Extracted verbatim
# from the per-language branches so the gate's fixtures attack the real
# spelling rather than a copy of it.
unit_go_sym() {     # <unit> <Cap> <camel>
    echo "^example[.]${2}${3}\$"
}
unit_cs_sym() {     # <unit> <Cap> <camel>
    echo "^Example[.]Schema:${2}${3}\$"
}
unit_rust_sym() {   # <unit> <fn-name>
    echo "${#2}$2"
}

# ---- objdump/JitDisasm -> otool-shape translation ----
# Library functions (the gate's selftest fixtures attack these, not copies):
# each turns one toolchain's disassembly into the "sym:" / "addr op target"
# shape count_calls parses.

# go tool objdump: TEXT headers become symbol headers; CALL name(SB) is a
# direct call, CALL Rn an indirect one; JMP name(SB) is a tail transfer
# into another function and counts as a call (issue #86) — the n(PC) and
# (Rn) JMP forms are intra-function control flow and trampolines, dropped.
go_translate() {
    awk '
        /^TEXT / { name = $2; sub(/\(SB\)$/, "", name); print name ":"; next }
        {
            for (i = 1; i <= NF; i++) {
                if ($i == "CALL") {
                    t = $(i + 1); sub(/\(SB\)$/, "", t)
                    if (t ~ /^[A-Z]+[0-9]*$/) print "0 blr " t   # CALL R26: indirect
                    else print "0 bl " t
                    break
                }
                if ($i == "JMP") {
                    t = $(i + 1)
                    if (t ~ /\(SB\)$/) { sub(/\(SB\)$/, "", t); print "0 b " t }
                    break
                }
            }
        }'
}

# DOTNET_JitDisasm: method-listing headers become symbol headers; bl and blr
# lines pass through (per §4.1 the C# count is bl AND blr in the method body;
# blr targets are opaque, so they are counted rather than silently dropped).
# A b past the G_M insGroup labels is a tail transfer out of the method
# (issue #86): every intra-method branch in JitDisasm goes to a G_M label,
# so a named target counts as a call and a raw-address target is as opaque
# as blr and counts the same way — dropping either would fake a full.
cs_translate() {
    awk '
        /^; Assembly listing for method / {
            m = $0; sub(/^; Assembly listing for method /, "", m); sub(/\(.*/, "", m)
            print m ":"; next
        }
        $1 == "bl"  { print "0 bl " $2 }
        $1 == "blr" { print "0 blr indirect" }
        $1 == "b" {
            t = $2
            if (t ~ /^G_M/) next            # insGroup label: control flow
            gsub(/[][]/, "", t)
            if (t ~ /^0x/ || t ~ /^[0-9]/) print "0 blr indirect"
            else print "0 b " t
        }'
}

# otool -tv of a rust binary -> the otool shape, with every symbol header AND
# every call target renamed CRATE_<class>_<sym> by its DEFINING crate.
#
# v0 mangling puts the defining crate first and the instantiating crate last,
# and serialize types appear inside the generated units' generics — so
# classify every name by its EARLIEST crate token, and refuse the naive
# substring match that would count an example shim mentioning WriteStream as a
# runtime call. Symbol HEADERS get the identical prefix, because count_calls
# joins helper-call targets against header names: leave the headers raw and
# every helper edge looks up a name that was never populated, contributes
# zero, and each loop whose runtime calls flow through an out-of-line helper
# publishes a false `full` (selftest_join is the assertion that catches that).
# A name matching no known crate classifies `other` — and `other` is neither
# runtime nor helper, so its calls are DROPPED: a generated unit missing from
# the token list below is a silent false full, not a wrong number.
rust_translate() {
    awk '
        function firstcrate(t,    best, cls, i, n, names, m, ncls) {
            ncls = split("9serialize 7example 9benchrust 4core 3std 5alloc", names, " ")
            best = 1000000; cls = "other"
            for (i = 1; i <= ncls; i++) {
                n = index(t, names[i])
                if (n > 0 && n < best) { best = n; m = names[i]; cls = substr(m, 2) }
            }
            return cls
        }
        /^[^ \t].*:$/ {
            s = substr($0, 1, length($0) - 1)
            print "CRATE_" firstcrate(s) "_" s ":"
            next
        }
        $2 == "bl"  { print $1 " bl CRATE_" firstcrate($3) "_" $3; next }
        $2 == "blr" { print $1 " blr indirect"; next }
        # tail branches (issue #86): a b to a raw address is intra-function
        # control flow, dropped here; a b to a symbol gets the same crate
        # rename as bl targets so count_calls sees one namespace (its own
        # self/own-chunk guards then apply on the renamed names)
        $2 == "b" && $3 !~ /^0x/ { print $1 " b CRATE_" firstcrate($3) "_" $3; next }
    '
}

# verdict from a transitive HOT count: 0 -> full, N -> partial:N
verdict_of() {
    if [ "$1" -eq 0 ]; then echo "full"; else echo "partial:$1"; fi
}

# the c/cpp/rust split-suffix cold signal (Mach-O's .text.unlikely): clang
# names the outlined cold path of foo `foo.cold` or `foo.cold.N`. The dot is
# a bracket expression, not \.: this value crosses awk -v, where POSIX leaves
# backslash escapes undefined — gawk strips the backslash to a bare `.` (any
# char) with a warning, while mawk/BSD keep `\.` — [.] means the same literal
# dot in every dialect.
COLD_SPLIT='[.]cold([.][0-9]+)?$'

# rust: cold fns scanned from the runtime source the bench built against —
# a `#[cold]` ATTRIBUTE LINE (anchored: a comment merely mentioning #[cold]
# is not an attribute) followed within a few lines by an anchored fn item,
# matched in v0 mangling as <len><name> with a non-digit boundary so a longer
# identifier cannot alias the token. Prints a regex alternation, or just the
# split-suffix signal when the crate marks nothing cold.
rust_cold_regex() {
    local crate_dir="$1" names
    names="$(awk '
        /^[ \t]*#\[cold\]/ { pending = 4; next }
        pending > 0 {
            if ($0 ~ /^[ \t]*(pub(\([a-z]+\))? +)?(default +)?(const +)?(async +)?(unsafe +)?(extern +"[^"]*" +)?fn +[A-Za-z0-9_]+/ \
                && match($0, /fn +[A-Za-z0-9_]+/)) {
                name = substr($0, RSTART + 2, RLENGTH - 2)
                sub(/^ +/, "", name)
                print length(name) name
                pending = 0
            } else if ($0 ~ /^[ \t]*#\[/) {
                # another attribute between #[cold] and its fn: keep waiting
            } else pending--
        }' "$crate_dir"/src/*.rs 2>/dev/null | sort -u | paste -sd'|' -)"
    if [ -n "$names" ]; then
        echo "[^0-9]($names)|$COLD_SPLIT"
    else
        echo "$COLD_SPLIT"
    fi
}

# ---- SELF-TEST: the join between symbol HEADERS and call TARGETS ----
# The bug this exists to catch (and did not exist to catch, once): a branch
# renames call targets into a namespace the symbol headers were never put in.
# count_calls then looks up total[] for names that were never populated, every
# helper edge contributes zero, and each loop whose runtime calls all flow
# through an out-of-line helper publishes a false `full`. Two assertions:
#   1. helper-typed call targets must resolve to symbol headers at all;
#   2. at least one symbol whose immediate helper has nonzero direct runtime
#      calls must itself report a nonzero transitive count.
# Either failing means the fixpoint join is broken — exit 1, verdicts unusable.
#   $1 = translated disasm (otool shape), $2 = counts.txt, $3 = HELPER regex
selftest_join() {
    awk -v HELPER="$3" -v countsf="$2" -v lang="$LANG_ARG" '
        BEGIN {
            while ((getline line < countsf) > 0) {
                n = split(line, a, " ")
                # hot + cold together: the join proof cares that helper edges
                # carry ANY runtime contribution, whichever temperature
                if (a[1] == "SYM" && n >= 7) { known[a[2]] = 1; direct[a[2]] = a[3] + a[6]; trans[a[2]] = a[4] + a[7] }
            }
        }
        /^[^ \t].*:$/ { sym = substr($0, 1, length($0) - 1); next }
        $2 == "bl" && sym != "" && $3 ~ HELPER { edge[sym SUBSEP $3] = 1; tgt[$3] = 1; nedge++ }
        # tail-branch helper edges (issue #86), under the same guards
        # count_calls applies — the join proof must see the same edge set
        $2 == "b" && sym != "" && $3 !~ /^0x/ && $3 != sym && \
            index($3, sym ".") != 1 && index(sym, $3 ".") != 1 && \
            $3 ~ HELPER { edge[sym SUBSEP $3] = 1; tgt[$3] = 1; nedge++ }
        END {
            if (nedge == 0) exit 0      # no helper edges anywhere: nothing to prove
            resolved = 0
            for (t in tgt) if (t in known) resolved++
            if (resolved == 0) {
                printf "inline-verdict SELF-TEST FAILED (%s): %d helper-call targets, and not one matches any symbol header — call targets and symbol headers are in different namespaces, so every helper edge joins to nothing and each loop would publish a false full\n", lang, nedge > "/dev/stderr"
                exit 1
            }
            expected = 0; witness = 0
            for (k in edge) {
                split(k, a, SUBSEP)
                if (direct[a[2]] + 0 > 0) {
                    expected++
                    if (trans[a[1]] + 0 > 0) witness++
                    else { badcaller = a[1]; badhelper = a[2] }
                }
            }
            if (expected > 0 && witness == 0) {
                printf "inline-verdict SELF-TEST FAILED (%s): %s calls helper %s, whose own direct runtime calls are nonzero, yet reports transitive 0 — the helper-edge join dropped the contribution\n", lang, badcaller, badhelper > "/dev/stderr"
                exit 1
            }
            exit 0
        }' "$1"
}

# transitive HOT count for the first symbol matching the regex; -1 if absent
count_for() {
    awk -v pat="$1" '$1 == "SYM" && $2 ~ pat { print $4; found = 1; exit } END { if (!found) print -1 }' "$VD/counts.txt"
}

# transitive COLD count for the first symbol matching the regex; 0 if absent
# (the verdict never keys on cold — it is ledger content beside the hot count)
cold_for() {
    awk -v pat="$1" '$1 == "SYM" && $2 ~ pat { print $7 + 0; found = 1; exit } END { if (!found) print 0 }' "$VD/counts.txt"
}

# PER-OP transitive count for a timed-loop symbol (§4.2: partial:N means N
# runtime calls PER OP, never N call sites in the emitted loop body). An
# unrolled loop repeats its per-op calls once per unrolled iteration — clang
# unrolled the C rt_bench_packet_read_loop 4x and the raw body count
# published partial:12 where the per-op truth is 3. §3.2 makes every
# out-of-line helper called from a timed loop a once-per-op call in source,
# so the smallest per-helper emitted edge count out of the loop body IS the
# unroll factor (peeled prologue/epilogue iterations only raise every count
# equally), and the loop's transitive count divides by it. A loop with no
# helper edges has no unroll witness and reports its body count unchanged.
# Prints -1 if the loop symbol is absent; -2 if the count does not divide
# by the unroll factor (not attributable — refusing beats guessing).
# The hot count is the verdict; perop_cold_for below is its ledger twin.
perop_for() {
    awk -v pat="$1" '
        $1 == "SYM" && !loopset && $2 ~ pat { loop = $2; loopset = 1; T = $4 }
        $1 == "EDGE" { e_sym[++ne] = $2; e_cnt[ne] = $4 }
        END {
            if (!loopset) { print -1; exit }
            k = 0
            for (i = 1; i <= ne; i++)
                if (e_sym[i] == loop && (k == 0 || e_cnt[i] < k)) k = e_cnt[i]
            if (k <= 1) { print T + 0; exit }
            if (T % k != 0) { print -2; exit }
            print T / k
        }' "$VD/counts.txt"
}

# PER-OP transitive COLD count for a timed-loop symbol, divided by the same
# unroll witness perop_for uses. Prints 0 if the loop is absent (nothing to
# record); "raw:N" when the cold count does not divide by the unroll factor —
# the ledger records it raw and says so, because cold is bookkeeping beside
# the verdict, never the verdict itself.
perop_cold_for() {
    awk -v pat="$1" '
        $1 == "SYM" && !loopset && $2 ~ pat { loop = $2; loopset = 1; TC = $7 }
        $1 == "EDGE" { e_sym[++ne] = $2; e_cnt[ne] = $4 }
        END {
            if (!loopset) { print 0; exit }
            k = 0
            for (i = 1; i <= ne; i++)
                if (e_sym[i] == loop && (k == 0 || e_cnt[i] < k)) k = e_cnt[i]
            if (k <= 1) { print TC + 0; exit }
            if (TC % k != 0) { print "raw:" TC; exit }
            print TC / k
        }' "$VD/counts.txt"
}

# sum of transitive HOT counts across every symbol (the whole-binary fallback;
# a binary whose only remaining serialize calls are cold is hot-clean)
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
            echo "# generated helpers, classified HOT or COLD by target (§4.2: cold ="
            echo "# split cold-path symbol, #[cold]-marked fn, or a documented per-"
            echo "# language equivalent; no signal = hot, never guessed). Compiler"
            echo "# remarks are advisory; the disassembly is the verdict. CSV verdicts"
            echo "# count HOT calls only: full = zero hot runtime calls per op,"
            echo "# partial:N = N hot calls remain PER OP (§4.2 — loop unrolling"
            echo "# divided back out), unknown = not attributable (stays un-ratioable)."
            echo "# The cold count rides beside every row here, never in the CSV shape."
        } > "$LEDGER"
    fi
}

# per-row ledger lines from verdicts.txt (<bench> <path> <verdict> <hot> <cold>)
emit_rows() {
    {
        echo "# per-row verdicts (hot is the CSV verdict; cold is recorded debt off the hot path):"
        awk -v lang="$LANG_ARG" '{ printf "row %s %s %s %s hot=%s cold=%s\n", lang, $1, $2, $3, $4, ($5 == "" ? 0 : $5) }' "$VD/verdicts.txt"
    } >> "$LEDGER"
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
    awk '$1 == "SYM" && $3 + $4 + $5 + $6 + $7 > 0' "$VD/counts.txt" | sort -k4 -rn | sed 's/^SYM /symbol /'
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

# Library mode ends here: every function above is defined, nothing below runs.
if [ -n "${INLINE_VERDICT_LIB:-}" ]; then
    return 0 2>/dev/null || exit 0
fi

case "$LANG_ARG" in

# --------------------------------------------------------------- C and C++
c|cpp)
    if [ "$LANG_ARG" = c ]; then
        BIN=build/bench/schema_bench_c
        RT='^_?serialize_'                  # serialize.c entry points
    else
        BIN=build/bench/schema_bench_cpp
        RT='^__?ZNK?9serialize'             # namespace serialize (runtime proper)
    fi
    HELPER="$(unit_helper_re "$LANG_ARG")" || exit 1
    [ -x "$BIN" ] || { echo "$BIN missing — run the pass first" >&2; exit 1; }
    otool -tv "$BIN" > "$VD/disasm.txt"
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$VD/disasm.txt" > "$VD/counts.txt"
    selftest_join "$VD/disasm.txt" "$VD/counts.txt" "$HELPER" || exit 1

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

    section "$LANG_ARG" "(otool -tv of $BIN; clang -Rpass=inline shadow compile; cold = split .cold/.cold.N symbols)"
    {
        echo "# symbol / direct-hot / transitive-hot / indirect / direct-cold / transitive-cold (nonzero only)"
        nonzero_symbols
        echo "# missed-inline remarks naming serialize (callee, caller, cost where reported):"
        grep -E "not inlined into" "$REMARKS" 2>/dev/null | grep -i serialize \
            | sed -E "s/^[^:]*:[0-9]+:[0-9]+: remark: //; s/ \[-Rpass.*//" \
            | sort | uniq -c | sort -rn | head -40 | sed 's/^ */remark /' \
            || true
    } >> "$LEDGER"

    : > "$VD/verdicts.txt"
    if [ "$LANG_ARG" = c ]; then
        # C per-row verdicts by walking the -g shadow's inline stacks — the
        # same technique as the cpp branch below, live for C since
        # bench/c/bench_message.inc gave every expansion real line numbers
        # (the old BENCH_MESSAGE macro put every statement of every expansion
        # on one line, which is why chat/test/batch rows used to go unknown
        # whenever their entry symbols inlined away). Each bench has its own
        # bench_message_<suffix> function, so the frame NAME carries the
        # bench and the .inc line carries timed-write / timed-read / untimed.
        [ -x "$VD/shadow" ] || { echo "shadow build failed — cannot attribute c verdicts (see $REMARKS)" >&2; exit 1; }
        otool -tv "$VD/shadow" > "$VD/shadow-disasm.txt"
        count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$VD/shadow-disasm.txt" > "$VD/shadow-counts.txt"
        selftest_join "$VD/shadow-disasm.txt" "$VD/shadow-counts.txt" "$HELPER" || exit 1

        # every remaining runtime/helper call site in the shadow text —
        # tail-branch b sites included (issue #86), under the same guards
        # count_calls applies: raw-address, own-entry, and own-split-chunk
        # b forms are control flow, not call sites
        awk -v RTPAT="$RT" -v HELPER="$HELPER" '
            /^[^ \t].*:$/ { sym = substr($0, 1, length($0) - 1); next }
            $2 == "bl" && ($3 ~ RTPAT || $3 ~ HELPER) { print $1, $3 }
            $2 == "b" && $3 !~ /^0x/ && $3 != sym && \
                index($3, sym ".") != 1 && index(sym, $3 ".") != 1 && \
                ($3 ~ RTPAT || $3 ~ HELPER) { print $1, $3 }
        ' "$VD/shadow-disasm.txt" > "$VD/addrs.txt"

        # drift guard, as cpp: hot+cold totals must match the measured binary
        M_TOTAL="$(awk '$1 == "SYM" { s += $4 + $7 } END { print s + 0 }' "$VD/counts.txt")"
        S_TOTAL="$(awk '$1 == "SYM" { s += $4 + $7 } END { print s + 0 }' "$VD/shadow-counts.txt")"
        if [ "$M_TOTAL" != "$S_TOTAL" ]; then
            echo "note: SHADOW DRIFT: measured binary has $M_TOTAL transitive runtime calls (hot+cold), -g shadow has $S_TOTAL — verdicts below describe the shadow" >> "$LEDGER"
        fi

        # timed-loop line ranges: write/read loops inside the include
        # template; batch loops inside bench_batch in bench_main.c
        INC=bench/c/bench_message.inc
        SRC=bench/c/bench_main.c
        # tail -1: the markers are the ones in CODE — a doc line quoting them
        # higher up must not win (it did once: every timed site went untimed
        # and the gen rows published full; the range sanity check below is
        # the guard that turns that bug into a refusal)
        W1=$(grep -n 'write path: 1 warmup' "$INC" | cut -d: -f1 | tail -1)
        W2=$(grep -n 'read path: 1 warmup' "$INC" | cut -d: -f1 | tail -1)
        RE=$(grep -n 'report( name, "write"' "$INC" | cut -d: -f1 | tail -1)
        B1=$(grep -n 'write path: whole batch' "$SRC" | cut -d: -f1 | head -1)
        B2=$(grep -n 'the read buffer: rebuild' "$SRC" | cut -d: -f1 | head -1)
        B3=$(grep -n 'read path: read messages' "$SRC" | cut -d: -f1 | head -1)
        B4=$(grep -n 'report( "message_batch"' "$SRC" | cut -d: -f1 | head -1)
        if [ -z "$W1" ] || [ -z "$W2" ] || [ -z "$RE" ] || [ "$W1" -ge "$W2" ] || [ "$W2" -ge "$RE" ] || \
           [ -z "$B1" ] || [ -z "$B2" ] || [ -z "$B3" ] || [ -z "$B4" ] || [ "$B1" -ge "$B2" ] || [ "$B2" -ge "$B3" ] || [ "$B3" -ge "$B4" ]; then
            echo "c attribution: timed-range markers missing or out of order (W1=$W1 W2=$W2 RE=$RE B=$B1/$B2/$B3/$B4) — inline stays unknown" >&2
            echo "note: c attribution marker failure (W1=$W1 W2=$W2 RE=$RE B=$B1/$B2/$B3/$B4) — all c gen rows left unknown" >> "$LEDGER"
            exit 1
        fi

        # inline stacks for every call site, batched through atos
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

        awk -v RTPAT="$RT" -v COLD="$COLD_SPLIT" -v addrsf="$VD/addrs.txt" -v countsf="$VD/shadow-counts.txt" \
            -v W1="$W1" -v W2="$W2" -v RE="$RE" -v B1="$B1" -v B2="$B2" -v B3="$B3" -v B4="$B4" '
            BEGIN {
                while ((getline line < addrsf) > 0) { na++; split(line, a, " "); target[na] = a[2] }
                while ((getline line < countsf) > 0) { split(line, a, " "); if (a[1] == "SYM") { tot[a[2]] = a[4]; ctot[a[2]] = a[7] } }
                # bench_message_<suffix> -> CSV bench name
                bmap["rigidbody"] = "rigidbody_moving"
                bmap["rigidbody_at_rest"] = "rigidbody_at_rest"
                bmap["chat"] = "chat"; bmap["test"] = "test"
                bmap["inputpacket"] = "inputpacket"; bmap["shipcreate"] = "shipcreate"
                bmap["ship_shallow"] = "ship_shallow"; bmap["probe_header"] = "probe_header"
                bmap["probebits"] = "probebits"; bmap["probearray"] = "probearray"
                bmap["testdata"] = "testdata"
                g = 0
            }
            function flush_group(    i, f, bench, path, L, sfx, t) {
                if (nf == 0) return
                g++
                bench = ""; path = ""
                for (i = 1; i <= nf; i++) {
                    f = frames[i]
                    if (match(f, /bench_message_[a-z_]+/)) {
                        sfx = substr(f, RSTART + 14, RLENGTH - 14)
                        if (match(f, /bench_message\.inc:[0-9]+/)) {
                            L = substr(f, RSTART + 18, RLENGTH - 18) + 0
                            if (L >= W1 && L < W2) path = "write"
                            else if (L >= W2 && L < RE) path = "read"
                            else { untimed++; nf = 0; return }
                            bench = bmap[sfx]
                        }
                        break
                    }
                    if (f ~ /bench_batch/ && match(f, /bench_main\.c:[0-9]+/)) {
                        L = substr(f, RSTART + 13, RLENGTH - 13) + 0
                        bench = "message_batch"
                        if (L >= B1 && L < B2) path = "write"
                        else if (L >= B3 && L < B4) path = "read"
                        else { untimed++; nf = 0; return }
                        break
                    }
                }
                if (bench == "" || path == "") { if (i <= nf) unattributed++; else untimed++; nf = 0; return }
                t = target[g]
                seen[bench "," path] = 1
                # §4.2 unroll analog for attribution: an unrolled timed loop
                # repeats each source-level call site once per unrolled copy,
                # and every copy carries the SAME inline stack and target —
                # so distinct (stack, target) signatures are the per-op
                # sites, and repeats are unroll copies, counted once. (The C
                # read loops measure unrolled 4x; raw counting published
                # partial:36 where the per-op truth is 9.)
                sig = t
                for (i = 1; i <= nf; i++) sig = sig SUBSEP frames[i]
                if (sig in sigseen) { dedup++; nf = 0; return }
                sigseen[sig] = 1
                # §4.2 hot/cold, exactly as cpp: split cold chunks count
                # cold once; hot runtime targets count hot; hot helpers
                # contribute their own hot and cold transitive counts
                if (t ~ COLD) {
                    c[bench "," path] += 1
                } else if (t ~ RTPAT) {
                    n[bench "," path] += 1
                } else {
                    n[bench "," path] += tot[t] + 0
                    c[bench "," path] += ctot[t] + 0
                }
                nf = 0
            }
            NF == 0 { flush_group(); next }
            { frames[++nf] = $0 }
            END {
                flush_group()
                for (k in seen) { split(k, a, ","); printf "N %s %s %d %d\n", a[1], a[2], n[k] + 0, c[k] + 0 }
                printf "STATS groups %d untimed %d unattributed %d unroll-dedup %d\n", g, untimed, unattributed, dedup
            }' "$VD/stacks.txt" > "$VD/attribution.txt"

        # the attribution must PROVE it ran — same gates as cpp: a missing
        # STATS line or an incomplete walk would default rows to 0 and fake
        # a full; refuse instead, rows stay unknown.
        if ! grep -q '^STATS groups' "$VD/attribution.txt"; then
            echo "c attribution failed (no STATS line in $VD/attribution.txt) — inline stays unknown" >&2
            echo "note: c attribution FAILED — all c gen rows left unknown" >> "$LEDGER"
            exit 1
        fi
        NADDR="$(wc -l < "$VD/addrs.txt" | tr -d ' ')"
        NACC="$(awk '/^STATS/ { print $3 + 0 }' "$VD/attribution.txt")"
        if [ "$NADDR" != "$NACC" ]; then
            echo "c attribution incomplete: $NACC of $NADDR call sites walked — inline stays unknown" >&2
            echo "note: c attribution INCOMPLETE ($NACC of $NADDR call sites) — all c gen rows left unknown" >> "$LEDGER"
            exit 1
        fi

        {
            echo "# c per-row attribution (atos inline stacks over the -g shadow;"
            echo "# untimed = call sites in setup/self-check/variant code, excluded):"
            sed 's/^/attr /' "$VD/attribution.txt"
        } >> "$LEDGER"

        echo "$BENCH_MAP" | while read -r bench snake camel; do
            for path in write read; do
                set -- $(awk -v b="$bench" -v p="$path" '$1 == "N" && $2 == b && $3 == p { print $4, $5; f = 1; exit } END { if (!f) print 0, 0 }' "$VD/attribution.txt")
                echo "$bench $path $(verdict_of "$1") $1 $2" >> "$VD/verdicts.txt"
            done
        done
        # families rt and bits: the noinline timed-loop symbols ARE the §4.1
        # ground truth, counted PER OP (perop_for divides loop unrolling back
        # out); a loop symbol that vanished stays unknown, honestly.
        echo "$RT_MAP" | while read -r bench snake lower camel; do
            for path in write read; do
                n="$(perop_for "^_?${snake}_${path}_loop\$")"
                cold="$(perop_cold_for "^_?${snake}_${path}_loop\$")"
                if [ "$n" -ge 0 ]; then
                    echo "$bench $path $(verdict_of "$n") $n $cold" >> "$VD/verdicts.txt"
                elif [ "$n" -eq -2 ]; then
                    echo "note: c $bench $path: loop body count does not divide by its unroll factor — inline stays unknown" >> "$LEDGER"
                else
                    echo "note: c $bench $path: ${snake}_${path}_loop not found in otool — inline stays unknown" >> "$LEDGER"
                fi
            done
        done
    else
        # C++ per-row verdicts by walking the -g shadow's inline stacks.
        # clang is deterministic: same compiler + flags + source means the
        # same inlining; -g adds only metadata. atos -i then names, for every
        # remaining call, which bench_message instantiation and which timed
        # loop it sits in — and setup/self-check/variant code is EXCLUDED
        # rather than miscounted.
        [ -x "$VD/shadow" ] || { echo "shadow build failed — cannot attribute cpp verdicts (see $REMARKS)" >&2; exit 1; }
        otool -tv "$VD/shadow" > "$VD/shadow-disasm.txt"
        count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$VD/shadow-disasm.txt" > "$VD/shadow-counts.txt"
        selftest_join "$VD/shadow-disasm.txt" "$VD/shadow-counts.txt" "$HELPER" || exit 1

        # every remaining runtime/helper call site in the shadow text —
        # tail-branch b sites included (issue #86), under the same guards
        # count_calls applies: raw-address, own-entry, and own-split-chunk
        # b forms are control flow, not call sites
        awk -v RTPAT="$RT" -v HELPER="$HELPER" '
            /^[^ \t].*:$/ { sym = substr($0, 1, length($0) - 1); next }
            $2 == "bl" && ($3 ~ RTPAT || $3 ~ HELPER) { print $1, $3 }
            $2 == "b" && $3 !~ /^0x/ && $3 != sym && \
                index($3, sym ".") != 1 && index(sym, $3 ".") != 1 && \
                ($3 ~ RTPAT || $3 ~ HELPER) { print $1, $3 }
        ' "$VD/shadow-disasm.txt" > "$VD/addrs.txt"

        # drift guard: the shadow must have the same call structure as the
        # measured binary, or the attribution below describes the wrong code
        # (hot + cold compared together: a call that changed temperature
        # between the builds is drift too)
        M_TOTAL="$(awk '$1 == "SYM" { s += $4 + $7 } END { print s + 0 }' "$VD/counts.txt")"
        S_TOTAL="$(awk '$1 == "SYM" { s += $4 + $7 } END { print s + 0 }' "$VD/shadow-counts.txt")"
        if [ "$M_TOTAL" != "$S_TOTAL" ]; then
            echo "note: SHADOW DRIFT: measured binary has $M_TOTAL transitive runtime calls (hot+cold), -g shadow has $S_TOTAL — verdicts below describe the shadow" >> "$LEDGER"
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
        # marker sanity (same guard as the c branch): a missing or reordered
        # marker silently reclassifies timed sites as untimed and publishes
        # false fulls — refuse instead
        if [ -z "$W1" ] || [ -z "$W2" ] || [ -z "$RE" ] || [ "$W1" -ge "$W2" ] || [ "$W2" -ge "$RE" ] || \
           [ -z "$B1" ] || [ -z "$B2" ] || [ -z "$B3" ] || [ -z "$B4" ] || [ "$B1" -ge "$B2" ] || [ "$B2" -ge "$B3" ] || [ "$B3" -ge "$B4" ]; then
            echo "cpp attribution: timed-range markers missing or out of order in $SRC (W1=$W1 W2=$W2 RE=$RE B=$B1/$B2/$B3/$B4) — inline stays unknown" >&2
            echo "note: cpp attribution marker failure — all cpp rows left unknown" >> "$LEDGER"
            exit 1
        fi

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

        awk -v RTPAT="$RT" -v COLD="$COLD_SPLIT" -v addrsf="$VD/addrs.txt" -v countsf="$VD/shadow-counts.txt" \
            -v benchf="$VD/benchlines.txt" \
            -v W1="$W1" -v W2="$W2" -v RE="$RE" -v B1="$B1" -v B2="$B2" -v B3="$B3" -v B4="$B4" '
            BEGIN {
                while ((getline line < addrsf) > 0) { na++; split(line, a, " "); target[na] = a[2] }
                while ((getline line < countsf) > 0) { split(line, a, " "); if (a[1] == "SYM") { tot[a[2]] = a[4]; ctot[a[2]] = a[7] } }
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
                seen[bench "," path] = 1
                # §4.2 unroll analog for attribution (same rule as the c
                # branch): distinct (stack, target) signatures are the
                # per-op sites; identical repeats are unrolled copies of one
                # source-level site and count once
                sig = t
                for (i = 1; i <= nf; i++) sig = sig SUBSEP frames[i]
                if (sig in sigseen) { dedup++; nf = 0; return }
                sigseen[sig] = 1
                # §4.2 hot/cold: a split cold-path target counts cold, once;
                # a hot runtime target counts hot; a hot helper contributes
                # its own hot and cold transitive counts separately
                if (t ~ COLD) {
                    c[bench "," path] += 1
                } else if (t ~ RTPAT) {
                    n[bench "," path] += 1
                } else {
                    n[bench "," path] += tot[t] + 0
                    c[bench "," path] += ctot[t] + 0
                }
                nf = 0
            }
            NF == 0 { flush_group(); next }
            { frames[++nf] = $0 }
            END {
                flush_group()
                for (k in seen) { split(k, a, ","); printf "N %s %s %d %d\n", a[1], a[2], n[k] + 0, c[k] + 0 }
                printf "STATS groups %d untimed %d unattributed %d unroll-dedup %d\n", g, untimed, unattributed, dedup
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
                set -- $(awk -v b="$bench" -v p="$path" '$1 == "N" && $2 == b && $3 == p { print $4, $5; f = 1; exit } END { if (!f) print 0, 0 }' "$VD/attribution.txt")
                echo "$bench $path $(verdict_of "$1") $1 $2" >> "$VD/verdicts.txt"
            done
        done
        # families rt and bits: the noinline timed-loop symbols in the
        # MEASURED binary are the §4.1 ground truth — counted PER OP
        # (perop_for divides loop unrolling back out), no atos needed
        # (their call sites show up as untimed in the atos walk).
        echo "$RT_MAP" | while read -r bench snake lower camel; do
            for path in write read; do
                n="$(perop_for "${snake}_${path}_loop")"
                cold="$(perop_cold_for "${snake}_${path}_loop")"
                if [ "$n" -ge 0 ]; then
                    echo "$bench $path $(verdict_of "$n") $n $cold" >> "$VD/verdicts.txt"
                elif [ "$n" -eq -2 ]; then
                    echo "note: cpp $bench $path: loop body count does not divide by its unroll factor — inline stays unknown" >> "$LEDGER"
                else
                    echo "note: cpp $bench $path: ${snake}_${path}_loop not found in otool — inline stays unknown" >> "$LEDGER"
                fi
            done
        done
    fi
    emit_rows
    backfill
    ;;

# --------------------------------------------------------------------- Go
go)
    command -v go >/dev/null 2>&1 || { echo "no go toolchain" >&2; exit 1; }
    # the measured shape: an ordinary optimized build of the runner
    # ($GO_MODFILE_ARG: the §3.5 override modfile when SERIALIZE_GO points
    # away from the default — the same resolution the measured leg used)
    ( cd bench/go && go build $GO_MODFILE_ARG -o "$OLDPWD/$VD/benchgo" . ) || exit 1
    go tool objdump "$VD/benchgo" > "$VD/objdump.txt" 2>/dev/null

    # translate objdump into the otool shape count_calls parses
    go_translate < "$VD/objdump.txt" > "$VD/calls.txt"
    GO_HELPER="$(unit_helper_re go)" || exit 1
    # COLD is empty: gc has no cold attribute, no section split, and no
    # remark that marks a callee cold — §4.2's hot-default applies to every
    # Go call, and the ledger note below says so out loud.
    count_calls 'serialize%2ego[.]|mas-bandwidth/serialize' "$GO_HELPER" '' < "$VD/calls.txt" > "$VD/counts.txt"
    selftest_join "$VD/calls.txt" "$VD/counts.txt" "$GO_HELPER" || exit 1

    # remarks: go build -a -gcflags=-m=2 — -a is MANDATORY (see header)
    ( cd bench/go && go build $GO_MODFILE_ARG -a -gcflags=all=-m=2 -o /dev/null . 2> "$OLDPWD/$VD/remarks.txt" ) || true
    grep -E "serialize\.go/" "$VD/remarks.txt" | grep -E ": can inline|: cannot inline" \
        | sed -E 's/^.*serialize\.go\///' | sed -E 's/ as:.*$//' | sort -u > "$VD/serialize-remarks.txt" || true

    # the §4.1 sanity cross-check: every runtime symbol the disassembly shows
    # as a call target should be one -m=2 could not inline, or one whose
    # callers ran out of budget — and the ledger says which.
    : > "$VD/crosscheck.txt"
    awk '($2 == "bl" || $2 == "b") && $3 ~ /serialize%2ego\./ { print $3 }' "$VD/calls.txt" | sort -u | while read -r target; do
        short="${target##*serialize%2ego.}"
        if grep -qF "cannot inline $short" "$VD/serialize-remarks.txt"; then
            echo "AGREE: $short is a call target and -m=2 says cannot inline" >> "$VD/crosscheck.txt"
        elif grep -qF "can inline $short" "$VD/serialize-remarks.txt"; then
            echo "NOTE: $short can inline by cost yet remains a call target (caller over budget)" >> "$VD/crosscheck.txt"
        else
            echo "DISAGREE: $short is a call target but -m=2 has no verdict for it" >> "$VD/crosscheck.txt"
        fi
    done

    section go "(go tool objdump of the measured build; go build -a -gcflags=all=-m=2; no cold signal exists in gc — every call is hot by §4.2's hot-default)"
    {
        echo "# sanity cross-check (§4.1): the universal ground truth (objdump call"
        echo "# targets) checked against the compiler-remark verdict:"
        sed 's/^/crosscheck /' "$VD/crosscheck.txt"
        echo "# serialize.go remark ledger (file:line, symbol, cost, budget):"
        sed 's/^/remark /' "$VD/serialize-remarks.txt"
        echo "# symbol / direct-hot / transitive-hot / indirect / direct-cold / transitive-cold (generated code, nonzero)"
        grep "^SYM example\." "$VD/counts.txt" | awk '$3 + $4 + $5 + $6 + $7 > 0' | sort -k4 -rn | sed 's/^SYM /symbol /'
    } >> "$LEDGER"

    : > "$VD/verdicts.txt"
    echo "$BENCH_MAP" | while read -r bench snake camel; do
        for path in write read; do
            Cap="$(echo "$path" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
            pat="$(unit_go_sym example "$Cap" "$camel")" || exit 1
            n="$(count_for "$pat")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n") $n 0" >> "$VD/verdicts.txt"
            else
                echo "note: go $bench $path: no objdump symbol matching $pat — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    # families rt and bits: the //go:noinline timed-loop symbols (§4.1),
    # counted PER OP (perop_for; gc does not unroll today, so k stays 1)
    echo "$RT_MAP" | while read -r bench snake lower camel; do
        for path in write read; do
            Cap="$(echo "$path" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
            n="$(perop_for "^main[.]${lower}${Cap}Loop\$")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n") $n 0" >> "$VD/verdicts.txt"
            elif [ "$n" -eq -2 ]; then
                echo "note: go $bench $path: loop body count does not divide by its unroll factor — inline stays unknown" >> "$LEDGER"
            else
                echo "note: go $bench $path: main.${lower}${Cap}Loop not found in objdump — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    emit_rows
    backfill
    ;;

# ------------------------------------------------------------------- Rust
rust)
    BIN=bench/rust/target/release/benchrust
    [ -x "$BIN" ] || { echo "$BIN missing — run the pass first" >&2; exit 1; }

    # the §4.2 cold signal for rust: fns marked #[cold] in the runtime crate
    # THIS bench built against (the §3.5-resolved SERIALIZE_RS checkout —
    # the same resolution the measured leg was verified against), plus any
    # split .cold suffix. Scanned from source because neither Mach-O sections
    # nor -Cremark reasons state the attribute; a fn not carrying #[cold]
    # in that crate stays hot, whatever its name looks like.
    RS_CRATE="$ABS_SERIALIZE_RS"
    [ -n "$RS_CRATE" ] || { echo "SERIALIZE_RS=$SERIALIZE_RS does not exist — cannot scan #[cold]" >&2; exit 1; }
    COLD_RS="$(rust_cold_regex "$RS_CRATE")"

    # rust_translate (library, above) renames every symbol header AND call
    # target by its DEFINING crate, so count_calls sees one namespace.
    otool -tv "$BIN" | rust_translate > "$VD/disasm.txt"
    RS_HELPER="$(unit_helper_re rust)" || exit 1
    count_calls '^CRATE_serialize_' "$RS_HELPER" "$COLD_RS" < "$VD/disasm.txt" > "$VD/counts.txt"
    selftest_join "$VD/disasm.txt" "$VD/counts.txt" "$RS_HELPER" || exit 1

    # remarks: RUSTFLAGS="-Cremark=inline -Cdebuginfo=1" rebuild (fingerprint
    # forces it); runs AFTER the measured binary was disassembled above.
    RUSTUP_BIN="/opt/homebrew/opt/rustup/bin"
    CARGO="cargo"; command -v cargo >/dev/null 2>&1 || CARGO="$RUSTUP_BIN/cargo"
    ( cd bench/rust && \
      RUSTFLAGS="-Cremark=inline -Cdebuginfo=1" CARGO_INCREMENTAL=0 \
      PATH="$RUSTUP_BIN:$PATH" "$CARGO" build --release --quiet $RS_CARGO_ARGS 2> "$OLDPWD/$VD/remarks.txt" ) || true
    grep -E "inline \(missed\)" "$VD/remarks.txt" | grep -F serialize \
        | sed -E 's/^[^:]*:[0-9]+:[0-9]+: *//; s/note: //' \
        | sort | uniq -c | sort -rn | head -40 > "$VD/serialize-remarks.txt" || true

    section rust "(otool -tv of the measured $BIN, targets classified by defining crate; -Cremark=inline shadow rebuild; cold = #[cold] fns scanned from $RS_CRATE + split .cold symbols)"
    {
        echo "# cold-classified targets (regex): $COLD_RS"
        echo "# symbol / direct-hot / transitive-hot / indirect / direct-cold / transitive-cold (nonzero only)"
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
            if [ "$bench" = ship_shallow ]; then
                # the generators disagree on this one name: C emits
                # write_ship_shallow, Rust emits write_ship_data_shallow —
                # the map's snake is the C spelling, and matching it against
                # the Rust binary is what kept both ship_shallow rows unknown
                name="${path}_ship_data_shallow"
            fi
            pat="$(unit_rust_sym example "$name")" || exit 1
            n="$(count_for "$pat")"
            cold="$(cold_for "$pat")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n") $n $cold" >> "$VD/verdicts.txt"
            elif [ "$(count_total)" -eq 0 ]; then
                echo "$bench $path full 0 0" >> "$VD/verdicts.txt"
            else
                echo "note: rust $bench $path: no symbol matching $pat and runtime calls exist elsewhere — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    # families rt and bits: the #[inline(never)] timed-loop symbols (§4.1),
    # counted PER OP (perop_for divides loop unrolling back out)
    echo "$RT_MAP" | while read -r bench snake lower camel; do
        for path in write read; do
            name="${snake}_${path}_loop"
            n="$(perop_for "${#name}${name}")"
            cold="$(perop_cold_for "${#name}${name}")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n") $n $cold" >> "$VD/verdicts.txt"
            elif [ "$n" -eq -2 ]; then
                echo "note: rust $bench $path: loop body count does not divide by its unroll factor — inline stays unknown" >> "$LEDGER"
            else
                echo "note: rust $bench $path: no ${name} symbol — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    emit_rows
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
      DOTNET_JitDisasm='Example.Schema:* Program:*' \
      DOTNET_JitStdOutFile="$OLDPWD/$JIT" \
      dotnet run -c Release --no-build -- --round 0 > /dev/null 2>&1 ) || true
    [ -s "$JIT" ] || { echo "JitDisasm produced nothing — is the Release build present?" >&2; exit 1; }

    # translate JitDisasm blocks into the otool shape (cs_translate above:
    # per §4.1 the C# count is bl AND blr in the method body; blr targets
    # are opaque, so they are counted rather than silently dropped)
    cs_translate < "$JIT" > "$VD/calls.txt"
    CS_HELPER="$(unit_helper_re cs)" || exit 1
    # COLD is empty: JitDisasm names no cold blocks or targets on this
    # surface — §4.2's hot-default applies to every C# call, said out loud
    # in the section header below.
    count_calls '.' "$CS_HELPER" '' < "$VD/calls.txt" > "$VD/counts.txt"
    # ('.' after the helper carve-out: every non-helper bl counts — Serialize.*
    # methods, JIT helpers, BCL — plus blr via the indirect column, folded in
    # below, because §4.1 counts them all for C#. selftest_join is not run
    # here: JitDisasm surfaces managed-to-managed calls as blr or raw
    # addresses, never as named bl targets, so cs has no helper edges for the
    # join self-test to prove anything about — and with a catch-all RT plus
    # folded indirect counts its helper-direct witness would mean something
    # else anyway.)

    # fold indirect (blr) counts into the per-method transitive totals
    # (EDGE lines pass through untouched — perop_for reads them)
    awk '$1 == "SYM" { $4 += $5 } { print }' "$VD/counts.txt" > "$VD/counts.folded" \
        && mv "$VD/counts.folded" "$VD/counts.txt"

    section cs "(DOTNET_JitDisasm, DOTNET_TieredCompilation=0, FullOpts; bl+blr per §4.1; no cold signal exists in JitDisasm — every call is hot by §4.2's hot-default)"
    {
        echo "# method / direct-calls / per-op-total-with-indirect / indirect / direct-cold / transitive-cold (nonzero only)"
        nonzero_symbols
        echo "# JIT inlinee summaries per method:"
        awk '/^; Assembly listing for method /{m=$0; sub(/^; Assembly listing for method /,"",m); sub(/ .*/,"",m)} /inlinees/{print "jit " m "  " substr($0,3)}' "$JIT" | head -40
    } >> "$LEDGER"

    : > "$VD/verdicts.txt"
    echo "$BENCH_MAP" | while read -r bench snake camel; do
        for path in write read; do
            Cap="$(echo "$path" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
            pat="$(unit_cs_sym example "$Cap" "$camel")" || exit 1
            n="$(count_for "$pat")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n") $n 0" >> "$VD/verdicts.txt"
            else
                echo "note: cs $bench $path: no JitDisasm method matching $pat — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    # families rt and bits: the [MethodImpl(NoInlining)] timed-loop methods
    # (§4.1; bl+blr counted, per the C# rule above; PER OP via perop_for —
    # the JIT does not unroll these loops today, so k stays 1)
    echo "$RT_MAP" | while read -r bench snake lower camel; do
        for path in write read; do
            Cap="$(echo "$path" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"
            n="$(perop_for "^Program:${camel}${Cap}Loop\$")"
            if [ "$n" -ge 0 ]; then
                echo "$bench $path $(verdict_of "$n") $n 0" >> "$VD/verdicts.txt"
            elif [ "$n" -eq -2 ]; then
                echo "note: cs $bench $path: loop body count does not divide by its unroll factor — inline stays unknown" >> "$LEDGER"
            else
                echo "note: cs $bench $path: Program:${camel}${Cap}Loop not in the JitDisasm output — inline stays unknown" >> "$LEDGER"
            fi
        done
    done
    emit_rows
    backfill
    ;;

js)
    # No verdict pass exists for js, DELIBERATELY: §4.1 attributes a verdict
    # by walking a compiled artifact's symbols, and a JIT leg has no AOT
    # artifact to walk — what V8 inlines is decided per tier at runtime and
    # is not a stable property of the build. js rows keep inline=unknown,
    # which is exactly what §4.2 does with them: refuses their ratios.
    echo "inline-verdict: js has no verdict pass (JIT — no AOT artifact to attribute); js rows stay inline=unknown" >&2
    exit 1
    ;;

*)
    echo "unknown language: $LANG_ARG (c|cpp|go|rust|cs)" >&2
    exit 1
    ;;
esac

echo "ledger: $LEDGER" >&2
