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
// installs the language's own toolchain, and only when it is not in place
// (make name-buildability-deps-<lang>), once per process (the builder): a
// language's compile row, which the pr job runs, never pays for the other
// languages' toolchains; and it builds the compiler make
// name-buildability-run links (build/schema-moo), once, before any call, so
// calls running side by side never build it at once.
var nameBuild = harness{
	Name: namebuild.Family,
	Build: func(lang string) [][]string {
		return [][]string{{"make", "name-buildability-deps-" + lang}, {"make", "build/schema-moo"}}
	},
	Run: runNameBuild,
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
// no verdict is left out, so the run reports it as not reported. other is
// what the rest of the run said: the parent test's output (a failed setup),
// the package's (a TestMain failure) and the build's (a test that does not
// compile), which names the cause when no asked subtest answered.
func nameBuildResults(r io.Reader, want map[string]string) (out []testreg.Result, other []string, err error) {
	type event struct {
		Action, Test, Output string
	}
	output := map[string][]string{}
	dec := json.NewDecoder(r)
	for {
		var ev event
		if err := dec.Decode(&ev); err == io.EOF {
			break
		} else if err != nil {
			return out, other, fmt.Errorf("go test -json: %w", err)
		}
		inst, asked := want[ev.Test]
		if !asked {
			if (ev.Action == "output" || ev.Action == "build-output") && ev.Output != "" {
				other = append(other, strings.TrimRight(ev.Output, "\n"))
			}
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
	return out, other, nil
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
	// a plant's environment, as runHarness hands it to the other harnesses
	for k, v := range testreg.EnvFrom(ctx) {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	f, openErr := os.Open(events)
	var parseErr error
	var other []string
	if openErr == nil {
		res, other, parseErr = nameBuildResults(f, want)
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
		// that answered nothing failed before its tests, and what the rest of
		// the run said names why
		return res, fmt.Errorf("make name-buildability-run: %w\n%s", runErr, tail(strings.Join(other, "\n"), 20))
	case len(res) == 0:
		// exit 0 with no verdict: the -run pattern matched no test (a
		// language with no TestNameBuildability_<lang>)
		return res, fmt.Errorf("make name-buildability-run answered none of the %d asked: %s matched no test\n%s", len(want), pattern, tail(strings.Join(other, "\n"), 20))
	}
	return res, nil
}
