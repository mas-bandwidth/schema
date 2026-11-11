package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE VERDICT DOES NOT DEPEND ON THE REPORT (docs/SPEC-TABLES.md, the FIXED
// read; docs/rulings.json strict-only-read): a value outside its writer's range
// refuses the whole load whether or not the caller passed a report. The report
// only receives the name when one is present.
const fixedVerdictSchema = `package verdictprobe

fixed table Sample {
	count int32 = 0 | min = 0, max = 1000
}
`

const fixedVerdictProbe = `using System;
using System.Buffers.Binary;
using Verdictprobe;

static class VerdictProbe
{
    static int failed = 0;

    static void Pass(string name) { Console.WriteLine("PASS " + name); }
    static void Fail(string name, string what) { Console.WriteLine("FAIL " + name + ": " + what); failed++; }

    static Sample Load(byte[] wire, bool withReport, out long n, out bool refused, out string reason)
    {
        var back = new Sample { Count = 7 };
        var plan = new TableFixedEntry[64];
        TableReport report = withReport ? new TableReport() : null;
        if (withReport) { n = Schema.SampleFixedLoad(back, wire, plan, report); }
        else { n = Schema.SampleFixedLoad(back, wire, plan); }
        refused = report != null && report.Refused;
        reason = report != null ? report.Reason : null;
        return back;
    }

    static void Case(string name, byte[] wire, bool withReport, long wantN, int wantField, bool wantRefused)
    {
        long n;
        bool refused;
        string reason;
        var back = Load(wire, withReport, out n, out refused, out reason);
        if (n != wantN) { Fail(name, "n=" + n + " want " + wantN + " reason=" + reason); return; }
        if (back.Count != wantField) { Fail(name, "field=" + back.Count + " want " + wantField + " reason=" + reason); return; }
        if (refused != wantRefused) { Fail(name, "refused=" + refused + " want " + wantRefused + " reason=" + reason); return; }
        Pass(name);
    }

    static int Main()
    {
        var value = new Sample { Count = 321 };
        byte[] wire = new byte[Schema.SampleFixedMeasure(1)];
        long wrote = Schema.SampleFixedSave(value, wire);
        if (wrote != wire.Length) { Fail("save", "wrote=" + wrote + " want " + wire.Length); return 1; }

        int bodyAt = Schema.TableFixedWire.HeaderBytes + 4 + (int)Schema.SampleFixedLayoutBytes + 8;
        if (BinaryPrimitives.ReadInt32LittleEndian(wire.AsSpan(bodyAt)) != 321)
        {
            Fail("body_offset", "the body's first field is not at the computed offset: " + BinaryPrimitives.ReadInt32LittleEndian(wire.AsSpan(bodyAt)));
            return 1;
        }

        byte[] above = (byte[])wire.Clone();
        BinaryPrimitives.WriteInt32LittleEndian(above.AsSpan(bodyAt), 5000);
        byte[] below = (byte[])wire.Clone();
        BinaryPrimitives.WriteInt32LittleEndian(below.AsSpan(bodyAt), -5);

        Case("in_range_without_report", wire, false, 1, 321, false);
        Case("in_range_with_report", wire, true, 1, 321, false);
        Case("above_max_without_report", above, false, -1, 0, false);
        Case("above_max_with_report", above, true, -1, 0, true);
        Case("below_min_without_report", below, false, -1, 0, false);
        Case("below_min_with_report", below, true, -1, 0, true);

        return failed == 0 ? 0 : 1;
    }
}
`

const (
	fixedVerdictClampVerdict = "            if (clamped != 0 && report != null && !report.Refused) { report.Refused = true; report.Reason = \"out_of_range\"; }\n            return clamped == 0;"
	fixedVerdictClampPrior   = "            if (clamped != 0 && report != null && !report.Refused) { report.Refused = true; report.Reason = \"out_of_range\"; }\n            if (clamped != 0 && report != null) { return false; }\n            return true;"
)

func runFixedVerdictProbe(t *testing.T, edit func(map[string][]byte)) (string, error) {
	t.Helper()
	u := unitFrom(t, fixedVerdictSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		edit(files)
	}
	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	probePath := filepath.Join(dir, "Program.cs")
	if err := os.WriteFile(probePath, []byte(fixedVerdictProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	sources = append(sources, probePath)

	csc, refs, runtime := csDotnetToolchain(t)
	dotnet := csDotnet()
	app := filepath.Join(dir, "verdictprobe.dll")
	args := []string{"exec", csc, "-nologo", "-noconfig", "-target:exe", "-optimize+", "-unsafe",
		"-nullable:annotations", "-langversion:latest", "-nostdlib+", "-out:" + app}
	for _, ref := range refs {
		args = append(args, "-r:"+ref)
	}
	args = append(args, sources...)
	compile := exec.Command(dotnet, args...)
	compile.Env, compile.Dir = csDotnetEnv(dir, dir), dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("csc compile failed: %v\n%s", err, out)
	}

	cfg := fmt.Sprintf(`{"runtimeOptions":{"tfm":"net10.0","framework":{"name":"Microsoft.NETCore.App","version":%q}}}`+"\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "verdictprobe.runtimeconfig.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(dotnet, "exec", app)
	run.Env, run.Dir = csDotnetEnv(dir, dir), dir
	out, err := run.CombinedOutput()
	return string(out), err
}

func TestCSharpFixedVerdictWithoutReport(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Parallel()
	out, err := runFixedVerdictProbe(t, nil)
	if err != nil {
		t.Fatalf("fixed verdict probe failed:\n%s\n%v", out, err)
	}
	for _, name := range []string{
		"in_range_without_report", "in_range_with_report",
		"above_max_without_report", "above_max_with_report",
		"below_min_without_report", "below_min_with_report",
	} {
		if !strings.Contains(out, "PASS "+name) {
			t.Fatalf("missing PASS for %s in:\n%s", name, out)
		}
	}
}

// THE NEGATIVE CONTROL restores the report-dependent verdict in a copy of the
// generated artifact: the clamp signals a refusal only when a report exists.
// The same probe must then go red by name on the no-report rows.
func TestCSharpFixedVerdictWithoutReportNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	t.Parallel()
	out, err := runFixedVerdictProbe(t, func(files map[string][]byte) {
		replaced := false
		for name, data := range files {
			s := string(data)
			if strings.Contains(s, fixedVerdictClampVerdict) {
				files[name] = []byte(strings.ReplaceAll(s, fixedVerdictClampVerdict, fixedVerdictClampPrior))
				replaced = true
			}
		}
		if !replaced {
			t.Fatalf("the generated artifact does not carry the verdict line:\n%s", fixedVerdictClampVerdict)
		}
	})
	if err == nil {
		t.Fatalf("report-dependent verdict control unexpectedly passed:\n%s", out)
	}
	for _, name := range []string{"above_max_without_report", "below_min_without_report"} {
		if !strings.Contains(out, "FAIL "+name) {
			t.Fatalf("report-dependent verdict control did not fail %s by name:\n%s", name, out)
		}
	}
}
