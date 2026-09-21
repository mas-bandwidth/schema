// rust/E6 — Renaming uses the declared identity (audit schema#898, matrix
// schema#876, roadmap rust/E6, docs/roadmap.sexp:2441).
//
// THE LAW (docs/SPEC-TABLES.md:3219, "Renaming is not a change"):
//   "`was = \"old_name\"` (§5) keeps a field's identity through a rename: the
//    wire id stays the hash of the old name. **A rename under `was` is
//    therefore not a change to a fixed table at all** — the lock records the
//    wire name, so the file does not move."
// And the read's half of it (docs/FIXED-FORM-ALGORITHM.md:1549, §5.9 #41;
// docs/FIXED-FORM-ALGORITHM.md:641):
//   "A `was =` RENAME IS PAIRED BY WIRE ID IN ANY HARNESS, NEVER BY NAME. ...
//    `id` is `fnv1a64(wire name)`, so `was =` matches by the OLD name."
// The declared identity of a field is fnv1a64 of the name the `was`
// attribute declares (ir/ir.go TableFieldWireName — "the `was` alias after a
// rename, else the name"; ir/tablewire.go TableWireId — "fnv1a64 of the
// name, with no fold and no rebound"). FX2 spells the field `renamed_to`
// and declares `was = "renamed"` (test/tables/FX2.schema), so its wire id is
// fnv1a64("renamed") and FX1's `renamed` field carries the same id — the
// two spellings are ONE wire identity, and a read pairs them by it.
//
// THE PRODUCTION PATH THIS TEST DRIVES is the leg's own fixed reader with a
// lineage handed in, which is what a lock-holding build runs:
// fx_root_fixed_load's non-identity branch (build/tables-generated-rust/
// tblfx2/src/fx2_fixed.rs) selects a locked layout, table_fixed_compile()
// (fixed_runtime.rs) pairs the two layouts' entries BY ID, table_fixed_run()
// lands the record over that plan, fx_root_fixed_scatter() moves the image
// into the value surface, and fx_root_fixed_clamp_body() is the read's
// bounds pass. A standalone crate carries a lineage of ONE — its own layout
// — so the test plays the lock (§5.9 #1: the backend opens no file; the
// caller hands the entries), which is the same hand-off
// internal/codegen/rusttable/fixedversioning_test.go makes the corpus rows.
//
// THE VECTOR is built in this file from the law, because the tracked tree
// carries no data for this cell: testdata/conformance/tables' `was` rows
// (w1/w2, r1/r2) are id-table wire instances on surfaces the Rust leg does
// not answer, and the fixed form's rename pair (FX1/FX2) lives in the
// untracked build/fixedform-corpus. The derivation, every step of it the
// law states:
//   - a layout block is a u32 entry count then 17-byte entries —
//     `id u64 LE, kind u8, size u32 LE, children u32 LE` —
//     ir/fixedform.go TableFixedLayoutBytes (TableFixedEntryBytes = 17);
//   - an entry's id is fnv1a64 of the field's WIRE name (§5);
//   - the pre-order walk pushes the root table first
//     (ir/fixedform.go TableFixedWalkRoot), then the fields in the
//     WRITER's declared order;
//   - the writer's record body is the values in that order, every field at
//     its declared storage width (docs/SPEC-TABLES.md §3.4).
// The OLD writer below spells the field `renamed` and declares its own
// order [renamed, keep, gone] — a lawful order and a DIFFERENT position
// from the reader's [.., renamed_to, ..], so a reader that paired by
// POSITION would land `gone`'s bytes in the renamed field and this test
// would red. The values are the fixedform corpus's own fixtures (the ones
// test/rust-fixedform asserts): keep 4242, renamed 321, gone 9.
//
// THE CONTROLS. (1) In-test, run on every invocation: the same read with
// the reader's block copy patched so the renamed entry carries the NEW
// name's hash — the exact wire a reader that ignores `was` would declare —
// must lose the field to its declared default and census it unknown, while
// `keep` still pairs: the pairing dies with the declared identity and with
// nothing else. (2) The file-level control this card runs once by hand:
// patch the same eight bytes of the generated constant
// (build/tables-generated-rust/tblfx2/src/fx2_fixed.rs, entry 3's id) and
// the whole test goes red; restore and it is green again.
//
// HOW THIS FILE REACHES THE GENERATED CODE: the leg's driver links two
// generated crates (graphdemo, blockdemo — test/conformance/rust/Cargo.toml)
// and neither declares a rename, so this cell's units are tblfx1/tblfx2 of
// make/rust.mk's RUST_TABLE_UNITS. Their rlibs cannot be linked on a bench
// without the serialize.rs sibling checkout (ci.json's "runtime", which CI
// checks out beside the tree), so the test compiles the SAME generated
// sources cargo would — by path, with no runtime, the fixed form's modules
// using none of serialize — and the assert is against the files the leg's
// own targets generate, not a copy of them.
//
// Standalone: depends on no other rows/ file, edits no shared file.
// Run from the repository root (make/rust.mk's flags are cargo's own
// --extern/-L for the conformance crate; this test needs none — see above):
//   rustc --edition 2021 --test test/conformance/rust/rows/E6.rs \
//     -o build/rows-rust-E6 && ./build/rows-rust-E6

#![allow(dead_code)]

#[path = "../../../../build/tables-generated-rust/tblfx2/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblfx2/src/fx2_records.rs"]
mod fx2_records;
#[path = "../../../../build/tables-generated-rust/tblfx2/src/fx2_fixed.rs"]
mod fx2_fixed;

pub use fixed_runtime::*;
pub use fx2_fixed::*;
pub use fx2_records::*;

// ---------------------------------------------------------------------------
// the law's own hash, and the old writer built from the law
// ---------------------------------------------------------------------------

/// fnv1a64 — the wire identity of a name (ir/tablewire.go TableWireId;
/// docs/SPEC-TABLES.md §5), "with no fold and no rebound".
fn fnv1a64(name: &str) -> u64 {
    let mut h: u64 = 0xCBF29CE484222325;
    for b in name.as_bytes() {
        h ^= *b as u64;
        h = h.wrapping_mul(0x100000001B3);
    }
    h
}

/// THE OLD WRITER'S LAYOUT, from the law: a u32 count then 17-byte entries
/// (id u64 LE, kind u8, size u32 LE, children u32 LE — ir/fixedform.go
/// TableFixedLayoutBytes). The root table first, then the fields in the
/// writer's declared order [renamed, keep, gone]: kind 13 is a table, kind 8
/// a u32, kind 4 an i32 (ir/tablekind.go), the root's size is the writer's
/// whole record body, and a field's children are 0.
///
/// `renamed` carries THE DECLARED IDENTITY — fnv1a64("renamed"), the name
/// FX2's `was` declares — at the FIRST position, while the reader spells the
/// same field `renamed_to` at its third: one identity, two positions, which
/// is what a rename through `was` is (docs/SPEC-TABLES.md §5).
fn the_old_writer_block() -> Vec<u8> {
    let mut b = Vec::new();
    let push32 = |b: &mut Vec<u8>, v: u32| b.extend_from_slice(&v.to_le_bytes());
    let push64 = |b: &mut Vec<u8>, v: u64| b.extend_from_slice(&v.to_le_bytes());
    let entry = |b: &mut Vec<u8>, id: u64, kind: u8, size: u32, children: u32| {
        push64(b, id);
        b.push(kind);
        push32(b, size);
        push32(b, children);
    };
    push32(&mut b, 4); // the entry count
    // the root: FxRoot, 12 body bytes, 3 fields
    entry(&mut b, fnv1a64("FxRoot"), 13, 12, 3);
    // `renamed` FIRST, under the name `was` declares
    entry(&mut b, fnv1a64("renamed"), 4, 4, 0);
    // `keep` second
    entry(&mut b, fnv1a64("keep"), 8, 4, 0);
    // `gone` third — the field the reader does not carry at all
    entry(&mut b, fnv1a64("gone"), 4, 4, 0);
    b
}

/// THE OLD WRITER'S RECORD BODY, in the writer's own order: renamed 321,
/// keep 4242, gone 9 — the fixedform corpus's fixture values, every one at
/// its declared width, nothing padded between them (§3.4).
fn the_old_writer_record() -> Vec<u8> {
    let mut b = Vec::new();
    b.extend_from_slice(&321u32.to_le_bytes()); // renamed
    b.extend_from_slice(&4242u32.to_le_bytes()); // keep
    b.extend_from_slice(&9u32.to_le_bytes()); // gone
    b
}

// ---------------------------------------------------------------------------
// the read: the lock-holding load's own calls, in its own order
// ---------------------------------------------------------------------------

/// Compiles the plan from a locked layout (the test plays the lock),
/// prefills the holes the plan does not cover from the declared defaults,
/// runs the record over the plan, and scatters into the value surface —
/// exactly fx_root_fixed_load's non-identity branch.
fn e6_read(mine_bytes: &[u8], report: &mut TableFixedReport) -> FxRootRow {
    let mine = TableFixedBlock::parse(mine_bytes).expect("the reader's layout parses");
    let locked = the_old_writer_block();
    let theirs = TableFixedBlock::parse(&locked).expect("the locked layout parses");
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let n = table_fixed_compile(
        &theirs,
        &mine,
        &FX_ROOT_FIXED_COUNTED,
        &mut plan,
        &mut remap,
        report,
    );
    let n = n.expect("the plan compiles");
    let mut image = [0u8; FX_ROOT_FIXED_BODY_BYTES];
    let mut cover = [0u8; FX_ROOT_FIXED_BODY_BYTES];
    let mut hole_buf = [TableFixedHole::default(); FX_ROOT_FIXED_BODY_BYTES];
    let holes = table_fixed_holes(&plan[..n], &mut cover, &mut hole_buf);
    for h in &hole_buf[..holes] {
        let (o, z) = (h.off as usize, h.size as usize);
        image[o..o + z].copy_from_slice(&FX_ROOT_FIXED_DEFAULTS[o..o + z]);
    }
    table_fixed_run(&plan[..n], &[], &the_old_writer_record(), &mut image, report);
    let mut value = FxRootRow::default();
    fx_root_fixed_scatter(&image, &mut value, report);
    // AND THE BOUND THE LOOP DOES NOT HOLD, the read's last pass (§3.4).
    let mut clamped = 0i32;
    let mut damaged = 0i32;
    fx_root_fixed_clamp_body(&mut value, &mut clamped, &mut damaged);
    report.clamped += clamped as u32;
    if damaged != 0 {
        report.malformed = true;
    }
    value
}

// ---------------------------------------------------------------------------
// the assertions, one printed line each on a red
// ---------------------------------------------------------------------------

/// THE DECLARED IDENTITY IS ON THE WIRE: the reader's own layout carries,
/// for the field it spells `renamed_to`, the id the `was` attribute
/// declares — fnv1a64("renamed") — at scalar-i32 shape, and the NEW name's
/// hash rides nowhere in the block.
#[test]
fn e6_the_renamed_field_rides_the_declared_identity() {
    let mine = TableFixedBlock::parse(&FX_ROOT_FIXED_BLOCK).expect("this build's own layout parses");
    let declared = fnv1a64("renamed");
    let new_name = fnv1a64("renamed_to");
    let mut found = false;
    for i in 0..mine.count() {
        let e = mine.entry(i);
        if e.id == declared {
            found = true;
            assert!(
                e.kind == 4 && e.size == 4 && e.children == 0,
                "the entry carrying the declared identity is not the renamed scalar: kind={} size={} children={}",
                e.kind, e.size, e.children
            );
        }
        assert!(
            e.id != new_name,
            "entry {i} carries fnv1a64(\"renamed_to\") — the NEW name rides the wire, and `was` declares the old one"
        );
    }
    assert!(
        found,
        "no entry of this build's layout carries fnv1a64(\"renamed\") — the declared identity is off the wire"
    );
}

/// THE RENAME READS THROUGH THE DECLARED IDENTITY, AND MOVES NO COUNTER:
/// the old writer's `renamed` bytes land in the value surface spelled
/// `renamed_to`, the field the writer does not carry keeps its declared
/// default, the reader cannot name `gone` (one unknown, and never the
/// renamed field), and nothing else moves.
#[test]
fn e6_a_rename_reads_through_the_declared_identity_and_moves_no_counter() {
    let mut report = TableFixedReport::default();
    let value = e6_read(&FX_ROOT_FIXED_BLOCK, &mut report);
    assert!(
        value.renamed_to == 321,
        "the old writer's `renamed` bytes did not land in `renamed_to`: renamed_to={}",
        value.renamed_to
    );
    assert!(
        value.keep == 4242,
        "the unmoved field beside the rename did not land: keep={}",
        value.keep
    );
    assert!(
        value.added == 11,
        "a field the old writer does not carry is not its declared default: added={}",
        value.added
    );
    assert!(
        value.narrow == 3,
        "a field the old writer does not carry is not its declared default: narrow={}",
        value.narrow
    );
    assert!(
        report.unknown == 1,
        "the one field this reader cannot name is `gone`, and the renamed field must census nothing: unknown={}",
        report.unknown
    );
    assert!(
        report.widened == 0 && report.kind_mismatch == 0 && report.clamped == 0,
        "a rename through `was` is not a version, and no counter moves for it: widened={} kind_mismatch={} clamped={}",
        report.widened, report.kind_mismatch, report.clamped
    );
    assert!(
        !report.malformed && !report.refused && report.reason == TableFixedReason::None,
        "a rename under `was` reads clean: {:?}",
        report
    );
}

/// THE CONTROL, RUN ON EVERY INVOCATION (the law is not true trivially):
/// the same read with the reader's block patched so the renamed entry
/// carries the NEW name's hash — the exact wire a reader that ignores `was`
/// would declare — must lose the field to its declared default, census it
/// unknown beside `gone`, and still pair `keep`: the pairing dies with the
/// declared identity and with nothing else.
#[test]
fn e6_control_a_wire_id_taken_from_the_new_name_loses_the_field() {
    // the renamed entry is where the walk puts it: root + 2 fields down
    let mine = TableFixedBlock::parse(&FX_ROOT_FIXED_BLOCK).expect("this build's own layout parses");
    assert!(
        mine.entry(3).id == fnv1a64("renamed"),
        "the entry the walk puts the renamed field at is not the declared identity: {:#x}",
        mine.entry(3).id
    );
    let mut broken = FX_ROOT_FIXED_BLOCK;
    let at = 4 + 3 * 17; // entry 3's id, the law's 17-byte entry
    broken[at..at + 8].copy_from_slice(&fnv1a64("renamed_to").to_le_bytes());
    let mut report = TableFixedReport::default();
    let value = e6_read(&broken, &mut report);
    assert!(
        value.renamed_to == 5,
        "a block keyed by the NEW name must lose the field to its declared default: renamed_to={}",
        value.renamed_to
    );
    assert!(
        value.keep == 4242,
        "the break must be the renamed field's alone: keep={}",
        value.keep
    );
    assert!(
        report.unknown == 2,
        "with the declared identity gone, the old writer's `renamed` is one more field this reader cannot name: unknown={}",
        report.unknown
    );
}
