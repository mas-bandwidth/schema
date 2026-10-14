// Program.cs — the C# leg of the card conformance set
// (tables/cards/conformance/FORMAT.md). It answers the driver contract
// test/conformance/README.md states — `<driver> <manifest> list` and
// `<driver> <manifest> <surface> <outdir>` — over the nine card surfaces
// through the facade in tables/cards/cs/Cards.cs: load, save, render, parse
// and bounds (a-cs-card-round-trips-and-renders-the-shared-corpus), then
// upgrade, edit, taskhash and view (this card). It is the concrete side of
// tla/CardHops.tla: ImplementationsAgree and HopOrderIrrelevant say every
// language's driver answers the same bytes, and this one must too.
//
// Two test-only environment switches turn exactly one surface red for the
// negative controls, never a tracked file:
//   CARDS_CS_SABOTAGE=1          changes one rendered byte (render alone);
//   CARDS_CS_UPGRADE_SABOTAGE=1  drops the retained fields (upgrade alone);
//   CARDS_CS_HASH_SABOTAGE=1     folds needs into the hash (taskhash alone).

using System;
using System.IO;
using System.Text;
using Cards;

internal static class Program
{
    private static readonly string[] Surfaces =
    {
        "load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"
    };

    private sealed class Case
    {
        public string Surface;
        public string Name;
        public string Input;
        public string[] Edit;
    }

    private static Case[] ReadManifest(string path)
    {
        var cases = new System.Collections.Generic.List<Case>();
        foreach (string raw in File.ReadAllLines(path))
        {
            string line = raw.Trim();
            if (line.Length == 0 || line.StartsWith("#", StringComparison.Ordinal)) continue;
            string[] f = line.Split(new[] { ' ', '\t' }, StringSplitOptions.RemoveEmptyEntries);
            if (f.Length < 3)
            {
                Console.Error.WriteLine("driver: malformed manifest line \"" + line + "\"");
                Environment.Exit(1);
            }
            var c = new Case { Surface = f[0], Name = f[1], Input = f[2], Edit = new string[0] };
            if (f.Length > 3)
            {
                c.Edit = new string[f.Length - 3];
                Array.Copy(f, 3, c.Edit, 0, c.Edit.Length);
            }
            cases.Add(c);
        }
        return cases.ToArray();
    }

    private static void WriteFile(string outdir, string name, string ext, byte[] data)
    {
        File.WriteAllBytes(Path.Combine(outdir, name + "." + ext), data);
    }

    private static void Refuse(string outdir, string name, string cls)
    {
        WriteFile(outdir, name, "refused", Encoding.UTF8.GetBytes(cls + "\n"));
    }

    // RefusalClass maps a facade error to one class of the closed list
    // FORMAT.md fixes, never free text, so every language can spell the
    // same refusal.
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

    // FieldName reads the collection name from a bounds error, whose text
    // is "cards: <field> has N entries, past the 512-entry ceiling ...".
    private static string FieldName(string msg)
    {
        string rest = msg.StartsWith("cards: ", StringComparison.Ordinal) ? msg.Substring("cards: ".Length) : msg;
        int i = rest.IndexOf(" has ", StringComparison.Ordinal);
        return i < 0 ? rest : rest.Substring(0, i);
    }

    // QuotedWord reads the first double-quoted token of an error, the
    // stranded header key a parse refusal names.
    private static string QuotedWord(string msg)
    {
        int i = msg.IndexOf('"');
        if (i < 0) return "";
        int j = msg.IndexOf('"', i + 1);
        return j < 0 ? "" : msg.Substring(i + 1, j - i - 1);
    }

    // WriteReport writes the read report, one Name=value line per counter
    // of the Go TableReport in declaration order
    // (tables/cards/conformance/FORMAT.md).
    private static void WriteReport(string outdir, string name, TableReport r)
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
        WriteFile(outdir, name, "report", Encoding.UTF8.GetBytes(b.ToString()));
    }

    // ViewText renders the generated view (docs/SPEC-TABLES.md §8): every
    // field of every table in the unit, tables in the generated view's order
    // and fields in declaration order, one "<table> <field> <kind> <shape>
    // <bound>" line each — the C# spelling of the Go driver's view() over
    // cards.UnitView() (tables/cards/cmd/cards-driver/main.go).
    private static byte[] ViewText()
    {
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
        return Encoding.UTF8.GetBytes(b.ToString());
    }

    // ApplyEdit applies one edit line — "needs=a,b", "tier=heavy",
    // "priority=blocker", "paths=x,y", "column=landed", "holder=h" — through
    // the Set* edit of the same name (tables/cards/edit.go).
    private static void ApplyEdit(Record rec, string[] edit)
    {
        if (edit.Length != 1)
        {
            throw new CardException("cards-driver: an edit line carries exactly one edit");
        }
        string line = edit[0];
        int eq = line.IndexOf('=');
        if (eq < 0)
        {
            throw new CardException("cards-driver: edit \"" + line + "\" has no =");
        }
        string key = line.Substring(0, eq);
        string value = line.Substring(eq + 1);
        switch (key)
        {
            case "needs": rec.SetNeeds(SplitList(value)); return;
            case "paths": rec.SetPaths(SplitList(value)); return;
            case "tier": rec.SetTier(ParseTier(value)); return;
            case "priority": rec.SetPriority(ParsePriority(value)); return;
            case "column": rec.SetColumn(ParseColumn(value)); return;
            case "holder": rec.SetHolder(value); return;
        }
        throw new CardException("cards-driver: unknown edit \"" + key + "\"");
    }

    private static string[] SplitList(string v)
    {
        if (v.Length == 0)
        {
            return new string[0];
        }
        return v.Split(',');
    }

    private static Tier ParseTier(string s)
    {
        switch (s)
        {
            case "flash": return Tier.flash;
            case "pro": return Tier.pro;
            case "heavy": return Tier.heavy;
            case "frontier": return Tier.frontier;
        }
        throw new CardException("cards-driver: unknown tier \"" + s + "\"");
    }

    private static Priority ParsePriority(string s)
    {
        switch (s)
        {
            case "normal": return Priority.normal;
            case "high": return Priority.high;
            case "critical": return Priority.critical;
            case "blocker": return Priority.blocker;
        }
        throw new CardException("cards-driver: unknown priority \"" + s + "\"");
    }

    private static Column ParseColumn(string s)
    {
        switch (s)
        {
            case "waiting": return Column.waiting;
            case "ready": return Column.ready;
            case "working": return Column.working;
            case "review": return Column.review;
            case "merging": return Column.merging;
            case "landed": return Column.landed;
            case "plan": return Column.plan;
            case "check": return Column.check;
        }
        throw new CardException("cards-driver: unknown column \"" + s + "\"");
    }

    private static void One(string surface, Case c, string outdir)
    {
        switch (surface)
        {
            case "load":
            {
                byte[] wire = File.ReadAllBytes(c.Input);
                Record rec = Record.Load(wire, out TableReport report);
                WriteFile(outdir, c.Name, "json", rec.ToJson());
                WriteReport(outdir, c.Name, report);
                break;
            }
            case "save":
            case "bounds":
            {
                byte[] text = File.ReadAllBytes(c.Input);
                try
                {
                    Record rec = Record.FromJson(text);
                    WriteFile(outdir, c.Name, "wire", rec.Wire());
                }
                catch (CardException e)
                {
                    Refuse(outdir, c.Name, RefusalClass(e));
                }
                break;
            }
            case "render":
            {
                byte[] wire = File.ReadAllBytes(c.Input);
                Record rec = Record.Load(wire, out _);
                byte[] text = rec.Render();
                if (Environment.GetEnvironmentVariable("CARDS_CS_SABOTAGE") == "1" && text.Length > 0)
                {
                    // The negative control: change one rendered byte.
                    text = (byte[])text.Clone();
                    text[0] = text[0] == (byte)'R' ? (byte)'S' : (byte)'R';
                }
                WriteFile(outdir, c.Name, "txt", text);
                break;
            }
            case "parse":
            {
                byte[] text = File.ReadAllBytes(c.Input);
                try
                {
                    Record rec = Record.Parse(text);
                    WriteFile(outdir, c.Name, "wire", rec.Wire());
                }
                catch (CardException e)
                {
                    Refuse(outdir, c.Name, RefusalClass(e));
                }
                break;
            }
            case "upgrade":
            {
                byte[] wire = File.ReadAllBytes(c.Input);
                try
                {
                    byte[] upgraded;
                    TableReport report;
                    if (Environment.GetEnvironmentVariable("CARDS_CS_UPGRADE_SABOTAGE") == "1")
                    {
                        // The negative control: drive the package-private
                        // retention seam with no id storage, so every
                        // retained field is dropped and the upgrade refuses
                        // rather than reproducing the expected wire.
                        upgraded = Record.UpgradeSeam(wire, Record.MaxRetainBytes, 0, out report);
                    }
                    else
                    {
                        upgraded = Record.Upgrade(wire, out report);
                    }
                    WriteReport(outdir, c.Name, report);
                    WriteFile(outdir, c.Name, "wire", upgraded);
                }
                catch (CardException e)
                {
                    Refuse(outdir, c.Name, RefusalClass(e));
                }
                break;
            }
            case "edit":
            {
                byte[] wire = File.ReadAllBytes(c.Input);
                Record rec = Record.Load(wire, out _);
                try
                {
                    ApplyEdit(rec, c.Edit);
                    WriteFile(outdir, c.Name, "wire", rec.Wire());
                }
                catch (CardException e)
                {
                    Refuse(outdir, c.Name, RefusalClass(e));
                }
                break;
            }
            case "taskhash":
            {
                byte[] wire = File.ReadAllBytes(c.Input);
                Record rec = Record.Load(wire, out _);
                bool sabotage = Environment.GetEnvironmentVariable("CARDS_CS_HASH_SABOTAGE") == "1";
                byte[] hash = Record.TaskHash(rec, sabotage);
                WriteFile(outdir, c.Name, "hex",
                    Encoding.UTF8.GetBytes(BitConverter.ToString(hash).Replace("-", "").ToLowerInvariant()));
                break;
            }
            case "view":
            {
                WriteFile(outdir, c.Name, "txt", ViewText());
                break;
            }
        }
    }

    private static int Main(string[] args)
    {
        if (args.Length < 2)
        {
            Console.Error.WriteLine("usage: driver <manifest> list\n       driver <manifest> <surface> <outdir>");
            return 2;
        }
        string manifest = args[0];
        string verb = args[1];
        if (verb == "list")
        {
            foreach (string s in Surfaces) Console.WriteLine(s);
            return 0;
        }
        if (args.Length < 3)
        {
            Console.Error.WriteLine("usage: driver <manifest> <surface> <outdir>");
            return 2;
        }
        string surface = verb;
        string outdir = args[2];
        if (Array.IndexOf(Surfaces, surface) < 0)
        {
            return 2; // not a card surface
        }
        Case[] cases = ReadManifest(manifest);
        Directory.CreateDirectory(outdir);
        foreach (Case c in cases)
        {
            if (c.Surface != surface) continue;
            One(surface, c, outdir);
        }
        return 0;
    }
}
