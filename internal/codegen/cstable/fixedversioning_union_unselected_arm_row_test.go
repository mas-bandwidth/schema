package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// ruled 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed
// tag did not select is UNSPECIFIED on separate-storage targets. The tag and the
// SELECTED arm are the whole of what a union read promises, so the row's whole
// content is the assertion it REFUSES to make — this probe does NOT compare
// pick.beta or pick.gamma. C# leaves the caller's bytes in an unselected arm,
// and that is lawful. The poison is laid FIELD BY FIELD on the destination
// objects (the destination is a managed array and there is no memset over it),
// on BOTH columns, and a future reader who "completes" this test by asserting
// pick.gamma.p == 0 has reversed a ruling and should read the issue first.
// Named *_arm_row_test.go, not *_arm_test.go: Go reads a trailing _arm before
// _test.go as a GOARCH build constraint and silently skips the file.
func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	compiledBody := `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            back[0].Pick.Type = (PickType)0x5A;
            back[0].Pick.Alpha.M = 0x5A5A5A5A;
            back[0].Pick.Beta.N = 0x5A5A5A5A;
            back[0].Pick.Gamma.P = 0x5A5A5A5A;
            back[0].Seq = 0x5A5A5A5A;
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the file carries one record, not " + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Pick.Type != PickType.Alpha)
            {
                bad += ProbeLog.Fail(@NAME@, "pick.type is the writer's alpha arm, not " + back[0].Pick.Type);
            }
            if (back[0].Pick.Alpha.M != 7)
            {
                bad += ProbeLog.Fail(@NAME@, "pick.alpha.m is the writer's 7, not " + back[0].Pick.Alpha.M);
            }
            if (back[0].Seq != 15)
            {
                bad += ProbeLog.Fail(@NAME@, "seq is the writer's 15, not " + back[0].Seq);
            }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read moved a counter: unknown=" + r.Unknown + " kind=" + r.KindMismatch + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`

	identityBody := `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            back[0].Pick.Type = (PickType)0x5A;
            back[0].Pick.Alpha.M = 0x5A5A5A5A;
            back[0].Pick.Beta.N = 0x5A5A5A5A;
            back[0].Seq = 0x5A5A5A5A;
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the file carries one record, not " + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Pick.Type != PickType.Alpha)
            {
                bad += ProbeLog.Fail(@NAME@, "pick.type is the writer's alpha arm, not " + back[0].Pick.Type);
            }
            if (back[0].Pick.Alpha.M != 7)
            {
                bad += ProbeLog.Fail(@NAME@, "pick.alpha.m is the writer's 7, not " + back[0].Pick.Alpha.M);
            }
            if (back[0].Seq != 15)
            {
                bad += ProbeLog.Fail(@NAME@, "seq is the writer's 15, not " + back[0].Seq);
            }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean read moved a counter: unknown=" + r.Unknown + " kind=" + r.KindMismatch + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`

	compiled := csVersionProbe{
		name:   "union_unselected_arm/new_reads_old",
		reader: "VNEW_union_append",
		older:  []string{"VOLD_union_append"},
		file:   "old_union_append.bin",
		body:   compiledBody,
	}
	identity := csVersionProbe{
		name:   "union_unselected_arm/identity",
		reader: "VOLD_union_append",
		file:   "old_union_append.bin",
		body:   identityBody,
	}

	var calls []string
	for i, p := range []csVersionProbe{compiled, identity} {
		ns, table, files := csVersionGenerate(t, p)
		gen := filepath.Join(dir, "gen", fmt.Sprintf("u%03d", i))
		csVersionWrite(t, gen, files)
		joined := ""
		for _, data := range files {
			joined += string(data)
		}
		probeClass := fmt.Sprintf("Probe%03d", i)
		body := strings.ReplaceAll(p.body, "@JSON@", csVersionJSON(joined, table))
		body = strings.NewReplacer(
			"@NAME@", fmt.Sprintf("%q", p.name),
			"@TABLE@", table,
			"@FILE@", fmt.Sprintf("%q", p.file),
		).Replace(body)
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

	csVersionAssert(t, out, runErr, compiled.name)
	csVersionAssert(t, out, runErr, identity.name)
}
