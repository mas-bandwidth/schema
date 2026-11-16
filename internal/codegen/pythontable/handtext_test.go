package pythontable

import (
    "strings"
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
