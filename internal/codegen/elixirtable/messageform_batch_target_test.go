package elixirtable

// TestElixirMessageBatchTargetRunsUnderTestElixir IS THE WIRING HALF of the
// Elixir message form's gate. TestMessageFormBatchElixir gets its Elixir through
// elixirBinary (fixedversioning_test.go), which SKIPS itself when no toolchain
// is found and only FAILS by name under SCHEMA_REQUIRE_CORPUS, so a bare
// `go test` on a bench with no Elixir never runs the cell and exits 0 — exactly
// how the message form's :done was read, not run. This test reads make/elixir.mk
// from the repository root and pins three things: the target
// tables-elixir-message-batch exists, its recipe runs the batch test under
// SCHEMA_REQUIRE_CORPUS=1 with the exact -run, and a `test-elixir:` line names
// it — so the gate cannot be deleted or disarmed without this test going red.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestElixirMessageBatchTargetRunsUnderTestElixir(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "make", "elixir.mk"))
	if err != nil {
		t.Fatalf("make/elixir.mk is unreadable: %v", err)
	}
	text := strings.ReplaceAll(string(data), "\\\n", " ")

	const target = "tables-elixir-message-batch"
	recipe := makeTargetRecipe(t, text, target)
	if !strings.Contains(recipe, "SCHEMA_REQUIRE_CORPUS=1") {
		t.Errorf("the %s recipe does not set SCHEMA_REQUIRE_CORPUS=1, so a missing Elixir skips instead of failing:\n%s", target, recipe)
	}
	if !strings.Contains(recipe, "./internal/codegen/elixirtable/") {
		t.Errorf("the %s recipe does not run go test on ./internal/codegen/elixirtable/:\n%s", target, recipe)
	}
	if !strings.Contains(recipe, "-run '^TestMessageFormBatchElixir$$'") {
		t.Errorf("the %s recipe does not select ^TestMessageFormBatchElixir$:\n%s", target, recipe)
	}

	if !testElixirNames(t, text, target) {
		t.Fatalf("no `test-elixir:` line names %s, so `make test-elixir` never reaches the message batch gate", target)
	}
}

// makeTargetRecipe returns one make target's recipe: the tab-indented lines that
// follow its `target:` rule, with `\\\n` continuations already collapsed to a
// single line so a wrapped recipe is read whole.
func makeTargetRecipe(t *testing.T, text, target string) string {
	t.Helper()
	prefix := target + ":"
	in := false
	var recipe strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			in = true
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(line, "\t") {
			recipe.WriteString(line)
			recipe.WriteByte('\n')
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		break
	}
	if recipe.Len() == 0 {
		t.Fatalf("make/elixir.mk has no recipe for %s: the Elixir message form batch runs under no gate", target)
	}
	return recipe.String()
}

// testElixirNames reports whether some `test-elixir:` rule lists the target as a
// prerequisite.
func testElixirNames(t *testing.T, text, target string) bool {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		if !strings.HasPrefix(line, "test-elixir:") {
			continue
		}
		for field := range strings.FieldsSeq(strings.TrimPrefix(line, "test-elixir:")) {
			if field == target {
				return true
			}
		}
	}
	return false
}
