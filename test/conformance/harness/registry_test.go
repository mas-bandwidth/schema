package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
		"#!/bin/sh\ncase \"$1\" in\nbuild) exit 0 ;;\nrun) echo 'zz,bench_table,write,1,1,1,1,1,1,1,0,0,table,pkg,contract,default,unknown' ;;\nesac\n")
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
		key := strings.SplitN(what, "-", 2)[0]
		if !strings.Contains(after[key], needle) {
			t.Errorf("%s did not discover the planted language: want %q in\n%s", key, needle, after[key])
		}
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

	m, err := matrix(filepath.Join(tree, "test", "conformance"))
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
