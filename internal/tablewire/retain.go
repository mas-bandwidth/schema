package tablewire

import (
	"encoding/binary"
	"errors"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// errRetainNeedsReport is SaveRetain's own refusal (docs/SPEC-TABLES.md §6.6):
// the save is the only place a caller learns that a record was dropped, so the
// report is required here where it is optional everywhere else.
var errRetainNeedsReport = errors.New("SaveRetain refuses a null report: it is the only place a caller learns a record was dropped (docs/SPEC-TABLES.md §6.6)")

// RETAIN-UNKNOWN IN THE ORACLE (docs/SPEC-TABLES.md §6.6). This engine is the
// wire fuzzer's divergence oracle (§4.2), a third reading of §3 written from
// the page rather than from a backend, and this file is that same reading of
// §6.6: the caller's two stores, the resolving walk one pass each way, the six
// excluded classes counted at one each, the drop rule, and the merged
// first-use trailer.
//
// IT IS A REGION ROUND TRIP AND ONLY THAT (§6.6): [DecodeRetain] is [Decode]'s
// path into the tree this engine holds, and [EncodeRetain] saves from that
// same tree. The plain [Decode], [Encode] pair is unchanged and retains
// nothing, so every existing caller and every golden stands byte for byte.

// Retain is THE CALLER'S TWO STORES (docs/SPEC-TABLES.md §6.6): a byte
// capacity for the retained records and an entry capacity for the retained
// ids' own list. It allocates nothing the caller did not ask for, it never
// grows, and it is the whole of the memory this feature can command. A
// retention buffer belongs to ONE loaded region, and the next [DecodeRetain]
// into it resets both stores.
type Retain struct {
	// Capacity is the retention buffer's bytes. A record the remaining
	// capacity cannot hold WHOLE is not kept at all: one `retain_lost` counts
	// and the buffer never holds a truncated field.
	Capacity int
	// IdCapacity is the entries the retained ids' own list holds. THE
	// GENERATED ID TABLE CANNOT HOLD A RETAINED ID — it is sized by a
	// compile-time constant, the distinct names this unit's closure can spell
	// — so the retention tail carries its own list and the caller places it.
	// `Capacity / 8` is the count bound no file can beat, and the number of
	// entries below it is the caller's own policy.
	IdCapacity int

	used int
	// ids is the caller's list, FILLED BY THE SAVE WALK: a load resets it and
	// writes into neither list, because a retained record carries its field's
	// identity in the record itself with every reference resolved (§6.6).
	ids []uint64
	// records are the retained fields, by the BODY OCCURRENCE that carried
	// them. A record belongs to that occurrence and dies with it: a later
	// legal occurrence of a known body is a new instance in this engine, so
	// the earlier occurrence's records are discarded with the values it held
	// and neither counter moves (§6.6).
	records map[*tabletext.Instance][]retainedField
}

// Used is what the retention buffer holds, in bytes.
func (r *Retain) Used() int { return r.used }

// Ids is the retained-id list as the save walk interned it, in first-use
// order. It is empty until a save has run.
func (r *Retain) Ids() []uint64 { return r.ids }

func (r *Retain) reset() {
	r.used = 0
	r.ids = r.ids[:0]
	r.records = map[*tabletext.Instance][]retainedField{}
}

// fits reports whether a record of `cost` bytes fits WHOLE in what is left,
// and takes the room when it does. THE CALLER'S CAPACITY IS THE ONLY CEILING
// AND THE WIRE CANNOT RAISE IT (§6.6).
func (r *Retain) fits(cost int) bool {
	if r.used+cost > r.Capacity {
		return false
	}
	r.used += cost
	return true
}

// retainedField is ONE unknown field kept whole, with every reference replaced
// by the sixty-four-bit id it names. It is READER-PRIVATE (§6.6): no form
// byte, no version, no declared byte order, nothing writes one to disk, and
// nothing compares two ports' records. What the page fixes is what a record
// must CARRY — the body it belongs to, the field's identity and its bytes with
// every reference resolved — and the layout inside is this engine's own.
type retainedField struct {
	id      uint64
	kind    uint8
	payload []byte
	cost    int
}

// retainDepthCap is THE WALK'S NESTING CAP, a small stated constant
// (docs/SPEC-TABLES.md §6.6): a retained record's inner nesting is the
// WRITER's and not this build's, so it is the one depth on this path a file
// can drive. A record past the cap is dropped on the same rule as any other
// shape the walk cannot take. It bounds the recursion and nothing else.
const retainDepthCap = 64

// retainState is what a retaining read carries beside the plain one: the
// caller's stores, and the set of ids this build can spell, which is what
// decides WHICH STORE an id inside a retained record takes at save.
type retainState struct {
	store *Retain
	known map[uint64]bool
}

func newRetainState(m *tabletext.Model, store *Retain) *retainState {
	store.reset()
	known := map[uint64]bool{}
	for _, id := range ir.TableWireIds(m.Unit) {
		known[id] = true
	}
	return &retainState{store: store, known: known}
}

// lost counts one unknown this load or save could not keep. Every exclusion
// counts it, so a caller that needs to know retention held reads ONE NUMBER
// and never has to reason about the list (§6.6).
func (rt *retainState) lost(report *tabletext.Report) { report.RetainLost++ }

// ---------------------------------------------------------------------------
// the resolving walk, capture side
// ---------------------------------------------------------------------------

// cursor is a reader over one payload's bytes. The resolving walk reads KIND
// BYTES, LENGTHS AND REFERENCES and nothing else: no value is decoded, no
// bound is checked, no branch is taken on a payload byte, and no allocation is
// sized from one (docs/SPEC-TABLES.md §6.6, THE SECURITY BOUND).
type cursor struct {
	buf []byte
	off int
}

func (c *cursor) has(n int) bool { return n >= 0 && c.off+n <= len(c.buf) }

func (c *cursor) leb() (uint64, bool) {
	v, next, ok := readLeb(c.buf, c.off)
	if !ok {
		return 0, false
	}
	c.off = next
	return v, true
}

// resolver captures one record. `bad` is the DROP: any shape the walk cannot
// frame, any reference it cannot resolve, and any kind `17` at any depth sets
// it, and the caller counts one `retain_lost` and changes nothing else.
type resolver struct {
	ids []uint64
	out []byte
	bad bool
}

// id resolves one reference against the FILE's id table and answers the
// sixty-four-bit id it names. THE WALK IS AN INTERPRETATION AND ITS VERDICT IS
// STATED (§6.6): a reference above the entry count, a reference at an entry of
// ZERO, which names no id at all, and a reference at one of the three RESERVED
// ids, which would be re-emitted into a nested body where it is malformed, all
// DROP THE RECORD.
func (rs *resolver) id(ref uint64) uint64 {
	if ref == 0 || ref > uint64(len(rs.ids)) {
		rs.bad = true
		return 0
	}
	id := rs.ids[ref-1]
	switch id {
	case 0, ir.TableNodeWireId, ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId:
		rs.bad = true
		return 0
	}
	return id
}

func (rs *resolver) u64(v uint64) { rs.out = binary.LittleEndian.AppendUint64(rs.out, v) }
func (rs *resolver) u8(v uint8)   { rs.out = append(rs.out, v) }

// mark reserves a fixed-width length and answers where it sits. THE LENGTHS
// INSIDE A RECORD ARE THIS ENGINE'S OWN SPELLING and nothing outside the
// reader ever reads them, so each one is written AFTER the content it frames
// rather than before, and a record costs ONE PASS to capture (§6.6). A walk
// that instead measured a content and then walked it again to write it would
// double its work at every level of a nesting the FILE chooses.
func (rs *resolver) mark() int {
	at := len(rs.out)
	rs.u64(0)
	return at
}

func (rs *resolver) patch(at int) {
	binary.LittleEndian.PutUint64(rs.out[at:], uint64(len(rs.out)-at-8))
}

// verbatim copies a payload the walk does not touch, exactly as the wire
// spells it. Every scalar, every fixed-point and 128-bit kind, and kinds 12,
// 31 and 33 are copied this way (§6.6), and so is an array whose element kind
// carries no reference of its own.
func (rs *resolver) verbatim(c *cursor, from int) {
	rs.u64(uint64(c.off - from))
	rs.out = append(rs.out, c.buf[from:c.off]...)
}

// skipPayload steps one payload of `kind`, by §3's framing rules alone. It is
// the same four rules the plain read's skip takes, over a cursor rather than a
// reader.
func skipPayload(c *cursor, kind uint8) bool {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8,
		ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32,
		ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128:
		w := ir.TableKindWidth(int(kind))
		if !c.has(w) {
			return false
		}
		c.off += w
		return true
	case ir.TableKindPointer, ir.TableKindEnum:
		_, ok := c.leb()
		return ok
	case ir.TableKindString, ir.TableKindTable, ir.TableKindArray, ir.TableKindKeyed,
		ir.TableKindEscape, ir.TableKindNoPayload, ir.TableKindWstring:
		n, ok := c.leb()
		if !ok || n > uint64(len(c.buf)) || !c.has(int(n)) {
			return false
		}
		c.off += int(n)
		return true
	case ir.TableKindUnion:
		arm, ok := c.leb()
		if !ok {
			return false
		}
		if arm == 0 {
			return true
		}
		if !c.has(1) {
			return false
		}
		c.off++
		n, ok := c.leb()
		if !ok || n > uint64(len(c.buf)) || !c.has(int(n)) {
			return false
		}
		c.off += int(n)
		return true
	}
	// A kind a reader does not know at all is not skippable, and is framing
	// damage — kind 34 included, which no writer emits and which a reader of
	// this major cannot skip (§3).
	return false
}

// walkPayload is the resolving walk over one self-framed payload.
//
// WHICH KINDS IT TOUCHES (docs/SPEC-TABLES.md §6.6): kind 13, a body's fields;
// kind 15, an arm id and then the arm's payload under this same rule; kind 30,
// a variant id; kind 14 where the element kind is 13, 15 or 30; and kind 16 at
// EVERY element kind, because an enum-keyed array's body carries a KEY
// REFERENCE per slot whatever the elements are. Every other payload is copied
// verbatim.
//
// KIND 17 IS WHAT THE WALK IS LOOKING FOR AS MUCH AS A REFERENCE IS: meeting
// one anywhere in the payload drops the whole record and keeps the record
// atomic.
func (rs *resolver) walkPayload(c *cursor, kind uint8, depth int) {
	if rs.bad {
		return
	}
	if depth > retainDepthCap {
		rs.bad = true
		return
	}
	switch kind {
	case ir.TableKindPointer:
		// THE NODE-INDEX CLASS, met inside a payload rather than at the outer
		// kind: the record is dropped whole (§6.6)
		rs.bad = true
		return
	case ir.TableKindEnum:
		ref, ok := c.leb()
		if !ok {
			rs.bad = true
			return
		}
		rs.u64(rs.id(ref))
		return
	case ir.TableKindTable:
		n, ok := c.leb()
		if !ok || n > uint64(len(c.buf)) || !c.has(int(n)) {
			rs.bad = true
			return
		}
		body := &cursor{buf: c.buf[c.off : c.off+int(n)]}
		c.off += int(n)
		at := rs.mark()
		rs.walkBody(body, depth+1)
		rs.patch(at)
		return
	case ir.TableKindUnion:
		ref, ok := c.leb()
		if !ok {
			rs.bad = true
			return
		}
		if ref == 0 {
			// AN ARM ID OF ZERO is the union holding nothing, and it names no
			// id: it is the arm's own None and never a reference (§3)
			rs.u64(0)
			return
		}
		rs.u64(rs.id(ref))
		if !c.has(1) {
			rs.bad = true
			return
		}
		armKind := c.buf[c.off]
		c.off++
		rs.u8(armKind)
		n, ok := c.leb()
		if !ok || n > uint64(len(c.buf)) || !c.has(int(n)) {
			rs.bad = true
			return
		}
		arm := &cursor{buf: c.buf[c.off : c.off+int(n)]}
		c.off += int(n)
		at := rs.mark()
		rs.walkContent(arm, armKind, depth+1)
		rs.patch(at)
		return
	case ir.TableKindArray, ir.TableKindKeyed:
		from := c.off
		n, ok := c.leb()
		if !ok || n > uint64(len(c.buf)) || !c.has(int(n)) {
			rs.bad = true
			return
		}
		body := &cursor{buf: c.buf[c.off : c.off+int(n)]}
		c.off += int(n)
		// A BODY TOO SHORT FOR ITS OWN HEADER is inert (§4): there is no
		// element kind to read and nothing to resolve, so it copies whole.
		if len(body.buf) < 2 {
			rs.verbatim(c, from)
			return
		}
		ek := body.buf[0]
		if ek == ir.TableKindPointer {
			// AN ARRAY WHOSE ELEMENT KIND IS 17 (§6.6): the excluded class,
			// caught here for a keyed body exactly as for a positional one
			rs.bad = true
			return
		}
		if kind == ir.TableKindArray && !rs.walks(ek) {
			// an array of a kind carrying no reference of its own copies whole
			rs.verbatim(c, from)
			return
		}
		body.off = 1
		count, ok := body.leb()
		if !ok {
			rs.bad = true
			return
		}
		at := rs.mark()
		rs.u8(ek)
		rs.u64(count)
		for range count {
			if rs.bad {
				break
			}
			if kind == ir.TableKindKeyed {
				rs.keyedSlot(body, ek, depth+1)
				continue
			}
			rs.walkPayload(body, ek, depth+1)
		}
		rs.patch(at)
		return
	}
	// EVERY OTHER PAYLOAD IS COPIED VERBATIM: every scalar, every fixed-point
	// and 128-bit kind, and kinds 12, 31 and 33. Kind 32 has no payload and
	// there is nothing to walk. A kind the framing rules do not cover is
	// damage and drops the record.
	from := c.off
	if !skipPayload(c, kind) {
		rs.bad = true
		return
	}
	rs.verbatim(c, from)
}

// walks reports whether an ARRAY's element kind carries a reference of its
// own, which is kind 13, 15 or 30 and nothing else (§6.6).
func (rs *resolver) walks(ek uint8) bool {
	return ek == ir.TableKindTable || ek == ir.TableKindUnion || ek == ir.TableKindEnum
}

// keyedSlot is one triple of an enum-keyed body: the KEY REFERENCE, the
// element's own length, and the element. THE KEYS RESOLVE EVEN WHEN THE
// ELEMENTS CARRY NO REFERENCE OF THEIR OWN (§3.2, §6.6).
func (rs *resolver) keyedSlot(c *cursor, ek uint8, depth int) {
	ref, ok := c.leb()
	if !ok {
		rs.bad = true
		return
	}
	rs.u64(rs.id(ref))
	n, ok := c.leb()
	if !ok || n > uint64(len(c.buf)) || !c.has(int(n)) {
		rs.bad = true
		return
	}
	elem := &cursor{buf: c.buf[c.off : c.off+int(n)]}
	c.off += int(n)
	at := rs.mark()
	rs.walkContent(elem, ek, depth)
	rs.patch(at)
}

// walkContent resolves a payload ALREADY FRAMED by an outer length — a union
// arm's, and an enum-keyed slot's element. A body carries its fields directly
// with no inner length of its own.
func (rs *resolver) walkContent(c *cursor, kind uint8, depth int) {
	if rs.bad {
		return
	}
	switch kind {
	case ir.TableKindPointer:
		rs.bad = true
	case ir.TableKindTable:
		rs.walkBody(c, depth+1)
	case ir.TableKindEnum:
		ref, ok := c.leb()
		if !ok || c.off != len(c.buf) {
			rs.bad = true
			return
		}
		rs.u64(rs.id(ref))
	case ir.TableKindUnion:
		rs.walkPayload(c, ir.TableKindUnion, depth)
		if c.off != len(c.buf) {
			rs.bad = true
		}
	default:
		// the content is copied whole: it carries no reference, and its outer
		// length is what frames it
		rs.u64(uint64(len(c.buf)))
		rs.out = append(rs.out, c.buf...)
	}
}

// walkBody resolves one table body: its fields, each an id REFERENCE, a kind
// byte and a payload, ending at the body's own ZERO REFERENCE. AN INNER BODY
// WHOSE TERMINATOR FALLS SHORT drops the record (§6.6).
func (rs *resolver) walkBody(c *cursor, depth int) {
	if depth > retainDepthCap {
		rs.bad = true
		return
	}
	for {
		if rs.bad {
			return
		}
		ref, ok := c.leb()
		if !ok {
			rs.bad = true
			return
		}
		if ref == 0 {
			if c.off != len(c.buf) {
				rs.bad = true
			}
			return
		}
		if !c.has(1) {
			rs.bad = true
			return
		}
		kind := c.buf[c.off]
		c.off++
		rs.u64(rs.id(ref))
		rs.u8(kind)
		rs.walkPayload(c, kind, depth+1)
	}
}

// capture is the whole of the load side for one unknown field: the outer-kind
// exclusions, the resolving walk, the capacity, and the counter each answers.
// The bytes it walks are the ones the plain read's own SKIP already delimited,
// so the ADDED cost is one pass over the record (§6.6).
func (rt *retainState) capture(inst *tabletext.Instance, id uint64, kind uint8, payload []byte, ids []uint64, report *tabletext.Report) {
	if kind == ir.TableKindPointer {
		// A FIELD WHOSE PAYLOAD CARRIES A NODE INDEX: kind 17 itself, the
		// excluded class at the outer kind (§6.6)
		rt.lost(report)
		return
	}
	rs := &resolver{ids: ids}
	c := &cursor{buf: payload}
	rs.walkPayload(c, kind, 1)
	if rs.bad || c.off != len(c.buf) {
		// THE WALK CAN MEET DAMAGE THE READ NEVER LOOKED AT, and its verdict
		// changes nothing else: the record is dropped, one retain_lost counts,
		// and `malformed` DOES NOT MOVE. The outer framing was sound, every
		// sibling decoded, and the reader's own data is exactly what it would
		// have been with retention off (§6.6).
		rt.lost(report)
		return
	}
	// THE EXPANSION IS BOUNDED, AND THIS ENGINE STATES THE CONSTANT: a record
	// costs the id it rides under, its kind byte and the resolved payload,
	// which is the wire's bytes plus seven for each reference widened to eight
	// and this engine's own fixed-width inner lengths (§6.6).
	cost := 9 + len(rs.out)
	if !rt.store.fits(cost) {
		// REFUSAL IS PER RECORD AND NEVER PARTIAL: the record is not written
		// at all, the read continues, and the buffer never holds a truncated
		// field. A full buffer degrades to the default behavior one field at a
		// time.
		rt.lost(report)
		return
	}
	rt.store.records[inst] = append(rt.store.records[inst], retainedField{id: id, kind: kind, payload: rs.out, cost: cost})
	report.Retained++
}

// ---------------------------------------------------------------------------
// the resolving walk, emit side
// ---------------------------------------------------------------------------

// emitter writes one body's retained tail back onto the wire. It is the walk
// above run in the other direction: every resolved id is re-interned into THIS
// file's table, and every length is written back as canonical LEB128.
//
// IT IS ONE POST-ORDER PASS: a wire length rides FIRST, so each content is
// built into its own buffer and the length that frames it is written from that
// buffer's size. A port that instead measured a content by walking it, then
// walked it again to write it, would double its work at every level of a
// nesting the FILE chooses, and §6.6's security bound forbids it.
type emitter struct {
	e   *encoder
	rt  *retainState
	bad bool
}

// ref interns one id of a retained record into the file's ONE id table. THE
// FILE STILL CARRIES ONE ID TABLE and the split is the writer's storage rather
// than the wire's (§6.6): an id this build can name takes its entry from the
// generated table, a retained id takes its entry from the caller's list, and
// both are numbered into one trailer in the order the walk first uses them.
func (em *emitter) ref(w *buf, id uint64) {
	if !em.rt.known[id] && !em.intern(id) {
		// A RETAINED ID PAST THE CAPACITY counts one `retain_lost` and its
		// record is dropped, and the save is never refused (§6.6)
		em.bad = true
		return
	}
	w.leb(em.e.ids.ref(id))
}

// intern takes the id's entry from the caller's list, or reports that the list
// is full. A retained id used by two records takes ONE entry, exactly as any
// repeat does.
func (em *emitter) intern(id uint64) bool {
	for _, held := range em.rt.store.ids {
		if held == id {
			return true
		}
	}
	if len(em.rt.store.ids) >= em.rt.store.IdCapacity {
		return false
	}
	em.rt.store.ids = append(em.rt.store.ids, id)
	return true
}

// record is one retained field back on the wire: the id reference, the kind
// byte, and the payload under the same rules the capture walked.
func (em *emitter) record(w *buf, rec retainedField) {
	em.ref(w, rec.id)
	if em.bad {
		return
	}
	w.u8(rec.kind)
	c := &cursor{buf: rec.payload}
	em.payload(w, c, rec.kind)
}

func (em *emitter) framed(c *cursor) *cursor {
	if !c.has(8) {
		em.bad = true
		return &cursor{}
	}
	n := int(binary.LittleEndian.Uint64(c.buf[c.off:]))
	c.off += 8
	if n < 0 || !c.has(8+n-8) || c.off+n > len(c.buf) {
		em.bad = true
		return &cursor{}
	}
	sub := &cursor{buf: c.buf[c.off : c.off+n]}
	c.off += n
	return sub
}

func (em *emitter) resolved(c *cursor) uint64 {
	if !c.has(8) {
		em.bad = true
		return 0
	}
	v := binary.LittleEndian.Uint64(c.buf[c.off:])
	c.off += 8
	return v
}

func (em *emitter) payload(w *buf, c *cursor, kind uint8) {
	if em.bad {
		return
	}
	switch kind {
	case ir.TableKindEnum:
		em.ref(w, em.resolved(c))
		return
	case ir.TableKindTable:
		body := em.framed(c)
		inner := &buf{}
		em.body(inner, body)
		w.leb(uint64(len(inner.b)))
		w.raw(inner.b)
		return
	case ir.TableKindUnion:
		arm := em.resolved(c)
		if arm == 0 {
			w.leb(0)
			return
		}
		em.ref(w, arm)
		if !c.has(1) {
			em.bad = true
			return
		}
		armKind := c.buf[c.off]
		c.off++
		w.u8(armKind)
		content := em.framed(c)
		inner := &buf{}
		em.content(inner, content, armKind)
		w.leb(uint64(len(inner.b)))
		w.raw(inner.b)
		return
	case ir.TableKindArray, ir.TableKindKeyed:
		body := em.framed(c)
		if body.off == 0 && len(body.buf) > 0 && !em.structured(kind, body) {
			// a body copied whole rides back exactly as it came
			w.leb(uint64(len(body.buf)))
			w.raw(body.buf)
			return
		}
		inner := &buf{}
		em.arrayBody(inner, body, kind)
		w.leb(uint64(len(inner.b)))
		w.raw(inner.b)
		return
	}
	verbatim := em.framed(c)
	w.raw(verbatim.buf)
}

// structured reports whether an array or keyed payload was captured in the
// resolved shape rather than copied whole. A keyed body always is, because its
// keys resolve at every element kind; a positional array is exactly when its
// element kind is 13, 15 or 30 (§6.6).
func (em *emitter) structured(kind uint8, body *cursor) bool {
	if kind == ir.TableKindKeyed {
		return true
	}
	if len(body.buf) < 1 {
		return false
	}
	ek := body.buf[0]
	return ek == ir.TableKindTable || ek == ir.TableKindUnion || ek == ir.TableKindEnum
}

func (em *emitter) arrayBody(w *buf, c *cursor, kind uint8) {
	if !c.has(1) {
		em.bad = true
		return
	}
	ek := c.buf[c.off]
	c.off++
	w.u8(ek)
	count := em.resolved(c)
	w.leb(count)
	for range count {
		if em.bad {
			return
		}
		if kind == ir.TableKindKeyed {
			em.ref(w, em.resolved(c))
			content := em.framed(c)
			inner := &buf{}
			em.content(inner, content, ek)
			w.leb(uint64(len(inner.b)))
			w.raw(inner.b)
			continue
		}
		em.payload(w, c, ek)
	}
}

func (em *emitter) content(w *buf, c *cursor, kind uint8) {
	if em.bad {
		return
	}
	switch kind {
	case ir.TableKindTable:
		em.body(w, c)
	case ir.TableKindEnum:
		em.ref(w, em.resolved(c))
	case ir.TableKindUnion:
		em.payload(w, c, ir.TableKindUnion)
	default:
		verbatim := em.framed(c)
		w.raw(verbatim.buf)
	}
}

func (em *emitter) body(w *buf, c *cursor) {
	for c.off < len(c.buf) {
		if em.bad {
			return
		}
		id := em.resolved(c)
		if !c.has(1) {
			em.bad = true
			return
		}
		kind := c.buf[c.off]
		c.off++
		em.ref(w, id)
		if em.bad {
			return
		}
		w.u8(kind)
		em.payload(w, c, kind)
	}
	w.u8(0) // the body ENDS AT ITS OWN ZERO REFERENCE
}

// tail writes one body's retained fields AT THE END OF THAT BODY, IN THE ORDER
// RETAINED (docs/SPEC-TABLES.md §6.6). Position carries nothing on this wire,
// so appending is chosen for three properties: it is a write with no splice,
// the retained order among the retained fields is preserved, and the result is
// IDEMPOTENT after the first save.
//
// A record whose emit goes bad — a retained id the caller's list had no room
// for — is not written at all: one `retain_lost` counts, nothing else about
// the save changes, and the save is never refused.
func (e *encoder) tail(w *buf, inst *tabletext.Instance) {
	if e.rt == nil {
		return
	}
	for _, rec := range e.rt.store.records[inst] {
		em := &emitter{e: e, rt: e.rt}
		inner := &buf{}
		mark := e.ids.mark()
		em.record(inner, rec)
		if em.bad {
			e.ids.rollback(mark)
			e.retainLost++
			continue
		}
		w.raw(inner.b)
	}
}

// retains reports whether a body holds a retained field. A BODY THAT CARRIES
// ONE DOES NOT ELIDE (§6.6): a by-value `T` at its defaults writes nothing,
// and one whose body holds a retained field writes its body, that field and
// its terminator, because elision is about what a body CONTAINS and a retained
// field is content.
func (e *encoder) retains(inst *tabletext.Instance) bool {
	if e.rt == nil || inst == nil {
		return false
	}
	return len(e.rt.store.records[inst]) > 0
}

// ---------------------------------------------------------------------------
// the two verbs
// ---------------------------------------------------------------------------

// DecodeRetain is [Decode] with retention on (docs/SPEC-TABLES.md §6.6). It
// reads exactly what [Decode] reads and reports exactly what [Decode] reports
// — RETENTION MOVES NO EXISTING COUNTER, and a retained field still counts
// `unknown`, because `unknown` says what a READER could not name and that
// stays true — and beside that it keeps the fields this build cannot name, in
// the caller's own buffer.
//
// RETENTION CAN LOSE A FIELD. IT CAN NEVER TURN A GOOD READ INTO A BAD ONE,
// and that is the property that lets a caller switch it on without re-reading
// its own error handling.
func DecodeRetain(m *tabletext.Model, inst *tabletext.Instance, data []byte, retain *Retain, report *tabletext.Report) (bool, error) {
	if retain == nil {
		return Decode(m, inst, data, report)
	}
	rt := newRetainState(m, retain)
	return decodeWith(m, inst, data, report, rt)
}

// EncodeRetain is [Encode] from the same tree, with the retained fields
// written back at the end of the bodies they came from. The report is
// REQUIRED here where it is optional everywhere else: `MeasureRetain` and
// `SaveRetain` drop the same records under the same walk, so the save is the
// only place a caller learns that a record was dropped, and a surface that let
// a caller retain, save and never find out would be a promise it could not
// check (§6.6).
func EncodeRetain(m *tabletext.Model, inst *tabletext.Instance, retain *Retain, report *tabletext.Report) ([]byte, error) {
	if report == nil {
		// SaveRetain REFUSES A NULL REPORT (§6.6)
		return nil, errRetainNeedsReport
	}
	if retain == nil {
		return Encode(m, inst)
	}
	// THE LIST FILLS AS THE SAVE WALK INTERNS, and one save's interning is not
	// the next one's: a second save of the same region reaches the same ids in
	// the same order, which is what makes it byte-identical.
	retain.ids = retain.ids[:0]
	rt := &retainState{store: retain, known: map[uint64]bool{}}
	for _, id := range ir.TableWireIds(m.Unit) {
		rt.known[id] = true
	}
	out, lost, err := encodeWith(m, inst, rt)
	report.RetainLost += lost
	return out, err
}
