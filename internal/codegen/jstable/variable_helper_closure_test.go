package jstable

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Generic variable codecs refer to text-list helpers even when every field in
// this particular unit is scalar. Lint the emitted modules, not just a unit
// that happens to exercise the text-list branch.
func TestJavaScriptVariableHelperClosure(t *testing.T) {
	slowtest.Gate(t, "eslint (generated variable runtime dependencies)")
	tool := os.Getenv("ESLINT")
	if tool == "" {
		tool = "eslint"
	}
	eslint, err := exec.LookPath(tool)
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatal(err)
		}
		t.Skipf("eslint unavailable: %v", err)
	}
	config, err := filepath.Abs("../../../eslint.config.mjs")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, source string }{
		{"scalar", "package probe\ntable Root { value int32 }"},
		{"nested", "package probe\ntable Child { value int32 }\ntable Root { child Child\n ptr *Child }"},
		{"text-list", "package probe\ntable Root { values []string(8)\n optional [..3]?string(8) }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, err := generateVariableFiles(unitFrom(t, tc.source), nil)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(eslint, "--config", config, "--max-warnings", "0", ".")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated runtime lint: %v\n%s", err, out)
			}
		})
	}
}
