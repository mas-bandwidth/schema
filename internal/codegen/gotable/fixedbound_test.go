package gotable

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestFixedFormCountAndLengthOverBoundRefuseByName is the red-then-green pin for
// the OWNER RULE (2026-10-15): a fixed-form count or length past the PLAN's
// bound refuses BY NAME, `count_over_bound` / `length_over_bound`, before any
// destination byte is written — it never clamps (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md §4.5). A counted `[..95]T` and a `string(41)`
// carry forged counts/lengths past their bounds, each with and without slack
// behind the bound, on the IDENTITY plan and on a compiled lineage plan, and in
// every case the load refuses by name and writes no destination byte (§5.8:
// REFUSE is total).
func TestFixedFormCountAndLengthOverBoundRefuseByName(t *testing.T) {
	t.Parallel()

	// IDENTITY plan: the build's own hash takes the baked identity plan.
	runGenerated(t, `package probe
fixed table Root {
    n   [..95]int32
    txt string(41)
}
`, `package probe
import ("testing";"bytes";"encoding/binary";"unsafe")

func TestRefuseByName(t *testing.T) {
	one := Root{}
	RootReset(&one)
	one.NCount = 1
	one.N[0] = 0x11223344
	one.TxtLength = 1
	one.Txt[0] = '!'
	need := RootFixedMeasure(1)
	buf := make([]byte, need)
	if n := RootFixedSave([]Root{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	countNeedle := []byte{0x44, 0x33, 0x22, 0x11}
	cAt := bytes.Index(buf, countNeedle)
	if cAt < 0 {
		t.Fatal("identity: count field not found")
	}
	countOff := cAt - 4
	lenNeedle := []byte{1, 0, 0, 0, '!'}
	lAt := bytes.Index(buf, lenNeedle)
	if lAt < 0 {
		t.Fatal("identity: length field not found")
	}
	lenOff := lAt

	plan := make([]TableFixedEntry, 256)
	run := func(name string, off int, word uint32, wantReason string) {
		data := append([]byte(nil), buf...)
		binary.LittleEndian.PutUint32(data[off:], word)
		got := make([]Root, 1)
		pdst := tableFixedOverlay(unsafe.Pointer(&got[0]), unsafe.Sizeof(got[0]))
		for i := range pdst {
			pdst[i] = 0x5A
		}
		var r TableReport
		if n := RootFixedLoad(got, data, plan, &r); n >= 0 || r.Reason != wantReason {
			t.Fatalf("%s: n=%d reason=%q, want %q", name, n, r.Reason, wantReason)
		}
		for i, b := range pdst {
			if b != 0x5A {
				t.Fatalf("%s: the refusal wrote destination byte %d: 0x%02X, want 0x5A (§5.8: not one destination byte)", name, i, b)
			}
		}
	}

	run("identity count over bound", countOff, 200, "count_over_bound")
	run("identity count negative", countOff, 0xFFFFFFFF, "count_over_bound")
	run("identity length over bound", lenOff, 99, "length_over_bound")
	run("identity length negative", lenOff, 0xFFFFFFFF, "length_over_bound")

	// WITH SLACK BEHIND THE BOUND: garbage behind the bound changes nothing, the
	// refusal is checked before any destination byte is written.
	slack := append([]byte(nil), buf...)
	for i := countOff + 4; i < len(slack) && i < countOff+4+95*4; i++ {
		slack[i] = 0xFF
	}
	binary.LittleEndian.PutUint32(slack[countOff:], 200)
	got := make([]Root, 1)
	pdst := tableFixedOverlay(unsafe.Pointer(&got[0]), unsafe.Sizeof(got[0]))
	for i := range pdst {
		pdst[i] = 0x5A
	}
	var r TableReport
	if n := RootFixedLoad(got, slack, plan, &r); n >= 0 || r.Reason != "count_over_bound" {
		t.Fatalf("slack count: n=%d reason=%q, want count_over_bound", n, r.Reason)
	}
	for i, b := range pdst {
		if b != 0x5A {
			t.Fatalf("slack count: wrote destination byte %d: 0x%02X", i, b)
		}
	}
}
`)

	// COMPILED LINEAGE plan: VNEW reads VOLD through its lineage, and a forged
	// count past the WRITER's bound (4) refuses `count_over_bound`.
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_array_bounded_grow")
	writer := readSchema(t, "VOLD_array_bounded_grow")
	table := fixedRootName(t, writer)
	src := fmt.Sprintf(`package probe
import ("bytes";"encoding/binary";"os";"testing";"unsafe")
func TestCountOverBoundCompiled(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("lineage: count field not found")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("lineage: non-unique needle")
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
		t.Fatalf("lineage: n=%%d reason=%%q, want count_over_bound", n, r.Reason)
	}
	for i, b := range pdst {
		if b != 0x5A {
			t.Fatalf("lineage: wrote destination byte %%d: 0x%%02X", i, b)
		}
	}
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	if out, err := runVersionProbe(t, newer, []string{writer}, src); err != nil {
		t.Fatalf("lineage: %v\n%s", err, out)
	}
}
