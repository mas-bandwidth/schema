// One built-in target, registered from its own init so a target is one file
// and a new one adds a file (docs/CONTRIBUTING.md, "Adding a language").
//
// The packet wire is available through the ordinary built-in registry.
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
func (pythonTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	surfaces, err := pythontable.ParseSurfaces(opts["surface"])
	if err != nil {
		return nil, err
	}
	if err := pythontable.ValidateSurfaces(u, surfaces); err != nil {
		return nil, err
	}
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
	if err := pythonRefuseUnsupportedLists(u); err != nil {
		return nil, err
	}
	if len(u.Tables) > 0 {
		return pythontable.GenerateSurface(u, surfaces)
	}
	if err := refuseTables(u, "python"); err != nil {
		return nil, err
	}
	return python.Generate(u)
}

// Python's list codec currently carries only a subset of list element shapes.
// Keep the public driver honest for the rest until their codecs land.
func pythonRefuseUnsupportedLists(u *ir.Unit) error {
	var unsupported []string
	for name := range ir.TableClosure(u) {
		st := ir.ClosureMember(u, name)
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if !f.IsList() {
				continue
			}
			carried := !f.Type.Pointer && !f.Type.Optional
			if f.Type.Pointer && !f.Type.Optional && !f.Type.Blob() {
				if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
					carried = true
				}
			}
			if carried {
				switch f.Type.Kind {
				case ir.TBool, ir.TInt:
				case ir.TNamed:
					_, carried = f.Type.Ref.(*ir.Struct)
				default:
					carried = false
				}
			}
			if !carried {
				unsupported = append(unsupported, name+"."+f.Name)
			}
		}
	}
	if len(unsupported) == 0 {
		return nil
	}
	sort.Strings(unsupported)
	return fmt.Errorf("python does not carry the unbounded array element on %s (docs/SPEC-TABLES.md §2.9)", englishList(unsupported))
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
		englishList(fields), englishList(carry), englishList(flags))
}

func init() {
	registerBuiltin(pythonTarget{}, true, true, false, false)
	wasRowTargets = append(wasRowTargets, "python")
}
