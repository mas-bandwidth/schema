package cstable

import (
	"fmt"
	"math"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE CLR'S VALUE-TYPE BOUNDS (docs/SPEC-TABLES.md §11). The block form's
// rows and projections and the cook's rows spell a record as a C# struct, and
// CoreCLR lays a struct out under two limits, measured on .NET 10 (macOS
// arm64):
//
//   - its size is an int32: StructLayout's Size is an int, and a fixed-size
//     buffer holds at most int.MaxValue bytes (CS1664). A struct of one
//     2^31-1 byte buffer compiles and loads.
//   - no field starts past offset 2^27-8 (134217720): a byte field at that
//     offset loads, one at 2^27-7 throws TypeLoadException. CoreCLR keeps a
//     field's offset in 27 bits and reserves the top few values.
//
// A record whose struct breaks either compiles to a type CoreCLR will not
// load, or does not compile at all, so the C# generator refuses the unit by
// name before it emits anything.
const (
	csValueTypeMaxSize        = int64(math.MaxInt32)
	csValueTypeMaxFieldOffset = int64(1)<<27 - 8
)

// CSValueTypeTooLarge names the refusal of a unit whose C# by-value struct
// passes the CLR's value-type bounds (docs/SPEC-TABLES.md §11).
const CSValueTypeTooLarge = "cs_value_type_too_large"

// ValueTypeRefusal is the C# generator's refusal of a unit with a record whose
// C# struct passes the CLR's value-type bounds.
type ValueTypeRefusal struct {
	Record string // the declaration
	Struct string // the C# struct that spells it
	Size   int64  // the struct's size
	Field  int64  // the largest offset a field of it starts at
}

func (e *ValueTypeRefusal) Error() string {
	return fmt.Sprintf("%s: %s's C# struct %s is %d bytes with a field at offset %d; CoreCLR loads no value type over %d bytes or with a field past offset %d, so the C# generator refuses the unit (docs/SPEC-TABLES.md §11)",
		CSValueTypeTooLarge, e.Record, e.Struct, e.Size, e.Field, csValueTypeMaxSize, csValueTypeMaxFieldOffset)
}

// csValueTypeLimit refuses a unit any of whose C# structs passes the CLR's
// bounds: every block row, every block projection, every cooked row and every
// union row the generator would emit, with the fields each one spells.
func csValueTypeLimit(u *ir.Unit, blocks *ir.BlockUnit) error {
	check := func(record, spelled string, size, field int64) error {
		if size > csValueTypeMaxSize || field > csValueTypeMaxFieldOffset {
			return &ValueTypeRefusal{Record: record, Struct: spelled, Size: size, Field: field}
		}
		return nil
	}
	if blocks != nil {
		for _, name := range blocks.Order {
			ml := blocks.Layout(name)
			if err := check(name, name+"Row", ml.Size, csStructFieldMax(u, ml, false, false)); err != nil {
				return err
			}
		}
		for _, bl := range blocks.Tables {
			if err := check(bl.Table.Name, bl.Table.Name+"BlockProjection", bl.Projection.Size, csStructFieldMax(u, &bl.Projection, true, false)); err != nil {
				return err
			}
		}
	}
	if len(u.Tables) == 0 {
		return nil
	}
	ck := cookUnitOf(u)
	for _, name := range ck.order {
		ml := ck.members[name]
		if err := check(name, name+"Row", ml.Size, csStructFieldMax(u, ml, false, true)); err != nil {
			return err
		}
	}
	for _, un := range ck.unions {
		size, _, _, armAt := ir.UnionLayout(u, un)
		if err := check(un.Name, un.Name+"Row", size, armAt); err != nil {
			return err
		}
	}
	return nil
}

// csStructFieldMax is the largest offset at which the struct the block or
// cook emitter spells for ml starts a field: each storage piece, each slot of
// an inline array of non-primitive elements (or, past 128 slots, the byte
// buffer after the first), and the last byte of trailing padding, which is a
// field of its own.
func csStructFieldMax(u *ir.Unit, ml *ir.MemberLayout, projection, cook bool) int64 {
	var most, end int64
	for _, fl := range ml.Fields {
		f := fl.Field
		var pieces []ir.BlockFieldPiece
		if cook {
			pieces = ir.FieldPieces(u, f, fl.Offset)
		} else {
			pieces = ir.BlockFieldPieceOffsets(u, f, fl.Offset, projection)
		}
		for i, p := range pieces {
			start := p.Offset
			if i == 0 && csSlottedArray(u, f, projection, cook) {
				elem := p.Size / f.ArrayBound
				if f.ArrayBound > 128 {
					start = p.Offset + elem
				} else {
					start = p.Offset + (f.ArrayBound-1)*elem
				}
			}
			most = max(most, start)
			end = max(end, p.Offset+p.Size)
		}
	}
	if end < ml.Size {
		most = max(most, ml.Size-1)
	}
	return most
}

// csSlottedArray reports whether the emitters spell f as an inline array of
// non-primitive elements, one field per slot, in the order their switches
// test it.
func csSlottedArray(u *ir.Unit, f *ir.Field, projection, cook bool) bool {
	switch {
	case cook && (f.IsList() || f.IsMap()):
		return false
	case cook && f.Type.Pointer && f.Array == ir.ArrayNone:
		return false
	case cook && f.Type.TextUnbounded():
		return false
	case !cook && projection && ir.BlockOutOfLine(f):
		return false
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		return false
	case f.KeyEnum != "", f.Array == ir.ArrayFixed, f.Array == ir.ArrayCounted:
	default:
		return false
	}
	if f.ArrayBound <= 0 {
		return false
	}
	t := f.Type
	if cook && t.Pointer {
		return false // a pointer's slot is a long, a primitive buffer
	}
	return !csFixedBufferPrimitive(csBlittableType(u, t))
}
