package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The C generated checker lens is a make fragment plus a workflow, and both are
// easy to gut silently: a suppression added without a reason, a cppcheck target
// moved onto the push lane, or a negative control that stops being registered.
// These tests hold those properties, and the last one runs the control so the
// planted line is known to fire rather than merely to be written down.

const (
	cLensMakeFile = "make/schema-c-generated-weverything-cppcheck-gate.mk"
	cLensWorkflow = ".github/workflows/schema-c-generated-weverything-cppcheck-gate.yml"
	cLensControl  = "tables-c-weverything-negative-control"
)

func readAt(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// Every -Wno flag the lens passes to clang must be named in a comment with a
// reason. The list above the variable is the review; a flag without a line
// there is a blanket disable nobody argued for.
func TestSchemaCGeneratedWeverythingSuppressionsAreJustified(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	makefile := readAt(t, root, cLensMakeFile)

	_, block, ok := strings.Cut(makefile, "C_GENERATED_SUPPRESSIONS :=")
	if !ok {
		t.Fatalf("%s has no C_GENERATED_SUPPRESSIONS variable", cLensMakeFile)
	}
	block, _, _ = strings.Cut(block, "\n\n")

	flag := regexp.MustCompile(`-Wno-[a-zA-Z0-9-]+`)
	flags := flag.FindAllString(block, -1)
	if len(flags) == 0 {
		t.Fatalf("%s passes no reviewed suppressions", cLensMakeFile)
	}
	seen := map[string]bool{}
	for _, f := range flags {
		if seen[f] {
			continue
		}
		seen[f] = true
		if !regexp.MustCompile(`(?m)^# ` + regexp.QuoteMeta(f) + `:`).MatchString(makefile) {
			t.Errorf("%s is suppressed with no reviewed reason: add a '# %s: <why>' line above the list", f, f)
		}
	}

	for _, want := range []string{"-Weverything", "-Werror"} {
		if !strings.Contains(block, want) && !strings.Contains(makefile, "C_GENERATED_WARNINGS := -std=c99 "+want) {
			if !strings.Contains(makefile, want) {
				t.Errorf("the lens no longer asks clang for %s", want)
			}
		}
	}
}

// The corpus is the leg's own generation stamps, not a second list of schemas.
func TestSchemaCGeneratedWeverythingUsesTheLegsCorpus(t *testing.T) {
	t.Parallel()
	makefile := readAt(t, repoRoot(t), cLensMakeFile)
	for _, stamp := range []string{
		"generated/c/.stamp",
		"generated/c-ludicrous/.stamp",
		"build/tables-generated-c/.stamp",
		"generated/bench/c/.stamp",
		"generated/bench/tables/c/.stamp",
		"generated/bench/paired/c/.stamp",
	} {
		if !strings.Contains(makefile, stamp) {
			t.Errorf("the lens does not depend on the leg's own %s", stamp)
		}
	}
	if strings.Contains(makefile, "find examples") || strings.Contains(makefile, "find tables/") {
		t.Error("the lens walks the schema directories directly instead of the leg's generated stamps")
	}
}

// Fast tools on push, cppcheck only on the nightly trigger.
func TestSchemaCGeneratedCheckerLensSplitsFastAndNightly(t *testing.T) {
	t.Parallel()
	wf := readAt(t, repoRoot(t), cLensWorkflow)
	for _, want := range []string{"  push:", "  pull_request:", "  schedule:", "  workflow_dispatch:"} {
		if !strings.Contains(wf, want) {
			t.Errorf("%s has no %q trigger", cLensWorkflow, strings.TrimSpace(want))
		}
	}

	jobs := yamlJobs(wf)
	fast, ok := jobs["weverything"]
	if !ok {
		t.Fatalf("%s has no weverything job", cLensWorkflow)
	}
	if strings.Contains(fast, "tables-c-cppcheck") {
		t.Error("the push lane runs cppcheck; cppcheck belongs on the nightly trigger")
	}
	if !strings.Contains(fast, "tables-c-weverything") {
		t.Error("the push lane does not run tables-c-weverything")
	}

	nightly, ok := jobs["cppcheck"]
	if !ok {
		t.Fatalf("%s has no nightly cppcheck job", cLensWorkflow)
	}
	if !strings.Contains(nightly, "github.event_name == 'schedule'") {
		t.Error("the cppcheck job is not gated to the schedule")
	}
	if !strings.Contains(nightly, "tables-c-cppcheck") {
		t.Error("the nightly job does not run tables-c-cppcheck")
	}
}

// The control must be in the manifest the pull-request leg enumerates, and the
// control must actually fire: red with the plant in, green with it out.
func TestSchemaCGeneratedWeverythingNegativeControl(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skipf("the control needs clang: %v", err)
	}
	manifest := readAt(t, root, "make/negative-controls.json")
	if !strings.Contains(manifest, `"`+cLensControl+`"`) {
		t.Errorf("%s is not registered in make/negative-controls.json", cLensControl)
	}

	cmd := exec.Command("make", "--no-print-directory", cLensControl)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly", "NOVA_TEST_NO_HOST=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", cLensControl, err, out)
	}
	if !strings.Contains(string(out), "planted unused variable turns the C -Weverything gate RED") {
		t.Fatalf("the control did not report its red and green halves:\n%s", out)
	}
}
