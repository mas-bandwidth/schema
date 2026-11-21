package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const goFixture = `package p

import "errors"

var ErrBig = errors.New("big")

type w struct{ n int }

func (x *w) put(v int) { x.n += v }

func check(vs []int, limit int) (int, error) {
	total := 0
	for _, v := range vs {
		if v > limit {
			return 0, ErrBig
		}
		if v < 0 && limit == 3 {
			return 0, errors.New("negative")
		}
		total += v << 8
	}
	name := "default"
	_ = name
	x := &w{}
	x.put(vs[0])
	x.put(total)
	return total, nil
}
`

func opsOf(muts []Mutant) map[string]int {
	out := map[string]int{}
	for _, m := range muts {
		out[m.Op]++
	}
	return out
}

func TestEveryGoOperatorFindsItsSite(t *testing.T) {
	muts, err := enumerateGo("p.go", []byte(goFixture), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := opsOf(muts)
	for _, op := range []string{opCmpFlip, opOffByOne, opBitWidth, opDeleteRefuse, opNilError, opZeroValue, opDropCheck, opNegateCond, opDropCall, opDropOperand} {
		if got[op] == 0 {
			t.Errorf("operator %s found no site in the fixture; it found %v", op, got)
		}
	}
	for _, m := range muts {
		out, err := apply([]byte(goFixture), m)
		if err != nil {
			t.Fatalf("%s: %v", m.Op, err)
		}
		if string(out) == goFixture {
			t.Errorf("%s at line %d changed nothing", m.Op, m.Line)
		}
	}
}

func TestGoOperatorsMakeTheEditTheyName(t *testing.T) {
	muts, err := enumerateGo("p.go", []byte(goFixture), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		opDeleteRefuse: {"return 0, ErrBig", "continue"},
		opNilError:     {"ErrBig", "nil"},
		opDropCheck:    {"v > limit", "false"},
		opNegateCond:   {"v > limit", "!(v > limit)"},
		opBitWidth:     {"8", "9"},
		opDropCall:     {"x.put(total)", ""},
		opDropOperand:  {"v < 0 && limit == 3", "v < 0"},
		opOffByOne:     {"3", "4"},
	}
	for op, edit := range want {
		found := false
		for _, m := range muts {
			if m.Op == op && m.Old == edit[0] && m.New == edit[1] {
				found = true
			}
		}
		if !found {
			t.Errorf("no %s mutant rewrites %q to %q", op, edit[0], edit[1])
		}
	}
	// a refusal outside a loop cannot become continue
	src := "package p\n\nfunc f(v int) error {\n\tif v > 1 {\n\t\treturn ErrBig\n\t}\n\treturn nil\n}\n"
	muts, err = enumerateGo("q.go", []byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range muts {
		if m.Op == opDeleteRefuse && m.New != "{}" {
			t.Errorf("a refusal outside a loop became %q", m.New)
		}
	}
}

func TestIdsSurviveAnUnrelatedEdit(t *testing.T) {
	a, err := enumerateGo("p.go", []byte(goFixture), nil)
	if err != nil {
		t.Fatal(err)
	}
	moved := strings.Replace(goFixture, "var ErrBig", "// a line nobody mutates\n\nvar ErrBig", 1)
	b, err := enumerateGo("p.go", []byte(moved), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("%d mutants before the edit and %d after", len(a), len(b))
	}
	seen := map[string]bool{}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("mutant %d was %s and is %s after a comment was added above it", i, a[i].ID, b[i].ID)
		}
		if seen[a[i].ID] {
			t.Errorf("id %s names two mutants", a[i].ID)
		}
		seen[a[i].ID] = true
	}
}

func TestSampleIsTheSeedsAndHoldsEveryOperator(t *testing.T) {
	muts, err := enumerateGo("p.go", []byte(goFixture), nil)
	if err != nil {
		t.Fatal(err)
	}
	ops := opsOf(muts)
	first := sample(append([]Mutant(nil), muts...), len(ops), 7)
	again := sample(append([]Mutant(nil), muts...), len(ops), 7)
	if !reflect.DeepEqual(first, again) {
		t.Error("one seed picked two different samples")
	}
	if got := opsOf(first); len(got) != len(ops) {
		t.Errorf("a sample of %d over %d operators holds %v", len(first), len(ops), got)
	}
	differs := false
	for seed := uint64(8); seed < 40 && !differs; seed++ {
		differs = !reflect.DeepEqual(first, sample(append([]Mutant(nil), muts...), len(ops), seed))
	}
	if !differs {
		t.Error("thirty-two seeds picked one sample: the seed selects nothing")
	}
	if got := sample(muts, 0, 1); len(got) != len(muts) {
		t.Errorf("n=0 returned %d of %d", len(got), len(muts))
	}
}

func TestTextOperatorsPerSyntax(t *testing.T) {
	cases := []struct {
		rules textRules
		line  string
		op    string
		old   string
		want  string
	}{
		{textRules{"paren", "//", "false"}, "if ( len > 32 ) { return false; } // 7 is prose", opTextDropCheck, "len > 32", "false"},
		{textRules{"paren", "//", "false"}, "if ( len > 32 ) { return false; }", opTextNegate, "len > 32", "!(len > 32)"},
		{textRules{"paren", "//", "false"}, "if ( len > 32 ) { return false; }", opTextAccept, "false", "true"},
		{textRules{"paren", "//", "false"}, "if ( len > 32 ) { return false; }", opTextCmpFlip, ">", ">="},
		{textRules{"paren", "//", "false"}, "write_bits( stream, v, 32 );", opTextBitWidth, "32", "33"},
		{textRules{"paren", "//", "false"}, "buffer[4] = 0;", opTextOffByOne, "4", "5"},
		{textRules{"then", "--", "false"}, "if n == 8 then return false end", opTextCmpFlip, "==", "~="},
		{textRules{"then", "--", "false"}, "if rem > n - i + 1 then return false end", opTextDropCheck, "rem > n - i + 1", "false"},
		{textRules{"then", "--", "false"}, "if rem > n then return false end", opTextNegate, "rem > n", "not (rem > n)"},
		{textRules{"colon", "#", "False"}, "if count > limit: # a bound", opTextDropCheck, "count > limit", "False"},
		{textRules{"colon", "#", "False"}, "    return False", opTextAccept, "False", "True"},
		{textRules{"brace", "//", "false"}, "if bits > 56 { return x; }", opTextDropCheck, "bits > 56", "false"},
		{textRules{"do", "#", "false"}, "if size > max do", opTextNegate, "size > max", "!(size > max)"},
		{textRules{"line", "#", "false"}, "return nil if count > limit", opTextDropCheck, "count > limit", "false"},
	}
	for _, c := range cases {
		found := false
		for _, m := range c.rules.enumerate(0, c.line, false, true) {
			if m.Op == c.op && m.Old == c.old && m.New == c.want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s over %q (%s): no mutant rewrites %q to %q", c.op, c.line, c.rules.Syntax, c.old, c.want)
		}
	}
}

func TestTextOperatorsLeaveCommentsFormatVerbsAndProseAlone(t *testing.T) {
	r := textRules{"paren", "//", "false"}
	for _, line := range []string{
		"// if ( a < 3 ) return false; 12",
		"printf( \"%2d %[1]s %016x\", v ); x_1 = y2 + 0x10 + 1.5;",
		"refuse if the count is large",
	} {
		if muts := r.enumerate(0, line, false, false); len(muts) != 0 {
			t.Errorf("%q has mutants %+v", line, muts)
		}
	}
	// a `\n` inside an interpreted literal ends the line, so a comment on the
	// first emitted line does not hide the second
	muts := r.enumerate(0, `// a note\nif ( a < b ) { return false; }\n`, true, false)
	if opsOf(muts)[opTextDropCheck] != 1 {
		t.Errorf("the second emitted line of an interpreted literal was not read: %+v", muts)
	}
	// only a runtime file loses whole lines
	if got := opsOf(r.enumerate(0, "  stream.write_bits(v, 3);", false, true))[opTextDropLine]; got != 1 {
		t.Errorf("a runtime file's write line: %d drop-line mutants", got)
	}
	if got := opsOf(r.enumerate(0, "  stream.write_bits(v, 3);", false, false))[opTextDropLine]; got != 0 {
		t.Errorf("a template's write line: %d drop-line mutants", got)
	}
}

func TestTemplatesAreMutatedInsideGoLiterals(t *testing.T) {
	src := "package p\n\nimport \"fmt\"\n\nfunc emit() { fmt.Print(\"if ( n > 16 ) { return false; }\\n\") }\n"
	rules := textRules{"paren", "//", "false"}
	muts, err := enumerateGo("g.go", []byte(src), &rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range muts {
		if m.Op != opTextDropCheck {
			continue
		}
		out, err := apply([]byte(src), m)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), `"if ( false ) { return false; }\n"`) {
			t.Errorf("the template became %s", out)
		}
		return
	}
	t.Errorf("no %s inside the literal: %v", opTextDropCheck, opsOf(muts))
}

// fake is an Exec that runs no process: it answers from what the scratch
// file holds, and counts what it was asked.
type fake struct {
	t       *testing.T
	file    string
	calls   []string
	verdict func(sh, content string) (out string, ok, timedOut bool)
}

func (f *fake) run(dir, sh string, _ time.Duration) ([]byte, bool, bool, time.Duration) {
	b, err := os.ReadFile(filepath.Join(dir, f.file))
	if err != nil {
		f.t.Fatal(err)
	}
	f.calls = append(f.calls, sh)
	out, ok, timedOut := f.verdict(sh, string(b))
	return []byte(out), ok, timedOut, time.Second
}

func fixtureScope(t *testing.T) (Scope, string, []Mutant) {
	t.Helper()
	tree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tree, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "pkg", "p.go"), []byte(goFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Scope{Name: "fixture", Go: []string{"pkg"}, Min: 1, Stages: []Stage{
		{Name: "fast", Sh: "test-fast", Proof: goTestProof},
		{Name: "slow", Sh: "test-slow", Proof: goTestProof, Deep: true},
	}}
	muts, err := s.enumerate(tree)
	if err != nil {
		t.Fatal(err)
	}
	return s, tree, muts
}

func TestSweepClassifiesAndRestores(t *testing.T) {
	s, tree, muts := fixtureScope(t)
	f := &fake{t: t, file: "pkg/p.go", verdict: func(sh, content string) (string, bool, bool) {
		switch {
		case content == goFixture:
			return "ok  \tpkg\t0.1s", true, false
		case sh == "go build ./pkg/...":
			return "p.go:1: broken", strings.Contains(content, "x.put(total)"), false
		case strings.Contains(content, "v >= limit"):
			return "FAIL pkg", false, false // the fast stage notices
		case strings.Contains(content, "v << 9"):
			return "", false, true // an endless loop
		case strings.Contains(content, "!(v > limit)") && sh == "test-slow":
			return "FAIL pkg", false, false // only the slow stage notices
		}
		return "ok  \tpkg\t0.1s", true, false
	}}
	var results []Result
	err := sweep(s, tree, muts, sweepOptions{Deep: true, BaselineLimit: time.Minute}, f.run, &strings.Builder{}, func(r Result) { results = append(results, r) })
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(muts) {
		t.Fatalf("%d results for %d mutants", len(results), len(muts))
	}
	got := map[string]int{}
	for _, r := range results {
		got[r.Outcome+"/"+r.Stage]++
	}
	for _, want := range []string{"killed/fast", "killed/slow", "timeout/fast", "build-broken/build", "survived/slow"} {
		if got[want] == 0 {
			t.Errorf("no mutant ended %s: %v", want, got)
		}
	}
	back, err := os.ReadFile(filepath.Join(tree, "pkg", "p.go"))
	if err != nil || string(back) != goFixture {
		t.Error("the scratch file is not what it was before the sweep")
	}
}

func TestSweepRefusesToProveNothing(t *testing.T) {
	green := func(string, string) (string, bool, bool) { return "ok  \tpkg\t0.1s", true, false }
	run := func(t *testing.T, muts func([]Mutant) []Mutant, verdict func(sh, content string) (string, bool, bool), opt sweepOptions) error {
		s, tree, all := fixtureScope(t)
		f := &fake{t: t, file: "pkg/p.go", verdict: verdict}
		opt.BaselineLimit = time.Minute
		return sweep(s, tree, muts(all), opt, f.run, &strings.Builder{}, func(Result) {})
	}
	all := func(m []Mutant) []Mutant { return m }
	cases := []struct {
		name    string
		muts    func([]Mutant) []Mutant
		verdict func(sh, content string) (string, bool, bool)
		opt     sweepOptions
		want    string
	}{
		{"no mutants", func([]Mutant) []Mutant { return nil }, green, sweepOptions{}, "no mutant to apply"},
		{"a red baseline", all, func(string, string) (string, bool, bool) { return "FAIL", false, false }, sweepOptions{}, "unmutated tree is red"},
		{"a baseline that tested nothing", all, func(string, string) (string, bool, bool) { return "", true, false }, sweepOptions{}, "it tested nothing"},
		{"a baseline that hangs", all, func(string, string) (string, bool, bool) { return "", false, true }, sweepOptions{}, "did not finish"},
		{"a stage that does not exist", all, green, sweepOptions{Only: []string{"nope"}}, `no stage "nope"`},
		{"every mutant fails to build", all, func(sh, content string) (string, bool, bool) {
			if content != goFixture && strings.HasPrefix(sh, "go build") {
				return "broken", false, false
			}
			return "ok  \tpkg\t0.1s", true, false
		}, sweepOptions{}, "no test stage ran"},
	}
	for _, c := range cases {
		err := run(t, c.muts, c.verdict, c.opt)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the sweep said %v, want an error naming %q", c.name, err, c.want)
		}
	}
	// a tree left red after the last revert voids the run
	calls := 0
	err := run(t, all, func(sh, content string) (string, bool, bool) {
		calls++
		if calls > 1 && content == goFixture && sh == "test-fast" {
			return "FAIL", false, false
		}
		return "ok  \tpkg\t0.1s", true, false
	}, sweepOptions{})
	if err == nil || !strings.Contains(err.Error(), "every verdict of this run is void") {
		t.Errorf("a red tree after the sweep: %v", err)
	}
}

func TestEnforceNamesAnUnreviewedSurvivor(t *testing.T) {
	results := []Result{
		{Mutant: Mutant{ID: "aaaaaaaaaaaa"}, Scope: "check", Outcome: outcomeKilled},
		{Mutant: Mutant{ID: "bbbbbbbbbbbb"}, Scope: "check", Outcome: outcomeSurvived},
	}
	if err := enforceEquivalents(results[:1]); err != nil {
		t.Errorf("a killed mutant failed the enforcement: %v", err)
	}
	if err := enforceEquivalents(results); err == nil {
		t.Error("an unreviewed survivor passed the enforcement")
	}
}

// repo is the checkout this test runs in.
func repo(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEveryScopeHasItsFloorOfMutantsAndProvableStages(t *testing.T) {
	root := repo(t)
	all, err := scopes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(coreScopes)+18 {
		t.Fatalf("%d scopes; want the %d core packages and the 18 targets", len(all), len(coreScopes))
	}
	known := map[string]map[string]bool{}
	for _, s := range all {
		muts, err := s.enumerate(root)
		if err != nil {
			t.Errorf("%s: %v", s.Name, err)
			continue
		}
		if len(muts) < s.Min {
			t.Errorf("scope %s has %d mutants and its floor is %d", s.Name, len(muts), s.Min)
		}
		ids := map[string]bool{}
		for _, m := range muts {
			if ids[m.ID] {
				t.Errorf("scope %s: id %s names two mutants (%s:%d)", s.Name, m.ID, m.File, m.Line)
			}
			ids[m.ID] = true
		}
		known[s.Name] = ids
		if s.Rules != nil && opsOf(muts)[opTextCmpFlip] == 0 {
			t.Errorf("target scope %s has no text mutant: its templates were not read", s.Name)
		}
		for _, st := range s.Stages {
			if st.Proof == "" || st.Sh == "" {
				t.Errorf("scope %s stage %s has no command or no proof that it tested", s.Name, st.Name)
			}
		}
	}

	// the reviewed list and the smoke set name mutants that exist
	reviewed, err := loadEquivalents()
	if err != nil {
		t.Fatal(err)
	}
	for key, e := range reviewed {
		if !known[e.Scope][e.ID] {
			t.Errorf("equivalent-mutants.json: %s is no mutant of the tree any more; the source moved, so remove the entry or review the mutant again", key)
		}
	}
	var set []Smoke
	if err := json.Unmarshal(smokeJSON, &set); err != nil {
		t.Fatal(err)
	}
	if len(set) == 0 {
		t.Error("smoke.json is empty")
	}
	for _, s := range set {
		if !known[s.Scope][s.ID] {
			t.Errorf("smoke.json: %s/%s is no mutant of the tree any more; pick a replacement with `go run ./tools/mutation list -scope %s`", s.Scope, s.ID, s.Scope)
		}
		if strings.TrimSpace(s.Why) == "" {
			t.Errorf("smoke.json: %s/%s does not say what kills it", s.Scope, s.ID)
		}
	}
}
