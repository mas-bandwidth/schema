package cstable

const tableLoadMeasureSource = `
    static long Align(long value, int alignment) { return (value + alignment - 1) & -(long)alignment; }
    static bool ExtentBody(Reader r, TableTypeInfo type, ref long at, ref string reason)
    {
        while (r.Ref(out ulong reference, out ulong id) && reference != 0 && r.Has(1))
        {
            byte kind = r.Byte(); TableFieldInfo field = FindField(type, id);
            if (field != null && Kind(field) != kind) { field = null; }
            if (field == null) { if (!r.Skip(kind)) { break; } continue; }
            if (!ExtentField(ref r, field, true, ref at, ref reason)) { return false; }
        }
        return true;
    }
    static bool ExtentField(ref Reader r, TableFieldInfo f, bool framed, ref long at, ref string reason)
    {
        if (f.IsArray)
        {
            Reader a = r;
            if (framed && !r.Slice(out a)) { r.Offset = r.Buffer.Length; return true; }
            if (!a.Has(2)) { return true; }
            byte sourceKind = a.Byte();
            if (sourceKind != f.Kind && !Widen(sourceKind, f.Kind) || !a.Var(out ulong n)) { return true; }
            if (f.Dynamic)
            {
                if (n > int.MaxValue) { reason = "count_over_extent_cap"; return false; }
                int floor = sourceKind == 13 ? 2 : sourceKind == 15 || sourceKind == 17 || sourceKind == 30 ? 1 : Width(sourceKind);
                if (floor < 1 || n > (ulong)((a.Buffer.Length - a.Offset) / floor)) { reason = "count_over_length"; return false; }
                at = Align(at, f.StorageAlign) + (long)n * f.StorageSize;
            }
            for (ulong i = 0; i < n && a.Has(1); i++)
            {
                Reader source = a;
                if (f.KeyId != null)
                { if (!a.Var(out _) || !a.Slice(out source)) { break; } }
                if (f.Kind == 13)
                {
                    Reader element = source;
                    if (f.KeyId == null && !a.Slice(out element)) { break; }
                    if (!ExtentBody(element, f.Table, ref at, ref reason)) { return false; }
                }
                else if (f.Kind == 15)
                {
                    if (f.KeyId != null) { if (!ExtentUnion(ref source, f, ref at, ref reason)) { return false; } }
                    else if (!ExtentUnion(ref a, f, ref at, ref reason)) { return false; }
                }
                else { break; }
            }
            if (!framed) { r.Offset = r.Buffer.Length; }
            return true;
        }
        if (f.Kind == 13)
        {
            Reader body = r;
            if (framed && !r.Slice(out body)) { r.Offset = r.Buffer.Length; return true; }
            return ExtentBody(body, f.Table, ref at, ref reason);
        }
        if (f.Kind == 15) { return ExtentUnion(ref r, f, ref at, ref reason); }
        // A value the skip cannot honour ENDS the record's scan, as the reference scan's
        // does: the reader goes to its end rather than resuming at the bytes after it.
        if (!framed || !r.Skip(f.Kind)) { r.Offset = r.Buffer.Length; }
        return true;
    }
    static bool ExtentUnion(ref Reader r, TableFieldInfo f, ref long at, ref string reason)
    {
        // A frame that does not parse ENDS the record's scan, as the reference
        // scan's does (internal/tablewire/extent.go): the reader goes to its
        // end rather than resuming at the bytes after a length or a reference
        // it could not honour, which would read a payload as fields and
        // measure storage the framing never commanded.
        if (!r.Ref(out ulong reference, out ulong id)) { r.Offset = r.Buffer.Length; return true; }
        if (reference == 0) { return true; }
        if (!r.Has(1)) { r.Offset = r.Buffer.Length; return true; }
        r.Byte(); // the framing scan follows a named arm even if its value kind mismatches
        if (!r.Slice(out Reader body)) { r.Offset = r.Buffer.Length; return true; }
        int tag = FindVariant(f, id, false);
        if (tag == 0) { return true; }
        TableFieldInfo arm = f.Arms.Arms[tag].Field;
        if (arm == null) { return true; }
        return ExtentField(ref body, arm, false, ref at, ref reason);
    }
    // Locate the winning node table and validate its framing with no objects or
    // node list. The returned reader starts at the first record, after count.
    static bool NodeFrames(Reader root, out Reader records, out int count)
    {
        records = default; count = 0; Reader scan = root, payload = default;
        bool present = false;
        while (scan.Ref(out ulong reference, out ulong id) && reference != 0 && scan.Has(1))
        {
            byte kind = scan.Byte();
            if (id == ulong.MaxValue)
            {
                present = true;
                if (kind != 12 || !scan.Slice(out payload)) { return false; }
            }
            else if (!scan.Skip(kind)) { break; }
        }
        if (!present) { return true; }
        if (!payload.Var(out ulong declared)) { return false; }
        records = payload;
        while (payload.Offset < payload.Buffer.Length)
        {
            if (!payload.Ref(out ulong reference, out _) || reference == 0 || !payload.Slice(out _)) { return false; }
            count++;
        }
        return declared == (ulong)count;
    }
    public static long LoadMeasure(TableTypeInfo type, ReadOnlySpan<byte> bytes, out string reason)
    {
        reason = null;
        if (bytes.Length < 1) { return -1; }
        if (bytes[0] != 1) { reason = "unknown_form"; return -1; }
        if (bytes.Length < 9) { return -1; }
        ulong count = Read64(bytes, bytes.Length - 8);
        if (count > (ulong)(bytes.Length - 9) / 8) { return -1; }
        int end = bytes.Length - 8 - (int)count * 8;
        ReadOnlySpan<byte> ids = bytes.Slice(end, (int)count * 8);
        for (int i = 0; i < ids.Length; i += 8)
        { for (int j = 0; j < i; j += 8) { if (Read64(ids, i) == Read64(ids, j)) { return -1; } } }
        Reader root = new Reader(bytes.Slice(1, end - 1), ids);
        if (EndsEarly(root)) { return -1; }
        long extent = 0;
        if (!ExtentBody(root, type, ref extent, ref reason)) { return -1; }
        long size = Align(Align(type.StorageSize, type.RegionAlign) + extent, type.RegionAlign);
        if (!NodeFrames(root, out Reader payload, out int nodes)) { return size + 16; }
        Reader scan = payload;
        for (int i = 0; i < nodes; i++)
        {
            scan.Ref(out _, out ulong id); scan.Slice(out Reader body);
            if ((type.BytesEdge && id == BytesTypeId) || (type.StringEdge && id == StringTypeId))
            { size += Align(8L + body.Buffer.Length + (id == StringTypeId ? 1 : 0), type.RegionAlign); continue; }
            TableTypeInfo candidate = type.PointerType(id);
            if (candidate == null) { continue; }
            extent = 0;
            if (!ExtentBody(body, candidate, ref extent, ref reason)) { return -1; }
            size += Align(Align(candidate.StorageSize, type.RegionAlign) + extent, type.RegionAlign);
        }
        return size + (nodes + 1L) * 16;
    }
`
