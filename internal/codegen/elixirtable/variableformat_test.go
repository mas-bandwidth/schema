package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVariableFormEmitsFormatCanonicalSource pins issue #547's last half on
// the Makefile side. The variable form's Table.ex and TableRuntime.ex are
// emitted in mix format's shape, so make/elixir.mk's raw-format gate holds
// every file the table backend writes: ELIXIR_EMIT_FORMAT_PENDING is set and
// EMPTY. And no stamp or generate recipe runs `$(MIX) format ` without
// --check-formatted over generated table units, so tables-elixir-clean and
// the stamps that compile those units see the emitters' raw output and not a
// canonicalized copy of it. It reads make/elixir.mk only: no Elixir, no clock.
func TestVariableFormEmitsFormatCanonicalSource(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")

	found := false
	for _, line := range lines {
		if name, value, ok := strings.Cut(line, ":="); ok && strings.TrimSpace(name) == "ELIXIR_EMIT_FORMAT_PENDING" {
			found = true
			if p := strings.Fields(value); len(p) != 0 {
				t.Errorf("ELIXIR_EMIT_FORMAT_PENDING names %v: the raw-format gate must hold every emitted file", p)
			}
		}
	}
	if !found {
		t.Fatal("make/elixir.mk sets no ELIXIR_EMIT_FORMAT_PENDING")
	}

	// every recipe line, with the target it belongs to; a backslash
	// continuation is part of the line it continues
	target := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if !strings.HasPrefix(line, "\t") {
			if head, _, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, "#") && !strings.Contains(head, "=") && !strings.HasPrefix(line, " ") {
				target = strings.TrimSpace(head)
			}
			continue
		}
		r := line
		for strings.HasSuffix(strings.TrimSpace(lines[i]), "\\") && i+1 < len(lines) {
			i++
			r += "\n" + lines[i]
		}
		if strings.HasPrefix(strings.TrimSpace(r), "#") {
			continue
		}
		if !strings.Contains(r, "$(MIX) format ") || strings.Contains(r, "--check-formatted") {
			continue
		}
		if strings.Contains(r, "tables-generated-elixir") || strings.Contains(r, "$$files") ||
			strings.Contains(r, "elixir-fixedform") || strings.Contains(r, "elixir-fixed-bench") ||
			strings.Contains(r, "generated/bench/paired/elixir") || strings.Contains(r, "ELIXIR_EMIT_FORMAT") {
			t.Errorf("make/elixir.mk %s runs mix format without --check-formatted over generated table units: %q", target, strings.TrimSpace(r))
		}
	}
}
