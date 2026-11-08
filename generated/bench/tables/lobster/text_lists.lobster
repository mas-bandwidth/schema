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
// `Table.field` a total's diagnostic names, and whether the element (or the map's
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

// A total is a bound on the complete decoded list; no prefix is retained.
def total_keep(r:Reader, sl:Slot, fact:TextListFact) -> bool:
    if fact.total <= 0:
        return true
    var spent = 0
    for(sl.count) i:
        let rec = sl.elems[i].tab
        if rec:
            let n = text_value_len(rec)
            if n > fact.total - spent:
                return read_refuse(r.st.report, "length_over_bound")
            spent += n
    return true

// Check the total after the complete array has decoded.
class TextTotalShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let ok = array_body(r, sl, -1)
        return ok and total_keep(r, sl, text_list_of(sl.fd))
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let ok, selected = array_body(r, fv, n)
        return ok and selected and total_keep(r, fv, text_list_of(fv.fd))

let _text_total = register_shape(TextTotalShape { key: "arr" })

// A PLAIN TEXT LIST's element (§2.13, §2.13.1): its own L and well-formed
// text. A bounded element must fit M; an unbounded element has no length
// bound.
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
        let keep = n
        if vd.s != "text" and keep > vd.bound:
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
            return false
        let keep = n
        if sl.fd.s != "text" and keep > sl.fd.bound:
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
        if fv.fd.s != "text" and keep > fv.fd.bound:
            return read_refuse(rep, "length_over_bound")
        fv.cell.s = substring(r.b, r.off, keep)
        fv.count = keep
        r.off += n
        return true

let _text_string = register_shape(TextStringShape { key: "str" })

// MapShape uses the field descriptor's text/str distinction, so bounded
// and unbounded keys share the same one-read implementation.
class TextMapShape : MapShape

let _text_map = register_shape(TextMapShape { key: "map" })
