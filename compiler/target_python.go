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
	"github.com/mas-bandwidth/schema/v2/ir"
)

type pythonTarget struct{}

func (pythonTarget) Names() []string { return []string{"python"} }

// Generate is the packet core (docs/PORT-PYTHON.md, THE PACKET CORE CUT). The
// shared refusal chain every port calls refuses, by name, each construct the
// target does not carry, as it does for every skeleton (docs/PORT-GOOSE.md,
// THE CARRIES PATTERN); the constructs keyed in compiler/carries.go are refused
// by the driver against the python carries set, which compiler/carries_python.go
// registers empty. Then the emitter holds every name to the leg's identifier
// rules, emits every packet declaration's class surface and the wire of the
// carried kinds, and refuses a field or item of any other kind by name, naming
// the design's packet layer.
func (pythonTarget) Generate(u *ir.Unit, _ Options) (map[string][]byte, error) {
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
