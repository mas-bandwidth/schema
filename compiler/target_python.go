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
// ValidateOptions names surface conflicts before the shared forms refusal.
func (pythonTarget) ValidateOptions(u *ir.Unit, opts Options) error {
	surfaces, err := pythontable.ParseSurfaces(opts["surface"])
	if err != nil {
		return err
	}
	if err := pythontable.ValidateSurfaces(u, surfaces); err != nil {
		return err
	}
	return pythontable.ValidateAvailableSurfaces(surfaces)
}

func (pythonTarget) Generate(u *ir.Unit, opts Options) (map[string][]byte, error) {
	return pythonGenerate(u, opts, nil)
}

func (pythonTarget) GenerateLineage(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error) {
	return pythonGenerate(u, opts, lineage)
}

func pythonGenerate(u *ir.Unit, opts Options, lineage *FixedLineage) (map[string][]byte, error) {
	surfaces, err := pythontable.ParseSurfaces(opts["surface"])
	if err != nil {
		return nil, err
	}
	if err := pythontable.ValidateSurfaces(u, surfaces); err != nil {
		return nil, err
	}
	if err := pythontable.ValidateAvailableSurfaces(surfaces); err != nil {
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
		return pythontable.GenerateSurfaceLineage(u, surfaces, pythonTableLineage(u, lineage))
	}
	if err := refuseTables(u, "python"); err != nil {
		return nil, err
	}
	out, err := python.Generate(u)
	if err != nil {
		return nil, err
	}
	if surfaces.View {
		if err := pythontable.AddView(u, out); err != nil {
			return nil, err
		}
	}
	return out, nil
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
}

func init() {
	registerBuiltin(pythonTarget{}, true, true, false, false)
	wasRowTargets = append(wasRowTargets, "python")
	// Python carries string, bytes and flags defaults on the FORM-1 table
	// wire in every field shape: a default lives on a plain field of a table
	// or a type, and the parser refuses one on a container in every target
	// (TestValueDefaultsEveryShape).
	valueDefaultTargets = append(valueDefaultTargets, "python")
}
