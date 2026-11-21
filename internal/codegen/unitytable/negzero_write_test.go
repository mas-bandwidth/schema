package unitytable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE VARIABLE WRITER STORES +0 FOR A COMPRESSED FLOAT'S -0 (docs/SPEC-TABLES.md
// §3.3, ruled 2026-11-10 fixed-compressed-negzero): the same bytes it stores
// for +0, where the value rides — a leaf under a non-zero default, and an
// element of an array that rides for another element. A plain float keeps its
// -0 bit-exact.
func TestUnityVariableWriterStoresCompressedNegZeroAsPositive(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect compressed -0 on the variable writer")
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, unityNegZeroWriteSchema), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityNegZeroWriteMain, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	for _, row := range []string{
		"leaf_nonzero_default",
		"leaf_zero_default_elided",
		"array_element",
		"plain_float_keeps_negzero",
	} {
		if !strings.Contains(out, "ROW "+row+": ok") {
			t.Errorf("row %s is not held:\n%s", row, out)
		}
	}
	if code != 0 && !t.Failed() {
		t.Fatalf("probe exited %d:\n%s", code, out)
	}
}

const unityNegZeroWriteSchema = `package nzw

table Var
{
    a  float32 = 0.05 | min = 0, max = 0.1, resolution = 0.01
    z  float32 | min = 0, max = 0.1, resolution = 0.01
    xs [2]float32 | min = 0, max = 0.1, resolution = 0.01
    p  float32
}
`

const unityNegZeroWriteMain = `using System;

static class ProbeMain
{
    static int Row(string name, bool ok, string why)
    {
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED " + why));
        return ok ? 0 : 1;
    }
    static byte[] Save(Nzw.Var v)
    {
        long size = Nzw.Schema.VarMeasure(ref v);
        if (size < 0) { return new byte[0]; }
        byte[] buf = new byte[size];
        long n = Nzw.Schema.VarSave(ref v, buf);
        return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray();
    }
    static string Hex(byte[] b) { return BitConverter.ToString(b).Replace("-", ""); }
    static int Same(string name, Nzw.Var neg, Nzw.Var pos)
    {
        byte[] a = Save(neg), b = Save(pos);
        return Row(name, a.Length > 0 && a.AsSpan().SequenceEqual(b), Hex(a) + " vs " + Hex(b));
    }
    static int Main()
    {
        int bad = 0;
        { var n = Nzw.Schema.NewVar(); n.A = -0.0f; var p = Nzw.Schema.NewVar(); p.A = 0.0f; bad += Same("leaf_nonzero_default", n, p); }
        { var n = Nzw.Schema.NewVar(); n.Z = -0.0f; bad += Same("leaf_zero_default_elided", n, Nzw.Schema.NewVar()); }
        {
            // xs[1] makes the array ride, so xs[0] is stored
            var n = Nzw.Schema.NewVar(); n.Xs[0] = -0.0f; n.Xs[1] = 0.05f;
            var p = Nzw.Schema.NewVar(); p.Xs[0] = 0.0f; p.Xs[1] = 0.05f;
            bad += Same("array_element", n, p);
        }
        {
            var n = Nzw.Schema.NewVar(); n.P = -0.0f;
            byte[] a = Save(n), b = Save(Nzw.Schema.NewVar());
            bad += Row("plain_float_keeps_negzero", a.Length > b.Length && a.AsSpan().IndexOf(BitConverter.GetBytes(0x80000000u)) >= 0, Hex(a) + " vs " + Hex(b));
        }
        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`
