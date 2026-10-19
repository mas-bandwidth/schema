//go:build schema_leg_squirrel

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg squirrel`. The schema_leg_squirrel tag keeps
// this target out of the default nine until the packet surface has claims in
// compiler/*_test.go; `go build -tags schema_leg_squirrel` is `schema generate
// --lang squirrel` (make/squirrel.mk builds it as build/schema-squirrel). Drop
// the tag when the port joins the builtins (docs/PORT-SQUIRREL.md, THE CARD
// MAP).
package compiler

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type squirrelTarget struct{}

func (squirrelTarget) Names() []string { return []string{"squirrel"} }

// Generate emits the TYPE surface (internal/codegen/squirrel) for every unit
// and the TABLE surface (internal/codegen/squirreltable) for a unit that
// declares tables. Neither carries a codec yet; the refusals below are the
// skeleton's, each lifted by the card that carries its construct
// (docs/PORT-SQUIRREL.md, THE REGISTER).
func (squirrelTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if err := ir.RefuseWideTableKinds(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseWideText(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "squirrel"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "squirrel"); err != nil {
		return nil, err
	}
	files, err := squirrel.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		if _, dup := files[name]; dup {
			return nil, fmt.Errorf("generated file %s is claimed twice — the table module %s.nut and a schema file of that base collide; rename the file (docs/PORT-SQUIRREL.md)", name, squirreltable.ModuleName(u))
		}
		files[name] = data
	}
	return files, nil
}

func init() {
	registerBuiltin(squirrelTarget{}, false, false, false, false)
}
