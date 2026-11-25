// C7 — a ranged scalar off its declared range REFUSES `out_of_range`
//
// Law (docs/FIXED-FORM-ALGORITHM.md §4.6, the bounds-pass row of the answer
// table): "a ranged scalar off the plan's end" answers `out_of_range`, and
// the whole read refuses: nothing lands and every row handed in goes back to
// its declared default (SPEC-TABLES §6.5, strict-only). Until 2026-11-25 this
// row asserted the retired clamp-and-count answer through a report field,
// `oor`, that the report no longer has.
//
// The production entry point is fx_root_fixed_load: it runs the plan
// (table_fixed_run) and then the clamp pass (fx_root_fixed_clamp_body) over
// every record it reads. This test writes an out-of-range value through the
// production WRITER (fx_root_fixed_save) as a LAWFUL record and then patches
// the two ranged slots on the wire — the writer's bounds are debug_asserts
// (the write contract of an assert leg), so a debug build refuses to write
// the poison itself — reads it back through the production reader, and holds
// the refusal to the law.
//
// The patch is the vector: `renamed` (body offset 6) carries 5000, past its
// declared | max = 1000; `gone` (body offset 10) carries -7, under its
// declared | min = 0 (test/tables/FX1.schema).
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

// THE VECTOR: a lawful record from the production writer, then the two ranged
// slots patched past their declared bounds.
fn forged() -> Vec<u8> {
    let row = FxRootRow {
        keep: 1,
        narrow: 0,
        renamed: 500,
        gone: 500,
        nested: FxNestedRow { a: 111, b: 222 },
        ..Default::default()
    };
    let mut file = vec![0u8; fx_root_fixed_measure(1)];
    assert_eq!(
        fx_root_fixed_save(&[row], &mut file),
        Some(file.len()),
        "the writer must land the lawful record"
    );
    let body = body_at();
    file[body + 6..body + 10].copy_from_slice(&5000i32.to_le_bytes()); // renamed: max = 1000
    file[body + 10..body + 14].copy_from_slice(&(-7i32).to_le_bytes()); // gone: min = 0
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

// THE CELL: the writer lands an out-of-range value and the reader's bounds
// pass refuses the whole read; the rows handed in are back at their defaults.
#[test]
fn c7_out_of_range_refuses() {
    let file = forged();
    let mut want = FxRootRow::default();
    fx_root_fixed_scatter(&FX_ROOT_FIXED_DEFAULTS, &mut want, &mut TableFixedReport::default());
    let (n, values, report) = load(&file);
    check(n.is_none(), "C7: an out-of-range ranged scalar refuses the read");
    check(report.refused, "C7: the read is refused, never clamped and landed");
    check(report.reason == TableFixedReason::OutOfRange, "C7: refused out_of_range");
    check(
        values.iter().all(|v| v.renamed == want.renamed && v.gone == want.gone && v.nested.a == want.nested.a),
        "C7: every row handed in is back at its declared defaults",
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
}

// AN IN-RANGE VALUE LANDS, proving the bounds pass only refuses values
// outside the declared bounds.
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
    check(!report.refused, "C7: in-range refuses nothing");
}

fn main() {
    c7_out_of_range_refuses();
    c7_plan_run_alone_leaves_out_of_range_standing();
    c7_in_range_value_is_untouched();
    println!("C7: all assertions passed");
}
