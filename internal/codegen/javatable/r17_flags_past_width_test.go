package javatable

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R17 (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the
// newer reader reads old data, and never the other way. The older build of
// this unit names two Cap bits (W = 2); the newer build appended Extra. The
// older generated Java refuses the whole load out_of_range when a file holds
// a bit at or above W, in the variable form (a field and a list element) and
// the fixed form, and reads the twin files inside W. The variable and fixed
// savers assert (debug only, java -ea) on a mask past W. The old code kept the
// bit and wrote it back: planting it back reads every past file and writes.
const javaR17Older = "package probe\n\nflags Cap { Trace, Verbose }\n\ntable Doc\n{\n    cap Cap\n    caps []Cap\n}\n\nfixed table Rec\n{\n    cap Cap\n}\n"

type javaR17Case struct {
	name   string
	wire   []byte
	refuse bool
}

// javaR17Cases builds the files with the Go reference engine and checks that
// engine answers each one the way the generated code must.
func javaR17Cases(t *testing.T, older, newer *ir.Unit) []javaR17Case {
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
	cases := []javaR17Case{
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

func javaR17Ints(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ",")
}

func TestJavaR17FlagsPastWidthRefuse(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	older := unitFrom(t, javaR17Older)
	newer := unitFrom(t, strings.Replace(javaR17Older, "Verbose }", "Verbose, Extra }", 1))
	cases := javaR17Cases(t, older, newer)
	var calls, want strings.Builder
	for _, c := range cases {
		fn := "doc"
		if strings.HasPrefix(c.name, "rec") {
			fn = "rec"
		}
		fmt.Fprintf(&calls, "        %s(%q, b(new int[]{%s}));\n", fn, c.name, javaR17Ints(c.wire))
		switch {
		case !c.refuse:
			fmt.Fprintf(&want, "%s read\n", c.name)
		case fn == "doc":
			fmt.Fprintf(&want, "%s refused out_of_range\n", c.name)
		default:
			fmt.Fprintf(&want, "%s refused outOfRange\n", c.name)
		}
	}
	want.WriteString("doc_save asserted\nrec_save asserted")
	driver := `import probe.*;

public final class Main {
    static byte[] b(int[] v) {
        byte[] o = new byte[v.length];
        for (int i = 0; i < v.length; i++) { o[i] = (byte) v[i]; }
        return o;
    }
    static void doc(String name, byte[] data) {
        TableVariable.Report r = new TableVariable.Report();
        DocVariable.load(data, r);
        System.out.println(name + (r.refused ? " refused " + r.reason : " read"));
    }
    static void rec(String name, byte[] data) {
        RecFixed.Value[] v = { new RecFixed.Value() };
        TableFixed.Report r = new TableFixed.Report();
        RecFixed.load(v, 1, data, TableFixed.plan(1 << 10), new short[1 << 10], RecFixed.image(), r);
        System.out.println(name + (r.refused ? " refused " + r.reason : " read"));
    }
    public static void main(String[] args) {
` + calls.String() + `        try {
            DocVariable.Value v = new DocVariable.Value();
            v.cap = 5L;
            DocVariable.save(v);
            System.out.println("doc_save wrote");
        } catch (AssertionError e) {
            System.out.println("doc_save asserted");
        }
        try {
            RecFixed.Value[] v = { new RecFixed.Value() };
            v[0].cap = 5L;
            RecFixed.save(v, 1, new byte[RecFixed.measure(1)]);
            System.out.println("rec_save wrote");
        } catch (AssertionError e) {
            System.out.println("rec_save asserted");
        }
    }
}
`
	_, out := compileAndRunJavaProbe(t, javaR17Older, "probe", driver)
	if out != want.String() {
		t.Fatalf("the older generated Java answered\n%s\nwant\n%s", out, want.String())
	}
}
