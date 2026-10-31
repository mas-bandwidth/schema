// The wire's READ half (docs/SPEC-TABLES.md §4): any reader reads any data, and
// evolution is reported, never fatal. Unknown ids are skipped by their
// length and counted, a kind mismatch is skipped rather than misdecoded,
// out-of-range values clamp. Damage anywhere discards the whole load and
// reports `malformed`. Every decision mirrors the generated C++ reader.
package tablewire

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// FormRefusal is the wire a reader will not decode at all: a FORM BYTE it does
// not know (docs/SPEC-TABLES.md §3). The refusal is not one of §4's events and
// moves none of the report's five counters, because nothing was decoded and
// there is nothing to count — so it is returned rather than folded into the
// report, and a caller must not carry on as if it had a value.
type FormRefusal struct{ Form uint8 }

func (e *FormRefusal) Error() string {
	return fmt.Sprintf("wire form %d is newer than form %d, the one this reader carries — refused, and no damage is reported (docs/SPEC-TABLES.md §3)", e.Form, ir.TableWireForm)
}

// Decode fills one instance from a root table's wire bytes, counting every
// evolution event in the report. The error is a REFUSAL — a wire this engine
// does not decode at all — and is returned rather than folded into the report.
// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): false with
// `malformed` in the report means the instance holds its declared defaults
// and nothing the walk read.
func Decode(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	return decodeWith(m, inst, data, report, nil, false)
}

// DecodeRegion models a load into a region sized by FileRegionMeasure. A
// collection header can finish beyond its own L during value decoding even
// though the framing pass bounded that header by L. A count beyond the storage
// cap then empties that collection and reports damage; a builder instead
// refuses its partial value. The caller must honor a framing refusal first.
func DecodeRegion(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	return decodeWith(m, inst, data, report, nil, true)
}

// decodeWith is Decode with RETENTION's state threaded through it
// (docs/SPEC-TABLES.md §6.6). `rt` is nil for every plain read, and the three
// names are ADDITIVE: nothing below reads a byte differently, counts an event
// differently or places a value differently because retention is on.
func decodeWith(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report, rt *retainState, region bool) (bool, error) {
	// A NON-FINITE FLOAT IS THIS READ'S OWN REFUSAL (docs/SPEC-TABLES.md
	// §3.3): a name an earlier read left on a reused report neither refuses
	// this one nor is lost by it.
	prior := report.NonFiniteFloat
	report.NonFiniteFloat = ""
	priorMalformed := report.Malformed
	report.Malformed = false
	ok, err := decodeWalk(m, inst, data, report, rt, region)
	if refusal := refusedNonFinite(report); refusal != nil {
		report.Malformed = report.Malformed || priorMalformed
		return false, refusal
	}
	report.NonFiniteFloat = prior
	if report.Malformed {
		// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): nothing
		// the walk decoded, defaulted or retained before or after the damage
		// is handed back. The instance returns to its declared defaults and
		// the retention store to empty, and `malformed` is the answer.
		m.Clear(inst)
		if rt != nil {
			rt.store.reset()
		}
		return false, err
	}
	report.Malformed = priorMalformed
	return ok, err
}

func decodeWalk(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report, rt *retainState, region bool) (bool, error) {
	// THE FORM BYTE IS READ FIRST, before the trailer and before any body, so
	// a file that is both a newer form and damaged is a refusal and never
	// damage (§3).
	if len(data) < 1 {
		report.Malformed = true
		return false, nil
	}
	if data[0] != ir.TableWireForm {
		report.Refused = true
		if data[0] == ir.TableWireMessageForm {
			// FORM `2` IS A STREAM FORM AND NEVER A FILE'S OWN FORM
			// (docs/SPEC-TABLES.md §3.3): a message stored on its own is not
			// readable, because its table is somewhere else, so a reader
			// handed one where a file was expected refuses BY NAME rather
			// than merely by form byte.
			return false, &MessageRefusal{Reason: ReasonMessageFormAsFile}
		}
		return false, &FormRefusal{Form: data[0]}
	}
	body, ids, ok := trailer(data)
	if !ok {
		// the one malformed case that stops the FILE rather than a nesting
		// level: every id in every body resolves through the table, and a body
		// read without it would be read without identity (§3)
		report.Malformed = true
		return false, nil
	}
	// ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST ENTRY IS
	// MALFORMED, because no field claims it and the two ends of the file have
	// met (§3). Nothing is decoded, and one event is counted.
	if end, terminated := bodyExtent(body, ids); terminated && end != len(body) {
		report.Malformed = true
		return false, nil
	}
	if !ir.VariableTables(m.Unit)[inst.Def.Name] {
		// a value-only table has no pointer, therefore no node table, therefore
		// exactly the bytes §3 already describes
		r := &wireReader{buf: body, report: report, m: m, ids: ids, rt: rt}
		return r.body(inst), nil
	}
	return decodeVariable(m, inst, body, ids, report, rt, region)
}

// trailer locates the ID TABLE from the END of the wire (docs/SPEC-TABLES.md
// §3): the final eight bytes are the ENTRY COUNT, a fixed little-endian u64,
// the `8 × count` bytes before them are the ENTRIES, and the body ends where
// the first entry begins.
//
// A table that cannot be read whole is malformed: fewer than eight bytes in
// the file, a count whose `8 × count + 8` runs past the front, a count that
// leaves no room for the form byte, or ONE ID IN TWO ENTRIES.
func trailer(data []byte) (body []byte, ids []uint64, ok bool) {
	return trailerBounded(data, ir.TableWireIdTableBound)
}

// trailerBounded is trailer under an explicit entry-count bound, so a test can
// show that the bound is what refuses a table of bound + 1 entries and nothing
// else is (docs/LAYERS/1-framing.md F6).
func trailerBounded(data []byte, bound uint64) (body []byte, ids []uint64, ok bool) {
	if len(data) < 9 {
		return nil, nil, false
	}
	count := binary.LittleEndian.Uint64(data[len(data)-8:])
	if count > uint64(len(data))/8 || count > bound {
		// THE ENTRY COUNT HAS A MAXIMUM (§3, THE DISTINCTNESS CHECK): a
		// table above it is malformed on the whole-wire rule, whatever the
		// buffer would hold
		return nil, nil, false
	}
	span := int(count)*8 + 8
	if span+1 > len(data) {
		return nil, nil, false
	}
	first := len(data) - span
	ids = make([]uint64, count)
	seen := make(map[uint64]bool, count)
	for i := range ids {
		id := binary.LittleEndian.Uint64(data[first+i*8:])
		if seen[id] {
			// A TABLE THAT CARRIES ONE ID TWICE: the whole wire is malformed.
			// A reader that resolved both entries would buy nothing, because no
			// wire this schema writes carries a repeat, and it would leave one
			// more shape of table for a hostile writer to aim at (§3).
			return nil, nil, false
		}
		seen[id] = true
		ids[i] = id
	}
	return data[1:first], ids, true
}

// bodyExtent walks a body's framing to the zero reference that ends it, so the
// caller can tell a body that ENDED EARLY — leaving bytes no field claims —
// from one that is merely damaged. `terminated` is false where the walk cannot
// reach a terminator at all, which is §4's ordinary framing damage on that body
// and not the file's.
func bodyExtent(body []byte, ids []uint64) (end int, terminated bool) {
	r := &wireReader{buf: body, report: &tabletext.Report{}, ids: ids}
	for {
		ref, next, good := readLeb(r.buf, r.off)
		if !good {
			return 0, false
		}
		r.off = next
		if ref == 0 {
			return r.off, true
		}
		if ref > uint64(len(ids)) {
			return 0, false
		}
		if !r.has(1) {
			return 0, false
		}
		if !r.skip(r.u8()) {
			return 0, false
		}
	}
}

type wireReader struct {
	// Installed by decodeVariable and shared by its nested readers. Only
	// value-only and framing/key scanners omit it; none can decode a list.
	countRefusal *bool // a storage refusal, not a malformed event
	region       bool  // region value pass, after the bounded framing pass

	buf    []byte
	off    int
	report *tabletext.Report
	m      *tabletext.Model
	ids    []uint64     // the file's id table, resolved once at open (§3)
	st     *decodeState // the save's numbering, shared by every sub-reader
	// rt is RETENTION's state when the caller opted in (docs/SPEC-TABLES.md
	// §6.6), and nil otherwise. It is shared by every sub-reader exactly as
	// the numbering is, and nil is the whole of the plain read: every branch
	// that tests it is a branch a plain read does not take.
	rt *retainState
}

func (r *wireReader) has(n int) bool { return n >= 0 && r.off+n <= len(r.buf) }

func (r *wireReader) u8() uint8 {
	v := r.buf[r.off]
	r.off++
	return v
}

func (r *wireReader) u32() uint32 {
	v := binary.LittleEndian.Uint32(r.buf[r.off:])
	r.off += 4
	return v
}

func (r *wireReader) u64() uint64 {
	v := binary.LittleEndian.Uint64(r.buf[r.off:])
	r.off += 8
	return v
}

// leb reads one length, count or index. ok is false where the encoding is
// truncated or NON-CANONICAL, which is framing damage on the body carrying it
// (§3).
func (r *wireReader) leb() (v uint64, ok bool) {
	v, next, good := readLeb(r.buf, r.off)
	if !good {
		return 0, false
	}
	r.off = next
	return v, true
}

// id resolves one reference against the file's table. ok is false where the
// reference is ABOVE the entry count, which is framing damage on the body that
// carries it. Reference `0` never reaches here: it names NO ID, and the three
// places it is a value read it before asking.
func (r *wireReader) id(ref uint64) (uint64, bool) {
	if ref == 0 || ref > uint64(len(r.ids)) {
		return 0, false
	}
	return r.ids[ref-1], true
}

func (r *wireReader) sub(n int) *wireReader {
	return &wireReader{buf: r.buf[r.off : r.off+n], report: r.report, m: r.m, ids: r.ids, st: r.st, rt: r.rt, countRefusal: r.countRefusal, region: r.region}
}

// subTo spans from the cursor to `end`, the last byte of the body the cursor
// is inside. Its header was read inside that body (§3), so the cursor is at or
// before `end`.
func (r *wireReader) subTo(end int) *wireReader {
	return r.sub(end - r.off)
}

// skip steps past a field whose id this reader cannot name — the kind byte and
// nothing else is needed, which is what makes an unknown field survivable (§3).
// Four rules cover the set.
func (r *wireReader) skip(kind uint8) bool {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8,
		ir.TableKindI16, ir.TableKindU16,
		ir.TableKindI32, ir.TableKindU32, ir.TableKindF32,
		ir.TableKindI64, ir.TableKindU64, ir.TableKindF64,
		ir.TableKindI128, ir.TableKindU128,
		ir.TableKindFixed8, ir.TableKindFixed16, ir.TableKindFixed32, ir.TableKindFixed64, ir.TableKindFixed128,
		ir.TableKindUFixed8, ir.TableKindUFixed16, ir.TableKindUFixed32, ir.TableKindUFixed64, ir.TableKindUFixed128,
		ir.TableKindTimestamp:
		// kind 36, the timestamp, is an eight-byte value and skips its width
		// like the i64 it is laid out as (docs/SPEC-TABLES.md §3, §2.11)
		w := ir.TableKindWidth(int(kind))
		if !r.has(w) {
			return false
		}
		r.off += w
		return true
	case ir.TableKindPointer, ir.TableKindEnum:
		// kinds 17 and 30 read one LEB128 value and stop
		_, ok := r.leb()
		return ok
	case ir.TableKindString, ir.TableKindTable, ir.TableKindArray, ir.TableKindKeyed,
		ir.TableKindEscape, ir.TableKindNoPayload, ir.TableKindWstring:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			return false
		}
		r.off += int(n)
		return true
	case ir.TableKindReservedFloat16:
		// KIND 34 IS RESERVED FOR float16 AND IS NOT PART OF THIS MAJOR (§3):
		// no writer emits it and no reader has a rule for it, so a reader
		// meets it only as DAMAGE, exactly as it meets 35 or 200
		return false
	case ir.TableKindUnion:
		// kind 15 reads the arm id reference and stops there if it is 0, else
		// reads the kind byte, then L, and skips L bytes
		arm, ok := r.leb()
		if !ok {
			return false
		}
		if arm == 0 {
			return true
		}
		if !r.has(1) {
			return false
		}
		r.off++ // the arm's kind byte
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			return false
		}
		r.off += int(n)
		return true
	}
	// A kind a reader does not know at all is not skippable, and is framing
	// damage — which is why the set is closed and why kind 31 exists (§3).
	return false
}

// wireKind is the kind byte a field's payload rides under. It is
// ir.TableWireFieldKind with the one case that function does not see: a POINTER
// field rides as a NODE INDEX under its own kind `17`, because a body that may
// be named twice cannot also sit inline at one of its names (docs/SPEC-TABLES.md
// §3.1). `*T` and a by-value `T` are therefore no longer one framing: `T` and
// `?T` share kind `13`, and an edit between a pointer and either of the others
// — or between a pointer and a plain `uint32` — is §4's kind mismatch, counted
// and never misdecoded.
func wireKind(f *ir.Field) int {
	if f.Type.Pointer && f.Array == ir.ArrayNone {
		return ir.TableKindPointer // an ARRAY of pointers is kind 14 with element kind 17 (§2.1)
	}
	if f.Type.Kind == ir.TBytes {
		return ir.TableKindArray
	}
	return ir.TableWireFieldKind(f)
}

func (r *wireReader) body(inst *tabletext.Instance) bool {
	return r.bodyAt(inst, false)
}

// bodyAt decodes one table body. `nested` says the body is not the root's, and
// it decides one rule: THE RESERVED ID INSIDE A NESTED BODY IS MALFORMED, on
// the numbering's own rule — a second numbering cannot exist, so a body
// claiming one is damaged (§3.1).
func (r *wireReader) bodyAt(inst *tabletext.Instance, nested bool) bool {
	index := map[uint64]int{}
	for i := range inst.Fields {
		index[ir.TableFieldWireId(inst.Fields[i].Def)] = i
	}
	// ONE ID, ONCE PER BODY (docs/SPEC-TABLES.md §3, §4; tla/Framing.tla,
	// OneIdOncePerBody): no writer puts an id down twice in one body, so a
	// body carrying one twice is damage, whether or not this reader can name
	// the id. The id table holds each id once, so the reference stands for
	// the id.
	seen := map[uint64]bool{}
	for {
		if r.countRefused() {
			return false
		}
		ref, ok := r.leb()
		if !ok {
			r.report.Malformed = true
			return false
		}
		if ref == 0 {
			return true // the body ENDS AT ITS OWN ZERO REFERENCE
		}
		id, named := r.id(ref)
		if !named || seen[ref] {
			r.report.Malformed = true
			return false
		}
		seen[ref] = true
		if !r.has(1) {
			r.report.Malformed = true
			return false
		}
		kind := r.u8()
		if id == ir.TableBuildVersionWireId || id == ir.TableMessageVocabularyWireId {
			// THE RESERVED BUILD-VERSION AND VOCABULARY IDS ride in the
			// ANNOUNCEMENT and nowhere else (docs/SPEC-TABLES.md §3.3): a
			// reserved id in any body but the one whose transport it is, is
			// MALFORMED (§3.1).
			r.report.Malformed = true
			return false
		}
		if id == ir.TableNodeWireId {
			if nested {
				r.report.Malformed = true
				return false
			}
			if r.st != nil && r.m.IsVariable(inst.Def.Name) {
				// the reserved id is this reader's OWN (§3.1), read before any
				// body and not a field of the table: a VARIABLE-class root
				// steps over it and never counts it unknown. A reader that
				// cannot name it — a fixed-class body, or a build without kind
				// 17 — counts one, which is the case §4's counter describes.
				if !r.skip(kind) {
					r.report.Malformed = true
					return false
				}
				continue
			}
		}
		i, known := index[id]
		if !known {
			r.report.Unknown++
			// WHAT IS RETAINED: A FIELD WHOSE ID THIS READER CANNOT NAME
			// (docs/SPEC-TABLES.md §6.6), at any depth, in the root body and
			// in every node record, except the six excluded classes. The skip
			// below is the plain read's own work and it delimits the field, so
			// the ADDED cost of retention is ONE PASS over the bytes it just
			// framed. A retained field still counts `unknown` above, because
			// `unknown` says what a READER could not name and that stays true.
			from := r.off
			if !r.skip(kind) {
				r.report.Malformed = true
				return false
			}
			if r.rt != nil {
				if !retainedPayloadFinite(kind, r.buf[from:r.off]) {
					// A RETAINED UNKNOWN FLOAT IS TESTED LIKE A KNOWN ONE
					// (docs/SPEC-TABLES.md §3.3, §6.6), and a NaN or an
					// infinity refuses the read: uniformity wins over carrying
					// bytes this reader cannot name
					r.report.RefuseNonFinite(retainedFloatName(inst, id))
					continue
				}
				r.rt.capture(inst, id, kind, r.buf[from:r.off], r.ids, r.report)
			}
			continue
		}
		fv := &inst.Fields[i]
		if int(kind) != wireKind(fv.Def) {
			if ir.TableKindWidens(int(kind), wireKind(fv.Def)) {
				// WIDENED (§4): a kind that grew since the writer decodes
				// exactly at its own width, the value lands, and one
				// `widened` counts. The field rode, so an optional is PRESENT.
				if !r.scalarAt(&fv.Cell, fv.Def, int(kind)) {
					return false
				}
				if !inst.Def.IsMapEntry() || fv.Def.Name != ir.MapKeyFieldName {
					r.report.Widened++
				}
				if fv.Def.Type.Optional {
					fv.Present = true
				}
				continue
			}
			// AT A POSITION THE READER DOES NAME, a field that arrives under
			// kind 31 or kind 32 where the declaration says otherwise takes
			// this same rule and no other (§3)
			r.report.KindMismatch++
			if !r.skip(kind) {
				r.report.Malformed = true
				return false
			}
			continue
		}
		if !r.field(fv) {
			return false
		}
		if fv.Def.Type.Optional && fv.Def.Array == ir.ArrayNone {
			// the field rode, so it is PRESENT — content decides nothing here
			// either (docs/SPEC-TABLES.md §2.3). An optional ARRAY's presence is
			// settled inside r.array instead: a foreign ELEMENT kind leaves it
			// absent, exactly as the generated reader does (§2.3).
			fv.Present = true
		}
	}
}

func (r *wireReader) field(fv *tabletext.Field) bool {
	f := fv.Def
	switch {
	case f.IsMap():
		return r.mapField(fv)
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		// A pointer field's payload is a NUMBER: it is bounds-checked and
		// stored, never followed. There is no traversal on the load path, and
		// therefore no traversal bound — no depth cap, no visited set, no
		// ordering rule on the indices (docs/SPEC-TABLES.md §3.1).
		index, ok := r.leb()
		if !ok {
			r.report.Malformed = true
			return false
		}
		r.resolve(fv, index)
		return true
	case f.KeyEnum != "":
		return r.keyed(fv)
	case f.Type.Kind == ir.TString:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false
		}
		if !textValid(r.buf[r.off : r.off+int(n)]) {
			// ILL-FORMED TEXT IS DAMAGE (§3, §4): the field reads its declared
			// default, one malformed counts, and the parent reads on past L
			r.report.Malformed = true
			fv.Cell = r.m.FieldDefaultCell(f)
			fv.Count = len(fv.Cell.Str)
			r.off += int(n)
			return true
		}
		keep := int(n)
		if bound := int(f.Type.Size); keep > bound {
			// A CLAMP CUTS AT A CODE POINT BOUNDARY (§3): the last whole code
			// point that fits within the bound
			keep = textBoundary(r.buf[r.off:r.off+int(n)], bound)
			r.report.Clamped++
		}
		fv.Cell.Str = append([]byte(nil), r.buf[r.off:r.off+keep]...)
		fv.Count = keep
		r.off += int(n)
		return true
	case f.Type.Kind == ir.TWString:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false
		}
		payload := r.buf[r.off : r.off+int(n)]
		r.off += int(n)
		if n&1 != 0 {
			// AN ODD L IS FRAMING DAMAGE on the body that carries it (§3): the
			// value is L / 2 code units, so an odd L is not a wstring at any
			// length. The field reads its declared default and the parent
			// reads on past L.
			r.report.Malformed = true
			fv.Cell = r.m.FieldDefaultCell(f)
			fv.Count = len(fv.Cell.Units)
			return true
		}
		units := wideUnits(payload)
		if !wideValid(units) {
			// ILL-FORMED TEXT IS DAMAGE (§3, §4): an unpaired surrogate or a
			// zero code unit reads the declared default, one malformed counts
			r.report.Malformed = true
			fv.Cell = r.m.FieldDefaultCell(f)
			fv.Count = len(fv.Cell.Units)
			return true
		}
		keep := len(units)
		if bound := int(f.Type.Size); keep > bound {
			// A CLAMP NEVER SPLITS A PAIR (§3)
			keep = wideBoundary(units, bound)
			r.report.Clamped++
		}
		fv.Cell.Units = units[:keep]
		fv.Count = keep
		return true
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		return r.array(fv)
	case tabletext.UnionOf(f) != nil:
		return r.union(fv)
	case tabletext.EnumOf(f) != nil:
		return r.enumCell(&fv.Cell, f, false)
	case tabletext.StructOf(f) != nil:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false
		}
		sub := r.sub(int(n))
		// THE BODY IS A MEMBER of its parent in every generated target, so
		// storage already standing is reset rather than replaced.
		if fv.Cell.Tab == nil {
			fv.Cell.Tab = r.m.New(tabletext.StructOf(f))
		} else {
			r.m.Refill(fv.Cell.Tab)
		}
		sub.bodyAt(fv.Cell.Tab, true)
		if r.countRefused() {
			return false
		}
		// A BODY'S TERMINATOR IS THE END OF ITS PAYLOAD (§3): a body whose
		// terminator is not the last byte of its L is framing damage — the
		// payload stops, the field reads its declared defaults, and the
		// enclosing body continues past it by L.
		if sub.off != int(n) {
			r.report.Malformed = true
			r.m.Refill(fv.Cell.Tab)
		}
		r.off += int(n)
		return true
	}
	return r.scalarAt(&fv.Cell, f, ir.TableScalarKind(f))
}

// enumCell reads an enum value: kind 30's payload is the reference to the
// VARIANT NAME's id, `0` for None. A reference this reader's enum cannot name
// is §4's ordinary `unknown` — the field reads None and one event counts — and
// a reference ABOVE the entry count is framing damage like any other (§3).
func (r *wireReader) enumCell(cell *tabletext.Cell, f *ir.Field, element bool) bool {
	ref, ok := r.leb()
	if !ok {
		r.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0 // the zero reference is the enum's None
		return true
	}
	id, named := r.id(ref)
	if !named {
		r.report.Malformed = true
		return false
	}
	en := tabletext.EnumOf(f)
	v := enumValueForId(en, id)
	if v < 0 {
		cell.U = 0
		r.report.Unknown++
		if r.rt != nil {
			// AN UNKNOWN ENUM VARIANT REFERENCE IS AN EXCLUDED CLASS
			// (docs/SPEC-TABLES.md §6.6): the FIELD is the reader's, and the
			// reader writes its own value under that id, so a retained copy
			// would be a second occurrence of an id the reader already wrote.
			r.rt.lost(r.report)
		}
		return true
	}
	if tabletext.VariantDeprecated(en, v) {
		// A DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, and an
		// element as the ELEMENT's default; it is never unknown (docs/SPEC-TABLES.md
		// §2.15). The ordinal and name hash stay, so the id still names it.
		if element {
			*cell = tabletext.Cell{}
		} else {
			*cell = r.m.FieldDefaultCell(f)
		}
		return true
	}
	cell.U = uint64(v)
	return true
}

// mapField decodes one map (docs/SPEC-TABLES.md §2.8), and it is where every
// reader rule the section states lands: the key BEFORE the slot is chosen,
// ascending against the key of the last entry that LANDED, a repeated or a
// descending key flagging `malformed` (damage, §4), a key past the reader's bound dropping
// its entry and counting `clamped`, and a key KIND the reader does not declare
// emptying the map for one `kind_mismatch`.
func (r *wireReader) mapField(fv *tabletext.Field) bool {
	f := fv.Def
	n, ok := r.leb()
	if !ok || n > uint64(len(r.buf)) || !r.has(int(n)) {
		r.report.Malformed = true
		return false
	}
	bodyLen := int(n)
	end := r.off + bodyLen
	// THE HEADER LIES INSIDE THE MAP'S L (docs/SPEC-TABLES.md §3): a body too
	// short to carry the element kind and the count, and a count whose
	// spelling runs past L, are damage, because no writer produces either.
	if bodyLen < 2 {
		r.report.Malformed = true
		return false
	}
	{
		elemKind := r.u8()
		count, good := r.leb()
		if !good || r.off > end {
			r.report.Malformed = true
			return false
		}
		// A MAP HEADER WHOSE ELEMENT KIND IS NOT 13 is the ordinary array kind
		// mismatch of §4, and nothing about a map is special-cased
		if int(elemKind) != ir.TableKindTable {
			r.report.KindMismatch++
			r.off = end
			return true
		}
		if count > uint64(math.MaxInt32) {
			r.off = end
			return r.collectionCap()
		}
		sub := r.subTo(end)
		var last tabletext.MapKey
		landed := false
		widened := false
		for range count {
			elemLen, good := sub.leb()
			if !good || elemLen > uint64(len(sub.buf)) || !sub.has(int(elemLen)) {
				r.report.Malformed = true
				break
			}
			body := sub.sub(int(elemLen))
			sub.off += int(elemLen)
			entry := r.m.NewMapEntry(f)
			kindBad := false
			entryWidened := false
			key, keyOk := body.mapKey(f, entry, &kindBad, &entryWidened)
			if entryWidened && !widened {
				// the MAP counts one `widened`, at the first key the
				// declaration widens (§2.8, §4)
				widened = true
				r.report.Widened++
			}
			if kindBad {
				// A MAP WITH HALF ITS KEYS IS NOT A MAP (§2.8): the map resets
				// to EMPTY, ONE kind_mismatch counts for it, and the rest is
				// skipped. Events counted inside earlier entries stand.
				r.report.KindMismatch++
				fv.Entries = nil
				break
			}
			if !keyOk {
				r.report.Malformed = true
				break
			}
			if !tabletext.MapKeyFits(f, key) {
				// KEYS NEVER CLAMP: the entry is dropped whole, one count per
				// entry, and the rest of the map lands normally
				r.report.Clamped++
				continue
			}
			order := -1
			if landed {
				order = tabletext.MapKeyOrder(f, last, key)
			}
			if order >= 0 {
				// DESCENDING or EQUAL: not a body any conforming writer
				// produced, so it is damage (§2.8, §4). A repeated key is the
				// EQUAL case: a writer puts each key down once.
				r.report.Malformed = true
				break
			}
			// the entry's body is read WHOLE now the key has chosen its place
			whole := body.sub(0)
			whole.buf = body.buf
			whole.off = 0
			decoded := r.m.NewMapEntry(f)
			whole.bodyAt(decoded, true)
			if r.countRefused() {
				return false
			}
			fv.Entries = append(fv.Entries, tabletext.Cell{Tab: decoded})
			last = tabletext.MapKeyOf(f, decoded)
			landed = true
		}
	}
	r.off = end // the remaining entries skip by the map's L
	return true
}

// collectionCap matches TableListFillBegin and TableMapFillBegin: builder storage refuses the
// partial value, while a region read reports damage and resumes after L.
func (r *wireReader) collectionCap() bool {
	if r.region || r.countRefusal == nil {
		r.report.Malformed = true
		return true
	}
	*r.countRefusal = true
	return false
}

// mapKey is the KEY SCAN (docs/SPEC-TABLES.md §2.8): the reader does not
// assume where the key sits, so before an entry's body decodes it scans that
// body's field headers for the key's id and reads the key. A body carrying
// the key's id twice is damage (§3); a body with no key field at all has the
// key's declared default.
//
// THE KEY KIND IS THE READER'S DECLARATION, never the first entry's: a key
// under another kind desynchronizes the rest of the scan, and the honest
// answer is the KIND rather than the framing damage that follows from it.
func (r *wireReader) mapKey(f *ir.Field, entry *tabletext.Instance, kindBad, widened *bool) (tabletext.MapKey, bool) {
	keyField := ir.MapKeyField(f)
	want := ir.TableWireScalarKind(keyField)
	key := tabletext.MapKeyOf(f, entry)
	scan := &wireReader{buf: r.buf, report: &tabletext.Report{}, m: r.m, ids: r.ids, st: r.st}
	keySeen := false
	for {
		ref, ok := scan.leb()
		if !ok {
			return key, false
		}
		if ref == 0 {
			return key, scan.off == len(scan.buf) // the terminator must consume the entry's L
		}
		id, named := scan.id(ref)
		if !named || !scan.has(1) {
			return key, false
		}
		kind := scan.u8()
		if id == ir.MapKeyWireId {
			if keySeen {
				// the key's id twice in one entry body: damage (§3, §4)
				return key, false
			}
			keySeen = true
			cell := &entry.Fields[0].Cell
			if int(kind) != want && ir.TableKindWidens(int(kind), want) {
				*widened = true
				if !scan.scalarAt(cell, keyField, int(kind)) {
					return key, false
				}
				key = tabletext.MapKeyOf(f, entry)
				continue
			}
			*kindBad = int(kind) != want
			if *kindBad {
				if !scan.skip(kind) {
					return key, false
				}
				continue
			}

			if !scan.scalarOrString(cell, keyField) {
				return key, false
			}
			key = tabletext.MapKeyOf(f, entry)
			continue
		}
		if !scan.skip(kind) {
			return key, false
		}
	}
}

// scalarOrString reads one key value: a `string(N)`'s length and bytes, or a
// scalar at its own width. A key is one or the other (§2.8).
func (r *wireReader) scalarOrString(cell *tabletext.Cell, f *ir.Field) bool {
	if f.Type.Kind == ir.TString {
		n, ok := r.leb()
		if !ok || n > uint64(len(r.buf)) || !r.has(int(n)) {
			return false
		}
		if !textValid(r.buf[r.off : r.off+int(n)]) {
			return false // a key a string value would refuse as malformed makes the MAP malformed (§2.8)
		}
		cell.Str = append([]byte(nil), r.buf[r.off:r.off+int(n)]...)
		r.off += int(n)
		return true
	}
	return r.scalarAt(cell, f, ir.TableScalarKind(f))
}

func (r *wireReader) array(fv *tabletext.Field) bool {
	ok, _ := r.arrayBody(fv, -1)
	return ok
}

// arrayBody decodes an array payload. A FIELD carries its own length first; an
// ARM does not — the arm's L already framed it (docs/SPEC-TABLES.md §2.6,
// §3), so the caller passes the length it was framed by.
//
// `ok` is false where the framing is damaged and the walk stops. `selected` is
// false where the body declares a DIFFERENT ELEMENT KIND: the payload is not
// this array's, so a field keeps its declared default and an ARM holding it is
// not selected — the union reads None (§3).
func (r *wireReader) arrayBody(fv *tabletext.Field, framed int) (ok, selected bool) {
	f := fv.Def
	bound := int(f.ArrayBound)
	if f.Type.Kind == ir.TBytes {
		bound = int(f.Type.Size)
	}
	counted := f.Type.Kind == ir.TBytes || f.CountedOnWire()
	bodyLen := framed
	if framed < 0 {
		n, good := r.leb()
		if !good || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false, false
		}
		bodyLen = int(n)
	}
	if !r.has(bodyLen) {
		r.report.Malformed = true
		return false, false
	}
	end := r.off + bodyLen
	// THE HEADER LIES INSIDE THE BODY'S L (docs/SPEC-TABLES.md §3, §4;
	// tla/Framing.tla, HeaderInsideBody): the element kind byte and the count.
	// A body too short to carry them, and a count whose spelling runs past L or
	// cannot complete, are damage, because no writer produces either.
	if bodyLen < 2 {
		r.report.Malformed = true
		return false, false
	}
	{
		decoded := 0
		ek := r.u8()
		count, good := r.leb()
		if !good || r.off > end {
			r.report.Malformed = true
			return false, false
		}
		if int(ek) != ir.TableWireElemKind(f) {
			if !ir.TableKindWidens(int(ek), ir.TableWireElemKind(f)) {
				// the element kind is part of the array's identity (§3): counted,
				// the field left at its declared default — for an OPTIONAL array
				// that default is ABSENT, so Present is not set below. AN ELEMENT
				// KIND OF 31 OR 32 TAKES THAT SAME RULE AND NO OTHER.
				r.report.KindMismatch++
				r.off = end
				return true, false
			}
			// WIDENED at an element kind (§4): every element decodes at the
			// wire kind's width, and ONE `widened` counts for the field
			// however many elements it holds
			r.report.Widened++
		}
		keep := int(count)
		switch {
		case f.Array == ir.ArrayList:
			// Replacement empties the holder even when its count cannot land.
			fv.Elems = fv.Elems[:0]
			if count > uint64(math.MaxInt32) {
				r.off = end
				return r.collectionCap(), false
			}
			// Slots grow one at a time against the body's own length.

		case count > uint64(bound):
			keep = bound
			r.report.Clamped++
			if f.IsTextList() && r.report.Reason == "" {
				// a text-list COUNT clamp names the field (§2.13), as the Go leg does
				r.report.Reason = "clamped: " + f.TextListElem().MapEntryOf
			}
		}
		// elements are BOUNDED by the field body: a count the length cannot
		// cover keeps the decoded prefix, flags malformed, and the parent
		// continues at the next field — a neighbour's bytes are never
		// fabricated into elements (§4)
		sub := r.subTo(end)
		if f.Type.Kind == ir.TBytes {
			payload := make([]byte, 0, keep)
			for i := 0; i < keep; i++ {
				if !sub.has(1) {
					r.report.Malformed = true
					break
				}
				payload = append(payload, sub.u8())
				decoded = i + 1
			}
			fv.Cell.Str = payload
		} else {
			for i := 0; i < keep; i++ {
				if f.Array == ir.ArrayList {
					fv.Elems = append(fv.Elems, r.m.ElementZero(f))
				}
				if !sub.element(fv, i, int(ek)) {
					break
				}
				decoded = i + 1
			}
			if f.Array == ir.ArrayList {
				fv.Elems = fv.Elems[:decoded] // the prefix the body covered
			}
			decoded = r.textTotal(fv, decoded)
		}
		if counted {
			// the slots past the decoded count hold the value-initialized
			// element (§7.2)
			if f.Array == ir.ArrayCounted {
				for i := decoded; i < fv.Count; i++ {
					fv.Elems[i] = r.m.ElementZero(f)
				}
			}
			fv.Count = decoded
		}
	}
	if f.Type.Optional {
		// the field rode with its own element kind, so it is PRESENT (§2.3);
		// the element-kind mismatch above returned before this line
		fv.Present = true
	}
	r.off = end // excess elements and slack skip via the length
	return true, true
}

func (r *wireReader) element(fv *tabletext.Field, i int, ek int) bool {
	f := fv.Def
	if ir.TextListPlain(f) {
		// A PLAIN TEXT LIST's element is kind 12: its own L, then L bytes
		// (docs/SPEC-TABLES.md §2.13, §3).
		return r.textListElement(fv, i)
	}
	if tabletext.UnionOf(f) != nil {
		// an element of an ARRAY OF UNIONS is an ARM HEADER and carries its own
		// kind, so the arm rules apply once per element (§3)
		if !r.has(1) {
			r.report.Malformed = true
			return false
		}
		fv.Elems[i].U = 0
		fv.Elems[i].Tab = nil
		return r.unionCell(&fv.Elems[i], fv)
	}
	if f.Type.Pointer {
		// an element of an array of pointers: a node index, bounds-checked and
		// stored, never followed (§3.1)
		index, ok := r.leb()
		if !ok {
			r.report.Malformed = true
			return false
		}
		r.resolveCell(&fv.Elems[i], f, index)
		return true
	}
	if tabletext.EnumOf(f) != nil {
		return r.enumCell(&fv.Elems[i], f, true)
	}
	if tabletext.StructOf(f) != nil {
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			r.report.Malformed = true
			return false
		}
		sub := r.sub(int(n))
		if fv.Elems[i].Tab == nil {
			fv.Elems[i].Tab = r.m.New(tabletext.StructOf(f))
		} else {
			r.m.Refill(fv.Elems[i].Tab)
		}
		sub.bodyAt(fv.Elems[i].Tab, true)
		if r.countRefused() {
			return false
		}
		if sub.off != int(n) {
			r.report.Malformed = true
			r.m.Refill(fv.Elems[i].Tab)
		}
		r.off += int(n)
		return true
	}
	// a scalar element decodes at the ELEMENT KIND the header carried, which
	// is the declared kind or one it widens (§4)
	return r.scalarAt(&fv.Elems[i], f, ek)
}

// textTotal holds a text list's `| total = N` on the read (docs/SPEC-TABLES.md
// §2.13): it keeps the longest PREFIX of the decoded elements whose byte
// lengths sum within N, each element already clamped at its M, drops the first
// element that would carry the sum past N and every element after it, and
// counts `clamped` ONCE for the field, naming it. A count clamped against
// `[..N]` above is counted apart. It returns the count the field keeps.
func (r *wireReader) textTotal(fv *tabletext.Field, decoded int) int {
	f := fv.Def
	if f.TextTotal <= 0 || !f.IsTextList() {
		return decoded
	}
	var sum int64
	keep := decoded
	for i := range decoded {
		if rec := fv.Elems[i].Tab; rec != nil {
			sum += int64(len(rec.Fields[0].Cell.Str))
		}
		if sum > f.TextTotal {
			keep = i
			break
		}
	}
	if keep == decoded {
		return decoded
	}
	r.report.Clamped++
	if r.report.Reason == "" {
		r.report.Reason = "clamped: " + f.TextListElem().MapEntryOf
	}
	if f.Array == ir.ArrayList {
		fv.Elems = fv.Elems[:keep]
	} else {
		for i := keep; i < decoded; i++ {
			fv.Elems[i] = r.m.ElementZero(f)
		}
	}
	return keep
}

// textListElement reads one element of a PLAIN text list (docs/SPEC-TABLES.md
// §2.13, §3): its own L, then L bytes of well-formed UTF-8 with no zero byte,
// clamped at M on a code point boundary and counted. The value lands in the
// element record's one field.
func (r *wireReader) textListElement(fv *tabletext.Field, i int) bool {
	f := fv.Def
	n, ok := r.leb()
	if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
		r.report.Malformed = true
		return false
	}
	if !textValid(r.buf[r.off : r.off+int(n)]) {
		// ill-formed UTF-8 or a zero byte in an element is malformed (§2.13)
		r.report.Malformed = true
		return false
	}
	keep := int(n)
	if bound := int(ir.TextListValue(f).Type.Size); keep > bound {
		keep = textBoundary(r.buf[r.off:r.off+int(n)], bound)
		r.report.Clamped++
		if r.report.Reason == "" {
			// a text-list ELEMENT clamp names the field (§2.13), as the Go leg does
			r.report.Reason = "clamped: " + f.TextListElem().MapEntryOf
		}
	}
	rec := fv.Elems[i].Tab
	if rec == nil {
		rec = r.m.New(f.TextListElem())
		fv.Elems[i].Tab = rec
	}
	vc := &rec.Fields[0]
	vc.Cell.Str = append([]byte(nil), r.buf[r.off:r.off+keep]...)
	vc.Count = keep
	r.off += int(n)
	return true
}

// keyed places an enum-keyed array's triples BY KEY REFERENCE, so a slot lands
// by name however the enum moved (docs/SPEC-TABLES.md §3.2). A key this reader
// cannot name is skipped by its length and counted unknown; a slot the writer
// never sent keeps its declared default; a stored key reference of `0` is
// MALFORMED rather than an unknown variant, because `0` names no id at all and
// a slot must say which variant it keys.
func (r *wireReader) keyed(fv *tabletext.Field) bool {
	f := fv.Def
	n, ok := r.leb()
	if !ok || n > uint64(len(r.buf)) {
		r.report.Malformed = true
		return false
	}
	bodyLen := int(n)
	if !r.has(bodyLen) {
		r.report.Malformed = true
		return false
	}
	end := r.off + bodyLen
	// the header lies inside L, as for every array body (§3, §4)
	if bodyLen < 2 {
		r.report.Malformed = true
		return false
	}
	elemKind := r.u8()
	count, good := r.leb()
	if !good || r.off > end {
		r.report.Malformed = true
		return false
	}
	if int(elemKind) != ir.TableWireElemKind(f) {
		if !ir.TableKindWidens(int(elemKind), ir.TableWireElemKind(f)) {
			r.report.KindMismatch++
			r.off = end
			return true
		}
		// WIDENED at a keyed body's element kind (§4): one count for the
		// field, every slot decoded at the wire kind's width
		r.report.Widened++
	}
	sub := r.subTo(end)
	// ONE SLOT KEY, ONCE PER BODY (§3.2, §4): a writer puts each slot down once
	seen := map[uint64]bool{}
	for range count {
		key, good := sub.leb()
		if !good {
			r.report.Malformed = true
			break
		}
		if key == 0 {
			// None is the enum's null and keys no slot, and `0` names no id at
			// all, so a body carrying one is damaged rather than merely
			// foreign: the read stops this body and keeps what it decoded (§3.2)
			r.report.Malformed = true
			break
		}
		keyID, named := sub.id(key)
		if !named || seen[key] {
			r.report.Malformed = true
			break
		}
		seen[key] = true
		elemLen, good := sub.leb()
		if !good || elemLen > uint64(len(sub.buf)) || !sub.has(int(elemLen)) {
			r.report.Malformed = true
			break
		}
		slot := tabletext.KeyedValueSlot(f, enumValueForId(f.KeyEnumRef, keyID))
		if slot < 0 {
			r.report.Unknown++ // a slot this reader cannot name
			if r.rt != nil {
				// AN UNKNOWN KEYED-ARRAY SLOT IS AN EXCLUDED CLASS
				// (docs/SPEC-TABLES.md §6.6): a slot is not a field, the
				// reader rewrites the array body whole, and a slot has
				// nowhere to append to.
				r.rt.lost(r.report)
			}
			sub.off += int(elemLen)
			continue
		}
		elem := sub.sub(int(elemLen))
		switch {
		case tabletext.EnumOf(f) != nil:
			elem.enumCell(&fv.Elems[slot], f, true)
		case tabletext.StructOf(f) != nil:
			if fv.Elems[slot].Tab == nil {
				fv.Elems[slot].Tab = r.m.New(tabletext.StructOf(f))
			} else {
				r.m.Refill(fv.Elems[slot].Tab)
			}
			elem.bodyAt(fv.Elems[slot].Tab, true)
			if r.countRefused() {
				return false
			}
			if elem.off != int(elemLen) {
				r.report.Malformed = true
				r.m.Refill(fv.Elems[slot].Tab)
			}
		default:
			elem.scalarAt(&fv.Elems[slot], f, int(elemKind))
		}
		sub.off += int(elemLen)
	}
	r.off = end // unread triples and slack skip via the length
	return true
}

func (r *wireReader) union(fv *tabletext.Field) bool { return r.unionCell(&fv.Cell, fv) }

// unionCell decodes the union framing into one cell — a field's, or an element
// of an array of unions (§3). AN ARM HEADER IS A FIELD HEADER: the arm id
// reference, the arm's KIND byte, `L`, then `L` bytes of arm payload.
func (r *wireReader) unionCell(cell *tabletext.Cell, fv *tabletext.Field) bool {
	f := fv.Def
	un := tabletext.UnionOf(f)
	ref, ok := r.leb()
	if !ok {
		r.report.Malformed = true
		return false
	}
	if ref == 0 {
		cell.U = 0
		cell.Tab = nil
		cell.Arm = nil
		return true // empty: the id is the whole payload, not even a kind byte
	}
	armID, named := r.id(ref)
	if !named {
		r.report.Malformed = true
		return false
	}
	if !r.has(1) {
		r.report.Malformed = true
		return false
	}
	kind := r.u8()
	n, good := r.leb()
	if !good || n > uint64(len(r.buf)) || !r.has(int(n)) {
		r.report.Malformed = true
		return false
	}
	length := int(n)
	sub := r.sub(length)
	tag := 0
	for i, v := range un.Variants {
		if ir.TableWireId(v.WireName()) == armID {
			tag = i + 1
			break
		}
	}
	switch {
	case tag == 0:
		// an arm this reader cannot name: the value reads EMPTY and the body
		// is skipped by its length, never misdecoded.
		cell.U = 0
		cell.Tab = nil
		cell.Arm = nil
		r.report.Unknown++
		if r.rt != nil {
			// AN UNKNOWN UNION ARM ID IS AN EXCLUDED CLASS
			// (docs/SPEC-TABLES.md §6.6): the same as an unknown enum
			// variant, one level in, because the union FIELD is the
			// reader's.
			r.rt.lost(r.report)
		}
	case int(kind) != armWireKind(un.Variants[tag-1]):
		cell.U = 0
		cell.Tab = nil
		cell.Arm = nil
		arm := un.Variants[tag-1]
		if ir.TableKindWidens(int(kind), armWireKind(arm)) {
			// WIDENED AT AN ARM (§4): the arm's kind byte is read and its
			// payload DECODED at its own width rather than skipped. An `L`
			// that is not that width is the arm's own framing damage (§3).
			if length != ir.TableKindWidth(int(kind)) {
				r.report.Malformed = true
				break
			}
			fv := r.m.NewArm(arm)
			if sub.scalarAt(&fv.Cell, arm.F, int(kind)) {
				cell.U = uint64(tag)
				cell.Arm = fv
				r.report.Widened++
			}
			break
		}
		// A RETYPED ARM IS JUDGED BY THE FIELD RULES: an arm arriving under a
		// kind the reader does not declare for it is a KIND MISMATCH — the arm
		// skips by L, the union reads None, and the parent reads on (§3).
		r.report.KindMismatch++
	default:
		arm := un.Variants[tag-1]
		if arm.Body() {
			payload := r.m.New(arm.Ref)
			sub.bodyAt(payload, true)
			if r.countRefused() {
				return false
			}
			if sub.off != length {
				// a body whose terminator is not the last byte of its L is
				// framing damage: the union reads None and the enclosing body
				// continues past the arm by L (§3)
				r.report.Malformed = true
				cell.U = 0
				cell.Tab = nil
			} else {
				cell.U = uint64(tag)
				cell.Tab = payload
			}
		} else {
			cell.Tab = nil
			if !sub.arm(cell, arm, tag, length) {
				cell.U = 0
				cell.Arm = nil
			}
		}
	}
	r.off += length
	return true
}

// arm decodes ONE ARM's payload out of a reader bounded to the arm's `L`
// (docs/SPEC-TABLES.md §3's arm payload table). The kind byte already agreed,
// so what is left to check is the LENGTH: a fixed-width arm whose `L` is not
// its kind's width, a reference-shaped arm whose `L` is not the byte count of
// the reference it frames, and a length-shaped arm damaged inside its `L` are
// that arm's own framing damage — the union reads None, `malformed` counts,
// and the parent reads on past `L`.
func (r *wireReader) arm(cell *tabletext.Cell, arm ir.UnionVariant, tag, n int) bool {
	if arm.Void() {
		if n != 0 {
			// a payload-free arm carries nothing: an L that is not zero is
			// that arm's own framing damage (§3)
			r.report.Malformed = true
			return false
		}
		cell.U = uint64(tag)
		cell.Arm = nil
		return true
	}
	f := arm.F
	if w := ir.ArmWireFixedWidth(f); w > 0 && n != w {
		r.report.Malformed = true
		return false
	}
	fv := r.m.NewArm(arm)
	ok := true
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index, good := r.leb()
		if !good || r.off != n {
			r.report.Malformed = true
			return false
		}
		r.resolveCell(&fv.Cell, f, index)
	case f.Type.Kind == ir.TString:
		if !textValid(r.buf[r.off : r.off+n]) {
			// ILL-FORMED TEXT at an arm: the union reads None, one malformed
			// counts, and the parent reads on past L (§3)
			r.report.Malformed = true
			return false
		}
		keep := n
		if bound := int(f.Type.Size); keep > bound {
			keep = textBoundary(r.buf[r.off:r.off+n], bound)
			r.report.Clamped++
		}
		fv.Cell.Str = append([]byte(nil), r.buf[r.off:r.off+keep]...)
		fv.Count = keep
		r.off += n
	case f.Type.Kind == ir.TWString:
		if n&1 != 0 {
			// an ODD L is that arm's own framing damage: the union reads None
			r.report.Malformed = true
			return false
		}
		units := wideUnits(r.buf[r.off : r.off+n])
		if !wideValid(units) {
			// ILL-FORMED TEXT at an arm: the union reads None, one malformed
			// counts, and the parent reads on past L (§3)
			r.report.Malformed = true
			return false
		}
		keep := len(units)
		if bound := int(f.Type.Size); keep > bound {
			keep = wideBoundary(units, bound)
			r.report.Clamped++
		}
		fv.Cell.Units = units[:keep]
		fv.Count = keep
		r.off += n
	case f.Type.Kind == ir.TBytes, f.Array != ir.ArrayNone:
		var selected bool
		ok, selected = r.arrayBody(fv, n)
		if ok && !selected {
			// the payload declares another array's element kind: the arm is
			// not this one, so the union reads None and the parent reads on
			// past L, exactly as a field's element-kind mismatch does (§3)
			return false
		}
	case tabletext.UnionOf(f) != nil:
		ok = r.unionCell(&fv.Cell, fv, -1)
	case tabletext.EnumOf(f) != nil:
		ok = r.enumCell(&fv.Cell, f, false)
		if ok && r.off != n {
			r.report.Malformed = true
			return false
		}
	default:
		ok = r.scalarAt(&fv.Cell, f, ir.TableScalarKind(f))
	}
	if !ok {
		return false
	}
	cell.U = uint64(tag)
	cell.Arm = fv
	return true
}

// scalarAt decodes one fixed-width value that arrived under `kind`: the
// field's own kind, or one the field's kind WIDENS (docs/SPEC-TABLES.md §4).
// The payload is read at the WIRE kind's width and signedness, sign-extended
// on the signed ladder and zero-extended on the unsigned one, and then takes
// the field's own clamps exactly as a payload at the declared width does, so
// the value is the writer's own and the reader's range still rules.
func (r *wireReader) scalarAt(cell *tabletext.Cell, f *ir.Field, kind int) bool {
	declared := ir.TableScalarKind(f)
	width := ir.TableKindWidth(kind)
	if !r.has(width) {
		r.report.Malformed = true
		return false
	}
	switch kind {
	case ir.TableKindBool:
		b := r.u8()
		if b > 1 {
			r.report.Malformed = true
			if r.report.Reason == "" {
				r.report.Reason = "malformed: " + f.Name
			}
		}
		cell.B = b != 0
		return true
	case ir.TableKindF32:
		bits := r.u32()
		if !tabletext.FiniteF32(bits) {
			// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS, before the bits
			// become a value and before the field is written
			// (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "same rule as
			// packets"): every read refuses it as non_finite_float, a compressed
			// float included, whose test runs before its bounds. The walk goes
			// on so the framing verdict is the one a finite wire gets, and the
			// read answers the refusal at its end (refusedNonFinite).
			r.report.RefuseNonFinite(FloatOwner(r.m, f))
			return true
		}
		v := float64(math.Float32frombits(bits))
		if declared == ir.TableKindF64 {
			// f32 WIDENED into f64 (§4): every finite float32 value is exactly
			// representable, and a float64 field's declared range clamps
			// nothing on this wire, exactly as a payload at eight bytes
			cell.F = v
			return true
		}
		if f.HasFloatRange {
			switch {
			case v < f.FMin:
				v = f.FMin
				r.report.Clamped++
			case v > f.FMax:
				v = f.FMax
				r.report.Clamped++
			case v == 0:
				v = 0 // a -0 lands as +0, uncounted (§3.3, the special values)
			}
		}
		cell.F = float64(float32(v))
		return true
	case ir.TableKindF64:
		bits := r.u64()
		if !tabletext.FiniteF64(bits) {
			r.report.RefuseNonFinite(FloatOwner(r.m, f))
			return true
		}
		cell.F = math.Float64frombits(bits)
		return true
	}
	if ir.TableKindWide(kind) {
		raw := tabletext.WideFromBytes(r.buf[r.off:r.off+width], kind)
		r.off += width
		// the declared range on the RAW scale — a fixed field's whole-unit
		// bounds shifted by F — clamps and counts as every bounded scalar does (§4)
		clamped := false
		cell.Wide, clamped = tabletext.WideClamp(raw, f)
		if clamped {
			r.report.Clamped++
		}
		return true
	}
	signed := ir.TableKindSigned(kind)
	var raw uint64
	switch width {
	case 1:
		raw = uint64(r.u8())
	case 2:
		raw = uint64(binary.LittleEndian.Uint16(r.buf[r.off:]))
		r.off += 2
	case 4:
		raw = uint64(r.u32())
	default:
		raw = r.u64()
	}
	value := int64(raw)
	if signed && width < 8 {
		shift := uint(64 - width*8)
		value = int64(raw<<shift) >> shift
	}
	if ir.TableKindWide(declared) {
		// an integer kind WIDENED into a 128-bit declaration (§4): sixteen
		// bytes extended from the value, then the declared range on the raw
		// scale, exactly as a payload at sixteen bytes takes it
		var wide [16]byte
		binary.LittleEndian.PutUint64(wide[:8], uint64(value))
		if signed && value < 0 {
			for i := 8; i < 16; i++ {
				wide[i] = 0xFF
			}
		}
		clamped := false
		cell.Wide, clamped = tabletext.WideClamp(tabletext.WideFromBytes(wide[:], declared), f)
		if clamped {
			r.report.Clamped++
		}
		return true
	}
	if f.HasIntRange {
		lo, hi := bigInt64(f.IntMin), bigInt64(f.IntMax)
		if signed {
			if value < lo {
				value = lo
				r.report.Clamped++
			} else if value > hi {
				value = hi
				r.report.Clamped++
			}
		} else {
			if raw < uint64(lo) {
				raw = uint64(lo)
				r.report.Clamped++
			} else if raw > uint64(hi) {
				raw = uint64(hi)
				r.report.Clamped++
			}
			value = int64(raw)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		max := uint64(1)<<uint(f.Type.Width) - 1
		if uint64(value) > max {
			value = int64(max)
			r.report.Clamped++
		}
	}
	cell.I = value
	cell.U = uint64(value)
	// raw was already zero-extended when read. A declared range may clamp a
	// widened value above the source width; masking here would truncate the
	// result back to the old width (u8 255 -> u64 min 1000 became 232).
	return true
}

// enumValueForId is the declaration-side value an id names, -1 when no variant
// does.
func enumValueForId(e *ir.Enum, id uint64) int64 {
	for i := range e.Variants {
		if ir.TableWireId(e.VariantWireName(i)) == id {
			return int64(i + 1)
		}
	}
	return -1
}

func bigInt64(v *big.Int) int64 {
	if v == nil {
		return 0
	}
	return v.Int64()
}

// CountRefusal is the file reader's builder-path storage refusal (§2.9).
// The caller must discard the partial value. Earlier report events stand;
// meeting the cap itself adds no malformed event or other counter.
type CountRefusal struct{}

func (*CountRefusal) Error() string {
	return "collection count exceeds the int32 storage cap; discard the partial value"
}

func (r *wireReader) countRefused() bool { return r.countRefusal != nil && *r.countRefusal }

// NonFiniteRefusal is every read's refusal of a float whose bits are a NaN or
// an infinity (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables go
// finite-only too, same rule as packets"): the tolerant read refuses it as the
// strict one does, so the caller must discard the partial value. Field names
// the first such field, <Table>.<field>, and the report carries the same name.
type NonFiniteRefusal struct{ Field string }

func (e *NonFiniteRefusal) Error() string { return "non_finite_float: " + e.Field }

// refusedNonFinite answers a read whose walk recorded a non-finite float: the
// refusal outranks every other verdict a body could reach after it, because a
// non-finite float is not a value any reader can repair or keep.
func refusedNonFinite(report *tabletext.Report) error {
	if report.NonFiniteFloat == "" {
		return nil
	}
	report.Reason = "non_finite_float: " + report.NonFiniteFloat
	return &NonFiniteRefusal{Field: report.NonFiniteFloat}
}

// FloatOwner names a float field as <Owner>.<field> for the non_finite_float
// refusal (docs/SPEC-TABLES.md §3.3), the owner being the table, type or union
// that declares it — the name the generated readers record. The cook's engine
// (internal/tablecook) names its refusals by it too.
func FloatOwner(m *tabletext.Model, f *ir.Field) string {
	if m != nil && m.Unit != nil {
		for _, set := range []map[string]*ir.Struct{m.Unit.Tables, m.Unit.Structs} {
			for _, st := range set {
				if slices.Contains(st.Fields, f) {
					return st.Name + "." + f.Name
				}
			}
		}
		for _, set := range []map[string]*ir.Union{m.Unit.Unions, m.Unit.TableUnions} {
			for _, u := range set {
				for i := range u.Variants {
					if u.Variants[i].F == f {
						return u.Name + "." + f.Name
					}
				}
			}
		}
	}
	return f.Name
}

// retainedPayloadFinite is the raw-bits test on an unknown entry the read is
// retaining (docs/SPEC-TABLES.md §3.3, §6.6): kind 10's four bytes and kind
// 11's eight, little-endian, and true for every other kind.
func retainedPayloadFinite(kind uint8, payload []byte) bool {
	switch int(kind) {
	case ir.TableKindF32:
		return len(payload) != 4 || tabletext.FiniteF32(binary.LittleEndian.Uint32(payload))
	case ir.TableKindF64:
		return len(payload) != 8 || tabletext.FiniteF64(binary.LittleEndian.Uint64(payload))
	}
	return true
}

// retainedFloatName names a retained unknown float for the refusal: the body's
// table and the field id it cannot name, as <Table>.0x<id>.
func retainedFloatName(inst *tabletext.Instance, id uint64) string {
	return fmt.Sprintf("%s.0x%016x", inst.Def.Name, id)
}
