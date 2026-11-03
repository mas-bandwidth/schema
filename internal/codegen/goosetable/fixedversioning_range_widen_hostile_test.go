package goosetable

import (
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vector every leg reads. hostile_range_widen.bin is the
// OLD writer's | 0..100 file with `v` forged to 150, inside the NEW reader's
// | 0..200; the NEW reader clamps it to the WRITER's max and counts `clamped`
// once. The answer is the manifest's `expect=`, which is what the C++
// reference's own NEW reader landed from the same bytes.
func TestGooseFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "goose (it runs the generated NEW reader over the forged file)")
	t.Parallel()
	corpus := fixedCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	older := evolutionLoad(t, "test/tables/VOLD_range_widen.schema")
	newer := evolutionLoad(t, "test/tables/VNEW_range_widen.schema")
	got := gooseVersionProbe(t, gooseVersionCase{reader: newer, older: []*ir.Unit{older}, root: "RangeWiden",
		capacity: 1, data: gooseCorpusFile(t, corpus, fixedvector.RangeWidenHostile),
		print: []string{"values[0].Lead", "values[0].V", "values[0].Trail"}})
	got.reads(t, 1)
	if len(got.printed) != 3 {
		t.Fatalf("range_widen_hostile: printed %v, want lead, v and trail", got.printed)
	}
	t.Logf("range_widen_hostile: landed v=%s clamped=%d", got.printed[1], got.clamped)
	for k, path := range []string{"r0.lead", "r0.v", "r0.trail"} {
		w, err := want.Int(path)
		if err != nil {
			t.Fatal(err)
		}
		if got.printed[k] != strconv.FormatInt(w, 10) {
			t.Errorf("range_widen_hostile: %s landed %s, want %d: the forged 150 clamps to the WRITER's max", path, got.printed[k], w)
		}
	}
	if got.clamped != want.Clamped {
		t.Errorf("range_widen_hostile: clamped=%d, want exactly %d", got.clamped, want.Clamped)
	}
	if got.widened != 0 || got.unknown != 0 || got.kindMismatch != 0 {
		t.Errorf("range_widen_hostile: a counter that must not move did: %s", got.raw)
	}
}
