//go:build schema_leg_goose

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg goose`; docs/PORT-GOOSE.md is the port's
// design. The schema_leg_goose tag keeps this target out of the default nine
// until the packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_goose` is `schema generate --lang goose`. Drop
// the tag when the port joins the builtins.
package compiler

import (
	"maps"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type gooseTarget struct{}

func (gooseTarget) Names() []string { return []string{"goose"} }

// Generate is the type surface (one module per schema file) and, for a unit
// that declares tables, the table surface (one <Base>Table.goose per file
// with tables) — the descriptors and constructors, no codec yet
// (docs/PORT-GOOSE.md, THE CARD MAP). A construct either surface cannot emit
// is refused by name.
func (gooseTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	files, err := goose.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := goosetable.Generate(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	return files, nil
}

func init() {
	registerBuiltin(gooseTarget{}, false, false, false, false)
}
