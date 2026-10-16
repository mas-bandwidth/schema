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

	var pending []string
	found := false
	for _, line := range lines {
		if name, value, ok := strings.Cut(line, ":="); ok && strings.TrimSpace(name) == "ELIXIR_EMIT_FORMAT_PENDING" {
			pending = strings.Fields(value)
			found = true
		}
	}
	if !found {
		t.Fatal("make/elixir.mk sets no ELIXIR_EMIT_FORMAT_PENDING")
	}

	// the files the backend writes for a one-table variable unit: the
	// variable form and its runtime are exactly the pending pair's home
	written := map[string]bool{}
	out, err := Generate(cardUnit(&ir.Field{Name: "id", Type: ir.FieldType{Kind: ir.TInt, Width: 32}}))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name := range out {
		written[name] = true
	}
	for _, p := range pending {
		if !written[p] {
			t.Errorf("ELIXIR_EMIT_FORMAT_PENDING names %s, which the Elixir table backend does not write", p)
		}
	}
	for _, p := range pending {
		if p != "Table.ex" && p != "TableRuntime.ex" {
			t.Errorf("ELIXIR_EMIT_FORMAT_PENDING names %s: only the variable form's Table.ex and TableRuntime.ex may wait for their own card", p)
		}
	}
}
