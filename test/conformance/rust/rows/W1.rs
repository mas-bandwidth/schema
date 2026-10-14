// ROW W1 — "write slack is template zeros" (docs/roadmap.sexp:3475,
// schema#898 audit; the matrix's rust/W1 cell).
//
// THE LAW, governing sentence: docs/SPEC-TABLES.md:6745 — "**ZERO ON WRITE.** A
// writer zero-fills every byte of slack — the bytes past a string's length,
// past an array's count, behind a union's narrower arm, and under an absent
// optional's present flag. It costs the writer nothing: the template is zero
// everywhere a value lands, so the zeros are already there." Fix 1 of
// docs/FIXED-FORM-ALGORITHM.md:1724 states it for this cell: "the writer
// writes `length` units and `count` elements onto the zeroed template and
// stops, never the caller's leftovers or an element's default image."
//
// THE PRODUCTION PATH: the fixed-form WRITER the compiler emits for the Rust
// leg is `fx_root_fixed_save` (build/tables-generated-rust/tblfx1/src/fx1_fixed.rs),
// which for every record does `body.fill(0)` — the zeroed template — and then
// `fx_root_fixed_write_body`, a straight line of stores that writes `length`
// units and `count` elements and STOPS. This test drives that path end to end
// with a value whose UNUSED slots carry the caller's leftovers and a buffer
// the caller handed in DIRTY (every byte 0xff), and asserts the slack that
// comes out is the template's zeros — never the leftovers and never an
// element's default image.
//
// It is a STANDALONE rustc test: it compiles the generated fixed-form modules
// in place (the same generated tree the leg's driver links, build/tables-generated-rust,
// from make/rust.mk's build/tables-generated-rust/.stamp target) and depends
// on no other rows/ file. Run from ./repo:
//
//   rustc --edition 2021 --test test/conformance/rust/rows/W1.rs -o build/rows-rust-W1 \
//       && ./build/rows-rust-W1
//
// Exit 0 green, exit 1 red, one printed line per assertion.
#![allow(non_snake_case)]

#[path = "../../../../build/tables-generated-rust/tblfx1/src/fx1_records.rs"]
mod fx1_records;
#[path = "../../../../build/tables-generated-rust/tblfx1/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblfx1/src/fx1_fixed.rs"]
mod fx1_fixed;
pub use fx1_records::*;
pub use fixed_runtime::*;
pub use fx1_fixed::*;

#[test]
fn TestRowW1() {
    let mut fails = 0u32;

    // A value whose UNUSED slots carry the CALLER's leftovers: a label whose
    // bytes past the used length are 'X', an array whose slots past the live
    // count hold 0x11223344 / 0x55667788, a blob whose bytes past the used
    // length are 0xff. A writer that copied the caller's leftovers or an
    // element's full image into the slack would put these bytes on the wire.
    let mut v = FxRootRow::default();
    v.keep = 7;
    v.narrow = 3;
    v.renamed = 5;
    v.gone = 9;
    v.nested = FxNestedRow { a: 1, b: 2 };
    v.label = *b"hiXXXXXXX"; // string(8), used length 2, leftovers 'X'
    v.label_length = 2;
    v.marks = [1, 2, 0x11223344, 0x55667788]; // [..4]int32, live count 2
    v.marks_count = 2;
    v.blob = [0xaa, 0xbb, 0xcc, 0xff, 0xee, 0xdd]; // bytes(6), used length 3
    v.blob_length = 3;

    // A DIRTY caller buffer: every byte is 0xff before the writer runs, so a
    // writer that relied on the caller's zeros would leave 0xff in the slack.
    // The law says the writer's own template carries the zeros.
    let mut out = vec![0xffu8; fx_root_fixed_measure(1)];
    if fx_root_fixed_save(&[v], &mut out) != Some(out.len()) {
        println!("FAIL: W1 write: save did not fill what measure says");
        fails += 1;
    }

    // The record body: after the pinned header, the u32 layout length and the
    // layout itself sits one record — 8 hash bytes, then the 64-byte body.
    let rec = TABLE_FIXED_HEADER_BYTES + 4 + FX_ROOT_FIXED_BLOCK.len();
    let body = &out[rec + 8..rec + 8 + FX_ROOT_FIXED_BODY_BYTES];

    // The USED bytes land exactly: the scalars, the lengths, and the live
    // extent of each slack-carrying field.
    if &body[0..4] != &7u32.to_le_bytes()
        || &body[4..6] != &3u16.to_le_bytes()
        || &body[6..10] != &5u32.to_le_bytes()
        || &body[10..14] != &9u32.to_le_bytes()
    {
        println!("FAIL: W1 write: a scalar's bytes are not the value's");
        fails += 1;
    }
    if &body[22..26] != &2u32.to_le_bytes() || &body[26..28] != b"hi" {
        println!("FAIL: W1 write: the string's used length or bytes are not the value's");
        fails += 1;
    }
    if &body[34..38] != &2u32.to_le_bytes() {
        println!("FAIL: W1 write: the array's count is not the value's");
        fails += 1;
    }
    if &body[38..42] != &1i32.to_le_bytes() || &body[42..46] != &2i32.to_le_bytes() {
        println!("FAIL: W1 write: the array's live elements are not the value's");
        fails += 1;
    }
    if &body[54..58] != &3u32.to_le_bytes() || &body[58..61] != &[0xaa, 0xbb, 0xcc] {
        println!("FAIL: W1 write: the bytes' used length or bytes are not the value's");
        fails += 1;
    }

    // THE LAW, asserted byte by byte: the slack past the string's length, the
    // array's count and the bytes' length is TEMPLATE ZEROS — never the
    // caller's leftovers (the 'X's, 0x11223344/0x55667788, 0xff), never an
    // element's default image, and never the 0xff of the dirty caller buffer.
    if &body[28..34] != &[0u8; 6] {
        println!("FAIL: W1 write: string(8) slack past the used length is not template zeros");
        fails += 1;
    }
    if &body[46..54] != &[0u8; 8] {
        println!("FAIL: W1 write: [..4]int32 slack past the live count is not template zeros");
        fails += 1;
    }
    if &body[61..64] != &[0u8; 3] {
        println!("FAIL: W1 write: bytes(6) slack past the used length is not template zeros");
        fails += 1;
    }

    if fails != 0 {
        panic!("W1 write slack is not template zeros ({fails} assertion(s) failed)");
    }
    println!("PASS: W1 write slack is template zeros (string, array, bytes)");
}