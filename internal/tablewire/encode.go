// Package tablewire is the compiler-side encoder and decoder for the neutral
// TABLE wire (SPEC-TABLES.md §3), over the instance model in tabletext.
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

// KindKeyed is the enum-keyed array's OWN wire kind (SPEC-TABLES.md §3.2). Its
// body opens as an array's does — `element kind (u8)`, then a u32 count — but
// the count counts SLOTS PRESENT and each element is a `(variant id u16, L u32,
// element)` pair, ascending by variant ordinal, defaults elided per slot, and
// None's slot never riding. A positional array stays kind 14, so the two
// incompatible layouts are told apart by the kind byte alone and the
// `[E]T` <-> `[E.Max + 1]T` edit is an ordinary kind mismatch.
const KindKeyed = 16

// Encode is the root instance's wire bytes and nothing else (SPEC-TABLES.md
// §17.2): no magic, no content hash, no protocol id, no length prefix around
// the whole.
func Encode(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	if err := RefuseVariable(m, inst.Def); err != nil {
		return nil, err
	}
	return encodeBody(m, inst)
}

// There is no Measure here on purpose. §9's measure/save split exists so a
// build can size subtables on N workers and scatter-write disjoint ranges; this
// engine writes one buffer on one goroutine and patches its own length prefixes
// as it appends, so a Measure beside it could only be `len(Encode(...))` — and
// a gate that asserts that against itself cannot fail. The measure == save
// invariant is held where it is load-bearing, on the generated code, by the
// mandatory battery there.

// RefuseVariable refuses a VARIABLE-LENGTH root by name. A pointer-bearing
// table is never held by value: it is built through an arena and read through a
// region (§6.2), and its text form reads through the builder — a named
// follow-on the generated C++ does not emit either (§16.2).
func RefuseVariable(m *tabletext.Model, st *ir.Struct) error {
	if ir.VariableTables(m.Unit)[st.Name] {
		return fmt.Errorf("%s is VARIABLE-LENGTH — a pointer in its by-value closure — and the text form of one reads through its builder, a named follow-on (SPEC-TABLES.md §16.2, §15); pack a fixed-size root", st.Name)
	}
	return nil
}

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
func encodeBody(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	w := &buf{}
	guards := tabletext.Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if err := encodeField(m, w, inst, fv); err != nil {
			return nil, err
		}
	}
	w.u16(0) // terminator
	return w.b, nil
}

func encodeField(m *tabletext.Model, w *buf, inst *tabletext.Instance, fv *tabletext.Field) error {
	f := fv.Def
	id := ir.TableFieldId(f)
	kind := tabletext.ScalarKind(f)

	if f.Type.Pointer {
		return fmt.Errorf("field %s is a pointer — the pack engine holds fixed-size roots only (SPEC-TABLES.md §16.2)", f.Name)
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
		case kind == tabletext.KindTable:
			body, err := encodeBody(m, subInstance(m, f, &fv.Cell))
			if err != nil {
				return err
			}
			w.u16(id)
			w.u8(uint8(tabletext.KindTable))
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
			if err := encodeElement(m, w, f, kind, &fv.Cell); err != nil {
				return err
			}
		}
		return nil

	case f.KeyEnum != "":
		return encodeKeyed(m, w, fv, id, kind)

	case f.Type.Kind == ir.TString:
		if fv.Count == 0 {
			return nil
		}
		w.u16(id)
		w.u8(uint8(tabletext.KindString))
		w.u32(uint32(len(fv.Cell.Str)))
		w.raw(fv.Cell.Str)
		return nil

	case f.Type.Kind == ir.TBytes:
		if fv.Count == 0 {
			return nil
		}
		w.u16(id)
		w.u8(uint8(tabletext.KindArray))
		w.u32(uint32(5 + len(fv.Cell.Str)))
		w.u8(uint8(tabletext.KindU8)) // bytes ride as an array of u8 (§2.5)
		w.u32(uint32(len(fv.Cell.Str)))
		w.raw(fv.Cell.Str)
		return nil

	case f.Array == ir.ArrayCounted:
		if fv.Count == 0 {
			return nil
		}
		return encodeArray(m, w, fv, id, kind, fv.Count)

	case f.Array == ir.ArrayFixed && kind == tabletext.KindTable:
		// a fixed array of tables always rides — position is identity there,
		// so no element-default compare can elide one
		return encodeArray(m, w, fv, id, kind, int(f.ArrayBound))

	case f.Array == ir.ArrayFixed:
		// a fixed array is positional; an all-default array elides entirely,
		// parity with the reader's prefill
		allDefault := true
		for i := 0; i < int(f.ArrayBound); i++ {
			if !cellIsDefault(m, f, &fv.Elems[i]) {
				allDefault = false
				break
			}
		}
		if allDefault {
			return nil
		}
		return encodeArray(m, w, fv, id, kind, int(f.ArrayBound))

	case kind == tabletext.KindUnion:
		un := tabletext.UnionOf(f)
		if fv.Cell.U == 0 {
			return nil // None elides — TLV absence is the None
		}
		if int(fv.Cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (SPEC-TABLES.md §5)", un.Name, fv.Cell.U)
		}
		arm := un.Variants[fv.Cell.U-1]
		payload := fv.Cell.Tab
		if payload == nil {
			payload = m.New(arm.Ref)
		}
		body, err := encodeBody(m, payload)
		if err != nil {
			return err
		}
		w.u16(id)
		w.u8(uint8(tabletext.KindUnion))
		// the ARM ID is the hash of the arm's NAME (§5), so arms may be added
		// anywhere, removed and reordered
		w.u16(ir.VariantId(arm.Name))
		w.u32(uint32(len(body)))
		w.raw(body)
		return nil

	case kind == tabletext.KindTable:
		body, err := encodeBody(m, subInstance(m, f, &fv.Cell))
		if err != nil {
			return err
		}
		if len(body) <= 2 {
			return nil // an all-default nested table elides
		}
		w.u16(id)
		w.u8(uint8(tabletext.KindTable))
		w.u32(uint32(len(body)))
		w.raw(body)
		return nil

	case tabletext.EnumOf(f) != nil:
		if cellIsDefault(m, f, &fv.Cell) {
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
		if cellIsDefault(m, f, &fv.Cell) {
			return nil
		}
		w.u16(id)
		w.u8(uint8(kind))
		return encodeElement(m, w, f, kind, &fv.Cell)
	}
}

// encodeArray writes a positional array body: kind 14, then the element kind,
// the count, and the elements.
func encodeArray(m *tabletext.Model, w *buf, fv *tabletext.Field, id uint16, kind, count int) error {
	body := &buf{}
	body.u8(uint8(kind))
	body.u32(uint32(count))
	for i := range count {
		if err := encodeElement(m, body, fv.Def, kind, &fv.Elems[i]); err != nil {
			return err
		}
	}
	w.u16(id)
	w.u8(uint8(tabletext.KindArray))
	w.u32(uint32(len(body.b)))
	w.raw(body.b)
	return nil
}

// encodeKeyed writes an enum-keyed array (SPEC-TABLES.md §3.2): kind 16, the
// body length, then one `(variant id, L, element body)` pair per PRESENT slot,
// ascending by variant ordinal. A slot holding its default elides exactly as a
// defaulted field does, an array with no present slot is not written at all,
// and None — slot 0 — never rides.
func encodeKeyed(m *tabletext.Model, w *buf, fv *tabletext.Field, id uint16, kind int) error {
	f := fv.Def
	body := &buf{}
	pairs := uint32(0)
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		cell := &fv.Elems[slot]
		var elem []byte
		if kind == tabletext.KindTable {
			b, err := encodeBody(m, subInstance(m, f, cell))
			if err != nil {
				return err
			}
			if len(b) <= 2 {
				continue // an all-default slot elides
			}
			elem = b
		} else {
			if cellIsDefault(m, f, cell) {
				continue // a default slot elides
			}
			eb := &buf{}
			if err := encodeElement(m, eb, f, kind, cell); err != nil {
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
		if keyID == 0 {
			return fmt.Errorf("enum-keyed array %s: slot %d is None's, and None keys no record (SPEC-TABLES.md §3.2)", f.Name, slot)
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
	w.u8(KindKeyed)
	w.u32(uint32(5 + len(body.b)))
	w.u8(uint8(kind))
	w.u32(pairs)
	w.raw(body.b)
	return nil
}

// encodeElement writes one element at its kind's own framing: a table element
// carries its own length, a scalar rides at its fixed width, and an enum rides
// as the u16 hash of its variant name.
func encodeElement(m *tabletext.Model, w *buf, f *ir.Field, kind int, cell *tabletext.Cell) error {
	if e := tabletext.EnumOf(f); e != nil {
		vid, err := variantWireId(e, cell.U, f.Name)
		if err != nil {
			return err
		}
		w.u16(vid)
		return nil
	}
	switch kind {
	case tabletext.KindBool:
		if cell.B {
			w.u8(1)
		} else {
			w.u8(0)
		}
	case tabletext.KindF32:
		w.u32(math.Float32bits(float32(cell.F)))
	case tabletext.KindF64:
		w.u64(math.Float64bits(cell.F))
	case tabletext.KindTable:
		body, err := encodeBody(m, subInstance(m, f, cell))
		if err != nil {
			return err
		}
		w.u32(uint32(len(body)))
		w.raw(body)
	default:
		w.width(tabletext.KindWidth(kind), cell.U)
	}
	return nil
}

// subInstance is a nested table or type's instance, materialized at its
// declared defaults when nothing placed one.
func subInstance(m *tabletext.Model, f *ir.Field, cell *tabletext.Cell) *tabletext.Instance {
	if cell.Tab != nil {
		return cell.Tab
	}
	return m.New(tabletext.StructOf(f))
}

// variantWireId is an enum value's wire identity: the hash of its VARIANT
// NAME, with None's reserved 0. A value no variant names has none, and save
// returns failure rather than writing None over it (§5).
func variantWireId(e *ir.Enum, value uint64, field string) (uint16, error) {
	name := tabletext.EnumName(e, int64(value))
	if name == "" {
		return 0, fmt.Errorf("field %s: enum %s value %d names no variant, so it has no wire identity — save refuses it (SPEC-TABLES.md §5)", field, e.Name, value)
	}
	if name == "None" {
		return 0, nil
	}
	return ir.VariantId(name), nil
}

// cellIsDefault is the writer's elision test: a field holding its declared
// default is not written at all, which is why old readers and new writers meet
// cleanly (§3).
func cellIsDefault(m *tabletext.Model, f *ir.Field, cell *tabletext.Cell) bool {
	def := m.FieldDefaultCell(f)
	switch {
	case f.Type.Kind == ir.TBool:
		return cell.B == def.B
	case f.Type.Kind == ir.TFloat32:
		return float32(cell.F) == float32(def.F)
	case f.Type.Kind == ir.TFloat64:
		return cell.F == def.F
	case f.Type.Kind == ir.TInt && f.Type.Signed:
		return cell.I == def.I
	}
	return cell.U == def.U
}
