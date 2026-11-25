package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// THE BUILDS A GO TEST READS (testreg.GoNeedsPath). A Go test that opens a
// build output (the fixed-form corpus, the all-port differential's answers)
// has it built first, by its declared make target, through the run's builder:
// in Prepare (testreg.Preparer), before any call's deadline starts, and again
// before the call (the builder answers a step it ran at once). A build that
// fails fails exactly the rows whose tests read it, by name, and their go
// test does not run; the other rows run.

// goTestRef is one Go test: its package and top-level name.
type goTestRef struct{ pkg, test string }

// needs is the needs table, read once from the adapter's root.
func (a *goTestAdapter) needs() ([]testreg.GoNeed, error) {
	a.needsOnce.Do(func() {
		a.needsTable, a.needsErr = testreg.LoadGoNeeds(filepath.Join(a.root, testreg.GoNeedsPath))
	})
	return a.needsTable, a.needsErr
}

// builder runs one build step through the run's builder, or one of the
// adapter's own.
func (a *goTestAdapter) builder() func(context.Context, []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.build == nil {
		a.build = (&builder{log: a.log, done: map[string]error{}}).run
	}
	return a.build
}

// needsStep is the make command that builds target: the argv a row's own
// "make" input runs (goTestAdapter.Prepare), so the run's builder makes a
// target either names once.
func needsStep(target string) []string {
	return []string{"make", target}
}

// buildNeeds builds every target the refs read, each once, and returns the
// failed ones by target. Only a needs table that cannot be read is an error.
func (a *goTestAdapter) buildNeeds(ctx context.Context, refs []goTestRef) (map[string]error, error) {
	needs, err := a.needs()
	if err != nil {
		return nil, err
	}
	run := a.builder()
	failed := map[string]error{}
	seen := map[string]bool{}
	for _, r := range refs {
		for _, target := range testreg.GoNeedsOf(needs, r.pkg, r.test) {
			if seen[target] {
				continue
			}
			seen[target] = true
			if err := run(ctx, needsStep(target)); err != nil {
				failed[target] = err
			}
		}
	}
	return failed, nil
}

// needsFailure is why a test's declared build failed, or "" when every one
// it reads was built.
func (a *goTestAdapter) needsFailure(failed map[string]error, r goTestRef) string {
	needs, err := a.needs()
	if err != nil {
		return ""
	}
	for _, target := range testreg.GoNeedsOf(needs, r.pkg, r.test) {
		if err := failed[target]; err != nil {
			return fmt.Sprintf("the build %s %s reads (%s) failed: %s", r.pkg, r.test, target, buildFailure(err))
		}
	}
	return ""
}

// prepareNeeds builds what the rows' Go tests read, for Prepare
// (testreg.Preparer). A build that fails is not Prepare's error: Run fails
// the rows that read it, by name, and the rest of the group runs.
func (a *goTestAdapter) prepareNeeds(ctx context.Context, instances []testreg.Instance) error {
	var refs []goTestRef
	for _, inst := range instances {
		in, err := goTestInputsOf(inst.Test)
		if err != nil {
			continue // Run fails the row, naming its inputs
		}
		refs = append(refs, goTestRef{in.Package, in.Test})
	}
	if _, err := a.buildNeeds(ctx, refs); err != nil {
		return err
	}
	// a build its deadline cut short is the deadline's, named as such by the
	// run, never a build failure answered from the builder's record
	return ctx.Err()
}

// unionRefs is the Go tests a language's union instances run.
func unionRefs(lang string, instances []testreg.Instance) []goTestRef {
	var refs []goTestRef
	for _, inst := range instances {
		tests, ok, err := testreg.GoTestsOf(inst.Test)
		if !ok || err != nil {
			continue
		}
		if g, has := tests[lang]; has {
			refs = append(refs, goTestRef{g.Package, g.Test})
		}
	}
	return refs
}

// builderKey carries the corpus adapter's builder to the union harness's
// Run, so the builds Prepare ran answer there at once.
type builderKey struct{}

func withBuilder(ctx context.Context, b *builder) context.Context {
	return context.WithValue(ctx, builderKey{}, b)
}

func builderFrom(ctx context.Context) *builder {
	b, _ := ctx.Value(builderKey{}).(*builder)
	return b
}

// buildFailure is a failed build's first line and, when the build printed
// one, its last: the step and its exit, then make's own reason.
func buildFailure(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	if len(lines) == 1 {
		return lines[0]
	}
	return lines[0] + ": " + strings.TrimSpace(lines[len(lines)-1])
}
