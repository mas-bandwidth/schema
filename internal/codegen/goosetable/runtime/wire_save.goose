// wire_save.goose — the Goose port's VARIABLE TABLE WIRE SAVE (docs/SPEC-TABLES.md
// §3, §3.1, §3.2, §2.5, §2.8; docs/PORT-GOOSE.md, THE TABLE WIRE): the writer
// of form 1, ported decision for decision from the Go reference engine
// (internal/tablewire encode.go, nodes.go, nodetable.go). It writes from the
// FLAT VALUE TABLE load.goose builds (or a caller fills), reading nothing but
// the descriptors and the unit, so one walk serves every table.
//
// The file is the FORM BYTE, the ROOT BODY, the ID TABLE (§3): every field in
// declaration order, a field at its declared default elided (and its id with
// it: an elided field costs nothing in the id table either), the node table
// last in the root body (§3.1), the ids in FIRST-USE order, then their count.
// The node numbering is RE-DERIVED here, never carried: the first-visit order
// of a depth-first pre-order walk from the root over pointer edges, through
// every by-value edge (§3.1; tablewire.Number). A blob two slots name is
// refused by name (`blob_shared`, §2.5), and so is a data cycle.
//
// THE OUTPUT IS WRITTEN IN PLACE. A nested body's length rides before it and
// is canonical LEB128, so the body is written first and its length inserted
// in front of it (`frame`); an elided body is cut back off (`out.resize`),
// and its ids rolled back with it.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;
import .schema_tables;
import .wire_load;
import .wire_retain;

let KindUnion: u8 = 15;
let KindKeyed: u8 = 16;
let KindWstring: u8 = 33;
let KindEnum: u8 = 30;
let KindU8: u8 = 6;

// SaveCtx is one save's context of borrowed tables (the Goose spec's struct-of-references pattern).
struct SaveCtx {
    data: const u8[:],
    unit: LoadUnit,
    vals: LoadVal[>..]&,
    nodes: LoadNode[>..]&,
    out: u8[>..<]&,
    ids: u64[>..<]&,        // the file's id table, first-use order (§3)
    order: i64[>..<]&,      // the records, by WIRE node index, in save order
    index: i64[>..<]&,      // the save's node index, by wire node index
    open: bool[>..<]&,      // a descent still open, by wire node index
    root_tab: i64,
    refusal: i64,           // the first refusal (SaveRefuse*), 0 while none
    retain: RetainStore&,
    retaining: bool,
    retain_lost: i64,
}

// ---- the byte moves ----

fn put(c: SaveCtx&, b: u8) { c.out.push(b); }

fn put_leb(c: SaveCtx&, v: u64) {
    var x = v;
    loop {
        let low = (x & 0x7F) as! u8;
        x = x >> 7;
        if x == 0 { c.out.push(low); return; }
        c.out.push(low | 0x80);
    }
}

fn put_le(c: SaveCtx&, v: u64, w: i64) {
    var x = v;
    for i in w {
        c.out.push((x & 0xFF) as! u8);
        x = x >> 8;
    }
}

fn put_view(c: SaveCtx&, off: i64, n: i64) {
    for i in n { c.out.push(c.data[off + i]); }
}

fn leb_size(v: u64) -> i64 {
    var n: i64 = 1;
    var x = v >> 7;
    while x != 0 { n += 1; x = x >> 7; }
    return n;
}

// insert_leb puts v's canonical LEB128 at `at`, moving what follows right.
fn insert_leb(c: SaveCtx&, at: i64, v: u64) {
    let n = leb_size(v);
    let end = c.out.len;
    for i in n { c.out.push(0); }
    var j = end - 1;
    while j >= at {
        c.out[j + n] = c.out[j];
        j -= 1;
    }
    var x = v;
    for i in n {
        let low = (x & 0x7F) as! u8;
        x = x >> 7;
        c.out[at + i] = if i + 1 < n { low | 0x80 } else { low };
    }
}

// frame puts the length of everything written since `at` in front of it.
fn frame(c: SaveCtx&, at: i64) { insert_leb(c, at, (c.out.len - at) as! u64); }

// ---- the id table (§3): first use appends, a repeat references ----

fn id_ref(c: SaveCtx&, id: u64) -> u64 {
    for x, i in c.ids { if x == id { return (i + 1) as! u64; } }
    c.ids.push(id);
    return c.ids.len as! u64;
}

fn cut(c: SaveCtx&, at: i64, mark: i64) {
    c.out.resize(at);
    c.ids.resize(mark);
}

// THE SAVE'S REFUSALS, each by the name every writer gives it
// (docs/SPEC-TABLES.md §2.5, §3.1, §5, §2.15).
let SaveRefuseOk: i64 = 0;
let SaveRefuseBlobShared: i64 = 1;
let SaveRefuseDataCycle: i64 = 2;
let SaveRefuseEnumNoVariant: i64 = 3;
let SaveRefuseDeprecatedVariant: i64 = 4;
let SaveRefuseUnionNoArm: i64 = 5;
let SaveRefuseUnknownRoot: i64 = 6;
let SaveRefuseNonFiniteFloat: i64 = 7;

// save_refusal_name is a refusal's name, "" for none.
fn save_refusal_name(code: i64) -> const u8[:] {
    return match code {
        SaveRefuseBlobShared => "blob_shared",
        SaveRefuseDataCycle => "data_cycle",
        SaveRefuseEnumNoVariant => "enum_no_variant",
        SaveRefuseDeprecatedVariant => "deprecated_variant",
        SaveRefuseUnionNoArm => "union_no_arm",
        SaveRefuseUnknownRoot => "unknown_root",
        SaveRefuseNonFiniteFloat => "non_finite_float",
        _ => "",
    };
}

fn refuse(c: SaveCtx&, code: i64) {
    if c.refusal == 0 { c.refusal = code; }
}

// ---- the values ----

// slot_of is field i of record `rec`: -1 for a record that has no slots, which
// holds every field at its default (docs/PORT-GOOSE.md, THE THREE STATES).
fn slot_of(c: SaveCtx&, rec: i64, i: i64) -> i64 {
    if rec < 0 || !c.vals[rec].made { return -1; }
    return c.vals[rec].first + i;
}

fn state_of(c: SaveCtx&, s: i64) -> u8 { if s < 0 { ValAbsent } else { c.vals[s].state } }

fn save_table_index(c: SaveCtx&, name: const u8[:]) -> i64 {
    for t, i in c.unit.tables { if t.name == name { return i; } }
    return -1;
}

fn save_union_index(c: SaveCtx&, name: const u8[:]) -> i64 {
    for u, i in c.unit.unions { if u.name == name { return i; } }
    return -1;
}

fn bool_of(c: SaveCtx&, s: i64, d: FieldDesc) -> bool {
    if state_of(c, s) == ValAbsent { return d.has_def && d.def_b; }
    return c.vals[s].bits.lo != 0;
}

fn f64_bits(x: f64) -> u64 {
    var a: f64[1] = [x];
    let b = bytes_of(a);
    var v: u64 = 0;
    for i in 8 {
        let byte: u64 = b[i];
        v |= byte << ((8 * i) as! u64);
    }
    return v;
}

// the default's own bits for a float field (FLOAT32: float32(v) for f32)
fn float_default_bits(d: FieldDesc) -> u64 {
    let v = if d.has_def { d.def_f } else { 0.0 };
    if d.is_f32 { return f32_bits(v as f32); }
    return f64_bits(v);
}

// float_special: a NaN or an infinity, which no form carries (§3.3)
fn float_special(bits: u64, single: bool) -> bool {
    if single { return (bits & 0x7F800000) == 0x7F800000; }
    return (bits & 0x7FF0000000000000) == 0x7FF0000000000000;
}

fn float_nan(bits: u64, single: bool) -> bool {
    if single { return (bits & 0x7F800000) == 0x7F800000 && (bits & 0x007FFFFF) != 0; }
    return (bits & 0x7FF0000000000000) == 0x7FF0000000000000 && (bits & 0x000FFFFFFFFFFFFF) != 0;
}

// float_is_default: a PLAIN float compares bit patterns (§3.3: -0 is not +0);
// a compressed one compares values, so -0 is +0 and NaN is never default.
fn float_is_default(bits: u64, d: FieldDesc) -> bool {
    let def = float_default_bits(d);
    if !d.frange { return bits == def; }
    if float_nan(bits, d.is_f32) || float_nan(def, d.is_f32) { return false; }
    let mag: u64 = if d.is_f32 { 0x7FFFFFFF } else { 0x7FFFFFFFFFFFFFFF };
    if (bits & mag) == 0 && (def & mag) == 0 { return true; }
    return bits == def;
}

fn view_equals(c: SaveCtx&, off: i64, n: i64, want: const u8[:]) -> bool {
    if n != want.len { return false; }
    for i in n { if c.data[off + i] != want[i] { return false; } }
    return true;
}

// is_default is the writer's elision test for a one-value field (§3,
// tablewire.cellIsDefault): a field holding its declared default is not
// written. An absent value IS its default.
fn is_default(c: SaveCtx&, s: i64, d: FieldDesc) -> bool {
    if state_of(c, s) == ValAbsent { return true; }
    let v = c.vals[s];
    if d.shape == ShapeBool { return (v.bits.lo != 0) == (d.has_def && d.def_b); }
    if d.shape == ShapeFloat { return float_is_default(v.bits.lo, d); }
    if d.shape == ShapeStr && d.wide { return v.len == 0; }
    if d.shape == ShapeStr || d.shape == ShapeText || d.shape == ShapeBytes {
        return view_equals(c, v.off, v.len, if d.has_def { d.def_s } else { "" });
    }
    if d.shape == ShapeEnum || d.shape == ShapeInt {
        let def = if d.has_def { d.def_i } else { Int128 { lo: 0, hi: 0 } };
        return v.bits.lo == def.lo && v.bits.hi == def.hi;
    }
    if d.shape == ShapePtr || d.shape == ShapeSBlob || d.shape == ShapeBBlob { return v.bits.lo == 0; }
    if d.shape == ShapeUnion { return v.bits.lo == 0; }
    return false;
}

// ---- guards (SPEC §4.5): a field under a false branch is not written ----

fn guard_holds(c: SaveCtx&, tab: i64, rec: i64, i: i64) -> bool {
    if c.unit.guards.len == 0 { return true; }
    let name = c.unit.tables[tab].name;
    for g in c.unit.guards {
        if g.ordinal != i || g.owner != name { continue; }
        let cond = c.unit.tables[tab].fields[g.cond];
        if bool_of(c, slot_of(c, rec, g.cond), cond) != g.want { return false; }
    }
    return true;
}

// ---- THE NUMBERING (§3.1; tablewire.Number, visitEdges) ----

// visit numbers one pointer edge's target: a node takes its index the first
// time it is reached. A blob reached twice is refused (§2.5); a node still
// open is a data cycle.
recursive fn visit(c: SaveCtx&, wire: i64) {
    if wire <= 0 || c.refusal != 0 { return; }
    if c.open[wire] { refuse(c, SaveRefuseDataCycle); return; }
    let blob = wire >= 2 && c.nodes[wire - 2].blob != 0;
    if c.index[wire] != 0 {
        if blob { refuse(c, SaveRefuseBlobShared); }
        return;
    }
    c.order.push(wire);
    c.index[wire] = c.order.len + 1;
    if blob { return; }
    c.open[wire] = true;
    let n = wire - 2;
    let rec = c.nodes[n].val;
    if rec >= 0 { edges(c, c.nodes[n].tab, rec); }
    c.open[wire] = false;
}

fn edge_of(c: SaveCtx&, s: i64) -> i64 {
    if s < 0 || c.vals[s].state != ValPresent { return 0; }
    return c.vals[s].bits.lo as! i64;
}

// live_of: the elements an array carries on the wire — every slot of a fixed
// or keyed one, the live count of a counted array or a list.
fn live_of(c: SaveCtx&, s: i64, d: FieldDesc) -> i64 {
    if s < 0 || !c.vals[s].made { return 0; }
    if d.array == ArrayCounted || d.array == ArrayList { return c.vals[s].len; }
    return c.vals[s].count;
}

// edges walks one record's by-value closure for pointer edges, in field
// order: a field the writer does not write is not an edge.
recursive fn edges(c: SaveCtx&, tab: i64, rec: i64) {
    for d, i in c.unit.tables[tab].fields {
        if c.refusal != 0 { return; }
        let s = slot_of(c, rec, i);
        if s < 0 { continue; }
        if !guard_holds(c, tab, rec, i) { continue; }
        if d.optional && c.vals[s].state == ValAbsent { continue; }
        edges_value(c, s, d);
    }
}

fn edges_value(c: SaveCtx&, s: i64, d: FieldDesc) {
    if d.shape == ShapePtr || d.shape == ShapeSBlob || d.shape == ShapeBBlob {
        visit(c, edge_of(c, s));
        return;
    }
    if d.shape == ShapeUnion {
        edges_arm(c, s, d.ref);
        return;
    }
    if d.shape == ShapeTab {
        if c.vals[s].made { edges(c, c.vals[s].tab, s); }
        return;
    }
    if d.shape == ShapeMap {
        if !c.vals[s].made { return; }
        for k in c.vals[s].len {
            let e = c.vals[s].first + k;
            if c.vals[e].made { edges(c, c.vals[e].tab, e); }
        }
        return;
    }
    if d.shape == ShapeArr {
        let e = d.elem;
        guard e else { return; }
        if e.shape != ShapePtr && e.shape != ShapeUnion && e.shape != ShapeTab { return; }
        let live = live_of(c, s, d);
        for k in live {
            let es = c.vals[s].first + k;
            if e.shape == ShapePtr { visit(c, edge_of(c, es)); }
            else if e.shape == ShapeUnion { edges_arm(c, es, e.ref); }
            else if c.vals[es].made { edges(c, c.vals[es].tab, es); }
        }
    }
}

// edges_arm: a union's SET arm is an edge (tablewire.visitArmEdges): a body
// arm is descended, a pointer or blob arm is visited, an array-of-pointers arm
// is visited per live element, and a nested union is one more arm.
recursive fn edges_arm(c: SaveCtx&, s: i64, name: const u8[:]) {
    let tag = c.vals[s].bits.lo as! i64;
    if c.vals[s].state != ValPresent || tag == 0 { return; }
    let u = save_union_index(c, name);
    if u < 0 || tag > c.unit.unions[u].arms.len { return; }
    let a = c.unit.unions[u].arms[tag - 1];
    let p = c.vals[s].first;
    if p < 0 { return; }
    if a.shape == ShapeTab {
        if c.vals[p].made { edges(c, c.vals[p].tab, p); }
        return;
    }
    if a.shape == ShapePtr || a.shape == ShapeSBlob || a.shape == ShapeBBlob {
        visit(c, edge_of(c, p));
        return;
    }
    if a.shape == ShapeArr {
        let e = a.elem;
        guard e else { return; }
        if e.shape != ShapePtr { return; }
        for k in live_of(c, p, a) { visit(c, edge_of(c, c.vals[p].first + k)); }
        return;
    }
    if a.shape == ShapeUnion { edges_arm(c, p, a.ref); }
}

// node_ref is a pointer slot's payload: the SAVE's index of the node the
// slot names, 0 for null (§3.1).
fn node_ref(c: SaveCtx&, s: i64) -> u64 {
    let wire = edge_of(c, s);
    if wire <= 0 { return 0; }
    if wire == 1 { return 1; }
    return c.index[wire] as! u64;
}

// ---- THE BODY (§3) ----

// body writes record `rec` of table `tab`: its fields in declaration order,
// the retained tail when retaining (§6.6), then — for the root only — the node
// table, then the zero reference that ends it. rec -1 is a record at its
// defaults.
recursive fn body(c: SaveCtx&, tab: i64, rec: i64, root: bool) {
    for d, i in c.unit.tables[tab].fields {
        if c.refusal != 0 { return; }
        if !guard_holds(c, tab, rec, i) { continue; }
        field(c, slot_of(c, rec, i), d, tab, i);
    }
    if c.retaining { retain_tail(c, rec); }
    if root && c.order.len > 0 { node_table(c); }
    put(c, 0);
}

// a header: the id's reference and the kind byte, the whole of it (§3)
fn header(c: SaveCtx&, id: u64, kind: u8) {
    put_leb(c, id_ref(c, id));
    put(c, kind);
}

fn field(c: SaveCtx&, s: i64, d: FieldDesc, owner: i64, ordinal: i64) {
    if d.shape == ShapePtr || d.shape == ShapeSBlob || d.shape == ShapeBBlob {
        // NULL IS ELIDED, and a non-null pointer always rides (§3.1, §2.5)
        let index = node_ref(c, s);
        if index == 0 { return; }
        header(c, d.id, KindPointer);
        put_leb(c, index);
        return;
    }
    if d.shape == ShapeMap { field_map(c, s, d, owner); return; }
    if d.optional {
        // PRESENCE IS THE PAYLOAD: a present optional always rides (§2.3)
        if state_of(c, s) == ValAbsent { return; }
        if d.shape == ShapeArr {
            let count = if d.array == ArrayCounted || d.array == ArrayList { c.vals[s].len } else { d.bound };
            field_array(c, s, d, count);
            return;
        }
        if d.shape == ShapeTab {
            header(c, d.id, KindTable);
            let start = c.out.len;
            body(c, save_table_index(c, d.ref), s, false);
            frame(c, start);
            return;
        }
        field_simple(c, s, d);
        return;
    }
    if d.shape == ShapeArr && d.array == ArrayKeyed { field_keyed(c, s, d, owner, ordinal); return; }
    if d.shape == ShapeStr && d.wide {
        // WIDE TEXT TAKES NO SPECIFIED DEFAULT: the empty value elides
        if state_of(c, s) == ValAbsent || c.vals[s].len == 0 { return; }
        header(c, d.id, KindWstring);
        put_leb(c, c.vals[s].len as! u64);
        put_view(c, c.vals[s].off, c.vals[s].len);
        return;
    }
    if d.shape == ShapeStr || d.shape == ShapeText {
        if is_default(c, s, d) { return; }
        header(c, d.id, KindString);
        put_leb(c, c.vals[s].len as! u64);
        put_view(c, c.vals[s].off, c.vals[s].len);
        return;
    }
    if d.shape == ShapeBytes {
        if is_default(c, s, d) { return; }
        header(c, d.id, KindArray);
        let n = c.vals[s].len as! u64;
        put_leb(c, 1 + (leb_size(n) as! u64) + n);
        put(c, KindU8); // bytes ride as an array of u8 (§2.5)
        put_leb(c, n);
        put_view(c, c.vals[s].off, c.vals[s].len);
        return;
    }
    if d.shape == ShapeArr {
        if d.array == ArrayCounted || d.array == ArrayList {
            // an unbounded array is the counted array's own bytes (§2.9); an
            // empty one elides
            let n = if s < 0 || !c.vals[s].made { 0 } else { c.vals[s].len };
            if n == 0 { return; }
            field_array(c, s, d, n);
            return;
        }
        let e = d.elem;
        guard e else { return; }
        if e.shape == ShapeTab {
            // a fixed array of tables always rides: position is identity
            field_array(c, s, d, d.bound);
            return;
        }
        // a fixed array is positional; an all-default one elides
        var all = true;
        if s >= 0 && c.vals[s].made {
            for k in d.bound {
                if !elem_default(c, c.vals[s].first + k, e) { all = false; break; }
            }
        }
        if all { return; }
        field_array(c, s, d, d.bound);
        return;
    }
    if d.shape == ShapeUnion {
        if state_of(c, s) != ValPresent || c.vals[s].bits.lo == 0 { return; } // None elides
        header(c, d.id, KindUnion);
        arm_header(c, s, d.ref);
        return;
    }
    if d.shape == ShapeTab {
        // AN ELIDED FIELD COSTS NOTHING IN THE ID TABLE EITHER: the body
        // decides, and an all-default one is cut back off with its ids
        let at = c.out.len;
        let mark = c.ids.len;
        header(c, d.id, KindTable);
        let start = c.out.len;
        body(c, save_table_index(c, d.ref), s, false);
        if c.out.len - start <= 1 {
            cut(c, at, mark);
            return;
        }
        frame(c, start);
        return;
    }
    if is_default(c, s, d) { return; }
    field_simple(c, s, d);
}

// elem_default: an element slot at its default (an element declares none).
fn elem_default(c: SaveCtx&, s: i64, e: const FieldDesc&) -> bool {
    if state_of(c, s) == ValAbsent { return true; }
    return is_default(c, s, e);
}

// field_simple: one value at its own kind's framing — a scalar, an enum
// reference, a string's L and bytes (§3).
fn field_simple(c: SaveCtx&, s: i64, d: FieldDesc) {
    if d.shape == ShapeEnum {
        header(c, d.id, KindEnum);
        put_enum(c, s, d);
        return;
    }
    if d.shape == ShapeUnion {
        header(c, d.id, KindUnion);
        arm_header(c, s, d.ref);
        return;
    }
    if d.shape == ShapeBytes {
        header(c, d.id, KindArray);
        let n = (if s < 0 { 0 } else { c.vals[s].len }) as! u64;
        put_leb(c, 1 + (leb_size(n) as! u64) + n);
        put(c, KindU8);
        put_leb(c, n);
        if s >= 0 { put_view(c, c.vals[s].off, c.vals[s].len); }
        return;
    }
    if d.shape == ShapeStr && d.wide {
        header(c, d.id, KindWstring);
        put_text(c, s, d);
        return;
    }
    let kind = d.kind;
    header(c, d.id, kind);
    put_value(c, s, d, kind);
}

// put_text: a text value's L, then its bytes (kind 12, kind 33).
fn put_text(c: SaveCtx&, s: i64, d: const FieldDesc&) {
    if state_of(c, s) == ValAbsent {
        let def = if d.has_def && !d.wide { d.def_s } else { "" };
        put_leb(c, def.len as! u64);
        for b in def { put(c, b); }
        return;
    }
    put_leb(c, c.vals[s].len as! u64);
    put_view(c, c.vals[s].off, c.vals[s].len);
}

// put_enum: the reference to the variant name's id, the ZERO REFERENCE for
// None (§3). A deprecated variant is refused by a strict write (§2.15).
fn put_enum(c: SaveCtx&, s: i64, d: const FieldDesc&) {
    var ordinal: u64 = 0;
    if state_of(c, s) == ValAbsent {
        if d.has_def { ordinal = d.def_i.lo; }
    } else {
        ordinal = c.vals[s].bits.lo;
    }
    if ordinal == 0 { put_leb(c, 0); return; }
    if ordinal > d.variants.len as! u64 {
        refuse(c, SaveRefuseEnumNoVariant);
        put_leb(c, 0);
        return;
    }
    let id = d.variants[(ordinal - 1) as! i64].id;
    for dv in c.unit.deprecated {
        if dv.id == id && dv.enum_name == d.ref { refuse(c, SaveRefuseDeprecatedVariant); }
    }
    put_leb(c, id_ref(c, id));
}

// scalar_bits is a one-value slot's effective bits: the value, or the
// declared default where nothing rode.
fn scalar_bits(c: SaveCtx&, s: i64, d: const FieldDesc&) -> Int128 {
    if state_of(c, s) != ValAbsent { return c.vals[s].bits; }
    if d.shape == ShapeBool { return Int128 { lo: if d.has_def && d.def_b { 1 } else { 0 }, hi: 0 }; }
    if d.shape == ShapeFloat { return Int128 { lo: float_default_bits(d), hi: 0 }; }
    if d.has_def { return d.def_i; }
    return Int128 { lo: 0, hi: 0 };
}

// put_value: one element at its kind's own framing (tablewire.encodeElement).
recursive fn put_value(c: SaveCtx&, s: i64, d: const FieldDesc&, kind: u8) {
    if d.shape == ShapeEnum { put_enum(c, s, d); return; }
    if kind == KindString || kind == KindWstring { put_text(c, s, d); return; }
    if kind == KindTable {
        let t = save_table_index(c, d.ref);
        let start = c.out.len;
        body(c, t, if s >= 0 && c.vals[s].made { s } else { -1 }, false);
        frame(c, start);
        return;
    }
    if kind == KindPointer { put_leb(c, node_ref(c, s)); return; }
    if kind == KindUnion {
        // an element of an ARRAY OF UNIONS: None rides as the one zero byte
        if state_of(c, s) != ValPresent || c.vals[s].bits.lo == 0 { put_leb(c, 0); return; }
        arm_header(c, s, d.ref);
        return;
    }
    let v = scalar_bits(c, s, d);
    if d.shape == ShapeFloat && float_special(v.lo, kind == KindF32) {
        // every writer refuses a NaN or an infinity (§3, §3.3)
        refuse(c, SaveRefuseNonFiniteFloat);
        return;
    }
    let w = kind_width(kind);
    if w == 16 {
        put_le(c, v.lo, 8);
        put_le(c, v.hi, 8);
        return;
    }
    put_le(c, v.lo, w);
}

// field_array: kind 14 — the element kind, the count, the elements (§3).
fn field_array(c: SaveCtx&, s: i64, d: FieldDesc, count: i64) {
    header(c, d.id, KindArray);
    let start = c.out.len;
    array_body(c, s, d, count);
    frame(c, start);
}

recursive fn array_body(c: SaveCtx&, s: i64, d: const FieldDesc&, count: i64) {
    let e = d.elem;
    guard e else { return; }
    let ek = if e.shape == ShapePtr { KindPointer } else { d.ek };
    put(c, ek);
    put_leb(c, count as! u64);
    for k in count {
        let es = if s >= 0 && c.vals[s].made && k < c.vals[s].count { c.vals[s].first + k } else { -1 };
        if ek == KindString && e.shape == ShapeTab {
            // a PLAIN TEXT LIST's element is kind 12: its record's one value
            let t = save_table_index(c, e.ref);
            let vs = slot_of(c, es, 0);
            put_text(c, vs, c.unit.tables[t].fields[0]);
            continue;
        }
        put_value(c, es, e, ek);
    }
}

// field_keyed: kind 16 (§3.2) — one (key reference, L, element) triple per
// slot not at its default, ascending by variant; none at all elides.
fn field_keyed(c: SaveCtx&, s: i64, d: FieldDesc, owner: i64, ordinal: i64) {
    let e = d.elem;
    guard e else { return; }
    let at = c.out.len;
    let mark = c.ids.len;
    header(c, d.id, KindKeyed);
    let start = c.out.len;
    put(c, d.ek);
    let pairs_at = c.out.len;
    var pairs: u64 = 0;
    var keys = d.variants;
    let name = c.unit.tables[owner].name;
    for k in c.unit.keys {
        if k.ordinal == ordinal && k.owner == name { keys = k.variants; }
    }
    if s >= 0 && c.vals[s].made {
        for i in d.bound {
            if i >= keys.len { break; }
            let es = c.vals[s].first + i;
            let slot_at = c.out.len;
            let slot_mark = c.ids.len;
            put_leb(c, id_ref(c, keys[i].id));
            let el = c.out.len;
            if e.shape == ShapeTab {
                body(c, save_table_index(c, e.ref), if c.vals[es].made { es } else { -1 }, false);
                if c.out.len - el <= 1 { cut(c, slot_at, slot_mark); continue; }
            } else {
                if elem_default(c, es, e) { cut(c, slot_at, slot_mark); continue; }
                put_value(c, es, e, d.ek);
            }
            frame(c, el);
            pairs += 1;
        }
    }
    if pairs == 0 { cut(c, at, mark); return; }
    insert_leb(c, pairs_at, pairs);
    frame(c, start);
}

// field_map: kind 14 over element kind 13, the entries in ASCENDING KEY
// ORDER with no key twice (§2.8); an empty map elides, and an entry always
// rides.
fn field_map(c: SaveCtx&, s: i64, d: FieldDesc, owner: i64) {
    if s < 0 || !c.vals[s].made || c.vals[s].len == 0 { return; }
    // the entry table is the record the load placed (<Owner><Slot>Entry)
    let t = c.vals[c.vals[s].first].tab;
    header(c, d.id, KindArray);
    let start = c.out.len;
    put(c, KindTable);
    put_leb(c, c.vals[s].len as! u64);
    for k in c.vals[s].len {
        let e = c.vals[s].first + k;
        let el = c.out.len;
        body(c, t, e, false);
        frame(c, el);
    }
    frame(c, start);
}

// arm_header: a SET arm whole — the arm id's reference, the arm's KIND, L,
// then L bytes of payload (§3, tablewire.encodeArmHeader).
recursive fn arm_header(c: SaveCtx&, s: i64, name: const u8[:]) {
    let u = save_union_index(c, name);
    let tag = c.vals[s].bits.lo as! i64;
    if u < 0 || tag < 1 || tag > c.unit.unions[u].arms.len { refuse(c, SaveRefuseUnionNoArm); return; }
    let a = c.unit.unions[u].arms[tag - 1];
    put_leb(c, id_ref(c, a.id));
    put(c, arm_kind(a));
    let start = c.out.len;
    let p = c.vals[s].first;
    arm_payload(c, if c.vals[s].made { p } else { -1 }, a);
    frame(c, start);
}

// arm_payload: ONE ARM's payload, exactly what a FIELD of that type puts after
// its own framing prefix (§2.6, tablewire.encodeArm).
fn arm_payload(c: SaveCtx&, p: i64, a: FieldDesc) {
    if a.shape == ShapeVoid { return; }
    if a.shape == ShapeTab {
        body(c, save_table_index(c, a.ref), if p >= 0 && c.vals[p].made { p } else { -1 }, false);
        return;
    }
    if a.shape == ShapePtr || a.shape == ShapeSBlob || a.shape == ShapeBBlob { put_leb(c, node_ref(c, p)); return; }
    if a.shape == ShapeStr || a.shape == ShapeText {
        if state_of(c, p) == ValAbsent {
            let def = if a.has_def && !a.wide { a.def_s } else { "" };
            for b in def { put(c, b); }
            return;
        }
        put_view(c, c.vals[p].off, c.vals[p].len);
        return;
    }
    if a.shape == ShapeBytes {
        put(c, KindU8);
        let n = if state_of(c, p) == ValAbsent { 0 } else { c.vals[p].len };
        put_leb(c, n as! u64);
        if n > 0 { put_view(c, c.vals[p].off, n); }
        return;
    }
    if a.shape == ShapeArr {
        let count = if a.array == ArrayCounted || a.array == ArrayList { if p >= 0 && c.vals[p].made { c.vals[p].len } else { 0 } } else { a.bound };
        array_body(c, p, a, count);
        return;
    }
    if a.shape == ShapeUnion {
        if state_of(c, p) != ValPresent || c.vals[p].bits.lo == 0 { put_leb(c, 0); return; }
        arm_header(c, p, a.ref);
        return;
    }
    if a.shape == ShapeEnum { put_enum(c, p, a); return; }
    put_value(c, p, a, a.kind);
}

// ---- the node table (§3.1): the last field of the root body ----

fn node_table(c: SaveCtx&) {
    header(c, NodeWireId, KindString);
    let start = c.out.len;
    put_leb(c, c.order.len as! u64);
    for k in c.order.len {
        let wire = c.order[k];
        let n = c.nodes[wire - 2];
        if n.blob != 0 {
            // a BYTE BUFFER's record is the bytes under a reserved type id
            put_leb(c, id_ref(c, if n.blob == KindString { StringTypeId } else { BytesTypeId }));
            put_leb(c, n.len as! u64);
            put_view(c, n.off, n.len);
            continue;
        }
        put_leb(c, id_ref(c, c.unit.tables[n.tab].id));
        let el = c.out.len;
        body(c, n.tab, n.val, false);
        frame(c, el);
    }
    frame(c, start);
}

// ---- THE SAVE ----

// save_into is the one save both entry points share.
fn save_into(c: SaveCtx&, root_val: i64) -> i64 {
    let t = c.root_tab;
    for i in c.nodes.len + 2 {
        c.index.push(0);
        c.open.push(false);
    }
    // THE NUMBERING FIRST: the root is index 1 and open while it descends
    c.open[1] = true;
    c.index[1] = 1;
    edges(c, t, root_val);
    c.open[1] = false;
    if c.refusal != 0 { return c.refusal; }
    put(c, 1); // THE FORM BYTE (§3)
    body(c, t, root_val, true);
    if c.refusal != 0 { return c.refusal; }
    // THE ID TABLE IS THE LAST THING IN THE FILE: the entries, then the count
    for id in c.ids { put_le(c, id, 8); }
    put_le(c, c.ids.len as! u64, 8);
    return 0;
}

// save_table is THE SAVE: the flat value table `vals` (with `nodes`, and the
// input `data` its views read) of root record `root_val`, a `root`, written
// as a form-1 file appended to `out`. It answers 0 or the refusal every
// writer gives (SaveRefuseBlobShared, SaveRefuseDataCycle, ...;
// save_refusal_name spells it), in which case `out` holds no file.
fn save_table(unit: LoadUnit, root: const u8[:], data: const u8[:], vals: LoadVal[>..]&, nodes: LoadNode[>..]&, root_val: i64, out: u8[>..<]&) -> i64 {
    var ids: u64[>..<] = [];
    var order: i64[>..<] = [];
    var index: i64[>..<] = [];
    var open: bool[>..<] = [];
    var rbytes: u8[>..<] = [];
    var rrecs: RetainRec[>..<] = [];
    var rids: u64[>..<] = [];
    var none = RetainStore { capacity: 0, id_capacity: 0, used: 0, bytes: rbytes, recs: rrecs, ids: rids };
    let at = out.len;
    var c = SaveCtx { data: data, unit: unit, vals: vals, nodes: nodes, out: out, ids: ids, order: order, index: index, open: open, root_tab: -1, refusal: 0, retain: none, retaining: false, retain_lost: 0 };
    for d, i in unit.tables { if d.name == root { c.root_tab = i; } }
    if c.root_tab < 0 { return SaveRefuseUnknownRoot; }
    let refusal = save_into(c, root_val);
    if refusal != 0 { out.resize(at); }
    return refusal;
}
