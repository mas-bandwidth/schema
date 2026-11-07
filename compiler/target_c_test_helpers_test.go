package compiler

import "github.com/mas-bandwidth/schema/v2/ir"

// generateCUnqualified is the C emitters' output in the schema's own
// spellings, before internal/codegen/cqualify gives every name its package.
// The emitter-shape tests read this form: what a test pins there is WHAT is
// emitted, and the package spelling is cqualify's own tests' business.
func generateCUnqualified(u *ir.Unit) (map[string][]byte, error) {
	return emitC(u, nil)
}
