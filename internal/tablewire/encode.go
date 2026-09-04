// Package tablewire is the compiler-side encoder and decoder for the neutral
// TABLE wire (docs/SPEC-TABLES.md §3), over the instance model in tabletext.
//
// It is TOOLING, never a runtime: `schema pack` (§17) is a Go command and a
// compiler cannot execute the code it emits, so the packer carries its own
// engine and the goldens hold it to the generated C++ byte for byte. Every
// framing and elision decision here mirrors internal/codegen/cpptable — a port
// never invents a contract.
package tablewire

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Encode is the root instance's wire bytes and nothing else (docs/SPEC-TABLES.md
// §17.2): no magic, no content hash, no protocol id, no length prefix around
// the whole.
func Encode(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	g, err := Number(m, inst)
	if err != nil {
		return nil, err
	}
	e := &encoder{m: m, g: g}
	body, err := encodeBody(e, inst)
	if err != nil {
		return nil, err
	}
	if len(g.Records()) == 0 {
		// a root that reaches no nodes writes none of them, like every other
		// empty thing (§3) — and a VALUE-ONLY table moves not one byte for any
		// of this (§3.1)
		return body, nil
	}
	return e.appendNodeTable(body, g)
}

// encoder is one save's context: the closure the walks run over, and the
// NUMBERING every pointer slot resolves through. The numbering is derived once
// per save from the graph and never carried on the instance (§3.1).
type encoder struct {
	m *tabletext.Model
	g *NodeGraph
}

// There is no Measure here on purpose. §9's measure/save split exists so a
// build can size subtables on N workers and scatter-write disjoint ranges; this
// engine writes one buffer on one goroutine and patches its own length prefixes
// as it appends, so a Measure beside it could only be `len(Encode(...))` — and
// a gate that asserts that against itself cannot fail. The measure == save
// invariant is held where it is load-bearing, on the generated code, by the
// mandatory battery there.

type buf struct{ b []byte }

func (w *buf) u8(v uint8)   { w.b = append(w.b, v) }
func (w *buf) u16(v uint16) { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *buf) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *buf) u64(v uint64) { w.b = binary.LittleEndian.AppendUint64(w.b, v) }
func (w *buf) raw(v []byte) { w.b = append(w.b, v...) }

// width writes the low `n` bytes of a value, which is what the generated code's
// `w.put8( uint8_t( value ) )` does for a storage wider than its wire kind —
// `bits(6)` rides in a u8 (§3).
func (w *buf) width(n int, v uint64) {
	switch n {
	case 1:
		w.u8(uint8(v))
	case 2:
		w.u16(uint16(v))
	case 4:
		w.u32(uint32(v))
	case 8:
		w.u64(v)
	}
}

// encodeBody is one table body: its fields in declaration order, then the u16
// zero terminator.
func encodeBody(e *encoder, inst *tabletext.Instance) ([]byte, error) {
	w := &buf{}
	guards := tabletext.Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if err := encodeField(e, w, inst, fv); err != nil {
			return nil, err
		}
	}
	w.u16(0) // terminator
	return w.b, nil
}

func encodeField(e *encoder, w *buf, inst *tabletext.Instance, fv *tabletext.Field) error {
	f := fv.Def
	id := ir.TableFieldId(f)
	kind := ir.TableScalarKind(f)

	if f.Type.Pointer && f.Array == ir.ArrayNone {
		// A pointer to a table rides as `id (u16), kind = 17, index (u32)`
		// (docs/SPEC-TABLES.md §3.1). NULL IS INDEX 0, AND NULL IS ELIDED: absence
		// and null are one value, because a pointer takes no specified default.
		// A non-null pointer ALWAYS rides, even when its node's body is
		// entirely default, or null and "points at an empty node" would be one
		// value on the wire. A BYTE BUFFER's slot is the same seven bytes, and
		// a zero-length blob rides for the same reason (§2.5).
		index := e.g.Index(fv.Cell.Node)
		if f.Type.Blob() {
			index = e.g.BlobIndex(fv.Cell.Blob)
		}
		if index == ir.NodeIndexNull {
			return nil
		}
		w.u16(id)
		w.u8(uint8(ir.TableKindPointer))
		w.u32(index)
		return nil
	}

	switch {
	case f.Type.Optional:
		// PRESENCE is the payload: a present optional ALWAYS rides, even when
		// its value is entirely default — otherwise absent and
		// present-with-nothing-to-say would be one value on the wire (§2.3)
		if !fv.Present {
			return nil
		}
		switch {
		case f.Array != ir.ArrayNone:
			// an OPTIONAL ARRAY rides the array framing whole (§2.3): the live
			// count for the counted spelling — ZERO INCLUDED, the five-byte
			// body — and every declared element for the fixed one. No content
			// test anywhere: presence already decided.
			count := int(f.ArrayBound)
			if f.Array == ir.ArrayCounted {
				count = fv.Count
			}
			return encodeArray(e, w, fv, id, kind, count)
		case kind == ir.TableKindTable:
			body, err := encodeBody(e, subInstance(e, f, &fv.Cell))
			if err != nil {
				return err
			}
			w.u16(id)
			w.u8(uint8(ir.TableKindTable))
			w.u32(uint32(len(body)))
			w.raw(body)
		case tabletext.EnumOf(f) != nil:
			vid, err := variantWireId(tabletext.EnumOf(f), fv.Cell.U, f.Name)
			if err != nil {
				return err
			}
			w.u16(id)
			w.u8(uint8(kind))
			w.u16(vid)
		default:
			w.u16(id)
			w.u8(uint8(kind))
			if err := encodeElement(e, w, f, kind, &fv.Cell); err != nil {
				return err
			}
		}
		return nil

	case f.KeyEnum != "":
		return encodeKeyed(e, w, fv, id, kind)

	case f.Type.Kind == ir.TString:
		if fv.Count == 0 {
			return nil
		}
		w.u16(id)
		w.u8(uint8(ir.TableKindString))
		w.u32(uint32(len(fv.Cell.Str)))
		w.raw(fv.Cell.Str)
		return nil

	case f.Type.Kind == ir.TBytes:
		if fv.Count == 0 {
			return nil
		}
		w.u16(id)
		w.u8(uint8(ir.TableKindArray))
		w.u32(uint32(5 + len(fv.Cell.Str)))
		w.u8(uint8(ir.TableKindU8)) // bytes ride as an array of u8 (§2.5)
		w.u32(uint32(len(fv.Cell.Str)))
		w.raw(fv.Cell.Str)
		return nil

	case f.Array == ir.ArrayCounted:
		if fv.Count == 0 {
			return nil
		}
		return encodeArray(e, w, fv, id, kind, fv.Count)

	case f.Array == ir.ArrayFixed && kind == ir.TableKindTable:
		// a fixed array of tables always rides — position is identity there,
		// so no element-default compare can elide one
		return encodeArray(e, w, fv, id, kind, int(f.ArrayBound))

	case f.Array == ir.ArrayFixed:
		// a fixed array is positional; an all-default array elides entirely,
		// parity with the reader's prefill
		allDefault := true
		for i := 0; i < int(f.ArrayBound); i++ {
			if !cellIsDefault(e, f, &fv.Elems[i]) {
				allDefault = false
				break
			}
		}
		if allDefault {
			return nil
		}
		return encodeArray(e, w, fv, id, kind, int(f.ArrayBound))

	case kind == ir.TableKindUnion:
		un := tabletext.UnionOf(f)
		if fv.Cell.U == 0 {
			return nil // None elides — TLV absence is the None
		}
		if int(fv.Cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", un.Name, fv.Cell.U)
		}
		arm := un.Variants[fv.Cell.U-1]
		payload := fv.Cell.Tab
		if payload == nil {
			payload = e.m.New(arm.Ref)
		}
		body, err := encodeBody(e, payload)
		if err != nil {
			return err
		}
		w.u16(id)
		w.u8(uint8(ir.TableKindUnion))
		// the ARM ID is the hash of the arm's NAME (§5), so arms may be added
		// anywhere, removed and reordered
		w.u16(ir.VariantId(arm.Name))
		w.u32(uint32(len(body)))
		w.raw(body)
		return nil

	case kind == ir.TableKindTable:
		body, err := encodeBody(e, subInstance(e, f, &fv.Cell))
		if err != nil {
			return err
		}
		if len(body) <= 2 {
			return nil // an all-default nested table elides
		}
		w.u16(id)
		w.u8(uint8(ir.TableKindTable))
		w.u32(uint32(len(body)))
		w.raw(body)
		return nil

	case tabletext.EnumOf(f) != nil:
		if cellIsDefault(e, f, &fv.Cell) {
			return nil
		}
		vid, err := variantWireId(tabletext.EnumOf(f), fv.Cell.U, f.Name)
		if err != nil {
			return err
		}
		w.u16(id)
		w.u8(uint8(kind))
		w.u16(vid)
		return nil

	default:
		if cellIsDefault(e, f, &fv.Cell) {
			return nil
		}
		w.u16(id)
		w.u8(uint8(kind))
		return encodeElement(e, w, f, kind, &fv.Cell)
	}
}

// encodeArray writes a positional array body: kind 14, then the element kind,
// the count, and the elements.
func encodeArray(e *encoder, w *buf, fv *tabletext.Field, id uint16, kind, count int) error {
	body := &buf{}
	body.u8(uint8(kind))
	body.u32(uint32(count))
	for i := range count {
		if err := encodeElement(e, body, fv.Def, kind, &fv.Elems[i]); err != nil {
			return err
		}
	}
	w.u16(id)
	w.u8(uint8(ir.TableKindArray))
	w.u32(uint32(len(body.b)))
	w.raw(body.b)
	return nil
}

// encodeKeyed writes an enum-keyed array (docs/SPEC-TABLES.md §3.2): kind 16, the
// body length, then one `(variant id, L, element body)` pair per PRESENT slot,
// ascending by variant ordinal. A slot holding its default elides exactly as a
// defaulted field does, an array with no present slot is not written at all,
// and None keys no slot, so nothing is stored for it and nothing rides (§2.4).
func encodeKeyed(e *encoder, w *buf, fv *tabletext.Field, id uint16, kind int) error {
	f := fv.Def
	body := &buf{}
	pairs := uint32(0)
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		cell := &fv.Elems[slot]
		var elem []byte
		if kind == ir.TableKindTable {
			b, err := encodeBody(e, subInstance(e, f, cell))
			if err != nil {
				return err
			}
			if len(b) <= 2 {
				continue // an all-default slot elides
			}
			elem = b
		} else {
			if cellIsDefault(e, f, cell) {
				continue // a default slot elides
			}
			eb := &buf{}
			if err := encodeElement(e, eb, f, kind, cell); err != nil {
				return err
			}
			elem = eb.b
		}
		// a slot no variant names has no wire identity: save refuses it rather
		// than renaming it silently (§3.2)
		keyID, err := variantWireId(f.KeyEnumRef, uint64(tabletext.KeyedSlotValue(f, slot)), f.Name)
		if err != nil {
			return err
		}
		body.u16(keyID)
		body.u32(uint32(len(elem)))
		body.raw(elem)
		pairs++
	}
	if pairs == 0 {
		return nil
	}
	w.u16(id)
	w.u8(ir.TableKindKeyed)
	w.u32(uint32(5 + len(body.b)))
	w.u8(uint8(kind))
	w.u32(pairs)
	w.raw(body.b)
	return nil
}

// encodeElement writes one element at its kind's own framing: a table element
// carries its own length, a scalar rides at its fixed width, and an enum rides
// as the u16 hash of its variant name.
func encodeElement(e *encoder, w *buf, f *ir.Field, kind int, cell *tabletext.Cell) error {
	if e := tabletext.EnumOf(f); e != nil {
		vid, err := variantWireId(e, cell.U, f.Name)
		if err != nil {
			return err
		}
		w.u16(vid)
		return nil
	}
	switch kind {
	case ir.TableKindBool:
		if cell.B {
			w.u8(1)
		} else {
			w.u8(0)
		}
	case ir.TableKindF32:
		w.u32(narrowF32(cell.F))
	case ir.TableKindF64:
		w.u64(math.Float64bits(cell.F))
	case ir.TableKindTable:
		body, err := encodeBody(e, subInstance(e, f, cell))
		if err != nil {
			return err
		}
		w.u32(uint32(len(body)))
		w.raw(body)
	case ir.TableKindPointer:
		// an element of an ARRAY OF POINTERS is a node index like any pointer
		// field's payload (docs/SPEC-TABLES.md §3.1): null rides as 0 in its slot
		w.u32(e.g.Index(cell.Node))
	case ir.TableKindUnion:
		// an element of an ARRAY OF UNIONS is the union payload in its place
		// (docs/SPEC-TABLES.md §3): the arm id, then the arm length-prefixed —
		// and a None element rides as arm id 0, because position is identity
		// in an array
		un := tabletext.UnionOf(f)
		if cell.U == 0 {
			w.u16(0)
			return nil
		}
		if int(cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", un.Name, cell.U)
		}
		arm := un.Variants[cell.U-1]
		payload := cell.Tab
		if payload == nil {
			payload = e.m.New(arm.Ref)
		}
		body, err := encodeBody(e, payload)
		if err != nil {
			return err
		}
		w.u16(ir.VariantId(arm.Name))
		w.u32(uint32(len(body)))
		w.raw(body)
	default:
		if ir.TableKindWide(kind) {
			// the raw integer at the kind's width, two's complement, little-endian —
			// sixteen bytes low half first for the 128-bit kinds, the type wire's
			// own order (docs/SPEC-TABLES.md §3)
			w.raw(tabletext.WideBytes(tabletext.WideValue(cell), kind))
			return nil
		}
		w.width(tabletext.KindWidth(kind), cell.U)
	}
	return nil
}

// subInstance is a nested table or type's instance, materialized at its
// declared defaults when nothing placed one.
func subInstance(e *encoder, f *ir.Field, cell *tabletext.Cell) *tabletext.Instance {
	if cell.Tab != nil {
		return cell.Tab
	}
	return e.m.New(tabletext.StructOf(f))
}

// variantWireId is an enum value's wire identity: the hash of its VARIANT
// NAME, with None's reserved 0. A value no variant names has none, and save
// returns failure rather than writing None over it (§5).
func variantWireId(e *ir.Enum, value uint64, field string) (uint16, error) {
	name := tabletext.EnumName(e, int64(value))
	if name == "" {
		return 0, fmt.Errorf("field %s: enum %s value %d names no variant, so it has no wire identity — save refuses it (docs/SPEC-TABLES.md §5)", field, e.Name, value)
	}
	if name == "None" {
		return 0, nil
	}
	return ir.VariantId(name), nil
}

// cellIsDefault is the writer's elision test: a field holding its declared
// default is not written at all, which is why old readers and new writers meet
// cleanly (§3).
func cellIsDefault(e *encoder, f *ir.Field, cell *tabletext.Cell) bool {
	if f.Type.Pointer {
		return cell.Node == nil // null is a pointer slot's only default (§2.1)
	}
	def := e.m.FieldDefaultCell(f)
	switch {
	case f.Type.Kind == ir.TBool:
		return cell.B == def.B
	case f.Type.Kind == ir.TFloat32:
		return float32(cell.F) == float32(def.F)
	case f.Type.Kind == ir.TFloat64:
		return cell.F == def.F
	case ir.TableKindWide(ir.TableScalarKind(f)):
		return tabletext.WideValue(cell).Cmp(tabletext.WideValue(&def)) == 0
	case f.Type.Kind == ir.TInt && f.Type.Signed:
		return cell.I == def.I
	}
	return cell.U == def.U
}

// A float32 NaN's PAYLOAD IS DATA the reference carries bit for bit, and the
// hardware conversions between float32 and float64 set the quiet bit on a
// signaling one — so a NaN crosses the two widths by bit surgery instead, the
// 23 payload bits riding in the top of the double's 52. The round trip through
// these two is exact for every float32 NaN.

func widenF32NaN(bits uint32) float64 {
	sign := uint64(bits>>31) << 63
	payload := uint64(bits&0x007FFFFF) << 29
	return math.Float64frombits(sign | 0x7FF0000000000000 | payload)
}

func narrowF32(v float64) uint32 {
	b := math.Float64bits(v)
	if b&0x7FF0000000000000 == 0x7FF0000000000000 && b&0x000FFFFFFFFFFFFF != 0 {
		sign := uint32(b>>63) << 31
		payload := uint32((b >> 29) & 0x007FFFFF)
		if payload == 0 {
			payload = 0x00400000 // a payload below float32's bits still reads NaN, never infinity
		}
		return sign | 0x7F800000 | payload
	}
	return math.Float32bits(float32(v))
}
