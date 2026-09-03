/*
    THE COOKED FORM's C# READ SIDE, under test (SPEC-TABLES.md §7).

    `schema cook` writes the file and the generated <Root>Cook.Open points at
    it, and the two were written from the page independently: the tool in Go,
    this side in C#, neither reading the other and neither reading the C++ leg.
    That is what makes the first two modes CROSS-IMPLEMENTATION gates rather
    than one implementation agreeing with itself.

      golden <root> <cook>      every node the C# side reaches through its own
                                derefs is a node the cook's ATTRIBUTION part
                                names, at that offset, with that type id — and
                                the two sets are equal
      dump   <root> <cook>      the same walk, written as canonical text, so the
                                C++ leg's dump and this one are byte-compared:
                                the lock on VALUES and not only on offsets
      fixedvalues <root> <cook> a FIXED root's values read back out of a cook
                                the C++ side wrote the wire for and the tool
                                cooked — a three-language crossing
      usage  <root> <cook>      USAGE.md's C# cook example, compiled and run
      forge  <root> <cook>      the directed battery: one edit per fact §7 says
                                Open checks, each refused; and one edit per fact
                                §7 says Open does NOT check, each opened
      fuzz   <root> <cook>      the seeded fuzzer, same oracle (SEED=, N=)
      time   <root> <a> <b>     open time flat across two cooks of very
                                different sizes: the O(1) bar (§7)
      accept <root> <cook>      the byte-order leg: a cook of THIS build's
      refuse <root> <cook>      order opens, one of the other order refuses

    A COOK IS TRUSTED INPUT, LOADED FROM DISK (§7), so nothing here is a threat
    model. The battery and the fuzzer HARDEN THE REFUSAL PATH: Open runs on
    whatever bytes a disk hands back, and what these hold is that refusing is
    CLEAN. They ask Open to validate nothing, and the forgeries that OPEN by
    design are that ruling written as a test.

    Prints OK and exits 0 — no test framework, the exit code is the verdict.
*/

using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Text;
using Graphdemo;

static class Fail
{
    // The last thing attempted, FLUSHED before every attempt: C# has no
    // sanitizer death callback, and a read past the guard page ends the process
    // where it stands, so the site line has to already be on the wire.
    public static string Site = "(none)";

    public static void Describe(string what)
    {
        Site = what;
        Console.Out.Flush();
    }

    public static void Now(string what)
    {
        Console.Out.Flush();
        Console.Error.WriteLine();
        Console.Error.WriteLine("FAILED: " + what);
        Console.Error.WriteLine("  site  " + Site);
        Console.Error.WriteLine();
        Console.Error.Flush();
        Environment.Exit(1);
    }
}

static unsafe class Program
{
    // ---- the roots under test ----
    //
    // One entry per root a fixture is cooked at. `Open` is the generated entry
    // point and `Type` its descriptor — the two things the whole test is
    // written against, and neither of them knows what wrote the file.

    delegate IntPtr OpenFn(IntPtr bytes, long length);

    sealed class Root
    {
        public string Name;
        public OpenFn Open;
        public TableCookInfo Type;
        public long Size;
    }

    static IntPtr OpenScene(IntPtr p, long n) { SceneCook c; return SceneCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }
    static IntPtr OpenDepot(IntPtr p, long n) { DepotCook c; return DepotCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }
    static IntPtr OpenAlbum(IntPtr p, long n) { AlbumCook c; return AlbumCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }
    static IntPtr OpenTree(IntPtr p, long n) { TreeNodeCook c; return TreeNodeCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }
    static IntPtr OpenList(IntPtr p, long n) { ListNodeCook c; return ListNodeCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }
    static IntPtr OpenMarker(IntPtr p, long n) { MarkerCook c; return MarkerCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }
    // the FIXED class: a cook of one is ONE REGION OF ONE NODE (§7), and it is
    // the same header match. Settings is a fixed table something POINTS at;
    // Stamp is one nothing points at, declared in a file with no variable table
    // of its own — the shape a file-scoped emission rule forgets.
    static IntPtr OpenSettings(IntPtr p, long n) { SettingsCook c; return SettingsCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }
    static IntPtr OpenStamp(IntPtr p, long n) { StampCook c; return StampCook.Open(out c, p, n) ? (IntPtr)c.Region : IntPtr.Zero; }

    static readonly Root[] roots =
    {
        new Root { Name = "Scene",    Open = OpenScene,    Type = SceneCook.Type,    Size = SceneCook.RootSize },
        new Root { Name = "Depot",    Open = OpenDepot,    Type = DepotCook.Type,    Size = DepotCook.RootSize },
        new Root { Name = "Album",    Open = OpenAlbum,    Type = AlbumCook.Type,    Size = AlbumCook.RootSize },
        new Root { Name = "TreeNode", Open = OpenTree,     Type = TreeNodeCook.Type, Size = TreeNodeCook.RootSize },
        new Root { Name = "ListNode", Open = OpenList,     Type = ListNodeCook.Type, Size = ListNodeCook.RootSize },
        new Root { Name = "Marker",   Open = OpenMarker,   Type = MarkerCook.Type,   Size = MarkerCook.RootSize },
        new Root { Name = "Settings", Open = OpenSettings, Type = SettingsCook.Type, Size = SettingsCook.RootSize },
        new Root { Name = "Stamp",    Open = OpenStamp,    Type = StampCook.Type,    Size = StampCook.RootSize },
    };

    // Every table of the unit, so a directory entry's type id can be turned
    // back into the descriptor that says how big that node is. Reached through
    // the roots' own descriptor graph rather than through a generated registry,
    // because what it must not do is come from the same place the file's type
    // ids come from.
    static readonly Dictionary<ulong, TableCookInfo> unitTables = BuildTableIndex();

    static Dictionary<ulong, TableCookInfo> BuildTableIndex()
    {
        Dictionary<ulong, TableCookInfo> byId = new Dictionary<ulong, TableCookInfo>();
        Dictionary<string, TableCookInfo> byName = new Dictionary<string, TableCookInfo>();
        Action<TableCookInfo> walk = null;
        walk = delegate (TableCookInfo t)
        {
            if (t == null || byName.ContainsKey(t.Name))
            {
                return;
            }
            byName[t.Name] = t;
            foreach (TableCookFieldInfo f in t.Fields)
            {
                walk(f.Record);
            }
        };
        foreach (Root r in roots)
        {
            walk(r.Type);
        }
        foreach (KeyValuePair<string, TableCookInfo> e in byName)
        {
            byId[Header.Fnv1a64(e.Key)] = e.Value;
        }
        return byId;
    }

    static Root RootNamed(string name)
    {
        foreach (Root r in roots)
        {
            if (r.Name == name)
            {
                return r;
            }
        }
        Fail.Now("no root named " + name + " is under test");
        return null;
    }

    static byte[] WholeFile(string path)
    {
        if (!File.Exists(path))
        {
            Fail.Now("cannot open " + path);
        }
        byte[] data = File.ReadAllBytes(path);
        if (data.Length == 0)
        {
            Fail.Now(path + " is empty");
        }
        return data;
    }

    static long AlignmentOf(byte[] source)
    {
        long a = (long)Header.Read64(source, Header.WordAlignment);
        if (a < 1 || a > 64 || (a & (a - 1)) != 0)
        {
            a = 8; // a forged word: place the buffer at the format's own floor
        }
        return a;
    }

    // ---- mode: golden — the cross-implementation lock ----

    static void ModeGolden(Root root, string path, bool asDump)
    {
        byte[] source = WholeFile(path);
        Fail.Describe((asDump ? "dump " : "golden ") + root.Name + " over " + path);
        Native.File file = Native.Place(source, source.Length, 0, AlignmentOf(source));

        IntPtr opened = root.Open((IntPtr)file.Base, file.Length);
        if (opened == IntPtr.Zero)
        {
            Fail.Now("the cook " + path + " did not open — the tool wrote it and this build cannot point at it");
        }

        ulong alignment = Header.Read64(file.Base + Header.WordAlignment);
        ulong dataLength = Header.Read64(file.Base + Header.WordDataLength);
        byte* region = file.Base + Header.DataOffset(alignment);
        if ((byte*)opened != region)
        {
            Fail.Now("Open returned a root that is not at the data part's base");
        }
        if (Header.Read64(file.Base + Header.WordBuildVersion) != Schema.BuildVersion)
        {
            Fail.Now("the cook's build version is not this build's, yet it opened");
        }
        if (Header.Read64(file.Base + Header.WordMagic) != Header.Magic)
        {
            Fail.Now("the cook's magic is not the constant §7.1 states, yet it opened");
        }

        Directory dir = Directory.Of(file.Base, (ulong)source.Length);
        StringBuilder text = asDump ? new StringBuilder() : null;
        Walk.Run(root.Type, region, dataLength, text);

        // (1) every node the walk reached is a node the directory names, at
        // that offset, with the type id the declaration requires. This is the
        // whole crossing: if this runtime laid one record out one byte
        // differently from the model the tool computed the cook for, a deref
        // lands off a directory entry and it is this line that says so.
        for (int i = 0; i < Walk.reached.Count; i++)
        {
            bool found = false;
            for (ulong e = 0; e < dir.Count && !found; e++)
            {
                if (dir.Offset(e) != Walk.reached[i].Offset)
                {
                    continue;
                }
                found = true;
                ulong want = Header.Fnv1a64(Walk.reached[i].Type.Name);
                if (dir.Type(e) != want)
                {
                    Fail.Now("the walk reached offset " + Walk.reached[i].Offset + " as " + Walk.reached[i].Type.Name +
                             ", and the directory names it 0x" + dir.Type(e).ToString("x16"));
                }
            }
            if (!found)
            {
                Fail.Now("the walk reached offset " + Walk.reached[i].Offset + " (" + Walk.reached[i].Type.Name +
                         ") and the directory names no node there");
            }
        }

        // (2) and every node the directory names was reached. A cook is
        // produced from a wire whose node table is the pre-order from the root,
        // so nothing in it is unreachable — an entry the walk never met means
        // this side stopped following an edge the writer wrote.
        for (ulong e = 0; e < dir.Count; e++)
        {
            if (Walk.Find(dir.Offset(e)) < 0)
            {
                Fail.Now("the directory names a node at offset " + dir.Offset(e) + " (type id 0x" +
                         dir.Type(e).ToString("x16") + ") that the walk never reached");
            }
        }
        if ((ulong)Walk.reached.Count != dir.Count)
        {
            Fail.Now("the walk reached " + Walk.reached.Count + " nodes and the directory names " + dir.Count);
        }

        // (3) the directory's own shape, as §7.1 states it, checked against the
        // descriptors: the root first at offset zero, offsets ascending, each
        // node's storage fitting before the next entry.
        if (dir.Count == 0 || dir.Offset(0) != 0 || dir.Type(0) != Header.Fnv1a64(root.Type.Name))
        {
            Fail.Now("the directory's first entry is not the root at offset zero");
        }
        for (ulong e = 0; e < dir.Count; e++)
        {
            TableCookInfo type;
            if (!unitTables.TryGetValue(dir.Type(e), out type))
            {
                Fail.Now("the directory names a type id 0x" + dir.Type(e).ToString("x16") +
                         " no table in this unit has");
            }
            ulong start = dir.Offset(e);
            ulong end = e + 1 < dir.Count ? dir.Offset(e + 1) : dataLength;
            if (e + 1 < dir.Count && end <= start)
            {
                Fail.Now("the directory does not ascend at entry " + e);
            }
            if (end - start < (ulong)type.Size)
            {
                Fail.Now("node " + e + " (" + type.Name + ") has " + (end - start) +
                         " bytes before the next entry and needs " + type.Size);
            }
        }

        // (4) EVERY BYTE NO FIELD COVERS IS ZERO (§7.2). Not tidiness: a cooked
        // artifact is CONTENT-ADDRESSED by (asset hash, build version), so two
        // cooks of one wire have to be one artifact and one uninitialized pad
        // byte would make them two.
        for (ulong e = 0; e < dir.Count; e++)
        {
            TableCookInfo type = unitTables[dir.Type(e)];
            ulong used = dir.Offset(e) + (ulong)type.Size;
            ulong next = e + 1 < dir.Count ? dir.Offset(e + 1) : dataLength;
            for (ulong a = used; a < next; a++)
            {
                if (region[a] != 0)
                {
                    Fail.Now("the byte at region offset " + a + " covers no field and is 0x" +
                             region[a].ToString("x2") + ", not zero (SPEC-TABLES.md §7.2)");
                }
            }
        }

        if (asDump)
        {
            Console.Out.Write(text.ToString());
        }
        else
        {
            Console.WriteLine("cook golden lock: " + root.Name + " over " + path + " — " + Walk.reached.Count +
                              " nodes, every one at the offset and type the tool's directory names, " +
                              "every byte no field covers zero");
        }
        file.Destroy();
    }

    // ---- mode: fixedvalues — the VALUE crossing, three languages deep ----
    //
    // A FIXED table has no pointer, so it has no node table and no kind 17: the
    // C++ backend's wire and the tool's are the SAME BYTES for one. So the
    // chain is C++ writes the wire -> `schema cook` cooks it, from its own
    // reading of that wire and its own model of the record's layout -> THIS
    // side opens the cook and reads the fields back. Three implementations, and
    // the values must be the ones the first one wrote.

    const int SettingsQuality = 3;
    const string SettingsLabel = "cooked-fixed";
    const int StampSeq = 907;
    const string StampTag = "stamped";

    static void ModeFixedValues(Root root, string path)
    {
        byte[] source = WholeFile(path);
        Fail.Describe("fixedvalues " + root.Name + " over " + path);
        Native.File file = Native.Place(source, source.Length, 0, AlignmentOf(source));

        if (root.Name == "Settings")
        {
            SettingsCook cook;
            if (!SettingsCook.Open(out cook, (IntPtr)file.Base, file.Length))
            {
                Fail.Now("the Settings cook did not open");
            }
            SettingsRow* row = cook.RootPointer;
            Check(row->Quality == SettingsQuality, "Settings.quality is " + row->Quality + ", not " + SettingsQuality);
            CheckText(SettingsCook.Label(row), SettingsLabel, "Settings.label");
            Check(row->LabelLength == SettingsLabel.Length,
                  "Settings.label used length is " + row->LabelLength + ", not " + SettingsLabel.Length);
            CheckZeroTail(row->Label, row->LabelLength, 16, "Settings.label");
        }
        else if (root.Name == "Stamp")
        {
            StampCook cook;
            if (!StampCook.Open(out cook, (IntPtr)file.Base, file.Length))
            {
                Fail.Now("the Stamp cook did not open");
            }
            StampRow* row = cook.RootPointer;
            Check(row->Seq == StampSeq, "Stamp.seq is " + row->Seq + ", not " + StampSeq);
            CheckText(StampCook.Tag(row), StampTag, "Stamp.tag");
            Check(row->TagLength == StampTag.Length,
                  "Stamp.tag used length is " + row->TagLength + ", not " + StampTag.Length);
            CheckZeroTail(row->Tag, row->TagLength, 8, "Stamp.tag");
        }
        else
        {
            Fail.Now("the value crossing covers the FIXED roots only, and was asked for " + root.Name);
        }

        Console.WriteLine("cook value crossing: " + root.Name + " over " + path +
                          " — the C++ side wrote the wire, the tool cooked it, and this side reads every value " +
                          "back, zero tail included (SPEC-TABLES.md §7.2, §7.5)");
        file.Destroy();
    }

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Fail.Now(what);
        }
    }

    static void CheckText(ReadOnlySpan<byte> got, string want, string what)
    {
        if (got.Length != want.Length)
        {
            Fail.Now(what + " is " + got.Length + " bytes, not " + want.Length);
        }
        for (int i = 0; i < want.Length; i++)
        {
            if (got[i] != (byte)want[i])
            {
                Fail.Now(what + " differs at byte " + i);
            }
        }
    }

    // EVERY BYTE NO FIELD COVERS IS ZERO (§7.2), and a string's unused tail is
    // one of them — checked here because it is a fact of the region the value
    // read would otherwise walk straight past.
    static void CheckZeroTail(byte* buffer, int used, int declared, string what)
    {
        for (int i = used; i <= declared; i++)
        {
            if (buffer[i] != 0)
            {
                Fail.Now(what + "'s zero tail is not zero at byte " + i);
            }
        }
    }

    // ---- mode: usage — the documented surface, compiled and run ----

    static int UsageExample(IntPtr bytes, long length)
    {
        // in the game — mmap the file or read it, then just point. Nothing is
        // parsed, nothing is allocated, and nothing is walked.
        // the region must stay put and stay ALIGNED for as long as the handle,
        // or anything reached through it, is used: nothing here copies, and
        // nothing here pins
        SceneCook scene;
        if (!SceneCook.Open(out scene, bytes, length))
        {
            // wrong build, corrupt, truncated, or a foreign byte order: fall
            // back to a wire load, which is the path that carries every version
            return 0;
        }

        // a string is a SPAN over the region — no copy, no allocation — and a
        // reference is one add through <T>Cook.At, which takes the SLOT because
        // the delta is relative to the slot's own address
        SceneRow* root = scene.RootPointer;
        Console.WriteLine(Encoding.UTF8.GetString(SceneCook.Name(root)) + " v" + root->Version);
        int nodes = 0;
        for (ListNodeRow* n = ListNodeCook.At(&root->Head); n != null; n = ListNodeCook.At(&n->Next))
        {
            nodes++;
            // USAGE prints each node here; the gate only has to READ the same
            // bytes, and a hundred lines of chain would drown the run's verdict
            sink += (ulong)(n->Value + ListNodeCook.Name(n).Length);
        }
        return nodes;
    }

    static ulong sink;

    static void ModeUsage(Root root, string path)
    {
        if (root.Name != "Scene")
        {
            Fail.Now("USAGE's example is written against Scene, and was asked for " + root.Name);
        }
        byte[] source = WholeFile(path);
        Fail.Describe("usage over " + path);
        Native.File file = Native.Place(source, source.Length, 0, AlignmentOf(source));
        int nodes = UsageExample((IntPtr)file.Base, file.Length);
        if (nodes <= 0)
        {
            Fail.Now("USAGE's example did not open the cook, or found no chain in it");
        }
        Console.WriteLine("cook usage example: USAGE.md's C# compiles and runs — " + nodes + " chain nodes off Scene.head");
        file.Destroy();
    }

    // ---- mode: forge — the directed battery ----

    static int forged;
    static int refused;

    static void ExpectRefusal(Root root, byte[] source, long claim, int lead, long at, int width, ulong value, string what)
    {
        Native.File file = Native.Place(source, claim, lead, AlignmentOf(source));
        if (at >= 0 && at + width <= claim)
        {
            if (width == 8)
            {
                Header.Write64(file.Base + at, value);
            }
            else
            {
                for (int i = 0; i < width; i++)
                {
                    file.Base[at + i] = (byte)(value >> (8 * i));
                }
            }
        }
        Fail.Describe(what);
        forged++;
        if (root.Open((IntPtr)file.Base, file.Length) != IntPtr.Zero)
        {
            Fail.Now("a forgery OPENED that §7 says Open refuses: " + what);
        }
        refused++;
        file.Destroy();
    }

    static void ExpectOpen(Root root, byte[] source, long at, int width, ulong value, string what)
    {
        Native.File file = Native.Place(source, source.Length, 0, AlignmentOf(source));
        if (at >= 0)
        {
            for (int i = 0; i < width; i++)
            {
                file.Base[at + i] = (byte)(value >> (8 * i));
            }
        }
        Fail.Describe(what);
        forged++;
        IntPtr opened = root.Open((IntPtr)file.Base, file.Length);
        if (opened == IntPtr.Zero)
        {
            Fail.Now("Open REFUSED an edit §7 says it does not check: " + what);
        }
        // and it must have read nothing but the header to decide: the answer is
        // the same as the unmutated file's, which is what O(1) means as a
        // property rather than as a timing
        sink += *(byte*)opened;
        file.Destroy();
    }

    static void ModeForge(Root root, string path)
    {
        byte[] source = WholeFile(path);
        long length = source.Length;
        ulong alignment = Header.Read64(source, Header.WordAlignment);
        ulong dataLength = Header.Read64(source, Header.WordDataLength);
        ulong attribution = Header.Read64(source, Header.WordAttributionLength);
        long offset = (long)Header.DataOffset(alignment);
        forged = 0;
        refused = 0;

        // it opens unforged, so a green run is not a reader that refuses
        // everything
        {
            Native.File file = Native.Place(source, length, 0, (long)alignment);
            Fail.Describe("the valid cook, unforged");
            if (root.Open((IntPtr)file.Base, file.Length) == IntPtr.Zero)
            {
                Fail.Now("the valid cook did not open");
            }
            file.Destroy();
        }

        // THE MAGIC, bytewise: every byte of it, one at a time, and the whole
        // word byte-reversed — which is a cook of the OTHER byte order and
        // refuses here rather than reaching a fix-up pass.
        for (int b = 0; b < 8; b++)
        {
            ulong m = Header.Read64(source, Header.WordMagic);
            ulong mask = 0xffUL << (8 * b);
            ExpectRefusal(root, source, length, 0, Header.WordMagic, 8, m ^ mask, "one byte of the magic");
        }
        {
            ulong m = Header.Read64(source, Header.WordMagic);
            ulong swapped = 0;
            for (int i = 0; i < 8; i++)
            {
                swapped |= ((m >> (8 * i)) & 0xff) << (8 * (7 - i));
            }
            ExpectRefusal(root, source, length, 0, Header.WordMagic, 8, swapped,
                          "the magic byte-reversed: a cook of the other byte order");
        }
        ExpectRefusal(root, source, length, 0, Header.WordMagic, 8, 0x4b4c42414d484353UL,
                      "the BLOCK form's magic: a block where a cook was written");

        // THE BYTE ORDER word, which the magic has already agreed with
        for (ulong v = 0; v < 4; v++)
        {
            if (v == Header.Read64(source, Header.WordByteOrder))
            {
                continue;
            }
            ExpectRefusal(root, source, length, 0, Header.WordByteOrder, 8, v, "the byte-order word");
        }

        // THE BUILD VERSION: the sole guard between a runtime and a foreign region
        ExpectRefusal(root, source, length, 0, Header.WordBuildVersion, 8, 0, "a zero build version");
        ExpectRefusal(root, source, length, 0, Header.WordBuildVersion, 8, Schema.BuildVersion ^ 1UL,
                      "a build version one bit away from this build's");
        ExpectRefusal(root, source, length, 0, Header.WordBuildVersion, 8, ulong.MaxValue, "a saturated build version");

        // THE RESERVED WORDS
        ExpectRefusal(root, source, length, 0, Header.WordReserved0, 8, 1, "the first reserved word non-zero");
        ExpectRefusal(root, source, length, 0, Header.WordReserved1, 8, 1, "the second reserved word non-zero");
        ExpectRefusal(root, source, length, 0, Header.WordReserved0, 8, ulong.MaxValue, "the first reserved word saturated");

        // THE ALIGNMENT WORD, which the rest of the check does arithmetic with
        ulong[] badAlignments = { 0, 1, 2, 3, 4, 5, 6, 7, 12, 24, 128, 1UL << 63, ulong.MaxValue };
        foreach (ulong bad in badAlignments)
        {
            ExpectRefusal(root, source, length, 0, Header.WordAlignment, 8, bad,
                          "an alignment word that is not a region's alignment");
        }

        // THE TWO PART LENGTHS, against the length the caller passed
        ExpectRefusal(root, source, length, 0, Header.WordDataLength, 8, dataLength + 1, "a data length one byte long");
        ExpectRefusal(root, source, length, 0, Header.WordDataLength, 8, dataLength - 1, "a data length one byte short");
        ExpectRefusal(root, source, length, 0, Header.WordDataLength, 8, ulong.MaxValue, "a saturated data length");
        ExpectRefusal(root, source, length, 0, Header.WordDataLength, 8, (ulong)root.Size - 1,
                      "a data part too short to hold the root, and one byte off the total too");

        // A DATA PART TOO SHORT TO HOLD THE ROOT, with the file's TOTAL kept
        // exact so nothing but the root-fits check can refuse it.
        {
            Native.File file = Native.Place(source, length, 0, (long)alignment);
            Header.Write64(file.Base + Header.WordDataLength, 8);
            Header.Write64(file.Base + Header.WordAttributionLength, (ulong)(length - offset - 8));
            Fail.Describe("a data part of eight bytes, with the attribution length made up so the total still matches");
            forged++;
            if (root.Open((IntPtr)file.Base, file.Length) != IntPtr.Zero)
            {
                Fail.Now("a data part too short to hold the root OPENED, and its root's storage is outside the file");
            }
            refused++;
            file.Destroy();
        }
        ExpectRefusal(root, source, length, 0, Header.WordDataLength, 8, 0, "an empty data part");
        ExpectRefusal(root, source, length, 0, Header.WordAttributionLength, 8, attribution + 1,
                      "an attribution length one byte long");
        ExpectRefusal(root, source, length, 0, Header.WordAttributionLength, 8, attribution - 1,
                      "an attribution length one byte short");
        ExpectRefusal(root, source, length, 0, Header.WordAttributionLength, 8, ulong.MaxValue,
                      "a saturated attribution length");

        // TRUNCATION AND EXTENSION are one refusal
        long[] claims = { 0, 1, 8, 63, 64, 65 };
        foreach (long claim in claims)
        {
            ExpectRefusal(root, source, claim, 0, -1, 0, 0, "a truncated file");
        }
        ExpectRefusal(root, source, length - 1, 0, -1, 0, 0, "one byte short");
        ExpectRefusal(root, source, length + 1, 0, -1, 0, 0, "one trailing byte");
        ExpectRefusal(root, source, offset, 0, -1, 0, 0, "the header alone");
        ExpectRefusal(root, source, offset + (long)dataLength, 0, -1, 0, 0,
                      "the data part with the attribution cut off, and the header still claiming it");

        // AN UNALIGNED BASE
        for (int lead = 1; lead < 64; lead++)
        {
            if (((ulong)lead % alignment) == 0)
            {
                continue;
            }
            ExpectRefusal(root, source, length, lead, -1, 0, 0, "an unaligned base");
        }

        // A NULL POINTER, which is the caller's own error
        Fail.Describe("a NULL buffer");
        forged++;
        if (root.Open(IntPtr.Zero, length) != IntPtr.Zero)
        {
            Fail.Now("Open accepted a NULL buffer");
        }
        refused++;

        // AND THE OTHER HALF: what §7 says Open does NOT check. Each of these
        // OPENS, because Open reads the header and points and that is the whole
        // check — and each is a refusal `schema cook-check` owns instead (§7.4).
        if (dataLength >= (ulong)(offset + 8))
        {
            ExpectOpen(root, source, offset + root.Size - 8, 8, ulong.MaxValue / 2,
                       "a reference slot with an enormous forward delta — cook-check's refusal, not Open's");
            ExpectOpen(root, source, offset + root.Size - 8, 8, (ulong)(-(offset + 4096)),
                       "a negative delta past the base — cook-check's refusal, not Open's");
        }
        ExpectOpen(root, source, offset + (long)dataLength, 8, ulong.MaxValue,
                   "a directory entry naming an offset outside the region — the attribution is not read at open");

        Console.WriteLine("cook forgery battery: " + root.Name + " over " + path + " — " + forged + " forgeries, " +
                          refused + " refused, and the ones §7 hands to cook-check opened");
    }

    // ---- mode: fuzz — the seeded forgery fuzzer ----

    struct Rng
    {
        public ulong State;
        public ulong Next()
        {
            State += 0x9e3779b97f4a7c15UL;
            ulong z = State;
            z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9UL;
            z = (z ^ (z >> 27)) * 0x94d049bb133111ebUL;
            return z ^ (z >> 31);
        }
        public ulong Below(ulong n) { return n == 0 ? 0 : Next() % n; }
    }

    static ulong Mix(ulong a, ulong b)
    {
        ulong z = a + 0x9e3779b97f4a7c15UL * (b + 1);
        z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9UL;
        z = (z ^ (z >> 27)) * 0x94d049bb133111ebUL;
        return z ^ (z >> 31);
    }

    static void ModeFuzz(Root root, string path, ulong seed, long mutants)
    {
        byte[] source = WholeFile(path);
        long length = source.Length;
        ulong alignment = Header.Read64(source, Header.WordAlignment);
        ulong dataLength = Header.Read64(source, Header.WordDataLength);
        long offset = (long)Header.DataOffset(alignment);
        byte[] scratch = new byte[root.Size];

        long opened = 0;
        long headerMutants = 0;
        long dataMutants = 0;

        for (long k = 0; k < mutants; k++)
        {
            Rng rng;
            rng.State = Mix(seed, (ulong)k);

            // one of three axes, so every pass gets coverage rather than the
            // largest one swallowing the budget
            ulong axis = rng.Below(3);
            long claim = length;
            int lead = 0;
            if (axis == 2)
            {
                claim = (long)rng.Below((ulong)length + 65);
                if (rng.Below(4) == 0)
                {
                    lead = 1 + (int)rng.Below(63);
                }
            }

            Native.File file = Native.Place(source, claim, lead, (long)alignment);

            bool headerTouched = axis != 2 || claim != length || lead != 0;
            if (axis == 0)
            {
                // the HEADER: a boundary value into one of its words, or a byte flip
                ulong word = rng.Below(8) * 8;
                ulong[] values =
                {
                    0, 1, 2, 3, 7, 8, 15, 16, 63, 64, 65,
                    0x7fffffffUL, 0x80000000UL, 0xffffffffUL,
                    1UL << 62, 1UL << 63, ulong.MaxValue - 1, ulong.MaxValue,
                };
                if ((long)word + 8 <= claim)
                {
                    if (rng.Below(2) == 0)
                    {
                        Header.Write64(file.Base + word, values[rng.Below((ulong)values.Length)]);
                    }
                    else
                    {
                        file.Base[word + rng.Below(8)] ^= (byte)(1u << (int)rng.Below(8));
                    }
                }
                headerMutants++;
            }
            else if (axis == 1 && claim > offset)
            {
                // the DATA part: Open's answer must not move, because Open
                // reads no byte of it. This is the O(1) promise as a property,
                // not a timing.
                long a = offset + (long)rng.Below((ulong)(claim - offset));
                file.Base[a] ^= (byte)(1u << (int)rng.Below(8));
                headerTouched = false;
                dataMutants++;
            }

            Fail.Describe("fuzz " + root.Name + " seed=" + seed + " k=" + k + " axis=" + axis +
                          " claim=" + claim + " lead=" + lead);

            IntPtr result = root.Open((IntPtr)file.Base, file.Length);

            if (!headerTouched && result == IntPtr.Zero)
            {
                Fail.Now("a mutation INSIDE THE DATA PART changed Open's answer — Open read a byte it must not");
            }

            if (result != IntPtr.Zero)
            {
                opened++;
                // and what it handed back is inside what the caller passed: the
                // root's whole storage, actually READ, so the guard page proves
                // it to the byte rather than the oracle computing that it does
                byte* at = (byte*)result;
                if (at < file.Base || at + root.Size > file.Base + file.Length)
                {
                    Fail.Now("an opened cook's root storage leaves the length the caller passed");
                }
                for (long i = 0; i < root.Size; i++)
                {
                    scratch[i] = at[i];
                }
                sink += (ulong)(scratch[0] + scratch[root.Size - 1]);

                if (axis == 0 && claim == length && lead == 0)
                {
                    // a HEADER mutation that opened: the data part is still
                    // this build's own bytes, so the whole graph must still
                    // agree with the directory, exactly as in the golden mode
                    ulong openedAlignment = Header.Read64(file.Base + Header.WordAlignment);
                    ulong openedData = Header.Read64(file.Base + Header.WordDataLength);
                    if (openedAlignment == alignment && openedData == dataLength)
                    {
                        Walk.Run(root.Type, file.Base + offset, dataLength, null);
                    }
                }
            }

            file.Destroy();
        }

        Console.WriteLine("cook forgery fuzzer: " + root.Name + " over " + path + " — " + mutants + " mutants (" +
                          headerMutants + " header, " + dataMutants + " data), " + opened + " opened, " +
                          "none read past the length the caller passed (SPEC-TABLES.md §7, §7.5)");
    }

    // ---- mode: time — the O(1) bar ----

    static double MedianOpenNs(Root root, Native.File file, long iterations)
    {
        const int runs = 9;
        double[] samples = new double[runs];
        for (int r = 0; r < runs; r++)
        {
            long start = Stopwatch.GetTimestamp();
            for (long i = 0; i < iterations; i++)
            {
                IntPtr p = root.Open((IntPtr)file.Base, file.Length);
                sink += p != IntPtr.Zero ? 1UL : 0UL;
            }
            long end = Stopwatch.GetTimestamp();
            samples[r] = (double)(end - start) * 1e9 / Stopwatch.Frequency / iterations;
        }
        Array.Sort(samples);
        return samples[runs / 2];
    }

    static void ModeTime(Root root, string smallPath, string largePath)
    {
        byte[] smallSource = WholeFile(smallPath);
        byte[] largeSource = WholeFile(largePath);
        Native.File smallFile = Native.Place(smallSource, smallSource.Length, 0, AlignmentOf(smallSource));
        Native.File largeFile = Native.Place(largeSource, largeSource.Length, 0, AlignmentOf(largeSource));
        Fail.Describe("time " + root.Name + " over " + smallPath + " and " + largePath);

        if (root.Open((IntPtr)smallFile.Base, smallFile.Length) == IntPtr.Zero ||
            root.Open((IntPtr)largeFile.Base, largeFile.Length) == IntPtr.Zero)
        {
            Fail.Now("one of the two fixtures did not open");
        }

        const long iterations = 200000;
        // warm both — a tiered runtime measured before it settles measures
        // tier-up and not codegen — then interleave, so a machine that drifts
        // drifts under both arms
        MedianOpenNs(root, smallFile, iterations);
        MedianOpenNs(root, largeFile, iterations);
        double smallNs = MedianOpenNs(root, smallFile, iterations);
        double largeNs = MedianOpenNs(root, largeFile, iterations);

        double ratio = largeNs / smallNs;
        Console.WriteLine("cook open is O(1) in the file's size: " + root.Name + " at " + smallSource.Length +
                          " bytes opens in " + smallNs.ToString("F1") + " ns, at " + largeSource.Length +
                          " bytes in " + largeNs.ToString("F1") + " ns (medians of 9 x " + iterations +
                          ", paired, one sitting) — ratio " + ratio.ToString("F3"));

        // The bar is FLAT, and flat is stated as a band rather than as
        // equality: a header match is tens of nanoseconds, where the scheduler
        // and the cache are the whole variance. A walk of any shape over a
        // hundred-megabyte region would be five orders of magnitude out, so
        // this band cannot pass one by accident.
        if (ratio > 2.0 || ratio < 0.5)
        {
            Fail.Now("open time is not flat across the two sizes: ratio " + ratio.ToString("F3"));
        }

        smallFile.Destroy();
        largeFile.Destroy();
    }

    // ---- modes: accept / refuse — the byte-order leg ----

    static void ModeOrder(Root root, string path, bool expectAccept)
    {
        byte[] source = WholeFile(path);
        Fail.Describe((expectAccept ? "accept " : "refuse ") + root.Name + " over " + path);
        Native.File file = Native.Place(source, source.Length, 0, AlignmentOf(source));
        IntPtr opened = root.Open((IntPtr)file.Base, file.Length);
        if (expectAccept)
        {
            if (opened == IntPtr.Zero)
            {
                Fail.Now("a cook produced for THIS build's byte order did not open");
            }
            ulong order = Header.Read64(file.Base + Header.WordByteOrder);
            if (order != 1 && order != 2)
            {
                Fail.Now("the byte-order word is " + order);
            }
            Console.WriteLine("cook byte-order leg: a cook written in this build's order (" +
                              (order == 1 ? "little" : "big") + ") opens natively");
        }
        else
        {
            if (opened != IntPtr.Zero)
            {
                Fail.Now("a cook of the OTHER byte order opened — the magic is what refuses it");
            }
            // AND A C# BIG-ENDIAN CONSUMER IS UNPROVEN, stated rather than
            // implied (SPEC-TABLES.md §7.5): there is no big-endian .NET, so
            // this leg proves the REFUSAL and nothing about a native open of a
            // big-endian cook. The C++ leg proves that half on s390x.
            Console.WriteLine("cook byte-order leg: a cook of the other byte order is refused by the MAGIC, read " +
                              "bytewise — and a big-endian C# CONSUMER stays unproven until a big-endian .NET exists");
        }
        file.Destroy();
    }

    // ---- the layout contract, at start-up rather than at first open ----

    static void ModeLayout()
    {
        Fail.Describe("the layout contract");
        TableCookLayout.Verify();
        Console.WriteLine("cook layout contract: every cooked record's size and every field's offset agree with the " +
                          "compiler's model in this runtime (SPEC-TABLES.md §19.3, §20.3)");
    }

    static int Main(string[] args)
    {
        if (args.Length < 1)
        {
            Console.WriteLine("usage: schemacooktest golden|dump|fixedvalues|usage|forge|fuzz|time|accept|refuse|layout <root> <file> [file]");
            return 1;
        }
        string mode = args[0];
        if (mode == "layout")
        {
            ModeLayout();
            return 0;
        }
        if (args.Length < 3)
        {
            Console.WriteLine("usage: schemacooktest <mode> <root> <file> [file]");
            return 1;
        }
        Root root = RootNamed(args[1]);
        switch (mode)
        {
            case "golden": ModeGolden(root, args[2], false); break;
            case "dump": ModeGolden(root, args[2], true); break;
            case "fixedvalues": ModeFixedValues(root, args[2]); break;
            case "usage": ModeUsage(root, args[2]); break;
            case "forge": ModeForge(root, args[2]); break;
            case "fuzz":
            {
                ulong seed = 0xc00c1e5eedUL;
                long mutants = 20000;
                string s = Environment.GetEnvironmentVariable("SEED");
                string n = Environment.GetEnvironmentVariable("N");
                if (!string.IsNullOrEmpty(s)) { seed = ParseNumber(s); }
                if (!string.IsNullOrEmpty(n)) { mutants = (long)ParseNumber(n); }
                ModeFuzz(root, args[2], seed, mutants);
                break;
            }
            case "time":
                if (args.Length < 4)
                {
                    Console.WriteLine("FAILED: time wants two cooks");
                    return 1;
                }
                ModeTime(root, args[2], args[3]);
                break;
            case "accept": ModeOrder(root, args[2], true); break;
            case "refuse": ModeOrder(root, args[2], false); break;
            default:
                Console.WriteLine("FAILED: unknown mode " + mode);
                return 1;
        }
        return 0;
    }

    static ulong ParseNumber(string s)
    {
        if (s.StartsWith("0x", StringComparison.Ordinal) || s.StartsWith("0X", StringComparison.Ordinal))
        {
            return Convert.ToUInt64(s.Substring(2), 16);
        }
        return Convert.ToUInt64(s, 10);
    }
}
