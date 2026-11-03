package gotable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vector every leg reads. hostile_range_widen.bin is the
// OLD writer's | 0..100 file with `v` forged to 150, inside the NEW reader's
// | 0..200; the NEW reader clamps it to the WRITER's max and counts `clamped`
// once. The answer is the manifest's `expect=`, which is what the C++
// reference's own NEW reader landed from the same bytes.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "go test (a generated probe package) (it runs the generated NEW reader over the forged file)")
	corpus := fixedCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	lead, err := want.Int("r0.lead")
	if err != nil {
		t.Fatal(err)
	}
	v, err := want.Int("r0.v")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := want.Int("r0.trail")
	if err != nil {
		t.Fatal(err)
	}
	older := readSchema(t, "VOLD_range_widen")
	newer := readSchema(t, "VNEW_range_widen")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestRangeWidenHostile(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := %[2]sFixedLoadTolerant(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("the forged file reads one record: n=%%d %%+v", n, r)
	}
	if back[0].V != %[3]d || r.Clamped != %[4]d {
		t.Fatalf("v=%%d clamped=%%d, want v=%[3]d clamped=%[4]d: the forged 150 clamps to the WRITER's max and counts once", back[0].V, r.Clamped)
	}
	if back[0].Lead != %[5]d || back[0].Trail != %[6]d {
		t.Fatalf("the bracketing fields moved: %%+v", back[0])
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("a counter that must not move did: %%+v", r)
	}
}
`, filepath.Join(corpus, fixedvector.RangeWidenHostile), table, v, want.Clamped, lead, trail)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("range_widen_hostile: %v\n%s", err, out)
	}
}
