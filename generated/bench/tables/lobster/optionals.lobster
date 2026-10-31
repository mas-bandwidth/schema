// optionals.lobster — the Lobster table runtime's OPTIONALS surface
// (docs/PORT-LOBSTER.md, THE CARD MAP: schema-lobster-vt-optionals).
//
// The reachable optionals facts are the walkers' own and need no shape here:
// a `?T` field rides while its Slot.present is true (§2.3), an optional array
// writes its presence and its used extent, and the load's MapShape sets
// present for a `?map[K]V` (§2.8). The enum-keyed array's key registration
// is emitted by lobstertable/descriptors.go from the descriptor facts
// (§3.2), beside the keyed Shape this runtime family already registers.
//
// `| required` and the present-empty `?map[K]V` are SLOT facts no Shape
// registration can reach (docs/SPEC-TABLES.md §2.3, §2.8), so this card added
// their walker hooks: runtime/load.lobster reports LoadReport.missing_required
// and refuses the record at any depth, and runtime/schema_save.lobster
// consults the map's presence companion and skips default-elision for a
// required field. This file keeps the presence count the test names them
// with.
// SPDX-License-Identifier: NONE — emitted output, yours under terms of your choice.

import .schema_tables

namespace schema_rt

// present_optionals counts the present `?T`/`?map` slots of one record, in
// descriptor order: the reachable presence facts (docs/SPEC-TABLES.md §2.3),
// used by TestLobsterOptionals to name the construct with its count.
def present_optionals(rec:Record) -> int:
    var n = 0
    for(rec.fields) sl:
        if sl.fd.present != "" and sl.present:
            n++
    return n
