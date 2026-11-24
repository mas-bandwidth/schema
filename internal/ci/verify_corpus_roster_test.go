package ci

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// W-ROSTER (docs/SPEC-CONFORMANCE.md §2): a change to the wire-fuzz corpus,
// the harness or the absent-root ledger runs the per-PR fuzz row of EVERY leg, so a
// root added to the corpus without its drivers goes red on the PR that adds it
// and not in the nightly. The shared_broad path rule is that selection.
func TestVerifyLegsCorpusRosterIsShared(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/ci-fast.yml")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	var rule string
	for i, line := range lines {
		if strings.TrimSpace(line) == "shared_broad=no" && i+1 < len(lines) {
			m := regexp.MustCompile(`grep -qE \x27([^\x27]+)\x27`).FindStringSubmatch(lines[i+1])
			if m == nil {
				t.Fatalf("the line after shared_broad=no has no grep -qE rule: %q", lines[i+1])
			}
			rule = m[1]
			break
		}
	}
	if rule == "" {
		t.Fatal("ci-fast.yml has no shared_broad=no plan step")
	}
	re, err := regexp.Compile(rule)
	if err != nil {
		t.Fatalf("shared_broad rule %q: %v", rule, err)
	}
	for _, path := range []string{
		"testdata/conformance/tables/MANIFEST.txt",
		"test/conformance/harness/wirefuzz.go",
		"test/conformance/wire-absent/c-plain.tsv",
		"test/tables/surfaces/Surfaces.schema",
	} {
		if !re.MatchString(path) {
			t.Errorf("a change to %s does not select every verify leg (shared_broad rule %q)", path, rule)
		}
	}
}
