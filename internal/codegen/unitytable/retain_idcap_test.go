package unitytable_test

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/idcapwire"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE RETAINING WRITER KEEPS THE 4096-ID BUDGET (docs/SPEC-TABLES.md §3 and
// §6.6, ruling R8 follow-up), for C# (unity). The older build loads a lawful
// 4096-id file with retention (the newer build's field z is the one retained
// foreign id), sets x (the 4096th known id) and saves: 4097 ids if the
// retained id were written beside them. The save must refuse, or drop the
// retained id and count it lost; the file it writes, if any, has at most 4096
// ids and loads.
func TestUnityRetainWriterKeeps4096IdBudget(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect retain id budget")
	u := fixedproof.ParseCheckUnit(t, tablewire.IdCapSchema())
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatalf("unityGenerateFiles: %v", err)
	}
	wire := base64.StdEncoding.EncodeToString(idcapwire.NewerFile(t))
	probe := fmt.Sprintf(`using System;
using Idcap;

class Program
{
    static int Main()
    {
        byte[] wire = Convert.FromBase64String("%s");
        var retain = new Schema.TableRetain(1 << 20, 1 << 14);
        var report = new Schema.TableReport();
        Root root = default;
        if (!Schema.RootLoadRetain(ref root, wire, retain, ref report)) throw new Exception("the lawful 4096-id file did not load with retention");
        if (report.Retained != 1) throw new Exception("z was not retained: " + report.Retained);
        root.X = 1;
        long size = Schema.RootMeasureRetain(ref root, retain);
        if (size >= 0)
        {
            byte[] saved = new byte[size];
            long w = Schema.RootSaveRetain(ref root, retain, saved, ref report);
            if (w != size) throw new Exception("save wrote " + w + " of " + size);
            long count = 0;
            for (int i = 0; i < 8; i++) count |= (long)saved[saved.Length - 8 + i] << (8 * i);
            if (count > 4096) throw new Exception("the retaining save wrote an id table of " + count + " entries");
        }
        Console.WriteLine("UNITY_RETAIN_ID_BUDGET_OK");
        return 0;
    }
}
`, wire)
	mustRunUnityVTProbe(t, files, probe)
}
