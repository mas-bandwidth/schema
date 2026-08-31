// The C# cross-language wire test: the generated C# writes the SAME pinned
// instances the C++ test pins in testdata/wire/*.bin and byte-compares against
// those files — cross-language wire identity is the §7.2 gate this binary
// carries (goal 3: byte-identical wire output across all targets, and the
// readers agree on what they reject). Plus round-trips through the C# reader,
// the §5 branch-zeroing checks, and the specified-defaults checks.
//
// Prints OK and exits 0, exactly like its C++, Go and Rust twins. Run from
// test/cs (the Makefile does): the wire goldens are at ../../testdata/wire;
// a fallback resolves them from the build output directory so `dotnet run`
// stays honest whatever the working directory.
//
// Mirrors test/go/main.go block for block where applicable. The Go test's
// typed-nil refusal case has no C# twin: null IS the None terminator here (a
// C# null carries no type), so the only out-of-set value is a user-defined
// Generated
// classes have no operator==, so per-type Eq helpers compare field by field.
//
// Error semantics under test: wire functions return bool; a schema validation
// refusal (wrong constant, nonzero reserved, interior null, a generated write
// guard) returns false WITHOUT latching — stream.Error stays None — while
// stream and runtime-range failures latch on stream.Error. The rejection
// suite asserts that distinction explicitly.

using System;
using System.IO;
using System.Text;
using Example;
using Serialize;
using static Example.Schema;

static class Program
{
    static bool failed;
    static string wireDir;

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed = true;
        }
    }

    // GoldenWire byte-compares written wire against the C++-pinned golden.
    static void GoldenWire(string name, ReadOnlySpan<byte> data)
    {
        byte[] golden;
        try
        {
            golden = File.ReadAllBytes(Path.Combine(wireDir, name + ".bin"));
        }
        catch (Exception e)
        {
            Console.WriteLine("FAILED: read wire golden " + name + ": " + e.Message);
            failed = true;
            return;
        }
        Check(data.SequenceEqual(golden), "wire golden " + name + " — C# bytes must equal the C++-pinned bytes");
    }

    static byte[] ReadGolden(string name)
    {
        try
        {
            return File.ReadAllBytes(Path.Combine(wireDir, name + ".bin"));
        }
        catch (Exception e)
        {
            Console.WriteLine("FAILED: read wire golden " + name + ": " + e.Message);
            failed = true;
            return null;
        }
    }




    static string FindTestDataDir(string kind)
    {
        string[] candidates =
        {
            Path.Combine("..", "..", "testdata", kind),
            Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "testdata", kind),
        };
        foreach (string candidate in candidates)
        {
            if (Directory.Exists(candidate))
            {
                return candidate;
            }
        }
        return candidates[0];
    }

    static WriteStream NewWriteStream()
    {
        return new WriteStream(new byte[2048]);
    }

    // Data flushes the stream and copies the written wire out.
    static byte[] Data(WriteStream ws)
    {
        ws.Flush();
        return ws.Data.ToArray();
    }

    static void SetText(byte[] buffer, string text)
    {
        Encoding.ASCII.GetBytes(text).CopyTo(buffer, 0);
    }

    // ---- per-type equality: classes have no ==, so fields compare directly ----

    static bool EqBytes(byte[] a, byte[] b)
    {
        return a.AsSpan().SequenceEqual(b);
    }

    static bool EqVec3(Vec3 a, Vec3 b)
    {
        return a.X == b.X && a.Y == b.Y && a.Z == b.Z;
    }

    static bool EqQuat(Quat a, Quat b)
    {
        return a.X == b.X && a.Y == b.Y && a.Z == b.Z && a.W == b.W;
    }

    static bool EqQuantizedPosition(QuantizedPosition a, QuantizedPosition b)
    {
        return a.X == b.X && a.Y == b.Y && a.Z == b.Z;
    }

    static bool EqQuantizedVelocity(QuantizedVelocity a, QuantizedVelocity b)
    {
        return a.X == b.X && a.Y == b.Y && a.Z == b.Z;
    }

    static bool EqQuantizedRotation(QuantizedRotation a, QuantizedRotation b)
    {
        return a.X == b.X && a.Y == b.Y && a.Z == b.Z && a.W == b.W;
    }

    static bool EqShipCreate(ShipCreate a, ShipCreate b)
    {
        return a.ShipType == b.ShipType && EqQuantizedPosition(a.Position, b.Position)
            && EqQuantizedRotation(a.Rotation, b.Rotation) && EqQuantizedVelocity(a.LinearVelocity, b.LinearVelocity)
            && a.HasFlags == b.HasFlags && a.Flags == b.Flags && a.Team == b.Team
            && a.Health == b.Health && a.Thrust == b.Thrust;
    }

    static bool EqRigidBody(RigidBody a, RigidBody b)
    {
        return EqVec3(a.Position, b.Position) && EqQuat(a.Orientation, b.Orientation) && a.AtRest == b.AtRest
            && EqVec3(a.LinearVelocity, b.LinearVelocity) && EqVec3(a.AngularVelocity, b.AngularVelocity);
    }

    static bool EqChat(Chat a, Chat b)
    {
        return a.TextLength == b.TextLength && EqBytes(a.Text, b.Text);
    }

    static bool EqBlock(Block a, Block b)
    {
        return a.DataLength == b.DataLength && EqBytes(a.Data, b.Data);
    }

    static bool EqProbeHeader(ProbeHeader a, ProbeHeader b)
    {
        return a.Version == b.Version && a.ProbeId == b.ProbeId;
    }

    static bool EqInput(Input a, Input b)
    {
        return a.StickX == b.StickX && a.StickY == b.StickY && a.Throttle == b.Throttle
            && a.Yaw == b.Yaw && a.Pitch == b.Pitch && a.Fire == b.Fire && a.AltFire == b.AltFire
            && a.Boost == b.Boost && a.Brake == b.Brake && a.Aim == b.Aim && a.LockOn == b.LockOn
            && a.Zoom == b.Zoom && a.Ping == b.Ping;
    }

    static bool EqInputPacket(InputPacket a, InputPacket b)
    {
        if (a.SynchronizeSequence != b.SynchronizeSequence || a.CurrentFrame != b.CurrentFrame
            || a.StartFrame != b.StartFrame || a.InputsCount != b.InputsCount)
        {
            return false;
        }
        for (int i = 0; i < a.Inputs.Length; i++)
        {
            if (!EqInput(a.Inputs[i], b.Inputs[i]))
            {
                return false;
            }
        }
        return true;
    }

    static bool EqTestData(TestData a, TestData b)
    {
        return a.A == b.A && a.B == b.B && a.C == b.C && a.D == b.D && a.E == b.E && a.F == b.F
            && a.G == b.G && a.ItemsCount == b.ItemsCount && a.Items.AsSpan().SequenceEqual(b.Items)
            && a.FloatValue == b.FloatValue && a.CompressedFloatValue == b.CompressedFloatValue
            && a.DoubleValue == b.DoubleValue && a.Int8Value == b.Int8Value && a.Int16Value == b.Int16Value
            && a.Uint8Value == b.Uint8Value && a.Uint16Value == b.Uint16Value && a.Uint32Value == b.Uint32Value
            && a.Uint64Value == b.Uint64Value && a.Int64Full == b.Int64Full && a.Int64Range == b.Int64Range
            && EqBytes(a.FixedBytes, b.FixedBytes) && a.TextLength == b.TextLength && EqBytes(a.Text, b.Text);
    }

    static bool EqProbeBits(ProbeBits a, ProbeBits b)
    {
        return a.Small == b.Small && a.Boundary == b.Boundary && a.Wide == b.Wide
            && a.Sensor == b.Sensor && a.Nonce == b.Nonce;
    }

    static bool EqTest(Test a, Test b)
    {
        return a.TestA == b.TestA && a.TestB == b.TestB && a.TestC == b.TestC && a.TestD == b.TestD;
    }

    static bool EqProbeReport(ProbeReport a, ProbeReport b)
    {
        return EqProbeHeader(a.Header, b.Header) && a.Flags == b.Flags && EqTest(a.Echo, b.Echo);
    }





    // TestDataInstance is the deterministic TestData the C++ test pins — the
    // values must stay mirrored on both sides.
    static TestData TestDataInstance()
    {
        TestData input = new TestData();
        input.A = -100;
        input.B = 100;
        input.C = 149;
        input.D = 0x11;
        input.E = 0x22;
        input.F = 0x33;
        input.G = true;
        input.ItemsCount = 3;
        input.Items[0] = 0;
        input.Items[1] = 128;
        input.Items[2] = 255;
        input.FloatValue = 3.1415926f;
        input.CompressedFloatValue = 2.5f;
        input.DoubleValue = 1.0 / 3.0;
        input.Int8Value = -128;
        input.Int16Value = -32768;
        input.Uint8Value = 255;
        input.Uint16Value = 65535;
        input.Uint32Value = 4294967295;
        input.Uint64Value = 18446744073709551615;
        input.Int64Full = long.MinValue; // -9223372036854775808, the Go test's literal
        input.Int64Range = -999999999999;
        for (int i = 0; i < input.FixedBytes.Length; i++)
        {
            input.FixedBytes[i] = (byte)(i * 3);
        }
        SetText(input.Text, "the quick brown fox");
        input.TextLength = 19;
        return input;
    }

    static int Main()
    {
        wireDir = FindTestDataDir("wire");

        // ---- ShipCreate: the bool-gated flags branch, both ways ----
        {
            ShipCreate input = new ShipCreate();
            input.ShipType = ShipType.Bomber;
            input.Position.X = 1000;
            input.Position.Y = -2000;
            input.Position.Z = 3000;
            input.HasFlags = true;
            input.Flags = ShipFlagsBoosting | ShipFlagsAiming;
            input.Team = Team.Blue;
            input.Health = 750;
            input.Thrust = 55;

            WriteStream ws = NewWriteStream();
            Check(WriteShipCreate(ws, input), "write ShipCreate");
            byte[] wire = Data(ws);
            GoldenWire("shipcreate_flags", wire);

            ShipCreate output = new ShipCreate();
            ReadStream rs = new ReadStream(wire);
            Check(ReadShipCreate(rs, output), "read ShipCreate");
            Check(EqShipCreate(output, input), "ShipCreate round-trips");

            // untaken branch: flags must read back ZERO (SPEC §5) — into the same
            // output value, so stale flags would be caught
            input.HasFlags = false;
            WriteStream ws2 = NewWriteStream();
            Check(WriteShipCreate(ws2, input), "write ShipCreate no-flags");
            byte[] wire2 = Data(ws2);
            ReadStream rs2 = new ReadStream(wire2);
            Check(ReadShipCreate(rs2, output), "read ShipCreate no-flags");
            Check(!output.HasFlags && output.Flags == 0, "untaken branch reads as zero (SPEC §5)");
        }

        // ---- RigidBody: the back-reference example, both branch sides ----
        {
            RigidBody input = new RigidBody();
            input.Position.X = 1.5;
            input.Position.Y = -2.5;
            input.Position.Z = 3.25;
            input.Orientation.X = 0.1;
            input.Orientation.Y = 0.2;
            input.Orientation.Z = 0.3;
            input.Orientation.W = 0.9;
            input.AtRest = false;
            input.LinearVelocity.X = 10.0;
            input.LinearVelocity.Y = 20.0;
            input.LinearVelocity.Z = -3.0;
            input.AngularVelocity.X = 0.25;
            input.AngularVelocity.Y = 0.5;
            input.AngularVelocity.Z = 0.75;

            WriteStream ws = NewWriteStream();
            Check(WriteRigidBody(ws, input), "write RigidBody moving");
            GoldenWire("rigidbody_moving", Data(ws));

            input.AtRest = true;
            WriteStream ws2 = NewWriteStream();
            Check(WriteRigidBody(ws2, input), "write RigidBody at rest");
            byte[] wire2 = Data(ws2);
            GoldenWire("rigidbody_at_rest", wire2);

            // the at-rest read must ZERO both velocities (SPEC §5), even though
            // the written value had them set
            RigidBody output = new RigidBody();
            ReadStream rs = new ReadStream(wire2);
            Check(ReadRigidBody(rs, output), "read RigidBody at rest");
            Check(output.AtRest, "at_rest reads true");
            Check(EqVec3(output.LinearVelocity, new Vec3()) && EqVec3(output.AngularVelocity, new Vec3()),
                "velocities read as zero under the taken at-rest branch (SPEC §5)");
        }

        // ---- Chat: the string framing == classic serialize_string over N + 1 ----
        {
            Chat input = new Chat();
            SetText(input.Text, "wire parity");
            input.TextLength = 11;

            WriteStream ws = NewWriteStream();
            Check(WriteChat(ws, input), "write Chat");
            byte[] wire = Data(ws);
            GoldenWire("chat", wire);

            Chat output = new Chat();
            ReadStream rs = new ReadStream(wire);
            Check(ReadChat(rs, output), "read Chat");
            Check(EqChat(output, input), "Chat round-trips");
        }

        // ---- ProbeHeader: const/reserved/align on the wire; corruption rejected ----
        {
            ProbeHeader input = new ProbeHeader();
            input.Version = 5;
            input.ProbeId = 0x1122334455667788;
            WriteStream ws = NewWriteStream();
            Check(WriteProbeHeader(ws, input), "write ProbeHeader");
            byte[] wire = Data(ws);
            Check(wire[0] == 0xAB, "const(0xAB, 8) leads the wire");
            GoldenWire("probe_header", wire);

            ProbeHeader output = new ProbeHeader();
            ReadStream rs = new ReadStream(wire);
            Check(ReadProbeHeader(rs, output), "read ProbeHeader");
            Check(EqProbeHeader(output, input), "ProbeHeader round-trips");

            byte[] corrupt = (byte[])wire.Clone();
            corrupt[0] = 0xAC;
            ReadStream rs2 = new ReadStream(corrupt);
            Check(!ReadProbeHeader(rs2, output), "a corrupted wire constant is REJECTED (SPEC §4.3)");
        }

        // ---- InputPacket: counted array of nested classes ----
        {
            InputPacket input = new InputPacket();
            input.SynchronizeSequence = 7;
            input.CurrentFrame = 123456789;
            input.StartFrame = 123456780;
            input.InputsCount = 2;
            input.Inputs[0].Throttle = 0.5f;
            input.Inputs[0].Fire = true;
            input.Inputs[1].StickX = -0.25f;
            input.Inputs[1].Boost = true;

            WriteStream ws = NewWriteStream();
            Check(WriteInputPacket(ws, input), "write InputPacket");
            byte[] wire = Data(ws);

            InputPacket output = new InputPacket();
            ReadStream rs = new ReadStream(wire);
            Check(ReadInputPacket(rs, output), "read InputPacket");
            Check(EqInputPacket(output, input), "InputPacket round-trips");
        }

        // ---- TestData: the vanilla library's own test type, deterministic values ----
        {
            TestData input = TestDataInstance();

            WriteStream ws = NewWriteStream();
            Check(WriteTestData(ws, input), "write TestData");
            byte[] wire = Data(ws);

            TestData output = new TestData();
            ReadStream rs = new ReadStream(wire);
            Check(ReadTestData(rs, output), "read TestData");
            Check(EqTestData(output, input), "TestData round-trips — signed narrows, full-range ints, align, fixed bytes, string");
        }

        // ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
        // 0.005 quantizes to 1 under the float32 two-rounding law (a fused or
        // double build says 0); -4.8585 over the non-zero-min range quantizes
        // to 142 (a double build says 141). Same pinned instance as the C++
        // leg, against the same golden.
        {
            CompressedProbe input = new CompressedProbe();
            input.Boundary = 0.005f;
            input.Offset = -4.8585f;

            WriteStream ws = NewWriteStream();
            Check(WriteCompressedProbe(ws, input), "write CompressedProbe");
            byte[] wire = Data(ws);
            GoldenWire("compressed_probe", wire);

            CompressedProbe output = new CompressedProbe();
            ReadStream rs = new ReadStream(wire);
            Check(ReadCompressedProbe(rs, output), "read CompressedProbe");
            // through locals, not constants: the C# compiler may fold
            // constant float expressions in higher precision than the
            // float32 per-op arithmetic the reader performs
            float maxIntBoundary = 1000.0f;
            float maxIntOffset = 10000.0f;
            Check(output.Boundary == 1.0f / maxIntBoundary * 10.0f, "boundary reconstructs integer 1");
            Check(output.Offset == 142.0f / maxIntOffset * 10.0f - 5.0f, "offset reconstructs integer 142");
        }

        // ---- specified defaults: construction carries them; Zero* is the zero form ----
        {
            ProbeSample sample = new ProbeSample();
            Check(sample.Active, "ProbeSample.active defaults true");
            ZeroProbeSample(sample);
            Check(!sample.Active, "the §5 zero form stays zero (Zero* does not reapply defaults)");
            ProbeConfig config = new ProbeConfig();
            Check(config.Retries == -1, "ProbeConfig.retries defaults -1");
            Check(config.Preferred == Weapon.Railgun, "ProbeConfig.preferred defaults Railgun");
        }

        // ---- ProbeBits: the full-range uint/ulong paths, C++-pinned ----
        {
            ProbeBits input = new ProbeBits();
            input.Small = 0x1FF;
            input.Boundary = 0x1FFFFFFFF;
            input.Wide = 0xFEDCBA9876543210;
            input.Sensor = 4294967295;
            input.Nonce = 18446744073709551615;

            WriteStream ws = NewWriteStream();
            Check(WriteProbeBits(ws, input), "write ProbeBits");
            byte[] wire = Data(ws);
            GoldenWire("probebits", wire);

            ProbeBits output = new ProbeBits();
            ReadStream rs = new ReadStream(wire);
            Check(ReadProbeBits(rs, output), "read ProbeBits");
            Check(EqProbeBits(output, input), "ProbeBits round-trips — 9/33/64-bit and full-range paths");
        }

        // ---- ProbeCollider: first-class one-of (SPEC §4.8) — C++-pinned wire,
        // round trip, the None arm, an array of unions, and the refusal
        // negative controls ----
        {
            ProbeCollider input = new ProbeCollider();
            Check(input.Shape.Type == ProbeShapeType.None, "construction is the empty union");
            Check(ProbeShapeMaxBits == 2 + 16, "MaxBits is tag + the largest arm");

            input.Armor = 7;
            input.Shape.Type = ProbeShapeType.Slab;
            input.Shape.Slab.Width = 42;
            input.Shape.Slab.Height = 9;
            // input.Backup stays None — the empty arm costs the tag bits only
            input.ExtrasCount = 1;
            input.Extras[0].Type = ProbeShapeType.Ring;
            input.Extras[0].Ring.Radius = 777;

            WriteStream ws = NewWriteStream();
            Check(WriteProbeCollider(ws, input), "write ProbeCollider");
            byte[] wire = Data(ws);
            GoldenWire("probecollider", wire);

            ProbeCollider output = new ProbeCollider();
            output.Backup.Type = ProbeShapeType.Ring; // dirty — the read must restore None
            ReadStream rs = new ReadStream(wire);
            Check(ReadProbeCollider(rs, output), "read ProbeCollider");
            Check(output.Armor == 7 && output.Shape.Type == ProbeShapeType.Slab &&
                  output.Shape.Slab.Width == 42 && output.Shape.Slab.Height == 9,
                  "the selected arm round-trips");
            Check(output.Backup.Type == ProbeShapeType.None, "the None arm reads back empty");
            Check(output.ExtrasCount == 1 && output.Extras[0].Type == ProbeShapeType.Ring &&
                  output.Extras[0].Ring.Radius == 777, "the union array round-trips");

            // NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8,
            // range [0, 2]; forcing both bits makes 3 and the read must refuse
            byte[] corrupt = (byte[]) wire.Clone();
            corrupt[1] |= 0x03;
            ProbeCollider bad = new ProbeCollider();
            Check(!ReadProbeCollider(new ReadStream(corrupt), bad),
                  "an out-of-range union tag is refused (SPEC §4.8)");

            // NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits
            // at bit offset 10 with range [0, 100]; all seven bits decode 127
            corrupt = (byte[]) wire.Clone();
            corrupt[1] |= 0xFC;
            corrupt[2] |= 0x01;
            Check(!ReadProbeCollider(new ReadStream(corrupt), bad),
                  "a corrupt union arm payload is refused (SPEC §4.8)");

            // the write side validates the tag BEFORE it rides
            ProbeShape rogue = new ProbeShape();
            rogue.Type = (ProbeShapeType) 3;
            WriteStream ws2 = NewWriteStream();
            Check(!WriteProbeShape(ws2, rogue), "an out-of-set union tag writes nothing (SPEC §4.8)");
        }

        // ---- TestData and InputPacket against their C++ pins ----
        {
            TestData input = TestDataInstance();
            WriteStream ws = NewWriteStream();
            Check(WriteTestData(ws, input), "write TestData (pin)");
            GoldenWire("testdata", Data(ws));

            InputPacket packet = new InputPacket();
            packet.SynchronizeSequence = 7;
            packet.CurrentFrame = 123456789;
            packet.StartFrame = 123456780;
            packet.InputsCount = 2;
            packet.Inputs[0].Throttle = 0.5f;
            packet.Inputs[0].Fire = true;
            packet.Inputs[1].StickX = -0.25f;
            packet.Inputs[1].Boost = true;
            WriteStream ws2 = NewWriteStream();
            Check(WriteInputPacket(ws2, packet), "write InputPacket (pin)");
            GoldenWire("inputpacket", Data(ws2));
        }

        // ---- ProbeSample: the nested if/else wire, both ways, and §5 zeroing ----
        {
            ProbeSample input = new ProbeSample(); // active = true by default
            input.Orientation = 90.0f;
            input.RawDelta = -5;
            input.BigDelta = -1234567890123;
            input.Weapon = Weapon.Laser;
            input.HasTarget = true;
            input.TargetId = 777;
            input.IdleTicks = 12345; // untaken side on the wire — must read back ZERO
            input.SamplesCount = 1;
            input.Samples[0] = 42;

            WriteStream ws = NewWriteStream();
            Check(WriteProbeSample(ws, input), "write ProbeSample active");
            byte[] wire = Data(ws);
            ProbeSample output = new ProbeSample();
            ReadStream rs = new ReadStream(wire);
            Check(ReadProbeSample(rs, output), "read ProbeSample active");
            Check(output.Active && output.Weapon == Weapon.Laser && output.HasTarget && output.TargetId == 777,
                "the taken branch round-trips, nested branch included");
            Check(output.IdleTicks == 0, "the untaken else side reads as zero (SPEC §5)");
            Check(output.Orientation == 90.0f, "compressed float round-trips exactly at its resolution");

            input.Active = false;
            input.HasTarget = false;
            WriteStream ws2 = NewWriteStream();
            Check(WriteProbeSample(ws2, input), "write ProbeSample idle");
            byte[] wire2 = Data(ws2);
            ReadStream rs2 = new ReadStream(wire2);
            Check(ReadProbeSample(rs2, output), "read ProbeSample idle");
            Check(!output.Active && output.IdleTicks == 12345, "the else branch round-trips");
            Check(output.Weapon == Weapon.None && !output.HasTarget && output.TargetId == 0,
                "the whole untaken then side reads as zero, nested branch included (SPEC §5)");
        }

        // ---- ProbeArray: transitive defaults and its C++ pin ----
        {
            ProbeArray fresh = new ProbeArray();
            Check(fresh.Samples[0].Active && fresh.Samples[1].Active, "defaults reach through a fixed array");
            Check(fresh.Config.Retries == -1 && fresh.Config.Preferred == Weapon.Railgun,
                "defaults reach through a plain member");

            ProbeArray input = new ProbeArray();
            input.Samples[0].Orientation = 90.0f;
            input.Samples[0].RawDelta = -5;
            input.Samples[0].BigDelta = -1234567890123;
            input.Samples[0].Weapon = Weapon.Laser;
            input.Samples[0].HasTarget = true;
            input.Samples[0].TargetId = 777;
            input.Samples[0].SamplesCount = 1;
            input.Samples[0].Samples[0] = 42;
            input.Samples[1].Active = false;
            input.Samples[1].Orientation = -45.5f;
            input.Samples[1].RawDelta = 7;
            input.Samples[1].BigDelta = 99;
            input.Samples[1].IdleTicks = 1000;
            input.Samples[1].SamplesCount = 2;
            input.Samples[1].Samples[0] = 7;
            input.Samples[1].Samples[1] = 8;
            input.Config.Retries = 3;
            input.Config.Preferred = Weapon.Missile;

            WriteStream ws = NewWriteStream();
            Check(WriteProbeArray(ws, input), "write ProbeArray");
            byte[] wire = Data(ws);
            GoldenWire("probearray", wire);

            ProbeArray output = new ProbeArray();
            ReadStream rs = new ReadStream(wire);
            Check(ReadProbeArray(rs, output), "read ProbeArray");
            Check(!output.Samples[1].Active && output.Samples[1].IdleTicks == 1000, "nested else branch round-trips");
            Check(output.Samples[1].Weapon == Weapon.None && !output.Samples[1].HasTarget,
                "nested untaken side reads as zero (SPEC §5)");
            Check(output.Config.Retries == 3 && output.Config.Preferred == Weapon.Missile, "config round-trips");
        }

        // ---- ProbeReport: nested composition, and the widened flags wire ----
        {
            ProbeReport input = new ProbeReport();
            input.Header.Version = 3;
            input.Header.ProbeId = 0xCAFEBABE;
            input.Flags = ProbeFlagsArmed | ProbeFlagsDamaged;
            input.Echo.TestA = 555;
            input.Echo.TestB = 1000;

            WriteStream ws = NewWriteStream();
            Check(WriteProbeReport(ws, input), "write ProbeReport");
            byte[] wire = Data(ws);
            ProbeReport output = new ProbeReport();
            ReadStream rs = new ReadStream(wire);
            Check(ReadProbeReport(rs, output), "read ProbeReport");
            Check(EqProbeReport(output, input), "ProbeReport round-trips — a named type as an ordinary field");

            // a mask bit above the widened 8-bit wire is refused, not truncated —
            // this refusal is a GENERATED guard (the runtime's raw bit calls mask
            // silently), so bool is the whole verdict: nothing latches
            input.Flags = 1ul << 9;
            WriteStream ws2 = NewWriteStream();
            Check(!WriteProbeReport(ws2, input), "a mask bit above the flags wire width is refused");
            Check(ws2.Error == SerializeError.None, "the generated guard refuses via bool alone — nothing latches");
        }

        // ---- Block: the bytes(N) framing ----
        {
            Block input = new Block();
            for (int i = 0; i < 100; i++)
            {
                input.Data[i] = (byte)i;
            }
            input.DataLength = 100;

            WriteStream ws = NewWriteStream();
            Check(WriteBlock(ws, input), "write Block");
            byte[] wire = Data(ws);
            Block output = new Block();
            ReadStream rs = new ReadStream(wire);
            Check(ReadBlock(rs, output), "read Block");
            Check(EqBlock(output, input), "Block round-trips — bytes(N) framing");
        }

        // ---- the readers agree on what they REJECT (goal 3's second half) ----
        {
            // an interior null in a string is content the read refuses:
            // validation — false with NOTHING latched (the C# twin of Go's
            // ErrValidation vs stream-error distinction)
            byte[] chatGolden = ReadGolden("chat");
            if (chatGolden != null)
            {
                byte[] corrupt = (byte[])chatGolden.Clone();
                corrupt[4] = 0; // inside the text bytes (length rides bytes 0-1, align pads to byte 2)
                Chat output = new Chat();
                ReadStream rs = new ReadStream(corrupt);
                Check(!ReadChat(rs, output) && rs.Error == SerializeError.None,
                    "an interior null is rejected as validation — false, stream.Error stays None");

                // a truncated stream is the stream's own error, never a content verdict
                byte[] truncated = new byte[3];
                Array.Copy(chatGolden, truncated, 3);
                Chat output2 = new Chat();
                ReadStream rs2 = new ReadStream(truncated);
                Check(!ReadChat(rs2, output2) && rs2.Error != SerializeError.None,
                    "truncation surfaces as the latched stream error");
            }

            // a nonzero reserved bit is rejected — validation again: not latched
            byte[] probeGolden = ReadGolden("probe_header");
            if (probeGolden != null)
            {
                byte[] corrupt2 = (byte[])probeGolden.Clone();
                corrupt2[1] |= 0x08; // the first reserved bit above version's 3
                ProbeHeader output3 = new ProbeHeader();
                ReadStream rs3 = new ReadStream(corrupt2);
                Check(!ReadProbeHeader(rs3, output3) && rs3.Error == SerializeError.None,
                    "a nonzero reserved bit is rejected — false, stream.Error stays None");
            }

            // an out-of-range array count is refused before any element rides —
            // corrupt the count bits INSIDE a complete valid wire (the preamble is
            // 16+64+64 = 144 bits, so the 5-bit count sits at byte 18 bits 0-4),
            // so the refusal is the RANGE check, not a truncation overflow; the
            // runtime's ranged read is the refuser here, so it LATCHES
            byte[] packetGolden = ReadGolden("inputpacket");
            if (packetGolden != null)
            {
                byte[] corrupt3 = (byte[])packetGolden.Clone();
                corrupt3[18] = (byte)((corrupt3[18] & 0xE0) | 17); // count 2 -> 17, over [0, 16]
                InputPacket output4 = new InputPacket();
                ReadStream rs4 = new ReadStream(corrupt3);
                Check(!ReadInputPacket(rs4, output4) && rs4.Error == SerializeError.ValueOutOfRange,
                    "an out-of-range count is refused before the loop, latched by the runtime");
            }
        }

        // ---- RigidBody: the moving branch read back whole ----
        {
            RigidBody input = new RigidBody();
            input.Position.X = 1.5;
            input.Position.Y = -2.5;
            input.Position.Z = 3.25;
            input.Orientation.X = 0.1;
            input.Orientation.Y = 0.2;
            input.Orientation.Z = 0.3;
            input.Orientation.W = 0.9;
            input.AtRest = false;
            input.LinearVelocity.X = 10.0;
            input.LinearVelocity.Y = 20.0;
            input.LinearVelocity.Z = -3.0;
            input.AngularVelocity.X = 0.25;
            input.AngularVelocity.Y = 0.5;
            input.AngularVelocity.Z = 0.75;

            WriteStream ws = NewWriteStream();
            Check(WriteRigidBody(ws, input), "write RigidBody moving (read-back)");
            byte[] wire = Data(ws);
            RigidBody output = new RigidBody();
            ReadStream rs = new ReadStream(wire);
            Check(ReadRigidBody(rs, output), "read RigidBody moving");
            Check(EqRigidBody(output, input), "the moving branch round-trips with velocities intact");
        }

        // ---- FlagName / FlagNames: per-bit names and the set renderer ----
        {
            Check(FlagNameShipFlags(0) == "FiringLaser", "FlagName names bit 0");
            Check(FlagNameShipFlags(9) == "???", "FlagName is out-of-range safe");
            Check(FlagNamesShipFlags(0) == "0", "FlagNames renders the empty set as 0");
            Check(FlagNamesShipFlags(ShipFlagsFiringLaser | ShipFlagsBraking) == "FiringLaser|Braking", "FlagNames renders the set bits");
            Check(FlagNamesShipFlags(ShipFlagsAiming | (1ul << 63)) == "Aiming|0x8000000000000000", "FlagNames renders unknown high bits as hex");
        }

        // ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
        //
        // Twelve shapes written back to back into ONE stream against the one
        // C++-pinned golden, in the C++ test's order. A fixed scalar array
        // whose elements an emitter places TWICE is invisible to a
        // same-language round trip; only the byte compare against another
        // language's bytes names it.
        {
            Vec2 vec2 = new Vec2 { X = 1.5, Y = -2.25 };

            SpanF64 spanF64 = new SpanF64();
            spanF64.Values[0] = 3.5; spanF64.Values[1] = -4.75;

            SpanU64 spanU64 = new SpanU64();
            spanU64.Values[0] = 0xDEADBEEFCAFEBABE; spanU64.Values[1] = 1;

            SpanI64 spanI64 = new SpanI64();
            spanI64.Values[0] = -1234567890123; spanI64.Values[1] = 42;

            SpanOne spanOne = new SpanOne();
            spanOne.Values[0] = 0x0123456789ABCDEF;

            SpanChunk spanChunk = new SpanChunk();
            spanChunk.Values[0] = 0x1111; spanChunk.Values[1] = 0x2222;
            spanChunk.Values[2] = 0x3333; spanChunk.Values[3] = 0x4444;

            SpanTail spanTail = new SpanTail();
            spanTail.Values[0] = 6.125; spanTail.Values[1] = -7.0;
            spanTail.Tail = 0xFEEDFACE;

            SpanTwice spanTwice = new SpanTwice();
            spanTwice.A[0] = 8.5; spanTwice.A[1] = 9.5;
            spanTwice.B[0] = -10.5; spanTwice.B[1] = -11.5;

            Trio trio = new Trio { A = 0xABCDE, B = 0x12345, C = 0xFFFFF };

            TrioSole trioSole = new TrioSole();
            trioSole.Inner.A = 1; trioSole.Inner.B = 2; trioSole.Inner.C = 3;

            TrioFirst trioFirst = new TrioFirst();
            trioFirst.Inner.A = 0xAAAAA; trioFirst.Inner.B = 0x55555;
            trioFirst.Inner.C = 0xF0F0F; trioFirst.Trailer = 0xBEEF;

            TrioStraddle straddle = new TrioStraddle();
            straddle.Pad0 = 0x0011223344556677;
            straddle.Pad1 = 0x8899AABBCCDDEEFF;
            straddle.Pad2 = 0xFFFFFFFFFFFFFFFF;
            straddle.Pad3 = 0;
            straddle.Pad4 = 0x123456789ABCDEF0;
            straddle.Pad5 = 0xABCDEF;
            straddle.Inner.A = 0x11111; straddle.Inner.B = 0x22222; straddle.Inner.C = 0x33333;

            WriteStream ws = NewWriteStream();
            Check(WriteVec2(ws, vec2), "write Vec2");
            Check(WriteSpanF64(ws, spanF64), "write SpanF64");
            Check(WriteSpanU64(ws, spanU64), "write SpanU64");
            Check(WriteSpanI64(ws, spanI64), "write SpanI64");
            Check(WriteSpanOne(ws, spanOne), "write SpanOne");
            Check(WriteSpanChunk(ws, spanChunk), "write SpanChunk");
            Check(WriteSpanTail(ws, spanTail), "write SpanTail");
            Check(WriteSpanTwice(ws, spanTwice), "write SpanTwice");
            Check(WriteTrio(ws, trio), "write Trio");
            Check(WriteTrioSole(ws, trioSole), "write TrioSole");
            Check(WriteTrioFirst(ws, trioFirst), "write TrioFirst");
            Check(WriteTrioStraddle(ws, straddle), "write TrioStraddle");
            Check(ws.BitsProcessed == 128 + 128 + 128 + 128 + 64 + 64 + 160 + 256 + 64 + 64 + 80 + 408,
                "the twelve degenerate shapes ride their declared widths and nothing more");
            byte[] wire = Data(ws);
            GoldenWire("degenerate", wire);

            Vec2 rVec2 = new Vec2();
            SpanF64 rSpanF64 = new SpanF64();
            SpanU64 rSpanU64 = new SpanU64();
            SpanI64 rSpanI64 = new SpanI64();
            SpanOne rSpanOne = new SpanOne();
            SpanChunk rSpanChunk = new SpanChunk();
            SpanTail rSpanTail = new SpanTail();
            SpanTwice rSpanTwice = new SpanTwice();
            Trio rTrio = new Trio();
            TrioSole rTrioSole = new TrioSole();
            TrioFirst rTrioFirst = new TrioFirst();
            TrioStraddle rStraddle = new TrioStraddle();

            ReadStream rs = new ReadStream(wire);
            Check(ReadVec2(rs, rVec2), "read Vec2");
            Check(ReadSpanF64(rs, rSpanF64), "read SpanF64");
            Check(ReadSpanU64(rs, rSpanU64), "read SpanU64");
            Check(ReadSpanI64(rs, rSpanI64), "read SpanI64");
            Check(ReadSpanOne(rs, rSpanOne), "read SpanOne");
            Check(ReadSpanChunk(rs, rSpanChunk), "read SpanChunk");
            Check(ReadSpanTail(rs, rSpanTail), "read SpanTail");
            Check(ReadSpanTwice(rs, rSpanTwice), "read SpanTwice");
            Check(ReadTrio(rs, rTrio), "read Trio");
            Check(ReadTrioSole(rs, rTrioSole), "read TrioSole");
            Check(ReadTrioFirst(rs, rTrioFirst), "read TrioFirst");
            Check(ReadTrioStraddle(rs, rStraddle), "read TrioStraddle");

            Check(rVec2.X == 1.5 && rVec2.Y == -2.25, "Vec2 round-trips");
            Check(rSpanF64.Values[0] == 3.5 && rSpanF64.Values[1] == -4.75, "SpanF64 round-trips");
            Check(rSpanU64.Values[0] == 0xDEADBEEFCAFEBABE && rSpanU64.Values[1] == 1, "SpanU64 round-trips");
            Check(rSpanI64.Values[0] == -1234567890123 && rSpanI64.Values[1] == 42, "SpanI64 round-trips");
            Check(rSpanOne.Values[0] == 0x0123456789ABCDEF, "SpanOne round-trips");
            Check(rSpanChunk.Values[0] == 0x1111 && rSpanChunk.Values[3] == 0x4444, "SpanChunk round-trips");
            Check(rSpanTail.Values[0] == 6.125 && rSpanTail.Values[1] == -7.0 && rSpanTail.Tail == 0xFEEDFACE,
                "SpanTail round-trips");
            Check(rSpanTwice.A[0] == 8.5 && rSpanTwice.B[1] == -11.5, "SpanTwice round-trips");
            Check(rTrio.A == 0xABCDE && rTrio.B == 0x12345 && rTrio.C == 0xFFFFF, "Trio round-trips");
            Check(rTrioSole.Inner.A == 1 && rTrioSole.Inner.C == 3, "TrioSole round-trips");
            Check(rTrioFirst.Inner.A == 0xAAAAA && rTrioFirst.Trailer == 0xBEEF, "TrioFirst round-trips");
            Check(rStraddle.Pad0 == 0x0011223344556677 && rStraddle.Pad4 == 0x123456789ABCDEF0,
                "TrioStraddle pads round-trip");
            Check(rStraddle.Pad5 == 0xABCDEF && rStraddle.Inner.A == 0x11111 && rStraddle.Inner.C == 0x33333,
                "TrioStraddle's nested fields round-trip across the boundary");
        }

        // ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----
        //
        // Degenerate.schema is every-type-a-whole-number-of-bytes by
        // construction, so no clause boundary in it lands mid-byte. These two
        // units are chosen so they do. Each shape is written to its OWN
        // stream and flushed, and the golden is those concatenated — the
        // shapes are not byte-aligned, so a shared stream would not equal the
        // concatenation every emitter can produce.
        {
            var stream = new System.Collections.Generic.List<byte>();

            void Emit(string name, int bits, Func<WriteStream, bool> write)
            {
                WriteStream ws = NewWriteStream();
                Check(write(ws), "write " + name);
                Check(ws.BitsProcessed == bits, name + " rides its declared width");
                byte[] bytes = Data(ws);
                Check(bytes.Length == (bits + 7) / 8, name + " byte width");
                stream.AddRange(bytes);
            }

            int off = 0;
            void Consume(string name, int bits, Func<ReadStream, bool> read)
            {
                int n = (bits + 7) / 8;
                byte[] slice = new byte[n];
                stream.CopyTo(off, slice, 0, n);
                Check(read(new ReadStream(slice)), "read " + name);
                off += n;
            }

            // ---- Clauses.schema ----

            int[] w13Counts = { 0, 1, 3, 4, 5, 7, 12 };
            foreach (int c in w13Counts)
            {
                W13 v = new W13 { ItemsCount = c };
                for (int i = 0; i < c; i++) v.Items[i] = (ushort)(8191 - i * 733);
                Emit("W13/" + c, 4 + 13 * c, ws => WriteW13(ws, v));
            }

            int[] w17Counts = { 0, 1, 2, 3, 4, 9 };
            foreach (int c in w17Counts)
            {
                W17 v = new W17 { ItemsCount = c };
                for (int i = 0; i < c; i++) v.Items[i] = (uint)(131071 - i * 11117);
                Emit("W17/" + c, 4 + 17 * c, ws => WriteW17(ws, v));
            }

            int[] w26Counts = { 0, 1, 2, 3, 6 };
            foreach (int c in w26Counts)
            {
                W26 v = new W26 { ItemsCount = c };
                for (int i = 0; i < c; i++) v.Items[i] = (uint)(67108863 - i * 5555555);
                Emit("W26/" + c, 3 + 26 * c, ws => WriteW26(ws, v));
            }

            int[] w1Counts = { 0, 1, 3, 4, 5, 20 };
            foreach (int c in w1Counts)
            {
                W1 v = new W1 { ItemsCount = c };
                for (int i = 0; i < c; i++) v.Items[i] = (byte)(i % 2);
                Emit("W1/" + c, 5 + c, ws => WriteW1(ws, v));
            }

            for (int c = 0; c <= 3; c++)
            {
                W52 v = new W52 { ItemsCount = c };
                for (int i = 0; i < c; i++) v.Items[i] = 4503599627370495UL - (ulong)i * 123456789UL;
                Emit("W52/" + c, 2 + 52 * c, ws => WriteW52(ws, v));
            }

            for (int c = 0; c <= 3; c++)
            {
                W50 v = new W50 { ItemsCount = c };
                for (int i = 0; i < c; i++) v.Items[i] = 1125899906842623UL - (ulong)i * 987654321UL;
                Emit("W50/" + c, 2 + 50 * c, ws => WriteW50(ws, v));
            }

            F13 f13 = new F13();
            for (int i = 0; i < 7; i++) f13.Items[i] = (ushort)(8191 - i * 911);
            Emit("F13", 91, ws => WriteF13(ws, f13));

            int[] triCounts = { 0, 1, 3, 4, 5, 10 };
            foreach (int c in triCounts)
            {
                ArrTri3 v = new ArrTri3 { ItemsCount = c };
                for (int i = 0; i < c; i++) { v.Items[i].A = (uint)(i % 2); v.Items[i].B = (uint)(i % 4); }
                Emit("ArrTri3/" + c, 4 + 3 * c, ws => WriteArrTri3(ws, v));
            }

            ArrEleven arrEleven = new ArrEleven();
            for (int i = 0; i < 9; i++) { arrEleven.Items[i].A = (uint)(i % 8); arrEleven.Items[i].B = (uint)(255 - i * 17); }
            Emit("ArrEleven", 99, ws => WriteArrEleven(ws, arrEleven));

            // lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag
            EmptyUnionType[] emptyArms = { EmptyUnionType.None, EmptyUnionType.A, EmptyUnionType.B };
            foreach (EmptyUnionType arm in emptyArms)
            {
                HoldsEmptyUnion v = new HoldsEmptyUnion { Lead = 21, Tail = 99 };
                v.U.Type = arm;
                Emit("HoldsEmptyUnion/" + arm, 14, ws => WriteHoldsEmptyUnion(ws, v));
            }

            // lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s bytes,
            // b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail. The
            // 5-bit lead is what puts the align at a non-zero offset.
            int[] strsBits = { 27, 155, 75 };
            byte[][] strsS = { new byte[0], Encoding.ASCII.GetBytes("abcdefgh"), Encoding.ASCII.GetBytes("xyz") };
            byte[][] strsB = { new byte[0], new byte[] { 0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7 }, new byte[] { 1, 2, 3 } };
            for (int k = 0; k < 3; k++)
            {
                Strs v = new Strs { Lead = 21, Tail = 5, SLength = strsS[k].Length, BLength = strsB[k].Length };
                Array.Copy(strsS[k], v.S, strsS[k].Length);
                Array.Copy(strsB[k], v.B, strsB[k].Length);
                Emit("Strs/" + k, strsBits[k], ws => WriteStrs(ws, v));
            }

            for (int c = 0; c <= 4; c++)
            {
                ArrNested v = new ArrNested { Lead = 21, Tail = 5, ItemsCount = c };
                for (int i = 0; i < c; i++) { v.Items[i].A = (uint)(i % 8); v.Items[i].B = (uint)(200 - i * 7); }
                Emit("ArrNested/" + c, 11 + 11 * c, ws => WriteArrNested(ws, v));
            }

            Sole sole = new Sole { Only = 5555 };
            Emit("Sole", 13, ws => WriteSole(ws, sole));

            GoldenWire("clauses", stream.ToArray());

            // Read each shape back out of its own slice. A clause that decodes
            // a different number of elements than the writer encoded shows up
            // here even where the byte compare above happens to pass.
            foreach (int c in w13Counts)
            {
                W13 r = new W13();
                Consume("W13/" + c, 4 + 13 * c, rs => ReadW13(rs, r));
                Check(r.ItemsCount == c, "W13 count round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i] == (ushort)(8191 - i * 733), "W13 element round-trips");
            }
            foreach (int c in w17Counts)
            {
                W17 r = new W17();
                Consume("W17/" + c, 4 + 17 * c, rs => ReadW17(rs, r));
                Check(r.ItemsCount == c, "W17 count round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i] == (uint)(131071 - i * 11117), "W17 element round-trips");
            }
            foreach (int c in w26Counts)
            {
                W26 r = new W26();
                Consume("W26/" + c, 3 + 26 * c, rs => ReadW26(rs, r));
                Check(r.ItemsCount == c, "W26 count round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i] == (uint)(67108863 - i * 5555555), "W26 element round-trips");
            }
            foreach (int c in w1Counts)
            {
                W1 r = new W1();
                Consume("W1/" + c, 5 + c, rs => ReadW1(rs, r));
                Check(r.ItemsCount == c, "W1 count round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i] == (byte)(i % 2), "W1 element round-trips");
            }
            for (int c = 0; c <= 3; c++)
            {
                W52 r = new W52();
                Consume("W52/" + c, 2 + 52 * c, rs => ReadW52(rs, r));
                Check(r.ItemsCount == c, "W52 count round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i] == 4503599627370495UL - (ulong)i * 123456789UL, "W52 element round-trips");
            }
            for (int c = 0; c <= 3; c++)
            {
                W50 r = new W50();
                Consume("W50/" + c, 2 + 50 * c, rs => ReadW50(rs, r));
                Check(r.ItemsCount == c, "W50 count round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i] == 1125899906842623UL - (ulong)i * 987654321UL, "W50 element round-trips");
            }
            {
                F13 r = new F13();
                Consume("F13", 91, rs => ReadF13(rs, r));
                for (int i = 0; i < 7; i++) Check(r.Items[i] == (ushort)(8191 - i * 911), "F13 element round-trips");
            }
            foreach (int c in triCounts)
            {
                ArrTri3 r = new ArrTri3();
                Consume("ArrTri3/" + c, 4 + 3 * c, rs => ReadArrTri3(rs, r));
                Check(r.ItemsCount == c, "ArrTri3 count round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i].A == (uint)(i % 2) && r.Items[i].B == (uint)(i % 4), "ArrTri3 element round-trips");
            }
            {
                ArrEleven r = new ArrEleven();
                Consume("ArrEleven", 99, rs => ReadArrEleven(rs, r));
                for (int i = 0; i < 9; i++) Check(r.Items[i].A == (uint)(i % 8) && r.Items[i].B == (uint)(255 - i * 17), "ArrEleven element round-trips");
            }
            foreach (EmptyUnionType arm in emptyArms)
            {
                HoldsEmptyUnion r = new HoldsEmptyUnion();
                Consume("HoldsEmptyUnion/" + arm, 14, rs => ReadHoldsEmptyUnion(rs, r));
                Check(r.Lead == 21 && r.Tail == 99 && r.U.Type == arm, "HoldsEmptyUnion round-trips");
            }
            for (int k = 0; k < 3; k++)
            {
                Strs r = new Strs();
                Consume("Strs/" + k, strsBits[k], rs => ReadStrs(rs, r));
                Check(r.Lead == 21 && r.Tail == 5, "Strs lead and tail round-trip");
                Check(r.SLength == strsS[k].Length && r.BLength == strsB[k].Length, "Strs lengths round-trip");
                for (int i = 0; i < strsS[k].Length; i++) Check(r.S[i] == strsS[k][i], "Strs string byte round-trips");
                for (int i = 0; i < strsB[k].Length; i++) Check(r.B[i] == strsB[k][i], "Strs bytes byte round-trips");
            }
            for (int c = 0; c <= 4; c++)
            {
                ArrNested r = new ArrNested();
                Consume("ArrNested/" + c, 11 + 11 * c, rs => ReadArrNested(rs, r));
                Check(r.ItemsCount == c && r.Lead == 21 && r.Tail == 5, "ArrNested round-trips");
                for (int i = 0; i < c; i++) Check(r.Items[i].A == (uint)(i % 8) && r.Items[i].B == (uint)(200 - i * 7), "ArrNested element round-trips");
            }
            {
                Sole r = new Sole();
                Consume("Sole", 13, rs => ReadSole(rs, r));
                Check(r.Only == 5555, "Sole round-trips");
            }
            Check(off == stream.Count, "the clauses reads consume the whole golden");

            // ---- Joins.schema ----
            //
            // Every branch is written on BOTH arms, so no path is pinned by
            // omission. The expected value after a round trip is not the value
            // written: the untaken side reads back as zero (SPEC §5).

            stream.Clear();
            off = 0;

            for (int fi = 0; fi <= 1; fi++)
            {
                bool f = fi != 0;
                // the arms agree on WIDTH but not on value, so a join that
                // keeps the wrong arm is a value mismatch, not just a width one
                ArmsAgree agree = new ArmsAgree { Lead = 21, Flag = f, A = 1234, B = 1500, Tail = 99 };
                Emit("ArmsAgree/" + f, 24, ws => WriteArmsAgree(ws, agree));

                ArmsDisagree disagree = new ArmsDisagree { Lead = 21, Flag = f, A = 1234, B = 5, Tail = 99 };
                Emit("ArmsDisagree/" + f, f ? 24 : 16, ws => WriteArmsDisagree(ws, disagree));

                ArmEmpty armEmpty = new ArmEmpty { Lead = 21, Flag = f, A = 456789, Tail = 99 };
                Emit("ArmEmpty/" + f, f ? 32 : 13, ws => WriteArmEmpty(ws, armEmpty));

                ArmAlign alignStr = new ArmAlign { Lead = 21, Flag = f, SLength = 4, B = 1000, Tail = 99 };
                Array.Copy(Encoding.ASCII.GetBytes("abcd"), alignStr.S, 4);
                Emit("ArmAlign/" + f, f ? 55 : 23, ws => WriteArmAlign(ws, alignStr));

                ArmAlign alignEmpty = new ArmAlign { Lead = 21, Flag = f, B = 1000, Tail = 99 };
                Emit("ArmAlignEmptyStr/" + f, 23, ws => WriteArmAlign(ws, alignEmpty));
            }

            for (int oi = 0; oi <= 1; oi++)
            {
                for (int ii = 0; ii <= 1; ii++)
                {
                    bool o = oi != 0, inn = ii != 0;
                    ArmsNested v = new ArmsNested { Lead = 5, Outer = o, Inner = inn, X = 500000000, Y = 17, Z = 4000, Tail = 33 };
                    Emit("ArmsNested/" + oi + ii, o ? (inn ? 40 : 16) : 23, ws => WriteArmsNested(ws, v));
                }
            }

            for (int fi = 0; fi <= 1; fi++)
            {
                for (int c = 0; c <= 3; c++)
                {
                    bool f = fi != 0;
                    ArmArray v = new ArmArray { Lead = 21, Flag = f, ItemsCount = c, B = 300, Tail = 99 };
                    for (int i = 0; i < c; i++) v.Items[i] = (ushort)(8191 - i * 777);
                    Emit("ArmArray/" + f + "/" + c, f ? 15 + 13 * c : 22, ws => WriteArmArray(ws, v));
                }
            }

            // lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits
            UnevenType[] unevenArms = { UnevenType.None, UnevenType.Narrow, UnevenType.Wide };
            int[] unevenBits = { 18, 21, 55 };
            for (int k = 0; k < 3; k++)
            {
                HoldsUneven v = new HoldsUneven { Lead = 21, Tail = 1500 };
                v.U.Type = unevenArms[k];
                if (unevenArms[k] == UnevenType.Narrow) v.U.Narrow.N = 5;
                if (unevenArms[k] == UnevenType.Wide) v.U.Wide.W = 123456789012UL;
                Emit("HoldsUneven/" + unevenArms[k], unevenBits[k], ws => WriteHoldsUneven(ws, v));
            }

            // alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37)
            int[] unevenItemBits = { 0, 5, 44, 49 };
            for (int c = 0; c <= 3; c++)
            {
                ArrUneven v = new ArrUneven { Lead = 21, Tail = 5, ItemsCount = c };
                for (int i = 0; i < c; i++)
                {
                    if (i % 2 == 0) { v.Items[i].Type = UnevenType.Narrow; v.Items[i].Narrow.N = (uint)(i % 8); }
                    else { v.Items[i].Type = UnevenType.Wide; v.Items[i].Wide.W = 99887766554UL + (ulong)i; }
                }
                Emit("ArrUneven/" + c, 10 + unevenItemBits[c], ws => WriteArrUneven(ws, v));
            }

            // lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s,
            // then a 32 + 29 + 19 + 4 static run after the align regains it
            for (int c = 0; c <= 3; c++)
            {
                for (int sl = 0; sl <= 4; sl += 4)
                {
                    RegainAfterAlign v = new RegainAfterAlign
                    {
                        Lead = 21, ItemsCount = c, SLength = sl,
                        P = 0xDEADBEEF, Q = (1u << 29) - 7, R = (1u << 19) - 3, Tail = 9,
                    };
                    if (sl != 0) Array.Copy(Encoding.ASCII.GetBytes("wxyz"), v.S, 4);
                    for (int i = 0; i < c; i++) v.Items[i] = (ushort)(8191 - i * 999);
                    int afterAlign = ((5 + 2 + 13 * c + 3) + 7) / 8 * 8;
                    Emit("Regain/" + c + "/" + sl, afterAlign + 8 * sl + 84, ws => WriteRegainAfterAlign(ws, v));
                }
            }

            GoldenWire("joins", stream.ToArray());

            for (int fi = 0; fi <= 1; fi++)
            {
                bool f = fi != 0;
                ArmsAgree agree = new ArmsAgree();
                Consume("ArmsAgree/" + f, 24, rs => ReadArmsAgree(rs, agree));
                Check(agree.Lead == 21 && agree.Flag == f && agree.Tail == 99, "ArmsAgree round-trips");
                Check(f ? (agree.A == 1234 && agree.B == 0) : (agree.B == 1500 && agree.A == 0),
                    "ArmsAgree's untaken side reads as zero (SPEC §5)");

                ArmsDisagree disagree = new ArmsDisagree();
                Consume("ArmsDisagree/" + f, f ? 24 : 16, rs => ReadArmsDisagree(rs, disagree));
                Check(disagree.Lead == 21 && disagree.Tail == 99, "ArmsDisagree round-trips");
                Check(f ? (disagree.A == 1234 && disagree.B == 0) : (disagree.B == 5 && disagree.A == 0),
                    "ArmsDisagree's untaken side reads as zero");

                ArmEmpty armEmpty = new ArmEmpty();
                Consume("ArmEmpty/" + f, f ? 32 : 13, rs => ReadArmEmpty(rs, armEmpty));
                Check(armEmpty.Lead == 21 && armEmpty.Tail == 99, "ArmEmpty round-trips");
                Check(armEmpty.A == (f ? 456789u : 0u), "ArmEmpty's absent arm reads as zero");

                ArmAlign alignStr = new ArmAlign();
                Consume("ArmAlign/" + f, f ? 55 : 23, rs => ReadArmAlign(rs, alignStr));
                Check(alignStr.Lead == 21 && alignStr.Tail == 99, "ArmAlign round-trips");
                Check(f ? (alignStr.SLength == 4 && alignStr.S[0] == (byte)'a' && alignStr.S[3] == (byte)'d' && alignStr.B == 0)
                        : (alignStr.B == 1000 && alignStr.SLength == 0),
                    "ArmAlign's untaken side reads as zero");

                ArmAlign alignEmpty = new ArmAlign();
                Consume("ArmAlignEmptyStr/" + f, 23, rs => ReadArmAlign(rs, alignEmpty));
                Check(alignEmpty.Lead == 21 && alignEmpty.Tail == 99, "ArmAlign with an empty string round-trips");
                Check(f ? (alignEmpty.SLength == 0 && alignEmpty.B == 0) : (alignEmpty.B == 1000),
                    "ArmAlign's empty string round-trips");
            }

            for (int oi = 0; oi <= 1; oi++)
            {
                for (int ii = 0; ii <= 1; ii++)
                {
                    bool o = oi != 0, inn = ii != 0;
                    ArmsNested r = new ArmsNested();
                    Consume("ArmsNested/" + oi + ii, o ? (inn ? 40 : 16) : 23, rs => ReadArmsNested(rs, r));
                    Check(r.Lead == 5 && r.Tail == 33 && r.Outer == o, "ArmsNested round-trips");
                    if (o)
                    {
                        Check(r.Inner == inn && r.Z == 0, "ArmsNested's outer arm round-trips");
                        Check(inn ? (r.X == 500000000 && r.Y == 0) : (r.Y == 17 && r.X == 0), "ArmsNested's inner arm round-trips");
                    }
                    else
                    {
                        Check(r.Z == 4000 && r.X == 0 && r.Y == 0, "ArmsNested's else arm round-trips");
                    }
                }
            }

            for (int fi = 0; fi <= 1; fi++)
            {
                for (int c = 0; c <= 3; c++)
                {
                    bool f = fi != 0;
                    ArmArray r = new ArmArray();
                    Consume("ArmArray/" + f + "/" + c, f ? 15 + 13 * c : 22, rs => ReadArmArray(rs, r));
                    Check(r.Lead == 21 && r.Tail == 99, "ArmArray round-trips");
                    if (f)
                    {
                        Check(r.ItemsCount == c && r.B == 0, "ArmArray's array arm round-trips");
                        for (int i = 0; i < c; i++) Check(r.Items[i] == (ushort)(8191 - i * 777), "ArmArray element round-trips");
                    }
                    else
                    {
                        Check(r.B == 300 && r.ItemsCount == 0, "ArmArray's scalar arm round-trips");
                    }
                }
            }

            for (int k = 0; k < 3; k++)
            {
                HoldsUneven r = new HoldsUneven();
                Consume("HoldsUneven/" + unevenArms[k], unevenBits[k], rs => ReadHoldsUneven(rs, r));
                Check(r.Lead == 21 && r.Tail == 1500 && r.U.Type == unevenArms[k], "HoldsUneven round-trips");
                if (unevenArms[k] == UnevenType.Narrow) Check(r.U.Narrow.N == 5, "HoldsUneven's narrow arm round-trips");
                if (unevenArms[k] == UnevenType.Wide) Check(r.U.Wide.W == 123456789012UL, "HoldsUneven's wide arm round-trips");
            }

            for (int c = 0; c <= 3; c++)
            {
                ArrUneven r = new ArrUneven();
                Consume("ArrUneven/" + c, 10 + unevenItemBits[c], rs => ReadArrUneven(rs, r));
                Check(r.ItemsCount == c && r.Lead == 21 && r.Tail == 5, "ArrUneven round-trips");
                for (int i = 0; i < c; i++)
                {
                    if (i % 2 == 0) Check(r.Items[i].Type == UnevenType.Narrow && r.Items[i].Narrow.N == (uint)(i % 8), "ArrUneven narrow element round-trips");
                    else Check(r.Items[i].Type == UnevenType.Wide && r.Items[i].Wide.W == 99887766554UL + (ulong)i, "ArrUneven wide element round-trips");
                }
            }

            for (int c = 0; c <= 3; c++)
            {
                for (int sl = 0; sl <= 4; sl += 4)
                {
                    RegainAfterAlign r = new RegainAfterAlign();
                    int afterAlign = ((5 + 2 + 13 * c + 3) + 7) / 8 * 8;
                    Consume("Regain/" + c + "/" + sl, afterAlign + 8 * sl + 84, rs => ReadRegainAfterAlign(rs, r));
                    Check(r.Lead == 21 && r.ItemsCount == c && r.SLength == sl, "RegainAfterAlign round-trips");
                    Check(r.P == 0xDEADBEEF && r.Q == (1u << 29) - 7 && r.R == (1u << 19) - 3 && r.Tail == 9,
                        "RegainAfterAlign's static run after the align round-trips");
                    for (int i = 0; i < c; i++) Check(r.Items[i] == (ushort)(8191 - i * 999), "RegainAfterAlign element round-trips");
                }
            }
            Check(off == stream.Count, "the joins reads consume the whole golden");
        }

        if (failed)
        {
            return 1;
        }
        Console.WriteLine("OK");
        return 0;
    }
}
