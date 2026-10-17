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
	assertRefuse := func(name string, data []byte, wantReason string) {
		got := make([]Root, 1)
		pdst := tableFixedOverlay(unsafe.Pointer(&got[0]), unsafe.Sizeof(got[0]))
		for i := range pdst {
			pdst[i] = 0x5A
		}
		var r TableReport
		if n := RootFixedLoad(got, data, plan, &r); n >= 0 || r.Reason != wantReason {
			t.Fatalf("%s: n=%d reason=%q, want %q", name, n, r.Reason, wantReason)
		}
		if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0 || r.Malformed {
			t.Fatalf("%s: refusal moved a counter or marked malformed: %+v", name, r)
		}
		for i, b := range pdst {
			if b != 0x5A {
				t.Fatalf("%s: the refusal wrote destination byte %d: 0x%02X, want 0x5A (§5.8: not one destination byte)", name, i, b)
			}
		}
	}

	// WITHOUT SLACK behind the bound: a plain forged word.
	data := append([]byte(nil), buf...)
	binary.LittleEndian.PutUint32(data[countOff:], 200)
	assertRefuse("identity count over bound", data, "count_over_bound")
	data = append([]byte(nil), buf...)
	binary.LittleEndian.PutUint32(data[countOff:], 0xFFFFFFFF)
	assertRefuse("identity count negative", data, "count_over_bound")
	data = append([]byte(nil), buf...)
	binary.LittleEndian.PutUint32(data[lenOff:], 99)
	assertRefuse("identity length over bound", data, "length_over_bound")
	data = append([]byte(nil), buf...)
	binary.LittleEndian.PutUint32(data[lenOff:], 0xFFFFFFFF)
	assertRefuse("identity length negative", data, "length_over_bound")

	// WITH SLACK BEHIND THE BOUND: garbage behind the bound changes nothing, the
	// refusal is checked before any destination byte is written.
	slack := append([]byte(nil), buf...)
	for i := countOff + 4; i < len(slack) && i < countOff+4+95*4; i++ {
		slack[i] = 0xFF
	}
	binary.LittleEndian.PutUint32(slack[countOff:], 200)
	assertRefuse("identity count over bound with slack", slack, "count_over_bound")

	slack = append([]byte(nil), buf...)
	for i := lenOff + 4; i < len(slack) && i < lenOff+4+41; i++ {
		slack[i] = 0xFF
	}
	binary.LittleEndian.PutUint32(slack[lenOff:], 99)
	assertRefuse("identity length over bound with slack", slack, "length_over_bound")
}
`)

	// COMPILED LINEAGE, count: VNEW reads VOLD array_bounded_grow ([..4] ->
	// [..8]) through its lineage. A forged count of 200 past the WRITER's bound
	// (4) refuses `count_over_bound`, with and without slack behind the bound.
	corpus := fixedCorpus(t)
	newer := readSchema(t, "VNEW_array_bounded_grow")
	writer := readSchema(t, "VOLD_array_bounded_grow")
	table := fixedRootName(t, writer)
	countSrc := fmt.Sprintf(`package probe
import ("bytes";"encoding/binary";"os";"testing";"unsafe")
func TestCountOverBoundCompiled(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("lineage count: field not found")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("lineage count: non-unique needle")
	}
	plan := make([]TableFixedEntry, 4096)
	assertRefuse := func(name string, d []byte, wantReason string) {
		back := make([]%[2]s, 8)
		pdst := tableFixedOverlay(unsafe.Pointer(&back[0]), unsafe.Sizeof(back[0]))
		for i := range pdst {
			pdst[i] = 0x5A
		}
		var r TableReport
		n := %[2]sFixedLoad(back, d, plan, &r)
		if n >= 0 || r.Reason != wantReason {
			t.Fatalf("%%s: n=%%d reason=%%q, want %%q", name, n, r.Reason, wantReason)
		}
		if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0 || r.Malformed {
			t.Fatalf("%%s: refusal moved a counter or marked malformed: %%+v", name, r)
		}
		for i, b := range pdst {
			if b != 0x5A {
				t.Fatalf("%%s: wrote destination byte %%d: 0x%%02X", name, i, b)
			}
		}
	}
	d := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(d[at+4:], 200)
	assertRefuse("lineage count over bound", d, "count_over_bound")
	d = append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(d[at+4:], 200)
	for i := at + 8; i < len(d) && i < at+8+4*4; i++ {
		d[i] = 0xFF
	}
	assertRefuse("lineage count over bound with slack", d, "count_over_bound")
}
`, filepath.Join(corpus, "old_array_bounded_grow.bin"), table)
	if out, err := runVersionProbe(t, newer, []string{writer}, countSrc); err != nil {
		t.Fatalf("lineage count: %v\n%s", err, out)
	}

	// COMPILED LINEAGE, length: VNEW reads VOLD string_grow (string(8) ->
	// string(16)) through its lineage. A forged length of 99 past the WRITER's
	// bound (8) refuses `length_over_bound`, with and without slack behind the
	// bound.
	newerS := readSchema(t, "VNEW_string_grow")
	writerS := readSchema(t, "VOLD_string_grow")
	tableS := fixedRootName(t, writerS)
	lenSrc := fmt.Sprintf(`package probe
import ("bytes";"encoding/binary";"os";"testing";"unsafe")
func TestLengthOverBoundCompiled(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x08, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatal("lineage length: field not found")
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatal("lineage length: non-unique needle")
	}
	plan := make([]TableFixedEntry, 4096)
	assertRefuse := func(name string, d []byte, wantReason string) {
		back := make([]%[2]s, 8)
		pdst := tableFixedOverlay(unsafe.Pointer(&back[0]), unsafe.Sizeof(back[0]))
		for i := range pdst {
			pdst[i] = 0x5A
		}
		var r TableReport
		n := %[2]sFixedLoad(back, d, plan, &r)
		if n >= 0 || r.Reason != wantReason {
			t.Fatalf("%%s: n=%%d reason=%%q, want %%q", name, n, r.Reason, wantReason)
		}
		if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0 || r.Malformed {
			t.Fatalf("%%s: refusal moved a counter or marked malformed: %%+v", name, r)
		}
		for i, b := range pdst {
			if b != 0x5A {
				t.Fatalf("%%s: wrote destination byte %%d: 0x%%02X", name, i, b)
			}
		}
	}
	d := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(d[at+4:], 99)
	assertRefuse("lineage length over bound", d, "length_over_bound")
	d = append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(d[at+4:], 99)
	for i := at + 8; i < len(d) && i < at+8+8; i++ {
		d[i] = 0xFF
	}
	assertRefuse("lineage length over bound with slack", d, "length_over_bound")
}
`, filepath.Join(corpus, "old_string_grow.bin"), tableS)
	if out, err := runVersionProbe(t, newerS, []string{writerS}, lenSrc); err != nil {
		t.Fatalf("lineage length: %v\n%s", err, out)
	}
}
