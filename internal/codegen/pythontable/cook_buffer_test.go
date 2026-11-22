package pythontable

import (
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"strings"
	"testing"
)

func TestPythonCookBufferShape(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, cookSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	oracle := makeCookOracle(t, u, files)
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from cookcorpus import Root, Refused
from cookcorpus._schema import cook
with open("sample", "rb") as sample:
    raw = bytearray(sample.read())
assert cook.open_cook(Root, memoryview(raw)) is not None
for view in (memoryview(raw)[::2], memoryview(raw).cast("B", shape=[len(raw), 1])):
    try: cook.open_cook(Root, view)
    except Refused as err: assert err.reason == "invalid_buffer"
    else: raise AssertionError("invalid memoryview accepted")
print("OK")
`
	for name, runner := range cookPins() {
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "sample": oracle.cooks["full"][0]}))
		if !strings.Contains(out, "OK\n") {
			t.Errorf("%s: %s", name, out)
		}
	}
	t.Run("noncontiguous-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookcorpus/_schema/cook.py"
		const from = "data.format != \"B\" or data.ndim != 1 or not data.contiguous"
		text := string(planted[path])
		if !strings.Contains(text, from) {
			t.Fatal("buffer control anchor absent")
		}
		planted[path] = []byte(strings.Replace(text, from, "data.format != \"B\"", 1))
		copyDir := t.TempDir()
		writeTree(t, copyDir, planted)
		for name := range cookPins() {
			var interpreter func(testing.TB) string
			if name == "python-3.11.18" {
				interpreter = pyrun.Floor
			} else {
				interpreter = pyrun.Interpreter
			}
			_, stderr, err := pyrun.Try(t, interpreter(t), copyDir, "main.py", map[string][]byte{"main.py": []byte(probe), "sample": oracle.cooks["full"][0]})
			if err == nil || (!strings.Contains(stderr, "AssertionError") && !strings.Contains(stderr, "BufferError")) {
				t.Errorf("%s: buffer control did not fail by name: %v\n%s", name, err, stderr)
			}
		}
	})

}
