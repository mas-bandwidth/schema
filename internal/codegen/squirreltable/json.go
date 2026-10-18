package squirreltable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE TEXT FORM'S DESCRIPTOR FACTS (docs/SPEC-TABLES.md §16.2), joined to the
// table module through [RegisterSurface] as the port's design has every form
// join it (docs/PORT-SQUIRREL.md, THE RUNTIME LAYOUT). The wire walks need
// none of these, so the shared descriptor does not carry them; the JSON walk
// (runtime/json.nut) does, and reads each as an optional key:
//
//	frac       a fixed(I, F) or ufixed(I, F) value's F: the text is the value
//	           in WHOLE UNITS, raw / 2^F (§16.2's fixed row)
//	flagNames  a flags mask's variant names, bit i first: the text is the
//	           name array (§16.2's flags row)
//	fmin fmax  a compressed float's declared range, which the read clamps to
//	jsonArray  a table declared `json = "array"`, whose text is its one
//	           field's array (§16.10)
//
// Each fact is set on the field descriptor, its element descriptor and every
// union arm's payload descriptor that has one; no other key is touched.
func init() {
	RegisterSurface(Surface{Name: "json", Table: jsonTable, Module: jsonModule})
}

func jsonTable(m *Module, st *ir.Struct) {
	if st.JsonArray {
		m.Printf("_M.%sDescriptor.jsonArray <- true;\n", st.Name)
	}
	for i, f := range st.Fields {
		jsonFacts(m, f, "_M."+st.Name+"Descriptor.fields["+fmt.Sprint(i)+"]")
	}
}

func jsonModule(m *Module) {
	for _, un := range unions(m.Closure) {
		for i, v := range un.Variants {
			if v.Void() {
				continue
			}
			jsonFacts(m, v.F, "_M."+un.Name+"Descriptor.arms["+fmt.Sprint(i)+"].payload")
		}
	}
}

// jsonFacts emits the facts of one field's VALUE type at target, and at its
// element descriptor when the field is an array (the element shares them).
func jsonFacts(m *Module, f *ir.Field, target string) {
	at := []string{target}
	if f.Array != ir.ArrayNone {
		at = append(at, target+".elem")
	}
	for _, t := range at {
		if f.Type.Kind == ir.TFixed {
			m.Printf("%s.frac <- %d;\n", t, f.Type.FracBits)
		}
		if f.HasFloatRange {
			m.Printf("%s.fmin <- %s;\n%s.fmax <- %s;\n", t, squirrel.Float64Lit(f.FMin), t, squirrel.Float64Lit(f.FMax))
		}
		if isFlags(f) {
			fl := f.Type.Ref.(*ir.Flags)
			names := make([]string, len(fl.Variants))
			for i, v := range fl.Variants {
				names[i] = squirrel.StringLit([]byte(v))
			}
			m.Printf("%s.flagNames <- [%s];\n", t, strings.Join(names, ", "))
		}
	}
}
