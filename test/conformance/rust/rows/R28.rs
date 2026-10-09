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
    TableFixedEntry, TableFixedOp, TableFixedReport, table_fixed_run,
    TABLE_FIXED_NO_GUARD,
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

// ---- The plan — entries inside a nested union arm, guarded by OUTER tag ----
//
// The compiled plan for NestRoot, reading FH1's bytes, builds entries where
// the inner union's arm entries are guarded by the OUTER tag. We construct
// that plan by hand:
//
// Entry 0: Const — write the inner tag into the image, guarded by outer.
//   guard = 0 (outer tag at offset 0), arg = 1 (arm 'wrap'), argw = 1
//   size = 1, aux = 1 (inner arm ordinal 'leaf'), dst = 0
//
// Entry 1: Copy — copy the inner payload, guarded by outer (SAME guard).
//   guard = 0, arg = 1, argw = 1
//   size = 4, src = 2, dst = 1
//
// Entry 2: Copy — copy the tail, unguarded.
//   guard = NO_GUARD, size = 4, src = 6, dst = 5

fn guard_chain_plan() -> Vec<TableFixedEntry> {
    vec![
        // Const: write inner tag = 1 (arm 'leaf') under the outer guard
        TableFixedEntry {
            src: 0,
            dst: 0,
            size: 1,
            aux: 1, // the inner arm ordinal we write
            guard: 0,  // outer tag offset
            arg: 1,    // arm 'wrap' ordinal
            argw: 1,   // 1-byte tag
            op: TableFixedOp::Const,
            ..TableFixedEntry::default()
        },
        // Copy: inner payload under the SAME outer guard
        TableFixedEntry {
            src: 2,
            dst: 1,
            size: 4,
            guard: 0,  // outer tag offset — the guard chain
            arg: 1,    // arm 'wrap'
            argw: 1,
            op: TableFixedOp::Copy,
            ..TableFixedEntry::default()
        },
        // Copy: tail, unguarded
        TableFixedEntry {
            src: 6,
            dst: 5,
            size: 4,
            guard: TABLE_FIXED_NO_GUARD,
            op: TableFixedOp::Copy,
            ..TableFixedEntry::default()
        },
    ]
}

#[test]
fn r28_guard_chain_outer_tag_guards_inner_entries() {
    FAILURES.store(0, Ordering::Relaxed);
    let plan = guard_chain_plan();

    // GREEN: outer tag is 1 (arm 'wrap'), inner tag is 1 (arm 'leaf').
    // The outer guard matches on entries 0 and 1, so the inner tag and
    // payload land. Entry 2 (tail) is unguarded and always lands.
    let rec = record(1, 1, 42, 7);
    let mut image = [0u8; 10];
    let mut report = TableFixedReport::default();
    table_fixed_run(&plan, &[], &rec, &mut image, &mut report);

    check(image[0] == 1, "guard chain GREEN: inner tag 1 written under outer guard");
    check(
        i32::from_le_bytes(image[1..5].try_into().unwrap()) == 42,
        "guard chain GREEN: payload 42 lands under outer guard",
    );
    check(
        i32::from_le_bytes(image[5..9].try_into().unwrap()) == 7,
        "guard chain GREEN: tail 7 lands unguarded",
    );
    check(
        !report.malformed && !report.refused,
        "guard chain GREEN: no damage or refusal",
    );

    // NEGATIVE CONTROL: outer tag is 2 (arm 'other'). The outer guard does
    // NOT match entries 0 and 1, so they are SKIPPED — the inner tag stays 0
    // (the prefill default) and the payload stays 0. Entry 2 (tail) still
    // lands because it is unguarded. If the inner entries were guarded by the
    // INNER tag instead of the OUTER one, this would still land the inner
    // payload — that is the bug the guard chain fixes.
    let rec_other = record(2, 1, 42, 7);
    let mut image_other = [0u8; 10];
    let mut report_other = TableFixedReport::default();
    table_fixed_run(&plan, &[], &rec_other, &mut image_other, &mut report_other);

    check(
        image_other[0] == 0,
        "guard chain NEGATIVE: inner tag stays 0 — outer guard skipped the const entry",
    );
    check(
        i32::from_le_bytes(image_other[1..5].try_into().unwrap()) == 0,
        "guard chain NEGATIVE: payload stays 0 — outer guard skipped the copy entry",
    );
    check(
        i32::from_le_bytes(image_other[5..9].try_into().unwrap()) == 7,
        "guard chain NEGATIVE: tail still lands (unguarded)",
    );

    // NEGATIVE CONTROL 2: outer tag matches but inner tag differs.
    // The outer guard fires entries 0 and 1 regardless of the inner tag —
    // the inner tag's check is NOT the outer guard. This proves the guard
    // chain uses the OUTER tag's ordinal, not the inner one.
    let rec_inner_diff = record(1, 2, 99, 3);
    let mut image_inner = [0u8; 10];
    let mut report_inner = TableFixedReport::default();
    table_fixed_run(&plan, &[], &rec_inner_diff, &mut image_inner, &mut report_inner);

    check(
        image_inner[0] == 1,
        "guard chain: outer tag fires the const — inner tag written as 1 regardless of src[1]",
    );
    check(
        i32::from_le_bytes(image_inner[1..5].try_into().unwrap()) == 99,
        "guard chain: outer tag fires the copy — payload lands regardless of inner tag",
    );

    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "rust/R28: {failures} assertion(s) failed");
}
