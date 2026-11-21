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

// The same rule on the committed corpus (F-091): blob_equal_bytes is an
// accepted wire row of blobdemo.Catalog, and a leg that refuses it, here with
// blob_shared's own reason, fails by that reason; blob_shared, its refused twin
// on the same root, passes with the same answer; an EMPTY .absent on either is
// the missing codec.
func TestReasonedAbsentOnACommittedAcceptedRowFails(t *testing.T) {
	chdirRepoRoot(t)
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]Instance{}
	for _, inst := range m.Instances {
		if inst.Name == "blob_equal_bytes" || inst.Name == "blob_shared" {
			rows[inst.Name] = inst
		}
	}
	accepted, refused := rows["blob_equal_bytes"], rows["blob_shared"]
	if accepted.Name == "" || refused.Name == "" || accepted.WireRefused || !refused.WireRefused || accepted.Root != refused.Root {
		t.Fatalf("the manifest no longer carries the accepted/refused pair on one root: %+v %+v", accepted, refused)
	}
	score := func(inst Instance, answer []byte) *result {
		want, err := os.ReadFile(inst.Wire)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, inst.Name+".absent"), answer, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("ruby", "wire", dir, expectation{name: inst.Name, want: want}, inst, r)
		return r
	}
	if r := score(accepted, []byte("blob_shared")); len(r.failures) != 1 || r.missing != 0 || r.pass != 0 {
		t.Fatalf("a reasoned .absent on the accepted %s scored %+v, want one failure", accepted.Name, r)
	}
	if r := score(refused, []byte("blob_shared")); r.pass != 1 || len(r.failures) != 0 {
		t.Fatalf("the owed refusal on %s scored %+v, want PASS", refused.Name, r)
	}
	for _, inst := range []Instance{accepted, refused} {
		if r := score(inst, nil); r.missing != 1 || r.pass != 0 || len(r.failures) != 0 {
			t.Fatalf("an empty .absent on %s scored %+v, want one missing case", inst.Name, r)
		}
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
