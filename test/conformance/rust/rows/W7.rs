// test/conformance/rust/rows/W7.rs — schema matrix cell rust/W7
//
// "arg and meta are two lanes" (docs/FIXED-FORM-ALGORITHM.md:245, fix 12):
// **`arg` and `meta` ARE TWO LANES BECAUSE THEY ARE TWO FACTS, and must never
// share one.**
//
// A `string(N)` under a UNION ARM needs both at once: which arm's tag guards
// the entry (the ordinal, `arg`) and which flavour of text it moves (the
// flavour, `meta` — utf8 / wide / bytes). One lane holding both loses
// whichever was stamped last: a utf8 string under arm 2 read as WIDE, or an
// entry that runs only when the tag equals the flavour — UNDER THE WRONG ARM.
//
// THIS PORT SPENDS TWO LANES WITHOUT A `meta` FIELD: `arg` is a full-width u64
// that holds the arm ordinal and NOTHING ELSE, and the flavour is settled at
// COMPILE time out of the layout's own kind into the entry's `size` (the
// payload's byte span) and `aux` (the span in UNITS — utf8 unit 1, wide unit
// 2). They can never overwrite one another because they never share a field.
//
// The plan is DATA, so the case is reached by BUILDING THE ENTRY — exactly what
// the plan compiler builds for a text field inside a union's second arm — and
// running the runtime loop over it (`table_fixed_run`). The runtime is the
// shared generated fixed-form runtime under build/tables-generated-rust; it is
// self-contained (no `serialize` dependency), which is why this test links it
// by path and not as a crate.

// The shared fixed-form runtime THIS unit generated. It is byte-identical across
// every unit (it is emitted once per unit, `make/rust.mk` RUST_TABLE_UNITS), so
// any unit's copy names the same TableFixedEntry / table_fixed_run the conformance
// driver exercises.
#[path = "../../../../build/tables-generated-rust/tblfu1/src/fixed_runtime.rs"]
mod tblfu1;

use tblfu1::{TableFixedEntry, TableFixedOp, TableFixedReport, table_fixed_run};

use std::sync::atomic::{AtomicUsize, Ordering};

static FAILURES: AtomicUsize = AtomicUsize::new(0);

fn check(ok: bool, msg: &str) {
    if ok {
        println!("ok {msg}");
    } else {
        println!("FAIL {msg}");
        FAILURES.fetch_add(1, Ordering::Relaxed);
    }
}

#[test]
fn arg_and_meta_are_two_lanes() {
    FAILURES.store(0, Ordering::Relaxed);

    // The smallest byte vector from the law: a fixed record body whose tag
    // lives at offset 0 (one byte) and whose `string(8)` lives behind it — a
    // four-byte little-endian used length, then the eight declared content
    // bytes. Arm 2 is the SECOND arm (the labelled one), so the tag is 2; the
    // text is "hello" (5 used bytes).
    //
    //   [0] tag = 2            <- the guard's ordinal fact
    //   [1..5] length = 5      <- the text op's length word
    //   [5..13] "hello\0\0\0"  <- the declared 8-byte span (slack zeroed)
    let record: [u8; 13] = [2u8, 5, 0, 0, 0, b'h', b'e', b'l', b'l', b'o', 0, 0, 0];

    // THE TWO FACTS, TWO LANES. `arg` = 2 is the arm ordinal and nothing else;
    // the utf8 flavour is the unit the compiler baked into `size` (the 8-byte
    // span) and `aux` (8 units). They are different fields, so neither can
    // overwrite the other.
    let text_utf8 = TableFixedEntry {
        src: 1,    // the length word, past the one-byte tag
        dst: 0,    // image: length at 0, content at 4
        size: 8,   // FLAVOUR lane, byte half: the payload's 8-byte span
        aux: 8,    // FLAVOUR lane, unit half: 8 units, because utf8 unit == 1
        guard: 0,  // the tag lives at offset 0
        arg: 2,    // the ORDINAL lane: arm 2, and nothing else
        argw: 1,   // a one-byte tag
        op: TableFixedOp::Text,
        ..TableFixedEntry::default()
    };

    // GREEN: the ordinal lane says arm 2, the record's tag is 2, so the entry
    // runs — and it lands the utf8 BYTES, not a wide re-read of the same span.
    let mut image = [0u8; 12];
    let mut report = TableFixedReport::default();
    table_fixed_run(&[text_utf8], &[], &record, &mut image, &mut report);
    check(
        image[0..4] == 5u32.to_le_bytes(),
        "arg-v-meta: the used length 5 lands",
    );
    check(
        &image[4..9] == b"hello",
        "arg-v-meta: the five utf8 BYTES land under arm 2",
    );
    check(
        !report.malformed && report.clamped == 0 && !report.refused,
        "arg-v-meta: nothing damaged, clamped or refused",
    );

    // THE FLAVOUR LANE IS INDEPENDENT OF THE ORDINAL LANE. The SAME arm
    // ordinal (arg == 2) with a WIDE flavour: `wstring` counts u16 units, so
    // the compiler bakes unit 2 — the 8-byte span becomes 4 units in `aux`
    // while `arg` stays exactly 2. The ordinal did not move when the flavour
    // changed, which is the whole law.
    let text_wide = TableFixedEntry {
        aux: 4, // wide: 8 bytes = 4 u16 units
        ..text_utf8
    };
    // tag 2, then a wstring of 2 used units ("h\0", "e\0") in the 8-byte span:
    // 1 tag byte + 4 length bytes + 8 content bytes = 13 bytes.
    let wide_record: [u8; 13] = [2u8, 2, 0, 0, 0, b'h', 0, b'e', 0, b'l', 0, b'l', 0];
    let mut wide_image = [0u8; 12];
    let mut wide_report = TableFixedReport::default();
    table_fixed_run(
        &[text_wide],
        &[],
        &wide_record,
        &mut wide_image,
        &mut wide_report,
    );
    check(
        wide_image[0..4] == 2u32.to_le_bytes(),
        "arg-v-meta: the WIDE length lands in units (2), not bytes",
    );
    check(
        &wide_image[4..8] == &[b'h', 0, b'e', 0],
        "arg-v-meta: the first wide units land, flavour settled by `aux` alone",
    );
    check(
        text_wide.arg == 2 && text_wide.aux == 4 && text_utf8.arg == 2 && text_utf8.aux == 8,
        "arg-v-meta: arg is the ordinal and nothing else; the flavour rides in aux",
    );

    // NEGATIVE CONTROL — the OLD one-lane encoding, planted: stamp the utf8
    // flavour OVER the ordinal in the arg lane (arg := flavour's value, 1).
    // The record is still arm 2 (tag == 2), but the guard now compares the tag
    // against the FLAVOUR, and arm 2 is not the flavour — the entry no longer
    // fires, so the string under arm 2 DROPS. If the two facts still shared
    // one lane this would stay green; it is the stain a folded lane leaves.
    let mut old = text_utf8;
    old.arg = 1; // the flavour, stamped over the arm ordinal
    let mut lost = [0u8; 12];
    let mut lost_report = TableFixedReport::default();
    table_fixed_run(&[old], &[], &record, &mut lost, &mut lost_report);
    check(
        lost == [0u8; 12],
        "NEGATIVE CONTROL: flavour in the arg lane does NOT land the string under arm 2",
    );

    let failures = FAILURES.load(Ordering::Relaxed);
    assert_eq!(failures, 0, "{failures} assertion(s) failed");
}
