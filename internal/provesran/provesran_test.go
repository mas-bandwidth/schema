package provesran

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestGlobRefusesToMatchNothing is the negative control of the zero-match
// rule: the same pattern is green over a directory that holds a match and red
// over one that does not.
func TestGlobRefusesToMatchNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"b.mk", "a.mk", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Glob(filepath.Join(dir, "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(dir, "a.mk"), filepath.Join(dir, "b.mk")}; !slices.Equal(got, want) {
		t.Errorf("Glob = %v, want %v (sorted)", got, want)
	}
	if _, err := Glob(filepath.Join(dir, "*.yml")); err == nil || !strings.Contains(err.Error(), "matched no file") {
		t.Errorf("a pattern that matches nothing returned %v, want the matched-no-file refusal", err)
	}
	if _, err := Glob(filepath.Join(dir, "moved", "*.mk")); err == nil {
		t.Error("a pattern under a directory that does not exist returned no error")
	}
	if _, err := Glob("[bad"); err == nil {
		t.Error("a malformed pattern returned no error")
	}
}

// TestLedgerIsRedOnZeroAndOnUnknown plants the three accounts a scan can end
// with: judged everything (green), judged nothing (red), and met a candidate no
// rule placed (red, by name, however many others were judged).
func TestLedgerIsRedOnZeroAndOnUnknown(t *testing.T) {
	t.Parallel()
	green := NewLedger("planted scan")
	green.Judged()
	green.Judged()
	if err := green.Err(); err != nil {
		t.Errorf("a scan that judged 2 candidates and met no unknown: %v", err)
	}
	if green.Count() != 2 {
		t.Errorf("Count = %d, want 2", green.Count())
	}

	empty := NewLedger("planted scan")
	if err := empty.Err(); err == nil || !strings.Contains(err.Error(), "judged nothing") {
		t.Errorf("a scan that judged nothing returned %v, want the judged-nothing refusal", err)
	}

	unknown := NewLedger("planted scan")
	unknown.Judged()
	unknown.Unknown("z_test.go", "spawns through a wrapper no marker names")
	unknown.Unknown("a_test.go", "no rule")
	err := unknown.Err()
	if err == nil {
		t.Fatal("a scan that met two unknown candidates returned no error")
	}
	for _, want := range []string{"2 candidate(s) match no rule", "a_test.go: no rule", "z_test.go: spawns through a wrapper no marker names"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	if got := unknown.Unknowns(); !slices.Equal(got, []string{"a_test.go: no rule", "z_test.go: spawns through a wrapper no marker names"}) {
		t.Errorf("Unknowns = %v, want both, sorted", got)
	}
}
