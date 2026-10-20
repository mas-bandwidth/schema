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
//   - `?map[K]V` (docs/SPEC-TABLES.md §2.8): absent, present-empty and
//     present are three values. The load walker's MapShape already sets
//     Slot.present; save did not consult it and elided every empty entry
//     list. The map branch in runtime/schema_save.lobster now checks the
//     presence companion first, so an ABSENT map elides while a
//     PRESENT-EMPTY one writes its kind-14 body with count 0. (The
//     compiler-side tablewriter in internal/tablewire elides both, so the
//     test carries the present-empty row's expected bytes itself; the
//     generated Go writer keeps them apart, `if value.TagsPresent { ... }`.)
//   - `| required` (docs/SPEC-TABLES.md §2.3): load counts a required field
//     the body never NAMED in LoadReport.missing_required, names it in
//     reason, and refuses the record at any depth; save skips its
//     default-elision for every `f.required` branch. Both are SLOT facts,
//     not shape facts, so no Shape registration can reach them: this card
//     added the two walker hooks in runtime/load.lobster and
//     runtime/schema_save.lobster (the load hook is a report field, the seen
//     flags and the terminator check; the save hook is `not f.required` on
//     each elision). A required field inside an `if` guard is not demanded
//     here, because FieldDescriptor does not carry the guard expression; the
//     guard surface is a later card's.
//
// The carried-construct registrations stay in compiler/carries_lobster_
// optionals.go, which records why the lobster target still refuses each one
// by name: the target refuses every string blob and every map (and so the
// central probes for these constructs) until the collections and type-wire
// cards lift those refusals.
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
