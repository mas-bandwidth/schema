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
// A BATCH IS OF ONE ROOT, and which root it is, is the APPLICATION's: a peer
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
//   - DAMAGE INSIDE BODY `k` DELIVERS BODIES 1 TO `k - 1`: the returned count
//     says `k - 1`, one `malformed` counts, and nothing at or after body `k`
//     is read. The storage for body `k` holds whatever the decode had put in
//     it, and the COUNT is what says it is not a body.
func DecodeMessages(m *tabletext.Model, insts []*tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (int, bool, error) {
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
	// A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
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
		d := &bitDecoder{m: m, v: v, report: report, r: r, refBits: v.RefBits(), indexBits: ir.TableMessageBitsRequired(0, 1)}
		if !d.root(insts[i]) {
			return i, false, nil
		}
	}
	// THE TRAILING PAD IS VERIFIED ZERO, which is the packet wire's rule for
	// the same reason (SPEC.md §4.3), and BYTES AFTER THE PAD ARE MALFORMED:
	// the batch ends at the pad and a buffer with bytes left over describes no
	// batch this reader can name. Both are damage AFTER the last body, so the
	// count stands at `M` and the bodies stand with it.
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

// bitDecoder is one body's read: the announced vocabulary, the bit cursor the
// whole batch shares, and the numbering a pointered root resolves through.
type bitDecoder struct {
	m         *tabletext.Model
	v         *Vocabulary
	report    *tabletext.Report
	r         *bitReader
	refBits   int
	indexBits int
	st        *decodeState
	// spots, when set, collects the bit position and width of every
	// REFERENCE and every NODE INDEX the read meets, which is what the wire
	// fuzzer's reference pass mutates (§3.3, §4.2)
	spots *[]BitSpot
	// mapKey, when set, is the flag ONE map raises where its KEY kind is one
	// this reader's declaration widens: the map counts ONE `widened` however
	// many entries carry it (§2.8, §4). It is consumed by the entry body it
	// was set for, so a nested body never sees it.
	mapKey *bool
}

// BitSpot is one number's place in a batch's bit stream: a reference at
// `bits_required(0, E)` or a node index at the body's index width.
type BitSpot struct {
	Off, Width int
	Value      uint64
	Index      bool // a node index rather than a reference
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

// index reads one node index at the body's width, recording it.
func (d *bitDecoder) index() (uint64, bool) {
	at := d.r.off
	if d.indexBits <= 0 {
		return 0, false // no numbering: no width to read an index at
	}
	v, ok := d.r.get(d.indexBits)
	if ok && d.spots != nil {
		*d.spots = append(*d.spots, BitSpot{Off: at, Width: d.indexBits, Value: v, Index: true})
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

// root decodes one body of a batch. A VARIABLE-class root reads its NODE TABLE
// first, because it is the body's first field and a pointer index's width is
// settled by the node count it carries.
func (d *bitDecoder) root(inst *tabletext.Instance) bool {
	if !ir.VariableTables(d.m.Unit)[inst.Def.Name] {
		// A FIXED ROOT NUMBERS NO NODE, so its batch has no index width: an
		// unknown entry of kind 17 inside one cannot be stepped over and is
		// damage (§3.1, §3.3)
		d.indexBits = 0
		return d.body(inst)
	}
	st := &decodeState{root: inst}
	d.st = st
	if !d.nodeTable(inst, st) {
		return false
	}
	return d.body(inst)
}

// nodeTable reads the ROOT body's first field when it is the node table
// (§3.1): the numbering, whole, so an index resolves whichever way it points.
func (d *bitDecoder) nodeTable(inst *tabletext.Instance, st *decodeState) bool {
	at := d.r.off
	ref, ok := d.reference()
	if !ok {
		d.report.Malformed = true
		return false
	}
	entry, named := d.entry(ref)
	if !named && ref != 0 {
		d.report.Malformed = true
		return false
	}
	if !named || entry.Id != ir.TableNodeWireId {
		// a pointered root that reaches no node writes no node table, exactly
		// as every other empty thing is not written (§3.1)
		d.r.off = at
		st.good = true
		return true
	}
	count, ok := d.r.get(32)
	if !ok {
		d.report.Malformed = true
		return false
	}
	d.indexBits = ir.TableMessageBitsRequired(0, int64(count)+1)

	// THE NODE TYPES THIS ROOT CAN PLACE are the numbering's own walk, and the
	// id is the WIRE name's so a table renamed under `was` is the node type
	// its old name numbers (§3.1, §5). It is the file form's map, built the
	// same way over the same walk.
	//
	// The PLACEABLE SET is named on its own line, before the id map is built
	// from it, because it is the seam the node-type negative control replaces
	// with the whole unit closure. The MESSAGE form is where that control
	// bites: a connection's table announces every table's name id whether or
	// not a pointer names it (§3.3), so this is the only form whose wire can
	// carry a record of a type no pointer below the root targets.
	byTypeId := map[uint64]*ir.Struct{}
	placeable := map[string]bool{}
	for _, st := range ir.PointerReachable(inst.Def) {
		placeable[st.Name] = true
	}
	for name := range placeable {
		if sd := d.m.Lookup(name); sd != nil {
			byTypeId[ir.TableWireId(sd.WireName())] = sd
		}
	}
	// A RECORD'S FRAMING IS ITS TYPE ID'S, AND ITS PLACEMENT IS THE ROOT'S,
	// which are two questions and are asked separately here (§2.5, §3.1, §3.3).
	// The two RESERVED BLOB IDS say a thirty-two bit length, an align and the
	// bytes wherever they appear, and every reader knows that by the id alone
	// because the announcement's tail carries both ids whether or not this
	// root names them. A bit stream has no length for a reader to step a
	// record over by, so a reader that framed a blob record as a TABLE body
	// would be reading its bytes as fields and would not be reading the same
	// wire. Whether this ROOT can PLACE such a node is the second question,
	// answered below by the edges its pointers reach.
	blobKind := map[uint64]ir.FieldTypeKind{}
	bytesEdge, stringEdge := ir.PointerReachableBlobs(inst.Def)
	if bytesEdge {
		blobKind[ir.BytesWireTypeId] = ir.TBytes
	}
	if stringEdge {
		blobKind[ir.StringWireTypeId] = ir.TString
	}
	blobFramed := map[uint64]bool{ir.BytesWireTypeId: true, ir.StringWireTypeId: true}

	// LOAD IS A SCAN, and a bit stream forces the same two passes a file's
	// lengths allowed (§3.1): PASS ONE walks the records to learn what each
	// node IS and where its bits are, so that an index resolves whichever way
	// it points, and PASS TWO decodes each body into its own storage.
	type record struct {
		typeId     uint64
		blob       []byte
		start, end int
	}
	// THE COUNT IS THE WIRE'S CLAIM, NOT A SIZE TO TRUST. It is a 32-bit
	// number off hostile bytes, and reserving it outright lets a mutated
	// message command gigabytes before a single record has been read. A record
	// costs AT LEAST its type reference, so the bits left in the stream bound
	// how many the wire can possibly carry, and the reservation is the smaller
	// of the claim and that bound. The loop below is unchanged: it reads until
	// the stream runs out, which is what settles the real count.
	records := make([]record, 0, min(int64(count), d.r.left()/int64(max(d.refBits, 1))))
	for range count {
		typeRef, ok := d.reference()
		if !ok {
			d.report.Malformed = true
			return false
		}
		// a type id REFERENCE of 0 is damage, and so is one naming anything
		// but a kind-0 entry: a record must say what it is
		typeEntry, named := d.name(typeRef)
		if !named {
			return false
		}
		if blobFramed[typeEntry.Id] {
			n, ok := d.r.get(32)
			if !ok || !d.r.align() || !d.r.has(int(n)*8) {
				d.report.Malformed = true
				return false
			}
			data, _ := d.r.bytes(int(n))
			records = append(records, record{typeId: typeEntry.Id, blob: data})
			continue
		}
		at := d.r.off
		if !d.skipBody() {
			d.report.Malformed = true
			return false
		}
		records = append(records, record{typeId: typeEntry.Id, start: at, end: d.r.off})
	}

	st.nodes = make([]Node, len(records))
	for i, rec := range records {
		if blobFramed[rec.typeId] {
			kind, placeable := blobKind[rec.typeId]
			if !placeable {
				// THIS ROOT REACHES NO SUCH BLOB EDGE, so the record commands
				// no storage, its bytes are the ones the framing already
				// stepped over, and ONE unknown counts at the node (§2.5)
				d.report.Unknown++
				continue
			}
			st.nodes[i] = Node{Blob: &tabletext.Blob{Data: rec.blob}, Kind: kind}
			continue
		}
		sd := byTypeId[rec.typeId]
		if sd == nil {
			// a node whose type id this reader cannot name KEEPS ITS INDEX,
			// and every pointer naming it reads null. The unknown is counted
			// once, at the node, not once per pointer.
			d.report.Unknown++
			continue
		}
		st.nodes[i] = Node{Inst: d.m.New(sd)}
	}
	st.good = true
	for i, rec := range records {
		if st.nodes[i].Inst == nil {
			continue
		}
		sub := &bitDecoder{m: d.m, v: d.v, report: d.report, refBits: d.refBits, indexBits: d.indexBits, st: st, spots: d.spots,
			r: &bitReader{b: d.r.b, n: rec.end, off: rec.start}}
		if !sub.body(st.nodes[i].Inst) {
			return false
		}
	}
	st.good = true
	return true
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
	// the MAP's key flag belongs to THIS body's own fields (§2.8): a nested
	// body below it is an ordinary body and counts its own events
	mapKey := d.mapKey
	d.mapKey = nil
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
			// A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS
			// MALFORMED (§3.1, §3.3). The node table is the ROOT body's first
			// field and is read before this walk begins, so meeting one here
			// is a second numbering wherever it sits.
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
				// WIDENED (§4), and §3.3 holds that row to the file form's
				// word: a kind below this reader's on the same ladder decodes
				// EXACTLY at the SENDER's announced width, the value lands,
				// and one `widened` counts. The field rode, so an optional is
				// PRESENT.
				if !d.field(fv, entry) {
					return false
				}
				if mapKey != nil && entry.Id == ir.MapKeyWireId {
					*mapKey = true // the MAP counts ONE, at the key (§2.8, §4)
				} else {
					d.report.Widened++
				}
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
	case f.IsMap():
		return d.mapField(fv, entry)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index, ok := d.index()
		if !ok {
			d.report.Malformed = true
			return false
		}
		d.resolveCell(&fv.Cell, f, index)
		return true
	case f.KeyEnum != "":
		return d.keyed(fv, entry)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return d.text(&fv.Cell, &fv.Count, f, entry)
	case f.Type.Kind == ir.TWString:
		return d.wideText(&fv.Cell, &fv.Count, f, entry)
	case f.Array != ir.ArrayNone:
		return d.array(fv, entry)
	case tabletext.UnionOf(f) != nil:
		return d.unionCell(&fv.Cell, f)
	case tabletext.EnumOf(f) != nil:
		return d.enumCell(&fv.Cell, f)
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
// and textBoundary the file form reads with: a payload that is not well-formed
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

// wideText reads a `wstring(N)` on this form (docs/SPEC-TABLES.md §3.3): the
// length at bits_required( 0, max ), NO align, then SIXTEEN BITS A CODE UNIT.
// The content rule is kind 33's own (§3): an unpaired surrogate or a zero unit
// is DAMAGE, checked as the units arrive and before this reader's bound, and
// a payload longer than the bound clamps without splitting a pair.
func (d *bitDecoder) wideText(cell *tabletext.Cell, count *int, f *ir.Field, entry ir.TableVocabularyEntry) bool {
	n, ok := d.r.get(ir.TableMessageBitsRequired(0, entry.Shape.Max))
	if !ok || !d.r.has(int(n)*16) {
		d.report.Malformed = true
		return false
	}
	units := make([]uint16, n)
	for i := range units {
		u, got := d.r.get(16)
		if !got {
			d.report.Malformed = true
			return false
		}
		units[i] = uint16(u)
	}
	if !wideValid(units) {
		// ILL-FORMED TEXT IS DAMAGE HERE TOO, AND IT IS TERMINAL (§3.3): a bit
		// stream has no defined position to continue from, which is the only
		// thing that differs from §3's recovery
		d.report.Malformed = true
		return false
	}
	keep := len(units)
	if bound := int(f.Type.Size); keep > bound {
		keep = wideBoundary(units, bound)
		d.report.Clamped++
	}
	cell.Units = units[:keep]
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
	switch {
	case f.Array == ir.ArrayList:
		// AN UNBOUNDED ARRAY HAS NO BOUND TO CLAMP AGAINST (§2.9): its count
		// is the thirty-two bits the data decides, and its slots are grown
		// one at a time against the batch's own bits, so a count no batch can
		// cover allocates nothing past the damage
		if n > uint64(math.MaxInt32) {
			d.report.Malformed = true
			return false
		}
		// AND THE BITS HAVE TO BACK THE COUNT, which the growth alone does
		// not settle once an element can be free: a ranged element whose
		// `min` equals its `max` rides no bits, so an element costs AT LEAST
		// one bit for this bound and a count the batch cannot cover is damage
		// rather than two billion slots bought with six bytes (§3.3)
		floor := max(run, 1)
		if int64(n) > d.r.left()/floor {
			d.report.Malformed = true
			return false
		}
		fv.Elems = fv.Elems[:0]
	case kept > bound:
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
		var sink tabletext.Cell
		cell := &sink
		if f.Array == ir.ArrayList {
			fv.Elems = append(fv.Elems, d.m.ElementZero(f))
			cell = &fv.Elems[i]
		} else if i < kept {
			cell = &fv.Elems[i]
		}
		if !d.element(cell, f, shape) {
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
func (d *bitDecoder) element(cell *tabletext.Cell, f *ir.Field, shape ir.TableMessageShape) bool {
	inner := ir.TableMessageShape{}
	if shape.Inner != nil {
		inner = *shape.Inner
	}
	switch int(shape.Elem) {
	case ir.TableKindTable:
		cell.Tab = d.m.New(tabletext.StructOf(f))
		return d.body(cell.Tab)
	case ir.TableKindPointer:
		index, ok := d.index()
		if !ok {
			d.report.Malformed = true
			return false
		}
		d.resolveCell(cell, f, index)
		return true
	case ir.TableKindUnion:
		return d.unionCell(cell, f)
	case ir.TableKindEnum:
		return d.enumCell(cell, f)
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
		if !d.element(cell, f, shape) {
			return false
		}
	}
	return true
}

// mapField decodes one map (docs/SPEC-TABLES.md §2.8, §3.3): the count at the
// thirty-two bits the data decides, then the generated `{ key, value }`
// bodies, each decoded whole and then placed by its key: ascending against
// the key of the last entry that LANDED, a duplicate replacing the slot it
// took, a key that does not fit dropped whole and counted `clamped`. A
// DESCENDING key is damage the batch cannot recover from, because there is no
// map L for the parent to read on past.
func (d *bitDecoder) mapField(fv *tabletext.Field, entry ir.TableVocabularyEntry) bool {
	f := fv.Def
	n, ok := d.r.get(ir.TableMessageCountBits(entry.Shape))
	if !ok {
		d.report.Malformed = true
		return false
	}
	n += uint64(entry.Shape.Min)
	if n > uint64(math.MaxInt32) {
		d.report.Malformed = true
		return false
	}
	fv.Entries = nil
	var last *tabletext.Instance
	// A KEY KIND THIS DECLARATION WIDENS IS NOT A DISAGREEMENT (§2.8, §4):
	// the entries land and the MAP counts ONE `widened`, however many keys
	// carry it, exactly as the `kind_mismatch` it replaces counted once.
	keyWidened := false
	for i := uint64(0); i < n; i++ {
		decoded := d.m.NewMapEntry(f)
		d.mapKey = &keyWidened
		ok := d.body(decoded)
		d.mapKey = nil
		if !ok {
			return false
		}
		key := tabletext.MapKeyOf(f, decoded)
		if !tabletext.MapKeyFits(f, key) {
			// KEYS NEVER CLAMP: the entry is dropped whole, one count per entry
			d.report.Clamped++
			continue
		}
		order := -1
		if last != nil {
			order = tabletext.MapKeyOrder(f, tabletext.MapKeyOf(f, last), key)
		}
		if order > 0 {
			d.report.Malformed = true // DESCENDING: not a body any conforming writer produced
			return false
		}
		if order == 0 {
			// EQUAL: a DUPLICATE. Last wins WHOLE, and the count excludes it.
			fv.Entries[len(fv.Entries)-1] = tabletext.Cell{Tab: decoded}
			d.report.Duplicate++
			last = decoded
			continue
		}
		fv.Entries = append(fv.Entries, tabletext.Cell{Tab: decoded})
		last = decoded
	}
	if keyWidened {
		d.report.Widened++
	}
	fv.Count = len(fv.Entries)
	return true
}

// unionCell reads a union: the ARM's reference, and then the payload a FIELD
// of the arm's type would carry. An arm this reader cannot name is §4's
// ordinary `unknown`, so the field reads None and one event counts, and the
// arm's payload is skipped by the ARM's own announced entry.
func (d *bitDecoder) unionCell(cell *tabletext.Cell, f *ir.Field) bool {
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
		if ir.TableWireId(v.Name) == entry.Id {
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
	fv := d.m.NewArm(arm)
	cell.Arm = fv
	af := arm.F
	switch {
	case af.Type.Pointer && af.Array == ir.ArrayNone:
		index, ok := d.index()
		if !ok {
			d.report.Malformed = true
			return false
		}
		d.resolveCell(&fv.Cell, af, index)
		return true
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
		return d.text(&fv.Cell, &fv.Count, af, entry)
	case af.Type.Kind == ir.TWString:
		return d.wideText(&fv.Cell, &fv.Count, af, entry)
	case af.Array != ir.ArrayNone:
		return d.array(fv, entry)
	case tabletext.UnionOf(af) != nil:
		return d.unionCell(&fv.Cell, af)
	case tabletext.EnumOf(af) != nil:
		return d.enumCell(&fv.Cell, af)
	case tabletext.StructOf(af) != nil:
		fv.Cell.Tab = d.m.New(tabletext.StructOf(af))
		return d.body(fv.Cell.Tab)
	}
	return d.scalar(&fv.Cell, af, entry.Kind, entry.Shape)
}

// enumCell reads an enum value: the reference naming its VARIANT's name, `0`
// for None. A reference this reader's enum cannot name is §4's ordinary
// `unknown`.
func (d *bitDecoder) enumCell(cell *tabletext.Cell, f *ir.Field) bool {
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
	v := enumValueForId(tabletext.EnumOf(f), entry.Id)
	if v < 0 {
		cell.U = 0
		d.report.Unknown++
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
	if bits&0x7F800000 == 0x7F800000 && bits&0x007FFFFF != 0 {
		// a NaN's PAYLOAD IS DATA the reference carries bit for bit, and a NaN
		// compares outside every range, so no clamp could fire on it
		cell.F = widenF32NaN(bits)
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
	if v < f.FMin {
		d.report.Clamped++
		return f.FMin
	}
	if v > f.FMax {
		d.report.Clamped++
		return f.FMax
	}
	return v
}

// resolveCell places one node index in a pointer slot against the numbering,
// under the file form's own rules: the resolution is the same walk, and only
// where the index came from moved.
func (d *bitDecoder) resolveCell(cell *tabletext.Cell, f *ir.Field, index uint64) {
	r := &wireReader{report: d.report, m: d.m, st: d.st}
	r.resolveCell(cell, f, index)
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
		_, ok := d.index()
		return ok
	case ir.TableKindString:
		n, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))
		if !ok || !d.r.align() {
			return false
		}
		return d.r.skip(int(n) * 8)
	case ir.TableKindWstring:
		// the length, NO align, then SIXTEEN bits a code unit (§3.3)
		n, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))
		if !ok {
			return false
		}
		return d.r.skip(int(n) * 16)
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
			if entry.Kind == ir.TableKindKeyed {
				if _, ok := d.reference(); !ok {
					return false
				}
			}
			if !d.skip(ir.TableVocabularyEntry{Kind: shape.Elem, Shape: inner}) {
				return false
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
