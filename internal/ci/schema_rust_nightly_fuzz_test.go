// Package ci holds the gate on this repo's continuous integration.
//
// This file is the static gate over make/schema-rust-nightly-fuzz.mk and its
// workflow. The property it pins is the harness's SHAPE, which is what the
// card can check without a fuzz clock: one fuzz_target per reader kind (load,
// strict, json), the corpus read from the port's one corpus list
// (RUST_TABLE_UNITS), a replay that passes -runs and never -max_total_time, a
// planted reader bug that the nightly leg must find, and a scheduled trigger
// for the heavy half. The behavioural half — the fuzzer's exit status and the
// replay of the saved input — is the make target the card names; this test is
// the planted-pattern part.
package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readNightlyFuzzFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// checkNightlyFuzzHarness is the pattern the test plants against. It refuses a
// harness that has lost a reader kind, a corpus list, a clock-free replay, the
// plant, or the scheduled trigger; a copy with a reader kind removed is the
// planted bad pattern the test below requires to go red.
func checkNightlyFuzzHarness(mk, workflow string) []string {
	var bad []string
	want := []string{
		"FUZZ_SECONDS ?= 600",
		"FUZZ_CARGO_FUZZ_VERSION ?= 0.13.1",
		"tool missing: cargo-fuzz",
		"for kind in load strict json",
		"pat='_load('",
		"pat='_load_retain('",
		"pat='_from_json('",
		"RUST_TABLE_UNITS",
		"$(FUZZ_DIR)/corpus.txt",
		"schema-rust-nightly-fuzz-replay",
		"-runs=$$n",
		"schema-rust-nightly-fuzz-negative-control",
		"wire.len() < 9",
	}
	for _, w := range want {
		if !strings.Contains(mk, w) {
			bad = append(bad, "make/schema-rust-nightly-fuzz.mk lost "+w)
		}
	}
	if strings.Contains(mk, "fuzz run $$kind -- -max_total_time=$(FUZZ_SECONDS)") == false {
		bad = append(bad, "the nightly fuzz target does not pass the FUZZ_SECONDS budget")
	}
	// the replay must never carry a wall clock: it passes the file count, and
	// the only -max_total_time uses are the nightly run and the plant's proof
	if strings.Count(mk, "-max_total_time=") != 2 {
		bad = append(bad, "the replay carries a wall clock (unexpected -max_total_time count)")
	}
	for _, w := range []string{"schedule:", "workflow_dispatch:", "cron:", "make schema-rust-nightly-fuzz FUZZ_SECONDS=600"} {
		if !strings.Contains(workflow, w) {
			bad = append(bad, ".github/workflows/schema-rust-nightly-fuzz.yml lost "+w)
		}
	}
	return bad
}

func TestSchemaRustNightlyFuzzHarness(t *testing.T) {
	t.Parallel()
	mk := readNightlyFuzzFile(t, "make/schema-rust-nightly-fuzz.mk")
	workflow := readNightlyFuzzFile(t, ".github/workflows/schema-rust-nightly-fuzz.yml")
	if bad := checkNightlyFuzzHarness(mk, workflow); len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}

// TestSchemaRustNightlyFuzzPlantedPattern is the plant the card names: remove
// one reader kind from a scratch copy of the make file and the same check must
// go red; with the file untouched it is green. Nothing tracked is edited.
func TestSchemaRustNightlyFuzzPlantedPattern(t *testing.T) {
	t.Parallel()
	mk := readNightlyFuzzFile(t, "make/schema-rust-nightly-fuzz.mk")
	workflow := readNightlyFuzzFile(t, ".github/workflows/schema-rust-nightly-fuzz.yml")

	if bad := checkNightlyFuzzHarness(mk, workflow); len(bad) > 0 {
		t.Fatalf("the harness is already red before the plant: %s", strings.Join(bad, "\n"))
	}

	planted := strings.Replace(mk, "for kind in load strict json", "for kind in load json", 1)
	planted = strings.ReplaceAll(planted, "pat='_load_retain('", "pat='_load_retain_sabotaged('")
	bad := checkNightlyFuzzHarness(planted, workflow)
	if len(bad) == 0 {
		t.Fatal("the planted pattern (a missing strict reader kind) left the gate green")
	}
	found := false
	for _, b := range bad {
		if strings.Contains(b, "_load_retain") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the gate went red for the wrong reason: %s", strings.Join(bad, "\n"))
	}
}
