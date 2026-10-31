// fixedevolve.goose — the fixed-form evolution runtime (docs/PORT-GOOSE.md,
// THE RUNTIME LAYOUT; docs/FIXED-FORM-VERSIONING-TESTS.md; docs/FIXED-FORM-
// ALGORITHM.md §5). It holds the lineage report (the §5.4 counters), the
// refusal names the evolution read carries by Go's name, and the framing
// checks that read a file before any record lands. The per-root lineage and
// the <Root>FixedEvolveLoad function are emitted by fixedevolve.go.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;
import .schema_tables;
import .fixed;

// The TWO lineage refusals the fixed form's base runtime does not name
// (docs/SPEC-TABLES.md §21.3, docs/FIXED-FORM-ALGORITHM.md §5.3):
// layout_unsupported is below the retirement floor (upgrade the client);
// layout_newer is reserved until the lineage proves direction.
let FixedRefuseLayoutUnsupported: i64 = 29;
let FixedRefuseLayoutNewer: i64 = 30;

// The versioning counters a NEW-READS-OLD read reports when it returns
// (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #6): every read starts at
// zero, each op counts once per entry per record, and a refusal lands nothing.
// `unknown` is once per FIELD per peer, never per record.
struct FixedEvolveReport {
    clamped: i64,
    widened: i64,
    unknown: i64,
    kind_mismatch: i64,
    malformed: bool,
    reason: i64,
    hash: u64,
}

// fixed_evolve_refusal_name is a refusal's name, the string Go's Error()
// gives (internal/codegen/gotable/refusal.go; docs/SPEC-TABLES.md §6.5).
fn fixed_evolve_refusal_name(code: i64) -> const u8[:] {
    match code {
        0 => "ok",
        4 => "reserved_not_zero",
        6 => "truncated",
        14 => "invalid_value",
        18 => "clamped",
        25 => "previous_form",
        26 => "newer_form",
        27 => "message_form_as_file",
        28 => "layout_unknown",
        29 => "layout_unsupported",
        30 => "layout_newer",
        31 => "layout_malformed",
        40 => "no_layout",
        41 => "batch_too_large",
        42 => "text_ill_formed",
        43 => "count_over_bound",
        44 => "length_over_bound",
        _ => "???",
    }
}

// fixed_evolve_refuse records a refusal by name; nothing landed, no counter
// moved, and the file's hash survives when the hash is what refused.
fn fixed_evolve_refuse(report: FixedEvolveReport&, code: i64, malformed: bool) -> i64 {
    let hash = report.hash;
    report = FixedEvolveReport { .. };
    report.hash = hash;
    report.reason = code;
    report.malformed = malformed;
    return -1;
}

// fixed_evolve_header holds a file to §2's framing before the hash selects a
// plan: the form byte before the length, the twenty-byte minimum, the
// reserved zeros and the layout's length. 0 is none.
fn fixed_evolve_header(b: const u8[:], report: FixedEvolveReport&) -> i64 {
    if b.len < 1 { return fixed_evolve_refuse(report, FixedRefuseTruncated, true); }
    if b[0] != FixedForm {
        if b[0] == 1 { return fixed_evolve_refuse(report, FixedRefusePreviousForm, false); }
        if b[0] == 2 { return fixed_evolve_refuse(report, FixedRefuseMessageFormAsFile, false); }
        return fixed_evolve_refuse(report, FixedRefuseNewerForm, false);
    }
    if b.len < FixedHeaderBytes + 4 { return fixed_evolve_refuse(report, FixedRefuseTruncated, true); }
    var i: i64 = 1;
    while i < 8 {
        if b[i] != 0 { return fixed_evolve_refuse(report, FixedRefuseReservedNotZero, true); }
        i += 1;
    }
    let layout_length = fixed_u32(b, FixedHeaderBytes) as i64;
    if layout_length + FixedHeaderBytes + 4 > b.len {
        return fixed_evolve_refuse(report, FixedRefuseLayoutMalformed, false);
    }
    return 0;
}

// fixed_evolve_layout_ok is the byte comparison against the entry the lock
// recorded — never a walk (§5.3 step 5): a known hash over different bytes is
// layout_malformed.
fn fixed_evolve_layout_ok(b: const u8[:], layout_length: i64, layout: const u8[:]) -> bool {
    if layout_length != layout.len { return false; }
    var i: i64 = 0;
    while i < layout.len {
        if b[FixedHeaderBytes + 4 + i] != layout[i] { return false; }
        i += 1;
    }
    return true;
}

// fixed_evolve_read_u reads `w` bytes little-endian at `at` as a u64. It is
// the element read of an array element widening (docs/FIXED-FORM-VERSIONING-
// TESTS.md, array_elem_widen): the written record carries `w`-byte elements,
// and the reader widens each one in place.
fn fixed_evolve_read_u(b: const u8[:], at: i64, w: i64) -> u64 {
    var v: u64 = 0;
    var i: i64 = 0;
    while i < w {
        v |= (b[at + i] as u64) << (8 * i);
        i += 1;
    }
    return v;
}
