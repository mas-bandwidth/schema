// cook_write.goose — THE COOK'S REGION WRITER (docs/SPEC-TABLES.md §7.6, §7.2,
// §3.1, §2.8, §2.9, §2.13.1), the write side of every root the typed writer
// does not carry: a pointered root (a variable closure: pointers, lists, maps,
// blobs, unbounded text), and a fixed root whose closure holds a kind the
// typed writer has no spelling for. It writes from THE FLAT VALUE TABLE the
// table wire's load builds (wire_load.goose, THE LOADED VALUE), which is the
// Goose value of a variable table, so a wire a load landed cooks without a
// copy. It is the tool's cooker (internal/tablecook: Cook, Layout,
// Region.Write and the extent carver) step for step:
//
//   NUMBER the graph by the depth-first first-visit walk of §3.1 from the
//   root over pointer edges, through every by-value edge — the wire's own
//   numbering, except that a blob two slots name is ONE node written once
//   (tablewire.NumberPacking) — and refuse a data cycle;
//   LAY the nodes out in index order, the root at zero, each at its own
//   type's alignment: a record's storage (rounded to where its extent begins
//   when it can hold one) and then its EXTENT, the bytes its lists, maps and
//   unbounded text take behind it; a blob's eight-byte header and its bytes;
//   WRITE each record piece by piece at the offsets the compiler's layout
//   gives (the generated CookRegionLayout, its tables and unions parallel to
//   the unit's TableUnit), every reference as the SELF-RELATIVE delta of
//   §6.3, every scalar in the order the cook is FOR; then the node directory
//   beside the data and the header in front of it.
//
// EVERY BYTE NO FIELD COVERS IS ZERO (§7.2): the extent is zeroed once by
// cook_begin. A REFUSAL WRITES NOTHING: the whole write runs once dry (an
// empty view, which every put ignores), then for real.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_tables;
import .block_form;
import .cook_form;
import .wire_load;

// CookCol is one field's (or one union arm's) cooked storage, relative to its
// record's (or the arm's) base: the facts ir.FieldPieces, ir.ListElementLayout
// and the record layouts give, as data. An array's element is described by the
// same column: its scalar width, the record or union it names.
struct CookCol {
    off: i64,         // the value piece: the scalar, the buffer, the first slot, the reference
    size: i64,        // the value piece's bytes (a text buffer's)
    len_off: i64,     // a text's used length or a counted array's count, -1 for none
    present_off: i64, // an optional's presence, -1 for none
    stride: i64,      // one array slot, one list element or one map entry
    ealign: i64,      // a list element's or a map entry's alignment
    w: i64,           // a scalar's storage width in bytes (16 for a 128-bit lane pair)
    tab: i64,         // the record a value, element or map entry is, as a TableUnit table index
    uni: i64,         // the union a value or element is, as a TableUnit union index
    ext: bool,        // the field's by-value storage can hold an extent
    eext: bool,       // a list element or a map entry can hold one
}

// CookTab is one record of TableUnit.tables as a node: its storage (rounded to
// where its extent begins when it can hold one), its node alignment, whether
// it can hold an extent, and one column per field in descriptor order.
struct CookTab { record: i64, align: i64, ext: bool, cols: const CookCol[:] }

// CookUni is one union of TableUnit.unions: the tag's width, the arms'
// shared offset, and one column per arm in tag order, relative to that offset.
struct CookUni { tag: i64, arm: i64, cols: const CookCol[:] }

// CookUnitLayout is the unit's cook layout as data: the tables and unions
// parallel to TableUnit's, and the alignment an unbounded text's blob record
// rounds to (ir.TableRegionAlign).
struct CookUnitLayout { tabs: const CookTab[:], unis: const CookUni[:], text_align: i64 }

// CookCtx is one cook's context of borrowed tables (the Goose spec's
// struct-of-references pattern). The numbering is by WIRE node index: 1 is
// the root, i + 2 the load's node i; `index` holds a node's place in the
// numbering plus one, `order` the wire index at each place.
struct CookCtx {
    unit: LoadUnit,
    lay: CookUnitLayout,
    data: const u8[:],
    vals: const LoadVal[:],
    nodes: const LoadNode[:],
    root_tab: i64,
    root: i64,
    region: u8[:],      // the data part being written; empty while measuring or dry
    big: bool,
    index: i64[>..<]&,
    order: i64[>..<]&,
    open: bool[>..<]&,
    offs: i64[>..<]&,   // each place's offset in the region
    exts: i64[>..<]&,   // each place's extent, as the layout measured it
    refused: bool,
    writing: bool,      // the carve writes the elements it places
    ext_base: i64,      // where the node being carved keeps its extent
    at: i64,            // the carve's running offset inside that extent
}

fn cook_align_up(v: i64, a: i64) -> i64 {
    if a <= 1 { return v; }
    return (v + a - 1) / a * a;
}

fn cook_refuse(c: CookCtx&) -> bool {
    c.refused = true;
    return false;
}

// ---- the loaded value's reads: a slot -1 or ValAbsent is the declared default ----

fn cook_slot(c: CookCtx&, rec: i64, i: i64) -> i64 {
    if rec < 0 || !c.vals[rec].made || i >= c.vals[rec].count { return -1; }
    return c.vals[rec].first + i;
}

fn cook_rode(c: CookCtx&, s: i64) -> bool { return s >= 0 && c.vals[s].state != ValAbsent; }

fn cook_record_of(c: CookCtx&, s: i64) -> i64 {
    if !cook_rode(c, s) || !c.vals[s].made { return -1; }
    return s;
}

fn cook_elem(c: CookCtx&, s: i64, k: i64) -> i64 {
    if !cook_rode(c, s) || !c.vals[s].made || k >= c.vals[s].count { return -1; }
    return c.vals[s].first + k;
}

// cook_live is a collection's live count: a counted array's (its declared
// least count where it did not ride, §4.6), a list's or a map's.
fn cook_live(c: CookCtx&, s: i64, d: const FieldDesc&) -> i64 {
    if !cook_rode(c, s) || !c.vals[s].made {
        if d.array == ArrayCounted { return d.min; }
        return 0;
    }
    var n = c.vals[s].len;
    if n < 0 { n = 0; }
    if d.array == ArrayCounted && n > d.bound { n = d.bound; }
    return n;
}

fn cook_edge(c: CookCtx&, s: i64) -> i64 {
    if s < 0 || c.vals[s].state != ValPresent { return 0; }
    return c.vals[s].bits.lo as! i64;
}

fn cook_bool_of(c: CookCtx&, s: i64, d: const FieldDesc&) -> bool {
    if !cook_rode(c, s) { return d.has_def && d.def_b; }
    return c.vals[s].bits.lo != 0;
}

fn cook_guard_holds(c: CookCtx&, tab: i64, rec: i64, i: i64) -> bool {
    if c.unit.guards.len == 0 { return true; }
    let name = c.unit.tables[tab].name;
    for g in c.unit.guards {
        if g.ordinal != i || g.owner != name { continue; }
        if cook_bool_of(c, cook_slot(c, rec, g.cond), c.unit.tables[tab].fields[g.cond]) != g.want { return false; }
    }
    return true;
}

// ---- THE NUMBERING (§3.1; tablewire.NumberPacking) ----

// cook_visit numbers one pointer edge's target the first time it is reached;
// a node still open is a data cycle. A blob reached twice is one node.
recursive fn cook_visit(c: CookCtx&, wire: i64) {
    if wire <= 0 || c.refused { return; }
    if c.open[wire] { c.refused = true; return; }
    if c.index[wire] != 0 { return; }
    c.order.push(wire);
    c.index[wire] = c.order.len;
    if wire == 1 || c.nodes[wire - 2].blob != 0 { return; }
    let rec = c.nodes[wire - 2].val;
    if rec < 0 { return; }
    c.open[wire] = true;
    cook_edges(c, c.nodes[wire - 2].tab, rec);
    c.open[wire] = false;
}

// cook_edges walks one record's by-value closure for pointer edges, in field
// order: a field under a false guard or an absent optional is not an edge.
recursive fn cook_edges(c: CookCtx&, tab: i64, rec: i64) {
    for d, i in c.unit.tables[tab].fields {
        if c.refused { return; }
        let s = cook_slot(c, rec, i);
        if s < 0 { continue; }
        if !cook_guard_holds(c, tab, rec, i) { continue; }
        if d.optional && c.vals[s].state == ValAbsent { continue; }
        cook_edges_value(c, s, d);
    }
}

recursive fn cook_edges_value(c: CookCtx&, s: i64, d: const FieldDesc&) {
    if d.shape == ShapePtr || d.shape == ShapeSBlob || d.shape == ShapeBBlob {
        cook_visit(c, cook_edge(c, s));
        return;
    }
    if d.shape == ShapeUnion { cook_edges_arm(c, s, d.ref); return; }
    if d.shape == ShapeTab {
        if c.vals[s].made { cook_edges(c, c.vals[s].tab, s); }
        return;
    }
    if !c.vals[s].made { return; }
    if d.shape == ShapeMap {
        for k in c.vals[s].len {
            let e = c.vals[s].first + k;
            if c.vals[e].made { cook_edges(c, c.vals[e].tab, e); }
        }
        return;
    }
    if d.shape == ShapeArr {
        let e = d.elem;
        guard e else { return; }
        if e.shape != ShapePtr && e.shape != ShapeUnion && e.shape != ShapeTab { return; }
        var live = c.vals[s].count;
        if d.array == ArrayCounted || d.array == ArrayList { live = c.vals[s].len; }
        for k in live {
            let es = c.vals[s].first + k;
            if e.shape == ShapePtr { cook_visit(c, cook_edge(c, es)); }
            else if e.shape == ShapeUnion { cook_edges_arm(c, es, e.ref); }
            else if c.vals[es].made { cook_edges(c, c.vals[es].tab, es); }
        }
    }
}

// cook_edges_arm: a union's SET arm is an edge (tablewire.visitArmEdges): a
// body arm is descended, a pointer or blob arm is visited, an
// array-of-pointers arm is visited per live element, and a nested union is
// one more arm.
recursive fn cook_edges_arm(c: CookCtx&, s: i64, name: const u8[:]) {
    let tag = c.vals[s].bits.lo as! i64;
    if c.vals[s].state != ValPresent || tag == 0 { return; }
    var u: i64 = -1;
    for x, i in c.unit.unions { if x.name == name { u = i; break; } }
    if u < 0 || tag > c.unit.unions[u].arms.len { return; }
    let a = c.unit.unions[u].arms[tag - 1];
    let p = c.vals[s].first;
    if p < 0 { return; }
    if a.shape == ShapeTab {
        if c.vals[p].made { cook_edges(c, c.vals[p].tab, p); }
        return;
    }
    if a.shape == ShapePtr || a.shape == ShapeSBlob || a.shape == ShapeBBlob {
        cook_visit(c, cook_edge(c, p));
        return;
    }
    if a.shape == ShapeArr {
        let e = a.elem;
        guard e else { return; }
        if e.shape != ShapePtr || !c.vals[p].made { return; }
        var live = c.vals[p].count;
        if a.array == ArrayCounted || a.array == ArrayList { live = c.vals[p].len; }
        for k in live { cook_visit(c, cook_edge(c, c.vals[p].first + k)); }
        return;
    }
    if a.shape == ShapeUnion { cook_edges_arm(c, p, a.ref); }
}

// ---- THE STORES, through the order the cook is FOR ----

fn cook_put_at(c: CookCtx&, at: i64, v: u64, w: i64) { cook_put(c.region, at, v, w, c.big); }

// cook_wide stores a 128-bit lane pair: a big-endian region holds the value
// with its high half first, as it holds a u64 with its high byte first.
fn cook_wide(c: CookCtx&, at: i64, v: Int128) {
    if c.big {
        cook_put_at(c, at, v.hi, 8);
        cook_put_at(c, at + 8, v.lo, 8);
    } else {
        cook_put_at(c, at, v.lo, 8);
        cook_put_at(c, at + 8, v.hi, 8);
    }
}

// cook_ref writes a reference slot: the self-relative delta from the slot to
// the node, zero for null. A node the numbering did not carry is refused.
fn cook_ref(c: CookCtx&, at: i64, wire: i64) -> bool {
    if wire <= 0 { cook_put_at(c, at, 0, 8); return true; }
    if wire >= c.index.len || c.index[wire] == 0 { return cook_refuse(c); }
    let delta = c.offs[c.index[wire] - 1] - at;
    if delta == 0 { return cook_refuse(c); }
    cook_put_at(c, at, delta as! u64, 8);
    return true;
}

// cook_text writes a text or `bytes` buffer piece and its used length: the
// used bytes (a wstring's code units, each in the cook's order), then the zero
// tail the extent already holds.
fn cook_text(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&, at: i64) -> bool {
    if cook_rode(c, s) { return cook_text_put(c, c.data[c.vals[s].off..c.vals[s].off + c.vals[s].len], d.wide, col, at); }
    if !d.has_def { return cook_text_put(c, c.data[0..0], d.wide, col, at); }
    if d.wide && d.def_s.len > 0 { return cook_refuse(c); }
    return cook_text_put(c, d.def_s, d.wide, col, at);
}

fn cook_text_put(c: CookCtx&, v: const u8[:], wide: bool, col: const CookCol&, at: i64) -> bool {
    let n = v.len;
    if n > col.size { return cook_refuse(c); }
    if wide {
        var k: i64 = 0;
        while k < n / 2 {
            let lo: u64 = v[2 * k];
            let hi: u64 = v[2 * k + 1];
            cook_put_at(c, at + col.off + 2 * k, lo | (hi << 8), 2);
            k += 1;
        }
        cook_put_at(c, at + col.len_off, (n / 2) as! u64, 4);
        return true;
    }
    cook_put_bytes(c.region, at + col.off, v);
    cook_put_at(c, at + col.len_off, n as! u64, 4);
    return true;
}

// ---- THE RECORD WRITER (tablecook regionWriter) ----

// cook_record writes record `rec` of table `tab` at `at`: every field, piece
// by piece. A list's, a map's and an unbounded text's slot is the carve's.
recursive fn cook_record(c: CookCtx&, tab: i64, rec: i64, at: i64) -> bool {
    let cols = c.lay.tabs[tab].cols;
    for d, i in c.unit.tables[tab].fields {
        if !cook_field(c, cook_slot(c, rec, i), d, cols[i], at) { return false; }
    }
    return true;
}

// cook_field writes one field of the record at `at`: its presence where it is
// optional, then its storage.
recursive fn cook_field(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&, at: i64) -> bool {
    if d.optional { cook_put_at(c, at + col.present_off, if cook_rode(c, s) { 1 } else { 0 }, 1); }
    if d.shape == ShapeMap || d.shape == ShapeText { return true; }
    if d.shape == ShapeStr || d.shape == ShapeBytes { return cook_text(c, s, d, col, at); }
    if d.shape != ShapeArr { return cook_value(c, s, d, col, at + col.off); }
    if d.array == ArrayList { return true; }
    let e = d.elem;
    guard e else { return cook_refuse(c); }
    // every slot is written: the storage is allocate-max, and a slot past the
    // live count holds the value-initialized element
    for k in d.bound {
        if !cook_value(c, cook_elem(c, s, k), e, col, at + col.off + k * col.stride) { return false; }
    }
    if d.array == ArrayCounted { cook_put_at(c, at + col.len_off, cook_live(c, s, d) as! u64, 4); }
    return true;
}

// cook_value writes ONE value of a field's or an element's type at `at`.
recursive fn cook_value(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&, at: i64) -> bool {
    let rode = cook_rode(c, s);
    if d.shape == ShapePtr || d.shape == ShapeSBlob || d.shape == ShapeBBlob {
        return cook_ref(c, at, cook_edge(c, s));
    }
    if d.shape == ShapeBool {
        cook_put_at(c, at, if cook_bool_of(c, s, d) { 1 } else { 0 }, 1);
        return true;
    }
    if d.shape == ShapeFloat {
        var bits: u64 = 0;
        if rode { bits = c.vals[s].bits.lo; }
        else if d.has_def {
            bits = if d.is_f32 { accel_f32_bits(d.def_f as f32) } else { accel_f64_bits(d.def_f) };
        }
        // A COOKED FLOAT IS FINITE (§3.3): NaN and infinity refuse
        if d.is_f32 && (bits & 0x7F800000) == 0x7F800000 { return cook_refuse(c); }
        if !d.is_f32 && (bits & 0x7FF0000000000000) == 0x7FF0000000000000 { return cook_refuse(c); }
        cook_put_at(c, at, bits, col.w);
        return true;
    }
    if d.shape == ShapeInt || d.shape == ShapeEnum {
        var v = Int128 { lo: 0, hi: 0 };
        if rode { v = c.vals[s].bits; } else if d.has_def { v = d.def_i; }
        if col.w == 16 { cook_wide(c, at, v); } else { cook_put_at(c, at, v.lo, col.w); }
        return true;
    }
    if d.shape == ShapeTab { return cook_record(c, col.tab, cook_record_of(c, s), at); }
    if d.shape == ShapeUnion { return cook_union(c, s, col.uni, at); }
    return cook_refuse(c);
}

// cook_union writes the tag, then the SET arm at the arms' offset; every byte
// outside it stays zero.
recursive fn cook_union(c: CookCtx&, s: i64, u: i64, at: i64) -> bool {
    let un = c.lay.unis[u];
    var tag: i64 = 0;
    if cook_rode(c, s) { tag = c.vals[s].bits.lo as! i64; }
    if tag < 0 || tag > c.unit.unions[u].arms.len { return cook_refuse(c); }
    cook_put_at(c, at, tag as! u64, un.tag);
    if tag == 0 { return true; }
    let a = c.unit.unions[u].arms[tag - 1];
    if a.shape == ShapeVoid { return true; }
    var p: i64 = -1;
    if c.vals[s].made { p = c.vals[s].first; }
    return cook_field(c, p, a, un.cols[tag - 1], at + un.arm);
}

// ---- THE EXTENT CARVE (tablecook carver): one walk, measuring or writing ----

// cook_carve_record walks one record's fields in declaration order for the
// arrays its extent holds. A field the walk does not reach — under a false
// guard, or an absent optional — must hold no extent.
recursive fn cook_carve_record(c: CookCtx&, tab: i64, rec: i64, at: i64) -> bool {
    let cols = c.lay.tabs[tab].cols;
    for d, i in c.unit.tables[tab].fields {
        let s = cook_slot(c, rec, i);
        var live = cook_guard_holds(c, tab, rec, i);
        if d.optional && !cook_rode(c, s) { live = false; }
        if !live {
            if !cook_unreached(c, s, d, cols[i]) { return false; }
            continue;
        }
        if !cook_carve_field(c, s, d, cols[i], at) { return false; }
    }
    return true;
}

// cook_unreached measures a value the walk does not reach, alone: a cook
// cannot carry storage the walk does not reach (§2.3, §7.6).
recursive fn cook_unreached(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&) -> bool {
    let at = c.at;
    let writing = c.writing;
    c.at = 0;
    c.writing = false;
    let ok = cook_carve_field(c, s, d, col, 0);
    let held = c.at;
    c.at = at;
    c.writing = writing;
    if !ok { return false; }
    if held != 0 { return cook_refuse(c); }
    return true;
}

recursive fn cook_unreached_elem(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&) -> bool {
    let at = c.at;
    let writing = c.writing;
    c.at = 0;
    c.writing = false;
    let ok = cook_carve_value(c, s, d, col, 0);
    let held = c.at;
    c.at = at;
    c.writing = writing;
    if !ok { return false; }
    if held != 0 { return cook_refuse(c); }
    return true;
}

recursive fn cook_carve_field(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&, at: i64) -> bool {
    if d.shape == ShapeMap { return cook_carve_map(c, s, col, at + col.off); }
    if d.shape == ShapeText { return cook_carve_text(c, s, d, at + col.off); }
    if d.shape == ShapeArr && d.array == ArrayList { return cook_carve_list(c, s, d, col, at + col.off); }
    if !col.ext { return true; }
    if d.shape != ShapeArr { return cook_carve_value(c, s, d, col, at + col.off); }
    let e = d.elem;
    guard e else { return cook_refuse(c); }
    var live = d.bound;
    if d.array == ArrayCounted { live = cook_live(c, s, d); }
    for k in d.bound {
        let es = cook_elem(c, s, k);
        if k < live {
            if !cook_carve_value(c, es, e, col, at + col.off + k * col.stride) { return false; }
        } else if !cook_unreached_elem(c, es, e, col) {
            // a slot past the live count is storage the walk does not reach
            return false;
        }
    }
    return true;
}

// cook_carve_value descends ONE by-value value: a nested record, or a union's
// set arm.
recursive fn cook_carve_value(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&, at: i64) -> bool {
    if d.shape == ShapeTab { return cook_carve_record(c, col.tab, cook_record_of(c, s), at); }
    if d.shape != ShapeUnion || !cook_rode(c, s) { return true; }
    let u = col.uni;
    let tag = c.vals[s].bits.lo as! i64;
    if tag <= 0 || tag > c.unit.unions[u].arms.len { return true; }
    let a = c.unit.unions[u].arms[tag - 1];
    if a.shape == ShapeVoid { return true; }
    var p: i64 = -1;
    if c.vals[s].made { p = c.vals[s].first; }
    return cook_carve_field(c, p, a, c.lay.unis[u].cols[tag - 1], at + c.lay.unis[u].arm);
}

// cook_carve_list carves ONE unbounded array (§2.9): the whole array at the
// element's alignment, the slot's delta and count, then element by element
// in index order the arrays each element holds by value.
recursive fn cook_carve_list(c: CookCtx&, s: i64, d: const FieldDesc&, col: const CookCol&, slot: i64) -> bool {
    let e = d.elem;
    guard e else { return cook_refuse(c); }
    let count = cook_live(c, s, d);
    c.at = cook_align_up(c.at, col.ealign);
    let start = c.at;
    c.at += count * col.stride;
    let array = c.ext_base + start;
    if c.writing {
        cook_put_at(c, slot, if count > 0 { (array - slot) as! u64 } else { 0 }, 8);
        cook_put_at(c, slot + 8, count as! u64, 4);
        for k in count {
            if !cook_value(c, cook_elem(c, s, k), e, col, array + k * col.stride) { return false; }
        }
    }
    if !col.eext { return true; }
    for k in count {
        if !cook_carve_value(c, cook_elem(c, s, k), e, col, array + k * col.stride) { return false; }
    }
    return true;
}

// cook_carve_map carves ONE map (§2.8): the entry array in ascending key
// order, the slot's delta and count, then entry by entry the arrays each
// entry holds by value.
recursive fn cook_carve_map(c: CookCtx&, s: i64, col: const CookCol&, slot: i64) -> bool {
    var count: i64 = 0;
    if cook_rode(c, s) && c.vals[s].made { count = c.vals[s].len; }
    c.at = cook_align_up(c.at, col.ealign);
    let start = c.at;
    c.at += count * col.stride;
    let array = c.ext_base + start;
    if c.writing {
        cook_put_at(c, slot, if count > 0 { (array - slot) as! u64 } else { 0 }, 8);
        cook_put_at(c, slot + 8, count as! u64, 4);
        for k in count {
            if !cook_record(c, col.tab, c.vals[s].first + k, array + k * col.stride) { return false; }
        }
    }
    if !col.eext { return true; }
    for k in count {
        if !cook_carve_record(c, col.tab, c.vals[s].first + k, array + k * col.stride) { return false; }
    }
    return true;
}

// cook_carve_text carves ONE unbounded text (§2.13.1): a blob record in the
// holder's extent at eight, its length, its bytes and a terminator, rounded
// to the region's alignment; an empty or absent text is the null reference.
fn cook_carve_text(c: CookCtx&, s: i64, d: const FieldDesc&, slot: i64) -> bool {
    if cook_rode(c, s) { return cook_carve_blob(c, c.data[c.vals[s].off..c.vals[s].off + c.vals[s].len], slot); }
    if d.has_def && !d.optional { return cook_carve_blob(c, d.def_s, slot); }
    return cook_carve_blob(c, c.data[0..0], slot);
}

fn cook_carve_blob(c: CookCtx&, v: const u8[:], slot: i64) -> bool {
    let n = v.len;
    if n == 0 {
        if c.writing { cook_put_at(c, slot, 0, 8); }
        return true;
    }
    if n > 0xFFFFFFFF { return cook_refuse(c); }
    c.at = cook_align_up(c.at, 8);
    let start = c.at;
    c.at += cook_align_up(8 + n + 1, c.lay.text_align);
    if c.writing {
        let blob = c.ext_base + start;
        cook_put_at(c, slot, (blob - slot) as! u64, 8);
        cook_put_at(c, blob, n as! u64, 4);
        cook_put_bytes(c.region, blob + 8, v);
    }
    return true;
}

// ---- THE LAYOUT AND THE WRITE (tablecook Layout, Region.Write, Cook) ----

// cook_node is a place's record: its table and its loaded record, -1 for a blob.
fn cook_node(c: CookCtx&, wire: i64) -> i64, i64 {
    if wire == 1 { return c.root_tab, c.root; }
    let n = c.nodes[wire - 2];
    if n.blob != 0 { return -1, -1; }
    return n.tab, n.val;
}

// cook_node_id is a place's directory type id (§3.1, §6.3).
fn cook_node_id(c: CookCtx&, wire: i64) -> u64 {
    if wire == 1 { return c.unit.tables[c.root_tab].id; }
    let n = c.nodes[wire - 2];
    if n.blob == KindString { return StringTypeId; }
    if n.blob != 0 { return BytesTypeId; }
    return c.unit.tables[n.tab].id;
}

// cook_layout numbers the graph and lays it out: every place's offset and
// extent, and the data part's length and alignment.
fn cook_layout(c: CookCtx&) -> i64, i64 {
    for i in c.nodes.len + 2 {
        c.index.push(0);
        c.open.push(false);
    }
    c.order.push(1);
    c.index[1] = 1;
    c.open[1] = true;
    cook_edges(c, c.root_tab, c.root);
    c.open[1] = false;
    if c.refused { return -1, 0; }
    var offset: i64 = 0;
    var align: i64 = 8;
    for wire in c.order {
        let tab, rec = cook_node(c, wire);
        if tab < 0 {
            // a BYTE BUFFER's node: the header and its bytes, at eight (§7.2)
            let n = c.nodes[wire - 2];
            if n.len > 0xFFFFFFFF { c.refused = true; return -1, 0; }
            offset = cook_align_up(offset, 8);
            c.offs.push(offset);
            c.exts.push(0);
            offset += 8 + n.len + (if n.blob == KindString { 1 } else { 0 });
            continue;
        }
        let t = c.lay.tabs[tab];
        if t.align > align { align = t.align; }
        var ext: i64 = 0;
        if t.ext {
            c.at = 0;
            c.ext_base = 0;
            c.writing = false;
            if !cook_carve_record(c, tab, rec, 0) { return -1, 0; }
            ext = c.at;
        }
        offset = cook_align_up(offset, t.align);
        c.offs.push(offset);
        c.exts.push(ext);
        offset += t.record + ext;
    }
    return cook_align_up(offset, align), align;
}

// cook_data writes every node at its place into c.region (empty: the dry run).
fn cook_data(c: CookCtx&) -> bool {
    for wire, i in c.order {
        let tab, rec = cook_node(c, wire);
        let at = c.offs[i];
        if tab < 0 {
            let n = c.nodes[wire - 2];
            cook_put_at(c, at, n.len as! u64, 4);
            cook_put_bytes(c.region, at + 8, c.data[n.off..n.off + n.len]);
            continue;
        }
        if !cook_record(c, tab, rec, at) { return false; }
        if !c.lay.tabs[tab].ext { continue; }
        // THE NODE'S EXTENT, after its record: the walk the layout measured,
        // now placing each array; the extent written is the extent measured
        c.at = 0;
        c.ext_base = at + c.lay.tabs[tab].record;
        c.writing = true;
        if !cook_carve_record(c, tab, rec, at) { return false; }
        if c.at != c.exts[i] { return cook_refuse(c); }
    }
    return true;
}

// cook_region_write is THE REGION COOK of root record `root` of TableUnit
// table `root_tab`: the whole cooked file's length (a measure, `out` unused
// and `measure` true), or the file written into out[0..length] in the order
// `big` names. It answers -1, writing nothing, on a data cycle, a non-finite
// float, a union tag past its arms, extent the walk does not reach, or a
// capacity short of the length.
fn cook_region_write(unit: LoadUnit, lay: CookUnitLayout, build: u64, root_tab: i64, data: const u8[:], vals: const LoadVal[:], nodes: const LoadNode[:], root: i64, out: u8[:], big: bool, measure: bool) -> i64 {
    if root < 0 || root >= vals.len || root_tab < 0 { return -1; }
    var index: i64[>..<] = [];
    var order: i64[>..<] = [];
    var open: bool[>..<] = [];
    var offs: i64[>..<] = [];
    var exts: i64[>..<] = [];
    var c = CookCtx { unit: unit, lay: lay, data: data, vals: vals, nodes: nodes, root_tab: root_tab, root: root, region: out[0..0], big: big, index: index, order: order, open: open, offs: offs, exts: exts, refused: false, writing: false, ext_base: 0, at: 0 };
    let bytes, align = cook_layout(c);
    if bytes < 0 { return -1; }
    let data_offset = cook_align_up(TableCookHeaderBytes, align);
    let total = data_offset + bytes + c.order.len * 16;
    // THE DRY RUN: every refusal the write can meet, before a byte is written
    if !cook_data(c) { return -1; }
    if measure { return total; }
    if out.len < total { return -1; }
    cook_begin(out[0..total], build, bytes, c.order.len, align, big);
    c.region = out[data_offset..data_offset + bytes];
    if !cook_data(c) { return -1; }
    // the ATTRIBUTION part: the node directory (§6.3), one entry per node in
    // index order, its offset and its type id
    for wire, i in c.order {
        let at = data_offset + bytes + i * 16;
        cook_put(out, at, c.offs[i] as! u64, 8, big);
        cook_put(out, at + 8, cook_node_id(c, wire), 8, big);
    }
    return total;
}

// cook_region_measure is cook_region_write's measure: the whole cooked file's
// length, or -1.
fn cook_region_measure(unit: LoadUnit, lay: CookUnitLayout, build: u64, root_tab: i64, data: const u8[:], vals: const LoadVal[:], nodes: const LoadNode[:], root: i64) -> i64 {
    var none: u8[1] = [0];
    return cook_region_write(unit, lay, build, root_tab, data, vals, nodes, root, none[0..0], false, true);
}
