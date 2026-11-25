package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// makeInst is one make row's instance.
func makeInst(t *testing.T, name, file, target string) testreg.Instance {
	t.Helper()
	in, err := json.Marshal(makeInputs{File: file, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	return testreg.Instance{Full: name, Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Adapter: "make", Type: "harness-run", Inputs: in}}
}

// fakeMake is a make adapter over a make file held in memory and a make
// that answers from a table: no unit test reads the tree or runs make.
func fakeMake(file string, rules string, runs map[string]struct {
	log string
	err error
}, ran *[]string) makeAdapter {
	return makeAdapter{
		read: func(p string) ([]byte, error) {
			if p != file {
				return nil, errors.New("no such file")
			}
			return []byte(rules), nil
		},
		exec: func(_ context.Context, argv []string) (string, error) {
			target := argv[len(argv)-1]
			*ran = append(*ran, target)
			r := runs[target]
			return r.log, r.err
		},
		plan: emptyPlan,
	}
}

const fakeRules = `# a make file
VAR := x
gate-check: dep
	@echo checking
gate-skips gate-red: dep
	@echo two rules
`

// TestAMakeRowWhoseTargetIsGoneFails: a row whose target its file no longer
// writes fails by name and runs nothing (it is a test that does not exist).
func TestAMakeRowWhoseTargetIsGoneFails(t *testing.T) {
	t.Parallel()
	var ran []string
	a := fakeMake("make/x.mk", fakeRules, nil, &ran)
	res, err := a.Run(context.Background(), "", []testreg.Instance{makeInst(t, "gone", "make/x.mk", "gate-renamed"), makeInst(t, "var", "make/x.mk", "VAR")})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Verdict != testreg.Fail || !strings.Contains(r.Reason, "writes no rule") {
			t.Errorf("%s: %+v, want a fail naming the missing rule", r.Instance, r)
		}
	}
	if len(res) != 2 || len(ran) != 0 {
		t.Errorf("results %+v, make ran %v: a missing target runs nothing", res, ran)
	}
}

// TestAMakeTargetPassesOnlyOnExitZeroWithNoSkip: exit 0 passes; a non-zero
// exit fails with its log; exit 0 with a skip in the log fails.
func TestAMakeTargetPassesOnlyOnExitZeroWithNoSkip(t *testing.T) {
	t.Parallel()
	var ran []string
	a := fakeMake("make/x.mk", fakeRules, map[string]struct {
		log string
		err error
	}{
		"gate-check": {log: "checking\nok\n"},
		"gate-red":   {log: "FAIL: row 3\n", err: errors.New("exit status 2")},
		"gate-skips": {log: "lua not found, skipping the lua rows\n"},
	}, &ran)
	res, err := a.Run(context.Background(), "", []testreg.Instance{
		makeInst(t, "check", "make/x.mk", "gate-check"),
		makeInst(t, "red", "make/x.mk", "gate-red"),
		makeInst(t, "skips", "make/x.mk", "gate-skips"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]testreg.Result{}
	for _, r := range res {
		got[r.Instance] = r
	}
	if got["check"].Verdict != testreg.Pass {
		t.Errorf("exit 0: %+v", got["check"])
	}
	if got["red"].Verdict != testreg.Fail || !strings.Contains(got["red"].Reason, "FAIL: row 3") {
		t.Errorf("a non-zero exit: %+v", got["red"])
	}
	if got["skips"].Verdict != testreg.Fail || !strings.Contains(got["skips"].Reason, "skip") {
		t.Errorf("a skip in the log: %+v", got["skips"])
	}
	if strings.Join(ran, " ") != "gate-check gate-red gate-skips" {
		t.Errorf("make ran %v, want each target once, in order", ran)
	}
}

// TestMakeInputsNameOneTargetOfAMakeFile: inputs that are not one target of
// the Makefile or a .mk file are not a make row (Answers is false, and Run
// refuses them as a crash, never as a pass).
func TestMakeInputsNameOneTargetOfAMakeFile(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		`{"file":"make/x.mk","target":"a b"}`,
		`{"file":"make/x.mk","target":"-k"}`,
		`{"file":"make/x.mk","target":"X=1"}`,
		`{"file":"scripts/x.sh","target":"a"}`,
		`{"file":"make/x.mk"}`,
		`{"file":"make/x.mk","target":"a","env":{}}`,
	} {
		test := testreg.Test{Scope: testreg.LangFree, Inputs: json.RawMessage(in)}
		if (makeAdapter{}).Answers(test) {
			t.Errorf("%s answered as a make row", in)
		}
		if _, err := (makeAdapter{}).Run(context.Background(), "", []testreg.Instance{{Full: "x", Test: test}}); err == nil {
			t.Errorf("%s ran", in)
		}
	}
}

// TestAMakeRowRunsWithTheCoresAndAControlWithout: a make row runs make -j by
// the cores, as the workflows ran it; a negative control runs with no -j, as
// the controls job ran it.
func TestAMakeRowRunsWithTheCoresAndAControlWithout(t *testing.T) {
	t.Parallel()
	if newMakeAdapter().jobs != runtime.NumCPU() || newPlantAdapter(nil).make.jobs != 0 {
		t.Fatalf("make row jobs %d, control jobs %d", newMakeAdapter().jobs, newPlantAdapter(nil).make.jobs)
	}
	for jobs, want := range map[int]string{4: "make --no-print-directory -j4 gate-check", 0: "make --no-print-directory gate-check"} {
		var argv []string
		a := makeAdapter{jobs: jobs, read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
			exec: func(_ context.Context, av []string) (string, error) { argv = av; return "", nil }, plan: emptyPlan}
		if _, err := a.Run(context.Background(), "", []testreg.Instance{makeInst(t, "c", "make/x.mk", "gate-check")}); err != nil {
			t.Fatal(err)
		}
		if strings.Join(argv, " ") != want {
			t.Errorf("jobs %d ran %q, want %q", jobs, strings.Join(argv, " "), want)
		}
	}
}

// TestAMakeRowSetsItsVariablesOnTheCommandLine: a lint row's make variables
// follow its target on make's command line, in name order, so a row can
// name its formatter (CLANG_FORMAT=clang-format-18); a variable that is not
// an upper-case name with a one-line value is not a make row.
func TestAMakeRowSetsItsVariablesOnTheCommandLine(t *testing.T) {
	t.Parallel()
	in, err := json.Marshal(makeInputs{File: "make/x.mk", Target: "gate-check", Vars: map[string]string{"DART": "dart", "CLANG_FORMAT": "clang-format-18"}})
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "v", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Inputs: in}}
	var argv []string
	a := makeAdapter{read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
		exec: func(_ context.Context, av []string) (string, error) { argv = av; return "", nil }, plan: emptyPlan}
	if _, err := a.Run(context.Background(), "", []testreg.Instance{inst}); err != nil {
		t.Fatal(err)
	}
	if want := "make --no-print-directory gate-check CLANG_FORMAT=clang-format-18 DART=dart"; strings.Join(argv, " ") != want {
		t.Errorf("ran %q, want %q", strings.Join(argv, " "), want)
	}
	for _, bad := range []string{
		`{"file":"make/x.mk","target":"a","vars":{"dart":"x"}}`,
		`{"file":"make/x.mk","target":"a","vars":{"A B":"x"}}`,
		`{"file":"make/x.mk","target":"a","vars":{"DART":"x\ny"}}`,
		`{"file":"make/x.mk","target":"a","tools":["/usr/bin/dart"]}`,
		`{"file":"make/x.mk","target":"a","tools":[""]}`,
		`{"file":"make/x.mk","target":"a","siblings":["sibling-repo"]}`,
		`{"file":"make/x.mk","target":"a","siblings":["../a/b"]}`,
		`{"file":"make/x.mk","target":"a","siblings":["../.."]}`,
	} {
		if (makeAdapter{}).Answers(testreg.Test{Scope: testreg.LangFree, Inputs: json.RawMessage(bad)}) {
			t.Errorf("%s answered as a make row", bad)
		}
	}
}

// TestAMakeRowLackingAToolOrASiblingFailsNamingTheRunner: a tool not on
// PATH or a sibling checkout not beside this one fails the row before make
// runs, naming each lack and the runner; with both there, make runs.
func TestAMakeRowLackingAToolOrASiblingFailsNamingTheRunner(t *testing.T) {
	t.Parallel()
	in, err := json.Marshal(makeInputs{File: "make/x.mk", Target: "gate-check", Tools: []string{"dart", "clang-format-18"}, Siblings: []string{"../sibling-repo"}})
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "lint", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Inputs: in}}
	for _, c := range []struct {
		onPath  map[string]bool
		sibling bool
		says    []string
	}{
		{map[string]bool{"dart": true, "clang-format-18": true}, true, nil},
		{map[string]bool{"dart": true}, true, []string{"runner bench-7", "the tool clang-format-18 is not on PATH"}},
		{map[string]bool{"dart": true, "clang-format-18": true}, false, []string{"runner bench-7", "the sibling checkout ../sibling-repo is not beside this one"}},
	} {
		ran := 0
		a := makeAdapter{
			read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
			exec: func(context.Context, []string) (string, error) { ran++; return "", nil },
			plan: emptyPlan,
			look: func(name string) (string, error) {
				if c.onPath[name] {
					return "/bin/" + name, nil
				}
				return "", errors.New("not found")
			},
			stat: func(p string) (os.FileInfo, error) {
				if c.sibling && p == filepath.FromSlash("../sibling-repo") {
					return dirInfo{}, nil
				}
				return nil, os.ErrNotExist
			},
			host: func() (string, error) { return "bench-7", nil },
		}
		res, err := a.Run(context.Background(), "", []testreg.Instance{inst})
		if err != nil || len(res) != 1 {
			t.Fatalf("%v %+v", err, res)
		}
		if c.says == nil {
			if res[0].Verdict != testreg.Pass || ran != 1 {
				t.Errorf("with everything there: %+v, make ran %d times", res[0], ran)
			}
			continue
		}
		if res[0].Verdict != testreg.Fail || ran != 0 {
			t.Errorf("%v: %+v, make ran %d times: a lack fails before make runs", c.says, res[0], ran)
		}
		for _, s := range c.says {
			if !strings.Contains(res[0].Reason, s) {
				t.Errorf("%q does not say %q", res[0].Reason, s)
			}
		}
	}
}

// dirInfo is a directory's file info, for a stat a unit test hands in.
type dirInfo struct{ os.FileInfo }

func (dirInfo) IsDir() bool { return true }

// TestATargetSpecificVariableIsNotARule: "name: export X = 1" and "name: X
// := 1" set a variable for a rule and write none, so a row naming only
// such a line names a test that does not exist; a recipe after ";" may
// hold "=".
func TestATargetSpecificVariableIsNotARule(t *testing.T) {
	t.Parallel()
	rules, err := makeRules([]byte("only-var: export X = 1\nother-var: X := 1\ninline: ; @echo a=b\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rules["only-var"] || rules["other-var"] || !rules["inline"] {
		t.Errorf("rules %v, want inline only", rules)
	}
}

// TestEveryMakeInTheCheckoutHoldsIt: a make row's make, a harness's build
// step and the harness itself each hold the checkout while they run, so the runner's groups,
// which run side by side, never build one build/ at once. (Not parallel:
// it reads a count every make in the checkout adds to.)
func TestEveryMakeInTheCheckoutHoldsIt(t *testing.T) {
	before := checkoutHolds.Load()
	if _, err := execMake(context.Background(), []string{"true"}); err != nil {
		t.Fatal(err)
	}
	b := &builder{log: io.Discard, done: map[string]error{}}
	if err := b.run(context.Background(), []string{"true"}); err != nil {
		t.Fatal(err)
	}
	// a harness asked for nothing that reports nothing; its hold is what is
	// counted
	if _, err := runHarness(context.Background(), []string{"true"}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := checkoutHolds.Load() - before; got != 3 {
		t.Fatalf("%d holds of the checkout, want one for the make row, one for the build step and one for the harness", got)
	}
}

// TestAWaitForTheCheckoutEndsWithItsContext: while another make holds the
// checkout, a make, a build step or a harness whose context ends fails
// naming the wait, and runs nothing. (Not parallel: it holds the checkout.)
func TestAWaitForTheCheckoutEndsWithItsContext(t *testing.T) {
	release, err := holdCheckout(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, run := range map[string]func() error{
		"a make row": func() error { _, err := execMake(ctx, []string{"true"}); return err },
		"a build step": func() error {
			b := &builder{log: io.Discard, done: map[string]error{}}
			return b.run(ctx, []string{"true"})
		},
		"a harness": func() error { _, err := runHarness(ctx, []string{"true"}, nil, io.Discard); return err },
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "waiting for the checkout") {
			t.Errorf("%s ran on past its context: %v", name, err)
		}
	}
}

// TestAMakeVariableCannotTurnTheGateOff: SHELL, MAKEFLAGS and make's other
// own variables would let a row pass without its recipe checking anything;
// a row may not set them.
func TestAMakeVariableCannotTurnTheGateOff(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"SHELL", "MAKEFLAGS", "MFLAGS", "GNUMAKEFLAGS", "MAKE", "MAKEFILES", "MAKELEVEL", "MAKECMDGOALS", "MAKEOVERRIDES", "VPATH", "SUFFIXES"} {
		in := `{"file":"make/x.mk","target":"a","vars":{"` + v + `":"true"}}`
		if (makeAdapter{}).Answers(testreg.Test{Scope: testreg.LangFree, Inputs: json.RawMessage(in)}) {
			t.Errorf("%s answered as a make row", in)
		}
	}
}

// fakeDatabase is make -p's output for the given variables, pattern rules
// and rules (each as make -p prints them: recipe lines start with a tab),
// stopped at the sentinel goal as make stops.
func fakeDatabase(vars, patterns, rules string) string {
	return "# GNU Make 4.3\n\n# Make data base, printed on Wed Nov 25 2026\n\n# Variables\n\n" + vars +
		"\n# Directories\n\n# Implicit Rules\n\n" + patterns +
		"\n# Files\n\n" + rules +
		"\n# VPATH Search Paths\n\n# finished make data base\nmake: *** No rule to make target '" + makeDBSentinel + "'.  Stop.\n"
}

// gateDatabase is a database in which gate-check makes a stamp from two
// schemas and a pattern-built object, and runs its recipe.
func gateDatabase(recipe string) string {
	return fakeDatabase("GO_TEST_JSON = go test -json\n",
		"build/objs/%.o: test/tables/%.cpp build/gen/.stamp\n\t$(CXX) -c $< -o $@\n",
		"gate-check: SOME_VAR = 1\ngate-check: build/bin\n"+recipe+"\n\nbuild/bin: build/objs/main.o\n\nbuild/gen/.stamp: bin/schema test/tables/VNEW_a.schema\n\t./bin/schema generate $^\n\nbuild/objs/main.o:\n#  Implicit rule search has not been done.\n")
}

// emptyPlan is a database in which every asked target runs nothing.
func emptyPlan(context.Context, []string) (string, error) {
	return fakeDatabase("", "", "gate-check:\n\ngate-red:\n\ngate-skips:\n"), errors.New("exit status 2")
}

// vetRun runs one make row of the given inputs against database db and a
// make whose log is log, and returns its result and whether the target ran.
func vetRun(t *testing.T, in makeInputs, db string, log string) (testreg.Result, bool) {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	inst := testreg.Instance{Full: "row", Lang: testreg.LangFree, Test: testreg.Test{Scope: testreg.LangFree, Inputs: raw}}
	ran := false
	var asked []string
	a := makeAdapter{
		read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
		plan: func(_ context.Context, argv []string) (string, error) {
			asked = argv
			return db, errors.New("exit status 2")
		},
		exec: func(context.Context, []string) (string, error) { ran = true; return log, nil },
	}
	res, err := a.Run(context.Background(), "", []testreg.Instance{inst})
	if err != nil || len(res) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	if strings.Join(asked, " ") != strings.Join(makeDBArgv, " ") {
		t.Errorf("read the database with %q, want %q", asked, makeDBArgv)
	}
	return res[0], ran
}

// TestAMakeRowReadsOnlyWhatItsTargetDependsOn: a row's reads are each a
// file its target depends on, through rules, pattern rules and stamps, or
// the row fails before the target runs, naming the file; a name that only
// starts like the file is not it.
func TestAMakeRowReadsOnlyWhatItsTargetDependsOn(t *testing.T) {
	t.Parallel()
	db := gateDatabase("\t./build/bin")
	for _, c := range []struct {
		reads []string
		says  string
	}{
		{[]string{"test/tables/VNEW_a.schema", "test/tables/main.cpp"}, ""},
		{[]string{"test/tables/VNEW_a.schema", "test/tables/VNEW_b.schema"}, "reads test/tables/VNEW_b.schema, and nothing make gate-check makes depends on it"},
		{[]string{"test/tables/VNEW_a.sch"}, "reads test/tables/VNEW_a.sch, and nothing"},
	} {
		r, ran := vetRun(t, makeInputs{File: "make/x.mk", Target: "gate-check", Reads: c.reads}, db, "")
		if c.says == "" {
			if r.Verdict != testreg.Pass || !ran {
				t.Errorf("%v: %+v, ran %v", c.reads, r, ran)
			}
			continue
		}
		if r.Verdict != testreg.Fail || ran || !strings.Contains(r.Reason, c.says) {
			t.Errorf("%v: %+v, ran %v, want a fail saying %q before the target runs", c.reads, r, ran, c.says)
		}
	}
	for _, bad := range []string{
		`{"file":"make/x.mk","target":"a","reads":[""]}`,
		`{"file":"make/x.mk","target":"a","reads":["../x/a.schema"]}`,
		`{"file":"make/x.mk","target":"a","reads":["/abs/a.schema"]}`,
		`{"file":"make/x.mk","target":"a","reads":["test/./a.schema"]}`,
		`{"file":"make/x.mk","target":"a","reads":["test/a b.schema"]}`,
	} {
		if (makeAdapter{}).Answers(testreg.Test{Scope: testreg.LangFree, Inputs: json.RawMessage(bad)}) {
			t.Errorf("%s answered as a make row", bad)
		}
	}
}

// TestARecipeThatRunsGoTestWithoutJSONFails: go test's plain output prints
// nothing for a skipped test, so a target whose recipes (its own, its
// prerequisites', a variable they expand, a target made by $(MAKE)) run go
// test without -json fails before it runs, naming the command; -json on a
// continuation line or in the variable counts, and a path that starts "go
// test/" is no go test.
func TestARecipeThatRunsGoTestWithoutJSONFails(t *testing.T) {
	t.Parallel()
	for recipe, fails := range map[string]bool{
		"\tSCHEMA_SLOW=1 go test ./internal/x -run '^TestA$$' -count=1": true,
		"\t@if go test -count=1 ./x > log 2>&1; then exit 1; fi":        true,
		"\tcd sub && go  test ./...":                                    true,
		"\t$(call RUN_GO,./x)":                                          true,
		"\t$(MAKE) --no-print-directory gate-inner":                     true,
		// -json in a path is no -json flag
		"\t@if go test -v -overlay build/c-json-label-nc/overlay.json ./compiler; then exit 1; fi": true,
		"\tgo test ./x > build/x-json.log":                                                       true,
		"\tgo test -json=true ./x":                                                                false,
		"\tgo test -count=1 \\\n\t\t-json ./internal/x -run '^TestA$$'": false,
		"\tgo test -json ./x | tee build/x.json":                        false,
		"\t$(GO_TEST_JSON) ./x":                                         false,
		"\tprintf 'go test/conformance/go/driver\\n' > build/solo.txt":  false,
		"\techo no go here":                                             false,
	} {
		db := fakeDatabase("GO_TEST_JSON = go test -json\ndefine RUN_GO\ngo test $(1)\nendef\n", "",
			"gate-check:\n"+recipe+"\n\ngate-inner:\n\tgo test ./inner\n")
		r, ran := vetRun(t, makeInputs{File: "make/x.mk", Target: "gate-check"}, db, "")
		if !fails {
			if r.Verdict != testreg.Pass || !ran {
				t.Errorf("%q: %+v, ran %v", recipe, r, ran)
			}
			continue
		}
		if r.Verdict != testreg.Fail || ran || !strings.Contains(r.Reason, "runs go test without -json") {
			t.Errorf("%q: %+v, ran %v, want a fail before the target runs", recipe, r, ran)
		}
	}
}

// TestGoTestPlainOutputInTheLogFails: a target that exits 0 with go test's
// plain summary in its log (a script the database cannot see into ran go
// test without -json) fails; go test -json's events pass.
func TestGoTestPlainOutputInTheLogFails(t *testing.T) {
	t.Parallel()
	for log, fails := range map[string]bool{
		"ok  \tgithub.com/mas-bandwidth/schema/v2/internal/x\t0.123s\n":                                    true,
		"ok  \tgithub.com/mas-bandwidth/schema/v2/internal/x\t(cached)\n":                                  true,
		"?   \tgithub.com/mas-bandwidth/schema/v2/internal/y\t[no test files]\n":                           true,
		`{"Action":"pass","Package":"github.com/mas-bandwidth/schema/v2/internal/x","Elapsed":0.1}` + "\n": false,
		"ok, every row checked\n": false,
		"ok  \tgithub.com/mas-bandwidth/schema/v2/internal/x\t0.123s (go test -json, read by gotestrun)\n": false,
	} {
		r, _ := vetRun(t, makeInputs{File: "make/x.mk", Target: "gate-check"}, gateDatabase("\ttrue"), log)
		if got := r.Verdict == testreg.Fail; got != fails {
			t.Errorf("%q: %+v, want failed %v", log, r, fails)
		}
		if fails && !strings.Contains(r.Reason, "plain output") {
			t.Errorf("%q: %q does not name the plain output", log, r.Reason)
		}
	}
}

// TestADatabaseReadInPartFailsTheRow: a database with no Files section, or
// one make stopped printing for a reason other than the sentinel goal,
// cannot say what a target runs, so the row fails naming why and never
// runs; an adapter with no way to read the database refuses to run at all.
func TestADatabaseReadInPartFailsTheRow(t *testing.T) {
	t.Parallel()
	whole := gateDatabase("\ttrue")
	for name, db := range map[string]string{
		"no Files section":    strings.Replace(whole, "\n# Files\n", "\n", 1),
		"stopped another way": strings.Replace(whole, "No rule to make target '"+makeDBSentinel+"'", "Makefile:3: *** missing separator", 1),
	} {
		r, ran := vetRun(t, makeInputs{File: "make/x.mk", Target: "gate-check"}, db, "")
		if r.Verdict != testreg.Fail || ran || !strings.Contains(r.Reason, "cannot be read before it runs") {
			t.Errorf("%s: %+v, ran %v", name, r, ran)
		}
	}
	a := makeAdapter{read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
		exec: func(context.Context, []string) (string, error) { return "", nil }}
	if _, err := a.Run(context.Background(), "", []testreg.Instance{makeInst(t, "c", "make/x.mk", "gate-check")}); err == nil || !strings.Contains(err.Error(), "no way to read make's database") {
		t.Errorf("an adapter with no database ran: %v", err)
	}
}

// TestTheDatabaseIsReadOncePerRun: the rows of one run share one read of
// make's database.
func TestTheDatabaseIsReadOncePerRun(t *testing.T) {
	t.Parallel()
	reads := 0
	a := makeAdapter{read: func(string) ([]byte, error) { return []byte(fakeRules), nil },
		plan:  func(ctx context.Context, argv []string) (string, error) { reads++; return emptyPlan(ctx, argv) },
		exec:  func(context.Context, []string) (string, error) { return "", nil },
		cache: &makeDBCache{}}
	res, err := a.Run(context.Background(), "", []testreg.Instance{makeInst(t, "a", "make/x.mk", "gate-check"), makeInst(t, "b", "make/x.mk", "gate-red")})
	if err != nil || len(res) != 2 || res[0].Verdict != testreg.Pass || res[1].Verdict != testreg.Pass {
		t.Fatalf("%v %+v", err, res)
	}
	if reads != 1 {
		t.Errorf("the database was read %d times for one run, want once", reads)
	}
}
