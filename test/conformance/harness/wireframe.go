// The wire fuzzer's MUTATORS (docs/SPEC-TABLES.md §4.2): the framing of a pinned
// wire, located so that a mutation can aim at a number rather than at a byte,
// and the passes that turn one seed into its mutants.
//
// The scanner here is a LOCATOR, not a reader. It walks §3's framing to find
// where every reference, kind, length, count, index and id-table entry sits,
// and which lengths enclose each field. It decodes no value and mirrors no
// reader's decisions — the oracle is internal/tablewire, and this file never
// says what a mutant means.
//
// EVERY NUMBER ON THIS WIRE IS A VARIABLE-WIDTH LEB128 (§3), so a mutation is
// a SPLICE rather than a patch: the spot's bytes are replaced and every length
// framing it grows or shrinks by the same delta, innermost first, so the one
// thing wrong with a mutant is the thing the mutator aimed at.
package main

import (
	"encoding/binary"
	"slices"
	"sort"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type wireSpotKind int

const (
	spotRef        wireSpotKind = iota // a field's id reference
	spotKind                           // a field's kind byte, or an array's element kind
	spotLength                         // an L; limit is the end of the enclosing body
	spotCount                          // an N
	spotIndex                          // a node index under kind 17
	spotKey                            // an enum-keyed triple's key reference
	spotArm                            // a union's arm id reference
	spotVariant                        // an enum's variant id reference, under kind 30
	spotNodeCount                      // the node table's node_count
	spotRecordType                     // a node record's type id reference
	spotEntry                          // one id-table entry: a fixed little-endian u64
	spotEntryCount                     // the id table's entry count: the one fixed-width number
	spotForm                           // the FORM BYTE, and it is the whole header
)

type wireSpot struct {
	kind  wireSpotKind
	off   int
	width int
	limit int // the end of the body the number is read inside
	value uint64
	// leb marks a number written as a canonical LEB128, which is every one of
	// them but the id table's entry count and the form byte (§3)
	leb bool
	// arm marks a length that frames a UNION ARM payload. An arm header
	// carries the arm's KIND now (§3), so the L is checked against that kind's
	// width — and a mutant that moves it is that arm's own framing damage.
	arm bool
	// body marks a length that frames a TABLE BODY — a kind 13 field, or an
	// arm whose payload is a body. A body's terminator is the end of its
	// payload (§3), and the mutator writes one earlier.
	body bool
	// enclosing is every length spot framing this one, outermost first, so a
	// splice can keep the framing consistent
	enclosing []int
}

// remaining is how many bytes a length spot may claim without leaving its
// body: the number "past the body" is one more than this.
func (s wireSpot) remaining() int { return s.limit - (s.off + s.width) }

type wireField struct {
	start, end int   // the reference, the kind and the payload
	enclosing  []int // every length spot framing this field, outermost first
}

type wireFrame struct {
	spots   []wireSpot
	fields  []wireField
	records int // node records the root body carries

	// the three parts of a saved table (§3): the form byte, the root body, and
	// the id table located from the END
	bodyStart int
	bodyEnd   int
	entries   []uint64
	entriesAt int
	countAt   int
}

// entryOf is the id one reference names, and false when the reference names no
// entry — `0`, which names no id, or one past the table.
func (f *wireFrame) entryOf(ref uint64) (uint64, bool) {
	if ref == 0 || ref > uint64(len(f.entries)) {
		return 0, false
	}
	return f.entries[ref-1], true
}

type frameScanner struct {
	data  []byte
	f     *wireFrame
	chain []int
}

// leb reads one canonical LEB128 at off, and answers its width. A width of 0
// means the number runs off the end, and the scanner stops there — it is only
// ever pointed at a pinned, valid wire.
func readLeb(data []byte, off, end int) (value uint64, width int) {
	shift := uint(0)
	for i := 0; off+i < end && i < 10; i++ {
		b := data[off+i]
		value |= uint64(b&0x7F) << shift
		if b&0x80 == 0 {
			return value, i + 1
		}
		shift += 7
	}
	return 0, 0
}

// appendLeb is one value in its one legal spelling.
func appendLeb(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, uint8(v)|0x80)
		v >>= 7
	}
	return append(out, uint8(v))
}

func lebBytes(v uint64) []byte { return appendLeb(nil, v) }

// frameWire locates every number in one saved table: the form byte, the id
// table it ends with, and every spot of the root body between them.
func frameWire(data []byte) *wireFrame { return frameWireForm(data, nil) }

// frameWireForm is frameWire over EITHER form (docs/SPEC-TABLES.md §3, §3.3).
// `entries` nil is the FILE form, whose id table is its own trailer; entries
// non-nil is the MESSAGE FORM, whose table is the CONNECTION's — so the wire
// is the form byte and the root body, there is no trailer to locate and none
// to mutate, and the whole of its attack surface is the body.
func frameWireForm(data []byte, entries []uint64) *wireFrame {
	if entries != nil {
		f := &wireFrame{}
		if len(data) < 2 {
			return f
		}
		f.spots = append(f.spots, wireSpot{kind: spotForm, off: 0, width: 1, limit: len(data), value: uint64(data[0])})
		f.entries = entries
		f.entriesAt = len(data)
		f.countAt = len(data)
		f.bodyStart = 1
		f.bodyEnd = len(data)
		s := &frameScanner{data: data, f: f}
		s.body(f.bodyStart, f.bodyEnd, true)
		return f
	}
	f := &wireFrame{}
	if len(data) < 9 {
		return f
	}
	f.spots = append(f.spots, wireSpot{kind: spotForm, off: 0, width: 1, limit: len(data), value: uint64(data[0])})
	count := binary.LittleEndian.Uint64(data[len(data)-8:])
	span := int(count)*8 + 8
	if count > uint64(len(data)/8) || span+1 > len(data) {
		return f
	}
	f.entriesAt = len(data) - span
	f.countAt = len(data) - 8
	f.entries = make([]uint64, count)
	for i := range f.entries {
		f.entries[i] = binary.LittleEndian.Uint64(data[f.entriesAt+i*8:])
	}
	f.bodyStart = 1
	f.bodyEnd = f.entriesAt
	s := &frameScanner{data: data, f: f}
	s.body(f.bodyStart, f.bodyEnd, true)
	for i := range f.entries {
		f.spots = append(f.spots, wireSpot{kind: spotEntry, off: f.entriesAt + i*8, width: 8, limit: len(data), value: f.entries[i]})
	}
	f.spots = append(f.spots, wireSpot{kind: spotEntryCount, off: f.countAt, width: 8, limit: len(data), value: count})
	return f
}

// spotLeb records one canonical LEB128 number and answers the offset after it,
// or -1 where it runs off the end.
func (s *frameScanner) spotLeb(kind wireSpotKind, off, end int) int {
	v, w := readLeb(s.data, off, end)
	if w == 0 {
		return -1
	}
	s.f.spots = append(s.f.spots, wireSpot{
		kind: kind, off: off, width: w, limit: end, value: v, leb: true,
		enclosing: append([]int(nil), s.chain...),
	})
	return off + w
}

func (s *frameScanner) spotByte(kind wireSpotKind, off, end int) {
	s.f.spots = append(s.f.spots, wireSpot{
		kind: kind, off: off, width: 1, limit: end, value: uint64(s.data[off]),
		enclosing: append([]int(nil), s.chain...),
	})
}

func (s *frameScanner) lastSpot() int { return len(s.f.spots) - 1 }

// framedAs opens one L-framed payload: the length spot is recorded and pushed
// onto the enclosing chain, `inner` walks the body, and the chain is popped.
// It answers the offset after the body, or -1 when the length runs past `end`.
func (s *frameScanner) framedAs(off, end int, arm, body bool, inner func(start, stop int)) int {
	after := s.spotLeb(spotLength, off, end)
	if after < 0 {
		return -1
	}
	ls := s.lastSpot()
	s.f.spots[ls].arm = arm
	s.f.spots[ls].body = body
	n := int(s.f.spots[ls].value)
	if n < 0 || after+n > end {
		return -1
	}
	s.chain = append(s.chain, ls)
	if inner != nil {
		inner(after, after+n)
	}
	s.chain = s.chain[:len(s.chain)-1]
	return after + n
}

func (s *frameScanner) framed(off, end int, inner func(start, stop int)) int {
	return s.framedAs(off, end, false, false, inner)
}

func (s *frameScanner) body(start, end int, root bool) {
	off := start
	for off < end {
		fieldStart := off
		after := s.spotLeb(spotRef, off, end)
		if after < 0 {
			return
		}
		ref := s.f.spots[s.lastSpot()].value
		off = after
		if ref == 0 {
			return // the body ENDS AT ITS OWN ZERO REFERENCE
		}
		id, named := s.f.entryOf(ref)
		if off+1 > end {
			return
		}
		kind := s.data[off]
		s.spotByte(spotKind, off, end)
		off++
		off = s.payload(off, end, kind, root && named && id == ir.TableNodeWireId)
		if off < 0 {
			return
		}
		s.f.fields = append(s.f.fields, wireField{start: fieldStart, end: off, enclosing: append([]int(nil), s.chain...)})
	}
}

// payload walks one field's payload by its kind, and answers the offset after
// it. `nodeTable` marks the one field the numbering rides in (§3.1).
func (s *frameScanner) payload(off, end int, kind uint8, nodeTable bool) int {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8, ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32, ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128:
		w := kindWidth(int(kind))
		if off+w > end {
			return -1
		}
		return off + w
	case ir.TableKindPointer:
		return s.spotLeb(spotIndex, off, end)
	case ir.TableKindEnum:
		return s.spotLeb(spotVariant, off, end)
	case ir.TableKindString:
		var inner func(int, int)
		if nodeTable {
			inner = s.nodeTable
		}
		return s.framed(off, end, inner)
	case ir.TableKindTable:
		return s.framedAs(off, end, false, true, func(a, b int) { s.body(a, b, false) })
	case ir.TableKindArray:
		return s.framed(off, end, s.array)
	case ir.TableKindKeyed:
		return s.framed(off, end, s.keyed)
	case ir.TableKindEscape, ir.TableKindNoPayload:
		return s.framed(off, end, nil)
	case ir.TableKindUnion:
		return s.arm(off, end)
	}
	return -1
}

// arm walks a union payload in its place (§3): the arm id reference, and when
// it is not 0 the arm's KIND byte, its L and its payload.
func (s *frameScanner) arm(off, end int) int {
	after := s.spotLeb(spotArm, off, end)
	if after < 0 {
		return -1
	}
	ref := s.f.spots[s.lastSpot()].value
	off = after
	if ref == 0 {
		return off // the zero reference is the empty union, and carries nothing
	}
	if off+1 > end {
		return -1
	}
	kind := s.data[off]
	s.spotByte(spotKind, off, end)
	off++
	// AN ARM HEADER IS A FIELD HEADER (§3), so the payload under its L is what
	// a FIELD of the arm's type puts after its own prefix. Two of them are
	// walkable: a body, and a NESTED UNION, whose payload is the union in its
	// place and carries an arm reference of its own.
	return s.framedAs(off, end, true, kind == ir.TableKindTable, func(a, b int) { s.framedPayload(int(kind), a, b) })
}

// framedPayload walks what sits under an L once the kind is known: a body, a
// nested union in its place, an array or keyed body, or one of the two
// REFERENCE-shaped payloads. Everything else frames no number the mutators can
// aim at, and the scanner is a LOCATOR — a spot it cannot frame is a spot it
// does not record.
func (s *frameScanner) framedPayload(kind, a, b int) {
	switch kind {
	case ir.TableKindTable:
		s.body(a, b, false)
	case ir.TableKindUnion:
		s.arm(a, b)
	case ir.TableKindArray:
		s.array(a, b)
	case ir.TableKindKeyed:
		s.keyed(a, b)
	case ir.TableKindEnum:
		s.spotLeb(spotVariant, a, b)
	case ir.TableKindPointer:
		s.spotLeb(spotIndex, a, b)
	}
}

// nodeTable walks the node table's payload (§3.1): node_count, then whole
// records — a type id reference, a length and a body.
func (s *frameScanner) nodeTable(off, end int) {
	off = s.spotLeb(spotNodeCount, off, end)
	if off < 0 {
		return
	}
	for off < end {
		before := len(s.f.spots)
		off = s.spotLeb(spotRecordType, off, end)
		if off < 0 {
			return
		}
		// A BLOB RECORD'S BODY IS THE BYTES THEMSELVES (§2.5, §3.1) — no
		// fields, no terminator and no framing inside — so the scan stops at
		// its length rather than walking bytes that frame nothing
		id, named := s.f.entryOf(s.f.spots[before].value)
		blob := named && (id == ir.BytesWireTypeId || id == ir.StringWireTypeId)
		s.f.records++
		if blob {
			off = s.framed(off, end, nil)
		} else {
			off = s.framed(off, end, func(a, b int) { s.body(a, b, false) })
		}
		if off < 0 {
			return
		}
	}
}

// array walks an array body: the element kind byte, N, then the elements at
// the element kind's own framing.
func (s *frameScanner) array(off, end int) {
	if off+1 > end {
		return
	}
	ek := int(s.data[off])
	s.spotByte(spotKind, off, end)
	off++
	after := s.spotLeb(spotCount, off, end)
	if after < 0 {
		return
	}
	count := int(s.f.spots[s.lastSpot()].value)
	off = after
	for i := 0; i < count && off < end; i++ {
		switch ek {
		case ir.TableKindPointer:
			off = s.spotLeb(spotIndex, off, end)
		case ir.TableKindEnum:
			off = s.spotLeb(spotVariant, off, end)
		case ir.TableKindTable:
			off = s.framedAs(off, end, false, true, func(a, b int) { s.body(a, b, false) })
		case ir.TableKindUnion:
			// AN ELEMENT OF AN ARRAY OF UNIONS IS AN ARM HEADER (§3), framed
			// exactly as a union field's payload is
			off = s.arm(off, end)
		case ir.TableKindString:
			off = s.framed(off, end, nil)
		default:
			w := kindWidth(ek)
			if w == 0 || off+w > end {
				return
			}
			off += w
		}
		if off < 0 {
			return
		}
	}
}

// keyed walks an enum-keyed body (§3.2): the element kind byte, N, then N
// triples of a key reference, L and the element.
func (s *frameScanner) keyed(off, end int) {
	if off+1 > end {
		return
	}
	ek := int(s.data[off])
	s.spotByte(spotKind, off, end)
	off++
	after := s.spotLeb(spotCount, off, end)
	if after < 0 {
		return
	}
	count := int(s.f.spots[s.lastSpot()].value)
	off = after
	for i := 0; i < count && off < end; i++ {
		off = s.spotLeb(spotKey, off, end)
		if off < 0 {
			return
		}
		off = s.framed(off, end, func(a, b int) { s.framedPayload(ek, a, b) })
		if off < 0 {
			return
		}
	}
}

// kindWidth is a fixed-width kind's payload width, and 0 for every other kind.
func kindWidth(kind int) int {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8, ir.TableKindFixed8, ir.TableKindUFixed8:
		return 1
	case ir.TableKindI16, ir.TableKindU16, ir.TableKindFixed16, ir.TableKindUFixed16:
		return 2
	case ir.TableKindI32, ir.TableKindU32, ir.TableKindF32, ir.TableKindPointer, ir.TableKindFixed32, ir.TableKindUFixed32:
		return 4
	case ir.TableKindI64, ir.TableKindU64, ir.TableKindF64, ir.TableKindFixed64, ir.TableKindUFixed64:
		return 8
	case ir.TableKindI128, ir.TableKindU128, ir.TableKindFixed128, ir.TableKindUFixed128:
		return 16
	}
	return 0
}

// wireKindLast is the highest kind byte the wire defines; a swap to one past
// it is the one outside the closed set on the high side.
const wireKindLast = ir.TableKindUFixed128

// ---------------------------------------------------------------------------
// the passes
// ---------------------------------------------------------------------------

// wireSeed is one pinned wire the mutators start from: a manifest instance or
// report case, framed once.
type wireSeed struct {
	name  string
	unit  string
	root  string
	wire  []byte
	frame *wireFrame
	// a PINNED VECTOR rides exactly as it is and is not mutated. It is a red
	// the fuzzer already found, kept so the next run seeks it rather than
	// searching for it (testdata/wire/tables/fuzz-vectors/INDEX.txt).
	vector bool
	// the MESSAGE FORM (docs/SPEC-TABLES.md §3.3): this seed's mutants are
	// form-2 wires read against the connection's table, which is what the
	// replay command has to say to reproduce one.
	message bool
}

// wireMutant is one input the leg and the oracle both read.
type wireMutant struct {
	seed  *wireSeed
	pass  string
	index int // the mutant's number within its pass
	data  []byte
}

func put(data []byte, off, width int, value uint64) {
	switch width {
	case 1:
		data[off] = uint8(value)
	case 2:
		binary.LittleEndian.PutUint16(data[off:], uint16(value))
	case 4:
		binary.LittleEndian.PutUint32(data[off:], uint32(value))
	case 8:
		binary.LittleEndian.PutUint64(data[off:], value)
	}
}

// spliceSpot replaces one spot's bytes and grows or shrinks every length
// framing it by the same delta, INNERMOST FIRST — so the framing stays
// consistent and the one thing wrong with the mutant is the thing the mutator
// aimed at (§4.2). A canonical LEB128 cannot be patched in place, which is why
// every mutation of a number on this wire is a splice.
func spliceSpot(seed *wireSeed, si int, newBytes []byte) []byte {
	f := seed.frame
	sp := f.spots[si]
	type edit struct {
		off, width int
		bytes      []byte
	}
	edits := []edit{{sp.off, sp.width, newBytes}}
	d := len(newBytes) - sp.width
	for _, encl := range slices.Backward(sp.enclosing) {
		e := f.spots[encl]
		grown := max(int64(e.value)+int64(d), 0)
		nb := lebBytes(uint64(grown))
		d += len(nb) - e.width
		edits = append(edits, edit{e.off, e.width, nb})
	}
	sort.Slice(edits, func(a, b int) bool { return edits[a].off < edits[b].off })
	out := make([]byte, 0, len(seed.wire)+d)
	at := 0
	for _, e := range edits {
		out = append(out, seed.wire[at:e.off]...)
		out = append(out, e.bytes...)
		at = e.off + e.width
	}
	return append(out, seed.wire[at:]...)
}

// patched writes one value at one spot: a splice for the LEB128 numbers, and
// an in-place put for the three fixed-width things on the wire — the form
// byte, an id-table entry and the entry count.
func patched(seed *wireSeed, si int, value uint64) []byte {
	sp := seed.frame.spots[si]
	if !sp.leb {
		out := append([]byte(nil), seed.wire...)
		put(out, sp.off, sp.width, value)
		return out
	}
	return spliceSpot(seed, si, lebBytes(value))
}

// nonCanonical is one value in a spelling that is NOT its own: the minimal
// bytes with `extra` redundant continuation bytes after them, which §3 calls
// malformed because one value has one spelling.
func nonCanonical(v uint64, extra int) []byte {
	out := lebBytes(v)
	out[len(out)-1] |= 0x80
	for i := 1; i < extra; i++ {
		out = append(out, 0x80)
	}
	return append(out, 0)
}

// duplicateField copies one field's bytes in right after itself. With `fix`
// every length framing the field grows by the copy, so the framing stays
// valid and only the repeated reference is the event; without it the enclosing
// lengths are one field short, which is framing damage.
func duplicateField(seed *wireSeed, fi int, fix bool) []byte {
	fld := seed.frame.fields[fi]
	n := fld.end - fld.start
	out := make([]byte, 0, len(seed.wire)+n+8)
	if !fix {
		out = append(out, seed.wire[:fld.end]...)
		out = append(out, seed.wire[fld.start:fld.end]...)
		return append(out, seed.wire[fld.end:]...)
	}
	// the enclosing lengths grow by the copy, innermost first, exactly as a
	// splice does
	type edit struct {
		off, width int
		bytes      []byte
	}
	var edits []edit
	d := n
	for _, encl := range slices.Backward(fld.enclosing) {
		e := seed.frame.spots[encl]
		nb := lebBytes(e.value + uint64(d))
		d += len(nb) - e.width
		edits = append(edits, edit{e.off, e.width, nb})
	}
	sort.Slice(edits, func(a, b int) bool { return edits[a].off < edits[b].off })
	at := 0
	for _, e := range edits {
		out = append(out, seed.wire[at:e.off]...)
		out = append(out, e.bytes...)
		at = e.off + e.width
	}
	out = append(out, seed.wire[at:fld.end]...)
	out = append(out, seed.wire[fld.start:fld.end]...)
	return append(out, seed.wire[fld.end:]...)
}

// duplicateFieldDamaged copies one field in after itself with the framing
// grown to fit, then makes a length INSIDE the copy impossible. The repeat is
// entered and then fails partway, which is where the last occurrence's claim
// is sharpest (§3): the reader must land on what the damaged repeat decodes,
// never on what the first occurrence left standing. It answers false for a
// field whose body carries no length of its own.
func duplicateFieldDamaged(seed *wireSeed, fi int) ([]byte, bool) {
	fld := seed.frame.fields[fi]
	inside := 0
	for _, sp := range seed.frame.spots {
		if sp.kind != spotLength || sp.off < fld.start || sp.off >= fld.end {
			continue
		}
		inside++
		if inside < 2 {
			continue
		}
		out := duplicateField(seed, fi, true)
		// the copy sits at the very end of the grown field, so the length's
		// place inside it is found from the tail rather than from an offset
		// the growth moved
		copyStart := len(out) - (len(seed.wire) - fld.end) - (fld.end - fld.start)
		at := copyStart + (sp.off - fld.start)
		if at < 0 || at+sp.width > len(out) {
			return nil, false
		}
		// THE LENGTH IS MADE IMPOSSIBLE AT ITS OWN WIDTH: the largest value
		// that spells in the bytes it already occupies, so the ONE thing wrong
		// with the mutant is the length and not a second shift of the framing
		for k := 0; k < sp.width-1; k++ {
			out[at+k] = 0xFF
		}
		out[at+sp.width-1] = 0x7F
		return out, true
	}
	return nil, false
}

// earlyTerminator writes the ZERO REFERENCE ahead of a body's last byte, so
// the body ends early and the bytes after it are claimed by no field (§3).
func earlyTerminator(seed *wireSeed, sp wireSpot) ([]byte, bool) {
	start := sp.off + sp.width
	end := start + int(sp.value)
	if int(sp.value) < 3 || end > len(seed.wire) {
		return nil, false
	}
	at := end - 2 // one byte ahead of the payload's last
	if seed.wire[at] == 0 {
		return nil, false
	}
	out := append([]byte(nil), seed.wire...)
	out[at] = 0
	return out, true
}

// enumerated yields every deterministic mutant of one seed, in a fixed order,
// whatever N is: these are the mutants that aim at the checks by name, and
// they cost nothing to run.
func enumerated(seed *wireSeed, emit func(pass string, data []byte)) {
	f := seed.frame
	wire := seed.wire

	// every truncation length the framing makes distinct: the reader must
	// stop at each one
	for _, n := range truncations(seed) {
		emit("truncate", append([]byte(nil), wire[:n]...))
	}

	records := uint64(f.records)
	entries := uint64(len(f.entries))

	// THE FOUR VALUES A 64-BIT LEB CAN SPELL THAT A LENGTH NEVER LEGALLY IS
	// (§4.2), which is what a variable-width number opened up
	extremes := []uint64{0x7FFFFFFF, 0x80000000, 0xFFFFFFFF, 0xFFFFFFFFFFFFFFFF}

	for si := range f.spots {
		sp := f.spots[si]
		switch sp.kind {
		case spotForm:
			// THE FORM BYTE set to 0, to 2 and to 0xFF, which must be a named
			// refusal and never damage (§3)
			for _, v := range []uint64{0, 2, 0xFF} {
				emit("form", patched(seed, si, v))
			}
		case spotKind:
			// every kind byte swapped to every other value, the two outside
			// the closed set included: 0 and one past the last are not
			// skippable, and 31 and 32 are the escape and the payload-free kind
			for k := uint64(0); k <= uint64(wireKindLast)+1; k++ {
				if k != sp.value {
					emit("kind", patched(seed, si, k))
				}
			}
		case spotLength:
			rem := uint64(sp.remaining())
			values := append([]uint64{0, 1, sp.value - 1, sp.value + 1, rem, rem + 1, rem + 2}, extremes...)
			for _, v := range values {
				if v != sp.value {
					emit("length", patched(seed, si, v))
				}
			}
			if sp.arm {
				// AN ARM'S L MOVED OFF ITS KIND'S WIDTH (§3, §4.2): every
				// fixed width the closed set has, and zero
				for _, v := range []uint64{0, 1, 2, 4, 8, 16} {
					if v != sp.value {
						emit("arm-length", patched(seed, si, v))
					}
				}
			}
			if sp.body {
				if data, ok := earlyTerminator(seed, sp); ok {
					emit("terminator", data)
				}
			}
		case spotCount:
			rem := uint64(sp.remaining())
			values := append([]uint64{0, sp.value + 1, sp.value*2 + 1, rem, rem + 1}, extremes...)
			for _, v := range values {
				if v != sp.value {
					emit("count", patched(seed, si, v))
				}
			}
		case spotIndex:
			// null, the root, the first record, the last, the two past it,
			// and the extremes the encoding can spell
			values := append([]uint64{0, 1, 2, records, records + 1, records + 2, records + 3}, extremes...)
			for _, v := range values {
				if v != sp.value {
					emit("index", patched(seed, si, v))
				}
			}
		case spotRef, spotArm, spotKey, spotVariant, spotRecordType:
			// THE REFERENCE CLASS (§3, §4.2), which is this form's own attack
			// surface: `0`, the entry count — the LAST LEGAL SLOT, which must
			// RESOLVE — the count plus one, and the extremes
			pass := "reference"
			switch sp.kind {
			case spotKey:
				pass = "key"
			case spotArm:
				pass = "arm"
			case spotRecordType:
				pass = "record-type"
			}
			values := append([]uint64{0, entries, entries + 1}, extremes...)
			for _, v := range values {
				if v != sp.value {
					emit(pass, patched(seed, si, v))
				}
			}
		case spotNodeCount:
			values := append([]uint64{0, sp.value + 1, sp.value - 1}, extremes...)
			for _, v := range values {
				if v != sp.value {
					emit("node-count", patched(seed, si, v))
				}
			}
		case spotEntry:
			// AN ENTRY'S OWN EIGHT BYTES FLIPPED, which must read as an
			// ordinary `unknown` and never as damage (§4.2) — and the RESERVED
			// node-table id planted in one, which a nested body cannot claim
			for _, v := range []uint64{sp.value ^ 1, 0, ir.TableNodeWireId} {
				if v != sp.value {
					emit("entry", patched(seed, si, v))
				}
			}
			// THE SAME ID IN TWO ENTRIES is malformed for the whole wire (§3)
			if len(f.entries) > 1 {
				other := f.entries[0]
				if other == sp.value {
					other = f.entries[len(f.entries)-1]
				}
				if other != sp.value {
					emit("entry-repeat", patched(seed, si, other))
				}
			}
		case spotEntryCount:
			// THE TABLE TRAILER (§3, §4.2): the entry count off by one each
			// way, at both extremes, and set so the entries overrun the front
			values := []uint64{sp.value + 1, sp.value - 1, 0, uint64(len(wire)), 0xFFFFFFFFFFFFFFFF}
			for _, v := range values {
				if v != sp.value {
					emit("entry-count", patched(seed, si, v))
				}
			}
		}
		// EVERY NUMBER IN ITS NON-MINIMAL SPELLINGS (§3, §4.2): one redundant
		// continuation byte, then nine, and an eleven-byte form. The framing
		// around it grows to fit, so the spelling is the whole of the event.
		if sp.leb {
			for _, extra := range []int{1, 9, 11} {
				emit("canonical", spliceSpot(seed, si, nonCanonical(sp.value, extra)))
			}
		}
	}

	for fi := range f.fields {
		emit("duplicate", duplicateField(seed, fi, true))
		emit("duplicate-unframed", duplicateField(seed, fi, false))
		if data, ok := duplicateFieldDamaged(seed, fi); ok {
			emit("duplicate-damaged", data)
		}
	}
}

// ---------------------------------------------------------------------------
// the random pass
// ---------------------------------------------------------------------------

// truncations is where a seed is cut. A wire up to truncateEvery bytes is cut
// at every length; a longer one — the wide instances are hundreds of
// kilobytes of payload — is cut around every number the framing carries and
// at every field boundary, which is every length at which a reader can meet
// a different decision, since a cut inside a payload is the same event at
// every byte of it.
func truncations(seed *wireSeed) []int {
	n := len(seed.wire)
	if n <= truncateEvery {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	set := map[int]bool{0: true, n - 1: true}
	for _, sp := range seed.frame.spots {
		for _, at := range []int{sp.off - 1, sp.off, sp.off + 1, sp.off + sp.width - 1, sp.off + sp.width, sp.off + sp.width + 1} {
			if at >= 0 && at < n {
				set[at] = true
			}
		}
	}
	for _, fld := range seed.frame.fields {
		for _, at := range []int{fld.start, fld.end - 1, fld.end} {
			if at >= 0 && at < n {
				set[at] = true
			}
		}
	}
	out := make([]int, 0, len(set))
	for at := range set {
		out = append(out, at)
	}
	sort.Ints(out)
	return out
}

// truncateEvery is the wire size up to which every length is a truncation.
const truncateEvery = 1024

// splitmix64 is the generator, written out so that a mutant is the same bytes
// under every Go release: every mutant is a pure function of (seed, index).
type splitmix64 struct{ state uint64 }

func (r *splitmix64) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func (r *splitmix64) below(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// randomMutant is mutant `index` of the random pass: a seed chosen by the
// generator, then one to three strategies stacked on it.
func randomMutant(seeds []*wireSeed, seed uint64, index int) *wireMutant {
	r := &splitmix64{state: seed ^ (uint64(index) * 0xD1B54A32D192ED03)}
	s := seeds[r.below(len(seeds))]
	data := append([]byte(nil), s.wire...)
	steps := 1 + r.below(3)
	for range steps {
		data = randomStep(r, s, seeds, data)
	}
	return &wireMutant{seed: s, pass: "random", index: index, data: data}
}

func randomStep(r *splitmix64, s *wireSeed, seeds []*wireSeed, data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	switch r.below(9) {
	case 0: // bit flips
		for range 1 + r.below(8) {
			i := r.below(len(data))
			data[i] ^= 1 << r.below(8)
		}
	case 1: // byte overwrites
		for range 1 + r.below(4) {
			data[r.below(len(data))] = uint8(r.next())
		}
	case 2: // truncation
		data = data[:r.below(len(data))]
	case 3: // an inserted run
		at := r.below(len(data) + 1)
		run := make([]byte, 1+r.below(8))
		for i := range run {
			run[i] = uint8(r.next())
		}
		data = append(data[:at], append(run, data[at:]...)...)
	case 4: // a deleted run
		at := r.below(len(data))
		n := 1 + r.below(8)
		if at+n > len(data) {
			n = len(data) - at
		}
		data = append(data[:at], data[at+n:]...)
	case 5, 6: // a framing number set to a value the strategy picks
		if len(s.frame.spots) == 0 || len(data) != len(s.wire) {
			data[r.below(len(data))] = uint8(r.next())
			break
		}
		sp := s.frame.spots[r.below(len(s.frame.spots))]
		var v uint64
		switch r.below(4) {
		case 0:
			v = r.next()
		case 1:
			v = sp.value + 1 + uint64(r.below(4))
		case 2:
			v = uint64(sp.remaining()) + 1 + uint64(r.below(4))
		case 3:
			v = uint64(r.below(20))
		}
		put(data, sp.off, sp.width, v)
	case 7: // a window spliced in from another seed of the same unit
		var pool []*wireSeed
		for _, o := range seeds {
			if o.unit == s.unit && len(o.wire) > 0 {
				pool = append(pool, o)
			}
		}
		if len(pool) == 0 {
			break
		}
		o := pool[r.below(len(pool))]
		from := r.below(len(o.wire))
		n := 1 + r.below(16)
		if from+n > len(o.wire) {
			n = len(o.wire) - from
		}
		at := r.below(len(data) + 1)
		window := append([]byte(nil), o.wire[from:from+n]...)
		data = append(data[:at], append(window, data[at:]...)...)
	case 8: // a field duplicated, framing fixed or not
		if len(s.frame.fields) == 0 || len(data) != len(s.wire) {
			break
		}
		fixed := duplicateField(s, r.below(len(s.frame.fields)), r.below(2) == 0)
		data = fixed
	}
	return data
}

// earlyTerminator writes a u16 zero AHEAD of a body payload's last two bytes,
// so the body ends inside its own `L` and the bytes after it are claimed by no
// field (docs/SPEC-TABLES.md §3). It answers false where the payload is too
// short to carry the move, or where the bytes it would write are already zero.
