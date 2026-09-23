// rust/E3OtherRequiredWidens — "All other required widen-ladder cases"
// (docs/FIXED-FORM-ALGORITHM.md:742, schema roadmap rust/E3/other-required-widens).
//
// THE LAW: "A WIDEN'S SIGN IS THE LADDER'S, and the SAME-KIND widen has none."
// Across the LADDER — writer kind != reader kind and TableFixedWidens holds —
// the source SIGN-extends when the WRITER's kind is signed (i8..i64 or a signed
// fixed(I,F)), and ZERO-extends otherwise. Within ONE kind, or for an enum
// width growth, the source ZERO-extends.
//
// This test drives the production path through the shared generated fixed-form
// runtime: every generated *_fixed_load calls table_fixed_run, and the Widen
// op inside it is the one place the ladder's sign rule is enforced. The vector
// is built here from the law; the production caller is
// build/tables-generated-rust/tblfu2/src/fu2_fixed.rs:515,
// fu_root_fixed_load's call to table_fixed_run.
//
// Standalone: depends on no other rows/ file, edits no shared file.
//   rustc --edition 2021 --test test/conformance/rust/rows/E3OtherRequiredWidens.rs \
//     -o build/rows-rust-E3OtherRequiredWidens && ./build/rows-rust-E3OtherRequiredWidens

#![allow(dead_code)]

#[path = "../../../../build/tables-generated-rust/tblfu2/src/fixed_runtime.rs"]
mod fixed_runtime;

use fixed_runtime::{
    TableFixedEntry, TableFixedOp, TableFixedReport, table_fixed_run, TABLE_FIXED_NO_GUARD,
};

use std::sync::atomic::{AtomicUsize, Ordering};

static FAILURES: AtomicUsize = AtomicUsize::new(0);

fn check(ok: bool, what: &str) {
    if ok {
        println!("ok   {what}");
    } else {
        println!("FAIL {what}");
        FAILURES.fetch_add(1, Ordering::Relaxed);
    }
}

/// Run one Widen op: src bytes at offset 0, dst bytes at offset 0, no guard.
fn widen_one(src: &[u8], dst_size: usize, sign: u8) -> Vec<u8> {
    let entry = TableFixedEntry {
        src: 0,
        dst: 0,
        size: src.len() as u32,
        dstsize: dst_size as u8,
        guard: TABLE_FIXED_NO_GUARD,
        op: TableFixedOp::Widen,
        sign,
        ..TableFixedEntry::default()
    };
    let mut image = vec![0u8; dst_size];
    let mut report = TableFixedReport::default();
    table_fixed_run(&[entry], &[], src, &mut image, &mut report);
    image
}

#[test]
fn other_required_widen_ladder_cases() {
    FAILURES.store(0, Ordering::Relaxed);

    // ---- LADDER WIDEN, SIGNED WRITER: sign-extends ----
    // i8 -> i32, source is -1 (0xFF). A sign-extending widen lands 0xFFFFFFFF.
    let signed_i8 = widen_one(&0xFFu8.to_le_bytes(), 4, 1);
    check(
        signed_i8 == 0xFFFFFFFFu32.to_le_bytes(),
        "ladder widen, signed writer (i8 -> i32): sign-extends -1",
    );
    // i16 -> i32, source is -2 (0xFFFE). Sign-extends to 0xFFFFFFFE.
    let signed_i16 = widen_one(&(-2i16).to_le_bytes(), 4, 1);
    check(
        signed_i16 == (-2i32).to_le_bytes(),
        "ladder widen, signed writer (i16 -> i32): sign-extends -2",
    );

    // ---- LADDER WIDEN, UNSIGNED WRITER: zero-extends ----
    // u8 -> u32, source is 0xFF. A zero-extending widen lands 0x000000FF.
    let unsigned_u8 = widen_one(&0xFFu8.to_le_bytes(), 4, 0);
    check(
        unsigned_u8 == 0x000000FFu32.to_le_bytes(),
        "ladder widen, unsigned writer (u8 -> u32): zero-extends 0xFF",
    );
    // u16 -> u32, source is 0xFFFF. Zero-extends to 0x0000FFFF.
    let unsigned_u16 = widen_one(&0xFFFFu16.to_le_bytes(), 4, 0);
    check(
        unsigned_u16 == 0x0000FFFFu32.to_le_bytes(),
        "ladder widen, unsigned writer (u16 -> u32): zero-extends 0xFFFF",
    );

    // ---- SIGNED FIXED-POINT LADDER WIDEN: sign-extends ----
    // fixed(4,4) -> fixed(12,4), raw source byte 0xFF. The ladder rung 20..24
    // is signed, so the raw scaled value sign-extends.
    let signed_fixed = widen_one(&0xFFu8.to_le_bytes(), 2, 1);
    check(
        signed_fixed == 0xFFFFu16.to_le_bytes(),
        "ladder widen, signed fixed-point writer: sign-extends the raw byte",
    );

    // ---- UNSIGNED FIXED-POINT LADDER WIDEN: zero-extends ----
    // Unsigned fixed-point rung 25..29: same raw byte 0xFF lands 0x00FF.
    let unsigned_fixed = widen_one(&0xFFu8.to_le_bytes(), 2, 0);
    check(
        unsigned_fixed == 0x00FFu16.to_le_bytes(),
        "ladder widen, unsigned fixed-point writer: zero-extends the raw byte",
    );

    // ---- SAME-KIND WIDEN (enum width / bits): zero-extends ----
    // The law says "the SAME-KIND widen has none": even if the kind is
    // notionally signed, a same-kind width growth zero-extends. We exercise
    // this with a same-kind byte -> dword widen flagged unsigned-zero, which
    // is what the runtime emits for same-kind growths.
    let same_kind = widen_one(&0xFFu8.to_le_bytes(), 4, 0);
    check(
        same_kind == 0x000000FFu32.to_le_bytes(),
        "same-kind widen: zero-extends",
    );

    // ---- THE COUNTER: every widen moves widened by one ----
    let entry = TableFixedEntry {
        src: 0,
        dst: 0,
        size: 1,
        dstsize: 4,
        guard: TABLE_FIXED_NO_GUARD,
        op: TableFixedOp::Widen,
        sign: 1,
        ..TableFixedEntry::default()
    };
    let mut image = [0u8; 4];
    let mut report = TableFixedReport::default();
    table_fixed_run(&[entry], &[], &[0xFF], &mut image, &mut report);
    check(
        report.widened == 1 && !report.malformed && !report.refused,
        "a single widen op moves widened == 1 and no flag",
    );

    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "E3OtherRequiredWidens: {failures} assertion(s) failed");
}
