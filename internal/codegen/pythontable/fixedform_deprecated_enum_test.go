package pythontable_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

func TestPythonFixedFormDeprecatedEnum(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins and the live C++ fixed-form oracle")
	schema := `package probe
enum Mode {
 Old
 Live
}
union Choice { value Mode }
fixed table T {
 mode Mode = Live
 fixed_values [2]Mode
 counted_values [..2]Mode
 choice Choice
}
`
	old := fixedproof.ParseCheckUnit(t, schema)
	oldCase := fixedproof.RowCase{ID: "python-fixed-enum-old", Schema: schema, Root: "T", Count: 1, Body: ir.TableFixedTypeBytes(old.Tables["T"]), CppSetup: `
value[0].mode=Mode::Old;
value[0].fixed_values[0]=Mode::Old;
value[0].fixed_values[1]=Mode::Live;
value[0].counted_values_count=2;
value[0].counted_values[0]=Mode::Old;
value[0].counted_values[1]=Mode::Live;
value[0].choice.type=ChoiceType::Value;
value[0].choice.value=Mode::Old;
`}
	wire, _ := fixedproof.CppOracle(t, oldCase, old)
	currentSchema := strings.Replace(schema, " Old\n", " Old | deprecated\n", 1)
	current := fixedproof.ParseCheckUnit(t, currentSchema)
	currentCase := oldCase
	currentCase.ID = "python-fixed-enum-current"
	currentCase.Schema = currentSchema
	currentCase.CppSetup = `
value[0].mode=Mode::Live;
value[0].fixed_values[1]=Mode::Live;
value[0].counted_values_count=2;
value[0].counted_values[1]=Mode::Live;
value[0].choice.type=ChoiceType::Value;
value[0].choice.value=(Mode)0;
`
	expected, verify := fixedproof.CppOracle(t, currentCase, current)
	if !bytes.Equal(wire[8:16], expected[8:16]) {
		t.Fatal("enum deprecation unexpectedly changed wire identity")
	}
	files, err := pythontable.Generate(current)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `from probe import T, FixedLoadReport
with open('wire','rb') as f:wire=f.read()
report=FixedLoadReport()
v=T.load(wire,report)
assert v.mode==2 and v.fixed_values==[0,2] and v.counted_values==[0,2] and v.choice.type==1 and v.choice.value==0,'fixed_deprecated_enum_control'
assert (report.unknown,report.widened,report.retained,report.retain_lost)==(0,0,0,0)
with open('saved','wb') as f:f.write(v.save())
`
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, files)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err != nil {
			t.Fatalf("deprecated enum read: %v\n%s", err, stderr)
		}
		saved, err := os.ReadFile(filepath.Join(dir, "saved"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, expected) {
			t.Fatalf("normalized enum bytes differ:\n got %x\nwant %x", saved, expected)
		}
		if err := verify(saved); err != nil {
			t.Fatal(err)
		}
		if output, err := pyrun.Analyze(t, dir, "probe"); err != nil {
			t.Fatalf("enum strict mypy: %v\n%s", err, output)
		}
	}
	// Reinstating the old raw-ordinal decode must turn the same value assertion red.
	broken := maps.Clone(files)
	name := "probe/_schema/fixedform.py"
	const anchor = `return spec.get("default", 0) if shape == "enum" and value in spec.get("deprecated", ()) else value`
	if strings.Count(string(broken[name]), anchor) != 1 {
		t.Fatal("deprecated enum control lost its decode anchor")
	}
	broken[name] = []byte(strings.Replace(string(broken[name]), anchor, "return value", 1))
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := pythonFixedTree(t, broken)
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe), "wire": wire})
		if err == nil || !strings.Contains(stderr, "fixed_deprecated_enum_control") {
			t.Fatalf("deprecated enum sabotage did not fail by name: %v\n%s", err, stderr)
		}
	}
}
