// test/conformance/rust/rows/R8.rs — rust/R8
//
// LAW (docs/FIXED-FORM-ALGORITHM.md:1265, §5.3's condition table):
//   | the hash is in no lineage entry | `layout_unknown`, in every leg | the file's hash, and nothing else |
//
// and §5.3 "STEP 5's ANSWER IS `layout_unknown`, IN EVERY LEG, AND
// `layout_newer` IS RETIRED" (SPEC-TABLES §21.3, decided 2026-11-03): nothing
// in a lineage proves a hash newer, so every step-5 miss is layout_unknown and
// no read answers layout_newer.
//
// THE PRODUCTION READER, NOT A MODEL OF IT. This row first carried its own
// copy of the runtime types and of the step-5 select, and asserted the copy:
// it could not go red on the generated reader, and it pinned the retired
// layout_newer. It now reads real files through the generated Rust reader of
// FE_ROOT (build/tables-generated-rust/tblfe1, test/tables FE1), the unit R3
// reads too. The file is what the generated writer lays down; only the
// header's hash is stamped over with hashes no lineage entry holds. Each read
// must answer EXACTLY refused + layout_unknown + the file's hash — the whole
// report equal to that and nothing else — and land nothing: every row comes
// back at its declared default (ruling R4).

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
fn r8_hash_in_no_lineage_entry_is_layout_unknown() {
    let mut effect = EffectRow::default();
    effect.set_boost(BoostRow { power: 42 });
    let row = FeRootRow {
        grade: 1,
        effect,
        tail: 99,
    };
    let mut file = vec![0u8; fe_root_fixed_measure(1)];
    let written = fe_root_fixed_save(&[row], &mut file);
    check(written == Some(file.len()), "the fixture writes a whole file");

    // THE CONTROL: the clean file reads, so a refusal below is the hash's.
    let mut values = [FeRootRow::default(); 4];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fe_root_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report);
    check(n == Some(1) && !report.refused, "the writer's own file reads (the control)");

    let strangers = [
        0xDEAD_BEEF_CAFE_F00Du64,
        1,
        u64::MAX,
        FE_ROOT_FIXED_HASH ^ (1u64 << 63),
    ];
    for stranger in strangers {
        check(
            FE_ROOT_FIXED_LINEAGE.iter().all(|k| k.hash != stranger),
            "the stranger hash is in no lineage entry",
        );
        let mut forged = file.clone();
        forged[TABLE_FIXED_HASH_AT..TABLE_FIXED_HASH_AT + 8].copy_from_slice(&stranger.to_le_bytes());

        let mut values = [FeRootRow::default(); 4];
        let mut plan = [TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = TableFixedReport::default();
        let n = fe_root_fixed_load(&mut values, &forged, &mut plan, &mut remap, &mut report);

        check(n.is_none(), "a hash in no lineage entry is refused");
        println!(
            "  stranger {stranger:#018x}: reason {} layout_hash {:#018x}",
            report.reason.name(),
            report.layout_hash
        );
        check(
            report.reason.name() == "layout_unknown",
            "the refusal is layout_unknown (§5.3 step 5), never the retired layout_newer",
        );
        let want = TableFixedReport {
            refused: true,
            reason: TableFixedReason::LayoutUnknown,
            layout_hash: stranger,
            ..TableFixedReport::default()
        };
        check(
            report == want,
            "the report is exactly refused + layout_unknown + THE FILE'S hash, and nothing else",
        );
        // RULING R4: a refused load leaves every row handed in at its DECLARED
        // default (test/tables/FE1.schema: grade = Silver, effect no arm,
        // tail = 7); none of the file's values (grade 1, tail 99, boost 42) is
        // among them, so nothing was decoded.
        check(
            values
                .iter()
                .all(|v| v.grade == 2 && v.tail == 7 && v.effect.tag() == 0),
            "nothing decoded: every row holds its declared defaults (R4)",
        );
    }

    assert_eq!(
        FAILURES.load(Ordering::Relaxed),
        0,
        "rust/R8: a hash in no lineage entry is layout_unknown, reporting the file's hash and nothing else"
    );
}
