// message.goose — the MESSAGE FORM (form byte 2) runtime (docs/SPEC-TABLES.md
// §3.3; docs/PORT-GOOSE.md, THE RUNTIME LAYOUT and THE CARD MAP:
// schema-goose-message-form). It holds what every message-form root shares:
// the bit stream (bit i of the stream is bit i % 8 of byte i / 8, low bit
// first, the packet wire's order), the aligns and their verified-zero pad, the
// float rules (finite only; the compressed float's quantization in float32 and
// by nothing else), the refusal names and the report; AnnounceRead and the
// peer's VOCABULARY (announce_read: a form-1 file into one direction's id
// table, each entry's id, kind and shape); and the reader BY PEER ENTRY: the
// reference resolve, §4's widening rule, the skip of an entry by its announced
// shape, and the payloads decoded at the sender's shape. The per-table bodies
// — every field a reference then a payload, ending at the zero reference — are
// emitted by messageform.go as straight-line code over these helpers.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_tables;
import .fixed;
import .wire_load;

// The form byte and the batch's wire bound (docs/SPEC-TABLES.md §3.3: the
// count is a ranged integer over [1, 256], eight bits carrying M - 1).
let MsgForm: u8 = 2;
let MsgBatchMax: i64 = 256;

// THE REFUSALS OF THE MESSAGE PATH, by the Go engine's names
// (internal/tablewire/message.go, MessageReason; docs/SPEC-TABLES.md §3.3).
let MsgOk: i64 = 0;
let MsgRefuseNewerForm: i64 = 1;
let MsgRefuseNoVocabulary: i64 = 2;
let MsgRefuseSecondAnnouncement: i64 = 3;
let MsgRefuseVocabularyTooLarge: i64 = 4;
let MsgRefuseMessageFormAsFile: i64 = 5;
let MsgRefuseBatchTooLarge: i64 = 6;
let MsgRefuseNonFiniteFloat: i64 = 7;
let MsgRefuseInvalidValue: i64 = 8;

fn msg_reason_name(code: i64) -> const u8[:] {
    match code {
        0 => "ok",
        1 => "newer_form",
        2 => "no_vocabulary",
        3 => "second_announcement",
        4 => "vocabulary_too_large",
        5 => "message_form_as_file",
        6 => "batch_too_large",
        7 => "non_finite_float",
        8 => "invalid_value",
        _ => "???",
    }
}

// MsgReport is one call's report (docs/SPEC-TABLES.md §3.3, §4): the refusal
// (reason non-zero), the damage flag, §4's counters, and on a capacity
// refusal the count the wire carries ("the reader sets the returned count to
// the wire's M before it returns the refusal"). AnnounceRead counts the
// unknown fields of the announcement's own body in `unknown`.
struct MsgReport {
    reason: i64,
    malformed: bool,
    unknown: i64,
    kind_mismatch: i64,
    widened: i64,
    clamped: i64,
    duplicate: i64,
    count: i64,
}

// ---- the writer -------------------------------------------------------------

// MsgWriter is the bit writer's state over a caller's growable buffer: the
// partial byte, how many of its bits are used, the stream's bit count, and
// whether this is a MEASURE (nothing is pushed). ok false is a refusal, its
// reason beside it.
struct MsgWriter { cur: u64, used: i64, bits: i64, measure: bool, ok: bool = true, reason: i64 }

// msg_put appends the low n bits of v (0 <= n <= 64), low bit first. A value
// moves a byte at a time: the partial byte takes what fits, each whole byte
// after it is pushed.
fn msg_put(w: MsgWriter&, out: u8[>..]&, v: u64, n: i64) {
    var left = n;
    var x = v;
    w.bits += n;
    while left > 0 {
        let space = 8 - w.used;
        var take = space;
        if left < take { take = left; }
        let mask: u64 = (1 << (take as u64)) - 1;
        w.cur |= (x & mask) << (w.used as u64);
        w.used += take;
        left -= take;
        if take < 64 { x = x >> (take as u64); }
        if w.used == 8 {
            if !w.measure { out.push(w.cur as! u8); }
            w.cur = 0;
            w.used = 0;
        }
    }
}

fn msg_put_bool(w: MsgWriter&, out: u8[>..]&, b: bool) {
    if b { msg_put(w, out, 1, 1); } else { msg_put(w, out, 0, 1); }
}

// msg_align zero-fills to the next byte boundary, relative to the batch's
// byte 0, the form byte's own (docs/SPEC-TABLES.md §3.3).
fn msg_align(w: MsgWriter&, out: u8[>..]&) {
    if w.used > 0 {
        msg_put(w, out, 0, 8 - w.used);
    }
}

// msg_put_bytes appends whole bytes after an align.
fn msg_put_bytes(w: MsgWriter&, out: u8[>..]&, s: const u8[:], n: i64) {
    var i: i64 = 0;
    while i < n {
        msg_put(w, out, s[i], 8);
        i += 1;
    }
}

// msg_refuse records a write refusal; the caller returns false.
fn msg_refuse(w: MsgWriter&, code: i64) -> bool {
    w.ok = false;
    w.reason = code;
    return false;
}

// ---- the vocabulary (docs/SPEC-TABLES.md §3.3) ---------------------------------

// MsgEntry is ONE announced entry, decoded once at AnnounceRead: the id, the
// kind, and the facts of its shape a reader decodes and skips by. `bits` is
// the value's width on the wire (1 for a bool, the ranged or quantized width,
// the kind's own width raw, -1 for a kind that is not one value), the base is
// the ranged base's two's-complement lanes, the quantized triple is resolved
// to its delta and step count, `min` and `max` are an array's counts or a
// text's capacity, and an array's or a keyed array's ELEMENT shape is the
// record `inner` names in the same vocabulary (-1 for none).
struct MsgEntry {
    id: u64,
    kind: u8,
    packing: u8,
    bits: i64,
    base_lo: u64,
    base_hi: u64,
    qmin: f32,
    qdelta: f32,
    qcount: u32,
    min: i64,
    max: i64,
    elem: u8,
    inner: i64 = -1,
}

// MsgVocabulary is ONE DIRECTION's id table (docs/SPEC-TABLES.md §3.3): slot
// k is entries[k - 1] for k in 1..count, and the element shapes those entries
// name sit after them. Only the first announcement sets it, a refused one sets
// none for the connection's life, and the build version is kept the moment it
// is read. The two bounds are the receiver's declared capacity (§3.3: 4096
// entries, a 64 KiB vocabulary field by default).
struct MsgVocabulary {
    max_entries: i64 = 4096,
    max_bytes: i64 = 65536,
    count: i64 = 0,
    ref_bits: i64 = 1,
    build_version: u64 = 0,
    announced: bool = false,
    refused: bool = false,
    entries: MsgEntry[>..<],
}

fn msg_bits_required(lo: i64, hi: i64) -> i64 {
    if hi <= lo { return 0; }
    var span: u64 = (hi as! u64) - (lo as! u64);
    var n: i64 = 0;
    while span > 0 {
        n += 1;
        span = span >> 1;
    }
    return n;
}

fn msg_int_kind(k: u8) -> bool { (k >= 2 && k <= 9) || k == 18 || k == 19 }
fn msg_fixed_kind(k: u8) -> bool { k >= 20 && k <= 29 }
fn msg_wide_kind(k: u8) -> bool { k == 18 || k == 19 || k == 24 || k == 29 }
fn msg_signed_kind(k: u8) -> bool { (k >= 2 && k <= 5) || (k >= 20 && k <= 23) }

// msg_known_kind is §3's closed set and the 0 of a name.
fn msg_known_kind(k: u8) -> bool { k <= 32 }

// msg_kind_bits is the widest RANGED value a kind carries, its own storage
// width: a width above it is a hostile width on the announcement.
fn msg_kind_bits(k: u8) -> i64 {
    return match k {
        2, 6, 20, 25 => 8,
        3, 7, 21, 26 => 16,
        4, 8, 22, 27 => 32,
        5, 9, 23, 28 => 64,
        _ => 128,
    };
}

// msg_quantization is SPEC.md §4.3's derivation over an announced triple, in
// float32 and by nothing else; false is a non-conforming triple, which on the
// announcement is a hostile width.
fn msg_quantization(qmin: f32, qmax: f32, qres: f32, e: MsgEntry&) -> bool {
    if !(qmin < qmax) || !(qres > 0.0) { return false; }
    let delta: f32 = qmax - qmin;
    var values: f32 = delta / qres;
    if !(delta - delta == 0.0) || !(values - values == 0.0) { return false; }
    if !(values >= 1.0) {
        values = 1.0;
    } else if values > 4294967040.0 {
        values = 4294967040.0;
    }
    var count = values as! u32;
    let whole: f32 = count;
    if whole < values { count += 1; }
    e.qmin = qmin;
    e.qdelta = delta;
    e.qcount = count;
    e.bits = msg_bits_required(0, count);
    return true;
}

fn msg_f32_at(w: const u8[:], at: i64) -> f32 {
    return fixed_bits_f32(le_at(w, at, 4) as! u32);
}

// msg_shape reads the shape a kind names (ir.DecodeShape) into e, at `at`.
// An array's element shape is read into a record of its own: appended to the
// vocabulary when `store` is set, and only checked when it is not.
recursive fn msg_shape(w: const u8[:], at: i64&, kind: u8, e: MsgEntry&, v: MsgVocabulary&, store: bool) -> bool {
    e.kind = kind;
    e.bits = -1;
    e.inner = -1;
    if kind == 1 { e.bits = 1; }
    if kind == 11 { e.bits = 64; }
    if msg_int_kind(kind) || msg_fixed_kind(kind) || kind == 10 {
        if at >= w.len { return false; }
        e.packing = w[at];
        at += 1;
        if e.packing == 0 {
            e.bits = if kind == 10 { 32 } else { msg_kind_bits(kind) };
            return true;
        }
        if e.packing == 1 && kind != 10 {
            let width, next, ok = leb_at(w, at, w.len);
            if !ok || width > msg_kind_bits(kind) as! u64 { return false; }
            at = next;
            e.bits = width as! i64;
            if msg_wide_kind(kind) || msg_fixed_kind(kind) {
                if at + 16 > w.len { return false; }
                e.base_lo = le_at(w, at, 8);
                e.base_hi = le_at(w, at + 8, 8);
                at += 16;
                if !msg_wide_kind(kind) {
                    // a 64-bit-or-narrower fixed kind's base is its low lane
                    e.base_hi = 0;
                }
                return true;
            }
            let z, after, good = leb_at(w, at, w.len);
            if !good { return false; }
            at = after;
            if msg_signed_kind(kind) {
                let half: u64 = z >> 1;
                let sign: u64 = 0 - (z & 1);
                e.base_lo = half ^ sign;
            } else {
                e.base_lo = z;
            }
            return true;
        }
        if e.packing == 2 && kind == 10 {
            if at + 12 > w.len { return false; }
            let qmin = msg_f32_at(w, at);
            let qmax = msg_f32_at(w, at + 4);
            let qres = msg_f32_at(w, at + 8);
            at += 12;
            return msg_quantization(qmin, qmax, qres, e);
        }
        return false;
    }
    if kind == 12 {
        let n, next, ok = leb_at(w, at, w.len);
        if !ok || n > 0x7FFFFFFF { return false; }
        at = next;
        e.max = n as! i64;
        return true;
    }
    if kind == 14 || kind == 16 {
        if kind == 14 {
            let lo, next, ok = leb_at(w, at, w.len);
            if !ok || lo > 0xFFFFFFFF { return false; }
            at = next;
            e.min = lo as! i64;
        }
        let hi, next, ok = leb_at(w, at, w.len);
        if !ok || hi > 0xFFFFFFFF || (hi as! i64) < e.min { return false; }
        at = next;
        e.max = hi as! i64;
        if at >= w.len { return false; }
        e.elem = w[at];
        at += 1;
        // an element kind of 12 is refused at the announcement (§3.3)
        if !msg_known_kind(e.elem) || e.elem == 12 { return false; }
        var sub = MsgEntry { .. };
        if !msg_shape(w, at, e.elem, sub, v, store) { return false; }
        if store {
            e.inner = v.entries.len;
            v.entries.push(sub);
        }
        return true;
    }
    return true;
}

// msg_entry reads one entry, its id, kind and shape, at `at`.
fn msg_entry(w: const u8[:], at: i64&, e: MsgEntry&, v: MsgVocabulary&, store: bool) -> bool {
    if at + 9 > w.len { return false; }
    e.id = le_at(w, at, 8);
    let kind = w[at + 8];
    at += 9;
    if !msg_known_kind(kind) { return false; }
    return msg_shape(w, at, kind, e, v, store);
}

// msg_file_skip steps past one form-1 field the announcement does not name,
// by its kind alone (§3), inside [off, end).
fn msg_file_skip(data: const u8[:], off: i64&, end: i64, kind: u8) -> bool {
    if (kind >= 1 && kind <= 11) || (kind >= 18 && kind <= 29) || kind == 36 {
        let width = kind_width(kind);
        if off + width > end { return false; }
        off += width;
        return true;
    }
    if kind == 17 || kind == 30 {
        let x, next, ok = leb_at(data, off, end);
        if ok { off = next; }
        return ok;
    }
    if kind == 12 || kind == 13 || kind == 14 || kind == 16 || kind == 31 || kind == 32 {
        let n, next, ok = leb_at(data, off, end);
        if !ok || n > (end - next) as! u64 { return false; }
        off = next + (n as! i64);
        return true;
    }
    if kind == 15 {
        let arm, next, ok = leb_at(data, off, end);
        if !ok { return false; }
        off = next;
        if arm == 0 { return true; }
        if off + 1 > end { return false; }
        off += 1;
        let n, after, good = leb_at(data, off, end);
        if !good || n > (end - after) as! u64 { return false; }
        off = after + (n as! i64);
        return true;
    }
    return false;
}

// announce_once is the one read a connection gets (internal/tablewire
// message.go, announceRead): the announcement is a FORM-1 FILE whose body
// carries the build version and the vocabulary, each exactly once, under §4's
// ordinary tolerance for anything else.
fn announce_once(v: MsgVocabulary&, data: const u8[:], report: MsgReport&) -> bool {
    if data.len < 1 {
        report.malformed = true;
        return false;
    }
    if data[0] != 1 {
        report.reason = if data[0] == 2 { MsgRefuseMessageFormAsFile } else { MsgRefuseNewerForm };
        return false;
    }
    if data.len < 9 {
        report.malformed = true;
        return false;
    }
    // THE ID TABLE, from the end (§3): the count, then 8 x count entries
    let ids_n = le_at(data, data.len - 8, 8);
    if ids_n > (data.len / 8) as! u64 {
        report.malformed = true;
        return false;
    }
    let span = (ids_n as! i64) * 8 + 8;
    if span + 1 > data.len {
        report.malformed = true;
        return false;
    }
    let first = data.len - span;
    for i in ids_n as! i64 {
        let a = le_at(data, first + i * 8, 8);
        for j in i {
            if le_at(data, first + j * 8, 8) == a {
                report.malformed = true;
                return false;
            }
        }
    }
    // the body's extent: a body that ENDED EARLY is malformed (§3)
    var off: i64 = 1;
    loop {
        let ref, next, ok = leb_at(data, off, first);
        if !ok { break; }
        off = next;
        if ref == 0 {
            if off != first {
                report.malformed = true;
                return false;
            }
            break;
        }
        if ref > ids_n || off + 1 > first { break; }
        let kind = data[off];
        off += 1;
        if !msg_file_skip(data, off, first, kind) { break; }
    }
    // the fields: the build version and the vocabulary, each exactly once
    var version: u64 = 0;
    var seen_version: i64 = 0;
    var seen_words: i64 = 0;
    var words_at: i64 = 0;
    var words_end: i64 = 0;
    var good = true;
    off = 1;
    loop {
        let ref, next, ok = leb_at(data, off, first);
        if !ok {
            report.malformed = true;
            return false;
        }
        off = next;
        if ref == 0 { break; }
        if ref > ids_n || off + 1 > first {
            report.malformed = true;
            return false;
        }
        let id = le_at(data, first + ((ref - 1) as! i64) * 8, 8);
        let kind = data[off];
        off += 1;
        if id == BuildVersionWireId {
            if kind != 9 || off + 8 > first {
                good = false;
                break;
            }
            version = le_at(data, off, 8);
            off += 8;
            seen_version += 1;
        } else if id == VocabularyWireId {
            // kind 14 over element kind 6: §3's spelling of an opaque run of bytes
            let n, after, lok = leb_at(data, off, first);
            if kind != 14 || !lok || n > (first - after) as! u64 {
                good = false;
                break;
            }
            let at = after;
            let end = after + (n as! i64);
            off = end;
            if at >= end || data[at] != 6 {
                good = false;
                break;
            }
            let length, body, bok = leb_at(data, at + 1, end);
            if !bok || body + (length as! i64) != end {
                good = false;
                break;
            }
            if length > v.max_bytes as! u64 {
                // the byte bound, read off the field's own length (§3.3)
                report.reason = MsgRefuseVocabularyTooLarge;
                return false;
            }
            words_at = body;
            words_end = end;
            seen_words += 1;
        } else {
            report.unknown += 1;
            if !msg_file_skip(data, off, first, kind) {
                report.malformed = true;
                return false;
            }
        }
    }
    // THE BUILD VERSION IS KEPT THE MOMENT IT IS READ, refusal or not (§3.3)
    if !good { version = 0; }
    v.build_version = version;
    if !good || seen_version != 1 || seen_words != 1 {
        report.malformed = true;
        return false;
    }
    // the entries, checked whole before one is kept
    let words = data[words_at..words_end];
    var spans: i64[>..<] = [];
    var count: i64 = 0;
    var nodes: i64 = 0;
    var at: i64 = 0;
    while at < words.len {
        let began = at;
        var e = MsgEntry { .. };
        if !msg_entry(words, at, e, v, false) {
            report.malformed = true;
            return false;
        }
        // the announcement's own two ids never take a slot, and the node
        // table's takes exactly one
        if e.id == BuildVersionWireId || e.id == VocabularyWireId {
            report.malformed = true;
            return false;
        }
        if e.id == NodeWireId {
            nodes += 1;
            if nodes > 1 {
                report.malformed = true;
                return false;
            }
        }
        // A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE (§3.3)
        var k: i64 = 0;
        while k < count {
            let s = spans[2 * k];
            let length = spans[2 * k + 1] - s;
            if length == at - began {
                var same = true;
                for b in length {
                    if words[s + b] != words[began + b] {
                        same = false;
                        break;
                    }
                }
                if same {
                    report.malformed = true;
                    return false;
                }
            }
            k += 1;
        }
        spans.push(began);
        spans.push(at);
        count += 1;
    }
    if count > v.max_entries {
        report.reason = MsgRefuseVocabularyTooLarge;
        return false;
    }
    // kept: the slots first, then the element shapes they name
    v.entries.clear();
    v.entries.resize(count, MsgEntry { .. });
    at = 0;
    for i in count {
        var e = MsgEntry { .. };
        if !msg_entry(words, at, e, v, true) {
            report.malformed = true;
            return false;
        }
        v.entries[i] = e;
    }
    v.count = count;
    v.ref_bits = if count <= 0 { 1 } else { msg_bits_required(0, count) };
    v.announced = true;
    return true;
}

// announce_read is AnnounceRead (docs/SPEC-TABLES.md §3.3): it reads the
// peer's announcement into ONE direction's vocabulary. The first announcement
// sets it and is the only one that can: a second, or any after a refused
// first, is refused `second_announcement` and changes nothing. A refused or
// malformed announcement sets no vocabulary, for the connection's life.
fn announce_read(v: MsgVocabulary&, data: const u8[:], report: MsgReport&) -> bool {
    report = MsgReport { .. };
    if v.announced || v.refused {
        report.reason = MsgRefuseSecondAnnouncement;
        return false;
    }
    let ok = announce_once(v, data, report);
    if !v.announced {
        v.refused = true;
        v.entries.clear();
    }
    return ok;
}

// ---- the reader -------------------------------------------------------------

// MsgReader is the bit reader over a batch: the bytes, the bit position from
// byte 0, the end, the vocabulary every reference resolves against, ok false
// at the first damage (or a refusal, reason beside it), and §4's counters.
struct MsgReader {
    b: const u8[:],
    pos: i64,
    end: i64,
    voc: const MsgVocabulary&,
    ok: bool = true,
    reason: i64 = 0,
    unknown: i64 = 0,
    kind_mismatch: i64 = 0,
    widened: i64 = 0,
    clamped: i64 = 0,
    depth: i64 = 0,
}

fn msg_has(r: MsgReader&, n: i64) -> bool {
    return n >= 0 && r.pos + n <= r.end;
}

// msg_get reads n bits (0 <= n <= 64); past the end it sets ok false.
fn msg_get(r: MsgReader&, n: i64) -> u64 {
    if !msg_has(r, n) {
        r.ok = false;
        return 0;
    }
    var v: u64 = 0;
    var got: i64 = 0;
    while got < n {
        let byte = r.b[r.pos >> 3];
        let at = r.pos & 7;
        var take = 8 - at;
        if n - got < take { take = n - got; }
        let mask: u64 = (1 << (take as u64)) - 1;
        let chunk: u64 = (byte >> (at as u64)) & mask;
        v |= chunk << (got as u64);
        got += take;
        r.pos += take;
    }
    return v;
}

// msg_align_read steps to the next byte boundary and VERIFIES THE PAD IS ZERO
// (docs/SPEC-TABLES.md §3.3: every align's pad bits inside a batch are
// verified zero, not only the final pad).
fn msg_align_read(r: MsgReader&) -> bool {
    let rem = r.pos & 7;
    if rem != 0 {
        if msg_get(r, 8 - rem) != 0 { r.ok = false; }
    }
    return r.ok;
}

// msg_skip_bits steps over n bits.
fn msg_skip_bits(r: MsgReader&, n: i64) -> bool {
    if !msg_has(r, n) {
        r.ok = false;
        return false;
    }
    r.pos += n;
    return true;
}

fn msg_damage(r: MsgReader&) -> bool {
    r.ok = false;
    return false;
}

fn msg_refuse_read(r: MsgReader&, code: i64) -> bool {
    r.ok = false;
    r.reason = code;
    return false;
}

fn msg_reserved(id: u64) -> bool { id >= VocabularyWireId }

// msg_field reads one FIELD's reference and answers the entry it names, or -1
// at the zero reference that ends the body. A reference above the vocabulary,
// or one naming a reserved id, is damage (§3.1, §3.3): ok goes false.
fn msg_field(r: MsgReader&) -> i64 {
    let ref = msg_get(r, r.voc.ref_bits);
    if ref == 0 || !r.ok { return -1; }
    if ref > r.voc.count as! u64 {
        r.ok = false;
        return -1;
    }
    let e = (ref - 1) as! i64;
    if msg_reserved(r.voc.entries[e].id) {
        r.ok = false;
        return -1;
    }
    return e;
}

// msg_name resolves a reference used as a VALUE (an enum's variant, a keyed
// array's key), which must name a kind-0 entry; -1 is the zero reference. An
// entry of the wrong sort is damage (§3.3).
fn msg_name(r: MsgReader&) -> i64 {
    let ref = msg_get(r, r.voc.ref_bits);
    if ref == 0 || !r.ok { return -1; }
    if ref > r.voc.count as! u64 {
        r.ok = false;
        return -1;
    }
    let e = (ref - 1) as! i64;
    if msg_reserved(r.voc.entries[e].id) || r.voc.entries[e].kind != 0 {
        r.ok = false;
        return -1;
    }
    return e;
}

// msg_arm resolves a UNION's arm reference, which must name an entry carrying
// the arm's own kind and shape; -1 is None.
fn msg_arm(r: MsgReader&) -> i64 {
    let ref = msg_get(r, r.voc.ref_bits);
    if ref == 0 || !r.ok { return -1; }
    if ref > r.voc.count as! u64 {
        r.ok = false;
        return -1;
    }
    let e = (ref - 1) as! i64;
    if msg_reserved(r.voc.entries[e].id) || r.voc.entries[e].kind == 0 {
        r.ok = false;
        return -1;
    }
    return e;
}

fn msg_id(r: MsgReader&, e: i64) -> u64 { r.voc.entries[e].id }

fn msg_inner(r: MsgReader&, e: i64) -> i64 { r.voc.entries[e].inner }

// msg_widens is §4's widening rule read off the ANNOUNCEMENT (internal/tablewire
// messageWidens): a kind below this reader's on the same ladder, or where both
// sides announce an array, an element kind below this reader's.
fn msg_widens(their_kind: u8, their_elem: u8, kind: u8, elem: u8) -> bool {
    if their_elem == elem { return widens(their_kind, kind); }
    if their_kind == kind && kind == 14 { return widens(their_elem, elem); }
    return false;
}

// msg_accept compares the announced entry with this reader's declaration of
// the same id: 0 the same kind, 1 WIDENED (counted; the payload decodes at the
// sender's shape), 2 a KIND MISMATCH, counted and stepped over by the
// announced shape, -1 damage on that step (§3.3, §4).
fn msg_accept(r: MsgReader&, e: i64, kind: u8, elem: u8) -> i64 {
    let their_kind = r.voc.entries[e].kind;
    let their_elem = r.voc.entries[e].elem;
    if their_kind == kind && their_elem == elem { return 0; }
    if msg_widens(their_kind, their_elem, kind, elem) {
        r.widened += 1;
        return 1;
    }
    r.kind_mismatch += 1;
    if !msg_skip(r, e) {
        r.ok = false;
        return -1;
    }
    return 2;
}

// msg_unknown counts an entry this reader cannot name and steps over it by
// its announced shape (§3.3); false is damage.
fn msg_unknown(r: MsgReader&, e: i64) -> bool {
    r.unknown += 1;
    if !msg_skip(r, e) { r.ok = false; }
    return r.ok;
}

// msg_run_bits is the bits ONE element of an array occupies where that is a
// number the reader can multiply, and -1 where elements are walked one at a
// time because each resolves something (internal/tablewire elementRunBits).
fn msg_run_bits(r: MsgReader&, e: i64) -> i64 {
    if r.voc.entries[e].kind != 14 { return -1; }
    let elem = r.voc.entries[e].elem;
    if elem == 13 || elem == 15 || elem == 30 || elem == 17 { return -1; }
    if elem == 0 || elem == 32 { return 0; }
    let inner = r.voc.entries[e].inner;
    if inner < 0 { return -1; }
    return r.voc.entries[inner].bits;
}

// msg_skip_run steps over n elements of one fixed width in one step.
fn msg_skip_run(r: MsgReader&, n: u64, width: i64) -> bool {
    if width == 0 { return true; }
    if width < 0 || n > (0x7FFFFFFFFFFFFFFF / (width as! u64)) { return false; }
    let total = (n * (width as! u64)) as! i64;
    if !msg_has(r, total) { return false; }
    r.pos += total;
    return true;
}

// msg_skip steps past one entry's payload by its announced shape alone
// (internal/tablewire skip; docs/SPEC-TABLES.md §3.3). The walk is bounded at
// depth 128 (message_skip_depth): a nested body, an arm or an element past it
// is malformed.
recursive fn msg_skip(r: MsgReader&, e: i64) -> bool {
    if r.depth > 128 { return false; }
    let kind = r.voc.entries[e].kind;
    if kind == 0 || kind == 32 { return true; }
    if kind == 30 {
        // a variant reference is resolved even on the skip path
        msg_name(r);
        return r.ok;
    }
    if kind == 15 {
        let arm = msg_arm(r);
        if !r.ok { return false; }
        if arm < 0 { return true; }
        r.depth += 1;
        let ok = msg_skip(r, arm);
        r.depth -= 1;
        return ok;
    }
    if kind == 13 {
        r.depth += 1;
        let ok = msg_skip_body(r);
        r.depth -= 1;
        return ok;
    }
    if kind == 17 {
        // a fixed root numbers no node: an index has no width to read at
        return false;
    }
    let max = r.voc.entries[e].max;
    if kind == 12 {
        let n = msg_get(r, msg_bits_required(0, max));
        if !r.ok || !msg_align_read(r) { return false; }
        return msg_skip_run(r, n, 8);
    }
    if kind == 31 {
        if !msg_align_read(r) { return false; }
        let n = msg_get(r, 32);
        if !r.ok { return false; }
        return msg_skip_run(r, n, 8);
    }
    if kind == 14 || kind == 16 {
        let min = r.voc.entries[e].min;
        var n: u64 = min as! u64;
        var width = msg_bits_required(min, max);
        if kind == 16 {
            width = msg_bits_required(0, max);
            n = 0;
        }
        if width > 0 {
            n = msg_get(r, width);
            if !r.ok { return false; }
            if kind == 14 { n += min as! u64; }
        }
        let elem = r.voc.entries[e].elem;
        if kind == 14 && elem == 6 && !msg_align_read(r) { return false; }
        let run = msg_run_bits(r, e);
        if run >= 0 { return msg_skip_run(r, n, run); }
        let inner = r.voc.entries[e].inner;
        var i: u64 = 0;
        while i < n {
            let at = r.pos;
            if kind == 16 {
                msg_get(r, r.voc.ref_bits);
                if !r.ok { return false; }
            }
            var ok = false;
            if inner >= 0 {
                r.depth += 1;
                ok = msg_skip(r, inner);
                r.depth -= 1;
            } else {
                ok = elem == 0 || elem == 32;
            }
            if !ok { return false; }
            // A WALKED ELEMENT THAT CONSUMED NO BITS: the rest of the run is
            // stepped in one, and a count the bits left cannot back is damage
            if r.pos == at {
                if n - i - 1 > (r.end - r.pos) as! u64 { return false; }
                return true;
            }
            i += 1;
        }
        return true;
    }
    let width = r.voc.entries[e].bits;
    if width < 0 { return false; }
    return msg_skip_bits(r, width);
}

// msg_skip_body walks a nested body it cannot name, field by field.
fn msg_skip_body(r: MsgReader&) -> bool {
    loop {
        let ref = msg_get(r, r.voc.ref_bits);
        if !r.ok { return false; }
        if ref == 0 { return true; }
        if ref > r.voc.count as! u64 { return false; }
        if !msg_skip(r, (ref - 1) as! i64) { return false; }
    }
    return false;
}

// ---- payloads at the SENDER's shape (docs/SPEC-TABLES.md §3.3) --------------

// msg_number reads ONE integer or fixed-point value at the width and against
// the base the announced entry states, and answers it as two's-complement
// lanes (lo, hi): sign-extended from a signed kind, zero-extended from an
// unsigned one, so a widened value lands whole. ok false is damage.
fn msg_number(r: MsgReader&, e: i64) -> u64, u64 {
    let kind = r.voc.entries[e].kind;
    let width = r.voc.entries[e].bits;
    if width < 0 || width > 128 {
        r.ok = false;
        return 0, 0;
    }
    var lo: u64 = 0;
    var hi: u64 = 0;
    if width > 64 {
        lo = msg_get(r, 64);
        hi = msg_get(r, width - 64);
    } else {
        lo = msg_get(r, width);
    }
    if !r.ok { return 0, 0; }
    if r.voc.entries[e].packing == 1 {
        let before = lo;
        lo += r.voc.entries[e].base_lo;
        if msg_wide_kind(kind) {
            hi = hi + r.voc.entries[e].base_hi + (if lo < before { 1 } else { 0 });
        }
    } else if msg_signed_kind(kind) && width > 0 && width < 64 && ((lo >> ((width - 1) as! u64)) & 1) == 1 {
        lo |= 0xFFFFFFFFFFFFFFFF << (width as! u64);
    }
    if msg_signed_kind(kind) {
        hi = if (lo as! i64) < 0 { 0xFFFFFFFFFFFFFFFF } else { 0 };
    }
    return lo, hi;
}

// msg_f32 reads a kind-10 value at the sender's packing: the IEEE bits raw,
// or the float the quantized index names (an index above the step count is
// damage, SPEC.md §4.3). A NaN or an infinity refuses the batch on its raw bits.
fn msg_f32(r: MsgReader&, e: i64) -> f32 {
    if r.voc.entries[e].packing == 2 {
        let index = msg_get(r, r.voc.entries[e].bits);
        if !r.ok { return 0.0; }
        let count = r.voc.entries[e].qcount;
        if index > count {
            r.ok = false;
            return 0.0;
        }
        return msg_dequantize(index, r.voc.entries[e].qmin, r.voc.entries[e].qdelta, count);
    }
    let raw = msg_get(r, 32) as! u32;
    if !r.ok { return 0.0; }
    if !msg_finite32(raw) {
        msg_refuse_read(r, MsgRefuseNonFiniteFloat);
        return 0.0;
    }
    return fixed_bits_f32(raw);
}

// msg_f64 reads a value into a float64: kind 11's sixty-four bits, or a
// kind-10 value WIDENED (§4), which every float32 value is exactly.
fn msg_f64(r: MsgReader&, e: i64) -> f64 {
    if r.voc.entries[e].kind == 10 {
        return msg_f32(r, e);
    }
    let raw = msg_get(r, 64);
    if !r.ok { return 0.0; }
    if !msg_finite64(raw) {
        msg_refuse_read(r, MsgRefuseNonFiniteFloat);
        return 0.0;
    }
    return fixed_bits_f64(raw);
}

// msg_bool reads a kind-1 value.
fn msg_bool(r: MsgReader&) -> bool { msg_get(r, 1) != 0 }

// msg_count is an array's count at the announced shape: no count rides where
// `min` equals `max`, and an array of u8 aligns before its bytes (§3.3).
fn msg_count(r: MsgReader&, e: i64) -> i64 {
    let min = r.voc.entries[e].min;
    let width = msg_bits_required(min, r.voc.entries[e].max);
    var n = min;
    if width > 0 {
        n = (msg_get(r, width) as! i64) + min;
    }
    if r.voc.entries[e].kind == 14 && r.voc.entries[e].elem == 6 {
        msg_align_read(r);
    }
    return n;
}

// msg_keyed_count is a keyed array's present-slot count (§3.2).
fn msg_keyed_count(r: MsgReader&, e: i64) -> i64 {
    return msg_get(r, msg_bits_required(0, r.voc.entries[e].max)) as! i64;
}

// msg_text_len is a string's or a bytes' length at the announced shape: a
// string's at bits_required(0, max) and then the align, a bytes' at its
// count width and then the same align; the bytes must be there.
fn msg_text_len(r: MsgReader&, e: i64) -> i64 {
    var width = msg_bits_required(0, r.voc.entries[e].max);
    if r.voc.entries[e].kind == 14 {
        width = msg_bits_required(r.voc.entries[e].min, r.voc.entries[e].max);
    }
    let n = msg_get(r, width) as! i64;
    if !r.ok || !msg_align_read(r) || !msg_has(r, n * 8) {
        r.ok = false;
        return 0;
    }
    return n;
}

// ---- floats -----------------------------------------------------------------

// A NON-FINITE FLOAT IS REFUSED on every form (docs/SPEC-TABLES.md §3.3): on
// the write before the quantizer runs, on the read for the whole batch.
fn msg_finite32(u: u32) -> bool {
    return (u & 0x7F800000) != 0x7F800000;
}

fn msg_finite64(u: u64) -> bool {
    return (u & 0x7FF0000000000000) != 0x7FF0000000000000;
}

// msg_quantize is the compressed float's index (docs/SPEC-TABLES.md §3.3,
// SPEC.md §4.3): clamp (value - min) / delta to [0, 1], multiply by count and
// round THAT product to float32, add 0.5, floor, clamp the integer to count.
// Every product is its own statement, so no two roundings fuse.
fn msg_quantize(value: f32, qmin: f32, delta: f32, count: u32) -> u64 {
    let shifted: f32 = value - qmin;
    var normalized: f32 = shifted / delta;
    if !(normalized >= 0.0) {
        normalized = 0.0;
    } else if !(normalized <= 1.0) {
        normalized = 1.0;
    }
    let total: f32 = count;
    let scaled: f32 = normalized * total;
    let half: f32 = scaled + 0.5;
    var index: u64 = half as! u64;
    if index > count { index = count; }
    return index;
}

// msg_dequantize is the reader's half: index / count in float32, times delta
// rounded to float32, plus min.
fn msg_dequantize(index: u64, qmin: f32, delta: f32, count: u32) -> f32 {
    let numerator: f32 = index;
    let total: f32 = count;
    let normalized: f32 = numerator / total;
    let scaled: f32 = normalized * delta;
    let out: f32 = scaled + qmin;
    return out;
}

// ---- text -------------------------------------------------------------------

// msg_utf8_keep is how many of a string's n bytes at `at` a reader keeps under
// its own bound: n when it fits, else the bound backed off to a character
// boundary (a continuation byte never starts the dropped tail).
fn msg_utf8_keep(b: const u8[:], at: i64, n: i64, bound: i64) -> i64 {
    if n <= bound { return n; }
    var k = bound;
    while k > 0 && (b[at + k] & 0xC0) == 0x80 {
        k -= 1;
    }
    return k;
}
