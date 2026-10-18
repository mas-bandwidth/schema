package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Testgen returns a unit's generated round-trip and damage tests for target,
// keyed by output file name (docs/SPEC-TABLES.md §16.11). Like Generate it
// writes nothing. GO CARRIES IT FIRST: every other target is refused here,
// once, by name, so no other emitter is edited.
func (c *Compiler) Testgen(u *ir.Unit, target string) (map[string][]byte, error) {
	if target != "go" {
		return nil, fmt.Errorf("target %q refuses testgen — Go carries the generated round-trip and damage tests first; no other target emits them until its turn (docs/SPEC-TABLES.md §16.11)", target)
	}
	return gotable.Testgen(u)
}
