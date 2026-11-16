package pythontable

import (
    "strings"
    "fmt"
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
    files, err := Generate(loadUnit(t, handtextSchema))
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
from probe._schema.textform_scan import TextError
for stem in ("roadmap", "layout", "kinds", "variable"):
    data = Path(stem + ".input.sexp").read_bytes()
    doc = (Graph if stem == "variable" else Document).from_text_form(data)
    assert doc.write() == Path(stem + ".rewrite.sexp").read_bytes(), stem
    if stem == "variable":
        assert doc.value.items[0] is doc.value.items[1]
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
}

func quotePythonNames(names []string) []string {
    out := make([]string, len(names))
    for i, name := range names { out[i] = strconv.Quote(name) }
    return out
}
