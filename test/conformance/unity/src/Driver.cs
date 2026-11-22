// The Unity leg's conformance driver (test/conformance/README.md; docs/UNITY.md).
// One .NET host process per surface run (and streaming stdin/stdout for wire-fuzz).
// Each manifest unit is compiled in the Unity dialect as its own schemaunit.dll
// (unit/schemaunit.csproj: netstandard2.1, LangVersion=9.0, AllowUnsafeBlocks=false)
// under <units>/<unit>/, where <units> is UNITY_CONFORMANCE_UNITS (default
// build/conformance-unity). Reflection lives strictly in this test host; the
// generated Unity C# uses none.

using System;
using System.Buffers.Binary;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Reflection;
using System.Runtime.CompilerServices;
using System.Runtime.InteropServices;
using System.Runtime.Loader;
using System.Text;

namespace SchemaUnityConformance
{
    internal sealed class DriverReport
    {
        public int Unknown;
        public int Widened;
        public int Retained;
        public int RetainLost;
        public bool Refused;
        public bool Malformed;
        public int ReasonCode;
        public string ReasonText;
    }

    internal sealed class RetainOutcome
    {
        public DriverReport Report = new DriverReport();
        public long Need = -1;
        public long Written = -1;
        public bool Loaded;
        public int Kept;
        public int Lost;
        public byte[] Saved = Array.Empty<byte>();
        public object Value;
    }

    internal interface ICodec
    {
        string Unit { get; }
        string Root { get; }
        Type ValueType { get; }
        Assembly UnitAssembly { get; }
        bool IsFixed { get; }
        bool HasWire { get; }
        bool HasJson { get; }
        bool HasMessage { get; }
        bool HasCookWrite { get; }
        bool HasRetain { get; }
        byte[] Announcement { get; }

        long LoadMeasure(byte[] wire);
        object Load(byte[] wire, DriverReport report);
        object VariableLoad(byte[] wire, DriverReport report);
        long Measure(object value);
        long Save(object value, byte[] buffer);
        long VariableSave(object value, byte[] buffer);
        long FixedSave(object value, byte[] buffer);

        object FromJson(byte[] text, DriverReport report);
        long ToJsonMeasure(object value);
        long ToJson(object value, byte[] buffer);

        long CookMeasure(object value);
        bool Cook(object value, byte[] buffer, bool bigEndian);

        object MessageLoad(byte[] announcement, byte[] wire, DriverReport report);
        // MessageBatchCount loads one batch with room for every body and answers
        // the count LoadMessages returned (a `message-batch` row, SPEC-TABLES §3.3).
        int MessageBatchCount(byte[] announcement, byte[] wire);
        long MessageMeasure(object value);
        long MessageSave(object value, byte[] buffer);

        RetainOutcome RunRetain(byte[] wire, bool shortBytes, int idCapacity);

        // Fresh is the root's declared-default value (a write-refusal wire of `-`).
        object Fresh();
        // ReadInto is a read-into row (Surfaces1122.cs): ONE report and one set
        // of count values through a read of lawful, damaged and lawful again.
        byte[] ReadInto(string form, byte[] lawful, byte[] damaged, int count, byte[] announcement);
    }

    internal static partial class Driver
    {
        private const int FixedPlanCapacity = 1 << 14;

        private static readonly Dictionary<string, Assembly> unitCache = new Dictionary<string, Assembly>(StringComparer.Ordinal);
        private static readonly Dictionary<string, ICodec> codecCache = new Dictionary<string, ICodec>(StringComparer.Ordinal);
        private static readonly List<string[]> manifestLines = new List<string[]>();

        static Driver()
        {
            AppDomain.CurrentDomain.AssemblyResolve += (sender, args) =>
            {
                string shortName = new AssemblyName(args.Name).Name;
                if (args.RequestingAssembly != null && !string.IsNullOrEmpty(args.RequestingAssembly.Location))
                {
                    string dir = Path.GetDirectoryName(args.RequestingAssembly.Location);
                    string candidate = Path.Combine(dir, shortName + ".dll");
                    if (File.Exists(candidate))
                    {
                        return Assembly.LoadFrom(candidate);
                    }
                }
                return null;
            };
        }

        internal static string UnitsRoot()
        {
            string root = Environment.GetEnvironmentVariable("UNITY_CONFORMANCE_UNITS");
            return string.IsNullOrEmpty(root) ? "build/conformance-unity" : root;
        }

        internal static Assembly LoadUnit(string rootDir, string key)
        {
            string cacheKey = rootDir + "\0" + key;
            if (unitCache.TryGetValue(cacheKey, out Assembly cached))
            {
                return cached;
            }
            string unitDir = Path.GetFullPath(Path.Combine(rootDir, key));
            string path = Path.Combine(unitDir, "schemaunit.dll");
            Assembly asm = null;
            if (File.Exists(path))
            {
                var alc = new AssemblyLoadContext("unity_" + key);
                alc.Resolving += (ctx, name) =>
                {
                    string dep = Path.Combine(unitDir, name.Name + ".dll");
                    return File.Exists(dep) ? ctx.LoadFromAssemblyPath(dep) : null;
                };
                asm = alc.LoadFromAssemblyPath(path);
            }
            unitCache[cacheKey] = asm;
            return asm;
        }

        internal static Type FindSchemaType(Assembly unit)
        {
            if (unit == null) { return null; }
            foreach (Type t in unit.GetTypes())
            {
                if (t.Name == "Schema" && t.IsAbstract && t.IsSealed)
                {
                    return t;
                }
            }
            return null;
        }

        internal static ICodec FindCodec(string unitKey, string rootName, string rootDir = null)
        {
            rootDir ??= UnitsRoot();
            string cacheKey = rootDir + "\0" + unitKey + "\0" + rootName;
            if (codecCache.TryGetValue(cacheKey, out ICodec cached))
            {
                return cached;
            }
            Assembly asm = LoadUnit(rootDir, unitKey);
            Type schemaType = FindSchemaType(asm);
            if (schemaType == null)
            {
                codecCache[cacheKey] = null;
                return null;
            }
            string ns = schemaType.Namespace;
            Type valueType = asm.GetType(ns + "." + rootName);
            Type reportType = schemaType.GetNestedType("TableReport", BindingFlags.Public);
            Type retainType = schemaType.GetNestedType("TableRetain", BindingFlags.Public);
            Type retainIdType = retainType?.GetNestedType("Id", BindingFlags.Public);
            Type vocabType = schemaType.GetNestedType("TableVocabulary", BindingFlags.Public);
            Type orderType = schemaType.GetNestedType("TableByteOrder", BindingFlags.Public);
            PropertyInfo infoProp = schemaType.GetProperty(rootName + "TableType", BindingFlags.Public | BindingFlags.Static);
            if (valueType == null || reportType == null || retainType == null || retainIdType == null || vocabType == null || orderType == null || infoProp == null)
            {
                codecCache[cacheKey] = null;
                return null;
            }
            object info = infoProp.GetValue(null);
            bool isFixed = (bool)info.GetType().GetField("Fixed", BindingFlags.Public | BindingFlags.Instance).GetValue(info);

            Type generic = typeof(TypedCodec<,,,,,>).MakeGenericType(valueType, reportType, retainType, retainIdType, vocabType, orderType);
            ICodec built = (ICodec)Activator.CreateInstance(generic, unitKey, rootName, asm, schemaType, isFixed);
            codecCache[cacheKey] = built;
            return built;
        }

        private delegate void InitRefDel<T>(ref T value);
        private delegate T NewDel<T>();
        private delegate bool LoadDel<T, TRep>(ref T value, ReadOnlySpan<byte> bytes, ref TRep report);
        private delegate long MeasureRefDel<T>(ref T value);
        private delegate long SaveRefDel<T>(ref T value, Span<byte> buffer);
        private delegate long LoadMeasureDel(ReadOnlySpan<byte> bytes);
        private delegate long FixedLoadDel<T, TRep>(Span<T> values, ReadOnlySpan<byte> data, ref TRep report, int planCapacity);
        private delegate long FixedMeasureDel(long count);
        private delegate long FixedSaveDel<T>(ReadOnlySpan<T> values, Span<byte> buffer);
        private delegate bool LoadRetainDel<T, TRet, TRep>(ref T value, ReadOnlySpan<byte> bytes, TRet retain, ref TRep report);
        private delegate long MeasureRetainRefDel<T, TRet>(ref T value, TRet retain);
        private delegate long SaveRetainRefDel<T, TRet, TRep>(ref T value, TRet retain, Span<byte> bytes, ref TRep report);
        private delegate long MeasureRetainDel<T, TRet>(T value, TRet retain);
        private delegate long SaveRetainDel<T, TRet, TRep>(T value, TRet retain, Span<byte> bytes, ref TRep report);
        private delegate long CookMeasureRefDel<T>(ref T value);
        private delegate bool CookRefDel<T, TOrder>(ref T value, Span<byte> bytes, TOrder order);
        private delegate long CookMeasureDel<T>(T value);
        private delegate bool CookDel<T, TOrder>(T value, Span<byte> bytes, TOrder order);
        private delegate ReadOnlySpan<byte> AnnounceDel();
        private delegate bool AnnounceReadDel<TVocab, TRep>(TVocab vocabulary, ReadOnlySpan<byte> bytes, ref TRep report);
        private delegate long MeasureMessagesDel<T>(ReadOnlySpan<T> values);
        private delegate long SaveMessagesDel<T>(ReadOnlySpan<T> values, Span<byte> bytes);
        private delegate bool LoadMessagesDel<T, TVocab, TRep>(Span<T> values, ReadOnlySpan<byte> bytes, TVocab vocabulary, ref TRep report, out int count);

        private sealed class TypedCodec<T, TRep, TRet, TRetId, TVocab, TOrder> : ICodec
            where TRep : struct
            where TRet : class, new()
            where TRetId : struct
            where TVocab : class, new()
            where TOrder : struct
        {
            public string Unit { get; }
            public string Root { get; }
            public Type ValueType => typeof(T);
            public Assembly UnitAssembly { get; }
            public bool IsFixed { get; }
            public bool HasWire => varLoad != null || fixedLoad != null || IsFixed;
            public bool HasJson => fromJson != null && toJson != null && toJsonMeasure != null;
            public bool HasMessage => loadMessages != null && saveMessages != null && measureMessages != null;
            public bool HasCookWrite => (cookRef != null || cookVal != null) && (cookMeasureRef != null || cookMeasureVal != null);
            public bool HasRetain => loadRetain != null && (measureRetainRef != null || measureRetainVal != null) && (saveRetainRef != null || saveRetainVal != null);
            public byte[] Announcement { get; }

            private readonly InitRefDel<T> initRef;
            private readonly NewDel<T> newValue;
            private readonly LoadMeasureDel loadMeasure;
            private readonly LoadDel<T, TRep> varLoad;
            private readonly MeasureRefDel<T> varMeasure;
            private readonly SaveRefDel<T> varSave;
            private readonly FixedLoadDel<T, TRep> fixedLoad;
            private readonly FixedMeasureDel fixedMeasure;
            private readonly FixedSaveDel<T> fixedSave;
            private readonly LoadRetainDel<T, TRet, TRep> loadRetain;
            private readonly MeasureRetainRefDel<T, TRet> measureRetainRef;
            private readonly SaveRetainRefDel<T, TRet, TRep> saveRetainRef;
            private readonly MeasureRetainDel<T, TRet> measureRetainVal;
            private readonly SaveRetainDel<T, TRet, TRep> saveRetainVal;
            private readonly LoadDel<T, TRep> fromJson;
            private readonly MeasureRefDel<T> toJsonMeasure;
            private readonly SaveRefDel<T> toJson;
            private readonly CookMeasureRefDel<T> cookMeasureRef;
            private readonly CookRefDel<T, TOrder> cookRef;
            private readonly CookMeasureDel<T> cookMeasureVal;
            private readonly CookDel<T, TOrder> cookVal;
            private readonly AnnounceReadDel<TVocab, TRep> announceRead;
            private readonly TVocab ownVocab;
            private readonly MeasureMessagesDel<T> measureMessages;
            private readonly SaveMessagesDel<T> saveMessages;
            private readonly LoadMessagesDel<T, TVocab, TRep> loadMessages;

            private readonly FieldInfo fUnknown = typeof(TRep).GetField("Unknown");
            private readonly FieldInfo fWidened = typeof(TRep).GetField("Widened");
            private readonly FieldInfo fRetained = typeof(TRep).GetField("Retained");
            private readonly FieldInfo fRetainLost = typeof(TRep).GetField("RetainLost");
            private readonly FieldInfo fRefused = typeof(TRep).GetField("Refused");
            private readonly FieldInfo fDamaged = typeof(TRep).GetField("Damaged");
            private readonly FieldInfo fReason = typeof(TRep).GetField("Reason");
            private readonly FieldInfo fReasonText = typeof(TRep).GetField("ReasonText");
            private readonly MethodInfo mReasonName = typeof(TRep).GetMethod("ReasonName", BindingFlags.Public | BindingFlags.Static);

            private readonly FieldInfo fRetBytes = typeof(TRet).GetField("Bytes");
            private readonly FieldInfo fRetCap = typeof(TRet).GetField("Capacity");
            private readonly FieldInfo fRetUsed = typeof(TRet).GetField("Used");
            private readonly FieldInfo fRetIds = typeof(TRet).GetField("Ids");
            private readonly FieldInfo fRetIdCap = typeof(TRet).GetField("IdCapacity");

            private readonly TOrder orderLittle = (TOrder)Enum.ToObject(typeof(TOrder), 1);
            private readonly TOrder orderBig = (TOrder)Enum.ToObject(typeof(TOrder), 2);

            private T MakeFresh()
            {
                if (initRef != null)
                {
                    T v = default;
                    initRef(ref v);
                    return v;
                }
                return newValue != null ? newValue() : default;
            }

            public TypedCodec(string unit, string root, Assembly asm, Type schema, bool isFixed)
            {
                Unit = unit;
                Root = root;
                UnitAssembly = asm;
                IsFixed = isFixed;

                const BindingFlags Flags = BindingFlags.Public | BindingFlags.Static;
                Type refT = typeof(T).MakeByRefType();
                Type refRep = typeof(TRep).MakeByRefType();
                initRef = Bind<InitRefDel<T>>(schema.GetMethod("Init" + root, Flags, null, new[] { refT }, null));
                if (initRef == null)
                {
                    newValue = Bind<NewDel<T>>(schema.GetMethod("New" + root, Flags, null, Type.EmptyTypes, null));
                }
                loadMeasure = Bind<LoadMeasureDel>(schema.GetMethod(root + "LoadMeasure", Flags, null, new[] { typeof(ReadOnlySpan<byte>) }, null));
                varLoad = Bind<LoadDel<T, TRep>>(schema.GetMethod(root + "Load", Flags, null, new[] { refT, typeof(ReadOnlySpan<byte>), refRep }, null));
                varMeasure = Bind<MeasureRefDel<T>>(schema.GetMethod(root + "Measure", Flags, null, new[] { refT }, null));
                varSave = Bind<SaveRefDel<T>>(schema.GetMethod(root + "Save", Flags, null, new[] { refT, typeof(Span<byte>) }, null));

                fixedLoad = Bind<FixedLoadDel<T, TRep>>(schema.GetMethod(root + "FixedLoad", Flags, null, new[] { typeof(Span<T>), typeof(ReadOnlySpan<byte>), refRep, typeof(int) }, null));
                fixedMeasure = Bind<FixedMeasureDel>(schema.GetMethod(root + "FixedMeasure", Flags, null, new[] { typeof(long) }, null));
                fixedSave = Bind<FixedSaveDel<T>>(schema.GetMethod(root + "FixedSave", Flags, null, new[] { typeof(ReadOnlySpan<T>), typeof(Span<byte>) }, null));

                loadRetain = Bind<LoadRetainDel<T, TRet, TRep>>(schema.GetMethod(root + "LoadRetain", Flags, null, new[] { refT, typeof(ReadOnlySpan<byte>), typeof(TRet), refRep }, null));
                measureRetainRef = Bind<MeasureRetainRefDel<T, TRet>>(schema.GetMethod(root + "MeasureRetain", Flags, null, new[] { refT, typeof(TRet) }, null));
                saveRetainRef = Bind<SaveRetainRefDel<T, TRet, TRep>>(schema.GetMethod(root + "SaveRetain", Flags, null, new[] { refT, typeof(TRet), typeof(Span<byte>), refRep }, null));
                if (measureRetainRef == null)
                {
                    measureRetainVal = Bind<MeasureRetainDel<T, TRet>>(schema.GetMethod(root + "MeasureRetain", Flags, null, new[] { typeof(T), typeof(TRet) }, null));
                    saveRetainVal = Bind<SaveRetainDel<T, TRet, TRep>>(schema.GetMethod(root + "SaveRetain", Flags, null, new[] { typeof(T), typeof(TRet), typeof(Span<byte>), refRep }, null));
                }

                fromJson = Bind<LoadDel<T, TRep>>(schema.GetMethod(root + "FromJson", Flags, null, new[] { refT, typeof(ReadOnlySpan<byte>), refRep }, null));
                toJsonMeasure = Bind<MeasureRefDel<T>>(schema.GetMethod(root + "ToJsonMeasure", Flags, null, new[] { refT }, null));
                toJson = Bind<SaveRefDel<T>>(schema.GetMethod(root + "ToJson", Flags, null, new[] { refT, typeof(Span<byte>) }, null));

                cookMeasureRef = Bind<CookMeasureRefDel<T>>(schema.GetMethod(root + "CookMeasure", Flags, null, new[] { refT }, null));
                cookRef = Bind<CookRefDel<T, TOrder>>(schema.GetMethod(root + "Cook", Flags, null, new[] { refT, typeof(Span<byte>), typeof(TOrder) }, null));
                if (cookMeasureRef == null)
                {
                    cookMeasureVal = Bind<CookMeasureDel<T>>(schema.GetMethod(root + "CookMeasure", Flags, null, new[] { typeof(T) }, null));
                    cookVal = Bind<CookDel<T, TOrder>>(schema.GetMethod(root + "Cook", Flags, null, new[] { typeof(T), typeof(Span<byte>), typeof(TOrder) }, null));
                }

                var announce = Bind<AnnounceDel>(schema.GetMethod("Announce", Flags, null, Type.EmptyTypes, null));
                announceRead = Bind<AnnounceReadDel<TVocab, TRep>>(schema.GetMethod("AnnounceRead", Flags, null, new[] { typeof(TVocab), typeof(ReadOnlySpan<byte>), refRep }, null));
                if (announce != null && announceRead != null)
                {
                    Announcement = announce().ToArray();
                    ownVocab = new TVocab();
                    TRep ignored = default;
                    announceRead(ownVocab, Announcement, ref ignored);
                }

                measureMessages = Bind<MeasureMessagesDel<T>>(schema.GetMethod(root + "MeasureMessages", Flags, null, new[] { typeof(ReadOnlySpan<T>) }, null));
                saveMessages = Bind<SaveMessagesDel<T>>(schema.GetMethod(root + "SaveMessages", Flags, null, new[] { typeof(ReadOnlySpan<T>), typeof(Span<byte>) }, null));
                loadMessages = Bind<LoadMessagesDel<T, TVocab, TRep>>(schema.GetMethod(root + "LoadMessages", Flags, null, new[] { typeof(Span<T>), typeof(ReadOnlySpan<byte>), typeof(TVocab), refRep, typeof(int).MakeByRefType() }, null));
            }

            private static TDel Bind<TDel>(MethodInfo m) where TDel : Delegate
            {
                return m == null ? null : (TDel)m.CreateDelegate(typeof(TDel));
            }

            private void CopyReport(TRep inner, DriverReport dst)
            {
                if (dst == null) { return; }
                object boxed = inner;
                dst.Unknown = (int)fUnknown.GetValue(boxed);
                dst.Widened = (int)fWidened.GetValue(boxed);
                dst.Retained = fRetained == null ? 0 : (int)fRetained.GetValue(boxed);
                dst.RetainLost = fRetainLost == null ? 0 : (int)fRetainLost.GetValue(boxed);
                dst.Refused = (bool)fRefused.GetValue(boxed);
                dst.Malformed = fDamaged != null && (bool)fDamaged.GetValue(boxed);
                dst.ReasonCode = (int)fReason.GetValue(boxed);
                string rt = fReasonText?.GetValue(boxed) as string;
                if (string.IsNullOrEmpty(rt) && mReasonName != null)
                {
                    rt = mReasonName.Invoke(null, new object[] { dst.ReasonCode }) as string;
                }
                dst.ReasonText = rt;
                // Note: FixedLoad refuses damaged wire bytes (truncated, layout_malformed, reserved_not_zero, integrity_mismatch) via Refused=true without setting Damaged; Refused || Malformed suffices for all strict checks.
            }

            public long LoadMeasure(byte[] wire)
            {
                return IsFixed || loadMeasure == null ? -1 : loadMeasure(wire);
            }

            public object VariableLoad(byte[] wire, DriverReport report)
            {
                if (varLoad == null)
                {
                    if (report != null) { report.Refused = true; }
                    return null;
                }
                T val = MakeFresh();
                TRep inner = default;
                bool ok = varLoad(ref val, wire, ref inner);
                CopyReport(inner, report);
                return ok ? (object)val : null;
            }

            public object Load(byte[] wire, DriverReport report)
            {
                if (IsFixed)
                {
                    if (fixedLoad == null)
                    {
                        if (report != null) { report.Refused = true; report.ReasonText = "no_file_form"; }
                        return null;
                    }
                    T val = MakeFresh();
                    TRep inner = default;
                    long n = fixedLoad(MemoryMarshal.CreateSpan(ref val, 1), wire, ref inner, FixedPlanCapacity);
                    CopyReport(inner, report);
                    if (n != 1)
                    {
                        if (report != null && !report.Refused && !report.Malformed)
                        {
                            report.Refused = true;
                        }
                        return null;
                    }
                    return val;
                }
                return VariableLoad(wire, report);
            }

            public long Measure(object value)
            {
                if (IsFixed)
                {
                    return fixedMeasure == null ? -1 : fixedMeasure(1);
                }
                if (varMeasure == null) { return -1; }
                T v = (T)value;
                return varMeasure(ref v);
            }

            public long Save(object value, byte[] buffer)
            {
                if (IsFixed)
                {
                    return FixedSave(value, buffer);
                }
                return VariableSave(value, buffer);
            }

            public long VariableSave(object value, byte[] buffer)
            {
                if (varSave == null) { return -1; }
                T v = (T)value;
                return varSave(ref v, buffer);
            }

            public long FixedSave(object value, byte[] buffer)
            {
                if (fixedSave == null) { return -1; }
                T v = (T)value;
                return fixedSave(MemoryMarshal.CreateReadOnlySpan(ref v, 1), buffer);
            }

            public object FromJson(byte[] text, DriverReport report)
            {
                if (fromJson == null)
                {
                    if (report != null) { report.Refused = true; }
                    return null;
                }
                T val = MakeFresh();
                TRep inner = default;
                bool ok = fromJson(ref val, text, ref inner);
                CopyReport(inner, report);
                return ok ? (object)val : null;
            }

            public long ToJsonMeasure(object value)
            {
                if (toJsonMeasure == null) { return -1; }
                T v = (T)value;
                return toJsonMeasure(ref v);
            }

            public long ToJson(object value, byte[] buffer)
            {
                if (toJson == null) { return -1; }
                T v = (T)value;
                return toJson(ref v, buffer);
            }

            public long CookMeasure(object value)
            {
                T v = (T)value;
                if (cookMeasureRef != null) { return cookMeasureRef(ref v); }
                return cookMeasureVal == null ? -1 : cookMeasureVal(v);
            }

            public bool Cook(object value, byte[] buffer, bool bigEndian)
            {
                T v = (T)value;
                TOrder ord = bigEndian ? orderBig : orderLittle;
                if (cookRef != null) { return cookRef(ref v, buffer, ord); }
                return cookVal != null && cookVal(v, buffer, ord);
            }

            public object MessageLoad(byte[] announcement, byte[] wire, DriverReport report)
            {
                if (loadMessages == null || announceRead == null)
                {
                    if (report != null) { report.Refused = true; }
                    return null;
                }
                TVocab vocab;
                TRep inner = default;
                if (ReferenceEquals(announcement, Announcement))
                {
                    vocab = ownVocab;
                }
                else
                {
                    vocab = new TVocab();
                    announceRead(vocab, announcement, ref inner);
                    CopyReport(inner, report);
                    if (report != null && (report.Refused || report.Malformed))
                    {
                        return null;
                    }
                }
                return MessageLoadCore(vocab, wire, report);
            }

            [MethodImpl(MethodImplOptions.NoInlining)]
            private object MessageLoadCore(TVocab vocab, byte[] wire, DriverReport report)
            {
                T[] values = new T[256];
                int initCount = Math.Min(256, wire.Length >= 2 ? wire[1] + 1 : 1);
                for (int i = 0; i < initCount; i++)
                {
                    values[i] = MakeFresh();
                }
                TRep inner = default;
                bool ok = loadMessages(values, wire, vocab, ref inner, out int count);
                CopyReport(inner, report);
                if (!ok || count < 1)
                {
                    if (report != null && !report.Refused && !report.Malformed)
                    {
                        report.Refused = true;
                    }
                    return null;
                }
                if (count == 1)
                {
                    return values[0];
                }
                T[] batch = new T[count];
                Array.Copy(values, 0, batch, 0, count);
                return batch;
            }

            public int MessageBatchCount(byte[] announcement, byte[] wire)
            {
                if (loadMessages == null || announceRead == null) { return -1; }
                var vocab = new TVocab();
                TRep announced = default;
                announceRead(vocab, announcement, ref announced);
                T[] values = new T[256];
                for (int i = 0; i < values.Length; i++) { values[i] = MakeFresh(); }
                TRep inner = default;
                loadMessages(values, wire, vocab, ref inner, out int count);
                return count;
            }

            public long MessageMeasure(object value)
            {
                if (measureMessages == null) { return -1; }
                // never name T[] on the one-value path: an array of a value type
                // past .NET's element-size limit (tblsurf Big, 70012 bytes) cannot
                // even be type-tested, and the runtime takes a span
                if (value is Array) { return MeasureBatch(value); }
                T v = (T)value;
                return measureMessages(MemoryMarshal.CreateReadOnlySpan(ref v, 1));
            }

            public long MessageSave(object value, byte[] buffer)
            {
                if (saveMessages == null) { return -1; }
                if (value is Array) { return SaveBatch(value, buffer); }
                T v = (T)value;
                return saveMessages(MemoryMarshal.CreateReadOnlySpan(ref v, 1), buffer);
            }

            [MethodImpl(MethodImplOptions.NoInlining)]
            private long MeasureBatch(object batch) { return measureMessages((T[])batch); }

            [MethodImpl(MethodImplOptions.NoInlining)]
            private long SaveBatch(object batch, byte[] buffer) { return saveMessages((T[])batch, buffer); }

            private byte[] scratchRetainBytes;
            private TRetId[] scratchRetainIds;

            private TRet MakeRetain(long byteCap, int idCap, bool reuseScratch = false)
            {
                TRet store = new TRet();
                if (reuseScratch && byteCap <= (1 << 20) && idCap <= (1 << 17))
                {
                    scratchRetainBytes ??= new byte[1 << 20];
                    scratchRetainIds ??= new TRetId[1 << 17];
                    fRetBytes.SetValue(store, scratchRetainBytes);
                    fRetCap.SetValue(store, byteCap);
                    fRetIds.SetValue(store, scratchRetainIds);
                    fRetIdCap.SetValue(store, idCap);
                    return store;
                }
                fRetBytes.SetValue(store, new byte[1 << 20]);
                fRetCap.SetValue(store, byteCap);
                fRetIds.SetValue(store, new TRetId[Math.Max(1, idCap)]);
                fRetIdCap.SetValue(store, idCap);
                return store;
            }

            public object Fresh() { return MakeFresh(); }

            public byte[] ReadInto(string form, byte[] lawful, byte[] damaged, int count, byte[] announcement)
            {
                switch (form)
                {
                    case "variable": if (varLoad == null) { throw new InvalidOperationException("read-into: " + Unit + "." + Root + " has no variable Load"); } break;
                    case "json": if (fromJson == null) { throw new InvalidOperationException("read-into: " + Unit + "." + Root + " has no FromJson"); } break;
                    case "fixed": if (fixedLoad == null) { throw new InvalidOperationException("read-into: " + Unit + "." + Root + " has no FixedLoad"); } break;
                    case "message": if (loadMessages == null || announceRead == null) { throw new InvalidOperationException("read-into: " + Unit + "." + Root + " has no LoadMessages"); } break;
                    default: throw new InvalidOperationException("read-into: unknown form " + form);
                }
                if (toJsonMeasure == null || toJson == null) { throw new InvalidOperationException("read-into: " + Unit + "." + Root + " has no ToJson"); }
                TVocab vocab = null;
                if (form == "message")
                {
                    vocab = new TVocab();
                    TRep announced = default;
                    if (!announceRead(vocab, announcement, ref announced)) { throw new InvalidOperationException("read-into: the connection's announcement does not read"); }
                }
                T[] values = new T[count];
                for (int i = 0; i < count; i++) { values[i] = MakeFresh(); }
                TRep report = default;
                var answer = new MemoryStream();
                void Line(string text) { byte[] b = Encoding.ASCII.GetBytes(text + "\n"); answer.Write(b, 0, b.Length); }
                void Step(byte[] bytes)
                {
                    string line;
                    try
                    {
                        bool ok;
                        switch (form)
                        {
                            case "variable": ok = varLoad(ref values[0], bytes, ref report); break;
                            case "json": ok = fromJson(ref values[0], bytes, ref report); break;
                            case "fixed": ok = fixedLoad(values, bytes, ref report, FixedPlanCapacity) >= 0; break;
                            default: ok = loadMessages(values, bytes, vocab, ref report, out _); break;
                        }
                        object boxed = report;
                        bool refused = (bool)fRefused.GetValue(boxed) || (fDamaged != null && (bool)fDamaged.GetValue(boxed));
                        line = ok && !refused ? "read" : "refused";
                    }
                    catch (Exception e) { line = "threw " + e.GetType().Name; }
                    Line(line);
                }
                Step(lawful);
                Step(damaged);
                for (int i = 0; i < count; i++)
                {
                    long size = toJsonMeasure(ref values[i]);
                    if (size < 0) { Line("tojson-refused"); continue; }
                    byte[] text = new byte[checked((int)size)];
                    if (toJson(ref values[i], text) != size) { Line("tojson-size"); continue; }
                    answer.Write(text, 0, text.Length);
                }
                Step(lawful);
                return answer.ToArray();
            }

            public RetainOutcome RunRetain(byte[] wire, bool shortBytes, int idCapacity)
            {
                var res = new RetainOutcome();
                res.Need = LoadMeasure(wire);
                if (loadRetain == null)
                {
                    res.Report.Refused = true;
                    return res;
                }
                long cap = 1 << 20;
                if (shortBytes)
                {
                    TRet probeStore = MakeRetain(1 << 20, idCapacity);
                    T probeVal = MakeFresh();
                    TRep probeRep = default;
                    loadRetain(ref probeVal, wire, probeStore, ref probeRep);
                    long used = (long)fRetUsed.GetValue(probeStore);
                    cap = Math.Max(0, used - 1);
                }
                TRet store = MakeRetain(cap, idCapacity, !shortBytes);
                T val = MakeFresh();
                TRep loadRep = default;
                bool ok = loadRetain(ref val, wire, store, ref loadRep);
                CopyReport(loadRep, res.Report);
                res.Loaded = ok && !res.Report.Refused && !res.Report.Malformed;
                res.Kept = res.Report.Retained;
                res.Lost = res.Report.RetainLost;
                if (res.Loaded)
                {
                    res.Value = val;
                    long size = measureRetainRef != null ? measureRetainRef(ref val, store) : measureRetainVal(val, store);
                    if (size >= 0)
                    {
                        byte[] outBuf = new byte[checked((int)size)];
                        TRep saveRep = default;
                        long written = saveRetainRef != null ? saveRetainRef(ref val, store, outBuf, ref saveRep) : saveRetainVal(val, store, outBuf, ref saveRep);
                        var saveDrv = new DriverReport();
                        CopyReport(saveRep, saveDrv);
                        res.Lost = saveDrv.RetainLost > res.Lost ? saveDrv.RetainLost : (res.Report.RetainLost + saveDrv.RetainLost);
                        // Note: SaveRetain does `report.RetainLost += box.RetainLost;` when passed a fresh struct report, so saveDrv.RetainLost is box.RetainLost alone!
                        res.Lost = res.Report.RetainLost + saveDrv.RetainLost;
                        res.Saved = outBuf;
                        res.Written = written;
                    }
                }
                return res;
            }
        }

        // ---- --verdicts <units> <list> <out> ----

        private static int RunVerdicts(string rootDir, string listPath, string outPath)
        {
            var b = new StringBuilder();
            foreach (string raw in File.ReadAllLines(listPath))
            {
                string[] f = raw.Split(' ');
                if (f.Length != 3) { continue; }
                ICodec codec = FindCodec(f[0], f[1], rootDir);
                if (codec == null || (!codec.IsFixed && !codec.HasWire) || (codec.IsFixed && !codec.HasWire))
                {
                    b.Append("absent\n");
                    continue;
                }
                // Verdicts probes the form-1 codec directly (tablesunity_test.go).
                var rep = new DriverReport();
                byte[] wire = File.ReadAllBytes(f[2]);
                object val = null;
                string loadThrew = null;
                long? loadOk = RunTrappingAsserts(() =>
                {
                    val = codec.VariableLoad(wire, rep);
                    return 0L;
                }, out loadThrew);
                if (loadThrew != null || loadOk == null)
                {
                    b.Append("fault ").Append((loadThrew ?? "load_assert").Replace(' ', '_')).Append('\n');
                    continue;
                }
                if (val == null && !rep.Refused && !rep.Malformed)
                {
                    b.Append("absent\n");
                    continue;
                }
                if (val == null || rep.Refused || rep.Malformed)
                {
                    b.Append("refused\n");
                    continue;
                }
                string threwM = null;
                long? mOpt = codec.IsFixed ? -1L : RunTrappingAsserts(() => codec.Measure(val), out threwM);
                if (threwM != null || mOpt == null)
                {
                    b.Append("fault write_assert\n");
                    continue;
                }
                long m = mOpt.Value;
                if (codec.IsFixed)
                {
                    byte[] tmp = new byte[1 << 16];
                    long? wOpt = RunTrappingAsserts(() => codec.VariableSave(val, tmp), out string threwW);
                    if (threwW != null || wOpt == null || wOpt.Value < 0) { b.Append("fault save_failed\n"); continue; }
                    b.Append("read ").Append(rep.Unknown).Append(' ').Append(rep.Widened).Append(' ')
                        .Append(Convert.ToHexString(tmp, 0, (int)wOpt.Value).ToLowerInvariant()).Append('\n');
                    continue;
                }
                if (m < 0)
                {
                    b.Append("fault measure_negative\n");
                    continue;
                }
                byte[] saved = new byte[m];
                long? writtenOpt = RunTrappingAsserts(() => codec.VariableSave(val, saved), out string threwS);
                if (threwS != null || writtenOpt == null || writtenOpt.Value != m)
                {
                    b.Append("fault save_size_mismatch\n");
                    continue;
                }
                b.Append("read ").Append(rep.Unknown).Append(' ').Append(rep.Widened).Append(' ')
                    .Append(Convert.ToHexString(saved).ToLowerInvariant()).Append('\n');
            }
            File.WriteAllText(outPath, b.ToString());
            return 0;
        }

        // ---- wire-fuzz streaming binary protocol ----

        private static int RunWireFuzz()
        {
            using var input = new BinaryReader(Console.OpenStandardInput());
            using var output = new BinaryWriter(Console.OpenStandardOutput());
            uint count = input.ReadUInt32();
            var roster = new ICodec[count];
            var forms = new byte[count];
            var retaining = new bool[count];
            for (int i = 0; i < count; i++)
            {
                string unit = Encoding.UTF8.GetString(input.ReadBytes(input.ReadUInt16()));
                string root = Encoding.UTF8.GetString(input.ReadBytes(input.ReadUInt16()));
                byte form = input.ReadByte();
                byte retain = input.ReadByte();
                ICodec codec = FindCodec(unit, root);
                if (retain != 0 && (codec == null || !codec.HasRetain || codec.IsFixed || form == 2 || form == 5))
                {
                    codec = null;
                }
                if ((form == 2 || form == 5) && (codec == null || !codec.HasMessage))
                {
                    codec = null;
                }
                if (form != 2 && form != 5 && retain == 0 && (codec == null || !codec.HasWire))
                {
                    codec = null;
                }
                if (form != 1 && form != 2 && form != 5)
                {
                    codec = null;
                }
                roster[i] = codec;
                forms[i] = form;
                retaining[i] = retain != 0;
                output.Write((byte)(codec != null ? 1 : 0));
            }
            output.Flush();

            for (;;)
            {
                uint index;
                try
                {
                    index = input.ReadUInt32();
                }
                catch (EndOfStreamException)
                {
                    return 0;
                }
                byte[] bytes = input.ReadBytes(checked((int)input.ReadUInt32()));
                ICodec codec = roster[index];
                if (retaining[index])
                {
                    RetainOutcome ans = codec.RunRetain(bytes, false, 1 << 17);
                    DriverReport r = ans.Report;
                    output.Write((byte)(ans.Loaded ? 1 : 0));
                    output.Write(r.Unknown);
                    output.Write(0);
                    output.Write(r.Widened);
                    output.Write(0);
                    output.Write(0);
                    output.Write((byte)(r.Malformed ? 1 : 0));
                    output.Write((byte)(r.Refused ? 1 : 0));
                    output.Write(ans.Need);
                    output.Write(ans.Kept);
                    output.Write(ans.Lost);
                    output.Write(ans.Written);
                    if (ans.Written > 0)
                    {
                        output.Write(ans.Saved, 0, checked((int)ans.Written));
                    }
                    output.Flush();
                    continue;
                }

                var rep = new DriverReport();
                bool message = forms[index] == 2 || forms[index] == 5;
                long need = message ? -1 : codec.LoadMeasure(bytes);
                object value = null;
                if (forms[index] == 5)
                {
                    if (bytes.Length < 4)
                    {
                        rep.Refused = true;
                    }
                    else
                    {
                        uint alen = BinaryPrimitives.ReadUInt32LittleEndian(bytes.AsSpan(0, 4));
                        if (4L + alen > bytes.Length)
                        {
                            rep.Refused = true;
                        }
                        else
                        {
                            byte[] ann = alen == 0 ? codec.Announcement : bytes.AsSpan(4, (int)alen).ToArray();
                            byte[] batch = bytes.AsSpan(4 + (int)alen).ToArray();
                            value = codec.MessageLoad(ann, batch, rep);
                        }
                    }
                }
                else
                {
                    value = message
                        ? codec.MessageLoad(codec.Announcement, bytes, rep)
                        : codec.Load(bytes, rep);
                }
                bool loaded = value != null && !rep.Refused && !rep.Malformed;
                output.Write((byte)(loaded ? 1 : 0));
                output.Write(rep.Unknown);
                output.Write(0);
                output.Write(rep.Widened);
                output.Write(0);
                output.Write(0);
                output.Write((byte)(rep.Malformed ? 1 : 0));
                output.Write((byte)(rep.Refused ? 1 : 0));
                output.Write(need);
                output.Write(0);
                output.Write(0);
                long size = loaded ? (message ? codec.MessageMeasure(value) : codec.Measure(value)) : -1;
                byte[] saved = size <= 0 ? Array.Empty<byte>() : new byte[checked((int)size)];
                long written = !loaded || size < 0 ? -1 : (message ? codec.MessageSave(value, saved) : codec.Save(value, saved));
                output.Write(written);
                if (written > 0)
                {
                    output.Write(saved, 0, checked((int)written));
                }
                output.Flush();
            }
        }

        // ---- Manifest helpers & core surfaces ----

        internal static void ReadManifest(string path)
        {
            manifestLines.Clear();
            foreach (string raw in File.ReadAllLines(path))
            {
                string text = raw.Trim();
                if (text.Length == 0 || text[0] == '#') { continue; }
                manifestLines.Add(text.Split((char[])null, StringSplitOptions.RemoveEmptyEntries));
            }
        }

        internal static IEnumerable<string[]> Kind(string kind)
        {
            foreach (string[] f in manifestLines)
            {
                if (f[0] == kind) { yield return f; }
            }
        }

        internal static string[] FindLine(Predicate<string[]> pred)
        {
            return manifestLines.Find(pred);
        }

        internal static void SpillAbsent(string outDir, string name, string why = "")
        {
            File.WriteAllText(Path.Combine(outDir, name + ".absent"), why);
        }

        private static bool NoText(string[] f) => f.Length > 5 && f[5] == "no-text";

        private static bool StrictRefused(object value, DriverReport r) => value == null || r.Refused || r.Malformed;

        private static bool SpillWire(ICodec codec, object value, string outDir, string name)
        {
            long size = codec.Measure(value);
            if (size < 0)
            {
                Console.Error.WriteLine("driver: " + name + " holds a value its measure refuses");
                return false;
            }
            byte[] buffer = new byte[checked((int)size)];
            if (codec.Save(value, buffer) != size)
            {
                Console.Error.WriteLine("driver: " + name + " saves a size its measure did not name");
                return false;
            }
            File.WriteAllBytes(Path.Combine(outDir, name + ".wire"), buffer);
            return true;
        }

        private static int AnswerRead(string outDir, string name, ICodec codec, object value)
        {
            long size = codec.ToJsonMeasure(value);
            if (size < 0)
            {
                Console.Error.WriteLine("driver: " + name + " holds a value ToJson refuses");
                return 1;
            }
            byte[] text = new byte[checked((int)size)];
            if (codec.ToJson(value, text) != size)
            {
                Console.Error.WriteLine("driver: " + name + " writes a text its measure did not name");
                return 1;
            }
            if (!SpillWire(codec, value, outDir, name)) { return 1; }
            byte[] head = Encoding.ASCII.GetBytes("read\n");
            byte[] both = new byte[head.Length + text.Length];
            head.CopyTo(both, 0);
            text.CopyTo(both, head.Length);
            File.WriteAllBytes(Path.Combine(outDir, name), both);
            return 0;
        }

        private static int SurfaceWire(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("instance"))
            {
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasWire)
                {
                    SpillAbsent(outDir, f[1], "root not carried on wire");
                    continue;
                }
                byte[] wire = File.ReadAllBytes(f[4]);
                var rep = new DriverReport();
                object value = codec.Load(wire, rep);
                if (value == null)
                {
                    if (rep.ReasonText != null && rep.ReasonText.Contains("blob_shared", StringComparison.Ordinal))
                    {
                        File.WriteAllText(Path.Combine(outDir, f[1] + ".absent"), "blob_shared");
                        continue;
                    }
                    Console.Error.WriteLine("driver: " + f[1] + " does not load (" + rep.ReasonText + ")");
                    return 1;
                }
                long size = codec.Measure(value);
                if (size < 0)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " measure refused the value");
                    return 1;
                }
                byte[] buffer = new byte[checked((int)size)];
                if (codec.Save(value, buffer) != size)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " saves a size its measure did not name");
                    return 1;
                }
                File.WriteAllBytes(Path.Combine(outDir, f[1]), buffer);
            }
            return 0;
        }

        private static int SurfaceMessage(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("message"))
            {
                string[] conn = FindLine(c => c[0] == "connection" && c[1] == f[2]);
                ICodec codec = FindCodec(conn[2], f[3]);
                if (f[5] == "-" || (codec != null && !codec.HasMessage))
                {
                    File.WriteAllText(Path.Combine(outDir, f[1] + ".absent"), "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)");
                    continue;
                }
                if (codec == null)
                {
                    SpillAbsent(outDir, f[1]);
                    continue;
                }
                var rep = new DriverReport();
                object value = codec.MessageLoad(File.ReadAllBytes(conn[4]), File.ReadAllBytes(f[5]), rep);
                if (StrictRefused(value, rep))
                {
                    Console.Error.WriteLine("message read failed: " + f[1] + " (" + rep.ReasonText + ")");
                    return 1;
                }
                long size = codec.MessageMeasure(value);
                if (size < 0) { return 1; }
                byte[] bytes = new byte[checked((int)size)];
                if (codec.MessageSave(value, bytes) != size) { return 1; }
                File.WriteAllBytes(Path.Combine(outDir, f[1]), bytes);
            }
            // message-batch <case> <connection> <root> <wire>: the count
            // LoadMessages returned with room for every body (FORMAT.md)
            foreach (string[] f in Kind("message-batch"))
            {
                string[] conn = FindLine(c => c[0] == "connection" && c[1] == f[2]);
                ICodec codec = FindCodec(conn[2], f[3]);
                if (codec == null || !codec.HasMessage) { Console.Error.WriteLine("driver: no message codec for " + conn[2] + "." + f[3]); return 1; }
                int count = codec.MessageBatchCount(File.ReadAllBytes(conn[4]), File.ReadAllBytes(f[4]));
                File.WriteAllText(Path.Combine(outDir, f[1]), "count " + count + "\n");
            }
            return 0;
        }

        private static int SurfaceReport(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("report"))
            {
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasWire)
                {
                    SpillAbsent(outDir, f[1]);
                    continue;
                }
                var rep = new DriverReport();
                object value = codec.Load(File.ReadAllBytes(f[4]), rep);
                if (StrictRefused(value, rep))
                {
                    File.WriteAllText(Path.Combine(outDir, f[1]), "refused\n");
                    continue;
                }
                int rc = AnswerRead(outDir, f[1], codec, value);
                if (rc != 0) { return rc; }
            }
            return 0;
        }

        private static int SurfaceFraming(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("framing"))
            {
                bool noValue = f.Length > 5 && f[5] == "no-value";
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null)
                {
                    SpillAbsent(outDir, f[1]);
                    continue;
                }
                byte[] wire = File.ReadAllBytes(f[4]);
                var rep = new DriverReport();
                object value = codec.IsFixed ? codec.VariableLoad(wire, rep) : codec.Load(wire, rep);
                if (StrictRefused(value, rep))
                {
                    File.WriteAllText(Path.Combine(outDir, f[1]), "refused\n");
                    continue;
                }
                if (noValue)
                {
                    if (!SpillWire(codec, value, outDir, f[1])) { return 1; }
                    File.WriteAllText(Path.Combine(outDir, f[1]), "read\n");
                    continue;
                }
                int rc = AnswerRead(outDir, f[1], codec, value);
                if (rc != 0) { return rc; }
            }
            return 0;
        }

        private static int SurfaceRetain(string outDir, bool savedOnly)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("retain"))
            {
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasRetain)
                {
                    SpillAbsent(outDir, f[1]);
                    continue;
                }
                byte[] wire = File.ReadAllBytes(f[4]);
                bool shortBytes = f[5] == "short";
                int idCap = f[6] == "full" ? (1 << 17) : int.Parse(f[6], CultureInfo.InvariantCulture);
                RetainOutcome ans = codec.RunRetain(wire, shortBytes, idCap);
                if (!ans.Loaded)
                {
                    if (!savedOnly)
                    {
                        File.WriteAllText(Path.Combine(outDir, f[1]), "refused\n");
                    }
                    continue;
                }
                if (ans.Written < 0 || ans.Written != ans.Saved.Length)
                {
                    Console.Error.WriteLine("retain save failed: " + f[1]);
                    return 1;
                }
                if (savedOnly)
                {
                    File.WriteAllBytes(Path.Combine(outDir, f[1]), ans.Saved);
                }
                else
                {
                    long jsize = codec.ToJsonMeasure(ans.Value);
                    if (jsize < 0) { return 1; }
                    byte[] jtext = new byte[checked((int)jsize)];
                    if (codec.ToJson(ans.Value, jtext) != jsize) { return 1; }
                    byte[] head = Encoding.ASCII.GetBytes("read\n");
                    byte[] both = new byte[head.Length + jtext.Length];
                    head.CopyTo(both, 0);
                    jtext.CopyTo(both, head.Length);
                    File.WriteAllBytes(Path.Combine(outDir, f[1]), both);
                    File.WriteAllBytes(Path.Combine(outDir, f[1] + ".wire"), ans.Saved);
                }
            }
            return 0;
        }

        private static int SurfaceJsonRead(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("instance"))
            {
                if (NoText(f)) { continue; }
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasJson || !codec.HasWire)
                {
                    SpillAbsent(outDir, f[1]);
                    continue;
                }
                string path = Path.Combine("testdata", "conformance", "tables", "json", f[1] + ".json");
                var rep = new DriverReport();
                object value = codec.FromJson(File.ReadAllBytes(path), rep);
                if (StrictRefused(value, rep))
                {
                    Console.Error.WriteLine("driver: " + f[1] + " does not read as JSON (" + rep.ReasonText + ")");
                    return 1;
                }
                long size = codec.Measure(value);
                if (size < 0)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " measures as unsaveable after a clean read");
                    return 1;
                }
                byte[] buffer = new byte[checked((int)size)];
                if (codec.Save(value, buffer) != size)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " saves a size its measure did not name");
                    return 1;
                }
                File.WriteAllBytes(Path.Combine(outDir, f[1]), buffer);
            }
            return 0;
        }

        private static int SurfaceJsonWrite(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("instance"))
            {
                if (NoText(f)) { continue; }
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasJson)
                {
                    SpillAbsent(outDir, f[1] + ".json");
                    continue;
                }
                var rep = new DriverReport();
                object value = codec.Load(File.ReadAllBytes(f[4]), rep);
                if (StrictRefused(value, rep))
                {
                    Console.Error.WriteLine("driver: " + f[1] + " does not load");
                    return 1;
                }
                long size = codec.ToJsonMeasure(value);
                if (size < 0)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " holds a value ToJson refuses");
                    return 1;
                }
                byte[] text = new byte[checked((int)size)];
                if (codec.ToJson(value, text) != size)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " writes a text its measure did not name");
                    return 1;
                }
                File.WriteAllBytes(Path.Combine(outDir, f[1] + ".json"), text);
            }
            foreach (string[] f in Kind("json-write-refusal"))
            {
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasJson)
                {
                    SpillAbsent(outDir, f[1] + ".json");
                    continue;
                }
                var rep = new DriverReport();
                object value = codec.Load(File.ReadAllBytes(f[4]), rep);
                if (StrictRefused(value, rep))
                {
                    Console.Error.WriteLine("driver: " + f[1] + " does not load");
                    return 1;
                }
                if (!DamageText(value, f[5]))
                {
                    Console.Error.WriteLine("driver: " + f[1] + " damage did not apply: " + f[5]);
                    return 1;
                }
                byte[] buf = new byte[1 << 20];
                long? m = RunTrappingAsserts(() => codec.ToJsonMeasure(value), out string threwM);
                long? w = m == null || m < 0 ? null : RunTrappingAsserts(() => codec.ToJson(value, buf), out string threwW);
                if (threwM != null || (m != null && m >= 0 && w != null && w >= 0))
                {
                    Console.Error.WriteLine("driver: " + f[1] + " ToJson did not refuse damaged value");
                    return 1;
                }
                File.WriteAllText(Path.Combine(outDir, f[1] + ".json"), "refused\n");
            }
            return 0;
        }

        private static int SurfaceJsonHostile(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("json-hostile"))
            {
                ICodec codec = FindCodec(f[2], f[3]);
                if (codec == null || !codec.HasJson)
                {
                    SpillAbsent(outDir, f[1]);
                    continue;
                }
                byte[] text = File.ReadAllBytes(Path.Combine(f[4], f[3] + ".json"));
                var rep = new DriverReport();
                object value = codec.FromJson(text, rep);
                if (StrictRefused(value, rep))
                {
                    File.WriteAllText(Path.Combine(outDir, f[1]), "refused\n");
                    continue;
                }
                int rc = AnswerRead(outDir, f[1], codec, value);
                if (rc != 0) { return rc; }
            }
            return 0;
        }

        private static int SurfaceCookWrite(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("cook-write"))
            {
                string[] inst = FindLine(c => c[0] == "instance" && c[1] == f[1]);
                ICodec codec = FindCodec(inst[2], inst[3]);
                if (codec == null || !codec.HasCookWrite)
                {
                    SpillAbsent(outDir, f[1]);
                    SpillAbsent(outDir, f[1] + "-be");
                    continue;
                }
                var rep = new DriverReport();
                bool fromWire = !(f.Length > 2 && f[2] != "-");
                object value = !fromWire
                    ? codec.FromJson(File.ReadAllBytes(f[2]), rep)
                    : codec.Load(File.ReadAllBytes(inst[4]), rep);
                if (StrictRefused(value, rep))
                {
                    Console.Error.WriteLine("cook input did not read: " + f[1]);
                    return 1;
                }
                if (f.Length > 4 && f[3] != "-")
                {
                    if (!ShareBlob(value, f[3], f[4]))
                    {
                        Console.Error.WriteLine("cook share did not apply: " + f[1]);
                        return 1;
                    }
                }
                long size = codec.CookMeasure(value);
                if (size < 0)
                {
                    Console.Error.WriteLine("cook measure failed: " + f[1]);
                    return 1;
                }
                byte[] bytes = new byte[checked((int)size)];
                foreach (bool big in new[] { false, true })
                {
                    if (!codec.Cook(value, bytes, big))
                    {
                        Console.Error.WriteLine("cook failed: " + f[1]);
                        return 1;
                    }
                    File.WriteAllBytes(Path.Combine(outDir, f[1] + (big ? "-be" : "")), bytes);
                }
            }
            return 0;
        }

        private const string AllSurfaces =
            "wire\nmessage\nreport\nframing\nretain\nretain-save\n" +
            "json-read\njson-write\njson-hostile\n" +
            "cook\ncook-write\ncook-foreign\n" +
            "block\nblock-foreign\nblock-dump\n" +
            "forgery\ncook-forgery\n" +
            "write-refusal\nwrite-refusal-fixed\nwrite-refusal-message\nwrite-refusal-json\n" +
            "write-refusal-cook\nwrite-refusal-block\nannounce\nread-into\njson-write-into\n";

        private static int Main(string[] args)
        {
            if (args.Length == 4 && args[0] == "--verdicts")
            {
                return RunVerdicts(args[1], args[2], args[3]);
            }
            if (args.Length == 1 && args[0] == "wire-fuzz")
            {
                return RunWireFuzz();
            }
            if (args.Length < 2)
            {
                Console.Error.WriteLine("usage: unityconformance <manifest> list | <manifest> <surface> <outdir> | wire-fuzz | --verdicts <units> <list> <out>");
                return 2;
            }
            if (args[1] == "list")
            {
                Console.Out.Write(AllSurfaces);
                return 0;
            }
            if (args.Length < 3)
            {
                return 2;
            }
            ReadManifest(args[0]);
            string surface = args[1];
            string outDir = args[2];
            switch (surface)
            {
                case "wire": return SurfaceWire(outDir);
                case "message": return SurfaceMessage(outDir);
                case "report": return SurfaceReport(outDir);
                case "framing": return SurfaceFraming(outDir);
                case "retain": return SurfaceRetain(outDir, false);
                case "retain-save": return SurfaceRetain(outDir, true);
                case "json-read": return SurfaceJsonRead(outDir);
                case "json-write": return SurfaceJsonWrite(outDir);
                case "json-hostile": return SurfaceJsonHostile(outDir);
                case "cook-write": return SurfaceCookWrite(outDir);
                case "cook": return SurfaceCook(outDir);
                case "cook-foreign": return SurfaceCookForeign(outDir);
                case "cook-forgery": return SurfaceCookForgery(outDir);
                case "block": return SurfaceBlock(outDir);
                case "block-foreign": return SurfaceBlockForeign(outDir);
                case "block-dump": return SurfaceBlockDump(outDir);
                case "forgery": return SurfaceForgery(outDir);
                case "write-refusal":
                case "write-refusal-fixed":
                case "write-refusal-message":
                case "write-refusal-cook":
                case "write-refusal-block":
                case "write-refusal-json":
                    return SurfaceWriteRefusal(outDir, surface);
                case "announce": return SurfaceAnnounce(outDir);
                case "read-into": return SurfaceReadInto(outDir);
                case "json-write-into": return SurfaceJsonWriteInto(outDir);
                default:
                    return 2;
            }
        }
    }
}
