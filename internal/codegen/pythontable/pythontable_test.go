package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const coreSchema = `package core

fixed table Counters
{
    enabled bool
    small   int8
    count   uint32
    wide    int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    huge    uint128
}

table Node
{
    value int32
    score uint64
    next  *Node
}

table Root
{
    live    bool
    total   int64
    head    *Node
    tail    *Node
    counter Counters
}
`

// loadUnit parses and checks one schema file.
func loadUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("Core.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Core.schema", Name: "Core.schema", Base: "Core",
		Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// expectedDescriptors is the descriptor facts the probe must print, computed
// from the IR so the test is not a second copy of the emitter's constants.
func expectedDescriptors(u *ir.Unit) string {
	var b strings.Builder
	names := make([]string, 0, len(ir.TableClosure(u)))
	for name := range ir.TableClosure(u) {
		names = append(names, name)
	}
	sort.Strings(names)
	variable := ir.VariableTables(u)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		fmt.Fprintf(&b, "table %s %016x %t %d\n", st.Name, ir.TableWireId(st.WireName()), variable[name], len(st.Fields))
		for _, f := range st.Fields {
			lo, hi := intRange(f)
			fmt.Fprintf(&b, "field %s %s %016x %d %s %s %s\n", st.Name, f.Name, ir.TableFieldWireId(f), ir.TableWireFieldKind(f), codecOf(f), lo, hi)
		}
	}
	return b.String()
}

func TestPythonTableCore(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, coreSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + probeSource
	got := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	gotFloor := string(pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	if got != gotFloor {
		t.Fatalf("the two pinned interpreters disagree:\ncurrent:\n%s\nfloor:\n%s", got, gotFloor)
	}
	if out, err := pyrun.Analyze(t, dir, "core"); err != nil {
		t.Fatalf("mypy --strict over the generated package:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	var descriptors strings.Builder
	rest := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "table ") || strings.HasPrefix(line, "field ") {
			descriptors.WriteString(line + "\n")
			continue
		}
		rest = append(rest, line)
	}
	if want := expectedDescriptors(u); descriptors.String() != want {
		t.Fatalf("descriptor facts differ:\ngot:\n%s\nwant:\n%s", descriptors.String(), want)
	}
	for _, line := range []string{
		"defaults True 0 0 True",
		"measure True",
		"roundtrip True True True",
		"report 0",
		"empty 0100" + strings.Repeat("0", 16) + " 10 True",
		"refuse total_bool refused",
		"refuse small_range refused",
		"refuse count_range refused",
		"refuse live_int refused",
		"load form2 message_form_as_file",
		"load truncated wire_damaged",
		"load trailing wire_damaged",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks the line %q:\n%s", line, got)
		}
	}
	if len(rest) == 0 {
		t.Fatal("the probe printed nothing")
	}
}

const probeSource = `from core import Counters, InvalidValue, LoadReport, Node, Refused, Root

print("descriptor", Root.descriptor.name, "%016x" % Root.descriptor.type_id)
for cls in (Counters, Node, Root):
    print("table", cls.descriptor.name, "%016x" % cls.descriptor.type_id,
          str(cls.descriptor.variable).lower(), len(cls.descriptor.fields))
    for field in cls.descriptor.fields:
        print("field", cls.descriptor.name, field.name, "%016x" % field.field_id,
              field.kind, field.codec, field.lo, field.hi)

d = Root()
print("defaults", d.live is False, d.counter.small, d.counter.huge, d.head is None)

def build():
    r = Root()
    r.live = True
    r.total = -5
    c = r.counter
    c.enabled = True
    c.small = -7
    c.count = 4000000000
    c.wide = -(1 << 100)
    c.huge = (1 << 127) + 5
    a = Node()
    a.value = 1
    a.score = (1 << 64) - 1
    b = Node()
    b.value = 2
    b.score = 0
    a.next = b
    r.head = a
    r.tail = b
    return r

r = build()
data = r.save()
print("measure", r.measure() == len(data))
r2 = Root.load(data)
print("roundtrip", r2 == r, r2.head.next is r2.tail, r2.head is not r2.tail)
report = LoadReport()
Root.load(data, report=report)
print("report", report.unknown)

e = Root()
empty = e.save()
print("empty", empty.hex(), len(empty), Root.load(empty) == e)

def refuses(label, value):
    r = Root()
    if label == "total_bool":
        r.total = True
    elif label == "small_range":
        r.counter.small = 128
    elif label == "count_range":
        r.counter.count = 1 << 32
    elif label == "live_int":
        r.live = 1
    try:
        r.save()
        print("refuse", label, "wrote")
    except InvalidValue:
        print("refuse", label, "refused")

refuses("total_bool", True)
refuses("small_range", 128)
refuses("count_range", 1 << 32)
refuses("live_int", 1)

def load_refusal(label, data):
    try:
        Root.load(data)
        print("load", label, "read")
    except Refused as err:
        print("load", label, err.reason)

form2 = bytearray(Root().save())
form2[0] = 2
load_refusal("form2", bytes(form2))
load_refusal("truncated", Root().save()[:-3])
load_refusal("trailing", Root().save() + b"\x00")

print("OK")
`

// TestPythonTableCoreNegativeControl moves the form byte the generated
// runtime writes, in a copy, and requires the probe to go red: a gate is only
// watching the codec if a broken codec fails it.
func TestPythonTableCoreNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, coreSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	sabotaged := make(map[string][]byte, len(files))
	for name, data := range files {
		sabotaged[name] = data
	}
	const path = "core/_schema/save.py"
	text := string(sabotaged[path])
	const anchor = "writer.byte(FORM)"
	if !strings.Contains(text, anchor) {
		t.Fatalf("the negative control's anchor %q is absent from %s", anchor, path)
	}
	sabotaged[path] = []byte(strings.Replace(text, anchor, "writer.byte(FORM + 1)", 1))
	dir := t.TempDir()
	writeTree(t, dir, sabotaged)
	_, _, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probeSource)})
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: moving the form byte left the probe green")
	}
}

const shapesSchema = `package shapes

enum Key { Alpha, Beta, Gamma }

table Lean
{
    text         string(8)
    blob         *bytes
    blob_text    *string
    maybe        ?int32
    values       []int32
    fixed_values [3]int32
    by_key       [Key]uint16
    lookup       map[uint32]int64
}
`

func TestPythonTableDescriptorsCoverEveryConstruct(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, shapesSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import shapes
for cls in (shapes.Lean, shapes.LeanLookupEntry):
    print("table", cls.descriptor.name, str(cls.descriptor.variable).lower(), len(cls.descriptor.fields))
    for f in cls.descriptor.fields:
        print("field", cls.descriptor.name, f.name, f.kind, f.codec, str(f.array).lower(), f.bound, f.key_enum or "-", str(f.pointer).lower(), str(f.optional).lower(), f.target or "-")
print("defaults", shapes.Lean().text, shapes.Lean().maybe is None, shapes.Lean().values, shapes.Lean().lookup)
print("OK")
`
	got := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	if !strings.Contains(got, "OK\n") {
		t.Fatalf("probe did not finish:\n%s", got)
	}
	var want strings.Builder
	names := make([]string, 0, len(ir.TableClosure(u)))
	for name := range ir.TableClosure(u) {
		names = append(names, name)
	}
	sort.Strings(names)
	variable := ir.VariableTables(u)
	for _, name := range names {
		st := u.Tables[name]
		if st == nil {
			st = u.Structs[name]
		}
		fmt.Fprintf(&want, "table %s %t %d\n", st.Name, variable[name], len(st.Fields))
		for _, f := range st.Fields {
			key := f.KeyEnum
			if key == "" {
				key = "-"
			}
			target := targetName(f)
			if target == "" {
				target = "-"
			}
			fmt.Fprintf(&want, "field %s %s %d %s %t %d %s %t %t %s\n", st.Name, f.Name, ir.TableWireFieldKind(f), codecOf(f), f.Array != ir.ArrayNone, f.ArrayBound, key, f.Type.Pointer, f.Type.Optional, target)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.HasPrefix(line, "table ") || strings.HasPrefix(line, "field ") {
			if !strings.Contains(want.String(), line+"\n") {
				t.Errorf("probe line is not in the descriptor set:\n%s", line)
			}
		}
	}
}
