// cook_form.goose — the cooked form's runtime (docs/SPEC-TABLES.md §7): the
// header check every generated <Name>CookOpen is, plus its own root's two
// layout facts, and the deref a cooked pointer slot takes. It sits beside
// block_form.goose, whose word moves, refusal vocabulary and record
// descriptors it shares (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
//
// A COOK IS OPENED IN O(1) (docs/PORTING.md M8): the header and nothing per
// node, so a one-megabyte cook and a one-gigabyte cook open in the same time.
// On a match the bytes ARE what this build wrote, in this build's layout and
// byte order, so there is nothing to validate and nothing to fix up: the
// region is a view of the data part and the root is at its offset zero.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .block_form;
import .wire_load;

// THE HEADER (§7.1): eight u64 words, 64 bytes, in the order that wrote the
// file. The magic is SCHMCOOK read as ASCII in the order a little-endian store
// produces; its byte reversal is a cook of the other order. THIS BUILD'S ORDER
// IS LITTLE (block_form.goose).
let TableCookMagic: u64 = 0x4b4f4f434d484353;
let TableCookMagicSwapped: u64 = 0x5343484d434f4f4b;
let TableCookByteOrder: u64 = 1;
let TableCookHeaderBytes: i64 = 64;
// the greatest region alignment a cook may name: the data part begins at
// align_up(64, alignment), which is 64 for every unit this language declares
let TableCookMaxAlign: u64 = 64;

// cook_check is THE WHOLE CHECK, in §7's order: the magic read first (its
// reversal a cook of the other order), the byte order word it establishes,
// the build version against this build's own, both RESERVED words zero, the
// region alignment the header names (a power of two, at least eight, at most
// the cap, a multiple of the root's), the two part lengths against the length
// the caller passed — truncation and trailing bytes are one refusal — the
// root's own storage inside the data part, and LAST the base's alignment as a
// residue: `lead` is the buffer's address modulo sixty-four as the caller
// states it, below zero for no buffer at all. Every number out of the file is
// bounded before it is added, so a forged length near 2^64 refuses rather than
// wrapping. Nothing per node.
fn cook_check(b: const u8[:], lead: i64, root_size: u64, root_align: u64, build: u64) -> i64 {
    if lead < 0 { return TableRefuseUnalignedBase; }
    let length = b.len as u64;
    if length < TableCookHeaderBytes as u64 { return TableRefuseTruncated; }
    let magic = accel_u64(b, 0);
    if magic != TableCookMagic {
        if magic == TableCookMagicSwapped { return TableRefuseForeignOrder; }
        return TableRefuseNotACook;
    }
    if accel_u64(b, 16) != TableCookByteOrder { return TableRefuseNotACook; }
    if accel_u64(b, 8) != build { return TableRefuseWrongBuildVersion; }
    if accel_u64(b, 48) != 0 { return TableRefuseReservedNotZero; }
    if accel_u64(b, 56) != 0 { return TableRefuseReservedNotZero; }
    let data_length = accel_u64(b, 24);
    let attribution_length = accel_u64(b, 32);
    let alignment = accel_u64(b, 40);
    if alignment < 8 || alignment > TableCookMaxAlign { return TableRefuseBadAlignment; }
    if (alignment & (alignment - 1)) != 0 { return TableRefuseBadAlignment; }
    if alignment % root_align != 0 { return TableRefuseBadAlignment; }
    let data_offset = ((TableCookHeaderBytes as u64) + alignment - 1) & ~(alignment - 1);
    if length < data_offset { return TableRefuseTruncated; }
    if data_length > length - data_offset { return TableRefuseTruncated; }
    if attribution_length != length - data_offset - data_length { return TableRefuseTruncated; }
    if data_length < root_size { return TableRefuseTruncated; }
    // the data part is padded to the region's alignment, so the base's residue
    // is the buffer's
    if ((lead as u64) + data_offset) % alignment != 0 { return TableRefuseUnalignedBase; }
    return TableRefuseOk;
}

// cook_region is an OPENED cook's data part, as a view: the region the root
// sits at the base of. Call it only on a cook Open accepted.
fn cook_region(b: const u8[:]) -> const u8[:] {
    let alignment = accel_u64(b, 40) as! i64;
    let data_offset = (TableCookHeaderBytes + alignment - 1) / alignment * alignment;
    let data_length = accel_u64(b, 24) as! i64;
    return b[data_offset..data_offset + data_length];
}

// cook_deref follows the `*T` slot at `slot` in a region: the slot holds the
// signed self-relative delta of §6.3, so a deref is one add, and NULL IS A
// DELTA OF ZERO. The delta is data, so it is added as an unsigned integer and
// bounded before it is used: a target outside the region answers -1, as null
// does, and never an offset a read could leave the region by.
fn cook_deref(region: const u8[:], slot: i64) -> i64 {
    let delta = accel_u64(region, slot);
    if delta == 0 { return -1; }
    let target = (slot as u64) + delta;
    if target >= region.len as u64 { return -1; }
    return target as! i64;
}

// ---- THE WRITE SIDE (§7.6; internal/codegen/goosetable/cook_write.go) ----

// cook_put writes the low w bytes of v at an offset in the order the cook is
// FOR — `big` is the target build's order, never the host's — and nothing at
// all into an empty view, which is the dry run a write validates with first.
fn cook_put(b: u8[:], at: i64, v: u64, w: i64, big: bool) {
    if b.len == 0 { return; }
    var i: i64 = 0;
    while i < w {
        let shift = if big { w - 1 - i } else { i };
        b[at + i] = (v >> ((shift as u64) * 8)) as! u8;
        i += 1;
    }
}

// cook_put_bytes writes a string's or a `bytes`' used bytes, which no byte
// order touches.
fn cook_put_bytes(b: u8[:], at: i64, src: const u8[:]) {
    if b.len == 0 { return; }
    var i: i64 = 0;
    while i < src.len {
        b[at + i] = src[i];
        i += 1;
    }
}

// cook_begin zeroes the whole extent — EVERY BYTE NO FIELD COVERS IS ZERO
// (§7.2), one zeroing rather than a rule per padding site — and writes the
// header (§7.1): the magic and every word after it in the target's order, the
// order word itself (1 little, 2 big), the two part lengths and the region
// alignment; both reserved words stay zero.
fn cook_begin(b: u8[:], build: u64, data: i64, nodes: i64, align: i64, big: bool) {
    var i: i64 = 0;
    while i < b.len {
        b[i] = 0;
        i += 1;
    }
    cook_put(b, 0, TableCookMagic, 8, big);
    cook_put(b, 8, build, 8, big);
    cook_put(b, 16, if big { 2 } else { 1 }, 8, big);
    cook_put(b, 24, data as! u64, 8, big);
    cook_put(b, 32, (nodes * 16) as! u64, 8, big);
    cook_put(b, 40, align as! u64, 8, big);
}

// THE LOADED VALUE'S READS (wire_load.goose, LoadVal) the typed copy takes:
// field i of a record, element k of a collection, a union's payload slot —
// each -1 where the load made no slot — and a value's bits, count and text.
fn cook_loaded_field(vals: const LoadVal[:], rec: i64, i: i64) -> i64 {
    if rec < 0 || !vals[rec].made || i >= vals[rec].count { return -1; }
    return vals[rec].first + i;
}

fn cook_loaded_elem(vals: const LoadVal[:], s: i64, k: i64) -> i64 {
    if s < 0 || !vals[s].made || k >= vals[s].count { return -1; }
    return vals[s].first + k;
}

fn cook_loaded_payload(vals: const LoadVal[:], s: i64) -> i64 {
    if s < 0 || !vals[s].made { return -1; }
    return vals[s].first;
}

fn cook_loaded_rode(vals: const LoadVal[:], s: i64) -> bool {
    return s >= 0 && vals[s].state != ValAbsent;
}

fn cook_loaded_bits(vals: const LoadVal[:], s: i64) -> u64 { return vals[s].bits.lo; }

fn cook_loaded_count(vals: const LoadVal[:], s: i64, bound: i64) -> i32 {
    var n = vals[s].len;
    if n < 0 { n = 0; }
    if n > bound { n = bound; }
    return n as! i32;
}

// cook_loaded_text copies a text value's bytes out of the input into the
// record's buffer and answers the used length.
fn cook_loaded_text(data: const u8[:], vals: const LoadVal[:], s: i64, dst: u8[:]) -> i32 {
    var n = vals[s].len;
    if n > dst.len { n = dst.len; }
    if n < 0 { n = 0; }
    let off = vals[s].off;
    var i: i64 = 0;
    while i < n {
        dst[i] = data[off + i];
        i += 1;
    }
    return n as! i32;
}
