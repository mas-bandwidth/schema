// Nightly fuzzing for the C# port: the corpus manifest and the planted-bad-
// pattern test that make/schema-cs-nightly-fuzz.mk runs around the fuzzer.
//
// Both tests read what the make fragment wrote under build/, so they are
// functional-tier: the default `go test ./...` skips them through slowtest.
// Under SCHEMA_SLOW=1 alone (ci-full.yml's blanket go test) they skip by name
// when the corpus/readers are not built; under SCHEMA_REQUIRE_CORPUS=1 — which
// `make schema-cs-nightly-fuzz-planted-pattern` sets after generating them — a
// missing build is a hard failure, never a silent green. The planted test's
// contract is the negative control's: the clean reader tree is GREEN, the tree
// with the plant is RED.
package ci

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

const csFuzzPlantedMarker = "SABOTAGED: dropped bounds check"

func csFuzzReadersDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("SCHEMA_CS_FUZZ_READERS"); dir != "" {
		return dir
	}
	abs, err := filepath.Abs(filepath.Join("..", "..", "build", "schema-cs-nightly-fuzz", "readers"))
	if err != nil {
		t.Fatalf("resolve readers dir: %v", err)
	}
	return abs
}

func csRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}

// TestSchemaCSNightlyFuzzCorpus covers the card's corpus rule: every schema the
// generation targets read is in the manifest, and every manifest line has a
// generated reader. A corpus directory dropped from the list is the failure
// this test exists to name.
func TestSchemaCSNightlyFuzzCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz corpus")

	root := csRepoRoot(t)
	manifest := filepath.Join(filepath.Dir(csFuzzReadersDir(t)), "corpus.txt")
	f, err := os.Open(manifest)
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("open corpus manifest %s: %v (run make schema-cs-nightly-fuzz-readers)", manifest, err)
		}
		t.Skipf("schema-cs-nightly-fuzz: corpus manifest %s absent; run make schema-cs-nightly-fuzz-readers", manifest)
	}
	defer f.Close()

	seenDir := map[string]bool{}
	lines := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("corpus manifest line %q is not schema<TAB>outdir", line)
		}
		schema, outdir := parts[0], parts[1]
		if !filepath.IsAbs(schema) {
			schema = filepath.Join(root, schema)
		}
		if !filepath.IsAbs(outdir) {
			outdir = filepath.Join(root, outdir)
		}
		if _, err := os.Stat(schema); err != nil {
			t.Errorf("corpus schema %s: %v", schema, err)
		}
		units, err := filepath.Glob(filepath.Join(outdir, "*.cs"))
		if err != nil || len(units) == 0 {
			t.Errorf("corpus schema %s has no generated reader under %s", schema, outdir)
		}
		seenDir[filepath.Dir(strings.TrimPrefix(schema, root+string(os.PathSeparator)))] = true
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read corpus manifest: %v", err)
	}
	if lines == 0 {
		t.Fatal("the corpus manifest is empty")
	}

	for _, dir := range []string{
		"tables/examples", "tables/pointers", "tables/block", "tables/blockhome",
		"tables/messages", "tables/stream", "tables/blobs", "tables/scalars",
		"tables/maps", "tables/lists", "tables/backend",
		"tables/vocab", "tables/vocab9", "examples-wide",
	} {
		if !seenDir[dir] {
			t.Errorf("corpus directory %s contributes no schema to the manifest", dir)
		}
	}
}

// TestSchemaCSNightlyFuzzPlantedPattern is the planted-bad-pattern test. It
// PASSES on the clean reader tree and FAILS the moment the control's plant is
// in, naming the marker and the file. The make target asserts both directions:
// the clean run must print --- PASS and the planted run must be red.
func TestSchemaCSNightlyFuzzPlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C# nightly-fuzz planted reader")

	readers := csFuzzReadersDir(t)
	files, err := filepath.Glob(filepath.Join(readers, "*", "*.cs"))
	if err != nil {
		t.Fatalf("list generated readers: %v", err)
	}
	if len(files) == 0 {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("no generated readers under %s (run make schema-cs-nightly-fuzz-readers)", readers)
		}
		t.Skipf("schema-cs-nightly-fuzz: no generated readers under %s; run make schema-cs-nightly-fuzz-readers", readers)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(data), csFuzzPlantedMarker) {
			t.Fatalf("planted reader bug %q present in %s", csFuzzPlantedMarker, file)
		}
	}
}
