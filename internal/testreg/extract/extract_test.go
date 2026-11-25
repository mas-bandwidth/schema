package extract

import (
	"maps"
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
	"pkg/b_test.go":                               "//go:build linux\n\npackage pkg_test\n\nimport \"testing\"\n\nfunc TestLinux(t *testing.T) {}\n",
	"pkg/testdata/fixture_test.go":                "package fixture\n\nimport \"testing\"\n\nfunc TestNotATest(t *testing.T) {}\n",
	"Makefile":                                    "test-all: build\n\tgo test ./...\n\nbuild:\n\tgo build\n\nprep:\n\ttrue\n\nquoted:\n\ttrue\n\nnc-one:\n\ttrue\n\nrow-only:\n\ttrue\n\nlib-check: ; true\n",
	"test/registry/registry.jsonl":                `{"rec":"test","key":"harness-run.makefile.row-only","adapter":"make","inputs":{"file":"Makefile","target":"row-only"}}` + "\n",
	"make/negative-controls.json":                 `{"groups": [{"name": "g1", "when": "pull-request", "targets": ["nc-one"]}], "excluded": [{"target": "nc-two", "reason": "slow"}]}`,
	".github/workflows/ci.yml":                    "on: [pull_request]\njobs:\n  lint:\n    runs-on: x\n    steps:\n      - run: make prep && make -j 4 test-all X=1\n      - run: make -j\"$(nproc)\" quoted\n  # a comment\n  build:\n    runs-on: x\n  rows:\n    runs-on: x\n    steps:\n      - run: go run ./tools/schema-test run --group pr --shard 1/2\n  plan:\n    runs-on: x\n    steps:\n      - run: go run ./tools/schema-test langs --ready\n      - run: add_reader ./tools/schema-test ;;\n",
	"testdata/corpus/cases.jsonl":                 "{\"name\": \"one\"}\n\n{\"id\": \"two\"}\n{\"v\": 3}\n",
	"testdata/corpus/x.in":                        "in",
	"testdata/corpus/x.out":                       "out",
	"testdata/corpus/README.md":                   "doc",
	"test/conformance/zz/driver.py":               "print()",
	"test/conformance/zz/src/a.py":                "print()",
	"tla/m/MCSpec.cfg":                            "INIT Init",
	"test/testguard/rest/pkg/TestPlain":           "",
	"test/testguard/rest.tsv":                     "pkg\tTestPlain\n",
	"test/conformance/zz/Directory.Build.targets": "<Project/>",
	"working/x_test.go":                           "package x\n\nimport \"testing\"\n\nfunc TestScratch(t *testing.T) {}\n",
	"test/u/obj/Debug/gen.cs":                     "// a dotnet build output file, never a test\n",
	"test/u/bin/Debug/x/u.deps.json":              "{}\n",
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
// working/, build output under obj/, TestMain and a lower-case Testhelper
// are not tests, nor is a testguard record of a Go test (its shard's .tsv
// is a case), the MSBuild configuration a driver directory imports, or a
// workflow job that runs the driver (its tests are the rows it runs). A
// rule only a registry make row runs (row-only) is run by name all the same.
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
		"make-target Makefile quoted [functional] ",
		"make-target Makefile row-only [functional] ",
		"make-target Makefile test-all [functional] ",
		"negative-control (excluded) nc-two [nightly] ",
		"negative-control g1 nc-one [functional] ",
		"workflow-job .github/workflows/ci.yml build [functional] ",
		"workflow-job .github/workflows/ci.yml lint [functional] ",
		"workflow-job .github/workflows/ci.yml plan [functional] ",
		"data-case pkg/testdata fixture_test [functional] ",
		"data-case test/testguard rest [functional] ",
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

// TestASubtestRowClaimsItsSubtest: the parent gotest row claims the top-level
// test and a row that names a subtest claims that subtest, so the two are not
// "already runs". The extract lists the top-level test, which holds the
// subtest claim. A second row for the same subtest is refused, and a subtest
// whose top-level test the repository does not hold is named. The subtest
// row's files index the data case; the parent, naming none, does not.
func TestASubtestRowClaimsItsSubtest(t *testing.T) {
	t.Parallel()
	top := Item{Kind: KindGoTest, Dir: "internal/baseline", Name: "TestCommittedEditPairs", Source: "internal/baseline/editpairs_test.go:19"}
	res := &Result{Items: []Item{
		top,
		{Kind: KindDataCase, Dir: "internal/baseline/testdata/editpairs/f058-fixed-to-bounded", Name: "want", Source: "internal/baseline/testdata/editpairs/f058-fixed-to-bounded/want.txt:1"},
	}}
	parent := gotestRow("tooling.baseline.committed-edit-pairs", "./internal/baseline", "TestCommittedEditPairs")
	child := testreg.Test{Key: "tooling.baseline.edit-pair-f058-fixed-to-bounded", Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(`{"package":"./internal/baseline","test":"TestCommittedEditPairs","subtest":"f058-fixed-to-bounded","files":["internal/baseline/testdata/editpairs/f058-fixed-to-bounded/want.txt"]}`)}
	sib := testreg.Test{Key: "tooling.baseline.edit-pair-f058-control-bounded-to-map", Scope: testreg.LangFree, Adapter: "gotest", Inputs: []byte(`{"package":"./internal/baseline","test":"TestCommittedEditPairs","subtest":"f058-control-bounded-to-map"}`)}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{parent, child, sib}}
	out, problems := Covered(reg)
	if len(problems) != 0 {
		t.Fatalf("parent and its subtests: %v", problems)
	}
	if out[top.ID()] != parent.Key {
		t.Fatalf("parent claim %q, want %s", out[top.ID()], parent.Key)
	}
	sub := Item{Kind: KindGoTest, Dir: "internal/baseline", Name: "TestCommittedEditPairs/f058-fixed-to-bounded"}
	if out[sub.ID()] != child.Key {
		t.Fatalf("subtest claim %q, want %s", out[sub.ID()], child.Key)
	}
	if p := Check(res, reg, nil); len(p) != 0 {
		t.Fatalf("the top-level test holds its subtests: %v", p)
	}
	if got := Indexed(reg)[Item{Kind: KindDataCase, Dir: "internal/baseline/testdata/editpairs/f058-fixed-to-bounded", Name: "want"}.ID()]; got != child.Key {
		t.Fatalf("the case file is indexed by %q, want the subtest row", got)
	}
	dup := testreg.Test{Key: "tooling.baseline.edit-pair-again", Adapter: "gotest", Inputs: child.Inputs}
	if _, problems := Covered(&testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{child, dup}}); len(problems) != 1 || !strings.Contains(problems[0], "already runs") {
		t.Fatalf("two rows for one subtest: %v", problems)
	}
	gone := &Result{}
	p := Check(gone, reg, nil)
	joined := strings.Join(p, "\n")
	if !strings.Contains(joined, "tooling.baseline.edit-pair-f058-fixed-to-bounded runs go-test internal/baseline TestCommittedEditPairs/f058-fixed-to-bounded, which the repository does not hold") {
		t.Fatalf("a subtest whose test is gone:\n%s", joined)
	}
}

// TestMakePlantAndGoldenRowsCoverTheirTests: a make row covers its make
// target, a plant row its negative control, a golden row the data case its
// files are; each row naming a test the repository does not hold is named.
func TestMakePlantAndGoldenRowsCoverTheirTests(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindMakeTarget, Dir: "Makefile", Name: "prep", Source: "Makefile:1"},
		{Kind: KindNegativeControl, Dir: "g1", Name: "nc-one", Source: "make/negative-controls.json:1"},
		{Kind: KindDataCase, Dir: "testdata/corpus", Name: "x", Source: "testdata/corpus/x.in:1"},
	}}
	row := func(key, adapter, inputs string) testreg.Test {
		return testreg.Test{Key: key, Adapter: adapter, Inputs: []byte(inputs)}
	}
	good := []testreg.Test{
		row("harness-run.makefile.prep", "make", `{"file":"Makefile","target":"prep"}`),
		row("plant.g1.nc-one", "plant", `{"make":"nc-one","file":"Makefile","group":"g1"}`),
		// one golden row per pinned file: the rows of one case share it
		row("golden.x.x-in", "golden", `{"writer":"pins-x","files":["testdata/corpus/x.in"]}`),
		row("golden.x.x-out", "golden", `{"writer":"pins-x","files":["testdata/corpus/x.out"]}`),
		row("golden.x.x-bench", "golden", `{"writer":"pins-x","files":["bench/x.bin"]}`),
		row("plant.f.env", "plant", `{"target":"go.functional.roundtrip.f.c","env":{"B":"1"}}`),
	}
	if p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: good}, nil); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	for name, c := range map[string]struct {
		extra testreg.Test
		says  string
	}{
		"a renamed make target":   {row("harness-run.makefile.gone", "make", `{"file":"Makefile","target":"gone"}`), "registry row harness-run.makefile.gone runs make-target Makefile gone, which the repository does not hold"},
		"a control in no group":   {row("plant.g1.x", "plant", `{"make":"nc-two","file":"Makefile"}`), "names no group"},
		"a control twice":         {row("plant.g1.again", "plant", `{"make":"nc-one","file":"Makefile","group":"g1"}`), "plant.g1.again runs negative control nc-one, which plant.g1.nc-one already runs"},
		"a deleted golden":        {row("golden.x.gone", "golden", `{"writer":"pins-x","files":["testdata/corpus/gone.bin"]}`), "registry row golden.x.gone runs data-case testdata/corpus gone, which the repository does not hold"},
		"a golden twice":          {row("golden.x.again", "golden", `{"writer":"pins-x","files":["testdata/corpus/x.out"]}`), "golden.x.again pins testdata/corpus/x.out, which golden row golden.x.x-out already pins"},
		"a golden with no files":  {row("golden.x.none", "golden", `{"writer":"pins-x"}`), "golden.x.none is a golden row without files"},
		"a golden of a directory": {row("golden.x.dir", "golden", `{"writer":"pins-x","files":["testdata/corpus/"]}`), "golden.x.dir pins testdata/corpus/, which is not a file"},
		"a make row with no file": {row("harness-run.makefile.nofile", "make", `{"target":"prep"}`), "is a make row without a file and a target"},
	} {
		p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: append(slices.Clone(good), c.extra)}, nil)
		if !strings.Contains(strings.Join(p, "\n"), c.says) {
			t.Errorf("%s: want %q in:\n%s", name, c.says, strings.Join(p, "\n"))
		}
	}
}

// TestTheShardRowsOfAGateShareItsTarget: the rows of a sharded make gate,
// each setting one *_SHARD var to K/N, together cover its target once; a
// shard run twice, a shard of another N, a shard row gone, a whole row
// beside the shards, two shard vars on one row, and a value that is not K/N
// are each named.
func TestTheShardRowsOfAGateShareItsTarget(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{{Kind: KindMakeTarget, Dir: "make/x.mk", Name: "lint", Source: "make/x.mk:1"}}}
	shard := func(key, vars string) testreg.Test {
		return testreg.Test{Key: key, Adapter: "make", Inputs: []byte(`{"file":"make/x.mk","target":"lint","vars":{` + vars + `}}`)}
	}
	good := []testreg.Test{
		shard("lint.x.lint-shard-1-of-3", `"X_SHARD":"1/3","OTHER":"y"`),
		shard("lint.x.lint-shard-2-of-3", `"X_SHARD":"2/3","OTHER":"y"`),
		shard("lint.x.lint-shard-3-of-3", `"X_SHARD":"3/3","OTHER":"y"`),
	}
	if p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: good}, nil); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	for name, c := range map[string]struct {
		tests []testreg.Test
		says  string
	}{
		"a shard run twice":          {append(slices.Clone(good), shard("lint.x.again", `"X_SHARD":"2/3"`)), "lint.x.again runs shard 2/3 of make lint, which lint.x.lint-shard-2-of-3 already runs"},
		"a shard of another N":       {append(slices.Clone(good), shard("lint.x.four", `"X_SHARD":"1/4"`)), "lint.x.four shards make lint as X_SHARD=1/4, but lint.x.lint-shard-1-of-3 shards it as X_SHARD of 3"},
		"a shard row gone":           {[]testreg.Test{good[0], good[2]}, "no row runs shard 2/3 of make lint (X_SHARD)"},
		"a whole row after":          {append(slices.Clone(good), shard("lint.x.whole", `"OTHER":"y"`)), "lint.x.whole runs make lint, which lint.x.lint-shard-1-of-3 already runs"},
		"a whole row before":         {append([]testreg.Test{shard("lint.x.whole", `"OTHER":"y"`)}, good...), "lint.x.lint-shard-1-of-3 runs shard 1/3 of make lint, which lint.x.whole already runs whole"},
		"two shard vars":             {append(slices.Clone(good), shard("lint.x.two", `"X_SHARD":"1/3","Y_SHARD":"1/2"`)), "lint.x.two: sets two shard vars, X_SHARD and Y_SHARD"},
		"a shard that is not K of N": {append(slices.Clone(good), shard("lint.x.zero", `"X_SHARD":"0/3"`)), `lint.x.zero: X_SHARD="0/3" is not a shard K/N`},
	} {
		p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: c.tests}, nil)
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

// TestAGoldenCoversACaseOnlyWhenItPinsEveryFile: a case whose expectation is
// pinned beside an input the languages still run stays in the ledger; a case
// every file of which is pinned is covered.
func TestAGoldenCoversACaseOnlyWhenItPinsEveryFile(t *testing.T) {
	t.Parallel()
	conf := Item{Kind: KindDataCase, Dir: "testdata/conformance", Name: "c", Source: "testdata/conformance/c.json:1"}
	pin := Item{Kind: KindDataCase, Dir: "testdata/pins", Name: "p", Source: "testdata/pins/p.h:1"}
	res := &Result{Items: []Item{conf, pin}, CaseFiles: map[string][]string{
		conf.ID(): {"testdata/conformance/c.json", "testdata/conformance/c.expect"},
		pin.ID():  {"testdata/pins/p.h", "testdata/pins/p.cpp"},
	}}
	row := func(key, file string) testreg.Test {
		return testreg.Test{Key: key, Adapter: "golden", Inputs: []byte(`{"writer":"pins-x","files":["` + file + `"]}`)}
	}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{
		row("golden.c.expect", "testdata/conformance/c.expect"),
		row("golden.p.h", "testdata/pins/p.h"),
		row("golden.p.cpp", "testdata/pins/p.cpp"),
	}}
	covered, p := CoveredIn(res, reg)
	if len(p) != 0 {
		t.Fatalf("problems: %v", p)
	}
	if _, ok := covered[conf.ID()]; ok {
		t.Errorf("a case with an unpinned input is covered by %s", covered[conf.ID()])
	}
	if covered[pin.ID()] == "" {
		t.Errorf("a case every file of which is pinned is not covered")
	}
	ledger := Ledger(res, reg, func(string, string, []Item) (string, string) { return "L4", "the languages run it" })
	if len(ledger) != 1 || ledger[0].Dir != "testdata/conformance" || ledger[0].Count != 1 {
		t.Errorf("the ledger: %+v", ledger)
	}
}

// TestAMakeRowsReadsCoverTheCasesItRuns: a make row covers the data cases
// its reads name, every file of each; a case a golden row pins and a make
// row reads is claimed by both with no problem (two checks of one case),
// and stays the golden row's; a read of a file no case holds, a read
// outside the data paths and a case only partly read are each named or
// left in the ledger.
func TestAMakeRowsReadsCoverTheCasesItRuns(t *testing.T) {
	t.Parallel()
	vnew := Item{Kind: KindDataCase, Dir: "test/tables", Name: "VNEW_a", Source: "test/tables/VNEW_a.schema:1"}
	pin := Item{Kind: KindDataCase, Dir: "testdata/pins", Name: "p", Source: "testdata/pins/p.h:1"}
	pair := Item{Kind: KindDataCase, Dir: "test/tables", Name: "pair", Source: "test/tables/pair.in:1"}
	gate := Item{Kind: KindMakeTarget, Dir: "Makefile", Name: "gate", Source: "Makefile:1"}
	res := &Result{Items: []Item{gate, vnew, pin, pair}, CaseFiles: map[string][]string{
		vnew.ID(): {"test/tables/VNEW_a.schema"},
		pin.ID():  {"testdata/pins/p.h"},
		pair.ID(): {"test/tables/pair.in", "test/tables/pair.out"},
	}}
	mk := func(reads string) testreg.Test {
		return testreg.Test{Key: "harness-run.makefile.gate", Adapter: "make", Inputs: []byte(`{"file":"Makefile","target":"gate","reads":[` + reads + `]}`)}
	}
	golden := testreg.Test{Key: "golden.p.h", Adapter: "golden", Inputs: []byte(`{"writer":"pins-x","files":["testdata/pins/p.h"]}`)}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{golden, mk(`"test/tables/VNEW_a.schema","testdata/pins/p.h","test/tables/pair.in"`)}}
	covered, p := CoveredIn(res, reg)
	if len(p) != 0 {
		t.Fatalf("a golden row and a make row on one case: %v", p)
	}
	if covered[vnew.ID()] != "harness-run.makefile.gate" || covered[pin.ID()] != "golden.p.h" {
		t.Errorf("covered %v: the read case is the make row's, the pinned one the golden row's", covered)
	}
	if _, ok := covered[pair.ID()]; ok {
		t.Errorf("a case read in part is covered by %s", covered[pair.ID()])
	}
	ledger := []LedgerLine{{Kind: KindDataCase, Dir: "test/tables", Count: 1}}
	if p := Check(res, reg, ledger); len(p) != 0 {
		t.Errorf("the exact account: %v", p)
	}
	for name, c := range map[string]struct {
		reads, says string
	}{
		"a file no case holds":   {`"test/tables/VNEW_gone.schema"`, "harness-run.makefile.gate reads test/tables/VNEW_gone.schema, which the repository does not hold"},
		"a file of no data path": {`"internal/x/a.go"`, "harness-run.makefile.gate reads internal/x/a.go, which is no data case's file"},
	} {
		p := Check(res, &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{golden, mk(c.reads)}}, nil)
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
// no row runs or reads is named with its place; one with a row, a data case a
// per-language row reads (Indexed, as the ledger counts it), and one main
// has, are not.
func TestANewTestWithoutARowIsNamed(t *testing.T) {
	t.Parallel()
	before := &Result{Items: []Item{{Kind: KindGoTest, Dir: "a", Name: "TestOld", Source: "a/x_test.go:1"}}}
	now := &Result{Items: []Item{
		{Kind: KindGoTest, Dir: "a", Name: "TestOld", Source: "a/x_test.go:1"},
		{Kind: KindGoTest, Dir: "a", Name: "TestRow", Source: "a/x_test.go:5"},
		{Kind: KindGoTest, Dir: "a", Name: "TestPlanted", Source: "a/y_test.go:3"},
		{Kind: KindDataCase, Dir: "testdata/corpus", Name: "x", Source: "testdata/corpus/x.in:1"},
	}}
	perLang := testreg.Test{Key: "roundtrip.conformance-wire.x", Inputs: []byte(`{"surface":"wire","case":"x","files":["testdata/corpus/x.in"],"driver":"test/conformance/{lang}/driver"}`)}
	reg := &testreg.Registry{Path: "r.jsonl", Langs: []testreg.Lang{{Name: "zz", Adapter: "corpus"}}, Tests: []testreg.Test{gotestRow("tooling.a.row", "./a", "TestRow"), perLang}}
	p := NewWithoutRows(before, now, reg)
	if len(p) != 1 || !strings.Contains(p[0], "go-test a TestPlanted at a/y_test.go:3 is a new test with no registry row") {
		t.Fatalf("want exactly TestPlanted named: %v", p)
	}
}

// TestAnUnreadableRegistryIsNamed: a registry the scan cannot read is an
// unparsed file, named, never a tree with no make rows.
func TestAnUnreadableRegistryIsNamed(t *testing.T) {
	t.Parallel()
	files := maps.Clone(fixture)
	delete(files, testreg.DefaultPath)
	root := writeFixture(t, files)
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(testreg.DefaultPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range res.Unparsed {
		if u.File == testreg.DefaultPath && strings.Contains(u.Reason, "read") {
			return
		}
	}
	t.Fatalf("an unreadable registry was not named: %+v", res.Unparsed)
}

// TestAPerLanguageRowRunsEachLanguagesTarget: a per-language row whose
// target holds {lang} runs that target for every language the registry
// lists, so each language's rule is a make target as a make row's would be;
// a language with no such rule adds nothing, a row scoped to no language
// expands nothing, the language-free scope "none" is no language even when
// a lang record names it, and a target with no {lang} is not a make row's.
func TestAPerLanguageRowRunsEachLanguagesTarget(t *testing.T) {
	t.Parallel()
	files := maps.Clone(fixture)
	files["Makefile"] += "\nruns-aa-corpus:\n\ttrue\n\nruns-bb-corpus:\n\ttrue\n\nruns-cc-corpus:\n\ttrue\n\nloose-zz:\n\ttrue\n\nruns-aa-x:\n\ttrue\n\nruns-none-corpus:\n\ttrue\n"
	files[testreg.DefaultPath] += `{"rec":"lang","name":"aa","adapter":"corpus"}` + "\n" +
		`{"rec":"lang","name":"bb","adapter":"corpus"}` + "\n" +
		`{"rec":"lang","name":"dd","adapter":"corpus"}` + "\n" +
		`{"rec":"lang","name":"none","adapter":"corpus"}` + "\n" +
		`{"rec":"test","key":"decode.c.one","type":"decode","family":"c","case":"one","inputs":{"target":"runs-{lang}-corpus"}}` + "\n" +
		`{"rec":"test","key":"decode.c.free","type":"decode","family":"c","case":"free","scope":"none","adapter":"gotest","inputs":{"target":"runs-{lang}-x"}}` + "\n" +
		`{"rec":"test","key":"decode.c.loose","type":"decode","family":"c","case":"loose","inputs":{"target":"loose-zz"}}` + "\n"
	res, err := Extract(writeFixture(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Unparsed) != 0 {
		t.Fatalf("unparsed: %+v", res.Unparsed)
	}
	got := map[string]bool{}
	for _, it := range res.Items {
		if it.Kind == KindMakeTarget {
			got[it.Name] = true
		}
	}
	for name, want := range map[string]bool{"runs-aa-corpus": true, "runs-bb-corpus": true, "runs-cc-corpus": false, "loose-zz": false, "runs-aa-x": false, "runs-none-corpus": false, "row-only": true} {
		if got[name] != want {
			t.Errorf("make target %s counted %v, want %v", name, got[name], want)
		}
	}
}

// TestAMalformedMakeRowIsNamed: a make row of the registry that is not JSON
// is named, never read as no row at all.
func TestAMalformedMakeRowIsNamed(t *testing.T) {
	t.Parallel()
	files := maps.Clone(fixture)
	files[testreg.DefaultPath] = `{"rec":"test","adapter":"make","inputs":{` + "\n"
	res, err := Extract(writeFixture(t, files))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range res.Unparsed {
		if u.File == testreg.DefaultPath && strings.Contains(u.Reason, "a make row") {
			return
		}
	}
	t.Fatalf("a malformed make row was not named: %+v", res.Unparsed)
}

// TestAMakeTargetThatStopsBeingATestIsNamed: a make target main counts as a
// test that this tree does not, while its rule stands (its row deleted), is
// named; one whose rule went with it, and one still counted, are not.
func TestAMakeTargetThatStopsBeingATestIsNamed(t *testing.T) {
	t.Parallel()
	before := &Result{Items: []Item{
		{Kind: KindMakeTarget, Dir: "Makefile", Name: "gate-kept"},
		{Kind: KindMakeTarget, Dir: "Makefile", Name: "gate-unrowed"},
		{Kind: KindMakeTarget, Dir: "Makefile", Name: "gate-removed"},
	}}
	now := &Result{
		Items: []Item{{Kind: KindMakeTarget, Dir: "Makefile", Name: "gate-kept"}},
		Rules: map[string]bool{RuleID("Makefile", "gate-kept"): true, RuleID("Makefile", "gate-unrowed"): true},
	}
	p := DroppedMakeTargets(before, now)
	if len(p) != 1 || !strings.Contains(p[0], "make target gate-unrowed (Makefile) is a test on main and no longer one here") {
		t.Fatalf("want exactly gate-unrowed named: %v", p)
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
		// a skip is a fail under the driver, so no ledger line accounts
		// for one: the skip-red kind is gone
		{"skip-red\ta:TestX\t1\tL5\tred\n", `unknown kind "skip-red"`},
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
	lines := Ledger(res, reg, func(kind, dir string, _ []Item) (string, string) { return "L9", "fixture" })
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
// language that answers the row. A language-free row indexes the files its
// inputs name when it names its adapter, and nothing when it names none;
// and a data case no row reads is still unmoved. Only what runs counts: the
// driver of a language with no adapter is unmoved, and a row no ready
// language answers (none has an adapter, or each declares it unrepresentable) indexes nothing.
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
	// a language-free row runs through its adapter, so the files it names
	// are indexed (y, read by the Go test TestY); one naming no adapter
	// indexes nothing (planted: drop the adapter check, and it indexes z)
	unrun := testreg.Test{Key: "tooling.t.z", Scope: testreg.LangFree, Inputs: []byte(`{"files":["testdata/corpus/z.in"]}`)}
	// an adapter that does not hold a row to its files (model-check, plant)
	// indexes none of them, however the row names them
	unheld := testreg.Test{Key: "tooling.t.w", Scope: testreg.LangFree, Adapter: "model-check", Inputs: []byte(`{"files":["testdata/corpus/w.in"]}`)}
	reg.Tests = append(reg.Tests, free, unrun, unheld)
	got = Indexed(reg)
	if len(got) != 4 || got[res.Items[1].ID()] != free.Key || got[Item{Kind: KindDataCase, Dir: "testdata/corpus", Name: "z"}.ID()] != "" ||
		got[Item{Kind: KindDataCase, Dir: "testdata/corpus", Name: "w"}.ID()] != "" {
		t.Fatalf("language-free rows indexed %v, want y and neither z nor w", got)
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
// move artifacts, use the repository's setup actions (testreg.SetupActions)
// and call the driver runs no test of its own, and neither does a job whose
// only work is setup actions (it builds what the rows read); a job that
// also runs a make target (on its own line or after a shell operator),
// uses another action (a local one that is no setup action included), runs
// under another shell, or runs nothing at all is still a test.
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
		"  empty:\n    runs-on: x\n" +
		"  setup:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@abc\n      - uses: ./.github/actions/merge-main\n      - uses: ./.github/actions/schema-test-setup\n        with:\n          lang: none\n      - run: go run ./tools/schema-test run --group pr --lang none --shard 1/2\n" +
		"  produce:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@abc\n      - uses: ./.github/actions/produce\n        with:\n          mode: produce\n" +
		"  producers:\n    runs-on: x\n    strategy:\n      matrix:\n        include: [{\"name\":\"lint-c\",\"target\":\"tables-c-clean-inputs\"}]\n    steps:\n      - uses: ./.github/actions/produce\n        with:\n          mode: produce\n          target: ${{ matrix.target }}\n" +
		"  producecheck:\n    runs-on: x\n    steps:\n      - uses: ./.github/actions/produce\n        with:\n          mode: produce\n          target: tables-c-clean\n" +
		"  producematrix:\n    runs-on: x\n    strategy:\n      matrix:\n        include: [{\"name\":\"c\",\"target\":\"tables-c-clean\"}]\n    steps:\n      - uses: ./.github/actions/produce\n        with:\n          mode: produce\n          target: ${{ matrix.target }}\n" +
		"  local:\n    runs-on: x\n    steps:\n      - uses: ./.github/actions/eslint\n      - run: go run ./tools/fullrun merge\n"
	res, err := Extract(writeFixture(t, map[string]string{"go.mod": "module example.com/fx\n", ".github/workflows/n.yml": wf}))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range res.Items {
		got = append(got, it.Name)
	}
	sort.Strings(got)
	if want := []string{"action", "alias", "bare", "chained", "continued", "empty", "flow", "local", "mixed", "producecheck", "producematrix", "setchain", "shell", "spaced"}; !slices.Equal(got, want) {
		t.Errorf("jobs counted as tests: %v, want %v", got, want)
	}
}

// TestAPerLanguageRowsTargetNoMakeFileDefinesIsNamed: a per-language row
// whose make target, {lang} expanded, no make file writes runs nothing in
// that language, so the extract names it (once per target) rather than
// leaving every instance red for a reason no case shows. A target a pattern
// rule matches is defined; a language that declares the row unrepresentable
// has no instance to run it.
func TestAPerLanguageRowsTargetNoMakeFileDefinesIsNamed(t *testing.T) {
	t.Parallel()
	res := &Result{Rules: map[string]bool{
		RuleID("Makefile", "runs-aa-corpus"):    true,
		RuleID("make/x.mk", "runs-%-text"):      true,
		RuleID("make/x.mk", "runs-cc-corpus"):   true,
		RuleID("make/x.mk", "unrelated-target"): true,
	}}
	row := func(key, family, target string) testreg.Test {
		return testreg.Test{Key: key, Family: family, Inputs: []byte(`{"target":"` + target + `","case":"x"}`)}
	}
	reg := &testreg.Registry{
		Path:  "r.jsonl",
		Langs: []testreg.Lang{{Name: "aa"}, {Name: "bb"}, {Name: "cc"}},
		Tests: []testreg.Test{
			row("decode.corpus.one", "corpus", "runs-{lang}-corpus"),
			row("decode.corpus.two", "corpus", "runs-{lang}-corpus"),
			row("decode.text.one", "text", "runs-{lang}-text"),
		},
	}
	_, p := CoveredIn(res, reg)
	if len(p) != 1 || !strings.Contains(p[0], "decode.corpus.one (and every row of family corpus) runs make runs-bb-corpus for bb, which no make file defines") {
		t.Fatalf("want one problem naming runs-bb-corpus, got %d:\n%s", len(p), strings.Join(p, "\n"))
	}
	// the pattern rules empty stem is no match: runs--text is not runs-%-texts
	if makeDefines(map[string]bool{}, []string{"runs-%-text"}, "runs--text") {
		t.Errorf("an empty stem matched a pattern rule")
	}
}

// TestARowNamingAJsonlFileReadsEveryLine: a .jsonl file's lines are cases
// of their own (dataCases names each for its line), so a row naming the
// file claims no stem for it; it reads every line, so each line case counts
// as the row's in Check, and a line case no row's file names is unmoved.
func TestARowNamingAJsonlFileReadsEveryLine(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindDataCase, Dir: "testdata/corpus", Name: "cases/line1", Source: "testdata/corpus/cases.jsonl:1"},
		{Kind: KindDataCase, Dir: "testdata/corpus", Name: "cases/line2", Source: "testdata/corpus/cases.jsonl:2"},
		{Kind: KindDataCase, Dir: "testdata/other", Name: "cases/line1", Source: "testdata/other/cases.jsonl:1"},
	}}
	row := testreg.Test{Key: "roundtrip.conformance-wire.cases", Inputs: []byte(`{"surface":"wire","case":"cases","files":["testdata/corpus/cases.jsonl"],"driver":"test/conformance/{lang}/driver"}`)}
	reg := &testreg.Registry{Path: "r.jsonl", Langs: []testreg.Lang{{Name: "zz", Adapter: "corpus"}}, Tests: []testreg.Test{row}}
	ledger := []LedgerLine{{Kind: KindDataCase, Dir: "testdata/other", Count: 1}}
	if p := Check(res, reg, ledger); len(p) != 0 {
		t.Fatalf("the exact account: %v", p)
	}
	// the row gone, its two line cases are unmoved again
	if p := Check(res, &testreg.Registry{Path: "r.jsonl", Langs: reg.Langs}, ledger); !strings.Contains(strings.Join(p, "\n"), "testdata/corpus: 2 tests are neither registry rows") {
		t.Fatalf("a dropped row: %v", p)
	}
	// a row no ready language answers reads nothing
	idle := &testreg.Registry{Path: "r.jsonl", Langs: []testreg.Lang{{Name: "zz"}}, Tests: []testreg.Test{row}}
	if p := Check(res, idle, ledger); !strings.Contains(strings.Join(p, "\n"), "testdata/corpus: 2 tests are neither registry rows") {
		t.Fatalf("an unanswered row: %v", p)
	}
}

// TestAGoTestSubtestRowClaimsTheSubtest: a row that names a subtest claims
// Test/sub, and the parent row still claims Test. The subtest is held when
// that Test exists. It does not cover the parent, and a second row for the
// same subtest is named. A subtest whose test the repository does not hold
// is named. A subtest name with a slash is not a claim.
func TestAGoTestSubtestRowClaimsTheSubtest(t *testing.T) {
	t.Parallel()
	res := &Result{Items: []Item{
		{Kind: KindGoTest, Dir: "compiler", Name: "TestParent", Source: "compiler/x_test.go:1"},
	}}
	parent := gotestRow("compiler.compiler.parent", "./compiler", "TestParent")
	child := testreg.Test{Key: "compiler.compiler.child", Adapter: "gotest", Inputs: []byte(`{"package":"./compiler","test":"TestParent","subtest":"Player.schema"}`)}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{parent, child}}
	if p := Check(res, reg, nil); len(p) != 0 {
		t.Fatalf("parent and its subtest: %v", p)
	}
	covered, problems := Covered(reg)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if covered["go-test compiler TestParent"] != "compiler.compiler.parent" {
		t.Errorf("parent claim: %v", covered)
	}
	if covered["go-test compiler TestParent/Player.schema"] != "compiler.compiler.child" {
		t.Errorf("subtest claim: %v", covered)
	}
	onlyChild := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{child}}
	p := Check(res, onlyChild, nil)
	joined := strings.Join(p, "\n")
	if !strings.Contains(joined, "TestParent") || strings.Contains(joined, "does not hold") {
		t.Fatalf("a subtest row does not cover its parent, and the subtest is held: %v", p)
	}
	again := child
	again.Key = "compiler.compiler.again"
	p = Check(res, &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{parent, child, again}}, nil)
	if !strings.Contains(strings.Join(p, "\n"), "already runs") {
		t.Fatalf("two rows for one subtest: %v", p)
	}
	orphan := testreg.Test{Key: "compiler.compiler.orphan", Adapter: "gotest", Inputs: []byte(`{"package":"./compiler","test":"TestGone","subtest":"X.schema"}`)}
	p = Check(res, &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{parent, orphan}}, nil)
	if !strings.Contains(strings.Join(p, "\n"), "registry row compiler.compiler.orphan runs go-test compiler TestGone/X.schema, which the repository does not hold") {
		t.Fatalf("orphan subtest: %v", p)
	}
	slash := testreg.Test{Key: "compiler.compiler.slash", Adapter: "gotest", Inputs: []byte(`{"package":"./compiler","test":"TestParent","subtest":"a/b"}`)}
	p = Check(res, &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{parent, slash}}, nil)
	if !strings.Contains(strings.Join(p, "\n"), `names subtest "a/b"`) {
		t.Fatalf("slash: %v", p)
	}
}

// TestAMakeRowsSourceIsHeldToItsRulesLine: a make row's source is the file
// and the first line its target's rule is written on; a source on another
// line, with no line, or in another file is named with the line to set, and
// a row naming the right line, a per-language row and a rule no make file
// writes are not named here. Planted: without MakeSourceProblems in Check, a
// stale line passes.
func TestAMakeRowsSourceIsHeldToItsRulesLine(t *testing.T) {
	t.Parallel()
	root := writeFixture(t, fixture)
	res, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	row := func(key, target, source string) testreg.Test {
		return testreg.Test{Key: key, Adapter: "make", Source: source, Inputs: []byte(`{"file":"Makefile","target":"` + target + `"}`)}
	}
	reg := &testreg.Registry{Path: "r.jsonl", Tests: []testreg.Test{
		row("a.right", "prep", "Makefile:7"),
		row("a.stale", "prep", "Makefile:8"),
		row("a.noline", "quoted", "Makefile"),
		row("a.elsewhere", "build", "make/x.mk:4"),
		row("a.none", "no-such-rule", "Makefile:1"),
		{Key: "a.perlang", Adapter: "make", Source: "Makefile:99", Inputs: []byte(`{"file":"Makefile","target":"runs-{lang}-x"}`)},
	}}
	got := MakeSourceProblems(res, reg)
	want := []string{
		`r.jsonl:0: a.stale source is "Makefile:8", and prep is written at Makefile:7: set source to "Makefile:7"`,
		`r.jsonl:0: a.noline source is "Makefile", and quoted is written at Makefile:10: set source to "Makefile:10"`,
		`r.jsonl:0: a.elsewhere source is "make/x.mk:4", and build is written at Makefile:4: set source to "Makefile:4"`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("make row sources:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// Check carries them: the stale row is named by Check
	found := false
	for _, p := range Check(res, reg, nil) {
		if strings.Contains(p, "a.stale source is") {
			found = true
		}
	}
	if !found {
		t.Errorf("Check does not name a make row whose source line is stale")
	}
}
