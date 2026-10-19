package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestSchemaJavaRunsHostileCorpus keeps the runner exhaustive as the shared
// manifest grows: every carrier named by the manifest must have an explicit
// Java dispatch arm. The planted omission proves this census can go red.
func TestSchemaJavaRunsHostileCorpus(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	manifest, err := os.ReadFile(filepath.Join(root, "internal", "ci", "hostilecorpus", "corpus", "manifest.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(root, "test", "conformance", "java", "src", "HostileCorpus.java"))
	if err != nil {
		t.Fatal(err)
	}
	if missing := missingJavaHostileCarriers(string(manifest), string(source)); len(missing) != 0 {
		t.Fatalf("Java hostile-corpus runner has no explicit dispatch for carriers: %v", missing)
	}
	planted := strings.Replace(string(source), `case "wire" ->`, `case "wire-removed" ->`, 1)
	missing := missingJavaHostileCarriers(string(manifest), planted)
	if len(missing) != 1 || missing[0] != "wire" {
		t.Fatalf("planted missing wire arm was not caught: missing=%v", missing)
	}
}

func missingJavaHostileCarriers(manifest, source string) []string {
	carriers := map[string]bool{}
	for _, line := range strings.Split(manifest, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) >= 2 {
			carriers[fields[1]] = true
		}
	}
	var missing []string
	for carrier := range carriers {
		quoted := regexp.MustCompile(`case\s+(?:"[^"]+"\s*,\s*)*"` + regexp.QuoteMeta(carrier) + `"(?:\s*,|\s*->)`).MatchString(source)
		if !quoted {
			missing = append(missing, carrier)
		}
	}
	sort.Strings(missing)
	return missing
}
