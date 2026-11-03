// THE WRITE-REFUSAL SURFACE (testdata/conformance/tables/FORMAT.md). Each case
// loads a wire, sets ONE field to a value its declaration forbids (or the edge
// it admits), and saves through the variable-form Save the wire surface uses.
//
// C# is an assert leg: a ranged integer or a bits(N) value outside its field is
// caller error on the write side and fires a Debug.Assert in a debug build, the
// idiom serialize.cs and the packet writer share. This driver is built Debug,
// so the asserts are live; a listener turns a fired assert into an exception
// for the length of one save, and the case answers "refused". The checks every
// build keeps (count, length, enum, union tag, non-finite float) answer -1.
using System;
using System.Diagnostics;
using System.Globalization;
using System.IO;

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

    // the save's verdict: "refused" on -1 or a fired write assert, else "wrote"
    static string WriteRefusalSave(Codec codec, object value)
    {
        TraceListener[] saved = new TraceListener[Trace.Listeners.Count];
        Trace.Listeners.CopyTo(saved, 0);
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new WriteRefusalListener());
        try
        {
            return codec.Save(value, new byte[1 << 20]) < 0 ? "refused" : "wrote";
        }
        catch (WriteRefusalAssert)
        {
            return "refused";
        }
        finally
        {
            Trace.Listeners.Clear();
            Trace.Listeners.AddRange(saved);
        }
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
                        {
                            int n = int.Parse(text, CultureInfo.InvariantCulture);
                            for (int i = 0; i < Math.Min(n, root.VersionNote.Length); i++) { root.VersionNote[i] = (byte)'a'; }
                            root.VersionNoteLength = n;
                            return true;
                        }
                    }
                    break;
                }
                case Tabledemo.LoadoutConfig loadout:
                    switch (path + " " + kind)
                    {
                        case "grade ordinal": loadout.Grade = (Tabledemo.Grade)checked((byte)ulong.Parse(text, CultureInfo.InvariantCulture)); return true;
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

    static int SurfaceWriteRefusal(string outDir)
    {
        // write-refusal <case> <unit> <root> <wire> <path> <kind> <value>
        foreach (string[] f in Kind("write-refusal"))
        {
            if (f.Length < 8)
            {
                Console.Error.WriteLine("driver: write-refusal line has " + f.Length + " fields");
                return 1;
            }
            Codec codec = Find(f[2], f[3]);
            if (codec == null)
            {
                Console.Error.WriteLine("driver: write-refusal " + f[1] + " names no codec " + f[2] + "." + f[3]);
                return 1;
            }
            byte[] wire = File.ReadAllBytes(f[4]);
            object value = codec.Load(wire, new Report());
            if (value == null)
            {
                Console.Error.WriteLine("driver: " + f[1] + " does not load");
                return 1;
            }
            string verdict = WriteRefusalSet(codec, wire, value, f[5], f[6], f[7]) ? WriteRefusalSave(codec, value) : "unrepresentable";
            File.WriteAllText(Path.Combine(outDir, f[1]), verdict + "\n");
        }
        return 0;
    }
}
