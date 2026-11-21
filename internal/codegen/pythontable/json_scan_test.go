package pythontable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/jsontestsuite"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestPythonJsonScan holds the text scanner to the engine's JSON dialect.
func TestPythonJsonScan(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from core._schema.json_scan import parse, Number
from core import Refused

assert parse('\ufeff { /* comment */ "a": [true, null, -12.50e2,], }') == {"a": [True, None, Number('-12.50e2')]}
assert parse('"\\ud834\\udd1e"') == '\U0001d11e'
assert parse('"\\u0000"') == '\x00'  # The field walk decides whether a NUL is stored.
for text in ('{"a":1,"a":2}', '"\\ud800"', '"\\udc00"', '01', '1.', '1e', 'NaN', '/*x', '[1,,2]', 'true false', '"\\q"'):
    try:
        parse(text)
    except Refused:
        continue
    raise AssertionError('accepted ' + repr(text))
print('ok')
`
	input := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", input)); got != "ok\n" {
		t.Fatalf("current scanner: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", input)); got != "ok\n" {
		t.Fatalf("floor scanner: %q", got)
	}
	if out, err := pyrun.Analyze(t, dir, "core/_schema/json_scan.py"); err != nil {
		t.Fatalf("mypy --strict: %s", out)
	}
}

func TestPythonJsonScanPolicy(t *testing.T) {
	t.Parallel()
	suite, err := jsontestsuite.Load("../../..")
	if err != nil {
		t.Fatal(err)
	}
	suite.Pending = map[string]string{}
	files, err := Generate(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := make(map[string][]byte, len(suite.Files)+1)
	maps.Copy(inputs, suite.Files)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + `from core._schema.json_scan import parse
from core import Refused
for name in sorted(n for n in os.listdir('.') if n.endswith('.json')):
    try:
        with open(name, 'rb') as source:
            parse(source.read().decode('utf-8'))
    except (Refused, UnicodeDecodeError):
        print(name + '=0')
    else:
        print(name + '=1')
`)
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		got := make(map[string]bool)
		for line := range strings.SplitSeq(strings.TrimSpace(string(run.fn(t, dir, "main.py", inputs))), "\n") {
			name, verdict, ok := strings.Cut(line, "=")
			if !ok || (verdict != "0" && verdict != "1") {
				t.Fatalf("%s: malformed verdict %q", run.name, line)
			}
			got[name] = verdict == "1"
		}
		mismatches, pending := jsontestsuite.Check(suite, got)
		for _, failure := range append(mismatches, pending...) {
			t.Errorf("%s: %s", run.name, failure)
		}
	}
}

func TestPythonJsonScalarRoundTrip(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from core import Counters, Refused
from core._schema.json_table import from_json, to_json

text = '{\n  "enabled": true,\n  "small": -3,\n  "count": 4,\n  "wide": "5",\n  "huge": "6"\n}'
value = from_json(Counters, text)
assert (value.enabled, value.small, value.count, value.wide, value.huge) == (True, -3, 4, 5, 6)
assert to_json(value) == text + '\n'
assert from_json(Counters, text).save() == value.save()
for bad in ('{"wide":5}', '{"small":true}', '{"count":-1}', '{"small": 128}', '{"small":1,"small":2}'):
    try:
        from_json(Counters, bad)
    except Refused:
        continue
    raise AssertionError('accepted ' + bad)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
	if out, err := pyrun.Analyze(t, dir, "core/_schema/json_table.py"); err != nil {
		t.Fatalf("mypy --strict: %s", out)
	}
}

func TestPythonJsonFieldKeyAndRequired(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfields
table Example
{
    size int32 | json = "length", required
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonfields import Example, Refused
from jsonfields._schema.json_table import from_json, to_json
assert from_json(Example, '{"length":3}').size == 3
assert Example.from_json('{"length":3}').size == 3
assert to_json(from_json(Example, '{"length":3}')) == '{\n  "length": 3\n}\n'
for text in ('{}', '{"size":3}'):
    for read in (Example.from_json, lambda source: from_json(Example, source)):
        try:
            read(text)
        except Refused:
            continue
        raise AssertionError('accepted ' + text)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonTextAndBytes(t *testing.T) {
	t.Parallel()
	const schema = `package jsontext
table Example
{
    title string(8)
    payload bytes(4)
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsontext import Example, Refused, InvalidValue
from jsontext._schema.json_table import from_json, to_json
text = '{\n  "title": "é\\n",\n  "payload": "AP8="\n}'
value = from_json(Example, text)
assert value.title == 'é\n' and value.payload == b'\x00\xff'
assert to_json(value) == text + '\n'
for text in ('{"title":"\\u0000"}', '{"title":"123456789"}', '{"payload":"AB=="}', '{"payload":"AAAAAA==="}'):
    try:
        from_json(Example, text)
    except Refused:
        continue
    raise AssertionError('accepted ' + text)
value.title = '\x00'
try:
    to_json(value)
except InvalidValue:
    pass
else:
    raise AssertionError('wrote NUL')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonSharedNodes(t *testing.T) {
	t.Parallel()
	const schema = `package jsongraph
table Node {
    value int32
    next *Node
}

table Root {
    first *Node
    second *Node
}
`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsongraph import Root, Refused, InvalidValue, LoadReport
from jsongraph._schema.json_table import from_json, to_json
text = '{\n  "first": {\n    "&node": 1,\n    "value": 7,\n    "next": null\n  },\n  "second": {\n    "&node": 1\n  }\n}'
root = from_json(Root, text)
assert root.first is root.second and root.first.value == 7
assert to_json(root) == text + '\n'
report = LoadReport()
dropped = from_json(Root, '{"unknown":{"&node":9,"value":1},"first":{"&node":9}}', report)
assert dropped.first is None and report.unknown == 1
report.unknown = 17
try:
    from_json(Root, '{"unknown":"\\u0000"}', report)
except Refused:
    assert report.unknown == 17
else:
    raise AssertionError('accepted NUL in skipped string')
for bad in ('{"first":{"&node":1}}', '{"first":{"&node":1,"next":{"&node":1}}}', '{"first":{"value":0,"&node":1}}'):
    try:
        from_json(Root, bad)
    except Refused:
        continue
    raise AssertionError('accepted ' + bad)
root.first.next = root.first
try:
    to_json(root)
except InvalidValue:
    pass
else:
    raise AssertionError('wrote cycle')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonList(t *testing.T) {
	t.Parallel()
	const schema = `package jsonlist
table Root {
    values []int32
    wide []int64
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonlist import Root, Refused, InvalidValue
from jsonlist._schema.json_table import from_json, to_json
text = '{\n  "values": [\n    1,\n    -2\n  ],\n  "wide": [\n    "9007199254740993"\n  ]\n}'
value = from_json(Root, text)
assert value.values == [1, -2] and value.wide == [9007199254740993] and to_json(value) == text + '\n'
assert from_json(Root, '{"values":[]}').values == []
for bad in ('{"values":[true]}', '{"values":[2147483648]}', '{"wide":[9007199254740993]}'):
    try:
        from_json(Root, bad)
    except Refused:
        continue
    raise AssertionError('accepted ' + bad)
value.values.append(True)
try:
    to_json(value)
except InvalidValue:
    pass
else:
    raise AssertionError('wrote bool as int')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}
func TestPythonJsonFixedDecimal(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfixed
table Root {
    small ufixed(8, 8) | min = 0, max = 200
    wide fixed(48, 16) | min = -100000, max = 100000
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonfixed import Root, Refused
from jsonfixed._schema.json_table import from_json, to_json
text = '{\n  "small": 3.25,\n  "wide": "-12345.5"\n}'
value = from_json(Root, text)
assert value.small == 832 and value.wide == -809074688
assert to_json(value) == text + '\n'
for bad in ('{"small":0.1}', '{"wide":-1.0}', '{"wide":"99999999999999999999999999999999999999999"}'):
    try:
        from_json(Root, bad)
    except Refused:
        continue
    raise AssertionError('accepted ' + bad)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonWideCorpus(t *testing.T) {
	t.Parallel()
	const schema = `package jsonintegers
table Integers {
    small int32
    wide int64
    uwide uint64
    wide128 int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    ranged int64 | min = 0, max = 9007199254740993
    frac fixed(48, 16) | min = -10, max = 10
    counts map[uint64]int32
}`
	dir := filepath.Join("..", "..", "..", "testdata", "wire", "json-integers")
	inputs := make(map[string][]byte)
	var expect []string
	var numberStart int
	for _, manifest := range []string{"MANIFEST.tsv", "NUMBER-TOKEN.tsv"} {
		if manifest == "NUMBER-TOKEN.tsv" {
			numberStart = len(expect)
		}
		data, err := os.ReadFile(filepath.Join(dir, manifest))
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) != 5 {
				t.Fatalf("%s: malformed row %q", manifest, line)
			}
			body, err := os.ReadFile(filepath.Join(dir, cols[2]))
			if err != nil {
				t.Fatal(err)
			}
			inputs[fmt.Sprintf("row%03d.txt", len(expect))] = body
			expect = append(expect, cols[3])
			count++
		}
		if manifest == "NUMBER-TOKEN.tsv" && count != 13 {
			t.Fatalf("got %d numeric-token rows, want 13", count)
		}
	}
	canonical, err := os.ReadFile(filepath.Join(dir, "canonical.read.json"))
	if err != nil {
		t.Fatal(err)
	}
	inputs["canonical.txt"] = canonical
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	writeTree(t, work, files)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + `from jsonintegers import Integers, Refused
from jsonintegers._schema.json_table import from_json, to_json
with open('canonical.txt', encoding='utf-8') as f:
    canonical = f.read().rstrip('\n') + '\n'
for name in sorted(n for n in os.listdir('.') if n.startswith('row') and n.endswith('.txt')):
    with open(name, encoding='utf-8') as f:
        text = f.read()
    try:
        value = from_json(Integers, text)
    except Refused as error:
        print(name + '|refused|' + error.reason)
    else:
        assert to_json(value) == canonical, name
        print(name + '|read')
`)
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		lines := strings.Split(strings.TrimSpace(string(run.fn(t, work, "main.py", inputs))), "\n")
		if len(lines) != len(expect) {
			t.Fatalf("%s: got %d rows, want %d", run.name, len(lines), len(expect))
		}
		for i, line := range lines {
			prefix := fmt.Sprintf("row%03d.txt|%s", i, expect[i])
			if !strings.HasPrefix(line, prefix) {
				t.Errorf("%s: %s, want %s", run.name, line, prefix)
			}
			if i >= numberStart && line != fmt.Sprintf("row%03d.txt|refused|kind_mismatch", i) {
				t.Errorf("%s: numeric-token row %s did not refuse by kind", run.name, line)
			}
		}
	}
}

func TestPythonJsonOptionalWide(t *testing.T) {
	t.Parallel()
	const schema = `package jsonoptional
table Root {
    count ?int64
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonoptional import Root, Refused
from jsonoptional._schema.json_table import from_json, to_json
assert to_json(from_json(Root, '{}')) == '{}\n'
assert to_json(from_json(Root, '{"count":null}')) == '{}\n'
assert to_json(from_json(Root, '{"count":"9007199254740993"}')) == '{\n  "count": "9007199254740993"\n}\n'
try:
    from_json(Root, '{"count":9007199254740993}')
except Refused as error:
    assert error.reason == 'kind_mismatch'
else:
    raise AssertionError('accepted a numeric token')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonUnionWide(t *testing.T) {
	t.Parallel()
	const schema = `package jsonunion
union Pick {
    wide int64
    narrow int32
}
table Root {
    pick Pick
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonunion import Root, Refused, LoadReport
from jsonunion._schema.json_table import from_json, to_json
text = '{\n  "pick": {\n    "wide": "9007199254740993"\n  }\n}'
value = from_json(Root, text)
assert value.pick.type == 1 and value.pick.wide == 9007199254740993
assert to_json(value) == text + '\n'
report = LoadReport()
unknown = from_json(Root, '{"pick":{"&node":1}}', report)
assert unknown.pick.type == 0 and report.unknown == 1
try:
    from_json(Root, '{"pick":{"wide":9007199254740993}}')
except Refused as error:
    assert error.reason == 'kind_mismatch'
else:
    raise AssertionError('accepted a numeric token')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonUnionBlobAgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsonunionblob
 union Reach { text *string }
 table Root { reach Reach }`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	instance := model.New(model.Lookup("Root"))
	input := []byte(`{"reach":{"text":"hello"}}`)
	var report tabletext.Report
	if !model.Read(instance, input, &report) {
		t.Fatalf("engine refused: %+v", report)
	}
	canonical, err := model.Write(instance)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := tablewire.Encode(model, instance)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonunionblob import Root
with open('input', encoding='utf-8') as f: text = f.read()
with open('text','rb') as f: expected_text = f.read()
with open('wire','rb') as f: expected_wire = f.read()
value = Root.from_json(text)
assert value.reach.type == 1 and value.reach.text == 'hello'
assert value.to_json().encode() == expected_text
assert value.save() == expected_wire
assert Root.load(expected_wire).to_json().encode() == expected_text
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "input": input, "text": append(canonical, '\n'), "wire": wire}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonKeyedWideAndVocabulary(t *testing.T) {
	t.Parallel()
	const schema = `package jsonkeyed
enum Slot { A, B }
enum Shade { Red, Blue }
flags Marks { X, Y }
table Root {
    slots [Slot]int64
    shade Shade
    marks Marks
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonkeyed import Root, Refused, LoadReport
from jsonkeyed._schema.json_table import from_json, to_json
text = '{\n  "slots": {\n    "A": "-42",\n    "B": "42"\n  },\n  "shade": "Blue",\n  "marks": [\n    "X"\n  ]\n}'
value = from_json(Root, text)
assert value.slots[1] == -42 and value.slots[2] == 42 and value.shade == 2 and value.marks == 1
assert to_json(value) == text + '\n'
try:
    from_json(Root, '{"slots":{"A":42}}')
except Refused as error:
    assert error.reason == 'kind_mismatch'
else:
    raise AssertionError('accepted a numeric token')
report = LoadReport()
value = from_json(Root, '{"shade":"Other"}', report)
assert value.shade == 0 and report.unknown == 1
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonKeyedEnumAgainstEngine(t *testing.T) {
	t.Parallel()
	unit := loadUnit(t, strings.Replace(pyKeyedEnumSchema, "fixed table Root", "table Root", 1))
	model := tabletext.NewModel(unit)
	instance := model.New(model.Lookup("Root"))
	input := []byte(`{"ranks":{"head":"Low","body":"Mid","feet":"None"},"tail":4}`)
	var report tabletext.Report
	if !model.Read(instance, input, &report) {
		t.Fatalf("engine refused: %+v", report)
	}
	canonical, err := model.Write(instance)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := tablewire.Encode(model, instance)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from enumkey import Root
with open('input', encoding='utf-8') as f: text = f.read()
with open('text','rb') as f: expected_text = f.read()
with open('wire','rb') as f: expected_wire = f.read()
value = Root.from_json(text)
assert value.ranks[1] == 1 and value.ranks[2] == 2 and value.ranks[3] == 0
assert value.to_json().encode() == expected_text
assert value.save() == expected_wire, (value.save().hex(), expected_wire.hex())
assert Root.load(expected_wire).to_json().encode() == expected_text
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "input": input, "text": append(canonical, '\n'), "wire": wire}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonMapKeyedValuesAgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsonmapkeyed
 enum Slot { A, B }
 table Root { seats map[int32][Slot]int32 }`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	instance := model.New(model.Lookup("Root"))
	input := []byte(`{"seats":{"-3":{"A":4,"B":7},"2":{"A":9}}}`)
	var report tabletext.Report
	if !model.Read(instance, input, &report) {
		t.Fatalf("engine refused: %+v", report)
	}
	canonical, err := model.Write(instance)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := tablewire.Encode(model, instance)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonmapkeyed import Root
with open('input', encoding='utf-8') as f: text = f.read()
with open('text','rb') as f: expected_text = f.read()
with open('wire','rb') as f: expected_wire = f.read()
value = Root.from_json(text)
assert value.seats[-3][1] == 4 and value.seats[-3][2] == 7
assert value.to_json().encode() == expected_text
assert value.save() == expected_wire
assert Root.load(expected_wire).to_json().encode() == expected_text
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "input": input, "text": append(canonical, '\n'), "wire": wire}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonTimestamp(t *testing.T) {
	t.Parallel()
	const schema = `package jsontimestamp
table Root {
    instant timestamp
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsontimestamp import Root, Refused
from jsontimestamp._schema.json_table import from_json, to_json
value = from_json(Root, '{"instant":"1970-01-01t01:00:00.1234567898+01:00"}')
assert value.instant == 123456789
assert to_json(value) == '{\n  "instant": "1970-01-01T00:00:00.123456789Z"\n}\n'
for bad in ('{"instant":0}', '{"instant":"1970-01-01T00:00:60Z"}', '{"instant":"2000-02-30T00:00:00Z"}'):
    try:
        from_json(Root, bad)
    except Refused:
        continue
    raise AssertionError('accepted ' + bad)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonFieldMatchNegativeControl(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []struct {
		name, path, original, replacement, expression string
	}{
		{"descriptor", "core/_schema/json_table.py", "field = fields.get(key)", "field = fields.get('sabotaged:' + key)", `from_json(Counters, '{"small":7}')`},
		{"generated", "core/Core.py", `elif key == "small":`, `elif key == "sabotaged":`, `Counters.from_json('{"small":7}')`},
	} {
		copyFiles := maps.Clone(files)
		source := string(copyFiles[control.path])
		if strings.Count(source, control.original) != 1 {
			t.Fatalf("%s field match anchor occurs %d times", control.name, strings.Count(source, control.original))
		}
		copyFiles[control.path] = []byte(strings.Replace(source, control.original, control.replacement, 1))
		dir := t.TempDir()
		writeTree(t, dir, copyFiles)
		probe := pyrun.ImportPrelude + `from core import Counters
from core._schema.json_table import from_json
` + "value = " + control.expression + `
assert value.small == 7, 'FIELD_KEY_MATCH_CONTROL'
`
		inputs := map[string][]byte{"main.py": []byte(probe)}
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			_, stderr, runErr := pyrun.Try(t, pin, dir, "main.py", inputs)
			if runErr == nil || !strings.Contains(stderr, "FIELD_KEY_MATCH_CONTROL") {
				t.Fatalf("%s field-match sabotage was not caught by %s: error %v, stderr %q", control.name, pin, runErr, stderr)
			}
		}
	}
}

func TestPythonJsonSharedNodesThroughMap(t *testing.T) {
	t.Parallel()
	const schema = `package jsonmapgraph
table Node {
    number int32
}
table Root {
    first *Node
    nodes map[string(8)]*Node
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonmapgraph import Root
from jsonmapgraph._schema.json_table import from_json, to_json
text = '{\n  "first": {\n    "&node": 1,\n    "number": 3\n  },\n  "nodes": {\n    "x": {\n      "&node": 1\n    }\n  }\n}'
root = from_json(Root, text)
assert root.first is root.nodes['x']
assert to_json(root) == text + '\n'
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonMapWideValue(t *testing.T) {
	t.Parallel()
	const schema = `package jsonmapwide
table Root {
    counts map[uint64]int64
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonmapwide import Root, Refused
from jsonmapwide._schema.json_table import from_json, to_json
text = '{\n  "counts": {\n    "1": "9007199254740993"\n  }\n}'
value = from_json(Root, text)
assert value.counts == {1: 9007199254740993}
assert to_json(value) == text + '\n'
try:
    from_json(Root, '{"counts":{"1":9007199254740993}}')
except Refused as error:
    assert error.reason == 'kind_mismatch'
else:
    raise AssertionError('accepted a numeric map value')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonFloatRounding(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfloat
table Root {
    f float32
    d float64
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonfloat import Root, Refused
from jsonfloat._schema.json_table import from_json, to_json
import struct
value = from_json(Root, '{"f":0.3333333333333333,"d":1e-6}')
assert to_json(value) == '{\n  "f": 0.33333334,\n  "d": 1e-06\n}\n'
for token, bits in (('7.0064923216240853547e-46', 1), ('-7.0064923216240853547e-46', 0x80000001), ('1.0000000596046447753906251', 0x3f800001), ('1.000000059604644775390625', 0x3f800000), ('16777217.000000000000000001', 0x4b800001)):
    f = from_json(Root, '{"f":' + token + '}').f
    assert struct.unpack('<I', struct.pack('<f', f))[0] == bits, token
for text in ('{"f":1e9999}', '{"d":1e9999}', '{"f":"1.0"}'):
    try:
        from_json(Root, text)
    except Refused:
        continue
    raise AssertionError('accepted ' + text)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonArrayRoot(t *testing.T) {
	t.Parallel()
	const schema = `package jsonroot
table Root | json = "array" {
    values []int32
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonroot import Root, Refused
from jsonroot._schema.json_table import from_json, to_json
value = from_json(Root, '[1, -2]')
assert value.values == [1, -2]
assert to_json(value) == '[\n  1,\n  -2\n]\n'
try:
    from_json(Root, '{"values":[1]}')
except Refused as error:
    assert error.reason == "json_root_not_array"
else:
    raise AssertionError('accepted object root')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonBlobs(t *testing.T) {
	t.Parallel()
	const schema = `package jsonblobs
table Root {
    title *string
    data *bytes
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonblobs import Root, Refused
from jsonblobs._schema.json_table import from_json, to_json
text = '{\n  "title": "hello",\n  "data": "AP8="\n}'
value = from_json(Root, text)
assert value.title == 'hello' and value.data == b'\x00\xff' and to_json(value) == text + '\n'
value = from_json(Root, '{"title":null,"data":null}')
assert value.title is None and value.data is None
assert to_json(value) == '{\n  "title": null,\n  "data": null\n}\n'
try:
    from_json(Root, '{"title":"\\u0000"}')
except Refused:
    pass
else:
    raise AssertionError('accepted zero in text blob')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonBlobRef(t *testing.T) {
	t.Parallel()
	const schema = `package jsonblobref
 table Child { text *string }
 union Reach { text *string }
 table Root {
 first *string
 second *string
 left *Child
 right *Child
 reach Reach
 }`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonblobref import Root, Child
from jsonblobref._schema import BlobRef, InvalidValue
value = Root()
value.first = BlobRef('same')
value.second = BlobRef('same')
assert Root.from_json(value.to_json()).first == 'same'
value.second = value.first
try:
    value.to_json()
except InvalidValue as error:
    assert error.rule == 'blob_shared'
else: raise AssertionError('shared blob was written')
value.first = 'same'
value.second = 'same'
assert Root.from_json(value.to_json()).second == 'same'
shared = BlobRef('node')
value.left = Child()
value.left.text = shared
value.right = value.left
Root.from_json(value.to_json())
value.right = Child()
value.right.text = shared
try:
    value.to_json()
except InvalidValue as error:
    assert error.rule == 'blob_shared'
else: raise AssertionError('nested shared blob was written')
value.left = None
value.right = None
value.reach.type = 1
value.reach.text = shared
assert Root.from_json(value.to_json()).reach.text == 'node'
value.first = shared
try:
    value.to_json()
except InvalidValue as error:
    assert error.rule == 'blob_shared'
else: raise AssertionError('union-arm shared blob was written')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonPublicMethodsAndNames(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from core import Counters, LoadReport
report = LoadReport()
value = Counters.from_json('{"small":7}', report)
assert value.small == 7
assert '"small": 7' in value.to_json()
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
	if out, err := pyrun.Analyze(t, dir, "core"); err != nil {
		t.Fatalf("mypy --strict over generated package: %s", out)
	}
	for _, name := range []string{"from_json", "to_json"} {
		unit := loadUnit(t, "package collision\ntable Root {\n value int32\n}\n")
		unit.Tables["Root"].Fields[0].Name = name
		if _, err := Generate(unit); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("field %q should refuse its generated-method collision, got %v", name, err)
		}
	}
	packet := loadUnit(t, "package collision\ntype Packet {\n from_json int32\n to_json int32\n}\ntable Root {\n count int32\n}\n")
	if _, err := Generate(packet); err != nil {
		t.Fatalf("packet fields that have no JSON methods must remain legal: %v", err)
	}
}

func TestPythonJsonDeclaredForms(t *testing.T) {
	t.Parallel()
	const schema = `package jsonforms
 table Hidden | forms = "wire" { value int32 }
 table Visible | forms = "wire,json" { value int32 }
 table Normal { value int32 }`
	unit := loadUnit(t, schema)
	unit.Tables["Hidden"].Fields[0].Name = "from_json"
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonforms import Hidden, Visible, Normal
h = Hidden()
assert h.from_json == 0 and not hasattr(Hidden, 'to_json')
assert Visible.from_json('{"value":7}').value == 7
assert Normal.from_json('{"value":8}').value == 8
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonSharedNodesThroughUnion(t *testing.T) {
	t.Parallel()
	const schema = `package jsonuniongraph
table Node {
    number int32
}
union Pick {
    target *Node
}
table Root {
    first *Node
    pick Pick
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonuniongraph import Root
text = '{\n  "first": {\n    "&node": 1,\n    "number": 3\n  },\n  "pick": {\n    "target": {\n      "&node": 1\n    }\n  }\n}'
root = Root.from_json(text)
assert root.first is root.pick.target
assert root.to_json() == text + '\n'
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonConformanceDriver(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "unit"), files)
	for _, name := range []string{"_common.py", "_json_common.py", "json-read.py", "json-write.py", "json-hostile.py", "report.py", "write-refusal.py", "write-refusal-json.py"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "python", "surfaces", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	probe := pyrun.ImportPrelude + `import importlib, _common
_common.ROOT = os.path.dirname(os.path.abspath(__file__))
from unit.core import Root
value = Root()
value.live = True
wire = value.save()
text = value.to_json()
os.makedirs('testdata/conformance/tables/json', exist_ok=True)
os.makedirs('hostile', exist_ok=True)
os.makedirs('hostilebad', exist_ok=True)
os.makedirs('out', exist_ok=True)
with open('wire.bin', 'wb') as f: f.write(wire)
with open('testdata/conformance/tables/json/case.json', 'w', encoding='utf-8') as f: f.write(text)
with open('hostile/Root.json', 'w', encoding='utf-8') as f: f.write('{"live":true}')
with open('hostilebad/Root.json', 'w', encoding='utf-8') as f: f.write('{"live":true,"live":false}')
with open('manifest', 'w', encoding='utf-8') as f:
    f.write('instance case unit Root wire.bin\n')
    f.write('json-hostile hostilecase unit Root hostile read\n')
    f.write('json-hostile hostilebad unit Root hostilebad refused\n')
    f.write('report reportcase unit Root wire.bin\n')
    f.write('write-refusal-json badvalue unit Root wire.bin total int 9223372036854775808 refused\n')
for surface in ('json-read', 'json-write', 'json-hostile', 'report', 'write-refusal-json'):
    importlib.import_module(surface).run('manifest', 'out')
with open('out/case', 'rb') as f: assert f.read() == wire
with open('out/case.json', 'rb') as f: assert f.read() == text.encode('utf-8')
with open('out/hostilecase', 'rb') as f: assert f.read().startswith(b'read\n')
with open('out/hostilebad', 'rb') as f: assert f.read() == b'refused\n'
with open('out/reportcase', 'rb') as f: assert f.read() == b'read\n' + text.encode('utf-8')
with open('out/badvalue', 'rb') as f: assert f.read() == b'refused\n'
# A fixed declaration can exceed the fixed-layout size limit. Its ordinary
# single-value API remains available even though fixed_load is not emitted.
import _json_common
class LargeFixed:
    @classmethod
    def load(cls, data):
        assert data == wire
        return cls()
    def save(self):
        return wire
assert isinstance(_json_common.read_wire(LargeFixed, 'wire.bin'), LargeFixed)
assert _json_common.write_wire(LargeFixed, LargeFixed()) == wire
cls = _common.root_class('unit', 'Root')
field = next(field for field in cls.descriptor.fields if field.name == 'live')
field.codec = 'unsupported'
try:
    importlib.import_module('json-hostile').run('manifest', 'out')
except ValueError as error:
    assert error.reason == 'unsupported'
else:
    raise AssertionError('unsupported runtime was counted as a refusal')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonHostedPacketKeyed(t *testing.T) {
	t.Parallel()
	files, _ := pythonHostedPacketKeyedTableFiles(t)
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from packetkeyed import Holder
text = '{"board":{"per_slot":{"Alpha":7,"Beta":11}},"direct":{"Alpha":3,"Beta":4}}'
value = Holder.from_json(text)
assert type(value.board.per_slot) is list and value.board.per_slot == [7, 11]
assert value.direct._values == [3, 4]
again = Holder.from_json(value.to_json())
assert again.board.per_slot == [7, 11] and again.direct._values == [3, 4]
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonUnionArray(t *testing.T) {
	t.Parallel()
	const schema = `package jsonunionarray
union Pick {
    wide int64
    narrow int32
}
table Root {
    picks [2]Pick
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonunionarray import Root, Refused
text = '{\n  "picks": [\n    {\n      "wide": "42"\n    },\n    {\n      "narrow": 7\n    }\n  ]\n}'
value = Root.from_json(text)
assert value.picks[0].wide == 42 and value.picks[1].narrow == 7
assert value.to_json() == text + '\n'
try:
    Root.from_json('{"picks":[{"wide":42}]}')
except Refused as error:
    assert error.reason == 'kind_mismatch'
else:
    raise AssertionError('accepted a numeric union element')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonEngineAndWire(t *testing.T) {
	t.Parallel()
	const schema = `package jsonoracle
table Node {
    number int32
}
table Root {
    enabled bool
    wide int64
    values []int32
    counts map[string(8)]int32
    first *Node
    second *Node
}`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	if root == nil {
		t.Fatal("missing Root")
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	for index, text := range []string{
		`{}`,
		`{"enabled":true,"wide":"9007199254740993","values":[1,-2],"counts":{"z":2,"a":1}}`,
		`{"first":{"&node":3,"number":7},"second":{"&node":3}}`,
	} {
		instance := model.New(root)
		var report tabletext.Report
		if !model.Read(instance, []byte(text), &report) {
			t.Fatalf("engine refused %s: %+v", text, report)
		}
		canonical, err := model.Write(instance)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(model, instance)
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("input%d", index)] = []byte(text)
		inputs[fmt.Sprintf("text%d", index)] = append(canonical, '\n')
		inputs[fmt.Sprintf("wire%d", index)] = wire
	}
	probe := pyrun.ImportPrelude + `from jsonoracle import Root
for i in range(3):
    with open('input' + str(i), encoding='utf-8') as f: text = f.read()
    with open('text' + str(i), encoding='utf-8') as f: expected_text = f.read()
    with open('wire' + str(i), 'rb') as f: expected_wire = f.read()
    value = Root.from_json(text)
    assert value.to_json() == expected_text, i
    assert value.save() == expected_wire, i
    assert Root.load(expected_wire).to_json() == expected_text, i
print('ok')
`
	inputs["main.py"] = []byte(probe)
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonOptionalMapsAgainstEngine(t *testing.T) {
	t.Parallel()
	unit := loadUnit(t, optionalMapSchema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	for i, text := range []string{`{}`, `{"tags":{}}`, `{"tags":{"a":1},"nodes":{"1":{"value":7}}}`} {
		instance := model.New(root)
		var report tabletext.Report
		if !model.Read(instance, []byte(text), &report) {
			t.Fatalf("engine refused %s: %+v", text, report)
		}
		canonical, err := model.Write(instance)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(model, instance)
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("input%d", i)] = []byte(text)
		inputs[fmt.Sprintf("text%d", i)] = append(canonical, '\n')
		inputs[fmt.Sprintf("wire%d", i)] = wire
	}
	probe := pyrun.ImportPrelude + `from optmap import Root
for i in range(3):
    with open('input'+str(i), encoding='utf-8') as f: text = f.read()
    with open('text'+str(i), encoding='utf-8') as f: expected_text = f.read()
    with open('wire'+str(i), 'rb') as f: expected_wire = f.read()
    value = Root.from_json(text)
    assert value.to_json() == expected_text, i
    assert value.save() == expected_wire, i
    assert Root.load(expected_wire).to_json() == expected_text, i
print('ok')
`
	inputs["main.py"] = []byte(probe)
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonPointerArraysAgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsonpointerarrays
 table Node { number int32 }
 table Root {
   enabled bool
   nodes [..4]*Node
   if enabled {
     extra []*Node
   }
 }`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	for i, text := range []string{
		`{"nodes":[null,{"&node":7,"number":3},{"&node":7}]}`,
		`{"enabled":true,"nodes":[{"&node":5,"number":9}],"extra":[{"&node":5}]}`,
		`{"enabled":false,"extra":[{"number":4}]}`,
	} {
		instance := model.New(root)
		var report tabletext.Report
		if !model.Read(instance, []byte(text), &report) {
			t.Fatalf("engine refused %s: %+v", text, report)
		}
		canonical, err := model.Write(instance)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(model, instance)
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("input%d", i)] = []byte(text)
		inputs[fmt.Sprintf("text%d", i)] = append(canonical, '\n')
		inputs[fmt.Sprintf("wire%d", i)] = wire
	}
	probe := pyrun.ImportPrelude + `from jsonpointerarrays import Root
for i in range(3):
    with open('input'+str(i), encoding='utf-8') as f: text = f.read()
    with open('text'+str(i), encoding='utf-8') as f: expected_text = f.read()
    with open('wire'+str(i), 'rb') as f: expected_wire = f.read()
    value = Root.from_json(text)
    assert value.to_json() == expected_text, i
    assert value.save() == expected_wire, i
    assert Root.load(expected_wire).to_json() == expected_text, i
print('ok')
`
	inputs["main.py"] = []byte(probe)
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonUnboundedMapKeys(t *testing.T) {
	t.Parallel()
	const schema = `package jsonlongkey
table Root {
    counts map[string]int32
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonlongkey import Root, Refused
key = 'x' * 300
value = Root.from_json('{"counts":{"' + key + '":7}}')
assert value.counts == {key: 7}
assert Root.from_json(value.to_json()).counts == value.counts
try:
    Root.from_json('{"' + key + '":0}')
except Refused:
    pass
else:
    raise AssertionError('accepted a long table field key')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonTextList(t *testing.T) {
	t.Parallel()
	const schema = `package jsontextlist
table Root {
    words []string(8) | total = 12
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsontextlist import Root, Refused
text = '{\n  "words": [\n    "one",\n    "two"\n  ]\n}'
value = Root.from_json(text)
assert value.words == ['one', 'two']
assert Root.load(value.save()).words == value.words
assert value.to_json() == text + '\n'
for bad in ('{"words":["123456789"]}', '{"words":["12345678","12345"]}'):
    try:
        Root.from_json(bad)
    except Refused:
        continue
    raise AssertionError('accepted ' + bad)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonNullableTextList(t *testing.T) {
	t.Parallel()
	const schema = `package jsonoptionaltext
 table Root { words []?string(8) | total = 3 }`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonoptionaltext import Root, Refused, InvalidValue
value = Root.from_json('{"words":[null,"é","a"]}')
assert value.words == [None, 'é', 'a']
assert Root.load(value.save()).words == value.words
assert Root.from_json(value.to_json()).words == value.words
try:
    Root.from_json('{"words":["é","é"]}')
except Refused: pass
else: raise AssertionError('accepted excess UTF-8 byte total')
value.words = ['é', 'é']
try:
    value.to_json()
except InvalidValue: pass
else: raise AssertionError('wrote excess UTF-8 byte total')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonUnboundedNullableTextList(t *testing.T) {
	t.Parallel()
	const schema = `package jsonunboundedtext
 table Root { words []?string }`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonunboundedtext import Root
text = 'é' * 300
value = Root.from_json('{"words":[null,"' + text + '"]}')
assert value.words == [None, text]
assert Root.load(value.save()).words == value.words
assert Root.from_json(value.to_json()).words == value.words
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonFloat32AgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfloatoracle
table Root {
    f float32
}`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	if root == nil {
		t.Fatal("missing Root")
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	tokens := []string{
		"7.0064923216240853547e-46", "-7.0064923216240853547e-46",
		"7.006492321624085354618647916449580656401309709382578858785341419448955413429303007433190941810607910156250e-46",
		"7.0064923216240853546e-46", "1.0000000596046447753906251", "1.000000059604644775390625", "1.0000000596046447753906249",
		"1.000000178813934326171875", "1.0000001788139343261718749", "16777217.000000000000000001", "16777217", "1.401298464324817e-45", "3.4028234663852886e38",
	}
	for i, token := range tokens {
		text := []byte(`{"f":` + token + `}`)
		instance := model.New(root)
		var report tabletext.Report
		if !model.Read(instance, text, &report) {
			t.Fatalf("engine refused %s: %+v", token, report)
		}
		canonical, err := model.Write(instance)
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("input%02d", i)] = text
		inputs[fmt.Sprintf("expect%02d", i)] = append(canonical, '\n')
	}
	probe := pyrun.ImportPrelude + `from jsonfloatoracle import Root
for i in range(13):
    with open('input%02d' % i, encoding='utf-8') as f: text = f.read()
    with open('expect%02d' % i, encoding='utf-8') as f: expected = f.read()
    actual = Root.from_json(text).to_json()
    assert actual == expected, (i, actual, expected)
print('ok')
`
	inputs["main.py"] = []byte(probe)
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonDepthAgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsondepth
table Node {
    next *Node
}`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Node")
	if root == nil {
		t.Fatal("missing Node")
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	for _, depth := range []int{126, 127, 128, 129} {
		text := "{}"
		for range depth {
			text = `{"next":` + text + `}`
		}
		instance := model.New(root)
		var report tabletext.Report
		read := model.Read(instance, []byte(text), &report)
		inputs[fmt.Sprintf("input%d", depth)] = []byte(text)
		if read {
			canonical, err := model.Write(instance)
			if err != nil {
				t.Fatalf("engine wrote depth %d: %v", depth, err)
			}
			inputs[fmt.Sprintf("expect%d", depth)] = append(canonical, '\n')
		} else {
			inputs[fmt.Sprintf("expect%d", depth)] = []byte("REFUSED")
		}
	}
	probe := pyrun.ImportPrelude + `from jsondepth import Node, Refused
for depth in (126,127,128,129):
    with open('input' + str(depth), encoding='utf-8') as f: text = f.read()
    with open('expect' + str(depth), encoding='utf-8') as f: expected = f.read()
    try:
        value = Node.from_json(text)
    except Refused:
        assert expected == 'REFUSED', depth
    else:
        assert expected != 'REFUSED', depth
        assert value.to_json() == expected, depth
print('ok')
`
	inputs["main.py"] = []byte(probe)
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

// TestPythonJsonShapeFunctionalBench compares two scalar hot-path shapes.
func TestPythonJsonShapeFunctionalBench(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import time
from core import Counters
from core._schema.json_scan import parse
from core._schema.json_table import _read_field, _write_field, from_json as walk_read, to_json as walk_write
value = Counters()
value.enabled = True
value.small = -3
value.count = 7
value.wide = 42
value.huge = 99
text = value.to_json()
a,b,c,d,e = Counters.descriptor.fields

def direct_read():
    data = parse(text, None)
    obj = Counters()
    obj.enabled = _read_field(a, data['enabled'], None, [0])
    obj.small = _read_field(b, data['small'], None, [0])
    obj.count = _read_field(c, data['count'], None, [0])
    obj.wide = _read_field(d, data['wide'], None, [0])
    obj.huge = _read_field(e, data['huge'], None, [0])
    return obj

def direct_write():
    return '{\n  "enabled": ' + _write_field(a, value.enabled) + ',\n  "small": ' + _write_field(b, value.small) + ',\n  "count": ' + _write_field(c, value.count) + ',\n  "wide": ' + _write_field(d, value.wide) + ',\n  "huge": ' + _write_field(e, value.huge) + '\n}\n'

assert direct_read() == Counters.from_json(text) == walk_read(Counters, text) == value
assert direct_write() == walk_write(value) == text

def measure(fn):
    samples = []
    for _ in range(5):
        start = time.perf_counter_ns()
        for _ in range(10000): fn()
        samples.append((time.perf_counter_ns() - start) / 10000)
    return min(samples)
for name, fn in (('read_direct', direct_read), ('read_generated', lambda: Counters.from_json(text)), ('read_walk', lambda: walk_read(Counters, text)), ('write_direct', direct_write), ('write_generated', value.to_json), ('write_walk', lambda: walk_write(value))):
    print(name, round(measure(fn), 1))
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		out := string(run.fn(t, dir, "main.py", inputs))
		if !strings.HasSuffix(out, "ok\n") {
			t.Fatalf("%s: %s", run.name, out)
		}
		t.Logf("%s scalar shape timings (ns/op):\n%s", run.name, out)
	}
}

func TestPythonJsonGeneratedWriterMatchesWalk(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfast
enum Tone { Red, Blue }
flags Mark { X, Y }
table Root {
    live bool
    total int64
    title string(8) | json = "caption"
    payload bytes(4)
    tone Tone
    mark Mark
}`
	unit := loadUnit(t, schema)
	unit.Tables["Root"].Fields[2].JsonKey = "caption\"\x01é"
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonfast import Root, InvalidValue, LoadReport, Refused
from jsonfast._schema.json_table import from_json, to_json
v = Root()
v.live, v.total, v.title, v.payload, v.tone, v.mark = True, -42, 'é', b'\x00\xff', 2, 1
assert v.to_json() == to_json(v)
for text in (v.to_json(), '{}', '{"unknown":{"&node":1,"future":3}}'):
    fast_report, walk_report = LoadReport(), LoadReport()
    fast, walk = Root.from_json(text, fast_report), from_json(Root, text, walk_report)
    assert fast == walk and fast_report.unknown == walk_report.unknown
for text in ('{"live":1}', '{"total":42}', '{"live":true,"live":false}'):
    reasons = []
    for read in (Root.from_json, lambda source: from_json(Root, source)):
        try: read(text)
        except Refused as error: reasons.append(error.reason)
        else: raise AssertionError('accepted invalid text')
    assert reasons[0] == reasons[1]
for name, bad in (('live', 1), ('total', 1 << 63), ('title', '\x00'), ('payload', b'12345'), ('tone', 7), ('mark', 4)):
    value = Root()
    setattr(value, name, bad)
    for write in (value.to_json, lambda: to_json(value)):
        try: write()
        except InvalidValue: continue
        raise AssertionError('accepted invalid ' + name)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonCountedMinimum(t *testing.T) {
	t.Parallel()
	const schema = `package jsoncountmin
table Root {
    items ?[2..3]int32
}`
	files, err := Generate(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsoncountmin import Root, Refused
assert Root.from_json('{}').items is None
assert Root.from_json('{"items":null}').items is None
assert Root.from_json('{"items":[1,2]}').items == [1,2]
for text in ('{"items":[]}', '{"items":[1]}', '{"items":[1,2,3,4]}'):
    try: Root.from_json(text)
    except Refused as error: assert error.reason == 'count_over_bound'
    else: raise AssertionError('accepted ' + text)
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe)}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonVocabularyAgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsonvocabulary
enum Tone { Red, Blue }
flags Mark { X, Y }
table Root {
    tone Tone
    marks Mark
}`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	if root == nil {
		t.Fatal("missing Root")
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	for i, text := range []string{`{"tone":"Blue","marks":["Y","X"]}`, `{"tone":"future","marks":["future","future"]}`, `{}`} {
		instance := model.New(root)
		var report tabletext.Report
		if !model.Read(instance, []byte(text), &report) {
			t.Fatalf("engine refused %s: %+v", text, report)
		}
		canonical, err := model.Write(instance)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := tablewire.Encode(model, instance)
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("input%d", i)] = []byte(text)
		inputs[fmt.Sprintf("text%d", i)] = append(canonical, '\n')
		inputs[fmt.Sprintf("wire%d", i)] = wire
		inputs[fmt.Sprintf("unknown%d", i)] = []byte(fmt.Sprint(report.Unknown))
	}
	probe := pyrun.ImportPrelude + `from jsonvocabulary import Root, LoadReport
for i in range(3):
    with open('input' + str(i), encoding='utf-8') as f: text = f.read()
    with open('text' + str(i), encoding='utf-8') as f: expected_text = f.read()
    with open('wire' + str(i), 'rb') as f: expected_wire = f.read()
    with open('unknown' + str(i), encoding='utf-8') as f: expected_unknown = int(f.read())
    report = LoadReport()
    value = Root.from_json(text, report)
    assert value.to_json() == expected_text, i
    assert value.save() == expected_wire, i
    assert report.unknown == expected_unknown, i
print('ok')
`
	inputs["main.py"] = []byte(probe)
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

var jsonFixedGrammarVerdicts = []string{"refused", "refused", "refused", "read", "refused", "refused", "refused", "read", "read", "read"}

func TestPythonJsonFixedTextGrammarAgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfixedgrammar
table Root {
    f fixed(48, 16) | min = -10, max = 10
    n int64
}`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	if root == nil {
		t.Fatal("missing Root")
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	for i, text := range []string{`{"f":"+1"}`, `{"f":"01"}`, `{"f":".5"}`, `{"f":"1e0"}`, `{"f":"nan"}`, `{"f":"1x"}`, `{"f":""}`, `{"f":"1.0"}`, `{"f":"\u0031"}`, `{"n":"\u0031"}`} {
		var report tabletext.Report
		ok := model.Read(model.New(root), []byte(text), &report)
		if ok != (jsonFixedGrammarVerdicts[i] == "read") {
			t.Errorf("engine input%d: read=%t, want %s", i, ok, jsonFixedGrammarVerdicts[i])
		}
		inputs[fmt.Sprintf("input%d", i)] = []byte(text)
	}
	probe := pyrun.ImportPrelude + `from jsonfixedgrammar import Root, Refused
for i in range(10):
    with open('input' + str(i), encoding='utf-8') as f: text = f.read()
    try: Root.from_json(text)
    except Refused: print('refused')
    else: print('read')
`
	inputs["main.py"] = []byte(probe)
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		got := strings.Split(strings.TrimSpace(string(run.fn(t, dir, "main.py", inputs))), "\n")
		if len(got) != len(jsonFixedGrammarVerdicts) {
			t.Fatalf("%s: got %d verdicts, want %d", run.name, len(got), len(jsonFixedGrammarVerdicts))
		}
		for i := range jsonFixedGrammarVerdicts {
			if got[i] != jsonFixedGrammarVerdicts[i] {
				t.Errorf("%s input%d: got %s, want %s", run.name, i, got[i], jsonFixedGrammarVerdicts[i])
			}
		}
	}
}

func TestPythonJsonFixedGrammarCppOracle(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ grammar oracle")
	const schema = `package jsonfixedgrammar
table Root {
    f fixed(48, 16) | min = -10, max = 10
    n int64
}`
	unit := loadUnit(t, schema)
	files, err := cpp.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	writeTree(t, dir, files)
	var header string
	var sources []string
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" {
		t.Fatal("missing C++ table header")
	}
	serialize, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	program := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include "%s"
using namespace jsonfixedgrammar;
int main() {
 const char *inputs[] = { R"j({"f":"+1"})j", R"j({"f":"01"})j", R"j({"f":".5"})j", R"j({"f":"1e0"})j", R"j({"f":"nan"})j", R"j({"f":"1x"})j", R"j({"f":""})j", R"j({"f":"1.0"})j", R"j({"f":"\u0031"})j", R"j({"n":"\u0031"})j" };
 for (auto text : inputs) { RootBuilder value; TableReport report{}; std::puts(RootFromJson(value,text,(int64_t)std::strlen(text),&report) ? "read" : "refused"); }
}
`, header)
	path := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(path, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + dir, "-I" + serialize, path}, sources...)
	args = append(args, "-o", bin)
	if output, err := testproc.Command(t, "c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("C++ compile: %v\n%s", err, output)
	}
	output, err := testproc.Command(t, bin).CombinedOutput()
	if err != nil {
		t.Fatalf("C++ run: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 10 {
		t.Fatalf("C++ returned %d verdicts: %s", len(lines), output)
	}
	t.Logf("C++ verdicts for +1, 01, .5, 1e0, nan, 1x, empty, 1.0, escaped fixed 1, escaped int 1: %s", strings.Join(lines, ", "))
	for index, expected := range jsonFixedGrammarVerdicts {
		if lines[index] != expected {
			t.Errorf("C++ case %d: got %s, want %s", index, lines[index], expected)
		}
	}
}

func TestPythonJsonFixedClassIntegration(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned Python interpreters")
	const schema = `package jsonfixedclass
table Root {
    values [3]int64
    weights [1..3]float64
    comp float32 | min = -1.0, max = 1.0, resolution = 0.25
}`
	unit := loadUnit(t, schema)
	const text = `{"values":["1","2"],"weights":[0.5],"comp":-0.0}`
	wire := cppWideWire(t, unit, wideVTConstruct{schema: schema, root: "Root", json: text})
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from jsonfixedclass import Root, Refused, InvalidValue
from jsonfixedclass._schema.json_table import to_json
text = '{"values":["1","2"],"weights":[0.5],"comp":-0.0}'
value = Root.from_json(text)
assert value.values == [1,2,0] and value.weights == [0.5] and value.comp == 0.0
expected = '{\n  "values": [\n    "1",\n    "2",\n    "0"\n  ],\n  "weights": [\n    0.5\n  ],\n  "comp": 0\n}\n'
assert value.to_json() == to_json(value) == expected
with open('cpp-wire', 'rb') as source: assert value.save() == source.read()
for text in ('{"values":[1]}', '{"weights":[]}', '{"comp":1.25}'):
    try: Root.from_json(text)
    except Refused: continue
    raise AssertionError('accepted ' + text)
value.comp = 1.25
try: value.to_json()
except InvalidValue: pass
else: raise AssertionError('wrote outside compressed range')
print('ok')
`
	inputs := map[string][]byte{"main.py": []byte(probe), "cpp-wire": wire}
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}

func TestPythonJsonFloat64AgainstEngine(t *testing.T) {
	t.Parallel()
	const schema = `package jsonfloat64oracle
table Root {
    f float64
}`
	unit := loadUnit(t, schema)
	model := tabletext.NewModel(unit)
	root := model.Lookup("Root")
	if root == nil {
		t.Fatal("missing Root")
	}
	files, err := Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{}
	tokens := []string{"0", "-0.0", "1e-6", "0.1", "1.2345678901234567", "5e-324", "2.4703282292062327e-324", "2.4703282292062328e-324", "1.7976931348623157e308"}
	for i, token := range tokens {
		text := []byte(`{"f":` + token + `}`)
		instance := model.New(root)
		var report tabletext.Report
		if !model.Read(instance, text, &report) {
			t.Fatalf("engine refused %s: %+v", token, report)
		}
		canonical, err := model.Write(instance)
		if err != nil {
			t.Fatal(err)
		}
		inputs[fmt.Sprintf("input%02d", i)] = text
		inputs[fmt.Sprintf("expect%02d", i)] = append(canonical, '\n')
	}
	probe := pyrun.ImportPrelude + `from jsonfloat64oracle import Root
for i in range(9):
    with open('input%02d' % i, encoding='utf-8') as f: text = f.read()
    with open('expect%02d' % i, encoding='utf-8') as f: expected = f.read()
    actual = Root.from_json(text).to_json()
    assert actual == expected, (i, actual, expected)
print('ok')
`
	inputs["main.py"] = []byte(probe)
	if got := string(pyrun.Run(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("current: %q", got)
	}
	if got := string(pyrun.RunFloor(t, dir, "main.py", inputs)); got != "ok\n" {
		t.Fatalf("floor: %q", got)
	}
}
