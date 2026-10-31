// The strict read and every refusal by Go's name (docs/SPEC-TABLES.md §6.5,
// §2.3; docs/PORT-LOBSTER.md, THE LOAD). The read side lives once in
// runtime/strict_and_refusals.lobster, emitted by the runtime glob: the plain
// load is the strict read, the tolerant walk is load_tolerant, and every
// read-time refusal carries Go's TableRefuseReason spelling. This file joins
// the descriptor emitter through Register and refuses, at compile time and by
// the same name, the one construct the Lobster walk does not carry: a
// collection bound above the int32 extent cap (count_over_extent_cap, §2.2).
// A `| required` field is carried: the load counts and names
// missing_required at any depth and the save never elides one (§2.3,
// runtime/load.lobster and runtime/schema_save.lobster).
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
