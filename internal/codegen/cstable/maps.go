package cstable

const tableMapWireSource = `
    struct MapKey
    {
        public byte[] Text;
        public ulong Raw;
    }
    static MapKey KeyOf(object entry, TableFieldInfo key)
    {
        return key.Kind == 12 ? new MapKey { Text = key.GetBuffer(entry).AsSpan(0, key.GetCount(entry)).ToArray() } : new MapKey { Raw = key.GetRaw(entry, 0) };
    }
    static int KeyOrder(MapKey x, MapKey y, TableFieldInfo key)
    {
        if (key.Kind == 12) { return x.Text.AsSpan().SequenceCompareTo(y.Text); }
        return key.Kind >= 2 && key.Kind <= 5 ? unchecked((long)x.Raw).CompareTo(unchecked((long)y.Raw)) : x.Raw.CompareTo(y.Raw);
    }
    static bool ScanMapKey(Reader body, TableFieldInfo f, out MapKey key, out bool mismatch, out bool widened)
    {
        object entry = f.Table.Create(); TableFieldInfo k = f.Table.Fields[0];
        key = KeyOf(entry, k); mismatch = false; widened = false;
        TableReport ignored = new TableReport();
        for (;;)
        {
            if (!body.Ref(out ulong reference, out ulong id)) { return false; }
            if (reference == 0) { return true; }
            if (!body.Has(1)) { return false; }
            byte kind = body.Byte();
            if (id != k.Id) { if (!body.Skip(kind)) { return false; } continue; }
            if (kind != k.Kind)
            {
                if (!Widen(kind, k.Kind)) { mismatch = true; return false; }
                widened = true;
            }
            if (kind == 12)
            {
                if (!body.Slice(out Reader text) || !TextValid(text.Buffer)) { return false; }
                key.Text = text.Buffer.ToArray();
            }
            else
            {
                if (!ReadScalar(ref body, entry, k, 0, kind, ignored)) { return false; }
                key.Raw = k.GetRaw(entry, 0);
            }
        }
    }
    static bool ReadMapBody(ref Reader a, object value, TableFieldInfo f, TableReport report)
    {
        f.ResetField(value);
        if (!a.Has(2)) { return true; }
        byte kind = a.Byte();
        if (!a.Var(out ulong count)) { Damage(report); return true; }
        if (kind != 13) { report.KindMismatch++; return true; }
        int landed = 0; bool widened = false; MapKey last = default;
        TableFieldInfo keyField = f.Table.Fields[0];
        for (ulong i = 0; i < count; i++)
        {
            if (!a.Slice(out Reader body)) { Damage(report); break; }
            bool keyOk = ScanMapKey(body, f, out MapKey key, out bool mismatch, out bool entryWidened);
            if (entryWidened && !widened) { widened = true; report.Widened++; }
            if (mismatch) { report.KindMismatch++; landed = 0; break; }
            if (!keyOk) { Damage(report); break; }
            if (keyField.Kind == 12 && key.Text.Length > keyField.ArrayBound) { report.Clamped++; continue; }
            int order = landed == 0 ? -1 : KeyOrder(last, key, keyField);
            if (order > 0) { Damage(report); break; }
            int slot = order == 0 ? landed - 1 : landed;
            f.EnsureCount(value, slot + 1);
            object entry = f.Table.Create(); f.SetChild(value, slot, entry);
            ReadBody(ref body, entry, f.Table, report, true);
            if (order == 0) { report.Duplicate++; }
            else { last = KeyOf(entry, keyField); landed++; }
        }
        f.SetCount(value, landed); return true;
    }
`

const tableMapJsonSource = `
    static bool WriteMap(ref Out o, object owner, TableFieldInfo f, int depth)
    {
        int count = f.GetCount(owner);
        if (count == 0) { o.Text("{}"); return true; }
        if (!TableWire.MapOrdered(owner, f)) { return false; }
        o.Put((byte)'{'); TableFieldInfo key = f.Table.Fields[0], value = f.Table.Fields[1];
        for (int i = 0; i < count; i++)
        {
            if (i != 0) { o.Put((byte)','); } o.Line(depth + 1);
            object entry = f.GetChild(owner, i);
            if (key.Kind == 12) { WriteString(ref o, key.GetBuffer(entry).AsSpan(0, key.GetCount(entry))); }
            else
            {
                o.Put((byte)'"'); ulong raw = key.GetRaw(entry, 0);
                if (key.Kind >= 2 && key.Kind <= 5) { WriteSigned(ref o, unchecked((long)raw)); } else { WriteUnsigned(ref o, raw); }
                o.Put((byte)'"');
            }
            o.Text(": "); if (!WriteField(ref o, entry, value, depth + 1)) { return false; }
        }
        o.Line(depth); o.Put((byte)'}'); return true;
    }
    static bool ReadMap(ReadOnlySpan<byte> text, ref In input, object owner, TableFieldInfo f, int depth)
    {
        if (Peek(text, ref input) != '{') { input.Bad = true; return false; }
        input.Pos++; f.ResetField(owner);
        TableFieldInfo key = f.Table.Fields[0], field = f.Table.Fields[1];
        int count = 0;
        for (;;)
        {
            byte c = Peek(text, ref input);
            if (c == '}') { input.Pos++; return true; }
            Span<byte> spelling = stackalloc byte[MaxKey];
            int beforeClamp = input.Report.Clamped;
            if (!ScanString(text, ref input, spelling, true, out int used)) { return false; }
            bool over = input.Report.Clamped != beforeClamp;
            input.Report.Clamped = beforeClamp;
            if (Peek(text, ref input) != ':') { input.Bad = true; return false; }
            input.Pos++;
            object entry = f.Table.Create();
            bool drop = false;
            if (key.Kind == 12)
            {
                if (over || used > key.ArrayBound) { input.Report.Clamped++; drop = true; }
                else { spelling.Slice(0, used).CopyTo(key.GetBuffer(entry)); key.SetCount(entry, used); }
            }
            else if (over) { input.Report.KindMismatch++; drop = true; }
            else
            {
                In probe = new In { Report = new TableReport() };
                ReadOnlySpan<byte> token = spelling.Slice(0, used);
                Space(token, ref probe);
                if (probe.Pos != 0 || !ReadScalar(token, ref probe, entry, key, 0, depth) || probe.Pos != used || probe.Bad)
                { input.Bad = true; return false; }
                if (probe.Report.Clamped != 0 || probe.Report.KindMismatch != 0) { input.Report.KindMismatch++; drop = true; }
            }
            if (drop) { if (!SkipValue(text, ref input, depth)) { return false; } }
            else
            {
                if (field.Kind == 17 && !field.IsArray && ValueShape(text, ref input) == 'z')
                { if (!Literal(text, ref input, "null")) { return false; } }
                else if (!ReadField(text, ref input, entry, field, depth)) { return false; }
                int at = 0;
                while (at < count && TableWire.CompareKey(f.GetChild(owner, at), entry, key) < 0) { at++; }
                if (at < count && TableWire.CompareKey(f.GetChild(owner, at), entry, key) == 0)
                { f.SetChild(owner, at, entry); input.Report.Duplicate++; }
                else
                {
                    f.EnsureCount(owner, count + 1);
                    for (int i = count; i > at; i--) { f.SetChild(owner, i, f.GetChild(owner, i - 1)); }
                    f.SetChild(owner, at, entry); count++; f.SetCount(owner, count);
                }
            }
            c = Peek(text, ref input);
            if (c == ',') { input.Pos++; continue; }
            if (c == '}') { input.Pos++; return true; }
            input.Bad = true; return false;
        }
    }
`
