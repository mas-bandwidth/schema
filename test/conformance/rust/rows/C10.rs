// C10 — an ENUM ORDINAL PAST THE TOP VARIANT REFUSES THE LOAD out_of_range.
//
// THE LAW MOVED (docs/rulings.json strict-only-read, 2026-11-03, superseding
// "the fixed form's bounds-pass clamp and forged-ordinal-to-None"): no read
// clamps, and a forged ordinal refuses the WHOLE load out_of_range (the
// retired `clamped`, renamed in place: docs/plans/strict-only.md decision 1).
// docs/FIXED-FORM-ALGORITHM.md §4.5's `ordinal` row: the op writes nothing
// and sets the forged flag; the bounds pass refuses on it. The quote below is
// the row's ORIGINAL law, kept for the mapping of each old assertion:
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

// A refused load leaves every row handed in at its DECLARED default (ruling
// R4). The declared defaults are the schema's own (test/tables/FE1.schema:
// `grade Grade = Silver`, `effect Effect` with no arm, `tail int32 = 7`),
// written here as the oracle — FeRootRow::default() is the cooked zero form,
// not the declaration.
const DECLARED_GRADE: u8 = 2; // Silver, a position from 1
const DECLARED_TAIL: i32 = 7;
fn at_defaults(v: &FeRootRow) -> bool {
    v.grade == DECLARED_GRADE && v.tail == DECLARED_TAIL && v.effect.tag() == 0
}

// THE POSITIVE CONTROL, IN THE SAME BINARY: a lawful ordinal lands the value
// and moves no counter, so the forgery below is the byte and not a reader that
// refuses everything.
#[test]
fn c10_a_lawful_ordinal_lands_and_counts_nothing() {
    println!("C10 positive control: Gold(3) lands 3, not refused");
    let (n, values, report) = load(&forged(3));
    assert_eq!(n, Some(1), "a lawful record reads one record: {report:?}");
    assert_eq!(values[0].grade, 3, "Gold lands Gold, not None");
    assert_eq!(values[0].tail, 55, "the field after the enum lands");
    // strict-only-read: was `oor == 0` (clamps nothing); no read clamps, so a
    // lawful ordinal is a load that neither refuses nor damages.
    assert!(!report.refused && !report.damaged, "a lawful ordinal is not refused: {report:?}");
    assert_eq!(report.reason, TableFixedReason::None, "no out_of_range: {report:?}");
}

// THE CELL: one past the WRITER's top variant is a FORGED ordinal. It once
// remapped to `None` and counted ONE clamped; strict-only-read (docs/rulings.json,
// superseding "the fixed form's bounds-pass clamp and forged-ordinal-to-None")
// makes it refuse the whole load out_of_range (docs/FIXED-FORM-ALGORITHM.md §4.5
// `ordinal` row: the op sets the FORGED FLAG and the bounds pass refuses on it).
// An ordinal past the WRITER's count is not an unknown variant from a newer
// writer (decision 4 keeps that one as evolution): the writer here is this build.
#[test]
fn c10_ordinal_one_past_top_refuses_out_of_range() {
    println!("C10: ordinal 4 (one past Gold) refuses the whole load out_of_range");
    let (n, values, report) = load(&forged(4));
    assert_eq!(n, None, "a forged ordinal refuses the whole load: {report:?}");
    assert!(report.refused, "refused is set: {report:?}");
    assert_eq!(report.reason, TableFixedReason::OutOfRange, "the reason is out_of_range: {report:?}");
    // was: `values[0].grade == 0` (landed None) — a refusal lands nothing, the
    // row stays at its declared default (Silver), never the forged ordinal.
    assert!(at_defaults(&values[0]), "a refusal lands nothing: grade={} tail={}", values[0].grade, values[0].tail);
}

// The extreme byte is the same law, not a second one.
#[test]
fn c10_ordinal_byte_max_refuses_out_of_range() {
    println!("C10: ordinal 255 refuses out_of_range");
    let (n, values, report) = load(&forged(255));
    assert_eq!(n, None, "255 is past the top variant and refuses: {report:?}");
    assert!(report.refused, "refused is set: {report:?}");
    assert_eq!(report.reason, TableFixedReason::OutOfRange, "the reason is out_of_range: {report:?}");
    assert!(at_defaults(&values[0]), "a refusal lands nothing");
}

// An out-of-range ordinal is a REFUSAL BY NAME, not DAMAGE (the old row held
// "not damage and not a refusal"; strict-only-read keeps the first half and
// reverses the second): REFUSE is total, so no counter moves.
#[test]
fn c10_forged_ordinal_is_a_refusal_not_damage() {
    println!("C10: a forged ordinal is refused, not malformed, and moves no counter");
    let (n, _, report) = load(&forged(4));
    assert_eq!(n, None, "the read refuses: {report:?}");
    assert!(!report.damaged, "an out-of-range ordinal is not damage: {report:?}");
    assert!(report.refused, "an out-of-range ordinal IS a refusal now (strict-only-read): {report:?}");
    assert_eq!(report.reason, TableFixedReason::OutOfRange, "refusal name out_of_range: {report:?}");
    assert_eq!(report.unknown, 0, "unknown stays zero: {report:?}");
    // strict-only-read: was `mismatch == 0`; kind_mismatch is retired as a count
    // and would itself refuse KindMismatch, so the reason above being
    // OutOfRange is the statement that no kind mismatch fired.
    assert_ne!(report.reason, TableFixedReason::KindMismatch, "kind_mismatch did not fire: {report:?}");
    assert_eq!(report.widened, 0, "widened stays zero: {report:?}");
}

// The fields BESIDE the forged ordinal once still landed (the bounds pass
// closed one field and the record read on). strict-only-read: no partial
// value — the whole load refuses and every field, the union and the scalar
// after the enum included, stays at its declared default (R4).
#[test]
fn c10_fields_beside_the_forged_ordinal_do_not_land() {
    println!("C10: the fields beside the forged ordinal do not land");
    let (n, values, report) = load(&forged(4));
    assert_eq!(n, None, "the whole load refuses: {report:?}");
    assert_ne!(values[0].tail, 55, "the scalar after the enum does not land");
    assert_eq!(values[0].tail, DECLARED_TAIL, "it holds its declared default");
    assert_eq!(values[0].effect.tag(), 0, "the union beside the enum holds its default: no arm");
    assert_ne!(
        values[0].effect.boost().map(|b| b.power),
        Some(17),
        "and the arm's payload did not land"
    );
}

// THE BOUNDARY CONTROL: the top variant is AT the bound, not past it.
#[test]
fn c10_top_variant_is_not_past_the_bound() {
    let (n, values, report) = load(&forged(3));
    assert_eq!(n, Some(1), "the top variant reads: {report:?}");
    assert_eq!(values[0].grade, 3);
    assert!(!report.refused, "the top variant is not refused: {report:?}");
}
