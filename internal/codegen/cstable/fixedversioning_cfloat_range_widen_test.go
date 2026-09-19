package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// §5.8 row `cfloat_range_widen`: VOLD `aim float32 | min = -1, max = 1,
// resolution = 0.01`, VNEW widens to min = -2, max = 2. A compressed float
// rides as the float32 itself (SPEC §3.4), so min/max are digest definitions
// and the read's bounds pass is the READER's own: a forged value inside the new
// range (`hostile_`, 1.5) lands WHOLE with clamp 0, and only a value past the
// reader's own bound (`past_`, 5.0) clamps to 2.0 and counts clamp EXACTLY 1.
// `old_` (0.5) is the un-forged control with clamp 0. The clamp counter is
// asserted exactly, never >= 1, because this row catches a reader that counts
// twice; and `hostile_` alone would not prove the pass is the reader's, since
// deleting the emitter's bounds pass outright leaves `hostile_` green and only
// turns `past_` red.
func TestFixedVersioningCfloatRangeWiden(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	// TWO OF THE THREE EXPECTED VALUES COME FROM THE CORPUS MANIFEST AND THE
	// THIRD CANNOT (schema#1164). This leg already compared BITS, which is the
	// right comparison; what it did not do is take the number from the
	// reference. `old_`'s is the manifest's `values=r0.aim`, `hostile_`'s is the
	// manifest's own `forged=r0.aim@104` -- the value the reference wrote into
	// those bytes. `past_`'s 0x40000000 stays a literal and says why: the file
	// carries 5.0 and what lands is THIS READER'S OWN DECLARED MAX 2.0, which is
	// the whole content of that column and must not be dressed as an oracle
	// value.
	oldAim := fmt.Sprintf("0x%08X", csManifestFloatBits(t, corpus, "old_cfloat_range_widen.bin", "values", "r0.aim"))
	hostileAim := fmt.Sprintf("0x%08X", csManifestFloatBits(t, corpus, "hostile_cfloat_range_widen.bin", "forged", "r0.aim"))

	probes := []csVersionProbe{
		{name: "cfloat_range_widen/new_reads_old", reader: "VNEW_cfloat_range_widen",
			older: []string{"VOLD_cfloat_range_widen"}, file: "old_cfloat_range_widen.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the newer reader did not read exactly one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Lead != 1u || back[0].Trail != 2u)
            {
                bad += ProbeLog.Fail(@NAME@, "a neighbour moved: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (System.BitConverter.SingleToInt32Bits(back[0].Aim) != @OLDAIM@)
            {
                bad += ProbeLog.Fail(@NAME@, "the old writer's 0.5 did not land whole and uncounted: bits=0x" + System.BitConverter.SingleToInt32Bits(back[0].Aim).ToString("x8") + " clamped=" + r.Clamped);
            }
            if (r.Clamped != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "an in-range value counted a clamp: clamped=" + r.Clamped);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter other than clamp moved: widened=" + r.Widened + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
		{name: "cfloat_range_widen/hostile", reader: "VNEW_cfloat_range_widen",
			older: []string{"VOLD_cfloat_range_widen"}, suffix: "h", file: "hostile_cfloat_range_widen.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the newer reader did not read exactly one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Lead != 1u || back[0].Trail != 2u)
            {
                bad += ProbeLog.Fail(@NAME@, "a neighbour moved: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (System.BitConverter.SingleToInt32Bits(back[0].Aim) != @HOSTILEAIM@)
            {
                bad += ProbeLog.Fail(@NAME@, "1.5 inside the widened range did not land whole and uncounted: bits=0x" + System.BitConverter.SingleToInt32Bits(back[0].Aim).ToString("x8") + " clamped=" + r.Clamped);
            }
            if (r.Clamped != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a value inside the widened range counted a clamp: clamped=" + r.Clamped);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter other than clamp moved: widened=" + r.Widened + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
		{name: "cfloat_range_widen/past", reader: "VNEW_cfloat_range_widen",
			older: []string{"VOLD_cfloat_range_widen"}, suffix: "p", file: "past_cfloat_range_widen.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the newer reader did not read exactly one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Lead != 1u || back[0].Trail != 2u)
            {
                bad += ProbeLog.Fail(@NAME@, "a neighbour moved: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (System.BitConverter.SingleToInt32Bits(back[0].Aim) != 0x40000000)
            {
                bad += ProbeLog.Fail(@NAME@, "5.0 past the reader's own bound did not clamp to 2.0: bits=0x" + System.BitConverter.SingleToInt32Bits(back[0].Aim).ToString("x8") + " clamped=" + r.Clamped);
            }
            if (r.Clamped != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "a value past the reader's own bound clamps exactly once: clamped=" + r.Clamped);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter other than clamp moved: widened=" + r.Widened + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
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
			"@HOSTILEAIM@", hostileAim,
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
