package pythontable_test

// Bounded memory and time on hostile inputs for the Python leg of schema
// (docs/SPEC-TABLES.md §16.9.4; docs/PORT-PYTHON.md, THE NUMBER MODEL, names
// tracemalloc as the allocation instrument). The generated Python text reader
// runs under both pinned interpreters on hostile documents: deep nesting,
// recursion, a huge count, a giant string and many unknown fields. Each shape
// must cost at most F times its input plus a fixed slack, and a number of
// executed lines linear in the input; the shapes the reader refuses must refuse
// by name, and no shape may crash.
//
// The planted-bad-pattern control raises the scanner's depth cap in a copy of
// the emitted runtime and requires the deep shape to stop refusing as malformed.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// pythonAllocationFactorF is the proportional budget: a read allocates at
// most F times the input size.
const pythonAllocationFactorF = 256.0

// pythonAllocationSlackKB is the fixed overhead above F*input, in bytes: the
// frozen runtime tables and one refusal's own objects are not chargeable per
// read.
const pythonAllocationSlackKB = 1024.0

const pythonResourceBoundsSchema = `package probe

table H
{
    n int32 | min = 0, max = 100000
    big [..1000]int32
    next *H
}
`

// pythonResourceBoundsDocs is one document per hostile dimension.
func pythonResourceBoundsDocs() map[string]string {
	chain := ""
	for i := 132; i >= 1; i-- {
		if chain == "" {
			chain = fmt.Sprintf(`{"&node": %d}`, i)
		} else {
			chain = fmt.Sprintf(`{"&node": %d, "next": %s}`, i, chain)
		}
	}
	return map[string]string{
		// deep nesting: more open lists than the scanner's depth cap of 128.
		"deep-nesting": strings.Repeat("[", 132) + strings.Repeat("]", 132),
		// huge count: far more elements than the counted array's bound.
		"huge-count": `{"big": [` + strings.Repeat("1,", 99999) + "1]}",
		// recursion: the parser descends one frame per pointer body.
		"recursion": chain,
		// giant string: far past the declared string size of any field.
		"giant-string": `"` + strings.Repeat("x", 70000) + `"`,
		// many unknown fields: 2000 undeclared keys, counted and skipped.
		"many-unknown-fields": pythonResourceBoundsUnknownFields(2000),
	}
}

// pythonResourceBoundsUnknownFields is one object of count undeclared keys.
func pythonResourceBoundsUnknownFields(count int) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := range count {
		if i != 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"zz%d":0`, i)
	}
	b.WriteByte('}')
	return b.String()
}

// pythonResourceBoundsWant is the documented verdict each shape must reach.
type pythonResourceBoundsWant struct {
	verdict string
	reason  string
}

func pythonResourceBoundsWants() map[string]pythonResourceBoundsWant {
	return map[string]pythonResourceBoundsWant{
		"deep-nesting":        {"REFUSED", "malformed"},
		"huge-count":          {"REFUSED", "count_over_bound"},
		"recursion":           {"REFUSED", "malformed"},
		"giant-string":        {"REFUSED", "kind_mismatch"},
		"many-unknown-fields": {"OK", ""},
	}
}

// pythonResourceBoundsProbe prints one tab-separated line per shape: name,
// verdict (OK / REFUSED / CRASH), refusal name, allocated bytes, executed line
// events and input bytes.
const pythonResourceBoundsProbe = `import sys
import tracemalloc

from probe import H, Refused

NAMES = ("deep-nesting", "huge-count", "recursion", "giant-string", "many-unknown-fields")


def read_text(name):
    with open(name, "rb") as handle:
        return handle.read().decode("utf-8")


def run(name, text):
    tracemalloc.start()
    tracemalloc.reset_peak()
    base = tracemalloc.get_traced_memory()[0]
    steps = [0]

    def trace(frame, event, arg):
        if event == "line":
            steps[0] += 1
        return trace

    sys.settrace(trace)
    try:
        H.from_json(text)
        verdict, reason = "OK", ""
    except Refused as exc:
        verdict, reason = "REFUSED", exc.reason
    except BaseException as exc:
        verdict, reason = "CRASH", type(exc).__name__ + ":" + str(exc)
    finally:
        sys.settrace(None)
    _, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    allocated = peak - base
    print(name + "\t" + verdict + "\t" + reason + "\t" + str(allocated) + "\t" + str(steps[0]) + "\t" + str(len(text.encode("utf-8"))))


for shape in NAMES:
    run(shape, read_text(shape))
`

// pythonResourceBoundsUnit generates the hostile unit once. A non-empty
// plantFrom edits a COPY of the emitted scanner (the negative control raises
// the depth cap), never the tree.
func pythonResourceBoundsUnit(t *testing.T, plantFrom, plantTo string) string {
	t.Helper()
	schemaDir := t.TempDir()
	schema := filepath.Join(schemaDir, "H.schema")
	if err := os.WriteFile(schema, []byte(pythonResourceBoundsSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := pythonHostileLoadUnit(t, schema)
	files, err := compiler.New().Generate(u, "python", compiler.Options{})
	if err != nil {
		t.Fatalf("generate python resource-bounds unit: %v", err)
	}
	if plantFrom != "" {
		files = maps.Clone(files)
		planted := false
		for name := range files {
			if !strings.HasSuffix(name, "/_schema/json_scan.py") {
				continue
			}
			src := string(files[name])
			if !strings.Contains(src, plantFrom) {
				t.Fatalf("%s does not carry the planted pattern %q", name, plantFrom)
			}
			files[name] = []byte(strings.ReplaceAll(src, plantFrom, plantTo))
			planted = true
		}
		if !planted {
			t.Fatalf("the backend emitted no _schema/json_scan.py")
		}
	}
	dir := t.TempDir()
	pythonHostileWriteTree(t, dir, files)
	return dir
}

type pythonResourceBoundsResult struct {
	verdict   string
	reason    string
	allocated int
	steps     int
	bytes     int
}

func pythonResourceBoundsRun(t *testing.T, dir string) map[string]pythonResourceBoundsResult {
	t.Helper()
	inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + pythonResourceBoundsProbe)}
	for name, text := range pythonResourceBoundsDocs() {
		inputs[name] = []byte(text)
	}
	got := map[string]pythonResourceBoundsResult{}
	for _, interpreter := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		stdout, stderr, err := pyrun.Try(t, interpreter, dir, "main.py", inputs)
		if err != nil {
			t.Fatalf("%s: the resource-bounds probe failed: %v\nstderr:\n%s\nstdout:\n%s", filepath.Base(interpreter), err, stderr, stdout)
		}
		for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) != 6 {
				t.Fatalf("bad resource-bounds probe line %q", line)
			}
			var r pythonResourceBoundsResult
			r.verdict, r.reason = fields[1], fields[2]
			if _, err := fmt.Sscanf(fields[3], "%d", &r.allocated); err != nil {
				t.Fatalf("%s: bad allocated bytes %q: %v", fields[0], fields[3], err)
			}
			if _, err := fmt.Sscanf(fields[4], "%d", &r.steps); err != nil {
				t.Fatalf("%s: bad steps %q: %v", fields[0], fields[4], err)
			}
			if _, err := fmt.Sscanf(fields[5], "%d", &r.bytes); err != nil {
				t.Fatalf("%s: bad bytes %q: %v", fields[0], fields[5], err)
			}
			if previous, ok := got[fields[0]]; ok {
				if previous.verdict != r.verdict || previous.reason != r.reason || previous.bytes != r.bytes {
					t.Fatalf("%s: the two pinned interpreters disagree: %+v vs %+v", fields[0], previous, r)
				}
				// Allocation and step counts are VM measurements, not values: hold
				// each interpreter to the budget and keep the larger of the two.
				r.allocated = max(r.allocated, previous.allocated)
				r.steps = max(r.steps, previous.steps)
			}
			got[fields[0]] = r
		}
	}
	return got
}

// TestPythonResourceBoundsHostileShapes: every shape refuses by its documented
// name or is accepted, allocates at most F times its input plus slack, and
// costs a number of executed lines linear in the input.
func TestPythonResourceBoundsHostileShapes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	dir := pythonResourceBoundsUnit(t, "", "")
	got := pythonResourceBoundsRun(t, dir)
	wants := pythonResourceBoundsWants()
	if len(wants) == 0 {
		t.Fatal("wants is empty: the loop below would check nothing")
	}
	for _, name := range slices.Sorted(maps.Keys(wants)) {
		r, ok := got[name]
		if !ok {
			t.Fatalf("shape %s: the probe wrote no line", name)
		}
		want := wants[name]
		if r.verdict != want.verdict || r.reason != want.reason {
			t.Fatalf("shape %s: %s %s, want %s %s", name, r.verdict, r.reason, want.verdict, want.reason)
		}
		budget := int(pythonAllocationFactorF*float64(r.bytes)) + int(pythonAllocationSlackKB*1024)
		if r.allocated > budget {
			t.Fatalf("shape %s allocated %d bytes, over the F*input + slack budget %d (F=%.0f)", name, r.allocated, budget, pythonAllocationFactorF)
		}
		stepBudget := 128*r.bytes + 1<<20
		if r.steps > stepBudget {
			t.Fatalf("shape %s cost %d line events, over the linear budget %d", name, r.steps, stepBudget)
		}
		t.Logf("python resource-bounds shape=%s input=%dB allocated=%dB budget=%dB steps=%d stepBudget=%d",
			name, r.bytes, r.allocated, budget, r.steps, stepBudget)
	}
}

// TestPythonResourceBoundsPlantedPattern is the planted-bad-pattern control:
// with the scanner's depth cap in place the deep shape refuses as malformed;
// with the cap raised in a copy of the emitted runtime it does not. The test
// is red when the harness cannot see the guard.
func TestPythonResourceBoundsPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	guarded, ok := pythonResourceBoundsRun(t, pythonResourceBoundsUnit(t, "", ""))["deep-nesting"]
	if !ok || guarded.verdict != "REFUSED" || guarded.reason != "malformed" {
		t.Fatalf("with the depth guard in place the deep-nesting shape must refuse malformed, got %+v", guarded)
	}
	planted, ok := pythonResourceBoundsRun(t, pythonResourceBoundsUnit(t, "if depth > 128:", "if depth > 100000:"))["deep-nesting"]
	if !ok {
		t.Fatal("the planted run wrote no deep-nesting line")
	}
	if planted.verdict == "REFUSED" && planted.reason == "malformed" {
		t.Fatalf("the planted pattern (depth cap raised) did not change the deep-nesting verdict: %+v; the harness cannot see the guard", planted)
	}
	t.Logf("planted-bad-pattern control: guarded REFUSED malformed, planted %s %s", planted.verdict, planted.reason)
}
