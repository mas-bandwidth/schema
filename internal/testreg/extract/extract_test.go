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
		"go-test pkg TestEnv [unit] ",
		"go-test pkg TestLinux [unit] build: linux",
		"go-test pkg TestPlain [unit] ",
		`go-test pkg TestSkips [unit] skip: pkg/a_test.go:18 if os.Getenv("NO") != ""`,
		"go-test pkg TestSlow [unit] ",
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

func modelRow(key, inputs string) testreg.Test {
	return testreg.Test{Key: key, Adapter: "model-check", Inputs: []byte(inputs)}
}

// TestAModelCheckRowCoversItsConfig: a model-check row covers the TLC
// configuration its inputs name, so the model leaves the ledger; a row
// naming a configuration tla/ does not hold, two rows for one, and inputs
// that do not read are each named. The controls row names no config and
// covers nothing.
func TestAModelCheckRowCoversItsConfig(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindModel, Dir: "tla", Name: "MCA.cfg", Source: "tla/MCA.cfg:1"},
		{Kind: KindModel, Dir: "tla", Name: "MCB.cfg", Source: "tla/MCB.cfg:1"},
	}}
	rows := []testreg.Test{modelRow("model.a.holds", `{"config":"MCA.cfg"}`), modelRow("plant.a.controls", `{"controls":true}`)}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: rows}
	exact := []LedgerLine{{Kind: KindModel, Dir: "tla", Count: 1}}
	if p := Check(res, reg, exact); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	for name, c := range map[string]struct {
		rows []testreg.Test
		says string
	}{
		"a row for a config tla/ does not hold": {append(slices.Clone(rows), modelRow("model.a.gone", `{"config":"MCGone.cfg"}`)), "registry row model.a.gone runs model tla MCGone.cfg, which the repository does not hold"},
		"two rows for one config":               {append(slices.Clone(rows), modelRow("model.a.again", `{"config":"MCA.cfg"}`)), "model.a.again runs tla/MCA.cfg, which model.a.holds already runs"},
		"inputs that do not read":               {append(slices.Clone(rows), modelRow("model.a.bad", `{"config":7}`)), "model.a.bad is a model-check row whose inputs do not read"},
		"the covered config left in the ledger": {rows[1:], "model tla counts 1 unmoved tests, and there are 2"},
	} {
		p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: c.rows}, exact)
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

// tierResult is a repository with one Go test that skips, one that does not,
// and one a registry row runs.
func tierResult() *Result {
	return &Result{Items: []Item{
		{Kind: KindGoTest, Dir: "a", Name: "TestNeeds", Source: "a/x_test.go:1"},
		{Kind: KindGoTest, Dir: "a", Name: "TestRow", Source: "a/x_test.go:10"},
		{Kind: KindGoTest, Dir: "a", Name: "TestClean", Source: "a/x_test.go:20"},
	}}
}

// TestTierLinesAreHeldToTheirTests: a tier line names a Go test that exists
// and has no row, and sets its tier; one naming no test, or a test a row
// runs, is named, and a tier line is never an unmoved count.
func TestTierLinesAreHeldToTheirTests(t *testing.T) {
	t.Parallel()
	res := tierResult()
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow")}}
	unmoved := LedgerLine{Kind: KindGoTest, Dir: "a", Count: 2}
	tier := func(place string) LedgerLine {
		return LedgerLine{Kind: KindTier, Dir: place, Count: 1, Layer: "L5", Reason: "functional: needs cc", Line: 9}
	}
	if p := Check(res, reg, []LedgerLine{unmoved, tier("a:TestNeeds")}); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	ApplyTiers(res, []LedgerLine{tier("a:TestNeeds"), {Kind: KindTier, Dir: "a:TestClean", Count: 1, Reason: "nightly: a long run"}})
	if res.Items[0].Tier != "functional" || res.Items[1].Tier != "" || res.Items[2].Tier != "nightly" {
		t.Fatalf("ApplyTiers: %+v", res.Items)
	}
	for name, c := range map[string]struct {
		line LedgerLine
		says string
	}{
		"no such test":   {tier("a:TestGone"), "tier a:TestGone names a Go test the repository does not hold"},
		"a test the row": {tier("a:TestRow"), "tier a:TestRow names a test registry row tooling.a.row runs: the row carries its tier, so delete the line"},
	} {
		p := Check(tierResult(), reg, []LedgerLine{unmoved, c.line})
		if len(p) != 1 || !strings.Contains(p[0], c.says) {
			t.Errorf("%s: want exactly %q, have:\n%s", name, c.says, strings.Join(p, "\n"))
		}
	}
}

// TestATestThatSkipsIsRefused: Check names every Skip a Go test reaches,
// whatever the ledger says; there is no line that permits one.
func TestATestThatSkipsIsRefused(t *testing.T) {
	t.Parallel()
	res := tierResult()
	res.Items[2].Gates = []string{"skip: a/x_test.go:22 if os.Getenv(\"X\") == \"\""}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow")}}
	p := Check(res, reg, []LedgerLine{{Kind: KindGoTest, Dir: "a", Count: 2}})
	if len(p) != 1 || !strings.Contains(p[0], `a TestClean reaches a skip at a/x_test.go:22 if os.Getenv("X") == "": no test skips`) {
		t.Fatalf("want exactly TestClean's skip named: %v", p)
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
	got, err := ReadLedger(strings.NewReader("# header\ngo-test\ta\t2\tL3\twhy\ntier\ta:TestX\t1\tL5\tnightly: long\n"), "u.tsv")
	if err != nil || len(got) != 2 || got[0].Count != 2 || got[0].Line != 2 || got[1].Kind != KindTier || got[1].Dir != "a:TestX" {
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
	// the one disagreement left is the fixture's skip, which no ledger line
	// can permit
	if p := Check(res, reg, back); len(p) != 1 || !strings.Contains(p[0], "pkg TestSkips reaches a skip") {
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
// language that answers the row. A language-free row's inputs index nothing,
// and a data case no row reads is still unmoved. Only what runs counts: the
// driver of a language with no adapter is unmoved, and a row no ready
// language answers (none has an adapter, or each owes it) indexes nothing.
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
	reg := &testreg.Registry{Path: "r.jsonl", Langs: []testreg.Lang{{Name: "zz", Adapter: "corpus"}, {Name: "ww", Adapter: "corpus"}, {Name: "qq"}}, Tests: []testreg.Test{row}}
	// a driver is claimed for every ready language; ww has none on disk, so
	// its claim matches nothing, and qq has no adapter, so its driver is not
	// claimed (planted: claim it, and the ledger's qq line is red)
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
	// no ready language answers it: it indexes nothing
	idle := &testreg.Registry{Path: "r.jsonl", Langs: []testreg.Lang{{Name: "zz"}, {Name: "qq"}}, Tests: []testreg.Test{row}}
	if got := Indexed(idle); len(got) != 0 {
		t.Fatalf("a row no adapter answers indexed %v", got)
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

// TestADriverOnlyJobIsNotATest: a workflow job whose steps only check out,
// move artifacts and call the driver runs no test of its own; a job that
// also runs a make target (on its own line or after a shell operator),
// uses another action, runs under another shell, or runs nothing at all is
// still a test.
func TestADriverOnlyJobIsNotATest(t *testing.T) {
	t.Parallel()
	wf := "on: [schedule]\njobs:\n" +
		"  driver:\n    runs-on: x\n    defaults:\n      run:\n        shell: go run ./tools/pgroup -- bash -e {0}\n    steps:\n      - uses: actions/checkout@abc # v7\n      - run: |\n          set -euo pipefail\n          go run ./tools/fullrun launch --part 1/2\n      - uses: actions/upload-artifact@def\n        with:\n          path: |\n            out/REDS.tsv\n      - run: go run ./tools/schema-test run --group nightly\n" +
		"  mixed:\n    runs-on: x\n    steps:\n      - run: |\n          go run ./tools/fullrun merge\n          make test-all\n" +
		"  action:\n    runs-on: x\n    steps:\n      - uses: some/checker@abc\n      - run: go run ./tools/fullrun merge\n" +
		"  shell:\n    runs-on: x\n    defaults:\n      run:\n        shell: python {0}\n    steps:\n      - run: go run ./tools/fullrun merge\n" +
		"  chained:\n    runs-on: x\n    steps:\n      - run: go run ./tools/schema-test run --group x && make verify-all\n" +
		"  continued:\n    runs-on: x\n    steps:\n      - run: go run ./tools/schema-test run --group x\n          && make test-all\n      - uses: actions/checkout@abc\n" +
		"  flow:\n    runs-on: x\n    steps:\n      - run: go run ./tools/fullrun merge\n      - {run: make test-all}\n" +
		"  bare:\n    runs-on: x\n    steps:\n      - run: go run ./tools/fullrun merge\n      - run:\n          make test-all\n" +
		"  spaced:\n    runs-on: x\n    steps:\n      - run: go run ./tools/fullrun merge\n      -   run: make test-all\n" +
		"  setchain:\n    runs-on: x\n    steps:\n      - run: |\n          set -e; make test-all\n          go run ./tools/fullrun merge\n" +
		"  alias:\n    runs-on: x\n    steps:\n      - run: go run ./tools/fullrun merge\n      - *tests\n" +
		"  empty:\n    runs-on: x\n"
	res, err := Extract(writeFixture(t, map[string]string{"go.mod": "module example.com/fx\n", ".github/workflows/n.yml": wf}))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range res.Items {
		got = append(got, it.Name)
	}
	sort.Strings(got)
	if want := []string{"action", "alias", "bare", "chained", "continued", "empty", "flow", "mixed", "setchain", "shell", "spaced"}; !slices.Equal(got, want) {
		t.Errorf("jobs counted as tests: %v, want %v", got, want)
	}
}
