// strict_and_refusals.lobster — the strict read's one-call form and every
// read-time refusal by Go's name (docs/SPEC-TABLES.md §6.5; docs/PORT-LOBSTER.md,
// THE LOAD).
//
// THE PLAIN LOAD IS THE STRICT READ; THE TOLERANT READ HAS A NAME OF ITS OWN
// (§6.5). Both live in load.lobster: schema_rt.load is strict (nil on any
// refusal, the reason from strict_reason) and schema_rt.load_tolerant keeps
// the recovered record and the counters. This file adds load_strict, Go's
// ReadStrict: one call, the record or ONE typed reason — non_finite_float,
// unknown_form, missing_required, malformed, blob_shared, clamped or
// kind_mismatch. Unknown and widened alone are not loss and do not refuse
// (§6.5). Every name is Go's TableRefuseReason spelling (refusal.go).
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// load_strict is the strict read in one call (§6.5): a refusal answers nil
// and the reason; a clean read answers the record and "". Unknown and
// widened alone are not a refusal.
def load_strict(root:string, data:string, report:LoadReport) -> Record?, string:
    let rec = load_tolerant(root, data, report)
    let reason = strict_reason(report)
    if reason != "":
        return nil, reason
    return rec, ""
