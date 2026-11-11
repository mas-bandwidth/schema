package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCSharpTablesCheckedBuild holds the rule that the generated C# table
// sources read, save and round-trip valid data identically whether the
// consuming project compiles them unchecked (the language default) or checked
// (CheckForOverflowUnderflow). The unit is generated here, compiled by the
// SDK's own compiler with -checked+, and run: every row must complete without
// an OverflowException and round-trip byte for byte.
func TestCSharpTablesCheckedBuild(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")
	out, code := runCSharpCheckedBuild(t, csharpCheckedBuildFiles(t), true)
	for _, row := range []string{"fixed_roundtrip", "variable_roundtrip", "json_negative"} {
		if !strings.Contains(out, "ROW "+row+": ok") {
			t.Fatalf("%s did not pass under -checked+ (exit %d)\n%s", row, code, out)
		}
	}
	if code != 0 || !strings.Contains(out, "CHECKED-BUILD PASS") {
		t.Fatalf("checked build exit %d\n%s", code, out)
	}
}

// TestCSharpTablesCheckedBuildNegativeControl strips the unchecked marking from
// one fixed-load site in a copy of the generated artifact. The checked build
// must then go red by that row's name, so the assertions above are known to be
// able to fail.
func TestCSharpTablesCheckedBuildNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK")
	files := csharpCheckedBuildFiles(t)
	const site = "case 0: t.A = unchecked((sbyte)v);"
	const bare = "case 0: t.A = (sbyte)v;"
	found := false
	for name, data := range files {
		if strings.Contains(string(data), site) {
			files[name] = []byte(strings.Replace(string(data), site, bare, 1))
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the generated artifact has no fixed-load site %q", site)
	}
	out, code := runCSharpCheckedBuild(t, files, true)
	if code == 0 || strings.Contains(out, "CHECKED-BUILD PASS") {
		t.Fatalf("negative control stayed green (exit %d)\n%s", code, out)
	}
	if !strings.Contains(out, "ROW fixed_roundtrip: FAILED") {
		t.Fatalf("negative control did not fail by name\n%s", out)
	}
}

const csharpCheckedBuildSchema = `package checkedprobe

fixed table Fix {
    a int8
    b int32
    c int64
}

table Var {
    big uint32
    small int32
    label string(512)
    tags map[uint32]int32
    data *bytes
}
`

func csharpCheckedBuildFiles(t *testing.T) map[string][]byte {
	t.Helper()
	u := unitFrom(t, csharpCheckedBuildSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return files
}

const csharpCheckedBuildDriver = `using System;
using Checkedprobe;

static class CheckedBuildProbe
{
    static int bad;

    static void Ok(string row) { Console.WriteLine("ROW " + row + ": ok"); }
    static void Fail(string row, Exception e) { Console.WriteLine("ROW " + row + ": FAILED " + e.GetType().Name + ": " + e.Message); bad++; }
    static void Fail(string row, string what) { Console.WriteLine("ROW " + row + ": FAILED " + what); bad++; }

    static bool Same(byte[] a, byte[] b)
    {
        if (a.Length != b.Length) { return false; }
        for (int i = 0; i < a.Length; ++i) { if (a[i] != b[i]) { return false; } }
        return true;
    }

    static void Run(string row, Action body)
    {
        try { body(); Ok(row); } catch (Exception e) { Fail(row, e); }
    }

    static byte[] FileSave(Var value)
    {
        long n = Schema.VarMeasure(value);
        byte[] buffer = new byte[n];
        if (Schema.VarSave(value, buffer) != n) { throw new Exception("save returned a different size"); }
        return buffer;
    }

    static void SameVar(Var a, Var b)
    {
        if (a.Big != b.Big || a.Small != b.Small) { throw new Exception("scalars differ"); }
        if (a.LabelLength != b.LabelLength) { throw new Exception("label length differs"); }
        for (int i = 0; i < a.LabelLength; ++i) { if (a.Label[i] != b.Label[i]) { throw new Exception("label bytes differ"); } }
        if (a.TagsCount != b.TagsCount) { throw new Exception("map count differs"); }
        for (int i = 0; i < a.TagsCount; ++i)
        {
            if (a.Tags[i].Key != b.Tags[i].Key || a.Tags[i].Value != b.Tags[i].Value) { throw new Exception("map entry differs"); }
        }
        if ((a.Data == null) != (b.Data == null)) { throw new Exception("blob presence differs"); }
        if (a.Data != null && !Same(a.Data.Data, b.Data.Data)) { throw new Exception("blob bytes differ"); }
    }

    static void FixedRoundtrip()
    {
        var value = new Fix { A = -8, B = -1234567, C = -9876543210L };
        long n = Schema.FixFixedMeasure(1);
        byte[] buffer = new byte[n];
        if (Schema.FixFixedSave(value, buffer) != n) { throw new Exception("fixed save returned a different size"); }
        var back = new Fix();
        var report = new TableReport();
        var plan = new TableFixedEntry[1024];
        if (Schema.FixFixedLoad(back, buffer, plan, report) != 1) { throw new Exception("fixed load did not return one record"); }
        if (report.Refused || report.Damaged) { throw new Exception("fixed load reported a refusal or damage"); }
        if (back.A != value.A || back.B != value.B || back.C != value.C) { throw new Exception("fixed scalars differ"); }
        byte[] again = new byte[n];
        if (Schema.FixFixedSave(back, again) != n || !Same(buffer, again)) { throw new Exception("fixed round trip bytes differ"); }
    }

    static byte[] Blob(int length)
    {
        byte[] data = new byte[length];
        for (int i = 0; i < length; ++i) { data[i] = (byte)((i * 7) & 0xff); }
        return data;
    }

    static void VariableRoundtrip()
    {
        var value = new Var { Big = 1000000u, Small = -123456 };
        value.LabelLength = 300;
        for (int i = 0; i < value.LabelLength; ++i) { value.Label[i] = (byte)('a' + i % 26); }
        value.Tags = new VarTagsEntry[] {
            new VarTagsEntry { Key = 200u, Value = -9 },
            new VarTagsEntry { Key = 300u, Value = 70000 },
            new VarTagsEntry { Key = 400u, Value = 500 },
        };
        value.TagsCount = 3;
        value.Data = new TableBlob(Blob(300));
        byte[] wire = FileSave(value);
        var back = new Var();
        var report = new TableReport();
        if (!Schema.VarLoad(back, wire, report)) { throw new Exception("load refused valid data"); }
        SameVar(value, back);
        if (!Same(wire, FileSave(back))) { throw new Exception("variable round trip bytes differ"); }
    }

    static void JsonNegative()
    {
        var value = new Var { Big = 200u, Small = -7 };
        value.LabelLength = 300;
        for (int i = 0; i < value.LabelLength; ++i) { value.Label[i] = (byte)('A' + i % 26); }
        value.Tags = new VarTagsEntry[] { new VarTagsEntry { Key = 5u, Value = -42 } };
        value.TagsCount = 1;
        value.Data = new TableBlob(Blob(300));
        long n = Schema.VarToJsonMeasure(value);
        byte[] text = new byte[n];
        if (Schema.VarToJson(value, text) != n) { throw new Exception("to-json returned a different size"); }
        var back = new Var();
        var report = new TableReport();
        if (!Schema.VarFromJson(back, text, report)) { throw new Exception("from-json refused valid text"); }
        if (back.Small != value.Small || back.LabelLength != value.LabelLength || back.TagsCount != 1 || back.Tags[0].Value != -42) { throw new Exception("json scalars differ"); }
        if (back.Data == null || !Same(back.Data.Data, value.Data.Data)) { throw new Exception("json blob differs"); }
        long ren = Schema.VarToJsonMeasure(back);
        byte[] retext = new byte[ren];
        if (Schema.VarToJson(back, retext) != ren || !Same(text, retext)) { throw new Exception("json round trip bytes differ"); }

        var fixedValue = new Fix { A = -1, B = -2, C = -3 };
        long fn = Schema.FixToJsonMeasure(fixedValue);
        byte[] fixedText = new byte[fn];
        if (Schema.FixToJson(fixedValue, fixedText) != fn) { throw new Exception("fixed to-json returned a different size"); }
        var fixedBack = new Fix();
        var fixedReport = new TableReport();
        if (!Schema.FixFromJson(fixedBack, fixedText, fixedReport)) { throw new Exception("fixed from-json refused valid text"); }
        if (fixedBack.A != fixedValue.A || fixedBack.B != fixedValue.B || fixedBack.C != fixedValue.C) { throw new Exception("fixed json scalars differ"); }
        long frn = Schema.FixToJsonMeasure(fixedBack);
        byte[] fixedRetext = new byte[frn];
        if (Schema.FixToJson(fixedBack, fixedRetext) != frn || !Same(fixedText, fixedRetext)) { throw new Exception("fixed json round trip bytes differ"); }
    }

    public static int Main()
    {
        Run("fixed_roundtrip", FixedRoundtrip);
        Run("variable_roundtrip", VariableRoundtrip);
        Run("json_negative", JsonNegative);
        Console.WriteLine(bad == 0 ? "CHECKED-BUILD PASS" : "CHECKED-BUILD FAIL");
        return bad == 0 ? 0 : 1;
    }
}
`

func runCSharpCheckedBuild(t *testing.T, files map[string][]byte, checked bool) (string, int) {
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
	driver := filepath.Join(dir, "CheckedBuildProbe.cs")
	if err := os.WriteFile(driver, []byte(csharpCheckedBuildDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)

	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
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
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	compileOut, compileErr := compile.CombinedOutput()
	if compileErr != nil {
		return string(compileOut), 1
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "checkedbuild.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	runOut, runErr := run.CombinedOutput()
	code := 0
	if runErr != nil {
		code = 1
	}
	return string(compileOut) + string(runOut), code
}
