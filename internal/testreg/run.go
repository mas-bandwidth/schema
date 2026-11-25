package testreg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"
)

// Adapter runs one language's instances. It is thin: it is handed the
// instances, builds and invokes that language's code, and returns one Result
// per instance. It holds no case list and no skip logic: what to run comes
// from the registry, and an instance it cannot run is a failure the runner
// names, never one it leaves out.
type Adapter interface {
	// Answers reports whether the adapter has a harness for the test (in
	// practice, for its family). A test it does not answer, in a language
	// with no unrepresentable declaration for it, is MISSING.
	Answers(t Test) bool
	// Run runs the instances, all of one language, and returns a result
	// for each. An error is a crash; ctx carries the deadline.
	Run(ctx context.Context, lang string, instances []Instance) ([]Result, error)
}

// Preparer is an adapter whose calls need something built first (a leg's
// driver, a harness). The runner calls Prepare for every group, one after
// another, before any call starts and outside every call's deadline: a cold
// checkout's build is the run's cost, paid once, and never eats the deadline
// of the call that happens to need it first. Each Prepare has a deadline of
// its own, the limit a call has (Runner.Context, group "prepare <group>"),
// starting when it starts: a build that outlasts it fails its group by name. A shard pays it whole,
// and its seconds are shared over its group's rows (the rows' weights), so a
// cold build is in what the sharder cuts by.
type Preparer interface {
	Prepare(ctx context.Context, lang string, instances []Instance) error
}

// Serial is an adapter whose calls take the checkout (make, a writer, a
// plant's runs): its calls would only wait for each other, so the runner runs
// every Serial group one after another, each call's deadline starting when
// that call starts, never while it waits behind another's work. The sharder
// already weighs a shard as the sum of its rows' seconds, which is what
// running them one after another costs.
type Serial interface {
	Serial() bool
}

// Verdicts an adapter may report.
const (
	Pass = "pass"
	Fail = "fail"
)

// Result is the one shared result format: the instance's full name, its
// verdict (pass or fail) and, for a fail, the reason.
type Result struct {
	Instance string `json:"instance"`
	Verdict  string `json:"verdict"`
	Reason   string `json:"reason,omitempty"`
	// Seconds is how long the instance took, when its adapter measures it
	// (the gotest adapter reads go test's own clock).
	Seconds float64 `json:"seconds,omitempty"`
}

// Outcome is how one instance (or one stray result) ended in a run.
type Outcome struct {
	Full    string
	Status  string // PASS, FAIL, MISSING, UNREPRESENTABLE, UNASKED
	Reason  string
	Seconds float64 // as the adapter measured it, or 0
}

// Statuses an Outcome can have.
const (
	StatusPass    = "PASS"
	StatusFail    = "FAIL"
	StatusMissing = "MISSING"
	StatusUnrep   = "UNREPRESENTABLE"
	StatusUnasked = "UNASKED"
)

// Report is a run's whole account. Expected counts every selected instance
// that is not declared unrepresentable; Ran counts those an adapter
// reported. A run is green only when nothing failed, nothing is missing,
// no adapter reported a stray result, and Expected == Ran == Passed > 0.
type Report struct {
	Expected, Ran, Passed, Failed, Missing, Unrep int
	Outcomes                                      []Outcome
	Problems                                      []string // run-level failures: expected != ran, stray results
}

// OK reports whether the run is green.
func (r *Report) OK() bool {
	return len(r.Problems) == 0 && r.Failed == 0 && r.Missing == 0 && r.Expected == r.Ran && r.Passed == r.Expected && r.Expected > 0
}

// Summary is the counts line every run prints.
func (r *Report) Summary() string {
	return fmt.Sprintf("expected %d, ran %d, passed %d, failed %d, missing %d, unrepresentable %d",
		r.Expected, r.Ran, r.Passed, r.Failed, r.Missing, r.Unrep)
}

// Print writes every outcome, every run-level problem and the counts line.
func (r *Report) Print(out io.Writer) error {
	var w strings.Builder
	for _, o := range r.Outcomes {
		took := ""
		if o.Seconds > 0 {
			took = fmt.Sprintf(" (%.2fs)", o.Seconds)
		}
		if o.Reason != "" {
			fmt.Fprintf(&w, "%s %s%s: %s\n", o.Status, o.Full, took, o.Reason)
		} else {
			fmt.Fprintf(&w, "%s %s%s\n", o.Status, o.Full, took)
		}
	}
	for _, p := range r.Problems {
		fmt.Fprintf(&w, "FAIL run: %s\n", p)
	}
	verdict := "PASS"
	if !r.OK() {
		verdict = "FAIL"
	}
	fmt.Fprintf(&w, "schema-test %s: %s\n", verdict, r.Summary())
	_, err := io.WriteString(out, w.String())
	return err
}

// Runner runs instances through the adapters the registry names.
type Runner struct {
	Reg      *Registry
	Adapters map[string]Adapter // by the name a language row or a language-free test gives
	// Context gives the context one adapter call runs under, from the run's
	// parent; it carries the deadline. Time is an argument: a test hands in
	// a context whose deadline has already passed instead of waiting.
	Context func(parent context.Context, group string) (context.Context, context.CancelFunc)
	// Now, when set, is the clock the runner times each call and each
	// Prepare by: a row an adapter gives no seconds for is given its call's
	// time shared over the call's rows, and every row its share of its
	// group's Prepare, so the sharder weighs what a cold shard pays. Nil
	// measures nothing (time is an argument: a test hands in its own clock).
	Now func() time.Time
}

// clock is Now, or a stopped clock when there is none, so every time
// measured is 0.
func (rn *Runner) clock() time.Time {
	if rn.Now == nil {
		return time.Time{}
	}
	return rn.Now()
}

// WithTimeout is the usual Context: a deadline d from now, or none when d is 0.
func WithTimeout(d time.Duration) func(context.Context, string) (context.Context, context.CancelFunc) {
	return func(parent context.Context, _ string) (context.Context, context.CancelFunc) {
		if d == 0 {
			return context.WithCancel(parent)
		}
		return context.WithTimeout(parent, d)
	}
}

type envKey struct{}

// WithEnv returns a context whose adapter calls add env to their children's
// environment: a plant (a negative control) runs its target this way.
func WithEnv(ctx context.Context, env map[string]string) context.Context {
	merged := map[string]string{}
	maps.Copy(merged, EnvFrom(ctx))
	maps.Copy(merged, env)
	return context.WithValue(ctx, envKey{}, merged)
}

// EnvFrom is the extra environment a context carries (WithEnv).
func EnvFrom(ctx context.Context) map[string]string {
	m, _ := ctx.Value(envKey{}).(map[string]string)
	return m
}

// Plan is a selection's account before anything runs: what will run and
// what is MISSING. Every name selected is expected: it runs and passes or
// fails, and a MISSING one fails.
type Plan struct {
	Selected, ToRun, Missing int
	status                   map[string]Outcome
	groups                   []group
}

type group struct {
	name      string // the language, or "none/<adapter>"
	lang      string
	adapter   string
	instances []Instance
}

// String is the selection line every run prints before it runs.
func (p *Plan) String() string {
	return fmt.Sprintf("selected %d instances: %d to run, %d MISSING", p.Selected, p.ToRun, p.Missing)
}

// Plan classifies the instances: each is MISSING (no adapter, an adapter
// schema-test does not have, or one that does not answer it) or to run
// through its adapter.
func (rn *Runner) Plan(instances []Instance) *Plan {
	p := &Plan{Selected: len(instances), status: map[string]Outcome{}}
	idx := map[string]int{}
	for _, inst := range instances {
		if reason, ok := rn.Reg.Unrepresentable(inst.Test.Key, inst.Lang); ok {
			// Instances never yields one; a caller that builds one by hand
			// gets a failure, never a quiet pass
			p.status[inst.Full] = Outcome{Full: inst.Full, Status: StatusFail, Reason: "declared unrepresentable, so no such instance exists: " + reason}
			continue
		}
		name, adapter := inst.Lang, inst.Test.Adapter
		if inst.Test.PerLang() {
			l, _ := rn.Reg.Lang(inst.Lang)
			adapter = l.Adapter
		} else {
			name = LangFree + "/" + adapter
		}
		var missing string
		a, ok := rn.Adapters[adapter]
		switch {
		case adapter == "":
			missing = fmt.Sprintf("language %s has no adapter", inst.Lang)
		case !ok:
			missing = fmt.Sprintf("%s names adapter %q, which schema-test does not have", name, adapter)
		case !a.Answers(inst.Test):
			missing = fmt.Sprintf("adapter %s has no instance of %s and %s declares it no unrepresentable", adapter, inst.Test.Key, inst.Lang)
		}
		if missing != "" {
			p.status[inst.Full] = Outcome{Full: inst.Full, Status: StatusMissing, Reason: missing}
			p.Missing++
			continue
		}
		if u := callUnit(inst); u != "" {
			name += " (" + u + ")"
		}
		i, seen := idx[name]
		if !seen {
			i = len(p.groups)
			idx[name] = i
			p.groups = append(p.groups, group{name: name, lang: inst.Lang, adapter: adapter})
		}
		p.groups[i].instances = append(p.groups[i].instances, inst)
		p.ToRun++
	}
	return p
}

// Run runs the instances and accounts for every one of them.
func (rn *Runner) Run(parent context.Context, instances []Instance) *Report {
	plan := rn.Plan(instances)
	rep := &Report{Expected: plan.Selected}
	status := plan.status

	type groupRun struct {
		results []Result
		err     error
		seconds float64 // the call's own time, when the runner has a clock
	}
	runs := make([]groupRun, len(plan.groups))
	prepared, prepSeconds := rn.prepare(parent, plan.groups)
	call := func(gi int) {
		g := plan.groups[gi]
		if prepared[gi] != nil {
			runs[gi] = groupRun{nil, prepared[gi], 0}
			return
		}
		ctx, cancel := rn.Context(parent, g.name)
		defer cancel()
		start := rn.clock()
		res, err := rn.Adapters[g.adapter].Run(ctx, g.lang, g.instances)
		seconds := rn.clock().Sub(start).Seconds()
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
		switch {
		case timedOut && err == nil:
			err = fmt.Errorf("adapter %s timed out", g.adapter)
		case timedOut:
			err = fmt.Errorf("adapter %s timed out: %w", g.adapter, err)
		case err != nil:
			err = fmt.Errorf("adapter %s crashed: %w", g.adapter, err)
		}
		runs[gi] = groupRun{res, err, seconds}
	}
	var serial []int
	var wg sync.WaitGroup
	for gi, g := range plan.groups {
		if s, ok := rn.Adapters[g.adapter].(Serial); ok && s.Serial() {
			serial = append(serial, gi)
			continue
		}
		wg.Go(func() { call(gi) })
	}
	wg.Go(func() {
		for _, gi := range serial {
			call(gi)
		}
	})
	wg.Wait()

	for gi, g := range plan.groups {
		run := runs[gi]
		asked := map[string]bool{}
		for _, inst := range g.instances {
			asked[inst.Full] = true
		}
		// a row's seconds: its adapter's, else its share of what the call
		// took beyond the rows its adapter timed, shared over the asked rows
		// it did not time; and its share of its group's Prepare either way
		rest, n := run.seconds, 0
		for _, res := range run.results {
			switch {
			case !asked[res.Instance]:
			case res.Seconds > 0:
				rest -= res.Seconds
			default:
				n++
			}
		}
		share := 0.0
		if n > 0 && rest > 0 {
			share = rest / float64(n)
		}
		prep := prepSeconds[gi] / float64(len(g.instances))
		secondsOf := func(res Result) float64 {
			if res.Seconds > 0 {
				return res.Seconds + prep
			}
			return share + prep
		}
		got := map[string]bool{}
		for _, res := range run.results {
			switch {
			case !asked[res.Instance]:
				rep.Outcomes = append(rep.Outcomes, Outcome{Full: res.Instance, Status: StatusUnasked, Reason: fmt.Sprintf("the %s adapter reported an instance it was not asked for", g.adapter)})
				rep.Problems = append(rep.Problems, fmt.Sprintf("the %s adapter (%s) reported %q, which it was not asked for", g.adapter, g.name, res.Instance))
			case got[res.Instance]:
				rep.Problems = append(rep.Problems, fmt.Sprintf("the %s adapter (%s) reported %s twice", g.adapter, g.name, res.Instance))
				status[res.Instance] = Outcome{Full: res.Instance, Status: StatusFail, Reason: "reported twice"}
			default:
				got[res.Instance] = true
				rep.Ran++
				switch {
				case res.Verdict == Pass && RefuseSkips(strings.Split(res.Reason, "\n")) != nil:
					// a pass whose reason says it skipped did not run what it
					// passes for
					status[res.Instance] = Outcome{Full: res.Instance, Status: StatusFail, Reason: "a pass whose reason reports a skip, and nothing is ever skipped: " + res.Reason, Seconds: secondsOf(res)}
				case res.Verdict == Pass:
					// a pass keeps its reason: a fuzz row's names its seed
					status[res.Instance] = Outcome{Full: res.Instance, Status: StatusPass, Reason: res.Reason, Seconds: secondsOf(res)}
				case res.Verdict == Fail:
					reason := res.Reason
					if strings.TrimSpace(reason) == "" {
						reason = "the adapter gave no reason"
					}
					status[res.Instance] = Outcome{Full: res.Instance, Status: StatusFail, Reason: reason, Seconds: secondsOf(res)}
				default:
					// there is no skipped verdict: a test passes or fails
					status[res.Instance] = Outcome{Full: res.Instance, Status: StatusFail, Reason: fmt.Sprintf("verdict %q is not pass or fail", res.Verdict)}
				}
			}
		}
		for _, inst := range g.instances {
			if got[inst.Full] {
				continue
			}
			reason := fmt.Sprintf("the %s adapter did not report it", g.adapter)
			if run.err != nil {
				reason += ": " + firstLine(run.err.Error())
			}
			status[inst.Full] = Outcome{Full: inst.Full, Status: StatusFail, Reason: reason}
		}
		if run.err != nil {
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s: %v", g.name, run.err))
		}
	}

	for _, inst := range instances {
		o := status[inst.Full]
		switch o.Status {
		case StatusPass:
			rep.Passed++
		case StatusFail:
			rep.Failed++
		case StatusMissing:
			rep.Missing++
		}
		rep.Outcomes = append(rep.Outcomes, o)
	}
	sort.SliceStable(rep.Outcomes, func(i, j int) bool { return rank(rep.Outcomes[i].Status) < rank(rep.Outcomes[j].Status) })
	if rep.Expected != rep.Ran {
		rep.Problems = append(rep.Problems, fmt.Sprintf("expected %d instances to run, %d ran", rep.Expected, rep.Ran))
	}
	// a run that ran nothing proves nothing: it is red, never a pass of 0
	if rep.Expected == 0 {
		rep.Problems = append(rep.Problems, "the run had no instance to run, and a run of 0 tests is no pass")
	}
	return rep
}

// prepare runs each group's Prepare (Preparer), one after another, each under
// a deadline of its own (Runner.Context for "prepare <group>") and no call's,
// and returns each group's error.
func (rn *Runner) prepare(parent context.Context, groups []group) ([]error, []float64) {
	out := make([]error, len(groups))
	seconds := make([]float64, len(groups))
	for gi, g := range groups {
		p, ok := rn.Adapters[g.adapter].(Preparer)
		if !ok {
			continue
		}
		ctx, cancel := rn.Context(parent, "prepare "+g.name)
		start := rn.clock()
		err := p.Prepare(ctx, g.lang, g.instances)
		seconds[gi] = rn.clock().Sub(start).Seconds()
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)
		cancel()
		switch {
		case err != nil && timedOut:
			out[gi] = fmt.Errorf("adapter %s timed out preparing %s: %w", g.adapter, g.name, err)
		case err != nil:
			out[gi] = fmt.Errorf("adapter %s could not prepare %s: %w", g.adapter, g.name, err)
		}
	}
	return out, seconds
}

// Prepare builds what the instances' calls need (Preparer), as Run does
// before its first call: a plant prepares the rows it runs before its own
// call starts.
func (rn *Runner) Prepare(parent context.Context, instances []Instance) error {
	errs, _ := rn.prepare(parent, rn.Plan(instances).groups)
	return errors.Join(errs...)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// rank orders a report so the reds come last, next to the counts.
func rank(s string) int {
	switch s {
	case StatusPass:
		return 0
	case StatusUnrep:
		return 1
	case StatusMissing:
		return 2
	default:
		return 3
	}
}
