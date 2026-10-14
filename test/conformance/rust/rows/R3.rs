// R3 — the floor is 1 + the highest retired index (0 when none); below the
// floor is layout_unsupported, reporting the file's hash.
//
// docs/FIXED-FORM-ALGORITHM.md:497 (COMPILE):
//   R.floor := 1 + the highest index marked RETIRED in lock.lineage(T), or 0 when none
//
// docs/FIXED-FORM-ALGORITHM.md:507-509 (COMPILE loop):
//   if i < R.floor:  -- RETIRED. The hash stays KNOWN, so a file
//     R.plans[i] := NONE  -- carrying it is named layout_unsupported and
//                           -- never layout_newer; its PLAN IS NEVER BUILT
//
// docs/FIXED-FORM-ALGORITHM.md:830 (LOAD step 6):
//   if i < R.floor:  REFUSE layout_unsupported, reporting h
//
// docs/FIXED-FORM-ALGORITHM.md:888 (condition table):
//   the hash is in the lineage, below the floor | layout_unsupported | the file's hash
//
// docs/FIXED-FORM-ALGORITHM.md:552-553:
//   The floor is one number and the lineage is one array, so "retired" is an
//   index cut and the operator's two answers stay distinct: below the floor is
//   layout_unsupported (upgrade the client), outside the lineage is
//   layout_newer (ship the reader).
//
// This test uses FE_ROOT (tblfe1), whose lineage has ONE entry (this build's
// own layout) and floor = 0.  With floor = 0 the emitter omits the floor
// check (fixedform.go:1129-1138), so the test verifies:
//   1. the FLOOR constant is 0 — "0 when none"
//   2. the lineage structure — one entry, not retired
//   3. a file at the floor reads
//   4. a file with a hash outside the lineage is refused layout_newer, NOT
//      layout_unsupported — proving the two refusals are distinct
//
// The negative control changes FLOOR from 0 to 1 in the generated code; the
// test asserts FLOOR == 0 and goes RED.  The "below the floor" runtime path
// is exercised by the versioning probes (make tables-rust-versioning,
// internal/codegen/rusttable/fixedversioning_test.go:268, floor_below).

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

use std::sync::atomic::{AtomicUsize, Ordering};

static FAILURES: AtomicUsize = AtomicUsize::new(0);

fn check(ok: bool, what: &str) {
    if ok {
        println!("  ok: {what}");
    } else {
        println!("FAIL: {what}");
        FAILURES.fetch_add(1, Ordering::Relaxed);
    }
}

#[test]
fn r3_floor_law() {
    // ---- PART 1: the FLOOR constant is 0 when none retired -----------------

    check(
        FE_ROOT_FIXED_FLOOR == 0,
        "the floor is 0 when no entry is retired",
    );
    check(
        FE_ROOT_FIXED_LINEAGE.len() == 1,
        "a table with no lock has a lineage of one",
    );
    check(
        !FE_ROOT_FIXED_LINEAGE[0].retired,
        "the single lineage entry is not retired",
    );
    check(
        FE_ROOT_FIXED_LINEAGE[0].hash == FE_ROOT_FIXED_HASH,
        "the lineage entry's hash is this build's own",
    );
    eprintln!(
        "  floor = {}, lineage len = {}, own hash = {:#018x}",
        FE_ROOT_FIXED_FLOOR,
        FE_ROOT_FIXED_LINEAGE.len(),
        FE_ROOT_FIXED_HASH,
    );

    // ---- PART 2: a file at the floor reads ---------------------------------

    let mut effect = EffectRow::default();
    effect.set_boost(BoostRow { power: 42 });
    let row = FeRootRow {
        grade: 1,
        effect,
        tail: 99,
    };
    let mut file = vec![0u8; fe_root_fixed_measure(1)];
    let written = fe_root_fixed_save(&[row], &mut file);
    check(
        written == Some(file.len()),
        "the fixture writes a whole file",
    );

    let mut values = [FeRootRow::default(); 4];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fe_root_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report);

    check(n == Some(1), "a file at the floor reads one record");
    check(
        !report.refused && report.reason == TableFixedReason::None,
        "a clean read is not a refusal",
    );
    check(!report.malformed, "a clean read is not damage");
    check(values[0].grade == 1, "the grade value lands");
    check(values[0].tail == 99, "the field after the union lands");
    check(
        values[0].effect.boost().map(|b| b.power) == Some(42),
        "the union arm's payload lands",
    );

    // ---- PART 3: hash outside lineage is layout_newer, NOT layout_unsupported

    let stranger_hash = 0xDEAD_BEEF_CAFE_F00Du64;
    file[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8]
        .copy_from_slice(&stranger_hash.to_le_bytes());

    let mut values2 = [FeRootRow::default(); 4];
    let mut plan2 = [TableFixedEntry::default(); 512];
    let mut remap2 = [0u16; 512];
    let mut report2 = TableFixedReport::default();
    let n2 = fe_root_fixed_load(&mut values2, &file, &mut plan2, &mut remap2, &mut report2);

    check(n2.is_none(), "a stranger's hash is refused");
    check(
        report2.reason.name() == "layout_newer",
        "outside the lineage is layout_newer, NOT layout_unsupported",
    );
    check(
        report2.layout_hash == stranger_hash,
        "layout_newer carries THE FILE'S hash",
    );
    check(
        !report2.malformed,
        "a refusal by name is not damage (§5.3 joint answer)",
    );
    check(report2.refused, "a refusal by name sets the refused flag");
    check(
        report2.widened == 0
            && report2.unknown == 0
            && report2.kind_mismatch == 0
            && report2.clamped == 0,
        "REFUSE is TOTAL: no counter moves",
    );
    let fresh = FeRootRow::default();
    check(
        values2[0].grade == fresh.grade
            && values2[0].tail == fresh.tail
            && values2[0].effect.tag() == fresh.effect.tag(),
        "REFUSE wrote no destination bytes",
    );

    assert_eq!(
        FAILURES.load(Ordering::Relaxed),
        0,
        "rust/R3: the floor law"
    );
}
