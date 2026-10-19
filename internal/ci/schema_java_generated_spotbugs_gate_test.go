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
// line is the make target, run per push and nightly by the card's own workflow
// on the fleet schema runners. This Go wrapper costs ~75 s, so it runs only
// when SCHEMA_REQUIRE_SPOTBUGS=1 asks for it (ci-full SCHEMA_SLOW runs, some on
// GitHub-hosted runners without the fleet's ~/sdk, do not pay it); once asked,
// a missing tool is a failure, never a skip.
func spotbugsHome(t *testing.T) string {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_SPOTBUGS") != "1" {
		t.Skip("SCHEMA_REQUIRE_SPOTBUGS=1 runs the Java SpotBugs control here; the card workflow runs the make target itself")
	}
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
			t.Fatalf("tool missing: %s on %s: %v", p, host, err)
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

	scratch := t.TempDir()
	out, err := exec.Command("make", "-C", root, "--no-print-directory",
		"schema-java-generated-spotbugs-gate-negative-control",
		"SPOTBUGS_HOME="+home, "SPOTBUGS_NC_WORK="+filepath.Join(scratch, "nc")).CombinedOutput()
	if err != nil {
		t.Fatalf("the negative-control target failed: %v\n%s", err, out)
	}
	for _, want := range []string{"RED on PREDICTABLE_RANDOM", "RED on FE_TEST_IF_EQUAL_TO_NOT_A_NUMBER"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("the control did not report %q:\n%s", want, out)
		}
	}

	work := filepath.Join(scratch, "clean")
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
