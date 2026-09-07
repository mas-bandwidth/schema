// The C# TABLE-wire conformance test (docs/SPEC-TABLES.md) — the twin of
// test/tables/main.cpp's FIXED-class half. Three generated units in one
// assembly: the tables corpus (tabledemo), and the two-generation evolution
// pair (tblv1/tblv2) whose schemas disagree on purpose.
//
// The load-bearing gate is the SHARED GOLDEN WIRE: C++ pins the encoding of a
// set of named instances into testdata/wire/tables/*.bin, and this program
// builds THE SAME instances value for value, byte-compares its own Save
// against those files, and loads the C++-written bytes back. Cross-language
// bit identity on the table wire is the property this file exists to prove.
//
// Prints OK and exits 0, exactly like its C++ twin.

using System;
using System.IO;
using System.Text;
using Demo = Tabledemo;
using V1 = Tblv1;
using V2 = Tblv2;
using P1 = Tblp1;
using P3 = Tblp3;

static partial class Program
{
    static bool failed;
    static string goldenDir;

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed = true;
        }
    }

    // Independently written from SPEC-TABLES section 3: full FNV-1a identity,
    // canonical unsigned LEB128 and a vocabulary at the end of the file.
    static ulong FieldId(string name)
    {
        ulong h = 0xcbf29ce484222325ul;
        foreach (byte c in Encoding.UTF8.GetBytes(name)) { h = unchecked((h ^ c) * 0x100000001b3ul); }
        return h;
    }
    static byte[] Join(params byte[][] pieces)
    {
        using MemoryStream into = new MemoryStream();
        foreach (byte[] piece in pieces) { into.Write(piece); }
        return into.ToArray();
    }
    static byte[] Var(ulong v)
    {
        using MemoryStream into = new MemoryStream();
        while (v >= 128) { into.WriteByte((byte)(v | 128)); v >>= 7; }
        into.WriteByte((byte)v); return into.ToArray();
    }
    static byte[] U32(uint v) { byte[] bytes = new byte[4]; Le32(bytes, 0, v); return bytes; }
    static byte[] Fixture(byte[] body, params string[] names)
    {
        using MemoryStream into = new MemoryStream();
        into.WriteByte(1); into.Write(body);
        foreach (string name in names)
        {
            ulong id = FieldId(name);
            for (int i = 0; i < 8; i++) { into.WriteByte((byte)id); id >>= 8; }
        }
        ulong count = (ulong)names.Length;
        for (int i = 0; i < 8; i++) { into.WriteByte((byte)count); count >>= 8; }
        return into.ToArray();
    }

    static string FindGoldenDir()
    {
        string[] candidates =
        {
            Path.Combine("..", "..", "testdata", "wire", "tables"),
            Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "..", "testdata", "wire", "tables"),
        };
        foreach (string candidate in candidates)
        {
            if (Directory.Exists(candidate))
            {
                return candidate;
            }
        }
        return candidates[0];
    }

    static byte[] ReadGolden(string name)
    {
        try
        {
            return File.ReadAllBytes(Path.Combine(goldenDir, name + ".bin"));
        }
        catch (Exception e)
        {
            Console.WriteLine("FAILED: read table wire golden " + name + ": " + e.Message);
            failed = true;
            return new byte[0];
        }
    }

    // GoldenWire byte-compares this backend's Save against the C++-pinned bytes.
    static void GoldenWire(string name, ReadOnlySpan<byte> data)
    {
        byte[] golden = ReadGolden(name);
        Check(data.SequenceEqual(golden),
            "table wire golden " + name + " — C# bytes must equal the C++-pinned bytes (" +
            data.Length + " written, " + golden.Length + " pinned)");
    }

    static void SetString(byte[] dest, ref int length, string s)
    {
        byte[] raw = Encoding.ASCII.GetBytes(s);
        raw.CopyTo(dest, 0);
        length = raw.Length;
    }

    static string GetString(byte[] src, int length)
    {
        return Encoding.ASCII.GetString(src, 0, length);
    }

    static void Le16(byte[] p, int at, ushort v)
    {
        p[at] = (byte)v;
        p[at + 1] = (byte)(v >> 8);
    }

    static void Le32(byte[] p, int at, uint v)
    {
        p[at] = (byte)v;
        p[at + 1] = (byte)(v >> 8);
        p[at + 2] = (byte)(v >> 16);
        p[at + 3] = (byte)(v >> 24);
    }

    static Demo.TableFieldInfo DemoField(Demo.TableTypeInfo type, string name)
    {
        foreach (Demo.TableFieldInfo f in type.Fields)
        {
            if (f.Name == name) { return f; }
        }
        return null;
    }

    static V1.TableFieldInfo V1Field(V1.TableTypeInfo type, string name)
    {
        foreach (V1.TableFieldInfo f in type.Fields)
        {
            if (f.Name == name) { return f; }
        }
        return null;
    }

    // ---- the pinned instances, mirroring test/tables/main.cpp value for value ----

    static void BuildGoldenRoot(Demo.RootConfig root)
    {
        SetString(root.VersionNote, ref root.VersionNoteLength, "golden-v1");

        root.WeaponsCount = 2;
        root.Weapons[0].Damage = 40.5f;
        root.Weapons[0].Speed = 250.0f;
        root.Weapons[0].Penetration = 7;
        root.Weapons[0].Channel = 45;
        root.Weapons[0].Homing = true;
        root.Weapons[0].Effect.Type = Demo.EffectType.Buff;
        root.Weapons[0].Effect.Buff.Multiplier = 3.25f;
        root.Weapons[1].Effect.Type = Demo.EffectType.Debuff;
        root.Weapons[1].Effect.Debuff.Amount = 42;

        root.ProfilesCount = 1;
        Demo.ProfileConfig p = root.Profiles[0];
        SetString(p.Name, ref p.NameLength, "player one");
        p.Icon[0] = 1; p.Icon[1] = 2; p.Icon[2] = 250; p.IconLength = 3;
        p.Experience = 777;
        p.Tilt = -12;
        p.Heading = -30000;
        p.Timestamp = -5000000000L;
        p.Badge = 200;
        p.Port = 40000;
        p.Epoch = 0x1122334455667788ul;
        p.Precision = 2.5;
        p.Ratings[2] = 0.5f;
        p.HasLoadout = true;
        p.Loadout.Grade = Demo.Grade.Gold;
        p.Loadout.GradesCount = 2;
        p.Loadout.Grades[0] = Demo.Grade.Bronze;
        p.Loadout.Grades[1] = Demo.Grade.Gold;
        p.Loadout.Podium[0] = Demo.Grade.Gold;
        p.Loadout.Podium[2] = Demo.Grade.Silver;
        p.Loadout.Perks = Demo.Schema.PerksCloaked | Demo.Schema.PerksTurbo;
        p.Loadout.Primary.Penetration = 7;
        p.Loadout.Backups[0].Damage = 1.0f;
        p.Loadout.AttachmentsCount = 2;
        p.Loadout.Attachments[0].Slot = 3;
        p.Loadout.Attachments[0].Power = 2.0f;
        p.Loadout.Attachments[1].Slot = 5;
    }

    static void BuildGoldenLoadout(Demo.LoadoutConfig loadout)
    {
        loadout.Grade = Demo.Grade.Bronze;
        loadout.GradesCount = 3;
        loadout.Grades[0] = Demo.Grade.Gold;
        loadout.Grades[1] = Demo.Grade.Silver;
        loadout.Grades[2] = Demo.Grade.Bronze;
        loadout.Podium[1] = Demo.Grade.Bronze;
        loadout.Perks = Demo.Schema.PerksShielded;
        loadout.Primary.Damage = 12.5f;
        loadout.Primary.Homing = true;
        loadout.Primary.Effect.Type = Demo.EffectType.Buff;
        loadout.Primary.Effect.Buff.Multiplier = 0.5f;
        loadout.Backups[1].Channel = 63;
        loadout.AttachmentsCount = 1;
        loadout.Attachments[0].Slot = 6;
        loadout.Attachments[0].Power = 0.25f;
    }

    static void BuildGoldenWide(Demo.WideBlob blob)
    {
        blob.LabelLength = 70000;
        for (int i = 0; i < 70000; i++) { blob.Label[i] = (byte)('a' + (i % 26)); }
        blob.PayloadLength = 100;
        for (int i = 0; i < 100; i++) { blob.Payload[i] = (byte)(i * 7 + 3); }
        blob.SamplesCount = 70000;
        for (int i = 0; i < 70000; i++) { blob.Samples[i] = (ushort)(i * 37 + 11); }
    }

    static void BuildGoldenV1(V1.Cfg cfg)
    {
        cfg.A = 9;
        cfg.B = 8.5f;
        cfg.Mode = V1.Mode.Alpha;
        SetString(cfg.Name, ref cfg.NameLength, "aged");
        cfg.Inner.Factor = 1.25f;
        cfg.ItemsCount = 3;
        cfg.Items[0] = 1; cfg.Items[1] = 20; cfg.Items[2] = 255;
        cfg.Grade = V1.Grade.Gold;
        cfg.GradesCount = 2;
        cfg.Grades[0] = V1.Grade.Gold;
        cfg.Grades[1] = V1.Grade.Bronze;
        cfg.Podium[0] = V1.Grade.Bronze;
        cfg.Podium[2] = V1.Grade.Gold;
        cfg.SlotsCount = 4;
        cfg.Slots[0] = 11; cfg.Slots[1] = 22; cfg.Slots[2] = 33; cfg.Slots[3] = 44;
        cfg.Tally[0] = 5; cfg.Tally[2] = 7;
        cfg.Effect.Type = V1.EffectType.Ward;
        cfg.Effect.Ward.Charge = 0.75f;
    }

    static void BuildGoldenV2(V2.Cfg cfg)
    {
        cfg.A = 7.5f;
        cfg.C = false;
        cfg.Mode = V2.Mode.Alpha;
        SetString(cfg.Title, ref cfg.TitleLength, "fresh");
        cfg.Inner.Factor = 9.5f;
        cfg.Inner.Gain = 4.0f;
        cfg.ItemsCount = 2;
        cfg.Items[0] = 10; cfg.Items[1] = 200;
        cfg.Grade = V2.Grade.Gold;
        cfg.GradesCount = 3;
        cfg.Grades[0] = V2.Grade.Silver;
        cfg.Grades[1] = V2.Grade.Gold;
        cfg.Grades[2] = V2.Grade.Bronze;
        cfg.Podium[1] = V2.Grade.Silver;
        cfg.SlotsCount = 3;
        cfg.Slots[0] = 7; cfg.Slots[1] = 8; cfg.Slots[2] = 9;
        cfg.Tally[1] = 3; cfg.Tally[3] = 9;
        cfg.Effect.Type = V2.EffectType.Hex;
        cfg.Effect.Hex.Level = 6;
    }

    // ---- gate 1: the shared golden wire, written from C# ----

    static void TestGoldenWireWrite()
    {
        byte[] buffer = new byte[1 << 20];

        Demo.RootConfig root = new Demo.RootConfig();
        BuildGoldenRoot(root);
        long need = Demo.Schema.RootConfigMeasure(root);
        long wrote = Demo.Schema.RootConfigSave(root, buffer);
        Check(wrote > 0 && wrote == need, "root_full: measure == save");
        GoldenWire("root_full", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        Demo.RootConfig empty = new Demo.RootConfig();
        wrote = Demo.Schema.RootConfigSave(empty, buffer);
        Check(wrote == 10, "root_default: everything elides to the form, terminator and empty vocabulary");
        GoldenWire("root_default", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        // the ELISION shape: non-default fields around an all-default nested
        // table inside a taken guard. Its bytes are what pin the "all-default
        // nested elides" decision across languages.
        Demo.ProfileConfig profile = new Demo.ProfileConfig();
        profile.Experience = 1;
        profile.HasLoadout = true; // loadout itself stays all-default: elides
        wrote = Demo.Schema.ProfileConfigSave(profile, buffer);
        Check(wrote > 0 && wrote == Demo.Schema.ProfileConfigMeasure(profile), "profile_elide: measure == save");
        GoldenWire("profile_elide", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        Demo.LoadoutConfig loadout = new Demo.LoadoutConfig();
        BuildGoldenLoadout(loadout);
        wrote = Demo.Schema.LoadoutConfigSave(loadout, buffer);
        Check(wrote > 0 && wrote == Demo.Schema.LoadoutConfigMeasure(loadout), "loadout_full: measure == save");
        GoldenWire("loadout_full", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        Demo.WideBlob blob = new Demo.WideBlob();
        BuildGoldenWide(blob);
        wrote = Demo.Schema.WideBlobSave(blob, buffer);
        Check(wrote > 0 && wrote == Demo.Schema.WideBlobMeasure(blob), "wide_blob: measure == save");
        GoldenWire("wide_blob", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        Demo.ArchiveConfig archive = new Demo.ArchiveConfig();
        BuildGoldenRoot(archive.Root);
        archive.Count = 5;
        wrote = Demo.Schema.ArchiveConfigSave(archive, buffer);
        Check(wrote > 0 && wrote == Demo.Schema.ArchiveConfigMeasure(archive), "archive: measure == save");
        GoldenWire("archive", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        V1.Cfg v1 = new V1.Cfg();
        BuildGoldenV1(v1);
        wrote = V1.Schema.CfgSave(v1, buffer);
        Check(wrote > 0 && wrote == V1.Schema.CfgMeasure(v1), "v1_cfg: measure == save");
        GoldenWire("v1_cfg", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        V2.Cfg v2 = new V2.Cfg();
        BuildGoldenV2(v2);
        wrote = V2.Schema.CfgSave(v2, buffer);
        Check(wrote > 0 && wrote == V2.Schema.CfgMeasure(v2), "v2_cfg: measure == save");
        GoldenWire("v2_cfg", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));
    }

    // ---- gate 2: LOAD the C++-written bytes ----

    static void TestGoldenWireRead()
    {
        {
            byte[] golden = ReadGolden("root_full");
            Demo.TableReport report = new Demo.TableReport();
            Demo.RootConfig root = new Demo.RootConfig();
            Check(Demo.Schema.RootConfigLoad(root, golden, report), "root_full loads");
            Check(!report.Malformed && report.Unknown == 0 && report.KindMismatch == 0 && report.Clamped == 0,
                "root_full: silence — the data matched this reader's schema exactly");
            Check(GetString(root.VersionNote, root.VersionNoteLength) == "golden-v1", "root_full version_note");
            Check(root.WeaponsCount == 2, "root_full weapons count");
            Check(root.Weapons[0].Damage == 40.5f && root.Weapons[0].Channel == 45 && root.Weapons[0].Homing,
                "root_full weapon 0 scalars");
            Check(root.Weapons[0].Effect.Type == Demo.EffectType.Buff &&
                  root.Weapons[0].Effect.Buff.Multiplier == 3.25f, "root_full weapon 0 union arm");
            Check(root.Weapons[1].Effect.Type == Demo.EffectType.Debuff &&
                  root.Weapons[1].Effect.Debuff.Amount == 42, "root_full weapon 1 union arm");
            Demo.ProfileConfig p = root.Profiles[0];
            Check(root.ProfilesCount == 1 && GetString(p.Name, p.NameLength) == "player one", "root_full profile name");
            Check(p.IconLength == 3 && p.Icon[2] == 250, "root_full profile icon");
            Check(p.Experience == 777 && p.Tilt == -12 && p.Heading == -30000, "root_full profile ints");
            Check(p.Timestamp == -5000000000L && p.Badge == 200 && p.Port == 40000, "root_full profile ints 2");
            Check(p.Epoch == 0x1122334455667788ul && p.Precision == 2.5, "root_full profile u64/f64");
            Check(p.Ratings[2] == 0.5f && p.HasLoadout, "root_full profile ratings/guard");
            Check(p.Loadout.Grade == Demo.Grade.Gold, "root_full loadout grade");
            Check(p.Loadout.GradesCount == 2 && p.Loadout.Grades[1] == Demo.Grade.Gold, "root_full loadout grades");
            Check(p.Loadout.Podium[0] == Demo.Grade.Gold && p.Loadout.Podium[2] == Demo.Grade.Silver,
                "root_full loadout podium");
            Check(p.Loadout.Perks == (Demo.Schema.PerksCloaked | Demo.Schema.PerksTurbo), "root_full loadout perks");
            Check(p.Loadout.AttachmentsCount == 2 && p.Loadout.Attachments[1].Slot == 5, "root_full attachments");
        }
        {
            byte[] golden = ReadGolden("root_default");
            Demo.TableReport report = new Demo.TableReport();
            Demo.RootConfig root = new Demo.RootConfig();
            root.WeaponsCount = 3; // junk the reset must erase
            Check(Demo.Schema.RootConfigLoad(root, golden, report), "root_default loads");
            Check(root.WeaponsCount == 0 && root.VersionNoteLength == 0, "root_default: defaults restored");
        }
        {
            byte[] golden = ReadGolden("profile_elide");
            Demo.TableReport report = new Demo.TableReport();
            Demo.ProfileConfig profile = new Demo.ProfileConfig();
            profile.Loadout.Grade = Demo.Grade.Gold; // junk the reset must erase
            Check(Demo.Schema.ProfileConfigLoad(profile, golden, report), "profile_elide loads");
            Check(!report.Malformed && report.Unknown == 0, "profile_elide: silence");
            Check(profile.Experience == 1 && profile.HasLoadout, "profile_elide fields");
            Check(profile.Loadout.Grade == Demo.Grade.Silver, "profile_elide: the elided nesting reads its declared defaults");
        }
        {
            byte[] golden = ReadGolden("loadout_full");
            Demo.TableReport report = new Demo.TableReport();
            Demo.LoadoutConfig loadout = new Demo.LoadoutConfig();
            Check(Demo.Schema.LoadoutConfigLoad(loadout, golden, report), "loadout_full loads");
            Check(!report.Malformed && report.Unknown == 0, "loadout_full: silence");
            Check(loadout.Grade == Demo.Grade.Bronze && loadout.GradesCount == 3, "loadout_full grade/grades");
            Check(loadout.Primary.Effect.Type == Demo.EffectType.Buff &&
                  loadout.Primary.Effect.Buff.Multiplier == 0.5f, "loadout_full nested union");
            Check(loadout.Backups[1].Channel == 63, "loadout_full fixed array of tables");
            Check(loadout.Attachments[0].Power == 0.25f, "loadout_full attachments");
        }
        {
            byte[] golden = ReadGolden("wide_blob");
            Demo.TableReport report = new Demo.TableReport();
            Demo.WideBlob blob = new Demo.WideBlob();
            Check(Demo.Schema.WideBlobLoad(blob, golden, report), "wide_blob loads");
            Check(!report.Malformed && report.Clamped == 0, "wide_blob: silence");
            Check(blob.LabelLength == 70000 && blob.Label[69999] == (byte)('a' + (69999 % 26)), "wide_blob label");
            Check(blob.PayloadLength == 100 && blob.Payload[99] == unchecked((byte)(99 * 7 + 3)), "wide_blob payload");
            Check(blob.SamplesCount == 70000 && blob.Samples[69999] == unchecked((ushort)(69999 * 37 + 11)), "wide_blob samples");
        }
        {
            byte[] golden = ReadGolden("archive");
            Demo.TableReport report = new Demo.TableReport();
            Demo.ArchiveConfig archive = new Demo.ArchiveConfig();
            Check(Demo.Schema.ArchiveConfigLoad(archive, golden, report), "archive loads");
            Check(archive.Count == 5 && GetString(archive.Root.VersionNote, archive.Root.VersionNoteLength) == "golden-v1",
                "archive cross-file nesting");
        }
        {
            byte[] golden = ReadGolden("v1_cfg");
            V1.TableReport report = new V1.TableReport();
            V1.Cfg cfg = new V1.Cfg();
            Check(V1.Schema.CfgLoad(cfg, golden, report), "v1_cfg loads");
            Check(!report.Malformed && report.Unknown == 0 && report.Clamped == 0, "v1_cfg: silence");
            Check(cfg.A == 9 && cfg.B == 8.5f && cfg.Mode == V1.Mode.Alpha, "v1_cfg scalars");
            Check(GetString(cfg.Name, cfg.NameLength) == "aged", "v1_cfg name");
            Check(cfg.ItemsCount == 3 && cfg.Items[2] == 255, "v1_cfg items");
            Check(cfg.Effect.Type == V1.EffectType.Ward && cfg.Effect.Ward.Charge == 0.75f, "v1_cfg union");
            Check(cfg.Tally[0] == 5 && cfg.Tally[2] == 7, "v1_cfg tally");
        }
        {
            byte[] golden = ReadGolden("v2_cfg");
            V2.TableReport report = new V2.TableReport();
            V2.Cfg cfg = new V2.Cfg();
            Check(V2.Schema.CfgLoad(cfg, golden, report), "v2_cfg loads");
            Check(!report.Malformed && report.Unknown == 0 && report.Clamped == 0, "v2_cfg: silence");
            Check(cfg.A == 7.5f && !cfg.C && cfg.Mode == V2.Mode.Alpha, "v2_cfg scalars");
            Check(GetString(cfg.Title, cfg.TitleLength) == "fresh", "v2_cfg title");
            Check(cfg.Inner.Factor == 9.5f && cfg.Inner.Gain == 4.0f, "v2_cfg inner");
            Check(cfg.Effect.Type == V2.EffectType.Hex && cfg.Effect.Hex.Level == 6, "v2_cfg union");
        }
    }

    // ---- exact capacity: measure's answer IS the buffer size ----

    static void CheckExactCapacityV1(V1.Cfg value)
    {
        byte[] roomy = new byte[65536];
        byte[] exact;
        long need = V1.Schema.CfgMeasure(value);
        Check(need >= 2, "exact capacity: measure >= 2");
        Check(V1.Schema.CfgSave(value, roomy) == need, "exact capacity: roomy save == measure");
        exact = new byte[need];
        Check(V1.Schema.CfgSave(value, exact) == need, "exact capacity: exact save == measure");
        Check(new ReadOnlySpan<byte>(roomy, 0, (int)need).SequenceEqual(exact), "exact capacity: same bytes");
        if (need > 2)
        {
            Check(V1.Schema.CfgSave(value, new byte[need - 1]) == -1, "exact capacity: one byte short refuses");
        }
    }

    static void TestExactCapacity()
    {
        // the repro shape: non-default fields around an ALL-DEFAULT nested
        // table — the elided field must not touch the buffer at all
        V1.Cfg cfg = new V1.Cfg();
        cfg.A = 9;
        cfg.B = 8.5f;
        SetString(cfg.Name, ref cfg.NameLength, "exact");
        CheckExactCapacityV1(cfg);

        CheckExactCapacityV1(new V1.Cfg()); // all-default: 2 bytes

        // an elided nested table inside a GUARDED group
        Demo.ProfileConfig profile = new Demo.ProfileConfig();
        profile.Experience = 1;
        profile.HasLoadout = true; // loadout itself stays all-default: elides
        long need = Demo.Schema.ProfileConfigMeasure(profile);
        byte[] exact = new byte[need];
        Check(Demo.Schema.ProfileConfigSave(profile, exact) == need, "profile: exact capacity");
        Check(Demo.Schema.ProfileConfigSave(profile, new byte[need - 1]) == -1, "profile: one byte short refuses");

        // nested tables of nested tables, some elided, some riding
        Demo.ArchiveConfig archive = new Demo.ArchiveConfig();
        archive.Count = 9;
        archive.Root.WeaponsCount = 1;
        need = Demo.Schema.ArchiveConfigMeasure(archive);
        Check(Demo.Schema.ArchiveConfigSave(archive, new byte[need]) == need, "archive: exact capacity");

        // loadout: fixed array of tables (always rides) around all-default elements
        Demo.LoadoutConfig loadout = new Demo.LoadoutConfig();
        loadout.Grade = Demo.Grade.Gold;
        need = Demo.Schema.LoadoutConfigMeasure(loadout);
        Check(Demo.Schema.LoadoutConfigSave(loadout, new byte[need]) == need, "loadout: exact capacity");

        // enum ARRAYS, both shapes, each riding u16 variant hashes per element
        Demo.LoadoutConfig enums = new Demo.LoadoutConfig();
        enums.GradesCount = 3;
        enums.Grades[0] = Demo.Grade.Bronze;
        enums.Grades[1] = Demo.Grade.None; // None is a legal element: it rides as 0
        enums.Grades[2] = Demo.Grade.Gold;
        enums.Podium[0] = Demo.Grade.Gold;
        enums.Podium[1] = Demo.Grade.Silver;
        enums.Podium[2] = Demo.Grade.Bronze;
        need = Demo.Schema.LoadoutConfigMeasure(enums);
        Check(Demo.Schema.LoadoutConfigSave(enums, new byte[need]) == need, "enum arrays: exact capacity");
    }

    // ---- storage invariants: the write side validates what it reads ----

    static void TestStorageInvariants()
    {
        byte[] buffer = new byte[4096];

        Demo.RootConfig root = new Demo.RootConfig();
        root.WeaponsCount = 9; // bound is 8
        Check(Demo.Schema.RootConfigMeasure(root) == -1, "count above bound: measure refuses");
        Check(Demo.Schema.RootConfigSave(root, buffer) == -1, "count above bound: save refuses");

        V1.Cfg cfg = new V1.Cfg();
        cfg.NameLength = -1;
        Check(V1.Schema.CfgMeasure(cfg) == -1, "negative length: measure refuses");
        Check(V1.Schema.CfgSave(cfg, buffer) == -1, "negative length: save refuses");

        V1.Cfg cfg2 = new V1.Cfg();
        cfg2.ItemsCount = -3;
        Check(V1.Schema.CfgMeasure(cfg2) == -1, "negative count: measure refuses");
        Check(V1.Schema.CfgSave(cfg2, buffer) == -1, "negative count: save refuses");

        // a violation deep inside a nested, guarded table propagates up
        Demo.ProfileConfig profile = new Demo.ProfileConfig();
        profile.HasLoadout = true;
        profile.Loadout.AttachmentsCount = -5;
        Check(Demo.Schema.ProfileConfigMeasure(profile) == -1, "nested violation: measure refuses");
        Check(Demo.Schema.ProfileConfigSave(profile, buffer) == -1, "nested violation: save refuses");

        // an out-of-range union tag refuses in measure exactly as in write
        Demo.WeaponConfig weapon = new Demo.WeaponConfig();
        weapon.Effect.Type = (Demo.EffectType)9;
        Check(Demo.Schema.WeaponConfigMeasure(weapon) == -1, "bad union tag: measure refuses");
        Check(Demo.Schema.WeaponConfigSave(weapon, buffer) == -1, "bad union tag: save refuses");
    }

    // ---- bounded elements: a count the body length cannot cover never reads
    // ---- the following fields' bytes (skipped, NEVER misdecoded)

    static void TestBoundedElements()
    {
        foreach (bool prefix in new[] { false, true })
        {
            byte[] payload = prefix ? Join(new byte[] { 4, 2 }, U32(10), new byte[] { 0, 0 }) : new byte[] { 4, 2 };
            byte[] wire = Fixture(Join(new byte[] { 1, 14 }, Var((ulong)payload.Length), payload,
                new byte[] { 2, 4 }, U32(42), new byte[] { 0 }), "items", "a");
            V1.Cfg value = new V1.Cfg(); V1.TableReport report = new V1.TableReport();
            Check(V1.Schema.CfgLoad(value, wire, report), "bounded elements: parent reaches terminator");
            Check(report.Malformed && value.A == 42, "bounded elements: local damage preserves sibling");
            Check(value.ItemsCount == (prefix ? 1 : 0), "bounded elements: decoded prefix only");
            if (prefix) { Check(value.Items[0] == 10, "bounded elements: prefix value"); }
        }
    }

    // ---- all-default: everything elides, decode restores every default ----

    static void TestAllDefault()
    {
        Demo.WeaponConfig weapon = new Demo.WeaponConfig();
        byte[] buffer = new byte[64];
        long wrote = Demo.Schema.WeaponConfigSave(weapon, buffer);
        Check(wrote == 10, "all-default: form, terminator and empty vocabulary");
        Check(Demo.Schema.WeaponConfigMeasure(weapon) == 10, "all-default: measure == 10");

        Demo.TableReport report = new Demo.TableReport();
        Demo.WeaponConfig outWeapon = new Demo.WeaponConfig();
        outWeapon.Damage = -1.0f; // garbage the reset must erase
        Check(Demo.Schema.WeaponConfigLoad(outWeapon, new ReadOnlySpan<byte>(buffer, 0, (int)wrote), report),
            "all-default: load");
        Check(outWeapon.Damage == 21.0f && outWeapon.Speed == 500.0f && outWeapon.Penetration == 1,
            "all-default: declared defaults restored");
        Check(!report.Malformed && report.Unknown == 0, "all-default: silence");
    }

    // ---- guarded fields stay off the wire when the guard says so ----

    static void TestGuard()
    {
        Demo.ProfileConfig p = new Demo.ProfileConfig();
        p.HasLoadout = false;
        p.Loadout.Grade = Demo.Grade.Gold; // junk behind an untaken guard

        byte[] buffer = new byte[512];
        long wrote = Demo.Schema.ProfileConfigSave(p, buffer);
        Check(wrote == 10, "guard: guard false + everything else default: all elides");
        Check(Demo.Schema.ProfileConfigMeasure(p) == wrote, "guard: measure == save");

        Demo.TableReport report = new Demo.TableReport();
        Demo.ProfileConfig outProfile = new Demo.ProfileConfig();
        Check(Demo.Schema.ProfileConfigLoad(outProfile, new ReadOnlySpan<byte>(buffer, 0, (int)wrote), report),
            "guard: load");
        Check(outProfile.Loadout.Grade == Demo.Grade.Silver, "guard: untaken side decodes to defaults");
    }

    // ---- evolution, both directions (any reader x any data) ----

    static void TestEvolutionOldReaderNewData()
    {
        V2.Cfg v2 = new V2.Cfg();
        v2.A = 7.5f;
        v2.C = false;
        v2.Mode = V2.Mode.Alpha;
        SetString(v2.Title, ref v2.TitleLength, "fresh");
        v2.Inner.Factor = 9.5f;
        v2.Inner.Gain = 4.0f;
        v2.Items[0] = 10;
        v2.ItemsCount = 1;

        byte[] wire = new byte[1024];
        long bytes = V2.Schema.CfgSave(v2, wire);
        Check(bytes > 0 && bytes == V2.Schema.CfgMeasure(v2), "old reader: v2 saved");

        V1.TableReport report = new V1.TableReport();
        V1.Cfg outCfg = new V1.Cfg();
        Check(V1.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "old reader: load");
        Check(!report.Malformed, "old reader: not malformed");
        Check(report.Unknown == 2, "old reader: c (top level) + gain (nested) unknown");
        Check(report.KindMismatch == 1, "old reader: a is f32 on the wire, i32 here");
        Check(outCfg.A == 5, "old reader: a skipped -> v1 default, never misdecoded");
        Check(outCfg.B == 1.5f, "old reader: removed in v2 -> absent -> v1 default");
        Check(GetString(outCfg.Name, outCfg.NameLength) == "fresh", "old reader: was = \"name\" identity survived");
        Check(outCfg.Mode == V1.Mode.Alpha, "old reader: mode");
        Check(outCfg.Inner.Factor == 9.5f, "old reader: nested factor");
        Check(outCfg.ItemsCount == 1 && outCfg.Items[0] == 10, "old reader: items");
    }

    static void TestEvolutionNewReaderOldData()
    {
        V1.Cfg v1 = new V1.Cfg();
        v1.A = 9;
        v1.B = 8.5f;
        SetString(v1.Name, ref v1.NameLength, "aged");
        v1.Inner.Factor = 1.25f;

        byte[] wire = new byte[1024];
        long bytes = V1.Schema.CfgSave(v1, wire);
        Check(bytes > 0 && bytes == V1.Schema.CfgMeasure(v1), "new reader: v1 saved");

        V2.TableReport report = new V2.TableReport();
        V2.Cfg outCfg = new V2.Cfg();
        Check(V2.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "new reader: load");
        Check(!report.Malformed, "new reader: not malformed");
        Check(report.Unknown == 1, "new reader: b, removed in v2");
        Check(report.KindMismatch == 1, "new reader: a is i32 on the wire, f32 here");
        Check(outCfg.A == 5.0f, "new reader: v2 default");
        Check(outCfg.C, "new reader: added in v2, absent in old data -> default");
        Check(GetString(outCfg.Title, outCfg.TitleLength) == "aged", "new reader: old name data lands in the renamed field");
        Check(outCfg.Mode == V2.Mode.Beta, "new reader: mode default");
        Check(outCfg.Inner.Factor == 1.25f, "new reader: nested factor");
        Check(outCfg.Inner.Gain == 1.0f, "new reader: nested added field defaults");
    }

    // ---- a variant inserted IN THE MIDDLE: identity is the NAME, not the ordinal ----

    static void TestEvolutionEnumInsertOldData()
    {
        V1.Cfg v1 = new V1.Cfg();
        v1.Grade = V1.Grade.Gold; // ordinal 2 in V1, ordinal 3 in V2

        byte[] wire = new byte[1024];
        long bytes = V1.Schema.CfgSave(v1, wire);
        Check(bytes > 0 && bytes == V1.Schema.CfgMeasure(v1), "enum insert: v1 saved");

        V1.TableFieldInfo grade = V1Field(V1.Schema.CfgTableType(), "grade");
        Check(grade != null && grade.Kind == 30, "enum insert: kU16 for every enum, every width");
        Check(grade.VariantId != null && grade.VariantId(2) == FieldId("Gold"), "enum insert: the wire value is the NAME hash");

        V2.TableReport report = new V2.TableReport();
        V2.Cfg outCfg = new V2.Cfg();
        Check(V2.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "enum insert: load");
        Check(!report.Malformed && report.Unknown == 0 && report.Clamped == 0, "enum insert: silence");
        Check(outCfg.Grade == V2.Grade.Gold, "enum insert: Gold, NOT Silver (which holds ordinal 2 in V2)");

        V1.Cfg arrays = new V1.Cfg();
        arrays.GradesCount = 2;
        arrays.Grades[0] = V1.Grade.Gold;
        arrays.Grades[1] = V1.Grade.Bronze;
        arrays.Podium[0] = V1.Grade.Bronze;
        arrays.Podium[1] = V1.Grade.Gold;
        arrays.Podium[2] = V1.Grade.None;
        bytes = V1.Schema.CfgSave(arrays, wire);
        Check(bytes > 0 && bytes == V1.Schema.CfgMeasure(arrays), "enum insert arrays: saved");

        V2.TableReport report2 = new V2.TableReport();
        V2.Cfg out2 = new V2.Cfg();
        Check(V2.Schema.CfgLoad(out2, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report2), "enum insert arrays: load");
        Check(!report2.Malformed && report2.Unknown == 0 && report2.Clamped == 0, "enum insert arrays: silence");
        Check(out2.GradesCount == 2, "enum insert arrays: count");
        Check(out2.Grades[0] == V2.Grade.Gold && out2.Grades[1] == V2.Grade.Bronze, "enum insert arrays: counted elements");
        Check(out2.Podium[0] == V2.Grade.Bronze && out2.Podium[1] == V2.Grade.Gold && out2.Podium[2] == V2.Grade.None,
            "enum insert arrays: fixed elements");
    }

    static void TestEvolutionEnumInsertNewData()
    {
        V2.Cfg v2 = new V2.Cfg();
        v2.Grade = V2.Grade.Silver; // V1 has no name for it at all

        byte[] wire = new byte[1024];
        long bytes = V2.Schema.CfgSave(v2, wire);
        Check(bytes > 0, "enum insert new data: saved");

        V1.TableReport report = new V1.TableReport();
        V1.Cfg outCfg = new V1.Cfg();
        Check(V1.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "enum insert new data: load");
        Check(!report.Malformed, "enum insert new data: not malformed");
        Check(report.Unknown == 1, "enum insert new data: an id this reader cannot name");
        Check(outCfg.Grade == V1.Grade.None, "enum insert new data: None, never a neighbour's variant");

        V2.Cfg gold = new V2.Cfg();
        gold.Grade = V2.Grade.Gold; // ordinal 3 in V2, 2 in V1
        bytes = V2.Schema.CfgSave(gold, wire);
        V1.TableReport report2 = new V1.TableReport();
        V1.Cfg out2 = new V1.Cfg();
        Check(V1.Schema.CfgLoad(out2, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report2), "enum insert moved ordinal: load");
        Check(!report2.Malformed && report2.Unknown == 0, "enum insert moved ordinal: silence");
        Check(out2.Grade == V1.Grade.Gold, "enum insert moved ordinal: lands correctly");

        V2.Cfg arrays = new V2.Cfg();
        arrays.GradesCount = 2;
        arrays.Grades[0] = V2.Grade.Gold;   // V1 names it
        arrays.Grades[1] = V2.Grade.Silver; // V1 does not
        bytes = V2.Schema.CfgSave(arrays, wire);
        V1.TableReport report3 = new V1.TableReport();
        V1.Cfg out3 = new V1.Cfg();
        Check(V1.Schema.CfgLoad(out3, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report3), "enum insert array element: load");
        Check(!report3.Malformed && report3.Unknown == 1, "enum insert array element: one unknown");
        Check(out3.GradesCount == 2, "enum insert array element: count");
        Check(out3.Grades[0] == V1.Grade.Gold && out3.Grades[1] == V1.Grade.None,
            "enum insert array element: unnameable lands on None");
    }

    static void TestEvolutionUnionInsertOldData()
    {
        V1.Cfg v1 = new V1.Cfg();
        v1.Effect.Type = V1.EffectType.Ward; // tag 2 in V1, tag 3 in V2
        v1.Effect.Ward.Charge = 7.5f;

        byte[] wire = new byte[1024];
        long bytes = V1.Schema.CfgSave(v1, wire);
        Check(bytes > 0 && bytes == V1.Schema.CfgMeasure(v1), "union insert: v1 saved");

        V1.TableFieldInfo effect = V1Field(V1.Schema.CfgTableType(), "effect");
        Check(effect != null && effect.Kind == 15 && effect.EnumMax == 2, "union insert: descriptor");
        Check(effect.VariantId != null && effect.VariantId(2) == FieldId("ward"), "union insert: arm id is the NAME hash");
        Check(effect.EnumName != null && effect.EnumName(2) == "ward", "union insert: arm name");

        V2.TableReport report = new V2.TableReport();
        V2.Cfg outCfg = new V2.Cfg();
        Check(V2.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "union insert: load");
        Check(!report.Malformed && report.Unknown == 0, "union insert: silence");
        Check(outCfg.Effect.Type == V2.EffectType.Ward, "union insert: Ward, NOT hex (which holds tag 2 in V2)");
        Check(outCfg.Effect.Ward.Charge == 7.5f, "union insert: arm payload");
    }

    static void TestEvolutionUnionInsertNewData()
    {
        V2.Cfg v2 = new V2.Cfg();
        v2.Effect.Type = V2.EffectType.Hex; // V1 has no name for this arm
        v2.Effect.Hex.Level = 4;

        byte[] wire = new byte[1024];
        long bytes = V2.Schema.CfgSave(v2, wire);
        Check(bytes > 0, "union insert new data: saved");

        V1.TableReport report = new V1.TableReport();
        V1.Cfg outCfg = new V1.Cfg();
        Check(V1.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "union insert new data: load");
        Check(!report.Malformed, "union insert new data: not malformed");
        Check(report.Unknown == 1, "union insert new data: an arm id V1 cannot name");
        Check(outCfg.Effect.Type == V1.EffectType.None, "union insert new data: empty, never a neighbour's arm");

        V2.Cfg ward = new V2.Cfg();
        ward.Effect.Type = V2.EffectType.Ward; // tag 3 in V2, 2 in V1
        ward.Effect.Ward.Charge = -2.0f;
        bytes = V2.Schema.CfgSave(ward, wire);
        V1.TableReport report2 = new V1.TableReport();
        V1.Cfg out2 = new V1.Cfg();
        Check(V1.Schema.CfgLoad(out2, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report2), "union insert moved tag: load");
        Check(!report2.Malformed && report2.Unknown == 0, "union insert moved tag: silence");
        Check(out2.Effect.Type == V1.EffectType.Ward && out2.Effect.Ward.Charge == -2.0f,
            "union insert moved tag: lands correctly");
    }

    // ---- hostile: a REPEATED field id whose second occurrence names an arm or
    // ---- a variant this build cannot name ----

    static void TestRepeatedIdUnnameableVariant()
    {
        byte[] wire = Fixture(new byte[] {
            1,15,2,13,7,3,10,0,0,0,63,0, // ward { charge = 0.5 }
            4,30,5,                       // grade = Gold
            1,15,6,13,1,0,                // same union, an unknown arm
            4,30,6,0                      // same enum, an unknown variant
        }, "effect", "ward", "charge", "grade", "Gold", "unknown");
        V1.Cfg value = new V1.Cfg(); V1.TableReport report = new V1.TableReport();
        Check(V1.Schema.CfgLoad(value, wire, report), "repeated variant: load");
        Check(!report.Malformed && report.Unknown == 2, "repeated variant: two unknown names");
        Check(value.Effect.Type == V1.EffectType.None && value.Grade == V1.Grade.None,
            "repeated variant: last occurrence replaces earlier value");
    }

    // ---- an array's BOUND is not wire identity ----

    static void TestEvolutionArrayBounds()
    {
        V1.Cfg wide = new V1.Cfg();
        wide.SlotsCount = 6;                                     // v2's bound is 3
        for (int i = 0; i < 6; i++) { wide.Slots[i] = 100 + i; }
        for (int i = 0; i < 3; i++) { wide.Tally[i] = 10 + i; }  // v2's tally is [4]

        byte[] wire = new byte[1024];
        long bytes = V1.Schema.CfgSave(wide, wire);
        Check(bytes > 0 && bytes == V1.Schema.CfgMeasure(wide), "array bounds: v1 saved");

        V2.TableReport report = new V2.TableReport();
        V2.Cfg outCfg = new V2.Cfg();
        Check(V2.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "array bounds: load");
        Check(!report.Malformed, "array bounds: a shrunk bound is not framing damage");
        Check(report.Clamped == 1, "array bounds: slots — 6 offered, 3 kept");
        Check(outCfg.SlotsCount == 3, "array bounds: bounded prefix");
        Check(outCfg.Slots[0] == 100 && outCfg.Slots[1] == 101 && outCfg.Slots[2] == 102, "array bounds: prefix values");
        Check(outCfg.Tally[0] == 10 && outCfg.Tally[1] == 11 && outCfg.Tally[2] == 12, "array bounds: tally prefix");
        Check(outCfg.Tally[3] == 0, "array bounds: the grown tail defaults");

        V2.Cfg narrow = new V2.Cfg();
        narrow.SlotsCount = 3;
        for (int i = 0; i < 3; i++) { narrow.Slots[i] = 200 + i; }
        for (int i = 0; i < 4; i++) { narrow.Tally[i] = 20 + i; } // v1's tally is [3]

        bytes = V2.Schema.CfgSave(narrow, wire);
        Check(bytes > 0 && bytes == V2.Schema.CfgMeasure(narrow), "array bounds: v2 saved");

        V1.TableReport report2 = new V1.TableReport();
        V1.Cfg out2 = new V1.Cfg();
        Check(V1.Schema.CfgLoad(out2, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report2), "array bounds reverse: load");
        Check(!report2.Malformed, "array bounds reverse: not malformed");
        Check(report2.Clamped == 1, "array bounds reverse: tally — 4 offered, v1 keeps 3");
        Check(out2.SlotsCount == 3, "array bounds reverse: under v1's bound, no clamp");
        Check(out2.Slots[0] == 200 && out2.Slots[2] == 202, "array bounds reverse: values");
        Check(out2.Slots[3] == 0 && out2.Slots[5] == 0, "array bounds reverse: the unwritten tail");
        Check(out2.Tally[0] == 20 && out2.Tally[1] == 21 && out2.Tally[2] == 22, "array bounds reverse: tally prefix");
    }

    // ---- a value no variant names has no wire identity ----

    static void TestUnnameableEnumRefused()
    {
        byte[] buffer = new byte[256];
        V1.Cfg cfg = new V1.Cfg();
        cfg.Grade = (V1.Grade)9;
        Check(V1.Schema.CfgMeasure(cfg) == -1, "unnameable enum: measure refuses");
        Check(V1.Schema.CfgSave(cfg, buffer) == -1, "unnameable enum: save refuses");

        V1.Cfg counted = new V1.Cfg();
        counted.GradesCount = 2;
        counted.Grades[0] = V1.Grade.Gold;
        counted.Grades[1] = (V1.Grade)9;
        Check(V1.Schema.CfgMeasure(counted) == -1, "unnameable enum element: measure refuses");
        Check(V1.Schema.CfgSave(counted, buffer) == -1, "unnameable enum element: save refuses");
        counted.GradesCount = 1; // the bad element is now above the count
        Check(V1.Schema.CfgMeasure(counted) > 0, "unnameable enum element: only elements below the count are examined");

        V1.Cfg fixedArr = new V1.Cfg();
        fixedArr.Podium[2] = (V1.Grade)9;
        Check(V1.Schema.CfgMeasure(fixedArr) == -1, "unnameable enum fixed element: measure refuses");
        Check(V1.Schema.CfgSave(fixedArr, buffer) == -1, "unnameable enum fixed element: save refuses");
    }

    static void TestUnnameableEnumElementRead()
    {
        byte[] wire = Fixture(new byte[] { 1,14,5,30,3,2,3,4,0 }, "grades", "Gold", "unknown", "Bronze");
        V1.Cfg value = new V1.Cfg(); V1.TableReport report = new V1.TableReport();
        Check(V1.Schema.CfgLoad(value, wire, report), "enum elements: load");
        Check(!report.Malformed && report.Unknown == 1, "enum elements: one unknown");
        Check(value.GradesCount == 3 && value.Grades[0] == V1.Grade.Gold &&
            value.Grades[1] == V1.Grade.None && value.Grades[2] == V1.Grade.Bronze, "enum elements: named values survive");
    }

    // ---- FLAGS STAY POSITIONAL ----

    static void TestFlagsArePositional()
    {
        Demo.TableFieldInfo perks = DemoField(Demo.Schema.LoadoutConfigTableType(), "perks");
        Check(perks != null, "flags: descriptor found");
        Check(perks.Kind == 9, "flags: kU64 — the mask's raw storage");
        Check(perks.VariantId == null, "flags: no per-variant wire id exists to carry");
        // the bits DO have names, and the descriptor carries them: EnumMax is
        // the highest declared BIT INDEX and EnumName spells one bit
        // (docs/SPEC-TABLES.md §8). The missing VariantId beside a present
        // EnumName is what says "positional vocabulary" — a bit has a name,
        // never a wire id, and that is how a generic walk tells a flags field
        // from an enum one.
        Check(perks.EnumMax == 2, "flags: the highest declared bit index");
        Check(perks.EnumName != null, "flags: a bit-index -> name function");
        Check(perks.EnumName(0) == "Shielded", "flags: bit 0");
        Check(perks.EnumName(1) == "Cloaked", "flags: bit 1");
        Check(perks.EnumName(2) == "Turbo", "flags: bit 2");

        Demo.LoadoutConfig loadout = new Demo.LoadoutConfig();
        loadout.Perks = Demo.Schema.PerksCloaked; // bit 1
        byte[] buffer = new byte[1024];
        long wrote = Demo.Schema.LoadoutConfigSave(loadout, buffer);
        Check(wrote > 0, "flags: saved");

        byte[] expected = Fixture(Join(new byte[] { 1,9 }, new byte[] { 2,0,0,0,0,0,0,0 }, new byte[] { 2,14,6,13,2,1,0,1,0,0 }), "perks", "backups");
        Check(buffer.AsSpan(0, (int)wrote).SequenceEqual(expected), "flags: raw positional mask");
    }

    // ---- extents past 65535: canonical LEB128 lengths and counts ----

    static void TestWideExtents()
    {
        Demo.WideBlob blob = new Demo.WideBlob();
        blob.LabelLength = 70000;
        for (int i = 0; i < 70000; i++) { blob.Label[i] = (byte)'w'; }
        blob.PayloadLength = 70000;
        for (int i = 0; i < 70000; i++) { blob.Payload[i] = (byte)(i & 0xff); }
        blob.SamplesCount = 70000;
        for (int i = 0; i < 70000; i++) { blob.Samples[i] = (ushort)(i & 0xffff); }

        long need = Demo.Schema.WideBlobMeasure(blob);
        Check(need > 65535 * 3, "wide extents: past the old 16-bit ceiling");
        byte[] buffer = new byte[need];
        Check(Demo.Schema.WideBlobSave(blob, buffer) == need, "wide extents: exact capacity holds out here too");
        Check(Demo.Schema.WideBlobSave(blob, new byte[need - 1]) == -1, "wide extents: one byte short refuses");

        Demo.TableReport report = new Demo.TableReport();
        Demo.WideBlob outBlob = new Demo.WideBlob();
        Check(Demo.Schema.WideBlobLoad(outBlob, buffer, report), "wide extents: load");
        Check(!report.Malformed && report.Unknown == 0 && report.Clamped == 0, "wide extents: silence");
        Check(outBlob.LabelLength == 70000 && outBlob.Label[69999] == (byte)'w', "wide extents: label");
        Check(outBlob.PayloadLength == 70000 && outBlob.Payload[69999] == (byte)(69999 & 0xff), "wide extents: payload");
        Check(outBlob.SamplesCount == 70000 && outBlob.Samples[69999] == unchecked((ushort)69999), "wide extents: samples");

        byte[] payload = Join(new byte[] { 7 }, Var(70000), new byte[] { 11,0,22,0 });
        byte[] wire = Fixture(Join(new byte[] { 1,14 }, Var((ulong)payload.Length), payload,
            new byte[] { 2,12,2,(byte)'o',(byte)'k',0 }), "samples", "label");
        Demo.TableReport report2 = new Demo.TableReport(); Demo.WideBlob out2 = new Demo.WideBlob();
        Check(Demo.Schema.WideBlobLoad(out2, wire, report2), "wide prefix: load");
        Check(report2.Malformed && out2.SamplesCount == 2 && out2.Samples[0] == 11 && out2.Samples[1] == 22,
            "wide prefix: bounded payload cannot fabricate 70000 elements");
        Check(out2.LabelLength == 2 && GetString(out2.Label, 2) == "ok", "wide prefix: sibling survived");
    }

    // ---- clamping: hostile or stale numerics clamp and count ----

    static void TestClamping()
    {
        V1.Cfg value = new V1.Cfg(); V1.TableReport report = new V1.TableReport();
        byte[] wire = Fixture(Join(new byte[] { 1,4 }, U32(2000), new byte[] { 0 }), "a");
        Check(V1.Schema.CfgLoad(value, wire, report) && report.Clamped == 1 && value.A == 1000, "integer clamp");
        Demo.WeaponConfig weapon = new Demo.WeaponConfig(); Demo.TableReport bits = new Demo.TableReport();
        Check(Demo.Schema.WeaponConfigLoad(weapon, Fixture(new byte[] { 1,6,200,0 }, "channel"), bits) &&
            bits.Clamped == 1 && weapon.Channel == 63, "bits width clamp");
        report = new V1.TableReport();
        wire = Fixture(Join(new byte[] { 1,12,40 }, Encoding.UTF8.GetBytes(new string('y',40)), new byte[] { 0 }), "name");
        Check(V1.Schema.CfgLoad(value, wire, report) && report.Clamped == 1 && value.NameLength == 32, "string bound clamp");
    }

    // ---- malformed framing: decode stops, partial result kept, flag raised ----

    static void TestMalformed()
    {
        V1.Cfg value = new V1.Cfg();
        foreach (byte form in new byte[] { 0,2,3,255 })
        {
            V1.TableReport r = new V1.TableReport();
            Check(!V1.Schema.CfgLoad(value, new byte[] { form }, r) && r.Refused && !r.Malformed,
                "unknown form refuses before trailer damage");
            Check(r.Reason == (form == 2 ? "message_form_as_file" : "newer_form"), "named form refusal");
        }
        V1.TableReport report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(value, new byte[] { 1 }, report) && report.Malformed, "known form with missing trailer is damaged");
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(value, Fixture(new byte[] { 1,4,0,0 }, "a"), report) && report.Malformed,
            "truncated scalar stops root");
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(value, Fixture(Join(new byte[] { 1,4 }, U32(42)), "a"), report) && report.Malformed && value.A == 42,
            "missing root terminator keeps good prefix");
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(value, Fixture(Join(new byte[] { 1,4 }, U32(42), new byte[] { 0,99 }), "a"), report) && value.A == 5,
            "slack before vocabulary defaults the entire file");
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(value, Fixture(new byte[] { 0 }, "a", "a"), report) && report.Malformed,
            "duplicate vocabulary entry is damaged");
        report = new V1.TableReport();
        Check(!V1.Schema.CfgLoad(value, Fixture(new byte[] { 0x80,0 }), report) && report.Malformed,
            "noncanonical reference is damaged");
    }

    // ---- reflection: walk, identify and describe fields with no schema files ----

    static void TestReflection()
    {
        Demo.TableTypeInfo weapon = Demo.Schema.WeaponConfigTableType();
        Check(weapon.Name == "WeaponConfig", "reflection: type name");
        Check(weapon.NumFields == weapon.Fields.Length, "reflection: field count");

        // the was rename: speed's wire id is hash("velocity"), not hash("speed")
        Demo.TableFieldInfo speed = DemoField(weapon, "speed");
        Check(speed != null && speed.Id == FieldId("velocity"), "reflection: was rename keeps the old id");
        Check(speed.Id != FieldId("speed"), "reflection: and it is NOT the new name's id");
        Demo.TableFieldInfo damage = DemoField(weapon, "damage");
        Check(damage != null && damage.Id == FieldId("damage") && damage.Kind == 10, "reflection: damage is kF32");

        Demo.TableFieldInfo pen = DemoField(weapon, "penetration");
        Check(pen != null && pen.HasRange && pen.RangeMin == 0.0 && pen.RangeMax == 10.0, "reflection: ranges surface");

        Demo.TableFieldInfo grade = DemoField(Demo.Schema.LoadoutConfigTableType(), "grade");
        Check(grade != null && grade.EnumMax == 3 && grade.EnumName != null, "reflection: enum vocabulary");
        Check(grade.EnumName(3) == "Gold" && grade.EnumName(9) == "???", "reflection: enum names");
        Check(grade.VariantId != null, "reflection: variant ids present");
        Check(grade.VariantId(0) == 0, "reflection: None is the reserved id");
        Check(grade.VariantId(1) == FieldId("Bronze"), "reflection: Bronze id");
        Check(grade.VariantId(2) == FieldId("Silver"), "reflection: Silver id");
        Check(grade.VariantId(3) == FieldId("Gold"), "reflection: Gold id");
        Check(grade.VariantId(9) == 0, "reflection: no variant names 9");

        Demo.TableTypeInfo rootType = Demo.Schema.RootConfigTableType();
        Demo.TableFieldInfo profiles = DemoField(rootType, "profiles");
        Check(profiles != null && profiles.Kind == 13 && profiles.IsArray && profiles.Counted,
            "reflection: nested-table array");
        Check(profiles.ArrayBound == 4, "reflection: array bound");
        Check(ReferenceEquals(profiles.Table, Demo.Schema.ProfileConfigTableType()), "reflection: descriptors chain");

        Demo.TableFieldInfo loadout = DemoField(Demo.Schema.ProfileConfigTableType(), "loadout");
        Check(loadout != null && loadout.Guard == "has_loadout", "reflection: guards surface machine-usable");

        Demo.TableTypeInfo profileType = Demo.Schema.ProfileConfigTableType();
        string[] scalarFields = { "tilt", "heading", "timestamp", "badge", "port", "experience", "epoch", "precision" };
        byte[] scalarKinds = { 2, 3, 5, 6, 7, 8, 9, 11 };
        for (int i = 0; i < scalarFields.Length; i++)
        {
            Demo.TableFieldInfo field = DemoField(profileType, scalarFields[i]);
            Check(field != null && field.Kind == scalarKinds[i], "reflection: kind of " + scalarFields[i]);
        }
        Demo.TableFieldInfo homing = DemoField(weapon, "homing");
        Check(homing != null && homing.Kind == 1, "reflection: bool");
        Demo.TableFieldInfo effect = DemoField(weapon, "effect");
        Check(effect != null && effect.Kind == 15, "reflection: union");
        Check(effect.EnumMax == 2 && effect.EnumName != null && effect.VariantId != null, "reflection: arm vocabulary");
        Check(effect.EnumName(0) == "None" && effect.EnumName(1) == "buff", "reflection: arm names");
        Check(effect.VariantId(0) == 0 && effect.VariantId(2) == FieldId("debuff"), "reflection: arm ids");
        Demo.TableFieldInfo nameF = DemoField(profileType, "name");
        Check(nameF != null && nameF.Kind == 12, "reflection: string");
    }

    // ---- cross-file nesting ----

    static void TestCrossFile()
    {
        Demo.ArchiveConfig archive = new Demo.ArchiveConfig();
        SetString(archive.Root.VersionNote, ref archive.Root.VersionNoteLength, "deep");
        archive.Root.WeaponsCount = 1;
        archive.Root.Weapons[0].Homing = true;
        archive.Count = 5;

        byte[] buffer = new byte[16384];
        long wrote = Demo.Schema.ArchiveConfigSave(archive, buffer);
        Check(wrote > 0 && wrote == Demo.Schema.ArchiveConfigMeasure(archive), "cross-file: measure == save");

        Demo.TableReport report = new Demo.TableReport();
        Demo.ArchiveConfig outArchive = new Demo.ArchiveConfig();
        Check(Demo.Schema.ArchiveConfigLoad(outArchive, new ReadOnlySpan<byte>(buffer, 0, (int)wrote), report),
            "cross-file: load");
        Check(GetString(outArchive.Root.VersionNote, outArchive.Root.VersionNoteLength) == "deep", "cross-file: nested string");
        Check(outArchive.Root.WeaponsCount == 1 && outArchive.Root.Weapons[0].Homing, "cross-file: nested array");
        Check(outArchive.Count == 5, "cross-file: own field");
    }


    // ---- the SEAM instances: `?T` (§2.3) and `[E]T` (§2.4) ----
    //
    // Mirroring test/tables/main.cpp value for value. A keyed field is reached
    // through its INDEXER, which takes the KEY as every port's does; C# has no
    // non-boxing generic enum-to-int, so the CAST is written here and the
    // SHIFT is not (see TableKeyed in the generated runtime). The generated
    // codecs walk .Slots by storage index instead, past the None guard.

    // a `type` body's keyed array is a PLAIN ARRAY (docs/SPEC-TABLES.md §2.4): no
    // wrapper and no indexer, so the STORAGE INDEX is spelled here — the key
    // minus one, the same shift TableKeyed does for a table body.
    static int KeyedIndex(int key) { return key - 1; }

    static void BuildGoldenKeyed(Demo.KeyedConfig cfg)
    {
        Demo.TeamConfig red = cfg.Teams[(int)Demo.Team.Red];
        red.SpawnCount = 8;
        SetString(red.Banner, ref red.BannerLength, "red");
        Demo.TeamConfig green = cfg.Teams[(int)Demo.Team.Green];
        green.SpawnCount = 2;
        SetString(green.Banner, ref green.BannerLength, "green");
        // Blue's slot stays entirely default: a default slot ELIDES (§3.2)

        Demo.HullConfig gunship = cfg.Hulls[(int)Demo.Hull.Gunship];
        gunship.Health = 250.0f;
        gunship.Mass = 3.5f;
        Demo.TurretConfig cannon = gunship.Turrets[(int)Demo.Weapon.Cannon];
        cannon.Damage = 40.0f;
        cannon.GunnerPresent = true;            // present, and entirely DEFAULT: it still rides
        Demo.TurretConfig mine = gunship.Turrets[(int)Demo.Weapon.Mine];
        mine.Damage = 5.0f;
        mine.Cooldown = 9.0f;
        mine.GunnerPresent = true;
        mine.Gunner.Reaction = 0.75f;
        mine.Gunner.Tracking = true;

        Demo.HullConfig freighter = cfg.Hulls[(int)Demo.Hull.Freighter];
        freighter.Mass = 12.0f;                 // turrets all default: the keyed array elides whole

        // a `type`.s keyed field: PLAIN ARRAY storage, shifted like any other
        // keyed array (key k at index k-1), and a keyed BODY on this wire
        cfg.Scores.PerTeam[KeyedIndex((int)Demo.Team.Red)] = 10;
        cfg.Scores.PerTeam[KeyedIndex((int)Demo.Team.Green)] = 30;
    }

    static void BuildGoldenV1Seams(V1.Cfg cfg)
    {
        cfg.A = 3;
        V1.Cell alpha = cfg.Bank[(int)V1.Slot.Alpha];
        alpha.Power = 11;
        SetString(alpha.Label, ref alpha.LabelLength, "a1");
        cfg.Bank[(int)V1.Slot.Beta].Power = 22;   // ordinal 2 in V1, 3 in V2
        cfg.Bank[(int)V1.Slot.Gamma].Power = 33;  // REMOVED in V2
        cfg.Tokens[(int)V1.Slot.Alpha] = 101;
        cfg.Tokens[(int)V1.Slot.Beta] = 102;
        cfg.Tokens[(int)V1.Slot.Delta] = 104;
        cfg.Ranks[(int)V1.Slot.Alpha] = V1.Grade.Gold;
        cfg.Ranks[(int)V1.Slot.Gamma] = V1.Grade.Bronze;
        cfg.Ledger[0] = 7; cfg.Ledger[2] = 9;   // POSITIONAL in V1, KEYED in V2: kind 14 vs 16
        cfg.ExtraPresent = true;
        cfg.Extra.Factor = 6.25f;
        cfg.TierPresent = true;
        cfg.Tier = 41;
        cfg.MarkPresent = true;
        cfg.Mark = V1.Grade.Gold;
    }

    static void BuildGoldenV2Seams(V2.Cfg cfg)
    {
        cfg.A = 1.5f;
        V2.Cell alpha = cfg.Bank[(int)V2.Slot.Alpha];
        alpha.Power = 11;
        SetString(alpha.Label, ref alpha.LabelLength, "a1");
        cfg.Bank[(int)V2.Slot.Omega].Power = 44;  // INSERTED in V2; V1 cannot name it
        cfg.Bank[(int)V2.Slot.Beta].Power = 22;   // slid from ordinal 2 to 3
        cfg.Bank[(int)V2.Slot.Sigma].Power = 55;  // appended; V1 cannot name it
        cfg.Tokens[(int)V2.Slot.Alpha] = 101;
        cfg.Tokens[(int)V2.Slot.Beta] = 102;
        cfg.Ranks[(int)V2.Slot.Alpha] = V2.Grade.Gold;
        cfg.Ledger[(int)V2.Grade.Bronze] = 7;
        cfg.Ledger[(int)V2.Grade.Gold] = 9;       // KEYED in V2
        cfg.ExtraPresent = true;
        cfg.Extra.Factor = 6.25f;
        cfg.TierPresent = false;                // absent: nothing rides
        cfg.MarkPresent = true;
        cfg.Mark = V2.Grade.Gold;
    }

    static void BuildGoldenChainValue(P1.Chain chain)
    {
        SetString(chain.Name, ref chain.NameLength, "chain");
        chain.Link.Value = 7;
        SetString(chain.Link.Tag, ref chain.Link.TagLength, "tip");
    }

    // ---- gate: the seam goldens, written from C# ----

    static void TestGoldenSeamsWrite()
    {
        byte[] buffer = new byte[1 << 20];

        Demo.KeyedConfig cfg = new Demo.KeyedConfig();
        BuildGoldenKeyed(cfg);
        long wrote = Demo.Schema.KeyedConfigSave(cfg, buffer);
        Check(wrote > 0 && wrote == Demo.Schema.KeyedConfigMeasure(cfg), "keyed_config: measure == save");
        GoldenWire("keyed_config", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        Demo.KeyedConfig empty = new Demo.KeyedConfig();
        wrote = Demo.Schema.KeyedConfigSave(empty, buffer);
        Check(wrote == 10, "keyed_default: every slot default, every keyed array elides");
        GoldenWire("keyed_default", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        V1.Cfg v1 = new V1.Cfg();
        BuildGoldenV1Seams(v1);
        wrote = V1.Schema.CfgSave(v1, buffer);
        Check(wrote > 0 && wrote == V1.Schema.CfgMeasure(v1), "v1_seams: measure == save");
        GoldenWire("v1_seams", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        V2.Cfg v2 = new V2.Cfg();
        BuildGoldenV2Seams(v2);
        wrote = V2.Schema.CfgSave(v2, buffer);
        Check(wrote > 0 && wrote == V2.Schema.CfgMeasure(v2), "v2_seams: measure == save");
        GoldenWire("v2_seams", new ReadOnlySpan<byte>(buffer, 0, (int)wrote));

        // T and ?T over NON-DEFAULT content: byte-identical, one framing with
        // two spellings. *T is NOT in that family — a pointer rides as a node
        // index under its own kind (docs/SPEC-TABLES.md §2.3, §3.1); C# refuses
        // the pointered unit, so its bytes come from the C++-pinned golden.
        P1.Chain value = new P1.Chain();
        BuildGoldenChainValue(value);
        long wValue = P1.Schema.ChainSave(value, buffer);
        Check(wValue > 0, "chain_value: saved");
        GoldenWire("chain_value", new ReadOnlySpan<byte>(buffer, 0, (int)wValue));

        byte[] other = new byte[4096];
        P3.Chain optional = new P3.Chain();
        SetString(optional.Name, ref optional.NameLength, "chain");
        optional.LinkPresent = true;
        optional.Link.Value = 7;
        SetString(optional.Link.Tag, ref optional.Link.TagLength, "tip");
        long wOpt = P3.Schema.ChainSave(optional, other);
        Check(wOpt == wValue && new ReadOnlySpan<byte>(other, 0, (int)wOpt).SequenceEqual(new ReadOnlySpan<byte>(buffer, 0, (int)wValue)),
            "?T and a plain nesting are byte-identical over non-default content");
        GoldenWire("chain_optional", new ReadOnlySpan<byte>(other, 0, (int)wOpt));
        Check(!new ReadOnlySpan<byte>(other, 0, (int)wOpt).SequenceEqual(ReadGolden("chain_pointer")),
            "and *T is not — the pointer's bytes, written by C++, carry a node index and a node table");

        // the ASYMMETRY at the empty end: a by-value nesting at its defaults
        // writes nothing; a PRESENT optional writes its body anyway
        P1.Chain valueEmpty = new P1.Chain();
        long wValueEmpty = P1.Schema.ChainSave(valueEmpty, buffer);
        Check(wValueEmpty == 10, "chain_value_empty: an all-default nesting elides");
        GoldenWire("chain_value_empty", new ReadOnlySpan<byte>(buffer, 0, (int)wValueEmpty));

        P3.Chain optionalEmpty = new P3.Chain();
        optionalEmpty.LinkPresent = true; // present and all-default: it RIDES
        long wOptEmpty = P3.Schema.ChainSave(optionalEmpty, other);
        Check(wOptEmpty > wValueEmpty, "chain_optional_empty: presence decides, not content");
        GoldenWire("chain_optional_empty", new ReadOnlySpan<byte>(other, 0, (int)wOptEmpty));
        Check(!new ReadOnlySpan<byte>(other, 0, (int)wOptEmpty).SequenceEqual(ReadGolden("chain_pointer_empty")),
            "a non-null empty pointer shares the rule (presence decides) but not the bytes: it rides as an index and a node table");
    }

    // ---- gate: LOAD the C++-written seam bytes ----

    static void TestGoldenSeamsRead()
    {
        {
            byte[] golden = ReadGolden("keyed_config");
            Demo.TableReport report = new Demo.TableReport();
            Demo.KeyedConfig cfg = new Demo.KeyedConfig();
            Check(Demo.Schema.KeyedConfigLoad(cfg, golden, report), "keyed_config loads");
            Check(!report.Malformed && report.Unknown == 0 && report.KindMismatch == 0 && report.Clamped == 0,
                "keyed_config: silence");
            Check(report.Duplicate == 0, "keyed_config: duplicate is the text form's counter — a wire read leaves it zero");
            Check(cfg.Teams[(int)Demo.Team.Red].SpawnCount == 8, "keyed: Red's slot");
            Check(GetString(cfg.Teams[(int)Demo.Team.Green].Banner, cfg.Teams[(int)Demo.Team.Green].BannerLength) == "green",
                "keyed: Green's slot");
            Check(cfg.Teams[(int)Demo.Team.Blue].SpawnCount == 4, "keyed: Blue's elided slot keeps its declared default");
            Demo.HullConfig gunship = cfg.Hulls[(int)Demo.Hull.Gunship];
            Check(gunship.Health == 250.0f && gunship.Mass == 3.5f, "keyed: nested hull");
            Demo.TurretConfig cannon = gunship.Turrets[(int)Demo.Weapon.Cannon];
            Check(cannon.Damage == 40.0f, "keyed: nested keyed array");
            Check(cannon.GunnerPresent && cannon.Gunner.Reaction == 0.2f,
                "keyed: a PRESENT all-default optional rode, and reads back present at its defaults");
            Demo.TurretConfig mine = gunship.Turrets[(int)Demo.Weapon.Mine];
            Check(mine.GunnerPresent && mine.Gunner.Reaction == 0.75f && mine.Gunner.Tracking, "keyed: optional with content");
            Check(!gunship.Turrets[(int)Demo.Weapon.Missile].GunnerPresent,
                "keyed: an untouched slot's optional is ABSENT");
            Check(cfg.Hulls[(int)Demo.Hull.Freighter].Mass == 12.0f, "keyed: freighter");
            Check(cfg.Scores.PerTeam[KeyedIndex((int)Demo.Team.Red)] == 10
                && cfg.Scores.PerTeam[KeyedIndex((int)Demo.Team.Green)] == 30, "keyed: a type's keyed field");
            Check(cfg.Scores.PerTeam[KeyedIndex((int)Demo.Team.Blue)] == 0, "keyed: an unsent slot defaults");
        }
        {
            byte[] golden = ReadGolden("v1_seams");
            V1.TableReport report = new V1.TableReport();
            V1.Cfg cfg = new V1.Cfg();
            Check(V1.Schema.CfgLoad(cfg, golden, report), "v1_seams loads");
            Check(!report.Malformed && report.Unknown == 0 && report.Clamped == 0, "v1_seams: silence");
            Check(cfg.Bank[(int)V1.Slot.Alpha].Power == 11 && cfg.Bank[(int)V1.Slot.Beta].Power == 22
                && cfg.Bank[(int)V1.Slot.Gamma].Power == 33, "v1_seams: keyed table slots");
            Check(cfg.Tokens[(int)V1.Slot.Delta] == 104 && cfg.Ranks[(int)V1.Slot.Alpha] == V1.Grade.Gold,
                "v1_seams: keyed scalars and enums");
            Check(cfg.ExtraPresent && cfg.Extra.Factor == 6.25f, "v1_seams: optional table");
            Check(cfg.TierPresent && cfg.Tier == 41, "v1_seams: optional scalar");
            Check(cfg.MarkPresent && cfg.Mark == V1.Grade.Gold, "v1_seams: optional enum");
        }
        {
            byte[] golden = ReadGolden("v2_seams");
            V2.TableReport report = new V2.TableReport();
            V2.Cfg cfg = new V2.Cfg();
            Check(V2.Schema.CfgLoad(cfg, golden, report), "v2_seams loads");
            Check(!report.Malformed && report.Unknown == 0, "v2_seams: silence");
            Check(cfg.Bank[(int)V2.Slot.Sigma].Power == 55, "v2_seams: the appended slot");
            Check(!cfg.TierPresent && cfg.Tier == 0, "v2_seams: an absent optional stays absent at its default");
            Check(cfg.Ledger[(int)V2.Grade.Bronze] == 7 && cfg.Ledger[(int)V2.Grade.Gold] == 9,
                "v2_seams: the keyed ledger");
        }
        // the pointer's bytes, read by the two spellings C# carries: a REPORTED
        // edit, not a decode. The pointer rides as a node index under kind 17,
        // so a T or ?T reader counts one kind mismatch, leaves the field at its
        // declared default and reads the rest of the body on; the node table
        // rides under the reserved id 0xFFFF and is one unknown field to a
        // reader that cannot name it (docs/SPEC-TABLES.md §2.3, §3.1)
        {
            byte[] golden = ReadGolden("chain_pointer");
            P1.TableReport r1 = new P1.TableReport();
            P1.Chain byValue = new P1.Chain();
            byValue.Link.Value = 7; // junk the reset must erase
            Check(P1.Schema.ChainLoad(byValue, golden, r1), "chain_pointer loads into a by-value nesting");
            Check(!r1.Malformed && r1.KindMismatch == 1 && r1.Unknown == 1,
                "*T through T: one kind mismatch for the index, one unknown for the node table");
            Check(GetString(byValue.Name, byValue.NameLength) == "chain" && byValue.Link.Value == 0,
                "*T through T: the field takes its declared default and the rest of the body reads on");
            P3.TableReport r3 = new P3.TableReport();
            P3.Chain optional = new P3.Chain();
            optional.LinkPresent = true; // junk the reset must erase
            Check(P3.Schema.ChainLoad(optional, golden, r3), "chain_pointer loads into an optional");
            Check(!r3.Malformed && r3.KindMismatch == 1 && r3.Unknown == 1,
                "*T through ?T: one kind mismatch for the index, one unknown for the node table");
            Check(!optional.LinkPresent && optional.Link.Value == 0 && GetString(optional.Name, optional.NameLength) == "chain",
                "*T through ?T: absent, at its default, and the rest of the body reads on");
        }
        {
            byte[] golden = ReadGolden("chain_value_empty");
            P3.TableReport r3 = new P3.TableReport();
            P3.Chain optional = new P3.Chain();
            optional.LinkPresent = true; // junk the reset must erase
            Check(P3.Schema.ChainLoad(optional, golden, r3), "chain_value_empty loads into an optional");
            Check(!optional.LinkPresent, "an elided by-value nesting reads as ABSENT through ?T — the right answer");
        }
        {
            byte[] golden = ReadGolden("chain_pointer_empty");
            P1.TableReport r1 = new P1.TableReport();
            P1.Chain byValue = new P1.Chain();
            Check(P1.Schema.ChainLoad(byValue, golden, r1), "chain_pointer_empty loads into a by-value nesting");
            Check(!r1.Malformed && r1.KindMismatch == 1 && r1.Unknown == 1 && byValue.Link.Value == 0,
                "a non-null empty pointee is still an index: reported, and the field reads as the declared default by value");
        }
    }

    // ---- optional fields: PRESENCE decides, never content (§2.3) ----

    static void TestOptionalPresence()
    {
        byte[] wire = new byte[1024];

        // ABSENT: nothing rides at all
        V1.Cfg none = new V1.Cfg();
        long bytes = V1.Schema.CfgSave(none, wire);
        Check(bytes == 10, "optional: absent writes nothing — the terminator alone");

        // PRESENT and entirely DEFAULT: it rides anyway
        V1.Cfg present = new V1.Cfg();
        present.ExtraPresent = true;
        long presentBytes = V1.Schema.CfgSave(present, wire);
        Check(presentBytes > bytes, "optional: present-at-default RIDES");
        Check(presentBytes == V1.Schema.CfgMeasure(present), "optional: measure == save");

        V1.TableReport report = new V1.TableReport();
        V1.Cfg outCfg = new V1.Cfg();
        Check(V1.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)presentBytes), report), "optional: load");
        Check(outCfg.ExtraPresent, "optional: a field that rode is PRESENT");
        Check(outCfg.Extra.Factor == 2.5f, "optional: present, at its declared default");
        Check(!outCfg.TierPresent && !outCfg.MarkPresent, "optional: the fields that did not ride are ABSENT");

        // a present scalar with content, and the reset erasing stale presence
        V1.Cfg scalar = new V1.Cfg();
        scalar.TierPresent = true;
        scalar.Tier = 9;
        bytes = V1.Schema.CfgSave(scalar, wire);
        V1.TableReport r2 = new V1.TableReport();
        V1.Cfg out2 = new V1.Cfg();
        out2.ExtraPresent = true; // junk the reset must erase
        out2.MarkPresent = true;
        Check(V1.Schema.CfgLoad(out2, new ReadOnlySpan<byte>(wire, 0, (int)bytes), r2), "optional scalar: load");
        Check(out2.TierPresent && out2.Tier == 9, "optional scalar: present with content");
        Check(!out2.ExtraPresent && !out2.MarkPresent, "optional: the reset erases stale presence");
    }

    // ---- enum-keyed arrays: the middle insert and the removal (§3.2) ----

    static void TestKeyedEvolution()
    {
        byte[] wire = new byte[4096];

        V1.Cfg v1 = new V1.Cfg();
        BuildGoldenV1Seams(v1);
        long bytes = V1.Schema.CfgSave(v1, wire);
        Check(bytes > 0, "keyed evolution: v1 saved");

        V2.TableReport report = new V2.TableReport();
        V2.Cfg newReader = new V2.Cfg();
        Check(V2.Schema.CfgLoad(newReader, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "keyed evolution: v2 reads v1");
        Check(!report.Malformed, "keyed evolution: not malformed");
        // Gamma was REMOVED in V2, so its slot is a key V2 cannot name: 2 of
        // them (bank and tokens carry a Gamma slot; ranks does not)
        // Gamma was REMOVED in V2, so V1's two Gamma slots (bank and ranks)
        // are keys V2 cannot name; `a` changed kind and `ledger` changed
        // ENCODING, positional to keyed. The C++ leg pins the same four counts
        // on the same bytes.
        Check(report.Unknown == 2, "keyed evolution: two Gamma slots V2 cannot name — counted, never misplaced");
        Check(report.KindMismatch == 2, "keyed evolution: a (i32 -> f32) and ledger (positional -> keyed)");
        Check(report.Clamped == 0, "keyed evolution: nothing clamped");
        // BETA SLID from ordinal 2 to 3 and still lands in its own home
        Check(newReader.Bank[(int)V2.Slot.Beta].Power == 22,
            "keyed evolution: Beta rode by NAME — a positional encoding would have put it in Omega's slot");
        Check(newReader.Bank[(int)V2.Slot.Alpha].Power == 11, "keyed evolution: Alpha");
        Check(newReader.Bank[(int)V2.Slot.Omega].Power == 0,
            "keyed evolution: Omega is new in V2 and the writer never sent it — its slot keeps its default");
        Check(newReader.Tokens[(int)V2.Slot.Beta] == 102, "keyed evolution: a scalar slot by name");
        Check(newReader.Tokens[(int)V2.Slot.Delta] == 104, "keyed evolution: Delta kept ordinal 4");

        // the other direction: V2's data into V1
        V2.Cfg v2 = new V2.Cfg();
        BuildGoldenV2Seams(v2);
        bytes = V2.Schema.CfgSave(v2, wire);
        V1.TableReport back = new V1.TableReport();
        V1.Cfg oldReader = new V1.Cfg();
        Check(V1.Schema.CfgLoad(oldReader, new ReadOnlySpan<byte>(wire, 0, (int)bytes), back), "keyed evolution: v1 reads v2");
        Check(!back.Malformed, "keyed evolution reverse: not malformed");
        Check(oldReader.Bank[(int)V1.Slot.Beta].Power == 22, "keyed evolution reverse: Beta found its home");
        Check(oldReader.Bank[(int)V1.Slot.Gamma].Power == 0, "keyed evolution reverse: Gamma unsent, keeps its default");
        Check(back.Unknown == 2, "keyed evolution reverse: Omega and Sigma are names V1 does not have");
        Check(back.KindMismatch == 2, "keyed evolution reverse: a and ledger again");

        // THE NEGATIVE CONTROL, in the data: the keyed body's whole point is
        // that Beta's slot is found by NAME. Under a positional encoding
        // Beta (ordinal 2 in V1) would land in ordinal 2 of V2 — Omega's slot.
        Check(newReader.Bank[(int)V2.Slot.Omega].Power != 22,
            "keyed evolution: Beta did NOT land in the slot its old ordinal names");
    }

    // ---- keyed and positional do not decode each other (§3.2) ----

    static void TestKeyedVersusPositional()
    {
        // `ledger` is [Grade.Max + 1]int32 in V1 (kind 14, positional) and
        // [Grade]int32 in V2 (kind 16, keyed): the same field name, two
        // encodings, and neither may be decoded as the other.
        V1.TableFieldInfo v1Ledger = V1Field(V1.Schema.CfgTableType(), "ledger");
        Check(v1Ledger != null && v1Ledger.KeyTypeName == null, "ledger: positional in V1 — no key vocabulary");

        byte[] wire = new byte[4096];
        V1.Cfg v1 = new V1.Cfg();
        v1.Ledger[0] = 7; v1.Ledger[2] = 9;
        long bytes = V1.Schema.CfgSave(v1, wire);

        V2.TableReport report = new V2.TableReport();
        V2.Cfg outCfg = new V2.Cfg();
        Check(V2.Schema.CfgLoad(outCfg, new ReadOnlySpan<byte>(wire, 0, (int)bytes), report), "keyed vs positional: load");
        Check(report.KindMismatch == 1, "keyed vs positional: a positional body under a keyed field is a KIND MISMATCH");
        Check(!report.Malformed, "keyed vs positional: skipped, not damaged");
        Check(outCfg.Ledger[(int)V2.Grade.Bronze] == 0 && outCfg.Ledger[(int)V2.Grade.Silver] == 0,
            "keyed vs positional: the array is left at its declared defaults, never misdecoded");

        // and the reverse
        V2.Cfg v2 = new V2.Cfg();
        v2.Ledger[(int)V2.Grade.Bronze] = 7; v2.Ledger[(int)V2.Grade.Gold] = 9;
        bytes = V2.Schema.CfgSave(v2, wire);
        V1.TableReport back = new V1.TableReport();
        V1.Cfg old = new V1.Cfg();
        Check(V1.Schema.CfgLoad(old, new ReadOnlySpan<byte>(wire, 0, (int)bytes), back), "keyed vs positional reverse: load");
        Check(back.KindMismatch == 1, "keyed vs positional reverse: a keyed body under a positional field is a KIND MISMATCH");
        Check(old.Ledger[1] == 0 && old.Ledger[2] == 0, "keyed vs positional reverse: left at defaults");
    }

    // ---- a stored key of 0 is MALFORMED and STOPS THE BODY (§3.2) ----

    static void TestKeyedHostileKeys()
    {
        byte[] payload = Join(new byte[] { 4,2,2,4 }, U32(77), new byte[] { 0,4 }, U32(88));
        byte[] wire = Fixture(Join(new byte[] { 1,16 }, Var((ulong)payload.Length), payload,
            new byte[] { 3,4 }, U32(42), new byte[] { 0 }), "tokens", "Beta", "a");
        V1.Cfg value = new V1.Cfg(); V1.TableReport report = new V1.TableReport();
        Check(V1.Schema.CfgLoad(value, wire, report), "null key: load");
        Check(report.Malformed && report.Unknown == 0 && value.Tokens[(int)V1.Slot.Beta] == 77 && value.A == 42,
            "null key: damage keeps prefix and sibling");
        payload = Join(new byte[] { 4,2,2,4 }, U32(88), new byte[] { 3,4 }, U32(99));
        wire = Fixture(Join(new byte[] { 1,16 }, Var((ulong)payload.Length), payload, new byte[] { 0 }), "tokens", "unknown", "Delta");
        report = new V1.TableReport();
        Check(V1.Schema.CfgLoad(value, wire, report) && !report.Malformed && report.Unknown == 1 &&
            value.Tokens[(int)V1.Slot.Delta] == 99, "unknown key: skip and continue");
    }

    // ---- reflection: the presence companion and the key's vocabulary (§8) ----

    static void TestSeamReflection()
    {
        V1.TableTypeInfo cfg = V1.Schema.CfgTableType();

        V1.TableFieldInfo extra = V1Field(cfg, "extra");
        Check(extra != null && extra.Optional, "reflection: ?T is marked optional");
        Check(extra.Kind == 13, "reflection: an optional table body is a table kind — the framing ?T and T share");
        V1.TableFieldInfo tier = V1Field(cfg, "tier");
        Check(tier != null && tier.Optional && tier.Kind == 4, "reflection: an optional scalar");
        V1.TableFieldInfo grade = V1Field(cfg, "grade");
        Check(grade != null && !grade.Optional, "reflection: a plain field is not optional");

        V1.TableFieldInfo bank = V1Field(cfg, "bank");
        Check(bank != null && bank.IsArray && !bank.Counted, "reflection: a keyed array is an array with no count");
        Check(bank.ArrayBound == 4, "reflection: Slot.Max — one slot per named variant");
        Check(bank.KeyTypeName == "Slot", "reflection: the keying enum is named");
        // KeyName and KeyId are functions of the KEY, not of the storage index:
        // a walker steps [0, ArrayBound) and asks about index + 1
        Check(bank.KeyName(2) == "Beta" && bank.KeyId(2) == FieldId("Beta"), "reflection: key 2 is Beta in V1");
        // None is a KEY the enum has and it names no slot
        Check(bank.KeyId(0) == 0, "reflection: KeyId(0) is 0 — the reserved id says None keys no slot");
        Check(bank.KeyName(0) == "None", "reflection: KeyName(0) is None");
        for (int slot = 0; slot < bank.ArrayBound; slot++)
        {
            Check(bank.KeyId((ulong)(slot + 1)) != 0, "reflection: every stored slot holds a nameable key");
        }

        // a keyed array OF enums carries BOTH vocabularies
        V1.TableFieldInfo ranks = V1Field(cfg, "ranks");
        Check(ranks != null && ranks.KeyTypeName != null && ranks.EnumName != null, "reflection: both vocabularies");
        Check(ranks.EnumName(2) == "Gold" && ranks.KeyName(1) == "Alpha", "reflection: element and key names");

        // a POSITIONAL array names no key — the contrast the feature exists for
        V1.TableFieldInfo tally = V1Field(cfg, "tally");
        Check(tally != null && tally.IsArray, "reflection: tally is an array");
        Check(tally.KeyTypeName == null && tally.KeyName == null && tally.KeyId == null,
            "reflection: a positional array carries no key vocabulary");
    }

    // ---- the keyed indexer refuses the None key at runtime (§2.4) ----

    static void TestKeyedIndexerRefusesNone()
    {
        Demo.KeyedConfig cfg = new Demo.KeyedConfig();
        bool threw = false;
        try
        {
            Demo.TeamConfig ignored = cfg.Teams[0];
            Check(ignored == null, "unreachable");
        }
        catch (ArgumentOutOfRangeException)
        {
            threw = true;
        }
        Check(threw, "keyed indexer: None is the null key and indexing it is an error");

        // and a real slot is reachable through the same indexer
        Check(cfg.Teams[(int)Demo.Team.Blue] != null, "keyed indexer: a named slot reads");
    }

    // ---- iteration over the VALID slots (§2.4) ----
    //
    // The C++ twin of this test is test_keyed_iteration in test/tables/main.cpp.
    // foreach walks every stored slot and yields the KEY, 1..E.Max, beside the
    // element — the same currency the indexer takes — so no call site out here
    // spells a bound, a lower limit or the shift.

    static void TestKeyedIteration()
    {
        Demo.KeyedConfig cfg = new Demo.KeyedConfig();

        // WRITING through the iteration: the element of a class-typed keyed
        // array is the live instance, so filling one is the same walk
        int spawn = 10;
        int seen = 0;
        int expect = 1; // keys arrive in ascending variant order, from 1
        foreach (var (key, team) in cfg.Teams)
        {
            Check(key != 0, "keyed iteration: None is never yielded");
            Check(key == expect, "keyed iteration: keys ascend from 1");
            expect++;
            seen++;
            team.SpawnCount = spawn++;
        }
        Check(seen == 3, "keyed iteration: one slot per named variant, never Max + 1");
        Check(cfg.Teams[(int)Demo.Team.Red].SpawnCount == 10, "keyed iteration: Red filled");
        Check(cfg.Teams[(int)Demo.Team.Green].SpawnCount == 12, "keyed iteration: Green filled");
        // there is no None slot: the storage is one element per named variant
        Check(cfg.Teams.Slots.Length == 3, "keyed iteration: E.Max slots, nothing for None");

        // every keyed array in the corpus, including a nested one
        foreach (var entry in cfg.Hulls)
        {
            Check(entry.Key != 0, "keyed iteration: hulls never yield None");
            int turrets = 0;
            foreach (var turret in entry.Element.Turrets)
            {
                Check(turret.Key != 0, "keyed iteration: nested turrets never yield None");
                turrets++;
            }
            Check(turrets == 3, "keyed iteration: one turret slot per weapon");
        }

        // the space-shaped corpus: one record per ship type, one threshold per
        // difficulty — the config bin this construct exists for. Its ships are
        // CLASS elements, so the iteration's element is the live instance and
        // writing through it is visible; its thresholds are int32 VALUES, so
        // the iteration reads them and the indexer writes them.
        Demo.PackConfig pack = new Demo.PackConfig();
        int ships = 0;
        foreach (var (ship_type, ship) in pack.Ships)
        {
            Check(ship_type != 0, "keyed iteration: ships never yield None");
            ship.Mass = 2.0f;
            ships++;
        }
        Check(ships == 3, "keyed iteration: one record per ship type");
        Check(pack.Ships.Slots.Length == 3, "keyed iteration: E.Max ship slots, nothing for None");
        Check(pack.Ships[(int)Demo.ShipType.Bomber].Mass == 2.0f,
            "keyed iteration: a class element is the live instance");

        pack.Thresholds[(int)Demo.Difficulty.Hard] = 700;
        int thresholds = 0;
        int highest = 0;
        foreach (var (difficulty, threshold) in pack.Thresholds)
        {
            Check(difficulty != 0, "keyed iteration: thresholds never yield None");
            highest = threshold > highest ? threshold : highest;
            thresholds++;
        }
        Check(thresholds == 3 && highest == 700,
            "keyed iteration: a value element reads back what the indexer wrote");

        // both generations of the evolution unit: tables, scalars and enums as
        // elements, over an enum V2 inserts into and removes from — V1's Slot
        // has four variants, V2's five, and each iteration is E.Max long in
        // the generation that declares it. A scalar slot is a VALUE here, not
        // a reference: the indexer is how a scalar slot is written, and
        // iteration is how it is read.
        V1.Cfg v1 = new V1.Cfg();
        int v1bank = 0, v1tokens = 0, v1ranks = 0;
        foreach (var entry in v1.Bank)
        {
            Check(entry.Key != 0, "keyed iteration: V1 bank never yields None");
            v1bank++;
        }
        foreach (var entry in v1.Tokens)
        {
            Check(entry.Key != 0, "keyed iteration: V1 tokens never yield None");
            v1tokens++;
        }
        foreach (var entry in v1.Ranks)
        {
            Check(entry.Key != 0, "keyed iteration: V1 ranks never yield None");
            v1ranks++;
        }
        Check(v1bank == 4 && v1tokens == 4 && v1ranks == 4,
            "keyed iteration: V1's Slot has four variants");

        V2.Cfg v2 = new V2.Cfg();
        v2.Bank[(int)V2.Slot.Beta].Power = 42;
        int bank = 0;
        int power = 0;
        foreach (var (slot, cell) in v2.Bank)
        {
            Check(slot != 0, "keyed iteration: bank never yields None");
            power += cell.Power;
            bank++;
        }
        Check(bank == 5 && power == 42, "keyed iteration: a keyed array of tables");

        v2.Tokens[(int)V2.Slot.Alpha] = 10;
        int tokens = 0;
        int total = 0;
        foreach (var (key, value) in v2.Tokens)
        {
            Check(key != 0, "keyed iteration: tokens never yield None");
            total += value;
            tokens++;
        }
        Check(tokens == 5 && total == 10, "keyed iteration: five slots, one written");

        int ranks = 0;
        foreach (var rank in v2.Ranks)
        {
            Check(rank.Key != 0, "keyed iteration: ranks never yield None");
            ranks++;
        }
        Check(ranks == 5, "keyed iteration: an enum-element keyed array");

        int ledger = 0;
        foreach (var slot in v2.Ledger)
        {
            Check(slot.Key != 0, "keyed iteration: ledger never yields None");
            ledger++;
        }
        Check(ledger == 3, "keyed iteration: Grade's three variants");

        // a value filled by iteration rides and reads back by name
        byte[] wire = new byte[8192];
        long wrote = Demo.Schema.KeyedConfigSave(cfg, wire);
        Check(wrote > 0, "keyed iteration: saved");
        Demo.KeyedConfig back = new Demo.KeyedConfig();
        Demo.TableReport report = new Demo.TableReport();
        Check(Demo.Schema.KeyedConfigLoad(back, new ReadOnlySpan<byte>(wire, 0, (int)wrote), report),
            "keyed iteration: loaded");
        foreach (var (key, team) in back.Teams)
        {
            Check(team.SpawnCount == 9 + key, "keyed iteration: every slot rode by name");
        }
    }

    static int Main()
    {
        goldenDir = FindGoldenDir();

        TestWireContracts();
        TestUnionContracts();
        TestCollections();
        TestSurfaces();
        TestCollectionMessages();
        TestGoldenWireWrite();
        TestGoldenWireRead();
        TestGoldenSeamsWrite();
        TestGoldenSeamsRead();
        TestOptionalPresence();
        TestKeyedEvolution();
        TestKeyedVersusPositional();
        TestKeyedHostileKeys();
        TestSeamReflection();
        TestKeyedIndexerRefusesNone();
        TestKeyedIteration();
        TestExactCapacity();
        TestStorageInvariants();
        TestBoundedElements();
        TestAllDefault();
        TestGuard();
        TestEvolutionOldReaderNewData();
        TestEvolutionNewReaderOldData();
        TestEvolutionEnumInsertOldData();
        TestEvolutionEnumInsertNewData();
        TestEvolutionUnionInsertOldData();
        TestEvolutionUnionInsertNewData();
        TestRepeatedIdUnnameableVariant();
        TestEvolutionArrayBounds();
        TestUnnameableEnumRefused();
        TestUnnameableEnumElementRead();
        TestFlagsArePositional();
        TestWideExtents();
        TestClamping();
        TestMalformed();
        TestReflection();
        TestCrossFile();

        if (failed)
        {
            Console.WriteLine("cs tables test: FAILED");
            return 1;
        }
        Console.WriteLine("cs tables test passed");
        return 0;
    }
}
