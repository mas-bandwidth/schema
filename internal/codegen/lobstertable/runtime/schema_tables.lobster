// schema_tables.lobster — the Lobster table surface's runtime
// (docs/PORT-LOBSTER.md, THE RUNTIME LAYOUT). Emitted beside the generated
// <Base>Table.lobster modules and imported as `import .schema_tables`. It
// holds the DESCRIPTOR classes every table module's data is built from and
// the registry a walk resolves a descriptor's name through: the table codecs
// (load.lobster, save.lobster, fixed.lobster, message.lobster, json.lobster,
// text.lobster) are later runtime files beside this one, each walking these
// descriptors as data (docs/SPEC-TABLES.md §8.1; docs/PORTING.md M13).
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .schema_runtime

namespace schema_rt

// An EnumVariant is one variant of an enum a field names: identity on the
// table wire is the variant NAME's hash (docs/SPEC-TABLES.md §5).
class EnumVariant:
    name:string
    id:int

// A FieldDescriptor is one field, one union arm or one array element, as data
// (docs/SPEC-TABLES.md §3, §8.1). Every field carries its wire id (the FNV-1a
// 64 hash of its name or of its was alias, §5), its name, its Lobster slot,
// its text-form key (§16.3), its ordinal o and its header kind k; s is its
// SHAPE and the rest is what that shape needs (the list is in
// lobstertable.go's fieldDesc). An element descriptor carries the shape facts
// alone. Every member has its zero as a default, so a descriptor names only
// the facts its shape has.
class FieldDescriptor:
    id:int = 0
    name:string = ""
    slot:string = ""
    json:string = ""
    o:int = 0
    k:int = 0
    s:string = ""
    tag:int = 0
    present:string = ""
    required:bool = false
    deprecated:bool = false
    when:[string] = []
    // a list's or map's element size and alignment in the C ABI region
    lsize:int = 0
    lalign:int = 1
    t:string = ""
    ek:int = 0
    e:FieldDescriptor? = nil
    array:string = ""
    bound:int = 0
    min:int = 0
    wide:bool = false
    f32:bool = false
    franged:bool = false
    flo:float = 0.0
    fhi:float = 0.0
    res:float = 0.0
    vs:[EnumVariant] = []
    kw:int = 0
    signed:bool = false
    dom:string = ""
    ranged:bool = false
    lo:Int128 = Int128 { 0, 0 }
    hi:Int128 = Int128 { 0, 0 }
    bits:int = 0
    ibits:int = 0
    fbits:int = 0
    timestamp:bool = false
    has_def:bool = false
    def_bool:bool = false
    def_int:Int128 = Int128 { 0, 0 }
    def_float:float = 0.0
    def_str:string = ""

// A TableDescriptor is one table, or one packet type a table reaches by value:
// its node type id (§3.1, the hash of its wire name), its declared class
// (§2.2), what a pointer walk from it may place (§3.1) and its fields in
// declared order.
class TableDescriptor:
    name:string
    id:int
    table:bool
    fixed:bool
    variable:bool
    place_tables:[string]
    place_str:bool
    place_bytes:bool
    // the C ABI record size and the unit's region alignment (§6.5), which
    // load_measure reads
    rsize:int = 0
    ralign:int = 8
    fields:[FieldDescriptor]

// A UnionDescriptor is one union: its arms in tag order, each a field line
// under the arm's id (§2.6).
class UnionDescriptor:
    name:string
    arms:[FieldDescriptor]

let table_registry:[TableDescriptor] = []
let union_registry:[UnionDescriptor] = []

// table registers a table descriptor under its name and returns it; two
// descriptors of one name are a generation defect, refused at load.
def table(d:TableDescriptor) -> TableDescriptor:
    assert not lookup_table(d.name) and not lookup_union(d.name)
    push(table_registry, d)
    return d

// union registers a union descriptor under its name and returns it.
def union_of(d:UnionDescriptor) -> UnionDescriptor:
    assert not lookup_table(d.name) and not lookup_union(d.name)
    push(union_registry, d)
    return d

// lookup_table is the table descriptor a field's t names, or nil: every
// module whose descriptor names another imports the module that registers
// it, so a loaded root's closure is registered.
def lookup_table(name:string) -> TableDescriptor?:
    for(table_registry) d:
        if d.name == name:
            return d
    return nil

// lookup_union is the union descriptor a field's t names, or nil.
def lookup_union(name:string) -> UnionDescriptor?:
    for(union_registry) d:
        if d.name == name:
            return d
    return nil

// registered_tables is the descriptors registered so far.
def registered_tables() -> [TableDescriptor]:
    return table_registry

// registered_unions is the union descriptors registered so far.
def registered_unions() -> [UnionDescriptor]:
    return union_registry
