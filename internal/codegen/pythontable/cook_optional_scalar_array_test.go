package pythontable

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
)

func TestPythonCookOptionalScalarArrays(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookoptionalarray
 enum Shade { Red, Green, Blue }
 table Root {
  shades ?[2]Shade
  values ?[2]float32
  doubles ?[..3]float64
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from cookoptionalarray import Root
from cookoptionalarray._schema import cook
import sys
r = Root(); r.shades = [1, 2]; r.values = [1.5, -2.25]; r.doubles = [-0.0, 3.125]
print(r.save().hex())
for order in (1,2):
    data = cook.cook(r, order)
    if (order == 1) == (sys.byteorder == "little"):
        cook.check_cook(Root, data)
    print(data.hex())
`
	for name, runner := range cookPins() {
		lines := strings.Split(strings.TrimSpace(string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))), "\n")
		if len(lines) != 3 {
			t.Fatalf("%s: expected wire and two cooks, got %q", name, lines)
		}
		wire, err := hex.DecodeString(lines[0])
		if err != nil {
			t.Fatal(err)
		}
		m := tabletext.NewModel(u)
		root := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if err := decodeCookWire(m, root, wire, &report); err != nil {
			t.Fatal(err)
		}
		for i, big := range []bool{false, true} {
			want, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			if lines[i+1] != hex.EncodeToString(want) {
				t.Errorf("%s order %d: optional scalar array cook differs", name, i+1)
			}
		}
	}

	t.Run("presence-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookoptionalarray/_schema/cookwrite.py"
		const anchor = "buf[base + fd.pieces[-1][0]] = 1"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("presence control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "buf[base + fd.pieces[-1][0]] = 0", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "absent optional has nonzero payload") {
				t.Errorf("%s: optional control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})

}
