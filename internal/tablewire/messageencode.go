// THE MESSAGE FORM's WRITER (docs/SPEC-TABLES.md §3.3): the bitpacked body,
// and the BATCH that is the form's primitive.
//
// The unit of this wire is a NUMBER OF BODIES OF ONE ROOT and never one body:
// a batch writes one buffer, one count and one continuous bit stream with no
// alignment between the bodies, and a single message is the batch of one.
// Writing many at a time is what later forms need to spend a value once across
// a batch, or to delta one body against the last of its table, and neither is
// possible on a wire whose primitive is one message.
package tablewire

import (
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TableMessageBatchMax is the largest batch this form can spell
// (docs/SPEC-TABLES.md §3.3). The count is a ranged integer over `[1, 256]`,
// eight bits carrying `M - 1`, and 256 is a WIRE CONSTANT of the form rather
// than a receiver's policy, because the count's WIDTH depends on it and two
// peers that disagreed on the width would not be reading the same wire.
const TableMessageBatchMax = 256

// EncodeMessages is a BATCH's form-2 wire: the FORM BYTE, the BODY COUNT, and
// the bodies as ONE CONTINUOUS BIT STREAM, zero-padded to the next byte at the
// end and nowhere else.
//
// Every reference is a SLOT of the unit's vocabulary, which is a compile-time
// fact, so nothing is interned and no id is written.
func EncodeMessages(m *tabletext.Model, insts []*tabletext.Instance) ([]byte, error) {
	if len(insts) == 0 {
		// A BATCH OF ZERO IS NOT SPELLABLE, and a caller with nothing to send
		// writes nothing at all.
		return nil, fmt.Errorf("a batch of zero bodies is not spellable: a caller with nothing to send writes nothing (docs/SPEC-TABLES.md §3.3)")
	}
	if len(insts) > TableMessageBatchMax {
		// `M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME (§3.3): nothing
		// is written, no batch is concatenated on the caller's behalf, and the
		// reason rides the message path's own refusal. A caller with more
		// bodies calls again.
		return nil, &MessageRefusal{Reason: ReasonBatchTooLarge}
	}
	vocabulary := ir.TableVocabulary(m.Unit)
	slots := make(map[string]uint64, len(vocabulary))
	for i, e := range vocabulary {
		slots[e.Key()] = uint64(i + 1)
	}
	w := &bitWriter{}
	w.put(uint64(len(insts)-1), 8) // the count, a ranged integer over [1, 256]
	for _, inst := range insts {
		g, err := Number(m, inst)
		if err != nil {
			return nil, err
		}
		e := &bitEncoder{m: m, g: g, slots: slots, refBits: ir.TableMessageRefBits(len(vocabulary)), indexBits: ir.TableMessageBitsRequired(0, 1)}
		if err := encodeBitBody(e, w, inst, true); err != nil {
			return nil, err
		}
		if e.missing != "" {
			// an entry the walk reached that the unit's own vocabulary does
			// not spell is a compiler defect, never a wire one: the vocabulary
			// is the closure's whole entry set by construction (§3.3)
			return nil, fmt.Errorf("the unit's vocabulary names no slot for %s, and the message form's vocabulary is the closure's whole entry set (docs/SPEC-TABLES.md §3.3)", e.missing)
		}
	}
	w.align() // THE FINAL FLUSH ZERO-FILLS to the next byte boundary
	return append([]byte{ir.TableWireMessageForm}, w.b...), nil
}

// EncodeMessage is the BATCH OF ONE, which is the only sense in which this
// wire carries a single message (docs/SPEC-TABLES.md §3.3).
func EncodeMessage(m *tabletext.Model, inst *tabletext.Instance) ([]byte, error) {
	return EncodeMessages(m, []*tabletext.Instance{inst})
}

// bitEncoder is one body's context: the closure the walks run over, the
// NUMBERING its pointers resolve through, and the unit's compiled slot table,
// which is the same table for every body of the batch.
type bitEncoder struct {
	m         *tabletext.Model
	g         *NodeGraph
	slots     map[string]uint64
	refBits   int
	indexBits int // a node index's width: bits_required(0, node count)
	missing   string
}

// ref writes one entry's REFERENCE: its slot in the unit's vocabulary, in
// `bits_required(0, E)` bits.
func (e *bitEncoder) ref(w *bitWriter, entry ir.TableVocabularyEntry) {
	slot, known := e.slots[entry.Key()]
	if !known {
		e.missing = fmt.Sprintf("id %016x at kind %d", entry.Id, entry.Kind)
	}
	w.put(slot, e.refBits)
}

// name writes a reference to an entry that names no field shape: a variant
// name, an arm's own name where the arm is what is being named, a table's name
// id (§3.3's kind 0 rows).
func (e *bitEncoder) name(w *bitWriter, id uint64) {
	e.ref(w, ir.TableVocabularyEntry{Id: id})
}

// encodeBitBody is one table body: its fields, then the ZERO REFERENCE that
// ends it. THE NODE TABLE, WHEN A ROOT HAS ONE, IS ITS FIRST FIELD, because a
// pointer index is `bits_required(0, node count)` wide and the node count
// rides in the node table (§3.1, §3.3).
func encodeBitBody(e *bitEncoder, w *bitWriter, inst *tabletext.Instance, root bool) error {
	if root && e.g != nil && len(e.g.Records()) > 0 {
		if err := encodeBitNodeTable(e, w); err != nil {
			return err
		}
	}
	guards := tabletext.Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if err := encodeBitField(e, w, fv); err != nil {
			return err
		}
	}
	w.put(0, e.refBits)
	return nil
}

func encodeBitField(e *bitEncoder, w *bitWriter, fv *tabletext.Field) error {
	f := fv.Def
	entry := ir.TableFieldEntry(f)
	shape := entry.Shape
	kind := ir.TableWireScalarKind(f)

	if f.Type.Pointer && f.Array == ir.ArrayNone {
		index := e.g.Index(fv.Cell.Node)
		if f.Type.Blob() {
			index = e.g.BlobIndex(fv.Cell.Blob)
		}
		if index == ir.NodeWireIndexNull {
			return nil // null is elided, exactly as it is on a file (§3.1)
		}
		e.ref(w, entry)
		w.put(index, e.indexBits)
		return nil
	}

	switch {
	case f.IsMap():
		return encodeBitMap(e, w, fv, entry)

	case f.Type.Optional:
		if !fv.Present {
			return nil // PRESENCE is the payload (§2.3)
		}
		e.ref(w, entry)
		switch {
		case f.Array != ir.ArrayNone:
			count := int(f.ArrayBound)
			if f.CountedOnWire() {
				count = fv.Count
			}
			return encodeBitArray(e, w, fv, entry, count)
		case kind == ir.TableKindTable:
			return encodeBitBody(e, w, subInstanceOf(e.m, f, &fv.Cell), false)
		case kind == ir.TableKindEnum:
			return encodeBitEnum(e, w, f, &fv.Cell)
		default:
			return encodeBitValue(e, w, f, entry.Kind, shape, &fv.Cell)
		}

	case f.KeyEnum != "":
		return encodeBitKeyed(e, w, fv, entry)

	case f.Type.Kind == ir.TString:
		if fv.Count == 0 {
			return nil
		}
		e.ref(w, entry)
		w.put(uint64(len(fv.Cell.Str)), ir.TableMessageBitsRequired(0, shape.Max))
		w.align() // a string ALIGNS before its bytes, which buys a memcpy
		w.bytes(fv.Cell.Str)
		return nil

	case f.Type.Kind == ir.TBytes:
		if fv.Count == 0 {
			return nil
		}
		e.ref(w, entry)
		w.put(uint64(len(fv.Cell.Str)), ir.TableMessageCountBits(shape))
		w.align() // an array of element kind 6 aligns before its bytes
		w.bytes(fv.Cell.Str)
		return nil

	case f.CountedOnWire():
		if fv.Count == 0 {
			return nil
		}
		e.ref(w, entry)
		return encodeBitArray(e, w, fv, entry, fv.Count)

	case f.Array == ir.ArrayFixed && kind == ir.TableKindTable:
		e.ref(w, entry)
		return encodeBitArray(e, w, fv, entry, int(f.ArrayBound))

	case f.Array == ir.ArrayFixed:
		rides := false
		for i := 0; i < int(f.ArrayBound); i++ {
			if kind == ir.TableKindUnion {
				if fv.Elems[i].U != 0 {
					rides = true
					break
				}
				continue
			}
			if !cellIsDefaultIn(e.m, f, &fv.Elems[i]) {
				rides = true
				break
			}
		}
		if !rides {
			return nil
		}
		e.ref(w, entry)
		return encodeBitArray(e, w, fv, entry, int(f.ArrayBound))

	case kind == ir.TableKindUnion:
		un := tabletext.UnionOf(f)
		if fv.Cell.U == 0 {
			return nil // None elides, and the absence of the field is the None
		}
		if int(fv.Cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm, and save refuses it (docs/SPEC-TABLES.md §5)", un.Name, fv.Cell.U)
		}
		e.ref(w, entry)
		return encodeBitArm(e, w, un.Variants[fv.Cell.U-1], &fv.Cell)

	case kind == ir.TableKindTable:
		body := &bitWriter{}
		sub := *e
		if err := encodeBitBody(&sub, body, subInstanceOf(e.m, f, &fv.Cell), false); err != nil {
			return err
		}
		e.missing = sub.missing
		if body.bits() <= e.refBits {
			return nil // an all-default nested table elides: its body is its terminator alone
		}
		e.ref(w, entry)
		w.splice(body)
		return nil

	case kind == ir.TableKindEnum:
		if cellIsDefaultIn(e.m, f, &fv.Cell) {
			return nil
		}
		e.ref(w, entry)
		return encodeBitEnum(e, w, f, &fv.Cell)

	default:
		if cellIsDefaultIn(e.m, f, &fv.Cell) {
			return nil
		}
		e.ref(w, entry)
		return encodeBitValue(e, w, f, entry.Kind, shape, &fv.Cell)
	}
}

// encodeBitEnum writes an enum value: the reference naming its VARIANT's name,
// and the ZERO REFERENCE for None, the one value that names no entry (§3).
func encodeBitEnum(e *bitEncoder, w *bitWriter, f *ir.Field, cell *tabletext.Cell) error {
	en := tabletext.EnumOf(f)
	vid, none, err := variantWireId(en, cell.U, f.Name)
	if err != nil {
		return err
	}
	if none {
		w.put(0, e.refBits)
		return nil
	}
	e.name(w, vid)
	return nil
}

// encodeBitArray writes an array's COUNT and then its elements back to back.
// NO COUNT RIDES when the shape's `min` equals its `max`, which is every fixed
// array: the declaration already said how many.
func encodeBitArray(e *bitEncoder, w *bitWriter, fv *tabletext.Field, entry ir.TableVocabularyEntry, count int) error {
	shape := entry.Shape
	if bits := ir.TableMessageCountBits(shape); bits > 0 {
		// the count rides as its OFFSET from the announced minimum, which is
		// what a ranged integer over [min, max] is (§3.3)
		w.put(uint64(count)-uint64(shape.Min), bits)
	}
	if ir.TableMessageAligns(entry.Kind, shape) {
		w.align()
	}
	f := fv.Def
	for i := range count {
		if err := encodeBitElement(e, w, f, shape, &fv.Elems[i]); err != nil {
			return err
		}
	}
	return nil
}

// encodeBitElement writes one element at its element kind's own framing.
func encodeBitElement(e *bitEncoder, w *bitWriter, f *ir.Field, shape ir.TableMessageShape, cell *tabletext.Cell) error {
	inner := ir.TableMessageShape{}
	if shape.Inner != nil {
		inner = *shape.Inner
	}
	switch int(shape.Elem) {
	case ir.TableKindTable:
		return encodeBitBody(e, w, subInstanceOf(e.m, f, cell), false)
	case ir.TableKindPointer:
		w.put(e.g.Index(cell.Node), e.indexBits)
		return nil
	case ir.TableKindEnum:
		return encodeBitEnum(e, w, f, cell)
	case ir.TableKindUnion:
		un := tabletext.UnionOf(f)
		if cell.U == 0 {
			w.put(0, e.refBits) // a None element is the zero reference in its place
			return nil
		}
		if int(cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm, and save refuses it (docs/SPEC-TABLES.md §5)", un.Name, cell.U)
		}
		return encodeBitArm(e, w, un.Variants[cell.U-1], cell)
	}
	return encodeBitValue(e, w, f, shape.Elem, inner, cell)
}

// encodeBitValue writes ONE value at the width its declaration states, which
// is what the packet wire writes for that declaration: a ranged integer as
// `value - base`, a quantized float as its step index, a bare integer at its
// storage width.
func encodeBitValue(e *bitEncoder, w *bitWriter, f *ir.Field, kind uint8, shape ir.TableMessageShape, cell *tabletext.Cell) error {
	switch int(kind) {
	case ir.TableKindBool:
		if cell.B {
			w.put(1, 1)
		} else {
			w.put(0, 1)
		}
		return nil
	case ir.TableKindF64:
		w.put(math.Float64bits(cell.F), 64)
		return nil
	case ir.TableKindF32:
		if shape.Packing == ir.TableMessageQuantized {
			// THE PACKET WIRE'S RULE, IN FLOAT32 (SPEC.md §4.3, §3.3): the
			// index a message carries is the index a packet carries
			w.put(uint64(ir.TableMessageQuantize(shape, float32(cell.F))), int(shape.Bits))
			return nil
		}
		w.put(uint64(narrowF32(cell.F)), 32)
		return nil
	}
	if ir.TableKindWide(int(kind)) && f.Type.Width > 64 && shape.Packing != ir.TableMessageRanged {
		w.bytes(tabletext.WideBytes(tabletext.WideValue(cell), int(kind)))
		return nil
	}
	bits := int(ir.TableMessageValueBits(kind, shape))
	if bits <= 0 {
		return nil
	}
	if shape.Packing == ir.TableMessageRanged {
		base := shape.Base
		if base == nil {
			base = big.NewInt(0)
		}
		if ir.TableKindWide(int(kind)) {
			raw := tabletext.WideValue(cell)
			if raw == nil {
				raw = big.NewInt(0)
			}
			w.putBig(new(big.Int).Sub(raw, base), bits)
			return nil
		}
		if f.Type.Kind == ir.TInt && f.Type.Signed {
			w.put(uint64(cell.I-base.Int64()), bits)
			return nil
		}
		w.put(cell.U-base.Uint64(), bits) // an unsigned base spans the whole domain, 2^63 and above included
		return nil
	}
	if ir.TableKindWide(int(kind)) {
		w.bytes(tabletext.WideBytes(tabletext.WideValue(cell), int(kind)))
		return nil
	}
	if f.Type.Kind == ir.TInt && f.Type.Signed {
		w.put(uint64(cell.I), bits)
		return nil
	}
	w.put(cell.U, bits)
	return nil
}

// encodeBitArm writes one union ARM: its NAME's reference and then the payload
// a FIELD of the arm's type would carry (§2.6). A payload-free arm carries
// nothing at all, where the file form spent a kind byte and a zero length.
func encodeBitArm(e *bitEncoder, w *bitWriter, arm ir.UnionVariant, cell *tabletext.Cell) error {
	entry := ir.TableArmEntry(arm)
	e.ref(w, entry)
	switch {
	case arm.Void():
		return nil
	case arm.Body():
		payload := cell.Tab
		if payload == nil {
			payload = e.m.New(arm.Ref)
		}
		return encodeBitBody(e, w, payload, false)
	}
	f := arm.F
	fv := cell.Arm
	if fv == nil {
		fv = e.m.NewArm(arm)
	}
	shape := entry.Shape
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index := e.g.Index(fv.Cell.Node)
		if f.Type.Blob() {
			index = e.g.BlobIndex(fv.Cell.Blob)
		}
		w.put(index, e.indexBits)
		return nil
	case f.Type.Kind == ir.TString:
		w.put(uint64(len(fv.Cell.Str)), ir.TableMessageBitsRequired(0, shape.Max))
		w.align()
		w.bytes(fv.Cell.Str)
		return nil
	case f.Type.Kind == ir.TBytes:
		w.put(uint64(len(fv.Cell.Str)), ir.TableMessageCountBits(shape))
		w.align()
		w.bytes(fv.Cell.Str)
		return nil
	case f.Array != ir.ArrayNone:
		count := int(f.ArrayBound)
		if f.CountedOnWire() {
			count = fv.Count
		}
		return encodeBitArray(e, w, fv, entry, count)
	case tabletext.UnionOf(f) != nil:
		inner := tabletext.UnionOf(f)
		if fv.Cell.U == 0 {
			w.put(0, e.refBits)
			return nil
		}
		if int(fv.Cell.U) > len(inner.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm, and save refuses it (docs/SPEC-TABLES.md §5)", inner.Name, fv.Cell.U)
		}
		return encodeBitArm(e, w, inner.Variants[fv.Cell.U-1], &fv.Cell)
	case tabletext.EnumOf(f) != nil:
		return encodeBitEnum(e, w, f, &fv.Cell)
	case tabletext.StructOf(f) != nil:
		return encodeBitBody(e, w, subInstanceOf(e.m, f, &fv.Cell), false)
	}
	return encodeBitValue(e, w, f, entry.Kind, shape, &fv.Cell)
}

// encodeBitKeyed writes an enum-keyed array (docs/SPEC-TABLES.md §3.2): the
// number of PRESENT slots, then one `(key reference, element)` pair per slot,
// ascending by variant ordinal. Elision is the file form's, unchanged.
func encodeBitKeyed(e *bitEncoder, w *bitWriter, fv *tabletext.Field, entry ir.TableVocabularyEntry) error {
	f := fv.Def
	shape := entry.Shape
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
		if int(shape.Elem) == ir.TableKindTable {
			sub := *e
			if err := encodeBitBody(&sub, elem, subInstanceOf(e.m, f, cell), false); err != nil {
				return err
			}
			e.missing = sub.missing
			if elem.bits() <= e.refBits {
				continue // an all-default slot elides
			}
		} else {
			if cellIsDefaultIn(e.m, f, cell) {
				continue // a default slot elides
			}
			if err := encodeBitElement(e, elem, f, shape, cell); err != nil {
				return err
			}
		}
		e.name(pairs, keyID)
		pairs.splice(elem)
		n++
	}
	if n == 0 {
		return nil
	}
	e.ref(w, entry)
	w.put(n, ir.TableMessageBitsRequired(0, shape.Max))
	w.splice(pairs)
	return nil
}

// encodeBitMap writes one map field (docs/SPEC-TABLES.md §2.8): the entry
// count, then the generated `{ key, value }` bodies in ascending key order
// with no key twice. An entry always rides, because identity there is the key.
func encodeBitMap(e *bitEncoder, w *bitWriter, fv *tabletext.Field, entry ir.TableVocabularyEntry) error {
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
	e.ref(w, entry)
	w.put(uint64(len(order)), ir.TableMessageCountBits(entry.Shape))
	for _, at := range order {
		if err := encodeBitBody(e, w, fv.Entries[at].Tab, false); err != nil {
			return err
		}
	}
	return nil
}

// encodeBitNodeTable writes the numbering as the ROOT body's FIRST field
// (§3.1, §3.3): the node count, then the records back to back, each its type
// id's reference and then its body or its bytes.
//
// A pointer index is `bits_required(0, node count)` wide, so the node count
// has to be read before an index can be, and that is the one ordering
// constraint this form puts on a body.
func encodeBitNodeTable(e *bitEncoder, w *bitWriter) error {
	records := e.g.Records()
	e.indexBits = ir.TableMessageBitsRequired(0, int64(len(records))+1)
	e.name(w, ir.TableNodeWireId)
	w.put(uint64(len(records)), 32)
	for _, node := range records {
		e.name(w, node.TypeId())
		if node.Blob != nil {
			w.put(uint64(len(node.Blob.Data)), 32)
			w.align()
			w.bytes(node.Blob.Data)
			continue
		}
		if err := encodeBitBody(e, w, node.Inst, false); err != nil {
			return err
		}
	}
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
