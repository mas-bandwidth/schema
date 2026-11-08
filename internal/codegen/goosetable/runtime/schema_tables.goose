// schema_tables.goose — the Goose table surface's descriptor types
// (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT): emitted beside the generated
// modules and imported as `import .schema_tables;`. Every generated
// <Name>Descriptor is a global constant of these types — the table AS DATA
// (docs/SPEC-TABLES.md §3, §3.1, §8.1) — which the one runtime walk of each
// later form reads. Later cards add one runtime file each beside this one.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;

// The shapes a field descriptor takes (docs/PORT-GOOSE.md, THE TYPE MAPPING).
let ShapeBool: u8 = 1;   // bool
let ShapeInt: u8 = 2;    // an integer at its kind's width; bits(N) and fixed too
let ShapeFloat: u8 = 3;  // float32, float64
let ShapeStr: u8 = 4;    // string(N), wstring(N)
let ShapeText: u8 = 5;   // an unbounded string element or map key
let ShapeBytes: u8 = 6;  // bytes(N)
let ShapeEnum: u8 = 7;   // kind 30, the variant name's id
let ShapePtr: u8 = 8;    // *T: kind 17, a node index
let ShapeSBlob: u8 = 9;  // *string
let ShapeBBlob: u8 = 10; // *bytes
let ShapeTab: u8 = 11;   // a table or packet type by value (kind 13)
let ShapeUnion: u8 = 12; // a union (kind 15)
let ShapeArr: u8 = 13;   // []T, [..N]T, [N]T, [E]T over an element descriptor
let ShapeMap: u8 = 14;   // map[K]V over its generated entry table
let ShapeVoid: u8 = 15;  // a payload-free union arm

// The array kinds of ShapeArr.
let ArrayFixed: u8 = 1;
let ArrayCounted: u8 = 2;
let ArrayList: u8 = 3;
let ArrayKeyed: u8 = 4;

// VariantDesc is one enum variant: its name and its table-wire id, the
// FNV-1a 64 of the variant's wire name (docs/SPEC-TABLES.md §5).
struct VariantDesc { name: const u8[:], id: u64 }

// FieldDesc is one field (or union arm, or array element) as data. Integer
// facts are Int128 lanes so one shape holds every domain; a bound or default
// a field lacks is its flag false.
struct FieldDesc {
    id: u64,                  // the field's wire id (docs/SPEC-TABLES.md §3.1)
    name: const u8[:],        // the schema name
    slot: const u8[:],        // the Goose field name (ir.GoExportName)
    json: const u8[:],        // the text key
    ordinal: i32,
    kind: u8,                 // the header kind (docs/SPEC-TABLES.md §3)
    shape: u8,
    ek: u8,                   // an array's element kind, a bytes field's
    array: u8,                // ArrayFixed ... ArrayKeyed for ShapeArr
    bound: i64,               // an array's, a string's or a bytes field's bound
    min: i64,                 // a counted array's least count
    elem: const FieldDesc?,   // an array's element descriptor
    ref: const u8[:],         // the table, type, union or enum named
    variants: const VariantDesc[:],
    kw: i32,                  // an integer kind's width in bytes
    signed: bool,
    ranged: bool,             // lo and hi hold the declared clamp range
    lo: Int128,
    hi: Int128,
    bits: i32,                // bits(N)
    ibits: i32,               // fixed(I, F)
    fbits: i32,
    is_f32: bool,
    frange: bool,             // flo, fhi, fres hold a compressed float's range
    flo: f64,
    fhi: f64,
    fres: f64,
    wide: bool,               // wstring
    optional: bool,           // ?T, its presence in <slot>Present
    required: bool,
    deprecated: bool,
    timestamp: bool,
    has_def: bool,            // a declared default, in the def_* of its shape
    def_i: Int128,
    def_f: f64,
    def_b: bool,
    def_s: const u8[:],
    tag: i64,                 // a union arm's tag
    type_name: const u8[:],   // the declared type's spelling (docs/SPEC-TABLES.md §8.1)
    arm_offset: i64,          // a union arm's offset in the union's storage at the
                              // record layout: the tag at 0, every arm at the one
                              // payload base (§7.2, §8.1); 0 on every other field
    doc: const u8[:],         // the `///` block above it, verbatim (SPEC §4.1);
                              // TableDocNone when there is none
    num_tags: i32,            // its tags (SPEC §4.2), in DECLARED order; 0 and an
    tags: const (const u8[:])[:], // empty list when there are none
    text_total: i64,          // a text list's `| total = N` (docs/SPEC-TABLES.md
                              // §2.13): the most bytes its elements hold in
                              // all; 0 for none
}

// TableDesc is one table — or a packet type a table reaches by value, which
// rides the table wire as a body — as data (docs/SPEC-TABLES.md §3, §8.1).
struct TableDesc {
    name: const u8[:],
    id: u64,                  // FNV-1a 64 of its wire name (§3.1)
    table: bool,
    fixed: bool,
    variable: bool,
    json_array: bool,
    integrity: bool,
    place: const (const u8[:])[:], // what a pointer walk from it may place
    place_str: bool,
    place_bytes: bool,
    fields: const FieldDesc[:],
    doc: const u8[:],         // the declaration's own `///` block (§8.1)
    num_tags: i32,            // and its tags, in DECLARED order
    tags: const (const u8[:])[:],
}

// TableDocNone is THE ONE EMPTY DOC (docs/SPEC-TABLES.md §8.1, §8.7): every
// descriptor of a declaration or a field with no `///` block names it, so the
// emitted text defines the empty doc once against every row that has none.
let TableDocNone: const u8[:] = "";

// UnionDesc is one union as data: one entry per arm, in tag order (§2.6).
struct UnionDesc {
    name: const u8[:],
    arms: const FieldDesc[:],
}

// LoadReg is one row of THE LOAD DISPATCH TABLE (registry/tablewire.goosereg,
// assembled onto this file as `load_registry`): the descriptor facts it
// matches — the shape, the array kind (0: any) and wstring — and the Loader
// it selects. The first row that matches wins. It lives here, beside the
// array it types, so this module checks on its own.
struct LoadReg { shape: u8, array: u8, wide: bool, loader: Loader }

// Crc32cTable is §2.14's CRC-32C (Castagnoli, reflected polynomial
// 0x82F63B78) as a byte table: entry i is the CRC register after the eight
// shifts of byte i.
let Crc32cTable: u32[256] = [
    0x00000000, 0xF26B8303, 0xE13B70F7, 0x1350F3F4, 0xC79A971F, 0x35F1141C, 0x26A1E7E8, 0xD4CA64EB,
    0x8AD958CF, 0x78B2DBCC, 0x6BE22838, 0x9989AB3B, 0x4D43CFD0, 0xBF284CD3, 0xAC78BF27, 0x5E133C24,
    0x105EC76F, 0xE235446C, 0xF165B798, 0x030E349B, 0xD7C45070, 0x25AFD373, 0x36FF2087, 0xC494A384,
    0x9A879FA0, 0x68EC1CA3, 0x7BBCEF57, 0x89D76C54, 0x5D1D08BF, 0xAF768BBC, 0xBC267848, 0x4E4DFB4B,
    0x20BD8EDE, 0xD2D60DDD, 0xC186FE29, 0x33ED7D2A, 0xE72719C1, 0x154C9AC2, 0x061C6936, 0xF477EA35,
    0xAA64D611, 0x580F5512, 0x4B5FA6E6, 0xB93425E5, 0x6DFE410E, 0x9F95C20D, 0x8CC531F9, 0x7EAEB2FA,
    0x30E349B1, 0xC288CAB2, 0xD1D83946, 0x23B3BA45, 0xF779DEAE, 0x05125DAD, 0x1642AE59, 0xE4292D5A,
    0xBA3A117E, 0x4851927D, 0x5B016189, 0xA96AE28A, 0x7DA08661, 0x8FCB0562, 0x9C9BF696, 0x6EF07595,
    0x417B1DBC, 0xB3109EBF, 0xA0406D4B, 0x522BEE48, 0x86E18AA3, 0x748A09A0, 0x67DAFA54, 0x95B17957,
    0xCBA24573, 0x39C9C670, 0x2A993584, 0xD8F2B687, 0x0C38D26C, 0xFE53516F, 0xED03A29B, 0x1F682198,
    0x5125DAD3, 0xA34E59D0, 0xB01EAA24, 0x42752927, 0x96BF4DCC, 0x64D4CECF, 0x77843D3B, 0x85EFBE38,
    0xDBFC821C, 0x2997011F, 0x3AC7F2EB, 0xC8AC71E8, 0x1C661503, 0xEE0D9600, 0xFD5D65F4, 0x0F36E6F7,
    0x61C69362, 0x93AD1061, 0x80FDE395, 0x72966096, 0xA65C047D, 0x5437877E, 0x4767748A, 0xB50CF789,
    0xEB1FCBAD, 0x197448AE, 0x0A24BB5A, 0xF84F3859, 0x2C855CB2, 0xDEEEDFB1, 0xCDBE2C45, 0x3FD5AF46,
    0x7198540D, 0x83F3D70E, 0x90A324FA, 0x62C8A7F9, 0xB602C312, 0x44694011, 0x5739B3E5, 0xA55230E6,
    0xFB410CC2, 0x092A8FC1, 0x1A7A7C35, 0xE811FF36, 0x3CDB9BDD, 0xCEB018DE, 0xDDE0EB2A, 0x2F8B6829,
    0x82F63B78, 0x709DB87B, 0x63CD4B8F, 0x91A6C88C, 0x456CAC67, 0xB7072F64, 0xA457DC90, 0x563C5F93,
    0x082F63B7, 0xFA44E0B4, 0xE9141340, 0x1B7F9043, 0xCFB5F4A8, 0x3DDE77AB, 0x2E8E845F, 0xDCE5075C,
    0x92A8FC17, 0x60C37F14, 0x73938CE0, 0x81F80FE3, 0x55326B08, 0xA759E80B, 0xB4091BFF, 0x466298FC,
    0x1871A4D8, 0xEA1A27DB, 0xF94AD42F, 0x0B21572C, 0xDFEB33C7, 0x2D80B0C4, 0x3ED04330, 0xCCBBC033,
    0xA24BB5A6, 0x502036A5, 0x4370C551, 0xB11B4652, 0x65D122B9, 0x97BAA1BA, 0x84EA524E, 0x7681D14D,
    0x2892ED69, 0xDAF96E6A, 0xC9A99D9E, 0x3BC21E9D, 0xEF087A76, 0x1D63F975, 0x0E330A81, 0xFC588982,
    0xB21572C9, 0x407EF1CA, 0x532E023E, 0xA145813D, 0x758FE5D6, 0x87E466D5, 0x94B49521, 0x66DF1622,
    0x38CC2A06, 0xCAA7A905, 0xD9F75AF1, 0x2B9CD9F2, 0xFF56BD19, 0x0D3D3E1A, 0x1E6DCDEE, 0xEC064EED,
    0xC38D26C4, 0x31E6A5C7, 0x22B65633, 0xD0DDD530, 0x0417B1DB, 0xF67C32D8, 0xE52CC12C, 0x1747422F,
    0x49547E0B, 0xBB3FFD08, 0xA86F0EFC, 0x5A048DFF, 0x8ECEE914, 0x7CA56A17, 0x6FF599E3, 0x9D9E1AE0,
    0xD3D3E1AB, 0x21B862A8, 0x32E8915C, 0xC083125F, 0x144976B4, 0xE622F5B7, 0xF5720643, 0x07198540,
    0x590AB964, 0xAB613A67, 0xB831C993, 0x4A5A4A90, 0x9E902E7B, 0x6CFBAD78, 0x7FAB5E8C, 0x8DC0DD8F,
    0xE330A81A, 0x115B2B19, 0x020BD8ED, 0xF0605BEE, 0x24AA3F05, 0xD6C1BC06, 0xC5914FF2, 0x37FACCF1,
    0x69E9F0D5, 0x9B8273D6, 0x88D28022, 0x7AB90321, 0xAE7367CA, 0x5C18E4C9, 0x4F48173D, 0xBD23943E,
    0xF36E6F75, 0x0105EC76, 0x12551F82, 0xE03E9C81, 0x34F4F86A, 0xC69F7B69, 0xD5CF889D, 0x27A40B9E,
    0x79B737BA, 0x8BDCB4B9, 0x988C474D, 0x6AE7C44E, 0xBE2DA0A5, 0x4C4623A6, 0x5F16D052, 0xAD7D5351
];

// crc32c is §2.14's CRC-32C over data[start..end], one table lookup per byte.
// GCC 15 -O2 (-foptimize-crc) miscompiles the bitwise loop shape Goose lowers
// `while` into, so the table is the form this runtime keeps.
fn crc32c(data: const u8[:], start: i64, end: i64) -> u32 {
    var crc: u32 = 0xFFFFFFFF;
    var i = start;
    while i < end {
        crc = Crc32cTable[((crc ^ data[i]) & 0xFF) as! i64] ^ (crc >> 8);
        i += 1;
    }
    return crc ^ 0xFFFFFFFF;
}

// integrity_holds is §2.14's read check: a file or batch of at least
// min_body + 4 bytes whose last four bytes (little-endian) match the
// CRC-32C of every byte before them.
fn integrity_holds(data: const u8[:], min_body: i64) -> bool {
    let n = data.len - 4;
    if n < min_body { return false; }
    let got: u32 = data[n]
        | ((data[n + 1] as u32) << 8)
        | ((data[n + 2] as u32) << 16)
        | ((data[n + 3] as u32) << 24);
    return got == crc32c(data, 0, n);
}
