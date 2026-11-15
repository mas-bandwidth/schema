package csharp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCSharpPacketCheckedBuild holds the rule that the generated C# packet
// sources read and write valid data identically whether the consuming project
// compiles them unchecked (the language default) or checked
// (CheckForOverflowUnderflow). The unit is generated here, compiled by the
// SDK's own compiler with -checked+, and run: every row must complete without
// an OverflowException and round-trip.
func TestCSharpPacketCheckedBuild(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C# packet probe)")
	files := packetCheckedBuildFiles(t)
	out, code := runPacketCheckedBuild(t, files, true)
	for _, row := range []string{
		"scalar_negative", "scalar_min", "packed_negative", "packed_min",
		"ranged_negative", "ranged_max",
	} {
		if !strings.Contains(out, "ROW "+row+": ok") {
			t.Fatalf("%s did not pass under -checked+ (exit %d)\n%s", row, code, out)
		}
	}
	if code != 0 || !strings.Contains(out, "CHECKED-BUILD PASS") {
		t.Fatalf("checked build exit %d\n%s", code, out)
	}
}

// TestCSharpPacketCheckedBuildNegativeControl strips the unchecked marking from
// the signed 64-bit read in a copy of the generated artifact. The checked
// build must then go red by that row's name, so the assertions above are known
// to be able to fail.
func TestCSharpPacketCheckedBuildNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C# packet probe)")
	files := packetCheckedBuildFiles(t)
	const site = "value.I64 = unchecked((long)rawValue);"
	const bare = "value.I64 = (long)rawValue;"
	found := false
	for name, data := range files {
		if strings.Contains(string(data), site) {
			files[name] = []byte(strings.Replace(string(data), site, bare, 1))
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the generated artifact has no signed 64-bit read site %q", site)
	}
	out, code := runPacketCheckedBuild(t, files, true)
	if code == 0 || strings.Contains(out, "CHECKED-BUILD PASS") {
		t.Fatalf("negative control stayed green (exit %d)\n%s", code, out)
	}
	if !strings.Contains(out, "ROW scalar_min: FAILED") && !strings.Contains(out, "ROW scalar_negative: FAILED") {
		t.Fatalf("negative control did not fail by name\n%s", out)
	}
}

const packetCheckedBuildSchema = `package checkedpacket

type Scalar {
    align
    i8 int8
    align
    i16 int16
    align
    i32 int32
    align
    i64 int64
}

type Packed {
    i8 int8
    i16 int16
    i32 int32
    i64 int64
}

type Ranged {
    range int32 | min = -1000, max = 1000
    q float32 | min = -180, max = 180, resolution = 0.01
}
`

func packetCheckedBuildFiles(t *testing.T) map[string][]byte {
	t.Helper()
	return generateCs(t, "CheckedPacket", packetCheckedBuildSchema)
}

const packetCheckedBuildDriver = `using System;
using Serialize;
using Checkedpacket;

static class PacketCheckedBuildProbe
{
    static int bad;

    static void Ok(string row) { Console.WriteLine("ROW " + row + ": ok"); }
    static void Fail(string row, Exception e) { Console.WriteLine("ROW " + row + ": FAILED " + e.GetType().Name + ": " + e.Message); bad++; }
    static void Fail(string row, string what) { Console.WriteLine("ROW " + row + ": FAILED " + what); bad++; }

    static void Run(string row, Action body)
    {
        try { body(); Ok(row); } catch (Exception e) { Fail(row, e); }
    }

    static void SameScalar(Scalar a, Scalar b)
    {
        if (a.I8 != b.I8) { throw new Exception("i8 differs"); }
        if (a.I16 != b.I16) { throw new Exception("i16 differs"); }
        if (a.I32 != b.I32) { throw new Exception("i32 differs"); }
        if (a.I64 != b.I64) { throw new Exception("i64 differs"); }
    }

    static void ScalarRoundtrip(Scalar value)
    {
        byte[] buffer = new byte[Schema.ScalarMaxBytes];
        var writer = new WriteStream(buffer);
        if (!Schema.WriteScalar(writer, ref value)) { throw new Exception("write refused valid data"); }
        writer.Flush();
        var back = new Scalar();
        var reader = new ReadStream(writer.Data.Slice(0, (int)writer.BytesProcessed).ToArray());
        if (!Schema.ReadScalar(reader, ref back)) { throw new Exception("read refused valid data"); }
        SameScalar(value, back);
    }

    static void SamePacked(Packed a, Packed b)
    {
        if (a.I8 != b.I8) { throw new Exception("i8 differs"); }
        if (a.I16 != b.I16) { throw new Exception("i16 differs"); }
        if (a.I32 != b.I32) { throw new Exception("i32 differs"); }
        if (a.I64 != b.I64) { throw new Exception("i64 differs"); }
    }

    static void PackedRoundtrip(Packed value)
    {
        byte[] buffer = new byte[Schema.PackedMaxBytes];
        var writer = new WriteStream(buffer);
        if (!Schema.WritePacked(writer, ref value)) { throw new Exception("write refused valid data"); }
        writer.Flush();
        var back = new Packed();
        var reader = new ReadStream(writer.Data.Slice(0, (int)writer.BytesProcessed).ToArray());
        if (!Schema.ReadPacked(reader, ref back)) { throw new Exception("read refused valid data"); }
        SamePacked(value, back);
    }

    static void RangedRoundtrip(Ranged value)
    {
        byte[] buffer = new byte[Schema.RangedMaxBytes];
        var writer = new WriteStream(buffer);
        if (!Schema.WriteRanged(writer, ref value)) { throw new Exception("write refused valid data"); }
        writer.Flush();
        var back = new Ranged();
        var reader = new ReadStream(writer.Data.Slice(0, (int)writer.BytesProcessed).ToArray());
        if (!Schema.ReadRanged(reader, ref back)) { throw new Exception("read refused valid data"); }
        if (back.Range != value.Range) { throw new Exception("range differs"); }
        if (Math.Abs(back.Q - value.Q) > 0.005f) { throw new Exception("quantized float differs"); }
    }

    static void ScalarNegative()
    {
        ScalarRoundtrip(new Scalar { I8 = -1, I16 = -2, I32 = -3, I64 = -4 });
    }

    static void ScalarMin()
    {
        ScalarRoundtrip(new Scalar { I8 = sbyte.MinValue, I16 = short.MinValue, I32 = int.MinValue, I64 = long.MinValue });
    }

    static void PackedNegative()
    {
        PackedRoundtrip(new Packed { I8 = -1, I16 = -2, I32 = -3, I64 = -4 });
    }

    static void PackedMin()
    {
        PackedRoundtrip(new Packed { I8 = sbyte.MinValue, I16 = short.MinValue, I32 = int.MinValue, I64 = long.MinValue });
    }

    static void RangedNegative()
    {
        RangedRoundtrip(new Ranged { Range = -1000, Q = -1.50f });
    }

    static void RangedMax()
    {
        RangedRoundtrip(new Ranged { Range = 1000, Q = 180.0f });
    }

    public static int Main()
    {
        Run("scalar_negative", ScalarNegative);
        Run("scalar_min", ScalarMin);
        Run("packed_negative", PackedNegative);
        Run("packed_min", PackedMin);
        Run("ranged_negative", RangedNegative);
        Run("ranged_max", RangedMax);
        Console.WriteLine(bad == 0 ? "CHECKED-BUILD PASS" : "CHECKED-BUILD FAIL");
        return bad == 0 ? 0 : 1;
    }
}
`

func runPacketCheckedBuild(t *testing.T, files map[string][]byte, checked bool) (string, int) {
	t.Helper()
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	runtimeDir := os.Getenv("SERIALIZE_CS")
	if runtimeDir == "" {
		runtimeDir = "../../../../serialize.cs"
	}
	runtimeDir, err := filepath.Abs(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		path := filepath.Join(runtimeDir, "src", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("serialize.cs runtime missing at %s: %v", path, err)
		}
		sources = append(sources, path)
	}

	driver := filepath.Join(dir, "PacketCheckedBuildProbe.cs")
	if err := os.WriteFile(driver, []byte(packetCheckedBuildDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)

	csc, refs, runtime := packetCheckedDotnetToolchain(t)
	dotnet := packetCheckedDotnet()
	app := filepath.Join(dir, "checkedbuild.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	if checked {
		args = append(args, "-checked+")
	}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = packetCheckedEnv(dir, dir), dir
	compileOut, compileErr := compile.CombinedOutput()
	if compileErr != nil {
		return string(compileOut), 1
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "checkedbuild.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = packetCheckedEnv(dir, dir), dir
	runOut, runErr := run.CombinedOutput()
	code := 0
	if runErr != nil {
		code = 1
	}
	return string(compileOut) + string(runOut), code
}

func packetCheckedDotnet() string {
	if dotnet := os.Getenv("DOTNET"); dotnet != "" {
		return dotnet
	}
	return "dotnet"
}

func packetCheckedDotnetToolchain(t *testing.T) (csc string, refs []string, runtime string) {
	t.Helper()
	path, err := exec.LookPath(packetCheckedDotnet())
	if err != nil {
		t.Fatalf("dotnet not found: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	root := filepath.Dir(resolved)
	cscs, _ := filepath.Glob(filepath.Join(root, "sdk", "*", "Roslyn", "bincore", "csc.dll"))
	if len(cscs) == 0 {
		t.Fatalf("no csc.dll under %s", root)
	}
	sort.Strings(cscs)
	csc = cscs[len(cscs)-1]
	refDirs, _ := filepath.Glob(filepath.Join(root, "packs", "Microsoft.NETCore.App.Ref", "*", "ref", "net*"))
	if len(refDirs) == 0 {
		t.Fatalf("no reference assemblies under %s", root)
	}
	sort.Strings(refDirs)
	refs, _ = filepath.Glob(filepath.Join(refDirs[len(refDirs)-1], "*.dll"))
	if len(refs) == 0 {
		t.Fatalf("no reference assemblies in %s", refDirs[len(refDirs)-1])
	}
	runtimes, _ := filepath.Glob(filepath.Join(root, "shared", "Microsoft.NETCore.App", "*"))
	if len(runtimes) == 0 {
		t.Fatalf("no shared runtime under %s", root)
	}
	sort.Strings(runtimes)
	runtime = filepath.Base(runtimes[len(runtimes)-1])
	return csc, refs, runtime
}

func packetCheckedEnv(home, tmp string) []string {
	env := make([]string, 0, len(os.Environ())+8)
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "HOME="),
			strings.HasPrefix(e, "TMPDIR="),
			strings.HasPrefix(e, "TMP="),
			strings.HasPrefix(e, "TEMP="),
			strings.HasPrefix(e, "DOTNET_"):
			continue
		}
		env = append(env, e)
	}
	return append(env,
		"DOTNET_NOLOGO=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"HOME="+home,
		"DOTNET_CLI_HOME="+home,
		"TMPDIR="+tmp,
		"TMP="+tmp,
		"TEMP="+tmp,
	)
}
