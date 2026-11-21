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

// THE VARIABLE WRITER STORES +0 FOR A COMPRESSED FLOAT'S -0 (docs/SPEC-TABLES.md
// §3.3, ruled 2026-11-10 fixed-compressed-negzero): the same bytes it stores
// for +0, on every C# (modern) path that writes the variable form. The rows
// are the committed corpus testdata/conformance/feature-witnesses/
// compressed-negzero-writers (cases.tsv): each loads the reference's +0 twin,
// sets the field to -0 in the NATIVE value after the load, saves, and owes
// the reference bytes. The value rows cover the descriptor walk (a variable
// table: a leaf under a non-zero default, a leaf whose -0 is its +0 default,
// an array element stored because its neighbour rides) and the typed writer
// (a fixed table's variable-form save); the region rows cover the region
// writer. A plain float keeps its -0 (the control rows).
func TestCSharpVariableWriterStoresCompressedNegZeroAsPositive(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs the generated C# writer)")
	schema, rows := csNegZeroWitness(t)
	out, err := csNegZeroWriteRun(t, schema, csNegZeroWriteMain(rows))
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for _, row := range rows {
		if !strings.Contains(string(out), "ROW "+row.name+": ok") {
			t.Errorf("row %s is not held:\n%s", row.name, out)
		}
	}
}

const csNegZeroWitnessDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-writers"

type csNegZeroRow struct {
	name, carrier, root, path string
	base, expect              []byte
}

// csNegZeroWitness reads the corpus: its unit, and every row with its two
// pinned wires.
func csNegZeroWitness(t *testing.T) (string, []csNegZeroRow) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(csNegZeroWitnessDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(csNegZeroWitnessDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(csNegZeroWitnessDir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var rows []csNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, csNegZeroRow{name: f[0], carrier: f[1], root: f[2], path: f[4], base: wire(f[3]), expect: wire(f[5])})
	}
	if len(rows) == 0 {
		t.Fatal("cases.tsv names no row")
	}
	return string(schema), rows
}

// csNegZeroMember is a corpus path as a C# member: `xs[0]` is `Xs[0]`.
func csNegZeroMember(path string) string {
	return strings.ToUpper(path[:1]) + path[1:]
}

func csNegZeroBytes(b []byte) string {
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

// csNegZeroWriteMain is the probe over the rows: one block per row.
func csNegZeroWriteMain(rows []csNegZeroRow) string {
	var b strings.Builder
	b.WriteString(`using System;
using System.Runtime.InteropServices;
using Nzw;

static class ProbeMain
{
    static string Hex(byte[] b) { return Convert.ToHexString(b); }
    static int Check(string name, bool loaded, byte[] got, byte[] want)
    {
        bool ok = loaded && got.AsSpan().SequenceEqual(want);
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED loaded=" + loaded + " " + Hex(got) + " vs " + Hex(want)));
        return ok ? 0 : 1;
    }
    static byte[] Cut(byte[] buf, long n) { return buf.AsSpan(0, (int)Math.Max(0, n)).ToArray(); }
    static byte[] Save(Var v) { byte[] buf = new byte[Schema.VarMeasure(v)]; return Cut(buf, Schema.VarSave(v, buf)); }
    static byte[] Save(Fix v) { byte[] buf = new byte[Schema.FixMeasure(v)]; return Cut(buf, Schema.FixSave(v, buf)); }
    static byte[] SaveRegion(IntPtr region) { byte[] buf = new byte[Schema.VarMeasure(region)]; return Cut(buf, Schema.VarSave(region, buf)); }

    static unsafe int Main()
    {
        int bad = 0;
        const long cap = 1 << 16;
`)
	for _, r := range rows {
		m := csNegZeroMember(r.path)
		switch r.carrier {
		case "value":
			fmt.Fprintf(&b, "        { var v = new %s(); bool loaded = Schema.%sLoad(v, %s, new TableReport()); v.%s = -0.0f; bad += Check(%q, loaded, Save(v), %s); }\n",
				r.root, r.root, csNegZeroBytes(r.base), m, r.name, csNegZeroBytes(r.expect))
		case "region":
			fmt.Fprintf(&b, "        { IntPtr region = Marshal.AllocHGlobal((IntPtr)cap); try { %sRow* row = Schema.%sLoad(region, cap, %s, new TableReport()); if (row != null) { row->%s = -0.0f; } bad += Check(%q, row != null, row != null ? SaveRegion(region) : new byte[0], %s); } finally { Marshal.FreeHGlobal(region); } }\n",
				r.root, r.root, csNegZeroBytes(r.base), m, r.name, csNegZeroBytes(r.expect))
		default:
			fmt.Fprintf(&b, "        Console.WriteLine(\"ROW %s: FAILED unknown carrier %s\"); bad++;\n", r.name, r.carrier)
		}
	}
	b.WriteString(`        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`)
	return b.String()
}

func csNegZeroWriteRun(t *testing.T, schema, probe string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	u := unitFrom(t, schema)
	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
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
	runtimeDir := os.Getenv("SERIALIZE_CS")
	if runtimeDir == "" {
		runtimeDir = "../../../../serialize.cs"
	}
	runtimeDir, err = filepath.Abs(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Serialize.cs", "Int128Pair.cs"} {
		path := filepath.Join(runtimeDir, "src", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the C# runtime checkout: %v", err)
		}
		sources = append(sources, path)
	}
	main := filepath.Join(dir, "NegZeroWrite.cs")
	if err := os.WriteFile(main, []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "csnegzerowrite.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-nowarn:CS8500", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	args = append(args, main)
	// both children through testproc, so neither outlives the test
	// (internal/ci's TestEveryTestChildIsBounded)
	compile := testproc.Command(t, dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	compileOut, compileErr := compile.CombinedOutput()
	if compileErr != nil {
		return compileOut, fmt.Errorf("compile: %w", compileErr)
	}
	cfg := fmt.Sprintf("{\"runtimeOptions\":{\"tfm\":\"net10.0\",\"framework\":{\"name\":\"Microsoft.NETCore.App\",\"version\":%q}}}\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "csnegzerowrite.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := testproc.Command(t, dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	runOut, runErr := run.CombinedOutput()
	return append(compileOut, runOut...), runErr
}
