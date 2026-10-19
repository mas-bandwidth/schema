package ci

// The Java signedness lens's wiring, held hermetically. The checker itself runs
// nightly over the whole generated corpus (make/schema-java-generated-checker-
// signedness.mk and its own workflow): it is a minutes-long instrument, not a
// unit test. What rides every `go test` here is the part a reader can get wrong
// — the pin, the target names, the corpus roots, the probe's refusal, the
// registered negative control and the nightly trigger — so the expensive run
// cannot quietly become a green line without a tool.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const signednessMakeFile = "make/schema-java-generated-checker-signedness.mk"

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(body)
}

// TestSchemaJavaGeneratedCheckerSignednessGateWiring pins the pieces the gate
// is made of: a pinned Checker Framework version, the Signedness processor, the
// five corpus roots the Java generate targets already own, and the refusal to
// certify an unannotated tree.
func TestSchemaJavaGeneratedCheckerSignednessGateWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	body := readRepoFile(t, root, signednessMakeFile)
	for _, want := range []string{
		"CHECKER_FRAMEWORK_VERSION ?= 4.3.1",
		"org.checkerframework.checker.signedness.SignednessChecker",
		"org.checkerframework.checker.signedness.qual.Unsigned",
		"tool missing: checker-framework",
		"generated/java/.stamp",
		"generated/java-ludicrous/.stamp",
		"generated/bench/java/.stamp",
		"build/tables-generated-java/.stamp",
		"build/java-clean-packet/.stamp",
		"REFUSES a vacuous pass",
		"schema-java-generated-checker-signedness-negative-control",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s does not carry %q", signednessMakeFile, want)
		}
	}
}

// TestSchemaJavaGeneratedCheckerSignednessIsNotAPort holds the lens out of the
// wide-scalar refusal gate's port registry: that gate discovers every
// make/*.mk as a live compiler backend, and this fragment is a lens. Without
// the exclusion the registry runs `schema generate --lang schema-java-...` and
// fails as an unregistered backend, reddening a gate the lens never touched.
func TestSchemaJavaGeneratedCheckerSignednessIsNotAPort(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	body := readRepoFile(t, root, "Makefile")
	if !strings.Contains(body, "$(filter-out make/schema-%.mk,$(wildcard make/*.mk))") {
		t.Errorf("tables-ports-refuse-wide-scalars does not filter the schema-* lens namespace out of its registry")
	}
}

// TestSchemaJavaGeneratedCheckerSignednessProbeNamesAMissingTool requires the
// probe to report by name and exit non-zero when the pin does not resolve. A
// probe that passed over an absent tool would make the nightly green over a
// corpus nothing looked at.
func TestSchemaJavaGeneratedCheckerSignednessProbeNamesAMissingTool(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	missing := filepath.Join(t.TempDir(), "no-checker-framework-here")
	cmd := exec.Command("make", "-s", "-C", root,
		"schema-java-generated-checker-signedness-tool",
		"CHECKER_FRAMEWORK_HOME="+missing)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the probe passed with CHECKER_FRAMEWORK_HOME=%s; output:\n%s", missing, out)
	}
	if !strings.Contains(string(out), "tool missing: checker-framework") {
		t.Fatalf("the probe refused without naming the missing tool; output:\n%s", out)
	}
	if !strings.Contains(string(out), missing) {
		t.Errorf("the probe did not name the path it looked in (%s); output:\n%s", missing, out)
	}
}

// TestSchemaJavaGeneratedCheckerSignednessControlIsRegistered holds the
// planted-pattern control in the negative-control manifest: it must be either
// in a group or in the exclusion list with a written reason. It is excluded
// from the shared leg because that leg installs no Checker Framework; this
// lens's own workflow runs it, so the control is not a silent skip.
func TestSchemaJavaGeneratedCheckerSignednessControlIsRegistered(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var m struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Targets []string `json:"targets"`
		} `json:"groups"`
		Excluded []struct {
			Target string `json:"target"`
			Reason string `json:"reason"`
		} `json:"excluded"`
	}
	if err := json.Unmarshal([]byte(readRepoFile(t, root, "make/negative-controls.json")), &m); err != nil {
		t.Fatalf("parse make/negative-controls.json: %v", err)
	}
	const target = "schema-java-generated-checker-signedness-negative-control"
	for _, g := range m.Groups {
		for _, got := range g.Targets {
			if got == target {
				if g.When != "nightly" {
					t.Errorf("%s is in the %q tier; the checker over the corpus is minutes and belongs nightly", target, g.When)
				}
				return
			}
		}
	}
	for _, e := range m.Excluded {
		if e.Target == target {
			if strings.TrimSpace(e.Reason) == "" {
				t.Errorf("%s is excluded with no reason", target)
			}
			return
		}
	}
	t.Fatalf("%s is not in make/negative-controls.json", target)
}

// TestSchemaJavaGeneratedCheckerSignednessWorkflowTiers pins WHEN the lens runs:
// the fast hermetic wiring on push and pull request, and the heavy checker on a
// schedule and workflow_dispatch, never on every push.
func TestSchemaJavaGeneratedCheckerSignednessWorkflowTiers(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	body := readRepoFile(t, root, ".github/workflows/schema-java-generated-checker-signedness.yml")
	for _, want := range []string{
		"pull_request:",
		"push:",
		"schedule:",
		"workflow_dispatch:",
		"schema-java-generated-checker-signedness-tool",
		"schema-java-generated-checker-signedness-negative-control",
		"schema-java-generated-checker-signedness",
		"internal/ci",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the signedness workflow does not carry %q", want)
		}
	}
}
