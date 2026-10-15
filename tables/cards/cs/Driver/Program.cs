// Program.cs — the C# leg of the card conformance set
// (tables/cards/conformance/FORMAT.md). It answers the driver contract
// test/conformance/README.md states — `<driver> <manifest> list` and
// `<driver> <manifest> <surface> <outdir>` — over the five surfaces this
// card owns (load, save, render, parse, bounds), through the facade in
// tables/cards/cs/Cards.cs. The upgrade, edit, taskhash and view surfaces
// belong to a-cs-card-upgrades-edits-and-walks-the-shared-corpus and are
// answered absent (exit 2). It is the concrete side of tla/CardHops.tla:
// every language's driver must answer the same bytes.
//
// A test-only environment switch, CARDS_CS_SABOTAGE=1, changes one rendered
// byte so the negative control can prove the render surface goes red while
// the other four stay green.

using System;
using System.IO;
using System.Text;
using Cards;

internal static class Program
{
    private static readonly string[] Surfaces = { "load", "save", "render", "parse", "bounds" };

    private sealed class Case
    {
        public string Surface;
        public string Name;
        public string Input;
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
            cases.Add(new Case { Surface = f[0], Name = f[1], Input = f[2] });
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
            return 2; // upgrade, edit, taskhash, view: another card's surfaces
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
