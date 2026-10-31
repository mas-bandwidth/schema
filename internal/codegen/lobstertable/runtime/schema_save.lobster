// schema_save.lobster — the Lobster table runtime's VARIABLE FORM (form 1) save
// (docs/SPEC-TABLES.md §3, §3.1, §6.6; docs/PORT-LOBSTER.md, THE RUNTIME
// LAYOUT and THE LOAD). A port of the Go reference engine's write half
// (internal/tablewire encode.go, nodes.go, nodetable.go, leb.go) onto the
// descriptors schema_tables.lobster declares: every rule lives once, here,
// and every fact (ids, kinds, bounds, defaults, the order) is read from a
// descriptor as data (docs/SPEC-TABLES.md §8.1).
//
// THE STORAGE is the load's (docs/PORT-LOBSTER.md, THE LOAD): a Record — the
// table as data, one Slot per field in descriptor order, each holding a
// Value. Save walks the same Shape keys the load dispatches on, so a
// construct card adds or replaces a shape in its own runtime file and never
// edits this file.
//
// THE TWO ELISIONS are the reference's (§3): a field at its declared default
// is not written, so old readers and new writers meet cleanly, and an elided
// field costs nothing in the id table either — it is interned, the payload
// that decides is built, and both are undone when nothing rides.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// SaveOut is one byte buffer being written: b is the storage and n its used
// extent (docs/PORT-LOBSTER.md, BYTE BUFFERS). A write may move the string,
// so every write stores what the builtin returns.
class SaveOut:
    b:string = ""
    n:int = 0

def so_grow(o:SaveOut, k:int):
    let need = o.n + k
    if length(o.b) < need:
        o.b = ensure_size(o.b, need, 0)

def so_u8(o:SaveOut, v:int):
    so_grow(o, 1)
    o.b = write_int8_le(o.b, o.n, v & 0xff)
    o.n += 1

def so_u32(o:SaveOut, v:int):
    so_grow(o, 4)
    o.b = write_int32_le(o.b, o.n, v)
    o.n += 4

def so_u64(o:SaveOut, v:int):
    so_grow(o, 8)
    o.b = write_int64_le(o.b, o.n, v)
    o.n += 8

def so_bytes(o:SaveOut, s:string):
    so_grow(o, length(s))
    o.b = write_substring(o.b, o.n, s, false)
    o.n += length(s)

// THE ONE NUMBER SHAPE (docs/SPEC-TABLES.md §3): every length, count, index
// and id reference is an unsigned LEB128 in its one canonical spelling.
def so_leb(o:SaveOut, v:int):
    var x = v
    while x >= 0x80:
        so_u8(o, (x & 0x7f) | 0x80)
        x = shr(x, 7)
    so_u8(o, x)

def so_take(o:SaveOut) -> string:
    return substring(o.b, 0, o.n)

// THE ID TABLE (docs/SPEC-TABLES.md §3): every id the body used, once each,
// in FIRST-USE order over the whole wire, which the body names by position.
// mark and rollback are how a writer elides without spending an entry.
class SaveIds:
    ids:[int] = []

def sid_ref(t:SaveIds, id:int) -> int:
    for(t.ids) x, i:
        if x == id:
            return i + 1
    push(t.ids, id)
    return length(t.ids)

def sid_mark(t:SaveIds) -> int:
    return length(t.ids)

def sid_rollback(t:SaveIds, mark:int):
    truncate(t.ids, mark)

// THE FLAT NODE TABLE's numbering (docs/SPEC-TABLES.md §3.1): the FIRST-VISIT
// order of a depth-first pre-order walk from the root over POINTER EDGES
// ONLY, descending through every by-value edge to reach the pointer fields
// inside them. A field the writer does not write is not an edge. A record is
// one node however many slots name it; a blob is owned by one slot and each
// blob occurrence on a savable wire is its own node (§2.5).
class SaveNode:
    kind:int = 0
    rec:Record? = nil
    blob:string? = nil
    cell:Value? = nil
    tid:int = 0
    open:bool = false

class SaveGraph:
    nodes:[SaveNode] = []
    recs:[Record] = []
    rec_index:[int] = []
    cells:[Value] = []
    cell_index:[int] = []
    cycle:bool = false
    // blob_nodes are the loaded blob nodes the walk has met; a second slot
    // naming one of them sets blob_shared (§2.5)
    blob_nodes:[Node] = []
    blob_shared:bool = false

def sg_rec_index(g:SaveGraph, rec:Record) -> int:
    for(g.recs) r, i:
        if r == rec:
            return g.rec_index[i]
    return 0

def sg_blob_index(g:SaveGraph, cell:Value) -> int:
    for(g.cells) c, i:
        if c == cell:
            return g.cell_index[i]
    return 0

def sg_add_rec(g:SaveGraph, rec:Record) -> int:
    let at = sg_rec_index(g, rec)
    if at != 0:
        if g.nodes[at - 1].open:
            g.cycle = true
        return at
    push(g.nodes, SaveNode { kind: 1, rec: rec, tid: rec.d.id, open: true })
    let idx = length(g.nodes)
    push(g.recs, rec)
    push(g.rec_index, idx)
    sg_visit_edges(g, rec)
    g.nodes[idx - 1].open = false
    return idx

def sg_add_blob(g:SaveGraph, cell:Value, kind:int) -> int:
    let at = sg_blob_index(g, cell)
    if at != 0:
        return at
    let bn = cell.blob_node
    if bn:
        for(g.blob_nodes) x:
            if x == bn:
                g.blob_shared = true
        push(g.blob_nodes, bn)
    let idx = length(g.nodes) + 1
    push(g.nodes, SaveNode { kind: kind, blob: cell.blob, cell: cell, tid: if kind == 2: string_blob_id else: bytes_blob_id })
    push(g.cells, cell)
    push(g.cell_index, idx)
    return idx

// save_live_count is the extent a walk descends and a writer writes: a
// counted or unbounded array's live count, every slot otherwise (§2.9).
def save_live_count(d:FieldDescriptor, sl:Slot) -> int:
    if d.array == "counted" or d.array == "list":
        return sl.count
    return length(sl.elems)

def sg_visit_value(g:SaveGraph, v:Value, d:FieldDescriptor) -> void:
    let s = d.s
    if s == "tab":
        let t = v.tab
        if t:
            sg_visit_edges(g, t)
    elif s == "ptr":
        let n = v.node
        if n:
            sg_add_rec(g, n)
    elif s == "sblob":
        if v.blob:
            sg_add_blob(g, v, 2)
    elif s == "bblob":
        if v.blob:
            sg_add_blob(g, v, 3)
    elif s == "union":
        sg_visit_union(g, v, d)

// sg_map_order is one map field's entries in ASCENDING KEY ORDER
// (docs/SPEC-TABLES.md §2.8), the order the wire carries.
def sg_map_order(f:FieldDescriptor, sl:Slot) -> [int]:
    let ed = table_of(f.t)
    let key = ed.fields[0]
    let order = []
    var at = 0
    while at < length(sl.entries):
        push(order, at)
        at++
    var i = 1
    while i < length(order):
        let x = order[i]
        var j = i - 1
        var stop = false
        while j >= 0 and not stop:
            let a = sl.entries[order[j]].tab
            let b = sl.entries[x].tab
            let ka = if a: a.fields[0].cell else: Value {}
            let kb = if b: b.fields[0].cell else: Value {}
            if map_key_order(key, ka, kb) <= 0:
                stop = true
            else:
                order[j + 1] = order[j]
                j--
        order[j + 1] = x
        i++
    return order

def sg_visit_union(g:SaveGraph, v:Value, d:FieldDescriptor) -> void:
    if v.i == 0:
        return
    let un = lookup_union(d.t)
    assert un
    if v.i < 1 or v.i > length(un.arms):
        return
    let arm = un.arms[v.i - 1]
    if arm.s == "tab":
        let t = v.tab
        if t:
            sg_visit_edges(g, t)
    elif arm.s == "arr":
        let sl = v.arm
        if sl:
            let e = elem_of(arm)
            let n = save_live_count(arm, sl)
            var i = 0
            while i < n:
                sg_visit_value(g, sl.elems[i], e)
                i++
    elif arm.s == "map":
        let sl = v.arm
        if sl:
            for(sg_map_order(arm, sl)) at:
                let er = sl.entries[at].tab
                if er:
                    sg_visit_edges(g, er)
    elif arm.s == "union":
        let sl = v.arm
        if sl:
            sg_visit_union(g, sl.cell, arm)
    else:
        let sl = v.arm
        if sl:
            sg_visit_value(g, sl.cell, arm)

// guard_holds is whether a guarded field rides (Go's Instance.GuardHolds):
// every term of its guard names a bool field of this record holding the
// value the branch wants, `!` before the name wanting false. A field under a
// false guard is not written and is not an edge (§3.1, §4).
def guard_holds(rec:Record, f:FieldDescriptor) -> bool:
    for(f.when) term:
        var name = term
        var want = true
        if length(term) > 0 and term[0] == '!':
            name = substring(term, 1, length(term) - 1)
            want = false
        var found = false
        for(rec.fields) other:
            if not found and other.fd.name == name:
                found = true
                if other.cell.b != want:
                    return false
        if not found:
            return false
    return true

def sg_visit_edges(g:SaveGraph, rec:Record) -> void:
    for(rec.fields) sl:
        let f = sl.fd
        if f.present != "" and not sl.present:
            continue
        if not guard_holds(rec, f):
            continue
        if f.s == "map":
            for(sg_map_order(f, sl)) at:
                let er = sl.entries[at].tab
                if er:
                    sg_visit_edges(g, er)
            continue
        if f.s == "arr":
            let e = elem_of(f)
            let n = save_live_count(f, sl)
            var i = 0
            while i < n:
                sg_visit_value(g, sl.elems[i], e)
                i++
            continue
        sg_visit_value(g, sl.cell, f)

// THE WRITER. SaveEncoder is one save's shared state: the file's one id
// table and the numbering every pointer slot resolves through.
class SaveEncoder:
    ids:SaveIds
    g:SaveGraph
    // non_finite names the first NaN or infinity the writer met
    // (docs/SPEC-TABLES.md §3.3): the save refuses it as non_finite_float
    non_finite:string = ""

// RETAIN-UNKNOWN's save-side hook (docs/SPEC-TABLES.md §6.6): a retaining save
// registers one, and `tail` writes the fields the matching retaining load kept
// at the END of the body they belong to, before its terminator (and, in the
// root, before the node table).
class RetainTail:
    def tail(e:SaveEncoder, w:SaveOut, rec:Record) -> void:
        return

let retain_tails:[RetainTail] = []

def register_retain_tail(h:RetainTail):
    push(retain_tails, h)

def active_retain_tail() -> RetainTail?:
    if length(retain_tails) == 0:
        return nil
    return retain_tails[length(retain_tails) - 1]

// save_wide_raw writes a 128-bit value's two's-complement lanes, low lane
// first, at the kind's width (docs/SPEC-TABLES.md §3).
def save_wide_raw(o:SaveOut, w:Int128, width:int):
    if width == 16:
        so_u64(o, w.lo)
        so_u64(o, w.hi)
    else:
        save_raw_width(o, w.lo, width)

def save_raw_width(o:SaveOut, v:int, width:int):
    var x = v
    var n = width
    while n > 0:
        so_u8(o, x & 0xff)
        x = shr(x, 8)
        n--

// value_is_default is the writer's elision test: a field holding its
// declared default is not written at all (§3). A plain float compares BIT
// PATTERNS, so -0 is not +0 (§3.3).
def save_is_default(d:FieldDescriptor, v:Value) -> bool:
    let s = d.s
    if s == "bool":
        return v.b == d.def_bool
    if s == "float":
        if d.f32:
            if d.franged:
                return fround(v.f) == fround(d.def_float)
            return f32bits(v.f) == f32bits(d.def_float)
        if d.franged:
            return v.f == d.def_float
        return f64bits(v.f) == f64bits(d.def_float)
    if s == "int":
        if kind_wide(d.k):
            return v.w.lo == d.def_int.lo and v.w.hi == d.def_int.hi
        return v.i == d.def_int.lo
    if s == "enum":
        return v.i == d.def_int.lo
    if s == "str" or s == "text" or s == "bytes":
        return v.s == d.def_str
    return false

// save_elem_is_default is the element's own zero, which is what an array
// slot is born at: an element descriptor carries no specified default
// (docs/SPEC-TABLES.md §2.9), and the wire kind says which lane holds it.
def save_elem_is_default(d:FieldDescriptor, kind:int, v:Value) -> bool:
    let s = d.s
    if s == "ptr":
        return not v.node
    if s == "sblob" or s == "bblob":
        return not v.blob
    if s == "bool":
        return not v.b
    if s == "float":
        if d.f32:
            if d.franged:
                return fround(v.f) == 0.0
            return f32bits(v.f) == 0
        if d.franged:
            return v.f == 0.0
        return f64bits(v.f) == 0
    if s == "int":
        if kind_wide(kind):
            return v.w.lo == 0 and v.w.hi == 0
        return v.i == 0
    if s == "enum":
        return v.i == 0
    if s == "union":
        return v.i == 0
    if s == "str" or s == "text" or s == "bytes":
        return v.s == ""
    return false

// save_enum_ref is an enum value's reference: the variant name's id, and the
// ZERO REFERENCE for None, the one value that names no id (§3).
def save_enum_ref(t:SaveIds, d:FieldDescriptor, value:int) -> int:
    if value == 0:
        return 0
    if value < 1 or value > length(d.vs):
        return 0
    let v = d.vs[value - 1]
    if v.name == "None":
        return 0
    return sid_ref(t, v.id)

// save_element writes one element at its kind's own framing (Go's
// encodeElement, docs/SPEC-TABLES.md §3, §3.1).
def save_element(e:SaveEncoder, w:SaveOut, d:FieldDescriptor, kind:int, v:Value) -> void:
    if d.s == "tab" and kind == 12:
        let rec = v.tab
        var val = ""
        if rec:
            val = rec.fields[0].cell.s
        so_leb(w, length(val))
        so_bytes(w, val)
        return
    if d.s == "enum":
        so_leb(w, save_enum_ref(e.ids, d, v.i))
        return
    if kind == 1:
        so_u8(w, if v.b: 1 else: 0)
    elif kind == 12:
        so_leb(w, length(v.s))
        so_bytes(w, v.s)
    elif kind == 10:
        // A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): a NaN or an infinity
        // (a double past float32's range included, once narrowed) is refused
        // by the writer, never written; the ordinary conversion carries
        // every finite value, -0 and the subnormals bit for bit.
        let b = f32bits(v.f)
        if not finite_f32_bits(b) and e.non_finite == "":
            e.non_finite = float_owner(d)
        so_u32(w, b)
    elif kind == 11:
        let b = f64bits(v.f)
        if not finite_f64_bits(b) and e.non_finite == "":
            e.non_finite = float_owner(d)
        so_u64(w, b)
    elif kind == 13:
        var t = v.tab
        if not t:
            t = new_record(table_of(d.t))
        let body = encode_body(e, t)
        so_leb(w, length(body))
        so_bytes(w, body)
    elif kind == 17:
        let idx = if v.node: sg_rec_index(e.g, v.node) else: 0
        so_leb(w, idx)
    elif kind == 15:
        if v.i == 0:
            so_leb(w, 0)
        else:
            save_arm_header(e, w, d, v)
    elif kind_wide(kind):
        save_wide_raw(w, v.w, kind_width(kind))
    else:
        save_raw_width(w, v.i, kind_width(kind))

// save_arm_header writes a SET arm whole: AN ARM HEADER IS A FIELD HEADER
// (§3) — the arm id reference, the arm's kind byte, L, then L bytes.
def save_arm_header(e:SaveEncoder, w:SaveOut, d:FieldDescriptor, v:Value) -> void:
    let un = lookup_union(d.t)
    assert un
    let arm = un.arms[v.i - 1]
    so_leb(w, sid_ref(e.ids, arm.id))
    so_u8(w, arm_wire_kind(arm))
    let payload = save_arm_payload(e, arm, v)
    so_leb(w, payload.n)
    so_bytes(w, so_take(payload))

// save_arm_payload writes ONE ARM's payload — the bytes under the arm's L
// (docs/SPEC-TABLES.md §3's arm payload table); an arm is a field line
// (§2.6).
def save_arm_payload(e:SaveEncoder, arm:FieldDescriptor, v:Value) -> SaveOut:
    let w = SaveOut {}
    if arm.s == "void":
        return w
    if arm.s == "tab":
        var t = v.tab
        if not t:
            t = new_record(table_of(arm.t))
        so_bytes(w, encode_body(e, t))
        return w
    var sl = v.arm
    if not sl:
        sl = new_slot(arm)
    let c = sl.cell
    if arm.s == "ptr":
        let idx = if c.node: sg_rec_index(e.g, c.node) else: 0
        so_leb(w, idx)
    elif arm.s == "sblob" or arm.s == "bblob":
        let b = c.blob
        if b:
            so_bytes(w, b)
    elif arm.s == "str":
        so_bytes(w, c.s)
    elif arm.s == "bytes":
        so_u8(w, 6)
        so_leb(w, length(c.s))
        so_bytes(w, c.s)
    elif arm.s == "arr":
        let cnt = save_live_count(arm, sl)
        so_u8(w, arm.ek)
        so_leb(w, cnt)
        let ed = elem_of(arm)
        var i = 0
        while i < cnt:
            save_element(e, w, ed, arm.ek, sl.elems[i])
            i++
    elif arm.s == "union":
        if c.i == 0:
            so_leb(w, 0)
        else:
            save_arm_header(e, w, arm, c)
    else:
        if arm.s == "enum":
            so_leb(w, save_enum_ref(e.ids, arm, c.i))
        else:
            save_element(e, w, arm, arm.k, c)
    return w

// save_array writes a positional array body (§3): header kind 14, then the
// element kind, the count, and the elements.
def save_array(e:SaveEncoder, w:SaveOut, sl:Slot, elemKind:int, count:int) -> void:
    so_leb(w, sid_ref(e.ids, sl.fd.id))
    so_u8(w, 14)
    let body = SaveOut {}
    so_u8(body, elemKind)
    so_leb(body, count)
    let ed = elem_of(sl.fd)
    var i = 0
    while i < count:
        save_element(e, body, ed, elemKind, sl.elems[i])
        i++
    so_leb(w, body.n)
    so_bytes(w, so_take(body))

// save_keyed writes an enum-keyed array (§3.2): kind 16, then one
// (key reference, L, element body) triple per PRESENT slot, ascending. A
// default slot elides and costs no id entry.
def save_keyed(e:SaveEncoder, w:SaveOut, sl:Slot) -> void:
    let f = sl.fd
    let mark = sid_mark(e.ids)
    let ref = sid_ref(e.ids, f.id)
    let body = SaveOut {}
    let keys = keyed_variants(f)
    let ed = elem_of(f)
    var pairs = 0
    var slot = 0
    while slot < length(sl.elems):
        if slot < length(keys):
            let v = sl.elems[slot]
            let slotMark = sid_mark(e.ids)
            let keyRef = sid_ref(e.ids, keys[slot].id)
            var elem = ""
            var rides = true
            if ed.s == "tab":
                var t = v.tab
                if not t:
                    t = new_record(table_of(ed.t))
                let b = encode_body(e, t)
                if length(b) <= 1:
                    sid_rollback(e.ids, slotMark)
                    rides = false
                else:
                    elem = b
            else:
                if save_elem_is_default(ed, f.ek, v):
                    sid_rollback(e.ids, slotMark)
                    rides = false
                else:
                    let eb = SaveOut {}
                    save_element(e, eb, ed, f.ek, v)
                    elem = so_take(eb)
            if rides:
                so_leb(body, keyRef)
                so_leb(body, length(elem))
                so_bytes(body, elem)
                pairs++
        slot++
    if pairs == 0:
        sid_rollback(e.ids, mark)
        return
    so_leb(w, ref)
    so_u8(w, 16)
    let inner = SaveOut {}
    so_u8(inner, f.ek)
    so_leb(inner, pairs)
    so_bytes(inner, so_take(body))
    so_leb(w, inner.n)
    so_bytes(w, so_take(inner))

// encode_field writes one field's line, in declaration order (§3).
def encode_field(e:SaveEncoder, w:SaveOut, sl:Slot) -> void:
    let f = sl.fd
    if f.s == "ptr":
        let idx = if sl.cell.node: sg_rec_index(e.g, sl.cell.node) else: 0
        if idx == 0 and not f.required:
            return
        so_leb(w, sid_ref(e.ids, f.id))
        so_u8(w, 17)
        so_leb(w, idx)
        return
    if f.s == "sblob" or f.s == "bblob":
        if not sl.cell.blob and not f.required:
            return
        let idx = sg_blob_index(e.g, sl.cell)
        so_leb(w, sid_ref(e.ids, f.id))
        so_u8(w, 17)
        so_leb(w, idx)
        return
    if f.s == "map":
        // PRESENCE IS THE PAYLOAD (docs/SPEC-TABLES.md §2.8): an ABSENT
        // optional map elides; a PRESENT-EMPTY one writes its kind-14 body
        // with count 0, so absent and present-empty stay two values.
        if f.present != "" and not sl.present:
            return
        if length(sl.entries) == 0 and f.present == "":
            return
        so_leb(w, sid_ref(e.ids, f.id))
        so_u8(w, 14)
        let order = sg_map_order(f, sl)
        let body = SaveOut {}
        so_u8(body, 13)
        so_leb(body, length(order))
        for(order) at:
            let er = sl.entries[at].tab
            var eb = ""
            if er:
                eb = encode_body(e, er)
            so_leb(body, length(eb))
            so_bytes(body, eb)
        so_leb(w, body.n)
        so_bytes(w, so_take(body))
        return
    if f.present != "":
        if not sl.present:
            return
        if f.s == "arr":
            let cnt = if (f.array == "counted" or f.array == "list"): sl.count else: f.bound
            save_array(e, w, sl, f.ek, cnt)
            return
        if f.k == 13:
            let ref = sid_ref(e.ids, f.id)
            var t = sl.cell.tab
            if not t:
                t = new_record(table_of(f.t))
            let body = encode_body(e, t)
            so_leb(w, ref)
            so_u8(w, 13)
            so_leb(w, length(body))
            so_bytes(w, body)
            return
        so_leb(w, sid_ref(e.ids, f.id))
        so_u8(w, f.k)
        save_element(e, w, f, f.k, sl.cell)
        return
    if f.s == "arr":
        if f.array == "keyed":
            save_keyed(e, w, sl)
            return
        let ed = elem_of(f)
        if f.array == "counted" or f.array == "list":
            if sl.count == 0 and not f.required:
                return
            save_array(e, w, sl, f.ek, sl.count)
            return
        if f.ek == 13:
            save_array(e, w, sl, f.ek, f.bound)
            return
        var all = true
        var i = 0
        while i < f.bound:
            if i < length(sl.elems) and not save_elem_is_default(ed, f.ek, sl.elems[i]):
                all = false
            i++
        if all and not f.required:
            return
        save_array(e, w, sl, f.ek, f.bound)
        return
    if f.s == "bytes":
        if sl.cell.s == f.def_str and not f.required:
            return
        so_leb(w, sid_ref(e.ids, f.id))
        so_u8(w, 14)
        let body = SaveOut {}
        so_u8(body, 6)
        so_leb(body, length(sl.cell.s))
        so_bytes(body, sl.cell.s)
        so_leb(w, body.n)
        so_bytes(w, so_take(body))
        return
    if f.s == "str" or f.s == "text":
        if sl.cell.s == f.def_str and not f.required:
            return
        so_leb(w, sid_ref(e.ids, f.id))
        so_u8(w, 12)
        so_leb(w, length(sl.cell.s))
        so_bytes(w, sl.cell.s)
        return
    if f.s == "union":
        if sl.cell.i == 0:
            return
        so_leb(w, sid_ref(e.ids, f.id))
        so_u8(w, 15)
        save_arm_header(e, w, f, sl.cell)
        return
    if f.s == "tab":
        let mark = sid_mark(e.ids)
        let ref = sid_ref(e.ids, f.id)
        var t = sl.cell.tab
        if not t:
            t = new_record(table_of(f.t))
        let body = encode_body(e, t)
        if length(body) <= 1 and not f.required:
            sid_rollback(e.ids, mark)
            return
        so_leb(w, ref)
        so_u8(w, 13)
        so_leb(w, length(body))
        so_bytes(w, body)
        return
    if not f.required and save_is_default(f, sl.cell):
        return
    so_leb(w, sid_ref(e.ids, f.id))
    so_u8(w, f.k)
    save_element(e, w, f, f.k, sl.cell)

// encode_body_fields is a body WITHOUT its terminator; encode_body adds the
// one-byte zero reference that ends it (§3).
def encode_body_fields(e:SaveEncoder, w:SaveOut, rec:Record) -> void:
    for(rec.fields) sl:
        if guard_holds(rec, sl.fd):
            encode_field(e, w, sl)
    let tail = active_retain_tail()
    if tail:
        tail.tail(e, w, rec)

def encode_body(e:SaveEncoder, rec:Record) -> string:
    let w = SaveOut {}
    encode_body_fields(e, w, rec)
    so_u8(w, 0)
    return so_take(w)

// THE NODE TABLE (docs/SPEC-TABLES.md §3.1): under the reserved id, one
// kind-12 field carrying the count and then `type id reference, length,
// body` per record, in numbering order, after the root's own fields.
def encode_node_table(e:SaveEncoder, w:SaveOut, g:SaveGraph) -> void:
    so_leb(w, sid_ref(e.ids, node_table_id))
    so_u8(w, 12)
    let payload = SaveOut {}
    so_leb(payload, length(g.nodes) - 1)
    var i = 1
    while i < length(g.nodes):
        let node = g.nodes[i]
        so_leb(payload, sid_ref(e.ids, node.tid))
        if node.kind == 2 or node.kind == 3:
            let b = node.blob
            var data = ""
            if b:
                data = b
            so_leb(payload, length(data))
            so_bytes(payload, data)
        else:
            let rec = node.rec
            var body = ""
            if rec:
                body = encode_body(e, rec)
            so_leb(payload, length(body))
            so_bytes(payload, body)
        i++
    so_leb(w, payload.n)
    so_bytes(w, so_take(payload))

// save writes a form-1 wire from a Record (docs/SPEC-TABLES.md §3): the FORM
// BYTE, the ROOT BODY, the ID TABLE, in that order. A data cycle is refused
// as the reference refuses it (§3.1), and so is one blob named from two slots
// (§2.5: every writer refuses that graph rather than write a copy per slot)
// and a NaN or an infinity in any float (§3.3); the empty string is the
// refusal, and save_reasoned names it.
def save(rec:Record) -> string:
    return save_reasoned(rec)

// save_reasoned is save with the refusal's name beside the bytes: "cycle",
// "blob_shared", "non_finite_float" (Go's TableRefuseReason spellings), or ""
// when it wrote.
def save_reasoned(rec:Record) -> string, string:
    let g = SaveGraph {}
    push(g.nodes, SaveNode { kind: 1, rec: rec, tid: rec.d.id, open: true })
    push(g.recs, rec)
    push(g.rec_index, 1)
    sg_visit_edges(g, rec)
    g.nodes[0].open = false
    if g.cycle:
        return "", "cycle"
    if g.blob_shared:
        return "", "blob_shared"
    let e = SaveEncoder { ids: SaveIds {}, g: g }
    let fields = SaveOut {}
    encode_body_fields(e, fields, rec)
    if length(g.nodes) > 1:
        encode_node_table(e, fields, g)
    if e.non_finite != "":
        return "", "non_finite_float"
    let out = SaveOut {}
    so_u8(out, 1) // THE FORM BYTE
    so_bytes(out, so_take(fields))
    so_u8(out, 0) // the root body ENDS AT ITS OWN ZERO REFERENCE
    for(e.ids.ids) id:
        so_u64(out, id)
    so_u64(out, length(e.ids.ids))
    return so_take(out), ""
