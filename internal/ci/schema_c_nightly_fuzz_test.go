package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaCNightlyFuzzPlantedPattern is the card's planted-pattern gate.
//
// The C nightly fuzz negative control removes one bounds check from a scratch
// copy of a generated reader. This test holds the two ends of that control:
// the unplanted guard is recognised, and the exact plant the make target
// installs is recognised as a missing guard. When the plant is in the sample
// the test is RED; when it is out the test is GREEN. It reads no generated
// tree, so it runs on every push with the fast tier.
func TestSchemaCNightlyFuzzPlantedPattern(t *testing.T) {
	t.Parallel()

	const guarded = `static SCHEMA_UNUSED SCHEMA_TABLEDEMO_TABLE_INLINE int table_reader_has( const TableReader * r, int64_t n )
{ return n >= 0 && r->offset >= 0 && r->offset <= r->size && n <= r->size - r->offset; }`
	const planted = `static SCHEMA_UNUSED SCHEMA_TABLEDEMO_TABLE_INLINE int table_reader_has( const TableReader * r, int64_t n )
{ (void) r; (void) n; return 1; }`

	if !hasReaderGuard(guarded) {
		t.Fatal("the planted-pattern detector does not recognise the generated bounds check")
	}
	if hasReaderGuard(planted) {
		t.Fatal("PLANTED: the detector accepts a reader whose bounds check returns 1 unconditionally")
	}
	const room = `{ return r->offset >= 0 && r->offset <= r->size && n <= (uint64_t) (r->size - r->offset); }`
	const roomPlanted = `{ (void) r; (void) n; return 1; }`
	if !hasRoomGuard(room) {
		t.Fatal("the planted-pattern detector does not recognise the generated room check")
	}
	if hasRoomGuard(roomPlanted) {
		t.Fatal("PLANTED: the detector accepts a reader whose room check returns 1 unconditionally")
	}

	root := repoRoot(t)
	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-c-nightly-fuzz.mk"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"schema-c-nightly-fuzz-negative-control",
		"(void) r; (void) n; return 1;",
		"return n >= 0 && r->offset >= 0 && r->offset <= r->size && n <= r->size - r->offset;",
		"return r->offset >= 0 && r->offset <= r->size && n <= (uint64_t) (r->size - r->offset);",
		"SCHEMA_C_FUZZ_SECONDS   ?= 600",
		"tool missing: $(AFL_FUZZ)",
	} {
		if !strings.Contains(string(mk), want) {
			t.Errorf("make/schema-c-nightly-fuzz.mk does not name %q", want)
		}
	}
}

// hasReaderGuard reports whether text carries the generated reader's extent
// check. It is deliberately the same one-line invariant the make target
// removes: the control and this test cannot drift apart.
func hasReaderGuard(text string) bool {
	return strings.Contains(text, "return n >= 0 && r->offset >= 0 && r->offset <= r->size && n <= r->size - r->offset;")
}

// hasRoomGuard is the second guard the control removes: the unsigned room
// check the reader's length paths use. Removing has alone left the fuzzer
// green, so the control and this test name both.
func hasRoomGuard(text string) bool {
	return strings.Contains(text, "return r->offset >= 0 && r->offset <= r->size && n <= (uint64_t) (r->size - r->offset);")
}

// TestSchemaCNightlyFuzzCorpus holds the corpus list to the repository: every
// schema it names exists, it is not empty, and the list is the one the make
// file reads. A corpus of missing files is a fuzzer that passes by not running.
func TestSchemaCNightlyFuzzCorpus(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "make", "schema-c-nightly-fuzz.corpus"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n++
		if _, err := os.Stat(filepath.Join(root, line)); err != nil {
			t.Errorf("corpus schema %s: %v", line, err)
		}
	}
	if n < 10 {
		t.Fatalf("the corpus names %d schemas; too few to cover the table shapes", n)
	}
}
