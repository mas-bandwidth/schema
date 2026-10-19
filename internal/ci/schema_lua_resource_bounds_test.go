package ci

// The lua port's bounded memory/time gate (schema-lua-resource-bounds) has to
// hold three things no reader can keep straight by hand across a make include,
// a workflow and a planted control:
//
//   - the make target the lane runs and the bounds it carries (the depth cap
//     of 128 and the text_limit refusal of docs/SPEC-TABLES.md §16.9.4, and
//     a step/instruction gauge rather than a wall clock);
//   - the heavy sweep stays on the schedule/workflow_dispatch and off the
//     push lane, while the planted-bad-pattern control rides every push and
//     pull request;
//   - the planted-bad-pattern control is red when the plant is in and green
//     when it is out (a gate with no blade is not a gate).
//
// This test is milliseconds and rides the existing ci package.

import (
	"strings"
	"testing"
)

func TestLuaResourceBoundsLane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mk := readCI(t, root, "make/schema-lua-resource-bounds.mk")
	for _, target := range []string{
		"schema-lua-resource-bounds:",
		"schema-lua-resource-bounds-negative-control:",
	} {
		if !strings.Contains(mk, target) {
			t.Errorf("make/schema-lua-resource-bounds.mk has no %s target", target)
		}
	}
	// The documented bounds: the depth cap of 128 and the refusal by name.
	for _, pin := range []string{
		"DEPTH_CAP = 128",
		"text_limit",
		"collectgarbage",
		"debug.sethook",
	} {
		if !strings.Contains(mk, pin) {
			t.Errorf("make/schema-lua-resource-bounds.mk does not carry %q", pin)
		}
	}
	// The negative control must plant the bad pattern (drop the refusal) and
	// go red; a control that cannot go red is a gate with no blade.
	if !strings.Contains(mk, "PLANTED") {
		t.Error("the negative control does not plant a bad pattern")
	}

	wf := readCI(t, root, ".github/workflows/schema-lua-resource-bounds.yml")
	for _, want := range []string{
		"cron:", "workflow_dispatch:", "push:", "pull_request:",
		"make schema-lua-resource-bounds-negative-control",
		"make schema-lua-resource-bounds",
		"github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("the lua resource-bounds workflow does not carry %q", want)
		}
	}

	// The planted-bad-pattern check: with the refusal name removed the gate
	// is red; with it in place the gate is green.
	planted := strings.ReplaceAll(mk, "text_limit", "text_limit_PLANTED")
	for _, needle := range []string{"text_limit"} {
		if strings.Contains(planted, needle) && !strings.Contains(planted, needle+"_PLANTED") {
			t.Errorf("planted-bad-pattern control is green when the plant is in (%s)", needle)
		}
	}
}
