package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// The shared hostile corpus (internal/ci/hostilecorpus/corpus/manifest.tsv)
// as registry rows: each manifest case is one test, <type>.hostile.<case>,
// that every language answers. A language's instances are answered by one
// run of its corpus target, make schema-<lang>-runs-hostile-corpus, which
// runs every case once; each instance's verdict is the case's line in that
// run (hostilecorpus.ReadVerdicts). No case runs twice: the per-case rows
// are the make target's only row, and the Go test a target runs is claimed
// by them too (inputs "go_tests"; extract.CoveredIn).
const (
	hostileFamily = "hostile"
	// hostileCorpus is the corpus folder the rows are read from.
	hostileCorpus = "internal/ci/hostilecorpus/corpus"
	// hostileTarget is every language's corpus target.
	hostileTarget = "schema-" + testreg.LangPlaceholder + "-runs-hostile-corpus"
)

// hostileGoTests are the Go tests a Go-written corpus target runs, by
// package: each is TestHostileCorpus_<lang>. A language whose target runs
// no Go test has none in either package, and claims nothing here.
var hostileGoTests = []string{
	"./internal/ci TestHostileCorpus_" + testreg.LangPlaceholder,
	"./internal/codegen/pythontable TestHostileCorpus_" + testreg.LangPlaceholder,
}

// hostileInputs is a hostile row's inputs.
type hostileInputs struct {
	Target  string   `json:"target"`
	Case    string   `json:"case"`
	Input   string   `json:"input"`
	GoTests []string `json:"go_tests"`
}

var hostileHarness = harness{
	Name:  hostileFamily,
	Build: func(string) [][]string { return nil }, // the target builds what it runs
	Run:   runHostile,
}

// hostileCaseKey is a manifest name as a registry case: lower case, digits
// and single dashes (testreg's case rule); an underscore is a dash.
func hostileCaseKey(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

// hostileRows are the registry rows of every case the corpus folder
// registers, in manifest order. Two names that make one case key are
// refused, naming both.
func hostileRows(root string) ([]testreg.Test, error) {
	dir := filepath.Join(root, filepath.FromSlash(hostileCorpus))
	cases, err := hostilecorpus.Registered(dir)
	if err != nil {
		return nil, err
	}
	lines, err := hostileManifestLines(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]string{}
	var out []testreg.Test
	for _, c := range cases {
		key := hostileCaseKey(c.Name)
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s and %s are one registry case, %s", prev, c.Name, key)
		}
		seen[key] = c.Name
		in, err := json.Marshal(hostileInputs{
			Target:  hostileTarget,
			Case:    c.Name,
			Input:   hostileCorpus + "/" + c.Input,
			GoTests: hostileGoTests,
		})
		if err != nil {
			return nil, err
		}
		t := testreg.Test{
			Family: hostileFamily,
			Case:   key,
			// a toolchain per language (functional), and each run is the
			// whole corpus, which the retired workflows ran only nightly
			Tier:   "nightly",
			Inputs: in,
			Source: fmt.Sprintf("%s/%s:%d", hostileCorpus, hostilecorpus.ManifestName, lines[c.Name]),
		}
		if c.Verdict == hostilecorpus.Refuse {
			t.Type = "load-refusal"
			t.Checks = fmt.Sprintf("shared hostile corpus %s: the %s reader refuses %s as %s.%s", c.Name, c.Carrier, c.Input, c.Schema, c.Table)
			t.Expect = "refused on load (verdict only)"
		} else {
			t.Type = "decode"
			t.Checks = fmt.Sprintf("shared hostile corpus %s: the %s reader reads %s as %s.%s to the engine's value", c.Name, c.Carrier, c.Input, c.Schema, c.Table)
			t.Expect = "accepted, and the decoded value's digest is the manifest's"
		}
		t.Key = t.Type + "." + t.Family + "." + t.Case
		out = append(out, t)
	}
	return out, nil
}

// hostileManifestLines is each case's line number in the manifest.
func hostileManifestLines(dir string) (map[string]int, error) {
	data, err := os.ReadFile(filepath.Join(dir, hostilecorpus.ManifestName))
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, _ := strings.Cut(line, "\t")
		out[name] = i + 1
	}
	return out, nil
}

// hostilePlan reads the instances' inputs: the target they share and each
// asked case by its manifest name.
func hostilePlan(lang string, instances []testreg.Instance) (target string, want map[string]string, err error) {
	want = map[string]string{}
	for _, inst := range instances {
		var in hostileInputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return "", nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		if in.Target != hostileTarget || in.Case == "" {
			return "", nil, fmt.Errorf("%s: inputs want target %s and a case, got %q %q", inst.Full, hostileTarget, in.Target, in.Case)
		}
		if prev, dup := want[in.Case]; dup {
			return "", nil, fmt.Errorf("%s and %s both name the case %s", prev, inst.Full, in.Case)
		}
		want[in.Case] = inst.Full
	}
	if len(want) == 0 {
		return "", nil, errors.New("hostile: no instance asked for")
	}
	return strings.ReplaceAll(hostileTarget, testreg.LangPlaceholder, lang), want, nil
}

// runHostile runs one language's corpus target once and answers each asked
// case from its lines.
func runHostile(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	return runHostileWith(ctx, execMake, workRoot, lang, instances, log)
}

// runHostileWith runs the target through run (execMake, or a test's
// stand-in), with the verdicts file for a Go-written runner in a work folder
// under root that it removes.
func runHostileWith(ctx context.Context, run func(context.Context, []string) (string, error), root, lang string, instances []testreg.Instance, log io.Writer) (res []testreg.Result, err error) {
	target, want, err := hostilePlan(lang, instances)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(root, hostileFamily+"-"+lang+"-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rmErr := os.RemoveAll(work); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	verdicts, err := filepath.Abs(filepath.Join(work, "verdicts.log"))
	if err != nil {
		return nil, err
	}
	out, runErr := run(testreg.WithEnv(ctx, map[string]string{hostilecorpus.VerdictsEnv: verdicts}), []string{"make", "-s", target})
	if _, err := io.WriteString(log, out); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("make %s: %w", target, ctx.Err())
	}
	// every case the folder registers, so a FAIL for a case not asked is
	// still a case's (it explains a failed exit), never a stray line
	cases, err := hostilecorpus.Registered(filepath.FromSlash(hostileCorpus))
	if err != nil {
		return nil, err
	}
	registered := make([]string, 0, len(cases))
	for _, c := range cases {
		registered = append(registered, c.Name)
	}
	for name, inst := range want {
		if !slices.Contains(registered, name) {
			return nil, fmt.Errorf("%s names the case %s, which %s does not register", inst, name, hostileCorpus)
		}
	}
	// a Go-written runner's lines are in the verdicts file; every other
	// runner's are on the target's output
	text := out
	if data, rerr := os.ReadFile(verdicts); rerr == nil {
		text += "\n" + string(data)
	} else if !errors.Is(rerr, os.ErrNotExist) {
		return nil, rerr
	}
	v, err := hostilecorpus.ReadVerdicts(strings.NewReader(text), registered)
	if err != nil {
		return nil, err
	}
	if v.Blocks == 0 {
		return nil, fmt.Errorf("make %s closed no hostile-ledger block, so no case has a verdict (exit: %v)\n%s", target, runErr, tail(out, 30))
	}
	if len(v.Other) > 0 {
		// a carried finding, a ledger that did not close, a case the
		// manifest does not register: none is a case's verdict, and each
		// is the run's failure
		return nil, fmt.Errorf("make %s: %d line(s) in its verdict block are no case's verdict:\n%s", target, len(v.Other), tail(strings.Join(v.Other, "\n"), 20))
	}
	unanswered := 0
	for _, c := range cases {
		if !v.Ran[c.Name] {
			unanswered++
		}
	}
	for _, name := range slices.Sorted(maps.Keys(want)) {
		inst := want[name]
		switch why, bad := v.Failed[name]; {
		case bad:
			res = append(res, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: why})
		case v.Ran[name]:
			res = append(res, testreg.Result{Instance: inst, Verdict: testreg.Pass})
		default:
			res = append(res, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "make " + target + " gave the case no verdict: no line says it ran"})
		}
	}
	if runErr != nil && len(v.Failed) == 0 && unanswered == 0 {
		// every case ran and none failed, and the target failed: its
		// failure is in something no case shows, which is the run's
		return res, fmt.Errorf("make %s failed with every case passing: %w\n%s", target, runErr, tail(out, 30))
	}
	return res, nil
}
