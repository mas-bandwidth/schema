package extract

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testreg"
)

// fixture is a small repository holding one test of every kind, written to
// a temporary directory: the tests write nothing in the tree.
var fixture = map[string]string{
	"go.mod": "module example.com/fx\n\ngo 1.26\n",
	"pkg/a_test.go": `package pkg

import (
	"os"
	"testing"

	"example.com/fx/slowtest"
)

func TestPlain(t *testing.T) {}

func TestSlow(t *testing.T) { needsCC(t) }

func needsCC(t *testing.T) { slowtest.Gate(t, "cc") }

func TestSkips(t *testing.T) {
	if os.Getenv("NO") != "" {
		t.Skip("no")
	}
}

func TestEnv(t *testing.T) { _ = os.Getenv("SCHEMA_SLOW") }

func FuzzBytes(f *testing.F) {}

func BenchmarkRead(b *testing.B) {}

func Testhelper(t *testing.T) {}

func TestMain(m *testing.M) {}
`,
	"pkg/b_test.go":                 "//go:build linux\n\npackage pkg_test\n\nimport \"testing\"\n\nfunc TestLinux(t *testing.T) {}\n",
	"pkg/testdata/fixture_test.go":  "package fixture\n\nimport \"testing\"\n\nfunc TestNotATest(t *testing.T) {}\n",
	"Makefile":                      "test-all: build\n\tgo test ./...\n\nbuild:\n\tgo build\n\nprep:\n\ttrue\n\nnc-one:\n\ttrue\n\nlib-check: ; true\n",
	"make/negative-controls.json":   `{"groups": [{"name": "g1", "when": "pull-request", "targets": ["nc-one"]}], "excluded": [{"target": "nc-two", "reason": "slow"}]}`,
	".github/workflows/ci.yml":      "on: [pull_request]\njobs:\n  lint:\n    runs-on: x\n    steps:\n      - run: make prep && make -j 4 test-all X=1\n  # a comment\n  build:\n    runs-on: x\n",
	"testdata/corpus/cases.jsonl":   "{\"name\": \"one\"}\n\n{\"id\": \"two\"}\n{\"v\": 3}\n",
	"testdata/corpus/x.in":          "in",
	"testdata/corpus/x.out":         "out",
	"testdata/corpus/README.md":     "doc",
	"test/conformance/zz/driver.py": "print()",
	"test/conformance/zz/src/a.py":  "print()",
	"tla/m/MCSpec.cfg":              "INIT Init",
	"working/x_test.go":             "package x\n\nimport \"testing\"\n\nfunc TestScratch(t *testing.T) {}\n",
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestExtractFindsEveryKind: the fixture's tests, each of its kind, with
// the gates that decide its tier; fixtures under testdata, scratch under
// working/, TestMain and a lower-case Testhelper are not tests.
func TestExtractFindsEveryKind(t *testing.T) {
	t.Parallel()
	res, err := Extract(writeFixture(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unparsed) != 0 {
		t.Fatalf("unparsed: %+v", res.Unparsed)
	}
	var got []string
	for _, it := range res.Items {
		got = append(got, it.ID()+" ["+it.Tier+"] "+strings.Join(it.Gates, "; "))
	}
	want := []string{
		"go-test pkg BenchmarkRead [unit] ",
		"go-test pkg FuzzBytes [unit] ",
		"go-test pkg TestEnv [functional] slow",
		"go-test pkg TestLinux [unit] build: linux",
		"go-test pkg TestPlain [unit] ",
		`go-test pkg TestSkips [unit] skip: pkg/a_test.go:18 if os.Getenv("NO") != ""`,
		"go-test pkg TestSlow [functional] slow",
		"make-target Makefile lib-check [functional] ",
		"make-target Makefile prep [functional] ",
		"make-target Makefile test-all [functional] ",
		"negative-control (excluded) nc-two [nightly] ",
		"negative-control g1 nc-one [functional] ",
		"workflow-job .github/workflows/ci.yml build [functional] ",
		"workflow-job .github/workflows/ci.yml lint [functional] ",
		"data-case pkg/testdata fixture_test [functional] ",
		"data-case testdata/corpus cases/line4 [functional] ",
		"data-case testdata/corpus cases/one [functional] ",
		"data-case testdata/corpus cases/two [functional] ",
		"data-case testdata/corpus x [functional] ",
		"driver test/conformance/zz zz [functional] ",
		"model tla/m MCSpec.cfg [nightly] ",
	}
	if !slices.Equal(got, want) {
		t.Errorf("extract:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestAFileThatDoesNotParseIsNamed: a broken test file and a broken case
// line are unparsed, by name, and Check refuses them.
func TestAFileThatDoesNotParseIsNamed(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"go.mod":               "module example.com/fx\n",
		"pkg/a_test.go":        "package pkg\nfunc TestX(t *testing.T {",
		"testdata/cases.jsonl": "{\"name\": \"ok\"}\n{not json\n",
	}
	res, err := Extract(writeFixture(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range res.Unparsed {
		got = append(got, u.File)
	}
	if !slices.Equal(got, []string{"pkg/a_test.go", "testdata/cases.jsonl"}) {
		t.Fatalf("unparsed %+v", res.Unparsed)
	}
	if p := Check(res, &testreg.Registry{}, nil); len(p) < 2 || !strings.Contains(p[0], "could not be read for tests") {
		t.Fatalf("check: %v", p)
	}
}

func TestExtractRefusesANonRoot(t *testing.T) {
	t.Parallel()
	if _, err := Extract(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not the repository root") {
		t.Fatalf("err %v", err)
	}
}

func gotestRow(key, pkg, test string) testreg.Test {
	return testreg.Test{Key: key, Adapter: "gotest", Inputs: []byte(`{"package":"` + pkg + `","test":"` + test + `"}`)}
}

// TestCheckHoldsTheRepositoryTheRegistryAndTheLedgerExactly: each
// disagreement is named, and the exact account passes.
func TestCheckHoldsTheRepositoryTheRegistryAndTheLedgerExactly(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindGoTest, Dir: "a", Name: "TestRow", Source: "a/x_test.go:1"},
		{Kind: KindGoTest, Dir: "a", Name: "TestOne", Source: "a/x_test.go:2"},
		{Kind: KindGoTest, Dir: "a", Name: "TestTwo", Source: "a/x_test.go:3"},
		{Kind: KindDataCase, Dir: "d", Name: "c", Source: "d/c:1"},
	}}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow")}}
	exact := []LedgerLine{{Kind: KindGoTest, Dir: "a", Count: 2}, {Kind: KindDataCase, Dir: "d", Count: 1}}
	if p := Check(res, reg, exact); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	for name, c := range map[string]struct {
		reg    *testreg.Registry
		ledger []LedgerLine
		says   string
	}{
		"a new test":   {reg, []LedgerLine{{Kind: KindGoTest, Dir: "a", Count: 1}, exact[1]}, "there are 2: give each new test a registry row (TestOne"},
		"a moved test": {reg, []LedgerLine{{Kind: KindGoTest, Dir: "a", Count: 3}, exact[1]}, "there are 2: lower it to 2"},
		"a stale line": {reg, append(slices.Clone(exact), LedgerLine{Kind: KindModel, Dir: "tla", Count: 1}), "model tla counts 1 unmoved tests, and there are none"},
		"no line":      {reg, exact[:1], "data-case d: 1 tests are neither registry rows nor in"},
		"a row for nothing": {&testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow"), gotestRow("tooling.a.gone", "./a", "TestGone")}},
			exact, "registry row tooling.a.gone runs go-test a TestGone, which the repository does not hold"},
		"two rows for one test": {&testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow"), gotestRow("tooling.a.again", "./a", "TestRow")}},
			exact, "tooling.a.again runs ./a TestRow, which tooling.a.row already runs"},
		"a gotest row with no test": {&testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow"), {Key: "tooling.a.bad", Adapter: "gotest", Inputs: []byte(`{}`)}}},
			exact, "tooling.a.bad is a gotest row without a package and a test"},
	} {
		p := Check(res, c.reg, c.ledger)
		if !strings.Contains(strings.Join(p, "\n"), c.says) {
			t.Errorf("%s: want %q in:\n%s", name, c.says, strings.Join(p, "\n"))
		}
	}
}

// TestTheLedgerOnlyShrinks: against main's ledger, a lower count and a
// removed line pass; a higher count and a new place fail by name.
func TestTheLedgerOnlyShrinks(t *testing.T) {
	t.Parallel()
	main := []LedgerLine{{Kind: KindGoTest, Dir: "a", Count: 3}, {Kind: KindGoTest, Dir: "b", Count: 1}}
	if p := Shrinks(main, []LedgerLine{{Kind: KindGoTest, Dir: "a", Count: 2}}); len(p) != 0 {
		t.Fatalf("a shrinking ledger: %v", p)
	}
	p := Shrinks(main, []LedgerLine{{Kind: KindGoTest, Dir: "a", Count: 4}, {Kind: KindGoTest, Dir: "c", Count: 1}})
	if len(p) != 2 || !strings.Contains(p[0], "grew from 3 to 4") || !strings.Contains(p[1], "go-test c is a new ledger line") {
		t.Fatalf("a growing ledger: %v", p)
	}
}

// skipRedResult is a repository with one Go test holding two Skip sites, one
// holding none, and a data case.
func skipRedResult() *Result {
	return &Result{Items: []Item{
		{Kind: KindGoTest, Dir: "a", Name: "TestHides", Source: "a/x_test.go:1", Gates: []string{"slow", "skip: a/x_test.go:3 if x", "skip: a/x_test.go:9 "}},
		{Kind: KindGoTest, Dir: "a", Name: "TestClean", Source: "a/x_test.go:20"},
	}}
}

// TestSkipRedLinesAreHeldToTheirSkips: a skip-red line counts its test's
// Skip sites exactly; one naming no test, a test with no skip, or the wrong
// count is named; the exact line passes and is not an unmoved count.
func TestSkipRedLinesAreHeldToTheirSkips(t *testing.T) {
	t.Parallel()
	res := skipRedResult()
	reg := &testreg.Registry{Path: "r.jsonl"}
	unmoved := LedgerLine{Kind: KindGoTest, Dir: "a", Count: 2}
	red := func(place string, n int) LedgerLine {
		return LedgerLine{Kind: KindSkipRed, Dir: place, Count: n, Layer: "L5", Reason: "measured red", Line: 9}
	}
	if p := Check(res, reg, []LedgerLine{unmoved, red("a:TestHides", 2)}); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	for name, c := range map[string]struct {
		line LedgerLine
		says string
	}{
		"no such test":     {red("a:TestGone", 1), "skip-red a:TestGone names a Go test the repository does not hold"},
		"the skip is gone": {red("a:TestClean", 1), "skip-red a:TestClean counts 1 skips, and the test has none: its skip is gone, so delete the line"},
		"a wrong count":    {red("a:TestHides", 1), "skip-red a:TestHides counts 1 skips, and the test has 2: set it to 2"},
	} {
		p := Check(res, reg, []LedgerLine{unmoved, c.line})
		if len(p) != 1 || !strings.Contains(p[0], c.says) {
			t.Errorf("%s: want exactly %q, have:\n%s", name, c.says, strings.Join(p, "\n"))
		}
	}
}

// TestASkipRedLineStaysWhileItsSkipDoes: against main's ledger a skip-red
// line may leave only when its test's skips are gone, or its test is gone and
// no test reaches its skips; it may not be added or grow. A renamed test
// takes its line along, as a carry, never as a larger count.
func TestASkipRedLineStaysWhileItsSkipDoes(t *testing.T) {
	t.Parallel()
	res := skipRedResult()
	line := LedgerLine{Kind: KindSkipRed, Dir: "a:TestHides", Count: 2, Layer: "L5", Reason: "measured red"}
	main := []LedgerLine{line}
	if p := KeepsSkipReds(main, main, res, res); len(p) != 0 {
		t.Fatalf("a kept line: %v", p)
	}
	p := KeepsSkipReds(main, nil, res, res)
	if len(p) != 1 || !strings.Contains(p[0], "skip-red a:TestHides left the ledger, and the test still has 2 skips") {
		t.Fatalf("a line dropped while its skip stays: %v", p)
	}
	fixed := &Result{Items: []Item{{Kind: KindGoTest, Dir: "a", Name: "TestHides"}}}
	if p := KeepsSkipReds(main, nil, res, fixed); len(p) != 0 {
		t.Fatalf("a line dropped with its skip: %v", p)
	}
	if p := KeepsSkipReds(main, nil, res, &Result{}); len(p) != 0 {
		t.Fatalf("a line dropped with its test and its skips: %v", p)
	}
	p = KeepsSkipReds(main, nil, nil, &Result{})
	if len(p) != 1 || !strings.Contains(p[0], "skip-red a:TestHides left the ledger with its test, and without main's tree (--since)") {
		t.Fatalf("a line dropped with its test, no main tree: %v", p)
	}
	grown := line
	grown.Count = 3
	p = KeepsSkipReds(main, []LedgerLine{grown, {Kind: KindSkipRed, Dir: "a:TestNew", Count: 1}}, res, res)
	if len(p) != 2 || !strings.Contains(p[0], "skip-red a:TestHides grew from 2 to 3 skips") || !strings.Contains(p[1], "skip-red a:TestNew is a new ledger line") {
		t.Fatalf("a growing skip-red account: %v", p)
	}
	if p := Shrinks(main, []LedgerLine{grown}); len(p) != 0 {
		t.Fatalf("Shrinks leaves skip-red lines to KeepsSkipReds: %v", p)
	}
	// TestHides renamed to TestHidesStill, its skips moved down two lines
	renamed := &Result{Items: []Item{
		{Kind: KindGoTest, Dir: "a", Name: "TestHidesStill", Source: "a/x_test.go:1", Gates: []string{"slow", "skip: a/x_test.go:5 if x", "skip: a/x_test.go:11 "}},
		{Kind: KindGoTest, Dir: "a", Name: "TestClean", Source: "a/x_test.go:20"},
	}}
	p = KeepsSkipReds(main, nil, res, renamed)
	if len(p) != 1 || !strings.Contains(p[0], "skip-red a:TestHides left the ledger with its test, and its skip is still reached by a:TestHidesStill (at a/x_test.go)") {
		t.Fatalf("a renamed test that left its line behind: %v", p)
	}
	carried := LedgerLine{Kind: KindSkipRed, Dir: "a:TestHidesStill", Count: 2, Layer: "L5", Reason: "measured red"}
	if p := KeepsSkipReds(main, []LedgerLine{carried}, res, renamed); len(p) != 0 {
		t.Fatalf("a line carried with its renamed test: %v", p)
	}
	carried.Count = 3
	p = KeepsSkipReds(main, []LedgerLine{carried}, res, renamed)
	if len(p) != 1 || !strings.Contains(p[0], "skip-red a:TestHidesStill is a new ledger line") {
		t.Fatalf("a carry that grew: %v", p)
	}
}

// TestANewTestWithoutARowIsNamed: against main's tree, a test main lacks and
// no row runs is named with its place; one with a row, and one main has, are
// not.
func TestANewTestWithoutARowIsNamed(t *testing.T) {
	t.Parallel()
	before := &Result{Items: []Item{{Kind: KindGoTest, Dir: "a", Name: "TestOld", Source: "a/x_test.go:1"}}}
	now := &Result{Items: []Item{
		{Kind: KindGoTest, Dir: "a", Name: "TestOld", Source: "a/x_test.go:1"},
		{Kind: KindGoTest, Dir: "a", Name: "TestRow", Source: "a/x_test.go:5"},
		{Kind: KindGoTest, Dir: "a", Name: "TestPlanted", Source: "a/y_test.go:3"},
	}}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow")}}
	p := NewWithoutRows(before, now, reg)
	if len(p) != 1 || !strings.Contains(p[0], "go-test a TestPlanted at a/y_test.go:3 is a new test with no registry row") {
		t.Fatalf("want exactly TestPlanted named: %v", p)
	}
}

// TestReadLedgerRefusesABadLine: every malformed line is named by line.
func TestReadLedgerRefusesABadLine(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ text, says string }{
		{"go-test\ta\t1\tL3\n", "want 5 tab-separated fields"},
		{"go-test\ta\t0\tL3\twhy\n", `count "0" is not a positive number`},
		{"go-test\ta\tx\tL3\twhy\n", `count "x" is not a positive number`},
		{"gotest\ta\t1\tL3\twhy\n", `unknown kind "gotest"`},
		{"go-test\ta\t1\t\twhy\n", "names the layer that moves it and why"},
		{"go-test\ta\t1\tL3\twhy\n# c\ngo-test\ta\t2\tL3\twhy\n", "u.tsv:3: go-test a is already counted at line 1"},
	} {
		if _, err := ReadLedger(strings.NewReader(c.text), "u.tsv"); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%q: err %v, want %q", c.text, err, c.says)
		}
	}
	got, err := ReadLedger(strings.NewReader("# header\ngo-test\ta\t2\tL3\twhy\nskip-red\ta:TestX\t1\tL5\tred\n"), "u.tsv")
	if err != nil || len(got) != 2 || got[0].Count != 2 || got[0].Line != 2 || got[1].Kind != KindSkipRed || got[1].Dir != "a:TestX" {
		t.Fatalf("a good ledger: %+v %v", got, err)
	}
}

// TestLedgerCountsWhatIsUnmoved: the written ledger reads back as exactly
// the account Check holds.
func TestLedgerCountsWhatIsUnmoved(t *testing.T) {
	t.Parallel()
	res, err := Extract(writeFixture(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	reg := &testreg.Registry{Tests: []testreg.Test{gotestRow("tooling.pkg.plain", "./pkg", "TestPlain")}}
	lines := Ledger(res, reg, func(kind, dir string) (string, string) { return "L9", "fixture" })
	var b strings.Builder
	if err := WriteLedger(&b, "# h\n", lines); err != nil {
		t.Fatal(err)
	}
	back, err := ReadLedger(strings.NewReader(b.String()), "u.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if p := Check(res, reg, back); len(p) != 0 {
		t.Fatalf("the written ledger disagrees: %v", p)
	}
	if back[0].Kind != KindGoTest || back[0].Dir != "pkg" || back[0].Count != 6 {
		t.Fatalf("first line %+v, want go-test pkg 6 (seven tests, one a row)", back[0])
	}
}

// TestEveryTestIsARowOrInTheLedger is the repository's own account: every
// test it holds is a registry row or counted in test/registry/unmoved.tsv,
// exactly, and every row that claims a Go test names one that exists.
func TestEveryTestIsARowOrInTheLedger(t *testing.T) {
	t.Parallel()
	root := "../../.."
	res, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := testreg.Load(filepath.Join(root, testreg.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadLedger(filepath.Join(root, LedgerPath))
	if err != nil {
		t.Fatal(err)
	}
	if p := Check(res, reg, ledger); len(p) != 0 {
		t.Errorf("%d disagreements (go run ./tools/schema-test extract):\n  %s", len(p), strings.Join(p, "\n  "))
	}
	t.Logf("%d tests, %d ledger lines", len(res.Items), len(ledger))
}

// TestARowIndexesTheCorpusItReads: a per-language row's inputs name the
// committed files it reads and the driver each language answers it with, and
// those data cases and drivers count as the registry's: a file by its
// directory and stem (x.in and x.out are the case x), a driver for every
// language the registry lists. A language-free row's inputs index nothing,
// and a data case no row reads is still unmoved.
func TestARowIndexesTheCorpusItReads(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindDataCase, Dir: "testdata/corpus", Name: "x", Source: "testdata/corpus/x.in:1"},
		{Kind: KindDataCase, Dir: "testdata/corpus", Name: "y", Source: "testdata/corpus/y.in:1"},
		{Kind: KindDriver, Dir: "test/conformance/zz", Name: "zz", Source: "test/conformance/zz/driver:1"},
		{Kind: KindDriver, Dir: "test/conformance/qq", Name: "qq", Source: "test/conformance/qq/driver:1"},
	}}
	row := testreg.Test{Key: "roundtrip.conformance-wire.x", Inputs: []byte(`{"surface":"wire","case":"x","files":["testdata/corpus/x.in","testdata/corpus/x.out"],"driver":"test/conformance/{lang}/driver"}`)}
	free := testreg.Test{Key: "tooling.t.y", Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(`{"package":"./t","test":"TestY","files":["testdata/corpus/y.in"]}`)}
	reg := &testreg.Registry{Path: "r.jsonl", Langs: []testreg.Lang{{Name: "zz"}, {Name: "ww"}}, Tests: []testreg.Test{row}}
	// a driver is claimed for every language listed; ww has none on disk,
	// so its claim matches nothing
	got := Indexed(reg)
	if len(got) != 3 || got[res.Items[0].ID()] != row.Key || got[res.Items[2].ID()] != row.Key || got[res.Items[3].ID()] != "" {
		t.Fatalf("indexed %v, want the case x and the zz and ww drivers", got)
	}
	ledger := []LedgerLine{{Kind: KindDataCase, Dir: "testdata/corpus", Count: 1}, {Kind: KindDriver, Dir: "test/conformance/qq", Count: 1}}
	if p := Check(res, reg, ledger); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	// the row gone, x and the zz driver are unmoved again: red until counted
	if p := Check(res, &testreg.Registry{Path: "r.jsonl", Langs: reg.Langs}, ledger); !strings.Contains(strings.Join(p, "\n"), "there are 2: give each new test a registry row") {
		t.Fatalf("a dropped row: %v", p)
	}
	reg.Tests = append(reg.Tests, free)
	if got := Indexed(reg); len(got) != 3 {
		t.Fatalf("a language-free row indexed files: %v", got)
	}
}

// TestWhatGitIgnoresIsNotATest: in a git checkout, files git ignores (a
// build's output, a lock file it writes) are not tests; without the ignore
// they are. A .git that git cannot read is an error, never an empty set.
func TestWhatGitIgnoresIsNotATest(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"go.mod":               "module example.com/fx\n\ngo 1.26\n",
		"pkg/a_test.go":        "package pkg\n\nimport \"testing\"\n\nfunc TestKept(t *testing.T) {}\n",
		"out/x_test.go":        "package x\n\nimport \"testing\"\n\nfunc TestBuilt(t *testing.T) {}\n",
		"test/rust/Cargo.lock": "lock",
		"test/rust/case.in":    "in",
		".gitignore":           "/out/\nCargo.lock\n",
	}
	names := func(root string) string {
		t.Helper()
		res, err := Extract(root)
		if err != nil {
			t.Fatal(err)
		}
		var have []string
		for _, it := range res.Items {
			have = append(have, it.Kind+" "+it.Dir+" "+it.Name)
		}
		sort.Strings(have)
		return strings.Join(have, "\n")
	}
	plain := names(writeFixture(t, files))
	if !strings.Contains(plain, "TestBuilt") || !strings.Contains(plain, "Cargo") {
		t.Fatalf("without git, every file is read:\n%s", plain)
	}
	root := writeFixture(t, files)
	git := exec.Command("git", "init", "--quiet", root)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// a person's own excludes are not the repository's: they change no count
	if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("pkg/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	have := names(root)
	if strings.Contains(have, "TestBuilt") || strings.Contains(have, "Cargo") || !strings.Contains(have, "go-test pkg TestKept") || !strings.Contains(have, "data-case test/rust case") {
		t.Fatalf("in a checkout, what git ignores is not a test:\n%s", have)
	}
	broken := writeFixture(t, files)
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("gitdir: /nonexistent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(broken); err == nil || !strings.Contains(err.Error(), "to leave out what git ignores") {
		t.Fatalf("a .git that git cannot read: %v", err)
	}
}

// TestACarryIsARenameAndNothingElse: a skip-red line moves only to a new
// test, only from a test that is gone, only once, and a line of the same
// count first; tests that share a helper's skip do not hold each other's
// lines.
func TestACarryIsARenameAndNothingElse(t *testing.T) {
	t.Parallel()
	test := func(name string, sites ...string) Item {
		it := Item{Kind: KindGoTest, Dir: "a", Name: name, Source: "a/x_test.go:1"}
		for _, s := range sites {
			it.Gates = append(it.Gates, "skip: a/x_test.go:"+s)
		}
		return it
	}
	line := func(name string, n int) LedgerLine {
		return LedgerLine{Kind: KindSkipRed, Dir: "a:" + name, Count: n, Layer: "L5", Reason: "measured red"}
	}
	tree := func(its ...Item) *Result { return &Result{Items: its} }
	// two tests share a helper's skip; one is deleted with its line
	main := tree(test("TestA", "9 if x"), test("TestB", "9 if x"))
	if p := KeepsSkipReds([]LedgerLine{line("TestA", 1), line("TestB", 1)}, []LedgerLine{line("TestB", 1)}, main, tree(test("TestB", "9 if x"))); len(p) != 0 {
		t.Fatalf("a deleted test whose helper another lined test shares: %v", p)
	}
	// a test that still exists is not a carry source
	p := KeepsSkipReds([]LedgerLine{line("TestA", 1)}, []LedgerLine{line("TestC", 1)}, tree(test("TestA", "9 if x")), tree(test("TestA", "9 if x"), test("TestC", "9 if x")))
	if len(p) != 2 || !strings.Contains(p[0], "skip-red a:TestC is a new ledger line") || !strings.Contains(p[1], "skip-red a:TestA left the ledger, and the test still has 1 skips") {
		t.Fatalf("a carry off a test that still skips: %v", p)
	}
	// a line that stays is not a carry source
	p = KeepsSkipReds([]LedgerLine{line("TestA", 1)}, []LedgerLine{line("TestA", 1), line("TestC", 1)}, tree(test("TestA", "9 if x")), tree(test("TestC", "9 if x")))
	if len(p) != 1 || !strings.Contains(p[0], "skip-red a:TestC is a new ledger line") {
		t.Fatalf("a carry off a line that stays: %v", p)
	}
	// one old line is carried once
	p = KeepsSkipReds([]LedgerLine{line("TestA", 1)}, []LedgerLine{line("TestC", 1), line("TestD", 1)}, tree(test("TestA", "9 if x")), tree(test("TestC", "9 if x"), test("TestD", "9 if x")))
	if len(p) != 1 || !strings.Contains(p[0], "skip-red a:TestD is a new ledger line") {
		t.Fatalf("one line carried twice: %v", p)
	}
	// a carry goes to a new test only
	p = KeepsSkipReds([]LedgerLine{line("TestA", 1)}, []LedgerLine{line("TestB", 1)}, main, tree(test("TestB", "9 if x")))
	if len(p) != 1 || !strings.Contains(p[0], "skip-red a:TestB is a new ledger line") {
		t.Fatalf("a carry onto a test main holds: %v", p)
	}
	// two renamed tests sharing a skip: each line goes to its own count
	both := tree(test("TestA", "9 if x", "12 if y"), test("TestB", "9 if x"))
	renamed := tree(test("TestA2", "9 if x", "12 if y"), test("TestB2", "9 if x"))
	if p := KeepsSkipReds([]LedgerLine{line("TestA", 2), line("TestB", 1)}, []LedgerLine{line("TestB2", 1), line("TestA2", 2)}, both, renamed); len(p) != 0 {
		t.Fatalf("two renames sharing a skip: %v", p)
	}
}
