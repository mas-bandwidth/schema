// Program.cs — the C# card facade's test runner (docs/SPEC-TABLES.md §2,
// §6.2, §16). A plain Main with one check per named test and a nonzero exit
// on any failure; no test framework. Run from the repository root, which is
// where the Makefile runs it.

using System;
using System.IO;
using Cards;

internal static class Program
{
    private static bool _failed;

    private static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            _failed = true;
        }
    }

    private static void SameBytes(byte[] got, byte[] want, string what)
    {
        if (got.Length != want.Length)
        {
            Check(false, what + ": " + got.Length + " bytes, expected " + want.Length);
            return;
        }
        for (int i = 0; i < want.Length; i++)
        {
            if (got[i] != want[i])
            {
                Check(false, what + ": first byte differing at " + i);
                return;
            }
        }
    }

    // TestCardUnitBuilds loads the populated fixture wire and proves the
    // record's wire is the input byte for byte (STEP 2 build proof; the
    // named round-trip tests land with the facade they pin).
    private static void TestCardUnitBuilds()
    {
        byte[] wire = File.ReadAllBytes("tables/cards/fuzz/card-populated.wire");
        Record rec = Record.Load(wire, out _);
        SameBytes(rec.Wire(), wire, "loaded record wire reproduces card-populated.wire");
    }

    private static int Main()
    {
        TestCardUnitBuilds();
        if (_failed)
        {
            Console.WriteLine("cards tests: FAILED");
            return 1;
        }
        Console.WriteLine("cards tests: all checks passed");
        return 0;
    }
}
