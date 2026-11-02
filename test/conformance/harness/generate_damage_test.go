package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	if !reflect.DeepEqual(got, pinned) {
		t.Fatalf("generated report rows changed: got %+v, pinned %+v", got, pinned)
	}
	if !got["parts_index_out_of_range"].Malformed {
		t.Fatal("the malformed pointer row disappeared")
	}
	if !got["message_as_file"].Refused {
		t.Fatal("the named form refusal disappeared")
	}
	if got["parts_elem_kind"].Malformed || got["parts_elem_kind"].KindMismatch != 1 {
		t.Fatal("the recoverable kind mismatch became damage")
	}
}

// A decoder diagnostic is not permission to swallow setup errors or the
// storage-cap refusal. Neither produces the ordinary damage report above.
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
			if err == nil {
				t.Fatal("fatal report-generation error was swallowed")
			}
			if tc.count {
				if _, ok := errors.AsType[*tablewire.CountRefusal](err); !ok {
					t.Fatalf("wanted storage-cap refusal, got %v", err)
				}
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("failed generation published a report file: %v", err)
			}
		})
	}
}
