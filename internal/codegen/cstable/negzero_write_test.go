package cstable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE VARIABLE WRITER STORES +0 FOR A COMPRESSED FLOAT'S -0 (docs/SPEC-TABLES.md
// §3.3, ruled 2026-11-10 fixed-compressed-negzero): the same bytes it stores
// for +0, on every C# (modern) path that writes the variable form. A clean
// leaf (the typed writer) under a non-zero default and under a +0 default
// (where -0 IS the default and is elided, as the reference elides it), an
// array element (the descriptor walk), and a value saved
// from a native region (the region writer). A plain float keeps its -0.
func TestCSharpVariableWriterStoresCompressedNegZeroAsPositive(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C# writer)")
	out, err := csNegZeroWriteRun(t)
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for _, row := range []string{
		"leaf_nonzero_default",
		"leaf_zero_default_elided",
		"array_element",
		"region_writer",
		"plain_float_keeps_negzero",
	} {
		if !strings.Contains(string(out), "ROW "+row+": ok") {
			t.Errorf("row %s is not held:\n%s", row, out)
		}
	}
}

const csNegZeroWriteSchema = `package nzw

table Var
{
    a  float32 = 0.05 | min = 0, max = 0.1, resolution = 0.01
    z  float32 | min = 0, max = 0.1, resolution = 0.01
    xs [2]float32 | min = 0, max = 0.1, resolution = 0.01
    p  float32
}
`

const csNegZeroWriteMain = `using System;
using System.Runtime.InteropServices;
using Nzw;

static class ProbeMain
{
    static int Row(string name, bool ok, string why)
    {
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED " + why));
        return ok ? 0 : 1;
    }
    static byte[] Save(Var v)
    {
        byte[] buf = new byte[Schema.VarMeasure(v)];
        long n = Schema.VarSave(v, buf);
        return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
    }
    static string Hex(byte[] b) { return Convert.ToHexString(b); }
    static int Same(string name, Var neg, Var pos)
    {
        byte[] a = Save(neg), b = Save(pos);
        return Row(name, a.Length > 0 && a.AsSpan().SequenceEqual(b), Hex(a) + " vs " + Hex(b));
    }
    static unsafe byte[] ViaRegion(byte[] image)
    {
        long cap = 1 << 16;
        IntPtr region = Marshal.AllocHGlobal((IntPtr)cap);
        try
        {
            var rep = new TableReport();
            if (Schema.VarLoad(region, cap, image, rep) == null) { return new byte[0]; }
            byte[] buf = new byte[Schema.VarMeasure(region)];
            long n = Schema.VarSave(region, buf);
            return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
        }
        finally { Marshal.FreeHGlobal(region); }
    }
    // the image of a value whose a field is -0, made by writing a = 0.07 and
    // storing -0's bits over the one place its four bytes sit
    static byte[] NegZeroImage()
    {
        var v = new Var(); v.A = 0.07f;
        byte[] b = Save(v);
        byte[] pat = BitConverter.GetBytes(0.07f);
        int at = b.AsSpan().IndexOf(pat);
        if (at < 0 || b.AsSpan(at + 1).IndexOf(pat) >= 0) { return new byte[0]; }
        BitConverter.GetBytes(0x80000000u).CopyTo(b, at);
        return b;
    }

    static int Main()
    {
        int bad = 0;
        { var n = new Var(); n.A = -0.0f; var p = new Var(); p.A = 0.0f; bad += Same("leaf_nonzero_default", n, p); }
        { var n = new Var(); n.Z = -0.0f; bad += Same("leaf_zero_default_elided", n, new Var()); }
        { var n = new Var(); n.Xs[0] = -0.0f; var p = new Var(); p.Xs[0] = 0.0f; bad += Same("array_element", n, p); }
        {
            var p = new Var(); p.A = 0.0f;
            byte[] image = NegZeroImage();
            byte[] a = image.Length > 0 ? ViaRegion(image) : new byte[0], b = ViaRegion(Save(p));
            bad += Row("region_writer", a.Length > 0 && a.AsSpan().SequenceEqual(b), "image=" + Hex(image) + " " + Hex(a) + " vs " + Hex(b));
        }
        {
            var n = new Var(); n.P = -0.0f;
            byte[] a = Save(n), b = Save(new Var());
            bad += Row("plain_float_keeps_negzero", a.Length > b.Length && a.AsSpan().IndexOf(BitConverter.GetBytes(0x80000000u)) >= 0, Hex(a) + " vs " + Hex(b));
        }
        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`

func csNegZeroWriteRun(t *testing.T) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	u := unitFrom(t, csNegZeroWriteSchema)
	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	runtimeDir := os.Getenv("SERIALIZE_CS")
	if runtimeDir == "" {
		runtimeDir = "../../../../serialize.cs"
	}
	runtimeDir, err = filepath.Abs(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		path := filepath.Join(runtimeDir, "src", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the C# runtime checkout: %v", err)
		}
		sources = append(sources, path)
	}
	main := filepath.Join(dir, "NegZeroWrite.cs")
	if err := os.WriteFile(main, []byte(csNegZeroWriteMain), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "csnegzerowrite.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-nowarn:CS8500", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	args = append(args, main)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	compileOut, compileErr := compile.CombinedOutput()
	if compileErr != nil {
		return compileOut, fmt.Errorf("compile: %w", compileErr)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "csnegzerowrite.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	runOut, runErr := run.CombinedOutput()
	return append(compileOut, runOut...), runErr
}
