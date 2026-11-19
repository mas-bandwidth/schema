package unitytable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestUnityVTUnsortedMapEntriesSortCanonicallyAndRefuseDuplicates(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect unsorted map canonical save")

	const srcSchema = `package umapsort

table Root
{
    by_name map[string]int32
}
`
	u := fixedproof.ParseCheckUnit(t, srcSchema)
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatalf("unityGenerateFiles: %v", err)
	}

	probe := `using System;
using System.Text;
using Umapsort;

class Program
{
    static RootByNameEntry Mk(string k, int v)
    {
        byte[] kb = Encoding.UTF8.GetBytes(k);
        return new RootByNameEntry { Key = kb, KeyLength = kb.Length, Value = v };
    }

    static int Main()
    {
        var sorted = new Root();
        var rep = default(Schema.TableReport);
        byte[] jsrc = Encoding.UTF8.GetBytes("{\"by_name\":{\"\":1,\"a\":2,\"\uff61\":3,\"\ud83d\ude00\":4}}");
        if (!Schema.RootFromJson(sorted, jsrc, ref rep) || rep.Damaged || rep.Refused)
            throw new Exception("FromJson failed");
        byte[] wantWire = new byte[Schema.RootMeasure(sorted)];
        if (Schema.RootSave(sorted, wantWire) != wantWire.Length)
            throw new Exception("sorted Save failed");
        byte[] wantJson = new byte[Schema.RootToJsonMeasure(sorted)];
        if (Schema.RootToJson(sorted, wantJson) != wantJson.Length)
            throw new Exception("sorted ToJson failed");

        var rev = new Root();
        rev.ByName = new[] { Mk("\U0001F600", 4), Mk("\uff61", 3), Mk("a", 2), Mk("", 1) };
        rev.ByNameCount = 4;
        byte[] gotJson = new byte[4096];
        long jn = Schema.RootToJson(rev, gotJson);
        if (jn != wantJson.Length || !gotJson.AsSpan(0, (int)jn).SequenceEqual(wantJson))
            throw new Exception("unsorted ToJson mismatch jn=" + jn);

        var shuf = new Root();
        shuf.ByName = new[] { Mk("\U0001F600", 4), Mk("a", 2), Mk("\uff61", 3), Mk("", 1) };
        shuf.ByNameCount = 4;
        byte[] gotWire = new byte[4096];
        long wn = Schema.RootSave(shuf, gotWire);
        if (wn != wantWire.Length || !gotWire.AsSpan(0, (int)wn).SequenceEqual(wantWire))
            throw new Exception("unsorted Save mismatch wn=" + wn);

        var dup = new Root();
        dup.ByName = new[] { Mk("b", 1), Mk("a", 2), Mk("b", 3) };
        dup.ByNameCount = 3;
        if (Schema.RootSave(dup, gotWire) >= 0)
            throw new Exception("duplicate unsorted key accepted by Save");
        if (Schema.RootToJson(dup, gotJson) >= 0)
            throw new Exception("duplicate unsorted key accepted by ToJson");

        Console.WriteLine("UNITY_UNSORTED_MAP_OK");
        return 0;
    }
}
`
	mustRunUnityVTProbe(t, files, probe)
}
