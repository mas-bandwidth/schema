package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonCookDumpArrays(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookdumparray
 enum Shade { Red, Green, Blue }
 table Root {
  numbers [2]int16
  counted [..3]bool
  colors []Shade
  optional ?[2]int32
  words []string(8)
  free []?string
 }
 table FloatRoot { value ?float32 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from cookdumparray import Root, FloatRoot, Refused
from cookdumparray._schema import cook
r = Root(); r.numbers = [-3, 4]; r.counted = [True, False]; r.colors = [1, 2]; r.optional = [7, -9]; r.words = ["hello"]; r.free = ["text", None]
dump = cook.dump_cook(cook.check_cook(Root, cook.cook(r)))
for line in ('  numbers[0] = -3', '  numbers[1] = 4', '  counted[0] = true', '  counted#count = 2', '  colors[0] = 1', '  colors[1] = 2', '  colors#count = 2', '  optional[1] = -9', '  optional#present = true', '  words[0].value = "hello" len=5', '  free[0].value = "text" len=4'):
    assert line + '\n' in dump, (line, dump)
f = FloatRoot(); f.value = 1.5
try: cook.dump_cook(cook.check_cook(FloatRoot, cook.cook(f)))
except Refused as e: assert 'float' in str(e)
else: raise AssertionError('float dump did not refuse')
`
	for _, run := range cookPins() {
		run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
	}
	t.Run("array-negative-control", func(t *testing.T) {
		planted := cloneFiles(files)
		path := "cookdumparray/_schema/cook.py"
		anchor := `if codec in ("float32", "float64"):`
		if !strings.Contains(string(planted[path]), anchor) {
			t.Fatal("control anchor missing")
		}
		planted[path] = []byte(strings.Replace(string(planted[path]), anchor, `if False:`, 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []func(testing.TB) string{pyrun.Interpreter, pyrun.Floor} {
			_, stderr, err := pyrun.Try(t, pin(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "float dump did not refuse") {
				t.Errorf("control missed float fault: %v %s", err, stderr)
			}
		}
	})
}
