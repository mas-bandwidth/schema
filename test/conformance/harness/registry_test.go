package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestRegistryDiscoversAPlantedLanguage is the registry gate
// (docs/CONTRIBUTING.md, "Adding a language"): a new language is one file or
// one directory per registry, and no shared file lists it. The test copies
// the registries into a scratch tree, plants a language called zz — a driver
// and its ci.json, a bench leg, a make/zz.mk — and requires the harness, the
// CI matrix, the bench pass and the Makefile to discover it. It reads the
// same tree BEFORE planting and requires zz absent, which is what says the
// discovery is doing the work rather than a list somewhere.
func TestRegistryDiscoversAPlantedLanguage(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"make", "sh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s on PATH", tool)
		}
	}

	tree := t.TempDir()
	copyTree(t, filepath.Join(root, "test", "conformance"), filepath.Join(tree, "test", "conformance"),
		func(rel string) bool {
			base := filepath.Base(rel)
			return base == "driver" || base == "ci.json" || rel == "README.md"
		})
	copyTree(t, filepath.Join(root, "bench", "tables"), filepath.Join(tree, "bench", "tables"),
		func(rel string) bool { return filepath.Base(rel) == "leg" || rel == "run.sh" })
	copyTree(t, filepath.Join(root, "make"), filepath.Join(tree, "make"), func(string) bool { return true })
	copyFile(t, filepath.Join(root, "Makefile"), filepath.Join(tree, "Makefile"))

	before := discover(t, tree)
	for what, out := range before {
		if strings.Contains(out, "zz") {
			t.Fatalf("%s names zz before it is planted:\n%s", what, out)
		}
	}

	// the plant: one directory or file per registry, and nothing else
	writeExec(t, filepath.Join(tree, "test", "conformance", "zz", "driver"), "#!/bin/sh\nexit 0\n")
	writeFile(t, filepath.Join(tree, "test", "conformance", "zz", "ci.json"),
		`{"targets": "build/conformance-harness build/conformance-zz", "zztool": "1.0"}`+"\n")
	writeExec(t, filepath.Join(tree, "bench", "tables", "zz", "leg"),
		"#!/bin/sh\ncase \"$1\" in\nbuild) exit 0 ;;\nrun) echo 'zz,bench_table,write,1,1,1,1,1,1,1,0,0,table,pkg,contract,default,unknown'\n"+
			"echo 'zz,bench_table,round_trip,1,1,1,1,1,1,1,0,0,table,pkg,contract,default,unknown' ;;\nesac\n")
	writeFile(t, filepath.Join(tree, "make", "zz.mk"), strings.Join([]string{
		".PHONY: test-zz update-goldens-zz",
		"test-zz: ;",
		"update-goldens-zz: ;",
		"build/conformance-zz: ;",
		"generated/bench/tables/zz/.stamp: ;",
		"TEST_LEGS         += test-zz",
		"CONFORMANCE_LEGS  += build/conformance-zz",
		"CONFORMANCE_ENV   += ZZ=1",
		"BENCH_TABLES_LEGS += generated/bench/tables/zz/.stamp",
		"GOLDENS_LEGS      += update-goldens-zz",
		"",
	}, "\n"))

	after := discover(t, tree)
	want := map[string]string{
		"harness":     "zz",
		"matrix":      `"lang":"zz"`,
		"matrix-keys": `"zztool":""`, // every row carries every key
		"bench":       "zz,bench_table,write",
		"make":        "test: test-c test-cs test-dart test-elixir test-go test-java test-js test-rust test-zz",
		"make-conf":   "build/conformance-zz",
		"make-bench":  "generated/bench/tables/zz/.stamp",
		"make-golden": "update-goldens-zz",
	}
	for what, needle := range want {
		key, _, _ := strings.Cut(what, "-")
		if !strings.Contains(after[key], needle) {
			t.Errorf("%s did not discover the planted language: want %q in\n%s", key, needle, after[key])
		}
	}
	// a CI row with no driver behind it is refused, not run
	writeFile(t, filepath.Join(tree, "test", "conformance", "zy", "ci.json"), `{"targets": "build/conformance-zy"}`+"\n")
	if _, err := matrixFor(filepath.Join(tree, "test", "conformance"), tierPullRequest); err == nil || !strings.Contains(err.Error(), "zy") {
		t.Errorf("a ci.json with no driver was not refused: %v", err)
	}

	// the reference leg stays first whatever is planted

	if !strings.HasPrefix(after["harness"], "cpp ") {
		t.Errorf("the reference leg is not first: %s", after["harness"])
	}
}

// discover reads every registry of one tree: the harness's driver list, the
// CI matrix, the bench pass's rows, and what the Makefile registered.
func discover(t *testing.T, tree string) map[string]string {
	t.Helper()
	out := map[string]string{}

	drivers, err := discoverDrivers(filepath.Join(tree, "test", "conformance"))
	if err != nil {
		t.Fatal(err)
	}
	var langs []string
	for _, d := range drivers {
		langs = append(langs, d.lang)
	}
	out["harness"] = strings.Join(langs, " ")

	m, err := matrixFor(filepath.Join(tree, "test", "conformance"), tierPullRequest)
	if err != nil {
		t.Fatal(err)
	}
	out["matrix"] = string(m)

	bench := exec.Command("sh", "bench/tables/run.sh", "--only", "zz", "--bare")
	bench.Dir = tree
	var stdout, stderr bytes.Buffer
	bench.Stdout, bench.Stderr = &stdout, &stderr
	_ = bench.Run() // "no legs ran" exits 1 before the plant, which is the point
	out["bench"] = stdout.String()

	mk := exec.Command("make", "-s", "registry")
	mk.Dir = tree
	registry, err := mk.Output()
	if err != nil {
		t.Fatalf("make registry: %v", err)
	}
	out["make"] = string(registry)
	return out
}

func copyTree(t *testing.T, from, to string, keep func(rel string) bool) {
	t.Helper()
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if info.IsDir() || !keep(rel) {
			return nil
		}
		copyFile(t, p, filepath.Join(to, rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeExec(t *testing.T, path, text string) {
	t.Helper()
	writeFile(t, path, text)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestMatrixForTiers is the gate on the tier filter itself — the code that
// decides, for every leg, whether CI runs it on the commit or overnight. The
// committed registry is the subject: the per-commit tier must be the eight
// legs that fit the owner's one-to-two-minute law, the nightly tier exactly
// the legs moved out of it, and no leg may sit in both or in neither. A new
// port lands in this table with its tier, which is the deliberate act the law
// asks for.
func TestMatrixForTiers(t *testing.T) {
	const registry = ".."

	langsOf := func(t *testing.T, raw []byte) []string {
		t.Helper()
		var got struct {
			Include []map[string]string `json:"include"`
		}
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("the matrix is not JSON: %v", err)
		}
		var langs []string
		for _, row := range got.Include {
			langs = append(langs, row["lang"])
		}
		sort.Strings(langs)
		return langs
	}

	for _, tc := range []struct {
		name  string
		tier  string
		want  []string
		wantE string
	}{
		{
			name: "the per-commit tier is the eight legs inside the law",
			tier: tierPullRequest,
			want: []string{"cpp", "cs", "dart", "elixir", "go", "java", "js", "rust"},
		},
		{
			name: "the nightly tier is exactly the legs moved out of it",
			tier: tierNightly,
			want: []string{"c"},
		},
		{
			name:  "an unknown tier is refused, not silently empty",
			tier:  "bogus",
			wantE: `"bogus" is not a tier`,
		},
		{
			name:  "no tier at all is refused too",
			tier:  "",
			wantE: `"" is not a tier`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := matrixFor(registry, tc.tier)
			if tc.wantE != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantE) {
					t.Fatalf("matrixFor(%q): want an error naming %q, got %v", tc.tier, tc.wantE, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("matrixFor(%q): %v", tc.tier, err)
			}
			if got := langsOf(t, raw); !slices.Equal(got, tc.want) {
				t.Errorf("matrixFor(%q) legs = %v, want %v — a leg changed tier, or a port landed without one", tc.tier, got, tc.want)
			}
		})
	}

	// Every registered leg is in one tier and one only: a leg in both runs
	// twice under one job name, a leg in neither is a check that no workflow
	// performs.
	t.Run("the tiers partition the registry", func(t *testing.T) {
		perCommit, err := matrixFor(registry, tierPullRequest)
		if err != nil {
			t.Fatal(err)
		}
		nightly, err := matrixFor(registry, tierNightly)
		if err != nil {
			t.Fatal(err)
		}
		in := map[string]int{}
		for _, lang := range langsOf(t, perCommit) {
			in[lang]++
		}
		for _, lang := range langsOf(t, nightly) {
			in[lang]++
		}
		for lang, n := range in {
			if n != 1 {
				t.Errorf("%s is in %d tiers, want exactly 1", lang, n)
			}
		}
		drivers, err := discoverDrivers(registry)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range drivers {
			if in[d.lang] == 0 {
				t.Errorf("%s is registered and in no tier: no workflow runs it", d.lang)
			}
		}
		if len(in) != len(drivers) {
			t.Errorf("the tiers name %d legs, the registry has %d", len(in), len(drivers))
		}
	})
}

// TestMatrixForRefusals covers the two refusals the committed registry cannot
// reach: a ci.json naming a tier that does not exist, and a tier whose filter
// keeps nothing — a workflow that would expand to no job and report green.
func TestMatrixForRefusals(t *testing.T) {
	plant := func(t *testing.T, rows map[string]string) string {
		t.Helper()
		tree := t.TempDir()
		for lang, row := range rows {
			writeExec(t, filepath.Join(tree, lang, "driver"), "#!/bin/sh\nexit 0\n")
			writeFile(t, filepath.Join(tree, lang, "ci.json"), row+"\n")
		}
		return tree
	}

	t.Run("a ci.json naming a tier that does not exist", func(t *testing.T) {
		tree := plant(t, map[string]string{
			"cpp": `{"targets": "build/conformance-cpp"}`,
			"zz":  `{"targets": "build/conformance-zz", "tier": "weekly"}`,
		})
		_, err := matrixFor(tree, tierPullRequest)
		if err == nil || !strings.Contains(err.Error(), `"weekly" is not a tier`) || !strings.Contains(err.Error(), "zz") {
			t.Fatalf("a bad tier in a row was not refused by name: %v", err)
		}
	})

	t.Run("a tier no leg is in", func(t *testing.T) {
		tree := plant(t, map[string]string{"cpp": `{"targets": "build/conformance-cpp"}`})
		_, err := matrixFor(tree, tierNightly)
		if err == nil || !strings.Contains(err.Error(), "expands to no job") {
			t.Fatalf("an empty tier was not refused: %v", err)
		}
	})
}
