// THE MESSAGE FORM's READER (docs/SPEC-TABLES.md §3.3): the batch, the
// bitpacked body, and the tolerant path over both.
//
// A body carries NO KIND BYTE. What a reader needs to skip an id it cannot
// name is the announcement's per-entry RECORD, which says what a field header
// under that id spells and how wide it is, so an unknown field is skipped AT
// BIT GRANULARITY and the walk continues exactly where the writer meant it to.
// A KIND MISMATCH is the same event §4 already names, found by comparing the
// sender's announced record against the reader's own declaration rather than
// by reading a byte off the wire.
package tablewire

import (
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// MessageCount reads a batch's message count without decoding a body
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
		return 0, &MessageRefusal{Reason: ReasonNoVocabulary, BuildVersion: 0}
	}
	n, ok := newBitReader(data[1:]).leb()
	if !ok {
		return 0, nil
	}
	return int(n), nil
}

// DecodeMessages fills the caller's instances from a BATCH, resolving every
// reference against the announced table (docs/SPEC-TABLES.md §3.3).
//
// The instances are the caller's storage and their roots are the
// APPLICATION's: which root a message is, is not on this wire, exactly as it
// is not on a single message's. The error is a REFUSAL — a wire this reader
// will not decode at all — and is returned rather than folded into the report;
// false with a nil error is framing damage past the point the walk could
// continue, and each instance keeps what it decoded.
func DecodeMessages(m *tabletext.Model, insts []*tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (bool, error) {
	// THE FORM BYTE IS READ FIRST, before anything else, so a wire that is
	// both a form this reader does not carry and damaged is a refusal and
	// never damage (§3).
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
	count, ok := r.leb()
	if !ok {
		report.Malformed = true
		return false, nil
	}
	if count > uint64(len(insts)) {
		// the caller's storage does not hold the batch, which is the caller's
		// own bound and not a wire fact: nothing is decoded into memory that
		// was never sized for it
		report.Malformed = true
		return false, nil
	}
	for i := 0; i < int(count); i++ {
		d := &bitDecoder{m: m, v: v, report: report, r: r, refBits: v.RefBits()}
		if !d.root(insts[i]) {
			return false, nil
		}
	}
	return true, nil
}

// DecodeMessage is the BATCH OF ONE.
func DecodeMessage(m *tabletext.Model, inst *tabletext.Instance, data []byte, v *Vocabulary, report *tabletext.Report) (bool, error) {
	return DecodeMessages(m, []*tabletext.Instance{inst}, data, v, report)
}

// bitDecoder is one message's read: the announced table, the bit cursor the
// whole batch shares, and the numbering a pointered root resolves through.
type bitDecoder struct {
	m       *tabletext.Model
	v       *Vocabulary
	report  *tabletext.Report
	r       *bitReader
	refBits int
	st      *decodeState
}

// record is one slot's announced record, and ok is false for a reference that
// names no slot, which is §3's framing damage on the body that carries it.
func (d *bitDecoder) record(ref uint64) (uint64, ir.TableMessageDescriptor, bool) {
	if ref == 0 || ref > uint64(len(d.v.ids)) {
		return 0, ir.TableMessageDescriptor{}, false
	}
	return d.v.ids[ref-1], d.v.records[ref-1], true
}

// root decodes one message of a batch. A VARIABLE-class root reads its node
// table first — a reader has already read `head = 2` before it learns whether
// the table can be read at all — and a value-only root is one walk.
func (d *bitDecoder) root(inst *tabletext.Instance) bool {
	if !ir.VariableTables(d.m.Unit)[inst.Def.Name] {
		return d.body(inst, false)
	}
	start := d.r.off
	payload, present, framed := d.nodeTablePayload()
	records, scanned := []bitNodeRecord(nil), true
	if framed && present {
		records, scanned = d.scanRecords(payload)
	}
	st := &decodeState{root: inst}
	switch {
	case !framed && present, !scanned:
		d.report.Malformed = true
	default:
		st.good = true
	}
	d.st = st

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

	st.nodes = make([]Node, len(records))
	for i, rec := range records {
		if kind, known := blobKind[rec.TypeId]; known {
			st.nodes[i] = Node{Blob: &tabletext.Blob{Data: append([]byte(nil), rec.Body...)}, Kind: kind}
			continue
		}
		sd := byTypeId[rec.TypeId]
		if sd == nil {
			d.report.Unknown++ // skipped by its length, and it keeps its index
			continue
		}
		st.nodes[i] = Node{Inst: d.m.New(sd)}
	}
	for i, rec := range records {
		if st.nodes[i].Inst == nil {
			continue
		}
		sub := &bitDecoder{m: d.m, v: d.v, report: d.report, refBits: d.refBits, st: st,
			r: &bitReader{b: rec.Bits, n: rec.BitEnd, off: rec.BitStart}}
		sub.body(st.nodes[i].Inst, true)
	}
	d.r.off = start
	return d.body(inst, false)
}

// body decodes one table body: fields until the ZERO REFERENCE that ends it.
// `nested` decides one rule — THE RESERVED IDS INSIDE A NESTED BODY ARE
// MALFORMED (§3.1, §3.3), because a second numbering and a second announcement
// cannot exist.
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
		id, rec, named := d.record(ref)
		if !named {
			d.report.Malformed = true
			return false
		}
		if rec.Flags&ir.TableMessageAmbiguous != 0 {
			// ONE ID, TWO KINDS in the SENDER's unit: the record spells
			// neither, so the field cannot be read and cannot be stepped over.
			// The body stops, one `malformed` counts, and the fields decoded
			// before it stand (§3, §3.3).
			d.report.Malformed = true
			return false
		}
		if id == ir.TableBuildVersionWireId || id == ir.TableMessageRecordsWireId {
			// A RESERVED ID IN ANY BODY BUT THE ONE WHOSE TRANSPORT IT IS, IS
			// MALFORMED (§3.1, §3.3).
			d.report.Malformed = true
			return false
		}
		if id == ir.TableNodeWireId {
			if nested {
				d.report.Malformed = true
				return false
			}
			if !d.skip(rec) {
				d.report.Malformed = true
				return false
			}
			if d.st == nil || !ir.VariableTables(d.m.Unit)[inst.Def.Name] {
				// a reader that cannot name the node table counts one
				// unknown, which is the case §4's counter describes
				d.report.Unknown++
			}
			continue
		}
		i, known := index[id]
		if !known {
			d.report.Unknown++
			if !d.skip(rec) {
				d.report.Malformed = true
				return false
			}
			continue
		}
		fv := &inst.Fields[i]
		mine := ir.TableMessageFieldDescriptor(fv.Def)
		if rec.Kind != mine.Kind || rec.ElemKind != mine.ElemKind {
			// THE KIND MISMATCH IS FOUND IN THE ANNOUNCEMENT, not on the body.
			// A range that moved is NOT one: the widths differ and the record
			// carries the sender's, which is exactly what makes a message from
			// another build the ordinary case.
			d.report.KindMismatch++
			if !d.skip(rec) {
				d.report.Malformed = true
				return false
			}
			continue
		}
		if !d.field(fv, rec) {
			return false
		}
		if fv.Def.Type.Optional && fv.Def.Array == ir.ArrayNone {
			fv.Present = true
		}
	}
}

func (d *bitDecoder) field(fv *tabletext.Field, rec ir.TableMessageDescriptor) bool {
	f := fv.Def
	switch {
	case f.IsMap():
		return d.mapField(fv, rec)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index, ok := d.r.get(int(rec.ValueBits))
		if !ok {
			d.report.Malformed = true
			return false
		}
		d.resolveCell(&fv.Cell, f, index)
		return true
	case f.KeyEnum != "":
		return d.keyed(fv, rec)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		n, ok := d.length(rec)
		if !ok || !d.r.has(int(n)*8) {
			d.report.Malformed = true
			return false
		}
		raw, _ := d.r.bytes(int(n))
		keep := len(raw)
		if bound := int(f.Type.Size); keep > bound {
			keep = bound
			d.report.Clamped++
		}
		fv.Cell.Str = raw[:keep]
		fv.Count = keep
		return true
	case f.Array != ir.ArrayNone:
		return d.array(fv, rec)
	case tabletext.UnionOf(f) != nil:
		return d.unionCell(&fv.Cell, f)
	case tabletext.EnumOf(f) != nil:
		return d.enumCell(&fv.Cell, f)
	case tabletext.StructOf(f) != nil:
		fv.Cell.Tab = d.m.New(tabletext.StructOf(f))
		return d.body(fv.Cell.Tab, true)
	}
	return d.scalar(&fv.Cell, f, rec)
}

// length reads one length or count at the width the SENDER declared.
func (d *bitDecoder) length(rec ir.TableMessageDescriptor) (uint64, bool) {
	if rec.Flags&ir.TableMessageLebLength != 0 {
		return d.r.leb()
	}
	return d.r.get(int(rec.LengthBits))
}

// array decodes a positional array. A count above the reader's own bound keeps
// the first `N` elements and counts `clamped`, exactly as the file form does —
// the elements past it are READ and dropped, because the stream has to advance
// past them either way.
func (d *bitDecoder) array(fv *tabletext.Field, rec ir.TableMessageDescriptor) bool {
	f := fv.Def
	n, ok := d.length(rec)
	if !ok {
		d.report.Malformed = true
		return false
	}
	bound := uint64(f.ArrayBound)
	kept := n
	if kept > bound {
		kept = bound
		d.report.Clamped++
	}
	elemKind := int(rec.ElemKind)
	for i := uint64(0); i < n; i++ {
		var sink tabletext.Cell
		cell := &sink
		if i < kept {
			cell = &fv.Elems[i]
		}
		if !d.element(cell, f, elemKind, rec) {
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

// element reads one element at its kind's own bitpacked framing.
func (d *bitDecoder) element(cell *tabletext.Cell, f *ir.Field, kind int, rec ir.TableMessageDescriptor) bool {
	switch kind {
	case ir.TableKindTable:
		cell.Tab = d.m.New(tabletext.StructOf(f))
		return d.body(cell.Tab, true)
	case ir.TableKindPointer:
		index, ok := d.r.get(ir.PointerWireBits)
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
	return d.scalar(cell, f, rec)
}

// keyed decodes an enum-keyed array (docs/SPEC-TABLES.md §3.2): the number of
// present slots, then one `(key reference, element)` pair per slot. A key this
// reader's enum cannot name drops its element and counts one `unknown`, and
// the element is READ either way, because the stream advances past it.
func (d *bitDecoder) keyed(fv *tabletext.Field, rec ir.TableMessageDescriptor) bool {
	f := fv.Def
	n, ok := d.length(rec)
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
		id, _, named := d.record(ref)
		if !named {
			d.report.Malformed = true
			return false
		}
		value := enumValueForId(f.KeyEnumRef, id)
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
		if !d.element(cell, f, int(rec.ElemKind), rec) {
			return false
		}
	}
	return true
}

// mapField decodes one map (docs/SPEC-TABLES.md §2.8): the entry count, then
// the generated `{ key, value }` bodies.
func (d *bitDecoder) mapField(fv *tabletext.Field, rec ir.TableMessageDescriptor) bool {
	f := fv.Def
	n, ok := d.length(rec)
	if !ok {
		d.report.Malformed = true
		return false
	}
	fv.Entries = nil
	for i := uint64(0); i < n; i++ {
		entry := d.m.New(f.MapEntry)
		if !d.body(entry, true) {
			return false
		}
		if uint64(len(fv.Entries)) >= uint64(f.ArrayBound) && !f.IsList() {
			d.report.Clamped++
			continue
		}
		fv.Entries = append(fv.Entries, tabletext.Cell{Tab: entry})
	}
	fv.Count = len(fv.Entries)
	return true
}

// unionCell reads a union: the ARM's reference, and then the payload a FIELD
// of the arm's type would carry. An arm this reader cannot name is §4's
// ordinary `unknown` — the field reads None and one event counts — and the
// arm's payload is skipped by the ARM's own announced record.
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
	id, rec, named := d.record(ref)
	if !named {
		d.report.Malformed = true
		return false
	}
	tag := 0
	for i, v := range un.Variants {
		if ir.TableWireId(v.Name) == id {
			tag = i + 1
			break
		}
	}
	if tag == 0 {
		cell.U = 0
		d.report.Unknown++
		return d.skip(rec)
	}
	arm := un.Variants[tag-1]
	mine := ir.TableMessageFieldDescriptor(arm.F)
	switch {
	case arm.Void():
		cell.U = uint64(tag)
		return true
	case arm.Body():
		cell.U = uint64(tag)
		cell.Tab = d.m.New(arm.Ref)
		return d.body(cell.Tab, true)
	}
	if rec.Kind != mine.Kind || rec.ElemKind != mine.ElemKind {
		cell.U = 0
		d.report.KindMismatch++
		return d.skip(rec)
	}
	cell.U = uint64(tag)
	fv := d.m.NewArm(arm)
	cell.Arm = fv
	af := arm.F
	switch {
	case af.Type.Pointer && af.Array == ir.ArrayNone:
		index, ok := d.r.get(ir.PointerWireBits)
		if !ok {
			d.report.Malformed = true
			return false
		}
		d.resolveCell(&fv.Cell, af, index)
		return true
	case af.Type.Kind == ir.TString || af.Type.Kind == ir.TBytes:
		n, ok := d.length(rec)
		if !ok || !d.r.has(int(n)*8) {
			d.report.Malformed = true
			return false
		}
		raw, _ := d.r.bytes(int(n))
		keep := len(raw)
		if bound := int(af.Type.Size); keep > bound {
			keep = bound
			d.report.Clamped++
		}
		fv.Cell.Str = raw[:keep]
		fv.Count = keep
		return true
	case af.Array != ir.ArrayNone:
		return d.array(fv, rec)
	case tabletext.UnionOf(af) != nil:
		return d.unionCell(&fv.Cell, af)
	}
	return d.element(&fv.Cell, af, int(rec.Kind), rec)
}

// enumCell reads an enum value: the reference to the VARIANT NAME's id, `0`
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
	id, _, named := d.record(ref)
	if !named {
		d.report.Malformed = true
		return false
	}
	v := enumValueForId(tabletext.EnumOf(f), id)
	if v < 0 {
		cell.U = 0
		d.report.Unknown++
		return true
	}
	cell.U = uint64(v)
	return true
}

// scalar reads ONE value at the SENDER's declared width and range base, and
// then applies the READER's own bounds: a value outside them clamps and counts
// exactly as it does on a file (§4).
func (d *bitDecoder) scalar(cell *tabletext.Cell, f *ir.Field, rec ir.TableMessageDescriptor) bool {
	kind := ir.TableScalarKind(f)
	switch f.Type.Kind {
	case ir.TBool:
		v, ok := d.r.get(1)
		if !ok {
			d.report.Malformed = true
			return false
		}
		cell.B = v != 0
		return true
	case ir.TFloat32:
		raw, ok := d.r.get(32)
		if !ok {
			d.report.Malformed = true
			return false
		}
		bits := uint32(raw)
		if bits&0x7F800000 == 0x7F800000 && bits&0x007FFFFF != 0 {
			cell.F = widenF32NaN(bits)
			return true
		}
		v := float64(math.Float32frombits(bits))
		if f.HasFloatRange {
			if v < f.FMin {
				v, _ = f.FMin, 0
				d.report.Clamped++
			} else if v > f.FMax {
				v = f.FMax
				d.report.Clamped++
			}
		}
		cell.F = float64(float32(v))
		return true
	case ir.TFloat64:
		raw, ok := d.r.get(64)
		if !ok {
			d.report.Malformed = true
			return false
		}
		cell.F = math.Float64frombits(raw)
		return true
	}
	if ir.TableKindWide(kind) && f.Type.Width > 64 {
		raw, ok := d.r.bytes(16)
		if !ok {
			d.report.Malformed = true
			return false
		}
		value := tabletext.WideFromBytes(raw, kind)
		clamped := false
		cell.Wide, clamped = tabletext.WideClamp(value, f)
		if clamped {
			d.report.Clamped++
		}
		return true
	}
	raw, ok := d.r.get(int(rec.ValueBits))
	if !ok {
		d.report.Malformed = true
		return false
	}
	if ir.TableKindWide(kind) {
		value := new(big.Int).Add(new(big.Int).SetUint64(raw), big.NewInt(rec.Min))
		clamped := false
		cell.Wide, clamped = tabletext.WideClamp(value, f)
		if clamped {
			d.report.Clamped++
		}
		return true
	}
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	var value int64
	switch {
	case rec.Min != 0:
		value = int64(raw) + rec.Min
	case signed && int(rec.ValueBits) < 64 && rec.ValueBits > 0:
		shift := uint(64 - int(rec.ValueBits))
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

// resolveCell places one node index in a pointer slot against the numbering,
// under the file form's own rules — the resolution is the same walk, and only
// where the index came from moved.
func (d *bitDecoder) resolveCell(cell *tabletext.Cell, f *ir.Field, index uint64) {
	r := &wireReader{report: d.report, m: d.m, st: d.st}
	r.resolveCell(cell, f, index)
}

// skip advances past one field's payload without decoding it, using the
// announced record alone (docs/SPEC-TABLES.md §3.3). It is what makes an
// unknown id skippable on a body with no kind byte.
//
// An AMBIGUOUS entry — one the sender's closure gives more than one shape —
// cannot be skipped, and an entry that names no field shape at all cannot
// either: the body stops there, one `malformed` counts, and the fields decoded
// before it stand, which is §3's framing damage at the level it occurs.
func (d *bitDecoder) skip(rec ir.TableMessageDescriptor) bool {
	if rec.Flags&ir.TableMessageAmbiguous != 0 {
		return false
	}
	switch int(rec.Kind) {
	case 0:
		return false
	case ir.TableKindNoPayload:
		return true
	case ir.TableKindEnum:
		return d.r.skip(d.refBits)
	case ir.TableKindTable:
		return d.skipBody()
	case ir.TableKindUnion:
		ref, ok := d.r.get(d.refBits)
		if !ok {
			return false
		}
		if ref == 0 {
			return true
		}
		_, arm, named := d.record(ref)
		if !named {
			return false
		}
		return d.skip(arm)
	case ir.TableKindString:
		n, ok := d.length(rec)
		if !ok {
			return false
		}
		if rec.Flags&ir.TableMessageBitLength != 0 {
			return d.r.skip(int(n)) // the node table's payload is counted in BITS
		}
		return d.r.skip(int(n) * int(rec.ValueBits))
	case ir.TableKindArray, ir.TableKindKeyed:
		n, ok := d.length(rec)
		if !ok {
			return false
		}
		for i := uint64(0); i < n; i++ {
			if int(rec.Kind) == ir.TableKindKeyed && !d.r.skip(d.refBits) {
				return false
			}
			if !d.skipElement(rec) {
				return false
			}
		}
		return true
	}
	return d.r.skip(int(rec.ValueBits))
}

func (d *bitDecoder) skipElement(rec ir.TableMessageDescriptor) bool {
	switch int(rec.ElemKind) {
	case ir.TableKindTable:
		return d.skipBody()
	case ir.TableKindEnum:
		return d.r.skip(d.refBits)
	case ir.TableKindPointer:
		return d.r.skip(ir.PointerWireBits)
	case ir.TableKindUnion:
		return d.skip(ir.TableMessageDescriptor{Kind: ir.TableKindUnion})
	}
	return d.r.skip(int(rec.ValueBits))
}

// skipBody walks a nested body it cannot name, field by field, against the
// same announced table its parent resolves through.
func (d *bitDecoder) skipBody() bool {
	for {
		ref, ok := d.r.get(d.refBits)
		if !ok {
			return false
		}
		if ref == 0 {
			return true
		}
		_, rec, named := d.record(ref)
		if !named || !d.skip(rec) {
			return false
		}
	}
}

// nodeTablePayload finds the ROOT body's node-table field without decoding
// anything else: the fields are walked and skipped by their announced records,
// exactly as an unknown one is.
func (d *bitDecoder) nodeTablePayload() (payload *bitReader, present, framed bool) {
	start := d.r.off
	defer func() { d.r.off = start }()
	for {
		ref, ok := d.r.get(d.refBits)
		if !ok || ref == 0 {
			return payload, present, true
		}
		id, rec, named := d.record(ref)
		if !named {
			return payload, present, true
		}
		if id == ir.TableNodeWireId {
			present = true
			n, ok := d.r.leb()
			if !ok || !d.r.has(int(n)) {
				return nil, present, false
			}
			payload = &bitReader{b: d.r.b, n: d.r.off + int(n)}
			payload.off = d.r.off
			d.r.off += int(n)
			continue
		}
		if !d.skip(rec) {
			return payload, present, true
		}
	}
}

// bitNodeRecord is one record as the scan found it: its wire type id, a
// BLOB's bytes where it is one, and the BIT EXTENT of its body where it is a
// table's.
type bitNodeRecord struct {
	TypeId   uint64
	Body     []byte
	Bits     []byte
	BitStart int
	BitEnd   int
}

// scanRecords is the record scan at bit granularity (docs/SPEC-TABLES.md
// §3.1): `node_count` is data from the wire, the scan reads records until the
// payload is consumed and takes what it finds, and a count that disagrees with
// the scan is malformed.
func (d *bitDecoder) scanRecords(payload *bitReader) ([]bitNodeRecord, bool) {
	declared, ok := payload.leb()
	if !ok {
		return nil, false
	}
	var records []bitNodeRecord
	for payload.off < payload.n {
		ref, ok := payload.get(d.refBits)
		if !ok || ref == 0 || ref > uint64(len(d.v.ids)) {
			return nil, false
		}
		id := d.v.ids[ref-1]
		if id == ir.BytesWireTypeId || id == ir.StringWireTypeId {
			n, ok := payload.leb()
			if !ok || !payload.has(int(n)*8) {
				return nil, false
			}
			body, _ := payload.bytes(int(n))
			records = append(records, bitNodeRecord{TypeId: id, Body: body})
			continue
		}
		// A TABLE RECORD IS A SELF-DELIMITING BODY on this wire, so the scan
		// walks it rather than reading a length: the records the announcement
		// describes are the records this walk can step over.
		at := payload.off
		sub := &bitDecoder{m: d.m, v: d.v, report: &tabletext.Report{}, r: payload, refBits: d.refBits}
		if !sub.skipBody() {
			return nil, false
		}
		records = append(records, bitNodeRecord{TypeId: id, Bits: payload.b, BitStart: at, BitEnd: payload.off})
	}
	if payload.off != payload.n {
		return nil, false
	}
	if declared != uint64(len(records)) {
		return nil, false
	}
	return records, true
}
