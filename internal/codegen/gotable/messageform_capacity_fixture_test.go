package gotable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGoMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of
// tables/backend, client_build 40, written by the Go engine and pinned to it
// by test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated Go's LoginRequestLoadMessages. Storage for one body
// whose client_build is the sentinel 17 owes "A refused batch returns a count
// of `0`" and "refuses before it decodes anything" (docs/SPEC-TABLES.md §3.3):
// count 0, refused, the sentinel untouched. Storage for two owes 2 and 40.
// A caller's capacity is a resource limit (§6.5), so this row runs here, in
// the leg's own tests, never in the shared suites.
func TestGoMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "backend", "Backend.schema"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedTables(t, string(schema), strings.ReplaceAll(goCapacityFixtureProbe, "FIXTURE", fixture))
}

const goCapacityFixtureProbe = `package backenddemo

import (
	"os"
	"testing"
)

func TestCapacityFixture(t *testing.T) {
	wire, err := os.ReadFile("FIXTURE")
	if err != nil {
		t.Fatal(err)
	}
	vocab := new(TableVocabulary)
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	var announced TableReport
	if !AnnounceRead(vocab, ann, &announced) {
		t.Fatalf("announce: %+v", announced)
	}
	one := make([]LoginRequest, 1)
	one[0].ClientBuild = 17
	var report TableReport
	count, ok := LoginRequestLoadMessages(one, vocab, wire, &report)
	if ok || count != 0 || report.Verdict != TableOpenRefused {
		t.Errorf("two bodies into storage for one: count=%d ok=%v %+v, want a refusal with count 0", count, ok, report)
	}
	if one[0].ClientBuild != 17 {
		t.Errorf("two bodies into storage for one: client_build reads %d, not the sentinel 17", one[0].ClientBuild)
	}
	two := make([]LoginRequest, 2)
	var loaded TableReport
	count, ok = LoginRequestLoadMessages(two, vocab, wire, &loaded)
	if !ok || count != 2 || two[0].ClientBuild != 40 || two[1].ClientBuild != 40 {
		t.Errorf("two bodies into storage for two: count=%d ok=%v client_build %d %d %+v", count, ok, two[0].ClientBuild, two[1].ClientBuild, loaded)
	}
}
`
