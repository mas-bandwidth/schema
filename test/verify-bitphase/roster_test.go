package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// corpus plants a bitphase corpus folder: the given manifest rows, and one
// file of its own bytes for every row of the required roster.
func corpus(t *testing.T, rows []manifestCase) string {
	t.Helper()
	dir := t.TempDir()
	for i, c := range requiredRoster() {
		if err := os.WriteFile(filepath.Join(dir, c.File), fmt.Appendf(nil, "vector %d", i), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var m strings.Builder
	for _, c := range rows {
		m.WriteString(c.Kind + "\t" + c.Type + "\t" + c.File + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.tsv"), []byte(m.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestManifestIsHeldToTheRosterByIdentity is F-043. The verifier counted 66
// manifest rows and never asked which: 66 copies of the first legal EmptyZero
// row printed BITPHASE_OK js 66, while 65 rows rightly failed. The required
// rows are checked by identity and distinctness now; both of the reviewer's
// inputs are here, beside the whole corpus as the control.
func TestManifestIsHeldToTheRosterByIdentity(t *testing.T) {
	roster := requiredRoster()
	if len(roster) != 66 {
		t.Fatalf("the required roster has %d rows; the oracle writes 66 (2 EmptyZero + 8 phases x 8)", len(roster))
	}
	cases, blobs, err := loadManifest(corpus(t, roster))
	if err != nil || len(cases) != 66 || len(blobs) != 66 {
		t.Fatalf("CONTROL FAILED: the whole corpus: %d cases, %d blobs, %v", len(cases), len(blobs), err)
	}

	repeated := make([]manifestCase, 66)
	for i := range repeated {
		repeated[i] = roster[0]
	}
	swapped := append([]manifestCase{}, roster...)
	swapped[5] = swapped[4]
	unknown := append([]manifestCase{}, roster...)
	unknown[7] = manifestCase{"valid", "Phase2", "valid_p2_v0.bin"}
	wrongKind := append([]manifestCase{}, roster...)
	wrongKind[0].Kind = "refuse"
	for _, tc := range []struct {
		name string
		rows []manifestCase
		says string
	}{
		{"66 copies of the first row", repeated, "line 2 repeats valid EmptyZero valid_empty.bin"},
		{"65 rows", roster[:65], "the required row refuse Phase63 hostile_trailbyte_p63.bin is missing"},
		{"66 rows, one twice for another", swapped, "the required row " + roster[5].Kind + " " + roster[5].Type + " " + roster[5].File + " is missing"},
		{"a row the roster has not", unknown, "is not a row of the required roster"},
		{"a row under the other kind", wrongKind, "is not a row of the required roster"},
		{"67 rows", append(append([]manifestCase{}, roster...), roster[3]), "repeats"},
		{"no row", nil, "is missing"},
	} {
		if _, _, err := loadManifest(corpus(t, tc.rows)); err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: want red saying %q, got %v", tc.name, tc.says, err)
		}
	}

	// one file copied over another: 66 distinct rows, 65 distinct vectors
	dir := corpus(t, roster)
	first, err := os.ReadFile(filepath.Join(dir, roster[2].File))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, roster[3].File), first, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadManifest(dir); err == nil || !strings.Contains(err.Error(), "carries the same bytes as") {
		t.Errorf("a vector copied over another: %v", err)
	}

	// a malformed line used to be skipped
	bad := corpus(t, roster)
	raw, err := os.ReadFile(filepath.Join(bad, "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "manifest.tsv"), append(raw, []byte("valid Phase0\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadManifest(bad); err == nil || !strings.Contains(err.Error(), "is not kind, type, file") {
		t.Errorf("a malformed line: %v", err)
	}
}
