// Defaults, renames, deprecation and the build version on the Lobster table
// surface (docs/SPEC-TABLES.md §2.15, §5, §20; docs/PORT-LOBSTER.md, THE CARD
// MAP). The defaults and the `was` aliases are descriptor facts the surface
// already emits: a field's id is the hash of its old name (§5), and the
// descriptor carries the declared default the load's new_record lays in
// (§4). This file adds the two facts the descriptor does not carry — the
// unit's BuildVersion (§20) and which enum variants are retired in place
// (§2.15) — and joins the runtime file that reads the second.
package lobstertable

import (
	"math/big"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("defaults-renames", emitDefaultsRenames) }

// emitDefaultsRenames writes the unit's build version and its deprecated
// variants into the unit's FIRST table module, so a program importing every
// module of the unit still defines BuildVersion once (Lobster has one
// namespace per package, and every module of a unit declares it). The value
// is the reference's own digest (ir.BuildVersion, §20.1) rendered as the hex
// bit pattern (docs/PORT-LOBSTER.md, THE NUMBER MODEL).
//
// A retired variant keeps its name and its id and is marked in place
// (§2.15): deprecate_variant records it for the enum shape
// defaults_renames.lobster installs. A unit with no retired variant emits
// neither call nor import, so every other unit is byte-identical to before.
func emitDefaultsRenames(g *lobster.Gen) error {
	mods := moduleFiles(g.Unit, closure(g.Unit))
	first := ""
	for _, f := range g.Unit.Files {
		if mods[f.Base] {
			first = f.Base
			break
		}
	}
	if g.File.Base != first {
		return nil
	}
	g.Pf("// BuildVersion is the unit's build version (docs/SPEC-TABLES.md §20):\n")
	g.Pf("// the one digest a block carries, a sender announces and the cook names.\n")
	g.Pf("let BuildVersion = %s\n\n", lobster.IntLit(new(big.Int).SetUint64(ir.BuildVersion(g.Unit))))

	names := make([]string, 0, len(g.Unit.Enums))
	for name := range g.Unit.Enums {
		names = append(names, name)
	}
	sort.Strings(names)
	emitted := false
	for _, name := range names {
		e := g.Unit.Enums[name]
		for i := range e.Variants {
			if i >= len(e.VariantDeprecated) || !e.VariantDeprecated[i] {
				continue
			}
			if !emitted {
				g.Pf("// A variant retired in place keeps its name and id; a reader yields\n")
				g.Pf("// the field default and a strict write refuses it (§2.15).\n")
				emitted = true
			}
			g.Pf("schema_rt.deprecate_variant(%s, %s) // %s.%s | deprecated\n",
				lobster.StringLit([]byte(name)),
				lobster.IntLit(new(big.Int).SetUint64(ir.TableWireId(e.VariantWireName(i)))),
				name, e.Variants[i])
		}
	}
	if emitted {
		g.Pf("\n")
		g.Import("defaults_renames")
	}
	return nil
}
