//go:build schema_leg_python

package compiler

// docs/SPEC-TABLES.md §2.3: the Python target carries `?T` and the by-value
// `?[N]T` over a variable closure (compiler/carries_python_optional.go), and
// the optional arrays `?[N]T` and `?[..N]T`. The bytes are held to the Go
// engine by internal/codegen/pythontable's TestPythonVtOptionalArrays; this
// holds the compiler gate: Compiler.Generate answers files for each shape.

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func init() {
	nonGoCarriers["python"] = append(nonGoCarriers["python"], carryOptionalVariableClosure)
}

func TestPythonCarriesOptionalVariableClosure(t *testing.T) {
	t.Parallel()
	if !carries("python", carryOptionalVariableClosure) {
		t.Error("python does not carry optional-variable-closure (compiler/carries_python_optional.go)")
	}
	if !slices.Contains(optionalArrayTargets, "python") {
		t.Fatal("python is not an optional-array carrier (compiler/carries_python_optional.go)")
	}
	g1, err := os.ReadFile("../test/tables/G1.schema")
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"G1.schema":   string(g1),
		"scalar":      "package probe\ntable Leaf\n{\n    next *Leaf\n}\ntable Root\n{\n    leaf ?Leaf\n}\n",
		"fixed-array": "package probe\ntable Leaf\n{\n    n int32\n}\ntable Inner\n{\n    child *Leaf\n}\ntable Outer\n{\n    pair ?[2]Inner\n}\n",
		"arrays":      "package probe\ntable Root\n{\n    whole ?[3]int16\n    counted ?[..4]bool\n}\n",
	} {
		files, err := New().Generate(unitFromSource(t, src), "python", Options{})
		if err != nil {
			t.Errorf("%s: python refused a construct the optional kind carries: %v", name, err)
			continue
		}
		if len(files) == 0 {
			t.Errorf("%s: python generated no files", name)
		}
	}
	files, err := New().Generate(unitFromSource(t, "package probe\ntable Root\n{\n    counted ?[..4]bool\n}\n"), "python", Options{})
	if err != nil {
		t.Fatal(err)
	}
	module := string(files["probe/Probe.py"])
	for _, want := range []string{`codec="optional",`, `inner="array",`, `array_kind=2,`} {
		if !strings.Contains(module, want) {
			t.Errorf("the generated optional array lacks %q:\n%s", want, module)
		}
	}
}
