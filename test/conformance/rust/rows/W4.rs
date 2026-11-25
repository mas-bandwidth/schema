// test/conformance/rust/rows/W4.rs — schema matrix cell rust/W4
//
// "read slack unspecified" (docs/roadmap.sexp:3346, roadmap row live-extents,
// audit schema#898, matrix schema#876; docs/FIXED-FORM-ALGORITHM.md:346).
//
// THE LAW AS IT STANDS (FIXED-FORM-ALGORITHM §3.1, §4.5, §4.6, §5.3 step 11;
// docs/rulings.json fixed-dead-bytes-are-zero): a counted array's slots past
// its count are DEAD bytes. The bounds pass never walks them, and a non-zero
// dead byte refuses the whole load as `wire_damaged`. A live value off its
// bound refuses `out_of_range` (strict-only). Until 2026-11-25 this row held
// the retired "slack is unspecified on read and not a refusal" and the
// retired clamp, through report fields (`oor`, `damaged`, `mismatch`) the
// report no longer has. The older text it was cut from:
//
//   docs/FIXED-FORM-ALGORITHM.md:346 (§4.6, the bounds pass):
//   "It walks only what a read can have written — a counted array's LIVE
//    elements and never its slack — ... because the prefill's defaults are
//    in range by construction, and clamping storage nobody wrote would
//    count a clamp on every clean read."
//
//   docs/SPEC-TABLES.md:6750 (§3.4, THE SLACK RULE, once for every row that
//   has slack):
//   "UNSPECIFIED ON READ, AND NOT A REFUSAL. A reader validates the USED
//    UNITS ONLY and never looks at the slack, so a peer that leaves garbage
//    there is a peer this reader reads correctly. NON-ZERO SLACK IS NOT
//    `malformed`, NOT A REFUSAL, AND MOVES NO COUNTER."
//
// THE PRODUCTION PATH THIS TEST DRIVES is the leg's own fixed read end to
// end: root_config_fixed_load (tables_fixed.rs:1819) selects the identity
// lineage by hash, runs the one-entry identity plan (ROOT_CONFIG_FIXED_PLAN —
// a whole-body Copy, so the FILE's slack bytes land in the image), scatters
// the image into the caller's value (all 8 weapon slots, tables_fixed.rs:500),
// and then the bounds pass root_config_fixed_clamp_body (tables_fixed.rs:535)
// walks `for i in 0..value.weapons_count.clamp(0, 8)` (tables_fixed.rs:542) —
// the LIVE elements only. The case is RootConfig's `weapons [..8]WeaponConfig`
// (tables/examples/Tables.schema): a counted array whose elements carry
// `penetration int32 = 1 | min = 0, max = 10`, a ranged scalar inside a
// counted element — the exact shape the law names. A slot past the count is
// slack: DEAD bytes, refused when non-zero, never bounded.
//
// THE VECTOR is built in this file from the law, because the tracked tree
// carries no data for this cell: the writer zero-fills every byte of declared
// slack (root_config_fixed_write_body's template, tables_fixed.rs:455, and
// SPEC §3.4 "ZERO ON WRITE"), so no fixture a lawful writer ever produced has
// garbage there — the garbage is written HERE, by the test, exactly as the
// peer the law names would leave it. One record, 2 live weapons of 8, saved
// clean; then slot 2's `penetration` bytes (slack under a count of 2) are
// patched to 999, outside [0, 10]. The byte addresses are the generated stores' own: the
// record body follows the 8-byte record hash at
//   TABLE_FIXED_HEADER_BYTES + 4 + ROOT_CONFIG_FIXED_BLOCK.len(),
// the weapons count sits at body+20 (write_body's b[20..24]), slot i at
// body+24+i*22 (`let at = 24 + i * 22`), and `penetration` at slot+8
// (weapon_config_fixed_write_body's `let at = 8`).
//
// THE CONTROL, in-test on every invocation: the SAME stained file with the
// count byte alone raised 2 -> 3 — one byte moved, nothing else — turns the
// stained slot LIVE, and the read must refuse it `out_of_range` instead of
// `wire_damaged`: the dead-byte answer above is the slot's live-ness and
// nothing else, and the read still bounds everything live.
//
// HOW THIS FILE REACHES THE GENERATED CODE (the rust/E6 precedent): the leg's
// driver links two generated crates (graphdemo, blockdemo —
// test/conformance/rust/Cargo.toml) and neither gives this law a case —
// blockdemo's PaddedRow bounds nothing (its `rows [..64]PaddedRow` emits no
// element pass at all), and its RenderFrame's 7.5 MB body carries no fixed
// form — so this cell's unit is tabledemo of make/rust.mk's RUST_TABLE_UNITS.
// Its rlib cannot be linked on a bench without the serialize.rs sibling
// checkout (ci.json's "runtime", which CI checks out beside the tree), so
// the test compiles the SAME generated sources cargo would — by path, with
// no runtime, the fixed form's modules using none of serialize — and the
// assert is against the files the leg's own targets generate, not a copy.
//
// Standalone: depends on no other rows/ file, edits no shared file. Run from
// the repository root (make/rust.mk's flags are cargo's own --extern/-L for
// the conformance crate; this test needs none — see above):
//   rustc --edition 2021 --test test/conformance/rust/rows/W4.rs \
//     -o build/rows-rust-W4 && ./build/rows-rust-W4

#![allow(dead_code)]

#[path = "../../../../build/tables-generated-rust/tabledemo/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tabledemo/src/tables_records.rs"]
mod tables_records;
#[path = "../../../../build/tables-generated-rust/tabledemo/src/tables_fixed.rs"]
mod tables_fixed;

pub use fixed_runtime::*;
pub use tables_fixed::*;
pub use tables_records::*;

// ---------------------------------------------------------------------------
// the record, the file, and the law's own byte addresses
// ---------------------------------------------------------------------------

/// One record with TWO live weapons of eight, every live value inside its
/// declared bounds — the shape every clause of the law is about.
fn w4_record() -> RootConfigRow {
    let mut v = RootConfigRow::default();
    v.version_note[..8].copy_from_slice(b"W4-slack");
    v.version_note_length = 8;
    v.weapons_count = 2;
    v.weapons[0].damage = 21.5;
    v.weapons[0].speed = 500.5;
    v.weapons[0].penetration = 3;
    v.weapons[0].channel = 5;
    v.weapons[0].homing = true;
    v.weapons[1].damage = 30.25;
    v.weapons[1].speed = 600.75;
    v.weapons[1].penetration = 7;
    v.weapons[1].channel = 63; // bits(6)'s own top: in range, clamps nothing
    v.weapons[1].homing = false;
    v
}

/// The record body's offset in a saved file: past the header, the 4-byte
/// layout length, the layout block, and the record's own 8-byte hash
/// (root_config_fixed_save, tables_fixed.rs:1782).
fn w4_body_at(file: &[u8]) -> usize {
    let body = TABLE_FIXED_HEADER_BYTES + 4 + ROOT_CONFIG_FIXED_BLOCK.len() + 8;
    assert!(
        file.len() >= body + ROOT_CONFIG_FIXED_BODY_BYTES,
        "the file is shorter than one record body"
    );
    body
}

/// `penetration`'s four bytes in weapon slot `i` of the record body — the
/// generated stores' own offsets (count at body+20, slots at 24 + i*22, the
/// ranged scalar at slot+8).
fn w4_penetration_at(body_at: usize, i: usize) -> usize {
    body_at + 24 + i * 22 + 8
}

/// The leg's own read, driver-shaped: one call, the whole production path.
fn w4_load(file: &[u8]) -> (Option<usize>, [RootConfigRow; 1], TableFixedReport) {
    let mut back = [RootConfigRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 1024];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let n = root_config_fixed_load(&mut back, file, &mut plan, &mut remap, &mut report);
    (n, back, report)
}

/// The clean record, and the same file with slot 2's penetration stained 999
/// — outside [0, 10], a value no lawful writer ever put there. Slot 2 is
/// SLACK under a count of 2.
fn w4_clean_and_stained() -> (Vec<u8>, Vec<u8>, usize) {
    let v = w4_record();
    let need = root_config_fixed_measure(1);
    let mut clean = vec![0u8; need];
    assert_eq!(
        root_config_fixed_save(&[v], &mut clean),
        Some(need),
        "the clean record saves"
    );
    let body_at = w4_body_at(&clean);
    // THE FILE'S SLACK REALLY IS THE WRITER'S ZEROS before the forge — the
    // template write_body zero-fills every byte of declared slack (§3.4), so
    // the stain below is this test's own doing and nothing the writer left.
    for i in 2..8 {
        let at = w4_penetration_at(body_at, i);
        assert_eq!(
            &clean[at..at + 4],
            &[0, 0, 0, 0][..],
            "CONTROL: clean slot {i}'s penetration is the template's zero, not garbage"
        );
    }
    let mut stained = clean.clone();
    let at = w4_penetration_at(body_at, 2);
    stained[at..at + 4].copy_from_slice(&999i32.to_le_bytes());
    (clean, stained, body_at)
}

// ---------------------------------------------------------------------------
// the law: the slack is DEAD — never bounded, and refused when non-zero
// ---------------------------------------------------------------------------

#[test]
fn w4_garbage_slack_is_dead_and_refuses_wire_damaged() {
    let (clean, stained, _) = w4_clean_and_stained();

    // THE CLEAN TWIN READS CLEAN: the same read with the writer's own bytes,
    // every live unit landing.
    let (n, back, report) = w4_load(&clean);
    assert_eq!(n, Some(1), "the clean record reads: {report:?}");
    assert!(!report.refused, "the clean twin refuses nothing: {report:?}");
    assert_eq!(back[0].weapons_count, 2, "the count is a used unit and reads");
    assert_eq!(&back[0].version_note[..8], b"W4-slack", "the string's used units read");
    assert_eq!(back[0].weapons[0].penetration, 3, "live weapon 0's penetration reads");
    assert_eq!(back[0].weapons[1].penetration, 7, "live weapon 1's penetration reads");
    assert_eq!(back[0].weapons[1].channel, 63, "live weapon 1's channel reads");

    // THE LAW: the stained slot is past the count, so it is DEAD. The bounds
    // pass never walks it (no out_of_range), and the dead-byte check refuses
    // the whole load as wire_damaged.
    let (n, back, report) = w4_load(&stained);
    assert_eq!(n, None, "a non-zero dead byte refuses the whole load: {report:?}");
    assert!(report.refused, "refused, never landed: {report:?}");
    assert_eq!(
        report.reason,
        TableFixedReason::WireDamaged,
        "the dead-byte rule's name, never the bounds pass's out_of_range — the bounds pass walked the slack: {report:?}"
    );
    assert_eq!(back[0].weapons_count, RootConfigRow::default().weapons_count, "nothing landed");
}

// ---------------------------------------------------------------------------
// the control, run on every invocation: the law is not true trivially
// ---------------------------------------------------------------------------

/// THE SAME FILE, ONE COUNT BYTE MOVED — the stained slot that was slack is
/// LIVE now — and the read's answer flips on nothing else: the live 999 is
/// walked by the bounds pass and refused `out_of_range` (§4.6, strict-only),
/// where the dead 999 above was refused `wire_damaged`.
#[test]
fn w4_control_the_same_bytes_made_live_refuse_out_of_range() {
    let (_, stained, body_at) = w4_clean_and_stained();

    // the count byte is the ONLY byte that moves: body+20 (write_body's
    // b[20..24]), the same forge either way.
    let mut live = stained.clone();
    let count_at = body_at + 20;
    live[count_at..count_at + 4].copy_from_slice(&3i32.to_le_bytes());
    let diffs: Vec<usize> = (0..stained.len())
        .filter(|&i| stained[i] != live[i])
        .collect();
    assert_eq!(
        diffs,
        vec![count_at],
        "the live twin differs from the slack twin at the count byte alone"
    );

    let (n, _, report) = w4_load(&live);
    assert_eq!(n, None, "an out-of-range LIVE element refuses the read: {report:?}");
    assert!(report.refused, "refused, never clamped and landed: {report:?}");
    assert_eq!(
        report.reason,
        TableFixedReason::OutOfRange,
        "the stained slot made LIVE is walked by the bounds pass: {report:?}"
    );
}
