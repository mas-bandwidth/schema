// json.goose — the JSON text form's runtime (docs/SPEC-TABLES.md §16;
// docs/PORT-GOOSE.md, THE RUNTIME LAYOUT, card schema-goose-json-form): the
// ONE text walk every unit shares, byte for byte (PORTING M9). It reads the
// descriptors (schema_tables.goose) for every decision — keys, bounds,
// ranges, variant names — and reaches a record's storage through the
// per-table accessor json_form.go emits, the only per-table code the form
// writes. Numbers convert EXACTLY in both directions (a port of the
// arbitrary-precision decimal Go's strconv falls back on): a float writes as
// C's %.*g at the shortest precision that reads back, a token reads to the
// nearest float of the field's own width with ONE rounding.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_tables;
import .fixed;

let JsonDecDigits: i64 = 800;

struct JsonDec {
    d: u8[800],
    nd: i64,
    dp: i64,
    neg: bool,
    trunc: bool,
}

fn json_dec_trim(a: JsonDec&) {
    while a.nd > 0 && a.d[a.nd - 1] == '0' { a.nd -= 1; }
    if a.nd == 0 { a.dp = 0; }
}

fn json_dec_assign(a: JsonDec&, v0: u64) {
    var buf: u8[24] = [0; 24];
    var n: i64 = 0;
    var v = v0;
    while v > 0 {
        let v1 = v / 10;
        buf[n] = ((v - 10 * v1) as! u8) + '0';
        n += 1;
        v = v1;
    }
    a.nd = 0;
    n -= 1;
    while n >= 0 {
        a.d[a.nd] = buf[n];
        a.nd += 1;
        n -= 1;
    }
    a.dp = a.nd;
    json_dec_trim(a);
}

fn json_dec_right(a: JsonDec&, k: u64) {
    var r: i64 = 0;
    var w: i64 = 0;
    var n: u64 = 0;
    while (n >> k) == 0 {
        if r >= a.nd {
            if n == 0 {
                a.nd = 0;
                return;
            }
            while (n >> k) == 0 {
                n = n * 10;
                r += 1;
            }
            break;
        }
        n = n * 10 + (a.d[r] - '0');
        r += 1;
    }
    a.dp -= r - 1;
    let mask: u64 = ((1 as u64) << k) - 1;
    while r < a.nd {
        let c = (a.d[r] - '0') as u64;
        let dig = n >> k;
        n &= mask;
        a.d[w] = (dig as! u8) + '0';
        w += 1;
        n = n * 10 + c;
        r += 1;
    }
    while n > 0 {
        let dig = n >> k;
        n &= mask;
        if w < JsonDecDigits {
            a.d[w] = (dig as! u8) + '0';
            w += 1;
        } else if dig > 0 {
            a.trunc = true;
        }
        n = n * 10;
    }
    a.nd = w;
    json_dec_trim(a);
}

// json_dec_left multiplies by 2^k: the product's digits are produced from the
// low end into a scratch run, then laid back in order.
fn json_dec_left(a: JsonDec&, k: u64) {
    var tmp: u8[840] = [0; 840];
    var t: i64 = 0;
    var r = a.nd - 1;
    var n: u64 = 0;
    while r >= 0 {
        n += ((a.d[r] - '0') as u64) << k;
        let quo = n / 10;
        tmp[t] = ((n - 10 * quo) as! u8) + '0';
        t += 1;
        n = quo;
        r -= 1;
    }
    while n > 0 {
        let quo = n / 10;
        tmp[t] = ((n - 10 * quo) as! u8) + '0';
        t += 1;
        n = quo;
    }
    let delta = t - a.nd;
    var w: i64 = 0;
    while t > 0 {
        t -= 1;
        if w < JsonDecDigits {
            a.d[w] = tmp[t];
            w += 1;
        } else if tmp[t] != '0' {
            a.trunc = true;
        }
    }
    a.nd = w;
    a.dp += delta;
    json_dec_trim(a);
}

fn json_dec_shift(a: JsonDec&, k0: i64) {
    if a.nd == 0 { return; }
    var k = k0;
    if k > 0 {
        while k > 60 {
            json_dec_left(a, 60);
            k -= 60;
        }
        json_dec_left(a, k as! u64);
    } else if k < 0 {
        while k < -60 {
            json_dec_right(a, 60);
            k += 60;
        }
        json_dec_right(a, (-k) as! u64);
    }
}

fn json_dec_round_up_at(a: const JsonDec&, nd: i64) -> bool {
    if nd < 0 || nd >= a.nd { return false; }
    if a.d[nd] == '5' && nd + 1 == a.nd {
        if a.trunc { return true; }
        return nd > 0 && ((a.d[nd - 1] - '0') % 2) == 1;
    }
    return a.d[nd] >= '5';
}

fn json_dec_round(a: JsonDec&, nd: i64) {
    if nd < 0 || nd >= a.nd { return; }
    if json_dec_round_up_at(a, nd) {
        var i = nd - 1;
        while i >= 0 {
            if a.d[i] < '9' {
                a.d[i] += 1;
                a.nd = i + 1;
                return;
            }
            i -= 1;
        }
        a.d[0] = '1';
        a.nd = 1;
        a.dp += 1;
    } else {
        a.nd = nd;
        json_dec_trim(a);
    }
}

fn json_dec_rounded_integer(a: const JsonDec&) -> u64 {
    if a.dp > 20 { return 0xFFFFFFFFFFFFFFFF; }
    var i: i64 = 0;
    var n: u64 = 0;
    while i < a.dp && i < a.nd {
        n = n * 10 + (a.d[i] - '0');
        i += 1;
    }
    while i < a.dp {
        n *= 10;
        i += 1;
    }
    if json_dec_round_up_at(a, a.dp) { n += 1; }
    return n;
}

// json_dec_set reads a JSON number token (the RFC 8259 production, already
// scanned) into a.
fn json_dec_set(a: JsonDec&, s: const u8[:]) {
    a.nd = 0;
    a.dp = 0;
    a.neg = false;
    a.trunc = false;
    var i: i64 = 0;
    if i < s.len && s[i] == '-' {
        a.neg = true;
        i += 1;
    }
    var sawdot = false;
    while i < s.len {
        let c = s[i];
        if c == '.' {
            sawdot = true;
            a.dp = a.nd;
        } else if c >= '0' && c <= '9' {
            if c == '0' && a.nd == 0 {
                a.dp -= 1;
            } else if a.nd < JsonDecDigits {
                a.d[a.nd] = c;
                a.nd += 1;
            } else if c != '0' {
                a.trunc = true;
            }
        } else {
            break;
        }
        i += 1;
    }
    if !sawdot { a.dp = a.nd; }
    if i < s.len && (s[i] == 'e' || s[i] == 'E') {
        i += 1;
        var esign: i64 = 1;
        if i < s.len && s[i] == '+' {
            i += 1;
        } else if i < s.len && s[i] == '-' {
            i += 1;
            esign = -1;
        }
        var e: i64 = 0;
        while i < s.len && s[i] >= '0' && s[i] <= '9' {
            if e < 10000 { e = e * 10 + (s[i] - '0'); }
            i += 1;
        }
        a.dp += e * esign;
    }
    if a.nd == 0 { a.dp = 0; }
}

fn json_dec_powtab(n: i64) -> i64 {
    if n >= 9 { return 27; }
    match n {
        0 => 1, 1 => 3, 2 => 6, 3 => 9, 4 => 13, 5 => 16, 6 => 19, 7 => 23, _ => 26,
    }
}

// json_dec_bits is the nearest float's bit pattern to a, rounded once, half to
// even, at mantbits/expbits/bias (52/11/-1023 for f64, 23/8/-127 for f32); the
// bool is true when the magnitude overflowed to an infinity. a is consumed.
fn json_dec_bits(a: JsonDec&, mantbits: u64, expbits: u64, bias: i64) -> u64, bool {
    var exp: i64 = 0;
    var mant: u64 = 0;
    var overflow = false;
    let expmax: i64 = ((1 as i64) << (expbits as! i64)) - 1;
    if a.nd == 0 {
        exp = bias;
    } else if a.dp > 310 {
        overflow = true;
    } else if a.dp < -330 {
        exp = bias;
    } else {
        while a.dp > 0 {
            let n = json_dec_powtab(a.dp);
            json_dec_shift(a, -n);
            exp += n;
        }
        while a.dp < 0 || (a.dp == 0 && a.d[0] < '5') {
            let n = json_dec_powtab(-a.dp);
            json_dec_shift(a, n);
            exp -= n;
        }
        exp -= 1;
        if exp < bias + 1 {
            let n = bias + 1 - exp;
            json_dec_shift(a, -n);
            exp += n;
        }
        if exp - bias >= expmax {
            overflow = true;
        } else {
            json_dec_shift(a, 1 + (mantbits as! i64));
            mant = json_dec_rounded_integer(a);
            if mant == ((2 as u64) << mantbits) {
                mant >>= 1;
                exp += 1;
                if exp - bias >= expmax { overflow = true; }
            }
            if !overflow && (mant & ((1 as u64) << mantbits)) == 0 { exp = bias; }
        }
    }
    if overflow {
        mant = 0;
        exp = expmax + bias;
    }
    var bits: u64 = mant & (((1 as u64) << mantbits) - 1);
    bits |= (((exp - bias) & expmax) as! u64) << mantbits;
    if a.neg { bits |= ((1 as u64) << mantbits) << expbits; }
    return bits, overflow;
}

// json_dec_of_bits is the exact decimal value of a finite float's bits.
fn json_dec_of_bits(a: JsonDec&, bits: u64, mantbits: u64, expbits: u64, bias: i64) {
    let expmask: u64 = ((1 as u64) << expbits) - 1;
    var exp: i64 = ((bits >> mantbits) & expmask) as! i64;
    var mant: u64 = bits & (((1 as u64) << mantbits) - 1);
    if exp == 0 {
        exp += 1;
    } else {
        mant |= (1 as u64) << mantbits;
    }
    exp += bias;
    a.nd = 0;
    a.dp = 0;
    a.trunc = false;
    a.neg = ((bits >> mantbits) >> expbits) != 0;
    json_dec_assign(a, mant);
    json_dec_shift(a, exp - (mantbits as! i64));
}

fn json_put_exp(out: u8[>..]&, e0: i64) {
    var e = e0;
    if e < 0 {
        out.push('-');
        e = -e;
    } else {
        out.push('+');
    }
    if e < 10 {
        out.push('0');
        out.push((e as! u8) + '0');
    } else if e < 100 {
        out.push(((e / 10) as! u8) + '0');
        out.push(((e % 10) as! u8) + '0');
    } else {
        out.push(((e / 100) as! u8) + '0');
        out.push((((e / 10) % 10) as! u8) + '0');
        out.push(((e % 10) as! u8) + '0');
    }
}

// json_put_g appends C's %.{prec}g of digs, which is already rounded to prec
// significant digits (so it carries no trailing zero).
fn json_put_g(out: u8[>..]&, digs: const JsonDec&, prec0: i64) {
    var prec = prec0;
    if digs.neg { out.push('-'); }
    var eprec = prec;
    if eprec > digs.nd && digs.nd >= digs.dp { eprec = digs.nd; }
    let exp = digs.dp - 1;
    if exp < -4 || exp >= eprec {
        if prec > digs.nd { prec = digs.nd; }
        let decimals = prec - 1;
        if digs.nd != 0 { out.push(digs.d[0]); } else { out.push('0'); }
        if decimals > 0 {
            out.push('.');
            var i: i64 = 1;
            while i <= decimals {
                if i < digs.nd { out.push(digs.d[i]); } else { out.push('0'); }
                i += 1;
            }
        }
        out.push('e');
        if digs.nd == 0 { json_put_exp(out, 0); } else { json_put_exp(out, exp); }
        return;
    }
    if prec > digs.dp { prec = digs.nd; }
    var decimals = prec - digs.dp;
    if decimals < 0 { decimals = 0; }
    if digs.dp > 0 {
        var m: i64 = 0;
        while m < digs.dp {
            if m < digs.nd { out.push(digs.d[m]); } else { out.push('0'); }
            m += 1;
        }
    } else {
        out.push('0');
    }
    if decimals > 0 {
        out.push('.');
        var i: i64 = 1;
        while i <= decimals {
            let j = digs.dp + i - 1;
            if j >= 0 && j < digs.nd { out.push(digs.d[j]); } else { out.push('0'); }
            i += 1;
        }
    }
}

// json_put_float writes a finite float at the SHORTEST %.*g precision that
// reads back as the same value at the field's own width (6..9 significant
// digits for a float32, 15..17 for a float64); a non-finite value has no JSON
// spelling and is refused (docs/SPEC-TABLES.md §16.1, §16.3).
fn json_put_float(out: u8[>..]&, v: f64, single: bool) -> bool {
    var bits: u64 = 0;
    var mantbits: u64 = 52;
    var expbits: u64 = 11;
    var bias: i64 = -1023;
    var low: i64 = 15;
    var high: i64 = 17;
    if single {
        bits = fixed_f32_bits(v as f32);
        mantbits = 23;
        expbits = 8;
        bias = -127;
        low = 6;
        high = 9;
    } else {
        bits = fixed_f64_bits(v);
    }
    let expmask: u64 = ((1 as u64) << expbits) - 1;
    if ((bits >> mantbits) & expmask) == expmask { return false; }
    var exact = JsonDec { d: [0; 800], nd: 0, dp: 0, neg: false, trunc: false };
    json_dec_of_bits(exact, bits, mantbits, expbits, bias);
    var p = low;
    loop {
        var t = exact;
        json_dec_round(t, p);
        if p >= high {
            json_put_g(out, t, p);
            return true;
        }
        var back = t;
        let b, over = json_dec_bits(back, mantbits, expbits, bias);
        if !over && b == bits {
            json_put_g(out, t, p);
            return true;
        }
        p += 1;
    }
}

// json_token_f64 is a number token's nearest double; false when its magnitude
// is past every finite double.
fn json_token_f64(tok: const u8[:]) -> f64, bool {
    var a = JsonDec { d: [0; 800], nd: 0, dp: 0, neg: false, trunc: false };
    json_dec_set(a, tok);
    let bits, over = json_dec_bits(a, 52, 11, -1023);
    if over { return 0.0, false; }
    return fixed_bits_f64(bits), true;
}

// json_token_f32 is a number token's nearest float32, rounded ONCE from the
// exact decimal (never through a double, docs/SPEC-TABLES.md §16.1:
// num-float32-upper-band); false past every finite float32.
fn json_token_f32(tok: const u8[:]) -> f32, bool {
    var a = JsonDec { d: [0; 800], nd: 0, dp: 0, neg: false, trunc: false };
    json_dec_set(a, tok);
    let bits, over = json_dec_bits(a, 23, 8, -127);
    if over { return 0.0, false; }
    return fixed_bits_f32(bits as! u32), true;
}

// ---- json walk: begin ----

let JsonMaxDepth: i64 = 128;
let JsonMaxKey: i64 = 255;

// The accessor's operations (docs/PORT-GOOSE.md, How a walk reaches a field):
// a read places the value, ABSENT puts an optional back at its default and
// absent, PRESENT marks it present; a write asks WHETHER the key is written,
// then writes its value.
let JsonOpRead: i64 = 0;
let JsonOpAbsent: i64 = 1;
let JsonOpPresent: i64 = 2;
let JsonOpTag: i64 = 3;
let JsonOpAsk: i64 = 0;
let JsonOpWrite: i64 = 1;

// The reasons a refused read names (docs/SPEC-TABLES.md §16.3).
let JsonReasonNone: i64 = 0;
let JsonReasonNotUtf8: i64 = 1;
let JsonReasonHoldsZero: i64 = 2;
let JsonReasonReservedKey: i64 = 3;

// JsonReport is a text read's report (docs/SPEC-TABLES.md §4, §16.2):
// widened stays zero in this form, and reason names a refusal.
struct JsonReport {
    unknown: i64,
    kind_mismatch: i64,
    widened: i64,
    clamped: i64,
    duplicate: i64,
    malformed: bool,
    reason: i64,
}

fn json_report() -> JsonReport {
    return JsonReport { unknown: 0, kind_mismatch: 0, widened: 0, clamped: 0, duplicate: 0, malformed: false, reason: 0 };
}

// JsonIn is the reader's cursor over the caller's text: the read copies
// nothing out of it but the values it places.
struct JsonIn {
    text: const u8[:],
    pos: i64,
    bad: bool,
    zero: bool,
    rep: JsonReport,
}

fn json_in(text: const u8[:]) -> JsonIn {
    var jr = JsonIn { text: text, pos: 0, bad: false, zero: false, rep: json_report() };
    // a leading UTF-8 byte-order mark is skipped, there and nowhere else
    // (docs/SPEC-TABLES.md §16.2, §16.12)
    if text.len >= 3 && text[0] == 0xEF && text[1] == 0xBB && text[2] == 0xBF { jr.pos = 3; }
    return jr;
}

fn json_fail(jr: JsonIn&) -> bool {
    jr.bad = true;
    return false;
}

// json_space skips whitespace and comments: `//` to the end of the line or of
// the input, `/* */` to its close; an unclosed one is malformed (§16.2).
fn json_space(jr: JsonIn&) {
    let t = jr.text;
    while jr.pos < t.len {
        let c = t[jr.pos];
        if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
            jr.pos += 1;
            continue;
        }
        if c == '/' && jr.pos + 1 < t.len {
            if t[jr.pos + 1] == '/' {
                jr.pos += 2;
                while jr.pos < t.len && t[jr.pos] != '\n' { jr.pos += 1; }
                continue;
            }
            if t[jr.pos + 1] == '*' {
                jr.pos += 2;
                while jr.pos + 1 < t.len && !(t[jr.pos] == '*' && t[jr.pos + 1] == '/') { jr.pos += 1; }
                if jr.pos + 1 >= t.len {
                    jr.bad = true;
                    return;
                }
                jr.pos += 2;
                continue;
            }
        }
        return;
    }
}

fn json_peek(jr: JsonIn&) -> u8 {
    json_space(jr);
    if jr.pos < jr.text.len { return jr.text[jr.pos]; }
    return 0;
}

// json_shape is the JSON type at the cursor, unconsumed: o a s b z (null) n.
fn json_shape(jr: JsonIn&) -> u8 {
    let c = json_peek(jr);
    if c == '{' { return 'o'; }
    if c == '[' { return 'a'; }
    if c == '"' { return 's'; }
    if c == 't' || c == 'f' { return 'b'; }
    if c == 'n' { return 'z'; }
    if c == 0 { return 0; }
    return 'n';
}

fn json_literal(jr: JsonIn&, word: const u8[:]) -> bool {
    if jr.pos + word.len > jr.text.len || jr.text[jr.pos..jr.pos + word.len] != word { return json_fail(jr); }
    jr.pos += word.len;
    return true;
}

fn json_expect(jr: JsonIn&, c: u8) -> bool {
    if json_peek(jr) != c { return json_fail(jr); }
    jr.pos += 1;
    return true;
}

// json_hex4 is one \uXXXX body, or -1.
fn json_hex4(jr: JsonIn&) -> i64 {
    if jr.pos + 4 > jr.text.len { return -1; }
    var v: i64 = 0;
    var i: i64 = 0;
    while i < 4 {
        let c = jr.text[jr.pos + i];
        var d: i64 = 0;
        if c >= '0' && c <= '9' {
            d = c - '0';
        } else if c >= 'a' && c <= 'f' {
            d = (c - 'a') as i64 + 10;
        } else if c >= 'A' && c <= 'F' {
            d = (c - 'A') as i64 + 10;
        } else {
            return -1;
        }
        v = v * 16 + d;
        i += 1;
    }
    jr.pos += 4;
    return v;
}

// json_utf8 is the UTF-8 sequence at s[at]: its code point and width, or a
// negative width whose magnitude is one maximal subpart (Unicode D93b;
// docs/SPEC-TABLES.md §16.3) when the bytes there are ill-formed.
fn json_utf8(s: const u8[:], at: i64) -> i64, i64 {
    let lead = s[at];
    if lead < 0x80 { return lead, 1; }
    var want: i64 = 0;
    var code: i64 = 0;
    var lo: u8 = 0x80;
    var hi: u8 = 0xBF;
    if lead >= 0xC2 && lead <= 0xDF {
        want = 2;
        code = lead & 0x1F;
    } else if lead >= 0xE0 && lead <= 0xEF {
        want = 3;
        code = lead & 0x0F;
        if lead == 0xE0 { lo = 0xA0; }
        if lead == 0xED { hi = 0x9F; }
    } else if lead >= 0xF0 && lead <= 0xF4 {
        want = 4;
        code = lead & 0x07;
        if lead == 0xF0 { lo = 0x90; }
        if lead == 0xF4 { hi = 0x8F; }
    } else {
        return -1, -1;
    }
    var i: i64 = 1;
    while i < want {
        if at + i >= s.len || s[at + i] < lo || s[at + i] > hi { return -1, -i; }
        code = code * 64 + (s[at + i] & 0x3F);
        lo = 0x80;
        hi = 0xBF;
        i += 1;
    }
    return code, want;
}

fn json_encode_utf8(code: i64, unit: u8[4]&) -> i64 {
    if code < 0x80 {
        unit[0] = code as! u8;
        return 1;
    }
    if code < 0x800 {
        unit[0] = (0xC0 | (code >> 6)) as! u8;
        unit[1] = (0x80 | (code & 0x3F)) as! u8;
        return 2;
    }
    if code < 0x10000 {
        unit[0] = (0xE0 | (code >> 12)) as! u8;
        unit[1] = (0x80 | ((code >> 6) & 0x3F)) as! u8;
        unit[2] = (0x80 | (code & 0x3F)) as! u8;
        return 3;
    }
    unit[0] = (0xF0 | (code >> 18)) as! u8;
    unit[1] = (0x80 | ((code >> 12) & 0x3F)) as! u8;
    unit[2] = (0x80 | ((code >> 6) & 0x3F)) as! u8;
    unit[3] = (0x80 | (code & 0x3F)) as! u8;
    return 4;
}

// json_scan decodes one JSON string and hands it to F ONE CODE POINT AT A
// TIME (the code point's UTF-8 bytes and their count), so a sink that clamps
// cuts at a code point boundary. A lone surrogate escape reads as U+FFFD; a
// byte sequence that is not UTF-8 refuses the read by name, and so does a
// U+0000 bound for text storage (store), docs/SPEC-TABLES.md §16.3.
fn json_scan<F>(jr: JsonIn&, store: bool) -> bool {
    if json_peek(jr) != '"' { return json_fail(jr); }
    jr.pos += 1;
    let t = jr.text;
    var unit: u8[4] = [0; 4];
    loop {
        if jr.pos >= t.len { return json_fail(jr); }
        let c = t[jr.pos];
        if c == '"' {
            jr.pos += 1;
            return true;
        }
        var n: i64 = 0;
        if c == '\\' {
            jr.pos += 1;
            if jr.pos >= t.len { return json_fail(jr); }
            let e = t[jr.pos];
            jr.pos += 1;
            n = 1;
            if e == '"' || e == '\\' || e == '/' {
                unit[0] = e;
            } else if e == 'b' {
                unit[0] = 8;
            } else if e == 'f' {
                unit[0] = 12;
            } else if e == 'n' {
                unit[0] = 10;
            } else if e == 'r' {
                unit[0] = 13;
            } else if e == 't' {
                unit[0] = 9;
            } else if e == 'u' {
                let high = json_hex4(jr);
                if high < 0 { return json_fail(jr); }
                var code = high;
                if high >= 0xD800 && high <= 0xDBFF && jr.pos + 2 <= t.len && t[jr.pos] == '\\' && t[jr.pos + 1] == 'u' {
                    let mark = jr.pos;
                    jr.pos += 2;
                    let low = json_hex4(jr);
                    if low >= 0xDC00 && low <= 0xDFFF {
                        code = 0x10000 + (high - 0xD800) * 1024 + (low - 0xDC00);
                    } else {
                        jr.pos = mark;
                    }
                }
                if code >= 0xD800 && code <= 0xDFFF { code = 0xFFFD; }
                if code == 0 && store {
                    jr.zero = true;
                    return json_fail(jr);
                }
                n = json_encode_utf8(code, unit);
            } else {
                return json_fail(jr);
            }
        } else if c < 0x20 {
            return json_fail(jr);
        } else if c < 0x80 {
            unit[0] = c;
            n = 1;
            jr.pos += 1;
        } else {
            let code, w = json_utf8(t, jr.pos);
            if w < 0 {
                if jr.rep.reason == 0 { jr.rep.reason = JsonReasonNotUtf8; }
                return json_fail(jr);
            }
            var k: i64 = 0;
            while k < w {
                unit[k] = t[jr.pos + k];
                k += 1;
            }
            n = w;
            jr.pos += w;
        }
        F(unit, n);
    }
}

// json_scan_into scans a string into dst, clamped at a code point boundary
// and counted when it does not fit; it answers the bytes placed.
fn json_scan_into(jr: JsonIn&, dst: u8[:], store: bool) -> i64, bool {
    var placed: i64 = 0;
    var clamped = false;
    let ok = json_scan(jr, store) { unit, n =>
        if !clamped && placed + n <= dst.len {
            var k: i64 = 0;
            while k < n {
                dst[placed + k] = unit[k];
                k += 1;
            }
            placed += n;
        } else {
            clamped = true;
        }
    };
    if !ok { return 0, false; }
    if clamped { jr.rep.clamped += 1; }
    return placed, true;
}

// json_skip_string scans past a string and keeps nothing.
fn json_skip_string(jr: JsonIn&) -> bool {
    return json_scan(jr, false) { unit, n => };
}

// json_key scans an object key or a variant name into a key buffer: a name is
// compared and never stored (§16.3).
fn json_key(jr: JsonIn&, key: u8[256]&) -> i64, bool {
    return json_scan_into(jr, key[..JsonMaxKey], false);
}

fn json_name_is(name: const u8[:], key: const u8[:]) -> bool {
    return name == key;
}

// json_walk_number scans one number to RFC 8259's own production, so `1-2`,
// `+1`, `01`, `.5` and `3.` are malformed rather than values (§16.2).
fn json_walk_number(jr: JsonIn&) -> bool, bool {
    json_space(jr);
    let t = jr.text;
    var whole = true;
    if jr.pos < t.len && t[jr.pos] == '-' { jr.pos += 1; }
    if jr.pos >= t.len { return false, false; }
    if t[jr.pos] == '0' {
        jr.pos += 1;
    } else if t[jr.pos] >= '1' && t[jr.pos] <= '9' {
        while jr.pos < t.len && t[jr.pos] >= '0' && t[jr.pos] <= '9' { jr.pos += 1; }
    } else {
        return false, false;
    }
    if jr.pos < t.len && t[jr.pos] == '.' {
        jr.pos += 1;
        if jr.pos >= t.len || t[jr.pos] < '0' || t[jr.pos] > '9' { return false, false; }
        while jr.pos < t.len && t[jr.pos] >= '0' && t[jr.pos] <= '9' { jr.pos += 1; }
        whole = false;
    }
    if jr.pos < t.len && (t[jr.pos] == 'e' || t[jr.pos] == 'E') {
        jr.pos += 1;
        if jr.pos < t.len && (t[jr.pos] == '-' || t[jr.pos] == '+') { jr.pos += 1; }
        if jr.pos >= t.len || t[jr.pos] < '0' || t[jr.pos] > '9' { return false, false; }
        while jr.pos < t.len && t[jr.pos] >= '0' && t[jr.pos] <= '9' { jr.pos += 1; }
        whole = false;
    }
    return whole, true;
}

// json_number scans a number token: its extent, and whether its spelling is
// integral (no fraction, no exponent).
fn json_number(jr: JsonIn&) -> i64, i64, bool, bool {
    json_space(jr);
    let start = jr.pos;
    let whole, ok = json_walk_number(jr);
    if !ok || jr.pos <= start {
        jr.bad = true;
        return 0, 0, false, false;
    }
    return start, jr.pos, whole, true;
}

// json_skip_value consumes one value of any shape, to the depth cap.
recursive fn json_skip_value(jr: JsonIn&, depth: i64) -> bool {
    let c = json_peek(jr);
    if c == '{' || c == '[' {
        if depth > JsonMaxDepth { return json_fail(jr); }
        let closing: u8 = if c == '{' { '}' } else { ']' };
        jr.pos += 1;
        loop {
            let d = json_peek(jr);
            if d == closing {
                jr.pos += 1;
                return true;
            }
            if d == 0 { return json_fail(jr); }
            if closing == '}' {
                if !json_skip_string(jr) { return false; }
                if !json_expect(jr, ':') { return false; }
            }
            if !json_skip_value(jr, depth + 1) { return false; }
            let e = json_peek(jr);
            if e == ',' {
                jr.pos += 1;
                continue;
            }
            if e == closing {
                jr.pos += 1;
                return true;
            }
            return json_fail(jr);
        }
    }
    if c == '"' { return json_skip_string(jr); }
    if c == 't' { return json_literal(jr, "true"); }
    if c == 'f' { return json_literal(jr, "false"); }
    if c == 'n' { return json_literal(jr, "null"); }
    if c == 0 { return json_fail(jr); }
    let whole, ok = json_walk_number(jr);
    if !ok { return json_fail(jr); }
    return true;
}

// json_after_value takes the separator after an element or a member: true to
// go on, false at the close (consumed); a malformed separator sets bad.
fn json_after_value(jr: JsonIn&, closing: u8) -> bool {
    let c = json_peek(jr);
    if c == ',' {
        jr.pos += 1;
        return true;
    }
    if c != closing { jr.bad = true; }
    jr.pos += 1;
    return false;
}

// ---- numbers into integer storage ----

// json_token_int is an integral token's exact value, digit by digit: the
// magnitude saturates and says so, the wire's rule for a value past what the
// reader holds (§4).
fn json_token_int(tok: const u8[:]) -> bool, u64, bool {
    var i: i64 = 0;
    var neg = false;
    if i < tok.len && tok[i] == '-' {
        neg = true;
        i += 1;
    }
    var mag: u64 = 0;
    var over = false;
    while i < tok.len {
        let d = (tok[i] - '0') as u64;
        if mag > (0xFFFFFFFFFFFFFFFF - d) / 10 {
            over = true;
            break;
        }
        mag = mag * 10 + d;
        i += 1;
    }
    return neg, mag, over;
}

// json_read_int places a number in an integer field: any token whose VALUE
// is integral (2.0 and 1e3 are integers), a fraction is kind_mismatch, and a
// value past the field's domain, its declared range or bits(N), and its
// storage width clamps and counts at each (docs/SPEC-TABLES.md §16.2).
fn json_read_int<T>(jr: JsonIn&, slot: T&, f: const FieldDesc&) -> bool {
    let start, end, whole, ok = json_number(jr);
    if !ok { return false; }
    let tok = jr.text[start..end];
    var sat = false;
    if f.signed {
        var v: i64 = 0;
        if whole {
            let neg, mag, over = json_token_int(tok);
            if neg {
                if over || mag > 0x8000000000000000 {
                    v = -9223372036854775807 - 1;
                    sat = true;
                } else {
                    v = (0 - mag) as! i64;
                }
            } else if over || mag > 0x7FFFFFFFFFFFFFFF {
                v = 9223372036854775807;
                sat = true;
            } else {
                v = mag as! i64;
            }
        } else {
            let d, fin = json_token_f64(tok);
            if !fin {
                jr.rep.kind_mismatch += 1;
                return true;
            }
            if d >= 9223372036854775808.0 {
                v = 9223372036854775807;
                sat = true;
            } else if d < -9223372036854775808.0 {
                v = -9223372036854775807 - 1;
                sat = true;
            } else if d != ((d as! i64) as f64) {
                jr.rep.kind_mismatch += 1;
                return true;
            } else {
                v = d as! i64;
            }
        }
        if sat { jr.rep.clamped += 1; }
        if f.ranged {
            let lo = f.lo.lo as! i64;
            let hi = f.hi.lo as! i64;
            if v < lo {
                v = lo;
                jr.rep.clamped += 1;
            } else if v > hi {
                v = hi;
                jr.rep.clamped += 1;
            }
        }
        if f.kw < 8 {
            let high: i64 = ((1 as i64) << ((f.kw * 8 - 1) as i64)) - 1;
            let low = -high - 1;
            if v > high {
                v = high;
                jr.rep.clamped += 1;
            } else if v < low {
                v = low;
                jr.rep.clamped += 1;
            }
        }
        slot = v as! T;
        return true;
    }
    var u: u64 = 0;
    if whole {
        let neg, mag, over = json_token_int(tok);
        if neg {
            u = 0;
            sat = mag != 0;
        } else if over {
            u = 0xFFFFFFFFFFFFFFFF;
            sat = true;
        } else {
            u = mag;
        }
    } else {
        let d, fin = json_token_f64(tok);
        if !fin {
            jr.rep.kind_mismatch += 1;
            return true;
        }
        if d < 0.0 {
            if d != ((d as! i64) as f64) {
                jr.rep.kind_mismatch += 1;
                return true;
            }
            u = 0;
            sat = true;
        } else if d >= 18446744073709551616.0 {
            u = 0xFFFFFFFFFFFFFFFF;
            sat = true;
        } else if d != ((d as! u64) as f64) {
            jr.rep.kind_mismatch += 1;
            return true;
        } else {
            u = d as! u64;
        }
    }
    if sat { jr.rep.clamped += 1; }
    var ranged = f.ranged;
    var lo: u64 = f.lo.lo;
    var hi: u64 = f.hi.lo;
    if !ranged && f.bits > 0 && f.bits < 64 {
        ranged = true;
        lo = 0;
        hi = ((1 as u64) << (f.bits as! u64)) - 1;
    }
    if ranged {
        if u < lo {
            u = lo;
            jr.rep.clamped += 1;
        } else if u > hi {
            u = hi;
            jr.rep.clamped += 1;
        }
    }
    if f.kw < 8 {
        let high: u64 = ((1 as u64) << ((f.kw * 8) as! u64)) - 1;
        if u > high {
            u = high;
            jr.rep.clamped += 1;
        }
    }
    slot = u as! T;
    return true;
}

// json_read_float places a number in a float field: a magnitude its width
// cannot hold is kind_mismatch and never stored; a compressed float clamps to
// its range and its -0 lands +0 (§16.2).
fn json_read_float(jr: JsonIn&, slot: f64&, f: const FieldDesc&) -> bool {
    let start, end, whole, ok = json_number(jr);
    if !ok { return false; }
    var v, fin = json_token_f64(jr.text[start..end]);
    if !fin {
        jr.rep.kind_mismatch += 1;
        return true;
    }
    if f.frange {
        if v < f.flo {
            v = f.flo;
            jr.rep.clamped += 1;
        } else if v > f.fhi {
            v = f.fhi;
            jr.rep.clamped += 1;
        } else if v == 0.0 {
            v = 0.0;
        }
    }
    slot = v;
    return true;
}

fn json_read_float(jr: JsonIn&, slot: f32&, f: const FieldDesc&) -> bool {
    let start, end, whole, ok = json_number(jr);
    if !ok { return false; }
    let x, fin = json_token_f32(jr.text[start..end]);
    if !fin {
        jr.rep.kind_mismatch += 1;
        return true;
    }
    var v = x as f64;
    if f.frange {
        if v < f.flo {
            v = f.flo;
            jr.rep.clamped += 1;
        } else if v > f.fhi {
            v = f.fhi;
            jr.rep.clamped += 1;
        } else if v == 0.0 {
            v = 0.0;
        }
    }
    let narrow = v as f32;
    if narrow - narrow != 0.0 {
        jr.rep.kind_mismatch += 1;
        return true;
    }
    slot = narrow;
    return true;
}

fn json_read_bool(jr: JsonIn&, slot: bool&) -> bool {
    if json_peek(jr) == 't' {
        if !json_literal(jr, "true") { return false; }
        slot = true;
        return true;
    }
    if !json_literal(jr, "false") { return false; }
    slot = false;
    return true;
}

// json_read_str places a string(N): clamped at a code point boundary and
// counted past N; a zero refuses (§16.2, §16.3).
fn json_read_str(jr: JsonIn&, buf: u8[:], length: i32&) -> bool {
    var i: i64 = 0;
    while i < buf.len {
        buf[i] = 0;
        i += 1;
    }
    let n, ok = json_scan_into(jr, buf, true);
    if !ok { return false; }
    length = n as! i32;
    return true;
}

fn json_b64_sextet(c: u8) -> i64 {
    if c >= 'A' && c <= 'Z' { return c - 'A'; }
    if c >= 'a' && c <= 'z' { return (c - 'a') as i64 + 26; }
    if c >= '0' && c <= '9' { return (c - '0') as i64 + 52; }
    if c == '+' { return 62; }
    if c == '/' { return 63; }
    return -1;
}

// json_read_bytes places a bytes(N) from base64: the string's escapes are
// undone first, the body is held to the grammar (standard alphabet, padded or
// not), a body outside it is kind_mismatch with the field at its default, and
// past N clamps and counts (§16.2).
fn json_read_bytes(jr: JsonIn&, buf: u8[:], length: i32&) -> bool {
    var symbols: i64 = 0;
    var pads: i64 = 0;
    var bad = false;
    let start = jr.pos;
    let ok = json_scan(jr, false) { unit, n =>
        var k: i64 = 0;
        while k < n {
            let c = unit[k];
            if !bad {
                if c == '=' {
                    pads += 1;
                } else if pads > 0 || json_b64_sextet(c) < 0 {
                    bad = true;
                } else {
                    symbols += 1;
                }
            }
            k += 1;
        }
    };
    if !ok { return false; }
    if !bad {
        if pads > 0 {
            if pads > 2 || (symbols + pads) % 4 != 0 || symbols % 4 < 2 { bad = true; }
        } else if symbols % 4 == 1 {
            bad = true;
        }
    }
    if bad {
        jr.rep.kind_mismatch += 1;
        return true;
    }
    let end = jr.pos;
    jr.pos = start;
    var i: i64 = 0;
    while i < buf.len {
        buf[i] = 0;
        i += 1;
    }
    var placed: i64 = 0;
    var acc: u64 = 0;
    var held: u64 = 0;
    var clamped = false;
    var done = false;
    let again = json_scan(jr, false) { unit, n =>
        var k: i64 = 0;
        while k < n {
            let c = unit[k];
            if c == '=' { done = true; }
            if !done {
                acc = (acc << 6) | (json_b64_sextet(c) as! u64);
                held += 6;
                if held >= 8 {
                    held -= 8;
                    if placed < buf.len {
                        buf[placed] = ((acc >> held) & 0xFF) as! u8;
                        placed += 1;
                    } else {
                        clamped = true;
                    }
                }
            }
            k += 1;
        }
    };
    if !again { return false; }
    jr.pos = end;
    if clamped { jr.rep.clamped += 1; }
    length = placed as! i32;
    return true;
}

// json_read_enum places an enum by its variant NAME: `None` is 0, and a name
// this build cannot name reads as None and counts unknown (§16.2).
fn json_read_enum<T>(jr: JsonIn&, slot: T&, f: const FieldDesc&) -> bool {
    var key: u8[256] = [0; 256];
    let n, ok = json_key(jr, key);
    if !ok { return false; }
    let name = key[..n];
    if json_name_is("None", name) {
        slot = 0 as! T;
        return true;
    }
    var v: i64 = 0;
    while v < f.variants.len {
        if json_name_is(f.variants[v].name, name) {
            slot = (v + 1) as! T;
            return true;
        }
        v += 1;
    }
    slot = 0 as! T;
    jr.rep.unknown += 1;
    return true;
}

// json_read_flags places a flags mask from its array of names: an unknown
// name is skipped and counted, a non-string element is kind_mismatch (§16.2).
fn json_read_flags<T>(jr: JsonIn&, slot: T&, names: const (const u8[:])[:], depth: i64) -> bool {
    if !json_expect(jr, '[') { return false; }
    var bits: u64 = 0;
    loop {
        let c = json_peek(jr);
        if c == ']' {
            jr.pos += 1;
            break;
        }
        if c == 0 { return json_fail(jr); }
        if c != '"' {
            jr.rep.kind_mismatch += 1;
            if !json_skip_value(jr, depth + 1) { return false; }
        } else {
            var key: u8[256] = [0; 256];
            let n, ok = json_key(jr, key);
            if !ok { return false; }
            var found = false;
            var b: i64 = 0;
            while b < names.len {
                if json_name_is(names[b], key[..n]) {
                    bits |= (1 as u64) << (b as! u64);
                    found = true;
                    break;
                }
                b += 1;
            }
            if !found { jr.rep.unknown += 1; }
        }
        if !json_after_value(jr, ']') {
            if jr.bad { return false; }
            break;
        }
    }
    slot = bits as! T;
    return true;
}

// json_read_array places a [N]T or a [..N]T: the caller has put every slot
// back at its default (a repeated key replaces the whole array); past the
// bound the excess is dropped and counted, a wrong-shaped element is
// kind_mismatch and keeps its slot, and the count is the elements placed
// (§16.2). F(jr, index, depth) reads one element.
fn json_read_array<F>(jr: JsonIn&, count: i32&, bound: i64, shape: u8, depth: i64) -> bool {
    if !json_expect(jr, '[') { return false; }
    var placed: i64 = 0;
    loop {
        let c = json_peek(jr);
        if c == ']' {
            jr.pos += 1;
            break;
        }
        if c == 0 { return json_fail(jr); }
        if placed >= bound {
            jr.rep.clamped += 1;
            if !json_skip_value(jr, depth + 1) { return false; }
        } else if json_shape(jr) != shape {
            jr.rep.kind_mismatch += 1;
            if !json_skip_value(jr, depth + 1) { return false; }
            placed += 1;
        } else {
            if !F(jr, placed, depth + 1) { return false; }
            placed += 1;
        }
        if !json_after_value(jr, ']') {
            if jr.bad { return false; }
            break;
        }
    }
    count = placed as! i32;
    return true;
}

// json_read_keyed places an [E]T, an object keyed by VARIANT NAME: slot i
// holds the key i + 1 (nothing is stored for None, so "None" names no slot);
// a repeated variant is last-wins and counted, an unknown one counted
// (§16.2). The caller has put every slot back at its default.
fn json_read_keyed<F>(jr: JsonIn&, names: const (const u8[:])[:], shape: u8, depth: i64) -> bool {
    if !json_expect(jr, '{') { return false; }
    var seen: u64[8] = [0; 8];
    loop {
        let c = json_peek(jr);
        if c == '}' {
            jr.pos += 1;
            break;
        }
        if c == 0 { return json_fail(jr); }
        var key: u8[256] = [0; 256];
        let n, ok = json_key(jr, key);
        if !ok { return false; }
        if !json_expect(jr, ':') { return false; }
        var slot: i64 = -1;
        var v: i64 = 0;
        while v < names.len {
            if json_name_is(names[v], key[..n]) {
                slot = v;
                break;
            }
            v += 1;
        }
        if slot >= 0 && slot < 512 {
            let bit = (1 as u64) << ((slot & 63) as! u64);
            if (seen[slot >> 6] & bit) != 0 { jr.rep.duplicate += 1; }
            seen[slot >> 6] |= bit;
        }
        if slot < 0 {
            jr.rep.unknown += 1;
            if !json_skip_value(jr, depth + 1) { return false; }
        } else if json_shape(jr) != shape {
            jr.rep.kind_mismatch += 1;
            if !json_skip_value(jr, depth + 1) { return false; }
        } else if !F(jr, slot, depth + 1) {
            return false;
        }
        if !json_after_value(jr, '}') {
            if jr.bad { return false; }
            break;
        }
    }
    return true;
}

// json_read_union places a union: an object with ONE key, the arm's name;
// `{}` is None, an unknown arm counts unknown, an arm value of the wrong
// shape is kind_mismatch, and two keys is malformed (§16.2). F(jr, u, tag,
// op, depth) reads an arm (JsonOpRead) or sets the tag (JsonOpTag).
fn json_read_union<U, F>(jr: JsonIn&, u: U&, d: const UnionDesc&, shapes: const u8[:], depth: i64) -> bool {
    if !json_expect(jr, '{') { return false; }
    if !F(jr, u, 0, JsonOpTag, depth) { return false; }
    if json_peek(jr) == '}' {
        jr.pos += 1;
        return true;
    }
    var key: u8[256] = [0; 256];
    let n, ok = json_key(jr, key);
    if !ok { return false; }
    if !json_expect(jr, ':') { return false; }
    var tag: i64 = 0;
    var a: i64 = 0;
    while a < d.arms.len {
        if json_name_is(d.arms[a].name, key[..n]) {
            tag = d.arms[a].tag;
            break;
        }
        a += 1;
    }
    if tag == 0 {
        jr.rep.unknown += 1;
        if !json_skip_value(jr, depth + 1) { return false; }
    } else if json_shape(jr) != shapes[a] {
        jr.rep.kind_mismatch += 1;
        if !json_skip_value(jr, depth + 1) { return false; }
    } else {
        if shapes[a] == 'z' {
            if !json_literal(jr, "null") { return false; }
        } else if !F(jr, u, tag, JsonOpRead, depth + 1) {
            return false;
        }
        if !F(jr, u, tag, JsonOpTag, depth) { return false; }
    }
    var c = json_peek(jr);
    if c == ',' {
        jr.pos += 1;
        c = json_peek(jr);
    }
    if c == '}' {
        jr.pos += 1;
        return true;
    }
    return json_fail(jr);
}

// json_read_table places one object into one record: keys in any order,
// unknown keys skipped and counted, a repeated key last-wins and counted
// (fields below index 512), an `&` key malformed (it is the form's, §16.7),
// a key of the wrong JSON type kind_mismatch, an optional's null ABSENT
// (docs/SPEC-TABLES.md §16.2). F(jr, rec, ordinal, op, depth) is the
// table's accessor; shapes[i] is field i's JSON type.
fn json_read_table<T, F>(jr: JsonIn&, rec: T&, d: const TableDesc&, shapes: const u8[:], depth: i64) -> bool {
    if depth > JsonMaxDepth { return json_fail(jr); }
    if !json_expect(jr, '{') { return false; }
    var seen: u64[8] = [0; 8];
    loop {
        let c = json_peek(jr);
        if c == '}' {
            jr.pos += 1;
            return true;
        }
        if c == 0 { return json_fail(jr); }
        var key: u8[256] = [0; 256];
        let n, ok = json_key(jr, key);
        if !ok { return false; }
        if !json_expect(jr, ':') { return false; }
        var index: i64 = -1;
        var i: i64 = 0;
        while i < d.fields.len {
            if json_name_is(d.fields[i].json, key[..n]) {
                index = i;
                break;
            }
            i += 1;
        }
        if n > 0 && key[0] == '&' {
            jr.rep.reason = JsonReasonReservedKey;
            return json_fail(jr);
        }
        if index < 0 {
            jr.rep.unknown += 1;
            if !json_skip_value(jr, depth + 1) { return false; }
        } else {
            if index < 512 {
                let bit = (1 as u64) << ((index & 63) as! u64);
                if (seen[index >> 6] & bit) != 0 { jr.rep.duplicate += 1; }
                seen[index >> 6] |= bit;
            }
            let got = json_shape(jr);
            let optional = d.fields[index].optional;
            if optional && got == 'z' {
                if !json_literal(jr, "null") { return false; }
                if !F(jr, rec, index, JsonOpAbsent, depth) { return false; }
            } else if got != shapes[index] {
                jr.rep.kind_mismatch += 1;
                if !json_skip_value(jr, depth + 1) { return false; }
            } else {
                if !F(jr, rec, index, JsonOpRead, depth) {
                    if jr.zero && jr.rep.reason == 0 {
                        jr.rep.reason = JsonReasonHoldsZero;
                    }
                    return false;
                }
                if optional && !F(jr, rec, index, JsonOpPresent, depth) { return false; }
            }
        }
        if !json_after_value(jr, '}') { return !jr.bad; }
    }
}

// json_finish closes a read: whitespace and comments after the root object
// are skipped, anything else is malformed (§16.1); the report is the
// caller's.
fn json_finish(jr: JsonIn&, ok: bool, report: JsonReport&) -> bool {
    var good = ok && !jr.bad;
    if good {
        json_space(jr);
        if jr.bad || jr.pos != jr.text.len { good = false; }
    }
    report = jr.rep;
    report.malformed = !good;
    return good;
}

// json_clean is the strict read's verdict over a tolerant read's report: a
// text that matched the schema exactly (§6.5, §16.2).
fn json_clean(report: const JsonReport&) -> bool {
    return !report.malformed && report.unknown == 0 && report.kind_mismatch == 0 && report.clamped == 0 && report.duplicate == 0;
}

// ---- writing ----

// json_line breaks a line and indents two spaces per level (§16.1).
fn json_line(out: u8[>..]&, depth: i64) {
    out.push('\n');
    var i: i64 = 0;
    while i < depth {
        out.append("  ");
        i += 1;
    }
}

// json_put_string writes a string with the seven short escapes, `\u00xx` in
// lowercase for every other control byte, and one U+FFFD per maximal subpart
// of an ill-formed sequence (§16.3).
fn json_put_string(out: u8[>..]&, s: const u8[:]) {
    let hex = "0123456789abcdef";
    out.push('"');
    var i: i64 = 0;
    while i < s.len {
        let c = s[i];
        if c == '"' {
            out.append("\\\"");
        } else if c == '\\' {
            out.append("\\\\");
        } else if c == 8 {
            out.append("\\b");
        } else if c == 12 {
            out.append("\\f");
        } else if c == 10 {
            out.append("\\n");
        } else if c == 13 {
            out.append("\\r");
        } else if c == 9 {
            out.append("\\t");
        } else if c < 0x20 {
            out.append("\\u00");
            out.push(hex[(c >> 4) as i64]);
            out.push(hex[(c & 15) as i64]);
        } else if c < 0x80 {
            out.push(c);
        } else {
            let code, w = json_utf8(s, i);
            if w < 0 {
                out.append("\xEF\xBF\xBD");
                i += -w - 1;
            } else {
                var k: i64 = 0;
                while k < w {
                    out.push(s[i + k]);
                    k += 1;
                }
                i += w - 1;
            }
        }
        i += 1;
    }
    out.push('"');
}

// json_put_base64 writes bytes as standard base64, padded (§16.2).
fn json_put_base64(out: u8[>..]&, s: const u8[:]) {
    let alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    out.push('"');
    var i: i64 = 0;
    while s.len - i >= 3 {
        let triple = (s[i] as u64) << 16 | (s[i + 1] as u64) << 8 | s[i + 2];
        out.push(alphabet[((triple >> 18) & 63) as! i64]);
        out.push(alphabet[((triple >> 12) & 63) as! i64]);
        out.push(alphabet[((triple >> 6) & 63) as! i64]);
        out.push(alphabet[(triple & 63) as! i64]);
        i += 3;
    }
    if i < s.len {
        let left = s.len - i;
        var triple = (s[i] as u64) << 16;
        if left == 2 { triple |= (s[i + 1] as u64) << 8; }
        out.push(alphabet[((triple >> 18) & 63) as! i64]);
        out.push(alphabet[((triple >> 12) & 63) as! i64]);
        if left == 2 { out.push(alphabet[((triple >> 6) & 63) as! i64]); } else { out.push('='); }
        out.push('=');
    }
    out.push('"');
}

fn json_put_uint(out: u8[>..]&, v0: u64) {
    var digits: u8[24] = [0; 24];
    var n: i64 = 24;
    var v = v0;
    loop {
        n -= 1;
        digits[n] = ((v % 10) as! u8) + '0';
        v /= 10;
        if v == 0 { break; }
    }
    out.append(digits[n..]);
}

fn json_put_int(out: u8[>..]&, v: i64) {
    if v < 0 {
        out.push('-');
        json_put_uint(out, 0 - (v as! u64));
        return;
    }
    json_put_uint(out, v as! u64);
}

fn json_write_bool(out: u8[>..]&, v: bool) -> bool {
    if v { out.append("true"); } else { out.append("false"); }
    return true;
}

fn json_write_int<T>(out: u8[>..]&, v: T, f: const FieldDesc&) -> bool {
    if f.signed { json_put_int(out, v as! i64); } else { json_put_uint(out, v as! u64); }
    return true;
}

fn json_write_float(out: u8[>..]&, v: f32) -> bool { return json_put_float(out, v, true); }
fn json_write_float(out: u8[>..]&, v: f64) -> bool { return json_put_float(out, v, false); }

fn json_write_str(out: u8[>..]&, s: const u8[:], length: i32) -> bool {
    if length < 0 || length > s.len { return false; }
    json_put_string(out, s[..length as i64]);
    return true;
}

fn json_write_bytes(out: u8[>..]&, s: const u8[:], length: i32) -> bool {
    if length < 0 || length > s.len { return false; }
    json_put_base64(out, s[..length as i64]);
    return true;
}

// json_write_enum writes a variant's NAME; a value no variant names is
// refused rather than written as something else (§16.3).
fn json_write_enum<T>(out: u8[>..]&, v: T, f: const FieldDesc&) -> bool {
    let x = v as! u64;
    if x == 0 {
        json_put_string(out, "None");
        return true;
    }
    if x > (f.variants.len as! u64) { return false; }
    json_put_string(out, f.variants[(x - 1) as! i64].name);
    return true;
}

// json_write_flags writes a mask as its names; a set bit no variant names is
// refused (§16.3).
fn json_write_flags<T>(out: u8[>..]&, v: T, names: const (const u8[:])[:], depth: i64) -> bool {
    let bits = v as! u64;
    if bits == 0 {
        out.append("[]");
        return true;
    }
    out.push('[');
    var first = true;
    var b: i64 = 0;
    while b < 64 {
        if (bits & ((1 as u64) << (b as! u64))) != 0 {
            if b >= names.len { return false; }
            if !first { out.push(','); }
            first = false;
            json_line(out, depth + 1);
            json_put_string(out, names[b]);
        }
        b += 1;
    }
    json_line(out, depth);
    out.push(']');
    return true;
}

// json_write_array writes count elements in index order; F(out, i, depth)
// writes one.
fn json_write_array<F>(out: u8[>..]&, count: i64, depth: i64) -> bool {
    if count == 0 {
        out.append("[]");
        return true;
    }
    out.push('[');
    var i: i64 = 0;
    while i < count {
        if i > 0 { out.push(','); }
        json_line(out, depth + 1);
        if !F(out, i, depth + 1) { return false; }
        i += 1;
    }
    json_line(out, depth);
    out.push(']');
    return true;
}

// json_write_keyed writes an [E]T as an object keyed by variant name, one
// entry per slot (§16.2).
fn json_write_keyed<F>(out: u8[>..]&, names: const (const u8[:])[:], depth: i64) -> bool {
    if names.len == 0 {
        out.append("{}");
        return true;
    }
    out.push('{');
    var i: i64 = 0;
    while i < names.len {
        if i > 0 { out.push(','); }
        json_line(out, depth + 1);
        json_put_string(out, names[i]);
        out.append(": ");
        if !F(out, i, depth + 1) { return false; }
        i += 1;
    }
    json_line(out, depth);
    out.push('}');
    return true;
}

// json_write_union writes a union as an object with ONE key, the arm's name,
// and `{}` for None; a tag no arm names is refused (§16.2, §16.3).
fn json_write_union<U, F>(out: u8[>..]&, u: const U&, d: const UnionDesc&, tag: i64, depth: i64) -> bool {
    if tag == 0 {
        out.append("{}");
        return true;
    }
    var a: i64 = 0;
    while a < d.arms.len && d.arms[a].tag != tag { a += 1; }
    if a >= d.arms.len { return false; }
    out.push('{');
    json_line(out, depth + 1);
    json_put_string(out, d.arms[a].name);
    out.append(": ");
    if d.arms[a].shape == ShapeVoid {
        out.append("null");
    } else if !F(out, u, tag, depth + 1) {
        return false;
    }
    json_line(out, depth);
    out.push('}');
    return true;
}

// json_write_table writes one record as one object, every field in
// declaration order, defaults included, an absent optional as no key
// (§16.1, §16.2). F(out, rec, ordinal, op, depth) is the table's accessor:
// JsonOpAsk answers whether the key is written, JsonOpWrite writes its value.
fn json_write_table<T, F>(out: u8[>..]&, rec: const T&, d: const TableDesc&, depth: i64) -> bool {
    if depth > JsonMaxDepth { return false; }
    var any = false;
    var i: i64 = 0;
    while i < d.fields.len {
        if F(out, rec, i, JsonOpAsk, depth) {
            if any { out.push(','); } else { out.push('{'); }
            any = true;
            json_line(out, depth + 1);
            json_put_string(out, d.fields[i].json);
            out.append(": ");
            if !F(out, rec, i, JsonOpWrite, depth + 1) { return false; }
        }
        i += 1;
    }
    if !any {
        out.append("{}");
        return true;
    }
    json_line(out, depth);
    out.push('}');
    return true;
}

// json_from reads one root from one text, tolerantly (docs/SPEC-TABLES.md
// §16.1): false (and report.malformed) when the text is not JSON or a value
// is refused by name; the caller then puts the record back at its defaults.
fn json_from<T, F>(rec: T&, d: const TableDesc&, shapes: const u8[:], text: const u8[:], report: JsonReport&) -> bool {
    var jr = json_in(text);
    let ok = json_read_table(jr, rec, d, shapes, 0, F);
    return json_finish(jr, ok, report);
}

// json_to appends one record's canonical text, ending in exactly one newline,
// and answers its length, or -1 (nothing appended) for a value the form
// cannot spell (§16.1, §16.3).
fn json_to<T, F>(rec: const T&, d: const TableDesc&, out: u8[>..]&) -> i64 {
    var text: u8[>..] = [];
    if !json_write_table(text, rec, d, 0, F) { return -1; }
    text.push('\n');
    out.append(text);
    return text.len;
}

// json_measure is json_to's exact length, or -1.
fn json_measure<T, F>(rec: const T&, d: const TableDesc&) -> i64 {
    var text: u8[>..] = [];
    if !json_write_table(text, rec, d, 0, F) { return -1; }
    return text.len + 1;
}

// ---- json walk: end ----
