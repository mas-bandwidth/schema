#!/bin/bash
# inline-gate.sh — the per-port inline-budget gate (issue #25; BENCH-STANDARD.md §4.3).
#
# Everything this estate measures depends on the serialize chain inlining end
# to end, and until this gate nothing warned when it stopped: Go's hot
# functions sit at EXACTLY the cost-80 cliff, and one added line silently
# halves throughput while the suite stays green. This script is the warning.
#
#   bench/tools/inline-gate.sh selftest
#       The adversarial fixtures, REQUIRED before any leg verdict is trusted
#       (issue #25: every self-reported green stage shipped a defect — four
#       for four — and each was found by an adversary, never by more tests
#       from the same worker). The fixtures source the REAL counting
#       machinery from inline-verdict.sh (INLINE_VERDICT_LIB=1) and attack
#       it: a known out-of-line call MUST produce partial:N, a cold-classified
#       call MUST NOT count hot, a call with no cold signal MUST NOT be
#       guessed cold, a tail-branch b into the runtime or a helper MUST count
#       as a call while local control flow MUST NOT (issue #86), the
#       objdump/JitDisasm translators MUST carry tail transfers through, the
#       helper-namespace join failure (the 4dc9b62 false
#       full) MUST be caught, unroll division MUST divide or refuse, and the
#       budget check itself MUST fail on over-budget, unknown, and absent
#       legs. Exit non-zero if the machinery cannot fail where it must.
#
#   bench/tools/inline-gate.sh leg <c|cpp|go|rust|cs>
#       The CI gate for one language: build and run one untimed round
#       (run.sh --only <lang> --round 0 — timings are discarded, only the
#       artifacts matter), derive the §4.2 verdicts from the CURRENT tree
#       (inline-verdict.sh), and fail unless every budgeted (bench, path)
#       leg's hot count is within bench/inline-budget.txt. For go, the
#       remark scan must be NON-EMPTY: `go build -a` is MANDATORY (§4.1) —
#       without -a the build cache serves cached objects and prints nothing,
#       which reads as a false clean bill of health (it nearly did once).
#
#   bench/tools/inline-gate.sh verdicts <lang> <opt> <verdicts.txt> [budget.txt]
#       The bare assertion over a leg's verdict output (the shape
#       inline-verdict.sh writes: "<bench> <path> <verdict> <hot> <cold>").
#       Fails if any budgeted leg is over budget, unknown, none, or ABSENT —
#       a verdict that vanished must not pass a gate.
#
#   bench/tools/inline-gate.sh check <results.csv> [budget.txt]
#       The same assertion over a verdict-backfilled CSV's rows (v2, 17
#       columns, keyed lang/bench/path/opt). Rows without a budget line pass
#       with a note; budgeted rows absent from the CSV are NOT failed here
#       (a single-language CSV legitimately lacks the other four) — the
#       per-leg form above is the completeness gate.
#
# The budget ledger fails on REGRESSION, not absolute badness (§4.3), and it
# also fires on compiler-version changes — accepted deliberately by issue
# #25; telling those apart is manual. Improving a budget is a deliberate
# ledger edit in the same commit as the improvement.
set -u

cd "$(dirname "$0")/../.."      # repo root

BUDGET_DEFAULT=bench/inline-budget.txt
GATE_DIR=build/bench/inline-gate

usage() {
    echo "usage: bench/tools/inline-gate.sh selftest" >&2
    echo "       bench/tools/inline-gate.sh leg <c|cpp|go|rust|cs>" >&2
    echo "       bench/tools/inline-gate.sh verdicts <lang> <opt> <verdicts.txt> [budget.txt]" >&2
    echo "       bench/tools/inline-gate.sh check <results.csv> [budget.txt]" >&2
    exit 1
}

[ $# -ge 1 ] || usage
CMD="$1"; shift

# ---- the assertion core -----------------------------------------------------
# verdict_hot <verdict>: hot count for a budget comparison, or -1 for a
# verdict a gate can never pass on (unknown = the pass did not attribute;
# none = the chain is entirely out of line — if that is ever the accepted
# state, the budget line says so as a number, not as a shrug).
#
# assert_verdicts <lang> <opt> <verdicts-file> <budget-file>
# Every budget line for (lang, opt) must find its (bench, path) in the
# verdicts file, within budget. Verdict rows without a budget line WARN —
# an unguarded leg is a hole in the fence, §4.3's ledger must know every
# bench — but do not fail (adding a bench and its budget is one commit).
assert_verdicts() {
    awk -v lang="$1" -v opt="$2" -v vf="$3" -v bf="$4" '
        BEGIN {
            fails = 0; oks = 0; warns = 0
            while ((getline line < bf) > 0) {
                if (line ~ /^[ \t]*(#|$)/) continue
                n = split(line, a, " ")
                if (n < 5) { printf "inline-gate: malformed budget line: %s\n", line; fails++; continue }
                if (a[1] != lang || a[4] != opt) continue
                budget[a[2] "/" a[3]] = a[5]
            }
            close(bf)
            if (length(budget) == 0) {
                printf "inline-gate FAIL (%s %s): no budget lines for this lang/opt in %s — an empty gate gates nothing\n", lang, opt, bf
                exit 1
            }
            while ((getline line < vf) > 0) {
                if (line ~ /^[ \t]*(#|$)/) continue
                n = split(line, a, " ")
                if (n < 4) continue
                key = a[1] "/" a[2]; verdict = a[3]; hot = a[4] + 0
                seen[key] = 1
                if (!(key in budget)) {
                    printf "inline-gate WARN (%s %s): %s %s has verdict %s but NO budget line — unguarded leg, add it to the ledger\n", lang, opt, a[1], a[2], verdict
                    warns++
                    continue
                }
                lim = budget[key] + 0
                if (verdict == "unknown") {
                    printf "inline-gate FAIL (%s %s): %s %s is unknown — a gate cannot pass on an unattributed verdict (budget %d)\n", lang, opt, a[1], a[2], lim
                    fails++
                } else if (verdict == "none") {
                    printf "inline-gate FAIL (%s %s): %s %s is none — the chain is entirely out of line (budget %d)\n", lang, opt, a[1], a[2], lim
                    fails++
                } else if (hot > lim) {
                    printf "inline-gate FAIL (%s %s): %s %s hot %d exceeds budget %d (verdict %s) — the serialize chain stopped inlining, or the compiler changed; telling those apart is manual (issue #25)\n", lang, opt, a[1], a[2], hot, lim, verdict
                    fails++
                } else {
                    if (hot < lim)
                        printf "inline-gate note (%s %s): %s %s hot %d under budget %d — tighten the ledger deliberately\n", lang, opt, a[1], a[2], hot, lim
                    oks++
                }
            }
            close(vf)
            for (key in budget) {
                if (!(key in seen)) {
                    split(key, a, "/")
                    printf "inline-gate FAIL (%s %s): budgeted leg %s %s produced NO verdict — a vanished verdict must not pass\n", lang, opt, a[1], a[2]
                    fails++
                }
            }
            if (fails > 0) {
                printf "inline-gate: %s %s: %d FAILURES, %d legs within budget, %d unguarded\n", lang, opt, fails, oks, warns
                exit 1
            }
            printf "inline-gate PASS: %s %s: %d legs within budget, %d unguarded\n", lang, opt, oks, warns
        }' /dev/null
}

# check mode: pull one (lang, opt) slice of a v2 CSV into verdict shape.
# CSV columns: 1 lang, 2 bench, 3 path, 16 opt, 17 inline.
csv_slice() {
    awk -F, -v lang="$1" -v opt="$2" '
        NF == 17 && $1 == lang && $16 == opt && $2 != "bench" {
            v = $17; hot = 0
            if (v ~ /^partial:/) { hot = substr(v, 9) + 0 }
            print $2, $3, v, hot, 0
        }' "$3"
}

case "$CMD" in

# --------------------------------------------------------------------- check
check)
    [ $# -ge 1 ] || usage
    CSV="$1"; BUDGET="${2:-$BUDGET_DEFAULT}"
    [ -f "$CSV" ] || { echo "no such csv: $CSV" >&2; exit 1; }
    [ -f "$BUDGET" ] || { echo "no such budget ledger: $BUDGET" >&2; exit 1; }
    mkdir -p "$GATE_DIR"
    rc=0
    # every (lang, opt) pair present in the CSV is gated against its budget
    # slice; pairs with no budget lines at all are reported and skipped (the
    # per-leg form is the completeness gate)
    awk -F, 'NF == 17 && $1 != "lang" { print $1, $16 }' "$CSV" | sort -u | while read -r lang opt; do
        if ! awk -v l="$lang" -v o="$opt" '!/^[ \t]*(#|$)/ && $1 == l && $4 == o { f = 1 } END { exit !f }' "$BUDGET"; then
            echo "inline-gate note: no budget lines for $lang $opt — CSV slice unguarded"
            continue
        fi
        csv_slice "$lang" "$opt" "$CSV" > "$GATE_DIR/slice-$lang-$opt.txt"
        # absent budgeted legs are tolerated in check mode: gate only rows
        # the CSV actually carries, by keeping just the budget lines seen
        awk -v sf="$GATE_DIR/slice-$lang-$opt.txt" -v l="$lang" -v o="$opt" '
            BEGIN { while ((getline line < sf) > 0) { split(line, a, " "); seen[a[1] "/" a[2]] = 1 } }
            /^[ \t]*(#|$)/ { print; next }
            $1 == l && $4 == o { if (($2 "/" $3) in seen) print; next }
            { print }' "$BUDGET" > "$GATE_DIR/budget-$lang-$opt.txt"
        assert_verdicts "$lang" "$opt" "$GATE_DIR/slice-$lang-$opt.txt" "$GATE_DIR/budget-$lang-$opt.txt" || echo "FAILED $lang $opt" >> "$GATE_DIR/check.fails"
    done
    if [ -f "$GATE_DIR/check.fails" ]; then rc=1; rm -f "$GATE_DIR/check.fails"; fi
    exit $rc
    ;;

# ------------------------------------------------------------------ verdicts
verdicts)
    [ $# -ge 3 ] || usage
    assert_verdicts "$1" "$2" "$3" "${4:-$BUDGET_DEFAULT}"
    ;;

# ----------------------------------------------------------------------- leg
leg)
    [ $# -eq 1 ] || usage
    LANG_GATE="$1"
    case "$LANG_GATE" in c|cpp|go|rust|cs) ;; *) usage ;; esac
    mkdir -p "$GATE_DIR"
    CSV="$GATE_DIR/gate-$LANG_GATE.csv"
    rm -f "$CSV"

    # one untimed round: builds the leg's artifacts and writes a CSV with the
    # real preamble (the verdict pass reads its flags lines); the timings in
    # it are advisory and never leave build/
    OPT_LEVEL="${BENCH_OPT_LEVEL:-O3}"
    echo "== inline-gate: $LANG_GATE leg — one verdict round (untimed use, $OPT_LEVEL) ==" >&2
    BENCH_NOISE="inline-gate verdict round — timings advisory, never published" \
        bench/run.sh --only "$LANG_GATE" --round 0 --out "$CSV" >&2 || {
        echo "inline-gate FAIL ($LANG_GATE): the bench round did not run — no artifacts to verdict" >&2
        exit 1
    }

    bench/tools/inline-verdict.sh "$LANG_GATE" "$CSV" >&2 || {
        echo "inline-gate FAIL ($LANG_GATE): the verdict pass refused — rows stay unknown, and a gate cannot pass on unknown" >&2
        exit 1
    }
    VD="build/bench/inline-verdict-$LANG_GATE"

    # the issue #25 rider, enforced live: the Go remark scan runs under
    # `go build -a` (inline-verdict.sh) — an EMPTY scan is the false-clean
    # signature build caching produces, so emptiness refuses rather than
    # reading as a clean bill of health.
    if [ "$LANG_GATE" = go ]; then
        if [ ! -s "$VD/serialize-remarks.txt" ]; then
            echo "inline-gate FAIL (go): the -gcflags=-m=2 remark scan produced NOTHING for serialize.go — the false-clean signature of a cached build (go build -a is mandatory, §4.1) or a broken module graph; refusing to gate on silence" >&2
            exit 1
        fi
    fi

    case "$LANG_GATE" in
        c|cpp)   OPT="$OPT_LEVEL" ;;
        rust)    OPT=O3 ;;          # cargo release is opt-level 3, always
        go|cs)   OPT=default ;;     # §3.3: the language has no level
    esac
    assert_verdicts "$LANG_GATE" "$OPT" "$VD/verdicts.txt" "$BUDGET_DEFAULT"
    ;;

# ------------------------------------------------------------------ selftest
selftest)
    mkdir -p "$GATE_DIR/selftest"
    ST="$GATE_DIR/selftest"
    FAILS=0
    fail() { echo "SELFTEST FAIL: $*" >&2; FAILS=$((FAILS + 1)); }
    ok()   { echo "selftest ok: $*" >&2; }

    # source the REAL machinery — the fixtures attack inline-verdict.sh's own
    # count_calls / perop_for / selftest_join / rust_cold_regex, not a copy
    INLINE_VERDICT_LIB=1
    LANG_ARG=fixture
    VD="$ST"
    . bench/tools/inline-verdict.sh

    RT='^_serialize_'
    HELPER='^_rt_fix'

    # ---- 1. a known out-of-line call MUST produce partial, never full ----
    # (the machinery must be able to say something is wrong: a broken counter
    # that reports full here is the 4dc9b62 failure class shipping again)
    cat > "$ST/outofline.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_write_bits
0000000100000104	bl	_serialize_write_bits
0000000100000108	bl	_serialize_flush
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/outofline.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    v="$(verdict_of "${n:-0}")"
    if [ "$v" = "partial:3" ]; then
        ok "out-of-line fixture verdicts partial:3"
    else
        fail "out-of-line fixture: 3 runtime calls in the loop body must verdict partial:3, got '$v' — the counter cannot see an uninlined chain"
    fi

    # ---- 2. a clean loop MUST verdict full (the negative control) ----
    cat > "$ST/clean.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	add	x0, x0, x1
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/clean.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    v="$(verdict_of "${n:-1}")"
    if [ "$v" = "full" ]; then
        ok "clean fixture verdicts full"
    else
        fail "clean fixture: a loop with zero runtime calls must verdict full, got '$v'"
    fi

    # ---- 3. a cold-classified call MUST NOT count hot (§4.2, d2a50ff-era
    # hot/cold split): the split .cold target counts cold beside the verdict,
    # and the verdict stays full ----
    cat > "$ST/cold.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_check_overflow.cold
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/cold.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    cold="$(perop_cold_for '^_rt_fix_write_loop$')"
    if [ "${n:-1}" = 0 ] && [ "${cold:-0}" = 1 ]; then
        ok "cold fixture: .cold target counts cold (hot 0, cold 1) — verdict full with debt beside it"
    else
        fail "cold fixture: a .cold-split target must count cold, never hot (got hot=$n cold=$cold) — a cold call counted hot is the 2e26eaa defect again"
    fi

    # ---- 4. a call with NO cold signal MUST stay hot: never guess cold —
    # hot-counted-cold is the direction that publishes a false full ----
    cat > "$ST/nosignal.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_slow_path
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/nosignal.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    if [ "${n:-0}" = 1 ]; then
        ok "no-signal fixture: an unmarked target stays hot (partial:1)"
    else
        fail "no-signal fixture: a target with no cold signal must count HOT (got hot=$n) — guessing cold publishes a false full"
    fi

    # ---- 5. helper-edge propagation: runtime calls flowing through an
    # out-of-line generated helper MUST reach the loop's transitive count ----
    cat > "$ST/helper.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_rt_fix_helper
_rt_fix_helper:
0000000100000200	bl	_serialize_write_bits
0000000100000204	bl	_serialize_write_bits
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/helper.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    if [ "${n:-0}" = 2 ]; then
        ok "helper fixture: transitive count propagates through the out-of-line helper (partial:2)"
    else
        fail "helper fixture: 2 runtime calls behind a helper edge must verdict partial:2 at the loop (got hot=$n)"
    fi
    if selftest_join "$ST/helper.disasm" "$VD/counts.txt" "$HELPER" ; then
        ok "join self-test accepts the well-formed helper join"
    else
        fail "join self-test rejected a well-formed helper join"
    fi

    # ---- 6. tail-branch calls MUST count (issue #86): a b whose target is
    # another function's symbol is a call the verdict must count — the callee
    # was NOT inlined, the transfer just reused the caller's frame. And
    # intra-function control flow must NOT count: a b to a raw address (otool
    # prints local targets as hex), a b.cond, a b back to the function's own
    # entry (its loop backedge), a b between a function and its own split
    # .cold chunk (one logical function), and a b to a symbol outside the
    # runtime/helper sets (the bl policy, mirrored). ----
    cat > "$ST/tail.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_write_bits
0000000100000104	b	_rt_fix_tailee
_rt_fix_tailee:
0000000100000200	bl	_serialize_write_bits
0000000100000204	b	_serialize_flush
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/tail.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    m="$(count_for '^_rt_fix_tailee$')"
    if [ "${m:-0}" = 2 ] && [ "${n:-0}" = 3 ]; then
        ok "tail-branch fixture: b into the runtime counts direct, b into a helper is an edge (tailee 2, loop 3)"
    else
        fail "tail-branch fixture: the tailee must count 2 (bl + tail b into the runtime) and the loop 3 (bl + tail edge through the tailee), got tailee=$m loop=$n — a helper reached by tail call is invisible and its green is a partial wearing a full's face (issue #86)"
    fi
    if selftest_join "$ST/tail.disasm" "$VD/counts.txt" "$HELPER"; then
        ok "join self-test accepts the tail-branch helper join"
    else
        fail "join self-test rejected a well-formed tail-branch helper join"
    fi
    cat > "$ST/tail-local.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_write_bits
0000000100000104	b.ne	0x100000100
0000000100000108	b	0x100000100
000000010000010c	b	_rt_fix_write_loop
0000000100000110	b	_memcpy
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/tail-local.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    if [ "${n:-0}" = 1 ]; then
        ok "tail-branch fixture: local/conditional/self/foreign b forms stay control flow (partial:1 from the bl alone)"
    else
        fail "tail-branch fixture: only the bl may count — a hex-target b, a b.cond, a self-branch, and a b outside the runtime/helper sets are not calls (got hot=$n) — counting control flow as calls is the false-partial direction"
    fi
    cat > "$ST/tail-cold.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_write_bits
0000000100000104	b	_rt_fix_write_loop.cold
_rt_fix_write_loop.cold:
0000000100000200	bl	_serialize_write_bits
_rt_fix_read_loop:
0000000100000300	b	_serialize_check_overflow.cold
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/tail-cold.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    h="$(perop_for '^_rt_fix_read_loop$')"
    c="$(perop_cold_for '^_rt_fix_read_loop$')"
    if [ "${n:-0}" = 1 ] && [ "${h:-1}" = 0 ] && [ "${c:-0}" = 1 ]; then
        ok "tail-branch fixture: own split chunk is control flow (hot 1); a tail b into another function's cold chunk counts cold (hot 0, cold 1)"
    else
        fail "tail-branch fixture: a b into the function's own .cold chunk must not count (want hot 1, got hot=$n) and a tail b into another function's cold chunk must count cold, never hot (want hot 0 cold 1, got hot=$h cold=$c)"
    fi

    # ---- 7. the translated surfaces carry tail transfers too (issue #86):
    # go tool objdump spells a tail call JMP name(SB) — the n(PC) and (Rn)
    # forms are intra-function control flow and trampolines, never calls;
    # JitDisasm spells one b to a method or a raw address — G_M insGroup
    # labels are intra-method control flow, and an address target is as
    # opaque as blr, so it counts the same way blr does (§4.1's C# rule:
    # silently dropping an opaque target would fake a full). ----
    cat > "$ST/go-tail.objdump" <<'EOF'
TEXT main.rtFixWriteLoop(SB) /x/main.go
  main.go:5	0x100	97ffffe0	CALL main.fixHelper(SB)
  main.go:6	0x104	14000000	JMP main.fixTail(SB)
  main.go:7	0x108	17fffffe	JMP 2(PC)
  main.go:8	0x10c	d61f0360	JMP (R27)
TEXT main.fixTail(SB) /x/main.go
  main.go:9	0x200	97ffffe0	CALL serialize%2ego.WriteBits(SB)
EOF
    go_translate < "$ST/go-tail.objdump" > "$ST/go-tail.calls"
    count_calls 'serialize%2ego[.]' '^example[.]|^main[.]' '' < "$ST/go-tail.calls" > "$VD/counts.txt"
    n="$(count_for '^main[.]rtFixWriteLoop$')"
    if [ "${n:-0}" = 1 ]; then
        ok "go translation: JMP name(SB) becomes a tail edge carrying the callee's runtime call; n(PC)/(Rn) forms do not"
    else
        fail "go translation: a JMP into a generated helper must carry that helper's 1 runtime call to the caller, and n(PC)/(Rn) jumps must not count (got transitive=$n) — the objdump translator drops tail calls (issue #86)"
    fi
    cat > "$ST/cs-tail.jit" <<'EOF'
; Assembly listing for method Program:RtFixWriteLoop() (FullOpts)
        bl      CORINFO_HELP_ASSIGN_REF
        b       G_M12345_IG04
        b       0x16E5B7A0
EOF
    cs_translate < "$ST/cs-tail.jit" > "$ST/cs-tail.calls"
    count_calls '.' '(Example[.]Schema|Program):' '' < "$ST/cs-tail.calls" > "$VD/counts.txt"
    awk '$1 == "SYM" { $4 += $5 } { print }' "$VD/counts.txt" > "$VD/counts.folded" \
        && mv "$VD/counts.folded" "$VD/counts.txt"
    n="$(count_for '^Program:RtFixWriteLoop$')"
    if [ "${n:-0}" = 2 ]; then
        ok "cs translation: a tail b past the insGroup labels counts like blr (bl + opaque tail = 2); G_M labels do not"
    else
        fail "cs translation: one bl plus one opaque tail b must count 2 with indirect folded, and the G_M label must not count (got $n) — JitDisasm tail transfers are invisible (issue #86)"
    fi

    # ---- 8. the namespace-join failure MUST be caught (the 4dc9b62 false
    # full: call targets in a namespace the symbol headers never joined) ----
    cat > "$ST/badjoin.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	CRATE_x__rt_fix_helper
_rt_fix_helper:
0000000100000200	bl	_serialize_write_bits
EOF
    count_calls "$RT" '^CRATE_x_' "$COLD_SPLIT" < "$ST/badjoin.disasm" > "$VD/counts.txt"
    if selftest_join "$ST/badjoin.disasm" "$VD/counts.txt" '^CRATE_x_' 2> "$ST/badjoin.err"; then
        fail "namespace-join fixture: targets renamed out of the header namespace joined to nothing and the self-test did NOT fire — the 4dc9b62 false-full detector is dead"
    else
        ok "namespace-join fixture: selftest_join fires on the 4dc9b62 failure class"
    fi

    # ---- 9. unroll division: an unrolled loop divides back to per-op, and a
    # count that does not divide REFUSES (unknown) rather than guessing ----
    cat > "$ST/unroll.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_write_bits
0000000100000104	bl	_serialize_write_bits
0000000100000108	bl	_serialize_write_bits
000000010000010c	bl	_serialize_write_bits
0000000100000110	bl	_serialize_write_bits
0000000100000114	bl	_serialize_write_bits
0000000100000118	bl	_serialize_write_bits
000000010000011c	bl	_serialize_write_bits
0000000100000120	bl	_rt_fix_helper
0000000100000124	bl	_rt_fix_helper
0000000100000128	bl	_rt_fix_helper
000000010000012c	bl	_rt_fix_helper
_rt_fix_helper:
0000000100000200	bl	_serialize_write_bits
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/unroll.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    if [ "${n:-0}" = 3 ]; then
        ok "unroll fixture: 4x-unrolled loop divides back to 3 per op"
    else
        fail "unroll fixture: (8 direct + 4x helper(1)) / 4 must be 3 per op, got '$n'"
    fi
    cat > "$ST/unroll-bad.disasm" <<'EOF'
_rt_fix_write_loop:
0000000100000100	bl	_serialize_write_bits
0000000100000104	bl	_serialize_write_bits
0000000100000108	bl	_serialize_write_bits
000000010000010c	bl	_rt_fix_helper
0000000100000110	bl	_rt_fix_helper
0000000100000114	bl	_rt_fix_helper
0000000100000118	bl	_rt_fix_helper
_rt_fix_helper:
0000000100000200	bl	_serialize_write_bits
EOF
    count_calls "$RT" "$HELPER" "$COLD_SPLIT" < "$ST/unroll-bad.disasm" > "$VD/counts.txt"
    n="$(perop_for '^_rt_fix_write_loop$')"
    if [ "${n:-0}" = -2 ]; then
        ok "unroll fixture: a non-dividing count refuses (-2 -> unknown) instead of guessing"
    else
        fail "unroll fixture: 7 calls against unroll witness 4 must refuse (-2), got '$n'"
    fi

    # ---- 10. the rust #[cold] source scan: a #[cold] fn is matched by its
    # mangled token, a hot fn is not ----
    mkdir -p "$ST/crate/src"
    cat > "$ST/crate/src/lib.rs" <<'EOF'
#[cold]
fn slow_path() {}
pub fn hot_path() {}
EOF
    RE="$(rust_cold_regex "$ST/crate")"
    if echo "_ZN9serialize9slow_path17h0123456789abcdefE" | grep -Eq "$RE" \
       && ! echo "_ZN9serialize8hot_path17h0123456789abcdefE" | grep -Eq "$RE"; then
        ok "rust cold scan: #[cold] fn matches by mangled token, hot fn does not"
    else
        fail "rust cold scan: regex '$RE' must match the #[cold] fn's mangled token and not the hot fn"
    fi

    # ---- 11. the budget assertion itself must be able to fail ----
    cat > "$ST/budget.txt" <<'EOF'
# lang bench path opt max-hot
fixture chat write O3 1
fixture chat read O3 0
EOF
    printf 'chat write partial:2 2 0\nchat read full 0 0\n' > "$ST/over.txt"
    if assert_verdicts fixture O3 "$ST/over.txt" "$ST/budget.txt" > /dev/null 2>&1; then
        fail "budget check: hot 2 against budget 1 PASSED — the gate cannot fail"
    else
        ok "budget check fails on over-budget"
    fi
    printf 'chat write unknown 0 0\nchat read full 0 0\n' > "$ST/unk.txt"
    if assert_verdicts fixture O3 "$ST/unk.txt" "$ST/budget.txt" > /dev/null 2>&1; then
        fail "budget check: an unknown verdict PASSED — unknown must refuse"
    else
        ok "budget check fails on unknown"
    fi
    printf 'chat write partial:1 1 0\n' > "$ST/missing.txt"
    if assert_verdicts fixture O3 "$ST/missing.txt" "$ST/budget.txt" > /dev/null 2>&1; then
        fail "budget check: a budgeted leg with NO verdict PASSED — absence must refuse"
    else
        ok "budget check fails on a vanished verdict"
    fi
    printf 'chat write partial:1 1 0\nchat read full 0 0\n' > "$ST/within.txt"
    if assert_verdicts fixture O3 "$ST/within.txt" "$ST/budget.txt" > /dev/null 2>&1; then
        ok "budget check passes within budget"
    else
        fail "budget check: within-budget verdicts must pass"
    fi

    # ---- 12. the GENERATED UNIT the map resolves against (issue #104) ----
    # UNIT_MAP carries two generated units: `example` (examples/*.schema ->
    # generated/<lang>) and `realworld` (bench/corpus/RealWorld.schema ->
    # generated/bench/<lang>/...). No bench row names either one today —
    # BENCH_MAP is empty since the §1.2 rows retired — so these fixtures are
    # the only thing keeping the unit-disambiguation honest until the #177
    # follow-on repopulates it. The two units' entry points are spelled
    # IDENTICALLY apart from the package /
    # namespace / crate prefix each port's layout puts in front — so a map that
    # cannot name a row's unit has two ways to be wrong, and each fixture below
    # attacks one of them:
    #
    #   RESOLUTION — the lookup lands on the OTHER unit's same-named entry
    #   point and publishes its count as this row's verdict. Every fixture
    #   plants a decoy of the same name in the other unit, with a different
    #   count, so a collision cannot pass by accident.
    #
    #   COMPLETENESS — the unit is missing from the language's HELPER regex, so
    #   a call from the row's entry point into that unit's own out-of-line
    #   helper matches neither the runtime nor the helper set and counts as
    #   NOTHING. That is not a wrong number, it is a false `full` — the
    #   direction this machinery exists to refuse. Each fixture routes the
    #   realworld unit's runtime calls through such a helper.
    cat > "$ST/unit-go.calls" <<'EOF'
example.WriteRealPacket:
0 bl github.com/mas-bandwidth/serialize%2ego.(*WriteStream).writeBits
0 bl github.com/mas-bandwidth/serialize%2ego.(*WriteStream).writeBits
0 bl github.com/mas-bandwidth/serialize%2ego.(*WriteStream).writeBits
0 bl github.com/mas-bandwidth/serialize%2ego.(*WriteStream).writeBits
0 bl github.com/mas-bandwidth/serialize%2ego.(*WriteStream).writeBits
bench/realworld.WriteRealPacket:
0 bl bench/realworld.writeVec3
bench/realworld.writeVec3:
0 bl github.com/mas-bandwidth/serialize%2ego.(*WriteStream).writeBits
0 bl github.com/mas-bandwidth/serialize%2ego.(*WriteStream).writeBits
EOF
    GOH="$(unit_helper_re go)"
    count_calls 'serialize%2ego[.]' "$GOH" '' < "$ST/unit-go.calls" > "$VD/counts.txt"
    rw="$(count_for "$(unit_go_sym realworld Write RealPacket)")"
    ex="$(count_for "$(unit_go_sym example Write RealPacket)")"
    if [ "${rw:-0}" = 2 ] && [ "${ex:-0}" = 5 ]; then
        ok "go unit fixture: the realworld row resolves to bench/realworld (2, through its own helper) and the example decoy stays 5"
    else
        fail "go unit fixture: the realworld row must resolve to bench/realworld.WriteRealPacket and carry its helper's 2 runtime calls, and must NOT pick up the example unit's same-named 5 (got realworld=$rw example=$ex) — a row resolved against the wrong unit publishes another workload's verdict, and a unit missing from the HELPER set publishes a false full"
    fi

    cat > "$ST/unit-cs.jit" <<'EOF'
; Assembly listing for method Example.Schema:WriteRealPacket(WriteStream,RealPacket) (FullOpts)
        bl      CORINFO_HELP_ASSIGN_REF
        bl      CORINFO_HELP_ASSIGN_REF
        bl      CORINFO_HELP_ASSIGN_REF
        bl      CORINFO_HELP_ASSIGN_REF
        bl      CORINFO_HELP_ASSIGN_REF
; Assembly listing for method Realworld.Schema:WriteRealPacket(WriteStream,RealPacket) (FullOpts)
        bl      Realworld.Schema:WriteRealPacketBatch
; Assembly listing for method Realworld.Schema:WriteRealPacketBatch(WriteBatch,RealPacket) (FullOpts)
        bl      CORINFO_HELP_ASSIGN_REF
        bl      CORINFO_HELP_ASSIGN_REF
EOF
    cs_translate < "$ST/unit-cs.jit" > "$ST/unit-cs.calls"
    count_calls '.' "$(unit_helper_re cs)" '' < "$ST/unit-cs.calls" > "$VD/counts.txt"
    rw="$(count_for "$(unit_cs_sym realworld Write RealPacket)")"
    ex="$(count_for "$(unit_cs_sym example Write RealPacket)")"
    if [ "${rw:-0}" = 2 ] && [ "${ex:-0}" = 5 ]; then
        ok "cs unit fixture: the realworld row resolves to Realworld.Schema (2, through its own Batch helper) and the example decoy stays 5"
    else
        fail "cs unit fixture: the realworld row must resolve to Realworld.Schema:WriteRealPacket and count its Batch helper's 2, not the example unit's same-named 5 and not the helper call itself as 1 (got realworld=$rw example=$ex) — with the catch-all C# RTPAT a unit missing from the HELPER set turns a whole helper body into a single counted call"
    fi

    cat > "$ST/unit-rust.disasm" <<'EOF'
__ZN7example17write_real_packet17h1111111111111111E:
0000000100000100	bl	__ZN9serialize11WriteStream10write_bits17h2222222222222222E
0000000100000104	bl	__ZN9serialize11WriteStream10write_bits17h2222222222222222E
0000000100000108	bl	__ZN9serialize11WriteStream10write_bits17h2222222222222222E
000000010000010c	bl	__ZN9serialize11WriteStream10write_bits17h2222222222222222E
0000000100000110	bl	__ZN9serialize11WriteStream10write_bits17h2222222222222222E
__ZN15realworldcorpus9realworld17write_real_packet17h3333333333333333E:
0000000100000200	bl	__ZN15realworldcorpus9realworld10write_vec317h4444444444444444E
__ZN15realworldcorpus9realworld10write_vec317h4444444444444444E:
0000000100000300	bl	__ZN9serialize11WriteStream10write_bits17h2222222222222222E
0000000100000304	bl	__ZN9serialize11WriteStream10write_bits17h2222222222222222E
EOF
    rust_translate < "$ST/unit-rust.disasm" > "$ST/unit-rust.calls"
    RSH="$(unit_helper_re rust)"
    count_calls '^CRATE_serialize_' "$RSH" '' < "$ST/unit-rust.calls" > "$VD/counts.txt"
    rw="$(count_for "$(unit_rust_sym realworld write_real_packet)")"
    ex="$(count_for "$(unit_rust_sym example write_real_packet)")"
    if [ "${rw:-0}" = 2 ] && [ "${ex:-0}" = 5 ]; then
        ok "rust unit fixture: the realworldcorpus crate classifies as its own CRATE_ class, resolves to 2 through its helper, and does not alias the example crate's same-named fn (5)"
    else
        fail "rust unit fixture: write_real_packet exists in BOTH crates; the realworld row must resolve to the realworldcorpus one and carry its helper's 2, the example row to the example one at 5 (got realworld=$rw example=$ex) — an unclassified crate is 'other', which is neither runtime nor helper, so its calls are dropped and the row reads full"
    fi
    if selftest_join "$ST/unit-rust.calls" "$VD/counts.txt" "$RSH"; then
        ok "join self-test accepts the two-unit rust join"
    else
        fail "join self-test rejected the two-unit rust join"
    fi

    cat > "$ST/unit-cpp.disasm" <<'EOF'
__ZN9realworld15WriteRealPacketERN9serialize11WriteStreamERKNS_10RealPacketE:
0000000100000100	bl	__ZN9realworld10write_vec3ERN9serialize11WriteStreamE
__ZN9realworld10write_vec3ERN9serialize11WriteStreamE:
0000000100000200	bl	__ZN9serialize11WriteStream10write_bitsEjj
0000000100000204	bl	__ZN9serialize11WriteStream10write_bitsEjj
EOF
    count_calls '^__?ZNK?9serialize' "$(unit_helper_re cpp)" "$COLD_SPLIT" < "$ST/unit-cpp.disasm" > "$VD/counts.txt"
    n="$(count_for '^__ZN9realworld15WriteRealPacket')"
    if [ "${n:-0}" = 2 ]; then
        ok "cpp unit fixture: namespace realworld is generated code, so its helper's 2 runtime calls reach the entry point"
    else
        fail "cpp unit fixture: the realworld entry point's 2 runtime calls flow through a realworld helper and must reach it (got $n) — a generated namespace missing from the cpp HELPER regex makes those call sites invisible to the atos attribution and the row publishes a false full"
    fi

    # a unit the map does not know must REFUSE, not silently answer with some
    # other unit's spelling: a typo'd unit column is exactly how a row starts
    # measuring the wrong code, and it must be loud at the first call.
    bogus=0
    unit_go_sym   bogusunit Write RealPacket > /dev/null 2>&1 && bogus=$((bogus + 1))
    unit_cs_sym   bogusunit Write RealPacket > /dev/null 2>&1 && bogus=$((bogus + 1))
    unit_rust_sym bogusunit write_real_packet > /dev/null 2>&1 && bogus=$((bogus + 1))
    unit_helper_re bogusfortran > /dev/null 2>&1 && bogus=$((bogus + 1))
    if [ "$bogus" = 0 ]; then
        ok "unknown unit/lang fixture: every spelling function refuses rather than answering with another unit's prefix"
    else
        fail "unknown unit/lang fixture: $bogus of 4 spelling functions ANSWERED for a unit/language they do not know — a row pointed at an unknown unit would silently be measured against whatever prefix was hardcoded"
    fi

    # ---- 13. end-to-end on this host where the toolchain allows: a compiled
    # binary with a known noinline callee must count as out of line ----
    if command -v otool > /dev/null 2>&1 && command -v cc > /dev/null 2>&1; then
        cat > "$ST/e2e.c" <<'EOF'
#include <stdint.h>
__attribute__((noinline)) uint64_t serialize_fix_bits(uint64_t v, int b) {
    return (v << b) | (v >> (64 - b));
}
__attribute__((noinline)) uint64_t rt_fix_write_loop(uint64_t v) {
    uint64_t acc = 0;
    for (int i = 1; i < 64; i++) acc += serialize_fix_bits(v + i, i);
    return acc;
}
int main(void) { return (int)(rt_fix_write_loop(41) & 1); }
EOF
        if cc -O2 -o "$ST/e2e" "$ST/e2e.c" 2> /dev/null; then
            otool -tv "$ST/e2e" > "$ST/e2e.disasm"
            count_calls '^_serialize_fix' '^_rt_fix' "$COLD_SPLIT" < "$ST/e2e.disasm" > "$VD/counts.txt"
            n="$(perop_for '^_rt_fix_write_loop$')"
            if [ "${n:-0}" -ge 1 ]; then
                ok "end-to-end fixture: compiled noinline callee counted out of line (hot=$n)"
            else
                fail "end-to-end fixture: a compiled __attribute__((noinline)) callee must count >= 1 hot call, got '$n' — the otool pipeline is blind"
            fi
        else
            echo "selftest note: cc failed on the e2e fixture — skipped (canned fixtures above still gate)" >&2
        fi
    else
        echo "selftest note: no otool/cc on this host — e2e fixture skipped (canned fixtures above still gate)" >&2
    fi

    if [ "$FAILS" -gt 0 ]; then
        echo "inline-gate selftest: $FAILS FAILURES — the verdict machinery cannot be trusted to fail; fix it before trusting any green" >&2
        exit 1
    fi
    echo "inline-gate selftest: all fixtures behave — the machinery can fail where it must" >&2
    ;;

*)
    usage
    ;;
esac
