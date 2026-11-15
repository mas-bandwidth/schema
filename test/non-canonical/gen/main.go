// Command gen writes the non-canonical spelling vectors
// (docs/SPEC-TABLES.md §3, §3.1, §3.3, §22; docs/rulings.json
// reads-refuse-non-canonical): the canonical variable-form file and the
// message-form batch the engine writes, then one file per spelling a
// conforming writer cannot emit, each re-encoded from the canonical bytes with
// every LEB128, length, count and id reference recomputed.
//
// The canonical bytes are the engine's own Encode. Each variant is parsed back
// into the field tree, changed only where the case names, and written again
// through the same id table walk, so no length is ever a hand-edited byte. The
// parser and writer reproduce the engine's canonical bytes byte for byte before
// any case is derived from them.
//
// The three message-form spellings (a default written, fields reordered, arm 0)
// are referenced from MANIFEST.tsv rather than copied here. A body that omits a
// field its announcement declares has no distinct wire: the omitted field reads
// its declared default, and the canonical batch of that value omits the same
// field, so the bytes are the canonical batch of a good value.
//
//	go run ./test/non-canonical/gen -out testdata/wire/non-canonical
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const defaultSchema = "test/non-canonical/NonCanonical.schema"

func main() {
	schema := flag.String("schema", defaultSchema, "the unit whose vectors are written")
	out := flag.String("out", "testdata/wire/non-canonical", "the vector directory")
	flag.Parse()

	c := compiler.New()
	u, err := c.Load([]string{*schema})
	if err != nil {
		fatal("load %s: %v", *schema, err)
	}
	m := tabletext.NewModel(u)

	canonical, err := variableWire(m)
	if err != nil {
		fatal("variable canonical: %v", err)
	}
	if got := encodeFile(mustParse(canonical)); !bytes.Equal(got, canonical) {
		fatal("the field walk does not reproduce the engine's canonical bytes:\n engine %x\n   walk %x", canonical, got)
	}

	message, err := messageWire(m)
	if err != nil {
		fatal("message canonical: %v", err)
	}
	announcement := tablewire.Announce(u)

	files := map[string][]byte{
		"variable-canonical.bin": canonical,
		"message-canonical.bin":  message,
		"announcement.bin":       announcement,
	}
	for name, wire := range variableCases(u, m, canonical) {
		files[name] = wire
	}
	manifest := manifestText()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal("%s: %v", *out, err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(*out, name), data, 0o644); err != nil {
			fatal("%s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(*out, "MANIFEST.tsv"), []byte(manifest), 0o644); err != nil {
		fatal("MANIFEST.tsv: %v", err)
	}
}

// variableWire is the engine's canonical form-1 file for one value: mode at
// its declared default (elided), count 5, head naming one Node record, and
// pick set to its one arm.
func variableWire(m *tabletext.Model) ([]byte, error) {
	root := m.New(m.Lookup("Root"))
	setField(root, "count", 5)
	head, ok := root.FieldByKey("head")
	if !ok {
		return nil, fmt.Errorf("Root has no field head")
	}
	node := m.New(m.Lookup("Node"))
	setField(node, "value", 3)
	head.Cell.Node = node
	pick, ok := root.FieldByKey("pick")
	if !ok {
		return nil, fmt.Errorf("Root has no field pick")
	}
	leaf := m.New(m.Lookup("Leaf"))
	setField(leaf, "tag", 4)
	pick.Cell.U = 1
	pick.Cell.Tab = leaf
	return tablewire.Encode(m, root)
}

// messageWire is the canonical batch of one Message: mode at its declared
// default (elided), count 5, pick None (elided).
func messageWire(m *tabletext.Model) ([]byte, error) {
	msg := m.New(m.Lookup("Message"))
	setField(msg, "count", 5)
	return tablewire.EncodeMessage(m, msg)
}

func variableCases(u *ir.Unit, m *tabletext.Model, canonical []byte) map[string][]byte {
	rootDef := u.Tables["Root"]
	if rootDef == nil {
		fatal("the unit declares no table Root")
	}
	fieldID := func(name string) uint64 {
		for _, f := range rootDef.Fields {
			if f.Name == name {
				return ir.TableFieldWireId(f)
			}
		}
		fatal("Root has no field %s", name)
		return 0
	}
	modeID := fieldID("mode")
	countID := fieldID("count")
	headID := fieldID("head")
	pickID := fieldID("pick")

	cases := map[string][]byte{}

	// FIELDS OUT OF THE WRITER'S DECLARATION ORDER: count and head swapped.
	f := mustParse(canonical)
	swapFields(f.root, countID, headID)
	cases["variable-fields-out-of-order.bin"] = encodeFile(f)

	// A FIELD WRITTEN AT THE DEFAULT THE WRITER ELIDES: mode = 7, first.
	f = mustParse(canonical)
	f.root.fields = append([]*wireField{{id: modeID, kind: byte(ir.TableKindU8), raw: []byte{7}}}, f.root.fields...)
	cases["variable-field-at-default.bin"] = encodeFile(f)

	// AN EXPLICIT NULL POINTER: the head field written with index 0.
	f = mustParse(canonical)
	findField(f.root, headID).index = 0
	cases["variable-explicit-null-pointer.bin"] = encodeFile(f)

	// AN UNUSED ID-TABLE ENTRY: one entry no body references.
	f = mustParse(canonical)
	other := u.Tables["OtherNode"]
	if other == nil {
		fatal("the unit declares no table OtherNode")
	}
	f.extra = append(f.extra, ir.TableWireId(other.WireName()))
	cases["variable-unused-id-entry.bin"] = encodeFile(f)

	// AN ID TABLE OUT OF FIRST-USE ORDER: the first two entries swapped and
	// every reference renumbered through the swapped table.
	f = mustParse(canonical)
	first := encodeFile(f)
	ids := trailerIDs(first)
	if len(ids) < 2 {
		fatal("the canonical id table has fewer than two entries")
	}
	order := append([]uint64(nil), ids...)
	order[0], order[1] = order[1], order[0]
	cases["variable-id-table-out-of-order.bin"] = encodeFileWithOrder(f, order)

	// AN EMPTY NODE TABLE: the node-table field present with a count of 0.
	f = mustParse(canonical)
	findField(f.root, ir.TableNodeWireId).nodes = &wireNodes{}
	cases["variable-empty-node-table.bin"] = encodeFile(f)

	// A NODE TABLE OUT OF ITS PLACE: the node-table field first in the root.
	f = mustParse(canonical)
	nodeTable := findField(f.root, ir.TableNodeWireId)
	removeField(f.root, ir.TableNodeWireId)
	f.root.fields = append([]*wireField{nodeTable}, f.root.fields...)
	cases["variable-node-table-out-of-place.bin"] = encodeFile(f)

	// A UNION WRITTEN WITH ARM REFERENCE 0: pick present, empty arm.
	f = mustParse(canonical)
	pick := findField(f.root, pickID)
	pick.un = &wireUnion{}
	cases["variable-union-arm-zero.bin"] = encodeFile(f)

	return cases
}

// manifestText is the vector directory's manifest: file, form, case, verdict.
// The three message-form spellings that already have a home are referenced
// there rather than copied here.
func manifestText() string {
	return "file\tform\tcase\tverdict\n" +
		"variable-canonical.bin\tvariable\tcanonical\tread\n" +
		"variable-fields-out-of-order.bin\tvariable\tfields-out-of-order\trefused\n" +
		"variable-field-at-default.bin\tvariable\tfield-at-default\trefused\n" +
		"variable-explicit-null-pointer.bin\tvariable\texplicit-null-pointer\trefused\n" +
		"variable-unused-id-entry.bin\tvariable\tunused-id-entry\trefused\n" +
		"variable-id-table-out-of-order.bin\tvariable\tid-table-out-of-order\trefused\n" +
		"variable-empty-node-table.bin\tvariable\tempty-node-table\trefused\n" +
		"variable-node-table-out-of-place.bin\tvariable\tnode-table-out-of-place\trefused\n" +
		"variable-union-arm-zero.bin\tvariable\tunion-arm-zero\trefused\n" +
		"message-canonical.bin\tmessage\tcanonical\tread\n" +
		"announcement.bin\tannouncement\tannouncement\tread\n" +
		"../message-one-encoding/explicit-default.bin\tmessage\tfield-at-default\trefused\n" +
		"../message-one-encoding/fields-reordered.bin\tmessage\tfields-out-of-order\trefused\n" +
		"../message-one-encoding/union-none.bin\tmessage\tunion-arm-zero\trefused\n"
}

// --- the field tree ---------------------------------------------------------

type wireFile struct {
	root  *wireBody
	extra []uint64
}

type wireBody struct {
	fields []*wireField
}

type wireField struct {
	id    uint64
	kind  byte
	raw   []byte     // a scalar or length-framed payload preserved verbatim
	body  *wireBody  // kind 13
	un    *wireUnion // kind 15
	index uint64     // kind 17
	refID uint64     // kind 30
	nodes *wireNodes // kind 12, the reserved node-table id
}

type wireUnion struct {
	arm  uint64
	kind byte
	body *wireBody
	raw  []byte
}

type wireNodes struct {
	recs []wireRecord
}

type wireRecord struct {
	typeID uint64
	body   *wireBody
}

func mustParse(data []byte) *wireFile {
	f, err := parseFile(data)
	if err != nil {
		fatal("parse canonical bytes: %v", err)
	}
	return f
}

func parseFile(data []byte) (*wireFile, error) {
	if len(data) < 1 || data[0] != byte(ir.TableWireForm) {
		return nil, fmt.Errorf("not a form-1 file")
	}
	ids, body, err := splitTrailer(data)
	if err != nil {
		return nil, err
	}
	b, err := parseBody(body, ids)
	if err != nil {
		return nil, err
	}
	return &wireFile{root: b}, nil
}

// splitTrailer returns the id table and the root body bytes before it.
func splitTrailer(data []byte) ([]uint64, []byte, error) {
	if len(data) < 8 {
		return nil, nil, fmt.Errorf("file shorter than its entry count")
	}
	n := binary.LittleEndian.Uint64(data[len(data)-8:])
	start := len(data) - 8 - int(8*n)
	if start < 1 {
		return nil, nil, fmt.Errorf("id table of %d entries runs past the file", n)
	}
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = binary.LittleEndian.Uint64(data[start+8*i:])
	}
	return ids, data[1:start], nil
}

// trailerIDs is the id table of an encoded file.
func trailerIDs(data []byte) []uint64 {
	ids, _, err := splitTrailer(data)
	if err != nil {
		fatal("%v", err)
	}
	return ids
}

func parseBody(data []byte, ids []uint64) (*wireBody, error) {
	b := &wireBody{}
	pos := 0
	for {
		if pos >= len(data) {
			return nil, fmt.Errorf("body did not end at a zero reference")
		}
		if data[pos] == 0 {
			pos++
			if pos != len(data) {
				return nil, fmt.Errorf("%d bytes after the body terminator", len(data)-pos)
			}
			return b, nil
		}
		f, next, err := parseField(data, pos, ids)
		if err != nil {
			return nil, err
		}
		b.fields = append(b.fields, f)
		pos = next
	}
}

func parseField(data []byte, pos int, ids []uint64) (*wireField, int, error) {
	ref, next, err := readLeb(data, pos)
	if err != nil {
		return nil, 0, err
	}
	if ref == 0 || ref > uint64(len(ids)) {
		return nil, 0, fmt.Errorf("reference %d outside an id table of %d", ref, len(ids))
	}
	if next >= len(data) {
		return nil, 0, fmt.Errorf("field header runs past the body")
	}
	f := &wireField{id: ids[ref-1], kind: data[next]}
	pos = next + 1
	switch f.kind {
	case byte(ir.TableKindString), byte(ir.TableKindArray), byte(ir.TableKindKeyed):
		l, next, err := readLeb(data, pos)
		if err != nil {
			return nil, 0, err
		}
		pos = next
		if pos+int(l) > len(data) {
			return nil, 0, fmt.Errorf("field %016x length runs past the body", f.id)
		}
		payload := data[pos : pos+int(l)]
		pos += int(l)
		if f.kind == byte(ir.TableKindString) && f.id == ir.TableNodeWireId {
			nodes, err := parseNodes(payload, ids)
			if err != nil {
				return nil, 0, err
			}
			f.nodes = nodes
		} else {
			f.raw = append([]byte(nil), payload...)
		}
	case byte(ir.TableKindTable):
		l, next, err := readLeb(data, pos)
		if err != nil {
			return nil, 0, err
		}
		pos = next
		if pos+int(l) > len(data) {
			return nil, 0, fmt.Errorf("field %016x body runs past the body", f.id)
		}
		f.body, err = parseBody(data[pos:pos+int(l)], ids)
		if err != nil {
			return nil, 0, err
		}
		pos += int(l)
	case byte(ir.TableKindUnion):
		arm, next, err := readLeb(data, pos)
		if err != nil {
			return nil, 0, err
		}
		pos = next
		f.un = &wireUnion{}
		if arm != 0 {
			if arm > uint64(len(ids)) {
				return nil, 0, fmt.Errorf("union arm reference %d outside the id table", arm)
			}
			f.un.arm = ids[arm-1]
			if pos >= len(data) {
				return nil, 0, fmt.Errorf("union arm kind runs past the body")
			}
			f.un.kind = data[pos]
			pos++
			l, next, err := readLeb(data, pos)
			if err != nil {
				return nil, 0, err
			}
			pos = next
			if pos+int(l) > len(data) {
				return nil, 0, fmt.Errorf("union arm payload runs past the body")
			}
			payload := data[pos : pos+int(l)]
			pos += int(l)
			if f.un.kind == byte(ir.TableKindTable) {
				f.un.body, err = parseBody(payload, ids)
				if err != nil {
					return nil, 0, err
				}
			} else {
				f.un.raw = append([]byte(nil), payload...)
			}
		}
	case byte(ir.TableKindPointer):
		index, next, err := readLeb(data, pos)
		if err != nil {
			return nil, 0, err
		}
		f.index = index
		pos = next
	case byte(ir.TableKindEnum):
		refID, next, err := readLeb(data, pos)
		if err != nil {
			return nil, 0, err
		}
		if refID != 0 {
			if refID > uint64(len(ids)) {
				return nil, 0, fmt.Errorf("enum reference %d outside the id table", refID)
			}
			f.refID = ids[refID-1]
		}
		pos = next
	default:
		n := ir.TableKindWidth(int(f.kind))
		if n == 0 {
			return nil, 0, fmt.Errorf("field %016x has unsupported kind %d", f.id, f.kind)
		}
		if pos+n > len(data) {
			return nil, 0, fmt.Errorf("field %016x payload runs past the body", f.id)
		}
		f.raw = append([]byte(nil), data[pos:pos+n]...)
		pos += n
	}
	return f, pos, nil
}

func parseNodes(payload []byte, ids []uint64) (*wireNodes, error) {
	count, pos, err := readLeb(payload, 0)
	if err != nil {
		return nil, err
	}
	nodes := &wireNodes{}
	for range count {
		typeRef, next, err := readLeb(payload, pos)
		if err != nil {
			return nil, err
		}
		if typeRef == 0 || typeRef > uint64(len(ids)) {
			return nil, fmt.Errorf("node type reference %d outside the id table", typeRef)
		}
		pos = next
		l, next, err := readLeb(payload, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		if pos+int(l) > len(payload) {
			return nil, fmt.Errorf("node record runs past the node table")
		}
		body, err := parseBody(payload[pos:pos+int(l)], ids)
		if err != nil {
			return nil, err
		}
		nodes.recs = append(nodes.recs, wireRecord{typeID: ids[typeRef-1], body: body})
		pos += int(l)
	}
	if pos != len(payload) {
		return nil, fmt.Errorf("%d bytes after the last node record", len(payload)-pos)
	}
	return nodes, nil
}

func readLeb(data []byte, pos int) (uint64, int, error) {
	var v uint64
	for i := 0; i < 10; i++ {
		if pos >= len(data) {
			return 0, 0, fmt.Errorf("unfinished LEB128")
		}
		b := data[pos]
		pos++
		if i == 9 && b > 1 {
			return 0, 0, fmt.Errorf("LEB128 past ten bytes")
		}
		v |= uint64(b&0x7f) << (7 * i)
		if b < 0x80 {
			return v, pos, nil
		}
	}
	return 0, 0, fmt.Errorf("LEB128 past ten bytes")
}

// --- the writer -------------------------------------------------------------

type idWriter struct {
	ids     []uint64
	refByID map[uint64]uint64
}

func newIDWriter(order []uint64) *idWriter {
	w := &idWriter{refByID: map[uint64]uint64{}}
	for i, id := range order {
		w.ids = append(w.ids, id)
		w.refByID[id] = uint64(i + 1)
	}
	return w
}

func (w *idWriter) ref(id uint64) uint64 {
	if r, ok := w.refByID[id]; ok {
		return r
	}
	w.ids = append(w.ids, id)
	r := uint64(len(w.ids))
	w.refByID[id] = r
	return r
}

func encodeFile(f *wireFile) []byte { return encodeFileWithOrder(f, nil) }

func encodeFileWithOrder(f *wireFile, order []uint64) []byte {
	ids := newIDWriter(order)
	body := encodeBodyFields(f.root, ids)
	out := []byte{byte(ir.TableWireForm)}
	out = append(out, body...)
	out = append(out, 0)
	for _, id := range ids.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	for _, id := range f.extra {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(len(ids.ids)+len(f.extra)))
	return out
}

func encodeBodyFields(b *wireBody, ids *idWriter) []byte {
	var out wireBuf
	for _, f := range b.fields {
		encodeField(&out, f, ids)
	}
	return out.b
}

func encodeField(out *wireBuf, f *wireField, ids *idWriter) {
	out.leb(ids.ref(f.id))
	out.u8(f.kind)
	switch f.kind {
	case byte(ir.TableKindTable):
		body := encodeBodyFields(f.body, ids)
		out.leb(uint64(len(body) + 1))
		out.raw(body)
		out.u8(0)
	case byte(ir.TableKindUnion):
		if f.un.arm == 0 {
			out.leb(0)
			return
		}
		out.leb(ids.ref(f.un.arm))
		out.u8(f.un.kind)
		if f.un.kind == byte(ir.TableKindTable) {
			body := encodeBodyFields(f.un.body, ids)
			out.leb(uint64(len(body) + 1))
			out.raw(body)
			out.u8(0)
		} else {
			out.leb(uint64(len(f.un.raw)))
			out.raw(f.un.raw)
		}
	case byte(ir.TableKindString), byte(ir.TableKindArray), byte(ir.TableKindKeyed):
		payload := f.raw
		if f.nodes != nil {
			payload = encodeNodes(f.nodes, ids)
		}
		out.leb(uint64(len(payload)))
		out.raw(payload)
	case byte(ir.TableKindPointer):
		out.leb(f.index)
	case byte(ir.TableKindEnum):
		if f.refID == 0 {
			out.leb(0)
		} else {
			out.leb(ids.ref(f.refID))
		}
	default:
		out.raw(f.raw)
	}
}

func encodeNodes(nodes *wireNodes, ids *idWriter) []byte {
	var out wireBuf
	out.leb(uint64(len(nodes.recs)))
	for _, r := range nodes.recs {
		out.leb(ids.ref(r.typeID))
		body := encodeBodyFields(r.body, ids)
		out.leb(uint64(len(body) + 1))
		out.raw(body)
		out.u8(0)
	}
	return out.b
}

type wireBuf struct{ b []byte }

func (w *wireBuf) u8(v byte)    { w.b = append(w.b, v) }
func (w *wireBuf) raw(v []byte) { w.b = append(w.b, v...) }
func (w *wireBuf) leb(v uint64) {
	for v >= 0x80 {
		w.b = append(w.b, byte(v)|0x80)
		v >>= 7
	}
	w.b = append(w.b, byte(v))
}

// --- tree edits -------------------------------------------------------------

func findField(b *wireBody, id uint64) *wireField {
	for _, f := range b.fields {
		if f.id == id {
			return f
		}
	}
	fatal("the canonical file has no field %016x", id)
	return nil
}

func removeField(b *wireBody, id uint64) {
	for i, f := range b.fields {
		if f.id == id {
			b.fields = append(b.fields[:i], b.fields[i+1:]...)
			return
		}
	}
	fatal("the canonical file has no field %016x", id)
}

func swapFields(b *wireBody, a, c uint64) {
	ai, ci := -1, -1
	for i, f := range b.fields {
		switch f.id {
		case a:
			ai = i
		case c:
			ci = i
		}
	}
	if ai < 0 || ci < 0 {
		fatal("the canonical file does not carry both %016x and %016x", a, c)
	}
	b.fields[ai], b.fields[ci] = b.fields[ci], b.fields[ai]
}

func setField(inst *tabletext.Instance, name string, v uint64) {
	fv, ok := inst.FieldByKey(name)
	if !ok {
		fatal("%s has no field %s", inst.Def.Name, name)
	}
	fv.Cell.U = v
	fv.Cell.I = int64(v)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "noncanonicalgen: "+format+"\n", args...)
	os.Exit(1)
}
