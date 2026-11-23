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
