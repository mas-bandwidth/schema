using System;
using System.Buffers.Binary;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Reflection;
using System.Text;

namespace SchemaUnityConformance
{
    internal static partial class Driver
    {
        // ---- Debug.Assert trap for write-refusal & json-write-refusal ----

        private sealed class WriteAssertException : Exception
        {
            public WriteAssertException(string message) : base(message) { }
        }

        private sealed class WriteAssertListener : TraceListener
        {
            public override void Write(string message) { }
            public override void WriteLine(string message) { }
            public override void Fail(string message) => throw new WriteAssertException(message);
            public override void Fail(string message, string detailMessage) => throw new WriteAssertException(message);
        }

        internal static long? RunTrappingAsserts(Func<long> action, out string threw)
        {
            threw = null;
            var saved = new TraceListener[Trace.Listeners.Count];
            Trace.Listeners.CopyTo(saved, 0);
            Trace.Listeners.Clear();
            Trace.Listeners.Add(new WriteAssertListener());
            try
            {
                return action();
            }
            catch (WriteAssertException)
            {
                return null;
            }
            catch (TargetInvocationException tie) when (tie.InnerException is WriteAssertException)
            {
                return null;
            }
            catch (Exception ex)
            {
                Exception inner = ex is TargetInvocationException t && t.InnerException != null ? t.InnerException : ex;
                threw = "threw " + inner.GetType().Name;
                return null;
            }
            finally
            {
                Trace.Listeners.Clear();
                Trace.Listeners.AddRange(saved);
            }
        }

        private static string PascalName(string snake)
        {
            var sb = new StringBuilder(snake.Length);
            bool upper = true;
            foreach (char c in snake)
            {
                if (c == '_') { upper = true; continue; }
                sb.Append(upper ? char.ToUpperInvariant(c) : c);
                upper = false;
            }
            return sb.ToString();
        }

        private static int BufferCapacity(object buf)
        {
            if (buf is byte[] ba) { return ba.Length; }
            if (buf is Array arr) { return arr.Length; }
            FieldInfo capField = buf.GetType().GetField("Capacity", BindingFlags.Public | BindingFlags.Static);
            if (capField != null) { return (int)capField.GetRawConstantValue(); }
            return buf.GetType().GetFields(BindingFlags.Public | BindingFlags.Instance).Length;
        }

        private static object BufferGet(object buf, int index)
        {
            if (buf is Array arr) { return arr.GetValue(index); }
            PropertyInfo idx = buf.GetType().GetProperty("Item", BindingFlags.Public | BindingFlags.Instance);
            return idx.GetValue(buf, new object[] { index });
        }

        private static void BufferSet(object buf, int index, object elem)
        {
            if (buf is Array arr)
            {
                arr.SetValue(elem, index);
                return;
            }
            PropertyInfo idx = buf.GetType().GetProperty("Item", BindingFlags.Public | BindingFlags.Instance);
            idx.SetValue(buf, elem, new object[] { index });
        }

        internal static bool ShareBlob(object value, string source, string alias)
        {
            Type t = value.GetType();
            FieldInfo sf = t.GetField(PascalName(source), BindingFlags.Public | BindingFlags.Instance);
            FieldInfo af = t.GetField(PascalName(alias), BindingFlags.Public | BindingFlags.Instance);
            if (sf == null || af == null) { return false; }
            af.SetValue(value, sf.GetValue(value));
            return true;
        }

        internal static bool DamageText(object value, string damage)
        {
            if (damage == "none") { return true; }
            string[] parts = damage.Split(':', 2);
            if (parts.Length != 2) { return false; }
            string member = PascalName(parts[1]);
            Type t = value.GetType();
            FieldInfo bufField = t.GetField(member, BindingFlags.Public | BindingFlags.Instance);
            FieldInfo lenField = t.GetField(member + "Length", BindingFlags.Public | BindingFlags.Instance);
            if (bufField == null || lenField == null) { return false; }
            object buf = bufField.GetValue(value);
            if (buf == null) { return false; }
            int cap = BufferCapacity(buf);
            if (parts[0] == "narrow")
            {
                if (cap < 3) { return false; }
                BufferSet(buf, 0, (byte)0x61);
                BufferSet(buf, 1, (byte)0xFF);
                BufferSet(buf, 2, (byte)0x62);
                bufField.SetValue(value, buf);
                lenField.SetValue(value, 3);
                return true;
            }
            if (parts[0] == "wide")
            {
                // UTF-8 byte storage cannot represent an unpaired UTF-16 code unit separate from narrow bytes unless char[]
                if (buf is char[] ca && ca.Length >= 3)
                {
                    ca[0] = 'a'; ca[1] = '\uD800'; ca[2] = 'b';
                    lenField.SetValue(value, 3);
                    return true;
                }
                return false;
            }
            return false;
        }

        // ---- Write-refusal surfaces ----

        private static byte[] ParseHexBytes(string hex)
        {
            byte[] b = new byte[hex.Length / 2];
            for (int i = 0; i < b.Length; i++)
            {
                b[i] = byte.Parse(hex.AsSpan(i * 2, 2), NumberStyles.HexNumber, CultureInfo.InvariantCulture);
            }
            return b;
        }

        private static bool MutatePath(ICodec codec, byte[] wire, object holder, string[] segments, int segIdx, string kind, string text)
        {
            string seg = segments[segIdx];
            int bracket = seg.IndexOf('[');
            string fieldRaw = bracket >= 0 ? seg.Substring(0, bracket) : seg;
            int? elemIdx = bracket >= 0
                ? int.Parse(seg.Substring(bracket + 1, seg.IndexOf(']') - bracket - 1), CultureInfo.InvariantCulture)
                : (int?)null;

            string member = PascalName(fieldRaw);
            Type holderType = holder.GetType();
            FieldInfo f = holderType.GetField(member, BindingFlags.Public | BindingFlags.Instance);
            if (f == null)
            {
                throw new InvalidOperationException($"write-refusal: missing field {member} on {holderType.FullName}");
            }

            if (segIdx < segments.Length - 1)
            {
                object child = f.GetValue(holder);
                if (elemIdx.HasValue)
                {
                    object elem = BufferGet(child, elemIdx.Value);
                    if (!MutatePath(codec, wire, elem, segments, segIdx + 1, kind, text)) { return false; }
                    BufferSet(child, elemIdx.Value, elem);
                }
                else
                {
                    if (!MutatePath(codec, wire, child, segments, segIdx + 1, kind, text)) { return false; }
                }
                f.SetValue(holder, child);
                return true;
            }

            // Leaf mutation
            if (elemIdx.HasValue)
            {
                throw new InvalidOperationException("write-refusal: unexpected indexed leaf " + seg);
            }

            switch (kind)
            {
                case "int":
                {
                    Type ft = f.FieldType;
                    object converted;
                    if (ft == typeof(sbyte)) { converted = checked((sbyte)long.Parse(text, CultureInfo.InvariantCulture)); }
                    else if (ft == typeof(short)) { converted = checked((short)long.Parse(text, CultureInfo.InvariantCulture)); }
                    else if (ft == typeof(int)) { converted = checked((int)long.Parse(text, CultureInfo.InvariantCulture)); }
                    else if (ft == typeof(long)) { converted = long.Parse(text, CultureInfo.InvariantCulture); }
                    else if (ft == typeof(byte)) { converted = checked((byte)ulong.Parse(text, CultureInfo.InvariantCulture)); }
                    else if (ft == typeof(ushort)) { converted = checked((ushort)ulong.Parse(text, CultureInfo.InvariantCulture)); }
                    else if (ft == typeof(uint)) { converted = checked((uint)ulong.Parse(text, CultureInfo.InvariantCulture)); }
                    else if (ft == typeof(ulong)) { converted = ulong.Parse(text, CultureInfo.InvariantCulture); }
                    else { throw new InvalidOperationException("write-refusal: unsupported int field type " + ft); }
                    f.SetValue(holder, converted);
                    return true;
                }
                case "float":
                {
                    double dv = text == "nan" ? double.NaN
                        : text == "inf" ? double.PositiveInfinity
                        : text == "-inf" ? double.NegativeInfinity
                        : double.Parse(text, NumberStyles.Float, CultureInfo.InvariantCulture);
                    if (f.FieldType == typeof(float)) { f.SetValue(holder, (float)dv); }
                    else { f.SetValue(holder, dv); }
                    return true;
                }
                case "ordinal":
                {
                    ulong raw = ulong.Parse(text, CultureInfo.InvariantCulture);
                    Type under = Enum.GetUnderlyingType(f.FieldType);
                    object rawVal = under == typeof(byte) ? (object)checked((byte)raw)
                        : under == typeof(ushort) ? checked((ushort)raw)
                        : under == typeof(uint) ? checked((uint)raw)
                        : under == typeof(ulong) ? raw
                        : under == typeof(sbyte) ? checked((sbyte)raw)
                        : under == typeof(short) ? checked((short)raw)
                        : under == typeof(int) ? checked((int)raw)
                        : checked((long)raw);
                    f.SetValue(holder, Enum.ToObject(f.FieldType, rawVal));
                    return true;
                }
                case "tag":
                {
                    object unionObj = f.GetValue(holder);
                    FieldInfo typeField = unionObj.GetType().GetField("Type", BindingFlags.Public | BindingFlags.Instance);
                    Type under = Enum.GetUnderlyingType(typeField.FieldType);
                    ulong raw = ulong.Parse(text, CultureInfo.InvariantCulture);
                    object rawVal = under == typeof(byte) ? (object)checked((byte)raw)
                        : under == typeof(ushort) ? checked((ushort)raw)
                        : under == typeof(uint) ? checked((uint)raw)
                        : checked((int)raw);
                    typeField.SetValue(unionObj, Enum.ToObject(typeField.FieldType, rawVal));
                    // Initialize any reference-type arm fields if null, and Init struct arm fields via Schema.Init<T> if present
                    Type schema = FindSchemaType(codec.UnitAssembly);
                    foreach (FieldInfo af in unionObj.GetType().GetFields(BindingFlags.Public | BindingFlags.Instance))
                    {
                        if (af.Name == "Type") { continue; }
                        if (af.FieldType.IsValueType)
                        {
                            MethodInfo initM = schema?.GetMethod("New" + af.FieldType.Name, BindingFlags.Public | BindingFlags.Static, null, Type.EmptyTypes, null);
                            if (initM != null)
                            {
                                af.SetValue(unionObj, initM.Invoke(null, null));
                            }
                        }
                        else if (af.GetValue(unionObj) == null && !af.FieldType.IsArray)
                        {
                            af.SetValue(unionObj, Activator.CreateInstance(af.FieldType));
                        }
                    }
                    f.SetValue(holder, unionObj);
                    return true;
                }
                case "count":
                {
                    int n = int.Parse(text, CultureInfo.InvariantCulture);
                    FieldInfo cf = holderType.GetField(member + "Count", BindingFlags.Public | BindingFlags.Instance);
                    int oldCount = (int)cf.GetValue(holder);
                    object buf = f.GetValue(holder);
                    int cap = BufferCapacity(buf);
                    for (int i = oldCount; i < Math.Min(n, cap); i++)
                    {
                        object freshRoot = codec.Load(wire, new DriverReport());
                        object freshBuf = f.GetValue(freshRoot);
                        BufferSet(buf, i, BufferGet(freshBuf, 0));
                    }
                    f.SetValue(holder, buf);
                    cf.SetValue(holder, n);
                    return true;
                }
                case "length":
                case "bytes":
                {
                    FieldInfo lf = holderType.GetField(member + "Length", BindingFlags.Public | BindingFlags.Instance);
                    object buf = f.GetValue(holder);
                    int cap = BufferCapacity(buf);
                    if (kind == "length")
                    {
                        int n = int.Parse(text, CultureInfo.InvariantCulture);
                        for (int i = 0; i < Math.Min(n, cap); i++) { BufferSet(buf, i, (byte)'a'); }
                        f.SetValue(holder, buf);
                        lf.SetValue(holder, n);
                        return true;
                    }
                    byte[] rawBytes = ParseHexBytes(text);
                    if (rawBytes.Length > cap) { return false; }
                    for (int i = 0; i < rawBytes.Length; i++) { BufferSet(buf, i, rawBytes[i]); }
                    f.SetValue(holder, buf);
                    lf.SetValue(holder, rawBytes.Length);
                    return true;
                }
                case "units":
                {
                    // Unity C# stores wstring as UTF-8 bytes, so unpaired UTF-16 code units are unrepresentable unless char[]
                    return false;
                }
                default:
                    throw new InvalidOperationException("write-refusal: unknown kind " + kind);
            }
        }

        private static bool WriteRefusalSet(ICodec codec, byte[] wire, object value, string path, string kind, string text)
        {
            try
            {
                string[] segments = path.Split('.');
                return MutatePath(codec, wire, value, segments, 0, kind, text);
            }
            catch (OverflowException)
            {
                return false;
            }
        }

        private static string WriteRefusalBinaryVerdict(Func<long> write)
        {
            long? n = RunTrappingAsserts(write, out string threw);
            if (threw != null) { return threw; }
            return n == null || n < 0 ? "refused" : "wrote";
        }

        private static string WriteRefusalJsonVerdict(ICodec codec, object value, string kind, string text)
        {
            byte[] buffer = new byte[1 << 20];
            long? n = RunTrappingAsserts(() => codec.ToJson(value, buffer), out string threw);
            if (threw != null) { return threw; }
            if (n == null || n < 0) { return "refused"; }
            string json = Encoding.UTF8.GetString(buffer, 0, checked((int)n));
            var spelled = new StringBuilder();
            if (kind == "bytes")
            {
                foreach (byte b in ParseHexBytes(text))
                {
                    spelled.Append(b == 0 ? "\\u0000" : ((char)b).ToString());
                }
            }
            return json.Contains("\"" + spelled + "\"", StringComparison.Ordinal) ? "wrote" : "misspelled";
        }

        private static int SurfaceWriteRefusal(string outDir, string surface)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind(surface))
            {
                if (f.Length < 8) { return 1; }
                string unit = f[2];
                if (surface == "write-refusal-message")
                {
                    string[] conn = FindLine(c => c[0] == "connection" && c[1] == f[2]);
                    if (conn == null) { return 1; }
                    unit = conn[2];
                }
                ICodec codec = FindCodec(unit, f[3]);
                if (codec == null)
                {
                    SpillAbsent(outDir, f[1]);
                    continue;
                }
                byte[] wire = File.ReadAllBytes(f[4]);
                object value = codec.Load(wire, new DriverReport());
                if (value == null)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " does not load");
                    return 1;
                }
                string verdict;
                if (!WriteRefusalSet(codec, wire, value, f[5], f[6], f[7]))
                {
                    verdict = "unrepresentable";
                }
                else
                {
                    byte[] buffer = new byte[1 << 20];
                    switch (surface)
                    {
                        case "write-refusal-fixed":
                            verdict = WriteRefusalBinaryVerdict(() => codec.FixedSave(value, buffer));
                            break;
                        case "write-refusal-message":
                            verdict = WriteRefusalBinaryVerdict(() => codec.MessageSave(value, buffer));
                            break;
                        case "write-refusal-json":
                            verdict = WriteRefusalJsonVerdict(codec, value, f[6], f[7]);
                            break;
                        default:
                            verdict = WriteRefusalBinaryVerdict(() => codec.VariableSave(value, buffer));
                            break;
                    }
                }
                File.WriteAllText(Path.Combine(outDir, f[1]), verdict + "\n");
            }
            return 0;
        }

        // ---- Block & Cook helpers ----

        private static byte[] SwapMagic(byte[] src)
        {
            byte[] dst = (byte[])src.Clone();
            if (dst.Length >= 8)
            {
                for (int i = 0; i < 4; i++)
                {
                    byte t = dst[i];
                    dst[i] = dst[7 - i];
                    dst[7 - i] = t;
                }
            }
            return dst;
        }

        private static void AppendDumpText(StringBuilder into, byte[] bytes, int at, int used)
        {
            if (used < 0) { used = 0; }
            into.Append('"');
            for (int i = 0; i < used; i++)
            {
                byte c = bytes[at + i];
                if (c >= 0x20 && c < 0x7f && c != (byte)'"' && c != (byte)'\\')
                {
                    into.Append((char)c);
                }
                else
                {
                    into.Append("\\x").Append(c.ToString("x2", CultureInfo.InvariantCulture));
                }
            }
            into.Append("\" len=").Append(used.ToString(CultureInfo.InvariantCulture));
        }

        private static Type FindBlockType(string blockCaseName)
        {
            Assembly asm = LoadUnit(UnitsRoot(), "blockdemo");
            if (asm == null) { return null; }
            Type schema = FindSchemaType(asm);
            if (schema == null) { return null; }
            string ns = schema.Namespace;
            if (blockCaseName.StartsWith("block_render", StringComparison.Ordinal))
            {
                return asm.GetType(ns + ".RenderFrameBlock");
            }
            if (blockCaseName.StartsWith("block_padded", StringComparison.Ordinal))
            {
                return asm.GetType(ns + ".PaddedFrameBlock");
            }
            return null;
        }

        private static (bool opened, object blockObj, byte[] buffer, int baseOff) TryOpenBlock(string blockCaseName, byte[] image, long extent, string pointerMode)
        {
            Type blockType = FindBlockType(blockCaseName);
            if (blockType == null)
            {
                throw new InvalidOperationException("driver: no block type for " + blockCaseName);
            }
            long claim = extent < 0 ? image.LongLength : extent;
            bool isNull = pointerMode == "null";
            int lead = isNull ? 0 : int.Parse(pointerMode, CultureInfo.InvariantCulture);
            byte[] buffer = null;
            if (!isNull)
            {
                long alloc = Math.Max(0L, Math.Min(claim, 64L << 20));
                buffer = new byte[checked((int)(lead + alloc))];
                int copy = (int)Math.Min(Math.Max(0L, claim), Math.Min(alloc, image.LongLength));
                if (copy > 0) { Array.Copy(image, 0, buffer, lead, copy); }
            }
            MethodInfo open = blockType.GetMethod("Open", BindingFlags.Public | BindingFlags.Static, null,
                new[] { blockType.MakeByRefType(), typeof(byte[]), typeof(int), typeof(long) }, null);
            object[] args = new object[] { null, buffer, lead, claim };
            bool ok = (bool)open.Invoke(null, args);
            return (ok, args[0], buffer, lead);
        }

        private static int SurfaceBlock(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("block"))
            {
                var r = TryOpenBlock(f[1], File.ReadAllBytes(f[3]), -1, "0");
                File.WriteAllText(Path.Combine(outDir, f[1]), r.opened ? "open\n" : "refuse\n");
            }
            return 0;
        }

        private static int SurfaceBlockForeign(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("block"))
            {
                var r = TryOpenBlock(f[1], SwapMagic(File.ReadAllBytes(f[3])), -1, "0");
                File.WriteAllText(Path.Combine(outDir, f[1]), r.opened ? "open\n" : "refuse\n");
            }
            return 0;
        }

        private static int SurfaceForgery(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("forgery"))
            {
                if (f[2] != "block") { continue; }
                long extent = long.Parse(f[5], CultureInfo.InvariantCulture);
                string ptr = f.Length > 6 ? f[6] : "0";
                var r = TryOpenBlock(f[3], File.ReadAllBytes(f[4]), extent, ptr);
                File.WriteAllText(Path.Combine(outDir, f[1]), r.opened ? "open\n" : "refuse\n");
            }
            return 0;
        }

        private static void DumpBlockScalar(StringBuilder into, byte[] bytes, int at, byte kind, int width)
        {
            ReadOnlySpan<byte> s = bytes.AsSpan(at, width);
            switch (kind)
            {
                case 1:
                    into.Append(s[0] != 0 ? "true" : "false");
                    return;
                case 10:
                    into.Append("0x").Append(BinaryPrimitives.ReadUInt32LittleEndian(s).ToString("x8", CultureInfo.InvariantCulture));
                    return;
                case 11:
                    into.Append("0x").Append(BinaryPrimitives.ReadUInt64LittleEndian(s).ToString("x16", CultureInfo.InvariantCulture));
                    return;
                case 2:
                case 3:
                case 4:
                case 5:
                {
                    long v = width == 1 ? (sbyte)s[0]
                        : width == 2 ? BinaryPrimitives.ReadInt16LittleEndian(s)
                        : width == 4 ? BinaryPrimitives.ReadInt32LittleEndian(s)
                        : BinaryPrimitives.ReadInt64LittleEndian(s);
                    into.Append(v.ToString(CultureInfo.InvariantCulture));
                    return;
                }
                default:
                {
                    ulong v = width == 1 ? s[0]
                        : width == 2 ? BinaryPrimitives.ReadUInt16LittleEndian(s)
                        : width == 4 ? BinaryPrimitives.ReadUInt32LittleEndian(s)
                        : BinaryPrimitives.ReadUInt64LittleEndian(s);
                    into.Append(v.ToString(CultureInfo.InvariantCulture));
                    return;
                }
            }
        }

        private static bool DumpBlockRecord(StringBuilder into, byte[] bytes, int storage, object info, string path)
        {
            if (info == null) { return false; }
            Type infoT = info.GetType();
            string infoName = (string)infoT.GetField("Name").GetValue(info);
            Array fields = (Array)infoT.GetField("Fields").GetValue(info);
            foreach (object fld in fields)
            {
                Type ft = fld.GetType();
                bool outOfLine = (bool)ft.GetField("OutOfLine").GetValue(fld);
                if (outOfLine) { continue; }
                string fname = (string)ft.GetField("Name").GetValue(fld);
                string name = string.IsNullOrEmpty(path) ? fname : path + "." + fname;
                bool counted = (bool)ft.GetField("Counted").GetValue(fld);
                int offset = (int)ft.GetField("Offset").GetValue(fld);
                int countOffset = (int)ft.GetField("CountOffset").GetValue(fld);
                int arrayBound = (int)ft.GetField("ArrayBound").GetValue(fld);
                bool isArray = (bool)ft.GetField("IsArray").GetValue(fld);
                int elemSize = (int)ft.GetField("ElemSize").GetValue(fld);
                byte kind = (byte)ft.GetField("Kind").GetValue(fld);
                bool optional = (bool)ft.GetField("Optional").GetValue(fld);
                int presentOffset = (int)ft.GetField("PresentOffset").GetValue(fld);
                object elementInfo = ft.GetProperty("Element").GetValue(fld);

                if (counted)
                {
                    int used = BinaryPrimitives.ReadInt32LittleEndian(bytes.AsSpan(storage + countOffset, 4));
                    if (used < 0 || used > arrayBound)
                    {
                        Console.Error.WriteLine($"driver: {infoName}.{fname} used length {used} outside [0, {arrayBound}]");
                        return false;
                    }
                    into.Append("  ").Append(name).Append(" = ");
                    AppendDumpText(into, bytes, storage + offset, used);
                    into.Append('\n');
                }
                else
                {
                    int slots = isArray ? arrayBound : 1;
                    for (int s = 0; s < slots; s++)
                    {
                        string atName = isArray ? $"{name}[{s}]" : name;
                        int valOff = storage + offset + s * elemSize;
                        if (elementInfo != null)
                        {
                            if (!DumpBlockRecord(into, bytes, valOff, elementInfo, atName)) { return false; }
                        }
                        else
                        {
                            into.Append("  ").Append(atName).Append(" = ");
                            DumpBlockScalar(into, bytes, valOff, kind, elemSize);
                            into.Append('\n');
                        }
                    }
                }
                if (optional)
                {
                    bool present = bytes[storage + presentOffset] != 0;
                    into.Append("  ").Append(name).Append("#present = ").Append(present ? "true" : "false").Append('\n');
                }
            }
            return true;
        }

        private static int SurfaceBlockDump(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("block"))
            {
                byte[] image = File.ReadAllBytes(f[3]);
                var r = TryOpenBlock(f[1], image, -1, "0");
                if (!r.opened) { return 1; }
                Type blockType = FindBlockType(f[1]);
                object info = blockType.GetField("Type", BindingFlags.Public | BindingFlags.Static).GetValue(null);
                Type infoT = info.GetType();
                string infoName = (string)infoT.GetField("Name").GetValue(info);
                var into = new StringBuilder();
                into.Append("projection ").Append(infoName).Append(" @0\n");
                if (!DumpBlockRecord(into, r.buffer, r.baseOff, info, "")) { return 1; }

                Array fields = (Array)infoT.GetField("Fields").GetValue(info);
                foreach (object fld in fields)
                {
                    Type ft = fld.GetType();
                    if (!(bool)ft.GetField("OutOfLine").GetValue(fld)) { continue; }
                    string fname = (string)ft.GetField("Name").GetValue(fld);
                    int offsetOfOff = (int)ft.GetField("OffsetOfOffset").GetValue(fld);
                    int countOff = (int)ft.GetField("CountOffset").GetValue(fld);
                    int strideOff = (int)ft.GetField("StrideOffset").GetValue(fld);
                    object rowInfo = ft.GetProperty("Element").GetValue(fld);
                    string rowName = (string)rowInfo.GetType().GetField("Name").GetValue(rowInfo);

                    ulong offsetOf = BinaryPrimitives.ReadUInt64LittleEndian(r.buffer.AsSpan(r.baseOff + offsetOfOff, 8));
                    uint count = BinaryPrimitives.ReadUInt32LittleEndian(r.buffer.AsSpan(r.baseOff + countOff, 4));
                    uint stride = BinaryPrimitives.ReadUInt32LittleEndian(r.buffer.AsSpan(r.baseOff + strideOff, 4));
                    into.Append($"array {fname} {rowName} @{offsetOf} count={count} stride={stride}\n");
                    for (uint row = 0; row < count; row++)
                    {
                        ulong at = offsetOf + (ulong)row * stride;
                        into.Append($"row {row} @{at}\n");
                        if (!DumpBlockRecord(into, r.buffer, r.baseOff + checked((int)at), rowInfo, "")) { return 1; }
                    }
                }
                File.WriteAllText(Path.Combine(outDir, f[1]), into.ToString());
            }
            return 0;
        }

        // ---- Cook open, foreign, forgery, and node dump ----

        private static Type FindCookType(string unitKey, string rootName)
        {
            if (unitKey != null)
            {
                Assembly asm = LoadUnit(UnitsRoot(), unitKey);
                Type schema = FindSchemaType(asm);
                if (schema != null)
                {
                    Type ct = asm.GetType(schema.Namespace + "." + rootName + "Cook");
                    if (ct != null) { return ct; }
                }
            }
            // Fallback for forgery rows that name only the subject root (e.g. Scene -> graphdemo)
            foreach (string candidateUnit in new[] { "graphdemo", "tabledemo", "listdemo", "mapdemo", "blobdemo" })
            {
                Assembly asm = LoadUnit(UnitsRoot(), candidateUnit);
                Type schema = FindSchemaType(asm);
                if (schema != null)
                {
                    Type ct = asm.GetType(schema.Namespace + "." + rootName + "Cook");
                    if (ct != null) { return ct; }
                }
            }
            return null;
        }

        private static (bool opened, object cookObj, Type cookType, byte[] buffer) TryOpenCook(string unitKey, string rootName, byte[] data, long extent, string pointerMode)
        {
            Type cookType = FindCookType(unitKey, rootName);
            if (cookType == null)
            {
                throw new InvalidOperationException($"driver: no cook type for {unitKey}.{rootName}");
            }
            long claim = extent < 0 ? data.LongLength : extent;
            bool isNull = pointerMode == "null";
            int lead = isNull ? 0 : int.Parse(pointerMode, CultureInfo.InvariantCulture);
            byte[] buffer = null;
            if (!isNull)
            {
                long alloc = Math.Max(0L, Math.Min(claim, 64L << 20));
                buffer = new byte[checked((int)(lead + alloc))];
                int copy = (int)Math.Min(Math.Max(0L, claim), Math.Min(alloc, data.LongLength));
                if (copy > 0) { Array.Copy(data, 0, buffer, lead, copy); }
            }
            MethodInfo open = cookType.GetMethod("Open", BindingFlags.Public | BindingFlags.Static, null,
                new[] { cookType.MakeByRefType(), typeof(byte[]), typeof(int), typeof(long) }, null);
            object[] args = new object[] { null, buffer, lead, claim };
            bool ok = (bool)open.Invoke(null, args);
            return (ok, args[0], cookType, buffer);
        }

        private static int SurfaceCookForeign(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("cook"))
            {
                var r = TryOpenCook(f[2], f[3], SwapMagic(File.ReadAllBytes(f[4])), -1, "0");
                File.WriteAllText(Path.Combine(outDir, f[1]), r.opened ? "open\n" : "refuse\n");
            }
            return 0;
        }

        private static int SurfaceCookForgery(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("forgery"))
            {
                if (f[2] != "cook") { continue; }
                long extent = long.Parse(f[5], CultureInfo.InvariantCulture);
                string ptr = f.Length > 6 ? f[6] : "0";
                var r = TryOpenCook(null, f[3], File.ReadAllBytes(f[4]), extent, ptr);
                File.WriteAllText(Path.Combine(outDir, f[1]), r.opened ? "open\n" : "refuse\n");
            }
            return 0;
        }

        private sealed class CookWalker
        {
            private readonly byte[] bytes;
            private readonly int region;
            private readonly int length;
            private readonly List<int> offsets = new List<int>();
            private readonly List<object> types = new List<object>();
            public readonly StringBuilder Dump = new StringBuilder();

            public CookWalker(byte[] bytes, int region, int length)
            {
                this.bytes = bytes;
                this.region = region;
                this.length = length;
            }

            public void WalkNode(int offset, object typeInfo, int depth)
            {
                if (depth > 4096) { throw new InvalidOperationException("cook walk exceeded depth cap"); }
                Type infoT = typeInfo.GetType();
                string typeName = (string)infoT.GetField("Name").GetValue(typeInfo);
                int typeSize = (int)infoT.GetField("Size").GetValue(typeInfo);

                int existing = offsets.IndexOf(offset);
                if (existing >= 0)
                {
                    if (!ReferenceEquals(types[existing], typeInfo))
                    {
                        throw new InvalidOperationException($"conflicting cook node types at @{offset}");
                    }
                    return;
                }
                if (offset < 0 || offset > length || typeSize > length - offset)
                {
                    throw new InvalidOperationException($"cook node {typeName} @{offset} outside region length {length}");
                }
                int index = offsets.Count;
                offsets.Add(offset);
                types.Add(typeInfo);
                Dump.Append($"node {index} {typeName} @{offset}\n");
                WalkStorage(region + offset, typeInfo, "", depth);
            }

            private void Emit(string path, string val)
            {
                Dump.Append("  ").Append(path).Append(" = ").Append(val).Append('\n');
            }

            private void WalkStorage(int at, object typeInfo, string path, int depth)
            {
                Type infoT = typeInfo.GetType();
                string typeName = (string)infoT.GetField("Name").GetValue(typeInfo);
                Array fields = (Array)infoT.GetField("Fields").GetValue(typeInfo);
                foreach (object fld in fields)
                {
                    Type ft = fld.GetType();
                    string fname = (string)ft.GetField("Name").GetValue(fld);
                    string name = string.IsNullOrEmpty(path) ? fname : path + "." + fname;
                    int offset = (int)ft.GetField("Offset").GetValue(fld);
                    int elemSize = (int)ft.GetField("ElemSize").GetValue(fld);
                    bool isArray = (bool)ft.GetField("IsArray").GetValue(fld);
                    int arrayBound = (int)ft.GetField("ArrayBound").GetValue(fld);
                    bool isPointer = (bool)ft.GetField("IsPointer").GetValue(fld);
                    int countOffset = (int)ft.GetField("CountOffset").GetValue(fld);
                    int presentOffset = (int)ft.GetField("PresentOffset").GetValue(fld);
                    string storageKind = ft.GetField("Storage").GetValue(fld).ToString();
                    object childInfo = ft.GetProperty("Record").GetValue(fld);

                    int used = -1;
                    if (countOffset >= 0)
                    {
                        used = BinaryPrimitives.ReadInt32LittleEndian(bytes.AsSpan(at + countOffset, 4));
                        if (used < 0 || used > arrayBound)
                        {
                            throw new InvalidOperationException($"{typeName}.{fname} count {used} outside [0, {arrayBound}]");
                        }
                    }

                    if (isPointer)
                    {
                        int slot = at + offset;
                        long delta = BinaryPrimitives.ReadInt64LittleEndian(bytes.AsSpan(slot, 8));
                        if (delta == 0)
                        {
                            Emit(name, "null");
                            continue;
                        }
                        long target = slot + delta;
                        if (target < region || target >= (long)region + length)
                        {
                            throw new InvalidOperationException($"{typeName}.{fname} delta {delta} resolves outside region");
                        }
                        if (childInfo == null)
                        {
                            throw new InvalidOperationException($"{typeName}.{fname} pointer has no Record descriptor");
                        }
                        Emit(name, $"-> @{target - region}");
                        WalkNode((int)(target - region), childInfo, depth + 1);
                        continue;
                    }

                    switch (storageKind)
                    {
                        case "String":
                        case "Bytes":
                        {
                            var sb = new StringBuilder();
                            AppendDumpText(sb, bytes, at + offset, used);
                            Emit(name, sb.ToString());
                            break;
                        }
                        case "Record":
                        {
                            int slots = isArray ? arrayBound : 1;
                            for (int s = 0; s < slots; s++)
                            {
                                string elName = isArray ? $"{name}[{s}]" : name;
                                WalkStorage(at + offset + s * elemSize, childInfo, elName, depth);
                            }
                            break;
                        }
                        default:
                        {
                            int slots = isArray ? arrayBound : 1;
                            for (int s = 0; s < slots; s++)
                            {
                                string elName = isArray ? $"{name}[{s}]" : name;
                                Emit(elName, FormatCookScalar(at + offset + s * elemSize, storageKind, elemSize));
                            }
                            break;
                        }
                    }

                    if (countOffset >= 0 && storageKind != "String" && storageKind != "Bytes")
                    {
                        Emit(name + "#count", used.ToString(CultureInfo.InvariantCulture));
                    }
                    if (presentOffset >= 0)
                    {
                        Emit(name + "#present", bytes[at + presentOffset] != 0 ? "true" : "false");
                    }
                }
            }

            private string FormatCookScalar(int at, string storageKind, int width)
            {
                ReadOnlySpan<byte> s = bytes.AsSpan(at, width);
                switch (storageKind)
                {
                    case "Boolean":
                        return s[0] != 0 ? "true" : "false";
                    case "Float":
                        throw new InvalidOperationException("cook dump met unexpected float");
                    case "Signed":
                        return (width == 1 ? (sbyte)s[0]
                            : width == 2 ? BinaryPrimitives.ReadInt16LittleEndian(s)
                            : width == 4 ? BinaryPrimitives.ReadInt32LittleEndian(s)
                            : BinaryPrimitives.ReadInt64LittleEndian(s)).ToString(CultureInfo.InvariantCulture);
                    default:
                        return (width == 1 ? (ulong)s[0]
                            : width == 2 ? BinaryPrimitives.ReadUInt16LittleEndian(s)
                            : width == 4 ? BinaryPrimitives.ReadUInt32LittleEndian(s)
                            : BinaryPrimitives.ReadUInt64LittleEndian(s)).ToString(CultureInfo.InvariantCulture);
                }
            }
        }

        private static int SurfaceCook(string outDir)
        {
            Directory.CreateDirectory(outDir);
            foreach (string[] f in Kind("cook"))
            {
                byte[] data = File.ReadAllBytes(f[4]);
                var r = TryOpenCook(f[2], f[3], data, -1, "0");
                if (!r.opened)
                {
                    Console.Error.WriteLine("driver: " + f[1] + " does not open");
                    return 1;
                }
                int region = (int)r.cookType.GetField("Region").GetValue(r.cookObj);
                int length = (int)r.cookType.GetField("Length").GetValue(r.cookObj);
                object info = r.cookType.GetField("Type", BindingFlags.Public | BindingFlags.Static).GetValue(null);
                var walker = new CookWalker(r.buffer, region, length);
                walker.WalkNode(0, info, 0);
                File.WriteAllText(Path.Combine(outDir, f[1]), walker.Dump.ToString());
            }
            return 0;
        }
    }
}
