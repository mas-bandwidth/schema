package lobstertable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6;
// docs/rulings.json strict-only-read), the shared cross-version vectors every
// leg reads (fixedvector.WriterRange): an OLD writer's file with one ranged
// value forged past that writer's range, read by the NEW reader whose own range
// is wider (or, for the _past vector, narrower than the forged value too). A
// scalar, one element of a ranged fixed array, a uint64 at or above 2^63
// (compared unsigned) and a compressed float (compared as the float32 it is,
// against the writer's float32-rounded bounds) each REFUSE the whole load
// out_of_range, and nothing lands. The expected answer is the C++ reference's
// manifest line (no expect= means the reference refused).
func TestLobsterFixedVersioningRangeWidenHostile(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Lobster toolchain and the C++ fixed-form corpus")
	corpus := lobsterFixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if !want.Refused {
				t.Fatalf("%s: the reference landed it; every writer's-range vector refuses under strict-only-read", vec.File)
			}
			data, err := os.ReadFile(filepath.Join(corpus, vec.File))
			if err != nil {
				t.Fatal(err)
			}
			older := lobsterReadSchema(t, "VOLD_"+vec.Row)
			newer := lobsterReadSchema(t, "VNEW_"+vec.Row)
			if root := lobsterFixedRoot(t, older).Name; root != vec.Root {
				t.Fatalf("VOLD_%s's fixed root is %s, the vector names %s", vec.Row, root, vec.Root)
			}
			pkg := lobsterUnitOf(t, newer).Package
			body := fmt.Sprintf(`
    let back:[%[1]s.%[2]s] = []
    for(8): push(back, %[1]s.New%[2]s())
    back[0].Lead = 0x5A5A5A5A
    let r = schema_rt.TableReport {}
    let plan = %[1]s.make_plan(4096)
    let n = %[1]s.%[2]sFixedLoad(back, read_in("in.bin"), plan, r)
    print "n=" + string(n) + " reason=" + r.reason + " lead=" + string(back[0].Lead)
`, pkg, vec.Root)
			out := strings.TrimSpace(runLobsterVersionProbe(t, newer, []string{older}, 0, body, map[string][]byte{"in.bin": data}))
			if wantOut := fmt.Sprintf("n=-1 reason=out_of_range lead=%d", 0x5A5A5A5A); out != wantOut {
				t.Fatalf("%s: got %q, want %q (refuse whole, nothing landed)", vec.File, out, wantOut)
			}
		})
	}
}
