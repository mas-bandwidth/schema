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
}

// TableDesc is one table — or a packet type a table reaches by value, which
// rides the table wire as a body — as data (docs/SPEC-TABLES.md §3, §8.1).
struct TableDesc {
    name: const u8[:],
    id: u64,                  // FNV-1a 64 of its wire name (§3.1)
    table: bool,
    fixed: bool,
    variable: bool,
    place: const (const u8[:])[:], // what a pointer walk from it may place
    place_str: bool,
    place_bytes: bool,
    fields: const FieldDesc[:],
}

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
