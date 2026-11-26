package fuzz_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedPointProbe is one seed's generated unit and the probe that reads its
// drawn values.
type fixedPointProbe struct {
	seed int
	src  string
	unit *ir.Unit
	test string
}

// runFixedPointProbes writes each probe as package s<seed> of one module and
// runs them in one go test, failing each probe that fails by its seed.
func runFixedPointProbes(t *testing.T, probes []fixedPointProbe) {
	t.Helper()
	dir := t.TempDir()
	if err := writeGeneratedTableModule(dir); err != nil {
		t.Fatal(err)
	}
	var pkgs []string
	for _, p := range probes {
		pkg := fmt.Sprintf("s%d", p.seed)
		if err := os.Mkdir(filepath.Join(dir, pkg), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := writeGeneratedTableProbe(filepath.Join(dir, pkg), p.unit, p.test); err != nil {
			t.Fatalf("seed %d: %v", p.seed, err)
		}
		pkgs = append(pkgs, "./"+pkg)
	}
	cmd := exec.Command("go", append([]string{"test", "-count=1", "-json", "-timeout=600s"}, pkgs...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	verdicts := fixedPointVerdicts(out)
	for _, p := range probes {
		v := verdicts["probe/"+fmt.Sprintf("s%d", p.seed)]
		if v.action != "pass" {
			t.Errorf("seed %d: generated Go reader failed (go test: %q)\n%s\n%s", p.seed, v.action, p.src, v.output)
		}
	}
	if runErr != nil && !t.Failed() {
		t.Fatalf("go test of the probes failed outside every probe: %v\n%s", runErr, stderr.String())
	}
}

// fixedPointVerdict is one probe package's end, as go test -json reports
// it: its final action ("pass", "fail", "skip"; "" when it never ended) and
// its output.
type fixedPointVerdict struct {
	action string
	output string
}

// fixedPointVerdicts reads go test -json's events into each package's
// verdict. A package-level event (no Test) that passes or fails is the
// package's end; a line that is not an event is kept as the output of the
// package "" so it is not lost.
func fixedPointVerdicts(stream []byte) map[string]fixedPointVerdict {
	out := map[string]fixedPointVerdict{}
	for line := range strings.SplitSeq(string(stream), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev struct {
			Action, Package, Test, Output string
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			v := out[""]
			v.output += line + "\n"
			out[""] = v
			continue
		}
		v := out[ev.Package]
		switch {
		case ev.Action == "output":
			v.output += ev.Output
		case ev.Test == "" && (ev.Action == "pass" || ev.Action == "fail" || ev.Action == "skip"):
			v.action = ev.Action
		}
		out[ev.Package] = v
	}
	return out
}

// TestFixedPointVerdictsReadEachPackage holds fixedPointVerdicts to go test
// -json: a package passes only on its own pass event, a test's pass inside a
// failing package does not pass it, a skipped package (no test files) is not
// a pass, and a package that never ended has no verdict. The verdict is what
// runFixedPointProbes fails a seed by. Planted: reading any pass event,
// a test's too, as the package's turns the failing package green (red).
func TestFixedPointVerdictsReadEachPackage(t *testing.T) {
	t.Parallel()
	stream := strings.Join([]string{
		`{"Action":"run","Package":"probe/s1","Test":"TestGeneratedTableJSON"}`,
		`{"Action":"pass","Package":"probe/s1","Test":"TestGeneratedTableJSON"}`,
		`{"Action":"pass","Package":"probe/s1"}`,
		`{"Action":"pass","Package":"probe/s2","Test":"TestGeneratedTableJSON"}`,
		`{"Action":"output","Package":"probe/s2","Output":"seed 2 instance 1: refused\n"}`,
		`{"Action":"fail","Package":"probe/s2"}`,
		`{"Action":"skip","Package":"probe/s3"}`,
		`{"Action":"run","Package":"probe/s4","Test":"TestGeneratedTableJSON"}`,
		"# probe/s5 [build failed]",
	}, "\n")
	v := fixedPointVerdicts([]byte(stream))
	for pkg, want := range map[string]string{"probe/s1": "pass", "probe/s2": "fail", "probe/s3": "skip", "probe/s4": "", "probe/s9": ""} {
		if got := v[pkg].action; got != want {
			t.Errorf("%s: verdict %q, want %q", pkg, got, want)
		}
	}
	if !strings.Contains(v["probe/s2"].output, "refused") {
		t.Errorf("probe/s2's output was lost: %q", v["probe/s2"].output)
	}
	if !strings.Contains(v[""].output, "build failed") {
		t.Errorf("a line that is not an event was lost: %q", v[""].output)
	}
}

// TestRandomTableGenFixedPoint pins the generator to valid schemas and values
// across deterministic seeds. Each sample is read by the generated Go table
// front end, following SPEC-TABLES §3 and §16.
func TestRandomTableGenFixedPoint(t *testing.T) {
	t.Parallel()

	fixedSeen := map[[2]int]bool{}
	ufixedSeen := map[[2]int]bool{}
	fractionSeen := map[string]bool{}
	floatSeen := map[string]bool{}
	var probes []fixedPointProbe
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
		probes = append(probes, fixedPointProbe{seed: seed, src: src, unit: unit, test: generatedTest.String()})
	}
	// Drawing is serial (one generator per seed, in order). The probes are
	// one module, a package each, run by one go test: one module load and one
	// build of the schema packages for all of them, where a go test per probe
	// paid both a hundred times (62 s alone on amd-2, 2026-11-25). Every
	// probe's package is run and answers by name (fixedPointVerdicts).
	runFixedPointProbes(t, probes)

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
	t.Fatal("no fixed/ufixed root field over seeds 0..399: the plant checked nothing")
}
