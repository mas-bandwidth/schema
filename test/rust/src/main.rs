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

        // a freshly constructed ProbeSample is wire-illegal (samples_count = 0
        // against [1, 8]) — the write must refuse loudly, exactly as Go does,
        // never emit a corrupt packet with Ok
        let fresh = ProbeSample::new();
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check(
            matches!(
                write_probe_sample(&mut ws2, &fresh),
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
