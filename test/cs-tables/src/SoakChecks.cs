using System;
using System.Diagnostics;
using Demo = Tabledemo;
using V1 = Tblv1;
using V2 = Tblv2;

static partial class Program
{
    // THE C# SOAK (docs/PORTING.md I9, schema#416; the twin of the C soak).
    //
    // The read, measure and save paths are run over the shared golden corpus in
    // a loop for the seconds asked, and the instrument is the runtime's own
    // managed allocator count: GC.GetAllocatedBytesForCurrentThread() read
    // either side of the measured loop must not move. A live-heap sample is a
    // LEAK instrument and nothing more — an allocation made and collected every
    // iteration leaves it flat — so the COUNT is the gate.
    //
    // The negative control, SOAK_SABOTAGE=1, plants a matched allocation per
    // iteration and requires the count to go red while the loop keeps loading
    // the same bytes.
    static void FailSoak(string what)
    {
        Console.WriteLine("SOAK FAILED: " + what);
        Environment.Exit(1);
    }

    static void RunSoak(int seconds)
    {
        byte[] buffer = new byte[1 << 20];

        Demo.RootConfig root = new Demo.RootConfig();
        Demo.TableReport rootReport = new Demo.TableReport();
        byte[] rootFull = ReadGolden("root_full");
        Demo.ProfileConfig profile = new Demo.ProfileConfig();
        Demo.TableReport profileReport = new Demo.TableReport();
        byte[] profileElide = ReadGolden("profile_elide");
        Demo.LoadoutConfig loadout = new Demo.LoadoutConfig();
        Demo.TableReport loadoutReport = new Demo.TableReport();
        byte[] loadoutFull = ReadGolden("loadout_full");
        Demo.WideBlob blob = new Demo.WideBlob();
        Demo.TableReport blobReport = new Demo.TableReport();
        byte[] wideBlob = ReadGolden("wide_blob");
        V1.Cfg v1 = new V1.Cfg();
        V1.TableReport v1Report = new V1.TableReport();
        byte[] v1Cfg = ReadGolden("v1_cfg");
        V2.Cfg v2 = new V2.Cfg();
        V2.TableReport v2Report = new V2.TableReport();
        byte[] v2Cfg = ReadGolden("v2_cfg");

        // one pass: each value and report is the CALLER's, allocated once above,
        // because a soak that allocated per iteration would measure its harness
        Action pass = () =>
        {
            if (!Demo.Schema.RootConfigLoad(root, rootFull, rootReport)) { FailSoak("root_full load"); }
            if (Demo.Schema.RootConfigSave(root, buffer) != Demo.Schema.RootConfigMeasure(root)) { FailSoak("root_full save"); }
            if (!Demo.Schema.ProfileConfigLoad(profile, profileElide, profileReport)) { FailSoak("profile_elide load"); }
            if (Demo.Schema.ProfileConfigSave(profile, buffer) != Demo.Schema.ProfileConfigMeasure(profile)) { FailSoak("profile_elide save"); }
            if (!Demo.Schema.LoadoutConfigLoad(loadout, loadoutFull, loadoutReport)) { FailSoak("loadout_full load"); }
            if (Demo.Schema.LoadoutConfigSave(loadout, buffer) != Demo.Schema.LoadoutConfigMeasure(loadout)) { FailSoak("loadout_full save"); }
            if (!Demo.Schema.WideBlobLoad(blob, wideBlob, blobReport)) { FailSoak("wide_blob load"); }
            if (Demo.Schema.WideBlobSave(blob, buffer) != Demo.Schema.WideBlobMeasure(blob)) { FailSoak("wide_blob save"); }
            if (!V1.Schema.CfgLoad(v1, v1Cfg, v1Report)) { FailSoak("v1_cfg load"); }
            if (V1.Schema.CfgSave(v1, buffer) != V1.Schema.CfgMeasure(v1)) { FailSoak("v1_cfg save"); }
            if (!V2.Schema.CfgLoad(v2, v2Cfg, v2Report)) { FailSoak("v2_cfg load"); }
            if (V2.Schema.CfgSave(v2, buffer) != V2.Schema.CfgMeasure(v2)) { FailSoak("v2_cfg save"); }
        };
        pass(); // warm pass, charged to the setup

        bool sabotage = Environment.GetEnvironmentVariable("SOAK_SABOTAGE") == "1";
        Stopwatch watch = Stopwatch.StartNew();
        long before = GC.GetAllocatedBytesForCurrentThread();
        long iterations = 0;
        while (watch.Elapsed.TotalSeconds < seconds)
        {
            if (sabotage)
            {
                // NEGATIVE CONTROL: a matched allocation/free per iteration, so
                // the live heap returns to where it was and only the COUNT moves.
                GC.KeepAlive(new byte[64]);
            }
            pass();
            iterations++;
        }
        long grew = GC.GetAllocatedBytesForCurrentThread() - before;

        if (sabotage)
        {
            if (grew == 0) { FailSoak("the planted allocation did not move the counter"); }
            Console.WriteLine("cs tables soak: the planted allocation moved the counter by " + grew + " byte(s) — the gate fires");
            return;
        }
        if (grew != 0)
        {
            FailSoak("the read, measure and save paths allocated " + grew + " byte(s) over " + iterations +
                " iteration(s) — they allocate NOTHING, and a live-heap sample cannot see a matched pair");
        }
        Console.WriteLine("cs tables soak: ZERO bytes allocated over " + iterations +
            " iteration(s) — the read, measure and save paths allocate nothing");
    }
}
