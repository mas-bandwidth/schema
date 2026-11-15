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
	if len(u.Tables) == 0 {
		if err := ir.RefuseWideTableKinds(u, "python"); err != nil {
			return nil, err
		}
	}
	if err := pythonRefuseUnported(u); err != nil {
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
	if len(u.Tables) > 0 {
		return pythontable.Generate(u)
	}
	if err := refuseTables(u, "python"); err != nil {
		return nil, err
	}
	return python.Generate(u)
}

// pythonRefuseUnported is the shared refusal chain with one table-frame
// carve-out: a bounded array of TABLE-SIDE unions is what this target carries
// (a union with a general arm), while an array of packet unions waits for the
// packet layer. The shared list is coarser because its test unit has packet
// arms, so it would otherwise speak for a construct Python already carries.
func pythonRefuseUnported(u *ir.Unit) error {
	if err := refuseValueDefaults(u, "python"); err != nil {
		return err
	}
	if err := refuseWasRows(u, "python"); err != nil {
		return err
	}
	if err := refuseTableArms(u, "python"); err != nil {
		return err
	}
	if err := refuseVoidArms(u, "python"); err != nil {
		return err
	}
	if !pythonCarriesUnionArrays(u) {
		if err := refuseUnionArrays(u, "python"); err != nil {
			return err
		}
	}
	if err := refuseBytes(u, "python"); err != nil {
		return err
	}
	return refuseStrings(u, "python")
}

// pythonCarriesUnionArrays reports whether every array of unions the table
// closure declares names a table-side union. A closure with no such array
// answers false, so the shared refusal keeps its own wording.
func pythonCarriesUnionArrays(u *ir.Unit) bool {
	seen := false
	for name := range ir.TableClosure(u) {
		st := ir.ClosureMember(u, name)
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Array == ir.ArrayNone || f.Type.Kind != ir.TNamed {
				continue
			}
			ref, ok := f.Type.Ref.(*ir.Union)
			if !ok {
				continue
			}
			seen = true
			if _, carried := u.TableUnions[ref.Name]; !carried {
				return false
			}
		}
	}
	return seen
}

func init() {
	registerBuiltin(pythonTarget{}, false, true, false, false)
}
