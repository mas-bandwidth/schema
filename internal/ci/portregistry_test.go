package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPortRegistryDefinedOnce pins the single port-registry definition in
// Makefile:
//   - Makefile defines SCHEMA_PORTS exactly once using $(filter-out %-%,...) over
//     $(patsubst make/%.mk,%,$(wildcard make/*.mk));
//   - No recipe line in Makefile, make/*.mk, or make/checks/*.mk iterates
//     $(wildcard make/*.mk) except through $(SCHEMA_PORTS);
//   - None of the per-card ad-hoc variants (TABLES_REFUSE_PORTS,
//     NON_PORT_MAKE_FRAGMENTS, or the "support fragment, not a language backend"
//     grep guard) appears in Makefile, make/*.mk, make/checks/*.mk, or
//     internal/ci/probe_test.go.
func TestPortRegistryDefinedOnce(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	makefileBytes, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(makefileBytes)

	const wantDef = "SCHEMA_PORTS := $(sort $(filter-out %-%,$(foreach p,$(patsubst make/%.mk,%,$(wildcard make/*.mk)),$(if $(findstring -,$(p)),,$(p)))))"
	defCount := 0
	usedInProbe := false
	for line := range strings.SplitSeq(makefile, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "SCHEMA_PORTS") && strings.Contains(trimmed, "=") && !strings.HasPrefix(trimmed, "#") {
			defCount++
			if trimmed != wantDef {
				t.Errorf("SCHEMA_PORTS definition = %q, want %q", trimmed, wantDef)
			}
		}
		if strings.Contains(line, "for lang in $(SCHEMA_PORTS); do") {
			usedInProbe = true
		}
	}
	if defCount != 1 {
		t.Errorf("Makefile defines SCHEMA_PORTS %d times, want 1 (%q)", defCount, wantDef)
	}
	if !usedInProbe {
		t.Errorf("Makefile tables-ports-refuse-wide-scalars does not iterate $(SCHEMA_PORTS)")
	}

	var checkFiles []string
	checkFiles = append(checkFiles, filepath.Join(root, "Makefile"))
	for _, pat := range []string{
		filepath.Join(root, "make", "*.mk"),
		filepath.Join(root, "make", "checks", "*.mk"),
	} {
		matches, err := filepath.Glob(pat)
		if err != nil {
			t.Fatalf("glob %s: %v", pat, err)
		}
		checkFiles = append(checkFiles, matches...)
	}

	forbiddenTokens := []string{
		"TABLES_REFUSE_PORTS",
		"NON_PORT_MAKE_FRAGMENTS",
		"support fragment, not a language backend",
	}

	for _, path := range checkFiles {
		rel, _ := filepath.Rel(root, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(raw)
		for _, bad := range forbiddenTokens {
			if strings.Contains(text, bad) {
				t.Errorf("%s contains forbidden per-card port-registry variant %q", rel, bad)
			}
		}
		for lineNo, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "\t") && strings.Contains(line, "$(wildcard make/*.mk)") {
				t.Errorf("%s:%d: recipe line iterates $(wildcard make/*.mk) directly instead of $(SCHEMA_PORTS): %s",
					rel, lineNo+1, strings.TrimSpace(line))
			}
		}
	}

	probeTestPath := filepath.Join(root, "internal", "ci", "probe_test.go")
	probeRaw, err := os.ReadFile(probeTestPath)
	if err != nil {
		t.Fatalf("read internal/ci/probe_test.go: %v", err)
	}
	probeText := string(probeRaw)
	for _, bad := range forbiddenTokens {
		if strings.Contains(probeText, bad) {
			t.Errorf("internal/ci/probe_test.go contains forbidden per-card variant %q", bad)
		}
	}
}
