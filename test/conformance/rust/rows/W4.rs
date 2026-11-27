// test/conformance/rust/rows/W4.rs — schema matrix cell rust/W4
//
// "read slack unspecified" (docs/roadmap.sexp:3346, roadmap row live-extents,
// audit schema#898, matrix schema#876; docs/FIXED-FORM-ALGORITHM.md:346).
//
// THE LAW MOVED, TWICE, and this row follows it as the Go row
// (test/conformance/go/rows/W4_test.go) and the cpp row (cpp/rows/W4.cpp) do:
//   * docs/rulings.json fixed-dead-bytes-are-zero (2026-11-10) supersedes the
//     slack rule quoted below: a counted array's slots past its count are
//     DEAD, and a non-zero dead byte refuses the whole load as wire damage in
//     the CHECK pass, before any record lands (docs/FIXED-FORM-ALGORITHM.md
//     §4.5 `count` row, §5.3 step 11); every row handed in stays at its
//     declared default (R4).
//   * docs/rulings.json strict-only-read (2026-11-03): no read clamps; a LIVE
//     value past its declared range refuses the whole load out_of_range.
// Each old assertion is mapped in place: "garbage slack reads clean, moves no
// counter" -> "garbage slack refuses as damage, lands nothing, moves no
// counter"; the control's "the live 999 clamps to 10 and counts one" -> "the
// live 999 refuses out_of_range and lands nothing". The quotes below are the
// row's ORIGINAL law, kept for that mapping.
//
// THE LAW, the read's half of the slack rule, in the two places it is written:
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
// slack; its bytes are unspecified, and no counter may move for them.
//
// THE VECTOR is built in this file from the law, because the tracked tree
// carries no data for this cell: the writer zero-fills every byte of declared
// slack (root_config_fixed_write_body's template, tables_fixed.rs:455, and
// SPEC §3.4 "ZERO ON WRITE"), so no fixture a lawful writer ever produced has
// garbage there — the garbage is written HERE, by the test, exactly as the
// peer the law names would leave it. One record, 2 live weapons of 8, saved
// clean; then the six slack slots' `penetration` bytes are patched to 999,
// outside [0, 10]. The byte addresses are the generated stores' own: the
// record body follows the 8-byte record hash at
//   TABLE_FIXED_HEADER_BYTES + 4 + ROOT_CONFIG_FIXED_BLOCK.len(),
// the weapons count sits at body+20 (write_body's b[20..24]), slot i at
// body+24+i*22 (`let at = 24 + i * 22`), and `penetration` at slot+8
// (weapon_config_fixed_write_body's `let at = 8`).
//
// THE CONTROLS. (1) In-test, on every invocation: the SAME forged file with
// the count byte alone raised 2 -> 3 — one byte moved, nothing else — turns
// the stained slot LIVE, and the read must clamp it, count exactly one
// clamp, and land the clamped 10 in the value: the no-counter result above
// is the slack's live-ness and nothing else, and the read still bounds
// everything it read. (2) The file-level control this card runs once by
// hand: patch the generated pass's live extent — tables_fixed.rs:542,
// `for i in 0..value.weapons_count.clamp(0, 8) as usize` -> `for i in 0..8` —
// and the forged slack is walked: the read counts exactly 6 clamps and the
// whole test goes red; restore and it is green again.
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
/// The caller's row is POISONED first (weapons_count 5), so a refused load
/// that leaves it at its declared default is R4's reset and not a no-op.
fn w4_load(file: &[u8]) -> (Option<usize>, [RootConfigRow; 1], TableFixedReport) {
    let mut back = [RootConfigRow::default(); 1];
    back[0].weapons_count = 5;
    let mut plan = [TableFixedEntry::default(); 1024];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let n = root_config_fixed_load(&mut back, file, &mut plan, &mut remap, &mut report);
    (n, back, report)
}

/// R4: every row handed in to a refused load holds its DECLARED default —
/// tables/examples/Tables.schema's RootConfig declares no `=` on
/// `version_note string(16)` or `weapons [..8]WeaponConfig`, so the empty
/// text and a count of 0 (the caller's poisoned 5 replaced) — and none of the
/// file's live values (penetration 3 and 7, the note "W4-slack") landed.
fn w4_at_defaults(v: &RootConfigRow) -> bool {
    v.weapons_count == 0
        && v.version_note_length == 0
        && v.weapons[0].penetration != 3
        && v.weapons[1].penetration != 7
}

// ---------------------------------------------------------------------------
// the law: a peer that leaves garbage in the slack reads clean
// ---------------------------------------------------------------------------

#[test]
fn w4_garbage_slack_is_dead_and_refuses_as_damage() {
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

    // THE CLEAN TWIN READS CLEAN: the same read with the writer's own bytes.
    let (n, back, report) = w4_load(&clean);
    assert_eq!(n, Some(1), "the clean record reads");
    // strict-only-read: was `oor == 0`; `held` is the runtime's out-of-range mark
    assert!(
        report.held == 0 && !report.damaged && !report.refused,
        "the clean twin moves no counter: {report:?}"
    );
    assert_eq!(back[0].weapons[1].penetration, 7, "the clean twin's live value lands");

    // THE FORGE: the six slack slots' penetration bytes become 999 — outside
    // [0, 10], a value no lawful writer ever put there. This is the peer the
    // law names: one that "leaves garbage there".
    let mut forged = clean.clone();
    let stain = 999i32.to_le_bytes();
    for i in 2..8 {
        let at = w4_penetration_at(body_at, i);
        forged[at..at + 4].copy_from_slice(&stain);
    }
    for i in 2..8 {
        let at = w4_penetration_at(body_at, i);
        assert_eq!(
            &forged[at..at + 4],
            &stain[..],
            "CONTROL: the forged file really carries the garbage in slot {i}'s slack"
        );
    }

    // THE LAW, WHOLE, AS RULED NOW (fixed-dead-bytes-are-zero): the garbage in
    // the dead slots refuses the whole load as wire DAMAGE in the CHECK pass
    // (was: "not malformed, not a refusal"), before any record lands — and it
    // is the dead-byte rule, not the bounds pass walking the slack: the reason
    // is not out_of_range and no out-of-range mark is left (was: `oor == 0`).
    let (n, back_forged, report) = w4_load(&forged);
    assert_eq!(n, None, "the record with garbage in its dead slots refuses the whole load");
    assert!(
        report.damaged && report.reason != TableFixedReason::OutOfRange,
        "non-zero dead bytes are wire damage, not an out_of_range refusal: {report:?}"
    );
    assert_eq!(
        report.held, 0,
        "the bounds pass never walked the dead slots: {report:?}"
    );
    assert_eq!(
        report.unknown, 0,
        "the garbage slack censuses nothing unknown: {report:?}"
    );
    // strict-only-read: was `mismatch == 0`; a kind mismatch refuses by name now
    assert_ne!(
        report.reason, TableFixedReason::KindMismatch,
        "the garbage slack is no kind mismatch: {report:?}"
    );
    assert_eq!(
        report.widened, 0,
        "the garbage slack moves no widening counter: {report:?}"
    );
    // was: "the live units read correctly" on the forged file — a refused load
    // lands nothing, so the forged read holds the declared defaults (R4) ...
    assert!(
        w4_at_defaults(&back_forged[0]),
        "a refused load lands nothing: the row holds its declared default (R4)"
    );

    // ... and the LIVE UNITS READ CORRECTLY on the clean twin, the same file
    // with zero in its dead slots: the count, the two live weapons' every
    // bounded field, and the string's used units.
    assert_eq!(back[0].weapons_count, 2, "the count is a used unit and reads");
    assert_eq!(back[0].version_note_length, 8, "the string's length reads");
    assert_eq!(
        &back[0].version_note[..8],
        b"W4-slack",
        "the string's used units read"
    );
    assert_eq!(back[0].weapons[0].damage, 21.5, "live weapon 0's damage reads");
    assert_eq!(back[0].weapons[0].speed, 500.5, "live weapon 0's speed reads");
    assert_eq!(
        back[0].weapons[0].penetration, 3,
        "live weapon 0's penetration reads, in range and untouched"
    );
    assert_eq!(back[0].weapons[0].channel, 5, "live weapon 0's channel reads");
    assert!(back[0].weapons[0].homing, "live weapon 0's homing reads");
    assert_eq!(back[0].weapons[1].damage, 30.25, "live weapon 1's damage reads");
    assert_eq!(back[0].weapons[1].speed, 600.75, "live weapon 1's speed reads");
    assert_eq!(
        back[0].weapons[1].penetration, 7,
        "live weapon 1's penetration reads, in range and untouched"
    );
    assert_eq!(back[0].weapons[1].channel, 63, "live weapon 1's channel reads");
    assert!(!back[0].weapons[1].homing, "live weapon 1's homing reads");
}

// ---------------------------------------------------------------------------
// the control, run on every invocation: the law is not true trivially
// ---------------------------------------------------------------------------

/// THE SAME FILE, ONE COUNT BYTE MOVED — the third slot becomes LIVE and
/// the stain in it is a live value past its declared max. To see the bounds
/// pass and not the dead-byte rule, the five slots still dead are zeroed
/// back (they would refuse as damage on their own): the only fault left is
/// the live 999, and strict-only-read refuses the whole load out_of_range
/// (was: "the clamped read still returns, a clamp is never a refusal").
#[test]
fn w4_control_the_same_bytes_made_live_refuse_out_of_range() {
    let v = w4_record();
    let need = root_config_fixed_measure(1);
    let mut clean = vec![0u8; need];
    root_config_fixed_save(&[v], &mut clean).expect("the record saves");
    let body_at = w4_body_at(&clean);

    // slot 2 alone carries the stain; slots 3..8 keep the writer's zeros
    let mut forged = clean.clone();
    let stain = 999i32.to_le_bytes();
    let at = w4_penetration_at(body_at, 2);
    forged[at..at + 4].copy_from_slice(&stain);

    // with the count at 2 the stain is DEAD and refuses as damage, not range
    let (n, _, report) = w4_load(&forged);
    assert_eq!(n, None, "a stain in a dead slot refuses the load");
    assert!(report.damaged && report.held == 0, "as damage, not out_of_range: {report:?}");

    // the count byte is the ONLY byte that moves: body+20 (write_body's
    // b[20..24]), the same forge either way.
    let mut live = forged.clone();
    let count_at = body_at + 20;
    live[count_at..count_at + 4].copy_from_slice(&3i32.to_le_bytes());
    let diffs: Vec<usize> = (0..forged.len())
        .filter(|&i| forged[i] != live[i])
        .collect();
    assert_eq!(
        diffs,
        vec![count_at],
        "the live twin differs from the slack twin at the count byte alone"
    );

    let (n, back, report) = w4_load(&live);
    // strict-only-read: was "a clamping read still returns", "clamped, not
    // refused", "counted exactly one", "lands clamped to 10"
    assert_eq!(n, None, "an out-of-range LIVE element refuses the whole load");
    assert!(
        report.refused && !report.damaged && report.reason == TableFixedReason::OutOfRange,
        "the stained slot made LIVE is walked by the bounds pass and refuses out_of_range: {report:?}"
    );
    assert_eq!(report.held, 0, "REFUSE is total: no mark left standing: {report:?}");
    // was: "the clean live elements beside it are untouched" and "the raised
    // count reads back" — a refusal lands none of them (R4)
    assert!(
        w4_at_defaults(&back[0]),
        "a refused load lands nothing: the live 999, its neighbours and the count hold their defaults"
    );

    // and the same live slot AT the declared max (10) reads: the bound is inclusive
    let mut at_max = live.clone();
    let at = w4_penetration_at(body_at, 2);
    at_max[at..at + 4].copy_from_slice(&10i32.to_le_bytes());
    let (n, back, report) = w4_load(&at_max);
    assert_eq!(n, Some(1), "a live value at the declared max reads: {report:?}");
    assert_eq!(back[0].weapons[2].penetration, 10, "the live 10 lands");
    assert_eq!(back[0].weapons[0].penetration, 3, "the clean live element beside it lands");
    assert_eq!(back[0].weapons[1].penetration, 7, "the clean live element beside it lands");
    assert_eq!(back[0].weapons_count, 3, "the raised count reads back");
}
