package mootable_test

import (
	"encoding/hex"
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
	for i, line := range strings.Split(string(manifest), "\n") {
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

	// A CONFORMING WRITER NEVER EMITS A BLOB RECORD NO POINTER REACHES: the
	// leg's save (plain and retaining) of each writer root, the blob set or
	// null, reads back under the engine accepted with nothing unknown.
	t.Run("the writer emits no unreachable blob record", func(t *testing.T) {
		const writerUnit = "test/pointer-target-type/blobkind/writer"
		g := generateSchemaDir(t, filepath.Join(root, writerUnit))
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, writerUnit)})
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(u)
		out := g.run(t, blobKindWriterProbe(g), nil)
		seen := 0
		for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 3 {
				t.Fatalf("the writer probe printed %q", line)
			}
			label, path, hexWire := f[0], f[1], f[2]
			if !strings.HasPrefix(hexWire, "x") {
				t.Fatalf("%s (%s): the save refused: %s", label, path, hexWire)
			}
			wire, err := hex.DecodeString(strings.TrimPrefix(hexWire, "x"))
			if err != nil {
				t.Fatalf("%s (%s): bad hex %q: %v", label, path, hexWire, err)
			}
			root, _, _ := strings.Cut(label, ".")
			def := m.Lookup(root)
			if def == nil {
				t.Fatalf("%s: no engine table %q", label, root)
			}
			var report tabletext.Report
			ok, err := tablewire.Decode(m, m.New(def), wire, &report)
			if !ok || err != nil || report.Refused || report.Unknown != 0 {
				t.Errorf("%s (%s): the writer emitted a wire the engine does not read clean: %v %+v", label, path, err, report)
			}
			seen++
		}
		if seen == 0 {
			t.Fatal("the writer probe answered no save")
		}
	})
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

// blobKindWriterProbe constructs each writer root with its blob field set and
// null, saves it plainly and retaining, and prints label, path and the wire as
// hex. A refusal prints its name where the hex would be.
func blobKindWriterProbe(g *generatedUnit) string {
	return "--!strict\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\nlocal W = require(\"./table_save\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return "x" .. table.concat(parts)
end
local function emit(root: string, field: string, data: string?)
	local d = St.lookup(root)
	local v = d.new()
	if data ~= nil then
		v[field] = buffer.fromstring(data)
	end
	local out, why = W.save(d, v)
	print(string.format("%s.%s\tset\t%s", root, field, if out ~= nil then hex(out) else "SAVENIL " .. tostring(why.name)))
	local report = L.newReport()
	local store = R.newStore(16777216, nil)
	local kept, whyKept = R.SaveRetain(root, v, store, report)
	print(string.format("%s.%s\tretain\t%s", root, field, if kept ~= nil then hex(kept) else "SAVENIL " .. tostring(whyKept.name)))
end
emit("Text", "body", "hello")
emit("Text", "body", nil)
emit("Bin", "data", "hello")
emit("Bin", "data", nil)
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
