package roadmap

import (
	"os"
	"strings"
	"testing"
)

// allTargetsRecipe is the recipe of make roadmap-all-targets-check: the lines
// after its rule, while they are recipe lines.
func allTargetsRecipe(mk string) string {
	_, rest, ok := strings.Cut(mk, "\nroadmap-all-targets-check:\n")
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(rest, "\n") {
		if !strings.HasPrefix(line, "\t") {
			break
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// allTargetsProblems names what keeps the all-targets acceptance from
// requiring every target the registry names: the list not read by the
// registry's own reader, a failure of that reader not stopping the target,
// the acceptance given another list, or the nine asked for instead.
func allTargetsProblems(mk string) []string {
	recipe := allTargetsRecipe(mk)
	if recipe == "" {
		return []string{"roadmap.mk has no rule roadmap-all-targets-check"}
	}
	var bad []string
	if !strings.Contains(recipe, `targets=$$(go run ./tools/schema-test langs --targets) || {`) {
		bad = append(bad, "the targets are not read by schema-test langs --targets, or its failure does not stop the target")
	}
	if strings.Count(recipe, "--require-all-targets") != 2 || !strings.Contains(recipe, `--require-all-targets "$$targets" docs/roadmap.sexp ROADMAP.md`) {
		bad = append(bad, "the acceptance is not given the registry's targets")
	}
	if strings.Contains(recipe, "--require-original-nine") {
		bad = append(bad, "the recipe asks for the original nine")
	}
	return bad
}

// TestAllTargetsCheckRequiresTheRegistrysTargets: make
// roadmap-all-targets-check (the registry row
// tooling.roadmap.all-targets-complete) requires every target the registry
// names, read by the registry's own reader, and nothing narrower; the
// Lisp suite (make roadmap-test) holds render.lisp to the list it is given.
// The plants: a hand-written list, the nine, a reader whose failure is
// ignored, and the rule gone.
func TestAllTargetsCheckRequiresTheRegistrysTargets(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("roadmap.mk")
	if err != nil {
		t.Fatal(err)
	}
	mk := string(data)
	if bad := allTargetsProblems(mk); len(bad) > 0 {
		t.Fatalf("make roadmap-all-targets-check does not require every target:\n  %s", strings.Join(bad, "\n  "))
	}
	for _, p := range []struct{ name, old, new string }{
		{"a hand-written list", `--require-all-targets "$$targets" docs`, `--require-all-targets cpp,c,cs,go,rust,java,js,dart,elixir docs`},
		{"the nine", `--require-all-targets "$$targets" docs`, `--require-original-nine docs`},
		{"the reader's failure ignored", `targets=$$(go run ./tools/schema-test langs --targets) || {`, `targets=$$(go run ./tools/schema-test langs --targets || true); {`},
		{"the rule gone", "\nroadmap-all-targets-check:\n", "\nroadmap-every-target-check:\n"},
	} {
		if strings.Count(mk, p.old) != 1 {
			t.Errorf("%s: the plant's anchor is not there exactly once", p.name)
			continue
		}
		if len(allTargetsProblems(strings.Replace(mk, p.old, p.new, 1))) == 0 {
			t.Errorf("PLANTED PATTERN %q left the check green", p.name)
		}
	}
}
