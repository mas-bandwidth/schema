using System;
using V1 = Tblv1;
using G = Graphdemo;

static partial class Program
{
    static void DirtyResetTarget(V1.Cfg value)
    {
        value.A = 99; value.B = 42; value.Mode = V1.Mode.Alpha;
        value.Inner.Factor = 99;
        value.NameLength = 4; Array.Fill(value.Name, (byte)7);
        value.ItemsCount = 3; Array.Fill(value.Items, 8);
        value.ExtraPresent = true; value.Extra.Factor = 33;
        value.TierPresent = true; value.Tier = 99;
        value.MarkPresent = true; value.Mark = V1.Grade.Gold;
    }

    static bool ResetTargetDefaults(V1.Cfg value, int a)
    {
        return value.A == a && value.B == 1.5f && value.Mode == V1.Mode.Beta &&
            value.Inner.Factor == 2.5f && value.NameLength == 0 &&
            Array.TrueForAll(value.Name, b => b == 0) && value.ItemsCount == 0 &&
            Array.TrueForAll(value.Items, n => n == 0) && !value.ExtraPresent &&
            value.Extra.Factor == 2.5f && !value.TierPresent && value.Tier == 0 &&
            !value.MarkPresent && value.Mark == V1.Grade.None;
    }

    static void TestRootReset()
    {
        V1.Cfg value = new V1.Cfg();
        V1.TableTypeInfo original = V1.Schema.CfgTableType();
        int resets = 0;
        // A fresh descriptor counts calls while forwarding the real generated
        // reset. Shared metadata remains untouched. This distinguishes the
        // single public-load prefill from an unnecessary second root walk.
        var type = new V1.TableTypeInfo {
            Name = original.Name, Id = original.Id, NumFields = original.NumFields,
            Fields = original.Fields,
            Reset = o => { if (ReferenceEquals(o, value)) { resets++; } original.Reset(o); }
        };
        void Load(byte[] bytes, V1.Schema.TableWire.Verdict expected, int a, string label)
        {
            DirtyResetTarget(value); resets = 0;
            V1.TableReport report = new V1.TableReport();
            Check(V1.Schema.TableWire.Load(value, type, bytes, report) == expected && report.Verdict == expected,
                "root reset verdict: " + label);
            Check(resets == 1 && ResetTargetDefaults(value, a), "one root reset and expected storage: " + label);
            Check(report.Malformed == (expected == V1.Schema.TableWire.Verdict.Damaged || expected == V1.Schema.TableWire.Verdict.BodyStopped) &&
                report.Refused == (expected == V1.Schema.TableWire.Verdict.Refused) &&
                report.Unknown == 0 && report.KindMismatch == 0 && report.Clamped == 0 && report.Widened == 0 && report.Duplicate == 0,
                "root reset preserves report: " + label);
            Check(report.Reason == (expected == V1.Schema.TableWire.Verdict.Refused ?
                (bytes[0] == 2 ? "message_form_as_file" : "newer_form") : null), "root reset refusal reason: " + label);
        }
        Load(Fixture(new byte[] { 0 }), V1.Schema.TableWire.Verdict.Ok, 5, "absent fields");
        byte[] field = Join(new byte[] { 1,4 }, U32(42));
        Load(Fixture(Join(field, new byte[] { 0 }), "a"), V1.Schema.TableWire.Verdict.Ok, 42, "present scalar");
        Load(Fixture(field, "a"), V1.Schema.TableWire.Verdict.BodyStopped, 42, "unterminated body keeps decoded prefix");
        Load(Array.Empty<byte>(), V1.Schema.TableWire.Verdict.Damaged, 5, "empty input");
        Load(new byte[] { 2 }, V1.Schema.TableWire.Verdict.Refused, 5, "message form");
        Load(new byte[] { 3 }, V1.Schema.TableWire.Verdict.Refused, 5, "newer form");
        Load(new byte[] { 1 }, V1.Schema.TableWire.Verdict.Damaged, 5, "truncated trailer");
        Load(Fixture(new byte[] { 0 }, "a", "a"), V1.Schema.TableWire.Verdict.Damaged, 5, "duplicate vocabulary");
        Load(Fixture(Join(field, new byte[] { 0,99 }), "a"), V1.Schema.TableWire.Verdict.Damaged, 5, "root framing slack before overlay");

        // Two occurrences replace the SAME nested target. The second, empty
        // body must restore factor's declared default, not retain the first 9.
        byte[] first = Join(new byte[] { 2,10 }, U32(0x41100000), new byte[] { 0 });
        Load(Fixture(Join(new byte[] { 1,13 }, Var((ulong)first.Length), first,
            new byte[] { 1,13,1,0,0 }), "inner", "factor"), V1.Schema.TableWire.Verdict.Ok, 5, "duplicate nested replacement");
        TestGraphRootReset();
    }

    static void TestGraphRootReset()
    {
        G.Scene value = new G.Scene();
        G.TableTypeInfo original = G.Schema.SceneTableType();
        int resets = 0;
        var type = new G.TableTypeInfo {
            Name = original.Name, Id = original.Id, NumFields = original.NumFields,
            Fields = original.Fields, Create = original.Create, Variable = original.Variable,
            StorageSize = original.StorageSize, StorageAlign = original.StorageAlign, RegionAlign = original.RegionAlign,
            PointerTypes = original.PointerTypes, PointerType = original.PointerType,
            BytesEdge = original.BytesEdge, StringEdge = original.StringEdge,
            Reset = o => { if (ReferenceEquals(o, value)) { resets++; } original.Reset(o); }
        };
        G.ListNode stale = new G.ListNode { Value = 99 };
        void Dirty()
        {
            value.Version = 88; value.Head = stale; value.Alias = stale;
            value.Ground.Depth = 99; value.Ground.Head = stale;
            value.Meta.Build = 99; value.Meta.TagLength = 3; Array.Fill(value.Meta.Tag, (byte)7);
            value.LayersCount = 2; value.Layers[1].Depth = 99; value.Layers[1].Head = stale;
            resets = 0;
        }
        Dirty();
        G.TableReport report = new G.TableReport();
        byte[] golden = ReadGolden("graph_shared");
        Check(G.Schema.TableWire.Load(value, type, golden, report) == G.Schema.TableWire.Verdict.Ok && resets == 1,
            "graph load resets reused root once");
        Check(value.Version == 1 && value.Meta.Build == 1 && value.Meta.TagLength == 0 &&
            Array.TrueForAll(value.Meta.Tag, b => b == 0) && value.LayersCount == 0 &&
            value.Layers[1].Depth == 0 && value.Layers[1].Head == null && GetString(value.Name, value.NameLength) == "shared",
            "graph absent fields and counted tails start at defaults");
        Check(value.Head != null && !ReferenceEquals(value.Head, stale) && value.Head.Value == 7 &&
            GetString(value.Head.Name, value.Head.NameLength) == "one" && value.Head.Next == null &&
            ReferenceEquals(value.Head, value.Alias) && ReferenceEquals(value.Head, value.Ground.Head) && value.Ground.Depth == 1 &&
            value.Tree?.Left != null && value.Tree.Right != null && ReferenceEquals(value.Tree.Right, value.Tree.Left.Left),
            "root and nested graph fields retain shared and diamond node identities");
        Check(!report.Malformed && !report.Refused && report.Unknown == 0 && report.KindMismatch == 0 &&
            report.Clamped == 0 && report.Widened == 0 && report.Duplicate == 0, "graph root reset preserves report");
        byte[] saved = new byte[golden.Length];
        Check(G.Schema.SceneSave(value, saved) == golden.Length && saved.AsSpan().SequenceEqual(golden),
            "graph root reset preserves exact shared golden bytes");

        Dirty(); report = new G.TableReport();
        Check(G.Schema.TableWire.Load(value, type, new byte[] { 2 }, report) == G.Schema.TableWire.Verdict.Refused && resets == 1 &&
            report.Refused && report.Reason == "message_form_as_file" && !report.Malformed &&
            value.Head == null && value.Alias == null && value.Ground.Head == null && value.Ground.Depth == 0 &&
            value.Version == 1 && value.Meta.Build == 1 && value.LayersCount == 0 && value.Layers[1].Head == null,
            "graph early refusal resets stale pointers and nested state before parsing");
    }
}
