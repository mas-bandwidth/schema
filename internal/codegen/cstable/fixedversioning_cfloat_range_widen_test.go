package cstable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// §5.8 row `cfloat_range_widen`: VOLD `aim float32 | min = -1, max = 1,
// resolution = 0.01`, VNEW widens to min = -2, max = 2. A compressed float
// rides as the float32 itself (SPEC §3.4), and on a cross-version read it is
// held to the WRITER's range the lineage plan carries, like every ranged kind
// (docs/rulings.json strict-only-read; docs/FIXED-FORM-ALGORITHM.md §4.6). A
// forged value past the old writer's max 1.0 but inside the new reader's 2.0
// (`hostile_`, 1.5) refuses the whole load out_of_range, and so does one past
// both (`past_`, 5.0) (docs/SPEC-TABLES.md §4: no clamp-and-count). `old_`
// (0.5) is the un-forged control: a writer range narrower than the reader's
// lands what the writer wrote.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	// THE LANDED VALUE COMES FROM THE CORPUS MANIFEST (schema#1164), compared
	// as BITS: `old_`'s is the manifest's `values=r0.aim`. `hostile_` and
	// `past_` land nothing: each refuses, and every value is back at its
	// declared default.
	oldAim := fmt.Sprintf("0x%08X", csManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"))

	probes := []csVersionProbe{
		{name: "cfloat_range_widen/new_reads_old", reader: "VNEW_cfloat_range_widen",
			older: []string{"VOLD_cfloat_range_widen"}, file: "old_cfloat_range_widen.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the newer reader did not read exactly one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Verdict != Schema.TableWire.Verdict.Ok || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read is not a refusal: reason=" + r.Reason + " verdict=" + r.Verdict);
            }
            if (back[0].Lead != 1u || back[0].Trail != 2u)
            {
                bad += ProbeLog.Fail(@NAME@, "a neighbor moved: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (System.BitConverter.SingleToInt32Bits(back[0].Aim) != @OLDAIM@)
            {
                bad += ProbeLog.Fail(@NAME@, "the old writer's 0.5 did not land whole and uncounted: bits=0x" + System.BitConverter.SingleToInt32Bits(back[0].Aim).ToString("x8"));
            }
            if (n != 1 || r.Verdict != Schema.TableWire.Verdict.Ok)
            {
                bad += ProbeLog.Fail(@NAME@, "an in-range value refused the read: n=" + n + " verdict=" + r.Verdict);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter moved: widened=" + r.Widened + " unknown=" + r.Unknown + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
		{name: "cfloat_range_widen/hostile", reader: "VNEW_cfloat_range_widen",
			older: []string{"VOLD_cfloat_range_widen"}, suffix: "h", file: "hostile_cfloat_range_widen.bin",
			body: csVersionHead + `            _ = want;
            // THE STRICT READ AGAINST THE WRITER'S RANGE (docs/rulings.json
            // strict-only-read, docs/FIXED-FORM-ALGORITHM.md §4.6): 1.5 is past
            // the old writer's max 1.0, which that writer could not have
            // written; the whole load refuses even though the reader's own
            // range 2.0 holds it.
            @TABLE@ fresh = new @TABLE@();
            if (n != -1)
            {
                bad += ProbeLog.Fail(@NAME@, "a value past the writer's bound must refuse the load: n=" + n);
            }
            if (!r.Refused || r.Verdict != Schema.TableWire.Verdict.Refused || r.Reason != "out_of_range")
            {
                bad += ProbeLog.Fail(@NAME@, "a value past the writer's bound is an out_of_range refusal: refused=" + r.Refused + " verdict=" + r.Verdict + " reason=" + r.Reason);
            }
            if (back[0].Lead != fresh.Lead || back[0].Trail != fresh.Trail)
            {
                bad += ProbeLog.Fail(@NAME@, "a refusal left a neighbor written: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (System.BitConverter.SingleToInt32Bits(back[0].Aim) != System.BitConverter.SingleToInt32Bits(fresh.Aim))
            {
                bad += ProbeLog.Fail(@NAME@, "1.5 past the writer's bound landed a value: bits=0x" + System.BitConverter.SingleToInt32Bits(back[0].Aim).ToString("x8"));
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "REFUSE is total: no counter moves: widened=" + r.Widened + " unknown=" + r.Unknown + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
		{name: "cfloat_range_widen/past", reader: "VNEW_cfloat_range_widen",
			older: []string{"VOLD_cfloat_range_widen"}, suffix: "p", file: "past_cfloat_range_widen.bin",
			body: csVersionHead + `            _ = want;
            // THE STRICT READ (docs/SPEC-TABLES.md §4, docs/FIXED-FORM-ALGORITHM.md
            // §4.6): 5.0 past the writer's max 1.0 (and the reader's 2.0) is never
            // clamped; the whole load refuses and every value it wrote is back at
            // its declared default.
            @TABLE@ fresh = new @TABLE@();
            if (n != -1)
            {
                bad += ProbeLog.Fail(@NAME@, "a value past the reader's own bound must refuse the load: n=" + n);
            }
            if (!r.Refused || r.Verdict != Schema.TableWire.Verdict.Refused)
            {
                bad += ProbeLog.Fail(@NAME@, "a value past the reader's own bound is a refusal, never a clamp: refused=" + r.Refused + " verdict=" + r.Verdict);
            }
            if (back[0].Lead != fresh.Lead || back[0].Trail != fresh.Trail)
            {
                bad += ProbeLog.Fail(@NAME@, "a refusal left a neighbor written: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (System.BitConverter.SingleToInt32Bits(back[0].Aim) != System.BitConverter.SingleToInt32Bits(fresh.Aim))
            {
                bad += ProbeLog.Fail(@NAME@, "5.0 past the reader's own bound landed a value: bits=0x" + System.BitConverter.SingleToInt32Bits(back[0].Aim).ToString("x8"));
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "REFUSE is total: no counter moves: widened=" + r.Widened + " unknown=" + r.Unknown + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
	}

	var calls []string
	for i, p := range probes {
		ns, table, files := csVersionGenerate(t, p)
		gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
		csVersionWrite(t, gen, files)
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.NewReplacer(
			"@OLDAIM@", oldAim,
			"@NAME@", fmt.Sprintf("%q", p.name),
			"@TABLE@", table,
			"@FILE@", fmt.Sprintf("%q", p.file),
		).Replace(p.body)
		src := fmt.Sprintf(`// generated by internal/codegen/cstable's versioning harness: one probe per row per COLUMN
using System;

namespace %s
{
    static class %s
    {
        public static int Run(string corpus)
        {
            int bad = 0;
%s
            if (bad == 0) { Console.WriteLine("ROW " + %s + ": ok"); }
            return bad;
        }
    }
}
`, ns, probeClass, body, fmt.Sprintf("%q", p.name))
		if err := os.WriteFile(filepath.Join(dir, probeClass+".cs"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, fmt.Sprintf("            bad += %s.%s.Run(corpus);", ns, probeClass))
	}

	main := fmt.Sprintf(`using System;

static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

static class ProbeMain
{
    static int Main()
    {
        string corpus = Environment.GetEnvironmentVariable("SCHEMA_FIXEDFORM_CORPUS");
        int bad = 0;
        try
        {
%s
        }
        catch (Exception e)
        {
            Console.WriteLine("ROW harness: FAILED " + e.ToString());
            return 1;
        }
        Console.WriteLine(bad == 0 ? "cs versioning: every row green" : "cs versioning: " + bad + " FAILED");
        return bad == 0 ? 0 : 1;
    }
}
`, strings.Join(calls, "\n"))
	if err := os.WriteFile(filepath.Join(dir, "ProbeMain.cs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	proj := fmt.Sprintf(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net10.0</TargetFramework>
    <ImplicitUsings>disable</ImplicitUsings>
    <Nullable>annotations</Nullable>
    <EnableDefaultCompileItems>false</EnableDefaultCompileItems>
    <AllowUnsafeBlocks>true</AllowUnsafeBlocks>
    <AssemblyName>csversioning</AssemblyName>
    <NoWarn>CS0162;CS0168;CS0219;CS8600;CS8602;CS8604;CS8625</NoWarn>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
    <Compile Include="gen/**/*.cs" />
    <Compile Include="%s/src/Serialize.cs" />
    <Compile Include="%s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, runtime, runtime)
	if err := os.WriteFile(filepath.Join(dir, "csversioning.csproj"), []byte(proj), 0o600); err != nil {
		t.Fatal(err)
	}

	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	cmd := exec.Command(dotnet, "run", "--project", dir, "-v", "q", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+corpus, "DOTNET_NOLOGO=1")
	out, runErr := cmd.CombinedOutput()

	csVersionAssert(t, out, runErr, "cfloat_range_widen/new_reads_old")
	csVersionAssert(t, out, runErr, "cfloat_range_widen/hostile")
	csVersionAssert(t, out, runErr, "cfloat_range_widen/past")
}
