package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"

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
// A row may also carry what its gate needs from the runner (the lint rows):
// "vars", make variables set on the command line (DART=dart, CLANG_FORMAT=
// clang-format-18); "tools", programs that must be on PATH; and "siblings",
// checkouts beside this one (../serialize.go). A tool not on PATH or a
// sibling not there FAILS, naming the runner, before make runs: a gate
// never passes by not finding what it checks with.
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
	// look finds a program on PATH, stat finds a sibling checkout and host
	// names the runner; a unit test hands in its own.
	look func(name string) (string, error)
	stat func(path string) (os.FileInfo, error)
	host func() (string, error)
}

type makeInputs struct {
	File     string            `json:"file"`
	Target   string            `json:"target"`
	Vars     map[string]string `json:"vars,omitempty"`
	Tools    []string          `json:"tools,omitempty"`
	Siblings []string          `json:"siblings,omitempty"`
}

func newMakeAdapter() makeAdapter {
	return makeAdapter{
		exec: execMake,
		read: func(p string) ([]byte, error) { return os.ReadFile(filepath.FromSlash(p)) },
		jobs: runtime.NumCPU(),
		look: exec.LookPath,
		stat: os.Stat,
		host: os.Hostname,
	}
}

// checkoutMake is held by every make that builds in the checkout (a make
// row, a negative control, a harness's build steps): they share build/ and
// bin/schema, and the runner runs adapter groups side by side, so two
// recipes would otherwise write one stamp at once. A golden's writer runs in
// its own copy and holds nothing.
var checkoutMake = make(chan struct{}, 1)

// checkoutHolds counts the holds of checkoutMake, so a unit test sees each
// make in the checkout take it.
var checkoutHolds atomic.Int64

// holdCheckout takes checkoutMake and returns its release. A wait for it
// ends with ctx: the runner's groups run side by side, each under its own
// deadline, and a group whose deadline passes while another make holds the
// checkout fails naming that wait, never running on with a dead context.
func holdCheckout(ctx context.Context) (func(), error) {
	select {
	case checkoutMake <- struct{}{}:
		checkoutHolds.Add(1)
		return func() { <-checkoutMake }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for the checkout, which another make of this run holds: %w", ctx.Err())
	}
}

func execMake(ctx context.Context, argv []string) (string, error) {
	release, err := holdCheckout(ctx)
	if err != nil {
		return "", err
	}
	defer release()
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
	err = cmd.Run()
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
	for k, v := range in.Vars {
		if !makeVarName.MatchString(k) || strings.ContainsAny(v, "\n\x00") {
			return in, fmt.Errorf("make variable %q=%q: want an upper-case name and a one-line value", k, v)
		}
		if makeOwnVar(k) {
			return in, fmt.Errorf("make variable %s is make's own: set on a row it could turn the gate off", k)
		}
	}
	for _, tool := range in.Tools {
		if tool == "" || strings.ContainsAny(tool, "/\\ \t") {
			return in, fmt.Errorf("tool %q: want a program name, found on PATH", tool)
		}
	}
	for _, s := range in.Siblings {
		name, ok := strings.CutPrefix(s, "../")
		if !ok || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
			return in, fmt.Errorf("sibling %q: want ../<checkout>, one directory beside this one", s)
		}
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
		if why := a.needs(in); why != "" {
			out = append(out, testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: why})
			continue
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		argv := []string{"make", "--no-print-directory"}
		if a.jobs > 0 {
			argv = append(argv, fmt.Sprintf("-j%d", a.jobs))
		}
		argv = append(argv, in.Target)
		for _, k := range slices.Sorted(maps.Keys(in.Vars)) {
			argv = append(argv, k+"="+in.Vars[k])
		}
		log, err := a.exec(ctx, argv)
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

// makeVarName is a make variable a row may set: DART, CLANG_FORMAT.
var makeVarName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// makeOwnVar is a variable make itself reads (SHELL runs every recipe,
// MAKEFLAGS takes -i or -n): a row that set one could pass without its
// recipe checking anything.
func makeOwnVar(k string) bool {
	switch k {
	case "SHELL", "MFLAGS", "GNUMAKEFLAGS", "VPATH", "SUFFIXES":
		return true
	}
	return strings.HasPrefix(k, "MAKE")
}

// needs is what the row needs from the runner and the runner lacks, named
// with the runner, or "".
func (a makeAdapter) needs(in makeInputs) string {
	var lack []string
	for _, tool := range in.Tools {
		if _, err := a.look(tool); err != nil {
			lack = append(lack, fmt.Sprintf("the tool %s is not on PATH", tool))
		}
	}
	for _, s := range in.Siblings {
		if info, err := a.stat(filepath.FromSlash(s)); err != nil || !info.IsDir() {
			lack = append(lack, fmt.Sprintf("the sibling checkout %s is not beside this one", s))
		}
	}
	if len(lack) == 0 {
		return ""
	}
	host, err := a.host()
	if err != nil {
		host = fmt.Sprintf("(name unreadable: %v)", err)
	}
	return fmt.Sprintf("make %s needs what runner %s lacks: %s", in.Target, host, strings.Join(lack, "; "))
}

// makeRules is every rule name a make file writes: "name:" at the start of
// a line (several names before one colon are several rules), not a variable
// assignment and not a target-specific variable ("name: export X = 1",
// "name: X := 1"), which sets a variable for a rule and writes no rule.
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
		deps, _, _ := strings.Cut(rest, ";")
		if !ok || strings.HasPrefix(rest, "=") || strings.ContainsAny(names, "=$") || strings.Contains(deps, "=") {
			continue
		}
		for name := range strings.FieldsSeq(names) {
			out[name] = true
		}
	}
	return out, sc.Err()
}
