// W3 — zero behind a narrower arm (the rust leg, fixed form, form byte 3).
//
// THE LAW (docs/FIXED-FORM-ALGORITHM.md:204, in the §3.1 paragraph the card
// points at :201; docs/SPEC-TABLES.md:6736 says it again per row):
//
//   "**A union** writes the tag and the taken arm only, zero behind a narrower
//   one."
//
//   "| a UNION | the tag ordinal at its tag type's storage width + `max` over
//   the arms of `C(arm)` — **tag, then the WIDEST ARM**; **tag `0` is `None`
//   ...**; ZERO on write behind a narrower arm, IGNORED on read, never a
//   refusal |"
//
// The zeros behind a narrower arm ARE the template's (:201-203: "the slack
// rule is one rule: write the live extent onto the zeroed template and stop"),
// so a writer that spilled ANYTHING behind a narrower arm — the caller's
// storage, the overlay's other arm, the widest arm's storage — breaks this law
// (fix 1's "a value nobody wrote").
//
// THE PRODUCTION PATH. The entry point is `fu_root_fixed_save`, the generated
// writer for FU1's fixed table FuRoot (test/tables/FU1.schema), through
// `fu_root_fixed_write_body` → `pick_fixed_write_body`, the union's stores:
// the tag at union+0, then the TAKEN arm only — the generated comment says
// exactly this ("The bytes between a narrower arm and the widest are declared
// slack and they stay the template's zeros, which this function reaches by not
// touching them"). FuRoot's `pick Pick` has two arms of DIFFERENT widths:
// `plain Plain` is 4 bytes and `labelled Labelled` is 20, so taking `plain`
// leaves 16 bytes of declared slack behind it. The writer is read back through
// `fu_root_fixed_load`, the identity plan (this build's own layout, hash
// FU_ROOT_FIXED_HASH).
//
// THE VECTOR is built HERE from the law: the file is the 16-byte header, the
// u32 layout length, the 242-byte layout block, then per record an 8-byte
// layout hash and the 37-byte body. The emitted writer lands the union's tag
// at body+6, the plain arm's payload at body+7..11, and `tail`/`mark`/`heat`
// at body+27..31/31..33/33..37 — so the slack behind the narrower arm is
// body+11..27, sixteen bytes. THE OUTPUT BUFFER IS STAINED with 0xAB before
// the save, and THE ROW'S OTHER ARM IS STAINED TOO (a labelled arm with
// distinctive bytes is set, then the plain arm over it) — every byte behind
// the narrower arm must still be ZERO, which is what separates the template's
// zeros from an untouched buffer and from a writer that writes the widest
// storage behind a narrower arm.
//
// THE NEGATIVE CONTROL, IN THE SAME BINARY: the widest arm under the same
// writer — the 20 arm bytes are live and the fields after the union land, so
// the zeros above are the narrower arm's doing and not a reader or writer that
// drops everything.
//
// It reaches the generated crate the way the driver's dependency graph does,
// one module per generated source file, skipping the WIRE module (`fu1.rs`)
// because this row is about the fixed form; the fixed modules have no
// dependency on the serialize runtime (docs/FIXED-FORM-ALGORITHM.md §3.4).
//
//   rustc --edition 2021 --test ... test/conformance/rust/rows/W3.rs \
//       -o build/rows-rust-W3 && ./build/rows-rust-W3
//
// Exit 0 green, 1 red; one printed line per assertion.
#![allow(unused_imports)]

#[path = "../../../../build/tables-generated-rust/tblfu1/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblfu1/src/fu1_records.rs"]
mod fu1_records;
#[path = "../../../../build/tables-generated-rust/tblfu1/src/fu1_fixed.rs"]
mod fu1_fixed;

pub use fixed_runtime::*;
pub use fu1_fixed::*;
pub use fu1_records::*;

static FAILURES: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);

fn check(ok: bool, what: &str) {
    if ok {
        println!("ok - {what}");
    } else {
        println!("FAIL - {what}");
        FAILURES.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }
}

const PICK_TAG_AT: usize = 6; // the union's tag byte, inside the body
const PLAIN_ARM_AT: usize = 7; // the plain arm's payload, 4 bytes
const PLAIN_ARM_BYTES: usize = 4; // Plain { n int32 }
const LABELLED_ARM_BYTES: usize = 20; // Labelled { lead, label(8), trail }
const SLACK_BEHIND: usize = PICK_TAG_AT + 1 + PLAIN_ARM_BYTES; // body+11
const SLACK_END: usize = PICK_TAG_AT + 1 + LABELLED_ARM_BYTES; // body+27, the widest arm's end
const TAIL_AT: usize = 27;
const MARK_AT: usize = 31;
const HEAT_AT: usize = 33;

fn file_body_offset() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + FU_ROOT_FIXED_BLOCK.len() + 8
}

// THE NARROW-ARM VECTOR: the overlay FIRST carries a labelled arm with
// distinctive bytes, then the plain arm is selected — the writer must take
// the plain arm ONLY and leave the 16 bytes behind it the template's zeros.
fn narrow_row() -> FuRootRow {
    let mut row = FuRootRow::default();
    row.flag = true;
    row.note_present = true;
    row.note = 7;
    let mut leaky = LabelledRow::default();
    leaky.lead = 0x1111_1111;
    leaky.label[..6].copy_from_slice(b"LEAKED");
    leaky.label_length = 6;
    leaky.trail = 0x2222_2222;
    row.pick.set_labelled(leaky);
    let mut plain = PlainRow::default();
    plain.n = 42;
    row.pick.set_plain(plain); // the narrower arm, tag 1
    row.tail = 55;
    row.mark = -2;
    row.heat = 1.5;
    row
}

fn wide_row() -> FuRootRow {
    let mut row = FuRootRow::default();
    row.flag = true;
    row.note_present = true;
    row.note = 7;
    let mut arm = LabelledRow::default();
    arm.lead = 5;
    arm.label[..3].copy_from_slice(b"abc");
    arm.label_length = 3;
    arm.trail = 9;
    row.pick.set_labelled(arm); // the widest arm, tag 2
    row.tail = 55;
    row.mark = -2;
    row.heat = 1.5;
    row
}

// The writer saves one record into a STAINED buffer: 0xAB everywhere, so a
// byte the writer never touches shows itself.
fn saved(row: &FuRootRow) -> Vec<u8> {
    let need = fu_root_fixed_measure(1);
    let mut file = vec![0xABu8; need];
    let written = fu_root_fixed_save(std::slice::from_ref(row), &mut file)
        .expect("the writer saves the record");
    assert_eq!(written, need, "the fixture must write a whole file");
    file
}

fn load(data: &[u8]) -> (Option<usize>, FuRootRow, TableFixedReport) {
    let mut values = [FuRootRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let n = fu_root_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values[0], report)
}

#[test]
fn test_row_w3() {
    let body = file_body_offset();

    // ---- THE POSITIVE CONTROL: the WIDEST arm, under the same writer ------
    //
    // The labelled arm is the widest (20 bytes), so there is no slack behind
    // it: its 20 bytes are live, and everything after the union lands.
    println!("W3 positive control: the widest arm (labelled, tag 2) writes and reads back");
    let file = saved(&wide_row());
    check(file[body + PICK_TAG_AT] == 2, "W3 control: the tag byte is 2 (labelled)");
    let arm = &file[body + PLAIN_ARM_AT..body + SLACK_END];
    check(
        arm[..4] == 5u32.to_le_bytes(),
        "W3 control: the labelled arm's lead lands at body+7..11",
    );
    check(
        arm[4..8] == 3u32.to_le_bytes(),
        "W3 control: the labelled arm's length word lands at body+11..15",
    );
    check(
        &arm[8..11] == b"abc",
        "W3 control: the labelled arm's text lands at body+15..18",
    );
    check(
        arm[16..20] == 9u32.to_le_bytes(),
        "W3 control: the labelled arm's trail lands at body+23..27",
    );
    check(
        u32::from_le_bytes(file[body + TAIL_AT..body + TAIL_AT + 4].try_into().unwrap()) == 55,
        "W3 control: tail lands at body+27..31, right behind the widest arm",
    );
    let (n, back, report) = load(&file);
    check(
        n == Some(1),
        &format!("W3 control: the identity plan reads one record: {report:?}"),
    );
    check(
        back.pick.tag() == 2 && back.pick.labelled().map(|a| a.trail) == Some(9),
        "W3 control: the labelled arm reads back",
    );

    // ---- THE CELL: the NARROWER arm, and the zeros behind it --------------
    println!("W3: the narrower arm (plain, tag 1) writes 4 live bytes and 16 template zeros");
    let file = saved(&narrow_row());
    check(
        file[body + PICK_TAG_AT] == 1,
        "W3: the tag byte is 1 (plain, the narrower arm)",
    );
    check(
        file[body + PLAIN_ARM_AT..body + PLAIN_ARM_AT + PLAIN_ARM_BYTES] == 42u32.to_le_bytes(),
        "W3: the taken arm's payload lands at body+7..11",
    );
    // THE LAW: zero behind a narrower arm — the 16 bytes between the plain
    // arm's end (body+11) and the widest arm's end (body+27) are the
    // template's zeros, with the caller's 0xAB stain and the overlay's leaked
    // labelled bytes both pushed out.
    let mut zeros_ok = true;
    for i in SLACK_BEHIND..SLACK_END {
        if file[body + i] != 0 {
            zeros_ok = false;
        }
    }
    check(
        zeros_ok,
        "W3: the 16 bytes behind the narrower arm (body+11..27) are the template's zeros — nothing of the caller's buffer, nothing of the other arm",
    );
    check(
        u32::from_le_bytes(file[body + TAIL_AT..body + TAIL_AT + 4].try_into().unwrap()) == 55,
        "W3: tail lands at body+27..31, right behind the union's widest extent",
    );
    check(
        i16::from_le_bytes(file[body + MARK_AT..body + MARK_AT + 2].try_into().unwrap()) == -2,
        "W3: mark lands at body+31..33",
    );
    check(
        f32::from_le_bytes(file[body + HEAT_AT..body + HEAT_AT + 4].try_into().unwrap()) == 1.5,
        "W3: heat lands at body+33..37",
    );
    let (n, back, report) = load(&file);
    check(
        n == Some(1),
        &format!("W3: the identity plan reads one record: {report:?}"),
    );
    check(back.pick.tag() == 1, "W3: the tag reads back 1");
    check(
        back.pick.plain().map(|a| a.n) == Some(42),
        "W3: the plain arm's payload reads back 42",
    );
    check(
        back.pick.labelled().is_none(),
        "W3: the other arm is None — the tag names the narrower arm only",
    );
    check(back.tail == 55, "W3: tail reads back through the identity plan");

    let fails = FAILURES.load(std::sync::atomic::Ordering::Relaxed);
    assert_eq!(fails, 0, "W3: {fails} assertion(s) failed");
}