package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// world is one test's repository: files under its own directory, and a fake
// shell that answers the coverage commands it is given.
type world struct {
	t   *testing.T
	dir string
	sh  map[string]string // command -> stdout; a command it does not know fails
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, dir: t.TempDir(), sh: map[string]string{}}
	for _, test := range []string{"TestOne", "TestTwo", "TestTwo/sub"} {
		w.file("bl/ex%2Fa/"+escape(test), "")
	}
	return w
}

func (w *world) file(name, body string) string {
	p := filepath.Join(w.dir, name)
	require.NoError(w.t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(w.t, os.WriteFile(p, []byte(body), 0o644))
	return name
}

func (w *world) shell(_, command string) ([]byte, error) {
	out, ok := w.sh[command]
	if !ok {
		return nil, errors.New("exit status 1")
	}
	return []byte(out), nil
}

func (w *world) check(args ...string) (int, string, string) {
	var so, se bytes.Buffer
	code := run(w.dir, append([]string{"check"}, args...), &so, &se, w.shell)
	return code, so.String(), se.String()
}

// The results every rule is planted into: one go package with three tests that
// ran and one ledgered skip, its baseline written by newWorld.
const goGreen = `{"Action":"start","Package":"ex/a"}
{"Action":"run","Package":"ex/a","Test":"TestOne"}
{"Action":"pass","Package":"ex/a","Test":"TestOne"}
{"Action":"run","Package":"ex/a","Test":"TestTwo"}
{"Action":"run","Package":"ex/a","Test":"TestTwo/sub"}
{"Action":"pass","Package":"ex/a","Test":"TestTwo/sub"}
{"Action":"pass","Package":"ex/a","Test":"TestTwo"}
{"Action":"run","Package":"ex/a","Test":"TestSkipped"}
{"Action":"skip","Package":"ex/a","Test":"TestSkipped"}
{"Action":"pass","Package":"ex/a"}
`

func (w *world) config(extra string) string {
	return w.file("testguard.tsv", "# a config\nsuite\tex/a\nskip\tex/a\tTestSkipped\tneeds a GPU\nbaseline\tbl\n"+extra)
}

func TestGreenRunIsOK(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	code, out, errOut := w.check("--config", w.config(""), w.file("go.json", goGreen))
	assert.Equal(t, exitOK, code, out+errOut)
	assert.Equal(t, "OK: every test that should run ran and passed; read 1 result files: 1 suites, 3 tests run, 1 skipped; 0 coverage tables, 0 pairs\n", out)
	assert.Empty(t, errOut)
}

func TestFailedTestIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	results := strings.Replace(goGreen, `"pass","Package":"ex/a","Test":"TestOne"`, `"fail","Package":"ex/a","Test":"TestOne"`, 1)
	code, out, _ := w.check("--config", w.config(""), w.file("go.json", results))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED failed ex/a\tTestOne: failed, or started and never ended; fix: make it pass\n")
}

func TestPackageFailureOutsideATestIsIncomplete(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	results := strings.Replace(goGreen, `{"Action":"pass","Package":"ex/a"}`, `{"Action":"fail","Package":"ex/a"}`, 1)
	code, out, _ := w.check("--config", w.config(""), w.file("go.json", results))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED incomplete ex/a: the package failed (a build failure, a panic outside a test, or a timeout);")
}

func TestUnledgeredSkipIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	cfg := w.file("c.tsv", "suite\tex/a\nbaseline\tbl\n")
	code, out, _ := w.check("--config", cfg, w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED skip ex/a\tTestSkipped: skipped, and the ledger does not hold it; fix: make it run, or add the line skip<TAB>SUITE<TAB>TEST<TAB>REASON to c.tsv\n")
	assert.Contains(t, out, "RED: 1 findings (skip 1); read 1 result files")
}

func TestLedgerIdsDoNotCollide(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	// Suite "a" test "b.c" and suite "a.b" test "c" are two tests; a ledgered skip of one does not hold the other.
	w.file("bl/a/b.c", "")
	w.file("bl/a.b/d", "")
	cfg := w.file("c.tsv", "suite\ta\nsuite\ta.b\nskip\ta\tb.c\tr\nbaseline\tbl\n")
	r := w.file("r.xml", `<testsuites><testsuite name="a"><testcase classname="b" name="c"/></testsuite><testsuite name="a.b"><testcase name="d"/><testcase name="c"><skipped/></testcase></testsuite></testsuites>`)
	code, out, _ := w.check("--config", cfg, r)
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED skip a.b\tc:")
	assert.Contains(t, out, "RED stale-skip a\tb.c:")
}

func TestLedgeredSkipThatRanIsStale(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	code, out, _ := w.check("--config", w.config("skip\tex/a\tTestOne\twas flaky\n"), w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED stale-skip ex/a\tTestOne: ledgered as a skip, and it did not skip here (it ran, or it is gone); fix: delete its skip line from testguard.tsv\n")
}

func TestMissingSuiteIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	code, out, _ := w.check("--config", w.config("suite\tex/b\n"), w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED missing-suite ex/b: registered, and absent from every result file;")
}

func TestEmptySuiteIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	results := goGreen + `{"Action":"output","Package":"ex/b","Output":"?   \tex/b\t[no test files]\n"}
{"Action":"skip","Package":"ex/b"}
`
	code, out, _ := w.check("--config", w.config("suite\tex/b\n"), w.file("go.json", results))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED empty-suite ex/b: registered, and ran 0 tests;")
	assert.NotContains(t, out, "unregistered")
}

func TestUnregisteredSuiteIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	results := goGreen + `{"Action":"run","Package":"ex/c","Test":"TestX"}
{"Action":"pass","Package":"ex/c","Test":"TestX"}
`
	code, out, _ := w.check("--config", w.config(""), w.file("go.json", results))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED unregistered-suite ex/c: ran 1 tests and is not registered")
}

func TestRefusals(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	good := w.file("go.json", goGreen)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--config", w.config("")}, "wants result files (none were read, and reading none is never a pass); run: go run ./tools/testguard check -h"},
		{[]string{"--config", w.config(""), "none.json"}, "reading a result file: open " + filepath.Join(w.dir, "none.json") + ": no such file or directory; run: ls -l none.json"},
		{[]string{"--config", w.config(""), w.file("r.md", "ok 1\n")}, "r.md has no format: its extension is none of"},
		{[]string{"--config", w.config(""), w.file("empty.json", "")}, "empty.json holds no go test -json event"},
		{[]string{"--config", w.config(""), w.file("doc.xml", "<html></html>")}, "doc.xml has a root <html>"},
		{[]string{"--config", w.config(""), w.file("zero.log", "ex/a: ran 0, failed 0\n")}, "the result files hold 0 tests that ran"},
		{[]string{"--config", w.file("n.tsv", "# none\n"), good}, "n.tsv has no suite line (a config that registers nothing checks nothing) and no baseline line; run: go run ./tools/testguard help check"},
		{[]string{"--config", w.file("nb.tsv", "suite\tex/a\n"), good}, "nb.tsv has no baseline line"},
		{[]string{"--config", w.file("ub.tsv", "suite\tex/a\nbaseline\tnope\n"), good}, "the baseline nope cannot be read"},
	} {
		code, out, errOut := w.check(c.args...)
		assert.Equal(t, exitRefused, code, c.want)
		assert.Empty(t, out, c.want)
		assert.Contains(t, errOut, c.want)
		assert.Equal(t, 1, strings.Count(errOut, "\n"), errOut)
	}
}

func TestAFormatPrefixChoosesTheReader(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.file("bl/r/a", "")
	code, out, _ := w.check("--config", w.config("suite\tr\n"), w.file("go.json", goGreen), "tap:"+w.file("r.md", "1..1\nok 1 - a\n"))
	assert.Equal(t, exitOK, code, out)
}

func TestBadConfigIsRefusedWithItsLine(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for body, want := range map[string]string{
		"suite\n":                         `:1: "suite" wants NAME, separated by tabs; run: sed -n 1p `,
		"# c\nskip\tex/a.TestX\tr\n":      `:2: "skip" wants SUITE, TEST and REASON`,
		"skip ex/a.TestX because\n":       `:1: "skip ex/a.TestX because" is no entry`,
		"skip\tex/a\tTestX \tr\n":         `a field is empty or starts or ends with a blank: "TestX "`,
		"cover\tt\tdiagonal\tfile\tx\n":   `"cover" wants TABLE, rows|columns|pairs`,
		"suite\t\n":                       `a field is empty or starts or ends with a blank`,
		"gap\tt\tr\tc\n":                  `"gap" wants TABLE, ROW, COLUMN and REASON`,
		"baseline\ta\nbaseline\tb\n":      `"baseline" wants DIR, once`,
		"suite\tex/a\nfrobnicate\tx\ty\n": `:2: "frobnicate" is no entry; the entries are suite, skip, cover, gap and baseline`,
		"suite\tex/a\nbaseline\tbl\ncover\tt\trows\tfile\tr.txt\n": `table t has no columns source`,
	} {
		code, _, errOut := w.check("--config", w.file("bad.tsv", body), w.file("go.json", goGreen))
		assert.Equal(t, exitRefused, code, body)
		assert.Contains(t, errOut, want, body)
		assert.Equal(t, 1, strings.Count(errOut, "\n"), body)
	}
}

func (w *world) coverConfig(extra string) string {
	return w.config("cover\tcorpus\trows\tfile\troots.txt\n" +
		"cover\tcorpus\tcolumns\trun\tls drivers\n" +
		"cover\tcorpus\tpairs\trun\tgrep -l roots\n" + extra)
}

func (w *world) corpus() {
	w.file("roots.txt", "# roots\nfx1\nfx2\n")
	w.sh["ls drivers"] = "go\nlua\n"
	w.sh["grep -l roots"] = "fx1\tgo\nfx1\tlua\nfx2\tgo\nfx2\tlua\n"
}

func TestCoveredTableIsOK(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.corpus()
	code, out, errOut := w.check("--config", w.coverConfig(""), w.file("go.json", goGreen))
	assert.Equal(t, exitOK, code, out+errOut)
	assert.Contains(t, out, "1 coverage tables, 4 pairs")
}

func TestUncoveredPairIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.corpus()
	w.sh["grep -l roots"] = "fx1\tgo\nfx1\tlua\nfx2\tgo\n"
	code, out, _ := w.check("--config", w.coverConfig(""), w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED uncovered corpus fx2 x lua: no test covers this pair, and no gap holds it; fix: cover it, or add a gap line with the reason to testguard.tsv\n")
	assert.Contains(t, out, "(uncovered 1)")
}

func TestLedgeredGapIsOKAndStaleGapIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.corpus()
	w.sh["grep -l roots"] = "fx1\tgo\nfx1\tlua\nfx2\tgo\n"
	code, out, _ := w.check("--config", w.coverConfig("gap\tcorpus\tfx2\tlua\tlua has no fx2 codec yet\n"), w.file("go.json", goGreen))
	assert.Equal(t, exitOK, code, out)

	code, out, _ = w.check("--config", w.coverConfig("gap\tcorpus\tfx2\tlua\tr\ngap\tcorpus\tfx1\tgo\tr\ngap\tcorpus\tfx9\tgo\tr\n"), w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED stale-gap corpus fx1 x go: a gap holds a pair that is covered;")
	assert.Contains(t, out, "RED stale-gap corpus fx9 x go: a gap names a row or column the table does not list;")
}

func TestEmptyAxisAndStrayPairAreRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.corpus()
	w.sh["ls drivers"] = ""
	w.sh["grep -l roots"] = "fx1\trust\n"
	code, out, _ := w.check("--config", w.coverConfig(""), w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED uncovered corpus: its columns list is empty, so the table vouches for nothing;")
	assert.Contains(t, out, "RED uncovered corpus fx1 x rust: a covered pair names a row or column the table does not list;")
}

func TestFailedCoverCommandIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.corpus()
	delete(w.sh, "ls drivers")
	code, _, errOut := w.check("--config", w.coverConfig(""), w.file("go.json", goGreen))
	assert.Equal(t, exitRefused, code)
	assert.Equal(t, "testguard check: table corpus columns: the command \"ls drivers\" failed: exit status 1; run: ls drivers\n", errOut)
}

func TestMalformedPairIsRefused(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.corpus()
	w.sh["grep -l roots"] = "fx1 go\n"
	code, _, errOut := w.check("--config", w.coverConfig(""), w.file("go.json", goGreen))
	assert.Equal(t, exitRefused, code)
	assert.Contains(t, errOut, `table corpus pairs: "fx1 go" is not ROW<TAB>COLUMN`)
}

func TestVanishedTestIsRedUntilItsEntryGoes(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	gone := strings.ReplaceAll(goGreen, `{"Action":"run","Package":"ex/a","Test":"TestTwo/sub"}
{"Action":"pass","Package":"ex/a","Test":"TestTwo/sub"}
`, "")
	code, out, _ := w.check("--config", w.config(""), w.file("gone.json", gone))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED vanished ex/a\tTestTwo/sub: ran on the baseline and did not run here; fix: make it run, or delete bl/ex%2Fa/TestTwo%2Fsub in this change\n")
	require.NoError(t, os.Remove(filepath.Join(w.dir, "bl/ex%2Fa/TestTwo%2Fsub")))
	code, out, _ = w.check("--config", w.config(""), w.file("gone.json", gone))
	assert.Equal(t, exitOK, code, out)
}

func TestATestAddedWithoutItsEntryIsRed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	added := goGreen + `{"Action":"run","Package":"ex/a","Test":"TestThree"}
{"Action":"pass","Package":"ex/a","Test":"TestThree"}
`
	code, out, _ := w.check("--config", w.config(""), w.file("added.json", added))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED unlisted ex/a\tTestThree: ran, and the baseline does not list it; fix: add the empty file bl/ex%2Fa/TestThree in this change\n")
	w.file("bl/ex%2Fa/TestThree", "")
	code, out, _ = w.check("--config", w.config(""), w.file("added.json", added))
	assert.Equal(t, exitOK, code, out)
}

func TestWriteBaselineBootstrapsAndNeverDropsOrAbsorbs(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	require.NoError(t, os.RemoveAll(filepath.Join(w.dir, "bl")))
	results := w.file("go.json", goGreen)

	// A first run, with no baseline directory (git holds no empty one): every
	// test is unlisted, and that is all; the entries are written, and the run
	// is still red, so the flag can never make a gate green.
	code, out, _ := w.check("--config", w.config(""), "--write-baseline", results)
	assert.Equal(t, exitFindings, code, out)
	assert.Contains(t, out, "WROTE: 3 baseline entries under bl; commit them, then check without --write-baseline (a write is never a pass)\n")
	for _, f := range []string{"TestOne", "TestTwo", "TestTwo%2Fsub"} {
		_, err := os.Stat(filepath.Join(w.dir, "bl/ex%2Fa", f))
		assert.NoError(t, err, f)
	}
	code, out, _ = w.check("--config", w.config(""), results)
	assert.Equal(t, exitOK, code, out)

	// Any other finding (here a vanished test beside a new one) writes nothing and drops nothing.
	w.file("bl/ex%2Fa/TestGone", "")
	added := goGreen + `{"Action":"run","Package":"ex/a","Test":"TestNew"}
{"Action":"pass","Package":"ex/a","Test":"TestNew"}
`
	code, out, _ = w.check("--config", w.config(""), "--write-baseline", w.file("added.json", added))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED vanished ex/a\tTestGone")
	assert.Contains(t, out, "nothing written: --write-baseline adds entries only to a run whose only findings are unlisted\n")
	_, err := os.Stat(filepath.Join(w.dir, "bl/ex%2Fa/TestNew"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(w.dir, "bl/ex%2Fa/TestGone"))
	assert.NoError(t, err)
}

func TestBaselineEntriesMustBeRegisteredSuitesTests(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.file("bl/ex%2Fold/TestOld", "")
	w.file("bl/s.tests", "TestOne\n")
	w.file("bl/ex%2Fa/sub/x", "")
	code, out, _ := w.check("--config", w.config(""), w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED baseline bl/ex%2Fold: is not the directory of a registered suite;")
	assert.Contains(t, out, "RED baseline bl/s.tests: is not the directory of a registered suite;")
	assert.Contains(t, out, "RED baseline bl/ex%2Fa/sub: is not a test entry")
}

func TestMissingSuiteIsNamedOnceNotPerBaselineTest(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.file("bl/ex%2Fb/TestB1", "")
	w.file("bl/ex%2Fb/TestB2", "")
	code, out, _ := w.check("--config", w.config("suite\tex/b\n"), w.file("go.json", goGreen))
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED missing-suite ex/b")
	assert.NotContains(t, out, "vanished")
}

func TestEscapeMakesOneSegmentEveryFilesystemHolds(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"..": "%2E%2E", ".": "%2E", "a/b": "a%2Fb", "50%": "50%25", "x y": "x%20y", "v1.2": "v1.2",
		"codec::tests::roundtrip": "codec%3A%3Atests%3A%3Aroundtrip", `a<b>c"d|e?f*g\h`: "a%3Cb%3Ec%22d%7Ce%3Ff%2Ag%5Ch",
		"ends.": "ends%2E", "ends ": "ends%20", "CON": "%43ON", "nul.txt": "%6Eul.txt", "COM1": "%43OM1", "CONSOLE": "CONSOLE",
		"~home": "%7Ehome", "#count": "%23count",
	} {
		assert.Equal(t, want, escape(name), name)
		assert.Equal(t, name, display(want), name)
		assert.True(t, validEntry(want), name)
	}
	long := escape(strings.Repeat("a b", 100))
	assert.LessOrEqual(t, len(long), maxEntry)
	assert.Regexp(t, `^(a%20b)+(a|a%20)?~[0-9a-f]{16}$`, long, "cut at a whole %XX, then the hash")
	assert.True(t, validEntry(long))
	assert.NotEqual(t, long, escape(strings.Repeat("a b", 101)), "a hash tells long names apart")
	for _, bad := range []string{".gitkeep2", "a b", "a:b", "%2e", "CON", "x~zz"} {
		assert.False(t, validEntry(bad), bad)
	}
}

func TestCountSuiteFloor(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	cfg := w.config("suite\tcards\n")
	logs := func(n int) []string {
		return []string{w.file("go.json", goGreen), w.file("cards.log", fmt.Sprintf("seeding\ncards: ran %d, failed 0 \n\n", n))}
	}
	code, out, _ := w.check(append([]string{"--config", cfg}, logs(40)...)...)
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED unlisted cards: ran 40 tests by its count line, and the baseline holds no floor for it; fix: write 40 into bl/cards/#count in this change\n")
	code, out, _ = w.check(append([]string{"--config", cfg, "--write-baseline"}, logs(40)...)...)
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "WROTE: 1 baseline entries")
	data, err := os.ReadFile(filepath.Join(w.dir, "bl/cards/#count"))
	require.NoError(t, err)
	assert.Equal(t, "40\n", string(data))
	code, out, _ = w.check(append([]string{"--config", cfg}, logs(41)...)...)
	assert.Equal(t, exitOK, code, out)
	code, out, _ = w.check(append([]string{"--config", cfg}, logs(1)...)...)
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED vanished cards: ran 1 tests by its count line, and its floor is 40;")
	w.file("bl/cards/#count", "zero\n")
	w.file("bl/ex%2Fa/#count", "3\n")
	code, out, _ = w.check(append([]string{"--config", cfg}, logs(40)...)...)
	assert.Equal(t, exitFindings, code)
	assert.Contains(t, out, "RED baseline bl/cards/#count: does not hold a count of 1 or more;")
	assert.Contains(t, out, "RED baseline bl/ex%2Fa/#count: is a count floor, and the suite reported no count line;")
}

func TestGitkeepIsNeverAnEntry(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.file("bl/.gitkeep", "")
	w.file("bl/ex%2Fa/.gitkeep", "")
	code, out, _ := w.check("--config", w.config(""), w.file("go.json", goGreen))
	assert.Equal(t, exitOK, code, out)
}
