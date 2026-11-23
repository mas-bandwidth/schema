package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A must-refuse row (message wire `-`, `wire-refused`) is answered by a
// refusal. An EMPTY `<case>.absent` is the driver contract's per-case
// absence: the leg has no codec for the root. It is a missing feature, and
// must never score the PASS the refusal earns, or a leg that cannot read the
// root at all passes every must-refuse row on it. A `.absent` that names a
// reason still transports the refusal.
func TestEmptyAbsentOnARefusalRowIsMissingNotPass(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, surface string
		inst          Instance
		want          []byte
	}{
		{"message refusal", "message", Instance{}, nil},
		{"wire refusal", "wire", Instance{WireRefused: true}, []byte("old bytes")},
	} {
		for _, body := range []string{"", "\n", " \n"} {
			t.Run(tc.name+"/empty", func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "case.absent"), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				r := &result{total: 1}
				scoreAnswer("go", tc.surface, dir, expectation{name: "case", want: tc.want}, tc.inst, r)
				if r.pass != 0 || r.missing != 1 || len(r.failures) != 0 || r.outcome["case"] != outcomeAbsent {
					t.Fatalf("an empty .absent on a must-refuse row scored %+v, want one missing case and no pass", r)
				}
			})
		}
		t.Run(tc.name+"/reason", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "case.absent"), []byte("a local diagnostic"), 0o644); err != nil {
				t.Fatal(err)
			}
			r := &result{total: 1}
			scoreAnswer("go", tc.surface, dir, expectation{name: "case", want: tc.want}, tc.inst, r)
			if r.pass != 1 || r.missing != 0 || len(r.failures) != 0 || r.outcome["case"] != outcomePass {
				t.Fatalf("a named refusal scored %+v, want PASS", r)
			}
		})
	}
}

// The reference leg may never be absent: an empty `.absent` from it on a
// must-refuse row is the corpus losing its own expectation.
func TestReferenceEmptyAbsentOnARefusalRowFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "case.absent"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := &result{total: 1}
	scoreAnswer("cpp", "message", dir, expectation{name: "case"}, Instance{}, r)
	if r.missing <= r.named {
		t.Fatalf("the reference's empty .absent was not counted as an absence: %+v", r)
	}
}
