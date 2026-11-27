package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/ir"
)

// TestCfDecodeDeclarationAgrees holds cfdecode.go's constants to the CfDecode
// declaration in BitPhase.schema and to the step count the compiler derives.
func TestCfDecodeDeclarationAgrees(t *testing.T) {
	steps, bits := ir.CompressedFloatParams(cfDecodeMin, cfDecodeMax, cfDecodeRes)
	if steps != cfDecodeSteps || bits != 14 {
		t.Fatalf("the compiler derives %d steps in %d bits from [%v, %v] @ %v; cfDecodeSteps is %d", steps, bits, cfDecodeMin, cfDecodeMax, cfDecodeRes, cfDecodeSteps)
	}
	src, err := os.ReadFile("BitPhase.schema")
	if err != nil {
		t.Fatal(err)
	}
	decl := fmt.Sprintf("type CfDecode\n{\n    c float32 | min = %v, max = %v, resolution = %v\n}", cfDecodeMin, cfDecodeMax, cfDecodeRes)
	if !strings.Contains(string(src), decl) {
		t.Fatalf("BitPhase.schema does not declare\n%s", decl)
	}
}

// TestCfDecodeSpecIsTheFloat32Decode pins the harness's decode at the index
// the audit reported: 0xc053126e, where adding the double min gives 0xc053126f.
func TestCfDecodeSpecIsTheFloat32Decode(t *testing.T) {
	if got := math.Float32bits(cfDecodeSpec(2)); got != 0xc053126e {
		t.Fatalf("index 2 decodes to %08x, want c053126e", got)
	}
	if got := math.Float32bits(cfDecodeDoubleMin(2)); got != 0xc053126f {
		t.Fatalf("the double-min decode of index 2 is %08x, want c053126f", got)
	}
}

// cfDecodeDoubleMin is the defect: every step float32 but the last add, which
// adds the double -3.3 (the schema literal) and rounds the sum.
func cfDecodeDoubleMin(i int) float32 {
	minF := float32(cfDecodeMin)
	delta := float32(float32(cfDecodeMax) - minF)
	q := float32(float32(i) / float32(cfDecodeSteps))
	p := float32(q * delta)
	return float32(float64(p) + cfDecodeMin)
}

// cfAnswer prints a tier's answers the way a driver does, from decode, for
// every index but those in skip.
func cfAnswer(tier string, decode func(int) float32, skip map[int]bool) string {
	var b strings.Builder
	for i := 0; i <= cfDecodeSteps; i++ {
		if !skip[i] {
			fmt.Fprintf(&b, "CFDECODE %s %d %016x\n", tier, i, math.Float64bits(float64(decode(i))))
		}
	}
	return b.String()
}

// cfPatterns prints a tier's answers as float32 patterns, 8 hex digits, the
// way the moo driver does.
func cfPatterns(tier string, decode func(int) float32) string {
	var b strings.Builder
	for i := 0; i <= cfDecodeSteps; i++ {
		fmt.Fprintf(&b, "CFDECODE %s %d %08x\n", tier, i, math.Float32bits(decode(i)))
	}
	return b.String()
}

// TestCfDecodeCheckRefusesEveryShortfall: the whole answer passes, and the
// defect, a missing index, a repeated one, a tier the leg does not carry, a
// tier left out, a value that is no float32 and a malformed line each fail by
// name. The defect fails at exactly the 2,535 indices the audit counted.
func TestCfDecodeCheckRefusesEveryShortfall(t *testing.T) {
	whole := cfAnswer("go", cfDecodeSpec, nil)
	if err := checkCfDecode("go", "BITPHASE_OK go 66\n"+whole); err != nil {
		t.Fatalf("CONTROL FAILED: the whole answer: %v", err)
	}
	if err := checkCfDecode("js", cfAnswer("stream", cfDecodeSpec, nil)+cfAnswer("flat", cfDecodeSpec, nil)); err != nil {
		t.Fatalf("CONTROL FAILED: both JavaScript tiers: %v", err)
	}
	if err := checkCfDecode("moo", cfPatterns("moo", cfDecodeSpec)); err != nil {
		t.Fatalf("CONTROL FAILED: the answer as float32 patterns (moo): %v", err)
	}
	for _, tc := range []struct {
		name, lang, out, says string
	}{
		{"double min", "dart", cfAnswer("dart", cfDecodeDoubleMin, nil),
			"tier dart decodes 2535 of 11001 indices off SPEC §4.6's float32 decode: index 2 read float32 c053126f, the decode gives c053126e"},
		{"js flat tier on the double min", "js", cfAnswer("stream", cfDecodeSpec, nil) + cfAnswer("flat", cfDecodeDoubleMin, nil),
			"tier flat decodes 2535 of 11001"},
		{"no answer", "c", "BITPHASE_OK c 66\n", "tier c leaves 11001 of 11001 indices unanswered"},
		{"one index left out", "go", cfAnswer("go", cfDecodeSpec, map[int]bool{7: true}), "tier go leaves 1 of 11001 indices unanswered (first [7])"},
		{"one index twice", "go", whole + "CFDECODE go 3 " + fmt.Sprintf("%016x", math.Float64bits(float64(cfDecodeSpec(3)))) + "\n", "answers go index 3 a second time"},
		{"index past the range", "go", whole + "CFDECODE go 11001 0000000000000000\n", `answers the index "11001", outside 0..11000`},
		{"a tier the leg lacks", "go", whole + "CFDECODE flat 0 0000000000000000\n", `answers the tier "flat", and go carries go`},
		{"js answers one tier", "js", cfAnswer("stream", cfDecodeSpec, nil), "tier flat leaves 11001 of 11001 indices unanswered"},
		{"not a float32", "go", strings.Replace(whole, "CFDECODE go 0 c00a666660000000", "CFDECODE go 0 c00a666660000001", 1), "index 0 read c00a666660000001, which is not a float32"},
		{"malformed line", "go", whole + "CFDECODE go 5\n", "is not CFDECODE <tier> <index> <16 or 8 hex digits>"},
		{"double min as float32 patterns", "moo", cfPatterns("moo", cfDecodeDoubleMin), "tier moo decodes 2535 of 11001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkCfDecode(tc.lang, tc.out)
			if err == nil {
				t.Fatalf("passed; want a failure saying %q", tc.says)
			}
			if !strings.Contains(err.Error(), tc.says) || !strings.HasPrefix(err.Error(), "FAILED: cfdecode ") {
				t.Fatalf("failed with\n%v\nwant it to say %q", err, tc.says)
			}
		})
	}
}

// TestEveryDriverAnswersTheDecodeCase: a driver source that never fills in
// the step count is one that does not run the case, so each carries the token.
func TestEveryDriverAnswersTheDecodeCase(t *testing.T) {
	for _, file := range []string{"drivers_compiled.go", "drivers_vm.go", "cpp/oracle.cpp"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		want := cfDecodeStepsToken
		if file == "cpp/oracle.cpp" {
			want = "CFDECODE_STEPS"
		}
		n := strings.Count(string(src), want)
		if n == 0 {
			t.Fatalf("%s never names %s", file, want)
		}
	}
	// every leg but cpp (the oracle) builds its source in the two driver files
	compiled, _ := os.ReadFile("drivers_compiled.go")
	vm, _ := os.ReadFile("drivers_vm.go")
	all := string(compiled) + string(vm)
	for _, lang := range allLangs {
		if lang == "cpp" {
			continue
		}
		tier := lang
		if lang == "cs" || lang == "unity" {
			tier = "%[1]s" // the one C# program, tagged per leg
		}
		if lang == "js" {
			tier = "stream"
		}
		if !strings.Contains(all, "CFDECODE "+tier+" ") {
			t.Errorf("no driver prints CFDECODE %s", tier)
		}
	}
}
