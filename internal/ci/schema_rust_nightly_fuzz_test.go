package ci

// The schema-rust-nightly-fuzz card (Glenn, 2026-10-18): nightly
// coverage-guided fuzzing of the Rust port's generated readers. These are the
// fast structural gates. They do NOT run cargo-fuzz (that is the functional,
// slow target); they refuse a harness whose corpus is a second list, whose
// three reader kinds are not all present, whose nightly parts leaked onto a
// push, or whose missing-tool path is not a named not-done.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nightlyFuzzMake = "make/schema-rust-nightly-fuzz.mk"
const nightlyFuzzWorkflow = ".github/workflows/schema-rust-nightly-fuzz.yml"

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(data)
}

// makeAssignmentValue returns the value of `NAME :=`/`NAME ?=` in text, with
// backslash continuations joined.
func makeAssignmentValue(text, name string) string {
	var out strings.Builder
	collecting := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t")
		if !collecting {
			for _, op := range []string{" := ", " ?= "} {
				if rest, ok := strings.CutPrefix(line, name+op); ok {
					out.WriteString(strings.TrimSuffix(strings.TrimSpace(rest), "\\"))
					collecting = strings.HasSuffix(line, "\\")
					break
				}
			}
			if out.Len() == 0 {
				continue
			}
		} else {
			out.WriteString(" ")
			out.WriteString(strings.TrimSuffix(strings.TrimSpace(line), "\\"))
			collecting = strings.HasSuffix(line, "\\")
		}
		if !collecting {
			break
		}
	}
	return strings.TrimSuffix(strings.TrimSpace(out.String()), "\\")
}

// rustTableUnits reads RUST_TABLE_UNITS from make/rust.mk: the corpus the
// Rust generate target actually reads.
func rustTableUnits(t *testing.T, root string) []string {
	t.Helper()
	value := makeAssignmentValue(readRepoFile(t, root, "make/rust.mk"), "RUST_TABLE_UNITS")
	if value == "" {
		t.Fatal("make/rust.mk: RUST_TABLE_UNITS not found")
	}
	return strings.Fields(value)
}

// validateFuzzCorpus refuses a corpus that is invented, duplicated, or names a
// path the tree does not have. expected is RUST_TABLE_UNITS plus the arms
// directory; the harness must name exactly those units, because every schema a
// generate target reads must be fuzzed and no second list may appear.
func validateFuzzCorpus(root string, units, expected []string) error {
	if len(units) == 0 {
		return fmt.Errorf("corpus is empty")
	}
	want := map[string]bool{}
	for _, e := range expected {
		want[e] = true
	}
	got := map[string]bool{}
	for _, entry := range units {
		name, path, ok := strings.Cut(entry, ":")
		if !ok || name == "" || path == "" {
			return fmt.Errorf("corpus entry %q is not <name>:<path>", entry)
		}
		if got[name] {
			return fmt.Errorf("corpus names %q twice", name)
		}
		got[name] = true
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			return fmt.Errorf("corpus entry %q names a missing path %q", name, path)
		}
		if !want[entry] {
			return fmt.Errorf("corpus entry %q is not in the port's generate corpus (a second corpus was invented)", entry)
		}
	}
	for _, e := range expected {
		if !got[strings.SplitN(e, ":", 2)[0]] {
			return fmt.Errorf("the port's generate corpus entry %q is not fuzzed", e)
		}
	}
	return nil
}

func TestSchemaRustNightlyFuzzCorpusIsThePortsOwn(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text := readRepoFile(t, root, nightlyFuzzMake)
	value := makeAssignmentValue(text, "SCHEMA_RUST_FUZZ_UNITS")
	if value != "$(RUST_TABLE_UNITS) armdemo:tables/arms" {
		t.Fatalf("SCHEMA_RUST_FUZZ_UNITS = %q, want the port's own RUST_TABLE_UNITS plus tables/arms", value)
	}

	units := append(rustTableUnits(t, root), "armdemo:tables/arms")
	expected := append(rustTableUnits(t, root), "armdemo:tables/arms")
	if err := validateFuzzCorpus(root, units, expected); err != nil {
		t.Fatal(err)
	}

	// THE PLANTED BAD PATTERN: one corpus unit removed, and the gate must go
	// red on the missing coverage. The real list above is the green half.
	planted := units[1:]
	if err := validateFuzzCorpus(root, planted, expected); err == nil {
		t.Fatalf("a corpus with %q removed stayed green; the corpus gate does not bite", units[0])
	}
}

func TestSchemaRustNightlyFuzzHasOneTargetPerReaderKind(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text := readRepoFile(t, root, nightlyFuzzMake)
	for _, want := range []string{
		"SCHEMA_RUST_FUZZ_TARGETS := load strict text",
		"load    <root>_load",
		"strict  <root>_fixed_load",
		"text    <root>_from_json",
		"fuzz_targets/$$kind.rs",
		"corpus/$$name/src/*_table.rs",
		"corpus/$$name/src/*_fixed.rs",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzMake, want)
		}
	}
}

func TestSchemaRustNightlyFuzzMissingToolIsNotDone(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text := readRepoFile(t, root, nightlyFuzzMake)
	for _, want := range []string{
		"tool missing: $$name",
		"command -v cargo-fuzz",
		"rustup toolchain list",
		"tool missing: serialize.rs@$(SERIALIZE_RS_PIN)",
		"SCHEMA_RUST_FUZZ_VERSION ?= 0.13.1",
		"SCHEMA_RUST_FUZZ_NIGHTLY ?= nightly-2026-10-01",
		"SERIALIZE_RS_PIN ?= v2.4.0",
		"schema-rust-nightly-fuzz: schema-rust-nightly-fuzz-tool schema-rust-nightly-fuzz-harness schema-rust-nightly-fuzz-seeds",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzMake, want)
		}
	}
}

func TestSchemaRustNightlyFuzzFastHalfIsReplayOnly(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	text := readRepoFile(t, root, nightlyFuzzMake)
	if !strings.Contains(text, "schema-rust-nightly-fuzz-replay:") {
		t.Fatal("no replay target: the fast half of the card is missing")
	}
	if !strings.Contains(text, "SCHEMA_RUST_FUZZ_TIME ?= 10m") {
		t.Fatal("the nightly time budget is not an argument with a 10m default")
	}
	if !strings.Contains(text, "schema-rust-nightly-fuzz-negative-control:") {
		t.Fatal("no planted-pattern negative control target")
	}
}

func TestSchemaRustNightlyFuzzWorkflowTriggers(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wf := readRepoFile(t, root, nightlyFuzzWorkflow)
	for _, want := range []string{
		"schedule:",
		"cron: '17 3 * * *'",
		"workflow_dispatch:",
		"pull_request:",
		"make schema-rust-nightly-fuzz-replay",
		"make schema-rust-nightly-fuzz SCHEMA_RUST_FUZZ_TIME=10m",
		"if: github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("%s does not carry %q", nightlyFuzzWorkflow, want)
		}
	}
}
