package elixirtable

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider. Under the one strict read, a
// value past the writing endpoint's range refuses the whole load; the probe
// pins that verdict for every vector.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "elixir (it runs the generated NEW reader over the forged file)")
	corpus := fixedCorpus(t)
	elixirBin := elixirBinary(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			if _, err := fixedvector.Read(corpus, vec.File); err != nil {
				t.Fatal(err)
			}
			body := `  data = File.read!(file())
  {tag, values, report} = load(data)
  check(tag == :error,
    "a forged ranged value must refuse on the NEW build: #{inspect(tag)} #{why(report)}")
  check(not is_list(values),
    "the refusal built no value: #{inspect(values)}")
  check(report.unknown == 0 and report.widened == 0,
    "the refusal moved nothing: #{why(report)}")`
			out, err := runVersionProbe(t, elixirBin, "VNEW_"+vec.Row, []string{"VOLD_" + vec.Row}, 0,
				filepath.Join(corpus, vec.File), body)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
			t.Logf("%s", out)
		})
	}
}
