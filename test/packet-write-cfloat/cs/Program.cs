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
    // CFloat.schema: x float32 | min = 0.0, max = 10.0, resolution = 0.01.
    const float Min = 0.0f;
    const float Delta = 10.0f;
    const uint Steps = 1000u;

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

    static bool Refused(float value)
    {
        CFloat input = new CFloat { X = value };
        WriteStream stream = new WriteStream(new byte[64]);
        bool wrote = false;
        bool asserted = AssertFires(() => { wrote = Schema.WriteCFloat(stream, ref input); });
        return asserted || !wrote;
    }

    static bool Written(float value, out float readBack)
    {
        readBack = 0.0f;
        CFloat input = new CFloat { X = value };
        WriteStream stream = new WriteStream(new byte[64]);
        bool wrote = false;
        bool asserted = AssertFires(() => { wrote = Schema.WriteCFloat(stream, ref input); });
        if (asserted || !wrote || !stream.Ok)
        {
            return false;
        }
        stream.Flush();
        if (!stream.Ok)
        {
            return false;
        }
        ReadStream reader = new ReadStream(stream.Data.ToArray());
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
    static float Quantized(float value)
    {
        float normalized = (value - Min) / Delta;
        float scaled = (float)(normalized * Steps);
        uint integer = (uint)Math.Floor((double)(float)(scaled + 0.5f));
        if (integer > Steps)
        {
            integer = Steps;
        }
        float quotient = (float)integer / Steps;
        float product = (float)(quotient * Delta);
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
        foreach (string raw in File.ReadAllLines(cases))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#", StringComparison.Ordinal))
            {
                continue;
            }
            string[] fields = line.Split((char[]?)null, StringSplitOptions.RemoveEmptyEntries);
            if (fields.Length < 3)
            {
                Check(false, "case line '" + line + "' is not <name> <value> <verdict>");
                continue;
            }
            string name = fields[0];
            float value = Parse(fields[1]);
            string verdict = fields[2];
            if (verdict == "refused")
            {
                Check(Refused(value), name + " refused");
                continue;
            }
            if (verdict != "written")
            {
                Check(false, name + " has unknown verdict " + verdict);
                continue;
            }
            written++;
            if (!Written(value, out float got))
            {
                Check(false, name + " written");
                continue;
            }
            float want = Quantized(value);
            Check(Bits(got) == Bits(want), name + " reads back its quantized value");
        }
        if (failed != 0)
        {
            return 1;
        }
        Console.WriteLine("packet-write-cfloat C#: refused cases refused, " + written + " lawful cases read back quantized");
        return 0;
    }
}
