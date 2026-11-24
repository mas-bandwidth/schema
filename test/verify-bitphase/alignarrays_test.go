package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

// TestAlignArraysRowsHoldTheAuditRows pins the four rows the 2026-11-24
// coverage audit found red on Elixir (F1) to the wire the SPEC §4.3 model
// gives, and the bytes the broken writer produced to the refuse rows: a
// change to the builder that moved either would quietly stop testing them.
func TestAlignArraysRowsHoldTheAuditRows(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"aa_emptyzero_valid_a-empty.bin":    "016100",
		"aa_phase63_valid_ab-e-acute.bin":   "0a616202c3a9",
		"aa_phase31_valid_nested.bin":       "2a6162000b6301",
		"aa_phase33_valid_two.bin":          "feffffffffffffff0b6162010000000000008000",
		"aa_emptyzero_refuse_elixir-f1.bin": "016101",
		"aa_phase63_refuse_elixir-f1.bin":   "0a61620ac3a9",
	}
	got := map[string]string{}
	for _, r := range alignArraysRows() {
		got[r.File] = hex.EncodeToString(r.data)
	}
	for file, w := range want {
		if got[file] != w {
			t.Errorf("%s: %s, want %s", file, got[file], w)
		}
	}
}

// TestAlignArraysCorpusIsItsRoster writes the family's corpus and loads it
// under its own roster: every row is there once, no two carry the same bytes
// (a padding bit planted past an align's width would leave a refuse row equal
// to its valid row), and every refuse row's type has a valid row beside it.
func TestAlignArraysCorpusIsItsRoster(t *testing.T) {
	saved := activeRoster
	activeRoster = alignArraysRoster
	t.Cleanup(func() { activeRoster = saved })
	dir := t.TempDir()
	if err := writeAlignArraysCorpus(dir); err != nil {
		t.Fatal(err)
	}
	cases, blobs, err := loadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != len(alignArraysRows()) || len(blobs) != len(cases) {
		t.Fatalf("%d cases, %d blobs, want %d", len(cases), len(blobs), len(alignArraysRows()))
	}
	types := map[string]bool{}
	valid := map[string]bool{}
	for _, c := range cases {
		types[c.Type] = true
		if c.Kind == "valid" {
			valid[c.Type] = true
		}
		if !strings.HasPrefix(c.File, "aa_") {
			t.Errorf("%s: a family file is named aa_<type>_<kind>_<name>.bin", c.File)
		}
	}
	for _, typ := range []string{"EmptyZero", "Phase0", "Phase1", "Phase7", "Phase31", "Phase32", "Phase33", "Phase56", "Phase63"} {
		if !valid[typ] {
			t.Errorf("%s has no valid row: every type the drivers dispatch on is written by every leg", typ)
		}
	}
	for typ := range types {
		if !valid[typ] {
			t.Errorf("%s has refuse rows and no valid row", typ)
		}
	}
}

// TestUnknownFamilyIsRefused: a misspelt family runs nothing and says so.
func TestUnknownFamilyIsRefused(t *testing.T) {
	t.Parallel()
	err := runFamily(t.TempDir(), "schema", "schema-moo", t.TempDir(), "align-array", "go")
	if err == nil || !strings.Contains(err.Error(), "is not a case family") {
		t.Fatalf("want a refusal naming the family, got %v", err)
	}
}
