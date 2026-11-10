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

// Generate emits the unit's table surface beside the packet skeleton. A unit
// that declares tables goes to pythontable, whose generated classes and form-1
// runtime carry the fixed and variable classes; a field whose type is a packet
// type is refused there by name until the packet layer lands. A table-free
// unit takes the skeleton path, where the shared refusal chain refuses, by
// name, each construct the target does not carry (docs/PORT-PYTHON.md, THE
// CARD MAP: P3). A unit declaring both is a table unit until the packet layer
// joins the two halves.
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
