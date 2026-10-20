// Package lobstertable's optionals surface (docs/PORT-LOBSTER.md, THE CARD
// MAP: schema-lobster-vt-optionals).
//
// What this file carries, and where the rule lives:
//
//   - `[E]T` (docs/SPEC-TABLES.md §3.2): the descriptor already spells the
//     shape as "keyed" (lobstertable.fieldDesc), but it does not carry the
//     KEY ENUM. This Part emits the one `schema_rt.keyed_keys(owner, field,
//     variants)` call the load walker's keyed registry needs, from
//     FieldDescriptor facts and ir.Field.KeyEnumRef, so a generated unit no
//     longer registers its keyed keys by hand.
//   - `?T`, optional nested values and optional arrays (docs/SPEC-TABLES.md
//     §2.3): the descriptor's `present` companion and the load/save walkers
//     already carry these; this card's test holds them to the Go engine.
//   - `?map[K]V` (docs/SPEC-TABLES.md §2.8): the load walker carries absent,
//     present-empty and present (MapShape sets Slot.present through body_at);
//     save does NOT: its map branch in runtime/schema_save.lobster elides on
//     `length(sl.entries) == 0` without consulting `f.present`, so an
//     ABSENT and a PRESENT-EMPTY optional map write the same bytes.
//   - `| required` (docs/SPEC-TABLES.md §2.3): NOT reachable here. It is a
//     property of the field's SLOT, not of its shape, and neither walker
//     exposes a field-level hook: load must count a required field the body
//     never NAMED (MissingRequired + report.reason) and save must skip its
//     default-elision when `f.required`. This card therefore does not
//     register `carryRequired` for lobster: the central refusal keeps
//     refusing it by name, which is the truth until the hooks land.
//
// PATHS-PROPOSED (files this card does not edit; the walkers belong to
// table-wire-load and table-wire-save):
//
//   - runtime/load.lobster, in body_at's field loop, where `named` is already
//     known: before `shape(shape_key(f)).field(...)`, mark the slot named;
//     after the body's terminator, for each field with `f.required` that was
//     not named, report.missing_required++ and set report.reason to
//     "missing_required: " + f.name (off-branch fields stay undemanded).
//   - runtime/schema_save.lobster, in encode_field's "map" branch: check
//     `f.present != "" and not sl.present` first (absent elides), then write
//     a present-empty map's kind-14 body with count 0 instead of returning on
//     `length(sl.entries) == 0`.
//   - runtime/schema_save.lobster, at the final `if save_is_default(...)`:
//     `if not f.required and save_is_default(f, sl.cell): return`, so a
//     required field always rides. A required field the writer does not
//     reach (an untaken branch) is already not demanded.
//
// With those three hooks a later attempt adds the runtime file and extends
// TestLobsterOptionals' probe units to `| required` and the present-empty
// optional map, holding all three to the Go engine.
package lobstertable

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("optionals", emitOptionals)
}

// emitOptionals emits this module's `[E]T` key registrations
// (docs/SPEC-TABLES.md §3.2; docs/PORT-LOBSTER.md, THE LOAD, "Not carried
// yet, by name"): the descriptor names the shape but not the key enum, so the
// generated module hands the walker the facts it needs once, at load.
func emitOptionals(g *lobster.Gen) error {
	var structs []*ir.Struct
	structs = append(structs, g.File.Tables...)
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok {
			structs = append(structs, st)
		}
	}
	emitted := false
	for _, st := range structs {
		for _, f := range st.Fields {
			if f.KeyEnumRef == nil {
				continue
			}
			if !emitted {
				g.Pf("// ENUM-KEYED ARRAYS (docs/SPEC-TABLES.md §3.2): the descriptor names\n")
				g.Pf("// the shape but not its key enum, so the module registers the keys here\n")
				g.Pf("// (docs/PORT-LOBSTER.md, THE LOAD).\n")
				emitted = true
			}
			g.Pf("schema_rt.keyed_keys(%s, %s, [", lobster.StringLit([]byte(st.Name)), lobster.StringLit([]byte(f.Name)))
			for i := range f.KeyEnumRef.Variants {
				if i > 0 {
					g.Pf(", ")
				}
				g.Pf("schema_rt.EnumVariant { name: %s, id: %s }",
					lobster.StringLit([]byte(f.KeyEnumRef.Variants[i])),
					lobster.IntLit(new(big.Int).SetUint64(ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))))
			}
			g.Pf("]::schema_rt.EnumVariant)\n")
		}
	}
	if emitted {
		g.Pf("\n")
		g.Runtimes = append(g.Runtimes, "load")
	}
	return nil
}
