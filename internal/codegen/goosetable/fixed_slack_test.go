package goosetable

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4; docs/SPEC-CONFORMANCE.md, "Counted-array slots after a lawful fixed
// load"). test/tables/FixedSlack.schema's header states the recipe: save A
// with every counted array at its bound and every slot at 77, save B with
// every count at 1, load A and then B into the SAME values, and every slot
// past every count holds what a fresh value loaded from B holds there. Goose
// lands the whole record image, so before the reset a dead slot held the
// writer's zeros (cells[1].id 0, not the declared 7).

// slackDumpGoose prints one SlackRoot as one line per slot, every slot to the
// bound, so a dead slot's value is compared and not skipped.
const slackDumpGoose = `
fn dump(v: const %[1]s::SlackRoot&) {
    print("cells ", v.CellsCount, " nums ", v.NumsCount);
    var i: i64 = 0;
    while i < 3 {
        print("cell ", i, " ", v.Cells[i].Id, " ", v.Cells[i].SubsCount, " ", v.Cells[i].Subs[0].V, " ", v.Cells[i].Subs[1].V);
        i += 1;
    }
    i = 0;
    while i < 4 {
        print("num ", i, " ", v.Nums[i]);
        i += 1;
    }
}

fn main() {
    var a: %[1]s::SlackRoot[1] = [%[1]s::SlackRoot { .. }; 1];
    a[0].CellsCount = 3;
    a[0].NumsCount = 4;
    var i: i64 = 0;
    while i < 3 {
        a[0].Cells[i].Id = 77;
        a[0].Cells[i].SubsCount = 2;
        a[0].Cells[i].Subs[0].V = 77;
        a[0].Cells[i].Subs[1].V = 77;
        i += 1;
    }
    i = 0;
    while i < 4 { a[0].Nums[i] = 77; i += 1; }
    var b: %[1]s::SlackRoot[1] = [%[1]s::SlackRoot { .. }; 1];
    b[0].CellsCount = 1;
    b[0].NumsCount = 1;
    b[0].Cells[0].Id = 77;
    b[0].Cells[0].SubsCount = 1;
    b[0].Cells[0].Subs[0].V = 77;
    b[0].Nums[0] = 77;
    var fa: u8[>..] = [];
    var fb: u8[>..] = [];
    print("save ", %[1]s::SlackRootFixedSave(a, fa), " ", %[1]s::SlackRootFixedSave(b, fb));

    var rep = schema_rt::FixedReport { .. };
    var fresh: %[1]s::SlackRoot[1] = [%[1]s::SlackRoot { .. }; 1];
    print("== fresh ", %[1]s::SlackRootFixedLoad(fresh, fb, rep));
    dump(fresh[0]);

    var reused: %[1]s::SlackRoot[1] = [%[1]s::SlackRoot { .. }; 1];
    print("loada ", %[1]s::SlackRootFixedLoad(reused, fa, rep));
    print("== reused ", %[1]s::SlackRootFixedLoad(reused, fb, rep));
    dump(reused[0]);

    var erep = schema_rt::FixedEvolveReport { .. };
    var evolved: %[1]s::SlackRoot[1] = [%[1]s::SlackRoot { .. }; 1];
    print("loada ", %[1]s::SlackRootFixedEvolveLoad(evolved, fa, erep));
    print("== evolved ", %[1]s::SlackRootFixedEvolveLoad(evolved, fb, erep));
    dump(evolved[0]);

    var bad: u8[>..] = [];
    i = 0;
    while i < fb.len { bad.push(fb[i]); i += 1; }
    bad.pop();
    print("loada ", %[1]s::SlackRootFixedLoad(reused, fa, rep));
    print("== refused ", %[1]s::SlackRootFixedLoad(reused, bad, rep));
    dump(reused[0]);

    var newborn: %[1]s::SlackRoot[1] = [%[1]s::SlackRoot { .. }; 1];
    print("== newborn 0");
    dump(newborn[0]);
}
`

// slackSections splits the probe's output into its "== <name> <n>" sections:
// the load's answer and the dump lines under it (a cells, cell or num line).
func slackSections(t *testing.T, out string) map[string][]string {
	t.Helper()
	got := map[string][]string{}
	cur := ""
	for line := range strings.SplitSeq(out, "\n") {
		if name, ok := strings.CutPrefix(line, "== "); ok {
			cur = name
			got[cur] = nil
			continue
		}
		if cur != "" && (strings.HasPrefix(line, "cell") || strings.HasPrefix(line, "num ")) {
			got[cur] = append(got[cur], line)
		}
	}
	return got
}

func TestGooseFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose (it runs the generated reader over FixedSlack.schema)")
	u := fixedUnit(t, "test/tables/FixedSlack.schema")
	out := runCustomFixedGoose(t, u, nil, fmt.Sprintf(slackDumpGoose, goose.Namespace(u)), nil)
	if m := regexp.MustCompile(`(?m)^save (\d+) (\d+)$`).FindStringSubmatch(out); m == nil {
		t.Fatalf("the probe did not save both files:\n%s", out)
	}
	s := slackSections(t, out)
	// what a fresh value loaded from B holds: the live slots at 77, every
	// slot past a count at its declared default (SlackCell.id 7, SlackSub.v 3,
	// a nested count 0, an int32 element 0)
	want := []string{
		"cells 1 nums 1",
		"cell 0 77 1 77 3",
		"cell 1 7 0 3 3",
		"cell 2 7 0 3 3",
		"num 0 77",
		"num 1 0",
		"num 2 0",
		"num 3 0",
	}
	for _, sec := range []string{"fresh 1", "reused 1", "evolved 1"} {
		if got := s[sec]; strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: after loading B the value holds\n%s\nwant what a fresh value loaded from B holds\n%s\nprobe output:\n%s",
				sec, strings.Join(got, "\n"), strings.Join(want, "\n"), out)
		}
	}
	// a refused load leaves every value handed in at its declared defaults
	// (R4), the same as a value never loaded
	newborn := s["newborn 0"]
	if len(newborn) != 8 || newborn[1] != "cell 0 7 0 3 3" {
		t.Fatalf("a fresh SlackRoot dumps %q, want every cell at id 7 and every sub at v 3", newborn)
	}
	if got := s["refused -1"]; strings.Join(got, "\n") != strings.Join(newborn, "\n") {
		t.Errorf("a refused load left\n%s\nwant a fresh value\n%s\nprobe output:\n%s", strings.Join(got, "\n"), strings.Join(newborn, "\n"), out)
	}
}

// slackResetLine is the generated reset of one slot past a counted array's
// count inside a _<T>FixedRead.
var slackResetLine = regexp.MustCompile(`(?m)^\s+v\.(\w+)\[j\d+\] = (\w+) \{ \.\. \};$`)

// TestGooseFixedSlackResetEmitted pins the emitter without a toolchain: for
// FixedSlack.schema the generated read resets SlackRoot.cells past its count
// to a fresh SlackCell and SlackCell.subs to a fresh SlackSub, and for
// DeadBytes.schema, whose only counted array of a struct (DeadSmall) declares
// all-zero defaults, it emits no reset at all.
func TestGooseFixedSlackResetEmitted(t *testing.T) {
	t.Parallel()
	resets := func(schema string) map[string]string {
		t.Helper()
		files, err := Generate(fixedUnit(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, data := range files {
			for _, m := range slackResetLine.FindAllStringSubmatch(string(data), -1) {
				got[m[1]] = m[2]
			}
		}
		return got
	}
	got := resets("test/tables/FixedSlack.schema")
	if len(got) != 2 || got["Cells"] != "SlackCell" || got["Subs"] != "SlackSub" {
		t.Errorf("FixedSlack.schema: the generated reads reset %v, want Cells to SlackCell and Subs to SlackSub", got)
	}
	if got := resets("test/tables/DeadBytes.schema"); len(got) != 0 {
		t.Errorf("DeadBytes.schema: the generated reads reset %v, want no reset for all-zero element defaults", got)
	}
}
