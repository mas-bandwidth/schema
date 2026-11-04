package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
		if v, ok := got[name]; !ok || v.String() != want.String() {
			t.Errorf("%s: got %s (present=%t), want %s", name, v, ok, want)
		}
	}
	for _, name := range []string{"parts_index_out_of_range", "message_as_file", "parts_elem_kind", "parts_count_past_bound"} {
		if !got[name].Refused {
			t.Errorf("%s lost its refusal", name)
		}
	}
}

// A missing declaration and this build's storage cap are infrastructure
// failures. Ordinary wire refusals still produce scored rows above.
func TestGenerateReportRowsKeepFatalErrors(t *testing.T) {
	m, _, _ := corpus(t)
	cases := []struct {
		name, unit, root, wire string
		count                  bool
	}{
		{"missing_root", "streamdemo", "NotATable", "testdata/wire/tables/parts_index_out_of_range.bin", false},
		{"storage_cap", "listdemo", "Save", "testdata/wire/tables/fuzz-vectors/list_count_cap.bin", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "reports.txt")
			reports := &Manifest{Units: m.Units, Reports: []ReportCase{{Name: tc.name, Unit: tc.unit, Root: tc.root, Wire: tc.wire}}}
			err := generate(reports, filepath.Join(dir, "json"), path)
			if tc.count {
				if _, ok := errors.AsType[*tablewire.CountRefusal](err); !ok {
					t.Fatalf("storage-cap error lost its typed cause: %v", err)
				}
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
