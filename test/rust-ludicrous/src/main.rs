// The Rust cross-language wire test for the fixed-point + 128-bit unit
// (examples128/): the generated Rust crate writes the SAME pinned instance
// test/ludicrous_main.cpp pins in testdata/wire/ludicrous_state*.bin and
// byte-compares against those files — cross-language wire identity for the
// serialize-phase-1 families (fixed(I, F), int128, uint128) is the §7.2 gate
// this binary carries. Plus round-trips through the Rust reader, the §5
// branch-zeroing check over a 128-bit field, the specified-defaults checks
// (native i128/u128 literals here — no pair composition), and the
// hostile-read rejections the C++ test carries (reject, never clamp —
// STANDARD.md).
//
// Prints OK and exits 0, exactly like its C++ and Go twins. Run from
// test/rust-ludicrous (the Makefile does): the wire goldens are at
// ../../testdata/wire.
//
// Mirrors test/ludicrous_main.cpp block for block.

// Instances are built field-by-field from new() exactly like the Go test
// (the files stay diffable side by side), so clippy's initializer-style
// suggestion is silenced for the whole binary.
#![allow(clippy::field_reassign_with_default)]

use std::sync::atomic::{AtomicBool, Ordering};

use ludicrous::*;
use serialize::{ReadStream, Stream, WriteStream};

static FAILED: AtomicBool = AtomicBool::new(false);

fn check(ok: bool, what: &str) {
    if !ok {
        println!("FAILED: {what}");
        FAILED.store(true, Ordering::Relaxed);
    }
}

fn check_err(result: ludicrous::Result, what: &str) {
    if let Err(e) = result {
        println!("FAILED: {what}: {e:?}");
        FAILED.store(true, Ordering::Relaxed);
    }
}

// golden_wire byte-compares written wire against the C++-pinned golden.
fn golden_wire(name: &str, data: &[u8]) {
    match std::fs::read(format!("../../testdata/wire/{name}.bin")) {
        Ok(golden) => check(
            data == golden.as_slice(),
            &format!("wire golden {name} — Rust bytes must equal the C++-pinned bytes"),
        ),
        Err(e) => {
            println!("FAILED: read wire golden {name}: {e}");
            FAILED.store(true, Ordering::Relaxed);
        }
    }
}

// set_bits forces bits [pos, pos + n) of the stream image to 1 — the hostile
// reader's tool: smuggling an offset into the range's bit headroom, which a
// read must REJECT, never clamp (STANDARD.md).
fn set_bits(data: &mut [u8], pos: usize, n: usize) {
    for i in pos..pos + n {
        data[i / 8] |= 1 << (i % 8);
    }
}

// make_state is test/ludicrous_main.cpp's make_state — the values must stay
// mirrored on both sides.
fn make_state() -> LudicrousState {
    let mut input = LudicrousState::new();
    input.mode = DriveMode::LUDICROUS;
    input.probe.angle = 2981888; // +45.5 * 2^16
    input.probe.position = -809119744; // -12346.1875 * 2^16
    input.probe.reach = 65536000000 - 1; // raw_max - 1
    input.probe.ticks = 777777;
    input.probe.samples[0] = -524288; // raw_min
    input.probe.samples[1] = 524288; // raw_max
    input.wide.entity_id = 0x0123456789ABCDEF_FEDCBA9876543210;
    input.wide.energy = 4999999999;
    input.wide.flux = (1i128 << 99) + 7;
    // wide.bias and wide.seed stay at their SPECIFIED DEFAULTS (-250 and
    // 2^65) — new() installs them, and they ride the wire as written
    input.keys_count = 2;
    input.keys[0] = 1;
    input.keys[1] = 1u128 << 127;
    input.has_target = true;
    input.target_id = 42;
    input
}

fn main() {
    // worst-case bounds, hand-derived (SPEC §6.1 item 4) — the same numbers
    // test/ludicrous_main.cpp static_asserts
    check(FIXED_PROBE_MAX_BITS == 156, "FixedProbe worst case");
    check(WIDE_PROBE_MAX_BITS == 403, "WideProbe worst case");
    check(LUDICROUS_STATE_MAX_BITS == 1205, "LudicrousState worst case");
    check(MESSAGE_MAX_BITS == 1206, "message-level bound");
    check(PROTOCOL_ID != 0, "the unit has a protocol id");

    // zero initialization with specified defaults (SPEC §4.2), sentinel-zero
    // composition: new() starts at DriveMode::NONE — the null rides in-band —
    // and the two defaulted 128-bit fields construct to their declared
    // values, one of which no i64 literal can spell
    {
        let zero = LudicrousState::new();
        check(zero.mode == DriveMode::NONE, "a fresh state starts at DriveMode None");
        check(zero.probe.reach == 0, "reach starts zero");
        check(zero.wide.entity_id == 0, "entity_id starts zero");
        check(zero.wide.bias == -250, "bias defaults -250");
        check(zero.wide.seed == 1u128 << 65, "seed defaults 2^65");
        check(zero.keys_count == 0, "keys start empty");
        check(zero.target_id == 0, "target_id starts zero");
        // Rust keeps the §5 zero form on Default; new() alone installs the
        // defaults (SPEC §4.2, the Rust column)
        check(LudicrousState::default().wide.bias == 0, "the plain zero value stays zero");
    }

    let mut taken_wire = [0u8; 256];
    let taken_len: usize;

    // ---- the taken-branch wire: generated bytes == the C++-pinned golden ----
    {
        let input = make_state();
        let mut buffer = [0u8; 256];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_ludicrous_state(&mut ws, &input), "write LudicrousState");
        ws.flush();
        taken_len = ws.bytes_processed() as usize;
        taken_wire[..taken_len].copy_from_slice(&buffer[..taken_len]);
        golden_wire("ludicrous_state", &taken_wire[..taken_len]);

        let mut out = LudicrousState::default();
        let mut rs = ReadStream::new(&taken_wire, taken_len);
        check_err(read_ludicrous_state(&mut rs, &mut out), "read LudicrousState");
        check(out.mode == DriveMode::LUDICROUS, "mode round-trips");
        check(out.probe.angle == input.probe.angle, "angle round-trips");
        check(out.probe.position == input.probe.position, "position round-trips");
        check(out.probe.reach == input.probe.reach, "reach round-trips");
        check(out.probe.ticks == input.probe.ticks, "ticks round-trips");
        check(out.probe.samples == input.probe.samples, "samples round-trip");
        check(out.wide.entity_id == input.wide.entity_id, "entity_id round-trips");
        check(out.wide.energy == input.wide.energy, "energy round-trips");
        check(out.wide.flux == input.wide.flux, "flux round-trips");
        check(out.wide.bias == -250, "the bias default rides the wire");
        check(out.wide.seed == 1u128 << 65, "the seed default rides the wire");
        check(out.keys_count == 2, "keys_count round-trips");
        check(
            out.keys[0] == input.keys[0] && out.keys[1] == input.keys[1],
            "keys round-trip",
        );
        check(out.has_target && out.target_id == 42, "the taken branch round-trips");
    }

    // ---- the untaken branch: identical prefix, and the 128-bit field under
    // it reads back ZERO into a dirty object (SPEC §5) ----
    {
        let mut input = make_state();
        input.has_target = false;
        let mut buffer = [0u8; 256];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(
            write_ludicrous_state(&mut ws, &input),
            "write LudicrousState untargeted",
        );
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("ludicrous_state_untargeted", &buffer[..n]);

        let mut out = LudicrousState::default();
        out.target_id = 0xDEAD; // dirty — the read must zero it
        let mut rs = ReadStream::new(&buffer, n);
        check_err(
            read_ludicrous_state(&mut rs, &mut out),
            "read LudicrousState untargeted",
        );
        check(!out.has_target, "has_target reads false");
        check(out.target_id == 0, "the untaken 128-bit field reads as zero (SPEC §5)");
    }

    // ---- hostile reads REJECT, never clamp (STANDARD.md, SPEC §5) ----
    {
        // fixed: angle's 25 offset bits start at bit 2; all-ones = 33554431,
        // above the raw range 360 * 2^16 = 23592960
        let mut hostile = [0u8; 256];
        hostile[..taken_len].copy_from_slice(&taken_wire[..taken_len]);
        set_bits(&mut hostile, 2, 25);
        let mut out = LudicrousState::default();
        let mut rs = ReadStream::new(&hostile, taken_len);
        check(
            read_ludicrous_state(&mut rs, &mut out).is_err(),
            "a smuggled fixed offset is REJECTED",
        );
    }
    {
        // int128: energy's 34 offset bits start at bit 286 (2+156+128);
        // all-ones = 2^34 - 1 = 17179869183, above the range 10^10
        let mut hostile = [0u8; 256];
        hostile[..taken_len].copy_from_slice(&taken_wire[..taken_len]);
        set_bits(&mut hostile, 286, 34);
        let mut out = LudicrousState::default();
        let mut rs = ReadStream::new(&hostile, taken_len);
        check(
            read_ludicrous_state(&mut rs, &mut out).is_err(),
            "a smuggled int128 offset is REJECTED",
        );
    }
    {
        // truncation: running out of input mid-read is a read failure (SPEC §5)
        let mut out = LudicrousState::default();
        let mut rs = ReadStream::new(&taken_wire, 4);
        check(
            read_ludicrous_state(&mut rs, &mut out).is_err(),
            "a truncated stream is a read failure",
        );
    }

    // ---- DegenerateProbe: min == max costs ZERO bits (SPEC §4.6, 2026-08-15) ----
    // The whole wire is the tail byte; a port that emits ANY bits for a
    // degenerate range shifts it and fails the golden compare.
    {
        check(DEGENERATE_PROBE_MAX_BITS == 8, "three degenerate fields cost zero bits");

        let mut input = DegenerateProbe::default();
        input.locked_fixed = -196608; // -3 * 2^16, the ONE legal raw
        input.locked_int = 7;
        input.locked_wide = -12345678901234;
        input.tail = 0xA5;

        let mut buffer = [0u8; 64];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_degenerate_probe(&mut ws, &input), "write DegenerateProbe");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("degenerate_probe", &buffer[..n]);

        let mut out = DegenerateProbe::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_degenerate_probe(&mut rs, &mut out), "read DegenerateProbe");
        check(out == input, "DegenerateProbe round-trips — every value materialized from its range");
    }

    // ---- NarrowBody: the narrowed fixed shallow (SPEC §4.8 rule 2b) ----
    // The pinned tie semantics: quantize rounds to nearest, ties AWAY FROM
    // ZERO — the one fixed-point rounding rule (SPEC §4.8, decided
    // 2026-08-15) — and unquantize is the left shift back. The wire
    // bytes are the C++-pinned goldens; the values mirror
    // test/ludicrous_main.cpp block for block.
    {
        check(NARROW_BODY_DATA_SHALLOW_MAX_BITS == 228, "narrowed shallow worst case");
        check(NARROW_BODY_DATA_DEEP_MAX_BITS == 332, "narrow deep worst case");

        let mut input = NarrowBodyData_Interpolate::default();
        input.position.x = 384; // +1.5 eighths: tie, rounds AWAY to 2
        input.position.y = -384; // -1.5 eighths: tie, rounds AWAY to -2 — THE distinguishing value
        input.position.z = -6586368; // -100.5 * 2^16, exact in 8 kept bits
        input.rotation.w = 1 << 30; // identity, hits the +1024 bound exactly
        input.velocity.x = 1;
        input.velocity.y = -1;
        input.velocity.z = 123456789;

        let mut sh = NarrowBodyData_Shallow::default();
        quantize_narrow_body(&input, &mut sh);
        check(sh.position_x == 2, "+1.5 eighths ties away to 2");
        check(sh.position_y == -2, "-1.5 eighths ties away from zero to -2 (the bare shift would say -1)");
        check(sh.position_z == -25728, "-100.5 units exact in 8 kept bits");
        check(sh.rotation_x == 0 && sh.rotation_y == 0 && sh.rotation_z == 0, "identity xyz quantize to 0");
        check(sh.rotation_w == 1024, "identity w hits the +1024 bound exactly");
        check(
            sh.velocity.x == 1 && sh.velocity.y == -1 && sh.velocity.z == 123456789,
            "full-precision velocity copies",
        );

        let mut back = NarrowBodyData_Interpolate::default();
        unquantize_narrow_body(&sh, &mut back);
        check(back.position.x == 512, "narrowing loss, 384 -> 2 -> 512");
        check(back.position.y == -512, "narrowing loss, -384 -> -2 -> -512");
        check(back.position.z == -6586368, "exact multiple of 2^8 restores exactly");
        check(back.rotation.w == 1 << 30, "the identity survives the round trip");

        let mut buffer = [0u8; 256];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_narrow_body_data_shallow(&mut ws, &sh), "write NarrowBodyData_Shallow");
        ws.flush();
        let sh_len = ws.bytes_processed() as usize;
        let mut sh_wire = [0u8; 256];
        sh_wire[..sh_len].copy_from_slice(&buffer[..sh_len]);
        golden_wire("narrow_body_shallow", &sh_wire[..sh_len]);

        let mut sh_out = NarrowBodyData_Shallow::default();
        let mut rs = ReadStream::new(&sh_wire, sh_len);
        check_err(read_narrow_body_data_shallow(&mut rs, &mut sh_out), "read NarrowBodyData_Shallow");
        check(
            sh_out.position_y == -2 && sh_out.rotation_w == 1024 && sh_out.velocity.z == 123456789,
            "shallow round trip",
        );

        let mut deep = NarrowBodyData_Deep::default();
        deep.position = input.position;
        deep.rotation = input.rotation;
        deep.velocity = input.velocity;
        let mut ws_deep = WriteStream::new(&mut buffer);
        check_err(write_narrow_body_data_deep(&mut ws_deep, &deep), "write NarrowBodyData_Deep");
        ws_deep.flush();
        let deep_len = ws_deep.bytes_processed() as usize;
        let mut deep_wire = [0u8; 256];
        deep_wire[..deep_len].copy_from_slice(&buffer[..deep_len]);
        golden_wire("narrow_body_deep", &deep_wire[..deep_len]);

        let mut deep_out = NarrowBodyData_Deep::default();
        let mut rs_deep = ReadStream::new(&deep_wire, deep_len);
        check_err(read_narrow_body_data_deep(&mut rs_deep, &mut deep_out), "read NarrowBodyData_Deep");
        check(
            deep_out.position.z == -6586368 && deep_out.rotation.w == 1 << 30,
            "deep full precision round trip",
        );

        // hostile shallow read: position_x's 26 offset bits all-ones =
        // 67108863, above the range size 51200000 — reject, never clamp
        let mut hostile = [0u8; 256];
        hostile[..sh_len].copy_from_slice(&sh_wire[..sh_len]);
        set_bits(&mut hostile, 0, 26);
        let mut h_out = NarrowBodyData_Shallow::default();
        let mut h_rs = ReadStream::new(&hostile, sh_len);
        check(
            read_narrow_body_data_shallow(&mut h_rs, &mut h_out).is_err(),
            "a smuggled narrowed offset is REJECTED",
        );
    }

    // ---- the message dispatch surface over the new unit ----
    {
        let input = make_state();
        let mut buffer = [0u8; 256];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(
            write_message(&mut ws, &Message::LudicrousState(input)),
            "write Message LudicrousState",
        );
        ws.flush();
        let n = ws.bytes_processed() as usize;

        let mut rs = ReadStream::new(&buffer, n);
        match read_message(&mut rs) {
            Ok(Message::LudicrousState(out)) => {
                check(out.wide.flux == input.wide.flux, "flux rides the dispatch surface");
                check(out.probe.angle == 2981888, "angle rides the dispatch surface");
            }
            other => check(false, &format!("expected the LudicrousState message, got {other:?}")),
        }
    }

    if FAILED.load(Ordering::Relaxed) {
        std::process::exit(1);
    }
    println!("OK");
}
