// fixedevolve.goose — the fixed-form evolution runtime (docs/PORT-GOOSE.md,
// THE RUNTIME LAYOUT; docs/FIXED-FORM-VERSIONING-TESTS.md): the counters the
// versioning reads carry and the lineage/floor tokens the refusal paths name.
// The fixed-form codec (fixedform.go, runtime/fixed.goose) is a dependency of
// this card and has not landed; this module is the surface it will fill.
//
// SPDX-License-Identifier: NONE — emitted into your output; the schema
// compiler is AGPL-3.0, its output is not.
namespace schema_rt;

import .schema_rt;

// The versioning counters a NEW-READS-OLD read reports when it returns
// (docs/FIXED-FORM-VERSIONING-TESTS.md §5.4, §5.9 #6): every read starts at
// zero, each op counts once per entry per record, and a refusal lands nothing.
struct EvolveCounters {
    clamped: i64,   // the bounds pass, once per entry per record
    widened: i64,   // a widened ordinal or integer, once per field
    unknown: i64,   // an unknown field, once per field per peer
}

// The refusal reasons a fixed-form evolution read carries, by Go's name.
let RefuseLayoutNewer: i32 = 1;
let RefuseLayoutUnsupported: i32 = 2;
let RefuseLayoutMalformed: i32 = 3;
