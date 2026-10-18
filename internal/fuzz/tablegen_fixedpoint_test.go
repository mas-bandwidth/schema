package fuzz_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRandomTableGenFixedPoint pins the generator to valid schemas and values
// across deterministic seeds. Each sample is read by the generated Go table
// front end, following SPEC-TABLES §3 and §16.
func TestRandomTableGenFixedPoint(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units)")

	fixedSeen := map[[2]int]bool{}
	ufixedSeen := map[[2]int]bool{}
	floatSeen := map[string]bool{}
	for seed := 0; seed < 100; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		src, model := randomSchema(r)
		unit := tableUnit(t, src)

		var generatedTest strings.Builder
		generatedTest.WriteString("package probe\nimport \"testing\"\nfunc TestGeneratedTableJSON(t *testing.T) {\n")
		for _, table := range append([]*genTable{model.Root}, model.Inner...) {
			for _, f := range table.Fields {
				pair := [2]int{f.FixedI, f.FixedF}
				switch f.Kind {
				case gFixed:
					fixedSeen[pair] = true
				case gUFixed:
					ufixedSeen[pair] = true
				}
			}
		}
		for instance := 1; instance <= 4; instance++ {
			value := randomJSON(r, model, instance)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(value), &fields); err != nil {
				t.Fatalf("seed %d instance %d: generated invalid JSON %q: %v", seed, instance, value, err)
			}
			for _, f := range model.Root.Fields {
				if f.Kind != gFloat32 && f.Kind != gFloat64 {
					continue
				}
				raw := string(fields[f.Name])
				width := "float32"
				if f.Kind == gFloat64 {
					width = "float64"
				}
				if raw == "-0" {
					floatSeen[width+":-0"] = true
				}
				if f.Kind == gFloat32 && (raw == "1e-45" || raw == "-1e-45") || f.Kind == gFloat64 && (raw == "5e-324" || raw == "-5e-324") {
					floatSeen[width+":subnormal"] = true
				}
				if f.Kind == gFloat32 && (raw == "3.4028234663852886e38" || raw == "-3.4028234663852886e38") || f.Kind == gFloat64 && (raw == "1.7976931348623157e308" || raw == "-1.7976931348623157e308") {
					floatSeen[width+":largest-finite"] = true
				}
			}
			fmt.Fprintf(&generatedTest, "{ var value Root; var report TableReport; RootReset(&value); if !RootFromJson(&value, []byte(%q), &report) { t.Fatalf(\"seed %%d instance %%d: generated Go reader refused its own JSON %%q\", %d, %d, %q) } }\n", value, seed, instance, value)
		}
		generatedTest.WriteString("}\n")
		output, err := runGeneratedTableUnit(t, unit, generatedTest.String(), map[string]string{"NOVA_TEST_NO_HOST": "1"})
		if err != nil {
			t.Fatalf("seed %d: generated Go reader failed\n%s\n%s", seed, src, output)
		}
	}

	for _, pair := range fixedPairs {
		if !fixedSeen[pair] {
			t.Errorf("fixed pair %v was not sampled", pair)
		}
		if !ufixedSeen[pair] {
			t.Errorf("ufixed pair %v was not sampled", pair)
		}
	}
	for _, width := range []string{"float32", "float64"} {
		for _, value := range []string{"-0", "subnormal", "largest-finite"} {
			if !floatSeen[width+":"+value] {
				t.Errorf("%s %s was not sampled", width, value)
			}
		}
	}
}
