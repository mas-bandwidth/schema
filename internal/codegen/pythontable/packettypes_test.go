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
const packetValueText = `{"direct":{"value":7,"count":9},"many":[{"value":1,"count":2},{"value":3,"count":4}],"pick":{"leaf":{"value":5,"count":6}}}`

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
// carried kinds only; tblw1's Badge uses string, tabledemo's Attachment uses
// float32, and neither may be refused as "a packet type".
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
		{filepath.Join("..", "..", "..", "tables", "examples"), "float32"},
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
