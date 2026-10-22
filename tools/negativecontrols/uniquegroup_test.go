package main

import (
	"strings"
	"testing"
)

// TestEveryControlIsInExactlyOneGroup holds the checked-in plan to the rule the
// CI matrix rests on: one control belongs to exactly one group. A target named
// twice runs twice in one matrix and is counted twice in the leg, and the group
// it lands in is whatever a reader happens to see first. The names on failure
// are the control and every group that holds it.
func TestEveryControlIsInExactlyOneGroup(t *testing.T) {
	t.Parallel()
	m, err := loadManifest(testRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Groups) == 0 {
		t.Fatal("the plan carries no groups: this test would pass over an empty set")
	}
	groups := map[string][]string{}
	var order []string
	for _, g := range m.Groups {
		for _, target := range g.Targets {
			if _, seen := groups[target]; !seen {
				order = append(order, target)
			}
			groups[target] = append(groups[target], g.Name)
		}
	}
	if len(order) == 0 {
		t.Fatal("the plan carries no controls: this test would pass over an empty set")
	}
	for _, target := range order {
		if len(groups[target]) != 1 {
			t.Errorf("%s is named in %d groups (%s): a control belongs to exactly one group",
				target, len(groups[target]), strings.Join(groups[target], ", "))
		}
	}
}

// TestTheCheckerRefusesAControlInTwoGroups is the reversed witness for the test
// above. The in-memory plan plants cards-conformance-negative-control in two
// groups, and the production checker that the tool's check command runs must
// refuse it, naming the control and both groups. The same plan with the
// duplicate removed must pass, so the witness is red on the planted manifest
// and green on the fixed one.
func TestTheCheckerRefusesAControlInTwoGroups(t *testing.T) {
	t.Parallel()
	const control = "cards-conformance-negative-control"

	planted := Manifest{Groups: []Group{
		{Name: "base", When: whenPullRequest, Targets: []string{control, "other-negative-control"}},
		{Name: "base-cards", When: whenPullRequest, Targets: []string{control}},
	}}
	covered, err := planted.covered()
	if err == nil {
		t.Fatalf("the checker accepted %s in both base and base-cards", control)
	}
	if covered != nil {
		t.Fatalf("the checker returned covered targets %v beside its refusal", covered)
	}
	for _, want := range []string{control, "base", "base-cards"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}

	fixed := Manifest{Groups: []Group{
		{Name: "base", When: whenPullRequest, Targets: []string{"other-negative-control"}},
		{Name: "base-cards", When: whenPullRequest, Targets: []string{control}},
	}}
	covered, err = fixed.covered()
	if err != nil {
		t.Fatalf("the checker refused the plan with the duplicate removed: %v", err)
	}
	if len(covered) != 2 {
		t.Fatalf("the fixed plan covers %d controls, want 2: %v", len(covered), covered)
	}
}
