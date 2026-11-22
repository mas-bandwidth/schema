package csharp_test

// THE NAME CORPUS, C# LEG: every case of testdata/name-buildability-emit that
// cases.tsv names for cs generates and compiles into one library with the
// serialize.cs runtime, every warning an error and every warning wave on — as
// strict as the repo's own projects (TreatWarningsAsErrors) and stricter. The
// names are a type spelled in lowercase letters only (CS8981), and a package
// or a type named Math, which hid System.Math from the code beside it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/nameemit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

func TestNameCorpusBuildsCs(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK and the serialize.cs sibling")
	runtimeDir := os.Getenv("SERIALIZE_CS")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("..", "..", "..", "..", "serialize.cs")
	}
	runtimeDir, err := filepath.Abs(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	var runtime []string
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		path := filepath.Join(runtimeDir, "src", name)
		if _, err := os.Stat(path); err != nil {
			if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
				t.Fatalf("the serialize.cs runtime is not staged at %s (SERIALIZE_CS, else the sibling checkout); SCHEMA_REQUIRE_CORPUS says the corpus must build", path)
			}
			t.Skipf("the serialize.cs runtime is not staged at %s (SERIALIZE_CS, else the sibling checkout)", path)
		}
		runtime = append(runtime, path)
	}
	csc, refs := cscToolchain(t)
	for _, c := range nameemit.For(t, "cs") {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			files := nameemit.Generate(t, c, "cs")
			dir := t.TempDir()
			sources := nameemit.Write(t, files, dir, ".cs")
			args := []string{"exec", csc, "-nologo", "-noconfig", "-target:library", "-unsafe",
				"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-warn:9999", "-warnaserror+",
				"-out:" + filepath.Join(dir, "probe.dll")}
			for _, ref := range refs {
				args = append(args, "-r:"+ref)
			}
			for _, s := range sources {
				args = append(args, filepath.Join(dir, s))
			}
			args = append(args, runtime...)
			cmd := testproc.Command(t, dotnet(), args...)
			cmd.Dir = dir
			cmd.Env = dotnetEnv(dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s (%s): the generated C# does not build: %v\n%s", c.Name, c.Note, err, out)
			}
		})
	}
}

func dotnet() string {
	if d := os.Getenv("DOTNET"); d != "" {
		return d
	}
	return "dotnet"
}

// cscToolchain finds the SDK's newest csc.dll and reference assemblies.
func cscToolchain(t *testing.T) (csc string, refs []string) {
	t.Helper()
	path, err := exec.LookPath(dotnet())
	if err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and dotnet is not found: %v", err)
		}
		t.Skipf("dotnet: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	root := filepath.Dir(path)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	sort.Strings(cscs)
	refDirs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net*"))
	if len(refDirs) == 0 {
		t.Fatalf("no reference assemblies under %s", root)
	}
	sort.Strings(refDirs)
	refs, _ = filepath.Glob(filepath.Join(refDirs[len(refDirs)-1], "*.dll"))
	if len(refs) == 0 {
		t.Fatalf("no reference assemblies in %s", refDirs[len(refDirs)-1])
	}
	return cscs[len(cscs)-1], refs
}

// dotnetEnv keeps the SDK's first-run state inside the test's own directory.
func dotnetEnv(dir string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "HOME="), strings.HasPrefix(e, "TMPDIR="), strings.HasPrefix(e, "TMP="),
			strings.HasPrefix(e, "TEMP="), strings.HasPrefix(e, "DOTNET_"):
			continue
		}
		env = append(env, e)
	}
	return append(env, "DOTNET_NOLOGO=1", "DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"HOME="+dir, "DOTNET_CLI_HOME="+dir, "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
}
