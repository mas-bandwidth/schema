package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A counted [A..B] holds A inside a union arm (ruling R10; SPEC.md §5.1 row
// 3): the writers (the variable save, the fixed save and the text writer)
// refuse an arm holding fewer than A elements, and write at A and at B. The
// reads are the conformance rows arm-min-fix-* and armmin_fix_count_*.
func TestPythonUnionArmCountedMinimumWrite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, "package armmin\nunion Pick\n{\n    few [2..4]uint32\n    other int32\n}\nfixed table Root { pick Pick }\ntable Var { pick Pick }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from armmin import Root, Var, InvalidValue
out = []
for few in ([7], [], [7, 9], [1, 2, 3, 4]):
    for name, cls, fn in (("fixed", Root, lambda v: Root.fixed_save([v])), ("var", Var, lambda v: v.save()), ("json", Root, lambda v: v.to_json())):
        v = cls(); v.pick.type = 1; v.pick.few = few
        try: fn(v); out.append("%s %d wrote" % (name, len(few)))
        except InvalidValue: out.append("%s %d refused" % (name, len(few)))
print(",".join(out))
`
	const want = "fixed 1 refused,var 1 refused,json 1 refused,fixed 0 refused,var 0 refused,json 0 refused," +
		"fixed 2 wrote,var 2 wrote,json 2 wrote,fixed 4 wrote,var 4 wrote,json 4 wrote"
	run := func(label string) string {
		out := ""
		for _, p := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			o, stderr, err := pyrun.Try(t, p.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err != nil {
				t.Fatalf("%s %s: %v %s", label, p.name, err, stderr)
			}
			out = strings.TrimSpace(o)
			if label == "writers" && out != want {
				t.Errorf("%s: %q, want %q", p.name, out, want)
			}
		}
		return out
	}
	run("writers")
	// the negative control: without the minimum checks the writers accept a
	// short arm
	for _, plant := range []struct{ file, old, with string }{
		{"kind_union.py", "if count < f.array_min or count > f.bound:", "if count > f.bound:"},
		{"fixedform.py", `if count < spec.get("minimum", 0) or count > spec["bound"]:`, `if count > spec["bound"]:`},
		{"json_table.py", "(field.array_kind == 2 and not field.array_min <= len(item) <= field.bound)", "(field.array_kind == 2 and not len(item) <= field.bound)"},
	} {
		path := filepath.Join(dir, "armmin", "_schema", plant.file)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), plant.old) {
			t.Fatalf("negative control anchor %q is not in %s", plant.old, plant.file)
		}
		if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(src), plant.old, plant.with)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := run("control"); got == want {
		t.Errorf("negative control: without the arm minimum the writers still refuse: %q", got)
	}
}
