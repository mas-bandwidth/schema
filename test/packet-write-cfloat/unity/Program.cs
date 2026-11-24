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

    // Writes one case; returns null when the writer refused (its assert fired
    // or it returned false).
    private static byte[] Write(float value, bool ratio)
    {
        WriteStream stream = new WriteStream(new byte[64]);
        try
        {
            bool wrote;
            if (ratio)
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
            string[] fields = line.Split((char[])null, StringSplitOptions.RemoveEmptyEntries);
            if (fields.Length < 3 || fields.Length % 2 != 1 || (fields[2] != "refused" && fields[2] != "written"))
            {
                Check(false, "case line '" + line + "' is malformed");
                continue;
            }
            string name = fields[0];
            float value = Parse(fields[1]);
            // [reads-back <v>] [type RatioEndpoint] [wire <hex>], in pairs
            bool ratio = false;
            string wireHex = null;
            bool malformed = false;
            for (int i = 3; i + 1 < fields.Length; i += 2)
            {
                if (fields[i] == "reads-back") { }
                else if (fields[i] == "type" && fields[i + 1] == "RatioEndpoint") ratio = true;
                else if (fields[i] == "wire") wireHex = fields[i + 1].ToLowerInvariant();
                else malformed = true;
            }
            if (malformed)
            {
                Check(false, "case line '" + line + "' has a malformed modifier");
                continue;
            }
            byte[] wire = Write(value, ratio);
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
        Check(ran > 0, "the shared cases in " + args[0] + " name at least one case");
        Console.WriteLine("packet-write-cfloat Unity C#: ran " + ran + ", failed " + failed);
        return failed == 0 ? 0 : 1;
    }
}
