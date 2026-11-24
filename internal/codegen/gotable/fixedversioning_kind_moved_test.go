package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// kind_moved: a handed-in lineage entry whose `v` was int32 and is float32 to
// this reader, a kind off every ladder, is UNLAWFUL. No plan is laid, the load
// refuses layout_malformed with every counter zero, and every row handed in
// is back at its declared defaults (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT,
// §5.4, §5.9 #30; SPEC-TABLES §6.5). The second load refuses the same way.
func TestFixedVersioningKindMoved(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older := readSchema(t, "VOLD_kind_moved")
	newer := readSchema(t, "VNEW_kind_moved")
	table := fixedRootName(t, older)
	src := fmt.Sprintf(`package probe

import ("errors"; "os"; "reflect"; "testing")

func TestKindMoved(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]%[2]s, 8)
	want := make([]%[2]s, len(back))
	for i := range want { %[2]sReset(&want[i]) }
	plan := make([]TableFixedEntry, 4096)
	for attempt := range 2 {
		for i := range back { %[2]sReset(&back[i]); back[i].Lead = 99; back[i].V = 5.5; back[i].Trail = 88 }
		var r TableReport
		n := %[2]sFixedLoad(back, data, plan, &r)
		if n >= 0 || !errors.Is(r.Err(), TableRefuseLayoutMalformed) || r.Unknown != 0 || r.Widened != 0 {
			t.Fatalf("kind_moved attempt %%d: not refused layout_malformed: n=%%d err=%%v report=%%+v", attempt, n, r.Err(), r)
		}
		if !reflect.DeepEqual(back, want) {
			t.Fatalf("kind_moved attempt %%d: rows not reset to declared defaults: %%+v", attempt, back[0])
		}
	}
}
`, filepath.Join(corpus, "old_kind_moved.bin"), table)
	out, err := runVersionProbe(t, newer, []string{older}, src)
	if err != nil {
		t.Fatalf("kind_moved: %v\n%s", err, out)
	}
}
