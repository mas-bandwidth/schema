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
	if out, err := pyrun.Analyze(t, dir, "core/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
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
	_, stderr, runErr := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probeSource)})
	if runErr == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: moving the form byte left the probe green")
	}
	const named = "message_form_as_file"
	if !strings.Contains(stderr, named) {
		t.Fatalf("NEGATIVE CONTROL FAILED: the probe went red, but not by name %q:\n%s", named, stderr)
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
lean = shapes.Lean()
print("defaults", lean.text, lean.maybe is None, lean.values, lean.fixed_values, lean.by_key.__class__.__name__, len(lean.by_key), lean.lookup)
print("keyed", [lean.by_key[k] for k in (1, 2, 3)], shapes.LeanByKey()._values)
print("OK")
`
	got := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	for _, line := range []string{
		"defaults  True [] [0, 0, 0] LeanByKey 3 {}",
		"keyed [0, 0, 0] [0, 0, 0]",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "shapes"); err != nil {
		t.Fatalf("mypy --strict over the shapes package:\n%s", out)
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

const fixedUnionSchema = `package fixedunion

enum Key { Alpha, Beta, Gamma }

table P
{
    value int32
}

union U
{
    p P
}

table Money
{
    f8   fixed(8, 0)    | min = -128, max = 127
    f16  fixed(16, 0)   | min = -32768, max = 32767
    f32  fixed(32, 0)   | min = -2147483648, max = 2147483647
    f64  fixed(64, 0)   | min = -9223372036854775808, max = 9223372036854775807
    f128 fixed(112, 16) | min = -1000000, max = 1000000
    u8   ufixed(8, 0)   | min = 0, max = 255
    u16  ufixed(16, 0)  | min = 0, max = 65535
    u32  ufixed(32, 0)  | min = 0, max = 4294967295
    u64  ufixed(64, 0)  | min = 0, max = 9223372036854775807
    u128 ufixed(112, 16) | min = 0, max = 2000000
}

table T
{
    u            U
    lookup       map[uint32]int64
    by_key       [Key]uint16
    values       []int32
    fixed_values [3]int32
}
`

// TestPythonTableCoreFixedUnion covers the fixed-point kinds' widths and the
// table-side union's generated class and descriptor, and the map entry link.
func TestPythonTableCoreFixedUnion(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, fixedUnionSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import fixedunion
from fixedunion import Money, T, U

m = Money()
m.f8 = -7
m.f16 = 1234
m.f32 = -123456
m.f64 = (1 << 40) + 3
m.f128 = -(1000000 << 16) + 9
m.u8 = 250
m.u16 = 65000
m.u32 = 4000000000
m.u64 = (1 << 62) + 7
m.u128 = (1999999 << 16) + 11
data = m.save()
print("fixed", Money.load(data) == m, m.measure() == len(data))

print("union", U.descriptor.name, len(U.descriptor.arms), U.descriptor.arms[0].name,
      "%016x" % U.descriptor.arms[0].arm_id, U.descriptor.arms[0].target)
print("uniondefault", U().type, isinstance(U().p, fixedunion.P))

entry = T.descriptor.field("lookup")
print("maplink", entry.target, entry.map_entry)
print("keyed", T().by_key.__class__.__name__, len(T().by_key), [T().by_key[k] for k in (1, 2, 3)])
print("defaults", T().values, T().fixed_values)
print("OK")
`
	got := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	for _, line := range []string{
		"fixed True True",
		"union U 1 p af63ed4c8602096f P",
		"uniondefault 0 True",
		"maplink TLookupEntry TLookupEntry",
		"keyed TByKey 3 [0, 0, 0]",
		"defaults [] [0, 0, 0]",
		"OK",
	} {
		if !strings.Contains(got, line+"\n") {
			t.Errorf("probe output lacks %q:\n%s", line, got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "fixedunion"); err != nil {
		t.Fatalf("mypy --strict over the fixed/union package:\n%s", out)
	}
	if out, err := pyrun.Analyze(t, dir, "fixedunion/_schema"); err != nil {
		t.Fatalf("mypy --strict over the runtime:\n%s", out)
	}
}

const packetUnionSchema = `package packetunion

type P
{
    value int32
}

union U
{
    p P
}

table T
{
    u U
}
`

// TestPythonTableCoreRefusesPacketThroughUnion requires a packet type reached
// through a union arm to be refused by name, as a direct field is.
func TestPythonTableCoreRefusesPacketThroughUnion(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, packetUnionSchema)
	_, err := Generate(u)
	if err == nil {
		t.Fatal("Generate accepted a packet type reached through a union arm")
	}
	for _, want := range []string{"P", "packet type"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}
