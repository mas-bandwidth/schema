//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/testinputs"
)

// The runner must hand both successful and failed harness exits to the scorer.
func TestRunnerScoresHarnessExit(t *testing.T) {
	runner, err := filepath.Abs("../../make/checks/wire-fuzz-release-run.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build/conformance-harness"), []byte("#!/bin/sh\nprintf 'fixture log\\n'\nexit \"$HARNESS_STATUS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	scorer := `#!/bin/sh
while [ "$#" -gt 1 ]; do
	if [ "$1" = -status ]; then
		printf '%s\n' "$2"
		exit 0
	fi
	shift
done
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "scorer"), []byte(scorer), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, status := range []string{"0", "42"} {
		t.Run(status, func(t *testing.T) {
			testinputs.Files(t, runner)
			cmd := exec.Command("bash", runner, "go", "plain", "1", "1", "rows")
			cmd.Dir = dir
			cmd.Env = append(cmd.Environ(), "WFR_TOOL=./scorer", "WFR_DRIVER=fixture", "WFR_FLAGS=", "WFR_ENV=", "HARNESS_STATUS="+status)
			out, err := cmd.CombinedOutput()
			if err != nil || string(out) != status+"\n" {
				t.Fatalf("runner exited with %v and passed %q to the scorer, want %q", err, out, status+"\n")
			}
			log, err := os.ReadFile(filepath.Join(dir, "rows/go.plain.1.log"))
			if err != nil || string(log) != "fixture log\n" {
				t.Fatalf("captured harness log = %q, err = %v", log, err)
			}
		})
	}
}
