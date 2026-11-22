package unitytable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// unityF32EndsCorpusDir is the cross-port corpus for A COMPRESSED FLOAT'S
// RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3). Its cases.tsv says what
// each row owes.
const unityF32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

// unityF32EndsProbe runs every cases.tsv row through the generated C# (unity)
// of Probe.schema, the corpus files beside it in its working directory. A read
// refusal is the load's false (variable, JSON) or a count other than 1
// (fixed); a write refusal is the writer's Debug.Assert (an assert leg,
// docs/SPEC.md §5.1), trapped by ProbeCommon's throwing listener in the DEBUG
// unit, or a negative return. Every accepted value is saved, read back and
// saved again, byte for byte against the corpus.
const unityF32EndsProbe = `using System;
using System.IO;
using F32ends;

static class F32EndsProbe
{
    static int bad;

    static string Hex(ReadOnlySpan<byte> b) { return Convert.ToHexString(b).ToLowerInvariant(); }
    static void Fail(string row, string what) { Console.WriteLine("ROW " + row + ": FAILED " + what); bad++; }
    static string Why(Schema.TableReport r) { return r.ReasonText ?? Schema.TableReport.ReasonName(r.Reason); }

    static byte[] SaveV(V v)
    {
        try
        {
            byte[] buf = new byte[Schema.VMeasure(v)];
            long n = Schema.VSave(v, buf);
            return n < 0 ? null : buf.AsSpan(0, (int)n).ToArray();
        }
        catch (AssertTrapException) { return null; }
    }

    static byte[] SaveF(F f)
    {
        try
        {
            byte[] buf = new byte[Schema.FFixedMeasure(1)];
            long n = Schema.FFixedSave(new F[] { f }, buf);
            return n < 0 ? null : buf.AsSpan(0, (int)n).ToArray();
        }
        catch (AssertTrapException) { return null; }
    }

    static bool LoadV(byte[] data, out V v, out Schema.TableReport r)
    {
        v = new V();
        r = new Schema.TableReport();
        return Schema.VLoad(ref v, data, ref r);
    }

    static bool LoadF(byte[] data, out F f, out Schema.TableReport r)
    {
        F[] back = new F[1];
        r = new Schema.TableReport();
        long n = Schema.FFixedLoad(back, data, ref r);
        f = back[0];
        return n == 1;
    }

    static void Check(string row, string form, byte[] got, byte[] want)
    {
        if (got == null) { Fail(row, "the " + form + " save was refused"); }
        else if (!got.AsSpan().SequenceEqual(want)) { Fail(row, "the " + form + " save:\n have " + Hex(got) + "\n want " + Hex(want)); }
    }

    static int Main()
    {
        ProbeCommon.InstallAssertTrap();
        foreach (string line in File.ReadAllLines("cases.tsv"))
        {
            if (line.Length == 0 || line.StartsWith("#")) { continue; }
            string[] c = line.Split('\t');
            string row = c[0], op = c[1], form = c[2], root = c[3], input = c[4], path = c[5], bits = c[6], expect = c[7];
            int before = bad;
            try
            {
                bool ok;
                Schema.TableReport r = new Schema.TableReport();
                V v = new V();
                F f = new F();
                if (op == "write")
                {
                    byte[] text = File.ReadAllBytes(input + ".json");
                    ok = root == "V" ? Schema.VFromJson(ref v, text, ref r) : Schema.FFromJson(ref f, text, ref r);
                    if (!ok) { Fail(row, input + ".json was refused: " + Why(r)); continue; }
                    float x = BitConverter.Int32BitsToSingle(unchecked((int)Convert.ToUInt32(bits, 16)));
                    if (root == "V") { if (path == "r") { v.R = x; } else { v.Xs[0] = x; } }
                    else { if (path == "r") { f.R = x; } else { f.Xs[0] = x; } }
                    byte[] got = form == "variable" ? SaveV(v) : SaveF(f);
                    if (expect == "refused") { if (got != null) { Fail(row, "the " + form + " save was not refused: " + Hex(got)); } }
                    else { Check(row, form, got, File.ReadAllBytes(expect + "." + form + ".bin")); }
                }
                else
                {
                    if (form == "json")
                    {
                        byte[] text = File.ReadAllBytes(input + ".json");
                        ok = root == "V" ? Schema.VFromJson(ref v, text, ref r) : Schema.FFromJson(ref f, text, ref r);
                    }
                    else
                    {
                        byte[] data = File.ReadAllBytes(input + "." + form + ".bin");
                        ok = root == "V" ? LoadV(data, out v, out r) : LoadF(data, out f, out r);
                    }
                    if (expect == "refused")
                    {
                        // refusal-verdict-only (docs/rulings.json): the verdict is checked, never the reason.
                        if (ok) { Fail(row, "the " + form + " read was not refused"); }
                    }
                    else if (!ok) { Fail(row, "the " + form + " read refused the value at the float32 end: " + Why(r)); }
                    else
                    {
                        string own = root == "V" ? "variable" : "fixed";
                        byte[] want = File.ReadAllBytes(expect + "." + own + ".bin");
                        byte[] got = root == "V" ? SaveV(v) : SaveF(f);
                        Check(row, own, got, want);
                        if (got != null)
                        {
                            Schema.TableReport r2;
                            bool back = root == "V" ? LoadV(got, out v, out r2) : LoadF(got, out f, out r2);
                            if (!back) { Fail(row, "reading back its own " + own + " save was refused: " + Why(r2)); }
                            else { Check(row, own + " (second)", root == "V" ? SaveV(v) : SaveF(f), want); }
                        }
                    }
                }
            }
            catch (Exception e) { Fail(row, e.ToString()); }
            if (bad == before) { Console.WriteLine("ROW " + row + ": ok"); }
        }
        return bad == 0 ? 0 : 1;
    }
}
`

// TestUnityCompressedF32EndsCorpus runs the compressed-f32-ends corpus through
// the generated C# (unity): under max = 0.1 the end is 0.1f (bits 3dcccccd),
// which every reader accepts and every writer saves byte for byte, and one
// float32 step past either end is refused by every reader and asserted by
// every writer. The leg emits the variable form, the fixed form and JSON, so no
// row is skipped.
func TestUnityCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc (it runs the generated C# (unity) over the compressed-f32-ends corpus)")
	entries, err := os.ReadDir(unityF32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(unityF32EndsCorpusDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		inputs[e.Name()] = data
	}
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, string(inputs["Probe.schema"])), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, runErr := runUnityProbe(t, files, unityF32EndsProbe, inputs)
	if runErr != nil {
		t.Fatalf("the probe did not run: %v\n%s", runErr, out)
	}
	rows := 0
	for line := range strings.SplitSeq(string(inputs["cases.tsv"]), "\n") {
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
	if t.Failed() || code != 0 {
		t.Fatalf("the C# (unity) corpus run (exit %d):\n%s", code, out)
	}
}
