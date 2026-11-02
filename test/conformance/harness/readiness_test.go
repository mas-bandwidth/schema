package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReadinessFixture(t *testing.T, dir string) {
	t.Helper()
	for i, lang := range originalNine {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("lane-%d", i), "readiness-"+lang+".txt"), "pass\n")
	}
}

func fakeRoadmapChecker(t *testing.T, code int) string {
	t.Helper()
	needShell(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "argv.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + fmt.Sprintf("%q", log) + "\n" +
		"test -f go.mod || exit 92\n" +
		"test \"$#\" -eq 6 || exit 93\n" +
		"test \"$1\" = --script || exit 94\n" +
		"test \"$2\" = tools/roadmap/render.lisp || exit 95\n" +
		"test \"$3\" = --check || exit 96\n" +
		"test \"$4\" = --require-original-nine || exit 97\n" +
		"test \"$5\" = docs/roadmap.sexp || exit 98\n" +
		"test \"$6\" = ROADMAP.md || exit 99\n" +
		"echo ROADMAP_CHECKER_DIAGNOSTIC\n" +
		fmt.Sprintf("exit %d\n", code)
	writeExec(t, filepath.Join(dir, "sbcl"), script)
	t.Setenv("PATH", dir)
	return log
}

func TestReadinessReceiptsRequireEveryOriginalLanguage(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		damage     func(*testing.T, string)
	}{
		{"complete", "", func(*testing.T, string) {}},
		{"missing cpp", "cpp: missing readiness", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "lane-0", "readiness-cpp.txt")); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing port", "elixir: missing readiness", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "lane-8", "readiness-elixir.txt")); err != nil {
				t.Fatal(err)
			}
		}},
		{"duplicate pass", "duplicate readiness receipt for cpp", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "another", "readiness-cpp.txt"), "pass")
		}},
		{"duplicate fail", "duplicate readiness receipt for cpp", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "another", "readiness-cpp.txt"), "fail")
		}},
		{"failed", "runtime acceptance requires pass", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "lane-1", "readiness-c.txt"), "fail\n")
		}},
		{"invalid", "runtime acceptance requires pass", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "lane-1", "readiness-c.txt"), "pass\nfail\n")
		}},
		{"empty", "runtime acceptance requires pass", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "lane-1", "readiness-c.txt"), "")
		}},
		{"whitespace", "runtime acceptance requires pass", func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, "lane-1", "readiness-c.txt"), " pass ")
		}},
		{"directory", "must be a regular file", func(t *testing.T, dir string) {
			path := filepath.Join(dir, "lane-1", "readiness-c.txt")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", "must be a regular file", func(t *testing.T, dir string) {
			path := filepath.Join(dir, "lane-1", "readiness-c.txt")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(dir, "lane-0", "readiness-cpp.txt"), path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeReadinessFixture(t, dir)
			tc.damage(t, dir)
			problems := readinessProblems(dir)
			if tc.want == "" && len(problems) != 0 || tc.want != "" && !strings.Contains(strings.Join(problems, "\n"), tc.want) {
				t.Fatalf("wanted %q; got %v", tc.want, problems)
			}
		})
	}
}

func TestOriginalNineCoverageRequiresRoadmapAndRuntimeAcceptance(t *testing.T) {
	t.Chdir("../../..")
	rows, fixed := completeCoverage(realCoverageShape(t))
	for _, tc := range []struct {
		name     string
		checker  int
		receipts bool
		want     string
	}{
		{"all obligations", 0, true, ""},
		{"green counts red roadmap", 1, true, "Original-nine roadmap: FAIL"},
		{"green counts missing runtime", 0, false, "cpp: missing readiness receipt"},
		{"both missing", 1, false, "Original-nine runtime acceptance: FAIL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := fakeRoadmapChecker(t, tc.checker)
			dir := t.TempDir()
			writeCoverageFixture(t, dir, rows, fixed)
			if tc.receipts {
				writeReadinessFixture(t, dir)
			}
			var out bytes.Buffer
			err := coverageReportWithImplementationCheck(&out, dir, defaultManifest, defaultJSONDir, defaultReports, true, func(*Manifest) []string { return nil })
			if (err != nil) != (tc.want != "") {
				t.Fatalf("err=%v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), "ROADMAP_CHECKER_DIAGNOSTIC") {
				t.Fatalf("lost diagnostic %q:\n%s", tc.want, out.String())
			}
			if tc.want != "" && strings.Contains(out.String(), "PASS: every original-nine") {
				t.Fatalf("incomplete readiness claimed success:\n%s", out.String())
			}
			argv, err := os.ReadFile(log)
			if err != nil || string(argv) != "--script\ntools/roadmap/render.lisp\n--check\n--require-original-nine\ndocs/roadmap.sexp\nROADMAP.md\n" {
				t.Fatalf("checker command changed: %q %v", argv, err)
			}
		})
	}
}

func TestRoadmapCheckerFindsRepositoryFromHarnessDirectory(t *testing.T) {
	fakeRoadmapChecker(t, 0)
	var out bytes.Buffer
	if err := checkOriginalNineRoadmap(&out); err != nil {
		t.Fatalf("nested checkout discovery: %v\n%s", err, out.String())
	}
}

func TestCoverageMissingSBCLFailsClosedButReportOnlyDoesNotCheck(t *testing.T) {
	t.Chdir("../../..")
	rows, fixed := completeCoverage(realCoverageShape(t))
	dir := t.TempDir()
	writeCoverageFixture(t, dir, rows, fixed)
	writeReadinessFixture(t, dir)
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	if err := coverageReportWithImplementationCheck(&out, dir, defaultManifest, defaultJSONDir, defaultReports, true, func(*Manifest) []string { return nil }); err == nil ||
		!strings.Contains(out.String(), "sbcl") || !strings.Contains(out.String(), "Original-nine roadmap: FAIL") {
		t.Fatalf("missing checker passed or hid reason: %v\n%s", err, out.String())
	}
	writeFile(t, filepath.Join(dir, "readiness-cpp.txt"), "fail")
	out.Reset()
	if err := coverageReport(&out, dir, "", "", "", false); err != nil ||
		strings.Contains(out.String(), "roadmap") || strings.Contains(out.String(), "runtime acceptance") {
		t.Fatalf("report-only acquired readiness requirements: %v\n%s", err, out.String())
	}
}

func TestCoverageImplementationFailureCannotBeOverriddenByReceipts(t *testing.T) {
	t.Chdir("../../..")
	fakeRoadmapChecker(t, 0)
	rows, fixed := completeCoverage(realCoverageShape(t))
	dir := t.TempDir()
	writeCoverageFixture(t, dir, rows, fixed)
	writeReadinessFixture(t, dir)
	var out bytes.Buffer
	err := coverageReportWithImplementationCheck(&out, dir, defaultManifest, defaultJSONDir, defaultReports, true,
		func(*Manifest) []string { return []string{"required feature witness missing"} })
	if err == nil || !strings.Contains(out.String(), "required feature witness missing") ||
		!strings.Contains(out.String(), "Original-nine runtime acceptance: PASS") ||
		!strings.Contains(out.String(), "Original-nine roadmap: PASS") ||
		strings.Contains(out.String(), "PASS: every original-nine") {
		t.Fatalf("green subset masked incomplete implementation: %v\n%s", err, out.String())
	}
}
