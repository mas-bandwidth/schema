// load.lobster — the Lobster table runtime's VARIABLE FORM (form 1) load and
// its read report (docs/SPEC-TABLES.md §3, §3.1, §4; docs/PORT-LOBSTER.md,
// THE LOAD). A port of the Go reference engine's read half
// (internal/tablewire decode.go, decodenodes.go, nodetable.go, text.go and
// leb.go) onto the descriptors schema_tables.lobster declares: every rule
// lives once, here, and every fact (ids, kinds, bounds, defaults, the order)
// is read from a descriptor as data (docs/SPEC-TABLES.md §8.1).
//
// THE STORAGE (docs/PORT-LOBSTER.md, THE LOAD): a load fills a Record — the
// table as data, one Slot per field in descriptor order, each holding a
// Value — never a generated class, because Lobster has no reflection. A
// form card's per-table accessor moves a Record into the generated class.
//
// THE DISPATCH: a field, an array element and a union arm are read by the
// Shape registered under the field's shape key (register_shape). Every
// shape the reference reads is registered below; a later runtime file adds
// or replaces a shape with one `let _ = schema_rt.register_shape(...)` of
// its own and never edits this file. The last registration of a key wins.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .schema_tables

namespace schema_rt

// Read diagnostics are local observations. refused and stopped describe a
// whole-load refusal; counters never select a recovery path.
class LoadReport:
    unknown:int = 0
    kind_mismatch:int = 0
    widened:int = 0
    clamped:int = 0
    duplicate:int = 0
    malformed:bool = false
    blob_shared:int = 0
    // missing_required is a record's `| required` refusal (docs/SPEC-TABLES.md
    // §2.3): one per body that ends without a required field, named in reason.
    missing_required:int = 0
    // non_finite_float names the first float whose raw bits were a NaN or an
    // infinity, <Owner>.<field> (docs/SPEC-TABLES.md §3.3): every read, the
    // tolerant one too, refuses the wire and lands nothing.
    non_finite_float:string = ""
    // RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6): the fields whose bytes a
    // retaining load kept, and every unknown the load could not keep.
    retained:int = 0
    retain_lost:int = 0
    refused:bool = false
    reason:string = ""
    stopped:bool = false
    // damaged is the verdict's damage (§4, §6.5): a wire whose framing does
    // not open (no bytes, an id table that does not hold, a root body that
    // ends before the id table), or a value-only root whose walk read past
    // damage to its terminator. The strict read names it wire_damaged; a
    // body that stopped the walk is malformed.
    damaged:bool = false

// A failed read stops its walk. Diagnostics are local; the verdict is whole.
def read_refuse(rep:LoadReport, reason:string) -> bool:
    rep.refused = true
    rep.stopped = true
    if rep.reason == "":
        rep.reason = reason
    return false

// Diagnostics belong to this invocation and cannot veto a later valid read.
def read_reset(rep:LoadReport):
    rep.unknown = 0
    rep.kind_mismatch = 0
    rep.widened = 0
    rep.clamped = 0
    rep.duplicate = 0
    rep.malformed = false
    rep.blob_shared = 0
    rep.missing_required = 0
    rep.non_finite_float = ""
    rep.retained = 0
    rep.retain_lost = 0
    rep.refused = false
    rep.reason = ""
    rep.stopped = false
    rep.damaged = false

class Record
class Slot
class Node

// A Value is one storage cell (Go's tabletext.Cell): only the member the
// field's shape names is read. ABSENT, PRESENT-EMPTY AND PRESENT
// (docs/PORT-LOBSTER.md, THE LOAD): a pointer or blob that is null holds
// nil in node or blob; an empty blob is the non-nil "" (null and empty are
// two values, §2.5); an optional is absent while its Slot's present is
// false, whatever the cell holds (§2.3); a union is None while i is 0.
class Value:
    b:bool = false
    // i: every integer of 64 bits or fewer as its bit pattern (Go's I and U
    // at once), an enum value, a union tag (docs/PORT-LOBSTER.md, THE NUMBER
    // MODEL)
    i:int = 0
    // w: the RAW value of a wide kind (§3, kinds 18-29): 128-bit integers
    // and every fixed-point kind, units x 2^F
    w:Int128 = Int128 { 0, 0 }
    f:float = 0.0
    // s: a string(N) or bytes(N) payload at its used extent
    s:string = ""
    // units: a wstring(N)'s UTF-16 code units at their used extent
    units:[int] = []
    // tab: a by-value table, a packet type, a body arm, an array element
    // record, a map entry
    tab:Record? = nil
    // arm: a union's selected arm that is not a body (a field line, §2.6)
    arm:Slot? = nil
    // node: a *T pointer's referent, nil for null (§3.1)
    node:Record? = nil
    // blob: a *bytes / *string referent, nil for null (§2.5)
    blob:string? = nil
    // blob_node: the ONE node a tolerant read decoded the blob from. Two
    // slots naming one blob name one node (§2.5), so the save meets the
    // second naming and refuses the graph by name, never writing a copy
    // per slot. nil for a blob the caller set.
    blob_node:Node? = nil

// A Slot is one field's storage (Go's tabletext.Field): the value, the
// counted extent and the presence companion beside it.
class Slot:
    fd:FieldDescriptor
    present:bool = false
    count:int = 0
    cell:Value
    elems:[Value]
    entries:[Value]

// A Record is one table or packet type value, its slots in descriptor order.
// RetainedField is ONE unknown field a retaining load kept, with every
// reference replaced by the id it names (docs/SPEC-TABLES.md §6.6, RETENTION
// FORM). It belongs to the body occurrence that carried it and dies with it.
class RetainedField:
    id:int
    kind:int
    payload:string
    cost:int
    placed:bool = false

class Record:
    d:TableDescriptor
    fields:[Slot]
    retained:[RetainedField] = []
    // held marks a node record the root reaches, for the one pass at the end
    // of a variable load that unlinks the records it does not (RELEASE)
    held:bool = false
    // walk is the cycle check's mark: 0 unvisited, 1 open, 2 closed
    walk:int = 0

// Fixed roots share their generated native codec with the generic Record
// surface. The adapter projects storage only; it owns no wire decoder.
class FixedRecordCodec:
    name:string
    def read_fixed(data:string, report:LoadReport) -> Record?:
        read_refuse(report, "layout_unsupported")
        return nil
    def write_fixed(rec:Record) -> string, string:
        return "", "layout_unsupported"

let fixed_record_codecs:[FixedRecordCodec] = []

def register_fixed_record_codec(codec:FixedRecordCodec):
    push(fixed_record_codecs, codec)

def fixed_record_codec(name:string) -> FixedRecordCodec:
    for(fixed_record_codecs) codec:
        if codec.name == name:
            return codec
    return FixedRecordCodec { name: name }

// A Node is one entry of THE FLAT NODE TABLE (docs/SPEC-TABLES.md §3.1):
// index i + 2 on the wire names nodes[i]. kind 0 is a record this reader
// cannot name — it keeps its index and every pointer naming it reads null —
// 1 a table record, 2 a *string blob, 3 a *bytes blob.
class Node:
    kind:int
    rec:Record?
    blob:string?

// THE RESERVED IDS (docs/SPEC-TABLES.md §3.1, §3.3, §2.5, §2.8), as the bit
// patterns the id table carries.
let node_table_id = 0xffffffffffffffff
let build_version_id = 0xfffffffffffffffe
let message_vocabulary_id = 0xfffffffffffffffd
let string_blob_id = 0x704be0d8faaffc58 // FNV-1a 64 of "string"
let bytes_blob_id = 0x2f2ec0474f1c4fe4 // FNV-1a 64 of "bytes"
let map_key_id = 0x3dc94a19365b10ec // FNV-1a 64 of "key"

// LoadState is one load's shared state: the id table, the report, and the
// numbering (Go's decodeState). variable is false for a value-only root,
// whose reader resolves nothing and names no node table (§3.1).
class LoadState:
    data:string
    ids:[int]
    report:LoadReport
    variable:bool = false
    good:bool = false
    root:Record? = nil
    nodes:[Node] = []
    named:[int] = []
    count_refused:bool = false

// A Reader is a cursor over data[lo:hi] (Go's wireReader over a sub-slice).
class Reader:
    b:string
    lo:int
    off:int
    hi:int
    st:LoadState

// RETAIN-UNKNOWN's per-kind hook (docs/SPEC-TABLES.md §6.6). A retaining load
// registers one; `capture` sees one unknown field id with its payload already
// delimited by the plain skip, and `lost` counts one excluded class the plain
// walk met where the reader could name the field it
class RetainCapture:
    def capture(rec:Record, id:int, kind:int, payload:string, ids:[int]) -> void:
        return
    def lost() -> void:
        return
    // built sees every map entry body a map occurrence decoded, landed or
    // not, and forget_built discards the retained fields under the ones the
    // slot's earlier occurrences built (Go's builtMapEntry and forgetMap): a
    // map a damaged key emptied still held bodies a later occurrence replaces
    def built(sl:Slot, rec:Record) -> void:
        return
    def forget_built(sl:Slot) -> void:
        return

let retain_captures:[RetainCapture] = []

def register_retain_capture(h:RetainCapture):
    push(retain_captures, h)

def active_retain_capture() -> RetainCapture?:
    if length(retain_captures) == 0:
        return nil
    return retain_captures[length(retain_captures) - 1]

def reader(st:LoadState, lo:int, hi:int) -> Reader:
    return Reader { b: st.data, lo: lo, off: lo, hi: hi, st: st }

// sub is the next n bytes as their own reader; the caller has checked has(n).
def sub(r:Reader, n:int) -> Reader:
    return Reader { b: r.b, lo: r.off, off: r.off, hi: r.off + n, st: r.st }

// sub_to spans the cursor to end, empty when a wide count already passed end
// (Go's subTo, §3).
def sub_to(r:Reader, end:int) -> Reader:
    var e = end
    if e < r.off:
        e = r.off
    return sub(r, e - r.off)

// has: n more bytes are inside this reader. A LEB-read length of 2^63 or
// more is a negative int here, and is never inside.
def has(r:Reader, n:int) -> bool:
    return n >= 0 and n <= r.hi - r.off

def u8(r:Reader) -> int:
    let v = r.b[r.off]
    r.off++
    return v

// read_leb is one canonical LEB128 (docs/SPEC-TABLES.md §3; Go's readLeb): a
// truncated value, a tenth byte past the 64th bit or a redundant
// continuation is not one.
def read_leb(b:string, off:int, hi:int) -> int, int, bool:
    var v = 0
    var shift = 0
    var o = off
    var i = 0
    while i < 10:
        if o >= hi:
            return 0, o, false
        let x = b[o]
        o++
        if i == 9 and x > 1:
            return 0, o, false
        v = v | shl(x & 0x7f, shift)
        if (x & 0x80) == 0:
            if i > 0 and x == 0:
                return 0, o, false
            return v, o, true
        shift += 7
        i++
    return 0, o, false

def leb(r:Reader) -> int, bool:
    let v, next, ok = read_leb(r.b, r.off, r.hi)
    if not ok:
        return 0, false
    r.off = next
    return v, true

// rid resolves one reference against the id table (§3): 0 and a reference
// above the entry count name no id.
def rid(r:Reader, ref:int) -> int, bool:
    let ids = r.st.ids
    if ref == 0 or ult(length(ids), ref):
        return 0, false
    return ids[ref - 1], true

// THE KIND TABLE (docs/SPEC-TABLES.md §3; Go's ir.TableKindWidth,
// TableKindSigned, TableKindWide, TableKindWidens).
def kind_width(kind:int) -> int:
    if kind == 1 or kind == 2 or kind == 6 or kind == 20 or kind == 25:
        return 1
    if kind == 3 or kind == 7 or kind == 21 or kind == 26:
        return 2
    if kind == 4 or kind == 8 or kind == 10 or kind == 17 or kind == 22 or kind == 27:
        return 4
    if kind == 5 or kind == 9 or kind == 11 or kind == 23 or kind == 28 or kind == 36:
        return 8
    if kind == 18 or kind == 19 or kind == 24 or kind == 29:
        return 16
    return 0

def kind_signed(kind:int) -> bool:
    return kind == 2 or kind == 3 or kind == 4 or kind == 5 or kind == 18 or (kind >= 20 and kind <= 24)

def kind_wide(kind:int) -> bool:
    return kind >= 18 and kind <= 29

// widens: a payload under kind decodes exactly into a field declared at
// declared (§4): the signed ladder 2 3 4 5 18, the unsigned 6 7 8 9 19, and
// f32 into f64.
def widens(kind:int, declared:int) -> bool:
    if declared == 3 or declared == 4 or declared == 5:
        return kind >= 2 and kind < declared
    if declared == 18:
        return kind >= 2 and kind <= 5
    if declared == 7 or declared == 8 or declared == 9:
        return kind >= 6 and kind < declared
    if declared == 19:
        return kind >= 6 and kind <= 9
    if declared == 11:
        return kind == 10
    return false

// skip steps past a field this reader cannot name (§3): the kind byte is
// all it needs. Kind 34 (float16) is reserved and, like every kind the set
// does not hold, is not skippable.
def skip(r:Reader, kind:int) -> bool:
    if (kind >= 1 and kind <= 11) or (kind >= 18 and kind <= 29) or kind == 36:
        let w = kind_width(kind)
        if not has(r, w):
            return false
        r.off += w
        return true
    if kind == 17 or kind == 30:
        let v, ok = leb(r)
        return ok or v != 0
    if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33:
        let n, ok = leb(r)
        if not ok or not has(r, n):
            return false
        r.off += n
        return true
    if kind == 15:
        let arm, ok = leb(r)
        if not ok:
            return false
        if arm == 0:
            return true
        if not has(r, 1):
            return false
        r.off++
        let n, ok2 = leb(r)
        if not ok2 or not has(r, n):
            return false
        r.off += n
        return true
    return false

// TEXT (docs/SPEC-TABLES.md §3; Go's text.go): well-formed UTF-8 with no
// zero byte; a clamp cuts at a code point boundary.
def text_valid(b:string, at:int, n:int) -> bool:
    var i = at
    let e = at + n
    while i < e:
        let b0 = b[i]
        if b0 == 0:
            return false
        if b0 < 0x80:
            i++
            continue
        var need = 0
        var cp = 0
        if b0 >= 0xC2 and b0 <= 0xDF:
            need = 1
            cp = b0 & 0x1F
        elif b0 >= 0xE0 and b0 <= 0xEF:
            need = 2
            cp = b0 & 0x0F
        elif b0 >= 0xF0 and b0 <= 0xF4:
            need = 3
            cp = b0 & 0x07
        else:
            return false
        if i + need >= e:
            return false
        var k = 1
        while k <= need:
            let x = b[i + k]
            if x < 0x80 or x > 0xBF:
                return false
            cp = cp * 64 + (x & 0x3F)
            k++
        let least = if need == 1: 0x80 elif need == 2: 0x800 else: 0x10000
        if cp < least or cp > 0x10FFFF or (cp >= 0xD800 and cp <= 0xDFFF):
            return false
        i += 1 + need
    return true

def text_boundary(b:string, at:int, n:int, bound:int) -> int:
    if n <= bound:
        return n
    var cut = bound
    while cut > 0 and (b[at + cut] & 0xC0) == 0x80:
        cut--
    return cut

def wide_units(b:string, at:int, n:int) -> [int]:
    let out = []
    var i = 0
    while i + 1 < n:
        push(out, b[at + i] | (b[at + i + 1] << 8))
        i += 2
    return out

def wide_valid(units:[int]) -> bool:
    var i = 0
    while i < length(units):
        let u = units[i]
        if u == 0:
            return false
        if u >= 0xD800 and u <= 0xDBFF:
            if i + 1 >= length(units):
                return false
            let low = units[i + 1]
            if low < 0xDC00 or low > 0xDFFF:
                return false
            i++
        elif u >= 0xDC00 and u <= 0xDFFF:
            return false
        i++
    return true

def wide_boundary(units:[int], bound:int) -> int:
    if length(units) <= bound:
        return length(units)
    var cut = bound
    if cut > 0:
        let last = units[cut - 1]
        if last >= 0xD800 and last <= 0xDBFF:
            cut--
    return cut

// THE INT128 ORDER (docs/PORT-LOBSTER.md, THE NUMBER MODEL): the high lane
// first, signed or unsigned, then the low lane unsigned.
def i128_lt(a:Int128, b:Int128, signed:bool) -> bool:
    if a.hi != b.hi:
        return if signed: a.hi < b.hi else: ult(a.hi, b.hi)
    return ult(a.lo, b.lo)

// i128_shl moves a fixed field's whole-unit bound onto the raw scale, x 2^F
// (Go's ir.TableRawRange).
def i128_shl(v:Int128, n:int) -> Int128:
    if n <= 0:
        return v
    if n >= 128:
        return Int128 { 0, 0 }
    if n >= 64:
        return Int128 { 0, shl(v.lo, n - 64) }
    return Int128 { shl(v.lo, n), shl(v.hi, n) | shr(v.lo, 64 - n) }

// read_raw reads width bytes as a two's-complement (or zero-extended) Int128.
def read_raw(r:Reader, width:int, signed:bool) -> Int128:
    if width == 16:
        let lo = read_int64_le(r.b, r.off)
        let hi = read_int64_le(r.b, r.off + 8)
        r.off += 16
        return Int128 { lo, hi }
    var v = 0
    if width == 1:
        v = r.b[r.off]
    elif width == 2:
        v = read_uint16_le(r.b, r.off)
    elif width == 4:
        v = read_uint32_le(r.b, r.off)
    else:
        v = read_int64_le(r.b, r.off)
    r.off += width
    if signed and width < 8 and (v & shl(1, width * 8 - 1)) != 0:
        v -= shl(1, width * 8)
    let hi = if signed and v < 0: -1 else: 0
    return Int128 { v, hi }

// A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3, the same rule as
// packets): the test is on the RAW BITS,
// before they become a value. -0 and the subnormals are finite and ride bit
// for bit; every finite float32 widens exactly through the ordinary
// conversion, so no bit surgery is left.
def finite_f32_bits(bits:int) -> bool:
    return (bits & 0x7F800000) != 0x7F800000

def finite_f64_bits(bits:int) -> bool:
    return (bits & 0x7FF0000000000000) != 0x7FF0000000000000

// finite_f32 and finite_f64 are the writer's test on a stored value: a
// float32 field's value is tested at the width it is written, so a double
// past float32's range (an infinity once narrowed) is refused too.
def finite_f32(x:float) -> bool:
    return finite_f32_bits(f32bits(x))

def finite_f64(x:float) -> bool:
    return finite_f64_bits(f64bits(x))

// parse_float64 is a number token as the nearest double, the answer strtod
// and Go's ParseFloat give on every host: a magnitude past the largest double
// is an infinity of the token's sign, and one below the smallest subnormal is
// a zero of it. Lobster's string_to_float is strtod on macOS and
// std::from_chars elsewhere, and from_chars leaves 0.0 for a token out of
// range at either end, so a zero is resolved by float_out_of_range.
def parse_float64(token:string) -> float:
    let d, _ = string_to_float(token)
    if d != 0.0:
        return d
    return float_out_of_range(token, d)

// float_out_of_range is the double a token that parsed to zero (d) stands
// for: d itself when the token's digits are all zero, and otherwise the end
// of the range it fell off, read off the decimal exponent of its first
// nonzero digit: at or above 10^0 it overflowed to an infinity, below it it
// underflowed to a zero, each of the token's sign.
def float_out_of_range(token:string, d:float) -> float:
    var i = 0
    var neg = false
    if i < length(token) and (token[i] == '-' or token[i] == '+'):
        neg = token[i] == '-'
        i++
    var point = false
    var nonzero = false
    var e10 = 0
    while i < length(token) and token[i] != 'e' and token[i] != 'E':
        let c = token[i]
        if c == '.':
            point = true
        elif nonzero:
            if not point:
                e10++
        elif c != '0':
            nonzero = true
            if point:
                e10--
        elif point:
            e10--
        i++
    if not nonzero:
        return d
    var exp = 0
    var eneg = false
    if i < length(token):
        i++
        if i < length(token) and (token[i] == '-' or token[i] == '+'):
            eneg = token[i] == '-'
            i++
        while i < length(token):
            exp = min(exp * 10 + (token[i] - '0'), 1000000)
            i++
    let total = e10 + (if eneg: -exp else: exp)
    if total >= 0:
        return if neg: -1.0 / 0.0 else: 1.0 / 0.0
    return if neg: f64frombits(0x8000000000000000) else: 0.0

// float_owner names a float descriptor as <Owner>.<field> for the
// non_finite_float refusal (Go's tablewire.FloatOwner): the table, type or
// union that declares the field, an array's element named by its field. It
// runs only on the refusal path.
def float_owner(fd:FieldDescriptor) -> string:
    for(registered_tables()) d:
        for(d.fields) f:
            if f == fd or f.e == fd:
                return d.name + "." + f.name
    for(registered_unions()) u:
        for(u.arms) a:
            if a == fd or a.e == fd:
                return u.name + "." + a.name
    return fd.name

// retained_payload_finite is whether a retained unknown scalar of a float
// kind holds a finite value: kind 10 a float32's bits, kind 11 a float64's.
def retained_payload_finite(kind:int, payload:string) -> bool:
    if kind == 10 and length(payload) == 4:
        return (read_int32_le(payload, 0) & 0x7F800000) != 0x7F800000
    if kind == 11 and length(payload) == 8:
        let hi = (read_int64_le(payload, 0) >> 52) & 0x7FF
        return hi != 0x7FF
    return true

// hex64 is a 64-bit pattern as sixteen lowercase hex digits
def hex64(v:int) -> string:
    let hi = (v >> 32) & 0xFFFFFFFF
    let lo = v & 0xFFFFFFFF
    return lowercase(number_to_string(hi, 16, 8) + number_to_string(lo, 16, 8))

// refuse_non_finite records the first non-finite float a read met; the read
// walks on, so its framing verdict is a finite wire's, and refuses at its end.
def refuse_non_finite(rep:LoadReport, fd:FieldDescriptor):
    if rep.non_finite_float == "":
        rep.non_finite_float = float_owner(fd)

// scalar_at decodes one fixed-width value that arrived under kind: the
// declared kind or one it widens (docs/SPEC-TABLES.md §4; Go's scalarAt).
// fd carries the facts (range, bits, name) — a field's, an arm's, or an
// array's element descriptor. The payload is read at the WIRE kind's width
// and signedness and then takes the declaration's clamps.
def scalar_at(r:Reader, cell:Value, fd:FieldDescriptor, declared:int, kind:int) -> bool:
    let rep = r.st.report
    let width = kind_width(kind)
    if not has(r, width):
        return read_refuse(rep, "wire_damaged")
    if kind == 1:
        let x = u8(r)
        if x > 1:
            return read_refuse(rep, "wire_damaged")
        cell.b = x != 0
        return true
    if kind == 10:
        let bits = read_uint32_le(r.b, r.off)
        let v = read_float32_le(r.b, r.off)
        r.off += 4
        if not finite_f32_bits(bits):
            refuse_non_finite(rep, fd)
            return read_refuse(rep, "non_finite_float")
        if declared != 11 and fd.franged and (v < fd.flo or v > fd.fhi):
            return read_refuse(rep, "out_of_range")
        cell.f = if declared == 11: v else: fround(v)
        return true
    if kind == 11:
        let bits = read_int64_le(r.b, r.off)
        let v = read_float64_le(r.b, r.off)
        r.off += 8
        if not finite_f64_bits(bits):
            refuse_non_finite(rep, fd)
            return read_refuse(rep, "non_finite_float")
        cell.f = v
        return true
    if kind_wide(kind):
        let signed = kind_signed(kind)
        let raw = read_raw(r, width, signed)
        if fd.ranged:
            let lo = i128_shl(fd.lo, fd.fbits)
            let hi = i128_shl(fd.hi, fd.fbits)
            if i128_lt(raw, lo, signed) or i128_lt(hi, raw, signed):
                return read_refuse(rep, "out_of_range")
        cell.w = raw
        return true
    let signed = kind_signed(kind)
    let raw = read_raw(r, width, false).lo
    var value = raw
    if signed and width < 8 and (raw & shl(1, width * 8 - 1)) != 0:
        value = raw - shl(1, width * 8)
    if kind_wide(declared):
        let dsigned = kind_signed(declared)
        let w = Int128 { value, if signed and value < 0: -1 else: 0 }
        if fd.ranged:
            let lo = i128_shl(fd.lo, fd.fbits)
            let hi = i128_shl(fd.hi, fd.fbits)
            if i128_lt(w, lo, dsigned) or i128_lt(hi, w, dsigned):
                return read_refuse(rep, "out_of_range")
        cell.w = w
        return true
    if fd.ranged:
        let lo = fd.lo.lo
        let hi = fd.hi.lo
        if signed:
            if value < lo or value > hi:
                return read_refuse(rep, "out_of_range")
        elif ult(raw, lo) or ult(hi, raw):
            return read_refuse(rep, "out_of_range")
    if fd.bits > 0 and fd.bits < width * 8:
        let most = shl(1, fd.bits) - 1
        if ult(most, value):
            return read_refuse(rep, "out_of_range")
    cell.i = value
    return true

// THE CONSTRUCTION (Go's tabletext Model.New, reset, elementZero,
// fieldDefault): a fresh record holds exactly what the generated class's
// constructor holds, so a field the wire never mentions keeps the default an
// absent field takes (§4).
def table_of(name:string) -> TableDescriptor:
    let d = lookup_table(name)
    assert d
    return d

def new_record(d:TableDescriptor) -> Record:
    let fields:[Slot] = []
    for(d.fields) f:
        push(fields, new_slot(f))
    return Record { d: d, fields: fields, retained: [] }

// slot_named is the Slot of rec's field whose schema name is name, the way a
// caller reaches a field of a loaded or cooked Record (docs/PORT-LOBSTER.md,
// THE LOAD). A name the table does not declare is REFUSED BY NAME in every
// build: a VM bounds error that names the table and the field, which
// --runtime-no-asserts keeps.
def slot_named(rec:Record, name:string) -> Slot:
    for(rec.fields) sl:
        if sl.fd.name == name:
            return sl
    return rec.fields[length(rec.fields) + length(["no field: " + rec.d.name + "." + name][1])]

def new_slot(f:FieldDescriptor) -> Slot:
    let sl = Slot { fd: f, present: false, count: 0, cell: Value {}, elems: [], entries: [] }
    reset_slot(sl)
    return sl

def reset_slot(sl:Slot):
    let f = sl.fd
    sl.present = false
    sl.count = 0
    sl.cell = Value {}
    sl.elems = []
    sl.entries = []
    if f.s == "map":
        return
    if f.s == "text" or f.s == "bytes" or (f.s == "str" and not f.wide):
        if length(f.def_str) > 0:
            sl.cell.s = copy(f.def_str)
            sl.count = length(f.def_str)
        return
    if f.s == "str":
        return
    if f.s == "arr":
        for(f.bound):
            push(sl.elems, element_zero(f))
        return
    sl.cell = field_default(f)

// element_zero is one value-initialized array slot: a nested table at its
// defaults, everything else zero.
def element_zero(f:FieldDescriptor) -> Value:
    let e = f.e
    if e and e.s == "tab":
        return Value { tab: new_record(table_of(e.t)) }
    return Value {}

// field_default is a non-array field's declared default.
def field_default(f:FieldDescriptor) -> Value:
    let s = f.s
    if s == "str" or s == "text" or s == "bytes":
        return Value { s: copy(f.def_str) }
    if s == "bool":
        return Value { b: f.def_bool }
    if s == "float":
        return Value { f: f.def_float }
    if s == "int":
        if kind_wide(f.k):
            return Value { w: f.def_int }
        return Value { i: f.def_int.lo }
    if s == "enum":
        return Value { i: f.def_int.lo }
    if s == "tab":
        return Value { tab: new_record(table_of(f.t)) }
    return Value {}

// forget discards every retained field under a body the wire is reading
// AGAIN (Go's retainState.forget, docs/SPEC-TABLES.md §6.6): the body's own,
// and every body it holds by value, through tables, arms, elements and map
// entries, never through a pointer. A discard moves no counter. A body reset
// by DAMAGE is not forgotten: its retained fields stand, and a save writes
// them, or counts them lost when it never reaches the body.
def forget(rec:Record?) -> void:
    let work:[Record] = []
    if rec:
        push(work, rec)
    while length(work) > 0:
        let r = pop(work)
        r.retained = []
        for(r.fields) sl:
            forget_slot(work, sl)

def forget_slot(work:[Record], sl:Slot) -> void:
    forget_cell(work, sl.cell)
    for(sl.elems) v:
        forget_cell(work, v)
    for(sl.entries) v:
        forget_cell(work, v)

def forget_cell(work:[Record], v:Value) -> void:
    let t = v.tab
    if t:
        push(work, t)
    let a = v.arm
    if a:
        forget_slot(work, a)

// forget_value is forget over one cell
def forget_value(v:Value) -> void:
    let work:[Record] = []
    forget_cell(work, v)
    for(work) r:
        forget(r)

// refill re-establishes a record's defaults IN PLACE, keeping every nested
// record it holds (Go's Model.Refill): a by-value body is a member of its
// parent, reset rather than replaced.
def refill(rec:Record) -> void:
    for(rec.fields) sl:
        let held = sl.cell.tab
        let elems:[Record?] = []
        for(sl.elems) v:
            push(elems, v.tab)
        reset_slot(sl)
        if held:
            refill(held)
            sl.cell.tab = held
        for(elems) t, j:
            if t and j < length(sl.elems):
                refill(t)
                sl.elems[j].tab = t

// THE DISPATCH TABLE (docs/PORT-LOBSTER.md, THE LOAD): one Shape per shape
// key. field reads a field's payload into its slot (Go's field), element
// reads array element i (Go's element), arm reads a union arm's payload
// bounded by its L (Go's arm). The base reads a fixed-width scalar, which is
// the reference's own fall-through.
class Shape:
    key:string
    def field(r:Reader, sl:Slot) -> bool:
        return scalar_at(r, sl.cell, sl.fd, sl.fd.k, sl.fd.k)
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        return scalar_at(r, sl.elems[i], elem_of(sl.fd), sl.fd.ek, ek)
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        return scalar_at(r, fv.cell, fv.fd, fv.fd.k, fv.fd.k)

let shape_registry:[Shape] = []

// register_shape adds a shape under its key; the last registration of a key
// is the one a load reads with.
def register_shape(s:Shape) -> Shape:
    push(shape_registry, s)
    return s

def shape(key:string) -> Shape:
    var i = length(shape_registry)
    while i > 0:
        i--
        if shape_registry[i].key == key:
            return shape_registry[i]
    return shape_registry[0]

// shape_key is the key a field, an element or an arm dispatches on.
def shape_key(fd:FieldDescriptor) -> string:
    let s = fd.s
    if s == "arr":
        return if fd.array == "keyed": "keyed" else: "arr"
    if s == "str" and fd.wide:
        return "wstr"
    if s == "text":
        return "str"
    if s == "sblob" or s == "bblob":
        return "ptr"
    if s == "bool" or s == "int" or s == "float":
        return "scalar"
    return s

// elem_key is an array's element shape: a PLAIN TEXT LIST's element rides
// as kind 12 into its element record (§2.13).
def elem_key(f:FieldDescriptor) -> string:
    let e = elem_of(f)
    if e.s == "tab" and f.ek == 12:
        return "textlist"
    return shape_key(e)

def elem_of(f:FieldDescriptor) -> FieldDescriptor:
    let e = f.e
    if e:
        return e
    return FieldDescriptor {}

let scalar_shape = register_shape(Shape { key: "scalar" })

// the KEYED KEY VARIANTS: an enum-keyed array's key enum, as { name, id }
// pairs, for the descriptor that does not carry them yet
// (docs/PORT-LOBSTER.md, THE LOAD). Registered per "<Table>.<field>".
class KeyedKeys:
    fd:FieldDescriptor
    vs:[EnumVariant]

let keyed_registry:[KeyedKeys] = []

def keyed_keys(owner:string, field:string, vs:[EnumVariant]):
    let d = table_of(owner)
    for(d.fields) f:
        if f.name == field:
            push(keyed_registry, KeyedKeys { fd: f, vs: vs })

def keyed_variants(f:FieldDescriptor) -> [EnumVariant]:
    for(keyed_registry) k:
        if k.fd == f:
            return k.vs
    return []

// enum_value is the declaration-side value a variant id names (1-based), -1
// when no variant does.
def enum_value(vs:[EnumVariant], id:int) -> int:
    for(vs) v, i:
        if v.id == id:
            return i + 1
    return -1

// enum_cell reads an enum value (§3, kind 30): 0 is None, a variant this
// enum cannot name reads None and counts unknown, a reference past the id
// table is damage.
def enum_cell(r:Reader, cell:Value, fd:FieldDescriptor) -> bool:
    let rep = r.st.report
    let ref, ok = leb(r)
    if not ok:
        rep.malformed = true
        return false
    if ref == 0:
        cell.i = 0
        return true
    let id, named = rid(r, ref)
    if not named:
        rep.malformed = true
        return false
    let v = enum_value(fd.vs, id)
    if v < 0:
        cell.i = 0
        rep.unknown++
        let retain = active_retain_capture()
        if retain:
            retain.lost()
        return true
    cell.i = v
    return true

class EnumShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        return enum_cell(r, sl.cell, sl.fd)
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        return enum_cell(r, sl.elems[i], elem_of(sl.fd))
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let ok = enum_cell(r, fv.cell, fv.fd)
        if ok and r.off != r.lo + n:
            r.st.report.malformed = true
            return false
        return ok

let enum_shape = register_shape(EnumShape { key: "enum" })

// resolve_cell places one node index in a pointer slot (§3.1, §2.5; Go's
// resolveCell). Every failure is one of §4's events and the slot stays null.
def resolve_cell(r:Reader, cell:Value, fd:FieldDescriptor, index:int) -> bool:
    cell.node = nil
    cell.blob = nil
    cell.blob_node = nil
    let st = r.st
    let rep = st.report
    if index == 0:
        return true
    if not st.variable or not st.good:
        return read_refuse(rep, "wire_damaged")
    let blob = fd.s == "sblob" or fd.s == "bblob"
    if index == 1:
        let root = st.root
        if blob or not root or root.d.name != fd.t:
            return read_refuse(rep, "kind_mismatch")
        cell.node = root
        return true
    if index < 2 or index - 2 >= length(st.nodes):
        return read_refuse(rep, "wire_damaged")
    let node = st.nodes[index - 2]
    if node.kind == 0:
        return true // a type this reader cannot name is evolution
    if blob:
        let want = if fd.s == "sblob": 2 else: 3
        if node.kind != want:
            return read_refuse(rep, "kind_mismatch")
        cell.blob = node.blob
        cell.blob_node = node
        return true
    let nr = node.rec
    if node.kind != 1 or not nr or nr.d.name != fd.t:
        return read_refuse(rep, "kind_mismatch")
    cell.node = nr
    return true

class PointerShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        // a pointer's payload is a NUMBER, bounds-checked and stored, never
        // followed (§3.1)
        let index, ok = leb(r)
        if not ok:
            r.st.report.malformed = true
            return false
        return resolve_cell(r, sl.cell, sl.fd, index)
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        let index, ok = leb(r)
        if not ok:
            r.st.report.malformed = true
            return false
        return resolve_cell(r, sl.elems[i], elem_of(sl.fd), index)
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let index, ok = leb(r)
        if not ok or r.off != r.lo + n:
            r.st.report.malformed = true
            return false
        return resolve_cell(r, fv.cell, fv.fd, index)

let pointer_shape = register_shape(PointerShape { key: "ptr" })

// THE STRING (§3): ill-formed text is damage and reads the declared
// default; a clamp cuts at a code point boundary and counts.
class StringShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let rep = r.st.report
        let n, ok = leb(r)
        if not ok or not has(r, n):
            rep.malformed = true
            return false
        if not text_valid(r.b, r.off, n):
            rep.malformed = true
            return false
        let keep = n
        if keep > sl.fd.bound:
            return read_refuse(rep, "length_over_bound")
        sl.cell.s = substring(r.b, r.off, keep)
        sl.count = keep
        r.off += n
        return true
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let rep = r.st.report
        if not text_valid(r.b, r.off, n):
            rep.malformed = true
            return false
        let keep = n
        if keep > fv.fd.bound:
            return read_refuse(rep, "length_over_bound")
        fv.cell.s = substring(r.b, r.off, keep)
        fv.count = keep
        r.off += n
        return true

let string_shape = register_shape(StringShape { key: "str" })

// WIDE TEXT (§3, kind 33): an odd L is damage, as is an unpaired surrogate
// or a zero unit; a clamp never splits a pair.
class WideShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let rep = r.st.report
        let n, ok = leb(r)
        if not ok or not has(r, n):
            rep.malformed = true
            return false
        let at = r.off
        r.off += n
        if (n & 1) != 0:
            rep.malformed = true
            return false
        let units = wide_units(r.b, at, n)
        if not wide_valid(units):
            rep.malformed = true
            return false
        let keep = length(units)
        if keep > sl.fd.bound:
            return read_refuse(rep, "length_over_bound")
        sl.cell.units = slice(units, 0, keep)
        sl.count = keep
        return true
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let rep = r.st.report
        if (n & 1) != 0:
            rep.malformed = true
            return false
        let units = wide_units(r.b, r.off, n)
        if not wide_valid(units):
            rep.malformed = true
            return false
        let keep = length(units)
        if keep > fv.fd.bound:
            return read_refuse(rep, "length_over_bound")
        fv.cell.units = slice(units, 0, keep)
        fv.count = keep
        r.off += n
        return true

let wide_shape = register_shape(WideShape { key: "wstr" })

// A nested body must decode completely inside its own length.
def nested_body(r:Reader, sl:Slot, i:int) -> bool:
    let n, ok = leb(r)
    if not ok or not has(r, n):
        return read_refuse(r.st.report, "wire_damaged")
    let rec = new_record(table_of(if i < 0: sl.fd.t else: elem_of(sl.fd).t))
    if i < 0:
        sl.cell.tab = rec
    else:
        sl.elems[i].tab = rec
    let sr = sub(r, n)
    if not body_at(sr, rec, true, false) or sr.off != sr.hi:
        return read_refuse(r.st.report, "wire_damaged")
    r.off += n
    return true

class TableShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        return nested_body(r, sl, -1)
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        return nested_body(r, sl, i)

let table_shape = register_shape(TableShape { key: "tab" })

// A plain text element has its own length and must fit its declared bound.
class TextListShape : Shape
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        let rep = r.st.report
        let n, ok = leb(r)
        if not ok or not has(r, n):
            rep.malformed = true
            return false
        if not text_valid(r.b, r.off, n):
            rep.malformed = true
            return false
        let ed = table_of(elem_of(sl.fd).t)
        let keep = n
        let bound = ed.fields[0].bound
        if keep > bound:
            return read_refuse(rep, "length_over_bound")
        let held = sl.elems[i].tab
        var rec = new_record(ed)
        if held:
            rec = held
        sl.elems[i].tab = rec
        rec.fields[0].cell.s = substring(r.b, r.off, keep)
        rec.fields[0].count = keep
        r.off += n
        return true

let text_list_shape = register_shape(TextListShape { key: "textlist" })

// Every collection count must fit the int32 storage cap.
def collection_cap(r:Reader) -> bool:
    r.st.count_refused = true
    return read_refuse(r.st.report, "count_over_extent_cap")

// array_body decodes an array or bytes payload (§2.9, §3; Go's arrayBody).
// framed < 0 reads the field's own L; an arm passes the L it was framed by.
// It returns ok (false: the walk stops) and selected (false: the body
// declares another element kind, so an arm holding it is not selected).
def array_body(r:Reader, sl:Slot, framed:int) -> bool, bool:
    let rep = r.st.report
    let f = sl.fd
    let is_bytes = f.s == "bytes"
    var body_len = framed
    if framed < 0:
        let n, good = leb(r)
        if not good:
            return read_refuse(rep, "wire_damaged"), false
        body_len = n
    if body_len < 2 or not has(r, body_len):
        return read_refuse(rep, "wire_damaged"), false
    let sr = sub(r, body_len)
    let ek = u8(sr)
    let count, good = leb(sr)
    if not good:
        return read_refuse(rep, "wire_damaged"), false
    if ek != f.ek and not widens(ek, f.ek):
        return read_refuse(rep, "kind_mismatch"), false
    if count < 0 or count > 0x7FFFFFFF:
        return collection_cap(r), false
    if f.array != "list" and count > f.bound:
        return read_refuse(rep, if is_bytes: "length_over_bound" else: "count_over_bound"), false
    if ek != f.ek:
        rep.widened++
    if is_bytes:
        if not has(sr, count):
            return read_refuse(rep, "wire_damaged"), false
        sl.cell.s = substring(sr.b, sr.off, count)
        sr.off += count
    else:
        if f.array == "list":
            sl.elems = []
        let es = shape(elem_key(f))
        var i = 0
        while i < count:
            if f.array == "list":
                push(sl.elems, element_zero(f))
            if not es.element(sr, sl, i, ek):
                return false, false
            i++
    if sr.off != sr.hi:
        return read_refuse(rep, "wire_damaged"), false
    if is_bytes or f.array == "counted" or f.array == "list":
        sl.count = count
    if f.present != "":
        sl.present = true
    r.off += body_len
    return true, true

class ArrayShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let ok = array_body(r, sl, -1)
        return ok
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let ok, selected = array_body(r, fv, n)
        return ok and selected

let array_shape = register_shape(ArrayShape { key: "arr" })
let bytes_shape = register_shape(ArrayShape { key: "bytes" })

// AN ENUM-KEYED ARRAY (§3.2; Go's keyed): triples placed BY KEY REFERENCE. A
// key this reader cannot name is skipped and counted unknown; a key of 0 is
// damage.
class KeyedShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let rep = r.st.report
        let f = sl.fd
        let n, ok = leb(r)
        if not ok or n < 2 or not has(r, n):
            return read_refuse(rep, "wire_damaged")
        let sr = sub(r, n)
        let ek = u8(sr)
        let count, good = leb(sr)
        if not good or count < 0 or count > 0x7FFFFFFF:
            return read_refuse(rep, "wire_damaged")
        if ek != f.ek:
            if not widens(ek, f.ek):
                return read_refuse(rep, "kind_mismatch")
            rep.widened++
        let keys = keyed_variants(f)
        let e = elem_of(f)
        var i = 0
        while i < count:
            i++
            let kref, kgood = leb(sr)
            let key_id, named = rid(sr, kref)
            if not kgood or not named:
                return read_refuse(rep, "wire_damaged")
            let elem_len, lgood = leb(sr)
            if not lgood or not has(sr, elem_len):
                return read_refuse(rep, "wire_damaged")
            let v = enum_value(keys, key_id)
            if v < 1 or v > f.bound:
                rep.unknown++
                let retain = active_retain_capture()
                if retain:
                    retain.lost()
                sr.off += elem_len
                continue
            let slot = v - 1
            let er = sub(sr, elem_len)
            var decoded = false
            if e.s == "enum":
                decoded = enum_cell(er, sl.elems[slot], e)
            elif e.s == "tab":
                let rec = new_record(table_of(e.t))
                sl.elems[slot].tab = rec
                decoded = body_at(er, rec, true, false)
            else:
                decoded = scalar_at(er, sl.elems[slot], e, f.ek, ek)
            if not decoded or er.off != er.hi:
                return read_refuse(rep, "wire_damaged")
            sr.off += elem_len
        if sr.off != sr.hi:
            return read_refuse(rep, "wire_damaged")
        r.off += n
        return true

let keyed_shape = register_shape(KeyedShape { key: "keyed" })

// THE UNION (§2.6, §3; Go's unionCell): AN ARM HEADER IS A FIELD HEADER —
// the arm id reference, the arm's kind byte, L, then L bytes of payload. An
// arm this reader cannot name is unknown and the union reads None; a
// retyped arm refuses unless its kind widens. Its payload must consume L.
def reset_union(cell:Value):
    cell.i = 0
    cell.tab = nil
    cell.arm = nil

def arm_wire_kind(a:FieldDescriptor) -> int:
    if a.s == "void":
        return 32
    return a.k

// arm_fixed_width is the byte count a fixed-width arm owes under its L, 0
// for a length- or reference-shaped arm (Go's ir.ArmWireFixedWidth).
def arm_fixed_width(a:FieldDescriptor) -> int:
    let s = a.s
    if s == "arr" or s == "str" or s == "text" or s == "bytes" or s == "ptr" or s == "sblob" or s == "bblob" or s == "tab" or s == "union" or s == "enum" or s == "map":
        return 0
    return kind_width(a.k)

def arm_key(a:FieldDescriptor) -> string:
    if a.s == "arr" or a.s == "bytes":
        return "arr"
    return shape_key(a)

// arm_read decodes one non-body arm's payload out of a reader bounded to its
// L (Go's arm).
def arm_read(r:Reader, cell:Value, a:FieldDescriptor, tag:int, n:int) -> bool:
    let rep = r.st.report
    if a.s == "void":
        if n != 0:
            rep.malformed = true
            return false
        cell.i = tag
        cell.arm = nil
        return true
    let w = arm_fixed_width(a)
    if w > 0 and n != w:
        rep.malformed = true
        return false
    let fv = new_slot(a)
    if not shape(arm_key(a)).arm(r, fv, n):
        return false
    cell.i = tag
    cell.arm = fv
    return true

def union_cell(r:Reader, cell:Value, fd:FieldDescriptor) -> bool:
    // A UNION WRITTEN AGAIN takes the earlier arm's retained fields with it
    // (Go's forgetArm), and the discard moves no counter
    forget_value(cell)
    let rep = r.st.report
    let un = lookup_union(fd.t)
    assert un
    let ref, ok = leb(r)
    if not ok:
        rep.malformed = true
        return false
    if ref == 0:
        reset_union(cell)
        return true
    let arm_id, named = rid(r, ref)
    if not named:
        rep.malformed = true
        return false
    if not has(r, 1):
        rep.malformed = true
        return false
    let kind = u8(r)
    let n, good = leb(r)
    if not good or not has(r, n):
        rep.malformed = true
        return false
    let sr = sub(r, n)
    var tag = 0
    for(un.arms) a, i:
        if tag == 0 and a.id == arm_id:
            tag = i + 1
    if tag == 0:
        reset_union(cell)
        rep.unknown++
        let retain = active_retain_capture()
        if retain:
            retain.lost()
    else:
        let a = un.arms[tag - 1]
        let want = arm_wire_kind(a)
        if kind != want:
            reset_union(cell)
            if widens(kind, want):
                // WIDENED AT AN ARM (§4): decoded at its own width
                if n != kind_width(kind):
                    return read_refuse(rep, "wire_damaged")
                else:
                    let fv = new_slot(a)
                    if scalar_at(sr, fv.cell, a, a.k, kind):
                        cell.i = tag
                        cell.arm = fv
                        rep.widened++
                    else:
                        return false
            else:
                return read_refuse(rep, "kind_mismatch")
        elif a.s == "tab":
            let payload = new_record(table_of(a.t))
            cell.tab = payload
            if not body_at(sr, payload, true, false):
                return false
            if sr.off != sr.hi:
                return read_refuse(rep, "wire_damaged")
            else:
                cell.i = tag
                cell.tab = payload
        else:
            cell.tab = nil
            if not arm_read(sr, cell, a, tag, n):
                return false
            if sr.off != sr.hi:
                return read_refuse(rep, "wire_damaged")
    r.off += n
    return true

class UnionShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        return union_cell(r, sl.cell, sl.fd)
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        // an element of an array of unions is an arm header, reset the
        // moment its arm id is read (§3)
        if not has(r, 1):
            r.st.report.malformed = true
            return false
        sl.elems[i].i = 0
        sl.elems[i].tab = nil
        return union_cell(r, sl.elems[i], elem_of(sl.fd))
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        return union_cell(r, fv.cell, fv.fd)

let union_shape = register_shape(UnionShape { key: "union" })

// Map entries decode once, including their required key. Keys must be
// strictly ascending; any key or value violation refuses the whole load.
def key_is_string(k:FieldDescriptor) -> bool:
    return k.s == "text" or (k.s == "str" and not k.wide)

def map_key_order(k:FieldDescriptor, a:Value, b:Value) -> int:
    if key_is_string(k):
        let n = min(length(a.s), length(b.s))
        var i = 0
        while i < n:
            if a.s[i] != b.s[i]:
                return if a.s[i] < b.s[i]: -1 else: 1
            i++
        if length(a.s) == length(b.s):
            return 0
        return if length(a.s) < length(b.s): -1 else: 1
    if k.s == "int" and k.ibits == 0 and k.fbits == 0 and (k.signed or k.timestamp):
        if a.i == b.i:
            return 0
        return if a.i < b.i: -1 else: 1
    if a.i == b.i:
        return 0
    return if ult(a.i, b.i): -1 else: 1

class MapShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let rep = r.st.report
        let n, ok = leb(r)
        if not ok or n < 2 or not has(r, n):
            return read_refuse(rep, "wire_damaged")
        let sr = sub(r, n)
        let ek = u8(sr)
        let count, good = leb(sr)
        if not good:
            return read_refuse(rep, "wire_damaged")
        if ek != 13:
            return read_refuse(rep, "kind_mismatch")
        if count < 0 or count > 0x7FFFFFFF:
            return collection_cap(r)
        let ed = table_of(sl.fd.t)
        let k = ed.fields[0]
        let keep = active_retain_capture()
        sl.entries = []
        var last = Value {}
        var i = 0
        while i < count:
            let elem_len, lgood = leb(sr)
            if not lgood or not has(sr, elem_len):
                return read_refuse(rep, "wire_damaged")
            let body = sub(sr, elem_len)
            let decoded = new_record(ed)
            push(sl.entries, Value { tab: decoded })
            if not body_at(body, decoded, true, true) or body.off != body.hi:
                return read_refuse(rep, "wire_damaged")
            let entry_key = decoded.fields[0].cell
            if i > 0 and map_key_order(k, last, entry_key) >= 0:
                return read_refuse(rep, "wire_damaged")
            if keep:
                keep.built(sl, decoded)
            last = entry_key
            sr.off += elem_len
            i++
        if sr.off != sr.hi:
            return read_refuse(rep, "wire_damaged")
        r.off += n
        return true

let map_shape = register_shape(MapShape { key: "map" })

// slot_by_id is the slot a field id names in a record, or nil.
def slot_by_id(rec:Record, id:int) -> Slot?:
    for(rec.fields) sl:
        if sl.fd.id == id:
            return sl
    return nil

// mark_seen records that this body NAMED the slot's field (§2.3): a required
// field the body names is not missing, whatever its kind or value.
def mark_seen(rec:Record, sl:Slot, seen:[bool]):
    for(rec.fields) x, i:
        if x == sl:
            seen[i] = true
            return

// body_at decodes one table body (§3, §4; Go's bodyAt): unknown ids skip by
// their kind and count after wire validation. Widening decodes; an
// incompatible known kind or any failed child stops the entire read.
// nested: a body that is not the root's, where the node table's reserved id
// is malformed (§3.1). entry: a map entry's body, whose widened key the map
// counts once (§2.8).
def body_at(r:Reader, rec:Record, nested:bool, entry:bool) -> bool:
    let rep = r.st.report
    // THE `| required` SEEN FLAGS (docs/SPEC-TABLES.md §2.3; Go's reqSeen):
    // one per field of this body, set when the body NAMES the field, and
    // judged at its terminator. A child body's refusal rides up (the report
    // is the record's at any depth).
    let missing_before = rep.missing_required
    let seen:[bool] = []
    for(rec.fields):
        push(seen, false)
    while true:
        if r.st.count_refused:
            return false
        let ref, ok = leb(r)
        if not ok:
            rep.malformed = true
            return false
        if ref == 0:
            // An elided map key is its default (zero or empty text), as
            // every other absent field. MapShape still checks strict order.
            let missing:[string] = []
            for(rec.fields) x, i:
                if x.fd.required and not seen[i]:
                    push(missing, x.fd.name)
            if length(missing) > 0:
                rep.missing_required++
                var names = ""
                for(missing) nm:
                    if names != "":
                        names += ", "
                    names += nm
                rep.reason = "missing_required: " + names
                return false
            if rep.missing_required > missing_before:
                return false
            return true
        let id, named = rid(r, ref)
        if not named or not has(r, 1):
            rep.malformed = true
            return false
        let kind = u8(r)
        if id == build_version_id or id == message_vocabulary_id:
            // the reserved announcement ids ride nowhere but the
            // announcement (§3.3)
            rep.malformed = true
            return false
        if id == node_table_id:
            if nested:
                rep.malformed = true
                return false
            // Every reader knows this reserved field. The wire pass has
            // already checked its records and graph, even for a value-only
            // declaration; it is never an unknown field to retain.
            if not skip(r, kind):
                return read_refuse(rep, "wire_damaged")
            continue
        let found = slot_by_id(rec, id)
        if not found:
            rep.unknown++
            let at0 = r.off
            if not skip(r, kind):
                rep.malformed = true
                return false
            let retain = active_retain_capture()
            if retain:
                let payload = substring(r.b, at0, r.off - at0)
                if not retained_payload_finite(kind, payload):
                    // A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE
                    // (docs/SPEC-TABLES.md §3.3, §6.6; Go's
                    // retainedPayloadFinite): a NaN or an infinity refuses
                    // the read, named <Table>.0x<id>
                    if rep.non_finite_float == "":
                        rep.non_finite_float = rec.d.name + ".0x" + hex64(id)
                    return read_refuse(rep, "non_finite_float")
                retain.capture(rec, id, kind, payload, r.st.ids)
            continue
        let sl = found
        let f = sl.fd
        if kind != f.k:
            if widens(kind, f.k):
                if not scalar_at(r, sl.cell, f, f.k, kind):
                    return false
                if not entry or f.name != "key":
                    rep.widened++
                if f.present != "":
                    sl.present = true
                mark_seen(rec, sl, seen)
                continue
            return read_refuse(rep, "kind_mismatch")
        mark_seen(rec, sl, seen)
        if f.s == "tab" or f.s == "union":
            // THE FIELD IS BEING READ AGAIN, whole (Go's discardsWhole and
            // forgetField): every retained field under it goes first
            forget_value(sl.cell)
        if not shape(shape_key(f)).field(r, sl):
            return false
        if f.present != "" and f.s != "arr":
            sl.present = true
    return false

// THE ID TABLE (§3; Go's trailer): the final eight bytes are the entry
// count, the 8 x count bytes before them the entries; a table that cannot be
// read whole, or that carries one id twice, is malformed.
def trailer(data:string) -> [int], int, bool:
    let size = length(data)
    if size < 9:
        return [], 0, false
    let count = read_int64_le(data, size - 8)
    // THE ENTRY COUNT IS BOUNDED AT 4096 (§3, the distinctness check;
    // docs/LAYERS/1-framing.md F6): a count above it is malformed whatever
    // the buffer would hold, and at or below it the entries are proved
    // distinct below
    if count < 0 or count > 4096 or count > size / 8:
        return [], 0, false
    let span = count * 8 + 8
    if span + 1 > size:
        return [], 0, false
    let first = size - span
    let ids = []
    var i = 0
    while i < count:
        let id = read_int64_le(data, first + i * 8)
        for(ids) seen:
            if seen == id:
                return [], 0, false
        push(ids, id)
        i++
    return ids, first, true

// body_extent walks the root body's framing to its terminator, so a body
// that ENDED EARLY is told from one that is merely damaged (Go's bodyExtent).
def body_extent(st:LoadState, hi:int) -> int, bool:
    let scratch = LoadState { data: st.data, ids: st.ids, report: LoadReport {}, variable: false, good: false, root: nil, nodes: [], named: [], count_refused: false }
    let r = reader(scratch, 1, hi)
    while true:
        let ref, ok = leb(r)
        if not ok:
            return 0, false
        if ref == 0:
            return r.off, true
        if ult(length(st.ids), ref) or not has(r, 1):
            return 0, false
        if not skip(r, u8(r)):
            return 0, false
    return 0, false

// node_table_bytes finds the node table in the ROOT body (§3.1; Go's
// nodeTableBytes): the one top-level kind 12 field under the reserved id,
// whole or nothing. It returns the payload's span, whether the field was
// present, and whether it framed.
def node_table_bytes(st:LoadState, hi:int) -> int, int, bool, bool:
    let scratch = LoadState { data: st.data, ids: st.ids, report: LoadReport {}, variable: false, good: false, root: nil, nodes: [], named: [], count_refused: false }
    let r = reader(scratch, 1, hi)
    var plo = 0
    var phi = 0
    var present = false
    while true:
        let ref, ok = leb(r)
        if not ok or ref == 0:
            return plo, phi, present, true
        let id, named = rid(r, ref)
        if not named or not has(r, 1):
            return plo, phi, present, true
        let kind = u8(r)
        if id == node_table_id:
            present = true
            if kind != 12:
                st.report.malformed = true
                return 0, 0, present, false
            let n, nok = leb(r)
            if not nok or not has(r, n):
                return 0, 0, present, false
            plo = r.off
            phi = r.off + n
            r.off += n
            continue
        if not skip(r, kind):
            return plo, phi, present, true
    return plo, phi, present, true

// A NodeRecord is one record as the scan found it: its type id and body.
class NodeRecord:
    tid:int
    lo:int
    hi:int

// scan_node_records is THE AUTHORITATIVE RECORD SCAN (§3.1; Go's
// scanNodeRecords): records until the payload is consumed, and a node_count
// that disagrees with the scan is malformed.
def scan_node_records(st:LoadState, plo:int, phi:int) -> [NodeRecord], bool:
    let data = st.data
    let declared, first, ok = read_leb(data, plo, phi)
    if not ok:
        return [], false
    var off = first
    let records = []
    while off < phi:
        let ref, next, good = read_leb(data, off, phi)
        if not good or ref == 0 or ult(length(st.ids), ref):
            return [], false
        let size, after, lgood = read_leb(data, next, phi)
        if not lgood:
            return [], false
        if size < 0 or size > phi - after:
            return [], false
        push(records, NodeRecord { tid: st.ids[ref - 1], lo: after, hi: after + size })
        off = after + size
    if off != phi or declared != length(records):
        return [], false
    return records, true

// decode_variable reads a pointered root (§3.1; Go's decodeVariable): the
// node table first, then pass one fills THE FLAT NODE TABLE from the framing
// (a record this root cannot place is unknown and keeps its index), pass two
// decodes each record's body, then the root body.
def decode_variable(st:LoadState, root:Record, hi:int):
    let rep = st.report
    st.variable = true
    let plo, phi, present, framed = node_table_bytes(st, hi)
    var records:[NodeRecord] = []
    var scanned = true
    if framed and present:
        records, scanned = scan_node_records(st, plo, phi)
    if not framed or not scanned:
        read_refuse(rep, "wire_damaged")
        return
    st.good = true
    let place:[TableDescriptor] = []
    for(root.d.place_tables) name:
        push(place, table_of(name))
    for(records) rec:
        var node = Node { kind: 0, rec: nil, blob: nil }
        if rec.tid == string_blob_id and root.d.place_str:
            if not text_valid(st.data, rec.lo, rec.hi - rec.lo):
                rep.malformed = true
            else:
                node = Node { kind: 2, rec: nil, blob: substring(st.data, rec.lo, rec.hi - rec.lo) }
        elif rec.tid == bytes_blob_id and root.d.place_bytes:
            node = Node { kind: 3, rec: nil, blob: substring(st.data, rec.lo, rec.hi - rec.lo) }
        else:
            var d:TableDescriptor? = nil
            for(place) p:
                if p.id == rec.tid:
                    d = p
            if d:
                node = Node { kind: 1, rec: new_record(d), blob: nil }
            else:
                rep.unknown++
                let retain = active_retain_capture()
                if retain:
                    retain.lost()
        push(st.nodes, node)
    for(records) rec, i:
        let node = st.nodes[i]
        let nr = node.rec
        if node.kind == 1 and nr:
            let body = reader(st, rec.lo, rec.hi)
            if not body_at(body, nr, true, false) or body.off != body.hi:
                read_refuse(rep, "wire_damaged")
                break
    if not rep.refused:
        if not body_at(reader(st, 1, hi), root, false, false):
            read_refuse(rep, "wire_damaged")
    if length(st.nodes) > 0:
        // A CYCLIC WIRE IS DAMAGE (docs/SPEC-TABLES.md §3.1, §4): the graph
        // the load stored is checked once every index has resolved
        if not st.report.malformed and load_cyclic(st, root):
            st.report.malformed = true
        unlink_unreached(st, root)
    if rep.refused:
        for(st.nodes) node:
            release(node.rec)

// load_cyclic reports whether the loaded graph holds a cycle. No writer
// produces a cyclic wire, so a cycle anywhere refuses the load, one the root
// never reaches included. The vertices are the root and every node record;
// the edges are the pointers each one's by-value closure stores (rec_edges).
// Each vertex is entered once: a pointer to an OPEN vertex is a cycle, and a
// pointer to a CLOSED one is a legal share. The walk keeps its own stack, so
// it is linear in vertices plus edges and a chain costs no stack.
def load_cyclic(st:LoadState, root:Record) -> bool:
    let all:[Record] = [root]
    for(st.nodes) node:
        let nr = node.rec
        if nr:
            push(all, nr)
    var found = false
    for(all) start:
        if not found and start.walk == 0:
            start.walk = 1
            let first:[Record] = []
            rec_edges(start, first, false)
            let stack:[Record] = [start]
            let edges:[[Record]] = [first]
            let at:[int] = [0]
            while not found and length(stack) > 0:
                let top = length(stack) - 1
                let es = edges[top]
                if at[top] >= length(es):
                    stack[top].walk = 2
                    pop(stack)
                    pop(edges)
                    pop(at)
                else:
                    let t = es[at[top]]
                    at[top] = at[top] + 1
                    if t.walk == 1:
                        found = true
                    elif t.walk == 0:
                        t.walk = 1
                        let out:[Record] = []
                        rec_edges(t, out, false)
                        push(stack, t)
                        push(edges, out)
                        push(at, 0)
    for(all) r:
        r.walk = 0
    return found

// The first local refusal diagnostic, or the empty string after success.
def strict_reason(rep:LoadReport) -> string:
    if rep.refused or rep.stopped or rep.malformed or rep.damaged:
        if rep.reason != "":
            return rep.reason
        return "wire_damaged"
    return ""

// The single read returns a complete value or nil. Unknowns and legal
// widening preserve evolution; diagnostics do not enable partial values.
def load(root:string, data:string, report:LoadReport) -> Record?:
    read_reset(report)
    if table_of(root).fixed:
        if length(data) == 0:
            read_refuse(report, "truncated")
            return nil
        if data[0] != 3:
            read_refuse(report, if data[0] == 1: "previous_form" else: if data[0] == 2: "message_form_as_file" else: "newer_form")
            return nil
        return fixed_record_codec(root).read_fixed(data, report)
    let rec = load_walk(root, data, report)
    if report.non_finite_float != "":
        report.reason = "non_finite_float: " + report.non_finite_float
        release(rec)
        return nil
    if report.refused or report.malformed or report.stopped:
        release(rec)
        if report.reason == "":
            read_refuse(report, "wire_damaged")
        return nil
    return rec

// RELEASE (docs/PORT-LOBSTER.md, THE LOAD). Lobster frees by reference
// counting, and a cycle of Records outlives every reference the caller holds:
// a load that refuses a cyclic wire (§3.1, §4) has still built one, and so
// has a load an orphan cycle never reaches. Two rules keep a load from
// leaking:
//
// - THE LOAD OWNS WHAT THE ROOT DOES NOT REACH. At the end of a variable
//   load, a node record no pointer path from the root reaches (an orphan,
//   or a cycle among orphans) has its own pointer edges unlinked, so nothing
//   holds it once the load returns. A reached record is untouched.
// - THE CALLER OWNS WHAT THE ROOT REACHES, and release(root) is the
//   owner-side end of it: every pointer edge reachable from the root is
//   cleared, so each record's count falls to what the caller still holds.
//   A load that answers nil after decoding (a refusal) releases its own
//   record first. After release the graph's pointers read null.
//
// Both walks clear an edge before following it, so they end on any graph,
// and both keep an explicit work list, so a long chain costs no stack.

// rec_edges walks one record and the records it holds BY VALUE (bodies,
// arms, elements, map entries), collecting the pointer targets into nodes,
// and clearing each pointer edge when clear is set.
def rec_edges(rec:Record, nodes:[Record], clear:bool) -> void:
    let stack:[Record] = [rec]
    while length(stack) > 0:
        let r = pop(stack)
        for(r.fields) sl:
            slot_edges(stack, nodes, sl, clear)

def slot_edges(stack:[Record], nodes:[Record], sl:Slot, clear:bool) -> void:
    value_edges(stack, nodes, sl.cell, clear)
    for(sl.elems) v:
        value_edges(stack, nodes, v, clear)
    for(sl.entries) v:
        value_edges(stack, nodes, v, clear)

def value_edges(stack:[Record], nodes:[Record], v:Value, clear:bool) -> void:
    let n = v.node
    if n:
        push(nodes, n)
        if clear:
            v.node = nil
    let t = v.tab
    if t:
        push(stack, t)
    let a = v.arm
    if a:
        slot_edges(stack, nodes, a, clear)

// unlink_unreached is the load's own pass: mark what the root reaches, unlink
// every node record it does not, then clear the marks.
def unlink_unreached(st:LoadState, root:Record) -> void:
    let marked:[Record] = [root]
    let work:[Record] = [root]
    root.held = true
    while length(work) > 0:
        let found:[Record] = []
        rec_edges(pop(work), found, false)
        for(found) n:
            if not n.held:
                n.held = true
                push(marked, n)
                push(work, n)
    for(st.nodes) node:
        let nr = node.rec
        if nr and not nr.held:
            rec_edges(nr, [], true)
    for(marked) r:
        r.held = false

// release is the owner-side end of a loaded graph (above).
def release(root:Record?) -> void:
    let work:[Record] = []
    if root:
        push(work, root)
    while length(work) > 0:
        let found:[Record] = []
        rec_edges(pop(work), found, true)
        for(found) n:
            push(work, n)

// The declaration-free pass inspects every payload, including unknown fields
// and records. An unknown value is skipped only after it is proved lawful.
// The pointer graph is built from wire indices, never from the known subset.
class WireCheck:
    st:LoadState
    edges:[int] = []
    node_lo:int = -1
    node_hi:int = -1
    depth:int = 0

let binary_max_depth = 128

def wire_scalar(c:WireCheck, r:Reader, kind:int) -> bool:
    let w = kind_width(kind)
    if not has(r, w):
        return read_refuse(c.st.report, "wire_damaged")
    if kind == 1 and r.b[r.off] > 1:
        return read_refuse(c.st.report, "wire_damaged")
    if kind == 10 and not finite_f32_bits(read_uint32_le(r.b, r.off)):
        return read_refuse(c.st.report, "non_finite_float")
    if kind == 11 and not finite_f64_bits(read_int64_le(r.b, r.off)):
        return read_refuse(c.st.report, "non_finite_float")
    r.off += w
    return true

def wire_payload(c:WireCheck, r:Reader, kind:int) -> bool:
    if (kind >= 1 and kind <= 11) or (kind >= 18 and kind <= 29) or kind == 36:
        return wire_scalar(c, r, kind)
    if kind == 17 or kind == 30:
        let v, ok = leb(r)
        if not ok:
            return read_refuse(c.st.report, "wire_damaged")
        if kind == 17:
            push(c.edges, v)
        elif v < 0 or v > length(c.st.ids):
            return read_refuse(c.st.report, "wire_damaged")
        return true
    if kind == 15:
        let arm, ok = leb(r)
        if not ok or arm < 0 or arm > length(c.st.ids):
            return read_refuse(c.st.report, "wire_damaged")
        if arm == 0:
            return true
        if not has(r, 1):
            return read_refuse(c.st.report, "wire_damaged")
        let ak = u8(r)
        let n, framed = leb(r)
        if not framed or not has(r, n):
            return read_refuse(c.st.report, "wire_damaged")
        if not wire_content(c, sub(r, n), ak):
            return false
        r.off += n
        return true
    if kind == 12 or kind == 13 or kind == 14 or kind == 16 or kind == 31 or kind == 32 or kind == 33:
        let n, ok = leb(r)
        if not ok or not has(r, n):
            return read_refuse(c.st.report, "wire_damaged")
        if not wire_content(c, sub(r, n), kind):
            return false
        r.off += n
        return true
    return read_refuse(c.st.report, "wire_damaged")

def wire_content(c:WireCheck, r:Reader, kind:int) -> bool:
    let nested = kind >= 13 and kind <= 16
    if nested:
        if c.depth >= binary_max_depth:
            return read_refuse(c.st.report, "depth_over_bound")
        c.depth++
    let ok = wire_content_at(c, r, kind)
    if nested:
        c.depth--
    return ok

// Structured kinds 13 through 16 add a level, including a nested union.
// A scalar arm's length adds none. Pointer records start at depth zero.
def wire_content_at(c:WireCheck, r:Reader, kind:int) -> bool:
    if kind == 12:
        if not text_valid(r.b, r.off, r.hi - r.off):
            return read_refuse(c.st.report, "wire_damaged")
        return true
    if kind == 33:
        let n = r.hi - r.off
        if (n & 1) != 0 or not wide_valid(wide_units(r.b, r.off, n)):
            return read_refuse(c.st.report, "wire_damaged")
        return true
    if kind == 31:
        return true // escape is the one intentionally opaque wire kind
    if kind == 32:
        return r.off == r.hi or read_refuse(c.st.report, "wire_damaged")
    if kind == 13:
        return wire_body(c, r, false)
    if kind == 14 or kind == 16:
        if not has(r, 2):
            return read_refuse(c.st.report, "wire_damaged")
        let ek = u8(r)
        let count, ok = leb(r)
        if not ok:
            return read_refuse(c.st.report, "wire_damaged")
        if count < 0 or count > 0x7FFFFFFF:
            return read_refuse(c.st.report, "count_over_extent_cap")
        // Every encoded element consumes at least one byte. Refuse the
        // impossible count before allocating a declaration-sized container.
        if count > r.hi - r.off:
            return read_refuse(c.st.report, "wire_damaged")
        let keys:[int] = []
        var i = 0
        while i < count:
            if kind == 16:
                let key, good = leb(r)
                if not good or key <= 0 or key > length(c.st.ids):
                    return read_refuse(c.st.report, "wire_damaged")
                for(keys) seen:
                    if seen == key:
                        return read_refuse(c.st.report, "wire_damaged")
                push(keys, key)
                let n, framed = leb(r)
                if not framed or not has(r, n):
                    return read_refuse(c.st.report, "wire_damaged")
                if not wire_content(c, sub(r, n), ek):
                    return false
                r.off += n
            elif not wire_payload(c, r, ek):
                return false
            i++
        return r.off == r.hi or read_refuse(c.st.report, "wire_damaged")
    if not wire_payload(c, r, kind):
        return false
    return r.off == r.hi or read_refuse(c.st.report, "wire_damaged")

def wire_body(c:WireCheck, r:Reader, root:bool) -> bool:
    let seen:[int] = []
    while true:
        let ref, ok = leb(r)
        if not ok:
            return read_refuse(c.st.report, "wire_damaged")
        if ref == 0:
            return r.off == r.hi or read_refuse(c.st.report, "wire_damaged")
        let id, named = rid(r, ref)
        if not named or not has(r, 1):
            return read_refuse(c.st.report, "wire_damaged")
        for(seen) old:
            if old == id:
                return read_refuse(c.st.report, "wire_damaged")
        push(seen, id)
        let kind = u8(r)
        if id == build_version_id or id == message_vocabulary_id:
            return read_refuse(c.st.report, "wire_damaged")
        if id == node_table_id:
            if not root or kind != 12:
                return read_refuse(c.st.report, "wire_damaged")
            let n, framed = leb(r)
            if not framed or not has(r, n):
                return read_refuse(c.st.report, "wire_damaged")
            c.node_lo = r.off
            c.node_hi = r.off + n
            r.off += n
        elif not wire_payload(c, r, kind):
            return false
    return false

def wire_graph(c:WireCheck, children:[[int]], blobs:[bool]) -> bool:
    let n = length(children)
    let names:[int] = []
    let marks:[int] = []
    for(n):
        push(names, 0)
        push(marks, 0)
    for(children) edges:
        for(edges) index:
            if index == 0:
                continue
            if index < 0 or index > n:
                return read_refuse(c.st.report, "wire_damaged")
            names[index - 1] = names[index - 1] + 1
    for(blobs) blob, i:
        if blob and names[i + 1] > 1:
            return read_refuse(c.st.report, "blob_shared")
    let stack:[int] = [0]
    let next:[int] = [0]
    marks[0] = 1
    while length(stack) > 0:
        let top = length(stack) - 1
        let node = stack[top]
        let edges = children[node]
        if next[top] == length(edges):
            marks[node] = 2
            pop(stack)
            pop(next)
            continue
        let index = edges[next[top]]
        next[top] = next[top] + 1
        if index == 0:
            continue
        let target = index - 1
        if marks[target] == 1:
            return read_refuse(c.st.report, "wire_damaged")
        if marks[target] == 0:
            marks[target] = 1
            push(stack, target)
            push(next, 0)
    for(marks) mark:
        if mark == 0:
            return read_refuse(c.st.report, "wire_damaged")
    return true

def wire_check(st:LoadState, hi:int) -> bool:
    let c = WireCheck { st: st }
    if not wire_body(c, reader(st, 1, hi), true):
        return false
    let children:[[int]] = [c.edges]
    let blobs:[bool] = []
    if c.node_lo >= 0:
        let records, ok = scan_node_records(st, c.node_lo, c.node_hi)
        if not ok:
            return read_refuse(st.report, "wire_damaged")
        for(records) node:
            c.edges = []
            let blob = node.tid == string_blob_id or node.tid == bytes_blob_id
            if blob:
                let n = node.hi - node.lo
                if n > 0xffffffff:
                    return read_refuse(st.report, "blob_over_size_cap")
                if node.tid == string_blob_id and not text_valid(st.data, node.lo, n):
                    return read_refuse(st.report, "wire_damaged")
            elif not wire_body(c, reader(st, node.lo, node.hi), false):
                return false
            push(blobs, blob)
            push(children, c.edges)
    return wire_graph(c, children, blobs)

// load_walk is the framing walk both reads share (Go's decodeWalk).
def load_walk(root:string, data:string, report:LoadReport) -> Record?:
    if length(data) < 1:
        report.malformed = true
        report.damaged = true
        report.stopped = true
        return nil
    let form = data[0]
    if form != 1:
        report.refused = true
        report.stopped = true
        report.reason = if form == 2: "message_form_as_file" else: "newer_form"
        return nil
    let d = table_of(root)
    if d.fixed:
        read_refuse(report, "unknown_form")
        return nil
    let wire, intact = wire_integrity_body(d, data)
    if not intact:
        report.malformed = true
        report.damaged = true
        report.stopped = true
        report.reason = "integrity_mismatch"
        return nil
    let rec = new_record(d)
    let ids, first, ok = trailer(wire)
    if not ok:
        report.malformed = true
        report.damaged = true
        report.stopped = true
        return rec
    let st = LoadState { data: wire, ids: ids, report: report, variable: false, good: false, root: rec, nodes: [], named: [], count_refused: false }
    if not wire_check(st, first):
        return nil
    let end, terminated = body_extent(st, first)
    if not terminated or end != first:
        report.malformed = true
        report.damaged = true
        report.stopped = true
        return rec
    if not rec.d.variable:
        if not body_at(reader(st, 1, first), rec, false, false):
            read_refuse(report, "wire_damaged")
        if not report.stopped and report.malformed:
            report.damaged = true
        if report.missing_required > 0:
            release(rec)
            return nil
        return rec
    decode_variable(st, rec, first)
    if st.count_refused:
        report.refused = true
        report.reason = "count_over_extent_cap"
        report.stopped = true
        return nil
    if report.missing_required > 0:
        release(rec)
        return nil
    return rec
