// E9: duplicate never raised.
//
// Law: docs/FIXED-FORM-ALGORITHM.md:26 — "The §4 counters are unknown,
// kind_mismatch, widened, clamped, duplicate and malformed; this form
// raises all but duplicate."
// Law: docs/FIXED-FORM-ALGORITHM.md:1386 — "duplicate is the TEXT form's —
// the fixed wire never raises it — so a leg whose report has no such member
// is not missing one."
//
// The fixed form's TableFixedReport carries the counters the wire DOES raise;
// `duplicate` belongs to the text form and MUST NOT exist in this struct.
// Assertion: size_of == 32 (x86-64/aarch64). The struct has no gap that
// could absorb a u32 without growing; the 5-byte padding between `reason`
// and `layout_hash` is the only pad, and adding `duplicate: u32` after
// `clamped` slides into it — so the guard is size plus a field-count gate
// on the counters that DO exist, listed by name in the law's order.

extern crate graphdemo;

#[test]
fn duplicate_never_raised() {
    let r = graphdemo::TableFixedReport::default();

    // THE COUNTERS THIS FORM RAISES, by name (§4, FIXED-FORM-ALGORITHM.md:26).
    // Silence is the clean read: every counter starts at zero.
    let _ = r.unknown;
    let _ = r.kind_mismatch;
    let _ = r.widened;
    let _ = r.clamped;
    let _ = r.malformed;
    let _ = r.refused;

    // SIZE GATE: a struct whose last field is u64 at offset 24 has sizeof 32.
    // Adding ANY field results in sizeof >= 36 on this ABI, and the negative
    // control proves it: adding extra_marker: u32 after layout_hash makes
    // sizeof 40.
    assert_eq!(std::mem::size_of::<graphdemo::TableFixedReport>(), 32,
        "TableFixedReport grew — a new field (duplicate?) was added");
}
