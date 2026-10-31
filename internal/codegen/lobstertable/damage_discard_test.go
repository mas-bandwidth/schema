package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// Unlike the framing report surface, these probes inspect the API's returned
// values: §4 permits no partial file value or delivered message before damage.
func lobsterDamageProbe(t *testing.T, patchFile, anchor string) []string {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["backenddemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	if patchFile != "" {
		path := filepath.Join(g.dir, patchFile)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(data), anchor) != 1 {
			t.Fatalf("%s discard anchor must occur exactly once", patchFile)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(data), anchor, "", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .load
import .message
let bad = read_file("bad")
assert bad
let frep = schema_rt.LoadReport {}
let rec = schema_rt.load_tolerant("LoginRequest", bad, frep)
print(if frep.malformed and not rec: "file discarded" else: "file kept")
schema_rt.release(rec)
let ann = read_file("ann")
let wire = read_file("wire")
assert ann and wire
let vocab = schema_rt.new_vocabulary(schema_rt.message_entries_here())
let arep = schema_rt.LoadReport {}
assert schema_rt.announce_read(vocab, ann, arep)
let mrep = schema_rt.LoadReport {}
let batch:[schema_rt.Record] = []
let count, ok = schema_rt.load_messages_into("Envelope", vocab, wire, mrep, 256, batch)
print(if mrep.malformed and not ok and count == 0 and length(batch) == 0: "batch discarded" else: "batch kept")
let tail = read_file("tail")
assert tail
let trep = schema_rt.LoadReport {}
let final:[schema_rt.Record] = []
let delivered, whole = schema_rt.load_messages_into("LoginRequest", vocab, tail, trep, 256, final)
print(if trep.malformed and not whole and delivered == 0 and length(final) == 0: "tail discarded" else: "tail kept")
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{"bad": {1}}
	for name, file := range map[string]string{"ann": "backend_conn.bin", "wire": "backend_batch_damaged_second_message.bin", "tail": "login_full_message.bin"} {
		data, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables", file))
		if err != nil {
			t.Fatal(err)
		}
		if name == "tail" {
			data = append(data, 0)
		}
		inputs[name] = data
	}
	return strings.Fields(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)))
}

func TestLobsterDamageDiscardsValues(t *testing.T) {
	t.Parallel()
	got := strings.Join(lobsterDamageProbe(t, "", ""), " ")
	if got != "file discarded batch discarded tail discarded" {
		t.Fatalf("damaged read exposed a value: %s", got)
	}
}

// Both controls remove the real discard boundary, while preserving parsing.
func TestLobsterDamageDiscardNegativeControl(t *testing.T) {
	for _, c := range []struct{ file, anchor, want string }{
		{"load.lobster", "    if report.malformed:\n        release(rec)\n        return nil\n", "file kept batch discarded tail discarded"},
		{"message.lobster", "            for(batch) value: release(value)\n            truncate(batch, 0)\n", "file discarded batch kept tail discarded"},
		{"message.lobster", "        for(batch) value: release(value)\n        truncate(batch, 0)\n", "file discarded batch discarded tail kept"},
	} {
		t.Run(c.file, func(t *testing.T) {
			got := strings.Join(lobsterDamageProbe(t, c.file, c.anchor), " ")
			if got != c.want {
				t.Fatalf("discard sabotage: got %q, want %q", got, c.want)
			}
		})
	}
}
