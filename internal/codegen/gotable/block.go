package gotable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateBlockFiles emits <Base>Block.go for the block form (docs/SPEC-TABLES.md
// §19). Not yet implemented; the surface lands in its own pass.
func generateBlockFiles(u *ir.Unit, blocks *ir.BlockUnit) (map[string][]byte, error) {
	_, _ = u, blocks
	return map[string][]byte{}, nil
}
