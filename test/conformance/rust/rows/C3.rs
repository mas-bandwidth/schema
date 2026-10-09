// cell rust/C3 — text length clamp
//
// The SPEC says:
//   text: unit := (meta == wide) ? 2 : 1; cap := size / unit;
//   v := SLE(4, record+src) clamped into [0, cap], COUNT clamped if it fired.
//
// PaddedRow has `label string(15)`, so cap = 15, unit = 1.
// The generated reader in padded_row_fixed_scatter reads i32 at offset 14,
// clamps to [0, 15], and counts clamped when it fires.
// This test proves the clamp works: forged bytes with label_length > 15
// land at 15 with clamped=1, and in-range bytes land verbatim.

use blockdemo::{
    PaddedRowRow, PADDED_ROW_FIXED_BODY_BYTES,
    padded_row_fixed_scatter, TableFixedReport,
};

fn test_label_length_clamped() {
    // body is 50 bytes of zeros (the declared default)
    let mut body = [0u8; PADDED_ROW_FIXED_BODY_BYTES];
    // label_length lives at offset 14 as a 4-byte signed LE i32 (padded_fixed.rs:97)
    // Set it to 20, past the cap of 15
    body[14..18].copy_from_slice(&(20i32).to_le_bytes());
    // also put some content so label[idx] is non-zero, though not required
    body[18] = b'A';

    let mut row = PaddedRowRow::default();
    let mut report = TableFixedReport::default();
    padded_row_fixed_scatter(&body, &mut row, &mut report);

    assert_eq!(row.label_length, 15, "text length must clamp to cap=15");
    assert_eq!(report.clamped, 1, "COUNT clamped must fire once");
}

fn test_label_length_in_range() {
    let mut body = [0u8; PADDED_ROW_FIXED_BODY_BYTES];
    // label_length = 5, inside [0, 15]
    body[14..18].copy_from_slice(&(5i32).to_le_bytes());
    body[18..23].copy_from_slice(b"Hello");

    let mut row = PaddedRowRow::default();
    let mut report = TableFixedReport::default();
    padded_row_fixed_scatter(&body, &mut row, &mut report);

    assert_eq!(row.label_length, 5, "in-range length must not change");
    assert_eq!(report.clamped, 0, "no clamp for in-range length");
}

fn test_label_length_negative() {
    // A NEGATIVE SLE(4) value clamps to 0 per [0, cap] (fix 16).
    let mut body = [0u8; PADDED_ROW_FIXED_BODY_BYTES];
    body[14..18].copy_from_slice(&(-1i32).to_le_bytes());

    let mut row = PaddedRowRow::default();
    let mut report = TableFixedReport::default();
    padded_row_fixed_scatter(&body, &mut row, &mut report);

    assert_eq!(row.label_length, 0, "negative length clamps to 0");
    assert!(report.clamped >= 1, "negative length fires clamp");
}

fn test_label_length_zero() {
    // Zero is the empty string, valid at the boundary.
    let mut body = [0u8; PADDED_ROW_FIXED_BODY_BYTES];
    body[14..18].copy_from_slice(&(0i32).to_le_bytes());

    let mut row = PaddedRowRow::default();
    let mut report = TableFixedReport::default();
    padded_row_fixed_scatter(&body, &mut row, &mut report);

    assert_eq!(row.label_length, 0, "length 0 must stay 0");
    assert_eq!(report.clamped, 0, "length 0 fires no clamp");
}

fn main() {
    test_label_length_clamped();
    test_label_length_in_range();
    test_label_length_negative();
    test_label_length_zero();
    println!("PASS text length clamp: all four cases");
}

#[cfg(test)]
mod tests {
    #[test]
    fn run() {
        super::main();
    }
}