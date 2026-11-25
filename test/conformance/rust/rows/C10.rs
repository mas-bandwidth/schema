// C10 — an ENUM ORDINAL PAST THE TOP VARIANT REFUSES `out_of_range`.
//
// docs/FIXED-FORM-ALGORITHM.md §4.6, the bounds-pass row of the answer table:
//
//   | the bounds pass | `out_of_range` | ... an enum ordinal past the plan's
//   | top variant (a FORGED ordinal), on the COMPILED plan exactly as on the
//   | identity one |
//
// and the tag and ordinal ruling (§5.9 #14, 2026-11-03): an ordinal past the
// last variant REFUSES `out_of_range`, on the identity path as well as the
// compiled one. The whole read refuses, nothing lands, and every row handed
// in goes back to its declared default (SPEC-TABLES §6.5). The verdict is
// compared across legs; the name is diagnostic.
//
// Until 2026-11-25 this row asserted the retired land-None-and-count answer
// through a report field, `oor`, that the report no longer has.
//
// THE PRODUCTION PATH. The entry point is `fe_root_fixed_load`, the generated
// reader for FE1's `fixed table FeRoot` (test/tables/FE1.schema; 3 variants
// Bronze/Silver/Gold, and the ordinal is a POSITION FROM 1 with 0 = None).
// This test forges the ordinal byte (`grade` = 4, one past the writer's
// three) and reads the real generated codec — no helper is called that the
// produced codec does not call itself.
//
// It reaches the generated crate the way the driver's dependency graph does,
// one module per generated source file, skipping the WIRE module (`fe1.rs`)
// because this row is about the fixed form; the fixed modules have no
// dependency on the serialize runtime.
#![allow(unused_imports)]

#[path = "../../../../build/tables-generated-rust/tblfe1/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblfe1/src/fe1_records.rs"]
mod fe1_records;
#[path = "../../../../build/tables-generated-rust/tblfe1/src/fe1_fixed.rs"]
mod fe1_fixed;

pub use fe1_fixed::*;
pub use fe1_records::*;
pub use fixed_runtime::*;

// THE FORGED BYTE sits at the record's body[0]: the file is 16 header bytes,
// then the u32 block length, then the block, then each record is an 8-byte
// layout hash and the body. `grade` is FeRoot's first field, at body[0].
fn grade_byte_at() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FE_ROOT_FIXED_BLOCK.len() + 8
}

// A lawful record, written by the generated writer, then the ordinal byte
// OVERWRITTEN with `ordinal`. `Gold` is the writer's top variant (3), so
// `ordinal == 4` is one past the WRITER's own variant count.
fn forged(ordinal: u8) -> Vec<u8> {
    let mut effect = EffectRow::default();
    effect.set_boost(BoostRow { power: 17 });
    let row = FeRootRow {
        grade: 3, // Gold
        effect,
        tail: 55, // the field after the enum
    };
    let mut file = vec![0u8; fe_root_fixed_measure(1)];
    assert_eq!(
        fe_root_fixed_save(&[row], &mut file),
        Some(file.len()),
        "the fixture must write a whole file"
    );
    file[grade_byte_at()] = ordinal;
    file
}

// The row at its declared defaults, as the reader's own reset scatters them.
fn declared_defaults() -> FeRootRow {
    let mut want = FeRootRow::default();
    fe_root_fixed_scatter(&FE_ROOT_FIXED_DEFAULTS, &mut want, &mut TableFixedReport::default());
    want
}

// Loads into rows poisoned off their defaults, so a refusal that leaves a
// caller's row alone is seen.
fn load(data: &[u8]) -> (Option<usize>, [FeRootRow; 4], TableFixedReport) {
    let mut values = [FeRootRow::default(); 4];
    for v in values.iter_mut() {
        v.grade = 2;
        v.tail = 0x5A;
    }
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fe_root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values, report)
}

fn assert_refused(what: &str, ordinal: u8) {
    let (n, values, report) = load(&forged(ordinal));
    let want = declared_defaults();
    assert_eq!(n, None, "{what}: an ordinal past the top variant refuses the read: {report:?}");
    assert!(report.refused, "{what}: the read is refused, never landed as None: {report:?}");
    assert_eq!(report.reason, TableFixedReason::OutOfRange, "{what}: {report:?}");
    assert_eq!(report.unknown, 0, "{what}: a refusal moves no counter: {report:?}");
    assert_eq!(report.widened, 0, "{what}: a refusal moves no counter: {report:?}");
    for (i, v) in values.iter().enumerate() {
        assert!(
            v.grade == want.grade && v.tail == want.tail,
            "{what}: row {i} handed in is back at its declared defaults, nothing landed"
        );
    }
}

// THE POSITIVE CONTROL, IN THE SAME BINARY: a lawful ordinal lands the value
// and refuses nothing, so the refusal below is the forged byte and not a
// reader that refuses everything.
#[test]
fn c10_a_lawful_ordinal_lands() {
    println!("C10 positive control: Gold(3) lands 3");
    let (n, values, report) = load(&forged(3));
    assert_eq!(n, Some(1), "a lawful record reads one record: {report:?}");
    assert!(!report.refused, "a lawful ordinal refuses nothing: {report:?}");
    assert_eq!(values[0].grade, 3, "Gold lands Gold, not None");
    assert_eq!(values[0].tail, 55, "the field after the enum lands");
    assert_eq!(values[0].effect.boost().map(|b| b.power), Some(17), "the arm's payload lands");
}

// THE CELL: one past the top variant refuses the whole read.
#[test]
fn c10_ordinal_one_past_top_refuses() {
    println!("C10: ordinal 4 (one past Gold) refuses out_of_range");
    assert_refused("ordinal 4", 4);
}

// The extreme byte is the same law, not a second one.
#[test]
fn c10_ordinal_byte_max_refuses() {
    println!("C10: ordinal 255 refuses out_of_range");
    assert_refused("ordinal 255", 255);
}
