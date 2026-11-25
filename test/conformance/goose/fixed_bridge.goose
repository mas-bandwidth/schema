// test/conformance/goose/fixed_bridge.goose — the flat value table to a fixed
// root's record body (docs/SPEC-TABLES.md §3.4), for a fixed root the unit
// emits no typed value bridge for (no <Root>ToLoaded / <Root>FromLoaded: a
// fixed-point field, as tables/scalars SimState). fixed_body_from_loaded
// writes the record body <Root>_FixedRead reads. Spliced into the glue module
// of every unit that needs it, by the conformance driver
// (test/conformance/goose/driver) and the hostile-corpus runner
// (make/goose-hostile.sh): one copy, two callers.
recursive fn fixed_tab_size(unit: schema_rt::LoadUnit, tab: i64) -> i64 {
    if tab < 0 || tab >= unit.tables.len { return 0; }
    var total: i64 = 0;
    for f in unit.tables[tab].fields {
        total += fixed_field_size(unit, f);
    }
    return total;
}
recursive fn fixed_union_arm_bytes(unit: schema_rt::LoadUnit, ui: i64) -> i64 {
    if ui < 0 || ui >= unit.unions.len { return 0; }
    var m: i64 = 0;
    for a in unit.unions[ui].arms {
        if a.shape != schema_rt::ShapeVoid {
            let sz = fixed_field_size(unit, a);
            if sz > m { m = sz; }
        }
    }
    return m;
}
recursive fn fixed_field_inner_size(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc) -> i64 {
    if f.shape == schema_rt::ShapeBool { return 1; }
    if f.shape == schema_rt::ShapeInt { return f.kw; }
    if f.shape == schema_rt::ShapeFloat { return if f.is_f32 { 4 } else { 8 }; }
    if f.shape == schema_rt::ShapeEnum {
        if f.variants.len >= 65536 { return 4; }
        if f.variants.len >= 256 { return 2; }
        return 1;
    }
    if f.shape == schema_rt::ShapeStr || f.shape == schema_rt::ShapeBytes {
        return 4 + f.bound;
    }
    if f.shape == schema_rt::ShapeTab { return fixed_tab_size(unit, schema_rt::value_table(unit, f.ref)); }
    if f.shape == schema_rt::ShapeUnion { return 1 + fixed_union_arm_bytes(unit, schema_rt::value_union(unit, f.ref)); }
    if f.shape == schema_rt::ShapeArr {
        let elem = f.elem;
        guard elem else { return 0; }
        let esz = fixed_field_size(unit, elem);
        if f.array == schema_rt::ArrayCounted { return 4 + f.bound * esz; }
        return f.bound * esz;
    }
    return 0;
}
recursive fn fixed_field_size(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc) -> i64 {
    let inner = fixed_field_inner_size(unit, f);
    return if f.optional { 1 + inner } else { inner };
}
recursive fn fixed_tab_to_loaded(unit: schema_rt::LoadUnit, tab: i64, b: const u8[:], pos: i64&, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&) -> i64 {
    if tab < 0 || tab >= unit.tables.len { return -1; }
    let t = unit.tables[tab];
    let rec = schema_rt::value_reserve(vals, t.fields.len + 1);
    vals[rec] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeTab, made: true, tab: tab, first: rec + 1, count: t.fields.len, .. };
    var i: i64 = 0;
    while i < t.fields.len {
        if !fixed_field_to_loaded(unit, t.fields[i], b, pos, data, vals, rec + 1 + i) { return -1; }
        i += 1;
    }
    return rec;
}
recursive fn fixed_field_to_loaded(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc, b: const u8[:], pos: i64&, data: u8[>..]&, vals: schema_rt::LoadVal[>..]&, s: i64) -> bool {
    if f.optional {
        let pres = b[pos] != 0;
        pos += 1;
        if !pres {
            pos += fixed_field_inner_size(unit, f);
            return true;
        }
    }
    if f.shape == schema_rt::ShapeBool {
        let v: u64 = if b[pos] != 0 { 1 } else { 0 };
        pos += 1;
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: f.shape, bits: schema_rt::Int128 { lo: v, hi: 0 }, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeInt || f.shape == schema_rt::ShapeFloat {
        let kw: i64 = if f.shape == schema_rt::ShapeFloat { if f.is_f32 { 4 } else { 8 } } else { f.kw };
        var lo: u64 = 0;
        var hi: u64 = 0;
        if kw == 16 {
            lo = schema_rt::fixed_get(b, pos, 8);
            hi = schema_rt::fixed_get(b, pos + 8, 8);
        } else {
            lo = schema_rt::fixed_get(b, pos, kw);
            if f.shape == schema_rt::ShapeInt && f.signed {
                let sign_bit: u64 = (1 as u64) << ((kw * 8 - 1) as! u64);
                if (lo & sign_bit) != 0 {
                    if kw < 8 {
                        lo |= 0xffffffffffffffff << ((kw * 8) as! u64);
                    }
                    hi = 0xffffffffffffffff;
                }
            }
        }
        pos += kw;
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: f.shape, bits: schema_rt::Int128 { lo: lo, hi: hi }, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeEnum {
        let ew: i64 = if f.variants.len >= 65536 { 4 } else if f.variants.len >= 256 { 2 } else { 1 };
        let ord = schema_rt::fixed_get(b, pos, ew);
        pos += ew;
        vals[s] = schema_rt::LoadVal { state: if ord == 0 { schema_rt::ValEmpty } else { schema_rt::ValPresent }, shape: f.shape, bits: schema_rt::Int128 { lo: ord, hi: 0 }, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeStr || f.shape == schema_rt::ShapeBytes {
        let used: i64 = schema_rt::fixed_u32(b, pos);
        let off = data.len;
        data.append(b[pos + 4 .. pos + 4 + used]);
        pos += 4 + f.bound;
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: f.shape, off: off, len: used, .. };
        return true;
    }
    if f.shape == schema_rt::ShapeTab {
        let sub = fixed_tab_to_loaded(unit, schema_rt::value_table(unit, f.ref), b, pos, data, vals);
        if sub < 0 { return false; }
        vals[s] = vals[sub];
        return true;
    }
    if f.shape == schema_rt::ShapeUnion {
        let ui = schema_rt::value_union(unit, f.ref);
        if ui < 0 { return false; }
        let u = unit.unions[ui];
        let max_arm = fixed_union_arm_bytes(unit, ui);
        let tag = schema_rt::fixed_get(b, pos, 1) as i64;
        let rec = schema_rt::value_reserve(vals, 2);
        vals[rec] = schema_rt::LoadVal { state: if tag == 0 { schema_rt::ValEmpty } else { schema_rt::ValPresent }, shape: schema_rt::ShapeUnion, made: true, tab: ui, bits: schema_rt::Int128 { lo: tag as! u64, hi: 0 }, first: rec + 1, count: 1, .. };
        if tag >= 1 && tag <= u.arms.len && u.arms[tag - 1].shape != schema_rt::ShapeVoid {
            var arm_pos = pos + 1;
            if !fixed_field_to_loaded(unit, u.arms[tag - 1], b, arm_pos, data, vals, rec + 1) { return false; }
        }
        pos += 1 + max_arm;
        vals[s] = vals[rec];
        return true;
    }
    if f.shape == schema_rt::ShapeArr {
        let elem = f.elem;
        guard elem else { return false; }
        var live = f.bound;
        if f.array == schema_rt::ArrayCounted {
            live = schema_rt::fixed_u32(b, pos);
            pos += 4;
        }
        let first = schema_rt::value_reserve(vals, live);
        vals[s] = schema_rt::LoadVal { state: schema_rt::ValPresent, shape: schema_rt::ShapeArr, made: true, first: first, count: live, len: live, .. };
        var k: i64 = 0;
        while k < live {
            if !fixed_field_to_loaded(unit, elem, b, pos, data, vals, first + k) { return false; }
            k += 1;
        }
        if f.array == schema_rt::ArrayCounted {
            pos += (f.bound - live) * fixed_field_size(unit, elem);
        }
        return true;
    }
    return false;
}
fn fixed_body_to_loaded(unit: schema_rt::LoadUnit, root: const u8[:], b: const u8[:], data: u8[>..]&, vals: schema_rt::LoadVal[>..]&) -> i64 {
    var pos: i64 = 0;
    return fixed_tab_to_loaded(unit, schema_rt::value_table(unit, root), b, pos, data, vals);
}
recursive fn fixed_tab_from_loaded(unit: schema_rt::LoadUnit, tab: i64, data: const u8[:], vals: const schema_rt::LoadVal[:], rec: i64, out: u8[>..]&) -> bool {
    if tab < 0 || tab >= unit.tables.len { return false; }
    let t = unit.tables[tab];
    var i: i64 = 0;
    while i < t.fields.len {
        let s = schema_rt::value_child(vals, rec, i);
        if !fixed_field_from_loaded(unit, t.fields[i], data, vals, s, out) { return false; }
        i += 1;
    }
    return true;
}
recursive fn fixed_field_from_loaded(unit: schema_rt::LoadUnit, f: schema_rt::FieldDesc, data: const u8[:], vals: const schema_rt::LoadVal[:], s: i64, out: u8[>..]&) -> bool {
    let rode = schema_rt::value_rode(vals, s);
    if f.optional {
        schema_rt::fixed_put_bool(out, rode);
        if !rode {
            schema_rt::fixed_put_zeros(out, fixed_field_inner_size(unit, f));
            return true;
        }
    }
    if f.shape == schema_rt::ShapeBool {
        let bv = if rode { vals[s].bits.lo != 0 } else { f.has_def && f.def_b };
        schema_rt::fixed_put_bool(out, bv);
        return true;
    }
    if f.shape == schema_rt::ShapeInt {
        let kw = f.kw as i64;
        let bits = if rode { vals[s].bits } else if f.has_def { f.def_i } else { schema_rt::Int128 { .. } };
        if kw == 16 {
            schema_rt::fixed_put(out, bits.lo, 8);
            schema_rt::fixed_put(out, bits.hi, 8);
        } else {
            schema_rt::fixed_put(out, bits.lo, kw);
        }
        return true;
    }
    if f.shape == schema_rt::ShapeFloat {
        let kw: i64 = if f.is_f32 { 4 } else { 8 };
        let fb = if rode { vals[s].bits.lo } else if f.has_def { if f.is_f32 { schema_rt::fixed_f32_bits(f.def_f as f32) } else { schema_rt::fixed_f64_bits(f.def_f) } } else { 0 };
        schema_rt::fixed_put(out, fb, kw);
        return true;
    }
    if f.shape == schema_rt::ShapeEnum {
        let ew: i64 = if f.variants.len >= 65536 { 4 } else if f.variants.len >= 256 { 2 } else { 1 };
        let ev = if rode { vals[s].bits.lo } else if f.has_def { f.def_i.lo } else { 0 };
        schema_rt::fixed_put(out, ev, ew);
        return true;
    }
    if f.shape == schema_rt::ShapeStr || f.shape == schema_rt::ShapeBytes {
        if rode {
            let off = vals[s].off;
            let n = vals[s].len;
            if off < 0 || n < 0 || off > data.len || n > data.len - off { return false; }
            if n > f.bound { return false; }
            schema_rt::fixed_put(out, n as! u64, 4);
            out.append(data[off .. off + n]);
            schema_rt::fixed_put_zeros(out, f.bound - n);
            return true;
        }
        let def_bytes = if f.has_def { f.def_s } else { "" };
        if def_bytes.len > f.bound { return false; }
        schema_rt::fixed_put(out, def_bytes.len as! u64, 4);
        out.append(def_bytes);
        schema_rt::fixed_put_zeros(out, f.bound - def_bytes.len);
        return true;
    }
    if f.shape == schema_rt::ShapeTab {
        return fixed_tab_from_loaded(unit, schema_rt::value_table(unit, f.ref), data, vals, if rode { s } else { -1 }, out);
    }
    if f.shape == schema_rt::ShapeUnion {
        let ui = schema_rt::value_union(unit, f.ref);
        if ui < 0 { return false; }
        let u = unit.unions[ui];
        let max_arm = fixed_union_arm_bytes(unit, ui);
        let tag = if rode { vals[s].bits.lo as! i64 } else { 0 };
        if tag < 0 || tag > u.arms.len { return false; }
        schema_rt::fixed_put(out, tag as! u64, 1);
        if tag == 0 {
            schema_rt::fixed_put_zeros(out, max_arm);
            return true;
        }
        let arm = u.arms[tag - 1];
        let before = out.len;
        if arm.shape != schema_rt::ShapeVoid {
            let p = if rode && vals[s].made { vals[s].first } else { -1 };
            if !fixed_field_from_loaded(unit, arm, data, vals, p, out) { return false; }
        }
        let used = out.len - before;
        if used < max_arm {
            schema_rt::fixed_put_zeros(out, max_arm - used);
        }
        return true;
    }
    if f.shape == schema_rt::ShapeArr {
        let elem = f.elem;
        guard elem else { return false; }
        let made = rode && vals[s].made;
        var live = f.bound;
        if f.array == schema_rt::ArrayCounted {
            live = if made { vals[s].len } else { f.min };
            if live < f.min || live > f.bound { return false; }
            schema_rt::fixed_put(out, live as! u64, 4);
        }
        var k: i64 = 0;
        while k < live {
            let es = if made && k < vals[s].count { vals[s].first + k } else { -1 };
            if !fixed_field_from_loaded(unit, elem, data, vals, es, out) { return false; }
            k += 1;
        }
        if f.array == schema_rt::ArrayCounted {
            schema_rt::fixed_put_zeros(out, (f.bound - live) * fixed_field_size(unit, elem));
        }
        return true;
    }
    return false;
}
fn fixed_body_from_loaded(unit: schema_rt::LoadUnit, root: const u8[:], data: const u8[:], vals: const schema_rt::LoadVal[:], rec: i64, out: u8[>..]&) -> bool {
    let tab = schema_rt::value_table(unit, root);
    if !schema_rt::value_rode(vals, rec) || !vals[rec].made || vals[rec].tab != tab { return false; }
    return fixed_tab_from_loaded(unit, tab, data, vals, rec, out);
}
