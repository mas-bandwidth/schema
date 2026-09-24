// C8 — fixed-point F-shift / bits(N) (docs/FIXED-FORM-ALGORITHM.md:80-84, 351-352)
//
// THE LAW (inlined from the spec):
//   A fixed-point field's bounds are in VALUE UNITS and its storage is raw,
//   so both ends are shifted by F first; a bits(N) clamps to 2^N - 1.
//
// This test reaches the generated Rust tables through the same fixed-form
// load path the driver and the rust-fixedform test use.

extern crate tabledemo;
extern crate benchfixed;

fn check(ok: bool, msg: &str) {
    if !ok {
        panic!("FAIL: {msg}");
    }
    println!("  ok: {msg}");
}

#[test]
fn c8_fixed_point_f_shift_and_bits_n() {
    // ================================================================
    // PART 1: bits(N) clamps to 2^N - 1
    // ================================================================
    // RangedWidths has bits(12) at body offset 20. bits(12) declares
    // range [0, 4095] = [0, 2^12 - 1]. A forged value of 4096 must
    // land at 4095 after the bounds pass.

    {
        let mut one = tabledemo::RangedWidthsRow::default();
        one.b12 = 3000;
        one.b48 = 0x1234_5678_9ABC_u64;
        let values = [one];
        let mut file = vec![0u8; tabledemo::ranged_widths_fixed_measure(1)];
        check(
            tabledemo::ranged_widths_fixed_save(&values, &mut file) == Some(file.len()),
            "bits(N): the record writes",
        );

        let mut back = [tabledemo::RangedWidthsRow::default(); 2];
        let mut plan = [tabledemo::TableFixedEntry::default(); 512];
        let mut remap = [0u16; 512];
        let mut report = tabledemo::TableFixedReport::default();
        let n = tabledemo::ranged_widths_fixed_load(&mut back, &file, &mut plan, &mut remap, &mut report);
        check(n == Some(1), "bits(N): the clean record reads");
        check(
            back[0].b12 == 3000 && report.clamped == 0,
            "bits(N): value inside bits(12) lands untouched",
        );

        // File layout: header(16) + layout_len(4) + layout(123) + record_hash(8) + body
        // Body offset = 16 + 4 + 123 + 8 = 151
        let body = tabledemo::TABLE_FIXED_HEADER_BYTES + 4
            + tabledemo::RANGED_WIDTHS_FIXED_BLOCK.len() + 8;
        // b12 sits at body offset 20 (b8=0, b16=4, b32=8, b64=12)
        let mut forged = file.clone();
        forged[body + 20] = 0x00;
        forged[body + 21] = 0x10; // 4096 = 0x1000 LE
        forged[body + 22] = 0x00;
        forged[body + 23] = 0x00;
        let mut report = tabledemo::TableFixedReport::default();
        let n = tabledemo::ranged_widths_fixed_load(&mut back, &forged, &mut plan, &mut remap, &mut report);
        check(n == Some(1), "bits(N): the forged record still reads");
        check(back[0].b12 == 4095, "bits(N): 4096 -> 4095 = 2^12 - 1");
        check(report.clamped == 1, "bits(N): counts ONE clamped");

        // bits(48) at body offset 24: u64::MAX past 2^48-1
        let mut forged = file.clone();
        for i in 0..8 { forged[body + 24 + i] = 0xFF; }
        let mut report = tabledemo::TableFixedReport::default();
        tabledemo::ranged_widths_fixed_load(&mut back, &forged, &mut plan, &mut remap, &mut report);
        check(
            back[0].b48 == 0xFFFF_FFFF_FFFF && report.clamped == 1,
            "bits(N): bits(48) past 2^48-1 clamps to 2^48-1",
        );

        // bits(32) fills its lane exactly — no clamp fires
        let mut forged = file.clone();
        for i in 0..4 { forged[body + 8 + i] = 0xFF; }
        let mut report = tabledemo::TableFixedReport::default();
        tabledemo::ranged_widths_fixed_load(&mut back, &forged, &mut plan, &mut remap, &mut report);
        check(
            back[0].b32 == 0xFFFF_FFFF && report.clamped == 0,
            "bits(N): bits(32) filling its lane clamps nothing (negative control)",
        );
    }

    // ================================================================
    // PART 2: fixed-point F-shift
    // ================================================================
    // BenchMixedRow.server_time is fixed(24, 8) | min = 0, max = 65535.
    // F = 8, so raw bounds are [0, 65535 << 8] = [0, 16776960].
    //
    // The law says "both ends are shifted by F first". To prove it:
    //   - raw value 65536 is past the value-unit max (65535) but INSIDE
    //     the shifted max (16776960) → must NOT clamp
    //   - raw value 16776961 is past the shifted max → clamps to 16776960

    {
        // Directly test the clamp_body function on a BenchMixedRow.
        // This is the function that implements the F-shift law.

        // Test 1: value inside shifted bounds, past value-unit max
        let mut value = benchfixed::BenchMixedRow::default();
        value.server_time = 65536; // past value-unit max 65535, inside shifted max 16776960
        value.ping = 100;
        let mut clamped: i32 = 0;
        let mut damaged: i32 = 0;
        benchfixed::bench_mixed_fixed_clamp_body(&mut value, &mut clamped, &mut damaged);
        check(
            value.server_time == 65536 && clamped == 0,
            "F-shift: raw 65536 (past value-unit max) survives — bound is SHIFTED by F=8",
        );

        // Test 2: value past shifted max
        let mut value = benchfixed::BenchMixedRow::default();
        value.server_time = 16776961; // one past shifted max
        value.ping = 100;
        let mut clamped: i32 = 0;
        let mut damaged: i32 = 0;
        benchfixed::bench_mixed_fixed_clamp_body(&mut value, &mut clamped, &mut damaged);
        check(
            value.server_time == 16776960,
            "F-shift: 16776961 -> 16776960 = 65535 << 8 (the SHIFTED max)",
        );
        check(clamped == 1, "F-shift: counts ONE clamped");

        // Test 3: value at shifted max — clean
        let mut value = benchfixed::BenchMixedRow::default();
        value.server_time = 16776960; // exactly at shifted max
        value.ping = 100;
        let mut clamped: i32 = 0;
        let mut damaged: i32 = 0;
        benchfixed::bench_mixed_fixed_clamp_body(&mut value, &mut clamped, &mut damaged);
        check(
            value.server_time == 16776960 && clamped == 0,
            "F-shift: raw 16776960 (exactly the shifted max) clamps nothing",
        );

        // Test 4: ping is ufixed(8,8) | min=0, max=250, F=8
        // Raw bounds: [0, 250 << 8] = [0, 64000]
        let mut value = benchfixed::BenchMixedRow::default();
        value.server_time = 0;
        value.ping = 64001; // one past the shifted max
        let mut clamped: i32 = 0;
        let mut damaged: i32 = 0;
        benchfixed::bench_mixed_fixed_clamp_body(&mut value, &mut clamped, &mut damaged);
        check(
            value.ping == 64000,
            "F-shift: ufixed(8,8) ping: 64001 -> 64000 = 250 << 8",
        );
        check(clamped == 1, "F-shift: ping counts ONE clamped");
    }

    println!("C8: all assertions passed");
}
