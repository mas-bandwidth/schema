package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonCookShapeFunctional(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters and a timed shape comparison")
	u := loadUnit(t, fixedUnionSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, module := range []string{"fixedunion", "fixedunion/_schema"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("mypy %s: %v\n%s", module, err, out)
		}
	}
	probe := pyrun.ImportPrelude + `import time
from fixedunion import Money
from fixedunion._schema import cook
m = Money(); m.f8 = -7; m.f16 = 1234; m.f32 = -123456; m.f64 = (1 << 40) + 3
m.f128 = -(1000000 << 16) + 9; m.u8 = 250; m.u16 = 65000; m.u32 = 4000000000
m.u64 = (1 << 62) + 7; m.u128 = (1999999 << 16) + 11
fast = Money._s_cook_write
expected = cook.cook(m); Money._s_cook_write = None
assert cook.cook(m) == expected, "cook shape bytes differ"
def run(fn):
    best = float("inf")
    for _ in range(3):
        start = time.perf_counter_ns()
        for _ in range(2000): fn()
        best = min(best, (time.perf_counter_ns() - start) / 2000)
    return round(best, 1)
slow = run(lambda: cook.cook(m))
Money._s_cook_write = fast
quick = run(lambda: cook.cook(m))
print("generated", quick, "descriptor", slow)
print("ok")
`
	allCookPins := cookPins()
	if len(allCookPins) == 0 {
		t.Fatal("cookPins() is empty: the loop below would check nothing")
	}
	for name, runner := range allCookPins {
		out := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if !strings.Contains(out, "ok\n") {
			t.Errorf("%s: %s", name, out)
		}
		t.Logf("%s %s", name, strings.TrimSpace(out))
	}
	t.Run("generated-store-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "fixedunion/Core.py"
		const anchor = "int(value.f8).to_bytes(1, order, signed=True)"
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("generated store control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, "int(0).to_bytes(1, order, signed=True)", 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "cook shape bytes differ") {
				t.Errorf("%s: store control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})

}
