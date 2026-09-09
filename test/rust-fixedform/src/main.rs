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
        v.hulls[0].turrets[1].gunner.reaction == 0.3,
        "keyed: an ABSENT optional's payload rides WHOLE all the same (§3.4)",
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
        let block_bytes =
            u32::from_le_bytes(golden[1..5].try_into().expect("four bytes")) as usize;
        let theirs = $krate::TableFixedBlock::parse(&golden[5..5 + block_bytes]);
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
        let rest = &golden[5 + block_bytes..];
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
        let block_bytes =
            u32::from_le_bytes(fx2[1..5].try_into().expect("four bytes")) as usize;
        let body = &fx2[5 + block_bytes + 8..];
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
        damaged[5] ^= 0xFF; // the entry count, so the tree cannot close
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

fn main() {
    let dir = match std::env::args().nth(1) {
        Some(d) => d,
        None => {
            println!("usage: rust-fixedform <corpus dir>");
            exit(1);
        }
    };
    the_write(&dir);
    the_compiled_plan(&dir);
    an_older_writer(&dir);
    a_newer_writer(&dir);
    an_optional(&dir);
    the_negative_controls(&dir);
    let failures = FAILURES.load(Ordering::Relaxed);
    if failures != 0 {
        println!("rust fixed form: {failures} FAILED");
        exit(1);
    }
    println!("rust fixed form: the bytes are the C++ reference's, and the versioned path is the path");
}
