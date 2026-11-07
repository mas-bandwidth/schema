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
//	guard      a field inside an `if` group: its branch condition, composed
//	           through nesting, as [member, want] pairs of the same body's
//	           bools; the write drops the field while any pair does not hold
//	           (§16.2, a guarded group's untaken side has no text to carry),
//	           the composition the Go engine's guard walk makes (re-derived
//	           here from the IR by [jsonGuards], since an emitter never
//	           imports the engine, the oracle it is checked against) and
//	           Go's tableJsonGuardHolds evaluates
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
	guards := jsonGuards(st)
	for i, f := range st.Fields {
		target := "_M." + st.Name + "Descriptor.fields[" + fmt.Sprint(i) + "]"
		jsonFacts(m, f, target)
		if terms, guarded := guards[f.Name]; guarded {
			jsonGuard(m, st, terms, target)
		}
	}
}

// jsonGuardTerm is one conjunct of a guarded field's branch condition: a bool
// field of the same body, and the value it must hold.
type jsonGuardTerm struct {
	Field string
	Want  bool
}

// jsonGuards maps each guarded field's name to its branch condition, composed
// through nesting (outermost first) from the IR's branch tree. A field with no
// entry is unguarded and always rides.
func jsonGuards(st *ir.Struct) map[string][]jsonGuardTerm {
	out := map[string][]jsonGuardTerm{}
	var walk func(items []ir.Item, cond []jsonGuardTerm)
	walk = func(items []ir.Item, cond []jsonGuardTerm) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				if len(cond) > 0 {
					out[item.F.Name] = append([]jsonGuardTerm(nil), cond...)
				}
			case *ir.Branch:
				walk(item.Then, append(cond[:len(cond):len(cond)], jsonGuardTerm{item.Cond, !item.Neg}))
				walk(item.Else, append(cond[:len(cond):len(cond)], jsonGuardTerm{item.Cond, item.Neg}))
			}
		}
	}
	walk(st.Items, nil)
	return out
}

// jsonGuard emits one guarded field's condition (§16.2) as [member, want]
// pairs, outermost first. A term names a bool of the same body (SPEC §4.5),
// which the checked unit guarantees; a term the body does not declare is
// emitted as a null member, and runtime/json.nut holds such a pair false.
func jsonGuard(m *Module, st *ir.Struct, terms []jsonGuardTerm, target string) {
	pairs := make([]string, len(terms))
	for i, term := range terms {
		member := "null"
		for _, g := range st.Fields {
			if g.Name == term.Field {
				member = squirrel.StringLit([]byte(squirrel.Member(g)))
				break
			}
		}
		pairs[i] = fmt.Sprintf("[%s, %v]", member, term.Want)
	}
	m.Printf("%s.guard <- [%s];\n", target, strings.Join(pairs, ", "))
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
