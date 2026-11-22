package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestJSONFuzzDeterministicAndCategories(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	first, err := generateJSONFuzzRawCases(m, defaultJSONDir, 1, 300)
	if err != nil {
		t.Fatalf("generateJSONFuzzRawCases(seed=1): %v", err)
	}
	second, err := generateJSONFuzzRawCases(m, defaultJSONDir, 1, 300)
	if err != nil {
		t.Fatalf("generateJSONFuzzRawCases(seed=1 repeat): %v", err)
	}
	if len(first) != 300 || len(second) != 300 {
		t.Fatalf("expected 300 cases, got %d and %d", len(first), len(second))
	}
	for i := range first {
		if first[i].category != second[i].category || first[i].unit != second[i].unit ||
			first[i].root != second[i].root || !bytes.Equal(first[i].raw, second[i].raw) {
			t.Fatalf("non-deterministic case at %d", i)
		}
	}

	other, err := generateJSONFuzzRawCases(m, defaultJSONDir, 2, 300)
	if err != nil {
		t.Fatalf("generateJSONFuzzRawCases(seed=2): %v", err)
	}
	sameAll := true
	for i := range first {
		if !bytes.Equal(first[i].raw, other[i].raw) {
			sameAll = false
			break
		}
	}
	if sameAll {
		t.Fatal("different seeds produced identical sequences")
	}

	seenPrefix := map[string]bool{}
	for _, c := range first {
		prefix, _, _ := strings.Cut(c.category, "-")
		seenPrefix[prefix] = true
	}
	for _, want := range []string{"num", "utf8", "dup", "order", "b64", "ts", "evo", "arrayroot", "bom", "comment", "nest", "amp", "mut"} {
		if !seenPrefix[want] {
			t.Errorf("missing adversarial category prefix %q in N=300 schedule", want)
		}
	}

	// Verify that VERIFY_JSON_FUZZ_N (360) includes 100% of buildTargetedJSONCases()
	// plus random seed mutations, including the jf00584 &-prefix regression cases.
	allTargeted := buildTargetedJSONCases()
	verifySlice, err := generateJSONFuzzRawCases(m, defaultJSONDir, 1, 360)
	if err != nil {
		t.Fatalf("generateJSONFuzzRawCases(N=360): %v", err)
	}
	if len(allTargeted) >= 360 {
		t.Fatalf("targeted bank (%d) exceeds VERIFY_JSON_FUZZ_N=360; raise VERIFY_JSON_FUZZ_N so 100%% of targeted cases run", len(allTargeted))
	}
	seenCat := map[string]bool{}
	for _, c := range verifySlice {
		seenCat[c.category] = true
	}
	for _, tc := range allTargeted {
		if !seenCat[tc.category] {
			t.Errorf("targeted regression case %q missing from N=360 verify slice", tc.category)
		}
	}
}

func TestJSONFuzzPlantedNegativeControls(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	makeDriverScript := func(t *testing.T, sabotage string) string {
		t.Helper()
		script := filepath.Join(t.TempDir(), "synthetic-driver.sh")
		body := "#!/bin/sh\n" +
			"export SCHEMA_JSONFUZZ_TEST_DRIVER=1\n" +
			"export SCHEMA_JSONFUZZ_SABOTAGE=" + sabotage + "\n" +
			"exec \"" + exe + "\" -test.run=^TestJSONFuzzSyntheticDriverHelper$ -- \"$@\"\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write synthetic driver: %v", err)
		}
		return script
	}

	// 1. Positive control: exact oracle behaviour passes 100%.
	var cleanOut bytes.Buffer
	err = jsonFuzz(&cleanOut, m, jsonFuzzOptions{
		driverCmd: makeDriverScript(t, "none"),
		only:      "synthetic",
		seed:      1,
		n:         40,
		jsonDir:   defaultJSONDir,
	})
	if err != nil {
		t.Fatalf("positive control failed: %v\n%s", err, cleanOut.String())
	}
	if !strings.Contains(cleanOut.String(), "PASS") {
		t.Fatalf("expected PASS in output, got:\n%s", cleanOut.String())
	}

	// 2. Planted negative controls across all 4 checked surfaces/invariants.
	for _, tc := range []struct {
		sabotage string
		wantSub  string
	}{
		{sabotage: "flip-json-char", wantSub: "FromJson -> ToJson differs from oracle"},
		{sabotage: "accept-refused", wantSub: "oracle REFUSED input, leg ACCEPTED"},
		{sabotage: "corrupt-wire", wantSub: "FromJson -> Save wire differs from oracle"},
		{sabotage: "flip-write-json", wantSub: "Load(wire) -> ToJson differs from oracle"},
	} {
		t.Run(tc.sabotage, func(t *testing.T) {
			var out bytes.Buffer
			failedFile := filepath.Join(t.TempDir(), "failed.json")
			err := jsonFuzz(&out, m, jsonFuzzOptions{
				driverCmd: makeDriverScript(t, tc.sabotage),
				only:      "synthetic",
				seed:      1,
				n:         40,
				failed:    failedFile,
				jsonDir:   defaultJSONDir,
			})
			if err == nil {
				t.Fatalf("planted negative control %s PASSED unexpectedly:\n%s", tc.sabotage, out.String())
			}
			if !strings.Contains(out.String(), tc.wantSub) {
				t.Fatalf("planted negative control %s missed expected diagnostic %q:\n%s", tc.sabotage, tc.wantSub, out.String())
			}
			if _, rerr := os.Stat(failedFile); rerr != nil {
				t.Fatalf("expected saved failure artifact at %s: %v", failedFile, rerr)
			}
		})
	}
}

func TestJSONFuzzSyntheticDriverHelper(t *testing.T) {
	if os.Getenv("SCHEMA_JSONFUZZ_TEST_DRIVER") != "1" {
		t.Skip("helper for TestJSONFuzzPlantedNegativeControls")
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) < 3 {
		os.Exit(2)
	}
	manifestPath, surface, outDir := args[0], args[1], args[2]
	sabotage := os.Getenv("SCHEMA_JSONFUZZ_SABOTAGE")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		os.Exit(1)
	}
	var m Manifest
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if f[0] == "unit" && len(f) >= 3 {
			m.Units = append(m.Units, Unit{Key: f[1], Paths: f[2:]})
		}
	}
	u := newUnits(&m)
	models := map[string]*tabletext.Model{}
	getModel := func(key string) *tabletext.Model {
		if mod := models[key]; mod != nil {
			return mod
		}
		irU, uerr := u.get(key)
		if uerr != nil {
			os.Exit(1)
		}
		mod := tabletext.NewModel(irU)
		models[key] = mod
		return mod
	}

	sabotaged := false
	switch surface {
	case "json-hostile":
		for _, line := range lines {
			f := strings.Fields(strings.TrimSpace(line))
			if len(f) < 5 || f[0] != "json-hostile" {
				continue
			}
			name, unitKey, rootName, tree := f[1], f[2], f[3], f[4]
			raw, rerr := os.ReadFile(filepath.Join(tree, rootName+".json"))
			if rerr != nil {
				os.Exit(1)
			}
			mod := getModel(unitKey)
			val := mod.New(mod.Lookup(rootName))
			var rep tabletext.Report
			ok := strictReadJSON(mod, val, raw, &rep)
			if !ok {
				if sabotage == "accept-refused" && !sabotaged {
					sabotaged = true
					_ = os.WriteFile(filepath.Join(outDir, name), []byte("read\n{}\n"), 0o644)
					continue
				}
				_ = os.WriteFile(filepath.Join(outDir, name), []byte("refused\n"), 0o644)
				continue
			}
			jb, _ := mod.Write(val)
			jb = oneText(jb)
			// a value the strict read accepted must save; a failed save is a
			// driver failure the parent reports by the exit status
			wb, eerr := tablewire.Encode(mod, val)
			if eerr != nil {
				os.Stderr.WriteString(name + ": save: " + eerr.Error() + "\n")
				os.Exit(1)
			}
			if sabotage == "flip-json-char" && !sabotaged && len(jb) > 2 {
				sabotaged = true
				jb = append([]byte{}, jb...)
				jb[0] = ' '
			}
			if sabotage == "corrupt-wire" && !sabotaged && len(wb) > 0 {
				sabotaged = true
				wb = append([]byte{}, wb...)
				wb[len(wb)-1] ^= 0xff
			}
			_ = os.WriteFile(filepath.Join(outDir, name), append([]byte("read\n"), jb...), 0o644)
			_ = os.WriteFile(filepath.Join(outDir, name+".wire"), wb, 0o644)
		}
	case "json-write":
		if len(lines) == 0 {
			t.Fatal("lines is empty: the loop below would check nothing")
		}
		for _, line := range lines {
			f := strings.Fields(strings.TrimSpace(line))
			if len(f) < 5 || f[0] != "instance" {
				continue
			}
			name, unitKey, rootName, wirePath := f[1], f[2], f[3], f[4]
			wb, rerr := os.ReadFile(wirePath)
			if rerr != nil {
				os.Exit(1)
			}
			mod := getModel(unitKey)
			val := mod.New(mod.Lookup(rootName))
			var rep tabletext.Report
			if ok, derr := tablewire.Decode(mod, val, wb, &rep); derr != nil || !ok {
				os.Exit(1)
			}
			jb, _ := mod.Write(val)
			jb = oneText(jb)
			if sabotage == "flip-write-json" && !sabotaged && len(jb) > 2 {
				sabotaged = true
				jb = append([]byte{}, jb...)
				jb[0] = ' '
			}
			_ = os.WriteFile(filepath.Join(outDir, name+".json"), jb, 0o644)
		}
	}
	os.Exit(0)
}

func chdirRepoRoot(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cur := wd
	for {
		if _, err := os.Stat(filepath.Join(cur, "testdata", "conformance", "tables", "MANIFEST.txt")); err == nil {
			if cur != wd {
				if err := os.Chdir(cur); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chdir(wd) })
			}
			return
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			t.Fatalf("could not find repo root from %s", wd)
		}
		cur = parent
	}
}
