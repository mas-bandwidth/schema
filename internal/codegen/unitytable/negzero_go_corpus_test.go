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

// EVERY C# (unity) WRITER STORES +0 FOR A COMPRESSED FLOAT'S -0
// (docs/SPEC-TABLES.md §3.3, ruled 2026-11-10 fixed-compressed-negzero), held
// to the 15-row corpus testdata/conformance/feature-witnesses/
// compressed-negzero-go. Each row loads <base>.<form>.bin natively, sets
// <path> to -0 IN THE NATIVE VALUE after the load, and saves:
//
//   - <form>: the variable save or the fixed-form save owes
//     <expect>.<form>.bin byte for byte;
//   - json, cook: the text writer and the cook owe what they write for the
//     value loaded from <expect> (the +0 twin; a plain row's twin holds -0);
//   - a fixed row also owes the twin's variable save and message save;
//   - a variable row on Var also runs the retaining save, which owes
//     <expect>.variable.bin.
//
// A plain row is the control: its json and cook must differ from the base's
// (a plain float's -0 rides), so a writer that normalized every float fails.
func TestUnityCompressedNegZeroGoCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect: every writer over the compressed -0 corpus")
	schema, rows := unityNegZeroGoCorpus(t)
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, schema), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code, _, err := runUnityProbe(t, files, unityNegZeroGoMain(rows), nil)
	if err != nil {
		t.Fatalf("probe build/exec error: %v\n%s", err, out)
	}
	for _, row := range rows {
		for _, check := range unityNegZeroGoChecks(row) {
			if !strings.Contains(out, "ROW "+row.name+"/"+check+": ok") {
				t.Errorf("row %s/%s is not held:\n%s", row.name, check, out)
			}
		}
	}
	if code != 0 && !t.Failed() {
		t.Fatalf("probe exited %d:\n%s", code, out)
	}
}

const unityNegZeroGoCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

type unityNegZeroGoRow struct {
	name, form, root, path string
	plain                  bool
	base, expect           []byte // <stem>.<form>.bin
	baseVar, expectVar     []byte // <stem>.variable.bin
}

func unityNegZeroGoChecks(r unityNegZeroGoRow) []string {
	checks := []string{r.form, "json", "cook"}
	if r.form == "fixed" {
		checks = append(checks, "typed", "message")
	}
	if r.form == "variable" && r.root == "Var" {
		checks = append(checks, "retain")
	}
	return checks
}

func unityNegZeroGoCorpus(t *testing.T) (string, []unityNegZeroGoRow) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(unityNegZeroGoCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(unityNegZeroGoCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(stem, form string) []byte {
		b, err := os.ReadFile(filepath.Join(unityNegZeroGoCorpusDir, stem+"."+form+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var rows []unityNegZeroGoRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, unityNegZeroGoRow{
			name: f[0], form: f[1], root: f[2], path: f[4], plain: f[3] != f[5],
			base: wire(f[3], f[1]), expect: wire(f[5], f[1]),
			baseVar: wire(f[3], "variable"), expectVar: wire(f[5], "variable"),
		})
	}
	if len(rows) != 15 {
		t.Fatalf("cases.tsv names %d rows, want the corpus's 15", len(rows))
	}
	return string(schema), rows
}

// unityNegZeroGoMember is a corpus path as a C# (unity) member: arrays and
// keyed slots are positional buffers (`ks[Low]` is slot Level.Low - 1),
// `pick.v` a union's arm.
func unityNegZeroGoMember(path string) string {
	m := strings.ToUpper(path[:1]) + path[1:]
	if head, arm, ok := strings.Cut(m, "."); ok {
		m = head + "." + strings.ToUpper(arm[:1]) + arm[1:]
	}
	if open := strings.IndexByte(m, '['); open >= 0 {
		key := m[open+1 : len(m)-1]
		if key[0] < '0' || key[0] > '9' {
			m = m[:open] + "[(int)Nzgo.Level." + key + " - 1]"
		}
	}
	return m
}

func unityNegZeroGoMain(rows []unityNegZeroGoRow) string {
	var b strings.Builder
	b.WriteString(`using System;
using Nzgo;

static class ProbeMain
{
    static string Hex(byte[] b) { return BitConverter.ToString(b).Replace("-", ""); }
    static int Same(string name, bool loaded, byte[] got, byte[] want)
    {
        bool ok = loaded && got.Length > 0 && got.AsSpan().SequenceEqual(want);
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED loaded=" + loaded + " " + Hex(got) + " vs " + Hex(want)));
        return ok ? 0 : 1;
    }
    // a plain row's control: the -0 rides, so the writer's bytes differ from the base's
    static int Rides(string name, bool loaded, byte[] got, byte[] twin, byte[] baseBytes)
    {
        bool ok = loaded && got.Length > 0 && got.AsSpan().SequenceEqual(twin) && !got.AsSpan().SequenceEqual(baseBytes);
        Console.WriteLine("ROW " + name + ": " + (ok ? "ok" : "FAILED loaded=" + loaded + " " + Hex(got) + " vs twin " + Hex(twin) + " base " + Hex(baseBytes)));
        return ok ? 0 : 1;
    }
    static byte[] Cut(byte[] buf, long n) { return n < 0 ? new byte[0] : buf.AsSpan(0, (int)n).ToArray(); }
    static byte[] Sized(long n) { return new byte[Math.Max(0, n)]; }
    static byte[] Save(Var v) { byte[] buf = Sized(Schema.VarMeasure(ref v)); return Cut(buf, Schema.VarSave(ref v, buf)); }
    static byte[] Save(Fix v) { byte[] buf = Sized(Schema.FixMeasure(ref v)); return Cut(buf, Schema.FixSave(ref v, buf)); }
    static byte[] Json(Var v) { byte[] buf = Sized(Schema.VarToJsonMeasure(ref v)); return Cut(buf, Schema.VarToJson(ref v, buf)); }
    static byte[] Json(Fix v) { byte[] buf = Sized(Schema.FixToJsonMeasure(ref v)); return Cut(buf, Schema.FixToJson(ref v, buf)); }
    static byte[] Cook(Var v) { byte[] buf = Sized(Schema.VarCookMeasure(ref v)); return Schema.VarCook(ref v, buf) ? buf : new byte[0]; }
    static byte[] Cook(Fix v) { byte[] buf = Sized(Schema.FixCookMeasure(ref v)); return Schema.FixCook(ref v, buf) ? buf : new byte[0]; }
    static byte[] Message(Fix v) { Fix[] one = new Fix[] { v }; byte[] buf = Sized(Schema.FixMeasureMessages(one)); return Cut(buf, Schema.FixSaveMessages(one, buf)); }
    static byte[] SaveFixed(Fix v) { Fix[] one = new Fix[] { v }; byte[] buf = Sized(Schema.FixFixedMeasure(1)); return Cut(buf, Schema.FixFixedSave(one, buf)); }
    static byte[] Retained(ref Var v, Schema.TableRetain retain) { var rep = new Schema.TableReport(); byte[] buf = Sized(Schema.VarMeasureRetain(ref v, retain)); return Cut(buf, Schema.VarSaveRetain(ref v, retain, buf, ref rep)); }
    static Var LoadVar(byte[] bytes, out bool ok) { Var v = Schema.NewVar(); var rep = new Schema.TableReport(); ok = Schema.VarLoad(ref v, bytes, ref rep); return v; }
    static Fix LoadFix(byte[] bytes, out bool ok) { Fix v = Schema.NewFix(); var rep = new Schema.TableReport(); ok = Schema.FixLoad(ref v, bytes, ref rep); return v; }
    static Fix LoadFixed(byte[] bytes, out bool ok) { Fix[] one = new Fix[] { Schema.NewFix() }; var rep = new Schema.TableReport(); ok = Schema.FixFixedLoad(one, bytes, ref rep) == 1; return one[0]; }

    static int Main()
    {
        int bad = 0;
`)
	for _, r := range rows {
		m := unityNegZeroGoMember(r.path)
		fmt.Fprintf(&b, "        { // %s\n", r.name)
		fmt.Fprintf(&b, "            byte[] baseVar = %s, expectVar = %s;\n", unityNegZeroBytes(r.baseVar), unityNegZeroBytes(r.expectVar))
		if r.form == "fixed" {
			fmt.Fprintf(&b, "            byte[] baseFixed = %s, expectFixed = %s;\n", unityNegZeroBytes(r.base), unityNegZeroBytes(r.expect))
			b.WriteString("            Fix v = LoadFixed(baseFixed, out bool loaded); Fix twin = LoadFixed(expectFixed, out bool twinLoaded); Fix plain = LoadFixed(baseFixed, out _);\n")
		} else {
			fmt.Fprintf(&b, "            %[1]s v = Load%[1]s(baseVar, out bool loaded); %[1]s twin = Load%[1]s(expectVar, out bool twinLoaded); %[1]s plain = Load%[1]s(baseVar, out _);\n", r.root)
		}
		fmt.Fprintf(&b, "            v.%s = -0.0f; loaded = loaded && twinLoaded;\n", m)
		if r.form == "fixed" {
			fmt.Fprintf(&b, "            bad += Same(%q, loaded, SaveFixed(v), expectFixed);\n", r.name+"/fixed")
			fmt.Fprintf(&b, "            bad += Same(%q, loaded, Save(v), Save(twin));\n", r.name+"/typed")
			fmt.Fprintf(&b, "            bad += Same(%q, loaded, Message(v), Message(twin));\n", r.name+"/message")
		} else {
			fmt.Fprintf(&b, "            bad += Same(%q, loaded, Save(v), expectVar);\n", r.name+"/variable")
		}
		for _, w := range []string{"Json", "Cook"} {
			check := strings.ToLower(w)
			if r.plain {
				fmt.Fprintf(&b, "            bad += Rides(%q, loaded, %s(v), %s(twin), %s(plain));\n", r.name+"/"+check, w, w, w)
			} else {
				fmt.Fprintf(&b, "            bad += Same(%q, loaded, %s(v), %s(twin));\n", r.name+"/"+check, w, w)
			}
		}
		if r.form == "variable" && r.root == "Var" {
			b.WriteString("            { var retain = new Schema.TableRetain(); var rep = new Schema.TableReport(); Var kept = Schema.NewVar(); bool keptLoaded = Schema.VarLoadRetain(ref kept, baseVar, retain, ref rep);\n")
			fmt.Fprintf(&b, "              kept.%s = -0.0f; bad += Same(%q, keptLoaded, Retained(ref kept, retain), expectVar); }\n", m, r.name+"/retain")
		}
		b.WriteString("        }\n")
	}
	b.WriteString(`        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`)
	return b.String()
}
