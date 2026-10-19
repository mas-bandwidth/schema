package rusttable

import (
	"fmt"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// refuseCardUnsupportedVariableConstructs is docs/SPEC-TABLES.md §11's refusal
// at generation: it refuses, BY NAME, every variable-class construct this
// backend does not carry on the table wire, while the variable class it does
// carry passes: table roots and pointers *T over the flat node table (§3.1),
// tables nested by value, *string and *bytes blobs (§2.5), bool and every
// int/uint/float/bits width, string(N) and bytes(N), enums, and unbounded and
// bounded counted arrays of scalar, enum, table and pointer elements (§2.9).
// The rest of the class stays absent and is refused rather than mis-encoded or
// silently dropped.
//
// A construct this backend does not reach is named as `<table>.<field>: <what>`
// so the message names the table, the field and the construct together, which
// is what a refusal by name owes (§11). A table whose own fields are carried but
// which reaches a table that is not is refused at the field that reaches it.
//
// SCOPED TO THE CARD'S VARIABLE-WIRE PATH, not to GenerateLineage: it guards
// [cardVariableModules] — the card unit's own module emitter, which runs the
// refusal FIRST (§11) — while [scopedVariableModules], GenerateLineage's entry
// point, leaves a variable table this leg does not carry ABSENT rather than
// refusing the unit whole. The scoping is what keeps the fixed-form corpus
// (rusttable_test.go's `pointered`) green: it declares variable tables with
// unions the variable wire does not carry, and refusing them at the top of
// GenerateLineage breaks the fixed form's own tests.
func refuseCardUnsupportedVariableConstructs(u *ir.Unit) error {
	variable := ir.VariableTables(u)
	if len(variable) == 0 {
		return nil
	}
	vc := newVariableClass(u)
	names := make([]string, 0, len(variable))
	for n := range variable {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if why := vc.reason[name]; why != "" {
			return fmt.Errorf("rust variable form: %s.%s (docs/SPEC-TABLES.md §11)", name, why)
		}
	}
	return nil
}

// variableClass is one unit's answer to "does this leg carry that table on the
// variable wire" (docs/SPEC-TABLES.md §3, §3.1): per table, "" when it does and
// `<field>: <construct>` when it does not, and the set the codec emits.
type variableClass struct {
	reason map[string]string
	// file is the declaring file of each table, because the codec is emitted
	// per file and a body that names a table of ANOTHER file would need that
	// file's module in scope (§11 names what is not carried).
	file map[string]string
	// emitted is every carried variable table and every table one of them
	// reaches by value, by element or by pointer: the storage structs and the
	// body codecs the wire needs. Roots are the variable ones among them.
	emitted map[string]bool
}

// newVariableClass decides the carried set: each table's own fields first,
// then a fixpoint over what each one reaches, so a table reaching a table this
// leg does not carry is refused too, at the field that reaches it.
func newVariableClass(u *ir.Unit) *variableClass {
	vc := &variableClass{reason: map[string]string{}, file: map[string]string{}, emitted: map[string]bool{}}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			vc.file[st.Name] = f.Base
		}
	}
	names := make([]string, 0, len(u.Tables))
	for n := range u.Tables {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := u.Tables[n]
		vc.reason[n] = ""
		for _, f := range st.Fields {
			if why := variableFieldConstruct(f); why != "" {
				vc.reason[n] = f.Name + ": " + why
				break
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, n := range names {
			if vc.reason[n] != "" {
				continue
			}
			st := u.Tables[n]
			for _, f := range st.Fields {
				ref, ok := f.Type.Ref.(*ir.Struct)
				if !ok || f.Type.Blob() {
					continue
				}
				if vf, ok := vc.file[ref.Name]; ok && vf != vc.file[n] {
					vc.reason[n] = fmt.Sprintf("%s: a table declared in another schema file (%s) is outside the variable class this leg carries (§11)", f.Name, ref.Name)
					changed = true
					break
				}
				if why := vc.reason[ref.Name]; why != "" {
					vc.reason[n] = fmt.Sprintf("%s: it reaches %s, whose %s", f.Name, ref.Name, why)
					changed = true
					break
				}
			}
		}
	}
	variable := ir.VariableTables(u)
	var reach func(n string)
	reach = func(n string) {
		if vc.emitted[n] {
			return
		}
		st := u.Tables[n]
		if st == nil {
			return
		}
		vc.emitted[n] = true
		for _, f := range st.Fields {
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Blob() {
				reach(ref.Name)
			}
		}
	}
	for _, n := range names {
		if variable[n] && vc.reason[n] == "" {
			reach(n)
		}
	}
	return vc
}

// carried reports whether the variable wire carries one table.
func (vc *variableClass) carried(name string) bool { return vc.reason[name] == "" }

// variableFieldConstruct returns the construct's name on a field the variable
// class this leg carries does not reach, and "" on every field it does: a
// scalar of any int/uint/float/bits width, a string(N), a bytes(N), a *string
// or *bytes blob (§2.5), an enum, a table nested by value or pointed at (§2.1,
// §3.1), and an unbounded []T or bounded [..N]T array of scalar, enum, table or
// pointer elements (§2.9). Whether a referenced TABLE is itself carried is the
// table-level fixpoint's question ([newVariableClass]).
func variableFieldConstruct(f *ir.Field) string {
	switch {
	case f.IsMap():
		return "map[K]V is outside the variable class this leg carries (§2.8)"
	case f.Type.Optional:
		return "optional ?T is outside the variable class this leg carries (§2.3)"
	case f.KeyEnum != "":
		return "enum-keyed array [E]T is outside the variable class this leg carries (§2.4)"
	case f.Type.Kind == ir.TInt && f.Type.Width == 128:
		return "the 128-bit integer is outside the variable class this leg carries (§15)"
	case f.Type.Kind == ir.TFixed:
		return "the fixed-point kind is outside the variable class this leg carries (§15)"
	case f.Type.Kind == ir.TWString:
		return "wstring(N) is outside the variable class this leg carries (§3)"
	case f.Guard != "":
		return "a guarded field (an if group) is outside the variable class this leg carries (§16.2)"
	case f.HasFloatRange:
		return "the compressed float is outside the variable class this leg carries (§3)"
	case f.Array == ir.ArrayFixed:
		return "the fixed-length array [N]T is outside the variable class this leg carries (§3)"
	case f.Array == ir.ArrayCounted && f.ArrayMin > 0:
		return "a counted array with a declared minimum [Min..N]T is outside the variable class this leg carries (§3)"
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Union:
		return "union arms are outside the variable class this leg carries (§2.6)"
	case *ir.Flags:
		return "the flags mask is outside the variable class this leg carries (§3)"
	case *ir.Struct:
		if !ref.IsTable {
			return "a nested type is outside the variable class this leg carries (§2)"
		}
	}
	return ""
}
