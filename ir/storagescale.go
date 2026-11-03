package ir

// TableStorageScale bounds the storage one byte of a form-1 wire can command
// (docs/SPEC-TABLES.md §11): a list element, a map entry and a node record
// each cost at least one wire byte, and none commands more than the largest
// record, list element or map entry the unit's tables reach, plus its
// alignment padding and a node record's 16-byte attribution. A wire shorter
// than MaxSizeBytes / TableStorageScale(u) bytes cannot measure past the
// storage cap, so a port that builds a value as it reads measures only a
// longer wire before it builds anything.
func TableStorageScale(u *Unit) int64 {
	scale := int64(16)
	seen := map[*Struct]bool{}
	seenUnion := map[*Union]bool{}
	var record func(st *Struct)
	var field func(f *Field)
	record = func(st *Struct) {
		if seen[st] {
			return
		}
		seen[st] = true
		scale = max(scale, RecordLayout(u, st).Size)
		for _, f := range st.Fields {
			field(f)
		}
	}
	field = func(f *Field) {
		switch {
		case f.MapEntry != nil:
			record(f.MapEntry)
		case f.Array != ArrayNone:
			size, _ := ListElementLayout(u, f)
			scale = max(scale, size)
		}
		if f.Type.Kind != TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *Struct:
			record(r)
		case *Union:
			if !seenUnion[r] {
				seenUnion[r] = true
				for _, v := range r.Variants {
					if v.F != nil {
						field(v.F)
					}
				}
			}
		}
	}
	for _, st := range u.Tables {
		record(st)
	}
	return scale + 256
}
