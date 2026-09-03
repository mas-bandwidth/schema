// The TABLE-WIRE KIND vocabulary (docs/SPEC-TABLES.md §3): the closed set of kind
// bytes the neutral table wire carries, and the mapping from a declaration's
// type onto it.
//
// It lives here, beside FieldId, because it is target-independent wire law:
// the C++ backend emits it into codecs and descriptors, and the tables
// baseline (internal/baseline) records it as the fact a reader keys a field's
// payload on. Two copies of this mapping would be two wires.
package ir

import (
	"fmt"
	"math/big"
	"strings"
)

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
	// kind 17 is the pointer index (TableKindPointer, beside the build
	// version that renders it).

	// The scalars the TYPE wire carries and the table wire gained with them
	// (docs/SPEC-TABLES.md §3): the 128-bit integers, sixteen bytes two's
	// complement little-endian — the low 64-bit half first, then the high
	// half, the type wire's own order — and the fixed-point family, which
	// rides as its RAW scaled storage integer at the storage width, one kind
	// per width and signedness. The (I, F) and the bounds stay in the schema,
	// as a ranged integer's bounds do; the kind is what tells a reader that
	// the bytes are a scaled value and not a count, which is the same reason
	// a pointer index is not a plain u32 (§3.1).
	TableKindI128      = 18
	TableKindU128      = 19
	TableKindFixed8    = 20
	TableKindFixed16   = 21
	TableKindFixed32   = 22
	TableKindFixed64   = 23
	TableKindFixed128  = 24
	TableKindUFixed8   = 25
	TableKindUFixed16  = 26
	TableKindUFixed32  = 27
	TableKindUFixed64  = 28
	TableKindUFixed128 = 29
)

// TableScalarKind is the kind a field's ELEMENT rides under: the field's own
// kind when it is not an array, and the element kind when it is. `bytes(N)`
// answers TableKindArray, because it rides as an array of u8 and its element
// kind is [TableElemKind]'s answer, not this one. Every declared type has a
// kind; 0 is the reserved value no declaration spells.
func TableScalarKind(f *Field) int {
	if f.Type.Blob() {
		// a BYTE BUFFER is a pointer to a blob node (docs/SPEC-TABLES.md §2.5):
		// its own payload is the node index, and the bytes ride as a record
		// under a reserved type id (§3.1) — so `*bytes` against `bytes(N)` is
		// kind 17 against kind 14, a reported edit in both directions
		return TableKindPointer
	}
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
			case 128:
				return TableKindI128
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
		case 128:
			return TableKindU128
		default:
			return TableKindU64
		}
	case TFixed:
		if f.Type.Signed {
			switch f.Type.Width {
			case 8:
				return TableKindFixed8
			case 16:
				return TableKindFixed16
			case 32:
				return TableKindFixed32
			case 128:
				return TableKindFixed128
			default:
				return TableKindFixed64
			}
		}
		switch f.Type.Width {
		case 8:
			return TableKindUFixed8
		case 16:
			return TableKindUFixed16
		case 32:
			return TableKindUFixed32
		case 128:
			return TableKindUFixed128
		default:
			return TableKindUFixed64
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
	if f.Type.Kind == TBytes && !f.Type.Blob() {
		return TableKindU8
	}
	if f.Array != ArrayNone {
		return TableScalarKind(f)
	}
	return 0
}

// TableKindWidth is a fixed-width kind's payload size in bytes — the skip rule's
// first row (docs/SPEC-TABLES.md §3) — and 0 for the framed kinds (string, table,
// array, union, keyed). Every backend's skipper and every engine's reader is
// this table, so it lives once.
func TableKindWidth(kind int) int {
	switch kind {
	case TableKindBool, TableKindI8, TableKindU8, TableKindFixed8, TableKindUFixed8:
		return 1
	case TableKindI16, TableKindU16, TableKindFixed16, TableKindUFixed16:
		return 2
	case TableKindI32, TableKindU32, TableKindF32, TableKindPointer, TableKindFixed32, TableKindUFixed32:
		return 4
	case TableKindI64, TableKindU64, TableKindF64, TableKindFixed64, TableKindUFixed64:
		return 8
	case TableKindI128, TableKindU128, TableKindFixed128, TableKindUFixed128:
		return 16
	}
	return 0
}

// TableKindWide reports whether a kind is one of the scalars the type wire
// brought — the 128-bit integers and the fixed-point family (kinds 18–29):
// the kinds whose value is a RAW INTEGER that may not fit 64 bits and whose
// text spelling is not the integer's own (docs/SPEC-TABLES.md §3, §16.2).
func TableKindWide(kind int) bool {
	return kind >= TableKindI128 && kind <= TableKindUFixed128
}

// TableKindSigned reports whether an integer-shaped kind's bytes are two's
// complement: the signed integers and the signed fixed-point kinds.
func TableKindSigned(kind int) bool {
	switch kind {
	case TableKindI8, TableKindI16, TableKindI32, TableKindI64, TableKindI128,
		TableKindFixed8, TableKindFixed16, TableKindFixed32, TableKindFixed64, TableKindFixed128:
		return true
	}
	return false
}

// TableRawRange is the range a reader CLAMPS a wide kind's raw value to
// (docs/SPEC-TABLES.md §4): a fixed field's declared whole-unit bounds shifted
// by F onto the raw scale, and a 128-bit integer's declared bounds as they
// are. ok is false where the declaration bounds nothing — a bare `uint128`,
// whose storage width is its only limit.
func TableRawRange(f *Field) (lo, hi *big.Int, ok bool) {
	if !f.HasIntRange || f.IntMin == nil || f.IntMax == nil {
		return nil, nil, false
	}
	if f.Type.Kind == TFixed {
		return new(big.Int).Lsh(f.IntMin, uint(f.Type.FracBits)), new(big.Int).Lsh(f.IntMax, uint(f.Type.FracBits)), true
	}
	return f.IntMin, f.IntMax, true
}

// TableWideFields lists every field of a unit's table closure that rides
// under a wide kind, as `Member.field spelling` lines in closure order — what
// a backend without those kinds refuses BY NAME rather than emitting a second
// wire for (docs/SPEC-TABLES.md §15).
func TableWideFields(u *Unit) []string {
	closure := TableClosure(u)
	var out []string
	for _, f := range u.Files {
		for _, st := range f.Tables {
			out = appendWideFields(out, st)
		}
		for _, d := range f.Decls {
			if st, ok := d.(*Struct); ok && closure[st.Name] {
				out = appendWideFields(out, st)
			}
		}
	}
	return out
}

func appendWideFields(out []string, st *Struct) []string {
	for _, f := range st.Fields {
		if TableKindWide(TableScalarKind(f)) {
			out = append(out, st.Name+"."+f.Name+" "+TableTypeSpelling(f))
		}
	}
	return out
}

// TableTypeSpelling is a field's declared scalar type as the language spells
// it — `fixed(16, 16)`, `uint128`, `bits(9)`, `float32` — for descriptors and
// diagnostics that name the declaration rather than the kind.
func TableTypeSpelling(f *Field) string {
	switch f.Type.Kind {
	case TBool:
		return "bool"
	case TInt:
		if f.Type.Signed {
			return "int" + itoa(f.Type.Width)
		}
		return "uint" + itoa(f.Type.Width)
	case TFixed:
		word := "fixed"
		if !f.Type.Signed {
			word = "ufixed"
		}
		return word + "(" + itoa(f.Type.IntBits) + ", " + itoa(f.Type.FracBits) + ")"
	case TBits:
		return "bits(" + itoa(f.Type.Width) + ")"
	case TFloat32:
		return "float32"
	case TFloat64:
		return "float64"
	case TString:
		return "string"
	case TBytes:
		return "bytes"
	case TNamed:
		return f.Type.Name
	}
	return "?"
}

func itoa(v int) string { return big.NewInt(int64(v)).String() }

// RefuseWideTableKinds is the refusal a backend without the wide kinds owes a
// unit that declares them (docs/SPEC-TABLES.md §15): BY NAME, naming every
// field and the follow-on, so no port emits a second wire for a kind it does
// not carry. A backend that carries the kinds does not call it.
func RefuseWideTableKinds(u *Unit, backend string) error {
	fields := TableWideFields(u)
	if len(fields) == 0 {
		return nil
	}
	return fmt.Errorf("the %s table backend does not carry the fixed-point and 128-bit table-wire kinds yet — %s "+
		"(docs/SPEC-TABLES.md §3, §15: every type the type wire carries rides in a table in the C++ reference and the tool, "+
		"and each port lands them as a row on schema#366)", backend, strings.Join(fields, ", "))
}
