// retain_unknown.lobster — RETAIN-UNKNOWN for the Lobster table runtime
// (docs/SPEC-TABLES.md §6.6; docs/PORT-LOBSTER.md, THE LOAD and THE RUNTIME
// LAYOUT). It joins the two walkers' per-kind hooks: load.lobster's
// RetainCapture sees a field id the reader cannot name, and schema_save's
// RetainTail writes the fields a retaining load kept at the END of the body
// that carried them. Nothing here changes a plain load or save.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_save
import .schema_tables

namespace schema_rt

// THE CALLER'S TWO STORES (docs/SPEC-TABLES.md §6.6): a byte capacity for the
// retained records and an entry capacity for the retained ids' own list. It
// allocates nothing the caller did not ask for and never grows. A load resets
// both; a save fills the id list in first-use order.
class Retain:
    capacity:int = 0
    id_capacity:int = 0
    used:int = 0
    ids:[int] = []
    known:[int] = []
    // holders is every record this load kept a field in, the store's own
    // index (Go's store.records keys): a save counts the ones it never
    // reached
    holders:[Record] = []

// retain_depth_cap is THE WALK'S NESTING CAP (docs/SPEC-TABLES.md §6.6): 64
// nested bodies, the one depth on this path a file can drive.
let retain_depth_cap = 64

let map_value_id = 0x7ce4fd9430e80cea // FNV-1a 64 of "value"

// A Cursor is the resolving walk's reader over one payload's bytes. It reads
// kind bytes, lengths and references and nothing else.
class Cursor:
    b:string
    off:int = 0

def cur_has(c:Cursor, n:int) -> bool:
    return n >= 0 and n <= length(c.b) - c.off

def cur_leb(c:Cursor) -> int, bool:
    let v, next, ok = read_leb(c.b, c.off, length(c.b))
    if ok:
        c.off = next
    return v, ok

def cur_u8(c:Cursor) -> int:
    let v = c.b[c.off]
    c.off++
    return v

// Resolver captures one record: every reference becomes the 64-bit id it
// names, and every length this engine frames becomes its own fixed-width
// eight-byte length, written after the content it frames.
class Resolver:
    ids:[int]
    out:SaveOut = SaveOut {}
    bad:bool = false

def res_u64(rs:Resolver, v:int):
    so_u64(rs.out, v)

def res_u8(rs:Resolver, v:int):
    so_u8(rs.out, v)

def res_raw(rs:Resolver, s:string):
    so_bytes(rs.out, s)

def res_mark(rs:Resolver) -> int:
    let at = rs.out.n
    so_u64(rs.out, 0)
    return at

def res_patch(rs:Resolver, at:int):
    rs.out.b = write_int64_le(rs.out.b, at, rs.out.n - at - 8)

// res_id resolves one reference against the FILE's id table (docs/SPEC-TABLES.md
// §6.6, THE WALK IS AN INTERPRETATION): a reference above the entry count, at
// an entry of ZERO, or at a reserved id DROPS the record.
def res_id(rs:Resolver, ref:int) -> int:
    if ref == 0 or ult(length(rs.ids), ref):
        rs.bad = true
        return 0
    let id = rs.ids[ref - 1]
    if id == 0 or id == node_table_id or id == build_version_id or id == message_vocabulary_id:
        rs.bad = true
        return 0
    return id

// res_skip_payload steps one payload the walk copies verbatim, by §3's framing
// rules alone.
def res_skip_payload(c:Cursor, kind:int) -> bool:
    if (kind >= 1 and kind <= 11) or (kind >= 18 and kind <= 29) or kind == 36:
        let w = kind_width(kind)
        if not cur_has(c, w):
            return false
        c.off += w
        return true
    if kind == 12 or kind == 31 or kind == 32 or kind == 33:
        let n, ok = cur_leb(c)
        if not ok or n < 0 or not cur_has(c, n):
            return false
        c.off += n
        return true
    return false

// res_framed reads one LENGTH and hands the content it frames to
// res_content; a nested L that runs past its parent drops the record.
def res_framed(rs:Resolver, c:Cursor, kind:int, depth:int) -> void:
    let n, ok = cur_leb(c)
    if not ok or n < 0 or not cur_has(c, n):
        rs.bad = true
        return
    let sub = Cursor { b: substring(c.b, c.off, n), off: 0 }
    c.off += n
    let at = res_mark(rs)
    res_content(rs, sub, kind, depth + 1)
    res_patch(rs, at)

// res_keyed_slot is one triple of an enum-keyed body: the KEY REFERENCE, the
// element's own length and the element.
def res_keyed_slot(rs:Resolver, c:Cursor, ek:int, depth:int) -> void:
    let ref, ok = cur_leb(c)
    if not ok:
        rs.bad = true
        return
    res_u64(rs, res_id(rs, ref))
    res_framed(rs, c, ek, depth)

// res_body resolves one table body: its fields, ending at the body's own ZERO
// REFERENCE, which rides at the width every other reference does.
def res_body(rs:Resolver, c:Cursor, depth:int) -> void:
    while true:
        if rs.bad:
            return
        let ref, ok = cur_leb(c)
        if not ok:
            rs.bad = true
            return
        if ref == 0:
            res_u64(rs, 0)
            return
        if not cur_has(c, 1):
            rs.bad = true
            return
        let kind = cur_u8(c)
        res_u64(rs, res_id(rs, ref))
        res_u8(rs, kind)
        res_payload(rs, c, kind, depth)

// res_content resolves a payload ALREADY FRAMED by a length. THE CONTENT MUST
// BE CONSUMED EXACTLY: slack the plain read would skip by L is a shape this
// walk cannot frame and drops the record.
def res_content(rs:Resolver, c:Cursor, kind:int, depth:int) -> void:
    if rs.bad:
        return
    if depth > retain_depth_cap:
        rs.bad = true
        return
    if kind == 17:
        rs.bad = true
        return
    if kind == 13:
        res_body(rs, c, depth)
    elif kind == 14 or kind == 16:
        if not cur_has(c, 1):
            rs.bad = true
            return
        let ek = cur_u8(c)
        if ek == 17:
            rs.bad = true
            return
        res_u8(rs, ek)
        let count, ok = cur_leb(c)
        if not ok:
            rs.bad = true
            return
        res_u64(rs, count)
        var i = 0
        while i < count:
            if rs.bad:
                return
            if kind == 16:
                res_keyed_slot(rs, c, ek, depth)
            else:
                res_payload(rs, c, ek, depth)
            i++
    elif kind == 15 or kind == 30:
        res_payload(rs, c, kind, depth)
    else:
        res_raw(rs, substring(c.b, c.off, length(c.b) - c.off))
        c.off = length(c.b)
    if not rs.bad and c.off != length(c.b):
        rs.bad = true

// res_payload resolves one self-framed payload (docs/SPEC-TABLES.md §6.6,
// WHICH KINDS THE RESOLVING WALK TOUCHES): kind 13, 15, 30, 14, 16 and every
// kind 17 is the excluded node-index class. Every other payload is copied
// verbatim.
def res_payload(rs:Resolver, c:Cursor, kind:int, depth:int) -> void:
    if rs.bad:
        return
    if kind == 17:
        rs.bad = true
    elif kind == 30:
        let ref, ok = cur_leb(c)
        if not ok:
            rs.bad = true
            return
        if ref == 0:
            res_u64(rs, 0)
            return
        res_u64(rs, res_id(rs, ref))
    elif kind == 15:
        let ref, ok = cur_leb(c)
        if not ok:
            rs.bad = true
            return
        if ref == 0:
            res_u64(rs, 0)
            return
        res_u64(rs, res_id(rs, ref))
        if not cur_has(c, 1):
            rs.bad = true
            return
        let arm_kind = cur_u8(c)
        res_u8(rs, arm_kind)
        res_framed(rs, c, arm_kind, depth)
    elif kind == 13 or kind == 14 or kind == 16:
        res_framed(rs, c, kind, depth)
    else:
        let at0 = c.off
        if not res_skip_payload(c, kind):
            rs.bad = true
            return
        res_raw(rs, substring(c.b, at0, c.off - at0))

// known_ids is every id this build can spell (ir.TableWireIds): an id inside a
// retained record that is here takes no entry from the caller's retained-id
// list, because the generated table can hold it.
def known_ids() -> [int]:
    let ids:[int] = [node_table_id, build_version_id, message_vocabulary_id, string_blob_id, bytes_blob_id, map_key_id, map_value_id]
    for(registered_tables()) d:
        for(d.fields) f:
            note_known(f, ids)
    for(registered_unions()) u:
        for(u.arms) a:
            note_known(a, ids)
    return ids

def note_known(f:FieldDescriptor, ids:[int]) -> void:
    push(ids, f.id)
    if f.s == "map":
        push(ids, map_key_id)
        push(ids, map_value_id)
    for(f.vs) v:
        push(ids, v.id)
    let e = f.e
    if e:
        push(ids, e.id)
        for(e.vs) v:
            push(ids, v.id)
    if f.s == "union":
        let un = lookup_union(f.t)
        if un:
            for(un.arms) a:
                note_known(a, ids)

class RetainCaptureImpl : RetainCapture
    retain:Retain
    report:LoadReport
    built_slots:[Slot] = []
    built_recs:[Record] = []
    def capture(rec:Record, id:int, kind:int, payload:string, ids:[int]) -> void:
        if kind == 17:
            lost()
            return
        let rs = Resolver { ids: ids, out: SaveOut {}, bad: false }
        let c = Cursor { b: payload, off: 0 }
        res_payload(rs, c, kind, 0)
        if rs.bad or c.off != length(c.b):
            lost()
            return
        let cost = 9 + rs.out.n
        if retain.used + cost > retain.capacity:
            lost()
            return
        retain.used += cost
        if length(rec.retained) == 0:
            push(retain.holders, rec)
        push(rec.retained, RetainedField { id: id, kind: kind, payload: so_take(rs.out), cost: cost, placed: false })
        report.retained++
    def lost() -> void:
        report.retain_lost++
    def built(sl:Slot, rec:Record) -> void:
        push(built_slots, sl)
        push(built_recs, rec)
    def forget_built(sl:Slot) -> void:
        var at = 0
        var i = 0
        while i < length(built_slots):
            if built_slots[i] == sl:
                forget(built_recs[i])
            else:
                built_slots[at] = built_slots[i]
                built_recs[at] = built_recs[i]
                at++
            i++
        truncate(built_slots, at)
        truncate(built_recs, at)

// load_retain is load with retention on (docs/SPEC-TABLES.md §6.6): it reads
// exactly what load reads and reports exactly what load reports, and beside
// that keeps the fields this build cannot name in the caller's Retain.
def load_retain(root:string, data:string, retain:Retain, report:LoadReport) -> Record:
    retain.used = 0
    retain.ids = []
    retain.holders = []
    retain.known = known_ids()
    let mark = length(retain_captures)
    register_retain_capture(RetainCaptureImpl { retain: retain, report: report })
    let rec = load_tolerant(root, data, report)
    truncate(retain_captures, mark)
    if rec:
        return rec
    return new_record(table_of(root))

// An Emitter writes one retained field back onto the wire: every resolved id
// is re-interned into THIS file's one id table, and every fixed-width length
// becomes canonical LEB128.
class Emitter:
    e:SaveEncoder
    retain:Retain
    report:LoadReport
    bad:bool = false

def em_known(em:Emitter, id:int) -> bool:
    for(em.retain.known) x:
        if x == id:
            return true
    return false

// em_ref interns one id. A retained id past the caller's list capacity drops
// the record; the save is never refused.
def em_ref(em:Emitter, w:SaveOut, id:int):
    if not em_known(em, id):
        var held = false
        for(em.retain.ids) x:
            if x == id:
                held = true
        if not held:
            if length(em.retain.ids) >= em.retain.id_capacity:
                em.bad = true
                return
            push(em.retain.ids, id)
    so_leb(w, sid_ref(em.e.ids, id))

def em_resolved(em:Emitter, c:Cursor) -> int:
    if not cur_has(c, 8):
        em.bad = true
        return 0
    let v = read_int64_le(c.b, c.off)
    c.off += 8
    return v

def em_framed_in(em:Emitter, c:Cursor) -> Cursor:
    if not cur_has(c, 8):
        em.bad = true
        return Cursor { b: "", off: 0 }
    let n = read_int64_le(c.b, c.off)
    c.off += 8
    if n < 0 or not cur_has(c, n):
        em.bad = true
        return Cursor { b: "", off: 0 }
    let sub = Cursor { b: substring(c.b, c.off, n), off: 0 }
    c.off += n
    return sub

// em_body writes one table body's fields and the zero reference that ends it.
def em_body(em:Emitter, w:SaveOut, c:Cursor) -> void:
    while true:
        if em.bad:
            return
        let id = em_resolved(em, c)
        if em.bad:
            return
        if id == 0:
            so_u8(w, 0)
            return
        if not cur_has(c, 1):
            em.bad = true
            return
        let kind = cur_u8(c)
        em_ref(em, w, id)
        if em.bad:
            return
        so_u8(w, kind)
        em_payload(em, w, c, kind)

def em_content(em:Emitter, w:SaveOut, c:Cursor, kind:int) -> void:
    if em.bad:
        return
    if kind == 13:
        em_body(em, w, c)
    elif kind == 14 or kind == 16:
        if not cur_has(c, 1):
            em.bad = true
            return
        let ek = cur_u8(c)
        so_u8(w, ek)
        let count = em_resolved(em, c)
        so_leb(w, count)
        var i = 0
        while i < count:
            if em.bad:
                return
            if kind == 16:
                em_ref(em, w, em_resolved(em, c))
                if not em.bad:
                    em_framed(em, w, c, ek)
            else:
                em_payload(em, w, c, ek)
            i++
    elif kind == 15 or kind == 30:
        em_payload(em, w, c, kind)
    else:
        so_bytes(w, substring(c.b, c.off, length(c.b) - c.off))
        c.off = length(c.b)

def em_framed(em:Emitter, w:SaveOut, c:Cursor, kind:int) -> void:
    let content = em_framed_in(em, c)
    if em.bad:
        return
    let inner = SaveOut {}
    em_content(em, inner, content, kind)
    if em.bad:
        return
    so_leb(w, inner.n)
    so_bytes(w, so_take(inner))

def em_payload(em:Emitter, w:SaveOut, c:Cursor, kind:int) -> void:
    if em.bad:
        return
    if kind == 30:
        let id = em_resolved(em, c)
        if id == 0:
            so_leb(w, 0)
            return
        em_ref(em, w, id)
    elif kind == 15:
        let arm = em_resolved(em, c)
        if arm == 0:
            so_leb(w, 0)
            return
        em_ref(em, w, arm)
        if em.bad or not cur_has(c, 1):
            em.bad = true
            return
        let arm_kind = cur_u8(c)
        so_u8(w, arm_kind)
        em_framed(em, w, c, arm_kind)
    elif kind == 13 or kind == 14 or kind == 16:
        em_framed(em, w, c, kind)
    else:
        let at0 = c.off
        if not res_skip_payload(c, kind):
            em.bad = true
            return
        so_bytes(w, substring(c.b, at0, c.off - at0))

def em_record(em:Emitter, w:SaveOut, rf:RetainedField):
    em_ref(em, w, rf.id)
    if em.bad:
        return
    so_u8(w, rf.kind)
    let c = Cursor { b: rf.payload, off: 0 }
    em_payload(em, w, c, rf.kind)

// RetainTailImpl writes the retained fields at the END of their own body. A
// record whose emit goes bad is not written at all: one retain_lost counts,
// both the file's table and the caller's list roll back, and the save is never
// refused.
class RetainTailImpl : RetainTail
    retain:Retain
    report:LoadReport
    def tail(e:SaveEncoder, w:SaveOut, rec:Record) -> void:
        for(rec.retained) rf:
            let mark = sid_mark(e.ids)
            let id_mark = length(retain.ids)
            let inner = SaveOut {}
            let em = Emitter { e: e, retain: retain, report: report, bad: false }
            em_record(em, inner, rf)
            rf.placed = true
            if em.bad:
                sid_rollback(e.ids, mark)
                truncate(retain.ids, id_mark)
                report.retain_lost++
            else:
                so_bytes(w, so_take(inner))

// save_retain is save with the retained fields written back (docs/SPEC-TABLES.md
// §6.6): the save walk reaches every body it writes, and the report accumulates
// what the load could not keep and what the save could not place.
def save_retain(rec:Record, retain:Retain, report:LoadReport) -> string:
    if length(retain.known) == 0:
        retain.known = known_ids()
    retain.ids = []
    // THE SAVE'S OWN MARK IS CLEARED BEFORE EVERY SAVE (Go's EncodeRetain)
    for(retain.holders) h:
        for(h.retained) rf:
            rf.placed = false
    let mark = length(retain_tails)
    register_retain_tail(RetainTailImpl { retain: retain, report: report })
    let out = save(rec)
    truncate(retain_tails, mark)
    // AND A RETAINED RECORD THE SAVE WALK NEVER REACHED COUNTS ONE
    // retain_lost (§6.6): the body it belonged to is not in this save, an
    // orphan node, or a body the caller dropped between load and save
    for(retain.holders) h:
        for(h.retained) rf:
            if not rf.placed:
                report.retain_lost++
    return out
