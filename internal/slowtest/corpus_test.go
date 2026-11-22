package slowtest

import "testing"

// TestCorpusRequired holds the rule: the make gates' variable, or CI running
// the slow half, make a missing corpus a failure; nothing else does.
func TestCorpusRequired(t *testing.T) {
	for _, c := range []struct {
		name              string
		require, ci, slow string
		want              bool
	}{
		{"default run", "", "", "", false},
		{"laptop slow run", "", "", "1", false},
		{"CI default run", "", "true", "", false},
		{"CI slow run", "", "true", "1", true},
		{"make gate", "1", "", "", true},
		{"make gate in CI", "1", "true", "1", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SCHEMA_REQUIRE_CORPUS", c.require)
			t.Setenv("CI", c.ci)
			t.Setenv("SCHEMA_SLOW", c.slow)
			if got := CorpusRequired(); got != c.want {
				t.Fatalf("CorpusRequired() = %v, want %v", got, c.want)
			}
		})
	}
}
