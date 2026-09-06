// The Rust cross-language wire test: the generated Rust crate writes the SAME
// pinned instances the C++ test pins in testdata/wire/*.bin and byte-compares
// against those files — cross-language wire identity is the §7.2 gate this
// binary carries (goal 3: byte-identical wire output across all targets, and
// the readers agree on what they reject). Plus round-trips through the Rust
// reader, the §5 branch-zeroing checks, and the specified-defaults checks.
//
// Prints OK and exits 0, exactly like its C++ and Go twins. Run from
// test/rust (the Makefile does): the wire goldens are at ../../testdata/wire.
//
// Mirrors test/go/main.go block for block where applicable.

// Instances are built field-by-field from Default exactly like the Go test
// (the two files stay diffable side by side), so clippy's initializer-style
// suggestion is silenced for the whole binary.
#![allow(clippy::field_reassign_with_default)]

use std::sync::atomic::{AtomicBool, Ordering};

use example::*;
use serialize::{ReadStream, Stream, WriteStream};

static FAILED: AtomicBool = AtomicBool::new(false);

fn check(ok: bool, what: &str) {
    if !ok {
        println!("FAILED: {what}");
        FAILED.store(true, Ordering::Relaxed);
    }
}

fn check_err(result: example::Result, what: &str) {
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

fn main() {
    // ---- ShipCreate: the bool-gated flags branch, both ways ----
    {
        let mut input = ShipCreate::default();
        input.ship_type = ShipType::BOMBER;
        input.position = QuantizedPosition {
            x: 1000,
            y: -2000,
            z: 3000,
        };
        input.has_flags = true;
        input.flags = SHIP_FLAGS_BOOSTING | SHIP_FLAGS_AIMING;
        input.team = Team::BLUE;
        input.health = 750;
        input.thrust = 55;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_ship_create(&mut ws, &input), "write ShipCreate");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("shipcreate_flags", &buffer[..n]);

        let mut out = ShipCreate::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_ship_create(&mut rs, &mut out), "read ShipCreate");
        check(out == input, "ShipCreate round-trips");

        // untaken branch: flags must read back ZERO (SPEC §5) — into the same
        // out value, so stale flags would be caught
        input.has_flags = false;
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check_err(write_ship_create(&mut ws2, &input), "write ShipCreate no-flags");
        ws2.flush();
        let n2 = ws2.bytes_processed() as usize;
        let mut rs2 = ReadStream::new(&buffer2, n2);
        check_err(read_ship_create(&mut rs2, &mut out), "read ShipCreate no-flags");
        check(
            !out.has_flags && out.flags == 0,
            "untaken branch reads as zero (SPEC §5)",
        );
    }

    // ---- RigidBody: the back-reference example, both branch sides ----
    {
        let mut input = RigidBody::default();
        input.position = Vec3 {
            x: 1.5,
            y: -2.5,
            z: 3.25,
        };
        input.orientation = Quat {
            x: 0.1,
            y: 0.2,
            z: 0.3,
            w: 0.9,
        };
        input.at_rest = false;
        input.linear_velocity = Vec3 {
            x: 10.0,
            y: 20.0,
            z: -3.0,
        };
        input.angular_velocity = Vec3 {
            x: 0.25,
            y: 0.5,
            z: 0.75,
        };

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_rigid_body(&mut ws, &input), "write RigidBody moving");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("rigidbody_moving", &buffer[..n]);

        input.at_rest = true;
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check_err(write_rigid_body(&mut ws2, &input), "write RigidBody at rest");
        ws2.flush();
        let n2 = ws2.bytes_processed() as usize;
        golden_wire("rigidbody_at_rest", &buffer2[..n2]);

        // the at-rest read must ZERO both velocities (SPEC §5), even though
        // the written value had them set
        let mut out = RigidBody::default();
        let mut rs = ReadStream::new(&buffer2, n2);
        check_err(read_rigid_body(&mut rs, &mut out), "read RigidBody at rest");
        check(out.at_rest, "at_rest reads true");
        check(
            out.linear_velocity == Vec3::default() && out.angular_velocity == Vec3::default(),
            "velocities read as zero under the taken at-rest branch (SPEC §5)",
        );
    }

    // ---- Chat: the string framing == classic serialize_string over N + 1 ----
    {
        let mut input = Chat::default();
        input.text[..11].copy_from_slice(b"wire parity");
        input.text_length = 11;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_chat(&mut ws, &input), "write Chat");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("chat", &buffer[..n]);

        let mut out = Chat::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_chat(&mut rs, &mut out), "read Chat");
        check(out == input, "Chat round-trips");
    }

    // ---- ProbeHeader: const/reserved/align on the wire; corruption rejected ----
    {
        let input = ProbeHeader {
            version: 5,
            probe_id: 0x1122334455667788,
        };
        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_probe_header(&mut ws, &input), "write ProbeHeader");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        check(buffer[0] == 0xAB, "const(0xAB, 8) leads the wire");
        golden_wire("probe_header", &buffer[..n]);

        let mut out = ProbeHeader::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_probe_header(&mut rs, &mut out), "read ProbeHeader");
        check(out == input, "ProbeHeader round-trips");

        let mut corrupt = [0u8; 2048];
        corrupt[..n].copy_from_slice(&buffer[..n]);
        corrupt[0] = 0xAC;
        let mut rs2 = ReadStream::new(&corrupt, n);
        check(
            read_probe_header(&mut rs2, &mut out).is_err(),
            "a corrupted wire constant is REJECTED (SPEC §4.3)",
        );
    }

    // ---- InputPacket: counted array of nested structs ----
    {
        let mut input = InputPacket::default();
        input.synchronize_sequence = 7;
        input.current_frame = 123456789;
        input.start_frame = 123456780;
        input.inputs_count = 2;
        input.inputs[0].throttle = 0.5;
        input.inputs[0].fire = true;
        input.inputs[1].stick_x = -0.25;
        input.inputs[1].boost = true;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_input_packet(&mut ws, &input), "write InputPacket");
        ws.flush();
        let n = ws.bytes_processed() as usize;

        let mut out = InputPacket::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_input_packet(&mut rs, &mut out), "read InputPacket");
        check(out == input, "InputPacket round-trips");
    }

    // ---- TestData: the vanilla library's own test type, deterministic values ----
    {
        let input = test_data_instance();

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_test_data(&mut ws, &input), "write TestData");
        ws.flush();
        let n = ws.bytes_processed() as usize;

        let mut out = TestData::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_test_data(&mut rs, &mut out), "read TestData");
        check(
            out == input,
            "TestData round-trips — signed narrows, full-range ints, align, fixed bytes, string",
        );
    }

    // ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
    // 0.005 quantizes to 1 under the float32 two-rounding law (a fused or
    // double build says 0); -4.8585 over the non-zero-min range quantizes to
    // 142 (a double build says 141). Same pinned instance as the C++ leg,
    // against the same golden.
    {
        let mut input = CompressedProbe::default();
        input.boundary = 0.005;
        input.offset = -4.8585;

        let mut buffer = [0u8; 64];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_compressed_probe(&mut ws, &input), "write CompressedProbe");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("compressed_probe", &buffer[..n]);

        let mut out = CompressedProbe::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_compressed_probe(&mut rs, &mut out), "read CompressedProbe");
        check(out.boundary == 1.0f32 / 1000.0f32 * 10.0f32, "boundary reconstructs integer 1");
        check(out.offset == 142.0f32 / 10000.0f32 * 10.0f32 - 5.0f32, "offset reconstructs integer 142");
    }

    // ---- specified defaults: new() carries them; the zero value stays zero ----
    {
        let sample = ProbeSample::new();
        check(sample.active, "ProbeSample.active defaults true");
        check(!ProbeSample::default().active, "the plain zero value stays zero");
        let config = ProbeConfig::new();
        check(config.retries == -1, "ProbeConfig.retries defaults -1");
        check(
            config.preferred == Weapon::RAILGUN,
            "ProbeConfig.preferred defaults Railgun",
        );
    }

    // ---- ProbeBits: the full-range u32/u64 paths, C++-pinned ----
    {
        let mut input = ProbeBits::default();
        input.small = 0x1FF;
        input.boundary = 0x1FFFFFFFF;
        input.wide = 0xFEDCBA9876543210;
        input.sensor = 4294967295;
        input.nonce = 18446744073709551615;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_probe_bits(&mut ws, &input), "write ProbeBits");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("probebits", &buffer[..n]);

        let mut out = ProbeBits::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_probe_bits(&mut rs, &mut out), "read ProbeBits");
        check(out == input, "ProbeBits round-trips — 9/33/64-bit and full-range paths");
    }

    // ---- ProbeCollider: first-class one-of (SPEC §4.8) — C++-pinned wire,
    // round trip, the None arm, an array of unions, and the refusal
    // negative controls ----
    {
        let mut input = ProbeCollider::default();
        check(
            input.shape == ProbeShape::None,
            "the default is the empty union",
        );
        check(
            PROBE_SHAPE_MAX_BITS == 2 + 16,
            "MAX_BITS is tag + the largest arm",
        );
        check(
            ProbeShapeType::MAX == ProbeShapeType(2),
            "the tag newtype rides beside the value enum",
        );

        input.armor = 7;
        input.shape = ProbeShape::Slab(ProbeSlab {
            width: 42,
            height: 9,
        });
        // input.backup stays None — the empty arm costs the tag bits only
        input.extras_count = 1;
        input.extras[0] = ProbeShape::Ring(ProbeRing { radius: 777 });

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_probe_collider(&mut ws, &input), "write ProbeCollider");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("probecollider", &buffer[..n]);

        let mut out = ProbeCollider::default();
        out.backup = ProbeShape::Ring(ProbeRing { radius: 1 }); // dirty — the read must restore None
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_probe_collider(&mut rs, &mut out), "read ProbeCollider");
        check(out == input, "ProbeCollider round-trips, None arm included");

        // NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8, range
        // [0, 2]; forcing both bits makes 3 and the read must refuse
        let mut corrupt = buffer;
        corrupt[1] |= 0x03;
        let mut bad = ProbeCollider::default();
        let mut crs = ReadStream::new(&corrupt, n);
        check(
            read_probe_collider(&mut crs, &mut bad).is_err(),
            "an out-of-range union tag is refused (SPEC §4.8)",
        );

        // NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits at
        // bit offset 10 with range [0, 100]; all seven bits decode 127
        let mut corrupt2 = buffer;
        corrupt2[1] |= 0xFC;
        corrupt2[2] |= 0x01;
        let mut crs2 = ReadStream::new(&corrupt2, n);
        check(
            read_probe_collider(&mut crs2, &mut bad).is_err(),
            "a corrupt union arm payload is refused (SPEC §4.8)",
        );
    }

    // ---- TestData and InputPacket against their C++ pins ----
    {
        let input = test_data_instance();
        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_test_data(&mut ws, &input), "write TestData (pin)");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("testdata", &buffer[..n]);

        let mut packet = InputPacket::default();
        packet.synchronize_sequence = 7;
        packet.current_frame = 123456789;
        packet.start_frame = 123456780;
        packet.inputs_count = 2;
        packet.inputs[0].throttle = 0.5;
        packet.inputs[0].fire = true;
        packet.inputs[1].stick_x = -0.25;
        packet.inputs[1].boost = true;
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check_err(write_input_packet(&mut ws2, &packet), "write InputPacket (pin)");
        ws2.flush();
        let n2 = ws2.bytes_processed() as usize;
        golden_wire("inputpacket", &buffer2[..n2]);
    }

    // ---- ProbeSample: the nested if/else wire, both ways, and §5 zeroing ----
    {
        let mut input = ProbeSample::new(); // active = true
        input.orientation = 90.0;
        input.raw_delta = -5;
        input.big_delta = -1234567890123;
        input.weapon = Weapon::LASER;
        input.has_target = true;
        input.target_id = 777;
        input.idle_ticks = 12345; // untaken side on the wire — must read back ZERO
        input.samples_count = 1;
        input.samples[0] = 42;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_probe_sample(&mut ws, &input), "write ProbeSample active");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        let mut out = ProbeSample::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_probe_sample(&mut rs, &mut out), "read ProbeSample active");
        check(
            out.active && out.weapon == Weapon::LASER && out.has_target && out.target_id == 777,
            "the taken branch round-trips, nested branch included",
        );
        check(out.idle_ticks == 0, "the untaken else side reads as zero (SPEC §5)");
        check(
            out.orientation == 90.0,
            "compressed float round-trips exactly at its resolution",
        );

        input.active = false;
        input.has_target = false;
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check_err(write_probe_sample(&mut ws2, &input), "write ProbeSample idle");
        ws2.flush();
        let n2 = ws2.bytes_processed() as usize;
        let mut rs2 = ReadStream::new(&buffer2, n2);
        check_err(read_probe_sample(&mut rs2, &mut out), "read ProbeSample idle");
        check(
            !out.active && out.idle_ticks == 12345,
            "the else branch round-trips",
        );
        check(
            out.weapon == Weapon::NONE && !out.has_target && out.target_id == 0,
            "the whole untaken then side reads as zero, nested branch included (SPEC §5)",
        );
    }

    // ---- ProbeArray: transitive defaults and its C++ pin ----
    {
        let fresh = ProbeArray::new();
        check(
            fresh.samples[0].active && fresh.samples[1].active,
            "defaults reach through a fixed array",
        );
        check(
            fresh.config.retries == -1 && fresh.config.preferred == Weapon::RAILGUN,
            "defaults reach through a plain member",
        );

        let mut input = ProbeArray::new();
        input.samples[0].orientation = 90.0;
        input.samples[0].raw_delta = -5;
        input.samples[0].big_delta = -1234567890123;
        input.samples[0].weapon = Weapon::LASER;
        input.samples[0].has_target = true;
        input.samples[0].target_id = 777;
        input.samples[0].samples_count = 1;
        input.samples[0].samples[0] = 42;
        input.samples[1].active = false;
        input.samples[1].orientation = -45.5;
        input.samples[1].raw_delta = 7;
        input.samples[1].big_delta = 99;
        input.samples[1].idle_ticks = 1000;
        input.samples[1].samples_count = 2;
        input.samples[1].samples[0] = 7;
        input.samples[1].samples[1] = 8;
        input.config.retries = 3;
        input.config.preferred = Weapon::MISSILE;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_probe_array(&mut ws, &input), "write ProbeArray");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("probearray", &buffer[..n]);

        let mut out = ProbeArray::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_probe_array(&mut rs, &mut out), "read ProbeArray");
        check(
            !out.samples[1].active && out.samples[1].idle_ticks == 1000,
            "nested else branch round-trips",
        );
        check(
            out.samples[1].weapon == Weapon::NONE && !out.samples[1].has_target,
            "nested untaken side reads as zero (SPEC §5)",
        );
        check(
            out.config.retries == 3 && out.config.preferred == Weapon::MISSILE,
            "config round-trips",
        );
    }

    // ---- ProbeReport: nested composition, and the widened flags wire ----
    {
        let mut input = ProbeReport::default();
        input.header.version = 3;
        input.header.probe_id = 0xCAFEBABE;
        input.flags = PROBE_FLAGS_ARMED | PROBE_FLAGS_DAMAGED;
        input.echo.test_a = 555;
        input.echo.test_b = 1000;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_probe_report(&mut ws, &input), "write ProbeReport");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        let mut out = ProbeReport::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_probe_report(&mut rs, &mut out), "read ProbeReport");
        check(out == input, "ProbeReport round-trips — a named type as an ordinary field");

        // a mask bit above the widened 8-bit wire is refused, not truncated
        input.flags = 1 << 9;
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check(
            write_probe_report(&mut ws2, &input).is_err(),
            "a mask bit above the flags wire width is refused",
        );
    }

    // ---- write-side refusals: serialize.rs only debug_asserts on write, so
    // the GENERATED guards are what stand between an out-of-contract caller
    // value and silently corrupt wire in release builds (the Go target gets
    // these refusals from its runtime; here they are emitted) ----
    {
        // enum headroom: Weapon(200) against wire [0, 15]
        let mut sample = ProbeSample::new();
        sample.samples_count = 1;
        sample.weapon = Weapon(200);
        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check(
            matches!(
                write_probe_sample(&mut ws, &sample),
                Err(Error::Stream(serialize::Error::ValueOutOfRange))
            ),
            "an out-of-set enum value is refused on write",
        );

        // a freshly constructed ProbeSample is WIRE-LEGAL: samples_count is
        // born at 1, the declared minimum of [1, 8] (SPEC §4.6) — the one
        // wire-legal count a fresh value can carry
        let fresh = ProbeSample::new();
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check(
            fresh.samples_count == 1,
            "a [1..8] count is born at its declared minimum",
        );
        check(
            write_probe_sample(&mut ws2, &fresh).is_ok(),
            "a freshly constructed value writes cleanly",
        );

        // and a count set below that minimum is still refused loudly, exactly
        // as Go does, never a corrupt packet with Ok
        let mut below = ProbeSample::new();
        below.samples_count = 0;
        let mut buffer_below = [0u8; 2048];
        let mut ws_below = WriteStream::new(&mut buffer_below);
        check(
            matches!(
                write_probe_sample(&mut ws_below, &below),
                Err(Error::Stream(serialize::Error::ValueOutOfRange))
            ),
            "a below-minimum array count is refused on write",
        );

        // ranged int: Test.test_b = 2000 against wire [0, 1000]
        let mut test = Test::default();
        test.test_b = 2000;
        let mut buffer3 = [0u8; 2048];
        let mut ws3 = WriteStream::new(&mut buffer3);
        check(
            matches!(
                write_test(&mut ws3, &test),
                Err(Error::Stream(serialize::Error::ValueOutOfRange))
            ),
            "an out-of-range int value is refused on write",
        );

        // bits(9): a tenth bit set
        let mut bits = ProbeBits::default();
        bits.small = 512;
        bits.nonce = 0; // in range
        let mut buffer4 = [0u8; 2048];
        let mut ws4 = WriteStream::new(&mut buffer4);
        check(
            matches!(
                write_probe_bits(&mut ws4, &bits),
                Err(Error::Stream(serialize::Error::ValueOutOfRange))
            ),
            "a bit above a bits(N) wire width is refused on write",
        );

        // string length beyond the buffer bound: refused, never a slice panic
        let mut chat = Chat::default();
        chat.text_length = 300;
        let mut buffer5 = [0u8; 2048];
        let mut ws5 = WriteStream::new(&mut buffer5);
        check(
            matches!(
                write_chat(&mut ws5, &chat),
                Err(Error::Stream(serialize::Error::ValueOutOfRange))
            ),
            "an over-length string is refused on write, not panicked",
        );
    }

    // ---- Block: the bytes(N) framing ----
    {
        let mut input = Block::default();
        for i in 0..100 {
            input.data[i] = i as u8;
        }
        input.data_length = 100;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_block(&mut ws, &input), "write Block");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        let mut out = Block::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_block(&mut rs, &mut out), "read Block");
        check(out == input, "Block round-trips — bytes(N) framing");
    }

    // ---- the readers agree on what they REJECT (goal 3's second half) ----
    {
        // an interior null in a string is content the read refuses
        let chat_golden = match std::fs::read("../../testdata/wire/chat.bin") {
            Ok(data) => data,
            Err(e) => {
                println!("FAILED: read chat golden for corruption: {e}");
                FAILED.store(true, Ordering::Relaxed);
                return;
            }
        };
        let mut corrupt = chat_golden.clone();
        corrupt[4] = 0; // inside the text bytes (length rides bytes 0-1, align pads to byte 2)
        let mut out = Chat::default();
        let mut rs = ReadStream::new(&corrupt, corrupt.len());
        check(
            read_chat(&mut rs, &mut out) == Err(Error::Validation),
            "an interior null is rejected as validation",
        );

        // a truncated stream is the stream's own error, never a content verdict
        let truncated = &chat_golden[..3];
        let mut out2 = Chat::default();
        let mut rs2 = ReadStream::new(truncated, truncated.len());
        let err = read_chat(&mut rs2, &mut out2);
        check(
            matches!(err, Err(Error::Stream(_))),
            "truncation surfaces as the stream error",
        );

        // a nonzero reserved bit is rejected
        let probe_golden = match std::fs::read("../../testdata/wire/probe_header.bin") {
            Ok(data) => data,
            Err(e) => {
                println!("FAILED: read probe golden for corruption: {e}");
                FAILED.store(true, Ordering::Relaxed);
                return;
            }
        };
        let mut corrupt2 = probe_golden.clone();
        corrupt2[1] |= 0x08; // the first reserved bit above version's 3
        let mut out3 = ProbeHeader::default();
        let mut rs3 = ReadStream::new(&corrupt2, corrupt2.len());
        check(
            read_probe_header(&mut rs3, &mut out3) == Err(Error::Validation),
            "a nonzero reserved bit is rejected",
        );

        // an out-of-range count is refused before any element rides —
        // corrupt the count bits INSIDE a complete valid wire (the preamble is
        // 16+64+64 = 144 bits, so the 5-bit count sits at byte 18 bits 0-4),
        // so the refusal is the RANGE check, not a truncation overflow
        let packet_golden = match std::fs::read("../../testdata/wire/inputpacket.bin") {
            Ok(b) => b,
            Err(e) => {
                println!("FAILED: read inputpacket golden for corruption: {e}");
                FAILED.store(true, Ordering::Relaxed);
                return;
            }
        };
        let mut corrupt3 = packet_golden.clone();
        corrupt3[18] = (corrupt3[18] & !0x1F) | 17; // count 2 -> 17, over [0, 16]
        let mut out4 = InputPacket::default();
        let mut rs4 = ReadStream::new(&corrupt3, corrupt3.len());
        check(
            matches!(read_input_packet(&mut rs4, &mut out4), Err(Error::Stream(_))),
            "an out-of-range count is refused before the loop",
        );
    }

    // ---- RigidBody: the moving branch read back whole ----
    {
        let mut input = RigidBody::default();
        input.position = Vec3 {
            x: 1.5,
            y: -2.5,
            z: 3.25,
        };
        input.orientation = Quat {
            x: 0.1,
            y: 0.2,
            z: 0.3,
            w: 0.9,
        };
        input.at_rest = false;
        input.linear_velocity = Vec3 {
            x: 10.0,
            y: 20.0,
            z: -3.0,
        };
        input.angular_velocity = Vec3 {
            x: 0.25,
            y: 0.5,
            z: 0.75,
        };

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_rigid_body(&mut ws, &input), "write RigidBody moving (read-back)");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        let mut out = RigidBody::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_rigid_body(&mut rs, &mut out), "read RigidBody moving");
        check(out == input, "the moving branch round-trips with velocities intact");
    }

    // ---- the string UTF-8 contract's debug assert can FAIL (SPEC §4.7) ----
    // string(N) payloads are well-formed UTF-8 by contract, writer-trusted,
    // debug_assert!ed on write. cargo run is a debug build, so a malformed
    // payload must PANIC here — proving the assert is live, not decorative.
    // (A release build writes the bytes silently; that is the contract's
    // whole design: no release-path cost.)
    #[cfg(debug_assertions)]
    {
        let hook = std::panic::take_hook();
        std::panic::set_hook(Box::new(|_| {})); // silence the expected panic's message
        let panicked = std::panic::catch_unwind(|| {
            let mut chat = Chat::default();
            chat.text[..2].copy_from_slice(&[0xC3, 0x28]); // a truncated lead — malformed UTF-8
            chat.text_length = 2;
            let mut buffer = [0u8; 64];
            let mut ws = WriteStream::new(&mut buffer);
            let _ = write_chat(&mut ws, &chat);
        })
        .is_err();
        std::panic::set_hook(hook);
        check(
            panicked,
            "writing malformed UTF-8 through a string(N) field panics the debug assert (SPEC §4.7)",
        );
    }

    // ---- flag_name / flag_names: per-bit names and the set renderer ----
    {
        check(example::flag_name_ship_flags(0) == "FiringLaser", "flag_name names bit 0");
        check(example::flag_name_ship_flags(9) == "???", "flag_name is out-of-range safe");
        check(example::flag_names_ship_flags(0) == "0", "flag_names renders the empty set as 0");
        check(
            example::flag_names_ship_flags(example::SHIP_FLAGS_FIRING_LASER | example::SHIP_FLAGS_BRAKING)
                == "FiringLaser|Braking",
            "flag_names renders the set bits",
        );
        check(
            example::flag_names_ship_flags(example::SHIP_FLAGS_AIMING | (1 << 63))
                == "Aiming|0x8000000000000000",
            "flag_names renders unknown high bits as hex",
        );
    }

    // ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
    //
    // Twelve shapes written back to back into ONE stream against the one
    // C++-pinned golden, in the C++ test's order. A fixed scalar array whose
    // elements an emitter places TWICE is invisible to a same-language round
    // trip; only the byte compare against another language's bytes names it.
    {
        let mut vec2 = Vec2::default();
        vec2.x = 1.5;
        vec2.y = -2.25;

        let mut span_f64 = SpanF64::default();
        span_f64.values = [3.5, -4.75];

        let mut span_u64 = SpanU64::default();
        span_u64.values = [0xDEADBEEFCAFEBABE, 1];

        let mut span_i64 = SpanI64::default();
        span_i64.values = [-1234567890123, 42];

        let mut span_one = SpanOne::default();
        span_one.values = [0x0123456789ABCDEF];

        let mut span_chunk = SpanChunk::default();
        span_chunk.values = [0x1111, 0x2222, 0x3333, 0x4444];

        let mut span_tail = SpanTail::default();
        span_tail.values = [6.125, -7.0];
        span_tail.tail = 0xFEEDFACE;

        let mut span_twice = SpanTwice::default();
        span_twice.a = [8.5, 9.5];
        span_twice.b = [-10.5, -11.5];

        let mut trio = Trio::default();
        trio.a = 0xABCDE;
        trio.b = 0x12345;
        trio.c = 0xFFFFF;

        let mut trio_sole = TrioSole::default();
        trio_sole.inner = Trio { a: 1, b: 2, c: 3 };

        let mut trio_first = TrioFirst::default();
        trio_first.inner = Trio { a: 0xAAAAA, b: 0x55555, c: 0xF0F0F };
        trio_first.trailer = 0xBEEF;

        let mut straddle = TrioStraddle::default();
        straddle.pad0 = 0x0011223344556677;
        straddle.pad1 = 0x8899AABBCCDDEEFF;
        straddle.pad2 = 0xFFFFFFFFFFFFFFFF;
        straddle.pad3 = 0;
        straddle.pad4 = 0x123456789ABCDEF0;
        straddle.pad5 = 0xABCDEF;
        straddle.inner = Trio { a: 0x11111, b: 0x22222, c: 0x33333 };

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_vec2(&mut ws, &vec2), "write Vec2");
        check_err(write_span_f64(&mut ws, &span_f64), "write SpanF64");
        check_err(write_span_u64(&mut ws, &span_u64), "write SpanU64");
        check_err(write_span_i64(&mut ws, &span_i64), "write SpanI64");
        check_err(write_span_one(&mut ws, &span_one), "write SpanOne");
        check_err(write_span_chunk(&mut ws, &span_chunk), "write SpanChunk");
        check_err(write_span_tail(&mut ws, &span_tail), "write SpanTail");
        check_err(write_span_twice(&mut ws, &span_twice), "write SpanTwice");
        check_err(write_trio(&mut ws, &trio), "write Trio");
        check_err(write_trio_sole(&mut ws, &trio_sole), "write TrioSole");
        check_err(write_trio_first(&mut ws, &trio_first), "write TrioFirst");
        check_err(write_trio_straddle(&mut ws, &straddle), "write TrioStraddle");
        check(
            ws.bits_processed() == 128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408,
            "the twelve degenerate shapes ride their declared widths and nothing more",
        );
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("degenerate", &buffer[..n]);

        let mut r_vec2 = Vec2::default();
        let mut r_span_f64 = SpanF64::default();
        let mut r_span_u64 = SpanU64::default();
        let mut r_span_i64 = SpanI64::default();
        let mut r_span_one = SpanOne::default();
        let mut r_span_chunk = SpanChunk::default();
        let mut r_span_tail = SpanTail::default();
        let mut r_span_twice = SpanTwice::default();
        let mut r_trio = Trio::default();
        let mut r_trio_sole = TrioSole::default();
        let mut r_trio_first = TrioFirst::default();
        let mut r_straddle = TrioStraddle::default();

        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_vec2(&mut rs, &mut r_vec2), "read Vec2");
        check_err(read_span_f64(&mut rs, &mut r_span_f64), "read SpanF64");
        check_err(read_span_u64(&mut rs, &mut r_span_u64), "read SpanU64");
        check_err(read_span_i64(&mut rs, &mut r_span_i64), "read SpanI64");
        check_err(read_span_one(&mut rs, &mut r_span_one), "read SpanOne");
        check_err(read_span_chunk(&mut rs, &mut r_span_chunk), "read SpanChunk");
        check_err(read_span_tail(&mut rs, &mut r_span_tail), "read SpanTail");
        check_err(read_span_twice(&mut rs, &mut r_span_twice), "read SpanTwice");
        check_err(read_trio(&mut rs, &mut r_trio), "read Trio");
        check_err(read_trio_sole(&mut rs, &mut r_trio_sole), "read TrioSole");
        check_err(read_trio_first(&mut rs, &mut r_trio_first), "read TrioFirst");
        check_err(read_trio_straddle(&mut rs, &mut r_straddle), "read TrioStraddle");

        check(r_vec2 == vec2, "Vec2 round-trips");
        check(r_span_f64 == span_f64, "SpanF64 round-trips");
        check(r_span_u64 == span_u64, "SpanU64 round-trips");
        check(r_span_i64 == span_i64, "SpanI64 round-trips");
        check(r_span_one == span_one, "SpanOne round-trips");
        check(r_span_chunk == span_chunk, "SpanChunk round-trips");
        check(r_span_tail == span_tail, "SpanTail round-trips");
        check(r_span_twice == span_twice, "SpanTwice round-trips");
        check(r_trio == trio, "Trio round-trips");
        check(r_trio_sole == trio_sole, "TrioSole round-trips");
        check(r_trio_first == trio_first, "TrioFirst round-trips");
        check(r_straddle == straddle, "TrioStraddle round-trips");
    }

    // ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----
    //
    // Degenerate.schema is every-type-a-whole-number-of-bytes by
    // construction, so no clause boundary in it lands mid-byte. These two
    // units are chosen so they do. Each shape is written to its OWN stream
    // and flushed, and the golden is those concatenated — the shapes are not
    // byte-aligned, so a shared stream would not equal the concatenation
    // every emitter can produce.
    {
        let mut stream: Vec<u8> = Vec::new();
        macro_rules! emit {
            ($name:expr, $bits:expr, $write:expr) => {{
                let mut buffer = [0u8; 64];
                let mut ws = WriteStream::new(&mut buffer);
                check_err($write(&mut ws), concat!("write ", $name));
                check(
                    ws.bits_processed() == $bits as u64,
                    concat!($name, " rides its declared width"),
                );
                ws.flush();
                let n = ws.bytes_processed() as usize;
                stream.extend_from_slice(&buffer[..n]);
            }};
        }

        let mut off = 0usize;
        macro_rules! consume {
            ($name:expr, $bits:expr, $read:expr) => {{
                let n = (($bits as usize) + 7) / 8;
                let mut slice = [0u8; 64]; // read allocations extend past the data
                slice[..n].copy_from_slice(&stream[off..off + n]);
                let mut rs = ReadStream::new(&slice, n);
                check_err($read(&mut rs), concat!("read ", $name));
                off += n;
            }};
        }

        // ---- Clauses.schema ----

        let w13_at = |c: usize| {
            let mut v = W13::default();
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = (8191 - i * 733) as u16;
            }
            v
        };
        let w13_counts = [0usize, 1, 3, 4, 5, 7, 12];
        for &c in &w13_counts {
            let v = w13_at(c);
            emit!("W13", 4 + 13 * c, |ws: &mut WriteStream<'_>| write_w13(ws, &v));
        }

        let w17_at = |c: usize| {
            let mut v = W17::default();
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = (131071 - i * 11117) as u32;
            }
            v
        };
        let w17_counts = [0usize, 1, 2, 3, 4, 9];
        for &c in &w17_counts {
            let v = w17_at(c);
            emit!("W17", 4 + 17 * c, |ws: &mut WriteStream<'_>| write_w17(ws, &v));
        }

        let w26_at = |c: usize| {
            let mut v = W26::default();
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = (67108863 - i * 5555555) as u32;
            }
            v
        };
        let w26_counts = [0usize, 1, 2, 3, 6];
        for &c in &w26_counts {
            let v = w26_at(c);
            emit!("W26", 3 + 26 * c, |ws: &mut WriteStream<'_>| write_w26(ws, &v));
        }

        let w1_at = |c: usize| {
            let mut v = W1::default();
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = (i % 2) as u8;
            }
            v
        };
        let w1_counts = [0usize, 1, 3, 4, 5, 20];
        for &c in &w1_counts {
            let v = w1_at(c);
            emit!("W1", 5 + c, |ws: &mut WriteStream<'_>| write_w1(ws, &v));
        }

        let w52_at = |c: usize| {
            let mut v = W52::default();
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = 4503599627370495u64 - (i as u64) * 123456789;
            }
            v
        };
        for c in 0usize..=3 {
            let v = w52_at(c);
            emit!("W52", 2 + 52 * c, |ws: &mut WriteStream<'_>| write_w52(ws, &v));
        }

        let w50_at = |c: usize| {
            let mut v = W50::default();
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = 1125899906842623u64 - (i as u64) * 987654321;
            }
            v
        };
        for c in 0usize..=3 {
            let v = w50_at(c);
            emit!("W50", 2 + 50 * c, |ws: &mut WriteStream<'_>| write_w50(ws, &v));
        }

        let mut f13 = F13::default();
        for i in 0..7 {
            f13.items[i] = (8191 - i * 911) as u16;
        }
        emit!("F13", 91, |ws: &mut WriteStream<'_>| write_f13(ws, &f13));

        let tri_at = |c: usize| {
            let mut v = ArrTri3::default();
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = Tri3 { a: (i % 2) as u32, b: (i % 4) as u32 };
            }
            v
        };
        let tri_counts = [0usize, 1, 3, 4, 5, 10];
        for &c in &tri_counts {
            let v = tri_at(c);
            emit!("ArrTri3", 4 + 3 * c, |ws: &mut WriteStream<'_>| write_arr_tri3(ws, &v));
        }

        let mut arr_eleven = ArrEleven::default();
        for i in 0..9 {
            arr_eleven.items[i] = Eleven { a: (i % 8) as u32, b: (255 - i * 17) as u32 };
        }
        emit!("ArrEleven", 99, |ws: &mut WriteStream<'_>| write_arr_eleven(ws, &arr_eleven));

        // lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag
        let empty_unions = [
            HoldsEmptyUnion { lead: 21, u: EmptyUnion::None, tail: 99 },
            HoldsEmptyUnion { lead: 21, u: EmptyUnion::A(EmptyA::default()), tail: 99 },
            HoldsEmptyUnion { lead: 21, u: EmptyUnion::B(EmptyB::default()), tail: 99 },
        ];
        for v in &empty_unions {
            emit!("HoldsEmptyUnion", 14, |ws: &mut WriteStream<'_>| write_holds_empty_union(ws, v));
        }

        // lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s bytes,
        // b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail. The
        // 5-bit lead is what puts the align at a non-zero offset.
        let mut strs_empty = Strs::default();
        strs_empty.lead = 21;
        strs_empty.tail = 5;
        let mut strs_full = Strs::default();
        strs_full.lead = 21;
        strs_full.tail = 5;
        strs_full.s.copy_from_slice(b"abcdefgh");
        strs_full.s_length = 8;
        for i in 0..8 {
            strs_full.b[i] = (0xF0 + i) as u8;
        }
        strs_full.b_length = 8;
        let mut strs_part = Strs::default();
        strs_part.lead = 21;
        strs_part.tail = 5;
        strs_part.s[..3].copy_from_slice(b"xyz");
        strs_part.s_length = 3;
        for i in 0..3 {
            strs_part.b[i] = (i + 1) as u8;
        }
        strs_part.b_length = 3;
        let strs = [(strs_empty, 27usize), (strs_full, 155), (strs_part, 75)];
        for (v, bits) in &strs {
            emit!("Strs", *bits, |ws: &mut WriteStream<'_>| write_strs(ws, v));
        }

        let nested_at = |c: usize| {
            let mut v = ArrNested::default();
            v.lead = 21;
            v.tail = 5;
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = Eleven { a: (i % 8) as u32, b: (200 - i * 7) as u32 };
            }
            v
        };
        for c in 0usize..=4 {
            let v = nested_at(c);
            emit!("ArrNested", 11 + 11 * c, |ws: &mut WriteStream<'_>| write_arr_nested(ws, &v));
        }

        let sole = Sole { only: 5555 };
        emit!("Sole", 13, |ws: &mut WriteStream<'_>| write_sole(ws, &sole));

        golden_wire("clauses", &stream);

        // Read each shape back out of its own slice. A clause that decodes a
        // different number of elements than the writer encoded shows up here
        // even where the byte compare above happens to pass.
        for &c in &w13_counts {
            let mut r = W13::default();
            consume!("W13", 4 + 13 * c, |rs: &mut ReadStream<'_>| read_w13(rs, &mut r));
            check(r == w13_at(c), "W13 round-trips");
        }
        for &c in &w17_counts {
            let mut r = W17::default();
            consume!("W17", 4 + 17 * c, |rs: &mut ReadStream<'_>| read_w17(rs, &mut r));
            check(r == w17_at(c), "W17 round-trips");
        }
        for &c in &w26_counts {
            let mut r = W26::default();
            consume!("W26", 3 + 26 * c, |rs: &mut ReadStream<'_>| read_w26(rs, &mut r));
            check(r == w26_at(c), "W26 round-trips");
        }
        for &c in &w1_counts {
            let mut r = W1::default();
            consume!("W1", 5 + c, |rs: &mut ReadStream<'_>| read_w1(rs, &mut r));
            check(r == w1_at(c), "W1 round-trips");
        }
        for c in 0usize..=3 {
            let mut r = W52::default();
            consume!("W52", 2 + 52 * c, |rs: &mut ReadStream<'_>| read_w52(rs, &mut r));
            check(r == w52_at(c), "W52 round-trips");
        }
        for c in 0usize..=3 {
            let mut r = W50::default();
            consume!("W50", 2 + 50 * c, |rs: &mut ReadStream<'_>| read_w50(rs, &mut r));
            check(r == w50_at(c), "W50 round-trips");
        }
        {
            let mut r = F13::default();
            consume!("F13", 91, |rs: &mut ReadStream<'_>| read_f13(rs, &mut r));
            check(r == f13, "F13 round-trips");
        }
        for &c in &tri_counts {
            let mut r = ArrTri3::default();
            consume!("ArrTri3", 4 + 3 * c, |rs: &mut ReadStream<'_>| read_arr_tri3(rs, &mut r));
            check(r == tri_at(c), "ArrTri3 round-trips");
        }
        {
            let mut r = ArrEleven::default();
            consume!("ArrEleven", 99, |rs: &mut ReadStream<'_>| read_arr_eleven(rs, &mut r));
            check(r == arr_eleven, "ArrEleven round-trips");
        }
        for v in &empty_unions {
            let mut r = HoldsEmptyUnion::default();
            consume!("HoldsEmptyUnion", 14, |rs: &mut ReadStream<'_>| read_holds_empty_union(rs, &mut r));
            check(r == *v, "HoldsEmptyUnion round-trips");
        }
        for (v, bits) in &strs {
            let mut r = Strs::default();
            consume!("Strs", *bits, |rs: &mut ReadStream<'_>| read_strs(rs, &mut r));
            check(r == *v, "Strs round-trips");
        }
        for c in 0usize..=4 {
            let mut r = ArrNested::default();
            consume!("ArrNested", 11 + 11 * c, |rs: &mut ReadStream<'_>| read_arr_nested(rs, &mut r));
            check(r == nested_at(c), "ArrNested round-trips");
        }
        {
            let mut r = Sole::default();
            consume!("Sole", 13, |rs: &mut ReadStream<'_>| read_sole(rs, &mut r));
            check(r == sole, "Sole round-trips");
        }
        check(off == stream.len(), "the clauses reads consume the whole golden");

        // ---- Joins.schema ----
        //
        // Every branch is written on BOTH arms, so no path is pinned by
        // omission. The expected value after a round trip is not the value
        // written: the untaken side reads back as zero (SPEC §5).

        stream.clear();
        off = 0;

        for f in [false, true] {
            // the arms agree on WIDTH but not on value, so a join that keeps
            // the wrong arm is a value mismatch and not just a width one
            let agree = ArmsAgree { lead: 21, flag: f, a: 1234, b: 1500, tail: 99 };
            emit!("ArmsAgree", 24usize, |ws: &mut WriteStream<'_>| write_arms_agree(ws, &agree));

            let disagree = ArmsDisagree { lead: 21, flag: f, a: 1234, b: 5, tail: 99 };
            emit!("ArmsDisagree", if f { 24usize } else { 16 }, |ws: &mut WriteStream<'_>| {
                write_arms_disagree(ws, &disagree)
            });

            let arm_empty = ArmEmpty { lead: 21, flag: f, a: 456789, tail: 99 };
            emit!("ArmEmpty", if f { 32usize } else { 13 }, |ws: &mut WriteStream<'_>| {
                write_arm_empty(ws, &arm_empty)
            });

            let mut align_str = ArmAlign::default();
            align_str.lead = 21;
            align_str.flag = f;
            align_str.s.copy_from_slice(b"abcd");
            align_str.s_length = 4;
            align_str.b = 1000;
            align_str.tail = 99;
            emit!("ArmAlign", if f { 55usize } else { 23 }, |ws: &mut WriteStream<'_>| {
                write_arm_align(ws, &align_str)
            });

            let mut align_empty = ArmAlign::default();
            align_empty.lead = 21;
            align_empty.flag = f;
            align_empty.b = 1000;
            align_empty.tail = 99;
            emit!("ArmAlignEmptyStr", 23usize, |ws: &mut WriteStream<'_>| {
                write_arm_align(ws, &align_empty)
            });
        }

        let nested_bits = |o: bool, i: bool| if o { if i { 40usize } else { 16 } } else { 23 };
        for o in [false, true] {
            for i in [false, true] {
                let v = ArmsNested {
                    lead: 5,
                    outer: o,
                    inner: i,
                    x: 500000000,
                    y: 17,
                    z: 4000,
                    tail: 33,
                };
                emit!("ArmsNested", nested_bits(o, i), |ws: &mut WriteStream<'_>| {
                    write_arms_nested(ws, &v)
                });
            }
        }

        let arm_array_at = |f: bool, c: usize| {
            let mut v = ArmArray::default();
            v.lead = 21;
            v.flag = f;
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = (8191 - i * 777) as u16;
            }
            v.b = 300;
            v.tail = 99;
            v
        };
        let arm_array_bits = |f: bool, c: usize| if f { 15 + 13 * c } else { 22usize };
        for f in [false, true] {
            for c in 0usize..=3 {
                let v = arm_array_at(f, c);
                emit!("ArmArray", arm_array_bits(f, c), |ws: &mut WriteStream<'_>| {
                    write_arm_array(ws, &v)
                });
            }
        }

        // lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits
        let unevens = [
            (HoldsUneven { lead: 21, u: Uneven::None, tail: 1500 }, 18usize),
            (HoldsUneven { lead: 21, u: Uneven::Narrow(Narrow { n: 5 }), tail: 1500 }, 21),
            (
                HoldsUneven { lead: 21, u: Uneven::Wide(Wide { w: 123456789012 }), tail: 1500 },
                55,
            ),
        ];
        for (v, bits) in &unevens {
            emit!("HoldsUneven", *bits, |ws: &mut WriteStream<'_>| write_holds_uneven(ws, v));
        }

        // alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37)
        let uneven_item_bits = [0usize, 5, 44, 49];
        let arr_uneven_at = |c: usize| {
            let mut v = ArrUneven::default();
            v.lead = 21;
            v.tail = 5;
            v.items_count = c as i32;
            for i in 0..c {
                v.items[i] = if i % 2 == 0 {
                    Uneven::Narrow(Narrow { n: (i % 8) as u32 })
                } else {
                    Uneven::Wide(Wide { w: 99887766554u64 + i as u64 })
                };
            }
            v
        };
        for c in 0usize..=3 {
            let v = arr_uneven_at(c);
            emit!("ArrUneven", 10 + uneven_item_bits[c], |ws: &mut WriteStream<'_>| {
                write_arr_uneven(ws, &v)
            });
        }

        // lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s,
        // then a 32 + 29 + 19 + 4 static run after the align regains it
        let regain_at = |c: usize, sl: usize| {
            let mut v = RegainAfterAlign::default();
            v.lead = 21;
            v.items_count = c as i32;
            v.s_length = sl as i32;
            if sl != 0 {
                v.s.copy_from_slice(b"wxyz");
            }
            for i in 0..c {
                v.items[i] = (8191 - i * 999) as u16;
            }
            v.p = 0xDEADBEEF;
            v.q = (1u32 << 29) - 7;
            v.r = (1u32 << 19) - 3;
            v.tail = 9;
            v
        };
        let regain_bits = |c: usize, sl: usize| ((5 + 2 + 13 * c + 3) + 7) / 8 * 8 + 8 * sl + 84;
        for c in 0usize..=3 {
            for sl in [0usize, 4] {
                let v = regain_at(c, sl);
                emit!("RegainAfterAlign", regain_bits(c, sl), |ws: &mut WriteStream<'_>| {
                    write_regain_after_align(ws, &v)
                });
            }
        }

        golden_wire("joins", &stream);

        for f in [false, true] {
            let mut r = ArmsAgree::default();
            consume!("ArmsAgree", 24usize, |rs: &mut ReadStream<'_>| read_arms_agree(rs, &mut r));
            let want = ArmsAgree {
                lead: 21,
                flag: f,
                a: if f { 1234 } else { 0 },
                b: if f { 0 } else { 1500 },
                tail: 99,
            };
            check(r == want, "ArmsAgree round-trips (untaken side zeroed, SPEC §5)");

            let mut r = ArmsDisagree::default();
            consume!("ArmsDisagree", if f { 24usize } else { 16 }, |rs: &mut ReadStream<'_>| {
                read_arms_disagree(rs, &mut r)
            });
            let want = ArmsDisagree {
                lead: 21,
                flag: f,
                a: if f { 1234 } else { 0 },
                b: if f { 0 } else { 5 },
                tail: 99,
            };
            check(r == want, "ArmsDisagree round-trips");

            let mut r = ArmEmpty::default();
            consume!("ArmEmpty", if f { 32usize } else { 13 }, |rs: &mut ReadStream<'_>| {
                read_arm_empty(rs, &mut r)
            });
            let want =
                ArmEmpty { lead: 21, flag: f, a: if f { 456789 } else { 0 }, tail: 99 };
            check(r == want, "ArmEmpty round-trips");

            let mut r = ArmAlign::default();
            consume!("ArmAlign", if f { 55usize } else { 23 }, |rs: &mut ReadStream<'_>| {
                read_arm_align(rs, &mut r)
            });
            let mut want = ArmAlign::default();
            want.lead = 21;
            want.flag = f;
            want.tail = 99;
            if f {
                want.s.copy_from_slice(b"abcd");
                want.s_length = 4;
            } else {
                want.b = 1000;
            }
            check(r == want, "ArmAlign round-trips");

            let mut r = ArmAlign::default();
            consume!("ArmAlignEmptyStr", 23usize, |rs: &mut ReadStream<'_>| {
                read_arm_align(rs, &mut r)
            });
            let mut want = ArmAlign::default();
            want.lead = 21;
            want.flag = f;
            want.tail = 99;
            if !f {
                want.b = 1000;
            }
            check(r == want, "ArmAlign with an empty string round-trips");
        }

        for o in [false, true] {
            for i in [false, true] {
                let mut r = ArmsNested::default();
                consume!("ArmsNested", nested_bits(o, i), |rs: &mut ReadStream<'_>| {
                    read_arms_nested(rs, &mut r)
                });
                let mut want = ArmsNested::default();
                want.lead = 5;
                want.outer = o;
                want.tail = 33;
                if o {
                    want.inner = i;
                    if i {
                        want.x = 500000000;
                    } else {
                        want.y = 17;
                    }
                } else {
                    want.z = 4000;
                }
                check(r == want, "ArmsNested round-trips");
            }
        }

        for f in [false, true] {
            for c in 0usize..=3 {
                let mut r = ArmArray::default();
                consume!("ArmArray", arm_array_bits(f, c), |rs: &mut ReadStream<'_>| {
                    read_arm_array(rs, &mut r)
                });
                let mut want = ArmArray::default();
                want.lead = 21;
                want.flag = f;
                want.tail = 99;
                if f {
                    want.items_count = c as i32;
                    for i in 0..c {
                        want.items[i] = (8191 - i * 777) as u16;
                    }
                } else {
                    want.b = 300;
                }
                check(r == want, "ArmArray round-trips");
            }
        }

        for (v, bits) in &unevens {
            let mut r = HoldsUneven::default();
            consume!("HoldsUneven", *bits, |rs: &mut ReadStream<'_>| read_holds_uneven(rs, &mut r));
            check(r == *v, "HoldsUneven round-trips");
        }

        for c in 0usize..=3 {
            let mut r = ArrUneven::default();
            consume!("ArrUneven", 10 + uneven_item_bits[c], |rs: &mut ReadStream<'_>| {
                read_arr_uneven(rs, &mut r)
            });
            check(r == arr_uneven_at(c), "ArrUneven round-trips");
        }

        for c in 0usize..=3 {
            for sl in [0usize, 4] {
                let mut r = RegainAfterAlign::default();
                consume!("RegainAfterAlign", regain_bits(c, sl), |rs: &mut ReadStream<'_>| {
                    read_regain_after_align(rs, &mut r)
                });
                check(r == regain_at(c, sl), "RegainAfterAlign round-trips");
            }
        }
        check(off == stream.len(), "the joins reads consume the whole golden");
    }

    if FAILED.load(Ordering::Relaxed) {
        std::process::exit(1);
    }
    println!("OK");
}


// test_data_instance is the deterministic TestData the C++ test pins — the
// values must stay mirrored on both sides (float_value's 3.1415926 is the
// pinned instance's digits, so approx_constant is silenced rather than the
// wire value changed).
#[allow(clippy::approx_constant)]
fn test_data_instance() -> TestData {
    let mut input = TestData::default();
    input.a = -100;
    input.b = 100;
    input.c = 149;
    input.d = 0x11;
    input.e = 0x22;
    input.f = 0x33;
    input.g = true;
    input.items_count = 3;
    input.items[0] = 0;
    input.items[1] = 128;
    input.items[2] = 255;
    input.float_value = 3.1415926;
    input.compressed_float_value = 2.5;
    input.double_value = 1.0 / 3.0;
    input.int8_value = -128;
    input.int16_value = -32768;
    input.uint8_value = 255;
    input.uint16_value = 65535;
    input.uint32_value = 4294967295;
    input.uint64_value = 18446744073709551615;
    input.int64_full = i64::MIN; // -9223372036854775808, the Go test's literal
    input.int64_range = -999999999999;
    for i in 0..input.fixed_bytes.len() {
        input.fixed_bytes[i] = (i * 3) as u8;
    }
    input.text[..19].copy_from_slice(b"the quick brown fox");
    input.text_length = 19;
    input
}
