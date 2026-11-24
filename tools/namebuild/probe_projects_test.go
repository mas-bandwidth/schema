package namebuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorded is what a probe build asked its toolchain to do: the command line
// of each step. No toolchain runs; the Runner answers every step with "ok".
type recorded struct {
	steps []string
}

func (r *recorded) run(_ string, _ []string, name string, args ...string) (string, error) {
	r.steps = append(r.steps, filepath.Base(name)+" "+strings.Join(args, " "))
	return "ok", nil
}

func probeEnv() Env {
	return Env{
		Root:        "/repo/root",
		Tool:        func(_, def string) string { return def },
		SerializeGo: "/rt/serialize.go",
		SerializeCS: "/rt/serialize.cs",
	}
}

func buildProbe(t *testing.T, target string, c Case) (string, *recorded) {
	t.Helper()
	dir := t.TempDir()
	rec := &recorded{}
	if err := Build(rec.run, probeEnv(), target, c, dir); err != nil {
		t.Fatalf("%s: %v", target, err)
	}
	return dir, rec
}

func readProbe(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestProbeProjectsBuildTableOutput holds the three probe projects that a
// table row (tb_*) cannot build without, each red on the project the nightly
// run of 2026-11-24 used: the C# project must allow the unsafe code the table
// cook emits, the Go module must resolve the table runtime from this
// repository, and the Java probe must name the package by an import and
// compile with no source path, or a package or file named System, probe or
// Probe is found as a source file in the work directory (CS0227, the missing
// tables/goruntime package, and "duplicate class" in the 2026-11-24 run).
func TestProbeProjectsBuildTableOutput(t *testing.T) {
	t.Parallel()
	c := Case{Bases: []string{"System", "Other"}, Pkg: "probe"}

	t.Run("cs", func(t *testing.T) {
		t.Parallel()
		dir, _ := buildProbe(t, "cs", c)
		if proj := readProbe(t, dir, "zz.csproj"); !strings.Contains(proj, "<AllowUnsafeBlocks>true</AllowUnsafeBlocks>") {
			t.Errorf("zz.csproj does not allow unsafe code, so table output (CS0227) cannot build:\n%s", proj)
		}
	})

	t.Run("go", func(t *testing.T) {
		t.Parallel()
		dir, _ := buildProbe(t, "go", c)
		mod := readProbe(t, dir, "go.mod")
		if !strings.Contains(mod, "github.com/mas-bandwidth/schema/v2 => /repo/root") {
			t.Errorf("go.mod does not replace the schema module with the repository, so tables/goruntime is not found:\n%s", mod)
		}
		if !strings.Contains(mod, "github.com/mas-bandwidth/serialize.go/v2 => /rt/serialize.go") {
			t.Errorf("go.mod lost the serialize.go replace:\n%s", mod)
		}
	})

	t.Run("java", func(t *testing.T) {
		t.Parallel()
		dir, rec := buildProbe(t, "java", c)
		main := readProbe(t, dir, "ZzMain.java")
		if !strings.HasPrefix(main, "import probe.Other;") {
			t.Errorf("ZzMain.java does not import the package, so a package named System or Probe is read as a type:\n%s", main)
		}
		if strings.Contains(main, "probe.Other.Holder") {
			t.Errorf("ZzMain.java still names the package in an expression:\n%s", main)
		}
		javac := rec.steps[0]
		if !strings.Contains(javac, "-sourcepath nosrc") {
			t.Errorf("javac runs with the default source path, so ./System.java and ./probe.java are read as classes of the default package: %s", javac)
		}
	})
}
