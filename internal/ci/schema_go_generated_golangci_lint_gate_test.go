package ci

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The make fragment is the gate. These tests pin what a reader cannot hold by
// hand: the checker is pinned, the three named linters are the ones enabled,
// a missing tool is reported rather than passed over, and the planted bad
// pattern actually turns the target red.

const golangciGateMake = "make/schema-go-generated-golangci-lint-gate.mk"

func TestGolangciLintGatePinsCheckerAndLinters(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), golangciGateMake))
	if err != nil {
		t.Fatalf("the card's make fragment is missing: %v", err)
	}
	mk := string(data)
	for _, want := range []string{
		"GOLANGCI_LINT_VERSION ?= 2.12.2",
		"- errcheck",
		"- gosec",
		"- ineffassign",
		"- govet",
		"tool missing: golangci-lint",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("%s does not name %q — the lens would be wider, narrower or silent than the card says", golangciGateMake, want)
		}
	}
}

func TestGolangciLintGateNegativeControl(t *testing.T) {
	t.Parallel()
	requireGolangciLint(t)
	runGolangciGate(t, "schema-go-generated-golangci-lint-gate-negative-control", "negative control")
}

func TestGolangciLintGateFastPasses(t *testing.T) {
	t.Parallel()
	requireGolangciLint(t)
	runGolangciGate(t, "schema-go-generated-golangci-lint-gate", "golangci-lint gate")
}

func requireGolangciLint(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		// The make target reports "tool missing: golangci-lint" and fails; a Go
		// unit test may not install a fleet tool, so it does not pretend a pass.
		t.Skipf("golangci-lint is not installed on this machine (the gate itself refuses with 'tool missing'): %v", err)
	}
}

func runGolangciGate(t *testing.T, target, want string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", "--no-print-directory", target)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make %s: %v\n%s", target, err, out)
	}
	if !strings.Contains(string(out), want) {
		t.Fatalf("make %s did not report %q:\n%s", target, want, out)
	}
}
