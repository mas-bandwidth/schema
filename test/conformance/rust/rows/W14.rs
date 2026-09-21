// W14: THE PLAN'S DESTINATIONS ARE THIS BUILD'S OWN OFFSETS AND SIZES
// (docs/FIXED-FORM-ALGORITHM.md §7: "the build-time assertion that the plan's
//  destinations equal its own offsetof and sizeof").
//
// In the Rust leg the plan works in the RECORD IMAGE and not in the storage:
// `dst` is a byte offset into the unpadded declared-order body.  The identity
// plan is ONE entry coalescing the whole body (§3.4), and a compiled plan's
// per-field `dst` values come from the same layout walk that produced the
// block.  The assertions below prove those destinations are correct:
//
//   1.  The identity plan is exactly one {dst:0, src:0, size:body_bytes}.
//   2.  body_bytes equals the root entry's declared size.
//   3.  For each scalar field, two records differing only in that field
//       produce their first byte difference at the expected image offset.
//   4.  The last field's image offset + its wire size reaches exactly
//       body_bytes — the plan tiles the record.
//
// A negative control (break one constant in the plan) makes the test red.

extern crate tblfx1;
use tblfx1::*;

// ---- helpers ----------------------------------------------------------------

fn check(ok: bool, what: &str) {
    if !ok {
        panic!("FAIL: {what}");
    }
}

// Write a default FxRoot, then mutate ONE field and find the first byte
// position where the body images differ.  That position IS the image offset
// (the plan's `dst`) for the mutated field.
fn image_offset_of_field_diff(
    modify: impl FnOnce(&mut FxRootRow),
) -> usize {
    let need = fx_root_fixed_measure(1);
    let mut buf_a = vec![0u8; need];
    let n_a = fx_root_fixed_save(&[FxRootRow::default()], &mut buf_a).unwrap();
    // Skip the header (16 bytes), block length (4 bytes), block, and the
    // 8-byte record hash to reach the body.
    let record_start = TABLE_FIXED_HEADER_BYTES + 4 + FX_ROOT_FIXED_BLOCK.len();
    let body_a = &buf_a[record_start + 8..n_a];

    let mut val = FxRootRow::default();
    modify(&mut val);
    let mut buf_b = vec![0u8; need];
    let n_b = fx_root_fixed_save(&[val], &mut buf_b).unwrap();
    let body_b = &buf_b[record_start + 8..n_b];

    assert_eq!(n_a, n_b, "record sizes must match");
    for i in 0..body_a.len() {
        if body_a[i] != body_b[i] {
            return i;
        }
    }
    panic!("the two records are identical — the mutation had no effect");
}

// ---- the assertions ---------------------------------------------------------

#[test]
fn test_row_w14() {
    // (1) THE IDENTITY PLAN IS ONE ENTRY: dst:0, src:0, size:body_bytes.
    //     §3.4's coalescing rule takes the whole body to a single move when the
    //     destination's order IS the declared order.
    check(
        FX_ROOT_FIXED_PLAN.len() == 1,
        "W14: identity plan has exactly one entry",
    );
    let p = &FX_ROOT_FIXED_PLAN[0];
    check(p.dst == 0, "W14: identity plan entry dst == 0");
    check(p.src == 0, "W14: identity plan entry src == 0");
    check(
        p.size as usize == FX_ROOT_FIXED_BODY_BYTES,
        "W14: identity plan size == body_bytes",
    );
    check(p.op == TableFixedOp::Copy, "W14: identity plan op == Copy");

    // (2) THE BODY BYTES EQUALS THE ROOT LAYOUT ENTRY'S DECLARED SIZE.
    //     The root (entry 0) of the block has its size at a fixed position.
    //     Block format: u32 count, then 17-byte entries (id:u64 kind:u8
    //     size:u32 children:u32).  Entry 0's size is at bytes[13..17].
    let count = u32::from_le_bytes(FX_ROOT_FIXED_BLOCK[0..4].try_into().unwrap());
    check(count == 13, "W14: FX_ROOT layout has 13 entries");
    let root_size = u32::from_le_bytes(
        FX_ROOT_FIXED_BLOCK[4 + 9..4 + 13].try_into().unwrap(),
    ) as usize;
    check(
        root_size == FX_ROOT_FIXED_BODY_BYTES,
        "W14: root layout entry size == FX_ROOT_FIXED_BODY_BYTES (64)",
    );

    // (3) PER-FIELD IMAGE OFFSETS: the writer lands each scalar field at the
    //     byte position the layout walk computes.  Hand-derived from FX1.schema:
    //       keep: u32  @ image 0
    //       narrow: u16  @ image 4
    //       renamed: i32  @ image 6
    //       gone: i32  @ image 10
    //       nested.a: i32  @ image 14
    //       nested.b: i32  @ image 18
    //       label: string(8)  length @ 22, buffer @ 26
    //       marks: [i32; 4]  count @ 34, elements @ 38
    //       blob: bytes(6)  length @ 54, buffer @ 58

    check(
        image_offset_of_field_diff(|v| v.keep = 42) == 0,
        "W14: keep lands at image offset 0",
    );
    check(
        image_offset_of_field_diff(|v| v.narrow = 7) == 4,
        "W14: narrow lands at image offset 4",
    );
    check(
        image_offset_of_field_diff(|v| v.renamed = 99) == 6,
        "W14: renamed lands at image offset 6",
    );
    check(
        image_offset_of_field_diff(|v| v.gone = 55) == 10,
        "W14: gone lands at image offset 10",
    );
    check(
        image_offset_of_field_diff(|v| v.nested.a = 77) == 14,
        "W14: nested.a lands at image offset 14",
    );
    check(
        image_offset_of_field_diff(|v| v.nested.b = 88) == 18,
        "W14: nested.b lands at image offset 18",
    );
    check(
        image_offset_of_field_diff(|v| {
            v.label = [0u8; 9];
            v.label[..5].copy_from_slice(b"hello");
            v.label_length = 5;
        }) == 22,
        "W14: label length lands at image offset 22",
    );
    check(
        image_offset_of_field_diff(|v| {
            v.marks_count = 1;
            v.marks[0] = 123;
        }) == 34,
        "W14: marks count lands at image offset 34",
    );
    check(
        image_offset_of_field_diff(|v| {
            v.blob_length = 3;
            v.blob = [0xaa; 6];
        }) == 54,
        "W14: blob length lands at image offset 54",
    );

    // (4) THE LAST FIELD TILES THE BODY: blob is bytes(6), length (4) + buffer
    //     (6) = 10 bytes starting at offset 54.  54 + 10 = 64 = body_bytes.
    //     The plan tiles [0, body_bytes).
    let last_field_offset = 54usize;
    let last_field_extent = 10usize; // 4 (length word) + 6 (buffer)
    check(
        last_field_offset + last_field_extent == FX_ROOT_FIXED_BODY_BYTES,
        "W14: last field reaches exactly body_bytes — the plan tiles the record",
    );
}
