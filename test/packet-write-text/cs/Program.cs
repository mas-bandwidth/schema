using System;
using System.Diagnostics;
using System.IO;
using Packetwritetext;
using Serialize;

internal sealed class AssertionTrapped : Exception { }

internal sealed class ThrowingListener : TraceListener
{
    public override void Write(string? message) { }
    public override void WriteLine(string? message) { }
    public override void Fail(string? message) { throw new AssertionTrapped(); }
    public override void Fail(string? message, string? detailMessage) { throw new AssertionTrapped(); }
}

static class Program
{
    static int Main(string[] args)
    {
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new ThrowingListener());
        int passed = 0, failed = 0;
        foreach (string raw in File.ReadLines(args[0]))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#")) continue;
            string[] parts = line.Split('|');
            string name = parts[0].Trim();
            string hex = parts[1].Trim();
            string verdict = parts[2].Trim();
            byte[] content = hex == "-" ? Array.Empty<byte>() : Convert.FromHexString(hex.Replace(" ", ""));

            Text value = new Text();
            content.AsSpan().CopyTo(value.Value[..]);
            value.ValueLength = content.Length;

            WriteStream stream = new WriteStream(new byte[256]);
            bool refused = false;
            try
            {
                if (!Schema.WriteText(stream, ref value))
                {
                    Console.WriteLine("FAILED: " + name + " write returned false");
                    failed++;
                    continue;
                }
                stream.Flush();
                if (!stream.Ok)
                {
                    Console.WriteLine("FAILED: " + name + " write latched a stream error");
                    failed++;
                    continue;
                }
            }
            catch (AssertionTrapped)
            {
                refused = true;
            }

            if (verdict == "refused")
            {
                if (!refused)
                {
                    Console.WriteLine("FAILED: " + name + " was not refused");
                    failed++;
                }
                else
                {
                    passed++;
                }
                continue;
            }

            if (refused)
            {
                Console.WriteLine("FAILED: " + name + " was refused");
                failed++;
                continue;
            }

            ReadStream reader = new ReadStream(stream.Data.ToArray());
            Text readback = new Text();
            if (!Schema.ReadText(reader, ref readback))
            {
                Console.WriteLine("FAILED: " + name + " did not read back");
                failed++;
                continue;
            }
            if (readback.ValueLength != content.Length ||
                !content.AsSpan().SequenceEqual(readback.Value[..readback.ValueLength]))
            {
                Console.WriteLine("FAILED: " + name + " did not round-trip its used bytes");
                failed++;
                continue;
            }
            passed++;
        }
        Console.WriteLine("packet write text C#: " + passed + " passed, " + failed + " failed");
        return failed == 0 ? 0 : 1;
    }
}
