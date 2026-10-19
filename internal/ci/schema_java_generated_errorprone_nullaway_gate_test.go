package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJavaGeneratedErrorProneNullAwayGate holds the two things a reader cannot
// hold by hand: the tool pin and the planted control. The static half always
// runs (the Makefile pins Error Prone and NullAway by version and sha256, and
// the workflow keeps the corpus on the schedule), so a deleted pin is red on
// every `go test ./internal/ci`. The functional half runs the planted control
// and is armed by SCHEMA_JAVA_CHECKER_GATE=1, which the card's nightly workflow
// sets: the control needs the pinned jars (a network fetch on a cold cache) and
// a JDK, so it is not a unit test.
func TestJavaGeneratedErrorProneNullAwayGate(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-java-generated-errorprone-nullaway-gate.mk"))
	if err != nil {
		t.Fatalf("reading the gate's make file: %v", err)
	}
	text := string(mk)
	for _, want := range []string{
		"JAVA_CHECKER_EP_VERSION  := 2.36.0",
		"JAVA_CHECKER_NA_VERSION  := 0.12.3",
		"error_prone_core/2.36.0/error_prone_core-2.36.0.jar:",
		"nullaway/0.12.3/nullaway-0.12.3.jar:",
		"-Xep:NullAway:ERROR",
		"tool missing: error-prone",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("make/schema-java-generated-errorprone-nullaway-gate.mk no longer pins %q", want)
		}
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-java-generated-errorprone-nullaway-gate.yml"))
	if err != nil {
		t.Fatalf("reading the gate's workflow: %v", err)
	}
	wft := string(wf)
	for _, want := range []string{
		"schedule:",
		"workflow_dispatch:",
		"java-generated-errorprone-nullaway-gate-negative-control",
		"java-generated-errorprone-nullaway-gate",
	} {
		if !strings.Contains(wft, want) {
			t.Errorf("the gate's workflow no longer runs %q", want)
		}
	}

	if os.Getenv("SCHEMA_JAVA_CHECKER_GATE") != "1" {
		t.Log("planted control not armed: set SCHEMA_JAVA_CHECKER_GATE=1 (the nightly workflow does)")
		return
	}
	cmd := exec.Command("make", "java-generated-errorprone-nullaway-gate-negative-control")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the planted control did not go red-and-green as required: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "[NullAway]") {
		t.Fatalf("the planted control passed without naming NullAway:\n%s", out)
	}
}
