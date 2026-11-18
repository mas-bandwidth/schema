using System;
using Serialize;
using Packettext;
using static Packettext.Buffers;

static class Program
{
    static string Hex(ReadOnlySpan<byte> bytes) => bytes.Length == 0 ? "-" : Convert.ToHexString(bytes).ToLowerInvariant();

    static void Main()
    {
        byte[] warmWire = { 2, 0xC3, 0xA9 };
        ReadStream warmStream = new ReadStream(warmWire);
        Narrow warmValue = new Narrow();
        for (int i = 0; i < 1000; i++)
        {
            warmStream.Reset(warmWire);
            if (!Schema.ReadNarrow(warmStream, ref warmValue)) throw new Exception("warm read");
        }
        long before = GC.GetAllocatedBytesForCurrentThread();
        for (int i = 0; i < 10000; i++)
        {
            warmStream.Reset(warmWire);
            if (!Schema.ReadNarrow(warmStream, ref warmValue)) throw new Exception("measured read");
        }
        if (GC.GetAllocatedBytesForCurrentThread() != before) throw new Exception("UTF-8 read allocated");
        string line;
        while ((line = Console.ReadLine()) != null)
        {
            byte[] wire = line == "-" ? Array.Empty<byte>() : Convert.FromHexString(line);
            ReadStream r = new ReadStream(wire);
            Narrow value = new Narrow();
            if (!Schema.ReadNarrow(r, ref value)) { Console.WriteLine("REFUSE"); continue; }
            if (!r.Ok) throw new Exception("read accepted with latched error");
            WriteStream w = new WriteStream(new byte[256]);
            if (!Schema.WriteNarrow(w, ref value)) throw new Exception("write refused");
            w.Flush();
            if (!w.Ok) throw new Exception("write error");
            Console.WriteLine($"OK {r.BitsProcessed} {Hex(value.Text.AsSpan()[..value.TextLength])} {w.BitsProcessed} {Hex(w.Data)}");
        }
    }
}
