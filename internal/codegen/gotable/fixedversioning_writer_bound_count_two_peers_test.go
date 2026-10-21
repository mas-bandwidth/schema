package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// ---- writer_bound_count, two lineage peers (§5.8 row 4, rewritten to the refusal) -
//
// The reader is VNEW_array_bounded_grow ([..8]int32). It is handed TWO lineage
// peers with DIFFERENT bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE
// the file, and VMID_array_bounded_grow ([..6]int32), the distractor, which did
// not. The count word is forged to 7 — past the WRITING peer's bound of 4. The
// OWNER RULE (2026-10-15) replaced the count clamp with a NAMED REFUSAL: the
// forged count refuses `count_over_bound` against the WRITER's bound carried by
// the plan, before any destination byte is written.
func TestFixedVersioningWriterBoundCountTwoPeers(t *testing.T) {
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_array_bounded_grow")
	writer := readSchema(t, "VOLD_array_bounded_grow")
	distractor := readSchema(t, "VMID_array_bounded_grow")
	table := fixedRootName(t, writer)
	src := fmt.Sprintf(`package probe

import ("bytes"; "encoding/binary"; "os"; "testing"; "unsafe")

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
	pdst := tableFixedOverlay(unsafe.Pointer(&back[0]), unsafe.Sizeof(back[0]))
	for i := range pdst {
		pdst[i] = 0x5A
	}
	var r TableReport
	plan := make([]TableFixedEntry, 4096)
	n := %[2]sFixedLoad(back, data, plan, &r)
	if n >= 0 || r.Reason != "count_over_bound" {
		t.Fatalf("writer_bound_count: n=%%d reason=%%q, want count_over_bound", n, r.Reason)
	}
	for i, b := range pdst {
		if b != 0x5A {
			t.Fatalf("writer_bound_count: the refusal wrote destination byte %%d: 0x%%02X (§5.8: not one destination byte)", i, b)
		}
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	out, err := runVersionProbe(t, newer, []string{writer, distractor}, src)
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
