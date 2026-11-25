package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// PlantAppendEnv is the hook a plant row of the test registry breaks a model
// with: its value is appended to every requested config, in a scratch copy
// of tla/, before TLC runs. An INVARIANT the module does not define, or one
// the model violates, makes the run something other than its RUNS.tsv row,
// and the instance must come out red.
const PlantAppendEnv = "SCHEMA_TEST_TLC_PLANT_APPEND"

// modelInputs is a registry row's inputs for the model-check adapter: one
// config under tla/ run by TLC and judged against its RUNS.tsv row, with the
// TLC worker count the run takes (1 when absent); or the runner's own
// controls (-controls), which prove the judge refuses three broken trees.
type modelInputs struct {
	Config   string `json:"config"`
	Workers  int    `json:"workers"`
	Controls bool   `json:"controls"`
}

// The runner a verdict names: a missing tool fails naming the host it is
// missing on, never as a skip.
func runnerName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "this runner (hostname unknown)"
	}
	return "runner " + h
}

// toolProblem names what a runner lacks to run TLC: the jar and java.
func toolProblem(jar, java string) string {
	if _, err := os.Stat(jar); err != nil {
		return fmt.Sprintf("no TLC jar at %s on %s (set TLC_JAR, or install tla2tools.jar there)", jar, runnerName())
	}
	if _, err := exec.LookPath(java); err != nil {
		return fmt.Sprintf("no %s on PATH of %s: TLC needs a JVM", java, runnerName())
	}
	return ""
}

// adapterJob is one requested instance, decoded.
type adapterJob struct {
	req testreg.Request
	in  modelInputs
	row Row
}

// runAdapter is the test registry's model-check harness (docs/TEST-REGISTRY.md):
// it reads request lines on in and writes one SCHEMA_TEST_RESULT line per
// request on out. Each request names one config, which must pair with its
// RUNS.tsv row (Plan) and is judged against it (Judge); the controls request
// runs runControls. A request the harness cannot read is an error, and so a
// crash of the whole call; a run that is not its recorded row is a fail
// verdict with the reason. parallel bounds how many TLC processes run at once.
func runAdapter(tlaDir string, tlc checker, tools string, parallel int, in io.Reader, out io.Writer) error {
	reqs, err := testreg.DecodeRequests(in)
	if err != nil {
		return err
	}
	if len(reqs) == 0 {
		return fmt.Errorf("no requests: a harness call runs at least one instance")
	}
	rows, problems := Plan(tlaDir)
	plant, planted := os.LookupEnv(PlantAppendEnv)
	var jobs []adapterJob
	var controls []testreg.Request
	byConfig := map[string]Row{}
	for _, r := range rows {
		byConfig[r.Config] = r
	}
	for _, q := range reqs {
		var mi modelInputs
		d := json.NewDecoder(bytes.NewReader(q.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&mi); err != nil {
			return fmt.Errorf("%s: inputs: %w", q.Instance, err)
		}
		switch {
		case mi.Controls && (mi.Config != "" || mi.Workers != 0):
			return fmt.Errorf("%s: the controls request names no config", q.Instance)
		case mi.Controls:
			controls = append(controls, q)
			continue
		case mi.Config == "" || filepath.Base(mi.Config) != mi.Config || !strings.HasSuffix(mi.Config, ".cfg"):
			return fmt.Errorf("%s: inputs want a config file name under tla/, got %q", q.Instance, mi.Config)
		case mi.Workers < 0:
			return fmt.Errorf("%s: workers %d", q.Instance, mi.Workers)
		}
		if mi.Workers == 0 {
			mi.Workers = 1
		}
		jobs = append(jobs, adapterJob{req: q, in: mi, row: byConfig[mi.Config]})
	}
	results := make(map[string]testreg.Result, len(reqs))
	fail := func(q testreg.Request, format string, a ...any) {
		results[q.Instance] = testreg.Result{Instance: q.Instance, Verdict: testreg.Fail, Reason: fmt.Sprintf(format, a...)}
	}
	if tools != "" {
		for _, q := range reqs {
			fail(q, "%s", tools)
		}
		return writeResults(out, reqs, results)
	}
	if len(problems) > 0 {
		// the tree does not pair: nothing it holds is a recorded run
		for _, q := range reqs {
			fail(q, "tla/ and tla/RUNS.tsv do not pair, so no run is recorded: %s", strings.Join(problems, "; "))
		}
		return writeResults(out, reqs, results)
	}
	dir := tlaDir
	if planted && len(jobs) > 0 {
		scratch, err := copyDir(tlaDir)
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(scratch) }() // errdiscard: the planted scratch copy; verdicts are written above it
		for _, j := range jobs {
			p := filepath.Join(scratch, j.in.Config)
			src, err := os.ReadFile(p)
			if err != nil {
				fail(j.req, "planted %s=%q: %v", PlantAppendEnv, plant, err)
				continue
			}
			if err := os.WriteFile(p, append(src, []byte("\n"+plant+"\n")...), 0o644); err != nil {
				return err
			}
		}
		dir = scratch
	}
	// every verdict reached without TLC is reached here, before any run
	// starts; each run writes only its own slot of errs, and the verdicts
	// are written from errs once every run is done: no run shares a map
	var runs []adapterJob
	for _, j := range jobs {
		if _, done := results[j.req.Instance]; done {
			continue
		}
		if j.row.Config == "" {
			fail(j.req, "tla/%s has no RUNS.tsv row, so no run is recorded for it", j.in.Config)
			continue
		}
		runs = append(runs, j)
	}
	errs := make([]error, len(runs))
	sem := make(chan struct{}, max(parallel, 1))
	var wg sync.WaitGroup
	for i, j := range runs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			errs[i] = runOneWith(dir, j.row, tlc, j.in.Workers)
		})
	}
	wg.Wait()
	for i, j := range runs {
		if errs[i] != nil {
			fail(j.req, "%s (module %s) is not its RUNS.tsv run: %v", j.row.Config, j.row.Module, errs[i])
			continue
		}
		results[j.req.Instance] = testreg.Result{Instance: j.req.Instance, Verdict: testreg.Pass}
	}
	for _, q := range controls {
		var sink strings.Builder
		if err := runControls(tlaDir, tlc, &sink); err != nil {
			fail(q, "the runner's controls: %v", err)
			continue
		}
		results[q.Instance] = testreg.Result{Instance: q.Instance, Verdict: testreg.Pass}
	}
	return writeResults(out, reqs, results)
}

func writeResults(out io.Writer, reqs []testreg.Request, results map[string]testreg.Result) error {
	var b strings.Builder
	names := make([]string, 0, len(reqs))
	for _, q := range reqs {
		names = append(names, q.Instance)
	}
	sort.Strings(names)
	for _, n := range names {
		r, ok := results[n]
		if !ok {
			return fmt.Errorf("%s: no verdict was reached", n)
		}
		b.WriteString(testreg.FormatResult(r) + "\n")
	}
	_, err := io.WriteString(out, b.String())
	return err
}

// adapterMain is tlcrun -adapter: TLC from jar and java, checked first, so a
// runner without either fails every requested instance naming the runner.
func adapterMain(tlaDir, jar, java string, parallel int, in io.Reader, out io.Writer) error {
	return runAdapter(tlaDir, realTLC{java: java, jar: jar, workers: 4}, toolProblem(jar, java), parallel, in, out)
}

// defaultParallel is how many TLC processes the harness runs at once: a
// quarter of the cores, at least one.
func defaultParallel() int { return max(runtime.NumCPU()/4, 1) }
