package cstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// csF32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S RANGE
// IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what each
// row owes.
const csF32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

// CSF32EndsDriver runs every cases.tsv row through the generated C# of
// Probe.schema. A read refusal is the load's false (variable, JSON) or -1
// (fixed); a write refusal is the writer's Debug.Assert (C# is an assert leg,
// docs/SPEC.md §5.1), trapped by a throwing trace listener in a DEBUG build, or
// a negative return. Every accepted value is saved, read back and saved again,
// byte for byte against the corpus.
const csF32EndsDriver = `using System;
using System.Diagnostics;
using System.IO;
using F32ends;

sealed class F32EndsAssertTrap : Exception { public F32EndsAssertTrap(string m) : base(m) { } }

sealed class F32EndsListener : TraceListener
{
    public override void Write(string message) { }
    public override void WriteLine(string message) { }
    public override void Fail(string message) { throw new F32EndsAssertTrap(message ?? "assert"); }
    public override void Fail(string message, string detail) { throw new F32EndsAssertTrap((message ?? "") + " " + (detail ?? "")); }
}

static class F32EndsProbe
{
    static string corpus;
    static int bad;

    static byte[] File(string name) { return System.IO.File.ReadAllBytes(Path.Combine(corpus, name)); }
    static string Hex(ReadOnlySpan<byte> b) { return Convert.ToHexString(b).ToLowerInvariant(); }
    static void Fail(string row, string what) { Console.WriteLine("ROW " + row + ": FAILED " + what); bad++; }

    // Save is the root's own form: V the variable form, F the fixed form. null
    // is a refusal (an assert or a negative return).
    static byte[] Save(object value, string form)
    {
        try
        {
            if (form == "variable")
            {
                V v = (V)value;
                byte[] buf = new byte[Schema.VMeasure(v)];
                long n = Schema.VSave(v, buf);
                return n < 0 ? null : buf.AsSpan(0, (int)n).ToArray();
            }
            F f = (F)value;
            byte[] fbuf = new byte[Schema.FFixedMeasure(1)];
            long fn = Schema.FFixedSave(f, fbuf);
            return fn < 0 ? null : fbuf.AsSpan(0, (int)fn).ToArray();
        }
        catch (F32EndsAssertTrap) { return null; }
    }

    // Load reads bytes of the root's own form; reason is the report's.
    static object Load(byte[] data, string root, out bool ok, out string reason)
    {
        TableReport r = new TableReport();
        if (root == "V")
        {
            V v = new V();
            ok = Schema.VLoad(v, data, r);
            reason = r.Reason;
            return v;
        }
        F f = new F();
        TableFixedEntry[] plan = new TableFixedEntry[8192];
        ok = Schema.FFixedLoad(f, data, plan, r) == 1;
        reason = r.Reason;
        return f;
    }

    static object ReadJson(byte[] text, string root, out bool ok)
    {
        TableReport r = new TableReport();
        if (root == "V") { V v = new V(); ok = Schema.VFromJson(v, text, r); return v; }
        F f = new F(); ok = Schema.FFromJson(f, text, r); return f;
    }

    static void Set(object value, string path, float x)
    {
        if (value is V v) { if (path == "r") { v.R = x; } else { v.Xs[0] = x; } return; }
        F f = (F)value;
        if (path == "r") { f.R = x; } else { f.Xs[0] = x; }
    }

    // SaveTwice saves, reads that save back and saves again: both owe want.
    static void SaveTwice(string row, object value, string root, string form, byte[] want)
    {
        byte[] got = Save(value, form);
        if (got == null) { Fail(row, "the " + form + " save of what was read was refused"); return; }
        if (!got.AsSpan().SequenceEqual(want)) { Fail(row, "the " + form + " save:\n have " + Hex(got) + "\n want " + Hex(want)); return; }
        bool ok; string reason;
        object back = Load(got, root, out ok, out reason);
        if (!ok) { Fail(row, "reading back its own " + form + " save was refused: " + reason); return; }
        byte[] again = Save(back, form);
        if (again == null || !again.AsSpan().SequenceEqual(want)) { Fail(row, "the second " + form + " save: " + (again == null ? "refused" : Hex(again))); }
    }

    static int Main(string[] args)
    {
        corpus = args[0];
        Trace.Listeners.Clear();
        Trace.Listeners.Add(new F32EndsListener());
        foreach (string line in System.IO.File.ReadAllLines(Path.Combine(corpus, "cases.tsv")))
        {
            if (line.Length == 0 || line.StartsWith("#")) { continue; }
            string[] c = line.Split('\t');
            string row = c[0], op = c[1], form = c[2], root = c[3], input = c[4], path = c[5], bits = c[6], expect = c[7];
            string own = root == "V" ? "variable" : "fixed";
            int before = bad;
            try
            {
                if (op == "write")
                {
                    bool jok;
                    object value = ReadJson(File(input + ".json"), root, out jok);
                    if (!jok) { Fail(row, input + ".json was refused"); continue; }
                    Set(value, path, BitConverter.Int32BitsToSingle(unchecked((int)Convert.ToUInt32(bits, 16))));
                    byte[] got = Save(value, form);
                    if (expect == "refused") { if (got != null) { Fail(row, "the " + form + " save was not refused: " + Hex(got)); } }
                    else if (got == null) { Fail(row, "the " + form + " save was refused"); }
                    else
                    {
                        byte[] want = File(expect + "." + form + ".bin");
                        if (!got.AsSpan().SequenceEqual(want)) { Fail(row, "the " + form + " save:\n have " + Hex(got) + "\n want " + Hex(want)); }
                    }
                }
                else
                {
                    bool ok;
                    string reason = null;
                    object value = form == "json"
                        ? ReadJson(File(input + ".json"), root, out ok)
                        : Load(File(input + "." + form + ".bin"), root, out ok, out reason);
                    if (expect == "refused")
                    {
                        if (ok) { Fail(row, "the " + form + " read was not refused"); }
                        else if (form != "json" && reason != "out_of_range") { Fail(row, "refused, but not out_of_range: " + reason); }
                    }
                    else if (!ok) { Fail(row, "the " + form + " read refused the value at the float32 end: " + reason); }
                    else { SaveTwice(row, value, root, own, File(expect + "." + own + ".bin")); }
                }
            }
            catch (Exception e) { Fail(row, e.ToString()); }
            if (bad == before) { Console.WriteLine("ROW " + row + ": ok"); }
        }
        return bad == 0 ? 0 : 1;
    }
}
`

// TestCompressedF32EndsCorpus runs the compressed-f32-ends corpus through the
// generated C#: under max = 0.1 the end is 0.1f (bits 3dcccccd), which every
// reader accepts and every writer saves byte for byte, and one float32 step
// past either end is refused by every reader and asserted by every writer. C#
// emits both the variable and the fixed form and JSON, so no row is skipped.
func TestCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it runs the generated C# over the compressed-f32-ends corpus)")
	corpus, err := filepath.Abs(csF32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	schemaSrc, err := os.ReadFile(filepath.Join(corpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	out, runErr := csF32EndsRun(t, string(schemaSrc), corpus)
	csF32EndsCheck(t, corpus, out, runErr)
}

// csF32EndsRun generates Probe.schema, compiles it with the driver as a DEBUG
// build and runs every row.
func csF32EndsRun(t *testing.T, schemaSrc, corpus string) (string, error) {
	t.Helper()
	files, err := Generate(unitFrom(t, schemaSrc))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".cs") {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, p)
	}
	driver := filepath.Join(dir, "F32EndsProbe.cs")
	if err := os.WriteFile(driver, []byte(csF32EndsDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, driver)

	csc, refs, runtime := csDotnetToolchain(t)
	app := filepath.Join(dir, "f32ends.dll")
	args := []string{"exec", csc, "-nologo", "-unsafe", "-define:DEBUG", "-define:TRACE", "-nowarn:CS8500", "-out:" + app}
	for _, r := range refs {
		args = append(args, "-r:"+r)
	}
	args = append(args, sources...)
	compile := testproc.Command(t, csDotnet(), args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc failed: %v\n%s", err, out)
	}
	cfg := fmt.Sprintf(`{"runtimeOptions":{"tfm":"net10.0","framework":{"name":"Microsoft.NETCore.App","version":%q}}}`+"\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "f32ends.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := testproc.Command(t, csDotnet(), "exec", app, corpus)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, runErr := run.CombinedOutput()
	return string(out), runErr
}

// csF32EndsCheck holds every cases.tsv row to its own "ok" line.
func csF32EndsCheck(t *testing.T, corpus, out string, runErr error) {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(corpus, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for line := range strings.SplitSeq(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, _ := strings.Cut(line, "\t")
		rows++
		if !strings.Contains(out, "ROW "+name+": ok\n") {
			t.Errorf("row %s is not green", name)
		}
	}
	if rows == 0 {
		t.Fatal("cases.tsv holds no rows")
	}
	if t.Failed() || runErr != nil {
		t.Fatalf("the C# corpus run (%v):\n%s", runErr, out)
	}
}
