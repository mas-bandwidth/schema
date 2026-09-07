package cstable

const tableRegionWriteSource = `
    static bool RegionDefaultElement(NativeValue value, TableFieldInfo f, int i)
    {
        if (f.Kind == 17) { return value.Pointer(f,i)<0; }
        if (f.Kind == 13) { return RegionEmpty(value.Child(f,i), f.Table); }
        if (f.Kind == 15) { return value.Child(f,i).Tag(f) == 0; }
        if (f.GetWide != null) { return value.Wide(f,i) == f.DefaultWide; }
        if (f.GetBuffer != null) { return value.Buffer(f)[i] == 0; }
        return DefaultRaw(value.Raw(f,i), f);
    }
    static bool RegionEmpty(NativeValue value, TableTypeInfo type)
    {
        foreach (TableFieldInfo f in type.Fields) { if (RegionRides(value, f)) { return false; } }
        return true;
    }
    static bool RegionRides(NativeValue value, TableFieldInfo f)
    {
        if (!value.Guard(f)) { return false; }
        if (f.Optional) { return value.Present(f); }
        if (f.Counted) {
            int n = value.Count(f);
            if (f.DefaultBytes != null) { return n != f.DefaultBytes.Length || !value.Buffer(f).Slice(0, n).SequenceEqual(f.DefaultBytes); }
            return n != 0;
        }
        if (!f.IsArray) { return !RegionDefaultElement(value, f, 0); }
        if (f.Kind == 13 && f.KeyId == null) { return true; }
        for (int i = 0; i < f.ArrayBound; i++) { if (!RegionDefaultElement(value, f, i)) { return true; } }
        return false;
    }
    static int RegionCount(NativeValue value, TableFieldInfo f) { return f.Counted ? value.Count(f) : f.ArrayBound; }
    static bool RegionCollectElement(NativeValue value, TableFieldInfo f, int i, ref RegionIds ids)
    {
        if (f.Kind == 13) { return RegionCollect(value.Child(f,i), f.Table, ref ids); }
        if (f.Kind == 15)
        {
            NativeValue union = value.Child(f,i);
            ulong tag = union.Tag(f);
            if (tag == 0) { return true; }
            if (tag > (ulong)f.EnumMax || !Named(f.EnumName(tag))) { return false; }
            return ids.Add(f.VariantId(tag)) && RegionCollectPayload(union.Arm(f), f.Arms.Arms[(int)tag].Field, ref ids);
        }
        if (f.Kind == 30)
        {
            ulong v = value.Raw(f,i);
            if (v == 0) { return true; }
            return v <= (ulong)f.EnumMax && Named(f.EnumName(v)) && ids.Add(f.VariantId(v));
        }
        return true;
    }
    static bool RegionCollectPayload(NativeValue value, TableFieldInfo f, ref RegionIds ids)
    {
        if (f == null) { return true; } // selected, payload-free arm
        if (f.Counted && (RegionCount(value, f) < 0 || RegionCount(value, f) > f.ArrayBound)) { return false; }
        if (!f.IsArray) { return RegionCollectElement(value, f, 0, ref ids); }
        for (int i = 0; i < RegionCount(value, f); i++)
        {
            if (f.KeyId != null)
            {
                if (RegionDefaultElement(value, f, i)) { continue; }
                if (!Named(f.KeyName((ulong)i + 1)) || !ids.Add(f.KeyId((ulong)i + 1))) { return false; }
            }
            if (!RegionCollectElement(value, f, i, ref ids)) { return false; }
        }
        return true;
    }
    static bool RegionCollect(NativeValue value, TableTypeInfo type, ref RegionIds ids)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (!value.Guard(f)) { continue; }
            if (f.Optional && !value.Present(f)) { continue; }
            if (f.Counted && (RegionCount(value, f) < 0 || RegionCount(value, f) > f.ArrayBound)) { return false; }
            if (RegionRides(value, f) && (!ids.Add(f.Id) || !RegionCollectPayload(value, f, ref ids))) { return false; }
        }
        return true;
    }
    static long RegionPayloadSize(NativeValue value, TableFieldInfo f, ref RegionIds ids)
    {
        if (f == null) { return 0; }
        if (f.IsArray) { return RegionArraySize(value, f, ref ids); }
        if (f.Kind == 12 || f.Kind == 33) { return RegionCount(value, f) * (f.Kind == 33 ? 2L : 1L); }
        return RegionElementSize(value, f, 0, ref ids, false);
    }
    static long RegionElementSize(NativeValue value, TableFieldInfo f, int i, ref RegionIds ids, bool framed)
    {
        if (f.Kind == 13)
        {
            long n = RegionBodySize(value.Child(f,i), f.Table, ref ids);
            return n + (framed ? VarSize((ulong)n) : 0);
        }
        if (f.Kind == 15)
        {
            NativeValue union = value.Child(f,i);
            ulong tag = union.Tag(f);
            if (tag == 0) { return 1; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            long n = RegionPayloadSize(union.Arm(f), arm.Field, ref ids);
            return VarSize(ids.Reference(f.VariantId(tag))) + 1 + VarSize((ulong)n) + n;
        }
        if (f.Kind == 30)
        {
            ulong v = value.Raw(f,i);
            return VarSize(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        if (f.Kind == 17) { return VarSize(ids.Graph.Index(value.Pointer(f,i))); }
        return Width(f.Kind);
    }
    static long RegionArraySize(NativeValue value, TableFieldInfo f, ref RegionIds ids)
    {
        long n = 0; int count = 0;
        for (int i = 0; i < RegionCount(value, f); i++)
        {
            if (f.KeyId != null)
            {
                if (RegionDefaultElement(value, f, i)) { continue; }
                long elem = RegionElementSize(value, f, i, ref ids, false);
                n += VarSize(ids.Reference(f.KeyId((ulong)i + 1))) + VarSize((ulong)elem) + elem;
            }
            else { n += RegionElementSize(value, f, i, ref ids, true); }
            count++;
        }
        return 1 + VarSize((ulong)count) + n;
    }
    static long RegionBodySize(NativeValue value, TableTypeInfo type, ref RegionIds ids)
    {
        long n = 1;
        foreach (TableFieldInfo f in type.Fields)
        {
            if (!RegionRides(value, f)) { continue; }
            n += VarSize(ids.Reference(f.Id)) + 1;
            if (f.IsArray) { long a = RegionArraySize(value, f, ref ids); n += VarSize((ulong)a) + a; }
            else if (f.Kind == 12 || f.Kind == 33) { long s = RegionPayloadSize(value, f, ref ids); n += VarSize((ulong)s) + s; }
            else { n += RegionElementSize(value, f, 0, ref ids, true); }
        }
        return n;
    }
    static void RegionWriteElement(ref Writer w, NativeValue value, TableFieldInfo f, int i, ref RegionIds ids, bool framed)
    {
        if (f.Kind == 13)
        {
            NativeValue child = value.Child(f,i);
            if (framed) { w.Var((ulong)RegionBodySize(child, f.Table, ref ids)); }
            RegionWriteBody(ref w, child, f.Table, ref ids);
        }
        else if (f.Kind == 15)
        {
            NativeValue union = value.Child(f,i); ulong tag = union.Tag(f);
            if (tag == 0) { w.Var(0); return; }
            TableUnionArmInfo arm = f.Arms.Arms[(int)tag];
            w.Var(ids.Reference(f.VariantId(tag))); w.Byte(arm.Field == null ? (byte)32 : Kind(arm.Field));
            w.Var((ulong)RegionPayloadSize(union.Arm(f), arm.Field, ref ids));
            RegionWritePayload(ref w, union.Arm(f), arm.Field, ref ids);
        }
        else if (f.Kind == 30)
        {
            ulong v = value.Raw(f,i);
            w.Var(v == 0 ? 0 : ids.Reference(f.VariantId(v)));
        }
        else if (f.Kind == 17) { w.Var(ids.Graph.Index(value.Pointer(f,i))); }
        else if (f.GetWide != null) {
            UInt128 raw = value.Wide(f,i); int width = Width(f.Kind);
            w.Fixed((ulong)raw, Math.Min(width, 8)); if (width == 16) { w.Fixed((ulong)(raw >> 64), 8); }
        }
        else { w.Fixed(f.GetBuffer != null ? value.Buffer(f)[i] : value.Raw(f,i), Width(f.Kind)); }
    }
    static void RegionWritePayload(ref Writer w, NativeValue value, TableFieldInfo f, ref RegionIds ids)
    {
        if (f == null) { return; }
        if (f.IsArray)
        {
            w.Byte(f.Kind);
            int count = RegionCount(value, f);
            if (f.KeyId != null)
            {
                count = 0;
                for (int i = 0; i < f.ArrayBound; i++) { if (!RegionDefaultElement(value, f, i)) { count++; } }
            }
            w.Var((ulong)count);
            for (int i = 0; i < RegionCount(value, f); i++)
            {
                if (f.KeyId != null)
                {
                    if (RegionDefaultElement(value, f, i)) { continue; }
                    w.Var(ids.Reference(f.KeyId((ulong)i + 1)));
                    w.Var((ulong)RegionElementSize(value, f, i, ref ids, false));
                }
                RegionWriteElement(ref w, value, f, i, ref ids, f.KeyId == null);
            }
        }
        else if (f.Kind == 33) { Span<char> chars = value.Chars(f); for (int i = 0; i < RegionCount(value, f); i++) { w.Fixed(chars[i], 2); } }
        else if (f.Kind == 12) { w.Raw(value.Buffer(f).Slice(0, RegionCount(value, f))); }
        else { RegionWriteElement(ref w, value, f, 0, ref ids, false); }
    }
    static void RegionWriteBody(ref Writer w, NativeValue value, TableTypeInfo type, ref RegionIds ids, bool root = false)
    {
        foreach (TableFieldInfo f in type.Fields)
        {
            if (!RegionRides(value, f)) { continue; }
            w.Var(ids.Reference(f.Id)); w.Byte(Kind(f));
            if (f.IsArray || f.Kind == 12 || f.Kind == 33 || f.Kind == 13) { w.Var((ulong)RegionPayloadSize(value, f, ref ids)); }
            RegionWritePayload(ref w, value, f, ref ids);
        }
        if (root) { RegionWriteNodes(ref w, ref ids); }
        w.Var(0);
    }
    public static unsafe long SaveRegion(IntPtr pointer, TableTypeInfo type, Span<byte> buffer, Span<ulong> vocabulary, bool measure)
    {
        if(pointer==IntPtr.Zero) { return -1; }
        NativeValue value=new NativeValue((byte*)pointer,0);
        RegionIds ids = new RegionIds(vocabulary);
        if (type.Variable) { ids.Graph = RegionNumber(value, type); if (ids.Graph == null) { return -1; } }
        if (!RegionCollect(value, type, ref ids) || !RegionCollectNodes(ref ids)) { return -1; }
        long n = 1 + RegionBodySize(value, type, ref ids) + 8L * ids.Count + 8;
        long nodes = RegionNodesSize(ref ids);
        if (nodes != 0) { n += VarSize(ids.Reference(ulong.MaxValue)) + 1 + VarSize((ulong)nodes) + nodes; }
        if (measure) { return n; }
        if (n > buffer.Length) { return -1; }
        Writer w = new Writer(buffer);
        w.Byte(1); RegionWriteBody(ref w, value, type, ref ids, true);
        for (int i = 0; i < ids.Count; i++) { w.Fixed(ids.Values[i], 8); }
        w.Fixed((ulong)ids.Count, 8);
        return w.Offset;
    }

`
