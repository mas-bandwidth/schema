package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The CBMC lens on the generated C readers (ideas roadmap group
// schema-per-language-lenses; Glenn, 2026-10-18) is a heavy gate, so it must
// pin its checker, probe for it, and run only on the nightly schedule — never
// per push. These two tests keep the lens's own files honest so the gate
// cannot silently drift to "runs nothing".

var (
	cbmcVersion = regexp.MustCompile(`CBMC_VERSION\s*\?=\s*[0-9]+\.[0-9]+\.[0-9]+`)
	cbmcProbe   = regexp.MustCompile(`tool missing: cbmc`)
)

func TestCBMCReadersMakefilePinsAndProbesTheTool(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "checks", "schema-c-generated-cbmc-readers.mk"))
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	s := string(b)
	if !cbmcVersion.MatchString(s) {
		t.Error("make/schema-c-generated-cbmc-readers.mk does not pin a CBMC version (want `CBMC_VERSION ?= x.y.z`)")
	}
	if !cbmcProbe.MatchString(s) {
		t.Error("make/schema-c-generated-cbmc-readers.mk does not probe for the tool (want `tool missing: cbmc`)")
	}
	if !strings.Contains(s, "VERIFICATION FAILED") {
		t.Error("the CBMC make fragment does not fail on a failed verification (want `VERIFICATION FAILED`)")
	}
	if !strings.Contains(s, "tool missing: serialize.c runtime") {
		t.Error("the CBMC make fragment does not name the missing serialize.c runtime")
	}
	if !strings.Contains(s, "schema-c-generated-cbmc-readers-negative-control") {
		t.Error("the CBMC make fragment has no planted-bad-pattern control target")
	}
}

func TestCBMCReadersWorkflowIsNightlyOnly(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), ".github", "workflows", "schema-c-generated-cbmc-readers.yml"))
	if err != nil {
		t.Fatalf("the card's workflow file is missing: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "schedule:") {
		t.Error("the CBMC readers workflow has no schedule trigger (the heavy gate is nightly-only)")
	}
	if !strings.Contains(s, "workflow_dispatch:") {
		t.Error("the CBMC readers workflow has no workflow_dispatch trigger")
	}
	for _, forbidden := range []string{"pull_request:", "push:"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("the CBMC readers workflow must not run on %s — CBMC is heavy", forbidden)
		}
	}
}

// The driver hands each reader a zeroed, correctly-typed value. The generated
// readers write through their value pointer, so a harness that passes NULL
// makes CBMC's pointer check fail every reader; a harness that calls the
// non-stream helper schema_interior_null_ (whose signature is not a stream
// reader) does not even type-check. These assertions pin the corrected driver.
func TestCBMCReadersDriverHarnessesTypedValues(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "checks", "schema-c-generated-cbmc-readers.mk"))
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	s := string(b)
	if strings.Contains(s, ", NULL)") {
		t.Error("the CBMC driver must not pass NULL as the reader's value pointer (readers write through it)")
	}
	for _, must := range []string{
		"memset(&v, 0, sizeof v)",
		"serialize_read_stream_t s;",
		"int main(void) {",
		"schema_interior_null_",
	} {
		if must == "schema_interior_null_" {
			if strings.Contains(s, "cbmc_harness_schema_interior_null_") {
				t.Error("the CBMC driver must not harness the non-stream helper schema_interior_null_")
			}
			continue
		}
		if !strings.Contains(s, must) {
			t.Errorf("the CBMC driver is missing expected construct %q", must)
		}
	}
}

// The CBMC fragment is a checks file, not a port. Every make/*.mk basename is
// discovered as a compiler language by tables-ports-refuse-wide-scalars, so a
// target-only fragment placed there is read as "a backend stopped for another
// reason" and reddens the port registry (and TestWideScalarProbeModuleBoundary
// with it). The include is by path, like the other make/checks fragments.
func TestCBMCReadersMakefileIsNotAPortFragment(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "make", "schema-c-generated-cbmc-readers.mk")); err == nil {
		t.Fatal("the CBMC make fragment is under make/*.mk, where every file is a discovered port; put it under make/checks/")
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(makefile), "include make/checks/schema-c-generated-cbmc-readers.mk") {
		t.Error("the Makefile does not include make/checks/schema-c-generated-cbmc-readers.mk by path")
	}
}
