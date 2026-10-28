package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestCIFullGoTestRunsEveryTestExactlyOnce holds ci-full.yml's go-test job to
// the rule its rows rest on: every top-level test in this module is selected by
// exactly one SCHEMA_SLOW=1 `go test` step of that job. The job is cut into
// rows by package and, for ./compiler, by test name (-run on one row, -skip
// with the same pattern on another), so the two ways it can go wrong are a
// test no row runs and a test two rows run. Both are red here by name.
//
// The selection is read with the same parser the slow-gate accounting credits
// CI steps with, so a row this test can read is a row the accounting can read,
// and a step the parser refuses selects nothing and turns its tests red here.
func TestCIFullGoTestRunsEveryTestExactlyOnce(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := workflowJob(string(body), "go-test")
	if err != nil {
		t.Fatal(err)
	}
	steps, err := parseWorkflowContent("ci-full.yml", job)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		t.Fatal("ci-full.yml's go-test job carries no `go test` step: this test would pass over an empty set")
	}
	for _, s := range steps {
		if !s.enabled {
			t.Errorf("%s (%s): a go test step the selection parser cannot read, or one with SCHEMA_SLOW unset or a row condition no matrix row satisfies; it is credited with nothing", s.desc, s.name)
		}
	}
	tests, err := moduleTests(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 {
		t.Fatal("found no tests in the module: this test would pass over an empty set")
	}
	for _, gt := range tests {
		var by []string
		for _, s := range steps {
			if s.enabled && s.selector(gt.pkg, gt.name) {
				by = append(by, s.name)
			}
		}
		switch len(by) {
		case 1:
		case 0:
			t.Errorf("%s is run by no row of ci-full.yml's go-test job", gt.Display())
		default:
			t.Errorf("%s is run by %d rows of ci-full.yml's go-test job (%s): a test belongs to exactly one", gt.Display(), len(by), strings.Join(by, "; "))
		}
	}
}

// TestShardAccountingRefusesAGapAndAnOverlap is the reversed witness for the
// test above: a -run row whose -skip twin uses a different pattern leaves one
// test in no row and puts another in two, and a complement that does not drop
// the package a named row runs runs it twice.
func TestShardAccountingRefusesAGapAndAnOverlap(t *testing.T) {
	t.Parallel()
	count := func(workflow string, pkg, name string) int {
		steps, err := parseWorkflowContent("ci-full.yml", workflow)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, s := range steps {
			if s.enabled && s.selector(pkg, name) {
				n++
			}
		}
		return n
	}
	const head = "jobs:\n  go-test:\n    strategy:\n      matrix:\n        group: [ a, b, c ]\n    steps:\n"
	step := func(group, run string) string {
		return fmt.Sprintf("      - name: %s\n        if: matrix.group == '%s'\n        env:\n          SCHEMA_SLOW: '1'\n        run: %s\n", group, group, run)
	}
	good := head + step("a", "go test ./compiler -run '^(TestA|TestB)$'") +
		step("b", "go test ./compiler -skip '^(TestA|TestB)$'") +
		step("c", "go test $(go list ./... | grep -v '/internal/codegen/' | grep -v '/compiler$')")
	for _, name := range []string{"TestA", "TestB", "TestC"} {
		if n := count(good, "./compiler", name); n != 1 {
			t.Errorf("complementary -run/-skip rows select compiler.%s %d times, want 1", name, n)
		}
	}
	if n := count(good, "./internal/fuzz", "TestF"); n != 1 {
		t.Errorf("the complement selects internal/fuzz.TestF %d times, want 1", n)
	}
	if n := count(good, "./internal/codegen/gotable", "TestG"); n != 0 {
		t.Errorf("the complement selects an emitter package %d times, want 0", n)
	}
	drift := head + step("a", "go test ./compiler -run '^(TestA|TestB)$'") +
		step("b", "go test ./compiler -skip '^(TestA|TestC)$'")
	if n := count(drift, "./compiler", "TestC"); n != 0 {
		t.Errorf("drifted patterns: compiler.TestC selected %d times, want 0 (the gap)", n)
	}
	if n := count(drift, "./compiler", "TestB"); n != 2 {
		t.Errorf("drifted patterns: compiler.TestB selected %d times, want 2 (the overlap)", n)
	}
	overlap := head + step("a", "go test ./compiler -run '^TestA$'") +
		step("c", "go test $(go list ./... | grep -v '/internal/codegen/')")
	if n := count(overlap, "./compiler", "TestA"); n != 2 {
		t.Errorf("a complement that keeps ./compiler selects compiler.TestA %d times, want 2", n)
	}
	sub := head + step("a", "go test $(go list ./internal/codegen/... | grep -v '/elixirtable$')") +
		step("b", "go test ./internal/codegen/elixirtable")
	if n := count(sub, "./internal/codegen/elixirtable", "TestE"); n != 1 {
		t.Errorf("a ./dir/... complement and a named step select elixirtable.TestE %d times, want 1", n)
	}
	if n := count(sub, "./internal/codegen/gotable", "TestG"); n != 1 {
		t.Errorf("a ./dir/... complement selects gotable.TestG %d times, want 1", n)
	}
	if n := count(sub, "./compiler", "TestA"); n != 0 {
		t.Errorf("a ./internal/codegen/... complement reaches outside its directory: compiler.TestA selected %d times, want 0", n)
	}
	unknownRow := head + "      - name: x\n        if: matrix.group == 'z'\n        env:\n          SCHEMA_SLOW: '1'\n        run: go test ./compiler\n"
	if n := count(unknownRow, "./compiler", "TestA"); n != 0 {
		t.Errorf("a step on a row the matrix does not declare selects compiler.TestA %d times, want 0", n)
	}
	moduleRoot := head + step("c", "go test $(go list ./... | grep -v '/v2/compiler$')")
	if n := count(moduleRoot, "./compiler", "TestA"); n != 0 {
		t.Errorf("a '/v2/compiler$' filter keeps ./compiler: selected %d times, want 0", n)
	}
	if n := count(moduleRoot, "./internal/compilerx", "TestA"); n != 1 {
		t.Errorf("a '$'-anchored filter drops a package it does not end: selected %d times, want 1", n)
	}
}

// workflowJob returns one job of a workflow file, with the file's top-level
// lines ahead of it, so the step parser sees that job's steps and no other's.
func workflowJob(content, name string) (string, error) {
	lines := strings.Split(content, "\n")
	start := -1
	for i, line := range lines {
		if line == "  "+name+":" {
			start = i
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("no job %q", name)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if len(line) > 2 && line[:2] == "  " && line[2] != ' ' && line[2] != '#' {
			end = i
			break
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
			end = i
			break
		}
	}
	return "jobs:\n" + strings.Join(lines[start:end], "\n") + "\n", nil
}

var topLevelTest = regexp.MustCompile(`(?m)^func (Test[^a-z(][^(]*|Test)\(\w+ \*testing\.T\)`)

// moduleTests lists every top-level test function in this module's packages:
// the directories `go list ./...` walks (no testdata, no _ or . prefix, no
// nested module), read from source rather than by running the toolchain.
func moduleTests(root string) ([]gatedTest, error) {
	var tests []gatedTest
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root {
				if name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := "./" + filepath.ToSlash(rel)
		if rel == "." {
			pkg = "."
		}
		for _, m := range topLevelTest.FindAllStringSubmatch(string(src), -1) {
			tests = append(tests, gatedTest{pkg: pkg, name: m[1]})
		}
		return nil
	})
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].pkg != tests[j].pkg {
			return tests[i].pkg < tests[j].pkg
		}
		return tests[i].name < tests[j].name
	})
	return tests, err
}
