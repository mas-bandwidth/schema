// rust/R28 — the guard chain: an arm inside an arm answers to the OUTER tag
// (docs/FIXED-FORM-ALGORITHM.md:238, roadmap.sexp:3624).
//
// THE LAW. The compiled plan entry carries offset+count into a pool of
// (guard, arg, argw) links, outermost first; the only bound the layout's 64.
// An arm inside an arm answers to the OUTER tag: the entry runs when BOTH tags
// hold their ordinal. The outer tag stamped OVER the inner one fired every
// inner arm the moment the outer one rode, the last arm winning; the inner
// tag alone landed an inner arm's bytes beneath an outer arm that never rode.
//
// This test builds the plan by hand — no schema generation needed — and runs
// it through table_fixed_run, the same loop the compiled-plan path uses.
// The runtime is the shared fixed-form runtime under build/tables-generated-rust;
// it is self-contained (no serialize dependency).
//
// rustc --edition 2021 --test test/conformance/rust/rows/R28.rs \
//     -o build/rows-rust-R28 && ./build/rows-rust-R28

#[path = "../../../../build/tables-generated-rust/tblfu1/src/fixed_runtime.rs"]
mod tblfu1;

use tblfu1::{
    TableFixedEntry, TableFixedGuard, TableFixedOp, TableFixedReport, table_fixed_run,
};

use std::sync::atomic::{AtomicUsize, Ordering};

static FAILURES: AtomicUsize = AtomicUsize::new(0);

fn check(ok: bool, msg: &str) {
    if ok {
        println!("ok {msg}");
    } else {
        println!("FAIL {msg}");
        FAILURES.fetch_add(1, Ordering::Relaxed);
    }
}

// ---- The record body for a nested-union scenario ----
//
// FH1/FH2's NestRoot has: pick Outer, tail int32.
// Outer has arms: wrap Wrap (which carries inner Inner), other Other.
// Inner has arms: leaf Leaf (int32 n), alt Alt (int32 k).
//
// For this test we simulate the COMPILED-PLAN walk over a record body whose
// layout is:
//   [0]  outer tag (1 byte)   — which arm of Outer
//   [1]  inner tag (1 byte)   — which arm of Inner (inside the 'wrap' arm)
//   [2..6]  inner payload (4 bytes, LE int32) — the arm's data
//   [6..10] tail int32 (4 bytes) — a field AFTER the nested union
//
// Total body: 10 bytes.
//
// The COMPILED PLAN the generator builds for this layout (FH2 reading FH1's
// bytes) has entries guarded by the OUTER tag. The inner union's own entries
// are ALSO guarded by the outer tag — the guard chain. An arm inside an arm
// answers to the OUTER tag.

const BODY_LEN: usize = 10;

fn record(outer_tag: u8, inner_tag: u8, payload: i32, tail: i32) -> [u8; BODY_LEN] {
    let mut r = [0u8; BODY_LEN];
    r[0] = outer_tag;
    r[1] = inner_tag;
    r[2..6].copy_from_slice(&payload.to_le_bytes());
    r[6..10].copy_from_slice(&tail.to_le_bytes());
    r
}

// ---- The plan — entries inside a nested union arm, guarded by a CHAIN ----
//
// The compiled plan for NestRoot, reading FH1's bytes, builds entries where
// the inner union's arm entries are guarded by a TWO-LINK chain: OUTER tag
// first (link 0), inner tag second (link 1). The entry carries offset+count
// into the pool, and runs only when EVERY link holds its ordinal. We construct
// that plan by hand:
//
//   guards[0] = outer tag at offset 0, ordinal 1 (arm 'wrap')
//   guards[1] = inner tag at offset 1, ordinal 1 (arm 'leaf')
//
// Entry 0: Const — write the inner tag into the image, guarded by the chain.
//   guard_off = 0, guard_count = 2, size = 1, aux = 1 (inner arm ordinal
//   'leaf'), dst = 0
//
// Entry 1: Copy — copy the inner payload, guarded by the SAME chain.
//   guard_off = 0, guard_count = 2, size = 4, src = 2, dst = 1
//
// Entry 2: Copy — copy the tail, unguarded.
//   guard_off = 0, guard_count = 0, size = 4, src = 6, dst = 5

fn guard_chain_plan() -> (Vec<TableFixedEntry>, Vec<TableFixedGuard>) {
    let guards = vec![
        // link 0: the OUTER tag at offset 0, arm 'wrap'
        TableFixedGuard { guard: 0, arg: 1, argw: 1 },
        // link 1: the INNER tag at offset 1, arm 'leaf'
        TableFixedGuard { guard: 1, arg: 1, argw: 1 },
    ];
    let plan = vec![
        // Const: write inner tag = 1 (arm 'leaf') under the whole chain
        TableFixedEntry {
            src: 0,
            dst: 0,
            size: 1,
            aux: 1, // the inner arm ordinal we write
            guard_off: 0,
            guard_count: 2, // OUTER then INNER, both must hold
            op: TableFixedOp::Const,
            ..TableFixedEntry::default()
        },
        // Copy: inner payload under the SAME chain
        TableFixedEntry {
            src: 2,
            dst: 1,
            size: 4,
            guard_off: 0,
            guard_count: 2,
            op: TableFixedOp::Copy,
            ..TableFixedEntry::default()
        },
        // Copy: tail, unguarded
        TableFixedEntry {
            src: 6,
            dst: 5,
            size: 4,
            guard_off: 0,
            guard_count: 0,
            op: TableFixedOp::Copy,
            ..TableFixedEntry::default()
        },
    ];
    (plan, guards)
}

#[test]
fn r28_guard_chain_outer_tag_guards_inner_entries() {
    FAILURES.store(0, Ordering::Relaxed);
    let (plan, guards) = guard_chain_plan();
    let remap: &[u16] = &[];

    // GREEN: outer tag is 1 (arm 'wrap'), inner tag is 1 (arm 'leaf'). Every
    // link of the chain holds, so entries 0 and 1 run: the inner tag and
    // payload land. Entry 2 (tail) is unguarded and always lands.
    let rec = record(1, 1, 42, 7);
    let mut image = [0u8; 10];
    let mut report = TableFixedReport::default();
    table_fixed_run(&plan, &guards, remap, &rec, &mut image, &mut report);

    check(image[0] == 1, "guard chain GREEN: inner tag 1 written under the chain");
    check(
        i32::from_le_bytes(image[1..5].try_into().unwrap()) == 42,
        "guard chain GREEN: payload 42 lands under the chain",
    );
    check(
        i32::from_le_bytes(image[5..9].try_into().unwrap()) == 7,
        "guard chain GREEN: tail 7 lands unguarded",
    );
    check(
        !report.malformed && !report.refused,
        "guard chain GREEN: no damage or refusal",
    );

    // NEGATIVE CONTROL: outer tag is 2 (arm 'other'). The OUTER link does
    // not hold, so entries 0 and 1 are SKIPPED — the inner tag stays 0 (the
    // prefill default) and the payload stays 0. Entry 2 (tail) still lands
    // because it is unguarded. If the inner entries were guarded by the INNER
    // tag only, this would still land the inner payload — that is the bug the
    // chain fixes: an arm inside an arm answers to the OUTER tag too.
    let rec_other = record(2, 1, 42, 7);
    let mut image_other = [0u8; 10];
    let mut report_other = TableFixedReport::default();
    table_fixed_run(&plan, &guards, remap, &rec_other, &mut image_other, &mut report_other);

    check(
        image_other[0] == 0,
        "guard chain NEGATIVE: inner tag stays 0 — outer link skipped the const entry",
    );
    check(
        i32::from_le_bytes(image_other[1..5].try_into().unwrap()) == 0,
        "guard chain NEGATIVE: payload stays 0 — outer link skipped the copy entry",
    );
    check(
        i32::from_le_bytes(image_other[5..9].try_into().unwrap()) == 7,
        "guard chain NEGATIVE: tail still lands (unguarded)",
    );

    // NEGATIVE CONTROL 2: outer tag matches but inner tag differs. The OUTER
    // link holds and the INNER link does not, so entries 0 and 1 are SKIPPED —
    // every link must hold, and the inner tag is its own condition. If the
    // guard chain checked only the OUTER tag, this would still land the inner
    // payload, which is the half-chain the two-link chain closes.
    let rec_inner_diff = record(1, 2, 99, 3);
    let mut image_inner = [0u8; 10];
    let mut report_inner = TableFixedReport::default();
    table_fixed_run(&plan, &guards, remap, &rec_inner_diff, &mut image_inner, &mut report_inner);

    check(
        image_inner[0] == 0,
        "guard chain: inner link holds the const — inner tag stays 0 when src[1] is not 1",
    );
    check(
        i32::from_le_bytes(image_inner[1..5].try_into().unwrap()) == 0,
        "guard chain: inner link holds the copy — payload stays 0 when the inner tag differs",
    );

    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "rust/R28: {failures} assertion(s) failed");
}
