package pythontable

import (
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/internal/tablecook"
	"github.com/mas-bandwidth/schema/v3/internal/tabletext"
	"github.com/mas-bandwidth/schema/v3/internal/tablewire"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestPythonCookUnionBlobArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnitFiles(t, filepath.Join("..", "..", "..", "tables", "arms"))
	testPythonCookUnionBlobArm(t, u, false)
}

func TestPythonCookUnionBytesArm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package armdemo
 union Reach {
  text *bytes
 }
 table Gate {
  reach Reach
  other *bytes
 }
`)
	testPythonCookUnionBlobArm(t, u, true)
}

func testPythonCookUnionBlobArm(t *testing.T, u *ir.Unit, bytesArm bool) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from armdemo import Gate
from armdemo._schema import cook
import sys
r = Gate(); r.reach.type = 2; r.reach.text = "hello"
wire = r.save(); loaded = Gate.load(wire)
assert loaded.reach.text == "hello"
print(wire.hex())
for order in (1,2):
    data = cook.cook(r, order)
    if (order == 1) == (sys.byteorder == "little"):
        cook.check_cook(Gate, data)
    print(data.hex())
`
	if bytesArm {
		probe = strings.ReplaceAll(probe, "r.reach.type = 2; r.reach.text = \"hello\"", "r.reach.type = 1; r.reach.text = b\"hello\"")
		probe = strings.ReplaceAll(probe, "loaded.reach.text == \"hello\"", "loaded.reach.text == b\"hello\"")
		probe += `
from armdemo import BlobRef, InvalidValue
shared = BlobRef(b"hello"); r = Gate(); r.reach.type = 1; r.reach.text = shared; r.other = shared
try:
    r.save()
except InvalidValue as e:
    assert "blob_shared" in e.rule
else:
    raise AssertionError("shared union blob was accepted")
`
	}
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
		root := m.New(m.Lookup("Gate"))
		var report tabletext.Report
		if err := decodeCookWire(m, root, wire, &report); err != nil {
			t.Fatal(err)
		}
		if report.Refused {
			t.Fatalf("%s: engine refused wire: %s", name, report.Reason)
		}
		expectedWire, err := tablewire.EncodeVariable(m, root)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(expectedWire) != lines[0] {
			t.Errorf("%s: union blob wire differs from engine", name)
		}
		for i, big := range []bool{false, true} {
			want, err := tablecook.Cook(m, root, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			if lines[i+1] != hex.EncodeToString(want) {
				t.Errorf("%s order %d: union blob cook differs", name, i+1)
			}
		}
	}
	t.Run("blob-arm-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "armdemo/_schema/kind_union.py"
		const anchor = "body = kind_blob._encode(f, value, _save._render(path))\n            blobs.append(len(ctx.order) + 2)"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("union blob control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "body = kind_blob._encode(f, value, _save._render(path))\n            pass", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "byte buffer slot has no node index") {
				t.Errorf("%s: union blob control did not detect omission: %v %s", pin.name, err, stderr)
			}
		}
	})

}
