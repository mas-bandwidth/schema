// The shared compressed-float packet-write cases (test/packet-write-cfloat) run
// against the generated C# CFloat writer. The contract is SPEC §5.1 row 10 and
// PACKET-WIRE-ALGORITHM.md §2.5: a NaN, an infinity, or a finite value outside
// [min, max] is refused in a debug build by the generated writer's Debug.Assert;
// a lawful value is written and reads back to its quantized value. A Debug build
// removes nothing here, and the assert is observed through a throwing listener.

using System;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using Cfloat;
using Serialize;

sealed class WriterAssertFired : Exception
{
    public WriterAssertFired(string message) : base(message) { }
}

sealed class ThrowingTraceListener : TraceListener
{
    public override void Write(string? message) { }
    public override void WriteLine(string? message) { }
    public override void Fail(string? message) => throw new WriterAssertFired(message ?? "Debug.Assert");
    public override void Fail(string? message, string? detailMessage) => throw new WriterAssertFired(message ?? "Debug.Assert");
}

static class Program
{
    // PacketCFloat.schema: CFloat.x float32 | min = 0.0, max = 10.0,
    // resolution = 0.01; RatioEndpoint.ratio float32 | min = 0.0, max = 1.0,
    // resolution = 0.01.
    const float Min = 0.0f;
    const float Delta = 10.0f;
    const uint Steps = 1000u;
    const float RatioDelta = 1.0f;
    const uint RatioSteps = 100u;

    static int failed;

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed++;
        }
    }

    static bool AssertFires(Action action)
    {
        TraceListener[] saved = new TraceListener[Trace.Listeners.Count];
        Trace.Listeners.CopyTo(saved, 0);
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new ThrowingTraceListener());
        try
        {
            action();
            return false;
        }
        catch (WriterAssertFired)
        {
            return true;
        }
        finally
        {
            Trace.Listeners.Clear();
            Trace.Listeners.AddRange(saved);
        }
    }

    static bool WriteOne(WriteStream stream, float value, bool ratio)
    {
        if (ratio)
        {
            RatioEndpoint r = new RatioEndpoint { Ratio = value };
            return Schema.WriteRatioEndpoint(stream, ref r);
        }
        CFloat input = new CFloat { X = value };
        return Schema.WriteCFloat(stream, ref input);
    }

    static bool Refused(float value, bool ratio)
    {
        WriteStream stream = new WriteStream(new byte[64]);
        bool wrote = false;
        bool asserted = AssertFires(() => { wrote = WriteOne(stream, value, ratio); });
        return asserted || !wrote;
    }

    static bool Written(float value, bool ratio, out float readBack, out byte[] wire)
    {
        readBack = 0.0f;
        wire = Array.Empty<byte>();
        WriteStream stream = new WriteStream(new byte[64]);
        bool wrote = false;
        bool asserted = AssertFires(() => { wrote = WriteOne(stream, value, ratio); });
        if (asserted || !wrote || !stream.Ok)
        {
            return false;
        }
        stream.Flush();
        if (!stream.Ok)
        {
            return false;
        }
        wire = stream.Data.ToArray();
        ReadStream reader = new ReadStream(wire);
        if (ratio)
        {
            RatioEndpoint r = new RatioEndpoint();
            if (!Schema.ReadRatioEndpoint(reader, ref r))
            {
                return false;
            }
            readBack = r.Ratio;
            return true;
        }
        CFloat output = new CFloat();
        if (!Schema.ReadCFloat(reader, ref output))
        {
            return false;
        }
        readBack = output.X;
        return true;
    }

    // The normative write quantization and decode (PACKET-WIRE-ALGORITHM §2.5):
    // round to the step, clamp only the INTEGER at the top, then dequantize with
    // float32 operations in order.
    static float Quantized(float value, bool ratio)
    {
        float delta = ratio ? RatioDelta : Delta;
        uint steps = ratio ? RatioSteps : Steps;
        float normalized = (value - Min) / delta;
        float scaled = (float)(normalized * steps);
        uint integer = (uint)Math.Floor((double)(float)(scaled + 0.5f));
        if (integer > steps)
        {
            integer = steps;
        }
        float quotient = (float)integer / steps;
        float product = (float)(quotient * delta);
        return (float)(product + Min);
    }

    static uint Bits(float value) => BitConverter.SingleToUInt32Bits(value);

    static float Parse(string text)
    {
        switch (text)
        {
            case "NaN": return float.NaN;
            case "+Inf": return float.PositiveInfinity;
            case "-Inf": return float.NegativeInfinity;
            default: return float.Parse(text, NumberStyles.Float, CultureInfo.InvariantCulture);
        }
    }

    static int Main(string[] args)
    {
        string dir = args.Length > 0 ? args[0] : "test/packet-write-cfloat";
        string cases = Path.Combine(dir, "cases.txt");
        int written = 0;
        int ran = 0;
        foreach (string raw in File.ReadAllLines(cases))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#", StringComparison.Ordinal))
            {
                continue;
            }
            ran++;
            string[] fields = line.Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries);
            if (fields.Length < 3)
            {
                Check(false, "case line '" + line + "' is not <name> <value> <verdict>");
                continue;
            }
            string name = fields[0];
            float value = Parse(fields[1]);
            string verdict = fields[2];
            bool ratio = false;
            string? wireHex = null;
            bool malformed = fields.Length % 2 != 1;
            for (int i = 3; i + 1 < fields.Length; i += 2)
            {
                switch (fields[i])
                {
                    case "reads-back": break;
                    case "type": ratio = fields[i + 1] == "RatioEndpoint"; malformed |= !ratio; break;
                    case "wire": wireHex = fields[i + 1].ToLowerInvariant(); break;
                    default: malformed = true; break;
                }
            }
            if (malformed)
            {
                Check(false, "case line '" + line + "' has a malformed modifier");
                continue;
            }
            if (verdict == "refused")
            {
                Check(Refused(value, ratio), name + " refused");
                continue;
            }
            if (verdict != "written")
            {
                Check(false, name + " has unknown verdict " + verdict);
                continue;
            }
            written++;
            if (!Written(value, ratio, out float got, out byte[] wire))
            {
                Check(false, name + " written");
                continue;
            }
            if (wireHex != null)
            {
                Check(Convert.ToHexString(wire).ToLowerInvariant() == wireHex, name + " writes the wire " + wireHex);
            }
            float want = Quantized(value, ratio);
            Check(Bits(got) == Bits(want), name + " reads back its quantized value");
        }
        // the count line build/mustrun holds to the registered count: a case
        // file that lost its rows runs nothing and is red
        Check(ran > 0, "the shared cases in " + cases + " name at least one case");
        Console.WriteLine("packet-write-cfloat C# (" + written + " lawful cases read back quantized): ran " + ran + ", failed " + failed);
        return failed == 0 ? 0 : 1;
    }
}
