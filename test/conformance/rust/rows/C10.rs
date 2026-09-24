// C10 — an ENUM ORDINAL PAST THE TOP VARIANT LANDS `None`.
//
// docs/FIXED-FORM-ALGORITHM.md:945 (the SPEC paragraph this row is cut from):
//
//   | the bounds pass | `clamped` | once per field: ... an enum ordinal past
//   | the top variant, and a FORGED ordinal remapped to `None` — one past the
//   | WRITER's own variant count, which the `ordinal` op lands as `0` and this
//   | pass counts, on the COMPILED plan exactly as on the identity one |
//
// and :946 — a port that ALSO counts in the `ordinal` op counts twice, so the
// count is asserted EXACTLY `== 1`.
//
// THE PRODUCTION PATH. The entry point is `fe_root_fixed_load`, the generated
// reader for FE1's `fixed table FeRoot` (test/tables/FE1.schema; 3 variants
// Bronze/Silver/Gold, and the ordinal is a POSITION FROM 1 with 0 = None).
// `fe_root_fixed_load` runs the identity plan and lands each field through
// `fe_root_fixed_scatter`, whose bounds pass is the `if raw > 3 { clamped += 1;
// grade = 0 }` arm over the ordinal byte. This test forges that byte exactly
// the way the fixed-form versioning row does (`r0.tier` = 4, one past the
// writer's three) and reads the real generated codec — no helper is called
// that the produced codec does not call itself.
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
        tail: 55, // the field after the enum, to prove the read goes on
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

fn load(data: &[u8]) -> (Option<usize>, [FeRootRow; 4], TableFixedReport) {
    let mut values = [FeRootRow::default(); 4];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fe_root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values, report)
}

// THE POSITIVE CONTROL, IN THE SAME BINARY: a lawful ordinal lands the value
// and moves no counter, so the forgery below is the byte and not a reader that
// answers `None` to everything.
#[test]
fn c10_a_lawful_ordinal_lands_and_counts_nothing() {
    println!("C10 positive control: Gold(3) lands 3 and clamped == 0");
    let (n, values, report) = load(&forged(3));
    assert_eq!(n, Some(1), "a lawful record reads one record: {report:?}");
    assert_eq!(values[0].grade, 3, "Gold lands Gold, not None");
    assert_eq!(values[0].tail, 55, "the field after the enum lands");
    assert_eq!(report.clamped, 0, "a lawful ordinal clamps nothing: {report:?}");
}

// THE CELL: one past the top variant remaps to `None` and counts EXACTLY one.
#[test]
fn c10_ordinal_one_past_top_lands_none() {
    println!("C10: ordinal 4 (one past Gold) lands None, clamped == 1");
    let (n, values, report) = load(&forged(4));
    assert_eq!(n, Some(1), "the record still reads: {report:?}");
    assert_eq!(values[0].grade, 0, "an ordinal past the top variant lands None");
    assert_eq!(report.clamped, 1, "and counts ONE clamped, never twice: {report:?}");
}

// The extreme byte is the same law, not a second one.
#[test]
fn c10_ordinal_byte_max_lands_none() {
    println!("C10: ordinal 255 lands None, clamped == 1");
    let (n, values, report) = load(&forged(255));
    assert_eq!(n, Some(1), "the record still reads: {report:?}");
    assert_eq!(values[0].grade, 0, "255 is past the top variant and lands None");
    assert_eq!(report.clamped, 1, "and counts ONE clamped: {report:?}");
}

// An out-of-range ordinal is NOT damage and NOT a refusal (§4.2): the read
// returns and every other counter stays at zero.
#[test]
fn c10_forged_ordinal_is_not_damage() {
    println!("C10: a forged ordinal is neither malformed nor refused");
    let (n, _, report) = load(&forged(4));
    assert_eq!(n, Some(1), "the read returns: {report:?}");
    assert!(!report.malformed, "an out-of-range ordinal is not damage: {report:?}");
    assert!(!report.refused, "an out-of-range ordinal is not a refusal: {report:?}");
    assert_eq!(report.reason, TableFixedReason::None, "no refusal name: {report:?}");
    assert_eq!(report.unknown, 0, "unknown stays zero: {report:?}");
    assert_eq!(report.kind_mismatch, 0, "kind_mismatch stays zero: {report:?}");
    assert_eq!(report.widened, 0, "widened stays zero: {report:?}");
}

// The fields BESIDE the forged ordinal still land, which says the bounds pass
// closed the one field and did not stop the record.
#[test]
fn c10_fields_beside_the_forged_ordinal_still_land() {
    println!("C10: the fields beside the forged ordinal still land");
    let (n, values, report) = load(&forged(4));
    assert_eq!(n, Some(1), "the record still reads: {report:?}");
    assert_eq!(values[0].tail, 55, "the scalar after the enum is untouched");
    assert_eq!(values[0].effect.tag(), 1, "the union beside the enum still points at boost");
    assert_eq!(
        values[0].effect.boost().map(|b| b.power),
        Some(17),
        "and the arm's payload landed"
    );
}