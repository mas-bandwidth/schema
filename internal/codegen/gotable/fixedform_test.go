package gotable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedFormEmitsSurface(t *testing.T) {
	files := generate(t, `package probe
fixed table Point {
    x int32
    y int32
}
`)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	for _, want := range []string{
		"const TableFixedForm uint8 = 3",
		"const TableFixedHeaderBytes = 16",
		"const TableFixedHashAt = 8",
		"func PointFixedSave(",
		"func PointFixedLoad(",
		"func PointFixedMeasure(",
		"layout_malformed",
		"no_layout",
		"plan_too_large",
		"newer_form",
		"previous_form",
		"tableFixedParseLayout",
		"tableFixedHoles",
		"PointFixedLayout",
		"PointFixedPlan",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fixed form is missing %q", want)
		}
	}
	if strings.Contains(body, "block_malformed") {
		t.Error("the layout is still named a block")
	}
	// DECLARED fixed, because #823's keyword is the SELECTION POINT: the form
	// follows the declaration and nothing is derived in either direction (§2.2,
	// [ir.TableFixedRoots]), so the surface above exists for a `fixed table`
	// and for nothing else. The keyword selects the FORM a writer emits; it
	// does not move the form-1 refusal out of FixedLoad and into <T>Load, which
	// stays the variable form's entry point and still reads form 1 — the pair is
	// held next door by TestFixedFormForm1RefusalLivesInFixedLoad, and the
	// variable half by TestForm1OfVariableTableStillLoads.
	if funcSource(body, "func PointLoad(") == "" {
		t.Fatal("PointLoad was not emitted")
	}
	fixedLoad := funcSource(body, "func PointFixedLoad(")
	if fixedLoad == "" {
		t.Fatal("PointFixedLoad was not emitted")
	}
	if !strings.Contains(fixedLoad, "tableFixedHoles") {
		t.Error("compiled FixedLoad does not prefill the plan's holes")
	}
	// SELECT BY HASH, never parse a stranger (docs/FIXED-FORM-ALGORITHM.md §5.3):
	// the lineage and the floor are static data, and the two answers an
	// unmatched hash owes are distinct.
	for _, want := range []string{"tableFixedSelect(PointFixedKnown", "layout_newer", "PointFixedFloor", "layout_unsupported"} {
		if !strings.Contains(fixedLoad, want) {
			t.Errorf("FixedLoad does not select by hash: missing %q", want)
		}
	}
	if strings.Contains(fixedLoad, "if identity {") {
		t.Error("identity flag still forks the record loop")
	}
	if strings.Contains(fixedLoad, "PointReset(&values[k])") {
		t.Error("identity still Reset's each record; prefill is the hole list")
	}
	if !strings.Contains(fixedLoad, "PointReset(&def)") {
		t.Error("holes have no default image")
	}
	if !strings.Contains(fixedLoad, "tableFixedRun(") {
		t.Error("FixedLoad does not walk the winning plan")
	}
	if strings.Contains(body, "PointFixedClamp") {
		t.Error("a type that declares nothing bounded must not emit a clamp pass")
	}
}

// TestFixedFormForm1RefusalLivesInFixedLoad is the other half of the surface
// above, and it is #823's `fixed table` keyword that makes it worth pinning:
// the keyword decides what a WRITER emits (form 3, always) and which ENTRY
// POINT names a form-1 file `previous_form` — <T>FixedLoad — and it does NOT
// turn <T>Load, the variable form's own entry point, into a refusal. The
// shared corpus pins `v1_cfg_as_v2` at `read` over a form-1 file of a DECLARED
// fixed root and the C++ reference reads it there, so a Go <T>Load that
// refused would be the one leg disagreeing with the reference
// (docs/SPEC-TABLES.md §3.4, §15). The schema says the keyword and the fixture
// walks the real parser: there is no second way to say the class here.
func TestFixedFormForm1RefusalLivesInFixedLoad(t *testing.T) {
	files := generate(t, `package probe
fixed table Point {
    x int32
    y int32
}
`)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	load := funcSource(body, "func PointLoad(")
	if load == "" {
		t.Fatal("PointLoad was not emitted")
	}
	if !strings.Contains(load, "PointLoadBody") {
		t.Error("PointLoad of a DECLARED fixed table must still walk form 1")
	}
	if strings.Contains(load, "previous_form") {
		t.Error("PointLoad, the form-1 entry point, refuses form 1")
	}
	fixedLoad := funcSource(body, "func PointFixedLoad(")
	if !strings.Contains(fixedLoad, "previous_form") || !strings.Contains(fixedLoad, "tableFixedRefuse") {
		t.Error("PointFixedLoad does not name a form-1 file previous_form")
	}
}

func TestFixedFormRoundTrip(t *testing.T) {
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("testing")

func TestRoundTrip(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointFixedMeasure(1)
	buf := make([]byte, need)
	if n := PointFixedSave([]Point{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	// THE FILE: form byte, seven reserved zeros, layout hash at 8, body at 16,
	// then the layout behind its u32 length, then the records.
	if buf[0] != 3 {
		t.Fatalf("form byte %d", buf[0])
	}
	if tableFixedGet32(buf[TableFixedHeaderBytes:]) != uint32(len(PointFixedLayout)) {
		t.Fatalf("layout length %d want %d", tableFixedGet32(buf[TableFixedHeaderBytes:]), len(PointFixedLayout))
	}
	layoutAt := TableFixedHeaderBytes + 4
	if string(buf[layoutAt:layoutAt+len(PointFixedLayout)]) != string(PointFixedLayout) {
		t.Fatal("the layout is not this build's layout")
	}
	if tableFixedGet64(buf[layoutAt+len(PointFixedLayout):]) != PointFixedHash {
		t.Fatal("the first record does not open with the layout hash")
	}
	got := make([]Point, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 64)
	if n := PointFixedLoad(got, buf, plan, &r); n != 1 || r != (TableReport{}) || got[0] != one {
		t.Fatalf("got %+v report %+v n=%d", got[0], r, n)
	}
	// EVERY ASSIGNED FORM BYTE HAS ITS OWN NAME (docs/SPEC-TABLES.md §3, THE
	// FIRST BYTE): 1 the variable form, 2 the message form, 3 this one. A byte
	// the registry has not assigned — 0 among them, there being no form 0 for
	// a file to be a previous form of — is newer_form, "a form byte this
	// reader does not carry".
	for form, want := range map[byte]string{
		0:    "newer_form",
		1:    "previous_form",
		2:    "message_form_as_file",
		4:    "newer_form",
		0xFF: "newer_form",
	} {
		wrong := append([]byte(nil), buf...)
		wrong[0] = form
		r = TableReport{}
		if n := PointFixedLoad(got, wrong, plan, &r); n >= 0 || r.Reason != want || r.Verdict != TableOpenRefused || r.Malformed {
			t.Fatalf("FixedLoad form %d: want %s, got %d %+v", form, want, n, r)
		}
	}
	// THE LAYOUT'S OWN RULES REFUSE UNDER THEIR OWN NAMES and before the header
	// is doubted (docs/FIXED-FORM-ALGORITHM.md §1.1, §2.1 step 5): breaking the
	// entry count is rule 1, so this is layout_count_mismatch and not the one
	// word the residue keeps.
	broken := append([]byte(nil), buf...)
	broken[TableFixedHeaderBytes+4] ^= 0xFF
	r = TableReport{}
	// Hash chooses first (bill §12.4): a known hash whose layout bytes differ
	// is layout_malformed. The seven §1.1 names are for a layout that is not
	// a layout; they do not fire against a header this build has locked.
	if n := PointFixedLoad(got, broken, plan, &r); n >= 0 || r.Reason != "layout_malformed" {
		t.Fatalf("known hash, different layout bytes: %d %+v", n, r)
	}
	lying := append([]byte(nil), buf...)
	lying[TableFixedHeaderBytes+4+len(PointFixedLayout)] ^= 0xFF
	r = TableReport{}
	if n := PointFixedLoad(got, lying, plan, &r); n >= 0 || r.Reason != "no_layout" {
		t.Fatalf("a record whose hash names no layout: %d %+v", n, r)
	}
}
`)
}

// P2: WRITE, READ, WRITE — THE BYTES ARE IDENTICAL (docs/FIXED-FORM-ALGORITHM.md
// §7 item 1, "Read a file and save it back; the bytes must be identical - a
// byte a port encodes differently is a byte that does not come back"). The
// corpus and the tests beside it compare the leg's WRITE against bytes the C++
// REFERENCE wrote; this one closes the loop on THIS leg's own writer: save
// values, load them, save the loaded values again, and the two buffers must
// match byte for byte — slack, template zeros, text spans and all. The sharp
// values make the second write non-trivial (the exact set §7 item 1's fixtures
// are built to catch): a PARTLY-USED text field, a PARTLY-FILLED counted array,
// an ABSENT optional whose payload is stained in STORAGE, and a union on its
// NARROWER arm. A buffer the caller pre-zeroed would hide a writer that did not
// lay the whole template, so the second write lands on 0x5A — the doc's own
// poison, the "on a POISONED destination it fails louder" of the slack rule.
// If a byte differs, the failure names which field's span it falls in.
func TestFixedFormWriteReadWriteIsByteIdentical(t *testing.T) {
	runGenerated(t, `package probe
union Pick
{
    a int32
    b int64
}
fixed table Root {
    note  string(12)
    marks [..4]int32
    opt   ?int64
    u     Pick
}
`, `package probe
import (
	"bytes"
	"fmt"
	"testing"
)

func TestWriteReadWriteIsByteIdentical(t *testing.T) {
	one := Root{}
	RootReset(&one)
	one.NoteLength = 5
	copy(one.Note[:], "hello")
	one.MarksCount = 2
	one.Marks[0] = 11
	one.Marks[1] = -22
	one.OptPresent = false
	one.Opt = 0x5A5A5A5A5A5A5A5A
	one.U.Type = PickTypeA
	one.U.A = 4242

	need := RootFixedMeasure(1)
	buf := make([]byte, need)
	if n := RootFixedSave([]Root{one}, buf); n != need {
		t.Fatalf("first save %d", n)
	}
	back := make([]Root, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := RootFixedLoad(back, buf, plan, &r); n != 1 || r != (TableReport{}) {
		t.Fatalf("load %d %+v", n, r)
	}
	again := make([]byte, need)
	for i := range again {
		again[i] = 0x5A
	}
	if n := RootFixedSave([]Root{back[0]}, again); n != need {
		t.Fatalf("second save %d", n)
	}
	if !bytes.Equal(buf, again) {
		t.Fatalf("WRITE-READ-WRITE IS NOT BYTE-IDENTICAL (docs/FIXED-FORM-ALGORITHM.md §7 item 1: read a file and save it back; the bytes must be identical - a byte a port encodes differently is a byte that does not come back): first write %x, second write %x, first differing byte: %s", buf, again, fixedDiffSpan(buf, again))
	}
}

// fixedDiffSpan names the file region a byte difference falls in, so a
// violation reports the field's span rather than an anonymous offset.
func fixedDiffSpan(buf, again []byte) string {
	off := 0
	for off < len(buf) && off < len(again) && buf[off] == again[off] {
		off++
	}
	if off >= len(buf) || off >= len(again) {
		return fmt.Sprintf("length: %d bytes match, then %d vs %d", off, len(buf), len(again))
	}
	switch {
	case off < TableFixedHeaderBytes:
		return fmt.Sprintf("the header byte %d", off)
	case off < TableFixedHeaderBytes+4:
		return fmt.Sprintf("the layout length byte %d", off)
	case off < TableFixedHeaderBytes+4+len(RootFixedLayout):
		return fmt.Sprintf("the layout bytes, offset %d", off)
	}
	recordStart := TableFixedHeaderBytes + 4 + len(RootFixedLayout)
	inRec := off - recordStart
	rec := inRec / RootFixedRecordBytes
	inRec %= RootFixedRecordBytes
	if inRec < 8 {
		return fmt.Sprintf("record %d's layout hash", rec)
	}
	bodyOff := inRec - 8
	return fmt.Sprintf("record %d, the %s", rec, fixedBodySpan(bodyOff))
}

// fixedBodySpan walks the layout's top-level children — the record's fields in
// declared order — and names the field whose body span holds the offset.
func fixedBodySpan(off int) string {
	view, _ := tableFixedParseLayout(RootFixedLayout)
	root := tableFixedEntryAt(view, 0)
	idx := int32(1)
	at := 0
	names := []string{"note", "marks", "opt", "u"}
	for i := 0; i < int(root.Children); i++ {
		e := tableFixedEntryAt(view, idx)
		span := int(e.Size)
		if off >= at && off < at+span {
			name := fmt.Sprintf("field %d", i)
			if i < len(names) {
				name = names[i]
			}
			return fmt.Sprintf("%s field (body %d..%d), file offset %d", name, at, at+span, off+TableFixedHeaderBytes+4+len(RootFixedLayout)+8)
		}
		at += span
		idx += tableFixedSubtree(view, idx)
	}
	return fmt.Sprintf("body offset %d", off)
}
`)
}

// RETIRED BY §5.6, AND OWED AGAIN ON THE LINEAGE HARNESS. This test and the two
// below read a file written under ANOTHER schema's layout WITHOUT that layout
// being in the reader's lineage, and they assert the run-time walk of a
// stranger's layout — the seven §1.1 names, and a forward read. §5.6 retires
// exactly that: "Reading a layout the lock has never seen. The run-time walk of
// a stranger's layout ... the recompute of the header's hash". Under §5 every
// one of these files comes back layout_newer, which is the new law and not a
// regression. What is OWED is the same coverage on the lineage harness
// (fixedversioning_test.go's runVersionProbe): the plan path with the peer's
// entry handed to the reader as its lock, and §1.1's seven rules moved to the
// LOCK's validation of what it records.
func TestFixedFormPlanPath(t *testing.T) {
	t.Skip("retired by docs/FIXED-FORM-ALGORITHM.md §5.6; owed again on the lineage harness")

	fx1, err := os.ReadFile("../../../test/tables/FX1.schema")
	if err != nil {
		t.Fatal(err)
	}
	fx2, err := os.ReadFile("../../../test/tables/FX2.schema")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	writeUnit(t, filepath.Join(dir, "fx1"), "tblfx1", string(fx1), runtime)
	writeUnit(t, filepath.Join(dir, "fx2"), "tblfx2", string(fx2), runtime)
	testDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module fixedformtest\n\ngo 1.26\n\nrequire (\n\ttblfx1 v0.0.0\n\ttblfx2 v0.0.0\n)\nreplace tblfx1 => ../fx1\nreplace tblfx2 => ../fx2\n"
	if err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `package fixedformtest

import (
	"testing"

	"tblfx1"
	"tblfx2"
)

func TestPlanPath(t *testing.T) {
	one := tblfx1.FxRoot{}
	tblfx1.FxRootReset(&one)
	one.Keep = 4242
	one.Narrow = 40000
	one.Renamed = 321
	one.Gone = 654
	one.Nested.A = 111
	one.Nested.B = 222
	w1 := make([]byte, tblfx1.FxRootFixedMeasure(1))
	if n := tblfx1.FxRootFixedSave([]tblfx1.FxRoot{one}, w1); n != int64(len(w1)) {
		t.Fatalf("FX1 save %d", n)
	}

	{
		back := make([]tblfx1.FxRoot, 1)
		var r tblfx1.TableReport
		plan := make([]tblfx1.TableFixedEntry, 1024)
		n := tblfx1.FxRootFixedLoad(back, w1, plan, &r)
		if n != 1 || back[0].Keep != 4242 || back[0].Narrow != 40000 || back[0].Renamed != 321 || back[0].Gone != 654 {
			t.Fatalf("same schema: %+v n=%d r=%+v", back[0], n, r)
		}
		if back[0].Nested.A != 111 || back[0].Nested.B != 222 || r != (tblfx1.TableReport{}) {
			t.Fatalf("same schema nesting/report: %+v %+v", back[0].Nested, r)
		}
	}

	{
		poison := tblfx1.FxRoot{}
		tblfx1.FxRootReset(&poison)
		poison.Keep = 1
		poison.Renamed = 5000
		poison.Gone = -7
		poison.Nested.A = 111
		poison.Nested.B = 222
		wb := make([]byte, tblfx1.FxRootFixedMeasure(1))
		if n := tblfx1.FxRootFixedSave([]tblfx1.FxRoot{poison}, wb); n != int64(len(wb)) {
			t.Fatalf("bounds save %d", n)
		}
		back := make([]tblfx1.FxRoot, 1)
		var r tblfx1.TableReport
		plan := make([]tblfx1.TableFixedEntry, 1024)
		if n := tblfx1.FxRootFixedLoad(back, wb, plan, &r); n != 1 {
			t.Fatalf("RANGE identity n=%d %+v", n, r)
		}
		if back[0].Renamed != 1000 || back[0].Gone != 0 || r.Clamped != 2 {
			t.Fatalf("RANGE identity: renamed=%d gone=%d clamped=%d", back[0].Renamed, back[0].Gone, r.Clamped)
		}
		back2 := make([]tblfx2.FxRoot, 1)
		var r2 tblfx2.TableReport
		plan2 := make([]tblfx2.TableFixedEntry, 2048)
		if n := tblfx2.FxRootFixedLoad(back2, wb, plan2, &r2); n != 1 {
			t.Fatalf("RANGE compiled n=%d %+v", n, r2)
		}
		if back2[0].RenamedTo != 1000 || r2.Clamped != 1 {
			t.Fatalf("RANGE compiled: renamed_to=%d clamped=%d (gone is a field FX2 cannot name)", back2[0].RenamedTo, r2.Clamped)
		}
	}

	{
		back := make([]tblfx2.FxRoot, 1)
		back[0].Added = 99
		back[0].Keep = 1
		var r tblfx2.TableReport
		plan := make([]tblfx2.TableFixedEntry, 1024)
		n := tblfx2.FxRootFixedLoad(back, w1, plan, &r)
		if n != 1 {
			t.Fatalf("older writer n=%d r=%+v", n, r)
		}
		if back[0].Keep != 4242 {
			t.Fatalf("unmoved %d", back[0].Keep)
		}
		if back[0].Narrow != 40000 || r.Widened != 1 {
			t.Fatalf("widened %d report %+v", back[0].Narrow, r)
		}
		if back[0].RenamedTo != 321 {
			t.Fatalf("renamed %d", back[0].RenamedTo)
		}
		if back[0].Added != 11 {
			t.Fatalf("missing default %d", back[0].Added)
		}
		if back[0].Nested.A != 111 || back[0].Nested.B != 222 {
			t.Fatalf("nesting %+v", back[0].Nested)
		}
		if r.Unknown != 1 {
			t.Fatalf("unknown %d", r.Unknown)
		}
	}

	two := tblfx2.FxRoot{}
	tblfx2.FxRootReset(&two)
	two.Keep = 5150
	two.Narrow = 70000
	two.RenamedTo = 808
	two.Added = 909
	two.Nested.A = 33
	two.Nested.B = 44
	two.Extra.X = 55
	two.Extra.Y = 66
	w2 := make([]byte, tblfx2.FxRootFixedMeasure(1))
	if n := tblfx2.FxRootFixedSave([]tblfx2.FxRoot{two}, w2); n != int64(len(w2)) {
		t.Fatalf("FX2 save %d", n)
	}
	{
		back := make([]tblfx1.FxRoot, 1)
		back[0].Gone = 99
		back[0].Narrow = 99
		var r tblfx1.TableReport
		plan := make([]tblfx1.TableFixedEntry, 1024)
		n := tblfx1.FxRootFixedLoad(back, w2, plan, &r)
		if n != 1 {
			t.Fatalf("newer writer n=%d r=%+v", n, r)
		}
		if back[0].Keep != 5150 || back[0].Renamed != 808 {
			t.Fatalf("newer %+v", back[0])
		}
		if back[0].Gone != 9 {
			t.Fatalf("dropped default %d", back[0].Gone)
		}
		if back[0].Nested.A != 33 || back[0].Nested.B != 44 {
			t.Fatalf("nesting past unknown type %+v", back[0].Nested)
		}
		if r.Unknown != 2 {
			t.Fatalf("unknown %d", r.Unknown)
		}
		if r.KindMismatch != 1 {
			t.Fatalf("kind_mismatch %d", r.KindMismatch)
		}
		if back[0].Narrow != 3 {
			t.Fatalf("narrowing left default %d", back[0].Narrow)
		}
	}

	{
		broken := append([]byte(nil), w2...)
		broken[tblfx1.TableFixedHeaderBytes+4] ^= 0xFF
		var r tblfx1.TableReport
		plan := make([]tblfx1.TableFixedEntry, 1024)
		v := make([]tblfx1.FxRoot, 1)
		n := tblfx1.FxRootFixedLoad(v, broken, plan, &r)
		// RULE 1 BY ITS OWN NAME: 4 + 17*count is not the length given. The
		// seven rules run before the header's hash is checked, so a broken
		// layout is never reported as a lying header (§1.1, §2.1 step 5).
		if n >= 0 || r.Reason != "layout_count_mismatch" || r.Unknown != 0 || r.KindMismatch != 0 || r.Malformed {
			t.Fatalf("layout_count_mismatch: n=%d %+v", n, r)
		}
	}
	{
		other := append([]byte(nil), w2...)
		other[0] = 4
		var r tblfx1.TableReport
		plan := make([]tblfx1.TableFixedEntry, 1024)
		v := make([]tblfx1.FxRoot, 1)
		n := tblfx1.FxRootFixedLoad(v, other, plan, &r)
		if n >= 0 || r.Reason != "newer_form" || r.Malformed {
			t.Fatalf("newer_form: n=%d %+v", n, r)
		}
	}
	{
		var r tblfx1.TableReport
		tiny := make([]tblfx1.TableFixedEntry, 1)
		v := make([]tblfx1.FxRoot, 1)
		n := tblfx1.FxRootFixedLoad(v, w2, tiny, &r)
		if n >= 0 || r.Reason != "plan_too_large" {
			t.Fatalf("plan_too_large: n=%d %+v", n, r)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(testDir, "plan_test.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = testDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("plan path: %v\n%s", err, out)
	}
}

func writeUnit(t *testing.T, out, pkg, schema, runtime string) {
	t.Helper()
	u := unitFrom(t, schema)
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range tables {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mod := fmt.Sprintf("module %s\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", pkg, runtime)
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestForm1OfFixedTableReadsAndFixedLoadRefuses is the runtime half: the two
// entry points of a DECLARED fixed table, each over the other's form byte —
// Load reads the form-1 file back, FixedLoad names it `previous_form`.
func TestForm1OfFixedTableReadsAndFixedLoadRefuses(t *testing.T) {
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("testing")

func TestForm1LoadReadsAndFixedLoadRefuses(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointMeasure(&one)
	if need < 0 {
		t.Fatal("measure")
	}
	form1 := make([]byte, need)
	if n := PointSave(&one, form1); n != need {
		t.Fatalf("form-1 save %d", n)
	}
	if form1[0] != 1 {
		t.Fatalf("form-1 save wrote %d", form1[0])
	}
	var got Point
	var r TableReport
	if !PointLoad(&got, form1, &r) {
		t.Fatalf("form-1 Load of a DECLARED fixed table refused: %+v", r)
	}
	if r.Reason != "" || r.Verdict != TableOpenOk || r.Malformed {
		t.Fatalf("form-1 Load of a DECLARED fixed table is not a clean read: %+v", r)
	}
	if got.X != 4242 || got.Y != -7 {
		t.Fatalf("form-1 Load of a DECLARED fixed table lost the record: %+v", got)
	}
	batch := make([]Point, 1)
	r = TableReport{}
	plan := make([]TableFixedEntry, 64)
	n := PointFixedLoad(batch, form1, plan, &r)
	if n >= 0 || r.Reason != "previous_form" || r.Verdict != TableOpenRefused {
		t.Fatalf("FixedLoad(form1): n=%d %+v", n, r)
	}
}
`)
}

// TestFixedFormRefuseIsTotal asserts §5.3 "REFUSE is total: no counter moves,
// nothing is decoded, and not one destination byte is written". The destination
// is pre-poisoned with 0x5A and every poisoned byte is still 0x5A after the
// refusal. Every counter is zero at the same time.
func TestFixedFormRefuseIsTotal(t *testing.T) {
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import (
	"testing"
	"unsafe"
)

func TestFixedFormRefuseIsTotal(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointMeasure(&one)
	form1 := make([]byte, need)
	if n := PointSave(&one, form1); n != need {
		t.Fatalf("form-1 save %d", n)
	}
	if form1[0] != 1 {
		t.Fatalf("form-1 save wrote form %d", form1[0])
	}
	batch := make([]Point, 1)
	const poison = 0x5A
	dst := (*[8]byte)(unsafe.Pointer(&batch[0]))
	dst[0] = poison
	dst[1] = poison
	dst[2] = poison
	dst[3] = poison
	dst[4] = poison
	dst[5] = poison
	dst[6] = poison
	dst[7] = poison
	var r TableReport
	plan := make([]TableFixedEntry, 64)
	n := PointFixedLoad(batch, form1, plan, &r)
	if n >= 0 || r.Reason != "previous_form" || r.Verdict != TableOpenRefused {
		t.Fatalf("REFUSE is total: want refusal, got n=%d %+v", n, r)
	}
	if dst[0] != poison || dst[1] != poison || dst[2] != poison || dst[3] != poison ||
		dst[4] != poison || dst[5] != poison || dst[6] != poison || dst[7] != poison {
		t.Fatalf("REFUSE is total: destination changed")
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 {
		t.Fatalf("REFUSE is total: counters not zero: %+v", r)
	}
}
`)
}

func TestForm1OfVariableTableStillLoads(t *testing.T) {
	runGenerated(t, `package probe
table Node {
    n int32 = 5
    next *Node
}
`, `package probe
import ("testing"; "unsafe")

func TestForm1VariableLoad(t *testing.T) {
	var b NodeBuilder
	if !b.Init() {
		t.Fatal("init")
	}
	defer b.Shutdown()
	root := b.GetRoot()
	root.N = 9
	need := NodeMeasure(root, &b.Arena)
	if need < 0 {
		t.Fatal("measure")
	}
	form1 := make([]byte, need)
	if n := NodeSave(root, form1, &b.Arena); n != need {
		t.Fatalf("save %d", n)
	}
	if form1[0] != 1 {
		t.Fatalf("variable save wrote form %d", form1[0])
	}
	size := NodeLoadMeasure(form1)
	if size < 0 {
		t.Fatal("load measure")
	}
	raw := make([]byte, size+16)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 15
	region := raw[off : off+uintptr(size)]
	var r TableReport
	got := NodeLoad(region, form1, &r)
	if got == nil || r != (TableReport{}) || got.N != 9 {
		t.Fatalf("variable form-1 Load: %+v report %+v", got, r)
	}
}
`)
}

// TestFixedFormHostileBoolByte is the CONTROL for the one place a Go
// destination is not the wire's own byte. A Go true is the byte 1 (`== true`,
// array equality). `if v` is TESTB (nonzero) on amd64 and TBZ bit 0 on arm64,
// so a raw 2 reads true on one and false on the other — that is not this
// op's comparison. The wire and the C++ reference both say NONZERO IS TRUE
// (docs/SPEC-TABLES.md §3); tableFixedBool normalises (byte != 0 → 1). Every
// bool destination is here — a plain field, an optional's PRESENT byte, an
// optional's own value, and an array of them, which the coalescer folds into
// ONE bool run — and both reader paths are: the identity plan the emitter laid
// down, and the plan the compiler builds when a writer's layout is not this
// build's.
func TestFixedFormHostileBoolByte(t *testing.T) {
	runGenerated(t, `package probe
fixed table Host {
    on bool
    off bool
    maybe ?bool
    many [4]bool
    tail int32 = 7
}
`, `package probe
import ("testing"; "unsafe")

// the record body, in declared order at declared widths: on, off, maybe's
// present byte, maybe's value, four of many, then the tail's four
const bodyOn, bodyOff, bodyPresent, bodyMaybe, bodyMany, bodyTail = 0, 1, 2, 3, 4, 8

func hostileRecord(t *testing.T) []byte {
	t.Helper()
	if HostFixedBodyBytes != 12 {
		t.Fatalf("the record body moved: %d bytes, so the offsets below are stale", HostFixedBodyBytes)
	}
	buf := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{{Tail: 7}}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	body := buf[len(buf)-HostFixedRecordBytes+8:]
	// EVERY BOOL BYTE HOSTILE, and 2 is the one that matters: it is nonzero,
	// so it is true on the wire, and it is not 1, so a raw copy is not a Go true
	body[bodyOn] = 2
	body[bodyOff] = 0
	body[bodyPresent] = 3
	body[bodyMaybe] = 0xFF
	body[bodyMany+0] = 2
	body[bodyMany+1] = 0
	body[bodyMany+2] = 5
	body[bodyMany+3] = 0
	return buf
}

func wantHostile(t *testing.T, what string, v Host, r TableReport) {
	t.Helper()
	// The op's contract is the dest BYTE is 1. if v and v == true are both
	// TESTB (nonzero) on linux amd64, so a raw 2 already reads as a Go true
	// there — those comparisons cannot tell the op ran.
	raw := tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v))
	if raw[unsafe.Offsetof(v.On)] != 1 {
		t.Fatalf("%s: hostile 2 was not normalised to a Go true (1), dest=%d", what, raw[unsafe.Offsetof(v.On)])
	}
	if v.On != true {
		t.Fatalf("%s: a hostile 2 in a bool landed as false", what)
	}
	if v.Off == true {
		t.Fatalf("%s: a zero bool landed as true", what)
	}
	if v.MaybePresent != true {
		t.Fatalf("%s: a hostile 3 in the present byte landed as absent", what)
	}
	if v.Maybe != true {
		t.Fatalf("%s: a hostile 0xFF in an optional bool landed as false", what)
	}
	if v.Many != [4]bool{true, false, true, false} {
		t.Fatalf("%s: the bool array landed %v", what, v.Many)
	}
	if v.Tail != 7 {
		t.Fatalf("%s: the tail past the bools is %d, not the 7 the writer put there", what, v.Tail)
	}
	if r != (TableReport{}) {
		t.Fatalf("%s: a hostile bool byte is not one of the six events: %+v", what, r)
	}
}

func TestHostileBoolOnTheIdentityPath(t *testing.T) {
	buf := hostileRecord(t)
	got := make([]Host, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := HostFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("load n=%d %+v", n, r)
	}
	wantHostile(t, "identity plan", got[0], r)
}

func TestHostileBoolOnThePlanPath(t *testing.T) {
	buf := hostileRecord(t)
	parsed, why := tableFixedParseLayout(HostFixedLayout)
	if why != "" {
		t.Fatalf("my own layout does not parse: %s", why)
	}
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	made := tableFixedCompile(parsed, HostFixedLayout, HostFixedDst, plan, &r)
	if made <= 0 {
		t.Fatalf("compile made %d", made)
	}
	var v Host
	HostReset(&v)
	v.Tail = 0
	record := buf[len(buf)-HostFixedRecordBytes:]
	tableFixedRun(plan, made, record[8:], tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v)), &r)
	wantHostile(t, "compiled plan", v, r)
}

// THE NEGATIVE CONTROL, in the same file: sabotage tableFixedBool to a copy
// (the op it replaced) and require the dest BYTE to stay the planted 2, not
// the normalised 1. if v and v == true are both TESTB (nonzero) on linux
// amd64, so a planted 2 reads as a Go true there and a truthiness check
// falsely claims the op was never needed (c4eec5b9, CI red on ffa5da4b).
func TestHostileBoolNegativeControl(t *testing.T) {
	buf := hostileRecord(t)
	record := buf[len(buf)-HostFixedRecordBytes:]
	if HostFixedPlan.Count <= 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: no identity plan")
	}
	plan := make([]TableFixedEntry, HostFixedPlan.Count)
	copy(plan, HostFixedPlan.Entries[:HostFixedPlan.Count])
	n := 0
	for i := range plan {
		if plan[i].Op == tableFixedBool {
			plan[i].Op = tableFixedCopy
			n++
		}
	}
	if n == 0 {
		t.Fatal("NEGATIVE CONTROL FAILED: identity plan has no tableFixedBool to sabotage")
	}
	var v Host
	HostReset(&v)
	var r TableReport
	dst := tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v))
	tableFixedRun(plan, int32(len(plan)), record[8:], dst, &r)
	if dst[unsafe.Offsetof(v.On)] == 1 {
		t.Fatal("NEGATIVE CONTROL FAILED: sabotaged copy still wrote a Go true (1); the bool op was not what ran")
	}
	if dst[unsafe.Offsetof(v.On)] != 2 {
		t.Fatalf("NEGATIVE CONTROL FAILED: planted 2, dest byte is %d", dst[unsafe.Offsetof(v.On)])
	}
	dst[unsafe.Offsetof(v.On)] = record[8+bodyOn] // the copy the op replaced
	if dst[unsafe.Offsetof(v.On)] != 2 {
		t.Fatal("NEGATIVE CONTROL FAILED: the planted 2 did not survive a raw store")
	}
}
`)
}

// TestFixedFormHostileUnionTag is the union tag's control. A tag beyond the
// arms this reader has is an ORDINAL past the set (docs/SPEC-TABLES.md §3.4):
// the storage pass lands None and `clamped` counts. Copied raw it would land
// as a discriminant no variant spells, with every arm's guard declining to
// fill it — a value that is neither None nor an arm, and nothing counted to
// say so.
func TestFixedFormHostileUnionTag(t *testing.T) {
	runGenerated(t, `package probe
type Hit { damage int32 }
type Chat { volume int32 }
union Pick
{
    hit  Hit
    chat Chat
}
fixed table Host {
    pick Pick
    tail int32 = 9
}
`, `package probe
import ("testing")

func TestUnknownTagIsNoneAndCounts(t *testing.T) {
	one := Host{Tail: 9}
	HostReset(&one)
	one.Pick.Type = PickTypeChat
	one.Pick.Chat.Volume = 4242
	buf := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{one}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	body := buf[len(buf)-HostFixedRecordBytes+8:]
	if body[0] != byte(PickTypeChat) {
		t.Fatalf("the tag does not lead the record: %d", body[0])
	}

	// the arm this reader HAS still lands, and nothing counts
	got := make([]Host, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := HostFixedLoad(got, buf, plan, &r); n != 1 || got[0].Pick.Type != PickTypeChat || got[0].Pick.Chat.Volume != 4242 {
		t.Fatalf("known arm: n=%d %+v %+v", n, got[0], r)
	}
	if r != (TableReport{}) || got[0].Tail != 9 {
		t.Fatalf("known arm moved a counter or the tail: %+v %+v", r, got[0])
	}

	// EVERY TAG BEYOND THE ARMS, and 0 stays None without counting
	for _, tag := range []byte{3, 4, 0x7F, 0xFF} {
		hostile := append([]byte(nil), buf...)
		hostile[len(hostile)-HostFixedRecordBytes+8] = tag
		got = make([]Host, 1)
		r = TableReport{}
		if n := HostFixedLoad(got, hostile, plan, &r); n != 1 {
			t.Fatalf("tag %d: n=%d %+v", tag, n, r)
		}
		if got[0].Pick.Type != PickTypeNone {
			t.Fatalf("tag %d landed as %d, not None", tag, got[0].Pick.Type)
		}
		if r.Clamped != 1 || r.Unknown != 0 || r.KindMismatch != 0 || r.Malformed || r.Verdict != TableOpenOk {
			t.Fatalf("tag %d: an ordinal past the set is one clamp and nothing else: %+v", tag, r)
		}
		if got[0].Tail != 9 {
			t.Fatalf("tag %d: the field past the union moved: %d", tag, got[0].Tail)
		}
	}

	none := append([]byte(nil), buf...)
	none[len(none)-HostFixedRecordBytes+8] = 0
	got = make([]Host, 1)
	r = TableReport{}
	if n := HostFixedLoad(got, none, plan, &r); n != 1 || got[0].Pick.Type != PickTypeNone || r != (TableReport{}) {
		t.Fatalf("None is not unknown: n=%d %+v %+v", n, got[0], r)
	}
}
`)
}

// TestFixedFormArgLaneTextUnderSecondArm is reference-fix 12's probe (Rowan,
// Java #833 read): a string(8) under a union's SECOND arm. Write on the
// identity path, read with a compiled plan from a different layout. If Arg
// is both the guard's arm ordinal and the text flavour, the compiled read
// drops the string silently.
func TestFixedFormArgLaneTextUnderSecondArm(t *testing.T) {
	t.Skip("retired by docs/FIXED-FORM-ALGORITHM.md §5.6; owed again on the lineage harness")
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../../serialize.go")
	if err != nil {
		t.Fatal(err)
	}
	writer := `package tblw
type ArmA { n int32 }
type ArmB { s string(8) }
union Pick
{
    a ArmA
    b ArmB
}
fixed table Root {
    pick Pick
}
`
	reader := `package tblr
type ArmA { n int32 }
type ArmB { s string(8) }
union Pick
{
    a ArmA
    b ArmB
}
fixed table Root {
    pick Pick
    tail int32 = 7
}
`
	writeUnit(t, filepath.Join(dir, "w"), "tblw", writer, runtime)
	writeUnit(t, filepath.Join(dir, "r"), "tblr", reader, runtime)
	testDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := fmt.Sprintf("module arglanetest\n\ngo 1.26\n\nrequire (\n\ttblw v0.0.0\n\ttblr v0.0.0\n\tgithub.com/mas-bandwidth/serialize.go v0.0.0\n)\nreplace tblw => ../w\nreplace tblr => ../r\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime)
	if err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `package arglanetest

import (
	"testing"

	"tblr"
	"tblw"
)

func TestArgLane(t *testing.T) {
	one := tblw.Root{}
	tblw.RootReset(&one)
	one.Pick.Type = tblw.PickTypeB
	copy(one.Pick.B.S[:], "hi")
	one.Pick.B.SLength = 2
	buf := make([]byte, tblw.RootFixedMeasure(1))
	if n := tblw.RootFixedSave([]tblw.Root{one}, buf); n != int64(len(buf)) {
		t.Fatalf("identity save %d", n)
	}

	// same-layout identity read: the string must survive or the fixture is wrong
	same := make([]tblw.Root, 1)
	var wr tblw.TableReport
	wplan := make([]tblw.TableFixedEntry, 256)
	if n := tblw.RootFixedLoad(same, buf, wplan, &wr); n != 1 || same[0].Pick.Type != tblw.PickTypeB || string(same[0].Pick.B.S[:same[0].Pick.B.SLength]) != "hi" {
		t.Fatalf("identity: n=%d %+v %+v", n, same[0], wr)
	}

	got := make([]tblr.Root, 1)
	got[0].Tail = 99
	var r tblr.TableReport
	plan := make([]tblr.TableFixedEntry, 256)
	if n := tblr.RootFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("compiled n=%d %+v", n, r)
	}
	if got[0].Pick.Type != tblr.PickTypeB {
		t.Fatalf("compiled arm %d, want B; report %+v", got[0].Pick.Type, r)
	}
	if string(got[0].Pick.B.S[:got[0].Pick.B.SLength]) != "hi" {
		t.Fatalf("ARG LANE BITES: compiled read of string under union arm 2 landed %q (len %d), identity had hi; report %+v", got[0].Pick.B.S[:got[0].Pick.B.SLength], got[0].Pick.B.SLength, r)
	}
	if got[0].Tail != 7 {
		t.Fatalf("reader tail default %d", got[0].Tail)
	}
}
`
	if err := os.WriteFile(filepath.Join(testDir, "arg_lane_test.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = testDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("arg-lane probe:\n%s", out)
	}
}

// TestFixedFormArgLaneOldEncodingControl plants the OLD encoding: flavour in
// Arg, Meta unused. A compiled text-under-arm-2 read must DROP the string —
// putting flavour in Arg must not stay green.
func TestFixedFormArgLaneOldEncodingControl(t *testing.T) {
	runGenerated(t, `package probe
type ArmA { n int32 }
type ArmB { s string(8) }
union Pick
{
    a ArmA
    b ArmB
}
fixed table Writer {
    pick Pick
}
fixed table Reader {
    pick Pick
    tail int32 = 7
}
`, `package probe
import (
	"testing"
	"unsafe"
)

func TestOldArgLaneControl(t *testing.T) {
	one := Writer{}
	WriterReset(&one)
	one.Pick.Type = PickTypeB
	copy(one.Pick.B.S[:], "hi")
	one.Pick.B.SLength = 2
	buf := make([]byte, WriterFixedMeasure(1))
	if n := WriterFixedSave([]Writer{one}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}

	layoutBytes := tableFixedGet32(buf[TableFixedHeaderBytes:])
	layout := buf[TableFixedHeaderBytes+4 : TableFixedHeaderBytes+4+layoutBytes]
	parsed, why := tableFixedParseLayout(layout)
	if why != "" {
		t.Fatalf("writer layout does not parse: %s", why)
	}
	plan := make([]TableFixedEntry, 256)
	var r TableReport
	made := tableFixedCompile(parsed, ReaderFixedLayout, ReaderFixedDst, plan, &r)
	if made <= 0 {
		t.Fatalf("compile made %d %+v", made, r)
	}

	var got Reader
	ReaderReset(&got)
	record := buf[len(buf)-WriterFixedRecordBytes:]
	tableFixedRun(plan, made, record[8:], tableFixedOverlay(unsafe.Pointer(&got), unsafe.Sizeof(got)), &r)
	if got.Pick.Type != PickTypeB || string(got.Pick.B.S[:got.Pick.B.SLength]) != "hi" {
		t.Fatalf("split itself is not in: arm %d %q %+v", got.Pick.Type, got.Pick.B.S[:got.Pick.B.SLength], r)
	}

	found := false
	for i := int32(0); i < made; i++ {
		if plan[i].Op != tableFixedText {
			continue
		}
		found = true
		if plan[i].Arg != 2 || plan[i].Meta != tableFixedTextUtf8 {
			t.Fatalf("text entry is not arm-2/utf8: Arg=%d Meta=%d", plan[i].Arg, plan[i].Meta)
		}
		// OLD ENCODING: flavour in Arg, Meta unused
		plan[i].Arg = uint32(plan[i].Meta)
		plan[i].Meta = 0
	}
	if !found {
		t.Fatal("compiled plan has no text entry to plant the old lane on")
	}

	got = Reader{}
	ReaderReset(&got)
	r = TableReport{}
	tableFixedRun(plan, made, record[8:], tableFixedOverlay(unsafe.Pointer(&got), unsafe.Sizeof(got)), &r)
	landed := string(got.Pick.B.S[:got.Pick.B.SLength])
	if landed == "hi" {
		t.Fatal("NEGATIVE CONTROL FAILED: flavour in Arg, Meta unused still landed the string under union arm 2; the old lane stayed green")
	}
	if got.Pick.Type != PickTypeB {
		t.Fatalf("old-lane sabotage moved the arm: %d %+v", got.Pick.Type, r)
	}
}
`)
}

// TestFixedFormRecordBound is the Go twin of the C++ reference's
// record_bound_case() (test/tables/fixedform_main.cpp): W10 — EVERY COMPILED
// ENTRY IS BOUNDED BY THE WRITER'S OWN DECLARED RECORD SIZE
// (docs/FIXED-FORM-ALGORITHM.md §4.2 fix 3). The plan's source offsets are
// arithmetic over sizes a WRITER wrote down, so a layout that passes every rule
// of §1.1 and still names a byte past `root.size` is refused WHOLE, never partly
// compiled, under `layout_record_too_large`.
//
// The layout is hand-built out of THIS build's own field ids, read out of
// HostFixedLayout rather than spelled as hashes, so the case does not quietly
// stop naming the fields it means. The overrun comes from an ARRAY entry's
// admitted size — `size % elem == 0` OR `4 + n*elem` — so a size of ZERO is a
// valid bare array of no elements, and the compiler takes the head from the
// READER's own row (d.Counted, `bytes(N)` is a counted array on this wire), so a
// reader whose field carries a live count subtracts four from zero: the elements
// land past a record that ends at four.
func TestFixedFormRecordBound(t *testing.T) {
	runGenerated(t, `package probe
fixed table Host {
    keep  uint32 = 7
    label string(8) = "hi"
    blob  bytes(6)
}
`, `package probe
import (
	"encoding/binary"
	"testing"
	"unsafe"
)

func putEntry(layout []byte, id uint64, kind uint8, size, children uint32) []byte {
	e := make([]byte, 17)
	binary.LittleEndian.PutUint64(e, id)
	e[8] = kind
	binary.LittleEndian.PutUint32(e[9:], size)
	binary.LittleEndian.PutUint32(e[13:], children)
	return append(layout, e...)
}

func count4(n uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, n)
	return b
}

func hashOf(layout []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, c := range layout {
		h ^= uint64(c)
		h *= 0x100000001b3
	}
	return h
}

func TestRecordBound(t *testing.T) {
	// the ids are READ OUT of this build's own layout, not spelled as hashes
	mine, why := tableFixedParseLayout(HostFixedLayout)
	if why != "" {
		t.Fatalf("W10: my own layout does not parse: %s", why)
	}
	rootID := tableFixedEntryAt(mine, 0).Id
	var keepID, blobID, blobElemID, labelID uint64
	for i := int32(1); i < mine.Count; i++ {
		e := tableFixedEntryAt(mine, i)
		if e.Kind == 8 && e.Size == 4 && keepID == 0 {
			keepID = e.Id // keep, a uint32
		}
		if e.Kind == 12 && labelID == 0 {
			labelID = e.Id // label, a string(8)
		}
		if e.Kind == 14 && i+1 < mine.Count {
			el := tableFixedEntryAt(mine, i+1)
			if el.Kind == 6 && el.Size == 1 {
				blobID = e.Id     // blob, bytes(6): an array of u8
				blobElemID = el.Id // the u8 element; may be zero
			}
		}
	}
	if rootID == 0 || keepID == 0 || blobID == 0 || labelID == 0 {
		t.Fatalf("W10: the ids this case names are the ids this build's own layout carries")
	}

	// 1. AN ENTRY WHOSE src+size REACHES PAST root.size. The record body is
	//    FOUR bytes (root kind 13 size 4), keep fills it at 0, then an array
	//    of size ZERO at offset 4. A size of zero is a valid bare array, but
	//    the reader's counted head turns it into elements past the record.
	blobSize := uint32(0)
	hostile := count4(4)
	hostile = putEntry(hostile, rootID, 13, 4+blobSize, 2)
	hostile = putEntry(hostile, keepID, 8, 4, 0)
	hostile = putEntry(hostile, blobID, 14, blobSize, 1)
	hostile = putEntry(hostile, blobElemID, 6, 1, 0)

	parsed, why := tableFixedParseLayout(hostile)
	if why != "" {
		t.Fatalf("W10: the layout passes all seven rules of §1.1, so the refusal below is the BOUND; got %s", why)
	}
	plan := make([]TableFixedEntry, 256)
	var cr TableReport
	made := tableFixedCompile(parsed, HostFixedLayout, HostFixedDst, plan, &cr)
	if made != -3 {
		t.Fatalf("W10: COMPILE answers %d, want -3 (hostile) for an entry past the writer's record size", made)
	}
	known := []TableFixedKnownLayout{{Hash: 0xDEADBEEF, Layout: hostile, Record: 4}}
	lineage := tableFixedLineagePlans(known, HostFixedLayout, HostFixedDst, HostFixedHash)
	if lineage[0].Why != "layout_record_too_large" {
		t.Fatalf("W10: COMPILE's refusal is named %q, want layout_record_too_large", lineage[0].Why)
	}

	// LOAD of the same bytes: hash selects, and a stranger is layout_newer.
	file := make([]byte, TableFixedHeaderBytes+4)
	file[0] = TableFixedForm
	tableFixedPut64(file[TableFixedHashAt:], hashOf(hostile))
	tableFixedPut32(file[TableFixedHeaderBytes:], uint32(len(hostile)))
	file = append(file, hostile...)
	got := make([]Host, 1)
	HostReset(&got[0])
	var r TableReport
	n := HostFixedLoad(got, file, make([]TableFixedEntry, 256), &r)
	if n >= 0 || r.Reason != "layout_newer" || r.Verdict != TableOpenRefused {
		t.Fatalf("W10: LOAD of the same bytes is not refused: n=%d %+v", n, r)
	}
	if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Clamped != 0 || r.Malformed {
		t.Fatalf("W10: the refusal sets nothing and counts nothing: %+v", r)
	}
	if got[0].Keep != 7 || got[0].BlobLength != 0 {
		t.Fatalf("W10: NOT PARTLY COMPILED — no entry of the plan ran: keep=%d blob_length=%d", got[0].Keep, got[0].BlobLength)
	}

	// 2. THE BOUNDARY ITSELF STANDS: a text entry whose length word and payload
	//    END EXACTLY at root.size is a layout with nothing wrong with it, and a
	//    bound that refused it would be a bound that refuses the wire.
	boundary := count4(3)
	boundary = putEntry(boundary, rootID, 13, 16, 2)
	boundary = putEntry(boundary, keepID, 8, 4, 0)
	boundary = putEntry(boundary, labelID, 12, 12, 0) // src 4, +4 the length word, 12 bytes to 16
	bparsed, bwhy := tableFixedParseLayout(boundary)
	if bwhy != "" {
		t.Fatalf("W10: the boundary layout does not parse: %s", bwhy)
	}
	bplan := make([]TableFixedEntry, 256)
	var br TableReport
	bmade := tableFixedCompile(bparsed, HostFixedLayout, HostFixedDst, bplan, &br)
	if bmade < 0 {
		t.Fatalf("W10: a text entry ending EXACTLY at root.size must still compile, got %d", bmade)
	}
	body := make([]byte, 16)
	tableFixedPut32(body, 4242)  // keep
	tableFixedPut32(body[4:], 2) // the label's length
	body[8] = 'h'
	body[9] = 'i'
	var v Host
	HostReset(&v)
	tableFixedRun(bplan, bmade, body, tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v)), &br)
	if v.Keep != 4242 || v.LabelLength != 2 || v.Label[0] != 'h' || v.Label[1] != 'i' {
		t.Fatalf("W10: the bound admits the last byte of the record: %+v", v)
	}
}
`)
}

func tableFile(files map[string][]byte) string {
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	return body
}

func TestFixedFormClampPassShape(t *testing.T) {
	tagOnly := generate(t, `package probe
type Boost { power int32 = 0 }
type Ward { charge float32 = 0.0 }
union Effect
{
    boost Boost
    ward Ward
}
fixed table Cfg {
    a int32 = 5 | min = 0, max = 1000
    effect Effect
    marks [..4]int32 | min = 0, max = 10
    note ?int32 | min = 0, max = 10
}
`)
	body := tableFile(tagOnly)
	clampBody := funcSource(body, "func CfgFixedClampBody(")
	if clampBody == "" {
		t.Fatal("CfgFixedClampBody was not emitted")
	}
	if !strings.Contains(clampBody, "if uint32(value.Effect.Type) > 2") {
		t.Error("a union tag past the arm count must still remap to None")
	}
	if strings.Contains(clampBody, "switch value.Effect.Type") {
		t.Error("a union whose arms declare nothing bounded must not emit a switch with nothing to switch on")
	}
	if !strings.Contains(clampBody, "for i := int64(0); i < int64(value.MarksCount); i++") {
		t.Error("a counted array must clamp LIVE elements, not the declared bound")
	}
	if strings.Contains(clampBody, "i < 4") {
		t.Error("a counted array's clamp loop must not walk the slack")
	}
	if !strings.Contains(clampBody, "if value.NotePresent") {
		t.Error("an absent optional's payload must not be held to a bound")
	}
	load := funcSource(body, "func CfgFixedLoad(")
	if !strings.Contains(load, "CfgFixedClamp(&values[k], report)") {
		t.Error("FixedLoad must call the storage pass after the run, both paths")
	}
	if strings.Contains(body, "Op: tableFixedTag") {
		t.Error("identity must copy the tag; the storage pass holds the bound")
	}

	rangedArm := generate(t, `package probe
type Boost { power int32 = 0 | min = 0, max = 10 }
type Ward { charge float32 = 0.0 }
union Effect
{
    boost Boost
    ward Ward
}
fixed table Cfg {
    a int32 = 5 | min = 0, max = 1000
    effect Effect
}
`)
	with := tableFile(rangedArm)
	bodyFn := funcSource(with, "func CfgFixedClampBody(")
	if !strings.Contains(bodyFn, "switch value.Effect.Type") {
		t.Error("a union with a bounded arm must still switch over the set arm")
	}
	if !strings.Contains(bodyFn, "case EffectTypeBoost:") {
		t.Error("the bounded arm must be a case")
	}
}

func TestFixedFormClampLiveCount(t *testing.T) {
	runGenerated(t, `package probe
enum Grade { Bronze, Gold }
type ArmA { n int32 = 0 | min = 0, max = 10 }
type ArmB { m int32 = 0 }
union Pick
{
    a ArmA
    b ArmB
}
fixed table Root {
    n     int32 = 0 | min = 0, max = 10
    marks [..4]int32 | min = 0, max = 10
    note  ?int32 | min = 0, max = 10
    pick  Pick
    grade Grade
}
`, `package probe
import ("testing")

func TestLiveCount(t *testing.T) {
	one := Root{}
	RootReset(&one)
	one.N = 99
	one.MarksCount = 1
	one.Marks[0] = 99
	one.Marks[1] = 99
	one.Marks[2] = 99
	one.Marks[3] = 99
	one.NotePresent = false
	one.Note = 99
	need := RootFixedMeasure(1)
	buf := make([]byte, need)
	if n := RootFixedSave([]Root{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	got := make([]Root, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := RootFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("load %d %+v", n, r)
	}
	if got[0].N != 10 || r.Clamped != 2 {
		t.Fatalf("LIVE: n=%d marks0=%d clamped=%d (want n=10, two clamps: n and marks[0]; slack must not count)", got[0].N, got[0].Marks[0], r.Clamped)
	}
	if got[0].Marks[0] != 10 {
		t.Fatalf("live element %d", got[0].Marks[0])
	}

	tag := Root{}
	RootReset(&tag)
	tag.Pick.Type = PickType(7)
	tb := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{tag}, tb); n != int64(len(tb)) {
		t.Fatalf("tag save %d", n)
	}
	got = make([]Root, 1)
	r = TableReport{}
	if n := RootFixedLoad(got, tb, plan, &r); n != 1 {
		t.Fatalf("tag load %d %+v", n, r)
	}
	if got[0].Pick.Type != PickTypeNone || r.Clamped != 1 || r.Unknown != 0 {
		t.Fatalf("ORDINAL tag: type=%d clamped=%d unknown=%d", got[0].Pick.Type, r.Clamped, r.Unknown)
	}

	en := Root{}
	RootReset(&en)
	en.Grade = Grade(9)
	eb := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{en}, eb); n != int64(len(eb)) {
		t.Fatalf("enum save %d", n)
	}
	got = make([]Root, 1)
	r = TableReport{}
	if n := RootFixedLoad(got, eb, plan, &r); n != 1 {
		t.Fatalf("enum load %d %+v", n, r)
	}
	if got[0].Grade != GradeNone || r.Clamped != 1 {
		t.Fatalf("ORDINAL enum: grade=%d clamped=%d", got[0].Grade, r.Clamped)
	}

	arm := Root{}
	RootReset(&arm)
	arm.Pick.Type = PickTypeB
	arm.Pick.A.N = 99
	ab := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{arm}, ab); n != int64(len(ab)) {
		t.Fatalf("arm save %d", n)
	}
	got = make([]Root, 1)
	r = TableReport{}
	if n := RootFixedLoad(got, ab, plan, &r); n != 1 {
		t.Fatalf("arm load %d %+v", n, r)
	}
	if r.Clamped != 0 {
		t.Fatalf("unselected arm must not count: clamped=%d", r.Clamped)
	}
}
`)
}

// THE INSERTED-VARIANT-AND-ARM ROW (schema#876 E7, the §8 matrix's
// "variant/arm inserted mid-list, keyed slots sliding, optional, moved kind"
// over test/tables/V1 and V2). Its reference is the C++ case at
// test/tables/fixedform_main.cpp:166-200: a V1 record whose `grade` is Gold
// (ordinal 2) and whose `effect` is Ward (arm 2), read by V2, which inserts
// Silver BETWEEN Bronze and Gold and Hex BETWEEN boost and ward. The values
// must come back by NAME, not by position: positional decoding lands Silver and
// Hex, one slot off, with nothing on the wire that could say so.
//
// The emitter is the site: tableFixedCompileEntry matches a union's arms and an
// enum's variants by their wire id — the hash of the variant/arm NAME
// (internal/codegen/gotable/fixedruntime.go, kind 15 at the `ta.Id == ma.Id`
// arm walk and kind 30 at the `landed = uint16(k + 1)` remap table). This case
// is the runtime proof of that match: disable the match and the record lands the
// wrong variant.
func TestFixedFormVariantOrArmInsertedMidList(t *testing.T) {
	v1src, err := os.ReadFile("../../../test/tables/V1.schema")
	if err != nil {
		t.Fatal(err)
	}
	v2src, err := os.ReadFile("../../../test/tables/V2.schema")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	v1u := unitFrom(t, string(v1src))
	v2u := unitFrom(t, string(v2src))

	// THE READER IS HANDED THE WRITER'S LOCKED LAYOUT (§5.2): V2's build carries
	// V1's layout as an older lineage entry, oldest first. Without it the file's
	// hash is outside V2's lineage and the read is `layout_newer` before any
	// record — a different refusal, not this row.
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(v1u) {
		e, ok := FixedLineageOf(v1u, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	writeFixedUnit(t, filepath.Join(dir, "tblv1"), v1u, nil)
	writeFixedUnit(t, filepath.Join(dir, "tblv2"), v2u, lineage)

	// THE SIBLING RUNTIME. The generated unit's PACKET declarations live beside
	// the table wire, and that half imports serialize.go. The fixed-form path
	// crosses none of it, but the unit still has to compile, so a compile-only
	// stub stands in for a checkout that is not in the sandbox. It is never
	// called: every symbol below exists only so the packet codecs type-check.
	serializeDir := filepath.Join(dir, "serialize")
	if err := os.MkdirAll(serializeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serializeDir, "go.mod"), []byte("module github.com/mas-bandwidth/serialize.go\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serializeDir, "serialize.go"), []byte(`package serialize

import "errors"

var ErrValueOutOfRange = errors.New("value out of range")

type WriteStream struct{}
type ReadStream struct{}

func (s *WriteStream) Err() error                       { return nil }
func (s *ReadStream) Err() error                        { return nil }
func (s *WriteStream) SerializeFloat32(v *float32)      {}
func (s *ReadStream) SerializeFloat32(v *float32)       {}
func (s *WriteStream) SerializeBits(v any, bits int)    {}
func (s *ReadStream) SerializeBits(v any, bits int)     {}
func (s *WriteStream) SerializeBits64(v *uint64, bits int) {}
func (s *ReadStream) SerializeBits64(v *uint64, bits int)  {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"tblv1", "tblv2"} {
		mod := fmt.Sprintf("module %s\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => ../serialize\n", pkg)
		if err := os.WriteFile(filepath.Join(dir, pkg, "go.mod"), []byte(mod), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	testDir := filepath.Join(dir, "test")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module rows\n\ngo 1.26\n\nrequire (\n\ttblv1 v0.0.0\n\ttblv2 v0.0.0\n)\nreplace tblv1 => ../tblv1\nreplace tblv2 => ../tblv2\nreplace github.com/mas-bandwidth/serialize.go => ../serialize\n"
	if err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `package rows

import (
	"testing"

	"tblv1"
	"tblv2"
)

func TestVariantOrArmInsertedMidList(t *testing.T) {
	one := tblv1.Cfg{}
	tblv1.CfgReset(&one)
	one.A = 42
	copy(one.Name[:], "hello")
	one.NameLength = 5
	one.Grade = tblv1.GradeGold // V1 ordinal 2; V2 puts Silver at 2 and Gold at 3
	one.Effect.Type = tblv1.EffectTypeWard
	one.Effect.Ward.Charge = 0.75 // V1 arm 2; V2 puts Hex at 2 and Ward at 3
	one.Tokens[int(tblv1.SlotAlpha)-1] = 21
	one.Tokens[int(tblv1.SlotDelta)-1] = 24 // V2 slides Beta and keeps Delta
	one.TierPresent = true
	one.Tier = 77

	wire := make([]byte, tblv1.CfgFixedMeasure(1))
	if n := tblv1.CfgFixedSave([]tblv1.Cfg{one}, wire); n != int64(len(wire)) {
		t.Fatalf("V1 save %d want %d", n, len(wire))
	}

	back := make([]tblv2.Cfg, 1)
	var r tblv2.TableReport
	plan := make([]tblv2.TableFixedEntry, 8192)
	if n := tblv2.CfgFixedLoad(back, wire, plan, &r); n != 1 {
		t.Fatalf("V2 reads V1: n=%d report=%+v", n, r)
	}
	if back[0].Grade != tblv2.GradeGold {
		t.Fatalf("ENUM: V1's Gold (ordinal 2) read as ordinal %d, not V2's Gold (ordinal 3): an inserted variant was not remapped by NAME", back[0].Grade)
	}
	if back[0].Effect.Type != tblv2.EffectTypeWard {
		t.Fatalf("UNION: V1's Ward (arm 2) read as arm %d, not V2's Ward (arm 3): an inserted arm was not remapped by NAME", back[0].Effect.Type)
	}
	if back[0].Effect.Ward.Charge != 0.75 {
		t.Fatalf("UNION: the remapped arm's payload is %v, not the writer's 0.75", back[0].Effect.Ward.Charge)
	}
	if back[0].Tokens[int(tblv2.SlotAlpha)-1] != 21 || back[0].Tokens[int(tblv2.SlotDelta)-1] != 24 {
		t.Fatalf("KEYED: a slid slot lost its value: alpha=%d delta=%d", back[0].Tokens[int(tblv2.SlotAlpha)-1], back[0].Tokens[int(tblv2.SlotDelta)-1])
	}
	if back[0].Tokens[int(tblv2.SlotSigma)-1] != 0 {
		t.Fatalf("KEYED: a key the writer has no name for is %d, not its default 0", back[0].Tokens[int(tblv2.SlotSigma)-1])
	}
	if !back[0].TierPresent || back[0].Tier != 77 {
		t.Fatalf("OPTIONAL: present=%v tier=%d, not the writer's 77", back[0].TierPresent, back[0].Tier)
	}
	if !back[0].C {
		t.Fatalf("MISSING: V2's own c field did not take its declared default true")
	}
	if back[0].A != 5.0 {
		t.Fatalf("KIND MOVED: int32 -> float32 landed %v, not the declared default 5.0", back[0].A)
	}
	if r.Malformed || r.Verdict != tblv2.TableOpenOk {
		t.Fatalf("V2 reads V1: damage or refusal: %+v", r)
	}
}
`
	if err := os.WriteFile(filepath.Join(testDir, "e7_test.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", ".")
	cmd.Dir = testDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("variant/arm inserted mid-list:\n%s", out)
	}
}

// writeFixedUnit writes one generated unit — the PACKET declarations and the
// table wire — into its own module directory. GenerateLineage is the only
// difference from writeUnit: the reader's unit carries the writer's locked
// layouts (§5.2), which is what makes an older file readable in a self-contained
// test.
func writeFixedUnit(t *testing.T, out string, u *ir.Unit, lineage map[string][]FixedLineageEntry) {
	t.Helper()
	packet, err := golang.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range packet {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range tables {
		if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFixedFormWideTextCodeUnits is item C5 of schema#876: a `wstring(N)`'s
// length rides the fixed form in UTF-16 CODE UNITS, so the read-side cap is
// `span / 2` and never `span`, and an astral pair counts TWO. The contract
// states it twice and in the same words (docs/FIXED-FORM-ALGORITHM.md §4.5's
// text row): "unit := (meta == wide) ? 2 : 1; cap := size / unit ... wide code
// units over v, never 2N, an astral pair counting two (fix 7)"; and the TEXT
// op's full form repeats it with "COPY(the WHOLE SPAN, never v * unit)" and a
// terminator of ONE UNIT at the used length.
//
// THE HOUSE FIXTURE IS test/tables/FXW.schema, and the C++ reference writes
// its oracle bytes at test/tables/fixedform_dump.cpp:581-609 (`fxw_file`), the
// ONE place a wide text payload is pinned: "a leg that counted bytes where it
// owed units comes out wrong here and nowhere else". The Dart leg reads those
// bytes at test/dart-tables/fixedform.dart:349-386 and is the ported reasoning
// this case follows: `caption_length is in units` (7), the narrow
// `label_length is in bytes` (6), `inner.text`'s wide flavour at a NESTED
// offset, and record 1's astral pair counting BOTH halves (5).
//
// This asserts the property where it BITES, not a self-agreeing round trip: a
// length that is legal in BYTES (16, the entry's whole span) but past the
// code-unit cap (8) must CLAMP to 8 and count once. Disabling the unit at
// internal/codegen/gotable/fixedruntime.go:306 (`unit = 2` -> `1`) leaves that
// forged read GREEN with length 16, so the case measures the unit and nothing
// else. The legal record reads back whole with `clamped == 0`, so it is not
// vacuous.
func TestFixedFormWideTextCodeUnits(t *testing.T) {
	runGenerated(t, `package probe
fixed table FxCaption {
    text wstring(4)
}
fixed table FxWide {
    caption wstring(8)
    label   string(8)
    inner   FxCaption
    seq     uint32 = 1 | min = 0, max = 1000
}
`, `package probe
import ("encoding/binary"; "testing")

func wideTextRecord(t *testing.T) []byte {
	t.Helper()
	one := FxWide{}
	FxWideReset(&one)
	// seven BASIC-PLANE code units, one short of the bound (FXW record 0)
	caption0 := []uint16{0x68, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0x21}
	copy(one.Caption[:], caption0)
	one.CaptionLength = 7
	copy(one.Label[:], "narrow")
	one.LabelLength = 6
	inner0 := []uint16{0x61, 0x62, 0x63, 0x64}
	copy(one.Inner.Text[:], inner0)
	one.Inner.TextLength = 4
	one.Seq = 41

	two := FxWide{}
	FxWideReset(&two)
	// AN ASTRAL PAIR AND THE TWO BASIC-PLANE ENDS OF THE RANGE (FXW record 1):
	// a surrogate pair is TWO code units and the length counts both.
	caption1 := []uint16{0xE000, 0xD83D, 0xDE00, 0xFFFF, 0x7A}
	copy(two.Caption[:], caption1)
	two.CaptionLength = 5
	two.Seq = 1000

	buf := make([]byte, FxWideFixedMeasure(2))
	if n := FxWideFixedSave([]FxWide{one, two}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}
	return buf
}

// wideCaptionEntry is the identity plan's wide text row, and it asserts the
// entry's SIZE is 2N BYTES while its cap is N UNITS — the two numbers the
// contract says a port gets wrong one at a time.
func wideCaptionEntry(t *testing.T) TableFixedEntry {
	t.Helper()
	for i := range FxWideFixedPlan.Entries {
		e := FxWideFixedPlan.Entries[i]
		if e.Op == tableFixedText && e.Meta == tableFixedTextWide {
			if e.Size != 16 {
				t.Fatalf("a wstring(8) entry spans 2N bytes, got %d", e.Size)
			}
			return e
		}
	}
	t.Fatal("the identity plan carries no WIDE text entry")
	return TableFixedEntry{}
}

func TestWideText(t *testing.T) {
	buf := wideTextRecord(t)
	wide := wideCaptionEntry(t)
	if wide.Src != 0 {
		t.Fatalf("the caption leads the body, got Src=%d", wide.Src)
	}
	rec := int(FxWideFixedRecordBytes)
	r0 := buf[len(buf)-2*rec+8:]
	r1 := buf[len(buf)-rec+8:]
	// THE WIRE LENGTH IS IN CODE UNITS, never bytes: 7 for seven units, and 5
	// for the four basic-plane ends plus the pair.
	if got := binary.LittleEndian.Uint32(r0[wide.Src:]); got != 7 {
		t.Fatalf("WIRE LENGTH IS BYTES: seven code units wrote %d, want 7", got)
	}
	if got := binary.LittleEndian.Uint32(r1[wide.Src:]); got != 5 {
		t.Fatalf("the astral pair counts TWO: five code units wrote %d, want 5", got)
	}
	// and each unit is two bytes, little-endian (0xE000 the pair's halves etc.)
	want := []byte{0x00, 0xE0, 0x3D, 0xD8, 0x00, 0xDE, 0xFF, 0xFF, 0x7A, 0x00}
	at := int(wide.Src) + 4
	for i, b := range want {
		if r1[at+i] != b {
			t.Fatalf("unit byte %d = %#x, want %#x", i, r1[at+i], b)
		}
	}

	got := make([]FxWide, 2)
	var rep TableReport
	plan := make([]TableFixedEntry, 256)
	if n := FxWideFixedLoad(got, buf, plan, &rep); n != 2 {
		t.Fatalf("load %d %+v", n, rep)
	}
	if rep != (TableReport{}) {
		t.Fatalf("a clean wide read moves a counter: %+v", rep)
	}
	if got[0].CaptionLength != 7 || got[0].LabelLength != 6 || got[0].Inner.TextLength != 4 || got[0].Seq != 41 {
		t.Fatalf("record 0: %+v", got[0])
	}
	for i, u := range []uint16{0x68, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0x21} {
		if got[0].Caption[i] != u {
			t.Fatalf("record 0 caption unit %d = %#x", i, got[0].Caption[i])
		}
	}
	if got[1].CaptionLength != 5 || got[1].LabelLength != 0 || got[1].Inner.TextLength != 0 || got[1].Seq != 1000 {
		t.Fatalf("record 1: %+v", got[1])
	}
	for i, u := range []uint16{0xE000, 0xD83D, 0xDE00, 0xFFFF, 0x7A} {
		if got[1].Caption[i] != u {
			t.Fatalf("record 1 caption unit %d = %#x, want %#x", i, got[1].Caption[i], u)
		}
	}

	// THE FORGE: a length legal in BYTES (16 == the whole span) but past the
	// code-unit cap (8). A leg counting bytes admits it and lands 16; the unit
	// cap clamps to 8 and counts exactly once.
	forged := append([]byte(nil), buf...)
	f1 := forged[len(forged)-rec+8:]
	binary.LittleEndian.PutUint32(f1[wide.Src:], 16)
	got = make([]FxWide, 2)
	rep = TableReport{}
	if n := FxWideFixedLoad(got, forged, plan, &rep); n != 2 {
		t.Fatalf("forged load %d %+v", n, rep)
	}
	if got[1].CaptionLength != 8 || rep.Clamped != 1 {
		t.Fatalf("CODE UNITS: a byte-legal length 16 on wstring(8) landed length %d, clamped=%d; want 8 and exactly one clamp", got[1].CaptionLength, rep.Clamped)
	}
}
`)
}
