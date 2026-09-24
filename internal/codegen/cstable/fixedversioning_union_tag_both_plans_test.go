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

// union_tag_both_plans (§5.8, the union twin of forged_ordinal_both_plans): a
// union TAG past the arm set was guarded by nothing on this leg before this
// row. VOLD_/VNEW_union_append read the SAME forged bytes twice — once by the
// NEW build's COMPILED plan, selected through the lineage, and once by the OLD
// build's IDENTITY plan, its own hash. The forge turns old_union_append.bin's
// one-byte tag from 1 (alpha) into 9, past the OLD writer's two arms AND the
// NEW reader's three, so no arm lands. The needle — 01 07 00 00 00 0f 00 00 00,
// pick.type=alpha, m=7, seq=15 — is located with bytes.Index and asserted to
// occur EXACTLY once, never zero and never twice, because a search that is not
// a locator is not a forge. Both plans land the tag None and never the forged 9,
// and leave seq at the writer's 15. The bound that wins is the WRITER's. On this
// leg the COMPILED plan lands None through the lineage plan's guarded const with
// NO counter (clamped == 0, schema#1254), while the IDENTITY plan counts exactly
// once through the decode bound's clamp (clamped == 1), so each column asserts
// what its own plan does. clamped is asserted == an exact number, never >= 1:
// a looser read is the thing this row exists to catch.
func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	old := filepath.Join(corpus, "old_union_append.bin")
	data, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	// old_union_append.bin holds r0.pick.type=1 (alpha), r0.pick.alpha.m=7,
	// r0.seq=15: the one-byte union tag, then the selected arm's payload, then
	// the int32 scalar, all LE. The nine-byte needle must occur EXACTLY once.
	needle := []byte{0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00}
	at := bytes.Index(data, needle)
	if at < 0 {
		t.Fatalf("pick.type=alpha, m=7, seq=15 is not in %s", old)
	}
	if bytes.Contains(data[at+1:], needle) {
		t.Fatalf("pick.type=alpha, m=7, seq=15 occurs more than once in %s", old)
	}
	data[at] = 9 // the forge: past the OLD writer's two arms and the NEW reader's three
	forged := filepath.Join(dir, "hostile_union_append.bin")
	if err := os.WriteFile(forged, data, 0o600); err != nil {
		t.Fatal(err)
	}

	probes := []csVersionProbe{
		{name: "union_tag_both_plans/compiled", reader: "VNEW_union_append",
			older: []string{"VOLD_union_append"}, file: "hostile_union_append.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan read one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a forged tag is not a refusal and not malformed on the COMPILED plan: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Pick.Type != PickType.None)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan lands the forged tag as None, not pick.type=" + (int)back[0].Pick.Type);
            }
            if ((int)back[0].Pick.Type == 9)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan never lands the forged tag as 9: pick.type=" + (int)back[0].Pick.Type);
            }
            if (back[0].Seq != 15)
            {
                bad += ProbeLog.Fail(@NAME@, "the scalar after the union is untouched: seq=" + back[0].Seq);
            }
            if (r.Clamped != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "the COMPILED plan lands None through the lineage plan's guarded const with no counter: clamped=" + r.Clamped);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter other than clamp moved on the COMPILED plan: widened=" + r.Widened + " unknown=" + r.Unknown + " kind=" + r.KindMismatch + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
		{name: "union_tag_both_plans/identity", reader: "VOLD_union_append",
			older: nil, file: "hostile_union_append.bin",
			body: csVersionHead + `            _ = want;
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan read one record: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a forged tag is not a refusal and not malformed on the IDENTITY plan: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Pick.Type != PickType.None)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan lands the forged tag as None, not pick.type=" + (int)back[0].Pick.Type);
            }
            if ((int)back[0].Pick.Type == 9)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan never lands the forged tag as 9: pick.type=" + (int)back[0].Pick.Type);
            }
            if (back[0].Seq != 15)
            {
                bad += ProbeLog.Fail(@NAME@, "the scalar after the union is untouched: seq=" + back[0].Seq);
            }
            if (r.Clamped != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the IDENTITY plan counts the forged tag exactly once through the decode bound's clamp: clamped=" + r.Clamped);
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

	csVersionAssert(t, out, runErr, "union_tag_both_plans/compiled")
	csVersionAssert(t, out, runErr, "union_tag_both_plans/identity")
}
