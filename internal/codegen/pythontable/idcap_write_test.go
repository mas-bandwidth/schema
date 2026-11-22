package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestPythonWriterRefusesMoreThan4096Ids holds the Python variable-form writer
// to the id table's maximum (docs/SPEC-TABLES.md §3, ruling R8). Python is on
// neither list of the write-side rule, so it refuses in every build: save()
// raises InvalidValue for a value that needs 4097 ids and writes nothing. The
// values that need 4095 and 4096 are written, carry that count word, and read
// back. The unit is tablewire.IdCapSchema.
func TestPythonWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	files, err := Generate(loadUnit(t, tablewire.IdCapSchema()))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import idcap
def value(x, y):
    v = idcap.Root()
    for k in range(1, 4095):
        v.keys[k] = 1
    v.x = x
    v.y = y
    return v
for x, ids in ((0, 4095), (1, 4096)):
    b = value(x, 0).save()
    assert int.from_bytes(b[-8:], "little") == ids, ids
    assert idcap.Root.load(b).x == x
try:
    value(1, 1).save()
except idcap.InvalidValue:
    print("ok")
else:
    raise AssertionError("4097 ids were written")
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(out) != "ok" {
			t.Fatalf("%v\n%s\n%s", err, out, stderr)
		}
	}
}
