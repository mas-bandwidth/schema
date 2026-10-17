//go:build schema_leg_luau

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg luau`; docs/PORT-LUAU.md is the port's
// design. The schema_leg_luau tag keeps this target out of the default nine
// until the packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_luau` is `schema generate --lang luau`. Drop the
// tag when the port joins the builtins.
package compiler

import (
	"maps"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luauTarget struct{}

func (luauTarget) Names() []string { return []string{"luau"} }

// Generate is the type surface (one module per schema file) and, for a unit
// that declares tables, the table surface (one <Base>Table.luau per file
// with tables) — the descriptors and constructors, no codec yet
// (docs/PORT-LUAU.md, THE CARD MAP). A construct the table surface does not
// describe is refused by luautable, by name.
func (luauTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	files, err := luau.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	return files, nil
}

func init() {
	registerBuiltin(luauTarget{}, false, false, false, false)
}
