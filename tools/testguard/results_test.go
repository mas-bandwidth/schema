package main

import (
	"bytes"
	"strings"
	"testing"
)

func read(t *testing.T, name, body string) results {
	t.Helper()
	f, path, err := format(name)
	require.NoError(t, err)
	r := results{}
	require.NoError(t, formats[f](r, path, []byte(body)))
	return r
}

func refusedWith(t *testing.T, name, body, want string) {
	t.Helper()
	f, path, err := format(name)
	require.NoError(t, err)
	err = formats[f](results{}, path, []byte(body))
	var r *refusal
	require.ErrorAs(t, err, &r, name)
	assert.Contains(t, r.what, want)
}

func TestFormatIsChosenByExtensionOrPrefix(t *testing.T) {
	t.Parallel()
	for arg, want := range map[string][2]string{
		"a/b.xml": {"junit", "a/b.xml"}, "go.JSON": {"gojson", "go.JSON"}, "go.jsonl": {"gojson", "go.jsonl"},
		"t.tap": {"tap", "t.tap"}, "c.log": {"count", "c.log"}, "c.txt": {"count", "c.txt"}, "c.out": {"count", "c.out"},
		"junit:report": {"junit", "report"}, "tap:x.log": {"tap", "x.log"}, "count:x.xml": {"count", "x.xml"},
	} {
		f, path, err := format(arg)
		require.NoError(t, err, arg)
		assert.Equal(t, want, [2]string{f, path}, arg)
	}
	_, _, err := format("report")
	assert.ErrorContains(t, err, "report has no format")
}

func TestJUnitSuitesCasesSkipsAndFailures(t *testing.T) {
	t.Parallel()
	r := read(t, "out/junit.xml", `<?xml version="1.0"?>
<testsuites>
  <testsuite name="cards">
    <testcase classname="cards" name="reads"/>
    <testcase classname="cards.Writer" name="writes"><failure message="x"/></testcase>
    <testcase name="crashes"><error message="segv"/></testcase>
    <testcase name="pending"><skipped/></testcase>
    <testcase name="off" status="disabled"/>
  </testsuite>
  <testsuite name="outer"><testsuite name="inner"><testcase name="deep"/></testsuite></testsuite>
  <testsuite name="none"></testsuite>
</testsuites>
<!-- a trailing comment is fine -->`)
	assert.Equal(t, map[string]bool{"reads": true, "cards.Writer.writes": true, "crashes": true}, r["cards"].ran)
	assert.Equal(t, map[string]bool{"cards.Writer.writes": true, "crashes": true}, r["cards"].failed)
	assert.Equal(t, map[string]bool{"pending": true, "off": true}, r["cards"].skipped)
	assert.Equal(t, map[string]bool{"deep": true}, r["inner"].ran)
	assert.Nil(t, r["outer"])
	assert.Equal(t, 0, r["none"].tests())
}

func TestJUnitEveryRootIsRead(t *testing.T) {
	t.Parallel()
	r := read(t, "lua-cards.xml", `<testsuite><testcase name="a"/></testsuite>
<testsuite name="s2"><testcase name="b"><skipped/></testcase></testsuite>`)
	assert.Equal(t, 1, r["lua-cards"].tests())
	assert.True(t, r["s2"].skipped["b"])
}

func TestJUnitRefusals(t *testing.T) {
	t.Parallel()
	refusedWith(t, "r.xml", `<testsuite name="s"><testcase name="t">`, "r.xml is not JUnit XML")
	refusedWith(t, "r.xml", `<testsuite name="s"/> harness died`, `r.xml has text outside its JUnit roots: "harness died"`)
	refusedWith(t, "r.xml", `<testsuite name="s"/><<<`, "r.xml is not JUnit XML")
	refusedWith(t, "r.xml", `<html/>`, "r.xml has a root <html>")
	refusedWith(t, "r.xml", `  `, "r.xml holds no <testsuites> or <testsuite>")
	refusedWith(t, "r.xml", `<testsuite name="s"><testcase name=""/><testcase name="t"/></testsuite>`, "r.xml has a <testcase> with no name")
	refusedWith(t, "r.xml", `<testsuites><testsuite name="o"><testsuite name="i"><testcase/></testsuite></testsuite></testsuites>`, "r.xml has a <testcase> with no name")
}

func TestGoJSONTestsSubtestsSkipsFailuresAndEmptyPackages(t *testing.T) {
	t.Parallel()
	r := read(t, "go.json", goGreen+`{"Action":"skip","Package":"ex/empty"}
{"Action":"run","Package":"ex/crash","Test":"TestCrash"}
{"Action":"fail","Package":"ex/crash"}
`)
	assert.Equal(t, map[string]bool{"TestOne": true, "TestTwo": true, "TestTwo/sub": true}, r["ex/a"].ran)
	assert.Equal(t, map[string]bool{"TestSkipped": true}, r["ex/a"].skipped)
	assert.Empty(t, r["ex/a"].failed)
	assert.Equal(t, 0, r["ex/empty"].tests())
	assert.True(t, r["ex/crash"].failed["TestCrash"], "a test that started and never ended failed")
	assert.Len(t, r["ex/crash"].problems, 1)
}

func TestGoJSONEveryIterationCounts(t *testing.T) {
	t.Parallel()
	r := read(t, "go.json", `{"Action":"run","Package":"s","Test":"TestA"}
{"Action":"skip","Package":"s","Test":"TestA"}
{"Action":"run","Package":"s","Test":"TestA"}
{"Action":"pass","Package":"s","Test":"TestA"}
{"Action":"run","Package":"s","Test":"TestB"}
{"Action":"pass","Package":"s","Test":"TestB"}
{"Action":"run","Package":"s","Test":"TestB"}
`)
	assert.True(t, r["s"].skipped["TestA"])
	assert.True(t, r["s"].ran["TestA"])
	assert.True(t, r["s"].failed["TestB"], "the second iteration never ended")
}

func TestGoJSONRefusals(t *testing.T) {
	t.Parallel()
	refusedWith(t, "go.json", goGreen+"FAIL ex/a 0.01s\n", "go.json:11 is not a go test -json event")
	refusedWith(t, "go.json", `{"Action":"pass","Package":"s","Te`+"\n", "go.json:1 is not a go test -json event")
	refusedWith(t, "go.json", `{"Package":"s"}`+"\n", "go.json:1 is not a go test -json event")
	refusedWith(t, "go.json", "\n", "go.json holds no go test -json event")
}

func TestTAPDescriptionsNumbersDirectivesAndFailures(t *testing.T) {
	t.Parallel()
	r := read(t, "t/wire.tap", `TAP version 14
1..7
ok 1 - encodes
not ok 2 - decodes
ok 3 # SKIP no codec
not ok 4 - pending thing # TODO later
ok 5 - b # SKIPPED no db
ok 6 - c # skipping: no db
ok 7
`)
	assert.Equal(t, map[string]bool{"encodes": true, "decodes": true, "7": true}, r["wire"].ran)
	assert.Equal(t, map[string]bool{"decodes": true}, r["wire"].failed)
	assert.Equal(t, map[string]bool{"3": true, "pending thing": true, "b": true, "c": true}, r["wire"].skipped)
	assert.Empty(t, r["wire"].problems)
}

func TestTAPSubtests(t *testing.T) {
	t.Parallel()
	r := read(t, "s.tap", `TAP version 14
# Subtest: a
    ok 1 - inner # SKIP no gpu
    not ok 2 - broken
    1..2
ok 1 - a
    ok 1 - headerless
    1..1
ok 2 - b
1..2
`)
	assert.Equal(t, map[string]bool{"a/inner": true}, r["s"].skipped)
	assert.Equal(t, map[string]bool{"a": true, "a/broken": true, "b": true, "b/headerless": true}, r["s"].ran)
	assert.Equal(t, map[string]bool{"a/broken": true}, r["s"].failed)
	assert.Empty(t, r["s"].problems)
}

func TestTAPIncompleteRuns(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]string{
		"1..5\nok 1 - a\n":                                       "the plan is 5 tests and 1 were reported",
		"ok 1 - a\n":                                             "the file has no plan line (1..N)",
		"1..2\nok 1 - a\nok 1 - a\n":                             "test number 1 is reported twice",
		"1..10\nok 1 - a\nBail out! db down\n":                   "the run bailed out: Bail out! db down",
		"1..1\n# Subtest: a\n    ok 1 - x\nok 1 - a\n":           "subtest a has no plan line",
		"1..1\n# Subtest: a\n    1..3\n    ok 1 - x\nok 1 - a\n": "subtest a planned 3 tests and reported 1",
	} {
		r := read(t, "s.tap", body)
		assert.Contains(t, strings.Join(r["s"].problems, "\n"), want, body)
	}
	refusedWith(t, "s.tap", "1..1\nok\n", "s.tap:2 is a test line with neither a number nor a description")
}

func TestTAPSecondPlanIsIncomplete(t *testing.T) {
	t.Parallel()
	r := read(t, "s.tap", "1..3\nok - a\n1..1\n")
	assert.Contains(t, strings.Join(r["s"].problems, "\n"), "a second plan line at line 3")
	assert.Contains(t, strings.Join(r["s"].problems, "\n"), "the plan is 3 tests and 1 were reported")
}

func TestTAPYAMLBlocksAreNotTAP(t *testing.T) {
	t.Parallel()
	r := read(t, "s.tap", "TAP version 13\n1..2\nok 1 - a\n  ---\n  ok: true\n  1..1\n  not ok 9 - x\n  ...\nok 2 - b\n")
	assert.Equal(t, map[string]bool{"a": true, "b": true}, r["s"].ran)
	assert.Empty(t, r["s"].problems)
	r = read(t, "s.tap", "1..1\nok 1 - a\n  ---\n  message: cut off\n")
	assert.Contains(t, strings.Join(r["s"].problems, "\n"), "the YAML block opened at line 3 never ends")
}

func TestTAPLineAtAnUnknownIndentIsRefused(t *testing.T) {
	t.Parallel()
	refusedWith(t, "s.tap", "1..1\n\tok 1 - i # SKIP no gpu\n\tnot ok 2 - j\nok 1 - a\n", `s.tap:2 is a TAP line indented by "\t"`)
	refusedWith(t, "s.tap", "1..1\n# Subtest: a\n  ok 1 - i\n  1..1\nok 1 - a\n", `s.tap:3 is a TAP line indented by "  "`)
	r := read(t, "s.tap", "1..1\n  some harness chatter\nok 1 - a\n")
	assert.Equal(t, map[string]bool{"a": true}, r["s"].ran, "a line that is not TAP may sit at any indent")
}

func TestCountLineEndsTheLog(t *testing.T) {
	t.Parallel()
	r := read(t, "logs/schema_test.log", "schema_test: seeding 412 vectors\nOK\nschema_test: ran 412, failed 0 \n\n")
	assert.Equal(t, 412, r["schema_test"].tests())
	assert.True(t, r["schema_test"].countOnly)
	assert.Len(t, r, 1)
	r = read(t, "logs/packet.log", "cards (lua): ran 7, failed 0\nran 3, failed 1\r\n")
	assert.Equal(t, 3, r["packet"].tests(), "only the last line counts")
	assert.Equal(t, 1, r["packet"].countFailed)
	assert.Nil(t, r["cards (lua)"])
}

func TestCountLogThatEndsOtherwiseIsRefused(t *testing.T) {
	t.Parallel()
	refusedWith(t, "cards.log", "ran 40, failed 0\nharness: segfault\n", `cards.log:2, its last line, is not a "[SUITE: ]ran N, failed M" line`)
	refusedWith(t, "cards.log", "we ran 9, failed 0 today\n", "cards.log:1, its last line, is not")
	refusedWith(t, "cards.log", "ok, building cards\n", "cards.log:1, its last line, is not")
	refusedWith(t, "cards.log", "\n \n", `cards.log holds no "ran N, failed M" line`)
}

func TestCLIHelpAndRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args      []string
		code      int
		err       string
		outPrefix string
	}{
		{args: nil, outPrefix: "testguard: fails a CI run"},
		{args: []string{"help"}, outPrefix: "testguard: fails a CI run"},
		{args: []string{"-h"}, outPrefix: "testguard: fails a CI run"},
		{args: []string{"help", "check"}, outPrefix: "testguard check: holds"},
		{args: []string{"check", "-h"}, outPrefix: "testguard check: holds"},
		{args: []string{"check", "--help"}, outPrefix: "testguard check: holds"},
		{args: []string{"help", "-h"}, code: 2, err: "testguard: help takes no flags and one topic, check; got \"-h\"; run: go run ./tools/testguard help\n"},
		{args: []string{"nope"}, code: 2, err: "testguard: \"nope\" is no verb; the verbs are check and help; run: go run ./tools/testguard help\n"},
		{args: []string{"check", "--no-such-flag"}, code: 2, err: "testguard check: unknown flag --no-such-flag; run: go run ./tools/testguard check -h\n"},
		{args: []string{"check", "x.json", "--config"}, code: 2, err: "testguard check: --config needs a value; run: go run ./tools/testguard check -h\n"},
		{args: []string{"check"}, code: 2, err: "testguard check: wants --config and result files (none were read, and reading none is never a pass); run: go run ./tools/testguard check -h\n"},
		{args: []string{"check", "--config", "/nonexistent/c.tsv", "r.xml"}, code: 2, err: "testguard check: reading the config: open /nonexistent/c.tsv: no such file or directory; run: ls -l /nonexistent/c.tsv\n"},
	} {
		var so, se bytes.Buffer
		code := run("", c.args, &so, &se, nil)
		name := strings.Join(c.args, " ")
		assert.Equal(t, c.code, code, name)
		if c.err != "" {
			assert.Equal(t, c.err, se.String(), name)
			assert.Empty(t, so.String(), name)
			continue
		}
		assert.Empty(t, se.String(), name)
		assert.True(t, strings.HasPrefix(so.String(), c.outPrefix), "%s: %q", name, so.String())
	}
}

func TestFlagsMayFollowTheResultFiles(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	code, out, errOut := w.check(w.file("go.json", goGreen), "--config", w.config(""))
	assert.Equal(t, exitOK, code, out+errOut)
}
