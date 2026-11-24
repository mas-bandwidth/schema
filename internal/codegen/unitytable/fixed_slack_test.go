package unitytable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD (docs/SPEC-TABLES.md §3.4, THE SLOTS
// PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED; test/tables/FixedSlack.schema
// states the recipe). Load file A (every counted array at its bound, every slot
// 77) into a value, then file B (every count 1) into THE SAME value: the value
// holds what a fresh value (Schema.New<T>) loaded from B holds, every slot past
// every count at its declared default (cells[1..2].id 7, every sub v 3, nums
// 0), never A's 77 and never the zero form. A refused load leaves the same
// defaults, and a fresh value holds them before any load.
func TestUnityFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect fixed slack slots")
	u := fixedproof.ParseCheckUnit(t, unityReadFixedSlack(t))
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityFixedSlackMain, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ROW fixed_slack: ok") || code != 0 {
		t.Fatalf("fixed slack slots (exit %d):\n%s", code, out)
	}
}

// THE EMITTER PIN, no toolchain: FixedSlack.schema's generated source builds
// every counted slot in the construction form and puts every slot past a count
// back on the fixed decode, and a unit whose counted arrays all hold elements
// with an all-zero default emits neither (§2.2 zero cost).
func TestUnityFixedSlackEmitterPin(t *testing.T) {
	t.Parallel()
	src := unityGeneratedSource(t, unityReadFixedSlack(t))
	for _, want := range []string{
		"for (int i = 0; i < 3; i++) { InitSlackCell(ref span[i]); }",
		"for (int i = 0; i < 2; i++) { InitSlackSub(ref span[i]); }",
		"for (int i0 = cnt; i0 < 3; i0++) { InitSlackCell(ref sp0[i0]); }",
		"for (int i0 = cnt; i0 < 2; i0++) { InitSlackSub(ref sp0[i0]); }",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("FixedSlack's generated Unity C# lacks %q", want)
		}
	}
	zero := unityGeneratedSource(t, `package zeroslack

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
	if strings.Contains(zero, "i0 = cnt;") || strings.Contains(zero, "InitZeroCell(ref span[i])") {
		t.Errorf("a unit whose counted arrays all hold all-zero element defaults must emit no slack reset")
	}
}

func unityReadFixedSlack(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "FixedSlack.schema"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func unityGeneratedSource(t *testing.T, schema string) string {
	t.Helper()
	files, err := unitytable.Generate(fixedproof.ParseCheckUnit(t, schema))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

const unityFixedSlackMain = `using System;
using Tblslack;

static class Program
{
    static int bad = 0;
    static void Fail(string what) { Console.WriteLine("FAIL " + what); bad++; }

    static byte[] Save(SlackRoot v)
    {
        byte[] buf = new byte[Schema.SlackRootFixedMeasure(1)];
        long w = Schema.SlackRootFixedSave(new SlackRoot[] { v }, buf);
        if (w != buf.Length) { Fail("save wrote " + w); }
        return buf;
    }

    // file A: every counted array at its bound, every slot off its default
    static byte[] FileA()
    {
        SlackRoot v = Schema.NewSlackRoot();
        Span<SlackCell> cells = v.Cells.AsSpan();
        v.CellsCount = cells.Length;
        for (int i = 0; i < cells.Length; i++)
        {
            cells[i].Id = 77;
            Span<SlackSub> subs = cells[i].Subs.AsSpan();
            cells[i].SubsCount = subs.Length;
            for (int j = 0; j < subs.Length; j++) { subs[j].V = 77; }
        }
        Span<int> nums = v.Nums.AsSpan();
        v.NumsCount = nums.Length;
        for (int i = 0; i < nums.Length; i++) { nums[i] = 77; }
        return Save(v);
    }

    // file B: every count 1, every live value 77
    static byte[] FileB()
    {
        SlackRoot v = Schema.NewSlackRoot();
        Span<SlackCell> cells = v.Cells.AsSpan();
        v.CellsCount = 1;
        cells[0].Id = 77;
        cells[0].SubsCount = 1;
        cells[0].Subs.AsSpan()[0].V = 77;
        v.NumsCount = 1;
        v.Nums.AsSpan()[0] = 77;
        return Save(v);
    }

    static string Snap(SlackRoot v)
    {
        var sb = new System.Text.StringBuilder();
        sb.Append(v.CellsCount).Append('|');
        for (int i = 0; i < 3; i++)
        {
            SlackCell c = v.Cells[i];
            sb.Append(c.Id).Append(':').Append(c.SubsCount).Append(':').Append(c.Subs[0].V).Append(',').Append(c.Subs[1].V).Append('|');
        }
        sb.Append(v.NumsCount).Append('|');
        for (int i = 0; i < 4; i++) { sb.Append(v.Nums[i]).Append(','); }
        return sb.ToString();
    }

    // every slot past every count, against the declared defaults
    static void Slack(string row, SlackRoot v)
    {
        for (int i = 1; i < 3; i++)
        {
            SlackCell c = v.Cells[i];
            if (c.Id != 7) { Fail(row + ": cells[" + i + "].id=" + c.Id + ", want its declared 7"); }
            if (c.SubsCount != 0) { Fail(row + ": cells[" + i + "].subs count=" + c.SubsCount + ", want 0"); }
            for (int j = 0; j < 2; j++)
            {
                if (c.Subs[j].V != 3) { Fail(row + ": cells[" + i + "].subs[" + j + "].v=" + c.Subs[j].V + ", want its declared 3"); }
            }
        }
        if (v.Cells[0].Subs[1].V != 3) { Fail(row + ": cells[0].subs[1].v=" + v.Cells[0].Subs[1].V + ", want its declared 3"); }
        for (int i = 1; i < 4; i++)
        {
            if (v.Nums[i] != 0) { Fail(row + ": nums[" + i + "]=" + v.Nums[i] + ", want 0"); }
        }
    }

    static int Main()
    {
        byte[] a = FileA();
        byte[] b = FileB();

        // a fresh value holds the declared defaults in every slot
        SlackRoot fresh = Schema.NewSlackRoot();
        Slack("fresh", fresh);
        if (fresh.Cells[0].Id != 7 || fresh.Cells[0].Subs[0].V != 3) { Fail("fresh: cells[0] is not at its declared defaults"); }

        // lawful: A then B into the same value
        SlackRoot[] v = new SlackRoot[] { Schema.NewSlackRoot() };
        Schema.TableReport r = default;
        long n = Schema.SlackRootFixedLoad(v, a, ref r);
        if (n != 1 || r.Refused) { Fail("load A: n=" + n); }
        n = Schema.SlackRootFixedLoad(v, b, ref r);
        if (n != 1 || r.Refused) { Fail("load B: n=" + n); }
        if (v[0].CellsCount != 1 || v[0].Cells[0].Id != 77 || v[0].Cells[0].SubsCount != 1 || v[0].Cells[0].Subs[0].V != 77 || v[0].NumsCount != 1 || v[0].Nums[0] != 77)
        {
            Fail("load B: the live values did not land: " + Snap(v[0]));
        }
        Slack("lawful", v[0]);

        // the same file into a fresh value reads back identically
        SlackRoot[] f = new SlackRoot[] { Schema.NewSlackRoot() };
        Schema.TableReport fr = default;
        n = Schema.SlackRootFixedLoad(f, b, ref fr);
        if (n != 1) { Fail("load B fresh: n=" + n); }
        if (Snap(v[0]) != Snap(f[0])) { Fail("reused " + Snap(v[0]) + " != fresh " + Snap(f[0])); }

        // refused: A, then a file whose form byte is wrong
        SlackRoot[] w = new SlackRoot[] { Schema.NewSlackRoot() };
        Schema.TableReport wr = default;
        Schema.SlackRootFixedLoad(w, a, ref wr);
        byte[] refused = (byte[])b.Clone();
        refused[0] = 9;
        n = Schema.SlackRootFixedLoad(w, refused, ref wr);
        if (n != -1 || !wr.Refused) { Fail("refused load: n=" + n); }
        Slack("refused", w[0]);
        if (Snap(w[0]) != Snap(Schema.NewSlackRoot())) { Fail("refused " + Snap(w[0]) + " is not a fresh value"); }

        if (bad == 0) { Console.WriteLine("ROW fixed_slack: ok"); }
        return bad == 0 ? 0 : 1;
    }
}
`
