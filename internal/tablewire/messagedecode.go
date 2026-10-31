// THE MESSAGE FORM's READER (docs/SPEC-TABLES.md §3.3): the batch, the
// bitpacked body, and the tolerant path over both.
//
// A body carries NO KIND BYTE and no length. What a reader needs to skip an
// entry it cannot name, and to decode one whose declaration has MOVED, is the
// announcement's ENTRY: an id beside a kind beside a SHAPE. A KIND MISMATCH is
// the same event §4 already names, found by comparing the announced kind
// against the reader's own declaration rather than by reading a byte.
//
// DAMAGE IS TERMINAL FOR THE BATCH. A byte-framed body has a place to resume
// and a bit stream does not, so a reader that has lost its position has lost
// it for the rest of the buffer: the fields decoded before it stand, ONE
// `malformed` counts, and nothing after it is read.
package tablewire

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageCount reads a batch's body count without decoding a body
// (docs/SPEC-TABLES.md §3.3), so a caller can size the storage the bodies land
// in before anything is read into it.
func MessageCount(data []byte, v *Vocabulary) (int, error) {
	if len(data) < 1 {
		return 0, nil
	}
	if data[0] != ir.TableWireMessageForm {
		return 0, &MessageRefusal{Reason: ReasonNewerForm}
	}
	if v == nil || !v.announced {
		return 0, &MessageRefusal{Reason: ReasonNoVocabulary}
	}
	n, ok := newBitReader(data[1:]).get(8)
	if !ok {
		return 0, nil
	}
	return int(n) + 1, nil
}

// DecodeMessages fills the caller's instances from a BATCH, resolving every
// reference against the announced vocabulary (docs/SPEC-TABLES.md §3.3), and
// answers HOW MANY BODIES IT DELIVERED.
//
// A BATCH IS OF ONE ROOT, and which root it is, is the APPLICATION's: an endpoint
// that mixes roots puts a discriminator in front of the bytes or wraps its
// message set in one root holding a union of them. The error is a REFUSAL, a
// wire this reader will not decode at all, and it is returned rather than
// folded into the report. False with a nil error is damage, which is terminal
// for the batch.
//
// THE BATCH SURFACE'S ANSWERS, each the page's (§3.3):
//
//   - `M` ABOVE THE CALLER'S STORAGE IS A REFUSAL BY NAME, `batch_too_large`,
//     found from the count before any body is decoded: no counter moves and
//     `malformed` does not fire. The returned count is the WIRE's `M`, so the
//     caller holds the number it was short by and calls again with storage at
//     or above it.
//   - DAMAGE ANYWHERE IN THE BATCH DELIVERS NO BODY (docs/SPEC-TABLES.md
//     §4): the returned count is zero, `malformed` is reported, and every
//     body's storage holds its declared defaults.
func DecodeMessages(m *tabletext.Model, insts []*tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (int, bool, error) {
	if len(insts) > 0 {
		if err := messageRoot(m, insts[0].Def); err != nil {
			return 0, false, err
		}
	}
	// A NON-FINITE FLOAT IN ANY BODY REFUSES THE BATCH (docs/SPEC-TABLES.md
	// §3.3): no body of it is delivered, so the count is zero.
	prior := report.NonFiniteFloat
	report.NonFiniteFloat = ""
	priorMalformed := report.Malformed
	report.Malformed = false
	n, ok, err := decodeMessagesWalk(m, insts, data, v, report)
	if refusal := refusedNonFinite(report); refusal != nil {
		report.Malformed = report.Malformed || priorMalformed
		return 0, false, refusal
	}
	report.NonFiniteFloat = prior
	if report.Malformed {
		// DAMAGE DISCARDS THE WHOLE BATCH (docs/SPEC-TABLES.md §4, §3.3): no
		// body is delivered, so the count is zero and every body the walk
		// touched returns to its declared defaults.
		for _, inst := range insts {
			m.Clear(inst)
		}
		return 0, false, err
	}
	report.Malformed = priorMalformed
	return n, ok, err
}

// messageRoot refuses a root the message form does not carry: the message
// form is FIXED TABLES ONLY (docs/SPEC-TABLES.md §3.3), and a variable table
// has no message form. It is a caller's error, never a wire's.
func messageRoot(m *tabletext.Model, root *ir.Struct) error {
	if ir.VariableTables(m.Unit)[root.Name] {
		return fmt.Errorf("%s is a variable table: the message form is fixed tables only (docs/SPEC-TABLES.md §3.3)", root.Name)
	}
	return nil
}

func decodeMessagesWalk(m *tabletext.Model, insts []*tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (int, bool, error) {
	// THE FORM BYTE IS READ FIRST, before the count and before any body, so a
	// wire that is both a form this reader does not carry and damaged is a
	// refusal and never damage (§3).
	if len(data) < 1 {
		report.Malformed = true
		return 0, false, nil
	}
	if data[0] != ir.TableWireMessageForm {
		report.Refused = true
		return 0, false, &MessageRefusal{Reason: ReasonNewerForm}
	}
	// A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
	// decoded, no counter moves and `malformed` does not fire.
	if v == nil || !v.announced {
		report.Refused = true
		return 0, false, &MessageRefusal{Reason: ReasonNoVocabulary}
	}
	r := newBitReader(data[1:])
	raw, ok := r.get(8)
	if !ok {
		report.Malformed = true
		return 0, false, nil
	}
	count := int(raw) + 1
	if count > len(insts) {
		report.Refused = true
		return count, false, &MessageRefusal{Reason: ReasonBatchTooLarge, BuildVersion: v.buildVersion}
	}
	for i := range count {
		d := &bitDecoder{m: m, v: v, report: report, r: r, refBits: v.RefBits()}
		if !d.body(insts[i]) {
			return i, false, nil
		}
	}
	// THE TRAILING PAD IS VERIFIED ZERO, which is the packet wire's rule for
	// the same reason (SPEC.md §4.3), and BYTES AFTER THE PAD ARE MALFORMED:
	// the batch ends at the pad and a buffer with bytes left over describes no
	// batch this reader can name. Both are damage, and damage after the last
	// body discards the batch like damage inside one (§4).
	if !r.align() || r.off != r.n {
		report.Malformed = true
		return count, false, nil
	}
	return count, true, nil
}

// DecodeMessage is the BATCH OF ONE.
func DecodeMessage(m *tabletext.Model, inst *tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (bool, error) {
	_, ok, err := DecodeMessages(m, []*tabletext.Instance{inst}, data, v, report)
	return ok, err
}

// bitDecoder is one body's read: the announced vocabulary and the bit cursor
// the whole batch shares.
type bitDecoder struct {
	m       *tabletext.Model
	v       *Vocabulary
	report  *tabletext.Report
	r       *bitReader
	refBits int
	// spots, when set, collects the bit position and width of every
	// REFERENCE the read meets, which is what the wire fuzzer's reference
	// pass mutates (§3.3, §4.2)
	spots *[]BitSpot
	// steps, when set, counts the elements the skip path WALKS one at a time
	// and stops the walk at its limit. It is a test's instrument and nil on
	// every real read: the skip's work is pinned by a count, never a clock.
	steps *skipSteps
}

// skipSteps is the skip walk's element count and the count it stops at.
type skipSteps struct{ n, limit uint64 }

// BitSpot is one reference's place in a batch's bit stream, at
// `bits_required(0, E)`.
type BitSpot struct {
	Off, Width int
	Value      uint64
}

// reference reads one reference at the vocabulary's width, recording it.
func (d *bitDecoder) reference() (uint64, bool) {
	at := d.r.off
	v, ok := d.r.get(d.refBits)
	if ok && d.spots != nil {
		*d.spots = append(*d.spots, BitSpot{Off: at, Width: d.refBits, Value: v})
	}
	return v, ok
}

// entry is one slot's announced entry, and ok is false for a reference that
// names no slot, which is damage on the batch that carries it.
func (d *bitDecoder) entry(ref uint64) (ir.TableVocabularyEntry, bool) {
	if ref == 0 || ref > uint64(len(d.v.entries)) {
		return ir.TableVocabularyEntry{}, false
	}
	return d.v.entries[ref-1], true
}

// reserved reports one of the three ids the language holds back
// (docs/SPEC-TABLES.md §3.1, §3.3, §5): each is malformed anywhere but its own
// transport, and the rule OUTRANKS the wrong-sort rule below.
func reserved(id uint64) bool {
	return id == ir.TableBuildVersionWireId || id == ir.TableMessageVocabularyWireId || id == ir.TableNodeWireId
}

// name resolves a reference used as a VALUE, which is an enum's variant, a
// keyed array's slot key or a node record's type id, and which must name a
// kind-0 entry (docs/SPEC-TABLES.md §3.3). A reference of `0` where an entry is required,
// one above `E`, one naming a reserved id, and one naming an entry that
// carries a payload are each damage: the reader RESOLVED the entry and it
// contradicts the position it was used in, so the next bit's meaning is what
// is in doubt. ok is false with `malformed` set.
func (d *bitDecoder) name(ref uint64) (ir.TableVocabularyEntry, bool) {
	entry, named := d.entry(ref)
	if !named || reserved(entry.Id) || entry.Kind != 0 {
		d.report.Malformed = true
		return ir.TableVocabularyEntry{}, false
	}
	return entry, true
}

// arm resolves a UNION's arm reference, which must name an entry carrying the
// arm's own kind and shape: a kind-0 entry frames nothing, and a reserved id
// belongs to no arm (docs/SPEC-TABLES.md §3.3). ok is false with `malformed`
// set.
func (d *bitDecoder) arm(ref uint64) (ir.TableVocabularyEntry, bool) {
	entry, named := d.entry(ref)
	if !named || reserved(entry.Id) || entry.Kind == 0 {
		d.report.Malformed = true
		return ir.TableVocabularyEntry{}, false
	}
	return entry, true
}

// messageWidens is §4's WIDENING RULE read off the ANNOUNCEMENT rather than
// off a kind byte (docs/SPEC-TABLES.md §3.3, §4): an announced kind below this
// reader's on the same ladder, at a field, an arm or a map key; or, where both
// sides announce an array, an announced ELEMENT kind below this reader's. The
// shapes are the sender's either way, so the payload is already read at the
// width the announcement states.
func messageWidens(theirs, mine ir.TableVocabularyEntry) bool {
	if theirs.Shape.Elem == mine.Shape.Elem {
		return ir.TableKindWidens(int(theirs.Kind), int(mine.Kind))
	}
	if theirs.Kind == mine.Kind && int(mine.Kind) == ir.TableKindArray {
		return ir.TableKindWidens(int(theirs.Shape.Elem), int(mine.Shape.Elem))
	}
	return false
}

// body decodes one table body: fields until the ZERO REFERENCE that ends it.
func (d *bitDecoder) body(inst *tabletext.Instance) bool {
	index := map[uint64]int{}
	for i := range inst.Fields {
		index[ir.TableFieldWireId(inst.Fields[i].Def)] = i
	}
	for {
		ref, ok := d.reference()
		if !ok {
			d.report.Malformed = true
			return false
		}
		if ref == 0 {
			return true
		}
		entry, named := d.entry(ref)
		if !named {
			d.report.Malformed = true
			return false
		}
		switch entry.Id {
		case ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId, ir.TableNodeWireId:
			// A RESERVED ID IN A BODY IS MALFORMED (§3.1, §3.3): a fixed root
			// numbers no node, and the other two are the announcement's.
			d.report.Malformed = true
			return false
		}
		i, known := index[entry.Id]
		if !known {
			d.report.Unknown++
			if !d.skip(entry) {
				d.report.Malformed = true
				return false
			}
			continue
		}
		fv := &inst.Fields[i]
		mine := ir.TableFieldEntry(fv.Def)
		if entry.Kind != mine.Kind || entry.Shape.Elem != mine.Shape.Elem {
			if messageWidens(entry, mine) {
				// WIDENED (§4), and §3.3 holds that row to the variable form's
				// word: a kind below this reader's on the same ladder decodes
				// EXACTLY at the SENDER's announced width, the value lands,
				// and one `widened` counts. The field rode, so an optional is
				// PRESENT.
				if !d.field(fv, entry) {
					return false
				}
				d.report.Widened++
				if fv.Def.Type.Optional && fv.Def.Array == ir.ArrayNone {
					fv.Present = true
				}
				continue
			}
			// THE KIND MISMATCH IS FOUND IN THE ANNOUNCEMENT, not on the body.
			// A RANGE that moved is NOT one: the shapes differ and the entry
			// carries the sender's, which is exactly what makes a body from
			// another build the ordinary case this wire exists for.
			d.report.KindMismatch++
			if !d.skip(entry) {
				d.report.Malformed = true
				return false
			}
			continue
		}
		if !d.field(fv, entry) {
			return false
		}
		if fv.Def.Type.Optional && fv.Def.Array == ir.ArrayNone {
			fv.Present = true
		}
	}
}

func (d *bitDecoder) field(fv *tabletext.Field, entry ir.TableVocabularyEntry) bool {
	f := fv.Def
	shape := entry.Shape
	switch {
	case f.KeyEnum != "":
		return d.keyed(fv, entry)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return d.text(&fv.Cell, &fv.Count, f, entry)
	case f.Array != ir.ArrayNone:
		return d.array(fv, entry)
	case tabletext.UnionOf(f) != nil:
		return d.unionCell(&fv.Cell, fv, -1)
	case tabletext.EnumOf(f) != nil:
		return d.enumCell(&fv.Cell, f, false)
	case tabletext.StructOf(f) != nil:
		fv.Cell.Tab = d.m.New(tabletext.StructOf(f))
		return d.body(fv.Cell.Tab)
	}
	return d.scalar(&fv.Cell, f, entry.Kind, shape)
}

// text reads a `string(N)` or a `bytes(N)`: the length at its own width, the
// ALIGN that buys a memcpy, then the bytes. A payload longer than this
// reader's bound keeps what fits and counts `clamped`, which is not damage,
// because the length was read and the position after it is known.
//
// The content rule is kind 12's own (§3), reached through the same textValid
// and textBoundary the variable form reads with: a payload that is not well-formed
// UTF-8, or that carries a zero byte, is DAMAGE, checked as the bytes arrive
// and before this reader's bound, and a clamp cuts at a code point boundary. A
// `bytes(N)` carries no such rule, because its payload is bytes.
func (d *bitDecoder) text(cell *tabletext.Cell, count *int, f *ir.Field, entry ir.TableVocabularyEntry) bool {
	shape := entry.Shape
	width := ir.TableMessageBitsRequired(0, shape.Max)
	if f.Type.Kind == ir.TBytes {
		width = ir.TableMessageCountBits(shape)
	}
	n, ok := d.r.get(width)
	if !ok || !d.r.align() || !d.r.has(int(n)*8) {
		d.report.Malformed = true
		return false
	}
	raw, _ := d.r.bytes(int(n))
	if f.Type.Kind == ir.TString && !textValid(raw) {
		// ILL-FORMED TEXT IS DAMAGE HERE TOO, AND IT IS TERMINAL (§3.3): a bit
		// stream has no defined position to continue from, which is the only
		// thing that differs from §3's recovery
		d.report.Malformed = true
		return false
	}
	keep := len(raw)
	if bound := int(f.Type.Size); keep > bound {
		keep = bound
		if f.Type.Kind == ir.TString {
			// A CLAMP CUTS AT A CODE POINT BOUNDARY (§3, §3.3): the last whole
			// code point that fits within the bound
			keep = textBoundary(raw, bound)
		}
		d.report.Clamped++
	}
	cell.Str = raw[:keep]
	*count = keep
	return true
}

// elementRunBits is the bits ONE element of an ARRAY occupies where that is a
// fixed number this reader can multiply, and -1 where the element has to be
// stepped one at a time. A nested body, a union arm, an enum's variant and a
// node index each RESOLVE something, and a resolve that contradicts its
// position is damage this reader must still find, so they are walked however
// many there are; a keyed entry's slot carries a key reference for the same
// reason. A ZERO is a real answer, and it is why this exists: a ranged element
// whose `min` equals its `max` rides no bits at all (§3.3).
func elementRunBits(kind, elem uint8, inner ir.TableMessageShape) int64 {
	if int(kind) != ir.TableKindArray {
		return -1
	}
	switch int(elem) {
	case ir.TableKindTable, ir.TableKindUnion, ir.TableKindEnum, ir.TableKindPointer:
		return -1
	case 0, ir.TableKindNoPayload:
		// A NAME AND A PAYLOAD-FREE ARM RIDE NO BITS (docs/SPEC-TABLES.md
		// §3.3), so a run of them is a zero-width run like a ranged element
		// whose `min` equals its `max`, and it is arithmetic: walked one at a
		// time, a count of 2^32 over one bought 2^32 iterations for a few
		// bytes of wire. The C# reader takes them the same way.
		return 0
	}
	return ir.TableMessageValueBits(elem, inner)
}

// skipRun steps over `n` elements of one fixed width in a single arithmetic
// step, refusing where the product runs past what a bit position holds. It is
// what keeps a wide count from buying a loop: a zero-width element under a
// count of 2^31 is six bytes of wire, and nothing in this form is superlinear
// in a batch's length (§3.3).
func skipRun(r *bitReader, n uint64, width int64) bool {
	if width == 0 {
		return true
	}
	if n > uint64(math.MaxInt64)/uint64(width) {
		return false
	}
	return r.skip(int(n * uint64(width)))
}

// array decodes a positional array. NO COUNT RIDES where the shape's `min`
// equals its `max`, which is every fixed array. A count above the reader's own
// bound keeps the first `N` elements and counts `clamped`: the elements past
// it are stepped over, because the stream advances past them either way.
func (d *bitDecoder) array(fv *tabletext.Field, entry ir.TableVocabularyEntry) bool {
	f := fv.Def
	shape := entry.Shape
	n := uint64(shape.Min)
	if bits := ir.TableMessageCountBits(shape); bits > 0 {
		raw, ok := d.r.get(bits)
		if !ok {
			d.report.Malformed = true
			return false
		}
		n = raw + uint64(shape.Min)
	}
	if ir.TableMessageAligns(entry.Kind, shape) && !d.r.align() {
		d.report.Malformed = true
		return false
	}
	bound := uint64(f.ArrayBound)
	kept := n
	inner := ir.TableMessageShape{}
	if shape.Inner != nil {
		inner = *shape.Inner
	}
	run := elementRunBits(entry.Kind, shape.Elem, inner)
	if kept > bound {
		kept = bound
		d.report.Clamped++
	}
	// THE SURPLUS OF A FIXED-WIDTH ELEMENT IS ARITHMETIC (§3.3). The walk
	// exists to find the bit the array ENDS at, and where the element's width
	// is announced that bit is a multiplication rather than a loop, which is
	// also what keeps a zero-width element under a wide count bounded.
	walk := n
	if run >= 0 && walk > kept {
		walk = kept
	}
	for i := uint64(0); i < walk; i++ {
		// an element past this build's bound is read into a sink and dropped
		var sink tabletext.Cell
		cell := &sink
		if i < kept {
			cell = &fv.Elems[i]
		}
		if !d.element(fv, int(i), cell, shape) {
			return false
		}
	}
	if walk < n && !skipRun(d.r, n-walk, run) {
		d.report.Malformed = true
		return false
	}
	if f.CountedOnWire() {
		fv.Count = int(kept)
	}
	if f.Type.Optional {
		fv.Present = true
	}
	return true
}

// element reads one element at its element kind's own framing.
func (d *bitDecoder) element(fv *tabletext.Field, i int, cell *tabletext.Cell, shape ir.TableMessageShape) bool {
	f := fv.Def
	inner := ir.TableMessageShape{}
	if shape.Inner != nil {
		inner = *shape.Inner
	}
	switch int(shape.Elem) {
	case ir.TableKindTable:
		cell.Tab = d.m.New(tabletext.StructOf(f))
		return d.body(cell.Tab)
	case ir.TableKindUnion:
		return d.unionCell(cell, fv, i)
	case ir.TableKindEnum:
		return d.enumCell(cell, f, true)
	}
	return d.scalar(cell, f, shape.Elem, inner)
}

// keyed decodes an enum-keyed array (docs/SPEC-TABLES.md §3.2): the number of
// present slots, then one `(key reference, element)` pair per slot. A key this
// reader's enum cannot name drops its element and counts one `unknown`, and
// the element is READ either way, because the stream advances past it.
func (d *bitDecoder) keyed(fv *tabletext.Field, entry ir.TableVocabularyEntry) bool {
	f := fv.Def
	shape := entry.Shape
	n, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))
	if !ok {
		d.report.Malformed = true
		return false
	}
	for range n {
		ref, ok := d.reference()
		if !ok {
			d.report.Malformed = true
			return false
		}
		// a reference of `0` where an entry is REQUIRED is damage (§3.2), and
		// a key naming an entry of the wrong sort is damage too (§3.3)
		key, named := d.name(ref)
		if !named {
			return false
		}
		value := enumValueForId(f.KeyEnumRef, key.Id)
		var sink tabletext.Cell
		cell := &sink
		switch {
		case value < 0:
			d.report.Unknown++
		case value > int64(tabletext.KeyedSlotCount(f)):
			d.report.Clamped++
		default:
			cell = &fv.Elems[value-1]
		}
		if !d.element(fv, int(value-1), cell, shape) {
			return false
		}
	}
	return true
}

// unionCell reads a union: the ARM's reference, and then the payload a FIELD
// of the arm's type would carry. An arm this reader cannot name is §4's
// ordinary `unknown`, so the field reads None and one event counts, and the
// arm's payload is skipped by the ARM's own announced entry.
func (d *bitDecoder) unionCell(cell *tabletext.Cell, fv *tabletext.Field, index int) bool {
	f := fv.Def
	un := tabletext.UnionOf(f)
	ref, ok := d.reference()
	if !ok {
		d.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0
		return true
	}
	entry, named := d.arm(ref)
	if !named {
		return false
	}
	tag := 0
	for i, v := range un.Variants {
		// the announcement's own entry, which reads the `was` alias
		// (docs/SPEC-TABLES.md §5): every id derivation does, so the
		// dispatch names what the writer wrote
		if ir.TableArmEntry(v).Id == entry.Id {
			tag = i + 1
			break
		}
	}
	if tag == 0 {
		cell.U = 0
		d.report.Unknown++
		return d.skip(entry)
	}
	arm := un.Variants[tag-1]
	mine := ir.TableArmEntry(arm)
	if entry.Kind != mine.Kind || entry.Shape.Elem != mine.Shape.Elem {
		if !messageWidens(entry, mine) {
			cell.U = 0
			d.report.KindMismatch++
			return d.skip(entry)
		}
		// WIDENED AT AN ARM (§3.3, §4): the arm is SELECTED and its payload
		// decodes at the width the announcement states, rather than skipped
		d.report.Widened++
	}
	cell.U = uint64(tag)
	switch {
	case arm.Void():
		return true
	case arm.Body():
		cell.Tab = d.m.New(arm.Ref)
		return d.body(cell.Tab)
	}
	afv := d.m.NewArm(arm)
	cell.Arm = afv
	af := arm.F
	switch {
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
		return d.text(&afv.Cell, &afv.Count, af, entry)
	case af.Array != ir.ArrayNone:
		return d.array(afv, entry)
	case tabletext.UnionOf(af) != nil:
		return d.unionCell(&afv.Cell, afv, -1)
	case tabletext.EnumOf(af) != nil:
		return d.enumCell(&afv.Cell, af, false)
	case tabletext.StructOf(af) != nil:
		afv.Cell.Tab = d.m.New(tabletext.StructOf(af))
		return d.body(afv.Cell.Tab)
	}
	return d.scalar(&afv.Cell, af, entry.Kind, entry.Shape)
}

// enumCell reads an enum value: the reference naming its VARIANT's name, `0`
// for None. A reference this reader's enum cannot name is §4's ordinary
// `unknown`.
func (d *bitDecoder) enumCell(cell *tabletext.Cell, f *ir.Field, element bool) bool {
	ref, ok := d.reference()
	if !ok {
		d.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0
		return true
	}
	entry, named := d.name(ref)
	if !named {
		return false
	}
	en := tabletext.EnumOf(f)
	v := enumValueForId(en, entry.Id)
	if v < 0 {
		cell.U = 0
		d.report.Unknown++
		return true
	}
	if tabletext.VariantDeprecated(en, v) {
		// A DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, and as
		// the ELEMENT default for an element; never unknown (§2.15).
		if element {
			*cell = tabletext.Cell{}
		} else {
			*cell = d.m.FieldDefaultCell(f)
		}
		return true
	}
	cell.U = uint64(v)
	return true
}

// scalar reads ONE value at the width the SENDER's shape states and against
// its range base, and then applies THIS reader's own bounds: a value outside
// them clamps and counts, exactly as it does on a file (§4). That is what
// keeps every evolution row standing under a bitpacked body.
func (d *bitDecoder) scalar(cell *tabletext.Cell, f *ir.Field, kind uint8, shape ir.TableMessageShape) bool {
	switch int(kind) {
	case ir.TableKindBool:
		v, ok := d.r.get(1)
		if !ok {
			d.report.Malformed = true
			return false
		}
		cell.B = v != 0
		return true
	case ir.TableKindF64:
		raw, ok := d.r.get(64)
		if !ok {
			d.report.Malformed = true
			return false
		}
		if !tabletext.FiniteF64(raw) {
			// refused on its raw bits (docs/SPEC-TABLES.md §3.3), as a file read does
			d.report.RefuseNonFinite(FloatOwner(d.m, f))
			return true
		}
		cell.F = math.Float64frombits(raw)
		return true
	case ir.TableKindF32:
		return d.float32(cell, f, shape)
	}
	width := int(ir.TableMessageValueBits(kind, shape))
	if width < 0 {
		return true
	}
	base := shape.Base
	if base == nil {
		base = big.NewInt(0)
	}
	if ir.TableKindWide(int(kind)) {
		var value *big.Int
		if shape.Packing == ir.TableMessageRanged {
			offset, ok := d.r.getBig(width)
			if !ok {
				d.report.Malformed = true
				return false
			}
			value = new(big.Int).Add(offset, base)
		} else {
			raw, ok := d.r.bytes(16)
			if !ok {
				d.report.Malformed = true
				return false
			}
			value = tabletext.WideFromBytes(raw, int(kind))
		}
		clamped := false
		cell.Wide, clamped = tabletext.WideClamp(value, f)
		if clamped {
			d.report.Clamped++
		}
		return true
	}
	raw, ok := d.r.get(width)
	if !ok {
		d.report.Malformed = true
		return false
	}
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	var value int64
	switch {
	case shape.Packing == ir.TableMessageRanged && signed:
		value = int64(raw) + base.Int64()
	case shape.Packing == ir.TableMessageRanged:
		value = int64(raw + base.Uint64()) // the unsigned domain, whole: the sum wraps into the bits below
	case signed && width < 64 && width > 0:
		shift := uint(64 - width)
		value = int64(raw<<shift) >> shift
	default:
		value = int64(raw)
	}
	if ir.TableKindWide(ir.TableScalarKind(f)) {
		// an integer kind WIDENED into a 128-bit declaration (§4): sixteen
		// bytes extended from the reconstructed value, then the declared range
		// on the raw scale, exactly as a payload at 128 bits takes it
		var wide [16]byte
		binary.LittleEndian.PutUint64(wide[:8], uint64(value))
		if signed && value < 0 {
			for i := 8; i < 16; i++ {
				wide[i] = 0xFF
			}
		}
		clamped := false
		cell.Wide, clamped = tabletext.WideClamp(tabletext.WideFromBytes(wide[:], ir.TableScalarKind(f)), f)
		if clamped {
			d.report.Clamped++
		}
		return true
	}
	if f.HasIntRange {
		lo, hi := bigInt64(f.IntMin), bigInt64(f.IntMax)
		if signed {
			if value < lo {
				value = lo
				d.report.Clamped++
			} else if value > hi {
				value = hi
				d.report.Clamped++
			}
		} else {
			// the bounds are read whole: an unsigned range reaches 2^64 - 1
			u, ulo, uhi := uint64(value), f.IntMin.Uint64(), f.IntMax.Uint64()
			if u < ulo {
				u = ulo
				d.report.Clamped++
			} else if u > uhi {
				u = uhi
				d.report.Clamped++
			}
			value = int64(u)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
		max := uint64(1)<<uint(f.Type.Width) - 1
		if uint64(value) > max {
			value = int64(max)
			d.report.Clamped++
		}
	}
	cell.I = value
	cell.U = uint64(value)
	if !signed && f.Type.Width < 64 && f.Type.Width > 0 {
		cell.U = uint64(value) & (uint64(1)<<uint(f.Type.Width) - 1)
		cell.I = int64(cell.U)
	}
	return true
}

// float32 reads a f32 at the sender's packing: the IEEE bit pattern where it
// rides raw, and `min + index * step` where the sender quantized it.
func (d *bitDecoder) float32(cell *tabletext.Cell, f *ir.Field, shape ir.TableMessageShape) bool {
	if shape.Packing == ir.TableMessageQuantized {
		index, ok := d.r.get(int(shape.Bits))
		if !ok {
			d.report.Malformed = true
			return false
		}
		// AN INDEX ABOVE `count` IS REJECTED, as the packet wire rejects it,
		// and is never reconstructed and clamped (§3.3, SPEC.md §4.3). The
		// width spells such an index whenever `count` is not one less than a
		// power of two, ten bits spelling 1023 over a count of 1000, and the
		// ranged offset's reconstruct-and-clamp rule does not reach here.
		count, _, derived := ir.TableMessageQuantization(shape)
		if !derived || index > uint64(count) {
			d.report.Malformed = true
			return false
		}
		// THE PACKET WIRE'S RULE, IN FLOAT32 (SPEC.md §4.3, §3.3): the float an
		// index names is the float a packet's reader names for it
		v := float64(ir.TableMessageDequantize(shape, uint32(index)))
		if ir.TableScalarKind(f) == ir.TableKindF64 {
			cell.F = v // widened into f64 (§4): exact, and no clamp fires
			return true
		}
		cell.F = float64(float32(d.clampFloat(v, f)))
		return true
	}
	raw, ok := d.r.get(32)
	if !ok {
		d.report.Malformed = true
		return false
	}
	bits := uint32(raw)
	if !tabletext.FiniteF32(bits) {
		// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (docs/SPEC-TABLES.md
		// §3.3), before the bits become a value, as a file read refuses it
		d.report.RefuseNonFinite(FloatOwner(d.m, f))
		return true
	}
	v := float64(math.Float32frombits(bits))
	if ir.TableScalarKind(f) == ir.TableKindF64 {
		// f32 WIDENED into f64 (§4): every float32 value is exactly
		// representable, and a float64 field's declared range clamps nothing
		// on this wire, exactly as a payload at sixty-four bits
		cell.F = v
		return true
	}
	cell.F = float64(float32(d.clampFloat(v, f)))
	return true
}

func (d *bitDecoder) clampFloat(v float64, f *ir.Field) float64 {
	if !f.HasFloatRange {
		return v
	}
	// THE ONE RULE (§3.3, the special values): a value is finite here, because
	// the read refused a NaN or an infinity on its bits first; one past an end
	// clamps and counts, and a -0 lands as +0, uncounted.
	if v < f.FMin {
		d.report.Clamped++
		return f.FMin
	}
	if v > f.FMax {
		d.report.Clamped++
		return f.FMax
	}
	if v == 0 {
		return 0
	}
	return v
}

// skip steps past one field's payload without decoding it, using the announced
// ENTRY alone (docs/SPEC-TABLES.md §3.3). It is what makes an unknown entry
// skippable on a body with no kind byte, and it is one walk over every table,
// because a shape says everything a skipper needs.
func (d *bitDecoder) skip(entry ir.TableVocabularyEntry) bool {
	shape := entry.Shape
	switch int(entry.Kind) {
	case 0, ir.TableKindNoPayload:
		return true
	case ir.TableKindEnum:
		// A VARIANT REFERENCE IS RESOLVED EVEN ON THE SKIP PATH (§3.3): every
		// reference above `E` is damage, and one naming an entry that carries
		// a payload contradicts the position it was used in, whether or not
		// this reader was going to keep the value.
		ref, ok := d.reference()
		if !ok {
			d.report.Malformed = true
			return false
		}
		if ref == 0 {
			return true // None: the reference is the whole payload
		}
		_, named := d.name(ref)
		return named
	case ir.TableKindUnion:
		ref, ok := d.reference()
		if !ok {
			return false
		}
		if ref == 0 {
			return true
		}
		arm, named := d.arm(ref)
		if !named {
			return false
		}
		return d.skip(arm)
	case ir.TableKindTable:
		return d.skipBody()
	case ir.TableKindPointer:
		// A FIXED ROOT NUMBERS NO NODE, so a node index has no width to be
		// stepped over at, and an entry of kind 17 in its batch is damage
		// (§3.1, §3.3)
		return false
	case ir.TableKindString:
		n, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))
		if !ok || !d.r.align() {
			return false
		}
		return d.r.skip(int(n) * 8)
	case ir.TableKindEscape:
		// THE ESCAPE: align, a thirty-two bit L, then L bytes, opaque. It is
		// the one path a later-major writer has on this form (§3.3)
		if !d.r.align() {
			return false
		}
		n, ok := d.r.get(32)
		if !ok {
			return false
		}
		return d.r.skip(int(n) * 8)
	case ir.TableKindArray, ir.TableKindKeyed:
		n := uint64(shape.Min)
		width := ir.TableMessageCountBits(shape)
		if entry.Kind == ir.TableKindKeyed {
			width, n = ir.TableMessageBitsRequired(0, shape.Max), 0
		}
		if width > 0 {
			raw, ok := d.r.get(width)
			if !ok {
				return false
			}
			n = raw
			if entry.Kind == ir.TableKindArray {
				n += uint64(shape.Min)
			}
		}
		if ir.TableMessageAligns(entry.Kind, shape) && !d.r.align() {
			return false
		}
		inner := ir.TableMessageShape{}
		if shape.Inner != nil {
			inner = *shape.Inner
		}
		// A RUN OF FIXED-WIDTH ELEMENTS IS ARITHMETIC (§3.3), and it has to
		// be: a ranged element whose `min` equals its `max` rides no bits at
		// all, so a count of 2^31 over one would otherwise buy 2^31 loop
		// iterations for six bytes of wire.
		if run := elementRunBits(entry.Kind, shape.Elem, inner); run >= 0 {
			return skipRun(d.r, n, run)
		}
		for i := uint64(0); i < n; i++ {
			if d.steps != nil {
				if d.steps.n++; d.steps.n >= d.steps.limit {
					return false
				}
			}
			at := d.r.off
			if entry.Kind == ir.TableKindKeyed {
				if _, ok := d.reference(); !ok {
					return false
				}
			}
			if !d.skip(ir.TableVocabularyEntry{Kind: shape.Elem, Shape: inner}) {
				return false
			}
			// A WALKED ELEMENT THAT CONSUMED NO BITS, a nested array whose
			// inner run is itself zero-width, is the same run in a shape the
			// arithmetic above cannot see (docs/SPEC-TABLES.md §3.3). Its skip
			// read nothing, so it took no branch a later element could take
			// differently, and every element after it consumes nothing too: the
			// rest of the run is stepped in one. A REMAINING COUNT THE BITS
			// LEFT COULD NOT BACK IS DAMAGE, the one bit an element is held to
			// on the list path, so a hostile count is refused here rather than
			// carried into a retaining walk, and the walk is bounded by the
			// batch's bits whatever the count says.
			if d.r.off == at {
				if n-i-1 > uint64(d.r.left()) {
					d.report.Malformed = true
					return false
				}
				return true
			}
		}
		return true
	}
	width := int(ir.TableMessageValueBits(entry.Kind, shape))
	if width < 0 {
		return false
	}
	return d.r.skip(width)
}

// skipBody walks a nested body it cannot name, field by field, against the
// same announced vocabulary its parent resolves through.
func (d *bitDecoder) skipBody() bool {
	for {
		ref, ok := d.reference()
		if !ok {
			return false
		}
		if ref == 0 {
			return true
		}
		entry, named := d.entry(ref)
		if !named || !d.skip(entry) {
			return false
		}
	}
}
