package pythontable

import (
	"bytes"
	"fmt"
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
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cppWideWire reads the same declared values with generated C++ and saves form 1.
func cppWideWire(t *testing.T, u *ir.Unit, c wideVTConstruct) []byte {
	t.Helper()
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
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.h") {
			header = name
		}
		if !strings.Contains(name, "/") && strings.HasSuffix(name, "Table.cpp") {
			sources = append(sources, filepath.Join(dir, name))
		}
	}
	if header == "" {
		t.Fatal("C++ table header absent")
	}
	ser, err := filepath.Abs("../../../../serialize")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ser, "serialize.h")); err != nil {
		t.Fatalf("C++ serialize sibling: %v", err)
	}
	init := fmt.Sprintf("%s value; %sReset(value);", c.root, c.root)
	if ir.VariableTables(u)[c.root] {
		init = fmt.Sprintf("%sBuilder value;", c.root)
	}
	source := fmt.Sprintf(`#include <cstdio>
#include <cstdint>
#include <cstring>
#include <vector>
#include "%s"
using namespace %s;
int main() {
  %s
  const char *text = R"json(%s)json";
  TableReport report{};
  if (!%sFromJson(value, text, (int64_t)std::strlen(text), &report)) return 2;
  int64_t n = %sMeasure(value);
  if (n <= 0) return 3;
  std::vector<uint8_t> wire((size_t)n);
  if (%sSave(value, wire.data(), n) != n) return 4;
  return std::fwrite(wire.data(), 1, wire.size(), stdout) == wire.size() ? 0 : 5;
}
`, header, u.Package, init, c.json, c.root, c.root, c.root)
	path := filepath.Join(dir, "probe.cpp")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{"-std=c++17", "-O1", "-I" + dir, "-I" + ser, path}, sources...)
	args = append(args, "-o", bin)
	if out, err := exec.Command("c++", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C++ oracle: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("run C++ oracle: %v", err)
	}
	return out
}

// wideDecodedChecks names expected raw values independently of both writers.
var wideDecodedChecks = map[string]string{
	"wide_field_alone": `assert value.x == -(1 << 127) and value.y == (1 << 128) - 1
assert value.f == -12345 * 65536 - 32768 and value.g == 832`,
	"wide_with_base_kinds": `assert value.b is True and value.i == -7 and value.u == (1 << 64) - 1 and value.h == (1 << 40) - 1
assert value.x == -(1 << 127) and value.y == (1 << 128) - 1 and value.f == -12345 * 65536 - 32768 and value.g == 832`,
	"wide_every_width": `assert (value.f8, value.f16, value.f32, value.f64, value.f128) == (-7, 1234, -123456, 1099511627779, -999999 * 65536 - 32768)
assert (value.u8, value.u16, value.u32, value.u64, value.u128) == (250, 65000, 4000000000, 4611686018427387911, 1999999 * 65536 + 32768)
assert value.i == 123456789012345678901234567890 and value.u == 123456789012345678901234567890`,
	"wide_nested_table": `assert value.tag is True and value.inner.x == 123456789012345678901234567890 and value.inner.y == -12 * 65536 - 16384
assert value.u == 12345678901234567890`,
	"wide_pointer_closure": `assert value.head is not None and value.head.v == -(1 << 127) and value.head.f == -24
assert value.head.next is not None and value.head.next.v == 1 and value.head.next.f == 36
assert value.n == (1 << 128) - 1`,
}

// TestPythonVtWideCppOracle compares independently generated C++ form-1 bytes
// with the engine and with Python's strict load/save under both pinned versions.
func TestPythonVtWideCppOracle(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "generated C++ and both pinned Python interpreters")
	for _, c := range wideVTConstructs {
		t.Run(c.name, func(t *testing.T) {
			checks, ok := wideDecodedChecks[c.name]
			if !ok || checks == "" {
				t.Fatalf("missing decoded checks for %s", c.name)
			}
			u := loadUnit(t, c.schema)
			wire := cppWideWire(t, u, c)
			if goWire := wideEngineWire(t, u, c.root, c.json); !bytes.Equal(wire, goWire) {
				t.Fatalf("C++ and Go wire differ: C++ %x Go %x", wire, goWire)
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			for _, pin := range []struct {
				name string
				path func(testing.TB) string
			}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
				out, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(strings.Replace(wideProbeSource(u.Package, c.root), "before = repr(value)", checks+"\nbefore = repr(value)", 1)), "wire": wire})
				if err != nil || strings.TrimSpace(out) != fmt.Sprintf("same %d", len(wire)) {
					t.Errorf("%s: %v stdout=%s stderr=%s", pin.name, err, out, stderr)
				}
			}
		})
	}
}
