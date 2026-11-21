package pythontable

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cookOpenScaleSizes are the file sizes every probe opens, smallest first.
const cookOpenScaleSizes = "(1 << 20, 100 << 20, 1 << 30)"

// cookOpenScalePrelude maps one sparse file per size: a valid header and an
// untouched hole, so an O(1) open touches the header page and nothing else.
const cookOpenScalePrelude = `import mmap, resource, time
from openscale import Root
from openscale._schema import cook
SIZES = ` + cookOpenScaleSizes + `
header = bytearray(cook.cook(Root())[:64])
def sparse(n):
    header[24:32] = (n - 64).to_bytes(8, "little")
    header[32:40] = (0).to_bytes(8, "little")
    path = "sparse-%d" % n
    with open(path, "w+b") as f:
        f.truncate(n); f.seek(0); f.write(header); f.flush()
    return path
paths = [sparse(n) for n in SIZES]
def mapped(path, n):
    f = open(path, "rb")
    mm = mmap.mmap(f.fileno(), n, access=mmap.ACCESS_READ)
    return f, mm, memoryview(mm)
def unmap(f, mm, view):
    view.release(); mm.close(); f.close()
`

// cookOpenTimeProbe prints each size's best per-open CPU time. Rounds
// interleave the sizes, so load that comes and goes lands on every size alike
// rather than on whichever size was measured during it, and thread CPU time
// leaves out the time the runner spends preempted by other jobs.
const cookOpenTimeProbe = cookOpenScalePrelude + `maps = [mapped(p, n) for p, n in zip(paths, SIZES)]
best = [None] * len(SIZES)
for _ in range(7):
    for i, (f, mm, view) in enumerate(maps):
        start = time.thread_time_ns()
        for _ in range(2000): cook.open_cook(Root, view)
        ns = (time.thread_time_ns() - start) / 2000
        best[i] = ns if best[i] is None else min(best[i], ns)
for n, ns in zip(SIZES, best):
    print(n, ns)
for m in maps: unmap(*m)
`

// cookOpenFaultProbe prints, per size, the fewest page faults one open takes
// on a fresh mapping: a size-independent count that no runner load moves.
// Reading the file faults its pages in; opening it in O(1) faults the header.
const cookOpenFaultProbe = cookOpenScalePrelude + `warm = mapped(paths[0], SIZES[0])
cook.open_cook(Root, warm[2])
unmap(*warm)
def faults():
    r = resource.getrusage(resource.RUSAGE_SELF)
    return r.ru_minflt + r.ru_majflt
for p, n in zip(paths, SIZES):
    fewest = None
    for _ in range(5):
        f, mm, view = mapped(p, n)
        before = faults()
        cook.open_cook(Root, view)
        taken = faults() - before
        unmap(f, mm, view)
        fewest = taken if fewest is None else min(fewest, taken)
    print(n, fewest)
`

// TestPythonCookOpenFunctionalScale uses sparse mapped files, so opening each
// size touches the header but does not materialize the untouched region. Two
// meters hold the open to O(1) in the file's size, each under the fivefold
// bound: the page faults one open takes (exact, load-proof: catches reading
// the file) and the per-open thread CPU time (catches size-proportional
// compute that touches no page). Each meter has its own reversed witness.
func TestPythonCookOpenFunctionalScale(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and real monotonic timing")
	u := loadUnit(t, "package openscale\nfixed table Root {\n x int32\n}\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	run := func(runner cookRunner, tree map[string][]byte, probe string) string {
		dir := t.TempDir()
		writeTree(t, dir, tree)
		return string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probe)}))
	}
	for name, runner := range cookPins() {
		out := run(runner, files, cookOpenFaultProbe)
		if lo, hi := cookScaleRange(t, name+" faults", out, 1); hi > 5*lo {
			t.Errorf("%s: page faults per open grew more than fivefold: %s", name, out)
		}
		t.Logf("%s faults/open: %s", name, strings.ReplaceAll(strings.TrimSpace(out), "\n", "; "))
		out = run(runner, files, cookOpenTimeProbe)
		if lo, hi := cookScaleRange(t, name+" time", out, 0); hi > 5*lo {
			t.Errorf("%s: opening time grew more than fivefold: %s", name, out)
		}
		t.Logf("%s cpu ns/open: %s", name, strings.ReplaceAll(strings.TrimSpace(out), "\n", "; "))
	}

	// Reversed witness, faults: an open that reads the whole file.
	out := run(pyrun.Run, plantCookOpen(t, files, "    __import__(\"zlib\").crc32(view)\n"), cookOpenFaultProbe)
	if lo, hi := cookScaleRange(t, "fault control", out, 1); hi <= 5*lo {
		t.Fatalf("fault negative control did not expose a whole-file read: %s", out)
	}
	// Reversed witness, time: an open whose work grows with the file's
	// length but touches no page beyond the header.
	out = run(pyrun.Run, plantCookOpen(t, files, "    sum(range((len(view) // 1048576) * 10))\n"), cookOpenTimeProbe)
	if lo, hi := cookScaleRange(t, "time control", out, 0); hi <= 5*lo {
		t.Fatalf("time negative control did not expose file-size work: %s", out)
	}
}

// plantCookOpen returns files with line inserted into the open path, after the
// buffer is taken.
func plantCookOpen(t *testing.T, files map[string][]byte, line string) map[string][]byte {
	t.Helper()
	planted := cloneFiles(files)
	const path = "openscale/_schema/cook.py"
	const anchor = "    view = _buffer(data)\n"
	text := string(planted[path])
	if !strings.Contains(text, anchor) {
		t.Fatal("scale control anchor absent")
	}
	planted[path] = []byte(strings.Replace(text, anchor, anchor+line, 1))
	return planted
}

// cookScaleRange parses a probe's three "size value" lines and returns the
// smallest and largest value. A value below floor counts as floor: zero page
// faults and one are the same O(1). With floor 0 every value must be positive.
func cookScaleRange(t *testing.T, name, out string, floor float64) (float64, float64) {
	t.Helper()
	lines := strings.Fields(out)
	if len(lines) != 6 {
		t.Fatalf("%s: expected three samples, got %q", name, out)
	}
	var lo, hi float64
	for i := 0; i < 6; i += 2 {
		v, err := strconv.ParseFloat(lines[i+1], 64)
		if err != nil || v < 0 || (floor == 0 && v == 0) {
			t.Fatalf("%s: invalid sample %q", name, lines[i+1])
		}
		if v < floor {
			v = floor
		}
		if i == 0 || v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}
