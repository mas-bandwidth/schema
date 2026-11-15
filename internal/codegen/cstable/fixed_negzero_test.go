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
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A COMPRESSED FLOAT'S -0 LANDS AS +0 AND IS WRITTEN AS +0
// (docs/SPEC-TABLES.md §3.3, §3.4; docs/rulings.json fixed-compressed-negzero).
// The vectors are the reference writer's files (fixedvector.NegativeZero), each
// holding the sign bit alone in the compressed field of test/tables/FNZ.schema's
// root. This leg reads each through its generated load: the load refuses
// nothing and counts nothing (every report member is at its zero value), the
// field lands the value 0 with the sign bit clear, and the save writes the
// canonical bytes: the same file with that field's word cleared. A plain
// float's -0 is carried bit-exact and is not this row's.
func TestFixedCompressedNegZeroRead(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the .NET SDK (it runs the generated C# reader over the compressed-float -0 vectors)")
	corpus := csFixedCorpus(t)
	u := csUnitOf(t, csReadSchema(t, "FNZ"))
	for _, vec := range fixedvector.NegativeZero {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(filepath.Join(corpus, vec.File)); err != nil {
				t.Fatalf("the shared vector %s is not in the corpus: %v", vec.File, err)
			}
			if u.Tables[vec.Root] == nil {
				t.Fatalf("FNZ.schema declares no root %s", vec.Root)
			}
			csNegZeroRun(t, u, corpus, vec)
		})
	}
}

func csNegZeroRun(t *testing.T, unit *ir.Unit, corpus string, vec fixedvector.Vector) {
	t.Helper()
	files, err := csharp.Generate(unit)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(unit, map[string][]FixedLineageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)

	dir := t.TempDir()
	csVersionWrite(t, filepath.Join(dir, "gen"), files)

	ns := capitalize(unit.Package)
	body := strings.NewReplacer(
		"@TABLE@", vec.Root,
		"@FILE@", fmt.Sprintf("%q", vec.File),
	).Replace(`
            string f = @FILE@;
            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, f));
            int layoutBytes = (int)BitConverter.ToUInt32(data, 16);
            // lead (4 bytes) comes first in the record, then the compressed value.
            int valueAt = 16 + 4 + layoutBytes + 8 + 4;
            uint stored = BitConverter.ToUInt32(data, valueAt);
            if (stored != 0x80000000u)
            {
                bad += ProbeLog.Fail("negzero", f + ": the vector does not hold a compressed float's -0: " + stored.ToString("x8"));
                return bad;
            }

            @TABLE@[] back = new @TABLE@[] { new @TABLE@() };
            Schema.TableReset(back[0]);
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != 1)
            {
                bad += ProbeLog.Fail("negzero", f + ": the reader refused a compressed -0: n=" + n + " verdict=" + r.Verdict + " reason=" + r.Reason);
                return bad;
            }
            // NOTHING IS COUNTED: every member of the report is at its zero value.
            if (r.Unknown != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0 ||
                r.Refused || r.Reason != null || r.NonFiniteFloat != null || r.NonFiniteReason != null ||
                r.Verdict != Schema.TableWire.Verdict.Ok || r.Damaged || r.KindChanged != 0 || r.LayoutHash != 0)
            {
                bad += ProbeLog.Fail("negzero", f + ": the read of a compressed -0 reported unknown=" + r.Unknown +
                    " widened=" + r.Widened + " retained=" + r.Retained + " retainLost=" + r.RetainLost +
                    " refused=" + r.Refused + " reason=" + r.Reason + " nonFinite=" + r.NonFiniteFloat +
                    " nonFiniteReason=" + r.NonFiniteReason + " verdict=" + r.Verdict + " damaged=" + r.Damaged +
                    " kindChanged=" + r.KindChanged + " layoutHash=" + r.LayoutHash);
            }
            uint bits = BitConverter.SingleToUInt32Bits(back[0].Value);
            if (bits != 0x00000000u)
            {
                bad += ProbeLog.Fail("negzero", f + ": a compressed float's -0 kept its sign: " + bits.ToString("x8"));
            }
            long need = Schema.@TABLE@FixedMeasure(1);
            byte[] saved = new byte[need];
            long got = Schema.@TABLE@FixedSave(back, saved);
            byte[] want = (byte[])data.Clone();
            Array.Clear(want, valueAt, 4);
            if (got != data.Length || !saved.AsSpan(0, (int)got).SequenceEqual(want))
            {
                bad += ProbeLog.Fail("negzero", f + ": saving the landed +0 did not write the canonical bytes: " + got + "/" + data.Length);
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
        public static int Run(string corpus)
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
        return %[1]s.Probe000.Run(Environment.GetEnvironmentVariable("SCHEMA_FIXEDFORM_CORPUS")) == 0 ? 0 : 1;
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
	cmd.Env = append(os.Environ(), "SCHEMA_FIXEDFORM_CORPUS="+corpus, "DOTNET_NOLOGO=1")
	out, runErr := cmd.CombinedOutput()
	if strings.Contains(string(out), "ROW negzero: ok") {
		return
	}
	t.Fatalf("the compressed-float -0 probe did not go green on %s:\n%s", vec.File, csVersionTail(string(out), runErr))
}
