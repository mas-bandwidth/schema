package cstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY C# (modern) WRITER STORES +0 FOR A COMPRESSED FLOAT'S -0
// (docs/SPEC-TABLES.md §3.3, ruled 2026-11-10 fixed-compressed-negzero), held
// to the 15-row corpus testdata/conformance/feature-witnesses/
// compressed-negzero-go. Each row loads <base>.<form>.bin natively, sets
// <path> to -0 IN THE NATIVE VALUE after the load, and saves:
//
//   - <form>: the variable save (a variable root: the descriptor walk; a
//     fixed root: the typed writer) or the fixed-form save owes
//     <expect>.<form>.bin byte for byte;
//   - json, cook: the text writer and the cook owe what they write for the
//     value loaded from <expect> (the +0 twin; a plain row's twin holds -0);
//   - a fixed row also owes the twin's variable save (the typed writer over
//     arrays, keyed slots and union arms) and the twin's message save;
//   - a variable row on Var also runs the native region: its variable save
//     owes <expect>.variable.bin and its cook owes the twin region's cook.
//
// A plain row is the control: its json and cook must differ from the base's
// (a plain float's -0 rides), so a writer that normalized every float fails.
func TestCSharpCompressedNegZeroGoCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it compiles and runs every generated C# writer over the -0 corpus)")
	schema, rows := csNegZeroGoCorpus(t)
	out, err := csNegZeroWriteRun(t, schema, csNegZeroGoMain(rows))
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, out)
	}
	for _, row := range rows {
		for _, check := range csNegZeroGoChecks(row) {
			if !strings.Contains(string(out), "ROW "+row.name+"/"+check+": ok") {
				t.Errorf("row %s/%s is not held:\n%s", row.name, check, out)
			}
		}
	}
}

// CompressedNegZeroGoCorpusDir is the shared corpus every leg runs.
const csNegZeroGoCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

type csNegZeroGoRow struct {
	name, form, root, path string
	plain                  bool
	base, expect           []byte // <stem>.<form>.bin
	baseVar, expectVar     []byte // <stem>.variable.bin: the value the json and cook checks load
}

// csNegZeroGoChecks names the checks the probe prints for a row.
func csNegZeroGoChecks(r csNegZeroGoRow) []string {
	checks := []string{r.form, "json", "cook"}
	if r.form == "fixed" {
		checks = append(checks, "typed", "message")
	}
	if r.form == "variable" && r.root == "Var" {
		checks = append(checks, "region", "region-cook")
	}
	return checks
}

func csNegZeroGoCorpus(t *testing.T) (string, []csNegZeroGoRow) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join(csNegZeroGoCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(csNegZeroGoCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(stem, form string) []byte {
		b, err := os.ReadFile(filepath.Join(csNegZeroGoCorpusDir, stem+"."+form+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var rows []csNegZeroGoRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, csNegZeroGoRow{
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

// csNegZeroGoMember is a corpus path as a C# member of the value (region
// false) or of the native region's row (region true): `ks[Low]` is a keyed
// slot (the value's TableKeyed takes the variant, the row is positional),
// `pick.v` a union's arm.
func csNegZeroGoMember(path string, region bool) string {
	m := strings.ToUpper(path[:1]) + path[1:]
	if head, arm, ok := strings.Cut(m, "."); ok {
		m = head + "." + strings.ToUpper(arm[:1]) + arm[1:]
	}
	if open := strings.IndexByte(m, '['); open >= 0 {
		key := m[open+1 : len(m)-1]
		if key[0] < '0' || key[0] > '9' {
			slot := "(int)Level." + key
			if region {
				slot += " - 1"
			}
			m = m[:open] + "[" + slot + "]"
		}
	}
	return m
}

func csNegZeroGoMain(rows []csNegZeroGoRow) string {
	var b strings.Builder
	b.WriteString(`using System;
using System.Runtime.InteropServices;
using Nzgo;

static class ProbeMain
{
    static string Hex(byte[] b) { return Convert.ToHexString(b); }
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
    static byte[] Save(Var v) { byte[] buf = new byte[Schema.VarMeasure(v)]; return Cut(buf, Schema.VarSave(v, buf)); }
    static byte[] Save(Fix v) { byte[] buf = new byte[Schema.FixMeasure(v)]; return Cut(buf, Schema.FixSave(v, buf)); }
    static byte[] Json(Var v) { byte[] buf = new byte[Schema.VarToJsonMeasure(v)]; return Cut(buf, Schema.VarToJson(v, buf)); }
    static byte[] Json(Fix v) { byte[] buf = new byte[Schema.FixToJsonMeasure(v)]; return Cut(buf, Schema.FixToJson(v, buf)); }
    static byte[] Cook(Var v) { byte[] buf = new byte[Schema.VarCookMeasure(v)]; return Schema.VarCook(v, buf) ? buf : new byte[0]; }
    static byte[] Cook(Fix v) { byte[] buf = new byte[Schema.FixCookMeasure(v)]; return Schema.FixCook(v, buf) ? buf : new byte[0]; }
    static byte[] Message(Fix v) { Fix[] one = new Fix[] { v }; byte[] buf = new byte[Schema.FixMeasureMessages(one)]; return Cut(buf, Schema.FixSaveMessages(one, buf)); }
    static byte[] SaveFixed(Fix v) { byte[] buf = new byte[Schema.FixFixedMeasure(1)]; return Cut(buf, Schema.FixFixedSave(v, buf)); }
    static Var LoadVar(byte[] bytes, out bool ok) { Var v = new Var(); ok = Schema.VarLoad(v, bytes, new TableReport()); return v; }
    static Fix LoadFix(byte[] bytes, out bool ok) { Fix v = new Fix(); ok = Schema.FixLoad(v, bytes, new TableReport()); return v; }
    static Fix LoadFixed(byte[] bytes, out bool ok) { Fix v = new Fix(); ok = Schema.FixFixedLoad(v, bytes, new TableFixedEntry[8192], new TableReport()) == 1; return v; }
    static byte[] SaveRegion(IntPtr region) { byte[] buf = new byte[Schema.VarMeasure(region)]; return Cut(buf, Schema.VarSave(region, buf)); }
    static byte[] CookRegion(IntPtr region) { byte[] buf = new byte[Schema.VarCookMeasure(region)]; return Schema.VarCook(region, buf) ? buf : new byte[0]; }

    static unsafe int Main()
    {
        int bad = 0;
        const long cap = 1 << 16;
`)
	for _, r := range rows {
		m := csNegZeroGoMember(r.path, false)
		fmt.Fprintf(&b, "        { // %s\n", r.name)
		fmt.Fprintf(&b, "            byte[] baseVar = %s, expectVar = %s;\n", csNegZeroBytes(r.baseVar), csNegZeroBytes(r.expectVar))
		if r.form == "fixed" {
			fmt.Fprintf(&b, "            byte[] baseFixed = %s, expectFixed = %s;\n", csNegZeroBytes(r.base), csNegZeroBytes(r.expect))
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
			rm := csNegZeroGoMember(r.path, true)
			b.WriteString("            IntPtr region = Marshal.AllocHGlobal((IntPtr)cap), twinRegion = Marshal.AllocHGlobal((IntPtr)cap), plainRegion = Marshal.AllocHGlobal((IntPtr)cap);\n")
			b.WriteString("            try\n            {\n")
			b.WriteString("                VarRow* row = Schema.VarLoad(region, cap, baseVar, new TableReport());\n")
			b.WriteString("                bool regionLoaded = row != null && Schema.VarLoad(twinRegion, cap, expectVar, new TableReport()) != null && Schema.VarLoad(plainRegion, cap, baseVar, new TableReport()) != null;\n")
			fmt.Fprintf(&b, "                if (row != null) { row->%s = -0.0f; }\n", rm)
			fmt.Fprintf(&b, "                bad += Same(%q, regionLoaded, regionLoaded ? SaveRegion(region) : new byte[0], expectVar);\n", r.name+"/region")
			if r.plain {
				fmt.Fprintf(&b, "                bad += Rides(%q, regionLoaded, regionLoaded ? CookRegion(region) : new byte[0], regionLoaded ? CookRegion(twinRegion) : new byte[0], regionLoaded ? CookRegion(plainRegion) : new byte[0]);\n", r.name+"/region-cook")
			} else {
				fmt.Fprintf(&b, "                bad += Same(%q, regionLoaded, regionLoaded ? CookRegion(region) : new byte[0], regionLoaded ? CookRegion(twinRegion) : new byte[0]);\n", r.name+"/region-cook")
			}
			b.WriteString("            }\n            finally { Marshal.FreeHGlobal(region); Marshal.FreeHGlobal(twinRegion); Marshal.FreeHGlobal(plainRegion); }\n")
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
