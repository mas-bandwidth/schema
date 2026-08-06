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
// Mirrors test/go/main.go block for block where applicable. The Go test's
// foreign-Message and typed-nil refusal cases have no Rust twin: the Message
// enum is closed, so no out-of-set or null message value exists to refuse.

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

    // ---- the Message dispatch surface: the enum IS the tagged union ----
    {
        let mut chat = Chat::default();
        chat.text[..8].copy_from_slice(b"dispatch");
        chat.text_length = 8;
        let mut test = Test::default();
        test.test_b = 42;

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_message(&mut ws, &Message::Chat(chat)), "write Message chat");
        check_err(write_message(&mut ws, &Message::Test(test)), "write Message test");
        check_err(write_message(&mut ws, &Message::None), "write Message terminator");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("message_stream", &buffer[..n]);

        // reads return by value into the caller's storage — the enum IS the
        // pre-allocated home, no heap per message (SPEC §6.1)
        let mut rs = ReadStream::new(&buffer, n);
        match read_message(&mut rs) {
            Ok(Message::Chat(c)) => check(
                c.text_length == 8 && c.text[..8] == *b"dispatch",
                "message 1 is the chat",
            ),
            other => check(false, &format!("message 1 is the chat (got {other:?})")),
        }
        match read_message(&mut rs) {
            Ok(Message::Test(t)) => check(t.test_b == 42, "message 2 is the test"),
            other => check(false, &format!("message 2 is the test (got {other:?})")),
        }
        match read_message(&mut rs) {
            Ok(Message::None) => {}
            other => check(false, &format!("message 3 is the None terminator (got {other:?})")),
        }

        // the tag pair stands alone too
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check_err(write_message_type(&mut ws2, MessageType::CHAT), "write message type");
        check_err(
            write_message_type(&mut ws2, MessageType::NONE),
            "write message type terminator",
        );
        ws2.flush();
        let n2 = ws2.bytes_processed() as usize;
        let mut rs2 = ReadStream::new(&buffer2, n2);
        let mut tag = MessageType::NONE;
        check_err(read_message_type(&mut rs2, &mut tag), "read message type");
        check(tag == MessageType::CHAT, "tag round-trips");
        check_err(read_message_type(&mut rs2, &mut tag), "read message type terminator");
        check(tag == MessageType::NONE, "terminator tag round-trips");
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

    // ---- the object views: quantize/unquantize and the shallow wire ----
    {
        let mut interp = ShipData_Interpolate::default();
        interp.ship_type = ShipType::CORVETTE;
        interp.position = Vec3 {
            x: 1.5,
            y: -2.25,
            z: 100.0,
        };
        interp.rotation = Quat {
            x: 0.0,
            y: 0.0,
            z: 0.0,
            w: 1.0,
        };
        interp.linear_velocity = Vec3 {
            x: 3.0,
            y: 0.0,
            z: -1.0,
        };
        interp.flags = SHIP_FLAGS_BOOSTING;
        interp.team = Team::RED;
        interp.health = 750; // wire-int domain (rule 5)
        interp.thrust = 55;

        let mut q = ShipData_Shallow::default();
        quantize_ship(&interp, &mut q);
        check(q.position_x == 1536, "1.5 * 1024 quantizes to 1536");
        check(q.position_y == -2304, "-2.25 * 1024 quantizes to -2304");
        check(q.rotation_w == 1024, "1.0 * 1024 quantizes to 1024");
        check(q.health == 750 && q.thrust == 55, "projected fields copy");
        check(
            q.team == Team::RED && q.flags == SHIP_FLAGS_BOOSTING,
            "discrete fields copy",
        );

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_ship_data_shallow(&mut ws, &q), "write ShipData_Shallow");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        golden_wire("ship_shallow", &buffer[..n]);

        let mut q2 = ShipData_Shallow::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_ship_data_shallow(&mut rs, &mut q2), "read ShipData_Shallow");
        check(q2 == q, "the shallow wire round-trips");

        let mut back = ShipData_Interpolate::default();
        unquantize_ship(&q2, &mut back);
        check(back.position.x == 1536.0 / 1024.0, "unquantize recovers x");
        check(back.position.y == -2304.0 / 1024.0, "unquantize recovers y");
        check(back.rotation.w == 1.0, "unquantize recovers w");
        check(
            back.health == 750 && back.team == Team::RED,
            "discrete and projected copy back",
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

    // ---- ProbeReport: message-as-field, and the widened flags wire ----
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
        check(out == input, "ProbeReport round-trips — a message as an ordinary field");

        // a mask bit above the widened 8-bit wire is refused, not truncated
        input.flags = 1 << 9;
        let mut buffer2 = [0u8; 2048];
        let mut ws2 = WriteStream::new(&mut buffer2);
        check(
            write_probe_report(&mut ws2, &input).is_err(),
            "a mask bit above the flags wire width is refused",
        );
    }

    // ---- Block: the bytes(N) framing ----
    {
        let mut input = Block::default();
        for i in 0..100 {
            input.block_data[i] = i as u8;
        }
        input.block_data_length = 100;

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

    // ---- Missile: a second object family end to end ----
    {
        let mut interp = MissileData_Interpolate::default();
        interp.missile_type = MissileType::TORPEDO;
        interp.position = Vec3 {
            x: -4.0,
            y: 8.0,
            z: 15.5,
        };
        interp.rotation = Quat {
            x: 0.0,
            y: 0.0,
            z: 0.0,
            w: 1.0,
        };
        interp.linear_velocity = Vec3 {
            x: 1.0,
            y: 2.0,
            z: 3.0,
        };
        interp.team = Team::BLUE;
        interp.flags = 0xF00F;

        let mut q = MissileData_Shallow::default();
        quantize_missile(&interp, &mut q);
        check(q.position_z == 15872, "15.5 * 1024 quantizes to 15872");
        check(
            q.rotation_w == 1024 && q.team == Team::BLUE && q.flags == 0xF00F,
            "discrete fields copy",
        );

        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_missile_data_shallow(&mut ws, &q), "write MissileData_Shallow");
        ws.flush();
        let n = ws.bytes_processed() as usize;
        let mut q2 = MissileData_Shallow::default();
        let mut rs = ReadStream::new(&buffer, n);
        check_err(read_missile_data_shallow(&mut rs, &mut q2), "read MissileData_Shallow");
        check(q2 == q, "the missile shallow wire round-trips");

        let mut back = MissileData_Interpolate::default();
        unquantize_missile(&q2, &mut back);
        check(back.position.z == 15872.0 / 1024.0, "unquantize recovers z");
    }

    // ---- the object tag pair ----
    {
        let mut buffer = [0u8; 2048];
        let mut ws = WriteStream::new(&mut buffer);
        check_err(write_object_type(&mut ws, ObjectType::TURRET), "write object type");
        check_err(
            write_object_type(&mut ws, ObjectType::NONE),
            "write object type sentinel",
        );
        ws.flush();
        let n = ws.bytes_processed() as usize;
        let mut rs = ReadStream::new(&buffer, n);
        let mut tag = ObjectType::NONE;
        check_err(read_object_type(&mut rs, &mut tag), "read object type");
        check(tag == ObjectType::TURRET, "object tag round-trips");
        check_err(read_object_type(&mut rs, &mut tag), "read object type sentinel");
        check(tag == ObjectType::NONE, "the None sentinel round-trips");
    }

    if FAILED.load(Ordering::Relaxed) {
        std::process::exit(1);
    }
    println!("OK");
}

// test_data_instance is the deterministic TestData the C++ test pins — the
// values must stay mirrored on both sides.
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
