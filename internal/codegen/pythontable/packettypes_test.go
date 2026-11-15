package pythontable

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// packetValueSchema is the card's construct: a PACKET TYPE carried by value in
// a table field, in an array and in a union arm, its own fields all kinds the
// packet core carries (docs/PORT-PYTHON.md, THE TABLE FRAME).
const packetValueSchema = `package packetvalues

type Leaf
{
    value int32
    count uint16
    wide  uint128
}

union Choice
{
    leaf Leaf
}

table Field
{
    direct Leaf
    many   []Leaf
    pick   Choice
}
`

// loadPaths builds one compilation unit from schema paths, a directory
// contributing its *.schema files (the table emitter's manifest units are
// multi-file).
func loadPaths(t *testing.T, paths ...string) *ir.Unit {
	t.Helper()
	var sources []check.SourceFile
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		if info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
					names = append(names, filepath.Join(p, e.Name()))
				}
			}
		} else {
			names = append(names, p)
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			tree, perrs := parser.Parse(name, data)
			if len(perrs) > 0 {
				t.Fatalf("parse %s: %v", name, perrs[0])
			}
			sources = append(sources, check.SourceFile{
				Path: name, Name: filepath.Base(name),
				Base:  strings.TrimSuffix(filepath.Base(name), ".schema"),
				Bytes: data, AST: tree,
			})
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	u, errs := check.Unit(sources)
	if len(errs) > 0 {
		t.Fatalf("check: %v", errs[0])
	}
	return u
}

// packetValueText is the engine's value: a packet type in a table field, in an
// array and in a union arm.
const packetValueText = `{"direct":{"value":7,"count":9,"wide":"340282366920938463463374607431768211455"},"many":[{"value":1,"count":2},{"value":3,"count":4}],"pick":{"leaf":{"value":5,"count":6}}}`

// TestPythonTablePacketTypes holds the card's one read on read-then-write: a
// Go-engine construct with a packet type by value in a table field, in an
// array and in a union arm, read by the generated table classes and written
// back byte-identical, under both pinned interpreters. It also holds that the
// blanket packet-type refusal is gone: the manifest units are refused, if at
// all, only by the packet kind the packet wire still lacks.
func TestPythonTablePacketTypes(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, packetValueSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate refused a packet type whose kinds the packet core carries: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	wire := packetValueWire(t, u)

	probe := pyrun.ImportPrelude + `import packetvalues
from packetvalues import Field

with open("wire.bin", "rb") as handle:
    data = handle.read()
value = Field.load(data)
print("wide", value.direct.wide, value.many[0].wide)
print("value", value.direct.value, value.direct.count, len(value.many),
      value.many[0].value, value.many[1].count, value.pick.type, value.pick.leaf.value)
written = value.save()
if written != data:
    raise SystemExit("wire differs: " + written.hex())
print("wire", written.hex())

try:
    Field.load(data[:-3])
    print("damaged read")
except packetvalues.Refused as err:
    print("damaged refused", err.reason)
print("OK")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire.bin": wire}
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	gotFloor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != gotFloor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, gotFloor)
	}
	for _, line := range []string{
		"value 7 9 2 1 4 1 5",
		"wide 340282366920938463463374607431768211455 0",
		"wire " + hexString(wire),
		"damaged refused wire_damaged",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	for _, path := range []string{"packetvalues", "packetvalues/_schema"} {
		if out, err := pyrun.Analyze(t, dir, path); err != nil {
			t.Fatalf("mypy --strict over %s:\n%s", path, out)
		}
	}
}

// TestPythonTablePacketTypesRefusals holds the other edge: the blanket refusal
// is gone, and only a packet kind the packet wire still lacks is refused, by
// name, naming the kind and the packet card. graphdemo's packet type uses
// carried kinds only; tblw1's Badge uses string, tabledemo's ScoreBoard uses a
// keyed array (its float32 Attachment is carried since the packet floats
// landed), and neither may be refused as "a packet type".
func TestPythonTablePacketTypesRefusals(t *testing.T) {
	t.Parallel()
	if _, err := Generate(loadPaths(t, filepath.Join("..", "..", "..", "tables", "pointers"))); err != nil {
		t.Fatalf("graphdemo was refused for its packet type: %v", err)
	}
	for _, c := range []struct {
		path string
		kind string
	}{
		{filepath.Join("..", "..", "..", "test", "tables", "W1.schema"), "string"},
		{filepath.Join("..", "..", "..", "tables", "examples"), "array"},
	} {
		_, err := Generate(loadPaths(t, c.path))
		if err == nil {
			t.Errorf("%s was accepted, but its packet type uses %s, which the packet wire does not carry", c.path, c.kind)
			continue
		}
		if !strings.Contains(err.Error(), c.kind) {
			t.Errorf("%s: the refusal does not name the kind %q: %v", c.path, c.kind, err)
		}
		if !strings.Contains(err.Error(), "P4 the packet wire") {
			t.Errorf("%s: the refusal does not name the packet card: %v", c.path, err)
		}
		if strings.Contains(err.Error(), "does not carry packet types yet") {
			t.Errorf("%s: the blanket packet-type refusal is still there: %v", c.path, err)
		}
	}
}

// TestPythonTablePacketTypesNegativeControl plants the fault in a COPY of the
// generated runtime: the nested-body length the table writer emits is moved by
// one, and the probe's byte compare must go red by name.
func TestPythonTablePacketTypesNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, packetValueSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := make(map[string][]byte, len(files))
	for name, data := range files {
		sabotaged[name] = data
	}
	const path = "packetvalues/_schema/kind_table.py"
	text := string(sabotaged[path])
	const anchor = "w.varint(len(body))"
	if !strings.Contains(text, anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(text, anchor, "w.varint(len(body) + 1)", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	main := pyrun.ImportPrelude + `from packetvalues import Field
with open("wire.bin", "rb") as handle:
    data = handle.read()
if Field.load(data).save() != data:
    raise SystemExit("wire differs")
print("OK")
`
	wire := packetValueWire(t, u)
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(main), "wire.bin": wire})
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: moving the nested body's length left the probe green")
	}
	if !strings.Contains(stderr, "wire differs") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name \"wire differs\":\n%s", stderr)
	}
}

// packetValueWire is the Go engine's canonical form-1 wire for the card's
// construct. The test reads this wire with the generated classes and requires
// the write-back to match it byte for byte.
func packetValueWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Field"))
	var report tabletext.Report
	if !m.Read(inst, []byte(packetValueText), &report) || !report.Silent() {
		t.Fatalf("the Go engine did not place the value: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine did not encode the value: %v", err)
	}
	return wire
}

func hexString(data []byte) string {
	const digits = "0123456789abcdef"
	var b strings.Builder
	for _, c := range data {
		b.WriteByte(digits[c>>4])
		b.WriteByte(digits[c&0x0F])
	}
	return b.String()
}

// packetMapValueSchema is a packet type as the VALUE of a map: the maps cell
// and this card meet here (docs/PORT-PYTHON.md, THE TABLE FRAME).
const packetMapValueSchema = `package packetmapvalues

type Leaf
{
    value int32
    count uint16
}

table Holder
{
    by map[uint8]Leaf
}
`

const packetMapValueText = `{"by":{"1":{"value":7,"count":9},"4":{"value":-2,"count":3}}}`

// TestPythonTablePacketTypesMapValue holds what a packet type as a map's value
// does after the maps cell and this card both landed: it is read by the
// generated classes and written back byte-identical to the Go engine's wire,
// under both pinned interpreters.
func TestPythonTablePacketTypesMapValue(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, packetMapValueSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate refused a packet type as a map value: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Holder"))
	var report tabletext.Report
	if !m.Read(inst, []byte(packetMapValueText), &report) || !report.Silent() {
		t.Fatalf("the Go engine did not place the value: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine did not encode the value: %v", err)
	}
	probe := pyrun.ImportPrelude + `from packetmapvalues import Holder

with open("wire.bin", "rb") as handle:
    data = handle.read()
value = Holder.load(data)
print("value", sorted(value.by), value.by[1].value, value.by[1].count, value.by[4].value, value.by[4].count)
written = value.save()
if written != data:
    raise SystemExit("wire differs: " + written.hex())
print("OK")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire.bin": wire}
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	gotFloor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != gotFloor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, gotFloor)
	}
	for _, line := range []string{"value [1, 4] 7 9 -2 3", "OK"} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	for _, path := range []string{"packetmapvalues", "packetmapvalues/_schema"} {
		if out, err := pyrun.Analyze(t, dir, path); err != nil {
			t.Fatalf("mypy --strict over %s:\n%s", path, out)
		}
	}
}

// TestPythonTablePacketTypesMapValueNegativeControl plants the fault in a COPY
// of the generated runtime: the nested-body length the table writer emits for
// the map entry's packet value is moved by one, and the probe's byte compare
// against the Go engine's wire must go red by name.
func TestPythonTablePacketTypesMapValueNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, packetMapValueSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const path = "packetmapvalues/_schema/kind_table.py"
	text := string(files[path])
	const anchor = "w.varint(len(body))"
	if !strings.Contains(text, anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	files[path] = []byte(strings.Replace(text, anchor, "w.varint(len(body) + 1)", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Holder"))
	var report tabletext.Report
	if !m.Read(inst, []byte(packetMapValueText), &report) || !report.Silent() {
		t.Fatalf("the Go engine did not place the value: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine did not encode the value: %v", err)
	}
	main := pyrun.ImportPrelude + `from packetmapvalues import Holder
with open("wire.bin", "rb") as handle:
    data = handle.read()
if Holder.load(data).save() != data:
    raise SystemExit("wire differs")
print("OK")
`
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(main), "wire.bin": wire})
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: moving the map value's nested body length left the probe green")
	}
	if !strings.Contains(stderr, "wire differs") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name \"wire differs\":\n%s", stderr)
	}
}

// packetFloatSchema is a packet type whose fields are the float and fixed
// kinds the packet wire carries since the packet floats landed, carried by
// value in a table field.
const packetFloatSchema = `package packetfloats

type Sample
{
    f32 float32
    f64 float64
    cf  float32 | min = -5, max = 5, resolution = 0.001
    q   fixed(16, 16) | min = -180, max = 180
}

table Reading
{
    sample Sample
}
`

const packetFloatText = `{"sample":{"f32":1.5,"f64":-2.25,"cf":1.25,"q":-90.5}}`

// TestPythonTablePacketTypesFloatKinds holds what a packet type of float and
// fixed fields does in a table field now that the packet wire carries those
// kinds and the table runtime does not yet: Generate accepts it, the generated
// package imports (the hosted packet codec's float checks resolve in the table
// runtime) and is mypy --strict clean, the packet class's own wire round-trips,
// and the table read and write refuse the float field by name through
// kind_unsupported.py, the whole value or nothing, under both pinned
// interpreters.
func TestPythonTablePacketTypesFloatKinds(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, packetFloatSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate refused a packet type of carried float kinds: %v", err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Reading"))
	var report tabletext.Report
	if !m.Read(inst, []byte(packetFloatText), &report) || !report.Silent() {
		t.Fatalf("the Go engine did not place the value: %+v", report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("the Go engine did not encode the value: %v", err)
	}
	probe := pyrun.ImportPrelude + `import packetfloats
from packetfloats import Reading, Sample

sample = Sample()
sample.f32 = 1.5
sample.f64 = -2.25
packet = sample.write()
again = Sample.read(packet)
print("packet", again.f32, again.f64, again.write() == packet)

with open("wire.bin", "rb") as handle:
    data = handle.read()
try:
    Reading.load(data)
    print("table read")
except packetfloats.Refused as err:
    print("table read refused", err.reason)
try:
    Reading().save()
    print("table written")
except packetfloats.InvalidValue:
    print("table write refused")
print("OK")
`
	inputs := map[string][]byte{"main.py": []byte(probe), "wire.bin": wire}
	got := string(pyrun.Run(t, dir, "main.py", inputs))
	gotFloor := string(pyrun.RunFloor(t, dir, "main.py", inputs))
	if got != gotFloor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, gotFloor)
	}
	for _, line := range []string{
		"packet 1.5 -2.25 True",
		"table read refused unsupported_kind",
		"table write refused",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	for _, path := range []string{"packetfloats", "packetfloats/_schema"} {
		if out, err := pyrun.Analyze(t, dir, path); err != nil {
			t.Fatalf("mypy --strict over %s:\n%s", path, out)
		}
	}
}
