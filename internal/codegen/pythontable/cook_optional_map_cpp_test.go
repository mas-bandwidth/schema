package pythontable

import (
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

func TestPythonCookOptionalMapsCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned interpreters")
	u := loadUnit(t, optionalMapSchema)
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
	source := `#include "RecordsTable.h"
#include <cstdio>
#include <cstring>
#include <vector>
using namespace optmap;
int main() {
 const char *texts[] = {"{}", "{\"tags\":{}}", "{\"tags\":{\"a\":-7,\"z\":3}}"};
 for (const char *text : texts) {
  RootBuilder builder; TableReport report;
  if (!RootFromJson(builder, text, (int64_t)std::strlen(text), &report)) return 1;
  auto n = RootCookMeasure(builder); if (n <= 0) return 3;
  for (auto order : {TableByteOrder::Little, TableByteOrder::Big}) {
   std::vector<unsigned char> out((size_t)n); if (!RootCook(builder, out.data(), out.size(), order)) return 4;
   for (auto b : out) std::printf("%02x", b); std::puts("");
  }
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
	if out, err := testproc.Command(t, "c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++: %v\n%s", err, out)
	}
	want, err := testproc.Command(t, bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	py, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pyDir := t.TempDir()
	writeTree(t, pyDir, py)
	probe := pyrun.ImportPrelude + `from optmap import Root
from optmap._schema import cook
absent = Root(); empty = Root(); empty.tags = {}; full = Root(); full.tags = {"a": -7, "z": 3}
for r in (absent, empty, full):
    for order in (1, 2): print(cook.cook(r, order).hex())
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), pyDir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || out != string(want) {
			t.Errorf("%s optional map C++/Python mismatch: %v\n%s\n%s\n%s", pin.name, err, stderr, out, want)
		}
	}
}
