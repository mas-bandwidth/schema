package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// receiptTree is a tree whose two gated tests each have an armed make line,
// and the go test -json stream a plain run of it writes.
func receiptTree(t *testing.T) (root string, recipes []recipe, stream []string) {
	t.Helper()
	root = t.TempDir()
	writeTree(t, root, map[string]string{
		"Makefile":      ".PHONY: first second\nfirst:\n\tSCHEMA_SLOW=1 go test -count=1 -run '^TestFirst$$' ./pkg/\nsecond:\n\tSCHEMA_SLOW=1 go test -count=1 -run '^TestSecond$$' ./pkg/\n",
		"pkg/a_test.go": "package pkg\n\nimport \"testing\"\n\nfunc TestFirst(t *testing.T) { slowtest.Gate(t, \"x\") }\nfunc TestSecond(t *testing.T) { slowtest.Gate(t, \"x\") }\n",
	})
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	recipes, err = scanMakefiles(root, gated)
	if err != nil {
		t.Fatal(err)
	}
	const pkg = `"Package":"github.com/mas-bandwidth/schema/v2/pkg"`
	stream = []string{
		`{"Action":"start",` + pkg + `}`,
		`{"Action":"output",` + pkg + `,"Test":"TestFirst","Output":"` + gateSkipMarker + `\n"}`,
		`{"Action":"skip",` + pkg + `,"Test":"TestFirst"}`,
		`{"Action":"output",` + pkg + `,"Test":"TestSecond","Output":"` + gateSkipMarker + `\n"}`,
		`{"Action":"skip",` + pkg + `,"Test":"TestSecond"}`,
		`{"Action":"pass",` + pkg + `}`,
	}
	return root, recipes, stream
}

func writeStream(t *testing.T, root, name string, lines []string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// F-233: a transcript cut at an event boundary parses cleanly and holds fewer
// skips than the run; a go test that exited nonzero cannot prove every gated
// test reached its gate. Both are red; the whole stream of a clean run is
// green.
func TestCoverageRefusesACutTranscriptAndAFailedGoTest(t *testing.T) {
	t.Parallel()
	root, recipes, stream := receiptTree(t)
	full := writeStream(t, root, "full.jsonl", stream)
	cut := writeStream(t, root, "cut.jsonl", stream[:3])

	if rc := runCoverage(root, cut, 0, recipes, nil, nil); rc != 1 {
		t.Errorf("a transcript with no closing package record: runCoverage = %d, want 1", rc)
	}
	if rc := runCoverage(root, full, 1, recipes, nil, nil); rc != 1 {
		t.Errorf("the whole transcript of a go test that exited 1: runCoverage = %d, want 1", rc)
	}
	if rc := runCoverage(root, full, 2, recipes, nil, nil); rc != 1 {
		t.Errorf("the whole transcript of a go test that exited 2: runCoverage = %d, want 1", rc)
	}
	if rc := runCoverage(root, full, 0, recipes, nil, nil); rc != 0 {
		t.Errorf("control: the whole transcript of a clean run: runCoverage = %d, want 0", rc)
	}
}

// F-234: the table is the receipt; when it cannot be written the verdict is
// red, never a green line naming a file that is not there.
func TestCoverageTableThatCannotBeWrittenIsRed(t *testing.T) {
	t.Parallel()
	root, recipes, stream := receiptTree(t)
	full := writeStream(t, root, "full.jsonl", stream)
	table := filepath.Join(root, "build", "slowgate", "coverage.tsv")

	if rc := runCoverage(root, full, 0, recipes, nil, nil); rc != 0 {
		t.Fatalf("control: runCoverage = %d, want 0", rc)
	}
	b, err := os.ReadFile(table)
	if err != nil {
		t.Fatalf("control: a green accounting left no table: %v", err)
	}
	for _, want := range []string{"make\t./pkg.TestFirst\tfirst", "make\t./pkg.TestSecond\tsecond"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("control: the table lacks %q:\n%s", want, b)
		}
	}

	if err := os.Remove(table); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(table, 0o755); err != nil {
		t.Fatal(err)
	}
	if rc := runCoverage(root, full, 0, recipes, nil, nil); rc != 1 {
		t.Errorf("the table's path is a directory: runCoverage = %d, want 1", rc)
	}
}

// F-241: the go tool builds no package under testdata, vendor, or a directory
// whose name starts with _ or .; and slowtest.Gate( spelled in a comment or a
// string is not a call. None of them is a gated package.
func TestGatedPackagesAreBuiltPackagesThatCallTheGate(t *testing.T) {
	t.Parallel()
	const call = "package p\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { slowtest.Gate(t, \"x\") }\n"
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"real/a_test.go":             call,
		"testdata/fixture/a_test.go": call,
		"vendor/v/a_test.go":         call,
		"_hidden/a_test.go":          call,
		".dot/a_test.go":             call,
		"spelled/a_test.go":          "package spelled\n\nimport \"testing\"\n\n// slowtest.Gate(t, \"x\")\nfunc TestY(t *testing.T) { _ = `slowtest.Gate(t, \"x\")` }\n",
	})
	gated, err := gatedPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(maps.Keys(gated)); !slices.Equal(got, []string{"./real"}) {
		t.Errorf("gated packages = %v, want [./real]", got)
	}

	// a tree whose only calls are in places the go tool does not build, or
	// spelled and not called, holds no gated package: refused, not empty
	only := t.TempDir()
	writeTree(t, only, map[string]string{
		"testdata/fixture/a_test.go": call,
		"spelled/a_test.go":          "package spelled\n\n// slowtest.Gate(t, \"x\")\n",
	})
	if gated, err := gatedPackages(only); err == nil {
		t.Errorf("a tree with no built call: gatedPackages = %v, want an error", gated)
	}
}
