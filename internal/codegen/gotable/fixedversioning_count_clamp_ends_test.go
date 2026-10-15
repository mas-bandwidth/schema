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
// They name the two ENDS of the count word's range — and since the owner's
// ruling of 2026-10-15 NEITHER END CLAMPS: a count the PLAN's bound cannot
// hold, negative or past the bound, REFUSES BY NAME `count_over_bound` before
// any destination byte is written (docs/SPEC-TABLES.md §3.4 THE SLACK RULE,
// docs/FIXED-FORM-ALGORITHM.md §4.5). On 2026-09-19 the negative arm was
// measured to be guarded by nothing on five legs: deleting it left every
// landed fixed-table test green, so both ends stay pinned here — as refusals.
// REFUSE is total: no counter moves, the report's counters are asserted all
// zero, and not one destination byte is written. The bound is the WRITER's
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
	if n != -1 || r.Reason != "count_over_bound" {
		t.Fatalf("count_clamp_ends: the negative end REFUSES count_over_bound, n=%%d reason=%%q: %%+v", n, r.Reason, r)
	}
	if r.Malformed || r.Clamped != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("count_clamp_ends: REFUSE is total, no counter moves: %%+v", r)
	}
	var zero %[2]s
	if back[0] != zero {
		t.Fatalf("count_clamp_ends: REFUSE writes not one destination byte: %%+v", back[0])
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
	if n != -1 || r.Reason != "count_over_bound" {
		t.Fatalf("count_clamp_ends: 9 past the WRITER's bound of 4 REFUSES count_over_bound (never clamps to 4, never to the reader's 8), n=%%d reason=%%q: %%+v", n, r.Reason, r)
	}
	if r.Malformed || r.Clamped != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("count_clamp_ends: REFUSE is total, no counter moves: %%+v", r)
	}
	var zero %[2]s
	if back[0] != zero {
		t.Fatalf("count_clamp_ends: REFUSE writes not one destination byte: %%+v", back[0])
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	out, err := runVersionProbe(t, newer, []string{writer}, src)
	if err != nil {
		t.Fatalf("count_clamp_ends: %v\n%s", err, out)
	}
}
