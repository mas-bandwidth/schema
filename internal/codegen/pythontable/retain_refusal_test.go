package pythontable

import (
	"encoding/binary"
	"hash/crc32"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4), and a file refused
// AT ITS FRAME, before any body is walked, is such a load: a form byte this
// reader does not take, a file too short to carry its id trailer, a failed
// integrity check. The retaining load hands the caller's store back empty on
// every refusal, not only on an accepted read: before this, load_retain reset
// the store inside the capture walk, which runs only after the plain read
// accepted, so a refused file left an earlier load's captures (its used and
// id_used counts, its loaded root) in the store.
//
// The committed corpus cases are the framing rows
// testdata/wire/tables/framing/frame_form_three_at_variable_root.bin (a
// fixed-form header at the variable root graphdemo ListNode) and
// node_count_disagrees.bin (body damage), beside node_clean.bin, their
// accepted twin on the same root.

// TestPythonRetainingLoadResetsBeforeItOpensTheFile is the fast half: the
// emitted load_retain empties the store before the integrity check and the
// file read, so no refusal can leave it as an earlier load left it.
func TestPythonRetainingLoadResetsBeforeItOpensTheFile(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, schema, open string }{
		{"plain", "package probe\ntable Root { n int32 }\n", "return load_table(cls, data, report, retain)"},
		{"crc32c", "package probe\ntable Root | integrity = \"crc32c\" { n int32 }\n", "data = _s_open_crc(data)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			files, err := Generate(loadUnit(t, c.schema))
			if err != nil {
				t.Fatal(err)
			}
			var all strings.Builder
			for _, data := range files {
				all.Write(data)
			}
			src := all.String()
			start := strings.Index(src, "    def load_retain(cls, data: bytes | bytearray | memoryview, retain: TableRetain")
			if start < 0 {
				t.Fatal("the retaining load is not emitted")
			}
			body := src[start:]
			if end := strings.Index(body, "\n    def measure_retain("); end >= 0 {
				body = body[:end]
			}
			reset := strings.Index(body, "retain.reset()")
			open := strings.Index(body, c.open)
			if open < 0 {
				t.Fatalf("the retaining load does not open the file with %q:\n%s", c.open, body)
			}
			if reset < 0 || reset > open {
				t.Fatalf("the retaining load opens the file before it empties the store:\n%s", body)
			}
		})
	}
}

// TestPythonRetainingLoadEmptiesTheStoreOnARefusal is the run-time proof, on
// both pinned interpreters: a store an earlier load left a capture in is
// handed back empty by every refused retaining load, and an accepted load of
// the clean twin is the control.
func TestPythonRetainingLoadEmptiesTheStoreOnARefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	root := filepath.Join("..", "..", "..")
	graph, err := Generate(loadUnitPaths(t, filepath.Join(root, "tables", "pointers")))
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, name := range []string{"frame_form_three_at_variable_root", "node_count_disagrees", "node_clean"} {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "framing", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	graphScript := pyrun.ImportPrelude + `from graphdemo import ListNode, TableRetain, Refused
def seeded():
    keep=TableRetain(bytearray(4096),bytearray(24*16))
    keep.used=7; keep.id_used=1; keep.loaded_root='an earlier load'
    return keep
def empty(keep):
    return (keep.used,keep.id_used,keep.loaded_root)==(0,0,None)
def read(name):
    with open(name,'rb') as f: return f.read()
cases=[(name,read(name)) for name in ('frame_form_three_at_variable_root','node_count_disagrees')]
cases+=[('form_two',bytes([2,0])+bytes(8)),('empty',b''),('short',bytes([1,0]))]
for name,wire in cases:
    keep=seeded()
    try: ListNode.load_retain(wire,keep)
    except Refused: pass
    else: raise AssertionError(name+': the retaining load accepted')
    assert empty(keep),(name,keep.used,keep.id_used,keep.loaded_root)
keep=seeded()
value=ListNode.load_retain(read('node_clean'),keep)
assert keep.used==0 and keep.id_used==0 and keep.loaded_root is value,(keep.used,keep.id_used)
print('retain-refusal')
`
	const crcSchema = "package probe\ntable Root | integrity = \"crc32c\" { n int32 }\n"
	crc, err := Generate(loadUnit(t, crcSchema))
	if err != nil {
		t.Fatal(err)
	}
	w := &unionWire{}
	body := w.file(unionCat(w.ref("n"), []byte{4}, unionI32(7)), unionCat(w.ref("future"), []byte{4}, unionI32(12)))
	sealed := binary.LittleEndian.AppendUint32(body, crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))
	crcScript := pyrun.ImportPrelude + `from probe import Root, TableRetain, Refused
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(1024),bytearray(24*8))
value=Root.load_retain(wire,keep)
assert keep.used>0 and keep.loaded_root is value,keep.used
keep.id_used=1
bad=bytearray(wire); bad[-1]^=1
try: Root.load_retain(bytes(bad),keep)
except Refused as err: assert err.reason=='integrity_mismatch',err.reason
else: raise AssertionError('the retaining load accepted a failed integrity check')
assert (keep.used,keep.id_used,keep.loaded_root)==(0,0,None),(keep.used,keep.id_used,keep.loaded_root)
print('retain-refusal')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		for _, run := range []struct {
			files  map[string][]byte
			script string
			inputs map[string][]byte
		}{
			{graph, graphScript, inputs},
			{crc, crcScript, map[string][]byte{"wire": sealed}},
		} {
			dir := t.TempDir()
			writeTree(t, dir, run.files)
			in := map[string][]byte{"main.py": []byte(run.script)}
			maps.Copy(in, run.inputs)
			out, stderr, err := pyrun.Try(t, pin, dir, "main.py", in)
			if err != nil || strings.TrimSpace(out) != "retain-refusal" {
				t.Fatalf("%s: a refused retaining load left the store as it was: %v\n%s\n%s", pin, err, out, stderr)
			}
		}
	}
}
