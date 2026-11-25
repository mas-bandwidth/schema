package squirreltable_test

// THE NIGHTLY CAMPAIGN (make/schema-squirrel-nightly-fuzz.mk). It is the SAME
// seeded mutation fuzzer the pull request runs (fuzz_test.go): every instance
// wire of the tables conformance corpus, mutated with splitmix64, read through
// the Squirrel leg's generated load and through the compiler's own engine, and
// required to agree. What the nightly adds is an argument: the corpus file the
// harness reads, the seed and the mutation count. The search is a pure function
// of those arguments and reads no clock; its wall budget is go test's -timeout,
// which the Makefile sets from SQUIRREL_FUZZ_SECONDS. No finding is invented
// here and no second fuzzer is forked.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// nightlyFuzzCorpus checks the file make writes, so a corpus that silently
// shrank (an empty wildcard, a moved directory) is a red and not a fast night.
func nightlyFuzzCorpus(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the corpus file is unreadable: %v", err)
	}
	var paths []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := os.Stat(line); err != nil {
			t.Fatalf("the corpus names %q, which does not exist: %v", line, err)
		}
		paths = append(paths, line)
	}
	if len(paths) == 0 {
		t.Fatal("the corpus file is empty: a nightly that fuzzes nothing is not a pass")
	}
	return paths
}

// nightlyCampaign returns the corpus file the nightly campaign reads (make
// schema-squirrel-nightly-fuzz sets SCHEMA_SQUIRREL_FUZZ_CORPUS); unset, it
// fails the test naming the variable. The test is a nightly-tier test.
func nightlyCampaign(t *testing.T) string {
	t.Helper()
	corpus := os.Getenv("SCHEMA_SQUIRREL_FUZZ_CORPUS")
	if corpus == "" {
		t.Fatal("SCHEMA_SQUIRREL_FUZZ_CORPUS is unset: the nightly corpus file is missing (make schema-squirrel-nightly-fuzz builds and names it)")
	}
	return corpus
}

// nightlyCorpusFromMake writes the corpus file the nightly target reads, with
// the target's own rule (make/schema-squirrel-nightly-fuzz.mk), for a run that
// requires the corpus (SCHEMA_REQUIRE_CORPUS) and was not handed one by
// SCHEMA_SQUIRREL_FUZZ_CORPUS: the schema set stays the Makefile's, and a rule
// that cannot write it is a red naming make's own words.
func nightlyCorpusFromMake(t *testing.T) string {
	t.Helper()
	testinputs.Make(t)
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	const file = "build/squirrel-nightly-fuzz/corpus.txt"
	cmd := testproc.Command(t, "make", "--no-print-directory", file)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SCHEMA_SQUIRREL_FUZZ_CORPUS is unset and `make %s` could not write the corpus: %v\n%s", file, err, out)
	}
	return filepath.Join(root, file)
}

// TestSquirrelNightlyFuzz is the target's test: the corpus the Makefile
// selected, then the differential over every seed's full mutant count. A
// campaign that cannot finish in the budget is killed by go test's -timeout,
// which names the test, rather than being silently truncated.
func TestSquirrelNightlyFuzz(t *testing.T) {
	t.Parallel()

	corpus := nightlyCampaign(t)
	if corpus == "" {
		corpus = nightlyCorpusFromMake(t)
	}
	paths := nightlyFuzzCorpus(t, corpus)

	n, err := runWireFuzzDifferential(t, nil)
	if err != nil {
		t.Fatalf("after %d agreeing mutants: %v", n, err)
	}
	if n == 0 {
		t.Fatal("the campaign compared no mutants")
	}
	t.Logf("nightly fuzz: %d mutants over %d corpus schemas, 0 divergences (seed %#x, %d per instance)",
		n, len(paths), fuzzSeed, fuzzMutantsPerInstance)
}
