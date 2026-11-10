package cstable_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// The sixteen rows. The method is the named check FixedFormChecks.cs runs
// through strict FixedLoad. The negative control flips one constant inside
// TestFixedValidDataBool and requires that run to go red.
var csharpValidDataRows = []struct {
	id     string
	method string
}{
	{"bool-values/cs/valid-data", "TestFixedValidDataBool"},
	{"signed-128/cs/valid-data", "TestFixedValidDataSigned128"},
	{"unsigned-128/cs/valid-data", "TestFixedValidDataUnsigned128"},
	{"ranged-integers/cs/valid-data", "TestFixedValidDataRangedIntegers"},
	{"signed-fixed-point/cs/valid-data", "TestFixedValidDataSignedFixedPoint"},
	{"unsigned-fixed-point/cs/valid-data", "TestFixedValidDataUnsignedFixedPoint"},
	{"enum-values/cs/valid-data", "TestFixedValidDataEnums"},
	{"utf8-values/cs/valid-data", "TestFixedValidDataUtf8"},
	{"byte-buffers/cs/valid-data", "TestFixedValidDataByteBuffers"},
	{"nested-fixed-tables/cs/valid-data", "TestFixedValidDataNested"},
	{"fixed-arrays/cs/valid-data", "TestFixedValidDataFixedArrays"},
	{"counted-arrays/cs/valid-data", "TestFixedValidDataCountedArrays"},
	{"union-values/cs/valid-data", "TestFixedValidDataUnions"},
	{"optional-nesting/cs/valid-data", "TestFixedValidDataOptionalNesting"},
	{"fixed-file-roundtrip/cs/valid-data", "TestFixedValidDataFileRoundtrip"},
	{"record-measurement/cs/valid-data", "TestFixedValidDataRecordMeasurement"},
}

const (
	validDataBoolToken = "        const bool validDataBoolExpect = true;"
	validDataBoolFault = "        const bool validDataBoolExpect = false;"
)

const validDataDriver = `using System;

static partial class Program
{
    static bool failed;

    static readonly (string Root, byte[] Layout, byte[] Digest, ulong Oracle, ulong Emitted)[] FixedHashOracleRoots =
        new (string Root, byte[] Layout, byte[] Digest, ulong Oracle, ulong Emitted)[0];

    static void Check(bool ok, string what)
    {
        if (!ok)
        {
            Console.WriteLine("FAILED: " + what);
            failed = true;
        }
        else
        {
            Console.WriteLine("OK: " + what);
        }
    }

    static int Main()
    {
        TestFixedValidDataRows();
        if (failed)
        {
            Console.WriteLine("VALID-DATA FAIL");
            return 1;
        }
        Console.WriteLine("VALID-DATA PASS");
        return 0;
    }
}
`

var (
	validDataGenOnce sync.Once
	validDataGenDir  string
	validDataGenErr  error
)

func TestCSharpFixedValidDataRows(t *testing.T) {
	requireValidDataSlow(t)
	checks := validDataChecksSource(t)
	if strings.Count(checks, validDataBoolToken) != 1 {
		t.Fatalf("the bool check's expected-value token appears %d times, want 1", strings.Count(checks, validDataBoolToken))
	}
	for _, row := range csharpValidDataRows {
		if !strings.Contains(checks, "static void "+row.method+"(") {
			t.Fatalf("FixedFormChecks.cs does not define %s", row.method)
		}
	}
	out, code := runCSharpValidData(t, checks)
	if code != 0 || strings.Contains(out, "FAILED:") || !strings.Contains(out, "VALID-DATA PASS") {
		t.Fatalf("valid-data rows exit %d\n%s", code, out)
	}
	for _, row := range csharpValidDataRows {
		if !validDataOK(out, row.id) {
			t.Fatalf("missing OK for %s\n%s", row.id, out)
		}
	}
}

func TestCSharpFixedValidDataRowsNegativeControl(t *testing.T) {
	requireValidDataSlow(t)
	checks := validDataChecksSource(t)
	if strings.Count(checks, validDataBoolToken) != 1 {
		t.Fatalf("negative control token appears %d times, want 1", strings.Count(checks, validDataBoolToken))
	}
	if strings.Count(checks, "static void TestFixedValidDataBool(") != 1 {
		t.Fatal("the real TestFixedValidDataBool check is not in FixedFormChecks.cs")
	}
	faulted := strings.Replace(checks, validDataBoolToken, validDataBoolFault, 1)
	if strings.Count(faulted, "static void TestFixedValidDataBool(") != 1 {
		t.Fatal("the negative control deleted TestFixedValidDataBool")
	}
	if strings.Contains(faulted, validDataBoolToken) || !strings.Contains(faulted, validDataBoolFault) {
		t.Fatal("the negative control did not plant the fault in the check")
	}
	out, code := runCSharpValidData(t, faulted)
	if code == 0 || strings.Contains(out, "VALID-DATA PASS") || !strings.Contains(out, "FAILED: bool-values/cs/valid-data") {
		t.Fatalf("negative control stayed green, exit %d\n%s", code, out)
	}
}

func requireValidDataSlow(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "dotnet (the C# toolchain)")
	slow := os.Getenv("SCHEMA_SLOW") != ""
	required := os.Getenv("SCHEMA_REQUIRE_CORPUS") != ""
	if !slow && required {
		t.Fatal("SCHEMA_REQUIRE_CORPUS is set and SCHEMA_SLOW is not")
	}
	if !slow {
		t.Skip("SCHEMA_SLOW is unset")
	}
}

func validDataOK(out, id string) bool {
	prefix := "OK: " + id
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func validDataChecksSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(validDataRepoRoot(t), "test", "cs-tables", "src", "FixedFormChecks.cs")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func validDataRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func runCSharpValidData(t *testing.T, checks string) (string, int) {
	t.Helper()
	gen := validDataGenerated(t)
	dir := t.TempDir()
	if err := copyValidDataTree(gen, dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "FixedFormChecks.cs"), []byte(checks), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ValidDataDriver.cs"), []byte(validDataDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	dotnet, csc, refs, version := validDataDotnet(t)
	sources := validDataCSFiles(dir)
	if len(sources) == 0 {
		t.Fatal("no C# sources to compile")
	}
	dll := filepath.Join(dir, "probe.dll")
	args := []string{"exec", csc, "-nologo", "-unsafe", "-nullable:annotations", "-out:" + dll}
	args = append(args, sources...)
	dlls, err := filepath.Glob(filepath.Join(refs, "*.dll"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dlls) == 0 {
		t.Fatalf("no reference assemblies in %s", refs)
	}
	for _, ref := range dlls {
		args = append(args, "-r:"+ref)
	}
	compileCtx, compileCancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer compileCancel()
	cmd := exec.CommandContext(compileCtx, dotnet, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("csc failed: %v\n%s", err, out)
	}
	rc := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}", version)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(rc), 0o600); err != nil {
		t.Fatal(err)
	}
	runCtx, runCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer runCancel()
	run := exec.CommandContext(runCtx, dotnet, "exec", dll)
	out, err := run.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("dotnet exec: %v\n%s", err, out)
		}
	}
	return string(out), code
}

func validDataGenerated(t *testing.T) string {
	t.Helper()
	validDataGenOnce.Do(func() {
		validDataGenDir, validDataGenErr = generateValidDataCS()
	})
	if validDataGenErr != nil {
		t.Fatal(validDataGenErr)
	}
	return validDataGenDir
}

func generateValidDataCS() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	sources := []string{
		"test/tables/FX1.schema",
		"test/tables/FX2.schema",
		"test/tables/V1.schema",
		"test/tables/V2.schema",
		"test/tables/P1.schema",
		"test/tables/P3.schema",
		"test/tables/Scalars2.schema",
		"test/tables/UT.schema",
		"tables/examples",
	}
	dir, err := os.MkdirTemp("", "cs-valid-data-gen-")
	if err != nil {
		return "", err
	}
	for _, src := range sources {
		paths, err := compiler.GatherPaths([]string{filepath.Join(root, src)})
		if err != nil {
			return "", fmt.Errorf("gather %s: %w", src, err)
		}
		c := compiler.New()
		u, err := c.Load(paths)
		if err != nil {
			return "", fmt.Errorf("load %s: %w", src, err)
		}
		files, err := c.Generate(u, "cs", compiler.Options{})
		if err != nil {
			return "", fmt.Errorf("generate %s: %w", src, err)
		}
		unitDir := filepath.Join(dir, unitDirName(src))
		for name, data := range files {
			if !keepValidDataCS(name) {
				continue
			}
			path := filepath.Join(unitDir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return "", err
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				return "", err
			}
		}
	}
	ser, pair, err := findValidDataSerialize(root)
	if err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(dir, "Serialize.cs"), ser); err != nil {
		return "", err
	}
	if err := copyFile(filepath.Join(dir, "Int128Pair.cs"), pair); err != nil {
		return "", err
	}
	return dir, nil
}

func unitDirName(src string) string {
	base := filepath.Base(src)
	base = strings.TrimSuffix(base, ".schema")
	return strings.ReplaceAll(base, string(os.PathSeparator), "_")
}

func keepValidDataCS(name string) bool {
	return strings.HasSuffix(filepath.Base(name), ".cs")
}

func findValidDataSerialize(root string) (string, string, error) {
	var candidates []string
	if s := os.Getenv("SERIALIZE"); s != "" {
		candidates = append(candidates, s, filepath.Join(s, "serialize.cs"))
	}
	candidates = append(candidates,
		filepath.Join(root, "..", "serialize.cs"),
		filepath.Join(root, "..", "serialize"),
	)
	for _, c := range candidates {
		ser := filepath.Join(c, "src", "Serialize.cs")
		pair := filepath.Join(c, "src", "Int128Pair.cs")
		if fileExists(ser) && fileExists(pair) {
			return ser, pair, nil
		}
		ser = filepath.Join(c, "Serialize.cs")
		pair = filepath.Join(c, "Int128Pair.cs")
		if fileExists(ser) && fileExists(pair) {
			return ser, pair, nil
		}
	}
	return "", "", fmt.Errorf("serialize.cs sources not found (set SERIALIZE)")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func copyFile(dst, src string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

func copyValidDataTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		return copyFile(target, path)
	})
}

func validDataCSFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".cs") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func validDataDotnet(t *testing.T) (dotnet, csc, refs, version string) {
	t.Helper()
	dotnet = os.Getenv("DOTNET")
	if dotnet == "" {
		found, err := exec.LookPath("dotnet")
		if err != nil {
			t.Fatalf("dotnet is not on PATH: %v", err)
		}
		dotnet = found
	}
	verOut, err := exec.Command(dotnet, "--version").Output()
	if err != nil {
		t.Fatalf("dotnet --version: %v", err)
	}
	version = strings.TrimSpace(string(verOut))
	resolved, err := filepath.EvalSymlinks(dotnet)
	if err != nil {
		t.Fatalf("resolve dotnet: %v", err)
	}
	root := filepath.Dir(resolved)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	refDirs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net10.0"))
	if len(refDirs) == 0 {
		t.Fatalf("no net10.0 reference pack under %s", root)
	}
	frameworks, _ := filepath.Glob(filepath.Join(root, "shared", "Microsoft.NETCore.App", "*"))
	if len(frameworks) == 0 {
		t.Fatalf("no shared framework under %s", root)
	}
	csc = pickVersionPath(cscs, version)
	refs = pickVersionPath(refDirs, version)
	version = filepath.Base(pickVersionPath(frameworks, version))
	return dotnet, csc, refs, version
}

func pickVersionPath(paths []string, version string) string {
	needle := string(os.PathSeparator) + version + string(os.PathSeparator)
	for _, p := range paths {
		if strings.Contains(p, needle) || strings.HasSuffix(p, version) {
			return p
		}
	}
	sort.Strings(paths)
	return paths[len(paths)-1]
}
