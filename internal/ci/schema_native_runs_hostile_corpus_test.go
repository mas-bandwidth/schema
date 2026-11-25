package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaNativeRunsHostileCorpusWiring holds the C and C++ cards' blades
// without a compiler in the Go unit tier: each port's make target and its
// planted-verdict control exist, the case binary is driven through
// tools/hostilecorpusexec, and no list carries a finding. The corpus runs as
// the registry's per-case hostile rows (tools/schema-test/hostile.go), its control as its own row.
func TestSchemaNativeRunsHostileCorpusWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	controls, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatalf("the controls registry is missing: %v", err)
	}

	for _, port := range []string{"c", "cpp"} {
		target := "schema-" + port + "-runs-hostile-corpus"
		makeFile, err := os.ReadFile(filepath.Join(root, "make", target+".mk"))
		if err != nil {
			t.Fatalf("make/%s.mk is missing: %v", target, err)
		}
		for _, want := range []string{
			".PHONY: " + target,
			target + "-negative-control",
			"hostilecorpusexec run -port " + port,
			"-fsanitize=address,undefined",
			"internal/ci/hostilecorpus/corpus",
		} {
			if !strings.Contains(string(makeFile), want) {
				t.Errorf("make/%s.mk does not carry %q", target, want)
			}
		}
		if !strings.Contains(string(controls), `"`+target+`-negative-control"`) {
			t.Errorf("make/negative-controls.json does not register %s-negative-control", target)
		}

		// NO LIST CARRIES A FINDING (docs/rulings.json no-skips): every
		// disagreement is red by its case's name
		if _, err := os.Stat(filepath.Join(root, "test", port+"-hostile", "known-findings.tsv")); !os.IsNotExist(err) {
			t.Errorf("test/%s-hostile/known-findings.tsv is present: a list that carries findings by name (%v)", port, err)
		}
		if strings.Contains(string(makeFile), "known-findings") || strings.Contains(string(makeFile), "-findings") {
			t.Errorf("make/%s.mk hands the runner a findings list: a finding is never carried", target)
		}
	}
}
