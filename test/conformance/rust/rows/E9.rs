// E9: duplicate never raised.
//
// Law (today): docs/FIXED-FORM-ALGORITHM.md:26-28 — "The one §4 counter this
// form raises is `widened`; every other condition is a refusal". The
// `duplicate` counter is retired outright (docs/plans/strict-only.md decision
// 5: a repeat is damage), and so are `kind_mismatch` and `clamped` as counts
// (docs/rulings.json strict-only-read: "the report counters kind_mismatch,
// clamped, duplicate ... the reason clamped is renamed out_of_range in place").
//
// Earlier text (docs/FIXED-FORM-ALGORITHM.md:26 before strict-only) said "this
// form raises all but duplicate", so the row named unknown, kind_mismatch,
// widened, clamped, damaged and refused as present. Under strict-only-read the
// `mismatch` and `oor` names went with `duplicate`; the row now names the
// report's public members that DO exist, and the size gate still catches any
// field (a `duplicate` among them) added to the struct.

extern crate graphdemo;

#[test]
fn duplicate_never_raised() {
    let r = graphdemo::TableFixedReport::default();

    // THE PUBLIC MEMBERS THIS FORM CARRIES, by name. Silence is the clean
    // read: every counter starts at zero and no flag is raised.
    // strict-only-read: `r.mismatch` and `r.oor` were here; both counters are
    // retired (a kind mismatch and an out-of-range value refuse, reason
    // KindMismatch / OutOfRange), so the row names the refusal channel instead.
    assert_eq!(r.unknown, 0, "unknown starts at zero");
    assert_eq!(r.widened, 0, "widened starts at zero");
    assert!(!r.damaged, "damaged starts down");
    assert!(!r.refused, "refused starts down");
    assert_eq!(r.reason, graphdemo::TableFixedReason::None, "no refusal reason on a default report");
    assert_eq!(r.reason.name(), "none");
    assert_eq!(graphdemo::TableFixedReason::OutOfRange.name(), "out_of_range",
        "the retired clamped count is the out_of_range refusal now");
    assert_eq!(graphdemo::TableFixedReason::KindMismatch.name(), "kind_mismatch",
        "the retired kind_mismatch count is a refusal now");
    assert_eq!(r.non_finite_float, "");
    assert_eq!(r.layout_hash, 0);

    // SIZE GATE: the struct as generated today is 48 bytes on x86-64/aarch64
    // (five u32 counters, two bools and the reason byte, the &'static str and
    // the u64 hash). Adding ANY field — a `duplicate: u32` among them — grows
    // it past 48: the five u32s, two bools and the u8 reason fill 23 bytes of a
    // 24-byte lane, so a u32 more spills into a new 8-byte slot.
    assert_eq!(std::mem::size_of::<graphdemo::TableFixedReport>(), 48,
        "TableFixedReport changed size — a new field (duplicate?) was added");
}
