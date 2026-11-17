package lobstertable_test

import (
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
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestLobsterPointerTargetBlobKindRefused holds the Lobster leg to every row
// of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv
// (docs/SPEC-TABLES.md §3.1, §6.5; docs/rulings.json
// blob-target-outside-closure-refused): the two reserved blob ids are types
// every reader of the variable form can name, whatever its root reaches, so a
// blob record named from a pointer slot this reader declares whose target is
// not that blob kind refuses the whole read. A blob record of a kind the
// root's closure lacks, named only from a field this reader does not declare,
// reads and counts unknown; a blob record no pointer reaches is damage.
//
// Every row runs through the plain load, the strict read, the retaining load
// and the measure. The same test confirms the leg's own save never writes a
// blob record no pointer reaches: each saved wire is decoded by the engine
// and must be accepted with nothing unknown. A negative control plants the
// old root-closure rule in a copy of the emitted reader and must go red by
// name on text-names-bytes.
func TestLobsterPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain (it runs the generated unit)")

	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatalf("vectors manifest: %v", err)
	}
	rows := blobKindRows(t, string(manifest))
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}

	byUnit := map[string][]blobKindRow{}
	var units []string
	for _, row := range rows {
		if _, seen := byUnit[row.unit]; !seen {
			units = append(units, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}

	inputs := map[string][]byte{}
	for _, row := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		inputs[row.name] = wire
	}

	var readerDir string
	for _, unit := range units {
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, unit)})
		if err != nil {
			t.Fatal(err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("%s: the surface refuses the unit: %v", unit, err)
		}
		if readerDir == "" {
			readerDir = g.dir
		}
		program := blobKindReadProgram(byUnit[unit])
		if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program), 0o644); err != nil {
			t.Fatal(err)
		}
		out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)))
		if !strings.HasSuffix(out, "blob kinds passed") || strings.Contains(out, "FAIL") {
			t.Fatalf("%s: %s", unit, out)
		}
	}

	t.Run("negative control: the root-closure rule misreads text-names-bytes", func(t *testing.T) {
		control := copyLobsterDir(t, readerDir)
		path := filepath.Join(control, "load.lobster")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		const anchor = "            elif rec.tid == string_blob_id or rec.tid == bytes_blob_id or declares(root.d, rec.tid):\n"
		const planted = "            elif declares(root.d, rec.tid):\n"
		if n := strings.Count(string(src), anchor); n != 1 {
			t.Fatalf("the reserved-id naming anchor is in the emitted runtime %d times, want 1", n)
		}
		if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, planted, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		program := blobKindReadProgram(byUnit[units[0]])
		if err := os.WriteFile(filepath.Join(control, "main.lobster"), []byte(program), 0o644); err != nil {
			t.Fatal(err)
		}
		out := string(lobsterrun.Run(t, control, "main.lobster", inputs))
		if !strings.Contains(out, "FAIL text-names-bytes") {
			t.Fatalf("the planted root-closure rule did not go red on text-names-bytes:\n%s", out)
		}
	})

	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		writerDir := filepath.Join(root, "test/pointer-target-type/blobkind/writer")
		paths, err := compiler.GatherPaths([]string{writerDir})
		if err != nil {
			t.Fatal(err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("the surface refuses the writer unit: %v", err)
		}
		if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(blobKindSaveProgram(g)), 0o644); err != nil {
			t.Fatal(err)
		}
		out := lobsterrun.Run(t, g.dir, "main.lobster", nil)
		saved := lobsterrun.HexLines(t, out)
		if len(saved) != 4 {
			t.Fatalf("the writer printed %d saved wires, want 4", len(saved))
		}

		reader := blobKindModel(t, filepath.Join(root, rows[0].unit))
		cases := []struct{ root, field string }{{"Text", "body"}, {"Bin", "data"}}
		i := 0
		for _, c := range cases {
			for _, set := range []bool{true, false} {
				wire := saved[i]
				i++
				inst := reader.New(reader.Lookup(c.root))
				var report tabletext.Report
				ok, err := tablewire.Decode(reader, inst, wire, &report)
				if !ok || err != nil || report.Refused || report.Unknown != 0 {
					t.Errorf("%s.%s set=%v: the saved wire is not accepted clean: %v %+v", c.root, c.field, set, err, report)
					continue
				}
				fv, ok := inst.FieldByKey(c.field)
				if !ok {
					t.Fatalf("%s has no field %s", c.root, c.field)
				}
				if set {
					if fv.Cell.Blob == nil || string(fv.Cell.Blob.Data) != "hello" {
						t.Errorf("%s.%s set: read back %+v, want the saved blob", c.root, c.field, fv.Cell.Blob)
					}
				} else if fv.Cell.Blob != nil || fv.Cell.Node != nil {
					t.Errorf("%s.%s null: read back a pointer, want null", c.root, c.field)
				}
			}
		}
	})
}

// blobKindRow is one manifest line: the root to read at, the wire file, and
// the verdict and unknown count the engine answers.
type blobKindRow struct {
	name, unit, root, file string
	read                   bool
	unknown                int
}

func blobKindRows(t *testing.T, manifest string) []blobKindRow {
	t.Helper()
	var rows []blobKindRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := blobKindRow{name: f[0], unit: f[1], root: f[2], file: f[3], unknown: -1}
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

// blobKindReadProgram checks each row through every read entry: the verdict,
// the unknown count where the row pins one, and, for a read row that counts
// unknown records, that every pointer slot the reader declares stays null.
func blobKindReadProgram(rows []blobKindRow) string {
	var b strings.Builder
	b.WriteString(blobKindProgram)
	for _, row := range rows {
		fmt.Fprintf(&b, "check(%q, %q, %v, %d)\n", row.name, row.root, row.read, row.unknown)
	}
	b.WriteString("print(if failures == 0: \"blob kinds passed\" else: \"blob kinds failed\")\n")
	return b.String()
}

const blobKindProgram = `import .ReaderTable
import .strict_and_refusals
import .retain_unknown
import .region_measure

var failures = 0

def fail(name:string, entry:string, what:string):
    print("FAIL " + name + " " + entry + ": " + what)
    failures++

def pointers_null(rec:schema_rt.Record) -> bool:
    var all = true
    for(rec.fields) sl:
        if sl.cell.node or sl.cell.blob:
            all = false
    return all

def held(name:string, entry:string, rec:schema_rt.Record?, rep:schema_rt.LoadReport, read:bool, unknown:int):
    if not read:
        if rec:
            fail(name, entry, "a refused row read")
        return
    if not rec:
        fail(name, entry, "a read row refused: " + schema_rt.strict_reason(rep))
        return
    if unknown >= 0 and rep.unknown != unknown:
        fail(name, entry, "unknown " + string(rep.unknown) + ", want " + string(unknown))
    if unknown > 0 and not pointers_null(rec):
        fail(name, entry, "a pointer read a record the reader cannot declare")

def check(name:string, root:string, read:bool, unknown:int):
    let data = read_file(name)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load(root, data, rep)
    held(name, "load", rec, rep, read, unknown)
    schema_rt.release(rec)
    let sr = schema_rt.LoadReport {}
    let strict, reason = schema_rt.load_strict(root, data, sr)
    held(name, "strict", strict, sr, read, unknown)
    if read != (reason == ""):
        fail(name, "strict", "reason " + reason)
    schema_rt.release(strict)
    let rr = schema_rt.LoadReport {}
    let retained = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
    let rrec = schema_rt.load_retain(root, data, retained, rr)
    held(name, "retain", rrec, rr, read, unknown)
    schema_rt.release(rrec)
    let n, whole, bad, why = schema_rt.load_measure(root, data)
    if read and (n < 0 or bad or why != ""):
        fail(name, "measure", "a read row refused: " + why)
    if not read and (n != -1 or whole or not bad or why == ""):
        fail(name, "measure", "a refused row measured " + string(n))

`

// blobKindSaveProgram saves a Text and a Bin value with the blob set and with
// it null. The writer's Text declares a field the reader does not (`extra
// *bytes`); it stays null, so a conforming save elides it and the wire holds
// no record no pointer reaches.
func blobKindSaveProgram(g *loadUnit) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(blobKindSave)
	return b.String()
}

const blobKindSave = `import .schema_save

def hexs(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)

def save_one(root:string, field:string, set:bool):
    let rec = schema_rt.new_record(schema_rt.table_of(root))
    for(rec.fields) sl:
        if sl.fd.name == field and set:
            sl.cell.blob = "hello"
    let out = schema_rt.save(rec)
    print(hexs(out))

save_one("Text", "body", true)
save_one("Text", "body", false)
save_one("Bin", "data", true)
save_one("Bin", "data", false)
`

// blobKindModel compiles one unit into the engine's value model, so a saved
// wire can be decoded by internal/tablewire.
func blobKindModel(t *testing.T, dir string) *tabletext.Model {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return tabletext.NewModel(u)
}

// copyLobsterDir copies every generated file into a fresh directory, so a
// negative control edits a copy and never the clean unit.
func copyLobsterDir(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}
