package squirreltable_test

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestSquirrelTestgenRoundTripAndDamage is the gate of `schema testgen
// --lang squirrel` (docs/SPEC-TABLES.md §16.11). It generates tables/examples,
// writes the generated modules and tests in a t.TempDir(), and runs every
// emitted test under the pinned interpreter: each covers its file's root
// tables through the wire, base64 and JSON carriers and refuses damage by
// name. A runtime patched in memory (Save returns nothing) turns it red, and a
// table whose forms leave out wire or json is refused by name.
func TestSquirrelTestgenRoundTripAndDamage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables", "examples")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	tests, err := squirreltable.Testgen(u)
	if err != nil {
		t.Fatalf("testgen: %v", err)
	}
	var want []string
	tables := 0
	for _, f := range u.Files {
		n := 0
		for _, st := range f.Tables {
			if st.IsTable && !st.IsMapEntry() {
				n++
			}
		}
		if n == 0 {
			continue
		}
		tables += n
		want = append(want, f.Base+"Table_gen_test.nut")
	}
	if len(tests) != len(want) {
		t.Fatalf("testgen wrote %d files, want %d (%v)", len(tests), len(want), want)
	}
	for _, name := range want {
		if _, ok := tests[name]; !ok {
			t.Fatalf("testgen wrote no %s", name)
		}
	}
	again, err := squirreltable.Testgen(u)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range tests {
		if !bytes.Equal(again[name], src) {
			t.Fatalf("%s is not deterministic", name)
		}
	}
	// §16.11: a generated test is the round trip AND the compare that makes a
	// wrong value fail. Check that each carrier's equal-compare is carried, so
	// deleting one from the emitter cannot leave a test that runs green.
	compares := []string{
		`if (J.ToJson(T, root, x) != text) fail(root + ": the JSON text did not read back equal");`,
		`if (!sameBlob(S.save(T, root, y), wire)) fail(root + ": the value read does not save to the sample's wire bytes");`,
		`if (J.ToJson(T, root, y) != text) fail(root + ": the wire did not read back equal to the JSON text");`,
		`if (!sameBlob(S.save(T, root, z), wire)) fail(root + ": the base64 text does not decode to the sample's wire");`,
		`if (J.ToJson(T, root, z) != text) fail(root + ": the base64 text does not read back equal to the JSON text");`,
	}
	for name, src := range tests {
		for _, compare := range compares {
			if !bytes.Contains(src, []byte(compare)) {
				t.Fatalf("%s does not carry the compare %q", name, compare)
			}
		}
	}
	if tables == 0 {
		t.Fatal("tables/examples declares no root table")
	}
	write := func(dir string, extra map[string][]byte) {
		t.Helper()
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tabs, err := squirreltable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tabs)
		maps.Copy(files, extra)
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("green", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		write(dir, tests)
		for _, name := range want {
			out := sqrun.Run(t, dir, name, nil)
			if !bytes.Contains(out, []byte("testgen tables=")) {
				t.Fatalf("%s did not report its covered tables:\n%s", name, out)
			}
		}
		t.Logf("%d tables covered across %d generated test files", tables, len(want))
	})

	t.Run("sabotaged_runtime", func(t *testing.T) {
		t.Parallel()
		sq := sqrun.Require(t)
		dir := t.TempDir()
		runtime := squirreltable.Runtime()["schema_table_save.nut"]
		anchor := regexp.MustCompile(`(?s)_M\.save <- function\(.*?\{\n`)
		found := anchor.FindAllIndex(runtime, -1)
		if len(found) != 1 {
			t.Fatalf("want exactly one Save entry point in schema_table_save.nut, found %d", len(found))
		}
		end := found[0][1]
		patched := make([]byte, 0, len(runtime)+len("  return null;\n"))
		patched = append(patched, runtime[:end]...)
		patched = append(patched, []byte("  return null;\n")...)
		patched = append(patched, runtime[end:]...)
		extra := map[string][]byte{"schema_table_save.nut": patched}
		maps.Copy(extra, tests)
		write(dir, extra)
		cmd := exec.Command(sq, want[0])
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err = cmd.Run()
		if err == nil && stderr.Len() == 0 {
			t.Fatalf("a sabotaged Save went unnoticed:\n%s", stdout.String())
		}
	})

	t.Run("refuses_forms", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "Probe.schema")
		if err := os.WriteFile(path, []byte("package probe\ntable Only | forms = \"wire\" { n int32 }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		probe, err := compiler.New().Load([]string{path})
		if err != nil {
			t.Fatal(err)
		}
		_, err = squirreltable.Testgen(probe)
		if err == nil || !strings.Contains(err.Error(), "Only") || !strings.Contains(err.Error(), "json") || !strings.Contains(err.Error(), "§16.11") {
			t.Fatalf("forms without json: %v", err)
		}
	})
}
