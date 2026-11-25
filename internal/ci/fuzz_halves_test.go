package ci

// THE FUZZ GATES IN PARTS (2026-11-24). verify-leg (rust, fuzz) and (goose,
// fuzz) were cut at the two-minute cap on main's push run 45431445496. Each
// now runs per commit in parts: Rust's in two halves on two rows, Goose's in
// two unit slices at the per-commit mutant cap. These tests hold the parts'
// make targets to the whole gate, so splitting a gate never drops a pass.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

func readRepoRel(t *testing.T, rel string) string {
	t.Helper()
	testinputs.Files(t, rel)
	data, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// subMakeTargets answers the targets a phony target's one recursive make
// names: `target:` followed by `@$(MAKE) ... t1 t2 \ t3 VAR=x`.
func subMakeTargets(t *testing.T, mk, target string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:\n\t@\$\(MAKE\)((?:[^\n]*\\\n)*[^\n]*)`)
	m := re.FindStringSubmatch(mk)
	if m == nil {
		t.Fatalf("no recursive make under %s:", target)
	}
	var out []string
	for f := range strings.FieldsSeq(strings.ReplaceAll(m[1], "\\\n", " ")) {
		if strings.HasPrefix(f, "_verify-") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// templatePrereqs answers the prerequisites of `_verify-$(1)-fuzz:` in the
// compiled legs' template, for leg.
func templatePrereqs(t *testing.T, mk, rule, leg string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(rule) + `:([^\n]*)$`)
	m := re.FindStringSubmatch(mk)
	if m == nil {
		t.Fatalf("no rule %s", rule)
	}
	var out []string
	for f := range strings.FieldsSeq(m[1]) {
		out = append(out, strings.ReplaceAll(f, "$(1)", leg))
	}
	sort.Strings(out)
	return out
}

func union(a, b []string) []string {
	out := append(append([]string(nil), a...), b...)
	sort.Strings(out)
	return slices.Compact(out)
}

// rustHalvesErrs answers how the two Rust halves differ from the whole gate.
func rustHalvesErrs(t *testing.T, mk string) []string {
	var errs []string
	whole := templatePrereqs(t, mk, "_verify-$(1)-fuzz", "rust")
	wire := subMakeTargets(t, mk, "verify-rust-fuzz-wire")
	row := subMakeTargets(t, mk, "verify-rust-conformance-and-driver-fuzz")
	driver := slices.DeleteFunc(slices.Clone(row), func(s string) bool {
		return s == "_verify-rust-conformance" || s == "_verify-rust-driver-write-refusal"
	})
	if !slices.Contains(row, "_verify-rust-conformance") {
		errs = append(errs, "the (rust, conformance) row's target no longer runs _verify-rust-conformance")
	}
	if got := union(wire, driver); !slices.Equal(got, whole) {
		errs = append(errs, "the two halves run "+strings.Join(got, " ")+", the whole gate "+strings.Join(whole, " "))
	}
	if !slices.Equal(subMakeTargets(t, mk, "verify-rust-fuzz-driver"), driver) {
		errs = append(errs, "verify-rust-fuzz-driver is not the conformance row's fuzz half")
	}
	return errs
}

// TestRustFuzzHalvesAreTheWholeGate: the (rust, fuzz) row's wire half and the
// (rust, conformance) row's driver half together are _verify-rust-fuzz.
func TestRustFuzzHalvesAreTheWholeGate(t *testing.T) {
	t.Parallel()
	mk := readRepoRel(t, "make/verify-compiled.mk")
	if errs := rustHalvesErrs(t, mk); len(errs) > 0 {
		t.Errorf("the Rust fuzz halves are not the whole gate: %s", strings.Join(errs, "; "))
	}
	// PLANTED: the message pass dropped from the conformance row.
	planted := strings.Replace(mk, "_verify-rust-conformance _verify-rust-fuzz-json _verify-rust-fuzz-message", "_verify-rust-conformance _verify-rust-fuzz-json", 1)
	if planted == mk {
		t.Fatal("the planted control did not apply")
	}
	if len(rustHalvesErrs(t, planted)) == 0 {
		t.Error("PLANTED CONTROL FAILED: the halves without the message pass still read as the whole gate")
	}
}

// gooseSliceUnits is the message pass's cut under a slice: the units whose
// legs the slice built.
const gooseSliceUnits = "\t\t$(if $(GOOSE_FUZZ_SLICE),--units \"$$(ls $(GOOSE_LEGS_DIR)/legs | paste -sd, -)\") \\\n"

// gooseErrs answers how the two Goose parts differ from the whole gate.
func gooseErrs(t *testing.T, mk string) []string {
	var errs []string
	re := regexp.MustCompile(`(?m)^_verify-goose-fuzz:([^\n]*)$`)
	m := re.FindStringSubmatch(mk)
	if m == nil {
		return []string{"no _verify-goose-fuzz rule"}
	}
	whole := strings.Fields(m[1])
	sort.Strings(whole)
	p1 := subMakeTargets(t, mk, "verify-goose-fuzz-part-1")
	p2 := subMakeTargets(t, mk, "verify-goose-fuzz-part-2")
	if got := union(p1, p2); !slices.Equal(got, whole) {
		errs = append(errs, "the two parts run "+strings.Join(got, " ")+", the whole gate "+strings.Join(whole, " "))
	}
	// the unit passes run in both parts, each over its own slice
	for _, pass := range []string{"_verify-goose-fuzz-plain", "_verify-goose-fuzz-retain", "_verify-goose-fuzz-message"} {
		if !slices.Contains(p1, pass) || !slices.Contains(p2, pass) {
			errs = append(errs, pass+" does not run in both slices")
		}
	}
	for _, slice := range []string{"GOOSE_FUZZ_SLICE=1/2", "GOOSE_FUZZ_SLICE=2/2"} {
		if !strings.Contains(mk, slice) {
			errs = append(errs, "no part takes "+slice)
		}
	}
	// a slice builds only its units' legs, so its message pass asks only
	// their roots (harness --units, read off the legs it built); the
	// unsliced gate asks the whole roster
	if !regexp.MustCompile(`(?m)^_verify-goose-fuzz-message:[^\n]*\n(?:\t[^\n]*\n)*?` + regexp.QuoteMeta(gooseSliceUnits)).MatchString(mk) {
		errs = append(errs, "_verify-goose-fuzz-message does not cut the roster to the slice's legs (--units)")
	}
	return errs
}

// TestGooseFuzzPartsAreTheWholeGate: the two verify-goose-fuzz parts run every
// pass of _verify-goose-fuzz, the unit passes over both slices; the whole
// roster at the full mutant count runs nightly (a registry row of tier
// nightly runs make verify-goose-fuzz-whole, whose one prerequisite is
// verify-goose-fuzz).
func TestGooseFuzzPartsAreTheWholeGate(t *testing.T) {
	t.Parallel()
	mk := readRepoRel(t, "make/verify-vm.mk")
	if errs := gooseErrs(t, mk); len(errs) > 0 {
		t.Errorf("the Goose fuzz parts are not the whole gate: %s", strings.Join(errs, "; "))
	}
	if row, ok := registryMakeRows(t, repoRoot(t))["verify-goose-fuzz-whole"]; !ok || row.Tier != "nightly" {
		t.Errorf("no registry row of tier nightly runs the whole Goose fuzz gate (make verify-goose-fuzz-whole): %+v", row)
	}
	if !regexp.MustCompile(`(?m)^verify-goose-fuzz-whole: verify-goose-fuzz$`).MatchString(mk) {
		t.Error("verify-goose-fuzz-whole is not exactly the whole gate (its one prerequisite verify-goose-fuzz)")
	}
	// PLANTED: part 2 without the retain pass.
	planted := strings.Replace(mk, "\t\t_verify-goose-fuzz-plain _verify-goose-fuzz-retain _verify-goose-fuzz-message \\\n\t\tGOOSE_FUZZ_SLICE=2/2", "\t\t_verify-goose-fuzz-plain _verify-goose-fuzz-message \\\n\t\tGOOSE_FUZZ_SLICE=2/2", 1)
	if planted == mk {
		t.Fatal("the planted control did not apply")
	}
	if len(gooseErrs(t, planted)) == 0 {
		t.Error("PLANTED CONTROL FAILED: part 2 without the retain pass still read as the whole gate")
	}
	// PLANTED: the message pass asks the whole roster under a slice.
	planted = strings.Replace(mk, gooseSliceUnits, "", 1)
	if planted == mk {
		t.Fatal("the planted --units control did not apply")
	}
	if len(gooseErrs(t, planted)) == 0 {
		t.Error("PLANTED CONTROL FAILED: a sliced message pass over the whole roster still read as the whole gate")
	}
}
