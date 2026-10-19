// fixed.goose — the fixed form (form byte 3) runtime
// (docs/FIXED-FORM-ALGORITHM.md). This file follows the design of
// docs/PORT-GOOSE.md (THE RUNTIME LAYOUT) and sits beside schema_tables.goose
// in the table surface's runtime module. It implements the form-3 wire:
// a file = form byte 3, seven reserved zero bytes, the layout hash (u64 LE),
// the layout's byte length (u32 LE), the layout, then records back to back.
// A record = u64 hash + the root's body (docs/SPEC-TABLES.md §3.4).
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;

// The kind codes of the closed set (docs/FIXED-FORM-ALGORITHM.md §1).
let KindBool: u8 = 1;
let KindI8: u8 = 2;
let KindI16: u8 = 3;
let KindI32: u8 = 4;
let KindI64: u8 = 5;
let KindU8: u8 = 6;
let KindU16: u8 = 7;
let KindU32: u8 = 8;
let KindU64: u8 = 9;
let KindF32: u8 = 10;
let KindF64: u8 = 11;
let KindString: u8 = 12;
let KindTable: u8 = 13;
let KindArray: u8 = 14;
let KindUnion: u8 = 15;
let KindKeyed: u8 = 16;
let KindPtr: u8 = 17;
let KindI128: u8 = 18;
let KindU128: u8 = 19;
let KindFixedS8: u8 = 20;
let KindFixedS16: u8 = 21;
let KindFixedS32: u8 = 22;
let KindFixedS64: u8 = 23;
let KindFixedS128: u8 = 24;
let KindFixedU8: u8 = 25;
let KindFixedU16: u8 = 26;
let KindFixedU32: u8 = 27;
let KindFixedU64: u8 = 28;
let KindFixedU128: u8 = 29;
let KindEnum: u8 = 30;
let KindVariant: u8 = 32;
let KindWString: u8 = 33;
let KindOptional: u8 = 35;

// A layout entry: 17 bytes, sent once per carrier, never in a record
// (docs/FIXED-FORM-ALGORITHM.md §1).
struct FixedLayoutEntry { id: u64, kind: u8, size: u32, children: u32 }

// fnv1a64 over a byte span, the layout hash and every id
// (docs/FIXED-FORM-ALGORITHM.md §1, §5.2).
fn fnv1a64(data: const u8[:]) -> u64 {
    var h: u64 = 0xcbf29ce484222325;
    var i: i64 = 0;
    while i < data.len {
        h ^= data[i] as u64;
        h *= 0x100000001b3;
        i += 1;
    }
    return h;
}

The The layout hash = fnv over the layout bytes (its 4-byte count included)
// then over the definitions digest (docs/FIXED-FORM-ALGORITHM.md §5.2).
// FNV is sequential, so the loop continues over the digest; the emitter
// computes this at build time and bakes the u64, and the runtime only ever
// compares a header hash to the baked one.
fn fixed_layout_hash(layout: const u8[:], digest: const u8[:]) -> u64 {
    var h: u64 = 0xcbf29ce484222325;
    var i: i64 = 0;
    while i < layout.len {
        h ^= layout[i] as u64;
        h *= 0x100000001b3;
        i += 1;
    }
    i = 0;
    while i < digest.len {
        h ^= digest[i] as u64;
        h *= 0x100000001b3;
        i += 1;
    }
    return h;
}

// A fixed record body is the declared storage image of the root's fields at
// their constant widths (docs/FIXED-FORM-ALGORITHM.md §3). Constant size is a
// compile-time fact; <T>FixedBodySize is emitted per table.

// Read one fixed file: header + layout + records
// (docs/FIXED-FORM-ALGORITHM.md §2). Returns an error name, or "" on success.
fn fixed_read(bytes: const u8[:], known_layout: const u8[:], known_bytes: u32, plan: const u8[:]) -> const u8[:] {
    // REFUSE names (docs/FIXED-FORM-ALGORITHM.md §2, §5.3).
    if bytes.len < 1 { return "malformed"; }
    if bytes[0] != 3 {
        if bytes[0] == 1 { return "previous_form"; }
        if bytes[0] == 2 { return "message_form_as_file"; }
        return "newer_form";
    }
    if bytes.len < 20 { return "malformed"; }
    var i: i64 = 1;
    while i < 8 {
        if bytes[i] != 0 { return "malformed"; }
        i += 1;
    }
    var L: u32 = bytes[16] as u32 | (bytes[17] as u32) << 8 | (bytes[18] as u32) << 16 | (bytes[19] as u32) << 24;
    if 20 + L > bytes.len as u32 { return "layout_malformed"; }
    var h: u64 = bytes[8] as u64 | (bytes[9] as u64) << 8 | (bytes[10] as u64) << 16 | (bytes[11] as u64) << 24
        | (bytes[12] as u64) << 32 | (bytes[13] as u64) << 40 | (bytes[14] as u64) << 48 | (bytes[15] as u64) << 56;
    _ = h;
    // Compare the file's layout to the known lock bytes.
    var j: i64 = 0;
    while j < known_layout.len {
        if bytes[20 + j] != known_layout[j] { return "layout_malformed"; }
        j += 1;
    }
    var rest: i64 = bytes.len as i64 - 20 - known_layout.len as i64;
    if rest <= 8 { return "malformed"; }
    var record_bytes: i64 = known_bytes as i64;
    if rest % record_bytes != 0 { return "malformed"; }
    // Per-record: the hash must match the layout's.
    var p: i64 = 20 + known_layout.len;
    while p < bytes.len {
        var rh: u64 = bytes[p] as u64 | (bytes[p+1] as u64) << 8 | (bytes[p+2] as u64) << 16 | (bytes[p+3] as u64) << 24
            | (bytes[p+4] as u64) << 32 | (bytes[p+5] as u64) << 40 | (bytes[p+6] as u64) << 48 | (bytes[p+7] as u64) << 56;
        if rh != h { return "no_layout"; }
        p += record_bytes;
    }
    return "";
}
