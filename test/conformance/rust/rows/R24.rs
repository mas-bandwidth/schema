// R24 — the TEXT ROW's three facts (cell rust/R24, docs/roadmap.sexp:2879
// on this branch; docs/roadmap.sexp:3098 at main eb12ceb3):
//
//   "ill-formed text in the USED units refuses by name (text_ill_formed);
//    text is counted in bytes with the clamp in units; slack is unspecified
//    on read and not a refusal"
//
// THE LAW MOVED TWICE since this row was cut, and the row follows it:
//   * strict-only-read (docs/rulings.json, 2026-11-03): no read clamps. A
//     length outside [0, cap] refuses the WHOLE load (docs/FIXED-FORM-ALGORITHM.md
//     :449, §4.5 `text` row: "REFUSE length_over_bound"; this leg's reason is
//     OutOfRange, the reason being per-language detail: refusal-verdict-only).
//     Clause 2's "clamp in units" is now "the BOUND in units".
//   * fixed-dead-bytes-are-zero (docs/rulings.json, 2026-11-10), superseding
//     the slack rule quoted below: "The bytes past the used length are DEAD,
//     and a non-zero one refuses wire_damaged in the CHECK pass" (§4.5).
//     Clause 3 is now "slack is DEAD and a non-zero byte refuses as damage".
// The original quotes are kept for the mapping of each old assertion.
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
// its length word at body[22..26] and its 8-byte span at body[26..34].
// (Wide text is RETIRED since this row was cut — docs/rulings.json
// wstring-removed-utf8 — so clause 1's wide half is now that ruling's verdict;
// clauses 2 and 3 keep their wide-shaped Text entries as the run's plain
// bound-in-`aux` arithmetic, which is unit-agnostic.) The
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
    table_fixed_run(&[entry], &[], &[], record, &mut image, &mut report);
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
        n.is_none() && !report.damaged,
        "truncated sequence: nothing decoded, the damage flag stays down: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 1 (wide), AS RULED NOW: wide text is RETIRED (docs/rulings.json
// wstring-removed-utf8, 2026-10-31: "I thought we removed wstring from
// serialize, and replaced with utf8, we should do the same from schema"). Its
// kind code 33 is "retired (wide text) — NOT in the closed set"
// (docs/FIXED-FORM-ALGORITHM.md:92), so there are no wide USED UNITS left to
// judge: this clause once ran a hand-built wide Text entry over an unpaired
// surrogate and asserted text_ill_formed. Its replacement is the verdict the
// spec gives a wide-text field on the wire today — a layout naming kind 33 is
// a form this reader never saw, refused BY NAME layout_kind_unknown, never
// stepped over — beside a control that the same layout with a narrow
// string(N) (kind 12) parses. The narrow content refusal is clause 1 above.
fn one_field_layout(kind: u8, size: u32) -> Vec<u8> {
    let mut b = Vec::new();
    b.extend_from_slice(&2u32.to_le_bytes());
    for (id, k, sz, ch) in [(1u64, 13u8, size, 1u32), (2u64, kind, size, 0u32)] {
        b.extend_from_slice(&id.to_le_bytes());
        b.push(k);
        b.extend_from_slice(&sz.to_le_bytes());
        b.extend_from_slice(&ch.to_le_bytes());
    }
    b
}

#[test]
fn r24_wide_text_is_retired_and_refused_by_name() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 1 (wide): a retired wide-text kind (33) is refused layout_kind_unknown");
    let wide = one_field_layout(33, 12);
    let got = TableFixedBlock::parse(&wide).err();
    checkf!(
        got == Some(TableFixedReason::LayoutKindUnknown),
        "wide text: a layout naming kind 33 refuses BY NAME layout_kind_unknown, got {:?}",
        got.map(|r| r.name())
    );
    let narrow = one_field_layout(12, 12);
    checkf!(
        TableFixedBlock::parse(&narrow).is_ok(),
        "wide text control: the same layout with string(N), kind 12, parses"
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
    // strict-only-read: was `oor == 0` (no clamp); now not refused out_of_range
    checkf!(
        report.reason.name() == "none" && !report.damaged && !report.refused,
        "five bytes: not out of range, no damage: {report:?}"
    );
    // the BOUND is in the length's own units: a length word of 100 over a
    // full, LEGAL 8-byte span is past the 8-byte bound. strict-only-read: it
    // once clamped to 8 and counted one clamped; it now refuses the whole
    // load (docs/FIXED-FORM-ALGORITHM.md:449) and lands nothing (R4).
    let mut file = record_with_label(b"abcdefgh"); // 8 legal bytes, no zero among them
    file[label_length_word_at()..label_length_word_at() + 4].copy_from_slice(&100i32.to_le_bytes());
    let (n, values, report) = load(&file);
    checkf!(n.is_none(), "length 100: the load REFUSES (was: reads on clamped): {report:?}");
    checkf!(
        values[0].label_length == FxRootRow::default().label_length,
        "length 100: nothing lands, the length is the declared default (was: clamps to 8)"
    );
    checkf!(
        report.refused && report.reason == TableFixedReason::OutOfRange,
        "length 100: refused out_of_range (was: the clamp counts ONCE): {report:?}"
    );
    checkf!(
        !report.damaged,
        "length 100: a bound refusal is not damage: {report:?}"
    );
    // the bound is exactly 8 bytes: a length word of 8 over the same span reads
    let mut file = record_with_label(b"abcdefgh");
    file[label_length_word_at()..label_length_word_at() + 4].copy_from_slice(&8i32.to_le_bytes());
    let (n, values, report) = load(&file);
    checkf!(
        n == Some(1) && values[0].label_length == 8 && &values[0].label[..8] == b"abcdefgh" && !report.refused,
        "length 8: AT the bound reads whole: {report:?}"
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
    // strict-only-read: `oor` is the runtime's `held` now — the out-of-range
    // mark LOAD refuses the whole batch out_of_range on (fixedform.go's load:
    // `if report.held != 0 { return report.refuse(OutOfRange) }`)
    checkf!(
        !report.damaged && !report.refused && report.held == 0,
        "wide units: nothing out of range, no damage: {report:?}"
    );
    // the BOUND holds in UNITS: a length word of 6 over a 4-unit bound is past
    // it. strict-only-read: the run raises the out-of-range mark ONCE, which
    // is the load's out_of_range refusal; it no longer clamps-and-counts as a
    // landed value (the image's word is scratch the refused load discards).
    let record: [u8; 12] = [6, 0, 0, 0, b'h', 0, b'e', 0, b'l', 0, b'l', 0];
    let (_image, report) = run_wide(wide_text_entry(), &record);
    checkf!(
        report.held == 1,
        "wide bound: a length of 6 units past the 4-unit bound raises the out-of-range mark ONCE: {report:?}"
    );
    // and 4 units is AT the bound: no mark
    let record: [u8; 12] = [4, 0, 0, 0, b'h', 0, b'e', 0, b'l', 0, b'l', 0];
    let (image, report) = run_wide(wide_text_entry(), &record);
    checkf!(
        report.held == 0 && image[0..4] == 4u32.to_le_bytes(),
        "wide bound: 4 units is in the bound, no mark: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 3, AS RULED NOW (docs/rulings.json fixed-dead-bytes-are-zero,
// superseding docs/SPEC-TABLES.md:6750's "unspecified on read and not a
// refusal"): the bytes past the used length are DEAD, and a non-zero one
// refuses the whole load as wire damage in the CHECK pass, before any record
// lands (docs/FIXED-FORM-ALGORITHM.md §4.5 `text` row, §5.3 step 11). The old
// assertions "reads on / not a refusal / no counter / text lands" map to
// "returns None / damaged / no counter / nothing lands".
#[test]
fn r24_slack_is_dead_and_nonzero_refuses_as_damage() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 3 (narrow): garbage in the slack refuses as damage");
    let mut file = record_with_label(b"ab");
    for k in 2..8 {
        file[label_byte_at(k)] = 0xFF; // garbage in the dead span, used length stays 2
    }
    let (n, values, report) = load(&file);
    checkf!(n.is_none(), "slack garbage: the load REFUSES (was: reads on): {report:?}");
    checkf!(
        report.damaged,
        "slack garbage: wire damage, the dead-byte check (was: not malformed): {report:?}"
    );
    checkf!(
        report.held == 0 && report.unknown == 0 && report.widened == 0,
        "slack garbage: no counter moves: {report:?}"
    );
    checkf!(
        values[0].label_length == FxRootRow::default().label_length,
        "slack garbage: nothing lands (was: the used text lands as written)"
    );
    // THE CONTROL: the same record with ZERO slack reads, the text as written
    let (n, values, report) = load(&record_with_label(b"ab"));
    checkf!(
        n == Some(1) && !report.damaged && !report.refused
            && values[0].label_length == 2 && &values[0].label[..2] == b"ab",
        "zero slack: the record reads and the used text lands: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// CLAUSE 3 (wide): the slack UNITS behind the used length are not judged
// as CONTENT — a lone surrogate there is no content violation, and the run
// raises nothing for it — but they are DEAD (fixed-dead-bytes-are-zero), so
// the CHECK pass the load runs before any record lands flags them.
#[test]
fn r24_wide_slack_is_not_content_and_is_dead() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("R24 clause 3 (wide): garbage units in the slack are dead, not content");
    // used = 1 unit; the rest of the span is surrogate and 0xFFFF garbage
    let record: [u8; 12] = [1, 0, 0, 0, 0x48, 0x00, 0x00, 0xD8, 0xFF, 0xFF, 0xFF, 0xFF];
    let (image, report) = run_wide(wide_text_entry(), &record);
    checkf!(
        image[0..4] == 1u32.to_le_bytes(),
        "wide slack: the used length lands 1 unit"
    );
    // strict-only-read: was `oor == 0`; the run's out-of-range mark is `held`
    checkf!(
        !report.damaged && !report.refused && report.held == 0,
        "wide slack: not a content refusal, no out-of-range mark: {report:?}"
    );
    checkf!(
        table_fixed_dead(&[wide_text_entry()], &[], &record),
        "wide slack: the CHECK pass flags the non-zero dead units (fixed-dead-bytes-are-zero)"
    );
    let clean: [u8; 12] = [1, 0, 0, 0, 0x48, 0x00, 0, 0, 0, 0, 0, 0];
    checkf!(
        !table_fixed_dead(&[wide_text_entry()], &[], &clean),
        "wide slack: zero dead units pass the CHECK pass"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}
