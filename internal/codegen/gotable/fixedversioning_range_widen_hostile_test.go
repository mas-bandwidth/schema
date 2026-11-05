package gotable

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
	slowtest.Gate(t, "go test (a generated probe package) (it runs the generated NEW reader over the forged file)")
	corpus := fixedCorpus(t)
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
				expr := "back[0]." + strings.ToUpper(l.Field[:1]) + l.Field[1:]
				if l.Index >= 0 {
					expr += fmt.Sprintf("[%d]", l.Index)
				}
				fmt.Fprintf(&checks, "\tif got := fmt.Sprint(%[1]s); got != %[2]q {\n\t\tt.Errorf(\"%[3]s landed %%s, want %[2]s: the forged value clamps to the WRITER's range\", got)\n\t}\n",
					expr, fmt.Sprint(l.Value), l.Path)
			}
			older := readSchema(t, "VOLD_"+vec.Row)
			newer := readSchema(t, "VNEW_"+vec.Row)
			table := fixedRootName(t, older)
			src := fmt.Sprintf(`package probe

import ("fmt"; "os"; "testing")

func TestWriterRangeHostile(t *testing.T) {
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
%[3]s	if r.Clamped != %[4]d {
		t.Errorf("clamped=%%d, want exactly %[4]d: one forged value, one clamp", r.Clamped)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Errorf("a counter that must not move did: %%+v", r)
	}
}
`, filepath.Join(corpus, vec.File), table, checks.String(), want.Clamped)
			out, err := runVersionProbe(t, newer, []string{older}, src)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
		})
	}
}
