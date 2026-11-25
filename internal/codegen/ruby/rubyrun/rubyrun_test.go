package rubyrun

import (
	"os"
	"path/filepath"
	"testing"
)

// The analyzers sit beside $RUBY when it is set and they have no variable of
// their own: the fleet SDK's env.sh exports RUBY and never RUBOCOP, STEEP or
// RBS, and make/ruby-toolchain.mk reads them beside $(RUBY). A lookup that
// went to dist/ instead failed every Ruby analyzer test on the fleet.
func TestAnalyzersResolveBesideRuby(t *testing.T) {
	dir := t.TempDir()
	interp := filepath.Join(dir, "ruby")
	for _, bin := range []string{"ruby", "rubocop", "steep", "rbs"} {
		if err := os.WriteFile(filepath.Join(dir, bin), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RUBY", interp)
	for _, a := range []struct {
		env, bin string
		get      func(testing.TB) string
	}{{"RUBOCOP", "rubocop", RuboCop}, {"STEEP", "steep", Steep}, {"RBS", "rbs", RBS}} {
		t.Setenv(a.env, "")
		if got, want := a.get(t), filepath.Join(dir, a.bin); got != want {
			t.Errorf("%s with RUBY=%s: %s, want %s", a.env, interp, got, want)
		}
	}
	// the analyzer's own variable still wins
	own := filepath.Join(dir, "own-rubocop")
	if err := os.WriteFile(own, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUBOCOP", own)
	if got := RuboCop(t); got != own {
		t.Errorf("RUBOCOP=%s: %s", own, got)
	}
}
