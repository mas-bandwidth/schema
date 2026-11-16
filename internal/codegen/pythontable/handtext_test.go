package pythontable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func generateHandtext(u *ir.Unit) (map[string][]byte, error) {
	return GenerateSurface(u, Surfaces{Wire: true, Fixed: true, JSON: true, TextForm: true})
}

// TestPythonHandtextSyntax exercises the grammar independently of the value walk.
func TestPythonHandtextSyntax(t *testing.T) {
	t.Parallel()
	files, err := generateHandtext(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	src := pyrun.ImportPrelude + `
from core._schema.textform_scan import parse, TextError
samples = [b'(:x 1 :s "a; b" :a (:V null))', b'; note\r\n() ;end', b'(:x 1e2)', b'(:x 1,)', b'() ()', b'(:x "\\ud800")', b';\x00\n()', b'(:x 1\r)']
for s in samples:
    try:
        print(parse(s).kind)
    except TextError as e:
        print(e.reason)
for kw in ({"max_bytes": 2}, {"max_tokens": 1}, {"max_nodes": 1}, {"max_allocation": 1}, {"max_depth": 1}):
    try:
        parse(b'(("abc"))', **kw)
    except TextError as e:
        assert e.reason == "text_limit"
    else:
        raise AssertionError(kw)
assert parse(b'(:x 1)', max_tokens=4, max_nodes=1).kind == 'list'
for invalid_root in (b'', b'true'):
    try:
        parse(invalid_root)
    except TextError as e:
        assert e.reason == 'text_syntax'
    else:
        raise AssertionError('root must be a record')
try:
    parse(b'(:x 1)', max_tokens=3)
except TextError as e:
    assert e.reason == 'text_limit'
else:
    raise AssertionError('closing delimiter token')
`
	inputs := map[string][]byte{"main.py": []byte(src)}
	want := "list\nlist\nlist\ntext_syntax\ntext_syntax\ntext_encoding\ntext_syntax\ntext_syntax\n"
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != want {
			t.Fatalf("syntax: got %s want %s", strings.TrimSpace(got), strings.TrimSpace(want))
		}
	}
}

func TestPythonHandtextResolution(t *testing.T) {
	t.Parallel()
	files, err := generateHandtext(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	src := pyrun.ImportPrelude + `
from core import Counters, Root
from core._schema.textform import load
from core._schema.textform_scan import TextError
for data in (b'(:small 1e1 :enabled true)', b'(:small 1.5)', b'(:small 128)', b'(:bogus 1)', b'(:small 1 "small" 2)'):
    try:
        _, value = load(Counters, data)
        print(value.small, value.enabled)
    except TextError as e:
        print(e.reason)
try:
    Counters.from_text_form(b'(:small 7)', max_bytes=4)
except TextError as e:
    assert e.reason == 'text_limit'
else:
    raise AssertionError('public limit')
doc = Counters.from_text_form(b'(:small 7 :enabled true)')
edited = doc.edit([('small', b'8'), ('enabled', b'false')])
assert edited.write() == b'(:small 8 :enabled false)' and doc.write() == b'(:small 7 :enabled true)'
try:
    doc.edit([('small', b'7'), ('small', b'8')])
except TextError as e:
    assert e.reason == 'text_edit_conflict'
else:
    raise AssertionError('overlapping no-op edit')
nested = Root.from_text_form(b'(:counter (:small 7))')
assert nested.edit([('counter.small', b'8')]).write() == b'(:counter (:small 8))'
assert nested.edit([(('counter', 'small'), b'9')]).write() == b'(:counter (:small 9))'
for token in (b'8 :enabled false', b' 8', b'(8)'):
    try:
        doc.edit([('small', token)])
    except TextError:
        pass
    else:
        raise AssertionError('non-scalar edit')
`
	inputs := map[string][]byte{"main.py": []byte(src)}
	want := "10 True\ntext_kind_mismatch\ntext_bound\ntext_unknown_key\ntext_duplicate_key\n"
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != want {
			t.Fatalf("resolution: got %s want %s", strings.TrimSpace(got), strings.TrimSpace(want))
		}
	}
}

func TestPythonHandtextReservedName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"from_text_form", "load_text", "save_text"} {
		unit := loadUnit(t, "package probe\ntable Root { "+name+" int32 }\n")
		if _, err := generateHandtext(unit); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("generated method collision %s was not refused: %v", name, err)
		}
	}
}

func TestPythonHandtextSurfaceSelection(t *testing.T) {
	t.Parallel()
	unit := loadUnit(t, "package probe\ntable Root { value int32 }\n")
	for _, tc := range []struct {
		name     string
		surfaces Surfaces
		want     bool
	}{
		{"default", DefaultSurfaces(), false},
		{"wire", Surfaces{Wire: true}, false},
		{"json", Surfaces{Wire: true, JSON: true}, false},
		{"textform", Surfaces{Wire: true, JSON: true, TextForm: true}, true},
	} {
		files, err := GenerateSurface(unit, tc.surfaces)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		src := string(files["probe/Core.py"])
		if strings.Contains(src, "def from_text_form(") != tc.want {
			t.Fatalf("%s: incorrect textform selection", tc.name)
		}
		if !strings.Contains(src, "def load_text(") || !strings.Contains(src, "def save_text(") {
			t.Fatalf("%s: missing wire text carrier", tc.name)
		}
	}
	field := loadUnit(t, "package probe\ntable Root { from_text_form int32 }\n")
	if _, err := GenerateSurface(field, Surfaces{Wire: true}); err != nil {
		t.Fatalf("unselected name refused: %v", err)
	}
	if _, err := generateHandtext(field); err == nil || !strings.Contains(err.Error(), "from_text_form") {
		t.Fatalf("selected name not refused: %v", err)
	}
	wireOnly := loadUnit(t, "package probe\ntable Root | forms = \"wire\" { from_text_form int32 }\n")
	files, err := Generate(wireOnly)
	if err != nil {
		t.Fatalf("wire-only name refused: %v", err)
	}
	if strings.Contains(string(files["probe/Core.py"]), "def from_text_form(") {
		t.Fatal("wire-only table emitted textform")
	}
	if _, ok := files["probe/_schema/json_table.py"]; ok {
		t.Fatal("wire-only table emitted JSON runtime")
	}
	if _, ok := files["probe/_schema/textform.py"]; ok {
		t.Fatal("wire-only table emitted textform runtime")
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root
v = Root()
v.from_text_form = 7
assert Root.load_text(v.save_text()).from_text_form == 7
assert not callable(v.from_text_form)
print('ok')
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); got != "ok\n" {
			t.Fatalf("wire-only runtime: %q", got)
		}
	}
}

func TestPythonHandtextSurfaceNegativeControl(t *testing.T) {
	t.Parallel()
	files, err := generateHandtext(loadUnit(t, "package probe\ntable Root { value int32 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	path := "probe/Core.py"
	source := string(files[path])
	const method = "def from_text_form("
	if strings.Count(source, method) != 1 {
		t.Fatal("surface control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, method, "def broken_text_form(", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root
assert hasattr(Root, 'from_text_form'), 'HANDTEXT_SURFACE_CONTROL'
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_SURFACE_CONTROL") {
			t.Fatalf("surface control: %v %s", err, stderr)
		}
	}
}

const handtextSchema = `package probe
enum State { unknown, done }
flags Features { Fast, Safe }
type Node {
 id string(16)
 title string(32)
 state State
}
type TextItem { line string(80) }
union Choice {
 amount int32
 ping
}
table Document {
 schema uint8 | min = 0, max = 3
 root string(32)
 title string(32)
 state State
 count int32 | min = 0, max = 10
 enabled bool
 text [..4]TextItem
 nodes [..3]Node
 pairs [2]int32
 optional ?[..4]int32
 blob bytes(4) | json = "bytes"
 signed_value int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727, json = "signed"
 unsigned_value uint128 | json = "unsigned"
 fraction fixed(16,16) | min = -32768, max = 32767
 real float32 | json = "float"
 time timestamp
 flags Features
 keyed [State]int32
 choice Choice
}
table GNode { id int32 }
table Graph {
 items []*GNode
 lookup map[string(8)]?int32
 blob *bytes
}
`

func TestPythonHandtextCorpus(t *testing.T) {
	t.Parallel()
	for _, fixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("fixed=%t", fixed), func(t *testing.T) {
			t.Parallel()
			testPythonHandtextCorpus(t, fixed)
		})
	}
}

func testPythonHandtextCorpus(t *testing.T, fixed bool) {
	schema := handtextSchema
	if fixed {
		schema = strings.Replace(schema, "table Document {", "fixed table Document {", 1)
	}
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	root := filepath.Join("..", "..", "..", "test", "conformance", "textform")
	inputs := map[string][]byte{}
	for _, stem := range []string{"roadmap", "layout", "kinds", "variable"} {
		for _, ext := range []string{"input.sexp", "rewrite.sexp"} {
			b, err := os.ReadFile(filepath.Join(root, stem+"."+ext))
			if err != nil {
				t.Fatal(err)
			}
			inputs[stem+"."+ext] = b
		}
	}
	for _, stem := range []string{"roadmap", "layout"} {
		b, err := os.ReadFile(filepath.Join(root, stem+".edited.sexp"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[stem+".edited.sexp"] = b
	}
	paths, err := filepath.Glob(filepath.Join(root, "refusals", "*.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 24 {
		t.Fatalf("expected 24 refusal vectors, got %d", len(paths))
	}
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		stem := strings.TrimSuffix(filepath.Base(path), ".sexp")
		for _, ext := range []string{"sexp", "reason"} {
			b, err := os.ReadFile(filepath.Join(root, "refusals", stem+"."+ext))
			if err != nil {
				t.Fatal(err)
			}
			inputs[stem+"."+ext] = b
		}
		names = append(names, stem)
	}
	src := pyrun.ImportPrelude + `
from probe import Document, Graph
from pathlib import Path
import struct
import json
from probe._schema.textform_scan import TextError
for stem in ("roadmap", "layout", "kinds", "variable"):
    data = Path(stem + ".input.sexp").read_bytes()
    doc = (Graph if stem == "variable" else Document).from_text_form(data)
    assert doc.write() == Path(stem + ".rewrite.sexp").read_bytes(), stem
    if stem == "variable":
        assert doc.value.items[0] is doc.value.items[1]
        assert doc.value.items[0].id == 6 and doc.value.items[2] is None
        assert doc.value.lookup == {"west": None, "east": 2} and doc.value.blob == b""
        assert doc.edit([('lookup[0][1]', b'4')]).value.lookup == {"west": 4, "east": 2}
        swapped = doc.edit([('lookup[0][0]', b'"east"'), ('lookup[1][0]', b'"west"')])
        assert swapped.value.lookup == {"east": None, "west": 2}
    if stem == "roadmap":
        assert doc.value.schema == 1 and doc.value.root == "schema/goal" and doc.value.count == 2
        assert [n.title for n in doc.value.nodes] == ["First", "Second"]
        assert [c.path for c in doc.comments] == [(), (), (), ("title",), ("nodes",), ("nodes", 0), ("nodes", 1), ("count",), (), ()]
        assert doc.edit([("nodes[0].title", b'"Changed"')]).write() == Path("roadmap.edited.sexp").read_bytes()
    if stem == "layout":
        assert doc.value.count == 2 and doc.value.title == "A B" and doc.value.enabled
        assert [c.path for c in doc.comments] == [(), ("title",), ()]
        assert doc.edit([("count", b"3")]).write() == Path("layout.edited.sexp").read_bytes()
        assert doc.edit([("count", b"2")]).write() == data
    if stem == "kinds":
        v = doc.value
        assert v.schema == 2 and v.pairs == [1, -2] and v.blob == b"\x00\xff"
        assert v.signed_value == -(1 << 127) and v.unsigned_value == (1 << 128) - 1
        assert v.fraction == 1 and struct.pack(">f", v.real) == bytes.fromhex("80000000")
        assert v.flags == 3 and v.optional == [] and v.text == []
        rendered = json.loads(v.to_json())
        assert rendered["state"] == "done" and rendered["keyed"] == {"unknown": 4, "done": 7}
        assert rendered["choice"] == {"ping": None} and rendered["time"] == "2026-10-15T12:34:05Z"
        assert doc.edit([('choice.ping', b'null')]).write() == data
print("positive", 4)
for stem in ` + fmt.Sprintf("%#v", names) + `:
    data = Path(stem + ".sexp").read_bytes()
    want = Path(stem + ".reason").read_text().strip()
    try:
        (Graph if stem in ("duplicate-map", "forward-reference") else Document).from_text_form(data)
    except TextError as e:
        assert e.reason == want, (stem, e.reason, want)
    else:
        raise AssertionError((stem, "accepted", want))
print("refusals", 24)
`
	// Use a Python tuple literal rather than Go's slice formatting.
	src = strings.Replace(src, fmt.Sprintf("%#v", names), "("+strings.Join(quotePythonNames(names), ",")+",)", 1)
	inputs["main.py"] = []byte(src)
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != "positive 4\nrefusals 24\n" {
			t.Fatalf("corpus: %s", got)
		}
	}
	if out, err := pyrun.Analyze(t, dir, "probe"); err != nil {
		t.Fatalf("mypy: %s", out)
	}
}

func quotePythonNames(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = strconv.Quote(name)
	}
	return out
}

func TestPythonHandtextNegativeControl(t *testing.T) {
	t.Parallel()
	files, err := generateHandtext(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []struct{ name, original, replacement, script string }{
		{"rewrite", "return self.source\n", "return self.source.strip()\n", `doc = Counters.from_text_form(b" ; keep\n(:small 7)\n")
assert doc.write() == b" ; keep\n(:small 7)\n", "HANDTEXT_REWRITE_CONTROL"`},
		{"field", "field = fields.get(name)\n        if field is None:", "field = fields.get('sabotaged:' + name)\n        if field is None:", `doc = Counters.from_text_form(b"(:small 7)")
assert doc.value.small == 7, "HANDTEXT_FIELD_CONTROL"`},
		{"attachment", "result.append(Comment(start, end, inside[2]))", "result.append(Comment(start, end, ()))", `doc = Counters.from_text_form(b"(:small ; field\n 7)")
assert doc.comments[0].path == ("small",), "HANDTEXT_ATTACHMENT_CONTROL"`},
	} {
		copied := maps.Clone(files)
		path := "core/_schema/textform.py"
		source := string(copied[path])
		if strings.Count(source, control.original) != 1 {
			t.Fatalf("%s anchor missing", control.name)
		}
		copied[path] = []byte(strings.Replace(source, control.original, control.replacement, 1))
		dir := t.TempDir()
		writeTree(t, dir, copied)
		probe := pyrun.ImportPrelude + "from core import Counters\n" + control.script + "\n"
		inputs := map[string][]byte{"main.py": []byte(probe)}
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			_, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
			if err == nil || (control.name == "rewrite" && !strings.Contains(stderr, "HANDTEXT_REWRITE_CONTROL")) || (control.name == "field" && !strings.Contains(stderr, "text_unknown_key")) || (control.name == "attachment" && !strings.Contains(stderr, "HANDTEXT_ATTACHMENT_CONTROL")) {
				t.Fatalf("%s control did not fail by name: %v %s", control.name, err, stderr)
			}
		}
	}
}

func TestPythonHandtextUnbounded(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Tags {
 rules []string
 first [..4]?string
 capped [..2]string
 byname map[string]int32
 extra [..4]?string(8) | total = 3
}`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	vectors := filepath.Join("..", "..", "..", "test", "conformance", "textform")
	inputs := map[string][]byte{}
	for _, name := range []string{"unbounded.input.sexp", "unbounded.rewrite.sexp"} {
		b, err := os.ReadFile(filepath.Join(vectors, name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = b
	}
	src := pyrun.ImportPrelude + `from pathlib import Path
from probe import Tags
from probe._schema.textform_scan import TextError
text = Path('unbounded.input.sexp').read_bytes()
doc = Tags.from_text_form(text)
assert doc.write() == Path('unbounded.rewrite.sexp').read_bytes()
assert doc.value.rules[0] == 'x' and len(doc.value.rules[1]) > 255
assert doc.value.first == ['x', None]
assert doc.value.capped == []
assert len(doc.value.byname) == 2 and max(map(len,doc.value.byname)) > 255
try:
    Tags.from_text_form(b'(:capped ("one" "two" "three"))')
except TextError as e:
    assert e.reason == 'text_bound'
else:
    raise AssertionError('count bound')
try:
    Tags.from_text_form(b'(:extra ("ab" null "cd"))')
except TextError as e:
    assert e.reason == 'text_bound'
else:
    raise AssertionError('text total')
print('ok')
`
	inputs["main.py"] = []byte(src)
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", inputs)); got != "ok\n" {
			t.Fatalf("unbounded: %s", got)
		}
	}
}

func TestPythonHandtextDottedKey(t *testing.T) {
	t.Parallel()
	unit := loadUnit(t, `package probe
fixed table Dot {
 value int32 | json = "a.b"
}`)
	files, err := generateHandtext(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Dot
doc = Dot.from_text_form(b'("a.b" 2e0)')
assert doc.edit([(('a.b',), b'3')]).write() == b'("a.b" 3)'
assert doc.edit([(('a.b',), b'2')]).write() == doc.write()
print('ok')
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); got != "ok\n" {
			t.Fatalf("dotted: %s", got)
		}
	}
}

func TestPythonHandtextKindArrays(t *testing.T) {
	t.Parallel()
	const schema = `package probe
enum Color { Red, Blue }
flags Feature { Fast, Safe }
table Values {
 colors []Color
 features []Feature
 floats [2]float32
 present ?[..3]int32
 names []?string(8) | total = 12
 marks ?[2]Color
}`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Values
from probe._schema.textform_scan import TextError
doc = Values.from_text_form(b'(:colors (:Red :Blue) :features ((:Fast) (:Fast :Safe)) :floats (1.0 -0.0) :present () :names ("alpha" null "beta") :marks (:Red :Blue))')
v = doc.value
assert v.colors == [1, 2] and v.features == [1, 3]
assert v.floats[0] == 1.0 and v.present == [] and v.names == ['alpha', None, 'beta']
assert v.marks == [1, 2]
try:
    doc.edit([('colors.Red', b':Red')])
except TextError as e:
    assert e.reason == 'text_edit_conflict'
else:
    raise AssertionError('sequence treated as a record')
for text, reason in ((b'(:colors (:Missing))', 'text_unknown_variant'), (b'(:features ((:Fast :Fast)))', 'text_duplicate_key'), (b'(:floats (1.0))', 'text_bound'), (b'(:names ("123456789"))', 'text_bound')):
    try:
        Values.from_text_form(text)
    except TextError as e:
        assert e.reason == reason, (e.reason, reason)
    else:
        raise AssertionError((text, reason))
print('ok')
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); got != "ok\n" {
			t.Fatalf("kind arrays: %s", got)
		}
	}
}

func TestPythonHandtextKindsNegativeControl(t *testing.T) {
	t.Parallel()
	const schema = `package probe
flags Feature { Fast, Safe }
table Values { features []Feature }
`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	path := "probe/_schema/textform.py"
	source := string(files[path])
	const anchor = "if name in names:"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("duplicate flag control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "if False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Values
from probe._schema.textform_scan import TextError
try:
    Values.from_text_form(b'(:features ((:Fast :Fast)))')
except TextError as e:
    assert e.reason == 'text_duplicate_key', 'HANDTEXT_DUPLICATE_FLAG_CONTROL'
else:
    raise AssertionError('HANDTEXT_DUPLICATE_FLAG_CONTROL')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_DUPLICATE_FLAG_CONTROL") {
			t.Fatalf("flag control: %v %s", err, stderr)
		}
	}
}

func TestPythonHandtextArrayRoot(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Root | json = "array" {
 values []int32
}
table Outer {
 inner Root
}`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root, Outer
assert Root.from_text_form(b'()').value.values == []
assert Root.from_text_form(b'(:values (1 2))').value.values == [1, 2]
assert Outer.from_text_form(b'(:inner (:values (3 4)))').value.inner.values == [3, 4]
print('ok')
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); got != "ok\n" {
			t.Fatalf("array root: %s", got)
		}
	}
	invalid := []byte(schema + "\ntable Pointer { target *Root }\n")
	parsed, parseErrors := parser.Parse("Core.schema", invalid)
	if len(parseErrors) != 0 {
		t.Fatal(parseErrors)
	}
	_, errors := check.Unit([]check.SourceFile{{Path: "Core.schema", Name: "Core.schema", Base: "Core", Bytes: invalid, AST: parsed}})
	if len(errors) == 0 || !strings.Contains(fmt.Sprint(errors), `json = "array" refuses a table any pointer targets`) {
		t.Fatalf("array-root pointer was not refused by name: %v", errors)
	}
}

func TestPythonHandtextArrayRootNegativeControl(t *testing.T) {
	t.Parallel()
	unit := loadUnit(t, `package probe
table Root | json = "array" { values []int32 }
`)
	files, err := generateHandtext(unit)
	if err != nil {
		t.Fatal(err)
	}
	path := "probe/_schema/textform.py"
	source := string(files[path])
	const anchor = "array_root = cls.descriptor.json_array and not label"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("array root control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "array_root = False", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root, Refused
try:
    value = Root.from_text_form(b'(:values (3))').value
except Refused:
    raise AssertionError('HANDTEXT_ARRAY_ROOT_CONTROL') from None
assert value.values == [3], 'HANDTEXT_ARRAY_ROOT_CONTROL'
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_ARRAY_ROOT_CONTROL") {
			t.Fatalf("array root control: %v %s", err, stderr)
		}
	}
}

func TestPythonHandtextWireCarrier(t *testing.T) {
	t.Parallel()
	files, err := generateHandtext(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import base64
from core import Counters, Root, Refused, LoadReport
for cls in (Counters, Root):
    value = cls()
    wire = value.save()
    text = value.save_text()
    assert text == base64.b64encode(wire).decode('ascii') and '\n' not in text
    assert cls.load_text(text).save() == wire
    for damaged, reason in (('/x==', 'base64_not_canonical'), ('AQ=', 'base64_damaged'), ('AQ===', 'base64_damaged'), ('AAAA=', 'base64_damaged'), ('A=AA', 'base64_damaged'), ('AQ==\n', 'base64_not_canonical'), ('AQ==\r\n', 'base64_not_canonical'), ('AQ== ', 'base64_damaged'), ('!AAA', 'base64_damaged'), ('é', 'base64_damaged'), (b'AQ==', 'base64_damaged')):
        report = LoadReport()
        report.unknown = 17
        try:
            cls.load_text(damaged, report)
        except Refused as e:
            assert e.reason == reason and report.unknown == 17, (damaged, e.reason)
        else:
            raise AssertionError(('accepted', damaged))
print('ok')
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); got != "ok\n" {
			t.Fatalf("wire carrier: %s", got)
		}
	}
}

func TestPythonHandtextWireCarrierNegativeControl(t *testing.T) {
	t.Parallel()
	files, err := generateHandtext(loadUnit(t, coreSchema))
	if err != nil {
		t.Fatal(err)
	}
	path := "core/_schema/text_carrier.py"
	source := string(files[path])
	const anchor = "if base64.b64encode(wire) != encoded:"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("wire carrier control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "if False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from core import Refused
from core._schema.text_carrier import decode_text
try:
    decode_text('/x==')
except Refused:
    pass
else:
    raise AssertionError('HANDTEXT_CARRIER_CANONICAL_CONTROL')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_CARRIER_CANONICAL_CONTROL") {
			t.Fatalf("wire carrier control: %v %s", err, stderr)
		}
	}
}

func TestPythonHandtextWireCarrierCRC(t *testing.T) {
	t.Parallel()
	unit := loadUnit(t, `package crcprobe
table Root | integrity = "crc32c" { n int32 }
`)
	files, err := generateHandtext(unit)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import base64
from crcprobe import Root, Refused, LoadReport
v = Root(); v.n = 7
text = v.save_text()
assert Root.load_text(text).n == 7
wire = base64.b64decode(text)
tampered = base64.b64encode(wire[:-1] + bytes([wire[-1] ^ 1])).decode('ascii')
report = LoadReport(); report.unknown = 77
try:
    Root.load_text(tampered, report)
except Refused as e:
    assert e.reason == 'integrity_mismatch' and report.unknown == 77
else:
    raise AssertionError('tampered CRC accepted')
print('ok')
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); got != "ok\n" {
			t.Fatalf("CRC carrier: %s", got)
		}
	}
}

func TestPythonHandtextUnionBlobs(t *testing.T) {
	t.Parallel()
	const schema = `package probe
union Reach {
 text *string
 blob *bytes
}
table Root {
 reach Reach
}`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root
from probe._schema.textform_scan import TextError
text = Root.from_text_form(b'(:reach (:text "hello"))')
assert text.value.reach.text == 'hello'
assert Root.load_text(text.value.save_text()).reach.text == 'hello'
blob = Root.from_text_form(b'(:reach (:blob "AP8="))')
assert blob.value.reach.blob == b'\x00\xff'
assert Root.load_text(blob.value.save_text()).reach.blob == b'\x00\xff'
try:
    Root.from_text_form(b'(:reach (:blob "AP9="))')
except TextError as e:
    assert e.reason == 'text_encoding'
else:
    raise AssertionError('noncanonical union blob')
print('ok')
`
	for _, run := range []func(testing.TB, string, string, map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
		if got := string(run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})); got != "ok\n" {
			t.Fatalf("union blobs: %s", got)
		}
	}
}

func TestPythonHandtextUnionBlobsNegativeControl(t *testing.T) {
	t.Parallel()
	const schema = `package probe
union Reach { blob *bytes }
table Root { reach Reach }
`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	path := "probe/_schema/textform.py"
	source := string(files[path])
	const anchor = "if base64.b64encode(decoded).decode() != node.value:"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("union blob control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "if False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root
from probe._schema.textform_scan import TextError
try:
    Root.from_text_form(b'(:reach (:blob "AP9="))')
except TextError as e:
    assert e.reason == 'text_encoding', 'HANDTEXT_UNION_BLOB_CONTROL'
else:
    raise AssertionError('HANDTEXT_UNION_BLOB_CONTROL')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_UNION_BLOB_CONTROL") {
			t.Fatalf("union blob control: %v %s", err, stderr)
		}
	}
}

func TestPythonHandtextWireCarrierCRCNegativeControl(t *testing.T) {
	t.Parallel()
	unit := loadUnit(t, `package crcprobe
table Root | integrity = "crc32c" { n int32 }
`)
	files, err := generateHandtext(unit)
	if err != nil {
		t.Fatal(err)
	}
	path := "crcprobe/_schema/integrity.py"
	source := string(files[path])
	const anchor = `if int.from_bytes(view[-4:], "little") != crc32c(body):`
	if strings.Count(source, anchor) != 1 {
		t.Fatal("CRC control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "if False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import base64
from crcprobe import Root, Refused
v = Root(); v.n = 7
wire = base64.b64decode(v.save_text())
text = base64.b64encode(wire[:-1] + bytes([wire[-1] ^ 1])).decode('ascii')
try:
    Root.load_text(text)
except Refused:
    pass
else:
    raise AssertionError('HANDTEXT_CARRIER_CRC_CONTROL')
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_CARRIER_CRC_CONTROL") {
			t.Fatalf("CRC control: %v %s", err, stderr)
		}
	}
}

func TestPythonHandtextEditPathNegativeControl(t *testing.T) {
	t.Parallel()
	const schema = `package probe
type Child { value int32 }
table Root { items [..3]Child }
`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	path := "probe/_schema/textform.py"
	source := string(files[path])
	const anchor = `elif getattr(desc, "codec", "") in ("array", "pointer_array", "union_array"):`
	if strings.Count(source, anchor) != 1 {
		t.Fatal("edit path control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "elif False:", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root, Refused
v = Root.from_text_form(b'(:items ((:value 1)))')
try:
    changed = v.edit([('items[0].value', b'2')])
except Refused:
    raise AssertionError('HANDTEXT_EDIT_PATH_CONTROL') from None
assert changed.value.items[0].value == 2, 'HANDTEXT_EDIT_PATH_CONTROL'
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_EDIT_PATH_CONTROL") {
			t.Fatalf("edit path control: %v %s", err, stderr)
		}
	}
}

func TestPythonHandtextCombinedEditNegativeControl(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Root { values map[string]int32 }
`
	files, err := generateHandtext(loadUnit(t, schema))
	if err != nil {
		t.Fatal(err)
	}
	path := "probe/_schema/textform.py"
	source := string(files[path])
	const anchor = "except TextError:\n                # A disjoint edit"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("combined edit control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "except TextError:\n                raise\n                # A disjoint edit", 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from probe import Root, Refused
v = Root.from_text_form(b'(:values (("a" 1) ("b" 2)))')
try:
    changed = v.edit([('values[0][0]', b'"b"'), ('values[1][0]', b'"a"')])
except Refused:
    raise AssertionError('HANDTEXT_COMBINED_EDIT_CONTROL') from None
assert changed.value.values == {'b': 1, 'a': 2}, 'HANDTEXT_COMBINED_EDIT_CONTROL'
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err == nil || !strings.Contains(stderr, "HANDTEXT_COMBINED_EDIT_CONTROL") {
			t.Fatalf("combined edit control: %v %s", err, stderr)
		}
	}
}
