// The Goose cross-language wire test: the generated Goose modules of
// examples/ write the SAME pinned instances the C++ test pins in
// testdata/wire/*.bin and byte-compare against those files — cross-language
// wire identity is the SPEC §7.2 gate this program carries — plus round
// trips through the Goose reader, the SPEC §5 branch-zeroing checks, the
// specified-defaults checks and the refusals the readers must agree on.
// It is test/go/main.go block for block, in Goose.
//
// The test (internal/codegen/goose, TestGooseExamplesAgreeWithTheWireGoldens)
// generates examples/ beside this file and hands every golden in as
// <name>.bin in the working directory. Prints OK and exits 0, like its twins.
import os;
import .ArmDefaults;
import .Clauses;
import .Constants;
import .Degenerate;
import .Enums;
import .Joins;
import .Render;
import .Types;
import .Wire;
import .serialize;

var failed = 0;

fn check(ok: bool, what: const u8[:]) {
    if !ok {
        print("FAILED: ", what);
        failed += 1;
    }
}

fn ok(refusal: i32, what: const u8[:]) {
    if refusal != SERIALIZE_ERROR_NONE {
        print("FAILED: ", what, ": ", serialize_refusal_name(refusal));
        failed += 1;
    }
}

// ---- the write side: one stream over one global buffer ----

var wbuf: u8[8192] = [0; 8192];
var wlen: i64 = 0;

fn fresh() -> WriteStream {
    for i in 8192 { wbuf[i] = 0; }
    wlen = 0;
    return serialize_write_stream_init(wbuf);
}

fn done(w: WriteStream&) {
    serialize_flush(w);
    wlen = serialize_bytes_processed(w);
}

fn same(a: const u8[:], b: const u8[:]) -> bool {
    if a.len != b.len { return false; }
    for i in a.len {
        if a[i] != b[i] { return false; }
    }
    return true;
}

// golden byte-compares the last flushed write against the C++-pinned golden.
fn golden(name: const u8[:]) {
    var want: u8[>..] = [];
    if !read_file(str(name, ".bin"), want) {
        check(false, str("read wire golden ", name));
        return;
    }
    check(same(wbuf[0..wlen], want), str("wire golden ", name, " — Goose bytes must equal the C++-pinned bytes"));
}

// ---- the read side ----

var rbuf: u8[8200] = [0; 8200];
var image: u8[8192] = [0; 8192];
var imageLen: i64 = 0;

// keep copies the last write into the image a reader is built over, so a
// test can corrupt it.
fn keep() {
    for i in wlen { image[i] = wbuf[i]; }
    imageLen = wlen;
}

fn readImage() -> ReadStream {
    return serialize_read_stream_init_padded(rbuf, image[0..imageLen]);
}

fn readWrite() -> ReadStream {
    keep();
    return readImage();
}

fn readFile(name: const u8[:]) -> bool {
    var data: u8[>..] = [];
    if !read_file(str(name, ".bin"), data) {
        check(false, str("read wire golden ", name));
        return false;
    }
    for i in data.len { image[i] = data[i]; }
    imageLen = data.len;
    return true;
}

fn text(dst: u8[:], s: const u8[:]) -> i32 {
    for i in s.len { dst[i] = s[i]; }
    return s.len as! i32;
}

fn testDataInstance() -> example::TestData {
    var v = example::TestData { .. };
    v.A = -100;
    v.B = 100;
    v.C = 149;
    v.D = 0x11;
    v.E = 0x22;
    v.F = 0x33;
    v.G = true;
    v.ItemsCount = 3;
    v.Items[0] = 0;
    v.Items[1] = 128;
    v.Items[2] = 255;
    v.FloatValue = 3.1415926;
    v.CompressedFloatValue = 2.5;
    v.DoubleValue = 1.0 / 3.0;
    v.Int8Value = -128;
    v.Int16Value = -32768;
    v.Uint8Value = 255;
    v.Uint16Value = 65535;
    v.Uint32Value = 4294967295;
    v.Uint64Value = 18446744073709551615;
    v.Int64Full = (-9223372036854775807 - 1);
    v.Int64Range = -999999999999;
    for i in 17 { v.FixedBytes[i] = (i * 3) as! u8; }
    v.TextLength = text(v.Text, "the quick brown fox");
    return v;
}

fn inputPacket() -> example::InputPacket {
    var v = example::InputPacket { .. };
    v.SynchronizeSequence = 7;
    v.CurrentFrame = 123456789;
    v.StartFrame = 123456780;
    v.InputsCount = 2;
    v.Inputs[0].Throttle = 0.5;
    v.Inputs[0].Fire = true;
    v.Inputs[1].StickX = -0.25;
    v.Inputs[1].Boost = true;
    return v;
}

fn rigidBody() -> example::RigidBody {
    var v = example::RigidBody { .. };
    v.Position = example::Vec3 { X: 1.5, Y: -2.5, Z: 3.25 };
    v.Orientation = example::Quat { X: 0.1, Y: 0.2, Z: 0.3, W: 0.9 };
    v.AtRest = false;
    v.LinearVelocity = example::Vec3 { X: 10.0, Y: 20.0, Z: -3.0 };
    v.AngularVelocity = example::Vec3 { X: 0.25, Y: 0.5, Z: 0.75 };
    return v;
}

fn testDefaultArmInitialization() {
    // Independent oracle: First=1 in 2 bits, count=0 in 2, marker=5 in 3.
    var input = example::DefaultChoice { .. };
    input.Type = example::DefaultChoiceTypeFirst;
    check(input.First.EntriesCount == 0, "DefaultArm constructor count is zero");
    for e in input.First.Entries {
        check(e.Retries == -1 && e.Preferred == example::WeaponRailgun, "DefaultArm constructor initializes both backing entries");
    }
    input.First.Marker = 5;
    var w = fresh();
    ok(example::WriteDefaultChoice(w, input), "write constructed DefaultChoice");
    check(serialize_bits_processed(w) == 7, "DefaultChoice writes the independent 7-bit oracle");
    done(w);
    check(wlen == 1 && wbuf[0] == 0x51, "DefaultChoice writes the independent 0x51 oracle");

    var out = example::DefaultChoice { .. };
    out.Type = example::DefaultChoiceTypeSecond;
    out.Second.EntriesCount = 1;
    out.Second.Marker = 3;
    for &e in out.Second.Entries { e = example::ProbeConfig { Retries: 91, Preferred: example::WeaponMissile }; }
    let second = out.Second;
    image[0] = 0x51;
    imageLen = 1;
    for attempt in 2 {
        // the second pass keeps the decoded First tag: both reads must
        // restore its whole backing array while the unselected Second stays
        out.First.EntriesCount = 2;
        out.First.Marker = 7;
        for &e in out.First.Entries { e = example::ProbeConfig { Retries: 123, Preferred: example::WeaponLaser }; }
        var r = readImage();
        let read = example::ReadDefaultChoice(r, out);
        check(read, "read independent DefaultChoice oracle");
        check(serialize_bits_processed(r) == 7, "DefaultChoice consumes exactly 7 bits");
        check(out.Type == example::DefaultChoiceTypeFirst && out.First.EntriesCount == 0 && out.First.Marker == 5,
              "DefaultChoice selects First and reads count zero, marker five");
        for e in out.First.Entries {
            check(e.Retries == -1 && e.Preferred == example::WeaponRailgun,
                  "DefaultChoice reconstructs both backing entries, including repeated tags");
        }
        check(out.Second == second, "DefaultChoice preserves the unselected arm and its backing storage");
    }
}

fn testExamples() {
    // ---- ShipCreate: the bool-gated flags branch, both ways ----
    {
        var v = example::ShipCreate { .. };
        v.ShipType = example::ShipTypeBomber;
        v.Position = example::QuantizedPosition { X: 1000, Y: -2000, Z: 3000 };
        v.HasFlags = true;
        v.Flags = example::ShipFlagsBoosting | example::ShipFlagsAiming;
        v.Team = example::TeamBlue;
        v.Health = 750;
        v.Thrust = 55;
        var w = fresh();
        ok(example::WriteShipCreate(w, v), "write ShipCreate");
        done(w);
        golden("shipcreate_flags");
        var out = example::ShipCreate { .. };
        var r = readWrite();
        check(example::ReadShipCreate(r, out), "read ShipCreate");
        check(out == v, "ShipCreate round-trips");

        // the untaken branch reads back ZERO (SPEC §5), into the same value
        v.HasFlags = false;
        var w2 = fresh();
        ok(example::WriteShipCreate(w2, v), "write ShipCreate no-flags");
        done(w2);
        var r2 = readWrite();
        check(example::ReadShipCreate(r2, out), "read ShipCreate no-flags");
        check(!out.HasFlags && out.Flags == 0, "untaken branch reads as zero (SPEC §5)");
    }

    // ---- RigidBody: the back-reference example, both branch sides ----
    {
        var v = rigidBody();
        var w = fresh();
        ok(example::WriteRigidBody(w, v), "write RigidBody moving");
        done(w);
        golden("rigidbody_moving");
        var out = example::RigidBody { .. };
        var r = readWrite();
        check(example::ReadRigidBody(r, out), "read RigidBody moving");
        check(out == v, "the moving branch round-trips with velocities intact");

        v.AtRest = true;
        var w2 = fresh();
        ok(example::WriteRigidBody(w2, v), "write RigidBody at rest");
        done(w2);
        golden("rigidbody_at_rest");
        var r2 = readWrite();
        check(example::ReadRigidBody(r2, out), "read RigidBody at rest");
        check(out.AtRest, "at_rest reads true");
        let zero = example::Vec3 { X: 0.0, Y: 0.0, Z: 0.0 };
        check(out.LinearVelocity == zero && out.AngularVelocity == zero,
              "velocities read as zero under the taken at-rest branch (SPEC §5)");
    }

    // ---- Chat: the string framing == classic serialize_string over N + 1 ----
    {
        var v = example::Chat { .. };
        v.TextLength = text(v.Text, "wire parity");
        var w = fresh();
        ok(example::WriteChat(w, v), "write Chat");
        done(w);
        golden("chat");
        var out = example::Chat { .. };
        var r = readWrite();
        check(example::ReadChat(r, out), "read Chat");
        check(out == v, "Chat round-trips");
    }

    // ---- ProbeHeader: const/reserved/align on the wire; corruption rejected ----
    {
        var v = example::ProbeHeader { Version: 5, ProbeId: 0x1122334455667788 };
        var w = fresh();
        ok(example::WriteProbeHeader(w, v), "write ProbeHeader");
        done(w);
        check(wbuf[0] == 0xAB, "const(0xAB, 8) leads the wire");
        golden("probe_header");
        var out = example::ProbeHeader { .. };
        var r = readWrite();
        check(example::ReadProbeHeader(r, out), "read ProbeHeader");
        check(out == v, "ProbeHeader round-trips");
        image[0] = 0xAC;
        var r2 = readImage();
        check(!example::ReadProbeHeader(r2, out), "a corrupted wire constant is REJECTED (SPEC §4.3)");
        check(serialize_error(r2) == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE, "a const mismatch latches value_out_of_range");
    }

    // ---- InputPacket: counted array of nested structs, and its pin ----
    {
        let v = inputPacket();
        var w = fresh();
        ok(example::WriteInputPacket(w, v), "write InputPacket");
        done(w);
        golden("inputpacket");
        var out = example::InputPacket { .. };
        var r = readWrite();
        check(example::ReadInputPacket(r, out), "read InputPacket");
        check(out == v, "InputPacket round-trips");
    }

    // ---- TestData: the vanilla library's own test type, and its pin ----
    {
        let v = testDataInstance();
        var w = fresh();
        ok(example::WriteTestData(w, v), "write TestData");
        done(w);
        golden("testdata");
        var out = example::TestData { .. };
        var r = readWrite();
        check(example::ReadTestData(r, out), "read TestData");
        check(out == v, "TestData round-trips — signed narrows, full-range ints, align, fixed bytes, string");
    }

    // ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
    {
        var v = example::CompressedProbe { .. };
        v.Boundary = 0.005;
        v.Offset = -4.8585;
        var w = fresh();
        ok(example::WriteCompressedProbe(w, v), "write CompressedProbe");
        done(w);
        golden("compressed_probe");
        var out = example::CompressedProbe { .. };
        var r = readWrite();
        check(example::ReadCompressedProbe(r, out), "read CompressedProbe");
        let one: f32 = 1.0;
        let ten: f32 = 10.0;
        let five: f32 = 5.0;
        let i142: f32 = 142.0;
        let maxBoundary: f32 = 1000.0;
        let maxOffset: f32 = 10000.0;
        let b: f32 = one / maxBoundary;
        let o: f32 = i142 / maxOffset;
        let ob: f32 = o * ten;
        check(out.Boundary == b * ten, "boundary reconstructs integer 1");
        check(out.Offset == ob - five, "offset reconstructs integer 142");
    }

    // ---- specified defaults: New* carries them ----
    {
        let sample = example::NewProbeSample();
        check(sample.Active, "ProbeSample.active defaults true");
        let config = example::NewProbeConfig();
        check(config.Retries == -1, "ProbeConfig.retries defaults -1");
        check(config.Preferred == example::WeaponRailgun, "ProbeConfig.preferred defaults Railgun");
        var zero = example::NewProbeSample();
        example::ZeroProbeSample(zero);
        check(!zero.Active && zero.SamplesCount == 0, "Zero sets the zero form, not the defaults (SPEC §5)");
    }

    // ---- ProbeBits: the full-range uint32/uint64 paths, C++-pinned ----
    {
        var v = example::ProbeBits { .. };
        v.Small = 0x1FF;
        v.Boundary = 0x1FFFFFFFF;
        v.Wide = 0xFEDCBA9876543210;
        v.Sensor = 4294967295;
        v.Nonce = 18446744073709551615;
        var w = fresh();
        ok(example::WriteProbeBits(w, v), "write ProbeBits");
        done(w);
        golden("probebits");
        var out = example::ProbeBits { .. };
        var r = readWrite();
        check(example::ReadProbeBits(r, out), "read ProbeBits");
        check(out == v, "ProbeBits round-trips — 9/33/64-bit and full-range paths");
    }

    // ---- ProbeCollider: first-class one-of (SPEC §4.8) ----
    {
        var v = example::ProbeCollider { .. };
        check(v.Shape.Type == example::ProbeShapeTypeNone, "the fresh value is the empty union");
        check(example::ProbeShapeMaxBits == 2 + 16, "ProbeShapeMaxBits is tag + the largest arm");
        v.Armor = 7;
        v.Shape.Type = example::ProbeShapeTypeSlab;
        v.Shape.Slab.Width = 42;
        v.Shape.Slab.Height = 9;
        v.ExtrasCount = 1;
        v.Extras[0].Type = example::ProbeShapeTypeRing;
        v.Extras[0].Ring.Radius = 777;
        var w = fresh();
        ok(example::WriteProbeCollider(w, v), "write ProbeCollider");
        done(w);
        golden("probecollider");
        var out = example::ProbeCollider { .. };
        out.Backup.Type = example::ProbeShapeTypeRing; // dirty: the read must restore None
        var r = readWrite();
        check(example::ReadProbeCollider(r, out), "read ProbeCollider");
        check(out.Armor == 7, "ProbeCollider.armor round-trips");
        check(out.Shape.Type == example::ProbeShapeTypeSlab, "the selected arm round-trips");
        check(out.Shape.Slab.Width == 42 && out.Shape.Slab.Height == 9, "the arm payload round-trips");
        check(out.Backup.Type == example::ProbeShapeTypeNone, "the None arm reads back empty");
        check(out.ExtrasCount == 1 && out.Extras[0].Type == example::ProbeShapeTypeRing && out.Extras[0].Ring.Radius == 777,
              "the union array round-trips");

        // perturb the tag: 2 bits at bit offset 8, range [0, 2]; 3 is refused
        image[1] = image[1] | 0x03;
        var bad = example::ProbeCollider { .. };
        var r2 = readImage();
        check(!example::ReadProbeCollider(r2, bad), "an out-of-range union tag is refused (SPEC §4.8)");
        // corrupt the arm payload: width rides 7 bits at bit 10, range [0, 100]
        keep();
        image[1] = image[1] | 0xFC;
        image[2] = image[2] | 0x01;
        var r3 = readImage();
        check(!example::ReadProbeCollider(r3, bad), "a corrupt union arm payload is refused (SPEC §4.8)");

        // the write side validates the tag BEFORE it rides
        var rogue = example::ProbeShape { .. };
        rogue.Type = 3;
        var w2 = fresh();
        check(example::WriteProbeShape(w2, rogue) == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE, "an out-of-set union tag writes nothing (SPEC §4.8)");
        check(serialize_bits_processed(w2) == 0, "the refused tag wrote no bit");
    }

    // ---- ProbeSample: the nested if/else wire, both ways, and §5 zeroing ----
    {
        var v = example::NewProbeSample();
        v.Orientation = 90.0;
        v.RawDelta = -5;
        v.BigDelta = -1234567890123;
        v.Weapon = example::WeaponLaser;
        v.HasTarget = true;
        v.TargetId = 777;
        v.IdleTicks = 12345; // the untaken side on the wire: must read back ZERO
        v.SamplesCount = 1;
        v.Samples[0] = 42;
        var w = fresh();
        ok(example::WriteProbeSample(w, v), "write ProbeSample active");
        done(w);
        var out = example::ProbeSample { .. };
        var r = readWrite();
        check(example::ReadProbeSample(r, out), "read ProbeSample active");
        check(out.Active && out.Weapon == example::WeaponLaser && out.HasTarget && out.TargetId == 777,
              "the taken branch round-trips, nested branch included");
        check(out.IdleTicks == 0, "the untaken else side reads as zero (SPEC §5)");
        check(out.Orientation == 90.0, "compressed float round-trips exactly at its resolution");

        v.Active = false;
        v.HasTarget = false;
        var w2 = fresh();
        ok(example::WriteProbeSample(w2, v), "write ProbeSample idle");
        done(w2);
        var r2 = readWrite();
        check(example::ReadProbeSample(r2, out), "read ProbeSample idle");
        check(!out.Active && out.IdleTicks == 12345, "the else branch round-trips");
        check(out.Weapon == example::WeaponNone && !out.HasTarget && out.TargetId == 0,
              "the whole untaken then side reads as zero, nested branch included (SPEC §5)");
    }

    // ---- ProbeArray: transitive defaults and its C++ pin ----
    {
        let fresh2 = example::NewProbeArray();
        check(fresh2.Samples[0].Active && fresh2.Samples[1].Active, "defaults reach through a fixed array");
        check(fresh2.Config.Retries == -1 && fresh2.Config.Preferred == example::WeaponRailgun,
              "defaults reach through a plain member");
        var v = example::NewProbeArray();
        v.Samples[0].Orientation = 90.0;
        v.Samples[0].RawDelta = -5;
        v.Samples[0].BigDelta = -1234567890123;
        v.Samples[0].Weapon = example::WeaponLaser;
        v.Samples[0].HasTarget = true;
        v.Samples[0].TargetId = 777;
        v.Samples[0].SamplesCount = 1;
        v.Samples[0].Samples[0] = 42;
        v.Samples[1].Active = false;
        v.Samples[1].Orientation = -45.5;
        v.Samples[1].RawDelta = 7;
        v.Samples[1].BigDelta = 99;
        v.Samples[1].IdleTicks = 1000;
        v.Samples[1].SamplesCount = 2;
        v.Samples[1].Samples[0] = 7;
        v.Samples[1].Samples[1] = 8;
        v.Config.Retries = 3;
        v.Config.Preferred = example::WeaponMissile;
        var w = fresh();
        ok(example::WriteProbeArray(w, v), "write ProbeArray");
        done(w);
        golden("probearray");
        var out = example::ProbeArray { .. };
        var r = readWrite();
        check(example::ReadProbeArray(r, out), "read ProbeArray");
        check(!out.Samples[1].Active && out.Samples[1].IdleTicks == 1000, "nested else branch round-trips");
        check(out.Samples[1].Weapon == example::WeaponNone && !out.Samples[1].HasTarget,
              "nested untaken side reads as zero (SPEC §5)");
        check(out.Config.Retries == 3 && out.Config.Preferred == example::WeaponMissile, "config round-trips");
    }

    // ---- ProbeReport: nested composition, and the widened flags wire ----
    {
        var v = example::ProbeReport { .. };
        v.Header.Version = 3;
        v.Header.ProbeId = 0xCAFEBABE;
        v.Flags = example::ProbeFlagsArmed | example::ProbeFlagsDamaged;
        v.Echo.TestA = 555;
        v.Echo.TestB = 1000;
        var w = fresh();
        ok(example::WriteProbeReport(w, v), "write ProbeReport");
        done(w);
        var out = example::ProbeReport { .. };
        var r = readWrite();
        check(example::ReadProbeReport(r, out), "read ProbeReport");
        check(out == v, "ProbeReport round-trips — a named type as an ordinary field");
        v.Flags = 512; // 1 << 9, above the widened 8-bit wire
        var w2 = fresh();
        check(example::WriteProbeReport(w2, v) == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE, "a mask bit above the flags wire width is refused");
    }

    // ---- Block: the bytes(N) framing ----
    {
        var v = example::Block { .. };
        for i in 100 { v.Data[i] = i as! u8; }
        v.DataLength = 100;
        var w = fresh();
        ok(example::WriteBlock(w, v), "write Block");
        done(w);
        var out = example::Block { .. };
        var r = readWrite();
        check(example::ReadBlock(r, out), "read Block");
        check(out == v, "Block round-trips — bytes(N) framing");
    }

    // ---- the readers agree on what they REJECT (goal 3's second half) ----
    {
        // an interior null in a string is content the read refuses
        if readFile("chat") {
            image[4] = 0; // inside the text bytes (length 9 bits, align to byte 2)
            var out = example::Chat { .. };
            var r = readImage();
            check(!example::ReadChat(r, out), "an interior null is rejected");
            check(serialize_error(r) == SERIALIZE_ERROR_EMBEDDED_NUL, "an interior null latches embedded_nul");
        }
        // a truncated stream is the stream's own error, never a content verdict
        if readFile("chat") {
            imageLen = 3;
            var out = example::Chat { .. };
            var r = readImage();
            check(!example::ReadChat(r, out), "a truncated Chat is refused");
            check(serialize_error(r) == SERIALIZE_ERROR_PAST_END, "truncation surfaces as the stream's past_end");
        }
        // a nonzero reserved bit is rejected
        if readFile("probe_header") {
            image[1] = image[1] | 0x08; // the first reserved bit above version's 3
            var out = example::ProbeHeader { .. };
            var r = readImage();
            check(!example::ReadProbeHeader(r, out), "a nonzero reserved bit is rejected");
        }
        // an out-of-range array count is refused before any element rides:
        // the 5-bit count sits at byte 18 bits 0-4, inside a complete wire
        if readFile("inputpacket") {
            image[18] = (image[18] & 0xE0) | 17; // count 2 -> 17, over [0, 16]
            var out = example::InputPacket { .. };
            var r = readImage();
            check(!example::ReadInputPacket(r, out), "an out-of-range count is refused before the loop");
            check(serialize_error(r) == SERIALIZE_ERROR_VALUE_OUT_OF_RANGE, "the count refusal latches value_out_of_range");
        }
    }
}

// ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
fn testDegenerate() {
    let vec2 = example::Vec2 { X: 1.5, Y: -2.25 };
    let spanF64 = example::SpanF64 { Values: [3.5, -4.75] };
    let spanU64 = example::SpanU64 { Values: [0xDEADBEEFCAFEBABE, 1] };
    let spanI64 = example::SpanI64 { Values: [-1234567890123, 42] };
    let spanOne = example::SpanOne { Values: [0x0123456789ABCDEF] };
    let spanChunk = example::SpanChunk { Values: [0x1111, 0x2222, 0x3333, 0x4444] };
    let spanTail = example::SpanTail { Values: [6.125, -7.0], Tail: 0xFEEDFACE };
    let spanTwice = example::SpanTwice { A: [8.5, 9.5], B: [-10.5, -11.5] };
    let trio = example::Trio { A: 0xABCDE, B: 0x12345, C: 0xFFFFF };
    let trioSole = example::TrioSole { Inner: example::Trio { A: 1, B: 2, C: 3 } };
    let trioFirst = example::TrioFirst { Inner: example::Trio { A: 0xAAAAA, B: 0x55555, C: 0xF0F0F }, Trailer: 0xBEEF };
    let straddle = example::TrioStraddle {
        Pad0: 0x0011223344556677,
        Pad1: 0x8899AABBCCDDEEFF,
        Pad2: 0xFFFFFFFFFFFFFFFF,
        Pad3: 0,
        Pad4: 0x123456789ABCDEF0,
        Pad5: 0xABCDEF,
        Inner: example::Trio { A: 0x11111, B: 0x22222, C: 0x33333 },
    };
    var w = fresh();
    ok(example::WriteVec2(w, vec2), "write Vec2");
    ok(example::WriteSpanF64(w, spanF64), "write SpanF64");
    ok(example::WriteSpanU64(w, spanU64), "write SpanU64");
    ok(example::WriteSpanI64(w, spanI64), "write SpanI64");
    ok(example::WriteSpanOne(w, spanOne), "write SpanOne");
    ok(example::WriteSpanChunk(w, spanChunk), "write SpanChunk");
    ok(example::WriteSpanTail(w, spanTail), "write SpanTail");
    ok(example::WriteSpanTwice(w, spanTwice), "write SpanTwice");
    ok(example::WriteTrio(w, trio), "write Trio");
    ok(example::WriteTrioSole(w, trioSole), "write TrioSole");
    ok(example::WriteTrioFirst(w, trioFirst), "write TrioFirst");
    ok(example::WriteTrioStraddle(w, straddle), "write TrioStraddle");
    check(serialize_bits_processed(w) == 128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408,
          "the twelve degenerate shapes ride their declared widths and nothing more");
    done(w);
    golden("degenerate");

    var rVec2 = example::Vec2 { .. };
    var rSpanF64 = example::SpanF64 { .. };
    var rSpanU64 = example::SpanU64 { .. };
    var rSpanI64 = example::SpanI64 { .. };
    var rSpanOne = example::SpanOne { .. };
    var rSpanChunk = example::SpanChunk { .. };
    var rSpanTail = example::SpanTail { .. };
    var rSpanTwice = example::SpanTwice { .. };
    var rTrio = example::Trio { .. };
    var rTrioSole = example::TrioSole { .. };
    var rTrioFirst = example::TrioFirst { .. };
    var rStraddle = example::TrioStraddle { .. };
    var r = readWrite();
    check(example::ReadVec2(r, rVec2), "read Vec2");
    check(example::ReadSpanF64(r, rSpanF64), "read SpanF64");
    check(example::ReadSpanU64(r, rSpanU64), "read SpanU64");
    check(example::ReadSpanI64(r, rSpanI64), "read SpanI64");
    check(example::ReadSpanOne(r, rSpanOne), "read SpanOne");
    check(example::ReadSpanChunk(r, rSpanChunk), "read SpanChunk");
    check(example::ReadSpanTail(r, rSpanTail), "read SpanTail");
    check(example::ReadSpanTwice(r, rSpanTwice), "read SpanTwice");
    check(example::ReadTrio(r, rTrio), "read Trio");
    check(example::ReadTrioSole(r, rTrioSole), "read TrioSole");
    check(example::ReadTrioFirst(r, rTrioFirst), "read TrioFirst");
    check(example::ReadTrioStraddle(r, rStraddle), "read TrioStraddle");
    check(rVec2 == vec2, "Vec2 round-trips");
    check(rSpanF64 == spanF64, "SpanF64 round-trips");
    check(rSpanU64 == spanU64, "SpanU64 round-trips");
    check(rSpanI64 == spanI64, "SpanI64 round-trips");
    check(rSpanOne == spanOne, "SpanOne round-trips");
    check(rSpanChunk == spanChunk, "SpanChunk round-trips");
    check(rSpanTail == spanTail, "SpanTail round-trips");
    check(rSpanTwice == spanTwice, "SpanTwice round-trips");
    check(rTrio == trio, "Trio round-trips");
    check(rTrioSole == trioSole, "TrioSole round-trips");
    check(rTrioFirst == trioFirst, "TrioFirst round-trips");
    check(rStraddle == straddle, "TrioStraddle round-trips");
}

// ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----
//
// Each shape is written to its OWN stream and flushed, and the golden is
// those concatenated; each is read back out of its own slice.

var stream: u8[>..] = [];
var off: i64 = 0;

fn emitted(w: WriteStream&, refusal: i32, name: const u8[:], bits: i64) {
    ok(refusal, str("write ", name));
    check(serialize_bits_processed(w) == bits, str(name, " rides its declared width"));
    done(w);
    for i in wlen { stream.push(wbuf[i]); }
}

fn take(bits: i64) -> ReadStream {
    let n = (bits + 7) / 8;
    for i in n { image[i] = stream[off + i]; }
    imageLen = n;
    off += n;
    return readImage();
}

fn goldenStream(name: const u8[:]) {
    var want: u8[>..] = [];
    if !read_file(str(name, ".bin"), want) {
        check(false, str("read wire golden ", name));
        return;
    }
    check(same(stream, want), str("wire golden ", name, " — Goose bytes must equal the C++-pinned bytes"));
}

fn w13At(c: i32) -> example::W13 {
    var v = example::W13 { .. };
    v.ItemsCount = c;
    for i in c { v.Items[i] = (8191 - i * 733) as! u16; }
    return v;
}
fn w17At(c: i32) -> example::W17 {
    var v = example::W17 { .. };
    v.ItemsCount = c;
    for i in c { v.Items[i] = (131071 - i * 11117) as! u32; }
    return v;
}
fn w26At(c: i32) -> example::W26 {
    var v = example::W26 { .. };
    v.ItemsCount = c;
    for i in c { v.Items[i] = (67108863 - i * 5555555) as! u32; }
    return v;
}
fn w1At(c: i32) -> example::W1 {
    var v = example::W1 { .. };
    v.ItemsCount = c;
    for i in c { v.Items[i] = (i % 2) as! u8; }
    return v;
}
fn w52At(c: i32) -> example::W52 {
    var v = example::W52 { .. };
    v.ItemsCount = c;
    for i in c { v.Items[i] = 4503599627370495 - (i as! u64) * 123456789; }
    return v;
}
fn w50At(c: i32) -> example::W50 {
    var v = example::W50 { .. };
    v.ItemsCount = c;
    for i in c { v.Items[i] = 1125899906842623 - (i as! u64) * 987654321; }
    return v;
}
fn triAt(c: i32) -> example::ArrTri3 {
    var v = example::ArrTri3 { .. };
    v.ItemsCount = c;
    for i in c { v.Items[i] = example::Tri3 { A: (i % 2) as! u32, B: (i % 4) as! u32 }; }
    return v;
}
fn nestedAt(c: i32) -> example::ArrNested {
    var v = example::ArrNested { .. };
    v.Lead = 21;
    v.Tail = 5;
    v.ItemsCount = c;
    for i in c { v.Items[i] = example::Eleven { A: (i % 8) as! u32, B: (200 - i * 7) as! u32 }; }
    return v;
}
fn strsAt(k: i64) -> example::Strs {
    var v = example::Strs { .. };
    v.Lead = 21;
    v.Tail = 5;
    if k == 1 {
        v.SLength = text(v.S, "abcdefgh");
        for i in 8 { v.B[i] = (0xF0 + i) as! u8; }
        v.BLength = 8;
    } else if k == 2 {
        v.SLength = text(v.S, "xyz");
        for i in 3 { v.B[i] = (i + 1) as! u8; }
        v.BLength = 3;
    }
    return v;
}

fn testClauses() {
    stream.clear();
    off = 0;
    let w13Counts: i32[7] = [0, 1, 3, 4, 5, 7, 12];
    let w17Counts: i32[6] = [0, 1, 2, 3, 4, 9];
    let w26Counts: i32[5] = [0, 1, 2, 3, 6];
    let w1Counts: i32[6] = [0, 1, 3, 4, 5, 20];
    let triCounts: i32[6] = [0, 1, 3, 4, 5, 10];
    let strsBits: i64[3] = [27, 155, 75];

    for c in w13Counts { var v = w13At(c); var w = fresh(); emitted(w, example::WriteW13(w, v), "W13", 4 + 13 * c); }
    for c in w17Counts { var v = w17At(c); var w = fresh(); emitted(w, example::WriteW17(w, v), "W17", 4 + 17 * c); }
    for c in w26Counts { var v = w26At(c); var w = fresh(); emitted(w, example::WriteW26(w, v), "W26", 3 + 26 * c); }
    for c in w1Counts { var v = w1At(c); var w = fresh(); emitted(w, example::WriteW1(w, v), "W1", 5 + c); }
    for c: i32 in 0..4 { var v = w52At(c); var w = fresh(); emitted(w, example::WriteW52(w, v), "W52", 2 + 52 * c); }
    for c: i32 in 0..4 { var v = w50At(c); var w = fresh(); emitted(w, example::WriteW50(w, v), "W50", 2 + 50 * c); }
    var f13 = example::F13 { .. };
    for i in 7 { f13.Items[i] = (8191 - i * 911) as! u16; }
    { var w = fresh(); emitted(w, example::WriteF13(w, f13), "F13", 91); }
    for c in triCounts { var v = triAt(c); var w = fresh(); emitted(w, example::WriteArrTri3(w, v), "ArrTri3", 4 + 3 * c); }
    var arrEleven = example::ArrEleven { .. };
    for i in 9 { arrEleven.Items[i] = example::Eleven { A: (i % 8) as! u32, B: (255 - i * 17) as! u32 }; }
    { var w = fresh(); emitted(w, example::WriteArrEleven(w, arrEleven), "ArrEleven", 99); }
    // lead 5 + tag 2 + tail 7: a zero-bit arm behind a tag costs the tag
    var empties: example::HoldsEmptyUnion[3] = [example::HoldsEmptyUnion { .. }; 3];
    for i in 3 {
        empties[i].Lead = 21;
        empties[i].Tail = 99;
        empties[i].U.Type = i as! example::EmptyUnionType;
    }
    for i in 3 { var w = fresh(); emitted(w, example::WriteHoldsEmptyUnion(w, empties[i]), "HoldsEmptyUnion", 14); }
    for k in 3 { var v = strsAt(k); var w = fresh(); emitted(w, example::WriteStrs(w, v), "Strs", strsBits[k]); }
    for c: i32 in 0..5 { var v = nestedAt(c); var w = fresh(); emitted(w, example::WriteArrNested(w, v), "ArrNested", 11 + 11 * c); }
    let sole = example::Sole { Only: 5555 };
    { var w = fresh(); emitted(w, example::WriteSole(w, sole), "Sole", 13); }
    goldenStream("clauses");

    for c in w13Counts { var o = example::W13 { .. }; var r = take(4 + 13 * c); check(example::ReadW13(r, o), "read W13"); check(o == w13At(c), "W13 round-trips"); }
    for c in w17Counts { var o = example::W17 { .. }; var r = take(4 + 17 * c); check(example::ReadW17(r, o), "read W17"); check(o == w17At(c), "W17 round-trips"); }
    for c in w26Counts { var o = example::W26 { .. }; var r = take(3 + 26 * c); check(example::ReadW26(r, o), "read W26"); check(o == w26At(c), "W26 round-trips"); }
    for c in w1Counts { var o = example::W1 { .. }; var r = take(5 + c); check(example::ReadW1(r, o), "read W1"); check(o == w1At(c), "W1 round-trips"); }
    for c: i32 in 0..4 { var o = example::W52 { .. }; var r = take(2 + 52 * c); check(example::ReadW52(r, o), "read W52"); check(o == w52At(c), "W52 round-trips"); }
    for c: i32 in 0..4 { var o = example::W50 { .. }; var r = take(2 + 50 * c); check(example::ReadW50(r, o), "read W50"); check(o == w50At(c), "W50 round-trips"); }
    { var o = example::F13 { .. }; var r = take(91); check(example::ReadF13(r, o), "read F13"); check(o == f13, "F13 round-trips"); }
    for c in triCounts { var o = example::ArrTri3 { .. }; var r = take(4 + 3 * c); check(example::ReadArrTri3(r, o), "read ArrTri3"); check(o == triAt(c), "ArrTri3 round-trips"); }
    { var o = example::ArrEleven { .. }; var r = take(99); check(example::ReadArrEleven(r, o), "read ArrEleven"); check(o == arrEleven, "ArrEleven round-trips"); }
    for i in 3 { var o = example::HoldsEmptyUnion { .. }; var r = take(14); check(example::ReadHoldsEmptyUnion(r, o), "read HoldsEmptyUnion"); check(o == empties[i], "HoldsEmptyUnion round-trips"); }
    for k in 3 { var o = example::Strs { .. }; var r = take(strsBits[k]); check(example::ReadStrs(r, o), "read Strs"); check(o == strsAt(k), "Strs round-trips"); }
    for c: i32 in 0..5 { var o = example::ArrNested { .. }; var r = take(11 + 11 * c); check(example::ReadArrNested(r, o), "read ArrNested"); check(o == nestedAt(c), "ArrNested round-trips"); }
    { var o = example::Sole { .. }; var r = take(13); check(example::ReadSole(r, o), "read Sole"); check(o == sole, "Sole round-trips"); }
    check(off == stream.len, "the clauses reads consume the whole golden");
}

fn pick(f: bool, t: i64, e: i64) -> i64 { if f { t } else { e } }

fn armArrayAt(flag: bool, count: i32) -> example::ArmArray {
    var v = example::ArmArray { .. };
    v.Lead = 21;
    v.Flag = flag;
    v.ItemsCount = count;
    v.B = 300;
    v.Tail = 99;
    for i in count { v.Items[i] = (8191 - i * 777) as! u16; }
    return v;
}

fn unevenAt(k: i64) -> example::HoldsUneven {
    var v = example::HoldsUneven { .. };
    v.Lead = 21;
    v.Tail = 1500;
    if k == 1 {
        v.U.Type = example::UnevenTypeNarrow;
        v.U.Narrow.N = 5;
    } else if k == 2 {
        v.U.Type = example::UnevenTypeWide;
        v.U.Wide.W = 123456789012;
    }
    return v;
}

fn arrUnevenAt(c: i32) -> example::ArrUneven {
    var v = example::ArrUneven { .. };
    v.Lead = 21;
    v.Tail = 5;
    v.ItemsCount = c;
    for i in c {
        if i % 2 == 0 {
            v.Items[i].Type = example::UnevenTypeNarrow;
            v.Items[i].Narrow.N = (i % 8) as! u32;
        } else {
            v.Items[i].Type = example::UnevenTypeWide;
            v.Items[i].Wide.W = 99887766554 + (i as! u64);
        }
    }
    return v;
}

fn regainAt(c: i32, sl: i32) -> example::RegainAfterAlign {
    var v = example::RegainAfterAlign { .. };
    v.Lead = 21;
    v.ItemsCount = c;
    v.SLength = sl;
    v.P = 0xDEADBEEF;
    v.Q = (1 << 29) - 7;
    v.R = (1 << 19) - 3;
    v.Tail = 9;
    if sl != 0 { text(v.S, "wxyz"); }
    for i in c { v.Items[i] = (8191 - i * 999) as! u16; }
    return v;
}

fn testJoins() {
    let slens: i32[2] = [0, 4];
    stream.clear();
    off = 0;
    // every branch is written on BOTH arms; the expected value after a round
    // trip has its untaken side at zero (SPEC §5)

    for f in 2 {
        let flag = f != 0;
        let agree = example::ArmsAgree { Lead: 21, Flag: flag, A: 1234, B: 1500, Tail: 99 };
        { var w = fresh(); emitted(w, example::WriteArmsAgree(w, agree), "ArmsAgree", 24); }
        let disagree = example::ArmsDisagree { Lead: 21, Flag: flag, A: 1234, B: 5, Tail: 99 };
        { var w = fresh(); emitted(w, example::WriteArmsDisagree(w, disagree), "ArmsDisagree", pick(flag, 24, 16)); }
        let armEmpty = example::ArmEmpty { Lead: 21, Flag: flag, A: 456789, Tail: 99 };
        { var w = fresh(); emitted(w, example::WriteArmEmpty(w, armEmpty), "ArmEmpty", pick(flag, 32, 13)); }
        var alignStr = example::ArmAlign { Lead: 21, Flag: flag, B: 1000, Tail: 99, .. };
        alignStr.SLength = text(alignStr.S, "abcd");
        { var w = fresh(); emitted(w, example::WriteArmAlign(w, alignStr), "ArmAlign", pick(flag, 55, 23)); }
        let alignEmpty = example::ArmAlign { Lead: 21, Flag: flag, B: 1000, Tail: 99, .. };
        { var w = fresh(); emitted(w, example::WriteArmAlign(w, alignEmpty), "ArmAlignEmptyStr", 23); }
    }
    for o in 2 {
        for i in 2 {
            let v = example::ArmsNested { Lead: 5, Outer: o != 0, Inner: i != 0, X: 500000000, Y: 17, Z: 4000, Tail: 33 };
            let bits = if o != 0 { if i != 0 { 40 } else { 16 } } else { 23 };
            var w = fresh();
            emitted(w, example::WriteArmsNested(w, v), "ArmsNested", bits);
        }
    }
    for f in 2 {
        for c: i32 in 0..4 {
            let v = armArrayAt(f != 0, c);
            let bits = if f != 0 { 15 + 13 * c } else { 22 };
            var w = fresh();
            emitted(w, example::WriteArmArray(w, v), "ArmArray", bits);
        }
    }
    let unevenBits: i64[3] = [18, 21, 55];
    for k in 3 { var v = unevenAt(k); var w = fresh(); emitted(w, example::WriteHoldsUneven(w, v), "HoldsUneven", unevenBits[k]); }
    let unevenItemBits: i64[4] = [0, 5, 44, 49];
    for c: i32 in 0..4 { var v = arrUnevenAt(c); var w = fresh(); emitted(w, example::WriteArrUneven(w, v), "ArrUneven", 10 + unevenItemBits[c]); }
    for c: i32 in 0..4 {
        for sl in slens {
            var v = regainAt(c, sl);
            let afterAlign = ((5 + 2 + 13 * c + 3) + 7) / 8 * 8;
            var w = fresh();
            emitted(w, example::WriteRegainAfterAlign(w, v), "RegainAfterAlign", afterAlign + 8 * sl + 84);
        }
    }
    goldenStream("joins");

    for f in 2 {
        let flag = f != 0;
        {
            var o = example::ArmsAgree { .. };
            var r = take(24);
            check(example::ReadArmsAgree(r, o), "read ArmsAgree");
            var want = example::ArmsAgree { Lead: 21, Flag: flag, Tail: 99, .. };
            if flag { want.A = 1234; } else { want.B = 1500; }
            check(o == want, "ArmsAgree round-trips (untaken sides zeroed, SPEC §5)");
        }
        {
            var o = example::ArmsDisagree { .. };
            var r = take(pick(flag, 24, 16));
            check(example::ReadArmsDisagree(r, o), "read ArmsDisagree");
            var want = example::ArmsDisagree { Lead: 21, Flag: flag, Tail: 99, .. };
            if flag { want.A = 1234; } else { want.B = 5; }
            check(o == want, "ArmsDisagree round-trips (untaken sides zeroed, SPEC §5)");
        }
        {
            var o = example::ArmEmpty { .. };
            var r = take(pick(flag, 32, 13));
            check(example::ReadArmEmpty(r, o), "read ArmEmpty");
            var want = example::ArmEmpty { Lead: 21, Flag: flag, Tail: 99, .. };
            if flag { want.A = 456789; }
            check(o == want, "ArmEmpty round-trips (untaken sides zeroed, SPEC §5)");
        }
        {
            var o = example::ArmAlign { .. };
            var r = take(pick(flag, 55, 23));
            check(example::ReadArmAlign(r, o), "read ArmAlign");
            var want = example::ArmAlign { Lead: 21, Flag: flag, Tail: 99, .. };
            if flag { want.SLength = text(want.S, "abcd"); } else { want.B = 1000; }
            check(o == want, "ArmAlign round-trips (untaken sides zeroed, SPEC §5)");
        }
        {
            var o = example::ArmAlign { .. };
            var r = take(23);
            check(example::ReadArmAlign(r, o), "read ArmAlignEmptyStr");
            var want = example::ArmAlign { Lead: 21, Flag: flag, Tail: 99, .. };
            if !flag { want.B = 1000; }
            check(o == want, "ArmAlignEmptyStr round-trips (untaken sides zeroed, SPEC §5)");
        }
    }
    for oi in 2 {
        for ii in 2 {
            let outer = oi != 0;
            let inner = ii != 0;
            let bits = if outer { if inner { 40 } else { 16 } } else { 23 };
            var o = example::ArmsNested { .. };
            var r = take(bits);
            check(example::ReadArmsNested(r, o), "read ArmsNested");
            var want = example::ArmsNested { Lead: 5, Outer: outer, Tail: 33, .. };
            if outer {
                want.Inner = inner;
                if inner { want.X = 500000000; } else { want.Y = 17; }
            } else {
                want.Z = 4000;
            }
            check(o == want, "ArmsNested round-trips (untaken sides zeroed, SPEC §5)");
        }
    }
    for f in 2 {
        for c: i32 in 0..4 {
            let flag = f != 0;
            var o = example::ArmArray { .. };
            var r = take(if flag { 15 + 13 * c } else { 22 });
            check(example::ReadArmArray(r, o), "read ArmArray");
            var want = example::ArmArray { Lead: 21, Flag: flag, Tail: 99, .. };
            if flag {
                want.ItemsCount = c;
                for i in c { want.Items[i] = (8191 - i * 777) as! u16; }
            } else {
                want.B = 300;
            }
            check(o == want, "ArmArray round-trips (untaken sides zeroed, SPEC §5)");
        }
    }
    for k in 3 {
        var o = example::HoldsUneven { .. };
        var r = take(unevenBits[k]);
        check(example::ReadHoldsUneven(r, o), "read HoldsUneven");
        check(o == unevenAt(k), "HoldsUneven round-trips");
    }
    for c: i32 in 0..4 {
        var o = example::ArrUneven { .. };
        var r = take(10 + unevenItemBits[c]);
        check(example::ReadArrUneven(r, o), "read ArrUneven");
        check(o == arrUnevenAt(c), "ArrUneven round-trips");
    }
    for c: i32 in 0..4 {
        for sl in slens {
            let afterAlign = ((5 + 2 + 13 * c + 3) + 7) / 8 * 8;
            var o = example::RegainAfterAlign { .. };
            var r = take(afterAlign + 8 * sl + 84);
            check(example::ReadRegainAfterAlign(r, o), "read RegainAfterAlign");
            check(o == regainAt(c, sl), "RegainAfterAlign round-trips");
        }
    }
    check(off == stream.len, "the joins reads consume the whole golden");
}

fn main() {
    testDefaultArmInitialization();
    testExamples();
    testDegenerate();
    testClauses();
    testJoins();
    if failed != 0 { exit(1); }
    print("OK");
}
