// THE RUST LEG OF THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3.
//
// Two things are proved here and they are the two halves of a port:
//
//   THE BYTES ARE THE REFERENCE'S. Every file this binary reads was written by
//   the C++ reference (test/tables/fixedform_dump.cpp). Reading one and saving
//   the values back has to reproduce it BYTE FOR BYTE — which reaches every
//   field, the block, the hash and every byte of declared slack, because a byte
//   this port encodes differently is a byte that does not come back.
//
//   THE VERSIONED PATH IS THE PATH. §3.4's invariant is that a record is
//   positional BY PLAN and the positions are the WRITER's block. So every
//   cross-schema case below reads a file written under ANOTHER block, through
//   the same loop over a plan compiled from it: a widened field, a rename under
//   `was`, a field this reader cannot name, a field the writer does not carry,
//   a whole nested TYPE stepped over by its block size, and an optional against
//   a value.
//
// And the NEGATIVE CONTROLS, because a test that never watched the wrong plan
// fail never checked the right one worked.
//
// THE UNION IS HERE TOO (docs/SPEC-TABLES.md §15). The reference's own paired
// bench corpus — 64 records of BenchMixed, whose `game_event` is a three-arm
// union — is read, value-checked against a straight-line decode of its own
// bytes, and saved back byte for byte; the FE1/FE2 pair slides an ARM INTO THE
// MIDDLE of a union and reads it back through a compiled plan; and a forged tag
// past the arm count is watched landing as None.
use std::fs;
use std::process::exit;
use std::sync::atomic::{AtomicU32, Ordering};

static FAILURES: AtomicU32 = AtomicU32::new(0);

fn check(ok: bool, what: &str) {
    if !ok {
        println!("FAIL: {what}");
        FAILURES.fetch_add(1, Ordering::Relaxed);
    }
}

fn slurp(dir: &str, name: &str) -> Vec<u8> {
    match fs::read(format!("{dir}/{name}")) {
        Ok(b) => b,
        Err(e) => {
            println!("FAIL: cannot read {dir}/{name}: {e}");
            exit(1);
        }
    }
}

fn records_at(data: &[u8]) -> usize {
    let h = tblfx1::TABLE_FIXED_HEADER_BYTES;
    h + 4 + u32::from_le_bytes(data[h..h + 4].try_into().expect("four bytes")) as usize
}

fn layout_of(data: &[u8]) -> &[u8] {
    let h = tblfx1::TABLE_FIXED_HEADER_BYTES;
    let n = u32::from_le_bytes(data[h..h + 4].try_into().expect("four bytes")) as usize;
    &data[h + 4..h + 4 + n]
}

// ---------------------------------------------------------------------------
// THE WRITE: read the reference's file, save it back, and the bytes must be
// identical. One macro because the shape is the same for every root and the
// only thing that moves is which crate's names it names.
// ---------------------------------------------------------------------------

macro_rules! round_trip {
    ($dir:expr, $file:expr, $krate:ident, $row:ident, $cap:expr, $load:ident, $save:ident, $measure:ident) => {{
        let golden = slurp($dir, $file);
        let mut values = [$krate::$row::default(); $cap];
        let mut plan = [$krate::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = $krate::TableFixedReport::default();
        let n = $krate::$load(
            &mut values,
            &golden,
            &mut plan,
            &mut remap,
            &mut report,
        );
        check(n.is_some(), concat!($file, ": the reference's file loads"));
        let n = n.unwrap_or(0);
        check(
            report == $krate::TableFixedReport::default(),
            concat!($file, ": its own block is the identity plan, and a clean read moves no counter"),
        );
        let mut out = vec![0u8; $krate::$measure(n)];
        let wrote = $krate::$save(&values[..n], &mut out);
        check(wrote == Some(out.len()), concat!($file, ": save fills what measure says"));
        check(out == golden, concat!($file, ": the bytes are the C++ reference's, exactly"));
        n
    }};
}

fn the_write(dir: &str) {
    let n = round_trip!(
        dir, "fx1.bin", tblfx1, FxRootRow, 8,
        fx_root_fixed_load, fx_root_fixed_save, fx_root_fixed_measure
    );
    check(n == 2, "fx1.bin: two records");
    round_trip!(
        dir, "fx2.bin", tblfx2, FxRootRow, 8,
        fx_root_fixed_load, fx_root_fixed_save, fx_root_fixed_measure
    );
    round_trip!(
        dir, "p1.bin", tblp1, ChainRow, 8,
        chain_fixed_load, chain_fixed_save, chain_fixed_measure
    );
    round_trip!(
        dir, "p3.bin", tblp3, ChainRow, 8,
        chain_fixed_load, chain_fixed_save, chain_fixed_measure
    );
    // the rich shape: keyed arrays nesting keyed arrays, an optional section,
    // strings, floats and a nested `type` that spells a keyed array of its own
    let n = round_trip!(
        dir, "keyed.bin", tabledemo, KeyedConfigRow, 8,
        keyed_config_fixed_load, keyed_config_fixed_save, keyed_config_fixed_measure
    );
    check(n == 2, "keyed.bin: two records");

    // AND THE VALUES THEMSELVES, because a read and a write that agreed with
    // each other and with nothing else would round-trip too. These are the
    // reference's own, set by hand in test/tables/fixedform_dump.cpp.
    let mut values = [tabledemo::KeyedConfigRow::default(); 8];
    let mut plan = [tabledemo::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tabledemo::TableFixedReport::default();
    let golden = slurp(dir, "keyed.bin");
    let n = tabledemo::keyed_config_fixed_load(&mut values, &golden, &mut plan, &mut remap, &mut report)
        .unwrap_or(0);
    check(n == 2, "keyed: the reference's two records");
    let v = values[1];
    check(v.teams[0].spawn_count == 14, "keyed: a slot's scalar, at the key that owns it");
    check(v.teams[1].banner[..4] == *b"blue", "keyed: a slot's string");
    check(v.teams[1].banner_length == 4, "keyed: a string's used length");
    check(v.teams[2].banner[..5] == *b"green", "keyed: the last slot's string");
    check(v.scores.per_team[2] == 3001, "keyed: a nested `type`'s own keyed array");
    check(v.hulls[2].health == 103.0, "keyed: a float in a keyed slot");
    check(v.hulls[1].turrets[2].cooldown == 0.75, "keyed: a keyed array nested in a keyed array");
    check(v.hulls[0].turrets[0].gunner_present, "keyed: an optional section that is PRESENT");
    check(!v.hulls[0].turrets[1].gunner_present, "keyed: an optional section that is ABSENT");
    check(
        v.hulls[0].turrets[1].gunner.reaction == 0.0,
        "keyed: an ABSENT optional's payload is the template's zeros (§3.4)",
    );

    // the packed corpus's root: COUNTED arrays, an enum with a declared
    // default, a fixed array of floats, and an optional section in a record
    let n = round_trip!(
        dir, "pack.bin", tabledemo, PackConfigRow, 8,
        pack_config_fixed_load, pack_config_fixed_save, pack_config_fixed_measure
    );
    check(n == 2, "pack.bin: two records");
    let mut values = [tabledemo::PackConfigRow::default(); 8];
    let mut plan = [tabledemo::TableFixedEntry::default(); 2048];
    let mut remap = [0u16; 2048];
    let mut report = tabledemo::TableFixedReport::default();
    let golden = slurp(dir, "pack.bin");
    tabledemo::pack_config_fixed_load(&mut values, &golden, &mut plan, &mut remap, &mut report);
    let v = values[0];
    check(v.version == 7, "pack: a scalar");
    check(v.global.tick_rate == 120, "pack: a nested table's scalar");
    check(v.global.difficulty == 3, "pack: an enum rides its ORDINAL, from 1");
    check(v.global.build_note[..11] == *b"first build", "pack: a nested string");
    check(v.global.spawn_delays[1] == 1.0, "pack: a fixed array of floats");
    check(v.ships[1].hardpoints_count == 2, "pack: a COUNTED array's used count");
    check(v.ships[1].hardpoints[1] == 2, "pack: a counted array's elements");
    check(v.reserves_count == 2, "pack: a counted array OF TABLES");
    check(v.reserves[1].display_name[..7] == *b"spare-b", "pack: a string inside one of its elements");
    check(!v.reserves[1].gunner_present, "pack: an absent optional inside one of its elements");
}

// ---------------------------------------------------------------------------
// THE COMPILED PLAN OVER MY OWN BLOCK
//
// §3.4's ruling is that there is ONE reader path and the only thing that
// differs is which plan it was handed. So the plan compiled from a block that
// is MINE, byte for byte, has to land exactly what the identity plan lands —
// and that is what reaches the ops the cross-schema cases below never touch:
// `count` over a counted array, `text` over a string, `ordinal` over an enum,
// and the keyed walk that matches slots by the KEY's own id.
//
// It is the same instrument the coalescer gets: two roads to one answer, with
// the answer already pinned to the C++ reference's bytes above.
// ---------------------------------------------------------------------------

macro_rules! compiled_self_plan {
    ($dir:expr, $file:expr, $krate:ident, $row:ident, $cap:expr, $load:ident, $save:ident,
     $measure:ident, $block:ident, $counted:ident, $defaults:ident, $body:ident, $scatter:ident) => {{
        let golden = slurp($dir, $file);

        // what the IDENTITY plan lands, which the round trip already pinned to
        // the reference's bytes
        let mut want = [$krate::$row::default(); $cap];
        let mut plan = [$krate::TableFixedEntry::default(); 2048];
        let mut remap = [0u16; 2048];
        let mut report = $krate::TableFixedReport::default();
        let n = $krate::$load(&mut want, &golden, &mut plan, &mut remap, &mut report).unwrap_or(0);

        // and what a plan COMPILED from the same block lands
        let theirs = $krate::TableFixedBlock::parse(layout_of(&golden));
        let mine = $krate::TableFixedBlock::parse(&$krate::$block);
        check(
            theirs.is_some() && mine.is_some(),
            concat!($file, ": the block parses on both sides"),
        );
        let mut report = $krate::TableFixedReport::default();
        let compiled = $krate::table_fixed_compile(
            &theirs.expect("their block"),
            &mine.expect("my block"),
            &$krate::$counted,
            &mut plan,
            &mut remap,
            &mut report,
        );
        check(compiled.is_some(), concat!($file, ": a plan compiles from my own block"));
        check(
            report == $krate::TableFixedReport::default(),
            concat!($file, ": a plan over MY OWN block names nothing unknown and moves nothing"),
        );
        let compiled = compiled.unwrap_or(0);
        check(
            compiled > 1,
            concat!($file, ": the compiled plan is a REAL plan — text, count, ordinal and the keyed walk break the runs"),
        );
        let rest = &golden[records_at(&golden)..];
        let record = $krate::$body + 8;
        let mut got = [$krate::$row::default(); $cap];
        for k in 0..n {
            let mut image = [0u8; $krate::$body];
            image.copy_from_slice(&$krate::$defaults);
            $krate::table_fixed_run(
                &plan[..compiled],
                &remap,
                &rest[k * record + 8..(k + 1) * record],
                &mut image,
                &mut report,
            );
            $krate::$scatter(&image, &mut got[k], &mut report);
        }
        // a Row is plain data with no PartialEq, so the comparison is the one
        // this form already trusts: the bytes the two values write
        let mut a = vec![0u8; $krate::$measure(n)];
        let mut b = vec![0u8; $krate::$measure(n)];
        $krate::$save(&want[..n], &mut a);
        $krate::$save(&got[..n], &mut b);
        check(
            a == b,
            concat!($file, ": a COMPILED plan over my own block lands exactly what the identity plan lands"),
        );
    }};
}

fn the_compiled_plan(dir: &str) {
    compiled_self_plan!(
        dir, "keyed.bin", tabledemo, KeyedConfigRow, 8,
        keyed_config_fixed_load, keyed_config_fixed_save, keyed_config_fixed_measure,
        KEYED_CONFIG_FIXED_BLOCK, KEYED_CONFIG_FIXED_COUNTED, KEYED_CONFIG_FIXED_DEFAULTS,
        KEYED_CONFIG_FIXED_BODY_BYTES, keyed_config_fixed_scatter
    );
    compiled_self_plan!(
        dir, "pack.bin", tabledemo, PackConfigRow, 8,
        pack_config_fixed_load, pack_config_fixed_save, pack_config_fixed_measure,
        PACK_CONFIG_FIXED_BLOCK, PACK_CONFIG_FIXED_COUNTED, PACK_CONFIG_FIXED_DEFAULTS,
        PACK_CONFIG_FIXED_BODY_BYTES, pack_config_fixed_scatter
    );
}

// ---------------------------------------------------------------------------
// THE VERSIONED PATH, which §3.4 says IS the path
// ---------------------------------------------------------------------------

fn an_older_writer(dir: &str) {
    // FX2 reads FX1: a WIDENED field, a RENAME under `was`, a field FX1 does
    // not carry (defaulted from the prefill), a whole nested TYPE FX1 never
    // heard of (defaulted whole), and one name FX2 cannot place.
    let golden = slurp(dir, "fx1.bin");
    let mut values = [tblfx2::FxRootRow::default(); 8];
    let mut plan = [tblfx2::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblfx2::TableFixedReport::default();
    let n = tblfx2::fx_root_fixed_load(&mut values, &golden, &mut plan, &mut remap, &mut report);
    check(n == Some(2), "older writer: both records");
    let v = values[0];
    check(v.keep == 4242, "older writer: an unmoved field");
    check(v.narrow == 40000, "WIDENED: uint16 into uint32, exactly");
    check(report.widened == 2, "WIDENED: one widened counts, per record");
    check(v.renamed_to == 321, "RENAMED: `was =` keeps the wire id");
    check(v.added == 11, "MISSING: a field the writer does not carry takes its declared default");
    check(v.extra.x == 0 && v.extra.y == 0, "MISSING: a whole nested type takes its defaults");
    check(v.nested.a == 111 && v.nested.b == 222, "older writer: the nesting");
    check(report.unknown == 1, "older writer: `gone` is the one field this reader cannot name");
    check(
        report.kind_mismatch == 0 && !report.malformed && !report.refused,
        "older writer: nothing else fired",
    );
    check(values[1].keep == 1 && values[1].narrow == 2, "older writer: the second record too");
}

fn a_newer_writer(dir: &str) {
    // FX1 reads FX2: an unknown FIELD and an unknown nested TYPE, stepped over
    // by the size their entries state — which is what puts `nested` in the
    // right place — and a NARROWING, which is a kind that MOVED.
    let golden = slurp(dir, "fx2.bin");
    let mut values = [tblfx1::FxRootRow::default(); 8];
    let mut plan = [tblfx1::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblfx1::TableFixedReport::default();
    let n = tblfx1::fx_root_fixed_load(&mut values, &golden, &mut plan, &mut remap, &mut report);
    check(n == Some(1), "newer writer: one record");
    let v = values[0];
    check(v.keep == 5150, "newer writer: an unmoved field lands past the unknowns");
    check(v.renamed == 808, "newer writer: `was =` reads the other way too");
    check(v.gone == 9, "newer writer: a field the writer dropped takes its declared default");
    check(v.nested.a == 33 && v.nested.b == 44, "newer writer: the nesting lands past the unknown type");
    check(report.unknown == 2, "newer writer: two names this reader does not have");
    check(report.kind_mismatch == 1, "newer writer: uint32 into uint16 is a kind that moved");
    check(v.narrow == 3, "newer writer: a narrowing leaves the declared default");
    check(!report.malformed && !report.refused, "newer writer: no damage and no refusal");
}

fn an_optional(dir: &str) {
    // §2.3 makes `?T` and a plain `T` nesting WIRE-IDENTICAL on form 1. ON THIS
    // FORM THEY ARE ONE BYTE APART, and §3.4 says so rather than leaving it to
    // be found: the edit reads as a kind mismatch and the field takes its
    // declared default, instead of every byte after it sliding by one.
    let golden = slurp(dir, "p1.bin");
    let mut values = [tblp3::ChainRow::default(); 8];
    let mut plan = [tblp3::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblp3::TableFixedReport::default();
    let n = tblp3::chain_fixed_load(&mut values, &golden, &mut plan, &mut remap, &mut report);
    check(n == Some(1), "optional: P3 reads P1's record");
    check(values[0].name[..9] == *b"chain-one", "optional: the field before the edit lands");
    check(report.kind_mismatch == 1, "optional: `?T` against `T` is a kind that MOVED and is seen");
    check(!values[0].link_present, "optional: the edited field takes its declared default");
    check(!report.malformed && !report.refused, "optional: no damage and no refusal");
}

// ---------------------------------------------------------------------------
// THE NEGATIVE CONTROLS. The plan is the whole of this form's safety, so a
// test that never watched a wrong plan fail is a test that never checked the
// right one worked.
// ---------------------------------------------------------------------------

fn the_negative_controls(dir: &str) {
    // 1. THE WRONG PLAN. FX1's IDENTITY plan run over an FX2 record must NOT
    //    reproduce it, while the loader — which picks its plan by the HASH —
    //    must.
    {
        let fx2 = slurp(dir, "fx2.bin");
        let body = &fx2[records_at(&fx2) + 8..];
        let mut wrong = [0u8; tblfx1::FX_ROOT_FIXED_BODY_BYTES];
        wrong.copy_from_slice(&tblfx1::FX_ROOT_FIXED_DEFAULTS);
        let mut report = tblfx1::TableFixedReport::default();
        tblfx1::table_fixed_run(&tblfx1::FX_ROOT_FIXED_PLAN, &[], body, &mut wrong, &mut report);
        let mut wrongly = tblfx1::FxRootRow::default();
        tblfx1::fx_root_fixed_scatter(&wrong, &mut wrongly, &mut report);
        check(
            wrongly.renamed != 808 || wrongly.gone != 9,
            "NEGATIVE CONTROL: FX1's identity plan over an FX2 record must NOT reproduce it",
        );
    }

    // 2. A BLOCK THAT IS NOT A BLOCK is `block_malformed`, and it is a refusal
    //    by name: nothing decoded, no counter moved.
    {
        let mut damaged = slurp(dir, "fx1.bin");
        damaged[tblfx1::TABLE_FIXED_HEADER_BYTES + 4] ^= 0xFF; // the layout's entry count, so the tree cannot close
        let mut values = [tblfx1::FxRootRow::default(); 8];
        let mut plan = [tblfx1::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tblfx1::TableFixedReport::default();
        let n = tblfx1::fx_root_fixed_load(&mut values, &damaged, &mut plan, &mut remap, &mut report);
        check(n.is_none(), "block_malformed: refused");
        check(
            report.refused && report.reason == tblfx1::TableFixedReason::BlockMalformed,
            "block_malformed: refused BY NAME",
        );
        check(
            !report.malformed && report.unknown == 0 && report.kind_mismatch == 0,
            "block_malformed: a refusal moves no counter",
        );
    }

    // 3. A FORM BYTE THIS BUILD DOES NOT CARRY is `newer_form`.
    {
        let mut future = slurp(dir, "fx1.bin");
        future[0] = 4;
        let mut values = [tblfx1::FxRootRow::default(); 8];
        let mut plan = [tblfx1::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tblfx1::TableFixedReport::default();
        let n = tblfx1::fx_root_fixed_load(&mut values, &future, &mut plan, &mut remap, &mut report);
        check(n.is_none(), "newer_form: refused");
        check(
            report.reason == tblfx1::TableFixedReason::NewerForm,
            "newer_form: refused BY NAME",
        );
    }

    // 4. A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE is `plan_too_large`:
    //    this codec never allocates, so the storage is the caller's and a block
    //    whose plan does not fit is a refusal rather than a heap.
    {
        let fx1 = slurp(dir, "fx1.bin");
        let mut values = [tblfx2::FxRootRow::default(); 8];
        let mut plan: [tblfx2::TableFixedEntry; 1] = [tblfx2::TableFixedEntry::default(); 1];
        let mut remap = [0u16; 4];
        let mut report = tblfx2::TableFixedReport::default();
        let n = tblfx2::fx_root_fixed_load(&mut values, &fx1, &mut plan, &mut remap, &mut report);
        check(n.is_none(), "plan_too_large: refused");
        check(
            report.reason == tblfx2::TableFixedReason::PlanTooLarge,
            "plan_too_large: refused BY NAME",
        );
    }

    // 5. BYTES LEFT OVER ARE `malformed`, which is §3's rule for the same
    //    reason: the two ends of the file have met.
    {
        let mut ragged = slurp(dir, "fx1.bin");
        ragged.push(0);
        let mut values = [tblfx1::FxRootRow::default(); 8];
        let mut plan = [tblfx1::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tblfx1::TableFixedReport::default();
        let n = tblfx1::fx_root_fixed_load(&mut values, &ragged, &mut plan, &mut remap, &mut report);
        check(n.is_none(), "a ragged tail: refused");
        check(report.malformed, "a ragged tail: malformed, because the two ends of the file met");
    }

    // 6. MORE RECORDS THAN THE CALLER HAS ROOM FOR: nothing is decoded.
    {
        let fx1 = slurp(dir, "fx1.bin");
        let mut values = [tblfx1::FxRootRow::default(); 1];
        let mut plan = [tblfx1::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tblfx1::TableFixedReport::default();
        let n = tblfx1::fx_root_fixed_load(&mut values, &fx1, &mut plan, &mut remap, &mut report);
        check(n.is_none(), "batch_too_large: refused");
        check(
            report.reason == tblfx1::TableFixedReason::BatchTooLarge,
            "batch_too_large: refused BY NAME",
        );
    }
}

// ---------------------------------------------------------------------------
// THE UNION, ON THE REFERENCE'S OWN BENCH CORPUS (docs/SPEC-TABLES.md §15)
//
// bench/paired/corpus/bench_fixed.bin is 64 records of BenchMixed written by
// the C++ reference, and BenchMixed carries `game_event MixedEvent` — a
// three-arm union whose pinned arm is `hit`. It is the shape this port had no
// blittable `<Name>Row` for until the `#[repr(C)]` twin landed, and it is
// therefore the one file that proves the twin: nothing on the wire moved, so
// the bytes have to come back EXACTLY, and the block has to be the corpus's
// byte for byte before any of that means anything.
//
// AND THE VALUES, from a STRAIGHT-LINE DECODE OF THE GOLDEN'S OWN BYTES at the
// offsets the C reference asserts (`BenchMixed.game_event` at body 1108, the
// arm at 1109). A read and a write that agreed with each other and with
// nothing else would round-trip too; this is the second road to the answer,
// and it shares no code with the plan or the scatter.
// ---------------------------------------------------------------------------

const BENCH_RECORDS: usize = 64;

/// The union's place in the record BODY, from the C++ reference's own
/// static_asserts (generated/bench/paired/c/FixedTableTable.h): the tag byte,
/// then the `hit` arm's four members at their declared storage widths.
const EVENT_TAG: usize = 1108;
const EVENT_ARM: usize = 1109;

fn u32_at(b: &[u8], at: usize) -> u32 {
    u32::from_le_bytes(b[at..at + 4].try_into().expect("four bytes"))
}

fn the_bench_corpus(dir: &str) {
    let golden = slurp(dir, "bench_fixed.bin");
    let vocab = slurp(dir, "bench_fixed.layout");

    // THE BLOCK IS THE CORPUS'S BLOCK, and that one comparison is what says
    // this leg speaks the form and not a near miss: the positions, the ids, the
    // kinds, the record's size and the hash every record carries are all
    // settled by these bytes.
    check(
        vocab == benchfixed::FIXED_TABLE_FIXED_BLOCK,
        "bench_fixed: this build's vocabulary block IS the corpus's, byte for byte",
    );
    check(
        benchfixed::FIXED_TABLE_FIXED_HASH == 0x32f1_c4a3_02a2_24eb,
        "bench_fixed: the pinned block hash",
    );

    let mut values = vec![benchfixed::FixedTableRow::default(); BENCH_RECORDS];
    let mut plan = vec![benchfixed::TableFixedEntry::default(); 2048];
    let mut remap = vec![0u16; 2048];
    let mut report = benchfixed::TableFixedReport::default();
    let n = benchfixed::fixed_table_fixed_load(
        &mut values,
        &golden,
        &mut plan,
        &mut remap,
        &mut report,
    );
    check(n == Some(BENCH_RECORDS), "bench_fixed: all 64 records load");
    check(
        report == benchfixed::TableFixedReport::default(),
        "bench_fixed: its own block is the identity plan, and a clean read moves no counter",
    );
    let n = n.unwrap_or(0);

    // THE BYTES ARE THE REFERENCE'S, which reaches the tag, the live arm and
    // every byte of the union's declared slack.
    let mut out = vec![0u8; benchfixed::fixed_table_fixed_measure(n)];
    let wrote = benchfixed::fixed_table_fixed_save(&values[..n], &mut out);
    check(wrote == Some(out.len()), "bench_fixed: save fills what measure says");
    check(out == golden, "bench_fixed: the bytes are the C++ reference's, exactly");

    // A REUSED TARGET ROUND-TRIPS TOO: the load owns the prefill, so nothing is
    // reset between passes — the same gate the C++ and C legs run.
    for _ in 0..2 {
        let mut again = benchfixed::TableFixedReport::default();
        let m = benchfixed::fixed_table_fixed_load(
            &mut values,
            &golden,
            &mut plan,
            &mut remap,
            &mut again,
        );
        let mut twin = vec![0u8; benchfixed::fixed_table_fixed_measure(m.unwrap_or(0))];
        benchfixed::fixed_table_fixed_save(&values[..m.unwrap_or(0)], &mut twin);
        check(twin == golden, "bench_fixed: a reused target round-trips too");
    }

    // AND THE VALUES. The straight-line decode below reads the golden's own
    // bytes at the reference's offsets; the loop compares every record's union
    // against it.
    let rest = &golden[records_at(&golden)..];
    let record = benchfixed::FIXED_TABLE_FIXED_RECORD_BYTES;
    let mut hits = 0;
    for k in 0..n {
        let body = &rest[k * record + 8..(k + 1) * record];
        let event = values[k].value.game_event;
        check(
            event.tag == body[EVENT_TAG],
            "bench_fixed: the tag landed as the record carries it",
        );
        if event.tag != 1 {
            continue; // `game_event` is STRUCTURE, pinned to `hit` in every record
        }
        hits += 1;
        // SAFETY: the tag is 1, which names `hit`; the twin's contract is that
        // the tag names the live arm, and the scatter that landed this value is
        // what set both.
        let hit = unsafe { event.arms.hit };
        check(
            hit.target_id == u32_at(body, EVENT_ARM)
                && hit.damage == u32_at(body, EVENT_ARM + 4) as i32
                && hit.hit_kind == u32_at(body, EVENT_ARM + 8) as i32
                && hit.crit == (body[EVENT_ARM + 12] != 0),
            "bench_fixed: the live arm's every member is the golden's own bytes",
        );
    }
    check(hits == BENCH_RECORDS, "bench_fixed: `hit` is the pinned arm in all 64 records");

    // THE COMPILED PLAN OVER MY OWN BLOCK, which is where the union's own ops
    // live: the runtime writes MY tag as a `const` under THEIR tag's GUARD and
    // then each arm's entries behind that guard. The identity plan is one move
    // and never touches them, so without this the union's compiler branch is
    // never run over a record whose answer is already pinned.
    compiled_self_plan!(
        dir, "bench_fixed.bin", benchfixed, FixedTableRow, BENCH_RECORDS,
        fixed_table_fixed_load, fixed_table_fixed_save, fixed_table_fixed_measure,
        FIXED_TABLE_FIXED_BLOCK, FIXED_TABLE_FIXED_COUNTED, FIXED_TABLE_FIXED_DEFAULTS,
        FIXED_TABLE_FIXED_BODY_BYTES, fixed_table_fixed_scatter
    );
}

// ---------------------------------------------------------------------------
// THE ORDINAL SLIDE: an enum and a UNION that both gained a variant in the
// middle (docs/SPEC-TABLES.md §3.4)
//
// FE2 inserts `Electrum` between Bronze and Silver, so Silver and Gold both
// slide by one; and it inserts the `shield` arm between `boost` and `ward`, so
// `ward`'s tag slides from 2 to 3 and the arm behind the tag moves with it. A
// reader that took the number for its own would read Gold as Silver and a ward
// as a shield, and NEITHER EDIT IS VISIBLE IN A RECORD'S BYTES — the ordinal is
// the only thing that carries them.
//
// §3.4's answer to both is that AN ORDINAL IS A POSITION IN THE LAYOUT and the
// plan REMAPS it BY NAME: the `ordinal` op resolves a variant through the
// plan's own table, and a union's tag is written as MY ordinal under THEIR
// tag's guard.
// ---------------------------------------------------------------------------

fn the_ordinal_slide() {
    // FE1 writes: Gold with a `ward`, and Bronze with a `boost`.
    let older = [
        tblfe1::FeRootRow {
            grade: 3, // Gold
            effect: tblfe1::EffectRow {
                tag: 2, // ward
                arms: tblfe1::EffectRowArms {
                    ward: tblfe1::WardRow { charge: 41 },
                },
            },
            tail: 99,
        },
        tblfe1::FeRootRow {
            grade: 1, // Bronze
            effect: tblfe1::EffectRow {
                tag: 1, // boost
                arms: tblfe1::EffectRowArms {
                    boost: tblfe1::BoostRow { power: 17 },
                },
            },
            tail: 100,
        },
    ];
    let mut slide = vec![0u8; tblfe1::fe_root_fixed_measure(older.len())];
    check(
        tblfe1::fe_root_fixed_save(&older, &mut slide) == Some(slide.len()),
        "the slide: FE1 writes its two records",
    );

    let mut values = [tblfe2::FeRootRow::default(); 4];
    let mut plan = [tblfe2::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblfe2::TableFixedReport::default();
    let n = tblfe2::fe_root_fixed_load(&mut values, &slide, &mut plan, &mut remap, &mut report);
    check(n == Some(2), "the slide: FE2 reads both of FE1's records");
    check(values[0].grade == 4, "the slide: the enum's Gold slid 3 -> 4 and was REMAPPED");
    check(values[0].effect.tag == 3, "the slide: the union's `ward` tag slid 2 -> 3");
    // SAFETY: the tag is 3, which is FE2's `ward`; the scatter set both.
    check(
        unsafe { values[0].effect.arms.ward }.charge == 41,
        "the slide: and the ARM BEHIND the slid tag landed",
    );
    check(
        values[1].grade == 1 && values[1].effect.tag == 1,
        "the slide: Bronze did not slide, and neither did the `boost` arm",
    );
    // SAFETY: the tag is 1, which is FE2's `boost`.
    check(
        unsafe { values[1].effect.arms.boost }.power == 17,
        "the slide: the unslid arm's payload",
    );
    check(
        values[0].tail == 99 && values[1].tail == 100,
        "the slide: the field past the union is undisturbed",
    );
    check(report.kind_mismatch == 0, "the slide: a slide is not a `kind_mismatch`");
    check(
        report.unknown == 0,
        "the slide: `shield` is a field this WRITER does not carry, so nothing counts",
    );
    check(
        report.clamped == 0 && !report.malformed && !report.refused,
        "the slide: a slide does not damage and does not clamp",
    );

    // AND BACK: FE1 reading FE2. `Electrum` and `shield` are variants this
    // reader has no name for, so the ordinal resolves to NONE and the arm is
    // simply never a source.
    let newer = [tblfe2::FeRootRow {
        grade: 2, // Electrum, which FE1 cannot name
        effect: tblfe2::EffectRow {
            tag: 2, // shield, which FE1 cannot name
            arms: tblfe2::EffectRowArms {
                shield: tblfe2::ShieldRow { plating: 5 },
            },
        },
        tail: 8,
    }];
    let mut back = vec![0u8; tblfe2::fe_root_fixed_measure(newer.len())];
    tblfe2::fe_root_fixed_save(&newer, &mut back);

    let mut values = [tblfe1::FeRootRow::default(); 4];
    let mut plan = [tblfe1::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblfe1::TableFixedReport::default();
    let n = tblfe1::fe_root_fixed_load(&mut values, &back, &mut plan, &mut remap, &mut report);
    check(n == Some(1), "coming back: FE1 reads FE2's record");
    check(values[0].grade == 0, "coming back: a variant this reader cannot name resolves to None");
    check(
        values[0].effect.tag == 0,
        "coming back: an ARM this reader cannot name leaves the tag at None",
    );
    check(values[0].tail == 8, "coming back: and the field past it still lands");
    check(
        report.clamped == 0 && !report.malformed && !report.refused,
        "coming back: nothing was damaged and nothing was clamped",
    );
}

// ---------------------------------------------------------------------------

fn the_union_controls(dir: &str) {
    // A TAG PAST THE ARM COUNT LANDS AS NONE AND COUNTS ONE CLAMPED, which is
    // the ruling this form takes on a forged ordinal: the identity plan copies
    // the tag verbatim out of a stranger's record, so the scatter is where the
    // range is closed — the same place a count's bound is closed.
    let golden = slurp(dir, "bench_fixed.bin");
    let head = records_at(&golden);
    let record = benchfixed::FIXED_TABLE_FIXED_RECORD_BYTES;
    let mut forged = golden[..head + record].to_vec();
    forged[head + 8 + EVENT_TAG] = 250; // no arm of this build is the 250th

    let mut values = vec![benchfixed::FixedTableRow::default(); 1];
    let mut plan = vec![benchfixed::TableFixedEntry::default(); 2048];
    let mut remap = vec![0u16; 2048];
    let mut report = benchfixed::TableFixedReport::default();
    let n = benchfixed::fixed_table_fixed_load(
        &mut values,
        &forged,
        &mut plan,
        &mut remap,
        &mut report,
    );
    check(n == Some(1), "a forged tag: the record still reads");
    check(
        values[0].value.game_event.tag == 0,
        "a forged tag: a tag past the arm count lands as None",
    );
    check(report.clamped == 1, "a forged tag: and it counts ONE clamped");
    check(
        !report.malformed && !report.refused && report.kind_mismatch == 0,
        "a forged tag: nothing else fired — an out-of-range ordinal is not damage",
    );
    check(
        values[0].value.sequence != 0,
        "a forged tag: every field beside the union still landed",
    );

    // AND ON THE COMPILED PATH. §3.4's ruling is that there is ONE reader path,
    // so a forged tag has to land as None there too — and it does, by a
    // different mechanism worth stating: the compiler writes MY ordinal as a
    // `const` under THEIR tag's GUARD, so a tag that matches no arm of the
    // writer's fires no entry at all and the image keeps the prefill's zero.
    // Nothing is out of range by the time the scatter sees it, which is why
    // this case lands None and counts NOTHING, where the identity path above
    // counts one clamped.
    {
        let older = [tblfe1::FeRootRow {
            grade: 1,
            effect: tblfe1::EffectRow {
                tag: 2, // ward
                arms: tblfe1::EffectRowArms {
                    ward: tblfe1::WardRow { charge: 41 },
                },
            },
            tail: 77,
        }];
        let mut file = vec![0u8; tblfe1::fe_root_fixed_measure(1)];
        tblfe1::fe_root_fixed_save(&older, &mut file);
        // FE1's body is `grade`, then the union, then `tail`: the tag leads the
        // union storage, so it is the byte after `grade`.
        let tag_at = records_at(&file) + 8 + 1;
        file[tag_at] = 250; // no arm of FE1's is the 250th either

        let mut values = [tblfe2::FeRootRow::default(); 4];
        let mut plan = [tblfe2::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tblfe2::TableFixedReport::default();
        let n =
            tblfe2::fe_root_fixed_load(&mut values, &file, &mut plan, &mut remap, &mut report);
        check(n == Some(1), "a forged tag, compiled plan: the record still reads");
        check(
            values[0].effect.tag == 0,
            "a forged tag, compiled plan: a tag no arm of the WRITER's names lands as None",
        );
        check(
            values[0].tail == 77,
            "a forged tag, compiled plan: the field past the union still lands",
        );
        check(
            !report.malformed && !report.refused,
            "a forged tag, compiled plan: no damage and no refusal",
        );
    }

    // AND THE NEGATIVE CONTROL FOR THE CONTROL: the UNFORGED record's tag is
    // the pinned arm, so the clamp above is the forgery and not the reader.
    let mut clean = benchfixed::TableFixedReport::default();
    benchfixed::fixed_table_fixed_load(
        &mut values,
        &golden[..head + record],
        &mut plan,
        &mut remap,
        &mut clean,
    );
    check(
        values[0].value.game_event.tag == 1 && clean.clamped == 0,
        "NEGATIVE CONTROL: the same record unforged lands `hit` and clamps nothing",
    );
}

// ---------------------------------------------------------------------------
// TEXT UNDER A UNION ARM (docs/SPEC-TABLES.md §3.4, §15) — test/tables/FU1 and
// FU2, and the hole they were written for.
//
// A PLAN ENTRY FOR A TEXT FIELD INSIDE A UNION ARM HAS TO CARRY TWO FACTS AT
// ONCE: which arm's tag it is guarded by, and which flavour of text it moves.
// A port that spends ONE LANE on both loses whichever it wrote second, and the
// loss is silent — a `string(N)` in a union's SECOND arm read as '' through a
// compiled plan while the identity read landed the text, with no counter and
// no refusal (reference-fix 12, found on the Dart leg). Nothing else in the
// corpus puts text inside an arm, so nothing else could have caught it.
//
// THIS PORT SPENDS TWO: the flavour is settled at COMPILE time out of the
// layout's own kind — 12 utf8, 33 wide, 34 bytes — into the entry's `size` and
// `aux`, and `arg` carries the arm ordinal and nothing else. The check below is
// what holds it there, and it is the one shape that would go red if a later
// refactor folded the two back into one lane: the two reads have to agree
// FIELD FOR FIELD, and the text is a field.
// ---------------------------------------------------------------------------

fn the_text_under_an_arm() {
    // FU1 writes: the LABELLED arm — the union's SECOND — with a scalar either
    // side of the text, and the `plain` arm beside it so both are exercised.
    let older = [
        tblfu1::FuRootRow {
            flag: true,
            note: 44,
            note_present: true,
            pick: tblfu1::PickRow {
                tag: 2, // labelled
                arms: tblfu1::PickRowArms {
                    labelled: tblfu1::LabelledRow {
                        lead: 101,
                        label: *b"hello\0\0\0\0",
                        label_length: 5,
                        trail: 202,
                    },
                },
            },
            tail: 11,
        },
        tblfu1::FuRootRow {
            flag: false,
            note: 0,
            note_present: false,
            pick: tblfu1::PickRow {
                tag: 1, // plain
                arms: tblfu1::PickRowArms {
                    plain: tblfu1::PlainRow { n: 303 },
                },
            },
            tail: 12,
        },
    ];
    let mut file = vec![0u8; tblfu1::fu_root_fixed_measure(older.len())];
    check(
        tblfu1::fu_root_fixed_save(&older, &mut file) == Some(file.len()),
        "text under an arm: FU1 writes its two records",
    );

    // THE IDENTITY READ, which is the one that was right all along.
    let mut mine = [tblfu1::FuRootRow::default(); 4];
    let mut plan = [tblfu1::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblfu1::TableFixedReport::default();
    let n = tblfu1::fu_root_fixed_load(&mut mine, &file, &mut plan, &mut remap, &mut report);
    check(n == Some(2), "text under an arm: the identity read takes both records");
    check(
        report == tblfu1::TableFixedReport::default(),
        "text under an arm: its own layout is the identity plan, and it moves no counter",
    );
    // SAFETY: the tag is 2, which names `labelled`; the scatter set both.
    let id_arm = unsafe { mine[0].pick.arms.labelled };
    check(
        mine[0].pick.tag == 2 && id_arm.label_length == 5 && &id_arm.label[..5] == b"hello",
        "text under an arm: the IDENTITY read lands the text",
    );
    check(
        mine[0].flag && mine[0].note_present && mine[0].note == 44,
        "text under an arm: and the bool and the optional that lead the body",
    );

    // AND THE COMPILED READ, of the same bytes through a peer whose layout hash
    // differs — which is the only thing that changes.
    let mut theirs = [tblfu2::FuRootRow::default(); 4];
    let mut plan2 = [tblfu2::TableFixedEntry::default(); 512];
    let mut remap2 = [0u16; 512];
    let mut report2 = tblfu2::TableFixedReport::default();
    let n2 = tblfu2::fu_root_fixed_load(&mut theirs, &file, &mut plan2, &mut remap2, &mut report2);
    check(n2 == Some(2), "text under an arm: the compiled read takes both records");
    check(
        !report2.malformed && !report2.refused && report2.clamped == 0
            && report2.kind_mismatch == 0,
        "text under an arm: and nothing was damaged, clamped or refused",
    );

    // FIELD FOR FIELD, which is the check the bug walked through: a compiled
    // read that landed '' while the identity read landed 'hello' passed every
    // other assertion in this file.
    // SAFETY: both tags are 2, which names `labelled` in both builds.
    let arm = unsafe { theirs[0].pick.arms.labelled };
    check(theirs[0].pick.tag == 2, "text under an arm: the tag agrees");
    check(arm.lead == id_arm.lead && arm.lead == 101, "text under an arm: the scalar BEFORE the text agrees");
    check(
        arm.label_length == id_arm.label_length && arm.label_length == 5,
        "text under an arm: the LENGTH agrees",
    );
    check(
        arm.label == id_arm.label && &arm.label[..5] == b"hello",
        "text under an arm: THE TEXT ITSELF agrees — every byte of the buffer",
    );
    check(
        arm.trail == id_arm.trail && arm.trail == 202,
        "text under an arm: the scalar AFTER the text agrees",
    );
    check(theirs[0].tail == mine[0].tail && theirs[0].tail == 11, "text under an arm: the field past the union agrees");
    check(
        theirs[0].extra == 11,
        "text under an arm: the field FU1 does not carry took its declared default",
    );

    // THE FIRST ARM TOO, because a lane that holds the arm ordinal is right by
    // accident for ordinal 1 and wrong from 2 up.
    // SAFETY: both tags are 1, which names `plain`.
    check(
        theirs[1].pick.tag == 1
            && unsafe { theirs[1].pick.arms.plain }.n == unsafe { mine[1].pick.arms.plain }.n
            && unsafe { theirs[1].pick.arms.plain }.n == 303,
        "text under an arm: the FIRST arm agrees as well",
    );
    check(theirs[1].tail == 12, "text under an arm: and its tail");

    // ---------------------------------------------------------------------
    // AND THE TWO BYTES THAT ARE TRUE WHEN THEY ARE NOT ZERO (§3.4).
    //
    // A `bool` and an optional's PRESENCE each ride as one byte, and the
    // ruling on both is `!= 0` and not `== 1`: a writer that spells `true` 2,
    // or presence 7, is a writer this reader still understands, and neither
    // is damage, a clamp or a refusal. FU1's body leads with those two bytes
    // exactly so the forgery below is at a known offset, and both reader
    // paths answer for them — the identity plan copies the byte verbatim out
    // of a stranger's record, and the compiled plan copies it verbatim too,
    // so the scatter is the one place the ruling can live.
    // ---------------------------------------------------------------------
    let body0 = records_at(&file) + 8;
    let mut forged = file.clone();
    forged[body0] = 2; // `flag`, spelled true the way a stranger spelled it
    forged[body0 + 1] = 7; // `note`'s present byte, likewise

    let mut mine2 = [tblfu1::FuRootRow::default(); 4];
    let mut r3 = tblfu1::TableFixedReport::default();
    let n3 = tblfu1::fu_root_fixed_load(&mut mine2, &forged, &mut plan, &mut remap, &mut r3);
    check(n3 == Some(2), "stray true bytes: the identity read still takes both records");
    check(mine2[0].flag, "stray true bytes: a bool byte of 2 lands TRUE on the identity path");
    check(
        mine2[0].note_present && mine2[0].note == 44,
        "stray true bytes: a present byte of 7 lands PRESENT, and the payload with it",
    );
    check(
        r3 == tblfu1::TableFixedReport::default(),
        "stray true bytes: and neither is damage, a clamp or a refusal",
    );

    let mut theirs2 = [tblfu2::FuRootRow::default(); 4];
    let mut r4 = tblfu2::TableFixedReport::default();
    let n4 = tblfu2::fu_root_fixed_load(&mut theirs2, &forged, &mut plan2, &mut remap2, &mut r4);
    check(n4 == Some(2), "stray true bytes: the compiled read still takes both records");
    check(theirs2[0].flag, "stray true bytes: a bool byte of 2 lands TRUE on the COMPILED path too");
    check(
        theirs2[0].note_present && theirs2[0].note == 44,
        "stray true bytes: and a present byte of 7 lands PRESENT there as well",
    );
    check(
        !r4.malformed && !r4.refused && r4.clamped == 0 && r4.kind_mismatch == 0,
        "stray true bytes: the compiled path calls neither of them an event either",
    );

    // NEGATIVE CONTROL FOR THE CONTROL: the SAME bytes with those two zeroed
    // land false and absent, so the reads above are the forgery answering and
    // not a reader that says `true` to everything.
    let mut off = file.clone();
    off[body0] = 0;
    off[body0 + 1] = 0;
    let mut mine3 = [tblfu1::FuRootRow::default(); 4];
    let mut r5 = tblfu1::TableFixedReport::default();
    tblfu1::fu_root_fixed_load(&mut mine3, &off, &mut plan, &mut remap, &mut r5);
    check(
        !mine3[0].flag && !mine3[0].note_present,
        "NEGATIVE CONTROL: a zero byte is still false, and a zero present byte still absent",
    );
}

// ---------------------------------------------------------------------------
// AN ENUM ORDINAL PAST THE DECLARED EXTENT (docs/SPEC-TABLES.md §3.4, §4.2).
//
// The ruling is the union tag's, and for the same reason: an ordinal out of
// range LANDS AS NONE AND COUNTS ONE CLAMPED rather than arriving as a number
// this build has no variant for. Both reader paths answer it, by the two
// mechanisms this form has — the scatter closes the range on the identity path,
// where the plan copies the ordinal verbatim out of a stranger's record, and the
// `ordinal` op closes it on the compiled path, where the number is resolved
// through the plan's own table.
// ---------------------------------------------------------------------------

fn the_enum_extent() {
    let older = [tblfe1::FeRootRow {
        grade: 3, // Gold
        effect: tblfe1::EffectRow {
            tag: 1, // boost
            arms: tblfe1::EffectRowArms {
                boost: tblfe1::BoostRow { power: 17 },
            },
        },
        tail: 55,
    }];
    let mut file = vec![0u8; tblfe1::fe_root_fixed_measure(1)];
    tblfe1::fe_root_fixed_save(&older, &mut file);
    let grade_at = records_at(&file) + 8; // FE1's body leads with `grade`
    let mut forged = file.clone();
    forged[grade_at] = 99; // FE1 declares three variants; nothing is the 99th

    // THE IDENTITY PATH.
    let mut values = [tblfe1::FeRootRow::default(); 4];
    let mut plan = [tblfe1::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblfe1::TableFixedReport::default();
    let n = tblfe1::fe_root_fixed_load(&mut values, &forged, &mut plan, &mut remap, &mut report);
    check(n == Some(1), "a forged ordinal: the record still reads");
    check(
        values[0].grade == 0,
        "a forged ordinal: an ordinal past the last variant lands as None",
    );
    check(report.clamped == 1, "a forged ordinal: and it counts ONE clamped");
    check(
        !report.malformed && !report.refused && report.kind_mismatch == 0,
        "a forged ordinal: nothing else fired — an out-of-range ordinal is not damage",
    );
    check(
        values[0].tail == 55 && values[0].effect.tag == 1,
        "a forged ordinal: every field beside it still landed",
    );

    // THE COMPILED PATH, over the same forged bytes.
    let mut theirs = [tblfe2::FeRootRow::default(); 4];
    let mut plan2 = [tblfe2::TableFixedEntry::default(); 512];
    let mut remap2 = [0u16; 512];
    let mut report2 = tblfe2::TableFixedReport::default();
    let n2 = tblfe2::fe_root_fixed_load(&mut theirs, &forged, &mut plan2, &mut remap2, &mut report2);
    check(n2 == Some(1), "a forged ordinal, compiled plan: the record still reads");
    check(
        theirs[0].grade == 0,
        "a forged ordinal, compiled plan: it lands as None there too",
    );
    check(
        report2.clamped == 1,
        "a forged ordinal, compiled plan: and counts ONE clamped, not two",
    );
    check(
        theirs[0].tail == 55,
        "a forged ordinal, compiled plan: the field past it still lands",
    );

    // AND THE NEGATIVE CONTROL FOR THE CONTROL: the same record UNFORGED reads
    // Gold and clamps nothing, so the clamp above is the forgery and not the
    // reader. A variant this reader cannot name is a DIFFERENT event — it
    // resolves to None through the table and counts nothing, which
    // `the_ordinal_slide` above pins coming back from FE2.
    let mut clean = tblfe1::TableFixedReport::default();
    let mut back = [tblfe1::FeRootRow::default(); 4];
    tblfe1::fe_root_fixed_load(&mut back, &file, &mut plan, &mut remap, &mut clean);
    check(
        back[0].grade == 3 && clean.clamped == 0,
        "NEGATIVE CONTROL: the same record unforged lands Gold and clamps nothing",
    );
}

// ---------------------------------------------------------------------------
// THE PREFILL IS THE BYTES THE PLAN DOES NOT WRITE.
//
// Identity's hole list is empty because the identity plan is one Copy of the
// whole body; a compiled plan that leaves a field out has that field in the
// list. There is no identity flag in the load loop. The negative control
// sabotages the identity plan so it leaves a hole: if that hole did not take
// the declared default, the empty-list claim would be false.
// ---------------------------------------------------------------------------

fn the_prefill_holes(dir: &str) {
    const BODY: usize = tblfx1::FX_ROOT_FIXED_BODY_BYTES;
    let mut cover = [0u8; BODY];
    let mut hole_buf = [tblfx1::TableFixedHole::default(); BODY];

    let n = tblfx1::table_fixed_holes(&tblfx1::FX_ROOT_FIXED_PLAN, &mut cover, &mut hole_buf);
    check(n == 0, "identity: the hole list is empty — the plan writes every byte");

    // IDENTITY DIRTY DEST STILL ROUND-TRIPS. Holes empty means the plan
    // overwrites the whole image, then the scatter overwrites the whole value.
    {
        let golden = slurp(dir, "fx1.bin");
        let mut values = [tblfx1::FxRootRow::default(); 8];
        for v in &mut values {
            v.keep = 0xFFFF_FFFF;
            v.gone = 12345;
            v.renamed = -99;
            v.narrow = 0xFFFF;
        }
        let mut plan = [tblfx1::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tblfx1::TableFixedReport::default();
        let n = tblfx1::fx_root_fixed_load(&mut values, &golden, &mut plan, &mut remap, &mut report)
            .unwrap_or(0);
        check(n == 2, "identity dirty dest: both records load");
        let mut out = vec![0u8; tblfx1::fx_root_fixed_measure(n)];
        tblfx1::fx_root_fixed_save(&values[..n], &mut out);
        check(out == golden, "identity dirty dest: holes empty, so the plan overwrites everything");
    }

    // COMPILED MISSING FIELD KEEPS THE DECLARED DEFAULT, and the dest was
    // poisoned so leftover dest cannot stand in for the hole.
    {
        let golden = slurp(dir, "fx1.bin");
        let mut values = [tblfx2::FxRootRow::default(); 8];
        for v in &mut values {
            v.added = 999;
            v.extra.x = 77;
            v.extra.y = 88;
        }
        let mut plan = [tblfx2::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tblfx2::TableFixedReport::default();
        let n = tblfx2::fx_root_fixed_load(&mut values, &golden, &mut plan, &mut remap, &mut report);
        check(n == Some(2), "compiled missing field: both records load");
        check(
            values[0].added == 11,
            "compiled missing field: a field the writer does not carry keeps its declared default",
        );
        check(
            values[0].extra.x == 0 && values[0].extra.y == 0,
            "compiled missing field: a whole nested type the writer does not carry keeps its defaults",
        );
        check(values[0].keep == 4242, "compiled missing field: an unmoved field still lands");
    }

    // NEGATIVE CONTROL: a sabotaged identity plan that leaves a hole must take
    // the default, or the empty-list claim is false.
    {
        let mut sab = tblfx1::FX_ROOT_FIXED_PLAN;
        check(
            sab[0].size as usize == BODY && sab[0].size >= 4,
            "NEGATIVE CONTROL: the identity plan is one Copy of the whole body",
        );
        sab[0].size -= 4;
        let n = tblfx1::table_fixed_holes(&sab, &mut cover, &mut hole_buf);
        check(n == 1, "NEGATIVE CONTROL: a sabotaged identity plan that leaves a hole has a non-empty list");
        check(
            hole_buf[0].off == BODY as u32 - 4 && hole_buf[0].size == 4,
            "NEGATIVE CONTROL: the hole is exactly the four bytes the sabotaged Copy no longer writes",
        );

        let golden = slurp(dir, "fx1.bin");
        let rec = records_at(&golden);
        let body = &golden[rec + 8..rec + 8 + BODY];
        check(
            body[BODY - 4..] != tblfx1::FX_ROOT_FIXED_DEFAULTS[BODY - 4..],
            "NEGATIVE CONTROL: the fixture's last four bytes are not already the default",
        );

        let mut image = [0xAAu8; BODY];
        for h in &hole_buf[..n] {
            let o = h.off as usize;
            let z = h.size as usize;
            image[o..o + z].copy_from_slice(&tblfx1::FX_ROOT_FIXED_DEFAULTS[o..o + z]);
        }
        let mut report = tblfx1::TableFixedReport::default();
        tblfx1::table_fixed_run(&sab, &[], body, &mut image, &mut report);
        check(
            image[BODY - 4..] == tblfx1::FX_ROOT_FIXED_DEFAULTS[BODY - 4..],
            "NEGATIVE CONTROL: the hole takes the declared default, not the dirty dest and not the record",
        );
        check(
            image[BODY - 4..] != [0xAA, 0xAA, 0xAA, 0xAA],
            "NEGATIVE CONTROL: the dirty dest did not survive in the hole",
        );
    }
}

// THE SLACK IS ZERO (docs/SPEC-TABLES.md §3.4), and this is the Rust twin of
// the reference's slack_case. A `string(N)` shorter than N, a `bytes(N)`
// shorter than N, and a `[..N]T` with unused slots are DECLARED bytes carrying
// no value: what rides in them is the TEMPLATE'S ZEROS, never whatever this
// writer's storage held past the used length or the live count.
//
// THE CONTROL IS THE STAIN: the storage past the used length and the live
// count is filled with a byte a clean record carries nowhere, the test proves
// the stain IS in the storage, then proves the WIRE carries none of it, then
// proves a whole-span copy of the same storage WOULD have carried it. There is
// one writer; identity and compiled both call it.
fn the_write_slack() {
    let mut v = tblfx1::FxRootRow::default();
    v.keep = 11;
    v.narrow = 22;
    v.renamed = 33;
    v.gone = 44;
    v.nested.a = 55;
    v.nested.b = 66;
    v.label = [0xAA; 9];
    v.label[0] = b'h';
    v.label[1] = b'i';
    v.label_length = 2;
    v.marks = [0x5A5A5A5A; 4];
    v.marks[0] = 7;
    v.marks_count = 1;
    v.blob = [0x11; 6];
    v.blob[0] = 0xDE;
    v.blob[1] = 0xAD;
    v.blob_length = 2;

    check(
        v.label[2] == 0xAA,
        "CONTROL: the text slack really is stained in storage",
    );
    check(
        v.marks[1] == 0x5A5A5A5A,
        "CONTROL: the array slack really is stained in storage",
    );
    check(
        v.blob[2] == 0x11,
        "CONTROL: the bytes slack really is stained in storage",
    );

    let mut file = vec![0u8; tblfx1::fx_root_fixed_measure(1)];
    let wrote = tblfx1::fx_root_fixed_save(&[v], &mut file);
    check(wrote == Some(file.len()), "slack: the record saves");
    let body_at = tblfx1::TABLE_FIXED_HEADER_BYTES + 4 + tblfx1::FX_ROOT_FIXED_BLOCK.len() + 8;
    let body = &file[body_at..body_at + tblfx1::FX_ROOT_FIXED_BODY_BYTES];
    check(
        !body.contains(&0xAA),
        "SLACK IS ZERO: not one stained TEXT byte reached the wire",
    );
    check(
        !body.contains(&0x5A),
        "SLACK IS ZERO: not one stained ARRAY byte reached the wire",
    );
    check(
        !body.contains(&0x11),
        "SLACK IS ZERO: not one stained BYTES byte reached the wire",
    );

    // NEGATIVE CONTROL: the same storage copied WHOLE — which is what the
    // writer did before this fix — carries the stain, so the checks above
    // discriminate and are not passing for some other reason.
    check(
        v.label.contains(&0xAA),
        "NEGATIVE CONTROL: a whole-span copy WOULD have carried the text stain",
    );
    check(
        v.marks[3] == 0x5A5A5A5A,
        "NEGATIVE CONTROL: a whole-span copy WOULD have carried the array stain",
    );
    check(
        v.blob[5] == 0x11,
        "NEGATIVE CONTROL: a whole-span copy WOULD have carried the bytes stain",
    );

    let mut back = [tblfx1::FxRootRow::default(); 1];
    let mut plan = [tblfx1::TableFixedEntry::default(); 512];
    let mut remap = [0u16; 512];
    let mut report = tblfx1::TableFixedReport::default();
    let n = tblfx1::fx_root_fixed_load(&mut back, &file, &mut plan, &mut remap, &mut report);
    check(n == Some(1), "slack: the record reads");
    check(
        back[0].label_length == 2
            && back[0].label[0] == b'h'
            && back[0].label[1] == b'i'
            && back[0].label[2] == 0,
        "slack: the used length reads, and the buffer terminates at it",
    );
    check(
        back[0].marks_count == 1 && back[0].marks[0] == 7,
        "slack: the live count reads",
    );
    check(
        back[0].marks[1] == 0 && back[0].marks[2] == 0 && back[0].marks[3] == 0,
        "slack: an unused slot lands as the wire's zero and not as some writer's leftover",
    );
    check(
        back[0].blob_length == 2 && back[0].blob[0] == 0xDE && back[0].blob[1] == 0xAD,
        "slack: the live bytes length reads",
    );
    check(
        back[0].blob[2] == 0 && back[0].blob[3] == 0 && back[0].blob[4] == 0 && back[0].blob[5] == 0,
        "slack: unused bytes land as the wire's zero",
    );
    check(
        report == tblfx1::TableFixedReport::default(),
        "slack: a clean read moves no counter",
    );
}

fn main() {
    let mut args = std::env::args().skip(1);
    let dir = match args.next() {
        Some(d) => d,
        None => {
            println!("usage: rust-fixedform <corpus dir> <bench paired corpus dir>");
            exit(1);
        }
    };
    let bench = match args.next() {
        Some(d) => d,
        None => {
            println!("usage: rust-fixedform <corpus dir> <bench paired corpus dir>");
            exit(1);
        }
    };
    the_write(&dir);
    the_write_slack();
    the_compiled_plan(&dir);
    the_prefill_holes(&dir);
    an_older_writer(&dir);
    a_newer_writer(&dir);
    an_optional(&dir);
    the_bench_corpus(&bench);
    the_ordinal_slide();
    the_text_under_an_arm();
    the_enum_extent();
    the_negative_controls(&dir);
    the_union_controls(&bench);
    let failures = FAILURES.load(Ordering::Relaxed);
    if failures != 0 {
        println!("rust fixed form: {failures} FAILED");
        exit(1);
    }
    println!(
        "rust fixed form: the bytes are the C++ reference's — the union corpus with them — and the versioned path is the path"
    );
}
