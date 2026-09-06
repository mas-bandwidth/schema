// The text form's WRITE half (docs/SPEC-TABLES.md §16.1): one instance as one JSON
// text — every field, in declaration order, defaults included, because a text
// is for people and tools and a text that elides is a text a reader has to
// know the schema to complete.
//
// The BYTES match the generated C++ writer: two-space indent, `": "` between a
// key and its value, a comma closing the previous line, `{}` and `[]` for the
// empty forms, a float at the shortest precision that reads back the same
// value at the field's own width. That is what makes `schema unpack` -> C++
// `ToJson` a byte comparison rather than a structural one.
package tabletext

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

type writer struct {
	b strings.Builder
	// graph is the pointered write's identity (docs/SPEC-TABLES.md §16.7): how
	// many slots name each node, learned in a first pass over the same walk,
	// and the `&node` label each shared node took at its first write.
	graph *graphOut
}

type graphOut struct {
	count  map[*Instance]int
	labels map[*Instance]uint64
	next   uint64
	// blobs counts the slots naming each BYTE BUFFER's blob (§2.5): a blob's
	// text is a string, which has no first key to carry `&node`, so a blob
	// named more than once has no spelling and the writer refuses the graph
	blobs map[*Blob]int
}

func (w *writer) raw(s string) { w.b.WriteString(s) }
func (w *writer) put(c byte)   { w.b.WriteByte(c) }
func (w *writer) line(depth int) {
	w.b.WriteByte('\n')
	for range depth {
		w.b.WriteString("  ")
	}
}

// Write renders one instance as one JSON text (docs/SPEC-TABLES.md §16.3). It fails
// where a value has no text spelling at all — a non-finite float, an enum value
// or union tag no variant names, a flags bit no variant names — which measure
// and save refuse for the same reason (§5), where the structure is nested past
// the depth cap, and where the graph carries a cycle, which the wire refuses
// too (§3.1).
func (m *Model) Write(inst *Instance) ([]byte, error) {
	w := &writer{}
	// PASS ONE (§16.7): one visit per node, every slot that names it counted,
	// so the write knows at a node's first occurrence whether it will be named
	// again. The ROOT's entry is open for the whole pass, so a reference back
	// at it is the cycle it is, and it takes no label.
	open := map[*Instance]bool{inst: true}
	graph := &graphOut{count: map[*Instance]int{}, labels: map[*Instance]uint64{}, blobs: map[*Blob]int{}}
	if err := m.countInstance(graph, inst, open); err != nil {
		return nil, err
	}
	w.graph = graph
	if err := m.writeValue(w, inst, 0); err != nil {
		return nil, err
	}
	return []byte(w.b.String()), nil
}

// countInstance is the first pass over the same fields the write walks — the
// guard's elision and the optional's presence decide what is written, and
// only what is written is an edge (§3.1).
func (m *Model) countInstance(g *graphOut, inst *Instance, open map[*Instance]bool) error {
	guards := Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		if fv.Def.Type.Optional && !fv.Present {
			continue
		}
		if err := m.countField(g, fv, open); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) countField(g *graphOut, fv *Field, open map[*Instance]bool) error {
	f := fv.Def
	switch {
	case f.Type.Blob():
		if fv.Cell.Blob != nil {
			g.blobs[fv.Cell.Blob]++
		}
		return nil
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return m.countNode(g, fv.Cell.Node, open)
	case f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString, f.Type.Kind == ir.TBytes:
		return nil
	case f.IsMap():
		// A MAP'S ENTRIES ARE BY-VALUE RECORDS (§2.8), and the value inside
		// one reaches nodes exactly as a nested table's fields do
		for i := range fv.Entries {
			if err := m.countInstance(g, fv.Entries[i].Tab, open); err != nil {
				return err
			}
		}
		return nil
	case f.KeyEnum != "":
		for slot := KeyedFirstSlot(); slot < KeyedSlotCount(f); slot++ {
			if err := m.countCell(g, &fv.Elems[slot], f, open); err != nil {
				return err
			}
		}
		return nil
	case f.Array != ir.ArrayNone:
		count := int(f.ArrayBound)
		if f.CountedOnWire() {
			count = fv.Count
		}
		for i := 0; i < count; i++ {
			if err := m.countCell(g, &fv.Elems[i], f, open); err != nil {
				return err
			}
		}
		return nil
	}
	return m.countCell(g, &fv.Cell, f, open)
}

func (m *Model) countCell(g *graphOut, cell *Cell, f *ir.Field, open map[*Instance]bool) error {
	if f.Type.Pointer {
		return m.countNode(g, cell.Node, open) // an element of an array of pointers (§2.1)
	}
	if un := UnionOf(f); un != nil {
		if cell.U != 0 && cell.Tab != nil {
			return m.countInstance(g, cell.Tab, open)
		}
		// an ARM that is not a body carries a field's storage, and the nodes
		// it reaches are that field's — a POINTER arm's pointee above all
		// (§2.6, §3.1)
		if cell.U != 0 && cell.Arm != nil {
			return m.countField(g, cell.Arm, open)
		}
		return nil
	}
	if StructOf(f) != nil && cell.Tab != nil {
		return m.countInstance(g, cell.Tab, open)
	}
	return nil
}

// countNode reaches one node through a pointer slot: a reference to a node
// whose descent is still open is a cycle, refused here as the wire refuses it
// (§3.1); a node already closed is sharing, counted and not descended again.
func (m *Model) countNode(g *graphOut, node *Instance, open map[*Instance]bool) error {
	if node == nil {
		return nil
	}
	g.count[node]++
	if open[node] {
		return fmt.Errorf("data cycle: a %s reaches a node whose descent is still open — a cycle is refused at save and at Lock, and the text form refuses it the same way (docs/SPEC-TABLES.md §3.1, §16.7)", node.Def.Name)
	}
	if g.count[node] > 1 {
		return nil
	}
	open[node] = true
	if err := m.countInstance(g, node, open); err != nil {
		return err
	}
	open[node] = false
	return nil
}

// writeFields renders one instance's fields, in declaration order, defaults
// included. `any` says whether the object is already open on entry — a shared
// node's `&node` opens it before the fields (§16.7) — and whether it is open on
// return.
func (m *Model) writeFields(w *writer, inst *Instance, depth int, any *bool) error {
	guards := Guards(inst.Def)
	for i := range inst.Fields {
		fv := &inst.Fields[i]
		if terms, guarded := guards[fv.Def.Name]; guarded && !inst.GuardHolds(terms) {
			continue
		}
		// an OPTIONAL field writes its key only when present: presence IS the
		// presence, and an absent key is the absence (docs/SPEC-TABLES.md §16.2)
		if fv.Def.Type.Optional && !fv.Present {
			continue
		}
		if !*any {
			w.put('{')
		} else {
			w.put(',')
		}
		*any = true
		w.line(depth + 1)
		writeString(w, []byte(ir.TableFieldJsonKey(fv.Def)))
		w.raw(": ")
		if err := m.writeField(w, fv, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) writeValue(w *writer, inst *Instance, depth int) error {
	// the write path carries the SAME cap the read path does (§16.2's 128):
	// a pointer chain nests as deep as it is long (§16.7), and the two walks
	// must agree about which structures are legal or one of them writes a
	// text the other refuses.
	if depth > maxJsonDepth {
		return fmt.Errorf("table %s: nested past the depth cap of %d — a pointer chain nests as deep as it is long (docs/SPEC-TABLES.md §16.7)", inst.Def.Name, maxJsonDepth)
	}
	any := false
	if err := m.writeFields(w, inst, depth, &any); err != nil {
		return err
	}
	if !any {
		w.raw("{}")
		return nil
	}
	w.line(depth)
	w.put('}')
	return nil
}

// writePointer renders the node a pointer slot names (docs/SPEC-TABLES.md
// §16.7): null as `null`, a node named once as its object in place, and a node
// named more than once DEFINED at its first occurrence — `&node` first, then its
// fields — and REFERENCED by `&node` alone after that, spelled the same way at
// every site. Labels run from 1 in first-write order and are the text's own, so
// a stray number in a hand-edited text is most often one never defined.
func (m *Model) writePointer(w *writer, node *Instance, depth int) error {
	if node == nil {
		w.raw("null")
		return nil
	}
	if w.graph == nil || w.graph.count[node] <= 1 {
		return m.writeValue(w, node, depth)
	}
	if depth > maxJsonDepth {
		return fmt.Errorf("table %s: nested past the depth cap of %d — a pointer chain nests as deep as it is long (docs/SPEC-TABLES.md §16.7)", node.Def.Name, maxJsonDepth)
	}
	if label, defined := w.graph.labels[node]; defined {
		w.put('{')
		w.line(depth + 1)
		w.raw(`"&node": `)
		w.raw(strconv.FormatUint(label, 10))
		w.line(depth)
		w.put('}')
		return nil
	}
	w.graph.next++
	label := w.graph.next
	w.graph.labels[node] = label
	w.put('{')
	w.line(depth + 1)
	w.raw(`"&node": `)
	w.raw(strconv.FormatUint(label, 10))
	any := true
	before := w.b.Len()
	if err := m.writeFields(w, node, depth, &any); err != nil {
		return err
	}
	// a definition carries at least one field, because a label alone is a
	// reference: a shared node with nothing to write has no definition this
	// form can spell, and the writer refuses it as it refuses any value it
	// cannot spell (§16.3)
	if w.b.Len() == before {
		return fmt.Errorf("table %s: a shared node with no field to write has no definition the text form can spell (docs/SPEC-TABLES.md §16.7)", node.Def.Name)
	}
	w.line(depth)
	w.put('}')
	return nil
}

func (m *Model) writeField(w *writer, fv *Field, depth int) error {
	f := fv.Def
	switch {
	case f.Type.Blob():
		// a BYTE BUFFER (docs/SPEC-TABLES.md §2.5, §16.7): null as `null`, a
		// blob named once as its bytes in place — base64 for a *bytes, the
		// string itself for a *string — and a blob named more than once
		// refused, because a string has no first key to carry `&node`
		blob := fv.Cell.Blob
		if blob == nil {
			w.raw("null")
			return nil
		}
		if w.graph != nil && w.graph.blobs[blob] > 1 {
			return fmt.Errorf("field %s names a byte buffer another slot names, and a blob's text is a string with no first key to carry `&node` — a shared blob has no spelling this form can carry (docs/SPEC-TABLES.md §16.7)", f.Name)
		}
		if f.Type.Kind == ir.TString {
			writeString(w, blob.Data)
		} else {
			writeBase64(w, blob.Data)
		}
		return nil
	case f.Type.Kind == ir.TString:
		writeString(w, fv.Cell.Str)
		return nil
	case f.Type.Kind == ir.TWString:
		writeWString(w, fv.Cell.Units)
		return nil
	case f.Type.Kind == ir.TBytes:
		writeBase64(w, fv.Cell.Str)
		return nil
	case f.IsMap():
		return m.writeMap(w, fv, depth)
	case f.KeyEnum != "":
		return m.writeKeyed(w, fv, depth)
	case f.Array != ir.ArrayNone:
		count := int(f.ArrayBound)
		if f.CountedOnWire() {
			count = fv.Count
		}
		if count == 0 {
			w.raw("[]")
			return nil
		}
		w.put('[')
		for i := 0; i < count; i++ {
			if i > 0 {
				w.put(',')
			}
			w.line(depth + 1)
			if err := m.writeScalar(w, &fv.Elems[i], f, depth+1); err != nil {
				return err
			}
		}
		w.line(depth)
		w.put(']')
		return nil
	}
	return m.writeScalar(w, &fv.Cell, f, depth)
}

// writeMap renders a MAP as a plain JSON object keyed by the KEY
// (docs/SPEC-TABLES.md §2.8, §16), in ASCENDING KEY ORDER with no key twice —
// the same order the wire is written in, because the order is the map's and
// not the projection's. JSON has no integer keys, so an integer-keyed map
// spells its keys as strings of digits.
func (m *Model) writeMap(w *writer, fv *Field, depth int) error {
	f := fv.Def
	if len(fv.Entries) == 0 {
		w.raw("{}")
		return nil
	}
	order := make([]int, len(fv.Entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return MapKeyOrder(f, MapKeyOf(f, fv.Entries[order[a]].Tab), MapKeyOf(f, fv.Entries[order[b]].Tab)) < 0
	})
	w.put('{')
	for i, at := range order {
		if i > 0 {
			w.put(',')
		}
		w.line(depth + 1)
		entry := fv.Entries[at].Tab
		writeString(w, mapKeyText(f, entry))
		w.raw(": ")
		if err := m.writeField(w, &entry.Fields[1], depth+1); err != nil {
			return err
		}
	}
	w.line(depth)
	w.put('}')
	return nil
}

// mapKeyText is one entry's key as the object's key: the bytes for a
// `string(N)`, and the decimal digits for an integer key.
func mapKeyText(f *ir.Field, entry *Instance) []byte {
	keyField := ir.MapKeyField(f)
	cell := &entry.Fields[0].Cell
	if keyField.Type.Kind == ir.TString {
		return cell.Str
	}
	if keyField.Type.Kind == ir.TInt && keyField.Type.Signed {
		return []byte(strconv.FormatInt(cell.I, 10))
	}
	return []byte(strconv.FormatUint(cell.U, 10))
}

// writeKeyed renders an enum-keyed array as an OBJECT keyed by variant name
// (docs/SPEC-TABLES.md §2.4, §16.2). Every slot the array KEYS is written, as a
// fixed array writes every element — the slots ARE the value. None keys no
// slot, so the storage holds none for it and nothing is written for it.
func (m *Model) writeKeyed(w *writer, fv *Field, depth int) error {
	f := fv.Def
	n := KeyedSlotCount(f)
	if n <= KeyedFirstSlot() {
		w.raw("{}")
		return nil
	}
	w.put('{')
	for slot := KeyedFirstSlot(); slot < n; slot++ {
		if slot > KeyedFirstSlot() {
			w.put(',')
		}
		w.line(depth + 1)
		name := EnumName(f.KeyEnumRef, KeyedSlotValue(f, slot))
		if name == "" {
			return fmt.Errorf("enum-keyed array %s: slot %d belongs to no variant of %s — a slot with no name has no text spelling (docs/SPEC-TABLES.md §3.2)", f.Name, slot, f.KeyEnum)
		}
		writeString(w, []byte(name))
		w.raw(": ")
		if err := m.writeScalar(w, &fv.Elems[slot], f, depth+1); err != nil {
			return err
		}
	}
	w.line(depth)
	w.put('}')
	return nil
}

func (m *Model) writeScalar(w *writer, cell *Cell, f *ir.Field, depth int) error {
	if un := UnionOf(f); un != nil {
		// a union is an object with ONE key, the arm's name; None is {}
		if cell.U == 0 {
			w.raw("{}")
			return nil
		}
		if int(cell.U) > len(un.Variants) {
			return fmt.Errorf("union %s: tag %d names no arm — the writer refuses it, exactly as measure does (docs/SPEC-TABLES.md §5)", un.Name, cell.U)
		}
		arm := un.Variants[cell.U-1]
		w.put('{')
		w.line(depth + 1)
		writeString(w, []byte(arm.Name))
		w.raw(": ")
		if arm.Void() {
			w.raw("null") // a payload-free arm: the name selects it (§2.6)
			w.line(depth)
			w.put('}')
			return nil
		}
		if !arm.Body() {
			// THE ARM'S VALUE TAKES THE ARM'S OWN ROW (§16.2): an arm is a
			// field line, so its value writes through the field walk
			fv := cell.Arm
			if fv == nil {
				fv = m.NewArm(arm)
			}
			if err := m.writeField(w, fv, depth+1); err != nil {
				return err
			}
			w.line(depth)
			w.put('}')
			return nil
		}
		payload := cell.Tab
		if payload == nil {
			payload = m.New(arm.Ref)
		}
		if err := m.writeValue(w, payload, depth+1); err != nil {
			return err
		}
		w.line(depth)
		w.put('}')
		return nil
	}
	if st := StructOf(f); st != nil {
		if f.Type.Pointer {
			return m.writePointer(w, cell.Node, depth)
		}
		inst := cell.Tab
		if inst == nil {
			inst = m.New(st)
		}
		return m.writeValue(w, inst, depth)
	}
	if e := EnumOf(f); e != nil {
		// a value no variant names has no text spelling, exactly as it has no
		// wire identity: the writer REFUSES rather than writing None over it
		name := EnumName(e, int64(cell.U))
		if name == "" {
			return fmt.Errorf("enum %s: value %d names no variant — the writer refuses it, exactly as measure does (docs/SPEC-TABLES.md §5)", e.Name, cell.U)
		}
		writeString(w, []byte(name))
		return nil
	}
	if fl := FlagsOf(f); fl != nil {
		bits := cell.U
		if bits == 0 {
			w.raw("[]")
			return nil
		}
		w.put('[')
		first := true
		for bit := range 64 {
			if bits&(uint64(1)<<uint(bit)) == 0 {
				continue
			}
			if bit >= len(fl.Variants) {
				return fmt.Errorf("flags %s: bit %d names no variant — a bit with no name has no text spelling (docs/SPEC-TABLES.md §16.2)", fl.Name, bit)
			}
			if !first {
				w.put(',')
			}
			first = false
			w.line(depth + 1)
			writeString(w, []byte(fl.Variants[bit]))
		}
		w.line(depth)
		w.put(']')
		return nil
	}
	if kind := ir.TableScalarKind(f); ir.TableKindWide(kind) {
		w.raw(FormatWide(WideValue(cell), kind, f.Type.FracBits))
		return nil
	}
	switch ir.TableScalarKind(f) {
	case ir.TableKindBool:
		if cell.B {
			w.raw("true")
		} else {
			w.raw("false")
		}
		return nil
	case ir.TableKindF32:
		return writeFloat(w, cell.F, true)
	case ir.TableKindF64:
		return writeFloat(w, cell.F, false)
	case ir.TableKindI8, ir.TableKindI16, ir.TableKindI32, ir.TableKindI64:
		w.raw(strconv.FormatInt(cell.I, 10))
		return nil
	default:
		w.raw(strconv.FormatUint(cell.U, 10))
		return nil
	}
}

// utf8Sequence is the length of ONE well-formed UTF-8 sequence at s, or -1 when
// the bytes there are not one. It rejects the lot: a stray continuation, an
// overlong form, a surrogate half, and anything past U+10FFFF.
func utf8Sequence(s []byte) int {
	lead := s[0]
	var want int
	var code uint32
	switch {
	case lead < 0x80:
		return 1
	case lead >= 0xc2 && lead <= 0xdf:
		want, code = 2, uint32(lead&0x1f)
	case lead >= 0xe0 && lead <= 0xef:
		want, code = 3, uint32(lead&0x0f)
	case lead >= 0xf0 && lead <= 0xf4:
		want, code = 4, uint32(lead&0x07)
	default:
		return -1
	}
	if len(s) < want {
		return -1
	}
	for i := 1; i < want; i++ {
		if s[i]&0xc0 != 0x80 {
			return -1
		}
		code = code<<6 | uint32(s[i]&0x3f)
	}
	switch {
	case want == 3 && code < 0x800: // overlong
		return -1
	case want == 4 && code < 0x10000: // overlong
		return -1
	case code >= 0xd800 && code <= 0xdfff: // a surrogate half
		return -1
	case code > 0x10ffff:
		return -1
	}
	return want
}

// writeString emits one JSON string. A JSON text MUST be valid UTF-8 (RFC 8259
// §8.1). The read path is byte-transparent — the wire imposes no encoding (§3)
// and a string may hold anything — so the WRITER is where that obligation is
// met: a byte that is not part of a well-formed sequence is written as U+FFFD,
// one per bad byte, and never raw (docs/SPEC-TABLES.md §16.3). The cost is stated
// plainly: for a string holding invalid UTF-8 the round trip is NOT
// byte-identical, because the alternative is emitting a text that is not JSON.
func writeString(w *writer, s []byte) {
	const hex = "0123456789abcdef"
	w.put('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			w.raw("\\\"")
		case '\\':
			w.raw("\\\\")
		case '\b':
			w.raw("\\b")
		case '\f':
			w.raw("\\f")
		case '\n':
			w.raw("\\n")
		case '\r':
			w.raw("\\r")
		case '\t':
			w.raw("\\t")
		default:
			switch {
			case c < 0x20:
				w.raw("\\u00")
				w.put(hex[c>>4])
				w.put(hex[c&0xf])
			case c < 0x80:
				w.put(c)
			default:
				if width := utf8Sequence(s[i:]); width < 0 {
					w.raw("\ufffd") // U+FFFD, one per bad byte
				} else {
					w.raw(string(s[i : i+width]))
					i += width - 1
				}
			}
		}
	}
	w.put('"')
}

// writeWString is a WIDE field's text (docs/SPEC-TABLES.md §16.2): the code
// units transcoded back to UTF-8. A SURROGATE PAIR is one code point; an
// UNPAIRED SURROGATE is not a code point at all, encodes to nothing, and
// writes one U+FFFD per ill-formed unit; a ZERO UNIT is U+0000, which JSON has
// an escape for, and writes \u0000 (§16.3). No wire can put either into
// storage (§3), so both answer for storage a PROGRAM built.
func writeWString(w *writer, units []uint16) {
	const hex = "0123456789abcdef"
	w.put('"')
	for i := 0; i < len(units); i++ {
		code := uint32(units[i])
		if code >= 0xd800 && code <= 0xdbff && i+1 < len(units) {
			if low := uint32(units[i+1]); low >= 0xdc00 && low <= 0xdfff {
				code = 0x10000 + (code-0xd800)<<10 + (low - 0xdc00)
				i++
			}
		}
		if code >= 0xd800 && code <= 0xdfff {
			code = 0xfffd // an unpaired surrogate
		}
		switch code {
		case '"':
			w.raw("\\\"")
		case '\\':
			w.raw("\\\\")
		case '\b':
			w.raw("\\b")
		case '\f':
			w.raw("\\f")
		case '\n':
			w.raw("\\n")
		case '\r':
			w.raw("\\r")
		case '\t':
			w.raw("\\t")
		default:
			if code < 0x20 {
				w.raw("\\u00")
				w.put(hex[code>>4])
				w.put(hex[code&0xf])
				continue
			}
			w.raw(string(encodeUTF8(code)))
		}
	}
	w.put('"')
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

func writeBase64(w *writer, data []byte) {
	w.put('"')
	i := 0
	for ; i+3 <= len(data); i += 3 {
		triple := uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
		w.put(base64Alphabet[triple>>18&0x3f])
		w.put(base64Alphabet[triple>>12&0x3f])
		w.put(base64Alphabet[triple>>6&0x3f])
		w.put(base64Alphabet[triple&0x3f])
	}
	if i < len(data) {
		left := len(data) - i
		triple := uint32(data[i]) << 16
		if left == 2 {
			triple |= uint32(data[i+1]) << 8
		}
		w.put(base64Alphabet[triple>>18&0x3f])
		w.put(base64Alphabet[triple>>12&0x3f])
		if left == 2 {
			w.put(base64Alphabet[triple>>6&0x3f])
		} else {
			w.put('=')
		}
		w.put('=')
	}
	w.put('"')
}

// writeFloat spells a float at the SHORTEST precision that reads back as the
// same value at the field's own width, so a round trip is exact and a text
// stays readable. Non-finite values have no JSON spelling at all and the
// writer REFUSES rather than losing one silently — the rule measure and save
// already apply to an enum value no variant names (§5).
func writeFloat(w *writer, value float64, single bool) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("float %v has no JSON spelling — the writer refuses it rather than losing it silently (docs/SPEC-TABLES.md §16.2)", value)
	}
	low, high := 15, 17
	if single {
		low, high = 6, 9
	}
	for digits := low; ; digits++ {
		text := formatG(value, digits)
		if digits >= high {
			w.raw(text)
			return nil
		}
		back, err := strconv.ParseFloat(text, 64)
		if err == nil {
			if single {
				if float64(float32(back)) == value {
					w.raw(text)
					return nil
				}
			} else if back == value {
				w.raw(text)
				return nil
			}
		}
	}
}

// formatG is C's `%.*g` for a finite double: the exponent form when the
// decimal exponent is below -4 or at least the precision, the plain form
// otherwise, trailing zeros and a trailing point removed, and an exponent of at
// least two digits. The generated writer reaches it through snprintf; a port
// spells it out, because the two must agree byte for byte.
func formatG(value float64, prec int) string {
	if prec < 1 {
		prec = 1
	}
	sci := strconv.FormatFloat(value, 'e', prec-1, 64)
	at := strings.IndexByte(sci, 'e')
	exp, _ := strconv.Atoi(sci[at+1:])
	if exp < -4 || exp >= prec {
		return trimZeros(sci[:at]) + "e" + expDigits(exp)
	}
	return trimZeros(strconv.FormatFloat(value, 'f', prec-1-exp, 64))
}

func trimZeros(s string) string {
	if !strings.ContainsRune(s, '.') {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

func expDigits(exp int) string {
	sign := "+"
	if exp < 0 {
		sign = "-"
		exp = -exp
	}
	if exp < 10 {
		return sign + "0" + strconv.Itoa(exp)
	}
	return sign + strconv.Itoa(exp)
}

// ---- the packer's two entry points (docs/SPEC-TABLES.md §17.1) ----

// WriteValue renders ONE FIELD's value as the text a `<field>.json` carries.
// The tree shape is the FIXED class's (§17.2): a variable root is one text, so
// no field written here holds a pointer, and the first pass finds nothing.
func (m *Model) WriteValue(fv *Field) ([]byte, error) {
	w := &writer{graph: &graphOut{count: map[*Instance]int{}, labels: map[*Instance]uint64{}}}
	if err := m.countField(w.graph, fv, map[*Instance]bool{}); err != nil {
		return nil, err
	}
	if err := m.writeField(w, fv, 0); err != nil {
		return nil, err
	}
	return []byte(w.b.String()), nil
}

// WriteElement renders ONE ELEMENT of an array field as the text a
// `<Variant>.json` or a bounded array's element file carries.
func (m *Model) WriteElement(fv *Field, slot int) ([]byte, error) {
	w := &writer{graph: &graphOut{count: map[*Instance]int{}, labels: map[*Instance]uint64{}}}
	if err := m.countCell(w.graph, &fv.Elems[slot], fv.Def, map[*Instance]bool{}); err != nil {
		return nil, err
	}
	if err := m.writeScalar(w, &fv.Elems[slot], fv.Def, 0); err != nil {
		return nil, err
	}
	return []byte(w.b.String()), nil
}
