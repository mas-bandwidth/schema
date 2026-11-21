package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

func TestPythonCookCountedUnionPadding(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookunionpadding
 union Choice { yes bool }
 table Root { choices [..1]Choice }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from cookunionpadding import Root, Refused
from cookunionpadding._schema import cook
r = Root(); data = bytearray(cook.cook(r))
f = r.descriptor.fields[0]
end = f.pieces[0][0] + f.pieces[0][1]
assert end < f.pieces[1][0], f.pieces
h = cook.open_cook(Root, data)
data[h.data_offset + end] = 1
try: cook.check_cook(Root, data)
except Refused: pass
else: raise AssertionError('counted-union-padding')
`
	for _, run := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
	}
	t.Run("padding-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookunionpadding/_schema/cook.py"
		const anchor = `if any(region[at + fd.bound * stride : count_at]):
                    raise Refused("bad_layout", "union array padding is not zero")`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("union padding control anchor missing")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "pass", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
			_, stderr, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "counted-union-padding") {
				t.Errorf("padding control missed fault: %v %s", err, stderr)
			}
		}
	})
}
