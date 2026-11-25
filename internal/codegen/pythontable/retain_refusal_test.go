package pythontable

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
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
// The shared cases are the conformance `retain-refusal` rows
// (testdata/conformance/tables/MANIFEST.txt): one store through a retaining
// read, a save, a refused read (form byte 3, two bytes, no bytes, the message
// form, damage mid-body) and a second save, answered by every leg.

// TestPythonRetainResetsBeforeOpen is the fast half: the
// emitted load_retain empties the store before the integrity check and the
// file read, so no refusal can leave it as an earlier load left it.
func TestPythonRetainResetsBeforeOpen(t *testing.T) {
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

// TestPythonRetainRefusalEmptiesTheStore is the run-time proof of the one
// refusal the shared corpus cannot carry, on both pinned interpreters: a
// failed integrity check (no conformance unit declares `integrity`). The
// frame, short, empty, form-two and body refusals are the conformance
// `retain-refusal` rows every leg answers (testdata/conformance/tables/
// MANIFEST.txt, test/conformance/python/surfaces/retain-refusal.py).
func TestPythonRetainRefusalEmptiesTheStore(t *testing.T) {
	t.Parallel()
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
		dir := t.TempDir()
		writeTree(t, dir, crc)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(crcScript), "wire": sealed})
		if err != nil || strings.TrimSpace(out) != "retain-refusal" {
			t.Fatalf("%s: a refused retaining load left the store as it was: %v\n%s\n%s", pin, err, out, stderr)
		}
	}
}
