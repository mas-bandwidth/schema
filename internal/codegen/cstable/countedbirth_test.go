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

// THE BIRTH VALUE OF A COUNTED ARRAY IN C# (modern) (docs/SPEC-TABLES.md
// §22.2, the [M..N]T row; finding F-115). A `few [2..4]T` is born holding two
// default elements, and the writers elide exactly that value: the oracle is
// the Go engine (internal/tablewire encodeArray's allBirth and messageElides),
// whose bytes are pinned below. A record element is at its default when its
// body is its terminator alone; before this fix the C# writers spelled a
// birth of records out in full. Two elements with one live ride, and so do
// three default elements. The pinned matrix carries the same rows for every
// element kind (test/verify-tablegen/pinned.go, pin:ci32:* and pin:crec:*).
func TestCSharpCountedArrayBirthValue(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C# writers)")
	dir := t.TempDir()
	u := unitFrom(t, countedBirthSchema)
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
	main := filepath.Join(dir, "Birth.cs")
	if err := os.WriteFile(main, []byte(csCountedBirthMain), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "csbirth.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-nowarn:CS8500", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(append(args, sources...), main)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "csbirth.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	if err != nil || strings.Contains(string(out), "FAILED") {
		t.Fatalf("probe: %v\n%s", err, out)
	}
}

// countedBirthSchema and its bytes are the Go engine's: one-field roots of a
// counted int32 and a counted record, each on the message form (Fix*) and the
// variable form (Var*).
const countedBirthSchema = `package probe

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

const csCountedBirthMain = `using System;

static class ProbeMain
{
    static int bad = 0;
    static void Row(string name, string got, string want)
    {
        bool ok = got == want;
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED got " + got + " want " + want));
        if (!ok) { bad++; }
    }
    static string Hex(byte[] b, long n) { return n < 0 ? "refused" : Convert.ToHexString(b, 0, (int)n).ToLowerInvariant(); }
    static string Msg(Probe.FixRoot v) { var buf = new byte[256]; return Hex(buf, Probe.Schema.FixRootSaveMessages(new Probe.FixRoot[] { v }, buf)); }
    static string Msg(Probe.FixRec v) { var buf = new byte[256]; return Hex(buf, Probe.Schema.FixRecSaveMessages(new Probe.FixRec[] { v }, buf)); }
    static string Var(Probe.VarRoot v) { var buf = new byte[Probe.Schema.VarRootMeasure(v)]; return Hex(buf, Probe.Schema.VarRootSave(v, buf)); }
    static string Var(Probe.VarRec v) { var buf = new byte[Probe.Schema.VarRecMeasure(v)]; return Hex(buf, Probe.Schema.VarRecSave(v, buf)); }

    static int Main()
    {
        Row("fresh_counts", "" + new Probe.FixRoot().FewCount + new Probe.VarRoot().FewCount + new Probe.FixRec().FewCount + new Probe.VarRec().FewCount, "2222");

        Row("i32_birth_msg", Msg(new Probe.FixRoot()), "020000");
        Row("i32_birth_var", Var(new Probe.VarRoot()), "01000000000000000000");
        var fi = new Probe.FixRoot(); fi.Few[1] = 7;
        Row("i32_min_one_msg", Msg(fi), "020002000000e000000000");
        var vi = new Probe.VarRoot(); vi.Few[1] = 7;
        Row("i32_min_one_var", Var(vi), "01010e0a0402000000000700000000198ceafe187dc6dc0100000000000000");
        var f3 = new Probe.FixRoot(); f3.FewCount = 3;
        Row("i32_min_plus1_msg", Msg(f3), "02000a000000000000000000000000");

        Row("rec_birth_msg", Msg(new Probe.FixRec()), "020000");
        Row("rec_birth_var", Var(new Probe.VarRec()), "01000000000000000000");
        var fr = new Probe.FixRec(); fr.Few[1].N = 56;
        Row("rec_min_one_msg", Msg(fr), "020001c30100000000");
        var vr = new Probe.VarRec(); vr.Few[1].N = 56;
        Row("rec_min_one_var", Var(vr), "01010e0c0d020100070204380000000000198ceafe187dc6dc71f801864ce363af0200000000000000");
        var fr3 = new Probe.FixRec(); fr3.FewCount = 3;
        Row("rec_min_plus1_msg", Msg(fr3), "0200090000");
        var vr3 = new Probe.VarRec(); vr3.FewCount = 3;
        Row("rec_min_plus1_var", Var(vr3), "01010e080d0301000100010000198ceafe187dc6dc0100000000000000");
        return bad;
    }
}
`
