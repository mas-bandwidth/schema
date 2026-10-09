// R14 — `layout_record_too_large` FOR AN ENTRY REACHING PAST THE WRITER'S
// DECLARED RECORD, not only for the 65536 bound and a zero root.
//
// rust/R14 (docs/roadmap.sexp:1740); the law:
//
//   docs/FIXED-FORM-ALGORITHM.md:301-305 (§4.2, the compile):
//     "every compiled entry is bounded by the WRITER'S OWN DECLARED RECORD
//     SIZE — `src + size`, plus 4 for a text entry's length word, and a
//     `guard` offset too, all within `root.size`. A layout reaching past it
//     is refused WHOLE and never partly compiled, under
//     `layout_record_too_large` (fix 3): the read side's whole defence."
//
//   docs/FIXED-FORM-ALGORITHM.md:1133 (§5.8 row 13): the reference wired
//     `layout_record_too_large` only to the 65536 bound and a zero root size
//     and returned `layout_malformed` through the compile's failure path; the
//     page owes "`layout_record_too_large` for the entry too (fix 3)".
//
// THE PRODUCTION PATH. The entry point is `table_fixed_compile`, the generated
// runtime's plan compiler (build/tables-generated-rust/<unit>/src/
// fixed_runtime.rs, emitted from internal/codegen/rusttable/fixedruntime.go):
// `Compiler::push` takes EVERY entry's reach — `src + size`, the union's None
// const at `src + size` too, a guard at `guard + width` — against
// `self.record = theirs.entry(0).size`, the WRITER'S declared root size, and
// one entry past it refuses the plan whole under `LayoutRecordTooLarge`
// (fixed_runtime.rs:984-1004, :1070, :1075-1077). The layout blocks it walks
// are the LOCK's bytes, so the vector here is a lock layout an entry of which
// reaches past its own declared record.
//
// THE VECTOR, built from the law (no corpus bytes exist for a forged lock
// layout — the same reasoning test/conformance/cpp/rows/R6.cpp records). A
// block is a u32 entry count then 17-byte entries: id u64LE, kind u8,
// size u32LE, children u32LE. Kind 13 is a table (size = the sum of its
// fields'), kind 15 a union (size = tag + widest arm, the tag 1/2/4/8), kind 4
// a u32. THEIRS is the writer's lock layout: a record of 5 bytes, one union
// whose tag is 1 and whose one arm is a u32. MINE names the same ids with the
// tag grown 1 -> 8 (a real §5.2 evolution): the compile lands the union's None
// const at src 0 with the READER'S tag width, so that one entry reaches
// 0 + 8 = 8, past the writer's declared record of 5 — under 65536, root
// non-zero, every other fact lawful. The reference answered that compile
// `layout_malformed`; the law answers `layout_record_too_large`.
//
// RUN (from ./repo):
//   rustc --edition 2021 --test test/conformance/rust/rows/R14.rs \
//     -o build/rows-rust-R14 && ./build/rows-rust-R14
// (the --extern/-L flags the conformance crate's make rule carries are EMPTY
// for this row: it reaches the generated crate the way rows/C10.rs does, one
// `#[path]` module per generated source file, and the fixed modules have no
// dependency on the serialize runtime — C10.rs:24-26.)
// Exit 0 green, exit 1 red, one printed line per assertion. Depends on no
// other rows/ file.

#[path = "../../../../build/tables-generated-rust/tblfe1/src/fixed_runtime.rs"]
mod fixed_runtime;

use fixed_runtime::{
    table_fixed_compile, TableFixedBlock, TableFixedEntry, TableFixedReason, TableFixedReport,
};

// ---- the vectors ------------------------------------------------------------

fn put32(at: &mut [u8], off: usize, value: u32) {
    at[off..off + 4].copy_from_slice(&value.to_le_bytes());
}

/// One 17-byte block entry: id u64LE, kind u8, size u32LE, children u32LE.
fn entry(id: u64, kind: u8, size: u32, children: u32) -> [u8; 17] {
    let mut e = [0u8; 17];
    e[..8].copy_from_slice(&id.to_le_bytes());
    e[8] = kind;
    put32(&mut e, 9, size);
    put32(&mut e, 13, children);
    e
}

/// A block: u32 count LE, then the entries, back to back.
fn block(entries: &[u8]) -> Vec<u8> {
    let mut b = Vec::with_capacity(4 + entries.len());
    b.extend_from_slice(&((entries.len() / 17) as u32).to_le_bytes());
    b.extend_from_slice(entries);
    b
}

// THEIRS — the writer's lock layout: record 5, a union of tag 1 over one u32.
fn theirs() -> Vec<u8> {
    block(&[
        entry(1, 13, 5, 1)[..].to_owned(),
        entry(7, 15, 5, 1)[..].to_owned(),
        entry(9, 4, 4, 0)[..].to_owned(),
    ]
    .concat())
}

// MINE — same ids, the union's tag grown to 8 (arms unchanged: one u32).
fn mine(tag: u32) -> Vec<u8> {
    let union = tag + 4; // a union's size is its tag plus its widest arm
    block(&[
        entry(1, 13, union, 1)[..].to_owned(),
        entry(7, 15, union, 1)[..].to_owned(),
        entry(9, 4, 4, 0)[..].to_owned(),
    ]
    .concat())
}

fn parse_ok<'a>(bytes: &'a [u8], what: &str) -> TableFixedBlock<'a> {
    match TableFixedBlock::parse(bytes) {
        Ok(b) => {
            println!("ok   parse: {what} is a lawful layout ({} entries)", b.count());
            b
        }
        Err(r) => panic!("{what} must parse before the compile is reached, got {}", r.name()),
    }
}

/// The compile, over the caller's plan storage the load contract declares.
fn compile(
    theirs: &TableFixedBlock,
    mine: &TableFixedBlock,
) -> (Option<usize>, TableFixedReport) {
    let mut plan = [TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = TableFixedReport::default();
    let out = table_fixed_compile(theirs, mine, &[], &mut plan, &mut remap, &mut report);
    (out, report)
}

// THE POSITIVE CONTROL, IN THE SAME BINARY: the writer's layout against
// itself compiles — two entries, no counters moved — so the cell below is the
// reach and not the shapes.
#[test]
fn r14_positive_control_same_layout_compiles() {
    println!("R14 positive control: THEIRS against THEIRS compiles whole");
    let t = theirs();
    let theirs = parse_ok(&t, "the writer's layout");
    assert_eq!(theirs.entry(0).size, 5, "the writer's declared record is 5");
    let (out, report) = compile(&theirs, &theirs);
    assert_eq!(
        out,
        Some(3),
        "the None const, the arm's tag const and the u32's copy compile: {report:?}"
    );
    assert_eq!(report.reason, TableFixedReason::None, "no refusal: {report:?}");
    assert!(!report.refused, "nothing refused: {report:?}");
    assert_eq!(report.unknown, 0, "unknown stays zero: {report:?}");
    assert_eq!(report.kind_mismatch, 0, "kind_mismatch stays zero: {report:?}");
}

// THE BOUNDARY: an entry reaching EXACTLY to the writer's record still
// compiles. The tag grown to 2 reaches 0 + 2 = 2 against a record of 5 — the
// bound is "past and not at" (fixed_runtime.rs:979-983).
#[test]
fn r14_reaching_exactly_the_record_still_compiles() {
    println!("R14 boundary: a tag grown to 2 reaches 2 of 5 and compiles whole");
    let t = theirs();
    let m = mine(2);
    let theirs = parse_ok(&t, "the writer's layout");
    let mine = parse_ok(&m, "the reader's layout (tag 2)");
    let (out, report) = compile(&theirs, &mine);
    assert_eq!(
        out,
        Some(3),
        "the plan compiles: None const, arm's tag const, the u32's copy: {report:?}"
    );
    assert_eq!(report.reason, TableFixedReason::None, "no refusal: {report:?}");
    assert!(!report.refused, "nothing refused: {report:?}");
}

// THE CELL: one entry whose reach passes the writer's declared record refuses
// the plan WHOLE, by the law's name — never `layout_malformed` (§5.8 row 13),
// never a partly compiled plan, and never a clamp or a count instead.
#[test]
fn r14_entry_past_the_writer_record_refuses_by_name() {
    println!("R14: the None const reaches 0 + 8 against a declared record of 5");
    let t = theirs();
    let m = mine(8);
    let theirs = parse_ok(&t, "the writer's layout");
    let mine = parse_ok(&m, "the reader's layout (tag 8)");
    assert_eq!(
        theirs.entry(0).size, 5,
        "the writer's declared record: 5 bytes, no 65536 bound, no zero root"
    );
    let (out, report) = compile(&theirs, &mine);
    assert_eq!(out, None, "the plan is refused whole: {report:?}");
    assert!(report.refused, "a refusal, not a decode: {report:?}");
    assert_eq!(
        report.reason,
        TableFixedReason::LayoutRecordTooLarge,
        "the law's name for an entry past the writer's record: {report:?}"
    );
    assert_eq!(
        report.reason.name(),
        "layout_record_too_large",
        "the name spells layout_record_too_large"
    );
    assert_ne!(
        report.reason,
        TableFixedReason::LayoutMalformed,
        "§5.8 row 13: the compile's failure path is NOT layout_malformed"
    );
    assert!(!report.malformed, "a refusal by name is not damage: {report:?}");
    assert_eq!(report.clamped, 0, "nothing clamped: {report:?}");
    assert_eq!(report.unknown, 0, "refuse is total: {report:?}");
    assert_eq!(report.kind_mismatch, 0, "refuse is total: {report:?}");
    assert_eq!(report.layout_hash, 0, "the name carries no hash: {report:?}");
}

// THE NAME IS ONE NAME across both wirings of the law: the 65536 parse bound
// and the compile's entry bound land the same constant, so the zero-root and
// 65536 cases the item's title sets aside stay under it too.
#[test]
fn r14_same_name_as_the_parse_bound() {
    println!("R14: the 65536 parse bound lands the same constant the compile does");
    let mut f = theirs();
    put32(&mut f, 4 + 9, 65537); // the root's size lane, past the ceiling
    match TableFixedBlock::parse(&f) {
        Err(r) => {
            assert_eq!(r.name(), "layout_record_too_large", "the parse bound's name: {r:?}");
            println!("ok   parse refuses 65537 under layout_record_too_large");
        }
        Ok(_) => panic!("a root of 65537 must not parse"),
    }
    let mut z = theirs();
    put32(&mut z, 4 + 9, 0); // the root's size lane, zero
    match TableFixedBlock::parse(&z) {
        Err(r) => {
            assert_eq!(r.name(), "layout_record_too_large", "the zero root's name: {r:?}");
            println!("ok   parse refuses a zero root under layout_record_too_large");
        }
        Ok(_) => panic!("a zero root must not parse"),
    }
}
