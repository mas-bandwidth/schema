package gotable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/ir"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

// TestHashIncludesTheCountWord pins §5 of docs/FIXED-FORM-ALGORITHM.md's WIRE
// IDENTITY by name, where the golden corpus only makes the handed constants
// AGREE: "the layout hash is fnv1a64 over the layout's bytes as written, the
// 4-byte count included". Every leg holds its hash as a CONSTANT and the
// runtime is right not to recompute it (§5.9 #47), so nothing on this leg
// computes the hash from the bytes it seals — a leg whose hash function skipped
// the count word would still pass every fixture it has, because it never
// computes the hash at all. That is the `~`: the behaviour is exercised, the
// rule is asserted by nothing.
//
// THIS CASE DOES COMPUTE IT, IN THE FIXTURE. It writes a file, lifts the layout
// AS WRITTEN out of that file (the layout's own first four bytes ARE its entry
// count), hashes those bytes with fnv1a64 written out LONGHAND — never borrowed
// from the code under test, which would "agree with whatever that code happened
// to do" — and asserts the header's OWN hash equals the number. It then drops
// the 4-byte count and asserts the number MOVES. TWO tables, so the case is not
// one lucky constant, and their hashes differ.
//
// The name is chosen to match `make tables-go-versioning`'s -run list, so the
// gate that carries the fixed form actually runs it.
func TestHashIncludesTheCountWord(t *testing.T) {
	// The generated case is run with -v and its log RE-EMITTED, so a plain
	// `go test -v -run TestHashIncludesTheCountWord` shows the inner
	// `--- PASS` and not merely the outer wrapper: a case nobody can watch run
	// is worth nothing.
	out, err := runGeneratedResult(t, `package probe
fixed table Point {
    x int32
    y int32
}
fixed table Quad {
    a int32
    b int32
    c int32
    d int32
}
`, `package probe

import (
	"encoding/binary"
	"testing"
)

// fnv1a64 is §5's hash written out LONGHAND in the fixture: a driver that
// hashed with the code it is checking "would agree with whatever that code
// happened to do". Here it is the oracle, not an echo.
func fnv1a64(b []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, v := range b {
		h ^= uint64(v)
		h *= 0x100000001b3
	}
	return h
}

func TestHashIncludesTheCountWord(t *testing.T) {
	cases := []struct {
		name   string
		layout []byte
		hash   uint64
		file   []byte
	}{
		{"Point", PointFixedLayout, PointFixedHash, func() []byte {
			f := make([]byte, PointFixedMeasure(1))
			PointFixedSave([]Point{{}}, f)
			return f
		}()},
		{"Quad", QuadFixedLayout, QuadFixedHash, func() []byte {
			f := make([]byte, QuadFixedMeasure(1))
			QuadFixedSave([]Quad{{}}, f)
			return f
		}()},
	}
	for _, tc := range cases {
		// THE FILE §2.1: form byte, seven reserved zeros, the layout hash at 8,
		// the body at 16, then the layout behind its u32 length — so the layout
		// starts at TableFixedHeaderBytes+4, and its OWN first four bytes are
		// its entry count.
		at := TableFixedHeaderBytes + 4
		asWritten := tc.file[at : at+len(tc.layout)]
		if got := binary.LittleEndian.Uint32(asWritten); got != uint32((len(tc.layout)-4)/17) {
			t.Fatalf("%s: the layout as written does not open with its 4-byte entry count: %d", tc.name, got)
		}
		// THE HEADER'S OWN HASH, taken from the file, is this build's constant.
		if onWire := binary.LittleEndian.Uint64(tc.file[TableFixedHashAt:]); onWire != tc.hash {
			t.Fatalf("%s: the header's own hash 0x%016x is not this build's constant 0x%016x", tc.name, onWire, tc.hash)
		}
		// THE RULE, BY NAME: the layout hash is fnv1a64 over the layout's bytes
		// as written, the 4-byte count included.
		want := fnv1a64(asWritten)
		if tc.hash != want {
			t.Fatalf("%s: the layout hash is fnv1a64 over the layout's bytes as written, the 4-byte count included: constant 0x%016x, recomputed over the %d bytes as written 0x%016x", tc.name, tc.hash, len(asWritten), want)
		}
		// AND THE COUNT IS IN THE INPUT: dropping it must move the number, or
		// "the count is included" is asserted by nothing.
		if dropped := fnv1a64(asWritten[4:]); dropped == tc.hash {
			t.Fatalf("%s: dropping the 4-byte count leaves the hash at 0x%016x, so the count is not in the input", tc.name, dropped)
		}
	}
	if cases[0].hash == cases[1].hash {
		t.Fatalf("two distinct layouts share the hash 0x%016x; the case is one lucky constant", cases[0].hash)
	}
}
`, "-v")
	if err != nil {
		t.Fatalf("the layout hash must include the 4-byte count: %v\n%s", err, out)
	}
	t.Logf("the generated case ran:\n%s", out)
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

// TestFixedFormHashCheckedLast is the row "hash checked LAST" (docs/FIXED-FORM-
// ALGORITHM.md §2's load order, §5.3 steps 3 to 5): the header's hash is TAKEN
// AS GIVEN and never recomputed from the wire, AND the layout's own framing is
// judged BEFORE the hash is looked up, so a file whose u32 layout length
// overruns it answers `layout_malformed` even when its header hash is one this
// reader has never locked. A reader that consulted the hash first would answer
// `layout_newer` and bury the framing damage — the order is the property, not
// the two individual names (§5.3: "The order is load-bearing").
//
// The reference states the same pair by hand: test/tables/fixedform_main.cpp's
// negative_control() flips the header hash and demands `layout_newer`
// (312-325), and layout_validation()'s residue builds a file whose length runs
// past its bytes under an unlocked hash and demands `layout_malformed`
// (519-527). This is that reasoning in the Go leg's generated-probe idiom; the
// taken-as-given half is also the generator's own §5.3 shape (fixedform.go:750).
func TestFixedFormHashCheckedLast(t *testing.T) {
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("encoding/binary"; "testing")

func TestHashCheckedLast(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointFixedMeasure(1)
	buf := make([]byte, need)
	if n := PointFixedSave([]Point{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	plan := make([]TableFixedEntry, 64)
	got := make([]Point, 1)
	var r TableReport

	// CONTROL 1 — THE RULE'S LEGITIMATE VALUE: the file's own, known hash. The
	// same loader reads it, so the refusals below are the hash and the length
	// and not the fixture.
	if n := PointFixedLoad(got, buf, plan, &r); n != 1 || r != (TableReport{}) || got[0] != one {
		t.Fatalf("the file with its own hash reads: n=%d got=%+v report=%+v", n, got[0], r)
	}

	// THE HASH IS TAKEN AS GIVEN, NEVER RECOMPUTED. A hash no lineage entry
	// holds is layout_newer, and the report carries THE FILE'S hash — the
	// flipped value, not this build's. A reader that recomputed hash_of(layout)
	// would find its own entry and read the file.
	unknown := append([]byte(nil), buf...)
	flipped := binary.LittleEndian.Uint64(unknown[TableFixedHashAt:]) ^ 0xFFFFFFFFFFFFFFFF
	binary.LittleEndian.PutUint64(unknown[TableFixedHashAt:], flipped)
	r = TableReport{}
	if n := PointFixedLoad(got, unknown, plan, &r); n != -1 || r.Reason != "layout_newer" || r.LayoutHash != flipped || r.Malformed {
		t.Fatalf("an unknown header hash is layout_newer carrying the file's hash: n=%d report=%+v", n, r)
	}

	// AND THE LAYOUT FRAMING IS JUDGED FIRST (§5.3 step 3 before steps 4-5): the
	// u32 length at 16 overruns the file, so the answer is layout_malformed even
	// though the header's hash is one this reader has never locked.
	overrun := append([]byte(nil), unknown...)
	binary.LittleEndian.PutUint32(overrun[TableFixedHeaderBytes:], uint32(len(overrun)))
	r = TableReport{}
	if n := PointFixedLoad(got, overrun, plan, &r); n != -1 || r.Reason != "layout_malformed" || r.Malformed {
		t.Fatalf("the layout framing is checked BEFORE the hash: n=%d report=%+v", n, r)
	}
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

// TestFixedFormHostileByteSweep is P3 on the Go leg: the C++ reference's
// record-byte sweep (test/tables/fixedform_properties.cpp:522-638, "mutate
// every record byte, both paths") and its whole-file control's law
// (test/tables/fixedform_main.cpp:642-694): every byte of a form-3 record is
// poisoned six ways and the reader answers ONE OF THREE ways and never a
// fourth — a refusal by name, a malformed read, or a read that lands values.
// The fourth answer, at the heart of docs/FIXED-FORM-ALGORITHM.md §7 item 5
// ("a byte-flip fuzz over the whole file, under a sanitizer"), is a read that
// returns records while a ranged field rode past its declared bound. Go's
// slice indexing is the sanitizer here: an out-of-bounds step is a panic, and
// a wrong-but-in-bounds step is what this case fails on by name.
//
// THE SITE IS THE READ-SIDE BOUNDS PASS (docs/SPEC-TABLES.md §3.4), emitted as
// <T>FixedClamp straight-line over the storage the one read loop just wrote,
// and it is that pass — not the loop, which moves bytes and holds no bound
// ("NO PLAN OP CLAMPS") — that keeps a hostile byte in bound. Host declares no
// version lineage, so the eight-byte layout hash always names the baked
// identity plan and that plan's read is the path swept below; a poisoned hash
// byte is a named refusal before any record. The pass runs for whichever plan
// won, so it is one function over storage and either plan is held by it.
// CONTROL 2, below, disconnects the pass from the loop to prove it — and not
// some other check — is what holds the value back.
func TestFixedFormHostileByteSweep(t *testing.T) {
	runGenerated(t, `package probe
fixed table Host {
    small int32 = 5 | min = 0, max = 1000
    marks [..4]int32 | min = 0, max = 1000
    note ?int32 | min = 0, max = 1000
    tail int32 = 7
}
`, `package probe
import (
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"
)

// The six poisons the C++ reference sweeps (fixedform_properties.cpp:170).
var sweepPoison = [6]byte{0x00, 0x01, 0x02, 0x7f, 0x80, 0xff}

func sweepClean(t *testing.T) []byte {
	t.Helper()
	v := Host{}
	HostReset(&v)
	v.Small = 5
	v.MarksCount = 2
	v.Marks[0] = 10
	v.Marks[1] = 20
	v.NotePresent = true
	v.Note = 30
	v.Tail = 7
	buf := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{v}, buf); n != int64(len(buf)) {
		t.Fatalf("save wrote %d of %d", n, len(buf))
	}
	return buf
}

// sweepInBound is the property: a read that RETURNED values landed every
// ranged value inside its declared bound. A value past the bound here is the
// fourth answer, and the message says so.
func sweepInBound(t *testing.T, what string, v Host, r TableReport) {
	t.Helper()
	if v.Small < 0 || v.Small > 1000 {
		t.Fatalf("%s: a read REPORTED SUCCESS with small=%d out of bound, report %+v", what, v.Small, r)
	}
	if v.MarksCount < 0 || v.MarksCount > 4 {
		t.Fatalf("%s: a read REPORTED SUCCESS with marks_count=%d out of bound, report %+v", what, v.MarksCount, r)
	}
	for i := int64(0); i < int64(v.MarksCount); i++ {
		if v.Marks[i] < 0 || v.Marks[i] > 1000 {
			t.Fatalf("%s: a read REPORTED SUCCESS with marks[%d]=%d out of bound, report %+v", what, i, v.Marks[i], r)
		}
	}
	if v.NotePresent && (v.Note < 0 || v.Note > 1000) {
		t.Fatalf("%s: a read REPORTED SUCCESS with note=%d out of bound, report %+v", what, v.Note, r)
	}
}

func TestHostileByteSweep(t *testing.T) {
	clean := sweepClean(t)

	// CONTROL 1, in the case: the legitimate value the rule admits does not
	// fire. A clean file clamps nothing, refuses nothing and lands in bound.
	{
		got := make([]Host, 1)
		var r TableReport
		plan := make([]TableFixedEntry, 256)
		if n := HostFixedLoad(got, clean, plan, &r); n != 1 {
			t.Fatalf("clean read n=%d %+v", n, r)
		}
		if r.Verdict != TableOpenOk || r.Reason != "" || r.Malformed || r.Clamped != 0 {
			t.Fatalf("clean read fired: %+v", r)
		}
		sweepInBound(t, "clean", got[0], r)
	}

	// THE SWEEP: every byte of the record — the eight-byte layout hash and the
	// body behind it — poisoned six ways through HostFixedLoad, the production
	// entrypoint. Host carries no lineage, so a poisoned hash byte meets a
	// named refusal and a poisoned body byte is read by the baked identity plan
	// and then held to its bound by the one pass that runs for whichever plan.
	for at := 0; at < HostFixedRecordBytes; at++ {
		for p := 0; p < len(sweepPoison); p++ {
			hit := append([]byte(nil), clean...)
			hit[len(hit)-HostFixedRecordBytes+at] = sweepPoison[p]
			got := make([]Host, 1)
			var r TableReport
			plan := make([]TableFixedEntry, 256)
			n := HostFixedLoad(got, hit, plan, &r)
			what := fmt.Sprintf("record byte %d poison 0x%02x", at, sweepPoison[p])
			if n < 0 {
				named := r.Verdict == TableOpenRefused && r.Reason != ""
				if !named && !r.Malformed {
					t.Fatalf("%s: a negative read is NEITHER a named refusal NOR malformed: %+v", what, r)
				}
				if named && r.Malformed {
					t.Fatalf("%s: a refusal is also damage: %+v", what, r)
				}
				continue
			}
			if r.Verdict != TableOpenOk || r.Reason != "" || r.Malformed {
				t.Fatalf("%s: a read that returned records carried a refusal or damage: %+v", what, r)
			}
			sweepInBound(t, what, got[0], r)
		}
	}

	// CONTROL 2, the negative control that compiles: revert the CALLER and keep
	// the HELPER. The loop a plan drives is tableFixedRun and its copy op lands
	// small verbatim — NO PLAN OP CLAMPS (§3.4). Plant the top of int32 where
	// small lives, run only the identity plan's loop, and the value lands
	// raw: the fourth answer the sweep above proves never closes a real read.
	// Then run the helper the caller runs after the loop — HostFixedClamp —
	// and the SAME value is one clamp back inside its declared bound.
	{
		if HostFixedPlan.Count <= 0 {
			t.Fatal("CONTROL 2 FAILED: no identity plan")
		}
		plan := make([]TableFixedEntry, HostFixedPlan.Count)
		copy(plan, HostFixedPlan.Entries[:HostFixedPlan.Count])
		var v Host
		HostReset(&v)
		smallDst := unsafe.Offsetof(v.Small)
		small := -1
		for i := range plan {
			if plan[i].Op == tableFixedCopy && plan[i].Size == 4 && plan[i].Dst == uint32(smallDst) {
				small = i
				break
			}
		}
		if small < 0 {
			t.Fatalf("CONTROL 2 FAILED: the identity plan carries no copy of small at dst %d", smallDst)
		}
		poisoned := append([]byte(nil), clean...)
		body := poisoned[len(poisoned)-HostFixedRecordBytes+8:]
		binary.LittleEndian.PutUint32(body[plan[small].Src:], 2147483647)
		dst := tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v))
		var r TableReport
		tableFixedRun(plan, int32(len(plan)), body, dst, &r)
		if v.Small != 2147483647 {
			t.Fatalf("CONTROL 2 FAILED: the loop alone landed small=%d, not the planted out-of-bound value", v.Small)
		}
		HostFixedClamp(&v, &r)
		if v.Small != 1000 || r.Clamped < 1 {
			t.Fatalf("CONTROL 2 FAILED: the bounds pass did not bring small back to bound: small=%d clamped=%d", v.Small, r.Clamped)
		}
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

// TestGoFixedFormLayoutAndHashAreIrsByteForByte is this leg's §7 item 1 gate —
// the "dump identity of bytes" cell of §8: the emitted layout block and the
// hash over it, byte for byte and number for number, against the compiler's own
// walk, ir.TableFixedLayoutBytes and ir.TableFixedLayoutHash. ir is the one law
// every port renders (C++ included) and it produces BYTES, so it is the oracle,
// never a second backend.
//
// The Go backend does not consume ir.TableFixedWalkRoot; it re-derives the walk
// in fixedWalkRoot, serialises it through fixedLayoutBytes and hashes the
// result with ir.TableFixedLayoutHash. Two layouts that differ anywhere are two
// ports that never read each other's records — silently, as `no_layout`. A
// round trip proves only that this leg agrees with itself; this test holds the
// emitted XxxFixedLayout and XxxFixedHash constants against ir's answer, per
// root, so the Go leg's layout can never drift from the reference's.
func TestGoFixedFormLayoutAndHashAreIrsByteForByte(t *testing.T) {
	src := `package probe
fixed table Point {
    x int32
    y int32
}
fixed table Vector {
    a int64
    b float32
    name string(8)
    tags [4]int16
}
`
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	goLayout, goHash := goFixedLayouts(tableFile(files))
	if len(goLayout) == 0 {
		t.Fatal("the Go backend emitted no fixed-form layout at all for a unit of fixed tables — the emitter, not the fixture, is what broke")
	}

	want := map[string]string{}
	wantHash := map[string]string{}
	var order []string
	for _, st := range ir.TableFixedRoots(u) {
		if !ir.TableFixedEmitted(u, st) {
			continue
		}
		layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
		want[st.Name] = byteSpell(layout)
		wantHash[st.Name] = fmt.Sprintf("0x%016x", ir.TableFixedLayoutHash(layout, st))
		order = append(order, st.Name)
	}
	if len(want) == 0 {
		t.Fatal("ir names no fixed root in a unit of fixed tables — the fixture, not the emitter, is what broke")
	}

	compared := 0
	for _, name := range order {
		got, ok := goLayout[name]
		if !ok {
			t.Errorf("ir names %s a fixed root and the Go backend emits no layout for it", name)
			continue
		}
		compared++
		if want[name] != got {
			t.Errorf("%s's emitted layout differs from the compiler's own walk — the block-equals-the-C++-reference test (docs/FIXED-FORM-ALGORITHM.md §7 item 1, §8 \"dump identity of bytes\"): this leg's emitted layout AND its hash must equal the reference byte for byte and number for number\n  ir %s\n  go %s", name, want[name], got)
		}
		if wantHash[name] != goHash[name] {
			t.Errorf("%s's fixed-form hash differs from ir's: ir %s, go %s — a record written against the law is `no_layout` to this port", name, wantHash[name], goHash[name])
		}
	}
	for name := range goLayout {
		if _, ok := want[name]; !ok {
			t.Errorf("the Go backend emits a fixed-form layout for %s and ir does not name it a fixed root — a port may carry fewer shapes than the law, never more", name)
		}
	}
	if compared == 0 {
		t.Fatal("no root was compared against ir at all")
	}

	// CONTROL 1 — not vacuous: two distinct roots must produce two distinct
	// layout blocks. A byte-for-byte test that passes against one file only is
	// a golden file with extra steps.
	if len(order) >= 2 && want[order[0]] == want[order[1]] {
		t.Fatalf("two distinct roots (%s, %s) emitted the same layout block; the fixture does not distinguish the roots", order[0], order[1])
	}
}

var (
	goFixedLayoutRe = regexp.MustCompile(`(?s)var (\w+)FixedLayout = \[\]byte\{(.*?)\}`)
	goFixedHashRe   = regexp.MustCompile(`const (\w+)FixedHash = (0x[0-9a-f]+)`)
	fixedHexByteRe  = regexp.MustCompile(`0x[0-9a-f]{2}`)
)

// goFixedLayouts keys on the Go root's own spelling (st.Name), which is what
// the emitted XxxFixedLayout and XxxFixedHash constants carry.
func goFixedLayouts(text string) (map[string]string, map[string]string) {
	layouts, hashes := map[string]string{}, map[string]string{}
	for _, m := range goFixedLayoutRe.FindAllStringSubmatch(text, -1) {
		layouts[m[1]] = strings.Join(fixedHexByteRe.FindAllString(m[2], -1), " ")
	}
	for _, m := range goFixedHashRe.FindAllStringSubmatch(text, -1) {
		hashes[m[1]] = m[2]
	}
	return layouts, hashes
}

func byteSpell(b []byte) string {
	hex := make([]string, 0, len(b))
	for _, v := range b {
		hex = append(hex, fmt.Sprintf("0x%02x", v))
	}
	return strings.Join(hex, " ")
}

// TestFixedFormBytesIsLayoutKind14 is item W11 of schema#876: `bytes(N)` is
// walked as an ARRAY OF u8 on this wire — kind 14 with ONE synthetic child at
// kind 6, size 1 — and never as a text kind (docs/FIXED-FORM-ALGORITHM.md:54,
// the closed-kind table at :75, and the buffer row at :696-703: "`bytes(N)` is
// an ARRAY row (fix 10)"). A constant being USED is not the property being
// ASSERTED, so this reads back THE LAYOUT THE EMITTER BUILDS rather than
// matching a substring of generated Go: it asks fixedWalkRoot — the same call
// emitFixedRoot makes at fixedform.go:647 — for the entries of a table with one
// `bytes(6)` and one `string(6)`, finds the bytes field by its WIRE ID, and pins
// the ARRAY entry and its synthetic u8 child. The string field is the other row
// (kind 12), so the case tells the two apart and is not matching whatever entry
// it happens to find first.
func TestFixedFormBytesIsLayoutKind14(t *testing.T) {
	u := unitFrom(t, `package probe
fixed table Carrier {
    tag bytes(6)
    label string(6)
}
`)
	carrier := u.Tables["Carrier"]
	if carrier == nil {
		t.Fatal("the fixed table Carrier is not in the unit's tables")
	}
	w := (&tableGen{}).fixedWalkRoot(carrier)

	tagID := ir.TableWireId("tag")
	labelID := ir.TableWireId("label")
	at, labelAt := -1, -1
	for i, e := range w.entries {
		switch e.id {
		case tagID:
			at = i
		case labelID:
			labelAt = i
		}
	}
	if at < 0 {
		t.Fatalf("the bytes(6) field is not in the layout under its wire id; entries: %+v", w.entries)
	}
	if e := w.entries[at]; e.kind != ir.TableKindArray {
		t.Fatalf("bytes(6) is layout kind %d, want %d (an array of u8)", e.kind, ir.TableKindArray)
	}
	if e := w.entries[at]; e.children != 1 {
		t.Fatalf("bytes(6) carries %d children, want the one synthetic u8", e.children)
	}
	if at+1 >= len(w.entries) {
		t.Fatal("bytes(6)'s synthetic element is not the entry after it")
	}
	if c := w.entries[at+1]; c.kind != ir.TableKindU8 || c.size != 1 || c.children != 0 {
		t.Fatalf("bytes(6)'s element is kind %d size %d children %d, want the synthetic u8 (6, 1, 0)", c.kind, c.size, c.children)
	}
	if labelAt < 0 {
		t.Fatal("the string(6) field is not in the layout under its wire id")
	}
	if e := w.entries[labelAt]; e.kind != ir.TableKindString {
		t.Fatalf("string(6) is layout kind %d, want %d; the bytes row and the text row are distinct", e.kind, ir.TableKindString)
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

// TestFixedFormLayoutMalformedTruncated is the row layout_malformed, truncated,
// item F4 of schema#876: a GAP on this leg. A file whose header DECLARES a
// layout LONGER than the bytes it carries is refused BY NAME — layout_malformed
// — and is not the `malformed` residue its siblings answer with: F7 is a file
// under the twenty-byte header, F8 a record region that is not whole records,
// both of which set report.Malformed and leave Reason UNTOUCHED. The
// declaration is the u32 at TableFixedHeaderBytes, and the law is
// docs/FIXED-FORM-ALGORITHM.md §2's load table step 3: `L := LE(4, b+16)`; if
// `20 + L > bytes`, `REFUSE layout_malformed`. The site is the step-3 guard
// emitted at fixedform.go:745. The pointer is test/tables/fixedform_main.cpp's
// residue case inside layout_validation() (lines 519-527): a twenty-byte file
// whose length word names a hundred absent bytes is `layout_malformed`,
// "NOTHING WAS DECODED AND NOTHING WAS COUNTED" (refuses(), lines 391-394).
// The destination is filled with a sentinel Point and proved still the
// sentinel, so "no byte is written" is observed rather than assumed; the whole
// report is compared to TableReport{Verdict: TableOpenRefused, Reason:
// "layout_malformed"} in one !=, pinning every counter, Malformed and any field
// added later to its zero value at once. CONTROL 1 is the unbroken file reading
// clean, so the refusals are the forged word and not the fixture. CONTROL 2
// removes the step-3 guard and the row must go RED.
func TestFixedFormLayoutMalformedTruncated(t *testing.T) {
	runGenerated(t, `package probe
fixed table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("testing")

func TestLayoutMalformedTruncated(t *testing.T) {
	one := Point{X: 4242, Y: -7}
	need := PointFixedMeasure(1)
	buf := make([]byte, need)
	if n := PointFixedSave([]Point{one}, buf); n != need {
		t.Fatalf("save %d", n)
	}
	got := make([]Point, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 64)

	// CONTROL 1: the unbroken file reads clean.
	if n := PointFixedLoad(got, buf, plan, &r); n != 1 || r != (TableReport{}) || got[0] != one {
		t.Fatalf("the good file does not read back: n=%d got=%+v report=%+v", n, got[0], r)
	}

	// THE REFERENCE'S FILE, byte for byte: twenty bytes of header, its u32
	// length word naming 100 bytes the file does not carry.
	ref := make([]byte, TableFixedHeaderBytes+4)
	ref[0] = 3
	tableFixedPut32(ref[TableFixedHeaderBytes:], 100)
	got = []Point{{X: -1, Y: -1}}
	r = TableReport{}
	if n := PointFixedLoad(got, ref, plan, &r); n != -1 || r != (TableReport{Verdict: TableOpenRefused, Reason: "layout_malformed"}) || got[0] != (Point{X: -1, Y: -1}) {
		t.Fatalf("declared length 100 in a twenty-byte file: n=%d report=%+v dest=%+v", n, r, got[0])
	}

	// AND A REAL FILE TRUNCATED, so its OWN declared layout no longer fits: the
	// length word is correct and the bytes are withdrawn from under it. This is
	// the same arm reached from the other side, and the case a leg cannot pass
	// by comparing the layout to the lock's, because the word is this build's.
	short := buf[:TableFixedHeaderBytes+4+int(tableFixedGet32(buf[TableFixedHeaderBytes:]))-1]
	got = []Point{{X: -1, Y: -1}}
	r = TableReport{}
	if n := PointFixedLoad(got, short, plan, &r); n != -1 || r != (TableReport{Verdict: TableOpenRefused, Reason: "layout_malformed"}) || got[0] != (Point{X: -1, Y: -1}) {
		t.Fatalf("a file truncated one byte into its own layout: n=%d report=%+v dest=%+v", n, r, got[0])
	}
}
`)
}

// THE TEXT LENGTH CLAMP (roadmap go/C3, the task node whose title is quoted here
// verbatim: "text length clamp"). docs/FIXED-FORM-ALGORITHM.md §4.5 states the
// rule for the `text` op:
//
//	"the length (i32 LE) ... clamped into [0, cap], COUNT clamped if it fired"
//
// and §4.5's table gives the cap: "the payload's BYTE span, min(mine, theirs)",
// so the cap is the span read in UNITS — `cap := size / unit`. The reader keeps
// this check in every build, release included, and a clamp counts ONCE for the
// field. The forged word is read as a SIGNED i32, so an all-ones word is -1 and
// clamps UP to zero; a word past the cap clamps DOWN to it.
//
// THE THREE VALUES ARE THE THREE FACTS: exactly the bound is ADMITTED and counts
// nothing (the case must not be vacuous), one past it clamps to the bound and
// counts one, and the all-ones word clamps to zero and counts one. The forged
// field is bracketed by `lead` and `trail`, so a clamp that mislaid the size
// moves a neighbour and this says so.
//
// THE SITE IS fixedruntime.go's `case tableFixedText`: `capn := p.Size / unit`
// then `if v < 0 { v = 0; report.Clamped++ } else if uint32(v) > capn { v =
// int32(capn); report.Clamped++ }`. DISABLING that `else if` leaves the forged
// length standing and the counters silent — a silently WRONG REPORT, measured
// in CONTROL 2 below.
func TestFixedFormTextLengthClamp(t *testing.T) {
	runGenerated(t, `package probe
fixed table Text {
    lead  uint32 = 1
    label string(8)
    trail uint32 = 2
}
`, `package probe
import ("bytes"; "encoding/binary"; "testing")

func TestTextLengthClamp(t *testing.T) {
	one := Text{}
	TextReset(&one)
	one.Lead = 0xAAAAAAAA
	copy(one.Label[:], "abcdefgh")
	one.LabelLength = 8
	one.Trail = 0xBBBBBBBB
	need := TextFixedMeasure(1)
	base := make([]byte, need)
	if n := TextFixedSave([]Text{one}, base); n != need {
		t.Fatalf("save %d want %d", n, need)
	}
	needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0x08, 0x00, 0x00, 0x00,
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h'}
	at := bytes.Index(base, needle)
	if at < 0 {
		t.Fatalf("text_length_clamp: the lead + length 8 + \"abcdefgh\" needle is not in the file")
	}
	if bytes.Index(base[at+1:], needle) >= 0 {
		t.Fatalf("text_length_clamp: the needle occurs more than once, so the search is not a locator")
	}
	// base[at+4:] is the length word. The cap is the field's own bound, 8.
	cases := []struct {
		name        string
		forge       uint32
		want        uint32
		wantClamped int32
	}{
		{"at the bound: admitted, no clamp", 8, 8, 0},
		{"one past the bound: clamped down", 9, 8, 1},
		{"all ones reads signed -1: clamped up to zero", 0xFFFFFFFF, 0, 1},
	}
	for _, c := range cases {
		buf := append([]byte(nil), base...)
		binary.LittleEndian.PutUint32(buf[at+4:], c.forge)
		got := make([]Text, 1)
		var r TableReport
		plan := make([]TableFixedEntry, 256)
		n := TextFixedLoad(got, buf, plan, &r)
		if n != 1 {
			t.Fatalf("%s: the forged file reads one record, n=%d %+v", c.name, n, r)
		}
		if got[0].LabelLength != int32(c.want) {
			t.Fatalf("%s: the length landed %d, not %d (never the forged %d): %+v",
				c.name, got[0].LabelLength, c.want, c.forge, got[0])
		}
		if c.want > 0 && string(got[0].Label[:c.want]) != "abcdefgh" {
			t.Fatalf("%s: the used bytes landed %q, not \"abcdefgh\": %+v",
				c.name, got[0].Label[:c.want], got[0])
		}
		if c.want == 0 && got[0].Label[0] != 0 {
			t.Fatalf("%s: the terminator did not zero the used length: %+v", c.name, got[0])
		}
		if r.Clamped != c.wantClamped {
			t.Fatalf("%s: Clamped=%d, want %d — once per field: %+v", c.name, r.Clamped, c.wantClamped, r)
		}
		if got[0].Lead != 0xAAAAAAAA || got[0].Trail != 0xBBBBBBBB {
			t.Fatalf("%s: the clamp moved a neighbour: %+v", c.name, got[0])
		}
		if r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 {
			t.Fatalf("%s: a counter other than Clamped moved: %+v", c.name, r)
		}
		if r.Malformed || r.Verdict == TableOpenRefused || r.Reason != "" {
			t.Fatalf("%s: a forged length reads clean, not a refusal: %+v", c.name, r)
		}
	}
}
`)
}

// TestFixedFormOptionalVersusPlainNesting is item E5 of schema#876, "?T vs
// plain nesting": the fixed form's one deliberate departure from §2.3. On form
// 1 a `?T` and a plain `T` nesting are wire-identical; HERE they are one byte
// apart, so the layout carries kind 35 against the plain nesting's kind 13
// (docs/FIXED-FORM-ALGORITHM.md §2.2, the `?T` row: "the present flag THEN the
// payload, which rides WHOLE"). A reader that declares `?OptLeaf` given a
// writer that sent `OptLeaf` bare owes `present == 1` and the payload exact
// (bill §12.8, docs/FIXED-FORM-VERSIONING-TESTS.md's `optional_add` row:
// "present == 1, value exact"): the present byte is a CONSTANT the plan emits
// (fixedruntime.go's `me.Kind == 35 && te.Kind != 35`), NOT the fresh value's
// false. The old writer is this build's own writer, so the file is lawful by
// construction and nothing here reads the C++ corpus; the reader takes the OLD
// unit's locked entry as its lineage, which is the only way a versioned read is
// reached now (docs/FIXED-FORM-ALGORITHM.md §5.6).
func TestFixedFormOptionalVersusPlainNesting(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "optional_add.bin")
	old := readSchema(t, "VOLD_optional_add")
	newer := readSchema(t, "VNEW_optional_add")
	// STAGE ONE: the plain-nesting writer writes its own file, 777 in the
	// payload and the 0xA/0xB brackets around it so a mislaid wrapper size
	// moves a neighbour.
	writeSrc := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestWrite(t *testing.T) {
	var v OptionalAdd
	OptionalAddReset(&v)
	v.Lead = 0xAAAAAAAA
	v.Link.Value = 777
	v.Trail = 0xBBBBBBBB
	buf := make([]byte, OptionalAddFixedMeasure(1))
	if OptionalAddFixedSave([]OptionalAdd{v}, buf) < 0 {
		t.Fatal("save")
	}
	if err := os.WriteFile(%q, buf, 0o600); err != nil {
		t.Fatal(err)
	}
}
`, file)
	if out, err := runVersionProbe(t, old, nil, writeSrc); err != nil {
		t.Fatalf("the plain-nesting writer did not write its own file: %v\n%s", err, out)
	}
	// STAGE TWO: the ?T reader, handed the plain writer's lock as its lineage.
	readSrc := fmt.Sprintf(`package probe

import ("os"; "testing")

func TestRead(t *testing.T) {
	data, err := os.ReadFile(%q)
	if err != nil {
		t.Fatal(err)
	}
	back := make([]OptionalAdd, 2)
	plan := make([]TableFixedEntry, 4096)
	var r TableReport
	n := OptionalAddFixedLoad(back, data, plan, &r)
	if n != 1 || r.Reason != "" || r.Malformed || r.Verdict == TableOpenRefused {
		t.Fatalf("the ?T reader refused the plain writer's file: n=%%d %%+v", n, r)
	}
	if !back[0].LinkPresent {
		t.Fatalf("T into ?T owes present = 1, not the fresh value's false: %%+v", back[0])
	}
	if back[0].Link.Value != 777 {
		t.Fatalf("the payload beside the present companion is not the writer's value: %%+v", back[0])
	}
	if back[0].Lead != 0xAAAAAAAA || back[0].Trail != 0xBBBBBBBB {
		t.Fatalf("the row moved a neighbour: %%+v", back[0])
	}
	if r.KindMismatch != 0 || r.Unknown != 0 || r.Widened != 0 || r.Clamped != 0 || r.Duplicate != 0 {
		t.Fatalf("a supported T-into-?T widening is not an event: %%+v", r)
	}
}
`, file)
	if out, err := runVersionProbe(t, newer, []string{old}, readSrc); err != nil {
		t.Fatalf("the ?T reader failed its read: %v\n%s", err, out)
	}
}

// P4, "THE WRONG PLAN MUST GO RED" (docs/FIXED-FORM-ALGORITHM.md §7 item 4).
// The reference's negative_control() (test/tables/fixedform_main.cpp:234-260)
// runs FX1's identity plan over an FX2 record and requires it NOT reproduce
// the record, because FX2 inserts `added` and widens `narrow`, so every offset
// past them has moved. This leg's versioning suite reads older and newer
// records through the RIGHT plan and refuses a hash it does not lock, but
// nothing here ever watched the wrong plan fail — and a check that never
// watched the wrong plan fail never checked the right one. This case is the Go
// twin of the reference's: FxOld is the old shape, FxNew the new one, the SAME
// nested table under both.
//
// Two answers are asserted by name: (1) FxOld's identity plan run straight
// over FxNew's record body — the path select-by-hash exists to prevent — must
// NOT reproduce the record; (2) the loader refuses the file whose hash it does
// not lock with `layout_newer` and moves no counter. The legitimate value the
// rule admits (FxOld reading FxOld) is asserted too, so neither half can pass
// vacuously.
//
// The name matches the `tables-go-versioning` gate's own `-run` pattern
// (make/go.mk:557), which is the half of `make tables-go-fixed-form
// tables-go-versioning` that runs this package; a `TestFixedForm...` name here
// would not be reached by that target at all.
func TestFixedVersioningWrongPlanGoesRed(t *testing.T) {
	runGenerated(t, `package probe

fixed table FxNested
{
    a int32 = 1
    b int32 = 2
}

fixed table FxExtra
{
    x int32 = 0
    y int32 = 0
}

// THE OLD SHAPE, FX1's: a narrow field, and no field between renamed and
// the nested table.
fixed table FxOld
{
    keep    uint32 = 7
    narrow  uint16 = 3
    renamed int32 = 5
    gone    int32 = 9
    nested  FxNested
}

// THE NEW SHAPE, FX2's: narrow widened, added inserted before the nested
// table, and a whole nested TYPE FxOld has no name for.
fixed table FxNew
{
    keep       uint32 = 7
    narrow     uint32 = 3
    renamed_to int32 = 5
    added      int32 = 11
    nested     FxNested
    extra      FxExtra
}
`, `package probe

import (
    "testing"
    "unsafe"
)

func TestWrongPlanGoesRed(t *testing.T) {
    two := FxNew{}
    FxNewReset(&two)
    two.Keep = 5150
    two.Narrow = 70000
    two.RenamedTo = 808
    two.Added = 909
    two.Nested.A = 33
    two.Nested.B = 44
    w2 := make([]byte, FxNewFixedMeasure(1))
    if n := FxNewFixedSave([]FxNew{two}, w2); n != int64(len(w2)) {
        t.Fatalf("FxNew save %d", n)
    }

    // (2) THE LOADER REFUSES. FxNew's record carries FxNew's hash in the
    // header at offset 8; FxOld's lock holds only FxOld's hash, so FxOld's
    // plan is never entered and the file is layout_newer.
    plan := make([]TableFixedEntry, 1024)
    var r TableReport
    old := make([]FxOld, 1)
    if n := FxOldFixedLoad(old, w2, plan, &r); n >= 0 || r.Reason != "layout_newer" {
        t.Fatalf("the loader did not refuse a file under another layout: n=%d %+v", n, r)
    }
    if r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Malformed {
        t.Fatalf("layout_newer is a refusal and moves no counter: %+v", r)
    }

    // (1) THE WRONG PLAN IS WRONG. FxOld's identity plan over FxNew's record
    // body: renamed and the nested table sit at moved offsets, so the values
    // cannot survive. This is the same run the reference's negative_control()
    // makes with FX1's plan over an FX2 body.
    body := w2[len(w2)-FxNewFixedRecordBytes:]
    if int32(len(body)) != FxNewFixedRecordBytes {
        t.Fatalf("record body is %d bytes, want %d", len(body), FxNewFixedRecordBytes)
    }
    wrong := FxOld{}
    FxOldReset(&wrong)
    var rr TableReport
    tableFixedRun(FxOldFixedPlan.Entries, FxOldFixedPlan.Count, body[8:],
        tableFixedOverlay(unsafe.Pointer(&wrong), unsafe.Sizeof(wrong)), &rr)
    intact := wrong.Renamed == two.RenamedTo &&
        wrong.Nested.A == two.Nested.A && wrong.Nested.B == two.Nested.B
    if intact {
        t.Fatalf("NEGATIVE CONTROL FAILED: FxOld's identity plan reproduced an FxNew record: %+v", wrong)
    }

    // THE LEGITIMATE VALUE THE RULE ADMITS: FxOld's own record reads back
    // whole under FxOld's own hash, so neither half above is vacuous.
    one := FxOld{}
    FxOldReset(&one)
    one.Keep = 4242
    one.Narrow = 40000
    one.Renamed = 321
    one.Gone = 654
    one.Nested.A = 111
    one.Nested.B = 222
    w1 := make([]byte, FxOldFixedMeasure(1))
    if n := FxOldFixedSave([]FxOld{one}, w1); n != int64(len(w1)) {
        t.Fatalf("FxOld save %d", n)
    }
    back := make([]FxOld, 1)
    r = TableReport{}
    if n := FxOldFixedLoad(back, w1, plan, &r); n != 1 ||
        back[0].Renamed != 321 || back[0].Nested.A != 111 || r != (TableReport{}) {
        t.Fatalf("the right plan must reproduce its own record: n=%d %+v %+v", n, back[0], r)
    }
}
`)
}

// TestFixedFormAbsentOptionalSkipsStore is W2, asserted by name: an absent
// optional writes flag 0 and SKIPS THE PAYLOAD STORE
// (docs/FIXED-FORM-ALGORITHM.md §3.1, fix 13). The payload rides WHOLE whether
// or not it is present, and when the flag is 0 what rides is the TEMPLATE'S
// ZEROS — an absent optional is a hole in the record and not a window into the
// writer's memory. The C++ reference pins the same rule in
// test/tables/fixedform_main.cpp:1401 `absent_optional_case`, and the C leg at
// test/c-tables/fixedform_v1.c:123 `fixed_v1_absent_optional`.
//
// THE CONTROL IS THE STAIN, as it is for every other kind of slack: the payload
// storage is filled with a byte a clean record carries nowhere (0x5A), the
// test proves the stain IS in storage, then proves THE WIRE CARRIES NONE OF
// IT, and then proves the SAME payload PRESENT does put those bytes on the
// wire — so the check is about the flag and not about the writer never having
// written a payload at all.
func TestFixedFormAbsentOptionalSkipsStore(t *testing.T) {
	runGenerated(t, `package probe
fixed table Link
{
    value int32 = 0 | min = 0, max = 1000
    tag   string(8)
}

fixed table Chain
{
    name string(16)
    link ?Link
}
`, `package probe
import ("bytes"; "testing")

func TestAbsentOptionalSkipsStore(t *testing.T) {
	one := Chain{}
	ChainReset(&one)
	copy(one.Name[:], "absent")
	one.NameLength = 6
	one.LinkPresent = false
	one.Link.Value = 0x5A5A5A
	for i := range one.Link.Tag {
		one.Link.Tag[i] = 0x5A
	}
	if one.Link.Value != 0x5A5A5A || one.Link.Tag[0] != 0x5A {
		t.Fatal("CONTROL: the absent payload really is stained in storage")
	}

	need := ChainFixedMeasure(1)
	buf := make([]byte, need)
	if n := ChainFixedSave([]Chain{one}, buf); n != need {
		t.Fatalf("absent optional: the record saves %d want %d", n, need)
	}
	body := buf[len(buf)-ChainFixedRecordBytes+8:]
	if hits := bytes.Count(body, []byte{0x5A}); hits != 0 {
		t.Fatalf("ABSENT OPTIONAL: %d bytes of the absent payload reached the wire", hits)
	}

	// and the reader reads what the flag says, with the payload at the
	// template's zeros
	back := make([]Chain, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := ChainFixedLoad(back, buf, plan, &r); n != 1 {
		t.Fatalf("absent optional: the record reads %d %+v", n, r)
	}
	if back[0].LinkPresent {
		t.Fatal("absent optional: the flag")
	}
	if back[0].Link.Value != 0 || back[0].Link.TagLength != 0 {
		t.Fatalf("absent optional: the payload reads %d/%d, not the wire's zeros", back[0].Link.Value, back[0].Link.TagLength)
	}
	if r != (TableReport{}) {
		t.Fatalf("absent optional: a clean read moves no counter: %+v", r)
	}

	// THE DISCRIMINATING HALF: the same payload, PRESENT. Those bytes do reach
	// the wire, so the check above is about the flag and not about the writer
	// never having written a payload.
	present := one
	present.LinkPresent = true
	present.Link.TagLength = 4
	pbuf := make([]byte, ChainFixedMeasure(1))
	if n := ChainFixedSave([]Chain{present}, pbuf); n != int64(len(pbuf)) {
		t.Fatalf("absent optional: the present twin saves %d", n)
	}
	pbody := pbuf[len(pbuf)-ChainFixedRecordBytes+8:]
	if bytes.IndexByte(pbody, 0x5A) < 0 {
		t.Fatal("NEGATIVE CONTROL: the SAME payload PRESENT did NOT reach the wire")
	}
}
`)
}

// A `bytes(N)` DESTINATION ROW IS AN ARRAY'S (docs/FIXED-FORM-ALGORITHM.md §4.1,
// docs/SPEC-TABLES.md §3.4). `bytes(N)` rides this form as an array of u8, so
// the plan compiler lands it through the ARRAY case: the count goes to the
// row's aux and the elements to the row's dst. A text field's row is the other
// way round — dst the length, aux the buffer — and under that convention a
// `bytes(N)` gets a count destination that is the BUFFER'S FIRST FOUR BYTES and
// an element destination that is the LENGTH FIELD. Identity lands the field
// with one TEXT op and reads neither column, so only the compiled path ever saw
// it. This is the Go port of the C++ reference's bytes_row_case
// (test/tables/fixedform_main.cpp:781-854), whose negative control is the OLD
// row: the two columns swapped on a copy of this build's own rows.
func TestFixedFormBytesNTakesTheArrayRow(t *testing.T) {
	runGenerated(t, `package probe
fixed table Probe
{
    label string(8)
    blob  bytes(6)
    marks [..4]int32
}
`, `package probe
import ("testing";"unsafe")

func TestBytesRow(t *testing.T) {
    one := Probe{}
    ProbeReset(&one)
    copy(one.Label[:], []byte("abc")); one.LabelLength = 3
    one.Blob[0] = 0xDE; one.Blob[1] = 0xAD; one.Blob[2] = 0xBE; one.Blob[3] = 0xEF
    one.BlobLength = 4
    one.Marks[0] = 11; one.Marks[1] = 22; one.MarksCount = 2
    w := make([]byte, ProbeFixedMeasure(1))
    if n := ProbeFixedSave([]Probe{one}, w); n != int64(len(w)) { t.Fatalf("save %d", n) }

    theirs, why := tableFixedParseLayout(ProbeFixedLayout)
    if why != "" { t.Fatalf("layout: %s", why) }

    // THE ROW IS FOUND BY ITS SHAPE and not by its index — stride one and a
    // live count is a bytes(N) and nothing else — so the control does not
    // quietly stop pointing at it the day a field moves.
    swapped := append([]TableFixedDst(nil), ProbeFixedDst...)
    found := -1
    for i := range swapped {
        if swapped[i].Stride == 1 && swapped[i].Counted != 0 { found = i; break }
    }
    if found < 0 { t.Fatal("bytes row: no row with stride one and a live count") }
    d := swapped[found].Dst
    swapped[found].Dst = swapped[found].Aux // the TEXT convention, as it was
    swapped[found].Aux = d

    body := w[TableFixedHeaderBytes+4+len(ProbeFixedLayout)+8:]
    for pass := 0; pass < 2; pass++ {
        rowset := ProbeFixedDst
        if pass == 1 { rowset = swapped }
        plan := make([]TableFixedEntry, 256)
        var c TableReport
        made := tableFixedCompile(theirs, ProbeFixedLayout, rowset, plan, &c)
        if made <= 0 { t.Fatalf("bytes row: the plan compiles either way, made %d %+v", made, c) }
        var back Probe
        ProbeReset(&back)
        r := TableReport{}
        tableFixedRun(plan, made, body, tableFixedOverlay(unsafe.Pointer(&back), unsafe.Sizeof(back)), &r)
        right := back.BlobLength == 4 && back.Blob[0] == 0xDE && back.Blob[1] == 0xAD && back.Blob[2] == 0xBE && back.Blob[3] == 0xEF
        if pass == 0 {
            if !right { t.Fatalf("BYTES(N): the array row must land the buffer in the buffer and the length in the length: %+v", back) }
        } else if right {
            t.Fatal("NEGATIVE CONTROL: the text row still landed the bytes; the old row was not the control")
        }
    }
}
`)
}

// TestFixedFormBoundsShiftFixedAndBits is C8 "fixed-point F-shift / bits(N)",
// asserted BY NAME against its own clause (docs/FIXED-FORM-ALGORITHM.md §4.6):
// "A fixed-point field's bounds are in VALUE UNITS and its storage is raw, so
// both ends are shifted by F first; a `bits(N)` clamps to `2^N - 1`." The
// emitter's numbers are ir.TableRawRange's — the declared whole-unit bounds
// Lsh(F) — and the bits branch clamps at the N-bit mask, NOT at the storage
// width. A poisoned raw rides the writer (the write side's bounds are
// debug-only and a range is not one of them) and the read-side pass over
// STORAGE is what holds it. The legitimate value the rule admits is the
// CONTROL and must not move the counter.
func TestFixedFormBoundsShiftFixedAndBits(t *testing.T) {
	runGenerated(t, `package probe
fixed table Probe {
    tilt fixed(12, 4) | min = -8, max = 7
    mask bits(12)
}
`, `package probe
import ("testing")

func oneRound(t *testing.T, in Probe) (Probe, TableReport) {
	t.Helper()
	need := ProbeFixedMeasure(1)
	buf := make([]byte, need)
	if n := ProbeFixedSave([]Probe{in}, buf); n != need {
		t.Fatalf("save %d want %d", n, need)
	}
	got := make([]Probe, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := ProbeFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("load %d %+v", n, r)
	}
	return got[0], r
}

func TestShiftAndBits(t *testing.T) {
	// CONTROL 1 (not vacuous): the legitimate value the rule admits — the
	// declared max shifted onto the raw scale (7<<4 = 112) and the bits mask
	// (2^12-1 = 4095) — must NOT fire.
	var ok Probe
	ProbeReset(&ok)
	ok.Tilt = 7 << 4
	ok.Mask = 1<<12 - 1
	clean, r := oneRound(t, ok)
	if clean.Tilt != 112 || clean.Mask != 4095 || r.Clamped != 0 {
		t.Fatalf("CONTROL: an admitted value moved: tilt=%d mask=%d clamped=%d", clean.Tilt, clean.Mask, r.Clamped)
	}

	// POISON: raw storage past both bounds, through the writer.
	var bad Probe
	ProbeReset(&bad)
	bad.Tilt = 200  // raw > 112
	bad.Mask = 5000 // raw > 4095
	held, r := oneRound(t, bad)
	if held.Tilt != 112 {
		t.Fatalf("F-SHIFT: fixed tilt landed %d, want 112 = max 7 shifted by F=4", held.Tilt)
	}
	if held.Mask != 4095 {
		t.Fatalf("BITS: mask landed %d, want 4095 = 2^12-1 (not the 32-bit storage top)", held.Mask)
	}
	if r.Clamped != 2 {
		t.Fatalf("clamped=%d, want 2 (one per poisoned field)", r.Clamped)
	}
}
`)
}

// TestFixedFormDuplicateNeverRaised pins E9 (schema#876): the fixed form's §4
// counter set is `unknown`, `kind_mismatch`, `widened`, `clamped`, `malformed`
// — "this form raises all but `duplicate`" (docs/FIXED-FORM-ALGORITHM.md §4,
// and §29: "`duplicate` is the TEXT form's — the fixed wire never raises it").
// `duplicate` is the TEXT form's counter for a repeated map key; the fixed form
// refuses a map field outright (ir.TableFixedSupported) and its only keyed
// structure, the enum-keyed array, is slot-indexed by variant with no key on the
// wire — so a fixed read must leave `Duplicate` at zero. The Go TableReport
// carries a `Duplicate` member (shared across forms, gotable.go), so this
// fixture asserts the counters that EXIST on this leg's report, by name, on
// both the identity plan and the compiled plan path.
func TestFixedFormDuplicateNeverRaised(t *testing.T) {
	runGenerated(t, `package probe
enum Key { a, b, c }
fixed table Child {
    n int32 = 0
}
fixed table Root {
    slots [Key]Child
    seq   int32 = 7
}
`, `package probe
import ("testing"; "unsafe")

func TestDuplicateStaysZero(t *testing.T) {
	one := Root{}
	RootReset(&one)
	one.Slots[0].N = 11
	one.Slots[1].N = 22
	one.Slots[2].N = 33
	one.Seq = 44
	buf := make([]byte, RootFixedMeasure(1))
	if n := RootFixedSave([]Root{one}, buf); n != int64(len(buf)) {
		t.Fatalf("save %d", n)
	}

	// IDENTITY PATH: the plan the emitter laid down.
	got := make([]Root, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := RootFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("identity n=%d %+v", n, r)
	}
	if got[0].Slots[0].N != 11 || got[0].Slots[2].N != 33 || got[0].Seq != 44 {
		t.Fatalf("identity keyed array did not land: %+v", got[0])
	}
	if r.Duplicate != 0 {
		t.Fatalf("duplicate raised on the identity fixed read: %d (the fixed form raises all §4 counters but duplicate)", r.Duplicate)
	}

	// COMPILED PLAN PATH: the plan tableFixedCompile builds from the layout.
	parsed, why := tableFixedParseLayout(RootFixedLayout)
	if why != "" {
		t.Fatalf("my own layout does not parse: %s", why)
	}
	var rr TableReport
	made := tableFixedCompile(parsed, RootFixedLayout, RootFixedDst, plan, &rr)
	if made <= 0 {
		t.Fatalf("compile made %d", made)
	}
	var v Root
	RootReset(&v)
	record := buf[len(buf)-RootFixedRecordBytes:]
	tableFixedRun(plan, made, record[8:], tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v)), &rr)
	if rr.Duplicate != 0 {
		t.Fatalf("duplicate raised on the compiled fixed read: %d (the fixed form raises all §4 counters but duplicate)", rr.Duplicate)
	}
}
`)
}

// TestFixedFormReadSlackUnspecified is W4: a fixed record's declared slack is
// UNSPECIFIED ON READ (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md
// §3.1): a reader validates the USED UNITS ONLY and never looks at the slack,
// so a peer that leaves garbage past a string's stated length or an array's
// live count is a peer this reader reads correctly. NON-ZERO SLACK IS NOT
// malformed, NOT A REFUSAL, AND MOVES NO COUNTER.
//
// The C++ reference's slack_case (test/tables/fixedform_main.cpp:715-777)
// reads a wire its OWN writer zeroed; the read half is exactly the tolerance
// above. This is that half with the wire slack poisoned AFTER the write, which
// is the only way to reach the read rule at all: the writer's zeros would make
// the assertion vacuous. The poison is chosen out of the array's declared
// [0,10] range so that a pass which walked the declared bound instead of the
// live count would count it.
func TestFixedFormReadSlackUnspecified(t *testing.T) {
	runGenerated(t, `package probe
fixed table Slack {
    label string(8)
    marks [..4]int32 | min = 0, max = 10
    tail  int32 = 42
}
`, `package probe
import ("testing"; "encoding/binary")

func TestReadSlackUnspecified(t *testing.T) {
	one := Slack{}
	SlackReset(&one)
	one.Label[0] = 'h'
	one.Label[1] = 'i'
	one.LabelLength = 2
	one.MarksCount = 1
	one.Marks[0] = 7
	buf := make([]byte, SlackFixedMeasure(1))
	if n := SlackFixedSave([]Slack{one}, buf); n != int64(len(buf)) {
		t.Fatalf("save: n=%d want %d", n, len(buf))
	}
	if SlackFixedBodyBytes != 36 {
		t.Fatalf("the record body moved (%d bytes), so the offsets below are stale", SlackFixedBodyBytes)
	}
	body := buf[len(buf)-SlackFixedRecordBytes+8:]
	// the declared wire order: label length (4) + 8 label bytes, marks count
	// (4) + four int32 elements (16), then tail (4).
	const labelSlackAt = 4 + 2              // past the stated length of 2
	const marksSlackAt = 4 + 8 + 4 + 4      // past the live count of 1
	// A PEER left garbage in the declared slack: a byte no clean record carries.
	copy(body[labelSlackAt:], []byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA})
	for i := 0; i < 3; i++ {
		binary.LittleEndian.PutUint32(body[marksSlackAt+4*i:], 0x5A5A5A5A)
	}
	// NOT VACUOUS: the wire really carries non-zero garbage in the declared
	// slack (the exact bytes are a control knob; the RULE is about non-zero).
	if body[labelSlackAt] == 0 || binary.LittleEndian.Uint32(body[marksSlackAt:]) == 0 {
		t.Fatal("the wire slack was not poisoned, so nothing below is tested")
	}

	got := make([]Slack, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := SlackFixedLoad(got, buf, plan, &r); n != 1 {
		t.Fatalf("a peer's garbage in the slack refused the record: n=%d %+v", n, r)
	}
	if r != (TableReport{}) {
		t.Fatalf("a peer's garbage in the slack moved a counter or named a refusal: %+v", r)
	}
	if got[0].LabelLength != 2 || got[0].Label[0] != 'h' || got[0].Label[1] != 'i' {
		t.Fatalf("the used text did not read: %+v", got[0])
	}
	if got[0].MarksCount != 1 || got[0].Marks[0] != 7 {
		t.Fatalf("the live prefix did not read: %+v", got[0])
	}
	if got[0].Tail != 42 {
		t.Fatalf("the field past the slack moved: %+v", got[0])
	}
}
`)
}

// TestFixedFormWriteSideBoundsAreDebugOnly names the rule that
// TestFixedFormClampLiveCount only exercises. It writes an out-of-contract
// count and length, and asserts the WRITE side does nothing: the save refuses
// nothing and clamps nothing, so the caller's own bytes land on the wire. The
// read side is what clamps, and only it (docs/FIXED-FORM-ALGORITHM.md §3.1:
// "Write-side bound checks are DEBUG ONLY, by rule. `count <= Max` and
// `length <= N` are a caller contract"). Go has no debug-only idiom (AGENTS.md
// rule 4), so the fixed form's save path holds NO write-side bound check in
// ANY build — this test asserts the rule's consequence by name: the write side
// clamps nothing and refuses nothing, and a caller's bug rides the wire
// unchanged rather than being silently clamped into a lawful record.
func TestFixedFormWriteSideBoundsAreDebugOnly(t *testing.T) {
	runGenerated(t, `package probe
fixed table Host {
    marks [..4]int32
    name  string(8)
    tail  int32 = 7
}
`, `package probe
import ("testing")

func TestWriteSideClampsNothingRefusesNothing(t *testing.T) {
	if HostFixedBodyBytes != 36 {
		t.Fatalf("the record body moved: %d bytes, so the offsets below are stale", HostFixedBodyBytes)
	}

	// THE NOT-VACUOUS CONTROL, first: a lawful count and length round-trip
	// cleanly — the write side produces the record's own bytes and the read
	// side clamps nothing, so this case is about the caller contract, not a
	// broken save.
	lawful := Host{Tail: 7}
	HostReset(&lawful)
	lawful.MarksCount = 2
	lawful.Marks[0] = 11
	lawful.Marks[1] = 22
	lawful.NameLength = 3
	copy(lawful.Name[:], "abc")
	lb := make([]byte, HostFixedMeasure(1))
	if n := HostFixedSave([]Host{lawful}, lb); n != int64(len(lb)) {
		t.Fatalf("lawful save %d", n)
	}
	got := make([]Host, 1)
	var r TableReport
	plan := make([]TableFixedEntry, 256)
	if n := HostFixedLoad(got, lb, plan, &r); n != 1 {
		t.Fatalf("lawful load %d %+v", n, r)
	}
	if got[0].MarksCount != 2 || got[0].NameLength != 3 || r.Clamped != 0 {
		t.Fatalf("a lawful count and length must not clamp: %+v %+v", got[0], r)
	}

	// THE RULE, BY NAME. count <= Max and length <= N are a CALLER CONTRACT:
	// 7 > 4 and 9 > 8 are the caller's bug, and the write side must refuse
	// nothing and clamp nothing — the release path pays nothing.
	one := Host{Tail: 7}
	HostReset(&one)
	one.MarksCount = 7
	one.NameLength = 9
	need := HostFixedMeasure(1)
	buf := make([]byte, need)
	if n := HostFixedSave([]Host{one}, buf); n != need {
		t.Fatalf("write-side bound checks are DEBUG ONLY, by rule: count <= Max and length <= N are a caller contract, not a wire defence — the release path must REFUSE NOTHING, but FixedSave returned %d, want %d", n, need)
	}
	body := buf[len(buf)-HostFixedRecordBytes+8:]
	if got := tableFixedGet32(body[0:]); got != 7 {
		t.Fatalf("write-side bound checks are DEBUG ONLY, by rule: count <= Max is a caller contract — the write side must CLAMP NOTHING, but marks count landed on the wire as %d, want the caller's 7", got)
	}
	if got := tableFixedGet32(body[20:]); got != 9 {
		t.Fatalf("write-side bound checks are DEBUG ONLY, by rule: length <= N is a caller contract — the write side must CLAMP NOTHING, but name length landed on the wire as %d, want the caller's 9", got)
	}
	if got := tableFixedGet32(body[32:]); got != 7 {
		t.Fatalf("the field past the out-of-contract count and length moved: %d, want the writer's 7", got)
	}
}
`)
}
