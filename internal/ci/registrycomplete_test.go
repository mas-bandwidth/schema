package ci

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestRegistryCoversEveryTarget holds the language registry's lists (Makefile,
// THE LANGUAGE REGISTRY) to all 18 targets: each port is on each list, or its
// make/<lang>.mk declares that list not applicable to it and says why
// (REGISTRY_NA, REGISTRY_NA_WHY.<list>.<lang>). A list that quietly covers
// some of the targets is the drift this gate exists to refuse.
func TestRegistryCoversEveryTarget(t *testing.T) {
	t.Parallel()
	out, err := registryComplete(t)
	if err != nil {
		t.Fatalf("make registry-complete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "registry complete: 18 targets") {
		t.Fatalf("make registry-complete did not report 18 targets:\n%s", out)
	}
}

// The reversed witnesses: each breaks the registry one way on the command line
// and requires the gate to refuse it by name.
func TestRegistryCompleteNegativeControls(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"a declaration dropped", []string{"REGISTRY_NA="}, "goldens:go is on neither"},
		{"a reason dropped", []string{"REGISTRY_NA_WHY.toolchain.rust="}, "toolchain:rust is declared not applicable with no reason"},
		{"a planted port", []string{"SCHEMA_PORTS=c cpp cs dart elixir go goose java js lobster lua luau moo python ruby rust squirrel unity zz"}, "test:zz is on neither"},
		{"a stale declaration", []string{"REGISTRY_NA=goldens:c " + registryNA(t)}, "goldens:c is on the list and also declared not applicable"},
		{"a declaration naming no port", []string{"REGISTRY_NA=goldens:zz " + registryNA(t)}, "goldens:zz names no list or no port"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := registryComplete(t, c.args...)
			if err == nil {
				t.Fatalf("make registry-complete stayed green:\n%s", out)
			}
			if !strings.Contains(out, c.want) {
				t.Fatalf("make registry-complete failed without naming %q:\n%s", c.want, out)
			}
		})
	}
}

func registryComplete(t *testing.T, args ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("this gate reads the make registry: %v", err)
	}
	cmd := exec.Command("make", append([]string{"--no-print-directory", "-s", "registry-complete"}, args...)...)
	cmd.Dir = repoRootDir(t)
	cmd.Env = append(os.Environ(), "SCHEMA_SKIP_LEGS=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// registryNA is the registry's own declaration list, so a witness can add one
// entry to it rather than replace it.
func registryNA(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("this gate reads the make registry: %v", err)
	}
	cmd := exec.Command("make", "--no-print-directory", "-s", "registry-na")
	cmd.Dir = repoRootDir(t)
	cmd.Env = append(os.Environ(), "SCHEMA_SKIP_LEGS=")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("make registry-na: %v", err)
	}
	return strings.TrimSpace(string(out))
}
