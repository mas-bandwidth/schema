package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// The accounting consumer also runs Dart in JIT mode and needs the generated
// source trees imported by the driver.
func TestDartConformanceHandoffIncludesGeneratedImports(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	driver := readCI(t, root, "test/conformance/dart/main.dart")
	helper := readCI(t, root, "make/dart-aot-handoff.py")
	_, paths, ok := strings.Cut(helper, "\nBUILD_PATHS = tuple(\n")
	if !ok {
		t.Fatal("handoff helper has no BUILD_PATHS manifest")
	}
	paths, _, ok = strings.Cut(paths, "\n)\n")
	if !ok {
		t.Fatal("handoff helper has no end to its BUILD_PATHS manifest")
	}
	imports := regexp.MustCompile(`(?m)^import '\.\./\.\./\.\./build/([^/]+)/`).FindAllStringSubmatch(driver, -1)
	if len(imports) == 0 {
		t.Fatal("Dart conformance driver has no generated imports")
	}
	checked := make(map[string]bool)
	for _, match := range imports {
		dir := match[1]
		if !checked[dir] && !strings.Contains(paths, `"`+dir+`"`) {
			t.Errorf("Dart handoff manifest is missing imported build directory %s", dir)
		}
		checked[dir] = true
	}
}

func TestFastInputAccountingKeepsBothCanaries(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the Linux input accounting lane runs its recipe in a POSIX host's Bash")
	}
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, job, ok := strings.Cut(string(data), "\n  input-family:\n")
	if !ok {
		t.Fatal("the input-family job is missing")
	}
	_, script, ok := strings.Cut(job, "          vars=()\n")
	if !ok {
		t.Fatal("the input-accounting recipe is missing its variables")
	}
	script, _, ok = strings.Cut(script, "\n      # A SKIP NAMES A REASON")
	if !ok {
		t.Fatal("the end of the input-accounting recipe is missing")
	}
	for _, tc := range []struct {
		family string
		want   string
	}{
		{"conformance-corpus", "-family conformance-corpus -leg lobster -check-extra VERIFY_JOBS=4\n" +
			"-family conformance-corpus -leg lobster -canary wrong VERIFY_JOBS=4\n"},
		{"packet-defaults", "-family packet-defaults -leg lobster\n" +
			"-family packet-defaults -leg lobster -canary extra\n" +
			"-family packet-defaults -leg lobster -canary wrong\n"},
	} {
		t.Run(tc.family, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "inputaccount")
			stub := "#!/bin/sh\nprintf '%s\\n' \"$*\"\n"
			if err := os.WriteFile(bin, []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", "vars=()\n"+script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "FAMILY="+tc.family, "LEG=lobster")
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != tc.want {
				t.Fatalf("accounting arguments = %q, %v; want %q", out, err, tc.want)
			}
		})
	}
}

// The producer runs for either existing Dart verification or corpus
// accounting. Only corpus accounting runs the dependent consumer.
func TestDartConformanceHandoffSelection(t *testing.T) {
	t.Parallel()
	wf := workflows(t, repoRoot(t))["ci-fast.yml"]
	for _, tc := range []struct {
		name, legs, account, headRepo string
		produce, consume              bool
	}{
		{"unrelated", `["go"]`, "false", "mas-bandwidth/schema", false, false},
		{"verify only", `["dart"]`, "false", "mas-bandwidth/schema", true, false},
		{"corpus only", `[]`, "true", "mas-bandwidth/schema", true, true},
		{"verify and account", `["dart"]`, "true", "mas-bandwidth/schema", true, true},
		{"fork", `["dart"]`, "true", "fork/schema", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := ciEvent{
				Name: "pull_request", Branch: "main", HeadRef: "topic",
				Repo: "mas-bandwidth/schema", HeadRepo: tc.headRepo,
				PlanVerifyLegs: tc.legs, PlanDartCorpus: tc.account,
			}
			for job, want := range map[string]bool{
				"verify-dart-conformance": tc.produce, "input-family-dart-conformance": tc.consume,
			} {
				if got, err := jobRunsInWorkflow(wf, job, event); got != want || err != nil {
					t.Errorf("%s returned %t, %v, want %t", job, got, err, want)
				}
			}
		})
	}
}

func TestFastPlanSelectsDartHandoffChanges(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the Linux input accounting lane runs its recipe in a POSIX host's Bash")
	}
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "\n          # THE INPUT FAMILIES (tools/inputfamilies, 2026-11-22).")
	if !ok {
		t.Fatal("input-family plan is missing")
	}
	start := "\n          if [ \"$all\" = yes ]; then\n"
	_, script, ok := strings.Cut(section, start)
	if !ok {
		t.Fatal("input-family selector is missing")
	}
	script, _, ok = strings.Cut(script, "\n          # THE NIGHTLY GATES'")
	if !ok {
		t.Fatal("the end of the input-family selector is missing")
	}
	for _, tc := range []struct {
		name, files, want, wantPackage string
	}{
		{"handoff helper", "make/dart-aot-handoff.py\n.github/workflows/ci-fast.yml\n", "true", ""},
		{"Dart driver build", "make/dart.mk\n", "true", ""},
		{"Dart driver imports", "test/conformance/dart/main.dart\n", "true", "./test/conformance/harness"},
		{"accounting runtime", "tools/inputaccount/main.go\ntools/inputaccount/watch_linux_test.go\n", "true", ""},
		{"accounting test only", "tools/inputaccount/watch_linux_test.go\n", "false", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "diff-files.txt"), []byte(tc.files), 0o600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "outputs")
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", start+script)
			cmd.Dir = root
			cmd.Env = append(scriptEnv(os.Environ()), "RUNNER_TEMP="+dir, "GITHUB_OUTPUT="+output, "all=no", "packages=")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("input-family selector failed: %v\n%s", err, out)
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			packages, ok := strings.CutPrefix(string(got), "dart_corpus_account="+tc.want+"\npackages=")
			packages, rowsOK := strings.CutSuffix(packages, "\ninput_rows=[]\n")
			if !ok || !rowsOK {
				t.Fatalf("input-family plan = %q, want account=%s and no generic rows", got, tc.want)
			}
			if tc.wantPackage == "" && packages != "" {
				t.Errorf("input-family plan added packages %q", packages)
			}
			if tc.wantPackage != "" && !slices.Contains(strings.Fields(packages), tc.wantPackage) {
				t.Errorf("input-family plan packages %q omitted %s", packages, tc.wantPackage)
			}
		})
	}
}
