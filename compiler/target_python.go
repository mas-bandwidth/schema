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
	if err := refuseNonPointerLists(u); err != nil {
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

<<<<<<< HEAD
// refuseNonPointerLists admits []*T pointer lists into the Python table leg
// while preserving the named refusal for uncarried []T value lists (and []*blob).
func refuseNonPointerLists(u *ir.Unit) error {
	var fields []string
	for name := range ir.TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
=======
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
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
<<<<<<< HEAD
			if f.IsList() && (!f.Type.Pointer || f.Type.Blob()) {
=======
			if f.Array == ir.ArrayNone || f.Type.Kind != ir.TNamed {
				continue
			}
			ref, ok := f.Type.Ref.(*ir.Union)
			if !ok {
				continue
			}
			if _, carried := u.TableUnions[ref.Name]; !carried {
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
				fields = append(fields, name+"."+f.Name)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
<<<<<<< HEAD
	carry, flags := carriers(listTargets)
	return fmt.Errorf("unit declares an unbounded array in a table closure (%s): a []T is %s only today, and the python form is a named follow-on. Generate with %s, or declare the array at a bound, [..N]T, which is the same bytes (docs/SPEC-TABLES.md §2.9, §11, §15)",
=======
	// the shared refusal's wording and carrier list, less python itself,
	// which carries only the table-side form
	var others []string
	for _, t := range unionArrayTargets {
		if t != "python" {
			others = append(others, t)
		}
	}
	carry, flags := carriers(others)
	return fmt.Errorf("unit declares an array of unions in a table closure (%s) whose arms are all packet types — such an array is %s only today, and the python form is the packet layer's named follow-on (the python table frame carries arrays of table-side unions); generate with %s, or wrap the union in a table and declare an array of that (docs/SPEC-TABLES.md §2.6, §11)",
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
		englishList(fields), englishList(carry), englishList(flags))
}

func init() {
<<<<<<< HEAD
	registerBuiltin(pythonTarget{}, false, false, false, false)
=======
	registerBuiltin(pythonTarget{}, false, true, false, false)
>>>>>>> 5043610a8358c251defbef6d002d940708010d41
}
