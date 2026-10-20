// defaults_renames.lobster — the Lobster table runtime's DEPRECATION rule
// (docs/SPEC-TABLES.md §2.15; docs/PORT-LOBSTER.md, THE CARD MAP). The
// descriptor marks a variant retired in place only through the registration
// this unit emits beside BuildVersion; this file records it and installs the
// enum shape that reads a retired variant as its slot's default. It joins
// the load's dispatch table with one register_shape and never edits
// load.lobster.
//
// Renames need no runtime: a field's, variant's, arm's or table's id is the
// hash of its old name (`was`, §5), so the descriptor's id already matches a
// wire written under the old spelling. Defaults need no runtime either: the
// descriptor carries def_bool, def_int, def_float and def_str and the load's
// new_record lays them in (§4).
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load

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
