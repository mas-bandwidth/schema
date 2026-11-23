package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
