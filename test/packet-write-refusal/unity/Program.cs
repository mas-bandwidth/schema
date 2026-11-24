// write-refusal-packet, Unity C# leg (docs/SPEC.md §5.1). The packet Write is
// an assert leg: in this Debug build every write check is a Debug.Assert,
// which a throwing TraceListener turns into the refusal. Each case starts from
// the construction form and changes ONE field; a refused value must trap, and
// its lawful twin at the same boundary must be written. The one row list is
// test/conformance/write-refusal-packet/rows.txt (<name> <verdict> <op> <arg>),
// keyed here by name.
using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using Packetrefusal;
using Serialize;

internal sealed class AssertionTrapped : Exception { }

internal sealed class ThrowingListener : TraceListener
{
    public override void Write(string message) { }
    public override void WriteLine(string message) { }
    public override void Fail(string message) { throw new AssertionTrapped(); }
    public override void Fail(string message, string detailMessage) { throw new AssertionTrapped(); }
}

internal delegate void Setter(ref PacketWriteRefusal value);

internal static class Program
{
    // The cases the driver can build. A case that cannot be built in the leg
    // would be named here as not representable; Unity C# builds every one.
    private static readonly Dictionary<string, Setter> Cases = new Dictionary<string, Setter>
    {
        // write-refusal-packet setters: begin
        { "scalar_high", (ref PacketWriteRefusal v) => v.Scalar = 11 },
        { "scalar_low", (ref PacketWriteRefusal v) => v.Scalar = -1 },
        { "scalar_at_max", (ref PacketWriteRefusal v) => v.Scalar = 10 },
        { "scalar_at_min", (ref PacketWriteRefusal v) => v.Scalar = 0 },
        { "bits_wide", (ref PacketWriteRefusal v) => v.Narrow = 200 },
        { "bits_just_wide", (ref PacketWriteRefusal v) => v.Narrow = 128 },
        { "bits_at_max", (ref PacketWriteRefusal v) => v.Narrow = 127 },
        { "enum_high", (ref PacketWriteRefusal v) => v.Grade = (Grade)4 },
        { "enum_at_max", (ref PacketWriteRefusal v) => v.Grade = Grade.Gold },
        { "union_tag_high", (ref PacketWriteRefusal v) => v.Shot.Type = (ShotType)3 },
        { "union_tag_at_max", (ref PacketWriteRefusal v) => v.Shot.Type = Schema.ShotTypeMax },
        { "count_high", (ref PacketWriteRefusal v) => v.WindowCount = 9 },
        { "count_low", (ref PacketWriteRefusal v) => v.WindowCount = 1 },
        { "count_at_max", (ref PacketWriteRefusal v) => v.WindowCount = 8 },
        { "count_at_min", (ref PacketWriteRefusal v) => v.WindowCount = 2 },
        { "text_long", (ref PacketWriteRefusal v) => v.TextLength = 9 },
        { "text_illformed", (ref PacketWriteRefusal v) => { v.Text[0] = 0xff; v.TextLength = 1; } },
        { "text_zero", (ref PacketWriteRefusal v) => { v.Text[0] = 0; v.TextLength = 1; } },
        { "text_at_max", (ref PacketWriteRefusal v) =>
            {
                for (int i = 0; i < 8; i++) v.Text[i] = (byte)'a';
                v.TextLength = 8;
            } },
        { "const_wrong", (ref PacketWriteRefusal v) => v.FixedConstant = 6 },
        { "const_at", (ref PacketWriteRefusal v) => v.FixedConstant = 5 },
        { "reserved_wrong", (ref PacketWriteRefusal v) => v.ReservedZero = 1 },
        { "reserved_at", (ref PacketWriteRefusal v) => v.ReservedZero = 0 },
        { "flags_wide", (ref PacketWriteRefusal v) => v.Perks = (Perk)(1ul << 3) },
        { "flags_wide_high", (ref PacketWriteRefusal v) => v.Perks = (Perk)(1ul << 40) },
        { "flags_at_max", (ref PacketWriteRefusal v) => v.Perks = Perk.Shield | Perk.Boost | Perk.Cloak },
        // write-refusal-packet setters: end
    };

    private static int Main(string[] args)
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new ThrowingListener());
        int passed = 0, failed = 0;
        HashSet<string> seen = new HashSet<string>();
        foreach (string raw in File.ReadLines(args[0]))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#", StringComparison.Ordinal)) continue;
            string[] parts = line.Split(new[] { ' ', '\t' }, StringSplitOptions.RemoveEmptyEntries);
            if (parts.Length != 4)
            {
                Console.WriteLine("FAILED: a row is not <name> <verdict> <op> <arg>: " + line);
                failed++;
                continue;
            }
            string name = parts[0];
            string verdict = parts[1];
            if (!seen.Add(name))
            {
                Console.WriteLine("FAILED: " + name + " named twice in rows.txt");
                failed++;
                continue;
            }
            Setter set;
            if (!Cases.TryGetValue(name, out set))
            {
                Console.WriteLine("FAILED: " + name + " a row in rows.txt this driver has no setter for");
                failed++;
                continue;
            }
            PacketWriteRefusal value = Schema.NewPacketWriteRefusal();
            set(ref value);
            WriteStream stream = new WriteStream(new byte[256]);
            bool refused = false;
            try
            {
                if (!Schema.WritePacketWriteRefusal(stream, ref value))
                {
                    Console.WriteLine("FAILED: " + name + " write returned false");
                    failed++;
                    continue;
                }
                stream.Flush();
            }
            catch (AssertionTrapped)
            {
                refused = true;
            }
            if (verdict == "refused" && !refused)
            {
                Console.WriteLine("FAILED: " + name + " was not refused");
                failed++;
            }
            else if (verdict == "wrote" && refused)
            {
                Console.WriteLine("FAILED: " + name + " lawful value at the boundary was refused");
                failed++;
            }
            else if (verdict != "refused" && verdict != "wrote")
            {
                Console.WriteLine("FAILED: " + name + " unknown verdict " + verdict);
                failed++;
            }
            else
            {
                passed++;
            }
        }
        foreach (string name in Cases.Keys)
        {
            if (!seen.Contains(name))
            {
                Console.WriteLine("FAILED: " + name + " a setter this driver runs that rows.txt does not name");
                failed++;
            }
        }
        Console.WriteLine("write-refusal-packet Unity C#: " + seen.Count + " rows, " + passed + " passed, " + failed + " failed");
        return failed == 0 ? 0 : 1;
    }
}
