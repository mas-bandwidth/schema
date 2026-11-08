using System;
using System.Collections.Generic;
using System.IO;
using System.Runtime.InteropServices;
using System.Text;
using C2 = Cscollections2;
using G = Graphdemo;
using L = Listdemo;
using R1 = Tblr1;
using V1 = Tblv1;

// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): a read that meets
// damage refuses as a whole, the report says so, and nothing it decoded
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
        DiscardCycleVectors();
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
            if (f.Length < 6 || f[0] != "framing" || f[5] != "refused") { continue; }
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
        Check(run >= 50, Discard + "every refused framing vector ran (" + run + ")");
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
        Check(!R1.Schema.CfgLoad(v, wire, report) && (report.Damaged || report.Refused), Discard + name + ": managed load answers false, refused");
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
        Check(!L.Schema.SquadLoad(v, wire, report) && (report.Damaged || report.Refused), Discard + name + ": managed load answers false, refused");
        Check(Json(v) == defaults, Discard + name + ": managed load hands back the declared defaults");
        DiscardNative(name, wire,
            (r, c, w, rep) => (IntPtr)L.Schema.SquadLoad(r, c, w, (L.TableReport)rep),
            (IntPtr r, long c, byte[] w, ref L.TableRetain s, L.TableReport rep) => (IntPtr)L.Schema.SquadLoadRetain(r, c, w, ref s, rep));
        using (var builder = new L.SquadBuilder())
        {
            var b = new L.TableReport();
            Check(!L.Schema.SquadLoadBuilder(builder, wire, b) && (b.Damaged || b.Refused), Discard + name + ": builder load answers false, refused");
            Check(builder.GetRoot()->Name == 0, Discard + name + ": builder root is back at its declared defaults");
        }
    }
    static string Json(L.Squad v) { byte[] t = new byte[L.Schema.SquadToJsonMeasure(v)]; L.Schema.SquadToJson(v, t); return Encoding.UTF8.GetString(t); }

    static void DiscardArmy(string name, byte[] wire)
    {
        L.Army v = new L.Army();
        byte[] t0 = new byte[L.Schema.ArmyToJsonMeasure(v)]; L.Schema.ArmyToJson(v, t0);
        var report = new L.TableReport();
        Check(!L.Schema.ArmyLoad(v, wire, report) && (report.Damaged || report.Refused), Discard + name + ": managed load answers false, refused");
        byte[] t1 = new byte[L.Schema.ArmyToJsonMeasure(v)]; L.Schema.ArmyToJson(v, t1);
        Check(t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
        DiscardNative(name, wire,
            (r, c, w, rep) => (IntPtr)L.Schema.ArmyLoad(r, c, w, (L.TableReport)rep),
            (IntPtr r, long c, byte[] w, ref L.TableRetain s, L.TableReport rep) => (IntPtr)L.Schema.ArmyLoadRetain(r, c, w, ref s, rep));
        using (var builder = new L.ArmyBuilder())
        {
            var b = new L.TableReport();
            Check(!L.Schema.ArmyLoadBuilder(builder, wire, b) && (b.Damaged || b.Refused), Discard + name + ": builder load answers false, refused");
        }
    }

    static void DiscardInts(string name, byte[] wire)
    {
        L.Ints v = new L.Ints();
        byte[] t0 = new byte[L.Schema.IntsToJsonMeasure(v)]; L.Schema.IntsToJson(v, t0);
        var report = new L.TableReport();
        Check(!L.Schema.IntsLoad(v, wire, report) && (report.Damaged || report.Refused), Discard + name + ": managed load answers false, refused");
        byte[] t1 = new byte[L.Schema.IntsToJsonMeasure(v)]; L.Schema.IntsToJson(v, t1);
        Check(t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
        DiscardNative(name, wire,
            (r, c, w, rep) => (IntPtr)L.Schema.IntsLoad(r, c, w, (L.TableReport)rep),
            (IntPtr r, long c, byte[] w, ref L.TableRetain s, L.TableReport rep) => (IntPtr)L.Schema.IntsLoadRetain(r, c, w, ref s, rep));
        using (var builder = new L.IntsBuilder())
        {
            var b = new L.TableReport();
            Check(!L.Schema.IntsLoadBuilder(builder, wire, b) && (b.Damaged || b.Refused), Discard + name + ": builder load answers false, refused");
        }
    }

    static void DiscardBounded(string name, byte[] wire)
    {
        L.Bounded v = new L.Bounded();
        byte[] t0 = new byte[L.Schema.BoundedToJsonMeasure(v)]; L.Schema.BoundedToJson(v, t0);
        var report = new L.TableReport();
        Check(!L.Schema.BoundedLoad(v, wire, report) && (report.Damaged || report.Refused), Discard + name + ": managed load answers false, refused");
        byte[] t1 = new byte[L.Schema.BoundedToJsonMeasure(v)]; L.Schema.BoundedToJson(v, t1);
        Check(t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
    }

    static void DiscardListNode(string name, byte[] wire)
    {
        G.ListNode v = new G.ListNode();
        byte[] t0 = new byte[G.Schema.ListNodeToJsonMeasure(v)]; G.Schema.ListNodeToJson(v, t0);
        var report = new G.TableReport();
        Check(!G.Schema.ListNodeLoad(v, wire, report) && (report.Damaged || report.Refused), Discard + name + ": managed load answers false, refused");
        long m1 = G.Schema.ListNodeToJsonMeasure(v);
        byte[] t1 = m1 >= 0 ? new byte[m1] : Array.Empty<byte>();
        if (m1 >= 0) { G.Schema.ListNodeToJson(v, t1); }
        Check(m1 >= 0 && t0.AsSpan().SequenceEqual(t1), Discard + name + ": managed load hands back the declared defaults");
        DiscardNativeG(name, wire,
            (r, c, w, rep) => (IntPtr)G.Schema.ListNodeLoad(r, c, w, rep),
            (IntPtr r, long c, byte[] w, ref G.TableRetain s, G.TableReport rep) => (IntPtr)G.Schema.ListNodeLoadRetain(r, c, w, ref s, rep));
        using (var builder = new G.ListNodeBuilder())
        {
            var b = new G.TableReport();
            Check(!G.Schema.ListNodeLoadBuilder(builder, wire, b) && (b.Damaged || b.Refused), Discard + name + ": builder load answers false, refused");
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
            Check(load((IntPtr)region, DiscardRegion, wire, report) == IntPtr.Zero && (report.Damaged || report.Refused), Discard + name + ": region load hands back no root, refused");
            var store = new L.TableRetain { Bytes = bytes, Capacity = 4096, Ids = ids, IdCapacity = 64 };
            store.Used = 7; store.Count = 1;
            var r = new L.TableReport();
            Check(retainLoad((IntPtr)region, DiscardRegion, wire, ref store, r) == IntPtr.Zero && (r.Damaged || r.Refused), Discard + name + ": retaining load hands back no root, refused");
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
            Check(load((IntPtr)region, DiscardRegion, wire, report) == IntPtr.Zero && (report.Damaged || report.Refused), Discard + name + ": region load hands back no root, refused");
            var store = new G.TableRetain { Bytes = bytes, Capacity = 4096, Ids = ids, IdCapacity = 64 };
            store.Used = 7; store.Count = 1;
            var r = new G.TableReport();
            Check(retainLoad((IntPtr)region, DiscardRegion, wire, ref store, r) == IntPtr.Zero && (r.Damaged || r.Refused), Discard + name + ": retaining load hands back no root, refused");
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
        // kind 37 under the known field `factor` is damage and a kind mismatch at
        // once; strict-only fixes no detection order, and either refuses the load.
        Check(report.Damaged || report.Refused, Discard + "nested fixed: the report does not say refused");
        Check(v.A == 5, Discard + "nested fixed: a damaged load handed back a=" + v.A);
        var nestedVerdict = V1.Schema.CfgLoadVerdict(new V1.Cfg(), wire, new V1.TableReport());
        Check(nestedVerdict == V1.Schema.TableWire.Verdict.Damaged || nestedVerdict == V1.Schema.TableWire.Verdict.Refused, Discard + "nested fixed: the verdict is a refusal");
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
        Check(report.Damaged || report.Refused, Discard + "nested variable: the report does not say refused");
        Check(v.Version == 1 && v.Meta.Build == 1 && v.Ground.Depth == 0, Discard + "nested variable: a damaged managed load handed back version=" + v.Version + " build=" + v.Meta.Build);

        byte* region = (byte*)NativeMemory.AlignedAlloc(DiscardRegion, 64);
        byte* bytes = (byte*)NativeMemory.Alloc(4096);
        G.TableRetain.Id* ids = (G.TableRetain.Id*)NativeMemory.Alloc((nuint)(64 * sizeof(G.TableRetain.Id)));
        try
        {
            var g = new G.TableReport();
            G.SceneRow* root = G.Schema.SceneLoad((IntPtr)region, DiscardRegion, wire, g);
            Check(root == null, Discard + "nested variable: region load handed back a root on damage" + (root != null ? ": version=" + root->Version : ""));
            Check(g.Damaged || g.Refused, Discard + "nested variable: the region report does not say refused");
            var store = new G.TableRetain { Bytes = bytes, Capacity = 4096, Ids = ids, IdCapacity = 64 };
            var rr = new G.TableReport();
            Check(G.Schema.SceneLoadRetain((IntPtr)region, DiscardRegion, wire, ref store, rr) == null, Discard + "nested variable: retaining load handed back a root on damage");
            Check((rr.Damaged || rr.Refused) && store.Used == 0 && store.Count == 0, Discard + "nested variable: retaining load refused=" + (rr.Damaged || rr.Refused) + " used=" + store.Used + " count=" + store.Count);
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.Free(bytes); NativeMemory.Free(ids); }

        using (var builder = new G.SceneBuilder())
        {
            var b = new G.TableReport();
            Check(!G.Schema.SceneLoadBuilder(builder, wire, b), Discard + "nested variable: builder load answered true on damage");
            Check(b.Damaged || b.Refused, Discard + "nested variable: the builder report does not say refused");
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
        Check(!V1.Schema.CfgFromJson(v, text, report) && report.Damaged, Discard + "text: the read answered true on damage");
        Check(v.A == 5, Discard + "text: a damaged text read handed back a=" + v.A);
    }

    static byte[] CycleWire(byte[] rootBody, params (ulong TypeId, byte[] Body)[] nodes)
    {
        string[] names = { "id", "active", "next", "guarded", "links", "edges", "key", "value", "CycleNode" };
        using var into = new MemoryStream();
        into.WriteByte(1);
        into.Write(rootBody);
        if (nodes.Length > 0)
        {
            using var table = new MemoryStream();
            table.Write(Var((ulong)nodes.Length));
            foreach (var n in nodes)
            {
                table.Write(Var(n.TypeId));
                table.Write(Var((ulong)n.Body.Length));
                table.Write(n.Body);
            }
            byte[] payload = table.ToArray();
            into.Write(Var((ulong)(names.Length + 1)));
            into.WriteByte(12);
            into.Write(Var((ulong)payload.Length));
            into.Write(payload);
        }
        into.WriteByte(0);
        foreach (string name in names)
        {
            ulong h = FieldId(name);
            for (int i = 0; i < 8; i++) { into.WriteByte((byte)h); h >>= 8; }
        }
        ulong max = ulong.MaxValue;
        for (int i = 0; i < 8; i++) { into.WriteByte((byte)max); max >>= 8; }
        ulong count = (ulong)names.Length + 1;
        for (int i = 0; i < 8; i++) { into.WriteByte((byte)count); count >>= 8; }
        return into.ToArray();
    }

    static void DiscardCycleNode(string name, byte[] wire, bool wantCyclic)
    {
        C2.CycleNode v = new C2.CycleNode { Id = 99 };
        var report = new C2.TableReport();
        bool ok = C2.Schema.CycleNodeLoad(v, wire, report);
        if (wantCyclic)
        {
            Check(!ok && report.Damaged, Discard + name + ": managed load answers false, malformed");
            Check(v.Id == 7 && v.Next == null && v.Guarded == null && v.LinksCount == 0 && v.EdgesCount == 0,
                Discard + name + ": managed load hands back the declared defaults");
        }
        else
        {
            Check(ok && !report.Damaged && v.Id == 42, Discard + name + ": acyclic managed load succeeds with id=42");
        }

        byte* region = (byte*)NativeMemory.AlignedAlloc(DiscardRegion, 64);
        byte* bytes = (byte*)NativeMemory.Alloc(4096);
        C2.TableRetain.Id* ids = (C2.TableRetain.Id*)NativeMemory.Alloc((nuint)(64 * sizeof(C2.TableRetain.Id)));
        try
        {
            var r = new C2.TableReport();
            C2.CycleNodeRow* row = C2.Schema.CycleNodeLoad((IntPtr)region, DiscardRegion, wire, r);
            var store = new C2.TableRetain { Bytes = bytes, Capacity = 4096, Ids = ids, IdCapacity = 64, Used = 7, Count = 1, IdUsed = 1 };
            var rr = new C2.TableReport();
            C2.CycleNodeRow* rrow = C2.Schema.CycleNodeLoadRetain((IntPtr)region, DiscardRegion, wire, ref store, rr);
            if (wantCyclic)
            {
                Check(row == null && r.Damaged, Discard + name + ": region load hands back no root, malformed");
                Check(rrow == null && rr.Damaged, Discard + name + ": retaining load hands back no root, malformed");
                Check(store.Used == 0 && store.Count == 0 && store.IdUsed == 0, Discard + name + ": retaining load leaves the store empty");
            }
            else
            {
                Check(row != null && !r.Damaged && row->Id == 42, Discard + name + ": acyclic region load succeeds");
                Check(rrow != null && !rr.Damaged && rrow->Id == 42, Discard + name + ": acyclic retaining load succeeds");
            }
        }
        finally { NativeMemory.AlignedFree(region); NativeMemory.Free(bytes); NativeMemory.Free(ids); }

        using (var builder = new C2.CycleNodeBuilder())
        {
            builder.GetRoot()->Id = 99;
            var b = new C2.TableReport();
            bool bok = C2.Schema.CycleNodeLoadBuilder(builder, wire, b);
            if (wantCyclic)
            {
                Check(!bok && b.Damaged, Discard + name + ": builder load answers false, malformed");
                Check(builder.GetRoot()->Id == 7 && builder.GetRoot()->Next == 0 && builder.GetRoot()->Guarded == 0,
                    Discard + name + ": builder root is back at its declared defaults");
            }
            else
            {
                Check(bok && !b.Damaged && builder.GetRoot()->Id == 42, Discard + name + ": acyclic builder load succeeds");
            }
        }
    }

    static void DiscardCycleVectors()
    {
        // Vocab indices in CycleWire: 1=id, 2=active, 3=next, 4=guarded, 5=links, 6=edges, 7=key, 8=value, 9=CycleNode
        byte[] id42 = Join(new byte[] { 1, 4 }, U32(42));
        byte[] leaf = Join(new byte[] { 1, 4 }, U32(10), new byte[] { 0 });

        // 1. Overwritten self-edge in one body: next=&root (node 1) then next=null (node 0) -> acyclic!
        byte[] overwritten = CycleWire(Join(id42, new byte[] { 3, 17, 1, 3, 17, 0 }));
        DiscardCycleNode("cycle_overwritten_self", overwritten, false);

        // 2. Unreached cycle: root has next=null, while node 2 and node 3 point to each other
        byte[] n2 = new byte[] { 3, 17, 3, 0 };
        byte[] n3 = new byte[] { 3, 17, 2, 0 };
        byte[] unreached = CycleWire(id42, (9, n2), (9, n3));
        DiscardCycleNode("cycle_unreached", unreached, true);

        // 3. False-guard pointer slot: active=false (0), guarded=&root (node 1) -> cyclic; guarded=&leaf (node 2) -> acyclic
        byte[] falseGuardCycle = CycleWire(Join(id42, new byte[] { 2, 1, 0, 4, 17, 1 }));
        DiscardCycleNode("cycle_false_guard", falseGuardCycle, true);
        byte[] falseGuardAcyclic = CycleWire(Join(id42, new byte[] { 2, 1, 0, 4, 17, 2 }), (9, leaf));
        DiscardCycleNode("acyclic_false_guard", falseGuardAcyclic, false);

        // 4. []*CycleNode list pointer slot: links=[&root] -> cyclic; links=[&leaf] -> acyclic
        byte[] listCycle = CycleWire(Join(id42, new byte[] { 5, 14, 3, 17, 1, 1 }));
        DiscardCycleNode("cycle_list_slot", listCycle, true);
        byte[] listAcyclic = CycleWire(Join(id42, new byte[] { 5, 14, 3, 17, 1, 2 }), (9, leaf));
        DiscardCycleNode("acyclic_list_slot", listAcyclic, false);

        // 5. map[int32]*CycleNode map pointer slot: edges={1: &root} -> cyclic; edges={1: &leaf} -> acyclic
        byte[] entrySelf = Join(new byte[] { 7, 4 }, U32(1), new byte[] { 8, 17, 1, 0 });
        byte[] mapPayloadSelf = Join(new byte[] { 13, 1 }, Var((ulong)entrySelf.Length), entrySelf);
        byte[] mapCycle = CycleWire(Join(id42, new byte[] { 6, 14 }, Var((ulong)mapPayloadSelf.Length), mapPayloadSelf));
        DiscardCycleNode("cycle_map_slot", mapCycle, true);

        byte[] entryLeaf = Join(new byte[] { 7, 4 }, U32(1), new byte[] { 8, 17, 2, 0 });
        byte[] mapPayloadLeaf = Join(new byte[] { 13, 1 }, Var((ulong)entryLeaf.Length), entryLeaf);
        byte[] mapAcyclic = CycleWire(Join(id42, new byte[] { 6, 14 }, Var((ulong)mapPayloadLeaf.Length), mapPayloadLeaf), (9, leaf));
        DiscardCycleNode("acyclic_map_slot", mapAcyclic, false);

        // 6. Manifest acyclic twins (node_chain_two and node_diamond_chain) load cleanly across all 4 entry points
        string manifest = DiscardManifest();
        if (manifest != null)
        {
            string root = Path.GetDirectoryName(Path.GetDirectoryName(Path.GetDirectoryName(Path.GetDirectoryName(manifest))));
            foreach (string line in File.ReadAllLines(manifest))
            {
                string[] f = line.Split((char[])null, StringSplitOptions.RemoveEmptyEntries);
                if (f.Length < 6 || f[0] != "framing") { continue; }
                string row = f[1];
                if (row == "node_chain_two")
                {
                    byte[] wire = File.ReadAllBytes(Path.Combine(root, f[4]));
                    var mrep = new G.TableReport();
                    var mv = new G.ListNode();
                    Check(G.Schema.ListNodeLoad(mv, wire, mrep) && !mrep.Damaged && mv.Next != null, Discard + row + ": managed load succeeds");
                    long need = G.Schema.ListNodeLoadMeasure(wire, out _, out _, out _);
                    Check(need > 0, Discard + row + ": LoadMeasure succeeds");
                    byte* reg = (byte*)NativeMemory.AlignedAlloc((nuint)(need + 64), 64);
                    try
                    {
                        var rrep = new G.TableReport();
                        G.ListNodeRow* rroot = G.Schema.ListNodeLoad((IntPtr)reg, need, wire, rrep);
                        Check(rroot != null && !rrep.Damaged && rroot->Next != 0, Discard + row + ": region load succeeds");
                    }
                    finally { NativeMemory.AlignedFree(reg); }
                    using var bld = new G.ListNodeBuilder();
                    var brep = new G.TableReport();
                    Check(G.Schema.ListNodeLoadBuilder(bld, wire, brep) && !brep.Damaged && bld.GetRoot()->Next != 0, Discard + row + ": builder load succeeds");
                }
                else if (row == "node_diamond_chain")
                {
                    byte[] wire = File.ReadAllBytes(Path.Combine(root, f[4]));
                    var mrep = new G.TableReport();
                    var mv = new G.Scene();
                    Check(G.Schema.SceneLoad(mv, wire, mrep) && !mrep.Damaged && mv.Tree != null, Discard + row + ": managed load succeeds");
                    long need = G.Schema.SceneLoadMeasure(wire, out _, out _, out _);
                    Check(need > 0, Discard + row + ": LoadMeasure succeeds");
                    byte* reg = (byte*)NativeMemory.AlignedAlloc((nuint)(need + 64), 64);
                    try
                    {
                        var rrep = new G.TableReport();
                        G.SceneRow* rroot = G.Schema.SceneLoad((IntPtr)reg, need, wire, rrep);
                        Check(rroot != null && !rrep.Damaged && rroot->Tree != 0, Discard + row + ": region load succeeds");
                    }
                    finally { NativeMemory.AlignedFree(reg); }
                    using var bld = new G.SceneBuilder();
                    var brep = new G.TableReport();
                    Check(G.Schema.SceneLoadBuilder(bld, wire, brep) && !brep.Damaged && bld.GetRoot()->Tree != 0, Discard + row + ": builder load succeeds");
                }
            }
        }
    }
}
