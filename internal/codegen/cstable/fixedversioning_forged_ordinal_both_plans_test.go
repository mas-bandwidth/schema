package cstable

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// §5.8 row 12 `forged_ordinal_both_plans`: VOLD_/VNEW_enum_append read the SAME
// forged bytes twice — once by the NEW build's COMPILED plan, selected through
// the lineage, and once by the OLD build's IDENTITY plan, its own hash. The
// forge turns old_enum_append.bin's r0.tier from 3 (Gold) into 4, an ordinal
// past the writer's three variants and a name the reader does have. Both reads
// land tier None and never Platinum, leave seq == 9, and count clamped EXACTLY
// == 1 on both plans — never >= 1, since a leg that also counts in the ordinal
// op lands 2; the equality of the two clamped counts is the row's whole proof.
func TestFixedVersioningForgedOrdinalBothPlans(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	old := filepath.Join(corpus, "old_enum_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// old_enum_append.bin holds r0.tier=3 (Gold), r0.seq=9: the enum ordinal
	// (one byte) followed by its neighbour, the int32 scalar, LE.
	needle := []byte{3, 9, 0, 0, 0}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("tier=3 followed by seq=9 is not in %s", old)
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatalf("tier=3 followed by seq=9 occurs more than once in %s", old)
	}
	data[at] = 4 // the forge: an ordinal past the writer's three variants
	forged := filepath.Join(dir, "hostile_enum_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	probes := []csVersionProbe{
		{name: "forged_ordinal_both_plans/compiled", reader: "VNEW_enum_append",
			older: []string{"VOLD_enum_append"}, file: "hostile_enum_append.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan read one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a forged ordinal is not a refusal and not malformed on the COMPILED plan: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if ((int)back[0].Tier != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan lands the forged ordinal as None, not tier=" + (int)back[0].Tier);
            }
            if ((int)back[0].Tier == 4)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan never lands the forged ordinal as Platinum: tier=" + (int)back[0].Tier);
            }
            if (back[0].Seq != 9)
            {
                bad += ProbeLog.Fail(@NAME@, "the scalar after the enum is untouched: seq=" + back[0].Seq);
            }
            if (r.Clamped != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan counts the forged ordinal exactly once: clamped=" + r.Clamped);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter other than clamp moved on the COMPILED plan: widened=" + r.Widened + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
		{name: "forged_ordinal_both_plans/identity", reader: "VOLD_enum_append",
			older: nil, file: "hostile_enum_append.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan read one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a forged ordinal is not a refusal and not malformed on the IDENTITY plan: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (0 != (int)back[0].Tier)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan lands the forged ordinal as None, not tier=" + (int)back[0].Tier);
            }
            if ((int)back[0].Tier == 4)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan never lands the forged ordinal as Platinum: tier=" + (int)back[0].Tier);
            }
            if (back[0].Seq != 9)
            {
                bad += ProbeLog.Fail(@NAME@, "the scalar after the enum is untouched: seq=" + back[0].Seq);
            }
            if (1 != r.Clamped)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan counts the forged ordinal exactly once: clamped=" + r.Clamped);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter other than clamp moved on the IDENTITY plan: widened=" + r.Widened + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
	}

	var calls []string
	for i, p := range probes {
		ns, table, files := csVersionGenerate(t, p)
		gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
		csVersionWrite(t, gen, files)
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.NewReplacer(
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
	cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+dir, "DOTNET_NOLOGO=1")
	out, runErr := cmd.CombinedOutput()

	csVersionAssert(t, out, runErr, "forged_ordinal_both_plans/compiled")
	csVersionAssert(t, out, runErr, "forged_ordinal_both_plans/identity")
}
