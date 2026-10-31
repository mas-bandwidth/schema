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
// fails; it holds the roots the leg cannot open to a pinned list, so a root that
// drops out fails by name; and a `test-elixir:` line names it.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

	const command = "ELIXIR_TABLES_EBIN=$(CURDIR)/build/elixir-tables-ebin ./build/conformance-harness wire-fuzz --driver 'test/conformance/elixir/driver wire-fuzz' --seed $(SEED) --n $(N)"
	if !strings.Contains(run, command) {
		t.Errorf("the %s recipe does not run %q:\n%s", target, command, recipe)
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

	// THE ABSENT ROOTS ARE PINNED BY NAME AND BY COUNT. The driver writes one
	// line per roster entry it answers 0 for to $ELIXIR_WIRE_FUZZ_ABSENT, and
	// the recipe compares that list with the pinned file, so a root the leg
	// stops opening fails the gate naming the root.
	const pin = "test/conformance/elixir/wire-fuzz-absent.txt"
	if !strings.Contains(run, "ELIXIR_WIRE_FUZZ_ABSENT=") {
		t.Errorf("the %s recipe does not ask the driver for its absent roots (ELIXIR_WIRE_FUZZ_ABSENT):\n%s", target, recipe)
	}
	if !strings.Contains(run, pin) {
		t.Errorf("the %s recipe does not compare the absent roots with %s:\n%s", target, pin, recipe)
	}
	m := regexp.MustCompile(`(?m)^ELIXIR_WIRE_FUZZ_ABSENT_COUNT := ([0-9]+)$`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("make/elixir.mk pins no ELIXIR_WIRE_FUZZ_ABSENT_COUNT")
	}
	want, _ := strconv.Atoi(m[1])
	if !strings.Contains(run, "$(ELIXIR_WIRE_FUZZ_ABSENT_COUNT)") {
		t.Errorf("the %s recipe does not check the absent count against ELIXIR_WIRE_FUZZ_ABSENT_COUNT:\n%s", target, recipe)
	}
	pinned, err := os.ReadFile(filepath.Join(root, pin))
	if err != nil {
		t.Fatalf("%s is unreadable: %v", pin, err)
	}
	var names []string
	for line := range strings.SplitSeq(string(pinned), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			names = append(names, line)
		}
	}
	if len(names) != want {
		t.Errorf("%s names %d absent roots, ELIXIR_WIRE_FUZZ_ABSENT_COUNT pins %d", pin, len(names), want)
	}
	const pinnedCount = 33
	if want != pinnedCount {
		t.Errorf("ELIXIR_WIRE_FUZZ_ABSENT_COUNT is %d, this test pins %d: a change in the roots the Elixir leg opens is a reviewed change to both", want, pinnedCount)
	}

	if !testElixirNames(t, text, target) {
		t.Fatalf("no `test-elixir:` line names %s, so `make test-elixir` never reaches the wire fuzzer over every root", target)
	}
}
