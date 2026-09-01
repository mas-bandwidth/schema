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

// TableFieldId is a field's EFFECTIVE table-wire id: the hash of its
// `was = "old_name"` alias when one is declared — so wire identity survives
// the rename — and of its own name otherwise.
func TableFieldId(f *Field) uint16 {
	if f.WasName != "" {
		return FieldId(f.WasName)
	}
	return FieldId(f.Name)
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
