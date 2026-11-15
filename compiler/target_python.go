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
	"fmt"
	"sort"

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
	if err := refuseUnported(u, "python"); err != nil {
		return nil, err
	}
	if err := pythonRefusePacketUnionArrays(u); err != nil {
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

// pythonRefusePacketUnionArrays is this target's one addition to the shared
// chain. Python registers as a union-array carrier (carries_python_unions.go),
// so refuseUnported's union-array step passes: an array of TABLE-SIDE unions
// (a union with a general arm) is what the table frame carries. An array of
// PACKET unions, whose arms are all packet types, waits for the packet layer
// (P4), so it is named here.
func pythonRefusePacketUnionArrays(u *ir.Unit) error {
	var fields []string
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
			if _, carried := u.TableUnions[ref.Name]; !carried {
				fields = append(fields, name+"."+f.Name)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	return fmt.Errorf("unit declares an array of packet unions in a table closure (%s) — the python table frame carries arrays of table-side unions, and an array of a union whose arms are all packet types is the packet layer's named follow-on; wrap the union in a table and declare an array of that (docs/PORT-PYTHON.md, docs/SPEC-TABLES.md §2.6, §11)",
		englishList(fields))
}

func init() {
	registerBuiltin(pythonTarget{}, false, true, false, false)
}
