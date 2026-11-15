package mootable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestMooPointerTargetTypeChecked holds every moo read of the variable form to
// docs/SPEC-TABLES.md §3.1's rule for a pointer's target type
// (docs/rulings.json pointer-target-type-checked): a pointer slot that names a
// record of a type this reader DECLARES but that is not the type the slot
// requires is damage, and the whole load refuses; a record of a type the
// reader cannot name at all (a newer writer's) is counted unknown once and
// every pointer naming it reads null. Each row of
// testdata/wire/pointer-target-type/MANIFEST.tsv runs through the plain load,
// the strict read and the retaining load, and each must answer the row's
// verdict.
func TestMooPointerTargetTypeChecked(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted readers)")
	t.Parallel()
	dir := filepath.Join(root, "testdata", "wire", "pointer-target-type")
	manifest, err := os.ReadFile(filepath.Join(dir, "MANIFEST.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		caseName string
		root     string
		read     bool
		unknown  int
	}
	var rows []row
	unit := ""
	inputs := map[string][]byte{}
	for i, line := range strings.Split(string(manifest), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("MANIFEST.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		if unit == "" {
			unit = f[1]
		} else if unit != f[1] {
			t.Fatalf("MANIFEST.tsv:%d: unit %q, the rows before read %q", i+1, f[1], unit)
		}
		if f[4] != "read" && f[4] != "refused" {
			t.Fatalf("MANIFEST.tsv:%d: verdict %q, want read or refused", i+1, f[4])
		}
		r := row{caseName: f[0], root: f[2], read: f[4] == "read", unknown: -1}
		if f[5] != "-" {
			n, err := strconv.Atoi(f[5])
			if err != nil {
				t.Fatalf("MANIFEST.tsv:%d: unknown %q: %v", i+1, f[5], err)
			}
			r.unknown = n
		}
		wire, err := os.ReadFile(filepath.Join(dir, f[3]))
		if err != nil {
			t.Fatal(err)
		}
		inputs[r.caseName] = wire
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("MANIFEST.tsv names no vectors")
	}
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, unit)})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	var main strings.Builder
	main.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal L = require(\"./table_load\")\nlocal R = require(\"./table_retain\")\n")
	main.WriteString(g.requireAll())
	main.WriteString("local rows: { any } = {\n")
	for _, r := range rows {
		fmt.Fprintf(&main, "\t{ case = %q, root = %q, unknown = %d },\n", r.caseName, r.root, r.unknown)
	}
	main.WriteString(`}
local function line(row: any, path: string, v: any, rep: any)
	local nextNull = "-"
	local nextSet = "-"
	if v ~= nil then
		nextNull = tostring(v.Next == nil)
		nextSet = tostring(v.Next ~= nil)
	end
	print(string.format("%s\t%s\t%s\t%d\t%s\t%s", row.case, path,
		if v ~= nil and rep.reason == nil then "read" else "refused", rep.unknown, nextNull, nextSet))
end
for _, row in rows do
	local wire = inputs[row.case]
	local rep = L.newReport()
	line(row, "load", L.load(row.root, wire, rep), rep)
	rep = L.newReport()
	line(row, "strict", L.ReadStrict(row.root, wire, rep), rep)
	rep = L.newReport()
	line(row, "retain", R.LoadRetain(row.root, wire, rep, R.newStore(1048576, nil)), rep)
end
`)
	out := g.run(t, main.String(), inputs)
	got := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("the probe printed %q", line)
		}
		got[f[0]+"\t"+f[1]] = f[2:]
	}
	for _, r := range rows {
		for _, path := range []string{"load", "strict", "retain"} {
			key := r.caseName + "\t" + path
			f, ok := got[key]
			if !ok {
				t.Fatalf("%s: the probe answered nothing", key)
			}
			want := "refused"
			if r.read {
				want = "read"
			}
			if f[0] != want {
				t.Errorf("%s: %s answers %s, want %s", r.caseName, path, f[0], want)
				continue
			}
			if !r.read {
				continue
			}
			if r.unknown >= 0 {
				unknown, err := strconv.Atoi(f[1])
				if err != nil {
					t.Fatalf("%s: unknown %q: %v", key, f[1], err)
				}
				if unknown != r.unknown {
					t.Errorf("%s: unknown %d, want %d", key, unknown, r.unknown)
				}
			}
			if r.unknown > 0 && f[2] != "true" {
				t.Errorf("%s: next names a record of a type the reader cannot name, want null", key)
			}
			if r.unknown == 0 && f[3] != "true" {
				t.Errorf("%s: next names a Node record and reads null", key)
			}
		}
	}
}
