package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// spotbugsHome locates the pinned SpotBugs the gate names. The certifying
// gate is the make target (run by the card's own workflow on fleet runners,
// where a missing tool is a red "tool missing" line). This Go test also rides
// ci-full's SCHEMA_SLOW=1 runs, some of them on GitHub-hosted runners without
// the fleet's ~/sdk, so there a missing tool skips with the reason, and
// SCHEMA_REQUIRE_SPOTBUGS=1 turns that skip into a failure.
func spotbugsHome(t *testing.T) string {
	t.Helper()
	host, _ := os.Hostname()
	home := os.Getenv("SPOTBUGS_HOME")
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		home = filepath.Join(dir, "sdk", "spotbugs-4.10.5")
	}
	for _, p := range []string{filepath.Join(home, "bin", "spotbugs"), filepath.Join(home, "findsecbugs-plugin.jar")} {
		if _, err := os.Stat(p); err != nil {
			if os.Getenv("SCHEMA_REQUIRE_SPOTBUGS") == "1" {
				t.Fatalf("tool missing: %s on %s: %v", p, host, err)
			}
			t.Skipf("tool missing: %s on %s: %v (SCHEMA_REQUIRE_SPOTBUGS=1 makes this a failure)", p, host, err)
		}
	}
	return home
}

// TestSchemaJavaSpotbugsGateNegativeControl holds the gate to a plant in a REAL
// generated class: the negative-control target runs the corpus target itself
// (its find, javac and spotbugs steps) with generated/java/Clauses.java swapped
// for a copy carrying `new java.util.Random()` and `d == Double.NaN`, and must
// go red naming both. The second half runs the same corpus target unplanted
// over the same narrowed corpus and requires green, so the control proves both
// directions on the same path.
func TestSchemaJavaSpotbugsGateNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Java SpotBugs gate")
	home := spotbugsHome(t)
	root := repoRoot(t)

	out, err := exec.Command("make", "-C", root, "--no-print-directory",
		"schema-java-generated-spotbugs-gate-negative-control",
		"SPOTBUGS_HOME="+home).CombinedOutput()
	if err != nil {
		t.Fatalf("the negative-control target failed: %v\n%s", err, out)
	}
	for _, want := range []string{"RED on PREDICTABLE_RANDOM", "RED on FE_TEST_IF_EQUAL_TO_NOT_A_NUMBER"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("the control did not report %q:\n%s", want, out)
		}
	}

	work := filepath.Join(t.TempDir(), "clean")
	out, err = exec.Command("make", "-C", root, "--no-print-directory",
		"schema-java-generated-spotbugs-gate", "SPOTBUGS_HOME="+home,
		"SPOTBUGS_WORK="+work, "SPOTBUGS_CORPUS=generated/java",
		"SPOTBUGS_STAMPS=generated/java/.stamp", "SPOTBUGS_MIN_CLASSES=9").CombinedOutput()
	if err != nil {
		t.Fatalf("the unplanted generated/java did not stay green: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "PLANTED") {
		t.Fatalf("the clean run planted:\n%s", out)
	}
}
