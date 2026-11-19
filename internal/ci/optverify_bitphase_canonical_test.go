package ci

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestOptVerifyBitPhaseAndCanonicalAll18 verifies that make/verify-widen-18.mk
// wires both Pillar #1 (verify-bitphase-<lang>, 0..63 packet bit-phase sweep)
// and Pillar #5 (verify-canonical-<lang>, canonical-write unsorted map &
// distinct equal-blob input permutations) into _verify-<lang>-bench across all
// 18 ports, and registers both sabotage negative controls.
func TestOptVerifyBitPhaseAndCanonicalAll18(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	widenBytes, err := os.ReadFile(filepath.Join(root, "make", "verify-widen-18.mk"))
	if err != nil {
		t.Fatalf("read make/verify-widen-18.mk: %v", err)
	}
	widenText := string(widenBytes)

	gotLegs := parseMakeWordList(t, widenText, "VERIFY_ALL_18_LEGS")
	sort.Strings(gotLegs)
	if !slices.Equal(gotLegs, allOptVerifyLangs) {
		t.Errorf("VERIFY_ALL_18_LEGS = %v; want %v", gotLegs, allOptVerifyLangs)
	}

	for _, needle := range []string{
		"build/verify-bitphase-bin:",
		"build/verify-bitphase/corpus/.stamp:",
		"build/verify-canonical-bin:",
		"build/verify-canonical/corpus/.stamp:",
		"verify-bitphase-$(1):",
		"verify-canonical-$(1):",
		"_verify-$(1)-bench: verify-bitphase-$(1) verify-canonical-$(1)",
		"verify-bitphase-negative-control:",
		"verify-canonical-negative-control:",
	} {
		if !strings.Contains(widenText, needle) {
			t.Errorf("make/verify-widen-18.mk missing %q", needle)
		}
	}

	for _, lang := range allOptVerifyLangs {
		depVar := "VERIFY_WIDEN_DEPS_" + lang
		if !strings.Contains(widenText, depVar) {
			t.Errorf("make/verify-widen-18.mk missing %s", depVar)
		}
	}

	ncBytes, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.d", "verify-negative-controls.json"))
	if err != nil {
		t.Fatalf("read verify-negative-controls.json: %v", err)
	}
	ncText := string(ncBytes)
	for _, nc := range []string{
		"verify-bitphase-negative-control",
		"verify-canonical-negative-control",
	} {
		if !strings.Contains(ncText, `"`+nc+`"`) {
			t.Errorf("verify-negative-controls.json missing %q", nc)
		}
	}
}
