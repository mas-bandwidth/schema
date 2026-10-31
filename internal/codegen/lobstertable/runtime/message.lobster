// message.lobster — the Lobster table runtime's MESSAGE FORM, form byte 2
// (docs/SPEC-TABLES.md §3.3; docs/PORT-LOBSTER.md, THE CARD MAP): the
// announcement and its reader, the connection's VOCABULARY, and the bitpacked
// BATCH of bodies of one root with its three verbs, load_messages,
// measure_messages and save_messages. A port of the Go generated runtime
// (internal/codegen/gotable/message.go, messageread.go, messagewrite.go) and of
// the reference engine's walk (internal/tablewire message*.go) onto the
// descriptors schema_tables.lobster declares: every rule lives once, here.
//
// THE UNIT'S FACTS are compile-time constants the table modules register
// (internal/codegen/lobstertable/messageform.go): the announcement bytes and
// the build version, the roots that may ride (a FIXED table's, §3.3), and every
// field's and every union arm's SLOT in the unit's own vocabulary, so a writer
// never looks a slot up by shape. The writer reads the payload widths off the
// unit's own announcement, resolved once (own_vocabulary).
//
// THE STORAGE is the load's Record (docs/PORT-LOBSTER.md, THE LOAD), and a
// scalar is decoded by reconstructing the announced value at the announced
// kind's storage width and handing those bytes to load.lobster's scalar_at,
// so the file form's clamps, widening and NaN rules are the message form's
// too (§3.3, "Typed readers then apply the existing exact file-domain clamps").
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_save
import .schema_tables
import .descriptors
import .strict_and_refusals
import .wide_kinds

namespace schema_rt

// THE BATCH'S WIRE CONSTANT (§3.3): the count is eight bits carrying M - 1.
let message_batch_max = 256

// the conforming default byte bound on a vocabulary field (§3.3, SECURITY)
let message_vocabulary_max_bytes = 64 * 1024

// THE SKIP WALK'S DEPTH BOUND (§3.3): a nested body, union arm or element
// skip past 128 levels is refused as malformed.
let message_skip_depth = 128

// ---- the unit's facts, registered by the table modules ----

class MessageVocabulary

class MessageSlots:
    owner:string
    slots:[int]

class MessageUnitFacts:
    announcement:string = ""
    build_version:int = 0
    registered:bool = false
    roots:[string] = []
    fields:[MessageSlots] = []
    arms:[MessageSlots] = []
    own:MessageVocabulary? = nil

let message_facts = MessageUnitFacts {}

// message_unit registers the unit's announcement and build version
// (§3.3, "THE WHOLE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT").
// Every table module of the unit registers the same two facts.
def message_unit(announcement:string, build_version:int) -> int:
    if message_facts.registered:
        assert message_facts.announcement == announcement and message_facts.build_version == build_version
        return 0
    message_facts.announcement = announcement
    message_facts.build_version = build_version
    message_facts.registered = true
    return 1

// message_root registers a table that may ride the form: a FIXED table's
// (§3.3, "a plain `table T` may not").
def message_root(name:string) -> int:
    for(message_facts.roots) r:
        if r == name:
            return 0
    push(message_facts.roots, name)
    return 1

// message_slots registers one record's field slots in descriptor order, and
// message_arm_slots one union's arm slots in tag order; 0 is a field the
// unit's vocabulary does not spell, which no writer can put on this wire.
def message_slots(owner:string, slots:[int]) -> int:
    push(message_facts.fields, MessageSlots { owner: owner, slots: slots })
    return length(slots)

def message_arm_slots(owner:string, slots:[int]) -> int:
    push(message_facts.arms, MessageSlots { owner: owner, slots: slots })
    return length(slots)

def message_slots_of(list:[MessageSlots], owner:string) -> [int]:
    for(list) m:
        if m.owner == owner:
            return m.slots
    return []

// is_message_root: the table may ride the message form (§3.3).
def is_message_root(name:string) -> bool:
    for(message_facts.roots) r:
        if r == name:
            return true
    return false

// message_build_version is the unit's own build version (§20.5).
def message_build_version() -> int:
    return message_facts.build_version

// ---- the vocabulary ----

// A MessageShape is one announced shape, resolved (Go's TableMessageShape):
// the kind, the packing, the payload's width already resolved out of the kind
// and the announced bits (-1 where the payload is not a fixed-width value),
// the ranged base as its two lanes, an array's or a string's bounds, and the
// quantized facts §4.3's rule leaves behind.
class MessageShape:
    kind:int = 0
    packing:int = 0
    bits:int = 0
    base_lo:int = 0
    base_hi:int = 0
    min:int = 0
    max:int = 0
    qmin:float = 0.0
    qdelta:float = 0.0
    qcount:int = 0

// A MessageEntry is one announced entry: the id, its shape, and an array's or
// a keyed array's element shape.
class MessageEntry:
    id:int
    shape:MessageShape
    element:MessageShape

// A MessageVocabulary is ONE DIRECTION's resolved vocabulary (§3.3): it lives
// with the connection, is filled by the first announcement alone, and its
// capacity is the caller's declared bound. A refused first announcement is
// terminal: refused stays set and nothing is ever announced on it.
class MessageVocabulary:
    entries:[MessageEntry] = []
    capacity:int = 4096
    count:int = 0
    ref_bits:int = 1
    build_version:int = 0
    build_version_own:int = 0
    announced:bool = false
    refused:bool = false
    max_bytes:int = 65536
    strict:bool = false

// new_vocabulary is a fresh direction declared at capacity entries.
def new_vocabulary(capacity:int) -> MessageVocabulary:
    return MessageVocabulary { entries: [], capacity: capacity }

def message_entry(v:MessageVocabulary, ref:int) -> MessageEntry?:
    if ref == 0 or ult(v.count, ref):
        return nil
    return v.entries[ref - 1]

// message_name resolves a reference used as a VALUE, which must name a kind-0
// entry and no reserved id (§3.3).
def message_name(v:MessageVocabulary, ref:int) -> int, bool:
    let e = message_entry(v, ref)
    if not e:
        return 0, false
    if e.shape.kind != 0 or not ult(e.id, message_vocabulary_id):
        return 0, false
    return e.id, true

// own_vocabulary is the unit's own vocabulary, resolved once from its own
// announcement: the writer's widths, and the slots its references carry.
def own_vocabulary() -> MessageVocabulary:
    let held = message_facts.own
    if held:
        return held
    assert message_facts.registered
    let v = new_vocabulary(1 << 20)
    let rep = LoadReport {}
    let ok = announce_read(v, message_facts.announcement, rep)
    assert ok
    message_facts.own = v
    return v

// message_entries_here is the unit's own entry count, the exact capacity a
// receiver that talks only to senders of its own schema declares (§3.3).
def message_entries_here() -> int:
    return own_vocabulary().count

// message_bits is the bit length of an unsigned word: bits_required(0, n).
def message_bits(n:int) -> int:
    var c = 0
    var x = n
    while x != 0:
        c++
        x = shr(x, 1)
    return c

def message_known_kind(k:int) -> bool:
    return k <= 32 or k == 36

def message_kind_bits(k:int) -> int:
    if k == 0 or k == 32:
        return 0
    if k == 1:
        return 1
    if k == 2 or k == 6 or k == 20 or k == 25:
        return 8
    if k == 3 or k == 7 or k == 21 or k == 26:
        return 16
    if k == 4 or k == 8 or k == 10 or k == 22 or k == 27:
        return 32
    if k == 5 or k == 9 or k == 11 or k == 23 or k == 28 or k == 36:
        return 64
    if k == 18 or k == 19 or k == 24 or k == 29:
        return 128
    return -1

// ---- the announcement ----

// announce_measure and announce are the unit's announcement, a constant
// (§3.3): a backend emits it as a byte array rather than as a walk.
def announce_measure() -> int:
    return length(message_facts.announcement)

def announce() -> string:
    return message_facts.announcement

def message_refuse(rep:LoadReport, reason:string) -> bool:
    rep.refused = true
    rep.reason = reason
    return false

// message_quantization is §4.3's derivation in float32 and nothing else: the
// step count and delta, and false for a non-conforming triple.
def message_quantization(lo:float, hi:float, res:float) -> float, int, bool:
    if not (lo < hi) or not (res > 0.0):
        return 0.0, 0, false
    let delta = fround(hi - lo)
    var values = fround(delta / res)
    if delta - delta != 0.0 or values - values != 0.0:
        return 0.0, 0, false
    if not (values >= 1.0):
        values = 1.0
    elif values > 4294967040.0:
        values = 4294967040.0
    return delta, ceiling(values), true

// message_shape_read reads one shape at kind k (Go's tableMessageShapeRead).
def message_shape_read(r:Reader, k:int) -> MessageShape, bool:
    let s = MessageShape { kind: k, bits: message_kind_bits(k) }
    if (k >= 2 and k <= 10) or (k >= 18 and k <= 29):
        if not has(r, 1):
            return s, false
        s.packing = u8(r)
        if s.packing == 0:
            return s, true
        if s.packing == 1 and k != 10:
            let n, ok = leb(r)
            if not ok or ult(s.bits, n):
                return s, false
            s.bits = n
            if k >= 18:
                if not has(r, 16):
                    return s, false
                s.base_lo = read_int64_le(r.b, r.off)
                s.base_hi = read_int64_le(r.b, r.off + 8)
                r.off += 16
                return s, true
            let base, bok = leb(r)
            if not bok:
                return s, false
            if k >= 2 and k <= 5:
                // a signed kind's base is a zigzag LEB128 (§3.3)
                s.base_lo = shr(base, 1) ^ (0 - (base & 1))
            else:
                s.base_lo = base
            return s, true
        if s.packing == 2 and k == 10:
            if not has(r, 12):
                return s, false
            let lo = read_float32_le(r.b, r.off)
            let hi = read_float32_le(r.b, r.off + 4)
            let res = read_float32_le(r.b, r.off + 8)
            r.off += 12
            let delta, count, ok = message_quantization(lo, hi, res)
            if not ok:
                return s, false
            s.qmin = lo
            s.qdelta = delta
            s.qcount = count
            s.bits = message_bits(count)
            return s, true
        return s, false
    if k == 12:
        let n, ok = leb(r)
        if not ok or ult(0x7FFFFFFF, n):
            return s, false
        s.max = n
    elif k == 14 or k == 16:
        if k == 14:
            let n, ok = leb(r)
            if not ok or ult(0xFFFFFFFF, n):
                return s, false
            s.min = n
        let n, ok = leb(r)
        if not ok or ult(0xFFFFFFFF, n) or n < s.min:
            return s, false
        s.max = n
    return s, true

// message_entry_read reads one entry: the id, the kind, the shape, and an
// array's or keyed array's element (Go's tableMessageEntryRead).
def message_entry_read(r:Reader) -> MessageEntry, bool:
    let e = MessageEntry { id: 0, shape: MessageShape {}, element: MessageShape {} }
    if not has(r, 9):
        return e, false
    e.id = read_int64_le(r.b, r.off)
    r.off += 8
    let kind = u8(r)
    if not message_known_kind(kind):
        return e, false
    let shape, ok = message_shape_read(r, kind)
    e.shape = shape
    if not ok:
        return e, false
    if kind == 14 or kind == 16:
        if not has(r, 1):
            return e, false
        let elem = u8(r)
        if not message_known_kind(elem) or elem == 12:
            return e, false
        let es, eok = message_shape_read(r, elem)
        e.element = es
        if not eok:
            return e, false
        if elem == 14 or elem == 16:
            if not has(r, 1):
                return e, false
            let inner = u8(r)
            if not message_known_kind(inner) or inner == 12:
                return e, false
    return e, true

// message_open is the announcement's form-1 open (§3): the form byte, the id
// trailer, and the framing pre-walk that tells a body that ENDED EARLY from
// one merely damaged. The verdict is 0 open, 1 refused, 2 damaged.
def message_open(data:string, rep:LoadReport) -> Reader?, int:
    let size = length(data)
    if size == 0:
        return nil, 2
    if data[0] != 1:
        return nil, 1
    if size < 9:
        return nil, 2
    let n = read_int64_le(data, size - 8)
    if ult((size - 9) / 8, n):
        return nil, 2
    let start = size - 8 - n * 8
    let ids:[int] = []
    var i = 0
    while i < n:
        push(ids, read_int64_le(data, start + i * 8))
        i++
    let st = LoadState { data: data, ids: ids, report: rep }
    let r = reader(st, 1, start)
    // the framing pre-walk, on a scratch report
    let scratch = LoadState { data: data, ids: ids, report: LoadReport {} }
    let w = reader(scratch, 1, start)
    while true:
        let ref, ok = leb(w)
        if not ok:
            return r, 0
        if ref == 0:
            if w.off != w.hi:
                return r, 2
            return r, 0
        let _, named = rid(w, ref)
        if not named or not has(w, 1):
            return r, 0
        if not skip(w, u8(w)):
            return r, 0
    return r, 0

// announce_inner is the one read a connection gets (Go's tableAnnounceRead).
def announce_inner(v:MessageVocabulary, data:string, rep:LoadReport) -> bool:
    let opened, verdict = message_open(data, rep)
    if verdict != 0:
        if verdict == 1:
            let reason = if length(data) > 0 and data[0] == 2: "message_form_as_file" else: "newer_form"
            return message_refuse(rep, reason)
        rep.malformed = true
        return false
    let r = opened
    assert r
    var version_count = 0
    var words_count = 0
    var words_lo = 0
    var words_n = 0
    while true:
        let ref, ok = leb(r)
        if not ok:
            rep.malformed = true
            return false
        if ref == 0:
            break
        let id, named = rid(r, ref)
        if not named or not has(r, 1):
            rep.malformed = true
            return false
        let kind = u8(r)
        if id == build_version_id:
            if kind != 9 or not has(r, 8):
                rep.malformed = true
                return false
            // THE BUILD VERSION IS KEPT THE MOMENT IT IS READ (§3.3)
            v.build_version = read_int64_le(r.b, r.off)
            r.off += 8
            version_count++
        elif id == message_vocabulary_id:
            if kind != 14:
                rep.malformed = true
                return false
            let body_n, bok = leb(r)
            if not bok or not has(r, body_n):
                rep.malformed = true
                return false
            let sr = sub(r, body_n)
            r.off += body_n
            if not has(sr, 1) or u8(sr) != 6:
                rep.malformed = true
                return false
            let n, nok = leb(sr)
            if not nok or n != sr.hi - sr.off:
                rep.malformed = true
                return false
            var limit = v.max_bytes
            if limit == 0:
                limit = message_vocabulary_max_bytes
            if n > limit:
                return message_refuse(rep, "vocabulary_too_large")
            words_lo = sr.off
            words_n = n
            words_count++
        else:
            rep.unknown++
            if not skip(r, kind):
                rep.malformed = true
                return false
    if version_count != 1 or words_count != 1:
        rep.malformed = true
        return false
    if v.strict and v.build_version != message_facts.build_version:
        v.build_version_own = message_facts.build_version
        return message_refuse(rep, "build_version_mismatch")
    let wst = LoadState { data: substring(data, words_lo, words_n), ids: [], report: LoadReport {} }
    let src = reader(wst, 0, words_n)
    let entries:[MessageEntry] = []
    let starts:[int] = []
    let ends:[int] = []
    var node_seen = false
    while src.off < words_n:
        if length(entries) >= v.capacity:
            return message_refuse(rep, "vocabulary_too_large")
        let began = src.off
        let e, eok = message_entry_read(src)
        if not eok or e.id == message_vocabulary_id or e.id == build_version_id or (e.id == node_table_id and node_seen):
            rep.malformed = true
            return false
        if e.id == node_table_id:
            node_seen = true
        // A TRIPLE ALREADY PLACED IS NEVER PLACED TWICE (§3.3): the entry's
        // encoding is canonical, so byte equality is declared-fact equality
        let span = src.off - began
        for(starts) a, j:
            if ends[j] - a == span and substring(wst.data, a, span) == substring(wst.data, began, span):
                rep.malformed = true
                return false
        push(starts, began)
        push(ends, src.off)
        push(entries, e)
    v.entries = entries
    v.count = length(entries)
    v.ref_bits = message_bits(v.count)
    if v.ref_bits == 0:
        v.ref_bits = 1
    v.announced = true
    return true

// announce_read reads the sender's announcement into this direction's vocabulary
// (§3.3, Go's AnnounceRead). THE FIRST ANNOUNCEMENT SETS THE VOCABULARY, AND
// IT IS THE ONLY ONE THAT CAN; REFUSAL IS TERMINAL.
def announce_read(v:MessageVocabulary, data:string, rep:LoadReport) -> bool:
    if v.announced or v.refused:
        return message_refuse(rep, "second_announcement")
    let ok = announce_inner(v, data, rep)
    if not ok:
        v.refused = true
    return ok

// ---- the bit stream ----

// MessageBits is the batch's bit cursor (Go's TableBitReader): the bits of
// b[lo:], low bit first, the packet wire's order (§3.3). Every align is
// relative to the batch's byte 0, and lo is 2, so a byte boundary here is one
// there.
class MessageBits:
    b:string
    lo:int
    n:int
    off:int = 0

def mb_get(r:MessageBits, n:int) -> int, bool:
    if n < 0 or n > 64 or r.off < 0 or n > r.n - r.off:
        return 0, false
    var v = 0
    var got = 0
    var at = r.off
    while got < n:
        let byte = r.b[r.lo + (at >> 3)]
        let shift = at & 7
        var take = 8 - shift
        if take > n - got:
            take = n - got
        let part = (byte >> shift) & ((1 << take) - 1)
        v = v | shl(part, got)
        got += take
        at += take
    r.off = at
    return v, true

def mb_skip(r:MessageBits, n:int) -> bool:
    if n < 0 or n > r.n - r.off:
        return false
    r.off += n
    return true

// mb_align reads the pad to the next byte boundary, which must be zero.
def mb_align(r:MessageBits) -> bool:
    let v, ok = mb_get(r, (0 - r.off) & 7)
    return ok and v == 0

// MessageWriter is the batch's bit writer (Go's TableBitWriter): measuring
// walks the identical stream, alignment included, without a buffer.
class MessageWriter:
    b:string = ""
    bits:int = 0
    measuring:bool = false
    overflow:bool = false
    clamped:int = 0
    ref_bits:int = 1

def mw_put(w:MessageWriter, value:int, n:int):
    if n < 0 or n > 64:
        w.overflow = true
        return
    if n == 0:
        return
    if w.measuring:
        w.bits += n
        return
    let need = (w.bits + n + 7) >> 3
    if length(w.b) < need:
        w.b = ensure_size(w.b, need, 0)
    var x = value
    var left = n
    while left > 0:
        let at = w.bits >> 3
        let shift = w.bits & 7
        var take = 8 - shift
        if take > left:
            take = left
        let part = (x & ((1 << take) - 1)) << shift
        w.b = write_int8_le(w.b, at, w.b[at] | part)
        x = shr(x, take)
        left -= take
        w.bits += take

def mw_align(w:MessageWriter):
    mw_put(w, 0, (0 - w.bits) & 7)

def mw_raw(w:MessageWriter, data:string):
    mw_align(w)
    for(data) c:
        mw_put(w, c, 8)

// ---- the read ----

// MessageRead is one batch's read (Go's TableMessageReader): the cursor, the
// announced vocabulary and the report. A fixed root numbers no node, so its
// index width is 0 and a pointer index cannot be read (§3.1, §3.3).
class MessageRead:
    bits:MessageBits
    v:MessageVocabulary
    rep:LoadReport
    index_bits:int = 0

def message_count(r:MessageBits, s:MessageShape) -> int, bool:
    let raw, ok = mb_get(r, message_bits(s.max - s.min))
    return raw + s.min, ok

// message_skip steps over one payload by its announced entry alone (Go's
// tableMessageSkip): a variant and an arm reference are resolved even here.
def message_skip(m:MessageRead, s:MessageShape, el:MessageShape, depth:int) -> bool:
    if depth > message_skip_depth:
        return false
    let r = m.bits
    let v = m.v
    let k = s.kind
    if k == 0 or k == 32:
        return true
    if k == 30:
        let ref, ok = mb_get(r, v.ref_bits)
        if not ok or ref == 0:
            return ok
        let _, named = message_name(v, ref)
        return named
    if k == 13:
        return message_skip_body(m, depth + 1)
    if k == 17:
        return m.index_bits > 0 and mb_skip(r, m.index_bits)
    if k == 15:
        let ref, ok = mb_get(r, v.ref_bits)
        if not ok or ref == 0:
            return ok
        let arm = message_entry(v, ref)
        if not arm:
            return false
        return ult(arm.id, message_vocabulary_id) and arm.shape.kind != 0 and message_skip(m, arm.shape, arm.element, depth + 1)
    if k == 12:
        let n, ok = message_count(r, s)
        if not ok:
            return false
        var width = 16
        if k == 12:
            width = 8
            if not mb_align(r):
                return false
        return mb_skip(r, n * width)
    if k == 31:
        if not mb_align(r):
            return false
        let n, ok = mb_get(r, 32)
        return ok and mb_skip(r, n * 8)
    if k == 14 or k == 16:
        let n, ok = message_count(r, s)
        if not ok:
            return false
        if k == 14 and el.kind == 6 and not mb_align(r):
            return false
        if el.bits >= 0 or el.kind == 0 or el.kind == 32:
            var width = max(el.bits, 0)
            if k == 16:
                width += v.ref_bits
            return mb_skip(r, n * width)
        var i = 0
        while i < n:
            if k == 16 and not mb_skip(r, v.ref_bits):
                return false
            let before = r.off
            if not message_skip(m, el, MessageShape {}, depth + 1) or r.off == before:
                return false
            i++
        return true
    return s.bits >= 0 and mb_skip(r, s.bits)

def message_skip_body(m:MessageRead, depth:int) -> bool:
    if depth > message_skip_depth:
        return false
    while true:
        let ref, ok = mb_get(m.bits, m.v.ref_bits)
        if not ok or ref == 0:
            return ok
        let e = message_entry(m.v, ref)
        if not e:
            return false
        if not message_skip(m, e.shape, e.element, depth):
            return false
    return false

// message_scalar reads ONE value at the SENDER's announced width and base,
// reconstructs it at the announced kind's storage width (Go's
// tableMessageReadScalar), and lands it through the file form's own clamps
// (scalar_at). A quantized index above count is REJECTED, as the packet wire
// rejects it (§3.3, SPEC.md §4.3).
def message_scalar(m:MessageRead, cell:Value, fd:FieldDescriptor, declared:int, s:MessageShape) -> bool:
    let r = m.bits
    let rep = m.rep
    let low_bits = min(s.bits, 64)
    let first, ok = mb_get(r, low_bits)
    var lo = first
    if not ok:
        rep.malformed = true
        return false
    var hi = 0
    if s.bits > 64:
        let h, hok = mb_get(r, s.bits - 64)
        if not hok:
            rep.malformed = true
            return false
        hi = h
    if s.packing == 1:
        let sum = lo + s.base_lo
        let carry = if ult(sum, lo): 1 else: 0
        lo = sum
        hi = hi + s.base_hi + carry
    if s.packing == 2:
        if ult(s.qcount, lo):
            rep.malformed = true
            return false
        let ratio = fround(fround(float(lo)) / fround(float(s.qcount)))
        let scaled = fround(ratio * s.qdelta)
        lo = f32bits(fround(scaled + s.qmin))
    let kind = s.kind
    let width = kind_width(kind)
    if width == 0:
        rep.malformed = true
        return false
    var raw = zeros(16)
    raw = write_int64_le(raw, 0, lo)
    raw = write_int64_le(raw, 8, hi)
    let st = LoadState { data: substring(raw, 0, width), ids: [], report: rep }
    let pr = reader(st, 0, width)
    if kind == 36:
        return wide_timestamp_at(pr, cell, fd)
    return scalar_at(pr, cell, fd, declared, kind)

// message_text reads a string(N): the length at its width, the align that
// buys a memcpy, the bytes; ill-formed text is terminal damage, and a clamp
// cuts at a code point boundary.
def message_text(m:MessageRead, sl:Slot, s:MessageShape) -> bool:
    let r = m.bits
    let rep = m.rep
    let n, ok = message_count(r, s)
    if not ok or not mb_align(r):
        rep.malformed = true
        return false
    let start = r.lo + (r.off >> 3)
    if not mb_skip(r, n * 8):
        rep.malformed = true
        return false
    if not text_valid(r.b, start, n):
        rep.malformed = true
        return false
    var kept = n
    if sl.fd.s == "str" and kept > sl.fd.bound:
        kept = text_boundary(r.b, start, n, sl.fd.bound)
    if kept < n:
        rep.clamped++
    sl.cell.s = substring(r.b, start, kept)
    sl.count = kept
    return true

// message_bytes reads a bytes(N): the count, the align, the bytes, clamped at
// the bound.
def message_bytes(m:MessageRead, sl:Slot, s:MessageShape) -> bool:
    let r = m.bits
    let rep = m.rep
    let n, ok = message_count(r, s)
    if not ok or not mb_align(r):
        rep.malformed = true
        return false
    let start = r.lo + (r.off >> 3)
    if not mb_skip(r, n * 8):
        rep.malformed = true
        return false
    var kept = n
    if kept > sl.fd.bound:
        kept = sl.fd.bound
        rep.clamped++
    sl.cell.s = substring(r.b, start, kept)
    sl.count = kept
    return true

// message_enum reads an enum value: the reference naming its VARIANT's name,
// 0 for None; a variant this reader cannot name is unknown and reads None, and
// a deprecated one reads the declared default (§2.15).
def message_enum(m:MessageRead, cell:Value, fd:FieldDescriptor, element:bool) -> bool:
    let rep = m.rep
    let ref, ok = mb_get(m.bits, m.v.ref_bits)
    if not ok:
        rep.malformed = true
        return false
    if ref == 0:
        cell.i = 0
        return true
    let id, named = message_name(m.v, ref)
    if not named:
        rep.malformed = true
        return false
    let value = enum_value(fd.vs, id)
    if value < 0:
        cell.i = 0
        rep.unknown++
        return true
    if is_deprecated_variant(fd.t, variant_name(fd, value)):
        cell.i = if element: 0 else: enum_default(fd)
        return true
    cell.i = value
    return true

// message_union reads a union: the ARM's reference, then the payload a FIELD
// of the arm's type would carry. An arm this reader cannot name is unknown
// and skipped by its own announced entry; a retyped arm is a kind mismatch
// unless its kind widens.
def message_union(m:MessageRead, cell:Value, fd:FieldDescriptor) -> bool:
    let rep = m.rep
    let v = m.v
    let ref, ok = mb_get(m.bits, v.ref_bits)
    if not ok:
        rep.malformed = true
        return false
    cell.tab = nil
    cell.arm = nil
    if ref == 0:
        cell.i = 0
        return true
    let e = message_entry(v, ref)
    if not e or not ult(e.id, message_vocabulary_id) or e.shape.kind == 0:
        rep.malformed = true
        return false
    let un = lookup_union(fd.t)
    assert un
    var tag = 0
    for(un.arms) a, i:
        if tag == 0 and a.id == e.id:
            tag = i + 1
    if tag == 0:
        cell.i = 0
        rep.unknown++
        if not message_skip(m, e.shape, e.element, 0):
            rep.malformed = true
            return false
        return true
    let a = un.arms[tag - 1]
    let slots = message_slots_of(message_facts.arms, un.name)
    let mine = message_own_entry(slots, tag - 1)
    var widened = false
    if e.shape.kind != mine.shape.kind or e.element.kind != mine.element.kind:
        if a.s != "void" and message_compatible(a, mine, e):
            widened = true
        else:
            cell.i = 0
            rep.kind_mismatch++
            if not message_skip(m, e.shape, e.element, 0):
                rep.malformed = true
                return false
            return true
    cell.i = tag
    if a.s == "tab":
        let payload = new_record(table_of(a.t))
        cell.tab = payload
        if not message_body(m, payload):
            return false
    elif a.s != "void":
        let fv = new_slot(a)
        cell.arm = fv
        if not message_value(m, fv, e):
            return false
    if widened:
        rep.widened++
    return true

// message_cell reads one non-array value into a cell: a nested body, a union,
// an enum or a scalar, at the announced shape s.
def message_cell(m:MessageRead, cell:Value, fd:FieldDescriptor, declared:int, s:MessageShape, element:bool) -> bool:
    let sh = fd.s
    if sh == "tab":
        let rec = new_record(table_of(fd.t))
        cell.tab = rec
        return message_body(m, rec)
    if sh == "union":
        return message_union(m, cell, fd)
    if sh == "enum":
        return message_enum(m, cell, fd, element)
    if sh == "bool" or sh == "int" or sh == "float":
        return message_scalar(m, cell, fd, declared, s)
    // a pointer, a blob, a map or a list: a fixed root numbers no node and
    // carries no container, so its index has no width (§3.1, §3.3)
    m.rep.malformed = true
    return false

// message_array reads a positional array: the count (none rides when min is
// max), the align for an element kind of 6, the elements kept up to the
// bound, and the surplus walked or stepped over by arithmetic (§3.3).
def message_array(m:MessageRead, sl:Slot, e:MessageEntry) -> bool:
    let rep = m.rep
    let f = sl.fd
    let n, ok = message_count(m.bits, e.shape)
    if not ok:
        rep.malformed = true
        return false
    if e.element.kind == 6 and not mb_align(m.bits):
        rep.malformed = true
        return false
    let ed = elem_of(f)
    var keep = n
    if ult(f.bound, n):
        keep = f.bound
        rep.clamped++
    var walk = n
    if e.element.bits >= 0:
        walk = keep
    var i = 0
    while i < walk:
        var cell = Value {}
        if i < keep:
            cell = sl.elems[i]
        if not message_cell(m, cell, ed, f.ek, e.element, true):
            return false
        i++
    if walk < n and not mb_skip(m.bits, (n - walk) * e.element.bits):
        rep.malformed = true
        return false
    if f.array == "counted":
        sl.count = keep
    return true

// message_keyed reads an enum-keyed array (§3.2): the present-slot count, then
// one (key reference, element) pair a slot; a key this reader cannot name
// reads into a sink and counts unknown.
def message_keyed(m:MessageRead, sl:Slot, e:MessageEntry) -> bool:
    let rep = m.rep
    let f = sl.fd
    let n, ok = message_count(m.bits, e.shape)
    if not ok:
        rep.malformed = true
        return false
    let keys = keyed_variants(f)
    let ed = elem_of(f)
    var i = 0
    while i < n:
        let ref, rok = mb_get(m.bits, m.v.ref_bits)
        if not rok:
            rep.malformed = true
            return false
        let id, named = message_name(m.v, ref)
        if not named:
            rep.malformed = true
            return false
        let value = enum_value(keys, id)
        var cell = Value {}
        if value >= 1 and value <= length(sl.elems):
            cell = sl.elems[value - 1]
        else:
            rep.unknown++
        if not message_cell(m, cell, ed, f.ek, e.element, true):
            return false
        i++
    return true

// message_value reads one field's payload under the entry e that announced
// it, by this reader's declaration (Go's emitMessageReadValue).
def message_value(m:MessageRead, sl:Slot, e:MessageEntry) -> bool:
    let f = sl.fd
    let sh = f.s
    if sh == "str" or sh == "text":
        return message_text(m, sl, e.shape)
    if sh == "bytes":
        return message_bytes(m, sl, e.shape)
    if sh == "arr":
        if f.array == "keyed":
            return message_keyed(m, sl, e)
        if f.array == "list":
            m.rep.malformed = true
            return false
        return message_array(m, sl, e)
    return message_cell(m, sl.cell, f, f.k, e.shape, false)

// message_own_entry is this unit's own entry for a slot list's i'th member.
def message_own_entry(slots:[int], i:int) -> MessageEntry:
    let own = own_vocabulary()
    if i < length(slots) and slots[i] > 0:
        let e = message_entry(own, slots[i])
        if e:
            return e
    return MessageEntry { id: 0, shape: MessageShape { kind: -1 }, element: MessageShape { kind: -1 } }

// message_compatible is §4's widening rule read off the announcement (Go's
// emitMessageCompatibility): a scalar kind below this reader's on the same
// ladder, or an array whose element kind is.
def message_compatible(f:FieldDescriptor, mine:MessageEntry, e:MessageEntry) -> bool:
    if mine.shape.kind == 14 and f.s != "map":
        return e.shape.kind == 14 and widens(e.element.kind, mine.element.kind)
    return e.element.kind == mine.element.kind and widens(e.shape.kind, mine.shape.kind)

// message_body decodes one table body: fields until the zero reference that
// ends it (Go's <Name>LoadMessageBody). A reserved id in a body is malformed.
def message_body(m:MessageRead, rec:Record) -> bool:
    let rep = m.rep
    let v = m.v
    let slots = message_slots_of(message_facts.fields, rec.d.name)
    while true:
        let ref, ok = mb_get(m.bits, v.ref_bits)
        if not ok:
            rep.malformed = true
            return false
        if ref == 0:
            return true
        let e = message_entry(v, ref)
        if not e or not ult(e.id, message_vocabulary_id):
            rep.malformed = true
            return false
        var at = -1
        for(rec.fields) sl, i:
            if at < 0 and sl.fd.id == e.id:
                at = i
        if at < 0:
            rep.unknown++
            if not message_skip(m, e.shape, e.element, 0):
                rep.malformed = true
                return false
            continue
        let sl = rec.fields[at]
        let f = sl.fd
        let mine = message_own_entry(slots, at)
        var widened = false
        if e.shape.kind != mine.shape.kind or e.element.kind != mine.element.kind:
            if message_compatible(f, mine, e):
                widened = true
            else:
                rep.kind_mismatch++
                if not message_skip(m, e.shape, e.element, 0):
                    rep.malformed = true
                    return false
                continue
        if not message_value(m, sl, e):
            return false
        if f.present != "":
            sl.present = true
        if widened:
            rep.widened++
    return false

// message_reset_report clears a report for one read (Go's *read = TableReport{}).
def message_reset_report(rep:LoadReport):
    rep.unknown = 0
    rep.kind_mismatch = 0
    rep.widened = 0
    rep.clamped = 0
    rep.duplicate = 0
    rep.malformed = false
    rep.blob_shared = 0
    rep.retained = 0
    rep.retain_lost = 0
    rep.refused = false
    rep.reason = ""

// load_messages_tolerant is THE TOLERANT batch read (§6.5, §3.3; Go's
// <Name>LoadMessagesTolerant): the form byte first, then the vocabulary, then
// the count against the caller's capacity, then the bodies, then the pad.
// It answers the bodies decoded, the count, and ok. DAMAGE INSIDE BODY k
// DELIVERS BODIES 1 TO k - 1, and the count says k - 1; a count above the
// capacity is refused before anything is decoded and answers the wire's M.
def load_messages_tolerant(root:string, v:MessageVocabulary, data:string, rep:LoadReport, capacity:int) -> [Record], int, bool:
    let batch:[Record] = []
    let count, ok = load_messages_into(root, v, data, rep, capacity, batch)
    // the bodies read whole; a body the stream stopped inside is dropped here
    // and kept by load_messages_into's caller
    if not ok and length(batch) > count:
        truncate(batch, count)
    return batch, count, ok

// load_messages_into is the batch read into the caller's batch (Go's
// tablewire.DecodeMessages over caller-held instances): each body is pushed
// before it is read, so a body the stream stops inside stays in the batch
// with what it decoded, as a tolerant read keeps its partial value (§4).
// It answers the batch's count M (or the bodies before a stop) and whether
// the whole batch read.
def load_messages_into(root:string, v:MessageVocabulary, data:string, rep:LoadReport, capacity:int, batch:[Record]) -> int, bool:
    if length(data) == 0:
        rep.malformed = true
        return 0, false
    if data[0] != 2:
        message_refuse(rep, "newer_form")
        return 0, false
    if not v.announced:
        message_refuse(rep, "no_vocabulary")
        return 0, false
    if length(data) < 2:
        rep.malformed = true
        return 0, false
    if not is_message_root(root):
        message_refuse(rep, "not_a_message_root: " + root)
        return 0, false
    let count = data[1] + 1
    if count > capacity:
        message_refuse(rep, "batch_too_large")
        return count, false
    let bits = MessageBits { b: data, lo: 2, n: (length(data) - 2) * 8 }
    let m = MessageRead { bits: bits, v: v, rep: rep }
    let d = table_of(root)
    var i = 0
    while i < count:
        let rec = new_record(d)
        push(batch, rec)
        if not message_body(m, rec):
            return i, false
        i++
    // THE TRAILING PAD IS VERIFIED ZERO, AND BYTES AFTER IT ARE MALFORMED
    if not mb_align(bits) or bits.off != bits.n:
        rep.malformed = true
        return count, false
    return count, true

// load_messages is the plain batch read, and it is STRICT (§6.5; Go's
// <Name>LoadMessages): the report is this read's, and a batch the strict rule
// alone refuses answers no bodies and a count of 0.
def load_messages(root:string, v:MessageVocabulary, data:string, rep:LoadReport, capacity:int) -> [Record], int, bool:
    message_reset_report(rep)
    let out, count, ok = load_messages_tolerant(root, v, data, rep, capacity)
    if not ok:
        return out, count, false
    if strict_reason(rep) != "":
        return [], 0, false
    return out, count, true

// ---- the write ----

// mw_cell_rides is a value's own elision (Go's emitMessageRides over an
// element or a field that is no array): a nested body rides when it holds
// more than its terminator, everything else when it is not its default.
def mw_cell_rides(w:MessageWriter, d:FieldDescriptor, kind:int, v:Value, element:bool) -> bool, bool:
    if d.s == "tab":
        let probe = MessageWriter { measuring: true, ref_bits: w.ref_bits }
        var t = v.tab
        if not t:
            t = new_record(table_of(d.t))
        if not mw_body(probe, t):
            return false, false
        return probe.bits > w.ref_bits, true
    if d.s == "union":
        return v.i != 0, true
    if element:
        return not save_elem_is_default(d, kind, v), true
    return not save_is_default(d, v), true

// mw_rides is a field's elision, the variable form's rule (§3.3, "ELISION,
// DEFAULTS AND THE READ REPORT ARE UNCHANGED"): a keyed array rides when a
// slot does, a counted array when it holds one, a fixed array of bodies
// always, any other fixed array when one element is not its zero.
def mw_rides(w:MessageWriter, sl:Slot) -> bool, bool:
    let f = sl.fd
    if f.present != "":
        return sl.present, true
    let sh = f.s
    if sh == "arr":
        if f.array == "keyed":
            let pairs, ok = mw_keyed_pairs(w, sl)
            return pairs > 0, ok
        if f.array == "counted" or f.array == "list":
            return sl.count > 0, true
        if f.ek == 13:
            return true, true
        let ed = elem_of(f)
        var i = 0
        while i < f.bound:
            if i < length(sl.elems):
                let rides, ok = mw_cell_rides(w, ed, f.ek, sl.elems[i], true)
                if not ok:
                    return false, false
                if rides:
                    return true, true
            i++
        return false, true
    if sh == "bytes" or sh == "text" or sh == "str":
        return sl.cell.s != f.def_str, true
    return mw_cell_rides(w, f, f.k, sl.cell, false)

def mw_keyed_pairs(w:MessageWriter, sl:Slot) -> int, bool:
    let f = sl.fd
    let ed = elem_of(f)
    var pairs = 0
    for(sl.elems) v:
        let rides, ok = mw_cell_rides(w, ed, f.ek, v, true)
        if not ok:
            return 0, false
        if rides:
            pairs++
    return pairs, true

// message_name_slot is the slot of a kind-0 entry, a variant's name, in the
// unit's own vocabulary (Go's tableMessageNameSlot); 0 names none.
def message_name_slot(id:int) -> int:
    let own = own_vocabulary()
    for(own.entries) e, i:
        if e.id == id and e.shape.kind == 0:
            return i + 1
    return 0

// mw_scalar writes one value at the width its declaration states (Go's
// tableMessageWriteScalar): a ranged value as its offset from the base, a
// quantized float as its step index by §4.3's rule in float32.
def mw_scalar(w:MessageWriter, d:FieldDescriptor, s:MessageShape, v:Value) -> bool:
    let kind = s.kind
    if kind == 1:
        mw_put(w, if v.b: 1 else: 0, 1)
        return true
    var lo = 0
    var hi = 0
    if kind == 10:
        lo = f32bits(v.f)
    elif kind == 11:
        lo = f64bits(v.f)
    elif kind_wide(kind):
        lo = v.w.lo
        hi = v.w.hi
    elif kind_width(kind) > 0:
        lo = v.i
    else:
        return false
    if s.packing == 1:
        let borrow = if ult(lo, s.base_lo): 1 else: 0
        lo = lo - s.base_lo
        hi = hi - s.base_hi - borrow
    if s.packing == 2:
        let value = fround(v.f)
        var ratio = fround(fround(value - s.qmin) / s.qdelta)
        if not (ratio >= 0.0):
            ratio = 0.0
            w.clamped++
        elif not (ratio <= 1.0):
            ratio = 1.0
            w.clamped++
        let scaled = fround(ratio * fround(float(s.qcount)))
        lo = floor(fround(scaled + 0.5))
        if lo > s.qcount:
            lo = s.qcount
    if s.bits < 0:
        return false
    mw_put(w, lo, min(s.bits, 64))
    if s.bits > 64:
        mw_put(w, hi, s.bits - 64)
    return true

// mw_cell writes one non-array value under its own announced shape.
def mw_cell(w:MessageWriter, d:FieldDescriptor, s:MessageShape, v:Value) -> bool:
    let sh = d.s
    if sh == "tab":
        var t = v.tab
        if not t:
            t = new_record(table_of(d.t))
        return mw_body(w, t)
    if sh == "union":
        return mw_union(w, d, v)
    if sh == "enum":
        if v.i == 0:
            mw_put(w, 0, w.ref_bits)
            return true
        if v.i < 1 or v.i > length(d.vs):
            return false
        let variant = d.vs[v.i - 1]
        // A STRICT WRITE REFUSES A DEPRECATED VARIANT (§2.15)
        if is_deprecated_variant(d.t, variant.name):
            return false
        let slot = message_name_slot(variant.id)
        if slot == 0:
            return false
        mw_put(w, slot, w.ref_bits)
        return true
    if sh == "bool" or sh == "int" or sh == "float":
        return mw_scalar(w, d, s, v)
    return false

// mw_union writes a union: the ARM's reference, then the payload a field of
// the arm's type carries; None is the zero reference.
def mw_union(w:MessageWriter, d:FieldDescriptor, v:Value) -> bool:
    if v.i == 0:
        mw_put(w, 0, w.ref_bits)
        return true
    let un = lookup_union(d.t)
    assert un
    if v.i < 1 or v.i > length(un.arms):
        return false
    let arm = un.arms[v.i - 1]
    let slots = message_slots_of(message_facts.arms, un.name)
    if v.i - 1 >= length(slots) or slots[v.i - 1] == 0:
        return false
    mw_put(w, slots[v.i - 1], w.ref_bits)
    if arm.s == "void":
        return true
    if arm.s == "tab":
        var t = v.tab
        if not t:
            t = new_record(table_of(arm.t))
        return mw_body(w, t)
    let mine = message_own_entry(slots, v.i - 1)
    var fv = v.arm
    if not fv:
        fv = new_slot(arm)
    return mw_value(w, fv, mine)

// mw_value writes one field's payload under its own entry (Go's
// emitMessageValue): the count and align of an array or a string, then
// what each element or the value carries.
def mw_value(w:MessageWriter, sl:Slot, e:MessageEntry) -> bool:
    let f = sl.fd
    let s = e.shape
    let sh = f.s
    let count_bits = message_bits(s.max - s.min)
    if sh == "str" or sh == "text":
        if sh == "str" and length(sl.cell.s) > f.bound:
            return false
        mw_put(w, length(sl.cell.s), count_bits)
        mw_raw(w, sl.cell.s)
        return true
    if sh == "bytes":
        if length(sl.cell.s) > f.bound:
            return false
        mw_put(w, length(sl.cell.s) - s.min, count_bits)
        mw_raw(w, sl.cell.s)
        return true
    if sh == "arr":
        let ed = elem_of(f)
        if f.array == "list":
            return false
        if f.array == "keyed":
            let keys = keyed_variants(f)
            let pairs, ok = mw_keyed_pairs(w, sl)
            if not ok:
                return false
            mw_put(w, pairs - s.min, count_bits)
            for(sl.elems) v, i:
                let rides, rok = mw_cell_rides(w, ed, f.ek, v, true)
                if not rok:
                    return false
                if rides:
                    if i >= length(keys):
                        return false
                    let slot = message_name_slot(keys[i].id)
                    if slot == 0:
                        return false
                    mw_put(w, slot, w.ref_bits)
                    if not mw_cell(w, ed, e.element, v):
                        return false
            return true
        var count = f.bound
        if f.array == "counted":
            count = sl.count
            if count < 0 or count > f.bound:
                return false
        mw_put(w, count - s.min, count_bits)
        if e.element.kind == 6:
            mw_align(w)
        var i = 0
        while i < count:
            if not mw_cell(w, ed, e.element, sl.elems[i]):
                return false
            i++
        return true
    return mw_cell(w, f, s, sl.cell)

// mw_body writes one table body: each field that rides, its slot as a
// reference and its payload, then the zero reference (Go's
// <Name>SaveMessageBody).
def mw_body(w:MessageWriter, rec:Record) -> bool:
    let slots = message_slots_of(message_facts.fields, rec.d.name)
    for(rec.fields) sl, i:
        let rides, ok = mw_rides(w, sl)
        if not ok:
            return false
        if rides:
            if i >= length(slots) or slots[i] == 0:
                return false
            mw_put(w, slots[i], w.ref_bits)
            if not mw_value(w, sl, message_own_entry(slots, i)):
                return false
    mw_put(w, 0, w.ref_bits)
    return not w.overflow

// message_batch_ok is the write side's batch rule (§3.3): 1 to 256 bodies of
// one message root, or a refusal by name.
def message_batch_ok(values:[Record], rep:LoadReport) -> bool:
    if length(values) < 1 or length(values) > message_batch_max:
        return message_refuse(rep, "batch_too_large")
    let name = values[0].d.name
    if not is_message_root(name):
        return message_refuse(rep, "not_a_message_root: " + name)
    for(values) rec:
        if rec.d.name != name:
            return message_refuse(rep, "not_a_message_root: " + rec.d.name)
    return true

// measure_messages sizes the batch's buffer (Go's <Name>MeasureMessages), -1
// for a batch the form refuses; the refusal is learned before a buffer is.
def measure_messages(values:[Record], rep:LoadReport) -> int:
    if not message_batch_ok(values, rep):
        return -1
    let w = MessageWriter { measuring: true, ref_bits: own_vocabulary().ref_bits }
    for(values) rec:
        if not mw_body(w, rec):
            return -1
    mw_align(w)
    if w.overflow:
        return -1
    return 2 + w.bits / 8

// save_messages writes the batch (Go's <Name>SaveMessages): the form byte, the
// count, the bodies as one bit stream, the zero pad. It answers the bytes and
// their length, or "" and -1 for a refused batch.
def save_messages(values:[Record], rep:LoadReport) -> string, int:
    if not message_batch_ok(values, rep):
        return "", -1
    let w = MessageWriter { ref_bits: own_vocabulary().ref_bits }
    for(values) rec:
        if not mw_body(w, rec):
            return "", -1
    mw_align(w)
    if w.overflow:
        return "", -1
    rep.clamped += w.clamped
    var out = zeros(2)
    out = write_int8_le(out, 0, 2)
    out = write_int8_le(out, 1, length(values) - 1)
    return out + w.b, 2 + w.bits / 8
