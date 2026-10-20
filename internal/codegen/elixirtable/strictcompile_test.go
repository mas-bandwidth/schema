package elixirtable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedTableCompilesStrictElixir generates every unit ELIXIR_FIXED_UNITS
// names and the paired bench pair, then compiles each through
// make/elixir-strict.sh (docs/SPEC-TABLES.md §2.9, §7.2, §11). Elixir 1.20's
// type checker must find no dead extent branch, no dead uncarried guard, no
// ungrouped json clause and no clause its unit proves unreachable. It runs the
// Elixir toolchain, so it is a functional test: SCHEMA_REQUIRE_CORPUS=1 makes
// it run, never skip.
func TestGeneratedTableCompilesStrictElixir(t *testing.T) {
	t.Parallel()

	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "1" {
		t.Skip("functional test: set SCHEMA_REQUIRE_CORPUS=1 to run the Elixir toolchain")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	strict := filepath.Join(root, "make", "elixir-strict.sh")

	run := func(dir string, name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	run(root, "make", "bin/schema")

	units := strictFixedUnits(t, root)
	tmp := t.TempDir()
	type generated struct {
		name  string
		files []string
	}
	var fixed []generated

	for _, unit := range units {
		parts := strings.SplitN(unit, ":", 2)
		if len(parts) != 2 {
			t.Fatalf("ELIXIR_FIXED_UNITS entry %q is not name:path", unit)
		}
		out := filepath.Join(tmp, "gen", parts[0])
		run(root, "./bin/schema", "generate", "--lang", "elixir", "--out", out, parts[1])
		fixed = append(fixed, generated{name: parts[0], files: exFiles(t, out)})
	}

	src := filepath.Join(tmp, "bench-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(root, "bench", "corpus", "Bench.schema"), filepath.Join(src, "Bench.schema"))
	copyFile(t, filepath.Join(root, "bench", "corpus", "FixedTable.schema"), filepath.Join(src, "Wrap.schema"))
	benchOut := filepath.Join(tmp, "gen", "bench")
	run(root, "./bin/schema", "generate", "--lang", "elixir", "--out", benchOut,
		filepath.Join(src, "Bench.schema"), filepath.Join(src, "Wrap.schema"))

	compile := func(name string, files []string) {
		t.Helper()
		ebin := filepath.Join(tmp, "ebin", name)
		if err := os.MkdirAll(ebin, 0o755); err != nil {
			t.Fatal(err)
		}
		args := append([]string{strict, "elixirc", "-o", ebin}, files...)
		cmd := exec.Command("sh", args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		text := string(out)
		if strings.Contains(text, "warning:") || err != nil {
			t.Fatalf("%s does not compile strictly (docs/SPEC-TABLES.md §2.9, §7.2, §11): %v\n%s",
				name, err, warningText(text))
		}
	}

	// Each unit compiles on its own: the strict gate must hold for the unit
	// make/elixir.mk builds, not only for the whole set side by side.
	for _, unit := range fixed {
		compile(unit.name, unit.files)
	}
	compile("bench", exFiles(t, benchOut))
}

// strictFixedUnits reads ELIXIR_FIXED_UNITS from the make database so the test
// and make/elixir.mk cannot drift apart.
func strictFixedUnits(t *testing.T, root string) []string {
	t.Helper()
	cmd := exec.Command("make", "-pn")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("make -pn: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "ELIXIR_FIXED_UNITS := "); ok {
			return strings.Fields(v)
		}
	}
	t.Fatal("make database names no ELIXIR_FIXED_UNITS")
	return nil
}

func exFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.ex"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no generated .ex files in %s", dir)
	}
	return files
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func warningText(text string) string {
	blocks := strings.Split(text, "warning:")
	if len(blocks) == 1 {
		return text
	}
	var b strings.Builder
	for _, block := range blocks[1:] {
		b.WriteString("warning:")
		b.WriteString(strings.TrimRight(block, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}
