// message.goose — the MESSAGE FORM (form byte 2) runtime (docs/SPEC-TABLES.md
// §3.3; docs/PORT-GOOSE.md, THE RUNTIME LAYOUT and THE CARD MAP:
// schema-goose-message-form). It holds what every message-form root shares:
// the bit stream (bit i of the stream is bit i % 8 of byte i / 8, low bit
// first, the packet wire's order), the aligns and their verified-zero pad, the
// float rules (finite only; the compressed float's quantization in float32 and
// by nothing else), the refusal names and the report. The per-table bodies —
// every field a reference then a payload, ending at the zero reference — are
// emitted by messageform.go as straight-line code over these helpers.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_tables;

// The form byte and the batch's wire bound (docs/SPEC-TABLES.md §3.3: the
// count is a ranged integer over [1, 256], eight bits carrying M - 1).
let MsgForm: u8 = 2;
let MsgBatchMax: i64 = 256;

// THE REFUSALS OF THE MESSAGE PATH, by the Go engine's names
// (internal/tablewire/message.go, MessageReason; docs/SPEC-TABLES.md §3.3).
let MsgOk: i64 = 0;
let MsgRefuseNewerForm: i64 = 1;
let MsgRefuseNoVocabulary: i64 = 2;
let MsgRefuseBatchTooLarge: i64 = 6;
let MsgRefuseNonFiniteFloat: i64 = 7;
let MsgRefuseInvalidValue: i64 = 8;

fn msg_reason_name(code: i64) -> const u8[:] {
    match code {
        0 => "ok",
        1 => "newer_form",
        2 => "no_vocabulary",
        6 => "batch_too_large",
        7 => "non_finite_float",
        8 => "invalid_value",
        _ => "???",
    }
}

// MsgReport is one batch call's report: the refusal (reason non-zero), the
// damage flag, the clamps, and on a capacity refusal the count the wire
// carries (docs/SPEC-TABLES.md §3.3: "the reader sets the returned count to
// the wire's M before it returns the refusal").
struct MsgReport { reason: i64, malformed: bool, clamped: i64, count: i64 }

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

// ---- the reader -------------------------------------------------------------

// MsgReader is the bit reader over a batch: the bytes, the bit position from
// byte 0, the end, and ok false at the first damage (or a refusal, reason
// beside it). clamped counts the read's clamps.
struct MsgReader { b: const u8[:], pos: i64, end: i64, ok: bool = true, reason: i64, clamped: i64 }

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

// msg_skip steps over n bits.
fn msg_skip(r: MsgReader&, n: i64) -> bool {
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
