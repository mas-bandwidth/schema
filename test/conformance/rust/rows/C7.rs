// C7 — ranged scalar clamp (docs/FIXED-FORM-ALGORITHM.md:351)
//
// Law: "A RANGED SCALAR clamps to its declared min and max, COUNT clamped."
//
// The production entry point is fx_root_fixed_load: it runs the plan
// (table_fixed_run) and then the clamp pass (fx_root_fixed_clamp_body) over
// every record it reads. This test writes an out-of-range value through the
// production WRITER (fx_root_fixed_save — the write side's bounds are
// debug-only by rule, so a caller CAN land one on the wire), reads it back
// through the production reader, and holds the loaded value and the clamped
// counter to the law.
//
// The byte vector is built by the writer, so its derivation is the law the
// writer implements: `renamed` carries 5000, one past its declared | max = 1000;
// `gone` carries -7, one under its declared | min = 0 (test/tables/FX1.schema).
//
// Exit 0 green / exit 1 red, one printed line per assertion.

// --- the generated FX1 fixed-form modules, via #[path] like C10 ---
#[path = "../../../../build/tables-generated-rust/tblfx1/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblfx1/src/fx1_records.rs"]
mod fx1_records;
#[path = "../../../../build/tables-generated-rust/tblfx1/src/fx1_fixed.rs"]
mod fx1_fixed;

pub use fixed_runtime::*;
pub use fx1_fixed::*;
pub use fx1_records::*;

fn check(ok: bool, msg: &str) {
    if !ok {
        panic!("FAIL: {msg}");
    }
    println!("  ok: {msg}");
}

// THE VECTOR: written by the production writer, the poison in-range values
// the writer's debug-only bounds cannot refuse.
fn forged() -> Vec<u8> {
    let row = FxRootRow {
        keep: 1,
        narrow: 0,
        renamed: 5000, // declared | min = 0, max = 1000
        gone: -7,      // and the low end of the same declaration
        nested: FxNestedRow { a: 111, b: 222 },
        ..Default::default()
    };
    let mut file = vec![0u8; fx_root_fixed_measure(1)];
    assert_eq!(
        fx_root_fixed_save(&[row], &mut file),
        Some(file.len()),
        "the writer must land the vector"
    );
    file
}

// THE BODY OFFSET: header(16) + layout_len(4) + layout(225) + record_hash(8) = 253
fn body_at() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FX_ROOT_FIXED_BLOCK.len() + 8
}

fn load(data: &[u8]) -> (Option<usize>, [FxRootRow; 2], TableFixedReport) {
    let mut values = [FxRootRow::default(); 2];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fx_root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values, report)
}

// THE POSITIVE CONTROL: the writer lands an out-of-range value and the
// reader's clamp pass brings it home.
#[test]
fn c7_out_of_range_clamps_to_bounds() {
    let file = forged();
    let (n, values, report) = load(&file);
    check(n == Some(1), "C7: the record reads on the identity plan");
    check(values[0].renamed == 1000, "C7: a value past max lands at max");
    check(values[0].gone == 0, "C7: a value under min lands at min");
    check(report.clamped == 2, "C7: two clamps, counted");
    check(
        values[0].nested.a == 111 && values[0].nested.b == 222,
        "C7: an in-range neighbour is untouched",
    );
}

// THE NEGATIVE CONTROL, in the test and compiling: the plan run ALONE, no
// straight-line pass after it. The same record, the same plan, and the
// out-of-range value survives — which is what says the bound is held by the
// pass after the loop and not by a plan entry.
#[test]
fn c7_plan_run_alone_leaves_out_of_range_standing() {
    let file = forged();
    let body = &file[body_at()..];
    let mut image = [0u8; FX_ROOT_FIXED_BODY_BYTES];
    let mut report = TableFixedReport::default();
    table_fixed_run(
        &FX_ROOT_FIXED_PLAN,
        &[],
        body,
        &mut image,
        &mut report,
    );
    // The plan copied raw bytes into image (body layout, not struct layout).
    // Use scatter to land them into the struct — same as the production code
    // does before the clamp pass — and show the values are still out of range.
    let mut loose = FxRootRow::default();
    fx_root_fixed_scatter(&image, &mut loose, &mut report);
    check(
        loose.renamed == 5000,
        "C7: the plan run alone leaves renamed=5000 standing",
    );
    check(loose.gone == -7, "C7: the plan run alone leaves gone=-7 standing");
    check(report.clamped == 0, "C7: the plan run alone counts nothing");
}

// AN IN-RANGE VALUE IS UNTOUCHED, proving the clamp pass only fires for
// values outside the declared bounds.
#[test]
fn c7_in_range_value_is_untouched() {
    let row = FxRootRow {
        keep: 1,
        narrow: 0,
        renamed: 500, // within [0, 1000]
        gone: 500,    // within [0, 1000]
        nested: FxNestedRow { a: 111, b: 222 },
        ..Default::default()
    };
    let mut file = vec![0u8; fx_root_fixed_measure(1)];
    assert_eq!(
        fx_root_fixed_save(&[row], &mut file),
        Some(file.len()),
        "the writer must land the vector"
    );
    let (n, values, report) = load(&file);
    check(n == Some(1), "C7: the in-range record reads");
    check(values[0].renamed == 500, "C7: in-range renamed survives");
    check(values[0].gone == 500, "C7: in-range gone survives");
    check(report.clamped == 0, "C7: in-range clamps nothing");
}

fn main() {
    c7_out_of_range_clamps_to_bounds();
    c7_plan_run_alone_leaves_out_of_range_standing();
    c7_in_range_value_is_untouched();
    println!("C7: all assertions passed");
}
