package unitytable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityMessageLoadCapacityFixture reads the committed capacity fixture,
// testdata/batch_capacity_two.bin (two LoginRequest bodies of
// tables/backend, client_build 40, written by the Go engine and pinned to it
// by test/conformance/harness TestBatchCapacityFixtureIsTheGoEnginesInEveryLeg),
// through the generated C# (unity) LoginRequestLoadMessages. Storage for one
// body whose ClientBuild is the sentinel 17 owes "A refused batch returns a
// count of `0`" and "refuses before it decodes anything" (docs/SPEC-TABLES.md
// §3.3): false, count 0. R4: every value handed in holds its declared default
// (client_build 0) after ANY refusal, the whole array and not 0..count: storage
// for three with the form byte changed, and with the last byte dropped, owe the
// same (slot 2 is past the batch's count of 2). Storage for two owes 2 and 40.
// A caller's capacity is a resource limit (§6.5), so this row runs here, in
// the leg's own tests, never in the shared suites.
func TestUnityMessageLoadCapacityFixture(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect capacity fixture")
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "tables", "backend", "Backend.schema"))
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "batch_capacity_two.bin"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, string(schema)), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, strings.ReplaceAll(unityCapacityFixtureMain, "FIXTURE", fixture), nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ROW capacity_fixture: ok") || code != 0 {
		t.Fatalf("capacity fixture (exit %d):\n%s", code, out)
	}
}

const unityCapacityFixtureMain = `using System;

static class ProbeMain
{
    static int Main()
    {
        byte[] wire = System.IO.File.ReadAllBytes(@"FIXTURE");
        var voc = new Backenddemo.Schema.TableVocabulary();
        var arep = new Backenddemo.Schema.TableReport();
        if (!Backenddemo.Schema.AnnounceRead(voc, Backenddemo.Schema.Announce(), ref arep)) { Console.WriteLine("ROW capacity_fixture: FAILED announce"); return 1; }
        var one = new Backenddemo.LoginRequest[] { Backenddemo.Schema.NewLoginRequest() };
        one[0].ClientBuild = 17;
        var rep = new Backenddemo.Schema.TableReport();
        bool ok = Backenddemo.Schema.LoginRequestLoadMessages(one, wire, voc, ref rep, out int count);
        if (ok || count != 0) { Console.WriteLine("ROW capacity_fixture: FAILED two bodies into storage for one: ok=" + ok + " count=" + count + ", and a refused batch returns 0"); return 1; }
        if (one[0].ClientBuild != 0) { Console.WriteLine("ROW capacity_fixture: FAILED ClientBuild reads " + one[0].ClientBuild + ", not its declared default 0"); return 1; }
        byte[] form = (byte[])wire.Clone(); form[0] = 3;
        byte[] cut = new byte[wire.Length - 1]; Array.Copy(wire, cut, cut.Length);
        foreach (byte[] bad in new byte[][] { form, cut })
        {
            var three = new Backenddemo.LoginRequest[] { Backenddemo.Schema.NewLoginRequest(), Backenddemo.Schema.NewLoginRequest(), Backenddemo.Schema.NewLoginRequest() };
            for (int i = 0; i < 3; i++) { three[i].ClientBuild = 17; }
            var badRep = new Backenddemo.Schema.TableReport();
            bool badOk = Backenddemo.Schema.LoginRequestLoadMessages(three, bad, voc, ref badRep, out int badCount);
            if (badOk || badCount != 0) { Console.WriteLine("ROW capacity_fixture: FAILED refused batch: ok=" + badOk + " count=" + badCount); return 1; }
            for (int i = 0; i < 3; i++)
            {
                if (three[i].ClientBuild != 0) { Console.WriteLine("ROW capacity_fixture: FAILED value " + i + " ClientBuild reads " + three[i].ClientBuild + ", not its declared default 0"); return 1; }
            }
        }
        var two = new Backenddemo.LoginRequest[] { Backenddemo.Schema.NewLoginRequest(), Backenddemo.Schema.NewLoginRequest() };
        var loaded = new Backenddemo.Schema.TableReport();
        bool ok2 = Backenddemo.Schema.LoginRequestLoadMessages(two, wire, voc, ref loaded, out int got);
        if (!ok2 || got != 2 || two[0].ClientBuild != 40 || two[1].ClientBuild != 40) { Console.WriteLine("ROW capacity_fixture: FAILED two bodies into storage for two: ok=" + ok2 + " count=" + got); return 1; }
        Console.WriteLine("ROW capacity_fixture: ok");
        return 0;
    }
}
`
