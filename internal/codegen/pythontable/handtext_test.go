package pythontable

import (
    "strings"
    "fmt"
    "maps"
    "os"
    "path/filepath"
    "strconv"
    "testing"

    "github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// TestPythonHandtextSyntax exercises the grammar independently of the value walk.
func TestPythonHandtextSyntax(t *testing.T) {
    t.Parallel()
    files, err := Generate(loadUnit(t, coreSchema))
    if err != nil { t.Fatal(err) }
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
`
    inputs := map[string][]byte{"main.py": []byte(src)}
    want := "list\nlist\nlist\ntext_syntax\ntext_syntax\ntext_encoding\ntext_syntax\ntext_syntax\n"
    for _, run := range []func(testing.TB,string,string,map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
        if got := string(run(t, dir, "main.py", inputs)); got != want { t.Fatalf("syntax: got %s want %s", strings.TrimSpace(got), strings.TrimSpace(want)) }
    }
}

func TestPythonHandtextResolution(t *testing.T) {
    t.Parallel()
    files, err := Generate(loadUnit(t, coreSchema))
    if err != nil { t.Fatal(err) }
    dir := t.TempDir()
    writeTree(t, dir, files)
    src := pyrun.ImportPrelude + `
from core import Counters
from core._schema.textform import load
from core._schema.textform_scan import TextError
for data in (b'(:small 1e1 :enabled true)', b'(:small 1.5)', b'(:small 128)', b'(:bogus 1)', b'(:small 1 "small" 2)'):
    try:
        _, value = load(Counters, data)
        print(value.small, value.enabled)
    except TextError as e:
        print(e.reason)
`
    inputs := map[string][]byte{"main.py": []byte(src)}
    want := "10 True\ntext_kind_mismatch\ntext_bound\ntext_unknown_key\ntext_duplicate_key\n"
    for _, run := range []func(testing.TB,string,string,map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
        if got := string(run(t, dir, "main.py", inputs)); got != want { t.Fatalf("resolution: got %s want %s", strings.TrimSpace(got), strings.TrimSpace(want)) }
    }
}

func TestPythonHandtextReservedName(t *testing.T) {
    t.Parallel()
    unit := loadUnit(t, "package probe\ntable Root { from_text_form int32 }\n")
    if _, err := Generate(unit); err == nil || !strings.Contains(err.Error(), "from_text_form") {
        t.Fatalf("generated method collision was not refused: %v", err)
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
    if fixed { schema = strings.Replace(schema, "table Document {", "fixed table Document {", 1) }
    files, err := Generate(loadUnit(t, schema))
    if err != nil { t.Fatal(err) }
    dir := t.TempDir()
    writeTree(t, dir, files)
    root := filepath.Join("..", "..", "..", "test", "conformance", "textform")
    inputs := map[string][]byte{}
    for _, stem := range []string{"roadmap", "layout", "kinds", "variable"} {
        for _, ext := range []string{"input.sexp", "rewrite.sexp"} {
            b, err := os.ReadFile(filepath.Join(root, stem+"."+ext)); if err != nil { t.Fatal(err) }
            inputs[stem+"."+ext] = b
        }
    }
    for _, stem := range []string{"roadmap", "layout"} {
        b, err := os.ReadFile(filepath.Join(root, stem+".edited.sexp")); if err != nil { t.Fatal(err) }
        inputs[stem+".edited.sexp"] = b
    }
    paths, err := filepath.Glob(filepath.Join(root,"refusals", "*.sexp")); if err != nil { t.Fatal(err) }
    if len(paths) != 24 { t.Fatalf("expected 24 refusal vectors, got %d", len(paths)) }
    names := make([]string, 0, len(paths))
    for _, path := range paths {
        stem := strings.TrimSuffix(filepath.Base(path), ".sexp")
        for _, ext := range []string{"sexp", "reason"} {
            b, err := os.ReadFile(filepath.Join(root,"refusals",stem+"."+ext)); if err != nil { t.Fatal(err) }
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
    if stem == "roadmap":
        assert doc.value.schema == 1 and doc.value.root == "schema/goal" and doc.value.count == 2
        assert [n.title for n in doc.value.nodes] == ["First", "Second"]
        assert [c.path for c in doc.comments] == [(), (), (), ("title",), ("nodes",), ("nodes", 0), ("nodes", 1), ("count",), (), ()]
        assert doc.edit([("nodes[0].title", b'"Changed"')]).write() == Path("roadmap.edited.sexp").read_bytes()
    if stem == "layout":
        assert doc.value.count == 2 and doc.value.title == "A B" and doc.value.enabled
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
    for _, run := range []func(testing.TB,string,string,map[string][]byte) []byte{pyrun.Run, pyrun.RunFloor} {
        if got := string(run(t,dir,"main.py",inputs)); got != "positive 4\nrefusals 24\n" { t.Fatalf("corpus: %s", got) }
    }
    if out, err := pyrun.Analyze(t, dir, "probe"); err != nil { t.Fatalf("mypy: %s", out) }
}

func quotePythonNames(names []string) []string {
    out := make([]string, len(names))
    for i, name := range names { out[i] = strconv.Quote(name) }
    return out
}

func TestPythonHandtextNegativeControl(t *testing.T) {
    t.Parallel()
    files, err := Generate(loadUnit(t, coreSchema))
    if err != nil { t.Fatal(err) }
    for _, control := range []struct{name, original, replacement, script string}{
        {"rewrite", "return self.source\n", "return self.source.strip()\n", `doc = Counters.from_text_form(b" ; keep\n(:small 7)\n")
assert doc.write() == b" ; keep\n(:small 7)\n", "HANDTEXT_REWRITE_CONTROL"`},
        {"field", "field = fields.get(name)\n        if field is None:", "field = fields.get('sabotaged:' + name)\n        if field is None:", `doc = Counters.from_text_form(b"(:small 7)")
assert doc.value.small == 7, "HANDTEXT_FIELD_CONTROL"`},
    } {
        copied := maps.Clone(files)
        path := "core/_schema/textform.py"
        source := string(copied[path])
        if strings.Count(source, control.original) != 1 { t.Fatalf("%s anchor missing", control.name) }
        copied[path] = []byte(strings.Replace(source, control.original, control.replacement, 1))
        dir := t.TempDir()
        writeTree(t, dir, copied)
        probe := pyrun.ImportPrelude + "from core import Counters\n" + control.script + "\n"
        inputs := map[string][]byte{"main.py": []byte(probe)}
        for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
            _, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
            if err == nil || (control.name == "rewrite" && !strings.Contains(stderr, "HANDTEXT_REWRITE_CONTROL")) || (control.name == "field" && !strings.Contains(stderr, "text_unknown_key")) {
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
}`
    files, err := Generate(loadUnit(t,schema)); if err != nil {t.Fatal(err)}
    dir := t.TempDir(); writeTree(t,dir,files)
    vectors := filepath.Join("..","..","..","test","conformance","textform")
    inputs := map[string][]byte{}
    for _, name := range []string{"unbounded.input.sexp","unbounded.rewrite.sexp"} {
        b, err := os.ReadFile(filepath.Join(vectors,name)); if err != nil {t.Fatal(err)}; inputs[name] = b
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
print('ok')
`
    inputs["main.py"] = []byte(src)
    for _, run := range []func(testing.TB,string,string,map[string][]byte) []byte{pyrun.Run,pyrun.RunFloor} {
        if got := string(run(t,dir,"main.py",inputs)); got != "ok\n" {t.Fatalf("unbounded: %s",got)}
    }
}
