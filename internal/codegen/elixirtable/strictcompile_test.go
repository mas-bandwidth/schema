package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGeneratedTableCompilesStrictElixir builds the units ELIXIR_FIXED_UNITS
// names and the paired bench unit through make/elixir-strict.sh (docs/
// SPEC-TABLES.md §2.9, §7.2, §11): the extent branches and the empty
// @uncarried guard clauses must not be emitted where Elixir 1.20 can prove
// them dead. It runs the Elixir toolchain, so it is a functional test:
// SCHEMA_REQUIRE_CORPUS=1 makes it run, never skip.
func TestGeneratedTableCompilesStrictElixir(t *testing.T) {
	t.Parallel()

	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "1" {
		t.Skip("functional test: set SCHEMA_REQUIRE_CORPUS=1 to run the Elixir toolchain")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))

	cmd := exec.Command("make", "ELIXIR=elixir", "ELIXIRC=elixirc", "MIX=mix",
		"build/elixir-fixedform/.stamp", "build/elixir-fixed-bench/.stamp")
	cmd.Dir = root
	out, _ := cmd.CombinedOutput()
	text := string(out)

	var bad []string
	for _, block := range strings.Split(text, "warning:")[1:] {
		if strings.Contains(block, "has_extent") || strings.Contains(block, "in @uncarried") {
			bad = append(bad, "warning:"+block)
		}
	}
	if len(bad) != 0 {
		t.Fatalf("strict compile names a dead extent or uncarried branch:\n%s", strings.Join(bad, "\n"))
	}
}
