package ci

import (
	"os/exec"
	"strings"
	"testing"
)

// TestVulnRecipeRunsThePinnedScannerSingular guards the vulnerability gate's
// Makefile entry (issue #1402). `make vuln` is the pull-request gate and must
// run the pinned govulncheck scanner directly, not `tools/vuln/govulncheck.sh
// run`: that script's bare `run` form forgives an unreachable database, and a
// PR-tier gate that cannot go red on an outage is a gate that has silently
// stopped watching — the one verdict that changes with no commit here is a
// new CVE, so the gate is the one place an outage must never pass as green.
//
// The target must also be defined exactly once. A second `vuln:` recipe is
// dead under GNU make's last-definition rule, and every `make` in the tree
// rides an `overriding recipe` warning while the recipe the author wrote is
// never run.
func TestVulnRecipeRunsThePinnedScannerSingular(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("vuln recipe check requires make: %v", err)
	}
	root := repoRoot(t)

	cmd := exec.Command("make", "-n", "vuln")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n vuln: %v\n%s", err, out)
	}
	dry := string(out)

	if strings.Contains(dry, "tools/vuln/govulncheck.sh run") {
		t.Errorf("`make -n vuln` runs the forgiving script, which forgives an unreachable database and lets the PR-tier gate pass an outage green (issue #1402):\n%s", dry)
	}
	if !strings.Contains(dry, "golang.org/x/vuln/cmd/govulncheck@") {
		t.Errorf("`make -n vuln` does not run the pinned govulncheck scanner (issue #1402):\n%s", dry)
	}
	if strings.Contains(dry, "overriding recipe") || strings.Contains(dry, "overriding commands") {
		t.Errorf("`make` warns of a duplicate recipe — a second `vuln:` recipe is dead under GNU make's last-definition rule (issue #1402):\n%s", dry)
	}
}
