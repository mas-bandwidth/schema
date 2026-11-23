package rusttable

import (
	"regexp"
	"strings"
	"testing"
)

// A MINIMUM OF ONE IS AN EMPTINESS TEST in the emitted Rust: clippy's len_zero
// (tables-rust-clippy, -D warnings) denies len() < 1 and len() >= 1, so the
// [1..N]T hold on the debug write, the text write and the text read is spelled
// with is_empty (docs/SPEC-TABLES.md §3). The [2..N]T hold stays a length
// comparison. A fast test: it generates and reads the source, compiling none.
func TestRustArrayMinOfOneIsAnEmptinessTest(t *testing.T) {
	t.Parallel()
	modules, err := Generate(unitFrom(t, variableMinSrc))
	if err != nil {
		t.Fatalf("rust refused the [Min..N]T unit: %v", err)
	}
	lenOne := regexp.MustCompile(`\.len\(\) (<|>=) 1\b`)
	empties, twos := 0, 0
	for name, src := range modules {
		if loc := lenOne.FindIndex(src); loc != nil {
			t.Errorf("%s compares a length with 1, which clippy's len_zero denies: %q", name, src[loc[0]:loc[1]])
		}
		empties += len(regexp.MustCompile(`rows\.is_empty\(\)`).FindAll(src, -1))
		twos += len(regexp.MustCompile(`items\.len\(\) (<|>=) 2`).FindAll(src, -1))
	}
	if empties == 0 {
		t.Errorf("no rows.is_empty() hold: the [1..2]Child minimum is not held at all")
	}
	if twos == 0 {
		t.Errorf("no items.len() < 2 hold: the [2..3]int32 minimum is not held at all")
	}

	// The by-value conversion into a fixed Row is a separate emitter path from
	// the variable wire and text paths above. It holds the same minimum before
	// copying the live elements.
	fixed := generate(t, `package minfixed

fixed table Root
{
    rows [1..3]int32
}
`)
	fixedValues := string(fixed["probe_fixed_values.rs"])
	if loc := lenOne.FindStringIndex(fixedValues); loc != nil {
		t.Fatalf("fixed value conversion compares a length with 1: %q", fixedValues[loc[0]:loc[1]])
	}
	if !strings.Contains(fixedValues, "!value.rows.is_empty()") {
		t.Fatal("fixed value conversion does not hold [1..3] with an emptiness test")
	}
}
