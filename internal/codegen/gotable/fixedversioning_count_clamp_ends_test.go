package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ---- count_over_bound ends (roadmap go/C1 and go/C2, rewritten to the refusal) -
//
// The array-bounds work set carries two task nodes, go/C1 and go/C2, whose
// titles were "count clamp v<0" and "count clamp v>Max". The OWNER RULE
// (2026-10-15) replaced the count clamp with a NAMED REFUSAL: a count past the
// PLAN's bound — negative or over — refuses `count_over_bound` before any
// record lands, and every row handed in then holds its declared default (R4) (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.5). Row 4 (writer_bound_count) forges the
// count word to 7 — past the PLAN's bound of 4 but neither negative nor past
// the READER's own 8 — so it exercises neither end. Both ends here assert the
// refusal and that no destination byte moved.
func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_array_bounded_grow")
	writer := readSchema(t, "VOLD_array_bounded_grow")
	table := fixedRootName(t, writer)
	src := fmt.Sprintf(`package probe

import ("bytes"; "encoding/binary"; "os"; "testing"; "unsafe")

func TestCountClampEndsC1(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("count_over_bound_ends: the lead 0xAAAAAAAA followed by vals_count 4 is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("count_over_bound_ends: the forged-count needle occurs more than once, so the search is not a locator")
	}
	binary.LittleEndian.PutUint32(data[at+4:], 0xFFFFFFFF)
	back := make([]%[2]s, 8)
	pdst := tableFixedOverlay(unsafe.Pointer(&back[0]), unsafe.Sizeof(back[0]))
	for i := range pdst {
		pdst[i] = 0x5A
	}
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n >= 0 || r.Reason != "count_over_bound" {
		t.Fatalf("count_over_bound_ends C1: n=%%d reason=%%q, want count_over_bound", n, r.Reason)
	}
	var r4want %[2]s
	%[2]sReset(&r4want)
	for k := range back {
		if back[k] != r4want {
			t.Fatalf("count_over_bound_ends C1: R4: row %%d handed in is %%+v after the refusal, want its declared default %%+v", k, back[k], r4want)
		}
	}
}

func TestCountClampEndsC2(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("count_over_bound_ends: the lead 0xAAAAAAAA followed by vals_count 4 is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("count_over_bound_ends: the forged-count needle occurs more than once, so the search is not a locator")
	}
	binary.LittleEndian.PutUint32(data[at+4:], 9)
	back := make([]%[2]s, 8)
	pdst := tableFixedOverlay(unsafe.Pointer(&back[0]), unsafe.Sizeof(back[0]))
	for i := range pdst {
		pdst[i] = 0x5A
	}
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n >= 0 || r.Reason != "count_over_bound" {
		t.Fatalf("count_over_bound_ends C2: n=%%d reason=%%q, want count_over_bound", n, r.Reason)
	}
	var r4want %[2]s
	%[2]sReset(&r4want)
	for k := range back {
		if back[k] != r4want {
			t.Fatalf("count_over_bound_ends C2: R4: row %%d handed in is %%+v after the refusal, want its declared default %%+v", k, back[k], r4want)
		}
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	out, err := runVersionProbe(t, newer, []string{writer}, src)
	if err != nil {
		t.Fatalf("count_over_bound_ends: %v\n%s", err, out)
	}
}
