package javatable

import (
	"path/filepath"
	"strconv"
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
// is what the C++ reference's own NEW reader landed from the same bytes. The
// probe dumps a primitive array as one `list:` value, so the expected
// elements are joined the same way.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "javac and java (it runs the generated NEW reader over the forged file)")
	corpus := corpusDir(t)
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
			expect := map[string]string{}
			var order []string
			for _, l := range leaves {
				v := strconv.FormatInt(l.Value, 10)
				prev, seen := expect[l.Field]
				if !seen {
					order = append(order, l.Field)
				}
				switch {
				case l.Index < 0:
					expect[l.Field] = v
				case !seen:
					expect[l.Field] = "list:" + v
				default:
					expect[l.Field] = prev + "," + v
				}
			}
			_, classes := buildRow(t, t.TempDir(), vec.Row, []sideSpec{
				{key: "reads", schema: "VNEW_" + vec.Row + ".schema", older: []string{"VOLD_" + vec.Row + ".schema"}},
			})
			r := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, vec.File))
			if r.n != 1 || r.refused || r.malformed {
				t.Fatalf("the forged file reads one record: %+v", r)
			}
			for _, field := range order {
				if got := r.value[field]; got != expect[field] {
					t.Errorf("%s landed %s, want %s: the forged value clamps to the WRITER's range", field, got, expect[field])
				}
			}
			if int64(r.clamped) != want.Clamped {
				t.Errorf("clamped=%d, want exactly %d: one forged value, one clamp", r.clamped, want.Clamped)
			}
			if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
				t.Errorf("a counter that must not move did: %+v", r)
			}
		})
	}
}
