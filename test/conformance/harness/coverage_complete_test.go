package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func completeCoverage(shape map[coverageKey]*coverageExpected) ([]coverageRow, map[string]string) {
	var rows []coverageRow
	fixed := map[string]string{}
	for _, lang := range originalNine {
		for key, want := range shape {
			named := want.namedRequired[lang]
			rows = append(rows, coverageRow{Lang: lang, Direction: directionReadsCpp,
				Surface: key.surface, Class: key.class, Cases: want.cases, Answered: want.cases - named, Named: named})
		}
		if lang != referenceLang {
			fixed[lang] = "pass"
		}
	}
	return rows, fixed
}

func smallCoverageShape() map[coverageKey]*coverageExpected {
	// Named wire refusal is required for Go and allowed for every writer.
	return map[coverageKey]*coverageExpected{
		{"wire", "fixed"}:    {cases: 3, namedAllowed: 1, namedRequired: map[string]int{"go": 1}},
		{"wire", "variable"}: {cases: 2},
		{"block", ""}:        {cases: 1},
	}
}

func TestOriginalNineCoverageRejectsIncompleteRecords(t *testing.T) {
	shape := smallCoverageShape()
	for _, tc := range []struct {
		name, diagnostic string
		damage           func([]coverageRow, map[string]string) []coverageRow
	}{
		{"missing language", "elixir /", func(r []coverageRow, _ map[string]string) []coverageRow {
			return slices.DeleteFunc(r, func(v coverageRow) bool { return v.Lang == "elixir" })
		}},
		{"missing canonical", "cpp / wire (fixed): missing", func(r []coverageRow, _ map[string]string) []coverageRow {
			return slices.DeleteFunc(r, func(v coverageRow) bool { return v.Lang == "cpp" && v.Surface == "wire" && v.Class == "fixed" })
		}},
		{"missing surface", "go / block", func(r []coverageRow, _ map[string]string) []coverageRow {
			return slices.DeleteFunc(r, func(v coverageRow) bool { return v.Lang == "go" && v.Surface == "block" })
		}},
		{"missing class", "go / wire (variable): missing", func(r []coverageRow, _ map[string]string) []coverageRow {
			return slices.DeleteFunc(r, func(v coverageRow) bool { return v.Lang == "go" && v.Class == "variable" })
		}},
		{"duplicate canonical", "duplicate coverage row", func(r []coverageRow, _ map[string]string) []coverageRow {
			return append(r, r[0])
		}},
		{"absent", "surface is absent", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].SurfaceAbsent = true; return r }},
		{"failed", "incomplete (failed=1", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Answered--; r[0].Failed++; return r }},
		{"unclaimed absence", "not-carried=1", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Answered--; r[0].NotCarried++; return r }},
		{"claimed absence", "claimed-absent=1", func(r []coverageRow, _ map[string]string) []coverageRow {
			r[0].Answered--
			r[0].AbsentClaimed++
			return r
		}},
		{"wrong count", "manifest requires", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Cases++; r[0].Answered++; return r }},
		{"inconsistent total", "counters do not sum", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Answered--; return r }},
		{"negative count", "counters do not sum", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Failed = -1; return r }},
		{"overflowing count", "counters do not sum", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Answered = int(^uint(0) >> 1); return r }},
		{"unknown surface", "unexpected surface/class", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Surface = "made-up"; return r }},
		{"wrong direction", "missing required", func(r []coverageRow, _ map[string]string) []coverageRow { r[0].Direction = directionCppReads; return r }},
		{"missing fixed form", "fixed form requires pass", func(r []coverageRow, f map[string]string) []coverageRow { delete(f, "java"); return r }},
		{"failed fixed form", "got \"fail\"", func(r []coverageRow, f map[string]string) []coverageRow { f["java"] = "fail"; return r }},
		{"uncarried fixed form", "got \"none\"", func(r []coverageRow, f map[string]string) []coverageRow { f["java"] = "none"; return r }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, fixed := completeCoverage(shape)
			problems := originalCoverageProblems(tc.damage(rows, fixed), fixed, shape)
			if !strings.Contains(strings.Join(problems, "\n"), tc.diagnostic) {
				t.Fatalf("missing refusal %q: %v", tc.diagnostic, problems)
			}
		})
	}
}

func TestOriginalNineCoverageNamedRefusalsAndExpansion(t *testing.T) {
	shape := smallCoverageShape()
	rows, fixed := completeCoverage(shape)
	rows = append(rows, coverageRow{Lang: "lua", Direction: directionReadsCpp, Surface: "wire", SurfaceAbsent: true})
	fixed["lua"] = "none"
	// An optional named wire refusal is valid for the reference too.
	for i := range rows {
		if rows[i].Lang == "cpp" && rows[i].Class == "fixed" {
			rows[i].Answered--
			rows[i].Named++
		}
	}
	if problems := originalCoverageProblems(rows, fixed, shape); len(problems) != 0 {
		t.Fatalf("valid refusals/optional expansion rejected: %v", problems)
	}
	for i := range rows {
		if rows[i].Lang == "go" && rows[i].Class == "fixed" {
			rows[i].Named = 0
			rows[i].Answered = rows[i].Cases
		}
	}
	if problems := originalCoverageProblems(rows, fixed, shape); !strings.Contains(strings.Join(problems, "\n"), "named refusals=0") {
		t.Fatalf("lost required refusal accepted: %v", problems)
	}
	rows, fixed = completeCoverage(shape)
	for i := range rows {
		if rows[i].Surface == "block" {
			rows[i].Named = rows[i].Cases
			rows[i].Answered = 0
			break
		}
	}
	if problems := originalCoverageProblems(rows, fixed, shape); !strings.Contains(strings.Join(problems, "\n"), "manifest permits 0..0") {
		t.Fatalf("invented named refusal accepted: %v", problems)
	}
}

func realCoverageShape(t *testing.T) map[coverageKey]*coverageExpected {
	t.Helper()
	m, err := ReadManifest(defaultManifest, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	reports, err := readReports(defaultReports)
	if err != nil {
		t.Fatal(err)
	}
	shape, err := requiredCoverageShape(m, reports, defaultJSONDir)
	if err != nil {
		t.Fatal(err)
	}
	// Every registered surface, including a zero-case one, must be named.
	for _, surface := range surfaces {
		count := 0
		for key := range shape {
			if key.surface == surface {
				count++
			}
		}
		if count == 0 {
			t.Fatalf("missing manifest surface %s", surface)
		}
	}
	return shape
}

func writeCoverageFixture(t *testing.T, dir string, rows []coverageRow, fixed map[string]string) {
	t.Helper()
	byLang := map[string][]coverageRow{}
	for _, row := range rows {
		byLang[row.Lang] = append(byLang[row.Lang], row)
	}
	for lang, some := range byLang {
		raw, err := json.Marshal(some)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "coverage-"+lang+".json"), string(raw))
	}
	for lang, result := range fixed {
		writeFile(t, filepath.Join(dir, "fixedform-"+lang+".txt"), result)
	}
}

func TestOriginalNineCoverageManifestReportAndRefusals(t *testing.T) {
	t.Chdir("../../..")
	shape := realCoverageShape(t)
	rows, fixed := completeCoverage(shape)
	dir := t.TempDir()
	writeCoverageFixture(t, dir, rows, fixed)
	var out bytes.Buffer
	if err := coverageReport(&out, dir, defaultManifest, defaultJSONDir, defaultReports, true); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "PASS: every original-nine") || !strings.Contains(out.String(), "+") {
		t.Fatalf("completion/refusals not reported: %s", out.String())
	}
	// Remove all C++ rows. Ports cannot define their own smaller denominator.
	if err := os.Remove(filepath.Join(dir, "coverage-cpp.json")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := coverageReport(&out, dir, defaultManifest, defaultJSONDir, defaultReports, true); err == nil {
		t.Fatal("missing canonical artifact passed")
	}
	if !strings.Contains(out.String(), "| surface |") || !strings.Contains(out.String(), "cpp /") {
		t.Fatalf("failure lost the ordinary table or diagnostics: %s", out.String())
	}
	// The ordinary report remains useful and does not enforce completion.
	out.Reset()
	if err := coverageReport(&out, dir, "", "", "", false); err != nil {
		t.Fatalf("informational mode became a completion gate: %v", err)
	}
}

func TestCoverageReportKeepsReadableRowsAfterDamagedInput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "coverage-aa.json"), "{broken")
	writeFile(t, filepath.Join(dir, "coverage-go.json"), `[{"lang":"go","direction":"reads-cpp","surface":"wire","cases":1,"answered":1}]`)
	writeFile(t, filepath.Join(dir, "fixedform-go.txt"), "pass")
	writeFile(t, filepath.Join(dir, "duplicate", "fixedform-go.txt"), "none")
	var out bytes.Buffer
	err := coverageReport(&out, dir, "", "", "", false)
	if err == nil || !strings.Contains(out.String(), "| wire | 1/1 |") ||
		!strings.Contains(out.String(), "duplicate fixed-form") || !strings.Contains(out.String(), "coverage-aa.json") {
		t.Fatalf("damaged input lost evidence or passed: %v\n%s", err, out.String())
	}
}

func TestOriginalNineCoverageWorkflowReportsBeforeFailure(t *testing.T) {
	needShell(t)
	raw, err := os.ReadFile("../../../.github/workflows/nightly-conformance.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "      - name: the coverage table\n")
	if start < 0 {
		t.Fatal("nightly coverage step missing")
	}
	text = text[start:]
	start = strings.Index(text, "        run: |\n")
	if start < 0 {
		t.Fatal("nightly coverage command missing")
	}
	text = text[start+len("        run: |\n"):]
	before, _, ok := strings.Cut(text, "      - uses:")
	if !ok {
		t.Fatal("nightly report upload missing")
	}
	var lines []string
	for line := range strings.SplitSeq(before, "\n") {
		lines = append(lines, strings.TrimPrefix(line, "          "))
	}
	script := strings.Join(lines, "\n")
	dir := t.TempDir()
	// Stand in for a missing lane: the harness gate fails, but ordinary
	// reporting remains successful. Other tests exercise the real verdict.
	writeExec(t, filepath.Join(dir, "bin", "go"), "#!/bin/sh\necho COVERAGE_DIAGNOSTIC\nfor arg in \"$@\"; do\n  if [ \"$arg\" = --require-original-nine ]; then exit 1; fi\ndone\nexit 0\n")
	summary := filepath.Join(dir, "summary.md")
	for _, guarded := range []bool{true, false} {
		run := script
		if !guarded {
			run = strings.ReplaceAll(run, "--require-original-nine", "")
		}
		cmd := exec.Command("bash", "-c", run)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin")+":"+os.Getenv("PATH"), "GITHUB_STEP_SUMMARY="+summary)
		out, err := cmd.CombinedOutput()
		if (err != nil) != guarded {
			t.Fatalf("guarded=%t exit=%v output=%s", guarded, err, out)
		}
		if !strings.Contains(string(out), "COVERAGE_DIAGNOSTIC") {
			t.Fatalf("failed gate hid stdout: %s", out)
		}
		data, err := os.ReadFile(summary)
		if err != nil || !strings.Contains(string(data), "COVERAGE_DIAGNOSTIC") {
			t.Fatalf("failed gate hid job summary: %s %v", data, err)
		}
	}
}

func TestCoverageReportAcceptsMixedArbitraryNamedFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "coverage-bundle.json"), `[{"lang":"cpp","direction":"reads-cpp","surface":"wire","cases":1,"answered":1},{"lang":"go","direction":"reads-cpp","surface":"wire","cases":1,"answered":1}]`)
	var out bytes.Buffer
	if err := coverageReport(&out, dir, "", "", "", false); err != nil {
		t.Fatalf("informational mixed-file report changed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "| surface | cpp | go |") {
		t.Fatalf("mixed rows lost: %s", out.String())
	}
}
