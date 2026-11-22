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
// of `0`" (docs/SPEC-TABLES.md §3.3) and, by R4 (§6.5), every value handed in
// at its declared default: count 0, refused, client_build the default and not
// the sentinel. Storage for two owes 2 and 40.
//
// R4 holds on EVERY refusal path and over EVERY value handed in, not only
// 0..count: storage for THREE values, all at the sentinel, is handed (a) the
// fixture with its form byte changed (a framing refusal before any body) and
// (b) the fixture with its last byte dropped (a body or close refusal). Each
// owes count 0, refused, and all three values at their declared default; slot
// 2 is past the batch's count of 2, so a 0..count reset would leave it at 17.
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
	runGeneratedLineageProbe(t, string(schema), nil, strings.ReplaceAll(goCapacityFixtureProbe, "FIXTURE", fixture))
}

const goCapacityFixtureProbe = `package probe

import (
	"os"
	"reflect"
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
	var want LoginRequest
	LoginRequestReset(&want)
	if want.ClientBuild == 17 {
		t.Fatal("the sentinel 17 is client_build's declared default; the probe cannot see a reset")
	}
	one := make([]LoginRequest, 1)
	one[0].ClientBuild = 17
	var report TableReport
	count, ok := LoginRequestLoadMessages(one, vocab, wire, &report)
	if ok || count != 0 || report.Verdict != TableOpenRefused {
		t.Errorf("two bodies into storage for one: count=%d ok=%v %+v, want a refusal with count 0", count, ok, report)
	}
	if !reflect.DeepEqual(one[0], want) {
		t.Errorf("two bodies into storage for one: value %+v after batch_too_large, want its declared default %+v", one[0], want)
	}

	refused := func(name string, data []byte) {
		t.Helper()
		three := make([]LoginRequest, 3)
		for i := range three {
			three[i].ClientBuild = 17
		}
		var r TableReport
		count, ok := LoginRequestLoadMessages(three, vocab, data, &r)
		if ok || count != 0 || r.Err() == nil {
			t.Errorf("%s: count=%d ok=%v %+v, want a refusal (refused or malformed) with count 0", name, count, ok, r)
		}
		for i := range three {
			if !reflect.DeepEqual(three[i], want) {
				t.Errorf("%s: slot %d reads %+v after the refusal, want its declared default %+v", name, i, three[i], want)
			}
		}
	}
	framing := append([]byte(nil), wire...)
	framing[0] = 1
	refused("form byte changed (framing refusal)", framing)
	refused("last byte dropped (body or close refusal)", wire[:len(wire)-1])

	two := make([]LoginRequest, 2)
	var loaded TableReport
	count, ok = LoginRequestLoadMessages(two, vocab, wire, &loaded)
	if !ok || count != 2 || two[0].ClientBuild != 40 || two[1].ClientBuild != 40 {
		t.Errorf("two bodies into storage for two: count=%d ok=%v client_build %d %d %+v", count, ok, two[0].ClientBuild, two[1].ClientBuild, loaded)
	}
}
`
