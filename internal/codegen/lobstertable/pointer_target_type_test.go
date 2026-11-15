package lobstertable_test

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestLobsterPointerTargetTypeChecked holds every Lobster read of the variable
// form to docs/SPEC-TABLES.md §3.1's pointer-target rule (docs/rulings.json
// pointer-target-type-checked): a pointer slot naming a record whose type the
// reader DECLARES but that is not the type the slot requires is damage and the
// whole load refuses; a record of a type the reader cannot name is the lawful
// newer-writer case, counted unknown once with every pointer naming it null.
// The rows are testdata/wire/pointer-target-type/ and its manifest, plus two
// nested rows built here (a Node record whose own next names a Quant record,
// and one whose next names a writer-only record), so a pointer inside a node
// record is held to the rule as well as the root's. Each row runs through the
// plain load, the strict read, the retaining load and the measure.
func TestLobsterPointerTargetTypeChecked(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain (it runs the generated unit)")

	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := pointerTargetRows(t, string(manifest))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	unit := rows[0].unit
	inputs := map[string][]byte{}
	for i := range rows {
		if rows[i].unit != unit || rows[i].root != "Node" {
			t.Fatalf("row %s: unit %s root %s; this test reads every row as Node of %s", rows[i].name, rows[i].unit, rows[i].root, unit)
		}
		wire, err := os.ReadFile(filepath.Join(dir, rows[i].file))
		if err != nil {
			t.Fatalf("%s: %v", rows[i].file, err)
		}
		inputs[rows[i].name] = wire
		if rows[i].unknown > 0 {
			rows[i].nullAt = 1
		}
	}

	reader := pointerTargetModel(t, filepath.Join(root, unit))
	writer := pointerTargetModel(t, filepath.Join(root, "test", "pointer-target-type", "writer"))
	inputs["nested-quant"] = nestedPointerTargetWire(t, reader, reader.Lookup("Quant"), "q")
	inputs["nested-writer-only"] = nestedPointerTargetWire(t, reader, writer.Lookup("Extra"), "x")
	rows = append(rows,
		pointerTargetRow{name: "nested-quant", read: false, unknown: -1},
		pointerTargetRow{name: "nested-writer-only", read: true, unknown: 1, nullAt: 2},
	)

	paths, err := compiler.GatherPaths([]string{filepath.Join(root, unit)})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	var program strings.Builder
	program.WriteString(pointerTargetProgram)
	for _, row := range rows {
		fmt.Fprintf(&program, "check(%q, %v, %d, %d)\n", row.name, row.read, row.unknown, row.nullAt)
	}
	program.WriteString("print(if failures == 0: \"pointer targets passed\" else: \"pointer targets failed\")\n")
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)))
	if !strings.HasSuffix(out, "pointer targets passed") || strings.Contains(out, "FAIL") {
		t.Fatalf("%s", out)
	}
}

type pointerTargetRow struct {
	name, unit, root, file string
	read                   bool
	unknown                int // -1 when the row does not pin a count
	nullAt                 int // the depth of the Node whose next must read null; 0 for none
}

func pointerTargetRows(t *testing.T, manifest string) []pointerTargetRow {
	t.Helper()
	var rows []pointerTargetRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := pointerTargetRow{name: f[0], unit: f[1], root: f[2], file: f[3], unknown: -1}
		switch f[4] {
		case "read":
			row.read = true
		case "refused":
		default:
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[4])
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil || n < 0 {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q, want a count or -", i+1, f[5])
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}

func pointerTargetModel(t *testing.T, dir string) *tabletext.Model {
	t.Helper()
	c := compiler.New()
	c.FormatInPlace = false
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return tabletext.NewModel(u)
}

// nestedPointerTargetWire assembles a reader-unit wire one level deeper than
// the manifest's: a Node root (value 7) whose next names node 2, a Node record
// (value 9) whose next names node 3, and node 3 a record of the type def with
// its one uint32 field set to 5. It is the framing of
// test/pointer-target-type/gen, which is held to the engine's own Encode.
func nestedPointerTargetWire(t *testing.T, reader *tabletext.Model, def *ir.Struct, field string) []byte {
	t.Helper()
	node := reader.Lookup("Node")
	if node == nil || len(node.Fields) != 2 || node.Fields[0].Name != "value" || node.Fields[1].Name != "next" {
		t.Fatal("the reader unit's Node must be `value uint32; next *Node`")
	}
	if def == nil || len(def.Fields) != 1 || def.Fields[0].Name != field {
		t.Fatalf("the target type must be one uint32 field %s", field)
	}
	valueID := ir.TableFieldWireId(node.Fields[0])
	nextID := ir.TableFieldWireId(node.Fields[1])
	ids := &ptIDs{slot: map[uint64]uint64{}}
	var root ptBuf
	root.leb(ids.ref(valueID))
	root.u8(byte(ir.TableKindU32))
	root.u32(7)
	root.leb(ids.ref(nextID))
	root.u8(byte(ir.TableKindPointer))
	root.leb(2)
	root.leb(ids.ref(ir.TableNodeWireId))
	root.u8(byte(ir.TableKindString))
	var payload ptBuf
	payload.leb(2) // node_count
	payload.leb(ids.ref(ir.TableWireId(node.WireName())))
	var mid ptBuf
	mid.leb(ids.ref(valueID))
	mid.u8(byte(ir.TableKindU32))
	mid.u32(9)
	mid.leb(ids.ref(nextID))
	mid.u8(byte(ir.TableKindPointer))
	mid.leb(3)
	mid.u8(0)
	payload.leb(uint64(len(mid.b)))
	payload.raw(mid.b)
	payload.leb(ids.ref(ir.TableWireId(def.WireName())))
	var leaf ptBuf
	leaf.leb(ids.ref(ir.TableFieldWireId(def.Fields[0])))
	leaf.u8(byte(ir.TableKindU32))
	leaf.u32(5)
	leaf.u8(0)
	payload.leb(uint64(len(leaf.b)))
	payload.raw(leaf.b)
	root.leb(uint64(len(payload.b)))
	root.raw(payload.b)
	out := []byte{byte(ir.TableWireForm)}
	out = append(out, root.b...)
	out = append(out, 0x00)
	for _, id := range ids.ids {
		out = binary.LittleEndian.AppendUint64(out, id)
	}
	return binary.LittleEndian.AppendUint64(out, uint64(len(ids.ids)))
}

type ptBuf struct{ b []byte }

func (w *ptBuf) u8(v byte)    { w.b = append(w.b, v) }
func (w *ptBuf) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *ptBuf) raw(v []byte) { w.b = append(w.b, v...) }
func (w *ptBuf) leb(v uint64) {
	for v >= 0x80 {
		w.b = append(w.b, byte(v)|0x80)
		v >>= 7
	}
	w.b = append(w.b, byte(v))
}

type ptIDs struct {
	ids  []uint64
	slot map[uint64]uint64
}

func (t *ptIDs) ref(id uint64) uint64 {
	if r, ok := t.slot[id]; ok {
		return r
	}
	t.ids = append(t.ids, id)
	t.slot[id] = uint64(len(t.ids))
	return uint64(len(t.ids))
}

// pointerTargetProgram checks one row through every read entry: the verdict,
// the unknown count where the row pins one, and the null pointer at depth
// null_at (the root is depth 1) where the row's record is a writer-only type.
const pointerTargetProgram = `import .ReaderTable
import .strict_and_refusals
import .retain_unknown
import .region_measure

var failures = 0

def fail(name:string, entry:string, what:string):
    print("FAIL " + name + " " + entry + ": " + what)
    failures++

def next_null(rec:schema_rt.Record, depth:int) -> bool:
    var at = rec
    var d = depth
    while d > 1:
        let n = at.fields[1].cell.node
        if not n:
            return false
        at = n
        d--
    return not at.fields[1].cell.node

def held(name:string, entry:string, rec:schema_rt.Record?, rep:schema_rt.LoadReport, read:bool, unknown:int, null_at:int):
    if not read:
        if rec:
            fail(name, entry, "a refused row read")
        return
    if not rec:
        fail(name, entry, "a read row refused: " + schema_rt.strict_reason(rep))
        return
    if unknown >= 0 and rep.unknown != unknown:
        fail(name, entry, "unknown " + string(rep.unknown) + ", want " + string(unknown))
    if null_at > 0 and not next_null(rec, null_at):
        fail(name, entry, "the pointer does not read null")

def check(name:string, read:bool, unknown:int, null_at:int):
    let data = read_file(name)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load("Node", data, rep)
    held(name, "load", rec, rep, read, unknown, null_at)
    schema_rt.release(rec)
    let sr = schema_rt.LoadReport {}
    let strict, reason = schema_rt.load_strict("Node", data, sr)
    held(name, "strict", strict, sr, read, unknown, null_at)
    if read != (reason == ""):
        fail(name, "strict", "reason " + reason)
    schema_rt.release(strict)
    let rr = schema_rt.LoadReport {}
    let retained = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
    let rrec = schema_rt.load_retain("Node", data, retained, rr)
    held(name, "retain", rrec, rr, read, unknown, null_at)
    schema_rt.release(rrec)
    let n, whole, bad, why = schema_rt.load_measure("Node", data)
    if read and (n < 0 or bad or why != ""):
        fail(name, "measure", "a read row refused: " + why)
    if not read and (n != -1 or whole or not bad or why == ""):
        fail(name, "measure", "a refused row measured " + string(n))

`
