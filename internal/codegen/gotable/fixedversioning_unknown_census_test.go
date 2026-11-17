package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// A handed-in lineage that removed Item.drop is unlawful: the whole load
// refuses before any record lands, with no compile census or other counter
// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.4, §5.8 rule 6).
func TestFixedVersioningUnknownCensus(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_unknown_census")
	newer := readSchema(t, "VNEW_unknown_census")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("os"; "reflect"; "testing")

func TestUnknownCensus(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
 for i := range back { %[2]sReset(&back[i]); back[i].Lead=99; back[i].Trail=88 }
 before := append([]%[2]s(nil), back...)
 plan := make([]TableFixedEntry, 4096)
 for attempt := range 2 {
  var r TableReport
  n := %[2]sFixedLoad(back, data, plan, &r)
  if n >= 0 || r.Err() == nil || r.Unknown != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0 {
   t.Fatalf("unlawful lineage attempt %%d: n=%%d report=%%+v", attempt,n,r)
  }
  if !reflect.DeepEqual(back, before) { t.Fatal("unlawful lineage wrote destination") }
 }

}
`, filepath.Join(corpus, "old_unknown_census.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("unknown_census: %v\n%s", err, out)
	}
}
