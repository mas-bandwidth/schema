// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3, for Goose. This
// file is the fixed form's card: it builds the layout (§1), the hash (§5.2)
// and the per-table Goose surface — the struct, the constant body size, the
// baked layout bytes and hash, and the save/load entry points that call the
// runtime in runtime/fixed.goose — following docs/PORT-GOOSE.md (THE RUNTIME
// LAYOUT) and docs/FIXED-FORM-ALGORITHM.md. It registers with the table
// surface through [Register] so it joins every <Base>Table.goose module.
package goosetable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Fixed-kind codes, the closed set (docs/FIXED-FORM-ALGORITHM.md §1).
const (
	kindBool     = 1
	kindI8       = 2
	kindI16      = 3
	kindI32      = 4
	kindI64      = 5
	kindU8       = 6
	kindU16      = 7
	kindU32      = 8
	kindU64      = 9
	kindF32      = 10
	kindF64      = 11
	kindString   = 12
	kindTable    = 13
	kindArray    = 14
	kindUnion    = 15
	kindKeyed    = 16
	kindPtr      = 17
	kindI128     = 18
	kindU128     = 19
	kindFixedS8  = 20
	kindFixedS16 = 21
	kindFixedS32 = 22
	kindFixedS64 = 23
	kindFixedU8  = 25
	kindFixedU16 = 26
	kindFixedU32 = 27
	kindFixedU64 = 28
	kindEnum     = 30
	kindVariant  = 32
	kindWString  = 33
	kindOptional = 35
)

// fixedLayoutEntry is one 17-byte row of a layout (docs/FIXED-FORM-ALGORITHM.md §1).
type fixedLayoutEntry struct {
	id       uint64
	kind     uint8
	size     uint32
	children uint32
}

func fixedLayoutBytes(entries []fixedLayoutEntry) []byte {
	out := make([]byte, 0, 4+17*len(entries))
	out = appendFixedU32(out, uint32(len(entries)))
	for _, e := range entries {
		out = appendFixedU64(out, e.id)
		out = append(out, byte(e.kind))
		out = appendFixedU32(out, e.size)
		out = appendFixedU32(out, e.children)
	}
	return out
}

func appendFixedU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

func appendFixedU64(b []byte, v uint64) []byte {
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// fixedKind returns the wire kind of a field (docs/FIXED-FORM-ALGORITHM.md §1).
func fixedKind(f *ir.Field) uint8 {
	switch f.Type.Kind {
	case ir.TBool:
		return kindBool
	case ir.TInt:
		if f.Type.Width == 128 {
			if f.Type.Signed {
				return kindI128
			}
			return kindU128
		}
		if f.Type.Signed {
			return uint8(kindI8 + (f.Type.Width/8-1)*1)
		}
		return uint8(kindU8 + (f.Type.Width/8-1)*1)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return kindU32
		}
		return kindU64
	case ir.TFloat32:
		return kindF32
	case ir.TFloat64:
		return kindF64
	case ir.TFixed:
		if f.Type.Signed {
			return uint8(kindFixedS8 + (f.Type.Width/8-1)*1)
		}
		return uint8(kindFixedU8 + (f.Type.Width/8-1)*1)
	case ir.TString:
		return kindString
	case ir.TWString:
		return kindWString
	case ir.TBytes:
		return kindArray // bytes(N) walks as an array of u8
	case ir.TNamed:
		if f.Type.Pointer {
			return kindPtr
		}
		switch f.Type.Ref.(type) {
		case *ir.Enum:
			return kindEnum
		case *ir.Struct:
			return kindTable
		case *ir.Union:
			return kindUnion
		}
	}
	return 0
}

// fixedBuildLayout walks a fixed root pre-order and returns its layout bytes
// (docs/FIXED-FORM-ALGORITHM.md §1). It is the compile-time walk a port does
// once and bakes as static data (§4.2).
func fixedBuildLayout(st *ir.Struct) []byte {
	var entries []fixedLayoutEntry
	var walk func(*ir.Struct) uint32
	walk = func(s *ir.Struct) uint32 {
		start := uint32(len(entries))
		entries = append(entries, fixedLayoutEntry{
			id:   fnv1a64(s.WireName()),
			kind: kindTable,
			size: uint32(fixedTypeBytes(s)),
		})
		e := &entries[start]
		for _, f := range s.Fields {
			c := fixedChildCount(f)
			e.children += c
			fixedWalkField(f, &entries)
		}
		return e.size
	}
	walk(st)
	return fixedLayoutBytes(entries)
}

// fnv1a64 mirrors ir/tablewire.go's id hash (docs/FIXED-FORM-ALGORITHM.md §1):
// the wire name's 64-bit FNV-1a.
func fnv1a64(s string) uint64 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 0x100000001b3
	}
	return h
}

// fixedStorageBytes is the declared storage width of a scalar leaf
// (docs/FIXED-FORM-ALGORITHM.md §3, the C table).
func fixedStorageBytes(t ir.FieldType) int64 {
	switch t.Kind {
	case ir.TBool:
		return 1
	case ir.TInt, ir.TFixed:
		if t.Width == 0 {
			return 1
		}
		return int64(t.Width / 8)
	case ir.TBits:
		if t.Width <= 32 {
			return 4
		}
		return 8
	case ir.TFloat32:
		return 4
	case ir.TFloat64:
		return 8
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return int64(r.StorageBits / 8)
		case *ir.Flags:
			return 8
		}
	}
	return 0
}

// fixedElementBytes is C of one element of a field's type
// (docs/FIXED-FORM-ALGORITHM.md §3).
func fixedElementBytes(f *ir.Field) int64 {
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return fixedTypeBytes(r)
		case *ir.Union:
			widest := int64(0)
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if n := fixedFieldBytes(v.F); n > widest {
					widest = n
				}
			}
			return int64(ir.StorageBitsFor(r.Max)/8) + widest
		}
	}
	return fixedStorageBytes(f.Type)
}

// fixedFieldBytes is C(f), a field's constant body size
// (docs/FIXED-FORM-ALGORITHM.md §3).
func fixedFieldBytes(f *ir.Field) int64 {
	var payload int64
	switch {
	case f.KeyEnum != "":
		payload = f.KeyEnumRef.Max * fixedElementBytes(f)
	case f.Array == ir.ArrayFixed:
		payload = f.ArrayBound * fixedElementBytes(f)
	case f.Array == ir.ArrayCounted:
		payload = 4 + f.ArrayBound*fixedElementBytes(f)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TBytes:
		payload = 4 + f.Type.Size
	case f.Type.Kind == ir.TWString:
		payload = 4 + 2*f.Type.Size
	default:
		payload = fixedElementBytes(f)
	}
	if f.Type.Optional {
		return 1 + payload
	}
	return payload
}

// fixedTypeBytes is C(T): the sum of a struct's fields' constant sizes
// (docs/FIXED-FORM-ALGORITHM.md §3).
func fixedTypeBytes(st *ir.Struct) int64 {
	var n int64
	for _, f := range st.Fields {
		n += fixedFieldBytes(f)
	}
	return n
}

// fixedChildCount is the layout-entry child count of a field (§1's table).
func fixedChildCount(f *ir.Field) uint32 {
	switch {
	case f.Type.Optional:
		return 1
	case f.KeyEnum != "":
		return 2
	case f.Array != ir.ArrayNone:
		return 1
	case f.Type.Kind == ir.TBytes:
		return 1
	case f.Type.Kind == ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			return uint32(len(r.Fields))
		case *ir.Union:
			return uint32(len(r.Variants))
		}
	}
	return 0
}

// fixedWalkField appends the layout entries for one field, pre-order (§1).
func fixedWalkField(f *ir.Field, entries *[]fixedLayoutEntry) {
	id := fnv1a64(ir.TableFieldWireName(f))
	size := fixedFieldBytes(f)
	if f.Type.Optional {
		*entries = append(*entries, fixedLayoutEntry{id: id, kind: kindOptional, size: uint32(size), children: 1})
		fixedWalkPayload(f, id, uint32(size-1), entries)
		return
	}
	fixedWalkPayload(f, id, uint32(size), entries)
}

// fixedWalkPayload appends the child entries of a field's payload.
func fixedWalkPayload(f *ir.Field, id uint64, size uint32, entries *[]fixedLayoutEntry) {
	if f.KeyEnum != "" {
		*entries = append(*entries, fixedLayoutEntry{id: id, kind: kindKeyed, size: size, children: 2})
		*entries = append(*entries, fixedLayoutEntry{id: fnv1a64(f.KeyEnum), kind: kindEnum, size: uint32(f.KeyEnumRef.StorageBits / 8), children: uint32(len(f.KeyEnumRef.Variants))})
		for i := range f.KeyEnumRef.Variants {
			*entries = append(*entries, fixedLayoutEntry{id: fnv1a64(f.KeyEnumRef.VariantWireName(i)), kind: kindVariant, size: 0, children: 0})
		}
		fixedWalkElement(f, entries)
		return
	}
	if f.Array != ir.ArrayNone {
		*entries = append(*entries, fixedLayoutEntry{id: id, kind: kindArray, size: size, children: 1})
		fixedWalkElement(f, entries)
		return
	}
	if f.Type.Kind == ir.TBytes {
		*entries = append(*entries, fixedLayoutEntry{id: id, kind: kindArray, size: size, children: 1})
		*entries = append(*entries, fixedLayoutEntry{id: 0, kind: kindU8, size: 1, children: 0})
		return
	}
	if f.Type.Kind == ir.TString {
		*entries = append(*entries, fixedLayoutEntry{id: id, kind: kindString, size: size, children: 0})
		return
	}
	if f.Type.Kind == ir.TWString {
		*entries = append(*entries, fixedLayoutEntry{id: id, kind: kindWString, size: size, children: 0})
		return
	}
	if f.Type.Kind != ir.TNamed {
		*entries = append(*entries, fixedLayoutEntry{id: id, kind: fixedKind(f), size: size, children: 0})
		return
	}
	fixedWalkElement(f, entries)
}

// fixedWalkElement appends the entry of a field's element (array element,
// keyed element, or a leaf/named-field payload).
func fixedWalkElement(f *ir.Field, entries *[]fixedLayoutEntry) {
	if f.Type.Kind != ir.TNamed {
		return
	}
	switch r := f.Type.Ref.(type) {
	case *ir.Struct:
		*entries = append(*entries, fixedLayoutEntry{id: fnv1a64(r.WireName()), kind: kindTable, size: uint32(fixedTypeBytes(r)), children: uint32(len(r.Fields))})
		for _, sub := range r.Fields {
			fixedWalkField(sub, entries)
		}
	case *ir.Union:
		*entries = append(*entries, fixedLayoutEntry{id: fnv1a64(r.Name), kind: kindUnion, size: uint32(fixedElementBytes(f)), children: uint32(len(r.Variants))})
		for _, v := range r.Variants {
			if v.F == nil {
				*entries = append(*entries, fixedLayoutEntry{id: fnv1a64(v.WireName()), kind: kindVariant, size: 0, children: 0})
				continue
			}
			fixedWalkField(v.F, entries)
		}
	case *ir.Enum:
		*entries = append(*entries, fixedLayoutEntry{id: fnv1a64(r.Name), kind: kindEnum, size: uint32(r.StorageBits / 8), children: uint32(len(r.Variants))})
		for i := range r.Variants {
			*entries = append(*entries, fixedLayoutEntry{id: fnv1a64(r.VariantWireName(i)), kind: kindVariant, size: 0, children: 0})
		}
	}
}

// A Writer is the emit sink this card needs: the port-design skeleton's
// *goose.Gen satisfies it (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT).
type Writer interface {
	Pf(format string, args ...any)
}

// EmitFixedForm is this card's contribution to each fixed root: the layout,
// the hash and the baked constants (docs/PORT-GOOSE.md, THE RUNTIME LAYOUT;
// docs/FIXED-FORM-ALGORITHM.md §5.2).
func EmitFixedForm(w Writer, st *ir.Struct) error {
	layout := fixedBuildLayout(st)
	hash := ir.TableFixedLayoutHash(layout, st)
	body := fixedTypeBytes(st)

	w.Pf("// ---- THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4) ----\n")
	w.Pf("let %sFixedBodySize: i64 = %d;\n", st.Name, body)
	w.Pf("let %sFixedHash: u64 = 0x%x;\n", st.Name, hash)
	w.Pf("let %sFixedLayout: const u8[:] = \"", st.Name)
	var sb strings.Builder
	for _, b := range layout {
		sb.WriteString(fmt.Sprintf("\\x%02x", b))
	}
	w.Pf("%s\";\n", sb.String())
	w.Pf("let %sFixedLayoutLength: u32 = %d;\n", st.Name, len(layout))
	return nil
}
