//go:build schema_leg_lobster

package compiler

// The optionals card registers NO carried construct yet, and this file says
// why rather than letting the registration imply a rule the runtimes do not
// keep.
//
//   - carryOptionalVariableClosure (docs/SPEC-TABLES.md §2.3): the load and
//     save walkers gate every pointer edge on the descriptor's present
//     companion, so `?T` over a nested table is already a shape fact. But the
//     probe unit the central rule is spelled with holds a `*string`, and the
//     lobster target refuses every string/bytes buffer by name
//     (compiler/target_lobster.go, refuseStrings) until its own card. A
//     registration here would make the central gate pass and the target fail
//     one line later, which is the wrong refusal.
//   - carryOptionalMap (docs/SPEC-TABLES.md §2.8): the load walker carries
//     absent, present-empty and present, but save's map branch does not
//     consult the presence companion, and the target refuses every map by
//     name (compiler/tablesmaps.go) until the collections card.
//   - carryRequired (docs/SPEC-TABLES.md §2.3): `| required` is a SLOT fact —
//     load must count MissingRequired for a field the body never named, save
//     must skip its default-elision — and neither walker has a field-level
//     hook.
//
// The exact one-line walker changes that unblock the last two are
// PATHS-PROPOSED in internal/codegen/lobstertable/optionals.go. Until they
// land, the central refusal keeps refusing all three by name, which is the
// truth: lobster cannot read or write them yet.
