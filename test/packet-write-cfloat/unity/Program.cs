// The shared compressed-float packet-write cases (test/packet-write-cfloat)
// against the generated Unity C# writer (docs/SPEC.md §5.1 row 10,
// PACKET-WIRE-ALGORITHM.md §2.5). Unity C# is an assert leg: in this Debug
// build a NaN, an infinity, or a finite value outside [min, max] trips the
// writer's Debug.Assert, which a throwing TraceListener turns into the
// refusal. A lawful value is written, reads back to its quantized value, and,
// when the case pins a wire, writes exactly those bytes.
using System;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using Cfloat;
using Serialize;

internal sealed class AssertionTrapped : Exception { }

internal sealed class ThrowingListener : TraceListener
{
    public override void Write(string message) { }
    public override void WriteLine(string message) { }
    public override void Fail(string message) { throw new AssertionTrapped(); }
    public override void Fail(string message, string detailMessage) { throw new AssertionTrapped(); }
}

internal static class Program
{
    // PacketCFloat.schema: CFloat.x in [0, 10] at 0.01 (1000 steps);
    // RatioEndpoint.ratio in [0, 1] at 0.01 (100 steps).
    private static int failed;

    private static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed++;
        }
    }

    // The case being run and the failures before it: Answer() prints its
    // "case <name> ok|FAILED: ..." line, the answer tools/casegate reads.
    private static string pending;
    private static int pendingFailed;

    private static void Answer()
    {
        if (pending != null)
        {
            Console.WriteLine("case " + pending + (failed == pendingFailed ? " ok" : " FAILED: see the FAILED line above"));
        }
        pending = null;
    }

    private static float Parse(string text)
    {
        switch (text)
        {
            case "NaN": return float.NaN;
            case "+Inf": return float.PositiveInfinity;
            case "-Inf": return float.NegativeInfinity;
            default: return float.Parse(text, NumberStyles.Float, CultureInfo.InvariantCulture);
        }
    }

    private static float Quantized(float value, bool ratio)
    {
        float delta = ratio ? 1.0f : 10.0f;
        uint steps = ratio ? 100u : 1000u;
        float normalized = value / delta;
        float scaled = (float)(normalized * steps);
        uint integer = (uint)Math.Floor((double)(float)(scaled + 0.5f));
        if (integer > steps) integer = steps;
        float quotient = (float)integer / steps;
        return (float)(quotient * delta);
    }

    // The case writes both fields of a RatioPair (its quantization is
    // RatioEndpoint's, so ratio is set too).
    private static bool pair;

    // Writes one case; returns null when the writer refused (its assert fired
    // or it returned false). bits is what the write emitted before the flush
    // pads the last byte.
    private static byte[] Write(float value, bool ratio, out long bits)
    {
        bits = -1;
        WriteStream stream = new WriteStream(new byte[64]);
        try
        {
            bool wrote;
            if (pair)
            {
                RatioPair p = new RatioPair { First = value, Second = value };
                wrote = Schema.WriteRatioPair(stream, ref p);
            }
            else if (ratio)
            {
                RatioEndpoint r = new RatioEndpoint { Ratio = value };
                wrote = Schema.WriteRatioEndpoint(stream, ref r);
            }
            else
            {
                CFloat c = new CFloat { X = value };
                wrote = Schema.WriteCFloat(stream, ref c);
            }
            if (!wrote || !stream.Ok) return null;
            bits = stream.BitsProcessed;
            stream.Flush();
        }
        catch (AssertionTrapped)
        {
            return null;
        }
        return stream.Ok ? stream.Data.ToArray() : null;
    }

    private static bool Read(byte[] wire, bool ratio, out float got)
    {
        ReadStream reader = new ReadStream(wire);
        if (pair)
        {
            RatioPair p = new RatioPair();
            bool pairRead = Schema.ReadRatioPair(reader, ref p);
            got = p.First;
            return pairRead && BitConverter.SingleToUInt32Bits(p.First) == BitConverter.SingleToUInt32Bits(p.Second);
        }
        if (ratio)
        {
            RatioEndpoint r = new RatioEndpoint();
            bool ok = Schema.ReadRatioEndpoint(reader, ref r);
            got = r.Ratio;
            return ok;
        }
        CFloat c = new CFloat();
        bool read = Schema.ReadCFloat(reader, ref c);
        got = c.X;
        return read;
    }

    private static int Main(string[] args)
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new ThrowingListener());
        int ran = 0;
        foreach (string raw in File.ReadAllLines(args[0]))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#", StringComparison.Ordinal)) continue;
            ran++;
            Answer();
            pending = line.Split((char[])null, StringSplitOptions.RemoveEmptyEntries)[0];
            pendingFailed = failed;
            string[] fields = line.Split((char[])null, StringSplitOptions.RemoveEmptyEntries);
            if (fields.Length < 3 || fields.Length % 2 != 1 || (fields[2] != "refused" && fields[2] != "written"))
            {
                Check(false, "case line '" + line + "' is malformed");
                continue;
            }
            string name = fields[0];
            float value = Parse(fields[1]);
            // [reads-back <v>] [type RatioEndpoint] [wire <hex>] [bits <n>], in pairs
            bool ratio = false;
            pair = false;
            string wireHex = null;
            long wantBits = -1;
            bool malformed = false;
            for (int i = 3; i + 1 < fields.Length; i += 2)
            {
                if (fields[i] == "reads-back") { }
                else if (fields[i] == "type" && (fields[i + 1] == "RatioEndpoint" || fields[i + 1] == "RatioPair")) { ratio = true; pair = fields[i + 1] == "RatioPair"; }
                else if (fields[i] == "wire") wireHex = fields[i + 1].ToLowerInvariant();
                else if (fields[i] == "bits") malformed |= !long.TryParse(fields[i + 1], NumberStyles.None, CultureInfo.InvariantCulture, out wantBits);
                else malformed = true;
            }
            if (malformed)
            {
                Check(false, "case line '" + line + "' has a malformed modifier");
                continue;
            }
            byte[] wire = Write(value, ratio, out long bits);
            if (fields[2] == "refused")
            {
                Check(wire == null, name + " refused");
                continue;
            }
            if (wire == null)
            {
                Check(false, name + " written");
                continue;
            }
            if (wantBits >= 0)
            {
                Check(bits == wantBits, name + " wrote " + bits + " bits before the flush, want " + wantBits);
            }
            if (wireHex != null)
            {
                Check(Convert.ToHexString(wire).ToLowerInvariant() == wireHex, name + " writes the wire " + wireHex);
            }
            float got;
            if (!Read(wire, ratio, out got))
            {
                Check(false, name + " reads back");
                continue;
            }
            Check(BitConverter.SingleToUInt32Bits(got) == BitConverter.SingleToUInt32Bits(Quantized(value, ratio)),
                name + " reads back its quantized value");
        }
        Answer();
        Check(ran > 0, "the shared cases in " + args[0] + " name at least one case");
        Console.WriteLine("packet-write-cfloat Unity C#: ran " + ran + ", failed " + failed);
        return failed == 0 ? 0 : 1;
    }
}
