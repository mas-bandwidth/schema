package gotable

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A widened reader still refuses a value outside the selected writer range.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "go test (a generated probe package)")
	corpus := fixedCorpus(t)
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			older := readSchema(t, "VOLD_"+vec.Row)
			newer := readSchema(t, "VNEW_"+vec.Row)
			table := fixedRootName(t, older)
			src := fmt.Sprintf(`package probe
import ("os"; "reflect"; "testing")
func TestWriterRangeHostile(t *testing.T) {
 t.Parallel()
 data, err := os.ReadFile(%[1]q)
 if err != nil { t.Fatal(err) }
 back := make([]%[2]s, 8)
 plan := make([]TableFixedEntry, 4096)
 var report TableReport
 if n := %[2]sFixedLoad(back, data, plan, &report); n >= 0 || report.Err() == nil {
  t.Fatalf("forged writer range accepted: n=%%d report=%%+v", n, report)
 }
 expected := make([]%[2]s, 8)
 %[2]sReset(&expected[0])
 if !reflect.DeepEqual(back, expected) { t.Fatal("refusal retained a partial value") }
}
`, filepath.Join(corpus, vec.File), table)
			out, err := runVersionProbe(t, newer, []string{older}, src)
			if err != nil {
				t.Fatalf("%s: %v\n%s", vec.File, err, out)
			}
		})
	}
}
