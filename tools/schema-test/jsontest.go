package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/jsontestsuite"
	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// The JSONTestSuite corpus (testdata/jsontestsuite/test_parsing,
// docs/SPEC-TABLES.md section 16.12) as registry rows: each vendored file
// is one test, <type>.jsontestsuite.<case>, that every language answers.
// The type is decode for a file the layer accepts and load-refusal for
// one it refuses, as the hostile corpus splits its rows. A language's
// instances of the family are answered by one run of that language's
// whole-corpus Go test, which runs every file once; each instance's
// verdict is the case's line in that run. A language with no such test
// gives each case no verdict, and each instance is red by name.
const (
	// jsontestFamily is the registry family of every JSONTestSuite row.
	jsontestFamily = "jsontestsuite"
	// jsontestCorpus is the corpus folder the rows are read from.
	jsontestCorpus = jsontestsuite.CorpusDir
)

// jsontestInputs is a JSONTestSuite row's inputs.
type jsontestInputs struct {
	Case  string   `json:"case"`
	Files []string `json:"files"`
}

// jsontestRunner is one language's whole-corpus gate: the Go test that
// holds its JSON layer to the suite. A language absent here has no
// runner yet, and the harness fails each of its instances by name.
type jsontestRunner struct {
	pkg, test string
}

var jsontestRunners = map[string]jsontestRunner{
	"go":     {"./internal/codegen/gotable", "TestJSONTestSuiteGenerated"},
	"python": {"./internal/codegen/pythontable", "TestPythonJsonScanPolicy"},
	"moo":    {"./internal/codegen/mootable", "TestMooJSONTestSuite"},
	"luau":   {"./internal/codegen/luautable", "TestLuauJSONTestSuite"},
}

// jsontestCaseKey is a corpus file name as a registry case: the .json
// suffix dropped, lower case, each underscore a separator, and each
// hyphen, dot, plus and hash named, so two files never share a case.
// Case alone would fold them: the check in jsontestKeys refuses any
// two files that still meet, naming both.
func jsontestCaseKey(file string) string {
	base := strings.TrimSuffix(file, ".json")
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	for _, ch := range base {
		switch ch {
		case '_':
			flush()
		case '-':
			flush()
			tokens = append(tokens, "dash")
		case '.':
			flush()
			tokens = append(tokens, "dot")
		case '+':
			flush()
			tokens = append(tokens, "plus")
		case '#':
			flush()
			tokens = append(tokens, "hash")
		default:
			cur.WriteRune(ch)
		}
	}
	flush()
	return strings.Join(tokens, "-")
}

// jsontestKeys is each file's registry case, refusing two files of one
// case, naming both.
func jsontestKeys(files []string) (map[string]string, error) {
	byCase := map[string]string{}
	out := map[string]string{}
	for _, f := range files {
		key := jsontestCaseKey(f)
		if key == "" {
			return nil, fmt.Errorf("%s has no letter or digit to name it by", f)
		}
		if prev, dup := byCase[key]; dup {
			return nil, fmt.Errorf("%s and %s are one registry case, %s", prev, f, key)
		}
		byCase[key] = f
		out[f] = key
	}
	return out, nil
}

// jsontestRows are the registry rows of every corpus file, in file
// order. The expected verdict is the suite's: its section 16.12 row
// where one stands, else accept for a y_ file and refuse for an n_ one.
func jsontestRows(root string) ([]testreg.Test, error) {
	suite, err := jsontestsuite.Load(root)
	if err != nil {
		return nil, err
	}
	names := suite.Names()
	keys, err := jsontestKeys(names)
	if err != nil {
		return nil, err
	}
	out := make([]testreg.Test, 0, len(names))
	for _, file := range names {
		accept := suite.Accept[file]
		reason := ""
		row, ok := suite.Rows[file]
		switch {
		case ok:
			reason = row.Reason
		case strings.HasPrefix(file, "y_"):
			reason = "a y_ file is accepted"
		case strings.HasPrefix(file, "n_"):
			reason = "an n_ file is refused"
		default:
			return nil, fmt.Errorf("%s: an i_ file with no row in the standard", file)
		}
		in, err := json.Marshal(jsontestInputs{
			Case:  file,
			Files: []string{jsontestCorpus + "/" + file},
		})
		if err != nil {
			return nil, err
		}
		t := testreg.Test{
			Family: jsontestFamily,
			Case:   keys[file],
			Tier:   "nightly",
			Inputs: in,
			Source: jsontestCorpus + "/" + file + ":1",
		}
		if accept {
			t.Type = "decode"
			t.Checks = fmt.Sprintf("JSONTestSuite %s: the JSON layer accepts %s (docs/SPEC-TABLES.md 16.12: %s)", file, jsontestCorpus+"/"+file, reason)
			t.Expect = "accepted: the JSON layer accepts the whole text"
		} else {
			t.Type = "load-refusal"
			t.Checks = fmt.Sprintf("JSONTestSuite %s: the JSON layer refuses %s (docs/SPEC-TABLES.md 16.12: %s)", file, jsontestCorpus+"/"+file, reason)
			t.Expect = "refused on load (verdict only)"
		}
		t.Key = t.Type + "." + t.Family + "." + t.Case
		out = append(out, t)
	}
	return out, nil
}

// jsontestHarness is the family's harness: its runner test builds what
// it runs, so there is no build step of its own.
var jsontestHarness = harness{
	Name:  jsontestFamily,
	Build: func(string) [][]string { return nil },
	Run:   runJSONTest,
}

// jsontestPlantEnv is the plant hook's variable for one language: its
// value, flip:<file> or absent:<file>, turns that case's answer around
// or drops it, and the driver must turn exactly that instance red.
func jsontestPlantEnv(lang string) string {
	return "SCHEMA_TEST_PLANT_JSONTEST_" + strings.ToUpper(lang)
}

// jsontestPlant is the plant value for one language in this run: the
// run's own environment, or the plant adapter's through the context.
func jsontestPlant(ctx context.Context, lang string) string {
	key := jsontestPlantEnv(lang)
	if v, ok := testreg.EnvFrom(ctx)[key]; ok {
		return v
	}
	return os.Getenv(key)
}

// jsontestPlan reads the instances' inputs: each asked case by its
// corpus file, and the verdict its row expects.
func jsontestPlan(instances []testreg.Instance) (want map[string]string, err error) {
	want = map[string]string{}
	for _, inst := range instances {
		var in jsontestInputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		if in.Case == "" || len(in.Files) != 1 || in.Files[0] != jsontestCorpus+"/"+in.Case {
			return nil, fmt.Errorf("%s: inputs want case and its one corpus file, got %q %q", inst.Full, in.Case, in.Files)
		}
		if inst.Test.Type != "decode" && inst.Test.Type != "load-refusal" {
			return nil, fmt.Errorf("%s: type %s is no JSONTestSuite verdict", inst.Full, inst.Test.Type)
		}
		if prev, dup := want[in.Case]; dup {
			return nil, fmt.Errorf("%s and %s both name the case %s", prev, inst.Full, in.Case)
		}
		want[in.Case] = inst.Full
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("%s: no instance asked for", jsontestFamily)
	}
	return want, nil
}

// jsontestFailures is each asked file its runner named as disagreeing:
// the line that names it, from the runner's mismatch and pending lines.
// A runner with empty PENDING names every disagreement a mismatch, so
// both spellings are failures here: the registry allows no pending.
func jsontestFailures(output string, files []string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.Contains(line, "mismatch:") && !strings.Contains(line, "pending:") && !strings.Contains(line, ": want ") {
			continue
		}
		for _, f := range files {
			if _, seen := out[f]; !seen && strings.Contains(line, f) {
				out[f] = strings.TrimSpace(line)
			}
		}
	}
	return out
}

// parseJSONTestPlant is a plant value as its mode and its file.
func parseJSONTestPlant(plant string) (mode, file string, ok bool) {
	mode, file, found := strings.Cut(plant, ":")
	if !found || file == "" || (mode != "flip" && mode != "absent") {
		return "", "", false
	}
	return mode, file, true
}

// jsontestVerdicts answers each asked case from one runner run's
// output. A run that never reports its test, or that fails with no
// case failing by name, fails every asked case with what it said.
func jsontestVerdicts(lang string, instances []testreg.Instance, testName, output string, runErr error, plant string) ([]testreg.Result, error) {
	want, err := jsontestPlan(instances)
	if err != nil {
		return nil, err
	}
	files := slices.Sorted(maps.Keys(want))
	failAll := func(why string) []testreg.Result {
		var all []testreg.Result
		for _, name := range files {
			all = append(all, testreg.Result{Instance: want[name], Verdict: testreg.Fail, Reason: why})
		}
		return all
	}
	if !strings.Contains(output, "--- PASS: "+testName) && !strings.Contains(output, "--- FAIL: "+testName) {
		return failAll(fmt.Sprintf("go test %s gave the cases no verdict (exit: %v): %s", testName, runErr, oneLine(tail(output, 5)))), nil
	}
	failures := jsontestFailures(output, files)
	if runErr != nil && len(failures) == 0 {
		return failAll(fmt.Sprintf("go test %s failed with no case failing by name (exit: %v): %s", testName, runErr, oneLine(tail(output, 5)))), nil
	}
	mode, plantFile, planted := parseJSONTestPlant(plant)
	var res []testreg.Result
	for _, name := range files {
		inst := want[name]
		r := testreg.Result{Instance: inst, Verdict: testreg.Pass}
		if why, bad := failures[name]; bad {
			r = testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: why}
		}
		if planted && plantFile == name {
			switch mode {
			case "absent":
				r = testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: fmt.Sprintf("MISSING: go test %s gave the case no verdict: the planted absent dropped %s for %s", testName, name, lang)}
			case "flip":
				if r.Verdict == testreg.Pass {
					r = testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: fmt.Sprintf("the planted flip turned %s around for %s: the runner accepted what it must refuse, or refused what it must accept", name, lang)}
				} else {
					r = testreg.Result{Instance: inst, Verdict: testreg.Pass}
				}
			}
		}
		res = append(res, r)
	}
	return res, nil
}

// execJSONTestSuite runs one language's whole-corpus gate once and
// returns its combined output.
func execJSONTestSuite(ctx context.Context, pkg, test string) (string, error) {
	release, err := holdCheckout(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	cmd := exec.CommandContext(ctx, "go", "test", pkg, "-run", "^"+test+"$", "-count=1", "-v")
	bound(cmd)
	if env := testreg.EnvFrom(ctx); len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	return out.String(), err
}

// runJSONTest runs one language's JSONTestSuite instances.
func runJSONTest(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	want, err := jsontestPlan(instances)
	if err != nil {
		return nil, err
	}
	runner, ok := jsontestRunners[lang]
	if !ok {
		mode, plantFile, planted := parseJSONTestPlant(jsontestPlant(ctx, lang))
		var res []testreg.Result
		for _, name := range slices.Sorted(maps.Keys(want)) {
			reason := fmt.Sprintf("no JSONTestSuite runner for %s: no Go test holds its JSON layer to the suite, so the case is unanswered in %s", lang, lang)
			if planted && plantFile == name && mode == "absent" {
				reason = fmt.Sprintf("MISSING: no JSONTestSuite runner for %s gave the case no verdict: the planted absent dropped %s", lang, name)
			}
			res = append(res, testreg.Result{Instance: want[name], Verdict: testreg.Fail, Reason: reason})
		}
		return res, nil
	}
	suite, err := jsontestsuite.Load(".")
	if err != nil {
		return nil, err
	}
	for name, inst := range want {
		if _, held := suite.Files[name]; !held {
			return nil, fmt.Errorf("%s names the case %s, which %s does not register", inst, name, jsontestCorpus)
		}
	}
	out, runErr := execJSONTestSuite(ctx, runner.pkg, runner.test)
	if _, err := io.WriteString(log, out); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("go test %s: %w", runner.test, ctx.Err())
	}
	return jsontestVerdicts(lang, instances, runner.test, out, runErr, jsontestPlant(ctx, lang))
}
