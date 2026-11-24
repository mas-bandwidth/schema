package cstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD (docs/SPEC-TABLES.md §3.4, THE SLOTS
// PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED; test/tables/FixedSlack.schema
// states the recipe). Load file A (every counted array at its bound, every slot
// 77) into a value, then file B (every count 1) into THE SAME value: the value
// holds what a fresh value loaded from B holds, every slot past every count at
// its declared default (cells[1..2].id 7, every sub v 3, nums 0), never A's 77
// and never the writer's zeros. A refused load leaves the same defaults.
func TestCSharpFixedSlackPastCountHoldsDefaults(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Parallel()
	out, err := runFixedSlackProbe(t)
	if err != nil || !strings.Contains(out, "ROW fixed_slack: ok") {
		t.Fatalf("the slack probe did not go green (%v):\n%s", err, out)
	}
}

// THE EMITTER PIN, no toolchain: FixedSlack.schema's generated source carries
// the slack pass and its call on the load, and a unit whose counted arrays all
// hold elements with an all-zero default emits none of it (§2.2 zero cost).
func TestCSharpFixedSlackEmitterPin(t *testing.T) {
	t.Parallel()
	src := csGeneratedSource(t, csReadSchema(t, "FixedSlack"))
	for _, want := range []string{
		"private static void TableFixedSlack(SlackRoot value)",
		"private static void TableFixedSlack(SlackCell value)",
		"for (int i = value.CellsCount; i < value.Cells.Length; ++i)",
		"TableReset(value.Cells[i]);",
		"for (int i = value.SubsCount; i < value.Subs.Length; ++i)",
		"TableReset(value.Subs[i]);",
		"TableFixedSlack(value.Cells[i]);",
		"TableFixedSlack(values[k]);",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("FixedSlack's generated C# lacks %q", want)
		}
	}
	if strings.Contains(src, "TableFixedSlack(SlackSub value)") {
		t.Errorf("SlackSub holds no counted array and must get no slack pass")
	}
	zero := csGeneratedSource(t, `package zeroslack

fixed table ZeroCell
{
    id   int32
    flag bool
}

fixed table ZeroRoot
{
    cells [..3]ZeroCell
    nums  [..4]int32
}
`)
	if strings.Contains(zero, "TableFixedSlack") {
		t.Errorf("a unit whose counted arrays all hold all-zero element defaults must emit no slack pass")
	}
}

func csGeneratedSource(t *testing.T, schema string) string {
	t.Helper()
	files, err := Generate(unitFrom(t, schema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

const fixedSlackProbe = `using System;
using System.Reflection;
using Tblslack;

static class SlackProbe
{
    static int bad = 0;
    static void Fail(string what) { Console.WriteLine("FAIL " + what); bad++; }

    static byte[] Save(SlackRoot v)
    {
        byte[] buf = new byte[Schema.SlackRootFixedMeasure(1)];
        long w = Schema.SlackRootFixedSave(v, buf);
        if (w != buf.Length) { Fail("save wrote " + w); }
        return buf;
    }

    // file A: every counted array at its bound, every slot off its default
    static byte[] FileA()
    {
        var v = new SlackRoot();
        v.CellsCount = v.Cells.Length;
        for (int i = 0; i < v.Cells.Length; ++i)
        {
            v.Cells[i].Id = 77;
            v.Cells[i].SubsCount = v.Cells[i].Subs.Length;
            for (int j = 0; j < v.Cells[i].Subs.Length; ++j) { v.Cells[i].Subs[j].V = 77; }
        }
        v.NumsCount = v.Nums.Length;
        for (int i = 0; i < v.Nums.Length; ++i) { v.Nums[i] = 77; }
        return Save(v);
    }

    // file B: every count 1, every live value 77
    static byte[] FileB()
    {
        var v = new SlackRoot();
        v.CellsCount = 1;
        v.Cells[0].Id = 77;
        v.Cells[0].SubsCount = 1;
        v.Cells[0].Subs[0].V = 77;
        v.NumsCount = 1;
        v.Nums[0] = 77;
        return Save(v);
    }

    static string Snap(object value)
    {
        var sb = new System.Text.StringBuilder();
        Write(sb, value);
        return sb.ToString();
    }

    static void Write(System.Text.StringBuilder sb, object value)
    {
        if (value == null) { sb.Append("null"); return; }
        if (value is Array a)
        {
            sb.Append('[');
            for (int i = 0; i < a.Length; ++i) { if (i != 0) sb.Append(','); Write(sb, a.GetValue(i)); }
            sb.Append(']');
            return;
        }
        Type t = value.GetType();
        if (t.IsPrimitive || t.IsEnum) { sb.Append(Convert.ToString(value, System.Globalization.CultureInfo.InvariantCulture)); return; }
        sb.Append('{');
        FieldInfo[] fields = t.GetFields(BindingFlags.Public | BindingFlags.Instance);
        Array.Sort(fields, (x, y) => string.CompareOrdinal(x.Name, y.Name));
        foreach (var f in fields) { sb.Append(f.Name).Append('='); Write(sb, f.GetValue(value)); sb.Append(';'); }
        sb.Append('}');
    }

    // every slot past every count, against the declared defaults and a fresh value
    static void Slack(string row, SlackRoot v)
    {
        var fresh = new SlackRoot();
        for (int i = 1; i < v.Cells.Length; ++i)
        {
            var c = v.Cells[i];
            if (c.Id != 7) { Fail(row + ": cells[" + i + "].id=" + c.Id + ", want its declared 7"); }
            if (c.SubsCount != 0) { Fail(row + ": cells[" + i + "].subs count=" + c.SubsCount + ", want 0"); }
            for (int j = 0; j < c.Subs.Length; ++j)
            {
                if (c.Subs[j].V != 3) { Fail(row + ": cells[" + i + "].subs[" + j + "].v=" + c.Subs[j].V + ", want its declared 3"); }
            }
            if (Snap(c) != Snap(fresh.Cells[i])) { Fail(row + ": cells[" + i + "] " + Snap(c) + " is not a fresh value's " + Snap(fresh.Cells[i])); }
        }
        if (v.Cells[0].Subs[1].V != 3) { Fail(row + ": cells[0].subs[1].v=" + v.Cells[0].Subs[1].V + ", want its declared 3"); }
        if (Snap(v.Cells[0].Subs[1]) != Snap(fresh.Cells[0].Subs[1])) { Fail(row + ": cells[0].subs[1] is not a fresh value's"); }
        for (int i = 1; i < v.Nums.Length; ++i)
        {
            if (v.Nums[i] != 0) { Fail(row + ": nums[" + i + "]=" + v.Nums[i] + ", want 0"); }
        }
    }

    static int Main()
    {
        byte[] a = FileA();
        byte[] b = FileB();
        var plan = new TableFixedEntry[4096];

        // lawful: A then B into the same value
        var v = new SlackRoot();
        var r = new TableReport();
        long n = Schema.SlackRootFixedLoad(v, a, plan, r);
        if (n != 1 || r.Refused) { Fail("load A: n=" + n + " reason=" + r.Reason); }
        n = Schema.SlackRootFixedLoad(v, b, plan, r);
        if (n != 1 || r.Refused) { Fail("load B: n=" + n + " reason=" + r.Reason); }
        if (v.CellsCount != 1 || v.Cells[0].Id != 77 || v.Cells[0].SubsCount != 1 || v.Cells[0].Subs[0].V != 77 || v.NumsCount != 1 || v.Nums[0] != 77)
        {
            Fail("load B: the live values did not land");
        }
        Slack("lawful", v);

        // the same file into a fresh value reads back identically
        var f = new SlackRoot();
        n = Schema.SlackRootFixedLoad(f, b, plan, new TableReport());
        if (n != 1) { Fail("load B fresh: n=" + n); }
        if (Snap(v) != Snap(f)) { Fail("reused " + Snap(v) + " != fresh " + Snap(f)); }

        // refused: A, then a file whose form byte is wrong
        var w = new SlackRoot();
        Schema.SlackRootFixedLoad(w, a, plan, new TableReport());
        byte[] refused = (byte[])b.Clone();
        refused[0] = 9;
        var rr = new TableReport();
        n = Schema.SlackRootFixedLoad(w, refused, plan, rr);
        if (n != -1 || !rr.Refused) { Fail("refused load: n=" + n); }
        Slack("refused", w);
        if (Snap(w) != Snap(new SlackRoot())) { Fail("refused " + Snap(w) + " is not a fresh value"); }

        if (bad == 0) { Console.WriteLine("ROW fixed_slack: ok"); }
        return bad == 0 ? 0 : 1;
    }
}
`

func runFixedSlackProbe(t *testing.T) (string, error) {
	t.Helper()
	files, err := Generate(unitFrom(t, csReadSchema(t, "FixedSlack")))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	probePath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(probePath, []byte(fixedSlackProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, probePath)

	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "slackprobe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	compile := testproc.Command(t, dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc compile failed: %v\n%s", err, out)
	}
	cfg := fmt.Sprintf(`{"runtimeOptions":{"tfm":"net10.0","framework":{"name":"Microsoft.NETCore.App","version":%q}}}`+"\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "slackprobe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := testproc.Command(t, dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	return string(out), err
}
