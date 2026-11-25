package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// makeAdapter runs make gates: a test whose inputs are {"file": "make/c.mk",
// "target": "tables-c-clean"} is one make target, and it passes when the
// target exits 0. The file is where the rule is written; make itself runs
// the root Makefile, which includes every make/*.mk.
//
// Before anything runs, every asked target must be a rule its named file
// writes: a row whose target was renamed or removed is a test that does not
// exist, and it fails by name instead of running something else. A target
// whose log reports a skip fails: nothing is ever skipped.
//
// The targets of one call run one after another, never side by side: they
// share build/ (bin/schema, the generated trees and their stamps), and two
// recipes writing one stamp at once is a race the targets were not written
// for.
type makeAdapter struct {
	// exec runs one make command line and returns its combined output; a
	// unit test hands in its own, so no unit test runs make.
	exec func(ctx context.Context, argv []string) (string, error)
	// read reads a make file by its repository path; a unit test hands in
	// its own, so no unit test reads the tree.
	read func(path string) ([]byte, error)
	// jobs is make's -j: the cores for a make row, as the workflows ran
	// them (make -j"$(nproc)"); 0 for a negative control, which the
	// controls job ran with no -j.
	jobs int
}

type makeInputs struct {
	File   string `json:"file"`
	Target string `json:"target"`
}

func newMakeAdapter() makeAdapter {
	return makeAdapter{exec: execMake, read: func(p string) ([]byte, error) { return os.ReadFile(filepath.FromSlash(p)) }, jobs: runtime.NumCPU()}
}

func execMake(ctx context.Context, argv []string) (string, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
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
	err := cmd.Run()
	return out.String(), err
}

func (makeAdapter) Answers(t testreg.Test) bool {
	if t.Scope != testreg.LangFree {
		return false
	}
	_, err := decodeMakeInputs(t.Inputs)
	return err == nil
}

func decodeMakeInputs(raw json.RawMessage) (makeInputs, error) {
	var in makeInputs
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return in, err
	}
	if in.File == "" || in.Target == "" || strings.HasPrefix(in.Target, "-") || strings.ContainsAny(in.Target, " \t=") {
		return in, fmt.Errorf("want a make file and one target, got %q %q", in.File, in.Target)
	}
	if in.File != "Makefile" && !strings.HasSuffix(in.File, ".mk") || in.File != path.Clean(in.File) || strings.HasPrefix(in.File, "../") || path.IsAbs(in.File) {
		return in, fmt.Errorf("make file %q is not the Makefile or a .mk file of the repository", in.File)
	}
	return in, nil
}

func (a makeAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	ins := make([]makeInputs, len(instances))
	for i, inst := range instances {
		in, err := decodeMakeInputs(inst.Test.Inputs)
		if err != nil {
			return nil, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		ins[i] = in
	}
	var out []testreg.Result
	rules := map[string]map[string]bool{}
	for i, inst := range instances {
		in := ins[i]
		if rules[in.File] == nil {
			data, err := a.read(in.File)
			if err != nil {
				return out, fmt.Errorf("%s: make file: %w", inst.Full, err)
			}
			r, err := makeRules(data)
			if err != nil {
				return out, fmt.Errorf("%s: %w", inst.Full, err)
			}
			rules[in.File] = r
		}
		if !rules[in.File][in.Target] {
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: fmt.Sprintf("%s writes no rule %q: the target this row runs does not exist", in.File, in.Target)})
			continue
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		argv := []string{"make", "--no-print-directory"}
		if a.jobs > 0 {
			argv = append(argv, fmt.Sprintf("-j%d", a.jobs))
		}
		log, err := a.exec(ctx, append(argv, in.Target))
		if ctx.Err() != nil {
			return out, fmt.Errorf("make %s: %w\n%s", in.Target, ctx.Err(), tail(log, 40))
		}
		lines := strings.Split(log, "\n")
		switch skipErr := testreg.RefuseSkips(lines); {
		case err != nil:
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: fmt.Sprintf("make %s: %v\n%s", in.Target, err, tail(log, 30))})
		case skipErr != nil:
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
				Reason: fmt.Sprintf("make %s exited 0, and %v", in.Target, skipErr)})
		default:
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Pass})
		}
	}
	return out, nil
}

// makeRules is every rule name a make file writes: "name:" at the start of
// a line (several names before one colon are several rules), not a variable
// assignment.
func makeRules(data []byte) (map[string]bool, error) {
	out := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '\t' || line[0] == '#' || line[0] == ' ' {
			continue
		}
		names, rest, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(rest, "=") || strings.ContainsAny(names, "=$") {
			continue
		}
		for name := range strings.FieldsSeq(names) {
			out[name] = true
		}
	}
	return out, sc.Err()
}
