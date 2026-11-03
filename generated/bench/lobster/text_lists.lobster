// text_lists.lobster — the Lobster table runtime's TEXT LISTS, their TOTAL
// and UNBOUNDED TEXT (docs/SPEC-TABLES.md §2.13, §2.13.1;
// docs/PORT-LOBSTER.md, THE LOAD). It adds the facts the descriptor emitter's
// part registers and the shapes that read a text list's elements without a
// declared element bound, hold a list's `| total = N`, and compare an
// unbounded map key at any length. It replaces shapes by key through the same
// register_shape hook load.lobster fixed; it never edits load.lobster or
// schema_save.lobster.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// A TextListFact is the descriptor-side fact a text list or an unbounded map
// key needs beyond the FieldDescriptor classes: the total (§2.13), the
// `Table.field` a total's clamp names, and whether the element (or the map's
// key) is unbounded text (§2.13.1). The emitter's text_lists.go part
// registers one per field after the descriptors.
class TextListFact:
    fd:FieldDescriptor
    total:int = 0
    total_of:string = ""
    unbounded_value:bool = false
    unbounded_key:bool = false

let text_list_facts:[TextListFact] = []

def text_list(fd:FieldDescriptor, total:int, total_of:string, unbounded_value:bool, unbounded_key:bool) -> TextListFact:
    let f = TextListFact { fd: fd, total: total, total_of: total_of, unbounded_value: unbounded_value, unbounded_key: unbounded_key }
    push(text_list_facts, f)
    return f

def text_list_of(fd:FieldDescriptor) -> TextListFact:
    for(text_list_facts) f:
        if f.fd == fd:
            return f
    return TextListFact { fd: fd }

// text_value_len is one element record's text length; an absent ?string
// element counts zero (docs/SPEC-TABLES.md §2.13).
def text_value_len(rec:Record) -> int:
    let v = rec.fields[0]
    if v.fd.present != "" and not v.present:
        return 0
    return length(v.cell.s)

// total_keep holds a text list's `| total = N` ON THE READ SIDE: the longest
// PREFIX whose byte sum is within the total is kept, the first element past it
// and every element after it are dropped, `clamped` is counted ONCE and the
// field is named (docs/SPEC-TABLES.md §2.13).
def total_keep(r:Reader, sl:Slot, fact:TextListFact):
    if fact.total <= 0:
        return
    let rep = r.st.report
    var spent = 0
    var kept = 0
    while kept < sl.count:
        let rec = sl.elems[kept].tab
        var n = 0
        if rec:
            n = text_value_len(rec)
        if spent + n > fact.total:
            break
        spent += n
        kept++
    if kept >= sl.count:
        return
    rep.clamped++
    if rep.reason == "":
        rep.reason = "clamped: " + fact.total_of
    if sl.fd.array == "list":
        truncate(sl.elems, kept)
    else:
        var j = kept
        while j < sl.count:
            sl.elems[j] = element_zero(sl.fd)
            j++
    sl.count = kept

// THE TOTAL'S DISPATCH: the "arr" shape with the prefix rule after the
// ordinary array body has read and count-clamped the list.
class TextTotalShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let ok = array_body(r, sl, -1)
        if ok:
            total_keep(r, sl, text_list_of(sl.fd))
        return ok
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let ok, selected = array_body(r, fv, n)
        return ok and selected

let _text_total = register_shape(TextTotalShape { key: "arr" })

// A PLAIN TEXT LIST's element (§2.13, §2.13.1): its own L and well-formed
// text. A bounded element clamps at M on a code point boundary and counts; an
// unbounded element (§2.13.1) is never clamped for its length.
class TextListElementShape : Shape
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
        let vd = ed.fields[0]
        var keep = n
        if vd.s != "text" and keep > vd.bound:
            keep = text_boundary(r.b, r.off, n, vd.bound)
            rep.clamped++
        let held = sl.elems[i].tab
        var rec = new_record(ed)
        if held:
            rec = held
        sl.elems[i].tab = rec
        rec.fields[0].cell.s = substring(r.b, r.off, keep)
        rec.fields[0].count = keep
        r.off += n
        return true

let _text_list = register_shape(TextListElementShape { key: "textlist" })

// THE STRING, with unbounded text's one difference: an `s: "text"` value has
// no element bound to clamp against (§2.13.1); every `string(N)` reads as
// StringShape reads it. This is the shape an optional text-list element's
// body reaches, and a bare unbounded field's value.
class TextStringShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let rep = r.st.report
        let n, ok = leb(r)
        if not ok or not has(r, n):
            rep.malformed = true
            return false
        if not text_valid(r.b, r.off, n):
            rep.malformed = true
            sl.cell = field_default(sl.fd)
            sl.count = length(sl.cell.s)
            r.off += n
            return true
        var keep = n
        if sl.fd.s != "text" and keep > sl.fd.bound:
            keep = text_boundary(r.b, r.off, n, sl.fd.bound)
            rep.clamped++
        sl.cell.s = substring(r.b, r.off, keep)
        sl.count = keep
        r.off += n
        return true
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let rep = r.st.report
        if not text_valid(r.b, r.off, n):
            rep.malformed = true
            return false
        var keep = n
        if fv.fd.s != "text" and keep > fv.fd.bound:
            keep = text_boundary(r.b, r.off, n, fv.fd.bound)
            rep.clamped++
        fv.cell.s = substring(r.b, r.off, keep)
        fv.count = keep
        r.off += n
        return true

let _text_string = register_shape(TextStringShape { key: "str" })

// THE MAP with an UNBOUNDED KEY (§2.13.1): the map's own framing, order,
// duplicate and kind rules, and a key of any length instead of one clamped at
// a string(N) bound. A bounded key keeps MapShape's rule exactly.
class TextMapShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let rep = r.st.report
        let n, ok = leb(r)
        if not ok or not has(r, n):
            rep.malformed = true
            return false
        let end = r.off + n
        if n >= 2:
            let ek = u8(r)
            let count, good = leb(r)
            if not good:
                rep.malformed = true
                r.off = end
                return true
            if ek != 13:
                rep.kind_mismatch++
                r.off = end
                return true
            sl.entries = []
            if count < 0 or count > 0x7FFFFFFF:
                r.off = end
                return collection_cap(r)
            let sr = sub_to(r, end)
            let ed = table_of(sl.fd.t)
            let k = ed.fields[0]
            let unbounded = text_list_of(sl.fd).unbounded_key
            var last = Value {}
            var landed = false
            var widened = false
            var i = 0
            while i < count:
                i++
                let elem_len, lgood = leb(sr)
                if not lgood or not has(sr, elem_len):
                    rep.malformed = true
                    break
                let body = sub(sr, elem_len)
                sr.off += elem_len
                let entry = new_record(ed)
                let mkey, key_ok, kind_bad, entry_widened = map_key(body, k, entry)
                if entry_widened and not widened:
                    widened = true
                    rep.widened++
                if kind_bad:
                    rep.kind_mismatch++
                    sl.entries = []
                    break
                if not key_ok:
                    rep.malformed = true
                    break
                if not unbounded and not map_key_fits(k, mkey):
                    rep.clamped++
                    continue
                var order = -1
                if landed:
                    order = map_key_order(k, last, mkey)
                if order > 0:
                    rep.malformed = true
                    break
                let whole = Reader { b: body.b, lo: body.lo, off: body.lo, hi: body.hi, st: body.st }
                let decoded = new_record(ed)
                body_at(whole, decoded, true, true)
                if r.st.count_refused:
                    return false
                if order == 0:
                    sl.entries[length(sl.entries) - 1] = Value { tab: decoded }
                    rep.duplicate++
                    continue
                push(sl.entries, Value { tab: decoded })
                last = decoded.fields[0].cell
                landed = true
        r.off = end
        return true

let _text_map = register_shape(TextMapShape { key: "map" })
