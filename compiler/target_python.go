//go:build schema_leg_python

// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// Scaffolded by `schema new-leg python`; docs/PORT-PYTHON.md is the port's
// design. The schema_leg_python tag keeps this target out of the builtins
// until the packet surface has claims in compiler/*_test.go;
// `go build -tags schema_leg_python` is `schema generate --lang python`. The
// tag is dropped when the port joins the builtins (P4, the packet wire).
package compiler

import (
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type pythonTarget struct{}

func (pythonTarget) Names() []string { return []string{"python"} }

// Generate selects the table frame for a table unit and the packet core
// otherwise. Each emitter holds the identifier rules and names any construct
// outside its carried surface (docs/PORT-PYTHON.md).
func (pythonTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
	if len(u.Tables) > 0 {
		return pythontable.Generate(u)
	}
	if err := ir.RefuseWideTableKinds(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseUnported(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseOptionalArrays(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseMaps(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseLists(u, "python"); err != nil {
		return nil, err
	}
	if err := refuseTables(u, "python"); err != nil {
		return nil, err
	}
	return python.Generate(u)
}

func init() {
	registerBuiltin(pythonTarget{}, false, false, false, false)
}
