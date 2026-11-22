package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonCookRequiredRange(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters")
	u := loadUnit(t, `package cookrequiredrange
 table Root { ver uint8 | min = 7, max = 7, required }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from cookrequiredrange import InvalidValue, Root
from cookrequiredrange._schema import cook
r = Root()
for operation in (lambda: cook.cook(r), lambda: cook.cook_measure(r)):
    try: operation()
    except InvalidValue: pass
    else: raise AssertionError("required out-of-range default accepted")
r.ver = 7
assert cook.cook_measure(r) == len(cook.cook(r))
print("ok")
`
	allCookPins := cookPins()
	if len(allCookPins) == 0 {
		t.Fatal("cookPins() is empty: the loop below would check nothing")
	}
	for name, runner := range allCookPins {
		got := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if strings.TrimSpace(got) != "ok" {
			t.Errorf("%s: %s", name, got)
		}
	}
	t.Run("required-range-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookrequiredrange/_schema/cookwrite.py"
		const anchor = `_validate(value, value.descriptor, "", set())`
		body := string(planted[path])
		if strings.Count(body, anchor) != 2 {
			t.Fatal("validation control anchors missing")
		}
		planted[path] = []byte(strings.ReplaceAll(body, anchor, "pass"))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "required out-of-range default accepted") {
				t.Errorf("%s control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})
}
