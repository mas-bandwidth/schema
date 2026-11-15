//go:build schema_leg_python

package compiler

// THE OPTIONAL FIELD KIND carries no construct from the §2 carry vocabulary
// (docs/SPEC-TABLES.md §2.3). A `?T` over a fixed closure is an ordinary table
// field: absence is the field not riding, so the shared refusal chain has no
// key for it to lift and none is registered here.
//
// The §2.3 key that DOES name an optional shape is optional-variable-closure,
// an optional whose by-value closure holds a pointer. The table runtime's
// presence gate numbers no node for an absent value, but this cut does not
// claim that construct at the compiler gate: it stays refused by name until
// its own registration. Optional arrays (`?[N]T`, `?[..N]T`) likewise stay
// refused by name: the array kind their present framing rides is a later
// layer, and the optional module already delegates through the descriptor's
// inner codec, so it will carry them once that kind registers.
