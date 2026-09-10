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
	if buf[0] != 3 {
		t.Fatalf("form byte %d", buf[0])
	}
	for i := 1; i < 8; i++ {
		if buf[i] != 0 {
			t.Fatalf("reserved byte %d = %d", i, buf[i])
		}
	}
	if tableFixedGet64(buf[TableFixedHashAt:]) != PointFixedHash {
		t.Fatalf("header hash 0x%016x want 0x%016x", tableFixedGet64(buf[TableFixedHashAt:]), PointFixedHash)
	}
	if tableFixedGet32(buf[TableFixedHeaderBytes:]) != uint32(len(PointFixedLayout)) {
		t.Fatalf("layout length %d want %d", tableFixedGet32(buf[TableFixedHeaderBytes:]), len(PointFixedLayout))
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
	lying[TableFixedHashAt] ^= 0xFF
	r = TableReport{}
	if n := PointFixedLoad(got, lying, plan, &r); n >= 0 || r.Reason != "layout_malformed" {
		t.Fatalf("lying header hash: %d %+v", n, r)
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
