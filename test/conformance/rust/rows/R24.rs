// R24 — the TEXT ROW's three facts (cell rust/R24, docs/roadmap.sexp:2879
// on this branch; docs/roadmap.sexp:3098 at main eb12ceb3):
//
//   "ill-formed text in the USED units refuses by name (text_ill_formed);
//    text is counted in bytes with the clamp in units; slack is unspecified
//    on read and not a refusal"
//
// THE LAW, sentence by sentence (the card's pointer is
// docs/FIXED-FORM-ALGORITHM.md:1682 — §7's proof item, the corpus row; a
// mechanical match, verified unchanged on fixed-table-form — and the
// governing sentences behind it):
//
//   docs/FIXED-FORM-ALGORITHM.md:333 (the §4.5 `text` row): "`v := SLE(4,
//   record+src)` clamped into `[0, cap]`, `COUNT clamped` if it fired. ...
//   The CONTENT RULES apply to the USED UNITS and nothing else: UTF-8
//   validity over `v` bytes, never over `N`; wide code units over `v`, never
//   `2N`, an astral pair counting two (fix 7). A content violation REFUSES
//   BY NAME, the verdict the packet reader gives";
//   docs/SPEC-TABLES.md:6733 (the constant-size table, `string(N)`): "the
//   length is in BYTES and `N` is a byte capacity";
//   docs/SPEC-TABLES.md:6734 (`wstring(N)`): "the length is in UTF-16 CODE
//   UNITS and the payload is two bytes each";
//   docs/SPEC-TABLES.md:6750 (the slack rule): "UNSPECIFIED ON READ, AND NOT
//   A REFUSAL. ... NON-ZERO SLACK IS NOT `malformed`, NOT A REFUSAL, AND
//   MOVES NO COUNTER."
//
// Three test functions, one per clause; each prints one line per assertion.
//
// The PRODUCTION path is `fx_root_fixed_load`, the generated reader for
// FX1's `fixed table FxRoot` (test/tables/FX1.schema): `label string(8)`,
// its length word at body[22..26] and its 8-byte span at body[26..34]. The
// WIDE half has no generated fixture on this leg — the wide-text unit is
// generated for the C++ reference and Dart and nobody else
// (test/tables/FXW.schema:10-14) — so the wide clauses are asserted on the
// leg's own generated runtime (`table_fixed_run`'s Text op, whose `aux`
// carries the bound in units), the same surface rust/W7's row reaches; the
// entry is built exactly as the plan compiler builds a wide text entry.
//
// Like every row here, this file links the generated fixed modules by path
// and compiles with bare rustc — no --extern/-L flags: the fixed modules
// have no dependency on the serialize runtime.
#![allow(unused_imports)]

#[path = "../../../../build/tables-generated-rust/tblfx1/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblfx1/src/fx1_records.rs"]
mod fx1_records;
#[path = "../../../../build/tables-generated-rust/tblfx1/src/fx1_fixed.rs"]
mod fx1_fixed;

pub use fixed_runtime::*;
pub use fx1_fixed::*;
pub use fx1_records::*;

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

// one printed line per assertion, with values formatted into the line
macro_rules! checkf {
    ($cond:expr, $($arg:tt)*) => {
        check($cond, &format!($($arg)*))
    };
}

// FxRoot's record body offset of `label`'s span byte `k` (the length word is
// body[22..26]): 16-byte header, u32 block length, block, then the record's
// 8-byte hash and 64-byte body.
fn label_byte_at(k: usize) -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FX_ROOT_FIXED_BLOCK.len() + 8 + 26 + k
}

fn label_length_word_at() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FX_ROOT_FIXED_BLOCK.len() + 8 + 22
}

// A lawful record, written by the generated writer, with `label` set to
// `content`.
fn record_with_label(content: &[u8]) -> Vec<u8> {
    let mut row = FxRootRow::default();
    row.label_length = content.len() as i32;
    row.label[..content.len()].copy_from_slice(content);
    let mut file = vec![0u8; fx_root_fixed_measure(1)];
    assert_eq!(
        fx_root_fixed_save(&[row], &mut file),
        Some(file.len()),
        "the fixture must write a whole file"
    );
    file
}

fn load(data: &[u8]) -> (Option<usize>, [FxRootRow; 4], TableFixedReport) {
    let mut values = [FxRootRow::default(); 4];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fx_root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values, report)
}

// A WIDE TEXT entry, built the way the plan compiler builds one (rust/W7's
// row is the precedent): the length word at `src`, the bound in CODE UNITS
// in `aux` (a wstring(4) span — 8 bytes, 4 units).
fn wide_text_entry() -> TableFixedEntry {
    TableFixedEntry {
        src: 0,
        dst: 0,
        size: 8, // the payload's byte span: 2N
        aux: 4,  // the bound in CODE UNITS, wide unit 2
        op: TableFixedOp::Text,
        ..TableFixedEntry::default()
    }
}

fn run_wide(entry: TableFixedEntry, record: &[u8]) -> (Vec<u8>, TableFixedReport) {
    let mut image = [0u8; 12];
    let mut report = TableFixedReport::default();
    table_fixed_run(&[entry], &[], record, &mut image, &mut report);
    (image.to_vec(), report)
}

// CLAUSE 1 (narrow): ill-formed text in the USED bytes refuses by name.
// The forgery is a TRUNCATED SEQUENCE — 0xC3 promises a continuation byte
// the used length does not carry — which is ill-formed UTF-8 and so not text.
#[test]
fn r24_ill_formed_used_units_refuse_by_name_narrow() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 1 (narrow): truncated UTF-8 in the used bytes refuses by name");
    let mut file = record_with_label(b"ab");
    file[label_byte_at(1)] = 0xC3; // "a\xC3" — the continuation byte never comes
    let (n, _, report) = load(&file);
    checkf!(
        report.refused,
        "truncated sequence: the load REFUSES (fix 11): {report:?}"
    );
    checkf!(
        report.reason.name() == "text_ill_formed",
        "truncated sequence: the refusal is NAMED text_ill_formed, got {:?}",
        report.reason.name()
    );
    checkf!(
        n.is_none() && !report.malformed,
        "truncated sequence: nothing decoded, the damage flag stays down: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 1 (wide): ill-formed text in the USED UNITS refuses by name. The
// used units carry an unpaired high surrogate — 0xD800 with no low half —
// which §4.5 names a content violation on the wide flavour.
#[test]
fn r24_ill_formed_used_units_refuse_by_name_wide() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 1 (wide): an unpaired surrogate in the used units refuses by name");
    // [len = 2 units] [0x0048 'H'] [0xD800 lone high] then slack units
    let record: [u8; 12] = [2, 0, 0, 0, 0x48, 0x00, 0x00, 0xD8, b'a', 0, b'b', 0];
    let (image, report) = run_wide(wide_text_entry(), &record);
    checkf!(
        report.refused,
        "unpaired surrogate: the entry REFUSES (fix 11): {report:?}"
    );
    checkf!(
        report.reason.name() == "text_ill_formed",
        "unpaired surrogate: the refusal is NAMED text_ill_formed, got {:?}",
        report.reason.name()
    );
    checkf!(
        image[0..4] == [0, 0, 0, 0],
        "unpaired surrogate: nothing lands, the length word stays zero"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 2: text is counted in bytes with the clamp in units. NARROW: the
// length word is the payload's BYTE count (SPEC-TABLES.md:6733), and a
// length past the bound clamps to it and counts ONCE (FIXED-FORM-ALGORITHM.md:333).
#[test]
fn r24_narrow_counted_in_bytes_with_the_clamp_in_bytes() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 2 (narrow): the length word counts bytes, the clamp counts once");
    // the length word IS the byte count: five payload bytes ride a length of 5
    let (n, values, report) = load(&record_with_label(b"hello"));
    checkf!(n == Some(1), "five bytes: the record reads: {report:?}");
    checkf!(
        values[0].label_length == 5,
        "five bytes: the length word lands 5, in BYTES"
    );
    checkf!(
        report.clamped == 0 && !report.malformed && !report.refused,
        "five bytes: no clamp, no damage: {report:?}"
    );
    // the clamp is in the length's own units: a length word of 100 over a
    // full, LEGAL 8-byte span clamps to 8 and counts one clamped
    let mut file = record_with_label(b"abcdefgh"); // 8 legal bytes, no zero among them
    file[label_length_word_at()..label_length_word_at() + 4].copy_from_slice(&100i32.to_le_bytes());
    let (n, values, report) = load(&file);
    checkf!(n == Some(1), "length 100: the record reads: {report:?}");
    checkf!(
        values[0].label_length == 8,
        "length 100: the length clamps to the bound, 8 bytes"
    );
    checkf!(
        report.clamped == 1,
        "length 100: the clamp counts ONCE: {report:?}"
    );
    checkf!(
        &values[0].label[..8] == b"abcdefgh" && !report.malformed && !report.refused,
        "length 100: the content is read over the clamped length: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 2 (wide): the length word counts CODE UNITS — four payload bytes are
// TWO units — and the clamp holds in those units (cap = size / unit).
#[test]
fn r24_wide_counted_in_units_with_the_clamp_in_units() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 2 (wide): the length word counts code units, the clamp in units");
    // two units ('h','e') ride a length of 2, not the payload's 4 bytes
    let record: [u8; 12] = [2, 0, 0, 0, b'h', 0, b'e', 0, 0, 0, 0, 0];
    let (image, report) = run_wide(wide_text_entry(), &record);
    checkf!(
        image[0..4] == 2u32.to_le_bytes(),
        "wide units: the length word lands 2 CODE UNITS, not the 4 payload bytes"
    );
    checkf!(
        image[4..8] == [b'h', 0, b'e', 0],
        "wide units: the two units land as their bytes"
    );
    checkf!(
        !report.malformed && !report.refused && report.clamped == 0,
        "wide units: no clamp, no damage: {report:?}"
    );
    // the clamp holds in UNITS: a length word of 6 over a 4-unit bound clamps
    // to 4 units and counts one clamped
    let record: [u8; 12] = [6, 0, 0, 0, b'h', 0, b'e', 0, b'l', 0, b'l', 0];
    let (image, report) = run_wide(wide_text_entry(), &record);
    checkf!(
        image[0..4] == 4u32.to_le_bytes(),
        "wide clamp: the length clamps to the bound, 4 units"
    );
    checkf!(
        report.clamped == 1,
        "wide clamp: the clamp counts ONCE: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 3: slack is unspecified on read and not a refusal
// (docs/SPEC-TABLES.md:6750). NARROW: garbage in the slack past the used
// length — well-formed or not — is read correctly, refuses nothing and moves
// no counter.
#[test]
fn r24_slack_unspecified_on_read_and_not_a_refusal() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 3 (narrow): garbage in the slack is not judged");
    let mut file = record_with_label(b"ab");
    for k in 2..8 {
        file[label_byte_at(k)] = 0xFF; // garbage in the slack, used length stays 2
    }
    let (n, values, report) = load(&file);
    checkf!(n == Some(1), "slack garbage: the record reads on: {report:?}");
    checkf!(
        !report.refused && !report.malformed,
        "slack garbage: NOT a refusal and NOT malformed: {report:?}"
    );
    checkf!(
        report.clamped == 0
            && report.unknown == 0
            && report.kind_mismatch == 0
            && report.widened == 0,
        "slack garbage: no counter moves: {report:?}"
    );
    checkf!(
        values[0].label_length == 2 && &values[0].label[..2] == b"ab",
        "slack garbage: the used text lands as written"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 3 (wide): the slack UNITS behind the used length are not judged
// either — even a lone surrogate there, which would refuse as CONTENT in the
// used units, is slack here: unspecified, not a refusal, no counter.
#[test]
fn r24_wide_slack_unspecified_and_not_a_refusal() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 3 (wide): garbage units in the slack are not judged");
    // used = 1 unit; the rest of the span is surrogate and 0xFFFF garbage
    let record: [u8; 12] = [1, 0, 0, 0, 0x48, 0x00, 0x00, 0xD8, 0xFF, 0xFF, 0xFF, 0xFF];
    let (image, report) = run_wide(wide_text_entry(), &record);
    checkf!(
        image[0..4] == 1u32.to_le_bytes(),
        "wide slack: the used length lands 1 unit"
    );
    checkf!(
        !report.malformed && !report.refused && report.clamped == 0,
        "wide slack: not judged, not a refusal, no counter: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}
