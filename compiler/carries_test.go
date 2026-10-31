package compiler

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// nonGoCarriers lists the constructs the non-Go targets carry: C, C#, C++,
// Dart and Java carry optional-variable-closure alongside Go (docs/SPEC-TABLES.md §2.3);
// C++ also carries the timestamp kind (§2.11), the JSON array root (§16.10),
// the one-sided integer range (§4.3) and ?map[K]V (§2.8); Lobster carries
// optional-variable-closure, the timestamp kind, the one-sided integer range
// and the text lists (docs/PORT-LOBSTER.md); Luau carries every construct but
// integrity (docs/SPEC-TABLES.md §2.14); Squirrel carries the constructs its
// compiler/target_squirrel_*.go files register; every other target
// carries none at this stage.
var nonGoCarriers = map[string][]string{
	"c":    {carryOptionalVariableClosure},
	"cs":   {carryOptionalVariableClosure},
	"cpp":  {carryOptionalVariableClosure, carryTimestamp, carryArrayRoot, carryOneSidedRange, carryOptionalMap},
	"dart": {carryOptionalVariableClosure},
	"java": {carryOptionalVariableClosure},
	"luau": {carryTimestamp, carryRequired, carryForms, carryUnboundedText, carryTextListTotal, carryTextList, carryArrayRoot, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap},
	// moo (behind schema_leg_moo; compiler/carries_moo.go): each proven by
	// internal/codegen/mootable's TestMooTableConstructsMatchTheGoEngine
	"moo":      {carryTimestamp, carryIntegrity, carryTextList, carryTextListTotal, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap},
	"lobster":  {carryOptionalVariableClosure, carryTimestamp, carryOneSidedRange, carryTextList, carryTextListTotal, carryUnboundedText, carryRequired, carryOptionalMap},
	"squirrel": {carryTimestamp, carryTextList, carryArrayRoot, carryOptionalVariableClosure, carryRequired, carryOptionalMap, carryForms, carryIntegrity, carryOneSidedRange, carryUnboundedText}, // docs/SPEC-TABLES.md §2.3, §2.8, §2.11, §2.12, §2.13, §2.13.1, §2.14, §4.3, §16.2, §16.10
}

func TestEachTargetDeclaresWhatItCarries(t *testing.T) {
	t.Parallel()
	targets := New().Targets()
	if len(targetCarries) != len(targets) {
		t.Fatalf("carry registrations %v do not match targets %v", targetCarries, targets)
	}
	for _, target := range targets {
		if _, ok := targetCarries[target]; !ok {
			t.Errorf("target %q has no carry registration", target)
		}
	}
	if got := len(targetCarries["go"]); got != len(carryConstructs) {
		t.Errorf("Go carries %d constructs, want all %d: %v", got, len(carryConstructs), targetCarries["go"])
	}
	for _, construct := range carryConstructs {
		if !carries("go", construct) {
			t.Errorf("Go does not carry %q", construct)
		}
		for _, target := range targets {
			if target == "go" {
				continue
			}
			want := slices.Contains(nonGoCarriers[target], construct)
			if carries(target, construct) != want {
				t.Errorf("target %q carries %q = %v, want %v", target, construct, carries(target, construct), want)
			}
		}
	}
	// Registration comes only through registerCarries in carries_<target>.go,
	// so the checks above cover every construct; no other file writes the
	// registry directly.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "carries.go" {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "targetCarries[") {
			t.Errorf("%s writes targetCarries directly; registration belongs in carries_<target>.go through registerCarries", name)
		}
	}
	known := make(map[string]bool, len(carryConstructs))
	for _, construct := range carryConstructs {
		known[construct] = true
	}
	for target, constructs := range targetCarries {
		for construct := range constructs {
			if !known[construct] {
				t.Errorf("target %q registers unknown construct %q", target, construct)
			}
			if target != "go" && !slices.Contains(nonGoCarriers[target], construct) {
				t.Errorf("target %q carries %q without a registration in nonGoCarriers", target, construct)
			}
		}
	}
	if !panics(func() { registerCarries("carry-test-unknown", "unknown") }) {
		t.Error("registerCarries accepted an unknown construct key")
	}
	if !panics(func() { registerCarries("go", carryTimestamp) }) {
		t.Error("registerCarries accepted a second registration for go")
	}

	cases := []carriesRow{
		{carryTimestamp, timestampUnit, "timestamp"},
		{carryRequired, "package probe\ntable Ship\n{\n    name string(32) | required\n}\n", "refuses | required"},
		{carryForms, "package probe\ntable Shape | forms = \"wire\"\n{\n    x int32\n}\n", "forms on table Shape"},
		{carryIntegrity, "package probe\ntable Ledger | integrity = \"crc32c\"\n{\n    x int32\n}\n", "integrity on table Ledger"},
		{carryUnboundedText, "package probe\ntable Where\n{\n    rules []string\n}\n", "unbounded text-list element on field Where.rules"},
		{carryTextListTotal, "package probe\ntable L\n{\n    names [..8]string(8) | total = 12\n}\n", "text list total on field L.names"},
		{carryTextList, "package probe\ntable Lists\n{\n    rules [..4]string(8)\n}\n", "text list on field Lists.rules"},
		{carryArrayRoot, "package probe\ntable Goal\n{\n    x int32\n}\ntable GoalList | json = \"array\"\n{\n    goals [..4]Goal\n}\n", `json = "array" on table GoalList`},
		{carryOneSidedRange, "package probe\ntable OneSided\n{\n    a int64 | min = 0\n}\n", "OneSided.a"},
		{carryOptionalVariableClosure, "package probe\ntable Leaf\n{\n    n int32\n}\ntable Inner\n{\n    child *Leaf\n}\ntable Outer\n{\n    inner ?Inner\n}\n", "optional over a variable closure on field Outer.inner"},
		{carryOptionalMap, "package probe\ntable M\n{\n    tags ?map[string(8)]int32\n}\n", "optional map on field M.tags"},
	}
	c := New()
	for _, tc := range carriesTable(cases) {
		t.Run(tc.construct, func(t *testing.T) {
			u := unitFromSource(t, tc.schema)
			// THE QUERY THE CONFORMANCE HARNESS CLAIMS BY names the same
			// construct the refusal below does (test/conformance/README.md,
			// "The nightly")
			if uses := UnitConstructs(u); !slices.Contains(uses, tc.construct) {
				t.Errorf("UnitConstructs = %v, does not name %q", uses, tc.construct)
			}
			for _, target := range targets {
				files, err := c.Generate(u, target, Options{"runtime": "standalone"})
				if carries(target, tc.construct) {
					if err != nil || len(files) == 0 {
						t.Errorf("target %q must generate the %q construct: files=%d err=%v", target, tc.construct, len(files), err)
					}
					continue
				}
				if err == nil {
					t.Errorf("target %q accepted %q", target, tc.construct)
					continue
				}
				if !strings.Contains(err.Error(), tc.refusal) || !strings.Contains(err.Error(), target) {
					t.Errorf("target %q refusal for %q does not name the construct: %v", target, tc.construct, err)
				}
			}
		})
	}
	seen := map[carriesRow]bool{}
	for _, tc := range carriesTable(cases) {
		if seen[tc] {
			t.Errorf("carries row %q is registered twice", tc.construct)
		}
		seen[tc] = true
	}
}

func TestRegisterCarriedAddsToAnExistingTarget(t *testing.T) {
	t.Parallel()
	registry := map[string]map[string]struct{}{
		"squirrel": {},
	}
	saved := len(targetCarries)

	registerCarriedIn(registry, "squirrel", carryRequired)
	registerCarriedIn(registry, "squirrel", carryTimestamp)
	if _, ok := registry["squirrel"][carryRequired]; !ok {
		t.Errorf("squirrel does not carry %q after registration", carryRequired)
	}
	if _, ok := registry["squirrel"][carryTimestamp]; !ok {
		t.Errorf("squirrel does not carry %q after registration", carryTimestamp)
	}
	if !panics(func() { registerCarriedIn(registry, "squirrel", carryRequired) }) {
		t.Error("registerCarriedIn accepted a construct registered twice")
	}
	if !panics(func() { registerCarriedIn(registry, "no-such-target", carryRequired) }) {
		t.Error("registerCarriedIn accepted an unregistered target")
	}
	if !panics(func() { registerCarriedIn(registry, "", carryRequired) }) {
		t.Error("registerCarriedIn accepted an empty target")
	}
	if !panics(func() { registerCarriedIn(registry, "squirrel", "unknown") }) {
		t.Error("registerCarriedIn accepted an unknown construct")
	}
	if len(targetCarries) != saved {
		t.Errorf("package-level targetCarries changed: got %d entries, want %d", len(targetCarries), saved)
	}
}

func panics(fn func()) (did bool) {
	defer func() { did = recover() != nil }()
	fn()
	return false
}

// TestCarriedQueries holds the exported reads to the registry: Carried answers
// a registration exactly, a target with none says so rather than reading as
// "carries nothing", and a unit holding no registered construct names none.
func TestCarriedQueries(t *testing.T) {
	t.Parallel()
	if got := CarryConstructs(); !slices.Equal(got, carryConstructs) {
		t.Errorf("CarryConstructs = %v, want %v", got, carryConstructs)
	}
	got, ok := Carried("go")
	if !ok || !slices.Equal(got, carryConstructs) {
		t.Errorf("Carried(go) = %v, %v; want every construct", got, ok)
	}
	got, ok = Carried("cpp")
	if !ok || !slices.Equal(got, []string{carryTimestamp, carryArrayRoot, carryOneSidedRange, carryOptionalVariableClosure, carryOptionalMap}) {
		t.Errorf("Carried(cpp) = %v, %v", got, ok)
	}
	if got, ok := Carried("carry-test-unregistered"); ok || got != nil {
		t.Errorf("Carried(unregistered) = %v, %v; want no registration", got, ok)
	}
	plain := unitFromSource(t, "package probe\ntable Plain\n{\n    x int32\n}\n")
	if uses := UnitConstructs(plain); len(uses) != 0 {
		t.Errorf("UnitConstructs(plain) = %v, want none", uses)
	}
}
