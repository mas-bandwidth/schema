// P3 — HOSTILE BYTES, SWEEP + SANITIZER (docs/roadmap.sexp:3929, rust/P3;
// hostile-input/rust).
//
// THE LAW, docs/FIXED-FORM-ALGORITHM.md:1686 (§7 proof 5 — the card's
// mechanical pointer :1080 lands on a table separator; the governing sentence
// for the title words is :1686, verified):
//
//   "a byte-flip fuzz over the whole file, under a sanitizer, and it is not
//    optional — every offset is arithmetic over sizes a stranger wrote down,
//    so every byte, one bit at a time, is answered one of three ways and
//    never a fourth: a refusal by name, a `malformed` read, or a read that
//    lands values."
//
// The same rule at docs/SPEC-TABLES.md:7131 ("A BYTE-FLIP FUZZ OVER A WHOLE
// FILE, UNDER A SANITIZER ... ONE OF THREE WAYS AND NEVER A FOURTH ... 'The
// reader never leaves the buffer' is a claim only a sanitizer can hold"), and
// the §8 coverage-matrix row at docs/FIXED-FORM-ALGORITHM.md:1714
// ("byte-flip fuzz, sanitizer, three answers never a fourth | §7 item 5").
//
// THE PRODUCTION PATH is `chain_fixed_load`, the generated form-3 reader for
// build/tables-generated-rust/tblp3 (test/tables/P3.schema — table Chain with
// its ?Link optional), reached the way row C10 reaches FE1: one module per
// generated source file by #[path], skipping the WIRE module (`p3.rs`)
// because the fixed modules have no dependency on the serialize runtime.
// The unit and fixture are the ones the sibling cell rows already drive:
// test/conformance/go/rows/P3_test.go and test/conformance/dart/rows/P3.dart.
//
// THE VECTOR, from the law's own datum: `chain_fixed_save` writes one lawful
// WHOLE file — 16 header bytes + 4 block-length + 106 layout + one 45-byte
// record = 171 bytes — fixture values mirrored from the go/dart rows (name
// "tips" length 4, link present with value 42, tag "tag" length 3). The tree
// carries no form-3 whole-file fixture under testdata/ (that corpus is
// build/fixedform-corpus, a make target), so the file is written by the
// generated writer itself; every offset of it is then arithmetic the reader
// takes from stranger-written sizes.
//
// THE SANITIZER HALF is the leg's own reading of the clause
// (test/rust-fixedform/src/main.rs:2295-2305): rustc stable carries no ASan,
// the BUILD is the sanitizer — every index this reader reaches runs through
// checked slices and `.get()`, and any escape out of the buffer is a PANIC,
// which under `rustc --test` fails this binary. A panic inside the sweep IS a
// fourth answer and never arrives quietly.
//
// No assertion of this cell existed under test/conformance/rust/ (STEP 2);
// the leg's earlier twin over fx1.bin is the_hostile_sweep at
// test/rust-fixedform/src/main.rs:2313, run by `make tables-rust-fixedform`
// (make/rust.mk:277), CI .github/workflows/ci-fast.yml:616. This file is the
// matrix row for the cell. One printed line per assertion.
//
// Run from ./repo (no --extern/-L needed: the included fixed modules have no
// dependency on the conformance crate's serialize runtime, like C10 and E6):
//
//   rustc --edition 2021 --test test/conformance/rust/rows/P3.rs \
//     -o build/rows-rust-P3 && ./build/rows-rust-P3
//
// Exit 0 = green, non-zero = red.
#![allow(unused_imports)]

#[path = "../../../../build/tables-generated-rust/tblp3/src/fixed_runtime.rs"]
mod fixed_runtime;
#[path = "../../../../build/tables-generated-rust/tblp3/src/p3_records.rs"]
mod p3_records;
#[path = "../../../../build/tables-generated-rust/tblp3/src/p3_fixed.rs"]
mod p3_fixed;

pub use fixed_runtime::*;
pub use p3_records::*;
pub use p3_fixed::*;

// One lawful whole file, by the generated writer: header, layout, one record.
fn lawful_file() -> Vec<u8> {
    let row = ChainRow {
        name: {
            let mut name = [0u8; 17];
            name[..4].copy_from_slice(b"tips");
            name
        },
        name_length: 4,
        link: LinkRow {
            value: 42,
            tag: {
                let mut tag = [0u8; 9];
                tag[..3].copy_from_slice(b"tag");
                tag
            },
            tag_length: 3,
        },
        link_present: true,
    };
    let mut file = vec![0u8; chain_fixed_measure(1)];
    let wrote = chain_fixed_save(&[row], &mut file);
    assert_eq!(
        wrote,
        Some(file.len()),
        "the fixture must write a whole file: {wrote:?}"
    );
    file
}

fn load(
    data: &[u8],
) -> (
    Option<usize>,
    [ChainRow; 4],
    TableFixedReport,
) {
    let mut values = [ChainRow::default(); 4];
    let mut plan = [TableFixedEntry::default(); 64];
    let mut remap = [0u16; 64];
    let mut report = TableFixedReport::default();
    let n = chain_fixed_load(&mut values, data, &mut plan, &mut remap, &mut report);
    (n, values, report)
}

// THE POSITIVE CONTROL, IN THE SAME BINARY: the lawful file lands its values
// and moves nothing, so every refusal below is the forgery and not a reader
// that answers hostile to everything.
#[test]
fn p3_a_lawful_file_lands_values() {
    println!("P3 positive control: the lawful Chain file lands its values and counts nothing");
    let (n, values, report) = load(&lawful_file());
    assert_eq!(n, Some(1), "the lawful file reads one record: {report:?}");
    assert_eq!(
        report,
        TableFixedReport::default(),
        "a lawful read moves no counter and sets no flag: {report:?}"
    );
    assert_eq!(&values[0].name[..4], b"tips", "name lands");
    assert_eq!(values[0].name_length, 4, "name_length lands");
    assert!(values[0].link_present, "link present lands");
    assert_eq!(values[0].link.value, 42, "link value lands");
    assert_eq!(&values[0].link.tag[..3], b"tag", "tag lands");
}

// THE CELL — every byte of the whole file, one bit at a time (§7 proof 5):
// each mutant is answered one of three ways — a refusal by name, a
// `malformed` read, or a read that lands values — and never a fourth. A panic
// anywhere in the sweep (the sanitizer's verdict: a read that left the buffer)
// fails this test before the counts are checked.
#[test]
fn p3_every_single_bit_mutation_gets_one_of_three_answers_never_a_fourth() {
    let file = lawful_file();
    let mut refused: u32 = 0;
    let mut damaged: u32 = 0;
    let mut landed: u32 = 0;
    let mut fourth: Option<String> = None;
    for at in 0..file.len() {
        for bit in 0..8u32 {
            let mut hit = file.clone();
            hit[at] ^= 1u8 << bit;
            let (n, _, report) = load(&hit);
            // THE THREE ANSWERS, classified exactly as the leg's twin does
            // (test/rust-fixedform/src/main.rs:2347-2354).
            let named = n.is_none()
                && report.refused
                && report.reason != TableFixedReason::None
                && !report.malformed;
            let hurt = report.malformed && !report.refused;
            let clean = n.is_some() && !report.refused && !report.malformed;
            if named {
                refused += 1;
            } else if hurt {
                damaged += 1;
            } else if clean {
                landed += 1;
            } else if fourth.is_none() {
                fourth = Some(format!(
                    "byte {at} bit {bit}: n={n:?} refused={} reason={} malformed={} — \
                     a refusal, a malformed read or a read that lands values, never a fourth",
                    report.refused,
                    report.reason.name(),
                    report.malformed
                ));
            }
        }
    }
    let total = (file.len() * 8) as u32;
    if let Some(what) = fourth {
        panic!("{what}");
    }
    assert_eq!(
        refused + damaged + landed,
        total,
        "every mutation answered exactly one way: refused={refused} malformed={damaged} landed={landed}"
    );
    println!(
        "P3: {total} single-bit mutants over {} bytes — refused={refused} malformed={damaged} landed={landed}, three answers never a fourth",
        file.len()
    );
}

// ANTI-VACUOUS: all three answers FIRED on this fixture — a sweep that only
// ever landed (a reader that stopped refusing) or only ever refused (a reader
// that decodes nothing) would still have answered "never a fourth".
#[test]
fn p3_all_three_answers_fire() {
    let file = lawful_file();
    let mut refused: u32 = 0;
    let mut damaged: u32 = 0;
    let mut landed: u32 = 0;
    for at in 0..file.len() {
        for bit in 0..8u32 {
            let mut hit = file.clone();
            hit[at] ^= 1u8 << bit;
            let (n, _, report) = load(&hit);
            if n.is_none() && report.refused && report.reason != TableFixedReason::None && !report.malformed {
                refused += 1;
            } else if report.malformed && !report.refused {
                damaged += 1;
            } else if n.is_some() && !report.refused && !report.malformed {
                landed += 1;
            }
        }
    }
    println!(
        "P3: all three answers fire on the sweep — refused={refused} malformed={damaged} landed={landed}"
    );
    assert!(refused > 0, "a refusal by name must fire: {refused}");
    assert!(damaged > 0, "a malformed read must fire: {damaged}");
    assert!(landed > 0, "a read that lands values must fire: {landed}");
}

// THE LENGTH IS STRANGER-CONTROLLED TOO: one byte off the end is arithmetic
// over a size a stranger wrote down — DAMAGE by name (`malformed`), never a
// silent short read and never a refusal dressed as one.
#[test]
fn p3_a_one_byte_short_file_is_damage() {
    println!("P3: a one-byte-short file is DAMAGE, not a short read");
    let file = lawful_file();
    let mut short = file.clone();
    short.pop();
    let (n, _, report) = load(&short);
    assert_eq!(n, None, "the short file does not land: {report:?}");
    assert!(report.malformed, "one byte off the end is malformed: {report:?}");
    assert!(
        !report.refused,
        "a ragged tail is damage, not a refusal: {report:?}"
    );
    assert_eq!(
        report.reason,
        TableFixedReason::None,
        "and it carries no refusal name: {report:?}"
    );
}
