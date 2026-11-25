package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// plantAdapter runs negative controls. A plant passes only when the
// sabotage it plants turns exactly the tests it names red. Its inputs take
// one of three forms:
//
//   - {"target": <full name>, "env": {...}, "reason": <text>}: the target runs
//     with env set, which turns on a hook its harness carries; it must run and
//     fail for the stated reason: its FAIL reason holds the text. A red for
//     any other reason (a missing tool, a build that broke) is not the
//     plant's red, and fails the plant.
//   - {"edits": [{"file", "anchor", "with"}...], "red": [<full name>...],
//     "run": [<full name>...], "reason": <text>}: the plant contract (docs/TEST-REGISTRY.md,
//     "Plants"). Each edit replaces its anchor in a copy of the tracked tree,
//     and the anchor must be in its file exactly once (0 or 2+ fails, named,
//     per edit); an anchor also written in another source file of the same
//     kind that no edit names fails, so a plant reaches every copy (G21).
//     The named tests then run in that copy through their own adapters, so
//     the build is the leg's own, flags and asserts included. Every name in
//     red must fail for the stated reason (its FAIL reason holds the text);
//     every other name in run must pass: a sabotage that reddens a row it
//     does not name, or a named row for another reason, fails the plant.
//   - {"make": <target>, "group": <group>}: a negative control of
//     make/negative-controls.json, whose recipe plants its sabotage and
//     holds its gate red itself; it passes when the target exits 0 with no
//     skip in its log.
type plantAdapter struct {
	rn   *testreg.Runner
	make makeAdapter
	// tree runs the edit form: copy the tracked tree, apply the edits and
	// run the names there, returning each name's status. A unit test hands
	// in its own, so no unit test copies a tree or starts a process.
	tree func(ctx context.Context, edits []plantEdit, names []string) (map[string]outcome, string, error)
}

// outcome is one name's status and reason, as its run reported them.
type outcome struct{ status, reason string }

type plantInputs struct {
	Target string            `json:"target"`
	Env    map[string]string `json:"env"`
	Edits  []plantEdit       `json:"edits"`
	Red    []string          `json:"red"`
	Run    []string          `json:"run"`
	Reason string            `json:"reason"`
	Make   string            `json:"make"`
	Group  string            `json:"group"`
	File   string            `json:"file"`
}

// plantEdit is one exact replacement: anchor, in file, becomes with.
type plantEdit struct {
	File   string `json:"file"`
	Anchor string `json:"anchor"`
	With   string `json:"with"`
}

func newPlantAdapter(rn *testreg.Runner) plantAdapter {
	serial := newMakeAdapter()
	serial.jobs = 0 // the controls ran with no -j
	return plantAdapter{rn: rn, make: serial, tree: plantInTree}
}

func decodePlant(raw json.RawMessage) (plantInputs, string, error) {
	var in plantInputs
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		return in, "", err
	}
	env := in.Target != "" || len(in.Env) > 0
	edits := len(in.Edits) > 0 || len(in.Red) > 0 || len(in.Run) > 0
	mk := in.Make != "" || in.Group != "" || in.File != ""
	if mk && in.Reason != "" {
		return in, "", fmt.Errorf("a make control holds its own gate red: it states no reason")
	}
	switch {
	case env && !edits && !mk:
		if in.Target == "" || len(in.Env) == 0 || in.Reason == "" {
			return in, "", fmt.Errorf("an env plant names its target, the env that breaks it and the reason the target fails for")
		}
		return in, "env", nil
	case edits && !env && !mk:
		if len(in.Edits) == 0 || len(in.Red) == 0 || in.Reason == "" {
			return in, "", fmt.Errorf("an edit plant names its edits, the tests it turns red and the reason they fail for")
		}
		for i, e := range in.Edits {
			if e.File == "" || e.Anchor == "" || e.Anchor == e.With || filepath.IsAbs(e.File) || strings.Contains(e.File, "..") {
				return in, "", fmt.Errorf("edit %d: want a repository file, an anchor and a different replacement", i+1)
			}
		}
		return in, "edits", nil
	case mk && !env && !edits:
		if in.Make == "" || in.Group == "" || in.File == "" {
			return in, "", fmt.Errorf("a make control names its target, its make file and its group")
		}
		return in, "make", nil
	}
	return in, "", fmt.Errorf("plant inputs are one form: {target, env, reason}, {edits, red, run, reason} or {make, file, group}")
}

func (plantAdapter) Answers(t testreg.Test) bool {
	if t.Scope != testreg.LangFree || t.Type != "plant" {
		return false
	}
	_, _, err := decodePlant(t.Inputs)
	return err == nil
}

func (a plantAdapter) Run(ctx context.Context, _ string, instances []testreg.Instance) ([]testreg.Result, error) {
	var out []testreg.Result
	for _, inst := range instances {
		in, form, err := decodePlant(inst.Test.Inputs)
		if err != nil {
			return out, fmt.Errorf("%s: inputs: %w", inst.Full, err)
		}
		var r testreg.Result
		switch form {
		case "env":
			r, err = a.runEnv(ctx, inst, in)
		case "edits":
			r, err = a.runEdits(ctx, inst, in)
		case "make":
			var res []testreg.Result
			mi := inst
			if mi.Test.Inputs, err = json.Marshal(makeInputs{File: in.File, Target: in.Make}); err != nil {
				return out, fmt.Errorf("%s: %w", inst.Full, err)
			}
			res, err = a.make.Run(ctx, "", []testreg.Instance{mi})
			if len(res) == 1 {
				r = res[0]
				// a control whose recipe ran and failed did not hold its gate
				// red; a control that never ran (its rule gone) says so itself
				if r.Verdict == testreg.Fail && strings.HasPrefix(r.Reason, "make "+in.Make) {
					r.Reason = "the control did not hold its gate red: " + r.Reason
				}
			}
		}
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (a plantAdapter) runEnv(ctx context.Context, inst testreg.Instance, in plantInputs) (testreg.Result, error) {
	target, err := a.rn.Reg.Resolve(in.Target)
	if err != nil {
		return testreg.Result{}, fmt.Errorf("%s: %w", inst.Full, err)
	}
	if target.Test.Type == "plant" {
		return testreg.Result{}, fmt.Errorf("%s: a plant's target is a test that is not a plant", inst.Full)
	}
	rep := a.rn.Run(testreg.WithEnv(ctx, in.Env), []testreg.Instance{target})
	var o testreg.Outcome
	for _, x := range rep.Outcomes {
		if x.Full == target.Full {
			o = x
		}
	}
	got := map[string]outcome{}
	if o.Full != "" {
		got[o.Full] = outcome{o.Status, o.Reason}
	}
	bad := judgePlant([]string{target.Full}, in.Reason, []string{target.Full}, got)
	if bad == "" && rep.Ran == 1 && len(rep.Problems) == 0 {
		return testreg.Result{Instance: inst.Full, Verdict: testreg.Pass}, nil
	}
	if bad == "" {
		bad = fmt.Sprintf("the planted run ran %d, with problems %v", rep.Ran, rep.Problems)
	}
	return testreg.Result{Instance: inst.Full, Verdict: testreg.Fail,
		Reason: fmt.Sprintf("planted %v: %s", in.Env, bad)}, nil
}

func (a plantAdapter) runEdits(ctx context.Context, inst testreg.Instance, in plantInputs) (testreg.Result, error) {
	names, err := a.plantNames(inst, in)
	if err != nil {
		return testreg.Result{}, err
	}
	status, log, err := a.tree(ctx, in.Edits, names)
	if err != nil {
		var pe plantEditError
		if asEditError(err, &pe) {
			// the sabotage itself did not land as written: the plant is red,
			// named, and nothing ran against an unsabotaged tree
			return testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: pe.Error()}, nil
		}
		return testreg.Result{}, fmt.Errorf("%s: %w\n%s", inst.Full, err, tail(log, 30))
	}
	if bad := judgePlant(in.Red, in.Reason, names, status); bad != "" {
		return testreg.Result{Instance: inst.Full, Verdict: testreg.Fail, Reason: bad + "\n" + tail(log, 30)}, nil
	}
	return testreg.Result{Instance: inst.Full, Verdict: testreg.Pass}, nil
}

// plantNames is the plant's whole run: the names in run, with every name in
// red among them. Each must be a test that exists and is not a plant.
func (a plantAdapter) plantNames(inst testreg.Instance, in plantInputs) ([]string, error) {
	seen := map[string]bool{}
	var names []string
	for _, n := range append(append([]string{}, in.Red...), in.Run...) {
		if seen[n] {
			continue
		}
		seen[n] = true
		t, err := a.rn.Reg.Resolve(n)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inst.Full, err)
		}
		if t.Test.Type == "plant" {
			return nil, fmt.Errorf("%s: %s is a plant, and a plant runs tests that are not", inst.Full, n)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// judgePlant is the plant's verdict over the outcomes its run reported:
// every red name FAIL with a reason whose first line holds the stated
// reason (the line the harness writes, never a log tail a broken build
// echoes the planted source into), every
// other name PASS. A red for another reason is not the plant's red: a
// missing tool or a broken build reddens a row without the sabotage having
// been caught. It returns what is wrong, or "".
func judgePlant(red []string, reason string, names []string, status map[string]outcome) string {
	want := map[string]bool{}
	for _, n := range red {
		want[n] = true
	}
	var bad []string
	for _, n := range names {
		got, ok := status[n]
		switch {
		case !ok:
			bad = append(bad, fmt.Sprintf("%s did not report", n))
		case want[n] && got.status != testreg.StatusFail:
			bad = append(bad, fmt.Sprintf("%s was %s, and the plant names it red", n, got.status))
		case want[n] && (reason == "" || !strings.Contains(firstLine(got.reason), reason)):
			bad = append(bad, fmt.Sprintf("%s was red for another reason than the plant states (%q): %s", n, reason, firstLine(got.reason)))
		case !want[n] && got.status != testreg.StatusPass:
			bad = append(bad, fmt.Sprintf("%s was %s, and the plant does not name it: a sabotage reddens only the rows it names", n, got.status))
		}
	}
	if len(bad) == 0 {
		return ""
	}
	return "the planted run is not exactly its named rows red for the stated reason: " + strings.Join(bad, "; ")
}

// plantEditError is an edit that did not land exactly once, or a copy of
// its anchor that no edit reaches.
type plantEditError struct{ msg string }

func (e plantEditError) Error() string { return e.msg }

func asEditError(err error, pe *plantEditError) bool {
	return errors.As(err, pe)
}

// applyEdits applies the edits to files (by repository path) and returns
// the edited contents. Each anchor must be in its file exactly once, after
// the edits before it; and an anchor written in another file of sources (a
// file of the same extension, not a test, where no edit sabotages that same
// anchor) is a copy the plant does not reach.
func applyEdits(edits []plantEdit, read func(string) ([]byte, error), sources []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	reached := map[string]bool{}
	for _, e := range edits {
		reached[e.File+"\x00"+e.Anchor] = true
	}
	for i, e := range edits {
		data, ok := out[e.File]
		if !ok {
			var err error
			if data, err = read(e.File); err != nil {
				return nil, plantEditError{fmt.Sprintf("edit %d: %s: %v", i+1, e.File, err)}
			}
		}
		switch n := bytes.Count(data, []byte(e.Anchor)); n {
		case 1:
		case 0:
			return nil, plantEditError{fmt.Sprintf("edit %d: the anchor is not in %s (0 matches): the code it sabotages moved, so the plant would test nothing: %q", i+1, e.File, firstLine(e.Anchor))}
		default:
			return nil, plantEditError{fmt.Sprintf("edit %d: the anchor is in %s %d times, not exactly once: name a longer anchor: %q", i+1, e.File, n, firstLine(e.Anchor))}
		}
		out[e.File] = bytes.Replace(data, []byte(e.Anchor), []byte(e.With), 1)
	}
	for i, e := range edits {
		ext := filepath.Ext(e.File)
		for _, src := range sources {
			if src == e.File || reached[src+"\x00"+e.Anchor] || filepath.Ext(src) != ext || isTestSource(src) {
				continue // its own file, or a copy another edit sabotages too
			}
			data, err := read(src)
			if err != nil {
				return nil, plantEditError{fmt.Sprintf("edit %d: reading %s for copies of the anchor: %v", i+1, src, err)}
			}
			if bytes.Contains(data, []byte(e.Anchor)) {
				return nil, plantEditError{fmt.Sprintf("edit %d: the anchor is also written in %s, which no edit names: a plant reaches every copy of the code it sabotages (G21): %q", i+1, src, firstLine(e.Anchor))}
			}
		}
	}
	return out, nil
}

func isTestSource(p string) bool {
	return strings.HasSuffix(p, "_test.go") || strings.Contains("/"+p, "/testdata/") || strings.HasPrefix(p, "docs/")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// plantInTree is the edit form for real: the tracked tree is copied beside
// the checkout (so ../serialize and the other siblings resolve as they do
// for the checkout), the edits are applied to the copy, and the copy's own
// schema-test, built from the sabotaged tree, runs the names there: a plant
// of the runner itself reaches the runner. The copy is removed afterwards.
func plantInTree(ctx context.Context, edits []plantEdit, names []string) (map[string]outcome, string, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	files, err := trackedFiles(ctx, root)
	if err != nil {
		return nil, "", err
	}
	edited, err := applyEdits(edits, func(p string) ([]byte, error) { return os.ReadFile(filepath.Join(root, filepath.FromSlash(p))) }, files)
	if err != nil {
		return nil, "", err
	}
	work, err := os.MkdirTemp(filepath.Dir(root), ".schema-test-plant-")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = os.RemoveAll(work) }()
	for _, f := range files {
		if err := copyFile(filepath.Join(root, filepath.FromSlash(f)), filepath.Join(work, filepath.FromSlash(f))); err != nil {
			return nil, "", err
		}
	}
	for f, data := range edited {
		if err := os.WriteFile(filepath.Join(work, filepath.FromSlash(f)), data, 0o644); err != nil {
			return nil, "", err
		}
	}
	self := filepath.Join(work, ".schema-test-planted")
	build := exec.CommandContext(ctx, "go", "build", "-o", self, "./tools/schema-test")
	bound(build)
	build.Dir = work
	if log, err := build.CombinedOutput(); err != nil {
		return nil, string(log), fmt.Errorf("building schema-test in the planted tree: %w", err)
	}
	cmd := exec.CommandContext(ctx, self, append([]string{"run", "--timeout", "0"}, names...)...)
	bound(cmd)
	cmd.Dir = work
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	status, err := parseOutcomes(out.String())
	if err != nil {
		return nil, out.String(), err
	}
	if len(status) == 0 {
		return nil, out.String(), fmt.Errorf("the planted run reported no outcome: %w", runErr)
	}
	return status, out.String(), nil
}

// parseOutcomes reads a run's printed outcomes ("PASS <name>", "FAIL
// <name>: <reason>", ...) into each name's status and reason. A reason runs
// on over the lines after its outcome line, up to the next outcome line, a
// run-level line ("FAIL run: ...") or the run's closing count.
func parseOutcomes(log string) (map[string]outcome, error) {
	out := map[string]outcome{}
	last := ""
	sc := bufio.NewScanner(strings.NewReader(log))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		status, rest, ok := strings.Cut(line, " ")
		switch status {
		case testreg.StatusPass, testreg.StatusFail, testreg.StatusMissing, testreg.StatusUnasked:
		default:
			ok = false
		}
		name, reason, _ := strings.Cut(rest, ": ")
		if !ok || name == "run" || strings.Count(name, ".") < 4 || strings.ContainsAny(name, " :") {
			if ok && name == "run" || strings.HasPrefix(line, "schema-test ") {
				last = "" // the run's own lines close the reason before them
			}
			if last != "" {
				o := out[last]
				o.reason += "\n" + line
				out[last] = o
			}
			continue
		}
		out[name] = outcome{status, reason}
		last = name
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading the planted run's outcomes: %w", err)
	}
	return out, nil
}

func trackedFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var files []string
	for f := range strings.SplitSeq(strings.TrimRight(string(out), "\x00"), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func copyFile(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // tracked but deleted in the checkout: the copy has it deleted too
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(from)
		if err != nil {
			return err
		}
		return os.Symlink(target, to)
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, info.Mode().Perm())
}
