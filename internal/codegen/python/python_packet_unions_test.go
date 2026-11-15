// Tagged unions of type payloads and named payload-free arms (SPEC §4.8),
// with the Python leg's half of test/packet-void: the carried-kind fixture
// reads and writes its SPEC-calculated wire byte for byte, a void arm spends
// exactly its tag bits, every arm is built at its declared default (SPEC §5),
// out-of-contract writes raise InvalidValue and truncated or hostile input
// raises Refused, under both pinned interpreters and mypy --strict. A planted
// extra bit on a void arm's write turns the probe red by name.
package python

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// packetVoidPythonFixture returns the Python half of test/packet-void: a
// schema whose unions use only the kinds the packet core carries, the probe
// that reads and writes it, and the SPEC-calculated wire it must match.
func packetVoidPythonFixture(t *testing.T) (schema, probe, goldens string) {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "test", "packet-void")
	return filepath.Join(dir, "Python.schema"),
		filepath.Join(dir, "python", "main.py"),
		filepath.Join(dir, "python", "goldens.txt")
}

func loadPacketVoidPython(t *testing.T) (files map[string][]byte, pkg string, probe, goldens []byte) {
	t.Helper()
	schema, probePath, goldensPath := packetVoidPythonFixture(t)
	u := loadPythonUnit(t, schema)
	generated, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the packet-void fixture: %v", err)
	}
	probe, err = os.ReadFile(probePath)
	if err != nil {
		t.Fatal(err)
	}
	goldens, err = os.ReadFile(goldensPath)
	if err != nil {
		t.Fatal(err)
	}
	if blocker := rootBlocker(u, "Mixed"); blocker != "" {
		t.Fatalf("the union root is on the golden harness's skip list, blocked by %s", blocker)
	}
	return generated, u.Package, probe, goldens
}

// TestPythonPacketUnions holds the union kind to test/packet-void's wire under
// both pinned interpreters, with the analyzer over the generated tree.
func TestPythonPacketUnions(t *testing.T) {
	t.Parallel()
	files, pkg, probe, goldens := loadPacketVoidPython(t)
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, files)
			got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{
				"main.py":     probe,
				"goldens.txt": goldens,
			})))
			if got != "ok" {
				t.Fatalf("packet-void unions under %s:\n%s", run.name, got)
			}
			out, err := pyrun.Analyze(t, dir, pkg)
			if err != nil || !strings.Contains(out, "Success: no issues found") {
				t.Fatalf("packet-void unions under %s: mypy --strict: %v\n%s", run.name, err, out)
			}
		})
	}
}

// TestPythonPacketUnionsNegativeControl plants one extra set bit on the Mixed
// union's payload-free arms on the write path alone, in a copy of the generated
// code, and requires the probe to go red by name.
func TestPythonPacketUnionsNegativeControl(t *testing.T) {
	t.Parallel()
	files, pkg, probe, goldens := loadPacketVoidPython(t)
	module := pkg + "/Python.py"
	text := string(files[module])
	const anchor = "        if self.type == 2:\n            self.payload._s_write(stream)\n"
	if strings.Count(text, anchor) != 1 {
		t.Fatalf("the Mixed writer anchor matched %d times:\n%s", strings.Count(text, anchor), text)
	}
	const planted = "        if self.type == 1 or self.type == 3:\n" +
		"            _s_ri.value = 1\n" +
		"            _s_serialize_int(_s_ri, 0, 1)\n"
	files[module] = []byte(strings.Replace(text, anchor, planted+anchor, 1))
	dir := t.TempDir()
	writeTree(t, dir, files)
	out, _, err := pyrun.Try(t, pyrun.Interpreter(t), dir, "main.py", map[string][]byte{
		"main.py":     probe,
		"goldens.txt": goldens,
	})
	if err == nil || !strings.Contains(out, "FAILED: write mixed-ping") {
		t.Fatalf("the planted void-arm bit did not go red on the write marker (err %v):\n%s", err, out)
	}
}
