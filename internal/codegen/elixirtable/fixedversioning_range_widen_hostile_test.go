package elixirtable

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
	slowtest.Gate(t, "elixir (it runs the generated NEW reader over the forged file)")
	corpus := fixedCorpus(t)
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
	elixirBin := elixirBinary(t)
	body := fmt.Sprintf(`  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the forged file refused on the NEW build: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  IO.puts("range_widen_hostile: landed v=#{v.v} clamped=#{report.clamped}")
  check(v.v == %[1]d and report.clamped == %[2]d, "v=#{v.v} clamped=#{report.clamped}, want v=%[1]d clamped=%[2]d: the forged 150 clamps to the WRITER's max and counts once")
  leadtrail(v, %[3]d, %[4]d)
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a counter that must not move did: #{why(report)}"
  )
  check(report.malformed == false, "a clamped read is not malformed")`, v, want.Clamped, lead, trail)
	out, err := runVersionProbe(t, elixirBin, "VNEW_range_widen", []string{"VOLD_range_widen"}, 0,
		filepath.Join(corpus, fixedvector.RangeWidenHostile), body)
	if err != nil {
		t.Fatalf("range_widen_hostile: %v\n%s", err, out)
	}
	t.Logf("%s", out)
}
