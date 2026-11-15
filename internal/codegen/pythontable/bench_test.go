package pythontable

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// TestPythonTableCoreShapeBench measures the two code shapes D1 names for the
// same scalar table (docs/PORT-PYTHON.md, THE TABLE FRAME): the generated
// straight-line pair against the runtime's descriptor walk, under both pinned
// interpreters. It is a functional test; the numbers it logs are the record in
// docs/PORT-PYTHON.md and it asserts only that both shapes agree.
func TestPythonTableCoreShapeBench(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, fixedUnionSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import time
import fixedunion.Core as Core
from fixedunion import Money
from fixedunion._schema.load import load_table
from fixedunion._schema.save import save_table

m = Money()
m.f8 = -7
m.f16 = 1234
m.f32 = -123456
m.f64 = (1 << 40) + 3
m.f128 = -(1000000 << 16) + 9
m.u8 = 250
m.u16 = 65000
m.u32 = 4000000000
m.u64 = (1 << 62) + 7
m.u128 = (1999999 << 16) + 11
data = m.save()
if Core._s_save_Money(m) != data:
    raise SystemExit("the straight-line save differs from the descriptor walk")
if Core._s_load_Money(Money, data) != m or load_table(Money, data) != m:
    raise SystemExit("the straight-line load differs from the descriptor walk")

N = 10000
ROUNDS = 5

def run(fn):
    best = None
    for _ in range(ROUNDS):
        start = time.perf_counter()
        for _ in range(N):
            fn()
        value = (time.perf_counter() - start) * 1e9 / N
        if best is None or value < best:
            best = value
    return best

def gen_save():
    Core._s_save_Money(m)

def desc_save():
    save_table(m)

def gen_load():
    Core._s_load_Money(Money, data)

def desc_load():
    load_table(Money, data)

gen_save()
desc_save()
gen_load()
desc_load()
print("save_generated", round(run(gen_save), 1))
print("save_descriptor", round(run(desc_save), 1))
print("load_generated", round(run(gen_load), 1))
print("load_descriptor", round(run(desc_load), 1))
print("OK")
`
	shapes := map[string]string{}
	for name, runner := range map[string]func(t testing.TB, dir, main string, inputs map[string][]byte) []byte{
		"python-3.15.2": func(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
			return pyrun.Run(t, dir, main, inputs)
		},
		"python-3.11.18": func(t testing.TB, dir, main string, inputs map[string][]byte) []byte {
			return pyrun.RunFloor(t, dir, main, inputs)
		},
	} {
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if !strings.Contains(out, "OK\n") {
			t.Fatalf("%s did not finish:\n%s", name, out)
		}
		shapes[name] = out
		for _, key := range []string{"save_generated", "save_descriptor", "load_generated", "load_descriptor"} {
			value := timedValue(t, out, key)
			t.Logf("%s %s %.1f ns/op", name, key, value)
		}
	}
	for _, key := range []string{"save_generated", "save_descriptor", "load_generated", "load_descriptor"} {
		current := timedValue(t, shapes["python-3.15.2"], key)
		floor := timedValue(t, shapes["python-3.11.18"], key)
		if current <= 0 || floor <= 0 {
			t.Fatalf("%s did not report a positive time: current %.1f, floor %.1f", key, current, floor)
		}
	}
}

var timedLine = regexp.MustCompile(`(?m)^([a-z_]+) ([0-9]+(?:\.[0-9]+)?)$`)

func timedValue(t *testing.T, out, key string) float64 {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		m := timedLine.FindStringSubmatch(line)
		if len(m) == 3 && m[1] == key {
			v, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				t.Fatalf("the %s line is not a number: %q", key, line)
			}
			return v
		}
	}
	t.Fatalf("the probe printed no %s line:\n%s", key, out)
	return 0
}
