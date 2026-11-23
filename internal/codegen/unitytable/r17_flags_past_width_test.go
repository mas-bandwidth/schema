package unitytable_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R17 (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the
// newer reader reads old data, and never the other way. The older build of
// this unit names two Cap bits (W = 2); the newer build appended Extra. The
// older generated C# (unity) refuses the whole load out_of_range when a file
// holds a bit at or above W, in the variable form (a field and a list
// element) and the fixed form, and reads the twin files inside W. The
// variable and fixed savers assert (debug only) on a mask past W. The old
// code kept the bit and wrote it back: planting it back reads every past file.
const unityR17Older = "package probe\n\nflags Cap { Trace, Verbose }\n\ntable Doc\n{\n    cap Cap\n    caps []Cap\n}\n\nfixed table Rec\n{\n    cap Cap\n}\n"

type unityR17Case struct {
	name   string
	wire   []byte
	refuse bool
}

// unityR17Cases builds the files with the Go reference engine and checks that
// engine answers each one the way the generated code must.
func unityR17Cases(t *testing.T, older, newer *ir.Unit) []unityR17Case {
	t.Helper()
	nm, om := tabletext.NewModel(newer), tabletext.NewModel(older)
	doc := func(text string) []byte {
		inst := nm.New(nm.Lookup("Doc"))
		var r tabletext.Report
		if !nm.Read(inst, []byte(text), &r) {
			t.Fatalf("the newer build read %s: %+v", text, r)
		}
		b, err := tablewire.Encode(nm, inst)
		if err != nil {
			t.Fatalf("the newer build wrote %s: %v", text, err)
		}
		return b
	}
	rec := func(mask uint64) []byte {
		inst := om.New(om.Lookup("Rec"))
		inst.Fields[0].Cell.U = mask
		b, err := tablewire.EncodeFixed(om, inst)
		if err != nil {
			t.Fatalf("fixed %#b: %v", mask, err)
		}
		return b
	}
	low, both := rec(0b01), rec(0b11)
	at := -1
	for i := range low {
		if low[i] != both[i] {
			at = i
		}
	}
	if at < 0 || len(low) != len(both) {
		t.Fatalf("no mask byte between %x and %x", low, both)
	}
	forged := bytes.Clone(low)
	forged[at] |= 0b100
	cases := []unityR17Case{
		{"doc_inside", doc(`{"cap":["Trace","Verbose"],"caps":[["Verbose"]]}`), false},
		{"doc_past", doc(`{"cap":["Trace","Extra"]}`), true},
		{"doc_list_past", doc(`{"caps":[["Trace"],["Extra"]]}`), true},
		{"rec_inside", low, false},
		{"rec_past", forged, true},
	}
	for _, c := range cases {
		root := "Doc"
		if strings.HasPrefix(c.name, "rec") {
			root = "Rec"
		}
		var r tabletext.Report
		ok, err := tablewire.Decode(om, om.New(om.Lookup(root)), c.wire, &r)
		if c.refuse && (ok || tablewire.RefusalReason(err) != tablewire.ReasonOutOfRange) {
			t.Fatalf("%s: the Go engine read %v %v, want out_of_range", c.name, ok, err)
		}
		if !c.refuse && (!ok || err != nil) {
			t.Fatalf("%s: the Go engine refused %v %v, want read", c.name, ok, err)
		}
	}
	return cases
}

func unityR17Ints(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ",")
}

func TestUnityR17FlagsPastWidthRefuse(t *testing.T) {
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect")
	t.Parallel()
	older := fixedproof.ParseCheckUnit(t, unityR17Older)
	newer := fixedproof.ParseCheckUnit(t, strings.Replace(unityR17Older, "Verbose }", "Verbose, Extra }", 1))
	cases := unityR17Cases(t, older, newer)
	var calls, want strings.Builder
	for _, c := range cases {
		fn := "LoadDoc"
		if strings.HasPrefix(c.name, "rec") {
			fn = "LoadRec"
		}
		fmt.Fprintf(&calls, "        %s(%q, new byte[] { %s });\n", fn, c.name, unityR17Ints(c.wire))
		if c.refuse {
			fmt.Fprintf(&want, "%s refused out_of_range\n", c.name)
		} else {
			fmt.Fprintf(&want, "%s read\n", c.name)
		}
	}
	want.WriteString("doc_save asserted\nrec_save asserted")
	probe := `using System;
using Probe;

public static class ProbeMain
{
    static void LoadDoc(string name, byte[] data)
    {
        var r = new Schema.TableReport();
        Schema.DocLoad(new Doc(), data, ref r);
        Console.WriteLine(r.Refused ? name + " refused " + Schema.TableReport.ReasonName(r.Reason) : name + " read");
    }

    static void LoadRec(string name, byte[] data)
    {
        var r = new Schema.TableReport();
        Schema.RecFixedLoad(new Rec[1], data, ref r);
        Console.WriteLine(r.Refused ? name + " refused " + Schema.TableReport.ReasonName(r.Reason) : name + " read");
    }

    public static int Main()
    {
        ProbeCommon.InstallAssertTrap();
` + calls.String() + `        try
        {
            var d = new Doc { Cap = (Cap)5UL };
            Schema.DocSave(d, new byte[Schema.DocMeasure(d)]);
            Console.WriteLine("doc_save wrote");
        }
        catch (AssertTrapException)
        {
            Console.WriteLine("doc_save asserted");
        }
        try
        {
            var e = new Rec[1];
            e[0].Cap = (Cap)5UL;
            Schema.RecFixedSave(e, new byte[Schema.RecFixedMeasure(1)]);
            Console.WriteLine("rec_save wrote");
        }
        catch (AssertTrapException)
        {
            Console.WriteLine("rec_save asserted");
        }
        return 0;
    }
}
`
	files, err := unityGenerateFiles(older, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := mustRunUnityVTProbe(t, files, probe)
	if got := strings.TrimSpace(out); got != want.String() {
		t.Fatalf("the older generated C# (unity) answered\n%s\nwant\n%s", got, want.String())
	}
}
