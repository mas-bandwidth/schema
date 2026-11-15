// pointertargetgen writes the POINTER-TARGET-TYPE vectors
// (docs/SPEC-TABLES.md §3.1; docs/rulings.json pointer-target-type-checked):
// the four wires a reader of `test/pointer-target-type/reader` meets, one per
// case the rule separates.
//
//   - canonical.bin:        `next` names a Node record — the reader reads it.
//   - next-quant.bin:       `next` names a Quant record, a type the reader
//     DECLARES that is not the type the slot requires —
//     damage, refused.
//   - next-bytes.bin:       `next` names a `*bytes` blob record under a `*Node`
//     slot — damage, refused (a wrong-type pointer target).
//   - next-writer-only.bin: `next` names a record of the WRITER-only type
//     Extra, which the reader does not declare — read,
//     the pointer null, one `unknown`.
//
// The two wrong-type wires are input no conforming writer produces, so the
// engine cannot write them: they are assembled here by hand, in the same
// framing the engine's own Encode writes, and the manual assembly is held to
// the engine byte for byte on the canonical case before either is trusted.
// Every length and reference is computed, never typed, so the files stay
// exact.
//
//	go run ./test/pointer-target-type/gen --out testdata/wire/pointer-target-type
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

const (
	readerDir = "test/pointer-target-type/reader"
	writerDir = "test/pointer-target-type/writer"
)

func main() {
	out := flag.String("out", "", "directory to write the four .bin vectors into")
	flag.Parse()
	if *out == "" {
		fatal("pointertargetgen needs --out <directory>")
	}

	reader := load(*out, readerDir)
	writer := load(*out, writerDir)

	nodeDef := reader.Lookup("Node")
	if nodeDef == nil {
		fatal("%s declares no table Node", readerDir)
	}
	if len(nodeDef.Fields) != 2 || nodeDef.Fields[0].Name != "value" || nodeDef.Fields[1].Name != "next" {
		fatal("%s's Node must be `value uint32; next *Node`", readerDir)
	}
	valueID := ir.TableFieldWireId(nodeDef.Fields[0])
	nextID := ir.TableFieldWireId(nodeDef.Fields[1])
	nodeTypeID := ir.TableWireId(nodeDef.WireName())

	// THE CANONICAL WIRE: `next` names a Node record. Assembled by hand and
	// held to the engine's own Encode of the same value byte for byte, so the
	// shared framing below is known exact before the two wrong-type wires
	// reuse it.
	manual := encodeRoot(valueID, nextID, 7, nodeTypeID, func(ids *genIDTable) []byte {
		b := &wireBuf{}
		b.leb(ids.ref(valueID))
		b.u8(byte(ir.TableKindU32))
		b.u32(9)
		b.raw([]byte{0x00}) // the record's terminator; `next` is null and elides
		return b.b
	})
	nodeRecord := reader.New(nodeDef)
	setField(nodeRecord, "value", 9)
	engine := encodeNode(reader, 7, nodeRecord)
	if !bytes.Equal(manual, engine) {
		fatal("the manual assembly disagrees with the engine's Encode:\n manual %x\nengine %x", manual, engine)
	}

	// NEXT NAMES A QUANT RECORD: a type the reader declares that is not the
	// type the slot requires.
	quantDef := reader.Lookup("Quant")
	if quantDef == nil || len(quantDef.Fields) != 1 || quantDef.Fields[0].Name != "q" {
		fatal("%s's Quant must be `q uint32`", readerDir)
	}
	qID := ir.TableFieldWireId(quantDef.Fields[0])
	nextQuant := encodeRoot(valueID, nextID, 7, ir.TableWireId(quantDef.WireName()), func(ids *genIDTable) []byte {
		b := &wireBuf{}
		b.leb(ids.ref(qID))
		b.u8(byte(ir.TableKindU32))
		b.u32(5)
		b.raw([]byte{0x00})
		return b.b
	})

	// NEXT NAMES A `*bytes` BLOB RECORD under a `*Node` slot: a blob record is
	// its bytes, with no fields and no terminator (§2.5).
	nextBytes := encodeRoot(valueID, nextID, 7, ir.BytesWireTypeId, func(ids *genIDTable) []byte {
		return []byte{0xAB, 0xCD, 0xEF}
	})

	// NEXT NAMES A RECORD OF THE WRITER-ONLY TYPE: the writer's Encode, read
	// by the reader as a type it does not declare.
	extraDef := writer.Lookup("Extra")
	if extraDef == nil {
		fatal("%s declares no table Extra", writerDir)
	}
	extra := writer.New(extraDef)
	setField(extra, "x", 5)
	nextWriterOnly := encodeNode(writer, 7, extra)

	write(*out, "canonical.bin", manual)
	write(*out, "next-quant.bin", nextQuant)
	write(*out, "next-bytes.bin", nextBytes)
	write(*out, "next-writer-only.bin", nextWriterOnly)
}

// encodeRoot assembles one reader-unit wire: the form byte, a Node root whose
// `value` is `rootValue` and whose `next` names the first (and only) record,
// the node table framing that record, the terminator, and the id table in
// first-use order. `recordBody` writes the record's body against the shared id
// table, so a body field interns its id after the record's own type id, as the
// engine's writer does.
func encodeRoot(valueID, nextID uint64, rootValue uint32, recordTypeID uint64, recordBody func(*genIDTable) []byte) []byte {
	ids := &genIDTable{slot: map[uint64]uint64{}}
	fields := &wireBuf{}
	fields.leb(ids.ref(valueID))
	fields.u8(byte(ir.TableKindU32))
	fields.u32(rootValue)
	fields.leb(ids.ref(nextID))
	fields.u8(byte(ir.TableKindPointer))
	fields.leb(2) // node index 2: the first record
	fields.leb(ids.ref(ir.TableNodeWireId))
	fields.u8(byte(ir.TableKindString))
	payload := &wireBuf{}
	payload.leb(1) // node_count
	payload.leb(ids.ref(recordTypeID))
	body := recordBody(ids)
	payload.leb(uint64(len(body)))
	payload.raw(body)
	fields.leb(uint64(len(payload.b)))
	fields.raw(payload.b)

	out := []byte{byte(ir.TableWireForm)}
	out = append(out, fields.b...)
	out = append(out, 0x00) // the root body's terminator
	for _, id := range ids.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	out = binary.LittleEndian.AppendUint64(out, uint64(len(ids.ids)))
	return out
}

// encodeNode is the engine's own save of a Node root whose `value` is
// `rootValue` and whose `next` names one record of the given instance. It is
// the reference the manual assembly is held to.
func encodeNode(m *tabletext.Model, rootValue uint32, next *tabletext.Instance) []byte {
	root := m.New(m.Lookup("Node"))
	setField(root, "value", rootValue)
	nf, ok := root.FieldByKey("next")
	if !ok {
		fatal("Node has no field next")
	}
	nf.Cell.Node = next
	wire, err := tablewire.Encode(m, root)
	if err != nil {
		fatal("Encode: %v", err)
	}
	return wire
}

func setField(inst *tabletext.Instance, key string, v uint32) {
	fv, ok := inst.FieldByKey(key)
	if !ok {
		fatal("%s has no field %s", inst.Def.Name, key)
	}
	fv.Cell.U = uint64(v)
	fv.Cell.I = int64(v)
}

// load compiles one unit and binds its model.
func load(out, dir string) *tabletext.Model {
	c := compiler.New()
	c.FormatInPlace = false
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		fatal("%s: %v", dir, err)
	}
	u, err := c.Load(paths)
	if err != nil {
		fatal("%s: %v", dir, err)
	}
	return tabletext.NewModel(u)
}

// write writes one vector file.
func write(out, name string, data []byte) {
	if err := os.WriteFile(filepath.Join(out, name), data, 0o644); err != nil {
		fatal("%s: %v", name, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pointertargetgen: "+format+"\n", args...)
	os.Exit(1)
}

// wireBuf and genIDTable are the one-file writer's half of the engine's
// encoder: canonical LEB128, a first-use id table, and the fixed little-endian
// widths — enough to spell a Node root and its one record exactly.

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

type genIDTable struct {
	ids  []uint64
	slot map[uint64]uint64
}

func (t *genIDTable) ref(id uint64) uint64 {
	if r, ok := t.slot[id]; ok {
		return r
	}
	t.ids = append(t.ids, id)
	r := uint64(len(t.ids))
	t.slot[id] = r
	return r
}
