package compiler

import (
	"strings"
	"testing"
)

// carryOverrides lists the targets that carry a construct besides Go. The
// central refusal for `forms` is lifted for "cpp" (docs/SPEC-TABLES.md §2.12):
// cpp registers carryForms in carries_cpp.go, so it drops out of the refusal
// tests by the carrier set rather than by an edit to a hardcoded list.
var carryOverrides = map[string]map[string]bool{
	"cpp": {carryForms: true},
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
			if target != "go" && carries(target, construct) && !carryOverrides[target][construct] {
				t.Errorf("target %q unexpectedly carries %q", target, construct)
			}
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
			if target != "go" && !carryOverrides[target][construct] {
				t.Errorf("target %q carries %q; only Go and the registered carriers carry constructs at this stage", target, construct)
			}
		}
	}
	if !panics(func() { registerCarries("carry-test-unknown", "unknown") }) {
		t.Error("registerCarries accepted an unknown construct key")
	}
	if !panics(func() { registerCarries("go", carryTimestamp) }) {
		t.Error("registerCarries accepted a second registration for go")
	}

	cases := []struct {
		construct string
		schema    string
		refusal   string
	}{
		{carryTimestamp, timestampUnit, "timestamp"},
		{carryRequired, "package probe\ntable Ship\n{\n    name string(32) | required\n}\n", "refuses | required"},
		{carryForms, "package probe\ntable Shape | forms = \"wire\"\n{\n    x int32\n}\n", "forms on table Shape"},
		{carryIntegrity, "package probe\ntable Ledger | integrity = \"crc32c\"\n{\n    x int32\n}\n", "integrity on table Ledger"},
		{carryUnboundedText, "package probe\ntable Where\n{\n    rules []string\n}\n", "unbounded text-list element on field Where.rules"},
		{carryTextListTotal, "package probe\ntable L\n{\n    names [..8]string(8) | total = 12\n}\n", "text list total on field L.names"},
		{carryTextList, "package probe\ntable Lists\n{\n    rules [..4]string(8)\n}\n", "text list on field Lists.rules"},
		{carryArrayRoot, "package probe\ntable Goal\n{\n    x int32\n}\ntable GoalList | json = \"array\"\n{\n    goals []Goal\n}\n", `json = "array" on table GoalList`},
		{carryOneSidedRange, "package probe\ntable OneSided\n{\n    a int64 | min = 0\n}\n", "OneSided.a"},
		{carryOptionalVariableClosure, "package probe\ntable Inner\n{\n    text *string\n}\ntable Outer\n{\n    inner ?Inner\n}\n", "optional over a variable closure on field Outer.inner"},
		{carryOptionalMap, "package probe\ntable M\n{\n    tags ?map[string(8)]int32\n}\n", "optional map on field M.tags"},
	}
	c := New()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.construct, func(t *testing.T) {
			u := unitFromSource(t, tc.schema)
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
}

func panics(fn func()) (did bool) {
	defer func() { did = recover() != nil }()
	fn()
	return false
}
