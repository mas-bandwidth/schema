// THE MESSAGE FORM's WRITER (docs/SPEC-TABLES.md §3.3): the bitpacked body,
// and the BATCH that is the form's primitive.
//
// The unit of this wire is a NUMBER OF MESSAGES and never one message: a batch
// writes one buffer, one count and one continuous bit stream, and a single
// message is the batch of one. Writing many at a time is what later forms need
// to spend a value once across a batch, or to delta one body against the last
// of its table, and neither is possible on a wire whose primitive is one
// message.
package tablewire

import (
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// EncodeMessages is a BATCH's form-2 wire (docs/SPEC-TABLES.md §3.3): the FORM
// BYTE, the message COUNT, and the bodies as ONE CONTINUOUS BIT STREAM with no
// alignment between them, padded to a byte at the end and nowhere else.
//
// Every reference is a SLOT of the unit's vocabulary, which is a compile-time
// fact, so nothing is interned and no id is written.
func EncodeMessages(m *tabletext.Model, insts []*tabletext.Instance) ([]byte, error) {
	vocabulary := ir.TableVocabulary(m.Unit)
	records := ir.TableVocabularyRecords(m.Unit)
	slots := make(map[uint64]uint64, len(vocabulary))
	for i, id := range vocabulary {
		slots[id] = uint64(i + 1)
	}
	w := &bitWriter{}
	w.leb(uint64(len(insts)))
	for _, inst := range insts {
		g, err := Number(m, inst)
		if err != nil {
			return nil, err
		}
		e := &bitEncoder{m: m, g: g, slots: slots, records: records, refBits: ir.TableMessageRefBits(len(vocabulary))}
		body, err := encodeBitBody(e, inst, true)
		if err != nil {
			return nil, err
		}
		if e.missing != 0 {
			// an id the walk reached that the unit's own vocabulary does not
			// spell is a compiler defect, never a wire one: the vocabulary is
			// the closure's whole id set by construction (§3.3)
			return nil, fmt.Errorf("the unit's vocabulary names no slot for id %016x — the message form's table is the closure's whole id set (docs/SPEC-TABLES.md §3.3)", e.missing)
		}
		w.splice(body)
	}
	w.align()
	return append([]byte{ir.TableWireMessageForm}, w.b...), nil
}

// EncodeMessage is the BATCH OF ONE, which is the only sense in which this
// wire carries a single message (docs/SPEC-TABLES.md §3.3).
func EncodeMessage(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	return EncodeMessages(m, []*tabletext.Instance{inst})
}

// bitEncoder is one batch's context: the closure the walks run over, the
// NUMBERING each message's pointers resolve through, and the unit's compiled
// slot table, which is the same table for every message in the batch.
type bitEncoder struct {
	m       *tabletext.Model
	g       *NodeGraph
	slots   map[uint64]uint64
	records []ir.TableMessageDescriptor
	refBits int
	missing uint64
}

// record is the CANONICAL shape the announcement publishes for an id, and it
// is what the writer spends its bits at: the record is the wire contract for
// the id, not the field's own declaration (docs/SPEC-TABLES.md §3.3).
func (e *bitEncoder) record(id uint64) ir.TableMessageDescriptor {
	slot, known := e.slots[id]
	if !known {
		e.missing = id
		return ir.TableMessageDescriptor{}
	}
	return e.records[slot-1]
}

// ref writes one id's REFERENCE: its slot in the unit's vocabulary, in
// `bits_required(entries)` bits.
func (e *bitEncoder) ref(w *bitWriter, id uint64) {
	slot, known := e.slots[id]
	if !known {
		e.missing = id
	}
	w.put(slot, e.refBits)
}

// encodeBitBody is one table body: its fields, then the ZERO REFERENCE that
// ends it. A ROOT body owes its node table after the fields and before the
// terminator (§3.1).
func encodeBitBody(e *bitEncoder, inst *tabletext.Instance, root bool) (*bitWriter, error) {
	w := &bitWriter{}
	guards := tabletext.Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if err := encodeBitField(e, w, fv); err != nil {
			return nil, err
		}
	}
	if root && e.g != nil && len(e.g.Records()) > 0 {
		if err := appendBitNodeTable(e, w); err != nil {
			return nil, err
		}
	}
	w.put(0, e.refBits)
	return w, nil
}

func encodeBitField(e *bitEncoder, w *bitWriter, fv *tabletext.Field) error {
	f := fv.Def
	id := ir.TableFieldWireId(f)
	d := e.record(id)
	kind := ir.TableWireScalarKind(f)
	if d.Flags&ir.TableMessageAmbiguous != 0 {
		// ONE ID, TWO KINDS: there is no shape that spells both, so there is
		// no honest byte string to write and the save refuses by name rather
		// than writing a wire no reader can walk (§3.3).
		return fmt.Errorf("field %s: id %016x is declared at two kinds in this unit, so the message form has no shape for it — the file form carries it unchanged (docs/SPEC-TABLES.md §3.3)", f.Name, id)
	}

	if f.Type.Pointer && f.Array == ir.ArrayNone {
		index := e.g.Index(fv.Cell.Node)
		if f.Type.Blob() {
			index = e.g.BlobIndex(fv.Cell.Blob)
		}
		if index == ir.NodeWireIndexNull {
			return nil // null is elided, exactly as it is on a file (§3.1)
		}
		e.ref(w, id)
		w.put(index, int(d.ValueBits))
		return nil
	}

	switch {
	case f.IsMap():
		return encodeBitMap(e, w, fv, id, d)

	case f.Type.Optional:
		if !fv.Present {
			return nil // PRESENCE is the payload (§2.3)
		}
		switch {
		case f.Array != ir.ArrayNone:
			count := int(f.ArrayBound)
			if f.CountedOnWire() {
				count = fv.Count
			}
			return encodeBitArray(e, w, fv, id, kind, d, count)
		case kind == ir.TableKindTable:
			e.ref(w, id)
			body, err := encodeBitBody(e, subInstanceOf(e.m, f, &fv.Cell), false)
			if err != nil {
				return err
			}
			w.splice(body)
			return nil
		default:
			return encodeBitSimple(e, w, f, id, kind, d, &fv.Cell)
		}

	case f.KeyEnum != "":
		return encodeBitKeyed(e, w, fv, id, kind, d)

	case f.Type.Kind == ir.TString:
		if fv.Count == 0 {
			return nil
		}
		e.ref(w, id)
		e.length(w, d, uint64(len(fv.Cell.Str)))
		w.bytes(fv.Cell.Str)
		return nil

	case f.Type.Kind == ir.TBytes:
		if fv.Count == 0 {
			return nil
		}
		e.ref(w, id)
		e.length(w, d, uint64(len(fv.Cell.Str)))
		w.bytes(fv.Cell.Str)
		return nil

	case f.CountedOnWire():
		if fv.Count == 0 {
			return nil
		}
		return encodeBitArray(e, w, fv, id, kind, d, fv.Count)

	case f.Array == ir.ArrayFixed && kind == ir.TableKindTable:
		return encodeBitArray(e, w, fv, id, kind, d, int(f.ArrayBound))

	case f.Array == ir.ArrayFixed:
		allDefault := true
		for i := 0; i < int(f.ArrayBound); i++ {
			if !cellIsDefaultIn(e.m, f, &fv.Elems[i]) {
				allDefault = false
				break
			}
		}
		if allDefault {
			return nil
		}
		return encodeBitArray(e, w, fv, id, kind, d, int(f.ArrayBound))

	case kind == ir.TableKindUnion:
		un := tabletext.UnionOf(f)
		if fv.Cell.U == 0 {
			return nil // None elides — the absence of the field is the None
		}
		if int(fv.Cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", un.Name, fv.Cell.U)
		}
		e.ref(w, id)
		return encodeBitArm(e, w, un.Variants[fv.Cell.U-1], &fv.Cell)

	case kind == ir.TableKindTable:
		body, err := encodeBitBody(e, subInstanceOf(e.m, f, &fv.Cell), false)
		if err != nil {
			return err
		}
		if body.bits() <= e.refBits {
			return nil // an all-default nested table elides: its body is its terminator alone
		}
		e.ref(w, id)
		w.splice(body)
		return nil

	default:
		if cellIsDefaultIn(e.m, f, &fv.Cell) {
			return nil
		}
		return encodeBitSimple(e, w, f, id, kind, d, &fv.Cell)
	}
}

// length writes one length or count at the width the declaration states, or as
// a bit LEB128 where it states none (docs/SPEC-TABLES.md §3.3).
func (e *bitEncoder) length(w *bitWriter, d ir.TableMessageDescriptor, n uint64) {
	if d.Flags&ir.TableMessageLebLength != 0 {
		w.leb(n)
		return
	}
	w.put(n, int(d.LengthBits))
}

func encodeBitSimple(e *bitEncoder, w *bitWriter, f *ir.Field, id uint64, kind int, d ir.TableMessageDescriptor, cell *tabletext.Cell) error {
	if en := tabletext.EnumOf(f); en != nil {
		vid, none, err := variantWireId(en, cell.U, f.Name)
		if err != nil {
			return err
		}
		e.ref(w, id)
		e.enumRef(w, vid, none)
		return nil
	}
	e.ref(w, id)
	return encodeBitElement(e, w, f, kind, d, cell)
}

// enumRef writes an enum value: the reference to its VARIANT NAME's id, and
// the ZERO REFERENCE for None (§3).
func (e *bitEncoder) enumRef(w *bitWriter, id uint64, none bool) {
	if none {
		w.put(0, e.refBits)
		return
	}
	e.ref(w, id)
}

// encodeBitArray writes an array field: the COUNT at the declared bound's
// width, then the elements back to back. There is no element kind byte and no
// body length: the announcement's record for this id carries the element kind,
// and the elements frame themselves.
func encodeBitArray(e *bitEncoder, w *bitWriter, fv *tabletext.Field, id uint64, kind int, d ir.TableMessageDescriptor, count int) error {
	elemKind := kind
	if fv.Def.Type.Pointer {
		elemKind = ir.TableKindPointer
	}
	e.ref(w, id)
	e.length(w, d, uint64(count))
	for i := range count {
		if err := encodeBitElement(e, w, fv.Def, elemKind, d, &fv.Elems[i]); err != nil {
			return err
		}
	}
	return nil
}

// encodeBitKeyed writes an enum-keyed array (docs/SPEC-TABLES.md §3.2): the
// number of PRESENT slots, then one `(key reference, element)` pair per slot,
// ascending by variant ordinal. Elision is the file form's, unchanged.
func encodeBitKeyed(e *bitEncoder, w *bitWriter, fv *tabletext.Field, id uint64, kind int, d ir.TableMessageDescriptor) error {
	f := fv.Def
	pairs := &bitWriter{}
	n := uint64(0)
	for slot := tabletext.KeyedFirstSlot(); slot < tabletext.KeyedSlotCount(f); slot++ {
		cell := &fv.Elems[slot]
		keyID, none, err := variantWireId(f.KeyEnumRef, uint64(tabletext.KeyedSlotValue(f, slot)), f.Name)
		if err != nil {
			return err
		}
		if none {
			return fmt.Errorf("field %s: a None key names no slot (docs/SPEC-TABLES.md §3.2)", f.Name)
		}
		elem := &bitWriter{}
		if kind == ir.TableKindTable {
			body, err := encodeBitBody(e, subInstanceOf(e.m, f, cell), false)
			if err != nil {
				return err
			}
			if body.bits() <= e.refBits {
				continue // an all-default slot elides
			}
			elem = body
		} else {
			if cellIsDefaultIn(e.m, f, cell) {
				continue // a default slot elides
			}
			if err := encodeBitElement(e, elem, f, kind, d, cell); err != nil {
				return err
			}
		}
		e.ref(pairs, keyID)
		pairs.splice(elem)
		n++
	}
	if n == 0 {
		return nil
	}
	e.ref(w, id)
	e.length(w, d, n)
	w.splice(pairs)
	return nil
}

// encodeBitMap writes one map field (docs/SPEC-TABLES.md §2.8): the entry
// count, then the generated `{ key, value }` bodies in ascending key order
// with no key twice. An entry always rides, because identity there is the key.
func encodeBitMap(e *bitEncoder, w *bitWriter, fv *tabletext.Field, id uint64, d ir.TableMessageDescriptor) error {
	f := fv.Def
	if len(fv.Entries) == 0 {
		return nil
	}
	order := make([]int, len(fv.Entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return tabletext.MapKeyOrder(f,
			tabletext.MapKeyOf(f, fv.Entries[order[a]].Tab),
			tabletext.MapKeyOf(f, fv.Entries[order[b]].Tab)) < 0
	})
	e.ref(w, id)
	e.length(w, d, uint64(len(order)))
	for _, at := range order {
		body, err := encodeBitBody(e, fv.Entries[at].Tab, false)
		if err != nil {
			return err
		}
		w.splice(body)
	}
	return nil
}

// encodeBitElement writes one value at its kind's own bitpacked framing.
func encodeBitElement(e *bitEncoder, w *bitWriter, f *ir.Field, kind int, d ir.TableMessageDescriptor, cell *tabletext.Cell) error {
	if en := tabletext.EnumOf(f); en != nil {
		vid, none, err := variantWireId(en, cell.U, f.Name)
		if err != nil {
			return err
		}
		e.enumRef(w, vid, none)
		return nil
	}
	switch kind {
	case ir.TableKindTable:
		body, err := encodeBitBody(e, subInstanceOf(e.m, f, cell), false)
		if err != nil {
			return err
		}
		w.splice(body)
		return nil
	case ir.TableKindPointer:
		w.put(e.g.Index(cell.Node), ir.PointerWireBits)
		return nil
	case ir.TableKindUnion:
		un := tabletext.UnionOf(f)
		if cell.U == 0 {
			w.put(0, e.refBits) // a None element is the zero reference in its place
			return nil
		}
		if int(cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", un.Name, cell.U)
		}
		return encodeBitArm(e, w, un.Variants[cell.U-1], cell)
	}
	return e.value(w, f, d, cell)
}

// value writes ONE scalar at its DECLARED width, which is the width the packet
// wire writes it at: a ranged integer as `value - min`, a fixed-point value as
// its raw scaled integer over the shifted range, a `bits(N)` in N, a bool in
// one. Floats ride uncompressed, at 32 or 64 bits.
func (e *bitEncoder) value(w *bitWriter, f *ir.Field, d ir.TableMessageDescriptor, cell *tabletext.Cell) error {
	switch f.Type.Kind {
	case ir.TBool:
		if cell.B {
			w.put(1, 1)
		} else {
			w.put(0, 1)
		}
		return nil
	case ir.TFloat32:
		w.put(uint64(narrowF32(cell.F)), 32)
		return nil
	case ir.TFloat64:
		w.put(math.Float64bits(cell.F), 64)
		return nil
	case ir.TString, ir.TBytes:
		w.put(cell.U, 8) // an element of a `bytes(N)`: one byte
		return nil
	}
	if f.Type.Width > 64 && (f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed) {
		w.bytes(tabletext.WideBytes(tabletext.WideValue(cell), ir.TableWireScalarKind(f)))
		return nil
	}
	if f.Type.Kind == ir.TFixed {
		raw := tabletext.WideValue(cell)
		if raw == nil {
			raw = big.NewInt(0)
		}
		w.put(uint64(new(big.Int).Sub(raw, big.NewInt(d.Min)).Int64()), int(d.ValueBits))
		return nil
	}
	if f.Type.Kind == ir.TInt && f.Type.Signed {
		if f.HasIntRange {
			w.put(uint64(cell.I-d.Min), int(d.ValueBits))
			return nil
		}
		w.put(uint64(cell.I), int(d.ValueBits))
		return nil
	}
	w.put(cell.U-uint64(d.Min), int(d.ValueBits))
	return nil
}

// encodeBitArm writes one union ARM: its NAME's reference and then the payload
// a FIELD of the arm's type would carry (§2.6). A payload-free arm carries
// nothing at all, where the file form spent a kind byte and a zero length.
func encodeBitArm(e *bitEncoder, w *bitWriter, arm ir.UnionVariant, cell *tabletext.Cell) error {
	e.ref(w, ir.TableWireId(arm.Name))
	switch {
	case arm.Void():
		return nil
	case arm.Body():
		payload := cell.Tab
		if payload == nil {
			payload = e.m.New(arm.Ref)
		}
		body, err := encodeBitBody(e, payload, false)
		if err != nil {
			return err
		}
		w.splice(body)
		return nil
	}
	f := arm.F
	fv := cell.Arm
	if fv == nil {
		fv = e.m.NewArm(arm)
	}
	d := e.record(ir.TableWireId(arm.Name))
	kind := ir.TableWireScalarKind(f)
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index := e.g.Index(fv.Cell.Node)
		if f.Type.Blob() {
			index = e.g.BlobIndex(fv.Cell.Blob)
		}
		w.put(index, ir.PointerWireBits)
		return nil
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		e.length(w, d, uint64(len(fv.Cell.Str)))
		w.bytes(fv.Cell.Str)
		return nil
	case f.Array != ir.ArrayNone:
		count := int(f.ArrayBound)
		if f.CountedOnWire() {
			count = fv.Count
		}
		elemKind := kind
		if f.Type.Pointer {
			elemKind = ir.TableKindPointer
		}
		e.length(w, d, uint64(count))
		for i := range count {
			if err := encodeBitElement(e, w, f, elemKind, d, &fv.Elems[i]); err != nil {
				return err
			}
		}
		return nil
	case tabletext.UnionOf(f) != nil:
		inner := tabletext.UnionOf(f)
		if fv.Cell.U == 0 {
			w.put(0, e.refBits)
			return nil
		}
		if int(fv.Cell.U) > len(inner.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — save refuses it (docs/SPEC-TABLES.md §5)", inner.Name, fv.Cell.U)
		}
		return encodeBitArm(e, w, inner.Variants[fv.Cell.U-1], &fv.Cell)
	}
	return encodeBitElement(e, w, f, kind, d, &fv.Cell)
}

// appendBitNodeTable writes the numbering's records into the ROOT body, in the
// place §3.1 puts them: after the root's own fields and before the terminator.
//
// Its payload is a BIT STREAM framed by a bit LEB128 of its BIT COUNT, because
// a byte count cannot frame a stream that never aligns. A reader that cannot
// name the reserved id skips exactly those bits and counts one unknown.
func appendBitNodeTable(e *bitEncoder, w *bitWriter) error {
	records := e.g.Records()
	payload := &bitWriter{}
	payload.leb(uint64(len(records)))
	for _, node := range records {
		e.ref(payload, node.TypeId())
		if node.Blob != nil {
			payload.leb(uint64(len(node.Blob.Data)))
			payload.bytes(node.Blob.Data)
			continue
		}
		body, err := encodeBitBody(e, node.Inst, false)
		if err != nil {
			return err
		}
		payload.splice(body)
	}
	e.ref(w, ir.TableNodeWireId)
	w.leb(uint64(payload.bits()))
	w.splice(payload)
	return nil
}

// subInstanceOf is a nested table or type's instance, materialized at its
// declared defaults when nothing placed one.
func subInstanceOf(m *tabletext.Model, f *ir.Field, cell *tabletext.Cell) *tabletext.Instance {
	if cell.Tab != nil {
		return cell.Tab
	}
	return m.New(tabletext.StructOf(f))
}

// cellIsDefaultIn is the writer's elision test, over a model rather than an
// encoder, because the two forms share it exactly.
func cellIsDefaultIn(m *tabletext.Model, f *ir.Field, cell *tabletext.Cell) bool {
	return cellIsDefault(&encoder{m: m}, f, cell)
}
