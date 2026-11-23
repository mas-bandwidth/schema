package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// THE FIXED-FORM HEADER AND COUNT VECTORS (docs/SPEC-TABLES.md §3.4, the load
// steps and the count rows of the fixed record): each is the lawful form-3
// instance fixedevo_v3_full with ONE defect patched in, carried as a `report`
// row on tblfixedevo.EvoRoot so every leg reads it through its Form-3 fixed
// reader alongside fixedevo_v1_as_v3.
func TestFixedHeaderReportVectorsArePinned(t *testing.T) {
	_, selfFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(selfFile), "../../../testdata/conformance/tables/wire")
	full, err := os.ReadFile(filepath.Join(dir, "fixedevo_v3_full.bin"))
	if err != nil {
		t.Fatal(err)
	}
	find := func(needle []byte) int {
		if bytes.Count(full, needle) != 1 {
			t.Fatalf("fixed header vectors: the needle is not unique in fixedevo_v3_full")
		}
		return bytes.Index(full, needle)
	}
	arr := find([]byte{6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x80})
	txt := find([]byte{0x20, 0, 0, 0, 0xf0, 0x9f, 0x9a, 0x80})
	set := func(off int, v byte) func(b []byte) []byte {
		return func(b []byte) []byte { b[off] = v; return b }
	}
	neg := func(off int) func(b []byte) []byte {
		return func(b []byte) []byte { copy(b[off:], []byte{0xff, 0xff, 0xff, 0xff}); return b }
	}
	cases := []struct {
		name  string
		patch func(b []byte) []byte
	}{
		{"sp_fixed_reserved_byte_1", set(1, 1)},
		{"sp_fixed_reserved_byte_4", set(4, 0x80)},
		{"sp_fixed_reserved_byte_7", set(7, 1)},
		{"sp_fixed_nineteen_bytes", func(b []byte) []byte { return b[:19] }},
		{"sp_fixed_form_one", set(0, 1)},
		{"sp_fixed_form_two", set(0, 2)},
		{"sp_fixed_form_four", set(0, 4)},
		{"sp_fixed_count_negative", neg(arr)},
		{"sp_fixed_count_over_bound", set(arr, 7)},
		{"sp_fixed_dead_slot_nonzero", set(arr, 5)},
		{"sp_fixed_text_length_negative", neg(txt)},
		{"sp_fixed_text_length_over_bound", set(txt, 33)},
	}
	for _, tc := range cases {
		want := tc.patch(append([]byte(nil), full...))
		got, err := os.ReadFile(filepath.Join(dir, tc.name+".bin"))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: committed bytes drifted from fixedevo_v3_full patch", tc.name)
		}
	}
}
