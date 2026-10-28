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

// rustStdShadowed is every standard-library name the emitted variable codec
// spells unqualified (std's prelude, the derives it writes, and what its
// modules import by name). The codec's storage struct for a body is the
// declaration's own name and the crate root glob-exports it, and a glob import
// shadows the prelude, so a body by one of these names would break every
// module that reaches it: test/tables/VOLD_nested_append.schema's
// `fixed table Vec` is the case. Such a body is left OFF the variable wire, by
// name (docs/SPEC-TABLES.md §11), and keeps its fixed form (§3.4).
var rustStdShadowed = map[string]bool{
	"Any": true, "Box": true, "Clone": true, "Copy": true, "Debug": true,
	"Default": true, "HashMap": true, "Layout": true, "Option": true,
	"PartialEq": true, "Rc": true, "Result": true, "String": true, "Vec": true,
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
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok {
				vc.file[st.Name] = f.Base
			}
		}
	}
	// THE CARRIED SET IS EVERY BODY A TABLE CAN REACH BY VALUE: the unit's
	// tables and its `type`s alike. A `type` a table holds by value, as an
	// array element, as an optional or as a union arm rides as a nested body
	// exactly as a table does (docs/SPEC-TABLES.md §2, §3: kind 13, L, the
	// body), so the same storage, numbering walk and codec carry it.
	names := make([]string, 0, len(u.Tables)+len(u.Structs))
	for n := range u.Tables {
		names = append(names, n)
	}
	for n := range u.Structs {
		if _, dup := u.Tables[n]; !dup {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		st := vStruct(u, n)
		vc.reason[n] = ""
		if rustStdShadowed[n] {
			vc.reason[n] = "name: " + n + " is a Rust standard name the variable codec spells unqualified, and a storage struct by that name would shadow it in every module of the crate"
			continue
		}
		for _, f := range st.Fields {
			if why := variableFieldConstruct(f); why != "" {
				// A map value of [N]T of a carried element is the entry's own row
				// (docs/SPEC-TABLES.md §2.8).
				if st.IsMapEntry() && f.Array == ir.ArrayFixed && mapValueArrayCarried(f) {
					continue
				}
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
			st := vStruct(u, n)
			for _, f := range st.Fields {
				if why, ok := vc.reached(n, f); ok && why != "" {
					vc.reason[n] = why
					changed = true
					break
				}
			}
		}
	}
	// THE ROOTS ARE EVERY TABLE OF THE CLOSURE, fixed and variable alike: a
	// `fixed table` rides the variable form too (docs/SPEC-TABLES.md §2.2,
	// §3), so its form-1 file is this codec's as much as a variable table's.
	roots := ir.TableClosure(u)
	var reach func(n string)
	reach = func(n string) {
		if vc.emitted[n] {
			return
		}
		st := vStruct(u, n)
		if st == nil {
			return
		}
		vc.emitted[n] = true
		for _, f := range st.Fields {
			vc.reachField(f, reach)
		}
	}
	for _, n := range names {
		if roots[n] && vc.reason[n] == "" {
			reach(n)
		}
	}
	return vc
}

// vStruct is the declaration a carried body names: a table, or a `type` a
// table reaches by value (docs/SPEC-TABLES.md §2).
func vStruct(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// carried reports whether the variable wire carries one table.
func (vc *variableClass) carried(name string) bool { return vc.reason[name] == "" }

// variableFieldConstruct returns the construct's name on a field the variable
// class this leg carries does not reach, and "" on every field it does: a
// scalar of any int/uint/float/bits width (128-bit included), a fixed(I, F) or
// ufixed(I, F) as its raw storage integer (§3, kinds 20–29), a string(N), a
// bytes(N), a wstring(N) field, arm or map value (§3, kind 33), a *string or
// *bytes blob (§2.5), an enum, a table nested by value
// or pointed at (§2.1, §3.1), an optional ?T (§2.3), an enum-keyed array [E]T
// (§2.4), a map[K]V (§2.8), a union or array of unions (§2.6), an unbounded
// []T or bounded [..N]T array (§2.9), and any of these inside an `if` group,
// which the walks gate on its branch condition (variableguard.go, §3.1, §4). Whether a referenced TABLE is itself
// carried is the table-level fixpoint's question ([newVariableClass]).
func variableFieldConstruct(f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TWString && (f.Array != ir.ArrayNone || f.Type.Optional || f.KeyEnum != "" || f.Type.Pointer):
		// wstring(N) rides as a field, an arm and a map value (§2.6, §2.8, §3);
		// as an element, an optional or a keyed slot it is not carried yet
		return "wstring(N) as an array element, an optional or an enum-keyed slot is outside the variable class this leg carries (§3)"
	case f.Type.Optional && (f.Type.Pointer || f.KeyEnum != "" || f.IsMap()):
		return "optional ?T of this shape is outside the variable class this leg carries (§2.3)"
	case f.KeyEnum != "":
		if f.KeyEnumRef == nil || f.Type.Pointer || f.Type.Optional || (classOf(f) != vScalar && classOf(f) != vEnum && classOf(f) != vTable) {
			return "enum-keyed array [E]T is outside the variable class this leg carries (§2.4)"
		}
		inner := *f
		inner.KeyEnum = ""
		inner.KeyEnumRef = nil
		inner.Array = ir.ArrayNone
		inner.ArrayBound = 0
		return variableFieldConstruct(&inner)
	case f.HasFloatRange:
		return "the compressed float is outside the variable class this leg carries (§3)"
	case f.Array == ir.ArrayCounted && f.ArrayMin > 0:
		return "a counted array with a declared minimum [Min..N]T is outside the variable class this leg carries (§3)"
	}
	if ref, ok := f.Type.Ref.(*ir.Union); ok {
		return unionFieldConstruct(ref, map[*ir.Union]bool{})
	}
	return ""
}

func unionFieldConstruct(un *ir.Union, seen map[*ir.Union]bool) string {
	if un == nil || seen[un] {
		return ""
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.Void() || v.F == nil {
			continue
		}
		if v.Body() {
			continue
		}
		if why := variableFieldConstruct(v.F); why != "" {
			if v.F.Array == ir.ArrayFixed && mapValueArrayCarried(v.F) {
				continue
			}
			return "arm " + v.Name + ": " + why
		}
	}
	return ""
}

// mapValueArrayCarried reports a [N]T map value whose element this leg already
// carries, so the entry is not refused for the array the map's row is
// (docs/SPEC-TABLES.md §2.8).
func mapValueArrayCarried(f *ir.Field) bool {
	inner := *f
	inner.Array = ir.ArrayNone
	inner.ArrayBound = 0
	return variableFieldConstruct(&inner) == ""
}

// reached is the fixpoint's edge: a map's entry, a union arm's table, or a
// nested table, and the refusal when that edge leaves the carried set
// (docs/SPEC-TABLES.md §2.6, §2.8, §11).
func (vc *variableClass) reached(owner string, f *ir.Field) (string, bool) {
	if f.IsMap() && f.MapEntry != nil {
		return vc.reachName(owner, f.Name, f.MapEntry.Name)
	}
	if un, ok := f.Type.Ref.(*ir.Union); ok {
		if why := vc.reachedUnion(owner, f.Name, un, map[*ir.Union]bool{}); why != "" {
			return why, true
		}
		return "", true
	}
	ref, ok := f.Type.Ref.(*ir.Struct)
	if !ok || f.Type.Blob() {
		return "", false
	}
	return vc.reachName(owner, f.Name, ref.Name)
}

func (vc *variableClass) reachedUnion(owner, field string, un *ir.Union, seen map[*ir.Union]bool) string {
	if un == nil || seen[un] {
		return ""
	}
	seen[un] = true
	for _, v := range un.Variants {
		if v.F == nil {
			continue
		}
		if ref, ok := v.F.Type.Ref.(*ir.Struct); ok && ref.IsTable && !v.F.Type.Blob() {
			if why, ok := vc.reachName(owner, field, ref.Name); ok && why != "" {
				return why
			}
		}
		if v.Ref != nil && v.Ref.IsTable {
			if why, ok := vc.reachName(owner, field, v.Ref.Name); ok && why != "" {
				return why
			}
		}
		if v.F.IsMap() && v.F.MapEntry != nil {
			if why, ok := vc.reachName(owner, field, v.F.MapEntry.Name); ok && why != "" {
				return why
			}
		}
		if inner := unionOf(v.F); inner != nil {
			if why := vc.reachedUnion(owner, field, inner, seen); why != "" {
				return why
			}
		}
	}
	return ""
}

func (vc *variableClass) reachName(owner, field, name string) (string, bool) {
	_ = owner
	if why := vc.reason[name]; why != "" {
		return fmt.Sprintf("%s: it reaches %s, whose %s", field, name, why), true
	}
	return "", true
}

// reachField descends one field the numbering walks (docs/SPEC-TABLES.md §3.1,
// §2.6, §2.8): a map's entry, a union arm's table, a nested table.
func (vc *variableClass) reachField(f *ir.Field, reach func(string)) {
	if f.IsMap() && f.MapEntry != nil {
		reach(f.MapEntry.Name)
		return
	}
	if un, ok := f.Type.Ref.(*ir.Union); ok {
		for _, v := range un.Variants {
			if v.Ref != nil && v.Ref.IsTable {
				reach(v.Ref.Name)
			}
			if v.F != nil {
				if ref, ok := v.F.Type.Ref.(*ir.Struct); ok && ref.IsTable && !v.F.Type.Blob() {
					reach(ref.Name)
				}
				vc.reachField(v.F, reach)
			}
		}
		return
	}
	if ref, ok := f.Type.Ref.(*ir.Struct); ok && !f.Type.Blob() {
		reach(ref.Name)
	}
}
