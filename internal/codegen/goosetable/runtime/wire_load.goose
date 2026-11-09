// wire_load.goose — the Goose port's VARIABLE TABLE WIRE LOAD (docs/SPEC-TABLES.md
// §3, §3.1, §3.2, §4; docs/PORT-GOOSE.md, THE LOADED VALUE): the strict read
// of form 1, ported decision for decision from the Go reference engine
// (internal/tablewire decode.go, decodenodes.go, nodetable.go). Any reader
// reads any data and the differences are REPORTED, never fatal: unknown ids
// are skipped by their length and counted, a kind that grew is widened and
// counted, out-of-range values clamp and count, a repeated map key counts
// repeated, and a form byte this reader does not carry is refused. Damage
// anywhere discards the whole load; no partial root is returned (§4).
//
// The walk is driven by THE LOAD DISPATCH TABLE (schema_tables.goose,
// load_registry): each value's descriptor names its Loader, and the field,
// element and arm positions each call one overload set on it (load_field,
// load_item, load_payload; the Goose spec's tag dispatch, case functions), one case function
// per registered kind. It reads nothing but the descriptors and the unit the
// caller hands it (LoadUnit). What it builds is the FLAT VALUE TABLE: every
// value it decodes is a LoadVal in the caller's pool, linked by index, and
// every node record of §3.1 is a LoadNode in the caller's node table. A form
// or construct card copies from there into the typed records through its
// per-table accessor; nothing here names a generated type.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;
import .schema_tables;
import .wire_retain;

// THE THREE STATES of a loaded value (docs/PORT-GOOSE.md, THE LOADED VALUE).
let ValAbsent: u8 = 0;  // never rode: the value is the descriptor's default
let ValEmpty: u8 = 1;   // rode and placed nothing: union None, a null or
                        // unresolved pointer, an unknown enum variant, damaged
                        // text read as its default; an optional is PRESENT
let ValPresent: u8 = 2; // rode and placed its value

// The header kinds (docs/SPEC-TABLES.md §3) the walk names.
let KindBool: u8 = 1;
let KindF32: u8 = 10;
let KindF64: u8 = 11;
let KindString: u8 = 12;
let KindTable: u8 = 13;
let KindArray: u8 = 14;
let KindPointer: u8 = 17;
let KindNoPayload: u8 = 32;

// The reserved ids (docs/SPEC-TABLES.md §3.1, §3.3, §2.5, §2.8).
let NodeWireId: u64 = 0xFFFFFFFFFFFFFFFF;
let BuildVersionWireId: u64 = 0xFFFFFFFFFFFFFFFE;
let VocabularyWireId: u64 = 0xFFFFFFFFFFFFFFFD;
let BytesTypeId: u64 = 0x2f2ec0474f1c4fe4;  // FNV-1a 64 of "bytes"
let StringTypeId: u64 = 0x704be0d8faaffc58; // FNV-1a 64 of "string"

// LoadReport is §4's read report: the five counters, the damage flag and the
// verdict (docs/SPEC-TABLES.md §3, §4). blob_shared is §2.5's extra namings;
// count_refused is §2.9's storage refusal on the builder path.
struct LoadReport {
    unknown: i64,
    mismatch: i64,
    widened: i64,
    oor: i64,
    repeated: i64,
    body_damaged: bool,
    refused: bool,
    blob_shared: i64,
    count_refused: bool,
    non_finite: bool,  // §3.3: a NaN or an infinity refused the read
    retained: i64,     // §6.6: the unknown fields whose bytes were kept
    retain_lost: i64,  // §6.6: every unknown the load or save could not keep
    stopped: bool,     // §3: framing damage stopped the walk; the whole load is discarded
    damaged: bool,     // §4's verdict is damage: the file failed its framing, or a value-only root's walk read past damage
    reason: i64,       // a refusal's name (LoadRefuse*; load_refusal_name), 0 for none
}

// THE LOAD'S REFUSALS BY THE ENGINE'S NAME AND NUMBER (gotable's
// TableRefuseReason; docs/SPEC-TABLES.md §3, §3.3, §6.5): a form byte other
// than 1 is refused, form 2 by name because a message is never a file's own
// form. report.reason holds the refusal the read itself names; load_error
// answers the strict read's one reason for any load.
let LoadRefuseOk: i64 = 0;
let LoadRefuseUnknownForm: i64 = 9;
let LoadRefuseCountOverExtentCap: i64 = 11;
let LoadRefuseWireDamaged: i64 = 16;
let LoadRefuseMalformed: i64 = 17;
let LoadRefuseOutOfRange: i64 = 18;
let LoadRefuseKindMismatch: i64 = 19;
let LoadRefuseIntegrityMismatch: i64 = 22;
let LoadRefuseNonFiniteFloat: i64 = 23;
let LoadRefuseBlobShared: i64 = 24;
let LoadRefuseNewerForm: i64 = 26;
let LoadRefuseMessageFormAsFile: i64 = 27;

// load_refusal_name is a refusal's name, the string the engine's error gives.
fn load_refusal_name(code: i64) -> const u8[:] {
    return match code {
        LoadRefuseUnknownForm => "unknown_form",
        LoadRefuseCountOverExtentCap => "count_over_extent_cap",
        LoadRefuseWireDamaged => "wire_damaged",
        LoadRefuseMalformed => "malformed",
        LoadRefuseOutOfRange => "out_of_range",
        LoadRefuseKindMismatch => "kind_mismatch",
        LoadRefuseIntegrityMismatch => "integrity_mismatch",
        LoadRefuseNonFiniteFloat => "non_finite_float",
        LoadRefuseBlobShared => "blob_shared",
        LoadRefuseNewerForm => "newer_form",
        LoadRefuseMessageFormAsFile => "message_form_as_file",
        _ => "",
    };
}

// load_error is THE STRICT READ'S ONE REASON for a load whose report is
// `report` and whose answer was `root` (docs/SPEC-TABLES.md §6.5; gotable's
// TableReport.Err and <T>ReadStrict, in their order): a non-finite float,
// then a form byte this reader does not carry (unknown_form, whose report
// names which), a failed integrity trailer, damage the verdict names
// (wire_damaged) apart from a malformed body (malformed), then a builder's
// count past the int32 storage cap (count_over_extent_cap, §2.9), then
// blob_shared, oor and kind_mismatch. A load that read nothing whole (no
// root, or a walk that stopped) and whose report names none of them is
// wire_damaged, as ReadStrict answers it. It is
// 0 for a load the strict read keeps: a clean one, or one whose only events
// are unknown, widened, repeated or retained.
fn load_error(report: const LoadReport&, root: i64) -> i64 {
    if report.non_finite { return LoadRefuseNonFiniteFloat; }
    if report.reason == LoadRefuseNewerForm || report.reason == LoadRefuseMessageFormAsFile { return LoadRefuseUnknownForm; }
    if report.reason == LoadRefuseIntegrityMismatch { return LoadRefuseIntegrityMismatch; }
    if report.damaged { return LoadRefuseWireDamaged; }
    if report.body_damaged { return LoadRefuseMalformed; }
    if report.count_refused { return LoadRefuseCountOverExtentCap; }
    if report.blob_shared > 0 { return LoadRefuseBlobShared; }
    if report.oor > 0 { return LoadRefuseOutOfRange; }
    if report.mismatch > 0 { return LoadRefuseKindMismatch; }
    if root < 0 || report.stopped { return LoadRefuseWireDamaged; }
    return LoadRefuseOk;
}

// KeyDesc is the key enum of one enum-keyed array (docs/SPEC-TABLES.md §3.2):
// the owner table, the field's ordinal and the key variants in declared order.
struct KeyDesc { owner: const u8[:], ordinal: i32, variants: const VariantDesc[:] }

// DeprecatedVariant is one enum variant retired in place (docs/SPEC-TABLES.md
// §2.15): the enum's name, as a field descriptor's ref names it, and the
// variant's table-wire id. A VariantDesc does not carry `| deprecated`, so the
// unit does (docs/PORT-GOOSE.md, THE LOADED VALUE).
struct DeprecatedVariant { enum_name: const u8[:], id: u64 }

// GuardDesc is one condition of a guarded field (SPEC §4.5; the if/else
// branches of a packet type a table reaches): the field at `ordinal` of table
// `owner` is written only where the bool field at `cond` holds `want`. A
// field under nested branches has one row per enclosing condition. A read
// never consults a guard (docs/SPEC-TABLES.md §3); a write does.
struct GuardDesc { owner: const u8[:], ordinal: i32, cond: i32, want: bool }

// LoadUnit is what a load resolves names against: every table descriptor
// (tables, map entries, packet types a table reaches), every union, the key
// enums of the enum-keyed arrays, the deprecated enum variants and the
// guards. The generated `TableUnit` of every unit is one
// (internal/codegen/goosetable/tablewire.go).
struct LoadUnit {
    tables: const TableDesc[:],
    unions: const UnionDesc[:],
    keys: const KeyDesc[:],
    deprecated: const DeprecatedVariant[:],
    guards: const GuardDesc[:],
    known: const u64[:],      // every id this unit can spell, ascending (§6.6)
}

// LoadReg, a row of THE LOAD DISPATCH TABLE, is declared in
// schema_tables.goose beside the `load_registry` it types, so that module
// checks on its own (make/goose-strict.sh).

// LoadVal is one loaded value (docs/PORT-GOOSE.md, THE LOADED VALUE). Every
// shape uses the same record:
//   bits   an integer's value (sign-extended to 128 bits on a signed kind), a
//          bool's 0 or 1, an enum's variant ordinal (0 = None), a union's tag,
//          a pointer's node index, a float's IEEE bits (f32 in the low 32)
//   off    a text, bytes or wstring value: a VIEW into the input, nothing
//   len    copied; len is the view's byte length, or a collection's live count
//   first  the first child slot: a record's fields in descriptor order, an
//   count  array's elements, a map's entries, a union's one payload slot
//   tab    a record's table index in the LoadUnit, a union's union index
//   made   the record or collection has its child slots
struct LoadVal {
    state: u8,
    shape: u8,
    made: bool,
    tab: i64,
    bits: Int128,
    off: i64,
    len: i64,
    first: i64,
    count: i64,
}

// LoadNode is one node record of the flat node table (docs/SPEC-TABLES.md
// §3.1): wire index i + 2. tab is the record's table, -1 for a record this
// root cannot place (counted unknown, every pointer naming it reads null);
// blob is KindString or KindArray for a *string or *bytes record, its bytes a
// view (off, len); val is the record's LoadVal.
struct LoadNode { tab: i64, blob: u8, off: i64, len: i64, val: i64, named: bool }

// Rd is one reader's window over the input: [off, end).
struct Rd { off: i64, end: i64 }

// LoadCtx is the walk's context of borrowed tables (the Goose spec's struct-of-references pattern).
struct LoadCtx {
    data: const u8[:],
    unit: LoadUnit,
    vals: LoadVal[>..]&,
    nodes: LoadNode[>..]&,
    report: LoadReport&,
    ids_at: i64,     // the id table's first entry (docs/SPEC-TABLES.md §3)
    ids_n: i64,
    numbered: bool,  // a variable-class read: the node table is this reader's
    good: bool,      // the node table read whole (§3.1)
    root_tab: i64,
    root_val: i64,
    cap: bool,       // §2.9: a count past int32 refuses rather than damages
    node_n: i64,     // the records the scan confirmed (0 when it failed)
    retain: RetainStore&, // §6.6: the caller's stores, when retaining
    retaining: bool,
}

// ---- the wire's number shapes (docs/SPEC-TABLES.md §3) ----

fn has(r: Rd&, n: i64) -> bool { n >= 0 && r.off + n <= r.end }

fn fits(r: Rd&, n: u64) -> bool { n <= (r.end - r.off) as! u64 }

fn le_at(data: const u8[:], at: i64, w: i64) -> u64 {
    var v: u64 = 0;
    for i in w {
        let b: u64 = data[at + i];
        v |= b << ((8 * i) as! u64);
    }
    return v;
}

fn rd_u8(c: LoadCtx&, r: Rd&) -> u8 {
    let v = c.data[r.off];
    r.off += 1;
    return v;
}

fn rd_le(c: LoadCtx&, r: Rd&, w: i64) -> u64 {
    let v = le_at(c.data, r.off, w);
    r.off += w;
    return v;
}

// leb_at decodes one unsigned LEB128 in its ONE legal spelling (§3): ok is
// false where it runs past end or ten bytes, or is non-minimal.
fn leb_at(data: const u8[:], off: i64, end: i64) -> u64, i64, bool {
    var v: u64 = 0;
    var shift: u64 = 0;
    var o = off;
    for i in 10 {
        if o >= end { return 0, o, false; }
        let b = data[o];
        o += 1;
        if i == 9 && b > 1 { return 0, o, false; }
        let low: u64 = b & 0x7F;
        v |= low << shift;
        if (b & 0x80) == 0 {
            if i > 0 && b == 0 { return 0, o, false; }
            return v, o, true;
        }
        shift += 7;
    }
    return 0, o, false;
}

fn leb(c: LoadCtx&, r: Rd&) -> u64, bool {
    let v, next, ok = leb_at(c.data, r.off, r.end);
    if ok { r.off = next; }
    return v, ok;
}

// idof resolves a 1-based reference against the id table (§3); reference 0
// names no id and a reference above the count is damage.
fn idof(c: LoadCtx&, ref: u64) -> u64, bool {
    if ref == 0 || ref > c.ids_n as! u64 { return 0, false; }
    return le_at(c.data, c.ids_at + ((ref - 1) as! i64) * 8, 8), true;
}

// kind_width is a fixed-width kind's payload width (§3), 0 for the rest.
fn kind_width(kind: u8) -> i64 {
    return match kind {
        1, 2, 6, 20, 25 => 1,
        3, 7, 21, 26 => 2,
        4, 8, 10, 17, 22, 27 => 4,
        5, 9, 11, 23, 28, 36 => 8,
        18, 19, 24, 29 => 16,
        _ => 0,
    };
}

fn kind_signed(kind: u8) -> bool {
    return match kind { 2, 3, 4, 5, 18, 20, 21, 22, 23, 24 => true, _ => false };
}

fn kind_wide(kind: u8) -> bool { kind >= 18 && kind <= 29 }

// widens is §4's ladder: a kind that GREW since the writer (ir.TableKindWidens).
fn widens(kind: u8, declared: u8) -> bool {
    return match declared {
        3, 4, 5 => kind >= 2 && kind < declared,
        18 => kind >= 2 && kind <= 5,
        7, 8, 9 => kind >= 6 && kind < declared,
        19 => kind >= 6 && kind <= 9,
        11 => kind == 10,
        _ => false,
    };
}

// skip steps past a field this reader cannot name, by its kind alone (§3).
fn skip(c: LoadCtx&, r: Rd&, kind: u8) -> bool {
    if (kind >= 1 && kind <= 11) || (kind >= 18 && kind <= 29) || kind == 36 {
        let w = kind_width(kind);
        if !has(r, w) { return false; }
        r.off += w;
        return true;
    }
    if kind == 17 || kind == 30 {
        let v, ok = leb(c, r);
        return ok;
    }
    if kind == 12 || kind == 13 || kind == 14 || kind == 16 || kind == 31 || kind == 32 || kind == 33 {
        let n, ok = leb(c, r);
        if !ok || !fits(r, n) { return false; }
        r.off += n as! i64;
        return true;
    }
    if kind == 15 {
        let arm, ok = leb(c, r);
        if !ok { return false; }
        if arm == 0 { return true; }
        if !has(r, 1) { return false; }
        r.off += 1;
        let n, good = leb(c, r);
        if !good || !fits(r, n) { return false; }
        r.off += n as! i64;
        return true;
    }
    // kind 34 is reserved for float16 and a kind no reader knows is damage
    return false;
}

// ---- text (docs/SPEC-TABLES.md §3, §4) ----

// text_valid: well-formed UTF-8 with no zero byte, checked as it arrives.
fn text_valid(data: const u8[:], off: i64, n: i64) -> bool {
    var i = off;
    let end = off + n;
    while i < end {
        let b = data[i];
        if b == 0 { return false; }
        if b < 0x80 { i += 1; continue; }
        var need: i64 = 0;
        var cp: u32 = 0;
        if b >= 0xC2 && b <= 0xDF { need = 1; cp = b & 0x1F; }
        else if b >= 0xE0 && b <= 0xEF { need = 2; cp = b & 0x0F; }
        else if b >= 0xF0 && b <= 0xF4 { need = 3; cp = b & 0x07; }
        else { return false; }
        if i + need >= end { return false; }
        for k in 1..need + 1 {
            let x = data[i + k];
            if (x & 0xC0) != 0x80 { return false; }
            let low: u32 = x & 0x3F;
            cp = (cp << 6) | low;
        }
        if need == 2 && (cp < 0x800 || (cp >= 0xD800 && cp <= 0xDFFF)) { return false; }
        if need == 3 && (cp < 0x10000 || cp > 0x10FFFF) { return false; }
        i += need + 1;
    }
    return true;
}

// text_boundary: a clamp cuts at the last whole code point within bound.
fn text_boundary(data: const u8[:], off: i64, n: i64, bound: i64) -> i64 {
    if n <= bound { return n; }
    var cut = bound;
    while cut > 0 && (data[off + cut] & 0xC0) == 0x80 { cut -= 1; }
    return cut;
}

fn unit_at(data: const u8[:], at: i64) -> u16 {
    let lo: u16 = data[at];
    let hi: u16 = data[at + 1];
    return lo | (hi << 8);
}

// wide_valid: kind 33's rule — surrogates paired, no zero code unit (§3).
fn wide_valid(data: const u8[:], off: i64, units: i64) -> bool {
    var i: i64 = 0;
    while i < units {
        let u = unit_at(data, off + 2 * i);
        if u == 0 { return false; }
        if u >= 0xD800 && u <= 0xDBFF {
            if i + 1 >= units { return false; }
            let low = unit_at(data, off + 2 * (i + 1));
            if low < 0xDC00 || low > 0xDFFF { return false; }
            i += 1;
        } else if u >= 0xDC00 && u <= 0xDFFF {
            return false;
        }
        i += 1;
    }
    return true;
}

// wide_boundary: a clamp never splits a surrogate pair (§3).
fn wide_boundary(data: const u8[:], off: i64, units: i64, bound: i64) -> i64 {
    if units <= bound { return units; }
    var cut = bound;
    if cut > 0 {
        let last = unit_at(data, off + 2 * (cut - 1));
        if last >= 0xD800 && last <= 0xDBFF { cut -= 1; }
    }
    return cut;
}

// ---- numbers (docs/PORT-GOOSE.md, THE NUMBER MODEL and FLOAT32) ----

fn i128_of(v: i64) -> Int128 {
    let hi: u64 = if v < 0 { 0xFFFFFFFFFFFFFFFF } else { 0 };
    return Int128 { lo: v as! u64, hi: hi };
}

// cmp128 orders two 128-bit values, each read signed or unsigned.
fn cmp128(a: Int128, a_signed: bool, b: Int128, b_signed: bool) -> i64 {
    let an = a_signed && (a.hi >> 63) == 1;
    let bn = b_signed && (b.hi >> 63) == 1;
    if an != bn { return if an { -1 } else { 1 }; }
    if a.hi != b.hi { return if a.hi < b.hi { -1 } else { 1 }; }
    if a.lo != b.lo { return if a.lo < b.lo { -1 } else { 1 }; }
    return 0;
}

// shl128 shifts left by n < 128; Goose masks a shift count, so 0 and 64 are
// guarded.
fn shl128(v: Int128, n: i64) -> Int128 {
    if n <= 0 { return v; }
    if n >= 64 { return Int128 { lo: 0, hi: v.lo << ((n - 64) as! u64) }; }
    let s = n as! u64;
    return Int128 { lo: v.lo << s, hi: (v.hi << s) | (v.lo >> (64 - s)) };
}

fn pow2(k: i64) -> f64 {
    var v: f64 = 1.0;
    if k >= 0 { for i in k { v = v * 2.0; } }
    else { for i in -k { v = v * 0.5; } }
    return v;
}

// f32_value is a float32's value from its bits, exactly, by arithmetic: Goose
// has no bits-to-float cast, and the clamp of a compressed float compares the
// value (NaN is handled by the caller on the bits).
fn f32_value(bits: u32) -> f64 {
    let neg = (bits >> 31) == 1;
    let e: i64 = (bits >> 23) & 0xFF;
    let m: i64 = bits & 0x7FFFFF;
    var v: f64 = 0.0;
    if e == 0xFF { v = 1.0 / 0.0; }
    else if e == 0 { v = m * pow2(-149); }
    else { v = (m + 0x800000) * pow2(e - 150); }
    return if neg { -v } else { v };
}

// f32_bits is float-to-bits through a one-element view (PORT-GOOSE FLOAT32).
fn f32_bits(x: f32) -> u32 {
    var a: f32[1] = [x];
    let b = bytes_of(a);
    let b0: u32 = b[0];
    let b1: u32 = b[1];
    let b2: u32 = b[2];
    let b3: u32 = b[3];
    return b0 | (b1 << 8) | (b2 << 16) | (b3 << 24);
}

// widen_f32 is the float64 bits of a float32's bits, exact, NaN payload kept
// as the Go reference's widenF32NaN keeps it (docs/SPEC-TABLES.md §4).
fn widen_f32(bits: u32) -> u64 {
    let sign: u64 = (bits >> 31);
    let s = sign << 63;
    let e: u64 = (bits >> 23) & 0xFF;
    var m: u64 = bits & 0x7FFFFF;
    if e == 0xFF { return s | 0x7FF0000000000000 | (m << 29); }
    if e == 0 {
        if m == 0 { return s; }
        var ex: u64 = 1023 - 126;
        while (m & 0x800000) == 0 { m = m << 1; ex -= 1; }
        return s | (ex << 52) | ((m & 0x7FFFFF) << 29);
    }
    return s | ((e + 1023 - 127) << 52) | (m << 29);
}

// ---- the flat value table ----

fn reserve(c: LoadCtx&, n: i64) -> i64 {
    let first = c.vals.len;
    for i in n { c.vals.push(LoadVal { .. }); }
    return first;
}

fn table_index(c: LoadCtx&, name: const u8[:]) -> i64 {
    for t, i in c.unit.tables { if t.name == name { return i; } }
    return -1;
}

fn union_index(c: LoadCtx&, name: const u8[:]) -> i64 {
    for u, i in c.unit.unions { if u.name == name { return i; } }
    return -1;
}

// clear_fields resets a record's fields to their descriptor defaults (the Go
// reference's Refill): a damaged body falls back on its defaults (§3).
fn clear_fields(c: LoadCtx&, rec: i64) {
    let t = c.vals[rec].tab;
    let first = c.vals[rec].first;
    for i in c.vals[rec].count {
        c.vals[first + i] = LoadVal { shape: c.unit.tables[t].fields[i].shape, .. };
    }
}

// make_record gives slot `v` the field slots of table t, or resets the ones
// it has: a body is the same storage across occurrences (§3).
fn make_record(c: LoadCtx&, v: i64, t: i64) {
    // a NEW OCCURRENCE of a body: what the earlier one retained dies with it
    if c.retaining && c.vals[v].made { forget_rec(c, v); }
    if c.vals[v].made && c.vals[v].tab == t {
        clear_fields(c, v);
        return;
    }
    let n = c.unit.tables[t].fields.len;
    let first = reserve(c, n);
    c.vals[v].made = true;
    c.vals[v].tab = t;
    c.vals[v].first = first;
    c.vals[v].count = n;
    clear_fields(c, v);
}

// fresh_record pushes a new record of table t and returns its slot.
fn fresh_record(c: LoadCtx&, t: i64) -> i64 {
    let v = reserve(c, 1);
    c.vals[v].shape = ShapeTab;
    make_record(c, v, t);
    return v;
}

// make_slots gives a collection slot n element slots (made once: a fixed,
// counted or keyed array keeps its storage across occurrences).
fn make_slots(c: LoadCtx&, v: i64, n: i64, shape: u8) {
    if c.vals[v].made { return; }
    let first = reserve(c, n);
    for i in n { c.vals[first + i].shape = shape; }
    c.vals[v].made = true;
    c.vals[v].first = first;
    c.vals[v].count = n;
}

// ride marks a value that rode: an optional is PRESENT (§2.3).
fn ride(c: LoadCtx&, v: i64) {
    if c.vals[v].state == ValAbsent { c.vals[v].state = ValEmpty; }
}

fn set_present(c: LoadCtx&, v: i64) { c.vals[v].state = ValPresent; }

// field_kind is the kind byte a field's payload rides under: a pointer is
// kind 17 (its descriptor says so), bytes(N) is an array, a payload-free arm
// is kind 32 (docs/SPEC-TABLES.md §3, §3.1).
fn field_kind(d: FieldDesc) -> u8 {
    if d.shape == ShapeBytes { return KindArray; }
    if d.shape == ShapeVoid { return KindNoPayload; }
    return d.kind;
}

// collection_cap is §2.9's int32 storage cap: a builder refuses its partial
// value; a reader with no builder reports damage and resumes after L.
fn collection_cap(c: LoadCtx&) -> bool {
    if !c.cap { c.report.body_damaged = true; return true; }
    c.report.count_refused = true;
    return false;
}

// ---- scalars (docs/SPEC-TABLES.md §3, §4, §15) ----

// scalar_at decodes one fixed-width payload that arrived under `kind` — the
// declared kind or one it widens (§4) — at the WIRE kind's width and
// signedness, then takes the declaration's clamps. Events go to rep, which is
// the load's report, or a throwaway one for the map key scan.
fn scalar_at(c: LoadCtx&, rep: LoadReport&, r: Rd&, out: LoadVal&, d: FieldDesc, declared: u8, kind: u8) -> bool {
    let width = kind_width(kind);
    if !has(r, width) { rep.body_damaged = true; return false; }
    out.state = ValPresent;
    if kind == KindBool {
        let b = rd_u8(c, r);
        if b > 1 { rep.body_damaged = true; }
        out.bits = Int128 { lo: if b != 0 { 1 } else { 0 }, hi: 0 };
        return true;
    }
    if kind == KindF32 {
        let bits = rd_le(c, r, 4) as! u32;
        if (bits & 0x7F800000) == 0x7F800000 {
            // A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the bits
            // become a value (docs/SPEC-TABLES.md §3.3, "same rule as
            // packets"), a compressed float included; the walk goes on so the
            // framing verdict is a finite wire's, and load_table answers the
            // refusal at its end
            rep.non_finite = true;
            out.state = ValAbsent;
            return true;
        }
        if declared == KindF64 {
            // f32 WIDENED into f64 (§4): exact, and no range clamps here
            out.bits = Int128 { lo: widen_f32(bits), hi: 0 };
            return true;
        }
        var keep: u32 = bits;
        if d.frange {
            let v = f32_value(bits);
            if v < d.flo { keep = f32_bits(d.flo as f32); rep.oor += 1; }
            else if v > d.fhi { keep = f32_bits(d.fhi as f32); rep.oor += 1; }
            else if v == 0.0 { keep = 0; } // a -0 lands as +0, uncounted (§3.3)
        }
        out.bits = Int128 { lo: keep, hi: 0 };
        return true;
    }
    if kind == KindF64 {
        let bits = rd_le(c, r, 8);
        if (bits & 0x7FF0000000000000) == 0x7FF0000000000000 {
            rep.non_finite = true; // §3.3, as kind 10 above
            out.state = ValAbsent;
            return true;
        }
        out.bits = Int128 { lo: bits, hi: 0 };
        return true;
    }
    let signed = kind_signed(kind);
    let declared_signed = kind_signed(declared);
    if kind_wide(kind) {
        // the 128-bit and FIXED-POINT kinds (ir.TableKindWide, 18..29): the
        // payload at the kind's own width, extended by its signedness, then
        // the declared range on the raw scale (tabletext.WideFromBytes)
        var raw = Int128 { lo: 0, hi: 0 };
        if width == 16 {
            raw.lo = rd_le(c, r, 8);
            raw.hi = rd_le(c, r, 8);
        } else {
            let u = rd_le(c, r, width);
            var v = u as! i64;
            if signed && width < 8 {
                let shift = (64 - width * 8) as! u64;
                v = ((u << shift) as! i64) >> shift;
            }
            raw = if signed { i128_of(v) } else { Int128 { lo: u, hi: 0 } };
        }
        out.bits = clamp_wide(rep, raw, signed, d);
        return true;
    }
    let raw = rd_le(c, r, width);
    var value = raw as! i64;
    if signed && width < 8 {
        let shift = (64 - width * 8) as! u64;
        value = ((raw << shift) as! i64) >> shift;
    }
    if kind_wide(declared) {
        // an integer kind WIDENED into a 128-bit declaration (§4): sixteen
        // bytes extended by the WIRE kind's signedness (a uint64 of all ones
        // is 2^64 - 1, never -1), then read as the declared kind and held to
        // its range on the raw scale (tablewire.decode's widen)
        let ext = if signed { i128_of(value) } else { Int128 { lo: raw, hi: 0 } };
        out.bits = clamp_wide(rep, ext, declared_signed, d);
        return true;
    }
    var uraw = raw;
    if d.ranged {
        let lo = d.lo.lo as! i64;
        let hi = d.hi.lo as! i64;
        if signed {
            if value < lo { value = lo; rep.oor += 1; }
            else if value > hi { value = hi; rep.oor += 1; }
        } else {
            if uraw < (lo as! u64) { uraw = lo as! u64; rep.oor += 1; }
            else if uraw > (hi as! u64) { uraw = hi as! u64; rep.oor += 1; }
            value = uraw as! i64;
        }
    }
    if d.bits > 0 && d.bits < width * 8 {
        let max = ((1 as u64) << (d.bits as! u64)) - 1;
        if (value as! u64) > max { value = max as! i64; rep.oor += 1; }
    }
    out.bits = i128_of(value);
    return true;
}

// clamp_wide is the declared range on the RAW scale (ir.TableRawRange): a
// fixed field's whole-unit bounds shifted by F (§4, §15).
fn clamp_wide(rep: LoadReport&, raw: Int128, raw_signed: bool, d: FieldDesc) -> Int128 {
    if !d.ranged { return raw; }
    var lo = d.lo;
    var hi = d.hi;
    if d.ibits != 0 || d.fbits != 0 {
        lo = shl128(lo, d.fbits);
        hi = shl128(hi, d.fbits);
    }
    if cmp128(raw, raw_signed, lo, d.signed) < 0 { rep.oor += 1; return lo; }
    if cmp128(raw, raw_signed, hi, d.signed) > 0 { rep.oor += 1; return hi; }
    return raw;
}

// ---- pointers (docs/SPEC-TABLES.md §3.1, §2.5) ----

// resolve places one node index in a pointer slot: bounds-checked and stored,
// never followed. Every failure leaves the pointer null.
fn resolve(c: LoadCtx&, v: i64, d: FieldDesc, index: u64) {
    c.vals[v].state = ValEmpty;
    c.vals[v].bits = Int128 { lo: 0, hi: 0 };
    if !c.numbered || !c.good { return; }
    let blob = d.shape == ShapeSBlob || d.shape == ShapeBBlob;
    if index == 0 { return; }
    if index == 1 {
        // the root carries no record: the reader's own root type is checked
        if blob || c.unit.tables[c.root_tab].name != d.ref { c.report.mismatch += 1; return; }
        c.vals[v].state = ValPresent;
        c.vals[v].bits = Int128 { lo: 1, hi: 0 };
        return;
    }
    if index - 2 >= c.node_n as! u64 { c.report.body_damaged = true; return; }
    let n = (index - 2) as! i64;
    if c.nodes[n].tab < 0 && c.nodes[n].blob == 0 { return; }
    if blob {
        let want = if d.shape == ShapeSBlob { KindString } else { KindArray };
        if c.nodes[n].blob != want { c.report.mismatch += 1; return; }
        c.vals[v].state = ValPresent;
        c.vals[v].bits = Int128 { lo: index, hi: 0 };
        if c.nodes[n].named { c.report.blob_shared += 1; } else { c.nodes[n].named = true; }
        return;
    }
    if c.nodes[n].tab < 0 || c.unit.tables[c.nodes[n].tab].name != d.ref {
        c.report.mismatch += 1;
        return;
    }
    c.vals[v].state = ValPresent;
    c.vals[v].bits = Int128 { lo: index, hi: 0 };
}

// ---- the walk ----

// body_at decodes one table body (docs/SPEC-TABLES.md §3, §4) into record
// `rec` of table `tab`. nested: a reserved node id here is malformed (§3.1).
// entry: the body is a map entry, whose key widening the map counts (§2.8).
recursive fn body_at(c: LoadCtx&, r: Rd&, tab: i64, rec: i64, nested: bool, entry: bool) -> bool {
    loop {
        if c.report.count_refused { return false; }
        let ref, ok = leb(c, r);
        if !ok { c.report.body_damaged = true; return false; }
        if ref == 0 { return true; } // the body ends at its own zero reference
        let id, named = idof(c, ref);
        if !named { c.report.body_damaged = true; return false; }
        if !has(r, 1) { c.report.body_damaged = true; return false; }
        let kind = rd_u8(c, r);
        if id == BuildVersionWireId || id == VocabularyWireId {
            c.report.body_damaged = true; // reserved ids ride the announcement only (§3.3)
            return false;
        }
        if id == NodeWireId {
            if nested { c.report.body_damaged = true; return false; }
            if c.numbered && c.unit.tables[tab].variable {
                if !skip(c, r, kind) { c.report.body_damaged = true; return false; }
                continue;
            }
        }
        var i: i64 = -1;
        for f, j in c.unit.tables[tab].fields {
            if f.id == id { i = j; break; }
        }
        if i < 0 {
            c.report.unknown += 1; // skipped by its length (§4)
            let from = r.off;
            if !skip(c, r, kind) { c.report.body_damaged = true; return false; }
            if c.retaining { capture(c, rec, id, kind, from, r.off); }
            continue;
        }
        let d = c.unit.tables[tab].fields[i];
        let slot = c.vals[rec].first + i;
        let want = field_kind(d);
        if kind != want {
            if widens(kind, want) {
                // WIDENED (§4): decoded at its own width, the value lands
                if !scalar_at(c, c.report, r, c.vals[slot], d, want, kind) { return false; }
                if !entry || i != 0 { c.report.widened += 1; }
                continue;
            }
            c.report.mismatch += 1;
            if !skip(c, r, kind) { c.report.body_damaged = true; return false; }
            continue;
        }
        if !load_shape(c, r, slot, d, tab) { return false; }
        if d.optional && d.shape != ShapeArr { ride(c, slot); }
    }
}

// ---- THE DISPATCH (schema_tables.goose, load_registry; docs/PORT-GOOSE.md,
// THE LOADED VALUE: the dispatch) ----

// loader_of looks a descriptor up in the registry: the first row whose shape,
// array kind (0: any) and wstring flag match. A shape no row registers reads
// as a fixed-width scalar, as a header kind it does not know is skipped by
// its width (docs/SPEC-TABLES.md §3).
fn loader_of(d: FieldDesc) -> Loader {
    for g in load_registry {
        if g.shape == d.shape && g.wide == d.wide && (g.array == 0 || g.array == d.array) { return g.loader; }
    }
    return Loader.Scalar;
}

// load_shape is THE FIELD POSITION's dispatch: the field's Loader, then its
// load_field case (docs/SPEC-TABLES.md §3: a field carries its own payload).
fn load_shape(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_field(loader_of(d), c, r, slot, d, owner);
}

// load_field: one case per registered kind, a FIELD's payload (§3).
fn load_field(k: Loader.Scalar, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return scalar_at(c, c.report, r, c.vals[slot], d, field_kind(d), field_kind(d));
}
fn load_field(k: Loader.Pointer, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_pointer(c, r, slot, d);
}
fn load_field(k: Loader.Text, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_text(c, r, slot, d);
}
fn load_field(k: Loader.Wide, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_wide(c, r, slot, d);
}
fn load_field(k: Loader.Enum, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_enum(c, r, slot, d, false);
}
fn load_field(k: Loader.Body, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_body(c, r, slot, d.ref);
}
fn load_field(k: Loader.Array, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    let ok, selected = load_array(c, r, slot, d, -1);
    return ok;
}
fn load_field(k: Loader.Keyed, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_keyed(c, r, slot, d, owner);
}
fn load_field(k: Loader.Map, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_map(c, r, slot, d, owner);
}
fn load_field(k: Loader.Union, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    return load_union(c, r, slot, d.ref);
}
fn load_field(k: Loader.Void, c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    // no field is payload-free: the header's own kind decides, as a scalar
    return scalar_at(c, c.report, r, c.vals[slot], d, field_kind(d), field_kind(d));
}

// load_pointer: kind 17's payload is a NODE INDEX (§3.1).
fn load_pointer(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc) -> bool {
    let index, ok = leb(c, r);
    if !ok { c.report.body_damaged = true; return false; }
    resolve(c, slot, d, index);
    return true;
}

// load_text: kind 12 (§3): ill-formed text reads the default and counts
// malformed; a clamp cuts at a code point boundary and counts.
fn load_text(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc) -> bool {
    let n, ok = leb(c, r);
    if !ok || !fits(r, n) { c.report.body_damaged = true; return false; }
    let len = n as! i64;
    if !text_valid(c.data, r.off, len) {
        c.report.body_damaged = true;
        c.vals[slot].state = ValAbsent;
        r.off += len;
        return true;
    }
    var keep = len;
    if d.shape == ShapeStr && keep > d.bound {
        keep = text_boundary(c.data, r.off, len, d.bound);
        c.report.oor += 1;
    }
    c.vals[slot].state = ValPresent;
    c.vals[slot].off = r.off;
    c.vals[slot].len = keep;
    r.off += len;
    return true;
}

// load_wide: kind 33 (§3): an odd L or ill-formed units read the default.
fn load_wide(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc) -> bool {
    let n, ok = leb(c, r);
    if !ok || !fits(r, n) { c.report.body_damaged = true; return false; }
    let len = n as! i64;
    let at = r.off;
    r.off += len;
    if (len & 1) != 0 || !wide_valid(c.data, at, len / 2) {
        c.report.body_damaged = true;
        c.vals[slot].state = ValAbsent;
        return true;
    }
    var keep = len / 2;
    if keep > d.bound {
        keep = wide_boundary(c.data, at, len / 2, d.bound);
        c.report.oor += 1;
    }
    c.vals[slot].state = ValPresent;
    c.vals[slot].off = at;
    c.vals[slot].len = keep * 2;
    return true;
}

// load_enum: kind 30's payload is the reference to the VARIANT NAME's id; a
// variant this reader cannot name reads None and counts unknown (§4, §5). A
// DEPRECATED variant (§2.15, READ) is never unknown: a field reads its
// declared default (None when it declares none), an element (`element`) the
// ELEMENT's default, None — the Go reference's enumCell.
fn load_enum(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, element: bool) -> bool {
    let ref, ok = leb(c, r);
    if !ok { c.report.body_damaged = true; return false; }
    c.vals[slot].bits = Int128 { lo: 0, hi: 0 };
    if ref == 0 { c.vals[slot].state = ValEmpty; return true; }
    let id, named = idof(c, ref);
    if !named { c.report.body_damaged = true; return false; }
    var v: u64 = 0;
    for vd, i in d.variants {
        if vd.id == id { v = (i + 1) as! u64; break; }
    }
    if v == 0 {
        c.vals[slot].state = ValEmpty;
        c.report.unknown += 1;
        if c.retaining { c.report.retain_lost += 1; } // an excluded class (§6.6)
        return true;
    }
    if variant_deprecated(c, d.ref, id) {
        if !element && d.has_def {
            c.vals[slot].state = ValPresent;
            c.vals[slot].bits = d.def_i;
        } else {
            c.vals[slot].state = ValEmpty;
        }
        return true;
    }
    c.vals[slot].state = ValPresent;
    c.vals[slot].bits = Int128 { lo: v, hi: 0 };
    return true;
}

// variant_deprecated: the unit marks this enum's variant `| deprecated`
// (docs/SPEC-TABLES.md §2.15).
fn variant_deprecated(c: LoadCtx&, name: const u8[:], id: u64) -> bool {
    for dv in c.unit.deprecated {
        if dv.id == id && dv.enum_name == name { return true; }
    }
    return false;
}

// load_body: a table or packet type by value, kind 13 (§3): a body whose
// terminator is not the last byte of its L is damage and reads its defaults.
fn load_body(c: LoadCtx&, r: Rd&, slot: i64, name: const u8[:]) -> bool {
    let n, ok = leb(c, r);
    if !ok || !fits(r, n) { c.report.body_damaged = true; return false; }
    let len = n as! i64;
    let t = table_index(c, name);
    var sub = Rd { off: r.off, end: r.off + len };
    make_record(c, slot, t);
    c.vals[slot].state = ValPresent;
    body_at(c, sub, t, slot, true, false);
    if c.report.count_refused { return false; }
    if sub.off != sub.end {
        c.report.body_damaged = true;
        clear_fields(c, slot);
    }
    r.off += len;
    return true;
}

// load_array decodes an array payload (§3, §2.9): a field carries its own L,
// an arm passes the L it was framed by. ok false: framing damage stops the
// walk; selected false: another element kind, the field keeps its default and
// an arm holding it reads None.
recursive fn load_array(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, framed: i64) -> bool, bool {
    let bytes = d.shape == ShapeBytes;
    let list = d.array == ArrayList;
    let counted = bytes || d.array == ArrayCounted || list;
    var body_len = framed;
    if framed < 0 {
        let n, good = leb(c, r);
        if !good || !fits(r, n) { c.report.body_damaged = true; return false, false; }
        body_len = n as! i64;
    }
    if !has(r, body_len) { c.report.body_damaged = true; return false, false; }
    let end = r.off + body_len;
    if body_len >= 2 {
        var decoded: i64 = 0;
        let ek = rd_u8(c, r);
        let count, good = leb(c, r);
        if !good {
            c.report.body_damaged = true;
            r.off = end;
            if d.optional { ride(c, slot); }
            return true, false;
        }
        let want = if bytes { 6 } else { d.ek };
        if ek != want && !widens(ek, want) {
            c.report.mismatch += 1;
            r.off = end;
            return true, false;
        }
        // a widened element kind counts once the count is known to land: the
        // builder's count-cap refusal moves no counter (§2.9), while the
        // region read's damage still counts it
        let widened = ek != want;
        var keep = count;
        if list {
            if count > 0x7FFFFFFF {
                r.off = end;
                if widened && !c.cap { c.report.widened += 1; }
                return collection_cap(c), false;
            }
        } else if count > d.bound as! u64 {
            keep = d.bound as! u64;
            c.report.oor += 1;
        }
        if widened { c.report.widened += 1; }
        var sub = Rd { off: r.off, end: if end < r.off { r.off } else { end } };
        if bytes {
            let at = sub.off;
            var i: u64 = 0;
            while i < keep {
                if !has(sub, 1) { c.report.body_damaged = true; break; }
                sub.off += 1;
                decoded += 1;
                i += 1;
            }
            c.vals[slot].off = at;
            c.vals[slot].len = decoded;
        } else {
            let e = d.elem;
            guard e else { r.off = end; return true, true; }
            var eshape = e.shape;
            if list {
                // a fresh run, bounded by the bytes that can carry elements;
                // the earlier occurrence's elements die with their records
                if c.retaining && c.vals[slot].made {
                    for k in c.vals[slot].count { forget_rec(c, c.vals[slot].first + k); }
                }
                let room = (sub.end - sub.off + 1) as! u64;
                let n = (if keep < room { keep } else { room }) as! i64;
                let first = reserve(c, n);
                for k in n { c.vals[first + k].shape = eshape; }
                c.vals[slot].made = true;
                c.vals[slot].first = first;
                c.vals[slot].count = n;
            } else {
                make_slots(c, slot, d.bound, eshape);
            }
            // a text list's total keeps the longest prefix within it: the
            // element that would carry the sum past it drops with every
            // element after it, oor counted once (§2.13)
            var spent: i64 = 0;
            var i: i64 = 0;
            while (i as! u64) < keep && i < c.vals[slot].count {
                if !load_element(c, sub, slot, e, d.ek, i, ek) { break; }
                if d.text_total > 0 {
                    let es = c.vals[slot].first + i;
                    let n = text_bytes(c, es);
                    if n > d.text_total - spent {
                        if c.retaining { forget_rec(c, es); }
                        c.vals[es] = LoadVal { shape: eshape, .. };
                        c.report.oor += 1;
                        break;
                    }
                    spent += n;
                }
                decoded = i + 1;
                i += 1;
            }
            if list { c.vals[slot].len = decoded; }
        }
        if counted {
            if d.array == ArrayCounted {
                // the slots the previous occurrence made live are reset (§7.2),
                // and their retained records die with them (§6.6)
                let first = c.vals[slot].first;
                for k in decoded..c.vals[slot].len {
                    if c.retaining { forget_rec(c, first + k); }
                    c.vals[first + k] = LoadVal { shape: c.vals[first + k].shape, .. };
                }
            }
            c.vals[slot].len = decoded;
        }
        c.vals[slot].state = ValPresent;
    }
    if d.optional { ride(c, slot); }
    r.off = end; // excess elements and slack skip via the length
    return true, true;
}

// text_bytes is a text list element's byte length: its record's one value,
// 0 for an absent `?string(M)` element or a record the read did not make.
fn text_bytes(c: LoadCtx&, es: i64) -> i64 {
    if !c.vals[es].made { return 0; }
    let fs = c.vals[es].first;
    if c.vals[fs].state != ValPresent { return 0; }
    return c.vals[fs].len;
}

// load_element decodes element i of an array (§3): THE ELEMENT POSITION's
// dispatch, the element descriptor's Loader, then its load_item case.
fn load_element(c: LoadCtx&, r: Rd&, arr: i64, e: const FieldDesc&, declared: u8, i: i64, ek: u8) -> bool {
    let es = c.vals[arr].first + i;
    return load_item(loader_of(e), c, r, es, e, declared, ek);
}

// load_item: one case per registered kind, an ELEMENT's payload (§3): a union
// element carries its own arm header, a pointer element a node index, a
// scalar element decodes at the element kind the header carried.
fn load_item(k: Loader.Scalar, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return scalar_at(c, c.report, r, c.vals[es], e, declared, ek);
}
fn load_item(k: Loader.Pointer, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    let index, ok = leb(c, r);
    if !ok { c.report.body_damaged = true; return false; }
    resolve(c, es, e, index);
    return true;
}
fn load_item(k: Loader.Text, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return item_text(c, r, es, e);
}
fn load_item(k: Loader.Wide, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return item_text(c, r, es, e);
}
fn load_item(k: Loader.Enum, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return load_enum(c, r, es, e, true);
}
fn load_item(k: Loader.Body, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    if declared == KindString {
        let t = table_index(c, e.ref);
        make_record(c, es, t);
        c.vals[es].state = ValPresent;
        return item_text(c, r, c.vals[es].first, c.unit.tables[t].fields[0]);
    }
    return load_body(c, r, es, e.ref);
}
fn load_item(k: Loader.Array, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return scalar_at(c, c.report, r, c.vals[es], e, declared, ek); // no element is an array
}
fn load_item(k: Loader.Keyed, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return scalar_at(c, c.report, r, c.vals[es], e, declared, ek); // no element is an array
}
fn load_item(k: Loader.Map, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return scalar_at(c, c.report, r, c.vals[es], e, declared, ek); // no element is a map
}
fn load_item(k: Loader.Union, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    if !has(r, 1) { c.report.body_damaged = true; return false; }
    c.vals[es] = LoadVal { shape: ShapeUnion, .. };
    return load_union(c, r, es, e.ref);
}
fn load_item(k: Loader.Void, c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&, declared: u8, ek: u8) -> bool {
    return scalar_at(c, c.report, r, c.vals[es], e, declared, ek); // no element is payload-free
}

// item_text: a plain text list's element: its own L, then the text (§2.13).
fn item_text(c: LoadCtx&, r: Rd&, es: i64, e: const FieldDesc&) -> bool {
    let n, ok = leb(c, r);
    if !ok || !fits(r, n) { c.report.body_damaged = true; return false; }
    let len = n as! i64;
    if !text_valid(c.data, r.off, len) { c.report.body_damaged = true; return false; }
    var keep = len;
    if e.shape == ShapeStr && keep > e.bound {
        keep = text_boundary(c.data, r.off, len, e.bound);
        c.report.oor += 1;
    }
    c.vals[es].state = ValPresent;
    c.vals[es].off = r.off;
    c.vals[es].len = keep;
    r.off += len;
    return true;
}

// key_variants is the key enum of an enum-keyed array (§3.2).
fn key_variants(c: LoadCtx&, owner: i64, d: FieldDesc) -> const VariantDesc[:] {
    let name = c.unit.tables[owner].name;
    for k in c.unit.keys {
        if k.ordinal == d.ordinal && k.owner == name { return k.variants; }
    }
    return d.variants;
}

// load_keyed places an enum-keyed array's triples BY KEY REFERENCE (§3.2): a
// key this reader cannot name counts unknown; a key reference of 0 is damage.
fn load_keyed(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    let n, ok = leb(c, r);
    if !ok || !fits(r, n) { c.report.body_damaged = true; return false; }
    let len = n as! i64;
    let end = r.off + len;
    if len < 2 { r.off = end; return true; }
    let ek = rd_u8(c, r);
    let count, good = leb(c, r);
    if !good { c.report.body_damaged = true; r.off = end; return true; }
    if ek != d.ek {
        if !widens(ek, d.ek) { c.report.mismatch += 1; r.off = end; return true; }
        c.report.widened += 1;
    }
    let e = d.elem;
    guard e else { r.off = end; return true; }
    var sub = Rd { off: r.off, end: if end < r.off { r.off } else { end } };
    let keys = key_variants(c, owner, d);
    make_slots(c, slot, d.bound, e.shape);
    c.vals[slot].state = ValPresent;
    c.vals[slot].len = d.bound;
    var k: u64 = 0;
    while k < count {
        k += 1;
        let key, kg = leb(c, sub);
        if !kg { c.report.body_damaged = true; break; }
        if key == 0 { c.report.body_damaged = true; break; }
        let key_id, named = idof(c, key);
        if !named { c.report.body_damaged = true; break; }
        let el, lg = leb(c, sub);
        if !lg || !fits(sub, el) { c.report.body_damaged = true; break; }
        let elen = el as! i64;
        var at: i64 = -1;
        for kv, i in keys {
            if kv.id == key_id && i < d.bound { at = i; break; }
        }
        if at < 0 {
            c.report.unknown += 1; // a slot this reader cannot name
            if c.retaining { c.report.retain_lost += 1; } // an excluded class (§6.6)
            sub.off += elen;
            continue;
        }
        var elem = Rd { off: sub.off, end: sub.off + elen };
        let es = c.vals[slot].first + at;
        if e.shape == ShapeEnum {
            load_enum(c, elem, es, e, true);
        } else if e.shape == ShapeTab {
            let t = table_index(c, e.ref);
            make_record(c, es, t);
            c.vals[es].state = ValPresent;
            body_at(c, elem, t, es, true, false);
            if c.report.count_refused { return false; }
            if elem.off != elem.end { c.report.body_damaged = true; clear_fields(c, es); }
        } else {
            scalar_at(c, c.report, elem, c.vals[es], e, d.ek, ek);
        }
        sub.off += elen;
    }
    r.off = end; // unread triples and slack skip via the length
    return true;
}

// map_key_scan is THE KEY SCAN (§2.8): before an entry decodes, its body's
// headers are scanned for the key's id and the LAST occurrence is read. The
// key kind is the reader's declaration; a key under another kind is
// kind_bad. Events inside the scan are not the load's (a throwaway report).
fn map_key_scan(c: LoadCtx&, body: Rd, kd: FieldDesc, key: LoadVal&, kind_bad: bool&, widened: bool&) -> bool {
    var scan = body;
    var scratch = LoadReport { .. };
    let want = field_kind(kd);
    loop {
        let ref, ok = leb(c, scan);
        if !ok { return false; }
        if ref == 0 { return scan.off == scan.end; }
        let id, named = idof(c, ref);
        if !named || !has(scan, 1) { return false; }
        let kind = rd_u8(c, scan);
        if id == kd.id {
            if kind != want && widens(kind, want) {
                widened = true;
                kind_bad = false; // this occurrence replaces the earlier key kind
                if !scalar_at(c, scratch, scan, key, kd, want, kind) { return false; }
                continue;
            }
            kind_bad = kind != want;
            if kind_bad {
                if !skip(c, scan, kind) { return false; }
                continue;
            }
            if kd.shape == ShapeStr || kd.shape == ShapeText {
                let n, g = leb(c, scan);
                if !g || !fits(scan, n) { return false; }
                let len = n as! i64;
                if !text_valid(c.data, scan.off, len) { return false; }
                key.state = ValPresent;
                key.off = scan.off;
                key.len = len;
                scan.off += len;
                continue;
            }
            if !scalar_at(c, scratch, scan, key, kd, want, want) { return false; }
            continue;
        }
        if !skip(c, scan, kind) { return false; }
    }
}

// key_order: byte-wise then by length for a string key, numeric otherwise —
// signed where the declaration is (tabletext.MapKeyOrder).
fn key_order(c: LoadCtx&, kd: FieldDesc, a: LoadVal, b: LoadVal) -> i64 {
    if kd.shape == ShapeStr || kd.shape == ShapeText {
        let n = if a.len < b.len { a.len } else { b.len };
        for i in n {
            let x = c.data[a.off + i];
            let y = c.data[b.off + i];
            if x != y { return if x < y { -1 } else { 1 }; }
        }
        if a.len != b.len { return if a.len < b.len { -1 } else { 1 }; }
        return 0;
    }
    if kd.signed && kd.kind >= 2 && kd.kind <= 5 {
        let x = a.bits.lo as! i64;
        let y = b.bits.lo as! i64;
        if x != y { return if x < y { -1 } else { 1 }; }
        return 0;
    }
    if a.bits.lo != b.bits.lo { return if a.bits.lo < b.bits.lo { -1 } else { 1 }; }
    return 0;
}

// load_map decodes one map (docs/SPEC-TABLES.md §2.8): ascending keys, a
// repeated key last-wins and counts repeated, a descending key stops the map
// as malformed, a key past the bound drops its entry and counts oor, a
// key of another kind empties the map for one kind_mismatch.
fn load_map(c: LoadCtx&, r: Rd&, slot: i64, d: FieldDesc, owner: i64) -> bool {
    let n, ok = leb(c, r);
    if !ok || !fits(r, n) { c.report.body_damaged = true; return false; }
    let end = r.off + (n as! i64);
    if (n as! i64) >= 2 {
        let ek = rd_u8(c, r);
        let count, good = leb(c, r);
        if !good { c.report.body_damaged = true; r.off = end; return true; }
        if ek != KindTable { c.report.mismatch += 1; r.off = end; return true; }
        if count > 0x7FFFFFFF { r.off = end; return collection_cap(c); }
        var sub = Rd { off: r.off, end: if end < r.off { r.off } else { end } };
        // the entry table is <Owner><Slot>Entry (docs/PORT-GOOSE.md, THE TYPE MAPPING)
        let et = entry_index(c, c.unit.tables[owner].name, d.slot);
        if et < 0 { r.off = end; return true; }
        let kd = c.unit.tables[et].fields[0];
        let room = (sub.end - sub.off) as! u64;
        let cap = (if count < room { count } else { room }) as! i64;
        // Replacing the map retires the earlier entries' retained bodies.
        if c.retaining && c.vals[slot].made { forget_rec(c, slot); }
        let first = reserve(c, cap);
        for k in cap { c.vals[first + k].shape = ShapeTab; }
        c.vals[slot].made = true;
        c.vals[slot].first = first;
        c.vals[slot].count = cap;
        c.vals[slot].state = ValPresent;
        var landed: i64 = 0;
        var widened = false;
        var k: u64 = 0;
        while k < count {
            k += 1;
            let el, g = leb(c, sub);
            if !g || !fits(sub, el) { c.report.body_damaged = true; break; }
            let body = Rd { off: sub.off, end: sub.off + (el as! i64) };
            sub.off = body.end;
            var key = LoadVal { .. };
            var kind_bad = false;
            var entry_widened = false;
            let key_ok = map_key_scan(c, body, kd, key, kind_bad, entry_widened);
            if entry_widened && !widened { widened = true; c.report.widened += 1; }
            if kind_bad { c.report.mismatch += 1; landed = 0; break; }
            if !key_ok { c.report.body_damaged = true; break; }
            if (kd.shape == ShapeStr) && key.len > kd.bound {
                c.report.oor += 1; // keys never clamp: the entry drops whole
                continue;
            }
            var order: i64 = -1;
            if landed > 0 {
                let last = c.vals[first + landed - 1];
                let lk = c.vals[last.first];
                order = key_order(c, kd, lk, key);
            }
            if order > 0 { c.report.body_damaged = true; break; } // descending
            // the entry's body is read WHOLE now the key has chosen its place
            let at = if order == 0 { first + landed - 1 } else { first + landed };
            if at >= first + cap { c.report.body_damaged = true; break; }
            if c.retaining && c.vals[at].made { forget_rec(c, at); } // a repeated key's earlier entry
            c.vals[at] = LoadVal { shape: ShapeTab, .. };
            make_record(c, at, et);
            c.vals[at].state = ValPresent;
            var whole = body;
            body_at(c, whole, et, at, true, true);
            if c.report.count_refused { return false; }
            if order == 0 { c.report.repeated += 1; continue; } // last wins whole
            landed += 1;
        }
        c.vals[slot].len = landed;
    }
    r.off = end; // the remaining entries skip by the map's L
    return true;
}

// entry_index finds a map's entry table, <Owner><Slot>Entry (docs/PORT-GOOSE.md,
// THE TYPE MAPPING): the field descriptor does not name it.
fn entry_index(c: LoadCtx&, owner: const u8[:], slot: const u8[:]) -> i64 {
    let tail = "Entry";
    for t, i in c.unit.tables {
        let n = t.name;
        if n.len != owner.len + slot.len + tail.len { continue; }
        if n[..owner.len] == owner && n[owner.len..owner.len + slot.len] == slot && n[owner.len + slot.len..] == tail {
            return i;
        }
    }
    return -1;
}

// arm_kind is the kind an arm rides under (docs/SPEC-TABLES.md §2.6, §3).
fn arm_kind(a: FieldDesc) -> u8 {
    if a.shape == ShapeVoid { return KindNoPayload; }
    if a.shape == ShapeTab { return KindTable; }
    return field_kind(a);
}

// load_union decodes the union framing (§3): the arm id reference, the arm's
// KIND, L, then L bytes. An arm this reader cannot name reads None and counts
// unknown; a retyped arm is judged by the field rules.
recursive fn load_union(c: LoadCtx&, r: Rd&, slot: i64, name: const u8[:]) -> bool {
    let u = union_index(c, name);
    if c.retaining && c.vals[slot].made && c.vals[slot].first >= 0 { forget_rec(c, c.vals[slot].first); }
    c.vals[slot].tab = u;
    let ref, ok = leb(c, r);
    if !ok { c.report.body_damaged = true; return false; }
    if ref == 0 {
        c.vals[slot].bits = Int128 { lo: 0, hi: 0 };
        c.vals[slot].count = 0;
        c.vals[slot].state = ValEmpty;
        return true;
    }
    // the arm's framing is read whole BEFORE the union is reset: framing
    // damage stops here and leaves what an earlier occurrence placed
    // (tablewire unionCell)
    let arm_id, named = idof(c, ref);
    if !named { c.report.body_damaged = true; return false; }
    if !has(r, 1) { c.report.body_damaged = true; return false; }
    let kind = rd_u8(c, r);
    let n, good = leb(c, r);
    if !good || !fits(r, n) { c.report.body_damaged = true; return false; }
    let len = n as! i64;
    var sub = Rd { off: r.off, end: r.off + len };
    c.vals[slot].bits = Int128 { lo: 0, hi: 0 };
    c.vals[slot].count = 0;
    c.vals[slot].state = ValEmpty;
    var tag: i64 = 0;
    for a, i in c.unit.unions[u].arms {
        if a.id == arm_id { tag = i + 1; break; }
    }
    if tag == 0 {
        c.report.unknown += 1;
        if c.retaining { c.report.retain_lost += 1; } // an excluded class (§6.6)
    } else {
        let arm = c.unit.unions[u].arms[tag - 1];
        let want = arm_kind(arm);
        if kind != want {
            if widens(kind, want) {
                // WIDENED AT AN ARM (§4): decoded at its own width
                if len != kind_width(kind) {
                    c.report.body_damaged = true;
                } else {
                    let p = reserve(c, 1);
                    c.vals[p].shape = arm.shape;
                    if scalar_at(c, c.report, sub, c.vals[p], arm, want, kind) {
                        set_union(c, slot, tag, p);
                        c.report.widened += 1;
                    }
                }
            } else {
                c.report.mismatch += 1;
            }
        } else {
            load_arm(c, sub, slot, arm, tag, len);
            if c.report.count_refused { return false; }
        }
    }
    r.off += len;
    return true;
}

fn set_union(c: LoadCtx&, slot: i64, tag: i64, p: i64) {
    c.vals[slot].state = ValPresent;
    c.vals[slot].bits = Int128 { lo: tag as! u64, hi: 0 };
    c.vals[slot].made = p >= 0;
    c.vals[slot].first = p;
    c.vals[slot].count = if p >= 0 { 1 } else { 0 };
}

// arm_fixed_width is ir.ArmWireFixedWidth: a fixed-width arm's L is its
// kind's width.
fn arm_fixed_width(a: FieldDesc) -> i64 {
    if a.shape == ShapeBool || a.shape == ShapeInt || a.shape == ShapeFloat { return kind_width(a.kind); }
    return 0;
}

// load_arm decodes ONE ARM's payload out of a reader bounded to its L (§3):
// THE ARM POSITION's dispatch. A fixed-width arm whose L is not its width, a
// reference arm whose L is not its reference, and a length arm damaged inside
// L are the arm's own framing damage — the union reads None and the parent
// reads on past L.
fn load_arm(c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let w = arm_fixed_width(a);
    if w > 0 && n != w { c.report.body_damaged = true; return false; }
    return load_payload(loader_of(a), c, r, slot, a, tag, n);
}

// arm_value is the arm's one payload slot.
fn arm_value(c: LoadCtx&, a: FieldDesc) -> i64 {
    let p = reserve(c, 1);
    c.vals[p].shape = a.shape;
    return p;
}

// arm_placed selects the arm when its payload read (§2.6).
fn arm_placed(c: LoadCtx&, slot: i64, tag: i64, p: i64, ok: bool) -> bool {
    if !ok { return false; }
    set_union(c, slot, tag, p);
    return true;
}

// load_payload: one case per registered kind, an ARM's payload of L bytes (§3).
fn load_payload(k: Loader.Scalar, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    return arm_placed(c, slot, tag, p, scalar_at(c, c.report, r, c.vals[p], a, a.kind, a.kind));
}
fn load_payload(k: Loader.Pointer, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    let index, good = leb(c, r);
    if !good || r.off != r.end { c.report.body_damaged = true; return false; }
    resolve(c, p, a, index);
    return arm_placed(c, slot, tag, p, true);
}
fn load_payload(k: Loader.Text, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    if !text_valid(c.data, r.off, n) { c.report.body_damaged = true; return false; }
    var keep = n;
    if a.shape == ShapeStr && keep > a.bound {
        keep = text_boundary(c.data, r.off, n, a.bound);
        c.report.oor += 1;
    }
    c.vals[p].state = ValPresent;
    c.vals[p].off = r.off;
    c.vals[p].len = keep;
    r.off += n;
    return arm_placed(c, slot, tag, p, true);
}
fn load_payload(k: Loader.Wide, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    if (n & 1) != 0 || !wide_valid(c.data, r.off, n / 2) { c.report.body_damaged = true; return false; }
    var keep = n / 2;
    if keep > a.bound {
        keep = wide_boundary(c.data, r.off, n / 2, a.bound);
        c.report.oor += 1;
    }
    c.vals[p].state = ValPresent;
    c.vals[p].off = r.off;
    c.vals[p].len = keep * 2;
    r.off += n;
    return arm_placed(c, slot, tag, p, true);
}
fn load_payload(k: Loader.Enum, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    let ok = load_enum(c, r, p, a, false);
    if ok && r.off != r.end { c.report.body_damaged = true; return false; }
    return arm_placed(c, slot, tag, p, ok);
}
fn load_payload(k: Loader.Body, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    // a table arm's L frames the body itself (§2.6, §3)
    let t = table_index(c, a.ref);
    let p = fresh_record(c, t);
    c.vals[p].state = ValPresent;
    body_at(c, r, t, p, true, false);
    if c.report.count_refused { return false; }
    if r.off != r.end { c.report.body_damaged = true; return false; } // the union reads None (§3)
    return arm_placed(c, slot, tag, p, true);
}
fn load_payload(k: Loader.Array, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    let ok, selected = load_array(c, r, p, a, n);
    if ok && !selected { return false; }
    return arm_placed(c, slot, tag, p, ok);
}
fn load_payload(k: Loader.Keyed, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    let ok, selected = load_array(c, r, p, a, n);
    if ok && !selected { return false; }
    return arm_placed(c, slot, tag, p, ok);
}
fn load_payload(k: Loader.Map, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a); // no arm is a map: the header's own kind decides
    return arm_placed(c, slot, tag, p, scalar_at(c, c.report, r, c.vals[p], a, a.kind, a.kind));
}
fn load_payload(k: Loader.Union, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    let p = arm_value(c, a);
    return arm_placed(c, slot, tag, p, load_union(c, r, p, a.ref));
}
fn load_payload(k: Loader.Void, c: LoadCtx&, r: Rd&, slot: i64, a: FieldDesc, tag: i64, n: i64) -> bool {
    if n != 0 { c.report.body_damaged = true; return false; }
    set_union(c, slot, tag, -1);
    return true;
}

// ---- the file: form byte, id table, node table (docs/SPEC-TABLES.md §3, §3.1) ----

// body_extent walks a body's framing to its terminator, so a body that ENDED
// EARLY (bytes no field claims) is told from a damaged one (§3).
fn body_extent(c: LoadCtx&, body: Rd) -> i64, bool {
    var r = body;
    loop {
        let ref, ok = leb(c, r);
        if !ok { return 0, false; }
        if ref == 0 { return r.off, true; }
        if ref > c.ids_n as! u64 { return 0, false; }
        if !has(r, 1) { return 0, false; }
        let kind = rd_u8(c, r);
        if !skip(c, r, kind) { return 0, false; }
    }
}

// node_table_bytes finds the root body's node-table field (§3.1): present,
// its payload window, and framed false when it cannot be read whole.
fn node_table_bytes(c: LoadCtx&, body: Rd) -> Rd, bool, bool {
    var r = body;
    var payload = Rd { off: 0, end: 0 };
    var present = false;
    loop {
        let ref, ok = leb(c, r);
        if !ok || ref == 0 { return payload, present, true; }
        let id, named = idof(c, ref);
        if !named || !has(r, 1) { return payload, present, true; }
        let kind = rd_u8(c, r);
        if id == NodeWireId {
            present = true;
            if kind != KindString {
                c.report.body_damaged = true;
                return Rd { off: 0, end: 0 }, present, false;
            }
            let n, good = leb(c, r);
            if !good || !fits(r, n) { return Rd { off: 0, end: 0 }, present, false; }
            payload = Rd { off: r.off, end: r.off + (n as! i64) };
            r.off = payload.end;
            continue;
        }
        if !skip(c, r, kind) { return payload, present, true; }
    }
}

// scan_nodes is the AUTHORITATIVE record scan (§3.1): records until the
// payload is consumed, and a node_count that disagrees is malformed.
fn scan_nodes(c: LoadCtx&, payload: Rd) -> bool {
    var r = payload;
    let declared, ok = leb(c, r);
    if !ok { return false; }
    var found: u64 = 0;
    while r.off < r.end {
        let ref, g = leb(c, r);
        if !g || ref == 0 || ref > c.ids_n as! u64 { return false; }
        let len, lg = leb(c, r);
        if !lg || !fits(r, len) { return false; }
        let type_id, named = idof(c, ref);
        c.nodes.push(LoadNode { tab: -1, blob: 0, off: r.off, len: len as! i64, val: -1, named: false });
        // the type id rides in val's place until pass one names it
        c.nodes[c.nodes.len - 1].val = type_id as! i64;
        r.off += len as! i64;
        found += 1;
    }
    return r.off == r.end && declared == found;
}

fn place_named(c: LoadCtx&, root: i64, name: const u8[:]) -> bool {
    for p in c.unit.tables[root].place { if p == name { return true; } }
    return false;
}

// load_variable reads a pointered root: the node table first, then each
// record's body into its own storage, then the root (§3.1).
fn load_variable(c: LoadCtx&, body: Rd) -> bool {
    c.numbered = true;
    c.cap = true;
    let payload, present, framed = node_table_bytes(c, body);
    var scanned = true;
    if framed && present {
        scanned = scan_nodes(c, payload);
        if scanned { c.node_n = c.nodes.len; }
    }
    if !framed && present { c.report.body_damaged = true; }
    else if !scanned { c.report.body_damaged = true; }
    else { c.good = true; }
    // PASS ONE: what each record IS, from the framing — the types this root
    // can place, and the two blob ids where an edge below it names them
    let root = c.root_tab;
    for k in c.node_n {
        let type_id = c.nodes[k].val as! u64;
        c.nodes[k].val = -1;
        if c.unit.tables[root].place_str && type_id == StringTypeId {
            if !text_valid(c.data, c.nodes[k].off, c.nodes[k].len) {
                c.report.body_damaged = true;
                continue;
            }
            c.nodes[k].blob = KindString;
            continue;
        }
        if c.unit.tables[root].place_bytes && type_id == BytesTypeId {
            c.nodes[k].blob = KindArray;
            continue;
        }
        var t: i64 = -1;
        for d, i in c.unit.tables {
            if d.id == type_id && place_named(c, root, d.name) { t = i; break; }
        }
        if t < 0 {
            c.report.unknown += 1;
            if c.retaining { c.report.retain_lost += 1; } // an excluded class (§6.6)
            continue;
        }
        c.nodes[k].tab = t;
    }
    // PASS TWO: decode each body into its own storage
    for k in c.node_n {
        let t = c.nodes[k].tab;
        if t < 0 { continue; }
        let v = fresh_record(c, t);
        c.vals[v].state = ValPresent;
        c.nodes[k].val = v;
        var sub = Rd { off: c.nodes[k].off, end: c.nodes[k].off + c.nodes[k].len };
        body_at(c, sub, t, v, true, false);
        if c.report.count_refused { return false; }
    }
    var r = body;
    let ok = body_at(c, r, root, c.root_val, false, false);
    if c.report.count_refused { return false; }
    // A CYCLIC WIRE IS DAMAGE (§3.1): the wire-alone check (wire_check) judged
    // the pointer graph over every slot, known or not, before this decode
    return ok;
}

// IdTableBound is the id table's maximum entry count (docs/SPEC-TABLES.md
// §3, THE DISTINCTNESS CHECK IS BOUNDED WORK; kTableProbeBound in the
// generated C++).
let IdTableBound: i64 = 4096;

// ids_sift moves ids[root] down the max-heap ids[0..n] to its place.
fn ids_sift(ids: u64[:], root: i64, n: i64) {
    var i = root;
    loop {
        var c = 2 * i + 1;
        if c >= n { return; }
        if c + 1 < n && ids[c] < ids[c + 1] { c += 1; }
        if !(ids[i] < ids[c]) { return; }
        let t = ids[i];
        ids[i] = ids[c];
        ids[c] = t;
        i = c;
    }
}

// ids_distinct proves the id table's n entries at `first` distinct (§3), n at
// most IdTableBound. Under eight entries a pairwise scan is at most 21
// comparisons; from eight up the entries are copied into a fixed stack array
// and heapsorted (in place, no allocation, at most about 2 n log2 n
// comparisons whatever the ids are), and a repeat sits beside its twin. No
// hostile table costs quadratic work.
fn ids_distinct(file: const u8[:], first: i64, n: i64) -> bool {
    if n < 8 {
        for i in n {
            let a = le_at(file, first + i * 8, 8);
            for j in i {
                if le_at(file, first + j * 8, 8) == a { return false; }
            }
        }
        return true;
    }
    var ids: u64[IdTableBound] = [0; IdTableBound];
    for i in n { ids[i] = le_at(file, first + i * 8, 8); }
    var i = n / 2;
    while i > 0 {
        i -= 1;
        ids_sift(ids, i, n);
    }
    var end = n;
    while end > 1 {
        end -= 1;
        let t = ids[0];
        ids[0] = ids[end];
        ids[end] = t;
        ids_sift(ids, 0, end);
    }
    for i in n - 1 {
        if ids[i] == ids[i + 1] { return false; }
    }
    return true;
}

// ---- THE WIRE-ALONE CHECK (docs/SPEC-TABLES.md §6.5, "TWO KINDS OF CONDITION") ----
//
// A port of internal/tablewire/wirecheck.go (checkFile): every condition
// decidable from the wire alone, applied to EVERY byte of a form-1 file before
// any value is decoded, in fields and records this reader declares and in
// those it does not. Skipped is not unread (§4): an unknown field is walked by
// the same rules as a known one, so damage inside it refuses the load.
//
// The walk takes no declaration. It checks the framing (lengths, counts,
// terminators, headers inside their own L, the id table's references,
// canonical LEB128, a kind no reader can skip, kind 32 with a nonzero L), a
// bool byte other than 0 or 1, ill-formed text, a non-finite float, a
// repeated field id or keyed slot in one body, bytes left inside an arm, an
// array's, a keyed array's or a map's L, the node table's framing, a reserved
// id out of place, a count past the int32 storage cap and the binary nesting
// bound. It collects every kind 17 payload in wire order and then judges the
// pointer graph: an index past the node table, a cycle, a record no pointer
// reaches, and one blob record named from two slots (§6.5, "THE GRAPH,
// DEFINED ONCE"). tla/ReadOrder.tla models the split (WireDamage,
// Unreachable, Cycle and BlobShared are SpecRefuses' wire-alone disjuncts);
// tla/Framing.tla's header-inside-its-own-L and no-read-on rules are the
// array, keyed and map checks.

// WireMaxDepth is the binary nesting bound (ir.TableBinaryMaxDepth, §4).
let WireMaxDepth: i64 = 128;

// WireCheck is the walk's state. fail is 0 or the first LoadRefuse* code the
// walk met; non_finite is noted apart (§3.3), as the Go walk does, and refuses
// the read like every other condition. ONE FIELD ID PER BODY: stamp holds, by
// id-table reference, the scope (a body or a keyed body) that last named it;
// a scope that names a reference already stamped with its own number is a
// repeat. undo holds (reference, previous stamp) pairs so a scope that closes
// gives its parent back the stamps a nested scope overwrote: O(1) per field,
// whatever the nesting.
struct WireCheck {
    data: const u8[:],
    ids_at: i64,
    ids_n: i64,
    announcement: bool,
    fail: i64,
    non_finite: bool,
    depth: i64,
    serial: i64,
    stamp: i64[>..<]&,
    undo: i64[>..<]&,
    edges: u64[>..<]&,     // every kind 17 payload, in wire order
    table_off: i64,        // the root body's node-table payload, when present
    table_end: i64,
    table_present: bool,
}

fn wc_damage(w: WireCheck&) -> bool {
    if w.fail == 0 { w.fail = LoadRefuseWireDamaged; }
    return false;
}

fn wc_refuse(w: WireCheck&, code: i64) -> bool {
    if w.fail == 0 { w.fail = code; }
    return false;
}

fn wc_id(w: WireCheck&, ref: u64) -> u64 {
    return le_at(w.data, w.ids_at + ((ref - 1) as! i64) * 8, 8);
}

// wc_open is a new scope's number and the undo mark that closes it.
fn wc_open(w: WireCheck&) -> i64, i64 {
    w.serial += 1;
    return w.serial, w.undo.len;
}

// wc_name stamps reference `ref` with `scope`: false when the scope named it already.
fn wc_name(w: WireCheck&, ref: u64, scope: i64) -> bool {
    let k = (ref - 1) as! i64;
    let prev = w.stamp[k];
    if prev == scope { return false; }
    w.undo.push(k);
    w.undo.push(prev);
    w.stamp[k] = scope;
    return true;
}

fn wc_close(w: WireCheck&, mark: i64) {
    while w.undo.len > mark {
        let prev = w.undo[w.undo.len - 1];
        let k = w.undo[w.undo.len - 2];
        w.stamp[k] = prev;
        w.undo.resize(w.undo.len - 2);
    }
}

fn wc_fixed_width(kind: u8) -> bool {
    return (kind >= 1 && kind <= 11) || (kind >= 18 && kind <= 29) || kind == 36;
}

fn wc_structural(kind: u8) -> bool {
    return kind == KindTable || kind == KindArray || kind == 15 || kind == 16;
}

// wc_scalar checks a fixed-width value's bytes: a bool is 0 or 1, and a float
// is finite (§3.3).
fn wc_scalar(w: WireCheck&, at: i64, kind: u8) -> bool {
    if kind == 1 {
        if w.data[at] > 1 { return wc_damage(w); }
    } else if kind == 10 {
        if (le_at(w.data, at, 4) & 0x7F800000) == 0x7F800000 { w.non_finite = true; }
    } else if kind == 11 {
        if (le_at(w.data, at, 8) & 0x7FF0000000000000) == 0x7FF0000000000000 { w.non_finite = true; }
    }
    return true;
}

// wc_header reads an array, keyed or map content's element kind and count.
// THE HEADER LIES INSIDE ITS OWN L (§4): content shorter than the two bytes a
// header needs, or a count that runs past it, is damage; a count past the
// int32 storage cap is §2.9's refusal. It answers the element kind, the count
// and the offset after the header, or ok false.
fn wc_header(w: WireCheck&, at: i64, end: i64) -> u8, u64, i64, bool {
    if end - at < 2 { wc_damage(w); return 0, 0, 0, false; }
    let count, next, ok = leb_at(w.data, at + 1, end);
    if !ok { wc_damage(w); return 0, 0, 0, false; }
    if count > 0x7FFFFFFF { wc_refuse(w, LoadRefuseCountOverExtentCap); return 0, 0, 0, false; }
    return w.data[at], count, next, true;
}

recursive fn wc_array(w: WireCheck&, at: i64, end: i64) -> bool {
    let ek, count, first, ok = wc_header(w, at, end);
    if !ok { return false; }
    var off = first;
    var i: u64 = 0;
    while i < count {
        off = wc_payload(w, off, end, ek);
        if off < 0 { return false; }
        i += 1;
    }
    if off != end { return wc_damage(w); }
    return true;
}

recursive fn wc_keyed(w: WireCheck&, at: i64, end: i64) -> bool {
    let ek, count, first, ok = wc_header(w, at, end);
    if !ok { return false; }
    let scope, mark = wc_open(w);
    var off = first;
    var i: u64 = 0;
    while i < count {
        let key, after_key, good = leb_at(w.data, off, end);
        if !good || key == 0 || key > w.ids_n as! u64 { wc_close(w, mark); return wc_damage(w); }
        if !wc_name(w, key, scope) { wc_close(w, mark); return wc_damage(w); }
        let n, after, framed = leb_at(w.data, after_key, end);
        if !framed || n > (end - after) as! u64 { wc_close(w, mark); return wc_damage(w); }
        off = after + (n as! i64);
        if !wc_content(w, after, off, ek) { wc_close(w, mark); return false; }
        i += 1;
    }
    wc_close(w, mark);
    if off != end { return wc_damage(w); }
    return true;
}

// wc_union walks a union payload in place: the arm reference, and for a set
// arm its kind byte, its L and the content the L frames. A STRUCTURAL ARM IS
// A LEVEL (§4). It answers the offset after the payload, or -1.
recursive fn wc_union(w: WireCheck&, at: i64, end: i64) -> i64 {
    let ref, next, ok = leb_at(w.data, at, end);
    if !ok { wc_damage(w); return -1; }
    if ref == 0 { return next; }
    if ref > w.ids_n as! u64 || next >= end { wc_damage(w); return -1; }
    let kind = w.data[next];
    let n, after, framed = leb_at(w.data, next + 1, end);
    if !framed || n > (end - after) as! u64 { wc_damage(w); return -1; }
    let stop = after + (n as! i64);
    let structural = wc_structural(kind);
    if structural {
        if w.depth >= WireMaxDepth { wc_damage(w); return -1; }
        w.depth += 1;
    }
    let good = wc_content(w, after, stop, kind);
    if structural { w.depth -= 1; }
    return if good { stop } else { -1 };
}

// wc_payload walks one SELF-FRAMED payload of `kind` at `at`: what follows a
// field's kind byte, and an array element. It answers the new offset, or -1.
recursive fn wc_payload(w: WireCheck&, at: i64, end: i64, kind: u8) -> i64 {
    if wc_fixed_width(kind) {
        let width = kind_width(kind);
        if width > end - at { wc_damage(w); return -1; }
        if !wc_scalar(w, at, kind) { return -1; }
        return at + width;
    }
    if kind == KindPointer {
        let index, next, ok = leb_at(w.data, at, end);
        if !ok { wc_damage(w); return -1; }
        w.edges.push(index);
        return next;
    }
    if kind == 30 {
        let ref, next, ok = leb_at(w.data, at, end);
        if !ok || ref > w.ids_n as! u64 { wc_damage(w); return -1; }
        return next;
    }
    if kind == 15 { return wc_union(w, at, end); }
    if kind == KindString || kind == 33 || kind == 31 || kind == KindNoPayload
        || kind == KindTable || kind == KindArray || kind == 16 {
        let n, next, ok = leb_at(w.data, at, end);
        if !ok || n > (end - next) as! u64 { wc_damage(w); return -1; }
        let stop = next + (n as! i64);
        if !wc_content(w, next, stop, kind) { return -1; }
        return stop;
    }
    wc_damage(w); // a kind no reader can skip
    return -1;
}

// wc_content walks a payload already framed by a length, and requires it to be
// consumed exactly: a field's L, an arm's L, a keyed slot's L.
recursive fn wc_content(w: WireCheck&, at: i64, end: i64, kind: u8) -> bool {
    if wc_fixed_width(kind) {
        if end - at != kind_width(kind) { return wc_damage(w); }
        return wc_scalar(w, at, kind);
    }
    if kind == KindString {
        if !text_valid(w.data, at, end - at) { return wc_damage(w); }
        return true;
    }
    if kind == 33 {
        if ((end - at) & 1) != 0 || !wide_valid(w.data, at, (end - at) / 2) { return wc_damage(w); }
        return true;
    }
    if kind == 31 { return true; } // opaque: the one kind whose meaning a reader may not know (§3)
    if kind == KindNoPayload {
        if end != at { return wc_damage(w); }
        return true;
    }
    if kind == KindTable || kind == KindArray || kind == 16 {
        // THE BINARY NESTING BOUND (§4): a framed body is one level deeper
        // than the payload holding it
        if w.depth >= WireMaxDepth { return wc_damage(w); }
        w.depth += 1;
        var good = false;
        if kind == KindTable { good = wc_body(w, at, end, false); }
        else if kind == KindArray { good = wc_array(w, at, end); }
        else { good = wc_keyed(w, at, end); }
        w.depth -= 1;
        return good;
    }
    if kind == KindPointer || kind == 30 || kind == 15 {
        let stop = wc_payload(w, at, end, kind);
        if stop < 0 { return false; }
        if stop != end { return wc_damage(w); } // bytes left inside an arm after its payload
        return true;
    }
    return wc_damage(w);
}

// wc_body walks one table body that must end with its terminator as its LAST
// byte. `root` admits the node table's reserved id, whose kind 12 payload is
// kept as the node table instead of being read as text, and, in an
// announcement, the two reserved announcement ids.
recursive fn wc_body(w: WireCheck&, at: i64, end: i64, root: bool) -> bool {
    let scope, mark = wc_open(w);
    var off = at;
    loop {
        let ref, next, ok = leb_at(w.data, off, end);
        if !ok { wc_close(w, mark); return wc_damage(w); }
        off = next;
        if ref == 0 {
            wc_close(w, mark);
            if off != end { return wc_damage(w); } // bytes after a body's terminator inside its L
            return true;
        }
        if ref > w.ids_n as! u64 { wc_close(w, mark); return wc_damage(w); }
        if !wc_name(w, ref, scope) { wc_close(w, mark); return wc_damage(w); } // the same field id twice in one body
        if off >= end { wc_close(w, mark); return wc_damage(w); }
        let kind = w.data[off];
        off += 1;
        let id = wc_id(w, ref);
        if id == BuildVersionWireId || id == VocabularyWireId {
            if !root || !w.announcement { wc_close(w, mark); return wc_damage(w); }
        } else if id == NodeWireId {
            if !root || kind != KindString || w.table_present { wc_close(w, mark); return wc_damage(w); }
            let n, after, framed = leb_at(w.data, off, end);
            if !framed || n > (end - after) as! u64 { wc_close(w, mark); return wc_damage(w); }
            w.table_present = true;
            w.table_off = after;
            w.table_end = after + (n as! i64);
            off = w.table_end;
            continue;
        }
        off = wc_payload(w, off, end, kind);
        if off < 0 { wc_close(w, mark); return false; }
    }
}

// wc_graph judges the pointer graph over `first` (each node's first edge in
// w.edges, with one more entry closing the last node) and `blob` (whether a
// node is a blob record). Node 0 is the root, node k is record k (wire index
// k + 1). Every pointer names null, the root or a record; a walk from the
// root that meets a node whose descent is still open is a cycle; a record the
// walk never enters is unreachable; and a blob record named from two slots,
// anywhere in the input, is blob_shared.
fn wc_graph(w: WireCheck&, first: const i64[:], blob: const bool[:]) -> bool {
    let n = blob.len;
    var names: i64[>..<] = [];
    for _ in n { names.push(0); }
    for e in w.edges {
        if e == 0 { continue; }
        if e - 1 >= n as! u64 { return wc_damage(w); } // a pointer index past the node table
        names[(e - 1) as! i64] += 1;
    }
    for k in n {
        if blob[k] && names[k] > 1 { return wc_refuse(w, LoadRefuseBlobShared); }
    }
    // 0 unvisited, 1 open, 2 closed; the stack holds (node, next edge)
    var mark: u8[>..<] = [];
    for _ in n { mark.push(0); }
    var stack: i64[>..<] = [];
    stack.push(0);
    stack.push(first[0]);
    mark[0] = 1;
    while stack.len > 0 {
        let node = stack[stack.len - 2];
        let next = stack[stack.len - 1];
        if next == first[node + 1] {
            mark[node] = 2;
            stack.resize(stack.len - 2);
            continue;
        }
        stack[stack.len - 1] = next + 1;
        let e = w.edges[next];
        if e == 0 { continue; }
        let t = (e - 1) as! i64;
        if mark[t] == 1 { return wc_damage(w); } // a cycle
        if mark[t] == 2 { continue; }
        mark[t] = 1;
        stack.push(t);
        stack.push(first[t]);
    }
    for k in 1..n {
        if mark[k] == 0 { return wc_damage(w); } // a node record no pointer reaches
    }
    return true;
}

// wire_check runs every wire-alone condition over a form-1 file's root body
// data[at, end) and its id table (ids_n entries at ids_at). It answers 0 or
// the first refusal's LoadRefuse* code, and whether a float the wire carries
// is not finite.
fn wire_check(data: const u8[:], at: i64, end: i64, ids_at: i64, ids_n: i64, announcement: bool) -> i64, bool {
    var stamp: i64[>..<] = [];
    for _ in ids_n { stamp.push(0); }
    var undo: i64[>..<] = [];
    var edges: u64[>..<] = [];
    var w = WireCheck { data: data, ids_at: ids_at, ids_n: ids_n, announcement: announcement, fail: 0,
                        non_finite: false, depth: 0, serial: 0, stamp: stamp, undo: undo, edges: edges,
                        table_off: 0, table_end: 0, table_present: false };
    if !wc_body(w, at, end, true) { return w.fail, w.non_finite; }
    var first: i64[>..<] = [];
    var blob: bool[>..<] = [];
    first.push(0);
    blob.push(false);
    if w.table_present {
        // THE NODE TABLE, READ WHOLE (§3.1): its count, then records of a
        // type reference and an L, consumed exactly and as many as declared
        let declared, after, ok = leb_at(data, w.table_off, w.table_end);
        if !ok { wc_damage(w); return w.fail, w.non_finite; }
        var off = after;
        var found: u64 = 0;
        while off < w.table_end {
            let ref, next, good = leb_at(data, off, w.table_end);
            if !good || ref == 0 || ref > ids_n as! u64 { wc_damage(w); return w.fail, w.non_finite; }
            let len, body, framed = leb_at(data, next, w.table_end);
            if !framed || len > (w.table_end - body) as! u64 { wc_damage(w); return w.fail, w.non_finite; }
            let stop = body + (len as! i64);
            first.push(w.edges.len);
            let type_id = wc_id(w, ref);
            if type_id == StringTypeId || type_id == BytesTypeId {
                // A BLOB RECORD IS ITS BYTES (§2.5, §3.1): the reserved type
                // ids are known to every reader, so the content rule of a text
                // blob and the size cap are wire-alone conditions
                if len > 0xFFFFFFFF { wc_damage(w); return w.fail, w.non_finite; }
                if type_id == StringTypeId && !text_valid(data, body, stop - body) {
                    wc_damage(w);
                    return w.fail, w.non_finite;
                }
                blob.push(true);
            } else {
                if !wc_body(w, body, stop, false) { return w.fail, w.non_finite; }
                blob.push(false);
            }
            off = stop;
            found += 1;
        }
        if off != w.table_end || found != declared { wc_damage(w); return w.fail, w.non_finite; }
    }
    first.push(w.edges.len);
    wc_graph(w, first, blob);
    return w.fail, w.non_finite;
}

// load_with is the one load both entry points share: load_table, and
// load_table_retain (wire_retain.goose) with retention on.
fn load_with(unit: LoadUnit, root: const u8[:], data: const u8[:], vals: LoadVal[>..]&, nodes: LoadNode[>..]&, report: LoadReport&, store: RetainStore&, retaining: bool) -> i64 {
    var c = LoadCtx { data: data, unit: unit, vals: vals, nodes: nodes, report: report, ids_at: 0, ids_n: 0, numbered: false, good: false, root_tab: -1, root_val: -1, cap: false, node_n: 0, retain: store, retaining: retaining };
    // THE FORM BYTE IS READ FIRST (§3): form 2 is a stream form and never a
    // file's own, and any form but 1 is refused with no damage reported
    if data.len < 1 { report.body_damaged = true; report.damaged = true; return -1; }
    if data[0] != 1 {
        report.refused = true;
        report.reason = if data[0] == 2 { LoadRefuseMessageFormAsFile } else { LoadRefuseNewerForm };
        return -1;
    }
    let t = table_index(c, root);
    if t < 0 { report.refused = true; return -1; }
    var file = data;
    if unit.tables[t].integrity {
        if !integrity_holds(data, 1) {
            report.body_damaged = true;
            report.damaged = true;
            report.reason = LoadRefuseIntegrityMismatch;
            return -1;
        }
        file = data[0..data.len - 4];
        c.data = file;
    }
    // THE ID TABLE, from the end (§3): the count, then 8 x count entries.
    // Damage here and an early end below are the verdict's damage (§4)
    if file.len < 9 { report.body_damaged = true; report.damaged = true; return -1; }
    let count = le_at(file, file.len - 8, 8);
    if count > (file.len / 8) as! u64 { report.body_damaged = true; report.damaged = true; return -1; }
    let span = (count as! i64) * 8 + 8;
    if span + 1 > file.len { report.body_damaged = true; report.damaged = true; return -1; }
    // THE BOUNDED-WORK CONTRACT (§3): a table that claims more than
    // IdTableBound entries is malformed, whatever the buffer would hold
    if count > IdTableBound as! u64 { report.body_damaged = true; report.damaged = true; return -1; }
    let first = file.len - span;
    // ONE ID IN TWO ENTRIES: the whole wire is malformed (§3)
    if !ids_distinct(file, first, count as! i64) { report.body_damaged = true; report.damaged = true; return -1; }
    c.ids_at = first;
    c.ids_n = count as! i64;
    let body = Rd { off: 1, end: first };
    let end, terminated = body_extent(c, body);
    if terminated && end != first { report.body_damaged = true; report.damaged = true; return -1; }
    // THE WIRE-ALONE CHECK (§6.5) over every byte of the file, known fields
    // and unknown ones, before any value is decoded: a refusal here lands
    // nothing, and a non-finite float anywhere refuses the read (§3.3)
    let wfail, wnon = wire_check(file, 1, first, first, count as! i64, false);
    if wfail == LoadRefuseWireDamaged { report.body_damaged = true; report.damaged = true; report.reason = wfail; return -1; }
    if wfail == LoadRefuseCountOverExtentCap { report.count_refused = true; report.refused = true; report.reason = wfail; return -1; }
    if wfail != 0 { report.refused = true; report.blob_shared += 1; report.reason = wfail; return -1; }
    if wnon { report.non_finite = true; report.reason = LoadRefuseNonFiniteFloat; return -1; }
    c.root_tab = t;
    c.root_val = fresh_record(c, t);
    c.vals[c.root_val].state = ValPresent;
    if !unit.tables[t].variable {
        // a value-only table has no node table (§3). Damage its walk read
        // past is the verdict's; damage that stopped the walk is a malformed
        // body (gotable's <T>LoadStrict)
        var r = body;
        if !body_at(c, r, t, c.root_val, false, false) { report.stopped = true; }
        else if report.body_damaged { report.damaged = true; }
    } else if !load_variable(c, body) {
        report.stopped = true;
    }
    // A NON-FINITE FLOAT IS THE READ'S OWN REFUSAL (§3.3): it outranks every
    // other verdict, and the caller discards the partial value
    if report.non_finite { report.reason = LoadRefuseNonFiniteFloat; return -1; }
    // §4: damage in any body invalidates the whole plain or retaining load.
    if report.body_damaged { return -1; }
    // THE STRICT READ REFUSES AT THE END OF THE LOAD (docs/SPEC-TABLES.md
    // §6.5; docs/plans/strict-only.md layer 4): a map key that repeats or
    // descends is damage (§2.8), and any other condition the walk met — a
    // value out of range, a kind this reader cannot land, a blob named twice,
    // a walk that stopped — refuses the whole read by load_error's one reason.
    // Only unknown, widened and retained fields leave a value.
    if report.repeated > 0 { report.body_damaged = true; report.damaged = true; report.reason = LoadRefuseWireDamaged; return -1; }
    let refusal = load_error(report, c.root_val);
    if refusal != LoadRefuseOk { report.refused = true; report.reason = refusal; return -1; }
    return c.root_val;
}

// load_table is THE LOAD: it reads `data` as a `root` (docs/SPEC-TABLES.md
// §3, §4) into the caller's flat value table and node table, counting every
// evolution event in `report`, and returns the root record's slot, -1 when
// nothing was decoded (a refusal, or a file that cannot be read whole).
fn load_table(unit: LoadUnit, root: const u8[:], data: const u8[:], vals: LoadVal[>..]&, nodes: LoadNode[>..]&, report: LoadReport&) -> i64 {
    var bytes: u8[>..<] = [];
    var recs: RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var none = RetainStore { capacity: 0, id_capacity: 0, used: 0, bytes: bytes, recs: recs, ids: ids };
    return load_with(unit, root, data, vals, nodes, report, none, false);
}
