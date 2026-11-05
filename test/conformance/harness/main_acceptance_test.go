package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestOriginalNineFlagCannotSilentlyWeakenAcceptance(t *testing.T) {
	const probe = "SCHEMA_TEST_ORIGINAL_NINE_FLAG"
	if os.Getenv(probe) == "1" {
		os.Args = []string{"harness", "run", "--require-original-nine"}
		main()
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOriginalNineFlagCannotSilentlyWeakenAcceptance$")
	cmd.Env = append(os.Environ(), probe+"=1")
	cmd.Dir = conformanceRoot(t)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "--require-original-nine is only valid with coverage, the unified full-implementation gate") {
		t.Fatalf("full-implementation flag silently weakened or failed for another reason: %v\n%s", err, out)
	}
}
