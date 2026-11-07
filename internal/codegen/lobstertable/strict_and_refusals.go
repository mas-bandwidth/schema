// The single read's diagnostic convenience entry point and compile-time
// extent-cap check. Runtime refusal details are local diagnostics rather than
// a cross-language taxonomy (docs/SPEC-TABLES.md §6.5).
package lobstertable

import (
	"fmt"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("strict-and-refusals", emitStrictAndRefusals) }

// emitStrictAndRefusals is the compile-time half of the card: it walks the
// file's tables and refuses a construct the strict read cannot enforce, each
// with the name Go's refusal enum gives it (docs/SPEC-TABLES.md §6.5). A clean
// file emits nothing; the strict read itself is runtime, not per-table code.
func emitStrictAndRefusals(g *lobster.Gen) error {
	for _, st := range g.File.Tables {
		if err := refuseStrictUncarried(st); err != nil {
			return err
		}
	}
	return nil
}

func refuseStrictUncarried(st *ir.Struct) error {
	for _, f := range st.Fields {
		if f.ArrayBound > math.MaxInt32 {
			return fmt.Errorf("count_over_extent_cap: %s.%s — a count above the int32 extent cap (docs/SPEC-TABLES.md §2.2, §6.5)", st.Name, f.Name)
		}
	}
	return nil
}
