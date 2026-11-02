using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using G = Graphdemo;
using L = Listdemo;
using R1 = Tblr1;
using V1 = Tblv1;

// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): a read that meets
// damage refuses as a whole, the report says malformed, and nothing it decoded
// is handed back. Every C# load entry point is held to it: the managed load,
// the native region load, the retaining load (its store emptied), the builder
// load (its root back at its declared defaults), and the text read.
//
// Two sources of damage. Every damaged FRAMING vector the conformance manifest
// names (testdata/wire/tables/framing, docs/LAYERS/1-framing.md) through every
// entry point its root has; and a body damaged INSIDE a nested body with whole
// fields before and after it, the case where a reader that kept what it
// decoded would still hand back a value.
static unsafe partial class Program
{
    const string Discard = "damage discards the whole load: ";

    static void TestDamageDiscardsTheWholeLoad()
    {
        DiscardFramingVectors();
        DiscardNestedFixed();
        DiscardNestedVariable();
        DiscardText();
    }

    static string DiscardManifest()
    {
        string dir = goldenDir;
        for (int i = 0; i < 6 && dir != null; i++, dir = Path.GetDirectoryName(dir))
        {
            string candidate = Path.Combine(dir, "testdata", "conformance", "tables", "MANIFEST.txt");
            if (File.Exists(candidate)) { return candidate; }
            candidate = Path.Combine(dir, "conformance", "tables", "MANIFEST.txt");
            if (File.Exists(candidate)) { return candidate; }
        }
        return null;
    }

    static void DiscardFramingVectors()
    {
        string manifest = DiscardManifest();
        Check(manifest != null, Discard + "the conformance manifest is found");
        if (manifest == null) { return; }
        string root = Path.GetDirectoryName(Path.GetDirectoryName(Path.GetDirectoryName(Path.GetDirectoryName(manifest))));
        int run = 0;
        foreach (string line in File.ReadAllLines(manifest))
        {
            string[] f = line.Split((char[])null, StringSplitOptions.RemoveEmptyEntries);
            if (f.Length < 6 || f[0] != "framing" || f[5] != "malformed") { continue; }
            string name = f[1], key = f[2] + " " + f[3];
            byte[] wire = File.ReadAllBytes(Path.Combine(root, f[4]));
            switch (key)
            {
                case "tblr1 Cfg": DiscardR1Cfg(name, wire); break;
                case "listdemo Squad": DiscardSquad(name, wire); break;
                case "listdemo Army": DiscardArmy(name, wire); break;
                case "listdemo Ints": DiscardInts(name, wire); break;
                case "listdemo Bounded": DiscardBounded(name, wire); break;
                case "graphdemo ListNode": DiscardListNode(name, wire); break;
                default: Check(false, Discard + name + ": no entry point for root " + key); continue;
            }
            run++;
        }
        Check(run >= 50, Discard + "every damaged framing vector ran (" + run + ")");
    }

    static string R1Json(R1.Cfg v)
    {
        byte[] text = new byte[R1.Schema.CfgToJsonMeasure(v)];
        R1.Schema.CfgToJson(v, text);
        return Encoding.UTF8.GetString(text);
    }

    static void DiscardR1Cfg(string name, byte[] wire)
    {
        R1.Cfg v = new R1.Cfg();
        string defaults = R1Json(v);
        var report = new R1.TableReport();
        Check(!R1.Schema.CfgLoad(v, wire, report) && report.Malformed, Discard + name + ": managed load answers false, malformed");
        Check(R1Json(v) == defaults, Discard + name + ": managed load hands back the declared defaults");
    }

    // The native region, retaining and builder loads of one variable root, held
    // the same way for every root: no root pointer, an empty store, a builder
    // that answers false with its root at the declared defaults.
    delegate IntPtr RegionLoad(IntPtr region, long capacity, byte[] wire, object report);

    static void DiscardSquad(string name, byte[] wire)
    {
        L.Squad v = new L.Squad();
        string defaults = Json(v);
        var report = new L.TableReport();
        Check(!L.Schema.SquadLoad(v, wire, report) && report.Malformed, Discard + name + ": managed load answers false, malformed");
        Check(Json(v) == defaults, Discard + name + ": managed load hands back the declared defaults");
        DiscardNative(name, wire,
            (r, c, w, rep) => (IntPtr)L.Schema.SquadLoad(r, c, w, (L.TableReport)rep),
            (IntPtr r, long c, byte[] w, ref L.TableRetain s, L.TableReport rep) => (IntPtr)L.Schema.SquadLoadRetain(r, c, w, ref s, rep));
        using (var builder = new L.SquadBuilder())
        {
            var b = new L.TableReport();
            Check(!L.Schema.SquadLoadBuilder(builder, wire, b) && b.Malformed, Discard + name + ": builder load answers false, malformed");
            Check(builder.GetRoot()->Name == 0, Discard + name + ": builder root is back at its declared defaults");
        }
    }
    static string Json(L.Squad v) { byte[] t = new byte[L.Schema.SquadToJsonMeasure(v)]; L.Schema.SquadToJson(v, t); return Encoding.UTF8.GetString(t); }

    static void DiscardArmy(string name, byte[] wire)
    {
        L.Army v = new L.Army();
        byte[] t0 = new byte[L.Schema.ArmyToJsonMeasure(v)]; L.Schema.ArmyToJson(v, t0);
        var report = new L.TableReport();
        Check(!L.Schema.ArmyLoad(v, wire, report) && report.Malformed, Discard + name + ": managed load answers false, malformed");
        byte[] t1 = new byte[L.Schema.ArmyToJsonMeasure(v)]; L.Schema.ArmyToJson(v, t1);
        Check(t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
        DiscardNative(name, wire,
            (r, c, w, rep) => (IntPtr)L.Schema.ArmyLoad(r, c, w, (L.TableReport)rep),
            (IntPtr r, long c, byte[] w, ref L.TableRetain s, L.TableReport rep) => (IntPtr)L.Schema.ArmyLoadRetain(r, c, w, ref s, rep));
        using (var builder = new L.ArmyBuilder())
        {
            var b = new L.TableReport();
            Check(!L.Schema.ArmyLoadBuilder(builder, wire, b) && b.Malformed, Discard + name + ": builder load answers false, malformed");
        }
    }

    static void DiscardInts(string name, byte[] wire)
    {
        L.Ints v = new L.Ints();
        byte[] t0 = new byte[L.Schema.IntsToJsonMeasure(v)]; L.Schema.IntsToJson(v, t0);
        var report = new L.TableReport();
        Check(!L.Schema.IntsLoad(v, wire, report) && report.Malformed, Discard + name + ": managed load answers false, malformed");
        byte[] t1 = new byte[L.Schema.IntsToJsonMeasure(v)]; L.Schema.IntsToJson(v, t1);
        Check(t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
        DiscardNative(name, wire,
            (r, c, w, rep) => (IntPtr)L.Schema.IntsLoad(r, c, w, (L.TableReport)rep),
            (IntPtr r, long c, byte[] w, ref L.TableRetain s, L.TableReport rep) => (IntPtr)L.Schema.IntsLoadRetain(r, c, w, ref s, rep));
        using (var builder = new L.IntsBuilder())
        {
            var b = new L.TableReport();
            Check(!L.Schema.IntsLoadBuilder(builder, wire, b) && b.Malformed, Discard + name + ": builder load answers false, malformed");
        }
    }

    static void DiscardBounded(string name, byte[] wire)
    {
        L.Bounded v = new L.Bounded();
        byte[] t0 = new byte[L.Schema.BoundedToJsonMeasure(v)]; L.Schema.BoundedToJson(v, t0);
        var report = new L.TableReport();
        Check(!L.Schema.BoundedLoad(v, wire, report) && report.Malformed, Discard + name + ": managed load answers false, malformed");
        byte[] t1 = new byte[L.Schema.BoundedToJsonMeasure(v)]; L.Schema.BoundedToJson(v, t1);
        Check(t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
    }

    static void DiscardListNode(string name, byte[] wire)
    {
        G.ListNode v = new G.ListNode();
        byte[] t0 = new byte[G.Schema.ListNodeToJsonMeasure(v)]; G.Schema.ListNodeToJson(v, t0);
        var report = new G.TableReport();
        Check(!G.Schema.ListNodeLoad(v, wire, report) && report.Malformed, Discard + name + ": managed load answers false, malformed");
        byte[] t1 = new byte[G.Schema.ListNodeToJsonMeasure(v)]; G.Schema.ListNodeToJson(v, t1);
        Check(t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
        DiscardNativeG(name, wire,
            (r, c, w, rep) => (IntPtr)G.Schema.ListNodeLoad(r, c, w, rep),
            (IntPtr r, long c, byte[] w, ref G.TableRetain s, G.TableReport rep) => (IntPtr)G.Schema.ListNodeLoadRetain(r, c, w, ref s, rep));
        using (var builder = new G.ListNodeBuilder())
        {
            var b = new G.TableReport();
            Check(!G.Schema.ListNodeLoadBuilder(builder, wire, b) && b.Malformed, Discard + name + ": builder load answers false, malformed");
            Check(builder.GetRoot()->Value == 0 && builder.GetRoot()->Next == 0, Discard + name + ": builder root is back at its declared defaults");
        }
    }

    delegate IntPtr LRetainLoad(IntPtr region, long capacity, byte[] wire, ref L.TableRetain store, L.TableReport report);
    delegate IntPtr GRetainLoad(IntPtr region, long capacity, byte[] wire, ref G.TableRetain store, G.TableReport report);

    const int DiscardRegion = 1 << 16;

    static void DiscardNative(string name, byte[] wire, RegionLoad load, LRetainLoad retainLoad)
    {
        byte* region = (byte*)NativeMemory.AlignedAlloc(DiscardRegion, 64);
        byte* bytes = (byte*)NativeMemory.Alloc(4096);
        L.TableRetain.Id* ids = (L.TableRetain.Id*)NativeMemory.Alloc((nuint)(64 * sizeof(L.TableRetain.Id)));
        try
        {
            var report = new L.TableReport();
            Check(load((IntPtr)region, DiscardRegion, wire, report) == IntPtr.Zero && report.Malformed, Discard + name + ": region load hands back no root, malformed");
            var store = new L.TableRetain { Bytes = bytes, Capacity = 4096, Ids = ids, IdCapacity = 64 };
            store.Used = 7; store.Count = 1;
            var r = new L.TableReport();
            Check(retainLoad((IntPtr)region, DiscardRegion, wire, ref store, r) == IntPtr.Zero && r.Malformed, Discard + name + ": retaining load hands back no root, malformed");
            Check(store.Used == 0 && store.Count == 0 && store.IdUsed == 0, Discard + name + ": retaining load leaves the store empty (used " + store.Used + ", count " + store.Count + ")");
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.Free(bytes); NativeMemory.Free(ids); }
    }

    delegate IntPtr GRegionLoad(IntPtr region, long capacity, byte[] wire, G.TableReport report);

    static void DiscardNativeG(string name, byte[] wire, GRegionLoad load, GRetainLoad retainLoad)
    {
        byte* region = (byte*)NativeMemory.AlignedAlloc(DiscardRegion, 64);
        byte* bytes = (byte*)NativeMemory.Alloc(4096);
        G.TableRetain.Id* ids = (G.TableRetain.Id*)NativeMemory.Alloc((nuint)(64 * sizeof(G.TableRetain.Id)));
        try
        {
            var report = new G.TableReport();
            Check(load((IntPtr)region, DiscardRegion, wire, report) == IntPtr.Zero && report.Malformed, Discard + name + ": region load hands back no root, malformed");
            var store = new G.TableRetain { Bytes = bytes, Capacity = 4096, Ids = ids, IdCapacity = 64 };
            store.Used = 7; store.Count = 1;
            var r = new G.TableReport();
            Check(retainLoad((IntPtr)region, DiscardRegion, wire, ref store, r) == IntPtr.Zero && r.Malformed, Discard + name + ": retaining load hands back no root, malformed");
            Check(store.Used == 0 && store.Count == 0 && store.IdUsed == 0, Discard + name + ": retaining load leaves the store empty (used " + store.Used + ", count " + store.Count + ")");
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.Free(bytes); NativeMemory.Free(ids); }
    }

    // A FIXED root damaged inside its nested body: `inner` carries a field of
    // kind 37, which no reader can skip, and the whole field `a` follows it.
    // The managed load must answer false, malformed, with `a` at its default 5.
    static void DiscardNestedFixed()
    {
        byte[] inner = new byte[] { 2, 37, 0 };
        byte[] body = Join(new byte[] { 1, 13 }, Var((ulong)inner.Length), inner, new byte[] { 3, 4 }, U32(42), new byte[] { 0 });
        byte[] wire = Fixture(body, "inner", "factor", "a");
        V1.Cfg v = new V1.Cfg();
        var report = new V1.TableReport();
        bool loaded = V1.Schema.CfgLoad(v, wire, report);
        Check(!loaded, Discard + "nested fixed: managed load answered true on damage");
        Check(report.Malformed, Discard + "nested fixed: the report does not say malformed");
        Check(v.A == 5, Discard + "nested fixed: a damaged load handed back a=" + v.A);
        Check(V1.Schema.CfgLoadVerdict(new V1.Cfg(), wire, new V1.TableReport()) == V1.Schema.TableWire.Verdict.Damaged, Discard + "nested fixed: the verdict is Damaged");
    }

    // A VARIABLE root damaged the same way: Scene's `ground` body carries
    // `depth` under kind 37, with `version` before it and `meta` after it.
    static void DiscardNestedVariable()
    {
        G.Scene source = new G.Scene { Version = 5 };
        source.Ground.Depth = 7;
        source.Meta.Build = 9;
        byte[] wire = new byte[G.Schema.SceneMeasure(source)];
        G.Schema.SceneSave(source, wire);
        int at = wire.AsSpan().IndexOf(new byte[] { 4, 7, 0, 0, 0 });
        Check(at > 0, Discard + "nested variable: ground's body is where this check expects it");
        if (at <= 0) { return; }
        wire[at] = 37;

        G.Scene v = new G.Scene();
        var report = new G.TableReport();
        Check(!G.Schema.SceneLoad(v, wire, report), Discard + "nested variable: managed load answered true on damage");
        Check(report.Malformed, Discard + "nested variable: the report does not say malformed");
        Check(v.Version == 1 && v.Meta.Build == 1 && v.Ground.Depth == 0, Discard + "nested variable: a damaged managed load handed back version=" + v.Version + " build=" + v.Meta.Build);

        byte* region = (byte*)NativeMemory.AlignedAlloc(DiscardRegion, 64);
        byte* bytes = (byte*)NativeMemory.Alloc(4096);
        G.TableRetain.Id* ids = (G.TableRetain.Id*)NativeMemory.Alloc((nuint)(64 * sizeof(G.TableRetain.Id)));
        try
        {
            var g = new G.TableReport();
            G.SceneRow* root = G.Schema.SceneLoad((IntPtr)region, DiscardRegion, wire, g);
            Check(root == null, Discard + "nested variable: region load handed back a root on damage" + (root != null ? ": version=" + root->Version : ""));
            Check(g.Malformed, Discard + "nested variable: the region report does not say malformed");
            var store = new G.TableRetain { Bytes = bytes, Capacity = 4096, Ids = ids, IdCapacity = 64 };
            var rr = new G.TableReport();
            Check(G.Schema.SceneLoadRetain((IntPtr)region, DiscardRegion, wire, ref store, rr) == null, Discard + "nested variable: retaining load handed back a root on damage");
            Check(rr.Malformed && store.Used == 0 && store.Count == 0, Discard + "nested variable: retaining load malformed=" + rr.Malformed + " used=" + store.Used + " count=" + store.Count);
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.Free(bytes); NativeMemory.Free(ids); }

        using (var builder = new G.SceneBuilder())
        {
            var b = new G.TableReport();
            Check(!G.Schema.SceneLoadBuilder(builder, wire, b), Discard + "nested variable: builder load answered true on damage");
            Check(b.Malformed, Discard + "nested variable: the builder report does not say malformed");
            Check(builder.GetRoot()->Version == 1 && builder.GetRoot()->Ground.Depth == 0, Discard + "nested variable: builder root handed back version=" + builder.GetRoot()->Version);
        }
    }

    // THE TEXT READ: a whole field, then a key the form reserves (§16.7), which
    // is damage. Nothing the walk placed before it is handed back.
    static void DiscardText()
    {
        byte[] text = Encoding.UTF8.GetBytes("{\"a\":42,\"&node\":1}");
        V1.Cfg v = new V1.Cfg();
        var report = new V1.TableReport();
        Check(!V1.Schema.CfgFromJson(v, text, report) && report.Malformed, Discard + "text: the read answered true on damage");
        Check(v.A == 5, Discard + "text: a damaged text read handed back a=" + v.A);
    }
}
