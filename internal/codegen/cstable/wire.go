// The form-1 fixed-table wire. A single descriptor walk uses the generated,
// typed storage accessors; no reflection, boxing, or per-read scratch allocation.
package cstable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitWireSurface(st *ir.Struct) {
	cap := ir.TableWireIdCapacity(g.unit)
	g.pf("public static long %sMeasure(%s value)\n{\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(value, %sTableType(), Span<byte>.Empty, ids, true);\n}\n\n", st.Name, st.Name, cap, st.Name)
	g.pf("public static long %sSave(%s value, Span<byte> buffer)\n{\n    Span<ulong> ids = stackalloc ulong[%d];\n    return TableWire.Save(value, %sTableType(), buffer, ids, false);\n}\n\n", st.Name, st.Name, cap, st.Name)
	g.pf("public static TableWire.Verdict %sLoadVerdict(%s value, ReadOnlySpan<byte> bytes, TableReport report)\n{\n    return TableWire.Load(value, %sTableType(), bytes, report);\n}\n\n", st.Name, st.Name, st.Name)
	g.pf("public static bool %sLoad(%s value, ReadOnlySpan<byte> bytes, TableReport report)\n{\n    return %sLoadVerdict(value, bytes, report) == TableWire.Verdict.Ok;\n}\n\n", st.Name, st.Name, st.Name)
}

// The wire's clamps use integer literals at their actual width, never the
// descriptor's double range columns (which are for text/UI reflection).
func (g *tableGen) wireColumns(f *ir.Field) string {
	var b strings.Builder
	guards := tableGuardExprs(g.owner)
	if guard := guards[f.Name]; guard != "" {
		fmt.Fprintf(&b, ", WireGuard = delegate(object o) { var value = (%s)o; return %s; }", g.owner.Name, guard)
	}
	reset := &tableGen{owner: g.owner, unit: g.unit}
	reset.emitTableResetField(f)
	fmt.Fprintf(&b, ", ResetField = delegate(object o) { var value = (%s)o; %s }", g.owner.Name, strings.Join(strings.Fields(reset.schema.String()), " "))
	if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || isClassRef(f.Type) {
		return b.String()
	}
	fmt.Fprintf(&b, ", DefaultRaw = %s", csRawGet(fieldDefaultExpr(f), f.Type))
	kind := tableScalarKind(f)
	if kind == tkBool || enumRef(f) != nil {
		return b.String()
	}
	var c strings.Builder
	typ := csFieldType(f.Type)
	fmt.Fprintf(&c, "%s v = %s; ", typ, strings.TrimSuffix(strings.TrimPrefix(csRawSet("v", "raw", f.Type), "v = "), ";"))
	if f.HasIntRange {
		width := tableKindWidth(kind)
		signed := f.Type.Kind == ir.TInt && f.Type.Signed
		low, high := tableClampEnds(f, width)
		if low {
			fmt.Fprintf(&c, "if (v < %s) { r.Clamped++; v = %s; } ", csIntLit(f.IntMin, signed, width), csIntLit(f.IntMin, signed, width))
		}
		if high {
			fmt.Fprintf(&c, "if (v > %s) { r.Clamped++; v = %s; } ", csIntLit(f.IntMax, signed, width), csIntLit(f.IntMax, signed, width))
		}
	} else if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		fmt.Fprintf(&c, "if (v > %dul) { r.Clamped++; v = %d; } ", (uint64(1)<<f.Type.Width)-1, (uint64(1)<<f.Type.Width)-1)
	}
	if f.HasFloatRange {
		lit := formatFloat64
		if kind == tkF32 {
			lit = formatFloat32
		}
		fmt.Fprintf(&c, "if (v < %s) { r.Clamped++; v = %s; } else if (v > %s) { r.Clamped++; v = %s; } ", lit(f.FMin), lit(f.FMin), lit(f.FMax), lit(f.FMax))
	}
	fmt.Fprintf(&c, "return %s;", csRawGet("v", f.Type))
	fmt.Fprintf(&b, ", ClampRaw = delegate(ulong raw, TableReport r) { %s }", c.String())
	return b.String()
}

const tableWireSource = `// ---- form-1 table wire: begin ----
public static class TableWire
{
    public enum Verdict { Ok, Refused, Damaged, BodyStopped }

    // One schema-bounded vocabulary lives on the caller's stack for a save.
    // Collect follows the emitted fields in order. Measuring and writing then
    // look up stable references, so a nested length never needs patching.
    ref struct Ids
    {
        public Span<ulong> Values;
        public int Count;
        public Ids(Span<ulong> values) { Values = values; Count = 0; }
        public ulong Reference(ulong id)
        {
            for (int i = 0; i < Count; i++) { if (Values[i] == id) { return (ulong)i + 1; } }
            return 0;
        }
        public bool Add(ulong id)
        {
            if (Reference(id) != 0) { return true; }
            if (Count == Values.Length) { return false; }
            Values[Count++] = id;
            return true;
        }
    }

    ref struct Writer
    {
        public Span<byte> Buffer;
        public int Offset;
        public Writer(Span<byte> buffer) { Buffer = buffer; Offset = 0; }
        public void Byte(byte v) { Buffer[Offset++] = v; }
        public void Raw(ReadOnlySpan<byte> v) { v.CopyTo(Buffer.Slice(Offset)); Offset += v.Length; }
        public void Fixed(ulong v, int n) { for (int i = 0; i < n; i++) { Byte((byte)v); v >>= 8; } }
        public void Var(ulong v)
        {
            while (v >= 128) { Byte((byte)(v | 128)); v >>= 7; }
            Byte((byte)v);
        }
    }

    ref struct Reader
    {
        public ReadOnlySpan<byte> Buffer, Vocabulary;
        public int Offset;
        public Reader(ReadOnlySpan<byte> buffer, ReadOnlySpan<byte> vocabulary)
        { Buffer = buffer; Vocabulary = vocabulary; Offset = 0; }
        public bool Has(int n) { return n >= 0 && n <= Buffer.Length - Offset; }
        public byte Byte() { return Buffer[Offset++]; }
        public ulong Fixed(int n)
        {
            ulong v = 0;
            for (int i = 0; i < n; i++) { v |= (ulong)Byte() << (8 * i); }
            return v;
        }
        public bool Var(out ulong v)
        {
            v = 0;
            int start = Offset;
            for (int i = 0; i < 10; i++)
            {
                if (!Has(1)) { Offset = start; return false; }
                byte b = Byte();
                if (i == 9 && b > 1) { Offset = start; return false; }
                v |= (ulong)(b & 127) << (7 * i);
                if (b < 128) { if (i == 0 || b != 0) { return true; } Offset = start; return false; }
            }
            Offset = start;
            return false;
        }
        public bool Ref(out ulong reference, out ulong id)
        {
            id = 0;
            if (!Var(out reference) || reference > (ulong)(Vocabulary.Length / 8)) { return false; }
            if (reference != 0) { id = Read64(Vocabulary, (int)(reference - 1) * 8); }
            return true;
        }
        public bool Slice(out Reader sub)
        {
            sub = default;
            if (!Var(out ulong n) || n > (ulong)(Buffer.Length - Offset)) { return false; }
            sub = new Reader(Buffer.Slice(Offset, (int)n), Vocabulary);
            Offset += (int)n;
            return true;
        }
        public bool Skip(byte kind)
        {
            int width = Width(kind);
            if (width != 0) { if (!Has(width)) { return false; } Offset += width; return true; }
            switch (kind)
            {
                case 17: return Var(out _);
                case 30: return Var(out _);
                case 12: case 13: case 14: case 16: case 31: case 32: case 33:
                    return Slice(out _);
                case 15:
                    if (!Var(out ulong arm)) { return false; }
                    if (arm == 0) { return true; }
                    if (!Has(1)) { return false; }
                    Byte();
                    return Slice(out _);
                default: return false;
            }
        }
    }

    static ulong Read64(ReadOnlySpan<byte> bytes, int offset)
    {
        ulong v = 0;
        for (int i = 0; i < 8; i++) { v |= (ulong)bytes[offset + i] << (8 * i); }
        return v;
    }
    static int Width(byte k)
    {
        switch (k)
        {
            case 1: case 2: case 6: case 20: case 25: return 1;
            case 3: case 7: case 21: case 26: return 2;
            case 4: case 8: case 10: case 22: case 27: return 4;
            case 5: case 9: case 11: case 23: case 28: return 8;
            case 18: case 19: case 24: case 29: return 16;
            default: return 0;
        }
    }
    static int VarSize(ulong v) { int n = 1; while (v >= 128) { n++; v >>= 7; } return n; }
    static byte Kind(TableFieldInfo f) { return f.KeyId != null ? (byte)16 : f.IsArray ? (byte)14 : f.Kind; }
    static bool Named(string name) { return name != null && name != "???"; }
    static bool DefaultRaw(ulong raw, TableFieldInfo f)
    {
        if (f.Kind == 10) { return TableBitsToFloat((uint)raw) == TableBitsToFloat((uint)f.DefaultRaw); }
        if (f.Kind == 11) { return TableBitsToDouble(raw) == TableBitsToDouble(f.DefaultRaw); }
        return raw == f.DefaultRaw;
    }
    static bool DefaultElement(object value, TableFieldInfo f, int i)
    {
        if (f.Kind == 13) { return Empty(f.GetChild(value, i), f.Table); }
        if (f.Kind == 15) { return f.Arms.GetTag(f.GetChild(value, i)) == 0; }
        if (f.GetBuffer != null) { return f.GetBuffer(value)[i] == 0; }
        return DefaultRaw(f.GetRaw(value, i), f);
    }
    static bool Empty(object value, TableTypeInfo type)
    {
        foreach (TableFieldInfo f in type.Fields) { if (Rides(value, f)) { return false; } }
        return true;
    }
    static bool Rides(object value, TableFieldInfo f)
    {
        if (f.WireGuard != null && !f.WireGuard(value)) { return false; }
        if (f.Optional) { return f.GetPresent(value); }
        if (f.Counted) { return f.GetCount(value) != 0; }
        if (!f.IsArray) { return !DefaultElement(value, f, 0); }
        if (f.Kind == 13 && f.KeyId == null) { return true; }
        for (int i = 0; i < f.ArrayBound; i++) { if (!DefaultElement(value, f, i)) { return true; } }
        return false;
    }
    static int Count(object value, TableFieldInfo f) { return f.Counted ? f.GetCount(value) : f.ArrayBound; }
    static bool CollectElement(object value, TableFieldInfo f, int i, ref Ids ids)
    {
        if (f.Kind == 13) { return Collect(f.GetChild(value, i), f.Table, ref ids); }
        if (f.Kind == 15)
        {
            object union = f.GetChild(value, i);
            ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { return true; }
            if (tag > (ulong)f.EnumMax || !Named(f.EnumName(tag))) { return false; }
            return ids.Add(f.VariantId(tag)) && CollectPayload(union, f.Arms.Arms[(int)tag].Field, ref ids);
        }
        if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value, i);
            if (v == 0) { return true; }
            return v <= (ulong)f.EnumMax && Named(f.EnumName(v)) && ids.Add(f.VariantId(v));
        }
        return true;
    }
    static bool CollectPayload(object value, TableFieldInfo f, ref Ids ids)
    {
        if (f == null) { return true; } // selected, payload-free arm
        if (f.Counted && (Count(value, f) < 0 || Count(value, f) > f.ArrayBound)) { return false; }
        if (!f.IsArray) { return CollectElement(value, f, 0, ref ids); }
        for (int i = 0; i < Count(value, f); i++)
        {
            if (f.KeyId != null)
            {
                if (DefaultElement(value, f, i)) { continue; }
                if (!Named(f.KeyName((ulong)i + 1)) || !ids.Add(f.KeyId((ulong)i + 1))) { return false; }
            }
            if (!CollectElement(value, f, i, ref ids)) { return false; }
        }
        return true;
    }
    static bool Collect(object value, TableTypeInfo type, ref Ids ids)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (f.WireGuard != null && !f.WireGuard(value)) { continue; }
            if (f.Optional && !f.GetPresent(value)) { continue; }
            if (f.Counted && (Count(value, f) < 0 || Count(value, f) > f.ArrayBound)) { return false; }
            if (Rides(value, f) && (!ids.Add(f.Id) || !CollectPayload(value, f, ref ids))) { return false; }
        }
        return true;
    }
    static long PayloadSize(object value, TableFieldInfo f, ref Ids ids)
    {
        if (f == null) { return 0; }
        if (f.IsArray) { return ArraySize(value, f, ref ids); }
        if (f.Kind == 12) { return Count(value, f); }
        return ElementSize(value, f, 0, ref ids, false);
    }
    static long ElementSize(object value, TableFieldInfo f, int i, ref Ids ids, bool framed)
    {
        if (f.Kind == 13)
        {
            long n = BodySize(f.GetChild(value, i), f.Table, ref ids);
            return n + (framed ? VarSize((ulong)n) : 0);
        }
        if (f.Kind == 15)
        {
            object union = f.GetChild(value, i);
            ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { return 1; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            long n = PayloadSize(union, arm.Field, ref ids);
            return VarSize(ids.Reference(f.VariantId(tag))) + 1 + VarSize((ulong)n) + n;
        }
        if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value, i);
            return VarSize(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        return Width(f.Kind);
    }
    static long ArraySize(object value, TableFieldInfo f, ref Ids ids)
    {
        long n = 0; int count = 0;
        for (int i = 0; i < Count(value, f); i++)
        {
            if (f.KeyId != null)
            {
                if (DefaultElement(value, f, i)) { continue; }
                long elem = ElementSize(value, f, i, ref ids, false);
                n += VarSize(ids.Reference(f.KeyId((ulong)i + 1))) + VarSize((ulong)elem) + elem;
            }
            else { n += ElementSize(value, f, i, ref ids, true); }
            count++;
        }
        return 1 + VarSize((ulong)count) + n;
    }
    static long BodySize(object value, TableTypeInfo type, ref Ids ids)
    {
        long n = 1;
        foreach (TableFieldInfo f in type.Fields)
        {
            if (!Rides(value, f)) { continue; }
            n += VarSize(ids.Reference(f.Id)) + 1;
            if (f.IsArray) { long a = ArraySize(value, f, ref ids); n += VarSize((ulong)a) + a; }
            else if (f.Kind == 12) { int s = Count(value, f); n += VarSize((ulong)s) + s; }
            else { n += ElementSize(value, f, 0, ref ids, true); }
        }
        return n;
    }
    static void WriteElement(ref Writer w, object value, TableFieldInfo f, int i, ref Ids ids, bool framed)
    {
        if (f.Kind == 13)
        {
            object child = f.GetChild(value, i);
            if (framed) { w.Var((ulong)BodySize(child, f.Table, ref ids)); }
            WriteBody(ref w, child, f.Table, ref ids);
        }
        else if (f.Kind == 15)
        {
            object union = f.GetChild(value, i); ulong tag = f.Arms.GetTag(union);
            if (tag == 0) { w.Var(0); return; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            w.Var(ids.Reference(f.VariantId(tag))); w.Byte(arm.Field == null ? (byte)32 : Kind(arm.Field));
            w.Var((ulong)PayloadSize(union, arm.Field, ref ids));
            WritePayload(ref w, union, arm.Field, ref ids);
        }
        else if (f.Kind == 30)
        {
            ulong v = f.GetRaw(value, i);
            w.Var(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        else { w.Fixed(f.GetBuffer != null ? f.GetBuffer(value)[i] : f.GetRaw(value, i), Width(f.Kind)); }
    }
    static void WritePayload(ref Writer w, object value, TableFieldInfo f, ref Ids ids)
    {
        if (f == null) { return; }
        if (f.IsArray)
        {
            w.Byte(f.Kind);
            int count = Count(value, f);
            if (f.KeyId != null)
            {
                count = 0;
                for (int i = 0; i < f.ArrayBound; i++) { if (!DefaultElement(value, f, i)) { count++; } }
            }
            w.Var((ulong)count);
            for (int i = 0; i < Count(value, f); i++)
            {
                if (f.KeyId != null)
                {
                    if (DefaultElement(value, f, i)) { continue; }
                    w.Var(ids.Reference(f.KeyId((ulong)i + 1)));
                    w.Var((ulong)ElementSize(value, f, i, ref ids, false));
                }
                WriteElement(ref w, value, f, i, ref ids, f.KeyId == null);
            }
        }
        else if (f.Kind == 12) { w.Raw(f.GetBuffer(value).AsSpan(0, Count(value, f))); }
        else { WriteElement(ref w, value, f, 0, ref ids, false); }
    }
    static void WriteBody(ref Writer w, object value, TableTypeInfo type, ref Ids ids)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (!Rides(value, f)) { continue; }
            w.Var(ids.Reference(f.Id)); w.Byte(Kind(f));
            if (f.IsArray || f.Kind == 12 || f.Kind == 13) { w.Var((ulong)PayloadSize(value, f, ref ids)); }
            WritePayload(ref w, value, f, ref ids);
        }
        w.Var(0);
    }
    public static long Save(object value, TableTypeInfo type, Span<byte> buffer, Span<ulong> vocabulary, bool measure)
    {
        Ids ids = new Ids(vocabulary);
        if (!Collect(value, type, ref ids)) { return -1; }
        long n = 1 + BodySize(value, type, ref ids) + 8L * ids.Count + 8;
        if (measure) { return n; }
        if (n > buffer.Length) { return -1; }
        Writer w = new Writer(buffer);
        w.Byte(1); WriteBody(ref w, value, type, ref ids);
        for (int i = 0; i < ids.Count; i++) { w.Fixed(ids.Values[i], 8); }
        w.Fixed((ulong)ids.Count, 8);
        return w.Offset;
    }

    static bool Widen(byte from, byte to)
    {
        return (from >= 2 && to <= 5 && from < to) || (from >= 6 && to <= 9 && from < to) || (from == 10 && to == 11);
    }
    // Numeric float conversion quiets signalling NaNs. Carry their sign and
    // payload by bits; ordinary finite values use the exact hardware widening.
    static ulong WidenFloat(uint raw)
    {
        if ((raw & 0x7f800000u) == 0x7f800000u)
        { return ((ulong)(raw & 0x80000000u) << 32) | 0x7ff0000000000000ul | ((ulong)(raw & 0x7fffffu) << 29); }
        return TableDoubleToBits((double)TableBitsToFloat(raw));
    }
    static bool Compatible(byte from, byte to, TableReport report)
    {
        if (from == to) { return true; }
        if (Widen(from, to)) { report.Widened++; return true; }
        report.KindMismatch++; return false;
    }
    static bool Damage(TableReport report) { report.Malformed = true; return false; }
    static bool EndsEarly(Reader r)
    {
        while (r.Ref(out ulong reference, out _))
        {
            if (reference == 0) { return r.Offset != r.Buffer.Length; }
            if (!r.Has(1) || !r.Skip(r.Byte())) { return false; }
        }
        return false;
    }
    static int FindVariant(TableFieldInfo f, ulong id, bool key)
    {
        int max = key ? f.ArrayBound : (int)f.EnumMax;
        for (int i = 1; i <= max; i++)
        {
            if (Named(key ? f.KeyName((ulong)i) : f.EnumName((ulong)i)) &&
                (key ? f.KeyId((ulong)i) : f.VariantId((ulong)i)) == id) { return i; }
        }
        return 0;
    }
    static bool ReadScalar(ref Reader r, object value, TableFieldInfo f, int index, byte kind, TableReport report)
    {
        ulong raw;
        if (kind == 30)
        {
            if (!r.Ref(out ulong reference, out ulong id)) { return Damage(report); }
            int variant = reference == 0 ? 0 : FindVariant(f, id, false);
            if (reference != 0 && variant == 0) { report.Unknown++; }
            raw = (ulong)variant;
        }
        else
        {
            int width = Width(kind);
            if (!r.Has(width)) { return Damage(report); }
            raw = r.Fixed(width);
            if (kind >= 2 && kind <= 5 && width < 8)
            { int shift = 64 - width * 8; raw = unchecked((ulong)((long)(raw << shift) >> shift)); }
            if (kind == 1) { raw = raw == 0 ? 0ul : 1ul; }
            if (kind == 10 && f.Kind == 11) { raw = WidenFloat((uint)raw); }
            if (f.ClampRaw != null) { raw = f.ClampRaw(raw, report); }
        }
        if (f.GetBuffer != null) { f.GetBuffer(value)[index] = (byte)raw; }
        else { f.SetRaw(value, index, raw); }
        return true;
    }
    static bool ReadElement(ref Reader r, object value, TableFieldInfo f, int index, byte kind, TableReport report, bool framed)
    {
        if (kind == 13)
        {
            Reader sub = r;
            if (framed && !r.Slice(out sub)) { return Damage(report); }
            object child = f.GetChild(value, index);
            ReadBody(ref sub, child, f.Table, report, true);
            if (!f.IsArray && sub.Offset != sub.Buffer.Length) { f.Table.Reset(child); Damage(report); }
            if (!framed) { r.Offset = r.Buffer.Length; }
            return true;
        }
        if (kind == 15)
        {
            if (!r.Var(out ulong reference)) { return Damage(report); }
            object union = f.GetChild(value, index);
            // Array elements establish None as soon as their reference is
            // present, even if its index or the following arm header is bad.
            // A scalar union field retains its old selection until the whole
            // header is bounded, matching the C++ reader under repeated fields.
            if (f.IsArray) { f.Arms.SetTag(union, 0); }
            if (reference == 0) { f.Arms.SetTag(union, 0); return true; }
            if (reference > (ulong)r.Vocabulary.Length / 8) { return Damage(report); }
            ulong id = Read64(r.Vocabulary, ((int)reference - 1) * 8);
            if (!r.Has(1)) { return Damage(report); }
            byte armKind = r.Byte();
            if (!r.Slice(out Reader arm)) { return Damage(report); }
            f.Arms.SetTag(union, 0);
            int tag = FindVariant(f, id, false);
            if (tag == 0) { report.Unknown++; return true; }
            TableUnionArmInfo info = f.Arms.Arms[tag];
            byte expected = info.Field == null ? (byte)32 : Kind(info.Field);
            // A widening arm must first have exactly the source kind's width.
            // Damaged framing never records a successful widening event.
            if (Widen(armKind, expected) && arm.Buffer.Length != Width(armKind))
            { Damage(report); return true; }
            if (!Compatible(armKind, expected, report)) { return true; }
            f.Arms.SetTag(union, (ulong)tag);
            if (!ReadArm(ref arm, union, info.Field, armKind, report)) { f.Arms.SetTag(union, 0); }
            return true;
        }
        return ReadScalar(ref r, value, f, index, kind, report);
    }
    // Selection establishes storage. Table bodies take their declared defaults;
    // general arms are zeroed, including unused tails of arrays of plain types.
    internal static void ResetArm(object value, TableFieldInfo f)
    {
        if (f.Kind == 13 && !f.IsArray) { f.Table.Reset(f.GetChild(value, 0)); }
        else { ZeroField(value, f); }
    }
    static void ZeroField(object value, TableFieldInfo f)
    {
        if (f.GetBuffer != null) { Array.Clear(f.GetBuffer(value), 0, f.ArrayBound); }
        else
        {
            int n = f.IsArray ? f.ArrayBound : 1;
            for (int i = 0; i < n; i++)
            {
                if (f.Kind == 13)
                {
                    object child = f.GetChild(value, i);
                    foreach (TableFieldInfo field in f.Table.Fields) { ZeroField(child, field); }
                }
                else if (f.Kind == 15) { f.Arms.SetTag(f.GetChild(value, i), 0); }
                else { f.SetRaw(value, i, 0); }
            }
        }
        if (f.Counted) { f.SetCount(value, 0); }
        if (f.Optional) { f.SetPresent(value, false); }
    }
    static bool ReadArm(ref Reader r, object value, TableFieldInfo f, byte kind, TableReport report)
    {
        if (f == null) { return r.Buffer.Length == 0 || Damage(report); }
        int width = Width(kind);
        if (width != 0 && r.Buffer.Length != width) { return Damage(report); }
        if (kind == 13)
        {
            ReadBody(ref r, f.GetChild(value, 0), f.Table, report, true);
            return r.Offset == r.Buffer.Length || Damage(report);
        }
        if (kind == 15) { return ReadElement(ref r, value, f, 0, kind, report, true); }
        if (kind == 12)
        {
            ResetArm(value, f);
            return ReadText(r.Buffer, value, f, report);
        }
        if (kind == 14)
        {
            ResetArm(value, f);
            if (!r.Has(2)) { return true; } // an inert short arm still selects
            byte elementKind = r.Byte();
            if (!r.Var(out ulong count)) { return Damage(report); }
            if (!Compatible(elementKind, f.Kind, report)) { return false; }
            int keep = (int)Math.Min(count, (ulong)f.ArrayBound);
            if (count > (ulong)f.ArrayBound) { report.Clamped++; }
            int decoded = 0;
            for (int i = 0; i < keep; i++)
            {
                if (!ReadElement(ref r, value, f, i, elementKind, report, true)) { break; }
                decoded++;
            }
            if (f.Counted) { f.SetCount(value, decoded); }
            return true; // damaged elements preserve the selected arm's prefix
        }
        if (!ReadScalar(ref r, value, f, 0, kind, report)) { return false; }
        return r.Offset == r.Buffer.Length || Damage(report);
    }
    static bool ReadText(ReadOnlySpan<byte> text, object value, TableFieldInfo f, TableReport report)
    {
        if (!TextValid(text)) { return Damage(report); }
        int keep = text.Length;
        if (keep > f.ArrayBound)
        {
            keep = f.ArrayBound; report.Clamped++;
            while (keep > 0 && (text[keep] & 0xc0) == 0x80) { keep--; }
        }
        text.Slice(0, keep).CopyTo(f.GetBuffer(value)); f.SetCount(value, keep);
        return true;
    }
    internal static bool TextValid(ReadOnlySpan<byte> text)
    {
        for (int i = 0; i < text.Length;)
        {
            byte c = text[i++];
            if (c == 0) { return false; }
            if (c < 128) { continue; }
            int n; uint v, min;
            if (c >= 0xc2 && c <= 0xdf) { n = 1; v = (uint)(c & 31); min = 0x80; }
            else if (c >= 0xe0 && c <= 0xef) { n = 2; v = (uint)(c & 15); min = 0x800; }
            else if (c >= 0xf0 && c <= 0xf4) { n = 3; v = (uint)(c & 7); min = 0x10000; }
            else { return false; }
            if (n > text.Length - i) { return false; }
            for (int j = 0; j < n; j++)
            { byte b = text[i++]; if ((b & 0xc0) != 0x80) { return false; } v = (v << 6) | (uint)(b & 63); }
            if (v < min || v > 0x10ffff || (v >= 0xd800 && v <= 0xdfff)) { return false; }
        }
        return true;
    }
    static bool ReadField(ref Reader r, object value, TableFieldInfo f, byte kind, TableReport report, out bool placed)
    {
        placed = true;
        if (kind == 12)
        {
            if (!r.Slice(out Reader text)) { return Damage(report); }
            if (!ReadText(text.Buffer, value, f, report)) { f.ResetField(value); }
            return true;
        }
        if (kind == 14 || kind == 16)
        {
            if (!r.Slice(out Reader array)) { return Damage(report); }
            if (array.Buffer.Length < 2) { return true; } // inert, including under a repeat
            byte elementKind = array.Byte();
            // Match the reference's header cursor: the count is read at the
            // enclosing cursor, then all elements are bounded by the array's L.
            // A count spilling past L can clamp, but cannot invent an element.
            Reader header = new Reader(r.Buffer.Slice(r.Offset - array.Buffer.Length + 1), r.Vocabulary);
            if (!header.Var(out ulong count)) { Damage(report); return true; }
            array.Offset = Math.Min(array.Buffer.Length, 1 + header.Offset);
            if (!Compatible(elementKind, f.Kind, report)) { placed = false; return true; }
            if (kind == 16)
            {
                for (ulong i = 0; i < count; i++)
                {
                    if (!array.Ref(out ulong reference, out ulong id) || reference == 0 || !array.Slice(out Reader element))
                    { Damage(report); break; }
                    int slot = FindVariant(f, id, true);
                    if (slot == 0) { report.Unknown++; continue; }
                    ReadElement(ref element, value, f, slot - 1, elementKind, report, false);
                }
            }
            else
            {
                int keep = (int)Math.Min(count, (ulong)f.ArrayBound);
                if (count > (ulong)f.ArrayBound) { report.Clamped++; }
                int decoded = 0;
                for (int i = 0; i < keep; i++)
                {
                    if (!ReadElement(ref array, value, f, i, elementKind, report, true)) { break; }
                    decoded++;
                }
                if (f.Counted) { f.SetCount(value, decoded); }
            }
            return true;
        }
        return ReadElement(ref r, value, f, 0, kind, report, true);
    }
    static bool ReadBody(ref Reader r, object value, TableTypeInfo type, TableReport report, bool nested)
    {
        type.Reset(value);
        for (;;)
        {
            if (!r.Ref(out ulong reference, out ulong id)) { return Damage(report); }
            if (reference == 0) { return true; }
            if ((nested && id == ulong.MaxValue) || id == ulong.MaxValue - 1 || id == ulong.MaxValue - 2) { return Damage(report); }
            if (!r.Has(1)) { return Damage(report); }
            byte kind = r.Byte();
            TableFieldInfo field = null;
            foreach (TableFieldInfo f in type.Fields) { if (f.Id == id) { field = f; break; } }
            if (field == null)
            {
                report.Unknown++;
                if (!r.Skip(kind)) { return Damage(report); }
                continue;
            }
            if (!Compatible(kind, Kind(field), report))
            {
                if (!r.Skip(kind)) { return Damage(report); }
                continue;
            }
            if (!ReadField(ref r, value, field, kind, report, out bool placed)) { return false; }
            if (field.Optional && placed) { field.SetPresent(value, true); }
        }
    }
    static Verdict Finish(TableReport report, Verdict verdict) { report.Verdict = verdict; return verdict; }
    public static Verdict Load(object value, TableTypeInfo type, ReadOnlySpan<byte> bytes, TableReport report)
    {
        if (report == null) { report = new TableReport(); }
        type.Reset(value);
        report.Refused = false; report.Reason = null;
        if (bytes.Length == 0) { Damage(report); return Finish(report, Verdict.Damaged); }
        if (bytes[0] != 1)
        {
            report.Refused = true;
            report.Reason = bytes[0] == 2 ? "message_form_as_file" : "newer_form";
            return Finish(report, Verdict.Refused);
        }
        if (bytes.Length < 9) { Damage(report); return Finish(report, Verdict.Damaged); }
        ulong count = Read64(bytes, bytes.Length - 8);
        if (count > (ulong)(bytes.Length - 9) / 8) { Damage(report); return Finish(report, Verdict.Damaged); }
        int end = bytes.Length - 8 - (int)count * 8;
        ReadOnlySpan<byte> vocabulary = bytes.Slice(end, (int)count * 8);
        for (int i = 0; i < vocabulary.Length; i += 8)
        {
            ulong id = Read64(vocabulary, i);
            for (int j = 0; j < i; j += 8)
            { if (id == Read64(vocabulary, j)) { Damage(report); return Finish(report, Verdict.Damaged); } }
        }
        Reader r = new Reader(bytes.Slice(1, end - 1), vocabulary);
        if (EndsEarly(r)) { Damage(report); return Finish(report, Verdict.Damaged); }
        return Finish(report, ReadBody(ref r, value, type, report, false) ? Verdict.Ok : Verdict.BodyStopped);
    }
}
// ---- form-1 table wire: end ----
`
