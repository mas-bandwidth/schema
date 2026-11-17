package squirreltable_test

// TestSquirrelPointerTargetBlobKindRefused holds the blob-kind rows of
// testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv through the Squirrel
// leg's strict and retaining reads (docs/SPEC-TABLES.md §3.1, §6.5;
// docs/rulings.json blob-target-outside-closure-refused): the two reserved blob
// ids are types every reader of the variable form can name, whatever its root
// reaches, so a declared *string slot naming a *bytes record (or the reverse,
// or a *T slot naming either) refuses the whole read. A blob record of a kind
// the root's closure lacks, named only from a field the reader does not
// declare, reads and counts the field and the record unknown. A record no
// pointer reaches is damage. Rows 8, 9 and 10 of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json (a table,
// a *bytes and a *string record outside the reader root's closure, named from
// a declared slot) refuse too, with the engine's reads as the oracle. Only
// the verdict and the pinned counters are compared; each read's reason is
// logged (docs/rulings.json refusal-verdict-only).
//
// The negative control plants the old rule — a blob is nameable only when the
// root's closure reaches its kind — in a copy of the generated reader, and the
// manifest must go red on text-names-bytes. The writer half holds the leg's
// own save and retaining save of a Text and a Bin value, blob null, empty and
// set, to a wire the engine reads with nothing unknown (a conforming writer
// never emits a blob record no pointer reaches), and requires the empty and
// set saves to differ from the null save, so the probe really wrote the blob.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type squirrelBlobKindRow struct {
	name, unit, root, file, verdict string
	unknown                         int
}

func readSquirrelBlobKindRows(t *testing.T, manifest string) []squirrelBlobKindRow {
	t.Helper()
	var rows []squirrelBlobKindRow
	for i, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		row := squirrelBlobKindRow{name: f[0], unit: f[1], root: f[2], file: f[3], verdict: f[4], unknown: -1}
		if row.verdict != "read" && row.verdict != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, row.verdict)
		}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			row.unknown = n
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	return rows
}

// squirrelBlobKindAnswer is one read's answer. The reason is logged, never
// compared: only a refusal's verdict is held across legs (docs/rulings.json
// refusal-verdict-only).
type squirrelBlobKindAnswer struct {
	read        bool
	unknown     int
	ptr, reason string
}

const squirrelBlobKindProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("__DIR__/schema_module.nut", true);
local T = schema.require("__MOD__");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local R = schema.require("schema_table_refusal");
local RT = schema.require("schema_table_retain");
local HEXB = array(256);
for (local i = 0; i < 256; i++) HEXB[i] = format("%02x", i);
local hexOf = function(b) {
  if (b == null) return "null";
  local s = "";
  for (local i = 0; i < b.len(); i++) s += HEXB[b[i] & 0xff];
  return s;
};
local memberOf = function(root) {
  foreach (f in T.Descriptors[root].fields) {
    if (f.shape == "ptr" || f.shape == "sblob" || f.shape == "bblob") return f.member;
  }
  return null;
};
local memberOfName = function(root, name) {
  foreach (f in T.Descriptors[root].fields) if (f.name == name) return f.member;
  return null;
};
local ptr = function(v, member) {
  if (v == null || member == null) return "nil";
  return v[member] == null ? "null" : "set";
};
local fresh = function() {
  return { unknown = 0, mismatch = 0, widened = 0, oor = 0, repeated = 0,
    bodyDamaged = false, refused = false };
};
local reasonOf = function(r) {
  return ("reason" in r && r.reason != null) ? r.reason : "-";
};
local READERS = __READERS__;
foreach (row in READERS) {
  local member = memberOf(row.root);
  local wire = inputs[row.key];
  local r1 = fresh();
  local v1 = R.ReadStrict(T, row.root, wire, r1);
  print("R|" + row.key + "|strict|" + (v1 != null ? "read" : "refused") + "|" +
    r1.unknown + "|" + ptr(v1, member) + "|" + reasonOf(r1) + "\n");
  local r2 = fresh();
  local store = { capacity = 1048576, idCapacity = 1024 };
  local v2 = RT.LoadRetain(T, row.root, wire, store, r2);
  print("R|" + row.key + "|retain|" + (v2 != null ? "read" : "refused") + "|" +
    r2.unknown + "|" + ptr(v2, member) + "|" + reasonOf(r2) + "\n");
}
local WRITERS = __WRITERS__;
foreach (row in WRITERS) {
  local member = memberOfName(row.root, row.member);
  local v = T.Descriptors[row.root].make();
  if (row.text != null) v[member] = row.text;
  else if (row.input != null) v[member] = inputs[row.input];
  local b = S.save(T, row.root, v);
  print("W|" + row.key + "|save|" + hexOf(b) + "\n");
  // SaveRetain writes through the store's emit, which a read of this build's
  // own wire seeds; a constructed value carries no retained record, so only
  // the save path itself is under test here.
  local store = { capacity = 1048576, idCapacity = 1024 };
  if (b == null || RT.LoadRetain(T, row.root, b, store, fresh()) == null) {
    print("W|" + row.key + "|retain|null\n");
    continue;
  }
  local m = RT.MeasureRetain(T, row.root, v, store);
  if (m < 0) { print("W|" + row.key + "|retain|null\n"); continue; }
  local out = blob(m);
  local sr = fresh();
  local n = RT.SaveRetain(T, row.root, v, store, out, sr);
  print("W|" + row.key + "|retain|" + (n < 0 ? "null" : hexOf(out)) + "\n");
}
`

func squirrelBlobKindReaderLiteral(rows []squirrelBlobKindRow) string {
	var b strings.Builder
	b.WriteString("[")
	for i, row := range rows {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "{ key = %q, root = %q }", row.name, row.root)
	}
	b.WriteString("]")
	return b.String()
}

func squirrelBlobKindWriterLiteral() string {
	var b strings.Builder
	b.WriteString("[")
	first := true
	add := func(key, root, member, text, input string) {
		if !first {
			b.WriteString(", ")
		}
		first = false
		t, i := "null", "null"
		if text != "<null>" {
			t = fmt.Sprintf("%q", text)
		}
		if input != "<null>" {
			i = fmt.Sprintf("%q", input)
		}
		fmt.Fprintf(&b, "{ key = %q, root = %q, member = %q, text = %s, input = %s }", key, root, member, t, i)
	}
	add("Text-null", "Text", "body", "<null>", "<null>")
	add("Text-empty", "Text", "body", "", "<null>")
	add("Text-value", "Text", "body", "hello", "<null>")
	add("Bin-null", "Bin", "data", "<null>", "<null>")
	add("Bin-empty", "Bin", "data", "<null>", "writer_empty")
	add("Bin-value", "Bin", "data", "<null>", "writer_hello")
	b.WriteString("]")
	return b.String()
}

func emitSquirrelBlobKindUnit(t *testing.T, dir, key string, u *ir.Unit, patch func(map[string][]byte)) string {
	t.Helper()
	files, err := squirrel.Generate(u)
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(u)
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	if patch != nil {
		patch(tables)
	}
	maps.Copy(files, tables)
	out := filepath.Join(dir, key)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	return squirreltable.ModuleName(u)
}

// plantRootClosureBlobRule restores the old rule in a copy of the generated
// reader: a blob record whose kind the root's closure lacks is a record this
// reader cannot name, so it counts unknown and every declared slot naming it
// reads null rather than the wrong-kind damage.
func plantRootClosureBlobRule(t *testing.T, tables map[string][]byte) {
	t.Helper()
	src := string(tables["schema_table_load.nut"])
	for _, plant := range []struct{ anchor, old string }{
		{
			`if (!c.blobString) { report.unknown = report.unknown + 1; if (retain != null) report.retainLost = report.retainLost + 1; }`,
			`if (!c.blobString) { report.unknown = report.unknown + 1; if (retain != null) report.retainLost = report.retainLost + 1; st.nodes.append(null); continue; }`,
		},
		{
			`if (!c.blobBytes) { report.unknown = report.unknown + 1; if (retain != null) report.retainLost = report.retainLost + 1; }`,
			`if (!c.blobBytes) { report.unknown = report.unknown + 1; if (retain != null) report.retainLost = report.retainLost + 1; st.nodes.append(null); continue; }`,
		},
	} {
		if n := strings.Count(src, plant.anchor); n != 1 {
			t.Fatalf("NEGATIVE CONTROL FAILED: the root-closure plant anchor appears %d times in schema_table_load.nut, want once", n)
		}
		src = strings.Replace(src, plant.anchor, plant.old, 1)
	}
	tables["schema_table_load.nut"] = []byte(src)
}

// runSquirrelBlobKindProbe reads the blobkind vectors with the leg's reader
// and saves the writer cases with the leg's writer.
func runSquirrelBlobKindProbe(t *testing.T, dir string, rows []squirrelBlobKindRow, patch func(map[string][]byte)) (map[string]squirrelBlobKindAnswer, map[string][]byte) {
	t.Helper()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind")
	u := loadUnit(t, repo, []string{rows[0].unit})
	input := map[string][]byte{
		"writer_hello": []byte("hello"),
		"writer_empty": {},
	}
	for _, row := range rows {
		wire, err := os.ReadFile(filepath.Join(vectors, row.file))
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		input[row.name] = wire
	}
	return runSquirrelProbe(t, dir, u, rows, input, squirrelBlobKindWriterLiteral(), patch)
}

// runSquirrelProbe emits u's Squirrel surfaces into dir, reads input[row.name]
// at row.root for every row through the strict and retaining loads, runs the
// writer cases, and parses the answers.
func runSquirrelProbe(t *testing.T, dir string, u *ir.Unit, rows []squirrelBlobKindRow, input map[string][]byte, writers string, patch func(map[string][]byte)) (map[string]squirrelBlobKindAnswer, map[string][]byte) {
	t.Helper()
	mod := emitSquirrelBlobKindUnit(t, dir, "blobkind", u, patch)
	probe := strings.NewReplacer(
		"__DIR__", "blobkind",
		"__MOD__", mod,
		"__READERS__", squirrelBlobKindReaderLiteral(rows),
		"__WRITERS__", writers,
	).Replace(squirrelBlobKindProbe)
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(probe))
	out := string(sqrun.Run(t, dir, "main.nut", input))

	answers := map[string]squirrelBlobKindAnswer{}
	wires := map[string][]byte{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Split(line, "|")
		switch {
		case len(f) == 7 && f[0] == "R":
			unknown, err := strconv.Atoi(f[4])
			if err != nil {
				t.Fatalf("probe line %q: %v", line, err)
			}
			answers[f[1]+"|"+f[2]] = squirrelBlobKindAnswer{read: f[3] == "read", unknown: unknown, ptr: f[5], reason: f[6]}
		case len(f) == 4 && f[0] == "W":
			if f[3] == "null" {
				wires[f[1]+"|"+f[2]] = nil
				continue
			}
			wire, err := hex.DecodeString(f[3])
			if err != nil {
				t.Fatalf("probe line %q: %v", line, err)
			}
			wires[f[1]+"|"+f[2]] = wire
		default:
			t.Fatalf("probe line %q: not a reader or writer answer", line)
		}
	}
	return answers, wires
}

func squirrelBlobKindVerdict(read bool) string {
	if read {
		return "accepted it"
	}
	return "refused it"
}

func squirrelBlobKindReadProblems(rows []squirrelBlobKindRow, answers map[string]squirrelBlobKindAnswer) []string {
	var problems []string
	for _, row := range rows {
		wantRead := row.verdict == "read"
		for _, path := range []string{"strict", "retain"} {
			a, ok := answers[row.name+"|"+path]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s (%s): no answer", row.name, path))
				continue
			}
			if a.read != wantRead {
				problems = append(problems, fmt.Sprintf("%s (%s): the read %s, the manifest says %s", row.name, path, squirrelBlobKindVerdict(a.read), row.verdict))
				continue
			}
			if !wantRead {
				continue
			}
			if row.unknown >= 0 && a.unknown != row.unknown {
				problems = append(problems, fmt.Sprintf("%s (%s): unknown %d, want %d", row.name, path, a.unknown, row.unknown))
			}
			if row.unknown > 0 && a.ptr != "null" {
				problems = append(problems, fmt.Sprintf("%s (%s): the pointer reads %s, want null", row.name, path, a.ptr))
			}
		}
	}
	return problems
}

func squirrelBlobKindWriterProblems(t *testing.T, u *ir.Unit, wires map[string][]byte) []string {
	t.Helper()
	m := tabletext.NewModel(u)
	var problems []string
	// A probe whose saves with the blob set or empty equal its save with the
	// blob null never wrote the blob, and proves nothing about the writer.
	for _, table := range []string{"Text", "Bin"} {
		for _, path := range []string{"save", "retain"} {
			null := wires[table+"-null|"+path]
			for _, c := range []string{"empty", "value"} {
				if w := wires[table+"-"+c+"|"+path]; w != nil && null != nil && bytes.Equal(w, null) {
					problems = append(problems, fmt.Sprintf("%s-%s (%s): the saved wire equals the save with the blob null: the probe did not write the blob", table, c, path))
				}
			}
		}
	}
	for _, root := range []struct{ table, field string }{{"Text", "body"}, {"Bin", "data"}} {
		for _, c := range []string{"null", "empty", "value"} {
			key := root.table + "-" + c
			for _, path := range []string{"save", "retain"} {
				wire, ok := wires[key+"|"+path]
				if !ok {
					problems = append(problems, fmt.Sprintf("%s (%s): no saved wire", key, path))
					continue
				}
				if wire == nil {
					problems = append(problems, fmt.Sprintf("%s (%s): the save answered null", key, path))
					continue
				}
				var report tabletext.Report
				inst := m.New(m.Lookup(root.table))
				decoded, err := tablewire.Decode(m, inst, wire, &report)
				if !decoded || err != nil || report.Refused || report.Unknown != 0 {
					problems = append(problems, fmt.Sprintf("%s (%s): the engine does not read the saved wire cleanly: decoded=%t err=%v report=%+v", key, path, decoded, err, report))
				}
			}
		}
	}
	return problems
}

// squirrelPointerWitness is one row of
// testdata/conformance/feature-witnesses/pointer-targets/cases.json.
type squirrelPointerWitness struct {
	Name, Writer, Reader string
	Input                json.RawMessage
	KindMismatch         int `json:"kind_mismatch"`
	Unknown              int
}

// squirrelWitnessRows are the feature-witness rows a blob or table record
// outside the reader root's closure flipped to refusals (rows 8, 9 and 10).
var squirrelWitnessRows = []struct {
	index int
	name  string
}{
	{8, "table outside root closure"},
	{9, "bytes outside root closure"},
	{10, "string outside root closure"},
}

// squirrelWitnessVectors encodes witness rows 8, 9 and 10 at their writer
// root with the engine, holds the engine's own strict and retaining reads at
// the reader root to the row's verdict (the oracle), and returns the rows and
// wires for the leg's reader.
func squirrelWitnessVectors(t *testing.T, u *ir.Unit, cases []byte) ([]squirrelBlobKindRow, map[string][]byte) {
	t.Helper()
	var all []squirrelPointerWitness
	if err := json.Unmarshal(cases, &all); err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	var rows []squirrelBlobKindRow
	input := map[string][]byte{}
	for _, w := range squirrelWitnessRows {
		if w.index > len(all) || all[w.index-1].Name != w.name {
			t.Fatalf("cases.json row %d is not %q", w.index, w.name)
		}
		c := all[w.index-1]
		if c.KindMismatch == 0 {
			t.Fatalf("cases.json row %d (%s) reads; this test holds it refused", w.index, c.Name)
		}
		writer, reader := m.Lookup(c.Writer), m.Lookup(c.Reader)
		if writer == nil || reader == nil {
			t.Fatalf("%s: no table %q or %q", c.Name, c.Writer, c.Reader)
		}
		v := m.New(writer)
		var r tabletext.Report
		if !m.Read(v, c.Input, &r) || !r.Silent() {
			t.Fatalf("%s: the writer's input does not read: %+v", c.Name, r)
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatalf("%s: Encode: %v", c.Name, err)
		}
		var strict tabletext.Report
		if ok, _ := tablewire.Decode(m, m.New(reader), wire, &strict); ok || !strict.Refused {
			t.Fatalf("%s: the engine's strict read accepts the row: %+v", c.Name, strict)
		}
		var retained tabletext.Report
		retain := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
		if ok, _ := tablewire.DecodeRetain(m, m.New(reader), wire, &retain, &retained); ok || !retained.Refused {
			t.Fatalf("%s: the engine's retaining read accepts the row: %+v", c.Name, retained)
		}
		key := fmt.Sprintf("witness-%d", w.index)
		rows = append(rows, squirrelBlobKindRow{name: key, root: c.Reader, verdict: "refused", unknown: -1})
		input[key] = wire
	}
	return rows, input
}

func logSquirrelBlobKindAnswers(t *testing.T, answers map[string]squirrelBlobKindAnswer) {
	t.Helper()
	for _, k := range slices.Sorted(maps.Keys(answers)) {
		a := answers[k]
		t.Logf("%s: %s, unknown %d, pointer %s, reason %s", k, squirrelBlobKindVerdict(a.read), a.unknown, a.ptr, a.reason)
	}
}

func TestSquirrelPointerTargetBlobKindRefused(t *testing.T) {
	t.Parallel()
	repo, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "pointer-target-type", "blobkind", "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := readSquirrelBlobKindRows(t, string(manifest))
	for _, row := range rows {
		if row.unit != rows[0].unit {
			t.Fatalf("%s reads %s; this probe reads %s", row.name, row.unit, rows[0].unit)
		}
	}

	u := loadUnit(t, repo, []string{rows[0].unit})
	t.Run("every row answers its verdict", func(t *testing.T) {
		answers, _ := runSquirrelBlobKindProbe(t, t.TempDir(), rows, nil)
		logSquirrelBlobKindAnswers(t, answers)
		for _, p := range squirrelBlobKindReadProblems(rows, answers) {
			t.Error(p)
		}
	})
	t.Run("feature-witness rows 8, 9 and 10 refuse", func(t *testing.T) {
		const witnesses = "testdata/conformance/feature-witnesses/pointer-targets"
		cases, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(witnesses), "cases.json"))
		if err != nil {
			t.Fatal(err)
		}
		wu := loadUnit(t, repo, []string{witnesses})
		wrows, input := squirrelWitnessVectors(t, wu, cases)
		answers, _ := runSquirrelProbe(t, t.TempDir(), wu, wrows, input, "[]", nil)
		logSquirrelBlobKindAnswers(t, answers)
		for _, p := range squirrelBlobKindReadProblems(wrows, answers) {
			t.Error(p)
		}
	})
	t.Run("negative control: the root-closure can-name rule", func(t *testing.T) {
		answers, _ := runSquirrelBlobKindProbe(t, t.TempDir(), rows, func(tables map[string][]byte) {
			plantRootClosureBlobRule(t, tables)
		})
		problems := squirrelBlobKindReadProblems(rows, answers)
		if len(problems) == 0 {
			t.Fatal("NEGATIVE CONTROL FAILED: planting the root-closure can-name rule left every row green")
		}
		if !strings.Contains(strings.Join(problems, "\n"), "text-names-bytes") {
			t.Fatalf("NEGATIVE CONTROL FAILED: the planted rule did not go red on text-names-bytes:\n%s", strings.Join(problems, "\n"))
		}
	})
	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		_, wires := runSquirrelBlobKindProbe(t, t.TempDir(), rows, nil)
		for _, p := range squirrelBlobKindWriterProblems(t, u, wires) {
			t.Error(p)
		}
	})
}
