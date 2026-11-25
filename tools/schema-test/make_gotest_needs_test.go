package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// TestAMakeGoTestRowBuildsWhatItsTestReadsInPrepare: a make-gotest row whose
// Go test reads a build (test/registry/go-needs.txt) has it made as a prepare
// step named by its target, before the target's call, each target once,
// through the run's builder; a row whose test reads nothing adds no step. A
// build that fails is the step's error, naming the row and the target, so
// the group's rows fail by name and make never runs. Planted: with no
// PrepareSteps the corpus is made inside the call (tables-c-versioning's
// 15 s), and this test is red.
func TestAMakeGoTestRowBuildsWhatItsTestReadsInPrepare(t *testing.T) {
	t.Parallel()
	root := needsRoot(t)
	row := func(test string) testreg.Instance {
		return testreg.Instance{Full: "none.functional.evolution.g." + test, Test: testreg.Test{Inputs: []byte(`{"file":"make/x.mk","target":"gate-check","package":"./g","test":"` + test + `"}`)}}
	}
	var built []string
	a := newMakeGoTestAdapterBuilding(root, func(_ context.Context, argv []string) error {
		built = append(built, strings.Join(argv, " "))
		return nil
	})
	var prep testreg.StepPreparer = a
	steps := prep.PrepareSteps("", []testreg.Instance{row("TestReads"), row("TestFree"), row("TestReads")})
	var names []string
	for _, s := range steps {
		names = append(names, s.Name)
		if err := s.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(names, ",") != "corpus" || strings.Join(built, ",") != "make corpus" {
		t.Fatalf("steps %v, built %v; want the one step corpus, built by make corpus", names, built)
	}
	if steps := prep.PrepareSteps("", []testreg.Instance{row("TestFree")}); len(steps) != 0 {
		t.Fatalf("a row whose test reads nothing has steps %v", steps)
	}

	failing := newMakeGoTestAdapterBuilding(root, func(context.Context, []string) error { return errors.New("exit status 2") })
	err := failing.Prepare(context.Background(), "", []testreg.Instance{row("TestReads")})
	if err == nil || !strings.Contains(err.Error(), "none.functional.evolution.g.TestReads") || !strings.Contains(err.Error(), "make corpus") {
		t.Fatalf("a failed build: err %v, want it naming the row and make corpus", err)
	}
	if err := newMakeGoTestAdapter(root).Prepare(context.Background(), "", []testreg.Instance{row("TestReads")}); err == nil || !strings.Contains(err.Error(), "no builder") {
		t.Fatalf("an adapter with no builder: err %v, want it named", err)
	}
}
