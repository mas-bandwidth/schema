// json_value.goose — THE JSON TEXT FORM over THE FLAT VALUE TABLE
// (docs/SPEC-TABLES.md §16.1, §16.2, §16.3, §16.7; docs/PORT-GOOSE.md, THE
// LOADED VALUE). A variable table lives in Goose as the flat value table the
// table wire loads into and saves from (wire_load.goose, wire_save.goose):
// every value a LoadVal in the caller's pool, linked by index, and every node
// record of §3.1 a LoadNode in the caller's node table. This file reads one
// text into that table and writes one text out of it, so a variable root's
// text form is the same storage its wire form is, and the save renumbers
// whatever nodes the read placed. It reads nothing but the descriptors, the
// unit the caller hands it and the unit's text facts (JsonUnit), and names no
// generated type; it is the decision-for-decision port of the compiler's own
// engine (internal/tabletext read.go, write.go, wide.go), as wire_load.goose
// is of internal/tablewire.
//
// The scanners, the number rules and the canonical spellings are the fixed
// walk's (schema_json.goose): a text this file writes is byte for byte the
// text that walk writes for the same values, and every counter a read raises
// is raised by the same rule.
//
//   json_load_table(unit, text_unit, root, text, data, vals, nodes, report) -> i64
//       the tolerant read: the root's slot in vals, or -1 when the text is
//       not JSON, a value is refused by name, or the root carries no text
//       form; the report is §4's. The decoded text and blob bytes are
//       appended to data, where the slots' views point.
//   json_save_table(unit, text_unit, root, data, vals, nodes, at, out) -> i64
//       appends the canonical text of the root at slot `at` and answers its
//       length, or -1 (nothing appended) for a value the form cannot spell.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;
import .schema_tables;
import .schema_json;
import .wire_load;
import .wire_save;
import .fixed;

// JsonFact is what a text needs of a value that the table wire does not
// (docs/SPEC-TABLES.md §16.2): a flags mask is its variant names, and a
// timestamp is an RFC 3339 string, and a text list's element record is its
// string. It names the value by its owner (a table
// or a union) and its ordinal (a field's, or a union arm's tag), and holds
// for the field's elements as for the field.
struct JsonFact {
    owner: const u8[:],
    ordinal: i32,
    is_flags: bool,
    flags: const (const u8[:])[:],
    timestamp: bool,
    text_list: bool,
}

// JsonUnit is a unit's text facts, and the tables declared without the json
// form (§2.12), which have no text.
struct JsonUnit {
    facts: const JsonFact[:],
    no_text: const (const u8[:])[:],
}

// The refusals this form names beside schema_json.goose's own (§16.2, §2.3).
let JsonReasonMissingRequired: i64 = 4;
let JsonReasonTimestamp: i64 = 5;

// JsonLabel is one `&node` label a read met (§16.7): the wire index of the
// node it defined (0 for a definition the read dropped, which names no node),
// that node's table, and whether its object is still open.
struct JsonLabel { label: u64, wire: i64, tab: i64, open: bool }

// ---- the unit's facts ----

fn jv_table(unit: LoadUnit, name: const u8[:]) -> i64 {
    for t, i in unit.tables { if t.name == name { return i; } }
    return -1;
}

fn jv_union(unit: LoadUnit, name: const u8[:]) -> i64 {
    for u, i in unit.unions { if u.name == name { return i; } }
    return -1;
}

// jv_entry is a map's entry table, <Owner><Slot>Entry (docs/PORT-GOOSE.md,
// THE TYPE MAPPING).
fn jv_entry(unit: LoadUnit, owner: const u8[:], slot: const u8[:]) -> i64 {
    let tail = "Entry";
    for t, i in unit.tables {
        let n = t.name;
        if n.len != owner.len + slot.len + tail.len { continue; }
        if n[..owner.len] == owner && n[owner.len..owner.len + slot.len] == slot && n[owner.len + slot.len..] == tail {
            return i;
        }
    }
    return -1;
}

// jv_keys is the key enum of an enum-keyed array (§3.2).
fn jv_keys(unit: LoadUnit, owner: const u8[:], d: FieldDesc) -> const VariantDesc[:] {
    for k in unit.keys {
        if k.ordinal == d.ordinal && k.owner == owner { return k.variants; }
    }
    return d.variants;
}

fn jv_deprecated(unit: LoadUnit, name: const u8[:], id: u64) -> bool {
    for dv in unit.deprecated {
        if dv.id == id && dv.enum_name == name { return true; }
    }
    return false;
}

fn jv_fact(text: JsonUnit, owner: const u8[:], ordinal: i32) -> i64 {
    for f, i in text.facts {
        if f.ordinal == ordinal && f.owner == owner { return i; }
    }
    return -1;
}

fn jv_flags(text: JsonUnit, owner: const u8[:], ordinal: i32) -> bool {
    let i = jv_fact(text, owner, ordinal);
    return i >= 0 && text.facts[i].is_flags;
}

fn jv_stamp(text: JsonUnit, d: FieldDesc, owner: const u8[:], ordinal: i32) -> bool {
    if d.timestamp { return true; }
    let i = jv_fact(text, owner, ordinal);
    return i >= 0 && text.facts[i].timestamp;
}

fn jv_has_text(text: JsonUnit, name: const u8[:]) -> bool {
    for n in text.no_text { if n == name { return false; } }
    return true;
}

// jv_wide: a 128-bit integer or a fixed-point kind, whose text converts
// exactly and never through a double (§16.2).
fn jv_wide(d: FieldDesc) -> bool { d.kw == 16 || d.ibits != 0 || d.fbits != 0 }

// jv_text_list: a text list (§2.13), whose element is a one-field record
// the text spells as its string.
fn jv_text_list(text: JsonUnit, owner: const u8[:], ordinal: i32) -> bool {
    let i = jv_fact(text, owner, ordinal);
    return i >= 0 && text.facts[i].text_list;
}

// jv_raw_range is a field's declared range on the RAW scale
// (ir.TableRawRange): a fixed kind's bounds are whole units, scaled by 2^F.
fn jv_raw_range(d: FieldDesc) -> Int128, Int128 {
    if d.ibits != 0 || d.fbits != 0 { return shl128(d.lo, d.fbits), shl128(d.hi, d.fbits); }
    return d.lo, d.hi;
}

fn jv_slot(vals: const LoadVal[:], rec: i64, i: i64) -> i64 {
    if rec < 0 || !vals[rec].made { return -1; }
    return vals[rec].first + i;
}

fn jv_state(vals: const LoadVal[:], s: i64) -> u8 { if s < 0 { ValAbsent } else { vals[s].state } }

fn jv_bool(vals: const LoadVal[:], s: i64, d: FieldDesc) -> bool {
    if jv_state(vals, s) == ValAbsent { return d.has_def && d.def_b; }
    return vals[s].bits.lo != 0;
}

fn jv_int(vals: const LoadVal[:], s: i64, d: FieldDesc) -> Int128 {
    if jv_state(vals, s) == ValAbsent {
        if d.has_def { return d.def_i; }
        return Int128 { lo: 0, hi: 0 };
    }
    return vals[s].bits;
}

// jv_guard: SPEC §4.5's guards over the record's current values; a field
// under a false branch is neither written nor required.
fn jv_guard(unit: LoadUnit, vals: const LoadVal[:], tab: i64, rec: i64, i: i64) -> bool {
    if unit.guards.len == 0 { return true; }
    let name = unit.tables[tab].name;
    for g in unit.guards {
        if g.ordinal != i || g.owner != name { continue; }
        let cond = unit.tables[tab].fields[g.cond];
        if jv_bool(vals, jv_slot(vals, rec, g.cond), cond) != g.want { return false; }
    }
    return true;
}

// ---- THE WIDE KINDS (§16.2; internal/tabletext wide.go): exact decimal
// conversion over 32-bit limbs, never through a double ----

let JvLimbs: i64 = 32;

struct JvBig { w: u32[32], over: bool }

fn jv_big() -> JvBig { JvBig { w: [0; 32], over: false } }

fn jv_big_zero(a: const JvBig&) -> bool {
    for x in a.w { if x != 0 { return false; } }
    return true;
}

fn jv_big_mul(a: JvBig&, m: u64, add: u64) {
    var carry: u64 = add;
    var i: i64 = 0;
    while i < JvLimbs {
        let x: u64 = a.w[i];
        let p = x * m + carry;
        a.w[i] = (p & 0xFFFFFFFF) as! u32;
        carry = p >> 32;
        i += 1;
    }
    if carry != 0 { a.over = true; }
}

fn jv_big_shl(a: JvBig&, n: i64) {
    var k: i64 = 0;
    while k < n {
        jv_big_mul(a, 2, 0);
        k += 1;
    }
}

// jv_big_div divides by a small d and answers the remainder.
fn jv_big_div(a: JvBig&, d: u64) -> u64 {
    var rem: u64 = 0;
    var i = JvLimbs - 1;
    while i >= 0 {
        let x: u64 = a.w[i];
        let cur = (rem << 32) | x;
        a.w[i] = (cur / d) as! u32;
        rem = cur % d;
        i -= 1;
    }
    return rem;
}

fn jv_big_of(v: Int128) -> JvBig {
    var a = jv_big();
    a.w[0] = (v.lo & 0xFFFFFFFF) as! u32;
    a.w[1] = (v.lo >> 32) as! u32;
    a.w[2] = (v.hi & 0xFFFFFFFF) as! u32;
    a.w[3] = (v.hi >> 32) as! u32;
    return a;
}

// jv_big_fits128: the magnitude is below 2^128 (lanes), or below 2^127 + 1
// (half) for a negative value of a signed kind.
fn jv_big_lanes(a: const JvBig&) -> Int128, bool {
    if a.over { return Int128 { lo: 0, hi: 0 }, false; }
    var i: i64 = 4;
    while i < JvLimbs {
        if a.w[i] != 0 { return Int128 { lo: 0, hi: 0 }, false; }
        i += 1;
    }
    let w0: u64 = a.w[0];
    let w1: u64 = a.w[1];
    let w2: u64 = a.w[2];
    let w3: u64 = a.w[3];
    let lo = w0 | (w1 << 32);
    let hi = w2 | (w3 << 32);
    return Int128 { lo: lo, hi: hi }, true;
}

fn jv_neg128(v: Int128) -> Int128 {
    let lo = (~v.lo) + 1;
    let hi = (~v.hi) + (if lo == 0 { 1 } else { 0 });
    return Int128 { lo: lo, hi: hi };
}

// jv_digit is digit j of a number token's digit run: the integer part's
// digits, then the fraction's.
fn jv_digit(tok: const u8[:], i0: i64, n_int: i64, f0: i64, j: i64) -> u64 {
    let at = if j < n_int { i0 + j } else { f0 + j - n_int };
    let digit: u64 = tok[at] - '0';
    return digit;
}

// jv_parse_wide is ParseWide: a token's value times 2^frac, exact or nothing;
// saturated when the magnitude was held to the kind's 128-bit domain.
fn jv_parse_wide(tok: const u8[:], signed: bool, frac: i64) -> Int128, bool, bool {
    var i: i64 = 0;
    var neg = false;
    if i < tok.len && (tok[i] == '-' || tok[i] == '+') {
        neg = tok[i] == '-';
        i += 1;
    }
    let i0 = i;
    while i < tok.len && tok[i] >= '0' && tok[i] <= '9' { i += 1; }
    let n_int = i - i0;
    var f0 = i;
    var n_frac: i64 = 0;
    if i < tok.len && tok[i] == '.' {
        i += 1;
        f0 = i;
        while i < tok.len && tok[i] >= '0' && tok[i] <= '9' { i += 1; }
        n_frac = i - f0;
    }
    var exp: i64 = 0;
    if i < tok.len && (tok[i] == 'e' || tok[i] == 'E') {
        i += 1;
        var eneg = false;
        if i < tok.len && (tok[i] == '-' || tok[i] == '+') {
            eneg = tok[i] == '-';
            i += 1;
        }
        while i < tok.len && tok[i] >= '0' && tok[i] <= '9' {
            let digit: i64 = tok[i] - '0';
            if exp < 100000 { exp = exp * 10 + digit; }
            i += 1;
        }
        if eneg { exp = -exp; }
    }
    let total = n_int + n_frac;
    var first: i64 = 0;
    while first < total && jv_digit(tok, i0, n_int, f0, first) == 0 { first += 1; }
    var last = total;
    while last > first && jv_digit(tok, i0, n_int, f0, last - 1) == 0 { last -= 1; }
    let zero = Int128 { lo: 0, hi: 0 };
    let high = if signed { Int128 { lo: 0xFFFFFFFFFFFFFFFF, hi: 0x7FFFFFFFFFFFFFFF } } else { Int128 { lo: 0xFFFFFFFFFFFFFFFF, hi: 0xFFFFFFFFFFFFFFFF } };
    let low = if signed { Int128 { lo: 0, hi: 0x8000000000000000 } } else { zero };
    if last == first { return zero, true, false; }
    let point = n_int + exp - first;
    let digits = last - first;
    if point > 40 {
        if neg { return low, true, true; }
        return high, true, true;
    }
    if point < -40 { return zero, false, false; }
    let scale = point - digits;
    if scale < 0 && -scale > frac { return zero, false, false; }
    var a = jv_big();
    var j = first;
    while j < last {
        jv_big_mul(a, 10, jv_digit(tok, i0, n_int, f0, j));
        j += 1;
    }
    jv_big_shl(a, frac);
    if scale >= 0 {
        var k: i64 = 0;
        while k < scale {
            jv_big_mul(a, 10, 0);
            k += 1;
        }
    } else {
        var k: i64 = 0;
        while k < -scale {
            if jv_big_div(a, 10) != 0 { return zero, false, false; }
            k += 1;
        }
    }
    let mag, fits = jv_big_lanes(a);
    if neg {
        // the magnitude may reach 2^127 for a signed kind, 0 for an unsigned one
        if !signed { return zero, true, true; }
        if !fits || mag.hi > 0x8000000000000000 || (mag.hi == 0x8000000000000000 && mag.lo != 0) { return low, true, true; }
        return jv_neg128(mag), true, false;
    }
    if !fits { return high, true, true; }
    if signed && (mag.hi >> 63) != 0 { return high, true, true; }
    return mag, true, false;
}

// jv_put_wide is FormatWide: a 128-bit integer as its decimal digits, a fixed
// value as the shortest exact decimal in whole units with at least one
// fractional digit.
fn jv_put_wide(out: u8[>..]&, v: Int128, signed: bool, fixed: bool, frac: i64) {
    var mag = v;
    if signed && (v.hi >> 63) != 0 {
        out.push('-');
        mag = jv_neg128(v);
    }
    var a = jv_big_of(mag);
    // the fraction's bits, masked off before the whole part is printed
    var f = jv_big();
    var b: i64 = 0;
    while b < frac {
        let limb = b / 32;
        let bit = (b % 32) as! u32;
        f.w[limb] |= a.w[limb] & ((1 as u32) << bit);
        b += 1;
    }
    var k: i64 = 0;
    while k < frac {
        jv_big_div(a, 2);
        k += 1;
    }
    var digits: u8[48] = [0; 48];
    var n: i64 = 48;
    loop {
        n -= 1;
        digits[n] = (jv_big_div(a, 10) as! u8) + '0';
        if jv_big_zero(a) { break; }
    }
    out.append(digits[n..]);
    if !fixed { return; }
    out.push('.');
    if jv_big_zero(f) {
        out.push('0');
        return;
    }
    while !jv_big_zero(f) {
        jv_big_mul(f, 10, 0);
        // the digit is what rose above the fraction's bits
        var d: u64 = 0;
        var t: i64 = 0;
        while t < 4 {
            let at = frac + t;
            let limb = at / 32;
            let bit = (at % 32) as! u32;
            if (f.w[limb] & ((1 as u32) << bit)) != 0 { d |= (1 as u64) << (t as! u64); }
            f.w[limb] &= ~((1 as u32) << bit);
            t += 1;
        }
        out.push((d as! u8) + '0');
    }
}

// ---- timestamps (§2.11, §16.2; internal/rfc3339) ----

fn jv_days_from_civil(y0: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y0 - 1 } else { y0 };
    let era = (if y >= 0 { y } else { y - 399 }) / 400;
    let yoe = y - era * 400;
    let mp = if m > 2 { m - 3 } else { m + 9 };
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    return era * 146097 + doe - 719468;
}

fn jv_leap(y: i64) -> bool { (y % 4 == 0) && (y % 100 != 0 || y % 400 == 0) }

fn jv_month_days(y: i64, m: i64) -> i64 {
    if m == 2 { return if jv_leap(y) { 29 } else { 28 }; }
    if m == 4 || m == 6 || m == 9 || m == 11 { return 30; }
    return 31;
}

fn jv_two(s: const u8[:], i: i64) -> i64, bool {
    if i + 1 >= s.len { return 0, false; }
    let a = s[i];
    let b = s[i + 1];
    if a < '0' || a > '9' || b < '0' || b > '9' { return 0, false; }
    let x: i64 = a - '0';
    let y: i64 = b - '0';
    return x * 10 + y, true;
}

// jv_rfc3339 reads exactly RFC 3339 §5.6's date-time and answers its UTC
// nanosecond count, or false (internal/rfc3339).
fn jv_rfc3339(s: const u8[:]) -> i64, bool {
    if s.len < 20 { return 0, false; }
    if s[4] != '-' || s[7] != '-' || s[13] != ':' || s[16] != ':' { return 0, false; }
    if s[10] != 'T' && s[10] != 't' { return 0, false; }
    var year: i64 = 0;
    for i in 4 {
        if s[i] < '0' || s[i] > '9' { return 0, false; }
        let digit: i64 = s[i] - '0';
        year = year * 10 + digit;
    }
    let month, m_ok = jv_two(s, 5);
    let day, d_ok = jv_two(s, 8);
    let hour, h_ok = jv_two(s, 11);
    let minute, n_ok = jv_two(s, 14);
    let second, s_ok = jv_two(s, 17);
    if !m_ok || !d_ok || !h_ok || !n_ok || !s_ok { return 0, false; }
    if month < 1 || month > 12 { return 0, false; }
    if day < 1 || day > jv_month_days(year, month) { return 0, false; }
    if hour > 23 || minute > 59 || second > 59 { return 0, false; }
    var at: i64 = 19;
    var nanos: i64 = 0;
    if at < s.len && s[at] == '.' {
        at += 1;
        var digits: i64 = 0;
        while at < s.len && s[at] >= '0' && s[at] <= '9' {
            let digit: i64 = s[at] - '0';
            if digits < 9 { nanos = nanos * 10 + digit; }
            digits += 1;
            at += 1;
        }
        if digits == 0 { return 0, false; }
        while digits < 9 {
            nanos *= 10;
            digits += 1;
        }
    }
    if at >= s.len { return 0, false; }
    var offset: i64 = 0;
    if s[at] == 'Z' || s[at] == 'z' {
        at += 1;
        if at != s.len { return 0, false; }
    } else if s[at] == '+' || s[at] == '-' {
        let sign: i64 = if s[at] == '-' { -1 } else { 1 };
        at += 1;
        let oh, oh_ok = jv_two(s, at);
        if !oh_ok { return 0, false; }
        at += 2;
        if at >= s.len || s[at] != ':' { return 0, false; }
        at += 1;
        let om, om_ok = jv_two(s, at);
        if !om_ok { return 0, false; }
        at += 2;
        if oh > 23 || om > 59 || at != s.len { return 0, false; }
        offset = sign * (oh * 3600 + om * 60);
    } else {
        return 0, false;
    }
    let seconds = jv_days_from_civil(year, month, day) * 86400 + hour * 3600 + minute * 60 + second - offset;
    if seconds < -9223372037 || seconds > 9223372036 { return 0, false; }
    if seconds == -9223372037 && nanos < 145224192 { return 0, false; }
    if seconds == 9223372036 && nanos > 854775807 { return 0, false; }
    return seconds * 1000000000 + nanos, true;
}

fn jv_put2(out: u8[>..]&, v: i64) {
    out.push(((v / 10) as! u8) + '0');
    out.push(((v % 10) as! u8) + '0');
}

// jv_put_stamp writes a count as RFC3339Nano in UTC: a fraction's trailing
// zeros dropped and the fraction omitted on a whole second. The second and the
// day are FLOORED (docs/SPEC-TABLES.md §16.14, W13), so -1 is
// 1969-12-31T23:59:59.999999999Z; each remainder is taken as the dividend less
// quotient times divisor, which floors under either rounding of `/`.
fn jv_put_stamp(out: u8[>..]&, nanos: i64) {
    var secs = nanos / 1000000000;
    var frac = nanos - secs * 1000000000;
    if frac < 0 {
        frac += 1000000000;
        secs -= 1;
    }
    var days = secs / 86400;
    var rem = secs - days * 86400;
    if rem < 0 {
        rem += 86400;
        days -= 1;
    }
    // civil from days (the proleptic Gregorian calendar)
    let z = days + 719468;
    let era = (if z >= 0 { z } else { z - 146096 }) / 146097;
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = if mp < 10 { mp + 3 } else { mp - 9 };
    let year = yoe + era * 400 + (if month <= 2 { 1 } else { 0 });
    out.push('"');
    jv_put2(out, year / 100);
    jv_put2(out, year % 100);
    out.push('-');
    jv_put2(out, month);
    out.push('-');
    jv_put2(out, day);
    out.push('T');
    jv_put2(out, rem / 3600);
    out.push(':');
    jv_put2(out, (rem / 60) % 60);
    out.push(':');
    jv_put2(out, rem % 60);
    if frac != 0 {
        var digits: u8[9] = [0; 9];
        var f = frac;
        var i: i64 = 9;
        while i > 0 {
            i -= 1;
            digits[i] = ((f % 10) as! u8) + '0';
            f /= 10;
        }
        var n: i64 = 9;
        while digits[n - 1] == '0' { n -= 1; }
        out.push('.');
        out.append(digits[..n]);
    }
    out.append("Z\"");
}

// jv_put_units writes a wstring's UTF-16LE units as a JSON string: a pair is
// one code point, an unpaired surrogate one U+FFFD, a zero unit \u0000.
fn jv_put_units(out: u8[>..]&, b: const u8[:]) {
    let hex = "0123456789abcdef";
    out.push('"');
    let n = b.len / 2;
    var i: i64 = 0;
    while i < n {
        let b0: i64 = b[2 * i];
        let b1: i64 = b[2 * i + 1];
        var code = b0 | (b1 << 8);
        if code >= 0xD800 && code <= 0xDBFF && i + 1 < n {
            let b2: i64 = b[2 * i + 2];
            let b3: i64 = b[2 * i + 3];
            let low = b2 | (b3 << 8);
            if low >= 0xDC00 && low <= 0xDFFF {
                code = 0x10000 + (code - 0xD800) * 1024 + (low - 0xDC00);
                i += 1;
            }
        }
        if code >= 0xD800 && code <= 0xDFFF { code = 0xFFFD; }
        if code == '"' {
            out.append("\\\"");
        } else if code == '\\' {
            out.append("\\\\");
        } else if code == 8 {
            out.append("\\b");
        } else if code == 12 {
            out.append("\\f");
        } else if code == 10 {
            out.append("\\n");
        } else if code == 13 {
            out.append("\\r");
        } else if code == 9 {
            out.append("\\t");
        } else if code < 0x20 {
            out.append("\\u00");
            out.push(hex[code >> 4]);
            out.push(hex[code & 15]);
        } else {
            var unit: u8[4] = [0; 4];
            let w = json_encode_utf8(code, unit);
            out.append(unit[..w]);
        }
        i += 1;
    }
    out.push('"');
}

// ======================================================================
// ---- THE WRITE (§16.1, §16.7; internal/tabletext write.go) ----
// ======================================================================

// JvOut is the write's context of borrowed tables. count, label and open are
// by WIRE node index (1 the root, i + 2 node i): how many written slots name
// the node, the label a shared node took at its first write, and whether its
// descent is still open in the first pass.
struct JvOut {
    unit: LoadUnit,
    text: JsonUnit,
    data: const u8[:],
    vals: const LoadVal[:],
    nodes: const LoadNode[:],
    count: i64[>..]&,
    label: u64[>..]&,
    open: bool[>..]&,
    scratch: i64[>..<]&,
    next: u64,
    bad: bool,
}

fn jw_edge(c: JvOut&, s: i64) -> i64 {
    if s < 0 || c.vals[s].state != ValPresent { return 0; }
    let w = c.vals[s].bits.lo as! i64;
    if w < 0 || w > c.nodes.len + 1 { return 0; }
    return w;
}

// jw_view is a text, bytes or wstring slot's bytes, or its declared default.
fn jw_view(c: JvOut&, s: i64, d: FieldDesc) -> const u8[:] {
    if jv_state(c.vals, s) == ValAbsent {
        if d.has_def { return d.def_s; }
        return "";
    }
    let off = c.vals[s].off;
    let n = c.vals[s].len;
    if off < 0 || n < 0 || off > c.data.len || n > c.data.len - off { return ""; }
    return c.data[off..off + n];
}

// ---- PASS ONE (§16.7): every written slot that names a node, counted ----

recursive fn jw_count_rec(c: JvOut&, tab: i64, rec: i64) {
    let n = c.unit.tables[tab].fields.len;
    var i: i64 = 0;
    while i < n {
        if c.bad { return; }
        let d = c.unit.tables[tab].fields[i];
        let s = jv_slot(c.vals, rec, i);
        if jv_guard(c.unit, c.vals, tab, rec, i) && !(d.optional && jv_state(c.vals, s) == ValAbsent) {
            jw_count_value(c, s, d);
        }
        i += 1;
    }
}

fn jw_count_node(c: JvOut&, wire: i64) {
    if wire <= 0 { return; }
    c.count[wire] += 1;
    if c.open[wire] {
        c.bad = true; // a data cycle (§3.1)
        return;
    }
    if c.count[wire] > 1 || wire < 2 { return; }
    let n = wire - 2;
    if c.nodes[n].blob != 0 || c.nodes[n].tab < 0 || c.nodes[n].val < 0 { return; }
    c.open[wire] = true;
    jw_count_rec(c, c.nodes[n].tab, c.nodes[n].val);
    c.open[wire] = false;
}

recursive fn jw_count_value(c: JvOut&, s: i64, d: FieldDesc) {
    if s < 0 { return; }
    if d.shape == ShapePtr || d.shape == ShapeSBlob || d.shape == ShapeBBlob {
        jw_count_node(c, jw_edge(c, s));
        return;
    }
    if d.shape == ShapeTab {
        if c.vals[s].made { jw_count_rec(c, c.vals[s].tab, s); }
        return;
    }
    if d.shape == ShapeUnion {
        jw_count_arm(c, s, d.ref);
        return;
    }
    if d.shape == ShapeMap {
        if !c.vals[s].made { return; }
        for k in c.vals[s].len {
            let e = c.vals[s].first + k;
            if c.vals[e].made { jw_count_rec(c, c.vals[e].tab, e); }
        }
        return;
    }
    if d.shape == ShapeArr {
        let e = d.elem;
        guard e else { return; }
        if !c.vals[s].made { return; }
        let live = if d.array == ArrayCounted || d.array == ArrayList { c.vals[s].len } else { c.vals[s].count };
        for k in live {
            let es = c.vals[s].first + k;
            jw_count_value(c, es, e);
        }
    }
}

fn jw_count_arm(c: JvOut&, s: i64, name: const u8[:]) {
    let tag = c.vals[s].bits.lo as! i64;
    if c.vals[s].state != ValPresent || tag == 0 || !c.vals[s].made { return; }
    let u = jv_union(c.unit, name);
    if u < 0 || tag > c.unit.unions[u].arms.len { return; }
    jw_count_value(c, c.vals[s].first, c.unit.unions[u].arms[tag - 1]);
}

// ---- PASS TWO: the text ----

// jw_rec writes one record as one object (writeValue): every field in
// declaration order, defaults included, an absent optional as no key, and a
// `json = "array"` table as its one field's array (§16.10).
recursive fn jw_rec(c: JvOut&, out: u8[>..]&, tab: i64, rec: i64, depth: i64) -> bool {
    if depth > JsonMaxDepth { return false; }
    let t = c.unit.tables[tab];
    if t.json_array && t.fields.len == 1 {
        return jw_field(c, out, jv_slot(c.vals, rec, 0), t.fields[0], t.name, 0, depth);
    }
    var any = false;
    if !jw_fields(c, out, tab, rec, depth, any) { return false; }
    if !any {
        out.append("{}");
        return true;
    }
    json_line(out, depth);
    out.push('}');
    return true;
}

// jw_fields writes a record's keys; `any` says whether the object is open on
// entry (a shared node's `&node` opens it) and on return.
recursive fn jw_fields(c: JvOut&, out: u8[>..]&, tab: i64, rec: i64, depth: i64, any: bool&) -> bool {
    let n = c.unit.tables[tab].fields.len;
    var i: i64 = 0;
    while i < n {
        let d = c.unit.tables[tab].fields[i];
        let s = jv_slot(c.vals, rec, i);
        if jv_guard(c.unit, c.vals, tab, rec, i) && !(d.optional && jv_state(c.vals, s) == ValAbsent) {
            if d.required && d.ranged && d.shape == ShapeInt {
                // an out-of-range required value is invalid (§2.3)
                let v = jv_int(c.vals, s, d);
                let lo, hi = jv_raw_range(d);
                if cmp128(v, d.signed, lo, d.signed) < 0 || cmp128(v, d.signed, hi, d.signed) > 0 { return false; }
            }
            if any { out.push(','); } else { out.push('{'); }
            any = true;
            json_line(out, depth + 1);
            json_put_string(out, d.json);
            out.append(": ");
            if !jw_field(c, out, s, d, c.unit.tables[tab].name, i as! i32, depth + 1) { return false; }
        }
        i += 1;
    }
    return true;
}

// jw_pointer writes the node a pointer slot names (§16.7): null, a node named
// once as its object in place, and a node named more than once DEFINED at its
// first write — `&node` first, then its fields — and REFERENCED by `&node`
// alone after.
recursive fn jw_pointer(c: JvOut&, out: u8[>..]&, wire: i64, depth: i64) -> bool {
    if wire <= 0 {
        out.append("null");
        return true;
    }
    if wire < 2 { return false; } // the root: a cycle, refused in pass one
    let n = wire - 2;
    let tab = c.nodes[n].tab;
    if tab < 0 { return false; }
    if c.count[wire] <= 1 { return jw_rec(c, out, tab, c.nodes[n].val, depth); }
    if depth > JsonMaxDepth { return false; }
    if c.label[wire] != 0 {
        out.push('{');
        json_line(out, depth + 1);
        out.append("\"&node\": ");
        json_put_uint(out, c.label[wire]);
        json_line(out, depth);
        out.push('}');
        return true;
    }
    c.next += 1;
    c.label[wire] = c.next;
    out.push('{');
    json_line(out, depth + 1);
    out.append("\"&node\": ");
    json_put_uint(out, c.next);
    var any = true;
    let before = out.len;
    if !jw_fields(c, out, tab, c.nodes[n].val, depth, any) { return false; }
    // a definition carries at least one field: a label alone is a reference
    if out.len == before { return false; }
    json_line(out, depth);
    out.push('}');
    return true;
}

// jw_field writes one field's (or arm's, or map value's) value: a byte
// buffer, text, a map, a keyed or plain array, or one value.
recursive fn jw_field(c: JvOut&, out: u8[>..]&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, depth: i64) -> bool {
    if d.shape == ShapeSBlob || d.shape == ShapeBBlob { return jw_blob(c, out, s, d); }
    if d.shape == ShapeStr && d.wide {
        if jv_state(c.vals, s) == ValAbsent {
            json_put_string(out, if d.has_def { d.def_s } else { "" });
        } else {
            jv_put_units(out, jw_view(c, s, d));
        }
        return true;
    }
    if d.shape == ShapeStr || d.shape == ShapeText {
        json_put_string(out, jw_view(c, s, d));
        return true;
    }
    if d.shape == ShapeBytes {
        json_put_base64(out, jw_view(c, s, d));
        return true;
    }
    if d.shape == ShapeMap { return jw_map(c, out, s, d, owner, depth); }
    if d.shape == ShapeArr {
        if d.array == ArrayKeyed { return jw_keyed(c, out, s, d, owner, ord, depth); }
        return jw_array(c, out, s, d, owner, ord, depth);
    }
    return jw_value(c, out, s, d, owner, ord, false, depth);
}

fn jw_blob(c: JvOut&, out: u8[>..]&, s: i64, d: FieldDesc) -> bool {
    let wire = jw_edge(c, s);
    if wire < 2 {
        out.append("null");
        return true;
    }
    // a blob two slots name has no spelling: a string has no first key to
    // carry `&node` (§16.7, blob_shared)
    if c.count[wire] > 1 { return false; }
    let node = c.nodes[wire - 2];
    if node.off < 0 || node.len < 0 || node.off > c.data.len || node.len > c.data.len - node.off { return false; }
    let b = c.data[node.off..node.off + node.len];
    if d.shape == ShapeSBlob { json_put_string(out, b); } else { json_put_base64(out, b); }
    return true;
}

// jw_array writes a plain array or a text list, in index order.
recursive fn jw_array(c: JvOut&, out: u8[>..]&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, depth: i64) -> bool {
    let e = d.elem;
    guard e else { return false; }
    let made = s >= 0 && c.vals[s].made;
    var count: i64 = d.bound;
    if d.array == ArrayCounted || d.array == ArrayList {
        count = if made { c.vals[s].len } else if d.array == ArrayCounted { d.min } else { 0 };
    }
    if count == 0 {
        out.append("[]");
        return true;
    }
    let text = jv_text_list(c.text, owner, ord);
    // a list past its total is refused, as a count past its bound is (§2.13)
    let total = if text { d.text_total } else { 0 };
    if total > 0 && made {
        var sum: i64 = 0;
        var j: i64 = 0;
        while j < count && j < c.vals[s].count {
            let rec = c.vals[s].first + j;
            if c.vals[rec].made {
                let et = c.vals[rec].tab;
                let fd = c.unit.tables[et].fields[0];
                let fs = jv_slot(c.vals, rec, 0);
                if !(fd.optional && jv_state(c.vals, fs) == ValAbsent) { sum += jw_view(c, fs, fd).len; }
            }
            j += 1;
        }
        if sum > total { return false; }
    }
    out.push('[');
    var k: i64 = 0;
    while k < count {
        if k > 0 { out.push(','); }
        json_line(out, depth + 1);
        let es = if made && k < c.vals[s].count { c.vals[s].first + k } else { -1 };
        if text {
            let rec = es;
            let et = if rec >= 0 && c.vals[rec].made { c.vals[rec].tab } else { jv_table(c.unit, e.ref) };
            if et < 0 { return false; }
            let fd = c.unit.tables[et].fields[0];
            let fs = jv_slot(c.vals, rec, 0);
            if fd.optional && jv_state(c.vals, fs) == ValAbsent {
                out.append("null");
            } else if fd.shape == ShapeStr && fd.wide {
                jv_put_units(out, jw_view(c, fs, fd));
            } else {
                json_put_string(out, jw_view(c, fs, fd));
            }
        } else if !jw_value(c, out, es, e, owner, ord, true, depth + 1) {
            return false;
        }
        k += 1;
    }
    json_line(out, depth);
    out.push(']');
    return true;
}

// jw_keyed writes an enum-keyed array as an object keyed by variant name,
// every slot (§2.4, §16.2).
recursive fn jw_keyed(c: JvOut&, out: u8[>..]&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, depth: i64) -> bool {
    let e = d.elem;
    guard e else { return false; }
    let keys = jv_keys(c.unit, owner, d);
    if d.bound <= 0 {
        out.append("{}");
        return true;
    }
    let made = s >= 0 && c.vals[s].made;
    out.push('{');
    var k: i64 = 0;
    while k < d.bound {
        if k > 0 { out.push(','); }
        json_line(out, depth + 1);
        if k >= keys.len { return false; }
        json_put_string(out, keys[k].name);
        out.append(": ");
        let es = if made && k < c.vals[s].count { c.vals[s].first + k } else { -1 };
        if !jw_value(c, out, es, e, owner, ord, true, depth + 1) { return false; }
        k += 1;
    }
    json_line(out, depth);
    out.push('}');
    return true;
}

// jw_key_cmp orders two map entries by key: bytes for a string key, the
// signed or unsigned value for an integer key (§2.8).
fn jw_key_cmp(c: JvOut&, kd: FieldDesc, a: i64, b: i64) -> i64 {
    let x = jv_slot(c.vals, a, 0);
    let y = jv_slot(c.vals, b, 0);
    if kd.shape == ShapeStr || kd.shape == ShapeText {
        let p = jw_view(c, x, kd);
        let q = jw_view(c, y, kd);
        let n = if p.len < q.len { p.len } else { q.len };
        for i in n {
            if p[i] != q[i] { return if p[i] < q[i] { -1 } else { 1 }; }
        }
        if p.len != q.len { return if p.len < q.len { -1 } else { 1 }; }
        return 0;
    }
    return cmp128(jv_int(c.vals, x, kd), kd.signed, jv_int(c.vals, y, kd), kd.signed);
}

// jw_map writes a map as an object keyed by the KEY, in ASCENDING key order;
// an absent `?T` value is `null` (§2.8, §16.2).
recursive fn jw_map(c: JvOut&, out: u8[>..]&, s: i64, d: FieldDesc, owner: const u8[:], depth: i64) -> bool {
    if s < 0 || !c.vals[s].made || c.vals[s].len == 0 {
        out.append("{}");
        return true;
    }
    let et = jv_entry(c.unit, owner, d.slot);
    if et < 0 { return false; }
    let kd = c.unit.tables[et].fields[0];
    let vd = c.unit.tables[et].fields[1];
    let ename = c.unit.tables[et].name;
    let n = c.vals[s].len;
    let mark = c.scratch.len;
    for k in n {
        // insertion by key: the order is the map's, never the storage's
        let e = c.vals[s].first + k;
        c.scratch.push(e);
        var j = c.scratch.len - 1;
        while j > mark && jw_key_cmp(c, kd, c.scratch[j - 1], c.scratch[j]) > 0 {
            let t = c.scratch[j - 1];
            c.scratch[j - 1] = c.scratch[j];
            c.scratch[j] = t;
            j -= 1;
        }
    }
    out.push('{');
    var ok = true;
    var k: i64 = 0;
    while k < n && ok {
        if k > 0 { out.push(','); }
        json_line(out, depth + 1);
        let e = c.scratch[mark + k];
        let ks = jv_slot(c.vals, e, 0);
        if kd.shape == ShapeStr || kd.shape == ShapeText {
            json_put_string(out, jw_view(c, ks, kd));
        } else {
            let v = jv_int(c.vals, ks, kd);
            out.push('"');
            if kd.signed { json_put_int(out, v.lo as! i64); } else { json_put_uint(out, v.lo); }
            out.push('"');
        }
        out.append(": ");
        let vs = jv_slot(c.vals, e, 1);
        if vd.optional && jv_state(c.vals, vs) == ValAbsent {
            out.append("null");
        } else if !jw_field(c, out, vs, vd, ename, 1, depth + 1) {
            ok = false;
        }
        k += 1;
    }
    c.scratch.resize(mark);
    if !ok { return false; }
    json_line(out, depth);
    out.push('}');
    return true;
}

// jw_value writes ONE value (writeScalar): a union, a pointer, a nested
// record, an enum, a flags mask, a bool, a float, a timestamp, a wide kind or
// an integer; at an element (`element`), a text or a byte buffer too.
recursive fn jw_value(c: JvOut&, out: u8[>..]&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, element: bool, depth: i64) -> bool {
    if d.shape == ShapeUnion { return jw_union(c, out, s, d, depth); }
    if d.shape == ShapePtr { return jw_pointer(c, out, jw_edge(c, s), depth); }
    if d.shape == ShapeTab {
        let rec = if s >= 0 && c.vals[s].made { s } else { -1 };
        let tab = if rec >= 0 { c.vals[rec].tab } else { jv_table(c.unit, d.ref) };
        if tab < 0 { return false; }
        return jw_rec(c, out, tab, rec, depth);
    }
    if d.shape == ShapeSBlob || d.shape == ShapeBBlob || d.shape == ShapeStr || d.shape == ShapeText || d.shape == ShapeBytes {
        return jw_field(c, out, s, d, owner, ord, depth);
    }
    if d.shape == ShapeEnum {
        let v = jv_int(c.vals, s, d).lo;
        if v == 0 {
            json_put_string(out, "None");
            return true;
        }
        if v > (d.variants.len as! u64) { return false; }
        let vd = d.variants[(v - 1) as! i64];
        // a strict write refuses a deprecated variant (§2.15)
        if jv_deprecated(c.unit, d.ref, vd.id) { return false; }
        json_put_string(out, vd.name);
        return true;
    }
    if d.shape == ShapeBool { return json_write_bool(out, jv_bool(c.vals, s, d)); }
    if d.shape == ShapeFloat {
        let bits = if jv_state(c.vals, s) == ValAbsent { float_default_bits(d) } else { c.vals[s].bits.lo };
        if d.is_f32 {
            let x = fixed_bits_f32(bits as! u32);
            return json_put_float(out, x, true);
        }
        return json_put_float(out, fixed_bits_f64(bits), false);
    }
    let v = jv_int(c.vals, s, d);
    let fi = jv_fact(c.text, owner, ord);
    if fi >= 0 && c.text.facts[fi].is_flags {
        return json_write_flags(out, v.lo, c.text.facts[fi].flags, depth);
    }
    if jv_stamp(c.text, d, owner, ord) {
        jv_put_stamp(out, v.lo as! i64);
        return true;
    }
    if jv_wide(d) {
        jv_put_wide(out, v, d.signed, d.ibits != 0 || d.fbits != 0, d.fbits);
        return true;
    }
    if d.signed { json_put_int(out, v.lo as! i64); } else { json_put_uint(out, v.lo); }
    return true;
}

// jw_union writes a union as an object with ONE key, the arm's name; None is
// `{}`, a payload-free arm's value `null`, and the arm's value takes the
// arm's own row (§16.2).
recursive fn jw_union(c: JvOut&, out: u8[>..]&, s: i64, d: FieldDesc, depth: i64) -> bool {
    let tag = if jv_state(c.vals, s) == ValAbsent { 0 } else { c.vals[s].bits.lo as! i64 };
    if tag == 0 {
        out.append("{}");
        return true;
    }
    let u = jv_union(c.unit, d.ref);
    if u < 0 || tag > c.unit.unions[u].arms.len { return false; }
    let arm = c.unit.unions[u].arms[tag - 1];
    out.push('{');
    json_line(out, depth + 1);
    json_put_string(out, arm.name);
    out.append(": ");
    let p = if c.vals[s].made { c.vals[s].first } else { -1 };
    if arm.shape == ShapeVoid {
        out.append("null");
    } else if arm.shape == ShapeTab {
        let rec = if p >= 0 && c.vals[p].made { p } else { -1 };
        let tab = if rec >= 0 { c.vals[rec].tab } else { jv_table(c.unit, arm.ref) };
        if tab < 0 || !jw_rec(c, out, tab, rec, depth + 1) { return false; }
    } else if !jw_field(c, out, p, arm, c.unit.unions[u].name, arm.tag as! i32, depth + 1) {
        return false;
    }
    json_line(out, depth);
    out.push('}');
    return true;
}

// json_save_table appends root `root`'s canonical text, the record at slot
// `at` of the flat value table, ending in exactly one newline, and answers
// its length; -1 appends nothing (§16.1, §16.3, §16.7).
fn json_save_table(unit: LoadUnit, text_unit: JsonUnit, root: const u8[:], data: const u8[:], vals: const LoadVal[:], nodes: const LoadNode[:], at: i64, out: u8[>..]&) -> i64 {
    let t = jv_table(unit, root);
    if t < 0 || !jv_has_text(text_unit, root) { return -1; }
    if at < 0 || at >= vals.len || !vals[at].made || vals[at].tab != t { return -1; }
    var count: i64[>..] = [];
    var label: u64[>..] = [];
    var open: bool[>..] = [];
    for i in nodes.len + 2 {
        count.push(0);
        label.push(0);
        open.push(false);
    }
    var scratch: i64[>..<] = [];
    var c = JvOut { unit: unit, text: text_unit, data: data, vals: vals, nodes: nodes, count: count, label: label, open: open, scratch: scratch, next: 0, bad: false };
    // the root is open for the whole first pass: a reference back at it is a
    // cycle, and it takes no label (§16.7)
    c.open[1] = true;
    jw_count_rec(c, t, at);
    if c.bad { return -1; }
    var text: u8[>..] = [];
    if !jw_rec(c, text, t, at, 0) { return -1; }
    text.push('\n');
    out.append(text);
    return text.len;
}

// ======================================================================
// ---- THE READ (§16.2, §16.7; internal/tabletext read.go) ----
// ======================================================================

// JvIn is the read's context of borrowed tables: the text cursor, the stores
// the read places into, the label map and the walk's scratch.
struct JvIn {
    jr: JsonIn&,
    unit: LoadUnit,
    text: JsonUnit,
    data: u8[>..]&,
    vals: LoadVal[>..]&,
    nodes: LoadNode[>..]&,
    labels: JsonLabel[>..]&,
    scratch: i64[>..<]&,
    graph: bool, // the root is variable: `&node` is the form's own construct
}

fn jr_reserve(c: JvIn&, n: i64) -> i64 {
    let first = c.vals.len;
    for i in n { c.vals.push(LoadVal { .. }); }
    return first;
}

// jr_record makes slot v a record of table tab at its defaults: every field
// slot ABSENT, which is its declared default (§4).
fn jr_record(c: JvIn&, v: i64, tab: i64) {
    let n = c.unit.tables[tab].fields.len;
    let first = jr_reserve(c, n);
    for i in n { c.vals[first + i].shape = c.unit.tables[tab].fields[i].shape; }
    c.vals[v].state = ValPresent;
    c.vals[v].shape = ShapeTab;
    c.vals[v].made = true;
    c.vals[v].tab = tab;
    c.vals[v].first = first;
    c.vals[v].count = n;
}

fn jr_new_record(c: JvIn&, tab: i64) -> i64 {
    let v = jr_reserve(c, 1);
    jr_record(c, v, tab);
    return v;
}

// jr_node places a new node record of table tab and answers its wire index.
fn jr_node(c: JvIn&, tab: i64, rec: i64) -> i64 {
    c.nodes.push(LoadNode { tab: tab, blob: 0, off: 0, len: 0, val: rec, named: false });
    return c.nodes.len + 1;
}

fn jr_point(c: JvIn&, s: i64, wire: i64) {
    c.vals[s].state = ValPresent;
    c.vals[s].bits = Int128 { lo: wire as! u64, hi: 0 };
}

fn jr_null(c: JvIn&, s: i64) {
    c.vals[s].state = ValEmpty;
    c.vals[s].bits = Int128 { lo: 0, hi: 0 };
}

fn jr_place(c: JvIn&, s: i64, v: Int128) {
    c.vals[s].state = ValPresent;
    c.vals[s].bits = v;
}

fn jr_place_view(c: JvIn&, s: i64, off: i64) {
    c.vals[s].state = ValPresent;
    c.vals[s].off = off;
    c.vals[s].len = c.data.len - off;
}

// jr_reset puts a field back at its defaults: the absence `null` reads as
// for a `?T`, and a null pointer (§16.2).
fn jr_reset(c: JvIn&, s: i64, d: FieldDesc) {
    c.vals[s] = LoadVal { shape: d.shape, .. };
}

// jr_present makes an optional PRESENT at its declared default where the
// read placed nothing: the KEY is the presence (§16.2).
fn jr_present(c: JvIn&, s: i64, d: FieldDesc) {
    if c.vals[s].state != ValAbsent { return; }
    if d.shape == ShapeTab {
        let t = jv_table(c.unit, d.ref);
        if t >= 0 { jr_record(c, s, t); }
        return;
    }
    if d.shape == ShapeArr || d.shape == ShapeMap {
        jr_collection(c, s, d, 0, 0);
        return;
    }
    if d.shape == ShapeStr || d.shape == ShapeText || d.shape == ShapeBytes {
        let off = c.data.len;
        if d.has_def { c.data.append(d.def_s); }
        jr_place_view(c, s, off);
        return;
    }
    if d.shape == ShapeFloat {
        jr_place(c, s, Int128 { lo: float_default_bits(d), hi: 0 });
        return;
    }
    if d.shape == ShapeBool {
        jr_place(c, s, Int128 { lo: if d.has_def && d.def_b { 1 } else { 0 }, hi: 0 });
        return;
    }
    jr_place(c, s, if d.has_def { d.def_i } else { Int128 { lo: 0, hi: 0 } });
}

// jr_default_elem puts an element slot at the element's default: a record
// element is made, a union element None.
fn jr_default_elem(c: JvIn&, v: i64, e: FieldDesc) {
    c.vals[v] = LoadVal { shape: e.shape, .. };
    if e.shape == ShapeTab {
        let t = jv_table(c.unit, e.ref);
        if t >= 0 { jr_record(c, v, t); }
    } else if e.shape == ShapeUnion {
        c.vals[v].state = ValEmpty;
        c.vals[v].tab = jv_union(c.unit, e.ref);
    }
}

// jr_collection makes slot s an array or map of n slots from the scratch run
// at mark (an element's temporary slot, or -1 for its default), live count
// placed. A fixed or counted array has its bound's slots.
fn jr_collection(c: JvIn&, s: i64, d: FieldDesc, mark: i64, placed: i64) {
    var n = placed;
    let e = d.elem;
    if d.shape == ShapeArr && d.array != ArrayList && n < d.bound { n = d.bound; }
    let first = jr_reserve(c, n);
    for k in n {
        let at = first + k;
        if k < placed && c.scratch[mark + k] >= 0 {
            c.vals[at] = c.vals[c.scratch[mark + k]];
        } else if d.shape == ShapeMap {
            c.vals[at] = LoadVal { shape: ShapeTab, .. };
        } else {
            guard e else { continue; }
            jr_default_elem(c, at, e);
        }
    }
    c.vals[s].state = ValPresent;
    c.vals[s].made = true;
    c.vals[s].first = first;
    c.vals[s].count = n;
    c.vals[s].len = if d.shape == ShapeArr && d.array == ArrayFixed { n } else { placed };
}

// jr_shape is the JSON type a value takes (Shape, ElementShape): o a s b n.
fn jr_shape(c: JvIn&, d: FieldDesc, owner: const u8[:], ord: i32, element: bool) -> u8 {
    if !element {
        if d.shape == ShapeStr || d.shape == ShapeText || d.shape == ShapeBytes || d.shape == ShapeSBlob || d.shape == ShapeBBlob { return 's'; }
        if d.shape == ShapeMap { return 'o'; }
        if d.shape == ShapeArr { return if d.array == ArrayKeyed { 'o' } else { 'a' }; }
    }
    if d.shape == ShapeUnion { return 'o'; }
    if d.shape == ShapeTab || d.shape == ShapePtr {
        let t = jv_table(c.unit, d.ref);
        if t >= 0 && c.unit.tables[t].json_array { return 'a'; }
        return 'o';
    }
    if d.shape == ShapeEnum { return 's'; }
    if d.shape == ShapeBool { return 'b'; }
    if jv_flags(c.text, owner, ord) { return 'a'; }
    if jv_stamp(c.text, d, owner, ord) { return 's'; }
    return 'n';
}

// jr_takes_null: a `?T` reads `null` as ABSENT and a pointer as null (§16.2).
fn jr_takes_null(d: FieldDesc) -> bool {
    return d.optional || d.shape == ShapePtr || d.shape == ShapeSBlob || d.shape == ShapeBBlob;
}

// jr_key scans an object key into a key buffer.
fn jr_key(c: JvIn&, key: u8[256]&) -> i64, bool {
    let n, ok = json_key(c.jr, key);
    if !ok { return 0, false; }
    if !json_expect(c.jr, ':') { return 0, false; }
    return n, true;
}

// jr_after takes the separator after a member or an element: 1 to go on, 0
// at the close (consumed), -1 malformed.
fn jr_after(c: JvIn&, closing: u8) -> i64 {
    let ch = json_peek(c.jr);
    if ch == ',' {
        c.jr.pos += 1;
        return 1;
    }
    if ch == closing {
        c.jr.pos += 1;
        return 0;
    }
    c.jr.bad = true;
    return -1;
}

fn jr_label_at(c: JvIn&, label: u64) -> i64 {
    for l, i in c.labels { if l.label == label { return i; } }
    return -1;
}

// jr_scan_label reads `&node`'s value: a positive integer spelled as one —
// digits, no sign, no fraction, no exponent, no leading zero (§16.7).
fn jr_scan_label(c: JvIn&) -> u64, bool {
    json_space(c.jr);
    let t = c.jr.text;
    if c.jr.pos >= t.len || t[c.jr.pos] < '1' || t[c.jr.pos] > '9' {
        c.jr.bad = true;
        return 0, false;
    }
    var v: u64 = 0;
    while c.jr.pos < t.len && t[c.jr.pos] >= '0' && t[c.jr.pos] <= '9' {
        let digit = (t[c.jr.pos] - '0') as u64;
        if v > (0xFFFFFFFFFFFFFFFF - digit) / 10 {
            c.jr.bad = true;
            return 0, false;
        }
        v = v * 10 + digit;
        c.jr.pos += 1;
    }
    return v, true;
}

// ---- skipping: a value the read does not place (§4), whose `&node`
// definitions still take their labels in a variable root (§16.7) ----

recursive fn jr_skip(c: JvIn&, depth: i64) -> bool {
    let ch = json_peek(c.jr);
    if ch != '{' && ch != '[' { return json_skip_value(c.jr, depth); }
    if depth > JsonMaxDepth { return json_fail(c.jr); }
    let closing: u8 = if ch == '{' { '}' } else { ']' };
    c.jr.pos += 1;
    var first = true;
    loop {
        let d = json_peek(c.jr);
        if d == closing {
            c.jr.pos += 1;
            return true;
        }
        if d == 0 { return json_fail(c.jr); }
        var amp = false;
        if closing == '}' {
            var key: u8[256] = [0; 256];
            let n, ok = jr_key(c, key);
            if !ok { return false; }
            if n > 0 && key[0] == '&' && c.graph {
                // a definition in a dropped value keeps its label, with no node
                if !first || key[..n] != "&node" { return json_fail(c.jr); }
                let label, lok = jr_scan_label(c);
                if !lok { return false; }
                if jr_label_at(c, label) < 0 { c.labels.push(JsonLabel { label: label, wire: 0, tab: -1, open: false }); }
                amp = true;
            }
        }
        first = false;
        if !amp && !jr_skip(c, depth + 1) { return false; }
        let r = jr_after(c, closing);
        if r < 0 { return false; }
        if r == 0 { return true; }
    }
}

// ---- tables ----

// jr_table places ONE table object into record rec (readTable).
recursive fn jr_table(c: JvIn&, tab: i64, rec: i64, depth: i64) -> bool {
    if depth > JsonMaxDepth { return json_fail(c.jr); }
    let t = c.unit.tables[tab];
    if t.json_array && t.fields.len == 1 {
        if json_shape(c.jr) != 'a' { return json_fail(c.jr); }
        if !jr_field(c, jv_slot(c.vals, rec, 0), t.fields[0], t.name, 0, depth) {
            if c.jr.zero && c.jr.rep.reason == 0 { c.jr.rep.reason = JsonReasonHoldsZero; }
            return false;
        }
        return true;
    }
    if !json_expect(c.jr, '{') { return false; }
    let none: u8[1] = [0];
    return jr_keys(c, tab, rec, depth, none[..0], false);
}

// jr_required closes one object: a `| required` field whose key the text
// never named, under a guard that holds, refuses the read (§2.3).
fn jr_required(c: JvIn&, tab: i64, rec: i64, seen: const u64[8]&) -> bool {
    let n = c.unit.tables[tab].fields.len;
    var i: i64 = 0;
    while i < n {
        let d = c.unit.tables[tab].fields[i];
        let named = i < 512 && (seen[i >> 6] & ((1 as u64) << ((i & 63) as! u64))) != 0;
        if d.required && !named && jv_guard(c.unit, c.vals, tab, rec, i) {
            if c.jr.rep.reason == 0 { c.jr.rep.reason = JsonReasonMissingRequired; }
            return json_fail(c.jr);
        }
        i += 1;
    }
    return true;
}

// jr_keys places the keys of an object whose brace is consumed
// (readTableKeys). A pointer's object hands in the first key it scanned
// (`first`, when has_first), its colon consumed.
recursive fn jr_keys(c: JvIn&, tab: i64, rec: i64, depth: i64, first: const u8[:], has_first: bool) -> bool {
    var seen: u64[8] = [0; 8];
    var pending = has_first;
    loop {
        var key: u8[256] = [0; 256];
        var n: i64 = 0;
        if pending {
            n = first.len;
            for b, i in first { key[i] = b; }
            pending = false;
        } else {
            let ch = json_peek(c.jr);
            if ch == '}' {
                c.jr.pos += 1;
                return jr_required(c, tab, rec, seen);
            }
            if ch == 0 { return json_fail(c.jr); }
            let kn, ok = jr_key(c, key);
            if !ok { return false; }
            n = kn;
        }
        if n > 0 && key[0] == '&' {
            // the reserved prefix where it cannot stand: malformed, never
            // unknown (§16.7)
            if c.jr.rep.reason == 0 { c.jr.rep.reason = JsonReasonReservedKey; }
            return json_fail(c.jr);
        }
        var index: i64 = -1;
        let fields = c.unit.tables[tab].fields;
        for f, i in fields {
            if f.json == key[..n] {
                index = i;
                break;
            }
        }
        if index < 0 {
            c.jr.rep.unknown += 1;
            if !jr_skip(c, depth + 1) { return false; }
        } else {
            if index < 512 {
                let bit = (1 as u64) << ((index & 63) as! u64);
                if (seen[index >> 6] & bit) != 0 { c.jr.rep.duplicate += 1; }
                seen[index >> 6] |= bit;
            }
            let d = fields[index];
            let s = jv_slot(c.vals, rec, index);
            let got = json_shape(c.jr);
            let name = c.unit.tables[tab].name;
            if got == 'z' && jr_takes_null(d) {
                if !json_literal(c.jr, "null") { return false; }
                jr_reset(c, s, d);
            } else {
                if got != jr_shape(c, d, name, index as! i32, false) {
                    c.jr.rep.kind_mismatch += 1;
                    if !jr_skip(c, depth + 1) { return false; }
                } else if !jr_field(c, s, d, name, index as! i32, depth) {
                    if c.jr.zero && c.jr.rep.reason == 0 { c.jr.rep.reason = JsonReasonHoldsZero; }
                    return false;
                }
                if d.optional { jr_present(c, s, d); }
            }
        }
        let r = jr_after(c, '}');
        if r < 0 { return false; }
        if r == 0 { return jr_required(c, tab, rec, seen); }
    }
}

// jr_field places one field's (or arm's, or map value's) value (readField).
recursive fn jr_field(c: JvIn&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, depth: i64) -> bool {
    if d.shape == ShapeSBlob || d.shape == ShapeBBlob { return jr_blob(c, s, d); }
    if d.shape == ShapePtr { return jr_pointer(c, s, d.ref, depth); }
    if d.shape == ShapeStr && d.wide {
        let off = c.data.len;
        if !jr_units(c, d.bound) { return false; }
        jr_place_view(c, s, off);
        return true;
    }
    if d.shape == ShapeStr || d.shape == ShapeText {
        let off = c.data.len;
        if !jr_text(c, if d.shape == ShapeStr { d.bound } else { -1 }) { return false; }
        jr_place_view(c, s, off);
        return true;
    }
    if d.shape == ShapeBytes {
        let off = c.data.len;
        let ok, placed = jr_base64(c, d.bound);
        if !ok { return false; }
        if placed { jr_place_view(c, s, off); } else { jr_reset(c, s, d); }
        return true;
    }
    if d.shape == ShapeMap { return jr_map(c, s, d, owner, depth); }
    if d.shape == ShapeArr {
        if d.array == ArrayKeyed { return jr_keyed(c, s, d, owner, ord, depth); }
        if jv_text_list(c.text, owner, ord) { return jr_text_list(c, s, d, owner, depth); }
        return jr_array(c, s, d, owner, ord, depth);
    }
    return jr_value(c, s, d, owner, ord, false, depth);
}

// jr_text appends one string's bytes to data, clamped at bound bytes on a
// code point boundary and counted; a U+0000 refuses (§16.2, §16.3).
fn jr_text(c: JvIn&, bound: i64) -> bool {
    var placed: i64 = 0;
    var clamped = false;
    let ok = json_scan(c.jr, true) { unit, n =>
        if !clamped && (bound < 0 || placed + n <= bound) {
            for k in n { c.data.push(unit[k]); }
            placed += n;
        } else {
            clamped = true;
        }
    };
    if !ok { return false; }
    if clamped { c.jr.rep.clamped += 1; }
    return true;
}

// jr_units appends one string's UTF-16LE code units to data, clamped at
// bound units with a pair never split (the wstring row, §16.2).
fn jr_units(c: JvIn&, bound: i64) -> bool {
    var placed: i64 = 0;
    var clamped = false;
    let ok = json_scan(c.jr, true) { unit, n =>
        var code: i64 = unit[0];
        if n == 2 { code = ((unit[0] & 0x1F) as i64) * 64 + (unit[1] & 0x3F) as i64; }
        if n == 3 { code = ((unit[0] & 0x0F) as i64) * 4096 + ((unit[1] & 0x3F) as i64) * 64 + (unit[2] & 0x3F) as i64; }
        if n == 4 { code = ((unit[0] & 0x07) as i64) * 262144 + ((unit[1] & 0x3F) as i64) * 4096 + ((unit[2] & 0x3F) as i64) * 64 + (unit[3] & 0x3F) as i64; }
        let need: i64 = if code >= 0x10000 { 2 } else { 1 };
        if !clamped && (bound < 0 || placed + need <= bound) {
            if need == 2 {
                let v = code - 0x10000;
                let hi = 0xD800 + (v >> 10);
                let lo = 0xDC00 + (v & 0x3FF);
                c.data.push((hi & 0xFF) as! u8);
                c.data.push((hi >> 8) as! u8);
                c.data.push((lo & 0xFF) as! u8);
                c.data.push((lo >> 8) as! u8);
            } else {
                c.data.push((code & 0xFF) as! u8);
                c.data.push((code >> 8) as! u8);
            }
            placed += need;
        } else {
            clamped = true;
        }
    };
    if !ok { return false; }
    if clamped { c.jr.rep.clamped += 1; }
    return true;
}

// jr_base64 decodes one base64 string onto data (the bytes(N) row, §16.2):
// placed false for a body outside the grammar, which is kind_mismatch and
// appends nothing; past bound (when bound >= 0) clamps and counts.
fn jr_base64(c: JvIn&, bound: i64) -> bool, bool {
    var symbols: i64 = 0;
    var pads: i64 = 0;
    var bad = false;
    let start = c.jr.pos;
    let ok = json_scan(c.jr, false) { unit, n =>
        for k in n {
            let ch = unit[k];
            if !bad {
                if ch == '=' {
                    pads += 1;
                } else if pads > 0 || json_b64_sextet(ch) < 0 {
                    bad = true;
                } else {
                    symbols += 1;
                }
            }
        }
    };
    if !ok { return false, false; }
    if !bad {
        if pads > 0 {
            if pads > 2 || (symbols + pads) % 4 != 0 || symbols % 4 < 2 { bad = true; }
        } else if symbols % 4 == 1 {
            bad = true;
        }
    }
    if bad {
        c.jr.rep.kind_mismatch += 1;
        return true, false;
    }
    let end = c.jr.pos;
    c.jr.pos = start;
    var placed: i64 = 0;
    var acc: u64 = 0;
    var held: u64 = 0;
    var clamped = false;
    var done = false;
    let again = json_scan(c.jr, false) { unit, n =>
        for k in n {
            let ch = unit[k];
            if ch == '=' { done = true; }
            if !done {
                acc = (acc << 6) | (json_b64_sextet(ch) as! u64);
                held += 6;
                if held >= 8 {
                    held -= 8;
                    if bound < 0 || placed < bound {
                        c.data.push(((acc >> held) & 0xFF) as! u8);
                        placed += 1;
                    } else {
                        clamped = true;
                    }
                }
            }
        }
    };
    if !again { return false, false; }
    c.jr.pos = end;
    if clamped { c.jr.rep.clamped += 1; }
    return true, true;
}

// jr_blob places a BYTE BUFFER (§2.5, §16.7): a `*string`'s string, a
// `*bytes`'s base64 with no bound; a body that is not base64 is
// kind_mismatch, the reference left null.
fn jr_blob(c: JvIn&, s: i64, d: FieldDesc) -> bool {
    jr_null(c, s);
    let off = c.data.len;
    if d.shape == ShapeSBlob {
        if !jr_text(c, -1) { return false; }
    } else {
        let ok, placed = jr_base64(c, -1);
        if !ok { return false; }
        if !placed { return true; }
    }
    let kind = if d.shape == ShapeSBlob { KindString } else { KindArray };
    c.nodes.push(LoadNode { tab: -1, blob: kind, off: off, len: c.data.len - off, val: -1, named: true });
    jr_point(c, s, c.nodes.len + 1);
    return true;
}

// jr_pointer places one pointer slot from its object (§16.7): its FIRST key
// decides — `&node` with fields after it DEFINES a label, `&node` alone REFERS
// to one already defined, and any other key is a node named once, its object
// in place.
recursive fn jr_pointer(c: JvIn&, s: i64, ref: const u8[:], depth: i64) -> bool {
    let tab = jv_table(c.unit, ref);
    if !c.graph || tab < 0 { return json_fail(c.jr); }
    // the pointee nests one level down: a chain nests as deep as it is long
    if depth + 1 > JsonMaxDepth { return json_fail(c.jr); }
    if !json_expect(c.jr, '{') { return false; }
    let ch = json_peek(c.jr);
    if ch == '}' {
        c.jr.pos += 1;
        let rec = jr_new_record(c, tab);
        jr_point(c, s, jr_node(c, tab, rec));
        let seen: u64[8] = [0; 8];
        return jr_required(c, tab, rec, seen);
    }
    if ch == 0 { return json_fail(c.jr); }
    var key: u8[256] = [0; 256];
    let n, ok = jr_key(c, key);
    if !ok { return false; }
    if key[..n] != "&node" {
        let rec = jr_new_record(c, tab);
        jr_point(c, s, jr_node(c, tab, rec));
        return jr_keys(c, tab, rec, depth + 1, key[..n], true);
    }
    let label, lok = jr_scan_label(c);
    if !lok { return false; }
    let at = jr_label_at(c, label);
    var next = json_peek(c.jr);
    if next == ',' {
        c.jr.pos += 1;
        next = json_peek(c.jr);
    }
    let bare = next == '}';
    if bare != (at >= 0) { return json_fail(c.jr); }
    if bare {
        // a REFERENCE: to an open definition is the cycle §3.1 refuses; to a
        // dropped one reads null; to a node of another table is kind_mismatch
        c.jr.pos += 1;
        let l = c.labels[at];
        if l.open { return json_fail(c.jr); }
        jr_null(c, s);
        if l.wire == 0 { return true; }
        if l.tab != tab {
            c.jr.rep.kind_mismatch += 1;
            return true;
        }
        jr_point(c, s, l.wire);
        return true;
    }
    // a DEFINITION, open until its object closes
    let rec = jr_new_record(c, tab);
    let wire = jr_node(c, tab, rec);
    jr_point(c, s, wire);
    let mine = c.labels.len;
    c.labels.push(JsonLabel { label: label, wire: wire, tab: tab, open: true });
    let none: u8[1] = [0];
    if !jr_keys(c, tab, rec, depth + 1, none[..0], false) { return false; }
    c.labels[mine].open = false;
    return true;
}

// jr_array places a [N]T, a [..N]T or a []T (readArray): every slot back at
// its default, past the bound dropped and counted, a wrong-shaped element
// kind_mismatch keeping its slot, a null element of an array of pointers a
// null slot.
recursive fn jr_array(c: JvIn&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, depth: i64) -> bool {
    let e = d.elem;
    guard e else { return json_fail(c.jr); }
    if !json_expect(c.jr, '[') { return false; }
    let bound = if d.array == ArrayList { 0x7FFFFFFF } else { d.bound };
    let shape = jr_shape(c, e, owner, ord, true);
    let mark = c.scratch.len;
    var placed: i64 = 0;
    var ok = true;
    loop {
        let ch = json_peek(c.jr);
        if ch == ']' {
            c.jr.pos += 1;
            break;
        }
        if ch == 0 {
            ok = json_fail(c.jr);
            break;
        }
        let got = json_shape(c.jr);
        if placed >= bound {
            c.jr.rep.clamped += 1;
            if !jr_skip(c, depth + 1) { ok = false; break; }
        } else if e.shape == ShapePtr && got == 'z' {
            if !json_literal(c.jr, "null") { ok = false; break; }
            c.scratch.push(-1);
            placed += 1;
        } else if got != shape {
            c.jr.rep.kind_mismatch += 1;
            if !jr_skip(c, depth + 1) { ok = false; break; }
            c.scratch.push(-1);
            placed += 1;
        } else {
            let t = jr_reserve(c, 1);
            jr_default_elem(c, t, e);
            c.scratch.push(t);
            placed += 1;
            if !jr_value(c, t, e, owner, ord, true, depth + 1) { ok = false; break; }
        }
        let r = jr_after(c, ']');
        if r < 0 { ok = false; break; }
        if r == 0 { break; }
    }
    if ok { jr_collection(c, s, d, mark, placed); }
    c.scratch.resize(mark);
    return ok;
}

// jr_text_list places a TEXT LIST (readTextList): strings, `null` for an
// absent `?string(M)` element; an element past M clamps, past N drops. A
// total keeps the longest prefix whose bytes are within it: the element that
// would carry the sum past it and every element after it drop, clamped
// counted once (§2.13).
fn jr_text_list(c: JvIn&, s: i64, d: FieldDesc, owner: const u8[:], depth: i64) -> bool {
    let total = d.text_total;
    let e = d.elem;
    guard e else { return json_fail(c.jr); }
    let et = jv_table(c.unit, e.ref);
    if et < 0 { return json_fail(c.jr); }
    let fd = c.unit.tables[et].fields[0];
    if !json_expect(c.jr, '[') { return false; }
    let bound = if d.array == ArrayList { 0x7FFFFFFF } else { d.bound };
    let mark = c.scratch.len;
    var placed: i64 = 0;
    var spent: i64 = 0;
    var full = false;
    var seen: i64 = 0; // the elements spelled once the total is reached
    var ok = true;
    loop {
        let ch = json_peek(c.jr);
        if ch == ']' {
            c.jr.pos += 1;
            break;
        }
        if ch == 0 {
            ok = json_fail(c.jr);
            break;
        }
        let got = json_shape(c.jr);
        if full {
            // the total dropped this element; one past [..N] is counted as
            // well, as the wire counts its count clamp (§2.13)
            seen += 1;
            if seen > bound { c.jr.rep.clamped += 1; }
            if !json_skip_value(c.jr, depth + 1) { ok = false; break; }
        } else if placed >= bound {
            c.jr.rep.clamped += 1;
            if !json_skip_value(c.jr, depth + 1) { ok = false; break; }
        } else if fd.optional && got == 'z' {
            if !json_literal(c.jr, "null") { ok = false; break; }
            c.scratch.push(-1);
            placed += 1;
        } else if got != 's' {
            c.jr.rep.kind_mismatch += 1;
            if !json_skip_value(c.jr, depth + 1) { ok = false; break; }
            c.scratch.push(-1);
            placed += 1;
        } else {
            let t = jr_new_record(c, et);
            let fs = c.vals[t].first;
            let off = c.data.len;
            let good = if fd.shape == ShapeStr && fd.wide { jr_units(c, fd.bound) } else { jr_text(c, if fd.shape == ShapeStr { fd.bound } else { -1 }) };
            if !good { ok = false; break; }
            jr_place_view(c, fs, off);
            let n = c.data.len - off;
            if total > 0 && n > total - spent {
                c.jr.rep.clamped += 1;
                json_clamped(c.jr.rep, owner, d.name);
                full = true;
                seen = placed + 1;
            } else {
                spent += n;
                c.scratch.push(t);
                placed += 1;
            }
        }
        let r = jr_after(c, ']');
        if r < 0 { ok = false; break; }
        if r == 0 { break; }
    }
    if ok { jr_collection(c, s, d, mark, placed); }
    c.scratch.resize(mark);
    return ok;
}

// jr_keyed places an enum-keyed array (readKeyed): an object keyed by variant
// NAME; a repeated slot is last-wins and counted, an unknown name (`None`
// included) counted unknown.
recursive fn jr_keyed(c: JvIn&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, depth: i64) -> bool {
    let e = d.elem;
    guard e else { return json_fail(c.jr); }
    if !json_expect(c.jr, '{') { return false; }
    let keys = jv_keys(c.unit, owner, d);
    let shape = jr_shape(c, e, owner, ord, true);
    jr_collection(c, s, d, c.scratch.len, 0);
    var seen: u64[8] = [0; 8];
    loop {
        let ch = json_peek(c.jr);
        if ch == '}' {
            c.jr.pos += 1;
            return true;
        }
        if ch == 0 { return json_fail(c.jr); }
        var key: u8[256] = [0; 256];
        let n, ok = jr_key(c, key);
        if !ok { return false; }
        var slot: i64 = -1;
        for k, i in keys {
            if i < d.bound && k.name == key[..n] {
                slot = i;
                break;
            }
        }
        if slot >= 0 && slot < 512 {
            let bit = (1 as u64) << ((slot & 63) as! u64);
            if (seen[slot >> 6] & bit) != 0 { c.jr.rep.duplicate += 1; }
            seen[slot >> 6] |= bit;
        }
        if slot < 0 {
            c.jr.rep.unknown += 1;
            if !jr_skip(c, depth + 1) { return false; }
        } else if json_shape(c.jr) != shape {
            c.jr.rep.kind_mismatch += 1;
            if !jr_skip(c, depth + 1) { return false; }
        } else if !jr_value(c, c.vals[s].first + slot, e, owner, ord, true, depth + 1) {
            return false;
        }
        let r = jr_after(c, '}');
        if r < 0 { return false; }
        if r == 0 { return true; }
    }
}

// jr_map_key places an object key into an entry's key slot: 0 placed, 1 a
// value the key kind does not hold (the entry drops, kind_mismatch), 2 not a
// JSON number at all (malformed). A key is its spelling, so a key with
// anything around the number is malformed (§2.8, §16.2).
fn jr_map_key(c: JvIn&, ks: i64, kd: FieldDesc, off: i64) -> i64 {
    if kd.shape == ShapeStr || kd.shape == ShapeText {
        c.vals[ks].state = ValPresent;
        c.vals[ks].off = off;
        c.vals[ks].len = c.data.len - off;
        return 0;
    }
    let tok = c.data[off..];
    var probe = json_in(tok);
    json_space(probe);
    if probe.pos != 0 { return 2; }
    let whole, ok = json_walk_number(probe);
    if !ok || probe.pos != tok.len { return 2; }
    let v, exact, sat = jv_parse_wide(tok, kd.signed, 0);
    if !exact || sat { return 1; }
    // the key kind's own domain: a value outside it drops, never clamps
    if kd.kw < 16 {
        let bits = (kd.kw as i64) * 8;
        if kd.signed {
            let lo = jv_neg128(Int128 { lo: (1 as u64) << ((bits - 1) as! u64), hi: 0 });
            let hi = Int128 { lo: ((1 as u64) << ((bits - 1) as! u64)) - 1, hi: 0 };
            if cmp128(v, true, lo, true) < 0 || cmp128(v, true, hi, true) > 0 { return 1; }
        } else {
            let hi = if bits >= 64 { 0xFFFFFFFFFFFFFFFF } else { ((1 as u64) << (bits as! u64)) - 1 };
            if v.hi != 0 || v.lo > hi { return 1; }
        }
    }
    jr_place(c, ks, v);
    return 0;
}

fn jr_key_cmp(c: JvIn&, kd: FieldDesc, a: i64, b: i64) -> i64 {
    let x = c.vals[a].first;
    let y = c.vals[b].first;
    if kd.shape == ShapeStr || kd.shape == ShapeText {
        let p = c.vals[x];
        let q = c.vals[y];
        let n = if p.len < q.len { p.len } else { q.len };
        for i in n {
            let u = c.data[p.off + i];
            let w = c.data[q.off + i];
            if u != w { return if u < w { -1 } else { 1 }; }
        }
        if p.len != q.len { return if p.len < q.len { -1 } else { 1 }; }
        return 0;
    }
    return cmp128(c.vals[x].bits, kd.signed, c.vals[y].bits, kd.signed);
}

// jr_map places a MAP from an object keyed by the KEY (readMap): a string key
// past its bound drops its entry, counted clamped; an integer key is read by
// the integer rule and nothing else; a repeated key is last-wins and counted;
// the entries land in ASCENDING key order (§2.8, §16.2).
recursive fn jr_map(c: JvIn&, s: i64, d: FieldDesc, owner: const u8[:], depth: i64) -> bool {
    let et = jv_entry(c.unit, owner, d.slot);
    if et < 0 { return json_fail(c.jr); }
    if !json_expect(c.jr, '{') { return false; }
    let kd = c.unit.tables[et].fields[0];
    let vd = c.unit.tables[et].fields[1];
    let ename = c.unit.tables[et].name;
    let mark = c.scratch.len;
    var placed: i64 = 0;
    var ok = true;
    loop {
        let ch = json_peek(c.jr);
        if ch == '}' {
            c.jr.pos += 1;
            break;
        }
        if ch == 0 {
            ok = json_fail(c.jr);
            break;
        }
        let off = c.data.len;
        if !jr_text(c, -1) { ok = false; break; }
        if !json_expect(c.jr, ':') { ok = false; break; }
        let klen = c.data.len - off;
        let entry = jr_new_record(c, et);
        let ks = c.vals[entry].first;
        var drop = false;
        if kd.shape == ShapeStr && (klen > JsonMaxKey || klen > kd.bound) {
            c.jr.rep.clamped += 1;
            drop = true;
        } else if kd.shape != ShapeStr && kd.shape != ShapeText && klen > JsonMaxKey {
            c.jr.rep.kind_mismatch += 1;
            drop = true;
        } else {
            let placed_key = jr_map_key(c, ks, kd, off);
            if placed_key == 2 {
                ok = json_fail(c.jr);
                break;
            }
            if placed_key == 1 {
                c.jr.rep.kind_mismatch += 1;
                drop = true;
            }
        }
        if drop {
            if !jr_skip(c, depth) { ok = false; break; }
        } else {
            // the VALUE: null is an absent `?T` and a null pointer; any
            // other value of an optional is present (§2.3, §2.8)
            let vs = ks + 1;
            let got = json_shape(c.jr);
            if got == 'z' && jr_takes_null(vd) {
                if !json_literal(c.jr, "null") { ok = false; break; }
                jr_reset(c, vs, vd);
            } else if got != jr_shape(c, vd, ename, 1, false) {
                c.jr.rep.kind_mismatch += 1;
                if !jr_skip(c, depth + 1) { ok = false; break; }
                if vd.optional { jr_present(c, vs, vd); }
            } else {
                if !jr_field(c, vs, vd, ename, 1, depth) { ok = false; break; }
                if vd.optional { jr_present(c, vs, vd); }
            }
            var dup: i64 = -1;
            for k in placed {
                if jr_key_cmp(c, kd, c.scratch[mark + k], entry) == 0 { dup = k; }
            }
            if dup >= 0 {
                c.jr.rep.duplicate += 1;
                c.scratch[mark + dup] = entry;
            } else {
                c.scratch.push(entry);
                placed += 1;
                var j = mark + placed - 1;
                while j > mark && jr_key_cmp(c, kd, c.scratch[j - 1], c.scratch[j]) > 0 {
                    let t = c.scratch[j - 1];
                    c.scratch[j - 1] = c.scratch[j];
                    c.scratch[j] = t;
                    j -= 1;
                }
            }
        }
        let r = jr_after(c, '}');
        if r < 0 { ok = false; break; }
        if r == 0 { break; }
    }
    if ok { jr_collection(c, s, d, mark, placed); }
    c.scratch.resize(mark);
    return ok;
}

// jr_value places ONE value (readScalar): a timestamp, a union, a pointer, a
// nested record, an enum, a flags mask, a bool, a float, a wide kind or an
// integer. `element` is an array's or keyed slot's element, whose deprecated
// variant reads None rather than the field's default (§2.15).
recursive fn jr_value(c: JvIn&, s: i64, d: FieldDesc, owner: const u8[:], ord: i32, element: bool, depth: i64) -> bool {
    if jv_stamp(c.text, d, owner, ord) {
        var buf: u8[64] = [0; 64];
        var n: i64 = 0;
        var long = false;
        let ok = json_scan(c.jr, false) { unit, k =>
            for i in k {
                if n < 64 { buf[n] = unit[i]; n += 1; } else { long = true; }
            }
        };
        if !ok { return false; }
        let nanos, good = jv_rfc3339(buf[..n]);
        if long || !good {
            c.jr.rep.reason = JsonReasonTimestamp;
            return json_fail(c.jr);
        }
        jr_place(c, s, Int128 { lo: nanos as! u64, hi: if nanos < 0 { 0xFFFFFFFFFFFFFFFF } else { 0 } });
        return true;
    }
    if d.shape == ShapeUnion { return jr_union(c, s, d.ref, depth); }
    if d.shape == ShapePtr { return jr_pointer(c, s, d.ref, depth); }
    if d.shape == ShapeTab {
        let tab = jv_table(c.unit, d.ref);
        if tab < 0 { return json_fail(c.jr); }
        jr_record(c, s, tab);
        return jr_table(c, tab, s, depth + 1);
    }
    if d.shape == ShapeSBlob || d.shape == ShapeBBlob || d.shape == ShapeStr || d.shape == ShapeText || d.shape == ShapeBytes {
        return jr_field(c, s, d, owner, ord, depth);
    }
    if d.shape == ShapeEnum {
        var v: u64 = 0;
        if !json_read_enum(c.jr, v, d) { return false; }
        if v != 0 && jv_deprecated(c.unit, d.ref, d.variants[(v - 1) as! i64].id) {
            // a deprecated variant reads as the declared default, never unknown
            v = if !element && d.has_def { d.def_i.lo } else { 0 };
        }
        jr_place(c, s, Int128 { lo: v, hi: 0 });
        return true;
    }
    let fi = jv_fact(c.text, owner, ord);
    if fi >= 0 && c.text.facts[fi].is_flags {
        var bits: u64 = 0;
        if !json_read_flags(c.jr, bits, c.text.facts[fi].flags, depth) { return false; }
        jr_place(c, s, Int128 { lo: bits, hi: 0 });
        return true;
    }
    if d.shape == ShapeBool {
        var b = false;
        if !json_read_bool(c.jr, b) { return false; }
        jr_place(c, s, Int128 { lo: if b { 1 } else { 0 }, hi: 0 });
        return true;
    }
    let mismatch = c.jr.rep.kind_mismatch;
    if d.shape == ShapeFloat {
        if d.is_f32 {
            var x: f32 = 0.0;
            if !json_read_float(c.jr, x, d) { return false; }
            if c.jr.rep.kind_mismatch == mismatch { jr_place(c, s, Int128 { lo: fixed_f32_bits(x), hi: 0 }); }
            return true;
        }
        var y: f64 = 0.0;
        if !json_read_float(c.jr, y, d) { return false; }
        if c.jr.rep.kind_mismatch == mismatch { jr_place(c, s, Int128 { lo: fixed_f64_bits(y), hi: 0 }); }
        return true;
    }
    if jv_wide(d) {
        let start, end, whole, ok = json_number(c.jr);
        if !ok { return false; }
        let v, exact, sat = jv_parse_wide(c.jr.text[start..end], d.signed, d.fbits);
        if !exact {
            c.jr.rep.kind_mismatch += 1;
            return true;
        }
        if sat { c.jr.rep.clamped += 1; }
        var x = v;
        if d.ranged {
            let lo, hi = jv_raw_range(d);
            if cmp128(x, d.signed, lo, d.signed) < 0 {
                x = lo;
                c.jr.rep.clamped += 1;
            } else if cmp128(x, d.signed, hi, d.signed) > 0 {
                x = hi;
                c.jr.rep.clamped += 1;
            }
        }
        jr_place(c, s, x);
        return true;
    }
    if d.signed {
        var v: i64 = 0;
        if !json_read_int(c.jr, v, d) { return false; }
        if c.jr.rep.kind_mismatch == mismatch { jr_place(c, s, Int128 { lo: v as! u64, hi: if v < 0 { 0xFFFFFFFFFFFFFFFF } else { 0 } }); }
        return true;
    }
    var u: u64 = 0;
    if !json_read_int(c.jr, u, d) { return false; }
    if c.jr.rep.kind_mismatch == mismatch { jr_place(c, s, Int128 { lo: u, hi: 0 }); }
    return true;
}

// jr_union places a union (readUnion): an object with ONE key, the arm's
// name; `{}` is None, an unknown arm counted unknown, an arm value of the
// wrong shape kind_mismatch with the union None, and two keys malformed.
recursive fn jr_union(c: JvIn&, s: i64, name: const u8[:], depth: i64) -> bool {
    if !json_expect(c.jr, '{') { return false; }
    let u = jv_union(c.unit, name);
    if u < 0 { return json_fail(c.jr); }
    c.vals[s] = LoadVal { shape: ShapeUnion, state: ValEmpty, tab: u, first: -1, .. };
    if json_peek(c.jr) == '}' {
        c.jr.pos += 1;
        return true;
    }
    var key: u8[256] = [0; 256];
    let n, ok = jr_key(c, key);
    if !ok { return false; }
    var tag: i64 = 0;
    let arms = c.unit.unions[u].arms;
    for a, i in arms {
        if a.name == key[..n] {
            tag = i + 1;
            break;
        }
    }
    let got = json_shape(c.jr);
    if tag == 0 {
        c.jr.rep.unknown += 1;
        if !jr_skip(c, depth + 1) { return false; }
    } else {
        let arm = arms[tag - 1];
        if arm.shape == ShapeVoid {
            if got != 'z' {
                c.jr.rep.kind_mismatch += 1;
                if !jr_skip(c, depth + 1) { return false; }
            } else {
                if !json_literal(c.jr, "null") { return false; }
                jr_arm(c, s, tag, -1);
            }
        } else if arm.shape == ShapeTab {
            let at = jv_table(c.unit, arm.ref);
            if at < 0 { return json_fail(c.jr); }
            let want: u8 = if c.unit.tables[at].json_array { 'a' } else { 'o' };
            if got != want {
                c.jr.rep.kind_mismatch += 1;
                if !jr_skip(c, depth + 1) { return false; }
            } else {
                let p = jr_new_record(c, at);
                if !jr_table(c, at, p, depth + 1) { return false; }
                jr_arm(c, s, tag, p);
            }
        } else {
            let p = jr_reserve(c, 1);
            c.vals[p].shape = arm.shape;
            let uname = c.unit.unions[u].name;
            if got == 'z' && jr_takes_null(arm) {
                if !json_literal(c.jr, "null") { return false; }
                jr_arm(c, s, tag, p);
            } else if got != jr_shape(c, arm, uname, tag as! i32, false) {
                c.jr.rep.kind_mismatch += 1;
                if !jr_skip(c, depth + 1) { return false; }
            } else {
                if !jr_field(c, p, arm, uname, tag as! i32, depth + 1) { return false; }
                jr_arm(c, s, tag, p);
            }
        }
    }
    var ch = json_peek(c.jr);
    if ch == ',' {
        c.jr.pos += 1;
        ch = json_peek(c.jr);
    }
    if ch == '}' {
        c.jr.pos += 1;
        return true;
    }
    return json_fail(c.jr);
}

fn jr_arm(c: JvIn&, s: i64, tag: i64, p: i64) {
    c.vals[s].state = ValPresent;
    c.vals[s].bits = Int128 { lo: tag as! u64, hi: 0 };
    c.vals[s].made = p >= 0;
    c.vals[s].first = p;
    c.vals[s].count = if p >= 0 { 1 } else { 0 };
}

// json_load_table reads one text as root `root` into the flat value table,
// tolerantly (§16.1): the root's slot, or -1 — the text is not JSON, a value
// is refused by name (report.malformed, report.reason), or the root has no
// text form. The caller discards what a refused read placed (§4).
fn json_load_table(unit: LoadUnit, text_unit: JsonUnit, root: const u8[:], text: const u8[:], data: u8[>..]&, vals: LoadVal[>..]&, nodes: LoadNode[>..]&, report: JsonReport&) -> i64 {
    report = json_report();
    let t = jv_table(unit, root);
    if t < 0 || !jv_has_text(text_unit, root) {
        report.malformed = true;
        return -1;
    }
    var jr = json_in(text);
    var labels: JsonLabel[>..] = [];
    var scratch: i64[>..<] = [];
    var c = JvIn { jr: jr, unit: unit, text: text_unit, data: data, vals: vals, nodes: nodes, labels: labels, scratch: scratch, graph: unit.tables[t].variable };
    let rec = jr_new_record(c, t);
    let ok = jr_table(c, t, rec, 0);
    if !json_finish(jr, ok, report) { return -1; }
    return rec;
}
