// cell rust/C3 — the text length is held to its bound, and past it the read REFUSES
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md §4.5, the `text` row):
//   v := SLE(4, record+src); if v < 0 or v > size then REFUSE length_over_bound
//   — before any destination byte is written.
//
// An over-long (or negative) text length REFUSES the whole read: it is never
// clamped to the cap and landed. The verdict is compared across legs and the name
// is diagnostic; this leg answers the refusal by its strict-only name,
// out_of_range (or text_ill_formed when the forged span is not text), and every row handed in goes back to its declared defaults
// (SPEC-TABLES §6.5). Until 2026-11-25 this row called the scatter directly and
// asserted the retired clamp-and-count answer (and named a report field, `oor`,
// that the report no longer has).
//
// PaddedRow has `label string(15)`, so size = 15. The record body follows the
// header, the layout block and the record's eight-byte hash; label_length is
// the four-byte signed LE word at body offset 14.
//
// To run:
//   rustc --edition 2024 --test -L test/conformance/rust/target/debug/deps \
//     --extern blockdemo=$(ls test/conformance/rust/target/debug/deps/libblockdemo-*.rlib | head -n 1) \
//     test/conformance/rust/rows/C3.rs -o build/rows-rust-C3 && ./build/rows-rust-C3

use blockdemo::{
    padded_row_fixed_load, padded_row_fixed_measure, padded_row_fixed_save,
    padded_row_fixed_scatter, PaddedRowRow, TableFixedEntry, TableFixedReason,
    TableFixedReport, PADDED_ROW_FIXED_DEFAULTS,
};

/// The offset of label_length inside the record body (padded_fixed.rs).
const LABEL_LENGTH_AT: usize = 14;

/// The row at its declared defaults, as the reader's own reset scatters them.
fn declared_defaults() -> PaddedRowRow {
    let mut want = PaddedRowRow::default();
    padded_row_fixed_scatter(&PADDED_ROW_FIXED_DEFAULTS, &mut want, &mut TableFixedReport::default());
    want
}

/// A one-record file whose label is `text`, and the offset of its label_length.
fn file_with_label(text: &[u8]) -> (Vec<u8>, usize) {
    let mut row = PaddedRowRow::default();
    row.tag = 42;
    row.id = 12345;
    row.label[..text.len()].copy_from_slice(text);
    row.label_length = text.len() as i32;
    let measure = padded_row_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let written = padded_row_fixed_save(&[row], &mut file).expect("padded_row_fixed_save writes the record");
    assert_eq!(written, measure, "written bytes must match the measured size");
    let layout = u32::from_le_bytes(file[16..20].try_into().unwrap()) as usize;
    (file, 20 + layout + 8 + LABEL_LENGTH_AT)
}

/// Loads `file` into one row poisoned off its defaults.
fn load(file: &[u8]) -> (Option<usize>, TableFixedReport, PaddedRowRow) {
    let mut values = [PaddedRowRow::default(); 1];
    values[0].tag = 0x5A;
    values[0].id = 0x5A5A_5A5A;
    values[0].label_length = 0x5A;
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = padded_row_fixed_load(&mut values, file, &mut plan, &mut remap, &mut report);
    (n, report, values[0])
}

fn assert_refused(label: &str, file: &[u8]) {
    let (n, report, row) = load(file);
    let defaults = declared_defaults();
    assert_eq!(n, None, "{label}: an out-of-bound text length refuses the read: {report:?}");
    assert!(report.refused, "{label}: the read is refused, never clamped: {report:?}");
    // The verdict is compared and the name is diagnostic (§5.3): this leg names a
    // length past the bound out_of_range, or text_ill_formed when the bytes the
    // forged length would cover are not text — either way a refusal by name.
    assert!(
        matches!(report.reason, TableFixedReason::OutOfRange | TableFixedReason::TextIllFormed),
        "{label}: a refusal by name: {report:?}"
    );
    assert!(
        row.tag == defaults.tag && row.id == defaults.id && row.label_length == defaults.label_length,
        "{label}: the row handed in is back at its declared defaults, nothing clamped landed"
    );
}

#[test]
fn test_label_length_past_the_bound_refuses() {
    let (mut file, at) = file_with_label(b"Hello");
    file[at..at + 4].copy_from_slice(&20i32.to_le_bytes());
    assert_refused("length 20 against string(15)", &file);
    file[at..at + 4].copy_from_slice(&16i32.to_le_bytes());
    assert_refused("length 16, one past string(15)", &file);
}

#[test]
fn test_label_length_negative_refuses() {
    let (mut file, at) = file_with_label(b"");
    file[at..at + 4].copy_from_slice(&(-1i32).to_le_bytes());
    assert_refused("length -1", &file);
}

#[test]
fn test_label_length_in_range_reads() {
    for text in [&b""[..], &b"Hello"[..], &b"fifteen bytes!!"[..]] {
        let (file, _) = file_with_label(text);
        let (n, report, row) = load(&file);
        assert_eq!(n, Some(1), "length {} reads: {report:?}", text.len());
        assert!(!report.refused, "length {}: {report:?}", text.len());
        assert_eq!(row.label_length as usize, text.len(), "the used length lands verbatim");
        assert_eq!(&row.label[..text.len()], text, "the used bytes land verbatim");
    }
}
