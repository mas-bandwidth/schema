package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5), the
// shared cross-version vector every leg reads. hostile_range_widen.bin is the
// OLD writer's | 0..100 file with `v` forged to 150, inside the NEW reader's
// | 0..200; the NEW reader clamps it to the WRITER's max and counts `clamped`
// once. The answer is the manifest's `expect=`, which is what the C++
// reference's own NEW reader landed from the same bytes.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "dotnet (it runs the generated NEW reader over the forged file)")
	corpus := csFixedCorpus(t)
	want, err := fixedvector.Read(corpus, fixedvector.RangeWidenHostile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := want.Int("r0.v")
	if err != nil {
		t.Fatal(err)
	}
	lead, err := want.Int("r0.lead")
	if err != nil {
		t.Fatal(err)
	}
	trail, err := want.Int("r0.trail")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := csVersionProbe{name: "range_widen/hostile", reader: "VNEW_range_widen",
		older: []string{"VOLD_range_widen"}, suffix: "h", file: fixedvector.RangeWidenHostile,
		body: csVersionHead + fmt.Sprintf(`            _ = want;
            Console.WriteLine("range_widen_hostile: landed v=" + back[0].V + " clamped=" + r.Clamped);
            if (n != 1 || r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "the forged file reads one record: n=" + n + " reason=" + r.Reason);
            }
            if (back[0].V != %[1]d || r.Clamped != %[2]d)
            {
                bad += ProbeLog.Fail(@NAME@, "v=" + back[0].V + " clamped=" + r.Clamped + ", want v=%[1]d clamped=%[2]d: the forged 150 clamps to the WRITER's max and counts once");
            }
            if (back[0].Lead != %[3]du || back[0].Trail != %[4]du)
            {
                bad += ProbeLog.Fail(@NAME@, "a neighbour moved: lead=" + back[0].Lead + " trail=" + back[0].Trail);
            }
            if (r.Widened != 0 || r.Unknown != 0 || r.KindMismatch != 0 || r.Duplicate != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter other than clamp moved");
            }`, v, want.Clamped, lead, trail)}
	ns, table, files := csVersionGenerate(t, p)
	csVersionWrite(t, filepath.Join(dir, "gen", "u000"), files)
	body := strings.NewReplacer(
		"@NAME@", fmt.Sprintf("%q", p.name),
		"@TABLE@", table,
		"@FILE@", fmt.Sprintf("%q", p.file),
	).Replace(p.body)
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
            if (bad == 0) { Console.WriteLine("ROW " + %[3]q + ": ok"); }
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
`, ns, body, p.name)
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
    <AssemblyName>csrangewiden</AssemblyName>
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
	if err := os.WriteFile(filepath.Join(dir, "csrangewiden.csproj"), []byte(proj), 0o600); err != nil {
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
	csVersionAssert(t, out, runErr, p.name)
	t.Logf("%s", out)
}
