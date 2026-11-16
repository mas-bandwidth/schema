package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func init() { nonGoCarriers["python"] = append(nonGoCarriers["python"], carryRequired) }

// Each case crosses the public driver, Go's writer/reader, both pinned Python
// runtimes and strict mypy. An old plain writer supplies a genuinely absent
// field; the new required writer supplies its declared default.
func TestPythonRequiredPublicShapes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and strict mypy")
	cases := []struct{ name, decl, shape string }{
		{"bool", "", "bool"}, {"signed", "", "int32"}, {"wide", "", "uint64"},
		{"bits", "", "bits(12)"}, {"fixedpoint", "", "fixed(16,16) | min = -100, max = 100"},
		{"ranged", "", "int32 | min = -3, max = 9"},
		{"compressed", "", "float32 | min = -1.0, max = 1.0, resolution = 0.25"},
		{"float32", "", "float32"}, {"float64", "", "float64"},
		{"text", "", "string(8)"}, {"bytes", "", "bytes(8)"},
		{"declared_int", "", "int32 = 4"}, {"declared_float", "", "float32 = 1.5"},
		{"declared_string", "", "string(8) = \"hi\""}, {"declared_bytes", "", "bytes(8) = \"ab\""},
		{"declared_enum", "enum E { A, B }", "E = B"},
		{"enum", "enum E { A, B }", "E"}, {"flags", "flags F { A, B }", "F"},
		{"fixed_int", "", "[2]int32"}, {"counted_int", "", "[..3]int32"},
		{"fixed_float", "", "[2]float32"}, {"counted_bool", "", "[..3]bool"},
		{"list_int", "", "[]int32"}, {"list_bool", "", "[]bool"},
		{"value", "fixed table Leaf { n int32 }", "Leaf"},
		{"value_list", "fixed table Leaf { n int32 }", "[]Leaf"},
		{"pointer", "fixed table Leaf { n int32 }", "*Leaf"},
		{"pointer_list", "fixed table Leaf { n int32 }", "[]*Leaf"},
		{"pointer_fixed", "fixed table Leaf { n int32 }", "[2]*Leaf"},
		{"pointer_counted", "fixed table Leaf { n int32 }", "[..3]*Leaf"},
		{"blob_string", "", "*string"}, {"blob_bytes", "", "*bytes"},
		{"union", "union Choice { n int32 }", "Choice"},
		{"keyed", "enum Slot { A, B }", "[Slot]int32"},
		{"map", "", "map[string(8)]int32"},
		{"fixed_float64", "", "[2]float64"}, {"counted_float", "", "[..2]float32"},
		{"fixed_enum", "enum E { A, B }", "[2]E"}, {"counted_enum", "enum E { A, B }", "[..2]E"},
		{"fixed_flags", "flags F { A, B }", "[2]F"}, {"counted_flags", "flags F { A, B }", "[..2]F"},
		{"fixed_value", "fixed table Leaf { n int32 }", "[2]Leaf"},
		{"counted_value", "fixed table Leaf { n int32 }", "[..2]Leaf"},
		{"fixed_union", "union Choice { n int32 }", "[2]Choice"},
		{"counted_union", "union Choice { n int32 }", "[..2]Choice"},
		{"keyed_bool", "enum Slot { A, B }", "[Slot]bool"},
		{"keyed_value", "enum Slot { A, B }\nfixed table Leaf { n int32 }", "[Slot]Leaf"},
		{"list_float32", "", "[]float32"}, {"list_float64", "", "[]float64"},
		{"list_enum", "enum E { A, B }", "[]E"}, {"list_flags", "flags F { A, B }", "[]F"},
		{"list_union", "union Choice { n int32 }", "[]Choice"},
		{"list_text", "", "[]string(8)"}, {"counted_text", "", "[..2]string(8)"},
		{"list_optional_text", "", "[]?string"},
		{"map_list_float", "", "map[uint8][]float32"},
		{"map_list_value", "table Item { n int32 }", "map[uint8][]Item"},
		{"map_list_pointer", "table Node { n int32 }", "map[uint8][]*Node"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := func(required bool) string {
				marker := ""
				if required {
					marker = " | required"
					if strings.Contains(c.shape, " | ") {
						marker = ", required"
					}
				}
				return fmt.Sprintf("package reqpublic\n%s\ntable Root { x %s%s }\n", c.decl, c.shape, marker)
			}
			unit := unitFromSource(t, src(true))
			old := unitFromSource(t, src(false))
			files, err := New().Generate(unit, "python", nil)
			if err != nil {
				t.Fatalf("public generation: %v", err)
			}
			dir := t.TempDir()
			for name, data := range files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := pyrun.Analyze(t, dir, "reqpublic"); err != nil {
				t.Fatalf("mypy: %v %s", err, out)
			}
			model := tabletext.NewModel(unit)
			wire, err := tablewire.EncodeVariable(model, model.New(model.Lookup("Root")))
			if err != nil {
				t.Fatalf("required writer: %v", err)
			}
			prior := tabletext.NewModel(old)
			beforeField := tabletext.NewModel(unitFromSource(t, "package reqpublic\ntable Root {}\n"))
			missing, err := tablewire.EncodeVariable(beforeField, beforeField.New(beforeField.Lookup("Root")))
			if err != nil {
				t.Fatal(err)
			}
			var report tabletext.Report
			if ok, err := tablewire.DecodeVariable(model, model.New(model.Lookup("Root")), missing, &report); ok || !report.Refused || !strings.Contains(report.Reason, "missing_required") {
				t.Fatalf("Go missing: %v %+v", err, report)
			}
			report = tabletext.Report{}
			if ok, err := tablewire.DecodeVariable(prior, prior.New(prior.Lookup("Root")), wire, &report); !ok {
				t.Fatalf("Go required removal: %v %+v", err, report)
			}
			script := pyrun.ImportPrelude + `from reqpublic import Root, Refused, LoadReport
with open("wire", "rb") as f: wire=f.read()
with open("missing", "rb") as f: missing=f.read()
value=Root()
assert value.save()==wire, "required default bytes"
assert Root.load(wire).save()==wire, "required roundtrip"
report=LoadReport();report.unknown=73
try: Root.load(missing, report=report)
except Refused as e: assert e.reason=="missing_required" and "x" in str(e), e
else: raise AssertionError("missing accepted")
assert report.unknown==73, "report changed"
print("required-public")
`
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire, "missing": missing})
				if err != nil || strings.TrimSpace(out) != "required-public" {
					t.Errorf("%s: %v %s %s", pin, err, out, stderr)
				}
			}
		})
	}
}

// Nondefault old-writer payloads cross the public reader and re-save, with
// independent decoded-value assertions for each carried family.
func TestPythonRequiredPublicNondefault(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	cases := []struct{ name, decl, shape, input, check string }{
		{"bool", "", "bool", `true`, `v.x is True`},
		{"int", "", "int32", `-7`, `v.x == -7`},
		{"wide", "", "uint64", `"9007199254740993"`, `v.x == 9007199254740993`},
		{"float32", "", "float32", `1.5`, `v.x == 1.5`},
		{"float64", "", "float64", `-2.25`, `v.x == -2.25`},
		{"string", "", "string(8)", `"hi"`, `v.x == "hi"`},
		{"bytes", "", "bytes(8)", `"aGk="`, `v.x == b"hi"`},
		{"enum", "enum E { A, B }", "E", `"B"`, `v.x == 2`},
		{"fixed", "", "[2]int32", `[1,-2]`, `v.x == [1,-2]`},
		{"counted", "", "[..3]int32", `[3,4]`, `v.x == [3,4]`},
		{"list", "", "[]bool", `[true,false]`, `v.x == [True,False]`},
		{"value", "fixed table Leaf { n int32 }", "Leaf", `{"n":7}`, `v.x.n == 7`},
		{"value_list", "fixed table Leaf { n int32 }", "[]Leaf", `[{"n":7},{"n":8}]`, `[e.n for e in v.x] == [7,8]`},
		{"pointer", "fixed table Leaf { n int32 }", "*Leaf", `{"n":7}`, `v.x.n == 7`},
		{"pointer_list", "fixed table Leaf { n int32 }", "[]*Leaf", `[{"n":7},null]`, `v.x[0].n == 7 and v.x[1] is None`},
		{"pointer_fixed", "fixed table Leaf { n int32 }", "[2]*Leaf", `[null,{"n":7}]`, `v.x[0] is None and v.x[1].n == 7`},
		{"blob", "", "*string", `"hi"`, `v.x == "hi"`},
		{"union", "union Choice { n int32 }", "Choice", `{"n":7}`, `v.x.n == 7`},
		{"keyed", "enum Slot { A, B }", "[Slot]int32", `{"A":7}`, `v.x[1] == 7`},
		{"map", "", "map[string(8)]int32", `{"k":7}`, `v.x == {"k":7}`},
		{"list_float", "", "[]float32", `[1.5,-2.0]`, `v.x == [1.5,-2.0]`},
		{"list_text", "", "[]string(8)", `["hi","ok"]`, `v.x == ["hi","ok"]`},
		{"map_list_float", "", "map[uint8][]float32", `{"4":[1.5,-2.0]}`, `v.x[4] == [1.5,-2.0]`},
		{"map_list_value", "table Item { n int32 }", "map[uint8][]Item", `{"4":[{"n":7},{"n":8}]}`, `[e.n for e in v.x[4]] == [7,8]`},
		{"map_list_pointer", "table Node { n int32 }", "map[uint8][]*Node", `{"4":[{"&node":1,"n":7},null,{"&node":1}]}`, `v.x[4][0].n == 7 and v.x[4][1] is None and v.x[4][0] is v.x[4][2]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := fmt.Sprintf("package reqvalues\n%s\ntable Root { x %s | required }\n", c.decl, c.shape)
			unit := unitFromSource(t, src)
			files, err := New().Generate(unit, "python", nil)
			if err != nil {
				t.Fatal(err)
			}
			model := tabletext.NewModel(unit)
			value := model.New(model.Lookup("Root"))
			var report tabletext.Report
			if !model.Read(value, []byte(`{"x":`+c.input+`}`), &report) || !report.Silent() {
				t.Fatalf("Go text: %+v", report)
			}
			wire, err := tablewire.EncodeVariable(model, value)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for name, data := range files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			script := pyrun.ImportPrelude + fmt.Sprintf(`from reqvalues import Root
with open("wire","rb") as f: wire=f.read()
v=Root.load(wire)
assert %s, "decoded value"
assert v.save()==wire, "nondefault bytes"
print("required-value")
`, c.check)
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
				if err != nil || strings.TrimSpace(out) != "required-value" {
					t.Errorf("%s: %v %s %s", pin, err, out, stderr)
				}
			}
		})
	}
}

func TestPythonRequiredPublicGuards(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	schema := `package reqguards
table Root { active bool
if active { amount int32 | required } else { fallback int32 | required } }
`
	unit := unitFromSource(t, schema)
	old := unitFromSource(t, strings.ReplaceAll(schema, " | required", ""))
	wire := func(uSrc string, text string) []byte {
		t.Helper()
		u := unit
		if uSrc == "old" {
			u = old
		}
		m := tabletext.NewModel(u)
		v := m.New(m.Lookup("Root"))
		var rep tabletext.Report
		if !m.Read(v, []byte(text), &rep) || !rep.Silent() {
			t.Fatalf("%s input: %+v", uSrc, rep)
		}
		b, err := tablewire.EncodeVariable(m, v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	inputs := map[string][]byte{"on": wire("new", `{"active":true,"amount":0}`), "off": wire("new", `{"fallback":0}`), "missing_on": wire("old", `{"active":true}`), "missing_off": wire("old", `{}`)}
	files, err := New().Generate(unit, "python", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	script := pyrun.ImportPrelude + `from reqguards import Root, Refused
def read(name):
    with open(name,"rb") as f: return f.read()
v=Root(); assert v.save()==read("off") and Root.load(read("off")).active is False
v.active=True; assert v.save()==read("on") and Root.load(read("on")).active is True
for name, field in (("missing_on","amount"),("missing_off","fallback")):
    try: Root.load(read(name))
    except Refused as e: assert e.reason=="missing_required" and field in str(e)
    else: raise AssertionError(name+" accepted")
print("required-guards")
`
	inputs["main.py"] = []byte(script)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if err != nil || strings.TrimSpace(out) != "required-guards" {
			t.Errorf("%s: %v %s %s", pin, err, out, stderr)
		}
	}
}
