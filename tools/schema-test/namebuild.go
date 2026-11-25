package main

import (
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
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
	"github.com/mas-bandwidth/schema/v2/tools/namebuild"
)

// nameBuild is the name-buildability family's harness: a row's instance for
// a language is the subtest <case> of tools/namebuild's
// TestNameBuildability_<lang>, which make name-buildability-run runs with the
// toolchains the make files name, writing go test -json events. Its Build
// installs those toolchains once per process (the builder), so languages run
// side by side never install one at the same time.
var nameBuild = harness{
	Name:  namebuild.Family,
	Build: func(string) [][]string { return [][]string{{"make", "name-buildability-deps"}} },
	Run:   runNameBuild,
}

// nameBuildPlan maps each requested subtest's full Go name to its instance
// and returns the go test -run pattern that selects exactly them.
func nameBuildPlan(lang string, instances []testreg.Instance) (map[string]string, string, error) {
	want := map[string]string{}
	var subs []string
	test := namebuild.TestName(lang)
	for _, inst := range instances {
		var in namebuild.Inputs
		d := json.NewDecoder(bytes.NewReader(inst.Test.Inputs))
		d.DisallowUnknownFields()
		if err := d.Decode(&in); err != nil {
			return nil, "", fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		if in.Package != namebuild.Package || in.Test != namebuild.TestPrefix+namebuild.LangPlaceholder || in.Subtest == "" {
			return nil, "", fmt.Errorf("%s: inputs want package %s, test %s and a subtest, got %q %q %q",
				inst.Full, namebuild.Package, namebuild.TestPrefix+namebuild.LangPlaceholder, in.Package, in.Test, in.Subtest)
		}
		full := test + "/" + in.Subtest
		if prev, dup := want[full]; dup {
			return nil, "", fmt.Errorf("%s and %s both name %s", prev, inst.Full, full)
		}
		want[full] = inst.Full
		subs = append(subs, regexp.QuoteMeta(in.Subtest))
	}
	if len(subs) == 0 {
		return nil, "", errors.New("name-buildability: no instance asked for")
	}
	return want, "^" + regexp.QuoteMeta(test) + "$/^(" + strings.Join(subs, "|") + ")$", nil
}

// nameBuildResults reads go test -json events and answers each asked subtest
// by its pass or fail event; a skip is a failure, and an asked subtest with
// no verdict is left out, so the run reports it as not reported.
func nameBuildResults(r io.Reader, want map[string]string) ([]testreg.Result, error) {
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
		inst, asked := want[ev.Test]
		if !asked {
			continue
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

// runNameBuild runs one language's name-buildability instances.
func runNameBuild(ctx context.Context, lang string, instances []testreg.Instance, log io.Writer) ([]testreg.Result, error) {
	return runNameBuildWith(ctx, []string{"make", "-s", "name-buildability-run"}, workRoot, lang, instances, log)
}

// runNameBuildWith runs argv (make name-buildability-run, or a test's
// stand-in) for the instances, in a work folder under root that it removes.
func runNameBuildWith(ctx context.Context, argv []string, root, lang string, instances []testreg.Instance, log io.Writer) (res []testreg.Result, err error) {
	want, pattern, err := nameBuildPlan(lang, instances)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(root, namebuild.Family+"-"+lang+"-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rmErr := os.RemoveAll(work); rmErr != nil && err == nil {
			err = rmErr
		}
	}()
	events := filepath.Join(work, "events.json")
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	bound(cmd)
	cmd.Env = append(os.Environ(), "NAME_BUILDABILITY_RUN="+pattern, "NAME_BUILDABILITY_JSON="+events)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	f, openErr := os.Open(events)
	var parseErr error
	if openErr == nil {
		res, parseErr = nameBuildResults(f, want)
		_ = f.Close() // read-only
	}
	if out.Len() > 0 {
		if _, err := io.Copy(log, &out); err != nil {
			return res, err
		}
	}
	switch {
	case ctx.Err() != nil:
		return res, fmt.Errorf("make name-buildability-run: %w", ctx.Err())
	case openErr != nil:
		return res, fmt.Errorf("make name-buildability-run wrote no events: %w", errors.Join(runErr, openErr))
	case parseErr != nil:
		return res, parseErr
	case runErr != nil && len(res) == 0:
		// a failing subtest exits non-zero too, and its event says so; a run
		// that answered nothing failed before its tests
		return res, fmt.Errorf("make name-buildability-run: %w", runErr)
	}
	return res, nil
}
