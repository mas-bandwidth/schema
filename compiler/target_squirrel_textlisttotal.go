// THE TEXT LIST AND ITS TOTAL (docs/SPEC-TABLES.md §2.13) on the Squirrel
// target. The registry entry is conditional because target_squirrel.go is
// behind schema_leg_squirrel: this file also carries the descriptor facts the
// shared descriptor does not (textList, total), which runtime/save.nut,
// runtime/load.nut and runtime/json.nut read. The facts are emitted through
// squirreltable's surface hook, the way every optional form fact joins that
// module (internal/codegen/squirreltable/json.go).
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	squirreltable.RegisterSurface(squirreltable.Surface{Name: "textlisttotal", Table: textListTotalTable})
	if squirrelBuilt() {
		registerListCarrier("squirrel")
		if _, ok := targetCarries["squirrel"]; ok {
			targetCarries["squirrel"][carryTextList] = struct{}{}
			targetCarries["squirrel"][carryTextListTotal] = struct{}{}
		} else {
			registerCarries("squirrel", carryTextList, carryTextListTotal)
		}
	}
}

// squirrelBuilt reports whether target_squirrel.go registered its built-in in
// this build (the schema_leg_squirrel tag).
func squirrelBuilt() bool {
	for _, g := range builtins() {
		for _, name := range g.Names() {
			if name == "squirrel" {
				return true
			}
		}
	}
	return false
}

// textListTotalTable emits the two facts a text list adds to its field
// descriptor: textList marks the array as §2.13's list of text (the wire and
// text walks spell it as an array of the element record), and total is the
// sum bound (§2.13). Every other field descriptor is untouched.
func textListTotalTable(m *squirreltable.Module, st *ir.Struct) {
	for i, f := range st.Fields {
		if !f.IsTextList() {
			continue
		}
		target := fmt.Sprintf("_M.%sDescriptor.fields[%d]", st.Name, i)
		m.Printf("%s.textList <- true;\n", target)
		if f.TextTotal > 0 {
			m.Printf("%s.total <- %d;\n", target, f.TextTotal)
		}
	}
}
