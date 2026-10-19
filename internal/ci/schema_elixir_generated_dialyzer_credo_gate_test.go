package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaElixirGeneratedLensPinsItsTools holds the make file to the two
// properties that make this a lens rather than a claim: both tools are named
// and pinned, and a missing tool is reported by name instead of passing. It
// also checks that every Credo check the gate disables carries a one-line
// reason beside it, because a suppression without a justification is a hole.
func TestSchemaElixirGeneratedLensPinsItsTools(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "schema-elixir-generated-dialyzer-credo-gate.mk"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"SCHEMA_ELIXIR_CREDO_VERSION ?= 1.7.12",
		"SCHEMA_ELIXIR_DIALYZER",
		"dialyzer",
		"tool missing:",
		"schema-elixir-generated-credo-gate-fast",
		"schema-elixir-generated-dialyzer-credo-gate-negative-control",
		// Dialyzer reads Elixir's debug_info through elixir_erl, which lives in
		// the Elixir installation: without ERL_LIBS the analysis crashes before
		// it reports anything, so the invocation must carry the same ERL_LIBS
		// the PLT build uses.
		`ERL_LIBS="$(SCHEMA_ELIXIR_ERL_LIBS)"`,
		"SCHEMA_ELIXIR_DIALYZER_WARNINGS",
		"-Wno_match -Wno_unused -Wno_fail_call",
		"{Credo.Check.Refactor.FunctionArity, false}",
		"{Credo.Check.Refactor.CondStatements, false}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("make/schema-elixir-generated-dialyzer-credo-gate.mk does not name %q", want)
		}
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "'    {Credo.Check.") || !strings.Contains(line, "false}") {
			continue
		}
		prev := ""
		if i > 0 {
			prev = strings.TrimSpace(lines[i-1])
		}
		if !strings.Contains(prev, "# ") {
			t.Errorf("disabled Credo check has no one-line justification beside it: %s", line)
		}
	}
}

// TestSchemaElixirGeneratedLensCatchesAPlant runs the planted-bad control the
// make file defines: a same-value comparison appended to a generated unit must
// red Credo --strict and be named in its output, while the same scratch tree
// without the plant is clean. It is functional (it runs bin/schema and the
// Credo escript), so it is gated on SCHEMA_REQUIRE_CORPUS=1 exactly as the
// other toolchain-backed tests in this package are: the make target runs it
// under that variable and refuses a skip.
func TestSchemaElixirGeneratedLensCatchesAPlant(t *testing.T) {
	t.Parallel()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "1" {
		t.Skip("functional: set SCHEMA_REQUIRE_CORPUS=1 (make schema-elixir-generated-dialyzer-credo-gate-negative-control)")
	}
	root := repoRoot(t)
	args := []string{"-f", filepath.Join(root, "Makefile"),
		"schema-elixir-generated-dialyzer-credo-gate-negative-control"}
	if credo := os.Getenv("CREDO"); credo != "" {
		args = append(args, "SCHEMA_ELIXIR_CREDO="+credo)
	}
	cmd := exec.Command("make", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("negative control: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "naming the comparison") {
		t.Fatalf("negative control did not report the named plant:\n%s", out)
	}
}
