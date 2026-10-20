//go:build schema_leg_lobster

package compiler

// The optionals card registers NO carried construct, and this file records
// the exact evidence rather than letting a registration imply a rule the
// runtimes (and the target's own refusals) do not keep.
//
//   - carryRequired (docs/SPEC-TABLES.md §2.3): not carried. `| required` is
//     a SLOT fact, not a shape fact: runtime/load.lobster must count a field
//     the body never NAMED (LoadReport has no missing_required counter at
//     all) and runtime/schema_save.lobster must skip its default-elision.
//     Neither walker exposes a field-level hook, so no runtime/*.lobster file
//     this card may add can reach it. Registering it here would make
//     compiler.TestEachTargetDeclaresWhatItCarries pass while
//     TestLobsterOptionals could not hold the construct to Go.
//   - carryOptionalVariableClosure (§2.3): the load and save walkers do gate
//     every pointer edge on the presence companion, so `?T` over a nested
//     table is already a shape fact. But the central probe the compiler gate
//     uses holds a `*string` over the closure, and the lobster target refuses
//     every string blob by name (compiler/target_lobster.go, refuseUnported ->
//     refuseStrings). Registering it would make the central gate pass and the
//     target refuse one line later.
//   - carryOptionalMap (§2.8): the load walker carries absent, present-empty
//     and present, but save's map branch does not consult the presence
//     companion, and the target refuses every map by name
//     (compiler/target_lobster.go, refuseMaps) until the collections card's
//     refusal is lifted. Registering it would fail
//     compiler.TestMapPresenceRefusedOffGo for lobster.
//
// The walker changes that unblock the last two are PATHS-PROPOSED in
// internal/codegen/lobstertable/optionals.go; the target refusals that block
// the other two are outside this card's PATHS. Until they land the central
// refusal keeps refusing all three by name, which is the truth: lobster
// cannot read or write them yet.
