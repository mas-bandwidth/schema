package elixirtable

// TestWireFuzzAllTargetElixir IS THE WIRING HALF of the Elixir leg's wire fuzzer
// over every root (docs/SPEC-TABLES.md §4.2; test/conformance/README.md, "The
// wire-fuzz driver"). The cards targets bind the Card root alone through
// --manifest; tables-elixir-wire-fuzz-all runs the harness's DEFAULT roster
// (testdata/conformance/tables/MANIFEST.txt, the default in
// test/conformance/harness/main.go), the roster tables-go-wire-fuzz runs. This
// test reads make/elixir.mk and pins four things, with no toolchain and no
// network: the recipe runs the harness with no --manifest; it carries the
// zero-coverage greps the card fuzz targets use, so a roster with no live root
// fails; it fails on any root the leg cannot open, naming it, with no pinned
// list to hold one back; and a `test-elixir:` line names it.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWireFuzzAllTargetElixir(t *testing.T) {
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

	const target = "tables-elixir-wire-fuzz-all"
	recipe := makeTargetRecipe(t, text, target)
	run := regexp.MustCompile(`\s+`).ReplaceAllString(recipe, " ")

	// The ebin is a variable so the negative control can run this same recipe
	// over a sabotaged copy; its default is the real build.
	const command = "ELIXIR_TABLES_EBIN=$(ELIXIR_WIRE_FUZZ_ALL_EBIN) ./build/conformance-harness wire-fuzz --driver 'test/conformance/elixir/driver wire-fuzz' --seed $(SEED) --n $(N)"
	if !strings.Contains(run, command) {
		t.Errorf("the %s recipe does not run %q:\n%s", target, command, recipe)
	}
	const ebin = "ELIXIR_WIRE_FUZZ_ALL_EBIN ?= $(CURDIR)/build/elixir-tables-ebin"
	if !strings.Contains(text, ebin) {
		t.Errorf("make/elixir.mk does not default the gate's ebin to the real build (%q)", ebin)
	}
	if strings.Contains(run, "--manifest") {
		t.Errorf("the %s recipe passes --manifest, so it fuzzes a chosen roster rather than the harness's default one:\n%s", target, recipe)
	}
	if strings.Contains(run, "--retain") {
		t.Errorf("the %s recipe passes --retain: the retention arm is its own gate, this one is the plain run:\n%s", target, recipe)
	}
	for _, grep := range []string{
		"grep -q '0 divergences'",
		"grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots'",
		"grep -qE '[1-9][0-9]* mutants'",
	} {
		if !strings.Contains(run, grep) {
			t.Errorf("the %s recipe lacks the zero-coverage check %q, so a roster with no live root could pass:\n%s", target, grep, recipe)
		}
	}

	// AN ABSENT ROOT IS RED. The driver writes one line per roster entry it
	// answers 0 for to $ELIXIR_WIRE_FUZZ_ABSENT, and the recipe fails on any
	// line there, naming the root. No pinned list holds a root back (the
	// no-skips ruling, docs/rulings.json), and none may come back.
	if !strings.Contains(run, "ELIXIR_WIRE_FUZZ_ABSENT=") {
		t.Errorf("the %s recipe does not ask the driver for its absent roots (ELIXIR_WIRE_FUZZ_ABSENT):\n%s", target, recipe)
	}
	const zero = `if [ "$$n" -ne 0 ]; then`
	if !strings.Contains(run, zero) {
		t.Errorf("the %s recipe does not fail on an absent root (%q):\n%s", target, zero, recipe)
	}
	for _, retired := range []string{"wire-fuzz-absent", "ELIXIR_WIRE_FUZZ_ABSENT_COUNT"} {
		if strings.Contains(text, retired) {
			t.Errorf("make/elixir.mk carries %q: a pinned list of absent roots holds roots back, and it is retired", retired)
		}
	}
	if strings.Contains(run, "pinned") {
		t.Errorf("the %s recipe compares the absent roots with a pinned list, which is retired:\n%s", target, recipe)
	}
	if _, err := os.Stat(filepath.Join(root, "test", "conformance", "elixir", "wire-fuzz-absent.txt")); err == nil {
		t.Errorf("test/conformance/elixir/wire-fuzz-absent.txt is back: a pinned list of absent roots is retired")
	}

	if !testElixirNames(t, text, target) {
		t.Fatalf("no `test-elixir:` line names %s, so `make test-elixir` never reaches the wire fuzzer over every root", target)
	}
}
