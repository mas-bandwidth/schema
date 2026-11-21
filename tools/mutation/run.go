package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The four things that can happen to a mutant.
const (
	outcomeKilled   = "killed"       // a stage went red: the tests noticed
	outcomeSurvived = "survived"     // every stage stayed green: nothing noticed
	outcomeBroken   = "build-broken" // the mutant does not compile: it tested nothing
	outcomeTimeout  = "timeout"      // a stage outran its bound: counted with the killed, named apart
)

// Result is one mutant's outcome.
type Result struct {
	Mutant
	Scope   string `json:"scope"`
	Outcome string `json:"outcome"`
	// Stage is the stage that killed the mutant, or the last one it survived.
	Stage string `json:"stage"`
	// Stages are the stages the mutant was run through, in order.
	Stages  []string `json:"stages"`
	Seconds float64  `json:"seconds"`
	// Tail is the end of the killing stage's output.
	Tail string `json:"tail,omitempty"`
}

// Exec runs one shell command at dir under a time limit. It is an argument so
// the sweep's own tests run no process and read no clock.
type Exec func(dir, sh string, limit time.Duration) (out []byte, ok, timedOut bool, took time.Duration)

// sweepOptions are the choices of one run.
type sweepOptions struct {
	// Deep runs the stages that cost minutes each.
	Deep bool
	// Only, when set, names the stages to run, and overrides Deep.
	Only []string
	// BaselineLimit bounds a stage on the unmutated tree.
	BaselineLimit time.Duration
}

var buildFailed = regexp.MustCompile(`\[build failed\]|\[setup failed\]`)

// sweep runs the mutants of one scope through its stages in the scratch tree.
//
// Every stage is first run on the unmutated tree and must pass and show its
// proof: a stage that is red before any mutation, or that passes without
// testing, would make every verdict below it meaningless, so the sweep
// refuses to start instead. The same stages are run again after the last
// mutant, and a red there voids the run: the tree did not come back.
//
// The sweep fails if it applied no mutant or ran no stage. A mutation driver
// that did nothing reports no survivors, which reads exactly like success.
func sweep(scope Scope, tree string, muts []Mutant, opt sweepOptions, run Exec, log io.Writer, emit func(Result)) error {
	stages, err := scope.stagesFor(opt)
	if err != nil {
		return err
	}
	if len(muts) == 0 {
		return fmt.Errorf("scope %s: no mutant to apply: a sweep that mutates nothing proves nothing", scope.Name)
	}
	limits := make([]time.Duration, len(stages))
	for i, st := range stages {
		took, err := baseline(scope, st, tree, opt.BaselineLimit, run)
		if err != nil {
			return err
		}
		limits[i] = 3*took + 2*time.Minute
		_, _ = fmt.Fprintf(log, "baseline %s/%s green in %.0fs\n", scope.Name, st.Name, took.Seconds())
	}
	build := "go build " + strings.Join(scope.goPackages(), " ")
	if out, ok, _, _ := run(tree, build, opt.BaselineLimit); !ok {
		return fmt.Errorf("scope %s: %q fails on the unmutated tree:\n%s", scope.Name, build, tail(out))
	}

	applied, stagesRun := 0, 0
	for _, m := range muts {
		path := filepath.Join(tree, filepath.FromSlash(m.File))
		original, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mutated, err := apply(original, m)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, mutated, info.Mode().Perm()); err != nil {
			return err
		}
		applied++
		res := Result{Mutant: m, Scope: scope.Name, Outcome: outcomeSurvived}
		var seconds float64
		if strings.HasSuffix(m.File, ".go") {
			out, ok, _, took := run(tree, build, opt.BaselineLimit)
			seconds += took.Seconds()
			if !ok {
				res.Outcome, res.Stage, res.Tail = outcomeBroken, "build", tail(out)
			}
		}
		if res.Outcome == outcomeSurvived {
			for i, st := range stages {
				out, ok, timedOut, took := run(tree, st.Sh, limits[i])
				stagesRun++
				seconds += took.Seconds()
				res.Stage = st.Name
				res.Stages = append(res.Stages, st.Name)
				if timedOut {
					res.Outcome, res.Tail = outcomeTimeout, tail(out)
					break
				}
				if !ok {
					res.Outcome, res.Tail = outcomeKilled, tail(out)
					if buildFailed.Match(out) {
						res.Outcome = outcomeBroken
					}
					break
				}
			}
		}
		res.Seconds = seconds
		if err := os.WriteFile(path, original, info.Mode().Perm()); err != nil {
			return fmt.Errorf("mutant %s could not be reverted in %s, the scratch tree is spoiled: %w", m.ID, m.File, err)
		}
		back, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(back, original) {
			return fmt.Errorf("mutant %s was not reverted in %s, the scratch tree is spoiled", m.ID, m.File)
		}
		_, _ = fmt.Fprintf(log, "%-12s %s %s:%d %s %q -> %q (%s, %.0fs)\n", res.Outcome, m.ID, m.File, m.Line, m.Op, clip(m.Old), clip(m.New), res.Stage, seconds)
		emit(res)
	}
	if applied == 0 {
		return fmt.Errorf("scope %s: applied no mutation", scope.Name)
	}
	if stagesRun == 0 {
		return fmt.Errorf("scope %s: %d mutants applied and no test stage ran: every one failed to build, so no test was asked anything", scope.Name, applied)
	}
	for _, st := range stages {
		if _, err := baseline(scope, st, tree, opt.BaselineLimit, run); err != nil {
			return fmt.Errorf("after the last mutant was reverted, and so every verdict of this run is void: %w", err)
		}
	}
	return nil
}

// baseline runs a stage on the unmutated tree, and holds it to green and to
// its proof.
func baseline(scope Scope, st Stage, tree string, limit time.Duration, run Exec) (time.Duration, error) {
	proof, err := regexp.Compile(st.Proof)
	if err != nil || st.Proof == "" {
		return 0, fmt.Errorf("scope %s stage %s: a stage needs a proof that it tested, and %q is not one", scope.Name, st.Name, st.Proof)
	}
	out, ok, timedOut, took := run(tree, st.Sh, limit)
	switch {
	case timedOut:
		return 0, fmt.Errorf("scope %s stage %s: the unmutated tree did not finish %q within %s:\n%s", scope.Name, st.Name, st.Sh, limit, tail(out))
	case !ok:
		return 0, fmt.Errorf("scope %s stage %s: the unmutated tree is red under %q, so the stage can tell a mutant nothing:\n%s", scope.Name, st.Name, st.Sh, tail(out))
	case !proof.Match(out):
		return 0, fmt.Errorf("scope %s stage %s: %q passed and its output does not match %q: it tested nothing:\n%s", scope.Name, st.Name, st.Sh, st.Proof, tail(out))
	}
	return took, nil
}

// stagesFor is the scope's stages under the options, and an error when that
// is none, or when a named stage does not exist.
func (s *Scope) stagesFor(opt sweepOptions) ([]Stage, error) {
	var out []Stage
	if len(opt.Only) > 0 {
		for _, name := range opt.Only {
			found := false
			for _, st := range s.Stages {
				if st.Name == name {
					out = append(out, st)
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("scope %s has no stage %q", s.Name, name)
			}
		}
		return out, nil
	}
	for _, st := range s.Stages {
		if !st.Deep || opt.Deep {
			out = append(out, st)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("scope " + s.Name + " has no stage to run")
	}
	return out, nil
}

func (s *Scope) goPackages() []string {
	var out []string
	for _, d := range s.Go {
		out = append(out, "./"+d+"/...")
	}
	return out
}

// tail is the end of a command's output, enough to see why it failed.
func tail(out []byte) string {
	const keep = 3000
	if len(out) > keep {
		out = out[len(out)-keep:]
		if i := bytes.IndexByte(out, '\n'); i >= 0 {
			out = out[i+1:]
		}
	}
	return string(out)
}

func clip(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
