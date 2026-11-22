// F8.rs — rust/F8: malformed, ragged tail (docs/FIXED-FORM-ALGORITHM.md:894)
//
// THE LAW:
//   "a ragged tail: rest mod record_bytes != 0 | — | malformed, and -1"
//
// A fixed-form file whose remaining bytes after the header and layout block are not
// an exact multiple of the record size is MALFORMED, the RESIDUE answer, never a refusal
// by name. The reader sets report.damaged = true, returns None, leaves report.reason
// untouched as TableFixedReason::None, and moves no counters.
//
// THE PRODUCTION PATH:
//   padded_row_fixed_save / padded_row_fixed_load in blockdemo (tables/block/Padded.schema).
//   PaddedRow has PADDED_ROW_FIXED_RECORD_BYTES (the hash and body).
//
// THE VECTOR:
//   Save one valid PaddedRow record to get a valid wire buffer.
//   For each extra byte length from 1 to record_bytes - 1:
//     Append `extra` bytes to the valid file.
//     Call padded_row_fixed_load on the ragged file.
//     Assert: returns None, report.damaged is true, report.refused is false,
//     report.reason is TableFixedReason::None, all counters zero, and the row
//     handed in (pre-poisoned off its defaults) holds its declared defaults
//     (ruling R4).
//
// THE NEGATIVE CONTROL:
//   extra == record_bytes is one WHOLE extra record, not a ragged tail.
//   With a destination slice of len 1, the reader owes batch_too_large (a refusal by name),
//   setting report.refused = true and leaving report.damaged = false.
//
// To run:
//   rustc --edition 2024 --test -L test/conformance/rust/target/debug/deps \
//     --extern blockdemo=$(ls test/conformance/rust/target/debug/deps/libblockdemo-*.rlib | head -n 1) \
//     test/conformance/rust/rows/F8.rs -o build/rows-rust-F8 && ./build/rows-rust-F8

use blockdemo::{
    padded_row_fixed_load, padded_row_fixed_measure, padded_row_fixed_save,
    padded_row_fixed_scatter, PaddedRowRow, TableFixedEntry, TableFixedReason,
    TableFixedReport, PADDED_ROW_FIXED_DEFAULTS, PADDED_ROW_FIXED_RECORD_BYTES,
};

/// The row at its declared defaults, as the reader's own reset scatters them.
fn declared_defaults() -> PaddedRowRow {
    let mut want = PaddedRowRow::default();
    padded_row_fixed_scatter(&PADDED_ROW_FIXED_DEFAULTS, &mut want, &mut TableFixedReport::default());
    want
}

/// Field by field, padding aside.
fn same_row(a: &PaddedRowRow, b: &PaddedRowRow) -> bool {
    a.tag == b.tag && a.value.to_bits() == b.value.to_bits() && a.flag == b.flag && a.id == b.id
        && a.label == b.label && a.label_length == b.label_length && a.slots == b.slots
        && a.teams == b.teams && a.counter == b.counter && a.counter_present == b.counter_present
}

#[test]
fn test_ragged_tail() {
    test_row_f8_ragged_tail();
}

fn test_row_f8_ragged_tail() {
    let mut row = PaddedRowRow::default();
    row.tag = 42;
    row.value = 3.14159;
    row.id = 12345;

    let measure = padded_row_fixed_measure(1);
    let mut clean_file = vec![0u8; measure];
    let written = padded_row_fixed_save(&[row], &mut clean_file)
        .expect("padded_row_fixed_save writes clean record");
    assert_eq!(written, measure, "written bytes must match measured size");

    // 1. CLEAN READ: verify valid file reads cleanly first
    {
        let mut values = [PaddedRowRow::default(); 1];
        let mut plan = [TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = TableFixedReport::default();
        let n = padded_row_fixed_load(&mut values, &clean_file, &mut plan, &mut remap, &mut report);
        assert_eq!(n, Some(1), "clean file must read 1 record");
        assert!(!report.damaged, "clean file must not be malformed");
        assert!(!report.refused, "clean file must not be refused");
        assert_eq!(report.reason, TableFixedReason::None);
        assert_eq!(values[0].tag, 42);
        assert_eq!(values[0].id, 12345);
    }

    // 2. EVERY RAGGED TAIL: 1..record_bytes-1 extra bytes
    let record_bytes = PADDED_ROW_FIXED_RECORD_BYTES;
    for extra in 1..record_bytes {
        let mut ragged = clean_file.clone();
        ragged.extend_from_slice(&vec![0x7A; extra]);

        let mut values = [PaddedRowRow::default(); 1];
        // Poison every field off its declared default (valid values only: a
        // bool takes true, never a raw 0x5A byte), so the reset is visible.
        values[0].tag = 0x5A;
        values[0].value = -5.5;
        values[0].flag = !declared_defaults().flag;
        values[0].id = 0x5A5A_5A5A;
        values[0].label = [0x5A; 16];
        values[0].label_length = 0x5A;
        values[0].slots = [0x5A5A; 4];
        values[0].teams = [0x5A; 4];
        values[0].counter = 0x5A5A;
        values[0].counter_present = !declared_defaults().counter_present;
        assert!(!same_row(&values[0], &declared_defaults()), "the poison is off the declared defaults");

        let mut plan = [TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = TableFixedReport::default();
        let n = padded_row_fixed_load(&mut values, &ragged, &mut plan, &mut remap, &mut report);

        assert!(n.is_none(), "ragged tail at +{extra} bytes must return None, got {n:?}");
        assert!(report.damaged, "ragged tail at +{extra} bytes must set malformed: true");
        assert!(!report.refused, "ragged tail at +{extra} bytes must not set refused");
        assert_eq!(
            report.reason,
            TableFixedReason::None,
            "ragged tail leaves reason untouched as None: {:?}", report.reason
        );
        assert_eq!(report.oor, 0, "ragged tail moves no clamped counter");
        assert_eq!(report.widened, 0, "ragged tail moves no widened counter");
        assert_eq!(report.unknown, 0, "ragged tail moves no unknown counter");
        assert_eq!(report.mismatch, 0, "ragged tail moves no kind_mismatch counter");

        // Ruling R4: the row handed in holds its declared defaults.
        assert!(
            same_row(&values[0], &declared_defaults()),
            "ragged tail at +{extra} bytes must leave the row at its declared defaults"
        );
    }

    // 3. NEGATIVE CONTROL: extra == record_bytes is a whole extra record, NOT ragged.
    // It owes batch_too_large (refusal by name) because capacity is 1, not malformed.
    {
        let mut whole = clean_file.clone();
        whole.extend_from_slice(&vec![0u8; record_bytes]);

        let mut values = [PaddedRowRow::default(); 1];
        let mut plan = [TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = TableFixedReport::default();
        let n = padded_row_fixed_load(&mut values, &whole, &mut plan, &mut remap, &mut report);

        assert!(n.is_none(), "whole extra record must not read past capacity");
        assert!(report.refused, "whole extra record must set refused: true");
        assert_eq!(
            report.reason,
            TableFixedReason::BatchTooLarge,
            "whole extra record owes batch_too_large, got {:?}", report.reason
        );
        assert!(!report.damaged, "whole extra record must NOT set malformed");
    }
}

fn main() {
    test_row_f8_ragged_tail();
    println!("PASS: cell rust/F8 ragged tail correctly rejected as malformed by fixed-table decoder");
}
