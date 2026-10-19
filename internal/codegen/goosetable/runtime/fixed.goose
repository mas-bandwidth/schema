// fixed.goose — the fixed form (form byte 3) runtime
// (docs/FIXED-FORM-ALGORITHM.md). This file follows the design of
// docs/PORT-GOOSE.md (THE RUNTIME LAYOUT) and sits beside schema_tables.goose
// in the table surface's runtime namespace. It implements the form-3 framing
// and the layout-header check: a file is the form byte 3, seven reserved zero
// bytes, the layout hash (u64 LE), the layout's byte length (u32 LE), the
// layout, then records back to back. A record is the same u64 hash followed by
// the root's constant body (docs/SPEC-TABLES.md §3.4).
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_tables;

// The fixed form's one form byte and the constant header (docs/SPEC-TABLES.md
// §3.4): form, seven reserved zeros, the layout hash.
let FixedForm: u8 = 3;
let FixedHeaderBytes: i64 = 16;

// fixed_u32 / fixed_u64 read one little-endian word at an offset
// (docs/PORT-GOOSE.md, BYTE BUFFERS: assembled with shifts, never a host-order
// view).
fn fixed_u32(b: const u8[:], at: i64) -> u32 {
    return b[at] as u32 | (b[at + 1] as u32) << 8 | (b[at + 2] as u32) << 16 | (b[at + 3] as u32) << 24;
}

fn fixed_u64(b: const u8[:], at: i64) -> u64 {
    return b[at] as u64 | (b[at + 1] as u64) << 8 | (b[at + 2] as u64) << 16 | (b[at + 3] as u64) << 24
        | (b[at + 4] as u64) << 32 | (b[at + 5] as u64) << 40 | (b[at + 6] as u64) << 48 | (b[at + 7] as u64) << 56;
}

// fixed_check_file holds ONE file to the fixed framing and the layout this
// build baked (docs/FIXED-FORM-ALGORITHM.md §2, §5.3). It returns the record
// count on success, or a negative refusal:
//
//	-1 malformed (short, ragged tail)
//	-2 previous_form, message_form_as_file or newer_form (the form byte)
//	-3 layout_malformed (length or layout bytes)
//	-4 layout_unknown (the header hash)
//	-5 no_layout (a record's hash)
//
// The body itself is a constant-size image, so the record stride is the
// caller's baked <Root>FixedRecordBytes (docs/FIXED-FORM-ALGORITHM.md §3);
// this check never walks a record, which is the one reader path's job.
fn fixed_check_file(b: const u8[:], layout: const u8[:], hash: u64, record_bytes: i64) -> i64 {
    if b.len < FixedHeaderBytes + 4 { return -1; }
    if b[0] != FixedForm { return -2; }
    var i: i64 = 1;
    while i < 8 {
        if b[i] != 0 { return -1; }
        i += 1;
    }
    if fixed_u64(b, 8) != hash { return -4; }
    let layout_length = fixed_u32(b, 16) as i64;
    if layout_length != layout.len { return -3; }
    if b.len < FixedHeaderBytes + 4 + layout_length { return -3; }
    i = 0;
    while i < layout.len {
        if b[FixedHeaderBytes + 4 + i] != layout[i] { return -3; }
        i += 1;
    }
    if record_bytes <= 0 { return -1; }
    let rest = b.len - FixedHeaderBytes - 4 - layout_length;
    if rest % record_bytes != 0 { return -1; }
    var at: i64 = FixedHeaderBytes + 4 + layout_length;
    while at < b.len {
        if fixed_u64(b, at) != hash { return -5; }
        at += record_bytes;
    }
    return rest / record_bytes;
}

// fixed_write_header lays the framing this build baked into a caller's buffer
// (docs/SPEC-TABLES.md §3.4, THE FRAMING): form byte, seven reserved zeros, the
// layout hash, the layout length, the layout. It returns the header's byte
// length, or -1 when the buffer cannot hold it; it never outgrows the caller's
// slice.
fn fixed_write_header(out: u8[>..]&, layout: const u8[:], hash: u64) -> i64 {
    out.push(FixedForm);
    var i: i64 = 0;
    while i < 7 { out.push(0); i += 1; }
    i = 0;
    while i < 8 { out.push((hash >> ((i as u64) * 8)) as! u8); i += 1; }
    let n = layout.len as u64;
    i = 0;
    while i < 4 { out.push((n >> ((i as u64) * 8)) as! u8); i += 1; }
    i = 0;
    while i < layout.len { out.push(layout[i]); i += 1; }
    return FixedHeaderBytes + 4 + layout.len;
}
