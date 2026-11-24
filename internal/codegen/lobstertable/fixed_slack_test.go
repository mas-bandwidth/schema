package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"), the recipe in test/tables/FixedSlack.schema's header: load A (every
// counted array at its bound, every slot 77) and then B (every count 1) into
// the SAME caller values through SlackRootFixedLoad, and every slot to the
// bound holds what a fresh value loaded from B holds there. The leg holds the
// rule already (each <T>FixedScatter resets the slots past a count with
// <E>Reset, fixedevolve.go scatterField); this test guards it.
func TestLobsterFixedSlackPastCountHoldsDefaults(t *testing.T) {
	slowtest.Gate(t, "lobster (it runs the generated fixed load over FixedSlack.schema)")
	t.Parallel()
	dir := tableSchemaFiles(t, "FixedSlack.schema")
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(lobsterSlackProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(lobsterrun.Run(t, dir, "main.lobster", nil)))
	want := []string{
		"cells 1 nums 1",
		"cell 0 77 1 77 3",
		"cell 1 7 0 3 3",
		"cell 2 7 0 3 3",
		"num 77 0 0 0",
	}
	for _, tag := range []string{"fresh", "reused"} {
		if got := lobsterSlackLines(out, tag); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: after loading B the value holds\n%s\nwant what a fresh value loaded from B holds\n%s\nprobe output:\n%s",
				tag, strings.Join(got, "\n"), strings.Join(want, "\n"), out)
		}
	}
	if !strings.Contains(out, "refused -1") {
		t.Fatalf("the truncated file read:\n%s", out)
	}
	newborn := lobsterSlackLines(out, "newborn")
	if len(newborn) != 5 || newborn[1] != "cell 0 7 0 3 3" {
		t.Fatalf("a fresh SlackRoot dumps %q", newborn)
	}
	if got := lobsterSlackLines(out, "refused"); strings.Join(got, "\n") != strings.Join(newborn, "\n") {
		t.Errorf("a refused load left %q, want a fresh value %q\nprobe output:\n%s", got, newborn, out)
	}
}

func lobsterSlackLines(out, tag string) []string {
	var got []string
	for line := range strings.SplitSeq(out, "\n") {
		if rest, ok := strings.CutPrefix(line, tag+" "); ok && rest != "-1" {
			got = append(got, rest)
		}
	}
	return got
}

const lobsterSlackProbe = `import .FixedSlackTable
// every slot to the bound, so a dead slot's value is compared and not skipped
def dump(tag:string, r:tblslack.SlackRoot):
    print(tag + " cells " + string(r.CellsCount) + " nums " + string(r.NumsCount))
    for(3) i:
        let c = r.Cells[i]
        print(tag + " cell " + string(i) + " " + string(c.Id) + " " + string(c.SubsCount) + " " + string(c.Subs[0].V) + " " + string(c.Subs[1].V))
    print(tag + " num " + string(r.Nums[0]) + " " + string(r.Nums[1]) + " " + string(r.Nums[2]) + " " + string(r.Nums[3]))
let a = tblslack.NewSlackRoot()
a.CellsCount = 3
a.NumsCount = 4
for(3) i:
    a.Cells[i].Id = 77
    a.Cells[i].SubsCount = 2
    for(2) j: a.Cells[i].Subs[j].V = 77
for(4) i: a.Nums[i] = 77
let b = tblslack.NewSlackRoot()
b.CellsCount = 1
b.NumsCount = 1
b.Cells[0].Id = 77
b.Cells[0].SubsCount = 1
b.Cells[0].Subs[0].V = 77
b.Nums[0] = 77
let fa = tblslack.SlackRootFixedSave([a])
let fb = tblslack.SlackRootFixedSave([b])
let plan:[schema_rt.TableFixedEntry] = []
for(4096): push(plan, schema_rt.TableFixedEntry {})
let fresh = [tblslack.NewSlackRoot()]
tblslack.SlackRootFixedLoad(fresh, fb, plan, nil)
dump("fresh", fresh[0])
let values = [tblslack.NewSlackRoot()]
tblslack.SlackRootFixedLoad(values, fa, plan, nil)
tblslack.SlackRootFixedLoad(values, fb, plan, nil)
dump("reused", values[0])
tblslack.SlackRootFixedLoad(values, fa, plan, nil)
print("refused " + string(tblslack.SlackRootFixedLoad(values, substring(fb, 0, length(fb) - 1), plan, schema_rt.TableReport {})))
dump("refused", values[0])
dump("newborn", tblslack.NewSlackRoot())
`
