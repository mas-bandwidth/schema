package ci

// NO SILENT SKIPS: A TEST THAT EXISTS RUNS, OR SAYS WHY NOT, AND CI COUNTS IT.
//
// The owner, 2026-11-21: "We need to stop having silent tests, or accidentally
// not running tests for any reason." A skipped test exits 0, a test file a
// build constraint excludes is never opened, and a TestMain that returns
// early runs nothing: each is green over a test that did not run.
//
// These gates read the source (silent_skips_scan_test.go), so they are red at
// the pull request that writes the skip, before any lane misses the test:
//
//   - every t.Skip gives a reason, and the reason is one a line of the skip
//     ledger (.github/skip-ledger.txt) names. The same ledger is what each
//     lane's record is read against after it ran (tools/gotestreport -skips):
//     there a skip the lane was meant to satisfy is red;
//   - every TestMain reaches m.Run, except in a helper process its own
//     package starts;
//   - every _test.go file a build constraint keeps out of the default build
//     is compiled by a lane that runs `go test` under that constraint.
//
// Each gate fails when it looked at nothing, and each has a negative control
// over a fixture tree.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/skipledger"
)

const skipLedgerPath = ".github/skip-ledger.txt"

// repoSkipScan reads the repository once for the three gates that share it.
var repoSkipScan = sync.OnceValues(func() (*skipScan, error) {
	root, err := filepath.Abs("../..")
	if err != nil {
		return nil, err
	}
	return scanSkipSites(root)
})

func readSkipLedger(t *testing.T, root string) *skipledger.Ledger {
	t.Helper()
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(skipLedgerPath)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	l, err := skipledger.Read(f, skipLedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// unledgered is what the skip gate refuses in a scan: a skip with no reason,
// and a reason no ledger line names.
func unledgered(scan *skipScan, l *skipledger.Ledger) []string {
	var out []string
	for _, s := range scan.sites {
		switch {
		case s.bare:
			out = append(out, fmt.Sprintf("%s:%d: the skip gives no reason: say why in t.Skip, so the lane that reads it can tell an expected skip from a lost toolchain", s.file, s.line))
		case s.literal && strings.TrimSpace(s.reason) == "":
			out = append(out, fmt.Sprintf("%s:%d: the skip's reason is empty", s.file, s.line))
		case s.literal && l.Classify(s.reason) == nil:
			out = append(out, fmt.Sprintf("%s:%d: the skip reason %q matches no line of %s: name its class there (which lane is meant to satisfy it?)", s.file, s.line, s.reason, l.Path))
		}
	}
	return out
}

func TestEverySkipInTheTreeNamesALedgerReason(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	l := readSkipLedger(t, root)
	scan, err := repoSkipScan()
	if err != nil {
		t.Fatal(err)
	}
	// A scan that found nothing passes for the wrong reason. The floors are
	// far under the tree (3,300 test files, 400 skips) and far over a scan
	// pointed at the wrong directory.
	if scan.files < 1000 || len(scan.sites) < 100 {
		t.Fatalf("the scan read %d files that import testing and %d skip calls: it is looking at the wrong tree", scan.files, len(scan.sites))
	}
	literal := 0
	for _, s := range scan.sites {
		if s.literal {
			literal++
		}
	}
	t.Logf("%d files import testing; %d skip calls, %d with a reason the source states (the rest build theirs at run time, and the lane's ledger reads those)", scan.files, len(scan.sites), literal)
	for _, msg := range unledgered(scan, l) {
		t.Error(msg)
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The negative control: a skip with a reason the ledger does not name, one
// with none, and a SkipNow are each refused by file and line; the skips the
// ledger names, and a Skip method that is not testing's, are not.
func TestSkipLedgerGateNegativeControl(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"p/p_test.go": `package p

import "testing"

type reader struct{}

func (reader) Skip(n int) {}

func TestKnown(t *testing.T)   { t.Skip("SCHEMA_SLOW: this test shells out to cc; set SCHEMA_SLOW=1 to run it") }
func TestFormat(t *testing.T)  { t.Skipf("no %s on PATH", "elixir") }
func TestExcuse(t *testing.T)  { t.Skip("a brand new excuse") }
func TestBare(t *testing.T)    { t.Skip() }
func TestNow(t *testing.T)     { t.SkipNow() }
func TestSub(t *testing.T)     { t.Run("x", func(t *testing.T) { t.Skipf("another %d excuse", 1) }) }
func TestDynamic(t *testing.T) { var why string; t.Skip(why) }
func TestReader(t *testing.T)  { var r reader; r.Skip(8) }
func helper(tb testing.TB)     { tb.Skip("yet another excuse") }
`,
		"p/testdata/q_test.go": `package q

import "testing"

func TestFixture(t *testing.T) { t.Skip("fixtures are data, not tests") }
`,
	})
	l, err := skipledger.Read(strings.NewReader("skip slow ^SCHEMA_SLOW: \nskip tool:any ^no \\S+ on PATH$\n"), "ledger")
	if err != nil {
		t.Fatal(err)
	}
	scan, err := scanSkipSites(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.sites) != 8 {
		t.Errorf("the scan found %d skip calls, want 8 (the reader's Skip and the testdata file are not test skips): %+v", len(scan.sites), scan.sites)
	}
	got := strings.Join(unledgered(scan, l), "\n")
	for _, want := range []string{
		`p/p_test.go:11: the skip reason "a brand new excuse" matches no line`,
		`p/p_test.go:12: the skip gives no reason`,
		`p/p_test.go:13: the skip gives no reason`,
		`p/p_test.go:14: the skip reason "another X excuse" matches no line`,
		`p/p_test.go:17: the skip reason "yet another excuse" matches no line`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the gate did not refuse %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "\n") + 1; n != 5 {
		t.Errorf("the gate refused %d skips, want 5:\n%s", n, got)
	}
}

func TestNoTestMainLeavesBeforeItRunsTheTests(t *testing.T) {
	t.Parallel()
	scan, err := repoSkipScan()
	if err != nil {
		t.Fatal(err)
	}
	if scan.testMains == 0 {
		t.Fatal("the scan found no TestMain: it is looking at the wrong tree")
	}
	t.Logf("%d TestMain functions, %d exits before m.Run (each a helper-process branch or a refusal below)", scan.testMains, len(scan.exits))
	for _, at := range scan.noRun {
		t.Errorf("%s: TestMain never calls m.Run, so no test of its package runs and `go test` prints ok", at)
	}
	for _, e := range scan.silentExits() {
		t.Errorf("%s:%d: TestMain leaves by %s before m.Run: every test of the package is skipped and the run is green. "+
			"Fail with a non-zero os.Exit, or skip inside the tests with a reason the ledger names", e.file, e.line, e.what)
	}
}

func TestTestMainGateNegativeControl(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		// a toolchain check that leaves quietly: the class the gate closes
		"quiet/main_test.go": `package quiet

import (
	"os"
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {
	if _, err := exec.LookPath("elixir"); err != nil {
		return
	}
	if os.Getenv("CI_SKIP_QUIET") != "" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
`,
		"never/main_test.go": `package never

import "testing"

func TestMain(m *testing.M) {}
`,
		// a helper process its own test starts, and a loud failure: both fine
		"helper/main_test.go": `package helper

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("HELPER_MODE") != "" {
		os.Exit(0)
	}
	if _, err := os.Stat("go.mod"); err != nil {
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestStartsHelper(t *testing.T) { t.Setenv("HELPER_MODE", "1") }
`,
		// the same, read through a constant in a switch
		"child/main_test.go": `package child

import (
	"os"
	"os/exec"
	"testing"
)

const childEnv = "CHILD_MODE"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "block":
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestStartsChild(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), childEnv+"=block")
}
`,
	})
	scan, err := scanSkipSites(root)
	if err != nil {
		t.Fatal(err)
	}
	if scan.testMains != 4 {
		t.Errorf("the scan found %d TestMain functions, want 4", scan.testMains)
	}
	if len(scan.noRun) != 1 || !strings.HasPrefix(scan.noRun[0], "never/main_test.go:") {
		t.Errorf("a TestMain with no m.Run was not refused: %v", scan.noRun)
	}
	var got []string
	for _, e := range scan.silentExits() {
		got = append(got, fmt.Sprintf("%s:%d %s", e.file, e.line, e.what))
	}
	want := []string{"quiet/main_test.go:11 return", "quiet/main_test.go:14 os.Exit(0)"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("silent exits:\n got %v\nwant %v", got, want)
	}
}

func TestEveryConstrainedTestFileCompilesInSomeLane(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	scan, err := scanConstrainedTests(root)
	if err != nil {
		t.Fatal(err)
	}
	proofs, err := laneProofs(root)
	if err != nil {
		t.Fatal(err)
	}
	// the tree carries tagged leg tests and platform-bound tests, and lanes
	// for them: a scan that sees none of either is reading the wrong text
	if scan.testFiles < 1000 || len(scan.constrained) == 0 || len(scan.excluded) == 0 {
		t.Fatalf("the scan read %d test files, %d with a build constraint, %d outside the default build: it is looking at the wrong tree",
			scan.testFiles, len(scan.constrained), len(scan.excluded))
	}
	if len(proofs) == 0 {
		t.Fatal("no workflow or make recipe runs `go test` under a build tag or on another OS: the lane reader is looking at the wrong text")
	}
	t.Logf("%d test files; %d carry a build constraint; %d are outside the default build (linux, amd64, no tags); %d tagged or other-OS go test commands",
		scan.testFiles, len(scan.constrained), len(scan.excluded), len(proofs))
	for _, c := range neverCompiled(scan, proofs) {
		t.Errorf("%s (%s) is compiled by no lane: no workflow, and no make target a workflow names, runs `go test` on ./%s under that constraint, "+
			"so its tests never run and nothing says so. Run the package in a lane that satisfies it", c.file, c.text, c.dir)
	}
}

func TestConstrainedTestGateNegativeControl(t *testing.T) {
	t.Parallel()
	const tagged = "//go:build schema_leg_x\n\npackage p\n"
	root := writeTree(t, map[string]string{
		"a/plain_test.go":        "package a\n",
		"a/unix_test.go":         "//go:build unix\n\npackage a\n",
		"a/on_test.go":           tagged,                                   // make target a workflow runs: proven
		"b/on_test.go":           tagged,                                   // the tagged command names ./a/ only
		"c/on_test.go":           "//go:build schema_leg_y\n\npackage c\n", // its make target is in no workflow
		"d/lock_windows_test.go": "package d\n",                            // a Windows job tests ./d/
		"e/lock_windows_test.go": "package e\n",                            // the Windows job only vets it
		"f/wire_plan9_test.go":   "package f\n",
		"g/both_test.go":         "//go:build windows && schema_leg_x\n\npackage g\n",
		"Makefile":               "test-x-tagged:\n\tSCHEMA_SLOW=1 go test -count=1 -tags schema_leg_x ./a/\n\ntest-y-tagged:\n\tgo test -tags schema_leg_y ./c/\n",
		// F-180: a Windows step that only echoes the command, one whose
		// test step is off, one that cds elsewhere first, one with a
		// working-directory, and a job that is off: none runs the package
		"h/echo_windows_test.go":   "package h\n",
		"i/off_windows_test.go":    "package i\n",
		"j/moved_windows_test.go":  "package j\n",
		"k/wd_windows_test.go":     "package k\n",
		"l/joboff_windows_test.go": "package l\n",
		".github/workflows/full.yml": "jobs:\n  tagged:\n    runs-on: [self-hosted, Linux]\n    steps:\n      - run: make test-x-tagged\n" +
			"  windows:\n    runs-on: windows-latest\n    steps:\n      - run: go vet ./...\n      - run: go test ./d/\n" +
			"      - run: echo go test ./h/\n" +
			"      - if: false\n        run: go test ./i/\n" +
			"      - run: |\n          cd nested\n          go test ./j/\n" +
			"      - working-directory: nested\n        run: go test ./k/\n" +
			"  windows-off:\n    if: ${{ false }}\n    runs-on: windows-latest\n    steps:\n      - run: go test ./l/\n",
	})
	scan, err := scanConstrainedTests(root)
	if err != nil {
		t.Fatal(err)
	}
	proofs, err := laneProofs(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range neverCompiled(scan, proofs) {
		got = append(got, c.file)
	}
	want := []string{"b/on_test.go", "c/on_test.go", "e/lock_windows_test.go", "f/wire_plan9_test.go", "g/both_test.go",
		"h/echo_windows_test.go", "i/off_windows_test.go", "j/moved_windows_test.go", "k/wd_windows_test.go", "l/joboff_windows_test.go"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("never compiled:\n got %v\nwant %v\nproofs: %+v", got, want, proofs)
	}
	if len(scan.constrained) != 13 || len(scan.excluded) != 12 {
		t.Errorf("the scan found %d constrained and %d excluded test files, want 13 and 12", len(scan.constrained), len(scan.excluded))
	}
}

// F-180 CORPUS: testdata/f180-lanes is a committed tree, a workflow of Windows
// jobs and a Windows-only test package for each. Only a go test the step
// itself runs, from the tree's root, in a job and step that are on, compiles
// its package; WANT lists the rest. d/ and m/ are the controls.
func TestConstrainedTestGateCorpus(t *testing.T) {
	t.Parallel()
	root := filepath.Join("testdata", "f180-lanes")
	b, err := os.ReadFile(filepath.Join(root, "WANT"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for line := range strings.Lines(string(b)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			want = append(want, line)
		}
	}
	scan, err := scanConstrainedTests(root)
	if err != nil {
		t.Fatal(err)
	}
	proofs, err := laneProofs(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range neverCompiled(scan, proofs) {
		got = append(got, c.file)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("never compiled:\n got %v\nwant %v\nproofs: %+v", got, want, proofs)
	}
	// the scan proves it read the tree: every package of it is a candidate
	if len(scan.excluded) != len(want)+2 {
		t.Errorf("the scan found %d Windows-only test files, want %d (WANT and the two controls)", len(scan.excluded), len(want)+2)
	}
}

// THE LANES READ THE LEDGER. A ci-fast job that runs `go test` records what
// ran (PGROUP_GO_TEST_JSON) and ends with the ledger step, so a job added
// tomorrow cannot run tests whose skips nobody counts.

type ledgerLane struct {
	job      string
	goTest   bool     // a step runs `go test` directly
	records  bool     // it sets PGROUP_GO_TEST_JSON
	reads    bool     // an `if: always()` step runs gotestreport -skips on the record
	provides []string // every class its -provides strings name
}

var (
	providesAssign = regexp.MustCompile(`provides="([^"]*)"`)
	providesFlag   = regexp.MustCompile(`-provides "([^"]*)"`)
	// go test where a shell runs it: first on the line (after `run:` and any
	// VAR=value words) or after ; && || ( — never the words inside a step's
	// name or an echo.
	goTestCommand = regexp.MustCompile(`(^|[;&|(]\s*)(-\s+)?(run:\s+)?([A-Za-z_][A-Za-z0-9_]*=\S*\s+)*go test(\s|$)`)
)

func ledgerLanes(workflow string) []ledgerLane {
	var lanes []ledgerLane
	var cur *ledgerLane
	always := false
	inJobs := false
	for raw := range strings.SplitSeq(workflow, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		if strings.HasPrefix(raw, "jobs:") {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		if m := skipJobHeader.FindStringSubmatch(raw); m != nil {
			lanes = append(lanes, ledgerLane{job: m[1]})
			cur = &lanes[len(lanes)-1]
			always = false
			continue
		}
		if cur == nil {
			continue
		}
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "- ") {
			always = false // a new step
		}
		if strings.Contains(line, "if: always()") {
			always = true
		}
		if goTestCommand.MatchString(line) {
			cur.goTest = true
		}
		if strings.Contains(line, `echo "PGROUP_GO_TEST_JSON=$RUNNER_TEMP/go-test-json" >> "$GITHUB_ENV"`) {
			cur.records = true
		}
		if strings.Contains(line, "go run ./tools/gotestreport") && strings.Contains(line, "-skips "+skipLedgerPath) && always {
			cur.reads = true
		}
		for _, re := range []*regexp.Regexp{providesAssign, providesFlag} {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				for class := range strings.FieldsSeq(m[1]) {
					if !strings.HasPrefix(class, "$") {
						cur.provides = append(cur.provides, class)
					}
				}
			}
		}
	}
	return lanes
}

// ledgerLaneFaults is what the lane gate refuses.
func ledgerLaneFaults(lanes []ledgerLane, l *skipledger.Ledger) []string {
	var out []string
	for _, lane := range lanes {
		if lane.goTest && (!lane.records || !lane.reads) {
			out = append(out, fmt.Sprintf("job %s runs `go test` and its skips are counted by nobody: record the run (PGROUP_GO_TEST_JSON) and end the job with an `if: always()` step that runs tools/gotestreport -skips %s", lane.job, skipLedgerPath))
		}
		if lane.records != lane.reads {
			out = append(out, fmt.Sprintf("job %s has half of the skip ledger (records: %v, reads: %v): a record nobody reads, or a reader with no record", lane.job, lane.records, lane.reads))
		}
		for _, class := range lane.provides {
			if !l.HasClass(class) {
				out = append(out, fmt.Sprintf("job %s provides %q, a class %s does not name", lane.job, class, l.Path))
			}
		}
	}
	return out
}

func TestFastGoTestLanesReadTheSkipLedger(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	l := readSkipLedger(t, root)
	lanes := ledgerLanes(workflows(t, root)["ci-fast.yml"])
	read := map[string]bool{}
	for _, lane := range lanes {
		if lane.reads {
			read[lane.job] = true
		}
	}
	// the three jobs of the fast lane whose steps reach go test; a reader
	// that finds none of them is reading the wrong text
	for _, job := range []string{"ci-policy", "go-test-touched", "verify-leg"} {
		if !read[job] {
			t.Errorf("ci-fast.yml's %s job does not read the skip ledger over its go test record", job)
		}
	}
	for _, fault := range ledgerLaneFaults(lanes, l) {
		t.Error("ci-fast.yml: " + fault)
	}
}

func TestSkipLedgerLaneGateNegativeControl(t *testing.T) {
	t.Parallel()
	const record = "      - run: |\n          echo \"PGROUP_GO_TEST_JSON=$RUNNER_TEMP/go-test-json\" >> \"$GITHUB_ENV\"\n"
	const reads = "      - if: always()\n        run: |\n          provides=\"slow\"\n          go run ./tools/gotestreport -skips .github/skip-ledger.txt -provides \"$provides tool:elixr\" \"$PGROUP_GO_TEST_JSON\"\n"
	wf := "jobs:\n" +
		"  whole:\n    steps:\n" + record + "      - run: go test ./...\n" + reads +
		"  uncounted:\n    steps:\n      - run: SCHEMA_SLOW=1 go test ./compiler/\n" +
		"  # go test in a comment is not a command\n" +
		"  lint:\n    steps:\n      - run: go vet ./...\n" +
		"  unread:\n    steps:\n" + record + "      - run: make verify-go-fuzz\n" +
		"  notalways:\n    steps:\n" + record + "      - run: go test ./...\n" + strings.Replace(reads, "      - if: always()\n", "      - name: report\n", 1)
	l, err := skipledger.Read(strings.NewReader("skip slow ^SCHEMA_SLOW: \nskip tool:elixir ^no Elixir\n"), skipLedgerPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ledgerLaneFaults(ledgerLanes(wf), l), "\n")
	for _, want := range []string{
		`job whole provides "tool:elixr", a class`,
		"job uncounted runs `go test` and its skips are counted by nobody",
		"job unread has half of the skip ledger (records: true, reads: false)",
		"job notalways runs `go test` and its skips are counted by nobody",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the gate did not refuse %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "job lint") || strings.Contains(got, "job whole runs") {
		t.Errorf("the gate refused a job that is whole, or one that runs no go test:\n%s", got)
	}
}

func TestNoTestReturnsQuietlyWhenItsToolchainIsMissing(t *testing.T) {
	t.Parallel()
	scan, err := repoSkipScan()
	if err != nil {
		t.Fatal(err)
	}
	if scan.files < 1000 {
		t.Fatalf("the scan read %d files that import testing: it is looking at the wrong tree", scan.files)
	}
	for _, d := range scan.disguised {
		t.Errorf("%s:%d: %s %s: go test prints PASS over a body that did not run, and no ledger sees a skip. "+
			"Call t.Skip with a reason the ledger names (slowtest.RequireTool fails instead where SCHEMA_REQUIRE_CORPUS is set), or t.Fatal", d.file, d.line, d.fn, d.what)
	}
}

// The negative control. TestCargo is the reviewer's case, kept as she found
// it in internal/codegen/rust: it probes for cargo and a sibling checkout,
// logs that it is "skipping", and returns PASS.
func TestDisguisedSkipGateNegativeControl(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"p/p_test.go": `package p

import (
	"os"
	"os/exec"
	"testing"
)

func TestCargo(t *testing.T) {
	var serializePath string
	if p := os.Getenv("SERIALIZE_RS"); p != "" {
		serializePath = p
	}
	cargoBin, err := exec.LookPath("cargo")
	if err != nil {
		if fi, err2 := os.Stat("/opt/rustup/bin/cargo"); err2 == nil && !fi.IsDir() {
			cargoBin = "/opt/rustup/bin/cargo"
		}
	}
	if cargoBin == "" || serializePath == "" {
		t.Logf("cargo (%s) or serialize.rs (%s) not found; skipping compilation check", cargoBin, serializePath)
		return
	}
	_ = exec.Command(cargoBin, "check").Run()
}

func TestLookPathReturn(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		return
	}
	_ = exec.Command(cc).Run()
}

func TestOnlyWhenFound(t *testing.T) {
	if node, err := exec.LookPath("node"); err == nil {
		if out, err := exec.Command(node, "x.js").CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
}

func TestEnvReturn(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		if os.Getenv("CORPUS_DIR") == "" {
			return
		}
		t.Log("ran")
	})
}

func TestSkips(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no cc on PATH")
	}
}

func TestFails(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal(err)
		return
	}
	_ = cc
}

func TestOptionalArgument(t *testing.T) {
	args := []string{"--edition", "2024"}
	if cc, err := exec.LookPath("cc"); err == nil {
		args = append(args, "-C", "linker="+cc)
	}
	if err := exec.Command("rustc", args...).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestAlreadyBuilt(t *testing.T) {
	if _, err := os.Stat("build/.stamp"); err == nil {
		return
	}
	t.Log("builds it")
}

func find() (string, bool) {
	p, err := exec.LookPath("cc")
	if err != nil {
		return "", false
	}
	return p, true
}
`,
	})
	scan, err := scanSkipSites(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range scan.disguised {
		got = append(got, fmt.Sprintf("%s:%d", d.fn, d.line))
	}
	want := []string{"TestCargo:20", "TestLookPathReturn:29", "TestOnlyWhenFound:36", "func literal:45"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("disguised skips:\n got %v\nwant %v", got, want)
	}
}
