package gotable

// go/R11 (array-bounds) and go/C14 (live-extents), under the one read.
//
// R11: "the plan carries the WRITER's bounds, per plan; the hostile pass runs
// against the plan's bounds". A fixed counted array's count word is the
// writer's. A reader that grew its own bound must still refuse a count past
// the bound OF THE LAYOUT THAT WROTE THE FILE, and a second lineage peer with
// a different bound is a distractor, not the answer. This file builds the wire
// its own writers would have written from the lineage entries the emitter
// records, so the row needs no C++ corpus: the old layout's bound is 4, the
// distractor's is 6, the reader's own is 8, and the same forged count 5 is
// refused under the 4 and read under the 6.
//
// C14: "the bounds pass walks live only". The read-side bounds walk a counted
// array's live count, never its slack. An endpoint's garbage past the live
// count is unspecified on read: it is neither a refusal nor a counter, while
// the live prefix still reads. The slack is poisoned past the declared element
// range and past the bool's 0/1 so a walk over the declared bound cannot stay
// green.
//
// TestGoFixedArrayBoundsAndLiveExtentsNegativeControl plants each fault in a
// COPY of the emitted artifact (no tracked file is written) and requires the
// matching probe to go red on its own line.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// runFixedProbe compiles the unit with the lineage the test plays the lock
// with, runs testSource in the generated package, and returns the transcript.
// The probe's go.mod replaces this checkout's own module path, so the test
// needs neither a serialize.go sibling nor the C++ corpus: the fixed form's
// generated code imports only tables/goruntime from this tree.
func runFixedProbe(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry, edit func(map[string][]byte), testSource string) ([]byte, error) {
	t.Helper()
	files, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\nreplace github.com/mas-bandwidth/schema/v2 => %q\n", root))
	files["values_test.go"] = []byte(strings.Replace(testSource, "package probe", "package "+u.Package, 1))
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// r11Lineage plays the lock: every older unit's own entry, oldest first.
func r11Lineage(t *testing.T, older []string) map[string][]FixedLineageEntry {
	t.Helper()
	lineage := map[string][]FixedLineageEntry{}
	for _, src := range older {
		o := unitOf(t, src)
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	return lineage
}

// r11Wire builds the fixed file one of the row's writers would have written:
// the header, the layout it locked, then one record whose count is COUNT.
// `lead` and `trail` bracket the array exactly as the schemas declare them.
func r11Wire(t *testing.T, unitSrc string, count uint32, vals []int32) []byte {
	t.Helper()
	u := unitOf(t, unitSrc)
	e, ok := FixedLineageOf(u, "ArrayBoundedGrow")
	if !ok {
		t.Fatal("array_bounded_grow has no lineage entry")
	}
	body := make([]byte, int(e.Record)-8)
	if len(body) != 4+4+4*len(vals)+4 {
		t.Fatalf("the row's body moved: %d bytes for %d values, so the offsets below are stale", len(body), len(vals))
	}
	binary.LittleEndian.PutUint32(body[0:], 0xAAAAAAAA)
	binary.LittleEndian.PutUint32(body[4:], count)
	for i, v := range vals {
		binary.LittleEndian.PutUint32(body[8+4*i:], uint32(v))
	}
	binary.LittleEndian.PutUint32(body[len(body)-4:], 0xBBBBBBBB)

	rec := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint64(rec, e.Wire)
	copy(rec[8:], body)

	out := make([]byte, 16)
	out[0] = 3
	binary.LittleEndian.PutUint64(out[8:], e.Wire)
	out = append(out, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(out[16:], uint32(len(e.Layout)))
	out = append(out, e.Layout...)
	out = append(out, rec...)
	return out
}

const r11Old = `package vold_array_bounded_grow

fixed table ArrayBoundedGrow
{
    lead  uint32 = 1
    vals  [..4]int32
    trail uint32 = 2
}
`

const r11Mid = `package vmid_array_bounded_grow

fixed table ArrayBoundedGrow
{
    lead  uint32 = 1
    vals  [..6]int32
    trail uint32 = 2
}
`

const r11New = `package vnew_array_bounded_grow

fixed table ArrayBoundedGrow
{
    lead  uint32 = 1
    vals  [..8]int32
    trail uint32 = 2
}
`

func TestGoFixedArrayBoundsAndLiveExtents(t *testing.T) {
	t.Run("writer-bounds-per-plan", func(t *testing.T) {
		newer := unitOf(t, r11New)
		lineage := r11Lineage(t, []string{r11Old, r11Mid})

		t.Run("control-the-writers-own-count-reads", func(t *testing.T) {
			data := r11Wire(t, r11Old, 4, []int32{11, 22, 33, 44})
			src := fmt.Sprintf(`package probe

import "testing"

func TestProbe(t *testing.T) {
	data := %#v
	back := make([]ArrayBoundedGrow, 4)
	var r TableReport
	n := ArrayBoundedGrowFixedLoad(back, data, make([]TableFixedEntry, ArrayBoundedGrowFixedPlanMax), &r)
	if n != 1 {
		t.Fatalf("the writer's own count must read: n=%%d %%+v", n, r)
	}
	if r != (TableReport{}) {
		t.Fatalf("a lawful read moved a counter or named a refusal: %%+v", r)
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB || back[0].ValsCount != 4 {
		t.Fatalf("the writer's record did not land: %%+v", back[0])
	}
	for i, want := range []int32{11, 22, 33, 44} {
		if back[0].Vals[i] != want {
			t.Fatalf("slot %%d: %%d, want %%d", i, back[0].Vals[i], want)
		}
	}
}
`, data)
			if out, err := runFixedProbe(t, newer, lineage, nil, src); err != nil {
				t.Fatalf("the control read: %v\n%s", err, out)
			}
		})

		t.Run("a-count-past-the-writing-endpoint-refuses", func(t *testing.T) {
			// 5 is past the OLD writer's bound of 4, but inside the
			// distractor's 6 and the reader's own 8. Only a plan carrying the
			// WRITING endpoint's bound refuses it.
			data := r11Wire(t, r11Old, 5, []int32{11, 22, 33, 44})
			src := fmt.Sprintf(`package probe

import "testing"

func TestProbe(t *testing.T) {
	data := %#v
	back := make([]ArrayBoundedGrow, 4)
	back[0].Lead = 0x11111111
	back[0].ValsCount = 3
	back[0].Trail = 0x22222222
	for i := range back[0].Vals {
		back[0].Vals[i] = 0x33333333
	}
	before := back[0]
	var r TableReport
	n := ArrayBoundedGrowFixedLoad(back, data, make([]TableFixedEntry, ArrayBoundedGrowFixedPlanMax), &r)
	if n != -1 || r.Verdict != TableOpenRefused || r.Reason != "count_over_bound" {
		t.Fatalf("a count past the WRITING plan's bound 4 must refuse count_over_bound, not the reader's 8 or the distractor's 6: n=%%d %%+v", n, r)
	}
	if back[0] != before {
		t.Fatalf("a named refusal writes no destination byte: before=%%+v after=%%+v", before, back[0])
	}
	if r.Unknown != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0 {
		t.Fatalf("REFUSE is total: it moves no counter: %%+v", r)
	}
	if r.Refusal.Error() == "wire_damaged" || r.Refusal.Error() == "malformed" {
		t.Fatalf("a refusal by name is not damage: %%+v", r)
	}
}
`, data)
			if out, err := runFixedProbe(t, newer, lineage, nil, src); err != nil {
				t.Fatalf("the forged read under the writing plan: %v\n%s", err, out)
			}
		})

		t.Run("the-same-count-reads-under-the-other-plan", func(t *testing.T) {
			// The SAME forged 5 from the 6-bounded peer is lawful there: the
			// plan is per lineage entry, not one global bound for the reader.
			data := r11Wire(t, r11Mid, 5, []int32{11, 22, 33, 44, 55, 0})
			src := fmt.Sprintf(`package probe

import "testing"

func TestProbe(t *testing.T) {
	data := %#v
	back := make([]ArrayBoundedGrow, 4)
	var r TableReport
	n := ArrayBoundedGrowFixedLoad(back, data, make([]TableFixedEntry, ArrayBoundedGrowFixedPlanMax), &r)
	if n != 1 || r != (TableReport{}) {
		t.Fatalf("the 6-bounded writer's count 5 is lawful under ITS plan: n=%%d %%+v", n, r)
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB || back[0].ValsCount != 5 {
		t.Fatalf("the peer's record did not land: %%+v", back[0])
	}
	for i, want := range []int32{11, 22, 33, 44, 55} {
		if back[0].Vals[i] != want {
			t.Fatalf("slot %%d: %%d, want %%d", i, back[0].Vals[i], want)
		}
	}
}
`, data)
			if out, err := runFixedProbe(t, newer, lineage, nil, src); err != nil {
				t.Fatalf("the lawful read under the other plan: %v\n%s", err, out)
			}
		})
	})

	t.Run("live-extents", func(t *testing.T) {
		schema := `package probe
fixed table Live {
    lead  uint32 = 1
    vals  [..4]int32 | min = 0, max = 100
    flags [..3]bool
    text  string(8)
    tail  uint32 = 2
}
`
		src := `package probe
import ("encoding/binary"; "testing")

func TestProbe(t *testing.T) {
	one := Live{}
	LiveReset(&one)
	one.Lead = 0xAAAAAAAA
	one.ValsCount = 2
	one.Vals[0] = 11
	one.Vals[1] = 22
	one.FlagsCount = 1
	one.Flags[0] = true
	copy(one.Text[:], "hi")
	one.TextLength = 2
	one.Tail = 0xBBBBBBBB
	buf := make([]byte, LiveFixedMeasure(1))
	if n := LiveFixedSave([]Live{one}, buf); n != int64(len(buf)) {
		t.Fatalf("save: n=%d want %d", n, len(buf))
	}
	if LiveFixedBodyBytes != 47 {
		t.Fatalf("the record body moved (%d bytes), so the offsets below are stale", LiveFixedBodyBytes)
	}
	body := buf[len(buf)-LiveFixedRecordBytes+8:]
	// the declared wire order: lead (4), vals count (4) + four int32 (16),
	// flags count (4) + three bools (3), text length (4) + eight bytes (8),
	// tail (4).
	const valsSlackAt = 4 + 4 + 2*4
	const flagsSlackAt = 4 + 4 + 4*4 + 4 + 1
	const textSlackAt = 4 + 4 + 4*4 + 4 + 3 + 4 + 2
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint32(body[valsSlackAt+4*i:], 0x7FFFFFFF)
	}
	for i := 0; i < 2; i++ {
		body[flagsSlackAt+i] = 0x02
	}
	for i := 0; i < 6; i++ {
		body[textSlackAt+i] = 0xAA
	}
	// NOT VACUOUS: the declared slack really carries garbage no clean record has.
	if binary.LittleEndian.Uint32(body[valsSlackAt:]) == 0 || body[flagsSlackAt] == 0 || body[textSlackAt] == 0 {
		t.Fatal("the wire slack was not poisoned, so nothing below is tested")
	}

	got := make([]Live, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := LiveFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("an endpoint's garbage in the slack refused the record: n=%d %+v", n, r)
	}
	if r != (TableReport{}) {
		t.Fatalf("an endpoint's garbage in the slack moved a counter or named a refusal: %+v", r)
	}
	if got[0].Lead != 0xAAAAAAAA || got[0].Tail != 0xBBBBBBBB {
		t.Fatalf("the bracketing fields did not land: %+v", got[0])
	}
	if got[0].ValsCount != 2 || got[0].Vals[0] != 11 || got[0].Vals[1] != 22 {
		t.Fatalf("the live array prefix did not read: %+v", got[0])
	}
	if got[0].FlagsCount != 1 || !got[0].Flags[0] {
		t.Fatalf("the live bool prefix did not read: %+v", got[0])
	}
	if got[0].TextLength != 2 || string(got[0].Text[:2]) != "hi" {
		t.Fatalf("the used text did not read: %+v", got[0])
	}
}
`
		if out, err := runFixedProbe(t, unitFrom(t, schema), nil, nil, src); err != nil {
			t.Fatalf("the live-extents read: %v\n%s", err, out)
		}
	})
}

// TestGoFixedArrayBoundsAndLiveExtentsNegativeControl plants each fault in a
// COPY of the real emitted artifact and requires the matching probe to go red
// on that fault, so neither half can pass by not running.
func TestGoFixedArrayBoundsAndLiveExtentsNegativeControl(t *testing.T) {
	t.Run("writer-bounds-per-plan", func(t *testing.T) {
		newer := unitOf(t, r11New)
		lineage := r11Lineage(t, []string{r11Old, r11Mid})
		data := r11Wire(t, r11Old, 5, []int32{11, 22, 33, 44})
		src := fmt.Sprintf(`package probe

import "testing"

func TestProbe(t *testing.T) {
	data := %#v
	back := make([]ArrayBoundedGrow, 4)
	var r TableReport
	n := ArrayBoundedGrowFixedLoad(back, data, make([]TableFixedEntry, ArrayBoundedGrowFixedPlanMax), &r)
	if n != -1 || r.Verdict != TableOpenRefused || r.Reason != "count_over_bound" {
		t.Fatalf("a count past the WRITING plan's bound 4 must refuse count_over_bound: n=%%d %%+v", n, r)
	}
}
`, data)
		out, err := runFixedProbe(t, newer, lineage, func(files map[string][]byte) {
			sabotageGenerated(t, files, "writer-bound count", [2]string{
				"Size: n, Aux: tel.Size",
				"Size: myN, Aux: tel.Size",
			})
		}, src)
		requireRed(t, "the plan's writer bound", out, err, "count_over_bound")
	})

	t.Run("live-extents", func(t *testing.T) {
		schema := `package probe
fixed table Live {
    lead  uint32 = 1
    vals  [..4]int32 | min = 0, max = 100
    flags [..3]bool
    text  string(8)
    tail  uint32 = 2
}
`
		src := `package probe
import ("encoding/binary"; "testing")

func TestProbe(t *testing.T) {
	one := Live{}
	LiveReset(&one)
	one.Lead = 0xAAAAAAAA
	one.ValsCount = 2
	one.Vals[0] = 11
	one.Vals[1] = 22
	one.FlagsCount = 1
	one.Flags[0] = true
	copy(one.Text[:], "hi")
	one.TextLength = 2
	one.Tail = 0xBBBBBBBB
	buf := make([]byte, LiveFixedMeasure(1))
	if n := LiveFixedSave([]Live{one}, buf); n != int64(len(buf)) {
		t.Fatalf("save: n=%d want %d", n, len(buf))
	}
	body := buf[len(buf)-LiveFixedRecordBytes+8:]
	const valsSlackAt = 4 + 4 + 2*4
	const flagsSlackAt = 4 + 4 + 4*4 + 4 + 1
	const textSlackAt = 4 + 4 + 4*4 + 4 + 3 + 4 + 2
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint32(body[valsSlackAt+4*i:], 0x7FFFFFFF)
	}
	for i := 0; i < 2; i++ {
		body[flagsSlackAt+i] = 0x02
	}
	for i := 0; i < 6; i++ {
		body[textSlackAt+i] = 0xAA
	}

	got := make([]Live, 1)
	var r TableReport
	if n := LiveFixedLoad(got, buf, make([]TableFixedEntry, 256), &r); n != 1 {
		t.Fatalf("an endpoint's garbage in the slack refused the record: n=%d %+v", n, r)
	}
	if r != (TableReport{}) {
		t.Fatalf("an endpoint's garbage in the slack moved a counter or named a refusal: %+v", r)
	}
}
`
		out, err := runFixedProbe(t, unitFrom(t, schema), nil, func(files map[string][]byte) {
			sabotageGenerated(t, files, "live array extents", [2]string{
				"for i := range int64(value.ValsCount)",
				"for i := range int64(4)",
			})
		}, src)
		requireRed(t, "the live array extent", out, err, "in the slack refused")
	})
}
