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
// Message subclass — refused with nothing written, tested below. Generated
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
    static string tableDir;

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

    // GoldenTable byte-compares table-wire output against the C++-pinned golden.
    static void GoldenTable(string name, byte[] data)
    {
        byte[] golden;
        try
        {
            golden = File.ReadAllBytes(Path.Combine(tableDir, name + ".bin"));
        }
        catch (Exception e)
        {
            Console.WriteLine("FAILED: read table golden " + name + ": " + e.Message);
            failed = true;
            return;
        }
        Check(data.AsSpan().SequenceEqual(golden), "table golden " + name + " — C# bytes must equal the C++-pinned bytes");
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

    static bool EqShipShallow(ShipData_Shallow a, ShipData_Shallow b)
    {
        return a.ShipType == b.ShipType
            && a.PositionX == b.PositionX && a.PositionY == b.PositionY && a.PositionZ == b.PositionZ
            && a.RotationX == b.RotationX && a.RotationY == b.RotationY && a.RotationZ == b.RotationZ && a.RotationW == b.RotationW
            && a.LinearVelocityX == b.LinearVelocityX && a.LinearVelocityY == b.LinearVelocityY && a.LinearVelocityZ == b.LinearVelocityZ
            && a.Flags == b.Flags && a.Team == b.Team && a.Health == b.Health && a.Thrust == b.Thrust;
    }

    static bool EqMissileShallow(MissileData_Shallow a, MissileData_Shallow b)
    {
        return a.MissileType == b.MissileType
            && a.PositionX == b.PositionX && a.PositionY == b.PositionY && a.PositionZ == b.PositionZ
            && a.RotationX == b.RotationX && a.RotationY == b.RotationY && a.RotationZ == b.RotationZ && a.RotationW == b.RotationW
            && a.LinearVelocityX == b.LinearVelocityX && a.LinearVelocityY == b.LinearVelocityY && a.LinearVelocityZ == b.LinearVelocityZ
            && a.Team == b.Team && a.Flags == b.Flags;
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
        tableDir = FindTestDataDir("table");

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

        // ---- the Message dispatch surface: abstract base + type-pattern switch ----
        {
            Chat chat = new Chat();
            SetText(chat.Text, "dispatch");
            chat.TextLength = 8;
            Test test = new Test();
            test.TestB = 42;

            WriteStream ws = NewWriteStream();
            Check(WriteMessage(ws, chat), "write Message chat");
            Check(WriteMessage(ws, test), "write Message test");
            Check(WriteMessage(ws, null), "write Message terminator");
            byte[] wire = Data(ws);
            GoldenWire("message_stream", wire);

            // reads land in pre-allocated storage — no heap per message (SPEC §6.1);
            // the returned Message points into it, the union's own discipline
            MessageStorage storage = new MessageStorage();
            ReadStream rs = new ReadStream(wire);
            Check(ReadMessage(rs, storage, out Message m1), "read message 1");
            Check(m1 is Chat c1 && c1.TextLength == 8
                && Encoding.ASCII.GetString(c1.Text, 0, 8) == "dispatch", "message 1 is the chat");
            Check(ReferenceEquals(m1, storage.Chat), "the read message points into the caller's storage");
            Check(ReadMessage(rs, storage, out Message m2), "read message 2");
            Check(m2 is Test t2 && t2.TestB == 42, "message 2 is the test");
            Check(ReadMessage(rs, storage, out Message m3), "read message 3");
            Check(m3 == null, "message 3 is the None terminator");

            // the tag pair stands alone too
            WriteStream ws2 = NewWriteStream();
            Check(WriteMessageType(ws2, MessageType.Chat), "write message type");
            Check(WriteMessageType(ws2, MessageType.None), "write message type terminator");
            byte[] wire2 = Data(ws2);
            ReadStream rs2 = new ReadStream(wire2);
            MessageType tag = MessageType.None;
            Check(ReadMessageType(rs2, ref tag), "read message type");
            Check(tag == MessageType.Chat, "tag round-trips");
            Check(ReadMessageType(rs2, ref tag), "read message type terminator");
            Check(tag == MessageType.None, "terminator tag round-trips");

            // a Message subclass from outside the generated set writes NOTHING —
            // the stream cannot be left with a tag and no payload (a desync), and
            // the refusal is loud. (The Go typed-nil case has no C# twin: null
            // carries no type here — null IS the None terminator.)
            WriteStream ws3 = NewWriteStream();
            Check(!WriteMessage(ws3, new ForeignMessage()), "a foreign Message subclass is refused");
            ws3.Flush();
            Check(ws3.BytesProcessed == 0, "and nothing was written");
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

        // ---- the object views: Quantize/Unquantize and the shallow wire ----
        {
            ShipData_Interpolate interp = new ShipData_Interpolate();
            interp.ShipType = ShipType.Corvette;
            interp.Position.X = 1.5;
            interp.Position.Y = -2.25;
            interp.Position.Z = 100.0;
            interp.Rotation.X = 0.0;
            interp.Rotation.Y = 0.0;
            interp.Rotation.Z = 0.0;
            interp.Rotation.W = 1.0;
            interp.LinearVelocity.X = 3.0;
            interp.LinearVelocity.Y = 0.0;
            interp.LinearVelocity.Z = -1.0;
            interp.Flags = ShipFlagsBoosting;
            interp.Team = Team.Red;
            interp.Health = 750; // wire-int domain (rule 5)
            interp.Thrust = 55;

            ShipData_Shallow q = new ShipData_Shallow();
            QuantizeShip(interp, q);
            Check(q.PositionX == 1536, "1.5 * 1024 quantizes to 1536");
            Check(q.PositionY == -2304, "-2.25 * 1024 quantizes to -2304");
            Check(q.RotationW == 1024, "1.0 * 1024 quantizes to 1024");
            Check(q.Health == 750 && q.Thrust == 55, "projected fields copy");
            Check(q.Team == Team.Red && q.Flags == ShipFlagsBoosting, "discrete fields copy");

            WriteStream ws = NewWriteStream();
            Check(WriteShipData_Shallow(ws, q), "write ShipData_Shallow");
            byte[] wire = Data(ws);
            GoldenWire("ship_shallow", wire);

            ShipData_Shallow q2 = new ShipData_Shallow();
            ReadStream rs = new ReadStream(wire);
            Check(ReadShipData_Shallow(rs, q2), "read ShipData_Shallow");
            Check(EqShipShallow(q2, q), "the shallow wire round-trips");

            ShipData_Interpolate back = new ShipData_Interpolate();
            UnquantizeShip(q2, back);
            Check(back.Position.X == 1536.0 / 1024.0, "unquantize recovers x");
            Check(back.Position.Y == -2304.0 / 1024.0, "unquantize recovers y");
            Check(back.Rotation.W == 1.0, "unquantize recovers w");
            Check(back.Health == 750 && back.Team == Team.Red, "discrete and projected copy back");
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

        // ---- ProbeReport: message-as-field, and the widened flags wire ----
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
            Check(EqProbeReport(output, input), "ProbeReport round-trips — a message as an ordinary field");

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

        // ---- Missile: a second object family end to end ----
        {
            MissileData_Interpolate interp = new MissileData_Interpolate();
            interp.MissileType = MissileType.Torpedo;
            interp.Position.X = -4.0;
            interp.Position.Y = 8.0;
            interp.Position.Z = 15.5;
            interp.Rotation.X = 0.0;
            interp.Rotation.Y = 0.0;
            interp.Rotation.Z = 0.0;
            interp.Rotation.W = 1.0;
            interp.LinearVelocity.X = 1.0;
            interp.LinearVelocity.Y = 2.0;
            interp.LinearVelocity.Z = 3.0;
            interp.Team = Team.Blue;
            interp.Flags = 0xF00F;

            MissileData_Shallow q = new MissileData_Shallow();
            QuantizeMissile(interp, q);
            Check(q.PositionZ == 15872, "15.5 * 1024 quantizes to 15872");
            Check(q.RotationW == 1024 && q.Team == Team.Blue && q.Flags == 0xF00F, "discrete fields copy");

            WriteStream ws = NewWriteStream();
            Check(WriteMissileData_Shallow(ws, q), "write MissileData_Shallow");
            byte[] wire = Data(ws);
            MissileData_Shallow q2 = new MissileData_Shallow();
            ReadStream rs = new ReadStream(wire);
            Check(ReadMissileData_Shallow(rs, q2), "read MissileData_Shallow");
            Check(EqMissileShallow(q2, q), "the missile shallow wire round-trips");

            MissileData_Interpolate back = new MissileData_Interpolate();
            UnquantizeMissile(q2, back);
            Check(back.Position.Z == 15872.0 / 1024.0, "unquantize recovers z");
        }

        // ---- the object tag pair ----
        {
            WriteStream ws = NewWriteStream();
            Check(WriteObjectType(ws, ObjectType.Turret), "write object type");
            Check(WriteObjectType(ws, ObjectType.None), "write object type sentinel");
            byte[] wire = Data(ws);
            ReadStream rs = new ReadStream(wire);
            ObjectType tag = ObjectType.None;
            Check(ReadObjectType(rs, ref tag), "read object type");
            Check(tag == ObjectType.Turret, "object tag round-trips");
            Check(ReadObjectType(rs, ref tag), "read object type sentinel");
            Check(tag == ObjectType.None, "the None sentinel round-trips");
        }

        // ================= THE TABLE WIRE (notes/table-wire.md) =================
        // The same instances the C++ test pins in testdata/table/*.bin — the C#
        // table writer must produce byte-identical output, and the C# reader must
        // honor the same permissive contract.

        // ---- RigidBody: pins, round-trip, branch-guard elision ----
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

            byte[] wire = TableWriteRigidBody(input);
            GoldenTable("rigidbody_moving", wire);

            RigidBody output = new RigidBody();
            TableReport report = new TableReport();
            Check(TableReadRigidBody(wire, ref output, report), "table read RigidBody");
            Check(report.Unknown == 0 && report.KindMismatch == 0 && report.Clamped == 0 && !report.Malformed,
                "same-schema table decode is silent");
            Check(EqRigidBody(output, input), "RigidBody table round-trips");

            input.AtRest = true;
            byte[] atRest = TableWriteRigidBody(input);
            GoldenTable("rigidbody_at_rest", atRest);

            RigidBody output2 = new RigidBody();
            output2.LinearVelocity.X = 99; // dirty — prefill must reset
            output2.LinearVelocity.Y = 99;
            output2.LinearVelocity.Z = 99;
            TableReport report2 = new TableReport();
            Check(TableReadRigidBody(atRest, ref output2, report2), "table read RigidBody at rest");
            Check(output2.AtRest, "at_rest reads true");
            Check(EqVec3(output2.LinearVelocity, new Vec3()) && EqVec3(output2.AngularVelocity, new Vec3()),
                "the guard kept both velocities off the wire; prefill supplies the defaults");
        }

        // ---- the all-default instance is a bare terminator: 2 bytes ----
        {
            RigidBody body = new RigidBody();
            Check(TableWriteRigidBody(body).Length == 2, "all-default RigidBody is 2 bytes");

            ProbeConfig config = new ProbeConfig();
            Check(TableWriteProbeConfig(config).Length == 2, "at-defaults ProbeConfig is 2 bytes");

            ProbeConfig output = new ProbeConfig();
            output.Retries = 99;
            TableReport report = new TableReport();
            Check(TableReadProbeConfig(TableWriteProbeConfig(config), ref output, report),
                "table read at-defaults ProbeConfig");
            Check(output.Retries == -1 && output.Preferred == Weapon.Railgun,
                "prefill restores specified defaults on an empty table");
        }

        // ---- ProbeArray: fixed table array, nested defaults, its pin ----
        {
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

            byte[] wire = TableWriteProbeArray(input);
            GoldenTable("probearray", wire);

            ProbeArray output = new ProbeArray();
            TableReport report = new TableReport();
            Check(TableReadProbeArray(wire, ref output, report), "table read ProbeArray");
            Check(report.Unknown == 0 && report.KindMismatch == 0 && !report.Malformed, "probearray decode is clean");
            Check(output.Samples[0].Weapon == Weapon.Laser && output.Samples[0].TargetId == 777,
                "taken-branch fields round-trip");
            Check(!output.Samples[1].Active && output.Samples[1].IdleTicks == 1000, "else-branch fields round-trip");
            Check(output.Samples[1].Weapon == Weapon.None && !output.Samples[1].HasTarget,
                "untaken-branch fields stayed off the wire and read as defaults");
            Check(output.Config.Retries == 3 && output.Config.Preferred == Weapon.Missile,
                "nested table round-trips");
        }

        // ---- TestData: strings, counted arrays, bits, fixed bytes, its pin ----
        {
            TestData input = TestDataInstance();
            byte[] wire = TableWriteTestData(input);
            GoldenTable("testdata", wire);

            TestData output = new TestData();
            TableReport report = new TableReport();
            Check(TableReadTestData(wire, ref output, report), "table read TestData");
            Check(report.Unknown == 0 && report.KindMismatch == 0 && report.Clamped == 0 && !report.Malformed,
                "testdata table decode is silent");
            Check(EqTestData(output, input), "TestData table round-trips");
        }

        // ---- reflection descriptors: walk, read and WRITE a table generically ----
        {
            TableTypeInfo info = TableTypeRigidBody();
            Check(info.Name == "RigidBody", "descriptor names its type");
            Check(info.Fields.Length == 5, "RigidBody descriptor carries 5 fields");

            // find fields by name — the flat-array walk, ref readonly: no copies
            int atRest = -1, position = -1, linearVelocity = -1;
            for (int i = 0; i < info.Fields.Length; i++)
            {
                ref readonly TableFieldInfo f = ref info.Fields[i];
                switch (f.Name)
                {
                    case "at_rest":
                        atRest = i;
                        break;
                    case "position":
                        position = i;
                        break;
                    case "linear_velocity":
                        linearVelocity = i;
                        break;
                }
            }
            Check(atRest >= 0 && position >= 0 && linearVelocity >= 0, "fields found by name");
            Check(info.Fields[atRest].Kind == 1 && info.Fields[atRest].Guard == "", "at_rest is an unguarded bool");
            Check(ReferenceEquals(info.Fields[position].Table, TableTypeVec3()),
                "nested descriptor link IS the Vec3 descriptor");
            Check(info.Fields[position].TypeName == "Vec3", "nested field carries its schema type name");
            Check(info.Fields[linearVelocity].Guard == "!at_rest", "the branch guard is machine-usable");

            // generic WRITE by name (the accessor stand-in for the C++ offset
            // write), then prove the storage sees it — directly AND back
            // through TableGet
            RigidBody body = new RigidBody();
            Check(TableSetRigidBody(body, "at_rest", TableValue.FromBool(true)), "TableSet writes a bool by name");
            Check(body.AtRest, "the storage sees the generic write");
            TableValue got;
            Check(TableGetRigidBody(body, "at_rest", out got), "TableGet reads the bool back");
            Check(got.Kind == TableValueKind.Bool && got.B, "the bool reads back true, unboxed");
            Check(!TableSetRigidBody(body, "position", TableValue.FromFloat(1.0)),
                "nested tables refuse the scalar write path");
            Check(!TableSetRigidBody(body, "no_such_field", TableValue.FromFloat(1.0)), "unknown fields refuse");

            // generic READ of a nested double through two descriptor hops
            body.Position.Y = -2.5;
            TableValue nested;
            Check(TableGetRigidBody(body, "position", out nested), "nested table reads by name");
            Vec3 vec = nested.Obj as Vec3;
            Check(nested.Kind == TableValueKind.Table && vec != null, "nested tables surface the member reference");
            Check(vec != null && vec.Y == -2.5, "the reference IS the member — its Y reads directly");
            TableValue y;
            Check(TableGetVec3(vec, "y", out y), "vector component reads by name");
            Check(y.Kind == TableValueKind.Float && y.F == -2.5, "nested double reads through two hops");

            // enum metadata: ProbeSample.weapon names its values and knows its max
            TableTypeInfo sample = TableTypeProbeSample();
            int weapon = -1, samples = -1;
            for (int i = 0; i < sample.Fields.Length; i++)
            {
                switch (sample.Fields[i].Name)
                {
                    case "weapon":
                        weapon = i;
                        break;
                    case "samples":
                        samples = i;
                        break;
                }
            }
            // EnumMax is the declared WIRE max ([max = 15] widening), not the
            // current variant count — future variants decode without clamping
            Check(weapon >= 0 && sample.Fields[weapon].EnumName != null && sample.Fields[weapon].EnumMax == 15,
                "weapon carries enum metadata");
            Check(sample.Fields[weapon].EnumName(0) == "None", "the None value names itself");
            Check(sample.Fields[weapon].EnumName(1) == "Laser", "enum values name themselves");
            Check(sample.Fields[weapon].EnumName(200) == "???", "out-of-set values name as ???");
            Check(sample.Fields[weapon].Guard == "active", "weapon's branch guard");

            // counted array metadata: element kind, bound, count companion
            Check(samples >= 0 && sample.Fields[samples].IsArray && sample.Fields[samples].Counted,
                "samples is a counted array");
            Check(sample.Fields[samples].ArrayBound == 8 && sample.Fields[samples].Kind == 7,
                "bound 8, element kind u16");

            // counted arrays read allocation-free: the member array reference
            // in Obj, the used count in Count — no copy, no ArraySegment box
            ProbeSample ps = new ProbeSample();
            ps.SamplesCount = 2;
            ps.Samples[0] = 7;
            ps.Samples[1] = 8;
            TableValue arr;
            Check(TableGetProbeSample(ps, "samples", out arr), "counted array reads by name");
            Check(arr.Kind == TableValueKind.Array && ReferenceEquals(arr.Obj, ps.Samples) && arr.Count == 2,
                "the member array reference plus the used count");

            // enum set clamps like the read side: out-of-set -> None
            Check(TableSetProbeSample(ps, "weapon", TableValue.FromUint(1)), "TableSet writes an enum by name");
            Check(ps.Weapon == Weapon.Laser, "the enum storage sees the write");
            Check(TableSetProbeSample(ps, "weapon", TableValue.FromUint(200)), "out-of-set enum write is accepted");
            Check(ps.Weapon == Weapon.None, "out-of-set -> None, as the read side does");

            // declared ranges surface for editors (TestData.a is [-100, 100])
            TableTypeInfo testdata = TableTypeTestData();
            int aField = -1;
            for (int i = 0; i < testdata.Fields.Length; i++)
            {
                if (testdata.Fields[i].Name == "a")
                {
                    aField = i;
                }
            }
            Check(aField >= 0 && testdata.Fields[aField].HasRange, "TestData.a has a declared range");
            Check(testdata.Fields[aField].RangeMin == -100.0 && testdata.Fields[aField].RangeMax == 100.0,
                "the [-100, 100] editor range");

            // TableSet clamps exactly like the read side: a = 500 -> 100
            TestData td = new TestData();
            Check(TableSetTestData(td, "a", TableValue.FromInt(500)), "TableSet accepts an editor numeric");
            Check(td.A == 100, "TableSet clamped to the declared max");
            TableValue a;
            Check(TableGetTestData(td, "a", out a), "the clamped value reads back");
            Check(a.Kind == TableValueKind.Int && a.I == 100, "TableGet agrees with the storage");
        }

        // ---- the permissive read contract, exercised with hand-built buffers ----
        {
            // unknown field id: skipped and counted, decode continues
            byte[] unknownField = { 0xEF, 0xBE, 6, 42, 0x00, 0x00 };
            RigidBody output = new RigidBody();
            TableReport report = new TableReport();
            Check(TableReadRigidBody(unknownField, ref output, report), "table read with unknown field");
            Check(report.Unknown == 1 && !report.Malformed, "unknown field counted, not fatal");

            // kind mismatch on a known id (at_rest as f32): skipped, default kept
            byte[] changedKind = { 0xEB, 0xF9, 10, 0, 0, 0, 0, 0x00, 0x00 };
            RigidBody output2 = new RigidBody();
            TableReport report2 = new TableReport();
            Check(TableReadRigidBody(changedKind, ref output2, report2), "table read with changed kind");
            Check(report2.KindMismatch == 1 && !report2.Malformed && !output2.AtRest,
                "changed kind skipped, default kept");

            // truncation: malformed reported, decode stops without crashing
            byte[] truncated = { 0xEB, 0xF9, 1 };
            RigidBody output3 = new RigidBody();
            TableReport report3 = new TableReport();
            Check(!TableReadRigidBody(truncated, ref output3, report3), "truncated table read fails");
            Check(report3.Malformed, "truncation reported as malformed");

            // out-of-range int clamps and counts (TestData.a is [-100, 100])
            TestData narrow = new TestData();
            narrow.A = 50;
            byte[] wire = TableWriteTestData(narrow);
            wire[3] = 200; // low byte of a's i32 payload: 50 -> 200
            TestData output4 = new TestData();
            TableReport report4 = new TableReport();
            Check(TableReadTestData(wire, ref output4, report4), "table read with out-of-range value");
            Check(report4.Clamped == 1 && output4.A == 100, "out-of-range clamps to the declared max");
        }

        // ---- AppendTableX: the reusable zero-allocation write path ----
        {
            RigidBody input = new RigidBody();
            input.Position.X = 1.5; input.Position.Y = -2.5; input.Position.Z = 3.25;
            input.Orientation.X = 0.1; input.Orientation.Y = 0.2; input.Orientation.Z = 0.3; input.Orientation.W = 0.9;
            input.LinearVelocity.X = 10.0; input.LinearVelocity.Y = 20.0; input.LinearVelocity.Z = -3.0;

            byte[] boxed = TableWriteRigidBody(input);

            TableWriter w = new TableWriter();
            AppendTableRigidBody(w, input);
            Check(w.Len == boxed.Length && new System.ReadOnlySpan<byte>(w.Buf, 0, w.Len).SequenceEqual(boxed),
                "AppendTable writes the exact TableWrite bytes");

            // two appends stack in one buffer
            AppendTableRigidBody(w, input);
            Check(w.Len == 2 * boxed.Length, "two appends stack in one buffer");
            Check(new System.ReadOnlySpan<byte>(w.Buf, boxed.Length, boxed.Length).SequenceEqual(boxed),
                "the second appended region carries the exact wire");

            // Clear keeps capacity; steady-state appends never allocate
            w.Clear();
            AppendTableRigidBody(w, input); // warm: capacity settled
            w.Clear();
            long before = System.GC.GetAllocatedBytesForCurrentThread();
            for (int i = 0; i < 100; i++)
            {
                w.Clear();
                AppendTableRigidBody(w, input);
            }
            long after = System.GC.GetAllocatedBytesForCurrentThread();
            Check(after == before, "steady-state AppendTable allocates nothing");
        }

        if (failed)
        {
            return 1;
        }
        Console.WriteLine("OK");
        return 0;
    }
}

// ForeignMessage satisfies the Message base from outside the generated set —
// WriteMessage must refuse it without touching the stream.
sealed class ForeignMessage : Example.Message
{
    public override Example.MessageType Type => Example.MessageType.Chat;
}
