// optionals.lobster — the Lobster table runtime's OPTIONALS surface
// (docs/PORT-LOBSTER.md, THE CARD MAP: schema-lobster-vt-optionals).
//
// The reachable optionals facts are the walkers' own and need no shape here:
// a `?T` field rides while its Slot.present is true (§2.3), an optional array
// writes its presence and its used extent, and the load's MapShape sets
// present for a `?map[K]V` (§2.8). The enum-keyed array's key registration
// is emitted by lobstertable/optionals.go from the descriptor facts
// (§3.2), beside the keyed Shape this runtime family already registers.
//
// NOT carried, and why (docs/SPEC-TABLES.md §2.3, §2.8): `| required` is a
// SLOT fact — load must count an unnamed required field (LoadReport has no
// such counter yet) and save must skip its default-elision — and neither
// walker has a field-level hook; and save's map branch does not consult the
// presence companion, so an absent and a present-empty optional map write the
// same bytes while the generated Go writer keeps them apart. Both need the
// walker changes recorded in internal/codegen/lobstertable/optionals.go
// (PATHS-PROPOSED); no shape can reach them.
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
