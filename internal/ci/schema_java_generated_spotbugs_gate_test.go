package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// spotbugsTool locates the pinned tool the gate names. It FAILS when the tool
// is absent rather than skipping: a gate line that skips is not a pass.
func spotbugsTool(t *testing.T) (home, plugin string) {
	t.Helper()
	home = os.Getenv("SPOTBUGS_HOME")
	if home == "" {
		t.Fatalf("tool missing: spotbugs (set SPOTBUGS_HOME to the pinned SpotBugs %s install)", "4.10.5")
	}
	if _, err := os.Stat(filepath.Join(home, "bin", "spotbugs")); err != nil {
		t.Fatalf("tool missing: spotbugs at %s: %v", filepath.Join(home, "bin", "spotbugs"), err)
	}
	plugin = os.Getenv("SPOTBUGS_PLUGIN")
	if plugin == "" {
		plugin = filepath.Join(home, "findsecbugs-plugin.jar")
	}
	if _, err := os.Stat(plugin); err != nil {
		t.Fatalf("tool missing: find-sec-bugs at %s: %v", plugin, err)
	}
	return home, plugin
}

// TestSchemaJavaSpotbugsGateNegativeControl holds the gate's own blade to the
// plant. The target compiles a scratch class carrying `new Random()` and runs
// the SAME SpotBugs invocation the gate certifies with; it must go red and
// name PREDICTABLE_RANDOM. The second half runs that invocation over a clean
// class and requires green, so the control proves both directions and not just
// "the tool told us something".
func TestSchemaJavaSpotbugsGateNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Java SpotBugs gate")
	home, plugin := spotbugsTool(t)
	root := repoRoot(t)

	cmd := exec.Command("make", "-C", root,
		"schema-java-generated-spotbugs-gate-negative-control",
		"SPOTBUGS_HOME="+home, "SPOTBUGS_PLUGIN="+plugin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the negative-control target failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PREDICTABLE_RANDOM") {
		t.Fatalf("the target went green without naming the plant:\n%s", out)
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "SpotbugsClean.java")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("public final class SpotbugsClean {\n    public static int token() { return 7; }\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("javac", "--release", "17", "-nowarn", "-d", classes, source).CombinedOutput(); err != nil {
		t.Fatalf("compiling the clean plant: %v\n%s", err, out)
	}
	findings := filepath.Join(dir, "findings.xml")
	run := exec.Command(filepath.Join(home, "bin", "spotbugs"),
		"-textui", "-effort:max", "-medium", "-exitcode", "-xml:withMessages",
		"-include", filepath.Join(root, "build", "schema-java-spotbugs", "include.xml"),
		"-exclude", filepath.Join(root, "build", "schema-java-spotbugs", "exclude.xml"),
		"-pluginList", plugin, "-output", findings, classes)
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("the clean class did not stay green: %v\n%s", err, out)
	}
	data, err := os.ReadFile(findings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PREDICTABLE_RANDOM") {
		t.Fatal("the clean class still named PREDICTABLE_RANDOM")
	}
}
