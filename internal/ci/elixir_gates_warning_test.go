package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestElixirGatesFailOnAPlantedWarning guards the Elixir gates against the
// lenient --warnings-as-errors spell. Elixir 1.20.4 on OTP 29 accepts the flag
// but exits 0 even when warnings are printed (the diagnostics go out through
// the logger, not the compiler's warning counter), so a gate line that compiles
// with `$(ELIXIRC) --warnings-as-errors` never fails on a warning. Every gate
// must compile through the strict ELIXIRC_STRICT invocation instead.
func TestElixirGatesFailOnAPlantedWarning(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "make", "elixir.mk"))
	if err != nil {
		t.Fatalf("read make/elixir.mk: %v", err)
	}
	var lenient []string
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "$(ELIXIRC)") && strings.Contains(line, "--warnings-as-errors") {
			lenient = append(lenient, fmt.Sprintf("%d: %s", i+1, line))
		}
	}
	if len(lenient) > 0 {
		t.Errorf("lenient elixirc invocation still present (%d line(s)):\n%s",
			len(lenient), strings.Join(lenient, "\n"))
	}
}
