// json.lobster — the Lobster table runtime's JSON TEXT FORM
// (docs/SPEC-TABLES.md §16 through §16.13; docs/PORT-LOBSTER.md, THE CARD
// MAP). A port of the Go reference engine's text form (internal/tabletext
// read.go, write.go, maps.go and wide.go) onto the load's Record: one JSON
// text into one Record (json_read, json_read_into) and one Record as one JSON
// text (json_write). Every fact is read from the descriptors as data; the
// facts they do not carry (a flags field's names, a `json = "array"` table,
// a guarded field's condition) are registered by the emitter's json.go part.
//
// THE NUMBERS ARE EXACT. A float is written at the shortest %g precision that
// reads back the same value at the field's width, spelled from the double's
// exact decimal expansion; a float32 field reads one correctly rounded
// conversion (strtod, then the one midpoint case decided exactly); a wide
// kind converts through an unsigned big integer, never a double.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables
import .descriptors

namespace schema_rt

// The caps the generated walk carries (Go's maxJsonDepth, maxJsonKey).
let json_max_depth = 128
let json_max_key = 255

// ---------------------------------------------------------------------------
// THE FACTS the descriptors do not carry, registered by json.go
// ---------------------------------------------------------------------------

class JsonFlags:
    fd:FieldDescriptor
    vs:[string]

class JsonGuard:
    fd:FieldDescriptor
    conds:[string]
    wants:[bool]

let json_flags_registry:[JsonFlags] = []
let json_guard_registry:[JsonGuard] = []
let json_array_registry:[string] = []
let json_text_list_registry:[FieldDescriptor] = []

// json_fd_of is the descriptor a table field or a union arm names.
def json_fd_of(owner:string, field:string) -> FieldDescriptor?:
    let d = lookup_table(owner)
    if d:
        for(d.fields) f:
            if f.name == field:
                return f
    let u = lookup_union(owner)
    if u:
        for(u.arms) a:
            if a.name == field:
                return a
    return nil

// json_flags registers a flags field's variant names, bit order (§16.2); an
// array of flags registers its element descriptor too.
def json_flags(owner:string, field:string, vs:[string]):
    let f = json_fd_of(owner, field)
    if f:
        push(json_flags_registry, JsonFlags { fd: f, vs: vs })
        let e = f.e
        if e:
            push(json_flags_registry, JsonFlags { fd: e, vs: vs })

def json_flags_of(f:FieldDescriptor) -> JsonFlags?:
    for(json_flags_registry) x:
        if x.fd == f:
            return x
    return nil

// json_text_list marks a TEXT LIST field, whose JSON is a plain array of
// strings (§2.13, §16.2).
def json_text_list(owner:string, field:string):
    let f = json_fd_of(owner, field)
    if f:
        push(json_text_list_registry, f)

// json_array_table marks a `json = "array"` table (§16.10).
def json_array_table(name:string):
    push(json_array_registry, name)

def json_is_array_table(name:string) -> bool:
    for(json_array_registry) n:
        if n == name:
            return true
    return false

// json_guard registers a guarded field's branch condition: bool fields of the
// same body and the values they must hold (Go's tabletext.Guards).
def json_guard(owner:string, field:string, conds:[string], wants:[bool]):
    let f = json_fd_of(owner, field)
    if f:
        push(json_guard_registry, JsonGuard { fd: f, conds: conds, wants: wants })

def json_guard_holds(rec:Record, sl:Slot) -> bool:
    for(json_guard_registry) g:
        if g.fd == sl.fd:
            for(g.conds) c, i:
                var found = false
                for(rec.fields) x:
                    if x.fd.name == c:
                        found = true
                        if x.cell.b != g.wants[i]:
                            return false
                if not found:
                    return false
    return true

// ---------------------------------------------------------------------------
// BYTES AND THE FIELD FACTS
// ---------------------------------------------------------------------------

def j_byte_strings() -> [string]:
    let out:[string] = []
    var i = 0
    while i < 256:
        push(out, write_int8_le(ensure_size("", 1, 0), 0, i))
        i++
    return out

let j_bytes = j_byte_strings()

def j_b64_table() -> [int]:
    let out:[int] = []
    for(256):
        push(out, -1)
    for("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/") c, i:
        out[c] = i
    return out

let j_b64 = j_b64_table()
let j_b64_alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// j_is_fixed: a fixed(I, F) or ufixed(I, F) value (§15).
def j_is_fixed(f:FieldDescriptor) -> bool:
    return f.s == "int" and (f.ibits > 0 or f.fbits > 0)

// j_kind is the table kind a scalar's facts name (Go's ir.TableScalarKind),
// from the descriptor alone, so an element descriptor (which carries no k)
// answers it too.
def j_kind(f:FieldDescriptor) -> int:
    if f.s == "bool":
        return 1
    if f.s == "float":
        return if f.f32: 10 else: 11
    if f.s != "int":
        return 0
    if f.timestamp:
        return 36
    let w = if f.kw == 1: 0 elif f.kw == 2: 1 elif f.kw == 4: 2 elif f.kw == 8: 3 else: 4
    if j_is_fixed(f):
        return if f.signed: 20 + w else: 25 + w
    if f.kw == 16:
        return if f.signed: 18 else: 19
    return if f.signed: 2 + w else: 6 + w

// j_storage_bytes is the field's own storage width (Go's StorageBytes).
def j_storage_bytes(f:FieldDescriptor) -> int:
    if f.s == "enum":
        return 2
    if f.bits > 0:
        return if f.bits <= 32: 4 else: 8
    if f.kw > 0:
        return f.kw
    return 8

def j_is_text_list(f:FieldDescriptor) -> bool:
    for(json_text_list_registry) x:
        if x == f:
            return true
    return false

// TakesNull: a `?T` reads null as ABSENT and a pointer as null (§16.2).
def j_takes_null(f:FieldDescriptor) -> bool:
    if f.present != "":
        return true
    if f.s == "ptr" or f.s == "sblob" or f.s == "bblob":
        return true
    if f.s == "arr":
        return elem_of(f).s == "ptr"
    return false

// j_shape is the JSON shape a field's declaration takes (Go's Shape).
def j_shape(f:FieldDescriptor) -> int:
    let s = f.s
    if s == "str" or s == "text" or s == "bytes" or s == "sblob" or s == "bblob":
        return 's'
    if s == "map":
        return 'o'
    if s == "arr":
        return if f.array == "keyed": 'o' else: 'a'
    return j_element_shape(f)

// j_element_shape is the same classifier one level down (Go's ElementShape).
def j_element_shape(f:FieldDescriptor) -> int:
    let s = f.s
    if s == "union":
        return 'o'
    if s == "tab" or s == "ptr":
        return if json_is_array_table(f.t): 'a' else: 'o'
    if s == "enum":
        return 's'
    if s == "int" and json_flags_of(f):
        return 'a'
    if s == "int" and f.timestamp:
        return 's'
    if s == "bool":
        return 'b'
    return 'n'

def j_enum_value(vs:[EnumVariant], name:string) -> int:
    if name == "None":
        return 0
    for(vs) v, i:
        if v.name == name:
            return i + 1
    return -1

// ---------------------------------------------------------------------------
// THE UNSIGNED BIG INTEGER: little-endian limbs of 24 bits, normalized
// ---------------------------------------------------------------------------

let j_bn_mask = 0xFFFFFF

def j_bn_norm(a:[int]) -> [int]:
    var n = length(a)
    while n > 0 and a[n - 1] == 0:
        n--
    truncate(a, n)
    return a

def j_bn_u64(v:int) -> [int]:
    let out:[int] = [v & j_bn_mask, shr(v, 24) & j_bn_mask, shr(v, 48) & j_bn_mask]
    return j_bn_norm(out)

def j_bn_copy(a:[int]) -> [int]:
    let out:[int] = []
    for(a) x:
        push(out, x)
    return out

def j_bn_cmp(a:[int], b:[int]) -> int:
    if length(a) != length(b):
        return if length(a) < length(b): -1 else: 1
    var i = length(a)
    while i > 0:
        i--
        if a[i] != b[i]:
            return if a[i] < b[i]: -1 else: 1
    return 0

def j_bn_add(a:[int], b:[int]) -> [int]:
    let out:[int] = []
    var carry = 0
    var i = 0
    while i < length(a) or i < length(b) or carry > 0:
        var s = carry
        if i < length(a):
            s += a[i]
        if i < length(b):
            s += b[i]
        push(out, s & j_bn_mask)
        carry = s >> 24
        i++
    return j_bn_norm(out)

// j_bn_sub is a - b for a >= b.
def j_bn_sub(a:[int], b:[int]) -> [int]:
    let out:[int] = []
    var borrow = 0
    for(a) x, i:
        var s = x - borrow
        if i < length(b):
            s -= b[i]
        if s < 0:
            s += 0x1000000
            borrow = 1
        else:
            borrow = 0
        push(out, s)
    return j_bn_norm(out)

// j_bn_mul_small is a x m for 0 <= m < 2^30.
def j_bn_mul_small(a:[int], m:int) -> [int]:
    let out:[int] = []
    var carry = 0
    for(a) x:
        let p = x * m + carry
        push(out, p & j_bn_mask)
        carry = p >> 24
    while carry > 0:
        push(out, carry & j_bn_mask)
        carry = carry >> 24
    return j_bn_norm(out)

def j_bn_add_small(a:[int], v:int) -> [int]:
    return j_bn_add(a, j_bn_u64(v))

// j_bn_divmod_small is a / d and a % d for 0 < d < 2^30.
def j_bn_divmod_small(a:[int], d:int) -> [int], int:
    let out:[int] = []
    for(a):
        push(out, 0)
    var rem = 0
    var i = length(a)
    while i > 0:
        i--
        let cur = rem * 0x1000000 + a[i]
        out[i] = cur / d
        rem = cur % d
    return j_bn_norm(out), rem

def j_bn_shl(a:[int], n:int) -> [int]:
    if length(a) == 0 or n <= 0:
        return j_bn_copy(a)
    let q = n / 24
    let r = n % 24
    let out:[int] = []
    for(q):
        push(out, 0)
    var carry = 0
    for(a) x:
        let v = (x << r) | carry
        push(out, v & j_bn_mask)
        carry = v >> 24
    if carry > 0:
        push(out, carry)
    return j_bn_norm(out)

def j_bn_shr(a:[int], n:int) -> [int]:
    if n <= 0:
        return j_bn_copy(a)
    let q = n / 24
    let r = n % 24
    let out:[int] = []
    var i = q
    while i < length(a):
        var v = a[i] >> r
        if i + 1 < length(a):
            v = v | ((a[i + 1] << (24 - r)) & j_bn_mask)
        push(out, v & j_bn_mask)
        i++
    return j_bn_norm(out)

// j_bn_low is the low n bits of a.
def j_bn_low(a:[int], n:int) -> [int]:
    let q = n / 24
    let r = n % 24
    let out:[int] = []
    for(a) x, i:
        if i < q:
            push(out, x)
        elif i == q:
            push(out, x & ((1 << r) - 1))
    return j_bn_norm(out)

def j_bn_pow2(n:int) -> [int]:
    return j_bn_shl([1], n)

def j_bn_mul_pow10(a:[int], k:int) -> [int]:
    var out = j_bn_copy(a)
    var left = k
    while left >= 6:
        out = j_bn_mul_small(out, 1000000)
        left -= 6
    while left > 0:
        out = j_bn_mul_small(out, 10)
        left--
    return out

def j_bn_mul_pow5(a:[int], k:int) -> [int]:
    var out = j_bn_copy(a)
    var left = k
    while left >= 10:
        out = j_bn_mul_small(out, 9765625)
        left -= 10
    while left > 0:
        out = j_bn_mul_small(out, 5)
        left--
    return out

def j_bn_from_dec(digits:string) -> [int]:
    var out:[int] = []
    var i = 0
    let n = length(digits)
    while i < n:
        var chunk = 0
        var mul = 1
        var k = 0
        while k < 6 and i < n:
            chunk = chunk * 10 + (digits[i] - '0')
            mul *= 10
            i++
            k++
        out = j_bn_add_small(j_bn_mul_small(out, mul), chunk)
    return out

def j_bn_to_dec(a:[int]) -> string:
    if length(a) == 0:
        return "0"
    let chunks:[int] = []
    var x = a
    while length(x) > 0:
        let q, r = j_bn_divmod_small(x, 1000000)
        push(chunks, r)
        x = q
    let parts:[string] = []
    var i = length(chunks)
    while i > 0:
        i--
        if i == length(chunks) - 1:
            push(parts, number_to_string(chunks[i], 10, 1))
        else:
            push(parts, number_to_string(chunks[i], 10, 6))
    return concat_string(parts, "")

// j_bn_lanes is a (< 2^128) as its two 64-bit lanes.
def j_bn_lanes(a:[int]) -> Int128:
    var lo = 0
    var hi = 0
    for(a) x, i:
        let at = i * 24
        if at < 64:
            lo = lo | shl(x, at)
            if at + 24 > 64:
                hi = hi | shr(x, 64 - at)
        elif at < 128:
            hi = hi | shl(x, at - 64)
    return Int128 { lo, hi }

def j_bn_of_lanes(v:Int128) -> [int]:
    return j_bn_add(j_bn_shl(j_bn_u64(v.hi), 64), j_bn_u64(v.lo))

def j_i128_neg(v:Int128) -> Int128:
    let lo = (~v.lo) + 1
    var hi = ~v.hi
    if lo == 0:
        hi += 1
    return Int128 { lo, hi }

// ---------------------------------------------------------------------------
// FLOATS: the exact decimal expansion, %g, and the float32 conversion
// ---------------------------------------------------------------------------

def j_f32frombits(b:int) -> float:
    return read_float32_le(write_int32_le(zeros(4), 0, b), 0)

def j_is_inf(x:float) -> bool:
    return x == x and (x - x) != (x - x)

// j_decimal is a finite nonzero |x| as its exact decimal digits (no leading
// or trailing zero) and the point: |x| = 0.digits x 10^point.
def j_decimal(x:float) -> string, int:
    let bits = f64bits(x) & 0x7FFFFFFFFFFFFFFF
    var e = shr(bits, 52)
    var m = bits & 0xFFFFFFFFFFFFF
    if e == 0:
        e = 1
    else:
        m = m | 0x10000000000000
    e -= 1075
    var digits = ""
    var point = 0
    if e >= 0:
        digits = j_bn_to_dec(j_bn_shl(j_bn_u64(m), e))
        point = length(digits)
    else:
        digits = j_bn_to_dec(j_bn_mul_pow5(j_bn_u64(m), -e))
        point = length(digits) + e
    var n = length(digits)
    while n > 0 and digits[n - 1] == '0':
        n--
    return substring(digits, 0, n), point

def j_strip_zeros(s:string) -> string:
    var n = length(s)
    while n > 0 and s[n - 1] == '0':
        n--
    return substring(s, 0, n)

// j_round rounds 0.digits x 10^point to nsig significant digits, half to
// even on the exact value; "" is zero.
def j_round(digits:string, point:int, nsig:int) -> string, int:
    if nsig >= length(digits):
        return digits, point
    if nsig < 0:
        return "", point
    let next = digits[nsig]
    let rest = length(digits) > nsig + 1
    var last = 0
    if nsig > 0:
        last = digits[nsig - 1] - '0'
    let up = next > '5' or (next == '5' and (rest or (last & 1) == 1))
    if not up:
        return j_strip_zeros(substring(digits, 0, nsig)), point
    // carry up through the kept digits
    let kept = substring(digits, 0, nsig)
    var i = nsig - 1
    while i >= 0 and kept[i] == '9':
        i--
    if i < 0:
        return "1", point + 1
    let bumped = substring(kept, 0, i) + j_bytes[kept[i] + 1]
    return j_strip_zeros(bumped), point

def j_exp_digits(x:int) -> string:
    if x < 0:
        return if x > -10: "-0" + string(-x) else: "-" + string(-x)
    return if x < 10: "+0" + string(x) else: "+" + string(x)

def j_zeros_text(n:int) -> string:
    var s = ""
    for(n):
        s += "0"
    return s

// j_format_g is C's `%.*g` for a finite double (Go's formatG).
def j_format_g(x:float, prec:int, digits:string, point:int) -> string:
    let sign = if (f64bits(x) & 0x8000000000000000) != 0: "-" else: ""
    if length(digits) == 0:
        return sign + "0"
    let d, p = j_round(digits, point, prec)
    let exp = p - 1
    if exp < -4 or exp >= prec:
        var mant = j_bytes[d[0]]
        if length(d) > 1:
            mant += "." + substring(d, 1, length(d) - 1)
        return sign + mant + "e" + j_exp_digits(exp)
    let k = prec - 1 - exp
    let d2, p2 = j_round(digits, point, point + k)
    if length(d2) == 0:
        return sign + "0"
    var whole = "0"
    if p2 > 0:
        if length(d2) >= p2:
            whole = substring(d2, 0, p2)
        else:
            whole = d2 + j_zeros_text(p2 - length(d2))
    var frac = ""
    if p2 < 0:
        frac = j_zeros_text(-p2) + d2
    elif length(d2) > p2:
        frac = substring(d2, p2, length(d2) - p2)
    if length(frac) > 0:
        return sign + whole + "." + frac
    return sign + whole

// j_write_float is the shortest precision that reads back the same value at
// the field's own width (Go's writeFloat); "" for a value with no spelling.
def j_float_text(x:float, single:bool) -> string:
    var digits = ""
    var point = 0
    if x != 0.0:
        digits, point = j_decimal(x)
    let low = if single: 6 else: 15
    let high = if single: 9 else: 17
    var n = low
    while true:
        let text = j_format_g(x, n, digits, point)
        if n >= high:
            return text
        let back, _ = string_to_float(text)
        if single:
            if fround(back) == x:
                return text
        elif back == x:
            return text
        n++
    return ""

// j_token_decimal is a number token's magnitude as digits (no leading or
// trailing zero) and the exponent E: |token| = digits x 10^E.
def j_token_decimal(token:string) -> string, int:
    var i = 0
    let n = length(token)
    if i < n and (token[i] == '-' or token[i] == '+'):
        i++
    var digits = ""
    var frac = 0
    let start = i
    while i < n and token[i] >= '0' and token[i] <= '9':
        i++
    digits = substring(token, start, i - start)
    if i < n and token[i] == '.':
        i++
        let fs = i
        while i < n and token[i] >= '0' and token[i] <= '9':
            i++
        digits += substring(token, fs, i - fs)
        frac = i - fs
    var exp = 0
    if i < n and (token[i] == 'e' or token[i] == 'E'):
        i++
        var neg = false
        if i < n and (token[i] == '-' or token[i] == '+'):
            neg = token[i] == '-'
            i++
        while i < n and token[i] >= '0' and token[i] <= '9':
            if exp < 100000:
                exp = exp * 10 + (token[i] - '0')
            i++
        if neg:
            exp = -exp
    var lead = 0
    while lead < length(digits) and digits[lead] == '0':
        lead++
    var e = exp - frac
    var end = length(digits)
    while end > lead and digits[end - 1] == '0':
        end--
        e++
    return substring(digits, lead, end - lead), e

// j_cmp_token_float compares |token| with a positive finite double exactly.
def j_cmp_token_float(token:string, x:float) -> int:
    let digits, e = j_token_decimal(token)
    if length(digits) == 0:
        return -1
    let bits = f64bits(x)
    var be = shr(bits, 52) & 0x7FF
    var m = bits & 0xFFFFFFFFFFFFF
    if be == 0:
        be = 1
    else:
        m = m | 0x10000000000000
    let k = be - 1075
    var l = j_bn_from_dec(digits)
    var r = j_bn_u64(m)
    if e >= 0:
        l = j_bn_mul_pow10(l, e)
    else:
        r = j_bn_mul_pow10(r, -e)
    if k >= 0:
        r = j_bn_shl(r, k)
    else:
        l = j_bn_shl(l, -k)
    return j_bn_cmp(l, r)

let j_two128 = f64frombits(0x47F0000000000000)
let j_flt_max = j_f32frombits(0x7F7FFFFF)

// j_parse_f32 is ONE correctly rounded conversion of a token to float32 (Go's
// ParseFloat(token, 32)): strtod, then float32 rounding, which only parts
// company with the exact answer when the double lands exactly on a float32
// midpoint — decided there against the token's exact value.
def j_parse_f32(token:string) -> float:
    let d = parse_float64(token)
    let neg = d < 0.0 or (d == 0.0 and (f64bits(d) & 0x8000000000000000) != 0)
    let a = if neg: -d else: d
    if a == 0.0 or j_is_inf(a):
        return d
    let f = fround(a)
    if f == a:
        return d
    var lo = 0.0
    var hi = 0.0
    if f > a:
        if j_is_inf(f):
            hi = j_two128
            lo = j_flt_max
        else:
            hi = f
            lo = j_f32frombits(f32bits(f) - 1)
    else:
        lo = f
        hi = if f == j_flt_max: j_two128 else: j_f32frombits(f32bits(f) + 1)
    let mid = (lo + hi) / 2.0
    var r = f
    if a == mid:
        let c = j_cmp_token_float(token, mid)
        if c > 0:
            r = if hi == j_two128: 1.0 / 0.0 else: hi
        elif c < 0:
            r = lo
    return if neg: -r else: r

// ---------------------------------------------------------------------------
// THE WIDE KINDS (Go's wide.go): exact in both directions
// ---------------------------------------------------------------------------

def j_wide_signed(kind:int) -> bool:
    return kind_signed(kind)

// j_parse_wide is a token as a wide kind's RAW value (value x 2^frac): exact
// is false for a finer fraction than Q I.F spells, saturated when the
// 128-bit domain clamped it.
def j_parse_wide(token:string, kind:int, frac:int) -> Int128, bool, bool:
    let neg = length(token) > 0 and token[0] == '-'
    let digits, e = j_token_decimal(token)
    let signed = j_wide_signed(kind)
    let lo = if signed: Int128 { 0, 0x8000000000000000 } else: Int128 { 0, 0 }
    let hi = if signed: Int128 { -1, 0x7FFFFFFFFFFFFFFF } else: Int128 { -1, -1 }
    if length(digits) == 0:
        return Int128 { 0, 0 }, true, false
    let point = e + length(digits)
    if point > 40:
        return (if neg: lo else: hi), true, true
    if point < -40:
        return Int128 { 0, 0 }, false, false
    var raw = j_bn_shl(j_bn_from_dec(digits), frac)
    if e >= 0:
        raw = j_bn_mul_pow10(raw, e)
    else:
        var k = -e
        while k > 0:
            let q, r = j_bn_divmod_small(raw, 10)
            if r != 0:
                return Int128 { 0, 0 }, false, false
            raw = q
            k--
    if neg:
        if not signed:
            return lo, true, true
        if j_bn_cmp(raw, j_bn_pow2(127)) > 0:
            return lo, true, true
        return j_i128_neg(j_bn_lanes(raw)), true, false
    let top = if signed: j_bn_sub(j_bn_pow2(127), [1]) else: j_bn_sub(j_bn_pow2(128), [1])
    if j_bn_cmp(raw, top) > 0:
        return hi, true, true
    return j_bn_lanes(raw), true, false

// j_format_wide spells a raw wide value as §16.2's text (Go's FormatWide).
def j_format_wide(w:Int128, kind:int, frac:int) -> string:
    let neg = j_wide_signed(kind) and w.hi < 0
    let mag = j_bn_of_lanes(if neg: j_i128_neg(w) else: w)
    let sign = if neg: "-" else: ""
    if kind == 18 or kind == 19:
        return sign + j_bn_to_dec(mag)
    let whole = j_bn_shr(mag, frac)
    var fraction = j_bn_low(mag, frac)
    let out = sign + j_bn_to_dec(whole) + "."
    if length(fraction) == 0:
        return out + "0"
    let parts:[string] = []
    while length(fraction) > 0:
        fraction = j_bn_mul_small(fraction, 10)
        let digit = j_bn_shr(fraction, frac)
        push(parts, j_bytes['0' + (if length(digit) > 0: digit[0] else: 0)])
        fraction = j_bn_low(fraction, frac)
    return out + concat_string(parts, "")

// ---------------------------------------------------------------------------
// TIMESTAMPS (§2.11): strict RFC 3339 in, RFC3339Nano UTC out
// ---------------------------------------------------------------------------

def j_digit(t:string, i:int) -> int:
    if i < 0 or i >= length(t) or t[i] < '0' or t[i] > '9':
        return -1
    return t[i] - '0'

def j_two(t:string, i:int) -> int:
    let a = j_digit(t, i)
    let b = j_digit(t, i + 1)
    if a < 0 or b < 0:
        return -1
    return a * 10 + b

def j_leap(y:int) -> bool:
    return y % 4 == 0 and (y % 100 != 0 or y % 400 == 0)

def j_month_days(y:int, m:int) -> int:
    if m == 2:
        return if j_leap(y): 29 else: 28
    if m == 4 or m == 6 or m == 9 or m == 11:
        return 30
    return 31

def j_floor_div(a:int, b:int) -> int:
    var q = a / b
    if (a % b != 0) and ((a < 0) != (b < 0)):
        q--
    return q

def j_days(year:int, month:int, day:int) -> int:
    var y = year
    if month <= 2:
        y--
    let era = j_floor_div(y, 400)
    let yoe = y - era * 400
    let moy = if month > 2: month - 3 else: month + 9
    let doy = (153 * moy + 2) / 5 + day - 1
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy
    return era * 146097 + doe - 719468

// j_rfc3339_parse is internal/rfc3339's Parse: the UTC nanosecond count.
def j_rfc3339_parse(t:string) -> int, bool:
    let n = length(t)
    if n < 20:
        return 0, false
    if t[4] != '-' or t[7] != '-' or t[13] != ':' or t[16] != ':':
        return 0, false
    if t[10] != 'T' and t[10] != 't':
        return 0, false
    var year = 0
    for(4) i:
        let d = j_digit(t, i)
        if d < 0:
            return 0, false
        year = year * 10 + d
    let month = j_two(t, 5)
    let day = j_two(t, 8)
    let hour = j_two(t, 11)
    let minute = j_two(t, 14)
    let second = j_two(t, 17)
    if month < 0 or day < 0 or hour < 0 or minute < 0 or second < 0:
        return 0, false
    if month < 1 or month > 12:
        return 0, false
    if day < 1 or day > j_month_days(year, month):
        return 0, false
    if hour > 23 or minute > 59 or second > 59:
        return 0, false
    var i = 19
    var nanos = 0
    if i < n and t[i] == '.':
        i++
        var digits = 0
        while i < n and t[i] >= '0' and t[i] <= '9':
            if digits < 9:
                nanos = nanos * 10 + (t[i] - '0')
            digits++
            i++
        if digits == 0:
            return 0, false
        while digits < 9:
            nanos *= 10
            digits++
    var offset = 0
    if i >= n:
        return 0, false
    let c = t[i]
    if c == 'Z' or c == 'z':
        i++
        if i != n:
            return 0, false
    elif c == '+' or c == '-':
        let sign = if c == '-': -1 else: 1
        i++
        let oh = j_two(t, i)
        if oh < 0:
            return 0, false
        i += 2
        if i >= n or t[i] != ':':
            return 0, false
        i++
        let om = j_two(t, i)
        if om < 0:
            return 0, false
        i += 2
        if oh > 23 or om > 59 or i != n:
            return 0, false
        offset = sign * (oh * 3600 + om * 60)
    else:
        return 0, false
    let seconds = j_days(year, month, day) * 86400 + hour * 3600 + minute * 60 + second - offset
    if seconds < -9223372037 or seconds > 9223372036:
        return 0, false
    if seconds == -9223372037 and nanos < 145224192:
        return 0, false
    if seconds == 9223372036 and nanos > 854775807:
        return 0, false
    return seconds * 1000000000 + nanos, true

def j_pad(v:int, n:int) -> string:
    return number_to_string(v, 10, n)

// j_rfc3339_format is Go's time.Unix(0, nanos).UTC() in RFC3339Nano.
def j_rfc3339_format(nanos:int) -> string:
    let secs = j_floor_div(nanos, 1000000000)
    let ns = nanos - secs * 1000000000
    let days = j_floor_div(secs, 86400)
    let rem = secs - days * 86400
    let z = days + 719468
    let era = j_floor_div(z, 146097)
    let doe = z - era * 146097
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100)
    let mp = (5 * doy + 2) / 153
    let d = doy - (153 * mp + 2) / 5 + 1
    let m = if mp < 10: mp + 3 else: mp - 9
    let y = yoe + era * 400 + (if m <= 2: 1 else: 0)
    var out = j_pad(y, 4) + "-" + j_pad(m, 2) + "-" + j_pad(d, 2) + "T" + j_pad(rem / 3600, 2) + ":" + j_pad((rem % 3600) / 60, 2) + ":" + j_pad(rem % 60, 2)
    if ns != 0:
        out += "." + j_strip_zeros(j_pad(ns, 9))
    return out + "Z"

// ---------------------------------------------------------------------------
// THE READ (Go's read.go)
// ---------------------------------------------------------------------------

class JsonLabel:
    label:int
    node:Record?
    st:string
    open:bool

class JsonReader:
    text:string
    pos:int = 0
    report:LoadReport
    bad:bool = false
    graph:bool = false
    labels:[JsonLabel] = []
    zero:bool = false
    utf8:bool = false

def j_space(r:JsonReader):
    let t = r.text
    let n = length(t)
    while r.pos < n:
        let c = t[r.pos]
        if c == ' ' or c == '\t' or c == '\n' or c == '\r':
            r.pos++
            continue
        if c == '/' and r.pos + 1 < n and t[r.pos + 1] == '/':
            r.pos += 2
            while r.pos < n and t[r.pos] != '\n':
                r.pos++
            continue
        if c == '/' and r.pos + 1 < n and t[r.pos + 1] == '*':
            let end = find_string(t, "*/", r.pos + 2)
            if end < 0:
                r.bad = true
                r.pos = n
                return
            r.pos = end + 2
            continue
        if c == '/':
            r.bad = true
        return

def j_peek(r:JsonReader) -> int:
    j_space(r)
    if r.pos < length(r.text):
        return r.text[r.pos]
    return 0

def j_value_shape(r:JsonReader) -> int:
    let c = j_peek(r)
    if c == '{':
        return 'o'
    if c == '[':
        return 'a'
    if c == '\"':
        return 's'
    if c == 't' or c == 'f':
        return 'b'
    if c == 'n':
        return 'z'
    if c == 0:
        return 0
    return 'n'

def j_literal(r:JsonReader, word:string) -> bool:
    let n = length(word)
    if r.pos + n > length(r.text) or substring(r.text, r.pos, n) != word:
        r.bad = true
        return false
    r.pos += n
    return true

def j_hex4(r:JsonReader) -> int:
    if r.pos + 4 > length(r.text):
        return -1
    var v = 0
    for(4) i:
        let c = r.text[r.pos + i]
        var d = 0
        if c >= '0' and c <= '9':
            d = c - '0'
        elif c >= 'a' and c <= 'f':
            d = c - 'a' + 10
        elif c >= 'A' and c <= 'F':
            d = c - 'A' + 10
        else:
            return -1
        v = (v << 4) | d
    r.pos += 4
    return v

def j_utf8(code:int) -> string:
    if code < 0x80:
        return j_bytes[code]
    if code < 0x800:
        return j_bytes[0xC0 | (code >> 6)] + j_bytes[0x80 | (code & 0x3F)]
    if code < 0x10000:
        return j_bytes[0xE0 | (code >> 12)] + j_bytes[0x80 | ((code >> 6) & 0x3F)] + j_bytes[0x80 | (code & 0x3F)]
    return j_bytes[0xF0 | (code >> 18)] + j_bytes[0x80 | ((code >> 12) & 0x3F)] + j_bytes[0x80 | ((code >> 6) & 0x3F)] + j_bytes[0x80 | (code & 0x3F)]

// j_scan_unit reads ONE string body character as UTF-8 (Go's scanUnit): the
// unit, whether the closing quote was consumed, and whether it is JSON.
def j_scan_unit(r:JsonReader) -> string, bool, bool:
    let t = r.text
    let n = length(t)
    if r.pos >= n:
        r.bad = true
        return "", false, false
    let c = t[r.pos]
    if c == '\"':
        r.pos++
        return "", true, true
    if c == '\\':
        r.pos++
        if r.pos >= n:
            r.bad = true
            return "", false, false
        let esc = t[r.pos]
        r.pos++
        if esc == '\"' or esc == '\\' or esc == '/':
            return j_bytes[esc], false, true
        if esc == 'b':
            return j_bytes[8], false, true
        if esc == 'f':
            return j_bytes[12], false, true
        if esc == 'n':
            return j_bytes[10], false, true
        if esc == 'r':
            return j_bytes[13], false, true
        if esc == 't':
            return j_bytes[9], false, true
        if esc == 'u':
            let high = j_hex4(r)
            if high < 0:
                r.bad = true
                return "", false, false
            var code = high
            if high >= 0xD800 and high <= 0xDBFF and r.pos + 2 <= n and t[r.pos] == '\\' and t[r.pos + 1] == 'u':
                let mark = r.pos
                r.pos += 2
                let low = j_hex4(r)
                if low >= 0xDC00 and low <= 0xDFFF:
                    code = 0x10000 + ((high - 0xD800) << 10) + (low - 0xDC00)
                else:
                    r.pos = mark
            if code >= 0xD800 and code <= 0xDFFF:
                code = 0xFFFD
            return j_utf8(code), false, true
        r.bad = true
        return "", false, false
    if c < 0x20:
        r.bad = true
        return "", false, false
    var want = 1
    if (c & 0xE0) == 0xC0:
        want = 2
    elif (c & 0xF0) == 0xE0:
        want = 3
    elif (c & 0xF8) == 0xF0:
        want = 4
    let at = r.pos
    r.pos++
    var got = 1
    while got < want and r.pos < n and (t[r.pos] & 0xC0) == 0x80:
        r.pos++
        got++
    if c >= 0x80 and not text_valid(t, at, got):
        r.utf8 = true
        if r.report.reason == "":
            r.report.reason = "string_not_utf8: <root>"
        r.bad = true
        return "", false, false
    return substring(t, at, got), false, true

// j_scan_text reads one JSON string a code point at a time, clamping AT A
// CODE POINT BOUNDARY as a prefix (Go's scanText); capacity < 0 is
// unbounded, store refuses a zero bound for text storage (§16.3). It answers
// the text, whether anything was dropped, and whether it is JSON.
def j_scan_text(r:JsonReader, capacity:int, store:bool) -> string, bool, bool:
    if j_peek(r) != '\"':
        r.bad = true
        return "", false, false
    r.pos++
    let t = r.text
    let n = length(t)
    let parts:[string] = []
    var size = 0
    var clamped = false
    while true:
        // a run of plain ASCII, placed whole
        let start = r.pos
        while r.pos < n:
            let c = t[r.pos]
            if c < 0x20 or c >= 0x80 or c == '\"' or c == '\\':
                break
            r.pos++
        let run = r.pos - start
        if run > 0 and not clamped:
            var take = run
            if capacity >= 0 and size + run > capacity:
                take = capacity - size
                clamped = true
            if take > 0:
                push(parts, substring(t, start, take))
                size += take
        elif run > 0:
            clamped = true
        let unit, done, ok = j_scan_unit(r)
        if not ok:
            return "", false, false
        if done:
            break
        if store and length(unit) == 1 and unit[0] == 0:
            r.zero = true
            r.bad = true
            return "", false, false
        if clamped or (capacity >= 0 and size + length(unit) > capacity):
            clamped = true
            continue
        push(parts, unit)
        size += length(unit)
    return concat_string(parts, ""), clamped, true

def j_scan_string(r:JsonReader, capacity:int) -> string, bool, bool:
    return j_scan_text(r, capacity, false)

def j_scan_stored(r:JsonReader, capacity:int) -> string, bool, bool:
    return j_scan_text(r, capacity, true)

// j_decode_utf8 is the code point of one well-formed UTF-8 unit.
def j_decode_utf8(u:string) -> int:
    let c = u[0]
    if c < 0x80:
        return c
    if length(u) == 2:
        return ((c & 0x1F) << 6) | (u[1] & 0x3F)
    if length(u) == 3:
        return ((c & 0x0F) << 12) | ((u[1] & 0x3F) << 6) | (u[2] & 0x3F)
    return ((c & 0x07) << 18) | ((u[1] & 0x3F) << 12) | ((u[2] & 0x3F) << 6) | (u[3] & 0x3F)

// j_scan_wstring reads one JSON string into UTF-16 code units, a pair never
// split by the clamp (Go's scanWString).
def j_scan_wstring(r:JsonReader, capacity:int) -> [int], bool, bool:
    let out:[int] = []
    if j_peek(r) != '\"':
        r.bad = true
        return out, false, false
    r.pos++
    var clamped = false
    while true:
        let unit, done, ok = j_scan_unit(r)
        if not ok:
            return [], false, false
        if done:
            break
        let code = j_decode_utf8(unit)
        if code == 0:
            r.zero = true
            r.bad = true
            return [], false, false
        let units:[int] = []
        if code < 0x10000:
            push(units, code)
        else:
            let rest = code - 0x10000
            push(units, 0xD800 + (rest >> 10))
            push(units, 0xDC00 + (rest & 0x3FF))
        if clamped or (capacity >= 0 and length(out) + length(units) > capacity):
            clamped = true
            continue
        for(units) u:
            push(out, u)
    return out, clamped, true

def j_is_digit(c:int) -> bool:
    return c >= '0' and c <= '9'

// j_walk_number steps over ONE RFC 8259 number (Go's walkNumber): whether
// it was integral in form, and whether it is one.
def j_walk_number(r:JsonReader) -> bool, bool:
    j_space(r)
    let t = r.text
    let n = length(t)
    var integral = true
    if r.pos < n and t[r.pos] == '-':
        r.pos++
    if r.pos >= n:
        return false, false
    let c = t[r.pos]
    if c == '0':
        r.pos++
    elif c >= '1' and c <= '9':
        while r.pos < n and j_is_digit(t[r.pos]):
            r.pos++
    else:
        return false, false
    if r.pos < n and t[r.pos] == '.':
        r.pos++
        if r.pos >= n or not j_is_digit(t[r.pos]):
            return false, false
        while r.pos < n and j_is_digit(t[r.pos]):
            r.pos++
        integral = false
    if r.pos < n and (t[r.pos] == 'e' or t[r.pos] == 'E'):
        r.pos++
        if r.pos < n and (t[r.pos] == '-' or t[r.pos] == '+'):
            r.pos++
        if r.pos >= n or not j_is_digit(t[r.pos]):
            return false, false
        while r.pos < n and j_is_digit(t[r.pos]):
            r.pos++
        integral = false
    return integral, true

def j_scan_number(r:JsonReader) -> string, bool, bool:
    j_space(r)
    let start = r.pos
    let integral, ok = j_walk_number(r)
    if not ok or r.pos <= start:
        return "", false, false
    return substring(r.text, start, r.pos - start), integral, true

// j_skip_string steps over one string the walk does not keep.
def j_skip_string(r:JsonReader) -> bool:
    let skipped, clamped, ok = j_scan_string(r, 0)
    return ok and (clamped or length(skipped) == 0)

def j_skip_value(r:JsonReader, depth:int) -> bool:
    let c = j_peek(r)
    if c == '{':
        return j_skip_container(r, '}', depth)
    if c == '[':
        return j_skip_container(r, ']', depth)
    if c == '\"':
        return j_skip_string(r)
    if c == 't':
        return j_literal(r, "true")
    if c == 'f':
        return j_literal(r, "false")
    if c == 'n':
        return j_literal(r, "null")
    if c == 0:
        r.bad = true
        return false
    let _, ok = j_walk_number(r)
    if not ok:
        r.bad = true
    return ok

def j_skip_container(r:JsonReader, close:int, depth:int) -> bool:
    if depth > json_max_depth:
        r.bad = true
        return false
    r.pos++
    var first = true
    while true:
        var c = j_peek(r)
        if c == close:
            r.pos++
            return true
        if c == 0:
            r.bad = true
            return false
        var keyed = false
        if close == '}':
            let key, _, ok = j_scan_string(r, json_max_key)
            if not ok:
                return false
            if j_peek(r) != ':':
                r.bad = true
                return false
            r.pos++
            if length(key) > 0 and key[0] == '&' and r.graph:
                if not first or not j_skipped_ampersand(r, key):
                    r.bad = true
                    return false
                first = false
                keyed = true
                c = j_peek(r)
                if c == ',':
                    r.pos++
                    continue
                if c == close:
                    r.pos++
                    return true
                r.bad = true
                return false
        if not keyed:
            first = false
            if not j_skip_value(r, depth + 1):
                return false
            c = j_peek(r)
            if c == ',':
                r.pos++
                continue
            if c == close:
                r.pos++
                return true
            r.bad = true
            return false
    return false

// j_find_label is the label entry a number names, or nil.
def j_find_label(r:JsonReader, label:int) -> JsonLabel?:
    for(r.labels) x:
        if x.label == label:
            return x
    return nil

// j_skipped_ampersand takes the label of a definition inside a value the walk
// is skipping, so the numbering survives the drop (§16.7).
def j_skipped_ampersand(r:JsonReader, key:string) -> bool:
    if not r.graph or key != "&node":
        return false
    let label, ok = j_scan_label(r)
    if not ok:
        return false
    if not j_find_label(r, label):
        push(r.labels, JsonLabel { label: label, node: nil, st: "", open: false })
    return true

// j_scan_label reads `&node`'s value: a positive integer, no sign, fraction,
// exponent or leading zero, within 64 bits (§16.7).
def j_scan_label(r:JsonReader) -> int, bool:
    j_space(r)
    let t = r.text
    let n = length(t)
    if r.pos >= n or t[r.pos] < '1' or t[r.pos] > '9':
        r.bad = true
        return 0, false
    var v = 0
    while r.pos < n and j_is_digit(t[r.pos]):
        let d = t[r.pos] - '0'
        let limit = if d <= 5: 1844674407370955161 else: 1844674407370955160
        if ult(limit, v):
            r.bad = true
            return 0, false
        v = v * 10 + d
        r.pos++
    return v, true

def j_field_index(rec:Record, key:string) -> int:
    for(rec.fields) sl, i:
        if sl.fd.json == key:
            return i
    return -1

def j_name_zero(r:JsonReader, table:string, sl:Slot):
    if r.zero and r.report.reason == "":
        r.report.reason = "text_holds_zero: " + table + "." + sl.fd.name

// j_read_table places ONE table object (Go's readTable).
def j_read_table(r:JsonReader, rec:Record, depth:int) -> bool:
    if depth > json_max_depth:
        r.bad = true
        return false
    if json_is_array_table(rec.d.name):
        if length(rec.fields) != 1:
            r.bad = true
            return false
        if j_value_shape(r) != 'a':
            if r.report.reason == "":
                r.report.reason = "json_root_not_array: " + rec.d.name
            r.bad = true
            return false
        if not j_read_field(r, rec.fields[0], depth):
            j_name_zero(r, rec.d.name, rec.fields[0])
            return false
        return true
    if j_peek(r) != '{':
        r.bad = true
        return false
    r.pos++
    return j_read_table_keys(r, rec, depth, "", false)

// j_read_table_keys places the keys of an object whose brace is consumed;
// a pointer's reader hands in the first key it scanned (Go's readTableKeys).
def j_read_table_keys(r:JsonReader, rec:Record, depth:int, first_key:string, has_first:bool) -> bool:
    let seen:[bool] = []
    for(rec.fields):
        push(seen, false)
    var pending = has_first
    while true:
        var key = ""
        if pending:
            key = first_key
            pending = false
        else:
            let c = j_peek(r)
            if c == '}':
                r.pos++
                return true
            if c == 0:
                r.bad = true
                return false
            let k, _, ok = j_scan_string(r, json_max_key)
            if not ok:
                return false
            if j_peek(r) != ':':
                r.bad = true
                return false
            r.pos++
            key = k
        if length(key) > 0 and key[0] == '&':
            // THE AMPERSAND PREFIX IS RESERVED TO THE FORM (§16.7)
            r.bad = true
            return false
        let index = j_field_index(rec, key)
        if index < 0:
            r.report.unknown++
            if not j_skip_value(r, depth + 1):
                return false
        else:
            if seen[index]:
                r.report.duplicate++
            seen[index] = true
            let sl = rec.fields[index]
            let shape = j_value_shape(r)
            if shape == 'z' and j_takes_null(sl.fd):
                if not j_literal(r, "null"):
                    return false
                reset_slot(sl)
            else:
                if shape != j_shape(sl.fd):
                    r.report.kind_mismatch++
                    if not j_skip_value(r, depth + 1):
                        return false
                elif not j_read_field(r, sl, depth):
                    j_name_zero(r, rec.d.name, sl)
                    return false
                if sl.fd.present != "":
                    sl.present = true
        let c = j_peek(r)
        if c == ',':
            r.pos++
            continue
        if c == '}':
            r.pos++
            return true
        r.bad = true
        return false
    return false

def j_read_field(r:JsonReader, sl:Slot, depth:int) -> bool:
    let f = sl.fd
    let s = f.s
    if s == "sblob" or s == "bblob":
        return j_read_blob(r, sl)
    if s == "ptr":
        return j_read_pointer_cell(r, sl.cell, f.t, depth)
    if s == "text" or (s == "str" and not f.wide):
        let out, clamped, ok = j_scan_stored(r, f.bound)
        if not ok:
            return false
        if clamped:
            r.report.clamped++
        sl.cell.s = out
        sl.count = length(out)
        return true
    if s == "str":
        let units, clamped, ok = j_scan_wstring(r, f.bound)
        if not ok:
            return false
        if clamped:
            r.report.clamped++
        sl.cell.units = units
        sl.count = length(units)
        return true
    if s == "bytes":
        return j_read_base64(r, sl, f.bound)
    if s == "map":
        return j_read_map(r, sl, depth)
    if s == "arr":
        if f.array == "keyed":
            return j_read_keyed(r, sl, depth)
        if j_is_text_list(f):
            return j_read_text_list(r, sl, depth)
        return j_read_array(r, sl, depth)
    return j_read_scalar(r, sl.cell, f, depth, false)

// j_read_blob places a BYTE BUFFER's string (§2.5, §16.2).
def j_read_blob(r:JsonReader, sl:Slot) -> bool:
    sl.cell.blob = nil
    sl.cell.blob_node = nil
    if sl.fd.s == "sblob":
        let out, _, ok = j_scan_stored(r, -1)
        if not ok:
            return false
        sl.cell.blob = out
        return true
    let before = r.report.kind_mismatch
    if not j_read_base64(r, sl, -1):
        return false
    let data = sl.cell.s
    sl.cell.s = ""
    sl.count = 0
    if r.report.kind_mismatch != before:
        return true
    sl.cell.blob = data
    return true

// j_read_base64 decodes a bytes body with §16.2's padding grammar; a body
// outside it is the wrong shape, counted, the field left at its default.
def j_read_base64(r:JsonReader, sl:Slot, bound:int) -> bool:
    let raw, _, ok = j_scan_string(r, -1)
    if not ok:
        return false
    sl.cell.s = ""
    sl.count = 0
    var symbols = 0
    var pad = 0
    var malformed = false
    for(raw) c:
        if not malformed:
            if c == '=':
                pad++
            elif pad > 0:
                malformed = true
            elif j_b64[c] < 0:
                malformed = true
            else:
                symbols++
    if not malformed:
        if pad > 0:
            if pad > 2 or (symbols + pad) % 4 != 0 or symbols % 4 < 2:
                malformed = true
        elif symbols % 4 == 1:
            malformed = true
    if malformed:
        r.report.kind_mismatch++
        return true
    let total = symbols * 6 / 8
    var keep = total
    if bound >= 0 and keep > bound:
        keep = bound
        r.report.clamped++
    var out = zeros(keep)
    var acc = 0
    var held = 0
    var at = 0
    var i = 0
    while i < symbols and at < keep:
        acc = ((acc << 6) | j_b64[raw[i]]) & 0xFFFFFF
        held += 6
        if held >= 8:
            held -= 8
            out = write_int8_le(out, at, (acc >> held) & 0xFF)
            at++
        i++
    sl.cell.s = out
    sl.count = keep
    return true

def j_read_array(r:JsonReader, sl:Slot, depth:int) -> bool:
    let f = sl.fd
    let e = elem_of(f)
    if j_peek(r) != '[':
        r.bad = true
        return false
    r.pos++
    for(length(sl.elems)) i:
        sl.elems[i] = element_zero(f)
    sl.count = 0
    var placed = 0
    let shape = j_element_shape(e)
    var bound = f.bound
    let list = f.array == "list"
    if list:
        bound = 0x7FFFFFFF
        sl.elems = []
    while true:
        var c = j_peek(r)
        if c == ']':
            r.pos++
            break
        if c == 0:
            r.bad = true
            return false
        if list and placed == length(sl.elems):
            push(sl.elems, element_zero(f))
        if placed >= bound:
            r.report.clamped++
            if not j_skip_value(r, depth + 1):
                return false
        elif e.s == "ptr" and j_value_shape(r) == 'z':
            if not j_literal(r, "null"):
                return false
            sl.elems[placed] = Value {}
            placed++
        elif j_value_shape(r) != shape:
            r.report.kind_mismatch++
            if not j_skip_value(r, depth + 1):
                return false
            placed++
        else:
            if not j_read_scalar(r, sl.elems[placed], e, depth + 1, true):
                return false
            placed++
        c = j_peek(r)
        if c == ',':
            r.pos++
            continue
        if c == ']':
            r.pos++
            break
        r.bad = true
        return false
    sl.count = placed
    return true

// j_read_text_list places a TEXT LIST's plain array of strings (§2.13).
def j_read_text_list(r:JsonReader, sl:Slot, depth:int) -> bool:
    let f = sl.fd
    if j_peek(r) != '[':
        r.bad = true
        return false
    r.pos++
    for(length(sl.elems)) i:
        sl.elems[i] = element_zero(f)
    sl.count = 0
    var placed = 0
    var bound = f.bound
    let list = f.array == "list"
    if list:
        bound = 0x7FFFFFFF
        sl.elems = []
    let vf = table_of(elem_of(f).t).fields[0]
    let optional = vf.present != ""
    while true:
        var c = j_peek(r)
        if c == ']':
            r.pos++
            break
        if c == 0:
            r.bad = true
            return false
        if list and placed == length(sl.elems):
            push(sl.elems, element_zero(f))
        if placed >= bound:
            r.report.clamped++
            if not j_skip_value(r, depth + 1):
                return false
        elif optional and j_value_shape(r) == 'z':
            if not j_literal(r, "null"):
                return false
            placed++
        elif j_value_shape(r) != 's':
            r.report.kind_mismatch++
            if not j_skip_value(r, depth + 1):
                return false
            placed++
        else:
            let out, clamped, ok = j_scan_stored(r, vf.bound)
            if not ok:
                return false
            if clamped:
                r.report.clamped++
            let t = sl.elems[placed].tab
            if t:
                t.fields[0].cell.s = out
                t.fields[0].count = length(out)
                if optional:
                    t.fields[0].present = true
            placed++
        c = j_peek(r)
        if c == ',':
            r.pos++
            continue
        if c == ']':
            r.pos++
            break
        r.bad = true
        return false
    sl.count = placed
    return true

// j_read_keyed places an enum-keyed array: an OBJECT keyed by variant name
// (§2.4, §16.2).
def j_read_keyed(r:JsonReader, sl:Slot, depth:int) -> bool:
    let f = sl.fd
    let e = elem_of(f)
    if j_peek(r) != '{':
        r.bad = true
        return false
    r.pos++
    for(length(sl.elems)) i:
        sl.elems[i] = element_zero(f)
    let shape = j_element_shape(e)
    let keys = keyed_variants(f)
    let seen:[bool] = []
    for(f.bound):
        push(seen, false)
    while true:
        var c = j_peek(r)
        if c == '}':
            r.pos++
            break
        if c == 0:
            r.bad = true
            return false
        let key, _, ok = j_scan_string(r, json_max_key)
        if not ok:
            return false
        if j_peek(r) != ':':
            r.bad = true
            return false
        r.pos++
        let v = j_enum_value(keys, key)
        let slot = if v < 1 or v > f.bound: -1 else: v - 1
        if slot >= 0:
            if seen[slot]:
                r.report.duplicate++
            seen[slot] = true
        if slot < 0:
            r.report.unknown++
            if not j_skip_value(r, depth + 1):
                return false
        elif j_value_shape(r) != shape:
            r.report.kind_mismatch++
            if not j_skip_value(r, depth + 1):
                return false
        elif not j_read_scalar(r, sl.elems[slot], e, depth + 1, true):
            return false
        c = j_peek(r)
        if c == ',':
            r.pos++
            continue
        if c == '}':
            r.pos++
            break
        r.bad = true
        return false
    return true

// j_read_scalar places one value at one cell (Go's readScalar). elem: the
// cell is an array or keyed element, whose deprecated variant reads None.
def j_read_scalar(r:JsonReader, cell:Value, f:FieldDescriptor, depth:int, elem:bool) -> bool:
    let s = f.s
    if s == "int" and f.timestamp:
        let text, _, ok = j_scan_string(r, -1)
        if not ok:
            return false
        let nanos, named = j_rfc3339_parse(text)
        if not named:
            r.report.reason = "timestamp_not_rfc3339: " + f.name
            r.bad = true
            return false
        cell.i = nanos
        return true
    if s == "union":
        let un = lookup_union(f.t)
        if not un:
            r.bad = true
            return false
        return j_read_union(r, cell, un, depth)
    if s == "ptr":
        return j_read_pointer_cell(r, cell, f.t, depth)
    if s == "tab":
        let rec = new_record(table_of(f.t))
        cell.tab = rec
        return j_read_table(r, rec, depth + 1)
    if s == "enum":
        let name, _, ok = j_scan_string(r, json_max_key)
        if not ok:
            return false
        let v = j_enum_value(f.vs, name)
        if v >= 0:
            if v >= 1 and is_deprecated_variant(f.t, name):
                cell.i = if elem: 0 else: enum_default(f)
                return true
            cell.i = v
            return true
        cell.i = 0
        r.report.unknown++
        return true
    let fl = json_flags_of(f)
    if fl:
        return j_read_flags(r, cell, fl.vs, depth)
    if s == "bool":
        if j_peek(r) == 't':
            if not j_literal(r, "true"):
                return false
            cell.b = true
            return true
        if not j_literal(r, "false"):
            return false
        cell.b = false
        return true
    let token, integral, ok = j_scan_number(r)
    if not ok:
        r.bad = true
        return false
    let kind = j_kind(f)
    if kind == 10 or kind == 11:
        j_place_float(r, cell, f, token, kind == 10)
        return true
    if kind_wide(kind):
        j_place_wide(r, cell, f, token, kind)
        return true
    if kind >= 2 and kind <= 9:
        j_place_integer(r, cell, f, token, integral, kind)
        return true
    r.bad = true
    return false

def j_place_float(r:JsonReader, cell:Value, f:FieldDescriptor, token:string, single:bool):
    var v = 0.0
    if single:
        v = j_parse_f32(token)
    else:
        v = parse_float64(token)
    if j_is_inf(v) or v != v:
        r.report.kind_mismatch++
        return
    if f.franged:
        if v < f.flo:
            v = f.flo
            r.report.clamped++
        elif v > f.fhi:
            v = f.fhi
            r.report.clamped++
        elif v == 0.0:
            v = 0.0
    if single:
        let narrow = fround(v)
        if j_is_inf(narrow):
            r.report.kind_mismatch++
            return
        v = narrow
    cell.f = v

// JsonInt is ONE CHECKED NUMERIC INTERPRETATION (Go's jsonInteger): a sign,
// a magnitude (a uint64 bit pattern) and a status.
class JsonInt:
    mag:int = 0
    neg:bool = false
    fractional:bool = false
    saturated:bool = false
    finite:bool = true

def j_mul10_add(n:JsonInt, d:int) -> bool:
    let limit = if d <= 5: 1844674407370955161 else: 1844674407370955160
    if ult(limit, n.mag):
        n.mag = -1
        n.saturated = true
        return false
    n.mag = n.mag * 10 + d
    return true

// j_interpret_integer is THE FIELD'S INTERPRETATION (Go's interpretInteger).
def j_interpret_integer(token:string, integral:bool) -> JsonInt:
    let out = JsonInt {}
    if integral:
        var i = 0
        if i < length(token) and token[i] == '-':
            out.neg = true
            i++
        while i < length(token):
            if not j_mul10_add(out, token[i] - '0'):
                break
            i++
        if out.mag == 0:
            out.neg = false
        return out
    let d = parse_float64(token)
    if j_is_inf(d) or d != d:
        out.finite = false
        return out
    out.neg = (f64bits(d) & 0x8000000000000000) != 0
    let whole = if d < 0.0: -d else: d
    if whole >= 18446744073709551616.0:
        out.mag = -1
        out.saturated = true
        return out
    if whole < 4503599627370496.0:
        let t = int(whole)
        if float(t) != whole:
            out.fractional = true
            return out
        out.mag = t
    elif whole >= 9223372036854775808.0:
        out.mag = int(whole - 9223372036854775808.0) | 0x8000000000000000
    else:
        out.mag = int(whole)
    if out.mag == 0:
        out.neg = false
    return out

// j_interpret_exact is THE MAP KEY'S INTERPRETATION (Go's
// interpretIntegerExact): the token's own digits, no float64.
def j_interpret_exact(token:string) -> JsonInt:
    let out = JsonInt {}
    out.neg = length(token) > 0 and token[0] == '-'
    let digits, e = j_token_decimal(token)
    if length(digits) == 0:
        out.neg = false
        return out
    let point = length(digits) + e
    if point < length(digits):
        out.fractional = true
        return out
    if point > 20:
        out.mag = -1
        out.saturated = true
        return out
    for(digits) c:
        if not j_mul10_add(out, c - '0'):
            return out
    var k = length(digits)
    while k < point:
        if not j_mul10_add(out, 0):
            return out
        k++
    return out

def j_u64_float(x:int) -> float:
    if x >= 0:
        return float(x)
    return float(shr(x, 11)) * 2048.0 + float(x & 0x7FF)

def j_scale(n:JsonInt) -> float:
    let v = j_u64_float(n.mag)
    return if n.neg: -v else: v

def j_i128_float(v:Int128) -> float:
    if v.hi == 0:
        return j_u64_float(v.lo)
    if v.hi == -1 and v.lo < 0:
        return float(v.lo)
    return float(v.hi) * 18446744073709551616.0 + j_u64_float(v.lo)

def j_integer_of(bound:float) -> JsonInt:
    let out = JsonInt {}
    out.neg = bound < 0.0
    let whole = if bound < 0.0: -bound else: bound
    if whole >= 18446744073709551616.0:
        out.mag = -1
    elif whole >= 9223372036854775808.0:
        out.mag = int(whole - 9223372036854775808.0) | 0x8000000000000000
    else:
        out.mag = int(whole)
    if out.mag == 0:
        out.neg = false
    return out

// j_in_domain is THE TARGET DOMAIN (Go's integerInDomain): the value the
// storage holds and whether the domain moved it.
def j_in_domain(n:JsonInt, signed:bool, bytes:int) -> int, bool:
    var mag = n.mag
    var moved = false
    if signed:
        var high = 0x7FFFFFFFFFFFFFFF
        if bytes < 8:
            high = shl(1, bytes * 8 - 1) - 1
        if n.neg:
            let low = high + 1
            if ult(low, mag):
                mag = low
                moved = true
            return (~mag) + 1, moved
        if ult(high, mag):
            mag = high
            moved = true
        return mag, moved
    if n.neg:
        return 0, true
    var high = -1
    if bytes < 8:
        high = shl(1, bytes * 8) - 1
    if ult(high, mag):
        mag = high
        moved = true
    return mag, moved

def j_place_integer(r:JsonReader, cell:Value, f:FieldDescriptor, token:string, integral:bool, kind:int):
    let signed = kind >= 2 and kind <= 5
    let number = j_interpret_integer(token, integral)
    if not number.finite or number.fractional:
        r.report.kind_mismatch++
        return
    if number.saturated:
        r.report.clamped++
    var bounded = number
    var implied = false
    var lo = 0.0
    var hi = 0.0
    if f.bits > 0 and not f.ranged and f.bits < 64:
        implied = true
        hi = j_u64_float(shl(1, f.bits) - 1)
    if f.ranged:
        implied = true
        lo = j_i128_float(f.lo)
        hi = j_i128_float(f.hi)
    if implied:
        let sc = j_scale(number)
        if sc < lo:
            bounded = j_integer_of(lo)
            r.report.clamped++
        elif sc > hi:
            bounded = j_integer_of(hi)
            r.report.clamped++
    let value, moved = j_in_domain(bounded, signed, j_storage_bytes(f))
    if moved:
        r.report.clamped++
    cell.i = value

def j_place_wide(r:JsonReader, cell:Value, f:FieldDescriptor, token:string, kind:int):
    let parsed, exact, saturated = j_parse_wide(token, kind, f.fbits)
    var raw = parsed
    if not exact:
        r.report.kind_mismatch++
        return
    if saturated:
        r.report.clamped++
    if f.ranged:
        let signed = kind_signed(kind)
        let lo = i128_shl(f.lo, f.fbits)
        let hi = i128_shl(f.hi, f.fbits)
        if i128_lt(raw, lo, signed):
            raw = lo
            r.report.clamped++
        elif i128_lt(hi, raw, signed):
            raw = hi
            r.report.clamped++
    cell.w = raw

// j_read_union is an object with ONE key, the arm's name; {} is None (§2.6).
def j_read_union(r:JsonReader, cell:Value, un:UnionDescriptor, depth:int) -> bool:
    if j_peek(r) != '{':
        r.bad = true
        return false
    r.pos++
    cell.i = 0
    cell.tab = nil
    cell.arm = nil
    if j_peek(r) == '}':
        r.pos++
        return true
    let key, _, ok = j_scan_string(r, json_max_key)
    if not ok:
        return false
    if j_peek(r) != ':':
        r.bad = true
        return false
    r.pos++
    var tag = 0
    for(un.arms) a, i:
        if tag == 0 and a.name == key:
            tag = i + 1
    if tag == 0:
        r.report.unknown++
        if not j_skip_value(r, depth + 1):
            return false
    else:
        let a = un.arms[tag - 1]
        if a.s == "void":
            if j_value_shape(r) != 'z':
                r.report.kind_mismatch++
                if not j_skip_value(r, depth + 1):
                    return false
            else:
                if not j_literal(r, "null"):
                    return false
                cell.i = tag
        elif a.s == "tab":
            if j_value_shape(r) != 'o':
                r.report.kind_mismatch++
                if not j_skip_value(r, depth + 1):
                    return false
            else:
                let payload = new_record(table_of(a.t))
                if not j_read_table(r, payload, depth + 1):
                    return false
                cell.i = tag
                cell.tab = payload
        else:
            let fv = new_slot(a)
            let shape = j_value_shape(r)
            if shape == 'z' and j_takes_null(a):
                if not j_literal(r, "null"):
                    return false
                cell.i = tag
                cell.arm = fv
            elif shape != j_shape(a):
                r.report.kind_mismatch++
                if not j_skip_value(r, depth + 1):
                    return false
            else:
                if not j_read_field(r, fv, depth + 1):
                    return false
                cell.i = tag
                cell.arm = fv
    var c = j_peek(r)
    if c == ',':
        r.pos++
        c = j_peek(r)
    if c == '}':
        r.pos++
        return true
    r.bad = true
    return false

def j_read_flags(r:JsonReader, cell:Value, names:[string], depth:int) -> bool:
    if j_peek(r) != '[':
        r.bad = true
        return false
    r.pos++
    var bits = 0
    while true:
        var c = j_peek(r)
        if c == ']':
            r.pos++
            break
        if c == 0:
            r.bad = true
            return false
        if c != '\"':
            r.report.kind_mismatch++
            if not j_skip_value(r, depth + 1):
                return false
        else:
            let name, _, ok = j_scan_string(r, json_max_key)
            if not ok:
                return false
            var found = false
            for(names) v, bit:
                if not found and v == name:
                    bits = bits | shl(1, bit)
                    found = true
            if not found:
                r.report.unknown++
        c = j_peek(r)
        if c == ',':
            r.pos++
            continue
        if c == ']':
            r.pos++
            break
        r.bad = true
        return false
    cell.i = bits
    return true

// j_read_pointer_cell places one pointer SLOT from its object (§16.7): the
// first key `&node` with fields after it DEFINES a label, alone it REFERS
// to one; any other first key is a node named once.
def j_read_pointer_cell(r:JsonReader, cell:Value, tname:string, depth:int) -> bool:
    if not r.graph or tname == "":
        r.bad = true
        return false
    if depth + 1 > json_max_depth:
        r.bad = true
        return false
    if j_peek(r) != '{':
        r.bad = true
        return false
    r.pos++
    let d = table_of(tname)
    var c = j_peek(r)
    if c == '}':
        r.pos++
        cell.node = new_record(d)
        return true
    if c == 0:
        r.bad = true
        return false
    let key, _, ok = j_scan_string(r, json_max_key)
    if not ok:
        return false
    if j_peek(r) != ':':
        r.bad = true
        return false
    r.pos++
    if key != "&node":
        let node = new_record(d)
        cell.node = node
        return j_read_table_keys(r, node, depth + 1, key, true)
    let label, lok = j_scan_label(r)
    if not lok:
        return false
    let entry = j_find_label(r, label)
    c = j_peek(r)
    if c == ',':
        r.pos++
        c = j_peek(r)
    let bare = c == '}'
    var defined = false
    if entry:
        defined = true
    if bare != defined:
        r.bad = true
        return false
    if entry:
        r.pos++
        if entry.open:
            r.bad = true
            return false
        cell.node = nil
        if entry.st == "":
            return true
        if entry.st != tname:
            r.report.kind_mismatch++
            return true
        cell.node = entry.node
        return true
    let node = new_record(d)
    cell.node = node
    let opened = JsonLabel { label: label, node: nil, st: "", open: true }
    push(r.labels, opened)
    if not j_read_table_keys(r, node, depth + 1, "", false):
        return false
    opened.node = node
    opened.st = tname
    opened.open = false
    return true

// THE MAP (§2.8, §16): a JSON object keyed by the KEY; keys never clamp, a
// repeat is duplicate and last-wins whole, and the entries land ascending.
def j_read_map(r:JsonReader, sl:Slot, depth:int) -> bool:
    if j_peek(r) != '{':
        r.bad = true
        return false
    r.pos++
    sl.entries = []
    let ed = table_of(sl.fd.t)
    let kf = ed.fields[0]
    let string_key = key_is_string(kf)
    while true:
        var c = j_peek(r)
        if c == '}':
            r.pos++
            break
        if c == 0:
            r.bad = true
            return false
        let key, over, ok = j_scan_stored(r, json_max_key)
        if not ok:
            return false
        if j_peek(r) != ':':
            r.bad = true
            return false
        r.pos++
        let entry = new_record(ed)
        var placed = 0
        if not over:
            placed = j_place_map_key(entry, kf, key)
        if placed == 2:
            r.report.malformed = true
            r.bad = true
            return false
        var fits = true
        if string_key and placed == 0 and not over:
            fits = length(entry.fields[0].cell.s) <= kf.bound
        if over and string_key:
            r.report.clamped++
            if not j_skip_value(r, depth):
                return false
        elif over:
            r.report.kind_mismatch++
            if not j_skip_value(r, depth):
                return false
        elif placed == 1:
            r.report.kind_mismatch++
            if not j_skip_value(r, depth):
                return false
        elif not fits:
            r.report.clamped++
            if not j_skip_value(r, depth):
                return false
        elif not j_read_map_value(r, entry.fields[1], depth):
            return false
        else:
            var at = -1
            for(sl.entries) v, i:
                let t = v.tab
                if at < 0 and t and map_key_order(kf, t.fields[0].cell, entry.fields[0].cell) == 0:
                    at = i
            if at >= 0:
                r.report.duplicate++
                sl.entries[at] = Value { tab: entry }
            else:
                push(sl.entries, Value { tab: entry })
        c = j_peek(r)
        if c == ',':
            r.pos++
            continue
        if c == '}':
            r.pos++
            break
        r.bad = true
        return false
    sl.entries = j_sorted_entries(kf, sl.entries)
    return true

// j_sorted_entries is a stable ascending sort by key (Go's sort.SliceStable).
def j_sorted_entries(kf:FieldDescriptor, entries:[Value]) -> [Value]:
    let out:[Value] = []
    for(entries) v:
        var i = length(out)
        while i > 0 and j_entry_order(kf, out[i - 1], v) > 0:
            i--
        insert(out, i, v)
    return out

def j_entry_order(kf:FieldDescriptor, a:Value, b:Value) -> int:
    let ta = a.tab
    let tb = b.tab
    if ta and tb:
        return map_key_order(kf, ta.fields[0].cell, tb.fields[0].cell)
    return 0

def j_read_map_value(r:JsonReader, sl:Slot, depth:int) -> bool:
    if j_value_shape(r) == 'z' and j_takes_null(sl.fd):
        if not j_literal(r, "null"):
            return false
        reset_slot(sl)
        return true
    if not j_read_field(r, sl, depth):
        return false
    if sl.fd.present != "":
        sl.present = true
    return true

// j_place_map_key writes an object key into the entry (Go's placeMapKey):
// 0 placed, 1 a value the key kind does not hold, 2 not a number at all.
def j_place_map_key(entry:Record, kf:FieldDescriptor, key:string) -> int:
    let ks = entry.fields[0]
    if key_is_string(kf):
        ks.cell.s = key
        ks.count = length(key)
        return 0
    let probe = JsonReader { text: key, report: LoadReport {} }
    j_space(probe)
    if probe.pos != 0:
        return 2
    let _, ok = j_walk_number(probe)
    if not ok or probe.pos != length(key):
        return 2
    let number = j_interpret_exact(key)
    if number.fractional or number.saturated:
        return 1
    let signed = kf.s == "int" and kf.signed and not j_is_fixed(kf)
    let value, moved = j_in_domain(number, signed, j_storage_bytes(kf))
    if moved:
        return 1
    ks.cell.i = value
    return 0

// json_read_into fills rec from one JSON text (docs/SPEC-TABLES.md §16.1):
// false when the text is not JSON or a value could not be placed, with
// report.malformed set; rec keeps what was placed before the stop.
def json_read_into(rec:Record, text:string, report:LoadReport) -> bool:
    let r = JsonReader { text: text, report: report, graph: rec.d.variable }
    if length(text) >= 3 and text[0] == 0xEF and text[1] == 0xBB and text[2] == 0xBF:
        r.pos = 3
    let ok = j_read_table(r, rec, 0)
    if ok:
        j_space(r)
        if r.pos != length(text):
            r.bad = true
    if r.bad or not ok:
        report.malformed = true
        return false
    return true

// json_read reads one JSON text at the root table named root (FromJson):
// the record, or nil with report.malformed set.
def json_read(root:string, text:string, report:LoadReport) -> Record?:
    let rec = new_record(table_of(root))
    if json_read_into(rec, text, report):
        return rec
    return nil

// ---------------------------------------------------------------------------
// THE WRITE (Go's write.go): every field in declaration order, defaults
// included, two-space indent, `": "`, the C++ writer's bytes
// ---------------------------------------------------------------------------

class JsonNodeCount:
    rec:Record
    count:int = 0
    label:int = 0
    open:bool = false

class JsonBlobCount:
    node:Node
    count:int = 0

class JsonWriter:
    parts:[string] = []
    nodes:[JsonNodeCount] = []
    blobs:[JsonBlobCount] = []
    next:int = 0
    err:string = ""

def j_indents() -> [string]:
    let out:[string] = []
    var s = "\n"
    for(json_max_depth + 4):
        push(out, s)
        s += "  "
    return out

let j_indent = j_indents()

def j_raw(w:JsonWriter, s:string):
    push(w.parts, s)

def j_line(w:JsonWriter, depth:int):
    if depth < length(j_indent):
        push(w.parts, j_indent[depth])
    else:
        var s = "\n"
        for(depth):
            s += "  "
        push(w.parts, s)

def j_fail(w:JsonWriter, why:string) -> bool:
    if w.err == "":
        w.err = why
    return false

def j_node_entry(w:JsonWriter, rec:Record) -> JsonNodeCount:
    for(w.nodes) x:
        if x.rec == rec:
            return x
    let x = JsonNodeCount { rec: rec }
    push(w.nodes, x)
    return x

// THE FIRST PASS (§16.7): every slot that names a node counted, a node whose
// descent is still open refused as the cycle it is.
def j_count_record(w:JsonWriter, rec:Record) -> bool:
    for(rec.fields) sl:
        if json_guard_holds(rec, sl) and (sl.fd.present == "" or sl.present):
            if not j_count_field(w, sl):
                return false
    return true

def j_count_field(w:JsonWriter, sl:Slot) -> bool:
    let f = sl.fd
    let s = f.s
    if s == "sblob" or s == "bblob":
        let n = sl.cell.blob_node
        if sl.cell.blob and n:
            var found = false
            for(w.blobs) b:
                if b.node == n:
                    b.count++
                    found = true
            if not found:
                push(w.blobs, JsonBlobCount { node: n, count: 1 })
        return true
    if s == "ptr":
        return j_count_node(w, sl.cell.node)
    if s == "str" or s == "text" or s == "bytes":
        return true
    if s == "map":
        for(sl.entries) v:
            let t = v.tab
            if t:
                if not j_count_record(w, t):
                    return false
        return true
    if s == "arr":
        let e = elem_of(f)
        var count = f.bound
        if f.array == "counted" or f.array == "list":
            count = sl.count
        var i = 0
        while i < count and i < length(sl.elems):
            if not j_count_cell(w, sl.elems[i], e):
                return false
            i++
        return true
    return j_count_cell(w, sl.cell, f)

def j_count_cell(w:JsonWriter, cell:Value, f:FieldDescriptor) -> bool:
    if f.s == "ptr":
        return j_count_node(w, cell.node)
    if f.s == "union":
        if cell.i != 0:
            let t = cell.tab
            if t:
                return j_count_record(w, t)
            let a = cell.arm
            if a:
                return j_count_field(w, a)
        return true
    if f.s == "tab":
        let t = cell.tab
        if t:
            return j_count_record(w, t)
    return true

def j_count_node(w:JsonWriter, node:Record?) -> bool:
    if not node:
        return true
    let x = j_node_entry(w, node)
    x.count++
    if x.open:
        return j_fail(w, "data cycle: a " + node.d.name + " reaches a node whose descent is still open (docs/SPEC-TABLES.md §3.1, §16.7)")
    if x.count > 1:
        return true
    x.open = true
    if not j_count_record(w, node):
        return false
    x.open = false
    return true

def j_write_value(w:JsonWriter, rec:Record, depth:int) -> bool:
    if depth > json_max_depth:
        return j_fail(w, "table " + rec.d.name + ": nested past the depth cap (docs/SPEC-TABLES.md §16.7)")
    if json_is_array_table(rec.d.name):
        if length(rec.fields) != 1:
            return j_fail(w, "table " + rec.d.name + ": a json = \"array\" table must declare exactly one array field (docs/SPEC-TABLES.md §16.10)")
        return j_write_field(w, rec.fields[0], depth)
    let ok, wrote = j_write_fields(w, rec, depth, false)
    if not ok:
        return false
    if not wrote:
        j_raw(w, "{{}}")
        return true
    j_line(w, depth)
    j_raw(w, "}}")
    return true

def j_write_fields(w:JsonWriter, rec:Record, depth:int, open:bool) -> bool, bool:
    var wrote = open
    for(rec.fields) sl:
        if json_guard_holds(rec, sl) and (sl.fd.present == "" or sl.present):
            j_raw(w, if wrote: "," else: "{{")
            wrote = true
            j_line(w, depth + 1)
            j_write_string(w, sl.fd.json)
            j_raw(w, ": ")
            if not j_write_field(w, sl, depth + 1):
                return false, wrote
    return true, wrote

def j_write_pointer(w:JsonWriter, node:Record?, depth:int) -> bool:
    if not node:
        j_raw(w, "null")
        return true
    let x = j_node_entry(w, node)
    if x.count <= 1:
        return j_write_value(w, node, depth)
    if depth > json_max_depth:
        return j_fail(w, "table " + node.d.name + ": nested past the depth cap (docs/SPEC-TABLES.md §16.7)")
    if x.label > 0:
        j_raw(w, "{{")
        j_line(w, depth + 1)
        j_raw(w, "\"&node\": " + number_to_string(x.label, 10, 1))
        j_line(w, depth)
        j_raw(w, "}}")
        return true
    w.next++
    x.label = w.next
    j_raw(w, "{{")
    j_line(w, depth + 1)
    j_raw(w, "\"&node\": " + number_to_string(x.label, 10, 1))
    let before = length(w.parts)
    let ok, _ = j_write_fields(w, node, depth, true)
    if not ok:
        return false
    if length(w.parts) == before:
        return j_fail(w, "table " + node.d.name + ": a shared node with no field to write has no definition the text form can spell (docs/SPEC-TABLES.md §16.7)")
    j_line(w, depth)
    j_raw(w, "}}")
    return true

def j_array_count(sl:Slot) -> int:
    let f = sl.fd
    if f.array == "counted" or f.array == "list":
        return sl.count
    return f.bound

def j_write_field(w:JsonWriter, sl:Slot, depth:int) -> bool:
    let f = sl.fd
    let s = f.s
    if s == "sblob" or s == "bblob":
        let blob = sl.cell.blob
        if not blob:
            j_raw(w, "null")
            return true
        let n = sl.cell.blob_node
        if n:
            for(w.blobs) b:
                if b.node == n and b.count > 1:
                    return j_fail(w, "field " + f.name + " names a byte buffer another slot names: a shared blob has no spelling this form can carry (docs/SPEC-TABLES.md §16.7)")
        if s == "sblob":
            j_write_string(w, blob)
        else:
            j_write_base64(w, blob)
        return true
    if s == "text" or (s == "str" and not f.wide):
        j_write_string(w, sl.cell.s)
        return true
    if s == "str":
        j_write_wstring(w, sl.cell.units)
        return true
    if s == "bytes":
        j_write_base64(w, sl.cell.s)
        return true
    if s == "map":
        return j_write_map(w, sl, depth)
    if s == "arr":
        if f.array == "keyed":
            return j_write_keyed(w, sl, depth)
        let count = j_array_count(sl)
        if count == 0:
            j_raw(w, "[]")
            return true
        let text_list = j_is_text_list(f)
        let e = elem_of(f)
        j_raw(w, "[")
        var i = 0
        while i < count:
            if i > 0:
                j_raw(w, ",")
            j_line(w, depth + 1)
            if text_list:
                let t = sl.elems[i].tab
                if t:
                    let v = t.fields[0]
                    if v.fd.present != "" and not v.present:
                        j_raw(w, "null")
                    else:
                        j_write_string(w, v.cell.s)
            elif not j_write_scalar(w, sl.elems[i], e, depth + 1):
                return false
            i++
        j_line(w, depth)
        j_raw(w, "]")
        return true
    return j_write_scalar(w, sl.cell, f, depth)

def j_write_map(w:JsonWriter, sl:Slot, depth:int) -> bool:
    if length(sl.entries) == 0:
        j_raw(w, "{{}}")
        return true
    let kf = table_of(sl.fd.t).fields[0]
    let entries = j_sorted_entries(kf, sl.entries)
    j_raw(w, "{{")
    for(entries) v, i:
        if i > 0:
            j_raw(w, ",")
        j_line(w, depth + 1)
        let entry = v.tab
        if entry:
            let k = entry.fields[0]
            if key_is_string(kf):
                j_write_string(w, k.cell.s)
            elif kf.s == "int" and kf.signed and not j_is_fixed(kf):
                j_write_string(w, string(k.cell.i))
            else:
                j_write_string(w, number_to_string(k.cell.i, 10, 1))
            j_raw(w, ": ")
            let value = entry.fields[1]
            if value.fd.present != "" and not value.present:
                j_raw(w, "null")
            elif not j_write_field(w, value, depth + 1):
                return false
    j_line(w, depth)
    j_raw(w, "}}")
    return true

def j_write_keyed(w:JsonWriter, sl:Slot, depth:int) -> bool:
    let f = sl.fd
    let n = f.bound
    if n <= 0:
        j_raw(w, "{{}}")
        return true
    let keys = keyed_variants(f)
    let e = elem_of(f)
    j_raw(w, "{{")
    var slot = 0
    while slot < n:
        if slot > 0:
            j_raw(w, ",")
        j_line(w, depth + 1)
        if slot >= length(keys):
            return j_fail(w, "enum-keyed array " + f.name + ": a slot with no name has no text spelling (docs/SPEC-TABLES.md §3.2)")
        j_write_string(w, keys[slot].name)
        j_raw(w, ": ")
        if not j_write_scalar(w, sl.elems[slot], e, depth + 1):
            return false
        slot++
    j_line(w, depth)
    j_raw(w, "}}")
    return true

def j_write_scalar(w:JsonWriter, cell:Value, f:FieldDescriptor, depth:int) -> bool:
    let s = f.s
    if s == "union":
        if cell.i == 0:
            j_raw(w, "{{}}")
            return true
        let un = lookup_union(f.t)
        if not un:
            return j_fail(w, "union " + f.t + " is not registered")
        if cell.i < 0 or cell.i > length(un.arms):
            return j_fail(w, "union " + un.name + ": tag " + string(cell.i) + " names no arm (docs/SPEC-TABLES.md §5)")
        let a = un.arms[cell.i - 1]
        j_raw(w, "{{")
        j_line(w, depth + 1)
        j_write_string(w, a.name)
        j_raw(w, ": ")
        if a.s == "void":
            j_raw(w, "null")
        elif a.s == "tab":
            let held = cell.tab
            let payload = if held: held else: new_record(table_of(a.t))
            if not j_write_value(w, payload, depth + 1):
                return false
        else:
            let held = cell.arm
            let fv = if held: held else: new_slot(a)
            if not j_write_field(w, fv, depth + 1):
                return false
        j_line(w, depth)
        j_raw(w, "}}")
        return true
    if s == "ptr":
        return j_write_pointer(w, cell.node, depth)
    if s == "tab":
        let held = cell.tab
        return j_write_value(w, if held: held else: new_record(table_of(f.t)), depth)
    if s == "enum":
        let v = cell.i
        if v >= 1 and v <= length(f.vs) and is_deprecated_variant(f.t, f.vs[v - 1].name):
            return j_fail(w, "field " + f.name + ": enum " + f.t + " value " + string(v) + " names a deprecated variant (docs/SPEC-TABLES.md §2.15)")
        if v == 0:
            j_write_string(w, "None")
            return true
        if v < 1 or v > length(f.vs):
            return j_fail(w, "enum " + f.t + ": value " + string(v) + " names no variant (docs/SPEC-TABLES.md §5)")
        j_write_string(w, f.vs[v - 1].name)
        return true
    let fl = json_flags_of(f)
    if fl:
        let bits = cell.i
        if bits == 0:
            j_raw(w, "[]")
            return true
        j_raw(w, "[")
        var first = true
        var bit = 0
        while bit < 64:
            if (shr(bits, bit) & 1) != 0:
                if bit >= length(fl.vs):
                    return j_fail(w, "flags bit " + string(bit) + " names no variant (docs/SPEC-TABLES.md §16.2)")
                if not first:
                    j_raw(w, ",")
                first = false
                j_line(w, depth + 1)
                j_write_string(w, fl.vs[bit])
            bit++
        j_line(w, depth)
        j_raw(w, "]")
        return true
    let kind = j_kind(f)
    if kind_wide(kind):
        j_raw(w, j_format_wide(cell.w, kind, f.fbits))
        return true
    if kind == 1:
        j_raw(w, if cell.b: "true" else: "false")
        return true
    if kind == 10 or kind == 11:
        let v = cell.f
        if v != v or j_is_inf(v):
            return j_fail(w, "a non-finite float has no JSON spelling (docs/SPEC-TABLES.md §16.2)")
        j_raw(w, j_float_text(v, kind == 10))
        return true
    if kind == 36:
        j_write_string(w, j_rfc3339_format(cell.i))
        return true
    if kind >= 2 and kind <= 5:
        j_raw(w, string(cell.i))
        return true
    j_raw(w, number_to_string(cell.i, 10, 1))
    return true

// j_utf8_sequence is the length of one well-formed UTF-8 sequence at s[i],
// or the negated length of the maximal subpart (Go's utf8Sequence).
def j_utf8_sequence(s:string, i:int) -> int:
    let lead = s[i]
    var want = 0
    var lo = 0x80
    var hi = 0xBF
    if lead < 0x80:
        return 1
    if lead >= 0xC2 and lead <= 0xDF:
        want = 2
    elif lead >= 0xE0 and lead <= 0xEF:
        want = 3
        if lead == 0xE0:
            lo = 0xA0
        if lead == 0xED:
            hi = 0x9F
    elif lead >= 0xF0 and lead <= 0xF4:
        want = 4
        if lead == 0xF0:
            lo = 0x90
        if lead == 0xF4:
            hi = 0x8F
    else:
        return -1
    var k = 1
    while k < want:
        if i + k >= length(s) or s[i + k] < lo or s[i + k] > hi:
            return -k
        lo = 0x80
        hi = 0xBF
        k++
    return want

let j_hex = "0123456789abcdef"

def j_escape(w:JsonWriter, c:int) -> bool:
    if c == '\"':
        j_raw(w, "\\\"")
    elif c == '\\':
        j_raw(w, "\\\\")
    elif c == 8:
        j_raw(w, "\\b")
    elif c == 12:
        j_raw(w, "\\f")
    elif c == 10:
        j_raw(w, "\\n")
    elif c == 13:
        j_raw(w, "\\r")
    elif c == 9:
        j_raw(w, "\\t")
    elif c < 0x20:
        j_raw(w, "\\u00" + j_bytes[j_hex[c >> 4]] + j_bytes[j_hex[c & 15]])
    else:
        return false
    return true

// j_write_string emits one JSON string; a byte outside a well-formed UTF-8
// sequence is written as U+FFFD, one per maximal subpart (§16.3).
def j_write_string(w:JsonWriter, s:string):
    j_raw(w, "\"")
    let n = length(s)
    var i = 0
    while i < n:
        let start = i
        while i < n:
            let c = s[i]
            if c < 0x20 or c >= 0x80 or c == '\"' or c == '\\':
                break
            i++
        if i > start:
            j_raw(w, substring(s, start, i - start))
        if i >= n:
            break
        let c = s[i]
        if j_escape(w, c):
            i++
        else:
            let width = j_utf8_sequence(s, i)
            if width < 0:
                j_raw(w, j_utf8(0xFFFD))
                i += -width
            else:
                j_raw(w, substring(s, i, width))
                i += width
    j_raw(w, "\"")

// j_write_wstring is a wide field's text: code units back to UTF-8, an
// unpaired surrogate as U+FFFD (§16.2, §16.3).
def j_write_wstring(w:JsonWriter, units:[int]):
    j_raw(w, "\"")
    var i = 0
    while i < length(units):
        var code = units[i]
        if code >= 0xD800 and code <= 0xDBFF and i + 1 < length(units):
            let low = units[i + 1]
            if low >= 0xDC00 and low <= 0xDFFF:
                code = 0x10000 + ((code - 0xD800) << 10) + (low - 0xDC00)
                i++
        if code >= 0xD800 and code <= 0xDFFF:
            code = 0xFFFD
        if not j_escape(w, code):
            j_raw(w, j_utf8(code))
        i++
    j_raw(w, "\"")

def j_write_base64(w:JsonWriter, data:string):
    let n = length(data)
    var out = zeros((n + 2) / 3 * 4 + 2)
    out = write_int8_le(out, 0, '\"')
    var at = 1
    var i = 0
    while n - i >= 3:
        let triple = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2]
        out = write_int8_le(out, at, j_b64_alphabet[(triple >> 18) & 63])
        out = write_int8_le(out, at + 1, j_b64_alphabet[(triple >> 12) & 63])
        out = write_int8_le(out, at + 2, j_b64_alphabet[(triple >> 6) & 63])
        out = write_int8_le(out, at + 3, j_b64_alphabet[triple & 63])
        at += 4
        i += 3
    if i < n:
        let left = n - i
        var triple = data[i] << 16
        if left == 2:
            triple = triple | (data[i + 1] << 8)
        out = write_int8_le(out, at, j_b64_alphabet[(triple >> 18) & 63])
        out = write_int8_le(out, at + 1, j_b64_alphabet[(triple >> 12) & 63])
        out = write_int8_le(out, at + 2, if left == 2: j_b64_alphabet[(triple >> 6) & 63] else: '=')
        out = write_int8_le(out, at + 3, '=')
        at += 4
    out = write_int8_le(out, at, '\"')
    j_raw(w, out)

// json_write renders one Record as one JSON text (ToJson, docs/SPEC-TABLES.md
// §16.1), closed by one newline as every port's ToJson closes it (the
// reference engine's Write, then "\n"): the text and "", or "" and why the value has no text spelling (a
// non-finite float, a value no variant names, a cycle, a shared blob).
def json_write(rec:Record) -> string, string:
    let w = JsonWriter {}
    let root = j_node_entry(w, rec)
    root.open = true
    if not j_count_record(w, rec):
        return "", w.err
    if not j_write_value(w, rec, 0):
        return "", w.err
    push(w.parts, "\n")
    return concat_string(w.parts, ""), ""
