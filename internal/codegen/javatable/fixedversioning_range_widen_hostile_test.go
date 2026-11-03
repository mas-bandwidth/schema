package javatable

import (
	"path/filepath"
	"strconv"
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
	slowtest.Gate(t, "javac and java (it runs the generated NEW reader over the forged file)")
	corpus := corpusDir(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	_, classes := buildRow(t, t.TempDir(), "range_widen", []sideSpec{
		{key: "reads", schema: "VNEW_range_widen.schema", older: []string{"VOLD_range_widen.schema"}},
	})
	r := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, fixedvector.RangeWidenHostile))
	t.Logf("range_widen_hostile: landed v=%s clamped=%d", r.value["v"], r.clamped)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("the forged file reads one record: %+v", r)
	}
	for _, path := range []string{"r0.lead", "r0.v", "r0.trail"} {
		w, err := want.Int(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.value[path[len("r0."):]]; got != strconv.FormatInt(w, 10) {
			t.Errorf("%s landed %s, want %d: the forged 150 clamps to the WRITER's max", path, got, w)
		}
	}
	if int64(r.clamped) != want.Clamped {
		t.Errorf("clamped=%d, want exactly %d", r.clamped, want.Clamped)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
		t.Errorf("a counter that must not move did: %+v", r)
	}
}
