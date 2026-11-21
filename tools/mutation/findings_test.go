package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The reviewer's findings on the sweep (F-051, F-052, F-226, F-227), each as
// her repro with her controls. The outputs below are what go test -v and cc
// print for testdata/corpus, recorded, so these tests run no process but git;
// `go run ./tools/mutation corpus` runs the same cases for real.

const sumGo = "package sum\n\nfunc Sum() int { return 2 + 3 }\n"

// What go test -v ./sum prints for each answer TestSum can get.
const (
	goPassed = "=== RUN   TestSum\n--- PASS: TestSum (0.00s)\nPASS\nok  \texample.org/mutationcorpus/sum\t0.002s\n"
	goFailed = "=== RUN   TestSum\n    sum_test.go:20: 2 + 3 = 4, want 5\n--- FAIL: TestSum (0.00s)\nFAIL\nFAIL\texample.org/mutationcorpus/sum\t0.002s\nFAIL\n"
	// Every test filtered out by -run: still `ok`, with nothing run.
	goFiltered = "testing: warning: no tests to run\nPASS\nok  \texample.org/mutationcorpus/sum\t0.002s [no tests to run]\n"
	// The only test skipped under the mutant: still `ok`.
	goSkipped = "=== RUN   TestSum\n    sum_test.go:16: the corpus's skipped-under-the-mutant case\n--- SKIP: TestSum (0.00s)\nPASS\nok  \texample.org/mutationcorpus/sum\t0.002s\n"
	// Go test's own -timeout: the stage exits red before the sweep's bound.
	goTesterDeadline = "=== RUN   TestSum\npanic: test timed out after 1s\n\trunning tests:\n\t\tTestSum (1s)\n\ngoroutine 34 [running]:\ntesting.(*M).startAlarm.func1()\nFAIL\texample.org/mutationcorpus/sum\t1.004s\nFAIL\n"
	goBuildFailed    = "# example.org/mutationcorpus/sum\nsum/sum.go:3:30: undefined: missing_value\nFAIL\texample.org/mutationcorpus/sum [build failed]\nFAIL\n"
)

// What the native stage prints: cc, then the program it built.
const (
	nativePassed      = "CORPUS_SUM_PASSED\n"
	nativeFailed      = "the sum is -1, want 5\n"
	nativeUnbuildable = "native/sum.c: In function 'main':\nnative/sum.c:7:20: error: 'missing_value' undeclared (first use in this function)\n    7 |         int got = 2 + missing_value;\n      |                       ^~~~~~~~~~~~~\n"
	// the same refusal from a make recipe, as a target's conformance stage
	// builds its driver
	nativeUnbuildableByMake = "mkdir -p build\ncc -std=c11 -o build/conformance-harness build/arithmetic.c\nbuild/arithmetic.c: In function 'main':\nbuild/arithmetic.c:7:5: error: duplicate case value\n    7 |     case 5: return 0;\n      |     ^~~~\nmake: *** [Makefile:5: arithmetic-build] Error 1\n"
)

// sumFile is the file a stage's mutants edit: sum.go for a go test stage, and
// for the native stage a file no Go build reads, so its compiler is the
// stage's own.
func sumFile(st Stage) string {
	if st.Counted {
		return "sum/sum.go"
	}
	return "native/sum.c"
}

// sumTree is a scratch tree holding the sum, the scope over it with the one
// stage, and one mutant per edit of "2 + 3", each named by its new text.
func sumTree(t *testing.T, st Stage, edits ...string) (Scope, string, []Mutant) {
	t.Helper()
	tree := t.TempDir()
	file := sumFile(st)
	if err := os.MkdirAll(filepath.Join(tree, filepath.Dir(file)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, file), []byte(sumGo), 0o644); err != nil {
		t.Fatal(err)
	}
	at := strings.Index(sumGo, "2 + 3")
	var muts []Mutant
	for _, e := range edits {
		muts = append(muts, Mutant{ID: e, File: file, Op: "corpus", Old: "2 + 3", New: e, Start: at, End: at + len("2 + 3")})
	}
	return Scope{Name: "corpus", Go: []string{"sum"}, Stages: []Stage{st}}, tree, muts
}

// answers is an Exec that prints, for the sum the scratch file holds, what the
// stage would print, and answers `go build` as Go's compiler would.
func answers(t *testing.T, bySum map[string]string, failing map[string]bool) Exec {
	return func(dir, sh string, _ time.Duration) ([]byte, bool, bool, time.Duration) {
		file := filepath.Join(dir, "sum", "sum.go")
		if !exists(file) {
			file = filepath.Join(dir, "native", "sum.c")
		}
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sum := strings.TrimSuffix(strings.TrimPrefix(string(b), "package sum\n\nfunc Sum() int { return "), " }\n")
		if strings.HasPrefix(sh, "go build") {
			if strings.Contains(sum, "missing") {
				return []byte("sum/sum.go:3:30: undefined: missing_value\n"), false, false, time.Second
			}
			return nil, true, false, time.Second
		}
		out, ok := bySum[sum]
		if !ok {
			t.Fatalf("no recorded output for the sum %q", sum)
		}
		return []byte(out), !failing[sum], false, time.Second
	}
}

func verdicts(t *testing.T, s Scope, tree string, muts []Mutant, run Exec) (map[string]Result, error) {
	t.Helper()
	got := map[string]Result{}
	err := sweep(s, tree, muts, sweepOptions{BaselineLimit: time.Minute}, run, &strings.Builder{}, func(r Result) { got[r.ID] = r })
	return got, err
}

// F-051: a go test that ran no test, on the unmutated tree or under one
// mutant, is not a stage that tested it.
func TestSweepHoldsEveryStageToTheTestsItRan(t *testing.T) {
	counted := Stage{Name: "go", Sh: "go test -count=1 -v ./sum", Proof: goTestProof, Counted: true}
	bySum := map[string]string{"2 + 3": goPassed, "3 + 2": goPassed, "2 + 2": goFailed, "2 - 3": goSkipped}
	s, tree, muts := sumTree(t, counted, "2 + 2", "3 + 2", "2 - 3")
	got, err := verdicts(t, s, tree, muts, answers(t, bySum, map[string]bool{"2 + 2": true}))
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{
		"2 + 2": outcomeKilled,   // control: the assertion fails
		"3 + 2": outcomeSurvived, // control: the same sum, the test ran and passed
		"2 - 3": outcomeUnproven, // TestSum skipped under the mutant: it was a survivor
	} {
		if got[id].Outcome != want {
			t.Errorf("mutant %s is %s, want %s: %s", id, got[id].Outcome, want, got[id].Tail)
		}
	}
	if r := got["2 - 3"]; !strings.Contains(r.Tail, "TestSum") {
		t.Errorf("the unproven mutant does not name the test that did not run: %q", r.Tail)
	}

	// every test filtered out, on every tree: the stage proves nothing and the
	// sweep refuses to start
	s, tree, muts = sumTree(t, counted, "2 + 2")
	got, err = verdicts(t, s, tree, muts, answers(t, map[string]string{"2 + 3": goFiltered, "2 + 2": goFiltered}, nil))
	if err == nil || !strings.Contains(err.Error(), "ran no test") {
		t.Errorf("a stage that filtered out every test: the sweep said %v with verdicts %v, want a refusal naming \"ran no test\"", err, outcomes(got))
	}

	// a stage that is not go test keeps its proof under every mutant
	native := Stage{Name: "native", Sh: "cc native/sum.c && ./a.out", Proof: "CORPUS_SUM_PASSED"}
	s, tree, muts = sumTree(t, native, "3 + 2", "2 - 3")
	got, err = verdicts(t, s, tree, muts, answers(t, map[string]string{"2 + 3": nativePassed, "3 + 2": nativePassed, "2 - 3": ""}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got["3 + 2"].Outcome != outcomeSurvived || got["2 - 3"].Outcome != outcomeUnproven {
		t.Errorf("proof kept: %s, proof lost: %s; want survived and unproven", got["3 + 2"].Outcome, got["2 - 3"].Outcome)
	}

	// two packages, each with a TestSum: sum's skips under the mutant and
	// other's, which skipped before, runs. By name alone one TestSum passed
	// before and after.
	two := Stage{Name: "go", Sh: "go test -count=1 -v ./sum ./other", Proof: goTestProof, Counted: true}
	const (
		twoBefore = "=== RUN   TestSum\n--- PASS: TestSum (0.00s)\nPASS\nok  \texample.org/mutationcorpus/sum\t0.002s\n=== RUN   TestSum\n    other_test.go:15: runs only under the mutant that skips sum's TestSum\n--- SKIP: TestSum (0.00s)\nPASS\nok  \texample.org/mutationcorpus/other\t0.002s\n"
		twoAfter  = "=== RUN   TestSum\n    sum_test.go:16: the corpus's skipped-under-the-mutant case\n--- SKIP: TestSum (0.00s)\nPASS\nok  \texample.org/mutationcorpus/sum\t0.002s\n=== RUN   TestSum\n--- PASS: TestSum (0.00s)\nPASS\nok  \texample.org/mutationcorpus/other\t0.002s\n"
	)
	s, tree, muts = sumTree(t, two, "3 + 2", "2 - 3")
	got, err = verdicts(t, s, tree, muts, answers(t, map[string]string{"2 + 3": twoBefore, "3 + 2": twoBefore, "2 - 3": twoAfter}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got["3 + 2"].Outcome != outcomeSurvived || got["2 - 3"].Outcome != outcomeUnproven {
		t.Errorf("two packages, same test name: control %s, sum's TestSum skipped %s; want survived and unproven", got["3 + 2"].Outcome, got["2 - 3"].Outcome)
	}
	if r := got["2 - 3"]; !strings.Contains(r.Tail, "example.org/mutationcorpus/sum TestSum") {
		t.Errorf("the unproven mutant does not name the package whose test did not run: %q", firstLine(r.Tail))
	}

	// a stage whose proof its own command line meets, as an empty make recipe
	// whose target the proof names: refused before it runs
	echo := Stage{Name: "native", Sh: ": native-gate", Proof: "native-gate"}
	s, tree, muts = sumTree(t, echo, "2 - 3")
	got, err = verdicts(t, s, tree, muts, answers(t, map[string]string{"2 + 3": "native-gate\n", "2 - 3": "native-gate\n"}, nil))
	if err == nil || !strings.Contains(err.Error(), "met by its own command line") {
		t.Errorf("a proof met by its own command: the sweep said %v with verdicts %v", err, outcomes(got))
	}

	// an unproven mutant fails the nightly as a survivor does
	if err := enforceEquivalents([]Result{{Mutant: Mutant{ID: "cccccccccccc"}, Scope: "check", Outcome: outcomeUnproven}}); err == nil {
		t.Error("an unproven mutant passed the enforcement")
	}
}

// F-051, the class: every go test stage the sweep runs is verbose and
// counted, and no stage's proof is met by what a stage that ran nothing
// prints.
func TestEveryStageProvesItTested(t *testing.T) {
	all, err := scopes(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range all {
		for _, st := range s.Stages {
			proof := regexp.MustCompile(st.Proof)
			if proofMetByCommand(proof, st.Sh) {
				t.Errorf("scope %s stage %s: the proof %q is met by its own command %q", s.Name, st.Name, st.Proof, st.Sh)
			}
			if !strings.Contains(st.Sh, "go test") {
				continue
			}
			n++
			if !st.Counted || !strings.Contains(st.Sh, " -v ") {
				t.Errorf("scope %s stage %s runs go test and does not count the tests it passed: %q", s.Name, st.Name, st.Sh)
			}
		}
	}
	if n == 0 {
		t.Fatal("no go test stage found: this test checked nothing")
	}

	// what the runners print when they ran: each proof meets it, and meets
	// nothing that ran no case
	for _, c := range []struct {
		name, proof, ran, ranNothing string
	}{
		{"conformance", conformanceProof("go"), "TABLES CONFORMANCE — surface x language\n\nsurface       go                     \n-------------------------------------\nwire          pass 139/139           \nwrite-refusal-message pass 27/27             \n\ntables conformance: every registered surface passes\n",
			"TABLES CONFORMANCE — surface x language\n\nsurface       go                     \n-------------------------------------\nwire          absent                 \n"},
		{"hostile, summary", hostileStage("c", "schema-c-runs-hostile-corpus").Proof, "c: 412 cases: 400 pass, 12 carried as FIX-CARD, 0 skipped, 0 unexplained, 0 stale\n", "make: Nothing to be done for 'schema-c-runs-hostile-corpus'.\nc: 0 cases: 0 pass, 0 carried as FIX-CARD, 0 skipped, 0 unexplained, 0 stale\n"},
		{"hostile, js summary", hostileStage("js", "schema-js-runs-hostile-corpus").Proof, "412 cases, 980 rows: 960 pass, 20 carried as FIX-CARD, 0 unexplained\n", "0 cases, 0 rows: 0 pass, 0 carried as FIX-CARD, 0 unexplained\n"},
		{"hostile, PASS lines", hostileStage("java", "schema-java-runs-hostile-corpus").Proof, "PASS empty-root\nPASS long-text\n", "java -cp build/java-hostile/classes Runner corpus findings packet\n"},
		{"hostile, slow gate", hostileStage("lua", "schema-lua-runs-hostile-corpus").Proof, "SLOW GATE schema-lua-runs-hostile-corpus: 3 named gate(s) ran and PASS under SCHEMA_SLOW=1 (build/slowgate/schema-lua-runs-hostile-corpus.log)\n", "lua hostile corpus: every verdict, named reason and decoded-value digest (Lua 5.4.8)\n"},
	} {
		p := regexp.MustCompile(c.proof)
		if !p.MatchString(c.ran) {
			t.Errorf("%s: %q does not meet what the runner prints when it ran", c.name, c.proof)
		}
		if p.MatchString(c.ranNothing) {
			t.Errorf("%s: %q is met by a run of no case", c.name, c.proof)
		}
	}
}

// F-226 and F-227: a red stage is judged by what went red.
func TestARedStageIsJudgedByWhatFailed(t *testing.T) {
	for _, c := range []struct {
		name, out, want string
	}{
		{"go test assertion (control)", goFailed, outcomeKilled},
		{"go compiler (control)", goBuildFailed, outcomeBroken},
		{"native program's check (control)", nativeFailed, outcomeKilled},
		{"C compiler", nativeUnbuildable, outcomeBroken},
		{"C compiler under make", nativeUnbuildableByMake, outcomeBroken},
		{"C compiler, a path with a space", strings.ReplaceAll(nativeUnbuildableByMake, "build/arithmetic.c", "build/arithmetic example.c"), outcomeBroken},
		{"javac, a path with a space", "src/My Driver.java:12: error: cannot find symbol\n", outcomeBroken},
		{"a go test log line naming a file (control)", "    sum_test.go:20: the sum is 4: error: want 5\n--- FAIL: TestSum (0.00s)\n", outcomeKilled},
		{"linker", "/usr/bin/ld: sum.o: in function `main':\nsum.c:(.text+0x9): undefined reference to `missing'\ncollect2: error: ld returned 1 exit status\n", outcomeBroken},
		{"javac", "src/Driver.java:12: error: cannot find symbol\n        int got = 2 + missing;\n1 error\n", outcomeBroken},
		{"dotnet", "Driver.cs(7,23): error CS0103: The name 'missing' does not exist in the current context\n", outcomeBroken},
		{"rustc", "error[E0425]: cannot find value `missing` in this scope\n --> src/main.rs:2:19\n", outcomeBroken},
		{"cargo test assertion (control)", "test sum ... FAILED\n\nfailures:\n\nerror: test failed, to rerun pass `--lib`\n", outcomeKilled},
		{"dart", "lib/sum.dart:3:27: Error: Undefined name 'missing'.\n", outcomeBroken},
		{"elixir", "** (CompileError) lib/sum.ex:3: undefined function missing/0\n", outcomeBroken},
		{"go test's own deadline", goTesterDeadline, outcomeTimeout},
		{"the harness's reply deadline", "  c / wire-fuzz: the driver stalled: no reply within 30s\n", outcomeTimeout},
	} {
		if got := failed([]byte(c.out)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}

	// and through the sweep: a native stage whose compiler refuses the mutant
	native := Stage{Name: "native", Sh: "cc native/sum.c && ./a.out", Proof: "CORPUS_SUM_PASSED"}
	s, tree, muts := sumTree(t, native, "2 - 3", "2 * missing_value")
	got, err := verdicts(t, s, tree, muts, answers(t, map[string]string{"2 + 3": nativePassed, "2 - 3": nativeFailed, "2 * missing_value": nativeUnbuildable}, map[string]bool{"2 - 3": true, "2 * missing_value": true}))
	if err != nil {
		t.Fatal(err)
	}
	if got["2 - 3"].Outcome != outcomeKilled || got["2 * missing_value"].Outcome != outcomeBroken {
		t.Errorf("failed check: %s, compiler refusal: %s; want killed and build-broken", got["2 - 3"].Outcome, got["2 * missing_value"].Outcome)
	}
	// a go test stage whose own -timeout fires
	counted := Stage{Name: "go", Sh: "go test -count=1 -v -timeout=1s ./sum", Proof: goTestProof, Counted: true}
	s, tree, muts = sumTree(t, counted, "3 + 3")
	got, err = verdicts(t, s, tree, muts, answers(t, map[string]string{"2 + 3": goPassed, "3 + 3": goTesterDeadline}, map[string]bool{"3 + 3": true}))
	if err != nil {
		t.Fatal(err)
	}
	if got["3 + 3"].Outcome != outcomeTimeout {
		t.Errorf("go test's own deadline: %s, want timeout", got["3 + 3"].Outcome)
	}
}

// F-052: the scratch copy is made in a directory the run makes, so nothing
// under -work that the tool did not make is removed, and two runs of one
// scope get two copies.
func TestScratchTreeRemovesNothingItDidNotMake(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not on the path, and the scratch copy is a git worktree")
	}
	root := t.TempDir()
	for name, content := range map[string]string{"Makefile": "all:\n", "SIBLINGS": "# none\n", "go.mod": "module example.org/root\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "root"},
	} {
		if b, err := exec.Command("git", append([]string{"-C", root, "-c", "core.hooksPath=/dev/null"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, b)
		}
	}
	work := t.TempDir()
	notes := filepath.Join(work, "check", "notes.txt")
	peer := filepath.Join(work, "peer.txt")
	for _, f := range []string{notes, peer} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("not the tool's\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first, removeFirst, err := scratchTree(root, work, "check")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{notes, peer} { // peer.txt is the control
		if !exists(f) {
			t.Errorf("%s was removed by making one scratch copy", f)
		}
	}
	second, removeSecond, err := scratchTree(root, work, "check")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("two runs of scope check share %s", first)
	}
	for _, tree := range []string{first, second} {
		if !exists(filepath.Join(tree, "go.mod")) {
			t.Errorf("%s holds no checkout", tree)
		}
	}
	for _, f := range []string{notes, peer} { // peer.txt is the control
		if !exists(f) {
			t.Errorf("%s was removed by making the scratch copies", f)
		}
	}
	removeFirst()
	removeSecond()
	for _, f := range []string{notes, peer} {
		if !exists(f) {
			t.Errorf("%s was removed with the scratch copies", f)
		}
	}
	for _, tree := range []string{first, second} {
		if exists(filepath.Dir(tree)) {
			t.Errorf("%s is left after its run removed it", filepath.Dir(tree))
		}
	}

	// a copy that cannot be made takes away what it made, and only that
	if _, remove, err := scratchTree(t.TempDir(), work, "check"); err == nil || remove != nil {
		t.Errorf("a scratch copy of a directory that is no git checkout: err %v, remove set %v", err, remove != nil)
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, " ") != "check peer.txt" || !exists(notes) {
		t.Errorf("after a failed copy the work directory holds %v", left)
	}
}
