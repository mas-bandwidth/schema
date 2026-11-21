package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
)

func TestPythonCookForms(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnit(t, `package cookforms
 table Inner | forms = "wire" { value int32 }
 table Root | forms = "wire,cook" { nested Inner }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from cookforms import Inner, InvalidValue, Refused, Root
from cookforms._schema import cook
r = Root(); r.nested.value = 3
raw = cook.cook(r); assert cook.cook_measure(r) == len(raw)
cook.open_cook(Root, raw); cook.check_cook(Root, raw)
for operation in (lambda: cook.cook(Inner()), lambda: cook.cook_measure(Inner())):
    try: operation()
    except InvalidValue as e: assert "cook" in e.rule
    else: raise AssertionError("excluded cook writer was accepted")
for operation in (lambda: cook.open_cook(Inner, b""), lambda: cook.check_cook(Inner, b"")):
    try: operation()
    except Refused as e: assert e.reason == "cook_form_not_carried", "excluded cook reader wrong refusal"
    else: raise AssertionError("excluded cook reader was accepted")
print("ok")
`
	for name, runner := range cookPins() {
		got := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if strings.TrimSpace(got) != "ok" {
			t.Errorf("%s: %s", name, got)
		}
	}
	for _, control := range []struct{ name, path, anchor, failure string }{
		{"writer", "cookforms/_schema/cookwrite.py", `if "cook" not in value.descriptor.forms:`, "excluded cook writer was accepted"},
		{"reader", "cookforms/_schema/cook.py", `if "cook" not in cls.descriptor.forms:`, "excluded cook reader wrong refusal"},
	} {
		t.Run(control.name+"-negative-control", func(t *testing.T) {
			t.Parallel()
			planted := cloneFiles(files)
			body := string(planted[control.path])
			if !strings.Contains(body, control.anchor) {
				t.Fatal("forms control anchor absent")
			}
			planted[control.path] = []byte(strings.Replace(body, control.anchor, "if False:", 1))
			dir := t.TempDir()
			writeTree(t, dir, planted)
			for _, pin := range []struct {
				name string
				path func(testing.TB) string
			}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
				_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
				if err == nil || !strings.Contains(stderr, control.failure) {
					t.Errorf("%s: forms %s control missed fault: %v %s", pin.name, control.name, err, stderr)
				}
			}
		})
	}

}

func TestPythonCookSurfaceSelection(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned interpreters and surface selection")
	u := loadUnit(t, `package cookselect
 table Root { value int32 }
`)
	wire, err := ParseSurfaces("wire")
	if err != nil {
		t.Fatal(err)
	}
	only, err := GenerateSurface(u, wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := only["cookselect/_schema/cook.py"]; exists {
		t.Fatal("wire-only generated the cook runtime")
	}
	if strings.Contains(string(only["cookselect/Core.py"]), "_s_cook_write") {
		t.Fatal("wire-only generated a cook writer")
	}
	selected, err := ParseSurfaces("wire,cook")
	if err != nil {
		t.Fatal(err)
	}
	files, err := GenerateSurface(u, selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := files["cookselect/_schema/cook.py"]; !exists {
		t.Fatal("selected cook runtime missing")
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, module := range []string{"cookselect", "cookselect/_schema"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("mypy %s: %v\n%s", module, err, out)
		}
	}
	probe := pyrun.ImportPrelude + `from cookselect import Root
from cookselect._schema import cook
r = Root(); r.value = 7
assert "cook" in r.descriptor.forms, "selected cook is missing"
data = cook.cook(r); assert cook.cook_measure(r) == len(data)
cook.check_cook(Root, data)
print("ok")
`
	for name, runner := range cookPins() {
		got := string(runner(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))
		if strings.TrimSpace(got) != "ok" {
			t.Errorf("%s: %s", name, got)
		}
	}
	t.Run("selection-negative-control", func(t *testing.T) {
		t.Parallel()
		planted := cloneFiles(files)
		const path = "cookselect/Core.py"
		const anchor = `forms=("wire", "cook"),`
		body := string(planted[path])
		if !strings.Contains(body, anchor) {
			t.Fatal("selection control anchor absent")
		}
		planted[path] = []byte(strings.Replace(body, anchor, `forms=("wire",),`, 1))
		dir := t.TempDir()
		writeTree(t, dir, planted)
		for _, pin := range []struct {
			name string
			path func(testing.TB) string
		}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
			_, stderr, err := pyrun.Try(t, pin.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err == nil || !strings.Contains(stderr, "selected cook is missing") {
				t.Errorf("%s: selection control missed fault: %v %s", pin.name, err, stderr)
			}
		}
	})
}
