package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestRelaxVectorVerdict pins --relax-vector's per-mutant rule (RELAXED
// 2026-11-14, docs/relaxed.md relax/unknown-arm-text-<lang>): a relaxed pinned
// vector that diverges is counted and not failed; one that agrees with the
// oracle fails the run; every other mutant keeps its verdict.
func TestRelaxVectorVerdict(t *testing.T) {
	t.Parallel()
	relax := map[string]bool{"v": true}

	seen := map[string]bool{}
	got, err := relaxVectorVerdict("vector", "v", "read acceptance differs", relax, seen)
	if err != nil || got != "" || !seen["v"] {
		t.Fatalf("a relaxed divergence: verdict %q, err %v, seen %v; want it counted and not failed", got, err, seen)
	}

	seen = map[string]bool{}
	if _, err := relaxVectorVerdict("vector", "v", "", relax, seen); err == nil || !strings.Contains(err.Error(), "now agrees with the oracle") {
		t.Fatalf("a relaxed vector that agrees did not fail the run: %v", err)
	}

	for _, c := range []struct{ pass, name string }{
		{"vector", "other"}, // a pinned vector nobody relaxed
		{"truncate", "v"},   // a mutant, even of a relaxed name
	} {
		seen = map[string]bool{}
		got, err := relaxVectorVerdict(c.pass, c.name, "diverged", relax, seen)
		if err != nil || got != "diverged" || len(seen) != 0 {
			t.Fatalf("%s %s: verdict %q, err %v, seen %v; want the verdict unchanged", c.pass, c.name, got, err, seen)
		}
	}
}

// TestRelaxedVectorSet pins --relax-vector's parse (RELAXED 2026-11-14,
// docs/relaxed.md relax/unknown-arm-text-<lang>): comma-separated names,
// blanks dropped, and an empty flag relaxes nothing.
func TestRelaxedVectorSet(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   string
		want map[string]bool
	}{
		{"", map[string]bool{}},
		{"unknown_arm_text_nul", map[string]bool{"unknown_arm_text_nul": true}},
		{" a , ,b,", map[string]bool{"a": true, "b": true}},
	} {
		if got := relaxedVectorSet(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("relaxedVectorSet(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
