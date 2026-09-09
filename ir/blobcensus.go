// The BYTE BUFFER's unit-level census (docs/SPEC-TABLES.md §2.5): every place
// a unit DECLARES a `*bytes` or a `*string`, whether or not a root's pointer
// walk reaches it.
package ir

import "sort"

// BlobPointerFields returns the qualified name of every BYTE BUFFER POINTER
// the unit declares — `Table.field` for a field of a table or a declared type,
// and `Union.arm` for a union arm whose payload is a `*bytes` or a `*string`
// (docs/SPEC-TABLES.md §2.5, §2.6).
//
// IT IS A DECLARATION SCAN AND NOT A REACHABILITY WALK, and that is the whole
// point of it. [PointerReachableBlobs] answers a WIRE question — which
// reserved node ids A GIVEN ROOT's load can place — and a reader owes exactly
// that set, so it must stay the numbering walk's answer. The question a
// BACKEND asks before it emits a byte buffer's runtime into a translation unit
// is a different one: can any call site in this unit name that runtime. A
// declaration scan is a SUPERSET of every such walk over the same unit, so a
// backend that gates on this census cannot lose a definition a call site it
// emitted still needs — including when an emitter's own walk and the numbering
// walk disagree, which is precisely what the arms gate's negative controls
// make happen on purpose (schema#565).
//
// The census is UNIT-LEVEL for the reason [WideTextFields] is: a runtime that
// sits behind one package-scoped guard is defined by whichever header of the
// package a translation unit includes first, so the question is asked of the
// unit and never of a file.
func BlobPointerFields(u *Unit) []string {
	var out []string
	collect := func(name string, st *Struct) {
		for _, f := range st.Fields {
			if f.Type.Blob() {
				out = append(out, name+"."+f.Name)
			}
		}
	}
	for name, st := range u.Structs {
		collect(name, st)
	}
	// a map's generated entry is a table of the unit (internal/check), so its
	// key and value are walked here with every other declaration
	for name, st := range u.Tables {
		collect(name, st)
	}
	arms := func(name string, un *Union) {
		for _, v := range un.Variants {
			if v.F != nil && v.F.Type.Blob() {
				out = append(out, name+"."+v.Name)
			}
		}
	}
	for name, un := range u.Unions {
		arms(name, un)
	}
	for name, un := range u.TableUnions {
		arms(name, un)
	}
	sort.Strings(out)
	return out
}
