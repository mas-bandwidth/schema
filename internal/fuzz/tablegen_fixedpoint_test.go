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
	fractionSeen := map[string]bool{}
	floatSeen := map[string]bool{}
	for seed := range 100 {
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
			fmt.Fprintf(&generatedTest, "{ var value Root; var report TableReport; RootReset(&value); if !RootFromJSON(&value, []byte(%q), &report) || report != (TableReport{}) { t.Fatalf(\"seed %%d instance %%d: generated Go reader did not read its own JSON cleanly (refused or clamped/mismatched/skipped): %%q report=%%+v\", %d, %d, %q, report) } }\n", value, seed, instance, value)
		}
		for key := range model.fixedFraction {
			fractionSeen[key] = true
		}
		generatedTest.WriteString("}\n")
		output, err := runGeneratedTableUnit(t, unit, generatedTest.String(), map[string]string{"NOVA_TEST_NO_HOST": "1"}, "-timeout=600s")
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
		if pair[1] == 0 {
			continue
		}
		for _, kind := range []string{"fixed", "ufixed"} {
			key := fmt.Sprintf("%s(%d,%d)", kind, pair[0], pair[1])
			if !fractionSeen[key] {
				t.Errorf("%s never drew a value with a nonzero fraction", key)
			}
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

// TestRandomTableGenFixedPointPlantClamp is the regression guard for the
// clean-read assertion above. It plants an out-of-range whole-unit value into a
// fixed/ufixed root leaf (declared bounds are min=0,max=1) and asserts the
// generated reader surfaces it as a clamp or refusal — never a silent clean
// read with an empty TableReport. Without the empty-report assertion a silent
// clamp (or kind mismatch) would pass as clean; this plant goes red if one does.
func TestRandomTableGenFixedPointPlantClamp(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units)")
	for seed := range 400 {
		r := rand.New(rand.NewSource(int64(seed)))
		src, model := randomSchema(r)
		var fixedField *genField
		for _, f := range model.Root.Fields {
			if f.Kind == gFixed || f.Kind == gUFixed {
				fixedField = f
				break
			}
		}
		if fixedField == nil {
			continue
		}
		unit := tableUnit(t, src)
		base := randomJSON(r, model, 1)
		planted := withFieldReplaced(base, fixedField.Name, "2")
		var probe strings.Builder
		probe.WriteString("package probe\nimport \"testing\"\nfunc TestClampDetected(t *testing.T) {\n")
		fmt.Fprintf(&probe, "{ var value Root; var report TableReport; RootReset(&value); ok := RootFromJSON(&value, []byte(%q), &report); if !ok { return }; if report != (TableReport{}) { return }; t.Errorf(\"seed %%d: out-of-range %s value read clean with an empty report: %%q\", %d, %q) }\n", planted, fixedKey(fixedField), seed, planted)
		probe.WriteString("}\n")
		output, err := runGeneratedTableUnit(t, unit, probe.String(), map[string]string{"NOVA_TEST_NO_HOST": "1"}, "-timeout=600s")
		if err != nil {
			t.Fatalf("seed %d: clamp plant failed for %s=%q in\n%s\noutput:\n%s", seed, fixedField.Name, planted, src, output)
		}
		return
	}
	t.Skip("no fixed/ufixed root field over seeds 0..399")
}
