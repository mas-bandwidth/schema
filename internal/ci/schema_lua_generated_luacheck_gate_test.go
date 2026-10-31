package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLuaGeneratedLuacheckWiring keeps the functional Linux gate connected to
// CI without running generation or an external checker from a unit test. The
// workflow itself executes the checker and its planted negative control.
func TestLuaGeneratedLuacheckWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	read := func(parts ...string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	mk := read("make", "schema-lua-generated-luacheck-gate.mk")
	for _, want := range []string{
		"tables-lua-luacheck:",
		"tables-lua-luacheck-negative-control:",
		"$(LUACHECK) --config",
		"lua_luacheck_plant",
		"tables-lua-format:",
		"tables-lua-format-negative-control:",
		"STYLUA_SHA256_linux-x86_64",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("makefile missing %q", want)
		}
	}

	// ZERO WARNINGS, NO ALLOWANCE: the generated code and the table runtime
	// are held to luacheck's default set. A global ignore list or a
	// line-length allowance would hide warnings rather than fix the generator.
	for _, banned := range []string{"max_line_length", "'ignore = {", "--no-max-line-length", "ratchet"} {
		if strings.Contains(mk, banned) {
			t.Errorf("makefile carries %q: the luacheck gate admits no allowance (fix the generator)", banned)
		}
	}

	workflow := read(".github", "workflows", "schema-lua-generated-luacheck-gate.yml")
	for _, want := range []string{
		"run: make tables-lua-luacheck",
		"run: make tables-lua-luacheck-negative-control",
		"run: make tables-lua-format",
		"run: make tables-lua-format-negative-control",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
}
