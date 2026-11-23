package main

import (
	"os"
	"path/filepath"
	"testing"
)

// R13 requires the explicit refusal on a must-refuse row. Missing codecs and
// driver failures are reported separately, regardless of the refusal owed.
func TestRefusalRowsDistinguishAnswerAbsenceAndDriverError(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, surface string
		inst          Instance
		want          []byte
	}{
		{"message refusal", "message", Instance{}, nil},
		{"wire refusal", "wire", Instance{WireRefused: true}, []byte("old bytes")},
	} {
		for _, answer := range []struct {
			name, file, body, outcome string
			pass, missing, failures   int
		}{
			{"explicit refusal", "case", "refused\n", outcomePass, 1, 0, 0},
			{"empty marker", "case.absent", "", outcomeAbsent, 0, 1, 0},
			{"whitespace marker", "case.absent", " \n", outcomeAbsent, 0, 1, 0},
			{"reasoned marker", "case.absent", "a local diagnostic", outcomeAbsent, 0, 1, 0},
			{"no output", "", "", outcomeFail, 0, 0, 1},
			{"value output", "case", "old bytes", outcomeFail, 0, 0, 1},
		} {
			t.Run(row.name+"/"+answer.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				if answer.file != "" {
					if err := os.WriteFile(filepath.Join(dir, answer.file), []byte(answer.body), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				r := &result{total: 1}
				scoreAnswer("go", row.surface, dir, expectation{name: "case", want: row.want}, row.inst, r)
				if r.pass != answer.pass || r.missing != answer.missing || len(r.failures) != answer.failures || r.outcome["case"] != answer.outcome {
					t.Fatalf("scored %+v, want %d pass, %d missing and %d failure", r, answer.pass, answer.missing, answer.failures)
				}
			})
		}
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
