package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The Squirrel target carries the `integrity` trailer (docs/SPEC-TABLES.md
// §2.14), so the central refusal in generate.go does not block it. The carry
// is registered only when the tagged build has registered the squirrel target
// (target_squirrel.go), so an untagged build's target set and carry set still
// match.
//
// The variable form's runtime (runtime/save.nut, runtime/load.nut) reads the
// check off the table descriptor, so this file also emits the descriptor key:
// the table's declared integrity name (docs/SPEC-TABLES.md §2.14). It joins
// the table module through squirreltable's surface rather than editing the
// descriptor's own file, so the keys there stay in the one file that owns
// them (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT).
func init() {
	for _, g := range builtins() {
		if g.Names()[0] == "squirrel" {
			registerCarries("squirrel", carryIntegrity)
			break
		}
	}
	squirreltable.RegisterSurface(squirreltable.Surface{
		Name: "integrity",
		Table: func(m *squirreltable.Module, st *ir.Struct) {
			if st.Integrity != "" {
				m.Printf("_M.%sDescriptor.integrity <- %s;\n", st.Name, squirrel.StringLit([]byte(st.Integrity)))
			}
		},
	})
}
