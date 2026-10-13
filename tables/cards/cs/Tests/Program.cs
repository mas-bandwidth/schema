// Program.cs — the C# card facade's test runner (docs/SPEC-TABLES.md §2,
// §6.2, §16). A plain Main with one check per named test and a nonzero exit
// on any failure; no test framework. The named tests are the ones the card's
// DONE-WHEN names:
//
//   TestCardRoundTripsThePopulatedFixture — Load/ToJSON/FromJSON/Wire
//     reproduce the committed fixture byte for byte, and the record's wire
//     is a defensive copy.
//   TestCardBoundsRefuseBeforePublishing — the customer ceilings
//     (MaxBodyBytes 65536, MaxListEntries 512) refuse before any bytes are
//     published.
//
// Run from the repository root, which is where the Makefile runs it.

using System;
using System.IO;
using System.Text;
using System.Text.Json;
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

    // TestCardRoundTripsThePopulatedFixture loads
    // tables/cards/fuzz/card-populated.wire, compares every field with
    // card-populated.json, the body byte for byte, and proves ToJSON and
    // FromJSON reproduce both files exactly; it also mutates the caller's
    // input and a Wire result and shows the record unchanged
    // (tables/cards/cards.go: Load, FromJSON, ToJSON, Wire).
    private static void TestCardRoundTripsThePopulatedFixture()
    {
        byte[] wire = File.ReadAllBytes("tables/cards/fuzz/card-populated.wire");
        byte[] json = File.ReadAllBytes("tables/cards/fuzz/card-populated.json");

        Record rec = Record.Load(wire, out _);

        // Every field against the JSON, and the body byte for byte: the
        // record's JSON text is the fixture's JSON text exactly.
        SameBytes(rec.ToJson(), json, "ToJSON reproduces card-populated.json");

        using (JsonDocument doc = JsonDocument.Parse(rec.ToJson()))
        {
            JsonElement root = doc.RootElement;
            Check(root.GetProperty("id").GetString() == "populated", "id round-trips");
            Check(root.GetProperty("tier").GetString() == "flash", "tier round-trips");
            Check(root.GetProperty("priority").GetString() == "critical", "priority round-trips");
            Check(root.GetProperty("_base").GetString() == "base", "_base round-trips");
            Check(root.GetProperty("paths").GetArrayLength() == 2, "paths round-trip");
            Check(root.GetProperty("_new").GetArrayLength() == 1, "_new round-trips");
            Check(root.GetProperty("needs").GetArrayLength() == 2, "needs round-trip");
            Check(root.GetProperty("deadline_minutes").GetInt32() == 45, "deadline_minutes round-trips");
            Check(root.GetProperty("column").GetString() == "review", "column round-trips");
            string body = root.GetProperty("body").GetString();
            using (JsonDocument want = JsonDocument.Parse(json))
            {
                SameBytes(Encoding.UTF8.GetBytes(body),
                    Encoding.UTF8.GetBytes(want.RootElement.GetProperty("body").GetString()),
                    "body byte for byte");
            }
        }

        // FromJSON reproduces the wire file exactly.
        Record rec2 = Record.FromJson(json);
        SameBytes(rec2.Wire(), wire, "FromJSON reproduces card-populated.wire");

        // FromJSON refuses a JSON text that is not an object, as the Go
        // facade does.
        try
        {
            Record.FromJson(Encoding.UTF8.GetBytes("[]"));
            Check(false, "FromJSON refuses a non-object JSON text");
        }
        catch (CardException e)
        {
            Check(e.Message.Contains("malformed"),
                "FromJSON refuses a non-object JSON text as malformed (" + e.Message + ")");
        }

        // The record owns a defensive copy: mutating the caller's input and
        // a Wire result leaves the record unchanged.
        byte[] caller = (byte[])wire.Clone();
        Record rec3 = Record.Load(caller, out _);
        caller[0] ^= 0xff;
        SameBytes(rec3.Wire(), wire, "record unchanged after the caller mutates its input");

        byte[] w = rec3.Wire();
        w[0] ^= 0xff;
        SameBytes(rec3.Wire(), wire, "record unchanged after a Wire result is mutated");
    }

    // TestCardBoundsRefuseBeforePublishing proves the customer ceilings
    // refuse before any bytes are published: 65536 body bytes accepted,
    // 65537 refused naming body and 65536; 512 entries accepted and 513
    // refused naming the field, for each collection (docs/SPEC-TABLES.md
    // §2.5, §2.9; tables/cards/cards.go: checkCard).
    private static void TestCardBoundsRefuseBeforePublishing()
    {
        string okBody = "{\"id\":\"x\",\"body\":\"" + new string('y', 65536) + "\"}";
        Record.FromJson(Encoding.UTF8.GetBytes(okBody));
        Check(true, "65536 body bytes accepted");

        string overBody = "{\"id\":\"x\",\"body\":\"" + new string('y', 65537) + "\"}";
        try
        {
            Record.FromJson(Encoding.UTF8.GetBytes(overBody));
            Check(false, "65537 body bytes refused");
        }
        catch (CardException e)
        {
            Check(e.Message.Contains("body") && e.Message.Contains("65536"),
                "65537 body bytes refused naming body and 65536 (" + e.Message + ")");
        }

        var collections = new (string Field, string Elem)[]
        {
            ("paths", "{\"s\":\"p\"}"),
            ("_new", "{\"s\":\"n\"}"),
            ("shared", "{\"s\":\"s\"}"),
            ("needs", "{\"s\":\"d\"}"),
            ("extra", "{\"key\":\"k\"}"),
            ("consumers", "{\"kind\":\"k\"}"),
        };
        foreach ((string field, string elem) in collections)
        {
            string ok = "{\"id\":\"x\",\"" + field + "\":[" + Join(elem, 512) + "]}";
            Record.FromJson(Encoding.UTF8.GetBytes(ok));
            Check(true, field + " 512 entries accepted");

            string over = "{\"id\":\"x\",\"" + field + "\":[" + Join(elem, 513) + "]}";
            try
            {
                Record.FromJson(Encoding.UTF8.GetBytes(over));
                Check(false, field + " 513 entries refused");
            }
            catch (CardException e)
            {
                Check(e.Message.Contains(field) && e.Message.Contains("512"),
                    field + " 513 entries refused naming the field and 512 (" + e.Message + ")");
            }
        }
    }

    private static string Join(string elem, int n)
    {
        var parts = new string[n];
        for (int i = 0; i < n; i++) parts[i] = elem;
        return string.Join(",", parts);
    }

    private static int Main()
    {
        TestCardRoundTripsThePopulatedFixture();
        TestCardBoundsRefuseBeforePublishing();
        if (_failed)
        {
            Console.WriteLine("cards tests: FAILED");
            return 1;
        }
        Console.WriteLine("cards tests: all checks passed");
        return 0;
    }
}
