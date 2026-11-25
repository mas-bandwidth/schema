package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Manifest is one shard's work, handed to `fullrun shard` on its bench.
type Manifest struct {
	Run   string            `json:"run"`
	Sha   string            `json:"sha"`
	Shard string            `json:"shard"`
	Bench string            `json:"bench"`
	Env   map[string]string `json:"env,omitempty"`
	Units []Unit            `json:"units"`
}

// Verdicts a name ends a run with.
const (
	Pass = "PASS"
	Fail = "FAIL"
)

// Result is one name's verdict, as a shard writes it (results.jsonl) and
// the collector reads it.
type Result struct {
	Name    string  `json:"name"`
	Unit    string  `json:"unit"`
	Verdict string  `json:"verdict"`
	First   string  `json:"first,omitempty"`
	Log     string  `json:"log"` // relative to the shard's out directory
	Seconds float64 `json:"seconds"`
}

// Problem is a shard-level finding that is not one name's verdict.
type Problem struct {
	Kind   string `json:"kind"`
	Unit   string `json:"unit"`
	Detail string `json:"detail"`
}

// The files a shard keeps in its out directory.
const (
	resultsFile  = "results.jsonl"
	problemsFile = "problems.jsonl"
	currentFile  = "current"
	doneFile     = "DONE"
	launcherPID  = "launcher.pid"
	logsDir      = "logs"
)

// fullEnv is set for every unit: no prompt waits for a password, GOFLAGS
// from the bench changes no test, and PUSH is cleared so no release target
// publishes.
var fullEnv = map[string]string{
	"GIT_TERMINAL_PROMPT": "0",
	"GOFLAGS":             "",
	"PUSH":                "",
}

// Cmd runs one command in the clone: argv with extra env, its output to
// out. It returns the command's error (an *exec.ExitError for a non-zero
// status).
type Cmd func(ctx context.Context, argv []string, env map[string]string, out io.Writer) error

// realCmd runs the command in the current directory, in a process group of
// its own (ownGroup): stopping the shard cancels it and kills that group.
func realCmd(ctx context.Context, argv []string, env map[string]string, out io.Writer) error {
	return unitCmd(ctx, argv, env, out).Run()
}

// unitCmd is the command realCmd runs.
func unitCmd(ctx context.Context, argv []string, env map[string]string, out io.Writer) *exec.Cmd {
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	ownGroup(c)
	c.Env = mergeEnv(os.Environ(), env)
	c.Stdout = out
	c.Stderr = out
	c.WaitDelay = 10 * time.Second
	return c
}

// mergeEnv sets each key of env in base; an empty value removes the key.
func mergeEnv(base []string, env map[string]string) []string {
	var out []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, set := env[k]; !set {
			out = append(out, kv)
		}
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if env[k] != "" {
			out = append(out, k+"="+env[k])
		}
	}
	return out
}

// Runner runs a shard's units in the clone it is started in.
type Runner struct {
	M          Manifest
	Out        string
	SchemaTest string
	Cmd        Cmd
	Now        func() time.Time
	Log        io.Writer
	// Reap kills what a dead shard left running under marker before a
	// shard started again runs anything (reapShard on a bench).
	Reap func(marker string) (int, error)
}

// shardMarker is the environment variable every unit of a shard runs
// with, naming the shard's out directory, so a shard started again finds
// the units its dead predecessor left running.
const shardMarker = "FULLRUN_SHARD"

// loadResults reads the results a shard has written, ignoring a last line
// a death cut short.
func loadResults(path string) ([]Result, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Result
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // a write the shard's death cut short; its unit runs again
		}
		out = append(out, r)
	}
	return out, nil
}

// Run runs every unit that has no verdict yet for every name, so a shard
// started again after a death carries on from the unit it died in.
func (r *Runner) Run(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Join(r.Out, logsDir), 0o755); err != nil {
		return err
	}
	// one shard at a time in an out directory (and so in its clone): a
	// shard the launcher took for dead while its bench did not answer may
	// still run
	unlock, err := lockOut(r.Out)
	if err != nil {
		return err
	}
	defer unlock()
	prev, err := loadResults(filepath.Join(r.Out, resultsFile))
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, res := range prev {
		have[res.Name] = true
	}
	marker, err := filepath.Abs(r.Out)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(r.Out, currentFile)); err == nil || len(prev) > 0 {
		if r.Reap == nil {
			return errors.New("a shard started again needs a way to stop what its dead predecessor left running")
		}
		n, err := r.Reap(marker)
		if err != nil {
			return err
		}
		if n > 0 {
			logf(r.Log, "fullrun shard %s: started again: killed %d processes its dead predecessor left running\n", r.M.Shard, n)
			if err := r.problem(Problem{"leftover", "", fmt.Sprintf("killed %d processes the dead shard left running", n)}); err != nil {
				return err
			}
		}
	}
	env := map[string]string{shardMarker: marker}
	for k, v := range fullEnv {
		env[k] = v
	}
	for k, v := range r.M.Env {
		env[k] = v
	}
	ran := 0
	for i, u := range r.M.Units {
		done := true
		for _, n := range u.Names {
			done = done && have[n.Name]
		}
		if done {
			continue
		}
		if err := os.WriteFile(filepath.Join(r.Out, currentFile), []byte(u.ID+"\n"), 0o644); err != nil {
			return err
		}
		if dirty := r.dirty(ctx); dirty != "" {
			if err := r.problem(Problem{"dirty-before", u.ID, dirty}); err != nil {
				return err
			}
			if err := r.reset(ctx, u.ID); err != nil {
				return err
			}
		}
		logRel := filepath.Join(logsDir, fmt.Sprintf("%04d-%s.log", i+1, sanitize(u.ID)))
		results, err := r.runUnit(ctx, u, env, logRel)
		if err != nil {
			return err
		}
		if err := r.appendResults(results); err != nil {
			return err
		}
		ran++
		if dirty := r.dirty(ctx); dirty != "" {
			if err := r.problem(Problem{"left-dirty", u.ID, dirty}); err != nil {
				return err
			}
			if err := r.reset(ctx, u.ID); err != nil {
				return err
			}
		}
	}
	// the shard's own temporary directory (startScript's TMPDIR) goes
	if t := r.Out + ".tmp"; os.Getenv("TMPDIR") == t {
		if err := os.RemoveAll(t); err != nil {
			return fmt.Errorf("remove the shard's temporary files: %w", err)
		}
	}
	return os.WriteFile(filepath.Join(r.Out, doneFile), fmt.Appendf(nil, "units %d ran-now %d\n", len(r.M.Units), ran), 0o644)
}

func (r *Runner) problem(p Problem) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(r.Out, problemsFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// appendResults writes a unit's verdicts in one write and syncs it.
func (r *Runner) appendResults(results []Result) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, res := range results {
		if err := enc.Encode(res); err != nil {
			return err
		}
	}
	path := filepath.Join(r.Out, resultsFile)
	// a line a death cut short is ended first, so this unit's first line
	// is not glued onto it
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 && old[len(old)-1] != '\n' {
		b = *bytes.NewBuffer(append([]byte{'\n'}, b.Bytes()...))
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b.Bytes()); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// dirty is the first changed or new path git sees in the clone, or "".
func (r *Runner) dirty(ctx context.Context) string {
	var out bytes.Buffer
	if err := r.Cmd(ctx, []string{"git", "status", "--porcelain", "--untracked-files=all"}, nil, &out); err != nil {
		return "git status failed: " + err.Error()
	}
	s := strings.TrimSpace(out.String())
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	return fmt.Sprintf("%d paths, first %s", len(lines), strings.TrimSpace(lines[0]))
}

// reset puts the clone back at its commit; a reset that fails is a
// problem, and the shard stops rather than run the next unit in a tree it
// could not clean.
func (r *Runner) reset(ctx context.Context, unit string) error {
	for _, argv := range [][]string{{"git", "reset", "-q", "--hard", "HEAD"}, {"git", "clean", "-fdq"}} {
		var out bytes.Buffer
		if err := r.Cmd(ctx, argv, nil, &out); err != nil {
			detail := fmt.Sprintf("%s: %v: %s", strings.Join(argv, " "), err, lastLine(out.String()))
			if perr := r.problem(Problem{"reset-failed", unit, detail}); perr != nil {
				return perr
			}
			return fmt.Errorf("reset the clone after %s: %s", unit, detail)
		}
	}
	return nil
}

var unsafeRE = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func sanitize(id string) string {
	s := unsafeRE.ReplaceAllString(id, "_")
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// runUnit runs one unit and returns a verdict for every one of its names.
func (r *Runner) runUnit(ctx context.Context, u Unit, env map[string]string, logRel string) ([]Result, error) {
	f, err := os.Create(filepath.Join(r.Out, logRel))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var argv []string
	switch u.Kind {
	case KindRegistry:
		argv = append([]string{r.SchemaTest, "run", "--timeout", "0"}, u.Args...)
	case KindGoTest:
		argv = []string{"go", "test", "-count=1", "-timeout", "0", "-json"}
		if u.Tags != "" {
			argv = append(argv, "-tags", u.Tags)
		}
		argv = append(argv, "-run", "^("+strings.Join(u.Args, "|")+")$", u.Dir)
	case KindGoBench:
		argv = []string{"go", "test", "-count=1", "-timeout", "0", "-json", "-run", "^$", "-bench", "^(" + strings.Join(u.Args, "|") + ")$", "-benchtime", "1x", u.Dir}
	case KindMake:
		argv = []string{"make", "--no-print-directory", u.Args[0]}
	default:
		return nil, fmt.Errorf("unit %s: a shard does not run a %s unit", u.ID, u.Kind)
	}
	logf(f, "# fullrun %s %s on %s, shard %s\n# %s\n", r.M.Run, u.ID, r.M.Bench, r.M.Shard, strings.Join(argv, " "))
	var out bytes.Buffer
	start := r.Now()
	runErr := r.Cmd(ctx, argv, env, io.MultiWriter(f, &out))
	secs := r.Now().Sub(start).Seconds()
	logf(f, "\n# exit: %v, %.1fs\n", runErr, secs)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var verdicts map[string]Result
	switch u.Kind {
	case KindRegistry:
		verdicts = schemaTestVerdicts(out.Bytes(), u.Args, runErr)
	case KindGoTest, KindGoBench:
		verdicts = goTestVerdicts(out.Bytes(), u.Args, runErr, u.Kind == KindGoBench, u.Tags)
	case KindMake:
		v := Result{Verdict: Pass}
		if runErr != nil {
			v = Result{Verdict: Fail, First: firstFailLine(strings.Split(out.String(), "\n"), fmt.Sprintf("make %s: %v", u.Args[0], runErr))}
		}
		verdicts = map[string]Result{u.Args[0]: v}
	}
	per := secs / float64(len(u.Names))
	results := make([]Result, len(u.Names))
	for i, n := range u.Names {
		v := verdicts[u.Args[i]]
		results[i] = Result{Name: n.Name, Unit: u.ID, Verdict: v.Verdict, First: v.First, Log: logRel, Seconds: per}
	}
	return results, nil
}

// outcomeRE is a schema-test outcome line: STATUS name, or STATUS name:
// reason, the name followed by how long it took when it ran (" (0.02s)").
var outcomeRE = regexp.MustCompile(`^(PASS|FAIL|MISSING|UNASKED|UNREPRESENTABLE) (\S+?)(?: \([0-9.]+s\))?(?:: (.*))?$`)

// shortRE is schema-test's run problem for instances that did not run; it
// is the asked names' own problem when their MISSING lines account for it.
var shortRE = regexp.MustCompile(`^FAIL run: expected ([0-9]+) instances to run, ([0-9]+) ran$`)

// schemaTestVerdicts reads schema-test run's outcome lines for the asked
// names. A name with no outcome line fails, naming the run's first problem.
func schemaTestVerdicts(out []byte, asked []string, runErr error) map[string]Result {
	want := map[string]bool{}
	for _, a := range asked {
		want[a] = true
	}
	got := map[string]Result{}
	var problems []string // the run's own problems, and then its summary
	runProblems := 0      // FAIL run: lines, a problem no name's outcome shows
	short, shortBy, missing := "", 0, 0
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if m := shortRE.FindStringSubmatch(line); m != nil && short == "" {
			x, _ := strconv.Atoi(m[1])
			y, _ := strconv.Atoi(m[2])
			short, shortBy = line, x-y
			continue
		}
		if strings.HasPrefix(line, "FAIL run: ") {
			problems = append(problems[:runProblems], append([]string{line}, problems[runProblems:]...)...)
			runProblems++
			continue
		}
		if strings.HasPrefix(line, "schema-test FAIL: ") {
			problems = append(problems, line) // the summary: a name failed
			continue
		}
		m := outcomeRE.FindStringSubmatch(line)
		if m == nil || !want[m[2]] {
			continue
		}
		if m[1] == "PASS" {
			got[m[2]] = Result{Verdict: Pass}
			continue
		}
		if m[1] == "MISSING" {
			missing++
		}
		reason := m[1]
		if m[3] != "" {
			reason += ": " + m[3]
		}
		got[m[2]] = Result{Verdict: Fail, First: clip(reason)}
	}
	// instances that did not run are the run's problem unless the asked
	// names' MISSING lines are exactly them
	if short != "" && shortBy != missing {
		problems = append([]string{short}, problems...)
		runProblems++
	}
	// a pass beside a run that failed with nothing to show for it is not a
	// pass: an adapter crashed, or schema-test exited non-zero after
	// reporting every asked name passed
	failed := false
	for _, v := range got {
		failed = failed || v.Verdict == Fail
	}
	if runProblems > 0 || runErr != nil && !failed {
		why := fmt.Sprintf("it passed, but schema-test exited %v", runErr)
		if runProblems > 0 {
			why = "it passed, but the run reported a problem: " + problems[0]
		} else if tail := lastLine(string(out)); tail != "" {
			why += ": " + tail
		}
		for a, v := range got {
			if v.Verdict == Pass {
				got[a] = Result{Verdict: Fail, First: clip(why)}
			}
		}
	}
	for _, a := range asked {
		if _, ok := got[a]; ok {
			continue
		}
		why := fmt.Sprintf("schema-test reported no outcome for it (exit: %v)", runErr)
		if len(problems) > 0 {
			why += ": " + problems[0]
		} else if tail := lastLine(string(out)); tail != "" {
			why += ": " + tail
		}
		got[a] = Result{Verdict: Fail, First: clip(why)}
	}
	return got
}

// goEvent is one line of go test -json.
type goEvent struct {
	Action     string
	Test       string
	Output     string
	ImportPath string
}

// goTestVerdicts reads go test -json for the asked top-level tests. A
// skipped test fails (nothing is ever skipped); a test with no verdict
// fails, naming the package's first error line (a build failure).
func goTestVerdicts(out []byte, asked []string, runErr error, bench bool, tags string) map[string]Result {
	want := map[string]bool{}
	for _, a := range asked {
		want[a] = true
	}
	got := map[string]Result{}
	output := map[string][]string{}
	subOut := map[string][]string{}
	skipped := map[string]string{} // a top-level test -> its first skipped subtest
	pkgFailed := false
	var pkgLines []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var ev goEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			pkgLines = append(pkgLines, sc.Text()) // go's own stderr: a build failure
			continue
		}
		top, _, _ := strings.Cut(ev.Test, "/")
		if ev.Test == "" || !want[top] {
			if ev.Output != "" {
				pkgLines = append(pkgLines, strings.TrimRight(ev.Output, "\n"))
			}
			pkgFailed = pkgFailed || ev.Test == "" && ev.Action == "fail"
			continue
		}
		if ev.Action == "output" {
			output[top] = append(output[top], strings.TrimRight(ev.Output, "\n"))
			if ev.Test != top {
				subOut[ev.Test] = append(subOut[ev.Test], strings.TrimRight(ev.Output, "\n"))
			}
			continue
		}
		if ev.Test != top {
			// a subtest's pass or fail its parent's verdict carries; a
			// skipped subtest fails its parent, as a skipped test does
			if ev.Action == "skip" && skipped[top] == "" {
				skipped[top] = ev.Test + ": " + firstSkipLine(subOut[ev.Test])
			}
			continue
		}
		switch ev.Action {
		case "pass":
			got[top] = Result{Verdict: Pass}
		case "fail":
			got[top] = Result{Verdict: Fail, First: firstFailLine(output[top], "--- FAIL: "+top)}
		case "skip":
			got[top] = Result{Verdict: Fail, First: clip("skipped, and nothing is ever skipped: " + firstSkipLine(output[top]))}
		}
	}
	for top, sub := range skipped {
		// a benchmark has no top-level pass event, so one with no verdict
		// yet is flipped too: else benchRan would pass it below
		if v, ok := got[top]; !ok || v.Verdict == Pass {
			got[top] = Result{Verdict: Fail, First: clip("subtest skipped, and nothing is ever skipped: " + sub)}
		}
	}
	// a pass beside a package that failed with no asked test failing is not
	// a pass: TestMain failed after m.Run, or the binary died at exit
	failed := false
	for _, v := range got {
		failed = failed || v.Verdict == Fail
	}
	if (runErr != nil || pkgFailed) && !failed {
		why := fmt.Sprintf("it passed, but go test exited %v", runErr)
		if line := firstFailLine(pkgLines, ""); line != "" {
			why += ": " + line
		}
		for a, v := range got {
			if v.Verdict == Pass {
				got[a] = Result{Verdict: Fail, First: clip(why)}
			}
		}
	}
	for _, a := range asked {
		if _, ok := got[a]; ok {
			continue
		}
		if bench && runErr == nil && benchRan(output[a], a) {
			got[a] = Result{Verdict: Pass}
			continue
		}
		why := fmt.Sprintf("go test reported no result for %s (exit: %v)", a, runErr)
		if tags != "" {
			why += " with -tags " + tags
		}
		if line := firstFailLine(pkgLines, ""); line != "" {
			why += ": " + line
		}
		got[a] = Result{Verdict: Fail, First: clip(why)}
	}
	return got
}

// benchRan reports whether a benchmark printed its result line.
func benchRan(lines []string, name string) bool {
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), name) && strings.Contains(l, "/op") {
			return true
		}
		if strings.Contains(l, "--- FAIL: "+name) {
			return false
		}
	}
	return false
}

var (
	goLineRE = regexp.MustCompile(`\.go:\d+: `)
	failRE   = regexp.MustCompile(`(?i)(\berror\b|\bfail|refuse|panic:|\*\*\*|fatal|mismatch|not found|no such)`)
	noiseRE  = regexp.MustCompile(`^(=== (RUN|PAUSE|CONT|NAME)|--- (PASS|SKIP)|PASS$|ok\s)`)
)

// firstFailLine is the line a reader of the log should see first: the
// first test log line (file.go:N:), else the first line that says it
// failed, else the last line, else def.
func firstFailLine(lines []string, def string) string {
	for _, l := range lines {
		if goLineRE.MatchString(l) && !noiseRE.MatchString(strings.TrimSpace(l)) {
			return clip(l)
		}
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !noiseRE.MatchString(t) && failRE.MatchString(t) {
			return clip(t)
		}
	}
	t := lastLine(strings.Join(lines, "\n"))
	switch {
	case def != "" && t != "":
		return clip(def + ": " + t)
	case t != "":
		return clip(t)
	}
	return clip(def)
}

func firstSkipLine(lines []string) string {
	for _, l := range lines {
		if goLineRE.MatchString(l) {
			return strings.TrimSpace(l)
		}
	}
	return lastLine(strings.Join(lines, "\n"))
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// clip keeps a line one TSV cell: no tabs or newlines, at most 300 bytes.
func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:297] + "..."
	}
	return s
}

// ShardStatus is what `fullrun shard-status` reports to the launcher.
type ShardStatus struct {
	Done    bool   `json:"done"`
	Alive   bool   `json:"alive"`
	Results int    `json:"results"`
	Current string `json:"current"`
}

// shardStatus reads a shard's out directory; alive asks whether a pid runs.
func shardStatus(out string, alive func(pid int) bool) (ShardStatus, error) {
	var st ShardStatus
	if _, err := os.Stat(filepath.Join(out, doneFile)); err == nil {
		st.Done = true
	}
	res, err := loadResults(filepath.Join(out, resultsFile))
	if err != nil {
		return st, err
	}
	st.Results = len(res)
	if b, err := os.ReadFile(filepath.Join(out, currentFile)); err == nil {
		st.Current = strings.TrimSpace(string(b))
	}
	b, err := os.ReadFile(filepath.Join(out, launcherPID))
	if err != nil {
		return st, fmt.Errorf("%s: %w", launcherPID, err)
	}
	var pid int
	if _, err := fmt.Sscan(string(b), &pid); err != nil || pid <= 0 {
		return st, fmt.Errorf("%s holds %q, not a pid", launcherPID, strings.TrimSpace(string(b)))
	}
	st.Alive = alive(pid)
	if !st.Alive && !st.Done {
		// the shard may have finished between the first look and the
		// liveness check: read again, so a done shard is not called dead
		if _, err := os.Stat(filepath.Join(out, doneFile)); err == nil {
			st.Done = true
			if res, err := loadResults(filepath.Join(out, resultsFile)); err == nil {
				st.Results = len(res)
			} else {
				return st, err
			}
		}
	}
	return st, nil
}
