package mootable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// checkedUnits are the two units this gate emits: a pointer graph and a
// message set, whose table modules between them load every runtime module.
var checkedUnits = map[string][]string{
	"pointers": {"tables/pointers"},
	"messages": {"tables/messages"},
}

// runChecked writes main.moo and runs it under the checked interpreter, which
// raises on arithmetic that leaves moo's number range.
func (g *generatedUnit) runChecked(t *testing.T, main string, inputs map[string][]byte) string {
	t.Helper()
	out, err := g.runCheckedErr(t, main, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// runCheckedErr is runChecked returning the failure instead of failing t (a
// negative control expects one).
func (g *generatedUnit) runCheckedErr(t *testing.T, main string, inputs map[string][]byte) (string, error) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.moo"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := moorun.RunCheckedErr(t, g.dir, "main.moo", inputs)
	return strings.TrimSpace(string(out)), err
}

// TestMooTableRuntimeLoadsUnderACheckedBuild holds the rule that the emitted
// moo table runtime loads, and computes the same bits, under a checked moo
// build as under the release pin: no arithmetic in it leaves moo's number
// range. Every table module of tables/pointers and tables/messages is required
// under the checked interpreter, and one instance is loaded and saved under
// both, whose output must be identical.
func TestMooTableRuntimeLoadsUnderACheckedBuild(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the checked moo interpreter (it loads the emitted table runtime)")
	units := map[string]*generatedUnit{}
	for _, key := range []string{"pointers", "messages"} {
		g := generateUnit(t, checkedUnits, key)
		if g.err != nil {
			t.Fatalf("unit %s is refused: %v", key, g.err)
		}
		mods := g.tableModules()
		if len(mods) == 0 {
			t.Fatalf("unit %s emitted no table module", key)
		}
		units[key] = g
		main := "--!strict\n" + g.requireAll() + fmt.Sprintf("print(\"loaded %d\")\n", len(mods))
		want := fmt.Sprintf("loaded %d", len(mods))
		if out := g.runChecked(t, main, nil); out != want {
			t.Fatalf("unit %s: the checked interpreter printed %q, want %q", key, out, want)
		}
	}

	g := units["pointers"]
	wire := readRoot(t, "testdata/wire/tables/graph_tree.bin")
	inputs := map[string][]byte{"wire": wire}
	main := strictRoundTrip(g, "Scene")
	release := g.run(t, main, inputs)
	if !strings.HasPrefix(release, "ok ") {
		t.Fatalf("the release interpreter did not load Scene: %s", release)
	}
	checked := g.runChecked(t, main, inputs)
	if checked != release {
		t.Fatalf("Scene under the checked interpreter is not the release result:\nchecked: %s\nrelease: %s", checked, release)
	}
	t.Logf("Scene load under both interpreters: %s", release)
}

// TestMooTableRuntimeLoadsUnderACheckedBuildNegativeControl plants the unbounded
// multiply back into an emitted copy of table_values.moo and requires the
// checked gate to go red by the overflow it raises, so the gate is known to be
// able to fail.
func TestMooTableRuntimeLoadsUnderACheckedBuildNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the checked moo interpreter (it loads the emitted table runtime)")
	g := generateUnit(t, checkedUnits, "pointers")
	if g.err != nil {
		t.Fatalf("unit pointers is refused: %v", g.err)
	}
	src := string(g.files["table_values.moo"])
	const bounded = "\t\tif i < 6 then\n\t\t\tp *= 128\n\t\tend"
	const unbounded = "\t\tp *= 128"
	if strings.Count(src, bounded) != 1 {
		t.Fatalf("the bounded power-table loop is not in the emitted table_values.moo exactly once:\n%s", src)
	}
	planted := strings.Replace(src, bounded, unbounded, 1)
	if err := os.WriteFile(filepath.Join(g.dir, "table_values.moo"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := g.runCheckedErr(t, "--!strict\n"+g.requireAll()+"print(\"loaded\")\n", nil)
	if err == nil {
		t.Fatalf("the checked gate stayed green with the unbounded power table: %s", out)
	}
	if !strings.Contains(err.Error(), "number overflow") {
		t.Fatalf("the checked gate went red without naming the overflow: %v", err)
	}
}
