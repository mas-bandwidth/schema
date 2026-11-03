// defaults_renames.lobster — the Lobster table runtime's DEPRECATION rule
// (docs/SPEC-TABLES.md §2.15; docs/PORT-LOBSTER.md, THE CARD MAP). The
// descriptor marks a variant retired in place only through the registration
// this unit emits beside BuildVersion; this file records it and installs the
// enum shape that reads a retired variant as its slot's default. It joins
// the load's dispatch table with one register_shape and never edits
// load.lobster.
//
// THE STRICT WRITE (§2.15): a variant retired in place reads as the field
// default and IS REFUSED ON WRITE. The save walker has no per-kind hook this
// file can join, so the rule is a walk of the record the writer is about to
// encode: save_strict names the write that refuses a retired variant in any
// place the writer would emit it — a scalar field, an array element, a union
// arm, a nested body, a map entry or a pointer referent — and answers "" as
// the writer's own cycle refusal does (§3). save itself stays the plain
// writer, exactly as load stays the tolerant read beside load_strict (§6.5).
//
// Renames need no runtime: a field's, variant's, arm's or table's id is the
// hash of its old name (`was`, §5), so the descriptor's id already matches a
// wire written under the old spelling. Defaults need no runtime either: the
// descriptor carries def_bool, def_int, def_float and def_str and the load's
// new_record lays them in (§4).
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_save

namespace schema_rt

// A RetiredVariant is one enum variant marked `| deprecated` (§2.15): the
// enum's name and the variant's wire id, which stay for life.
class RetiredVariant:
    en:string
    id:int

let retired_variants:[RetiredVariant] = []

// deprecate_variant records one variant retired in place. The generated
// module calls it once per retired variant of the unit, before any load.
def deprecate_variant(en:string, id:int):
    for(retired_variants) r:
        if r.en == en and r.id == id:
            return
    push(retired_variants, RetiredVariant { en: en, id: id })

// variant_retired reports whether the variant id names one the enum retired.
def variant_retired(en:string, id:int) -> bool:
    for(retired_variants) r:
        if r.en == en and r.id == id:
            return true
    return false

// dep_enum_cell reads an enum value (§3, kind 30) as enum_cell does — 0 is
// None, a variant this enum cannot name reads None and counts unknown, a
// reference past the id table is damage — except that a retired variant
// reads its slot's default and counts NOTHING unknown (§2.15): the FIELD's
// declared default for a field or an arm, the ELEMENT's own zero for an
// array element.
def dep_enum_cell(r:Reader, cell:Value, fd:FieldDescriptor, element:bool) -> bool:
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
    if variant_retired(fd.t, id):
        if element:
            cell.i = 0
        else:
            cell.i = fd.def_int.lo
        return true
    let v = enum_value(fd.vs, id)
    if v < 0:
        cell.i = 0
        rep.unknown++
        return true
    cell.i = v
    return true

// the enum shape, installed LAST so it is the one a load dispatches on
// (docs/PORT-LOBSTER.md, THE LOAD)
class DefaultsEnumShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        return dep_enum_cell(r, sl.cell, sl.fd, false)
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        return dep_enum_cell(r, sl.elems[i], elem_of(sl.fd), true)
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let ok = dep_enum_cell(r, fv.cell, fv.fd, false)
        if ok and r.off != r.lo + n:
            r.st.report.malformed = true
            return false
        return ok

let _defaults_enum = register_shape(DefaultsEnumShape { key: "enum" })

// retired_value is one enum ordinal that names a retired variant (§2.15):
// the value the writer would emit under that variant's id.
def retired_value(f:FieldDescriptor, value:int) -> bool:
    if value < 1 or value > length(f.vs):
        return false
    return variant_retired(f.t, f.vs[value - 1].id)

// save_strict is THE STRICT WRITE (§2.15): the plain save after one walk of
// the record proves no RETIRED VARIANT is written. A field at its declared
// default is elided before its variant is looked at, exactly as the writer
// elides it (§3); an array element, an arm and a nested body are always
// written once the collection or arm itself rides, so each is inspected
// there. A retired variant anywhere answers "", the writer's refusal.
def save_strict(rec:Record) -> string:
    let seen:[Record] = []
    if refused_record(rec, seen):
        return ""
    return save(rec)

def refused_record(rec:Record, seen:[Record]) -> bool:
    for(seen) r:
        if r == rec:
            return false
    push(seen, rec)
    for(rec.fields) sl:
        if refused_slot(sl, seen):
            return true
    return false

def refused_slot(sl:Slot, seen:[Record]) -> bool:
    let f = sl.fd
    if f.present != "" and not sl.present:
        return false
    if f.s == "enum":
        if save_is_default(f, sl.cell):
            return false
        return retired_value(f, sl.cell.i)
    if f.s == "arr":
        return refused_array(sl, seen)
    if f.s == "union":
        return refused_union(sl.cell, f, seen)
    if f.s == "tab":
        let t = sl.cell.tab
        if t:
            return refused_record(t, seen)
        return false
    if f.s == "ptr":
        let n = sl.cell.node
        if n:
            return refused_record(n, seen)
        return false
    if f.s == "map":
        for(sl.entries) v:
            let t = v.tab
            if t:
                if refused_record(t, seen):
                    return true
        return false
    return false

def refused_array(sl:Slot, seen:[Record]) -> bool:
    let f = sl.fd
    let e = elem_of(f)
    let n = if (f.array == "counted" or f.array == "list"): sl.count else: f.bound
    if f.array == "keyed":
        for(sl.elems) v:
            if not save_elem_is_default(e, f.ek, v) and refused_elem(e, f.ek, v, seen):
                return true
        return false
    if f.array == "fixed" and n > 0:
        var all = true
        var i = 0
        while i < n and i < length(sl.elems):
            if not save_elem_is_default(e, f.ek, sl.elems[i]):
                all = false
            i++
        if all:
            return false
    var i = 0
    while i < n and i < length(sl.elems):
        if refused_elem(e, f.ek, sl.elems[i], seen):
            return true
        i++
    return false

def refused_elem(e:FieldDescriptor, ek:int, v:Value, seen:[Record]) -> bool:
    if e.s == "enum":
        return retired_value(e, v.i)
    if e.s == "tab":
        let t = v.tab
        if t:
            return refused_record(t, seen)
        return false
    if e.s == "ptr":
        let n = v.node
        if n:
            return refused_record(n, seen)
        return false
    if e.s == "union":
        return refused_union(v, e, seen)
    return false

def refused_union(cell:Value, f:FieldDescriptor, seen:[Record]) -> bool:
    if cell.i == 0:
        return false
    let un = lookup_union(f.t)
    if not un or cell.i < 1 or cell.i > length(un.arms):
        return false
    let arm = un.arms[cell.i - 1]
    if arm.s == "enum":
        let sl = cell.arm
        if sl:
            return retired_value(arm, sl.cell.i)
        return false
    if arm.s == "tab":
        let t = cell.tab
        if t:
            return refused_record(t, seen)
        return false
    if arm.s == "ptr":
        let sl = cell.arm
        if not sl:
            return false
        let n = sl.cell.node
        if n:
            return refused_record(n, seen)
        return false
    if arm.s == "arr":
        let sl = cell.arm
        if not sl:
            return false
        let n = if (arm.array == "counted" or arm.array == "list"): sl.count else: arm.bound
        var i = 0
        while i < n and i < length(sl.elems):
            if refused_elem(elem_of(arm), arm.ek, sl.elems[i], seen):
                return true
            i++
        return false
    if arm.s == "union":
        let sl = cell.arm
        if sl:
            return refused_union(sl.cell, arm, seen)
        return false
    return false
