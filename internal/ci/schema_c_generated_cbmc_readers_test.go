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
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "schema-c-generated-cbmc-readers.mk"))
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
		t.Error("make/schema-c-generated-cbmc-readers.mk does not fail on a failed verification (want `VERIFICATION FAILED`)")
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
