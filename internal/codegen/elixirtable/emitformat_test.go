package elixirtable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitFormatTarget is the gate TestEmitFormatTargetChecksRawOutput reads.
const emitFormatTarget = "tables-elixir-emit-format-clean"

// TestEmitFormatTargetChecksRawOutput pins issue #547's formatter half on the
// Makefile side: make/elixir.mk's tables-elixir-emit-format-clean hands the
// RAW emitter output to `mix format --check-formatted`. Its recipe generates
// first and checks after, and no line between them runs `$(MIX) format `
// without --check-formatted — the canonicalizing step every older stamp ran,
// which is what made the older check unable to see an emitter's drift. And the
// exclusion list, ELIXIR_EMIT_FORMAT_PENDING, names only files the table
// backend actually writes, so a typo cannot quietly hold nothing. It reads
// make/elixir.mk and runs the generator in process: no Elixir, no clock.
func TestEmitFormatTargetChecksRawOutput(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "elixir.mk"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")

	var recipe []string
	for i, line := range lines {
		if strings.HasPrefix(line, emitFormatTarget+":") {
			for _, r := range lines[i+1:] {
				if !strings.HasPrefix(r, "\t") {
					break
				}
				recipe = append(recipe, r)
			}
			break
		}
	}
	if len(recipe) == 0 {
		t.Fatalf("make/elixir.mk defines no recipe for %s", emitFormatTarget)
	}

	generate, check := -1, -1
	for i, r := range recipe {
		if generate < 0 && strings.Contains(r, "generate --lang elixir") {
			generate = i
		}
		if strings.Contains(r, "$(MIX) format --check-formatted") {
			check = i
		}
	}
	if generate < 0 {
		t.Fatalf("%s's recipe never generates the corpus", emitFormatTarget)
	}
	if check < 0 {
		t.Fatalf("%s's recipe never runs $(MIX) format --check-formatted", emitFormatTarget)
	}
	if check < generate {
		t.Fatalf("%s's recipe checks (line %d) before it generates (line %d)", emitFormatTarget, check, generate)
	}
	for i, r := range recipe {
		if strings.Contains(r, "$(MIX) format ") && !strings.Contains(r, "--check-formatted") {
			t.Errorf("%s's recipe line %d formats the raw output before the check: %q", emitFormatTarget, i, strings.TrimSpace(r))
		}
	}
	// The gate must skip exactly the published pending list. A literal basename
	// in its filter would silently exempt another emitted file while this test's
	// check of ELIXIR_EMIT_FORMAT_PENDING below still passed.
	for _, r := range recipe {
		if strings.Contains(r, "continue ;; esac") && !strings.Contains(r, `case " $(ELIXIR_EMIT_FORMAT_PENDING) " in`) {
			t.Errorf("%s has an exclusion outside ELIXIR_EMIT_FORMAT_PENDING: %q", emitFormatTarget, strings.TrimSpace(r))
		}
	}

	pending, found := emitFormatPending(string(src))
	if !found {
		t.Fatal("make/elixir.mk sets no ELIXIR_EMIT_FORMAT_PENDING")
	}
	for _, problem := range pendingProblems(pending, emittedNames(t)) {
		t.Error(problem)
	}
}

// emitFormatPending reads ELIXIR_EMIT_FORMAT_PENDING from a make file's text:
// the names it holds, and whether it is set at all. An empty list is a valid
// state, and the shipped one: every emitted file is held (F-059).
func emitFormatPending(src string) (pending []string, found bool) {
	for line := range strings.SplitSeq(src, "\n") {
		if name, value, ok := strings.Cut(line, ":="); ok && strings.TrimSpace(name) == "ELIXIR_EMIT_FORMAT_PENDING" {
			pending = strings.Fields(value)
			found = true
		}
	}
	return pending, found
}

// emittedNames are the files the backend writes for a one-table variable unit:
// the variable form and its runtime are exactly the pending pair's home.
func emittedNames(t *testing.T) map[string]bool {
	t.Helper()
	written := map[string]bool{}
	out, err := Generate(cardUnit(&ir.Field{Name: "id", Type: ir.FieldType{Kind: ir.TInt, Width: 32}}))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name := range out {
		written[name] = true
	}
	if len(written) == 0 {
		t.Fatal("the Elixir table backend wrote no file for a one-table unit")
	}
	return written
}

// pendingProblems names each pending entry the backend does not write, and each
// that is not the variable form's pair. No entry, no problem.
func pendingProblems(pending []string, written map[string]bool) []string {
	var problems []string
	for _, p := range pending {
		if !written[p] {
			problems = append(problems, "ELIXIR_EMIT_FORMAT_PENDING names "+p+", which the Elixir table backend does not write")
		}
		if p != "Table.ex" && p != "TableRuntime.ex" {
			problems = append(problems, "ELIXIR_EMIT_FORMAT_PENDING names "+p+": only the variable form's Table.ex and TableRuntime.ex may wait for their own card")
		}
	}
	return problems
}

// TestEmitFormatPendingFixtures is F-059's regression test: the committed make
// fragments under testdata/emitformat hold the pending list's three states. An
// empty list is set and passes (a mechanical non-empty check once turned it
// red); a misspelled entry is refused (the control); an unset list is not
// found.
func TestEmitFormatPendingFixtures(t *testing.T) {
	t.Parallel()
	written := emittedNames(t)
	for _, c := range []struct {
		file     string
		found    bool
		problems int
	}{
		{"pending-empty.mk", true, 0},
		{"pending-misspelled.mk", true, 2},
		{"pending-unset.mk", false, 0},
	} {
		src, err := os.ReadFile(filepath.Join("testdata", "emitformat", c.file))
		if err != nil {
			t.Fatal(err)
		}
		pending, found := emitFormatPending(string(src))
		if found != c.found {
			t.Errorf("%s: found %v, want %v", c.file, found, c.found)
		}
		if got := pendingProblems(pending, written); len(got) != c.problems {
			t.Errorf("%s: %d problems %q, want %d", c.file, len(got), got, c.problems)
		}
	}
}
