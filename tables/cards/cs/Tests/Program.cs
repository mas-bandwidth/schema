// Program.cs — the C# card facade's test runner (docs/SPEC-TABLES.md §2,
// §4, §5, §6.2, §6.6, §8, §16). A plain Main with one check per named test
// and a nonzero exit on any failure; no test framework. The named tests are
// the ones the card's DONE-WHEN names:
//
//   TestCardRoundTripsThePopulatedFixture — Load/ToJSON/FromJSON/Wire
//     reproduce the committed fixture byte for byte, and the record's wire
//     is a defensive copy.
//   TestCardBoundsRefuseBeforePublishing — the customer ceilings
//     (MaxBodyBytes 65536, MaxListEntries 512) refuse before any bytes are
//     published.
//   TestParseRenderMatchesGo — Parse and Render match the Go leg's expected
//     bytes on the shared corpus.
//   TestUpgradeMatchesGo — every upgrade case in tables/cards/conformance
//     yields the expected wire and report byte for byte, or the expected
//     refusal class; an upgraded record upgrades byte-identically again;
//     retention storage one id short refuses with retain-lost and leaves
//     the input unchanged.
//   TestTaskHashFramesLogicalFields — the digest's frame is pinned by an
//     independently written byte vector, the logical fields are separated
//     by the frame, a lossy record is refused, and every expected .hex
//     matches.
//   TestFieldEditLeavesTheTaskHashAlone — needs/tier/priority/column/holder
//     edits change the wire and not the hash, a paths edit changes both, the
//     body is byte-equal throughout, every refusal leaves the wire
//     byte-equal, and every edit case in the corpus matches its expected
//     wire or refusal class.
//   TestViewWalksEveryField — the generated view matches an explicit
//     vocabulary written here (not derived from the descriptors) and the
//     corpus's expected view.txt, and a loaded and an edited record's
//     scalar, list and body values walk through the descriptors.
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

    // CorpusCase is one manifest line of the shared corpus.
    private sealed class CorpusCase
    {
        public string Name;
        public string Input;
        public string Edit;
    }

    // CorpusCases reads the committed manifest's lines for one surface
    // (tables/cards/conformance/MANIFEST.txt).
    private static CorpusCase[] CorpusCases(string surface)
    {
        var outCases = new System.Collections.Generic.List<CorpusCase>();
        foreach (string raw in File.ReadAllLines("tables/cards/conformance/MANIFEST.txt"))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#", StringComparison.Ordinal)) continue;
            string[] f = line.Split(new[] { ' ', '\t' }, StringSplitOptions.RemoveEmptyEntries);
            if (f.Length < 3 || f[0] != surface) continue;
            outCases.Add(new CorpusCase { Name = f[1], Input = f[2], Edit = f.Length > 3 ? f[3] : null });
        }
        return outCases.ToArray();
    }

    // ReportText spells a report as the corpus's X.report does, one
    // Name=value line per counter in declaration order
    // (tables/cards/conformance/FORMAT.md).
    private static string ReportText(TableReport r)
    {
        var b = new StringBuilder();
        b.Append("Retained=").Append(r.Retained).Append('\n');
        b.Append("RetainLost=").Append(r.RetainLost).Append('\n');
        b.Append("Widened=").Append(r.Widened).Append('\n');
        b.Append("Unknown=").Append(r.Unknown).Append('\n');
        b.Append("KindMismatch=").Append(r.KindMismatch).Append('\n');
        b.Append("Clamped=").Append(r.Clamped).Append('\n');
        b.Append("Duplicate=").Append(r.Duplicate).Append('\n');
        b.Append("Malformed=").Append(r.Malformed ? "true" : "false").Append('\n');
        return b.ToString();
    }

    // RefusalClass maps a facade error to one class of the closed list
    // FORMAT.md fixes (the driver's twin mapping).
    private static string RefusalClass(CardException err)
    {
        string msg = err.Message;
        if (msg.Contains("past the 65536-byte ceiling")) return "body-bound 65536";
        if (msg.Contains("past the 512-entry ceiling")) return "list-bound " + FieldName(msg) + " 512";
        if (msg.Contains("stranded header key")) return "stranded-key " + QuotedWord(msg);
        if (msg.Contains("retained field(s) lost")) return "retain-lost";
        if (msg.Contains("record has loss events")) return "unknown-present";
        return "malformed";
    }

    private static string FieldName(string msg)
    {
        string rest = msg.StartsWith("cards: ", StringComparison.Ordinal) ? msg.Substring("cards: ".Length) : msg;
        int i = rest.IndexOf(" has ", StringComparison.Ordinal);
        return i < 0 ? rest : rest.Substring(0, i);
    }

    private static string QuotedWord(string msg)
    {
        int i = msg.IndexOf('"');
        if (i < 0) return "";
        int j = msg.IndexOf('"', i + 1);
        return j < 0 ? "" : msg.Substring(i + 1, j - i - 1);
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


    // TestParseRenderMatchesGo proves Parse and Render match the Go leg's
    // expected bytes on the shared corpus: every parse case parses to the
    // expected wire, the rejected case refuses with the stranded key, and
    // every render case renders to the expected text byte for byte
    // (tables/cards/conformance; tables/cards/parse.go, render.go).
    private static void TestParseRenderMatchesGo()
    {
        byte[] text = File.ReadAllBytes("tables/cards/conformance/cases/normalized-valid.md");
        Record rec = Record.Parse(text);
        SameBytes(rec.Wire(),
            File.ReadAllBytes("tables/cards/conformance/expected/normalized-valid.wire"),
            "normalized-valid parses to the expected wire");

        byte[] legacy = File.ReadAllBytes("tables/cards/conformance/cases/rejected-legacy.md");
        try
        {
            Record.Parse(legacy);
            Check(false, "rejected-legacy refuses with the stranded key");
        }
        catch (CardException e)
        {
            Check(e.Message.Contains("stranded header key") && e.Message.Contains("PRIORITY"),
                "rejected-legacy refuses with the stranded key PRIORITY (" + e.Message + ")");
        }

        foreach (string name in new[] { "card-populated", "initial-populated", "consumers-populated", "empty-consumers" })
        {
            byte[] wire = File.ReadAllBytes("tables/cards/conformance/cases/" + name + ".wire");
            Record r = Record.Load(wire, out _);
            SameBytes(r.Render(),
                File.ReadAllBytes("tables/cards/conformance/expected/" + name + ".txt"),
                name + " renders to the expected text");
        }
    }

    // TestUpgradeMatchesGo proves the retained upgrade matches the Go leg on
    // the shared corpus (tla/CardVersioning.tla: NoFieldLost, BodyUnchanged,
    // RefusalKeepsOriginal, ShippedUpgradeSucceeds; tla/CardHops.tla:
    // ImplementationsAgree): every upgrade case yields the expected wire and
    // report byte for byte, or the expected refusal class; an upgraded record
    // upgrades byte-identically again; and retention storage one id short
    // refuses naming retain-lost while the input is unchanged
    // (docs/SPEC-TABLES.md §6.6; tables/cards/upgrade.go).
    private static void TestUpgradeMatchesGo()
    {
        foreach (CorpusCase c in CorpusCases("upgrade"))
        {
            byte[] wire = File.ReadAllBytes(c.Input);
            byte[] snapshot = (byte[])wire.Clone();
            string refusedPath = "tables/cards/conformance/expected/" + c.Name + ".refused";
            try
            {
                byte[] upgraded = Record.Upgrade(wire, out TableReport report);
                if (File.Exists(refusedPath))
                {
                    Check(false, c.Name + " is refused by the Go leg but upgraded here");
                    continue;
                }
                SameBytes(upgraded,
                    File.ReadAllBytes("tables/cards/conformance/expected/" + c.Name + ".wire"),
                    c.Name + " upgrades to the expected wire");
                SameBytes(Encoding.UTF8.GetBytes(ReportText(report)),
                    File.ReadAllBytes("tables/cards/conformance/expected/" + c.Name + ".report"),
                    c.Name + " upgrades to the expected report");
                // ShippedUpgradeSucceeds's idempotence: the upgrade of an
                // upgraded record is byte-identical (§6.6).
                SameBytes(Record.Upgrade(upgraded, out _), upgraded,
                    c.Name + " upgrades idempotently");
            }
            catch (CardException e)
            {
                if (!File.Exists(refusedPath))
                {
                    Check(false, c.Name + " was refused but the Go leg upgrades it (" + e.Message + ")");
                    continue;
                }
                SameBytes(Encoding.UTF8.GetBytes(RefusalClass(e) + "\n"),
                    File.ReadAllBytes(refusedPath),
                    c.Name + " refuses with the expected class");
            }
            SameBytes(wire, snapshot, c.Name + " leaves the input wire unchanged");
        }

        // The package-private retention seam driven one id short of the
        // future-note case's single retained field: RetainLost is named, no
        // output is published and the input is preserved, while the fixed
        // production capacities succeed (tables/cards/upgrade.go: upgrade;
        // TestUpgradeTinyCapacityRetainLost's twin).
        byte[] future = File.ReadAllBytes("tables/cards/conformance/cases/future-note.wire");
        byte[] futureSnapshot = (byte[])future.Clone();
        TableReport seamReport = null;
        try
        {
            Record.UpgradeSeam(future, Record.MaxRetainBytes, 0, out seamReport);
            Check(false, "retention storage one id short refuses");
        }
        catch (CardException e)
        {
            Check(e.Message.Contains("retained field(s) lost"),
                "retention storage one id short names retain-lost (" + e.Message + ")");
            Check(seamReport != null && seamReport.RetainLost > 0,
                "retention storage one id short reports RetainLost (" +
                (seamReport == null ? -1 : seamReport.RetainLost) + ")");
        }
        SameBytes(future, futureSnapshot, "retention storage one id short leaves the input unchanged");
        byte[] ok = Record.Upgrade(future, out TableReport full);
        Check(full.Retained == 1 && full.RetainLost == 0,
            "the fixed production capacities retain the future field (" + full.Retained + " retained)");
        Check(ok.Length > 0, "the fixed production capacities publish output");
    }

    // TestTaskHashFramesLogicalFields pins the digest's frame with an
    // independently written byte vector, proves the frame separates the
    // logical fields (body "a" with paths ["bc"] and body "ab" with paths
    // ["c"] hash differently), proves a lossy record is refused with no
    // digest, and matches every expected .hex of the corpus
    // (tla/CardHops.tla: TaskHashStable, ImplementationsAgree;
    // tables/cards/edit.go: TaskHash).
    private static void TestTaskHashFramesLogicalFields()
    {
        // The independent frame: body, paths, new, shared, test, done, each
        // text an unsigned 64-bit little-endian byte length then its bytes,
        // each list an unsigned 64-bit little-endian count then its
        // length-prefixed elements — written here, not shared with the
        // facade.
        System.Collections.Generic.List<byte> Frame(params object[] parts)
        {
            var b = new System.Collections.Generic.List<byte>();
            foreach (object p in parts)
            {
                if (p is string s)
                {
                    byte[] text = Encoding.UTF8.GetBytes(s);
                    for (int i = 0; i < 8; i++) b.Add((byte)(((long)text.Length >> (8 * i)) & 0xff));
                    b.AddRange(text);
                }
                else if (p is string[] list)
                {
                    for (int i = 0; i < 8; i++) b.Add((byte)(((long)list.Length >> (8 * i)) & 0xff));
                    foreach (string e in list)
                    {
                        byte[] text = Encoding.UTF8.GetBytes(e);
                        for (int i = 0; i < 8; i++) b.Add((byte)(((long)text.Length >> (8 * i)) & 0xff));
                        b.AddRange(text);
                    }
                }
            }
            return b;
        }

        Record a = Record.FromJson(Encoding.UTF8.GetBytes(
            "{\"id\":\"x\",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}],\"test\":\"t\",\"done_when\":\"d\"}"));
        byte[] want = System.Security.Cryptography.SHA256.HashData(
            Frame("a", new[] { "bc" }, new string[0], new string[0], "t", "d").ToArray());
        SameBytes(Record.TaskHash(a), want, "the digest frames body, paths, new, shared, test, done");

        Record ab = Record.FromJson(Encoding.UTF8.GetBytes(
            "{\"id\":\"x\",\"body\":\"ab\",\"paths\":[{\"s\":\"c\"}],\"test\":\"t\",\"done_when\":\"d\"}"));
        byte[] hashA = Record.TaskHash(a);
        byte[] hashAb = Record.TaskHash(ab);
        Check(hashA.Length == hashAb.Length, "both digests are 32 bytes");
        bool differ = false;
        for (int i = 0; i < hashA.Length; i++) if (hashA[i] != hashAb[i]) { differ = true; break; }
        Check(differ, "body \"a\" with paths [\"bc\"] and body \"ab\" with paths [\"c\"] hash differently");

        // needs, tier, priority and machine fields are excluded: editing
        // them cannot move the digest (TaskHashStable).
        Record edited = Record.FromJson(Encoding.UTF8.GetBytes(
            "{\"id\":\"x\",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}],\"test\":\"t\",\"done_when\":\"d\",\"needs\":[{\"s\":\"n\"}],\"tier\":\"pro\"}"));
        SameBytes(Record.TaskHash(edited), hashA, "needs and tier are excluded from the digest");

        // A lossy record is refused with an error and no digest
        // (tables/cards/edit.go: refuseLossy).
        Record lossy = Record.Load(
            File.ReadAllBytes("tables/cards/conformance/cases/future-note.wire"), out TableReport lossyReport);
        Check(lossyReport.Unknown > 0, "the future-note record is lossy");
        try
        {
            Record.TaskHash(lossy);
            Check(false, "a lossy record is refused");
        }
        catch (CardException e)
        {
            Check(e.Message.Contains("loss events"), "a lossy record is refused naming loss (" + e.Message + ")");
        }

        foreach (CorpusCase c in CorpusCases("taskhash"))
        {
            Record rec = Record.Load(File.ReadAllBytes(c.Input), out _);
            SameBytes(Encoding.UTF8.GetBytes(
                    BitConverter.ToString(Record.TaskHash(rec)).Replace("-", "").ToLowerInvariant()),
                File.ReadAllBytes("tables/cards/conformance/expected/" + c.Name + ".hex"),
                c.Name + " hashes to the expected digest");
        }
    }

    // TestFieldEditLeavesTheTaskHashAlone proves the atomic field edits
    // (tla/CardHops.tla: TaskHashStable; the reversed witness
    // EditDropsUnknown is why an edit over an unknown field refuses):
    // needs, tier, priority, column and holder edits change the wire and not
    // the hash; a paths edit changes both; the body is byte-equal throughout;
    // every refusal leaves the wire byte-equal to before; and every edit
    // case in the corpus matches its expected wire or refusal class
    // (docs/SPEC-TABLES.md §2.9, §6.2, §8; tables/cards/edit.go).
    private static void TestFieldEditLeavesTheTaskHashAlone()
    {
        byte[] wire = File.ReadAllBytes("tables/cards/conformance/cases/card-populated.wire");
        Record rec = Record.Load(wire, out _);
        byte[] bodyBefore = rec.Root.Body.Data;

        byte[] hashBefore = Record.TaskHash(rec);

        // Edits outside the task: the wire moves, the digest does not, the
        // body is byte-equal throughout.
        var quiet = new (string Name, Action<Record> Apply)[]
        {
            ("needs", r => r.SetNeeds(new[] { "a", "b" })),
            ("tier", r => r.SetTier(Tier.heavy)),
            ("priority", r => r.SetPriority(Priority.blocker)),
            ("column", r => r.SetColumn(Column.landed)),
            ("holder", r => r.SetHolder("h")),
        };
        foreach ((string name, Action<Record> apply) in quiet)
        {
            byte[] before = rec.Wire();
            apply(rec);
            byte[] after = rec.Wire();
            bool moved = !EqualBytes(after, before);
            Check(moved, name + " edit changes the wire");
            SameBytes(Record.TaskHash(rec), hashBefore, name + " edit leaves the task hash alone");
            SameBytes(rec.Root.Body.Data, bodyBefore, name + " edit leaves the body byte-equal");
        }

        // A paths edit is inside the task: both move.
        {
            byte[] before = rec.Wire();
            rec.SetPaths(new[] { "x", "y" });
            Check(!EqualBytes(rec.Wire(), before), "paths edit changes the wire");
            bool hashMoved = !EqualBytes(Record.TaskHash(rec), hashBefore);
            Check(hashMoved, "paths edit changes the task hash");
            SameBytes(rec.Root.Body.Data, bodyBefore, "paths edit leaves the body byte-equal");
        }

        // Every refusal leaves the wire byte-equal to before (§6.2), and the
        // per-entry bounds and declared vocabularies refuse
        // (tables/cards/edit.go: validateStrings, SetTier, SetPriority).
        var refusals = new (string Name, Action<Record> Apply)[]
        {
            ("tier past the last variant", r => r.SetTier((Tier)99)),
            ("priority past the last variant", r => r.SetPriority((Priority)99)),
            ("column past the last variant", r => r.SetColumn((Column)99)),
            ("needs element past 64 bytes", r => r.SetNeeds(new[] { new string('n', 65) })),
            ("paths element past 192 bytes", r => r.SetPaths(new[] { new string('p', 193) })),
            ("holder past 32 bytes", r => r.SetHolder(new string('h', 33))),
            ("needs element with a NUL", r => r.SetNeeds(new[] { "a\0b" })),
            ("needs past 512 entries", r => r.SetNeeds(LongList(513))),
            ("paths past 512 entries", r => r.SetPaths(LongList(513))),
        };
        foreach ((string name, Action<Record> apply) in refusals)
        {
            byte[] before = rec.Wire();
            try
            {
                apply(rec);
                Check(false, name + " refuses");
            }
            catch (CardException e)
            {
                Check(true, name + " refuses (" + e.Message + ")");
            }
            SameBytes(rec.Wire(), before, name + " leaves the wire byte-equal");
        }

        // Every edit case in the corpus matches its expected wire or refusal
        // class (ImplementationsAgree).
        foreach (CorpusCase c in CorpusCases("edit"))
        {
            Record caseRec = Record.Load(File.ReadAllBytes(c.Input), out _);
            string refusedPath = "tables/cards/conformance/expected/" + c.Name + ".refused";
            try
            {
                ApplyEditLine(caseRec, c.Edit);
                if (File.Exists(refusedPath))
                {
                    Check(false, c.Name + " is refused by the Go leg but edited here");
                    continue;
                }
                SameBytes(caseRec.Wire(),
                    File.ReadAllBytes("tables/cards/conformance/expected/" + c.Name + ".wire"),
                    c.Name + " edits to the expected wire");
            }
            catch (CardException e)
            {
                if (!File.Exists(refusedPath))
                {
                    Check(false, c.Name + " was refused but the Go leg edits it (" + e.Message + ")");
                    continue;
                }
                SameBytes(Encoding.UTF8.GetBytes(RefusalClass(e) + "\n"),
                    File.ReadAllBytes(refusedPath),
                    c.Name + " refuses with the expected class");
            }
        }
    }

    // ApplyEditLine applies one manifest edit line through the Set* edit of
    // the same name (the driver's twin; tables/cards/edit.go).
    private static void ApplyEditLine(Record rec, string edit)
    {
        int eq = edit.IndexOf('=');
        string key = edit.Substring(0, eq);
        string value = edit.Substring(eq + 1);
        switch (key)
        {
            case "needs": rec.SetNeeds(value.Length == 0 ? new string[0] : value.Split(',')); return;
            case "paths": rec.SetPaths(value.Length == 0 ? new string[0] : value.Split(',')); return;
            case "tier":
                if (value == "flash") { rec.SetTier(Tier.flash); return; }
                if (value == "pro") { rec.SetTier(Tier.pro); return; }
                if (value == "heavy") { rec.SetTier(Tier.heavy); return; }
                if (value == "frontier") { rec.SetTier(Tier.frontier); return; }
                throw new CardException("unknown tier \"" + value + "\"");
            case "priority":
                if (value == "normal") { rec.SetPriority(Priority.normal); return; }
                if (value == "high") { rec.SetPriority(Priority.high); return; }
                if (value == "critical") { rec.SetPriority(Priority.critical); return; }
                if (value == "blocker") { rec.SetPriority(Priority.blocker); return; }
                throw new CardException("unknown priority \"" + value + "\"");
            case "column":
                foreach (string v in new[] { "waiting", "ready", "working", "review", "merging", "landed", "plan", "check" })
                {
                    if (value == v)
                    {
                        rec.SetColumn((Column)Enum.Parse(typeof(Column), v));
                        return;
                    }
                }
                throw new CardException("unknown column \"" + value + "\"");
            case "holder": rec.SetHolder(value); return;
        }
        throw new CardException("unknown edit \"" + key + "\"");
    }

    private static bool EqualBytes(byte[] a, byte[] b)
    {
        if (a.Length != b.Length) return false;
        for (int i = 0; i < a.Length; i++) if (a[i] != b[i]) return false;
        return true;
    }

    private static string[] LongList(int n)
    {
        var list = new string[n];
        for (int i = 0; i < n; i++) list[i] = "e";
        return list;
    }

    // TestViewWalksEveryField proves the generated view (docs/SPEC-TABLES.md
    // §8) is the vocabulary: the walk over Schema.UnitView()'s descriptors
    // matches an explicit vocabulary written here — not derived from the
    // descriptors — and the corpus's expected view.txt, and a loaded and an
    // edited record's scalar, list and body values walk through the same
    // descriptors (tla/CardHops.tla: ImplementationsAgree).
    private static void TestViewWalksEveryField()
    {
        // The explicit vocabulary: one line per field of every table, tables
        // sorted by name, fields in declaration order
        // (tables/cards/conformance/expected/view.txt).
        string[] vocabulary =
        {
            "Card id string value 64",
            "Card sha string value 40",
            "Card tier Tier value -",
            "Card repo string value 96",
            "Card _base string value 96",
            "Card kind string value 24",
            "Card priority Priority value -",
            "Card stream string value 32",
            "Card paths PathText list -",
            "Card _new PathText list -",
            "Card shared PathText list -",
            "Card needs CardID list -",
            "Card test string value 192",
            "Card done string blob -",
            "Card deadline_minutes int32 value -",
            "Card source string blob -",
            "Card epoch int32 value -",
            "Card column Column value -",
            "Card holder string value 32",
            "Card attempt int32 value -",
            "Card head string value 40",
            "Card branch string value 128",
            "Card body string blob -",
            "Card extra KeyValue list -",
            "Card consumers Consumer list -",
            "CardID s string value 64",
            "Consumer kind string value 16",
            "Consumer who string value 32",
            "Consumer route string value 48",
            "Consumer model string value 64",
            "Consumer started int64 value -",
            "Consumer ended int64 value -",
            "Consumer outcome string value 16",
            "Consumer line string value 200",
            "Consumer cost_micro_usd int64 value -",
            "KeyValue key string value 32",
            "KeyValue value string blob -",
            "PathText s string value 192",
        };

        var b = new StringBuilder();
        foreach (ViewType t in Schema.UnitView().Tables.Span)
        {
            foreach (TableFieldInfo f in t.Type.Fields)
            {
                string shape = "value";
                if (f.PointerTypeId != 0)
                {
                    shape = "blob";
                }
                else if (f.IsArray && f.ArrayBound == int.MaxValue)
                {
                    shape = "list";
                }
                string bound = "-";
                if (shape == "value" && f.ArrayBound > 0)
                {
                    bound = f.ArrayBound.ToString();
                }
                b.Append(t.Name).Append(' ').Append(f.Name).Append(' ')
                    .Append(f.TypeName).Append(' ').Append(shape).Append(' ')
                    .Append(bound).Append('\n');
            }
        }
        byte[] walked = Encoding.UTF8.GetBytes(b.ToString());
        string walkedText = Encoding.UTF8.GetString(walked);
        string[] walkedLines = walkedText.Split('\n');
        Check(walkedLines.Length == vocabulary.Length + 1 && walkedLines[vocabulary.Length] == "",
            "the view walks " + vocabulary.Length + " fields, walked " + (walkedLines.Length - 1));
        for (int i = 0; i < vocabulary.Length; i++)
        {
            Check(walkedLines[i] == vocabulary[i],
                "the view's line " + (i + 1) + " is \"" + vocabulary[i] + "\", walked \"" + walkedLines[i] + "\"");
        }
        SameBytes(walked, File.ReadAllBytes("tables/cards/conformance/expected/view.txt"),
            "the view is the corpus's expected view.txt");

        // A loaded record's scalar, list and body values walk through the
        // descriptors (§8.1: the descriptor carries the accessors).
        Record rec = Record.Load(
            File.ReadAllBytes("tables/cards/conformance/cases/card-populated.wire"), out _);
        TableFieldInfo Field(string table, string name)
        {
            foreach (ViewType t in Schema.UnitView().Tables.Span)
            {
                if (t.Name != table) continue;
                foreach (TableFieldInfo f in t.Type.Fields)
                {
                    if (f.Name == name) return f;
                }
            }
            throw new CardException("no field " + table + "." + name);
        }
        Check(Field("Card", "tier").GetRaw(rec.Root, 0) == (ulong)Tier.flash,
            "the walk reads tier as flash");
        Check(Field("Card", "deadline_minutes").GetRaw(rec.Root, 0) == 45,
            "the walk reads deadline_minutes as 45");
        Check(Encoding.UTF8.GetString(Record.UsedBytes(
                Field("Card", "id").GetBuffer(rec.Root), Field("Card", "id").GetCount(rec.Root))) == "populated",
            "the walk reads id as populated");
        Check(Field("Card", "paths").GetCount(rec.Root) == 2,
            "the walk reads two paths");
        PathText firstPath = (PathText)Field("Card", "paths").GetChild(rec.Root, 0);
        Check(Encoding.UTF8.GetString(Record.UsedBytes(firstPath.S, firstPath.SLength)) == "path/one",
            "the walk reads the first path");
        TableBlob body = (TableBlob)Field("Card", "body").GetChild(rec.Root, 0);
        SameBytes(body.Data, rec.Root.Body.Data, "the walk reads the body byte for byte");

        // An edited record's values walk through the same descriptors: the
        // edits moved exactly the fields they name.
        rec.SetHolder("h");
        Check(Encoding.UTF8.GetString(Record.UsedBytes(
                Field("Card", "holder").GetBuffer(rec.Root), Field("Card", "holder").GetCount(rec.Root))) == "h",
            "the walk reads the edited holder as h");
        rec.SetNeeds(new[] { "a", "b" });
        Check(Field("Card", "needs").GetCount(rec.Root) == 2, "the walk reads two edited needs");
        CardID secondNeed = (CardID)Field("Card", "needs").GetChild(rec.Root, 1);
        Check(Encoding.UTF8.GetString(Record.UsedBytes(secondNeed.S, secondNeed.SLength)) == "b",
            "the walk reads the edited second need");
        SameBytes(((TableBlob)Field("Card", "body").GetChild(rec.Root, 0)).Data, rec.Root.Body.Data,
            "the walk still reads the body byte for byte after the edits");
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
        TestParseRenderMatchesGo();
        TestUpgradeMatchesGo();
        TestTaskHashFramesLogicalFields();
        TestFieldEditLeavesTheTaskHashAlone();
        TestViewWalksEveryField();
        if (_failed)
        {
            Console.WriteLine("cards tests: FAILED");
            return 1;
        }
        Console.WriteLine("cards tests: all checks passed");
        return 0;
    }
}
