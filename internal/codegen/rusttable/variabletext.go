package rusttable

// THE VARIABLE FORM'S TEXT (docs/SPEC-TABLES.md §16, §16.2, §16.7): the Rust
// JSON text form in and out for the variable class this leg carries. A
// pretty-printed writer that spells a table as the committed conformance texts
// byte for byte, and a tolerant reader that places the same values back so the
// wire round-trips (§16.5). A pointer is the pointee's object in place, and the
// one construct §16.7 adds for sharing — `&node`, a label on a node named more
// than once — is written where it must be and read wherever it is.

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitVariableText emits the JSON text form for every table the file's codec
// emits: each enum's name mapping, each union's text codec, and, per table, the
// counting pass, the write, the read and, for a variable root, the public roots
// (docs/SPEC-TABLES.md §16.1).
func (g *gen) emitVariableText(b *strings.Builder) {
	names := cardVariableTableNames(g)
	if len(names) == 0 {
		return
	}
	b.WriteString("\n// ---- the JSON text form (docs/SPEC-TABLES.md §16, §16.2, §16.7) ----\n")

	g.emitUnionText(b, names)

	for _, n := range names {
		g.emitTextCount(b, vStruct(g.unit, n))
	}
	for _, n := range names {
		g.emitTextWrite(b, vStruct(g.unit, n))
	}
	for _, n := range names {
		g.emitTextRead(b, vStruct(g.unit, n))
		g.emitTextReadPointer(b, vStruct(g.unit, n))
	}
	for _, n := range names {
		if g.isVariableRoot(vStruct(g.unit, n)) {
			g.emitTextRoots(b, vStruct(g.unit, n))
		}
	}
}

// textHelpers is the shared JSON machinery the generated body codecs ride on: a
// pretty-print newline, the string escape (RFC 8259, §16.2), a code-point
// clamp, base64 both ways, the float spelling, the writer's state for §16.7's
// labels, and the tolerant cursor (§16.2's report counters).
func textHelpers() string {
	return `
#[allow(dead_code)]
pub fn json_newline(out: &mut String, depth: usize) {
    out.push('\n');
    for _ in 0..depth {
        out.push_str("  ");
    }
}

/// Escape one string as a JSON string (docs/SPEC-TABLES.md §16.2): the control
/// characters JSON spells, the quote and the backslash; every other code point
/// rides raw, so multibyte text stays byte for byte.
#[allow(dead_code)]
pub fn json_escape(s: &str, out: &mut String) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{0008}' => out.push_str("\\b"),
            '\u{000c}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => {
                out.push_str("\\u00");
                let h = format!("{:02x}", c as u32);
                out.push_str(&h);
            }
            c => out.push(c),
        }
    }
    out.push('"');
}

/// Clamp a string to n bytes at a code point boundary (docs/SPEC-TABLES.md
/// §16.2): longer than n is clamped, counted, never cut through a multibyte
/// character.
#[allow(dead_code)]
pub fn json_clamp(s: String, n: usize, clamped: &mut u64) -> String {
    if s.len() <= n {
        return s;
    }
    let mut end = n;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    *clamped += 1;
    s[..end].to_string()
}

/// A JSON string as wstring(N) code units (docs/SPEC-TABLES.md §16.2's wstring
/// row): transcoded at the boundary, and clamped at N CODE UNITS with a pair
/// never split; once a character does not fit nothing after it is kept, and
/// the clamp counts once (the oracle's tabletext scanWString).
#[allow(dead_code)]
pub fn json_wide_clamp(s: &str, n: usize, clamped: &mut u64) -> Vec<u16> {
    let mut out: Vec<u16> = Vec::new();
    let mut buf = [0u16; 2];
    for c in s.chars() {
        let units = c.encode_utf16(&mut buf);
        if out.len() + units.len() > n {
            *clamped += 1;
            break;
        }
        out.extend_from_slice(units);
    }
    out
}

pub const JSON_B64: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

/// Bytes as a base64 JSON string (docs/SPEC-TABLES.md §16.2): the standard
/// alphabet, PADDED on write.
#[allow(dead_code)]
pub fn json_b64(bytes: &[u8], out: &mut String) {
    out.push('"');
    for chunk in bytes.chunks(3) {
        let triple = (chunk[0] as u32) << 16
            | (chunk.get(1).copied().unwrap_or(0) as u32) << 8
            | chunk.get(2).copied().unwrap_or(0) as u32;
        out.push(JSON_B64[(triple >> 18 & 0x3f) as usize] as char);
        out.push(JSON_B64[(triple >> 12 & 0x3f) as usize] as char);
        if chunk.len() > 1 { out.push(JSON_B64[(triple >> 6 & 0x3f) as usize] as char); } else { out.push('='); }
        if chunk.len() > 2 { out.push(JSON_B64[(triple & 0x3f) as usize] as char); } else { out.push('='); }
    }
    out.push('"');
}

/// Base64 back to bytes (docs/SPEC-TABLES.md §16.2): padded and unpadded both
/// read; anything else is None, which the caller counts kind_mismatch.
#[allow(dead_code)]
pub fn json_unb64(s: &str) -> Option<Vec<u8>> {
    let b = s.as_bytes();
    let mut end = b.len();
    while end > 0 && b[end - 1] == b'=' {
        end -= 1;
    }
    if b.len() - end > 2 || (b.len() != end && !b.len().is_multiple_of(4)) || end % 4 == 1 {
        return None;
    }
    let mut out = Vec::with_capacity(end * 3 / 4);
    let mut acc = 0u32;
    let mut bits = 0u32;
    for &c in &b[..end] {
        let v = match c {
            b'A'..=b'Z' => c - b'A',
            b'a'..=b'z' => c - b'a' + 26,
            b'0'..=b'9' => c - b'0' + 52,
            b'+' => 62,
            b'/' => 63,
            _ => return None,
        };
        acc = acc << 6 | v as u32;
        bits += 6;
        if bits >= 8 {
            bits -= 8;
            out.push((acc >> bits & 0xff) as u8);
        }
    }
    Some(out)
}

/// C's %.*g for a finite double, as the reference spells it
/// (internal/tabletext/write.go formatG): the exponent form when the decimal
/// exponent is below -4 or at least the precision, the plain form otherwise,
/// trailing zeros and a trailing point removed, an exponent of two digits.
#[allow(dead_code)]
pub fn json_format_g(v: f64, prec: usize) -> String {
    let trim = |s: &str| -> String {
        if !s.contains('.') {
            return s.to_string();
        }
        let t = s.trim_end_matches('0');
        t.trim_end_matches('.').to_string()
    };
    let sci = format!("{:.*e}", prec - 1, v);
    let (mant, exp) = sci.split_once('e').unwrap();
    let exp: i32 = exp.parse().unwrap();
    if exp < -4 || exp >= prec as i32 {
        let sign = if exp < 0 { '-' } else { '+' };
        return format!("{}e{}{:02}", trim(mant), sign, exp.abs());
    }
    trim(&format!("{:.*}", (prec as i32 - 1 - exp) as usize, v))
}

/// A FIXED value's text (docs/SPEC-TABLES.md §16.2): WHOLE UNITS, the shortest
/// EXACT decimal with at least one fractional digit (` + "`1.0`, `-0.25`" + `),
/// spelled from the raw integer at F fractional bits with no float between.
/// A signed raw value arrives sign-extended into the u128.
#[allow(dead_code)]
pub fn json_fixed(out: &mut String, raw: u128, signed: bool, frac: u32) {
    let mut m = raw;
    if signed && (raw as i128) < 0 {
        out.push('-');
        m = raw.wrapping_neg();
    }
    let whole = m.checked_shr(frac).unwrap_or(0);
    out.push_str(&whole.to_string());
    out.push('.');
    let mask = if frac >= 128 { u128::MAX } else { (1u128 << frac) - 1 };
    let mut f = m & mask;
    if f == 0 {
        out.push('0');
        return;
    }
    while f != 0 {
        // f × 10 across 132 bits: the digit is what rises past bit F
        let lo = (f as u64 as u128) * 10;
        let hi = (f >> 64) * 10 + (lo >> 64);
        let low = (hi << 64) | (lo & 0xffff_ffff_ffff_ffff);
        let carry = hi >> 64;
        let digit = if frac >= 128 { carry } else { (low >> frac) | (carry << (128 - frac)) };
        out.push((b'0' + digit as u8) as char);
        f = low & mask;
    }
}

/// A float at the SHORTEST precision that reads back as the same value at the
/// field's own width (docs/SPEC-TABLES.md §16.2), exactly as the reference
/// writes it. A non-finite value has no JSON spelling; the writer marks it.
#[allow(dead_code)]
pub fn json_float(v: f64, single: bool, w: &mut Jw) {
    if !v.is_finite() {
        w.refused = true;
        w.out.push_str("null");
        return;
    }
    let (low, high) = if single { (6, 9) } else { (15, 17) };
    let mut digits = low;
    loop {
        let text = json_format_g(v, digits);
        if digits >= high {
            w.out.push_str(&text);
            return;
        }
        if let Ok(back) = text.parse::<f64>() {
            let same = if single { (back as f32) as f64 == v } else { back == v };
            if same {
                w.out.push_str(&text);
                return;
            }
        }
        digits += 1;
    }
}

/// The text writer's state (docs/SPEC-TABLES.md §16.7): the text, how many slots
/// name each node (the first pass), the label each node named more than once
/// took (the second pass, from 1 in first-write order), and whether the graph
/// is past what the form can spell — the depth cap, or a value with no JSON
/// spelling (§16.3).
#[allow(dead_code)]
pub struct Jw {
    pub out: String,
    pub counts: HashMap<usize, u32>,
    pub labels: HashMap<usize, u64>,
    pub next: u64,
    pub refused: bool,
}

/// The DEPTH CAP the text form carries (docs/SPEC-TABLES.md §16.2, §16.7): a
/// chain nests as deep as it is long, and the writer refuses a graph past the
/// reader's cap rather than write a text the reader would refuse.
#[allow(dead_code)]
pub const JSON_DEPTH_CAP: usize = 128;

/// A label the reader has met (docs/SPEC-TABLES.md §16.7): Open while its
/// definition's object is still being read, then the node it defined — or
/// Dropped, a definition inside a value the walk skipped (past a bound, under
/// an unknown key). A dropped definition KEEPS its label and its reference
/// reads null, the wire's rule for an unnameable node (§3.1).
#[allow(dead_code)]
pub enum JsonLabel {
    Open,
    Node(Rc<dyn Any>),
    Dropped,
}

/// The tolerant JSON cursor (docs/SPEC-TABLES.md §16.2): it skips whitespace,
/// reads a string, a null and a number token, and skips a whole value it
/// cannot place. Its counters fold into the caller's report.
#[allow(dead_code)]
pub struct Jr<'a> {
    pub b: &'a [u8],
    pub p: usize,
    pub depth: usize,
    pub unknown: u64,
    pub kind_mismatch: u64,
    pub clamped: u64,
    pub duplicate: u64,
    pub malformed: bool,
    /// set when a string decoded the U+0000 escape; read_text refuses it (§16.3)
    pub zero: bool,
    pub labels: HashMap<u64, JsonLabel>,
}

#[allow(dead_code)]
impl<'a> Jr<'a> {
    pub fn new(b: &'a [u8]) -> Jr<'a> {
        Jr { b, p: 0, depth: 0, unknown: 0, kind_mismatch: 0, clamped: 0, duplicate: 0, malformed: false, zero: false, labels: HashMap::new() }
    }
    pub fn space(&mut self) {
        while self.p < self.b.len() {
            match self.b[self.p] {
                b' ' | b'\t' | b'\n' | b'\r' => self.p += 1,
                b'/' if self.p + 1 < self.b.len() && self.b[self.p + 1] == b'/' => {
                    self.p += 2;
                    while self.p < self.b.len() && self.b[self.p] != b'\n' {
                        self.p += 1;
                    }
                }
                b'/' if self.p + 1 < self.b.len() && self.b[self.p + 1] == b'*' => {
                    self.p += 2;
                    let mut closed = false;
                    while self.p + 1 < self.b.len() {
                        if self.b[self.p] == b'*' && self.b[self.p + 1] == b'/' {
                            self.p += 2;
                            closed = true;
                            break;
                        }
                        self.p += 1;
                    }
                    if !closed {
                        self.malformed = true;
                        self.p = self.b.len();
                        return;
                    }
                }
                b'/' => {
                    self.malformed = true;
                    return;
                }
                _ => break,
            }
        }
    }
    pub fn peek(&mut self) -> u8 {
        self.space();
        if self.p < self.b.len() {
            self.b[self.p]
        } else {
            0
        }
    }
    pub fn hex4(&mut self) -> i32 {
        if self.p + 4 > self.b.len() {
            return -1;
        }
        let mut v = 0i32;
        for i in 0..4 {
            let c = self.b[self.p + i];
            let d = match c {
                b'0'..=b'9' => (c - b'0') as i32,
                b'a'..=b'f' => (c - b'a' + 10) as i32,
                b'A'..=b'F' => (c - b'A' + 10) as i32,
                _ => return -1,
            };
            v = v * 16 + d;
        }
        self.p += 4;
        v
    }
    pub fn read_string(&mut self) -> Option<String> {
        self.space();
        if self.p >= self.b.len() || self.b[self.p] != b'"' {
            self.malformed = true;
            return None;
        }
        self.p += 1;
        let mut out: Vec<u8> = Vec::new();
        loop {
            if self.p >= self.b.len() {
                self.malformed = true;
                return None;
            }
            let c = self.b[self.p];
            if c == b'"' {
                self.p += 1;
                break;
            }
            if c < 0x20 {
                self.malformed = true;
                return None;
            }
            if c == b'\\' {
                self.p += 1;
                if self.p >= self.b.len() {
                    self.malformed = true;
                    return None;
                }
                let e = self.b[self.p];
                self.p += 1;
                match e {
                    b'"' => out.push(b'"'),
                    b'\\' => out.push(b'\\'),
                    b'/' => out.push(b'/'),
                    b'b' => out.push(0x08),
                    b'f' => out.push(0x0c),
                    b'n' => out.push(b'\n'),
                    b'r' => out.push(b'\r'),
                    b't' => out.push(b'\t'),
                    b'u' => {
                        let v = self.hex4();
                        if v < 0 {
                            self.malformed = true;
                            return None;
                        }
                        let mut cp = v as u32;
                        if (0xd800..0xdc00).contains(&cp)
                            && self.p + 6 <= self.b.len()
                            && self.b[self.p] == b'\\'
                            && self.b[self.p + 1] == b'u'
                        {
                            let mark = self.p;
                            self.p += 2;
                            let lo = self.hex4();
                            if (0xdc00..0xe000).contains(&lo) {
                                cp = 0x10000 + ((cp - 0xd800) << 10) + (lo as u32 - 0xdc00);
                            } else {
                                // a lone lead surrogate rides as itself, and the
                                // escape after it is read on its own
                                self.p = mark;
                            }
                        }
                        self.zero |= cp == 0;
                        let ch = char::from_u32(cp).unwrap_or('\u{fffd}');
                        let mut buf = [0u8; 4];
                        out.extend_from_slice(ch.encode_utf8(&mut buf).as_bytes());
                    }
                    _ => {
                        self.malformed = true;
                        return None;
                    }
                }
            } else {
                out.push(c);
                self.p += 1;
            }
        }
        // §16.3: bytes that are not well-formed UTF-8 spell no code point,
        // so the read refuses them by name (string_not_utf8) at every string
        // site, before any clamp, and never replaces them with U+FFFD
        match String::from_utf8(out) {
            Ok(s) => Some(s),
            Err(_) => {
                self.malformed = true;
                None
            }
        }
    }
    /// read_text reads a string the read PLACES in text storage: a U+0000 in
    /// it refuses the read (text_holds_zero, docs/SPEC-TABLES.md §16.3), while
    /// a string the read only compares or skips goes through read_string.
    pub fn read_text(&mut self) -> Option<String> {
        self.zero = false;
        let s = self.read_string()?;
        if self.zero {
            self.malformed = true;
            return None;
        }
        Some(s)
    }
    pub fn read_null(&mut self) -> bool {
        self.space();
        if self.p + 4 <= self.b.len() && &self.b[self.p..self.p + 4] == b"null" {
            self.p += 4;
            true
        } else {
            self.malformed = true;
            false
        }
    }
    /// One number token, by RFC 8259's grammar (docs/SPEC-TABLES.md §16.2): a
    /// token that is not a JSON number at all is malformed.
    pub fn number_token(&mut self) -> Option<String> {
        self.space();
        let b = self.b;
        let s = self.p;
        let mut p = s;
        let digit = |p: usize| p < b.len() && b[p].is_ascii_digit();
        if p < b.len() && b[p] == b'-' {
            p += 1;
        }
        if !digit(p) {
            self.malformed = true;
            return None;
        }
        if b[p] == b'0' {
            p += 1;
        } else {
            while digit(p) {
                p += 1;
            }
        }
        if p < b.len() && b[p] == b'.' {
            p += 1;
            if !digit(p) {
                self.malformed = true;
                return None;
            }
            while digit(p) {
                p += 1;
            }
        }
        if p < b.len() && (b[p] == b'e' || b[p] == b'E') {
            p += 1;
            if p < b.len() && (b[p] == b'+' || b[p] == b'-') {
                p += 1;
            }
            if !digit(p) {
                self.malformed = true;
                return None;
            }
            while digit(p) {
                p += 1;
            }
        }
        if p < b.len() && !matches!(b[p], b' ' | b'\t' | b'\n' | b'\r' | b',' | b'}' | b']' | b'/') {
            self.malformed = true;
            return None;
        }
        self.p = p;
        Some(String::from_utf8_lossy(&b[s..p]).into_owned())
    }
    /// An integer field's value (docs/SPEC-TABLES.md §16.2): any token whose
    /// VALUE is integral, saturating past what 64 bits hold; a fractional one
    /// is kind_mismatch, and a value of another JSON type is kind_mismatch,
    /// skipped.
    pub fn read_int(&mut self) -> Option<i128> {
        let c = self.peek();
        if matches!(c, b'{' | b'[' | b'"' | b't' | b'f' | b'n') {
            self.kind_mismatch += 1;
            self.skip_value();
            return None;
        }
        let t = self.number_token()?;
        if !t.contains(['.', 'e', 'E']) {
            let neg = t.starts_with('-');
            let digits = t.strip_prefix('-').unwrap_or(&t);
            let mut mag: u64 = 0;
            for b in digits.bytes() {
                let d = (b - b'0') as u64;
                if mag > (u64::MAX - d) / 10 {
                    mag = u64::MAX;
                    self.clamped += 1;
                    break;
                }
                mag = mag * 10 + d;
            }
            if mag == 0 {
                return Some(0);
            }
            return Some(if neg { -(mag as i128) } else { mag as i128 });
        }
        let f: f64 = t.parse().ok()?;
        if !f.is_finite() || f.fract() != 0.0 {
            self.kind_mismatch += 1;
            return None;
        }
        let neg = f.is_sign_negative();
        let whole = f.abs();
        let mag = if whole >= 18446744073709551616.0 {
            self.clamped += 1;
            u64::MAX
        } else {
            whole as u64
        };
        if mag == 0 {
            return Some(0);
        }
        Some(if neg { -(mag as i128) } else { mag as i128 })
    }
    /// A WIDE value (docs/SPEC-TABLES.md §16.2): a 128-bit integer (frac 0)
    /// or a fixed(I, F)'s raw integer, read EXACTLY from any number token whose
    /// value the Q I.F grid holds — ` + "`2`, `2.0` and `20e-1`" + ` are one value —
    /// with no binary float between, as two's complement in a u128. A digit
    /// the grid cannot hold is kind_mismatch, the wrong shape, never rounded;
    /// a magnitude past 128 bits saturates and counts clamped; a value of
    /// another JSON type is kind_mismatch, skipped. The caller clamps to the
    /// field's raw range.
    pub fn read_wide(&mut self, signed: bool, frac: u32) -> Option<u128> {
        let c = self.peek();
        if matches!(c, b'{' | b'[' | b'"' | b't' | b'f' | b'n') {
            self.kind_mismatch += 1;
            self.skip_value();
            return None;
        }
        let token = self.number_token()?;
        let t = token.as_bytes();
        let negative = t.first() == Some(&b'-');
        let mut i = usize::from(negative);
        let int_start = i;
        while i < t.len() && t[i].is_ascii_digit() {
            i += 1;
        }
        let int_len = i - int_start;
        let mut frac_start = i;
        let mut frac_len = 0;
        if i < t.len() && t[i] == b'.' {
            i += 1;
            frac_start = i;
            while i < t.len() && t[i].is_ascii_digit() {
                i += 1;
            }
            frac_len = i - frac_start;
        }
        let mut exp: i64 = 0;
        if i < t.len() && (t[i] == b'e' || t[i] == b'E') {
            i += 1;
            let mut eneg = false;
            if i < t.len() && (t[i] == b'-' || t[i] == b'+') {
                eneg = t[i] == b'-';
                i += 1;
            }
            while i < t.len() && t[i].is_ascii_digit() {
                if exp < 100000 {
                    exp = exp * 10 + i64::from(t[i] - b'0');
                }
                i += 1;
            }
            if eneg {
                exp = -exp;
            }
        }
        let digit = |k: usize| if k < int_len { t[int_start + k] } else { t[frac_start + k - int_len] };
        let (mut start, mut end) = (0usize, int_len + frac_len);
        let mut point = int_len as i64 + exp;
        while start < end && digit(start) == b'0' {
            start += 1;
            point -= 1;
        }
        while end > start && digit(end - 1) == b'0' {
            end -= 1;
        }
        let signed_max = i128::MAX as u128;
        let signed_min = 1u128 << 127;
        let mut raw = 0u128;
        let mut saturated = false;
        if start == end {
            // zero, in any spelling, -0 included
        } else if point > 40 {
            saturated = true;
            raw = match (negative, signed) {
                (false, true) => signed_max,
                (false, false) => u128::MAX,
                (true, true) => signed_min,
                (true, false) => 0,
            };
        } else if point < -40 {
            self.kind_mismatch += 1;
            return None;
        } else {
            // the decimal fraction, at most 128 places: Q I.F holds I + F <= 128
            // bits, so a nonzero digit past the 128th place is inexact
            let mut fd = [0u8; 128];
            let mut n = point.min(0).unsigned_abs() as usize;
            let mut k = (point.max(0) as usize) + start;
            while k < end {
                if n >= 128 {
                    if digit(k) != b'0' {
                        self.kind_mismatch += 1;
                        return None;
                    }
                } else {
                    fd[n] = digit(k) - b'0';
                    n += 1;
                }
                k += 1;
            }
            let mut fraction = 0u128;
            for _ in 0..frac {
                let mut carry = 0u8;
                for d in fd[..n].iter_mut().rev() {
                    let x = *d * 2 + carry;
                    *d = x % 10;
                    carry = x / 10;
                }
                fraction = (fraction << 1) | u128::from(carry);
            }
            if fd[..n].iter().any(|&d| d != 0) {
                self.kind_mismatch += 1;
                return None;
            }
            let mut whole = 0u128;
            let mut k = start as i64;
            while k < start as i64 + point && !saturated {
                let d = if (k as usize) < end { u128::from(digit(k as usize) - b'0') } else { 0 };
                match whole.checked_mul(10).and_then(|w| w.checked_add(d)) {
                    Some(w) => whole = w,
                    None => saturated = true,
                }
                k += 1;
            }
            if !saturated && frac > 0 && whole.checked_shr(128 - frac).unwrap_or(0) != 0 {
                saturated = true;
            }
            if !saturated {
                raw = whole.checked_shl(frac).unwrap_or(0) | fraction;
            }
            if signed {
                if !saturated && !negative && raw >> 127 != 0 {
                    saturated = true;
                }
                if !saturated && negative && raw > signed_min {
                    saturated = true;
                }
                if saturated {
                    raw = if negative { signed_min } else { signed_max };
                } else if negative {
                    raw = raw.wrapping_neg();
                }
            } else {
                if saturated {
                    raw = u128::MAX;
                }
                if negative && raw != 0 {
                    raw = 0;
                    saturated = true;
                }
            }
        }
        if saturated {
            self.clamped += 1;
        }
        Some(raw)
    }
    /// A float field's value (docs/SPEC-TABLES.md §16.2).
    pub fn read_float(&mut self) -> Option<f64> {
        let c = self.peek();
        if matches!(c, b'{' | b'[' | b'"' | b't' | b'f' | b'n') {
            self.kind_mismatch += 1;
            self.skip_value();
            return None;
        }
        let t = self.number_token()?;
        t.parse::<f64>().ok()
    }
    /// A float32 field's value, converted once at single width (§16.2).
    pub fn read_f32(&mut self) -> Option<f32> {
        let c = self.peek();
        if matches!(c, b'{' | b'[' | b'"' | b't' | b'f' | b'n') {
            self.kind_mismatch += 1;
            self.skip_value();
            return None;
        }
        let t = self.number_token()?;
        t.parse::<f32>().ok()
    }
    /// A label (docs/SPEC-TABLES.md §16.7): a positive integer spelled as one —
    /// digits, no sign, no fraction, no exponent, no leading zero.
    pub fn read_label(&mut self) -> Option<u64> {
        self.space();
        let b = self.b;
        let s = self.p;
        let mut p = s;
        if p >= b.len() || !(b'1'..=b'9').contains(&b[p]) {
            return None;
        }
        while p < b.len() && b[p].is_ascii_digit() {
            p += 1;
        }
        if p < b.len() && !matches!(b[p], b' ' | b'\t' | b'\n' | b'\r' | b',' | b'}' | b'/') {
            return None;
        }
        let v = std::str::from_utf8(&b[s..p]).ok()?.parse::<u64>().ok()?;
        self.p = p;
        Some(v)
    }
    /// A REFERENCE (docs/SPEC-TABLES.md §16.7): a label the text has defined,
    /// alone, resolves against the labeled node's TABLE — another table is
    /// kind_mismatch, the pointer null; a label not yet defined, or one whose
    /// definition is still open, is malformed.
    pub fn refer<T: 'static>(&mut self, label: u64) -> Option<Rc<T>> {
        match self.labels.get(&label) {
            Some(JsonLabel::Node(n)) => match n.clone().downcast::<T>() {
                Ok(t) => Some(t),
                Err(_) => {
                    self.kind_mismatch += 1;
                    None
                }
            },
            // a definition the walk dropped reads null, and counts nothing
            // past what dropping it already counted (§16.7)
            Some(JsonLabel::Dropped) => None,
            _ => {
                self.malformed = true;
                None
            }
        }
    }
    pub fn skip_value(&mut self) {
        self.space();
        if self.p >= self.b.len() {
            self.malformed = true;
            return;
        }
        match self.b[self.p] {
            b'"' => {
                let _ = self.read_string();
            }
            b'{' => {
                if self.depth > JSON_DEPTH_CAP {
                    self.malformed = true;
                    return;
                }
                self.p += 1;
                self.depth += 1;
                loop {
                    self.space();
                    if self.p >= self.b.len() {
                        self.malformed = true;
                        self.depth -= 1;
                        return;
                    }
                    if self.b[self.p] == b'}' {
                        self.p += 1;
                        self.depth -= 1;
                        return;
                    }
                    let key = self.read_string();
                    if self.malformed {
                        self.depth -= 1;
                        return;
                    }
                    self.space();
                    if self.p < self.b.len() && self.b[self.p] == b':' {
                        self.p += 1;
                    }
                    // A DEFINITION INSIDE A SKIPPED VALUE (§16.7): an &node
                    // label followed by a field keeps its label as Dropped, so a
                    // reference to it reads null; an &node alone is a reference
                    // and defines nothing.
                    let mut labelled = false;
                    if key.as_deref() == Some("&node")
                        && let Some(label) = self.read_label()
                    {
                        labelled = true;
                        self.space();
                        if self.p < self.b.len() && self.b[self.p] == b',' {
                            self.labels.entry(label).or_insert(JsonLabel::Dropped);
                        }
                    }
                    if !labelled {
                        self.skip_value();
                    }
                    if self.malformed {
                        self.depth -= 1;
                        return;
                    }
                    self.space();
                    if self.p < self.b.len() && self.b[self.p] == b',' {
                        self.p += 1;
                    }
                }
            }
            b'[' => {
                if self.depth > JSON_DEPTH_CAP {
                    self.malformed = true;
                    return;
                }
                self.p += 1;
                self.depth += 1;
                loop {
                    self.space();
                    if self.p >= self.b.len() {
                        self.malformed = true;
                        self.depth -= 1;
                        return;
                    }
                    if self.b[self.p] == b']' {
                        self.p += 1;
                        self.depth -= 1;
                        return;
                    }
                    self.skip_value();
                    if self.malformed {
                        self.depth -= 1;
                        return;
                    }
                    self.space();
                    if self.p < self.b.len() && self.b[self.p] == b',' {
                        self.p += 1;
                    }
                }
            }
            b't' => {
                if self.b[self.p..].starts_with(b"true") {
                    self.p += 4;
                } else {
                    self.malformed = true;
                }
            }
            b'f' => {
                if self.b[self.p..].starts_with(b"false") {
                    self.p += 5;
                } else {
                    self.malformed = true;
                }
            }
            b'n' => {
                let _ = self.read_null();
            }
            _ => {
                let _ = self.number_token();
            }
        }
    }
}
`
}

// emitEnumText emits one enum's JSON name maps: variant -> schema name for the
// writer, schema name -> variant for the reader (docs/SPEC-TABLES.md §16.2, the
// enum row's "the variant NAME").
func emitEnumText(b *strings.Builder, e *ir.Enum) {
	write := fn(e.Name, "json_name")
	fmt.Fprintf(b, "#[allow(dead_code)]\n")
	fmt.Fprintf(b, "pub fn %s(v: %s) -> &'static str {\n", write, e.Name)
	fmt.Fprintf(b, "    match v {\n")
	fmt.Fprintf(b, "        %s::None => \"None\",\n", e.Name)
	for _, v := range e.Variants {
		fmt.Fprintf(b, "        %s::%s => %q,\n", e.Name, rustVariant(v), v)
	}
	fmt.Fprintf(b, "    }\n}\n\n")

	read := fn(e.Name, "from_json_name")
	fmt.Fprintf(b, "#[allow(dead_code)]\n")
	fmt.Fprintf(b, "pub fn %s(s: &str) -> Option<%s> {\n", read, e.Name)
	fmt.Fprintf(b, "    match s {\n")
	fmt.Fprintf(b, "        \"None\" => Some(%s::None),\n", e.Name)
	for _, v := range e.Variants {
		fmt.Fprintf(b, "        %q => Some(%s::%s),\n", v, e.Name, rustVariant(v))
	}
	fmt.Fprintf(b, "        _ => None,\n")
	fmt.Fprintf(b, "    }\n}\n\n")
}

// emitTextCount emits <name>_json_count: the writer's FIRST pass (§16.7), which
// counts how many slots name each node over the same walk the numbering takes,
// descending into a node at its first naming only.
func (g *gen) emitTextCount(b *strings.Builder, st *ir.Struct) {
	fmt.Fprintf(b, "#[allow(unused_variables, dead_code)]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, w: &mut Jw) {\n", fn(st.Name, "json_count"), st.Name)
	conds := guardConds(st, "v")
	for _, f := range st.Fields {
		x := "v." + ir.RustSnake(f.Name)
		// a field under a false guard has no key in the text (§4, §16.1), so
		// it names no node either
		guardedField(b, conds[f.Name], func(b *strings.Builder) { g.emitFieldTextCount(b, f, x, bodyIndent) })
	}
	fmt.Fprintf(b, "}\n\n")
}

func (g *gen) emitFieldTextCount(b *strings.Builder, f *ir.Field, x, ind string) {
	if f.Type.Optional {
		inner := *f
		inner.Type.Optional = false
		fmt.Fprintf(b, "%sif let Some(opt) = &%s {\n", ind, x)
		g.emitFieldTextCount(b, &inner, "(*opt)", ind+"    ")
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	switch classOf(f) {
	case vBlob:
		// A BLOB IS A STRING IN THE TEXT AND TAKES NO LABEL (§16.7): a blob
		// two slots name has no spelling this form can carry, so the writer
		// refuses the graph, as the oracle does
		fmt.Fprintf(b, "%sif let Some(n) = %s { let c = w.counts.entry(Rc::as_ptr(n) as *const u8 as usize).or_insert(0); *c += 1; if *c > 1 { w.refused = true; } }\n", ind, borrowOf(x))
	case vPointer:
		count := fmt.Sprintf("let c = w.counts.entry(Rc::as_ptr(n) as *const () as usize).or_insert(0); *c += 1; if *c == 1 { %s(n, w); }", fn(f.Type.Name, "json_count"))
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "%sfor n in %s.iter().flatten() { %s }\n", ind, x, count)
		} else {
			fmt.Fprintf(b, "%sif let Some(n) = %s { %s }\n", ind, borrowOf(x), count)
		}
	case vTable:
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "%sfor e in %s.iter() { %s(e, w); }\n", ind, x, fn(f.Type.Name, "json_count"))
		} else {
			fmt.Fprintf(b, "%s%s(%s, w);\n", ind, fn(f.Type.Name, "json_count"), borrowOf(x))
		}
	case vMap:
		fmt.Fprintf(b, "%sfor e in %s.iter() { %s(e, w); }\n", ind, x, fn(f.MapEntry.Name, "json_count"))
	case vUnion:
		un := unionOf(f)
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			fmt.Fprintf(b, "%sfor e in %s.iter() { %s(e, w); }\n", ind, x, fn(un.Name, "json_count"))
		} else {
			fmt.Fprintf(b, "%s%s(%s, w);\n", ind, fn(un.Name, "json_count"), borrowOf(x))
		}
	}
}

// emitTextWrite emits one table's JSON write: every field in declaration order,
// defaults included (docs/SPEC-TABLES.md §16.1), opened by `&node` when the
// node is a shared one's definition (§16.7).
func (g *gen) emitTextWrite(b *strings.Builder, st *ir.Struct) {
	write := fn(st.Name, "json_write")
	fmt.Fprintf(b, "#[allow(unused_variables, unused_mut, dead_code)]\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s, w: &mut Jw, depth: usize, label: u64) {\n", write, st.Name)
	fmt.Fprintf(b, "    if depth > JSON_DEPTH_CAP { w.refused = true; }\n")
	fmt.Fprintf(b, "    w.out.push('{');\n")
	fmt.Fprintf(b, "    let mut sep = false;\n")
	fmt.Fprintf(b, "    if label > 0 {\n")
	fmt.Fprintf(b, "        json_newline(&mut w.out, depth + 1);\n")
	fmt.Fprintf(b, "        w.out.push_str(\"\\\"&node\\\": \");\n")
	fmt.Fprintf(b, "        w.out.push_str(&label.to_string());\n")
	fmt.Fprintf(b, "        sep = true;\n")
	fmt.Fprintf(b, "    }\n")
	conds := guardConds(st, "v")
	for _, f := range st.Fields {
		// a field under a false guard is not written (§4, §16.1): no key
		guardedField(b, conds[f.Name], func(b *strings.Builder) { g.emitFieldTextWrite(b, f) })
	}
	fmt.Fprintf(b, "    if sep {\n")
	fmt.Fprintf(b, "        json_newline(&mut w.out, depth);\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "    w.out.push('}');\n")
	fmt.Fprintf(b, "}\n\n")
}

// emitFieldTextWrite emits one field's key and value in the JSON write.
func (g *gen) emitFieldTextWrite(b *strings.Builder, f *ir.Field) {
	if f.Type.Optional {
		inner := *f
		inner.Type.Optional = false
		fmt.Fprintf(b, "    if let Some(opt) = &v.%s {\n", ir.RustSnake(f.Name))
		fmt.Fprintf(b, "        if sep { w.out.push(','); }\n")
		fmt.Fprintf(b, "        sep = true;\n")
		fmt.Fprintf(b, "        json_newline(&mut w.out, depth + 1);\n")
		fmt.Fprintf(b, "        json_escape(%q, &mut w.out);\n", g.fieldKey(f))
		fmt.Fprintf(b, "        w.out.push_str(\": \");\n")
		g.emitTextWriteValue(b, &inner, "(*opt)", "depth + 1", "        ")
		fmt.Fprintf(b, "    }\n")
		return
	}
	fmt.Fprintf(b, "    if sep { w.out.push(','); }\n")
	fmt.Fprintf(b, "    sep = true;\n")
	fmt.Fprintf(b, "    json_newline(&mut w.out, depth + 1);\n")
	fmt.Fprintf(b, "    json_escape(%q, &mut w.out);\n", g.fieldKey(f))
	fmt.Fprintf(b, "    w.out.push_str(\": \");\n")
	g.emitTextWriteValue(b, f, "v."+ir.RustSnake(f.Name), "depth + 1", "    ")
}

func (g *gen) fieldKey(f *ir.Field) string {
	if f.JsonKey != "" {
		return f.JsonKey
	}
	return f.Name
}

// emitTextWriteValue emits the value writer for one field (or, for an array,
// the array), the value's closing brace or bracket at depth `d`.
func (g *gen) emitTextWriteValue(b *strings.Builder, f *ir.Field, x, d, ind string) {
	if f.KeyEnum != "" && f.KeyEnumRef != nil {
		g.emitKeyedTextWrite(b, f, x, d, ind)
		return
	}
	if f.Array != ir.ArrayNone {
		fmt.Fprintf(b, "%sif %s.is_empty() {\n", ind, x)
		fmt.Fprintf(b, "%s    w.out.push_str(\"[]\");\n", ind)
		fmt.Fprintf(b, "%s} else {\n", ind)
		fmt.Fprintf(b, "%s    w.out.push('[');\n", ind)
		fmt.Fprintf(b, "%s    for (i, e) in %s.iter().enumerate() {\n", ind, x)
		fmt.Fprintf(b, "%s        if i > 0 { w.out.push(','); }\n", ind)
		fmt.Fprintf(b, "%s        json_newline(&mut w.out, %s + 1);\n", ind, d)
		g.emitTextWriteOne(b, f, "(*e)", d+" + 1", ind+"        ")
		fmt.Fprintf(b, "%s    }\n", ind)
		fmt.Fprintf(b, "%s    json_newline(&mut w.out, %s);\n", ind, d)
		fmt.Fprintf(b, "%s    w.out.push(']');\n", ind)
		fmt.Fprintf(b, "%s}\n", ind)
		return
	}
	g.emitTextWriteOne(b, f, x, d, ind)
}

// emitTextWriteOne emits the writer of ONE value of the field.
func (g *gen) emitTextWriteOne(b *strings.Builder, f *ir.Field, x, d, ind string) {
	switch classOf(f) {
	case vBlob:
		fmt.Fprintf(b, "%smatch %s {\n", ind, borrowOf(x))
		if f.Type.Kind == ir.TString {
			fmt.Fprintf(b, "%s    Some(s) => json_escape(s, &mut w.out),\n", ind)
		} else {
			fmt.Fprintf(b, "%s    Some(s) => json_b64(s, &mut w.out),\n", ind)
		}
		fmt.Fprintf(b, "%s    None => w.out.push_str(\"null\"),\n", ind)
		fmt.Fprintf(b, "%s}\n", ind)
	case vPointer:
		// a node named once is its object in place; a node named more than
		// once is DEFINED at its first write, `&node` first, and NAMED by
		// `&node` alone after (§16.7)
		fmt.Fprintf(b, "%smatch %s {\n", ind, borrowOf(x))
		fmt.Fprintf(b, "%s    None => w.out.push_str(\"null\"),\n", ind)
		fmt.Fprintf(b, "%s    Some(n) => {\n", ind)
		fmt.Fprintf(b, "%s        let key = Rc::as_ptr(n) as *const () as usize;\n", ind)
		fmt.Fprintf(b, "%s        if w.counts.get(&key).copied().unwrap_or(0) > 1 {\n", ind)
		fmt.Fprintf(b, "%s            if let Some(l) = w.labels.get(&key).copied() {\n", ind)
		fmt.Fprintf(b, "%s                w.out.push('{');\n", ind)
		fmt.Fprintf(b, "%s                json_newline(&mut w.out, %s + 1);\n", ind, d)
		fmt.Fprintf(b, "%s                w.out.push_str(\"\\\"&node\\\": \");\n", ind)
		fmt.Fprintf(b, "%s                w.out.push_str(&l.to_string());\n", ind)
		fmt.Fprintf(b, "%s                json_newline(&mut w.out, %s);\n", ind, d)
		fmt.Fprintf(b, "%s                w.out.push('}');\n", ind)
		fmt.Fprintf(b, "%s            } else {\n", ind)
		fmt.Fprintf(b, "%s                w.next += 1;\n", ind)
		fmt.Fprintf(b, "%s                let l = w.next;\n", ind)
		fmt.Fprintf(b, "%s                w.labels.insert(key, l);\n", ind)
		fmt.Fprintf(b, "%s                %s(n, w, %s, l);\n", ind, fn(f.Type.Name, "json_write"), d)
		fmt.Fprintf(b, "%s            }\n", ind)
		fmt.Fprintf(b, "%s        } else {\n", ind)
		fmt.Fprintf(b, "%s            %s(n, w, %s, 0);\n", ind, fn(f.Type.Name, "json_write"), d)
		fmt.Fprintf(b, "%s        }\n", ind)
		fmt.Fprintf(b, "%s    }\n", ind)
		fmt.Fprintf(b, "%s}\n", ind)
	case vTable:
		fmt.Fprintf(b, "%s%s(%s, w, %s, 0);\n", ind, fn(f.Type.Name, "json_write"), borrowOf(x), d)
	case vMap:
		g.emitMapTextWrite(b, f, x, d, ind)
	case vUnion:
		un := unionOf(f)
		fmt.Fprintf(b, "%s%s(%s, w, %s);\n", ind, fn(un.Name, "json_write"), borrowOf(x), d)
	case vString:
		fmt.Fprintf(b, "%sjson_escape(%s, &mut w.out);\n", ind, borrowOf(x))
	case vWString:
		// the same text, TRANSCODED at the boundary (docs/SPEC-TABLES.md
		// §16.2's wstring row): a stored value is paired, so nothing is lost
		fmt.Fprintf(b, "%sjson_escape(&String::from_utf16_lossy(%s), &mut w.out);\n", ind, borrowOf(x))
	case vBytes:
		fmt.Fprintf(b, "%sjson_b64(%s, &mut w.out);\n", ind, borrowOf(x))
	case vEnum:
		fmt.Fprintf(b, "%sjson_escape(%s(%s), &mut w.out);\n", ind, fn(f.Type.Name, "json_name"), unparen(x))
	default:
		if fl := isFlags(f); fl != nil {
			// a FLAGS MASK is the array of its set bits' names, in bit order
			// (docs/SPEC-TABLES.md §16.2); a bit no variant names has no text
			// spelling, so the writer refuses it as the oracle does
			fmt.Fprintf(b, "%s{\n", ind)
			fmt.Fprintf(b, "%s    const NAMES: [&str; %d] = [", ind, len(fl.Variants))
			for i, v := range fl.Variants {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(b, "%q", v)
			}
			b.WriteString("];\n")
			fmt.Fprintf(b, "%s    let bits: u64 = %s;\n", ind, unparen(x))
			fmt.Fprintf(b, "%s    if bits == 0 { w.out.push_str(\"[]\"); } else {\n", ind)
			fmt.Fprintf(b, "%s        w.out.push('[');\n", ind)
			fmt.Fprintf(b, "%s        let mut first = true;\n", ind)
			fmt.Fprintf(b, "%s        for (bit, name) in NAMES.iter().enumerate() {\n", ind)
			fmt.Fprintf(b, "%s            if bits & (1u64 << bit) == 0 { continue; }\n", ind)
			fmt.Fprintf(b, "%s            if !first { w.out.push(','); }\n", ind)
			fmt.Fprintf(b, "%s            first = false;\n", ind)
			fmt.Fprintf(b, "%s            json_newline(&mut w.out, %s + 1);\n", ind, d)
			fmt.Fprintf(b, "%s            json_escape(name, &mut w.out);\n", ind)
			fmt.Fprintf(b, "%s        }\n", ind)
			if len(fl.Variants) < 64 {
				// a set bit past the named ones has no spelling: refused
				fmt.Fprintf(b, "%s        if bits >> %d != 0 { w.refused = true; }\n", ind, len(fl.Variants))
			}
			fmt.Fprintf(b, "%s        json_newline(&mut w.out, %s);\n", ind, d)
			fmt.Fprintf(b, "%s        w.out.push(']');\n", ind)
			fmt.Fprintf(b, "%s    }\n", ind)
			fmt.Fprintf(b, "%s}\n", ind)
			return
		}
		switch f.Type.Kind {
		case ir.TBool:
			fmt.Fprintf(b, "%sw.out.push_str(if %s { \"true\" } else { \"false\" });\n", ind, x)
		case ir.TFloat32:
			fmt.Fprintf(b, "%sjson_float(%s as f64, true, w);\n", ind, x)
		case ir.TFloat64:
			fmt.Fprintf(b, "%sjson_float(%s, false, w);\n", ind, x)
		case ir.TFixed:
			// WHOLE UNITS, exact, from the raw integer (§16.2)
			fmt.Fprintf(b, "%sjson_fixed(&mut w.out, %s, %v, %d);\n", ind, wideRaw(f, x), f.Type.Signed, f.Type.FracBits)
		default:
			fmt.Fprintf(b, "%sw.out.push_str(&%s.to_string());\n", ind, x)
		}
	}
}

// textIntRange is the range an integer's text value clamps to (§16.2): the
// declared [min, max], else the implied one — bits(N)'s [0, 2^N − 1] — else
// the storage's own.
func textIntRange(f *ir.Field) (lo, hi string) {
	if l, h, ok := intRange(f); ok {
		return l, h
	}
	width := f.Type.Width
	if f.Type.Kind == ir.TBits {
		return "0", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1)).String()
	}
	if f.Type.Signed {
		lo := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), uint(width-1)))
		hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width-1)), big.NewInt(1))
		return lo.String(), hi.String()
	}
	return "0", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(width)), big.NewInt(1)).String()
}

// emitTextRead emits one table's JSON read: the tolerant object scan that
// places every key it can name and skips the rest (docs/SPEC-TABLES.md §16.2),
// as two functions — <name>_json_read, which opens the object, and
// <name>_json_fields, which reads its keys and is what a pointer's object
// continues with after its `&node` label (§16.7).
func (g *gen) emitTextRead(b *strings.Builder, st *ir.Struct) {
	read := fn(st.Name, "json_read")
	fields := fn(st.Name, "json_fields")
	fmt.Fprintf(b, "#[allow(dead_code)]\n")
	fmt.Fprintf(b, "pub fn %s(jr: &mut Jr, v: &mut %s) {\n", read, st.Name)
	fmt.Fprintf(b, "    jr.space();\n")
	fmt.Fprintf(b, "    if jr.peek() != b'{' { jr.malformed = true; return; }\n")
	fmt.Fprintf(b, "    if jr.depth > JSON_DEPTH_CAP { jr.malformed = true; return; }\n")
	fmt.Fprintf(b, "    jr.p += 1;\n")
	fmt.Fprintf(b, "    jr.depth += 1;\n")
	fmt.Fprintf(b, "    let _ = %s(jr, v, None);\n", fields)
	fmt.Fprintf(b, "    jr.depth -= 1;\n")
	fmt.Fprintf(b, "}\n\n")

	fmt.Fprintf(b, "/// The keys of one %s object, the opening brace already read; `first` is a\n", st.Name)
	fmt.Fprintf(b, "/// key the caller already read. Answers how many keys it read.\n")
	fmt.Fprintf(b, "#[allow(unused_variables, unused_mut, dead_code)]\n")
	fmt.Fprintf(b, "pub fn %s(jr: &mut Jr, v: &mut %s, first: Option<String>) -> usize {\n", fields, st.Name)
	fmt.Fprintf(b, "    let mut pending = first;\n")
	fmt.Fprintf(b, "    let mut n = 0usize;\n")
	fmt.Fprintf(b, "    let mut seen = [false; %d];\n", len(st.Fields))
	fmt.Fprintf(b, "    loop {\n")
	fmt.Fprintf(b, "        let key = match pending.take() {\n")
	fmt.Fprintf(b, "            Some(k) => k,\n")
	fmt.Fprintf(b, "            None => {\n")
	fmt.Fprintf(b, "                jr.space();\n")
	fmt.Fprintf(b, "                if jr.peek() == b'}' { jr.p += 1; return n; }\n")
	fmt.Fprintf(b, "                let Some(k) = jr.read_string() else { jr.malformed = true; return n; };\n")
	fmt.Fprintf(b, "                jr.space();\n")
	fmt.Fprintf(b, "                if jr.peek() != b':' { jr.malformed = true; return n; }\n")
	fmt.Fprintf(b, "                jr.p += 1;\n")
	fmt.Fprintf(b, "                k\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        };\n")
	fmt.Fprintf(b, "        n += 1;\n")
	fmt.Fprintf(b, "        match key.as_str() {\n")
	for i, f := range st.Fields {
		g.emitTextReadField(b, i, f, "v."+ir.RustSnake(f.Name), "            ")
	}
	fmt.Fprintf(b, "            _ => {\n")
	fmt.Fprintf(b, "                // a `&`-prefixed key among the keys a reader PLACES is malformed,\n")
	fmt.Fprintf(b, "                // never unknown: it is a construct this position cannot honor (§16.7)\n")
	fmt.Fprintf(b, "                if key.starts_with('&') { jr.malformed = true; return n; }\n")
	fmt.Fprintf(b, "                jr.unknown += 1;\n")
	fmt.Fprintf(b, "                jr.skip_value();\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        if jr.malformed { return n; }\n")
	fmt.Fprintf(b, "        jr.space();\n")
	fmt.Fprintf(b, "        let c = jr.peek();\n")
	fmt.Fprintf(b, "        if c == b',' { jr.p += 1; continue; }\n")
	fmt.Fprintf(b, "        if c == b'}' { jr.p += 1; return n; }\n")
	fmt.Fprintf(b, "        jr.malformed = true;\n")
	fmt.Fprintf(b, "        return n;\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "}\n\n")
}

// emitTextReadPointer emits <name>_json_ptr: one pointer slot's value (§16.2,
// §16.7) — null, the pointee's object in place, a DEFINITION (`&node` first,
// then at least one field), or a REFERENCE (`&node` alone).
func (g *gen) emitTextReadPointer(b *strings.Builder, st *ir.Struct) {
	fmt.Fprintf(b, "#[allow(dead_code)]\n")
	fmt.Fprintf(b, "pub fn %s(jr: &mut Jr) -> Option<Rc<%s>> {\n", fn(st.Name, "json_ptr"), st.Name)
	fmt.Fprintf(b, "    match jr.peek() {\n")
	fmt.Fprintf(b, "        b'n' => { jr.read_null(); None }\n")
	fmt.Fprintf(b, "        b'{' => {\n")
	fmt.Fprintf(b, "            if jr.depth > JSON_DEPTH_CAP { jr.malformed = true; return None; }\n")
	fmt.Fprintf(b, "            jr.p += 1;\n")
	fmt.Fprintf(b, "            if jr.peek() == b'}' { jr.p += 1; return Some(Rc::new(%s::default())); }\n", st.Name)
	fmt.Fprintf(b, "            let Some(key) = jr.read_string() else { jr.malformed = true; return None; };\n")
	fmt.Fprintf(b, "            if jr.peek() != b':' { jr.malformed = true; return None; }\n")
	fmt.Fprintf(b, "            jr.p += 1;\n")
	fmt.Fprintf(b, "            if key != \"&node\" {\n")
	fmt.Fprintf(b, "                let mut v = %s::default();\n", st.Name)
	fmt.Fprintf(b, "                jr.depth += 1;\n")
	fmt.Fprintf(b, "                let _ = %s(jr, &mut v, Some(key));\n", fn(st.Name, "json_fields"))
	fmt.Fprintf(b, "                jr.depth -= 1;\n")
	fmt.Fprintf(b, "                return Some(Rc::new(v));\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "            let Some(label) = jr.read_label() else { jr.malformed = true; return None; };\n")
	fmt.Fprintf(b, "            match jr.peek() {\n")
	fmt.Fprintf(b, "                b'}' => { jr.p += 1; jr.refer::<%s>(label) }\n", st.Name)
	fmt.Fprintf(b, "                b',' => {\n")
	fmt.Fprintf(b, "                    jr.p += 1;\n")
	fmt.Fprintf(b, "                    // a label already defined, or still open, takes no second definition\n")
	fmt.Fprintf(b, "                    if jr.labels.contains_key(&label) { jr.malformed = true; return None; }\n")
	fmt.Fprintf(b, "                    jr.labels.insert(label, JsonLabel::Open);\n")
	fmt.Fprintf(b, "                    let mut v = %s::default();\n", st.Name)
	fmt.Fprintf(b, "                    // a definition carries at least one field\n")
	fmt.Fprintf(b, "                    jr.depth += 1;\n")
	fmt.Fprintf(b, "                    let count = %s(jr, &mut v, None);\n", fn(st.Name, "json_fields"))
	fmt.Fprintf(b, "                    jr.depth -= 1;\n")
	fmt.Fprintf(b, "                    if count == 0 { jr.malformed = true; return None; }\n")
	fmt.Fprintf(b, "                    let n = Rc::new(v);\n")
	fmt.Fprintf(b, "                    jr.labels.insert(label, JsonLabel::Node(n.clone()));\n")
	fmt.Fprintf(b, "                    Some(n)\n")
	fmt.Fprintf(b, "                }\n")
	fmt.Fprintf(b, "                _ => { jr.malformed = true; None }\n")
	fmt.Fprintf(b, "            }\n")
	fmt.Fprintf(b, "        }\n")
	fmt.Fprintf(b, "        _ => { jr.kind_mismatch += 1; jr.skip_value(); None }\n")
	fmt.Fprintf(b, "    }\n")
	fmt.Fprintf(b, "}\n\n")
}

// emitTextReadField emits one field's decode arm inside the read match.
func (g *gen) emitTextReadField(b *strings.Builder, i int, f *ir.Field, field, ind string) {
	fmt.Fprintf(b, "%s%q => {\n", ind, g.fieldKey(f))
	fmt.Fprintf(b, "%s    if seen[%d] { jr.duplicate += 1; }\n", ind, i)
	fmt.Fprintf(b, "%s    seen[%d] = true;\n", ind, i)
	g.emitTextReadValue(b, f, field, ind+"    ")
	fmt.Fprintf(b, "%s}\n", ind)
}

func (g *gen) emitTextReadValue(b *strings.Builder, f *ir.Field, field, in string) {
	if f.Type.Optional {
		inner := *f
		inner.Type.Optional = false
		fmt.Fprintf(b, "%sif jr.peek() == b'n' { jr.read_null(); %s = None; }\n", in, field)
		fmt.Fprintf(b, "%selse {\n", in)
		fmt.Fprintf(b, "%s    let mut tmp = %s;\n", in, fieldDefault(&inner))
		g.emitTextReadValue(b, &inner, "tmp", in+"    ")
		fmt.Fprintf(b, "%s    if !jr.malformed { %s = Some(tmp); }\n", in, field)
		fmt.Fprintf(b, "%s}\n", in)
		return
	}
	if f.KeyEnum != "" && f.KeyEnumRef != nil {
		g.emitKeyedTextRead(b, f, field, in)
		return
	}
	if f.Array != ir.ArrayNone {
		// an array (§16.2): `[]` is empty and `null` is kind_mismatch; a
		// bounded [..N]T drops what is past N and counts it; a repeated key
		// replaces the array outright
		fmt.Fprintf(b, "%sif jr.peek() == b'[' {\n", in)
		fmt.Fprintf(b, "%s    jr.p += 1;\n", in)
		fmt.Fprintf(b, "%s    %s.clear();\n", in, field)
		fmt.Fprintf(b, "%s    loop {\n", in)
		fmt.Fprintf(b, "%s        if jr.peek() == b']' { jr.p += 1; break; }\n", in)
		elem := in + "        "
		if f.Array == ir.ArrayCounted || f.Array == ir.ArrayFixed {
			if f.ArrayBound == 1 {
				fmt.Fprintf(b, "%sif !%s.is_empty() {\n", elem, field)
			} else {
				fmt.Fprintf(b, "%sif %s.len() >= %d {\n", elem, field, f.ArrayBound)
			}
			fmt.Fprintf(b, "%s    jr.clamped += 1;\n", elem)
			fmt.Fprintf(b, "%s    jr.skip_value();\n", elem)
			fmt.Fprintf(b, "%s} else {\n", elem)
			g.emitTextReadElem(b, f, field, elem+"    ")
			fmt.Fprintf(b, "%s}\n", elem)
		} else {
			g.emitTextReadElem(b, f, field, elem)
		}
		fmt.Fprintf(b, "%s        if jr.malformed { break; }\n", in)
		fmt.Fprintf(b, "%s        let c = jr.peek();\n", in)
		fmt.Fprintf(b, "%s        if c == b',' { jr.p += 1; continue; }\n", in)
		fmt.Fprintf(b, "%s        if c == b']' { jr.p += 1; break; }\n", in)
		fmt.Fprintf(b, "%s        jr.malformed = true; break;\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
		if f.Array == ir.ArrayFixed {
			fmt.Fprintf(b, "%s    while %s.len() < %d { %s.push(%s); }\n", in, field, f.ArrayBound, field, fieldDefault(scalarOnly(f)))
		}
		fmt.Fprintf(b, "%s} else { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
		return
	}
	switch classOf(f) {
	case vPointer:
		fmt.Fprintf(b, "%s%s = %s(jr);\n", in, field, fn(f.Type.Name, "json_ptr"))
	case vTable:
		fmt.Fprintf(b, "%sif jr.peek() == b'{' { %s = %s::default(); %s(jr, &mut %s); }\n", in, field, f.Type.Name, fn(f.Type.Name, "json_read"), field)
		fmt.Fprintf(b, "%selse { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
	case vMap:
		g.emitMapTextRead(b, f, field, in)
	case vUnion:
		un := unionOf(f)
		fmt.Fprintf(b, "%s%s(jr, &mut %s);\n", in, fn(un.Name, "json_read"), field)
	default:
		g.emitTextReadScalar(b, f, field, in)
	}
}

// emitTextReadElem emits the read of one array element, pushed in place.
func (g *gen) emitTextReadElem(b *strings.Builder, f *ir.Field, field, ind string) {
	switch classOf(f) {
	case vPointer:
		fmt.Fprintf(b, "%slet e = %s(jr);\n", ind, fn(f.Type.Name, "json_ptr"))
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vTable:
		fmt.Fprintf(b, "%slet mut e = %s::default();\n", ind, f.Type.Name)
		fmt.Fprintf(b, "%sif jr.peek() == b'{' { %s(jr, &mut e); } else { jr.kind_mismatch += 1; jr.skip_value(); }\n", ind, fn(f.Type.Name, "json_read"))
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	case vUnion:
		un := unionOf(f)
		fmt.Fprintf(b, "%slet mut e = %s::None;\n", ind, un.Name)
		fmt.Fprintf(b, "%s%s(jr, &mut e);\n", ind, fn(un.Name, "json_read"))
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	default:
		fmt.Fprintf(b, "%slet mut e = %s;\n", ind, fieldDefault(scalarOnly(f)))
		g.emitTextReadScalar(b, f, "e", ind)
		fmt.Fprintf(b, "%s%s.push(e);\n", ind, field)
	}
}

// scalarOnly is the element of an array field, as a field of its own.
func scalarOnly(f *ir.Field) *ir.Field {
	e := *f
	e.Type.Optional = false
	e.KeyEnum = ""
	e.KeyEnumRef = nil
	e.Array = ir.ArrayNone
	e.ArrayBound = 0
	e.HasDefault = false
	return &e
}

// emitTextReadScalar emits the read of one non-table, non-pointer value into
// `dst` (§16.2's rows): a value of the wrong JSON type is kind_mismatch,
// skipped, never coerced.
func (g *gen) emitTextReadScalar(b *strings.Builder, f *ir.Field, dst, in string) {
	switch classOf(f) {
	case vBlob:
		fmt.Fprintf(b, "%sif jr.peek() == b'n' { jr.read_null(); %s = None; }\n", in, dst)
		fmt.Fprintf(b, "%selse if jr.peek() == b'\"' {\n", in)
		if f.Type.Kind == ir.TString {
			fmt.Fprintf(b, "%s    if let Some(s) = jr.read_text() { %s = Some(Rc::from(s)); }\n", in, dst)
		} else {
			fmt.Fprintf(b, "%s    if let Some(s) = jr.read_string() {\n", in)
			fmt.Fprintf(b, "%s        match json_unb64(&s) { Some(d) => %s = Some(Rc::from(d)), None => { jr.kind_mismatch += 1; %s = None; } }\n", in, dst, dst)
			fmt.Fprintf(b, "%s    }\n", in)
		}
		fmt.Fprintf(b, "%s} else { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
	case vString:
		fmt.Fprintf(b, "%sif jr.peek() == b'\"' { if let Some(s) = jr.read_text() { %s = json_clamp(s, %d, &mut jr.clamped); } }\n", in, dst, f.Type.Size)
		fmt.Fprintf(b, "%selse { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
	case vWString:
		fmt.Fprintf(b, "%sif jr.peek() == b'\"' { if let Some(s) = jr.read_text() { %s = json_wide_clamp(&s, %d, &mut jr.clamped); } }\n", in, dst, f.Type.Size)
		fmt.Fprintf(b, "%selse { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
	case vBytes:
		fmt.Fprintf(b, "%sif jr.peek() == b'\"' {\n", in)
		fmt.Fprintf(b, "%s    if let Some(s) = jr.read_string() {\n", in)
		fmt.Fprintf(b, "%s        match json_unb64(&s) {\n", in)
		fmt.Fprintf(b, "%s            Some(mut d) => { if d.len() > %d { d.truncate(%d); jr.clamped += 1; } %s = d; }\n", in, f.Type.Size, f.Type.Size, dst)
		fmt.Fprintf(b, "%s            None => { jr.kind_mismatch += 1; }\n", in)
		fmt.Fprintf(b, "%s        }\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
		fmt.Fprintf(b, "%s} else { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
	case vEnum:
		e := f.Type.Ref.(*ir.Enum)
		fmt.Fprintf(b, "%sif jr.peek() == b'\"' {\n", in)
		fmt.Fprintf(b, "%s    if let Some(s) = jr.read_string() {\n", in)
		fmt.Fprintf(b, "%s        %s = match %s(&s) {\n", in, dst, fn(e.Name, "from_json_name"))
		fmt.Fprintf(b, "%s            Some(x) => x,\n", in)
		fmt.Fprintf(b, "%s            None => { jr.unknown += 1; %s::None },\n", in, e.Name)
		fmt.Fprintf(b, "%s        };\n", in)
		fmt.Fprintf(b, "%s    }\n", in)
		fmt.Fprintf(b, "%s} else { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
	default:
		if fl := isFlags(f); fl != nil {
			// a FLAGS MASK reads from the array of its bits' names
			// (docs/SPEC-TABLES.md §16.2): a name no variant spells counts
			// unknown, an element that is not a string counts kind_mismatch
			fmt.Fprintf(b, "%sif jr.peek() == b'[' {\n", in)
			fmt.Fprintf(b, "%s    jr.p += 1;\n", in)
			fmt.Fprintf(b, "%s    let mut bits: u64 = 0;\n", in)
			fmt.Fprintf(b, "%s    loop {\n", in)
			fmt.Fprintf(b, "%s        if jr.peek() == b']' { jr.p += 1; break; }\n", in)
			fmt.Fprintf(b, "%s        if jr.peek() == b'\"' {\n", in)
			fmt.Fprintf(b, "%s            match jr.read_string() {\n", in)
			fmt.Fprintf(b, "%s                Some(s) => match s.as_str() {\n", in)
			for i, v := range fl.Variants {
				fmt.Fprintf(b, "%s                    %q => bits |= 1u64 << %d,\n", in, v, i)
			}
			fmt.Fprintf(b, "%s                    _ => jr.unknown += 1,\n", in)
			fmt.Fprintf(b, "%s                },\n", in)
			fmt.Fprintf(b, "%s                None => break,\n", in)
			fmt.Fprintf(b, "%s            }\n", in)
			fmt.Fprintf(b, "%s        } else { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
			fmt.Fprintf(b, "%s        if jr.peek() == b',' { jr.p += 1; continue; }\n", in)
			fmt.Fprintf(b, "%s        if jr.peek() == b']' { jr.p += 1; break; }\n", in)
			fmt.Fprintf(b, "%s        jr.malformed = true; break;\n", in)
			fmt.Fprintf(b, "%s    }\n", in)
			fmt.Fprintf(b, "%s    %s = bits;\n", in, dst)
			fmt.Fprintf(b, "%s} else { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
			return
		}
		switch f.Type.Kind {
		case ir.TBool:
			fmt.Fprintf(b, "%sif jr.peek() == b't' && jr.b[jr.p..].starts_with(b\"true\") { jr.p += 4; %s = true; }\n", in, dst)
			fmt.Fprintf(b, "%selse if jr.peek() == b'f' && jr.b[jr.p..].starts_with(b\"false\") { jr.p += 5; %s = false; }\n", in, dst)
			fmt.Fprintf(b, "%selse { jr.kind_mismatch += 1; jr.skip_value(); }\n", in)
		case ir.TFloat32:
			// a value a float32 field cannot hold is kind_mismatch, never
			// stored as infinity (§16.2)
			fmt.Fprintf(b, "%sif let Some(x) = jr.read_f32() {\n", in)
			fmt.Fprintf(b, "%s    if x.is_finite() { %s = x; } else { jr.kind_mismatch += 1; }\n", in, dst)
			fmt.Fprintf(b, "%s}\n", in)
		case ir.TFloat64:
			fmt.Fprintf(b, "%sif let Some(x) = jr.read_float() {\n", in)
			fmt.Fprintf(b, "%s    if x.is_finite() { %s = x; } else { jr.kind_mismatch += 1; }\n", in, dst)
			fmt.Fprintf(b, "%s}\n", in)
		default:
			if f.Type.Kind == ir.TFixed || f.Type.Width == 128 {
				emitTextReadWide(b, f, dst, in)
				return
			}
			lo, hi := textIntRange(f)
			fmt.Fprintf(b, "%sif let Some(n) = jr.read_int() {\n", in)
			fmt.Fprintf(b, "%s    let mut x = n;\n", in)
			fmt.Fprintf(b, "%s    if x < %si128 { x = %si128; jr.clamped += 1; } else if x > %si128 { x = %si128; jr.clamped += 1; }\n", in, lo, lo, hi, hi)
			fmt.Fprintf(b, "%s    %s = x as %s;\n", in, dst, scalarRust(f))
			fmt.Fprintf(b, "%s}\n", in)
		}
	}
}

// emitTextRoots emits the public roots: <name>_to_json, <name>_to_json_checked
// and <name>_from_json (docs/SPEC-TABLES.md §16.1, §16.7).
func (g *gen) emitTextRoots(b *strings.Builder, st *ir.Struct) {
	write := fn(st.Name, "to_json")
	fmt.Fprintf(b, "fn %s(v: &%s) -> Jw {\n", fn(st.Name, "to_json_both"), st.Name)
	fmt.Fprintf(b, "    let mut w = Jw { out: String::new(), counts: HashMap::new(), labels: HashMap::new(), next: 0, refused: false };\n")
	fmt.Fprintf(b, "    %s(v, &mut w);\n", fn(st.Name, "json_count"))
	fmt.Fprintf(b, "    %s(v, &mut w, 0, 0);\n", fn(st.Name, "json_write"))
	fmt.Fprintf(b, "    w.out.push('\\n');\n")
	fmt.Fprintf(b, "    w\n")
	fmt.Fprintf(b, "}\n\n")
	fmt.Fprintf(b, "/// %s — one %s as one pretty-printed text (docs/SPEC-TABLES.md §16.1):\n", write, st.Name)
	fmt.Fprintf(b, "/// TWO passes over one walk, so a node's first write knows whether it will\n")
	fmt.Fprintf(b, "/// be named again and takes its `&node` label there (§16.7).\n")
	fmt.Fprintf(b, "pub fn %s(v: &%s) -> String {\n", write, st.Name)
	fmt.Fprintf(b, "    %s(v).out\n", fn(st.Name, "to_json_both"))
	fmt.Fprintf(b, "}\n\n")
	fmt.Fprintf(b, "/// %s_checked — the same text, or None for a value the form cannot spell:\n", write)
	fmt.Fprintf(b, "/// a graph nested past the depth cap, or a float with no JSON spelling\n")
	fmt.Fprintf(b, "/// (docs/SPEC-TABLES.md §16.3, §16.7).\n")
	fmt.Fprintf(b, "pub fn %s_checked(v: &%s) -> Option<String> {\n", write, st.Name)
	fmt.Fprintf(b, "    let w = %s(v);\n", fn(st.Name, "to_json_both"))
	fmt.Fprintf(b, "    if w.refused { None } else { Some(w.out) }\n")
	fmt.Fprintf(b, "}\n\n")

	read := fn(st.Name, "from_json")
	readBytes := fn(st.Name, "from_json_bytes")
	fmt.Fprintf(b, "/// %s — the tolerant read of one text into one %s (docs/SPEC-TABLES.md §16.2).\n", read, st.Name)
	fmt.Fprintf(b, "pub fn %s(text: &str, report: &mut TableReport) -> Option<%s> {\n", read, st.Name)
	fmt.Fprintf(b, "    %s(text.as_bytes(), report)\n", readBytes)
	fmt.Fprintf(b, "}\n\n")
	fmt.Fprintf(b, "/// %s — the same read over the text's raw bytes, which a `&str` cannot\n", readBytes)
	fmt.Fprintf(b, "/// hold ill-formed: bytes that are not well-formed UTF-8 refuse the read\n")
	fmt.Fprintf(b, "/// (string_not_utf8, docs/SPEC-TABLES.md §16.3) rather than reading as U+FFFD.\n")
	fmt.Fprintf(b, "pub fn %s(text: &[u8], report: &mut TableReport) -> Option<%s> {\n", readBytes, st.Name)
	fmt.Fprintf(b, "    let mut jr = Jr::new(text);\n")
	fmt.Fprintf(b, "    let mut v = %s::default();\n", st.Name)
	fmt.Fprintf(b, "    %s(&mut jr, &mut v);\n", fn(st.Name, "json_read"))
	fmt.Fprintf(b, "    jr.space();\n")
	fmt.Fprintf(b, "    if jr.p != jr.b.len() { jr.malformed = true; }\n")
	fmt.Fprintf(b, "    report.unknown += jr.unknown;\n")
	fmt.Fprintf(b, "    report.kind_mismatch += jr.kind_mismatch;\n")
	fmt.Fprintf(b, "    report.clamped += jr.clamped;\n")
	fmt.Fprintf(b, "    report.duplicate += jr.duplicate;\n")
	fmt.Fprintf(b, "    report.malformed |= jr.malformed;\n")
	fmt.Fprintf(b, "    if jr.malformed { None } else { Some(v) }\n")
	fmt.Fprintf(b, "}\n\n")
}

// borrowOf renders a shared borrow of expression x: a dereferenced reference
// `(*r)` borrows as r itself (clippy's borrow_deref_ref), anything else as &x.
func borrowOf(x string) string {
	if strings.HasPrefix(x, "(*") && strings.HasSuffix(x, ")") && !strings.ContainsAny(x[2:len(x)-1], "() ") {
		return x[2 : len(x)-1]
	}
	return "&" + x
}

// unparen strips one pair of parentheses around a dereference, `(*e)` -> `*e`,
// where the expression stands alone (an argument, a let's value) and the
// parentheses are noise (clippy's double_parens, rustc's unused_parens).
func unparen(x string) string {
	if strings.HasPrefix(x, "(*") && strings.HasSuffix(x, ")") && !strings.ContainsAny(x[2:len(x)-1], "() ") {
		return x[1 : len(x)-1]
	}
	return x
}

// wideRaw is a wide value's raw integer as the u128 the text helpers take: a
// signed storage sign-extended through i128 (docs/SPEC-TABLES.md §16.2).
func wideRaw(f *ir.Field, x string) string {
	t := scalarRust(f)
	if f.Type.Signed {
		return rustCast(x, t, "i128") + " as u128"
	}
	if t == "u128" {
		return x
	}
	return x + " as u128"
}

// emitTextReadWide emits the read of a WIDE value — a 128-bit integer or a
// fixed(I, F) — into dst (docs/SPEC-TABLES.md §16.2): the exact raw integer
// from any number token (Jr::read_wide), then the field's declared range on
// the RAW scale, or, with none declared, the storage's own, each counted
// clamped.
func emitTextReadWide(b *strings.Builder, f *ir.Field, dst, in string) {
	t := scalarRust(f)
	wide := "u128"
	if f.Type.Signed {
		wide = "i128"
	}
	lo, hi, ok := ir.TableRawRange(f)
	if !ok && f.Type.Width < 128 {
		lo, hi = textStorageRange(f)
		ok = true
	}
	fmt.Fprintf(b, "%sif let Some(raw) = jr.read_wide(%v, %d) {\n", in, f.Type.Signed, f.Type.FracBits)
	if !ok {
		fmt.Fprintf(b, "%s    %s = %s;\n", in, dst, rustCastStmt("raw", "u128", t))
		fmt.Fprintf(b, "%s}\n", in)
		return
	}
	// an end ON the u128/i128 limit cannot fire, and clippy denies the
	// comparison (absurd_extreme_comparisons): it is elided
	wmin, wmax := textStorageRange(&ir.Field{Type: ir.FieldType{Width: 128, Signed: f.Type.Signed}})
	var clamps []string
	if lo.Cmp(wmin) > 0 {
		clamps = append(clamps, fmt.Sprintf("if x < %s%s { x = %s%s; jr.clamped += 1; }", lo, wide, lo, wide))
	}
	if hi.Cmp(wmax) < 0 {
		clamps = append(clamps, fmt.Sprintf("if x > %s%s { x = %s%s; jr.clamped += 1; }", hi, wide, hi, wide))
	}
	if len(clamps) == 0 {
		fmt.Fprintf(b, "%s    %s = %s;\n", in, dst, rustCastStmt("raw", "u128", t))
		fmt.Fprintf(b, "%s}\n", in)
		return
	}
	fmt.Fprintf(b, "%s    let mut x = %s;\n", in, rustCastStmt("raw", "u128", wide))
	fmt.Fprintf(b, "%s    %s\n", in, strings.Join(clamps, " else "))
	fmt.Fprintf(b, "%s    %s = %s;\n", in, dst, rustCastStmt("x", wide, t))
	fmt.Fprintf(b, "%s}\n", in)
}

// textStorageRange is a storage width's own range, for a wide value no
// declaration bounds.
func textStorageRange(f *ir.Field) (lo, hi *big.Int) {
	w := uint(f.Type.Width)
	if f.Type.Signed {
		lo = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), w-1))
		return lo, new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), w-1), big.NewInt(1))
	}
	return new(big.Int), new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), w), big.NewInt(1))
}
