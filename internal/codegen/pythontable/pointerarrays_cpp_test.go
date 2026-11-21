package pythontable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"

	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestPythonVtPointerArraysCppIdentity holds shared node identity against the
// bytes produced by the generated C++ reference, on both pinned interpreters.
func TestPythonVtPointerArraysCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned Python interpreters")
	u := loadNamedUnit(t, "PtrArr.schema", pointerArraysSchema)
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := cpptable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	writeTree(t, dir, files)
	header := ""
	var sources []string
	for name := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" || len(sources) == 0 {
		t.Fatal("C++ table sources absent")
	}
	source := fmt.Sprintf(`#include <cstdio>
#include <cstring>
#include <vector>
#include "%s"
using namespace ptrarr;
int main() {
 const char *text = R"json({"head":{"&node":1,"label":42},"list_nodes":[{"&node":1},null],"fixed_nodes":[null,{"&node":1},null],"bounded_nodes":[{"&node":1}]})json";
 RootBuilder value; TableReport report{};
 if (!RootFromJson(value,text,(int64_t)std::strlen(text),&report)) return 2;
 int64_t n=RootMeasure(value); if(n<=0) return 3; std::vector<uint8_t> wire((size_t)n);
 if(RootSave(value,wire.data(),n)!=n) return 4;
 return std::fwrite(wire.data(),1,wire.size(),stdout)==wire.size()?0:5;
}
`, header)
	path := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + dir, "-I" + ser, path}, sources...)
	args = append(args, "-o", bin)
	if out, err := testproc.Command(t, "c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++: %v\n%s", err, out)
	}
	wire, err := testproc.Command(t, bin).Output()
	if err != nil {
		t.Fatalf("C++ save: %v", err)
	}
	py, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pyDir := t.TempDir()
	writeTree(t, pyDir, py)
	probe := pyrun.ImportPrelude + `from ptrarr import Root
with open("wire", "rb") as handle: wire = handle.read()
v = Root.load(wire)
assert v.head is not None and v.head.label == 42
assert len(v.list_nodes) == 2 and v.list_nodes[0] is v.head and v.list_nodes[1] is None
assert v.fixed_nodes == [None, v.head, None] and v.fixed_nodes[1] is v.head
assert len(v.bounded_nodes) == 1 and v.bounded_nodes[0] is v.head
assert v.save() == wire and v.measure() == len(wire)
print("ok")
`
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, p.path(t), pyDir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil || out != "ok\n" {
			t.Errorf("%s: %v %s %s", p.name, err, out, stderr)
		}
	}
}
