package unitytable_test

import (
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE COOK REFUSES A DEPRECATED OR OUT-OF-SET ENUM BEFORE ANY BYTE (SF-25,
// SF-26; docs/SPEC.md §5.1 row 5; docs/SPEC-TABLES.md §2.15, §7.6), on the
// C# (unity) leg: an assert leg, so a Debug.Assert (the unit is built with
// -define:DEBUG) that fires in the placement pass, before the header is
// written or the buffer cleared, leaving the caller's 0x5a-filled buffer
// untouched. Phase.Next writes; Old (deprecated) and 9 (past the set)
// assert, on the fixed and the variable root, in both byte orders.
//
// THE PLANT rides in the test: the same unit with the enum assert moved to
// the write pass only (after the header and the clear) must answer
// "asserted touched", so the test proves it can see a late check.
const unityDeprCookProbe = `using System;
using System.Diagnostics;
using Depr;
sealed class WriteAssertion : Exception { }
sealed class AssertListener : TraceListener {
 public override void Write(string message) { }
 public override void WriteLine(string message) { }
 public override void Fail(string message) { throw new WriteAssertion(); }
 public override void Fail(string message, string detail) { throw new WriteAssertion(); }
}
class DeprCookProbe {
 static void Report(string name, byte[] buf, Func<bool> cook) {
  string verdict;
  try { verdict = cook() ? "wrote" : "refused"; } catch (WriteAssertion) { verdict = "asserted"; }
  bool touched = false;
  foreach (byte b in buf) { if (b != 0x5a) { touched = true; } }
  Console.WriteLine(name + " " + verdict + (touched ? " touched" : " untouched"));
 }
 static void Main() {
  Trace.Listeners.Clear();
  Trace.Listeners.Add(new AssertListener());
  long fixedSize = Schema.FixedRootCookMeasure(new FixedRoot { Phase = Phase.Next });
  long varSize = Schema.VarRootCookMeasure(new VarRoot { Phase = Phase.Next });
  foreach (var (tag, order) in new[] { ("le", Schema.TableByteOrder.Little), ("be", Schema.TableByteOrder.Big) }) {
   foreach (var (label, raw) in new[] { ("next", 3), ("old", 2), ("nine", 9) }) {
    var f = new FixedRoot { Phase = (Phase)(byte)raw };
    var fb = new byte[fixedSize + 64]; Array.Fill(fb, (byte)0x5a);
    Report("fixed_" + label + "_" + tag, fb, () => Schema.FixedRootCook(f, fb, order));
    var v = new VarRoot { Phase = (Phase)(byte)raw };
    var vb = new byte[varSize + 64]; Array.Fill(vb, (byte)0x5a);
    Report("var_" + label + "_" + tag, vb, () => Schema.VarRootCook(v, vb, order));
   }
  }
 }
}
`

func unityDeprCookWant(late bool) string {
	refused := "asserted untouched"
	if late {
		refused = "asserted touched"
	}
	var b strings.Builder
	for _, order := range []string{"le", "be"} {
		for _, label := range []string{"next", "old", "nine"} {
			for _, root := range []string{"fixed", "var"} {
				verdict := refused
				if label == "next" {
					verdict = "wrote touched"
				}
				fmt.Fprintf(&b, "%s_%s_%s %s\n", root, label, order, verdict)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

const (
	unityDeprEnumAssert     = "System.Diagnostics.Debug.Assert(!(f.Kind == 30 && i < live && "
	unityDeprEnumAssertLate = "System.Diagnostics.Debug.Assert(w.Measure || !(f.Kind == 30 && i < live && "
)

func TestUnityDeclaredDeprecatedEnumCookAssertsBeforeAnyByte(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc (it runs the generated C# (unity) cook over the declared-deprecated-enum witness)")
	src, err := os.ReadFile("../../../testdata/conformance/feature-witnesses/declared-deprecated-enum/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, string(src)), nil)
	if err != nil {
		t.Fatal(err)
	}
	planted := maps.Clone(files)
	hits := 0
	for name, data := range planted {
		if n := strings.Count(string(data), unityDeprEnumAssert); n > 0 {
			hits += n
			planted[name] = []byte(strings.ReplaceAll(string(data), unityDeprEnumAssert, unityDeprEnumAssertLate))
		}
	}
	if hits != 1 {
		t.Fatalf("the generated runtime holds the enum cook assert %d times, want 1: the plant cannot land", hits)
	}
	for _, c := range []struct {
		name  string
		files map[string][]byte
		late  bool
	}{{"fix", files, false}, {"plant", planted, true}} {
		out, code, _, runErr := runUnityProbe(t, c.files, unityDeprCookProbe, nil)
		if runErr != nil || code != 0 {
			t.Fatalf("%s: the probe did not run (exit %d): %v\n%s", c.name, code, runErr, out)
		}
		if want := unityDeprCookWant(c.late); strings.TrimSpace(out) != want {
			t.Fatalf("%s: the generated C# (unity) answered\n%s\nwant\n%s", c.name, out, want)
		}
	}
}
