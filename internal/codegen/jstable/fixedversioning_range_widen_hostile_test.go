package jstable

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. The reader clamps to the WRITER's
// range and counts `clamped` once: a scalar, one element of a ranged fixed
// array (held the same as a scalar), and a uint64 at or above 2^63 (compared
// unsigned, so it lands the max). The answer is the manifest's `expect=`, which
// is what the C++ reference's own NEW reader landed from the same bytes. A
// value is compared as its decimal text, so a uint64 held as a BigInt and an
// int32 held as a Number are spelled alike.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "node (it runs the generated NEW reader over the forged file)")
	corpus := jsFixedCorpus(t)
	node := jsNode(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			leaves, err := want.Leaves()
			if err != nil {
				t.Fatal(err)
			}
			var checks strings.Builder
			for _, l := range leaves {
				expr := "back[0]." + l.Field
				if l.Index >= 0 {
					expr += fmt.Sprintf("[%d]", l.Index)
				}
				fmt.Fprintf(&checks, "if (String(%[1]s) !== %[2]q) { fail(%[3]q + \" landed \" + String(%[1]s) + \", want %[2]s: the forged value clamps to the WRITER's range\"); }\n",
					expr, fmt.Sprint(l.Value), l.Path)
			}
			newer := jsReadSchema(t, "VNEW_"+vec.Row)
			older := jsReadSchema(t, "VOLD_"+vec.Row)
			table := jsFixedRootName(t, older)
			verdict := fmt.Sprintf(`if (n !== 1 || r.damaged || r.refused !== 0) { fail("the forged file reads one record: n=" + n + " " + reason(r)); }
%s`, checks.String())
			if want.Refused || want.Clamped > 0 {
				verdict = `if (n !== -1 || r.refused !== TableFixedRefusal.OutOfRange || r.damaged || r.oor !== 0) {
  fail("a value past the WRITER's range owes out_of_range: n=" + n + " " + reason(r));
}
if (show(back[0]) !== fresh0) {
  fail("a refused read must leave the destination untouched: " + show(back[0]));
}`
			}
			src := fmt.Sprintf(`
const data = readFileSync(%[1]q);
const back = []; for (let k = 0; k < 8; k++) { back.push(new T()); InitT(back[k]); }
const fresh0 = show(back[0]);
const r = new TableFixedReport();
const plan = TNewPlan(4096, 4096);
const n = TLoad(back, back.length, data, data.length, plan, r);
%[2]s
if (r.unknown !== 0 || r.mismatch !== 0 || r.widened !== 0 || r.repeated !== 0) { fail("a counter that must not move did: " + reason(r)); }
`, filepath.Join(corpus, vec.File), verdict)
			out, err := jsRunVersionProbe(t, node, newer, []string{older}, 0, table, src)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
