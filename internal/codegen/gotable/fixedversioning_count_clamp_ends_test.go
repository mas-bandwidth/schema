package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ---- count_clamp_ends (roadmap go/C1 and go/C2) -----------------------------
//
// The array-bounds work set carries two task nodes, go/C1 and go/C2, whose
// titles are quoted here verbatim: "count clamp v<0" and "count clamp v>Max".
// They name the two ENDS of the count clamp. Row 4 (writer_bound_count) forges
// the count word to 7 — past the PLAN's bound of 4 but neither negative nor
// past the READER's own 8 — so it exercises neither end. On 2026-09-19 the
// negative arm was measured to be guarded by nothing on five legs: deleting it
// left every landed fixed-table test green. Clamped is asserted == 1 and never
// >= 1 because the bounds pass counts once per entry per record; a leg that
// also counts in the count op lands 2. The C2 count of 4 is the WRITER's bound
// carried by the plan, never the reader's own 8.
func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_array_bounded_grow")
	writer := readSchema(t, "VOLD_array_bounded_grow")
	table := fixedRootName(t, writer)
	src := fmt.Sprintf(`package probe

import ("bytes"; "encoding/binary"; "os"; "testing")

func TestCountClampEndsC1(t *testing.T) {
	data, err := os.ReadFile(%[1]q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("count_clamp_ends: the lead 0xAAAAAAAA followed by vals_count 4 is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("count_clamp_ends: the forged-count needle occurs more than once, so the search is not a locator")
	}
	binary.LittleEndian.PutUint32(data[at+4:], 0xFFFFFFFF)
	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("count_clamp_ends: the forged old file reads one record, n=%%d %%+v", n, r)
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("count_clamp_ends: a forged count moved a neighbour: %%+v", back[0])
	}
	if back[0].ValsCount != 0 {
		t.Fatalf("count_clamp_ends: the negative count landed %%d, not 0 (never -1, never the writer's 4, never the reader's 8): %%+v", back[0].ValsCount, back[0])
	}
	if r.Clamped != 1 {
		t.Fatalf("count_clamp_ends: the bounds pass counts once per entry per record, Clamped=%%d, want 1: %%+v", r.Clamped, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("count_clamp_ends: a counter other than Clamped moved: %%+v", r)
	}
	if r.Malformed || r.Verdict == TableOpenRefused || r.Reason != "" {
		t.Fatalf("count_clamp_ends: a forged count reads clean, not a refusal: %%+v", r)
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
		t.Fatal("count_clamp_ends: the lead 0xAAAAAAAA followed by vals_count 4 is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("count_clamp_ends: the forged-count needle occurs more than once, so the search is not a locator")
	}
	binary.LittleEndian.PutUint32(data[at+4:], 9)
	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("count_clamp_ends: the forged old file reads one record, n=%%d %%+v", n, r)
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("count_clamp_ends: a forged count moved a neighbour: %%+v", back[0])
	}
	if back[0].ValsCount != 4 {
		t.Fatalf("count_clamp_ends: the count landed %%d, not the WRITER's bound 4 (never the reader's 8, never the forged 9): %%+v", back[0].ValsCount, back[0])
	}
	for k, want := range []int32{1000, 1001, 1002, 1003} {
		if back[0].Vals[k] != want {
			t.Fatalf("count_clamp_ends: Vals[%%d] landed %%d, want %%d: %%+v", k, back[0].Vals[k], want, back[0])
		}
	}
	for k := 4; k < 8; k++ {
		if back[0].Vals[k] != 0 {
			t.Fatalf("count_clamp_ends: the reader's slot %%d is not its declared default 0: %%+v", k, back[0])
		}
	}
	if r.Clamped != 1 {
		t.Fatalf("count_clamp_ends: the bounds pass counts once per entry per record, Clamped=%%d, want 1: %%+v", r.Clamped, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("count_clamp_ends: a counter other than Clamped moved: %%+v", r)
	}
	if r.Malformed || r.Verdict == TableOpenRefused || r.Reason != "" {
		t.Fatalf("count_clamp_ends: a forged count reads clean, not a refusal: %%+v", r)
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	out, err := runVersionProbe(t, newer, []string{writer}, src)
	if err != nil {
		t.Fatalf("count_clamp_ends: %v\n%s", err, out)
	}
}
