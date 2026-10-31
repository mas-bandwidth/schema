package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// vcAdd is one unit's census: the sums of the per-file counts.
func vcAdd(a, b lobstertable.VariableClassCounts) lobstertable.VariableClassCounts {
	return lobstertable.VariableClassCounts{
		Pointers:            a.Pointers + b.Pointers,
		PointerArrays:       a.PointerArrays + b.PointerArrays,
		UnionsWithTableArms: a.UnionsWithTableArms + b.UnionsWithTableArms,
		UnionArms:           a.UnionArms + b.UnionArms,
		PayloadFreeArms:     a.PayloadFreeArms + b.PayloadFreeArms,
		UnionArrays:         a.UnionArrays + b.UnionArrays,
	}
}

// vcCensus is one unit's census over every file it names.
func vcCensus(u *ir.Unit) lobstertable.VariableClassCounts {
	var c lobstertable.VariableClassCounts
	for _, f := range u.Files {
		c = vcAdd(c, lobstertable.VariableClassCensus(f))
	}
	return c
}

func vcString(c lobstertable.VariableClassCounts) string {
	return fmt.Sprintf("%d,%d,%d,%d,%d,%d", c.Pointers, c.PointerArrays, c.UnionsWithTableArms, c.UnionArms, c.PayloadFreeArms, c.UnionArrays)
}

// vcCheckCensus runs a program over the generated modules that prints each
// module's emitted <Base>VariableClassCensus(), and holds it to the IR. The
// program imports runtime/variable_class.lobster, so the join to the load's
// hook table is compiled and run too.
func vcCheckCensus(t *testing.T, g *loadUnit) {
	t.Helper()
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .variable_class\n")
	for _, m := range g.mods {
		base := strings.TrimSuffix(m, "Table")
		fmt.Fprintf(&b, "print(%s + \" \" + %s.%sVariableClassCensus())\n", lobster.StringLit([]byte(base)), g.u.Package, base)
	}
	path := filepath.Join(g.dir, "census.lobster")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimRight(string(lobsterrun.Run(t, g.dir, "census.lobster", nil)), "\n")
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		x := strings.Fields(line)
		if len(x) == 2 {
			got[x[0]] = x[1]
		} else {
			t.Fatalf("unit %s: census line %q is not <module> <counts>", g.u.Package, line)
		}
	}
	for _, m := range g.mods {
		base := strings.TrimSuffix(m, "Table")
		var f *ir.File
		for _, cand := range g.u.Files {
			if cand.Base == base {
				f = cand
				break
			}
		}
		if f == nil {
			t.Fatalf("module %s has no file %s", m, base)
		}
		want := vcString(lobstertable.VariableClassCensus(f))
		if got[base] != want {
			t.Errorf("module %s: the emitted census is %q, the IR's is %q", m, got[base], want)
		}
		if got[base] == "" {
			t.Errorf("module %s: the census function was not emitted", m)
		}
	}
	if !strings.Contains(censusSource(t, g.dir), "variable_class_carries") {
		t.Errorf("the census program did not compile runtime/variable_class.lobster")
	}
}

// censusSource reads the emitted variable-class runtime from the unit
// directory, so the test names the hook file it ran.
func censusSource(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "variable_class.lobster"))
	if err != nil {
		t.Fatalf("the variable-class runtime was not emitted: %v", err)
	}
	return string(data)
}

// TestLobsterVariableClass generates every manifest unit that uses the
// variable class — pointers over the flat node table, arrays of pointers,
// unions with table arms, union arms of any field type, payload-free arms and
// arrays of unions (docs/SPEC-TABLES.md §2.1, §2.6, §2.9, §3.1) — and holds
// its load, save and re-load to the Go reference engine
// (internal/tablewire, internal/tabletext): every report and every value dump
// byte for byte and report for report. The constructs are named with their
// counts, both from the IR and from the census the emitter writes into each
// generated module.
func TestLobsterVariableClass(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	expect := reportLines(t)
	rows := loadRows(t)
	byUnit := map[string][]loadRow{}
	var order []string
	for _, row := range rows {
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	var totals lobstertable.VariableClassCounts
	selected, reports, values, instances := 0, 0, 0, 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatalf("unit %s does not load: %v", key, err)
		}
		c := vcCensus(u)
		if c.Total() == 0 {
			continue
		}
		selected++
		totals = vcAdd(totals, c)
		t.Logf("unit %s constructs: %s", key, vcString(c))
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s: the variable class is refused: %v", key, err)
		}
		vcCheckCensus(t, g)

		unitRows := byUnit[key]
		res := runUnit(t, g, unitRows)
		for i, row := range unitRows {
			line, dump := res.lob[2*i], res.lob[2*i+1]
			if line != res.goLine[i] {
				t.Errorf("%s %s: the report is not the reference's:\nlobster: %s\ngo:      %s", row.kind, row.name, line, res.goLine[i])
				continue
			}
			if dump != res.goDump[i] {
				t.Errorf("%s %s: the loaded value is not the reference's at byte %s", row.kind, row.name, divergence(dump, res.goDump[i]))
				continue
			}
			if row.kind == "report" {
				want, ok := expect[row.name]
				switch {
				case !ok:
					t.Errorf("report %s: reports.txt names no such case", row.name)
				case line != want:
					t.Errorf("report %s: lobster %s, reports.txt %s", row.name, line, want)
				default:
					reports++
				}
			}
			values++
		}

		var saveRows []loadRow
		var want [][]byte
		for _, row := range unitRows {
			if row.kind != "instance" {
				continue
			}
			wire := row.data
			if wire == nil {
				if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
					t.Fatalf("%s: %v", row.name, err)
				}
			}
			out, goErr := goSave(g, row.root, wire)
			if goErr != nil {
				t.Logf("instance %s is refused by the Go writer and not compared: %v", row.name, goErr)
				continue
			}
			saveRows = append(saveRows, row)
			want = append(want, out)
		}
		if len(saveRows) == 0 {
			continue
		}
		sres := runSaveRows(t, g, saveRows)
		for i, row := range saveRows {
			wantHex := hex.EncodeToString(want[i])
			if sres.saved[i] != wantHex {
				t.Errorf("instance %s: the saved bytes are not the reference's:\nlobster: %s\ngo:      %s", row.name, sres.saved[i], wantHex)
				continue
			}
			_, goDump := goLoad(g.u, row.root, want[i])
			if sres.dump[i] != goDump {
				t.Errorf("instance %s: the re-loaded value is not the reference's at byte %s", row.name, divergence(sres.dump[i], goDump))
				continue
			}
			instances++
		}
	}
	if selected == 0 {
		t.Fatalf("no manifest unit uses the variable class: the comparison is vacuous")
	}
	if reports == 0 || values == 0 || instances == 0 {
		t.Fatalf("compared %d report rows, %d values and %d saved instances: the comparison is vacuous", reports, values, instances)
	}
	t.Logf("constructs: pointers=%d arrays_of_pointers=%d unions_with_table_arms=%d union_arms=%d payload_free_arms=%d arrays_of_unions=%d",
		totals.Pointers, totals.PointerArrays, totals.UnionsWithTableArms, totals.UnionArms, totals.PayloadFreeArms, totals.UnionArrays)
	t.Logf("%d units, %d report rows, %d values loaded equal to the reference's, %d instances saved and re-loaded byte for byte", selected, reports, values, instances)
}

// TestLobsterVariableClassNegativeControl patches the flat node table's index
// shift (docs/SPEC-TABLES.md §3.1: wire index k + 2 names nodes[k]) out of the
// emitted runtime — matched exactly once in load.lobster — and requires a
// pointer unit's value dump to go red, with the unpatched control green.
func TestLobsterVariableClassNegativeControl(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	var row loadRow
	found := false
	for _, r := range loadRows(t) {
		if r.kind != "instance" {
			continue
		}
		paths, err := compiler.GatherPaths(prefix(units[r.unit]))
		if err != nil {
			t.Fatal(err)
		}
		u, err := compiler.New().Load(paths)
		if err != nil {
			t.Fatal(err)
		}
		if vcCensus(u).Pointers > 0 {
			row, found = r, true
			break
		}
	}
	if !found {
		t.Fatalf("the manifest names no instance row in a unit with a pointer")
	}
	paths, err := compiler.GatherPaths(prefix(units[row.unit]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatal(err)
	}
	control := runUnit(t, g, []loadRow{row})
	if control.lob[1] != control.goDump[0] {
		t.Fatalf("the control, unpatched: %s is not the reference's", row.name)
	}
	path := filepath.Join(g.dir, "load.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "    if index >= 2 and index - 2 < length(st.nodes):\n        let node = st.nodes[index - 2]\n"
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the node-table shift is in the emitted runtime %d times, want 1", n)
	}
	patched := strings.Replace(string(src), anchor, "    if index >= 2 and index - 1 < length(st.nodes):\n        let node = st.nodes[index - 1]\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	after := runUnit(t, g, []loadRow{row})
	if after.lob[1] == after.goDump[0] {
		t.Fatalf("the patched runtime still loads the reference's value: %s", after.lob[1])
	}
	t.Logf("the value comparison went red under the shift patch: %s at byte %s", row.name, divergence(after.lob[1], after.goDump[0]))
}
