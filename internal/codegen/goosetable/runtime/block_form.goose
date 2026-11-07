// block_form.goose — the two ACCELERATORS' shared runtime: the block form
// (docs/SPEC-TABLES.md §19) and what the cook (§7, cook_form.goose) shares
// with it — the refusal vocabulary, the word moves and the record
// descriptors. It sits beside schema_tables.goose in the table surface's
// runtime namespace (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT). The per-table
// code — every constant of a block's layout, its Open, its Begin and its row
// accessors — is emitted by block_form.go into <Base>Block.goose, ON THE SIDE:
// a <Base>Table.goose carries not one symbol of it (§19, the zero-cost gate).
//
// A BLOCK IS BYTES ANOTHER BUILD WROTE, so it is read where it lies: a
// `const u8[:]` view of the caller's buffer, every word assembled with shifts
// in this build's byte order (docs/PORT-GOOSE.md, BYTE BUFFERS), nothing
// copied and nothing per row (docs/PORTING.md M7). A Goose slice carries no
// address a program can measure, so THE BASE'S ALIGNMENT IS THE CALLER'S
// STATED `lead` — the residue of its buffer's address modulo sixty-four, the
// Elixir leg's shape for the same absence — and a lead below zero is the
// reference's null base. Open checks it LAST, as §19.2 orders it.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

// THE REFUSAL VOCABULARY (docs/SPEC-TABLES.md §7, §19.2): TableRefuseReason by
// the reference's number. One enum serves both accelerators, because a
// consumer that falls back from either falls back the same way.
let TableRefuseOk: i64 = 0;
let TableRefuseNotACook: i64 = 1;
let TableRefuseForeignOrder: i64 = 2;
let TableRefuseWrongBuildVersion: i64 = 3;
let TableRefuseReservedNotZero: i64 = 4;
let TableRefuseBadAlignment: i64 = 5;
let TableRefuseTruncated: i64 = 6;
let TableRefuseUnalignedBase: i64 = 7;
let TableRefuseBadLayout: i64 = 8;

// table_refuse_name is a reason's value name, the spelling the conformance
// reason surfaces pin (test/conformance/README.md).
fn table_refuse_name(code: i64) -> const u8[:] {
    match code {
        0 => "ok",
        1 => "not_a_cook",
        2 => "foreign_order",
        3 => "wrong_build_version",
        4 => "reserved_not_zero",
        5 => "bad_alignment",
        6 => "truncated",
        7 => "unaligned_base",
        8 => "bad_layout",
        9 => "unknown_form",
        10 => "count_over_length",
        11 => "count_over_extent_cap",
        12 => "blob_over_size_cap",
        13 => "data_cycle",
        _ => "???",
    }
}

// THE BLOCK'S PROLOGUE (docs/SPEC-TABLES.md §19.1). The magic is SCHMABLK read
// as ASCII in the order a little-endian store produces; its byte reversal is a
// block of the other order. THIS BUILD'S ORDER IS LITTLE: every word below is
// assembled little-endian whatever the host, so the prologue's byte_order
// word this side writes and expects is 1.
let TableBlockMagic: u64 = 0x4b4c42414d484353;
let TableBlockMagicSwapped: u64 = 0x5343484d41424c4b;
let TableBlockByteOrder: u64 = 1;
let TableBlockAlign: i64 = 64;
let TableBlockPrologueBytes: i64 = 24;

// TableBlockRefusal names what Begin refused (docs/SPEC-TABLES.md §19.1): the
// array, its count and its maximum, so the call site knows which to clamp.
struct TableBlockRefusal { array: const u8[:], count: i64, maximum: i64 }

// THE WORD MOVES, little-endian, assembled with shifts and never through a
// host-order view (docs/PORT-GOOSE.md, BYTE BUFFERS).
fn accel_u8(b: const u8[:], at: i64) -> u64 { return b[at]; }

fn accel_u16(b: const u8[:], at: i64) -> u64 {
    let lo: u64 = b[at];
    let hi: u64 = b[at + 1];
    return lo | hi << 8;
}

fn accel_u32(b: const u8[:], at: i64) -> u64 {
    return accel_u16(b, at) | accel_u16(b, at + 2) << 16;
}

fn accel_u64(b: const u8[:], at: i64) -> u64 {
    return accel_u32(b, at) | accel_u32(b, at + 4) << 32;
}

// accel_get reads a w-byte little-endian word, w in 1, 2, 4, 8.
fn accel_get(b: const u8[:], at: i64, w: i64) -> u64 {
    match w {
        1 => accel_u8(b, at),
        2 => accel_u16(b, at),
        4 => accel_u32(b, at),
        _ => accel_u64(b, at),
    }
}

// accel_get_signed reads a w-byte two's-complement word, sign-extended.
fn accel_get_signed(b: const u8[:], at: i64, w: i64) -> i64 {
    let raw = accel_get(b, at, w);
    if w >= 8 { return raw as! i64; }
    let shift = (64 - w * 8) as u64;
    return ((raw << shift) as! i64) >> shift;
}

// accel_put writes the low w bytes of v, little-endian, at an offset.
fn accel_put(b: u8[:], at: i64, v: u64, w: i64) {
    var i: i64 = 0;
    while i < w {
        b[at + i] = (v >> ((i as u64) * 8)) as! u8;
        i += 1;
    }
}

// THE FLOAT BITS move through the one route fixed.goose fixes (FLOAT32,
// docs/PORT-GOOSE.md): a `bytes_of` view one way, a verified one-element image
// the other, never arithmetic, so a NaN's payload survives a row.
fn accel_f32_bits(x: f32) -> u64 {
    let a: f32[1] = [x];
    return accel_u32(bytes_of(a), 0);
}

fn accel_bits_f32(u: u64) -> f32 {
    var img: u8[>..] = [];
    img.push(4);
    var i: i64 = 0;
    while i < 4 {
        img.push((u >> ((i as u64) * 8)) as! u8);
        i += 1;
    }
    let arr, ok = from_bytes<f32[>..]>(img);
    if !ok || arr.len != 1 { return 0.0; }
    return arr[0];
}

fn accel_f64_bits(x: f64) -> u64 {
    let a: f64[1] = [x];
    return accel_u64(bytes_of(a), 0);
}

fn accel_bits_f64(u: u64) -> f64 {
    var img: u8[>..] = [];
    img.push(8);
    var i: i64 = 0;
    while i < 8 {
        img.push((u >> ((i as u64) * 8)) as! u8);
        i += 1;
    }
    let arr, ok = from_bytes<f64[>..]>(img);
    if !ok || arr.len != 1 { return 0.0; }
    return arr[0];
}

// accel_text is a string's or a `bytes`' USED bytes, as a view: the used
// length at count_at, held to [0, bound] so a forged companion never reaches
// past the field's own storage (a forged one reads as empty).
fn accel_text(b: const u8[:], at: i64, count_at: i64, bound: i64) -> const u8[:] {
    let used = accel_get_signed(b, count_at, 4);
    if used < 0 || used > bound { return b[at..at]; }
    return b[at..at + used];
}

// accel_put_text writes a string's or a `bytes`' value and its used length;
// a value past the bound writes the bound's worth. The bytes past the used
// length are the caller's (§19.1: a row the producer does not fill holds what
// the storage held).
fn accel_put_text(b: u8[:], at: i64, count_at: i64, bound: i64, v: const u8[:]) {
    var n = v.len;
    if n > bound { n = bound; }
    var i: i64 = 0;
    while i < n {
        b[at + i] = v[i];
        i += 1;
    }
    accel_put(b, count_at, n as! u64, 4);
}

// THE RECORD DESCRIPTORS (docs/SPEC-TABLES.md §8.1, §19.2): one record's
// layout AS DATA — every field's offset, size and table-wire kind, the
// row-walk columns (the array class, the slot count and element size, the
// count companion and the presence companion) and, for a block projection,
// each out-of-line array's triple. A consumer holding these reads an instance
// with no generated struct and no knowledge of the spelling that produced it.
// An offset this record has no companion for is -1.
struct AccelField {
    name: const u8[:],
    offset: i64,
    size: i64,
    kind: u8,               // the table-wire kind (docs/SPEC-TABLES.md §3)
    elem_size: i64,         // one slot's bytes: an enum's ordinal width, not its kind's
    is_array: bool,
    array_bound: i64,       // an array's slots, a string's or bytes' maximum
    counted: bool,
    count_offset: i64,      // the int32 count or used length
    optional: bool,
    present_offset: i64,    // the bool presence companion
    is_text: bool,          // string(N): used bytes, quoted
    is_bytes: bool,         // bytes(N): the same
    is_pointer: bool,       // *T: an int64 self-relative delta, null zero (§6.3)
    is_union: bool,         // a union: a tag beside overlaid arms
    record: i64,            // a nested or pointed-to record, by index; -1 for none
    out_of_line: bool,      // a block projection's out-of-line array
    offset_of_offset: i64,
    stride_offset: i64,
    stride: i64,            // this build's pitch, asserted against the row's size
}

struct AccelRecord {
    name: const u8[:],
    size: i64,
    align: i64,
    type_id: u64,           // the node type id (FNV-1a 64 of its wire name, §3.1)
    fields: const AccelField[:],
}

// block_check_array is one out-of-line array's clauses of §19.2's check, in
// the reference's order: the PITCH against this build's own, the COUNT
// against the declared maximum, the offset_of at or past the projection's end
// and a multiple of the array's start alignment, and the rows inside the
// extent. Every term is bounded before it is added, so a forged offset near
// 2^63 refuses rather than wrapping. It answers the reason, and moves `used`
// to the array's end.
fn block_check_array(b: const u8[:], triple: i64, stride: i64, maximum: i64, start_align: i64, projection: i64, used: i64&) -> i64 {
    let extent = b.len as u64;
    let offset_of = accel_u64(b, triple);
    let count = accel_u32(b, triple + 8);
    let pitch = accel_u32(b, triple + 12);
    if pitch != stride as u64 { return TableRefuseBadLayout; }
    if count > maximum as u64 { return TableRefuseBadLayout; }
    if offset_of < projection as u64 || offset_of % (start_align as u64) != 0 { return TableRefuseBadLayout; }
    if offset_of > extent { return TableRefuseBadLayout; }
    let rows = count * pitch; // both bounded above: this cannot carry
    if rows > extent - offset_of { return TableRefuseBadLayout; }
    let end = (offset_of + rows) as! i64;
    if end > used { used = end; }
    return TableRefuseOk;
}

// block_check_head is §19.2's first clauses: the bytes against the whole
// projection, the magic read first (its reversal a block of the other order),
// the byte_order word, then the build version.
fn block_check_head(b: const u8[:], projection: i64, build: u64) -> i64 {
    if b.len < projection { return TableRefuseTruncated; }
    let magic = accel_u64(b, 0);
    if magic != TableBlockMagic {
        if magic == TableBlockMagicSwapped { return TableRefuseForeignOrder; }
        return TableRefuseNotACook;
    }
    if accel_u64(b, 16) != TableBlockByteOrder { return TableRefuseNotACook; }
    if accel_u64(b, 8) != build { return TableRefuseWrongBuildVersion; }
    return TableRefuseOk;
}

// block_check_tail is §19.2's last two clauses: the USED extent — the
// greatest array end, rounded to sixty-four without the rounding itself
// wrapping — against the bytes the caller passed, and LAST the base's
// alignment, the one clause that reads nothing out of the block.
fn block_check_tail(b: const u8[:], used: i64, lead: i64) -> i64 {
    let padding = (TableBlockAlign - used % TableBlockAlign) % TableBlockAlign;
    if padding > b.len - used { return TableRefuseTruncated; }
    if lead % TableBlockAlign != 0 { return TableRefuseUnalignedBase; }
    return TableRefuseOk;
}

// block_begin_head stamps the prologue (§19.1): magic, build version, order.
fn block_begin_head(b: u8[:], build: u64) {
    accel_put(b, 0, TableBlockMagic, 8);
    accel_put(b, 8, build, 8);
    accel_put(b, 16, TableBlockByteOrder, 8);
}

// block_begin_array writes one array's triple at the running offset, rounded
// up to the array's start alignment, and answers the offset past its rows.
fn block_begin_array(b: u8[:], triple: i64, offset: i64, start_align: i64, count: i64, stride: i64) -> i64 {
    let at = (offset + start_align - 1) / start_align * start_align;
    accel_put(b, triple, at as! u64, 8);
    accel_put(b, triple + 8, count as! u64, 4);
    accel_put(b, triple + 12, stride as! u64, 4);
    return at + count * stride;
}
