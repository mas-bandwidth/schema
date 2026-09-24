package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// §5.8 count_clamp_ends, the roadmap's two array-bounds tasks cs/C1 "count
// clamp v<0" and cs/C2 "count clamp v>Max", whose titles are quoted verbatim.
// Row 4 forges the count to 7, between the writer's bound 4 and the reader's
// 8, so it exercises NEITHER end: not negative, not past the reader's own max.
// This row forges the two ends instead. The negative arm was measured on
// 2026-09-19 to be guarded by NOTHING on five legs — c, cs, dart, elixir, js —
// whose landed fixed-table tests all stayed green with it deleted, so C1 is the
// receipt this row exists to hold. clamped is asserted == 1, never >= 1,
// because a leg that counts once in the count op and again in the bounds pass
// lands 2 and a looser read is the thing this row catches. And C2's landed
// count of 4 is the WRITER's bound carried by the plan, never the reader's own
// 8 and never the forged 9.
func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	probes := []csVersionProbe{
		{name: "count_clamp_ends/c1", reader: "VNEW_array_bounded_grow",
			older: []string{"VOLD_array_bounded_grow"}, file: "old_array_bounded_grow.bin",
			body: `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            // THE FORGE'S LOCATOR: lead 0xAAAAAAAA immediately followed by the
            // count 4, both little-endian uint32 - the eight-byte needle that
            // must occur exactly once. Any other multiplicity is not a locator.
            byte[] needle = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
            int found = -1;
            for (int i = 0; i + needle.Length <= data.Length; i++)
            {
                bool ok = true;
                for (int j = 0; j < needle.Length; j++) { if (data[i + j] != needle[j]) { ok = false; break; } }
                if (ok)
                {
                    if (found != -1) { bad += ProbeLog.Fail(@NAME@, "the lead+count needle occurs twice; it is not a locator"); }
                    found = i;
                }
            }
            if (found == -1) { bad += ProbeLog.Fail(@NAME@, "the lead+count needle occurs zero times; it is not a locator"); }
            System.Buffers.Binary.BinaryPrimitives.WriteUInt32LittleEndian(data.AsSpan(found + 4), 0xFFFFFFFFu);
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "count_clamp_ends/c1: n is not 1: " + n);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a forged count is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Lead != 0xAAAAAAAAu || back[0].Trail != 0xBBBBBBBBu)
            {
                bad += ProbeLog.Fail(@NAME@, "the row moved a neighbour: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (back[0].ValsCount != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "the negative count clamps to ZERO, never -1 nor the writer's 4 nor the reader's 8: " + back[0].ValsCount);
            }
            if (r.Clamped != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the bounds pass counts once per entry per record, so clamped is EXACTLY 1, never 2: clamped=" + r.Clamped);
            }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "counters moved on a clean clamped read: unknown=" + r.Unknown + " kind=" + r.KindMismatch + " widened=" + r.Widened + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
            }`},
		{name: "count_clamp_ends/c2", reader: "VNEW_array_bounded_grow",
			older: []string{"VOLD_array_bounded_grow"}, suffix: "c2", file: "old_array_bounded_grow.bin",
			body: `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            // THE FORGE'S LOCATOR: lead 0xAAAAAAAA immediately followed by the
            // count 4, both little-endian uint32 - the eight-byte needle that
            // must occur exactly once. Any other multiplicity is not a locator.
            byte[] needle = { 0xAA, 0xAA, 0xAA, 0xAA, 0x04, 0x00, 0x00, 0x00 };
            int found = -1;
            for (int i = 0; i + needle.Length <= data.Length; i++)
            {
                bool ok = true;
                for (int j = 0; j < needle.Length; j++) { if (data[i + j] != needle[j]) { ok = false; break; } }
                if (ok)
                {
                    if (found != -1) { bad += ProbeLog.Fail(@NAME@, "the lead+count needle occurs twice; it is not a locator"); }
                    found = i;
                }
            }
            if (found == -1) { bad += ProbeLog.Fail(@NAME@, "the lead+count needle occurs zero times; it is not a locator"); }
            System.Buffers.Binary.BinaryPrimitives.WriteUInt32LittleEndian(data.AsSpan(found + 4), 9u);
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "count_clamp_ends/c2: n is not 1: " + n);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a forged count is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].Lead != 0xAAAAAAAAu || back[0].Trail != 0xBBBBBBBBu)
            {
                bad += ProbeLog.Fail(@NAME@, "the row moved a neighbour: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (back[0].ValsCount != 4)
            {
                bad += ProbeLog.Fail(@NAME@, "the count past both bounds clamps to the WRITER's bound 4, never the reader's own 8 nor the forged 9: " + back[0].ValsCount);
            }
            for (int i = 0; i < 4; ++i)
            {
                if (back[0].Vals[i] != 1000 + i)
                {
                    bad += ProbeLog.Fail(@NAME@, "vals[" + i + "] did not land its own value: " + back[0].Vals[i] + " != " + (1000 + i));
                }
            }
            for (int i = 4; i < 8; ++i)
            {
                if (back[0].Vals[i] != 0)
                {
                    bad += ProbeLog.Fail(@NAME@, "vals[" + i + "] is not the reader's declared default: " + back[0].Vals[i]);
                }
            }
            if (r.Clamped != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "the bounds pass counts once per entry per record, so clamped is EXACTLY 1, never 2: clamped=" + r.Clamped);
            }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Widened != 0 || r.Duplicate != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "counters moved on a clean clamped read: unknown=" + r.Unknown + " kind=" + r.KindMismatch + " widened=" + r.Widened + " duplicate=" + r.Duplicate + " retained=" + r.Retained + " lost=" + r.RetainLost);
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
	cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+corpus, "DOTNET_NOLOGO=1")
	out, runErr := cmd.CombinedOutput()

	csVersionAssert(t, out, runErr, "count_clamp_ends/c1")
	csVersionAssert(t, out, runErr, "count_clamp_ends/c2")
}
