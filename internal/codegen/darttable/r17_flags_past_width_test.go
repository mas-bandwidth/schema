package darttable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R17 (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the
// newer reader reads old data, and never the other way. The older build of
// this unit names two Cap bits (W = 2); the newer build appended Extra. The
// older generated Dart refuses the whole load out_of_range when a file holds
// a bit at or above W, in the variable form (a field and a list element) and
// the fixed form, and reads the twin files inside W. The variable and fixed
// savers assert (debug only, --enable-asserts) on a mask past W. The old code
// kept the bit and wrote it back: planting it back reads every past file.
const dartR17Older = "package probe\n\nflags Cap { Trace, Verbose }\n\ntable Doc\n{\n    cap Cap\n    caps []Cap\n}\n\nfixed table Rec\n{\n    cap Cap\n}\n"

type dartR17Case struct {
	name   string
	wire   []byte
	refuse bool
}

// dartR17Cases builds the files with the Go reference engine and checks that
// engine answers each one the way the generated code must.
func dartR17Cases(t *testing.T, older, newer *ir.Unit) []dartR17Case {
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
	cases := []dartR17Case{
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

func dartR17Ints(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ",")
}

func TestDartR17FlagsPastWidthRefuse(t *testing.T) {
	slowtest.Gate(t, "the Dart SDK (it runs the generated Dart)")
	t.Parallel()
	older := unitFrom(t, dartR17Older)
	newer := unitFrom(t, strings.Replace(dartR17Older, "Verbose }", "Verbose, Extra }", 1))
	cases := dartR17Cases(t, older, newer)
	var calls, want strings.Builder
	for _, c := range cases {
		fn := "doc"
		if strings.HasPrefix(c.name, "rec") {
			fn = "rec"
		}
		fmt.Fprintf(&calls, "  %s(%q, Uint8List.fromList([%s]));\n", fn, c.name, dartR17Ints(c.wire))
		switch {
		case !c.refuse:
			fmt.Fprintf(&want, "%s read\n", c.name)
		case fn == "doc":
			fmt.Fprintf(&want, "%s refused out_of_range\n", c.name)
		default:
			fmt.Fprintf(&want, "%s refused 21\n", c.name)
		}
	}
	want.WriteString("doc_save asserted\nrec_save asserted")
	driver := `import "dart:typed_data";
import "ProbeWire.dart" as w;
import "ProbeFixed.dart" as f;

void doc(String name, Uint8List data) {
  final r = w.TableWireReport();
  w.docWireLoad(data, r);
  print(r.refused ? "$name refused ${r.refusal}" : "$name read");
}

void rec(String name, Uint8List data) {
  final r = f.TableFixedReport();
  f.recFixedLoad([f.Rec()], 1, data, data.length, f.recFixedNewPlan(), r);
  print(r.refused != 0 ? "$name refused ${r.refused}" : "$name read");
}

void main() {
` + calls.String() + `  try {
    w.docWireSave(w.Doc()..cap = 5);
    print("doc_save wrote");
  } on AssertionError {
    print("doc_save asserted");
  }
  try {
    f.recFixedSave([f.Rec()..cap = 5], 1, Uint8List(f.recFixedMeasure(1)));
    print("rec_save wrote");
  } on AssertionError {
    print("rec_save asserted");
  }
}
`
	files, err := Generate(older)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, older))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := testproc.Command(t, dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dart probe: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != want.String() {
		t.Fatalf("the older generated Dart answered\n%s\nwant\n%s", got, want.String())
	}
}
