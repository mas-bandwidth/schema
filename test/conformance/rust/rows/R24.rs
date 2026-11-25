// R24 — the TEXT ROW's three facts (cell rust/R24, docs/roadmap.sexp:2879
// on this branch; docs/roadmap.sexp:3098 at main eb12ceb3):
//
//   "ill-formed text in the USED units refuses by name (text_ill_formed);
//    text is counted in bytes with the clamp in units; slack is unspecified
//    on read and not a refusal"
//
// AS THE LAW STANDS SINCE THE STRICT-ONLY AND DEAD-BYTE RULINGS: a length past
// the bound REFUSES (§4.5, never clamped), and the slack is DEAD — never
// judged as text, but a non-zero dead byte refuses wire_damaged (§3.1, §5.3
// step 11). Until 2026-11-25 this row asserted the retired clamp and the
// retired "slack is not a refusal" through report fields (`oor`, `damaged`)
// the report no longer has, and asserted that the plan RUN refuses ill-formed
// wide units and ignores wide slack; the run judges neither (the content and
// dead-byte checks are LOAD's, §5.3 step 11) and this leg declares no wide
// text field for a load to judge, so the wide half holds only the run's own
// fact — the length counted in code units.
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

fn run_wide(entry: TableFixedEntry, record: &[u8]) -> (bool, Vec<u8>) {
    let mut image = [0u8; 12];
    let mut report = TableFixedReport::default();
    let ran = table_fixed_run(&[entry], &[], &[], record, &mut image, &mut report);
    (ran, image.to_vec())
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
    checkf!(n.is_none(), "truncated sequence: nothing decoded: {report:?}");
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 2 (narrow): the length word is the payload's BYTE count
// (SPEC-TABLES.md:6733), and a length past the bound REFUSES the whole read
// (FIXED-FORM-ALGORITHM §4.5, the `text` row: "if v < 0 or v > size then
// REFUSE length_over_bound"); it is never clamped and landed. The verdict is
// compared and the name is diagnostic: this leg answers out_of_range, or
// text_ill_formed when the content pass meets the bytes first.
#[test]
fn r24_narrow_counted_in_bytes_past_the_bound_refuses() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 2 (narrow): the length word counts bytes, past the bound refuses");
    // the length word IS the byte count: five payload bytes ride a length of 5
    let (n, values, report) = load(&record_with_label(b"hello"));
    checkf!(n == Some(1), "five bytes: the record reads: {report:?}");
    checkf!(
        values[0].label_length == 5,
        "five bytes: the length word lands 5, in BYTES"
    );
    checkf!(!report.refused, "five bytes: nothing refused: {report:?}");
    // a length word of 100 over a full, LEGAL 8-byte span refuses the read
    let mut file = record_with_label(b"abcdefgh"); // 8 legal bytes, no zero among them
    file[label_length_word_at()..label_length_word_at() + 4].copy_from_slice(&100i32.to_le_bytes());
    let (n, _, report) = load(&file);
    checkf!(
        n.is_none() && report.refused,
        "length 100: past the bound of 8 the read REFUSES, never clamps: {report:?}"
    );
    checkf!(
        report.reason == TableFixedReason::OutOfRange || report.reason == TableFixedReason::TextIllFormed,
        "length 100: refused by a bound name, got {:?}",
        report.reason.name()
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 2 (wide): the length word counts CODE UNITS — four payload bytes are
// TWO units — on the run's own Text op, whose `aux` carries the bound in units.
#[test]
fn r24_wide_counted_in_units() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 2 (wide): the length word counts code units");
    // two units ('h','e') ride a length of 2, not the payload's 4 bytes
    let record: [u8; 12] = [2, 0, 0, 0, b'h', 0, b'e', 0, 0, 0, 0, 0];
    let (ran, image) = run_wide(wide_text_entry(), &record);
    checkf!(ran, "wide units: the run reaches nothing past the record");
    checkf!(
        image[0..4] == 2u32.to_le_bytes(),
        "wide units: the length word lands 2 CODE UNITS, not the 4 payload bytes"
    );
    checkf!(
        image[4..8] == [b'h', 0, b'e', 0],
        "wide units: the two units land as their bytes"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 3: the slack past the used length is DEAD. It is not judged as text
// (the content rule is over the used units alone), and a non-zero dead byte
// refuses the whole load as wire_damaged (FIXED-FORM-ALGORITHM §3.1, §4.5,
// §5.3 step 11; docs/rulings.json fixed-dead-bytes-are-zero). Zero slack reads.
#[test]
fn r24_slack_is_dead_never_judged_as_text() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 3 (narrow): garbage in the slack refuses wire_damaged, never text_ill_formed");
    let mut file = record_with_label(b"ab");
    for k in 2..8 {
        file[label_byte_at(k)] = 0xFF; // garbage in the slack, used length stays 2
    }
    let (n, _, report) = load(&file);
    checkf!(n.is_none() && report.refused, "slack garbage: the load REFUSES: {report:?}");
    checkf!(
        report.reason == TableFixedReason::WireDamaged,
        "slack garbage: the dead-byte rule's wire_damaged, never text_ill_formed, got {:?}",
        report.reason.name()
    );
    let (n, values, report) = load(&record_with_label(b"ab"));
    checkf!(n == Some(1) && !report.refused, "zero slack: the record reads: {report:?}");
    checkf!(
        values[0].label_length == 2 && &values[0].label[..2] == b"ab",
        "zero slack: the used text lands as written"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}
