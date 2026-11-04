package lobstertable

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Reflection shares the schema's namespace. Claim synthesized symbols before
// emission, including collisions between two synthesized accessor names.
func checkReflectionNames(u *ir.Unit) error {
	claims := map[string]string{}
	for name := range u.DeclFile {
		claims[name] = "declaration " + name
	}
	for name := range u.Tables {
		claims[name] = "table " + name
	}
	for name := range u.TableUnions {
		claims[name] = "union " + name
	}
	claim := func(name, owner string) error {
		if previous, ok := claims[name]; ok {
			return fmt.Errorf("lobster: generated reflection name %s for %s collides with %s; rename the declaration or field", name, owner, previous)
		}
		claims[name] = owner
		return nil
	}
	// Packet enum and flags members occupy this same namespace. Claim their
	// flattened spellings before the view's descriptors and accessors.
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Enum:
				for _, variant := range append([]string{"None"}, d.Variants...) {
					if err := claim(lobster.VariantName(d.Name, variant), d.Name+"."+variant); err != nil {
						return err
					}
				}
			case *ir.Flags:
				for _, variant := range d.Variants {
					if err := claim(d.Name+variant, d.Name+"."+variant); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := claim("UnitView", "unit registry"); err != nil {
		return err
	}
	structs := map[string]*ir.Struct{}
	for name, d := range u.Structs {
		structs[name] = d
	}
	for name, d := range u.Tables {
		structs[name] = d
	}
	owners := map[string]bool{}
	for name := range structs {
		owners[name] = true
	}
	for name := range u.Unions {
		owners[name] = true
	}
	for name := range u.TableUnions {
		owners[name] = true
	}
	names := make([]string, 0, len(owners))
	for name := range owners {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := claim(name+"Descriptor", name); err != nil {
			return err
		}
		if st := structs[name]; st != nil {
			for _, f := range st.Fields {
				if f.KeyEnumRef == nil {
					continue
				}
				for _, suffix := range []string{"Slot", "At", "Each"} {
					if err := claim(name+ir.GoExportName(f.Name)+suffix, name+"."+f.Name); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
