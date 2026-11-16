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
<<<<<<< HEAD
	if err := refuseNonPointerLists(u); err != nil {
=======
	if err := pythonRefuseUnsupportedLists(u); err != nil {
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
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
// Python's list codec currently carries only a subset of list element shapes.
// Keep the public driver honest for the rest until their codecs land.
func pythonRefuseUnsupportedLists(u *ir.Unit) error {
	var unsupported []string
	for name := range ir.TableClosure(u) {
		st := ir.ClosureMember(u, name)
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
<<<<<<< HEAD
			if f.IsList() && (!f.Type.Pointer || f.Type.Blob()) {
				fields = append(fields, name+"."+f.Name)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	sort.Strings(fields)
	carry, flags := carriers(listTargets)
	return fmt.Errorf("unit declares an unbounded array in a table closure (%s): a []T is %s only today, and the python form is a named follow-on. Generate with %s, or declare the array at a bound, [..N]T, which is the same bytes (docs/SPEC-TABLES.md §2.9, §11, §15)",
		englishList(fields), englishList(carry), englishList(flags))
=======
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
				case ir.TBool, ir.TInt, ir.TFixed, ir.TBits, ir.TFloat32, ir.TFloat64:
				case ir.TNamed:
					switch f.Type.Ref.(type) {
					case *ir.Struct, *ir.Enum, *ir.Flags, *ir.Union:
					default:
						carried = false
					}
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
>>>>>>> f21ed2ad0370caa9b8a18426125e142bc259a48f
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
