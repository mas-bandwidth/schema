package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/ci/hostilecorpus"
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

// TestNumberTokenCasesAreRowsThatReadTheirTrees: every case of the
// number-token manifest is a row of its own family, naming the conformance
// tree it copies (so the tree is a data case the registry reads), and a
// port with no number-token target fails each asked case by name, naming the
// missing target, without stopping the run.
func TestNumberTokenCasesAreRowsThatReadTheirTrees(t *testing.T) {
	t.Chdir("../..")
	dir := filepath.FromSlash(hostileCorpus)
	cases, err := hostileNumberToken.cases(dir)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := hostileNumberToken.rows(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(cases) {
		t.Fatalf("%d rows for %d cases", len(rows), len(cases))
	}
	var insts []testreg.Instance
	for _, r := range rows {
		var in hostileInputs
		if err := json.Unmarshal(r.Inputs, &in); err != nil {
			t.Fatal(err)
		}
		if r.Family != hostileNumberToken.family || len(in.Files) != 1 || !strings.HasPrefix(in.Files[0], "testdata/conformance/tables/json-hostile-number-token/") {
			t.Errorf("%s: family %s, files %v: want the number-token family reading its tree", r.Key, r.Family, in.Files)
		}
		insts = append(insts, testreg.Instance{Full: "go.nightly." + r.Key, Test: r})
	}
	stand := func(ctx context.Context, args []string) (string, error) {
		if got := strings.Join(args, " "); got != "make -s schema-go-runs-hostile-number-token" {
			t.Errorf("ran %q", got)
		}
		return "make: *** No rule to make target 'schema-go-runs-hostile-number-token'.  Stop.\n", errors.New("exit status 2")
	}
	var log bytes.Buffer
	res, err := hostileNumberToken.runWith(context.Background(), stand, t.TempDir(), "go", insts, &log)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(insts) {
		t.Fatalf("%d results for %d instances", len(res), len(insts))
	}
	for _, r := range res {
		if r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "schema-go-runs-hostile-number-token") {
			t.Errorf("%s: %s (%s), want a fail naming the missing target", r.Instance, r.Verdict, r.Reason)
		}
	}
}
