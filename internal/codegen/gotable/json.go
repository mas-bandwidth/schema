package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateJsonFiles emits the TEXT form (docs/SPEC-TABLES.md §16). Not yet
// implemented; the surface lands in its own pass.
func generateJsonFiles(u *ir.Unit, closure map[string]bool, home string) (map[string][]byte, error) {
	_, _, _ = u, closure, home
	return map[string][]byte{}, nil
}
