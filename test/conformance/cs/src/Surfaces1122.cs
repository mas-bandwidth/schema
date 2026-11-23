// THE 2026-11-22 SURFACES (test/conformance/README.md, "The 2026-11-22
// surfaces ask what no surface asked before"; docs/SPEC-CONFORMANCE.md §2):
// the units test/tables/surfaces (tblsurf), announce4096 and announce4097,
// the write-refusal kinds `elems`, `mask`, `dupkey` and `point`, and the
// surfaces write-refusal-cook, write-refusal-block, announce, read-into and
// json-write-into. Every answer is what the generated runtime returned; the
// driver only reports it.
using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Reflection;

static partial class Program
{
    static Report Copy(Surfaces.TableReport r)
    {
        return new Report { Unknown = r.Unknown, Widened = r.Widened, Refused = r.Refused, Malformed = r.Damaged };
    }

    delegate bool SurfCookOf<T>(T value, Span<byte> bytes, Surfaces.TableByteOrder order);
    delegate long SurfSaveMessagesOf<T>(T[] values, Span<byte> bytes, Surfaces.TableReport report);
    delegate Surfaces.Schema.TableWire.Verdict SurfLoadMessagesOf<T>(T[] values, ReadOnlySpan<byte> bytes, Surfaces.TableVocabulary vocabulary, Surfaces.TableReport report, out int count);

    // The cook writer of one tblsurf root.
    static void BindSurfCook<T>(string root, Func<T, long> measure, SurfCookOf<T> cook)
    {
        Codec c = Find("tblsurf", root);
        c.CookMeasure = value => measure((T)value);
        c.Cook = (value, bytes, big) => cook((T)value, bytes, big ? Surfaces.TableByteOrder.Big : Surfaces.TableByteOrder.Little);
    }

    // The message form of one fixed tblsurf root, against the unit's own
    // announcement (the connection surfaces_conn is that announcement).
    static void BindSurfMessage<T>(string root, Func<T[], long> measure, SurfSaveMessagesOf<T> save, SurfLoadMessagesOf<T> load) where T : new()
    {
        Codec c = Find("tblsurf", root);
        c.Announcement = Surfaces.Schema.Announce().ToArray();
        var ownVocabulary = new Surfaces.TableVocabulary();
        Surfaces.Schema.AnnounceRead(ownVocabulary, c.Announcement, new Surfaces.TableReport());
        c.MessageLoad = (announcement, bytes, report) =>
        {
            var vocabulary = ReferenceEquals(announcement, c.Announcement) ? ownVocabulary : new Surfaces.TableVocabulary();
            var inner = new Surfaces.TableReport();
            if (!ReferenceEquals(announcement, c.Announcement)) { Surfaces.Schema.AnnounceRead(vocabulary, announcement, inner); }
            var values = new T[256];
            for (int i = 0; i < (bytes.Length >= 2 ? bytes[1] + 1 : 1); i++) { values[i] = new T(); }
            int cnt = 0;
            if (!inner.Refused && !inner.Damaged)
            {
                bool ok = load(values, bytes, vocabulary, inner, out cnt) == Surfaces.Schema.TableWire.Verdict.Ok;
                if (!ok && !inner.Refused && !inner.Damaged) { inner.Refused = true; }
            }
            Fill(report, Copy(inner));
            return (!report.Refused && !report.Malformed && cnt > 1) ? (object)values[..cnt] : values[0];
        };
        c.MessageMeasure = value => value is T[] batch ? measure(batch) : measure(new T[] { (T)value });
        c.MessageSave = (value, bytes) => value is T[] batch ? save(batch, bytes, null) : save(new T[] { (T)value }, bytes, null);
    }

    static void RegisterSurfaces1122()
    {
        codecs.Add(Row<Surfaces.Chain, Surfaces.TableReport>("tblsurf", "Chain", () => new Surfaces.TableReport(), Copy,
            Surfaces.Schema.ChainLoad, Surfaces.Schema.ChainMeasure, Surfaces.Schema.ChainSave,
            Surfaces.Schema.ChainFromJson, Surfaces.Schema.ChainToJsonMeasure, Surfaces.Schema.ChainToJson, Surfaces.Schema.ChainLoadMeasure));
        codecs.Add(Row<Surfaces.Holder, Surfaces.TableReport>("tblsurf", "Holder", () => new Surfaces.TableReport(), Copy,
            Surfaces.Schema.HolderLoad, Surfaces.Schema.HolderMeasure, Surfaces.Schema.HolderSave,
            Surfaces.Schema.HolderFromJson, Surfaces.Schema.HolderToJsonMeasure, Surfaces.Schema.HolderToJson, Surfaces.Schema.HolderLoadMeasure));
        codecs.Add(Row<Surfaces.Counted, Surfaces.TableReport>("tblsurf", "Counted", () => new Surfaces.TableReport(), Copy,
            Surfaces.Schema.CountedLoad, Surfaces.Schema.CountedMeasure, Surfaces.Schema.CountedSave,
            Surfaces.Schema.CountedFromJson, Surfaces.Schema.CountedToJsonMeasure, Surfaces.Schema.CountedToJson, Surfaces.Schema.CountedLoadMeasure));
        codecs.Add(Row<Surfaces.Narrow, Surfaces.TableReport>("tblsurf", "Narrow", () => new Surfaces.TableReport(), Copy,
            Surfaces.Schema.NarrowLoad, Surfaces.Schema.NarrowMeasure, Surfaces.Schema.NarrowSave,
            Surfaces.Schema.NarrowFromJson, Surfaces.Schema.NarrowToJsonMeasure, Surfaces.Schema.NarrowToJson));
        codecs.Add(Row<Surfaces.Wide, Surfaces.TableReport>("tblsurf", "Wide", () => new Surfaces.TableReport(), Copy,
            Surfaces.Schema.WideLoad, Surfaces.Schema.WideMeasure, Surfaces.Schema.WideSave,
            Surfaces.Schema.WideFromJson, Surfaces.Schema.WideToJsonMeasure, Surfaces.Schema.WideToJson));
        codecs.Add(Row<Surfaces.Arrays, Surfaces.TableReport>("tblsurf", "Arrays", () => new Surfaces.TableReport(), Copy,
            Surfaces.Schema.ArraysLoad, Surfaces.Schema.ArraysMeasure, Surfaces.Schema.ArraysSave,
            Surfaces.Schema.ArraysFromJson, Surfaces.Schema.ArraysToJsonMeasure, Surfaces.Schema.ArraysToJson));
        codecs.Add(Row<Surfaces.Big, Surfaces.TableReport>("tblsurf", "Big", () => new Surfaces.TableReport(), Copy,
            Surfaces.Schema.BigLoad, Surfaces.Schema.BigMeasure, Surfaces.Schema.BigSave,
            Surfaces.Schema.BigFromJson, Surfaces.Schema.BigToJsonMeasure, Surfaces.Schema.BigToJson));

        BindSurfCook<Surfaces.Counted>("Counted", Surfaces.Schema.CountedCookMeasure, Surfaces.Schema.CountedCook);
        BindSurfCook<Surfaces.Arrays>("Arrays", Surfaces.Schema.ArraysCookMeasure, Surfaces.Schema.ArraysCook);
        BindSurfCook<Surfaces.Narrow>("Narrow", Surfaces.Schema.NarrowCookMeasure, Surfaces.Schema.NarrowCook);
        BindSurfCook<Surfaces.Wide>("Wide", Surfaces.Schema.WideCookMeasure, Surfaces.Schema.WideCook);
        BindSurfCook<Surfaces.Big>("Big", Surfaces.Schema.BigCookMeasure, Surfaces.Schema.BigCook);

        BindSurfMessage<Surfaces.Narrow>("Narrow", Surfaces.Schema.NarrowMeasureMessages, Surfaces.Schema.NarrowSaveMessages, Surfaces.Schema.NarrowLoadMessages);
        BindSurfMessage<Surfaces.Wide>("Wide", Surfaces.Schema.WideMeasureMessages, Surfaces.Schema.WideSaveMessages, Surfaces.Schema.WideLoadMessages);
        BindSurfMessage<Surfaces.Arrays>("Arrays", Surfaces.Schema.ArraysMeasureMessages, Surfaces.Schema.ArraysSaveMessages, Surfaces.Schema.ArraysLoadMessages);
        BindSurfMessage<Surfaces.Big>("Big", Surfaces.Schema.BigMeasureMessages, Surfaces.Schema.BigSaveMessages, Surfaces.Schema.BigLoadMessages);

        // the fixed roots' one file form (FixedCodecs.cs)
        BindFixed<Surfaces.Narrow, Surfaces.TableReport, Surfaces.TableFixedEntry>("tblsurf", "Narrow", () => new Surfaces.TableReport(), Copy, Surfaces.Schema.NarrowFixedLoad, Surfaces.Schema.NarrowFixedMeasure, Surfaces.Schema.NarrowFixedSave);
        BindFixed<Surfaces.Wide, Surfaces.TableReport, Surfaces.TableFixedEntry>("tblsurf", "Wide", () => new Surfaces.TableReport(), Copy, Surfaces.Schema.WideFixedLoad, Surfaces.Schema.WideFixedMeasure, Surfaces.Schema.WideFixedSave);
        BindFixed<Surfaces.Arrays, Surfaces.TableReport, Surfaces.TableFixedEntry>("tblsurf", "Arrays", () => new Surfaces.TableReport(), Copy, Surfaces.Schema.ArraysFixedLoad, Surfaces.Schema.ArraysFixedMeasure, Surfaces.Schema.ArraysFixedSave);
        // Big is a fixed table past the 65536-byte ceiling: it has no file
        // form, so every file read and write of it refuses (as WideBlob)
        {
            Codec c = Find("tblsurf", "Big");
            c.VariableLoad = c.Load;
            c.Load = (bytes, report) => { report.Refused = true; return null; };
            c.PartialLoad = (bytes, report) => { object value = c.Load(bytes, report); regionLoaded = value != null; return value; };
            c.Measure = v => -1;
            c.Save = (v, buffer) => -1;
        }
    }

    // ---- the write-refusal kinds of 2026-11-22 (WriteRefusal.cs calls these)

    // The root a fresh copy comes from: the row's wire, or the root's fresh
    // value when the wire is `-`.
    static object WriteRefusalFreshRoot(Codec codec, byte[] wire)
    {
        return wire == null ? codec.Fresh() : codec.Load(wire, new Report());
    }

    static object WriteRefusalElementZero(Codec codec, byte[] wire, object holder, FieldInfo f)
    {
        object fresh = WriteRefusalFreshRoot(codec, wire);
        if (fresh == null || fresh.GetType() != holder.GetType())
        {
            throw new InvalidOperationException("write-refusal: a copied element is taken from the root, and " + f.Name + " is not a root field");
        }
        return ((Array)f.GetValue(fresh)).GetValue(0);
    }

    // `elems`: the element count of a fixed [N]T or a keyed [E]T. A C# [N]T
    // is a T[] field, which can be handed any length; a keyed [E]T is a
    // TableKeyed whose Slots are readonly at one per variant, so it cannot
    // hold any other count (unrepresentable).
    static bool WriteRefusalElems(Codec codec, byte[] wire, object holder, FieldInfo f, string text)
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
                resized.SetValue(i < arr.Length ? arr.GetValue(i) : WriteRefusalElementZero(codec, wire, holder, f), i);
            }
            f.SetValue(holder, resized);
            return true;
        }
        return WriteRefusalCapacity(current) == n;
    }

    // `mask`: a flags field's raw bits. A bit pattern the field's storage
    // cannot hold is unrepresentable (the OverflowException WriteRefusalSet
    // catches).
    static bool WriteRefusalMask(object holder, FieldInfo f, string text)
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

    // `dupkey 1`: a copy of the map's first entry appended, so its key is held
    // twice; `dupkey 0` leaves the map as loaded.
    static bool WriteRefusalDupkey(Codec codec, byte[] wire, object holder, FieldInfo f, string text)
    {
        if (text == "0") { return true; }
        if (text != "1") { throw new InvalidOperationException("write-refusal: dupkey takes 0 or 1, not " + text); }
        FieldInfo cf = holder.GetType().GetField(f.Name + "Count", BindingFlags.Public | BindingFlags.Instance);
        if (cf == null) { throw new InvalidOperationException("write-refusal: dupkey on " + f.Name + ", which has no count"); }
        int count = (int)cf.GetValue(holder);
        if (count < 1) { throw new InvalidOperationException("write-refusal: dupkey on the empty map " + f.Name); }
        Array arr = (Array)f.GetValue(holder);
        if (arr.Length <= count)
        {
            Array grown = Array.CreateInstance(arr.GetType().GetElementType(), count + 1);
            Array.Copy(arr, grown, count);
            arr = grown;
        }
        arr.SetValue(WriteRefusalElementZero(codec, wire, holder, f), count);
        f.SetValue(holder, arr);
        cf.SetValue(holder, count + 1);
        return true;
    }

    static object WriteRefusalWalk(object at, string[] segments, int n, string path)
    {
        for (int i = 0; i < n; i++)
        {
            FieldInfo f = at.GetType().GetField(WriteRefusalPascal(segments[i]), BindingFlags.Public | BindingFlags.Instance);
            if (f == null) { throw new InvalidOperationException("write-refusal: point path " + path + " names no field " + segments[i] + " on " + at.GetType().FullName); }
            at = f.GetValue(at);
            if (at == null) { throw new InvalidOperationException("write-refusal: point path " + path + " is null at " + segments[i]); }
        }
        return at;
    }

    // `point`: the pointer at path set to the node the pointer at `from`
    // points at (a cycle when that node encloses path).
    static bool WriteRefusalPoint(object root, string path, string from)
    {
        string[] fromSegments = from.Split('.');
        object target = WriteRefusalWalk(root, fromSegments, fromSegments.Length, from);
        string[] segments = path.Split('.');
        object parent = WriteRefusalWalk(root, segments, segments.Length - 1, path);
        FieldInfo f = parent.GetType().GetField(WriteRefusalPascal(segments[segments.Length - 1]), BindingFlags.Public | BindingFlags.Instance);
        if (f == null || !f.FieldType.IsInstanceOfType(target))
        {
            throw new InvalidOperationException("write-refusal: point " + path + " cannot hold the node at " + from);
        }
        f.SetValue(parent, target);
        return true;
    }

    // write-refusal-cook: CookMeasure, then Cook in little-endian order.
    static string WriteRefusalCook(Codec codec, object value, string name)
    {
        if (codec.CookMeasure == null || codec.Cook == null)
        {
            throw new InvalidOperationException("write-refusal-cook " + name + ": no cook writer registered for " + codec.Unit + "." + codec.Root);
        }
        return WriteRefusalSave(() =>
        {
            long size = codec.CookMeasure(value);
            if (size < 0) { return -1; }
            byte[] bytes = new byte[checked((int)size)];
            return codec.Cook(value, bytes, false) ? size : -1;
        });
    }

    // write-refusal-block: the C# block builder is <Root>Block.Begin, which
    // is handed the value's COUNTS (its out-of-line arrays) and refuses one
    // past its maximum. It is handed nothing else of the value: a root with
    // no out-of-line array (StorePurchase, MatchResult) has empty counts, and
    // the used lengths and values are written into the block by the caller
    // after Begin returns, with no check.
    static string WriteRefusalBlock(object value, string name)
    {
        switch (value)
        {
            case Backenddemo.StorePurchase:
                return WriteRefusalSave(() =>
                {
                    using var storage = new Backenddemo.StorePurchaseBlockStorage();
                    return Backenddemo.StorePurchaseBlock.Begin(out _, storage, new Backenddemo.StorePurchaseCounts(), out _) ? 1 : -1;
                });
            case Backenddemo.MatchResult:
                return WriteRefusalSave(() =>
                {
                    using var storage = new Backenddemo.MatchResultBlockStorage();
                    return Backenddemo.MatchResultBlock.Begin(out _, storage, new Backenddemo.MatchResultCounts(), out _) ? 1 : -1;
                });
        }
        throw new InvalidOperationException("write-refusal-block " + name + ": no block builder registered for " + value.GetType().FullName);
    }

    // ---- announce <case> <unit> <root> <mode> <input>

    // The unit's announcement writer: AnnounceMeasure, then Announce. null is
    // a refusal (a negative measure).
    static byte[] AnnounceWrite(string unit)
    {
        long size;
        byte[] bytes;
        switch (unit)
        {
            case "tblann4096": size = Announce4096.Schema.AnnounceMeasure(); bytes = Announce4096.Schema.Announce().ToArray(); break;
            case "tblann4097": size = Announce4097.Schema.AnnounceMeasure(); bytes = Announce4097.Schema.Announce().ToArray(); break;
            case "tblsurf": size = Surfaces.Schema.AnnounceMeasure(); bytes = Surfaces.Schema.Announce().ToArray(); break;
            default: throw new InvalidOperationException("announce: no announcement writer registered for unit " + unit);
        }
        if (size < 0) { return null; }
        if (size != bytes.Length) { throw new InvalidOperationException("announce: " + unit + " measures " + size + " and announces " + bytes.Length + " bytes"); }
        return bytes;
    }

    // A fresh vocabulary of the unit, its strict flag set or left at its
    // default, reads the announcement: true when the read is accepted.
    static bool AnnounceReads(string unit, bool strict, byte[] announcement)
    {
        switch (unit)
        {
            case "messagedemo":
            {
                var vocabulary = new Messagedemo.TableVocabulary();
                if (strict) { vocabulary.Strict = true; }
                var report = new Messagedemo.TableReport();
                return Messagedemo.Schema.AnnounceRead(vocabulary, announcement, report) == Messagedemo.Schema.TableWire.Verdict.Ok && !report.Refused && !report.Damaged;
            }
            case "tblsurf":
            {
                var vocabulary = new Surfaces.TableVocabulary();
                if (strict) { vocabulary.Strict = true; }
                var report = new Surfaces.TableReport();
                return Surfaces.Schema.AnnounceRead(vocabulary, announcement, report) == Surfaces.Schema.TableWire.Verdict.Ok && !report.Refused && !report.Damaged;
            }
        }
        throw new InvalidOperationException("announce: no announcement reader registered for unit " + unit);
    }

    static int SurfaceAnnounce(string outDir)
    {
        foreach (string[] f in Kind("announce"))
        {
            if (f.Length != 6) { Console.Error.WriteLine("driver: announce line has " + f.Length + " fields"); return 1; }
            string answer = Path.Combine(outDir, f[1]);
            switch (f[4])
            {
                case "write":
                {
                    // a writer that throws rather than refusing answers
                    // that, and the next row still runs
                    byte[] bytes;
                    try { bytes = AnnounceWrite(f[2]); }
                    catch (Exception e) when (e is not InvalidOperationException || e.InnerException != null)
                    {
                        File.WriteAllText(answer, "threw " + (e.InnerException ?? e).GetType().Name + "\n");
                        break;
                    }
                    if (bytes == null) { File.WriteAllText(answer, "refused\n"); }
                    else { File.WriteAllBytes(answer, bytes); }
                    break;
                }
                case "strict":
                case "lenient":
                    File.WriteAllText(answer, AnnounceReads(f[2], f[4] == "strict", File.ReadAllBytes(f[5])) ? "read\n" : "refused\n");
                    break;
                case "send":
                {
                    Codec codec = Find(f[2], f[3]);
                    if (codec == null || codec.MessageSave == null) { Console.Error.WriteLine("driver: announce " + f[1] + " names no message codec " + f[2] + "." + f[3]); return 1; }
                    object value;
                    if (f[5] == "-") { value = codec.Fresh(); }
                    else
                    {
                        Report report = new Report();
                        value = codec.Load(File.ReadAllBytes(f[5]), report);
                        if (StrictRefused(value, report)) { Console.Error.WriteLine("driver: announce " + f[1] + " input does not load"); return 1; }
                    }
                    // SaveMessages with NO announcement read or written on
                    // this connection: the C# writer takes no vocabulary, so
                    // it is handed none
                    byte[] saved = null;
                    long? n = WriteRefusalRun(() =>
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

    // One set of values and ONE report through three reads. `read` returns
    // true when the call returned success and its report holds no refusal.
    static byte[] ReadIntoRun<T>(Codec codec, int count, Func<T> make, Func<T[], byte[], bool> read, byte[] lawful, byte[] damaged)
    {
        T[] values = new T[count];
        for (int i = 0; i < count; i++) { values[i] = make(); }
        var answer = new MemoryStream();
        void Line(string text) { byte[] b = System.Text.Encoding.ASCII.GetBytes(text + "\n"); answer.Write(b, 0, b.Length); }
        void Step(byte[] bytes)
        {
            string line;
            try { line = read(values, bytes) ? "read" : "refused"; }
            catch (Exception e) { line = "threw " + e.GetType().Name; }
            Line(line);
        }
        Step(lawful);
        Step(damaged);
        foreach (T value in values)
        {
            long size = codec.ToJsonMeasure(value);
            if (size < 0) { Line("tojson-refused"); continue; }
            byte[] text = new byte[checked((int)size)];
            if (codec.ToJson(value, text) != size) { Line("tojson-size"); continue; }
            answer.Write(text, 0, text.Length);
        }
        Step(lawful);
        return answer.ToArray();
    }

    static int SurfaceReadInto(string outDir)
    {
        foreach (string[] f in Kind("read-into"))
        {
            if (f.Length != 9) { Console.Error.WriteLine("driver: read-into line has " + f.Length + " fields"); return 1; }
            string answer = Path.Combine(outDir, f[1]);
            string form = f[4];
            if (form == "fixed-nocap")
            {
                // the C# fixed load's capacity is the span of values it is
                // handed: no call omits it, so no omitted capacity can be
                // asked of it (FL-17)
                File.WriteAllText(answer, "unrepresentable\n");
                continue;
            }
            Codec codec = Find(f[2], f[3]);
            if (codec == null) { Console.Error.WriteLine("driver: read-into " + f[1] + " names no codec " + f[2] + "." + f[3]); return 1; }
            int count = int.Parse(f[7], CultureInfo.InvariantCulture);
            byte[] lawful = File.ReadAllBytes(f[5]);
            byte[] damaged = File.ReadAllBytes(f[6]);
            byte[] result;
            switch (f[2] + " " + f[3] + " " + form)
            {
                case "tblsurf Counted variable":
                {
                    var report = new Surfaces.TableReport();
                    result = ReadIntoRun<Surfaces.Counted>(codec, count, () => new Surfaces.Counted(),
                        (values, bytes) => Surfaces.Schema.CountedLoad(values[0], bytes, report) && !report.Refused && !report.Damaged, lawful, damaged);
                    break;
                }
                case "tblsurf Counted json":
                {
                    var report = new Surfaces.TableReport();
                    result = ReadIntoRun<Surfaces.Counted>(codec, count, () => new Surfaces.Counted(),
                        (values, bytes) => Surfaces.Schema.CountedFromJson(values[0], bytes, report) && !report.Refused && !report.Damaged, lawful, damaged);
                    break;
                }
                case "tblsurf Narrow fixed":
                {
                    var report = new Surfaces.TableReport();
                    var plan = new Surfaces.TableFixedEntry[FixedPlanCapacity];
                    result = ReadIntoRun<Surfaces.Narrow>(codec, count, () => new Surfaces.Narrow(),
                        (values, bytes) => Surfaces.Schema.NarrowFixedLoad(values, bytes, plan, report) >= 0 && !report.Refused && !report.Damaged, lawful, damaged);
                    break;
                }
                case "backenddemo StorePurchase message":
                {
                    var vocabulary = new Backenddemo.TableVocabulary();
                    var announced = new Backenddemo.TableReport();
                    if (Backenddemo.Schema.AnnounceRead(vocabulary, File.ReadAllBytes(f[8]), announced) != Backenddemo.Schema.TableWire.Verdict.Ok)
                    {
                        Console.Error.WriteLine("driver: read-into " + f[1] + ": the connection's announcement does not read");
                        return 1;
                    }
                    var report = new Backenddemo.TableReport();
                    result = ReadIntoRun<Backenddemo.StorePurchase>(codec, count, () => new Backenddemo.StorePurchase(),
                        (values, bytes) => Backenddemo.Schema.StorePurchaseLoadMessages(values, bytes, vocabulary, report, out _) == Backenddemo.Schema.TableWire.Verdict.Ok && !report.Refused && !report.Damaged, lawful, damaged);
                    break;
                }
                default:
                    Console.Error.WriteLine("driver: read-into " + f[1] + ": no read registered for " + f[2] + " " + f[3] + " " + form);
                    return 1;
            }
            File.WriteAllBytes(answer, result);
        }
        return 0;
    }

    // ---- json-write-into <case> <unit> <root> <wire>

    static int SurfaceJsonWriteInto(string outDir)
    {
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
            Console.Error.WriteLine("driver: json-write-into: the process locale " + CultureInfo.CurrentCulture.Name + " is not comma-decimal (" + CultureInfo.CurrentCulture.NumberFormat.NumberDecimalSeparator + ")");
            return 1;
        }
        foreach (string[] f in Kind("json-write-into"))
        {
            if (f.Length != 5) { Console.Error.WriteLine("driver: json-write-into line has " + f.Length + " fields"); return 1; }
            Codec codec = Find(f[2], f[3]);
            if (codec == null) { Console.Error.WriteLine("driver: json-write-into " + f[1] + " names no codec " + f[2] + "." + f[3]); return 1; }
            Report report = new Report();
            object value = codec.Load(File.ReadAllBytes(f[4]), report);
            if (StrictRefused(value, report)) { Console.Error.WriteLine("driver: json-write-into " + f[1] + " does not load"); return 1; }
            long size = codec.ToJsonMeasure(value);
            string answer = Path.Combine(outDir, f[1]);
            if (size < 0) { File.WriteAllText(answer, "refused\n"); continue; }
            byte[] buffer = new byte[checked((int)size) + 64];
            Array.Fill(buffer, (byte)0xA5);
            long n = codec.ToJson(value, buffer);
            if (n < 0) { File.WriteAllText(answer, "refused\n"); continue; }
            bool tail = false;
            for (long i = n; i < buffer.Length; i++) { if (buffer[i] != 0xA5) { tail = true; break; } }
            if (tail) { File.WriteAllText(answer, "tail-written\n"); continue; }
            File.WriteAllBytes(answer, buffer.AsSpan(0, checked((int)n)).ToArray());
        }
        return 0;
    }
}
