package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A successful refusal is a passing observation, not a missing answer. The
// nightly artifact must agree with the verdict printed by the same run.
func TestRefusalCoverageMatchesScoredVerdicts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, surface, file, body, outcome string
		damaged                            bool
		answered, named, failed            int
	}{
		{"damage refused", "framing", "case", "0,0,0,0,0,true,read\n", outcomePass, true, 1, 0, 0},
		{"damage accepted", "framing", "case", "0,0,0,0,0,false,read\n", outcomeFail, true, 0, 0, 1},
		{"damage report invalid", "report", "case", "invalid\n", outcomeFail, true, 0, 0, 1},
		{"message named refusal", "message", "case.absent", VariableMessageRefusal, outcomeNamed, false, 0, 1, 0},
		{"message wrong reason", "message", "case.absent", "wrong", outcomeFail, false, 0, 0, 1},
		{"message writes bytes", "message", "case", "unexpected", outcomeFail, false, 0, 0, 1},
		{"message no answer", "message", "", "", outcomeFail, false, 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.file != "" {
				if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			e := expectation{name: "case", damaged: tc.damaged}
			r := &result{total: 1}
			scoreAnswer("go", tc.surface, dir, e, Instance{}, r)
			if r.outcome[e.name] != tc.outcome {
				t.Errorf("recorded verdict = %q, want %q", r.outcome[e.name], tc.outcome)
			}
			if r.pass != tc.answered || r.named != tc.named || len(r.failures) != tc.failed {
				t.Fatalf("scored result = %+v", r)
			}
			rows := coverageRows([]string{"go"}, map[string]map[string]*result{"go": {tc.surface: r}},
				map[string][]expectation{tc.surface: {e}}, &Manifest{}, nil)
			if len(rows) != 1 {
				t.Fatalf("coverage = %+v", rows)
			}
			got := rows[0]
			if got.Cases != 1 || got.Answered != tc.answered || got.Named != tc.named || got.Failed != tc.failed || got.NotCarried != 0 || got.AbsentClaimed != 0 {
				t.Fatalf("coverage disagrees with score: %+v", got)
			}
		})
	}
}
