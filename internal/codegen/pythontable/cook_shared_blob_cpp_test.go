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

func TestPythonCookSharedBlobCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned interpreters")
	u := loadUnit(t, `package cookshared
 table Root {
   left *bytes
   right *bytes
   first *string
   second *string
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
using namespace cookshared;
int main() {
 const char *text = "{\"left\":\"aGVsbG8=\",\"right\":\"aGVsbG8=\",\"first\":\"shared\",\"second\":\"shared\"}";
 RootBuilder builder; TableReport report;
 if (!RootFromJson(builder, text, (int64_t)std::strlen(text), &report)) return 1;
 Root *root = builder.GetRoot(); if (!root) return 2; root->right = root->left; root->second = root->first;
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
	probe := pyrun.ImportPrelude + `from cookshared import BlobRef, Root
from cookshared._schema import cook
r = Root(); data = BlobRef(b"hello"); text = BlobRef("shared")
r.left = data; r.right = data; r.first = text; r.second = text
for order in (1, 2): print(cook.cook(r, order).hex())
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), pyDir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || out != string(want) {
			t.Errorf("%s shared blob C++/Python mismatch: %v\n%s\n%s\n%s", pin.name, err, stderr, out, want)
		}
	}
	t.Run("shared-blob-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(py)
		const path = "cookshared/_schema/cookwrite.py"
		const anchor = "identity = id(target_value) if isinstance(target_value, BlobRef) else None"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("shared blob control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "identity = None", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for name, runner := range cookPins() {
			got := runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if string(got) == string(want) {
				t.Errorf("%s: lost identity was not detected", name)
			}
		}
	})

}
