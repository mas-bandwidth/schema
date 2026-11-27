package pythontable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"

	"github.com/mas-bandwidth/schema/v3/internal/testproc"
)

func TestPythonCookBlobSlotsCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned interpreters")
	u := loadUnitFiles(t, filepath.Join("..", "..", "..", "tables", "blobs"))
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
	source := `#include "AssetsTable.h"
#include <cstdio>
#include <cstring>
#include <vector>
using namespace blobdemo;
int main() {
 const char *text = "{\"name\":\"twin\",\"thumb\":\"ECAwQA==\",\"alias\":\"ECAwQA==\"}";
 CatalogBuilder builder; TableReport report;
 if (!CatalogFromJson(builder, text, (int64_t)std::strlen(text), &report)) return 1;
 auto n = CatalogCookMeasure(builder); if (n <= 0) return 3;
 for (auto order : {TableByteOrder::Little, TableByteOrder::Big}) {
  std::vector<unsigned char> out((size_t)n); if (!CatalogCook(builder, out.data(), out.size(), order)) return 4;
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
	probe := pyrun.ImportPrelude + `from blobdemo import Catalog
from blobdemo._schema import cook
r = Catalog(); r.name = "twin"; blob = bytearray(b"\x10\x20\x30\x40"); r.thumb = blob; r.alias = blob
for order in (1, 2): print(cook.cook(r, order).hex())
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), pyDir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || out != string(want) {
			t.Errorf("%s blob slot C++/Python mismatch: %v\n%s\n%s\n%s", pin.name, err, stderr, out, want)
		}
	}
}
