// Command gen writes the non-canonical spelling vectors (docs/SPEC-TABLES.md
// §3, §3.1, §3.3, §4; docs/rulings.json reads-refuse-non-canonical).
//
// It starts from the canonical bytes the engine writes for
// test/non-canonical/NonCanonical.schema: the variable form of a Root value
// and the message form of a Batch value. Every case is re-encoded from the
// same value with one spelling moved — a field order, a default, a null
// pointer, an id-table entry, a node table, a union arm, or an omitted
// message field — and every LEB, length and count it moves is recomputed. The
// manually assembled canonical files are held to the engine's own bytes before
// any case is trusted.
//
//	go run ./test/non-canonical/gen --out testdata/wire/non-canonical
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const schemaPath = "test/non-canonical/NonCanonical.schema"

func main() {
	out := flag.String("out", "", "directory to write the vectors and MANIFEST.tsv into")
	flag.Parse()
	if *out == "" {
		fatal("noncanonicalgen needs --out <directory>")
	}
	if err := run(*out); err != nil {
		fatal("%v", err)
	}
}

func run(out string) error {
	u, err := compiler.New().Load([]string{schemaPath})
	if err != nil {
		return fmt.Errorf("load %s: %w", schemaPath, err)
	}
	m := tabletext.NewModel(u)
	rootDef := m.Lookup("Root")
	nodeDef := m.Lookup("Node")
	otherDef := m.Lookup("Other")
	batchDef := m.Lookup("Batch")
	if rootDef == nil || nodeDef == nil || otherDef == nil || batchDef == nil {
		return fmt.Errorf("%s must declare Root, Node, Other and Batch", schemaPath)
	}

	ids, err := newVarIDs(rootDef, nodeDef)
	if err != nil {
		return err
	}

	// THE CANONICAL VARIABLE FILE is the engine's own save of a value that
	// reaches every field.
	engineVar, err := encodeEngineVariable(m, rootDef, nodeDef, 9, 5, 3, 4)
	if err != nil {
		return err
	}
	canonicalVar := encodeVariable(ids, varPlan{
		order:     []string{"a", "b", "next", "pick", "nodeTable"},
		a:         9,
		b:         5,
		nodeIndex: 2,
		nodeValue: 3,
		armRef:    ids.arm,
		armValue:  4,
		nodeTable: "record",
	})
	if !bytes.Equal(canonicalVar, engineVar) {
		return fmt.Errorf("the manual variable assembly disagrees with the engine's Encode:\n manual %x\nengine %x", canonicalVar, engineVar)
	}

	// THE CANONICAL MESSAGE FILE is the engine's own save of a Batch value.
	engineMsg, err := encodeEngineMessage(m, batchDef, 5, 3, 4)
	if err != nil {
		return err
	}
	message := newMessagePlan(u, batchDef)
	canonicalMsg, err := message.encode(5, 3, 4)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonicalMsg, engineMsg) {
		return fmt.Errorf("the manual message assembly disagrees with the engine's EncodeMessage:\n manual %x\nengine %x", canonicalMsg, engineMsg)
	}

	files := map[string][]byte{
		"variable-canonical.bin": canonicalVar,
		"fields-out-of-order.bin": encodeVariable(ids, varPlan{
			order: []string{"b", "a", "next", "pick", "nodeTable"}, a: 9, b: 5,
			nodeIndex: 2, nodeValue: 3, armRef: ids.arm, armValue: 4, nodeTable: "record",
		}),
		"default-written.bin": encodeVariable(ids, varPlan{
			order: []string{"a", "b", "next", "pick", "nodeTable"}, a: 7, b: 5,
			nodeIndex: 2, nodeValue: 3, armRef: ids.arm, armValue: 4, nodeTable: "record",
		}),
		"explicit-null-pointer.bin": encodeVariable(ids, varPlan{
			order: []string{"a", "b", "next", "pick"}, a: 9, b: 5,
			nodeIndex: 0, armRef: ids.arm, armValue: 4,
		}),
		"unused-id-entry.bin": encodeVariable(ids, varPlan{
			order: []string{"a", "b", "next", "pick", "nodeTable"}, a: 9, b: 5,
			nodeIndex: 2, nodeValue: 3, armRef: ids.arm, armValue: 4, nodeTable: "record",
			extraID: ir.TableWireId(otherDef.WireName()),
		}),
		"id-table-reordered.bin": encodeVariable(ids, varPlan{
			order: []string{"a", "b", "next", "pick", "nodeTable"}, a: 9, b: 5,
			nodeIndex: 2, nodeValue: 3, armRef: ids.arm, armValue: 4, nodeTable: "record",
			preseed: []uint64{ids.b, ids.a},
		}),
		"empty-node-table.bin": encodeVariable(ids, varPlan{
			order: []string{"a", "b", "pick", "nodeTable"}, a: 9, b: 5,
			armRef: ids.arm, armValue: 4, nodeTable: "empty",
		}),
		"node-table-misplaced.bin": encodeVariable(ids, varPlan{
			order: []string{"a", "nodeTable", "b", "next", "pick"}, a: 9, b: 5,
			nodeIndex: 2, nodeValue: 3, armRef: ids.arm, armValue: 4, nodeTable: "record",
		}),
		"union-arm-zero.bin": encodeVariable(ids, varPlan{
			order: []string{"a", "b", "next", "pick", "nodeTable"}, a: 9, b: 5,
			nodeIndex: 2, nodeValue: 3, armRef: 0, nodeTable: "record",
		}),
		"announcement.bin":      tablewire.Announce(u),
		"message-canonical.bin": canonicalMsg,
	}

	omit, err := message.encodeOmit(5, 4)
	if err != nil {
		return err
	}
	files["message-omits-field.bin"] = omit

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return os.WriteFile(filepath.Join(out, "MANIFEST.tsv"), []byte(manifest), 0o644)
}

// varIDs are the names one Root wire can carry, interned per case.
type varIDs struct {
	a, b, node, pick, arm uint64
	nodeType, nodeValue   uint64
}

func newVarIDs(rootDef, nodeDef *ir.Struct) (varIDs, error) {
	if len(rootDef.Fields) != 4 || len(nodeDef.Fields) != 1 {
		return varIDs{}, fmt.Errorf("the schema moved: Root needs four fields and Node one")
	}
	root := rootDef.Fields
	un := tabletext.UnionOf(root[3])
	if un == nil || len(un.Variants) != 1 {
		return varIDs{}, fmt.Errorf("the schema moved: Root.pick needs a one-arm union")
	}
	return varIDs{
		a:         ir.TableFieldWireId(root[0]),
		b:         ir.TableFieldWireId(root[1]),
		node:      ir.TableFieldWireId(root[2]),
		pick:      ir.TableFieldWireId(root[3]),
		arm:       ir.TableWireId(un.Variants[0].WireName()),
		nodeType:  ir.TableWireId(nodeDef.WireName()),
		nodeValue: ir.TableFieldWireId(nodeDef.Fields[0]),
	}, nil
}

// varPlan is one Root wire: the fields written, in the order written, with the
// one spelling that moves. `order` names root fields and the node table, so a
// node table out of its place is just an earlier element of the same list.
type varPlan struct {
	order     []string
	a, b      uint32
	nodeIndex uint64
	nodeValue uint32
	armRef    uint64 // 0 writes the None arm; otherwise the set arm's id
	armValue  uint32
	nodeTable string // "" none, "empty", "record"
	preseed   []uint64
	extraID   uint64
}

func encodeVariable(ids varIDs, plan varPlan) []byte {
	t := newIDTable(plan.preseed)
	var body wireBuf
	for _, name := range plan.order {
		switch name {
		case "a":
			body.leb(t.ref(ids.a))
			body.u8(byte(ir.TableKindU32))
			body.u32(plan.a)
		case "b":
			body.leb(t.ref(ids.b))
			body.u8(byte(ir.TableKindU32))
			body.u32(plan.b)
		case "next":
			body.leb(t.ref(ids.node))
			body.u8(byte(ir.TableKindPointer))
			body.leb(plan.nodeIndex)
		case "pick":
			body.leb(t.ref(ids.pick))
			body.u8(byte(ir.TableKindUnion))
			if plan.armRef == 0 {
				body.leb(0)
				break
			}
			body.leb(t.ref(plan.armRef))
			body.u8(byte(ir.TableKindI32))
			body.leb(4)
			body.u32(plan.armValue)
		case "nodeTable":
			body.leb(t.ref(ir.TableNodeWireId))
			body.u8(byte(ir.TableKindString))
			var payload wireBuf
			if plan.nodeTable == "empty" {
				payload.leb(0)
			} else {
				payload.leb(1) // node_count
				payload.leb(t.ref(ids.nodeType))
				var record wireBuf
				record.leb(t.ref(ids.nodeValue))
				record.u8(byte(ir.TableKindU32))
				record.u32(plan.nodeValue)
				record.u8(0) // the record body's terminator
				payload.leb(uint64(len(record.b)))
				payload.raw(record.b)
			}
			body.leb(uint64(len(payload.b)))
			body.raw(payload.b)
		default:
			panic("unknown field " + name)
		}
	}
	body.u8(0) // the root body's terminator

	t.append(plan.extraID)
	out := []byte{byte(ir.TableWireForm)}
	out = append(out, body.b...)
	for _, id := range t.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(t.ids)))
}

func encodeEngineVariable(m *tabletext.Model, rootDef, nodeDef *ir.Struct, a, b, nodeValue, armValue uint32) ([]byte, error) {
	root := m.New(rootDef)
	setU(root, "a", a)
	setU(root, "b", b)
	node := m.New(nodeDef)
	setU(node, "value", nodeValue)
	nf, ok := root.FieldByKey("next")
	if !ok {
		return nil, fmt.Errorf("Root has no field next")
	}
	nf.Cell.Node = node
	pf, ok := root.FieldByKey("pick")
	if !ok {
		return nil, fmt.Errorf("Root has no field pick")
	}
	un := tabletext.UnionOf(pf.Def)
	if un == nil || len(un.Variants) != 1 {
		return nil, fmt.Errorf("Root.pick is not a one-arm union")
	}
	pf.Cell.U = 1
	pf.Cell.Arm = m.NewArm(un.Variants[0])
	pf.Cell.Arm.Cell.I = int64(armValue)
	pf.Cell.Arm.Cell.U = uint64(armValue)
	return tablewire.Encode(m, root)
}

// messagePlan is the Batch unit's announced vocabulary, reduced to the slots
// and widths this small body uses.
type messagePlan struct {
	refBits             int
	count, note, pick   uint64
	arm                 uint64
	countBits, noteBits int
	armBits             int
}

func newMessagePlan(u *ir.Unit, batchDef *ir.Struct) messagePlan {
	slots := tablewire.MessageVocabularySlots(u)
	field := func(name string) (*ir.Field, uint64) {
		for i := range batchDef.Fields {
			if batchDef.Fields[i].Name == name {
				return batchDef.Fields[i], slots[ir.TableFieldEntry(batchDef.Fields[i]).Key()]
			}
		}
		panic("Batch has no field " + name)
	}
	un := tabletext.UnionOf(findField(batchDef, "pick"))
	arm := un.Variants[0]
	p := messagePlan{
		refBits: ir.TableMessageRefBits(len(tablewire.MessageVocabulary(u))),
		arm:     slots[ir.TableArmEntry(arm).Key()],
		armBits: 32,
	}
	countField, countSlot := field("count")
	noteField, noteSlot := field("note")
	_, pickSlot := field("pick")
	p.count, p.countBits = countSlot, valueBits(countField)
	p.note, p.noteBits = noteSlot, valueBits(noteField)
	p.pick = pickSlot
	return p
}

// valueBits is the width of a scalar field's payload: the raw storage width
// under the announced shape.
func valueBits(f *ir.Field) int {
	return int(ir.TableMessageValueBits(uint8(ir.TableWireScalarKind(f)), ir.TableFieldShape(f)))
}

func (p messagePlan) encode(count, note, armValue uint32) ([]byte, error) {
	return p.encodeFields(count, &note, armValue)
}

func (p messagePlan) encodeOmit(count, armValue uint32) ([]byte, error) {
	return p.encodeFields(count, nil, armValue)
}

func (p messagePlan) encodeFields(count uint32, note *uint32, armValue uint32) ([]byte, error) {
	w := &bitWriter{}
	w.put(0, 8) // the batch count, M - 1, for one body
	w.put(p.count, p.refBits)
	w.put(uint64(count), p.countBits)
	if note != nil {
		w.put(p.note, p.refBits)
		w.put(uint64(*note), p.noteBits)
	}
	w.put(p.pick, p.refBits)
	w.put(p.arm, p.refBits)
	w.put(uint64(armValue), p.armBits)
	w.put(0, p.refBits) // the body's terminator
	w.align()
	return append([]byte{byte(ir.TableWireMessageForm)}, w.b...), nil
}

func encodeEngineMessage(m *tabletext.Model, batchDef *ir.Struct, count, note, armValue uint32) ([]byte, error) {
	inst := m.New(batchDef)
	setU(inst, "count", count)
	setU(inst, "note", note)
	pf, ok := inst.FieldByKey("pick")
	if !ok {
		return nil, fmt.Errorf("Batch has no field pick")
	}
	un := tabletext.UnionOf(pf.Def)
	if un == nil || len(un.Variants) != 1 {
		return nil, fmt.Errorf("Batch.pick is not a one-arm union")
	}
	pf.Cell.U = 1
	pf.Cell.Arm = m.NewArm(un.Variants[0])
	pf.Cell.Arm.Cell.I = int64(armValue)
	pf.Cell.Arm.Cell.U = uint64(armValue)
	return tablewire.EncodeMessage(m, inst)
}

func findField(st *ir.Struct, name string) *ir.Field {
	for i := range st.Fields {
		if st.Fields[i].Name == name {
			return st.Fields[i]
		}
	}
	panic(st.Name + " has no field " + name)
}

func setU(inst *tabletext.Instance, key string, v uint32) {
	fv, ok := inst.FieldByKey(key)
	if !ok {
		fatal("%s has no field %s", inst.Def.Name, key)
	}
	fv.Cell.U = uint64(v)
	fv.Cell.I = int64(v)
}

// wireBuf and idTable are the canonical LEB128 and first-use id-table halves
// of the engine's writer, enough to spell one Root wire exactly.
type wireBuf struct{ b []byte }

func (w *wireBuf) u8(v byte)    { w.b = append(w.b, v) }
func (w *wireBuf) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *wireBuf) raw(v []byte) { w.b = append(w.b, v...) }
func (w *wireBuf) leb(v uint64) {
	for v >= 0x80 {
		w.b = append(w.b, byte(v)|0x80)
		v >>= 7
	}
	w.b = append(w.b, byte(v))
}

type idTable struct {
	ids  []uint64
	slot map[uint64]uint64
}

func newIDTable(preseed []uint64) *idTable {
	t := &idTable{slot: map[uint64]uint64{}}
	for _, id := range preseed {
		t.ref(id)
	}
	return t
}

func (t *idTable) ref(id uint64) uint64 {
	if r, ok := t.slot[id]; ok {
		return r
	}
	t.ids = append(t.ids, id)
	r := uint64(len(t.ids))
	t.slot[id] = r
	return r
}

func (t *idTable) append(id uint64) {
	if id != 0 {
		t.ids = append(t.ids, id)
	}
}

// bitWriter is the message form's bit stream: bit i of the stream is bit i%8
// of byte i/8, low bit first (docs/SPEC-TABLES.md §3.3).
type bitWriter struct {
	b   []byte
	off int
}

func (w *bitWriter) put(v uint64, bits int) {
	for i := 0; i < bits; i++ {
		if w.off%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>uint(i)&1 != 0 {
			w.b[w.off/8] |= 1 << uint(w.off%8)
		}
		w.off++
	}
}

func (w *bitWriter) align() {
	for w.off%8 != 0 {
		w.put(0, 1)
	}
}

var manifest = strings.Join([]string{
	"# The non-canonical spelling vectors (docs/SPEC-TABLES.md §3, §3.1,",
	"# §3.3, §4; docs/rulings.json reads-refuse-non-canonical). The variable cases",
	"# are form-1 files of test/non-canonical/NonCanonical.schema's Root: the",
	"# canonical spelling of one value, and one file per non-canonical spelling the",
	"# rule names. The message cases are form-2 batches of its fixed table Batch:",
	"# the canonical spelling, one body that omits a declared field, and the",
	"# message-one-encoding corpus's three cases, read there under",
	"# test/message-one-encoding/One.schema.",
	"#",
	"# Columns, tab-separated: file, form, case, verdict.",
	"#",
	"variable-canonical.bin\tvariable\tcanonical\tread",
	"fields-out-of-order.bin\tvariable\tfields-out-of-order\trefused",
	"default-written.bin\tvariable\tdefault-written\trefused",
	"explicit-null-pointer.bin\tvariable\texplicit-null-pointer\trefused",
	"unused-id-entry.bin\tvariable\tunused-id-entry\trefused",
	"id-table-reordered.bin\tvariable\tid-table-reordered\trefused",
	"empty-node-table.bin\tvariable\tempty-node-table\trefused",
	"node-table-misplaced.bin\tvariable\tnode-table-misplaced\trefused",
	"union-arm-zero.bin\tvariable\tunion-arm-zero\trefused",
	"announcement.bin\tmessage\tannouncement\tread",
	"message-canonical.bin\tmessage\tcanonical\tread",
	"message-omits-field.bin\tmessage\tomits-declared-field\trefused",
	"../message-one-encoding/fields-reordered.bin\tmessage\tfields-out-of-order\trefused",
	"../message-one-encoding/explicit-default.bin\tmessage\tdefault-written\trefused",
	"../message-one-encoding/union-none.bin\tmessage\tunion-arm-zero\trefused",
}, "\n") + "\n"

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "noncanonicalgen: "+format+"\n", args...)
	os.Exit(1)
}
