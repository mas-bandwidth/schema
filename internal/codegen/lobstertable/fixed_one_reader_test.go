package lobstertable_test

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// oneReaderByteArrays is a fixed table whose fields cover every byte-array
// shape the fixed form carries (fixed, counted, enum-keyed, nested, optional
// and a union arm).
const oneReaderByteArrays = `package bytearray
enum Key { First, Second }
type Leaf { loadout [4]uint8 }
union Choice { values [4]uint8 }
fixed table Row {
    loadout [4]uint8
    live [1..4]uint8
    keyed [Key]uint8
    nested Leaf
    optional ?Leaf
    choice Choice
    ranged [2]uint8 | min = 0, max = 200
}
`

// oneReaderDef is one generated Lobster definition: its name, its parameter
// list and its return type.
var oneReaderDef = regexp.MustCompile(`(?m)^def (\w+)\(([^)]*)\)(?: -> ([^:]+))?:`)

// TestFixedLobsterOneReader holds the Lobster fixed form to one read
// (docs/FIXED-FORM-ALGORITHM.md §4, docs/rulings.json strict-only-read): the
// generated module of a fixed table declares no definition that reads table
// records out of bytes other than <T>FixedLoad, and the callers that read a
// form-3 file go through <T>FixedLoad and pass under the pinned toolchain.
func TestFixedLobsterOneReader(t *testing.T) {
	t.Parallel()
	schemas := []string{filepath.Join(t.TempDir(), "Probe.schema")}
	if err := os.WriteFile(schemas[0], []byte(oneReaderByteArrays), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range fixedCorpus {
		schemas = append(schemas, filepath.Join(root, c.schema))
	}
	for _, schema := range schemas {
		u, tables := oneReaderGenerate(t, schema)
		structs := map[string]bool{}
		for _, f := range u.Files {
			for _, st := range f.Tables {
				structs[st.Name] = true
			}
			for _, d := range f.Decls {
				if st, ok := d.(*ir.Struct); ok {
					structs[st.Name] = true
				}
			}
		}
		loads := 0
		for name, src := range tables {
			for _, m := range oneReaderDef.FindAllStringSubmatch(string(src), -1) {
				fn, params, ret := m[1], m[2], strings.TrimSpace(m[3])
				if strings.HasSuffix(fn, "FixedLoad") {
					loads++
					continue
				}
				if strings.HasSuffix(fn, "FixedDecode") || strings.HasSuffix(fn, "FixedGet") {
					t.Errorf("%s: %s declares %s beside the strict <T>FixedLoad", schema, name, fn)
					continue
				}
				if !strings.Contains(params, ":string") || !strings.Contains(fn, "Fixed") {
					continue
				}
				if structs[strings.Trim(ret, "[]")] {
					t.Errorf("%s: %s declares %s(%s) -> %s, a second read of %s records", schema, name, fn, params, ret, strings.Trim(ret, "[]"))
				}
			}
		}
		if loads == 0 {
			t.Errorf("%s: no <T>FixedLoad was generated", schema)
		}
	}

	// The byte-oracle caller reads through <T>FixedLoad: the oracle's fx1
	// round-trips, and the same file with one trailing byte is refused whole.
	corpus := fixedCorpusDir(t)
	want, err := os.ReadFile(filepath.Join(corpus, "fx1.bin"))
	if err != nil {
		t.Fatalf("fx1.bin: %v", err)
	}
	if got := fixedRun(t, "test/tables/FX1.schema", "FxRoot", want); string(got) != string(want) {
		t.Errorf("fx1.bin: save after the one read is not the oracle's bytes (got %d, want %d)", len(got), len(want))
	}
	ragged := append(append([]byte(nil), want...), 0)
	if got := fixedRun(t, "test/tables/FX1.schema", "FxRoot", ragged); len(got) != 0 {
		t.Errorf("fx1.bin plus one byte: the one read landed it and the save wrote %d bytes; it refuses", len(got))
	}

	// The byte-array caller reads its saved file back through <T>FixedLoad.
	_, tables := oneReaderGenerate(t, schemas[0])
	dir := t.TempDir()
	for name, data := range tables {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const program = `import .ProbeTable
let bytes = write_int8_le(write_int8_le(write_int8_le(schema_rt.zeros(4), 1, 127), 2, 128), 3, 255)
let row = bytearray.NewRow()
row.Loadout = bytes
let wire = bytearray.RowFixedSave([row])
let rows = [bytearray.NewRow()]
let plan:[schema_rt.TableFixedEntry] = []
for(bytearray.RowFixedPlanMax): push(plan, schema_rt.TableFixedEntry {})
print("loaded " + string(bytearray.RowFixedLoad(rows, wire, plan, schema_rt.TableReport {}) == 1 and rows[0].Loadout == bytes))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil))); out != "loaded true" {
		t.Errorf("byte arrays through the one read: %q, want \"loaded true\"", out)
	}
}

// oneReaderGenerate compiles one schema and returns its unit and the Lobster
// files (the plain modules and the table modules) it generates.
func oneReaderGenerate(t *testing.T, schema string) (*ir.Unit, map[string][]byte) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{schema})
	if err != nil {
		t.Fatalf("%s: %v", schema, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s does not load: %v", schema, err)
	}
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatalf("%s: lobster: %v", schema, err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatalf("%s: lobster tables: %v", schema, err)
	}
	maps.Copy(files, tables)
	return u, files
}
