// THE 2026-11-22 SURFACES on the C# (unity) leg (test/conformance/README.md,
// "The 2026-11-22 surfaces ask what no surface asked before"): the
// write-refusal kinds `elems`, `mask`, `dupkey` and `point`, the fresh-value
// wire `-`, and the surfaces write-refusal-cook, write-refusal-block,
// announce, read-into and json-write-into. The units (tblsurf, tblann4096,
// tblann4097) are built from the manifest's unit lines like every other
// (make/unity.mk). Every answer is what the generated runtime returned.
using System;
using System.Globalization;
using System.IO;
using System.Reflection;

namespace SchemaUnityConformance
{
    internal static partial class Driver
    {
        // The root a fresh copy comes from: the row's wire, or the root's
        // fresh value when the wire is `-`.
        private static object FreshRoot(ICodec codec, byte[] wire)
        {
            return wire == null ? codec.Fresh() : codec.Load(wire, new DriverReport());
        }

        private static object ElementZero(ICodec codec, byte[] wire, object holder, FieldInfo f)
        {
            object fresh = FreshRoot(codec, wire);
            if (fresh == null || fresh.GetType() != holder.GetType())
            {
                throw new InvalidOperationException("write-refusal: a copied element is taken from the root, and " + f.Name + " is not a root field");
            }
            return BufferGet(f.GetValue(fresh), 0);
        }

        // `elems`: the element count of a fixed [N]T or a keyed [E]T. The
        // Unity dialect stores both inline as a BufferN<T> struct of exactly N
        // slots, which cannot hold any other count (unrepresentable); a T[]
        // field takes the new length.
        private static bool MutateElems(ICodec codec, byte[] wire, object holder, FieldInfo f, string text)
        {
            int n = int.Parse(text, CultureInfo.InvariantCulture);
            object current = f.GetValue(holder);
            if (current is Array arr)
            {
                if (n < 0) { return false; }
                if (n == arr.Length) { return true; }
                Array resized = Array.CreateInstance(arr.GetType().GetElementType(), n);
                for (int i = 0; i < n; i++)
                {
                    resized.SetValue(i < arr.Length ? arr.GetValue(i) : ElementZero(codec, wire, holder, f), i);
                }
                f.SetValue(holder, resized);
                return true;
            }
            return BufferCapacity(current) == n;
        }

        // `mask`: a flags field's raw bits; bits its storage cannot hold are
        // unrepresentable (the OverflowException WriteRefusalSet catches).
        private static bool MutateMask(object holder, FieldInfo f, string text)
        {
            ulong bits = ulong.Parse(text, CultureInfo.InvariantCulture);
            Type ft = f.FieldType;
            if (ft.IsEnum)
            {
                object raw = Convert.ChangeType(bits, Enum.GetUnderlyingType(ft), CultureInfo.InvariantCulture);
                f.SetValue(holder, Enum.ToObject(ft, raw));
                return true;
            }
            f.SetValue(holder, Convert.ChangeType(bits, ft, CultureInfo.InvariantCulture));
            return true;
        }

        // `dupkey 1`: a copy of the map's first entry appended, so its key is
        // held twice; `dupkey 0` leaves the map as loaded.
        private static bool MutateDupkey(ICodec codec, byte[] wire, object holder, FieldInfo f, string text)
        {
            if (text == "0") { return true; }
            if (text != "1") { throw new InvalidOperationException("write-refusal: dupkey takes 0 or 1, not " + text); }
            FieldInfo cf = holder.GetType().GetField(f.Name + "Count", BindingFlags.Public | BindingFlags.Instance);
            if (cf == null) { throw new InvalidOperationException("write-refusal: dupkey on " + f.Name + ", which has no count"); }
            int count = (int)cf.GetValue(holder);
            if (count < 1) { throw new InvalidOperationException("write-refusal: dupkey on the empty map " + f.Name); }
            if (!(f.GetValue(holder) is Array arr)) { throw new InvalidOperationException("write-refusal: dupkey on " + f.Name + ", which is not an entry array"); }
            if (arr.Length <= count)
            {
                Array grown = Array.CreateInstance(arr.GetType().GetElementType(), count + 1);
                Array.Copy(arr, grown, count);
                arr = grown;
            }
            arr.SetValue(ElementZero(codec, wire, holder, f), count);
            f.SetValue(holder, arr);
            cf.SetValue(holder, count + 1);
            return true;
        }

        private static object WalkPath(object at, string[] segments, int n, string path)
        {
            for (int i = 0; i < n; i++)
            {
                FieldInfo f = at.GetType().GetField(PascalName(segments[i]), BindingFlags.Public | BindingFlags.Instance);
                if (f == null) { throw new InvalidOperationException("write-refusal: point path " + path + " names no field " + segments[i] + " on " + at.GetType().FullName); }
                at = f.GetValue(at);
                if (at == null) { throw new InvalidOperationException("write-refusal: point path " + path + " is null at " + segments[i]); }
            }
            return at;
        }

        // `point`: the pointer at path set to the node the pointer at `from`
        // points at (a cycle when that node encloses path).
        private static bool MutatePoint(object root, string path, string from)
        {
            string[] fromSegments = from.Split('.');
            object target = WalkPath(root, fromSegments, fromSegments.Length, from);
            string[] segments = path.Split('.');
            object parent = WalkPath(root, segments, segments.Length - 1, path);
            if (parent.GetType().IsValueType) { throw new InvalidOperationException("write-refusal: point " + path + " sits in a value type"); }
            FieldInfo f = parent.GetType().GetField(PascalName(segments[segments.Length - 1]), BindingFlags.Public | BindingFlags.Instance);
            if (f == null || !f.FieldType.IsInstanceOfType(target))
            {
                throw new InvalidOperationException("write-refusal: point " + path + " cannot hold the node at " + from);
            }
            f.SetValue(parent, target);
            return true;
        }

        // write-refusal-cook: CookMeasure, then Cook in little-endian order.
        private static string WriteRefusalCookVerdict(ICodec codec, object value, string name)
        {
            if (!codec.HasCookWrite) { throw new InvalidOperationException("write-refusal-cook " + name + ": " + codec.Unit + "." + codec.Root + " has no cook writer"); }
            return WriteRefusalBinaryVerdict(() =>
            {
                long size = codec.CookMeasure(value);
                if (size < 0) { return -1; }
                byte[] bytes = new byte[checked((int)size)];
                return codec.Cook(value, bytes, false) ? size : -1;
            });
        }

        // write-refusal-block: the Unity block builder is <Root>Block.Begin(
        // bytes, baseOffset, refusal), handed the value's counts as its own
        // parameters where the root has an out-of-line array and nothing else
        // of the value: a root with none (StorePurchase, MatchResult) is
        // handed no count, no used length and no value, which the caller then
        // writes into the block with no check.
        private static string WriteRefusalBlockVerdict(ICodec codec, string name)
        {
            Type schema = FindSchemaType(codec.UnitAssembly);
            Type blockType = codec.UnitAssembly.GetType(schema.Namespace + "." + codec.Root + "Block");
            Type refusalType = schema.GetNestedType("TableBlockRefusal", BindingFlags.Public);
            MethodInfo begin = blockType?.GetMethod("Begin", BindingFlags.Public | BindingFlags.Static, null, new[] { typeof(byte[]), typeof(int), refusalType }, null);
            FieldInfo maxBytes = blockType?.GetField("BlockMaxBytes", BindingFlags.Public | BindingFlags.Static);
            if (begin == null || maxBytes == null)
            {
                throw new InvalidOperationException("write-refusal-block " + name + ": " + codec.Unit + "." + codec.Root + " has no block builder Begin(byte[], int, TableBlockRefusal)");
            }
            int bytes = (int)maxBytes.GetRawConstantValue();
            return WriteRefusalBinaryVerdict(() => begin.Invoke(null, new object[] { new byte[bytes], 0, Activator.CreateInstance(refusalType) }) == null ? -1 : 1);
        }

        // ---- announce <case> <unit> <root> <mode> <input>

        private interface IAnnounceOps
        {
            byte[] Write();
            bool Reads(bool strict, byte[] announcement);
        }

        private delegate int AnnounceMeasureOf();
        private delegate ReadOnlySpan<byte> AnnounceOf();
        private delegate bool AnnounceReadOf<TVocab, TRep>(TVocab vocabulary, ReadOnlySpan<byte> bytes, ref TRep report);

        private sealed class AnnounceOps<TVocab, TRep> : IAnnounceOps where TVocab : class, new() where TRep : struct
        {
            private readonly AnnounceMeasureOf measure;
            private readonly AnnounceOf announce;
            private readonly AnnounceReadOf<TVocab, TRep> read;
            private readonly FieldInfo fRefused = typeof(TRep).GetField("Refused");
            private readonly FieldInfo fDamaged = typeof(TRep).GetField("Damaged");
            private readonly FieldInfo fStrict = typeof(TVocab).GetField("Strict");

            public AnnounceOps(Type schema)
            {
                const BindingFlags Flags = BindingFlags.Public | BindingFlags.Static;
                MethodInfo m = schema.GetMethod("AnnounceMeasure", Flags, null, Type.EmptyTypes, null);
                MethodInfo a = schema.GetMethod("Announce", Flags, null, Type.EmptyTypes, null);
                MethodInfo r = schema.GetMethod("AnnounceRead", Flags, null, new[] { typeof(TVocab), typeof(ReadOnlySpan<byte>), typeof(TRep).MakeByRefType() }, null);
                if (m == null || a == null || r == null || fStrict == null || fRefused == null)
                {
                    throw new InvalidOperationException("announce: " + schema.FullName + " has no AnnounceMeasure/Announce/AnnounceRead or no strict flag");
                }
                measure = (AnnounceMeasureOf)m.CreateDelegate(typeof(AnnounceMeasureOf));
                announce = (AnnounceOf)a.CreateDelegate(typeof(AnnounceOf));
                read = (AnnounceReadOf<TVocab, TRep>)r.CreateDelegate(typeof(AnnounceReadOf<TVocab, TRep>));
            }

            // null is a refusal (a negative measure)
            public byte[] Write()
            {
                int size = measure();
                if (size < 0) { return null; }
                byte[] bytes = announce().ToArray();
                if (size != bytes.Length) { throw new InvalidOperationException("announce: measures " + size + " and announces " + bytes.Length + " bytes"); }
                return bytes;
            }

            public bool Reads(bool strict, byte[] announcement)
            {
                var vocabulary = new TVocab();
                if (strict) { fStrict.SetValue(vocabulary, true); }
                TRep report = default;
                bool ok = read(vocabulary, announcement, ref report);
                object boxed = report;
                return ok && !(bool)fRefused.GetValue(boxed) && !(fDamaged != null && (bool)fDamaged.GetValue(boxed));
            }
        }

        private static IAnnounceOps AnnounceOpsFor(string unitKey)
        {
            Assembly asm = LoadUnit(UnitsRoot(), unitKey);
            Type schema = FindSchemaType(asm);
            Type vocab = schema?.GetNestedType("TableVocabulary", BindingFlags.Public);
            Type report = schema?.GetNestedType("TableReport", BindingFlags.Public);
            if (vocab == null || report == null) { throw new InvalidOperationException("announce: unit " + unitKey + " has no vocabulary"); }
            return (IAnnounceOps)Activator.CreateInstance(typeof(AnnounceOps<,>).MakeGenericType(vocab, report), schema);
        }

        private static int SurfaceAnnounce(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("announce"))
            {
                if (f.Length != 6) { Console.Error.WriteLine("driver: announce line has " + f.Length + " fields"); return 1; }
                string answer = Path.Combine(outDir, f[1]);
                switch (f[4])
                {
                    case "write":
                    {
                        IAnnounceOps ops = AnnounceOpsFor(f[2]);
                        byte[] bytes;
                        // a writer that throws rather than refusing answers
                        // that, and the next row still runs
                        try { bytes = ops.Write(); }
                        catch (TypeInitializationException e) { File.WriteAllText(answer, "threw " + (e.InnerException ?? e).GetType().Name + "\n"); break; }
                        if (bytes == null) { File.WriteAllText(answer, "refused\n"); }
                        else { File.WriteAllBytes(answer, bytes); }
                        break;
                    }
                    case "strict":
                    case "lenient":
                        File.WriteAllText(answer, AnnounceOpsFor(f[2]).Reads(f[4] == "strict", File.ReadAllBytes(f[5])) ? "read\n" : "refused\n");
                        break;
                    case "send":
                    {
                        ICodec codec = FindCodec(f[2], f[3]);
                        if (codec == null || !codec.HasMessage) { Console.Error.WriteLine("driver: announce " + f[1] + " names no message codec " + f[2] + "." + f[3]); return 1; }
                        object value;
                        if (f[5] == "-") { value = codec.Fresh(); }
                        else
                        {
                            var report = new DriverReport();
                            value = codec.Load(File.ReadAllBytes(f[5]), report);
                            if (StrictRefused(value, report)) { Console.Error.WriteLine("driver: announce " + f[1] + " input does not load"); return 1; }
                        }
                        // SaveMessages with NO announcement read or written on
                        // this connection: the writer takes no vocabulary
                        byte[] saved = null;
                        long? n = RunTrappingAsserts(() =>
                        {
                            long size = codec.MessageMeasure(value);
                            if (size < 0) { return -1; }
                            byte[] buffer = new byte[checked((int)size)];
                            long wrote = codec.MessageSave(value, buffer);
                            if (wrote < 0) { return -1; }
                            if (wrote != size) { throw new InvalidOperationException("SaveMessages wrote " + wrote + " bytes, measured " + size); }
                            saved = buffer;
                            return wrote;
                        }, out string threw);
                        if (threw != null) { File.WriteAllText(answer, threw + "\n"); }
                        else if (n == null || n < 0) { File.WriteAllText(answer, "refused\n"); }
                        else { File.WriteAllBytes(answer, saved); }
                        break;
                    }
                    default:
                        Console.Error.WriteLine("driver: announce " + f[1] + " has the unknown mode " + f[4]);
                        return 1;
                }
            }
            return 0;
        }

        // ---- read-into <case> <unit> <root> <form> <lawful> <damaged> <count> <announcement>

        private static int SurfaceReadInto(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("read-into"))
            {
                if (f.Length != 9) { Console.Error.WriteLine("driver: read-into line has " + f.Length + " fields"); return 1; }
                string answer = Path.Combine(outDir, f[1]);
                if (f[4] == "fixed-nocap")
                {
                    // the Unity fixed load's record capacity is the span of
                    // values it is handed: no call omits it (FL-17)
                    File.WriteAllText(answer, "unrepresentable\n");
                    continue;
                }
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null) { Console.Error.WriteLine("driver: read-into " + f[1] + " names no codec " + f[2] + "." + f[3]); return 1; }
                int count = int.Parse(f[7], CultureInfo.InvariantCulture);
                byte[] announcement = f[8] == "-" ? null : File.ReadAllBytes(f[8]);
                File.WriteAllBytes(answer, codec.ReadInto(f[4], File.ReadAllBytes(f[5]), File.ReadAllBytes(f[6]), count, announcement));
            }
            return 0;
        }

        // ---- json-write-into <case> <unit> <root> <wire>

        private static int SurfaceJsonWriteInto(string outDir)
        {
            Directory.CreateDirectory(outDir);
            // THE PROCESS LOCALE (FL-14): .NET formats numbers by the current
            // culture, and CultureInfo.CurrentCulture is its own call for the
            // process's locale; the harness names a comma-decimal one.
            string locale = Environment.GetEnvironmentVariable("SCHEMA_COMMA_LOCALE");
            if (string.IsNullOrEmpty(locale)) { Console.Error.WriteLine("driver: json-write-into: SCHEMA_COMMA_LOCALE is not set"); return 1; }
            string name = locale.Split('.')[0].Replace('_', '-');
            CultureInfo culture;
            try { culture = CultureInfo.GetCultureInfo(name, true); }
            catch (CultureNotFoundException e) { Console.Error.WriteLine("driver: json-write-into: the locale " + locale + " is refused: " + e.Message); return 1; }
            CultureInfo.DefaultThreadCurrentCulture = culture;
            CultureInfo.CurrentCulture = culture;
            if (CultureInfo.CurrentCulture.NumberFormat.NumberDecimalSeparator != ",")
            {
                Console.Error.WriteLine("driver: json-write-into: the process locale " + CultureInfo.CurrentCulture.Name + " is not comma-decimal");
                return 1;
            }
            foreach (string[] f in Kind("json-write-into"))
            {
                if (f.Length != 5) { Console.Error.WriteLine("driver: json-write-into line has " + f.Length + " fields"); return 1; }
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasJson) { Console.Error.WriteLine("driver: json-write-into " + f[1] + " names no text codec " + f[2] + "." + f[3]); return 1; }
                var report = new DriverReport();
                object value = codec.Load(File.ReadAllBytes(f[4]), report);
                if (StrictRefused(value, report)) { Console.Error.WriteLine("driver: json-write-into " + f[1] + " does not load"); return 1; }
                string answer = Path.Combine(outDir, f[1]);
                long size = codec.ToJsonMeasure(value);
                if (size < 0) { File.WriteAllText(answer, "refused\n"); continue; }
                byte[] buffer = new byte[checked((int)size) + 64];
                for (int i = 0; i < buffer.Length; i++) { buffer[i] = 0xA5; }
                long n = codec.ToJson(value, buffer);
                if (n < 0) { File.WriteAllText(answer, "refused\n"); continue; }
                bool tail = false;
                for (long i = n; i < buffer.Length; i++) { if (buffer[i] != 0xA5) { tail = true; break; } }
                if (tail) { File.WriteAllText(answer, "tail-written\n"); continue; }
                byte[] text = new byte[checked((int)n)];
                Array.Copy(buffer, text, text.Length);
                File.WriteAllBytes(answer, text);
            }
            return 0;
        }
    }
}
