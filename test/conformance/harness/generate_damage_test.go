package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A report row describes the decoder's verdict even when the wire cannot
// produce a value. Exercise generate itself so update-goldens cannot regress
// to treating the pinned malformed-pointer verdict as a tool failure.
func TestGenerateReportRowsRetainDamageVerdict(t *testing.T) {
	m, pinned, _ := corpus(t)
	reports := &Manifest{Units: m.Units, Reports: m.Reports}
	dir := t.TempDir()
	path := filepath.Join(dir, "reports.txt")
	if err := generate(reports, filepath.Join(dir, "json"), path); err != nil {
		t.Fatal(err)
	}
	got, err := readReports(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(pinned) {
		t.Fatalf("report row count: got %d want %d", len(got), len(pinned))
	}
	for name, want := range pinned {
		if v, ok := got[name]; !ok || v != want {
			t.Errorf("%s: got %s (present=%t), want %s", name, v, ok, want)
		}
	}
	for _, name := range []string{"parts_index_out_of_range", "message_as_file", "parts_elem_kind"} {
		if got[name] != "refused" {
			t.Errorf("%s lost its refusal", name)
		}
	}
}

// A missing declaration is infrastructure failure; an invalid count is a read
// refusal and must still produce a scored report row.
func TestGenerateReportRowsKeepFatalErrors(t *testing.T) {
	m, _, _ := corpus(t)
	cases := []struct {
		name, unit, root, wire string
		count                  bool
	}{
		{"missing_root", "streamdemo", "NotATable", "testdata/wire/tables/parts_index_out_of_range.bin", false},
		{"count_refusal", "listdemo", "Save", "testdata/wire/tables/fuzz-vectors/list_count_cap.bin", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "reports.txt")
			reports := &Manifest{Units: m.Units, Reports: []ReportCase{{Name: tc.name, Unit: tc.unit, Root: tc.root, Wire: tc.wire}}}
			err := generate(reports, filepath.Join(dir, "json"), path)
			if tc.count {
				if err != nil {
					t.Fatal(err)
				}
				got, err := readReports(path)
				if err != nil {
					t.Fatal(err)
				}
				if got[tc.name] != "refused" {
					t.Fatal("oversized count was not refused")
				}
				return
			}
			if err == nil {
				t.Fatal("setup error was swallowed")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed generation published a report file: %v", err)
			}
		})
	}
}
