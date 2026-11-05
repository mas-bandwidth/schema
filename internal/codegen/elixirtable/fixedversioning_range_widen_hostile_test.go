package elixirtable

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
// is what the C++ reference's own NEW reader landed from the same bytes.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "elixir (it runs the generated NEW reader over the forged file)")
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
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
				expr := "v." + l.Field
				if l.Index >= 0 {
					expr = fmt.Sprintf("slot.(v.%s, %d)", l.Field, l.Index)
				}
				fmt.Fprintf(&checks, "  check(%[1]s == %[2]d, \"%[3]s landed #{inspect(%[1]s)}, want %[2]d: the forged value clamps to the WRITER's range\")\n",
					expr, l.Value, l.Path)
			}
			body := fmt.Sprintf(`  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :ok, "the forged file refused on the NEW build: #{inspect(tag)} #{why(report)}")
  check(length(values) == 1, "n == #{length(values)}, want 1")
  v = hd(values)
  slot = fn
    l, i when is_list(l) -> Enum.at(l, i)
    t, i when is_tuple(t) -> elem(t, i)
  end
  _ = slot
%[1]s  check(report.clamped == %[2]d, "clamped=#{report.clamped}, want exactly %[2]d: one forged value, one clamp")
  check(
    report.unknown == 0 and report.kind_mismatch == 0 and report.widened == 0 and
      report.duplicate == 0,
    "a counter that must not move did: #{why(report)}"
  )
  check(report.malformed == false, "a clamped read is not malformed")`, checks.String(), want.Clamped)
			out, err := runVersionProbe(t, elixirBin, "VNEW_"+vec.Row, []string{"VOLD_" + vec.Row}, 0,
				filepath.Join(corpus, vec.File), body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
