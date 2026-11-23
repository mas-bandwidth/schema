package cstable

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R17 (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH): the
// newer reader reads old data, and never the other way. The older build of
// this unit names two Cap bits (W = 2); the newer build appended Extra. The
// older generated C# refuses the whole load out_of_range when a file holds a
// bit at or above W, in the variable form (a field and a list element) and
// the fixed form, and reads the twin files inside W. The variable and fixed
// savers assert (debug only, -define:DEBUG) on a mask past W. The old code
// kept the bit and wrote it back: planting it back reads every past file.
const csR17Older = "package probe\n\nflags Cap { Trace, Verbose }\n\ntable Doc\n{\n    cap Cap\n    caps []Cap\n}\n\nfixed table Rec\n{\n    cap Cap\n}\n"

type csR17Case struct {
	name   string
	wire   []byte
	refuse bool
}

// csR17Cases builds the files with the Go reference engine and checks that
// engine answers each one the way the generated code must.
func csR17Cases(t *testing.T, older, newer *ir.Unit) []csR17Case {
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
	cases := []csR17Case{
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

func csR17Ints(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ",")
}

func TestCSharpR17FlagsPastWidthRefuse(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Parallel()
	older := unitFrom(t, csR17Older)
	newer := unitFrom(t, strings.Replace(csR17Older, "Verbose }", "Verbose, Extra }", 1))
	cases := csR17Cases(t, older, newer)
	var calls, want strings.Builder
	for _, c := range cases {
		fn := "Doc"
		if strings.HasPrefix(c.name, "rec") {
			fn = "Rec"
		}
		fmt.Fprintf(&calls, "  %s(%q, new byte[] { %s });\n", fn, c.name, csR17Ints(c.wire))
		if c.refuse {
			fmt.Fprintf(&want, "%s refused out_of_range\n", c.name)
		} else {
			fmt.Fprintf(&want, "%s read\n", c.name)
		}
	}
	want.WriteString("doc_save asserted\nrec_save asserted")
	driver := `using System;
using System.Diagnostics;
using Probe;
sealed class WriteAssertion : Exception { }
sealed class AssertListener : TraceListener {
 public override void Write(string message) { }
 public override void WriteLine(string message) { }
 public override void Fail(string message) { throw new WriteAssertion(); }
 public override void Fail(string message, string detail) { throw new WriteAssertion(); }
}
class R17Probe {
 static void Doc(string name, byte[] data) {
  var r = new TableReport();
  Schema.DocLoad(new Doc(), data, r);
  Console.WriteLine(r.Refused ? name + " refused " + r.Reason : name + " read");
 }
 static void Rec(string name, byte[] data) {
  var r = new TableReport();
  Schema.RecFixedLoad(new Rec(), data, new TableFixedEntry[1024], r);
  Console.WriteLine(r.Refused ? name + " refused " + r.Reason : name + " read");
 }
 static void Main() {
  Trace.Listeners.Clear();
  Trace.Listeners.Add(new AssertListener());
` + calls.String() + `  try {
   var d = new Doc { Cap = (Cap)5UL };
   Schema.DocSave(d, new byte[Schema.DocMeasure(d)]);
   Console.WriteLine("doc_save wrote");
  } catch (WriteAssertion) {
   Console.WriteLine("doc_save asserted");
  }
  try {
   var e = new Rec { Cap = (Cap)5UL };
   Schema.RecFixedSave(e, new byte[Schema.RecFixedMeasure(1)]);
   Console.WriteLine("rec_save wrote");
  } catch (WriteAssertion) {
   Console.WriteLine("rec_save asserted");
  }
 }
}
`
	files, err := Generate(older)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := csharp.Generate(older)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	runtimeDir, err := filepath.Abs("../../../../serialize.cs/src")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		path := filepath.Join(runtimeDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("C# packet runtime prerequisite: %v", err)
		}
		sources = append(sources, path)
	}
	main := filepath.Join(dir, "R17Probe.cs")
	if err := os.WriteFile(main, []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, main)
	csc, refs, runtime := csDotnetToolchain(t)
	args := append([]string{"exec", csc, "-nologo", "-unsafe", "-define:DEBUG", "-nowarn:CS8500", "-out:" + filepath.Join(dir, "probe.dll")}, sources...)
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	cmd := testproc.Command(t, csDotnet(), args...)
	cmd.Env = csDotnetEnv(dir, dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	config := fmt.Sprintf(`{"runtimeOptions":{"tfm":"net10.0","framework":{"name":"Microsoft.NETCore.App","version":%q}}}`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "probe.runtimeconfig.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = testproc.Command(t, csDotnet(), "exec", filepath.Join(dir, "probe.dll"))
	cmd.Env = csDotnetEnv(dir, dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != want.String() {
		t.Fatalf("the older generated C# answered\n%s\nwant\n%s", got, want.String())
	}
}
