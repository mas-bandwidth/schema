using System;
using System.IO;
using Tabledemo;

// cell-cs-flags-values-valid-data — Flags masks: valid-data write/read acceptance.
//
// The law (docs/FIXED-FORM-ALGORITHM.md §3.4 "record, flags";
// docs/SPEC-TABLES.md §3.4 RANGED `flags` rule):
// on the table wire a `flags` mask rides as its DECLARED bit width
// (W bits, not the variable form's raw 64), one bit per declared variant
// from Perks { Shielded = bit 0, Cloaked = bit 1, Turbo = bit 2 }
// (tables/examples/Tables.schema:22). The C# leg's `LoadoutConfig.Perks`
// is `ulong` and the writer packs and reads only the LOW W bits per
// `Field.Kind == 9` (the flag kind) in
// build/tables-generated-cs/examples/TablesTable.cs.
//
// This test proves three things on the cs leg:
//   1. a flags field round-trips an ALL-BITS-SET mask (0b111) — the
//      multi-bit case the law exercises;
//   2. the wire writes and reads back ZERO (no bits), Shielded alone,
//      and the all-bits-set mask correctly;
//   3. the pinned C++ leg's loadout (testdata/wire/tables/loadout_full.bin,
//      which carries Perks=Shielded) reads back as Perks = 0x1 on this
//      leg — the cross-leg assertion.
//
// Source of mask values: tables/examples/Tables.schema:22 declares the
// three variants in order, so the bits are 0x1 (Shielded), 0x2 (Cloaked),
// 0x4 (Turbo); `0x7` is the union of all three.
// Source of the golden loadout: test/tables/main.cpp:3282 sets
// `loadout.perks = tabledemo::Perks_Shielded` for `build_golden_loadout`,
// pinned to testdata/wire/tables/loadout_full.bin and
// testdata/conformance/tables/cook-write/loadout_full.cook by the C++
// leg.

internal static class Program
{
    static int Main()
    {
        bool ok = true;

        ok &= CheckRoundTrip(0ul,                  "zero mask");
        ok &= CheckRoundTrip(0x1ul,                "Shielded only");
        ok &= CheckRoundTrip(0x2ul,                "Cloaked only");
        ok &= CheckRoundTrip(0x4ul,                "Turbo only");
        ok &= CheckRoundTrip(0x3ul,                "Shielded + Cloaked (0b011)");
        ok &= CheckRoundTrip(0x7ul,                "all three bits set (0b111)");

        ok &= CheckPinnedGolden();

        if (!ok)
        {
            return 1;
        }

        Console.WriteLine("Flags masks: valid-data write/read acceptance");
        return 0;
    }

    static bool CheckRoundTrip(ulong wantMask, string label)
    {
        // Build a LoadoutConfig with the wanted Perks value, then save and
        // load through the C# writer/reader. Other fields stay at their
        // declared defaults (the LoadoutConfig ctor fills them) so the
        // diff is just the Perks mask.
        Tabledemo.LoadoutConfig src = new Tabledemo.LoadoutConfig();
        src.Perks = wantMask;

        long need = Tabledemo.Schema.LoadoutConfigMeasure(src);
        if (need <= 0)
        {
            Console.Error.WriteLine("FAIL: LoadoutConfigMeasure returned {0} for {1}", need, label);
            return false;
        }

        byte[] buffer = new byte[need];
        long wrote = Tabledemo.Schema.LoadoutConfigSave(src, buffer);
        if (wrote != need)
        {
            Console.Error.WriteLine(
                "FAIL: LoadoutConfigSave wrote {0} bytes, want {1} (measure) for {2}",
                wrote, need, label);
            return false;
        }

        Tabledemo.LoadoutConfig dst = new Tabledemo.LoadoutConfig();
        Tabledemo.TableReport report = new Tabledemo.TableReport();
        if (!Tabledemo.Schema.LoadoutConfigLoad(dst, buffer, report))
        {
            Console.Error.WriteLine(
                "FAIL: LoadoutConfigLoad refused {0} (kind_mismatch={1}, unknown={2}, clamped={3}, malformed={4}, refused={5}, reason='{6}')",
                label, report.KindMismatch, report.Unknown, report.Clamped,
                report.Malformed, report.Refused, report.Reason);
            return false;
        }
        if (report.Unknown != 0 || report.KindMismatch != 0 ||
            report.Clamped != 0 || report.Malformed || report.Refused)
        {
            Console.Error.WriteLine(
                "FAIL: LoadoutConfigLoad {0} reported damage: unknown={1}, kind_mismatch={2}, clamped={3}, malformed={4}, refused={5}, reason='{6}'",
                label, report.Unknown, report.KindMismatch, report.Clamped,
                report.Malformed, report.Refused, report.Reason);
            return false;
        }

        if (dst.Perks != wantMask)
        {
            Console.Error.WriteLine(
                "FAIL: round-trip {0}: want Perks=0x{1:x}, got 0x{2:x}",
                label, wantMask, dst.Perks);
            return false;
        }

        Console.WriteLine("OK: round-trip {0} Perks=0x{1:x} ({2} bytes)",
            label, wantMask, need);
        return true;
    }

    static bool CheckPinnedGolden()
    {
        // The C++ leg's `build_golden_loadout` sets Perks=Shielded (0x1)
        // (test/tables/main.cpp:3282) and writes the bytes to
        // testdata/wire/tables/loadout_full.bin, where the cook-master
        // pins them as well (testdata/conformance/tables/cook-write/
        // loadout_full.cook). A C# leg that asserts the same Perks
        // value proves the two implementations agree on the wire's
        // mask encoding.
        const string path = "../../../../../testdata/wire/tables/loadout_full.bin";
        byte[] bytes;
        try
        {
            bytes = File.ReadAllBytes(path);
        }
        catch (Exception ex)
        {
            Console.Error.WriteLine(
                "FAIL: could not read pinned golden {0}: {1}", path, ex.Message);
            return false;
        }

        Tabledemo.LoadoutConfig dst = new Tabledemo.LoadoutConfig();
        Tabledemo.TableReport report = new Tabledemo.TableReport();
        if (!Tabledemo.Schema.LoadoutConfigLoad(dst, bytes, report))
        {
            Console.Error.WriteLine(
                "FAIL: pinned golden refused (kind_mismatch={0}, unknown={1}, clamped={2}, malformed={3}, refused={4}, reason='{5}')",
                report.KindMismatch, report.Unknown, report.Clamped,
                report.Malformed, report.Refused, report.Reason);
            return false;
        }

        // The golden sets exactly Shielded (bit 0); Cloaked and Turbo are
        // off, so the mask is 0x1, not 0x7.
        const ulong want = 0x1ul;
        if (dst.Perks != want)
        {
            Console.Error.WriteLine(
                "FAIL: pinned golden — want Perks=0x{0:x} (Shielded), got 0x{1:x}",
                want, dst.Perks);
            return false;
        }

        Console.WriteLine("OK: pinned golden read back as Perks=0x{0:x} (Shielded)", want);
        return true;
    }
}
