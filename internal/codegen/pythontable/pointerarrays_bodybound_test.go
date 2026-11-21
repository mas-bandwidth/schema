package pythontable

import (
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

// A count that fits int32 but has no element bytes must refuse before it can
// allocate a list proportional to the hostile count.
func TestPythonVtPointerArrayBodyBound(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadNamedUnit(t, "PtrArr.schema", pointerArraysSchema)
	var id uint64
	for _, f := range u.Tables["Root"].Fields {
		if f.Name == "list_nodes" {
			id = ir.TableFieldWireId(f)
		}
	}
	if id == 0 {
		t.Fatal("list_nodes absent")
	}
	// 32M slots (~256 MiB of PyObject* slots): fits int32, trips the 192 MiB
	// RLIMIT_AS ceiling on Linux before touching memory, and stays bounded on
	// macOS (where XNU rejects RLIMIT_AS with EINVAL) while tracemalloc catches
	// the list allocation as MemoryError.
	payload := appendUleb([]byte{17}, 32<<20)
	body := appendUleb([]byte{1, 14}, uint64(len(payload)))
	body = append(body, payload...)
	body = append(body, 0)
	wire := buildForm1File(body, []uint64{id})
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `import resource
import sys
import tracemalloc
from ptrarr import Root, Refused
if sys.platform != "darwin":
    resource.setrlimit(resource.RLIMIT_AS, (192 * 1024 * 1024, 192 * 1024 * 1024))
with open("wire", "rb") as handle:
    data = handle.read()
tracemalloc.start()
tracemalloc.reset_peak()
base = tracemalloc.get_traced_memory()[0]
try:
    Root.load(data)
except Refused as err:
    _, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    if peak - base > 1024 * 1024:
        raise MemoryError(f"allocated {peak - base} bytes before refusing")
    assert err.reason == "wire_damaged", err.reason
else:
    tracemalloc.stop()
    raise AssertionError("accepted body with too few element bytes")
print("bounded")
`
	const path = "ptrarr/_schema/kind_pointer_array.py"
	const check = "        if count > sub.end - sub.pos:\n            raise Refused(\"wire_damaged\", \"a count the array body cannot cover\")\n"
	if strings.Count(string(files[path]), check) != 1 {
		t.Fatal("body-bound control anchor missing")
	}
	for _, planted := range []bool{false, true} {
		selected := maps.Clone(files)
		if planted {
			selected[path] = []byte(strings.Replace(string(selected[path]), check, "", 1))
		}
		dir := t.TempDir()
		writeTree(t, dir, selected)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
			if !planted && (err != nil || strings.TrimSpace(out) != "bounded") {
				t.Errorf("%s: %v %s %s", pin.name, err, out, stderr)
			}
			if planted && (err == nil || !strings.Contains(stderr, "MemoryError")) {
				t.Errorf("%s: removed body check did not expose allocation: %v %s %s", pin.name, err, out, stderr)
			}
		}
	}
}
