using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using Packetwriterefusal;
using Serialize;

internal sealed class AssertionTrapped : Exception { }

internal sealed class ThrowingListener : TraceListener
{
    public override void Write(string? message) { }
    public override void WriteLine(string? message) { }
    public override void Fail(string? message) { throw new AssertionTrapped(); }
    public override void Fail(string? message, string? detailMessage) { throw new AssertionTrapped(); }
}

// write-refusal-packet, C# leg (docs/SPEC.md §5.1). C# is an assert leg: the
// check is a Debug.Assert, answered here through a throwing TraceListener, and
// a Release build carries no write check by design. Each case starts from the
// type's default value and changes ONE field, the same values the Go leg's
// probe uses (test/conformance/harness/writerefusalpacket_test.go).
static class Program
{
    delegate void Setter(ref PacketWriteRefusal v);

    static readonly Dictionary<string, Setter> Cases = new Dictionary<string, Setter>
    {
        ["scalar_high"] = (ref PacketWriteRefusal v) => { v.Scalar = 11; },
        ["scalar_low"] = (ref PacketWriteRefusal v) => { v.Scalar = -1; },
        ["scalar_at_max"] = (ref PacketWriteRefusal v) => { v.Scalar = 10; },
        ["scalar_at_min"] = (ref PacketWriteRefusal v) => { v.Scalar = 0; },
        ["bits_wide"] = (ref PacketWriteRefusal v) => { v.Narrow = 200; },
        ["bits_at_max"] = (ref PacketWriteRefusal v) => { v.Narrow = 127; },
        ["enum_high"] = (ref PacketWriteRefusal v) => { v.Grade = (Grade)4; },
        ["enum_at_max"] = (ref PacketWriteRefusal v) => { v.Grade = Schema.GradeMax; },
        ["union_tag_high"] = (ref PacketWriteRefusal v) => { v.Shot.Type = (ShotType)3; },
        ["union_tag_at_max"] = (ref PacketWriteRefusal v) => { v.Shot.Type = Schema.ShotTypeMax; },
        ["count_high"] = (ref PacketWriteRefusal v) => { v.WindowCount = 9; },
        ["count_low"] = (ref PacketWriteRefusal v) => { v.WindowCount = 1; },
        ["count_at_max"] = (ref PacketWriteRefusal v) => { v.WindowCount = 8; },
        ["count_at_min"] = (ref PacketWriteRefusal v) => { v.WindowCount = 2; },
        ["text_long"] = (ref PacketWriteRefusal v) => { v.TextLength = 9; },
        ["text_illformed"] = (ref PacketWriteRefusal v) => { v.Text[0] = 0xff; v.TextLength = 1; },
        ["text_zero"] = (ref PacketWriteRefusal v) => { v.Text[0] = 0; v.TextLength = 1; },
        ["text_at_max"] = (ref PacketWriteRefusal v) => { for (int i = 0; i < 8; i++) { v.Text[i] = (byte)'a'; } v.TextLength = 8; },
        ["const_wrong"] = (ref PacketWriteRefusal v) => { v.FixedConstant = 6; },
        ["const_at"] = (ref PacketWriteRefusal v) => { v.FixedConstant = 5; },
        ["reserved_wrong"] = (ref PacketWriteRefusal v) => { v.ReservedZero = 1; },
        ["reserved_at"] = (ref PacketWriteRefusal v) => { v.ReservedZero = 0; },
    };

    // Cases C# cannot hold, named so that a case neither run nor named fails.
    // Every row is representable in C# (a byte enum casts, a struct tag casts).
    static readonly HashSet<string> NotRepresentable = new HashSet<string>();

    static int Main(string[] args)
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new ThrowingListener());
        int passed = 0, failed = 0;
        HashSet<string> seen = new HashSet<string>();
        foreach (string raw in File.ReadLines(args[0]))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#")) continue;
            string[] parts = line.Split('|');
            string name = parts[0].Trim();
            string verdict = parts[1].Trim();
            seen.Add(name);
            if (NotRepresentable.Contains(name)) { passed++; continue; }
            if (!Cases.TryGetValue(name, out Setter? set))
            {
                Console.WriteLine("FAILED: write-refusal-packet " + name + ": neither run nor named not representable in C#");
                failed++;
                continue;
            }
            PacketWriteRefusal value = new PacketWriteRefusal();
            set(ref value);
            WriteStream stream = new WriteStream(new byte[256]);
            bool refused = false;
            try
            {
                if (!Schema.WritePacketWriteRefusal(stream, ref value))
                {
                    Console.WriteLine("FAILED: write-refusal-packet " + name + ": write returned false");
                    failed++;
                    continue;
                }
                stream.Flush();
            }
            catch (AssertionTrapped)
            {
                refused = true;
            }
            if (verdict == "refused")
            {
                if (!refused) { Console.WriteLine("FAILED: write-refusal-packet " + name + ": was not refused"); failed++; }
                else passed++;
                continue;
            }
            if (refused) { Console.WriteLine("FAILED: write-refusal-packet " + name + ": lawful value was refused"); failed++; continue; }
            ReadStream reader = new ReadStream(stream.Data.ToArray());
            PacketWriteRefusal readback = new PacketWriteRefusal();
            if (!Schema.ReadPacketWriteRefusal(reader, ref readback))
            {
                Console.WriteLine("FAILED: write-refusal-packet " + name + ": did not read back");
                failed++;
                continue;
            }
            passed++;
        }
        foreach (string known in Cases.Keys)
        {
            if (!seen.Contains(known)) { Console.WriteLine("FAILED: write-refusal-packet " + known + ": driver case absent from cases.txt"); failed++; }
        }
        Console.WriteLine("write-refusal-packet C#: " + passed + " passed, " + failed + " failed");
        return failed == 0 ? 0 : 1;
    }
}
