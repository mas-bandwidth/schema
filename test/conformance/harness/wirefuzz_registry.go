package main

// THE WIRE FUZZ AS REGISTRY ROWS (docs/TEST-REGISTRY.md, "Adapters"): two
// families every language answers through its wire-fuzz driver, the same
// driver make/wire-fuzz-release.mk names for the leg.
//
//   - fuzz.wire-fuzz-vector.<vector>: one row per pinned vector of
//     testdata/wire/tables/fuzz-vectors/INDEX.txt. The instance replays the
//     vector EXACTLY as the index names it (its unit, its root, its form) to
//     the leg and the oracle, and passes when they agree on it.
//   - fuzz.wire-fuzz.<arm>: the whole roster, the enumerated pass and the
//     seeded random pass of wireFuzzN mutants, in one arm (plain, mixed:
//     --mixed-builder, retain: --retain). The seed is the run's, never the
//     row's: schema-test run hands it in as SCHEMA_TEST_SEED (testreg.SeedEnv:
//     --seed, or the UTC date), every verdict names it, pass or fail, and a
//     red re-runs by name with it. A missing or malformed seed fails the row.
//     A mutant a run once found is pinned as a vector row, never as a seed.
//
// A language may have more than one configuration of its leg (Python: the
// current and the floor interpreter, each in normal and -X dev mode; Ruby:
// the current and the floor interpreter). An instance runs every
// configuration the language's make target passes, and passes only when each
// one does: a configuration is how the language answers, never a case of its
// own.
//
// `harness wire-fuzz-rows` prints the rows; TestTheRegistryHoldsEveryWireFuzzRow
// holds test/registry/registry.jsonl to them. `harness wire-fuzz-adapter
// --config NAME=COMMAND...` answers the request lines on its standard input,
// one result line per request, each run a child wire-fuzz process.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

const (
	wireFuzzVectorFamily = "wire-fuzz-vector"
	wireFuzzFamily       = "wire-fuzz"
	// wireFuzzN is the random pass's count per run: the count the Python and
	// Ruby nightly fuzz workflows ran before these rows replaced them.
	wireFuzzN = 500000
)

// wireFuzzArms are the arms, each with the harness flag that selects it.
var wireFuzzArms = []struct{ Name, Flag, What string }{
	{"plain", "", "the plain read and write-back"},
	{"mixed", "--mixed-builder", "the mutable builder oracle on the variable file roots"},
	{"retain", "--retain", "the retaining read's canonical write-back"},
}

// wireFuzzInputs is a wire-fuzz row's inputs. A vector row names the vector
// and the committed files it reads (the registry indexes them where they are);
// a random-pass row names its arm and count (its seed is the run's).
type wireFuzzInputs struct {
	Vector  string   `json:"vector,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Root    string   `json:"root,omitempty"`
	Message bool     `json:"message,omitempty"`
	Arm     string   `json:"arm,omitempty"`
	N       int      `json:"n,omitempty"`
	Files   []string `json:"files,omitempty"`
}

type wireFuzzRow struct {
	Rec    string         `json:"rec"`
	Key    string         `json:"key"`
	Type   string         `json:"type"`
	Family string         `json:"family"`
	Case   string         `json:"case"`
	Tier   string         `json:"tier"`
	Checks string         `json:"checks"`
	Inputs wireFuzzInputs `json:"inputs"`
	Expect string         `json:"expect"`
	Source string         `json:"source"`
}

// wireFuzzRows is every wire-fuzz row, sorted by key: the index's vectors and
// the random pass's arms.
func wireFuzzRows(index, pinnedList string) ([]wireFuzzRow, error) {
	vecs, lines, err := readWireVectorLines(index)
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("%s: no pinned vector", index)
	}
	var out []wireFuzzRow
	seen := map[string]string{}
	for i, v := range vecs {
		c := caseSlug(v.name)
		if prev, dup := seen[c]; dup {
			return nil, fmt.Errorf("%s: vectors %s and %s are both case %s", index, prev, v.name, c)
		}
		seen[c] = v.name
		form := "file"
		if v.message {
			form = "message"
		}
		out = append(out, wireFuzzRow{
			Rec: "test", Key: "fuzz." + wireFuzzVectorFamily + "." + c, Type: "fuzz", Family: wireFuzzVectorFamily, Case: c,
			Tier:   "functional",
			Checks: fmt.Sprintf("pinned wire-fuzz vector %s: %s, read as a %s of %s.%s, gets the oracle's answer", v.name, filepath.ToSlash(v.file), form, v.unit, v.root),
			Inputs: wireFuzzInputs{Vector: v.name, Unit: v.unit, Root: v.root, Message: v.message, Files: []string{filepath.ToSlash(v.file), filepath.ToSlash(index)}},
			Expect: "the leg's acceptance, report, measure and write-back equal the oracle's on the vector, in every configuration of the leg",
			Source: fmt.Sprintf("%s:%d", filepath.ToSlash(index), lines[i]),
		})
	}
	for _, a := range wireFuzzArms {
		out = append(out, wireFuzzRow{
			Rec: "test", Key: "fuzz." + wireFuzzFamily + "." + a.Name, Type: "fuzz", Family: wireFuzzFamily, Case: a.Name,
			Tier:   "nightly",
			Checks: fmt.Sprintf("wire fuzz, %s arm (%s): every corpus root's enumerated mutants and %d random ones at the run's seed", a.Name, a.What, wireFuzzN),
			Inputs: wireFuzzInputs{Arm: a.Name, N: wireFuzzN},
			Expect: "no divergence from the oracle, at least one mutant, and no corpus root the leg has no codec for, in every configuration of the leg; the verdict names the seed",
			Source: "test/conformance/harness/wirefuzz_registry.go",
		})
	}
	pinned, err := readMooPinned(pinnedList)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{} // file and form, as the index replays them plain
	for _, v := range vecs {
		have[filepath.ToSlash(v.file)+fmt.Sprint(v.message)] = true
	}
	for _, p := range pinned {
		if !p.retain && have[p.file+fmt.Sprint(p.message)] {
			continue // the index's own vector, replayed the same way
		}
		stem := strings.TrimSuffix(filepath.Base(p.file), filepath.Ext(p.file))
		c := caseSlug(stem)
		arm, how := "", "replayed"
		if p.retain {
			c, arm, how = c+"-retain", "retain", "replayed in the retain arm"
		}
		if prev, dup := seen[c]; dup {
			return nil, fmt.Errorf("%s:%d: %s is case %s, which %s already is", pinnedList, p.line, p.file, c, prev)
		}
		seen[c] = p.file
		form := "file"
		if p.message {
			form = "message"
		}
		out = append(out, wireFuzzRow{
			Rec: "test", Key: "fuzz." + wireFuzzVectorFamily + "." + c, Type: "fuzz", Family: wireFuzzVectorFamily, Case: c,
			Tier:   "functional",
			Checks: fmt.Sprintf("pinned wire-fuzz mutant %s: %s, read as a %s of %s.%s and %s, gets the oracle's answer", stem, p.file, form, p.unit, p.root, how),
			Inputs: wireFuzzInputs{Vector: stem, Unit: p.unit, Root: p.root, Message: p.message, Arm: arm, Files: []string{p.file, pinnedList}},
			Expect: "the leg's acceptance, report, measure and write-back equal the oracle's on the mutant, in every configuration of the leg",
			Source: fmt.Sprintf("%s:%d", pinnedList, p.line),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// mooPinnedList is the moo leg's pinned mutants (make/moo-fuzz.mk): each
// line is a mutant the moo leg once diverged on. Every language answers it
// as a wire-fuzz-vector row, unless the index already replays it the same
// way.
const mooPinnedList = "test/conformance/moo/wire-fuzz-pinned.txt"

type mooPinned struct {
	file, unit, root string
	message, retain  bool
	line             int
}

// readMooPinned reads the list: <mutant file> <unit> <root> [message|retain].
func readMooPinned(path string) ([]mooPinned, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []mooPinned
	for i, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		p := mooPinned{file: f[0], line: i + 1}
		switch {
		case len(f) == 3:
		case len(f) == 4 && f[3] == "message":
			p.message = true
		case len(f) == 4 && f[3] == "retain":
			p.retain = true
		default:
			return nil, fmt.Errorf("%s:%d: a pinned mutant is <file> <unit> <root> [message|retain]", path, i+1)
		}
		p.unit, p.root = f[1], f[2]
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s names no mutant", path)
	}
	return out, nil
}

// readWireVectorLines is readWireVectors with each vector's line number.
func readWireVectorLines(path string) ([]wireVector, []int, error) {
	vecs, err := readWireVectors(path)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var lines []int
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, i+1)
	}
	if len(lines) != len(vecs) {
		return nil, nil, fmt.Errorf("%s: %d vector lines, %d vectors", path, len(lines), len(vecs))
	}
	return vecs, lines, nil
}

func writeWireFuzzRows(w io.Writer, rows []wireFuzzRow) error {
	var b bytes.Buffer
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	_, err := w.Write(b.Bytes())
	return err
}

// wireConfig is one configuration of a leg: its name and its driver command.
type wireConfig struct{ Name, Driver string }

// configFlag is the repeatable --config NAME=COMMAND.
type configFlag []wireConfig

func (c *configFlag) String() string { return fmt.Sprint(*c) }

func (c *configFlag) Set(s string) error {
	name, cmd, ok := strings.Cut(s, "=")
	name, cmd = strings.TrimSpace(name), strings.TrimSpace(cmd)
	if !ok || name == "" || cmd == "" {
		return fmt.Errorf("--config is NAME=COMMAND, not %q", s)
	}
	for _, have := range *c {
		if have.Name == name {
			return fmt.Errorf("--config %s is given twice", name)
		}
	}
	*c = append(*c, wireConfig{name, cmd})
	return nil
}

// wireFuzzRunArgs is the wire-fuzz command line one request's run in one
// configuration takes, and checks what the request's inputs say; seed is the
// run's, which only a random-pass row reads.
func wireFuzzRunArgs(q testreg.Request, cfg wireConfig, failed string, seed uint64) ([]string, error) {
	var in wireFuzzInputs
	d := json.NewDecoder(bytes.NewReader(q.Inputs))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return nil, fmt.Errorf("inputs: %w", err)
	}
	args := []string{"wire-fuzz", "--driver", cfg.Driver, "--failed", failed}
	switch q.Family {
	case wireFuzzVectorFamily:
		if in.Vector == "" || in.Unit == "" || in.Root == "" || len(in.Files) == 0 {
			return nil, fmt.Errorf("a vector row names its vector, unit, root and file")
		}
		args = append(args, "--replay", in.Files[0], "--unit", in.Unit, "--root", in.Root)
		if in.Message {
			args = append(args, "--message")
		}
		switch in.Arm {
		case "":
		case "retain":
			args = append(args, "--retain")
		default:
			return nil, fmt.Errorf("a vector is replayed plain or in the retain arm, not %q", in.Arm)
		}
	case wireFuzzFamily:
		flag, ok := "", false
		for _, a := range wireFuzzArms {
			if a.Name == in.Arm {
				flag, ok = a.Flag, true
			}
		}
		if !ok {
			return nil, fmt.Errorf("arm %q is none of the harness's", in.Arm)
		}
		if in.N < 1 {
			return nil, fmt.Errorf("a random-pass row's count must be positive, not %d", in.N)
		}
		if flag != "" {
			args = append(args, flag)
		}
		args = append(args, "--seed", strconv.FormatUint(seed, 10), "--n", strconv.Itoa(in.N))
	default:
		return nil, fmt.Errorf("family %s is not a wire-fuzz family", q.Family)
	}
	return args, nil
}

var (
	wireReplayDone   = regexp.MustCompile(`(^|\n)wire-fuzz: replay of .*, 1 mutant, 0 divergences`)
	wireZeroDiverged = regexp.MustCompile(`(^|[^0-9])0 divergences`)
	wireSeedsOver    = regexp.MustCompile(`[1-9][0-9]* seeds over [1-9][0-9]* roots`)
	wireMutants      = regexp.MustCompile(`= [1-9][0-9]* mutants`)
	wireAbsent       = regexp.MustCompile(`[0-9]+ seeds absent`)
)

// wireRunVerdict judges one finished run by its exit and its summary, as
// test/conformance/wire-fuzz-complete does: "" when it passed, else why not.
func wireRunVerdict(family string, exitErr error, out string) string {
	if exitErr != nil {
		return fmt.Sprintf("the run failed (%v): %s", exitErr, tailLines(out, 20))
	}
	if family == wireFuzzVectorFamily {
		if !wireReplayDone.MatchString(out) {
			return "the replay printed no summary of one mutant and 0 divergences: " + tailLines(out, 5)
		}
		return ""
	}
	switch {
	case wireAbsent.MatchString(out):
		return "the roster is incomplete: a corpus root has no codec in the leg: " + tailLines(out, 5)
	case !wireZeroDiverged.MatchString(out) || !wireSeedsOver.MatchString(out) || !wireMutants.MatchString(out):
		return "the run's coverage summary is missing or empty: " + tailLines(out, 5)
	}
	return ""
}

// wireCoverageLine is a random pass's summary line: its seed, roots and
// mutant counts.
func wireCoverageLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if wireSeedsOver.MatchString(l) && wireMutants.MatchString(l) {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// wireFuzzAdapter answers the requests: each in every configuration, jobs
// runs at a time, through run (a child wire-fuzz process). A request whose
// inputs do not read, and one in any configuration that does not pass, is a
// failure naming the configuration and why. seed is SCHEMA_TEST_SEED as the
// run handed it in: every random-pass verdict names it, and when it is
// missing or malformed every random-pass row fails, never defaults.
func wireFuzzAdapter(ctx context.Context, in io.Reader, out, log io.Writer, configs []wireConfig, work string, jobs int, seed string,
	run func(ctx context.Context, args []string) (string, error)) error {
	if len(configs) == 0 {
		return fmt.Errorf("wire-fuzz-adapter: no --config names the leg's driver")
	}
	if jobs < 1 {
		return fmt.Errorf("wire-fuzz-adapter: --jobs must be positive")
	}
	reqs, err := testreg.DecodeRequests(in)
	if err != nil {
		return err
	}
	if len(reqs) == 0 {
		return fmt.Errorf("wire-fuzz-adapter: no request on standard input")
	}
	seedN, seedErr := testreg.ParseSeed(seed)
	type job struct{ req, cfg int }
	reasons := make([][]string, len(reqs))
	counts := make([][]string, len(reqs)) // each passing random pass's coverage line, by configuration
	seconds := make([]float64, len(reqs)) // every configuration's run, summed: what the row costs a shard
	var mu sync.Mutex
	jobsCh := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobsCh {
				q, cfg := reqs[j.req], configs[j.cfg]
				failed := filepath.Join(work, fmt.Sprintf("%d-%s", j.req, cfg.Name), "failed.bin")
				var why, count string
				var took float64
				args, err := wireFuzzRunArgs(q, cfg, failed, seedN)
				switch {
				case err != nil:
					why = err.Error()
				case q.Family == wireFuzzFamily && seedErr != nil:
					why = seedErr.Error()
				default:
					start := time.Now()
					o, runErr := run(ctx, args)
					took = time.Since(start).Seconds()
					why = wireRunVerdict(q.Family, runErr, o)
					if why == "" && q.Family == wireFuzzFamily {
						count = wireCoverageLine(o)
					}
				}
				mu.Lock()
				seconds[j.req] += took
				fmt.Fprintf(log, "wire-fuzz-adapter: %s [%s]: %s\n", q.Instance, cfg.Name, map[bool]string{true: "pass", false: "FAIL"}[why == ""])
				if count != "" {
					counts[j.req] = append(counts[j.req], fmt.Sprintf("[%s] %s", cfg.Name, count))
				}
				if why != "" {
					reasons[j.req] = append(reasons[j.req], fmt.Sprintf("[%s] %s", cfg.Name, why))
				}
				mu.Unlock()
			}
		}()
	}
	for i := range reqs {
		for c := range configs {
			jobsCh <- job{i, c}
		}
	}
	close(jobsCh)
	wg.Wait()
	bw := bufio.NewWriter(out)
	for i, q := range reqs {
		r := testreg.Result{Instance: q.Instance, Verdict: testreg.Pass, Seconds: seconds[i]}
		if len(reasons[i]) > 0 {
			sort.Strings(reasons[i])
			r = testreg.Result{Instance: q.Instance, Verdict: testreg.Fail, Reason: strings.Join(reasons[i], "\n"), Seconds: seconds[i]}
		}
		if q.Family == wireFuzzFamily {
			named := fmt.Sprintf("seed %d", seedN)
			if seedErr != nil {
				named = "no seed"
			}
			if r.Reason != "" {
				named += ": " + r.Reason
			} else {
				// a pass names what each configuration covered: the counts a
				// red, or a retired workflow's run, is held against
				sort.Strings(counts[i])
				named += ": " + strings.Join(counts[i], "; ")
			}
			r.Reason = named
		}
		fmt.Fprintln(bw, testreg.FormatResult(r))
	}
	return bw.Flush()
}

// childWireFuzz runs this harness's own wire-fuzz in a child process.
func childWireFuzz(ctx context.Context, args []string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	for i, a := range args {
		if a == "--failed" && i+1 < len(args) {
			if err := os.MkdirAll(filepath.Dir(args[i+1]), 0o755); err != nil {
				return "", err
			}
		}
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	err = cmd.Run()
	return b.String(), err
}

// wireSeedEnv is where schema-test run hands the run's seed in.
const wireSeedEnv = testreg.SeedEnv
