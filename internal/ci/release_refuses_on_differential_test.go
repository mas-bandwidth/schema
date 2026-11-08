package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseRefusesOnDifferentialReachesDifferential keeps the release guard
// and its negative control aimed at the differential gate. The synthetic proof
// must reach the release command through the shared make variable, and the
// negative control must sabotage exactly the recipe that runs the differential.
func TestReleaseRefusesOnDifferentialReachesDifferential(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(data)

	gate := makeTargetBody(makefile, "release-refuses-on-differential")
	control := makeTargetBody(makefile, "release-refuses-on-differential-negative-control")
	if gate == "" {
		t.Fatal("Makefile has no release-refuses-on-differential target")
	}
	if control == "" {
		t.Fatal("Makefile has no release-refuses-on-differential-negative-control target")
	}

	const proofAssignment = "RELEASE_GATE_MAKE = $(MAKE) --no-print-directory -f $(firstword $(MAKEFILE_LIST)) release VERSION=v99999.0.0 RELEASE_COMMIT=HEAD RELEASE_MAIN=HEAD PROOF=$(RELEASE_GATE_TEST)/green.json"
	if !strings.Contains(makefile, proofAssignment) {
		t.Errorf("RELEASE_GATE_MAKE must pass the synthetic PROOF=$(RELEASE_GATE_TEST)/green.json")
	}
	if !strings.Contains(gate, "green.json") || !strings.Contains(gate, "$(RELEASE_GATE_MAKE)") {
		t.Errorf("release-refuses-on-differential must write a synthetic proof and invoke RELEASE_GATE_MAKE")
	}
	if !strings.Contains(control, "release-refuses-on-differential-negative-control: release-refuses-on-differential") ||
		!strings.Contains(control, "release-refuses-on-differential RELEASE_GATE_TEST=$(RELEASE_GATE_CONTROL)/test") {
		t.Errorf("negative control must run the proof-carrying release guard with its isolated RELEASE_GATE_TEST")
	}

	const sabotage = "@sed 's/^\\t$$(MAKE) --no-print-directory release-differential$$/\\t@: # SABOTAGED: the differential is not run first/' Makefile"
	if !strings.Contains(control, sabotage) {
		t.Errorf("negative control must sabotage the release-differential recipe with the expected sed pattern")
	}
	const recipe = "\t$(MAKE) --no-print-directory release-differential"
	count := 0
	for line := range strings.SplitSeq(makefile, "\n") {
		if line == recipe {
			count++
		}
	}
	if count != 1 {
		t.Errorf("negative-control sed pattern matches %d release-differential recipe lines, want exactly 1", count)
	}
}

func makeTargetBody(makefile, target string) string {
	lines := strings.Split(makefile, "\n")
	prefix := target + ":"
	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) || (len(line) > len(prefix) && line[len(prefix)] != ' ' && line[len(prefix)] != '\t') {
			continue
		}
		end := i + 1
		for end < len(lines) {
			line = lines[end]
			if strings.TrimSpace(line) != "" && line[0] != ' ' && line[0] != '\t' {
				break
			}
			end++
		}
		return strings.Join(lines[i:end], "\n")
	}
	return ""
}
