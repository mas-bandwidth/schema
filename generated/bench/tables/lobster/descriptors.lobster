// descriptors.lobster — the table surface's REFLECTION side
// (docs/SPEC-TABLES.md §8.2, §8.3; docs/PORT-LOBSTER.md, THE CARD MAP): the
// unit registry and type view, the doc and tag columns, the deprecated enum
// variant read, and the enum-keyed array's key variants. It joins the load
// and save runtimes through the shape registry, so neither is edited here.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// A ViewVariant is one enum variant, flags bit or union arm (§8.3).
class ViewVariant:
    value:int = 0
    payload_name:string = ""
    record:TableDescriptor? = nil
    field:FieldDescriptor? = nil
    name:string
    id:int
    deprecated:bool = false
    doc:string = ""
    tags:[string] = []

// A ViewVocabulary is one enum, flags or union declaration (§8.3).
class ViewVocabulary:
    // Payload base in the shared region/cook projection.
    payload_offset:int = 0
    max:int = 0
    storage_bits:int = 0
    name:string
    file:string
    variants:[ViewVariant]
    doc:string = ""
    tags:[string] = []

// A ViewType is one type or table declaration (§8.3).
class ViewType:
    name:string
    file:string
    table:bool
    d:TableDescriptor?
    doc:string = ""
    tags:[string] = []

// A ViewConstant is one const declaration (§8.3).
class ViewConstant:
    name:string
    file:string
    type_name:string
    is_float:bool
    int_value:int
    float_value:float
    doc:string = ""
    tags:[string] = []

// A UnitView is everything one build declared (§8.3).
class UnitView:
    package:string
    protocol_id:int
    types:[ViewType]
    tables:[ViewType]
    enums:[ViewVocabulary]
    flags:[ViewVocabulary]
    unions:[ViewVocabulary]
    constants:[ViewConstant]

let view_types:[ViewType] = []
let view_tables:[ViewType] = []
let view_enums:[ViewVocabulary] = []
let view_flags:[ViewVocabulary] = []
let view_unions:[ViewVocabulary] = []
let view_constants:[ViewConstant] = []
var view_package = ""
var view_protocol = 0

def set_unit(package:string, protocol_id:int):
    view_package = package
    view_protocol = protocol_id

def view_table(d:TableDescriptor, file:string, doc:string, tags:[string]):
    push(view_tables, ViewType { name: d.name, file: file, table: true, d: d, doc: doc, tags: tags })

def view_type(d:TableDescriptor, file:string, doc:string, tags:[string]):
    push(view_types, ViewType { name: d.name, file: file, table: false, d: d, doc: doc, tags: tags })

def view_vocabulary(name:string, file:string, doc:string, tags:[string], variants:[ViewVariant], kind:string, max:int, storage_bits:int, payload_offset:int):
    let v = ViewVocabulary { payload_offset: payload_offset, max: max, storage_bits: storage_bits, name: name, file: file, variants: variants, doc: doc, tags: tags }
    if kind == "enum":
        push(view_enums, v)
    elif kind == "flags":
        push(view_flags, v)
    else:
        push(view_unions, v)

def view_constant(name:string, file:string, type_name:string, is_float:bool, int_value:int, float_value:float, doc:string, tags:[string]):
    push(view_constants, ViewConstant { name: name, file: file, type_name: type_name, is_float: is_float, int_value: int_value, float_value: float_value, doc: doc, tags: tags })

// UnitView sets are ordered by declaration name (SPEC-TABLES §8.3).
def view_name_before(a:string, b:string) -> bool:
    for(min(length(a), length(b))) i:
        if a[i] != b[i]:
            return a[i] < b[i]
    return length(a) < length(b)

def view_sorted_types(items:[ViewType]) -> [ViewType]:
    let result:[ViewType] = []
    for(items) item:
        var at = length(result)
        while at > 0 and view_name_before(item.name, result[at - 1].name):
            at--
        insert(result, at, item)
    return result

def view_sorted_vocabulary(items:[ViewVocabulary]) -> [ViewVocabulary]:
    let result:[ViewVocabulary] = []
    for(items) item:
        var at = length(result)
        while at > 0 and view_name_before(item.name, result[at - 1].name):
            at--
        insert(result, at, item)
    return result

def view_sorted_constants(items:[ViewConstant]) -> [ViewConstant]:
    let result:[ViewConstant] = []
    for(items) item:
        var at = length(result)
        while at > 0 and view_name_before(item.name, result[at - 1].name):
            at--
        insert(result, at, item)
    return result

def unit_view() -> UnitView:
    return UnitView { package: view_package, protocol_id: view_protocol, types: view_sorted_types(view_types), tables: view_sorted_types(view_tables), enums: view_sorted_vocabulary(view_enums), flags: view_sorted_vocabulary(view_flags), unions: view_sorted_vocabulary(view_unions), constants: view_sorted_constants(view_constants) }

// THE ANNOTATION COLUMNS (§8.1): a field's doc and tags live beside its
// descriptor, keyed by its owner and name, so no second descriptor class is
// needed for the two columns that never reach a wire byte.
class FieldAnnotation:
    owner:string
    field:string
    doc:string
    tags:[string]

let field_annotations:[FieldAnnotation] = []

def field_annotation(owner:string, field:string, doc:string, tags:[string]):
    push(field_annotations, FieldAnnotation { owner: owner, field: field, doc: doc, tags: tags })

def field_doc(owner:string, field:string) -> string:
    for(field_annotations) a:
        if a.owner == owner and a.field == field:
            return a.doc
    return ""

def field_tags(owner:string, field:string) -> [string]:
    for(field_annotations) a:
        if a.owner == owner and a.field == field:
            return a.tags
    return []

// A MAP FIELD'S ENTRY TABLE (§2.8, §8.1): the descriptor names the generated
// { key, value } table, and the registry makes that name a reflection fact.
class MapEntryName:
    owner:string
    field:string
    entry:string

let map_entries:[MapEntryName] = []

def map_entry(owner:string, field:string, entry:string):
    push(map_entries, MapEntryName { owner: owner, field: field, entry: entry })

def map_entry_of(owner:string, field:string) -> string:
    for(map_entries) m:
        if m.owner == owner and m.field == field:
            return m.entry
    return ""

// AN ENUM-KEYED ARRAY'S KEY ENUM (§2.4, §3.2): the field's key vocabulary,
// named beside the variants keyed_keys carries.
class KeyEnumName:
    owner:string
    field:string
    enum_name:string

let key_enums:[KeyEnumName] = []

def keyed_enum(owner:string, field:string, enum_name:string):
    push(key_enums, KeyEnumName { owner: owner, field: field, enum_name: enum_name })

def keyed_enum_of(owner:string, field:string) -> string:
    for(key_enums) k:
        if k.owner == owner and k.field == field:
            return k.enum_name
    return ""

// keyed_slot is the slot an enum-keyed array keeps key in (docs/PORTING.md
// M5): key 1 is slot 0 and max the last. None (key 0), a negative key and any
// key past max are REFUSED BY NAME in every build: one unsigned compare of
// key - 1 against max covers both ends, and the refusal is a VM error that
// names the array and the key. It is an index past a one-element vector
// rather than an assert, because --runtime-no-asserts compiles asserts away
// and keeps the VM's bounds check.
def keyed_slot(key:int, max:int, what:string) -> int:
    if not ult(key - 1, max):
        return length(["keyed key refused: " + what + " key " + string(key)][1])
    return key - 1

// THE DEPRECATED ENUM VARIANT (§2.15): the variant keeps its id, but a read
// yields the field's declared default (an element's zero). The marker is
// registered from the descriptors, and this file replaces the enum shape.
class DeprecatedName:
    enum_name:string
    variant:string

let deprecated_names:[DeprecatedName] = []

def deprecated_variant(enum_name:string, variant:string):
    push(deprecated_names, DeprecatedName { enum_name: enum_name, variant: variant })

def is_deprecated_variant(enum_name:string, variant:string) -> bool:
    for(deprecated_names) d:
        if d.enum_name == enum_name and d.variant == variant:
            return true
    return false

def variant_name(fd:FieldDescriptor, value:int) -> string:
    if value >= 1 and value <= length(fd.vs):
        return fd.vs[value - 1].name
    return ""

def enum_default(fd:FieldDescriptor) -> int:
    if fd.has_def:
        return fd.def_int.lo
    return 0

class DeprecatedEnumShape : Shape
    def field(r:Reader, sl:Slot) -> bool:
        let ok = enum_cell(r, sl.cell, sl.fd)
        if ok and sl.cell.i > 0:
            if is_deprecated_variant(sl.fd.t, variant_name(sl.fd, sl.cell.i)):
                sl.cell.i = enum_default(sl.fd)
        return ok
    def element(r:Reader, sl:Slot, i:int, ek:int) -> bool:
        let e = elem_of(sl.fd)
        let ok = enum_cell(r, sl.elems[i], e)
        if ok and sl.elems[i].i > 0:
            if is_deprecated_variant(e.t, variant_name(e, sl.elems[i].i)):
                sl.elems[i].i = 0
        return ok
    def arm(r:Reader, fv:Slot, n:int) -> bool:
        let ok = enum_cell(r, fv.cell, fv.fd)
        if ok and fv.cell.i > 0:
            if is_deprecated_variant(fv.fd.t, variant_name(fv.fd, fv.cell.i)):
                fv.cell.i = enum_default(fv.fd)
        if ok and r.off != r.lo + n:
            r.st.report.malformed = true
            return false
        return ok

let _descriptors_enum = register_shape(DeprecatedEnumShape { key: "enum" })

// Union payload metadata shares the descriptor objects used by the runtime.
def view_union_arm(union_name:string, tag:int, payload_name:string, record:TableDescriptor?, general:bool):
    for(view_unions) vocabulary:
        if vocabulary.name == union_name:
            let variant = vocabulary.variants[tag]
            variant.payload_name = payload_name
            variant.record = record
            if general:
                let descriptor = lookup_union(union_name)
                if descriptor:
                    variant.field = descriptor.arms[tag - 1]
