package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A Python release row must reject a roster with absent corpus entries even
// when the harness reports zero divergences for the entries it did run.
func TestPythonWireFuzzReleaseRoster(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "conformance-harness"), []byte("#!/bin/sh\nprintf '%s\\n' \"$FUZZ_SUMMARY\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	owed := filepath.Join(dir, "owed.txt")
	if err := os.WriteFile(owed, nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ summary, want string }{
		{"wire-fuzz: seed 1, 1 seeds over 1 roots, 1 enumerated + 1 random = 2 mutants, 0 divergences", "pass (2 mutants"},
		{"wire-fuzz: seed 1, 1 seeds over 1 roots, 1 seeds absent, 1 enumerated + 1 random = 2 mutants, 0 divergences", "RED: incomplete Python wire-fuzz roster"},
		{"wire-fuzz: 1 seeds over 1 roots, 0 mutants, 0 divergences", "RED: missing or empty Python wire-fuzz mutation count"},
	} {
		cmd := exec.Command("sh", filepath.Join(root, "make/checks/wire-fuzz-release-run.sh"), "python", "plain", "1", "1", "out")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "WFR_DRIVER=test", "WFR_OWED="+owed, "FUZZ_SUMMARY="+tc.summary)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("release row: %v: %s", err, out)
		}
		row, err := os.ReadFile(filepath.Join(dir, "out/python.plain.1.row"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(row), tc.want) {
			t.Fatalf("got %q, want %q", row, tc.want)
		}
	}
}

// TestWireFuzzReleaseRowHoldsTheRunToItsBudget: a release row passes only on
// the harness's own count line with the seed and the budget that were asked
// for. The short, zero and absent cases are the reviewer's: a process that
// reported 1 mutant, 0 mutants or no count certified a Go row at N=100000.
func TestWireFuzzReleaseRowHoldsTheRunToItsBudget(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "conformance-harness"), []byte("#!/bin/sh\nprintf '%s\\n' \"$FUZZ_SUMMARY\"\nexit \"${FUZZ_EXIT:-0}\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	owed := filepath.Join(dir, "owed.txt")
	if err := os.WriteFile(owed, nil, 0644); err != nil {
		t.Fatal(err)
	}
	const tail = ", 0 divergences, 0.1 s, 100 mutants/s"
	for _, tc := range []struct {
		name, leg, arm, summary, exit, want string
	}{
		{"the budget asked for", "go", "plain", "wire-fuzz: seed 7, 9 seeds over 9 roots, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "pass (100003 mutants"},
		{"the retain arm", "go", "retain", "wire-fuzz retain: seed 7, 9 seeds over 9 roots, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "pass (100003 mutants"},
		{"a mixed-builder leg", "dart", "plain", "wire-fuzz-mixed-builder: seed 7, 9 seeds over 9 roots, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "pass (100003 mutants"},
		{"with seeds outside the arm", "go", "retain", "wire-fuzz retain: seed 7, 9 seeds over 9 roots, 2 seeds outside the arm, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "pass (100003 mutants"},

		{"one mutant", "go", "plain", "wire-fuzz: seed 7, 9 seeds over 9 roots, 0 enumerated + 1 random = 1 mutants" + tail, "0", "RED: the run reports 1 random mutants and 100000 were asked for"},
		{"one mutant, python", "python", "plain", "wire-fuzz: seed 7, 9 seeds over 9 roots, 0 enumerated + 1 random = 1 mutants" + tail, "0", "RED: the run reports 1 random mutants and 100000 were asked for"},
		{"zero mutants", "go", "plain", "wire-fuzz: seed 7, 9 seeds over 9 roots, 0 enumerated + 0 random = 0 mutants" + tail, "0", "RED: the run reports 0 random mutants and 100000 were asked for"},
		{"no count", "go", "plain", "wire-fuzz: seed 7, 9 seeds over 9 roots, 0 divergences", "0", "RED: the run reports no 'seed <S>"},
		{"no seed", "go", "plain", "wire-fuzz: 9 seeds over 9 roots, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "RED: the run reports no 'seed <S>"},
		{"another seed", "go", "plain", "wire-fuzz: seed 8, 9 seeds over 9 roots, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "RED: the run reports seed 8 and seed 7 was asked for"},
		{"a total that does not add up", "go", "plain", "wire-fuzz: seed 7, 9 seeds over 9 roots, 3 enumerated + 100000 random = 5 mutants" + tail, "0", "RED: the run reports 5 mutants for 3 enumerated + 100000 random"},
		{"the plain arm answered by retain", "go", "plain", "wire-fuzz retain: seed 7, 9 seeds over 9 roots, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "RED: the plain arm was asked for and the run names retain"},
		{"the retain arm answered by plain", "go", "retain", "wire-fuzz: seed 7, 9 seeds over 9 roots, 3 enumerated + 100000 random = 100003 mutants" + tail, "0", "RED: the retain arm was asked for and the run does not name it"},
		{"a failing process", "go", "plain", "wire-fuzz: check failed", "1", "RED: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("sh", filepath.Join(root, "make/checks/wire-fuzz-release-run.sh"), tc.leg, tc.arm, "7", "100000", "out")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "WFR_DRIVER=test", "WFR_OWED="+owed, "FUZZ_SUMMARY="+tc.summary, "FUZZ_EXIT="+tc.exit)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("release row: %v: %s", err, out)
			}
			row, err := os.ReadFile(filepath.Join(dir, "out", tc.leg+"."+tc.arm+".7.row"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(row), "\t"+tc.want) {
				t.Fatalf("got %q, want the verdict to start %q", row, tc.want)
			}
		})
	}
}
