package goldens

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// lintClassesDir is the golangci-lint classes corpus (PR #4515, F-210): a
// packet schema and a table schema that reach every class of finding the
// generated-Go lint gate's ratchet was lowered for. make
// schema-go-generated-golangci-lint-gate lints the same files with the pinned
// golangci-lint against lint.ratchet.tsv beside them.
const lintClassesDir = "../../testdata/golangci-lint-classes"

// TestGolangciLintClassesShapes generates Go from the classes corpus and holds
// every row of its shapes.tsv: errcheck's flat packet runs discard each sticky
// stream error explicitly, G115's same-width signed lanes and narrowing reads
// go through the lane helpers, masks and the kind check, and G103's region and
// overlay access goes through one helper per package. Each fixed shape sits
// beside a `0 0` row for the shape it replaced.
func TestGolangciLintClassesShapes(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(lintClassesDir, "shapes.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	generated := map[string]map[string][]byte{}
	for _, corpus := range []string{"packet", "tables"} {
		generated[corpus] = generate(t, loadCorpusDir(t, filepath.Join(lintClassesDir, corpus)), "go", nil)
	}
	classes := map[string]int{}
	for n, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("shapes.tsv line %d: %d fields, want 6", n+1, len(f))
		}
		corpus, file, class := f[0], f[1], f[4]
		low, err := strconv.Atoi(f[2])
		if err != nil {
			t.Fatalf("shapes.tsv line %d: min: %v", n+1, err)
		}
		high := -1
		if f[3] != "-" {
			if high, err = strconv.Atoi(f[3]); err != nil {
				t.Fatalf("shapes.tsv line %d: max: %v", n+1, err)
			}
		}
		re, err := regexp.Compile(f[5])
		if err != nil {
			t.Fatalf("shapes.tsv line %d: %v", n+1, err)
		}
		src, ok := generated[corpus][file]
		if !ok {
			t.Errorf("shapes.tsv line %d: the %s corpus emits no %s", n+1, corpus, file)
			continue
		}
		count := 0
		for l := range strings.SplitSeq(string(src), "\n") {
			if re.MatchString(l) {
				count++
			}
		}
		if count < low || (high >= 0 && count > high) {
			t.Errorf("%s/%s (%s): %d lines match %q, want %d..%s", corpus, file, class, count, f[5], low, f[3])
		}
		classes[class]++
	}
	for _, class := range []string{"errcheck", "G115", "G103"} {
		if classes[class] == 0 {
			t.Errorf("shapes.tsv has no %s row: every class the ratchet was lowered for is tied to an emitted shape", class)
		}
	}
}
