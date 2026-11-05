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
    integrity:bool = false
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

// CRC-32C covers the complete file before its root-only trailer (§2.14).
// Keep it independent of the record walk so a bad checksum decodes nothing.
let wire_crc32c_table = [
    0x00000000, 0xf26b8303, 0xe13b70f7, 0x1350f3f4, 0xc79a971f, 0x35f1141c, 0x26a1e7e8, 0xd4ca64eb,
    0x8ad958cf, 0x78b2dbcc, 0x6be22838, 0x9989ab3b, 0x4d43cfd0, 0xbf284cd3, 0xac78bf27, 0x5e133c24,
    0x105ec76f, 0xe235446c, 0xf165b798, 0x030e349b, 0xd7c45070, 0x25afd373, 0x36ff2087, 0xc494a384,
    0x9a879fa0, 0x68ec1ca3, 0x7bbcef57, 0x89d76c54, 0x5d1d08bf, 0xaf768bbc, 0xbc267848, 0x4e4dfb4b,
    0x20bd8ede, 0xd2d60ddd, 0xc186fe29, 0x33ed7d2a, 0xe72719c1, 0x154c9ac2, 0x061c6936, 0xf477ea35,
    0xaa64d611, 0x580f5512, 0x4b5fa6e6, 0xb93425e5, 0x6dfe410e, 0x9f95c20d, 0x8cc531f9, 0x7eaeb2fa,
    0x30e349b1, 0xc288cab2, 0xd1d83946, 0x23b3ba45, 0xf779deae, 0x05125dad, 0x1642ae59, 0xe4292d5a,
    0xba3a117e, 0x4851927d, 0x5b016189, 0xa96ae28a, 0x7da08661, 0x8fcb0562, 0x9c9bf696, 0x6ef07595,
    0x417b1dbc, 0xb3109ebf, 0xa0406d4b, 0x522bee48, 0x86e18aa3, 0x748a09a0, 0x67dafa54, 0x95b17957,
    0xcba24573, 0x39c9c670, 0x2a993584, 0xd8f2b687, 0x0c38d26c, 0xfe53516f, 0xed03a29b, 0x1f682198,
    0x5125dad3, 0xa34e59d0, 0xb01eaa24, 0x42752927, 0x96bf4dcc, 0x64d4cecf, 0x77843d3b, 0x85efbe38,
    0xdbfc821c, 0x2997011f, 0x3ac7f2eb, 0xc8ac71e8, 0x1c661503, 0xee0d9600, 0xfd5d65f4, 0x0f36e6f7,
    0x61c69362, 0x93ad1061, 0x80fde395, 0x72966096, 0xa65c047d, 0x5437877e, 0x4767748a, 0xb50cf789,
    0xeb1fcbad, 0x197448ae, 0x0a24bb5a, 0xf84f3859, 0x2c855cb2, 0xdeeedfb1, 0xcdbe2c45, 0x3fd5af46,
    0x7198540d, 0x83f3d70e, 0x90a324fa, 0x62c8a7f9, 0xb602c312, 0x44694011, 0x5739b3e5, 0xa55230e6,
    0xfb410cc2, 0x092a8fc1, 0x1a7a7c35, 0xe811ff36, 0x3cdb9bdd, 0xceb018de, 0xdde0eb2a, 0x2f8b6829,
    0x82f63b78, 0x709db87b, 0x63cd4b8f, 0x91a6c88c, 0x456cac67, 0xb7072f64, 0xa457dc90, 0x563c5f93,
    0x082f63b7, 0xfa44e0b4, 0xe9141340, 0x1b7f9043, 0xcfb5f4a8, 0x3dde77ab, 0x2e8e845f, 0xdce5075c,
    0x92a8fc17, 0x60c37f14, 0x73938ce0, 0x81f80fe3, 0x55326b08, 0xa759e80b, 0xb4091bff, 0x466298fc,
    0x1871a4d8, 0xea1a27db, 0xf94ad42f, 0x0b21572c, 0xdfeb33c7, 0x2d80b0c4, 0x3ed04330, 0xccbbc033,
    0xa24bb5a6, 0x502036a5, 0x4370c551, 0xb11b4652, 0x65d122b9, 0x97baa1ba, 0x84ea524e, 0x7681d14d,
    0x2892ed69, 0xdaf96e6a, 0xc9a99d9e, 0x3bc21e9d, 0xef087a76, 0x1d63f975, 0x0e330a81, 0xfc588982,
    0xb21572c9, 0x407ef1ca, 0x532e023e, 0xa145813d, 0x758fe5d6, 0x87e466d5, 0x94b49521, 0x66df1622,
    0x38cc2a06, 0xcaa7a905, 0xd9f75af1, 0x2b9cd9f2, 0xff56bd19, 0x0d3d3e1a, 0x1e6dcdee, 0xec064eed,
    0xc38d26c4, 0x31e6a5c7, 0x22b65633, 0xd0ddd530, 0x0417b1db, 0xf67c32d8, 0xe52cc12c, 0x1747422f,
    0x49547e0b, 0xbb3ffd08, 0xa86f0efc, 0x5a048dff, 0x8ecee914, 0x7ca56a17, 0x6ff599e3, 0x9d9e1ae0,
    0xd3d3e1ab, 0x21b862a8, 0x32e8915c, 0xc083125f, 0x144976b4, 0xe622f5b7, 0xf5720643, 0x07198540,
    0x590ab964, 0xab613a67, 0xb831c993, 0x4a5a4a90, 0x9e902e7b, 0x6cfbad78, 0x7fab5e8c, 0x8dc0dd8f,
    0xe330a81a, 0x115b2b19, 0x020bd8ed, 0xf0605bee, 0x24aa3f05, 0xd6c1bc06, 0xc5914ff2, 0x37faccf1,
    0x69e9f0d5, 0x9b8273d6, 0x88d28022, 0x7ab90321, 0xae7367ca, 0x5c18e4c9, 0x4f48173d, 0xbd23943e,
    0xf36e6f75, 0x0105ec76, 0x12551f82, 0xe03e9c81, 0x34f4f86a, 0xc69f7b69, 0xd5cf889d, 0x27a40b9e,
    0x79b737ba, 0x8bdcb4b9, 0x988c474d, 0x6ae7c44e, 0xbe2da0a5, 0x4c4623a6, 0x5f16d052, 0xad7d5351,
]

def wire_crc32c(data:string, used:int = -1) -> int:
    let n = if used < 0: length(data) else: used
    var crc = 0xffffffff
    for(n) i:
        crc = shr(crc, 8) ^ wire_crc32c_table[(crc ^ read_uint8_le(data, i)) & 0xff]
    return (crc ^ 0xffffffff) & 0xffffffff

// Called after the form-byte check, before resolving ids or reading fields.
def wire_integrity_body(d:TableDescriptor, data:string, minimum:int = 5) -> string, bool:
    if not d.integrity:
        return data, true
    if length(data) < minimum:
        return "", false
    let n = length(data) - 4
    if read_uint32_le(data, n) != wire_crc32c(data, n):
        return "", false
    return substring(data, 0, n), true
