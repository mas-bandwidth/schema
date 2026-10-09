package cstable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// §5.7's PREFILL, and this row exists because on THIS leg it was unobservable.
//
// THE MEASUREMENT, 2026-09-19 on the gate rig at 202af6e8. Shift 7 destroyed
// the declared-defaults prefill on every leg by making it write the WRONG
// value — `scalarReset`'s `(t) => { x = <default>; }` became `= default` — and
// one row of sixty-eight caught it on cs, against three to six elsewhere. Run
// the STRONGER mutation instead, the one that asks whether the prefill is
// needed at all — `(t) => { if (false) { x = <default>; } }`, so it compiles
// and never runs — and the answer is: SIXTY-NINE LEAVES, ZERO RED. The
// prefill can be deleted outright from this leg and not one landed fixed-table
// test notices.
//
// THE CAUSE IS THE PROBE, NOT THE LANGUAGE, WHICH IS WHY THIS IS A ROW AND NOT
// A DOCS RESIDUAL. Every cs probe builds its destination with
// `back[i] = new Lineage();`, and the generated C# class carries each declared
// default as a FIELD INITIALIZER — so the record ALREADY HOLDS the values the
// prefill exists to write, and "the prefill wrote 77" and "the constructor
// wrote 77" are the same sentence.
//
// THE POISON IS §5.7's, IN THE ONLY FORM THIS LEG CAN EXPRESS IT. The other
// legs memset 0x5A over a byte destination; C# has no reachable one here, so
// the poison is an EXPLICIT ASSIGNMENT of a value that is neither a declared
// default nor anything the writer wrote. 0x5A5A5A5A is 1515870810 as int32.
// After it, every byte the read owes is a value that must CHANGE, and `w == 77`
// can only have come from the prefill.
//
// The row is `field_append`: the OLD writer declares x/y/z and the NEW reader
// appends `w int32 = 77`. It is the ONE row on this leg whose appended slot has
// a NONZERO declared default — over a zeroed or constructor-default image a
// ZERO default passes whether the prefill ran or not, on every leg, so no other
// row here can carry this claim.
func TestFixedVersioningPrefillWrites(t *testing.T) {
	corpus := csFixedCorpus(t)
	dir := t.TempDir()

	probes := []csVersionProbe{
		{name: "prefill_writes", reader: "VNEW_field_append",
			older: []string{"VOLD_field_append"}, file: "old_field_append.bin",
			body: `            byte[] data = System.IO.File.ReadAllBytes(System.IO.Path.Combine(corpus, @FILE@));
            @TABLE@[] back = new @TABLE@[8];
            for (int i = 0; i < back.Length; ++i)
            {
                back[i] = new @TABLE@();
                // THE POISON (§5.7). The constructor has just written every
                // declared default, including w = 77 — the very value the
                // prefill owes. Overwrite all four with 0x5A5A5A5A, which is
                // neither a default nor anything the writer wrote, so the only
                // way w reads 77 after the load is that the PREFILL WROTE IT.
                back[i].X = unchecked((int)0x5A5A5A5A);
                back[i].Y = unchecked((int)0x5A5A5A5A);
                back[i].Z = unchecked((int)0x5A5A5A5A);
                back[i].W = unchecked((int)0x5A5A5A5A);
            }
            TableReport r = new TableReport();
            TableFixedEntry[] plan = new TableFixedEntry[8192];
            long n = Schema.@TABLE@FixedLoad(back, data, plan, r);
            if (n != 1)
            {
                bad += ProbeLog.Fail(@NAME@, "prefill_writes: the newer reader refused the older writer's file: n=" + n + " reason=" + r.Reason);
            }
            if (r.Refused || r.Malformed || r.Reason != null)
            {
                bad += ProbeLog.Fail(@NAME@, "a clean NEW-READS-OLD is not a refusal: reason=" + r.Reason + " malformed=" + r.Malformed);
            }
            if (back[0].X != 11 || back[0].Y != 22 || back[0].Z != 33)
            {
                bad += ProbeLog.Fail(@NAME@, "the old writer's values did not land over the poison: " + back[0].X + " " + back[0].Y + " " + back[0].Z);
            }
            if (back[0].W == unchecked((int)0x5A5A5A5A))
            {
                bad += ProbeLog.Fail(@NAME@, "the appended field KEPT THE POISON: the prefill wrote nothing there, and the constructor's default is not evidence that it did");
            }
            if (back[0].W != 77)
            {
                bad += ProbeLog.Fail(@NAME@, "the appended field is not its declared default: " + back[0].W);
            }
            if (r.Unknown != 0 || r.KindMismatch != 0 || r.Clamped != 0 || r.Duplicate != 0 || r.Widened != 0 || r.Retained != 0 || r.RetainLost != 0)
            {
                bad += ProbeLog.Fail(@NAME@, "counters moved on a clean backward read: unknown=" + r.Unknown + " kind=" + r.KindMismatch + " clamped=" + r.Clamped + " duplicate=" + r.Duplicate + " widened=" + r.Widened + " retained=" + r.Retained + " lost=" + r.RetainLost);
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

	csVersionAssert(t, out, runErr, "prefill_writes")
}
