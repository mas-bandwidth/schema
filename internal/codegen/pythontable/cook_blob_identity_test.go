package pythontable

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
)

func TestPythonCookBlobIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package blobidentity
 table Root {
    left *bytes
    right *bytes
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from blobidentity import Root
from blobidentity._schema import cook
for shared in (False, True):
    r = Root(); r.left = bytearray(b"data"); r.right = r.left if shared else bytearray(b"data")
    for order in (1,2): print(cook.cook(r, order).hex())
`
	m := tabletext.NewModel(u)
	var expected []string
	for range []bool{false, true} {
		root := m.New(m.Lookup("Root"))
		a := &tabletext.Blob{Data: []byte("data")}
		b := &tabletext.Blob{Data: []byte("data")}
		for i := range root.Fields {
			if root.Fields[i].Def.Name == "left" {
				root.Fields[i].Cell.Blob = a
			}
			if root.Fields[i].Def.Name == "right" {
				root.Fields[i].Cell.Blob = b
			}
		}
		for _, big := range []bool{false, true} {
			raw, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			expected = append(expected, hex.EncodeToString(raw))
		}
	}
	want := strings.Join(expected, "\n") + "\n"
	for name, runner := range cookPins() {
		got := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if got != want {
			t.Errorf("%s: blob identity bytes differ: %s", name, fmt.Sprintf("got %q want %q", got, want))
		}
	}
	t.Run("slot-identity-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "blobidentity/_schema/cookwrite.py"
		const from = "body = kind_blob._encode(fd, target_value, fd.name)"
		text := string(planted[path])
		if !strings.Contains(text, from) {
			t.Fatal("blob slot control anchor absent")
		}
		planted[path] = []byte(strings.Replace(text, from, "if current.blobs and current.blobs[-1].value is target_value:\n                current.blobs.append(current.blobs[-1])\n                continue\n            "+from, 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for name, runner := range cookPins() {
			got := strings.Split(strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))), "\n")
			if len(got) != 4 || got[0] != expected[0] || got[1] != expected[1] || got[2] == expected[2] || got[3] == expected[3] {
				t.Errorf("%s: blob slot control did not isolate the fault", name)
			}
		}
	})
}
