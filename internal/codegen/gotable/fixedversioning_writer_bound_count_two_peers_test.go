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
// not. The count word is forged to 7 exactly as the landed single-peer test
// forges it. The number that makes this test worth having is 6: a reader that
// clamps to its own bound lands 8, a reader that clamps to whichever peer it saw
// last lands 6, and only a plan that carries the WRITING peer's bound lands 4.
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
	binary.LittleEndian.PutUint32(data[at+4:], 7)
	back := make([]%[2]s, 8)
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n != 1 {
		t.Fatalf("writer_bound_count: the forged old file reads one record, n=%%d %%+v", n, r)
	}
	if back[0].ValsCount != 4 {
		t.Fatalf("writer_bound_count: the count landed %%d, not the WRITER's bound 4 (never the distractor's 6, never the reader's 8, never the forged 7): %%+v", back[0].ValsCount, back[0])
	}
	for k, want := range []int32{1000, 1001, 1002, 1003} {
		if back[0].Vals[k] != want {
			t.Fatalf("writer_bound_count: Vals[%%d] landed %%d, want %%d: %%+v", k, back[0].Vals[k], want, back[0])
		}
	}
	for k := 4; k < 8; k++ {
		if back[0].Vals[k] != 0 {
			t.Fatalf("writer_bound_count: the reader's slot %%d is not its declared default 0: %%+v", k, back[0])
		}
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("writer_bound_count: a mislaid size moved a neighbour: %%+v", back[0])
	}
	if r.Clamped != 1 {
		t.Fatalf("writer_bound_count: the bounds pass counts once per entry per record, Clamped=%%d, want 1: %%+v", r.Clamped, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
		t.Fatalf("writer_bound_count: a counter other than Clamped moved: %%+v", r)
	}
	if r.Malformed || r.Verdict == TableOpenRefused || r.Reason != "" {
		t.Fatalf("writer_bound_count: the forged count reads clean, not a refusal: %%+v", r)
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	out, err := runVersionProbe(t, newer, []string{writer, distractor}, src)
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
