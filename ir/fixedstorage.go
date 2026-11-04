package ir

// TableFixedStorageBoundNeeded answers whether a fixed record's write reads a
// COUNT or a USED LENGTH that indexes its storage: a counted array's live
// count, or a string's, wstring's or bytes' used length, anywhere in the
// record's closure (a nested record, a union arm, an optional's payload). Those
// two numbers bound how far the fixed writer reads into the record, so a
// writer in a language without a runtime bounds check refuses one past its
// storage in EVERY build (docs/SPEC.md §5.1: memory safety is not a value
// contract). A record of scalars, enums and fixed arrays of them has none.
func TableFixedStorageBoundNeeded(st *Struct) bool {
	return tableFixedStorageBoundNeeded(st, map[string]bool{})
}

func tableFixedStorageBoundNeeded(st *Struct, seen map[string]bool) bool {
	if seen[st.Name] {
		return false
	}
	seen[st.Name] = true
	for _, f := range st.Fields {
		if tableFixedStorageBoundNeededField(f, seen) {
			return true
		}
	}
	return false
}

// TableFixedStorageBoundNeededField is the same question about ONE field, its
// array or optional shape included.
func TableFixedStorageBoundNeededField(f *Field) bool {
	return tableFixedStorageBoundNeededField(f, map[string]bool{})
}

func tableFixedStorageBoundNeededField(f *Field, seen map[string]bool) bool {
	if f.Array == ArrayCounted {
		return true
	}
	if f.Array == ArrayNone && f.KeyEnum == "" {
		switch f.Type.Kind {
		case TString, TWString, TBytes:
			return true
		}
	}
	return tableFixedStorageBoundNeededElement(f, seen)
}

// TableFixedStorageBoundNeededElement is the question about ONE ELEMENT of a
// field: its type alone, without its array or optional shape.
func TableFixedStorageBoundNeededElement(f *Field) bool {
	return tableFixedStorageBoundNeededElement(f, map[string]bool{})
}

func tableFixedStorageBoundNeededElement(f *Field, seen map[string]bool) bool {
	if f.Type.Kind != TNamed {
		return false
	}
	switch r := f.Type.Ref.(type) {
	case *Struct:
		return tableFixedStorageBoundNeeded(r, seen)
	case *Union:
		for _, v := range r.Variants {
			if v.F != nil && tableFixedStorageBoundNeededField(v.F, seen) {
				return true
			}
		}
	}
	return false
}
