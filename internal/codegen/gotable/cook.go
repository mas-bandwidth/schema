package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateCookFiles emits <Base>Cook.go for the cooked form (docs/SPEC-TABLES.md
// §7). Not yet implemented; the surface lands in its own pass.
func generateCookFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	_, _ = u, blocks
	return map[string][]byte{}, nil
}
