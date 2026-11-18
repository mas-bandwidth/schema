package lobstertable_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
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
// of testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv and to the
// three root-closure rows of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json
// (docs/SPEC-TABLES.md §3.1, §6.5; docs/rulings.json
// blob-target-outside-closure-refused): the two reserved blob ids, and every
// table the unit declares, are types every reader of the variable form can
// name, whatever its root reaches, so a record named from a pointer slot this
// reader declares whose type is not the slot's target refuses the whole read.
// A blob record of a kind the root's closure lacks, named only from a field
// this reader does not declare, reads and counts unknown; a blob record no
// pointer reaches is damage.
//
// The Go engine is the oracle: each wire is first read by internal/tablewire,
// and the leg must give the engine's verdict, and its unknown count where the
// row pins one, through the plain load, the strict read, the retaining load
// and the measure. Only the verdict of a refusal is compared; its reason is
// logged (docs/rulings.json refusal-verdict-only). The same test confirms the leg's own saves (plain and retaining)
// never write a blob record no pointer reaches: each saved wire is decoded by
// the engine and must be accepted with nothing unknown. A negative control
// plants the old root-closure rule in a copy of the emitted reader and must go
// red by name on text-names-bytes.
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

	inputs := map[string][]byte{}
	byUnit := map[string][]blobKindRow{}
	var units []string
	models := map[string]*tabletext.Model{}
	for _, row := range rows {
		if _, seen := byUnit[row.unit]; !seen {
			units = append(units, row.unit)
			models[row.unit] = blobKindModel(t, filepath.Join(root, row.unit))
		}
		wire, err := os.ReadFile(filepath.Join(dir, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.file, err)
		}
		inputs[row.name] = wire
		// the engine answers first, and must answer the row as pinned
		read, unknown, reason := engineVerdict(t, models[row.unit], row.root, wire)
		if read != row.read || (row.unknown >= 0 && unknown != row.unknown) {
			t.Fatalf("%s: the engine answers read=%v unknown=%d (%s), the manifest pins read=%v unknown=%d", row.name, read, unknown, reason, row.read, row.unknown)
		}
		row.reason = reason
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}

	var readerDir string
	var readerRows []blobKindRow
	writerModel := blobKindModel(t, filepath.Join(root, "test/pointer-target-type/blobkind/writer"))
	for _, unit := range units {
		g := blobKindUnit(t, filepath.Join(root, unit))
		if readerDir == "" {
			readerDir, readerRows = g.dir, byUnit[unit]
		}
		out := runBlobKindRead(t, g, byUnit[unit], inputs)
		// THE RETAINING SAVE of every read row re-emits what the load kept;
		// the writer unit declares every field the wire carries, so the
		// engine must accept each with nothing unknown
		resaved := 0
		for line := range strings.SplitSeq(out, "\n") {
			rest, isResave := strings.CutPrefix(line, "RESAVE ")
			name, wire, ok := strings.Cut(rest, " ")
			if !isResave || !ok {
				continue
			}
			resaved++
			data, err := hex.DecodeString(wire)
			if err != nil {
				t.Fatalf("%s: resave hex: %v", name, err)
			}
			row := byUnit[unit][rowIndex(byUnit[unit], name)]
			if read, unknown, reason := engineVerdict(t, writerModel, row.root, data); !read || unknown != 0 {
				t.Errorf("%s: the retaining save of the read wire is not accepted clean by the writer unit: read=%v unknown=%d %s", name, read, unknown, reason)
			}
		}
		if want := countRead(byUnit[unit]); resaved != want {
			t.Errorf("%s: %d retaining resaves, want %d", unit, resaved, want)
		}
	}

	t.Run("feature witnesses: records outside the root closure refuse", func(t *testing.T) {
		wdir := filepath.Join(root, "testdata", "conformance", "feature-witnesses", "pointer-targets")
		m := blobKindModel(t, wdir)
		witnessRows, witnessInputs := blobKindWitnessRows(t, m, wdir)
		g := blobKindUnit(t, wdir)
		runBlobKindRead(t, g, witnessRows, witnessInputs)
	})

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
		if err := os.WriteFile(filepath.Join(control, "main.lobster"), []byte(blobKindReadProgram(readerMods(t, readerDir), readerRows)), 0o644); err != nil {
			t.Fatal(err)
		}
		out := string(lobsterrun.Run(t, control, "main.lobster", inputs))
		if !strings.Contains(out, "FAIL text-names-bytes") {
			t.Fatalf("the planted root-closure rule did not go red on text-names-bytes:\n%s", out)
		}
	})

	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		g := blobKindUnit(t, filepath.Join(root, "test/pointer-target-type/blobkind/writer"))
		if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(blobKindSaveProgram(g)), 0o644); err != nil {
			t.Fatal(err)
		}
		saved := lobsterrun.HexLines(t, lobsterrun.Run(t, g.dir, "main.lobster", nil))
		states := []string{"set", "empty", "null"}
		cases := []struct{ root, field string }{{"Text", "body"}, {"Bin", "data"}}
		if want := len(cases) * len(states) * 2; len(saved) != want {
			t.Fatalf("the writer printed %d saved wires, want %d", len(saved), want)
		}
		i := 0
		for _, c := range cases {
			byState := map[string][]byte{}
			for _, state := range states {
				plain, retained := saved[i], saved[i+1]
				i += 2
				if !bytes.Equal(plain, retained) {
					t.Errorf("%s.%s %s: the retaining save wrote %x, the plain save %x", c.root, c.field, state, retained, plain)
				}
				byState[state] = plain
				for _, path := range []struct {
					name string
					wire []byte
				}{{"save", plain}, {"save_retain", retained}} {
					inst := writerModel.New(writerModel.Lookup(c.root))
					var report tabletext.Report
					ok, err := tablewire.Decode(writerModel, inst, path.wire, &report)
					if !ok || err != nil || report.Refused || report.Unknown != 0 {
						t.Errorf("%s.%s %s (%s): the saved wire is not accepted clean: %v %+v", c.root, c.field, state, path.name, err, report)
						continue
					}
					fv, ok := inst.FieldByKey(c.field)
					if !ok {
						t.Fatalf("%s has no field %s", c.root, c.field)
					}
					switch state {
					case "set":
						if fv.Cell.Blob == nil || string(fv.Cell.Blob.Data) != "hello" {
							t.Errorf("%s.%s set (%s): read back %+v, want the saved blob", c.root, c.field, path.name, fv.Cell.Blob)
						}
					case "empty":
						if fv.Cell.Blob == nil || len(fv.Cell.Blob.Data) != 0 {
							t.Errorf("%s.%s empty (%s): read back %+v, want an empty blob", c.root, c.field, path.name, fv.Cell.Blob)
						}
					default:
						if fv.Cell.Blob != nil || fv.Cell.Node != nil {
							t.Errorf("%s.%s null (%s): read back a pointer, want null", c.root, c.field, path.name)
						}
					}
				}
			}
			// a probe whose saves are byte-identical wrote no blob at all
			if bytes.Equal(byState["set"], byState["null"]) || bytes.Equal(byState["empty"], byState["null"]) || bytes.Equal(byState["set"], byState["empty"]) {
				t.Errorf("%s.%s: the set, empty and null saves are not three different wires: %x %x %x", c.root, c.field, byState["set"], byState["empty"], byState["null"])
			}
		}
	})
}

// blobKindRow is one row: the root to read at, the wire file, the verdict and
// unknown count the engine answers, and the engine's reason, logged only.
type blobKindRow struct {
	name, unit, root, file string
	read                   bool
	unknown                int
	reason                 string
}

func blobKindRows(t *testing.T, manifest string) []blobKindRow {
	t.Helper()
	var rows []blobKindRow
	i := 0
	for line := range strings.SplitSeq(manifest, "\n") {
		i++
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i, len(f))
		}
		row := blobKindRow{name: f[0], unit: f[1], root: f[2], file: f[3], unknown: -1}
		switch f[4] {
		case "read":
			row.read = true
		case "refused":
		default:
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i, f[4])
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil || n < 0 {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q, want a count or -", i, f[5])
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	return rows
}

// blobKindWitnesses are the cases.json rows the ruling flipped to refusals:
// a table, a *bytes and a *string record outside the reader root's closure,
// each named from a slot the reader declares.
var blobKindWitnesses = []string{"table outside root closure", "bytes outside root closure", "string outside root closure"}

// blobKindWitnessRows writes each witness row's input at its writer root with
// the engine, reads it back at the reader root with the engine, and returns
// the rows with the engine's answer, which must be the refusal cases.json pins.
func blobKindWitnessRows(t *testing.T, m *tabletext.Model, dir string) ([]blobKindRow, map[string][]byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Writer, Reader string
		Input                json.RawMessage
		KindMismatch         int `json:"kind_mismatch"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var rows []blobKindRow
	inputs := map[string][]byte{}
	for _, name := range blobKindWitnesses {
		found := false
		for _, c := range cases {
			if c.Name != name {
				continue
			}
			found = true
			if c.KindMismatch == 0 {
				t.Fatalf("%s: cases.json no longer pins a refusal", name)
			}
			v := m.New(m.Lookup(c.Writer))
			var r tabletext.Report
			if !m.Read(v, c.Input, &r) || !r.Silent() {
				t.Fatalf("%s: the writer's input does not read: %+v", name, r)
			}
			wire, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatalf("%s: Encode: %v", name, err)
			}
			read, unknown, reason := engineVerdict(t, m, c.Reader, wire)
			if read {
				t.Fatalf("%s: the engine reads the row (unknown %d), cases.json pins a refusal", name, unknown)
			}
			file := strings.ReplaceAll(name, " ", "-")
			inputs[file] = wire
			rows = append(rows, blobKindRow{name: file, root: c.Reader, unknown: -1, reason: reason})
		}
		if !found {
			t.Fatalf("cases.json has no row %q", name)
		}
	}
	return rows, inputs
}

// engineVerdict reads one wire at one root with the Go engine.
func engineVerdict(t *testing.T, m *tabletext.Model, rootName string, wire []byte) (read bool, unknown int, reason string) {
	t.Helper()
	def := m.Lookup(rootName)
	if def == nil {
		t.Fatalf("no table %q", rootName)
	}
	var report tabletext.Report
	ok, err := tablewire.Decode(m, m.New(def), wire, &report)
	if ok && err == nil && !report.Refused {
		return true, report.Unknown, ""
	}
	reason = tablewire.RefusalReason(err)
	if reason == "" {
		t.Fatalf("%s: the engine failed without a read refusal: %v %+v", rootName, err, report)
	}
	return false, 0, reason
}

func rowIndex(rows []blobKindRow, name string) int {
	for i, r := range rows {
		if r.name == name {
			return i
		}
	}
	panic("no row " + name)
}

func countRead(rows []blobKindRow) int {
	n := 0
	for _, r := range rows {
		if r.read {
			n++
		}
	}
	return n
}

// blobKindUnit generates one schema directory's Lobster surfaces into a fresh
// directory.
func blobKindUnit(t *testing.T, dir string) *loadUnit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("%s: the surface refuses the unit: %v", dir, err)
	}
	return g
}

// runBlobKindRead runs the read program over rows in a generated unit and
// fails the test unless every row answers as the engine does; it returns the
// program's output.
func runBlobKindRead(t *testing.T, g *loadUnit, rows []blobKindRow, inputs map[string][]byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(blobKindReadProgram(g.mods, rows)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)))
	if !strings.HasSuffix(out, "blob kinds passed") || strings.Contains(out, "FAIL") {
		t.Fatalf("%s", out)
	}
	// why a read refused is per-language detail (docs/rulings.json
	// refusal-verdict-only): logged beside the engine's, never compared
	for _, row := range rows {
		if !row.read {
			t.Logf("%s: the engine refuses %s", row.name, row.reason)
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		if reason, ok := strings.CutPrefix(line, "REASON "); ok {
			t.Log(reason)
		}
	}
	return out
}

// readerMods lists the table modules a generated directory holds.
func readerMods(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*Table.lobster"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no table module in %s: %v", dir, err)
	}
	var mods []string
	for _, m := range matches {
		mods = append(mods, strings.TrimSuffix(filepath.Base(m), ".lobster"))
	}
	return mods
}

// blobKindReadProgram checks each row through every read entry: the verdict
// (printing a refusal's reason), the unknown count where the row pins one, and, for a
// read row that counts unknown records, that every pointer slot the reader
// declares stays null. Each read row is also loaded retaining and saved
// retaining, and the resaved wire printed for the engine.
func blobKindReadProgram(mods []string, rows []blobKindRow) string {
	var b strings.Builder
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(blobKindProgram)
	for _, row := range rows {
		fmt.Fprintf(&b, "check(%q, %q, %v, %d)\n", row.name, row.root, row.read, row.unknown)
	}
	b.WriteString("print(if failures == 0: \"blob kinds passed\" else: \"blob kinds failed\")\n")
	return b.String()
}

const blobKindProgram = `import .strict_and_refusals
import .retain_unknown
import .region_measure
import .schema_save

var failures = 0

def fail(name:string, entry:string, what:string):
    print("FAIL " + name + " " + entry + ": " + what)
    failures++

def hexs(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)

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
        else:
            print("REASON " + name + " " + entry + " " + schema_rt.strict_reason(rep))
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
    let strict, why = schema_rt.load_strict(root, data, sr)
    held(name, "strict", strict, sr, read, unknown)
    if read != (why == ""):
        fail(name, "strict", "reason " + why)
    schema_rt.release(strict)
    let rr = schema_rt.LoadReport {}
    let retained = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
    let rrec = schema_rt.load_retain(root, data, retained, rr)
    held(name, "retain", rrec, rr, read, unknown)
    if rrec:
        let sv = schema_rt.LoadReport {}
        let out = schema_rt.save_retain(rrec, retained, sv)
        if out == "" or sv.retain_lost != 0:
            fail(name, "save_retain", "the retaining save lost what the load kept")
        print("RESAVE " + name + " " + hexs(out))
    schema_rt.release(rrec)
    let n, whole, bad, mwhy = schema_rt.load_measure(root, data)
    if read and (n < 0 or bad or mwhy != ""):
        fail(name, "measure", "a read row refused: " + mwhy)
    if not read and (n >= 0 or not bad):
        fail(name, "measure", "a refused row measured " + string(n))

`

// blobKindSaveProgram saves a Text and a Bin value with the blob set, empty
// and null, each through the plain save and the retaining save, printing one
// hex line per save. The writer's Text declares a field the reader does not
// (`extra *bytes`); it stays null, so a conforming save elides it and the wire
// holds no record no pointer reaches.
func blobKindSaveProgram(g *loadUnit) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString(blobKindSave)
	return b.String()
}

const blobKindSave = `import .schema_save
import .retain_unknown

def hexs(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)

def save_one(root:string, field:string, state:string):
    let rec = schema_rt.new_record(schema_rt.table_of(root))
    var placed = false
    for(rec.fields) sl:
        if sl.fd.name == field:
            placed = true
            if state == "set":
                sl.cell.blob = "hello"
            elif state == "empty":
                sl.cell.blob = ""
    assert placed
    print(hexs(schema_rt.save(rec)))
    let retained = schema_rt.Retain { capacity: 4096, id_capacity: 256, used: 0, ids: [], known: [] }
    let rep = schema_rt.LoadReport {}
    print(hexs(schema_rt.save_retain(rec, retained, rep)))

for(["Text", "Bin"]) root:
    let field = if root == "Text": "body" else: "data"
    for(["set", "empty", "null"]) state:
        save_one(root, field, state)
`

// blobKindModel compiles one unit into the engine's value model, so a wire
// can be read and written by internal/tablewire.
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
