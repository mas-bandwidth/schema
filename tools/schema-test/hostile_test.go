package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

var updateHostile = flag.Bool("update-hostile", false, "rewrite the registry's hostile rows from the corpus manifest")

// hostileRowLine is a row as the registry file holds it.
func hostileRowLine(t testreg.Test) (string, error) {
	t.Line = 0
	b, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	return `{"rec":"test",` + string(b[1:]), nil
}

// isHostileFamily says whether a family is one of the hostile sets'.
func isHostileFamily(family string) bool {
	for _, s := range hostileSets {
		if s.family == family {
			return true
		}
	}
	return false
}

// TestTheRegistryHoldsEveryHostileCase holds the registry's hostile rows to
// the corpus manifests field for field: a case with no row, a row with no
// case and a row that differs are each red by key. -update-hostile rewrites
// them (keeping each row's measured seconds).
func TestTheRegistryHoldsEveryHostileCase(t *testing.T) {
	want, err := hostileRows("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the hostile corpus gives no rows")
	}
	path := "../../" + testreg.DefaultPath
	reg, err := testreg.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]testreg.Test{}
	for _, row := range reg.Tests {
		if isHostileFamily(row.Family) {
			have[row.Key] = row
		}
	}
	if *updateHostile {
		if err := rewriteHostileRows(path, want, have); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, w := range want {
		h, ok := have[w.Key]
		if !ok {
			t.Errorf("%s: the case %s has no registry row (go test ./tools/schema-test -run TestTheRegistryHoldsEveryHostileCase -args -update-hostile)", w.Key, w.Source)
			continue
		}
		delete(have, w.Key)
		w.Seconds, w.Line = h.Seconds, h.Line
		wl, err := hostileRowLine(w)
		if err != nil {
			t.Fatal(err)
		}
		hl, err := hostileRowLine(h)
		if err != nil {
			t.Fatal(err)
		}
		if wl != hl {
			t.Errorf("%s:%d: the row is not the manifest's:\n have %s\n want %s", path, h.Line, hl, wl)
		}
	}
	for key, h := range have {
		t.Errorf("%s:%d: %s is a hostile row the manifest has no case for", path, h.Line, key)
	}
}

// rewriteHostileRows drops every hostile family's row from the registry file and
// appends want, each keeping the seconds its old row measured.
func rewriteHostileRows(path string, want []testreg.Test, have map[string]testreg.Test) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	for line := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		var head struct{ Rec, Family string }
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if head.Rec == "test" && isHostileFamily(head.Family) {
			continue
		}
		out.WriteString(line + "\n")
	}
	for _, w := range want {
		w.Seconds = have[w.Key].Seconds
		l, err := hostileRowLine(w)
		if err != nil {
			return err
		}
		out.WriteString(l + "\n")
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// TestHostileCaseKeysAreDistinct: two manifest names that make one registry
// case are refused, naming both.
func TestHostileCaseKeysAreDistinct(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(hostileCorpus))
	if err := os.MkdirAll(filepath.Join(dir, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.bin", "b.bin"} {
		if err := os.WriteFile(filepath.Join(dir, "inputs", f), []byte{1}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := "# name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest\n" +
		"bare_minus\twire\ttables/examples\tT\tinputs/a.bin\tp\trefuse\tdamaged\t-\n" +
		"bare-minus\twire\ttables/examples\tT\tinputs/b.bin\tp\trefuse\tdamaged\t-\n"
	if err := os.WriteFile(filepath.Join(dir, hostilecorpus.ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hostileRows(root)
	if err == nil || !strings.Contains(err.Error(), "bare_minus and bare-minus are one registry case") {
		t.Fatalf("two names of one case key: got %v", err)
	}
}

// hostileInstance is an instance of a hostile row for lang, with the inputs
// a row of the case carries.
func hostileInstance(t *testing.T, lang, name string) testreg.Instance {
	t.Helper()
	in, err := json.Marshal(hostileInputs{Target: hostileTarget, Case: name, Input: "x", GoTests: hostileGoTests})
	if err != nil {
		t.Fatal(err)
	}
	key := hostileCaseKey(name)
	return testreg.Instance{
		Full: lang + ".nightly.load-refusal.hostile." + key,
		Test: testreg.Test{Key: "load-refusal.hostile." + key, Type: "load-refusal", Family: hostileFamily, Case: key, Tier: "nightly", Inputs: in},
	}
}

// TestRunHostileReadsEachCaseFromItsRun drives hostileMain.runWith with a
// stand-in make: each case's verdict is its line in the run, the lines a
// Go-written runner leaves in the verdicts file count as the output's do,
// and every way a run can fail to give a case its verdict is red by name,
// each asked case failing with what the run said.
// It runs from the repository root, where the corpus folder is.
func TestRunHostileReadsEachCaseFromItsRun(t *testing.T) {
	t.Chdir("../..")
	cases, err := hostilecorpus.Registered(filepath.FromSlash(hostileCorpus))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 4 {
		t.Fatal("the corpus has fewer than four cases")
	}
	a, b, c, d := cases[0].Name, cases[1].Name, cases[2].Name, cases[3].Name // d is never asked
	ranAll := func(except ...string) string {
		var s strings.Builder
		for _, k := range cases {
			left := false
			for _, e := range except {
				left = left || e == k.Name
			}
			if !left {
				s.WriteString("hostile-ledger ran " + k.Name + "\n")
			}
		}
		return s.String()
	}
	for _, tc := range []struct {
		name    string
		out     string // the target's output
		file    string // what the stand-in writes to the verdicts file
		exit    error
		want    []string // verdicts for a, b, c
		reasonA string
		errHas  string
	}{
		{name: "every-case-ran", out: "log line\n" + ranAll() + "hostile-ledger end\n", want: []string{"pass", "pass", "pass"}},
		{name: "a-failed", out: "FAIL " + a + " carrier=wire got=accept want=refuse\n" + ranAll() + "hostile-ledger end\n", exit: errors.New("exit status 1"),
			want: []string{"fail", "pass", "pass"}, reasonA: "got=accept"},
		{name: "a-never-ran", out: ranAll(a) + "hostile-ledger end\n", want: []string{"fail", "pass", "pass"}, reasonA: "no verdict"},
		{name: "row-ids-are-their-cases", out: "hostile-ledger ran " + a + "@tier1\n" + ranAll(a) + "hostile-ledger end\n", want: []string{"pass", "pass", "pass"}},
		{name: "go-runner-file", out: "PROOF ok\n", file: "FAIL " + b + " want refuse\n" + ranAll() + "hostile-ledger end\n", want: []string{"pass", "fail", "pass"}},
		{name: "lines-after-the-block-are-not-the-run", out: ranAll() + "hostile-ledger end\nFAIL " + a + " a carry control's scratch case\n", want: []string{"pass", "pass", "pass"}},
		{name: "no-block", out: ranAll(), want: []string{"fail", "fail", "fail"}, reasonA: "closed no hostile-ledger block"},
		{name: "no-target", out: "make: *** No rule to make target 'schema-go-runs-hostile-corpus'.  Stop.\n", exit: errors.New("exit status 2"),
			want: []string{"fail", "fail", "fail"}, reasonA: "No rule to make target"},
		{name: "carried-finding", out: "FIX-CARD: slug: inputs/x.bin\n" + ranAll() + "hostile-ledger end\n", want: []string{"fail", "fail", "fail"}, reasonA: "no case's verdict"},
		{name: "ledger-did-not-close", out: ranAll() + "FAIL c hostile-corpus ledger: short\nhostile-ledger end\n", want: []string{"fail", "fail", "fail"}, reasonA: "no case's verdict"},
		{name: "unregistered-case", out: "hostile-ledger ran no-such-case\n" + ranAll() + "hostile-ledger end\n", want: []string{"fail", "fail", "fail"}, reasonA: "no case's verdict"},
		{name: "exit-with-every-case-green", out: ranAll() + "hostile-ledger end\n", exit: errors.New("exit status 2"), want: []string{"pass", "pass", "pass"}, errHas: "failed with no case failing by name"},
		// a case not asked gave no verdict and no line names a failure: the
		// target failed in something no case shows, which is the run's
		{name: "exit-with-an-unasked-case-silent", out: ranAll(d) + "hostile-ledger end\n", exit: errors.New("exit status 2"), want: []string{"pass", "pass", "pass"}, errHas: "1 registered cases gave no verdict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var argv []string
			stand := func(ctx context.Context, args []string) (string, error) {
				argv = args
				if tc.file != "" {
					path := testreg.EnvFrom(ctx)[hostilecorpus.VerdictsEnv]
					if !filepath.IsAbs(path) {
						t.Errorf("the verdicts file %q is not absolute: a Go test runs in its package's folder", path)
					}
					if err := os.WriteFile(path, []byte(tc.file), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return tc.out, tc.exit
			}
			insts := []testreg.Instance{hostileInstance(t, "go", a), hostileInstance(t, "go", b), hostileInstance(t, "go", c)}
			var log bytes.Buffer
			res, err := hostileMain.runWith(context.Background(), stand, t.TempDir(), "go", insts, &log)
			if got := strings.Join(argv, " "); got != "make -s schema-go-runs-hostile-corpus" {
				t.Errorf("ran %q", got)
			}
			if tc.errHas != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errHas) {
					t.Fatalf("err %v, want one naming %q", err, tc.errHas)
				}
				if tc.want == nil {
					return
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got := map[string]testreg.Result{}
			for _, r := range res {
				got[r.Instance] = r
			}
			for i, inst := range insts {
				r, ok := got[inst.Full]
				if !ok {
					t.Errorf("%s: no result", inst.Full)
					continue
				}
				if r.Verdict != tc.want[i] {
					t.Errorf("%s: %s (%s), want %s", inst.Full, r.Verdict, r.Reason, tc.want[i])
				}
			}
			if tc.reasonA != "" && !strings.Contains(got[insts[0].Full].Reason, tc.reasonA) {
				t.Errorf("a's reason %q does not name %q", got[insts[0].Full].Reason, tc.reasonA)
			}
		})
	}
}

// TestHostilePlanRefusesWhatIsNoCase: an instance whose inputs name another
// target, no case, or a case another instance names is refused before
// anything runs.
func TestHostilePlanRefusesWhatIsNoCase(t *testing.T) {
	good := hostileInstance(t, "go", "wire-valid")
	other := good
	other.Full = "go.nightly.load-refusal.hostile.again"
	bad := good
	bad.Test.Inputs = json.RawMessage(`{"target":"schema-go-runs-hostile-corpus","case":"wire-valid","input":"x","go_tests":[]}`)
	none := good
	none.Test.Inputs = json.RawMessage(`{"target":"` + hostileTarget + `","case":"","input":"x","go_tests":[]}`)
	for name, insts := range map[string][]testreg.Instance{
		"resolved-target": {bad},
		"no-case":         {none},
		"twice":           {good, other},
		"nothing":         nil,
	} {
		if _, _, err := hostileMain.plan("go", insts); err == nil {
			t.Errorf("%s: planned", name)
		}
	}
	target, want, err := hostileMain.plan("lua", []testreg.Instance{good})
	if err != nil || target != "schema-lua-runs-hostile-corpus" || want["wire-valid"] != good.Full {
		t.Errorf("plan: %q %v %v", target, want, err)
	}
}

// TestNumberTokenCasesAreRowsThatReadTheirTrees: every bare number-token
// conformance vector (testdata/conformance/tables/JSON-HOSTILE-NUMBER-TOKEN.txt)
// is a case of the one corpus manifest, so every port's corpus run answers it
// (make schema-<lang>-runs-hostile-corpus), and its row names the tree it
// copies, so the tree is a data case the registry reads. A vector the corpus
// does not carry is red by name, and so is a row naming another tree.
func TestNumberTokenCasesAreRowsThatReadTheirTrees(t *testing.T) {
	t.Chdir("../..")
	data, err := os.ReadFile("testdata/conformance/tables/JSON-HOSTILE-NUMBER-TOKEN.txt")
	if err != nil {
		t.Fatal(err)
	}
	vectors := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 6 && f[0] == "json-hostile" {
			if !strings.HasPrefix(f[4]+"/", hostileNumberTokenTrees) {
				t.Fatalf("JSON-HOSTILE-NUMBER-TOKEN.txt: %s lives at %s, outside %s", f[1], f[4], hostileNumberTokenTrees)
			}
			vectors[f[4]+"/"+f[3]+".json"] = true
		}
	}
	if len(vectors) == 0 {
		t.Fatal("JSON-HOSTILE-NUMBER-TOKEN.txt lists no vector")
	}
	rows, err := hostileMain.rows(".")
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]string{}
	for _, r := range rows {
		var in hostileInputs
		if err := json.Unmarshal(r.Inputs, &in); err != nil {
			t.Fatal(err)
		}
		if in.Target != hostileTarget {
			t.Errorf("%s: target %s, want %s", r.Key, in.Target, hostileTarget)
		}
		for _, f := range in.Files {
			if !vectors[f] {
				t.Errorf("%s names %s, which JSON-HOSTILE-NUMBER-TOKEN.txt does not list", r.Key, f)
			}
			read[f] = r.Key
		}
	}
	for v := range vectors {
		if read[v] == "" {
			t.Errorf("the number-token vector %s is no case of %s/%s: no port's corpus run answers it", v, hostileCorpus, hostilecorpus.ManifestName)
		}
	}
}

// TestEveryHostileTargetDeclaresItsBuild holds the hostile harness's Build
// step to the make files: for every registry language, the step is make
// <target>-build, a rule declares that build target, and the corpus target
// depends on it. A language whose build is not declared would build inside
// the run, and a cold sanitizer build would then spend a row's deadline; a
// harness with no Build step would do the same for every language. Both are
// red here by language.
func TestEveryHostileTargetDeclaresItsBuild(t *testing.T) {
	t.Chdir("../..")
	reg, err := testreg.Load(testreg.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("make/*.mk")
	if err != nil {
		t.Fatal(err)
	}
	checks, err := filepath.Glob("make/checks/*.mk")
	if err != nil {
		t.Fatal(err)
	}
	prereqs := map[string][]string{} // rule target -> every prerequisite named
	for _, f := range append(append([]string{"Makefile"}, files...), checks...) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			head, rest, ok := strings.Cut(line, ":")
			if !ok || strings.HasPrefix(rest, "=") || strings.ContainsAny(head, " \t$#") || head == "" {
				continue
			}
			prereqs[head] = append(prereqs[head], strings.Fields(rest)...)
		}
	}
	langs := reg.LangNames()
	if len(langs) == 0 {
		t.Fatal("the registry names no language")
	}
	for _, lang := range langs {
		target := strings.ReplaceAll(hostileTarget, testreg.LangPlaceholder, lang)
		build := target + hostileBuildSuffix
		steps := hostileMain.harness().Build(lang)
		if len(steps) != 1 || strings.Join(steps[0], " ") != "make -s "+build {
			t.Errorf("%s: the hostile harness builds %q, want make -s %s", lang, steps, build)
		}
		if _, ok := prereqs[build]; !ok {
			t.Errorf("%s: no make file declares %s, the build of %s", lang, build, target)
			continue
		}
		if !slices.Contains(prereqs[target], build) {
			t.Errorf("%s: %s does not depend on %s (its prerequisites: %q)", lang, target, build, prereqs[target])
		}
	}
}

// TestEveryHostileTargetDryRunsWithoutWriting runs make -n on every registry
// language's corpus target (which names its declared build) and holds each to
// a dry run: make exits 0 and no file or directory of the tree is created,
// changed or removed. make -n still runs a recipe line whose text names
// $(MAKE), so a line that also untars, deletes or downloads does it for real;
// dist-squirrel's extract line did (make/squirrel.mk, SQUIRREL_SUBMAKE).
//
// It dry-runs a private copy of the checkout's files (tracked and untracked,
// not ignored) at build/dryrun-<n>/checkout, so no other run writing the
// checkout can turn it red, and the copy is cold the way a fresh CI checkout
// is. It watches all of build/dryrun-<n>, so a write through the make files'
// ../ sibling paths (../serialize and the rest) lands inside what it sees.
// Those siblings are absent there: the dry run takes the path a checkout
// without them takes. It sees writes to that folder only, not to caches or
// $HOME. Functional tier: it runs git and make.
func TestEveryHostileTargetDryRunsWithoutWriting(t *testing.T) {
	t.Chdir("../..")
	reg, err := testreg.Load(testreg.DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	langs := reg.LangNames()
	if len(langs) == 0 {
		t.Fatal("the registry names no language")
	}
	watched, tree := dryRunCopy(t)
	// make -n reads the copy's make files, which are the checkout's
	testinputs.Make(t)
	// the dry run is make's own, not a parent make's jobserver or flags
	var env []string
	for _, kv := range os.Environ() {
		switch k, _, _ := strings.Cut(kv, "="); k {
		case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "GNUMAKEFLAGS", "MAKEFILES":
		default:
			env = append(env, kv)
		}
	}
	before, err := checkoutState(watched)
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range langs {
		target := strings.ReplaceAll(hostileTarget, testreg.LangPlaceholder, lang)
		cmd := exec.Command("make", "-n", target)
		cmd.Dir = tree
		cmd.Env = env
		out, runErr := cmd.CombinedOutput()
		after, err := checkoutState(watched)
		if err != nil {
			t.Fatal(err)
		}
		if runErr != nil {
			t.Errorf("%s: make -n %s failed: %v\n%s", lang, target, runErr, tail(string(out), 10))
		}
		if changed := stateDiff(before, after); len(changed) > 0 {
			t.Errorf("%s: make -n %s wrote the tree: %s", lang, target, strings.Join(changed, ", "))
		}
		before = after
	}
}

// dryRunCopy copies every file git lists in the checkout (tracked, and
// untracked but not ignored) into checkout/ of a new folder under build/,
// removed when the test ends, and returns the folder and the copy. A tracked
// file deleted in the checkout is not copied: the copy is the working tree as
// it stands.
func dryRunCopy(t *testing.T) (string, string) {
	t.Helper()
	list, err := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if err := os.MkdirAll("build", 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("build", "dryrun-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove %s: %v", dir, err)
		}
	})
	copied := 0
	seen := map[string]bool{}
	for _, rel := range strings.Split(strings.TrimRight(string(list), "\x00"), "\x00") {
		if rel == "" || seen[rel] {
			continue
		}
		seen[rel] = true
		info, err := os.Lstat(rel)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(dir, "checkout", rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(rel)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, dst); err != nil {
				t.Fatal(err)
			}
		case info.Mode().IsRegular():
			data, err := os.ReadFile(rel)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("%s: git lists it, and it is neither a file nor a link (%s)", rel, info.Mode())
		}
		copied++
	}
	if copied == 0 {
		t.Fatal("git ls-files listed no file to copy")
	}
	return dir, filepath.Join(dir, "checkout")
}

// fileState is one path's kind, size, modification time and identity (its
// inode and change time, which a delete-and-restore changes even when tar
// puts the size and modification time back).
type fileState struct {
	dir   bool
	size  int64
	mtime int64
	id    string
}

// checkoutState is every path under root but .git.
func checkoutState(root string) (map[string]fileState, error) {
	out := map[string]fileState{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		id, err := fileIdentity(info)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		st := fileState{dir: d.IsDir(), mtime: info.ModTime().UnixNano(), id: id}
		if !st.dir {
			st.size = info.Size()
		}
		out[p] = st
		return nil
	})
	return out, err
}

// stateDiff names, in order, each path created, changed or removed between
// two states, at most 20 of them and then how many more.
func stateDiff(before, after map[string]fileState) []string {
	var changed []string
	for p, a := range after {
		b, ok := before[p]
		switch {
		case !ok:
			changed = append(changed, p+" (created)")
		case !a.dir && (a.size != b.size || a.mtime != b.mtime || a.id != b.id):
			changed = append(changed, p+" (changed)")
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			changed = append(changed, p+" (removed)")
		}
	}
	slices.Sort(changed)
	if len(changed) > 20 {
		changed = append(changed[:20], fmt.Sprintf("and %d more", len(changed)-20))
	}
	return changed
}

// TestStateDiffNamesEveryWrite holds stateDiff to naming a created, a changed,
// a removed and a deleted-and-restored path (same size and mtime, a new inode
// and change time), and nothing for an untouched one or a directory whose
// listing alone changed.
func TestStateDiffNamesEveryWrite(t *testing.T) {
	before := map[string]fileState{
		"same": {size: 1, mtime: 1, id: "1/1"}, "edited": {size: 1, mtime: 1, id: "2/1"}, "gone": {size: 1, mtime: 1, id: "3/1"},
		"retarred": {size: 1, mtime: 1, id: "4/1"}, "d": {dir: true, mtime: 1, id: "5/1"},
	}
	after := map[string]fileState{
		"same": {size: 1, mtime: 1, id: "1/1"}, "edited": {size: 1, mtime: 2, id: "2/1"}, "new": {size: 0, mtime: 3, id: "6/1"},
		// deleted and extracted again: tar restores size and mtime, never the
		// inode and change time
		"retarred": {size: 1, mtime: 1, id: "7/2"}, "d": {dir: true, mtime: 9, id: "5/9"},
	}
	want := []string{"edited (changed)", "gone (removed)", "new (created)", "retarred (changed)"}
	if got := stateDiff(before, after); !slices.Equal(got, want) {
		t.Errorf("stateDiff = %q, want %q", got, want)
	}
}
