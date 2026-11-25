package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestTheRegistryHoldsTheAlignArraysRows: the registry's align-arrays tests
// are this file's rows exactly, one test per row, with the row's schema,
// root, kind and bytes as inputs, a valid row a roundtrip and a refuse row a
// load-refusal. A row added here without a registry test, or a registry
// test whose bytes moved, is red.
func TestTheRegistryHoldsTheAlignArraysRows(t *testing.T) {
	t.Parallel()
	reg, err := testreg.Load("../../" + testreg.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]alignRow{}
	for _, r := range alignArraysRows() {
		rows[r.File] = r
	}
	seen := map[string]bool{}
	for _, tt := range reg.Tests {
		if tt.Family != AlignArraysFamily || !tt.PerLang() {
			continue
		}
		var in alignInputs
		if err := json.Unmarshal(tt.Inputs, &in); err != nil {
			t.Fatalf("%s: %v", tt.Key, err)
		}
		r, ok := rows[in.File]
		switch {
		case !ok:
			t.Errorf("%s names %s, no row of alignarrays.go", tt.Key, in.File)
			continue
		case seen[in.File]:
			t.Errorf("%s names %s, which another test names too", tt.Key, in.File)
		case in.Schema != AlignArraysSchema || in.Root != r.Type || in.Kind != r.Kind || in.Hex != hex.EncodeToString(r.data) ||
			len(in.Files) != 1 || in.Files[0] != AlignArraysSchema:
			t.Errorf("%s: inputs %+v, the row is %s %s %x", tt.Key, in, r.Type, r.Kind, r.data)
		case (r.Kind == "valid") != (tt.Type == "roundtrip") || (r.Kind == "refuse") != (tt.Type == "load-refusal"):
			t.Errorf("%s: a %s row is type %s", tt.Key, r.Kind, tt.Type)
		}
		seen[in.File] = true
	}
	for file := range rows {
		if !seen[file] {
			t.Errorf("the row %s has no registry test", file)
		}
	}
}

// TestTheAdapterNamesTheFailingRow: each leg's failure line names its row.
func TestTheAdapterNamesTheFailingRow(t *testing.T) {
	t.Parallel()
	for _, out := range []string{
		"panic: FAILED: bytes aa_phase63_valid_ab-e-acute.bin (got 5 want 6)",
		"lua: main.lua:30: FAILED: read aa_phase63_valid_ab-e-acute.bin",
		"FAILED: accepted aa_phase63_valid_ab-e-acute.bin",
	} {
		m := failedRow.FindStringSubmatch(out)
		if m == nil || m[1] != "aa_phase63_valid_ab-e-acute.bin" {
			t.Errorf("%q: %v", out, m)
		}
	}
}

// TestEachRowsVerdictIsItsOwnRun: a red batch says nothing about the rows a
// driver did not reach, or reached in another order, so each row's verdict
// comes from a run of that row alone. The fake driver here runs rows last to
// first and stops at its first failure, so the failure it names is the last
// bad row; a verdict read off where the batch stopped would pass the first
// bad row.
func TestEachRowsVerdictIsItsOwnRun(t *testing.T) {
	t.Parallel()
	rows := alignArraysRows()[:4]
	bad := map[string]bool{rows[0].File: true, rows[2].File: true}
	var runs []string
	reversed := func(dir string, rs []alignRow) error {
		runs = append(runs, dir)
		for _, r := range slices.Backward(rs) {
			if bad[r.File] {
				return fmt.Errorf("FAILED: bytes %s (got 1 want 2)", r.File)
			}
		}
		return nil
	}
	got, err := rowVerdicts(rows, reversed)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{testreg.Fail, testreg.Pass, testreg.Fail, testreg.Pass}
	if len(got) != len(rows) {
		t.Fatalf("%d verdicts for %d rows", len(got), len(rows))
	}
	for i, v := range got {
		if v.row.File != rows[i].File || v.verdict != want[i] {
			t.Errorf("row %d %s: %s, want %s", i, v.row.File, v.verdict, want[i])
		}
	}
	if strings.Join(runs, " ") != "all row-0 row-1 row-2 row-3" {
		t.Errorf("runs %v, want the batch then each row alone", runs)
	}
	// a green batch is every row's pass, in one run
	runs = nil
	if got, err := rowVerdicts(rows[1:2], reversed); err != nil || len(got) != 1 || got[0].verdict != testreg.Pass || len(runs) != 1 {
		t.Errorf("a green batch: %v %+v, runs %v", err, got, runs)
	}
	// a row alone that fails without naming itself is a crash
	if _, err := rowVerdicts(rows[:1], func(string, []alignRow) error { return fmt.Errorf("signal: killed") }); err == nil {
		t.Error("a run that names no row was taken as a verdict")
	}
}

// TestThePlantTurnsAValidRowAround: the negative control's handle runs one
// requested valid row as a refuse row, and names nothing else.
func TestThePlantTurnsAValidRowAround(t *testing.T) {
	t.Parallel()
	rows := alignArraysRows()[:3]
	got, err := plantRows(rows, rows[1].File)
	if err != nil || got[1].Kind != "refuse" || got[0].Kind != rows[0].Kind || rows[1].Kind != "valid" {
		t.Fatalf("%v: %+v", err, got)
	}
	if _, err := plantRows(rows, "aa_no_such.bin"); err == nil {
		t.Fatal("a plant naming no requested row was accepted")
	}
}

// TestARequestThatDisagreesWithItsRowIsRefused: a registry test whose bytes
// are not the row's is refused before anything runs.
func TestARequestThatDisagreesWithItsRowIsRefused(t *testing.T) {
	t.Parallel()
	r := alignArraysRows()[0]
	in := alignInputs{Schema: AlignArraysSchema, Root: r.Type, Kind: r.Kind, File: r.File, Hex: hex.EncodeToString(r.data), Files: []string{AlignArraysSchema}}
	ok, _ := json.Marshal(in)
	in.Hex = "00" + in.Hex
	bad, _ := json.Marshal(in)
	req := func(inputs []byte) []testreg.Request {
		return []testreg.Request{{Instance: "go.functional.x.align-arrays.y", Lang: "go", Family: AlignArraysFamily, Inputs: inputs}}
	}
	if _, _, err := rowsForRequests("go", req(ok)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rowsForRequests("go", req(bad)); err == nil || !strings.Contains(err.Error(), "do not agree") {
		t.Fatalf("a request with other bytes: %v", err)
	}
}
