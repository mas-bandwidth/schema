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
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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

// nightlyCampaign confines a nightly test to its campaign. The three nightly
// tests are minutes of interpreter time over the whole corpus, so they run only
// under make schema-squirrel-nightly-fuzz, which sets SCHEMA_SQUIRREL_FUZZ_CORPUS;
// the SCHEMA_SLOW=1 pull-request lane runs the same seeded differential at its
// own budget in fuzz_test.go. The skip names the campaign, so a reader of the
// slow lane sees what did not run and where it does. It returns the corpus file.
func nightlyCampaign(t *testing.T) string {
	t.Helper()
	corpus := os.Getenv("SCHEMA_SQUIRREL_FUZZ_CORPUS")
	if corpus == "" {
		t.Skip("the nightly campaign, not the pull request: make schema-squirrel-nightly-fuzz sets SCHEMA_SQUIRREL_FUZZ_CORPUS and runs it")
	}
	return corpus
}

// TestSquirrelNightlyFuzz is the target's test: the corpus the Makefile
// selected, then the differential over every seed's full mutant count. A
// campaign that cannot finish in the budget is killed by go test's -timeout,
// which names the test, rather than being silently truncated.
func TestSquirrelNightlyFuzz(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Squirrel interpreter and the tables conformance corpus")

	corpus := nightlyCampaign(t)
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
