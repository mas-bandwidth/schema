package pythontable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonCookExtentCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned interpreters")
	u := loadUnit(t, `package cookextent
 table Root {
   scores []int32
   values map[int32]int32
   samples []float32
   precise []float64
 }
`)
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
	source := `#include "CoreTable.h"
#include <cstdio>
#include <cstring>
#include <vector>
using namespace cookextent;
int main() {
 const char *text = "{\"scores\":[12,-7,42],\"values\":{\"-2\":11,\"7\":-3},\"samples\":[1.5,-2.25],\"precise\":[-0.0,3.125]}";
 RootBuilder builder; TableReport report;
 if (!RootFromJson(builder, text, (int64_t)std::strlen(text), &report)) return 1;
 auto n = RootCookMeasure(builder); if (n <= 0) return 3;
 for (auto order : {TableByteOrder::Little, TableByteOrder::Big}) {
  std::vector<unsigned char> out((size_t)n); if (!RootCook(builder, out.data(), out.size(), order)) return 4;
  for (auto b : out) std::printf("%02x", b); std::puts("");
 }
}`
	path := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c++17", "-O1", "-I" + dir, "-I" + ser, path}
	for name := range files {
		if !strings.Contains(name, "/") && strings.HasSuffix(name, ".cpp") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-o", bin)
	if out, err := exec.Command("c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++: %v\n%s", err, out)
	}
	want, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	py, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pyDir := t.TempDir()
	writeTree(t, pyDir, py)
	probe := pyrun.ImportPrelude + `from cookextent import Root
from cookextent._schema import cook
r = Root(); r.scores = [12, -7, 42]; r.values = {-2: 11, 7: -3}; r.samples = [1.5, -2.25]; r.precise = [-0.0, 3.125]
for order in (1, 2): print(cook.cook(r, order).hex())
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), pyDir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || out != string(want) {
			t.Errorf("%s extent C++/Python mismatch: %v\n%s\n%s\n%s", pin.name, err, stderr, out, want)
		}
	}
}
