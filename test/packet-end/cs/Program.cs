using System;
using System.IO;
using Serialize;
using Packetend;
using static Packetend.Schema;

static class Program
{
    static bool failed;

    // one counted unit per vector: Main prints "ran N, failed M" and
    // build/mustrun holds N to the count the recipe registers
    static int ran;
    static int failures;

    static readonly string[] Known = { "exact.bin", "trailing.bin", "padding.bin" };

    // The directory is held to the vectors this driver reads: a .bin it does
    // not read is red, so a vector added to the shared corpus cannot ride
    // unread. A named vector that is gone throws at its read.
    static void HoldDirectory(string dir)
    {
        foreach (string path in Directory.GetFiles(dir, "*.bin"))
        {
            string name = Path.GetFileName(path);
            if (Array.IndexOf(Known, name) < 0)
            {
                Console.WriteLine("FAILED: vector " + name + " is in " + dir + " and this driver does not read it");
                failures++;
                failed = true;
            }
        }
    }

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failures++;
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
        HoldDirectory(dir);

        ran++;
        End exact = new End();
        ReadStream exactStream = new ReadStream(Vector(dir, "exact.bin"));
        Check(ReadEnd(exactStream, ref exact), "exact packet accepted");
        Check(exact.Value == 3, "exact packet reads as 3");

        ran++;
        End trailing = new End();
        Check(!ReadEnd(new ReadStream(Vector(dir, "trailing.bin")), ref trailing),
            "trailing byte refused");

        ran++;
        End padding = new End();
        Check(!ReadEnd(new ReadStream(Vector(dir, "padding.bin")), ref padding),
            "final-byte unused bits refused");

        if (!failed)
        {
            Console.WriteLine("packet end C#: exact packet accepted as 3, trailing byte and final-byte padding refused");
        }
        Console.WriteLine("packet-end-cs: ran " + ran + ", failed " + (failed ? Math.Min(failures, ran) : 0));
        return (failed || ran == 0) ? 1 : 0;
    }
}
