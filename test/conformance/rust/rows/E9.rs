// E9: duplicate never raised.
//
// Law: docs/FIXED-FORM-ALGORITHM.md §0 — "The one §4 counter this form
// raises is `widened`; every other condition is a refusal" (the maintainer,
// 2026-11-03). `duplicate` is the TEXT form's: the fixed wire never raises
// it, so the fixed report has no such member.
//
// The fixed form's TableFixedReport carries `widened`, the census `unknown`,
// the refusal (`refused`, `reason`) and the two names a refusal reports
// (`non_finite_float`, `layout_hash`). Until 2026-11-25 this row named
// counters the strict-only report retired (`mismatch`, `oor`, `damaged`) and
// pinned a size the report had since outgrown.
//
// THE GATE is size plus the public members by name. The report is
// u64 + &str (16) + five u32 (three of them crate-private: held, slack,
// wire_damage) + bool + a one-byte enum = 46 bytes, padded to 48 on
// x86-64 and aarch64. A `duplicate: u32` makes 50, padded to 56, so the size
// gate goes red on it.

extern crate graphdemo;

#[test]
fn duplicate_never_raised() {
    let r = graphdemo::TableFixedReport::default();

    // THE PUBLIC MEMBERS, by name. Silence is the clean read: every one
    // starts at zero.
    assert_eq!(r.unknown, 0);
    assert_eq!(r.widened, 0);
    assert!(!r.refused);
    assert_eq!(r.reason, graphdemo::TableFixedReason::None);
    assert_eq!(r.non_finite_float, "");
    assert_eq!(r.layout_hash, 0);

    // SIZE GATE: adding a u32 counter (duplicate?) grows the report to 56.
    assert_eq!(std::mem::size_of::<graphdemo::TableFixedReport>(), 48,
        "TableFixedReport grew — a new field (duplicate?) was added");
}
