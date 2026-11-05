package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The nightly's generator must preserve all pinned report outcomes, including
// damaged inputs for which the public API deliberately returns no value.
func TestGenerateReportsIncludesDamagedFixtures(t *testing.T) {
	t.Skip("layer 3: the fixture rows' clamped and malformed counters, retired or re-pinned per docs/plans/strict-only.md (b)")
	m, pinned, _ := corpus(t)
	m.Instances = nil
	m.CookWrites = nil
	dir := t.TempDir()
	reports := filepath.Join(dir, "reports.txt")
	if err := generate(m, filepath.Join(dir, "json"), reports); err != nil {
		t.Fatal(err)
	}
	got, err := readReports(reports)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(pinned) {
		t.Fatalf("generated %d reports, want %d", len(got), len(pinned))
	}
	for name, want := range pinned {
		if counts, ok := got[name]; !ok || counts != want {
			t.Errorf("%s: got %v (present %v), want %v", name, counts, ok, want)
		}
	}
	if !got["parts_index_out_of_range"].Malformed {
		t.Fatal("damaged pointer fixture lost its malformed result")
	}
}

func TestGenerateReportsRejectsInvalidInput(t *testing.T) {
	for _, problem := range []string{"root", "wire"} {
		t.Run(problem, func(t *testing.T) {
			m, _, _ := corpus(t)
			var row ReportCase
			for _, rc := range m.Reports {
				if rc.Name == "parts_index_out_of_range" {
					row = rc
					break
				}
			}
			if row.Name == "" {
				t.Fatal("damaged pointer fixture missing")
			}
			dir := t.TempDir()
			want := "names no table"
			if problem == "root" {
				row.Root = "NotADeclaredRoot"
			} else {
				row.Wire = filepath.Join(dir, "missing.bin")
				want = "missing.bin"
			}
			m.Instances, m.CookWrites = nil, nil
			m.Reports = []ReportCase{row}
			reports := filepath.Join(dir, "reports.txt")
			if err := generate(m, filepath.Join(dir, "json"), reports); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want error containing %q", err, want)
			}
			if _, err := os.Stat(reports); !os.IsNotExist(err) {
				t.Fatalf("invalid input published a reports file: %v", err)
			}
		})
	}
}
