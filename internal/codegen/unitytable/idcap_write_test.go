package unitytable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for C# (unity). The unit is tablewire.IdCapSchema: the three
// values need 4095, 4096 and 4097 ids; the first two write (the count word
// says so), and the third makes Measure and Save answer -1 and write nothing.
func TestUnityWriterRefusesMoreThan4096Ids(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect id table maximum")
	u := fixedproof.ParseCheckUnit(t, tablewire.IdCapSchema())
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatalf("unityGenerateFiles: %v", err)
	}
	probe := `using System;
using Idcap;

class Program
{
    static Root Make(byte x, byte y)
    {
        var v = new Root { X = x, Y = y };
        var keys = v.Keys.AsSpan();
        for (int i = 0; i < keys.Length; i++) keys[i] = 1;
        return v;
    }

    static int Main()
    {
        (byte x, byte y, int ids)[] rows = { (0, 0, 4095), (1, 0, 4096) };
        foreach (var row in rows)
        {
            var v = Make(row.x, row.y);
            long n = Schema.RootMeasure(ref v);
            if (n <= 0) throw new Exception(row.ids + " ids: measure refused");
            byte[] buf = new byte[n];
            if (Schema.RootSave(ref v, buf) != n) throw new Exception(row.ids + " ids: save refused");
            long count = 0;
            for (int i = 0; i < 8; i++) count |= (long)buf[buf.Length - 8 + i] << (8 * i);
            if (count != row.ids) throw new Exception("id count " + count + ", want " + row.ids);
        }
        var over = Make(1, 1);
        if (Schema.RootMeasure(ref over) >= 0) throw new Exception("4097 ids: measure did not refuse");
        byte[] big = new byte[1 << 20];
        Array.Fill(big, (byte)0x5a);
        if (Schema.RootSave(ref over, big) >= 0) throw new Exception("4097 ids: save did not refuse");
        Console.WriteLine("UNITY_ID_CAP_OK");
        return 0;
    }
}
`
	mustRunUnityVTProbe(t, files, probe)
}
