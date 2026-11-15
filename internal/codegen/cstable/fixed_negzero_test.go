package cstable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COMPRESSED FLOAT'S -0 LANDS AS +0 (docs/SPEC-TABLES.md §3.3, §3.4;
// docs/rulings.json fixed-compressed-negzero): the read's bounds pass lands the
// value 0 itself, keeps no sign bit and counts nothing, so a read-then-save
// writes the canonical +0 image. A plain float's -0 is carried bit-exact and is
// not this row's. The probe forges the sign bit in a clean writer image so the
// record under test holds the bits of -0 whatever the writer does with a value
// that already is one.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it runs the generated C# reader over a compressed float's -0)")

	const schema = `package negzero

fixed table NegativeZero
{
    a float32 | min = -1, max = 1, resolution = 0.01
    b float32 | min = -1, max = 1, resolution = 0.01
}
`
	u := csUnitOf(t, schema)
	st := u.Tables["NegativeZero"]
	if st == nil {
		t.Fatal("the probe schema has no NegativeZero table")
	}
	offA := ir.TableFixedMemberOffset(u, st, "a")
	offB := ir.TableFixedMemberOffset(u, st, "b")

	files, err := csharp.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	dir := t.TempDir()
	csVersionWrite(t, filepath.Join(dir, "gen"), files)

	ns := capitalize(u.Package)
	body := strings.NewReplacer(
		"@A@", fmt.Sprint(offA),
		"@B@", fmt.Sprint(offB),
	).Replace(`
            NegativeZero v = new NegativeZero();
            Schema.TableReset(v);
            v.A = 0.0f;
            v.B = 0.0f;
            byte[] clean = new byte[Schema.NegativeZeroFixedMeasure(1)];
            long wrote = Schema.NegativeZeroFixedSave(new NegativeZero[] { v }, clean);
            if (wrote != clean.Length)
            {
                bad += ProbeLog.Fail("negzero", "the writer did not fill its measure: " + wrote + "/" + clean.Length);
                return bad;
            }
            byte[] data = (byte[])clean.Clone();
            int at = 16 + 4 + Schema.NegativeZeroFixedLayout.Length + 8;
            // THE BITS OF -0, written into the record so the reader meets the
            // sign bit alone in each field.
            data[at + @A@ + 3] = 0x80;
            data[at + @B@ + 3] = 0x80;

            NegativeZero[] back = new NegativeZero[] { new NegativeZero() };
            Schema.TableReset(back[0]);
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.NegativeZeroFixedLoad(back, data, plan, r);
            if (n != 1 || r.Refused || r.Verdict != Schema.TableWire.Verdict.Ok || r.Reason != null)
            {
                bad += ProbeLog.Fail("negzero", "the clean record with two -0 fields did not read: n=" + n + " verdict=" + r.Verdict + " reason=" + r.Reason);
            }
            else
            {
                uint a = BitConverter.SingleToUInt32Bits(back[0].A);
                uint b = BitConverter.SingleToUInt32Bits(back[0].B);
                if (a != 0x00000000u)
                {
                    bad += ProbeLog.Fail("negzero", "a landed " + a.ToString("x8") + ", not +0");
                }
                if (b != 0x00000000u)
                {
                    bad += ProbeLog.Fail("negzero", "b landed " + b.ToString("x8") + ", not +0");
                }
                byte[] saved = new byte[clean.Length];
                long got = Schema.NegativeZeroFixedSave(back, saved);
                if (got != clean.Length || !saved.AsSpan(0, (int)got).SequenceEqual(clean))
                {
                    bad += ProbeLog.Fail("negzero", "read-then-save did not write the canonical +0 image: " + got + "/" + clean.Length);
                }
            }
`)

	src := fmt.Sprintf(`using System;

static class ProbeLog
{
    public static int Fail(string row, string what)
    {
        Console.WriteLine("ROW " + row + ": FAILED " + what);
        return 1;
    }
}

namespace %[1]s
{
    static class Probe000
    {
        public static int Run()
        {
            int bad = 0;
%[2]s
            if (bad == 0) { Console.WriteLine("ROW negzero: ok"); }
            return bad;
        }
    }
}

static class ProbeMain
{
    static int Main()
    {
        return %[1]s.Probe000.Run() == 0 ? 0 : 1;
    }
}
`, ns, body)
	if err := os.WriteFile(filepath.Join(dir, "Probe.cs"), []byte(src), 0o600); err != nil {
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
    <AssemblyName>csnegzero</AssemblyName>
    <NoWarn>CS0162;CS0168;CS0219;CS8600;CS8602;CS8604;CS8625</NoWarn>
  </PropertyGroup>
  <ItemGroup>
    <Compile Include="*.cs" />
    <Compile Include="gen/**/*.cs" />
    <Compile Include="%[1]s/src/Serialize.cs" />
    <Compile Include="%[1]s/src/Int128Pair.cs" />
  </ItemGroup>
</Project>
`, runtime)
	if err := os.WriteFile(filepath.Join(dir, "csnegzero.csproj"), []byte(proj), 0o600); err != nil {
		t.Fatal(err)
	}

	dotnet := os.Getenv("DOTNET")
	if dotnet == "" {
		dotnet = "dotnet"
	}
	cmd := exec.Command(dotnet, "run", "--project", dir, "-v", "q", "--nologo")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOTNET_NOLOGO=1")
	out, runErr := cmd.CombinedOutput()
	if strings.Contains(string(out), "ROW negzero: ok") {
		return
	}
	t.Fatalf("the compressed-float -0 probe did not go green:\n%s", csVersionTail(string(out), runErr))
}
