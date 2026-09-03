// The TABLE-WIRE KIND vocabulary (docs/SPEC-TABLES.md §3): the closed set of kind
// bytes the neutral table wire carries, and the mapping from a declaration's
// type onto it.
//
// It lives here, beside FieldId, because it is target-independent wire law:
// the C++ backend emits it into codecs and descriptors, and the tables
// baseline (internal/baseline) records it as the fact a reader keys a field's
// payload on. Two copies of this mapping would be two wires.
package ir

// The kind numbers are WIRE FORMAT — frozen (docs/SPEC-TABLES.md §3).
const (
	TableKindBool   = 1
	TableKindI8     = 2
	TableKindI16    = 3
	TableKindI32    = 4
	TableKindI64    = 5
	TableKindU8     = 6
	TableKindU16    = 7
	TableKindU32    = 8
	TableKindU64    = 9
	TableKindF32    = 10
	TableKindF64    = 11
	TableKindString = 12
	TableKindTable  = 13
	TableKindArray  = 14
	TableKindUnion  = 15
	// an ENUM-KEYED array body is its OWN kind (docs/SPEC-TABLES.md §3.2): the
	// positional array body and the keyed one are incompatible, so a reader
	// meeting the other must see a KIND MISMATCH and skip, never misdecode.
	TableKindKeyed = 16
)

// TableScalarKind is the kind a field's ELEMENT rides under: the field's own
// kind when it is not an array, and the element kind when it is. `bytes(N)`
// answers TableKindArray, because it rides as an array of u8 and its element
// kind is [TableElemKind]'s answer, not this one. A field whose type has no
// table-wire kind (int128, fixed — refused in a table closure) answers 0.
func TableScalarKind(f *Field) int {
	switch f.Type.Kind {
	case TBool:
		return TableKindBool
	case TInt:
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return TableKindI8
			case 16:
				return TableKindI16
			case 32:
				return TableKindI32
			default:
				return TableKindI64
			}
		}
		switch f.Type.Width {
		case 8:
			return TableKindU8
		case 16:
			return TableKindU16
		case 32:
			return TableKindU32
		default:
			return TableKindU64
		}
	case TBits:
		switch {
		case f.Type.Width <= 8:
			return TableKindU8
		case f.Type.Width <= 16:
			return TableKindU16
		case f.Type.Width <= 32:
			return TableKindU32
		default:
			return TableKindU64
		}
	case TFloat32:
		return TableKindF32
	case TFloat64:
		return TableKindF64
	case TString:
		return TableKindString
	case TBytes:
		return TableKindArray
	case TNamed:
		switch f.Type.Ref.(type) {
		case *Enum:
			// an enum value rides as the u16 hash of its VARIANT NAME, whatever
			// the declaration-side storage width (docs/SPEC-TABLES.md §5): identity
			// is the name here, exactly as it is for a field
			return TableKindU16
		case *Flags:
			return TableKindU64
		case *Struct:
			// A POINTER to a table is a NODE INDEX, not a nested body: it rides
			// as four bytes under kind 17 into the flat node table
			// (docs/SPEC-TABLES.md §3.1). Spending the distinct kind costs no
			// wire byte and closes an edit that would otherwise be silent — a
			// stored index reading back as a plausible u32, a number read as an
			// index — and it makes the by-value/pointer edit an ordinary kind
			// mismatch instead.
			if f.Type.Pointer {
				return TableKindPointer
			}
			return TableKindTable
		case *Union:
			return TableKindUnion
		}
	}
	return 0
}

// TableFieldKind is the kind byte written for the FIELD itself: an
// enum-keyed array is TableKindKeyed, any other array is TableKindArray, and
// everything else is its scalar kind. An OPTIONAL field rides under the kind
// of the value it holds — absence is the absence of the field, not a kind of
// its own — which is what keeps `T` and `?T` wire-identical.
func TableFieldKind(f *Field) int {
	if f.KeyEnum != "" {
		return TableKindKeyed
	}
	if f.Array != ArrayNone {
		return TableKindArray
	}
	return TableScalarKind(f)
}

// TableElemKind is the element kind an array field's body opens with, and 0
// for a field that is not an array on the wire. `bytes(N)` is an array of u8
// (docs/SPEC-TABLES.md §3) even though it declares no array bound.
func TableElemKind(f *Field) int {
	if f.Type.Kind == TBytes {
		return TableKindU8
	}
	if f.Array != ArrayNone {
		return TableScalarKind(f)
	}
	return 0
}
