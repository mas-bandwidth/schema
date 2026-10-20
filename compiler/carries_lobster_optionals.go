//go:build schema_leg_lobster

package compiler

// The optionals card registers NO carried construct, and this file records
// the exact evidence rather than letting a registration imply a rule the
// target's own refusals do not keep. The walker side now does: `| required`
// is counted and refused by runtime/load.lobster (LoadReport.missing_required)
// and always written by runtime/schema_save.lobster; the slow `if`-guard half
// of §2.3 is not (FieldDescriptor carries no guard expression), and the
// present-empty `?map[K]V` save is the map branch's presence check.
//
//   - carryRequired (docs/SPEC-TABLES.md §2.3): not carried. The central
//     probe the compiler gate uses is `name string(32) | required`, and the
//     lobster target refuses every string blob by name
//     (compiler/target_lobster.go, refuseUnported -> refuseStrings) until the
//     type-wire card. Registering it would make the central gate pass and the
//     target refuse one line later.
//   - carryOptionalVariableClosure (§2.3): the load and save walkers do gate
//     every pointer edge on the presence companion, so `?T` over a nested
//     table is already a shape fact. But the central probe holds a `*string`
//     over the closure, and the lobster target refuses every string blob by
//     name (refuseUnported -> refuseStrings). Registering it would make the
//     central gate pass and the target refuse one line later.
//   - carryOptionalMap (§2.8): the load walker carries absent, present-empty
//     and present, and save's map branch now consults the presence companion,
//     but the target refuses every map by name (compiler/target_lobster.go,
//     refuseMaps) until the collections card's refusal is lifted.
//     Registering it would fail compiler.TestMapPresenceRefusedOffGo for
//     lobster.
//
// The target refusals that block all three are outside this card's PATHS.
// Until they lift, the central refusal keeps refusing all three by name,
// which is the truth: the descriptors and the emitted table runtime describe
// the constructs, but `schema generate --lang lobster` still refuses a unit
// that uses a string or a map.
