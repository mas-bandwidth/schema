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

// EVERY DEAD BYTE IS ZERO ON THE LINEAGE READ TOO (docs/rulings.json
// fixed-dead-bytes-zero; docs/SPEC-TABLES.md §3.4): the slack is the WRITER's.
// The NEW build ([..8]int32) reads the OLD file ([..4]int32, count 4, vals
// 1000..1003) through its lineage. The forge drops the count to 2, so the old
// writer's slots 2 and 3 become slack while still holding 1002 and 1003: the
// read must refuse the whole record, wire_damaged, and land nothing. The control
// zeroes those two slots too, and the same compiled plan then reads count 2.
func TestFixedVersioningDeadBytesLineage(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	probe := csVersionProbe{
		name:   "dead_bytes/new_reads_old",
		reader: "VNEW_array_bounded_grow",
		older:  []string{"VOLD_array_bounded_grow"},
		file:   "old_array_bounded_grow.bin",
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
            if (found == -1) { return bad; }
            // THE STAIN: count 2, so the writer's slots 2 and 3 (1002, 1003) are slack.
            System.Buffers.Binary.BinaryPrimitives.WriteUInt32LittleEndian(data.AsSpan(found + 4), 2u);
            if (System.Buffers.Binary.BinaryPrimitives.ReadInt32LittleEndian(data.AsSpan(found + 8 + 8)) != 1002 ||
                System.Buffers.Binary.BinaryPrimitives.ReadInt32LittleEndian(data.AsSpan(found + 8 + 12)) != 1003)
            {
                bad += ProbeLog.Fail(@NAME@, "CONTROL: the writer's slots 2 and 3 are not 1002 and 1003, so the stain is not where this probe says");
            }
            {
                @TABLE@[] back = new @TABLE@[8];
                for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
                back[0].Lead = 0x5A5A5A5Au;
                back[0].ValsCount = 0;
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
                if (n != -1)
                {
                    bad += ProbeLog.Fail(@NAME@, "a non-zero byte in the writer's array slack must refuse the record: n=" + n + " reason=" + r.Reason);
                }
                if (!r.Refused || r.Reason != "wire_damaged")
                {
                    bad += ProbeLog.Fail(@NAME@, "the refusal is wire_damaged by name: reason=" + r.Reason + " refused=" + r.Refused);
                }
                // a refused record is reset to its declared defaults (lead = 1,
                // no vals), never left holding the plan's partial landing
                if (back[0].Lead != 1u || back[0].ValsCount != 0 || back[0].Trail != 2u)
                {
                    bad += ProbeLog.Fail(@NAME@, "the refusal kept part of the record: lead=" + back[0].Lead + " count=" + back[0].ValsCount + " trail=" + back[0].Trail);
                }
            }
            // THE CONTROL: the same file with the slack zeroed reads clean.
            data.AsSpan(found + 8 + 8, 8).Clear();
            {
                @TABLE@[] back = new @TABLE@[8];
                for (int i = 0; i < back.Length; ++i) { back[i] = new @TABLE@(); }
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
                if (n != 1 || r.Refused || r.Reason != null)
                {
                    bad += ProbeLog.Fail(@NAME@, "CONTROL: zeroed slack reads clean: n=" + n + " reason=" + r.Reason);
                }
                if (back[0].ValsCount != 2 || back[0].Vals[0] != 1000 || back[0].Vals[1] != 1001)
                {
                    bad += ProbeLog.Fail(@NAME@, "CONTROL: the live count and slots land: count=" + back[0].ValsCount + " vals=" + back[0].Vals[0] + "," + back[0].Vals[1]);
                }
                if (back[0].Lead != 0xAAAAAAAAu || back[0].Trail != 0xBBBBBBBBu)
                {
                    bad += ProbeLog.Fail(@NAME@, "CONTROL: the neighbours land: lead=" + back[0].Lead + " trail=" + back[0].Trail);
                }
            }`,
	}

	ns, table, files := csVersionGenerate(t, probe)
	gen := filepath.Join(dir, "gen", "u000")
	csVersionWrite(t, gen, files)
	joined := ""
	for _, data := range files {
		joined += string(data)
	}
	body := strings.ReplaceAll(probe.body, "@JSON@", csVersionJSON(joined, table))
	body = strings.NewReplacer(
		"@NAME@", fmt.Sprintf("%q", probe.name),
		"@TABLE@", table,
		"@FILE@", fmt.Sprintf("%q", probe.file),
	).Replace(body)
	src := fmt.Sprintf(`// generated by internal/codegen/cstable's versioning harness: one probe per row per COLUMN
using System;

namespace %s
{
    static class Probe000
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
`, ns, body, fmt.Sprintf("%q", probe.name))
	if err := os.WriteFile(filepath.Join(dir, "Probe000.cs"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
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
            bad += %s.Probe000.Run(corpus);
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
`, ns)
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

	csVersionAssert(t, out, runErr, "dead_bytes/new_reads_old")
}
