// C4 — TEXT CONTENT REFUSES BY NAME (cell rust/C4, docs/roadmap.sexp:2871
// on this branch; docs/roadmap.sexp:3090 at main eb12ceb3).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:333 — the card's pointer :331 at main
// eb12ceb3; the §4.5 `text` row sits two lines lower on fixed-table-form):
//
//   | `text` | ... **The CONTENT RULES apply to the USED UNITS and nothing
//   | else**: UTF-8 validity over `v` bytes, never over `N`; wide code units
//   | over `v`, never `2N`, an astral pair counting two (fix 7). **A content
//   | violation REFUSES BY NAME, the verdict the packet reader gives**, this
//   | form having no `L` to continue past (fix 11) |
//
// A fixed-form reader that meets ill-formed content in a `string(N)`'s USED
// bytes must REFUSE the whole load — refused, under the refusal's own NAME
// (`text_ill_formed`, the name the roadmap cell states), nothing decoded, no
// counter moved, and `malformed` NOT fired. The verdict is the packet
// reader's because this form has no `L` to continue past: a record is one
// positional image, not a run of length-framed fields.
//
// THE PRODUCTION PATH. The entry point is `fx_root_fixed_load`, the generated
// reader for FX1's `fixed table FxRoot` (test/tables/FX1.schema; the text
// field is `label string(8) = "fx"`, its length word at body[22..26] and its
// 8-byte span at body[26..34]). This test writes a lawful record with the
// generated writer, overwrites the used bytes with ill-formed content, and
// reads the real generated codec — no helper is called that the produced
// codec does not call itself.
//
// It reaches the generated crate the way the driver's dependency graph does,
// one module per generated source file, skipping the WIRE module (`fx1.rs`)
// and the accelerators because this row is about the fixed form; the fixed
// modules have no dependency on the serialize runtime (as rust/C10's row
// states for the same reason), so this file compiles with bare rustc and no
// --extern/-L flags.
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

// The body byte of `label`'s span at index `k`: the file is the 16-byte
// header, the u32 block length, the block, then each record's 8-byte layout
// hash and body; the span starts at body[26] (the length word is body[22..26]).
fn label_byte_at(k: usize) -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FX_ROOT_FIXED_BLOCK.len() + 8 + 26 + k
}

// A lawful record, written by the generated writer, with `label` set to
// `content` — the span's declared bound is 8 — and fields BESIDE the text
// set, so a clean read is distinguishable from a refusal that decoded nothing.
fn record_with_label(content: &[u8]) -> Vec<u8> {
    let mut row = FxRootRow::default();
    row.label_length = content.len() as i32;
    row.label[..content.len()].copy_from_slice(content);
    row.marks_count = 1;
    row.marks[0] = 42;
    row.blob_length = 1;
    row.blob[0] = 7;
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

// THE POSITIVE CONTROL, IN THE SAME BINARY: lawful content reads clean and
// moves nothing, so the forgery below is the content and not a reader that
// refuses everything.
#[test]
fn c4_lawful_text_reads_clean() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("C4 positive control: lawful label reads clean");
    let (n, values, report) = load(&record_with_label(b"abc"));
    checkf!(n == Some(1), "lawful text: the record reads one record: {report:?}");
    checkf!(
        !report.refused && !report.malformed,
        "lawful text: no refusal, no damage: {report:?}"
    );
    checkf!(
        report.clamped == 0
            && report.unknown == 0
            && report.kind_mismatch == 0
            && report.widened == 0,
        "lawful text: no counter moved: {report:?}"
    );
    checkf!(
        values[0].label_length == 3 && &values[0].label[..3] == b"abc",
        "lawful text: the text lands as written"
    );
    checkf!(
        values[0].marks_count == 1 && values[0].marks[0] == 42 && values[0].blob[0] == 7,
        "lawful text: the fields beside the text land"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// THE CELL: ill-formed UTF-8 among the USED bytes refuses the load BY NAME.
#[test]
fn c4_ill_formed_used_bytes_refuse_by_name() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("C4: ill-formed UTF-8 in the used bytes refuses by name");
    let mut file = record_with_label(b"abc");
    file[label_byte_at(1)] = 0xFF; // "a\xFFc" — 0xFF cannot begin a UTF-8 sequence
    let (n, _, report) = load(&file);
    // THE LAW, clause by clause. The leg does not implement the refusal
    // (it defaults the field and counts `malformed`, the VARIABLE wire's
    // verdict), so the first check is the committed RED:
    checkf!(
        report.refused,
        "ill-formed used bytes: the load REFUSES (fix 11): {report:?}"
    );
    checkf!(
        report.reason.name() == "text_ill_formed",
        "ill-formed used bytes: the refusal is NAMED text_ill_formed, got {:?}",
        report.reason.name()
    );
    checkf!(
        n.is_none(),
        "ill-formed used bytes: nothing is decoded, the load returns none"
    );
    checkf!(
        !report.malformed,
        "ill-formed used bytes: a refusal is not `malformed`, the flag stays down: {report:?}"
    );
    checkf!(
        report.clamped == 0
            && report.unknown == 0
            && report.kind_mismatch == 0
            && report.widened == 0,
        "ill-formed used bytes: no counter moves on a refusal: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// THE ZERO RULE IS PART OF THE CONTENT RULE (fixed_runtime.rs, §3): a NUL is
// a legal code point and this wire does not carry one, so a zero byte among
// the USED bytes is the same content violation and refuses under the same name.
#[test]
fn c4_zero_byte_in_used_bytes_refuses_under_the_same_name() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("C4: a zero byte among the used bytes refuses under the same name");
    let mut file = record_with_label(b"abc");
    file[label_byte_at(1)] = 0x00; // "a\0c" — well-formed UTF-8, still not text
    let (n, _, report) = load(&file);
    checkf!(
        report.refused,
        "zero among used bytes: the load REFUSES: {report:?}"
    );
    checkf!(
        report.reason.name() == "text_ill_formed",
        "zero among used bytes: the refusal is NAMED text_ill_formed, got {:?}",
        report.reason.name()
    );
    checkf!(
        n.is_none() && !report.malformed && report.clamped == 0,
        "zero among used bytes: nothing decoded, no flag, no counter: {report:?}"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}

// THE FLIP SIDE, THE SAME ROW'S OWN WORDS: the content rules apply to the
// USED units and NOTHING ELSE — UTF-8 validity over `v` bytes, never over
// `N`. Ill-formed bytes in the SLACK (past the used length, inside the
// declared bound) are not judged, do not refuse and move no counter.
#[test]
fn c4_ill_formed_slack_is_not_judged() {
    FAILURES.store(0, Ordering::Relaxed);
    println!("C4: ill-formed bytes in the slack are not judged");
    let mut file = record_with_label(b"ab");
    for k in 2..8 {
        file[label_byte_at(k)] = 0xFF; // garbage in the slack, used length stays 2
    }
    let (n, values, report) = load(&file);
    checkf!(n == Some(1), "slack garbage: the record reads on: {report:?}");
    checkf!(
        !report.refused && !report.malformed,
        "slack garbage: no refusal, no damage: {report:?}"
    );
    checkf!(
        report.clamped == 0
            && report.unknown == 0
            && report.kind_mismatch == 0
            && report.widened == 0,
        "slack garbage: no counter moved: {report:?}"
    );
    checkf!(
        values[0].label_length == 2 && &values[0].label[..2] == b"ab",
        "slack garbage: the used text lands as written"
    );
    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}
