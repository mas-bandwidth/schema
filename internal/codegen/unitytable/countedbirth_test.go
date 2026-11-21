package unitytable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE BIRTH VALUE OF A COUNTED ARRAY IN C# (unity) (docs/SPEC-TABLES.md
// §22.2, the [M..N]T row; finding F-115). A `few [2..4]T` is born holding two
// default elements, and the writers elide exactly that value: the oracle is
// the Go engine (internal/tablewire encodeArray's allBirth and messageElides),
// whose bytes are pinned below. A record element is at its default when its
// body is its terminator alone; before this fix the Unity writers spelled a
// birth of records out in full. Two elements with one live ride, and so do
// three default elements. The pinned matrix carries the same rows for every
// element kind (test/verify-tablegen/pinned.go, pin:ci32:* and pin:crec:*).
func TestUnityCountedArrayBirthValue(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect counted-array birth value")
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, unityCountedBirthSchema), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityCountedBirthMain, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	for _, row := range []string{
		"fresh_counts",
		"i32_birth_msg", "i32_birth_var", "i32_min_one_msg", "i32_min_one_var", "i32_min_plus1_msg",
		"rec_birth_msg", "rec_birth_var", "rec_min_one_msg", "rec_min_one_var", "rec_min_plus1_msg", "rec_min_plus1_var",
	} {
		if !strings.Contains(out, "ROW "+row+": ok") {
			t.Errorf("row %s is not held:\n%s", row, out)
		}
	}
	if code != 0 && !t.Failed() {
		t.Fatalf("probe exited %d:\n%s", code, out)
	}
}

// the schema and its bytes are the Go engine's (the same unit as
// internal/codegen/cstable's TestCSharpCountedArrayBirthValue)
const unityCountedBirthSchema = `package probe

type Rec
{
    n int32 = 55
    on bool
}

fixed table FixRoot
{
    few [2..4]int32
}

table VarRoot
{
    few [2..4]int32
}

fixed table FixRec
{
    few [2..4]Rec
}

table VarRec
{
    few [2..4]Rec
}
`

const unityCountedBirthMain = `using System;

static class ProbeMain
{
    static int bad = 0;
    static void Row(string name, string got, string want)
    {
        bool ok = got == want;
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED got " + got + " want " + want));
        if (!ok) { bad++; }
    }
    static string Hex(byte[] b, long n) { return n < 0 ? "refused" : BitConverter.ToString(b, 0, (int)n).Replace("-", "").ToLowerInvariant(); }
    static string Msg(Probe.FixRoot v) { var buf = new byte[256]; return Hex(buf, Probe.Schema.FixRootSaveMessages(new Probe.FixRoot[] { v }, buf)); }
    static string Msg(Probe.FixRec v) { var buf = new byte[256]; return Hex(buf, Probe.Schema.FixRecSaveMessages(new Probe.FixRec[] { v }, buf)); }
    static string Var(Probe.VarRoot v) { var buf = new byte[Probe.Schema.VarRootMeasure(ref v)]; return Hex(buf, Probe.Schema.VarRootSave(ref v, buf)); }
    static string Var(Probe.VarRec v) { var buf = new byte[Probe.Schema.VarRecMeasure(ref v)]; return Hex(buf, Probe.Schema.VarRecSave(ref v, buf)); }

    static int Main()
    {
        Row("fresh_counts", "" + Probe.Schema.NewFixRoot().FewCount + Probe.Schema.NewVarRoot().FewCount + Probe.Schema.NewFixRec().FewCount + Probe.Schema.NewVarRec().FewCount, "2222");

        Row("i32_birth_msg", Msg(Probe.Schema.NewFixRoot()), "020000");
        Row("i32_birth_var", Var(Probe.Schema.NewVarRoot()), "01000000000000000000");
        var fi = Probe.Schema.NewFixRoot(); fi.Few[1] = 7;
        Row("i32_min_one_msg", Msg(fi), "020002000000e000000000");
        var vi = Probe.Schema.NewVarRoot(); vi.Few[1] = 7;
        Row("i32_min_one_var", Var(vi), "01010e0a0402000000000700000000198ceafe187dc6dc0100000000000000");
        var f3 = Probe.Schema.NewFixRoot(); f3.FewCount = 3;
        Row("i32_min_plus1_msg", Msg(f3), "02000a000000000000000000000000");

        Row("rec_birth_msg", Msg(Probe.Schema.NewFixRec()), "020000");
        Row("rec_birth_var", Var(Probe.Schema.NewVarRec()), "01000000000000000000");
        var fr = Probe.Schema.NewFixRec(); var fe = fr.Few[1]; fe.N = 56; fr.Few[1] = fe;
        Row("rec_min_one_msg", Msg(fr), "020001c30100000000");
        var vr = Probe.Schema.NewVarRec(); var ve = vr.Few[1]; ve.N = 56; vr.Few[1] = ve;
        Row("rec_min_one_var", Var(vr), "01010e0c0d020100070204380000000000198ceafe187dc6dc71f801864ce363af0200000000000000");
        var fr3 = Probe.Schema.NewFixRec(); fr3.FewCount = 3;
        Row("rec_min_plus1_msg", Msg(fr3), "0200090000");
        var vr3 = Probe.Schema.NewVarRec(); vr3.FewCount = 3;
        Row("rec_min_plus1_var", Var(vr3), "01010e080d0301000100010000198ceafe187dc6dc0100000000000000");
        return bad;
    }
}
`
