package ci_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestMainRedsPinsMatchCIFull holds the sibling-runtime pins in
// .github/workflows/ci-full.yml to the pins the make files resolve when the
// workflow runs a make target without setting the tag in the shell environment.
// A mismatch means a local `make` and a CI job build against different sibling
// tags.
func TestMainRedsPinsMatchCIFull(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci-full.yml"))
	if err != nil {
		t.Fatal(err)
	}
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	pins := []struct {
		ciEnv   string
		mkVar   string
		mkBytes []byte
		mkFile  string
	}{
		{"SERIALIZE_TAG", "SERIALIZE_PIN", makefile, "Makefile"},
		{"SERIALIZE_C_TAG", "SERIALIZE_C_PIN", makefile, "Makefile"},
		{"SERIALIZE_CS_TAG", "SERIALIZE_CS_PIN", makefile, "Makefile"},
		{"SERIALIZE_GO_TAG", "SERIALIZE_GO_PIN", makefile, "Makefile"},
		{"SERIALIZE_RS_TAG", "SERIALIZE_RS_PIN", makefile, "Makefile"},
		{"SERIALIZE_JS_TAG", "SERIALIZE_JS_PIN", makefile, "Makefile"},
	}
	for _, p := range pins {
		ciRe := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(p.ciEnv) + `:\s*(\S+)\s*$`)
		ciMatch := ciRe.FindSubmatch(workflow)
		if ciMatch == nil {
			t.Fatalf("ci-full.yml does not pin %s", p.ciEnv)
		}
		mkRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(p.mkVar) + `\s*\?=\s*(\S+)\s*$`)
		mkMatch := mkRe.FindSubmatch(p.mkBytes)
		if mkMatch == nil {
			t.Errorf("%s names no %s, so the %s sibling has no make pin to compare with ci-full.yml\x27s %s",
				p.mkFile, p.mkVar, p.ciEnv, string(ciMatch[1]))
			continue
		}
		if string(ciMatch[1]) != string(mkMatch[1]) {
			t.Errorf("ci-full.yml %s=%s != %s %s=%s",
				p.ciEnv, string(ciMatch[1]), p.mkFile, p.mkVar, string(mkMatch[1]))
		}
	}
}
