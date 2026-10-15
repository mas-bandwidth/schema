package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ---- writer_bound_count, two lineage peers (§5.8 row 4) ----------------------
//
// The reader is VNEW_array_bounded_grow ([..8]int32). It is handed TWO lineage
// peers with DIFFERENT bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE
// the file, and VMID_array_bounded_grow ([..6]int32), the distractor, which did
// not. The count word is forged to 5 — past the WRITER's bound of 4, inside the
// distractor's 6 and inside the reader's own 8. Since the owner's ruling of
// 2026-10-15 a count the plan's bound cannot hold REFUSES BY NAME
// `count_over_bound` and never clamps (docs/SPEC-TABLES.md §3.4 THE SLACK
// RULE): a reader holding its own bound or the distractor's lands the count,
// and only a plan carrying the WRITING peer's bound refuses.
func TestFixedVersioningWriterBoundCountTwoPeers(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_array_bounded_grow")
	writer := readSchema(t, "VOLD_array_bounded_grow")
	distractor := readSchema(t, "VMID_array_bounded_grow")
	table := fixedRootName(t, writer)
	src := fmt.Sprintf(`package probe

import ("bytes"; "encoding/binary"; "os"; "testing")

func TestWriterBoundCount(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("writer_bound_count: the lead 0xAAAAAAAA followed by vals_count 4 is not in the file")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("writer_bound_count: the forged-count needle occurs more than once, so the search is not a locator")
	}
	binary.LittleEndian.PutUint32(data[at+4:], 5)
	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != -1 || r.Reason != "count_over_bound" {
		t.Fatalf("writer_bound_count: 5 past the WRITING peer's bound of 4 REFUSES count_over_bound (never the distractor's 6, never the reader's 8), n=%%d reason=%%q: %%+v", n, r.Reason, r)
	}
	if r.Malformed || r.Clamped != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("writer_bound_count: REFUSE is total, no counter moves: %%+v", r)
	}
	var zero %[2]s
	if back[0] != zero {
		t.Fatalf("writer_bound_count: REFUSE writes not one destination byte: %%+v", back[0])
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	out, err := runVersionProbe(t, newer, []string{writer, distractor}, src)
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
