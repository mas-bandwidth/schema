// cell rust/C3 — text length BOUND (once the text length clamp)
//
// The SPEC says (docs/FIXED-FORM-ALGORITHM.md:449, §4.5's `text` row):
//   v := SLE(4, record+src); if v < 0 or v > size then REFUSE length_over_bound
//   — before any destination byte is written.
//
// THE LAW MOVED (docs/rulings.json strict-only-read; docs/plans/strict-only.md
// decisions 1-2): this row once asserted `v` clamped into [0, cap] with COUNT
// clamped == 1. No read clamps now: a length outside [0, cap] refuses the whole
// load. This leg spells the refusal `out_of_range` (TableFixedReason has no
// separate length_over_bound; the reason is per-language diagnostic detail,
// docs/rulings.json refusal-verdict-only) and leaves every row handed in at its
// declared default (ruling R4).
//
// THE PRODUCTION PATH: the row used to call padded_row_fixed_scatter alone and
// read the clamp counter; the scatter does not refuse (the LOAD does, on the
// count the scatter raises), so the row now goes through padded_row_fixed_load
// on a one-record file written by the generated padded_row_fixed_save, with
// the length word poked. PaddedRow has `label string(15)`: cap = 15, the
// length word at body[14..18], the payload at body[18..33].

use blockdemo::{
    padded_row_fixed_load, padded_row_fixed_measure, padded_row_fixed_save, PaddedRowRow,
    TableFixedEntry, TableFixedReason, TableFixedReport, PADDED_ROW_FIXED_BLOCK,
    TABLE_FIXED_HEADER_BYTES,
};

// record 0's body in a one-record file: header, u32 block length, block, hash
fn body_at() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + PADDED_ROW_FIXED_BLOCK.len() + 8
}

// a lawful one-record file with `label` = "Hello" and neighbours set, written
// by the unit's own writer, then the length word overwritten with `len`
fn file_with_length(len: i32) -> Vec<u8> {
    let mut v = PaddedRowRow::default();
    v.label[..5].copy_from_slice(b"Hello");
    v.label_length = 5;
    v.id = 777; // a neighbour that must not land on a refusal
    let mut file = vec![0u8; padded_row_fixed_measure(1)];
    assert_eq!(padded_row_fixed_save(&[v], &mut file), Some(file.len()), "the fixture writes a whole file");
    let at = body_at() + 14;
    file[at..at + 4].copy_from_slice(&len.to_le_bytes());
    file
}

fn load(data: &[u8]) -> (Option<usize>, PaddedRowRow, TableFixedReport) {
    let mut values = [PaddedRowRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 2048];
    let mut report = TableFixedReport::default();
    let n = padded_row_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values[0], report)
}

fn at_defaults(row: &PaddedRowRow) -> bool {
    let fresh = PaddedRowRow::default();
    row.label_length == fresh.label_length && row.id == fresh.id && row.label == fresh.label
}

fn test_label_length_past_cap_refuses() {
    // 20, past the cap of 15. The whole 15-byte span holds clean non-zero text,
    // so neither the dead-byte check nor the content rule can fire on it and
    // the length is the only fault.
    let mut file = file_with_length(20);
    let p = body_at() + 18;
    file[p..p + 15].copy_from_slice(b"ABCDEFGHIJKLMNO");
    let (n, row, report) = load(&file);
    // strict-only-read: was `label_length == 15` (clamped to cap) and `oor == 1`
    assert_eq!(n, None, "a text length past the cap refuses the whole load: {report:?}");
    assert!(report.refused && !report.damaged, "refused, not damage: {report:?}");
    assert_eq!(report.reason, TableFixedReason::OutOfRange, "the length bound refuses out_of_range: {report:?}");
    assert!(at_defaults(&row), "a refused load lands nothing (declared defaults)");
}

fn test_label_length_in_range() {
    let (n, row, report) = load(&file_with_length(5));
    assert_eq!(n, Some(1), "an in-range length reads: {report:?}");
    assert_eq!(row.label_length, 5, "in-range length must not change");
    assert_eq!(&row.label[..5], b"Hello");
    assert_eq!(row.id, 777, "the neighbour lands");
    // strict-only-read: was `oor == 0` (no clamp)
    assert!(!report.refused && !report.damaged, "no refusal for an in-range length: {report:?}");
}

fn test_label_length_negative_refuses() {
    // A NEGATIVE SLE(4) value is outside [0, cap] (fix 16). Used length 0 makes
    // the payload dead, so the payload is cleared first: the length is the only
    // fault the load can see.
    let mut file = file_with_length(-1);
    let p = body_at() + 18;
    file[p..p + 5].fill(0);
    let (n, row, report) = load(&file);
    // strict-only-read: was `label_length == 0` (clamped) and `oor >= 1`
    assert_eq!(n, None, "a negative length refuses the whole load: {report:?}");
    assert!(report.refused && !report.damaged, "refused, not damage: {report:?}");
    assert_eq!(report.reason, TableFixedReason::OutOfRange, "the length bound refuses out_of_range: {report:?}");
    assert!(at_defaults(&row), "a refused load lands nothing (declared defaults)");
}

fn test_label_length_zero() {
    // Zero is the empty string, valid at the boundary.
    let mut file = file_with_length(0);
    let p = body_at() + 18;
    file[p..p + 5].fill(0);
    let (n, row, report) = load(&file);
    assert_eq!(n, Some(1), "length 0 reads: {report:?}");
    assert_eq!(row.label_length, 0, "length 0 must stay 0");
    // strict-only-read: was `oor == 0`
    assert!(!report.refused && !report.damaged, "length 0 is not refused: {report:?}");
}

fn test_label_length_at_cap() {
    // the cap itself is IN the bound: 15 used bytes, no dead byte
    let mut file = file_with_length(15);
    let p = body_at() + 18;
    file[p..p + 15].copy_from_slice(b"ABCDEFGHIJKLMNO");
    let (n, row, report) = load(&file);
    assert_eq!(n, Some(1), "a length at the cap reads: {report:?}");
    assert_eq!(row.label_length, 15);
    assert!(!report.refused && !report.damaged, "the cap is in the bound: {report:?}");
}

fn main() {
    test_label_length_past_cap_refuses();
    test_label_length_in_range();
    test_label_length_negative_refuses();
    test_label_length_zero();
    test_label_length_at_cap();
    println!("PASS text length bound: refuses outside [0, cap], never clamps");
}

#[cfg(test)]
mod tests {
    #[test]
    fn run() {
        super::main();
    }
}
