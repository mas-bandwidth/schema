// R26 — a known hash whose LINEAGE ENTRY would not build is a LANE carrying
// that entry's own refusal name, never a throw
// (docs/FIXED-FORM-ALGORITHM.md:890, docs/FIXED-FORM-ALGORITHM.md:1494 §5.9 #36).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:890):
//
//   | a known hash whose LINEAGE ENTRY would not build | `layout_malformed` /
//     `plan_too_large` | the name the entry's own lane carries, never a throw
//     (§5.9 #36) |
//
// and §5.9 #36 (docs/FIXED-FORM-ALGORITHM.md:1494): "Every lineage entry becomes
// a lane CARRYING ITS OWN REFUSAL REASON — `layout_malformed` for a layout that
// would not parse, `plan_too_large` for a plan that would not fit — which LOAD
// reports by name if and only if a file selects that entry. Nothing on the load
// path throws, and an entry nobody selects costs nothing but the lane."
//
// THE PRODUCTION PATH, named end to end:
//   fx_root_fixed_load(fx2_fixed.rs)
//     -> FX_ROOT_FIXED_LINEAGE lookup by the header's hash as given
//     -> the entry's lane (`FX_ROOT_FIXED_PLANS.reason[i]`, forced off the load
//        path in the LazyLock)
//     -> the plan builder `table_fixed_compile` (fixed_runtime.rs)
//     -> the refusal by name on `TableFixedReport`.
// The Rust leg turns an entry whose plan would not build into `None` from
// `table_fixed_compile` — the `overflow` flag, never an out-of-bounds index —
// and the generated lane maps that `None` to `plan_too_large`; a layout the
// build could not parse is `layout_malformed`. This test drives the two
// production functions that lane is made of and asserts the names, and that
// nothing panics.
//
// WHY THE OWN-LANE MAPPING IS NOT REACHED DIRECTLY HERE: the generated lane
// (`out.reason[i]` and the `plans.reason[found]` read in fx2_fixed.rs) is
// emitted only for a unit whose lineage has MORE THAN ONE entry. Every table
// unit the Rust conformance leg generates has a lineage of ONE — its own
// layout — because the conformance corpus carries no schema.lock that would
// hand in an older entry (the emitter's `fixedLineage`). A deliberately
// unlawful lineage entry is handed in by the GENERATOR's test harness
// (internal/codegen/rusttable/fixedversioning_test.go, §5.9 #18's "probe units
// live beside the leg's generator"), not by a row of this corpus. What this
// row CAN drive — and what the lane is built from — is the production plan
// compiler and the production load's named refusal, which is what follows.
//
// The card names no data file for this cell (the conformance corpus has none),
// so the vector is built from the law: a lawful fixed-form FILE written by the
// generated `fx_root_fixed_save`, and the same file read with a plan slice
// too small to hold its plan. The names and shapes are the corpus's own.

// The generated fixed-form module of the tblfx2 unit, reached exactly as the
// conformance driver reaches its crates — the generated sources the build
// produced under build/tables-generated-rust/ (make build/conformance-rust /
// make build/tables-generated-rust). Only the FIXED surface is included: the
// fixed runtime, the blittable records and the fixed module reference no
// `serialize`, so rustc compiles them without the sibling checkout cargo needs
// for the packet surface.
#[path = "../../../../build/tables-generated-rust/tblfx2/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblfx2/src/build_version.rs"]
mod build_version;
#[path = "../../../../build/tables-generated-rust/tblfx2/src/fx2_records.rs"]
mod fx2_records;
#[path = "../../../../build/tables-generated-rust/tblfx2/src/fx2_fixed.rs"]
mod fx2_fixed;
pub use fixed_runtime::*;
pub use build_version::*;
pub use fx2_records::*;
pub use fx2_fixed::*;

fn check(ok: bool, what: &str) {
    println!("{} {}", if ok { "PASS" } else { "FAIL" }, what);
    assert!(ok, "{what}");
}

// One lawful record, written by the production writer, so the only variable in
// every case below is the plan's capacity.
fn lawful_file() -> Vec<u8> {
    let mut buffer = vec![0u8; fx_root_fixed_measure(1)];
    let written = fx_root_fixed_save(&[FxRootRow::default()], &mut buffer);
    assert_eq!(written, Some(buffer.len()), "the writer wrote a whole file");
    buffer
}

// THE PLAN COMPILER IS WHERE "WOULD NOT BUILD" IS DECIDED, and it refuses with
// `None` — the `overflow` flag in `Compiler::push` — and never by indexing past
// the caller's slice. A caller capacity of zero compiles no entry, so the
// build cannot be laid down, and the call returns cleanly.
#[test]
fn an_entry_that_would_not_build_is_none_never_a_throw() {
    let block = TableFixedBlock::parse(&FX_ROOT_FIXED_BLOCK).expect("the build's own layout parses");
    let mut plan: [TableFixedEntry; 0] = [];
    let mut remap: [u16; 0] = [];
    let mut report = TableFixedReport::default();
    let made = table_fixed_compile(
        &block,
        &block,
        &FX_ROOT_FIXED_COUNTED,
        &mut plan[..],
        &mut remap[..],
        &mut report,
    );
    check(
        made.is_none(),
        "a plan that does not fit the declared capacity returns None, not a panic",
    );

    // POSITIVE CONTROL: the same walk at a capacity that holds it returns a
    // plan, so the None above is the capacity's and not the fixture's.
    let mut big = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let made = table_fixed_compile(
        &block,
        &block,
        &FX_ROOT_FIXED_COUNTED,
        &mut big[..],
        &mut remap[..],
        &mut report,
    );
    check(
        made.is_some_and(|n| n >= 1),
        "the same plan at a capacity that holds it is built",
    );
}

// THE LOAD REPORTS THE NAME AND NOTHING ELSE. A file selecting a layout whose
// plan does not fit the caller's capacity is `plan_too_large` BY NAME: refused,
// never malformed, no counter moved, and — since this codec never allocates —
// no throw.
#[test]
fn a_plan_that_does_not_fit_is_refused_by_name() {
    let data = lawful_file();
    let mut values = [FxRootRow::default(); 1];
    let mut plan: [TableFixedEntry; 0] = [];
    let mut remap: [u16; 0] = [];
    let mut report = TableFixedReport::default();
    let n = fx_root_fixed_load(&mut values, &data, &mut plan[..], &mut remap[..], &mut report);
    check(n.is_none(), "a plan that does not fit: refused");
    check(
        report.refused && report.reason == TableFixedReason::PlanTooLarge,
        "a plan that does not fit: refused BY NAME — plan_too_large, the name §5.9 #36 owes",
    );
    check(
        !report.malformed
            && report.unknown == 0
            && report.kind_mismatch == 0
            && report.widened == 0
            && report.clamped == 0,
        "a plan that does not fit: a refusal by name is never malformed and moves no counter",
    );

    // POSITIVE CONTROL: at the build's own capacity the same bytes read.
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    check(
        fx_root_fixed_load(&mut values, &data, &mut plan[..], &mut remap[..], &mut report) == Some(1),
        "the same file reads at a capacity that holds the plan, so the refusal was the capacity's",
    );
}

// THE OTHER NAME THE ENTRY'S LANE CARRIES: a layout the entry would not parse
// is `layout_malformed`, by name, on a known hash, never a throw and never
// `malformed` damage. The header still hashes to this build's own layout; one
// byte behind its u32 length differs, so the entry does not match.
#[test]
fn an_entry_that_would_not_parse_is_layout_malformed_by_name() {
    let mut damaged = lawful_file();
    damaged[TABLE_FIXED_HEADER_BYTES + 4] ^= 0xFF; // one byte of the layout behind its length
    let mut values = [FxRootRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let n = fx_root_fixed_load(&mut values, &damaged, &mut plan[..], &mut remap[..], &mut report);
    check(n.is_none(), "a known-hash layout that differs: refused");
    check(
        report.refused && report.reason == TableFixedReason::LayoutMalformed,
        "a known-hash layout that differs: refused BY NAME — layout_malformed, never a throw",
    );
    check(
        !report.malformed && report.unknown == 0 && report.kind_mismatch == 0,
        "a known-hash layout that differs: a refusal by name moves no counter",
    );
}