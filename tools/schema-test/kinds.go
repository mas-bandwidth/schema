package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// goTestAdapter runs language-free Go tests: a test whose inputs are
// {"package": "./internal/x", "test": "TestY"} is one top-level Go test, run
// with go test -json and read from its events. A skipped Go test fails: there
// is no skipped verdict.
type goTestAdapter struct{ log io.Writer }

type goTestInputs struct {
	Package string `json:"package"`
	Test    string `json:"test"`
}

func (goTestAdapter) Answers(t testreg.Test) bool { return t.Scope == testreg.LangFree }

var goTestName = regexp.MustCompile(`^(Test|Example|Fuzz)[A-Za-z0-9_]*$`)

func (a goTestAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	byPkg := map[string]map[string]string{} // package -> Go test -> instance
	var pkgs []string
	for _, inst := range instances {
		var in goTestInputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		if !strings.HasPrefix(in.Package, "./") || !goTestName.MatchString(in.Test) {
			return nil, fmt.Errorf("%s: inputs want a ./package and a Go test name, got %q %q", inst.Full, in.Package, in.Test)
		}
		if byPkg[in.Package] == nil {
			byPkg[in.Package] = map[string]string{}
			pkgs = append(pkgs, in.Package)
		}
		byPkg[in.Package][in.Test] = inst.Full
	}
	var out []testreg.Result
	for _, pkg := range pkgs {
		tests := byPkg[pkg]
		names := make([]string, 0, len(tests))
		for n := range tests {
			names = append(names, n)
		}
		cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-json", "-run", "^("+strings.Join(names, "|")+")$", pkg)
		bound(cmd)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		res, err := goTestResults(&stdout, tests)
		out = append(out, res...)
		if err != nil {
			return out, err
		}
		if ctx.Err() != nil {
			return out, fmt.Errorf("go test %s: %w", pkg, ctx.Err())
		}
		// a failing test exits 1 and is a fail verdict already; a build
		// failure reports no test and is caught as unreported
		if runErr != nil && len(res) == 0 {
			return out, fmt.Errorf("go test %s: %w\n%s%s", pkg, runErr, tail(stderr.String(), 40), tail(stdout.String(), 40))
		}
	}
	return out, nil
}

// goTestResults reads go test -json events into one result per asked test.
func goTestResults(r io.Reader, tests map[string]string) ([]testreg.Result, error) {
	type event struct {
		Action, Test, Output string
	}
	output := map[string][]string{}
	var out []testreg.Result
	dec := json.NewDecoder(r)
	for {
		var ev event
		if err := dec.Decode(&ev); err == io.EOF {
			break
		} else if err != nil {
			return out, fmt.Errorf("go test -json: %w", err)
		}
		inst, asked := tests[ev.Test]
		if !asked {
			continue // the package's own lines and subtests
		}
		switch ev.Action {
		case "output":
			output[ev.Test] = append(output[ev.Test], strings.TrimRight(ev.Output, "\n"))
		case "pass":
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Pass})
		case "fail":
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: tail(strings.Join(output[ev.Test], "\n"), 20)})
		case "skip":
			out = append(out, testreg.Result{Instance: inst, Verdict: testreg.Fail, Reason: "the Go test skipped, and nothing is ever skipped: " + tail(strings.Join(output[ev.Test], "\n"), 5)})
		}
	}
	return out, nil
}

