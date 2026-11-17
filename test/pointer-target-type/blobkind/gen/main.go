// blobkindgen writes the BLOB-KIND vectors (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused): the five wires a
// reader of `test/pointer-target-type/blobkind/reader` meets, one per case the
// rule separates. Text's closure holds only a `*string` edge and Bin's only a
// `*bytes` edge, so each wrong-kind record is one of a kind the root's closure
// lacks.
//
//   - text-canonical.bin:           Text.body names a `*string` record — read.
//   - text-names-bytes.bin:         Text.body names a `*bytes` record —
//     damage, refused (a wrong-type pointer target).
//   - data-names-string.bin:        Bin.data names a `*string` record —
//     damage, refused.
//   - text-unknown-field-bytes.bin: the writer-only field Text.extra names a
//     `*bytes` record and `body` is null — read,
//     two `unknown`: the newer writer's field
//     and the record it names.
//   - text-orphan-bytes.bin:        `body` is null and one `*bytes` record no
//     pointer names — damage, refused.
//
// The wrong-kind and orphan wires are input no conforming writer produces, so
// the engine cannot write them: they are assembled here by hand, in the same
// framing the engine's own Encode writes, and the manual assembly is held to
// the engine byte for byte on both canonical shapes (a `*string` record under
// Text, a `*bytes` record under Bin) before any of them is trusted. The two
// wrong-kind records hold plain ASCII, so a reader that ignored the record's
// type id would ACCEPT them: the type id alone refuses them. Every length and
// reference is computed, never typed.
//
//	go run ./test/pointer-target-type/blobkind/gen --out testdata/wire/pointer-target-type/blobkind
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
	readerDir = "test/pointer-target-type/blobkind/reader"
	writerDir = "test/pointer-target-type/blobkind/writer"
)

// noSlot is the root that writes no pointer field: its one record is named by
// nothing.
const noSlot = 0

func main() {
	out := flag.String("out", "", "directory to write the five .bin vectors into")
	flag.Parse()
	if *out == "" {
		fatal("blobkindgen needs --out <directory>")
	}

	reader := load(readerDir)
	writer := load(writerDir)

	textID := onlyField(reader, "Text", "body")
	dataID := onlyField(reader, "Bin", "data")

	ascii := []byte("hello")
	notText := []byte{0xAB, 0xCD, 0xEF}

	// THE CANONICAL WIRES: assembled by hand and held to the engine's own
	// Encode of the same value byte for byte, so the framing below is known
	// exact for both blob kinds before the wrong-kind wires reuse it.
	textCanonical := assemble(textID, ir.StringWireTypeId, ascii)
	if engine := encodeBlob(reader, "Text", "body", ascii); !bytes.Equal(textCanonical, engine) {
		fatal("the manual Text assembly disagrees with the engine's Encode:\n manual %x\nengine %x", textCanonical, engine)
	}
	binCanonical := assemble(dataID, ir.BytesWireTypeId, notText)
	if engine := encodeBlob(reader, "Bin", "data", notText); !bytes.Equal(binCanonical, engine) {
		fatal("the manual Bin assembly disagrees with the engine's Encode:\n manual %x\nengine %x", binCanonical, engine)
	}

	// A `*string` SLOT NAMING A `*bytes` RECORD, and a `*bytes` slot naming a
	// `*string` record: each root's closure lacks the record's kind.
	textNamesBytes := assemble(textID, ir.BytesWireTypeId, ascii)
	dataNamesString := assemble(dataID, ir.StringWireTypeId, ascii)

	// A NEWER WRITER'S FIELD names the `*bytes` record: the writer's own
	// Encode, read by the reader as a field it does not declare.
	textUnknownField := encodeBlob(writer, "Text", "extra", notText)

	// A `*bytes` RECORD NO POINTER NAMES.
	textOrphan := assemble(noSlot, ir.BytesWireTypeId, ascii)

	write(*out, "text-canonical.bin", textCanonical)
	write(*out, "text-names-bytes.bin", textNamesBytes)
	write(*out, "data-names-string.bin", dataNamesString)
	write(*out, "text-unknown-field-bytes.bin", textUnknownField)
	write(*out, "text-orphan-bytes.bin", textOrphan)
}

// onlyField is the wire id of the one pointer field a reader root declares.
func onlyField(m *tabletext.Model, table, field string) uint64 {
	def := m.Lookup(table)
	if def == nil || len(def.Fields) != 1 || def.Fields[0].Name != field {
		fatal("%s's %s must declare exactly one field, %s", readerDir, table, field)
	}
	return ir.TableFieldWireId(def.Fields[0])
}

// assemble spells one reader-unit wire: the form byte, a root body whose one
// pointer field (`slotID`, or none for noSlot) names the first and only record,
// the node table framing that record, the terminator, and the id table in
// first-use order. A blob record is its bytes, with no fields and no
// terminator (§2.5).
func assemble(slotID, recordTypeID uint64, body []byte) []byte {
	ids := &genIDTable{slot: map[uint64]uint64{}}
	fields := &wireBuf{}
	if slotID != noSlot {
		fields.leb(ids.ref(slotID))
		fields.u8(byte(ir.TableKindPointer))
		fields.leb(2) // node index 2: the first record
	}
	fields.leb(ids.ref(ir.TableNodeWireId))
	fields.u8(byte(ir.TableKindString))
	payload := &wireBuf{}
	payload.leb(1) // node_count
	payload.leb(ids.ref(recordTypeID))
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

// encodeBlob is the engine's own save of one root whose blob field `field`
// holds `data` and whose other fields are null.
func encodeBlob(m *tabletext.Model, table, field string, data []byte) []byte {
	def := m.Lookup(table)
	if def == nil {
		fatal("no table %s", table)
	}
	root := m.New(def)
	fv, ok := root.FieldByKey(field)
	if !ok {
		fatal("%s has no field %s", table, field)
	}
	fv.Cell.Blob = &tabletext.Blob{Data: append([]byte(nil), data...)}
	wire, err := tablewire.Encode(m, root)
	if err != nil {
		fatal("Encode %s: %v", table, err)
	}
	return wire
}

// load compiles one unit and binds its model.
func load(dir string) *tabletext.Model {
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
	fmt.Fprintf(os.Stderr, "blobkindgen: "+format+"\n", args...)
	os.Exit(1)
}

// wireBuf and genIDTable are the one-file writer's half of the engine's
// encoder: canonical LEB128, a first-use id table, and the fixed little-endian
// widths — enough to spell a root and its one record exactly.

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
