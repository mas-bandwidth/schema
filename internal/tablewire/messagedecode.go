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
// reference against the announced vocabulary (docs/SPEC-TABLES.md §3.3).
//
// A BATCH IS OF ONE ROOT, and which root it is, is the APPLICATION's: a peer
// that mixes roots puts a discriminator in front of the bytes or wraps its
// message set in one root holding a union of them. The error is a REFUSAL — a
// wire this reader will not decode at all — and is returned rather than folded
// into the report; false with a nil error is damage, which is terminal for the
// batch.
func DecodeMessages(m *tabletext.Model, insts []*tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (bool, error) {
	// THE FORM BYTE IS READ FIRST, before the count and before any body, so a
	// wire that is both a form this reader does not carry and damaged is a
	// refusal and never damage (§3).
	if len(data) < 1 {
		report.Malformed = true
		return false, nil
	}
	if data[0] != ir.TableWireMessageForm {
		report.Refused = true
		return false, &MessageRefusal{Reason: ReasonNewerForm}
	}
	// A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
	// decoded, no counter moves and `malformed` does not fire.
	if v == nil || !v.announced {
		report.Refused = true
		return false, &MessageRefusal{Reason: ReasonNoVocabulary}
	}
	r := newBitReader(data[1:])
	raw, ok := r.get(8)
	if !ok {
		report.Malformed = true
		return false, nil
	}
	count := int(raw) + 1
	if count > len(insts) {
		// the caller's storage does not hold the batch, which is the caller's
		// own bound and not a wire fact: a count of 256 over a two-body buffer
		// is exhaustion and never an allocation
		report.Malformed = true
		return false, nil
	}
	for i := 0; i < count; i++ {
		d := &bitDecoder{m: m, v: v, report: report, r: r, refBits: v.RefBits()}
		if !d.root(insts[i]) {
			return false, nil
		}
	}
	// THE TRAILING PAD IS VERIFIED ZERO, which is the packet wire's rule for
	// the same reason (SPEC.md §4.3).
	if !r.align() {
		report.Malformed = true
		return false, nil
	}
	return true, nil
}

// DecodeMessage is the BATCH OF ONE.
func DecodeMessage(m *tabletext.Model, inst *tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (bool, error) {
	return DecodeMessages(m, []*tabletext.Instance{inst}, data, v, report)
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
}

// entry is one slot's announced entry, and ok is false for a reference that
// names no slot, which is damage on the batch that carries it.
func (d *bitDecoder) entry(ref uint64) (ir.TableVocabularyEntry, bool) {
	if ref == 0 || ref > uint64(len(d.v.entries)) {
		return ir.TableVocabularyEntry{}, false
	}
	return d.v.entries[ref-1], true
}

// root decodes one body of a batch. A VARIABLE-class root reads its NODE TABLE
// first, because it is the body's first field and a pointer index's width is
// settled by the node count it carries.
func (d *bitDecoder) root(inst *tabletext.Instance) bool {
	if !ir.VariableTables(d.m.Unit)[inst.Def.Name] {
		return d.body(inst, false)
	}
	st := &decodeState{root: inst}
	d.st = st
	if !d.nodeTable(inst, st) {
		return false
	}
	return d.body(inst, false)
}

// nodeTable reads the ROOT body's first field when it is the node table
// (§3.1): the numbering, whole, so an index resolves whichever way it points.
func (d *bitDecoder) nodeTable(inst *tabletext.Instance, st *decodeState) bool {
	at := d.r.off
	ref, ok := d.r.get(d.refBits)
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

	byTypeId := map[uint64]*ir.Struct{}
	for name := range ir.PointerReachable(d.m.Unit, inst.Def) {
		if sd := d.m.Lookup(name); sd != nil {
			byTypeId[ir.TableWireId(name)] = sd
		}
	}
	blobKind := map[uint64]ir.FieldTypeKind{}
	bytesEdge, stringEdge := ir.PointerReachableBlobs(d.m.Unit, inst.Def)
	if bytesEdge {
		blobKind[ir.BytesWireTypeId] = ir.TBytes
	}
	if stringEdge {
		blobKind[ir.StringWireTypeId] = ir.TString
	}

	// LOAD IS A SCAN, and a bit stream forces the same two passes a file's
	// lengths allowed (§3.1): PASS ONE walks the records to learn what each
	// node IS and where its bits are, so that an index resolves whichever way
	// it points, and PASS TWO decodes each body into its own storage.
	type record struct {
		typeId     uint64
		blob       []byte
		start, end int
	}
	records := make([]record, 0, count)
	for i := uint64(0); i < count; i++ {
		typeRef, ok := d.r.get(d.refBits)
		if !ok {
			d.report.Malformed = true
			return false
		}
		typeEntry, named := d.entry(typeRef)
		if !named {
			// a type id REFERENCE of 0 is damage: a record must say what it is
			d.report.Malformed = true
			return false
		}
		if _, isBlob := blobKind[typeEntry.Id]; isBlob {
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
		if kind, isBlob := blobKind[rec.typeId]; isBlob {
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
		sub := &bitDecoder{m: d.m, v: d.v, report: d.report, refBits: d.refBits, indexBits: d.indexBits, st: st,
			r: &bitReader{b: d.r.b, n: rec.end, off: rec.start}}
		if !sub.body(st.nodes[i].Inst, true) {
			return false
		}
	}
	st.good = true
	return true
}

// body decodes one table body: fields until the ZERO REFERENCE that ends it.
func (d *bitDecoder) body(inst *tabletext.Instance, nested bool) bool {
	index := map[uint64]int{}
	for i := range inst.Fields {
		index[ir.TableFieldWireId(inst.Fields[i].Def)] = i
	}
	for {
		ref, ok := d.r.get(d.refBits)
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
			_ = nested
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
		index, ok := d.r.get(d.indexBits)
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
	case f.Array != ir.ArrayNone:
		return d.array(fv, entry)
	case tabletext.UnionOf(f) != nil:
		return d.unionCell(&fv.Cell, f)
	case tabletext.EnumOf(f) != nil:
		return d.enumCell(&fv.Cell, f)
	case tabletext.StructOf(f) != nil:
		fv.Cell.Tab = d.m.New(tabletext.StructOf(f))
		return d.body(fv.Cell.Tab, true)
	}
	return d.scalar(&fv.Cell, f, entry.Kind, shape)
}

// text reads a `string(N)` or a `bytes(N)`: the length at its own width, the
// ALIGN that buys a memcpy, then the bytes. A payload longer than this
// reader's bound keeps what fits and counts `clamped`, which is not damage,
// because the length was read and the position after it is known.
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
	keep := len(raw)
	if bound := int(f.Type.Size); keep > bound {
		keep = bound
		d.report.Clamped++
	}
	cell.Str = raw[:keep]
	*count = keep
	return true
}

// array decodes a positional array. NO COUNT RIDES where the shape's `min`
// equals its `max`, which is every fixed array. A count above the reader's own
// bound keeps the first `N` elements and counts `clamped` — the elements past
// it are READ and dropped, because the stream advances past them either way.
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
	if kept > bound {
		kept = bound
		d.report.Clamped++
	}
	for i := uint64(0); i < n; i++ {
		var sink tabletext.Cell
		cell := &sink
		if i < kept {
			cell = &fv.Elems[i]
		}
		if !d.element(cell, f, shape) {
			return false
		}
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
		return d.body(cell.Tab, true)
	case ir.TableKindPointer:
		index, ok := d.r.get(d.indexBits)
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
	for i := uint64(0); i < n; i++ {
		ref, ok := d.r.get(d.refBits)
		if !ok {
			d.report.Malformed = true
			return false
		}
		key, named := d.entry(ref)
		if !named {
			// a reference of `0` where an entry is REQUIRED is damage (§3.2)
			d.report.Malformed = true
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

// mapField decodes one map (docs/SPEC-TABLES.md §2.8): the entry count, then
// the generated `{ key, value }` bodies.
func (d *bitDecoder) mapField(fv *tabletext.Field, entry ir.TableVocabularyEntry) bool {
	f := fv.Def
	n, ok := d.r.get(ir.TableMessageCountBits(entry.Shape))
	if !ok {
		d.report.Malformed = true
		return false
	}
	fv.Entries = nil
	for i := uint64(0); i < n; i++ {
		body := d.m.New(f.MapEntry)
		if !d.body(body, true) {
			return false
		}
		if uint64(len(fv.Entries)) >= uint64(f.ArrayBound) && !f.IsList() {
			d.report.Clamped++
			continue
		}
		fv.Entries = append(fv.Entries, tabletext.Cell{Tab: body})
	}
	fv.Count = len(fv.Entries)
	return true
}

// unionCell reads a union: the ARM's reference, and then the payload a FIELD
// of the arm's type would carry. An arm this reader cannot name is §4's
// ordinary `unknown` — the field reads None and one event counts — and the
// arm's payload is skipped by the ARM's own announced entry.
func (d *bitDecoder) unionCell(cell *tabletext.Cell, f *ir.Field) bool {
	un := tabletext.UnionOf(f)
	ref, ok := d.r.get(d.refBits)
	if !ok {
		d.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0
		return true
	}
	entry, named := d.entry(ref)
	if !named {
		d.report.Malformed = true
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
		cell.U = 0
		d.report.KindMismatch++
		return d.skip(entry)
	}
	cell.U = uint64(tag)
	switch {
	case arm.Void():
		return true
	case arm.Body():
		cell.Tab = d.m.New(arm.Ref)
		return d.body(cell.Tab, true)
	}
	fv := d.m.NewArm(arm)
	cell.Arm = fv
	af := arm.F
	switch {
	case af.Type.Pointer && af.Array == ir.ArrayNone:
		index, ok := d.r.get(d.indexBits)
		if !ok {
			d.report.Malformed = true
			return false
		}
		d.resolveCell(&fv.Cell, af, index)
		return true
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
		return d.text(&fv.Cell, &fv.Count, af, entry)
	case af.Array != ir.ArrayNone:
		return d.array(fv, entry)
	case tabletext.UnionOf(af) != nil:
		return d.unionCell(&fv.Cell, af)
	case tabletext.EnumOf(af) != nil:
		return d.enumCell(&fv.Cell, af)
	case tabletext.StructOf(af) != nil:
		fv.Cell.Tab = d.m.New(tabletext.StructOf(af))
		return d.body(fv.Cell.Tab, true)
	}
	return d.scalar(&fv.Cell, af, entry.Kind, entry.Shape)
}

// enumCell reads an enum value: the reference naming its VARIANT's name, `0`
// for None. A reference this reader's enum cannot name is §4's ordinary
// `unknown`.
func (d *bitDecoder) enumCell(cell *tabletext.Cell, f *ir.Field) bool {
	ref, ok := d.r.get(d.refBits)
	if !ok {
		d.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0
		return true
	}
	entry, named := d.entry(ref)
	if !named {
		d.report.Malformed = true
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
	case shape.Packing == ir.TableMessageRanged:
		value = int64(raw) + base.Int64()
	case signed && width < 64 && width > 0:
		shift := uint(64 - width)
		value = int64(raw<<shift) >> shift
	default:
		value = int64(raw)
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
			u := uint64(value)
			if u < uint64(lo) {
				u = uint64(lo)
				d.report.Clamped++
			} else if u > uint64(hi) {
				u = uint64(hi)
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
		v := float64(float32(float64(shape.QMin) + float64(index)*float64(shape.QStep)))
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
	cell.F = float64(float32(d.clampFloat(float64(math.Float32frombits(bits)), f)))
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
// under the file form's own rules — the resolution is the same walk, and only
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
		return d.r.skip(d.refBits)
	case ir.TableKindUnion:
		ref, ok := d.r.get(d.refBits)
		if !ok {
			return false
		}
		if ref == 0 {
			return true
		}
		arm, named := d.entry(ref)
		if !named {
			return false
		}
		return d.skip(arm)
	case ir.TableKindTable:
		return d.skipBody()
	case ir.TableKindPointer:
		return d.r.skip(d.indexBits)
	case ir.TableKindString:
		n, ok := d.r.get(ir.TableMessageBitsRequired(0, shape.Max))
		if !ok || !d.r.align() {
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
		for i := uint64(0); i < n; i++ {
			if entry.Kind == ir.TableKindKeyed && !d.r.skip(d.refBits) {
				return false
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
		ref, ok := d.r.get(d.refBits)
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
