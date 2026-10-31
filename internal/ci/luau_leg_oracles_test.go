package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLuauLegBuildsItsOracles holds the Luau test leg to making everything its
// tests read. test-luau once ran only behind toolchain-luau, so a fresh stage
// had no fixed-form corpus, no conformance driver modules and no sibling Go
// runtime; six luautable tests failed on build products nothing in that target
// made. The rule must carry the prerequisites and hand the resolved runtime to
// the tests as an absolute path (docs/PORT-LUAU.md, THE HARNESS).
//
// Text only: it reads make/luau.mk, no interpreter and no make run.
func TestLuauLegBuildsItsOracles(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "make", "luau.mk"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\\\n", " ")

	var head string
	var recipe strings.Builder
	in := false
	for line := range strings.SplitSeq(text, "\n") {
		if !in {
			if strings.HasPrefix(line, "test-luau:") {
				in = true
				head = line
			}
			continue
		}
		if strings.HasPrefix(line, "\t") {
			recipe.WriteString(line)
			recipe.WriteByte('\n')
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		break
	}
	if head == "" {
		t.Fatal("make/luau.mk has no test-luau rule")
	}
	for _, want := range []string{
		"tables-fixedform-corpus",
		"$(LUAU_CONFORMANCE_DRIVER)/inputs.luau",
		"$(LUAU_CONFORMANCE_DRIVER)/units.luau",
		"$(SERIALIZE_GO_RUNTIME)/go.mod",
	} {
		if !strings.Contains(head, want) {
			t.Errorf("test-luau does not name %s as a prerequisite", want)
		}
	}
	rec := recipe.String()
	if !strings.Contains(rec, "SCHEMA_REQUIRE_CORPUS=1") {
		t.Error("test-luau's recipe does not set SCHEMA_REQUIRE_CORPUS=1")
	}
	if !strings.Contains(rec, "SERIALIZE_GO=$(abspath $(SERIALIZE_GO_RUNTIME))") {
		t.Error("test-luau's recipe does not export SERIALIZE_GO as $(abspath $(SERIALIZE_GO_RUNTIME))")
	}

	// Every oracle resolves the runtime the same way. A test that names the
	// fetched build/serialize.go path directly dies on a staged bench, where
	// the sibling checkout is what the Makefile resolves.
	files, err := filepath.Glob(filepath.Join(root, "internal", "codegen", "luautable", "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no luautable tests found: this gate would pass over an empty set")
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "build/serialize.go") {
			t.Errorf("%s names build/serialize.go; resolve the runtime through slowtest.SerializeGo", filepath.Base(file))
		}
	}
}
