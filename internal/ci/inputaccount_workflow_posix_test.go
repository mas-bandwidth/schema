//go:build !windows

package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

func TestFastInputAccountingKeepsBothCanaries(t *testing.T) {
	t.Parallel()
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
	// the recipe ends where the job's next step begins
	script, _, ok = strings.Cut(script, "\n\n      - ")
	if !ok {
		t.Fatal("the end of the input-accounting recipe is missing")
	}
	for _, tc := range []struct {
		family, leg, control, shard string
		want                        string
	}{
		{"conformance-corpus", "lobster", "", "", "-family conformance-corpus -leg lobster -check-extra VERIFY_JOBS=4\n" +
			"-family conformance-corpus -leg lobster -canary wrong VERIFY_JOBS=4\n"},
		// a squirrel shard row accounts its share and records it; the merge
		// job holds the union, and the sibling row runs the wrong control
		{"conformance-corpus", "squirrel", "", "2/3", "-family conformance-corpus -leg squirrel -check-extra -shard 2/3 -opened-out RUNNER_TEMP/input-shard/record.json VERIFY_JOBS=4\n"},
		{"conformance-corpus", "squirrel", "wrong", "", "-family conformance-corpus -leg squirrel -canary wrong VERIFY_JOBS=4\n"},
		{"packet-defaults", "lobster", "", "", "-family packet-defaults -leg lobster\n" +
			"-family packet-defaults -leg lobster -canary extra\n" +
			"-family packet-defaults -leg lobster -canary wrong\n"},
		{"packet-defaults", "squirrel", "", "", "-family packet-defaults -leg squirrel\n" +
			"-family packet-defaults -leg squirrel -canary extra\n" +
			"-family packet-defaults -leg squirrel -canary wrong\n"},
	} {
		t.Run(tc.family+"/"+tc.leg+"/"+tc.control+"/"+tc.shard, func(t *testing.T) {
			dir := t.TempDir()
			tc.want = strings.ReplaceAll(tc.want, "RUNNER_TEMP", dir)
			bin := filepath.Join(dir, "inputaccount")
			stub := "#!/bin/sh\nprintf '%s\\n' \"$*\"\n"
			if err := os.WriteFile(bin, []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			testinputs.Files(t, ".github/workflows/ci-fast.yml")
			cmd := exec.Command("bash", "-eo", "pipefail", "-c", "vars=()\n"+script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "RUNNER_TEMP="+dir, "FAMILY="+tc.family, "LEG="+tc.leg, "ACCOUNT_CONTROL="+tc.control, "SHARD="+tc.shard)
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != tc.want {
				t.Fatalf("accounting arguments = %q, %v; want %q", out, err, tc.want)
			}
		})
	}
}

func TestFastPlanSplitsSquirrelCorpusAccountingRows(t *testing.T) {
	t.Parallel()
	data := readCI(t, repoRoot(t), ".github/workflows/ci-fast.yml")
	_, plan, ok := strings.Cut(data, "# Keep Squirrel's independent wrong-input control within its own job budget.\n")
	line, _, found := strings.Cut(plan, "\n")
	if !ok || !found {
		t.Fatal("the input-family matrix split is missing")
	}
	const corpusRow = `{"family":"conformance-corpus","leg":"squirrel"}`
	const otherRows = `{"family":"conformance-corpus","leg":"lobster"},{"family":"packet-defaults","leg":"squirrel"}`
	for _, tc := range []struct{ name, rows, want string }{
		{"empty", "[]", "[]"},
		{"unselected", "[" + otherRows + "]", "[" + otherRows + "]"},
		{"selected", "[" + corpusRow + "," + otherRows + "]",
			"[" + corpusRow + "," + otherRows + `,{"family":"conformance-corpus","leg":"squirrel","account_control":"wrong"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testinputs.Files(t, ".github/workflows/ci-fast.yml")
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", line+"\nprintf '%s\\n' \"$input_rows\"")
			cmd.Env = append(os.Environ(), "input_rows="+tc.rows)
			out, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != tc.want {
				t.Fatalf("input-family matrix = %q, %v; want %s", out, err, tc.want)
			}
		})
	}
}

func TestFastPlanSelectsConformanceHandoffChanges(t *testing.T) {
	t.Parallel()
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
		name, files, dart, cpp, wantPackage string
	}{
		{"handoff helper", "make/conformance-handoff.py\n.github/workflows/ci-fast.yml\n", "true", "true", ""},
		{"C++ driver", "test/conformance/cpp/main.cpp\n", "false", "true", "./test/conformance/harness"},
		{"Dart driver build", "make/dart.mk\n", "true", "false", ""},
		{"Dart driver imports", "test/conformance/dart/main.dart\n", "true", "false", "./test/conformance/harness"},
		{"accounting runtime", "tools/inputaccount/main.go\ntools/inputaccount/watch_linux_test.go\n", "true", "true", ""},
		{"accounting test only", "tools/inputaccount/watch_linux_test.go\n", "false", "false", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "diff-files.txt"), []byte(tc.files), 0o600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "outputs")
			testinputs.Files(t, ".github/workflows/ci-fast.yml", "make/conformance-handoff.py")
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", start+script)
			cmd.Dir = root
			cmd.Env = append(scriptEnv(os.Environ()), "RUNNER_TEMP="+dir, "GITHUB_OUTPUT="+output, "all=no", "packages=",
				"make_shared=no", "touched_legs=", "clean_legs=", "clean_legs_json=[]")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("input-family selector failed: %v\n%s", err, out)
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			outputs := map[string]string{}
			for line := range strings.Lines(string(got)) {
				k, v, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
				outputs[k] = v
			}
			packages := outputs["packages"]
			if outputs["dart_corpus_account"] != tc.dart || outputs["cpp_corpus_account"] != tc.cpp || outputs["input_rows"] != "[]" ||
				outputs["corpus_shards"] != "[]" || outputs["elixir_corpus_account"] != "false" {
				t.Fatalf("input-family plan = %q, want dart=%s cpp=%s and no generic rows", got, tc.dart, tc.cpp)
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
