package gotable

// THE PLAN CAPACITY THE LINEAGE NEEDS, EXPORTED (docs/FIXED-FORM-ALGORITHM.md
// §5.2, "AND THE CAPACITY A CALLER WOULD GUESS IS EXPORTED INSTEAD").
// <T>FixedLoad's plan argument stays a capacity check — §5.9 #5 and #45, the
// refusal R25 pins — but the size a lineage plan needs is the BUILD's number
// and not each adopter's guess: <T>FixedPlanMax is the largest entry count
// over the identity plan and every lineage plan, and a plan slice of that
// length never refuses plan_too_large for a layout the lineage holds.
//
// THE FIXTURE IS A LINEAGE OF TWO LOCKED LAYOUTS WHOSE OLDER PLAN IS THE
// LARGER: three int16 fields widened to int32. The identity plan coalesces
// the widened row into one copy run, while the compiled older plan gives each
// widening its own entry (a widen never coalesces), so the older plan is the
// larger one — and a slice ONE ENTRY under <T>FixedPlanMax refuses the older
// file plan_too_large, which is the exactness half of the rule: the export is
// the maximum, not a ceiling with slack in it.
//
// The harness is t04GoR19's (fixedroadmap_t04_test.go): BOTH sides are
// generated here — the OLD unit's identity plan writes the older image and
// the NEW unit, handed the OLD unit's locked lineage entry (§5.2), reads it
// back with its COMPILED plan — so nothing shells out to a foreign toolchain
// and nothing reads the C++ corpus: a bare `go test -run
// TestFixedPlanMaxCoversEveryLineagePlan` RUNS this, gate or no gate.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const planMaxOld = `package planmaxold

fixed table T
{
    a int16
    b int16
    c int16
    d int32
}
`

const planMaxNew = `package planmaxnew

fixed table T
{
    a int32
    b int32
    c int32
    d int32
}
`

const planMaxProbe = `package planmaxprobe

import (
	"testing"

	n "new_planmax"
	o "old_planmax"
)

func TestPlanMaxCoversEveryLineagePlan(t *testing.T) {
	// THE COVER: every plan the lineage built fits the exported capacity,
	// and the export is the EXACT maximum — the older lane is the largest
	// and the maximum is its count, so one entry under it must refuse.
	if n.TFixedPlan.Count > n.TFixedPlanMax {
		t.Fatalf("the identity plan has %d entries and FixedPlanMax is %d", n.TFixedPlan.Count, n.TFixedPlanMax)
	}
	for i, lane := range n.TFixedLineagePlans {
		if lane.Why != "" {
			t.Fatalf("lane %d refused at build time: %s", i, lane.Why)
		}
		if lane.Count > n.TFixedPlanMax {
			t.Fatalf("lane %d has %d entries and FixedPlanMax is %d", i, lane.Count, n.TFixedPlanMax)
		}
	}
	lane := n.TFixedLineagePlans[0]
	if lane.Count <= n.TFixedPlan.Count {
		t.Fatalf("the fixture's older plan (%d entries) must be the larger one over the identity plan (%d)", lane.Count, n.TFixedPlan.Count)
	}
	if n.TFixedPlanMax != lane.Count {
		t.Fatalf("FixedPlanMax = %d, want the largest plan's exact count %d", n.TFixedPlanMax, lane.Count)
	}

	// THE OLDER IMAGE, written by the older layout's own writer.
	old := make([]byte, o.TFixedMeasure(1))
	if got := o.TFixedSave([]o.T{{A: -1, B: 32767, C: -32768, D: 4}}, old); got != int64(len(old)) {
		t.Fatalf("the older writer saved %d of %d bytes", got, len(old))
	}
	newer := make([]byte, n.TFixedMeasure(1))
	if got := n.TFixedSave([]n.T{{A: 11, B: 22, C: 33, D: 44}}, newer); got != int64(len(newer)) {
		t.Fatalf("the newer writer saved %d of %d bytes", got, len(newer))
	}

	back := make([]n.T, 1)
	var r n.TableReport
	plan := make([]n.TableFixedEntry, n.TFixedPlanMax)

	// A PLAN OF <T>FixedPlanMax LOADS BOTH IMAGES the lineage holds.
	if got := n.TFixedLoad(back, old, plan, &r); got != 1 {
		t.Fatalf("a plan of FixedPlanMax loads the older image: n=%d %+v", got, r)
	}
	if back[0].A != -1 || back[0].B != 32767 || back[0].C != -32768 || back[0].D != 4 {
		t.Fatalf("the widened values did not land: %+v", back[0])
	}
	back[0] = n.T{}
	if got := n.TFixedLoad(back, newer, plan, &r); got != 1 {
		t.Fatalf("a plan of FixedPlanMax loads the identity image: n=%d %+v", got, r)
	}
	if back[0].A != 11 || back[0].B != 22 || back[0].C != 33 || back[0].D != 44 {
		t.Fatalf("the identity values did not land: %+v", back[0])
	}

	// ONE ENTRY FEWER REFUSES THE LARGER plan by name (§5.2's exactness):
	// the refusal R25 keeps, now owed at the one boundary that matters.
	short := make([]n.TableFixedEntry, n.TFixedPlanMax-1)
	back[0] = n.T{}
	r = n.TableReport{}
	if got := n.TFixedLoad(back, old, short, &r); got != -1 || r.Reason != "plan_too_large" {
		t.Fatalf("one entry under FixedPlanMax owes plan_too_large on the larger plan: n=%d %+v", got, r)
	}
	if back[0] != (n.T{}) {
		t.Fatalf("REFUSE is total: the short plan wrote destination bytes: %+v", back[0])
	}
}
`

func TestFixedPlanMaxCoversEveryLineagePlan(t *testing.T) {
	t.Parallel()
	oldU := unitFrom(t, planMaxOld)
	newU := unitFrom(t, planMaxNew)
	older, ok := FixedLineageOf(oldU, "T")
	if !ok {
		t.Fatal("the older unit has no lineage entry for T")
	}
	lineage := map[string][]FixedLineageEntry{"T": {older}}

	root := t.TempDir()
	planMaxWriteModule(t, filepath.Join(root, "old_planmax"), "old_planmax", oldU, false, nil)
	planMaxWriteModule(t, filepath.Join(root, "new_planmax"), "new_planmax", newU, true, lineage)

	probe := filepath.Join(root, "planmaxprobe")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module planmaxprobe\n\ngo 1.26\n\nrequire (\n\tnew_planmax v0.0.0\n\told_planmax v0.0.0\n)\n\n" +
		"replace new_planmax => ../new_planmax\nreplace old_planmax => ../old_planmax\n"
	if err := os.WriteFile(filepath.Join(probe, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "planmax_test.go"), []byte(planMaxProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "600s", ".")
	cmd.Dir = probe
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FixedPlanMax did not cover every lineage plan (§5.2):\n%s", out)
	}
}

// planMaxWriteModule writes one generated unit as its own module: the packet
// emitter's declarations folded in (t04MergeGo's step), and — for the NEWER
// side — the table files regenerated through GenerateLineage with the older
// unit's locked entry handed in, which is what makes the module's lineage two
// layouts deep (§5.2, COMPILE(lock, T)). Scalars only, so no sibling runtime
// is required and the go.mod is bare.
func planMaxWriteModule(t *testing.T, dir, mod string, u *ir.Unit, withLineage bool, lineage map[string][]FixedLineageEntry) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatalf("%s: golang.Generate: %v", mod, err)
	}
	var tables map[string][]byte
	if withLineage {
		tables, err = GenerateLineage(u, lineage)
	} else {
		tables, err = Generate(u)
	}
	if err != nil {
		t.Fatalf("%s: table generate: %v", mod, err)
	}
	maps.Copy(files, tables)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if filepath.Ext(name) != ".go" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gm := fmt.Sprintf("module %s\n\ngo 1.26\n", mod)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gm), 0o600); err != nil {
		t.Fatal(err)
	}
}
