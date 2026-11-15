package lobstertable_test

import (
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
)

// TestFixedDeadBytesAreZero holds every dead byte in a fixed record to zero
// (docs/FIXED-FORM-ALGORITHM.md §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4,
// §13.10; docs/rulings.json fixed-dead-bytes-are-zero). Each vector the
// shared corpus marks refused is refused by the generated Lobster load and
// leaves the caller's destination as it was; each control reads and saves
// back the same bytes.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpusDir(t)
	dir := deadBytesFiles(t)

	const main = `import .DeadBytesTable
def hexline(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)
def read_in() -> string:
    let f = read_file("in.bin")
    if f: return f
    return ""
let values = [tbldead.NewDeadRoot()]
let before = hexline(tbldead.DeadRootFixedSave(values))
print("before " + before)
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let report = schema_rt.TableReport {}
let n = tbldead.DeadRootFixedLoad(values, read_in(), plan, report)
if n < 0:
    print("refused " + hexline(tbldead.DeadRootFixedSave(values)))
else:
    print("read " + string(n) + " " + hexline(tbldead.DeadRootFixedSave(values)))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, vec := range fixedvector.DeadBytes {
		vec := vec
		t.Run(vec.Name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join(corpus, vec.File))
			if err != nil {
				t.Fatalf("%s: %v", vec.File, err)
			}
			out := lobsterrun.Run(t, dir, "main.lobster", map[string][]byte{"in.bin": want})
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) != 2 {
				t.Fatalf("%s: the probe printed %d lines, want 2:\n%s", vec.File, len(lines), out)
			}
			before, ok := strings.CutPrefix(lines[0], "before ")
			if !ok {
				t.Fatalf("%s: the probe printed %q, want its destination's image first", vec.File, lines[0])
			}
			if vec.Reads {
				fields := strings.Fields(lines[1])
				if len(fields) != 3 || fields[0] != "read" || fields[1] != "1" {
					t.Fatalf("%s: the control read %q, want one record", vec.File, lines[1])
				}
				got, err := hex.DecodeString(fields[2])
				if err != nil {
					t.Fatalf("%s: the probe's hex: %v", vec.File, err)
				}
				if string(got) != string(want) {
					t.Errorf("%s: the control saved %d bytes, want the file's %d", vec.File, len(got), len(want))
				}
				return
			}
			saved, ok := strings.CutPrefix(lines[1], "refused ")
			if !ok {
				t.Fatalf("%s: the load read a record with a non-zero dead byte: %s", vec.File, lines[1])
			}
			if saved != before {
				t.Errorf("%s: the refusal changed the caller's destination:\n got %s\nwas %s", vec.File, saved, before)
			}
		})
	}
}

// deadBytesFiles emits the shared dead-byte root's Lobster modules.
func deadBytesFiles(t *testing.T) string {
	t.Helper()
	schema := filepath.Join(root, "test", "tables", "DeadBytes.schema")
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
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
