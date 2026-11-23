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

// TestPythonCookCppIdentity holds the fixed scalar and bounded text writer to
// generated C++ in both byte orders, under both pinned interpreters.
func TestPythonCookCppIdentity(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned Python interpreters")
	u := loadUnit(t, `package cookcpp
enum Shade { Red, Blue, Green }
flags Traits { Fast, Strong }
union Choice {
    number int32
    ping
}
fixed table Element { x int32 }
fixed table Fixture
{
    number int32
    name string(8)
    data bytes(8)
    shade Shade
    traits Traits
    pairs [2]Element
    counted [..2]Element
    choice Choice
    f32 float32
    f64 float64
    samples [3]float32
    maybe ?float32
    wide int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
    huge uint128
    q fixed(16,16) | min = -8, max = 8
    qwide fixed(112,16) | min = -1000, max = 1000
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
	source := `#include "RecordsTable.h"
#include <cstdio>
#include <cstring>
#include <vector>
using namespace cookcpp;
int main() {
 Fixture v{}; v.number = -7; std::memcpy(v.name, "abc", 3); v.name_length = 3;
 v.data[0] = 0; v.data[1] = 255; v.data_length = 2;
 v.shade = Shade::Blue; v.traits = 3;
 v.choice.type = ChoiceType::Number; v.choice.number = -13;
 v.f32 = 1.5f; v.f64 = -0.0; v.samples[0] = -0.0f; v.samples[1] = 2.5f; v.samples[2] = -3.25f; v.maybe = -1.25f; v.maybe_present = true;
 v.wide = -(serialize::int128_t(1) << 100) + 42; v.huge = (serialize::uint128_t(1) << 120) + 77;
 v.q = -98304; v.qwide = serialize::int128_t(-655360);
 v.pairs[0].x = 3; v.pairs[1].x = -4; v.counted[0].x = 42; v.counted_count = 1;
 auto n = FixtureCookMeasure(v); if (n <= 0) return 1;
 for (auto order : {TableByteOrder::Little, TableByteOrder::Big}) {
  std::vector<unsigned char> out((size_t)n);
  if (!FixtureCook(v, out.data(), out.size(), order)) return 2;
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
	probe := pyrun.ImportPrelude + `from cookcpp import Fixture, Element
from cookcpp._schema import cook
v = Fixture(); v.number = -7; v.name = "abc"; v.data = b"\x00\xff"; v.shade = 2; v.traits = 3
v.choice.type = 1; v.choice.number = -13
v.f32 = 1.5; v.f64 = -0.0; v.samples = [-0.0, 2.5, -3.25]; v.maybe = -1.25
v.wide = -(1 << 100) + 42; v.huge = (1 << 120) + 77
v.q = -98304; v.qwide = -655360
v.pairs[0].x = 3; v.pairs[1].x = -4; v.counted = [Element()]; v.counted[0].x = 42
for order in (1, 2): print(cook.cook(v, order).hex())
`
	for _, pin := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, pin.path(t), pyDir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || out != string(want) {
			t.Errorf("%s: Python/C++ cook bytes differ: %v\n%s\nPython: %s\nC++: %s", pin.name, err, stderr, out, want)
		}
	}
}
