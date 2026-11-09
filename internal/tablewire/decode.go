// The wire's READ half (docs/SPEC-TABLES.md §4, §6.5): THE ONE READ. A read
// returns the whole value or refuses the whole load. What it accepts is
// evolution and evolution only: an unknown field, variant, arm or keyed slot
// is skipped and counted `unknown` (or retained), an absent field takes its
// default, and a widening decodes exactly and counts `widened`. Everything
// else refuses: damage, a kind mismatch, a value out of range, a count or a
// length past the reader's bound, a missing required field, a non-finite
// float. There is no partial value, no repair, no clamp and no reading past
// damage.
//
// The read runs in two phases. The conditions decidable from the wire alone
// are checked over every byte first (wirecheck.go); then the declaration-
// dependent conditions are checked as the value is decoded. Detection order is
// free (§6.5): tla/ReadOrder.tla's OneVerdict is the property that any order
// gives the same verdict, and this order is the one this engine takes.
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
// not know (docs/SPEC-TABLES.md §3).
type FormRefusal struct{ Form uint8 }

func (e *FormRefusal) Error() string {
	return fmt.Sprintf("newer_form: wire form %d is not form %d, the one this reader carries (docs/SPEC-TABLES.md §3)", e.Form, ir.TableWireForm)
}

// Decode fills one instance from a root table's wire bytes. It answers (true,
// nil) with the value and the read's counters in the report, or (false, err)
// with the refusal: the instance is at its declared defaults, the report says
// Refused with the reason as diagnostic text, and no counter is set.
func Decode(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	return decodeWith(m, inst, data, report, nil)
}

// DecodeVariable is the VARIABLE-form read whatever the root's declared class:
// form 1 even at a declared fixed root, which Decode reads as form 3. It exists
// for the write contract's conformance surface, whose rows load the corpus wire
// and then hand the value to a chosen writer.
func DecodeVariable(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	return decodeWithOrder(m, inst, data, report, nil, readOrder{variable: true})
}

// DecodeRegion is the region read. It refuses exactly what Decode refuses and
// returns the same value where it accepts (docs/SPEC-TABLES.md §6.5, "EVERY
// PATH AGREES ON THE OUTCOME").
func DecodeRegion(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report) (bool, error) {
	return decodeWith(m, inst, data, report, nil)
}

// decodeWith is Decode with RETENTION's state threaded through it
// (docs/SPEC-TABLES.md §6.6). `rt` is nil for every plain read, and retention
// is ADDITIVE: nothing below reads a byte differently or decides a verdict
// differently because retention is on.
func decodeWith(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report, rt *retainState) (bool, error) {
	return decodeWithOrder(m, inst, data, report, rt, readOrder{})
}

func decodeWithOrder(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report, rt *retainState, order readOrder) (bool, error) {
	if report == nil {
		report = &tabletext.Report{}
	}
	err := decodeWalk(m, inst, data, report, rt, order)
	if err != nil {
		// NOTHING THE READ PRODUCED IS DEFINED (§6.5): the instance returns to
		// its declared defaults, the retention store to empty, and the report
		// carries the verdict and the reason alone.
		m.Clear(inst)
		if rt != nil {
			rt.store.reset()
		}
		refuseReport(report, err)
		return false, err
	}
	report.Refused = false
	report.Reason = ""
	return true, nil
}

// readOrder is the order this read detects conditions in. DETECTION ORDER IS
// FREE (docs/SPEC-TABLES.md §6.5): every order gives the same verdict, which is
// tla/ReadOrder.tla's OneVerdict, and oneverdict_test.go holds this engine to
// it by reading the same inputs in other orders. The zero value is the order
// every read takes: the wire-alone conditions first, then the records in index
// order, then the root.
type readOrder struct {
	wireLast bool              // the declaration's conditions before the wire-alone ones
	records  func(n int) []int // the order pass two decodes the node records in
	// variable reads form 1 even at a declared fixed root; DecodeVariable sets
	// it for the write contract's load, whose wire is the corpus's form 1.
	variable bool
}

func decodeWalk(m *tabletext.Model, inst *tabletext.Instance, data []byte, report *tabletext.Report, rt *retainState, order readOrder) error {
	if FixedRoot(m, inst.Def) && !order.variable {
		// A READER THAT EXPECTS A FIXED TABLE REFUSES EVERY OTHER FORM, BY
		// NAME (docs/SPEC-TABLES.md §3.4): form 3 is read, and a form-1 file
		// is previous_form with nothing decoded
		return decodeFixed(m, inst, data, report)
	}
	// THE FORM BYTE IS READ FIRST (§3).
	if len(data) < 1 {
		return &Refusal{Reason: ReasonWireDamaged, Detail: "no form byte"}
	}
	if data[0] != ir.TableWireForm {
		if data[0] == ir.TableWireMessageForm {
			// FORM `2` IS A STREAM FORM AND NEVER A FILE'S OWN FORM
			// (docs/SPEC-TABLES.md §3.3)
			return &MessageRefusal{Reason: ReasonMessageFormAsFile}
		}
		return &FormRefusal{Form: data[0]}
	}
	var valid bool
	data, valid = fileIntegrityBody(inst.Def, data)
	if !valid {
		return &Refusal{Reason: ReasonIntegrityMismatch}
	}
	body, ids, ok := trailer(data)
	if !ok {
		return &Refusal{Reason: ReasonWireDamaged, Detail: "the id table cannot be read whole"}
	}
	// ANY BYTE BETWEEN THE ROOT'S TERMINATOR AND THE TABLE'S FIRST ENTRY IS
	// DAMAGE, because no field claims it (§3).
	if end, terminated := bodyExtent(body, ids); !terminated || end != len(body) {
		return &Refusal{Reason: ReasonWireDamaged, Detail: "the root body does not end at the id table"}
	}
	if order.wireLast {
		if err := decodeValue(m, inst, body, ids, report, rt, order); err != nil {
			return err
		}
		fail, nonFinite := checkFile(body, ids)
		if fail != nil {
			return fail
		}
		return nonFinite
	}
	// PHASE ONE: every condition decidable from the wire alone, over every
	// byte of the file (§6.5).
	fail, nonFinite := checkFile(body, ids)
	if fail != nil {
		return fail
	}
	// PHASE TWO: the declaration-dependent conditions, as the value decodes.
	// A non-finite float the decode reaches is refused there by its field's
	// name; one in bytes this reader cannot name refuses after.
	if err := decodeValue(m, inst, body, ids, report, rt, order); err != nil {
		return err
	}
	return nonFinite
}

// trailer locates the ID TABLE from the END of the wire (docs/SPEC-TABLES.md
// §3): the final eight bytes are the ENTRY COUNT, a fixed little-endian u64,
// the `8 × count` bytes before them are the ENTRIES, and the body ends where
// the first entry begins.
//
// A table that cannot be read whole is damage: fewer than eight bytes in the
// file, a count whose `8 × count + 8` runs past the front, a count that leaves
// no room for the form byte, or ONE ID IN TWO ENTRIES.
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
		// THE ENTRY COUNT HAS A MAXIMUM (§3, THE DISTINCTNESS CHECK)
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
			// A TABLE THAT CARRIES ONE ID TWICE: the whole wire is damage (§3)
			return nil, nil, false
		}
		seen[id] = true
		ids[i] = id
	}
	return data[1:first], ids, true
}

// bodyExtent walks a body's framing to the zero reference that ends it, so the
// caller can tell a body that ENDED EARLY, leaving bytes no field claims.
// `terminated` is false where the walk cannot reach a terminator at all.
func bodyExtent(body []byte, ids []uint64) (end int, terminated bool) {
	r := &wireReader{buf: body, report: &tabletext.Report{}, ids: ids, fail: &readFail{}}
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
	buf    []byte
	off    int
	report *tabletext.Report
	m      *tabletext.Model
	ids    []uint64     // the file's id table, resolved once at open (§3)
	st     *decodeState // the save's numbering, shared by every sub-reader
	// rt is RETENTION's state when the caller opted in (docs/SPEC-TABLES.md
	// §6.6), and nil otherwise.
	rt *retainState
	// fail is the first condition this read met, shared by every sub-reader.
	fail *readFail
}

// refuse records the read's refusal, the first one met, and answers false so
// a walk stops where it stands.
func (r *wireReader) refuse(err error) bool {
	if r.fail == nil {
		r.fail = &readFail{}
	}
	if r.fail.err == nil {
		r.fail.err = err
	}
	return false
}

func (r *wireReader) damage(detail string) bool {
	return r.refuse(&Refusal{Reason: ReasonWireDamaged, Detail: detail})
}

func (r *wireReader) refuseField(reason string, f *ir.Field) bool {
	return r.refuse(&Refusal{Reason: reason, Detail: FloatOwner(r.m, f)})
}

func (r *wireReader) err() error {
	if r.fail == nil {
		return nil
	}
	return r.fail.err
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
// truncated or NON-CANONICAL (§3).
func (r *wireReader) leb() (v uint64, ok bool) {
	v, next, good := readLeb(r.buf, r.off)
	if !good {
		return 0, false
	}
	r.off = next
	return v, true
}

// id resolves one reference against the file's table. ok is false where the
// reference is `0` or ABOVE the entry count.
func (r *wireReader) id(ref uint64) (uint64, bool) {
	if ref == 0 || ref > uint64(len(r.ids)) {
		return 0, false
	}
	return r.ids[ref-1], true
}

func (r *wireReader) sub(n int) *wireReader {
	if r.fail == nil {
		r.fail = &readFail{}
	}
	return &wireReader{buf: r.buf[r.off : r.off+n], report: r.report, m: r.m, ids: r.ids, st: r.st, rt: r.rt, fail: r.fail}
}

// lengthOf reads a length and requires it inside the buffer.
func (r *wireReader) lengthOf() (int, bool) {
	n, ok := r.leb()
	if !ok || n > uint64(len(r.buf)) || !r.has(int(n)) {
		return 0, r.damage("a length past the body carrying it")
	}
	return int(n), true
}

// skip steps past a payload of `kind` by §3's framing rules alone. The wire
// check has already judged every byte it steps over (wirecheck.go), so the
// skip only has to frame.
func (r *wireReader) skip(kind uint8) bool {
	if fixedWidthKind(int(kind)) {
		w := ir.TableKindWidth(int(kind))
		if !r.has(w) {
			return false
		}
		r.off += w
		return true
	}
	switch kind {
	case ir.TableKindPointer, ir.TableKindEnum:
		_, ok := r.leb()
		return ok
	case ir.TableKindString, ir.TableKindTable, ir.TableKindArray, ir.TableKindKeyed,
		ir.TableKindEscape, ir.TableKindNoPayload:
		n, ok := r.leb()
		if !ok || !r.has(int(n)) || n > uint64(len(r.buf)) {
			return false
		}
		r.off += int(n)
		return true
	case ir.TableKindRetiredWstring, ir.TableKindReservedFloat16:
		// KIND 33 IS RETIRED AND KIND 34 IS RESERVED FOR float16; NEITHER IS
		// PART OF THIS MAJOR (§3): no writer emits them and no reader has a
		// rule for them, so a reader meets them only as DAMAGE, exactly as it
		// meets 35 or 200
		return false
	case ir.TableKindUnion:
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
	// KIND 34 (reserved for float16) and every kind outside the closed set
	// are not skippable: framing damage (§3).
	return false
}

// wireKind is the kind byte a field's payload rides under: ir.TableWireFieldKind
// with the one case that function does not see, a POINTER field, which rides
// as a NODE INDEX under its own kind `17` (docs/SPEC-TABLES.md §3.1).
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

// bodyAt decodes one table body. The wire check has already refused a
// repeated field id, a reserved id out of place and every framing fault, so
// what is left here is the declaration: a kind that is not the declared one
// and not a widening refuses, and a required field the body lacks refuses.
func (r *wireReader) bodyAt(inst *tabletext.Instance, nested bool) bool {
	seen := make([]bool, len(inst.Fields))
	index := map[uint64]int{}
	for i := range inst.Fields {
		index[ir.TableFieldWireId(inst.Fields[i].Def)] = i
	}
	for {
		if r.err() != nil {
			return false
		}
		ref, ok := r.leb()
		if !ok {
			return r.damage("a field reference that is truncated or not canonical")
		}
		if ref == 0 {
			if missing := tabletext.MissingFields(inst, func(i int) bool { return seen[i] }); missing != "" {
				return r.refuse(&tabletext.MissingRequired{Reason: "missing_required: " + missing})
			}
			return true
		}
		id, named := r.id(ref)
		if !named || !r.has(1) {
			return r.damage("a field header that names no id")
		}
		kind := r.u8()
		switch id {
		case ir.TableBuildVersionWireId, ir.TableMessageVocabularyWireId:
			return r.damage("a reserved announcement id in a file body")
		case ir.TableNodeWireId:
			if nested {
				return r.damage("the node table's reserved id in a nested body")
			}
			// THE NODE TABLE'S OWN ID IS KNOWN TO EVERY READER (§3.1, §6.5): it
			// is parsed whatever the declaration and is never counted unknown
			if !r.skip(kind) {
				return r.damage("the node table cannot be framed")
			}
			continue
		}
		i, known := index[id]
		if !known {
			r.report.Unknown++
			// WHAT IS RETAINED: A FIELD WHOSE ID THIS READER CANNOT NAME
			// (docs/SPEC-TABLES.md §6.6), at any depth, in the root body and in
			// every node record, except the six excluded classes. A retained
			// field still counts `unknown`, because `unknown` says what a
			// READER could not name and that stays true.
			from := r.off
			if !r.skip(kind) {
				return r.damage("an unknown field that cannot be framed")
			}
			if r.rt != nil {
				r.rt.capture(inst, id, kind, r.buf[from:r.off], r.ids, r.report)
			}
			continue
		}
		fv := &inst.Fields[i]
		seen[i] = true
		if int(kind) != wireKind(fv.Def) {
			if !ir.TableKindWidens(int(kind), wireKind(fv.Def)) {
				// KIND MISMATCH (§4): kinds 31 and 32 at a position the reader
				// names take this same rule
				return r.refuseField(ReasonKindMismatch, fv.Def)
			}
			// WIDENED (§4): a kind that grew since the writer decodes exactly
			// at its own width, the value lands, and one `widened` counts.
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
		if !r.field(fv) {
			return false
		}
		if fv.Def.Type.Optional {
			// the field rode, so it is PRESENT — content decides nothing here
			// (docs/SPEC-TABLES.md §2.3)
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
		// A pointer field's payload is a NUMBER: it is resolved against the
		// numbering and stored, and the decode never follows it. The graph's
		// own rules ran in the wire check (§3.1, §6.5).
		index, ok := r.leb()
		if !ok {
			return r.damage("a node index that is truncated or not canonical")
		}
		return r.resolveCell(&fv.Cell, f, index)
	case f.KeyEnum != "":
		return r.keyed(fv)
	case f.Type.Kind == ir.TString:
		n, ok := r.lengthOf()
		if !ok {
			return false
		}
		payload := r.buf[r.off : r.off+n]
		r.off += n
		return r.text(&fv.Cell, &fv.Count, f, payload, !f.Type.TextUnbounded(), int(f.Type.Size))
	case f.Array != ir.ArrayNone || f.Type.Kind == ir.TBytes:
		return r.arrayBody(fv, -1)
	case tabletext.UnionOf(f) != nil:
		return r.unionCell(&fv.Cell, fv, -1)
	case tabletext.EnumOf(f) != nil:
		return r.enumCell(&fv.Cell, f, false)
	case tabletext.StructOf(f) != nil:
		n, ok := r.lengthOf()
		if !ok {
			return false
		}
		fv.Cell.Tab = r.m.New(tabletext.StructOf(f))
		if !r.nested(r.sub(n), fv.Cell.Tab, n) {
			return false
		}
		r.off += n
		return true
	}
	return r.scalarAt(&fv.Cell, f, ir.TableScalarKind(f))
}

// text places one kind 12 payload: well-formed UTF-8 with no zero byte, and no
// longer than the reader's declared bytes (`length_over_bound`). Nothing is cut.
func (r *wireReader) text(cell *tabletext.Cell, count *int, f *ir.Field, payload []byte, bounded bool, bound int) bool {
	if !textValid(payload) {
		return r.damage("ill-formed text")
	}
	if bounded && len(payload) > bound {
		return r.refuseField(ReasonLengthOverBound, f)
	}
	cell.Str = append([]byte(nil), payload...)
	*count = len(payload)
	return true
}

// nested decodes one L-framed nested body: a by-value table field, a table
// element, a keyed slot, a map entry or a union arm's table body. The body's
// terminator must be the last byte of its L (§3).
func (r *wireReader) nested(sub *wireReader, inst *tabletext.Instance, n int) bool {
	if !sub.bodyAt(inst, true) {
		return false
	}
	if sub.off != n {
		return r.damage("bytes after a nested body's terminator inside its L")
	}
	return true
}

// enumCell reads an enum value: kind 30's payload is the reference to the
// VARIANT NAME's id, `0` for None. A reference this reader's enum cannot name
// is §4's ordinary `unknown`: the field reads None and one event counts.
func (r *wireReader) enumCell(cell *tabletext.Cell, f *ir.Field, element bool) bool {
	ref, ok := r.leb()
	if !ok {
		return r.damage("a variant reference that is truncated or not canonical")
	}
	if ref == 0 {
		cell.U = 0 // the zero reference is the enum's None
		return true
	}
	id, named := r.id(ref)
	if !named {
		return r.damage("a variant reference past the id table")
	}
	en := tabletext.EnumOf(f)
	v := enumValueForId(en, id)
	if v < 0 {
		cell.U = 0
		r.report.Unknown++
		if r.rt != nil {
			// AN UNKNOWN ENUM VARIANT REFERENCE IS AN EXCLUDED CLASS
			// (docs/SPEC-TABLES.md §6.6)
			r.rt.lost(r.report)
		}
		return true
	}
	if tabletext.VariantDeprecated(en, v) {
		// A DEPRECATED VARIANT READS AS THE FIELD'S DECLARED DEFAULT, and an
		// element as the ELEMENT's default; it is never unknown
		// (docs/SPEC-TABLES.md §2.15).
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

// mapField decodes one map (docs/SPEC-TABLES.md §2.8). A map and a
// `[..N]Pair` are the same bytes, so the map's own rules are the declaration's
// and apply here only: every key is this reader's declared key kind or one it
// widens, every key fits the declaration, and the keys ascend strictly as this
// reader decodes them (an absent key field at its default). A repeated or
// descending key refuses.
func (r *wireReader) mapField(fv *tabletext.Field) bool {
	f := fv.Def
	bodyLen, ok := r.lengthOf()
	if !ok {
		return false
	}
	end := r.off + bodyLen
	if bodyLen < 2 {
		return r.damage("a map body too short for its header")
	}
	elemKind := r.u8()
	count, good := r.leb()
	if !good {
		return r.damage("a map count that is truncated or not canonical")
	}
	// A MAP HEADER WHOSE ELEMENT KIND IS NOT 13 is the array kind mismatch of
	// §4, and nothing about a map is special-cased
	if int(elemKind) != ir.TableKindTable {
		return r.refuseField(ReasonKindMismatch, f)
	}
	if count > uint64(math.MaxInt32) {
		return r.refuse(&CountRefusal{})
	}
	fv.Entries = nil
	sub := r.sub(end - r.off)
	var last tabletext.MapKey
	widened := false
	for i := range count {
		elemLen, ok := sub.lengthOf()
		if !ok {
			return false
		}
		body := sub.sub(elemLen)
		sub.off += elemLen
		entry := r.m.NewMapEntry(f)
		kindBad, entryWidened := false, false
		key, keyOk := body.mapKey(f, entry, &kindBad, &entryWidened)
		if r.err() != nil {
			return false
		}
		if kindBad {
			return r.refuseField(ReasonKindMismatch, f)
		}
		if !keyOk {
			return r.damage("a map entry whose key cannot be read")
		}
		if entryWidened && !widened {
			// the MAP counts one `widened` (§2.8, §4)
			widened = true
			r.report.Widened++
		}
		if !tabletext.MapKeyFits(f, key) {
			return r.refuseField(ReasonLengthOverBound, f)
		}
		if i > 0 && tabletext.MapKeyOrder(f, last, key) >= 0 {
			// A REPEATED OR DESCENDING KEY: not a body any conforming writer
			// produces under this declaration (§2.8, §4)
			return r.damage("map keys that do not ascend strictly")
		}
		decoded := r.m.NewMapEntry(f)
		whole := body.sub(0)
		whole.buf = body.buf
		whole.off = 0
		if !r.nested(whole, decoded, elemLen) {
			return false
		}
		fv.Entries = append(fv.Entries, tabletext.Cell{Tab: decoded})
		last = tabletext.MapKeyOf(f, decoded)
	}
	if sub.off != len(sub.buf) {
		return r.damage("bytes left inside a map's L after its entries")
	}
	r.off = end
	return true
}

// mapKey is the KEY SCAN (docs/SPEC-TABLES.md §2.8): before an entry's body
// decodes, the scan reads that body's field headers for the key's id and reads
// the key. A body with no key field at all has the key's declared default.
//
// THE KEY KIND IS THE READER'S DECLARATION, never the first entry's: a key
// under another kind that is not a widening sets kindBad.
func (r *wireReader) mapKey(f *ir.Field, entry *tabletext.Instance, kindBad, widened *bool) (tabletext.MapKey, bool) {
	keyField := ir.MapKeyField(f)
	want := ir.TableWireScalarKind(keyField)
	key := tabletext.MapKeyOf(f, entry)
	scan := &wireReader{buf: r.buf, report: &tabletext.Report{}, m: r.m, ids: r.ids, st: r.st, fail: r.fail}
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
		if id != ir.MapKeyWireId {
			if !scan.skip(kind) {
				return key, false
			}
			continue
		}
		cell := &entry.Fields[0].Cell
		if int(kind) != want {
			if !ir.TableKindWidens(int(kind), want) {
				*kindBad = true
				return key, true
			}
			*widened = true
			if !scan.scalarAt(cell, keyField, int(kind)) {
				return key, false
			}
		} else if !scan.scalarOrString(cell, keyField) {
			return key, false
		}
		key = tabletext.MapKeyOf(f, entry)
	}
}

// scalarOrString reads one key value: a `string(N)`'s length and bytes, or a
// scalar at its own width. A key is one or the other (§2.8).
func (r *wireReader) scalarOrString(cell *tabletext.Cell, f *ir.Field) bool {
	if f.Type.Kind == ir.TString {
		n, ok := r.lengthOf()
		if !ok {
			return false
		}
		if !textValid(r.buf[r.off : r.off+n]) {
			return r.damage("a map key that is not well-formed text")
		}
		cell.Str = append([]byte(nil), r.buf[r.off:r.off+n]...)
		r.off += n
		return true
	}
	return r.scalarAt(cell, f, ir.TableScalarKind(f))
}

// arrayBody decodes an array payload. A FIELD carries its own length first; an
// ARM does not, because the arm's L already framed it (docs/SPEC-TABLES.md
// §2.6, §3), so the caller passes the length it was framed by.
//
// THE HEADER LIES INSIDE ITS OWN L, and an L too short to hold it is damage
// (tla/Framing.tla). An element kind that is not the declared one and not a
// widening is a kind mismatch, a count past the reader's bound is
// count_over_bound, and bytes left inside L after the elements are damage.
func (r *wireReader) arrayBody(fv *tabletext.Field, framed int) bool {
	f := fv.Def
	bodyLen := framed
	if framed < 0 {
		n, ok := r.lengthOf()
		if !ok {
			return false
		}
		bodyLen = n
	}
	if !r.has(bodyLen) {
		return r.damage("an array body past the body carrying it")
	}
	end := r.off + bodyLen
	if bodyLen < 2 {
		return r.damage("an array body too short for its header")
	}
	ek := r.u8()
	count, good := r.leb()
	if !good || r.off > end {
		return r.damage("an array count that crosses its L")
	}
	if int(ek) != ir.TableWireElemKind(f) {
		if !ir.TableKindWidens(int(ek), ir.TableWireElemKind(f)) {
			// the element kind is part of the array's identity (§3)
			return r.refuseField(ReasonKindMismatch, f)
		}
		// WIDENED at an element kind (§4): every element decodes at the wire
		// kind's width, and ONE `widened` counts for the field
		r.report.Widened++
	}
	if count > uint64(math.MaxInt32) {
		return r.refuse(&CountRefusal{})
	}
	switch {
	case f.Type.Kind == ir.TBytes:
		if count > uint64(f.Type.Size) {
			return r.refuseField(ReasonLengthOverBound, f)
		}
	case f.Array != ir.ArrayList && count > uint64(f.ArrayBound):
		return r.refuseField(ReasonCountOverBound, f)
	}
	keep := int(count)
	sub := r.sub(end - r.off)
	if f.Type.Kind == ir.TBytes {
		if !sub.has(keep) {
			return r.damage("a count the array body cannot cover")
		}
		fv.Cell.Str = append([]byte(nil), sub.buf[:keep]...)
		sub.off = keep
	} else {
		if f.Array == ir.ArrayList {
			fv.Elems = fv.Elems[:0]
		}
		var spent int64
		for i := range keep {
			if f.Array == ir.ArrayList {
				fv.Elems = append(fv.Elems, r.m.ElementZero(f))
			}
			if !sub.element(fv, i, int(ek)) {
				return false
			}
			if !tabletext.TextTotalFits(fv, i, &spent) {
				// A TEXT LIST'S `| total` IS A BOUND, and past it the read refuses
				// (§2.13): nothing is dropped
				return r.refuse(&Refusal{Reason: ReasonLengthOverBound, Detail: f.TextListElem().MapEntryOf})
			}
		}
	}
	if sub.off != len(sub.buf) {
		return r.damage("bytes left inside an array's L after its elements")
	}
	if f.Type.Kind == ir.TBytes || f.CountedOnWire() {
		fv.Count = keep
	}
	if f.Type.Optional {
		fv.Present = true
	}
	r.off = end
	return true
}

func (r *wireReader) element(fv *tabletext.Field, i int, ek int) bool {
	f := fv.Def
	if ir.TextListPlain(f) {
		// A PLAIN TEXT LIST's element is kind 12: its own L, then L bytes
		// (docs/SPEC-TABLES.md §2.13, §3).
		n, ok := r.lengthOf()
		if !ok {
			return false
		}
		payload := r.buf[r.off : r.off+n]
		r.off += n
		rec := fv.Elems[i].Tab
		if rec == nil {
			rec = r.m.New(f.TextListElem())
			fv.Elems[i].Tab = rec
		}
		vc := &rec.Fields[0]
		return r.text(&vc.Cell, &vc.Count, f, payload, !ir.TextListUnbounded(f), int(ir.TextListValue(f).Type.Size))
	}
	if tabletext.UnionOf(f) != nil {
		// an element of an ARRAY OF UNIONS is an ARM HEADER and carries its own
		// kind, so the arm rules apply once per element (§3)
		fv.Elems[i].U = 0
		fv.Elems[i].Tab = nil
		return r.unionCell(&fv.Elems[i], fv, i)
	}
	if f.Type.Pointer {
		// an element of an array of pointers: a node index, resolved and
		// stored, never followed (§3.1)
		index, ok := r.leb()
		if !ok {
			return r.damage("a node index that is truncated or not canonical")
		}
		return r.resolveCell(&fv.Elems[i], f, index)
	}
	if tabletext.EnumOf(f) != nil {
		return r.enumCell(&fv.Elems[i], f, true)
	}
	if tabletext.StructOf(f) != nil {
		n, ok := r.lengthOf()
		if !ok {
			return false
		}
		fv.Elems[i].Tab = r.m.New(tabletext.StructOf(f))
		if !r.nested(r.sub(n), fv.Elems[i].Tab, n) {
			return false
		}
		r.off += n
		return true
	}
	// a scalar element decodes at the ELEMENT KIND the header carried, which
	// is the declared kind or one it widens (§4)
	return r.scalarAt(&fv.Elems[i], f, ek)
}

// keyed places an enum-keyed array's triples BY KEY REFERENCE, so a slot lands
// by name however the enum moved (docs/SPEC-TABLES.md §3.2). A key this reader
// cannot name is skipped by its length and counted unknown; a slot the writer
// never sent keeps its declared default.
func (r *wireReader) keyed(fv *tabletext.Field) bool {
	f := fv.Def
	bodyLen, ok := r.lengthOf()
	if !ok {
		return false
	}
	end := r.off + bodyLen
	if bodyLen < 2 {
		return r.damage("a keyed body too short for its header")
	}
	elemKind := r.u8()
	count, good := r.leb()
	if !good || r.off > end {
		return r.damage("a keyed count that crosses its L")
	}
	if int(elemKind) != ir.TableWireElemKind(f) {
		if !ir.TableKindWidens(int(elemKind), ir.TableWireElemKind(f)) {
			return r.refuseField(ReasonKindMismatch, f)
		}
		// WIDENED at a keyed body's element kind (§4)
		r.report.Widened++
	}
	sub := r.sub(end - r.off)
	for range count {
		key, good := sub.leb()
		if !good {
			return r.damage("a keyed slot's key that is truncated or not canonical")
		}
		keyID, named := sub.id(key)
		if !named {
			// `0` names no id at all, and None keys no slot (§3.2)
			return r.damage("a keyed slot whose key reference names no id")
		}
		elemLen, ok := sub.lengthOf()
		if !ok {
			return false
		}
		slot := tabletext.KeyedValueSlot(f, enumValueForId(f.KeyEnumRef, keyID))
		if slot < 0 {
			r.report.Unknown++ // a slot this reader cannot name
			if r.rt != nil {
				// AN UNKNOWN KEYED-ARRAY SLOT IS AN EXCLUDED CLASS
				// (docs/SPEC-TABLES.md §6.6)
				r.rt.lost(r.report)
			}
			sub.off += elemLen
			continue
		}
		elem := sub.sub(elemLen)
		switch {
		case tabletext.EnumOf(f) != nil:
			if !elem.enumCell(&fv.Elems[slot], f, true) {
				return false
			}
		case tabletext.StructOf(f) != nil:
			fv.Elems[slot].Tab = r.m.New(tabletext.StructOf(f))
			if !r.nested(elem, fv.Elems[slot].Tab, elemLen) {
				return false
			}
			elem.off = elemLen
		default:
			if !elem.scalarAt(&fv.Elems[slot], f, int(elemKind)) {
				return false
			}
		}
		if elem.off != elemLen {
			return r.damage("bytes left inside a keyed slot after its element")
		}
		sub.off += elemLen
	}
	if sub.off != len(sub.buf) {
		return r.damage("bytes left inside a keyed array's L after its slots")
	}
	r.off = end
	return true
}

// unionCell decodes the union framing into one cell — a field's, or an element
// of an array of unions (§3). AN ARM HEADER IS A FIELD HEADER: the arm id
// reference, the arm's KIND byte, `L`, then `L` bytes of arm payload.
func (r *wireReader) unionCell(cell *tabletext.Cell, fv *tabletext.Field, index int) bool {
	f := fv.Def
	un := tabletext.UnionOf(f)
	ref, ok := r.leb()
	if !ok {
		return r.damage("an arm reference that is truncated or not canonical")
	}
	cell.U = 0
	cell.Tab = nil
	cell.Arm = nil
	if ref == 0 {
		return true // empty: the id is the whole payload, not even a kind byte
	}
	armID, named := r.id(ref)
	if !named || !r.has(1) {
		return r.damage("an arm header that names no id")
	}
	kind := r.u8()
	length, ok := r.lengthOf()
	if !ok {
		return false
	}
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
		// an arm this reader cannot name: the value reads EMPTY and the body is
		// skipped by its length (§4)
		r.report.Unknown++
		if r.rt != nil {
			// AN UNKNOWN UNION ARM ID IS AN EXCLUDED CLASS (docs/SPEC-TABLES.md
			// §6.6)
			r.rt.lost(r.report)
		}
	case int(kind) != armWireKind(un.Variants[tag-1]):
		arm := un.Variants[tag-1]
		if !ir.TableKindWidens(int(kind), armWireKind(arm)) {
			// A RETYPED ARM IS JUDGED BY THE FIELD RULES: a kind mismatch (§3)
			return r.refuse(&Refusal{Reason: ReasonKindMismatch, Detail: un.Name + "." + arm.Name})
		}
		// WIDENED AT AN ARM (§4): decoded at its own width. An `L` that is
		// not that width is the arm's own framing damage (§3).
		if length != ir.TableKindWidth(int(kind)) {
			return r.damage("a widened arm whose L is not its kind's width")
		}
		av := r.m.NewArm(arm)
		if !sub.scalarAt(&av.Cell, arm.F, int(kind)) {
			return false
		}
		cell.U = uint64(tag)
		cell.Arm = av
		r.report.Widened++
	default:
		arm := un.Variants[tag-1]
		if arm.Body() {
			payload := r.m.New(arm.Ref)
			if !r.nested(sub, payload, length) {
				return false
			}
			cell.U = uint64(tag)
			cell.Tab = payload
		} else if !sub.arm(cell, arm, tag, length) {
			return false
		}
	}
	r.off += length
	return true
}

// arm decodes ONE ARM's payload out of a reader bounded to the arm's `L`
// (docs/SPEC-TABLES.md §3's arm payload table). The kind byte already agreed,
// so what is left to check is that L frames the payload exactly.
func (r *wireReader) arm(cell *tabletext.Cell, arm ir.UnionVariant, tag, n int) bool {
	if arm.Void() {
		if n != 0 {
			return r.damage("kind 32 with a nonzero L")
		}
		cell.U = uint64(tag)
		return true
	}
	f := arm.F
	if w := ir.ArmWireFixedWidth(f); w > 0 && n != w {
		return r.damage("an arm whose L is not its kind's width")
	}
	av := r.m.NewArm(arm)
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		index, good := r.leb()
		if !good {
			return r.damage("a node index that is truncated or not canonical")
		}
		if !r.resolveCell(&av.Cell, f, index) {
			return false
		}
	case f.Type.Kind == ir.TString:
		payload := r.buf[r.off : r.off+n]
		r.off += n
		if !r.text(&av.Cell, &av.Count, f, payload, true, int(f.Type.Size)) {
			return false
		}
	case f.Type.Kind == ir.TBytes, f.Array != ir.ArrayNone:
		if !r.arrayBody(av, n) {
			return false
		}
	case tabletext.UnionOf(f) != nil:
		if !r.unionCell(&av.Cell, av, -1) {
			return false
		}
	case tabletext.EnumOf(f) != nil:
		if !r.enumCell(&av.Cell, f, false) {
			return false
		}
	default:
		if !r.scalarAt(&av.Cell, f, ir.TableScalarKind(f)) {
			return false
		}
	}
	if r.off != n {
		// bytes after a nested union, a pointer or an enum inside the arm's L
		return r.damage("bytes left inside an arm's L after its payload")
	}
	cell.U = uint64(tag)
	cell.Arm = av
	return true
}

// scalarAt decodes one fixed-width value that arrived under `kind`: the
// field's own kind, or one the field's kind WIDENS (docs/SPEC-TABLES.md §4).
// The payload is read at the WIRE kind's width and signedness, sign-extended
// on the signed ladder and zero-extended on the unsigned one, and then held to
// the reader's declared range: a value outside it refuses `out_of_range`. The
// reader never clamps.
func (r *wireReader) scalarAt(cell *tabletext.Cell, f *ir.Field, kind int) bool {
	declared := ir.TableScalarKind(f)
	width := ir.TableKindWidth(kind)
	if !r.has(width) {
		return r.damage("a fixed-width payload cut short")
	}
	switch kind {
	case ir.TableKindBool:
		b := r.u8()
		if b > 1 {
			return r.damage("a bool byte other than 0 or 1")
		}
		cell.B = b != 0
		return true
	case ir.TableKindF32:
		bits := r.u32()
		if !tabletext.FiniteF32(bits) {
			// A NaN OR AN INFINITY IS REFUSED ON ITS RAW BITS (§3.3)
			return r.refuse(&NonFiniteRefusal{Field: FloatOwner(r.m, f)})
		}
		v := float64(math.Float32frombits(bits))
		if declared == ir.TableKindF64 {
			// f32 WIDENED into f64 (§4): exact, and a float64 field's
			// declared range is held as at eight bytes
			cell.F = v
			return true
		}
		if f.HasFloatRange {
			if v < f.FMin || v > f.FMax {
				return r.refuseField(ReasonOutOfRange, f)
			}
			if v == 0 {
				v = 0 // a compressed float's -0 lands as +0 (§3.3, the special values)
			}
		}
		cell.F = float64(float32(v))
		return true
	case ir.TableKindF64:
		bits := r.u64()
		if !tabletext.FiniteF64(bits) {
			return r.refuse(&NonFiniteRefusal{Field: FloatOwner(r.m, f)})
		}
		cell.F = math.Float64frombits(bits)
		return true
	}
	if ir.TableKindWide(kind) {
		raw := tabletext.WideFromBytes(r.buf[r.off:r.off+width], kind)
		r.off += width
		// the declared range on the RAW scale: a fixed field's whole-unit
		// bounds shifted by F (§4)
		v, outside := tabletext.WideClamp(raw, f)
		if outside {
			return r.refuseField(ReasonOutOfRange, f)
		}
		cell.Wide = v
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
		// bytes extended from the value, then the declared range
		var wide [16]byte
		binary.LittleEndian.PutUint64(wide[:8], uint64(value))
		if signed && value < 0 {
			for i := 8; i < 16; i++ {
				wide[i] = 0xFF
			}
		}
		v, outside := tabletext.WideClamp(tabletext.WideFromBytes(wide[:], declared), f)
		if outside {
			return r.refuseField(ReasonOutOfRange, f)
		}
		cell.Wide = v
		return true
	}
	if f.HasIntRange {
		lo, hi := bigInt64(f.IntMin), bigInt64(f.IntMax)
		if signed {
			if value < lo || value > hi {
				return r.refuseField(ReasonOutOfRange, f)
			}
		} else if raw < uint64(lo) || raw > uint64(hi) {
			return r.refuseField(ReasonOutOfRange, f)
		}
	}
	if f.Type.Kind == ir.TBits && f.Type.Width < width*8 {
		if uint64(value) > uint64(1)<<uint(f.Type.Width)-1 {
			return r.refuseField(ReasonOutOfRange, f)
		}
	}
	cell.I = value
	cell.U = uint64(value)
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

// CountRefusal is every read's refusal of a count above the int32 extent cap
// (docs/SPEC-TABLES.md §2.9): no region can hold it, whatever its size.
type CountRefusal struct{}

func (*CountRefusal) Error() string {
	return "count_over_extent_cap: a collection count above the int32 storage cap"
}

// NonFiniteRefusal is every read's refusal of a float whose bits are a NaN or
// an infinity (docs/SPEC-TABLES.md §3.3; the maintainer, 2026-10-27, "tables go
// finite-only too, same rule as packets"). Field names the field as
// <Table>.<field> where the read knows it.
type NonFiniteRefusal struct{ Field string }

func (e *NonFiniteRefusal) Error() string { return "non_finite_float: " + e.Field }

// FloatOwner names a field as <Owner>.<field> for a refusal's diagnostic
// (docs/SPEC-TABLES.md §6.5), the owner being the table, type or union that
// declares it. The cook's engine (internal/tablecook) names its refusals by it
// too.
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
