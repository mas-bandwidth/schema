package unitytable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestUnityMessageRefusalCodes: a message-form refusal is a refusal, not wire
// damage. A second announcement on a connection and a message read before any
// announcement carry their own TableReport codes (SecondAnnouncement,
// NoVocabulary), with the reason text agreeing, as Go and C# (cs) keep them
// apart. The old code reported WireDamaged for both.
func TestUnityMessageRefusalCodes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect message refusal codes")
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
	out, code, _, err := runUnityProbe(t, files, strings.ReplaceAll(unityRefusalCodesMain, "FIXTURE", fixture), nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ROW refusal_codes: ok") || code != 0 {
		t.Fatalf("refusal codes (exit %d):\n%s", code, out)
	}
}

const unityRefusalCodesMain = `using System;

static class ProbeMain
{
    static int Main()
    {
        byte[] wire = System.IO.File.ReadAllBytes(@"FIXTURE");
        var voc = new Backenddemo.Schema.TableVocabulary();
        var arep = new Backenddemo.Schema.TableReport();
        if (!Backenddemo.Schema.AnnounceRead(voc, Backenddemo.Schema.Announce(), ref arep)) { Console.WriteLine("ROW refusal_codes: FAILED announce"); return 1; }
        var again = new Backenddemo.Schema.TableReport();
        bool ok = Backenddemo.Schema.AnnounceRead(voc, Backenddemo.Schema.Announce(), ref again);
        if (ok || !again.Refused || again.Reason != Backenddemo.Schema.TableReport.SecondAnnouncement || again.ReasonText != "second_announcement")
        { Console.WriteLine("ROW refusal_codes: FAILED second announcement ok=" + ok + " reason=" + again.Reason + " text=" + again.ReasonText); return 1; }
        var fresh = new Backenddemo.Schema.TableVocabulary();
        var one = new Backenddemo.LoginRequest[] { Backenddemo.Schema.NewLoginRequest(), Backenddemo.Schema.NewLoginRequest() };
        var rep = new Backenddemo.Schema.TableReport();
        bool ok2 = Backenddemo.Schema.LoginRequestLoadMessages(one, wire, fresh, ref rep, out int count);
        if (ok2 || !rep.Refused || rep.Reason != Backenddemo.Schema.TableReport.NoVocabulary || rep.ReasonText != "no_vocabulary")
        { Console.WriteLine("ROW refusal_codes: FAILED no vocabulary ok=" + ok2 + " reason=" + rep.Reason + " text=" + rep.ReasonText); return 1; }
        Console.WriteLine("ROW refusal_codes: ok");
        return 0;
    }
}
`
