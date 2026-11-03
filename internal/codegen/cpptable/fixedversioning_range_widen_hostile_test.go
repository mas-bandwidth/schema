package cpptable

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
// once. The answer is the manifest's `expect=`, which the dump wrote from this
// leg's own reader over the same bytes; the probe here reads the FILE, so the
// generated lineage reader is held to it too.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "a C++ compiler (it runs the generated NEW reader over the forged file)")
	corpus := cppFixedCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := want.Int("r0.v")
	if err != nil {
		t.Fatal(err)
	}
	lead, err := want.Int("r0.lead")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := want.Int("r0.trail")
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`
    printf( "range_widen_hostile: landed v=%%d clamped=%%d\n", (int) back[0].v, (int) r.clamped );
    if ( n != 1 || r.malformed || r.refused ) { printf( "range_widen_hostile: n=%%lld malformed=%%d refused=%%d, want one record\n", (long long) n, (int) r.malformed, (int) r.refused ); return 1; }
    if ( back[0].v != %[1]d || r.clamped != %[2]d ) { printf( "range_widen_hostile: v=%%d clamped=%%d, want v=%[1]d clamped=%[2]d: the forged 150 clamps to the WRITER's max and counts once\n", (int) back[0].v, (int) r.clamped ); return 1; }
    if ( back[0].lead != %[3]du || back[0].trail != %[4]du ) { printf( "range_widen_hostile: a bracket moved: lead=%%u trail=%%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 ) { printf( "range_widen_hostile: a counter that must not move did\n" ); return 1; }
`, v, want.Clamped, lead, trail)
	out, err := cppRunVersionProbe(t, "range_widen", filepath.Join(corpus, fixedvector.RangeWidenHostile), body, "")
	if err != nil {
		t.Fatalf("range_widen_hostile: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
