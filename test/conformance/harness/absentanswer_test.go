package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A `.absent` NAMING A REASON is the leg's refusal (README.md). On a row that
// owes an ANSWER (the committed wire, a message golden, a written verdict) it
// is the wrong verdict and fails; it may never hide in the absence count, or a
// leg that refuses a valid input reads `pass 0/0 +1a` (review F-091). An EMPTY
// `.absent` beside it stays the missing codec it is, and the same reason on a
// row that owes the refusal still passes.
func TestReasonedAbsentOnAnAnswerRowFails(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, surface string
		want          []byte
	}{
		{"wire bytes", "wire", []byte("committed wire")},
		{"message golden", "message", []byte("message golden")},
		{"json-read", "json-read", []byte("committed wire")},
		{"write-refusal wrote", "write-refusal", []byte("wrote\n")},
		{"write-refusal refused", "write-refusal", []byte("refused\n")},
		{"report refused", "report", []byte("refused\n")},
		{"json-write", "json-write", []byte("{}\n")},
	} {
		t.Run(tc.name+"/reason", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "case.absent"), []byte("blob_shared"), 0o644); err != nil {
				t.Fatal(err)
			}
			r := &result{total: 1}
			scoreAnswer("ruby", tc.surface, dir, expectation{name: "case", want: tc.want}, Instance{}, r)
			if r.pass != 0 || r.missing != 0 || len(r.failures) != 1 || r.outcome["case"] != outcomeFail {
				t.Fatalf("a refusal on a row that owes an answer scored %+v, want one failure", r)
			}
			if !strings.Contains(r.failures[0], "blob_shared") {
				t.Fatalf("the failure does not name the refusal: %q", r.failures[0])
			}
		})
		t.Run(tc.name+"/empty", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "case.absent"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			r := &result{total: 1}
			scoreAnswer("ruby", tc.surface, dir, expectation{name: "case", want: tc.want}, Instance{}, r)
			if r.pass != 0 || r.missing != 1 || len(r.failures) != 0 || r.outcome["case"] != outcomeAbsent {
				t.Fatalf("an empty .absent scored %+v, want one missing case", r)
			}
		})
	}
	// the control: the same reason on the row that owes the refusal passes
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "case.absent"), []byte("blob_shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &result{total: 1}
	scoreAnswer("ruby", "wire", dir, expectation{name: "case", want: []byte("bytes")}, Instance{WireRefused: true}, r)
	if r.pass != 1 || r.outcome["case"] != outcomePass {
		t.Fatalf("the owed refusal scored %+v, want PASS", r)
	}
}

// A row on an opt-in unit the leg does not name is not asked of it, on every
// surface, json-write's `<case>.json` expectation included: it counts absent,
// never "the driver wrote nothing".
func TestOptInRowIsNotAskedOnEverySurface(t *testing.T) {
	t.Parallel()
	notAsked := map[string]bool{"shapes_bounds_ends": true}
	for _, tc := range []struct {
		surface, name string
		want          bool
	}{
		{"wire", "shapes_bounds_ends", true},
		{"json-read", "shapes_bounds_ends", true},
		{"json-write", "shapes_bounds_ends.json", true},
		{"json-write", "root_full.json", false},
		{"wire", "shapes_bounds_ends.json", false},
		{"wire", "root_full", false},
	} {
		if got := wasNotAsked(notAsked, tc.surface, tc.name); got != tc.want {
			t.Errorf("wasNotAsked(%s, %s) = %v, want %v", tc.surface, tc.name, got, tc.want)
		}
	}
}
