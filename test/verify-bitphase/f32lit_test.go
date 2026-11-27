package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestF32LitDeclarationAgrees holds f32lit.go's constants to BitPhase.schema.
func TestF32LitDeclarationAgrees(t *testing.T) {
	src, err := os.ReadFile("BitPhase.schema")
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range []string{
		fmt.Sprintf("const F32Const float32 = %v\n", f32LitConst),
		fmt.Sprintf("type F32Lit\n{\n    d float32 = %v\n    e float32 = %v | min = %v, max = %v, resolution = %v\n}", f32LitDefD, f32LitDefE, f32LitMin, f32LitMax, f32LitRes),
	} {
		if !strings.Contains(string(src), decl) {
			t.Fatalf("BitPhase.schema does not declare\n%s", decl)
		}
	}
	if steps, _ := ir.CompressedFloatParams(f32LitMin, f32LitMax, f32LitRes); steps != 600 {
		t.Fatalf("the compiler derives %d steps from [%v, %v] @ %v, want 600", steps, f32LitMin, f32LitMax, f32LitRes)
	}
}

// TestF32LitTheEndsLieOutsideTheirDecimals: the case asks something only
// because each value differs from its decimal read as a double, and each end
// lies outside its decimal, and each end writes and reads back as itself.
func TestF32LitTheEndsLieOutsideTheirDecimals(t *testing.T) {
	for _, v := range []float64{f32LitConst, f32LitDefD, f32LitDefE} {
		if float64(float32(v)) == v {
			t.Fatalf("%v is a float32: the probe asks nothing", v)
		}
	}
	if !(float64(float32(f32LitMin)) < f32LitMin) || !(float64(float32(f32LitMax)) > f32LitMax) {
		t.Fatalf("float32(%v) and float32(%v) must lie outside their decimals", f32LitMin, f32LitMax)
	}
	want := f32LitWant()
	if got := math.Float32bits(want["write-min"]); got != 0xbe99999a {
		t.Fatalf("float32(-0.3) round-trips to %08x, want be99999a", got)
	}
	if got := math.Float32bits(want["write-max"]); got != 0x3e99999a {
		t.Fatalf("float32(0.3) round-trips to %08x, want 3e99999a", got)
	}
	if f32LitDecimal(f32LitMin) != "-0.30000001192092896" || f32LitDecimal(f32LitMax) != "0.30000001192092896" {
		t.Fatalf("the end tokens spell %s and %s", f32LitDecimal(f32LitMin), f32LitDecimal(f32LitMax))
	}
}

// f32LitAnswer prints a tier's answers the way a driver does.
func f32LitAnswer(tier string, vals map[string]float64) string {
	var b strings.Builder
	for _, p := range f32LitProbes {
		v, ok := vals[p]
		if !ok {
			continue
		}
		if math.IsNaN(v) {
			fmt.Fprintf(&b, "F32LIT %s %s refused\n", tier, p)
			continue
		}
		fmt.Fprintf(&b, "F32LIT %s %s %016x\n", tier, p, math.Float64bits(v))
	}
	return b.String()
}

func f32LitRight() map[string]float64 {
	m := map[string]float64{}
	for p, v := range f32LitWant() {
		m[p] = float64(v)
	}
	return m
}

// f32LitDecimalDefect is the defect: every constant and default the double of
// its decimal, and a write guard held at the decimal that refuses both ends.
func f32LitDecimalDefect() map[string]float64 {
	return map[string]float64{
		"const": f32LitConst, "default-d": f32LitDefD, "default-e": f32LitDefE,
		"write-min": math.NaN(), "write-max": math.NaN(),
	}
}

// TestF32LitCheckRefusesEveryShortfall: the whole answer passes, on both JS
// tiers and as float32 patterns, and the defect, a probe left out, a probe
// answered twice, a tier the leg lacks and a malformed line each fail by name.
func TestF32LitCheckRefusesEveryShortfall(t *testing.T) {
	whole := f32LitAnswer("go", f32LitRight())
	if err := checkF32Lit("go", "BITPHASE_OK go 66\n"+whole); err != nil {
		t.Fatalf("CONTROL FAILED: the whole answer: %v", err)
	}
	if err := checkF32Lit("js", f32LitAnswer("stream", f32LitRight())+f32LitAnswer("flat", f32LitRight())); err != nil {
		t.Fatalf("CONTROL FAILED: both JavaScript tiers: %v", err)
	}
	var moo strings.Builder
	for _, p := range f32LitProbes {
		fmt.Fprintf(&moo, "F32LIT moo %s %08x\n", p, math.Float32bits(f32LitWant()[p]))
	}
	if err := checkF32Lit("moo", moo.String()); err != nil {
		t.Fatalf("CONTROL FAILED: the answer as float32 patterns (moo): %v", err)
	}
	noConst := f32LitRight()
	delete(noConst, "const")
	for _, tc := range []struct {
		name, lang, out string
		says            []string
	}{
		{"the decimal read as a double", "dart", f32LitAnswer("dart", f32LitDecimalDefect()), []string{
			"tier dart const reads 3fb999999999999a (0.1), which is no float32; the declaration names float32 3dcccccd",
			"tier dart default-e reads bfd3333333333333 (-0.3), which is no float32",
			"tier dart write-min: the write refused float32 be99999a",
			"tier dart write-max: the write refused float32 3e99999a",
		}},
		{"js stream tier only", "js", f32LitAnswer("stream", f32LitDecimalDefect()) + f32LitAnswer("flat", f32LitRight()), []string{"tier stream const reads"}},
		{"no answer", "c", "BITPHASE_OK c 66\n", []string{"tier c leaves const, default-d, default-e, write-max, write-min unanswered"}},
		{"a probe left out", "go", f32LitAnswer("go", noConst), []string{"tier go leaves const unanswered"}},
		{"a probe twice", "go", whole + "F32LIT go const 3fb99999a0000000\n", []string{"answers go const a second time"}},
		{"a tier the leg lacks", "go", whole + "F32LIT flat const 3fb99999a0000000\n", []string{`answers the tier "flat", and go carries go`}},
		{"a malformed line", "go", whole + "F32LIT go default-z 3fb99999a0000000\n", []string{"is not F32LIT <tier>"}},
		{"refused is no constant", "go", strings.Replace(whole, "F32LIT go const 3fb99999a0000000", "F32LIT go const refused", 1), []string{`tier go const answers "refused"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkF32Lit(tc.lang, tc.out)
			if err == nil {
				t.Fatalf("passed; want a failure saying %q", tc.says)
			}
			for _, s := range tc.says {
				if !strings.Contains(err.Error(), s) || !strings.HasPrefix(err.Error(), "FAILED: f32lit ") {
					t.Fatalf("failed with\n%v\nwant it to say %q", err, s)
				}
			}
		})
	}
}

// TestEveryDriverAnswersTheF32LitCase: every leg's driver prints the probes.
func TestEveryDriverAnswersTheF32LitCase(t *testing.T) {
	compiled, _ := os.ReadFile("drivers_compiled.go")
	vm, _ := os.ReadFile("drivers_vm.go")
	oracle, _ := os.ReadFile("cpp/oracle.cpp")
	all := string(compiled) + string(vm) + string(oracle)
	for _, lang := range allLangs {
		tier := lang
		if lang == "cs" || lang == "unity" {
			tier = "%[1]s" // the one C# program, tagged per leg
		}
		if lang == "js" {
			tier = "stream"
		}
		if !strings.Contains(all, "F32LIT "+tier+" ") {
			t.Errorf("no driver prints F32LIT %s", tier)
		}
	}
	for _, tok := range []string{f32LitMinToken, f32LitMaxToken} {
		if !strings.Contains(all, tok) {
			t.Errorf("no driver writes %s", tok)
		}
	}
}
