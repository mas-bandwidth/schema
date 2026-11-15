using System;
using System.IO;
using Serialize;
using Packetend;
using static Packetend.Schema;

static class Program
{
    static bool failed;

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed = true;
        }
    }

    static byte[] Vector(string dir, string name)
    {
        return File.ReadAllBytes(Path.Combine(dir, name));
    }

    static int Main(string[] args)
    {
        string dir = args.Length > 0 ? args[0] : "testdata/wire/packet-end";

        End exact = new End();
        ReadStream exactStream = new ReadStream(Vector(dir, "exact.bin"));
        Check(ReadEnd(exactStream, ref exact), "exact packet accepted");
        Check(exact.Value == 3, "exact packet reads as 3");

        End trailing = new End();
        Check(!ReadEnd(new ReadStream(Vector(dir, "trailing.bin")), ref trailing),
            "trailing byte refused");

        End padding = new End();
        Check(!ReadEnd(new ReadStream(Vector(dir, "padding.bin")), ref padding),
            "final-byte unused bits refused");

        if (failed)
        {
            return 1;
        }
        Console.WriteLine("packet end C#: exact packet accepted as 3, trailing byte and final-byte padding refused");
        return 0;
    }
}
