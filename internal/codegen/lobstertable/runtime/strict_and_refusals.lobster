// strict_and_refusals.lobster — the strict read and every read-time refusal
// by Go's name (docs/SPEC-TABLES.md §6.5; docs/PORT-LOBSTER.md, THE LOAD).
//
// THE PLAIN LOAD IS THE STRICT READ; THE TOLERANT READ HAS A NAME OF ITS OWN
// (§6.5). schema_rt.load is the tolerant walk (it keeps the recovered record
// and the counters); load_tolerant is that walk under its own name, and
// load_strict is the plain name a caller reaches by accident, which refuses
// malformed, clamped, kind_mismatched, blob_shared, non_finite and an unknown
// form with ONE name. Unknown and widened alone are not loss and do not
// refuse (§6.5). Every name is Go's TableRefuseReason spelling (refusal.go).
// The runtime file joins the emitted runtime by glob; it edits neither
// load.lobster nor schema_save.lobster.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// strict_reason is docs/SPEC-TABLES.md §6.5's one typed reason for a read:
// the same order Go's TableReport.Err() applies (blob_shared before clamped
// before kind_mismatch, after a form refusal and malformed), and the empty
// string on a read whose only counters are unknown or widened.
def strict_reason(rep:LoadReport) -> string:
    if rep.refused or rep.reason == "form" or rep.reason == "message_form_as_file":
        return "unknown_form"
    if rep.malformed:
        return "malformed"
    if rep.blob_shared > 0:
        return "blob_shared"
    if rep.clamped > 0:
        return "clamped"
    if rep.kind_mismatch > 0:
        return "kind_mismatch"
    if length(rep.reason) >= 12 and substring(rep.reason, 0, 12) == "non_finite: ":
        return "non_finite"
    return ""

// load_tolerant is THE TOLERANT read under its own name (§6.5): the framing
// walk's recovered record and its report, exactly schema_rt.load.
def load_tolerant(root:string, data:string, report:LoadReport) -> Record?:
    return load(root, data, report)

// load_strict is THE PLAIN NAME, and it is STRICT (§6.5): one call, one
// typed reason. A refusal answers nil and the reason; a clean read answers
// the record and "". Unknown and widened alone are not a refusal.
def load_strict(root:string, data:string, report:LoadReport) -> Record?, string:
    let rec = load(root, data, report)
    let reason = strict_reason(report)
    if reason != "":
        return nil, reason
    return rec, ""
