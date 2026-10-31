// wire_retain.goose — RETAIN-UNKNOWN on the Goose table wire
// (docs/SPEC-TABLES.md §6.6; docs/PORT-GOOSE.md, THE TABLE WIRE), ported from
// the Go oracle (internal/tablewire retain.go): an opt-in that keeps the
// fields this build cannot name and writes them back at the end of their own
// body.
//
// THE CALLER'S STORES. A RetainStore is the caller's three arrays (the
// records' resolved bytes, the record directory, and the retained ids' own
// list) and the two capacities: `capacity` bytes for the records and
// `id_capacity` entries for the ids, -1 for no ceiling. The codec never grows
// either past its capacity: a record that does not fit WHOLE is not kept and
// counts one `retain_lost`; a retained id with no entry left drops its record
// at save and counts one `retain_lost`; the save is never refused for it.
//
// A RECORD IS READER-PRIVATE (§6.6). Its layout is this port's own: the field
// id, the kind, and the payload with every reference resolved to the
// sixty-four-bit id it names and every length a fixed eight bytes (the Go
// oracle's spelling), costing 9 + its payload's bytes. It belongs to the BODY
// that carried it, named by that body's record slot in the flat value table —
// the region's own address, stable for the life of the load — and dies with
// it when a later legal occurrence of that body is read (forget_rec).
//
// WHAT IS NOT RETAINED counts `retain_lost` where the load meets it: a field
// whose payload carries a node index at any depth, an unknown enum variant,
// an unknown union arm, an unknown keyed slot, and a node record of a type
// this reader cannot name (wire_load.goose). A resolving walk that meets
// damage the plain read never looked at drops the record and never raises
// `malformed`.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;
import .schema_tables;
import .wire_load;
import .wire_save;

// RetainRec is one retained field's directory entry: the body it belongs to,
// its id and kind, its resolved payload in the store's bytes, and the save's
// own mark.
struct RetainRec { owner: i64, id: u64, kind: u8, off: i64, len: i64, dead: bool, placed: bool }

// RetainStore is THE CALLER'S STORES (§6.6).
struct RetainStore {
    capacity: i64,          // bytes the records may take, -1 for no ceiling
    id_capacity: i64,       // entries the retained ids may take, -1 for none
    used: i64,              // what the records hold, in bytes
    bytes: u8[>..<]&,
    recs: RetainRec[>..<]&,
    ids: u64[>..<]&,        // the retained ids as the save walk interned them
}

// THE WALK'S NESTING CAP: 64 nested bodies (§6.6).
let RetainDepthCap: i64 = 64;

// ---- the capture (load side) ----

// Cap is the resolving walk's cursor and its output, in the store's bytes.
struct Cap { off: i64, end: i64, bad: bool }

fn cap_u64(c: LoadCtx&, v: u64) {
    var x = v;
    for i in 8 {
        c.retain.bytes.push((x & 0xFF) as! u8);
        x = x >> 8;
    }
}

fn cap_leb(c: LoadCtx&, k: Cap&) -> u64 {
    let v, next, ok = leb_at(c.data, k.off, k.end);
    if !ok { k.bad = true; return 0; }
    k.off = next;
    return v;
}

// cap_id resolves one reference against the FILE's id table: a reference
// past the count, at an entry of zero, or at a reserved id drops the record.
fn cap_id(c: LoadCtx&, k: Cap&, ref: u64) -> u64 {
    let id, named = idof(c, ref);
    if !named || id == 0 || id == NodeWireId || id == BuildVersionWireId || id == VocabularyWireId {
        k.bad = true;
        return 0;
    }
    return id;
}

// cap_copy copies a payload that carries no reference, by §3's framing.
fn cap_copy(c: LoadCtx&, k: Cap&, kind: u8) {
    var r = Rd { off: k.off, end: k.end };
    let w = kind_width(kind);
    if w > 0 && kind != KindPointer {
        if !has(r, w) { k.bad = true; return; }
        r.off += w;
    } else if kind == KindString || kind == 31 || kind == KindNoPayload {
        let n, ok = leb(c, r);
        if !ok || !fits(r, n) { k.bad = true; return; }
        r.off += n as! i64;
    } else {
        k.bad = true;
        return;
    }
    for i in k.off..r.off { c.retain.bytes.push(c.data[i]); }
    k.off = r.off;
}

// cap_payload is the resolving walk over one self-framed payload (§6.6):
// kind 13 a body, 15 an arm, 30 a variant, 14 and 16 their elements; kind 17
// anywhere drops the record; every other payload is copied verbatim.
recursive fn cap_payload(c: LoadCtx&, k: Cap&, kind: u8, depth: i64) {
    if k.bad { return; }
    if kind == KindPointer { k.bad = true; return; }
    if kind == 30 {
        let ref = cap_leb(c, k);
        if k.bad { return; }
        if ref == 0 { cap_u64(c, 0); return; } // None is a value, not a reference
        cap_u64(c, cap_id(c, k, ref));
        return;
    }
    if kind == 15 {
        let ref = cap_leb(c, k);
        if k.bad { return; }
        if ref == 0 { cap_u64(c, 0); return; }
        cap_u64(c, cap_id(c, k, ref));
        if k.off >= k.end { k.bad = true; return; }
        let arm = c.data[k.off];
        k.off += 1;
        c.retain.bytes.push(arm);
        cap_framed(c, k, arm, depth);
        return;
    }
    if kind == KindTable || kind == KindArray || kind == 16 {
        cap_framed(c, k, kind, depth);
        return;
    }
    cap_copy(c, k, kind);
}

// cap_framed reads one LENGTH and resolves the content it frames, writing the
// content's own eight-byte length AFTER it (one pass): a nested L past its
// parent drops the record.
recursive fn cap_framed(c: LoadCtx&, k: Cap&, kind: u8, depth: i64) {
    let n = cap_leb(c, k);
    if k.bad { return; }
    if n > (k.end - k.off) as! u64 { k.bad = true; return; }
    var sub = Cap { off: k.off, end: k.off + (n as! i64), bad: false };
    k.off = sub.end;
    let at = c.retain.bytes.len;
    cap_u64(c, 0);
    cap_content(c, sub, kind, depth + 1);
    if sub.bad { k.bad = true; return; }
    var len = (c.retain.bytes.len - at - 8) as! u64;
    for i in 8 {
        c.retain.bytes[at + i] = (len & 0xFF) as! u8;
        len = len >> 8;
    }
}

// cap_content resolves a content already framed by a length, and must
// consume it exactly (§6.6: stricter than the plain read, and it may be).
fn cap_content(c: LoadCtx&, k: Cap&, kind: u8, depth: i64) {
    if depth > RetainDepthCap { k.bad = true; return; }
    if kind == KindPointer { k.bad = true; return; }
    if kind == KindTable {
        loop {
            if k.bad { return; }
            let ref = cap_leb(c, k);
            if k.bad { return; }
            if ref == 0 { cap_u64(c, 0); break; }
            if k.off >= k.end { k.bad = true; return; }
            let fk = c.data[k.off];
            k.off += 1;
            cap_u64(c, cap_id(c, k, ref));
            c.retain.bytes.push(fk);
            cap_payload(c, k, fk, depth);
        }
    } else if kind == KindArray || kind == 16 {
        if k.off >= k.end { k.bad = true; return; }
        let ek = c.data[k.off];
        k.off += 1;
        if ek == KindPointer { k.bad = true; return; }
        c.retain.bytes.push(ek);
        let count = cap_leb(c, k);
        if k.bad { return; }
        cap_u64(c, count);
        var i: u64 = 0;
        while i < count && !k.bad {
            i += 1;
            if kind == 16 {
                let ref = cap_leb(c, k);
                if k.bad { return; }
                cap_u64(c, cap_id(c, k, ref));
                cap_framed(c, k, ek, depth);
            } else {
                cap_payload(c, k, ek, depth);
            }
        }
    } else if kind == 15 || kind == 30 {
        cap_payload(c, k, kind, depth);
    } else {
        for i in k.off..k.end { c.retain.bytes.push(c.data[i]); }
        k.off = k.end;
    }
    if !k.bad && k.off != k.end { k.bad = true; }
}

// capture is the whole of the load side for one unknown field of record
// `owner`, its payload at [from, to): the outer-kind exclusion, the resolving
// walk, the capacity, and the counter each answers (§6.6).
fn capture(c: LoadCtx&, owner: i64, id: u64, kind: u8, from: i64, to: i64) {
    if kind == KindPointer { c.report.retain_lost += 1; return; }
    if (kind == KindF32 && to - from == 4 && (le_at(c.data, from, 4) & 0x7F800000) == 0x7F800000) ||
       (kind == KindF64 && to - from == 8 && (le_at(c.data, from, 8) & 0x7FF0000000000000) == 0x7FF0000000000000) {
        // A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE (§3.3, §6.6)
        c.report.non_finite = true;
        return;
    }
    let at = c.retain.bytes.len;
    var k = Cap { off: from, end: to, bad: false };
    cap_payload(c, k, kind, 0);
    if k.bad || k.off != k.end {
        // the walk's verdict changes nothing else: malformed does not move
        c.retain.bytes.resize(at);
        c.report.retain_lost += 1;
        return;
    }
    let len = c.retain.bytes.len - at;
    let cost = 9 + len;
    if c.retain.capacity >= 0 && c.retain.used + cost > c.retain.capacity {
        // REFUSAL IS PER RECORD AND NEVER PARTIAL
        c.retain.bytes.resize(at);
        c.report.retain_lost += 1;
        return;
    }
    c.retain.used += cost;
    c.retain.recs.push(RetainRec { owner: owner, id: id, kind: kind, off: at, len: len, dead: false, placed: false });
    c.report.retained += 1;
}

// forget_rec discards every record held under record `v` and the bodies it
// holds by value, because a later legal occurrence is about to replace it
// (§6.6, record lifetime): neither counter moves. A pointer is not followed.
recursive fn forget_rec(c: LoadCtx&, v: i64) {
    if v < 0 || c.retain.recs.len == 0 { return; }
    for &r in c.retain.recs { if r.owner == v { r.dead = true; } }
    if !c.vals[v].made { return; }
    let shape = c.vals[v].shape;
    if shape == ShapeUnion {
        if c.vals[v].first >= 0 { forget_rec(c, c.vals[v].first); }
        return;
    }
    for i in c.vals[v].count {
        let s = c.vals[v].first + i;
        if c.vals[s].made && c.vals[s].shape != ShapePtr { forget_rec(c, s); }
    }
}

// ---- the emit (save side) ----

fn known_id(c: SaveCtx&, id: u64) -> bool {
    var lo: i64 = 0;
    var hi = c.unit.known.len;
    while lo < hi {
        let mid = (lo + hi) / 2;
        let x = c.unit.known[mid];
        if x == id { return true; }
        if x < id { lo = mid + 1; } else { hi = mid; }
    }
    return false;
}

// em_ref interns one id of a retained record into the file's ONE id table: an
// id this build can name from the file's own table, a retained one also from
// the caller's list, and no entry left drops the record (§6.6).
fn em_ref(c: SaveCtx&, id: u64) -> bool {
    if !known_id(c, id) {
        var seen = false;
        for x in c.retain.ids { if x == id { seen = true; break; } }
        if !seen {
            if c.retain.id_capacity >= 0 && c.retain.ids.len >= c.retain.id_capacity { return false; }
            c.retain.ids.push(id);
        }
    }
    put_leb(c, id_ref(c, id));
    return true;
}

fn em_u64(c: SaveCtx&, k: Cap&) -> u64 {
    if k.end - k.off < 8 { k.bad = true; return 0; }
    var v: u64 = 0;
    for i in 8 {
        let b: u64 = c.retain.bytes[k.off + i];
        v |= b << ((8 * i) as! u64);
    }
    k.off += 8;
    return v;
}

fn em_byte(c: SaveCtx&, k: Cap&) -> u8 {
    if k.off >= k.end { k.bad = true; return 0; }
    let b = c.retain.bytes[k.off];
    k.off += 1;
    return b;
}

// em_copy writes back a payload copied whole, its own length included.
fn em_copy(c: SaveCtx&, k: Cap&, kind: u8) {
    let w = kind_width(kind);
    var n: i64 = 0;
    if w > 0 && kind != KindPointer {
        n = w;
    } else {
        // the copied length is the wire's own canonical LEB128
        var x: u64 = 0;
        var shift: u64 = 0;
        var used: i64 = 0;
        loop {
            if k.off + used >= k.end { k.bad = true; return; }
            let b = c.retain.bytes[k.off + used];
            used += 1;
            let low: u64 = b & 0x7F;
            x |= low << shift;
            if (b & 0x80) == 0 { break; }
            shift += 7;
            if shift > 63 { k.bad = true; return; }
        }
        n = used + (x as! i64);
    }
    if k.end - k.off < n { k.bad = true; return; }
    for i in n { put(c, c.retain.bytes[k.off + i]); }
    k.off += n;
}

recursive fn em_payload(c: SaveCtx&, k: Cap&, kind: u8) {
    if k.bad { return; }
    if kind == 30 {
        let id = em_u64(c, k);
        if k.bad { return; }
        if id == 0 { put_leb(c, 0); return; }
        if !em_ref(c, id) { k.bad = true; }
        return;
    }
    if kind == 15 {
        let arm = em_u64(c, k);
        if k.bad { return; }
        if arm == 0 { put_leb(c, 0); return; }
        if !em_ref(c, arm) { k.bad = true; return; }
        let ak = em_byte(c, k);
        if k.bad { return; }
        put(c, ak);
        em_framed(c, k, ak);
        return;
    }
    if kind == KindTable || kind == KindArray || kind == 16 { em_framed(c, k, kind); return; }
    em_copy(c, k, kind);
}

// em_framed writes one content and the canonical LEB128 length in front of
// it (frame: the content is written once, its length inserted after).
recursive fn em_framed(c: SaveCtx&, k: Cap&, kind: u8) {
    let n = em_u64(c, k);
    if k.bad { return; }
    if n > (k.end - k.off) as! u64 { k.bad = true; return; }
    var sub = Cap { off: k.off, end: k.off + (n as! i64), bad: false };
    k.off = sub.end;
    let start = c.out.len;
    em_content(c, sub, kind);
    if sub.bad { k.bad = true; return; }
    frame(c, start);
}

fn em_content(c: SaveCtx&, k: Cap&, kind: u8) {
    if kind == KindTable {
        loop {
            if k.bad { return; }
            let id = em_u64(c, k);
            if k.bad { return; }
            if id == 0 { put(c, 0); return; }
            let fk = em_byte(c, k);
            if k.bad { return; }
            if !em_ref(c, id) { k.bad = true; return; }
            put(c, fk);
            em_payload(c, k, fk);
        }
    }
    if kind == KindArray || kind == 16 {
        let ek = em_byte(c, k);
        if k.bad { return; }
        put(c, ek);
        let count = em_u64(c, k);
        if k.bad { return; }
        put_leb(c, count);
        var i: u64 = 0;
        while i < count && !k.bad {
            i += 1;
            if kind == 16 {
                let key = em_u64(c, k);
                if k.bad { return; }
                if !em_ref(c, key) { k.bad = true; return; }
                em_framed(c, k, ek);
            } else {
                em_payload(c, k, ek);
            }
        }
        return;
    }
    if kind == 15 || kind == 30 { em_payload(c, k, kind); return; }
    for i in k.off..k.end { put(c, c.retain.bytes[i]); }
    k.off = k.end;
}

// retain_tail writes record `rec`'s retained fields AT THE END OF ITS OWN
// BODY, in the order retained (§6.6). A record whose emit goes bad — a
// retained id the caller's list had no room for — is cut back off with its
// ids in both stores, and counts one retain_lost.
fn retain_tail(c: SaveCtx&, rec: i64) {
    if rec < 0 { return; }
    for i in c.retain.recs.len {
        let r = c.retain.recs[i];
        if r.dead || r.owner != rec { continue; }
        c.retain.recs[i].placed = true; // the walk REACHED it
        let at = c.out.len;
        let mark = c.ids.len;
        let id_mark = c.retain.ids.len;
        var k = Cap { off: r.off, end: r.off + r.len, bad: false };
        if em_ref(c, r.id) {
            put(c, r.kind);
            em_payload(c, k, r.kind);
        } else {
            k.bad = true;
        }
        if k.bad || k.off != k.end {
            cut(c, at, mark);
            c.retain.ids.resize(id_mark);
            c.retain_lost += 1;
        }
    }
}

// ---- the three verbs (§6.6) ----

// load_table_retain is load_table with retention on: it resets the caller's
// stores and keeps what this build cannot name. A fixed-class root carries no
// retention (§6.6): it is refused (-1, report.refused).
fn load_table_retain(unit: LoadUnit, root: const u8[:], data: const u8[:], vals: LoadVal[>..]&, nodes: LoadNode[>..]&, report: LoadReport&, store: RetainStore&) -> i64 {
    store.used = 0;
    store.bytes.clear();
    store.recs.clear();
    store.ids.clear();
    for t in unit.tables {
        if t.name == root && !t.variable { report.refused = true; return -1; }
    }
    return load_with(unit, root, data, vals, nodes, report, store, true);
}

// retain_last_short is the byte capacity ONE BYTE SHORT OF THE LAST RECORD a
// roomy load kept (test/conformance FORMAT.md, `short`).
fn retain_last_short(store: RetainStore&) -> i64 { store.used - 1 }

// save_table_retain is save_table with the retained tails written back: the
// caller's report ACCUMULATES across the pair, and a retained record the save
// never reached (its body is gone) counts one retain_lost (§6.6).
fn save_table_retain(unit: LoadUnit, root: const u8[:], data: const u8[:], vals: LoadVal[>..]&, nodes: LoadNode[>..]&, root_val: i64, store: RetainStore&, report: LoadReport&, out: u8[>..<]&) -> i64 {
    for &r in store.recs { r.placed = false; }
    store.ids.clear();
    var ids: u64[>..<] = [];
    var order: i64[>..<] = [];
    var index: i64[>..<] = [];
    var open: bool[>..<] = [];
    let at = out.len;
    var c = SaveCtx { data: data, unit: unit, vals: vals, nodes: nodes, out: out, ids: ids, order: order, index: index, open: open, root_tab: -1, refusal: 0, retain: store, retaining: true, retain_lost: 0 };
    for d, i in unit.tables { if d.name == root { c.root_tab = i; } }
    if c.root_tab < 0 { return SaveRefuseUnknownRoot; }
    let refusal = save_into(c, root_val);
    if refusal != 0 { out.resize(at); return refusal; }
    for r in store.recs { if !r.dead && !r.placed { c.retain_lost += 1; } }
    report.retain_lost += c.retain_lost;
    return 0;
}
