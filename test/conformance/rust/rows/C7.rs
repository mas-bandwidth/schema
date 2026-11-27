// C7 — ranged scalar BOUND (docs/FIXED-FORM-ALGORITHM.md §4.6)
//
// Law, as cut: "A RANGED SCALAR clamps to its declared min and max, COUNT
// clamped." THE LAW MOVED (docs/rulings.json strict-only-read; docs/plans/
// strict-only.md decision 1): no read clamps. A ranged scalar past its
// declared min or max refuses the WHOLE load out_of_range (the retired
// `clamped`, renamed in place) and every row handed in stays at its declared
// default (R4) — as test/conformance/go/rows/C7_test.go and
// test/conformance/cpp/rows/C7.cpp assert.
//
// The production entry point is fx_root_fixed_load: it runs the plan
// (table_fixed_run), the scatter, then the bounds pass
// (fx_root_fixed_clamp_body), and refuses on the out-of-range mark.
//
// THE VECTOR: the writer's bounds are DEBUG ASSERTS (a test build panics on an
// out-of-range save), so the out-of-range values are FORGED on the wire after
// a lawful save: `renamed` (body[6..10]) gets 5000, past its declared
// | max = 1000; `gone` (body[10..14]) gets -7, under its declared | min = 0
// (test/tables/FX1.schema).
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

// THE BODY OFFSET: header(16) + layout_len(4) + layout + record_hash(8)
fn body_at() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FX_ROOT_FIXED_BLOCK.len() + 8
}

// a lawful record, written by the production writer
fn lawful(renamed: i32, gone: i32) -> Vec<u8> {
    let row = FxRootRow {
        keep: 1,
        narrow: 0,
        renamed,
        gone,
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

// THE VECTOR: a lawful save with `renamed` and `gone` overwritten on the wire
fn forged(renamed: i32, gone: i32) -> Vec<u8> {
    let mut file = lawful(500, 500);
    let b = body_at();
    file[b + 6..b + 10].copy_from_slice(&renamed.to_le_bytes());
    file[b + 10..b + 14].copy_from_slice(&gone.to_le_bytes());
    file
}

fn load(data: &[u8]) -> (Option<usize>, [FxRootRow; 2], TableFixedReport) {
    let mut values = [FxRootRow::default(); 2];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fx_root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values, report)
}

// R4: a refused load leaves the row at its DECLARED defaults — the schema's
// own (test/tables/FX1.schema: keep = 7, renamed = 5, gone = 9, nested.a = 1,
// nested.b = 2), written here as the oracle; FxRootRow::default() is the
// cooked zero form, not the declaration.
fn at_defaults(v: &FxRootRow) -> bool {
    v.keep == 7 && v.renamed == 5 && v.gone == 9 && v.nested.a == 1 && v.nested.b == 2
}

fn refuses_out_of_range(file: &[u8], what: &str) {
    let (n, values, report) = load(file);
    check(n.is_none(), &format!("C7 {what}: the whole load refuses (was: reads on the identity plan)"));
    check(
        report.refused && !report.damaged && report.reason == TableFixedReason::OutOfRange,
        &format!("C7 {what}: refused out_of_range (was: counted clamped): {report:?}"),
    );
    check(
        at_defaults(&values[0]),
        &format!("C7 {what}: a refused load lands nothing (was: lands at the bound; neighbour untouched)"),
    );
}

// THE CELL (was: both values clamp, two clamps counted): each bound alone
// refuses, and both together refuse once — the verdict, not a count.
#[test]
fn c7_out_of_range_refuses_the_load() {
    refuses_out_of_range(&forged(5000, 500), "renamed 5000 past max 1000");
    refuses_out_of_range(&forged(500, -7), "gone -7 under min 0");
    refuses_out_of_range(&forged(5000, -7), "both past their bounds");
    refuses_out_of_range(&forged(1001, 500), "renamed ONE past max");
    refuses_out_of_range(&forged(500, -1), "gone ONE under min");
}

// THE NEGATIVE CONTROL, in the test and compiling: the plan run ALONE, no
// straight-line pass after it. The same record, the same plan, and the
// out-of-range value survives and nothing is refused — which is what says the
// bound is held by the pass after the loop and not by a plan entry.
#[test]
fn c7_plan_run_alone_leaves_out_of_range_standing() {
    let file = forged(5000, -7);
    let body = &file[body_at()..];
    let mut image = [0u8; FX_ROOT_FIXED_BODY_BYTES];
    let mut report = TableFixedReport::default();
    table_fixed_run(
        &FX_ROOT_FIXED_PLAN,
        &[],
        &[],
        body,
        &mut image,
        &mut report,
    );
    // The plan copied raw bytes into image (body layout, not struct layout).
    // Use scatter to land them into the struct — same as the production code
    // does before the bounds pass — and show the values are still out of range.
    let mut loose = FxRootRow::default();
    fx_root_fixed_scatter(&image, &mut loose, &mut report);
    check(
        loose.renamed == 5000,
        "C7: the plan run alone leaves renamed=5000 standing",
    );
    check(loose.gone == -7, "C7: the plan run alone leaves gone=-7 standing");
    // strict-only-read: was `oor == 0`; `held` is the out-of-range mark the
    // load refuses on, and the run and scatter alone raise none for a ranged scalar
    check(
        report.held == 0 && !report.refused,
        "C7: the plan run alone raises no out-of-range mark and refuses nothing",
    );
    // and the bounds pass is what sees them: two out of range
    let mut oor = 0i32;
    let mut damaged = 0i32;
    fx_root_fixed_clamp_body(&mut loose, &mut oor, &mut damaged);
    check(oor == 2, "C7: the bounds pass after the loop sees both values out of range");
}

// AN IN-RANGE VALUE IS UNTOUCHED, proving the bounds pass only fires for
// values outside the declared bounds — and the bounds are inclusive.
#[test]
fn c7_in_range_value_is_untouched() {
    for (r, g) in [(500, 500), (1000, 0), (0, 1000)] {
        let (n, values, report) = load(&lawful(r, g));
        check(n == Some(1), &format!("C7: the in-range record ({r}, {g}) reads"));
        check(values[0].renamed == r, "C7: in-range renamed survives");
        check(values[0].gone == g, "C7: in-range gone survives");
        // strict-only-read: was `oor == 0`
        check(!report.refused && !report.damaged, &format!("C7: in-range is not refused: {report:?}"));
        check(
            values[0].nested.a == 111 && values[0].nested.b == 222,
            "C7: an in-range neighbour is untouched",
        );
    }
}

fn main() {
    c7_out_of_range_refuses_the_load();
    c7_plan_run_alone_leaves_out_of_range_standing();
    c7_in_range_value_is_untouched();
    println!("C7: all assertions passed");
}
