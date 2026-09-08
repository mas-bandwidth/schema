package cstable

const tableVariableJsonSource = `
    public sealed class Label
    {
        public object Value;
        public TableTypeInfo Type;
        public bool Open;
    }
    static bool ScanLabel(ReadOnlySpan<byte> text, ref In input, out ulong label)
    {
        label = 0; byte c = Peek(text, ref input);
        if (c < '1' || c > '9') { input.Bad = true; return false; }
        while (input.Pos < text.Length && text[input.Pos] >= '0' && text[input.Pos] <= '9')
        {
            ulong digit = (ulong)(text[input.Pos++] - '0');
            if (label > (ulong.MaxValue - digit) / 10) { input.Bad = true; return false; }
            label = label * 10 + digit;
        }
        return true;
    }
    static bool ReadPointer(ReadOnlySpan<byte> text, ref In input, object owner, TableFieldInfo f, int index, int depth)
    {
        if (Peek(text, ref input) == 'n')
        { if (!Literal(text, ref input, "null")) { return false; } f.SetChild(owner, index, null); return true; }
        if (f.BlobKind != 0) { return ReadBlob(text, ref input, owner, f, index); }
        if (input.Labels == null || depth + 1 > MaxDepth || Peek(text, ref input) != '{') { input.Bad = true; return false; }
        int start = input.Pos++;
        if (Peek(text, ref input) == '}') { input.Pos++; f.SetChild(owner, index, f.Table.Create()); return true; }
        Span<byte> key = stackalloc byte[MaxKey];
        if (!ScanString(text, ref input, key, true, out int used)) { return false; }
        if (Peek(text, ref input) != ':') { input.Bad = true; return false; }
        input.Pos++;
        if (!Same(key.Slice(0, used), "&node"))
        {
            input.Pos = start;
            object single = f.Table.Create(); f.SetChild(owner, index, single);
            return ReadTable(text, ref input, single, f.Table, depth + 1);
        }
        if (!ScanLabel(text, ref input, out ulong id)) { return false; }
        bool defined = input.Labels.TryGetValue(id, out Label label);
        byte c = Peek(text, ref input);
        if (c == ',') { input.Pos++; c = Peek(text, ref input); }
        bool bare = c == '}';
        if (bare != defined) { input.Bad = true; return false; }
        if (bare)
        {
            input.Pos++;
            if (label.Open) { input.Bad = true; return false; }
            f.SetChild(owner, index, null);
            if (label.Type != null)
            {
                if (label.Type.Id != f.PointerTypeId) { input.Report.KindMismatch++; }
                else { f.SetChild(owner, index, label.Value); }
            }
            return true;
        }
        object child = f.Table.Create(); f.SetChild(owner, index, child);
        input.Labels.Add(id, new Label { Open = true });
        if (!ReadTable(text, ref input, child, f.Table, depth + 1, true)) { return false; }
        input.Labels[id] = new Label { Value = child, Type = f.Table }; return true;
    }
    static bool ReadBlob(ReadOnlySpan<byte> text, ref In input, object owner, TableFieldInfo f, int index)
    {
        f.SetChild(owner, index, null);
        if (f.BlobKind == 12)
        {
            In probe=input;
            if(!ScanText(text,ref probe,Span<byte>.Empty,Span<char>.Empty,false,false,out int size,true)) { input=probe; return false; }
            byte[] data=new byte[size];
            if (!ScanString(text, ref input, data, true, out _)) { return false; }
            f.SetChild(owner, index, new TableBlob(data)); return true;
        }
        if (Peek(text, ref input) != '"') { input.Bad = true; return false; }
        input.Pos++;
        int mark = input.Pos;
        int symbols = 0;
        int pad = 0;
        bool malformed = false;
        for (;;)
        {
            if (input.Pos >= text.Length) { input.Bad = true; return false; }
            byte c = text[input.Pos++];
            if (c == '"') { break; }
            if (malformed) { continue; }
            if (c == '=') { pad++; continue; }
            if (pad > 0) { malformed = true; continue; }
            int at = Base64Decode[c];
            if (at < 0) { malformed = true; continue; }
            symbols++;
        }
        if (!malformed)
        {
            if (pad > 0)
            {
                if (pad > 2 || (symbols + pad) % 4 != 0 || symbols % 4 < 2) { malformed = true; }
            }
            else if (symbols % 4 == 1)
            {
                malformed = true;
            }
        }
        if (malformed) { input.Report.KindMismatch++; return true; }
        byte[] bytes = new byte[symbols * 6 / 8];
        int count = 0, held = 0;
        uint accumulator = 0;
        for (int at = mark; ; at++)
        {
            byte c = text[at];
            if (c == '"' || c == '=') { break; }
            int symbol = Base64Decode[c];
            accumulator = (accumulator << 6) | (uint)symbol;
            held += 6;
            if (held >= 8)
            {
                held -= 8;
                if (count < bytes.Length) { bytes[count++] = (byte)(accumulator >> held); }
            }
        }
        f.SetChild(owner, index, new TableBlob(bytes));
        return true;
    }
    static bool WritePointer(ref Out o, object owner, TableFieldInfo f, int index, int depth)
    {
        object child = f.GetChild(owner, index);
        if (child == null) { o.Text("null"); return true; }
        if (f.BlobKind != 0)
        {
            if (o.Graph.Uses[child] > 1) { return false; }
            byte[] data = ((TableBlob)child).Data;
            if (f.BlobKind == 12) { WriteString(ref o, data); } else { WriteBase64(ref o, data); }
            return true;
        }
        if (o.Graph.Uses[child] <= 1) { return WriteValue(ref o, child, f.Table, depth); }
        if (depth > MaxDepth) { return false; }
        bool defined = o.Graph.Labels.TryGetValue(child, out ulong label);
        if (!defined) { label = (ulong)o.Graph.Labels.Count + 1; o.Graph.Labels.Add(child, label); }
        o.Put((byte)'{'); o.Line(depth + 1); WriteName(ref o, "&node"); o.Text(": "); WriteUnsigned(ref o, label);
        if (!defined)
        {
            long before = o.Offset;
            if (!WriteValue(ref o, child, f.Table, depth, true) || before == o.Offset) { return false; }
        }
        else { o.Line(depth); o.Put((byte)'}'); }
        return true;
    }
`
