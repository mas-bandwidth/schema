package unitytable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityFixedRefusalResetsEveryRow is the R4 ruling for the C# (unity)
// fixed batch: after ANY refusal every row handed in holds its declared
// default, the whole span and not the rows the walk reached. Every row starts
// poisoned; each refusal below (empty input, form byte, unknown hash, a record
// hash that names no layout, ragged tail, batch_too_large) owes -1 and every
// row at lead 1, trail 2, no vals. A clean control reads two records into three
// rows.
func TestUnityFixedRefusalResetsEveryRow(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect fixed refusal rows")
	u := fixedproof.ParseCheckUnit(t, "package probe\nfixed table G {\n    lead uint32 = 1\n    vals [..4]int32\n    trail uint32 = 2\n}\n")
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityFixedRefusalRowsMain, nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ROW fixed_refusal_rows: ok") || code != 0 {
		t.Fatalf("fixed refusal rows (exit %d):\n%s", code, out)
	}
}

const unityFixedRefusalRowsMain = `using System;
using Probe;

static class Program
{
    static string Refuse(string name, byte[] data, int rows)
    {
        G[] back = new G[rows];
        for (int i = 0; i < rows; i++)
        {
            back[i] = Schema.NewG();
            back[i].Lead = 0x5A5A5A5Au; back[i].Trail = 0x5A5A5A5Au; back[i].ValsCount = 3; back[i].Vals[0] = 9;
        }
        Schema.TableReport report = default;
        long n = Schema.GFixedLoad(back, data, ref report);
        if (n != -1) { return name + ": n=" + n + ", want a refusal"; }
        for (int i = 0; i < rows; i++)
        {
            if (back[i].Lead != 1 || back[i].Trail != 2 || back[i].ValsCount != 0 || back[i].Vals[0] != 0)
            {
                return name + ": row " + i + " lead=" + back[i].Lead + " trail=" + back[i].Trail + " valsCount=" + back[i].ValsCount + " vals0=" + back[i].Vals[0] + ", not its declared default";
            }
        }
        return null;
    }

    static int Main()
    {
        G[] two = new G[] { Schema.NewG(), Schema.NewG() };
        two[0].Lead = 10; two[1].Lead = 11;
        byte[] wire = new byte[Schema.GFixedMeasure(2)];
        Schema.GFixedSave(two, wire);
        byte[] form = (byte[])wire.Clone(); form[0] ^= 0x7f;
        byte[] unknown = (byte[])wire.Clone(); unknown[8] ^= 1;
        byte[] noLayout = (byte[])wire.Clone(); noLayout[wire.Length - (int)Schema.GFixedRecordBytes] ^= 1;
        byte[] ragged = new byte[wire.Length + 1]; Array.Copy(wire, ragged, wire.Length);
        string bad = Refuse("empty", new byte[0], 3) ?? Refuse("form", form, 3) ?? Refuse("unknown_hash", unknown, 3)
            ?? Refuse("no_layout", noLayout, 3) ?? Refuse("ragged", ragged, 3) ?? Refuse("batch_too_large", wire, 1);
        if (bad != null) { Console.WriteLine("ROW fixed_refusal_rows: FAILED " + bad); return 1; }
        G[] ok = new G[3];
        for (int i = 0; i < 3; i++) { ok[i] = Schema.NewG(); }
        Schema.TableReport rep = default;
        long got = Schema.GFixedLoad(ok, wire, ref rep);
        if (got != 2 || ok[0].Lead != 10 || ok[1].Lead != 11) { Console.WriteLine("ROW fixed_refusal_rows: FAILED control: n=" + got); return 1; }
        Console.WriteLine("ROW fixed_refusal_rows: ok");
        return 0;
    }
}
`
