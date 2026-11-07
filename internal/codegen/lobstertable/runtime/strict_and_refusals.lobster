// The single read with its local diagnostic returned beside the value.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .load
import .schema_tables

namespace schema_rt

// load_strict is the strict read in one call (§6.5): a refusal answers nil
// and the reason; a clean read answers the record and "". Unknown and
// widened alone are not a refusal.
def load_strict(root:string, data:string, report:LoadReport) -> Record?, string:
    let rec = load(root, data, report)
    let reason = strict_reason(report)
    if reason != "":
        return nil, reason
    return rec, ""
