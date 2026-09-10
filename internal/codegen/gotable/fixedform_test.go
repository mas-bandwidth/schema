package gotable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
)

func TestFixedFormEmitsSurface(t *testing.T) {
	files := generate(t, `package probe
table Point {
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
	// DERIVED into the fixed mode, not declared fixed: the form-3 surface
	// above is emitted, and the form-1 Load it never lost still reads.
	load := funcSource(body, "func PointLoad(")
	if load == "" {
		t.Fatal("PointLoad was not emitted")
	}
	if !strings.Contains(load, "PointLoadBody") {
		t.Error("PointLoad of a table nobody declared fixed must still walk form 1")
	}
	if strings.Contains(load, "previous_form") {
		t.Error("PointLoad refuses form 1 on a table nobody declared fixed")
	}
}

// TestFixedFormForm1RefusalIsByTheKeyword is the other half of the surface
// above, and the difference is #823's `fixed table`: a DECLARED fixed table
// encodes as form 3 always, so a form-1 file handed to its Load is a named
// refusal and never a slower read of the same records (Glenn 2026-09-09).
func TestFixedFormForm1RefusalIsByTheKeyword(t *testing.T) {
	files := generateFixed(t, `package probe
table Point {
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
	if !strings.Contains(load, "previous_form") || !strings.Contains(load, "tableFixedRefuse") {
		t.Error("PointLoad of a DECLARED fixed table is not a named form-1 refusal")
	}
	if strings.Contains(load, "PointLoadBody") {
		t.Error("PointLoad still walks a form-1 file of a declared fixed table")
	}
}

func TestFixedFormRoundTrip(t *testing.T) {
	runGenerated(t, `package probe
table Point {
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
	broken := append([]byte(nil), buf...)
	broken[TableFixedHeaderBytes+4] ^= 0xFF
	r = TableReport{}
	if n := PointFixedLoad(got, broken, plan, &r); n >= 0 || r.Reason != "layout_malformed" {
		t.Fatalf("layout_malformed: %d %+v", n, r)
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

func TestFixedFormPlanPath(t *testing.T) {
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
		back := make([]tblfx2.FxRoot, 1)
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
		if n >= 0 || r.Reason != "layout_malformed" || r.Unknown != 0 || r.KindMismatch != 0 || r.Malformed {
			t.Fatalf("layout_malformed: n=%d %+v", n, r)
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

func TestForm1OfFixedTableRefused(t *testing.T) {
	runGeneratedFixed(t, `package probe
table Point {
    x int32 = 1
    y int32 = 2
}
`, `package probe
import ("testing")

func TestForm1LoadIsNamedRefusal(t *testing.T) {
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
	if PointLoad(&got, form1, &r) {
		t.Fatalf("form-1 Load of a DECLARED fixed table succeeded: %+v seq=%+v", r, got)
	}
	if r.Reason != "previous_form" || r.Verdict != TableOpenRefused || r.Malformed {
		t.Fatalf("want named previous_form, got %+v", r)
	}
	if got.X == 4242 || got.Y == -7 {
		t.Fatalf("form-1 Load of a DECLARED fixed table was a slow read: %+v", got)
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
// destination is not the wire's own byte: a Go `bool` compares its byte
// against 1, so a raw copy lands a hostile 2 as FALSE, where the wire and the
// C++ reference both say NONZERO IS TRUE (docs/SPEC-TABLES.md §3). Every bool
// destination is here — a plain field, an optional's PRESENT byte, an
// optional's own value, and an array of them, which the coalescer folds into
// ONE bool run — and both reader paths are: the identity plan the emitter laid
// down, and the plan the compiler builds when a writer's layout is not this
// build's.
func TestFixedFormHostileBoolByte(t *testing.T) {
	runGenerated(t, `package probe
table Host {
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
	// so it is true on the wire, and it is not 1, so a raw copy makes it false
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
	if !v.On {
		t.Fatalf("%s: a hostile 2 in a bool landed as false", what)
	}
	if v.Off {
		t.Fatalf("%s: a zero bool landed as true", what)
	}
	if !v.MaybePresent {
		t.Fatalf("%s: a hostile 3 in the present byte landed as absent", what)
	}
	if !v.Maybe {
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
	parsed, ok := tableFixedParseLayout(HostFixedLayout)
	if !ok {
		t.Fatal("my own layout does not parse")
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

// THE NEGATIVE CONTROL, in the same file: with the normalisation removed the
// hostile 2 must land as false. A raw byte copy is exactly what the op
// replaced, so this is the op's own sabotage without a build overlay.
func TestHostileBoolNegativeControl(t *testing.T) {
	buf := hostileRecord(t)
	record := buf[len(buf)-HostFixedRecordBytes:]
	var v Host
	HostReset(&v)
	raw := tableFixedOverlay(unsafe.Pointer(&v), unsafe.Sizeof(v))
	raw[unsafe.Offsetof(v.On)] = record[8+bodyOn] // the copy the op replaced
	if v.On {
		t.Fatal("NEGATIVE CONTROL FAILED: a raw 2 copied into a Go bool read as true, so the op it replaced was never needed")
	}
}
`)
}

// TestFixedFormHostileUnionTag is the union tag's control. A tag beyond the
// arms this reader has is form 1's UNKNOWN ARM ID (docs/SPEC-TABLES.md §4):
// the union lands as None and `unknown` counts. Copied raw it would land as a
// discriminant no variant spells, with every arm's guard declining to fill it
// — a value that is neither None nor an arm, and nothing counted to say so.
func TestFixedFormHostileUnionTag(t *testing.T) {
	runGenerated(t, `package probe
type Hit { damage int32 }
type Chat { volume int32 }
union Pick
{
    hit  Hit
    chat Chat
}
table Host {
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
		if r.Unknown != 1 || r.KindMismatch != 0 || r.Malformed || r.Verdict != TableOpenOk {
			t.Fatalf("tag %d: an unknown arm is one unknown and nothing else: %+v", tag, r)
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
table Root {
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
table Root {
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
table Writer {
    pick Pick
}
table Reader {
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
	parsed, ok := tableFixedParseLayout(layout)
	if !ok {
		t.Fatal("writer layout does not parse")
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
		plan[i].Arg = plan[i].Meta
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
