package pythontable

// THE UNBOUNDED ARRAY on the Python table leg (ROADMAP.md, Variable tables;
// docs/SPEC-TABLES.md §2.9, §3), held to the one read: a construct that uses
// the kind alone and with the kinds the table frame already carries (bool,
// the full-width integers, bits, tables by value and single pointers to
// tables), read from a wire the Go engine wrote and written back byte for
// byte, or refused with no partial value and the caller's objects untouched.
//
// TestPythonVtUnbounded is the kind gate. TestPythonVtUnboundedNegativeControl
// removes each of the array codec's read refusals, the write's element
// validation and its length cap, one at a time, from a copy of the runtime and
// requires the case that watches it to go red by name.
// TestPythonVtUnboundedShapeBench times the element walk against the bulk
// shape (docs/PORT-PYTHON.md).

import (
	"encoding/binary"
	"fmt"
	"maps"
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
    counters   []Counters
    totals     []uint64
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
	{"list_wide", "Save", `{"wide":["-1","123456789012345678901234567890","-123456789012345678901234567890"]}`},
	{"list_tables", "Save", `{"placements":[{"x":1,"y":-2},{"x":3,"y":4}]}`},
	{"list_nested", "Save", `{"mids":[{"inner":{"label":1}},{"inner":{"label":65535}}]}`},
	{"list_table_pointers", "Save", `{"nodes":[{"value":1,"next":{"value":2}},{"value":3}]}`},
	{"list_fixed_tables", "Save", `{"counters":[{"enabled":true,"small":-3,"count":4000000000},{"small":7},{}]}`},
	{"list_unsigned_wide", "Save", `{"totals":["18446744073709551615","0","1"]}`},
	// list_empty: the Go engine ELIDES an empty plain array (§3), so this wire
	// carries no scores field at all: it reads to scores == [] by the declared
	// default and writes back the same elided wire. The array codec itself never
	// sees a count of 0 here; refusal/present_empty is the case that does.
	{"list_empty", "Save", `{"scores":[]}`},
	{"closure", "Save", `{"live":true,"total":"-5","counter":{"enabled":true,"small":-3,"count":4000000000},"scores":[1,2],"flags":[true],"wide":["7"],"placements":[{"x":1,"y":2}],"mids":[{"inner":{"label":9}}],"nodes":[{"value":4,"next":{"value":99}},{"value":5}],"head":{"value":6,"next":{"value":7}},"counters":[{"count":1}],"totals":["2"]}`},
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

// vtUnboundedRefusalProbe reads the wire at root and prints the verdict: the
// refusal's reason and whether the caller's object is untouched, then the
// refusal's whole text, so a gate can name WHICH check refused.
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
    print(str(err))
`, root, root, root)
}

// vtUnboundedRefusal is one wire the read must refuse: by reason, and by the
// refusal's own text, which names the one check that fired.
type vtUnboundedRefusal struct {
	name   string
	wire   []byte
	reason string
	detail string
}

// vtUnboundedRefusals are the read's refusals, each on a wire that reaches
// exactly one check of the array codec. The negative controls reuse them.
func vtUnboundedRefusals(t *testing.T, u *ir.Unit) []vtUnboundedRefusal {
	t.Helper()
	scores := vtUnboundedField(t, u, "Save", "scores")
	flags := vtUnboundedField(t, u, "Save", "flags")
	elemKind := byte(ir.TableWireElemKind(scores))
	boolKind := byte(ir.TableWireElemKind(flags))
	foreign := loadUnit(t, vtUnboundedForeignSchema)
	leb := vtUnboundedLEB
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	return []vtUnboundedRefusal{
		{"wrong_element_kind", vtUnboundedEngineWire(t, foreign, "Save", `{"scores":[1,2,3]}`), "kind_mismatch", "rides as an array of kind"},
		{"short_header", vtUnboundedFramed(scores, []byte{elemKind}), "wire_damaged", "an array body too short for its header"},
		{"unknown_element_kind", vtUnboundedFramed(scores, cat([]byte{14}, leb(0))), "wire_damaged", "an array element kind outside the closed list"},
		{"count_over_cap", vtUnboundedFramed(scores, cat([]byte{elemKind}, leb(1<<31))), "count_over_extent_cap", "past the int32 storage cap"},
		{"count_past_body", vtUnboundedFramed(scores, cat([]byte{elemKind}, leb(3))), "wire_damaged", "a count the array body cannot cover"},
		{"trailing_bytes", vtUnboundedFramed(scores, cat([]byte{elemKind}, leb(1), []byte{0, 0, 0, 0, 7})), "wire_damaged", "bytes left inside an array's L after its elements"},
		{"element_range", vtUnboundedFramed(scores, cat([]byte{elemKind}, leb(2), []byte{1, 0, 0, 0, 101, 0, 0, 0})), "out_of_range", "101 is outside [-100, 100]"},
		{"bool_payload", vtUnboundedFramed(flags, cat([]byte{boolKind}, leb(2), []byte{1, 2})), "wire_damaged", "a bool payload is not 0 or 1"},
		// A PRESENT EMPTY PLAIN ARRAY: kind 14, L = 2, the element kind and a
		// count of 0. An empty []T is the field's declared default, which every
		// conforming writer elides (§3; the Go engine writes list_empty with no
		// scores field), so this is a field stored at its declared default: a
		// non-canonical spelling of a good value, which the variable form's read
		// refuses (§4, docs/rulings.json reads-refuse-non-canonical). There is no
		// write-back: the read raises and returns nothing.
		{"present_empty", vtUnboundedFramed(scores, cat([]byte{elemKind}, leb(0))), "non_canonical", "rides present and empty"},
	}
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

	for _, c := range vtUnboundedRefusals(t, u) {
		t.Run("refusal/"+c.name, func(t *testing.T) {
			t.Parallel()
			vtUnboundedExpectRefusal(t, files, "Save", c)
		})
	}

	t.Run("write_validation", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTree(t, dir, files)
		got := vtUnboundedRun(t, dir, "main.py", map[string][]byte{"main.py": []byte(vtUnboundedWriteProbe)})
		for _, line := range vtUnboundedWriteLines {
			if !strings.Contains(got, line+"\n") {
				t.Fatalf("the write validation lacks %q:\n%s", line, got)
			}
		}
	})
}

// vtUnboundedWriteProbe holds the write's validation: each bad value raises
// InvalidValue before a byte is written and leaves the caller's list as it
// was. "long" is a list that reports a length past the int32 storage cap, so
// the write refuses the count its own read would refuse, without building two
// billion elements; _check_count is also held directly to a small cap.
const vtUnboundedWriteProbe = pyrun.ImportPrelude + `from unbounded import InvalidValue, Save
from unbounded._schema import kind_array

class Long(list):
    def __len__(self):
        return kind_array._MAX_COUNT + 1

def check(label, value):
    r = Save()
    r.scores = value
    before = list(value) if isinstance(value, list) else value
    try:
        r.save()
        print(label, "wrote")
    except InvalidValue:
        print(label, r.scores == before)
    except Exception as err:
        print(label, "raised", type(err).__name__)

check("range", [1, 101])
check("bool", [True])
check("tuple", (1, 2))
check("long", Long())
try:
    kind_array._check_count("x", 4, 3)
    print("cap passed")
except InvalidValue:
    print("cap refused")
try:
    kind_array._check_count("x", 3, 3)
    print("cap at", "ok")
except InvalidValue:
    print("cap at", "refused")
print("OK")
`

var vtUnboundedWriteLines = []string{"range True", "bool True", "tuple True", "long True", "cap refused", "cap at ok", "OK"}

// vtUnboundedExpectRefusal reads the case's wire at root and requires the one
// read to refuse by reason and by the named check, with no partial value and a
// caller's object untouched.
func vtUnboundedExpectRefusal(t *testing.T, files map[string][]byte, root string, c vtUnboundedRefusal) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	got := vtUnboundedRun(t, dir, "main.py", map[string][]byte{
		"main.py": []byte(vtUnboundedRefusalProbe(root)),
		"wire":    c.wire,
	})
	if !vtUnboundedRefusedBy(got, c) {
		t.Fatalf("%s: the wire does not refuse by %q (%q):\n%s", c.name, c.reason, c.detail, got)
	}
}

func vtUnboundedRefusedBy(got string, c vtUnboundedRefusal) bool {
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	return len(lines) == 2 && lines[0] == "refused "+c.reason+" True" && strings.Contains(lines[1], c.detail)
}

// TestPythonVtUnboundedShapeBench holds the array's two element shapes to the
// same value and the same bytes, and times them under both pins: the
// per-element walk through the kind module, and the bulk shape (one struct
// call, or one slice for bools). The numbers are docs/PORT-PYTHON.md's.
func TestPythonVtUnboundedShapeBench(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtUnboundedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import time
from unbounded import Save
from unbounded._schema import kind_array
from unbounded._schema.load import Reader
from unbounded._schema.save import Writer

desc = Save.descriptor
scores = next(f for f in desc.fields if f.name == "scores").element
flags = next(f for f in desc.fields if f.name == "flags").element
totals = next(f for f in desc.fields if f.name == "totals").element
COUNT = 1000
cases = {
    "small": (scores, [(i % 201) - 100 for i in range(COUNT)]),
    "large": (totals, [i * 0x9E3779B97F4A7C15 % (1 << 64) for i in range(COUNT)]),
    "flags": (flags, [i % 3 == 0 for i in range(COUNT)]),
}

N = 200
ROUNDS = 5

def run(fn):
    best = None
    for _ in range(ROUNDS):
        start = time.perf_counter()
        for _ in range(N):
            fn()
        value = (time.perf_counter() - start) * 1e9 / N
        if best is None or value < best:
            best = value
    return best

for name, (element, values) in cases.items():
    walked = bytearray()
    kind_array._write_walk(Writer(walked), element, values, None)
    bulked = bytearray()
    if not kind_array._write_bulk(Writer(bulked), element, values, None):
        raise SystemExit(name + ": the bulk shape does not carry the element")
    if walked != bulked:
        raise SystemExit(name + ": the bulk write differs from the walk")
    body = memoryview(bytes(walked))
    def reader():
        return Reader(body, 0, len(body), [])
    if kind_array._read_walk(reader(), element, COUNT, None) != values:
        raise SystemExit(name + ": the walk read differs from the value")
    if kind_array._read_bulk(reader(), element, COUNT, None) != values:
        raise SystemExit(name + ": the bulk read differs from the value")
    print("read_walk_" + name, round(run(lambda: kind_array._read_walk(reader(), element, COUNT, None)), 1))
    print("read_bulk_" + name, round(run(lambda: kind_array._read_bulk(reader(), element, COUNT, None)), 1))
    print("write_walk_" + name, round(run(lambda: kind_array._write_walk(Writer(bytearray()), element, values, None)), 1))
    print("write_bulk_" + name, round(run(lambda: kind_array._write_bulk(Writer(bytearray()), element, values, None)), 1))
print("OK")
`
	var keys []string
	for _, kind := range []string{"small", "large", "flags"} {
		for _, op := range []string{"read_walk_", "read_bulk_", "write_walk_", "write_bulk_"} {
			keys = append(keys, op+kind)
		}
	}
	for name, runner := range map[string]func(t testing.TB, dir, main string, inputs map[string][]byte) []byte{
		"python-3.15.2": func(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
			return pyrun.Run(t, dir, main, inputs)
		},
		"python-3.11.18": func(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
			return pyrun.RunFloor(t, dir, main, inputs)
		},
	} {
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if !strings.Contains(out, "OK\n") {
			t.Fatalf("%s did not finish:\n%s", name, out)
		}
		for _, key := range keys {
			value := timedValue(t, out, key)
			if value <= 0 {
				t.Fatalf("%s %s did not report a positive time", name, key)
			}
			t.Logf("%s %s %.1f ns/op", name, key, value)
		}
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

// vtUnboundedControl is one negative control: a check removed from a COPY of
// the generated runtime, and the gate case that must stop passing by name.
type vtUnboundedControl struct {
	name    string // the check, and the gate case it watches
	path    string // the runtime file the fault is planted in
	anchor  string // the check's line
	planted string // the line that replaces it
}

var vtUnboundedControls = []vtUnboundedControl{
	{"wrong_element_kind", "unbounded/_schema/kind_array.py", "        if ek != fd.elem_kind:\n", "        if False:\n"},
	{"short_header", "unbounded/_schema/kind_array.py", "        if length < 2:\n", "        if False:\n"},
	{"unknown_element_kind", "unbounded/_schema/kind_array.py", "        if not _element_kind_allowed(ek):\n", "        if False:\n"},
	{"count_over_cap", "unbounded/_schema/kind_array.py", "        if count > _MAX_COUNT:\n", "        if False:\n"},
	{"count_past_body", "unbounded/_schema/kind_array.py", "        if count > remaining:\n", "        if False:\n"},
	{"trailing_bytes", "unbounded/_schema/kind_array.py", "        if sub.pos != length:\n", "        if False:\n"},
	{"element_range", "unbounded/_schema/kind_array.py", "    if values and (min(values) < lo or max(values) > hi):\n", "    if False:\n"},
	{"bool_payload", "unbounded/_schema/kind_array.py", "        if raw.translate(None, b\"\\x00\\x01\"):\n", "        if False:\n"},
	{"present_empty", "unbounded/_schema/kind_array.py", "        if count == 0 and not fd.optional:\n", "        if False:\n"},
	// The write side: the element validation in the save walk, and the
	// length cap. Each must leave the write probe without its line.
	{"write:range True", "unbounded/_schema/save.py", "                    kinds._REGISTRY[element.codec].validate(element, item, cell)\n", "                    pass\n"},
	{"write:long True", "unbounded/_schema/save.py", "            kind_array._check_count(where, len(current))\n", "            pass\n"},
	{"write:cap refused", "unbounded/_schema/kind_array.py", "    if count > cap:\n", "    if False:\n"},
}

// TestPythonVtUnboundedNegativeControl plants each check's fault in a COPY of
// the generated runtime and requires the gate case that watches it to go red
// by name: a gate is only watching the array codec if a broken codec fails it.
// Every read refusal of the array codec has its control, and so do the write's
// element validation and its length cap.
func TestPythonVtUnboundedNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, vtUnboundedSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	refusals := map[string]vtUnboundedRefusal{}
	for _, c := range vtUnboundedRefusals(t, u) {
		refusals[c.name] = c
	}
	for _, ctl := range vtUnboundedControls {
		t.Run(strings.ReplaceAll(ctl.name, " ", "_"), func(t *testing.T) {
			t.Parallel()
			text := string(files[ctl.path])
			if strings.Count(text, ctl.anchor) != 1 {
				t.Fatalf("the control's anchor %q is not once in %s", ctl.anchor, ctl.path)
			}
			sabotaged := make(map[string][]byte, len(files))
			maps.Copy(sabotaged, files)
			sabotaged[ctl.path] = []byte(strings.Replace(text, ctl.anchor, ctl.planted, 1))
			if line, ok := strings.CutPrefix(ctl.name, "write:"); ok {
				clean := t.TempDir()
				writeTree(t, clean, files)
				inputs := map[string][]byte{"main.py": []byte(vtUnboundedWriteProbe)}
				if got := string(pyrun.Run(t, clean, "main.py", inputs)); !strings.Contains(got, line+"\n") {
					t.Fatalf("the clean runtime lacks %q:\n%s", line, got)
				}
				dir := t.TempDir()
				writeTree(t, dir, sabotaged)
				if got := string(pyrun.Run(t, dir, "main.py", inputs)); strings.Contains(got, line+"\n") {
					t.Fatalf("NEGATIVE CONTROL FAILED: removing the check still printed %q:\n%s", line, got)
				}
				return
			}
			c, ok := refusals[ctl.name]
			if !ok {
				t.Fatalf("the control %s watches no refusal case", ctl.name)
			}
			inputs := map[string][]byte{"main.py": []byte(vtUnboundedRefusalProbe("Save")), "wire": c.wire}
			clean := t.TempDir()
			writeTree(t, clean, files)
			if got := string(pyrun.Run(t, clean, "main.py", inputs)); !vtUnboundedRefusedBy(got, c) {
				t.Fatalf("the clean runtime does not refuse %s by %q (%q):\n%s", c.name, c.reason, c.detail, got)
			}
			dir := t.TempDir()
			writeTree(t, dir, sabotaged)
			if got := string(pyrun.Run(t, dir, "main.py", inputs)); vtUnboundedRefusedBy(got, c) {
				t.Fatalf("NEGATIVE CONTROL FAILED: removing the %s check still refused by name:\n%s", ctl.name, got)
			}
		})
	}
}
