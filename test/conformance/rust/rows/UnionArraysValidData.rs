// UnionArraysValidData.rs — rust/UnionArraysValidData: Arrays of unions:
// valid-data write/read acceptance, the assertion that proves it.
//
// Law: docs/FIXED-FORM-ALGORITHM.md:1081; contract: §2.6; §3.4 [N]T / [Min..Max]T.
// A union rides the fixed form as a 1-byte tag followed by the live arm's
// payload (§3.4). An array of records that carry a union writes the count
// companion then N elements, each with its union tag and the arm it names.
// The write and the read differ only in direction, same field offsets, same
// row — so a valid array of unions written and read back lands every tag and
// every payload byte.
//
// The vector is constructed here from the law: RootConfigRow from
// tabledemo, which carries `weapons [..8]WeaponConfig` — a counted array
// where each element is a WeaponConfigRow whose `effect` is an EffectRow
// union (arms: buff/BuffRow, debuff/DebuffRow). The write goes through
// root_config_fixed_write_body → weapon_config_fixed_write_body →
// effect_fixed_write_body, and the read reverses it. Two weapons with
// DIFFERENT union arms exercise both the array's count lane and the union's
// tag-match dispatch.
//
// The negative control rewrites the saved bytes so weapon 0's union tag is
// 0 (None) and verifies the read lands None — proving the tag is the fact
// that selects the arm. THE LAW MOVED under it (docs/rulings.json
// fixed-dead-bytes-are-zero, 2026-11-10): tag 0 makes the arm's bytes DEAD,
// so the buff payload still on the wire refuses the whole load as wire
// damage; the "tag selects the arm" half is now shown with the dead payload
// zeroed too. And strict-only-read (docs/rulings.json): the second control's
// flipped byte puts `amount` past its declared max 100, which refuses the
// load out_of_range instead of reaching the value; an in-range flip still
// reaches it.
//
//   rustc --edition 2021 --test \
//       --extern tabledemo=<tabledemo rlib> -L <tabledemo deps dir> \
//       test/conformance/rust/rows/UnionArraysValidData.rs \
//       -o build/rows-rust-UnionArraysValidData \
//       && ./build/rows-rust-UnionArraysValidData
//
// Exit 0 green, 1 red; one printed line per assertion.

use tabledemo::{
    root_config_fixed_load, root_config_fixed_measure, root_config_fixed_save,
    BuffRow, DebuffRow, RootConfigRow, TableFixedEntry, TableFixedReason, TableFixedReport,
    TABLE_FIXED_HEADER_BYTES, ROOT_CONFIG_FIXED_BLOCK,
};

static FAILURES: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0);

fn check(ok: bool, what: &str) {
    if ok {
        println!("ok - {what}");
    } else {
        println!("FAIL - {what}");
        FAILURES.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    }
}

// Body offsets derived from the generator: body bytes are 1248 total
// (ROOT_CONFIG_FIXED_BODY_BYTES). The weapons array starts at body+24, each
// weapon is 22 bytes, and a weapon's effect union starts at byte 17.
const WEAPON_BODY_SIZE: usize = 22;
const WEAPON_EFFECT_OFFSET: usize = 17;
const ROOT_WEAPONS_BODY_OFFSET: usize = 24;

fn effect_tag_in_body(_body: &[u8], weapon_index: usize) -> usize {
    ROOT_WEAPONS_BODY_OFFSET + weapon_index * WEAPON_BODY_SIZE + WEAPON_EFFECT_OFFSET
}

fn file_body_offset() -> usize {
    TABLE_FIXED_HEADER_BYTES + 4 + ROOT_CONFIG_FIXED_BLOCK.len() + 8
}

#[test]
fn test_union_arrays_valid_data_write_read() {
    // ---- BUILD TWO WEAPONS WITH DIFFERENT UNION ARMS ----
    let mut w0 = tabledemo::WeaponConfigRow::default();
    w0.damage = 21.0;
    w0.speed = 500.0;
    w0.penetration = 1;
    w0.channel = 3;
    w0.homing = true;
    w0.effect.set_buff(BuffRow { multiplier: 2.5 });

    let mut w1 = tabledemo::WeaponConfigRow::default();
    w1.damage = 42.0;
    w1.speed = 250.0;
    w1.penetration = 5;
    w1.channel = 7;
    w1.homing = false;
    w1.effect.set_debuff(DebuffRow { amount: 42 });

    let mut root = RootConfigRow::default();
    root.version_note[..4].copy_from_slice(b"v1.0");
    root.version_note_length = 4;
    root.weapons[0] = w0;
    root.weapons[1] = w1;
    root.weapons_count = 2;

    // ---- THE WRITE ----
    let measure = root_config_fixed_measure(1);
    let mut file = vec![0x00u8; measure];
    let written = root_config_fixed_save(&[root], &mut file).expect("save succeeds");
    check(written == measure, "arrays of unions: the production writer saves the record");

    // ---- THE READ BACK ----
    let mut back = [RootConfigRow::default(); 1];
    let mut plan = [TableFixedEntry::default(); 1024];
    let mut remap = [0u16; 1024];
    let mut report = TableFixedReport::default();
    let n = root_config_fixed_load(&mut back, &file, &mut plan, &mut remap, &mut report)
        .expect("read succeeds");
    check(n == 1, "arrays of unions: the reader reads one record");
    // strict-only-read (docs/rulings.json): the retired clamp count `oor == 0` is now
    // "no out_of_range refusal": a value past its bound refuses the whole load.
    check(report.reason.name() == "none" && !report.damaged && !report.refused,
          "arrays of unions: a clean read — not refused (no out_of_range), not damaged");

    let b = &back[0];
    check(b.weapons_count == 2, "arrays of unions: weapons_count == 2 read back");

    // Weapon 0: buff arm
    check(b.weapons[0].damage == 21.0, "arrays of unions: weapon[0].damage preserved");
    check(b.weapons[0].speed == 500.0, "arrays of unions: weapon[0].speed preserved");
    check(b.weapons[0].homing, "arrays of unions: weapon[0].homing preserved");
    check(b.weapons[0].effect.tag() == 1,
          "arrays of unions: weapon[0].effect tag is 1 (buff) read back");
    let buff = b.weapons[0].effect.buff().expect("buff arm selected");
    check(buff.multiplier == 2.5,
          "arrays of unions: weapon[0].effect.buff.multiplier == 2.5 read back");

    // Weapon 1: debuff arm
    check(b.weapons[1].damage == 42.0, "arrays of unions: weapon[1].damage preserved");
    check(b.weapons[1].speed == 250.0, "arrays of unions: weapon[1].speed preserved");
    check(!b.weapons[1].homing, "arrays of unions: weapon[1].homing preserved");
    check(b.weapons[1].effect.tag() == 2,
          "arrays of unions: weapon[1].effect tag is 2 (debuff) read back");
    let debuff = b.weapons[1].effect.debuff().expect("debuff arm selected");
    check(debuff.amount == 42,
          "arrays of unions: weapon[1].effect.debuff.amount == 42 read back");

    // ---- THE WIRE: verify the union tags ON THE WIRE ----
    let body = file_body_offset();
    let tag0 = file[body + effect_tag_in_body(&[], 0)];
    let tag1 = file[body + effect_tag_in_body(&[], 1)];
    check(tag0 == 1, "arrays of unions: weapon[0] effect tag on the wire is 1 (buff)");
    check(tag1 == 2, "arrays of unions: weapon[1] effect tag on the wire is 2 (debuff)");

    // ---- NEGATIVE CONTROL: change weapon 0's union tag on the wire to 0 ----
    let mut broken = file.clone();
    let tag0_at = body + effect_tag_in_body(&[], 0);
    // Save the original tag byte before zeroing
    let _original_tag0 = broken[tag0_at];
    broken[tag0_at] = 0; // sabotage: tag becomes None

    let mut back2 = [RootConfigRow::default(); 1];
    let mut plan2 = [TableFixedEntry::default(); 1024];
    let mut remap2 = [0u16; 1024];
    let mut report2 = TableFixedReport::default();
    let n2 = root_config_fixed_load(&mut back2, &broken, &mut plan2, &mut remap2, &mut report2);
    // fixed-dead-bytes-are-zero: the buff payload (2.5f) under a None tag is
    // dead and non-zero, so the whole load refuses as damage, nothing lands
    check(n2.is_none(), "arrays of unions NEGATIVE CONTROL: a None tag over a live payload refuses the load");
    check(report2.damaged, "arrays of unions NEGATIVE CONTROL: the non-zero dead arm bytes are wire damage");
    check(back2[0].weapons_count == 0,
          "arrays of unions NEGATIVE CONTROL: a refused load lands nothing (declared default count 0)");

    // the same sabotage with the now-dead arm payload ZEROED: the tag alone
    // moved, and the read lands None for weapon 0 — the tag selects the arm
    let mult_at = tag0_at + 1;
    for k in 0..4 {
        broken[mult_at + k] = 0;
    }
    let mut back2 = [RootConfigRow::default(); 1];
    let mut report2 = TableFixedReport::default();
    let n2 = root_config_fixed_load(&mut back2, &broken, &mut plan2, &mut remap2, &mut report2)
        .expect("reader opens the sabotaged file");
    check(n2 == 1 && !report2.damaged && !report2.refused,
          "arrays of unions NEGATIVE CONTROL: the reader opens the sabotaged file (dead payload zeroed)");

    // The sabotaged weapon's effect tag is 0 (None)
    check(back2[0].weapons[0].effect.tag() == 0,
          "arrays of unions NEGATIVE CONTROL: sabotaged tag reads as None (0)");
    check(back2[0].weapons[0].effect.buff().is_none(),
          "arrays of unions NEGATIVE CONTROL: buff arm NOT selected when tag is 0");
    check(back2[0].weapons[0].effect.debuff().is_none(),
          "arrays of unions NEGATIVE CONTROL: debuff arm NOT selected when tag is 0");

    // Weapon 1 (unsabotaged) is still correct
    check(back2[0].weapons[1].effect.tag() == 2,
          "arrays of unions NEGATIVE CONTROL: weapon[1] tag is still 2 (debuff)");
    let debuff2 = back2[0].weapons[1].effect.debuff().expect("debuff arm selected");
    check(debuff2.amount == 42,
          "arrays of unions NEGATIVE CONTROL: weapon[1] debuff amount preserved");

    // ---- SECOND NEGATIVE CONTROL: sabotage a byte in a union arm payload ----
    let mut broken2 = file.clone();
    // The debuff arm payload starts after the tag. In the body, weapon 1's
    // effect tag is at weapon body offset 17, and the debuff arm (amount: i32)
    // is at offset 18..22 (= 17+1..17+5).
    let debuff_amount_at = body + ROOT_WEAPONS_BODY_OFFSET + 1 * WEAPON_BODY_SIZE + WEAPON_EFFECT_OFFSET + 1;
    broken2[debuff_amount_at] ^= 0xFF; // flip all bits of one byte of amount

    let mut back3 = [RootConfigRow::default(); 1];
    let mut plan3 = [TableFixedEntry::default(); 1024];
    let mut remap3 = [0u16; 1024];
    let mut report3 = TableFixedReport::default();
    let n3 = root_config_fixed_load(&mut back3, &broken2, &mut plan3, &mut remap3, &mut report3);
    // strict-only-read: 42 ^ 0xFF in the low byte is 213, past `amount`'s
    // declared max 100 (tables/examples/Tables.schema: Debuff.amount | min = 0,
    // max = 100), so the whole load refuses out_of_range (was: "the reader
    // opens the sabotaged file")
    check(n3.is_none(), "arrays of unions NEGATIVE CONTROL 2: an out-of-range arm payload refuses the load");
    check(report3.refused && report3.reason == TableFixedReason::OutOfRange,
          "arrays of unions NEGATIVE CONTROL 2: refused out_of_range");

    // an IN-RANGE corruption of the same byte (42 -> 43) reads, and reaches the value
    let mut broken3 = file.clone();
    broken3[debuff_amount_at] = 43;
    let mut back3 = [RootConfigRow::default(); 1];
    let mut report3 = TableFixedReport::default();
    let n3 = root_config_fixed_load(&mut back3, &broken3, &mut plan3, &mut remap3, &mut report3)
        .expect("reader opens the sabotaged file");
    check(n3 == 1, "arrays of unions NEGATIVE CONTROL 2: the reader opens the in-range sabotaged file");

    // The debuff amount is now corrupted — should NOT equal 42
    let debuff3 = back3[0].weapons[1].effect.debuff().expect("debuff arm selected");
    check(debuff3.amount != 42,
          "arrays of unions NEGATIVE CONTROL 2: a corrupted union arm payload byte reaches the value");

    let fails = FAILURES.load(std::sync::atomic::Ordering::Relaxed);
    assert_eq!(fails, 0, "UnionArraysValidData: {fails} assertion(s) failed");
}