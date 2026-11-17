package mootable_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestMooPointerTargetBlobKindRefused holds every row of
// testdata/wire/pointer-target-type/blobkind/MANIFEST.tsv to the moo leg's
// reads (docs/SPEC-TABLES.md §3.1, §6.5; docs/rulings.json
// blob-target-outside-closure-refused): the two reserved blob ids are types
// every reader of the variable form can name, whatever its root reaches, so a
// blob record named from a pointer slot the reader DECLARES of the other kind
// refuses the whole read, while a newer writer's undeclared field naming a blob
// record reads and counts the field and its record unknown. The leg's own save
// is held in the same test to never emitting a blob record no pointer reaches.
func TestMooPointerTargetBlobKindRefused(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers and writers)")
	t.Parallel()
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type", "blobkind")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []blobKindRow
	units := map[string]bool{}
	i := -1
	for line := range strings.SplitSeq(string(manifest), "\n") {
		i++
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want case, unit, root, file, verdict, unknown", i+1, len(f))
		}
		if f[4] != "read" && f[4] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[4])
		}
		r := blobKindRow{caseName: f[0], unit: f[1], root: f[2], file: f[3], read: f[4] == "read", unknown: -1}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			r.unknown = n
		}
		rows = append(rows, r)
		units[r.unit] = true
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	inputs := map[string][]byte{}
	for _, r := range rows {
		wire, err := os.ReadFile(filepath.Join(dir, r.file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[r.caseName] = wire
	}

	generated := map[string]*generatedUnit{}
	for unit := range units {
		generated[unit] = generateSchemaDir(t, filepath.Join(root, unit))
	}

	answers := func(g *generatedUnit, unit string) map[string]blobKindAnswer {
		var picked []blobKindRow
		for _, r := range rows {
			if r.unit == unit {
				picked = append(picked, r)
			}
		}
		return runBlobKindReads(t, g, picked, inputs)
	}

	got := map[string]map[string]blobKindAnswer{}
	for unit, g := range generated {
		got[unit] = answers(g, unit)
	}
	for _, p := range blobKindProblems(rows, got) {
		t.Error(p)
	}

	t.Run("negative control: the root-closure rule reads text-names-bytes", func(t *testing.T) {
		const anchor = "if n.tid ~= want and (declared[n.tid] or n.tid == StringBlobId or n.tid == BytesBlobId) then"
		const planted = "if n.tid ~= want and (declared[n.tid] or place_types(_rootDesc)[n.tid] ~= nil) then"
		for unit := range units {
			g := generateSchemaDir(t, filepath.Join(root, unit))
			path := filepath.Join(g.dir, "table_load.moo")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(src), anchor) != 1 {
				t.Fatalf("the blob-kind resolution anchor is not in the emitted table_load.moo exactly once")
			}
			if err := os.WriteFile(path, []byte(strings.Replace(string(src), anchor, planted, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			var picked []blobKindRow
			for _, r := range rows {
				if r.unit == unit && r.caseName == "text-names-bytes" {
					picked = append(picked, r)
				}
			}
			if len(picked) == 0 {
				continue
			}
			problems := blobKindProblems(picked, map[string]map[string]blobKindAnswer{unit: answers(g, unit)})
			if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "text-names-bytes") {
				t.Fatalf("the planted root-closure rule still refuses text-names-bytes: %v", problems)
			}
		}
	})

	// THE FEATURE WITNESSES (testdata/conformance/feature-witnesses/
	// pointer-targets/cases.json): the engine saves each row's input at its
	// writer root and is the oracle for the read at its reader root; the leg's
	// three read paths must answer the same verdict and unknown count. Rows 8,
	// 9 and 10 (a declared table, then each blob kind, outside the reader
	// root's closure) are the refusals this ruling flipped.
	t.Run("the pointer-target feature witnesses answer the engine's verdict", func(t *testing.T) {
		const witnessDir = "testdata/conformance/feature-witnesses/pointer-targets"
		dir := filepath.Join(root, witnessDir)
		m := engineModel(t, dir)
		data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cases []pointerWitnessCase
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatal(err)
		}
		flipped := map[int]string{8: "table outside root closure", 9: "bytes outside root closure", 10: "string outside root closure"}
		for n, name := range flipped {
			if len(cases) < n || cases[n-1].Name != name || cases[n-1].KindMismatch == 0 {
				t.Fatalf("cases.json row %d is not the refused %q", n, name)
			}
		}
		var witnessRows []blobKindRow
		witnessInputs := map[string][]byte{}
		for _, c := range cases {
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
			var report tabletext.Report
			ok, err := tablewire.Decode(m, m.New(reader), wire, &report)
			engineRead := ok && err == nil && !report.Refused
			if engineRead != (c.KindMismatch == 0) || (engineRead && report.Unknown != c.Unknown) {
				t.Fatalf("%s: the engine answers read=%v unknown %d against cases.json kind_mismatch %d unknown %d", c.Name, engineRead, report.Unknown, c.KindMismatch, c.Unknown)
			}
			witnessRows = append(witnessRows, blobKindRow{caseName: c.Name, unit: witnessDir, root: c.Reader, read: engineRead, unknown: report.Unknown})
			witnessInputs[c.Name] = wire
		}
		g := generateSchemaDir(t, dir)
		got := map[string]map[string]blobKindAnswer{witnessDir: runBlobKindReads(t, g, witnessRows, witnessInputs)}
		for _, p := range blobKindProblems(witnessRows, got) {
			t.Error(p)
		}
	})

	// A CONFORMING WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: the
	// leg's save (plain and retaining) of each writer root, each blob field
	// set, empty or null, reads back under the engine accepted with nothing
	// unknown and the field's value intact, so a save that dropped the blob
	// (or never set it) cannot pass as the null save.
	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		const writerUnit = "test/pointer-target-type/blobkind/writer"
		g := generateSchemaDir(t, filepath.Join(root, writerUnit))
		m := engineModel(t, filepath.Join(root, writerUnit))
		out := g.run(t, blobKindWriterProbe(g), nil)
		seen := map[string]bool{}
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 5 {
				t.Fatalf("the writer probe printed %q", line)
			}
			rootName, field, state, path, hexWire := f[0], f[1], f[2], f[3], f[4]
			label := rootName + "." + field + " " + state + " (" + path + ")"
			if !strings.HasPrefix(hexWire, "x") {
				t.Fatalf("%s: the save refused: %s", label, hexWire)
			}
			wire, err := hex.DecodeString(strings.TrimPrefix(hexWire, "x"))
			if err != nil {
				t.Fatalf("%s: bad hex %q: %v", label, hexWire, err)
			}
			def := m.Lookup(rootName)
			if def == nil {
				t.Fatalf("%s: no engine table %q", label, rootName)
			}
			dest := m.New(def)
			var report tabletext.Report
			ok, err := tablewire.Decode(m, dest, wire, &report)
			if !ok || err != nil || report.Refused || report.Unknown != 0 {
				t.Errorf("%s: the writer emitted a wire the engine does not read clean: %v %+v", label, err, report)
				continue
			}
			text, err := m.Write(dest)
			if err != nil {
				t.Fatalf("%s: Write: %v", label, err)
			}
			var value map[string]any
			if err := json.Unmarshal(text, &value); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"null": nil, "empty": ""}[state]
			if state == "set" {
				want = "hello"
				if field != "body" {
					want = base64.StdEncoding.EncodeToString([]byte("hello"))
				}
			}
			if value[field] != want {
				t.Errorf("%s: the engine reads %s = %v from %s, want %v", label, field, value[field], hexWire, want)
			}
			seen[rootName+"."+field+" "+state+" "+path] = true
		}
		for _, rf := range []string{"Text.body", "Text.extra", "Bin.data"} {
			for _, state := range []string{"set", "empty", "null"} {
				for _, path := range []string{"save", "retain"} {
					if !seen[rf+" "+state+" "+path] {
						t.Errorf("the writer probe answered no %s %s save (%s)", rf, state, path)
					}
				}
			}
		}
	})
}

// pointerWitnessCase is one row of the pointer-target feature witnesses.
type pointerWitnessCase struct {
	Name, Writer, Reader string
	Input                json.RawMessage
	KindMismatch         int `json:"kind_mismatch"`
	Unknown              int
}

// engineModel compiles one schema directory into the engine's model.
func engineModel(t *testing.T, dir string) *tabletext.Model {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return tabletext.NewModel(u)
}

// blobKindRow is one row of the blob-kind manifest.
type blobKindRow struct {
	caseName, unit, root, file string
	read                       bool
	unknown                    int
}

// blobKindAnswer is one read path's answer for one vector: its verdict and the
// report's unknown count.
type blobKindAnswer struct {
	verdict string
	unknown int
}

// blobKindProblems names each read whose answer is not the row's verdict or
// pinned unknown count.
func blobKindProblems(rows []blobKindRow, got map[string]map[string]blobKindAnswer) []string {
	var problems []string
	for _, r := range rows {
		byPath := got[r.unit]
		for _, path := range []string{"load", "strict", "retain"} {
			a, ok := byPath[r.caseName+"\t"+path]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s (%s): the probe answered nothing", r.caseName, path))
				continue
			}
			want := "refused"
			if r.read {
				want = "read"
			}
			if a.verdict != want {
				problems = append(problems, fmt.Sprintf("%s (%s): answers %s, want %s", r.caseName, path, a.verdict, want))
				continue
			}
			if r.read && r.unknown >= 0 && a.unknown != r.unknown {
				problems = append(problems, fmt.Sprintf("%s (%s): unknown %d, want %d", r.caseName, path, a.unknown, r.unknown))
			}
		}
	}
	return problems
}

// runBlobKindReads runs every picked row through the plain load, the strict
// read's form dispatch and the retaining load, keyed case + path.
func runBlobKindReads(t *testing.T, g *generatedUnit, rows []blobKindRow, inputs map[string][]byte) map[string]blobKindAnswer {
	t.Helper()
	var main strings.Builder
	main.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal Rf = require(\"./table_refusal\")\nlocal R = require(\"./table_retain\")\n")
	main.WriteString(g.requireAll())
	main.WriteString("local rows: { any } = {\n")
	for _, r := range rows {
		fmt.Fprintf(&main, "\t{ case = %q, root = %q, module = %q },\n", r.caseName, r.root, tableModuleOf(g, r.root))
	}
	main.WriteString(`}
local function line(row: any, path: string, v: any, rep: any)
	local verdict = if v ~= nil and rep.reason == nil then "read" else "refused"
	print(string.format("%s\t%s\t%s\t%d", row.case, path, verdict, rep.unknown or 0))
end
for _, row in rows do
	local wire = inputs[row.case]
	local rep = L.newReport()
	line(row, "load", L.load(row.root, wire, rep), rep)
	rep = L.newReport()
	local T = require("./" .. row.module)
	line(row, "strict", Rf.ReadStrict(T, row.root, wire, rep), rep)
	rep = L.newReport()
	line(row, "retain", R.LoadRetain(row.root, wire, rep, R.newStore(1048576, nil)), rep)
end
`)
	out := g.run(t, main.String(), inputs)
	answers := map[string]blobKindAnswer{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			t.Fatalf("the probe printed %q", line)
		}
		n, err := strconv.Atoi(f[3])
		if err != nil {
			t.Fatalf("%s: unknown %q: %v", line, f[3], err)
		}
		answers[f[0]+"\t"+f[1]] = blobKindAnswer{verdict: f[2], unknown: n}
	}
	return answers
}

// blobKindWriterProbe constructs each writer root with one blob field set,
// empty or null, saves it plainly and retaining, and prints root, field,
// state, path and the wire as hex. A refusal prints its name where the hex
// would be. A record's slot is the field's exported name (desc.go), not the
// schema name.
func blobKindWriterProbe(g *generatedUnit) string {
	return "--!strict\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\nlocal W = require(\"./table_save\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return "x" .. table.concat(parts)
end
local function emit(root: string, field: string, slot: string, state: string, data: string?)
	local d = St.lookup(root)
	local v = d.new()
	if data ~= nil then
		v[slot] = buffer.fromstring(data)
	end
	local out, why = W.save(d, v)
	print(string.format("%s\t%s\t%s\tsave\t%s", root, field, state, if out ~= nil then hex(out) else "SAVENIL " .. tostring(why.name)))
	local report = L.newReport()
	local store = R.newStore(16777216, nil)
	local kept, whyKept = R.SaveRetain(root, v, store, report)
	print(string.format("%s\t%s\t%s\tretain\t%s", root, field, state, if kept ~= nil then hex(kept) else "SAVENIL " .. tostring(whyKept.name)))
end
for _, f in { { "Text", "body", "Body" }, { "Text", "extra", "Extra" }, { "Bin", "data", "Data" } } do
	emit(f[1], f[2], f[3], "set", "hello")
	emit(f[1], f[2], f[3], "empty", "")
	emit(f[1], f[2], f[3], "null", nil)
end
`
}

// generateSchemaDir compiles one schema directory and emits its moo packet and
// table surfaces into a fresh TempDir.
func generateSchemaDir(t *testing.T, dir string) *generatedUnit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("%s does not load: %v", dir, err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return writeUnit(t, u, files)
}
