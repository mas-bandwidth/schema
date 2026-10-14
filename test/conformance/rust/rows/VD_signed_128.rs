// VD_signed_128 — Signed 128-bit integers: valid-data write/read acceptance.
//
// docs/roadmap.sexp:4814, contract: §3.4 record, int128
//
// THE LAW (docs/SPEC-TABLES.md:3735, §3.4):
//   `18` i128 | 16 bytes: the low 64-bit half, then the high half, two's
//   complement for `18`, little-endian throughout
//
// And the clamping contract (docs/SPEC-TABLES.md:7294):
//   A 128-bit integer clamps to its declared bounds in 128 bits
//
// And the fixed-form payload (docs/FIXED-FORM-ALGORITHM.md:172):
//   int128 / uint128 | 16 | the low 64-bit half, then the high
//
// THE PRODUCTION PATH this test drives, end to end, every stage generated:
//   fixed_table_fixed_save     write of FixedTable (Bench.schema, FixedTable.schema)
//   fixed_table_fixed_load     read through the IDENTITY plan
//   bench_mixed_fixed_write_body / scatter
//                             the per-field stores for i128 at body offset 1205
//   bench_mixed_fixed_clamp_body
//                             the bounds pass over flux: [-1267650600228229401496703205376,
//                             1267650600228229401496703205376]
//
// THE VECTOR is built here from the law: the int128 field `flux` lives at body
// offset 1205, is 16 bytes wide, and the writer writes the value as the low
// 64-bit half first then the high half (little-endian, two's complement).
//
// THE NEGATIVE CONTROL is the wrong wire: a byte flip in the 16-byte flux slot
// that pushes the value outside the declared range. The clamp pass must count
// it; restoring the bytes must make the test green again.
//
// HOW THIS FILE REACHES THE GENERATED CODE: the same way E6.rs and C10.rs do
// — by path, with no runtime, the fixed form's modules using none of serialize.
// The benchfixed unit carries BenchMixedRow with flux: TableI128 at the
// declared range (Bench.schema, FixedTable.schema).
//
// Standalone: depends on no other rows/ file, edits no shared file.
// Run from the repository root:
//   rustc --edition 2021 --test test/conformance/rust/rows/VD_signed_128.rs \
//       -o build/rows-rust-VD_signed_128 && ./build/rows-rust-VD_signed_128
//
// Exit 0 green, 1 red; one printed line per assertion.

#![allow(dead_code)]

#[path = "../../../../build/tables-generated-rust/benchfixed/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/benchfixed/src/bench_records.rs"]
mod bench_records;
#[path = "../../../../build/tables-generated-rust/benchfixed/src/bench_fixed.rs"]
mod bench_fixed;
#[path = "../../../../build/tables-generated-rust/benchfixed/src/fixedtable_records.rs"]
mod fixedtable_records;
#[path = "../../../../build/tables-generated-rust/benchfixed/src/fixedtable_fixed.rs"]
mod fixedtable_fixed;

pub use fixed_runtime::*;
pub use bench_records::*;
pub use bench_fixed::*;
pub use fixedtable_records::*;
pub use fixedtable_fixed::*;

static FAILURES: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);

fn check(ok: bool, what: &str) {
    if ok {
        println!("ok - {what}");
    } else {
        let n = FAILURES.fetch_add(1, std::sync::atomic::Ordering::Relaxed) + 1;
        println!("FAIL #{n} - {what}");
    }
}

// THE FLUX FIELD sits at body offset 1205, is 16 bytes wide.
// The range is [-1267650600228229401496703205376, 1267650600228229401496703205376].
const FLUX_BODY_OFFSET: usize = 1205;
const FLUX_WIDTH: usize = 16;
const FLUX_MIN: i128 = -1267650600228229401496703205376;
const FLUX_MAX: i128 = 1267650600228229401496703205376;

fn file_body_offset() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FIXED_TABLE_FIXED_BLOCK.len() + 8
}

// ---- the write-read round trip, exercised below ----

fn round_trip(value: &BenchMixedRow) -> (FixedTableRow, TableFixedReport) {
    let measure = fixed_table_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let row = FixedTableRow { value: *value };
    fixed_table_fixed_save(&[row], &mut file).expect("the writer saves the record");

    let mut values = [FixedTableRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fixed_table_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the file");
    assert_eq!(n, 1, "the reader reads one record");
    (values[0], report)
}

// ---- the law: a positive i128 round-trips ----

#[test]
fn vd_signed_128_positive_value_round_trips() {
    println!("VD_signed_128: a positive i128 value round-trips through the fixed form");
    let mut row = BenchMixedRow::default();
    row.flux = TableI128(42);
    let (back, report) = round_trip(&row);
    check(back.value.flux.0 == 42, "VD_signed_128: positive flux round-trips as 42");
    check(report.clamped == 0, "VD_signed_128: no clamping for a value in range");
}

// ---- the law: a negative i128 round-trips ----

#[test]
fn vd_signed_128_negative_value_round_trips() {
    println!("VD_signed_128: a negative i128 value round-trips through the fixed form");
    let mut row = BenchMixedRow::default();
    row.flux = TableI128(-999999999);
    let (back, report) = round_trip(&row);
    check(back.value.flux.0 == -999999999, "VD_signed_128: negative flux round-trips");
    check(report.clamped == 0, "VD_signed_128: no clamping for a value in range");
}

// ---- the law: a value that exercises both halves (nonzero high 64 bits) ----

#[test]
fn vd_signed_128_wide_value_round_trips() {
    println!("VD_signed_128: a value with nonzero high 64 bits round-trips (both halves)");
    let mut row = BenchMixedRow::default();
    // (1i128 << 99) + 7 — exercises both the low and high 64-bit halves
    let expected: i128 = (1i128 << 99) + 7;
    row.flux = TableI128(expected);
    let (back, report) = round_trip(&row);
    check(back.value.flux.0 == expected, "VD_signed_128: wide flux round-trips with both halves nonzero");
    check(report.clamped == 0, "VD_signed_128: no clamping for a value in range");
}

// ---- the law: the byte encoding is low-half-first (little-endian) ----

#[test]
fn vd_signed_128_byte_order_is_little_endian() {
    println!("VD_signed_128: the 16-byte encoding is low 64-bit half first, little-endian");
    // A value that exercises both halves, within the declared range.
    // 0x00000000000003E8_000000000000002A = (1000 << 64) | 42
    let expected: i128 = (1000i128 << 64) | 42;
    let measure = fixed_table_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let row = FixedTableRow { value: BenchMixedRow { flux: TableI128(expected), ..BenchMixedRow::default() } };
    fixed_table_fixed_save(&[row], &mut file).expect("the writer saves the record");
    let body = file_body_offset();
    let slot = &file[body + FLUX_BODY_OFFSET..body + FLUX_BODY_OFFSET + FLUX_WIDTH];
    // low 64-bit half first (little-endian): 42 LE = [42, 0, 0, 0, 0, 0, 0, 0]
    let lo = expected as u64;
    let hi = (expected >> 64) as u64;
    let lo_bytes = lo.to_le_bytes();
    let hi_bytes = hi.to_le_bytes();
    check(slot[..8] == lo_bytes, "VD_signed_128: low 64-bit half is first (LE)");
    check(slot[8..] == hi_bytes, "VD_signed_128: high 64-bit half is second (LE)");
}

// ---- the law: maximum in-range value round-trips ----

#[test]
fn vd_signed_128_max_value_round_trips() {
    println!("VD_signed_128: the maximum declared value round-trips without clamping");
    let mut row = BenchMixedRow::default();
    row.flux = TableI128(FLUX_MAX);
    let (back, report) = round_trip(&row);
    check(back.value.flux.0 == FLUX_MAX, "VD_signed_128: max flux round-trips");
    check(report.clamped == 0, "VD_signed_128: max value is in range, no clamping");
}

// ---- the law: minimum in-range value round-trips ----

#[test]
fn vd_signed_128_min_value_round_trips() {
    println!("VD_signed_128: the minimum declared value round-trips without clamping");
    let mut row = BenchMixedRow::default();
    row.flux = TableI128(FLUX_MIN);
    let (back, report) = round_trip(&row);
    check(back.value.flux.0 == FLUX_MIN, "VD_signed_128: min flux round-trips");
    check(report.clamped == 0, "VD_signed_128: min value is in range, no clamping");
}

// ---- the law: zero round-trips (default value) ----

#[test]
fn vd_signed_128_zero_round_trips() {
    println!("VD_signed_128: the zero default round-trips");
    let row = BenchMixedRow::default();
    let (back, report) = round_trip(&row);
    check(back.value.flux.0 == 0, "VD_signed_128: zero flux round-trips");
    check(report.clamped == 0, "VD_signed_128: zero is in range, no clamping");
}

// ---- the law: an out-of-range value is clamped on read ----
//
// The writer's debug_assert rejects out-of-range values, so these tests
// construct the wire by writing a zero record and patching the flux slot.

#[test]
fn vd_signed_128_out_of_range_is_clamped() {
    println!("VD_signed_128: a value above the max is clamped on read");
    let measure = fixed_table_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let row = FixedTableRow { value: BenchMixedRow::default() };
    fixed_table_fixed_save(&[row], &mut file).expect("the writer saves the record");
    let body = file_body_offset();
    let over_max: i128 = FLUX_MAX + 1;
    file[body + FLUX_BODY_OFFSET..body + FLUX_BODY_OFFSET + FLUX_WIDTH]
        .copy_from_slice(&over_max.to_le_bytes());

    let mut values = [FixedTableRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fixed_table_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the file");
    check(n == 1, "VD_signed_128 OVER-MAX: the reader reads one record");
    check(values[0].value.flux.0 == FLUX_MAX, "VD_signed_128 OVER-MAX: over-range flux is clamped to max");
    check(report.clamped >= 1, "VD_signed_128 OVER-MAX: the clamp counter is incremented");
}

#[test]
fn vd_signed_128_below_min_is_clamped() {
    println!("VD_signed_128: a value below the min is clamped on read");
    let measure = fixed_table_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let row = FixedTableRow { value: BenchMixedRow::default() };
    fixed_table_fixed_save(&[row], &mut file).expect("the writer saves the record");
    let body = file_body_offset();
    let below_min: i128 = FLUX_MIN - 1;
    file[body + FLUX_BODY_OFFSET..body + FLUX_BODY_OFFSET + FLUX_WIDTH]
        .copy_from_slice(&below_min.to_le_bytes());

    let mut values = [FixedTableRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fixed_table_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the file");
    check(n == 1, "VD_signed_128 BELOW-MIN: the reader reads one record");
    check(values[0].value.flux.0 == FLUX_MIN, "VD_signed_128 BELOW-MIN: below-min flux is clamped to min");
    check(report.clamped >= 1, "VD_signed_128 BELOW-MIN: the clamp counter is incremented");
}

// ---- the negative control: a byte flip in the flux slot ----
//
// This is the file-level control: break one byte in the flux slot (push the
// value outside the declared range), run the test, it must go RED; restore and
// it must go GREEN. The control runs inside the test binary: we write a lawful
// file, flip one byte in the flux slot, and the scatter must read a value that
// the clamp pass rejects.
//
// FLUX_MAX - 100 has high word 0x00000000FFFFFFFF, low word 0xFFFFFFFFFFFFFF9C.
// Flipping byte 15 (MSB of the high word, 0x00) with XOR 0xFF makes the high
// word 0xFF000000FFFFFFFF — a negative value below FLUX_MIN — so the clamp
// brings it to FLUX_MIN, not FLUX_MAX.

#[test]
fn vd_signed_128_negative_control_byte_flip_is_clamped() {
    println!("VD_signed_128 NEGATIVE CONTROL: a byte flip in the flux slot is caught by the clamp");
    let mut row = BenchMixedRow::default();
    row.flux = TableI128(FLUX_MAX - 100); // well within range
    let measure = fixed_table_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let row_out = FixedTableRow { value: row };
    fixed_table_fixed_save(&[row_out], &mut file).expect("the writer saves the record");
    let body = file_body_offset();

    // Flip the highest byte of the flux slot (byte 15), the MSB of the high
    // word. The original high word is 0x00000000FFFFFFFF; XOR 0xFF makes it
    // 0xFF000000FFFFFFFF, which is negative in i128 — below FLUX_MIN.
    file[body + FLUX_BODY_OFFSET + FLUX_WIDTH - 1] ^= 0xFF;

    let mut values = [FixedTableRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fixed_table_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the file");
    check(n == 1, "VD_signed_128 NEGATIVE CONTROL: the reader reads one record");
    // The byte flip pushed the value below FLUX_MIN; the clamp must fire.
    check(report.clamped >= 1, "VD_signed_128 NEGATIVE CONTROL: byte-flipped flux is clamped");
    // The clamped value must equal FLUX_MIN (the flip makes it very negative).
    check(values[0].value.flux.0 == FLUX_MIN, "VD_signed_128 NEGATIVE CONTROL: clamped value is FLUX_MIN");

    // ---- RESTORE: same write without the flip must be GREEN ----
    let mut file_ok = vec![0u8; measure];
    fixed_table_fixed_save(&[row_out], &mut file_ok).expect("the writer saves the record again");
    let mut values = [FixedTableRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fixed_table_fixed_load(&mut values, &file_ok, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the restored file");
    check(n == 1, "VD_signed_128 RESTORE: the reader reads one record");
    check(values[0].value.flux.0 == FLUX_MAX - 100, "VD_signed_128 RESTORE: the restored flux round-trips");
    check(report.clamped == 0, "VD_signed_128 RESTORE: no clamping on the restored file");
}

// ---- the negative control (in-binary): a crafted wire that pushes flux above range ----
//
// This is the same control the card asks for: break one constant in the
// generated code (here, we simulate it by constructing a wire that the
// reader's clamp pass must reject). The test binary carries both the
// RED and the GREEN; the card's step 4b runs this test, then edits the
// generated constant, then runs it again to prove it bites.

#[test]
fn vd_signed_128_negative_control_crafted_wire() {
    println!("VD_signed_128 NEGATIVE CONTROL: a crafted wire with out-of-range flux");
    // Write a lawful file, then corrupt the flux slot to carry FLUX_MAX + 1.
    let mut row = BenchMixedRow::default();
    row.flux = TableI128(0); // zero is in range; the corruption is the control
    let measure = fixed_table_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let row_out = FixedTableRow { value: row };
    fixed_table_fixed_save(&[row_out], &mut file).expect("the writer saves the record");
    let body = file_body_offset();

    // Overwrite the flux slot with FLUX_MAX + 1 in little-endian.
    let over_max: i128 = FLUX_MAX + 1;
    file[body + FLUX_BODY_OFFSET..body + FLUX_BODY_OFFSET + FLUX_WIDTH]
        .copy_from_slice(&over_max.to_le_bytes());

    let mut values = [FixedTableRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fixed_table_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the crafted file");
    check(n == 1, "VD_signed_128 CRAFTED: the reader reads one record");
    check(report.clamped >= 1, "VD_signed_128 CRAFTED: the out-of-range flux is clamped");
    check(values[0].value.flux.0 == FLUX_MAX, "VD_signed_128 CRAFTED: the clamped value is FLUX_MAX");
}

#[test]
fn vd_signed_128_negative_control_below_min() {
    println!("VD_signed_128 NEGATIVE CONTROL: a crafted wire with flux below min");
    let mut row = BenchMixedRow::default();
    row.flux = TableI128(0);
    let measure = fixed_table_fixed_measure(1);
    let mut file = vec![0u8; measure];
    let row_out = FixedTableRow { value: row };
    fixed_table_fixed_save(&[row_out], &mut file).expect("the writer saves the record");
    let body = file_body_offset();

    // Overwrite the flux slot with FLUX_MIN - 1 in little-endian.
    let below_min: i128 = FLUX_MIN - 1;
    file[body + FLUX_BODY_OFFSET..body + FLUX_BODY_OFFSET + FLUX_WIDTH]
        .copy_from_slice(&below_min.to_le_bytes());

    let mut values = [FixedTableRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fixed_table_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report)
        .expect("the reader opens the crafted file");
    check(n == 1, "VD_signed_128 BELOW-MIN: the reader reads one record");
    check(report.clamped >= 1, "VD_signed_128 BELOW-MIN: the below-min flux is clamped");
    check(values[0].value.flux.0 == FLUX_MIN, "VD_signed_128 BELOW-MIN: the clamped value is FLUX_MIN");
}

// ---- the summary ----

#[test]
fn vd_signed_128_summary() {
    let fails = FAILURES.load(std::sync::atomic::Ordering::Relaxed);
    assert_eq!(fails, 0, "VD_signed_128: {fails} assertion(s) failed");
}
