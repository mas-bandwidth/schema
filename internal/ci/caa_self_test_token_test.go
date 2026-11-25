package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// caaRefusesWithoutToken runs the caa-self-test recipe of mk with no
// GITHUB_TOKEN and a python3 that only says it ran, so nothing reaches
// GitHub: the recipe must fail, naming GITHUB_TOKEN, before python3 runs. It
// returns why it does not, or "".
func caaRefusesWithoutToken(t *testing.T, mk string) string {
	t.Helper()
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Fatalf("make is not on PATH: %v", err)
	}
	bin := t.TempDir()
	stub := "#!/bin/sh\necho caa-python-ran\n"
	if err := os.WriteFile(filepath.Join(bin, "python3"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(makePath, "-s", "-f", mk, "caa-self-test")
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + cmd.Dir}
	out, err := cmd.CombinedOutput()
	switch {
	case strings.Contains(string(out), "caa-python-ran"):
		return "the recipe ran the self-test with no GITHUB_TOKEN: " + string(out)
	case err == nil:
		return "the recipe passed with no GITHUB_TOKEN: " + string(out)
	case !strings.Contains(string(out), "GITHUB_TOKEN"):
		return "the recipe failed without naming GITHUB_TOKEN: " + string(out)
	}
	return ""
}

// TestCAASelfTestFailsNamingTheMissingToken: tooling.caa.self-test (make
// caa-self-test) reads GitHub, so with no GITHUB_TOKEN it fails by that name
// before it reads anything. The plant is the recipe with its token guard
// removed: it then runs on, and the check names that.
func TestCAASelfTestFailsNamingTheMissingToken(t *testing.T) {
	t.Parallel()
	mk := filepath.Join(repoRoot(t), "make", "caa-self-test.mk")
	data, err := os.ReadFile(mk)
	if err != nil {
		t.Fatal(err)
	}
	if why := caaRefusesWithoutToken(t, mk); why != "" {
		t.Errorf("%s: %s", mk, why)
	}
	var kept []string
	for _, l := range strings.Split(string(data), "\n") {
		if !strings.Contains(l, `test -n "$$GITHUB_TOKEN"`) {
			kept = append(kept, l)
		}
	}
	planted := strings.Join(kept, "\n")
	if planted == string(data) {
		t.Fatal("the recipe has no token guard for the plant to remove")
	}
	pmk := filepath.Join(t.TempDir(), "caa-self-test.mk")
	if err := os.WriteFile(pmk, []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}
	if why := caaRefusesWithoutToken(t, pmk); !strings.Contains(why, "ran the self-test with no GITHUB_TOKEN") {
		t.Errorf("the recipe with its guard removed is not named (got %q)", why)
	}
}
