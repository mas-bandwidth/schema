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
// shared cross-version vectors every leg reads (fixedvector.WriterRange): an
// OLD writer's file with one ranged value forged past that writer's range, read
// by the NEW reader whose own range is wider: a scalar, one element of a ranged
// fixed array (held the same as a scalar), and a uint64 at or above 2^63
// (compared unsigned). Under the strict read the value past the WRITER's range
// refuses the whole load and never clamps; the answer is the manifest's, which
// carries no `expect=` when the C++ reference's own NEW reader refused the bytes
// (fixedvector.Read answers Refused), and the landed values when it did not.
func TestFixedVersioningRangeWidenHostile(t *testing.T) {
	slowtest.Gate(t, "dotnet (it runs the generated NEW reader over the forged file)")
	corpus := csFixedCorpus(t)
	runtime, err := filepath.Abs("../../../../serialize.cs")
	if err != nil {
		t.Fatal(err)
	}
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			leaves, err := want.Leaves()
			if err != nil {
				t.Fatal(err)
			}
			var checks strings.Builder
			for _, l := range leaves {
				expr := "back[0]." + strings.ToUpper(l.Field[:1]) + l.Field[1:]
				if l.Index >= 0 {
					expr += fmt.Sprintf("[%d]", l.Index)
				}
				fmt.Fprintf(&checks, `
            if (%[1]s.ToString() != %[2]q)
            {
                bad += ProbeLog.Fail(@NAME@, %[3]q + " landed " + %[1]s + ", want %[2]s: the reference landed it from the same bytes");
            }`, expr, fmt.Sprint(l.Value), l.Path)
			}
			dir := t.TempDir()
			p := csVersionProbe{name: vec.Name + "/hostile", reader: "VNEW_" + vec.Row,
				older: []string{"VOLD_" + vec.Row}, suffix: "h", file: vec.File,
				body: csVersionHead + fmt.Sprintf(`            _ = want;
            if (n != 1 || r.Refused || r.Verdict != Schema.TableWire.Verdict.Ok || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "the forged file reads one record: n=" + n + " reason=" + r.Reason);
            }%[1]s
            if (r.Widened != 0 || r.Unknown != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "a counter moved");
            }`, checks.String())}
			if want.Refused || want.Clamped > 0 {
				// A value past the WRITER's range refuses the whole load and the
				// destination holds its declared defaults (FIXED-FORM-ALGORITHM
				// §4.6, "REFUSE is total"); it never clamps.
				p.body = `            @TABLE@ fresh = new @TABLE@();
` + csVersionHead + `            _ = want;
            if (n != -1 || !r.Refused || r.Verdict != Schema.TableWire.Verdict.Refused)
            {
                bad += ProbeLog.Fail(@NAME@, "a value past the WRITER's range must refuse the load, never clamp: n=" + n + " verdict=" + r.Verdict);
            }
            if (r.Widened != 0 || r.Unknown != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "REFUSE is total: no counter moves");
            }
@JSON@`
			}
			ns, table, files := csVersionGenerate(t, p)
			csVersionWrite(t, filepath.Join(dir, "gen", "u000"), files)
			joined := ""
			for _, data := range files {
				joined += string(data)
			}
			p.body = strings.ReplaceAll(p.body, "@JSON@", csVersionJSON(joined, table))
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
		})
	}
}
