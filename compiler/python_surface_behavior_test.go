package compiler

import (
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// The public route always calls GenerateLineage, even without an on-disk lock.
// Exercise that route so selection cannot be silently lost at the adapter.
func TestPythonSurfaceBehavior(t *testing.T) {
	u := unitFromSource(t, "package surfaceprobe\nfixed table Root { value int32 }\ntable Variable { value int32 }\n")
	c := New()
	full, err := c.Generate(u, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := c.Generate(u, "python", Options{"surface": "wire,fixed,json,cook,block,view,message,retain"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, explicit) {
		t.Fatal("default differs from explicit full set")
	}
	for _, selection := range []string{"wire", "fixed", "wire,block", ""} {
		t.Run(selection, func(t *testing.T) {
			files, err := c.Generate(u, "python", Options{"surface": selection})
			if err != nil {
				t.Fatal(err)
			}
			fixed := selection == "fixed" || selection == ""
			if _, ok := files["surfaceprobe/_fixed.py"]; ok != fixed {
				t.Fatal("fixed metadata selection")
			}
			if _, ok := files["surfaceprobe/_schema/fixedform.py"]; ok != fixed {
				t.Fatal("fixed runtime selection")
			}
			probe := pythonSurfaceProbe(fixed)
			if selection == "wire" || selection == "fixed" {
				probe = "" + probe + "\nassert Root.block is None and not hasattr(Root, 'block_row'), 'block descriptor leaked'\n"
			}
			dir := t.TempDir()
			pythonWriterTree(t, dir, files)
			inputs := map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + probe)}
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
				if err != nil || string(out) != "surface-ok\n" {
					t.Fatalf("%s: %v\n%s\n%s", pin, err, out, stderr)
				}
			}
			if out, err := pyrun.Analyze(t, dir, "surfaceprobe"); err != nil {
				t.Fatalf("mypy: %v\n%s", err, out)
			}
			if fixed {
				return
			}
			// Leaking a batch API into wire-only output must be observable at runtime.
			mutated := maps.Clone(files)
			const path = "surfaceprobe/Core.py"
			mutated[path] = append(append([]byte{}, files[path]...), []byte("\nRoot.fixed_save = lambda rows: b''\n")...)
			bad := t.TempDir()
			pythonWriterTree(t, bad, mutated)
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				_, stderr, err := pyrun.Try(t, pin, bad, "main.py", inputs)
				if err == nil || !strings.Contains(stderr, "fixed batch surface leaked") {
					t.Fatalf("selection control: %v\n%s", err, stderr)
				}
			}
		})
	}
}

func pythonSurfaceProbe(fixed bool) string {
	const common = `import surfaceprobe as p
from surfaceprobe import Root, Variable, Refused, InvalidValue, LoadReport
v=Variable();v.value=11
wire=v.save()
assert wire[0]==1 and v.measure()==len(wire) and Variable.load(wire)==v
assert Root.descriptor.name=='Root' and Variable.descriptor.name=='Variable'
r=Root();r.value=7
`
	if fixed {
		return common + `assert hasattr(Root,'fixed_save') and hasattr(p,'FixedLoadReport')
wire=r.save()
assert wire[0]==3 and r.measure()==len(wire) and Root.load(wire)==r
assert Root.fixed_save([r])==wire and Root.fixed_measure(1)==len(wire)
print('surface-ok')
`
	}
	return common + `assert not hasattr(Root,'fixed_save') and not hasattr(Root,'fixed_load') and not hasattr(Root,'fixed_plan_max'), 'fixed batch surface leaked'
assert not hasattr(p,'FixedLoadReport')
for op in (r.save,r.measure):
    try: op()
    except InvalidValue as e: assert 'fixed_surface_not_selected' in str(e)
    else: raise AssertionError('fixed file method fell back to variable wire')
report=LoadReport();report.unknown=77
try: Root.load(wire,report)
except Refused as e: assert e.reason=='fixed_surface_not_selected' and report.unknown==77
else: raise AssertionError('fixed read fell back to variable wire')
print('surface-ok')
`
}
