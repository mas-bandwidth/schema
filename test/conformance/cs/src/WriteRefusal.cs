// THE WRITE-REFUSAL SURFACES (testdata/conformance/tables/FORMAT.md). Each case
// loads a wire, sets ONE field to a value its declaration forbids (or the edge
// it admits), and writes it through one writer: `write-refusal` the
// variable-form Save the wire surface uses, `write-refusal-fixed` the fixed
// form's FixedSave as a batch of one, `write-refusal-message` SaveMessages as a
// batch of one, and `write-refusal-json` the text writer.
//
// C# is an assert leg: a value its declaration refuses is caller error on the
// write side and fires a Debug.Assert in a debug build, the idiom serialize.cs
// and the packet writer share. This driver is built Debug, so the asserts are
// live; a listener turns a fired assert into an exception for the length of
// one write, and the case answers "refused". The checks every build keeps
// (count, length, enum, union tag, non-finite float, narrow text) answer -1.
using System;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Text;

static partial class Program
{
    sealed class WriteRefusalAssert : Exception
    {
        public WriteRefusalAssert(string message) : base(message) { }
    }

    sealed class WriteRefusalListener : TraceListener
    {
        public override void Write(string message) { }
        public override void WriteLine(string message) { }
        public override void Fail(string message) { throw new WriteRefusalAssert(message); }
        public override void Fail(string message, string detailMessage) { throw new WriteRefusalAssert(message); }
    }

    // the write's answer: its length, -1 on a refusal by return, or null when
    // a write assert fired
    static long? WriteRefusalRun(Func<long> write)
    {
        TraceListener[] saved = new TraceListener[Trace.Listeners.Count];
        Trace.Listeners.CopyTo(saved, 0);
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new WriteRefusalListener());
        try
        {
            return write();
        }
        catch (WriteRefusalAssert)
        {
            return null;
        }
        finally
        {
            Trace.Listeners.Clear();
            Trace.Listeners.AddRange(saved);
        }
    }

    // a binary writer's verdict: "refused" on -1 or a fired write assert
    static string WriteRefusalSave(Func<long> write)
    {
        long? n = WriteRefusalRun(write);
        return n == null || n < 0 ? "refused" : "wrote";
    }

    static float WriteRefusalFloat(string text)
    {
        switch (text)
        {
            case "nan": return float.NaN;
            case "inf": return float.PositiveInfinity;
            case "-inf": return float.NegativeInfinity;
        }
        return float.Parse(text, NumberStyles.Float, CultureInfo.InvariantCulture);
    }

    // a `bytes` value: two hex digits a byte
    static byte[] WriteRefusalBytes(string hex)
    {
        byte[] b = new byte[hex.Length / 2];
        for (int i = 0; i < b.Length; i++) { b[i] = byte.Parse(hex.AsSpan(i * 2, 2), NumberStyles.HexNumber, CultureInfo.InvariantCulture); }
        return b;
    }

    // a `units` value: four hex digits a UTF-16 unit
    static char[] WriteRefusalUnits(string hex)
    {
        char[] u = new char[hex.Length / 4];
        for (int i = 0; i < u.Length; i++) { u[i] = (char)ushort.Parse(hex.AsSpan(i * 4, 4), NumberStyles.HexNumber, CultureInfo.InvariantCulture); }
        return u;
    }

    // Sets a narrow text's used length (filled with `a`) or its exact bytes.
    // false: the storage cannot hold the bytes.
    static bool WriteRefusalText(byte[] storage, ref int length, string kind, string text)
    {
        if (kind == "length")
        {
            int n = int.Parse(text, CultureInfo.InvariantCulture);
            // a length past storage is set as is, for the writer to refuse
            for (int i = 0; i < Math.Min(n, storage.Length); i++) { storage[i] = (byte)'a'; }
            length = n;
            return true;
        }
        byte[] bytes = WriteRefusalBytes(text);
        if (bytes.Length > storage.Length) { return false; }
        bytes.CopyTo(storage, 0);
        length = bytes.Length;
        return true;
    }

    // Sets the ONE field a case names. The mutation is per root, by path and
    // kind; a path this table does not know is an error, never a silent pass.
    // false: the leg's storage cannot hold the value at all.
    static bool WriteRefusalSet(Codec codec, byte[] wire, object value, string path, string kind, string text)
    {
        try
        {
            switch (value)
            {
                case Tabledemo.RootConfig root:
                {
                    Tabledemo.WeaponConfig w0 = root.Weapons[0];
                    switch (path + " " + kind)
                    {
                        case "weapons[0].penetration int": w0.Penetration = checked((int)long.Parse(text, CultureInfo.InvariantCulture)); return true;
                        case "weapons[0].channel int": w0.Channel = checked((uint)ulong.Parse(text, CultureInfo.InvariantCulture)); return true;
                        case "weapons[0].damage float": w0.Damage = WriteRefusalFloat(text); return true;
                        case "weapons[0].effect tag":
                        {
                            byte tag = checked((byte)ulong.Parse(text, CultureInfo.InvariantCulture));
                            w0.Effect.Type = (Tabledemo.EffectType)tag;
                            // a lawful tag takes that arm at its declared defaults
                            if (tag == 1) { w0.Effect.Buff = new Tabledemo.Buff(); }
                            if (tag == 2) { w0.Effect.Debuff = new Tabledemo.Debuff(); }
                            return true;
                        }
                        case "weapons count":
                        {
                            int n = int.Parse(text, CultureInfo.InvariantCulture);
                            // new elements are copies of element 0, each its own load;
                            // a count past storage is set as is, for the writer to refuse
                            for (int i = root.WeaponsCount; i < Math.Min(n, root.Weapons.Length); i++)
                            {
                                root.Weapons[i] = ((Tabledemo.RootConfig)codec.Load(wire, new Report())).Weapons[0];
                            }
                            root.WeaponsCount = n;
                            return true;
                        }
                        case "version_note length":
                        case "version_note bytes":
                            return WriteRefusalText(root.VersionNote, ref root.VersionNoteLength, kind, text);
                    }
                    break;
                }
                case Tabledemo.LoadoutConfig loadout:
                    switch (path + " " + kind)
                    {
                        case "grade ordinal": loadout.Grade = (Tabledemo.Grade)checked((byte)ulong.Parse(text, CultureInfo.InvariantCulture)); return true;
                    }
                    break;
                case Wide.Caption caption:
                    switch (path + " " + kind)
                    {
                        case "title units":
                        {
                            char[] units = WriteRefusalUnits(text);
                            if (units.Length > caption.Title.Length) { return false; }
                            units.CopyTo(caption.Title, 0);
                            caption.TitleLength = units.Length;
                            return true;
                        }
                    }
                    break;
                case Backenddemo.StorePurchase purchase:
                    switch (path + " " + kind)
                    {
                        case "quantity int": purchase.Quantity = checked((byte)ulong.Parse(text, CultureInfo.InvariantCulture)); return true;
                        case "currency ordinal": purchase.Currency = (Backenddemo.Currency)checked((byte)ulong.Parse(text, CultureInfo.InvariantCulture)); return true;
                        case "sku length":
                        case "sku bytes":
                            return WriteRefusalText(purchase.Sku, ref purchase.SkuLength, kind, text);
                    }
                    break;
                case Backenddemo.MatchResult match:
                    switch (path + " " + kind)
                    {
                        case "players[0].score int": match.Players[0].Score = checked((int)long.Parse(text, CultureInfo.InvariantCulture)); return true;
                    }
                    break;
            }
        }
        catch (OverflowException)
        {
            return false;
        }
        throw new InvalidOperationException("write-refusal: no setter for " + codec.Unit + "." + codec.Root + " " + path + " " + kind);
    }

    // The fixed form's FixedSave, as a batch of one, per root the battery names.
    static long WriteRefusalFixedSave(object value, byte[] buffer)
    {
        switch (value)
        {
            case Tabledemo.RootConfig root: return Tabledemo.Schema.RootConfigFixedSave(root, buffer);
            case Tabledemo.LoadoutConfig loadout: return Tabledemo.Schema.LoadoutConfigFixedSave(loadout, buffer);
            case Wide.Caption caption: return Wide.Schema.CaptionFixedSave(caption, buffer);
        }
        throw new InvalidOperationException("write-refusal-fixed: no FixedSave for " + value.GetType().Name);
    }

    // The text writer's verdict: "wrote" only when it returned text AND the
    // text spells each zero byte or unit of the value as \u0000.
    static string WriteRefusalJson(Codec codec, object value, string kind, string text)
    {
        byte[] buffer = new byte[1 << 20];
        long? n = WriteRefusalRun(() => codec.ToJson(value, buffer));
        if (n == null || n < 0) { return "refused"; }
        string json = Encoding.UTF8.GetString(buffer, 0, checked((int)n));
        var spelled = new StringBuilder();
        if (kind == "bytes") { foreach (byte b in WriteRefusalBytes(text)) { spelled.Append(b == 0 ? "\\u0000" : ((char)b).ToString()); } }
        else { foreach (char u in WriteRefusalUnits(text)) { spelled.Append(u == 0 ? "\\u0000" : u.ToString()); } }
        return json.Contains("\"" + spelled + "\"", StringComparison.Ordinal) ? "wrote" : "misspelled";
    }

    static int SurfaceWriteRefusal(string outDir, string surface)
    {
        // <surface> <case> <unit> <root> <wire> <path> <kind> <value>
        foreach (string[] f in Kind(surface))
        {
            if (f.Length < 8)
            {
                Console.Error.WriteLine("driver: " + surface + " line has " + f.Length + " fields");
                return 1;
            }
            string unit = f[2];
            if (surface == "write-refusal-message")
            {
                // the unit column is a connection key
                string[] connection = lines.Find(c => c[0] == "connection" && c[1] == f[2]);
                if (connection == null)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " names no connection " + f[2]);
                    return 1;
                }
                unit = connection[2];
            }
            Codec codec = Find(unit, f[3]);
            if (codec == null)
            {
                Console.Error.WriteLine("driver: " + surface + " " + f[1] + " names no codec " + unit + "." + f[3]);
                return 1;
            }
            byte[] wire = File.ReadAllBytes(f[4]);
            object value = codec.Load(wire, new Report());
            if (value == null)
            {
                Console.Error.WriteLine("driver: " + f[1] + " does not load");
                return 1;
            }
            string verdict;
            if (!WriteRefusalSet(codec, wire, value, f[5], f[6], f[7])) { verdict = "unrepresentable"; }
            else
            {
                byte[] buffer = new byte[1 << 20];
                switch (surface)
                {
                    case "write-refusal-fixed": verdict = WriteRefusalSave(() => WriteRefusalFixedSave(value, buffer)); break;
                    case "write-refusal-message": verdict = WriteRefusalSave(() => codec.MessageSave(value, buffer)); break;
                    case "write-refusal-json": verdict = WriteRefusalJson(codec, value, f[6], f[7]); break;
                    default: verdict = WriteRefusalSave(() => codec.Save(value, buffer)); break;
                }
            }
            File.WriteAllText(Path.Combine(outDir, f[1]), verdict + "\n");
        }
        return 0;
    }
}
