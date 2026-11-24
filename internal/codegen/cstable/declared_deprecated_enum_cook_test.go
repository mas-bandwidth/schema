package cstable

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE COOK REFUSES A DEPRECATED OR OUT-OF-SET ENUM BEFORE ANY BYTE (SF-25,
// SF-26; docs/SPEC.md §5.1 row 5; docs/SPEC-TABLES.md §2.15, §7.6). C# is an
// assert leg, so the refusal is a Debug.Assert (-define:DEBUG). It must fire
// in the placement pass, before the header is written or the buffer cleared:
// the caller's 0x5a-filled buffer stays untouched. Phase.Next writes; Old
// (deprecated) and 9 (past the set) assert, on the fixed and the variable
// root, in both byte orders.
//
// THE PLANT rides in the test: the same unit with the enum assert moved to
// the write pass only (after the header and the clear) must answer
// "asserted touched", so the test proves it can see a late check.
const csDeprCookProbe = `using System;
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
  foreach (var (tag, order) in new[] { ("le", TableByteOrder.Little), ("be", TableByteOrder.Big) }) {
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

// csDeprCookWant is the probe's answer, with late naming the planted unit's
// answer for a refused value (the assert fires after the header).
func csDeprCookWant(late bool) string {
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

// csDeprEnumAssert is the native cook loop's enum assert as the runtime
// spells it; csDeprEnumAssertLate is the plant, gated to the write pass.
const (
	csDeprEnumAssert     = "System.Diagnostics.Debug.Assert(!(f.Kind == 30 && i < live && "
	csDeprEnumAssertLate = "System.Diagnostics.Debug.Assert(w.Measure || !(f.Kind == 30 && i < live && "
)

func TestCSharpDeclaredDeprecatedEnumCookAssertsBeforeAnyByte(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Parallel()
	src, err := os.ReadFile("../../../testdata/conformance/feature-witnesses/declared-deprecated-enum/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packet)
	planted := maps.Clone(files)
	hits := 0
	for name, data := range planted {
		if n := strings.Count(string(data), csDeprEnumAssert); n > 0 {
			hits += n
			planted[name] = []byte(strings.ReplaceAll(string(data), csDeprEnumAssert, csDeprEnumAssertLate))
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
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := csDeprCookRun(t, c.files)
			if want := csDeprCookWant(c.late); got != want {
				t.Fatalf("the generated C# answered\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func csDeprCookRun(t *testing.T, files map[string][]byte) string {
	t.Helper()
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
	main := filepath.Join(dir, "DeprCookProbe.cs")
	if err := os.WriteFile(main, []byte(csDeprCookProbe), 0o600); err != nil {
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
	return strings.TrimSpace(string(output))
}
