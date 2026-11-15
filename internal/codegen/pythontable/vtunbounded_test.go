package pythontable

// THE UNBOUNDED ARRAY on the Python table leg (ROADMAP.md, Variable tables;
// docs/SPEC-TABLES.md §2.9, §3), held to the one read: a construct that uses
// the kind alone and with the kinds the table frame already carries (bool,
// the full-width integers, bits, tables by value and single pointers to
// tables), read from a wire the Go engine wrote and written back byte for
// byte, or refused with no partial value and the caller's objects untouched.
//
// TestPythonVtUnbounded is the kind gate; its negative control plants the
// fault in a copy of the runtime's array module and requires the element-kind
// refusal to go red by name.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const vtUnboundedSchema = `package unbounded

fixed table Counters
{
    enabled bool
    small   int8
    count   uint32
}

table Node
{
    value int32
    next  *Node
}

table Placement
{
    x int32
    y int32
}

table Inner
{
    label uint16
}

table Mid
{
    inner Inner
}

table Save
{
    live       bool
    total      int64
    counter    Counters
    scores     []int32 | min = -100, max = 100
    flags      []bool
    mask       bits(12)
    wide       []int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    placements []Placement
    mids       []Mid
    nodes      []Node
    head       *Node
}
`

// vtUnboundedForeignSchema names the same table and field with a different
// element kind, so the Go engine writes a wire the Python read must refuse.
const vtUnboundedForeignSchema = `package unbounded

table Save
{
    scores []uint32
}
`

type vtUnboundedCase struct {
	name string
	root string
	json string
}

var vtUnboundedCases = []vtUnboundedCase{
	{"list_scalars", "Save", `{"scores":[5,-7,100,0]}`},
	{"list_bools", "Save", `{"flags":[true,false,true]}`},
	{"list_wide", "Save", `{"wide":[-1,123456789012345678901234567890,-123456789012345678901234567890]}`},
	{"list_tables", "Save", `{"placements":[{"x":1,"y":-2},{"x":3,"y":4}]}`},
	{"list_nested", "Save", `{"mids":[{"inner":{"label":1}},{"inner":{"label":65535}}]}`},
	{"list_table_pointers", "Save", `{"nodes":[{"value":1,"next":{"value":2}},{"value":3}]}`},
	{"list_empty", "Save", `{"scores":[]}`},
	{"closure", "Save", `{"live":true,"total":-5,"counter":{"enabled":true,"small":-3,"count":4000000000},"scores":[1,2],"flags":[true],"wide":[7],"placements":[{"x":1,"y":2}],"mids":[{"inner":{"label":9}}],"nodes":[{"value":4,"next":{"value":99}},{"value":5}],"head":{"value":6,"next":{"value":7}}}`},
}

// vtUnboundedEngineWire is the Go engine's form-1 wire of the JSON text at
// root.
func vtUnboundedEngineWire(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	inst := m.New(st)
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused || rep.Unknown > 0 {
		t.Fatalf("the Go engine does not read the JSON cleanly: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine does not write it: %v", err)
	}
	return wire
}

// vtUnboundedRun is one probe under both pinned interpreters: they must agree,
// and the answer is the current pin's stdout.
func vtUnboundedRun(t *testing.T, dir, main string, inputs map[string][]byte) string {
	t.Helper()
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	floor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != floor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, floor)
	}
	return got
}

func vtUnboundedRoundTripProbe(root string) string {
	return pyrun.ImportPrelude + fmt.Sprintf(`from unbounded import Refused, %s
data = open("wire", "rb").read()
keep = %s()
keep.scores = [1, 2]
before = list(keep.scores)
try:
    value = %s.load(data)
    out = value.save()
    again = %s.load(out)
    print("ok", len(out), out.hex(), value.measure() == len(out), again == value)
except Refused as err:
    print("refused", err.reason, keep.scores == before)
`, root, root, root, root)
}

func vtUnboundedRefusalProbe(root string) string {
	return pyrun.ImportPrelude + fmt.Sprintf(`from unbounded import Refused, %s
data = open("wire", "rb").read()
keep = %s()
keep.scores = [1, 2]
before = list(keep.scores)
try:
    %s.load(data)
    print("read", keep.scores == before)
except Refused as err:
    print("refused", err.reason, keep.scores == before)
`, root, root, root)
}

func TestPythonVtUnbounded(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtUnboundedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("analyze", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, files)
		if out, err := pyrun.Analyze(t, dir, "unbounded"); err != nil {
			t.Fatalf("mypy --strict over the generated array package:\n%s", out)
		}
		if out, err := pyrun.Analyze(t, dir, "unbounded/_schema"); err != nil {
			t.Fatalf("mypy --strict over the runtime:\n%s", out)
		}
	})

	for _, c := range vtUnboundedCases {
		c := c
		t.Run("roundtrip/"+c.name, func(t *testing.T) {
			t.Parallel()
			wire := vtUnboundedEngineWire(t, u, c.root, c.json)
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := vtUnboundedRun(t, dir, "main.py", map[string][]byte{
				"main.py": []byte(vtUnboundedRoundTripProbe(c.root)),
				"wire":    wire,
			})
			want := fmt.Sprintf("ok %d %x True True\n", len(wire), wire)
			if got != want {
				t.Fatalf("%s: the read-then-write is not the Go engine's wire:\ngot:  %q\nwant: %q", c.name, got, want)
			}
		})
	}

	scores := vtUnboundedField(t, u, "Save", "scores")
	elemKind := byte(ir.TableWireElemKind(scores))
	t.Run("refusal/wrong_element_kind", func(t *testing.T) {
		t.Parallel()
		foreign := loadUnit(t, vtUnboundedForeignSchema)
		wire := vtUnboundedEngineWire(t, foreign, "Save", `{"scores":[1,2,3]}`)
		vtUnboundedExpectRefusal(t, files, "Save", wire, "kind_mismatch")
	})
	t.Run("refusal/short_header", func(t *testing.T) {
		t.Parallel()
		wire := vtUnboundedFramed(scores, []byte{elemKind})
		vtUnboundedExpectRefusal(t, files, "Save", wire, "wire_damaged")
	})
	t.Run("refusal/unknown_element_kind", func(t *testing.T) {
		t.Parallel()
		wire := vtUnboundedFramed(scores, append([]byte{14}, vtUnboundedLEB(0)...))
		vtUnboundedExpectRefusal(t, files, "Save", wire, "wire_damaged")
	})
	t.Run("refusal/count_over_cap", func(t *testing.T) {
		t.Parallel()
		body := append([]byte{elemKind}, vtUnboundedLEB(1<<31)...)
		wire := vtUnboundedFramed(scores, body)
		vtUnboundedExpectRefusal(t, files, "Save", wire, "count_over_extent_cap")
	})
	t.Run("refusal/count_past_body", func(t *testing.T) {
		t.Parallel()
		body := append([]byte{elemKind}, vtUnboundedLEB(3)...)
		wire := vtUnboundedFramed(scores, body)
		vtUnboundedExpectRefusal(t, files, "Save", wire, "wire_damaged")
	})
	t.Run("refusal/trailing_bytes", func(t *testing.T) {
		t.Parallel()
		body := append([]byte{elemKind}, vtUnboundedLEB(1)...)
		body = append(body, 0, 0, 0, 0, 7)
		wire := vtUnboundedFramed(scores, body)
		vtUnboundedExpectRefusal(t, files, "Save", wire, "wire_damaged")
	})
	t.Run("refusal/element_range", func(t *testing.T) {
		t.Parallel()
		body := append([]byte{elemKind}, vtUnboundedLEB(1)...)
		body = append(body, 101, 0, 0, 0)
		wire := vtUnboundedFramed(scores, body)
		vtUnboundedExpectRefusal(t, files, "Save", wire, "out_of_range")
	})

	t.Run("write_validation", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := pyrun.ImportPrelude + `from unbounded import InvalidValue, Save

def check(label, value):
    r = Save()
    r.scores = value
    before = list(value) if isinstance(value, list) else value
    try:
        r.save()
        print(label, "wrote")
    except InvalidValue:
        print(label, r.scores == before)

check("range", [1, 101])
check("bool", [True])
check("tuple", (1, 2))
print("OK")
`
		got := vtUnboundedRun(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		for _, line := range []string{"range True", "bool True", "tuple True", "OK"} {
			if !strings.Contains(got, line+"\n") {
				t.Fatalf("the write validation lacks %q:\n%s", line, got)
			}
		}
	})
}

// vtUnboundedExpectRefusal reads wire at root and requires the one read to
// refuse by reason, with no partial value and a caller's object untouched.
func vtUnboundedExpectRefusal(t *testing.T, files map[string][]byte, root string, wire []byte, reason string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	got := vtUnboundedRun(t, dir, "main.py", map[string][]byte{
		"main.py": []byte(vtUnboundedRefusalProbe(root)),
		"wire":    wire,
	})
	want := "refused " + reason + " True\n"
	if got != want {
		t.Fatalf("the wire does not refuse by %q:\ngot:  %q\nwant: %q", reason, got, want)
	}
}

// vtUnboundedFramed wraps one array field body in a root body, an id table and
// the file's count: a wire whose one field is the construct under test.
func vtUnboundedFramed(f *ir.Field, body []byte) []byte {
	out := []byte{1}
	out = append(out, vtUnboundedLEB(1)...)
	out = append(out, byte(ir.TableKindArray))
	out = append(out, vtUnboundedLEB(uint64(len(body)))...)
	out = append(out, body...)
	out = append(out, 0)
	id := make([]byte, 8)
	binary.LittleEndian.PutUint64(id, ir.TableFieldWireId(f))
	out = append(out, id...)
	count := make([]byte, 8)
	binary.LittleEndian.PutUint64(count, 1)
	out = append(out, count...)
	return out
}

func vtUnboundedField(t *testing.T, u *ir.Unit, root, name string) *ir.Field {
	t.Helper()
	st := u.Tables[root]
	if st == nil {
		t.Fatalf("no table %s", root)
	}
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("%s has no field %s", root, name)
	return nil
}

func vtUnboundedLEB(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

// TestPythonVtUnboundedNegativeControl plants the element-kind check's fault
// in a COPY of the generated runtime and requires the refusal gate to go red
// by name: a gate is only watching the array codec if a broken codec fails it.
func TestPythonVtUnboundedNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtUnboundedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	foreign := loadUnit(t, vtUnboundedForeignSchema)
	wire := vtUnboundedEngineWire(t, foreign, "Save", `{"scores":[1,2,3]}`)
	probe := []byte(vtUnboundedRefusalProbe("Save"))
	want := "refused kind_mismatch True\n"

	cleanDir := t.TempDir()
	writeTree(t, cleanDir, files)
	if got := string(pyrun.Run(t, cleanDir, "main.py", map[string][]byte{"main.py": probe, "wire": wire})); got != want {
		t.Fatalf("the clean array kind does not refuse the foreign element kind by name:\ngot:  %q\nwant: %q", got, want)
	}

	sabotaged := make(map[string][]byte, len(files))
	for name, data := range files {
		sabotaged[name] = data
	}
	const path = "unbounded/_schema/kind_array.py"
	text := string(sabotaged[path])
	const anchor = "        if ek != fd.elem_kind:\n"
	if !strings.Contains(text, anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(text, anchor, "        if False:\n", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	got := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": probe, "wire": wire}))
	if strings.HasPrefix(got, "refused kind_mismatch") {
		t.Fatalf("NEGATIVE CONTROL FAILED: removing the element-kind check left %q", got)
	}
}
