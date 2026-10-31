package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luatable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Testgen returns a unit's generated round-trip and damage tests for target,
// keyed by output file name (docs/SPEC-TABLES.md §16.11). Like Generate it
// writes nothing. Go carried it first; Lua (one <Base>Table_gen_test.lua per
// schema file, run under the pinned interpreter) and Squirrel carry it beside
// Go; every other target is refused here, once, by name, so no other emitter is
// edited.
func (c *Compiler) Testgen(u *ir.Unit, target string) (map[string][]byte, error) {
	switch target {
	case "go":
		return gotable.Testgen(u)
	case "lua":
		return luatable.Testgen(u)
	case "squirrel":
		return squirreltable.Testgen(u)
	}
	return nil, fmt.Errorf("target %q refuses testgen — Go, Lua and Squirrel carry the generated round-trip and damage tests; no other target emits them until its turn (docs/SPEC-TABLES.md §16.11)", target)
}
