package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A reason-bearing `.absent` on a row that owes an answer is a failed verdict
// (F-091). An empty marker declares a missing codec. On a wire-refused row,
// only the explicit refusal answer earns credit (R13).
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
	// The control: the canonical refusal on a must-refuse row passes.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "case"), []byte("refused\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &result{total: 1}
	scoreAnswer("ruby", "wire", dir, expectation{name: "case", want: []byte("bytes")}, Instance{WireRefused: true}, r)
	if r.pass != 1 || r.outcome["case"] != outcomePass {
		t.Fatalf("the owed refusal scored %+v, want PASS", r)
	}
}

// The same rule on the committed corpus (F-091): blob_equal_bytes is an
// accepted wire row of blobdemo.Catalog. A legacy reason-bearing marker
// fails on that row. Its refusal twin, blob_shared, requires the canonical
// answer. An empty marker on either row declares a missing codec.
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
	want, err := os.ReadFile(accepted.Wire)
	if err != nil {
		t.Fatal(err)
	}
	score := func(inst Instance, suffix string, answer []byte) *result {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, inst.Name+suffix), answer, 0o644); err != nil {
			t.Fatal(err)
		}
		r := &result{total: 1}
		scoreAnswer("ruby", "wire", dir, expectation{name: inst.Name, want: want}, inst, r)
		return r
	}
	if r := score(accepted, ".absent", []byte("blob_shared")); len(r.failures) != 1 || r.missing != 0 || r.pass != 0 {
		t.Fatalf("a reasoned .absent on the accepted %s scored %+v, want one failure", accepted.Name, r)
	}
	if r := score(refused, "", []byte("refused\n")); r.pass != 1 || r.missing != 0 || len(r.failures) != 0 || r.outcome[refused.Name] != outcomePass {
		t.Fatalf("the explicit refusal on %s scored %+v, want PASS", refused.Name, r)
	}
	if r := score(refused, ".absent", []byte("blob_shared")); r.missing != 1 || r.pass != 0 || len(r.failures) != 0 || r.outcome[refused.Name] != outcomeAbsent {
		t.Fatalf("a reasoned .absent on %s scored %+v, want one missing case", refused.Name, r)
	}
	for _, inst := range []Instance{accepted, refused} {
		if r := score(inst, ".absent", nil); r.missing != 1 || r.pass != 0 || len(r.failures) != 0 {
			t.Fatalf("an empty .absent on %s scored %+v, want one missing case", inst.Name, r)
		}
	}
}
