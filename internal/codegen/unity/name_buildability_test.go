package unity_test

// THE NAME CORPUS, UNITY LEG: every case of testdata/name-buildability-emit
// that cases.tsv names for unity generates and compiles in the Unity dialect
// (test/unity/UnityDialect.props): the netstandard2.1 surface, C# 9, nullable
// off, no unsafe code, every warning an error. The serialize.cs runtime rides
// as its own assembly, the way a Unity project carries it, so a package name
// reaches the generated code only. The names are System types the generated
// code spells (a table named Math, a type named BitConverter), which a schema
// type or package of the same name hid.

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

func TestNameCorpusBuildsUnity(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK with the netstandard2.1 pack, and the serialize.cs sibling")
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
			skipOrFail(t, "the serialize.cs runtime is not staged at %s (SERIALIZE_CS, else the sibling checkout)", path)
		}
		runtime = append(runtime, path)
	}
	csc, refs := unityToolchain(t)

	// the runtime once, as its own netstandard2.1 assembly
	rtDir := t.TempDir()
	serializeDll := filepath.Join(rtDir, "Serialize.dll")
	args := append(cscArgs(csc, refs, serializeDll), "-nullable:enable")
	args = append(args, runtime...)
	if out, err := run(t, rtDir, args); err != nil {
		t.Fatalf("the serialize.cs runtime at %s does not build in the Unity dialect: %v\n%s", runtimeDir, err, out)
	}

	for _, c := range nameemit.For(t, "unity") {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			files := nameemit.Generate(t, c, "unity")
			dir := t.TempDir()
			sources := nameemit.Write(t, files, dir, ".cs")
			args := append(cscArgs(csc, refs, filepath.Join(dir, "probe.dll")), "-nullable:disable", "-r:"+serializeDll)
			for _, s := range sources {
				args = append(args, filepath.Join(dir, s))
			}
			if out, err := run(t, dir, args); err != nil {
				t.Fatalf("%s (%s): the generated Unity C# does not build: %v\n%s", c.Name, c.Note, err, out)
			}
		})
	}
}

// cscArgs is the Unity dialect's compile of a library to out.
func cscArgs(csc string, refs []string, out string) []string {
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:library", "-langversion:9",
		"-nostdlib+", "-warn:9999", "-warnaserror+", "-out:" + out}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	return args
}

func run(t *testing.T, dir string, args []string) ([]byte, error) {
	t.Helper()
	cmd := testproc.Command(t, dotnet(), args...)
	cmd.Dir = dir
	cmd.Env = dotnetEnv(dir)
	return cmd.CombinedOutput()
}

func skipOrFail(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatalf(format+"; SCHEMA_REQUIRE_CORPUS says the corpus must build", args...)
	}
	t.Skipf(format, args...)
}

func dotnet() string {
	if d := os.Getenv("DOTNET"); d != "" {
		return d
	}
	return "dotnet"
}

// unityToolchain finds the SDK's newest csc.dll and the netstandard2.1
// reference assemblies.
func unityToolchain(t *testing.T) (csc string, refs []string) {
	t.Helper()
	path, err := exec.LookPath(dotnet())
	if err != nil {
		skipOrFail(t, "dotnet: %v", err)
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
	refDirs, _ := filepath.Glob(filepath.Join(root, "packs", "NETStandard.Library.Ref", "*", "ref", "netstandard2.1"))
	if len(refDirs) == 0 {
		skipOrFail(t, "no netstandard2.1 reference assemblies under %s", root)
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
