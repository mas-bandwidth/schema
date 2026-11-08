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
        for (; ; )
        {
            if (!body.Ref(out ulong reference, out ulong id)) { return false; }
            if (reference == 0) { return body.Offset == body.Buffer.Length; }
            if (!body.Has(1)) { return false; }
            byte kind = body.Byte();
            if (id != k.Id) { if (!body.Skip(kind)) { return false; } continue; }
            if (kind != k.Kind && Widen(kind, k.Kind)) { mismatch = false; widened = true; }
            else
            {
                mismatch = kind != k.Kind;
                if (mismatch) { if (!body.Skip(kind)) { return false; } continue; }
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
        // THE HEADER LIES INSIDE THE MAP'S OWN L (docs/SPEC-TABLES.md §2.8, §3)
        if (!a.Has(2)) { return Damage(report); }
        byte kind = a.Byte();
        if (!a.Var(out ulong count)) { return Damage(report); }
        if (kind != 13) { return Fail(report, "kind_mismatch"); }
        if (count > int.MaxValue) { return CollectionCap(report); }
        f.ResetField(value);
        int landed = 0; bool widened = false; MapKey last = default;
        TableFieldInfo keyField = f.Table.Fields[0];
        for (ulong i = 0; i < count; i++)
        {
            if (!a.Slice(out Reader body)) { Damage(report); break; }
            bool keyOk = ScanMapKey(body, f, out MapKey key, out bool mismatch, out bool entryWidened);
            if (entryWidened && !widened) { widened = true; report.Widened++; }
            if (mismatch) { return Fail(report, "kind_mismatch"); }
            if (!keyOk) { Damage(report); break; }
            if (keyField.Kind == 12 && key.Text.Length > keyField.ArrayBound) { return Fail(report, "length_over_bound: " + keyField.Name); }
            int order = landed == 0 ? -1 : KeyOrder(last, key, keyField);
            if (order > 0) { Damage(report); break; }
            int slot = order == 0 ? landed - 1 : landed;
            f.EnsureCount(value, slot + 1);
            object entry = f.Table.Create(); f.SetChild(value, slot, entry);
            ReadBody(ref body, entry, f.Table, report, true);
            if (CountRefused(report)) { return false; }
            if (order == 0) { return Fail(report, "duplicate"); }
            else { last = KeyOf(entry, keyField); landed++; }
        }
        f.SetCount(value, landed);
        // bytes left inside the map's L after its entries are damage
        return report.Damaged || report.Refused || a.Offset == a.Buffer.Length || Damage(report);
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
            if (i != 0) { o.Put((byte)','); }
            o.Line(depth + 1);
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
        string mapField = input.TextField;
        Span<byte> spelling = stackalloc byte[MaxKey];
        for (; ; )
        {
            byte c = Peek(text, ref input);
            if (c == '}') { input.Pos++; return true; }
            input.TextField = mapField;
            scoped ReadOnlySpan<byte> actual; int used;
            if(key.Unbounded)
            {
                if(!DecodedStringBody(text,ref input,out actual)){return false;}
                if(!TableWire.TextValid(actual)){return RefuseText(ref input,"text_holds_zero");}
                used=actual.Length;
            }
            else {if (!ScanString(text, ref input, spelling, true, out used, stored: true)) { return false; } actual=spelling.Slice(0,used);}
            if (Peek(text, ref input) != ':') { input.Bad = true; return false; }
            input.Pos++;
            object entry = f.Table.Create();
            bool drop = false;
            if (key.Kind == 12)
            {
                if (used > key.ArrayBound) { return RefuseText(ref input, "length_over_bound"); }
                else { if(key.Unbounded){key.EnsureCount(entry,used);} actual.CopyTo(key.GetBuffer(entry)); key.SetCount(entry, used); }
            }
            else
            {
                In probe = new In { Report = new TableReport() };
                ReadOnlySpan<byte> token = actual;
                Space(token, ref probe);
                if (probe.Pos != 0 || !ReadScalar(token, ref probe, entry, key, 0, depth) || probe.Pos != used || probe.Bad)
                { input.Bad = true; return false; }
                if (probe.Report.Refused) { return RefuseText(ref input, "kind_mismatch"); }
            }
            if (drop) { if (!SkipValue(text, ref input, depth)) { return false; } }
            else
            {
                char shape = ValueShape(text, ref input);
                if ((field.Optional || field.Kind == 17 && !field.IsArray) && shape == 'z')
                {
                    if (!Literal(text, ref input, "null")) { return false; }
                    field.ResetField(entry);
                    if (field.Optional) { field.SetPresent(entry, false); }
                }
                else
                {
                    // A key names a present optional value even when the
                    // value's shape is wrong; the new entry keeps its default.
                    if (field.Optional) { field.SetPresent(entry, true); }
                    if (shape != Shape(field))
                    {
                        return RefuseText(ref input, "kind_mismatch");
                    }
                    else if (!ReadField(text, ref input, entry, field, depth)) { return false; }
                }
                int at = 0;
                while (at < count && TableWire.CompareKey(f.GetChild(owner, at), entry, key) < 0) { at++; }
                if (at < count && TableWire.CompareKey(f.GetChild(owner, at), entry, key) == 0)
                { return RefuseText(ref input, "duplicate"); }
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
