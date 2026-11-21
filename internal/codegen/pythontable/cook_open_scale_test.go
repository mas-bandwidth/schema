package pythontable

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

// TestPythonCookOpenFunctionalScale uses sparse mapped files, so opening each
// size touches the header but does not materialize the untouched region.
func TestPythonCookOpenFunctionalScale(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and real monotonic timing")
	u := loadUnit(t, "package openscale\nfixed table Root {\n x int32\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `import mmap, time
from openscale import Root
from openscale._schema import cook
header = bytearray(cook.cook(Root())[:64])
for n in (1 << 20, 100 << 20, 1 << 30):
    header[24:32] = (n - 64).to_bytes(8, "little")
    header[32:40] = (0).to_bytes(8, "little")
    with open("sparse", "w+b") as f:
        f.truncate(n); f.seek(0); f.write(header); f.flush()
        with mmap.mmap(f.fileno(), n, access=mmap.ACCESS_READ) as mm:
            view = memoryview(mm)
            timings = []
            for _ in range(3):
                start = time.perf_counter_ns()
                for _ in range(4000): cook.open_cook(Root, view)
                timings.append((time.perf_counter_ns() - start) / 4000)
            print(n, min(timings))
            view.release()
`
	for name, runner := range cookPins() {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		lines := strings.Fields(out)
		if len(lines) != 6 {
			t.Fatalf("%s: expected three samples, got %q", name, out)
		}
		lo, hi := cookScaleRange(t, name, lines)
		if hi > 5*lo {
			t.Errorf("%s: opening time grew more than fivefold: %s", name, out)
		}
		t.Logf("%s ns/open: %s", name, strings.ReplaceAll(strings.TrimSpace(out), "\n", "; "))
	}
	planted := cloneFiles(files)
	const path = "openscale/_schema/cook.py"
	const anchor = "    view = _buffer(data)\n"
	text := string(planted[path])
	if !strings.Contains(text, anchor) {
		t.Fatal("scale control anchor absent")
	}
	planted[path] = []byte(strings.Replace(text, anchor, anchor+"    sum(range((len(view) // 1048576) * 10))\n", 1))
	dir := t.TempDir()
	writeTree(t, dir, planted)
	out := string(pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
	lines := strings.Fields(out)
	if len(lines) != 6 {
		t.Fatalf("scale control: got %q", out)
	}
	lo, hi := cookScaleRange(t, "scale control", lines)
	if hi <= 5*lo {
		t.Fatalf("scale negative control did not expose file-size work: %s", out)
	}
}

func cookScaleRange(t *testing.T, name string, lines []string) (float64, float64) {
	t.Helper()
	var lo, hi float64
	for i := 0; i < 6; i += 2 {
		v, err := strconv.ParseFloat(lines[i+1], 64)
		if err != nil || v <= 0 {
			t.Fatalf("%s: invalid sample %q", name, lines[i+1])
		}
		if lo == 0 || v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}
