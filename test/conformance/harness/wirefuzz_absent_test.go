package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// W-ROSTER (docs/SPEC-CONFORMANCE.md §2, docs/SPEC-TABLES.md §4.2): a seed
// the leg has no codec for is a failure by name, never a pass. On main at
// 2d352b7dd8 the per-PR gate printed "30 seeds absent" for C and exited 0.

func TestWireFuzzAbsentRootsRefusedWithoutLedger(t *testing.T) {
	absent := map[string]int{"shapes.FixedBounds": 18, "tblsurf.Chain": 2}
	err := checkAbsentRoots(absent, nil, "")
	if err == nil {
		t.Fatal("20 absent seeds passed with no ledger")
	}
	for _, want := range []string{"20 seeds absent over 2 roots", "shapes.FixedBounds (18 seeds)", "tblsurf.Chain (2 seeds)", "W-ROSTER"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	if err := checkAbsentRoots(map[string]int{}, nil, ""); err != nil {
		t.Fatalf("a full roster refused: %v", err)
	}
}

// The ledger is matched as a SET: one root coming off while another goes
// absent keeps the count and is still red, naming both.
func TestWireFuzzAbsentRootsExactSet(t *testing.T) {
	ledger := map[string]string{"shapes.Bounds": "owed", "tblsurf.Chain@message": "owed"}
	if err := checkAbsentRoots(map[string]int{"shapes.Bounds": 4, "tblsurf.Chain@message": 2}, ledger, "c-plain.tsv"); err != nil {
		t.Fatalf("the exact ledgered set refused: %v", err)
	}
	err := checkAbsentRoots(map[string]int{"shapes.Bounds": 4, "tblsurf.Holder": 2}, ledger, "c-plain.tsv")
	if err == nil {
		t.Fatal("a swapped absent root passed on an equal count")
	}
	for _, want := range []string{"tblsurf.Holder (2 seeds)", "not in c-plain.tsv", "now answers", "tblsurf.Chain@message"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	err = checkAbsentRoots(map[string]int{}, ledger, "c-plain.tsv")
	if err == nil || !strings.Contains(err.Error(), "remove them") {
		t.Fatalf("stale ledger lines passed: %v", err)
	}
}

func TestWireFuzzAbsentLedgerParse(t *testing.T) {
	text := "# comment\n\nshapes.Bounds\towed by a card\ntblsurf.Chain@message\tanother\n"
	got, err := parseAbsentLedger(strings.NewReader(text), "L")
	if err != nil || len(got) != 2 || got["shapes.Bounds"] != "owed by a card" || got["tblsurf.Chain@message"] != "another" {
		t.Fatalf("got %v, %v", got, err)
	}
	for name, bad := range map[string]string{
		"no reason":    "shapes.Bounds\n",
		"empty reason": "shapes.Bounds\t \n",
		"space":        "shapes.Bounds why\n",
		"not a root":   "Bounds\twhy\n",
		"twice":        "shapes.Bounds\ta\nshapes.Bounds\tb\n",
		"no lines":     "# only a comment\n",
	} {
		if _, err := parseAbsentLedger(strings.NewReader(bad), "L"); err == nil {
			t.Errorf("%s: ledger accepted", name)
		}
	}
	if _, err := readAbsentLedger("wire-absent-no-such-ledger.tsv"); err == nil {
		t.Error("a missing ledger accepted")
	}
}

// Every checked-in ledger parses, and is named <leg>-<arm>.tsv.
func TestWireFuzzAbsentLedgersCheckedIn(t *testing.T) {
	files, err := filepath.Glob("../wire-absent/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("test/conformance/wire-absent holds no ledger: every leg answers every root, so retire this test with the directory")
	}
	for _, path := range files {
		if !strings.HasSuffix(path, ".tsv") || !strings.Contains(filepath.Base(path), "-") {
			t.Errorf("%s: a ledger is named <leg>-<arm>.tsv", path)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseAbsentLedger(strings.NewReader(string(data)), path); err != nil {
			t.Error(err)
		}
	}
}
