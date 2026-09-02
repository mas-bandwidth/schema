// Table-wire field identity and the table closure (SPEC-TABLES.md).
// Target-independent, so every backend and any generator outside this module
// derives one id for one field name.
package ir

import "sort"

// FieldId is the stable TABLE-wire identity of a field name:
// fold16(fnv1a32(name)), rebounding 0 (the terminator) to 1.
func FieldId(name string) uint16 {
	h := uint32(0x811C9DC5)
	for i := 0; i < len(name); i++ {
		h ^= uint32(name[i])
		h *= 0x01000193
	}
	id := uint16((h ^ (h >> 16)) & 0xFFFF)
	if id == 0 {
		id = 1
	}
	return id
}

// VariantId is the stable TABLE-wire identity of an enum variant or a union
// arm: the same fold a field name takes, over the variant's own name. An
// enum's implicit None and a union's empty arm ride as 0, which the fold's
// rebound keeps free of every declared name.
func VariantId(name string) uint16 { return FieldId(name) }

// TableFieldId is a field's EFFECTIVE table-wire id: the hash of its
// `was = "old_name"` alias when one is declared — so wire identity survives
// the rename — and of its own name otherwise.
func TableFieldId(f *Field) uint16 {
	if f.WasName != "" {
		return FieldId(f.WasName)
	}
	return FieldId(f.Name)
}

// VariableTables derives the table MODE — the compiler works it out; nobody
// declares it (the owner's ruling: "i wouldn't want to manually have to
// specify this… the compiler can work it out"). A closure member is
// VARIABLE-LENGTH when a pointer appears anywhere in its BY-VALUE closure:
// its own `*T` fields, or those of anything it nests by value. Everything
// else is FIXED-SIZE — a plain struct of known sizeof, exactly as every table
// was before pointers existed, and it gets none of the arena machinery.
//
// The derivation is a least-fixed-point over the by-value edges: pointer
// edges carry no size and never propagate the mode to the POINTING table's
// nester, they only mark the table that declares them.
func VariableTables(u *Unit) map[string]bool {
	closure := TableClosure(u)
	member := func(name string) *Struct {
		if st := u.Tables[name]; st != nil {
			return st
		}
		return u.Structs[name]
	}
	names := make([]string, 0, len(closure))
	for name := range closure {
		names = append(names, name)
	}
	sort.Strings(names)

	variable := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, name := range names {
			if variable[name] {
				continue
			}
			st := member(name)
			if st == nil {
				continue
			}
			for _, f := range st.Fields {
				if f.Type.Pointer {
					variable[name] = true
					break
				}
				if f.Type.Kind != TNamed {
					continue
				}
				switch ref := f.Type.Ref.(type) {
				case *Struct:
					if variable[ref.Name] {
						variable[name] = true
					}
				case *Union:
					for _, v := range ref.Variants {
						if variable[v.Type] {
							variable[name] = true
						}
					}
				}
				if variable[name] {
					break
				}
			}
			if variable[name] {
				changed = true
			}
		}
	}
	return variable
}

// PointerTargets is the set of tables some pointer field targets — the tables
// that need an arena allocation surface (Builder.Alloc<T>()) and a cooked
// accessor. A table can be a pointer target and a root at once.
func PointerTargets(u *Unit) map[string]bool {
	targets := map[string]bool{}
	for name := range TableClosure(u) {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Pointer {
				targets[f.Type.Name] = true
			}
		}
	}
	return targets
}

// TableClosure is the set of structs that carry table codecs and reflection
// descriptors: every `table` declaration plus every struct reachable from one
// through fields (nested tables and types, array elements, union payloads),
// transitively. Plain types outside the closure stay packet-wire only.
func TableClosure(u *Unit) map[string]bool {
	closure := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if closure[name] {
			return
		}
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		if st == nil {
			return
		}
		closure[name] = true
		for _, f := range st.Fields {
			if f.Type.Kind != TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *Struct:
				walk(ref.Name)
			case *Union:
				for _, v := range ref.Variants {
					walk(v.Type)
				}
			}
		}
	}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		walk(name)
	}
	return closure
}
