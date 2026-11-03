// text_form.lobster — the hand-editable text form (docs/SPEC-TABLES.md §16.9;
// docs/PORT-LOBSTER.md, THE CARD MAP). The restricted s-expression grammar of
// §16.9.1, read into the load's Record, kept with its source bytes so a
// rewrite is the input (§16.9.3), and one leaf edit by the token's span. The
// typed mapping here covers bool, int, float32/float64, string/N, bytes(N) as
// padded base64, enum, optional presence, nested by-value tables and fixed,
// bounded and unbounded arrays of those; the rest of §16.9.2's kinds are named
// as not carried in RESULT.md.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_save
import .schema_tables

namespace schema_rt

// TNode is one parsed token (§16.9.1): a list, a string, a number, a keyword,
// or one of true/false/null, with its source span.
class TNode
class TextSpan
class TextDoc
class TextParser

class TNode:
    kind:string
    lo:int
    hi:int
    text:string = ""
    items:[TNode] = []

// TextSpan is one scalar token's source range: the path names the field and
// lo/hi its bytes in the document (§16.9.3).
class TextSpan:
    path:string
    lo:int
    hi:int

// TextDoc is a document AND its typed table value (§16.9.3): the retained
// bytes, the root record, the leaf spans, and the root table's name.
class TextDoc:
    text:string
    rec:Record
    spans:[TextSpan]
    root:string

class TextParser:
    s:string
    n:int
    i:int
    fail:string = ""
    spans:[TextSpan] = []

def t_set_fail(p:TextParser, name:string):
    if p.fail == "":
        p.fail = name

def t_gap(p:TextParser):
    while p.i < p.n:
        let c = p.s[p.i]
        if c == 32 or c == 9 or c == 10 or c == 13:
            p.i++
        elif c == 59:
            while p.i < p.n and p.s[p.i] != 10:
                p.i++
        else:
            return

def t_name_char(c:int) -> bool:
    return (c >= 65 and c <= 90) or (c >= 97 and c <= 122) or c == 95 or (c >= 48 and c <= 57) or c == 45 or c == 43 or c == 47 or c == 46

def t_string(p:TextParser) -> TNode?:
    let lo = p.i
    p.i++
    var out = ""
    while p.i < p.n:
        let c = p.s[p.i]
        if c == 34:
            p.i++
            return TNode { kind: "str", lo: lo, hi: p.i, text: out }
        if c == 92:
            p.i++
            if p.i >= p.n:
                t_set_fail(p, "text_unterminated")
                return nil
            let e = p.s[p.i]
            if e == 110:
                out += "\n"
            elif e == 116:
                out += "\t"
            elif e == 114:
                out += "\r"
            elif e == 34:
                out += "\""
            elif e == 92:
                out += "\\"
            else:
                t_set_fail(p, "text_encoding")
                return nil
            p.i++
        else:
            if c < 0x20:
                t_set_fail(p, "text_encoding")
                return nil
            out += substring(p.s, p.i, 1)
            p.i++
    t_set_fail(p, "text_unterminated")
    return nil

def t_parse_value(p:TextParser) -> TNode?:
    t_gap(p)
    if p.i >= p.n:
        t_set_fail(p, "text_unterminated")
        return nil
    let c = p.s[p.i]
    if c == 40:
        let lo = p.i
        p.i++
        let items:[TNode] = []
        while true:
            t_gap(p)
            if p.i >= p.n:
                t_set_fail(p, "text_unterminated")
                return nil
            if p.s[p.i] == 41:
                p.i++
                return TNode { kind: "list", lo: lo, hi: p.i, items: items }
            let v = t_parse_value(p)
            if not v:
                return nil
            push(items, v)
    if c == 34:
        return t_string(p)
    if c == 58:
        let lo = p.i
        p.i++
        let ns = p.i
        while p.i < p.n and t_name_char(p.s[p.i]):
            p.i++
        if p.i == ns:
            t_set_fail(p, "text_syntax")
            return nil
        return TNode { kind: "kw", lo: lo, hi: p.i, text: substring(p.s, ns, p.i - ns) }
    if t_name_char(c) or c == 45:
        let lo = p.i
        while p.i < p.n and t_name_char(p.s[p.i]):
            p.i++
        let tok = substring(p.s, lo, p.i - lo)
        if tok == "true" or tok == "false":
            return TNode { kind: "bool", lo: lo, hi: p.i, text: tok }
        if tok == "null":
            return TNode { kind: "null", lo: lo, hi: p.i, text: tok }
        return TNode { kind: "num", lo: lo, hi: p.i, text: tok }
    t_set_fail(p, "text_syntax")
    return nil

def t_int(tok:string) -> int:
    var i = 0
    var neg = false
    if i < length(tok) and tok[i] == 45:
        neg = true
        i++
    var v = 0
    while i < length(tok):
        v = v * 10 + (tok[i] - 48)
        i++
    return if neg: -v else: v

def t_field_of(d:TableDescriptor, key:string) -> FieldDescriptor?:
    for(d.fields) f:
        if f.json == key:
            return f
    return nil

// t_b64_val is one base64 character's value, -1 for a character outside the
// standard alphabet (§16.9.2: bytes(N) is standard-alphabet padded base64).
def t_b64_val(c:int) -> int:
    if c >= 65 and c <= 90:
        return c - 65
    if c >= 97 and c <= 122:
        return c - 71
    if c >= 48 and c <= 57:
        return c + 4
    if c == 43:
        return 62
    if c == 47:
        return 63
    return -1

// t_base64 decodes one padded base64 string into the bytes it spells
// (§16.9.2: bytes(N)); an invalid character or a bad pad is text_encoding.
def t_base64(p:TextParser, s:string) -> string:
    let vals:[int] = []
    var acc = 0
    var bits = 0
    var i = 0
    let n = length(s)
    var pad = 0
    while i < n:
        let c = s[i]
        if c == 61:
            pad++
            i++
            continue
        if pad > 0:
            t_set_fail(p, "text_encoding")
            return ""
        let v = t_b64_val(c)
        if v < 0:
            t_set_fail(p, "text_encoding")
            return ""
        acc = acc * 64 + v
        bits += 6
        if bits >= 8:
            bits -= 8
            push(vals, (acc >> bits) & 255)
            acc = acc & ((1 << bits) - 1)
        i++
    if bits >= 6 or (pad > 0 and bits != 2 and bits != 4):
        t_set_fail(p, "text_encoding")
        return ""
    var out = zeros(length(vals))
    var j = 0
    while j < length(vals):
        out = write_int8_le(out, j, vals[j])
        j++
    return out

def t_key_text(n:TNode) -> string:
    return n.text

// THE FLAGS TABLE (docs/SPEC-TABLES.md §16.9.2): a flags field is an int
// shape on the wire, so the descriptor does not mark it; the emitter
// registers the variant names here, one call per flags field.
class TextFlags:
    fd:FieldDescriptor
    vs:[string]

let text_flags_registry:[TextFlags] = []

def text_flags(owner:string, field:string, vs:[string]):
    let d = lookup_table(owner)
    if not d:
        return
    for(d.fields) f:
        if f.name == field:
            push(text_flags_registry, TextFlags { fd: f, vs: vs })

def text_flags_of(f:FieldDescriptor) -> [string]:
    for(text_flags_registry) x:
        if x.fd == f:
            return x.vs
    return []

// THE INT128 LANES (docs/PORT-LOBSTER.md, THE NUMBER MODEL). Lobster's int
// is a 64-bit word, so a 128-bit value is two lanes and every operation that
// can carry is done lane by lane.
def i128_add(a:Int128, b:Int128) -> Int128:
    let lo = a.lo + b.lo
    let carry = if ult(lo, a.lo): 1 else: 0
    return Int128 { lo, a.hi + b.hi + carry }

def i128_neg(a:Int128) -> Int128:
    return i128_add(Int128 { a.lo ^ -1, a.hi ^ -1 }, Int128 { 1, 0 })

def i128_cmp(a:Int128, b:Int128, signed:bool) -> int:
    if a.hi == b.hi and a.lo == b.lo:
        return 0
    if signed:
        if a.hi != b.hi:
            return if a.hi < b.hi: -1 else: 1
        return if a.lo < b.lo: -1 else: 1
    if a.hi != b.hi:
        return if ult(a.hi, b.hi): -1 else: 1
    return if ult(a.lo, b.lo): -1 else: 1

// i128_mul_small multiplies by a factor below 2^32 (a decimal digit, or a
// small base), splitting each lane into 32-bit halves so no product leaves
// the 64-bit word.
def i128_mul_small(a:Int128, m:int) -> Int128:
    let lo0 = a.lo & 0xFFFFFFFF
    let lo1 = shr(a.lo, 32) & 0xFFFFFFFF
    let p0 = lo0 * m
    let p1 = lo1 * m + shr(p0, 32)
    let newlo = shl(p1, 32) | (p0 & 0xFFFFFFFF)
    let carry = shr(p1, 32)
    let hi0 = a.hi & 0xFFFFFFFF
    let hi1 = shr(a.hi, 32) & 0xFFFFFFFF
    let q0 = hi0 * m + carry
    let q1 = hi1 * m + shr(q0, 32)
    return Int128 { newlo, shl(q1, 32) | (q0 & 0xFFFFFFFF) }

def i128_bit(a:Int128, i:int) -> int:
    if i < 64:
        return shr(a.lo, i) & 1
    return shr(a.hi, i - 64) & 1

def i128_setbit(a:Int128, i:int, on:bool) -> Int128:
    if i < 64:
        let m = shl(1, i)
        if on:
            return Int128 { a.lo | m, a.hi }
        return Int128 { a.lo & (m ^ -1), a.hi }
    let m = shl(1, i - 64)
    if on:
        return Int128 { a.lo, a.hi | m }
    return Int128 { a.lo, a.hi & (m ^ -1) }

// i128_div_small is long division by a positive divisor below 2^62: the
// quotient and the remainder, both exact.
def i128_div_small(a:Int128, d:int) -> Int128, int:
    var q = Int128 { 0, 0 }
    var r = 0
    var i = 127
    while i >= 0:
        r = r * 2 + i128_bit(a, i)
        if r >= d:
            r -= d
            q = i128_setbit(q, i, true)
        i--
    return q, r

// t_dec is a decimal token split into its magnitude's digits and the
// power of ten the value carries (docs/SPEC-TABLES.md §16.9.2: exact
// integral decimal and exponent spellings, parsed without a binary float).
class TextDec:
    neg:bool
    digits:string
    exp:int
    ok:bool

def t_dec(tok:string) -> TextDec:
    var i = 0
    var neg = false
    let n = length(tok)
    if n == 0:
        return TextDec { neg: false, digits: "", exp: 0, ok: false }
    if tok[0] == 45:
        neg = true
        i++
    elif tok[0] == 43:
        i++
    var digits = ""
    var dot = -1
    var exp = 0
    var exp_neg = false
    var in_exp = false
    var exp_set = false
    while i < n:
        let c = tok[i]
        if c >= 48 and c <= 57:
            if in_exp:
                exp = exp * 10 + (c - 48)
                exp_set = true
            else:
                digits += substring(tok, i, 1)
        elif c == 46 and not in_exp and dot < 0:
            dot = length(digits)
        elif (c == 101 or c == 69) and not in_exp:
            in_exp = true
        elif c == 45 and in_exp and not exp_set:
            exp_neg = true
        elif c == 43 and in_exp and not exp_set:
            exp_neg = false
        else:
            return TextDec { neg: neg, digits: "", exp: 0, ok: false }
        i++
    if dot < 0:
        dot = length(digits)
    let e = (if exp_neg: -exp else: exp) - (length(digits) - dot)
    if length(digits) == 0:
        return TextDec { neg: neg, digits: "", exp: 0, ok: false }
    return TextDec { neg: neg, digits: digits, exp: e, ok: true }

def t_dec_value(d:TextDec) -> Int128:
    var v = Int128 { 0, 0 }
    for(length(d.digits)) i:
        v = i128_mul_small(v, 10)
        v = i128_add(v, Int128 { d.digits[i] - 48, 0 })
    return if d.neg: i128_neg(v) else: v

// t_int_token is a number of exact integral value (§16.9.2): a fractional
// spelling whose fraction is nonzero is text_kind_mismatch.
def t_int_token(tok:string) -> Int128, bool:
    let d = t_dec(tok)
    if not d.ok:
        return Int128 { 0, 0 }, false
    var digits = d.digits
    var e = d.exp
    if e > 0:
        for(e):
            digits += "0"
        e = 0
    elif e < 0:
        let cut = -e
        if cut >= length(digits):
            // zero only when every digit is zero, else a fraction
            var allzero = true
            for(length(digits)) i:
                if digits[i] != 48:
                    allzero = false
            if not allzero:
                return Int128 { 0, 0 }, false
            digits = "0"
        else:
            var j = length(digits) - cut
            while j < length(digits):
                if digits[j] != 48:
                    return Int128 { 0, 0 }, false
                j++
            digits = substring(digits, 0, length(digits) - cut)
    let dd = TextDec { neg: d.neg, digits: digits, exp: 0, ok: true }
    return t_dec_value(dd), true

// t_fixed_token scales a Q(I.F) token by 2^F without an intermediate float
// (§16.9.2): a fraction not on the grid is text_kind_mismatch.
def t_fixed_token(tok:string, fbits:int) -> Int128, bool:
    let d = t_dec(tok)
    if not d.ok:
        return Int128 { 0, 0 }, false
    var digits = d.digits
    var e = d.exp
    if e > 0:
        for(e):
            digits += "0"
        e = 0
    var value = t_dec_value(TextDec { neg: d.neg, digits: digits, exp: 0, ok: true })
    if e < 0:
        let n = -e
        if n > 18:
            return Int128 { 0, 0 }, false
        // the fraction must be on the Q grid: value * 2^F / 10^n exact
        value = i128_shl(value, fbits)
        var den = 1
        for(n):
            den *= 10
        let q, r = i128_div_small(value, den)
        if r != 0:
            return Int128 { 0, 0 }, false
        return q, true
    return i128_shl(value, fbits), true

// t_int_store places a parsed raw value, wide or at 64 bits or fewer, and
// checks the declared range and bits width before publishing (§16.9.2).
def t_int_store(p:TextParser, sl:Slot, f:FieldDescriptor, raw:Int128, signed:bool) -> bool:
    let v = raw
    if f.fbits > 0:
        // the descriptor's bounds are whole units; compare on their scale
        if f.ranged:
            if i128_cmp(v, i128_shl(f.lo, f.fbits), signed) < 0 or i128_cmp(i128_shl(f.hi, f.fbits), v, signed) < 0:
                t_set_fail(p, "text_bound")
                return false
    elif f.ranged:
        if i128_cmp(v, f.lo, signed) < 0 or i128_cmp(f.hi, v, signed) < 0:
            t_set_fail(p, "text_bound")
            return false
    if (f.k >= 18 and f.k <= 29) or f.dom == "int128":
        sl.cell.w = v
    else:
        if f.bits > 0 and f.bits < 64:
            let most = shl(1, f.bits)
            if ult(most - 1, v.lo):
                t_set_fail(p, "text_bound")
                return false
        sl.cell.i = v.lo
    return true

def t_set_flags(p:TextParser, sl:Slot, f:FieldDescriptor, v:TNode) -> bool:
    let names = text_flags_of(f)
    if length(names) == 0:
        t_set_fail(p, "text_kind_mismatch")
        return false
    if v.kind != "list":
        t_set_fail(p, "text_kind_mismatch")
        return false
    var mask = 0
    let seen:[string] = []
    for(v.items) it:
        if it.kind != "kw" and it.kind != "str":
            t_set_fail(p, "text_kind_mismatch")
            return false
        var at = -1
        for(names) nm, i:
            if nm == it.text:
                at = i
        if at < 0:
            t_set_fail(p, "text_unknown_variant")
            return false
        for(seen) s:
            if s == it.text:
                t_set_fail(p, "text_duplicate_key")
                return false
        push(seen, it.text)
        mask = mask | shl(1, at)
    sl.cell.i = mask
    return true

// t_set_keyed reads an enum-keyed array (§16.9.2): a record keyed by the
// variant names the emitter registered, missing slots keeping their defaults.
def t_set_keyed(p:TextParser, sl:Slot, f:FieldDescriptor, v:TNode, path:string) -> bool:
    if v.kind != "list":
        t_set_fail(p, "text_kind_mismatch")
        return false
    let keys = keyed_variants(f)
    var k = 0
    let seen:[int] = []
    while k < length(v.items):
        if k + 1 >= length(v.items):
            t_set_fail(p, "text_syntax")
            return false
        let key = v.items[k]
        if key.kind != "kw" and key.kind != "str":
            t_set_fail(p, "text_syntax")
            return false
        var slot = -1
        for(keys) kv, i:
            if kv.name == key.text:
                slot = i
        if slot < 0:
            t_set_fail(p, "text_unknown_key")
            return false
        for(seen) s:
            if s == slot:
                t_set_fail(p, "text_duplicate_key")
                return false
        push(seen, slot)
        if slot >= length(sl.elems):
            t_set_fail(p, "text_bound")
            return false
        let e = elem_of(f)
        let it = v.items[k + 1]
        let tmp = Slot { fd: e, present: false, count: 0, cell: Value {}, elems: [], entries: [] }
        if not t_value_into(p, tmp, e, it, path + "." + key.text):
            return false
        sl.elems[slot] = tmp.cell
        k += 2
    return true

// t_set_union reads a union (§16.9.2): `()` is None, otherwise exactly one
// arm key and its arm-kind value.
def t_set_union(p:TextParser, sl:Slot, f:FieldDescriptor, v:TNode, path:string) -> bool:
    let un = lookup_union(f.t)
    if not un:
        t_set_fail(p, "text_kind_mismatch")
        return false
    if v.kind != "list":
        t_set_fail(p, "text_kind_mismatch")
        return false
    if length(v.items) == 0:
        sl.cell.i = 0
        sl.cell.tab = nil
        sl.cell.arm = nil
        return true
    if length(v.items) != 2:
        t_set_fail(p, "text_kind_mismatch")
        return false
    let key = v.items[0]
    if key.kind != "kw" and key.kind != "str":
        t_set_fail(p, "text_syntax")
        return false
    var arm:FieldDescriptor? = nil
    for(un.arms) a:
        if a.json == key.text or a.name == key.text:
            arm = a
    if not arm:
        t_set_fail(p, "text_unknown_key")
        return false
    let a = arm
    let it = v.items[1]
    if a.s == "void":
        if it.kind != "null":
            t_set_fail(p, "text_kind_mismatch")
            return false
        sl.cell.i = a.tag
        sl.cell.tab = nil
        sl.cell.arm = nil
        return true
    if a.s == "tab":
        let inner = t_fill(p, it, table_of(a.t), path + "." + key.text)
        if not inner:
            return false
        sl.cell.i = a.tag
        sl.cell.tab = inner
        sl.cell.arm = nil
        return true
    let fv = Slot { fd: a, present: false, count: 0, cell: Value {}, elems: [], entries: [] }
    if not t_set_scalar(p, fv, it, path + "." + key.text):
        return false
    sl.cell.i = a.tag
    sl.cell.tab = nil
    sl.cell.arm = fv
    return true

def t_set_scalar(p:TextParser, sl:Slot, v:TNode, path:string) -> bool:
    let f = sl.fd
    let s = f.s
    if s == "bool":
        if v.kind != "bool":
            t_set_fail(p, "text_kind_mismatch")
            return false
        sl.cell.b = v.text == "true"
    elif s == "int":
        if length(text_flags_of(f)) > 0:
            return t_set_flags(p, sl, f, v)
        if v.kind != "num":
            t_set_fail(p, "text_kind_mismatch")
            return false
        let signed = f.signed and not f.timestamp
        var raw = Int128 { 0, 0 }
        if f.fbits > 0:
            let scaled, good = t_fixed_token(v.text, f.fbits)
            if not good:
                t_set_fail(p, "text_kind_mismatch")
                return false
            raw = scaled
        else:
            let parsed, integral = t_int_token(v.text)
            if not integral:
                t_set_fail(p, "text_kind_mismatch")
                return false
            raw = parsed
        if not t_int_store(p, sl, f, raw, signed):
            return false
    elif s == "float":
        if v.kind != "num":
            t_set_fail(p, "text_kind_mismatch")
            return false
        // the token is already the grammar's number, so the one failure
        // left is a magnitude past the storage, an infinity once parsed at
        // the field's width: a float is finite (docs/SPEC-TABLES.md §3.3)
        // and the text form refuses it as text_bound, as Go's resolver does
        let x = parse_float64(v.text)
        if (f.f32 and not finite_f32(x)) or not finite_f64(x):
            t_set_fail(p, "text_bound")
            return false
        if f.franged and (x < f.flo or x > f.fhi):
            t_set_fail(p, "text_bound")
            return false
        sl.cell.f = x
    elif s == "str" or s == "text":
        if v.kind != "str":
            t_set_fail(p, "text_kind_mismatch")
            return false
        if f.bound > 0 and length(v.text) > f.bound:
            t_set_fail(p, "text_bound")
            return false
        sl.cell.s = v.text
        sl.count = length(v.text)
    elif s == "bytes":
        if v.kind != "str":
            t_set_fail(p, "text_kind_mismatch")
            return false
        let dec = t_base64(p, v.text)
        if p.fail != "":
            return false
        if f.bound > 0 and length(dec) > f.bound:
            t_set_fail(p, "text_bound")
            return false
        sl.cell.s = dec
        sl.count = length(dec)
    elif s == "enum":
        if v.kind != "kw" and v.kind != "str":
            t_set_fail(p, "text_kind_mismatch")
            return false
        var found = false
        for(f.vs) ev, i:
            if ev.name == v.text:
                sl.cell.i = i + 1
                found = true
        if not found:
            t_set_fail(p, "text_unknown_variant")
            return false
    else:
        t_set_fail(p, "text_kind_mismatch")
        return false
    push(p.spans, TextSpan { path: path, lo: v.lo, hi: v.hi })
    return true

// t_value_into places one value at the shape its descriptor names: a nested
// by-value body, an array, a map, a union or a scalar (§16.9.2).
def t_value_into(p:TextParser, sl:Slot, f:FieldDescriptor, v:TNode, path:string) -> bool:
    if f.s == "tab":
        let inner = t_fill(p, v, table_of(f.t), path)
        if not inner:
            return false
        sl.cell.tab = inner
        return true
    if f.s == "arr":
        if f.array == "keyed":
            return t_set_keyed(p, sl, f, v, path)
        if v.kind != "list":
            t_set_fail(p, "text_kind_mismatch")
            return false
        let e = elem_of(f)
        sl.elems = []
        if e.s == "tab" and f.ek == 12:
            let ed = table_of(e.t)
            for(v.items) it:
                let rec = new_record(ed)
                let tmp = Slot { fd: ed.fields[0], present: false, count: 0, cell: Value {}, elems: [], entries: [] }
                if not t_set_scalar(p, tmp, it, path):
                    return false
                rec.fields[0].cell = tmp.cell
                rec.fields[0].count = tmp.count
                push(sl.elems, Value { tab: rec })
        else:
            for(v.items) it:
                let tmp = Slot { fd: e, present: false, count: 0, cell: Value {}, elems: [], entries: [] }
                if not t_value_into(p, tmp, e, it, path):
                    return false
                push(sl.elems, tmp.cell)
        if f.array == "fixed" and length(sl.elems) != f.bound:
            t_set_fail(p, "text_bound")
            return false
        if f.array == "counted" and length(sl.elems) > f.bound:
            t_set_fail(p, "text_bound")
            return false
        sl.count = length(sl.elems)
        return true
    if f.s == "union":
        return t_set_union(p, sl, f, v, path)
    if f.s == "map":
        if v.kind != "list":
            t_set_fail(p, "text_kind_mismatch")
            return false
        let ed = table_of(f.t)
        sl.entries = []
        for(v.items) pair:
            if pair.kind != "list" or length(pair.items) != 2:
                t_set_fail(p, "text_kind_mismatch")
                return false
            let entry = new_record(ed)
            if not t_value_into(p, entry.fields[0], entry.fields[0].fd, pair.items[0], path):
                return false
            if not t_value_into(p, entry.fields[1], entry.fields[1].fd, pair.items[1], path):
                return false
            push(sl.entries, Value { tab: entry })
        return true
    return t_set_scalar(p, sl, v, path)

def t_fill(p:TextParser, n:TNode, d:TableDescriptor, path:string) -> Record?:
    if n.kind != "list":
        t_set_fail(p, "text_kind_mismatch")
        return nil
    let rec = new_record(d)
    var k = 0
    while k < length(n.items):
        if k + 1 >= length(n.items):
            t_set_fail(p, "text_syntax")
            return nil
        let key = t_key_text(n.items[k])
        if n.items[k].kind != "kw" and n.items[k].kind != "str":
            t_set_fail(p, "text_syntax")
            return nil
        let f = t_field_of(d, key)
        if not f:
            t_set_fail(p, "text_unknown_key")
            return nil
        let sl = rec.fields[f.o]
        if f.present != "":
            sl.present = true
        let v = n.items[k + 1]
        let child = if path == "": key else: path + "." + key
        if not t_value_into(p, sl, f, v, child):
            return nil
        k += 2
    for(d.fields) f:
        if f.required:
            var seen = false
            for(n.items) it, j:
                if j % 2 == 0 and it.text == f.json:
                    seen = true
            if not seen:
                t_set_fail(p, "text_missing_required")
                return nil
    return rec

// text_load reads one hand-editable document (§16.9.1-§16.9.2): the document
// AND its typed Record, or nil with no usable pair (§16.9.4).
def text_load(root:string, text:string) -> TextDoc?:
    let d = lookup_table(root)
    if not d:
        return nil
    let p = TextParser { s: text, n: length(text), i: 0 }
    let node = t_parse_value(p)
    if not node or p.fail != "":
        return nil
    t_gap(p)
    if p.i != p.n:
        return nil
    let rec = t_fill(p, node, d, "")
    if not rec or p.fail != "":
        return nil
    return TextDoc { text: text, rec: rec, spans: p.spans, root: root }

// text_save writes a document back: the retained bytes (§16.9.3).
def text_save(doc:TextDoc) -> string:
    return doc.text

// text_edit replaces one existing scalar's token span and reads the result
// back; every other byte is the input's (§16.9.3).
def text_edit(doc:TextDoc, path:string, token:string) -> TextDoc?:
    for(doc.spans) sp:
        if sp.path == path:
            let out = substring(doc.text, 0, sp.lo) + token + substring(doc.text, sp.hi, length(doc.text) - sp.hi)
            return text_load(doc.root, out)
    return nil
