package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedlimit"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE FIXED FORM'S HARD LIMITS (docs/SPEC-TABLES.md §3.4's fixed_max_depth and
// fixed_record_bound), the shared vectors every leg reads. The NEW reader holds
// the OLD layout as a lineage entry, so the plan it reads the AT-limit file
// with is compiled from a layout that passed the bound: the file reads whole,
// and a reader whose bound is one short refuses it. The PAST-limit file's
// layout is forged under the OLD hash and refused by name before any record is
// touched; the layout bytes on their own name the bound itself. Every answer is
// limits.txt's, which the C++ reference wrote from its own reader.

func csLimitCounters(v fixedlimit.Vector) string {
	return fmt.Sprintf("r.Unknown != %d || r.KindMismatch != %d || r.Widened != %d || r.Clamped != %d || r.Duplicate != %d || (r.Malformed ? 1 : 0) != %d",
		v.Counters["unknown"], v.Counters["kind_mismatch"], v.Counters["widened"], v.Counters["clamped"], v.Counters["duplicate"], v.Counters["malformed"])
}

// csLimitLoad reads one corpus file through the NEW reader and its layout
// through the reader's own parser; the statements after it see data, back, r,
// n and parse.
const csLimitLoad = `
            {
                byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
                @TABLE@[] back = new @TABLE@[1];
                back[0] = new @TABLE@();
                TableReport r = new TableReport();
                TableFixedEntry[] plan = new TableFixedEntry[8192];
                long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
                int length = (int) System.Buffers.Binary.BinaryPrimitives.ReadUInt32LittleEndian(data.AsSpan(16));
                string parse = Schema.TableFixedWire.ParseLayout(data.AsSpan(20, length), out TableFixedLayoutView view, out string why) ? "ok" : why;
                _ = view;
@CHECKS@
            }
`

func csLimitRun(t *testing.T, row, at, over string, checks func(fixedlimit.Vector) string) {
	t.Helper()
	slowtest.Gate(t, "dotnet (it runs the generated NEW reader over the limit vectors)")
	corpus := csFixedCorpus(t)
	a, err := fixedlimit.Read(corpus, at)
	if err != nil {
		t.Fatal(err)
	}
	o, err := fixedlimit.Read(corpus, over)
	if err != nil {
		t.Fatal(err)
	}
	if a.Refused() || !o.Refused() {
		t.Fatalf("limits.txt: %s must read and %s must refuse: %+v %+v", at, over, a, o)
	}
	atChecks := fmt.Sprintf(`                if (n != 1 || r.Refused || r.Malformed || r.Reason != null) { bad += ProbeLog.Fail(@NAME@, %[1]q + ": n=" + n + " reason=" + r.Reason + ", want one record"); }
                if (%[2]s) { bad += ProbeLog.Fail(@NAME@, %[1]q + ": a counter moved"); }
%[3]s                if (parse != %[4]q) { bad += ProbeLog.Fail(@NAME@, %[1]q + ": the layout parses as " + parse + ", want %[4]s"); }
`, at, csLimitCounters(a), checks(a), a.Parse)
	overChecks := fmt.Sprintf(`                if (n >= 0 || !r.Refused || r.Reason != %[2]q) { bad += ProbeLog.Fail(@NAME@, %[1]q + ": n=" + n + " reason=" + r.Reason + ", want refused %[2]s"); }
                if (%[3]s) { bad += ProbeLog.Fail(@NAME@, %[1]q + ": a refusal moved a counter"); }
                if (parse != %[4]q) { bad += ProbeLog.Fail(@NAME@, %[1]q + ": the layout parses as " + parse + ", want %[4]s"); }
`, over, o.Reason, csLimitCounters(o), o.Parse)
	body := strings.Replace(csLimitLoad, "@FILE@", fmt.Sprintf("%q", at), 1) + strings.Replace(csLimitLoad, "@FILE@", fmt.Sprintf("%q", over), 1)
	body = strings.Replace(body, "@CHECKS@", atChecks, 1)
	body = strings.Replace(body, "@CHECKS@", overChecks, 1)

	dir := t.TempDir()
	p := csVersionProbe{name: "limit_" + row, reader: "VNEW_" + row, older: []string{"VOLD_" + row}, suffix: "l", file: at, body: body}
	ns, table, files := csVersionGenerate(t, p)
	csVersionWrite(t, filepath.Join(dir, "gen", "u000"), files)
	src := strings.NewReplacer("@NAME@", fmt.Sprintf("%q", p.name), "@TABLE@", table).Replace(p.body)
	program := fmt.Sprintf(`using System;

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
`, ns, src, p.name)
	if err := os.WriteFile(filepath.Join(dir, "Probe.cs"), []byte(program), 0o600); err != nil {
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
    <AssemblyName>cslimit</AssemblyName>
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
	if err := os.WriteFile(filepath.Join(dir, "cslimit.csproj"), []byte(proj), 0o600); err != nil {
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

func csLimitCheck(t *testing.T, a fixedlimit.Vector, path, expr string) string {
	t.Helper()
	want, err := a.Int(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("                if ((long) %[1]s != %[2]dL) { bad += ProbeLog.Fail(@NAME@, %[3]q + \" landed \" + %[1]s + \", want %[2]d\"); }\n", expr, want, path)
}

func TestFixedLimitDepth(t *testing.T) {
	csLimitRun(t, "limit_depth", fixedlimit.DepthAt, fixedlimit.DepthOver, func(a fixedlimit.Vector) string {
		return csLimitCheck(t, a, "r0.lead", "back[0].Lead") +
			csLimitCheck(t, a, fixedlimit.DeepPath(), "back[0]"+strings.Repeat(".D", 63)+".V") +
			csLimitCheck(t, a, "r0.trail", "back[0].Trail")
	})
}

func TestFixedLimitRecord(t *testing.T) {
	csLimitRun(t, "limit_record", fixedlimit.RecordAt, fixedlimit.RecordOver, func(a fixedlimit.Vector) string {
		return csLimitCheck(t, a, "r0.lead", "back[0].Lead") +
			csLimitCheck(t, a, "r0.v", "back[0].V") +
			csLimitCheck(t, a, "r0.text_length", "back[0].TextLength") +
			csLimitCheck(t, a, "r0.text[0]", "back[0].Text[0]") +
			csLimitCheck(t, a, "r0.text[32761]", "back[0].Text[32761]") +
			csLimitCheck(t, a, "r0.text[65519]", "back[0].Text[65519]") +
			csLimitCheck(t, a, "r0.trail", "back[0].Trail")
	})
}
