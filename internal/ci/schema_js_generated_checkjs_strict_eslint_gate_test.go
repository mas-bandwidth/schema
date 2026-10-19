package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The card's own make file is the lens, not a comment: it must pin the
// TypeScript version, ask for checkJs and --strict, and refuse by name when a
// tool is absent rather than skipping.
func TestSchemaJSGeneratedCheckJSEslintGatePins(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make", "schema-js-generated-checkjs-strict-eslint-gate.mk"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(data)
	for _, want := range []string{
		"SCHEMA_JS_CHECKJS_TS_VERSION ?= 5.9.3",
		"--checkJs --strict",
		"tool missing: eslint",
		"tool missing: tsc",
		"schema-js-generated-checkjs-strict-eslint-gate-eslint-negative-control",
		`"no-var":"error"`,
		`"eqeqeq"`,
		"generated/js generated/js-ludicrous generated/bench/js",
	} {
		if !strings.Contains(mk, want) {
			t.Fatalf("make/schema-js-generated-checkjs-strict-eslint-gate.mk does not name %q", want)
		}
	}
}

// The planted-bad-pattern test. The make target copies one generated module
// twice: the clean copy must lint green, the copy with a planted `var` and a
// loose `==` must exit non-zero and name the plant on no-var and eqeqeq. A
// skip is not a pass: the target's verdict line is required.
func TestSchemaJSGeneratedCheckJSEslintGateFiresOnThePlant(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("make", "schema-js-generated-checkjs-strict-eslint-gate-eslint-negative-control")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("negative control: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "negative control: the planted var/== is red") {
		t.Fatalf("negative control did not reach its verdict:\n%s", out)
	}
}
