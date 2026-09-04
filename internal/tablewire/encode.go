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

// Encode is the root instance's wire bytes and nothing else
// (docs/SPEC-TABLES.md §3): the FORM BYTE, the ROOT BODY, and the ID TABLE, in
// that order. No magic, no content hash, no protocol id, no length prefix
// around the whole.
func Encode(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	g, err := Number(m, inst)
	if err != nil {
		return nil, err
	}
	e := &encoder{m: m, g: g, ids: newIdTable()}
	fields, err := encodeBodyFields(e, inst)
	if err != nil {
		return nil, err
	}
	if len(g.Records()) > 0 {
		// a root that reaches no nodes writes none of them, like every other
		// empty thing (§3) — and a VALUE-ONLY table moves not one byte for any
		// of this (§3.1)
		fields, err = e.appendNodeTable(fields, g)
		if err != nil {
			return nil, err
		}
	}
	out := make([]byte, 0, 1+len(fields)+1+8*len(e.ids.ids)+8)
	out = append(out, ir.TableWireForm)
	out = append(out, fields...)
	out = append(out, 0) // the root body ENDS AT ITS OWN ZERO REFERENCE
	// THE ID TABLE IS THE LAST THING IN THE FILE, and a reader finds it from
	// the END: the entries, each a fixed little-endian u64, then the ENTRY
	// COUNT, the one fixed-width number on the wire (§3).
	for _, id := range e.ids.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(e.ids.ids))), nil
}

// encoder is one save's context: the closure the walks run over, the
// NUMBERING every pointer slot resolves through, and the file's ONE id table.
// The numbering is derived once per save from the graph and never carried on
// the instance (§3.1); the id table is built by the walk that writes the body,
// because first-use order is known only when the walk ends (§3).
type encoder struct {
	m   *tabletext.Model
	g   *NodeGraph
	ids *idTable
}

// There is no Measure here on purpose. §9's measure/save split exists so a
// build can size subtables on N workers and scatter-write disjoint ranges; this
// engine writes one buffer on one goroutine and builds each nested body before
// the length that frames it, so a Measure beside it could only be
// `len(Encode(...))` — and a gate that asserts that against itself cannot fail.
// The measure == save invariant is held where it is load-bearing, on the
// generated code, by the mandatory battery there.

type buf struct{ b []byte }

func (w *buf) u8(v uint8)   { w.b = append(w.b, v) }
func (w *buf) u16(v uint16) { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *buf) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *buf) u64(v uint64) { w.b = binary.LittleEndian.AppendUint64(w.b, v) }
func (w *buf) raw(v []byte) { w.b = append(w.b, v...) }

// leb writes one length, count, index or id reference in its one canonical
// spelling (§3).
func (w *buf) leb(v uint64) { w.b = appendLeb(w.b, v) }

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

// encodeBody is one table body: its fields in declaration order, then the
// one-byte zero reference that terminates it.
func encodeBody(e *encoder, inst *tabletext.Instance) ([]byte, error) {
	b, err := encodeBodyFields(e, inst)
	if err != nil {
		return nil, err
	}
	return append(b, 0), nil
}

// encodeBodyFields is a body WITHOUT its terminator: the root owes a node
// table after its own fields and before the terminator (§3.1), so the two
// halves are separable.
func encodeBodyFields(e *encoder, inst *tabletext.Instance) ([]byte, error) {
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
	return w.b, nil
}

// header writes a field header — the id REFERENCE and the kind byte, which is
// the whole of it (§3).
func (e *encoder) header(w *buf, id uint64, kind int) {
	w.leb(e.ids.ref(id))
	w.u8(uint8(kind))
}

func encodeField(e *encoder, w *buf, inst *tabletext.Instance, fv *tabletext.Field) error {
	f := fv.Def
	id := ir.TableFieldWireId(f)
	kind := ir.TableWireScalarKind(f)

	if f.Type.Pointer && f.Array == ir.ArrayNone {
		// A pointer to a table rides as `id reference, kind = 17, index`
		// (docs/SPEC-TABLES.md §3.1). NULL IS INDEX 0, AND NULL IS ELIDED: absence
		// and null are one value, because a pointer takes no specified default.
		// A non-null pointer ALWAYS rides, even when its node's body is
		// entirely default, or null and "points at an empty node" would be one
		// value on the wire. A BYTE BUFFER's slot is the same three parts, and
		// a zero-length blob rides for the same reason (§2.5).
		index := e.g.Index(fv.Cell.Node)
		if f.Type.Blob() {
			index = e.g.BlobIndex(fv.Cell.Blob)
		}
		if index == ir.NodeWireIndexNull {
			return nil
		}
		e.header(w, id, ir.TableKindPointer)
		w.leb(index)
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
			// count for the counted spelling — ZERO INCLUDED, the two-byte
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
			e.header(w, id, ir.TableKindTable)
			w.leb(uint64(len(body)))
			w.raw(body)
		default:
			return encodeSimple(e, w, f, id, kind, &fv.Cell)
		}
		return nil

	case f.KeyEnum != "":
		return encodeKeyed(e, w, fv, id, kind)

	case f.Type.Kind == ir.TString:
		if fv.Count == 0 {
			return nil
		}
		e.header(w, id, ir.TableKindString)
		w.leb(uint64(len(fv.Cell.Str)))
		w.raw(fv.Cell.Str)
		return nil

	case f.Type.Kind == ir.TBytes:
		if fv.Count == 0 {
			return nil
		}
		e.header(w, id, ir.TableKindArray)
		w.leb(uint64(2 + len(fv.Cell.Str)))
		w.u8(uint8(ir.TableKindU8)) // bytes ride as an array of u8 (§2.5)
		w.leb(uint64(len(fv.Cell.Str)))
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
			return nil // None elides — the absence of the field is the None
		}
		if int(fv.Cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", un.Name, fv.Cell.U)
		}
		arm := un.Variants[fv.Cell.U-1]
		w.leb(e.ids.ref(id))
		w.u8(uint8(ir.TableKindUnion))
		return encodeArmHeader(e, w, arm, &fv.Cell)

	case kind == ir.TableKindTable:
		// AN ELIDED FIELD COSTS NOTHING IN THE ID TABLE EITHER (§3), so the
		// walk interns the field's id, builds the body that decides, and undoes
		// both when the body turns out to be its terminator alone.
		mark := e.ids.mark()
		ref := e.ids.ref(id)
		body, err := encodeBody(e, subInstance(e, f, &fv.Cell))
		if err != nil {
			return err
		}
		if len(body) <= 1 {
			e.ids.rollback(mark) // an all-default nested table elides
			return nil
		}
		w.leb(ref)
		w.u8(uint8(ir.TableKindTable))
		w.leb(uint64(len(body)))
		w.raw(body)
		return nil

	default:
		if cellIsDefault(e, f, &fv.Cell) {
			return nil
		}
		return encodeSimple(e, w, f, id, kind, &fv.Cell)
	}
}

// encodeSimple writes a field whose payload is one element at its own kind's
// framing — a scalar, an enum reference, a flags mask.
func encodeSimple(e *encoder, w *buf, f *ir.Field, id uint64, kind int, cell *tabletext.Cell) error {
	if en := tabletext.EnumOf(f); en != nil {
		vid, none, err := variantWireId(en, cell.U, f.Name)
		if err != nil {
			return err
		}
		e.header(w, id, ir.TableKindEnum)
		w.leb(e.enumRef(vid, none))
		return nil
	}
	e.header(w, id, kind)
	return encodeElement(e, w, f, kind, cell)
}

// enumRef is an enum value's reference: the variant name's id, and the ZERO
// REFERENCE for None — the one value that names no id, so no declared variant
// can ever be mistaken for it (§3).
func (e *encoder) enumRef(id uint64, none bool) uint64 {
	if none {
		return 0
	}
	return e.ids.ref(id)
}

// encodeArray writes a positional array body: kind 14, then the element kind,
// the count, and the elements.
func encodeArray(e *encoder, w *buf, fv *tabletext.Field, id uint64, kind, count int) error {
	elemKind := kind
	if fv.Def.Type.Pointer {
		elemKind = ir.TableKindPointer
	}
	w.leb(e.ids.ref(id))
	w.u8(uint8(ir.TableKindArray))
	body := &buf{}
	body.u8(uint8(elemKind))
	body.leb(uint64(count))
	for i := range count {
		if err := encodeElement(e, body, fv.Def, elemKind, &fv.Elems[i]); err != nil {
			return err
		}
	}
	w.leb(uint64(len(body.b)))
	w.raw(body.b)
	return nil
}

// encodeKeyed writes an enum-keyed array (docs/SPEC-TABLES.md §3.2): kind 16, the
// body length, then one `(key reference, L, element body)` triple per PRESENT slot,
// ascending by variant ordinal. A slot holding its default elides exactly as a
// defaulted field does, an array with no present slot is not written at all,
// and None keys no slot, so nothing is stored for it and nothing rides (§2.4).
func encodeKeyed(e *encoder, w *buf, fv *tabletext.Field, id uint64, kind int) error {
	f := fv.Def
	mark := e.ids.mark()
	ref := e.ids.ref(id)
	body := &buf{}
	pairs := uint64(0)
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		cell := &fv.Elems[slot]
		// a slot no variant names has no wire identity: save refuses it rather
		// than renaming it silently (§3.2)
		keyID, none, err := variantWireId(f.KeyEnumRef, uint64(tabletext.KeyedSlotValue(f, slot)), f.Name)
		if err != nil {
			return err
		}
		if none {
			return fmt.Errorf("field %s: a None key names no slot (docs/SPEC-TABLES.md §3.2)", f.Name)
		}
		slotMark := e.ids.mark()
		keyRef := e.ids.ref(keyID)
		var elem []byte
		if kind == ir.TableKindTable {
			b, err := encodeBody(e, subInstance(e, f, cell))
			if err != nil {
				return err
			}
			if len(b) <= 1 {
				e.ids.rollback(slotMark) // an all-default slot elides
				continue
			}
			elem = b
		} else {
			if cellIsDefault(e, f, cell) {
				e.ids.rollback(slotMark) // a default slot elides
				continue
			}
			eb := &buf{}
			if err := encodeElement(e, eb, f, kind, cell); err != nil {
				return err
			}
			elem = eb.b
		}
		body.leb(keyRef)
		body.leb(uint64(len(elem)))
		body.raw(elem)
		pairs++
	}
	if pairs == 0 {
		e.ids.rollback(mark)
		return nil
	}
	w.leb(ref)
	w.u8(ir.TableKindKeyed)
	inner := &buf{}
	inner.u8(uint8(ir.TableWireElemKind(f)))
	inner.leb(pairs)
	inner.raw(body.b)
	w.leb(uint64(len(inner.b)))
	w.raw(inner.b)
	return nil
}

// encodeElement writes one element at its kind's own framing: a table element
// carries its own length, a scalar rides at its fixed width, and an enum rides
// as the reference to its variant name's id.
func encodeElement(e *encoder, w *buf, f *ir.Field, kind int, cell *tabletext.Cell) error {
	if en := tabletext.EnumOf(f); en != nil {
		vid, none, err := variantWireId(en, cell.U, f.Name)
		if err != nil {
			return err
		}
		w.leb(e.enumRef(vid, none))
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
		w.leb(uint64(len(body)))
		w.raw(body)
	case ir.TableKindPointer:
		// an element of an ARRAY OF POINTERS is a node index like any pointer
		// field's payload (docs/SPEC-TABLES.md §3.1): null rides as 0 in its slot
		w.leb(e.g.Index(cell.Node))
	case ir.TableKindUnion:
		// an element of an ARRAY OF UNIONS is the union payload in its place
		// (docs/SPEC-TABLES.md §3): the arm id reference, then the arm's own
		// kind byte, its L and its payload — and a None element rides as the
		// single zero byte, because position is identity in an array
		un := tabletext.UnionOf(f)
		if cell.U == 0 {
			w.leb(0)
			return nil
		}
		if int(cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", un.Name, cell.U)
		}
		return encodeArmHeader(e, w, un.Variants[cell.U-1], cell)
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

// encodeArmHeader writes a SET arm whole: AN ARM HEADER IS A FIELD HEADER
// (docs/SPEC-TABLES.md §3) — the arm id reference, the arm's KIND byte, `L`,
// then `L` bytes of arm payload. One framing serves a field and an arm, and
// the arm's kind byte is what makes a retyped arm an ordinary kind mismatch
// instead of a value read under the wrong rule.
func encodeArmHeader(e *encoder, w *buf, arm ir.UnionVariant, cell *tabletext.Cell) error {
	w.leb(e.ids.ref(ir.TableWireId(arm.Name)))
	w.u8(uint8(armWireKind(arm)))
	body, err := encodeArm(e, arm, cell)
	if err != nil {
		return err
	}
	w.leb(uint64(len(body)))
	w.raw(body)
	return nil
}

// armWireKind is the kind byte an arm header carries: kind 32 where the arm
// has NO PAYLOAD (§2.6), and otherwise the kind a FIELD of the arm's type
// takes.
func armWireKind(arm ir.UnionVariant) int {
	if arm.Void() {
		return ir.TableKindNoPayload
	}
	if arm.Body() {
		return ir.TableKindTable
	}
	f := arm.F
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return ir.TableKindPointer
	}
	if f.Type.Kind == ir.TBytes {
		return ir.TableKindArray
	}
	return ir.TableWireFieldKind(f)
}

// encodeArm writes ONE ARM's payload — the bytes under the arm's `L`
// (docs/SPEC-TABLES.md §3's arm payload table). AN ARM IS A FIELD LINE
// (§2.6), so the payload is exactly what a FIELD of that type puts after its
// own framing prefix: a table body for a declared type or table, the value at
// its width for a scalar, the bytes for a string, the array body for an array
// or a `bytes(N)`, a node index for a pointer, and the union payload in place
// for a nested union.
func encodeArm(e *encoder, arm ir.UnionVariant, cell *tabletext.Cell) ([]byte, error) {
	if arm.Void() {
		return nil, nil // a payload-free arm: kind 32 and L = 0 (§2.6, §3)
	}
	if arm.Body() {
		payload := cell.Tab
		if payload == nil {
			payload = e.m.New(arm.Ref)
		}
		return encodeBody(e, payload)
	}
	f := arm.F
	fv := cell.Arm
	if fv == nil {
		fv = e.m.NewArm(arm)
	}
	w := &buf{}
	kind := ir.TableWireScalarKind(f)
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index := e.g.Index(fv.Cell.Node)
		if f.Type.Blob() {
			index = e.g.BlobIndex(fv.Cell.Blob)
		}
		w.leb(index)
	case f.Type.Kind == ir.TString:
		w.raw(fv.Cell.Str)
	case f.Type.Kind == ir.TBytes:
		w.u8(uint8(ir.TableKindU8)) // bytes ride as an array of u8 (§2.5)
		w.leb(uint64(len(fv.Cell.Str)))
		w.raw(fv.Cell.Str)
	case f.Array != ir.ArrayNone:
		count := int(f.ArrayBound)
		if f.Array == ir.ArrayCounted {
			count = fv.Count
		}
		elemKind := kind
		if f.Type.Pointer {
			elemKind = ir.TableKindPointer
		}
		w.u8(uint8(elemKind))
		w.leb(uint64(count))
		for i := range count {
			if err := encodeElement(e, w, f, elemKind, &fv.Elems[i]); err != nil {
				return nil, err
			}
		}
	case tabletext.UnionOf(f) != nil:
		inner := tabletext.UnionOf(f)
		if fv.Cell.U == 0 {
			w.leb(0) // the inner union's None: L = 1 and that one zero byte (§3)
			break
		}
		if int(fv.Cell.U) > len(inner.Variants) {
			return nil, fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", inner.Name, fv.Cell.U)
		}
		if err := encodeArmHeader(e, w, inner.Variants[fv.Cell.U-1], &fv.Cell); err != nil {
			return nil, err
		}
	default:
		if en := tabletext.EnumOf(f); en != nil {
			vid, none, err := variantWireId(en, fv.Cell.U, f.Name)
			if err != nil {
				return nil, err
			}
			w.leb(e.enumRef(vid, none))
			break
		}
		if err := encodeElement(e, w, f, kind, &fv.Cell); err != nil {
			return nil, err
		}
	}
	return w.b, nil
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
// NAME, and `none` for the enum's None, which is the zero reference and names
// no id (§3). A value no variant names has no identity, and save returns
// failure rather than writing None over it (§5).
func variantWireId(e *ir.Enum, value uint64, field string) (id uint64, none bool, err error) {
	name := tabletext.EnumName(e, int64(value))
	if name == "" {
		return 0, false, fmt.Errorf("field %s: enum %s value %d names no variant, so it has no wire identity — save refuses it (docs/SPEC-TABLES.md §5)", field, e.Name, value)
	}
	if name == "None" {
		return 0, true, nil
	}
	return ir.TableWireId(name), false, nil
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
