// fixed.goose — the fixed form (form byte 3) runtime
// (docs/FIXED-FORM-ALGORITHM.md; docs/SPEC-TABLES.md §3.4). This file follows
// the design of docs/PORT-GOOSE.md (THE RUNTIME LAYOUT, BYTE BUFFERS, FLOAT32)
// and sits beside schema_tables.goose in the table surface's runtime
// namespace. It holds what every fixed root shares: the framing of a file
// (form byte 3, seven reserved zeros, the layout hash, the layout's byte
// length, the layout, then records back to back), the refusal names, the
// report, the little-endian word moves, the float bit routes and the text
// content rules. The per-table code — the record writer, the write contract,
// the reader's scan, landing and bounds pass — is emitted by fixedform.go as
// straight-line code over these helpers (docs/FIXED-FORM-ALGORITHM.md §4.2,
// the record-image shape: the plan is laid down at build time as code).
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_tables;

// The fixed form's one form byte and the constant header (docs/SPEC-TABLES.md
// §3.4): form, seven reserved zeros, the layout hash; the layout's u32 byte
// length follows at 16.
let FixedForm: u8 = 3;
let FixedHeaderBytes: i64 = 16;

// THE REFUSALS, BY GO'S NAME AND GO'S NUMBER (gotable's TableRefuseReason;
// docs/FIXED-FORM-ALGORITHM.md §2, §4.5, §5.3): a refusal is total — no value
// landed, no counter moved — and report.reason names it.
let FixedRefuseOk: i64 = 0;
let FixedRefuseReservedNotZero: i64 = 4;
let FixedRefuseTruncated: i64 = 6;
let FixedRefuseInvalidValue: i64 = 14;
let FixedRefuseClamped: i64 = 18;
let FixedRefusePreviousForm: i64 = 25;
let FixedRefuseNewerForm: i64 = 26;
let FixedRefuseMessageFormAsFile: i64 = 27;
let FixedRefuseLayoutUnknown: i64 = 28;
let FixedRefuseLayoutMalformed: i64 = 31;
let FixedRefuseNoLayout: i64 = 40;
let FixedRefuseBatchTooLarge: i64 = 41;
let FixedRefuseTextIllFormed: i64 = 42;
let FixedRefuseCountOverBound: i64 = 43;
let FixedRefuseLengthOverBound: i64 = 44;

// fixed_refusal_name is a refusal's name, the string Go's Error() gives.
fn fixed_refusal_name(code: i64) -> const u8[:] {
    match code {
        0 => "ok",
        4 => "reserved_not_zero",
        6 => "truncated",
        14 => "invalid_value",
        18 => "clamped",
        25 => "previous_form",
        26 => "newer_form",
        27 => "message_form_as_file",
        28 => "layout_unknown",
        31 => "layout_malformed",
        40 => "no_layout",
        41 => "batch_too_large",
        42 => "text_ill_formed",
        43 => "count_over_bound",
        44 => "length_over_bound",
        _ => "???",
    }
}

// FixedReport is one load's report (docs/FIXED-FORM-ALGORITHM.md §5.4): the
// clamped counter the bounds pass moves, whether the framing was damaged, the
// refusal's code (0 for none), and the file's hash when the hash refused.
struct FixedReport { clamped: i64, malformed: bool, reason: i64, hash: u64 }

// fixed_u32 / fixed_u64 / fixed_get read one little-endian word at an offset
// (docs/PORT-GOOSE.md, BYTE BUFFERS: assembled with shifts, never a host-order
// view).
fn fixed_u32(b: const u8[:], at: i64) -> u32 {
    return b[at] | (b[at + 1] as u32) << 8 | (b[at + 2] as u32) << 16 | (b[at + 3] as u32) << 24;
}

fn fixed_u64(b: const u8[:], at: i64) -> u64 {
    return b[at] | (b[at + 1] as u64) << 8 | (b[at + 2] as u64) << 16 | (b[at + 3] as u64) << 24
        | (b[at + 4] as u64) << 32 | (b[at + 5] as u64) << 40 | (b[at + 6] as u64) << 48 | (b[at + 7] as u64) << 56;
}

fn fixed_get(b: const u8[:], at: i64, w: i64) -> u64 {
    var r: u64 = 0;
    var i: i64 = 0;
    while i < w {
        r |= (b[at + i] as u64) << ((i as u64) * 8);
        i += 1;
    }
    return r;
}

// fixed_i32 is a count or a length: the int32 its storage is
// (docs/FIXED-FORM-ALGORITHM.md §3, `[Min..Max]T`, `string(N)`).
fn fixed_i32(b: const u8[:], at: i64) -> i32 {
    return fixed_u32(b, at) as! i32;
}

// fixed_put stores the low w bytes of v, little-endian, at the end of out
// (docs/FIXED-FORM-ALGORITHM.md §0, PUT). A signed value arrives as its
// two's-complement bits (`as! u64`), so its low w bytes are its image.
fn fixed_put(out: u8[>..]&, v: u64, w: i64) {
    var i: i64 = 0;
    while i < w {
        out.push((v >> ((i as u64) * 8)) as! u8);
        i += 1;
    }
}

// fixed_put_zeros is ZERO(p, n): declared slack, always zero on write
// (docs/FIXED-FORM-ALGORITHM.md §3.1).
fn fixed_put_zeros(out: u8[>..]&, n: i64) {
    var i: i64 = 0;
    while i < n {
        out.push(0);
        i += 1;
    }
}

// fixed_put_bool writes `0` or `1` (docs/FIXED-FORM-ALGORITHM.md §3, bool).
fn fixed_put_bool(out: u8[>..]&, v: bool) {
    let b: u8 = if v { 1 } else { 0 };
    out.push(b);
}

// THE FLOAT BITS (docs/PORT-GOOSE.md, FLOAT32; docs/FIXED-FORM-ALGORITHM.md
// §3: the IEEE-754 bit pattern, no canonicalisation). Float to bits is a
// `bytes_of` view of a one-element array; bits to float is a verified
// one-element image through `from_bytes`, which moves the bytes and never
// converts them, so a signalling NaN and its payload survive. Both are pure
// Goose: the C sidecar is schema-goose-serialize-vendor's and is not vendored
// yet.
fn fixed_f32_bits(x: f32) -> u32 {
    let a: f32[1] = [x];
    let b = bytes_of(a);
    return b[0] | (b[1] as u32) << 8 | (b[2] as u32) << 16 | (b[3] as u32) << 24;
}

fn fixed_bits_f32(u: u32) -> f32 {
    var img: u8[>..] = [];
    img.push(4);
    img.push(u as! u8);
    img.push((u >> 8) as! u8);
    img.push((u >> 16) as! u8);
    img.push((u >> 24) as! u8);
    let arr, ok = from_bytes<f32[>..]>(img);
    if !ok || arr.len != 1 { return 0.0; }
    return arr[0];
}

fn fixed_f64_bits(x: f64) -> u64 {
    let a: f64[1] = [x];
    let b = bytes_of(a);
    return fixed_u64(b, 0);
}

fn fixed_bits_f64(u: u64) -> f64 {
    var img: u8[>..] = [];
    img.push(8);
    fixed_put(img, u, 8);
    let arr, ok = from_bytes<f64[>..]>(img);
    if !ok || arr.len != 1 { return 0.0; }
    return arr[0];
}

// fixed_put_text writes a string(N) or bytes(N): the length (i32 LE), then
// the used bytes, then zero to N (docs/FIXED-FORM-ALGORITHM.md §3, §3.1: the
// live extent onto zero, never the storage past the length). The write
// contract has already held n to 0..N.
fn fixed_put_text(out: u8[>..]&, s: const u8[:], n: i32, cap: i64) {
    let used = n as i64;
    fixed_put(out, n as! u32, 4);
    var i: i64 = 0;
    while i < used {
        out.push(s[i]);
        i += 1;
    }
    fixed_put_zeros(out, cap - used);
}

// fixed_put_wtext writes a wstring(N): the length in CODE UNITS, then the used
// units, two bytes each, then zero to N units (docs/FIXED-FORM-ALGORITHM.md §3).
fn fixed_put_wtext(out: u8[>..]&, s: const u16[:], n: i32, cap: i64) {
    let used = n as i64;
    fixed_put(out, n as! u32, 4);
    var i: i64 = 0;
    while i < used {
        fixed_put(out, s[i], 2);
        i += 1;
    }
    fixed_put_zeros(out, (cap - used) * 2);
}

// fixed_get_text lands a text or bytes payload: the identity plan's text op
// copies the whole declared span (docs/FIXED-FORM-ALGORITHM.md §4.1, `text`:
// min(mine, theirs), which on the identity plan is N).
fn fixed_get_text(b: const u8[:], at: i64, dst: u8[:]) {
    var i: i64 = 0;
    while i < dst.len {
        dst[i] = b[at + i];
        i += 1;
    }
}

fn fixed_get_wtext(b: const u8[:], at: i64, dst: u16[:]) {
    var i: i64 = 0;
    while i < dst.len {
        dst[i] = b[at + 2 * i] | (b[at + 2 * i + 1] as u16) << 8;
        i += 1;
    }
}

// fixed_utf8_ok is THE CONTENT RULE of a string(N) over its USED bytes only
// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5 fix 7): well-formed UTF-8 — no
// overlong form, no surrogate, nothing past U+10FFFF, as Go's utf8.Valid
// answers — with no zero byte.
fn fixed_utf8_ok(s: const u8[:], at: i64, n: i64) -> bool {
    var i: i64 = 0;
    while i < n {
        let c = s[at + i];
        if c == 0 { return false; }
        if c < 0x80 {
            i += 1;
        } else {
            var need: i64 = 0;
            var lo: u8 = 0x80;
            var hi: u8 = 0xBF;
            if c >= 0xC2 && c <= 0xDF {
                need = 1;
            } else if c == 0xE0 {
                need = 2;
                lo = 0xA0;
            } else if c >= 0xE1 && c <= 0xEC {
                need = 2;
            } else if c == 0xED {
                need = 2;
                hi = 0x9F;
            } else if c >= 0xEE && c <= 0xEF {
                need = 2;
            } else if c == 0xF0 {
                need = 3;
                lo = 0x90;
            } else if c >= 0xF1 && c <= 0xF3 {
                need = 3;
            } else if c == 0xF4 {
                need = 3;
                hi = 0x8F;
            } else {
                return false;
            }
            if i + need >= n { return false; }
            let c1 = s[at + i + 1];
            if c1 < lo || c1 > hi { return false; }
            var k: i64 = 2;
            while k <= need {
                let ck = s[at + i + k];
                if ck < 0x80 || ck > 0xBF { return false; }
                k += 1;
            }
            i += need + 1;
        }
    }
    return true;
}

// fixed_utf16_unit_ok walks n code units through unit(i) — paired
// surrogates, no zero unit (docs/FIXED-FORM-ALGORITHM.md §3.1: an astral pair
// counts two). Two entry points: storage units and wire bytes.
fn fixed_utf16_ok(s: const u16[:], n: i64) -> bool {
    var i: i64 = 0;
    while i < n {
        let u = s[i];
        if u == 0 { return false; }
        if u >= 0xD800 && u <= 0xDBFF {
            if i + 1 >= n { return false; }
            let lo = s[i + 1];
            if lo < 0xDC00 || lo > 0xDFFF { return false; }
            i += 2;
        } else if u >= 0xDC00 && u <= 0xDFFF {
            return false;
        } else {
            i += 1;
        }
    }
    return true;
}

fn fixed_wire_utf16_ok(b: const u8[:], at: i64, n: i64) -> bool {
    var i: i64 = 0;
    while i < n {
        let u = b[at + 2 * i] | (b[at + 2 * i + 1] as u16) << 8;
        if u == 0 { return false; }
        if u >= 0xD800 && u <= 0xDBFF {
            if i + 1 >= n { return false; }
            let lo = b[at + 2 * i + 2] | (b[at + 2 * i + 3] as u16) << 8;
            if lo < 0xDC00 || lo > 0xDFFF { return false; }
            i += 2;
        } else if u >= 0xDC00 && u <= 0xDFFF {
            return false;
        } else {
            i += 1;
        }
    }
    return true;
}

// THE WRITE CONTRACT (docs/FIXED-FORM-ALGORITHM.md §3.1): Goose has no
// debug-only idiom the generated code can rely on, so the writer refuses as Go
// does, in every build — a length outside 0..N, a string whose used bytes are
// not UTF-8 or hold a NUL, a wstring whose used units are unpaired or zero.
fn fixed_len_ok(n: i32, cap: i64) -> bool {
    return n >= 0 && n <= cap;
}

fn fixed_text_ok(s: const u8[:], n: i32, cap: i64) -> bool {
    if !fixed_len_ok(n, cap) { return false; }
    return fixed_utf8_ok(s, 0, n);
}

fn fixed_wtext_ok(s: const u16[:], n: i32, cap: i64) -> bool {
    if !fixed_len_ok(n, cap) { return false; }
    return fixed_utf16_ok(s, n);
}

// THE READER'S SCAN (docs/FIXED-FORM-ALGORITHM.md §4.5): a count or a length
// past its bound, or text whose used units break the content rule, REFUSES BY
// NAME before any destination byte is written. flavour is §4.1's: 1 utf8,
// 2 wide, 3 bytes.
fn fixed_scan_count(b: const u8[:], at: i64, bound: i64) -> i64 {
    let v = fixed_i32(b, at);
    if v < 0 || v > bound { return FixedRefuseCountOverBound; }
    return 0;
}

fn fixed_scan_text(b: const u8[:], at: i64, cap: i64, flavour: i64) -> i64 {
    let v = fixed_i32(b, at);
    if v < 0 || v > cap { return FixedRefuseLengthOverBound; }
    if flavour == 1 && !fixed_utf8_ok(b, at + 4, v) { return FixedRefuseTextIllFormed; }
    if flavour == 2 && !fixed_wire_utf16_ok(b, at + 4, v) { return FixedRefuseTextIllFormed; }
    return 0;
}

// fixed_refuse records a refusal by name; the framing-damage ones also set
// malformed (docs/FIXED-FORM-ALGORITHM.md §2, §5.3).
fn fixed_refuse(report: FixedReport&, code: i64, malformed: bool) -> i64 {
    report.reason = code;
    report.malformed = malformed;
    return -1;
}

// fixed_load_frame holds ONE file to the fixed framing and the layout this
// build baked, in §2's order (docs/FIXED-FORM-ALGORITHM.md §2, §5.3): the form
// byte before the length, the twenty-byte minimum and the reserved zeros, the
// layout's length, the header's hash TAKEN AS GIVEN and selected (this build's
// own hash is the identity plan; the lineage is
// schema-goose-fixed-form-evolution's, so any other hash is layout_unknown),
// the layout bytes compared and never walked, the ragged tail, the caller's
// capacity, then every record's hash. It returns the record count, or -1
// with report.reason set; nothing is landed here.
fn fixed_load_frame(b: const u8[:], layout: const u8[:], hash: u64, record_bytes: i64, capacity: i64, report: FixedReport&) -> i64 {
    if b.len < 1 { return fixed_refuse(report, FixedRefuseTruncated, true); }
    if b[0] != FixedForm {
        if b[0] == 1 { return fixed_refuse(report, FixedRefusePreviousForm, false); }
        if b[0] == 2 { return fixed_refuse(report, FixedRefuseMessageFormAsFile, false); }
        return fixed_refuse(report, FixedRefuseNewerForm, false);
    }
    if b.len < FixedHeaderBytes + 4 { return fixed_refuse(report, FixedRefuseTruncated, true); }
    var i: i64 = 1;
    while i < 8 {
        if b[i] != 0 { return fixed_refuse(report, FixedRefuseReservedNotZero, true); }
        i += 1;
    }
    let layout_length = fixed_u32(b, FixedHeaderBytes) as i64;
    if layout_length + FixedHeaderBytes + 4 > b.len { return fixed_refuse(report, FixedRefuseLayoutMalformed, false); }
    let h = fixed_u64(b, 8);
    if h != hash {
        report.hash = h;
        return fixed_refuse(report, FixedRefuseLayoutUnknown, false);
    }
    if layout_length != layout.len { return fixed_refuse(report, FixedRefuseLayoutMalformed, false); }
    i = 0;
    while i < layout.len {
        if b[FixedHeaderBytes + 4 + i] != layout[i] { return fixed_refuse(report, FixedRefuseLayoutMalformed, false); }
        i += 1;
    }
    let rest = b.len - FixedHeaderBytes - 4 - layout_length;
    if record_bytes <= 8 || rest % record_bytes != 0 { return fixed_refuse(report, FixedRefuseTruncated, true); }
    let n = rest / record_bytes;
    if n > capacity { return fixed_refuse(report, FixedRefuseBatchTooLarge, false); }
    var at: i64 = FixedHeaderBytes + 4 + layout_length;
    while at < b.len {
        if fixed_u64(b, at) != hash { return fixed_refuse(report, FixedRefuseNoLayout, false); }
        at += record_bytes;
    }
    return n;
}

// fixed_write_header lays the framing this build baked onto out
// (docs/SPEC-TABLES.md §3.4, THE FRAMING): form byte, seven reserved zeros,
// the layout hash, the layout length, the layout. It returns the header's
// byte length.
fn fixed_write_header(out: u8[>..]&, layout: const u8[:], hash: u64) -> i64 {
    out.push(FixedForm);
    fixed_put_zeros(out, 7);
    fixed_put(out, hash, 8);
    fixed_put(out, layout.len as u64, 4);
    var i: i64 = 0;
    while i < layout.len {
        out.push(layout[i]);
        i += 1;
    }
    return FixedHeaderBytes + 4 + layout.len;
}
