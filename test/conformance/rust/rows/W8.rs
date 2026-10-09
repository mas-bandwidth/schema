// W8 — bytes(N) takes the array row (the rust leg, fixed form, form byte 3).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:202, §3.1, fix 10): the writer writes
// `length` units for text and bytes, NEVER all Max — the slack past the live
// extent stays the template's zeros (§3.4). A `bytes(N)` rides the form as
// kind 14, an ARRAY of u8 (§3.4, §4.1 line 696): the count lands at `aux`
// and the elements at `dst`, the same row as a counted array. Text and bytes
// are the two rows the other way round (line 702-703): text is length-at-dst,
// buffer-at-aux; bytes is buffer-at-dst, length-at-aux.
//
// THE PRODUCTION PATH this test drives, end to end, every stage generated:
//   padded_frame_fixed_save     the write of PaddedFrame, which has bytes(12)
//                               blob (tables/block/Padded.schema:42)
//   padded_frame_fixed_load     the read through the IDENTITY plan (this
//                               build's own layout, hash PADDED_FRAME_FIXED_HASH)
//   padded_frame_fixed_write_body / scatter
//                               the per-field stores, named in the generator
//                               (internal/codegen/rusttable/fixedtable.go,
//                               emitWriteBody / emitScatterBody)
//
// THE VECTOR is built HERE from the law: PaddedFrame declares `blob bytes(12)`,
// so the blob takes 4 + 12 = 16 bytes of body. The writer writes the LENGTH
// word at body+3213 and the live bytes at body+3217, exactly `length` units,
// and the slack at body+3221..3229 stays the template's zeros. The file-level
// offsets come from the layout: header 16, layout-len 4, layout 395, record
// hash 8, body 3229 → file offset 423 for the body's first byte.
//
// THE NEGATIVE CONTROL is the wire a wrong writer would produce: the length
// word at the buffer offset and the live bytes at the length offset — the
// text convention, as it was before fix 10. A reader that took the bytes
// row from the layout the SAME way would still land the buffer at its
// declared offset, and the value that comes back is the thing that proves
// the row was an array's, not a text field's. The control keeps every call
// and every include: it is the WRONG WIRE under the SAME reader.
//
//   rustc --edition 2021 --test ... test/conformance/rust/rows/W8.rs \
//       -o build/rows-rust-W8 && ./build/rows-rust-W8
//
// Exit 0 green, 1 red; one printed line per assertion.

use blockdemo::{
    padded_frame_fixed_load, padded_frame_fixed_measure, padded_frame_fixed_save,
    PaddedFrameRow, TableFixedEntry, TableFixedReport, TABLE_FIXED_HEADER_BYTES,
};

static FAILURES: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);

fn check(ok: bool, what: &str) {
    if ok {
        println!("ok - {what}");
    } else {
        println!("FAIL - {what}");
        FAILURES.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }
}

const LAYOUT_LEN_BYTES: usize = 4;
const RECORD_HASH_BYTES: usize = 8;
// 64 rows × 50 bytes/row + 13 (marker, stamp, count) = 3213
const BODY_BLOB_LENGTH_AT: usize = 3213;
const BODY_BLOB_BUFFER_AT: usize = 3217;
const BODY_BLOB_BUFFER_MAX: usize = 12;

fn file_body_offset() -> usize {
    TABLE_FIXED_HEADER_BYTES + LAYOUT_LEN_BYTES + blockdemo::PADDED_FRAME_FIXED_BLOCK.len()
        + RECORD_HASH_BYTES
}

#[test]
fn test_row_w8() {
    // ---- THE WRITE: one record whose `blob` carries FOUR live bytes ----
    //
    // The writer writes the length word at body+3213, the four live bytes at
    // body+3217..3221, and the eight bytes past them (body+3221..3229) stay
    // the template's zeros — `length` units, never all Max.
    let mut row = PaddedFrameRow::default();
    row.marker = 0x42;
    row.stamp = 0x0102_0304_0506_0708;
    row.rows_count = 0;
    row.blob_length = 4;
    row.blob[0] = 0xDE;
    row.blob[1] = 0xAD;
    row.blob[2] = 0xBE;
    row.blob[3] = 0xEF;

    let measure = padded_frame_fixed_measure(1);
    let mut file = vec![0xABu8; measure];
    let written = padded_frame_fixed_save(&[row], &mut file).expect("the writer saves the record");
    check(written == measure, "W8: the production writer saves the record (returns measure bytes)");

    let body = file_body_offset();

    // THE LENGTH WORD rides at body+3213 (= the body's blob_length offset).
    let len_word = u32::from_le_bytes(
        file[body + BODY_BLOB_LENGTH_AT..body + BODY_BLOB_LENGTH_AT + 4]
            .try_into()
            .expect("four bytes for the length word"),
    );
    check(len_word == 4, "W8: the length word at body+3213 is 4 (the live extent, not Max)");

    // THE LIVE BYTES ride at body+3217 (the body's blob buffer offset).
    let live = &file[body + BODY_BLOB_BUFFER_AT..body + BODY_BLOB_BUFFER_AT + 4];
    check(live == &[0xDE, 0xAD, 0xBE, 0xEF], "W8: the four live bytes ride at body+3217..3221");

    // THE SLACK past the live bytes is the TEMPLATE'S ZEROS — `length` units,
    // never all Max. A writer that took the text row or wrote the full buffer
    // would leave non-zero bytes here, and that is the broken case.
    let mut slack_ok = true;
    for i in BODY_BLOB_BUFFER_AT + 4..BODY_BLOB_BUFFER_AT + BODY_BLOB_BUFFER_MAX {
        if file[body + i] != 0 {
            slack_ok = false;
        }
    }
    check(slack_ok, "W8: the slack past the live bytes is the template's zeros (body+3221..3229)");

    // ---- THE READ: identity plan (this build's own hash) reads it back ---
    let mut values = [PaddedFrameRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = padded_frame_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens its own file");
    check(n == 1, "W8: the reader reads one record");
    let back = &values[0];
    check(back.blob_length == 4, "W8: the reader reads the length word as 4");
    let back_live: [u8; 4] = back.blob[..4].try_into().expect("four bytes");
    check(back_live == [0xDE, 0xAD, 0xBE, 0xEF], "W8: the reader reads the four live bytes back");

    // ---- THE NEGATIVE CONTROL: the wrong wire under the same reader ------
    //
    // A writer that took the TEXT ROW (length at dst, buffer at aux) would
    // produce a wire where the length word lands at the buffer's offset
    // (body+3217) and the live bytes ride at the length's offset (body+3213).
    // Construct that wrong wire by hand — same template, same length, same
    // payload, just the two columns swapped — and the reader takes the bytes
    // ROW from its own layout: it reads the LENGTH word at body+3213
    // (the buffer slot in this wrong wire) and the BUFFER at body+3217
    // (the length slot in this wrong wire). The value that comes back is the
    // thing that proves the row was an array's, not a text field's.
    let mut wrong = file.clone();
    // body+3213..3217 was the length word (= 4); put the live bytes there.
    wrong[body + BODY_BLOB_LENGTH_AT..body + BODY_BLOB_LENGTH_AT + 4]
        .copy_from_slice(&[0xDE, 0xAD, 0xBE, 0xEF]);
    // body+3217..3221 was the live bytes; put the length word there.
    wrong[body + BODY_BLOB_BUFFER_AT..body + BODY_BLOB_BUFFER_AT + 4]
        .copy_from_slice(&4u32.to_le_bytes());

    let mut values = [PaddedFrameRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = padded_frame_fixed_load(&mut values, &wrong, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the wrong wire");
    check(n == 1, "W8 NEGATIVE CONTROL: the reader still opens a wire where length and buffer were swapped");
    let back = &values[0];
    let back_live: [u8; 4] = back.blob[..4].try_into().expect("four bytes");
    // Under the wrong wire the reader lands the LENGTH WORD (4 LE: 04 00 00 00)
    // at the buffer offset, so back.blob[0..4] is { 04, 00, 00, 00 } and
    // back.blob_length is the bytes that were at the length slot
    // (0xDEADBEEF clamped to [0, 12] = 12). Neither is the live pattern.
    let live_was_lost = back_live != [0xDE, 0xAD, 0xBE, 0xEF] || back.blob_length != 4;
    check(live_was_lost, "W8 NEGATIVE CONTROL: the text row really did write the count into the buffer — live bytes lost");

    let fails = FAILURES.load(std::sync::atomic::Ordering::Relaxed);
    assert_eq!(fails, 0, "W8: {fails} assertion(s) failed");
}
