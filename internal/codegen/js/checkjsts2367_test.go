package js

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

func TestJsPacketScratchNarrowingHasNoDeadCompare(t *testing.T) {
	t.Parallel()
	modules := packetScratchCorpus(t)
	if _, ok := modules["examples/Wire.js"]; !ok {
		t.Fatal("examples/Wire.js was not generated")
	}
	if _, ok := modules["bench/corpus/Bench.js"]; !ok {
		t.Fatal("bench/corpus/Bench.js was not generated")
	}
	if faults := directScratchComparisons(modules); len(faults) != 0 {
		t.Fatalf("checkJs sees stale packet scratch narrowing:\n%s", strings.Join(faults, "\n"))
	}
	for name, wants := range map[string][]string{
		"examples/Wire.js": {
			"if (numberScratchValue() !== 171) { // const(171, 8)",
			"if (numberScratchValue() !== 0) { // reserved(5)",
		},
		"bench/corpus/Bench.js": {
			"if (numberScratchValue() !== 49374) { // const(49374, 16)",
			"if (numberScratchValue() !== 0) { // reserved(4)",
		},
	} {
		for _, want := range wants {
			if !strings.Contains(modules[name], want) {
				t.Errorf("%s lost required emitConstItem/emitReservedItem check %q", name, want)
			}
		}
	}
}

func TestJsPacketScratchNarrowingHasNoDeadCompareReversedWitness(t *testing.T) {
	t.Parallel()
	modules := packetScratchCorpus(t)
	const name = "examples/Wire.js"
	src := modules[name]
	needle := "if (numberScratchValue() !== 0) { // reserved(5)"
	plant := "if (NUMBER_SCRATCH.value !== 0) { // reserved(5)"
	if !strings.Contains(src, needle) {
		t.Fatalf("%s has no emitReservedItem witness", name)
	}
	planted := strings.Replace(src, needle, plant, 1)
	dir := t.TempDir()
	path := filepath.Join(dir, "Wire.js")
	if err := os.WriteFile(path, []byte(planted), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	faults := directScratchComparisons(map[string]string{name: string(data)})
	if len(faults) != 1 || !strings.Contains(faults[0], "emitReservedItem") {
		t.Fatalf("reversed witness must go red naming emitReservedItem: %v", faults)
	}
}

func directScratchComparisons(modules map[string]string) []string {
	var faults []string
	for name, src := range modules {
		for i, line := range strings.Split(src, "\n") {
			if !strings.Contains(line, "NUMBER_SCRATCH.value !==") {
				continue
			}
			emitter := "unknown emitter"
			switch {
			case strings.Contains(line, "// const("):
				emitter = "emitConstItem"
			case strings.Contains(line, "// reserved("):
				emitter = "emitReservedItem"
			}
			faults = append(faults, fmt.Sprintf("%s:%d: %s compares a stale narrowed scratch value", name, i+1, emitter))
		}
	}
	sort.Strings(faults)
	return faults
}

func packetScratchCorpus(t *testing.T) map[string]string {
	t.Helper()
	root := packetRepoRoot(t)
	units := map[string][]string{
		"examples":     schemaFiles(t, filepath.Join(root, "examples")),
		"bench/corpus": {filepath.Join(root, "bench/corpus/Bench.schema")},
	}
	modules := map[string]string{}
	for unit, paths := range units {
		var files []check.SourceFile
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			base := strings.TrimSuffix(filepath.Base(path), ".schema")
			ast, errs := parser.Parse(path, data)
			if len(errs) != 0 {
				t.Fatalf("parse %s: %v", path, errs)
			}
			files = append(files, check.SourceFile{Path: path, Name: filepath.Base(path), Base: base, Bytes: data, AST: ast})
		}
		u, errs := check.Unit(files)
		if len(errs) != 0 {
			t.Fatalf("check %s: %v", unit, errs)
		}
		out, err := Generate(u)
		if err != nil {
			t.Fatalf("generate %s: %v", unit, err)
		}
		for name, data := range out {
			modules[unit+"/"+name] = string(data)
		}
	}
	return modules
}

func packetRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			t.Fatal("go.mod not found")
		}
		dir = up
	}
}

func schemaFiles(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatalf("no schemas in %s", dir)
	}
	sort.Strings(paths)
	return paths
}
