// The cooked form: Open and the header check (docs/SPEC-TABLES.md §7, §7.1;
// docs/PORT-LOBSTER.md, THE CARD MAP). The per-table Open names the unit's
// build version and the root's own size and alignment; the check itself lives
// once in runtime/cook.lobster (internal/tablecook/cook.go, ReadHeader).
package lobstertable

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("cook", emitCook) }

// emitCook joins every table module with the cooked form's read surface: one
// <Name>Open per table, which matches the header and points at the region
// (docs/SPEC-TABLES.md §7). Cooking is an optimization: Open does no walk, no
// fix-up and no allocation.
func emitCook(g *lobster.Gen) error {
	emitted := false
	candidates := append([]*ir.Struct{}, g.File.Tables...)
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok {
			candidates = append(candidates, st)
		}
	}
	seen := map[string]bool{}
	for _, st := range candidates {
		if seen[st.Name] || st.IsMapEntry() || !st.CarriesForm("cook") {
			continue
		}
		seen[st.Name] = true
		ml := ir.RecordLayout(g.Unit, st)
		g.Pf("// %sOpen matches the cooked header for %s (docs/SPEC-TABLES.md §7.1):\n", st.Name, st.Name)
		g.Pf("// the magic and the order it establishes, the build version, the reserved\n")
		g.Pf("// words, the alignment and both part lengths. It points at the region and\n")
		g.Pf("// allocates nothing; the check lives once in the runtime.\n")
		g.Pf("def %sOpen(data:string) -> bool:\n", st.Name)
		g.Pf("    return schema_rt.cook_open(data, %s, %d, %d)\n\n",
			lobster.IntLit(new(big.Int).SetUint64(ir.BuildVersion(g.Unit))), ml.Align, ml.Size)
		emitted = true
	}
	if emitted {
		g.Import("cook")
	}
	return nil
}
