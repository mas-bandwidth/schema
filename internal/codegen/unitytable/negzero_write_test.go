package unitytable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE VARIABLE WRITER STORES +0 FOR A COMPRESSED FLOAT'S -0 (docs/SPEC-TABLES.md
// §3.3, ruled 2026-11-10 fixed-compressed-negzero): the same bytes it stores
// for +0. The rows are the committed corpus testdata/conformance/
// feature-witnesses/compressed-negzero-writers (cases.tsv), its `value` rows
// (the C# (unity) dialect has no native region): each loads the reference's
// +0 twin, sets the field to -0 in the native value after the load, saves,
// and owes the reference bytes — a leaf under a non-zero default, a leaf whose
// -0 is its +0 default, an array element stored because its neighbour rides,
// and a fixed table's leaf. A plain float keeps its -0 (the control row).
func TestUnityVariableWriterStoresCompressedNegZeroAsPositive(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect compressed -0 on the variable writer")
	schema, rows := unityNegZeroWitness(t)
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityNegZeroWriteMain(rows), nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	for _, row := range rows {
		if !strings.Contains(out, "ROW "+row.name+": ok") {
			t.Errorf("row %s is not held:\n%s", row.name, out)
		}
	}
	if code != 0 && !t.Failed() {
		t.Fatalf("probe exited %d:\n%s", code, out)
	}
}

const unityNegZeroWitnessDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-writers"

type unityNegZeroRow struct {
	name, root, path string
	base, expect     []byte
}

// unityNegZeroWitness reads the corpus: its unit, and every `value` row with
// its two pinned wires.
func unityNegZeroWitness(t *testing.T) (string, []unityNegZeroRow) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(unityNegZeroWitnessDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(unityNegZeroWitnessDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(unityNegZeroWitnessDir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var rows []unityNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		if f[1] != "value" {
			continue
		}
		rows = append(rows, unityNegZeroRow{name: f[0], root: f[2], path: f[4], base: wire(f[3]), expect: wire(f[5])})
	}
	if len(rows) == 0 {
		t.Fatal("cases.tsv names no value row")
	}
	return string(schema), rows
}

func unityNegZeroBytes(b []byte) string {
	var s strings.Builder
	s.WriteString("new byte[] {")
	for i, c := range b {
		if i > 0 {
			s.WriteString(",")
		}
		fmt.Fprintf(&s, "0x%02x", c)
	}
	s.WriteString("}")
	return s.String()
}

// unityNegZeroWriteMain is the probe over the rows: one block per row.
func unityNegZeroWriteMain(rows []unityNegZeroRow) string {
	var b strings.Builder
	b.WriteString(`using System;

static class ProbeMain
{
    static string Hex(byte[] b) { return BitConverter.ToString(b).Replace("-", ""); }
    static int Check(string name, bool loaded, byte[] got, byte[] want)
    {
        bool ok = loaded && got.AsSpan().SequenceEqual(want);
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED loaded=" + loaded + " " + Hex(got) + " vs " + Hex(want)));
        return ok ? 0 : 1;
    }
    static byte[] Cut(byte[] buf, long n) { return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray(); }
    static byte[] Save(Nzw.Var v) { long size = Nzw.Schema.VarMeasure(ref v); if (size < 0) { return new byte[0]; } byte[] buf = new byte[size]; return Cut(buf, Nzw.Schema.VarSave(ref v, buf)); }
    static byte[] Save(Nzw.Fix v) { long size = Nzw.Schema.FixMeasure(ref v); if (size < 0) { return new byte[0]; } byte[] buf = new byte[size]; return Cut(buf, Nzw.Schema.FixSave(ref v, buf)); }

    static int Main()
    {
        int bad = 0;
`)
	for _, r := range rows {
		m := strings.ToUpper(r.path[:1]) + r.path[1:]
		fmt.Fprintf(&b, "        { var v = Nzw.Schema.New%s(); var rep = new Nzw.Schema.TableReport(); bool loaded = Nzw.Schema.%sLoad(ref v, %s, ref rep); v.%s = -0.0f; bad += Check(%q, loaded, Save(v), %s); }\n",
			r.root, r.root, unityNegZeroBytes(r.base), m, r.name, unityNegZeroBytes(r.expect))
	}
	b.WriteString(`        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`)
	return b.String()
}
